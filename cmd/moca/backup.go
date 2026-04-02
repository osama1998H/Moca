package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/pkg/backup"
)

// NewBackupCommand returns the "moca backup" command group with all subcommands.
func NewBackupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Backup operations",
		Long:  "Create, restore, and manage site backups.",
	}

	cmd.AddCommand(
		newBackupCreateCmd(),
		newBackupRestoreCmd(),
		newBackupListCmd(),
		newBackupVerifyCmd(),
		newSubcommand("schedule", "Configure automated backups"),
		newSubcommand("upload", "Upload backup to remote storage"),
		newSubcommand("download", "Download backup from remote storage"),
		newSubcommand("prune", "Delete old backups per retention policy"),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// backup create
// ---------------------------------------------------------------------------

func newBackupCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Backup a site (or all sites)",
		Long:  "Create a database backup for a site. Produces a timestamped .sql.gz file.",
		RunE:  runBackupCreate,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")
	f.Bool("compress", true, "Compress backup with gzip")

	return cmd
}

func runBackupCreate(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return err
	}

	if depErr := backup.CheckDependencies(); depErr != nil {
		return output.NewCLIError("Missing PostgreSQL tools").
			WithCause(depErr.Error()).
			WithFix("Install pg_dump and psql, then retry.")
	}

	compress, _ := cmd.Flags().GetBool("compress")

	s := w.NewSpinner("Creating backup...")
	s.Start()

	info, err := backup.Create(cmd.Context(), backup.CreateOptions{
		Site:        siteName,
		ProjectRoot: ctx.ProjectRoot,
		Compress:    compress,
		DBConfig:    dbConnConfig(ctx.Project.Infrastructure.Database),
	})
	if err != nil {
		s.Stop("Failed")
		return output.NewCLIError("Backup creation failed").
			WithErr(err).
			WithCause(err.Error()).
			WithContext("site: " + siteName)
	}

	s.Stop("Backup created")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(info)
	}

	w.PrintSuccess(fmt.Sprintf("Backup created: %s", info.ID))
	w.Print("  Path: %s", info.Path)
	w.Print("  Size: %s", formatBytes(info.Size))
	w.Print("  Checksum: %s", info.Checksum)
	w.Print("")
	w.Print("Verify with: moca backup verify %s", info.Path)

	return nil
}

// ---------------------------------------------------------------------------
// backup restore
// ---------------------------------------------------------------------------

func newBackupRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore BACKUP_FILE",
		Short: "Restore a site from backup",
		Long:  "Restore a site's database from a backup file. Drops and recreates the site schema.",
		Args:  cobra.ExactArgs(1),
		RunE:  runBackupRestore,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")
	f.Bool("force", false, "Skip confirmation prompt")

	return cmd
}

func runBackupRestore(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	backupPath := args[0]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return err
	}

	if depErr := backup.CheckDependencies(); depErr != nil {
		return output.NewCLIError("Missing PostgreSQL tools").
			WithCause(depErr.Error()).
			WithFix("Install pg_dump and psql, then retry.")
	}

	force, _ := cmd.Flags().GetBool("force")
	if !force {
		confirmed, promptErr := confirmPrompt(
			fmt.Sprintf("This will DROP and recreate the schema for site '%s'. Continue?", siteName),
		)
		if promptErr != nil {
			return promptErr
		}
		if !confirmed {
			w.PrintWarning("Restore cancelled.")
			return nil
		}
	}

	s := w.NewSpinner("Restoring backup...")
	s.Start()

	err = backup.Restore(cmd.Context(), backup.RestoreOptions{
		Site:       siteName,
		BackupPath: backupPath,
		Force:      force,
		DBConfig:   dbConnConfig(ctx.Project.Infrastructure.Database),
	})
	if err != nil {
		s.Stop("Failed")
		return output.NewCLIError("Backup restore failed").
			WithErr(err).
			WithCause(err.Error()).
			WithContext("site: " + siteName + ", file: " + backupPath)
	}

	s.Stop("Restore complete")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"site":   siteName,
			"backup": backupPath,
			"status": "restored",
		})
	}

	w.PrintSuccess(fmt.Sprintf("Site '%s' restored from %s", siteName, backupPath))
	return nil
}

// ---------------------------------------------------------------------------
// backup list
// ---------------------------------------------------------------------------

func newBackupListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available backups",
		Long:  "List backup files for a site, sorted by date (newest first).",
		RunE:  runBackupList,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")
	f.Int("limit", 20, "Maximum number of backups to show")

	return cmd
}

func runBackupList(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return err
	}

	backups, err := backup.List(cmd.Context(), siteName, ctx.ProjectRoot)
	if err != nil {
		return output.NewCLIError("Failed to list backups").
			WithErr(err).
			WithContext("site: " + siteName)
	}

	limit, _ := cmd.Flags().GetInt("limit")
	if limit > 0 && len(backups) > limit {
		backups = backups[:limit]
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(backups)
	}

	if len(backups) == 0 {
		w.PrintInfo(fmt.Sprintf("No backups found for site '%s'.", siteName))
		w.Print("  Create one with: moca backup create --site %s", siteName)
		return nil
	}

	headers := []string{"BACKUP ID", "SITE", "TYPE", "SIZE", "AGE", "VERIFIED"}
	var rows [][]string
	for _, b := range backups {
		verified := "✗"
		if b.Verified {
			verified = "✓"
		}
		rows = append(rows, []string{
			b.ID,
			b.Site,
			b.Type,
			formatBytes(b.Size),
			formatAge(b.CreatedAt),
			verified,
		})
	}

	return w.PrintTable(headers, rows)
}

// ---------------------------------------------------------------------------
// backup verify
// ---------------------------------------------------------------------------

func newBackupVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify BACKUP_FILE",
		Short: "Verify backup integrity",
		Long:  "Validate a backup file's integrity (checksum, gzip envelope, SQL structure).",
		Args:  cobra.ExactArgs(1),
		RunE:  runBackupVerify,
	}

	f := cmd.Flags()
	f.Bool("deep", false, "Perform deep verification (decompress and count SQL objects)")

	return cmd
}

func runBackupVerify(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	backupPath := args[0]

	deep, _ := cmd.Flags().GetBool("deep")

	s := w.NewSpinner("Verifying backup...")
	s.Start()

	result, err := backup.Verify(cmd.Context(), backupPath, deep)
	if err != nil {
		s.Stop("Failed")
		return output.NewCLIError("Verification failed").
			WithErr(err).
			WithContext("file: " + backupPath)
	}

	if result.Valid {
		s.Stop("Verification passed")
	} else {
		s.Stop("Verification failed")
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(result)
	}

	if result.Valid {
		w.PrintSuccess(fmt.Sprintf("Backup is valid: %s", result.BackupID))
		w.Print("  Checksum: %s", result.Checksum)
		if deep && result.ObjectCount > 0 {
			w.Print("  SQL objects: %d", result.ObjectCount)
		}
	} else {
		w.PrintError(fmt.Sprintf("Backup is invalid: %s", result.BackupID))
		w.Print("  Error: %s", result.Error)
	}

	return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// dbConnConfig maps internal config.DatabaseConfig to backup.DBConnConfig.
func dbConnConfig(cfg config.DatabaseConfig) backup.DBConnConfig {
	return backup.DBConnConfig{
		Host:     cfg.Host,
		Port:     cfg.Port,
		User:     cfg.User,
		Password: cfg.Password,
		Database: cfg.SystemDB,
	}
}

// formatAge returns a human-readable age string from a timestamp.
func formatAge(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	}
}

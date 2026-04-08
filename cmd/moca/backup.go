package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/pkg/backup"
	"github.com/osama1998H/moca/pkg/storage"
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
		newBackupScheduleCmd(),
		newBackupUploadCmd(),
		newBackupDownloadCmd(),
		newBackupPruneCmd(),
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

// ---------------------------------------------------------------------------
// backup schedule
// ---------------------------------------------------------------------------

func newBackupScheduleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Configure automated backups",
		Long:  "Configure and manage automated backup scheduling via system crontab.",
		RunE:  runBackupSchedule,
	}

	f := cmd.Flags()
	f.String("cron", "", `Cron expression (e.g., "0 2 * * *")`)
	f.Bool("show", false, "Show current backup schedule")
	f.Bool("disable", false, "Disable automated backups")
	f.Bool("enable", false, "Enable automated backups")

	return cmd
}

func runBackupSchedule(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	projectName := ctx.Project.Project.Name
	disable, _ := cmd.Flags().GetBool("disable")
	enable, _ := cmd.Flags().GetBool("enable")
	cronExpr, _ := cmd.Flags().GetString("cron")

	switch {
	case disable:
		if err := backup.DisableCronSchedule(cmd.Context(), projectName); err != nil {
			return output.NewCLIError("Failed to disable backup schedule").
				WithErr(err).WithCause(err.Error())
		}
		w.PrintSuccess("Backup schedule disabled.")
		return nil

	case enable:
		if err := backup.EnableCronSchedule(cmd.Context(), projectName); err != nil {
			return output.NewCLIError("Failed to enable backup schedule").
				WithErr(err).WithCause(err.Error())
		}
		w.PrintSuccess("Backup schedule enabled.")
		return nil

	case cronExpr != "":
		if err := backup.InstallCronSchedule(cmd.Context(), cronExpr, projectName, ctx.ProjectRoot); err != nil {
			return output.NewCLIError("Failed to configure backup schedule").
				WithErr(err).WithCause(err.Error()).
				WithFix("Verify the cron expression is valid (e.g., \"0 2 * * *\").")
		}
		w.PrintSuccess(fmt.Sprintf("Backup schedule configured: %s", cronExpr))
		return nil

	default:
		// Default: show current schedule.
		info, err := backup.ShowSchedule(cmd.Context(), projectName)
		if err != nil {
			return output.NewCLIError("Failed to read backup schedule").
				WithErr(err).WithCause(err.Error())
		}

		if w.Mode() == output.ModeJSON {
			return w.PrintJSON(info)
		}

		if !info.Installed {
			w.PrintInfo("No backup schedule configured.")
			w.Print("  Configure with: moca backup schedule --cron \"0 2 * * *\"")
			return nil
		}

		status := "enabled"
		if !info.Enabled {
			status = "disabled"
		}
		w.Print("Schedule:     %s", info.CronExpr)
		w.Print("Status:       %s", status)
		w.Print("Project root: %s", info.ProjectRoot)
		return nil
	}
}

// ---------------------------------------------------------------------------
// backup upload
// ---------------------------------------------------------------------------

func newBackupUploadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upload BACKUP_ID",
		Short: "Upload backup to remote storage",
		Long:  "Upload a local backup to configured remote storage (S3/MinIO).",
		Args:  cobra.ExactArgs(1),
		RunE:  runBackupUpload,
	}

	f := cmd.Flags()
	f.String("destination", "", "Override remote storage destination (s3://bucket/prefix)")
	f.Bool("delete-local", false, "Delete local copy after successful upload")
	f.String("site", "", "Target site name")

	return cmd
}

func runBackupUpload(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	backupID := args[0]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return err
	}

	// Find the local backup by ID.
	backups, err := backup.List(cmd.Context(), siteName, ctx.ProjectRoot)
	if err != nil {
		return output.NewCLIError("Failed to list backups").
			WithErr(err).WithContext("site: " + siteName)
	}

	var info *backup.BackupInfo
	for i := range backups {
		if backups[i].ID == backupID {
			info = &backups[i]
			break
		}
	}
	if info == nil {
		return output.NewCLIError(fmt.Sprintf("Backup %q not found", backupID)).
			WithContext("site: " + siteName).
			WithFix("Run 'moca backup list' to see available backups.")
	}

	destination, _ := cmd.Flags().GetString("destination")
	remote, err := buildRemoteStorage(ctx.Project.Infrastructure.Storage, ctx.Project.Backup.Destination, destination)
	if err != nil {
		return err
	}

	s := w.NewSpinner("Uploading backup...")
	s.Start()

	key, err := remote.Upload(cmd.Context(), *info)
	if err != nil {
		s.Stop("Failed")
		return output.NewCLIError("Backup upload failed").
			WithErr(err).WithCause(err.Error()).
			WithContext("backup: " + backupID)
	}

	s.Stop("Upload complete")

	deleteLocal, _ := cmd.Flags().GetBool("delete-local")
	if deleteLocal {
		if err := os.Remove(info.Path); err != nil {
			w.PrintWarning(fmt.Sprintf("Upload succeeded but failed to delete local file: %v", err))
		} else {
			w.PrintInfo("Local copy deleted.")
		}
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"backup_id":  backupID,
			"remote_key": key,
			"size":       info.Size,
			"status":     "uploaded",
		})
	}

	w.PrintSuccess(fmt.Sprintf("Backup uploaded: %s", backupID))
	w.Print("  Remote key: %s", key)
	w.Print("  Size: %s", formatBytes(info.Size))
	return nil
}

// ---------------------------------------------------------------------------
// backup download
// ---------------------------------------------------------------------------

func newBackupDownloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download BACKUP_ID",
		Short: "Download backup from remote storage",
		Long:  "Download a backup from remote storage to local disk.",
		Args:  cobra.ExactArgs(1),
		RunE:  runBackupDownload,
	}

	f := cmd.Flags()
	f.String("output", "", "Local download path (default: site backup dir)")
	f.String("source", "", "Override remote storage source")
	f.String("site", "", "Target site name")

	return cmd
}

func runBackupDownload(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	backupID := args[0]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return err
	}

	source, _ := cmd.Flags().GetString("source")
	remote, err := buildRemoteStorage(ctx.Project.Infrastructure.Storage, ctx.Project.Backup.Destination, source)
	if err != nil {
		return err
	}

	// List remote backups to find the one matching BACKUP_ID.
	remoteBackups, err := remote.ListRemote(cmd.Context(), siteName)
	if err != nil {
		return output.NewCLIError("Failed to list remote backups").
			WithErr(err).WithContext("site: " + siteName)
	}

	var target *backup.RemoteBackupInfo
	for i := range remoteBackups {
		if remoteBackups[i].ID == backupID {
			target = &remoteBackups[i]
			break
		}
	}
	if target == nil {
		return output.NewCLIError(fmt.Sprintf("Backup %q not found in remote storage", backupID)).
			WithContext("site: " + siteName).
			WithFix("Run 'moca backup list --remote' to see available remote backups.")
	}

	// Determine output directory.
	outputDir, _ := cmd.Flags().GetString("output")
	if outputDir == "" {
		outputDir = filepath.Join(ctx.ProjectRoot, "sites", siteName, "backups")
	}
	if mkdirErr := os.MkdirAll(outputDir, 0o755); mkdirErr != nil {
		return output.NewCLIError("Failed to create output directory").
			WithErr(mkdirErr).WithContext("path: " + outputDir)
	}

	s := w.NewSpinner("Downloading backup...")
	s.Start()

	localPath, checksum, err := remote.Download(cmd.Context(), target.RemoteKey, outputDir)
	if err != nil {
		s.Stop("Failed")
		return output.NewCLIError("Backup download failed").
			WithErr(err).WithCause(err.Error()).
			WithContext("backup: " + backupID)
	}

	s.Stop("Download complete")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"backup_id": backupID,
			"path":      localPath,
			"checksum":  checksum,
			"size":      target.Size,
			"status":    "downloaded",
		})
	}

	w.PrintSuccess(fmt.Sprintf("Backup downloaded: %s", backupID))
	w.Print("  Path: %s", localPath)
	w.Print("  Checksum: %s", checksum)
	w.Print("  Size: %s", formatBytes(target.Size))
	return nil
}

// ---------------------------------------------------------------------------
// backup prune
// ---------------------------------------------------------------------------

func newBackupPruneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Delete old backups per retention policy",
		Long:  "Delete old backups according to the retention policy defined in moca.yaml.",
		RunE:  runBackupPrune,
	}

	f := cmd.Flags()
	f.Bool("dry-run", false, "Show what would be deleted")
	f.Bool("force", false, "Skip confirmation prompt")
	f.String("site", "", "Target site name")

	return cmd
}

func runBackupPrune(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return err
	}

	ret := ctx.Project.Backup.Retention
	if ret.Daily == 0 && ret.Weekly == 0 && ret.Monthly == 0 {
		return output.NewCLIError("No retention policy configured").
			WithContext("All retention values are 0 — this would delete all backups outside the 24h safety window.").
			WithFix("Set backup.retention in moca.yaml (e.g., daily: 7, weekly: 4, monthly: 3).")
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Build remote storage if configured.
	var remote *backup.RemoteStorage
	if ctx.Project.Backup.Destination.Driver == "s3" {
		r, remoteErr := buildRemoteStorage(ctx.Project.Infrastructure.Storage, ctx.Project.Backup.Destination, "")
		if remoteErr != nil {
			w.PrintWarning(fmt.Sprintf("Remote storage unavailable, pruning local only: %v", remoteErr))
		} else {
			remote = r
		}
	}

	result, err := backup.Prune(cmd.Context(), backup.PruneOptions{
		Site:        siteName,
		ProjectRoot: ctx.ProjectRoot,
		Retention:   ret,
		Remote:      remote,
		DryRun:      dryRun,
		Now:         time.Now(),
	})
	if err != nil {
		return output.NewCLIError("Backup pruning failed").
			WithErr(err).WithCause(err.Error()).
			WithContext("site: " + siteName)
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(result)
	}

	if len(result.Deleted) == 0 {
		w.PrintInfo("No backups to prune.")
		return nil
	}

	if dryRun {
		w.PrintInfo(fmt.Sprintf("Dry run: %d backup(s) would be deleted:", len(result.Deleted)))
		headers := []string{"BACKUP ID", "AGE", "SIZE"}
		var rows [][]string
		for _, b := range result.Deleted {
			rows = append(rows, []string{b.ID, formatAge(b.CreatedAt), formatBytes(b.Size)})
		}
		if tableErr := w.PrintTable(headers, rows); tableErr != nil {
			return tableErr
		}
		w.Print("")
		w.Print("Run without --dry-run to delete.")
		return nil
	}

	// Confirm before deleting.
	force, _ := cmd.Flags().GetBool("force")
	if !force {
		confirmed, promptErr := confirmPrompt(
			fmt.Sprintf("Delete %d backup(s)?", len(result.Deleted)),
		)
		if promptErr != nil {
			return promptErr
		}
		if !confirmed {
			w.PrintWarning("Prune cancelled.")
			return nil
		}

		// Re-run without dry-run now that user confirmed.
		result, err = backup.Prune(cmd.Context(), backup.PruneOptions{
			Site:        siteName,
			ProjectRoot: ctx.ProjectRoot,
			Retention:   ret,
			Remote:      remote,
			DryRun:      false,
			Now:         time.Now(),
		})
		if err != nil {
			return output.NewCLIError("Backup pruning failed").
				WithErr(err).WithCause(err.Error())
		}
	}

	w.PrintSuccess(fmt.Sprintf("Pruned %d backup(s), kept %d.", len(result.Deleted), len(result.Kept)))
	for _, e := range result.Errors {
		w.PrintWarning("  " + e)
	}
	return nil
}

// ---------------------------------------------------------------------------
// remote storage helpers
// ---------------------------------------------------------------------------

// buildRemoteStorage constructs a backup.RemoteStorage from project config.
// The destinationOverride flag (e.g., "s3://bucket/prefix") takes precedence.
func buildRemoteStorage(storageCfg config.StorageConfig, backupDest config.BackupDestination, destinationOverride string) (*backup.RemoteStorage, error) {
	// BackupDestination.Bucket overrides the default storage bucket.
	if backupDest.Bucket != "" {
		storageCfg.Bucket = backupDest.Bucket
	}

	prefix := backupDest.Prefix

	// CLI flag override: parse "s3://bucket/prefix".
	if destinationOverride != "" {
		bucket, p, err := parseS3URI(destinationOverride)
		if err != nil {
			return nil, err
		}
		storageCfg.Bucket = bucket
		if p != "" {
			prefix = p
		}
	}

	if storageCfg.Endpoint == "" {
		return nil, output.NewCLIError("No S3 endpoint configured").
			WithFix("Set infrastructure.storage.endpoint in moca.yaml or pass --destination s3://bucket/prefix.")
	}

	s3, err := storage.NewS3Storage(storageCfg)
	if err != nil {
		return nil, output.NewCLIError("Failed to connect to remote storage").
			WithErr(err).WithCause(err.Error()).
			WithFix("Check infrastructure.storage settings in moca.yaml.")
	}

	return backup.NewRemoteStorage(s3, prefix), nil
}

// parseS3URI parses "s3://bucket/prefix/path" into bucket and prefix.
func parseS3URI(uri string) (bucket, prefix string, err error) {
	if !strings.HasPrefix(uri, "s3://") {
		return "", "", output.NewCLIError(fmt.Sprintf("Invalid S3 URI: %q", uri)).
			WithFix("Use format: s3://bucket-name or s3://bucket-name/prefix")
	}

	trimmed := strings.TrimPrefix(uri, "s3://")
	parts := strings.SplitN(trimmed, "/", 2)
	bucket = parts[0]
	if bucket == "" {
		return "", "", output.NewCLIError("Empty bucket name in S3 URI").
			WithFix("Use format: s3://bucket-name or s3://bucket-name/prefix")
	}
	if len(parts) > 1 {
		prefix = parts[1]
	}
	return bucket, prefix, nil
}

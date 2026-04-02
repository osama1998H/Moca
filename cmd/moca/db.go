package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/pkg/apps"
	"github.com/osama1998H/moca/pkg/backup"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/orm"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// NewDBCommand returns the "moca db" command group with all subcommands.
func NewDBCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database operations",
		Long:  "Manage database schema, migrations, seeds, and fixtures.",
	}

	cmd.AddCommand(
		newDBMigrateCmd(),
		newDBRollbackCmd(),
		newDBDiffCmd(),
		newDBConsoleCmd(),
		newDBSnapshotCmd(),
		newDBSeedCmd(),
		newDBTrimTablesCmd(),
		newDBTrimDatabaseCmd(),
		newDBExportFixturesCmd(),
		newDBResetCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// db migrate
// ---------------------------------------------------------------------------

func newDBMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run pending schema migrations",
		Long: `Run pending schema migrations. This is the lower-level form of
'moca site migrate' — it only handles database schema, not the
full migration lifecycle (cache clear, search rebuild, etc.).`,
		RunE: runDBMigrate,
	}

	f := cmd.Flags()
	f.Bool("dry-run", false, "Show SQL that would be executed without running it")
	f.Int("step", 0, "Run only N migrations (0 = all)")
	f.String("skip", "", "Skip a specific migration by app:version")

	return cmd
}

func runDBMigrate(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return err
	}

	appsDir := filepath.Join(ctx.ProjectRoot, "apps")
	migrations, err := gatherMigrations(appsDir)
	if err != nil {
		return output.NewCLIError("Failed to gather migrations").
			WithErr(err).
			WithCause(err.Error())
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	step, _ := cmd.Flags().GetInt("step")
	skip, _ := cmd.Flags().GetString("skip")

	if dryRun {
		previews, dryRunErr := svc.Runner.DryRun(cmd.Context(), siteName, migrations)
		if dryRunErr != nil {
			return output.NewCLIError("Dry run failed").
				WithErr(dryRunErr).
				WithCause(dryRunErr.Error())
		}

		if len(previews) == 0 {
			w.PrintInfo("No pending migrations.")
			return nil
		}

		if w.Mode() == output.ModeJSON {
			return w.PrintJSON(previews)
		}

		w.Print("Pending migrations (dry run):")
		w.Print("")
		for _, p := range previews {
			w.Print("-- %s:%s", p.AppName, p.Version)
			w.Print("%s", p.SQL)
			w.Print("")
		}
		return nil
	}

	opts := orm.MigrateOptions{
		Step: step,
		Skip: skip,
	}

	s := w.NewSpinner("Running migrations...")
	s.Start()
	result, err := svc.Runner.Apply(cmd.Context(), siteName, migrations, opts)
	if err != nil {
		s.Stop("Failed")
		return output.NewCLIError("Migration failed").
			WithErr(err).
			WithCause(err.Error()).
			WithContext("site: " + siteName)
	}
	s.Stop("Migrations complete")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"applied": len(result.Applied),
			"skipped": len(result.Skipped),
			"batch":   result.Batch,
		})
	}

	if len(result.Applied) == 0 {
		w.PrintInfo("No pending migrations.")
		return nil
	}

	w.PrintSuccess(fmt.Sprintf("Applied %d migration(s) in batch %d", len(result.Applied), result.Batch))
	for _, m := range result.Applied {
		w.Print("  %s %s:%s", w.Color().Success("✓"), m.AppName, m.Version)
	}
	if len(result.Skipped) > 0 {
		w.Print("")
		w.PrintWarning(fmt.Sprintf("Skipped %d migration(s)", len(result.Skipped)))
		for _, m := range result.Skipped {
			w.Print("  %s %s:%s", w.Color().Muted("-"), m.AppName, m.Version)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// db rollback
// ---------------------------------------------------------------------------

func newDBRollbackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "Rollback last migration batch",
		Long:  "Rollback the last migration batch, executing DOWN SQL in reverse order.",
		RunE:  runDBRollback,
	}

	f := cmd.Flags()
	f.Int("step", 1, "Number of batches to rollback")
	f.Bool("dry-run", false, "Show what would be rolled back without executing")

	return cmd
}

func runDBRollback(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	step, _ := cmd.Flags().GetInt("step")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	opts := orm.RollbackOptions{
		Step:   step,
		DryRun: dryRun,
	}

	s := w.NewSpinner("Rolling back migrations...")
	s.Start()
	result, err := svc.Runner.Rollback(cmd.Context(), siteName, opts)
	if err != nil {
		s.Stop("Failed")
		return output.NewCLIError("Rollback failed").
			WithErr(err).
			WithCause(err.Error()).
			WithContext("site: " + siteName)
	}
	s.Stop("Rollback complete")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"rolled_back": len(result.Applied),
			"dry_run":     result.DryRun,
		})
	}

	if len(result.Applied) == 0 {
		w.PrintInfo("Nothing to rollback.")
		return nil
	}

	label := "Rolled back"
	if result.DryRun {
		label = "Would rollback"
	}
	w.PrintSuccess(fmt.Sprintf("%s %d migration(s)", label, len(result.Applied)))
	for _, m := range result.Applied {
		w.Print("  %s %s:%s", w.Color().Warning("↩"), m.AppName, m.Version)
	}
	return nil
}

// ---------------------------------------------------------------------------
// db diff
// ---------------------------------------------------------------------------

func newDBDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Show schema diff (meta vs actual DB)",
		Long: `Compare MetaType definitions against the actual database schema
and show the differences. Useful for diagnosing schema drift.`,
		RunE: runDBDiff,
	}

	f := cmd.Flags()
	f.String("doctype", "", "Check a specific DocType (required)")
	f.String("output", "text", "Output format: text, sql, json")

	return cmd
}

func runDBDiff(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return err
	}

	doctype, _ := cmd.Flags().GetString("doctype")
	if doctype == "" {
		return output.NewCLIError("--doctype flag is required").
			WithFix("Specify a DocType to diff: moca db diff --doctype SalesOrder")
	}

	outputFmt, _ := cmd.Flags().GetString("output")

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	// Load current MetaType from registry (what the DB knows about).
	current, err := svc.Registry.Get(cmd.Context(), siteName, doctype)
	if err != nil {
		// If not found in registry, current is nil (new DocType).
		current = nil
		w.Debugf("DocType %q not found in registry, treating as new", doctype)
	}

	// Load desired MetaType from app files.
	appsDir := filepath.Join(ctx.ProjectRoot, "apps")
	desired, err := findDoctypeInApps(appsDir, doctype)
	if err != nil {
		return output.NewCLIError(fmt.Sprintf("Cannot find DocType %q in app files", doctype)).
			WithErr(err).
			WithFix("Ensure the DocType JSON file exists in apps/<app>/modules/<module>/doctypes/")
	}

	statements := svc.Migrator.Diff(current, desired)

	switch outputFmt {
	case "json":
		type diffEntry struct {
			SQL     string `json:"sql"`
			Comment string `json:"comment"`
		}
		entries := make([]diffEntry, len(statements))
		for i, s := range statements {
			entries[i] = diffEntry{SQL: s.SQL, Comment: s.Comment}
		}
		data, _ := json.MarshalIndent(entries, "", "  ")
		w.Print("%s", string(data))

	case "sql":
		if len(statements) == 0 {
			w.Print("-- No differences found")
			return nil
		}
		for _, s := range statements {
			w.Print("%s;", s.SQL)
		}

	default: // "text"
		if len(statements) == 0 {
			w.PrintSuccess(fmt.Sprintf("DocType %q: schema matches definition", doctype))
			return nil
		}
		w.Print("Schema diff for DocType %q on site %q:", doctype, siteName)
		w.Print("")
		for _, s := range statements {
			w.Print("  %s %s", w.Color().Warning("~"), s.Comment)
			w.Debugf("    SQL: %s", s.SQL)
		}
		w.Print("")
		w.Print("Summary: %d difference(s) found. Run 'moca db migrate' to apply.", len(statements))
	}

	return nil
}

// findDoctypeInApps scans all apps to find a MetaType JSON definition for the given doctype name.
// It walks the standard module structure: {appDir}/modules/{module}/doctypes/{doctype}/{doctype}.json
func findDoctypeInApps(appsDir, doctype string) (*meta.MetaType, error) {
	appInfos, err := apps.ScanApps(appsDir)
	if err != nil {
		return nil, err
	}

	snake := doctypeToSnake(doctype)

	for _, ai := range appInfos {
		if ai.Manifest == nil {
			continue
		}
		for _, mod := range ai.Manifest.Modules {
			modSnake := doctypeToSnake(mod.Name)
			jsonPath := filepath.Join(ai.Path, "modules", modSnake, "doctypes", snake, snake+".json")

			data, err := os.ReadFile(jsonPath)
			if err != nil {
				continue // file doesn't exist, try next
			}

			mt, compileErr := meta.Compile(data)
			if compileErr != nil {
				return nil, fmt.Errorf("compile doctype %q: %w", doctype, compileErr)
			}
			return mt, nil
		}
	}

	return nil, fmt.Errorf("DocType %q not found in any app", doctype)
}

// doctypeToSnake converts a PascalCase or camelCase name to snake_case.
func doctypeToSnake(s string) string {
	var result strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				result.WriteByte('_')
			}
			result.WriteRune(r + ('a' - 'A'))
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// ---------------------------------------------------------------------------
// db console
// ---------------------------------------------------------------------------

func newDBConsoleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "console",
		Short: "Open interactive psql session",
		Long: `Opens an interactive PostgreSQL session for the active site's schema.
Uses psql with the site's search_path set automatically.`,
		RunE: runDBConsole,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")
	f.Bool("system", false, "Connect to the moca_system schema instead")
	f.Bool("readonly", false, "Open in read-only mode")

	return cmd
}

func runDBConsole(cmd *cobra.Command, _ []string) error {
	if depErr := backup.CheckDependencies(); depErr != nil {
		return output.NewCLIError("Missing PostgreSQL tools").
			WithCause(depErr.Error()).
			WithFix("Install psql, then retry.")
	}

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	system, _ := cmd.Flags().GetBool("system")
	readonly, _ := cmd.Flags().GetBool("readonly")

	var searchPath string
	if system {
		searchPath = "moca_system"
	} else {
		siteName, siteErr := resolveSiteName(cmd, ctx)
		if siteErr != nil {
			return siteErr
		}
		searchPath = tenancy.SchemaNameForSite(siteName)
	}

	dbCfg := ctx.Project.Infrastructure.Database

	psqlPath, _ := exec.LookPath("psql")

	args := []string{
		"psql",
		"-h", dbCfg.Host,
		"-p", strconv.Itoa(dbCfg.Port),
		"-U", dbCfg.User,
		"-d", dbCfg.SystemDB,
	}

	if readonly {
		args = append(args, "-v", "ON_ERROR_STOP=1", "--set", "default_transaction_read_only=on")
	}

	env := os.Environ()
	if dbCfg.Password != "" {
		env = append(env, "PGPASSWORD="+dbCfg.Password)
	}
	env = append(env, fmt.Sprintf("PGOPTIONS=-c search_path=%s", searchPath))

	// Use syscall.Exec on Unix for clean process replacement.
	if runtime.GOOS != "windows" {
		return syscall.Exec(psqlPath, args, env)
	}

	// Windows fallback: use exec.Command with stdin/stdout/stderr passthrough.
	psqlCmd := exec.CommandContext(cmd.Context(), psqlPath, args[1:]...)
	psqlCmd.Env = env
	psqlCmd.Stdin = os.Stdin
	psqlCmd.Stdout = os.Stdout
	psqlCmd.Stderr = os.Stderr
	return psqlCmd.Run()
}

// ---------------------------------------------------------------------------
// db seed
// ---------------------------------------------------------------------------

func newDBSeedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Load seed/fixture data",
		Long: `Load seed/fixture data from app fixture files.
Fixture files are JSON arrays of documents located in apps/{app}/fixtures/.`,
		RunE: runDBSeed,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")
	f.String("app", "", "Seed data from a specific app only")
	f.String("file", "", "Seed from a specific fixture file")
	f.Bool("force", false, "Overwrite existing data (upsert)")

	return cmd
}

func runDBSeed(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return err
	}

	appFilter, _ := cmd.Flags().GetString("app")
	fileFilter, _ := cmd.Flags().GetString("file")
	force, _ := cmd.Flags().GetBool("force")

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	pool, poolErr := svc.DB.ForSite(cmd.Context(), siteName)
	if poolErr != nil {
		return output.NewCLIError("Cannot connect to site database").
			WithErr(poolErr).
			WithContext("site: " + siteName)
	}

	appsDir := filepath.Join(ctx.ProjectRoot, "apps")
	appInfos, scanErr := apps.ScanApps(appsDir)
	if scanErr != nil {
		return output.NewCLIError("Failed to scan apps").WithErr(scanErr)
	}

	s := w.NewSpinner("Loading seed data...")
	s.Start()

	type seedResult struct {
		App      string `json:"app"`
		File     string `json:"file"`
		Inserted int    `json:"inserted"`
		Skipped  int    `json:"skipped"`
	}
	var results []seedResult

	for _, ai := range appInfos {
		if appFilter != "" && ai.Name != appFilter {
			continue
		}

		fixturesDir := filepath.Join(ai.Path, "fixtures")
		entries, readErr := os.ReadDir(fixturesDir)
		if readErr != nil {
			continue // no fixtures directory
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}

			if fileFilter != "" && entry.Name() != fileFilter {
				continue
			}

			fixPath := filepath.Join(fixturesDir, entry.Name())
			data, readErr := os.ReadFile(fixPath)
			if readErr != nil {
				s.Stop("Failed")
				return output.NewCLIError(fmt.Sprintf("Cannot read fixture file: %s", fixPath)).
					WithErr(readErr)
			}

			var docs []map[string]any
			if jsonErr := json.Unmarshal(data, &docs); jsonErr != nil {
				s.Stop("Failed")
				return output.NewCLIError(fmt.Sprintf("Invalid JSON in fixture file: %s", entry.Name())).
					WithErr(jsonErr)
			}

			inserted, skipped := 0, 0
			for _, doc := range docs {
				doctype, ok := doc["doctype"].(string)
				if !ok || doctype == "" {
					s.Stop("Failed")
					return output.NewCLIError(fmt.Sprintf("Document in %s missing 'doctype' field", entry.Name()))
				}

				tableName := "tab_" + doctypeToSnake(doctype)
				delete(doc, "doctype")

				if len(doc) == 0 {
					skipped++
					continue
				}

				insertErr := insertSeedDoc(cmd.Context(), pool, tableName, doc, force)
				if insertErr != nil {
					w.Debugf("seed insert error for %s: %v", tableName, insertErr)
					skipped++
				} else {
					inserted++
				}
			}

			results = append(results, seedResult{
				App: ai.Name, File: entry.Name(), Inserted: inserted, Skipped: skipped,
			})
		}
	}

	s.Stop("Seed complete")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"site": siteName, "results": results})
	}

	if len(results) == 0 {
		w.PrintInfo("No fixture files found.")
		return nil
	}

	for _, r := range results {
		w.Print("  %s %s/%s: %d inserted, %d skipped",
			w.Color().Success("✓"), r.App, r.File, r.Inserted, r.Skipped)
	}
	return nil
}

func insertSeedDoc(ctx context.Context, pool *pgxpool.Pool, tableName string, doc map[string]any, force bool) error {
	cols := make([]string, 0, len(doc))
	placeholders := make([]string, 0, len(doc))
	vals := make([]any, 0, len(doc))
	i := 1

	for k, v := range doc {
		cols = append(cols, pgx.Identifier{k}.Sanitize())
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		vals = append(vals, v)
		i++
	}

	var onConflict string
	if force {
		// Build SET clause for upsert.
		var sets []string
		for _, col := range cols {
			sets = append(sets, fmt.Sprintf("%s = EXCLUDED.%s", col, col))
		}
		onConflict = fmt.Sprintf("ON CONFLICT (name) DO UPDATE SET %s", strings.Join(sets, ", "))
	} else {
		onConflict = "ON CONFLICT DO NOTHING"
	}

	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) %s",
		pgx.Identifier{tableName}.Sanitize(),
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
		onConflict,
	)

	_, err := pool.Exec(ctx, sql, vals...)
	return err
}

// ---------------------------------------------------------------------------
// db reset
// ---------------------------------------------------------------------------

func newDBResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Drop and recreate site schema",
		Long: `Drop and recreate the site's database schema. This is DESTRUCTIVE
and will delete ALL data. Creates a backup first unless --no-backup is specified.`,
		RunE: runDBReset,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")
	f.Bool("force", false, "Skip confirmation prompt")
	f.Bool("no-backup", false, "Skip automatic backup before reset")

	return cmd
}

func runDBReset(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return err
	}

	force, _ := cmd.Flags().GetBool("force")
	noBackup, _ := cmd.Flags().GetBool("no-backup")

	if !force {
		confirmed, promptErr := confirmPrompt(
			fmt.Sprintf("This will DROP and recreate ALL data for site '%s'. Continue?", siteName),
		)
		if promptErr != nil {
			return promptErr
		}
		if !confirmed {
			w.PrintWarning("Reset cancelled.")
			return nil
		}
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	// Get site info before dropping (to preserve config for recreation).
	siteInfo, infoErr := svc.Sites.GetSiteInfo(cmd.Context(), siteName)
	if infoErr != nil {
		return output.NewCLIError("Site not found").
			WithErr(infoErr).
			WithContext("site: " + siteName)
	}

	// Create pre-reset backup unless --no-backup.
	var backupPath string
	if !noBackup {
		depErr := backup.CheckDependencies()
		if depErr != nil {
			w.PrintWarning("pg_dump not available — skipping pre-reset backup.")
		} else {
			s := w.NewSpinner("Creating pre-reset backup...")
			s.Start()
			info, bkErr := backup.Create(cmd.Context(), backup.CreateOptions{
				Site:        siteName,
				ProjectRoot: ctx.ProjectRoot,
				Compress:    true,
				DBConfig:    dbConnConfig(ctx.Project.Infrastructure.Database),
			})
			if bkErr != nil {
				s.Stop("Backup failed")
				w.PrintWarning(fmt.Sprintf("Pre-reset backup failed: %v. Continuing without backup.", bkErr))
			} else {
				s.Stop("Backup created")
				backupPath = info.Path
			}
		}
	}

	s := w.NewSpinner("Resetting site...")
	s.Start()

	// Drop site.
	if dropErr := svc.Sites.DropSite(cmd.Context(), siteName, tenancy.SiteDropOptions{Force: true}); dropErr != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to drop site").
			WithErr(dropErr).
			WithContext("site: " + siteName)
	}

	// Recreate site with preserved config.
	if createErr := svc.Sites.CreateSite(cmd.Context(), tenancy.SiteCreateConfig{
		Name:          siteName,
		AdminEmail:    siteInfo.AdminEmail,
		AdminPassword: "admin", // Default password after reset.
		Plan:          siteInfo.Plan,
		Config:        siteInfo.Config,
	}); createErr != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to recreate site").
			WithErr(createErr).
			WithCause(createErr.Error()).
			WithFix("The site was dropped but could not be recreated. Check PostgreSQL connectivity.")
	}

	s.Stop("Reset complete")

	if w.Mode() == output.ModeJSON {
		result := map[string]any{
			"site":   siteName,
			"status": "reset",
		}
		if backupPath != "" {
			result["backup"] = backupPath
		}
		return w.PrintJSON(result)
	}

	w.PrintSuccess(fmt.Sprintf("Site '%s' has been reset", siteName))
	if backupPath != "" {
		w.Print("  Pre-reset backup: %s", backupPath)
	}
	w.Print("  Admin password has been reset to 'admin'")
	return nil
}

// ---------------------------------------------------------------------------
// db snapshot
// ---------------------------------------------------------------------------

func newDBSnapshotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Save current schema as snapshot",
		Long: `Save the current database schema state as a timestamped snapshot.
Produces a .sql file in sites/{site}/snapshots/.`,
		RunE: runDBSnapshot,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")
	f.Bool("include-data", false, "Include row data (not just schema)")

	return cmd
}

func runDBSnapshot(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	if depErr := backup.CheckDependencies(); depErr != nil {
		return output.NewCLIError("Missing PostgreSQL tools").
			WithCause(depErr.Error()).
			WithFix("Install pg_dump, then retry.")
	}

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return err
	}

	includeData, _ := cmd.Flags().GetBool("include-data")
	dbCfg := ctx.Project.Infrastructure.Database
	schemaName := tenancy.SchemaNameForSite(siteName)

	snapshotDir := filepath.Join(ctx.ProjectRoot, "sites", siteName, "snapshots")
	if mkErr := os.MkdirAll(snapshotDir, 0o755); mkErr != nil {
		return output.NewCLIError("Cannot create snapshots directory").WithErr(mkErr)
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s.sql", timestamp)
	filePath := filepath.Join(snapshotDir, filename)

	args := []string{
		"--schema=" + schemaName,
		"-h", dbCfg.Host,
		"-p", strconv.Itoa(dbCfg.Port),
		"-U", dbCfg.User,
		"-f", filePath,
		dbCfg.SystemDB,
	}

	if !includeData {
		args = append([]string{"--schema-only"}, args...)
	} else {
		args = append([]string{"--inserts"}, args...)
	}

	s := w.NewSpinner("Creating snapshot...")
	s.Start()

	pgDumpPath, _ := exec.LookPath("pg_dump")
	pgCmd := exec.CommandContext(cmd.Context(), pgDumpPath, args...)
	pgCmd.Env = append(os.Environ(), "PGPASSWORD="+dbCfg.Password)

	if out, runErr := pgCmd.CombinedOutput(); runErr != nil {
		s.Stop("Failed")
		_ = os.Remove(filePath)
		return output.NewCLIError("pg_dump failed").
			WithErr(runErr).
			WithCause(string(out))
	}

	fi, _ := os.Stat(filePath)
	s.Stop("Snapshot created")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"site": siteName,
			"path": filePath,
			"size": fi.Size(),
		})
	}

	w.PrintSuccess(fmt.Sprintf("Snapshot saved: %s", filePath))
	if fi != nil {
		w.Print("  Size: %s", formatBytes(fi.Size()))
	}
	return nil
}

// ---------------------------------------------------------------------------
// db export-fixtures
// ---------------------------------------------------------------------------

func newDBExportFixturesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export-fixtures",
		Short: "Export data as fixture files",
		Long: `Export site data as JSON fixture files to an app's fixtures/ directory.
Each DocType is exported as a separate JSON file.`,
		RunE: runDBExportFixtures,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")
	f.String("app", "", "Target app to save fixtures into (required)")
	f.String("doctype", "", "Export a specific DocType only")
	f.String("filters", "", "JSON filter object to limit exported records")

	return cmd
}

func runDBExportFixtures(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return err
	}

	appName, _ := cmd.Flags().GetString("app")
	if appName == "" {
		return output.NewCLIError("--app flag is required").
			WithFix("Specify an app to export fixtures into: moca db export-fixtures --app core")
	}

	doctypeFilter, _ := cmd.Flags().GetString("doctype")
	filtersJSON, _ := cmd.Flags().GetString("filters")

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	pool, poolErr := svc.DB.ForSite(cmd.Context(), siteName)
	if poolErr != nil {
		return output.NewCLIError("Cannot connect to site database").
			WithErr(poolErr).
			WithContext("site: " + siteName)
	}

	// Determine which doctypes to export.
	var doctypes []string
	if doctypeFilter != "" {
		doctypes = []string{doctypeFilter}
	} else {
		rows, qErr := pool.Query(cmd.Context(), `SELECT name FROM tab_doctype ORDER BY name`)
		if qErr != nil {
			return output.NewCLIError("Failed to query registered MetaTypes").WithErr(qErr)
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if scanErr := rows.Scan(&name); scanErr != nil {
				return output.NewCLIError("Failed to scan doctype name").WithErr(scanErr)
			}
			doctypes = append(doctypes, name)
		}
		if rows.Err() != nil {
			return output.NewCLIError("Failed to iterate doctypes").WithErr(rows.Err())
		}
	}

	// Ensure fixtures directory exists.
	fixturesDir := filepath.Join(ctx.ProjectRoot, "apps", appName, "fixtures")
	if mkErr := os.MkdirAll(fixturesDir, 0o755); mkErr != nil {
		return output.NewCLIError("Cannot create fixtures directory").WithErr(mkErr)
	}

	s := w.NewSpinner("Exporting fixtures...")
	s.Start()

	type exportResult struct {
		DocType string `json:"doctype"`
		Path    string `json:"path"`
		Count   int    `json:"count"`
	}
	var results []exportResult

	for _, dt := range doctypes {
		tableName := "tab_" + doctypeToSnake(dt)
		query := fmt.Sprintf("SELECT row_to_json(t) FROM %s t", pgx.Identifier{tableName}.Sanitize())

		// Apply filters if provided.
		if filtersJSON != "" {
			var filters map[string]any
			if jsonErr := json.Unmarshal([]byte(filtersJSON), &filters); jsonErr != nil {
				s.Stop("Failed")
				return output.NewCLIError("Invalid --filters JSON").WithErr(jsonErr)
			}
			var conditions []string
			for k, v := range filters {
				conditions = append(conditions, fmt.Sprintf("%s = '%v'", pgx.Identifier{k}.Sanitize(), v))
			}
			if len(conditions) > 0 {
				query += " WHERE " + strings.Join(conditions, " AND ")
			}
		}

		rows, qErr := pool.Query(cmd.Context(), query)
		if qErr != nil {
			w.Debugf("skip %s: %v", dt, qErr)
			continue
		}

		var docs []map[string]any
		for rows.Next() {
			var rowJSON []byte
			if scanErr := rows.Scan(&rowJSON); scanErr != nil {
				rows.Close()
				continue
			}
			var doc map[string]any
			if jsonErr := json.Unmarshal(rowJSON, &doc); jsonErr != nil {
				rows.Close()
				continue
			}
			doc["doctype"] = dt
			docs = append(docs, doc)
		}
		rows.Close()

		if len(docs) == 0 {
			continue
		}

		fixturePath := filepath.Join(fixturesDir, doctypeToSnake(dt)+".json")
		data, _ := json.MarshalIndent(docs, "", "  ")
		if writeErr := os.WriteFile(fixturePath, data, 0o644); writeErr != nil {
			s.Stop("Failed")
			return output.NewCLIError(fmt.Sprintf("Cannot write fixture file: %s", fixturePath)).WithErr(writeErr)
		}

		results = append(results, exportResult{DocType: dt, Count: len(docs), Path: fixturePath})
	}

	s.Stop("Export complete")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"site": siteName, "app": appName, "results": results})
	}

	if len(results) == 0 {
		w.PrintInfo("No data exported.")
		return nil
	}

	for _, r := range results {
		w.Print("  %s %s: %d record(s) → %s", w.Color().Success("✓"), r.DocType, r.Count, r.Path)
	}
	w.PrintSuccess(fmt.Sprintf("Exported %d DocType(s)", len(results)))
	return nil
}

// ---------------------------------------------------------------------------
// db trim-tables
// ---------------------------------------------------------------------------

func newDBTrimTablesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trim-tables",
		Short: "Remove orphaned columns",
		Long: `Compare database columns against MetaType definitions and identify
orphaned columns that no longer exist in any definition.
Defaults to dry-run mode — use --execute to actually drop columns.`,
		RunE: runDBTrimTables,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")
	f.Bool("dry-run", true, "Show what would be removed (default: true)")
	f.Bool("execute", false, "Actually drop orphaned columns")
	f.String("doctype", "", "Target a specific DocType")

	return cmd
}

func runDBTrimTables(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return err
	}

	execute, _ := cmd.Flags().GetBool("execute")
	doctypeFilter, _ := cmd.Flags().GetString("doctype")

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	pool, poolErr := svc.DB.ForSite(cmd.Context(), siteName)
	if poolErr != nil {
		return output.NewCLIError("Cannot connect to site database").
			WithErr(poolErr).
			WithContext("site: " + siteName)
	}

	schemaName := tenancy.SchemaNameForSite(siteName)

	// Get list of registered doctypes.
	var doctypes []string
	if doctypeFilter != "" {
		doctypes = []string{doctypeFilter}
	} else {
		rows, qErr := pool.Query(cmd.Context(), `SELECT name FROM tab_doctype ORDER BY name`)
		if qErr != nil {
			return output.NewCLIError("Failed to query registered MetaTypes").WithErr(qErr)
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if scanErr := rows.Scan(&name); scanErr != nil {
				return output.NewCLIError("Failed to scan doctype name").WithErr(scanErr)
			}
			doctypes = append(doctypes, name)
		}
		if rows.Err() != nil {
			return output.NewCLIError("Failed to iterate doctypes").WithErr(rows.Err())
		}
	}

	type orphanedColumn struct {
		DocType string `json:"doctype"`
		Table   string `json:"table"`
		Column  string `json:"column"`
	}
	var orphaned []orphanedColumn
	var dropped int

	for _, dt := range doctypes {
		mt, getErr := svc.Registry.Get(cmd.Context(), siteName, dt)
		if getErr != nil {
			w.Debugf("skip %s: %v", dt, getErr)
			continue
		}

		tableName := "tab_" + doctypeToSnake(dt)

		// Build expected column set.
		expected := make(map[string]struct{})
		var stdCols []meta.StandardColumnDef
		if mt.IsChildTable {
			stdCols = meta.ChildStandardColumns()
		} else {
			stdCols = meta.StandardColumns()
		}
		for _, c := range stdCols {
			expected[c.Name] = struct{}{}
		}
		for _, f := range mt.Fields {
			if meta.ColumnType(f.FieldType) != "" {
				expected[f.Name] = struct{}{}
			}
		}

		// Query actual columns from information_schema.
		rows, qErr := pool.Query(cmd.Context(),
			`SELECT column_name FROM information_schema.columns
			 WHERE table_schema = $1 AND table_name = $2
			 ORDER BY ordinal_position`, schemaName, tableName)
		if qErr != nil {
			w.Debugf("skip %s: cannot query columns: %v", dt, qErr)
			continue
		}

		var actualCols []string
		for rows.Next() {
			var col string
			if scanErr := rows.Scan(&col); scanErr != nil {
				rows.Close()
				continue
			}
			actualCols = append(actualCols, col)
		}
		rows.Close()

		for _, col := range actualCols {
			if _, ok := expected[col]; !ok {
				orphaned = append(orphaned, orphanedColumn{DocType: dt, Table: tableName, Column: col})

				if execute {
					dropSQL := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s",
						pgx.Identifier{tableName}.Sanitize(),
						pgx.Identifier{col}.Sanitize())
					if _, execErr := pool.Exec(cmd.Context(), dropSQL); execErr != nil {
						w.PrintWarning(fmt.Sprintf("Failed to drop %s.%s: %v", tableName, col, execErr))
					} else {
						dropped++
					}
				}
			}
		}
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"site":     siteName,
			"orphaned": orphaned,
			"dropped":  dropped,
			"dry_run":  !execute,
		})
	}

	if len(orphaned) == 0 {
		w.PrintSuccess("No orphaned columns found.")
		return nil
	}

	headers := []string{"DOCTYPE", "TABLE", "COLUMN", "ACTION"}
	var rows [][]string
	for _, o := range orphaned {
		action := "would drop"
		if execute {
			action = "dropped"
		}
		rows = append(rows, []string{o.DocType, o.Table, o.Column, action})
	}
	if printErr := w.PrintTable(headers, rows); printErr != nil {
		return printErr
	}

	if !execute {
		w.Print("")
		w.PrintWarning(fmt.Sprintf("Found %d orphaned column(s). Run with --execute to drop them.", len(orphaned)))
	} else {
		w.PrintSuccess(fmt.Sprintf("Dropped %d orphaned column(s)", dropped))
	}
	return nil
}

// ---------------------------------------------------------------------------
// db trim-database
// ---------------------------------------------------------------------------

func newDBTrimDatabaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trim-database",
		Short: "Remove orphaned tables",
		Long: `Compare database tables against MetaType definitions and identify
orphaned tables that don't correspond to any registered MetaType.
Defaults to dry-run mode — use --execute to actually drop tables.`,
		RunE: runDBTrimDatabase,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")
	f.Bool("dry-run", true, "Show what would be removed (default: true)")
	f.Bool("execute", false, "Actually drop orphaned tables")

	return cmd
}

// systemTables are internal tables that should never be flagged as orphaned.
var systemTables = map[string]struct{}{
	"tab_doctype":     {},
	"moca_migrations": {},
}

func runDBTrimDatabase(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return err
	}

	execute, _ := cmd.Flags().GetBool("execute")

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	pool, poolErr := svc.DB.ForSite(cmd.Context(), siteName)
	if poolErr != nil {
		return output.NewCLIError("Cannot connect to site database").
			WithErr(poolErr).
			WithContext("site: " + siteName)
	}

	schemaName := tenancy.SchemaNameForSite(siteName)

	// Get all tab_* tables in the schema.
	rows, qErr := pool.Query(cmd.Context(),
		`SELECT table_name FROM information_schema.tables
		 WHERE table_schema = $1 AND table_name LIKE 'tab_%'
		 ORDER BY table_name`, schemaName)
	if qErr != nil {
		return output.NewCLIError("Failed to query tables").WithErr(qErr)
	}
	defer rows.Close()

	var actualTables []string
	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr != nil {
			return output.NewCLIError("Failed to scan table name").WithErr(scanErr)
		}
		actualTables = append(actualTables, name)
	}
	if rows.Err() != nil {
		return output.NewCLIError("Failed to iterate tables").WithErr(rows.Err())
	}

	// Get all registered MetaType names.
	dtRows, dtErr := pool.Query(cmd.Context(), `SELECT name FROM tab_doctype ORDER BY name`)
	if dtErr != nil {
		return output.NewCLIError("Failed to query registered MetaTypes").WithErr(dtErr)
	}
	defer dtRows.Close()

	expectedTables := make(map[string]struct{})
	for dtRows.Next() {
		var name string
		if scanErr := dtRows.Scan(&name); scanErr != nil {
			return output.NewCLIError("Failed to scan doctype name").WithErr(scanErr)
		}
		expectedTables["tab_"+doctypeToSnake(name)] = struct{}{}
	}
	if dtRows.Err() != nil {
		return output.NewCLIError("Failed to iterate doctypes").WithErr(dtRows.Err())
	}

	// Add system tables to expected set.
	for t := range systemTables {
		expectedTables[t] = struct{}{}
	}

	type orphanedTable struct {
		Table string `json:"table"`
	}
	var orphaned []orphanedTable
	var dropped int

	for _, t := range actualTables {
		if _, ok := expectedTables[t]; !ok {
			orphaned = append(orphaned, orphanedTable{Table: t})

			if execute {
				dropSQL := fmt.Sprintf("DROP TABLE %s CASCADE",
					pgx.Identifier{tableName(t)}.Sanitize())
				if _, execErr := pool.Exec(cmd.Context(), dropSQL); execErr != nil {
					w.PrintWarning(fmt.Sprintf("Failed to drop %s: %v", t, execErr))
				} else {
					dropped++
				}
			}
		}
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"site":     siteName,
			"orphaned": orphaned,
			"dropped":  dropped,
			"dry_run":  !execute,
		})
	}

	if len(orphaned) == 0 {
		w.PrintSuccess("No orphaned tables found.")
		return nil
	}

	headers := []string{"TABLE", "ACTION"}
	var tableRows [][]string
	for _, o := range orphaned {
		action := "would drop"
		if execute {
			action = "dropped"
		}
		tableRows = append(tableRows, []string{o.Table, action})
	}
	if printErr := w.PrintTable(headers, tableRows); printErr != nil {
		return printErr
	}

	if !execute {
		w.Print("")
		w.PrintWarning(fmt.Sprintf("Found %d orphaned table(s). Run with --execute to drop them.", len(orphaned)))
	} else {
		w.PrintSuccess(fmt.Sprintf("Dropped %d orphaned table(s)", dropped))
	}
	return nil
}

// tableName is an identity function used in trim-database for clarity.
func tableName(t string) string { return t }

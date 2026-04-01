package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/moca-framework/moca/internal/output"
	"github.com/moca-framework/moca/pkg/apps"
	"github.com/moca-framework/moca/pkg/meta"
	"github.com/moca-framework/moca/pkg/orm"
	"github.com/spf13/cobra"
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
		// Remaining commands stay as placeholders (MS-11).
		newSubcommand("console", "Open interactive psql session"),
		newSubcommand("snapshot", "Save current schema as snapshot"),
		newSubcommand("seed", "Load seed/fixture data"),
		newSubcommand("trim-tables", "Remove orphaned columns"),
		newSubcommand("trim-database", "Remove orphaned tables"),
		newSubcommand("export-fixtures", "Export data as fixture files"),
		newSubcommand("reset", "Drop and recreate site schema"),
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

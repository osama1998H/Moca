package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/pkg/i18n"
)

// NewTranslateCommand returns the "moca translate" command group with all subcommands.
func NewTranslateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "translate",
		Short: "Translation management",
		Long:  "Export, import, and compile translations for internationalization.",
	}

	cmd.AddCommand(
		newTranslateExportCmd(),
		newTranslateImportCmd(),
		newTranslateStatusCmd(),
		newTranslateCompileCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// translate export
// ---------------------------------------------------------------------------

func newTranslateExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export translatable strings",
		Long:  "Extract all translatable strings from installed apps and write them to a file.",
		RunE:  runTranslateExport,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")
	f.String("app", "", "Export from specific app only")
	f.String("format", "po", `Output format: "po", "csv", or "json"`)
	f.String("output", "", "Output file path (default: stdout)")

	return cmd
}

func runTranslateExport(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, cliCtx)
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), cliCtx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	format, _ := cmd.Flags().GetString("format")
	outputPath, _ := cmd.Flags().GetString("output")
	appFilter, _ := cmd.Flags().GetString("app")

	// Load all MetaTypes from the site.
	mts, err := svc.Registry.ListAll(cmd.Context(), siteName)
	if err != nil {
		return output.NewCLIError("Failed to load MetaTypes").WithErr(err)
	}

	extractor := &i18n.Extractor{}
	extracted := extractor.ExtractFromMetaTypes(mts)

	// Convert to Translation entries (source text only, no translated text yet).
	translations := make([]i18n.Translation, 0, len(extracted))
	for _, s := range extracted {
		translations = append(translations, i18n.Translation{
			SourceText: s.Source,
			Context:    s.Context,
			App:        appFilter,
		})
	}

	// Write output.
	var buf bytes.Buffer
	switch format {
	case "po":
		err = i18n.ExportPO(translations, &buf)
	case "csv":
		err = i18n.ExportCSV(translations, &buf)
	case "json":
		err = i18n.ExportJSON(translations, &buf)
	default:
		return output.NewCLIError(fmt.Sprintf("Unknown format %q", format)).
			WithFix(`Use "po", "csv", or "json".`)
	}
	if err != nil {
		return output.NewCLIError("Export failed").WithErr(err)
	}

	if outputPath != "" {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return output.NewCLIError("Cannot create output directory").WithErr(err)
		}
		if err := os.WriteFile(outputPath, buf.Bytes(), 0o644); err != nil {
			return output.NewCLIError("Cannot write output file").WithErr(err)
		}
		w.PrintSuccess(fmt.Sprintf("Exported %d strings to %s", len(translations), outputPath))
	} else {
		_, _ = fmt.Fprint(cmd.OutOrStdout(), buf.String())
	}

	return nil
}

// ---------------------------------------------------------------------------
// translate import
// ---------------------------------------------------------------------------

func newTranslateImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import FILE",
		Short: "Import translations",
		Long:  "Import translations from a PO, CSV, or JSON file into the database.",
		Args:  cobra.ExactArgs(1),
		RunE:  runTranslateImport,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")
	f.String("language", "", "Target language code (e.g., ar, fr, de)")
	f.String("app", "", "Target app name")
	f.Bool("overwrite", false, "Overwrite existing translations")

	_ = cmd.MarkFlagRequired("language")

	return cmd
}

func runTranslateImport(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, cliCtx)
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), cliCtx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	filePath := args[0]
	lang, _ := cmd.Flags().GetString("language")
	appName, _ := cmd.Flags().GetString("app")
	overwrite, _ := cmd.Flags().GetBool("overwrite")

	// Read and parse file.
	f, err := os.Open(filePath)
	if err != nil {
		return output.NewCLIError("Cannot open file").WithErr(err)
	}
	defer func() { _ = f.Close() }()

	var translations []i18n.Translation
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".po":
		translations, err = i18n.ImportPO(f)
	case ".csv":
		translations, err = i18n.ImportCSV(f)
	case ".json":
		translations, err = i18n.ImportJSON(f)
	default:
		return output.NewCLIError(fmt.Sprintf("Unknown file extension %q", ext)).
			WithFix(`Use a .po, .csv, or .json file.`)
	}
	if err != nil {
		return output.NewCLIError("Import parse failed").WithErr(err)
	}

	// Get tenant pool.
	pool, err := svc.DB.ForSite(cmd.Context(), siteName)
	if err != nil {
		return output.NewCLIError("Cannot connect to site database").WithErr(err)
	}

	// Upsert translations.
	imported := 0
	for _, t := range translations {
		t.Language = lang
		if appName != "" {
			t.App = appName
		}
		if t.SourceText == "" {
			continue
		}

		var query string
		if overwrite {
			query = `INSERT INTO tab_translation (source_text, language, translated_text, context, app)
				VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (source_text, language, context) DO UPDATE SET translated_text = EXCLUDED.translated_text, app = EXCLUDED.app`
		} else {
			query = `INSERT INTO tab_translation (source_text, language, translated_text, context, app)
				VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (source_text, language, context) DO NOTHING`
		}

		_, err := pool.Exec(cmd.Context(), query, t.SourceText, t.Language, t.TranslatedText, t.Context, t.App)
		if err != nil {
			return output.NewCLIError("Database insert failed").WithErr(err).
				WithContext(fmt.Sprintf("source: %q", t.SourceText))
		}
		imported++
	}

	// Invalidate Redis cache.
	translator := i18n.NewTranslator(svc.Redis.Cache, svc.DB.ForSite, svc.Logger)
	if err := translator.Invalidate(cmd.Context(), siteName, lang); err != nil {
		w.PrintWarning(fmt.Sprintf("Cache invalidation failed: %v", err))
	}

	w.PrintSuccess(fmt.Sprintf("Imported %d translations for language %q into site %q", imported, lang, siteName))
	return nil
}

// ---------------------------------------------------------------------------
// translate status
// ---------------------------------------------------------------------------

func newTranslateStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show translation coverage",
		Long:  "Display translation coverage statistics for installed apps.",
		RunE:  runTranslateStatus,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")
	f.String("app", "", "Filter by app")
	f.String("language", "", "Filter by language")
	f.Bool("json", false, "Output as JSON")

	return cmd
}

type translationStats struct {
	App        string `json:"app"`
	Language   string `json:"language"`
	Coverage   string `json:"coverage"`
	Translated int    `json:"translated"`
	Total      int    `json:"total"`
}

func runTranslateStatus(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, cliCtx)
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), cliCtx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	appFilter, _ := cmd.Flags().GetString("app")
	langFilter, _ := cmd.Flags().GetString("language")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	pool, err := svc.DB.ForSite(cmd.Context(), siteName)
	if err != nil {
		return output.NewCLIError("Cannot connect to site database").WithErr(err)
	}

	// Build query with optional filters.
	query := `SELECT COALESCE(app, '(none)') as app, language, COUNT(*) as total,
		COUNT(CASE WHEN translated_text != '' THEN 1 END) as translated
		FROM tab_translation`
	var conditions []string
	var queryArgs []interface{}
	argIdx := 1

	if appFilter != "" {
		conditions = append(conditions, fmt.Sprintf("app = $%d", argIdx))
		queryArgs = append(queryArgs, appFilter)
		argIdx++
	}
	if langFilter != "" {
		conditions = append(conditions, fmt.Sprintf("language = $%d", argIdx))
		queryArgs = append(queryArgs, langFilter)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " GROUP BY app, language ORDER BY app, language"

	rows, err := pool.Query(cmd.Context(), query, queryArgs...)
	if err != nil {
		return output.NewCLIError("Failed to query translation stats").WithErr(err)
	}
	defer rows.Close()

	var stats []translationStats
	for rows.Next() {
		var s translationStats
		if err := rows.Scan(&s.App, &s.Language, &s.Total, &s.Translated); err != nil {
			return output.NewCLIError("Failed to scan stats row").WithErr(err)
		}
		if s.Total > 0 {
			s.Coverage = fmt.Sprintf("%.1f%%", float64(s.Translated)/float64(s.Total)*100)
		} else {
			s.Coverage = "0.0%"
		}
		stats = append(stats, s)
	}
	if err := rows.Err(); err != nil {
		return output.NewCLIError("Failed to iterate stats rows").WithErr(err)
	}

	if jsonOutput {
		_ = w.PrintJSON(stats)
		return nil
	}

	if len(stats) == 0 {
		w.PrintInfo("No translations found.")
		return nil
	}

	tbl := output.NewTable([]string{"APP", "LANGUAGE", "TRANSLATED", "TOTAL", "COVERAGE"}, nil)
	for _, s := range stats {
		tbl.AddRow(
			s.App,
			s.Language,
			fmt.Sprintf("%d", s.Translated),
			fmt.Sprintf("%d", s.Total),
			s.Coverage,
		)
	}
	return tbl.Render(cmd.OutOrStdout())
}

// ---------------------------------------------------------------------------
// translate compile
// ---------------------------------------------------------------------------

func newTranslateCompileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compile",
		Short: "Compile translations to binary format",
		Long:  "Compile PO translation files to optimized binary MO format for production use.",
		RunE:  runTranslateCompile,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")
	f.String("language", "", "Compile a specific language only")
	f.String("app", "", "Compile for a specific app only")

	return cmd
}

func runTranslateCompile(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, cliCtx)
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), cliCtx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	langFilter, _ := cmd.Flags().GetString("language")
	appFilter, _ := cmd.Flags().GetString("app")

	pool, err := svc.DB.ForSite(cmd.Context(), siteName)
	if err != nil {
		return output.NewCLIError("Cannot connect to site database").WithErr(err)
	}

	// Get distinct languages to compile.
	languages, err := getTranslationLanguages(cmd.Context(), pool, langFilter)
	if err != nil {
		return output.NewCLIError("Failed to query languages").WithErr(err)
	}

	if len(languages) == 0 {
		w.PrintInfo("No translations found to compile.")
		return nil
	}

	// Create output directory.
	outputDir := filepath.Join(cliCtx.ProjectRoot, "sites", siteName, "translations")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return output.NewCLIError("Cannot create output directory").WithErr(err)
	}

	translator := i18n.NewTranslator(svc.Redis.Cache, svc.DB.ForSite, svc.Logger)

	for _, lang := range languages {
		// Query translations for this language.
		query := `SELECT source_text, language, translated_text, context, COALESCE(app, '') FROM tab_translation WHERE language = $1`
		var queryArgs []interface{}
		queryArgs = append(queryArgs, lang)
		if appFilter != "" {
			query += " AND app = $2"
			queryArgs = append(queryArgs, appFilter)
		}

		rows, err := pool.Query(cmd.Context(), query, queryArgs...)
		if err != nil {
			return output.NewCLIError("Failed to query translations").WithErr(err)
		}

		var translations []i18n.Translation
		for rows.Next() {
			var t i18n.Translation
			if scanErr := rows.Scan(&t.SourceText, &t.Language, &t.TranslatedText, &t.Context, &t.App); scanErr != nil {
				rows.Close()
				return output.NewCLIError("Failed to scan translation row").WithErr(scanErr)
			}
			translations = append(translations, t)
		}
		rows.Close()

		if len(translations) == 0 {
			continue
		}

		// Compile to MO.
		moPath := filepath.Join(outputDir, lang+".mo")
		moFile, createErr := os.Create(moPath)
		if createErr != nil {
			return output.NewCLIError("Cannot create MO file").WithErr(createErr)
		}

		if compileErr := i18n.CompileMO(translations, moFile); compileErr != nil {
			_ = moFile.Close()
			return output.NewCLIError("MO compilation failed").WithErr(compileErr)
		}
		_ = moFile.Close()

		// Invalidate Redis cache.
		if err := translator.Invalidate(cmd.Context(), siteName, lang); err != nil {
			w.PrintWarning(fmt.Sprintf("Cache invalidation for %q failed: %v", lang, err))
		}

		w.PrintSuccess(fmt.Sprintf("Compiled %d translations for %q → %s", len(translations), lang, moPath))
	}

	return nil
}

// getTranslationLanguages returns distinct language codes from tab_translation.
func getTranslationLanguages(ctx context.Context, pool *pgxpool.Pool, filter string) ([]string, error) {
	var query string
	var args []interface{}

	if filter != "" {
		query = `SELECT DISTINCT language FROM tab_translation WHERE language = $1 ORDER BY language`
		args = append(args, filter)
	} else {
		query = `SELECT DISTINCT language FROM tab_translation ORDER BY language`
	}

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var languages []string
	for rows.Next() {
		var lang string
		if err := rows.Scan(&lang); err != nil {
			return nil, err
		}
		languages = append(languages, lang)
	}
	return languages, rows.Err()
}

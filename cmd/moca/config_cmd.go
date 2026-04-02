package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/osama1998H/moca/internal/config"
	clicontext "github.com/osama1998H/moca/internal/context"
	"github.com/osama1998H/moca/internal/output"
)

// NewConfigCommand returns the "moca config" command group with all subcommands.
// File is named config_cmd.go to avoid collision with internal/config package name.
func NewConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configuration management",
		Long:  "Get, set, compare, and manage project and site configuration.",
	}

	cmd.AddCommand(
		newConfigGetCmd(),
		newConfigSetCmd(),
		newConfigRemoveCmd(),
		newConfigListCmd(),
		newConfigDiffCmd(),
		newConfigExportCmd(),
		newConfigImportCmd(),
		newConfigEditCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// config target helpers
// ---------------------------------------------------------------------------

// configTarget determines which YAML file to operate on.
type configTarget int

const (
	targetProject configTarget = iota
	targetCommon
	targetSite
)

// resolveConfigTarget reads --project, --common, --site flags and returns the
// target type and site name (if applicable).
func resolveConfigTarget(cmd *cobra.Command) (configTarget, string) {
	site, _ := cmd.Flags().GetString("site")
	common, _ := cmd.Flags().GetBool("common")

	if site != "" {
		return targetSite, site
	}
	if common {
		return targetCommon, ""
	}
	return targetProject, ""
}

// loadTargetConfig loads the config map for the resolved target.
func loadTargetConfig(target configTarget, projectRoot, site string) (map[string]any, error) {
	switch target {
	case targetSite:
		return config.LoadSiteConfig(projectRoot, site)
	case targetCommon:
		return config.LoadCommonSiteConfig(projectRoot)
	default:
		return config.LoadProjectConfigMap(projectRoot)
	}
}

// saveTargetConfig saves the config map to the resolved target file.
func saveTargetConfig(target configTarget, projectRoot, site string, data map[string]any) error {
	switch target {
	case targetSite:
		return config.SaveSiteConfig(projectRoot, site, data)
	case targetCommon:
		return config.SaveCommonSiteConfig(projectRoot, data)
	default:
		return config.SaveProjectConfigMap(projectRoot, data)
	}
}

// configFilePath returns the path to the config file for the given target.
func configFilePath(target configTarget, projectRoot, site string) string {
	switch target {
	case targetSite:
		return filepath.Join(projectRoot, "sites", site, "site_config.yaml")
	case targetCommon:
		return filepath.Join(projectRoot, "sites", "common_site_config.yaml")
	default:
		return filepath.Join(projectRoot, "moca.yaml")
	}
}

// resolveFullConfig loads and merges all 3 YAML layers for a site.
func resolveFullConfig(projectRoot, site string) (map[string]any, error) {
	projectMap, err := config.ConfigToMap(mustLoadProject(projectRoot))
	if err != nil {
		return nil, fmt.Errorf("convert project config: %w", err)
	}

	common, err := config.LoadCommonSiteConfig(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("load common site config: %w", err)
	}

	siteMap, err := config.LoadSiteConfig(projectRoot, site)
	if err != nil {
		return nil, fmt.Errorf("load site config: %w", err)
	}

	merged := config.MergeMaps(projectMap, common)
	merged = config.MergeMaps(merged, siteMap)
	return merged, nil
}

// mustLoadProject loads the project config from moca.yaml. Returns nil if not found.
func mustLoadProject(projectRoot string) *config.ProjectConfig {
	path := filepath.Join(projectRoot, "moca.yaml")
	cfg, _ := config.ParseFile(path)
	return cfg
}

// autoDetectType converts a string value to the appropriate Go type.
// Detection order: bool → int → JSON object/array → string.
func autoDetectType(value string) any {
	if value == "true" {
		return true
	}
	if value == "false" {
		return false
	}

	if n, err := strconv.Atoi(value); err == nil {
		return n
	}

	if (strings.HasPrefix(value, "{") && strings.HasSuffix(value, "}")) ||
		(strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]")) {
		var parsed any
		if err := json.Unmarshal([]byte(value), &parsed); err == nil {
			return parsed
		}
	}

	return value
}

// parseTypedValue converts a string value using the given type hint.
func parseTypedValue(value, typeHint string) (any, error) {
	switch typeHint {
	case "bool":
		return strconv.ParseBool(value)
	case "int":
		return strconv.Atoi(value)
	case "json":
		var parsed any
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		return parsed, nil
	case "string", "":
		return value, nil
	default:
		return nil, fmt.Errorf("unknown type %q (valid: string, int, bool, json)", typeHint)
	}
}

// secretPatterns lists substrings that identify sensitive config keys.
var secretPatterns = []string{"password", "secret_key", "access_key", "encryption_key", "api_key"}

// maskSecrets replaces sensitive values in a flattened config map.
func maskSecrets(flat map[string]any) map[string]any {
	result := make(map[string]any, len(flat))
	for k, v := range flat {
		lower := strings.ToLower(k)
		masked := false
		for _, pat := range secretPatterns {
			if strings.Contains(lower, pat) {
				result[k] = "***"
				masked = true
				break
			}
		}
		if !masked {
			result[k] = v
		}
	}
	return result
}

// sortedKeys returns the sorted keys of a map.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// unflattenMap converts a flat dot-notation map back to a nested map.
func unflattenMap(flat map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range flat {
		config.SetByPath(result, k, v)
	}
	return result
}

// ---------------------------------------------------------------------------
// config get
// ---------------------------------------------------------------------------

func newConfigGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get KEY",
		Short: "Get a config value",
		Long: `Get a config value by dot-notation key.

Config resolution order (highest priority first):
  1. Environment variable: MOCA_{KEY} (uppercased, dots → underscores)
  2. Site config: sites/{site}/site_config.yaml
  3. Common config: sites/common_site_config.yaml
  4. Project config: moca.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: runConfigGet,
	}

	f := cmd.Flags()
	f.String("site", "", "Get site-level config")
	f.Bool("resolved", false, "Show effective value after merging all layers")
	f.Bool("runtime", false, "Query live config from database")

	return cmd
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	key := args[0]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	runtime, _ := cmd.Flags().GetBool("runtime")
	resolved, _ := cmd.Flags().GetBool("resolved")

	var data map[string]any

	if runtime {
		siteName, siteErr := resolveSiteName(cmd, ctx)
		if siteErr != nil {
			return siteErr
		}

		verbose, _ := cmd.Flags().GetBool("verbose")
		svc, svcErr := newServices(cmd.Context(), ctx.Project, verbose)
		if svcErr != nil {
			return svcErr
		}
		defer svc.Close()

		data, err = config.LoadRuntimeConfig(cmd.Context(), siteName, svc.DB.SystemPool())
		if err != nil {
			return output.NewCLIError("Failed to load runtime config").
				WithErr(err).
				WithFix("Ensure the site exists and the database is accessible.")
		}
	} else if resolved {
		siteName, siteErr := resolveSiteName(cmd, ctx)
		if siteErr != nil {
			return siteErr
		}
		data, err = resolveFullConfig(ctx.ProjectRoot, siteName)
		if err != nil {
			return output.NewCLIError("Failed to resolve config").WithErr(err)
		}
	} else {
		site, _ := cmd.Flags().GetString("site")
		if site != "" {
			data, err = config.LoadSiteConfig(ctx.ProjectRoot, site)
		} else {
			data, err = config.ConfigToMap(ctx.Project)
		}
		if err != nil {
			return output.NewCLIError("Failed to load config").WithErr(err)
		}
	}

	// Check env var override: MOCA_{KEY} (uppercased, dots → underscores).
	envKey := "MOCA_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
	if envVal, ok := os.LookupEnv(envKey); ok {
		if w.Mode() == output.ModeJSON {
			return w.PrintJSON(map[string]any{"key": key, "value": envVal, "source": "env"})
		}
		w.Print("%v", envVal)
		return nil
	}

	val, ok := config.GetByPath(data, key)
	if !ok {
		if w.Mode() == output.ModeJSON {
			return w.PrintJSON(map[string]any{"key": key, "value": nil, "found": false})
		}
		return output.NewCLIError(fmt.Sprintf("Key %q not found", key)).
			WithFix("Use 'moca config list' to see available keys.")
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"key": key, "value": val})
	}

	w.Print("%v", val)
	return nil
}

// ---------------------------------------------------------------------------
// config set
// ---------------------------------------------------------------------------

func newConfigSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set KEY VALUE",
		Short: "Set a config value",
		Long: `Set a config value in the specified config file.

Value types are auto-detected: bool, int, JSON object/array, or string.
Use --type to override auto-detection.`,
		Args: cobra.ExactArgs(2),
		RunE: runConfigSet,
	}

	f := cmd.Flags()
	f.String("site", "", "Set in site config")
	f.Bool("common", false, "Set in common_site_config.yaml")
	f.String("type", "", `Value type hint: "string", "int", "bool", "json" (auto-detected)`)

	return cmd
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	key, rawValue := args[0], args[1]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	target, site := resolveConfigTarget(cmd)

	// Parse the value.
	typeHint, _ := cmd.Flags().GetString("type")
	var value any
	if typeHint != "" {
		value, err = parseTypedValue(rawValue, typeHint)
		if err != nil {
			return output.NewCLIError("Invalid value").
				WithErr(err).
				WithContext(fmt.Sprintf("key: %s, value: %s, type: %s", key, rawValue, typeHint))
		}
	} else {
		value = autoDetectType(rawValue)
	}

	// Load, modify, save.
	data, err := loadTargetConfig(target, ctx.ProjectRoot, site)
	if err != nil {
		return output.NewCLIError("Failed to load config").WithErr(err)
	}

	config.SetByPath(data, key, value)

	if err := saveTargetConfig(target, ctx.ProjectRoot, site, data); err != nil {
		return output.NewCLIError("Failed to save config").WithErr(err)
	}

	// Sync to DB if site-level change.
	if target == targetSite {
		resolved, resolveErr := resolveFullConfig(ctx.ProjectRoot, site)
		if resolveErr != nil {
			w.PrintWarning(fmt.Sprintf("Config saved to YAML but sync failed: %v", resolveErr))
		} else {
			syncErr := syncConfigToDB(cmd, ctx, site, resolved)
			if syncErr != nil {
				w.PrintWarning(fmt.Sprintf("Config saved to YAML but DB sync failed: %v", syncErr))
			}
		}
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"key":   key,
			"value": value,
			"file":  configFilePath(target, ctx.ProjectRoot, site),
		})
	}

	w.PrintSuccess(fmt.Sprintf("Set %s = %v", key, value))

	if target == targetProject {
		w.PrintInfo("Project config updated. Running sites need restart to pick up changes.")
	}

	return nil
}

// ---------------------------------------------------------------------------
// config remove
// ---------------------------------------------------------------------------

func newConfigRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove KEY",
		Short: "Remove a config key",
		Long:  "Remove a key from the specified config file.",
		Args:  cobra.ExactArgs(1),
		RunE:  runConfigRemove,
	}

	f := cmd.Flags()
	f.String("site", "", "Remove from site config")
	f.Bool("common", false, "Remove from common_site_config.yaml")

	return cmd
}

func runConfigRemove(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	key := args[0]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	target, site := resolveConfigTarget(cmd)

	data, err := loadTargetConfig(target, ctx.ProjectRoot, site)
	if err != nil {
		return output.NewCLIError("Failed to load config").WithErr(err)
	}

	if !config.RemoveByPath(data, key) {
		return output.NewCLIError(fmt.Sprintf("Key %q not found", key)).
			WithFix("Use 'moca config list' to see available keys.")
	}

	if err := saveTargetConfig(target, ctx.ProjectRoot, site, data); err != nil {
		return output.NewCLIError("Failed to save config").WithErr(err)
	}

	// Sync to DB if site-level change.
	if target == targetSite {
		resolved, resolveErr := resolveFullConfig(ctx.ProjectRoot, site)
		if resolveErr == nil {
			_ = syncConfigToDB(cmd, ctx, site, resolved)
		}
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"key": key, "removed": true})
	}

	w.PrintSuccess(fmt.Sprintf("Removed %s", key))
	return nil
}

// ---------------------------------------------------------------------------
// config list
// ---------------------------------------------------------------------------

func newConfigListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all effective config (merged)",
		Long:  "Display all resolved configuration keys and values.",
		RunE:  runConfigList,
	}

	f := cmd.Flags()
	f.String("site", "", "Show resolved config for a site")
	f.String("format", "table", `Output format: "table", "yaml", "json"`)
	f.String("filter", "", "Filter keys by glob pattern (e.g., \"database.*\")")

	return cmd
}

func runConfigList(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	var data map[string]any
	site, _ := cmd.Flags().GetString("site")
	if site != "" {
		data, err = resolveFullConfig(ctx.ProjectRoot, site)
	} else {
		data, err = config.ConfigToMap(ctx.Project)
	}
	if err != nil {
		return output.NewCLIError("Failed to load config").WithErr(err)
	}

	flat := config.FlattenMap(data, "")

	// Apply filter.
	filter, _ := cmd.Flags().GetString("filter")
	if filter != "" {
		filtered := make(map[string]any)
		for k, v := range flat {
			matched, _ := filepath.Match(filter, k)
			if matched {
				filtered[k] = v
			}
		}
		flat = filtered
	}

	format, _ := cmd.Flags().GetString("format")

	if w.Mode() == output.ModeJSON || format == "json" {
		return w.PrintJSON(flat)
	}

	if format == "yaml" {
		nested := unflattenMap(flat)
		raw, marshalErr := yaml.Marshal(nested)
		if marshalErr != nil {
			return output.NewCLIError("Failed to marshal YAML").WithErr(marshalErr)
		}
		w.Print("%s", string(raw))
		return nil
	}

	// Table format (default).
	keys := sortedKeys(flat)
	if len(keys) == 0 {
		w.PrintInfo("No configuration keys found.")
		return nil
	}

	headers := []string{"KEY", "VALUE"}
	var rows [][]string
	for _, k := range keys {
		rows = append(rows, []string{k, fmt.Sprintf("%v", flat[k])})
	}

	return w.PrintTable(headers, rows)
}

// ---------------------------------------------------------------------------
// config diff
// ---------------------------------------------------------------------------

func newConfigDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff SITE1 SITE2",
		Short: "Compare config between two sites",
		Long:  "Show key-by-key differences between two sites' resolved configurations.",
		Args:  cobra.ExactArgs(2),
		RunE:  runConfigDiff,
	}

	return cmd
}

func runConfigDiff(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	site1, site2 := args[0], args[1]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	cfg1, err := resolveFullConfig(ctx.ProjectRoot, site1)
	if err != nil {
		return output.NewCLIError(fmt.Sprintf("Failed to load config for %s", site1)).WithErr(err)
	}

	cfg2, err := resolveFullConfig(ctx.ProjectRoot, site2)
	if err != nil {
		return output.NewCLIError(fmt.Sprintf("Failed to load config for %s", site2)).WithErr(err)
	}

	flat1 := config.FlattenMap(cfg1, "")
	flat2 := config.FlattenMap(cfg2, "")

	// Build unified key set.
	allKeys := make(map[string]bool)
	for k := range flat1 {
		allKeys[k] = true
	}
	for k := range flat2 {
		allKeys[k] = true
	}

	keys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	type diffEntry struct {
		Key    string `json:"key"`
		Site1  any    `json:"site1"`
		Site2  any    `json:"site2"`
		Status string `json:"status"`
	}

	var diffs []diffEntry
	for _, k := range keys {
		v1, in1 := flat1[k]
		v2, in2 := flat2[k]

		var status string
		switch {
		case in1 && !in2:
			status = "removed"
		case !in1 && in2:
			status = "added"
		case fmt.Sprintf("%v", v1) != fmt.Sprintf("%v", v2):
			status = "modified"
		default:
			continue // equal — skip
		}
		diffs = append(diffs, diffEntry{Key: k, Site1: v1, Site2: v2, Status: status})
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(diffs)
	}

	if len(diffs) == 0 {
		w.PrintSuccess("Configurations are identical.")
		return nil
	}

	headers := []string{"KEY", site1, site2, "STATUS"}
	var rows [][]string
	for _, d := range diffs {
		rows = append(rows, []string{
			d.Key,
			fmt.Sprintf("%v", d.Site1),
			fmt.Sprintf("%v", d.Site2),
			d.Status,
		})
	}

	return w.PrintTable(headers, rows)
}

// ---------------------------------------------------------------------------
// config export
// ---------------------------------------------------------------------------

func newConfigExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export full config as YAML/JSON",
		Long:  "Export the full resolved configuration to a file or stdout.",
		RunE:  runConfigExport,
	}

	f := cmd.Flags()
	f.String("site", "", "Export for a specific site")
	f.String("format", "yaml", `Output format: "yaml", "json", "env"`)
	f.String("output", "", "Output file path (default: stdout)")
	f.Bool("secrets", false, "Include secret values (default: masked)")

	return cmd
}

func runConfigExport(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	var data map[string]any
	site, _ := cmd.Flags().GetString("site")
	if site != "" {
		data, err = resolveFullConfig(ctx.ProjectRoot, site)
	} else {
		data, err = config.ConfigToMap(ctx.Project)
	}
	if err != nil {
		return output.NewCLIError("Failed to load config").WithErr(err)
	}

	secrets, _ := cmd.Flags().GetBool("secrets")
	format, _ := cmd.Flags().GetString("format")

	var content []byte

	switch format {
	case "json":
		flat := config.FlattenMap(data, "")
		if !secrets {
			flat = maskSecrets(flat)
		}
		nested := unflattenMap(flat)
		content, err = json.MarshalIndent(nested, "", "  ")
		if err != nil {
			return output.NewCLIError("Failed to marshal JSON").WithErr(err)
		}
		content = append(content, '\n')

	case "env":
		flat := config.FlattenMap(data, "")
		if !secrets {
			flat = maskSecrets(flat)
		}
		keys := sortedKeys(flat)
		var sb strings.Builder
		for _, k := range keys {
			envKey := strings.ToUpper(strings.ReplaceAll(k, ".", "_"))
			fmt.Fprintf(&sb, "MOCA_%s=%v\n", envKey, flat[k])
		}
		content = []byte(sb.String())

	default: // yaml
		if !secrets {
			flat := config.FlattenMap(data, "")
			flat = maskSecrets(flat)
			data = unflattenMap(flat)
		}
		content, err = yaml.Marshal(data)
		if err != nil {
			return output.NewCLIError("Failed to marshal YAML").WithErr(err)
		}
	}

	outputPath, _ := cmd.Flags().GetString("output")
	if outputPath != "" {
		if writeErr := os.WriteFile(outputPath, content, 0o644); writeErr != nil {
			return output.NewCLIError("Failed to write file").WithErr(writeErr)
		}
		if w.Mode() == output.ModeJSON {
			return w.PrintJSON(map[string]any{"file": outputPath, "format": format})
		}
		w.PrintSuccess(fmt.Sprintf("Config exported to %s", outputPath))
		return nil
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"config": string(content), "format": format})
	}

	w.Print("%s", string(content))
	return nil
}

// ---------------------------------------------------------------------------
// config import
// ---------------------------------------------------------------------------

func newConfigImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import FILE",
		Short: "Import config from file",
		Long:  "Import configuration from a YAML/JSON file, merging with existing config.",
		Args:  cobra.ExactArgs(1),
		RunE:  runConfigImport,
	}

	f := cmd.Flags()
	f.String("site", "", "Import into site config")
	f.Bool("common", false, "Import into common_site_config.yaml")
	f.Bool("overwrite", false, "Overwrite conflicting keys (default: skip conflicts)")
	f.Bool("dry-run", false, "Show what would change without saving")

	return cmd
}

func runConfigImport(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	filePath := args[0]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	target, site := resolveConfigTarget(cmd)

	// Read and parse input file.
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return output.NewCLIError("Failed to read import file").
			WithErr(err).
			WithContext("file: " + filePath)
	}

	var imported map[string]any

	// Try JSON first, then YAML.
	if json.Valid(raw) {
		if unmarshalErr := json.Unmarshal(raw, &imported); unmarshalErr != nil {
			return output.NewCLIError("Failed to parse JSON").WithErr(unmarshalErr)
		}
	} else {
		if unmarshalErr := yaml.Unmarshal(raw, &imported); unmarshalErr != nil {
			return output.NewCLIError("Failed to parse YAML").WithErr(unmarshalErr)
		}
	}

	if imported == nil {
		imported = map[string]any{}
	}

	// Load existing config.
	existing, err := loadTargetConfig(target, ctx.ProjectRoot, site)
	if err != nil {
		return output.NewCLIError("Failed to load existing config").WithErr(err)
	}

	overwrite, _ := cmd.Flags().GetBool("overwrite")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Flatten both for comparison.
	flatExisting := config.FlattenMap(existing, "")
	flatImported := config.FlattenMap(imported, "")

	var added, modified, skipped int
	changes := make(map[string]any)

	for k, v := range flatImported {
		if existingVal, exists := flatExisting[k]; exists {
			if fmt.Sprintf("%v", existingVal) == fmt.Sprintf("%v", v) {
				continue // same value
			}
			if overwrite {
				changes[k] = v
				modified++
			} else {
				skipped++
			}
		} else {
			changes[k] = v
			added++
		}
	}

	if dryRun {
		if w.Mode() == output.ModeJSON {
			return w.PrintJSON(map[string]any{
				"added":    added,
				"modified": modified,
				"skipped":  skipped,
				"changes":  changes,
				"dry_run":  true,
			})
		}

		w.PrintInfo(fmt.Sprintf("Dry run: %d added, %d modified, %d skipped (conflicts)", added, modified, skipped))
		if len(changes) > 0 {
			keys := sortedKeys(changes)
			for _, k := range keys {
				w.Print("  %s = %v", k, changes[k])
			}
		}
		return nil
	}

	// Apply changes.
	var merged map[string]any
	if overwrite {
		merged = config.MergeMaps(existing, imported)
	} else {
		// Only add keys that don't exist.
		merged = config.MergeMaps(imported, existing)
	}

	if err := saveTargetConfig(target, ctx.ProjectRoot, site, merged); err != nil {
		return output.NewCLIError("Failed to save config").WithErr(err)
	}

	// Sync if site target.
	if target == targetSite {
		resolved, resolveErr := resolveFullConfig(ctx.ProjectRoot, site)
		if resolveErr == nil {
			_ = syncConfigToDB(cmd, ctx, site, resolved)
		}
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"added":    added,
			"modified": modified,
			"skipped":  skipped,
			"file":     configFilePath(target, ctx.ProjectRoot, site),
		})
	}

	w.PrintSuccess(fmt.Sprintf("Imported: %d added, %d modified, %d skipped", added, modified, skipped))
	return nil
}

// ---------------------------------------------------------------------------
// config edit
// ---------------------------------------------------------------------------

func newConfigEditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Open config in $EDITOR",
		Long:  "Open the configuration file in $EDITOR for manual editing. Validates after save.",
		RunE:  runConfigEdit,
	}

	f := cmd.Flags()
	f.String("site", "", "Edit site config")
	f.Bool("common", false, "Edit common_site_config.yaml")

	return cmd
}

func runConfigEdit(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	target, site := resolveConfigTarget(cmd)
	filePath := configFilePath(target, ctx.ProjectRoot, site)

	// Ensure the file exists (create empty if needed for site/common).
	if target != targetProject {
		if _, statErr := os.Stat(filePath); os.IsNotExist(statErr) {
			if mkdirErr := os.MkdirAll(filepath.Dir(filePath), 0o755); mkdirErr != nil {
				return output.NewCLIError("Failed to create config directory").WithErr(mkdirErr)
			}
			if writeErr := os.WriteFile(filePath, []byte("# Site configuration\n"), 0o644); writeErr != nil {
				return output.NewCLIError("Failed to create config file").WithErr(writeErr)
			}
		}
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	editorCmd := exec.Command(editor, filePath)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr

	if err := editorCmd.Run(); err != nil {
		return output.NewCLIError("Editor exited with error").WithErr(err)
	}

	// Validate after save.
	if target == targetProject {
		cfg, parseErr := config.ParseFile(filePath)
		if parseErr != nil {
			w.PrintWarning(fmt.Sprintf("Config has parse errors: %v", parseErr))
			w.Print("  File: %s", filePath)
			return nil
		}
		if errs := config.Validate(cfg); len(errs) > 0 {
			w.PrintWarning("Config has validation errors:")
			for _, e := range errs {
				w.Print("  %s: %s", e.Field, e.Message)
			}
			return nil
		}
	} else {
		raw, readErr := os.ReadFile(filePath)
		if readErr != nil {
			w.PrintWarning(fmt.Sprintf("Cannot read file after edit: %v", readErr))
			return nil
		}
		var data map[string]any
		if yamlErr := yaml.Unmarshal(raw, &data); yamlErr != nil {
			w.PrintWarning(fmt.Sprintf("Config has YAML errors: %v", yamlErr))
			return nil
		}
	}

	// Sync if site target.
	if target == targetSite {
		resolved, resolveErr := resolveFullConfig(ctx.ProjectRoot, site)
		if resolveErr == nil {
			_ = syncConfigToDB(cmd, ctx, site, resolved)
		}
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"file": filePath, "valid": true})
	}

	w.PrintSuccess(fmt.Sprintf("Config saved and validated: %s", filePath))
	return nil
}

// ---------------------------------------------------------------------------
// sync helper
// ---------------------------------------------------------------------------

// syncConfigToDB attempts to sync resolved config to the database. Errors are
// returned but not fatal — callers should warn the user.
func syncConfigToDB(cmd *cobra.Command, ctx *clicontext.CLIContext, site string, resolved map[string]any) error {
	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	var rs *config.RedisSync
	if svc.Redis != nil {
		rs = &config.RedisSync{
			Cache:  svc.Redis.Cache,
			PubSub: svc.Redis.PubSub,
		}
	}

	return config.SyncToDatabase(cmd.Context(), site, resolved, svc.DB.SystemPool(), rs, svc.Logger)
}

package scaffold

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"golang.org/x/mod/modfile"
)

// Template is the scaffold template mode.
type Template string

const (
	TemplateStandard Template = "standard"
	TemplateMinimal  Template = "minimal"
	TemplateAPIOnly  Template = "api-only"
)

// appNamePattern matches a valid app name: lowercase letter followed by lowercase
// letters, digits, or underscores. Mirrors pkg/apps/manifest.go.
var appNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ScaffoldOptions configures what ScaffoldApp generates.
type ScaffoldOptions struct {
	// AppName is the snake_case app identifier.
	AppName string

	// AppsDir is the absolute path to the project's apps/ directory.
	AppsDir string

	// ProjectRoot is the absolute path to the project root (for go.work update).
	ProjectRoot string

	// ModuleName is the TitleCase module name. If empty, derived from AppName.
	ModuleName string

	// Title is the human-readable app title. If empty, derived from AppName.
	Title string

	// Publisher is the publisher/organization name.
	Publisher string

	// License defaults to "MIT".
	License string

	// DocType is an optional initial DocType name to scaffold.
	DocType string

	// Template selects the scaffold mode. Defaults to TemplateStandard.
	Template Template

	// GoModulePath is the Go module path prefix override.
	// If empty, read from the project root go.mod.
	GoModulePath string

	// FrameworkModuleVersion is the required framework module version written
	// into the generated app go.mod. Defaults to v0.0.0 for in-repo development.
	FrameworkModuleVersion string

	// FrameworkReplacePath is an optional local replace directive target for the
	// framework module. Defaults to ../.. for in-repo development.
	FrameworkReplacePath string

	// SkipGoModTidy skips running "go mod tidy" after scaffolding.
	SkipGoModTidy bool

	// SkipGoWorkUpdate skips running "go work use" to update go.work.
	SkipGoWorkUpdate bool

	// IncludeDesk scaffolds a desk/ directory with desk-manifest.json template
	// for desk UI extensions (custom field types, pages, widgets).
	IncludeDesk bool
}

// ScaffoldApp creates a new Moca app directory structure at AppsDir/AppName.
func ScaffoldApp(opts ScaffoldOptions) error {
	if err := validateOpts(&opts); err != nil {
		return err
	}

	appDir := filepath.Join(opts.AppsDir, opts.AppName)

	// Read module path from project root go.mod if not provided.
	goModPath := opts.GoModulePath
	if goModPath == "" {
		goModPath = readGoModulePath(opts.ProjectRoot)
	}

	moduleSnake := toSnakeCase(opts.ModuleName)

	data := templateData{
		AppName:                opts.AppName,
		PackageName:            opts.AppName,
		ModuleName:             opts.ModuleName,
		ModuleSnake:            moduleSnake,
		Title:                  opts.Title,
		Publisher:              opts.Publisher,
		License:                opts.License,
		GoModulePath:           goModPath + "/apps/" + opts.AppName,
		FrameworkModuleVersion: opts.FrameworkModuleVersion,
		FrameworkReplacePath:   opts.FrameworkReplacePath,
		DocType:                opts.DocType,
		DocTypeSnake:           toSnakeCase(opts.DocType),
		IncludeDesk:            opts.IncludeDesk,
	}

	if err := createDirectories(appDir, opts.Template, moduleSnake, opts.IncludeDesk); err != nil {
		return fmt.Errorf("create directories: %w", err)
	}

	if err := renderTemplates(appDir, opts.Template, data); err != nil {
		return fmt.Errorf("render templates: %w", err)
	}

	if opts.DocType != "" {
		if err := scaffoldDocType(appDir, moduleSnake, data); err != nil {
			return fmt.Errorf("scaffold doctype: %w", err)
		}
	}

	if !opts.SkipGoWorkUpdate {
		relPath := filepath.Join(".", "apps", opts.AppName)
		if err := AddToGoWork(opts.ProjectRoot, relPath); err != nil {
			return fmt.Errorf("update go.work: %w", err)
		}
	}

	if !opts.SkipGoModTidy {
		if err := runGoModTidy(appDir); err != nil {
			return fmt.Errorf("go mod tidy: %w", err)
		}
	}

	return nil
}

// AddToGoWork adds a module path to the project's go.work file using `go work use`.
// Exported for reuse by other commands (e.g., moca app get).
func AddToGoWork(projectRoot, appRelPath string) error {
	cmd := exec.Command("go", "work", "use", appRelPath)
	cmd.Dir = projectRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func validateOpts(opts *ScaffoldOptions) error {
	if opts.AppName == "" {
		return fmt.Errorf("app name is required")
	}
	if !appNamePattern.MatchString(opts.AppName) {
		return fmt.Errorf("invalid app name %q: must match %s (lowercase letters, digits, underscores; must start with a letter)", opts.AppName, appNamePattern.String())
	}
	if opts.AppsDir == "" {
		return fmt.Errorf("apps directory is required")
	}

	appDir := filepath.Join(opts.AppsDir, opts.AppName)
	if _, err := os.Stat(appDir); err == nil {
		return fmt.Errorf("directory %q already exists", appDir)
	}

	// Apply defaults.
	if opts.License == "" {
		opts.License = "MIT"
	}
	if opts.Template == "" {
		opts.Template = TemplateStandard
	}
	if opts.ModuleName == "" {
		opts.ModuleName = deriveModuleName(opts.AppName)
	}
	if opts.Title == "" {
		opts.Title = deriveTitle(opts.AppName)
	}
	if opts.FrameworkModuleVersion == "" {
		opts.FrameworkModuleVersion = "v0.0.0"
		if opts.FrameworkReplacePath == "" {
			opts.FrameworkReplacePath = "../.."
		}
	}

	return nil
}

// deriveModuleName converts a snake_case app name to TitleCase module name.
// e.g., "my_app" → "MyApp", "crm" → "Crm".
func deriveModuleName(appName string) string {
	parts := strings.Split(appName, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	return b.String()
}

// deriveTitle converts a snake_case app name to a human-readable title.
// e.g., "my_app" → "My App", "crm" → "Crm".
func deriveTitle(appName string) string {
	parts := strings.Split(appName, "_")
	titled := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		titled = append(titled, strings.ToUpper(p[:1])+p[1:])
	}
	return strings.Join(titled, " ")
}

// toSnakeCase converts a TitleCase or mixed-case string to snake_case.
// Spaces and dashes are converted to underscores.
func toSnakeCase(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	var result strings.Builder
	for i, r := range runes {
		switch {
		case r >= 'A' && r <= 'Z':
			if i > 0 && runes[i-1] != ' ' && runes[i-1] != '-' && runes[i-1] != '_' {
				result.WriteByte('_')
			}
			result.WriteRune(r + ('a' - 'A'))
		case r == ' ' || r == '-':
			result.WriteByte('_')
		default:
			result.WriteRune(r)
		}
	}
	return result.String()
}

func createDirectories(appDir string, tmpl Template, moduleSnake string, includeDesk bool) error {
	// Common directories for all templates.
	dirs := []string{
		filepath.Join("modules", moduleSnake, "doctypes"),
	}

	switch tmpl {
	case TemplateStandard:
		dirs = append(dirs,
			filepath.Join("modules", moduleSnake, "pages"),
			filepath.Join("modules", moduleSnake, "reports"),
			"fixtures",
			"migrations",
			filepath.Join("templates", "portal"),
			"public",
			"tests",
		)
	case TemplateAPIOnly:
		// api-only has the module dir but no pages/reports.
	case TemplateMinimal:
		// minimal has just the module/doctypes dir.
	}

	if includeDesk {
		dirs = append(dirs,
			"desk",
			filepath.Join("desk", "fields"),
		)
	}

	for _, d := range dirs {
		full := filepath.Join(appDir, d)
		if err := os.MkdirAll(full, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	// Write .gitkeep in leaf directories that would otherwise be empty.
	gitkeepDirs := []string{
		filepath.Join("modules", moduleSnake, "doctypes"),
	}
	if tmpl == TemplateStandard {
		gitkeepDirs = append(gitkeepDirs,
			filepath.Join("modules", moduleSnake, "pages"),
			filepath.Join("modules", moduleSnake, "reports"),
		)
	}

	for _, d := range gitkeepDirs {
		gk := filepath.Join(appDir, d, ".gitkeep")
		if err := os.WriteFile(gk, nil, 0o644); err != nil {
			return fmt.Errorf("write .gitkeep in %s: %w", d, err)
		}
	}

	return nil
}

func renderTemplates(appDir string, tmpl Template, data templateData) error {
	// Files common to all templates.
	files := []struct {
		path     string
		template string
	}{
		{"manifest.yaml", manifestTmpl},
		{"hooks.go", hooksTmpl},
		{"go.mod", goModTmpl},
		{"README.md", readmeTmpl},
	}

	// Standard-only files.
	if tmpl == TemplateStandard {
		files = append(files,
			struct {
				path     string
				template string
			}{filepath.Join("migrations", "001_initial.sql"), migrationTmpl},
			struct {
				path     string
				template string
			}{filepath.Join("tests", "setup_test.go"), setupTestTmpl},
		)
	}

	// API-only files.
	if tmpl == TemplateAPIOnly {
		files = append(files, struct {
			path     string
			template string
		}{filepath.Join("modules", data.ModuleSnake, "api.go"), apiControllerTmpl})
	}

	// Desk extension files.
	if data.IncludeDesk {
		files = append(files,
			struct {
				path     string
				template string
			}{filepath.Join("desk", "desk-manifest.json"), deskManifestTmpl},
			struct {
				path     string
				template string
			}{filepath.Join("desk", "fields", "example.ts"), deskExampleFieldTmpl},
		)
	}

	for _, f := range files {
		if err := renderToFile(filepath.Join(appDir, f.path), f.template, data); err != nil {
			return fmt.Errorf("render %s: %w", f.path, err)
		}
	}

	return nil
}

func scaffoldDocType(appDir, moduleSnake string, data templateData) error {
	dtDir := filepath.Join(appDir, "modules", moduleSnake, "doctypes", data.DocTypeSnake)
	if err := os.MkdirAll(dtDir, 0o755); err != nil {
		return fmt.Errorf("mkdir doctype dir: %w", err)
	}

	// Remove .gitkeep since the directory now has content.
	gitkeep := filepath.Join(appDir, "modules", moduleSnake, "doctypes", ".gitkeep")
	os.Remove(gitkeep) //nolint:errcheck

	jsonPath := filepath.Join(dtDir, data.DocTypeSnake+".json")
	return renderToFile(jsonPath, docTypeTmpl, data)
}

func renderToFile(path, tmplText string, data any) error {
	t, err := template.New(filepath.Base(path)).Parse(tmplText)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return fmt.Errorf("execute template: %w", err)
	}

	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func runGoModTidy(appDir string) error {
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = appDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// readGoModulePath reads the module path from the go.mod file in the given directory.
// Returns a fallback if the file cannot be read or parsed.
func readGoModulePath(dir string) string {
	const fallback = "github.com/osama1998H/moca"

	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return fallback
	}

	if modPath := modfile.ModulePath(data); modPath != "" {
		return modPath
	}
	return fallback
}

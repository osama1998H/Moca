package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
)

// DeskScaffoldOptions configures what ScaffoldDesk generates.
type DeskScaffoldOptions struct {
	// ProjectRoot is the absolute path to the project root directory.
	ProjectRoot string

	// ProjectName is the project identifier used in package.json (e.g. "my-erp").
	ProjectName string

	// MocaDeskVersion is the semantic version of @osama1998h/desk (e.g. "0.1.0").
	// Used only for display/comments; the actual dependency spec is MocaDeskSpec.
	MocaDeskVersion string

	// MocaDeskSpec is the npm dependency specifier for @osama1998h/desk.
	// Examples: "^0.1.0", "file:../../desk".
	// Defaults to "^{MocaDeskVersion}" if empty.
	MocaDeskSpec string
}

// ScaffoldDesk creates a thin desk/ directory inside the project root with all
// files needed to consume the @osama1998h/desk npm package. The scaffolded project
// includes package.json, Vite config, TypeScript config, a minimal main.tsx
// entry point, and an overrides directory for project-level customizations.
func ScaffoldDesk(opts DeskScaffoldOptions) error {
	if err := validateDeskOpts(&opts); err != nil {
		return err
	}

	deskDir := filepath.Join(opts.ProjectRoot, "desk")

	// Fail if desk/ already exists.
	if _, err := os.Stat(deskDir); err == nil {
		return fmt.Errorf("desk/ directory already exists in %s", opts.ProjectRoot)
	}

	// Create directory structure.
	dirs := []string{
		deskDir,
		filepath.Join(deskDir, "src"),
		filepath.Join(deskDir, "src", "overrides"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create directory %s: %w", d, err)
		}
	}

	data := deskTemplateData{
		ProjectName:     opts.ProjectName,
		MocaDeskVersion: opts.MocaDeskVersion,
		MocaDeskSpec:    opts.MocaDeskSpec,
	}

	// Render all template files.
	files := []struct {
		path     string
		template string
	}{
		{"package.json", deskPackageJSONTmpl},
		{"index.html", deskIndexHTMLTmpl},
		{"vite.config.ts", deskViteConfigTmpl},
		{"tsconfig.json", deskTsconfigTmpl},
		{"tsconfig.app.json", deskTsconfigAppTmpl},
		{"tsconfig.node.json", deskTsconfigNodeTmpl},
		{filepath.Join("src", "main.tsx"), deskMainTsxTmpl},
		{filepath.Join("src", "overrides", "index.ts"), deskOverridesIndexTmpl},
		{filepath.Join("src", "overrides", "theme.ts"), deskOverridesThemeTmpl},
		{".gitignore", deskGitignoreTmpl},
		{".moca-extensions.ts", deskExtensionsStubTmpl},
	}

	for _, f := range files {
		fullPath := filepath.Join(deskDir, f.path)
		if err := renderToFile(fullPath, f.template, data); err != nil {
			return fmt.Errorf("render %s: %w", f.path, err)
		}
	}

	return nil
}

func validateDeskOpts(opts *DeskScaffoldOptions) error {
	if opts.ProjectRoot == "" {
		return fmt.Errorf("project root is required")
	}
	if opts.ProjectName == "" {
		return fmt.Errorf("project name is required")
	}

	// Apply defaults.
	if opts.MocaDeskVersion == "" {
		opts.MocaDeskVersion = "0.1.0"
	}
	if opts.MocaDeskSpec == "" {
		opts.MocaDeskSpec = "^" + opts.MocaDeskVersion
	}

	return nil
}

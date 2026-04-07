package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/pkg/apps"
	"github.com/spf13/cobra"
)

// NewBuildCommand returns the "moca build" command group with all subcommands.
func NewBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build operations",
		Long:  "Build frontend assets, compile apps, and produce server binaries.",
	}

	cmd.AddCommand(
		newBuildDeskCmd(),
		newSubcommand("portal", "Build portal/website assets"),
		newSubcommand("assets", "Build all static assets"),
		newBuildAppCmd(),
		newBuildServerCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// build desk
// ---------------------------------------------------------------------------

func newBuildDeskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "desk",
		Short: "Build React Desk frontend",
		Long: `Build the React Desk frontend using Vite.
Runs 'npx vite build' in the desk/ directory, producing optimized
production assets in desk/dist/.`,
		RunE: runBuildDesk,
	}

	f := cmd.Flags()
	f.Bool("verbose", false, "Show Vite build output")

	return cmd
}

func runBuildDesk(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}
	projectRoot := ctx.ProjectRoot
	verbose, _ := cmd.Flags().GetBool("verbose")

	deskDir := filepath.Join(projectRoot, "desk")

	// Verify desk/ directory exists with a package.json.
	pkgJSON := filepath.Join(deskDir, "package.json")
	if _, err := os.Stat(pkgJSON); err != nil {
		return output.NewCLIError("No desk/ project found").
			WithErr(err).
			WithCause(fmt.Sprintf("%s does not exist", pkgJSON)).
			WithFix("Run 'moca new desk' to scaffold the React Desk project, or create desk/package.json manually.")
	}

	// Discover app desk extensions and generate the import manifest.
	if extErr := generateDeskExtensions(projectRoot, deskDir); extErr != nil {
		return extErr
	}

	// Run npx vite build.
	buildCmd := exec.Command("npx", "vite", "build")
	buildCmd.Dir = deskDir
	buildCmd.Env = append(os.Environ(), "NODE_ENV=production")

	if verbose {
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
	}

	s := w.NewSpinner("Building React Desk frontend...")
	if !verbose {
		s.Start()
	}

	out, runErr := runCmdCaptureStderr(buildCmd, verbose)

	if !verbose {
		if runErr != nil {
			s.Stop("Failed")
		} else {
			s.Stop("OK")
		}
	}

	if runErr != nil {
		if out != "" {
			w.Print(out)
		}
		return output.NewCLIError("Desk build failed").
			WithErr(runErr).
			WithFix("Fix the build errors above and try again. Ensure Node.js and npm are installed.")
	}

	// Report output directory size if it exists.
	distDir := filepath.Join(deskDir, "dist")
	if info, statErr := os.Stat(distDir); statErr == nil && info.IsDir() {
		var totalSize int64
		_ = filepath.Walk(distDir, func(_ string, fi os.FileInfo, _ error) error {
			if fi != nil && !fi.IsDir() {
				totalSize += fi.Size()
			}
			return nil
		})
		w.PrintSuccess(fmt.Sprintf("Desk build complete: %s (%s)", distDir, formatBytes(totalSize)))
	} else {
		w.PrintSuccess("Desk build complete")
	}

	return nil
}

// ---------------------------------------------------------------------------
// build app
// ---------------------------------------------------------------------------

func newBuildAppCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app APP_NAME",
		Short: "Verify an app's Go code compiles",
		Long: `Verify that an app's Go code compiles cleanly within the workspace.
Does not produce a standalone binary — apps are composed into the
server binary via 'moca build server'.`,
		Args: cobra.ExactArgs(1),
		RunE: runBuildApp,
	}

	f := cmd.Flags()
	f.Bool("race", false, "Enable race detector")
	f.Bool("verbose", false, "Show compiler output")

	return cmd
}

func runBuildApp(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	appName := args[0]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}
	projectRoot := ctx.ProjectRoot

	race, _ := cmd.Flags().GetBool("race")
	verbose, _ := cmd.Flags().GetBool("verbose")

	// Verify app exists and has a valid manifest.
	appDir := filepath.Join(projectRoot, "apps", appName)
	if _, err := apps.LoadApp(appDir); err != nil {
		return output.NewCLIError(fmt.Sprintf("App %q not found or invalid", appName)).
			WithErr(err).
			WithCause(err.Error()).
			WithFix(fmt.Sprintf("Ensure apps/%s/ exists with a valid manifest.yaml.", appName))
	}

	// Build go build args.
	buildArgs := []string{"build"}
	if race {
		buildArgs = append(buildArgs, "-race")
	}
	buildArgs = append(buildArgs, "./apps/"+appName+"/...")

	if verbose {
		w.Print("Running: go %s", joinArgs(buildArgs))
	}

	goCmd := exec.Command("go", buildArgs...)
	goCmd.Dir = projectRoot
	goCmd.Env = ensureGoWork(os.Environ(), projectRoot)

	if verbose {
		goCmd.Stdout = os.Stdout
		goCmd.Stderr = os.Stderr
	} else {
		// Capture stderr for error reporting.
		goCmd.Stdout = nil
	}

	s := w.NewSpinner(fmt.Sprintf("Compiling app %q...", appName))
	if !verbose {
		s.Start()
	}

	out, runErr := runCmdCaptureStderr(goCmd, verbose)

	if !verbose {
		if runErr != nil {
			s.Stop("Failed")
		} else {
			s.Stop("OK")
		}
	}

	if runErr != nil {
		if out != "" {
			w.Print(out)
		}
		return output.NewCLIError(fmt.Sprintf("App %q failed to compile", appName)).
			WithErr(runErr).
			WithFix("Fix the compilation errors above and try again.")
	}

	w.PrintSuccess(fmt.Sprintf("App %q compiles cleanly", appName))
	return nil
}

// ---------------------------------------------------------------------------
// build server
// ---------------------------------------------------------------------------

func newBuildServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Compile server binary with all installed apps",
		Long: `Compile the moca-server binary with all installed apps included.
Uses the Go workspace (go.work) to compose the framework and all
app modules into a single binary.`,
		RunE: runBuildServer,
	}

	f := cmd.Flags()
	f.String("output", "", "Output binary path (default: bin/moca-server)")
	f.Bool("race", false, "Enable race detector")
	f.Bool("verbose", false, "Show compiler output")
	f.String("ldflags", "", "Additional linker flags")

	return cmd
}

func runBuildServer(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}
	projectRoot := ctx.ProjectRoot

	outputPath, _ := cmd.Flags().GetString("output")
	race, _ := cmd.Flags().GetBool("race")
	verbose, _ := cmd.Flags().GetBool("verbose")
	extraLdflags, _ := cmd.Flags().GetString("ldflags")

	// Default output path.
	if outputPath == "" {
		outputPath = filepath.Join(projectRoot, "bin", "moca-server")
	}

	// Ensure output directory exists.
	if mkdirErr := os.MkdirAll(filepath.Dir(outputPath), 0o755); mkdirErr != nil {
		return output.NewCLIError("Cannot create output directory").
			WithErr(mkdirErr).
			WithCause(mkdirErr.Error())
	}

	// Construct ldflags with version and build time.
	version := Version
	if ctx.Project.Moca != "" {
		version = ctx.Project.Moca
	}
	buildTime := time.Now().UTC().Format(time.RFC3339)
	ldflags := fmt.Sprintf("-X main.Version=%s -X main.BuildTime=%s", version, buildTime)
	if extraLdflags != "" {
		ldflags += " " + extraLdflags
	}

	// Build go build args.
	buildArgs := []string{"build", "-o", outputPath, "-ldflags", ldflags}
	if race {
		buildArgs = append(buildArgs, "-race")
	}
	buildArgs = append(buildArgs, "./cmd/moca-server/")

	if verbose {
		w.Print("Running: go %s", joinArgs(buildArgs))
	}

	goCmd := exec.Command("go", buildArgs...)
	goCmd.Dir = projectRoot
	goCmd.Env = ensureGoWork(os.Environ(), projectRoot)

	if verbose {
		goCmd.Stdout = os.Stdout
		goCmd.Stderr = os.Stderr
	}

	s := w.NewSpinner("Compiling server binary...")
	if !verbose {
		s.Start()
	}

	out, runErr := runCmdCaptureStderr(goCmd, verbose)

	if !verbose {
		if runErr != nil {
			s.Stop("Failed")
		} else {
			s.Stop("OK")
		}
	}

	if runErr != nil {
		if out != "" {
			w.Print(out)
		}
		return output.NewCLIError("Server compilation failed").
			WithErr(runErr).
			WithFix("Fix the compilation errors above and try again.")
	}

	// Report binary size.
	info, err := os.Stat(outputPath)
	if err == nil {
		w.PrintSuccess(fmt.Sprintf("Server binary: %s (%s)", outputPath, formatBytes(info.Size())))
	} else {
		w.PrintSuccess(fmt.Sprintf("Server binary: %s", outputPath))
	}

	return nil
}

// ---------------------------------------------------------------------------
// desk extension discovery
// ---------------------------------------------------------------------------

// extensionSource represents a discovered app with desk extensions.
type extensionSource struct {
	appName  string
	manifest *apps.DeskManifest // non-nil when desk-manifest.json exists
	// legacyImport is the relative POSIX import path for apps using the
	// older desk/setup.ts convention (empty when manifest is present).
	legacyImport string
}

// generateDeskExtensions scans installed apps for desk extensions and writes a
// synthetic .moca-extensions.ts file. It supports two discovery modes:
//
//  1. desk-manifest.json — structured manifest with field types, pages, sidebar
//     items, and dashboard widgets. Generates typed registration calls.
//  2. Legacy fallback — desk/setup.ts, desk/setup.tsx, or desk/index.ts. Generates
//     a bare side-effect import (backward compatible with pre-Phase 3 apps).
//
// If both exist for the same app, the manifest takes precedence.
func generateDeskExtensions(projectRoot, deskDir string) error {
	sources := discoverExtensionSources(projectRoot, deskDir)
	content := buildExtensionContent(sources, deskDir)

	extFile := filepath.Join(deskDir, ".moca-extensions.ts")
	if err := os.WriteFile(extFile, []byte(content), 0o644); err != nil {
		return output.NewCLIError("Failed to write desk extension manifest").
			WithErr(err).
			WithCause(err.Error()).
			WithFix("Check file permissions in the desk/ directory.")
	}

	return nil
}

// discoverExtensionSources scans all apps for desk extension entry points.
func discoverExtensionSources(projectRoot, deskDir string) []extensionSource {
	appsDir := filepath.Join(projectRoot, "apps")
	appInfos, scanErr := apps.ScanApps(appsDir)
	if scanErr != nil {
		return nil
	}

	var sources []extensionSource
	for _, app := range appInfos {
		// Prefer desk-manifest.json over legacy setup files.
		manifestPath := filepath.Join(app.Path, "desk", "desk-manifest.json")
		if _, err := os.Stat(manifestPath); err == nil {
			m, parseErr := apps.ParseDeskManifest(manifestPath)
			if parseErr != nil {
				continue
			}
			if valErr := apps.ValidateDeskManifest(m); valErr != nil {
				continue
			}
			sources = append(sources, extensionSource{
				appName:  app.Name,
				manifest: m,
			})
			continue
		}

		// Legacy fallback: bare setup file import.
		candidates := []string{"desk/setup.ts", "desk/setup.tsx", "desk/index.ts"}
		for _, candidate := range candidates {
			setupPath := filepath.Join(app.Path, candidate)
			if _, err := os.Stat(setupPath); err == nil {
				relPath, relErr := filepath.Rel(deskDir, setupPath)
				if relErr != nil {
					continue
				}
				relPath = strings.ReplaceAll(relPath, string(filepath.Separator), "/")
				relPath = strings.TrimSuffix(relPath, filepath.Ext(relPath))
				sources = append(sources, extensionSource{
					appName:      app.Name,
					legacyImport: relPath,
				})
				break
			}
		}
	}
	return sources
}

// buildExtensionContent generates the TypeScript content for .moca-extensions.ts.
func buildExtensionContent(sources []extensionSource, deskDir string) string {
	var buf strings.Builder
	buf.WriteString("// Auto-generated by 'moca build desk'. Do not edit.\n")

	if len(sources) == 0 {
		buf.WriteString("// No app desk extensions discovered.\n")
		return buf.String()
	}

	// Determine which registration functions are needed for the import header.
	needsRegister := map[string]bool{}
	for _, src := range sources {
		if src.manifest == nil {
			continue
		}
		ext := src.manifest.Extensions
		if len(ext.FieldTypes) > 0 {
			needsRegister["registerFieldType"] = true
		}
		if len(ext.Pages) > 0 {
			needsRegister["registerPage"] = true
		}
		if len(ext.SidebarItems) > 0 {
			needsRegister["registerSidebarItem"] = true
		}
		if len(ext.DashboardWidgets) > 0 {
			needsRegister["registerDashboardWidget"] = true
		}
	}

	if len(needsRegister) > 0 {
		var funcs []string
		for _, fn := range []string{"registerFieldType", "registerPage", "registerSidebarItem", "registerDashboardWidget"} {
			if needsRegister[fn] {
				funcs = append(funcs, fn)
			}
		}
		fmt.Fprintf(&buf, "import { %s } from \"@moca/desk\";\n", strings.Join(funcs, ", "))
	}

	for _, src := range sources {
		buf.WriteString("\n")
		fmt.Fprintf(&buf, "// === %s ===\n", src.appName)

		if src.manifest == nil {
			// Legacy: bare side-effect import.
			fmt.Fprintf(&buf, "import %q;\n", src.legacyImport)
			continue
		}

		ext := src.manifest.Extensions
		appDir := filepath.Join(filepath.Dir(deskDir), "apps", src.appName, "desk")

		// Field types.
		for name, componentPath := range ext.FieldTypes {
			importPath := resolveComponentImport(deskDir, appDir, componentPath)
			importName := generateImportName(src.appName, "Field", componentPath)
			fmt.Fprintf(&buf, "import %s from %q;\n", importName, importPath)
			fmt.Fprintf(&buf, "registerFieldType(%q, %s);\n", name, importName)
		}

		// Pages.
		for _, page := range ext.Pages {
			importPath := resolveComponentImport(deskDir, appDir, page.Component)
			importName := generateImportName(src.appName, "Page", page.Component)
			fmt.Fprintf(&buf, "import %s from %q;\n", importName, importPath)

			opts := buildOptionsLiteral(page.Label, page.Icon)
			if opts != "" {
				fmt.Fprintf(&buf, "registerPage(%q, %s, %s);\n", page.Path, importName, opts)
			} else {
				fmt.Fprintf(&buf, "registerPage(%q, %s);\n", page.Path, importName)
			}
		}

		// Sidebar items.
		for _, item := range ext.SidebarItems {
			fmt.Fprintf(&buf, "registerSidebarItem(%s);\n", buildSidebarItemLiteral(item))
		}

		// Dashboard widgets.
		for _, widget := range ext.DashboardWidgets {
			importPath := resolveComponentImport(deskDir, appDir, widget.Component)
			importName := generateImportName(src.appName, "Widget", widget.Component)
			fmt.Fprintf(&buf, "import %s from %q;\n", importName, importPath)

			if widget.Label != "" {
				fmt.Fprintf(&buf, "registerDashboardWidget(%q, %s, { label: %q });\n", widget.Name, importName, widget.Label)
			} else {
				fmt.Fprintf(&buf, "registerDashboardWidget(%q, %s);\n", widget.Name, importName)
			}
		}
	}

	return buf.String()
}

// generateImportName creates a unique TypeScript identifier for a component import.
// Format: {AppTitleCase}{Category}{FilenameStem} e.g. CrmFieldPhoneField.
func generateImportName(appName, category, componentPath string) string {
	// Extract filename stem: "./fields/PhoneField.tsx" -> "PhoneField"
	base := filepath.Base(componentPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))

	return titleCase(appName) + category + titleCase(stem)
}

// resolveComponentImport computes the POSIX relative import path from deskDir
// to the component file, stripping the file extension for TypeScript.
func resolveComponentImport(deskDir, appDeskDir, componentPath string) string {
	// componentPath is relative to the app's desk/ directory, e.g. "./fields/PhoneField.tsx"
	absPath := filepath.Join(appDeskDir, strings.TrimPrefix(componentPath, "./"))
	relPath, err := filepath.Rel(deskDir, absPath)
	if err != nil {
		return componentPath
	}
	relPath = strings.ReplaceAll(relPath, string(filepath.Separator), "/")
	relPath = strings.TrimSuffix(relPath, filepath.Ext(relPath))
	return relPath
}

// titleCase converts a snake_case or lowercase string to TitleCase.
func titleCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' })
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

// buildOptionsLiteral creates a JS object literal like { label: "X", icon: "Y" }.
// Returns empty string if no options are set.
func buildOptionsLiteral(label, icon string) string {
	var parts []string
	if label != "" {
		parts = append(parts, fmt.Sprintf("label: %q", label))
	}
	if icon != "" {
		parts = append(parts, fmt.Sprintf("icon: %q", icon))
	}
	if len(parts) == 0 {
		return ""
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

// buildSidebarItemLiteral serialises a DeskSidebarDef as a JS object literal.
func buildSidebarItemLiteral(item apps.DeskSidebarDef) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("label: %q", item.Label))
	if item.Icon != "" {
		parts = append(parts, fmt.Sprintf("icon: %q", item.Icon))
	}
	if item.Path != "" {
		parts = append(parts, fmt.Sprintf("path: %q", item.Path))
	}
	if item.Order != 0 {
		parts = append(parts, fmt.Sprintf("order: %d", item.Order))
	}
	if len(item.Children) > 0 {
		var children []string
		for _, child := range item.Children {
			children = append(children, fmt.Sprintf("{ label: %q, path: %q }", child.Label, child.Path))
		}
		parts = append(parts, "children: ["+strings.Join(children, ", ")+"]")
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// ensureGoWork returns a copy of env with GOWORK set to the project's go.work file.
func ensureGoWork(env []string, projectRoot string) []string {
	goWorkPath := filepath.Join(projectRoot, "go.work")
	// Replace existing GOWORK if present, otherwise append.
	found := false
	result := make([]string, 0, len(env)+1)
	for _, e := range env {
		if len(e) > 7 && e[:7] == "GOWORK=" {
			result = append(result, "GOWORK="+goWorkPath)
			found = true
		} else {
			result = append(result, e)
		}
	}
	if !found {
		result = append(result, "GOWORK="+goWorkPath)
	}
	return result
}

// runCmdCaptureStderr runs a command and returns captured stderr if not in verbose mode.
// In verbose mode, output goes directly to os.Stdout/Stderr and no capture occurs.
func runCmdCaptureStderr(cmd *exec.Cmd, verbose bool) (string, error) {
	if verbose {
		return "", cmd.Run()
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// joinArgs joins command arguments for display purposes.
func joinArgs(args []string) string {
	result := ""
	for i, a := range args {
		if i > 0 {
			result += " "
		}
		result += a
	}
	return result
}

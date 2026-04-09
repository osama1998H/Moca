package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	clicontext "github.com/osama1998H/moca/internal/context"
	"github.com/osama1998H/moca/internal/lockfile"
	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/internal/scaffold"
	"github.com/osama1998H/moca/pkg/apps"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"
)

const (
	frameworkModulePath       = "github.com/osama1998H/moca"
	localFrameworkVersion     = "v0.0.0"
	localFrameworkReplacePath = "../.."
)

// NewAppCommand returns the "moca app" command group with all subcommands.
func NewAppCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Application management",
		Long:  "Scaffold, install, update, and manage Moca applications.",
	}

	cmd.AddCommand(
		newAppInstallCmd(),
		newAppUninstallCmd(),
		newAppListCmd(),
		newAppNewCmd(),
		newAppGetCmd(),
		newSubcommand("remove", "Remove an app from project"),
		newAppUpdateCmd(),
		newAppResolveCmd(),
		newSubcommand("publish", "Publish app to registry"),
		newSubcommand("info", "Show app manifest details"),
		newAppDiffCmd(),
		newSubcommand("pin", "Pin app to exact version/commit"),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// app new
// ---------------------------------------------------------------------------

func newAppNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new APP_NAME",
		Short: "Scaffold a new Moca app",
		Long:  "Scaffold a new Moca application with directory structure, manifest, hooks, and Go module.",
		Args:  cobra.ExactArgs(1),
		RunE:  runAppNew,
	}

	f := cmd.Flags()
	f.String("module", "", "Initial module name (default: derived from app name)")
	f.String("title", "", "Human-readable app title")
	f.String("publisher", "", "Publisher/organization name")
	f.String("license", "MIT", "License identifier")
	f.String("doctype", "", "Create an initial DocType with the app")
	f.String("template", "standard", `App template: "standard", "minimal", "api-only"`)
	f.Bool("desk", false, "Include desk/ directory with desk-manifest.json for UI extensions")

	return cmd
}

func runAppNew(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	appName := args[0]

	// Normalize hyphens to underscores.
	if strings.Contains(appName, "-") {
		normalized := strings.ReplaceAll(appName, "-", "_")
		w.PrintWarning(fmt.Sprintf("App name %q contains hyphens; normalizing to %q", appName, normalized))
		appName = normalized
	}

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	moduleName, _ := cmd.Flags().GetString("module")
	title, _ := cmd.Flags().GetString("title")
	publisher, _ := cmd.Flags().GetString("publisher")
	license, _ := cmd.Flags().GetString("license")
	doctype, _ := cmd.Flags().GetString("doctype")
	tmpl, _ := cmd.Flags().GetString("template")
	includeDesk, _ := cmd.Flags().GetBool("desk")

	// Normalize and validate template flag.
	tmpl = strings.ToLower(tmpl)
	switch scaffold.Template(tmpl) {
	case scaffold.TemplateStandard, scaffold.TemplateMinimal, scaffold.TemplateAPIOnly:
		// valid
	default:
		return output.NewCLIError(fmt.Sprintf("Unknown template %q", tmpl)).
			WithFix(`Use one of: "standard", "minimal", "api-only"`)
	}

	appsDir := filepath.Join(ctx.ProjectRoot, "apps")
	frameworkVersion, frameworkReplacePath, err := resolveAppNewFrameworkDependency(ctx.ProjectRoot, Version)
	if err != nil {
		return err
	}

	opts := scaffold.ScaffoldOptions{
		AppName:                appName,
		AppsDir:                appsDir,
		ProjectRoot:            ctx.ProjectRoot,
		ModuleName:             moduleName,
		Title:                  title,
		Publisher:              publisher,
		License:                license,
		DocType:                doctype,
		Template:               scaffold.Template(tmpl),
		FrameworkModuleVersion: frameworkVersion,
		FrameworkReplacePath:   frameworkReplacePath,
		IncludeDesk:            includeDesk,
	}

	s := w.NewSpinner(fmt.Sprintf("Scaffolding app %q...", appName))
	s.Start()

	if err := scaffold.ScaffoldApp(opts); err != nil {
		s.Stop("Failed")
		return output.NewCLIError(fmt.Sprintf("Failed to scaffold app %q", appName)).
			WithErr(err).
			WithCause(err.Error())
	}

	s.Stop(fmt.Sprintf("App %q scaffolded", appName))

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"app":      appName,
			"path":     filepath.Join(appsDir, appName),
			"template": tmpl,
		})
	}

	w.PrintSuccess(fmt.Sprintf("App %q created at apps/%s", appName, appName))
	w.Print("")
	w.Print("Next steps:")
	w.Print("  moca app install %s --site <site-name>", appName)
	w.Print("")

	return nil
}

func resolveAppNewFrameworkDependency(projectRoot, cliVersion string) (string, string, error) {
	modulePath, err := readModulePathFromGoMod(filepath.Join(projectRoot, "go.mod"))
	if err != nil {
		return "", "", output.NewCLIError("Cannot determine project Go module").
			WithErr(err).
			WithFix("Ensure the project root contains a valid go.mod. Run 'moca init' to create one, or run 'go mod init <module-path>' in the project root.")
	}

	if modulePath == frameworkModulePath {
		return localFrameworkVersion, localFrameworkReplacePath, nil
	}

	if cliVersion == "" || cliVersion == "dev" {
		return "", "", output.NewCLIError("Standalone app scaffolding requires a released moca binary").
			WithContext(fmt.Sprintf("Project module %q is not the framework module %q.", modulePath, frameworkModulePath)).
			WithFix("Install or run a released moca build so the scaffold can pin github.com/osama1998H/moca to an exact version.")
	}

	// Go module versions require a "v" prefix (e.g. "v0.1.8").
	v := cliVersion
	if len(v) > 0 && v[0] != 'v' {
		v = "v" + v
	}
	return v, "", nil
}

func readModulePathFromGoMod(goModPath string) (string, error) {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", goModPath, err)
	}

	modulePath := modfile.ModulePath(data)
	if modulePath == "" {
		return "", fmt.Errorf("parse %s: module path not found", goModPath)
	}

	return modulePath, nil
}

// ---------------------------------------------------------------------------
// app install
// ---------------------------------------------------------------------------

func newAppInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install APP_NAME",
		Short: "Install an app on a site",
		Long:  "Install an app on a site. The app must already be in the project's apps/ directory.",
		Args:  cobra.ExactArgs(1),
		RunE:  runAppInstall,
	}

	f := cmd.Flags()
	f.Bool("all-sites", false, "Install on all sites")

	return cmd
}

func runAppInstall(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	appName := args[0]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	appsDir := filepath.Join(ctx.ProjectRoot, "apps")
	allSites, _ := cmd.Flags().GetBool("all-sites")

	var siteNames []string
	if allSites {
		sites, err := svc.Sites.ListSites(cmd.Context())
		if err != nil {
			return output.NewCLIError("Failed to list sites").WithErr(err)
		}
		for _, s := range sites {
			siteNames = append(siteNames, s.Name)
		}
		if len(siteNames) == 0 {
			return output.NewCLIError("No sites found").
				WithFix("Create a site first with 'moca site create <name>'.")
		}
	} else {
		siteName, err := resolveSiteName(cmd, ctx)
		if err != nil {
			return err
		}
		siteNames = []string{siteName}
	}

	for _, site := range siteNames {
		s := w.NewSpinner(fmt.Sprintf("Installing %q on %q...", appName, site))
		s.Start()
		if err := svc.Apps.Install(cmd.Context(), site, appName, appsDir); err != nil {
			s.Stop("Failed")
			return output.NewCLIError(fmt.Sprintf("Failed to install app %q on site %q", appName, site)).
				WithErr(err).
				WithCause(err.Error()).
				WithFix("Check that the app exists in apps/ and its dependencies are installed.")
		}
		s.Stop(fmt.Sprintf("App %q installed on %q", appName, site))
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"app":   appName,
			"sites": siteNames,
		})
	}

	w.PrintSuccess(fmt.Sprintf("App %q installed successfully", appName))
	return nil
}

// ---------------------------------------------------------------------------
// app uninstall
// ---------------------------------------------------------------------------

func newAppUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall APP_NAME",
		Short: "Uninstall an app from a site",
		Long:  "Uninstall an app from a site. This removes the app registration and optionally drops its database tables.",
		Args:  cobra.ExactArgs(1),
		RunE:  runAppUninstall,
	}

	f := cmd.Flags()
	f.Bool("force", false, "Skip confirmation")
	f.Bool("keep-data", false, "Remove app registration but keep database tables")
	f.Bool("dry-run", false, "Show what would be removed without executing")

	return cmd
}

func runAppUninstall(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	appName := args[0]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return err
	}

	force, _ := cmd.Flags().GetBool("force")
	if !force {
		ok, confirmErr := confirmPrompt(fmt.Sprintf("Uninstall app %q from site %q?", appName, siteName))
		if confirmErr != nil {
			return confirmErr
		}
		if !ok {
			w.Print("Aborted.")
			return nil
		}
	}

	keepData, _ := cmd.Flags().GetBool("keep-data")

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	opts := apps.UninstallOptions{
		DropTables: !keepData,
		Force:      force,
	}

	s := w.NewSpinner(fmt.Sprintf("Uninstalling %q from %q...", appName, siteName))
	s.Start()
	if err := svc.Apps.Uninstall(cmd.Context(), siteName, appName, opts); err != nil {
		s.Stop("Failed")
		return output.NewCLIError(fmt.Sprintf("Failed to uninstall app %q", appName)).
			WithErr(err).
			WithCause(err.Error()).
			WithContext("site: " + siteName)
	}
	s.Stop(fmt.Sprintf("App %q uninstalled from %q", appName, siteName))

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"app":    appName,
			"site":   siteName,
			"status": "uninstalled",
		})
	}

	w.PrintSuccess(fmt.Sprintf("App %q removed from site %q", appName, siteName))
	return nil
}

// ---------------------------------------------------------------------------
// app list
// ---------------------------------------------------------------------------

func newAppListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List apps (project or site level)",
		Long:  "List apps available in the project or installed on a specific site.",
		RunE:  runAppList,
	}

	f := cmd.Flags()
	f.Bool("project", false, "Show apps in the project (default when no --site)")

	return cmd
}

func runAppList(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteFlag, _ := cmd.Flags().GetString("site")
	projectFlag, _ := cmd.Flags().GetBool("project")

	// If --site is specified and --project is not, show installed apps on that site.
	if siteFlag != "" && !projectFlag {
		return appListInstalled(cmd, ctx, w, siteFlag)
	}

	// Default: show project-level apps from apps/ directory.
	return appListProject(ctx, w)
}

// appListProject lists apps discovered in the project's apps/ directory.
func appListProject(ctx *clicontext.CLIContext, w *output.Writer) error {
	appsDir := filepath.Join(ctx.ProjectRoot, "apps")
	appInfos, err := apps.ScanApps(appsDir)
	if err != nil {
		return output.NewCLIError("Failed to scan apps directory").
			WithErr(err).
			WithCause(err.Error())
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(appInfos)
	}

	if len(appInfos) == 0 {
		w.Print("No apps found in %s", appsDir)
		return nil
	}

	headers := []string{"APP", "VERSION", "PUBLISHER", "MODULES"}
	rows := make([][]string, 0, len(appInfos))
	for _, ai := range appInfos {
		version := ""
		publisher := ""
		modules := 0
		if ai.Manifest != nil {
			version = ai.Manifest.Version
			publisher = ai.Manifest.Publisher
			modules = len(ai.Manifest.Modules)
		}
		rows = append(rows, []string{
			ai.Name,
			version,
			publisher,
			fmt.Sprintf("%d", modules),
		})
	}
	return w.PrintTable(headers, rows)
}

// ---------------------------------------------------------------------------
// app get
// ---------------------------------------------------------------------------

func newAppGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get SOURCE",
		Short: "Download and install an app",
		Long:  "Downloads an app from a git URL, local path, or registry name and adds it to the project.",
		Args:  cobra.ExactArgs(1),
		RunE:  runAppGet,
	}

	f := cmd.Flags()
	f.String("version", "", "Version constraint (semver range)")
	f.String("branch", "", "Git branch to clone")
	f.String("ref", "", "Exact git ref (commit/tag)")
	f.Int("depth", 1, "Git clone depth (0 for full history)")
	f.Bool("no-resolve", false, "Skip dependency resolution")

	return cmd
}

func runAppGet(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	source := args[0]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	appsDir := filepath.Join(ctx.ProjectRoot, "apps")
	branch, _ := cmd.Flags().GetString("branch")
	ref, _ := cmd.Flags().GetString("ref")
	depth, _ := cmd.Flags().GetInt("depth")
	noResolve, _ := cmd.Flags().GetBool("no-resolve")

	// Parse source type and determine app name + target directory.
	sourceType, resolvedSource, appName := parseAppSource(source)

	targetDir := filepath.Join(appsDir, appName)
	if _, err := os.Stat(targetDir); err == nil {
		return output.NewCLIError(fmt.Sprintf("App directory %q already exists", targetDir)).
			WithFix(fmt.Sprintf("Use 'moca app update %s' to update, or remove the directory first.", appName))
	}

	s := w.NewSpinner(fmt.Sprintf("Getting app %q from %s...", appName, sourceType))
	s.Start()

	// Acquire the app.
	switch sourceType {
	case "git":
		if err := gitCloneApp(resolvedSource, targetDir, branch, ref, depth); err != nil {
			s.Stop("Failed")
			return output.NewCLIError(fmt.Sprintf("Failed to clone %q", resolvedSource)).
				WithErr(err).
				WithCause(err.Error()).
				WithFix("Check the URL and your network/authentication settings.")
		}
	case "local":
		if err := copyLocalApp(resolvedSource, targetDir); err != nil {
			s.Stop("Failed")
			return output.NewCLIError(fmt.Sprintf("Failed to copy from %q", resolvedSource)).
				WithErr(err).
				WithCause(err.Error())
		}
	}

	// Validate manifest. On failure, clean up the target directory.
	if _, err := apps.LoadApp(targetDir); err != nil {
		os.RemoveAll(targetDir) //nolint:errcheck
		s.Stop("Failed")
		return output.NewCLIError(fmt.Sprintf("App %q has no valid manifest", appName)).
			WithErr(err).
			WithCause(err.Error()).
			WithFix("Ensure the app contains a valid manifest.yaml.")
	}

	// Validate Go workspace dependencies.
	goModPath := filepath.Join(targetDir, "go.mod")
	if _, goModErr := os.Stat(goModPath); goModErr == nil {
		if conflicts := checkWorkspaceConflicts(w, goModPath, appsDir); len(conflicts) > 0 {
			w.PrintWarning(fmt.Sprintf("Detected %d major version conflict(s) — review before building.", len(conflicts)))
		}
	}

	// Update go.work.
	relPath := filepath.Join(".", "apps", appName)
	if err := scaffold.AddToGoWork(ctx.ProjectRoot, relPath); err != nil {
		w.PrintWarning(fmt.Sprintf("Failed to update go.work: %v", err))
	}

	// Resolve and write lockfile.
	if !noResolve {
		mocaVersion := ctx.Project.Moca
		if mocaVersion == "" {
			mocaVersion = "0.1.0"
		}
		lf, err := lockfile.Resolve(appsDir, mocaVersion, lockfile.ResolveOptions{})
		if err != nil {
			w.PrintWarning(fmt.Sprintf("Lockfile resolution failed: %v", err))
		} else {
			lockPath := filepath.Join(ctx.ProjectRoot, "moca.lock")
			if err := lockfile.Write(lockPath, lf); err != nil {
				w.PrintWarning(fmt.Sprintf("Failed to write lockfile: %v", err))
			}
		}
	}

	// Run go mod download in the app directory.
	if _, goModErr := os.Stat(goModPath); goModErr == nil {
		runGoModDownload(targetDir) //nolint:errcheck
	}

	s.Stop(fmt.Sprintf("App %q added", appName))

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"app":    appName,
			"source": resolvedSource,
			"path":   targetDir,
		})
	}

	w.PrintSuccess(fmt.Sprintf("App %q added to project", appName))
	w.Print("")
	w.Print("Next steps:")
	w.Print("  moca app install %s --site <site-name>", appName)
	w.Print("")

	return nil
}

// parseAppSource determines the source type (git, local) and extracts the app name.
// Returns (sourceType, resolvedSource, appName).
func parseAppSource(source string) (string, string, string) {
	// Local path: starts with ".", "/", or "~".
	if strings.HasPrefix(source, ".") || strings.HasPrefix(source, "/") || strings.HasPrefix(source, "~") {
		appName := filepath.Base(source)
		// Normalize hyphens to underscores.
		appName = strings.ReplaceAll(appName, "-", "_")
		return "local", source, appName
	}

	// Git URL: contains a domain-like pattern with "/" or ends with ".git".
	if strings.Contains(source, "/") || strings.HasSuffix(source, ".git") {
		// Extract app name from the URL path.
		name := filepath.Base(source)
		name = strings.TrimSuffix(name, ".git")
		name = strings.ReplaceAll(name, "-", "_")

		// Ensure it's a full URL — prepend https:// if missing.
		resolvedSource := source
		if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") && !strings.HasPrefix(source, "git@") {
			resolvedSource = "https://" + source
		}
		return "git", resolvedSource, name
	}

	// Registry name: treat as a shorthand for github.com/moca-apps/<name>.
	name := strings.ReplaceAll(source, "-", "_")
	return "git", "https://github.com/moca-apps/" + source, name
}

// gitCloneApp clones a git repository to the target directory.
func gitCloneApp(url, targetDir, branch, ref string, depth int) error {
	args := []string{"clone"}
	if depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", depth))
	}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, url, targetDir)

	gitCmd := exec.Command("git", args...)
	out, err := gitCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}

	// If a specific ref was requested, checkout that ref.
	if ref != "" {
		checkout := exec.Command("git", "checkout", ref)
		checkout.Dir = targetDir
		out, err := checkout.CombinedOutput()
		if err != nil {
			return fmt.Errorf("checkout %s: %s: %w", ref, strings.TrimSpace(string(out)), err)
		}
	}

	return nil
}

// copyLocalApp copies an app directory tree to the target location.
func copyLocalApp(src, dst string) error {
	src, err := filepath.Abs(src)
	if err != nil {
		return fmt.Errorf("resolve source path: %w", err)
	}

	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(dst, relPath)

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(targetPath, data, 0o644)
	})
}

// checkWorkspaceConflicts checks for Go module major version conflicts.
func checkWorkspaceConflicts(w *output.Writer, goModPath, appsDir string) []apps.GoModConflict {
	appMod, err := apps.ParseGoMod(goModPath)
	if err != nil {
		w.Debugf("Cannot parse %s: %v", goModPath, err)
		return nil
	}

	workspaceMods, err := apps.LoadWorkspaceMods(appsDir)
	if err != nil {
		w.Debugf("Cannot load workspace mods: %v", err)
		return nil
	}

	conflicts := apps.ValidateAppDependencies(appMod, workspaceMods)
	for _, c := range conflicts {
		w.PrintWarning(fmt.Sprintf("Major version conflict: %s requires %s %s, but %s requires %s",
			filepath.Base(goModPath), c.Package, c.NewVersion, c.App, c.OldVersion))
	}
	return conflicts
}

// runGoModDownload runs "go mod download" in the given directory.
func runGoModDownload(dir string) error {
	goCmd := exec.Command("go", "mod", "download")
	goCmd.Dir = dir
	out, err := goCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// app update
// ---------------------------------------------------------------------------

func newAppUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update [APP_NAME]",
		Short: "Update apps (all or specific)",
		Long:  "Update one or all apps, re-pulling from their configured source.",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runAppUpdate,
	}

	f := cmd.Flags()
	f.Bool("all", false, "Update all apps")
	f.Bool("dry-run", false, "Show what would be updated")
	f.Bool("force", false, "Force update even with local modifications")

	return cmd
}

func runAppUpdate(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	appsDir := filepath.Join(ctx.ProjectRoot, "apps")
	lockPath := filepath.Join(ctx.ProjectRoot, "moca.lock")
	allFlag, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Determine target apps.
	var targetApps []string
	if len(args) > 0 {
		targetApps = []string{args[0]}
	} else if allFlag {
		appInfos, scanErr := apps.ScanApps(appsDir)
		if scanErr != nil {
			return output.NewCLIError("Failed to scan apps").WithErr(scanErr)
		}
		for _, ai := range appInfos {
			targetApps = append(targetApps, ai.Name)
		}
	} else {
		return output.NewCLIError("No app specified").
			WithFix("Pass an app name or use --all to update all apps.")
	}

	// Read existing lockfile for comparison.
	var lf *lockfile.Lockfile
	if _, statErr := os.Stat(lockPath); statErr == nil {
		lf, err = lockfile.Read(lockPath)
		if err != nil {
			w.PrintWarning(fmt.Sprintf("Cannot read lockfile: %v", err))
			lf = nil
		}
	}

	// Build comparison table.
	headers := []string{"APP", "LOCKED", "CURRENT", "STATUS"}
	var rows [][]string
	var updatable []string

	for _, name := range targetApps {
		appDir := filepath.Join(appsDir, name)
		ai, loadErr := apps.LoadApp(appDir)
		if loadErr != nil {
			rows = append(rows, []string{name, "?", "?", "error: " + loadErr.Error()})
			continue
		}

		currentVersion := ai.Manifest.Version
		lockedVersion := "unlocked"
		status := "up to date"

		if lf != nil {
			if locked, ok := lf.Apps[name]; ok {
				lockedVersion = locked.Version
				if currentVersion != lockedVersion {
					status = "changed"
					updatable = append(updatable, name)
				}
			} else {
				status = "new (not in lockfile)"
				updatable = append(updatable, name)
			}
		} else {
			status = "no lockfile"
			updatable = append(updatable, name)
		}

		rows = append(rows, []string{name, lockedVersion, currentVersion, status})
	}

	if tableErr := w.PrintTable(headers, rows); tableErr != nil {
		return tableErr
	}

	if dryRun {
		return nil
	}

	if len(updatable) == 0 {
		w.PrintInfo("All apps are up to date.")
		return nil
	}

	// Re-resolve lockfile to capture current state.
	mocaVersion := ctx.Project.Moca
	if mocaVersion == "" {
		mocaVersion = "0.1.0"
	}
	newLF, err := lockfile.Resolve(appsDir, mocaVersion, lockfile.ResolveOptions{})
	if err != nil {
		return output.NewCLIError("Lockfile resolution failed").WithErr(err).WithCause(err.Error())
	}
	if err := lockfile.Write(lockPath, newLF); err != nil {
		return output.NewCLIError("Failed to write lockfile").WithErr(err).WithCause(err.Error())
	}

	w.PrintSuccess("Lockfile updated.")
	return nil
}

// ---------------------------------------------------------------------------
// app resolve
// ---------------------------------------------------------------------------

func newAppResolveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resolve",
		Short: "Resolve and lock dependency versions",
		Long:  "Scan all apps, validate dependencies, compute checksums, and regenerate moca.lock.",
		Args:  cobra.NoArgs,
		RunE:  runAppResolve,
	}

	f := cmd.Flags()
	f.Bool("dry-run", false, "Show resolution without writing lockfile")
	f.Bool("update", false, "Allow updating locked versions within constraints")
	f.Bool("strict", false, "Fail if any constraint cannot be satisfied")

	return cmd
}

func runAppResolve(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	appsDir := filepath.Join(ctx.ProjectRoot, "apps")
	lockPath := filepath.Join(ctx.ProjectRoot, "moca.lock")

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	update, _ := cmd.Flags().GetBool("update")
	strict, _ := cmd.Flags().GetBool("strict")

	mocaVersion := ctx.Project.Moca
	if mocaVersion == "" {
		mocaVersion = "0.1.0"
	}

	s := w.NewSpinner("Resolving app dependencies...")
	s.Start()

	lf, err := lockfile.Resolve(appsDir, mocaVersion, lockfile.ResolveOptions{
		Strict: strict,
		Update: update,
	})
	if err != nil {
		s.Stop("Failed")
		return output.NewCLIError("Dependency resolution failed").
			WithErr(err).
			WithCause(err.Error())
	}

	s.Stop(fmt.Sprintf("Resolved %d app(s)", len(lf.Apps)))

	if dryRun {
		// Print resolved lockfile as a table.
		headers := []string{"APP", "VERSION", "SOURCE", "CHECKSUM"}
		rows := make([][]string, 0, len(lf.Apps))
		for name, lock := range lf.Apps {
			checksum := lock.Checksum
			if len(checksum) > 20 {
				checksum = checksum[:20] + "..."
			}
			rows = append(rows, []string{name, lock.Version, lock.Source, checksum})
		}
		return w.PrintTable(headers, rows)
	}

	if err := lockfile.Write(lockPath, lf); err != nil {
		return output.NewCLIError("Failed to write lockfile").
			WithErr(err).
			WithCause(err.Error())
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"lockfile": lockPath,
			"apps":     len(lf.Apps),
		})
	}

	w.PrintSuccess(fmt.Sprintf("Lockfile written to %s (%d apps)", lockPath, len(lf.Apps)))
	return nil
}

// ---------------------------------------------------------------------------
// app diff
// ---------------------------------------------------------------------------

func newAppDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff APP_NAME",
		Short: "Show changes since last install",
		Long:  "Compare an app's current state against its locked version in moca.lock.",
		Args:  cobra.ExactArgs(1),
		RunE:  runAppDiff,
	}

	f := cmd.Flags()
	f.Bool("schema", false, "Show only schema/MetaType changes")
	f.Bool("hooks", false, "Show only hook changes")
	f.Bool("migrations", false, "Show only pending migrations")

	return cmd
}

func runAppDiff(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	appName := args[0]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	appsDir := filepath.Join(ctx.ProjectRoot, "apps")
	lockPath := filepath.Join(ctx.ProjectRoot, "moca.lock")

	schemaOnly, _ := cmd.Flags().GetBool("schema")
	hooksOnly, _ := cmd.Flags().GetBool("hooks")
	migrationsOnly, _ := cmd.Flags().GetBool("migrations")
	showAll := !schemaOnly && !hooksOnly && !migrationsOnly

	// Load current app.
	appDir := filepath.Join(appsDir, appName)
	ai, err := apps.LoadApp(appDir)
	if err != nil {
		return output.NewCLIError(fmt.Sprintf("Cannot load app %q", appName)).
			WithErr(err).
			WithCause(err.Error())
	}

	// Load lockfile.
	lf, err := lockfile.Read(lockPath)
	if err != nil {
		return output.NewCLIError("Cannot read lockfile").
			WithErr(err).
			WithCause(err.Error()).
			WithFix("Run 'moca app resolve' to generate a lockfile.")
	}

	locked, ok := lf.Apps[appName]
	if !ok {
		w.PrintWarning(fmt.Sprintf("App %q not found in lockfile — showing current state.", appName))
		w.Print("  Version: %s", ai.Manifest.Version)
		w.Print("  Modules: %d", len(ai.Manifest.Modules))
		return nil
	}

	type change struct {
		Category string
		Detail   string
	}
	var changes []change

	// Version comparison.
	if showAll || schemaOnly {
		if ai.Manifest.Version != locked.Version {
			changes = append(changes, change{
				Category: "version",
				Detail:   fmt.Sprintf("%s → %s", locked.Version, ai.Manifest.Version),
			})
		}
	}

	// Checksum comparison.
	if showAll {
		manifestPath := filepath.Join(ai.Path, "manifest.yaml")
		currentChecksum, err := lockfile.ComputeChecksum(manifestPath)
		if err == nil && locked.Checksum != "" && currentChecksum != locked.Checksum {
			changes = append(changes, change{
				Category: "manifest",
				Detail:   "manifest.yaml has been modified since last lock",
			})
		}
	}

	// Schema / DocType changes.
	if showAll || schemaOnly {
		for _, mod := range ai.Manifest.Modules {
			for _, dt := range mod.DocTypes {
				dtSnake := strings.ToLower(strings.ReplaceAll(dt, " ", "_"))
				modSnake := strings.ToLower(strings.ReplaceAll(mod.Name, " ", "_"))
				jsonPath := filepath.Join(appDir, "modules", modSnake, "doctypes", dtSnake, dtSnake+".json")
				if _, err := os.Stat(jsonPath); err == nil {
					changes = append(changes, change{
						Category: "schema",
						Detail:   fmt.Sprintf("DocType %s/%s present", mod.Name, dt),
					})
				}
			}
		}
	}

	// Migration changes.
	if showAll || migrationsOnly {
		migrationsDir := filepath.Join(appDir, "migrations")
		if entries, err := os.ReadDir(migrationsDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
					changes = append(changes, change{
						Category: "migration",
						Detail:   e.Name(),
					})
				}
			}
		}
	}

	// Hook changes.
	if showAll || hooksOnly {
		hooksPath := filepath.Join(appDir, "hooks.go")
		if _, err := os.Stat(hooksPath); err == nil {
			changes = append(changes, change{
				Category: "hooks",
				Detail:   "hooks.go present",
			})
		}
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"app":             appName,
			"locked_version":  locked.Version,
			"current_version": ai.Manifest.Version,
			"changes":         changes,
		})
	}

	if len(changes) == 0 {
		w.PrintSuccess(fmt.Sprintf("App %q matches locked version %s — no changes detected.", appName, locked.Version))
		return nil
	}

	w.Print("Changes in %s (locked: %s, current: %s):", appName, locked.Version, ai.Manifest.Version)
	w.Print("")

	headers := []string{"CATEGORY", "DETAIL"}
	rows := make([][]string, 0, len(changes))
	for _, c := range changes {
		rows = append(rows, []string{c.Category, c.Detail})
	}
	return w.PrintTable(headers, rows)
}

// appListInstalled lists apps installed on a specific site.
func appListInstalled(cmd *cobra.Command, ctx *clicontext.CLIContext, w *output.Writer, site string) error {
	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	installed, err := svc.Apps.ListInstalled(cmd.Context(), site)
	if err != nil {
		return output.NewCLIError("Failed to list installed apps").
			WithErr(err).
			WithCause(err.Error()).
			WithContext("site: " + site)
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(installed)
	}

	if len(installed) == 0 {
		w.Print("No apps installed on site %q", site)
		return nil
	}

	headers := []string{"APP", "VERSION", "INSTALLED"}
	rows := make([][]string, 0, len(installed))
	for _, a := range installed {
		rows = append(rows, []string{
			a.AppName,
			a.AppVersion,
			a.InstalledAt.Format("2006-01-02 15:04"),
		})
	}
	return w.PrintTable(headers, rows)
}

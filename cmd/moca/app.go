package main

import (
	"fmt"
	"path/filepath"

	clicontext "github.com/moca-framework/moca/internal/context"
	"github.com/moca-framework/moca/internal/output"
	"github.com/moca-framework/moca/pkg/apps"
	"github.com/spf13/cobra"
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
		// Remaining commands stay as placeholders (MS-13).
		newSubcommand("new", "Scaffold a new Moca app"),
		newSubcommand("get", "Download and install an app"),
		newSubcommand("remove", "Remove an app from project"),
		newSubcommand("update", "Update apps (all or specific)"),
		newSubcommand("resolve", "Resolve and lock dependency versions"),
		newSubcommand("publish", "Publish app to registry"),
		newSubcommand("info", "Show app manifest details"),
		newSubcommand("diff", "Show changes since last install"),
		newSubcommand("pin", "Pin app to exact version/commit"),
	)

	return cmd
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

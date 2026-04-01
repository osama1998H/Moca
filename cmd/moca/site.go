package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/moca-framework/moca/internal/output"
	"github.com/moca-framework/moca/pkg/tenancy"
	"github.com/spf13/cobra"
)

// NewSiteCommand returns the "moca site" command group with all subcommands.
func NewSiteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "site",
		Short: "Site management",
		Long:  "Create, manage, and maintain Moca sites (tenants).",
	}

	cmd.AddCommand(
		newSiteCreateCmd(),
		newSiteDropCmd(),
		newSiteListCmd(),
		newSiteUseCmd(),
		newSiteInfoCmd(),
		// Remaining commands stay as placeholders (MS-11).
		newSubcommand("browse", "Open site in browser"),
		newSubcommand("reinstall", "Reset site to fresh state"),
		newSubcommand("migrate", "Run pending migrations"),
		newSubcommand("enable", "Enable a disabled site"),
		newSubcommand("disable", "Disable a site (maintenance mode)"),
		newSubcommand("rename", "Rename a site"),
		newSubcommand("clone", "Clone a site (schema + data)"),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// site create
// ---------------------------------------------------------------------------

func newSiteCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create SITE_NAME",
		Short: "Create a new site",
		Long:  "Create a new tenant site with its own database schema, admin user, and initial configuration.",
		Args:  cobra.ExactArgs(1),
		RunE:  runSiteCreate,
	}

	f := cmd.Flags()
	f.String("admin-password", "", "Administrator password (prompted if not provided)")
	f.String("admin-email", "", "Administrator email (default: admin@SITE_NAME)")
	f.StringSlice("install-apps", nil, "Apps to install after site creation")
	f.String("timezone", "UTC", "Site timezone")
	f.String("language", "en", "Default language")
	f.String("currency", "USD", "Default currency")
	f.Bool("no-cache-warmup", false, "Skip initial cache warming")

	return cmd
}

func runSiteCreate(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	siteName := args[0]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	// Resolve admin password.
	adminPw, _ := cmd.Flags().GetString("admin-password")
	if adminPw == "" {
		adminPw, err = readPassword("Admin password: ")
		if err != nil {
			return err
		}
		if adminPw == "" {
			return output.NewCLIError("Admin password is required").
				WithFix("Pass --admin-password <password> or enter it when prompted.")
		}
		// Confirm password.
		confirm, confirmErr := readPassword("Confirm password: ")
		if confirmErr != nil {
			return confirmErr
		}
		if adminPw != confirm {
			return output.NewCLIError("Passwords do not match").
				WithFix("Try again with matching passwords.")
		}
	}

	// Resolve admin email.
	adminEmail, _ := cmd.Flags().GetString("admin-email")
	if adminEmail == "" {
		adminEmail = "admin@" + siteName
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	// Build config.
	timezone, _ := cmd.Flags().GetString("timezone")
	language, _ := cmd.Flags().GetString("language")
	currency, _ := cmd.Flags().GetString("currency")

	cfg := tenancy.SiteCreateConfig{
		Name:          siteName,
		AdminEmail:    adminEmail,
		AdminPassword: adminPw,
		Config: map[string]any{
			"timezone": timezone,
			"language": language,
			"currency": currency,
		},
	}

	s := w.NewSpinner(fmt.Sprintf("Creating site %q...", siteName))
	s.Start()
	if err := svc.Sites.CreateSite(cmd.Context(), cfg); err != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to create site").
			WithErr(err).
			WithCause(err.Error()).
			WithContext("site: " + siteName)
	}
	s.Stop(fmt.Sprintf("Site %q created", siteName))

	// Optionally install additional apps.
	installApps, _ := cmd.Flags().GetStringSlice("install-apps")
	appsDir := filepath.Join(ctx.ProjectRoot, "apps")
	for _, appName := range installApps {
		s = w.NewSpinner(fmt.Sprintf("Installing app %q...", appName))
		s.Start()
		if err := svc.Apps.Install(cmd.Context(), siteName, appName, appsDir); err != nil {
			s.Stop("Failed")
			w.PrintWarning(fmt.Sprintf("Failed to install app %q: %v", appName, err))
		} else {
			s.Stop(fmt.Sprintf("App %q installed", appName))
		}
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"site":        siteName,
			"admin_email": adminEmail,
			"status":      "created",
		})
	}

	w.PrintSuccess(fmt.Sprintf("Site %q is ready", siteName))
	w.Print("")
	w.Print("Next steps:")
	w.Print("  moca site use %s", siteName)
	w.Print("  moca app install <app-name> --site %s", siteName)
	return nil
}

// ---------------------------------------------------------------------------
// site drop
// ---------------------------------------------------------------------------

func newSiteDropCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "drop SITE_NAME",
		Short: "Delete a site",
		Long:  "Drop a site's database schema, remove Redis keys, and unregister from the system.",
		Args:  cobra.ExactArgs(1),
		RunE:  runSiteDrop,
	}

	f := cmd.Flags()
	f.Bool("force", false, "Skip confirmation prompt")
	f.Bool("no-backup", false, "Skip automatic backup before dropping")
	f.Bool("keep-database", false, "Don't drop the database schema")

	return cmd
}

func runSiteDrop(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	siteName := args[0]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	force, _ := cmd.Flags().GetBool("force")
	if !force {
		ok, confirmErr := confirmPrompt(fmt.Sprintf("Drop site %q? All data will be permanently deleted", siteName))
		if confirmErr != nil {
			return confirmErr
		}
		if !ok {
			w.Print("Aborted.")
			return nil
		}
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	s := w.NewSpinner(fmt.Sprintf("Dropping site %q...", siteName))
	s.Start()
	if err := svc.Sites.DropSite(cmd.Context(), siteName, tenancy.SiteDropOptions{Force: force}); err != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to drop site").
			WithErr(err).
			WithCause(err.Error()).
			WithContext("site: " + siteName)
	}
	s.Stop(fmt.Sprintf("Site %q dropped", siteName))

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"site":   siteName,
			"status": "dropped",
		})
	}

	w.PrintSuccess(fmt.Sprintf("Site %q has been removed", siteName))
	return nil
}

// ---------------------------------------------------------------------------
// site list
// ---------------------------------------------------------------------------

func newSiteListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all sites",
		Long:  "List all registered sites with status, installed apps, and database size.",
		RunE:  runSiteList,
	}

	f := cmd.Flags()
	f.String("status", "", "Filter by status (active, disabled)")

	return cmd
}

func runSiteList(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

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

	sites, err := svc.Sites.ListSites(cmd.Context())
	if err != nil {
		return output.NewCLIError("Failed to list sites").
			WithErr(err).
			WithCause(err.Error())
	}

	// Filter by status if requested.
	statusFilter, _ := cmd.Flags().GetString("status")
	if statusFilter != "" {
		var filtered []tenancy.SiteInfo
		for _, s := range sites {
			if s.Status == statusFilter {
				filtered = append(filtered, s)
			}
		}
		sites = filtered
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(sites)
	}

	if len(sites) == 0 {
		w.Print("No sites found.")
		w.Print("Create one with: moca site create <name> --admin-password <password>")
		return nil
	}

	headers := []string{"SITE", "STATUS", "APPS", "DB SIZE", "CREATED"}
	rows := make([][]string, 0, len(sites))
	for _, s := range sites {
		appsStr := strings.Join(s.Apps, ", ")
		if appsStr == "" {
			appsStr = "-"
		}
		rows = append(rows, []string{
			s.Name,
			s.Status,
			appsStr,
			formatBytes(s.DBSizeBytes),
			s.CreatedAt.Format(time.DateOnly),
		})
	}

	if err := w.PrintTable(headers, rows); err != nil {
		return err
	}

	// Show active site hint.
	if ctx.Site != "" {
		w.Print("")
		w.Print("Active site: %s", ctx.Site)
	}
	return nil
}

// ---------------------------------------------------------------------------
// site use
// ---------------------------------------------------------------------------

func newSiteUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use SITE_NAME",
		Short: "Set active site",
		Long:  "Set the active site for all subsequent commands. Stored in .moca/current_site.",
		Args:  cobra.ExactArgs(1),
		RunE:  runSiteUse,
	}
}

func runSiteUse(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	siteName := args[0]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	// Write .moca/current_site directly (no need for full service stack).
	dotMoca := filepath.Join(ctx.ProjectRoot, ".moca")
	if err := os.MkdirAll(dotMoca, 0o755); err != nil {
		return output.NewCLIError("Failed to create .moca directory").WithErr(err)
	}
	if err := os.WriteFile(filepath.Join(dotMoca, "current_site"), []byte(siteName), 0o644); err != nil {
		return output.NewCLIError("Failed to write current_site file").WithErr(err)
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]string{"active_site": siteName})
	}

	w.PrintSuccess(fmt.Sprintf("Active site set to %q", siteName))
	return nil
}

// ---------------------------------------------------------------------------
// site info
// ---------------------------------------------------------------------------

func newSiteInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info [SITE_NAME]",
		Short: "Show site details",
		Long:  "Display detailed information about a site including database size, installed apps, and configuration.",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runSiteInfo,
	}
	return cmd
}

func runSiteInfo(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	// Determine site name from arg or context.
	var siteName string
	if len(args) > 0 {
		siteName = args[0]
	} else {
		siteName, err = resolveSiteName(cmd, ctx)
		if err != nil {
			return err
		}
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	info, err := svc.Sites.GetSiteInfo(cmd.Context(), siteName)
	if err != nil {
		return output.NewCLIError("Failed to get site info").
			WithErr(err).
			WithCause(err.Error()).
			WithContext("site: " + siteName).
			WithFix("Run 'moca site list' to see available sites.")
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(info)
	}

	appsStr := strings.Join(info.Apps, ", ")
	if appsStr == "" {
		appsStr = "(none)"
	}

	w.Print("Site:            %s", info.Name)
	w.Print("Status:          %s", info.Status)
	w.Print("DB Schema:       %s", info.DBSchema)
	w.Print("DB Size:         %s", formatBytes(info.DBSizeBytes))
	w.Print("Admin Email:     %s", info.AdminEmail)
	w.Print("Installed Apps:  %s", appsStr)
	w.Print("Created:         %s", info.CreatedAt.Format(time.RFC3339))
	w.Print("Modified:        %s", info.ModifiedAt.Format(time.RFC3339))

	return nil
}

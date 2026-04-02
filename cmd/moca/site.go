package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/moca-framework/moca/internal/output"
	"github.com/moca-framework/moca/pkg/backup"
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
		newSiteBrowseCmd(),
		newSiteReinstallCmd(),
		newSubcommand("migrate", "Run pending migrations"),
		newSiteEnableCmd(),
		newSiteDisableCmd(),
		newSiteRenameCmd(),
		newSiteCloneCmd(),
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

// ---------------------------------------------------------------------------
// site enable
// ---------------------------------------------------------------------------

func newSiteEnableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enable [SITE_NAME]",
		Short: "Enable a disabled site",
		Long:  "Re-enable a site that was previously disabled (maintenance mode).",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runSiteEnable,
	}

	cmd.Flags().String("site", "", "Target site name")

	return cmd
}

func runSiteEnable(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

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

	s := w.NewSpinner(fmt.Sprintf("Enabling site %q...", siteName))
	s.Start()
	if enableErr := svc.Sites.EnableSite(cmd.Context(), siteName); enableErr != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to enable site").
			WithErr(enableErr).
			WithCause(enableErr.Error()).
			WithContext("site: " + siteName)
	}
	s.Stop(fmt.Sprintf("Site %q enabled", siteName))

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"site":   siteName,
			"status": "active",
		})
	}

	w.PrintSuccess(fmt.Sprintf("Site %q is now active", siteName))
	return nil
}

// ---------------------------------------------------------------------------
// site disable
// ---------------------------------------------------------------------------

func newSiteDisableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disable [SITE_NAME]",
		Short: "Disable a site (maintenance mode)",
		Long: `Put a site into maintenance mode. All requests will receive a
503 Service Unavailable response with a maintenance page.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runSiteDisable,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")
	f.String("message", "", "Custom maintenance message")
	f.StringSlice("allow", nil, "IP addresses to allow through during maintenance")

	return cmd
}

func runSiteDisable(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	var siteName string
	if len(args) > 0 {
		siteName = args[0]
	} else {
		siteName, err = resolveSiteName(cmd, ctx)
		if err != nil {
			return err
		}
	}

	message, _ := cmd.Flags().GetString("message")
	allowIPs, _ := cmd.Flags().GetStringSlice("allow")

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	s := w.NewSpinner(fmt.Sprintf("Disabling site %q...", siteName))
	s.Start()
	if disableErr := svc.Sites.DisableSite(cmd.Context(), siteName, message, allowIPs); disableErr != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to disable site").
			WithErr(disableErr).
			WithCause(disableErr.Error()).
			WithContext("site: " + siteName)
	}
	s.Stop(fmt.Sprintf("Site %q disabled", siteName))

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"site":    siteName,
			"status":  "disabled",
			"message": message,
		})
	}

	w.PrintSuccess(fmt.Sprintf("Site %q is now in maintenance mode", siteName))
	if message != "" {
		w.Print("  Message: %s", message)
	}
	if len(allowIPs) > 0 {
		w.Print("  Allowed IPs: %s", strings.Join(allowIPs, ", "))
	}
	return nil
}

// ---------------------------------------------------------------------------
// site rename
// ---------------------------------------------------------------------------

func newSiteRenameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename OLD_NAME NEW_NAME",
		Short: "Rename a site",
		Long: `Rename a site. Updates the database schema, system records,
Redis keys, and site directory.`,
		Args: cobra.ExactArgs(2),
		RunE: runSiteRename,
	}

	cmd.Flags().Bool("no-proxy-reload", false, "Skip reloading the reverse proxy")

	return cmd
}

func runSiteRename(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	oldName := args[0]
	newName := args[1]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	confirmed, promptErr := confirmPrompt(
		fmt.Sprintf("Rename site %q to %q? This will rename the database schema and site directory", oldName, newName),
	)
	if promptErr != nil {
		return promptErr
	}
	if !confirmed {
		w.Print("Aborted.")
		return nil
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	s := w.NewSpinner(fmt.Sprintf("Renaming %q to %q...", oldName, newName))
	s.Start()
	if renameErr := svc.Sites.RenameSite(cmd.Context(), oldName, newName, ctx.ProjectRoot); renameErr != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to rename site").
			WithErr(renameErr).
			WithCause(renameErr.Error()).
			WithContext(fmt.Sprintf("old: %s, new: %s", oldName, newName))
	}
	s.Stop(fmt.Sprintf("Site renamed to %q", newName))

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"old_name": oldName,
			"new_name": newName,
			"status":   "renamed",
		})
	}

	w.PrintSuccess(fmt.Sprintf("Site %q has been renamed to %q", oldName, newName))
	return nil
}

// ---------------------------------------------------------------------------
// site clone
// ---------------------------------------------------------------------------

func newSiteCloneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clone SOURCE_SITE NEW_SITE",
		Short: "Clone a site (schema + data)",
		Long: `Create a copy of an existing site including its database schema and data.
Optionally anonymize PII data for staging or testing use.`,
		Args: cobra.ExactArgs(2),
		RunE: runSiteClone,
	}

	f := cmd.Flags()
	f.Bool("anonymize", false, "Anonymize PII data in the clone (for staging/testing)")
	f.Bool("data-only", false, "Only copy data, not files/attachments")
	f.StringSlice("exclude", nil, "DocTypes to exclude from clone")

	return cmd
}

func runSiteClone(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	source := args[0]
	target := args[1]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	anonymize, _ := cmd.Flags().GetBool("anonymize")
	exclude, _ := cmd.Flags().GetStringSlice("exclude")

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	s := w.NewSpinner(fmt.Sprintf("Cloning %q to %q...", source, target))
	s.Start()
	if cloneErr := svc.Sites.CloneSite(cmd.Context(), source, target, tenancy.CloneOptions{
		Anonymize: anonymize,
		Exclude:   exclude,
	}); cloneErr != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to clone site").
			WithErr(cloneErr).
			WithCause(cloneErr.Error()).
			WithContext(fmt.Sprintf("source: %s, target: %s", source, target))
	}
	s.Stop(fmt.Sprintf("Site cloned to %q", target))

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"source":     source,
			"target":     target,
			"anonymized": anonymize,
			"status":     "cloned",
		})
	}

	w.PrintSuccess(fmt.Sprintf("Site %q cloned to %q", source, target))
	if anonymize {
		w.Print("  PII data has been anonymized in the clone.")
	}
	w.Print("")
	w.Print("Next steps:")
	w.Print("  moca site use %s", target)
	return nil
}

// ---------------------------------------------------------------------------
// site reinstall
// ---------------------------------------------------------------------------

func newSiteReinstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reinstall [SITE_NAME]",
		Short: "Reset site to fresh state",
		Long: `Completely reset a site by dropping all data and re-installing all
currently installed apps from scratch.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runSiteReinstall,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")
	f.String("admin-password", "", "New admin password (prompted if not provided)")
	f.Bool("force", false, "Skip confirmation prompt")
	f.Bool("no-backup", false, "Skip automatic backup before reinstall")

	return cmd
}

func runSiteReinstall(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	var siteName string
	if len(args) > 0 {
		siteName = args[0]
	} else {
		siteName, err = resolveSiteName(cmd, ctx)
		if err != nil {
			return err
		}
	}

	force, _ := cmd.Flags().GetBool("force")
	noBackup, _ := cmd.Flags().GetBool("no-backup")

	if !force {
		confirmed, promptErr := confirmPrompt(
			fmt.Sprintf("Reinstall site %q? All data will be permanently deleted and apps re-installed", siteName),
		)
		if promptErr != nil {
			return promptErr
		}
		if !confirmed {
			w.Print("Aborted.")
			return nil
		}
	}

	// Resolve admin password.
	adminPw, _ := cmd.Flags().GetString("admin-password")
	if adminPw == "" {
		adminPw, err = readPassword("New admin password: ")
		if err != nil {
			return err
		}
		if adminPw == "" {
			adminPw = "admin" // Default password on reinstall.
			w.PrintWarning("No password provided. Using default password 'admin'.")
		}
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	// Create pre-reinstall backup.
	var backupPath string
	if !noBackup {
		depErr := backup.CheckDependencies()
		if depErr != nil {
			w.PrintWarning("pg_dump not available — skipping pre-reinstall backup.")
		} else {
			bs := w.NewSpinner("Creating pre-reinstall backup...")
			bs.Start()
			info, bkErr := backup.Create(cmd.Context(), backup.CreateOptions{
				Site:        siteName,
				ProjectRoot: ctx.ProjectRoot,
				Compress:    true,
				DBConfig:    dbConnConfig(ctx.Project.Infrastructure.Database),
			})
			if bkErr != nil {
				bs.Stop("Backup failed")
				w.PrintWarning(fmt.Sprintf("Pre-reinstall backup failed: %v. Continuing without backup.", bkErr))
			} else {
				bs.Stop("Backup created")
				backupPath = info.Path
			}
		}
	}

	// Reinstall: drop + recreate.
	s := w.NewSpinner("Reinstalling site...")
	s.Start()
	previousApps, reinstallErr := svc.Sites.ReinstallSite(cmd.Context(), siteName, adminPw)
	if reinstallErr != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to reinstall site").
			WithErr(reinstallErr).
			WithCause(reinstallErr.Error()).
			WithContext("site: " + siteName)
	}
	s.Stop("Site reinstalled")

	// Re-install previously installed apps.
	appsDir := filepath.Join(ctx.ProjectRoot, "apps")
	var installedApps []string
	for _, appName := range previousApps {
		as := w.NewSpinner(fmt.Sprintf("Re-installing app %q...", appName))
		as.Start()
		if appErr := svc.Apps.Install(cmd.Context(), siteName, appName, appsDir); appErr != nil {
			as.Stop("Failed")
			w.PrintWarning(fmt.Sprintf("Failed to re-install app %q: %v", appName, appErr))
		} else {
			as.Stop(fmt.Sprintf("App %q installed", appName))
			installedApps = append(installedApps, appName)
		}
	}

	if w.Mode() == output.ModeJSON {
		result := map[string]any{
			"site":           siteName,
			"status":         "reinstalled",
			"apps_reinstalled": installedApps,
		}
		if backupPath != "" {
			result["backup_path"] = backupPath
		}
		return w.PrintJSON(result)
	}

	w.PrintSuccess(fmt.Sprintf("Site %q has been reinstalled", siteName))
	if len(installedApps) > 0 {
		w.Print("  Re-installed apps: %s", strings.Join(installedApps, ", "))
	}
	if backupPath != "" {
		w.Print("  Pre-reinstall backup: %s", backupPath)
	}
	return nil
}

// ---------------------------------------------------------------------------
// site browse
// ---------------------------------------------------------------------------

func newSiteBrowseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "browse [SITE_NAME]",
		Short: "Open site in browser",
		Long:  "Opens the site URL in the default browser.",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runSiteBrowse,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")
	f.String("user", "", "Login as a specific user (dev mode only)")
	f.Bool("print-url", false, "Print the URL instead of opening browser")

	return cmd
}

func runSiteBrowse(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	var siteName string
	if len(args) > 0 {
		siteName = args[0]
	} else {
		siteName, err = resolveSiteName(cmd, ctx)
		if err != nil {
			return err
		}
	}

	// Determine port from development config.
	port := ctx.Project.Development.Port
	if port == 0 {
		port = 8000
	}

	siteURL := fmt.Sprintf("http://%s:%d", siteName, port)

	// Warn about --user flag (requires auth module MS-14).
	if user, _ := cmd.Flags().GetString("user"); user != "" {
		w.PrintWarning("The --user flag requires the auth module (MS-14) which is not yet available.")
	}

	printURL, _ := cmd.Flags().GetBool("print-url")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]string{"url": siteURL})
	}

	if printURL {
		w.Print("%s", siteURL)
		return nil
	}

	// Open browser using platform-specific command.
	var openCmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		openCmd = exec.Command("open", siteURL)
	case "linux":
		openCmd = exec.Command("xdg-open", siteURL)
	case "windows":
		openCmd = exec.Command("cmd", "/c", "start", siteURL)
	default:
		w.Print("%s", siteURL)
		return nil
	}

	if startErr := openCmd.Start(); startErr != nil {
		return output.NewCLIError("Failed to open browser").
			WithErr(startErr).
			WithFix(fmt.Sprintf("Open %s manually in your browser.", siteURL))
	}

	w.PrintSuccess(fmt.Sprintf("Opened %s in browser", siteURL))
	return nil
}

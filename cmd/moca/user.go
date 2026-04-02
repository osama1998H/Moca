package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/moca-framework/moca/internal/output"
	"github.com/moca-framework/moca/pkg/auth"
	"github.com/moca-framework/moca/pkg/document"
	"github.com/moca-framework/moca/pkg/tenancy"
)

// NewUserCommand returns the "moca user" command group with all subcommands.
func NewUserCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "User management",
		Long:  "Create, manage, and configure user accounts and roles on a site.",
	}

	cmd.AddCommand(
		newUserAddCmd(),
		newUserRemoveCmd(),
		newUserSetPasswordCmd(),
		newUserSetAdminPasswordCmd(),
		newUserAddRoleCmd(),
		newUserRemoveRoleCmd(),
		newUserListCmd(),
		newUserDisableCmd(),
		newUserEnableCmd(),
		newUserImpersonateCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// cliAdminUser returns an auth.User representing the CLI administrator.
func cliAdminUser() *auth.User {
	return &auth.User{
		Email:    "Administrator",
		FullName: "Administrator",
		Roles:    []string{"System Manager"},
	}
}

// buildSiteContext creates a SiteContext for the given site name using the
// service's DB manager.
func buildSiteContext(cmd *cobra.Command, svc *Services, siteName string) (*tenancy.SiteContext, error) {
	pool, err := svc.DB.ForSite(cmd.Context(), siteName)
	if err != nil {
		return nil, output.NewCLIError(fmt.Sprintf("Cannot connect to site %q", siteName)).
			WithErr(err).
			WithFix("Ensure the site exists and the database is running.")
	}
	return &tenancy.SiteContext{
		Pool:        pool,
		Name:        siteName,
		DBSchema:    siteName,
		Status:      "active",
		RedisPrefix: siteName + ":",
	}, nil
}

// buildDocContext creates a DocContext for CLI operations on the given site.
func buildDocContext(cmd *cobra.Command, svc *Services, siteName string) (*document.DocContext, error) {
	site, err := buildSiteContext(cmd, svc, siteName)
	if err != nil {
		return nil, err
	}
	return document.NewDocContext(cmd.Context(), site, cliAdminUser()), nil
}

// setupUserCommand handles the common boilerplate: requireProject, resolveSiteName, newServices, buildDocContext.
// Returns (writer, docCtx, services, error). Caller must defer svc.Close().
func setupUserCommand(cmd *cobra.Command) (*output.Writer, *document.DocContext, *Services, error) {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return nil, nil, nil, err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return nil, nil, nil, err
	}

	docCtx, err := buildDocContext(cmd, svc, siteName)
	if err != nil {
		svc.Close()
		return nil, nil, nil, err
	}

	return w, docCtx, svc, nil
}

// extractChildRoles returns the list of role maps from a User document's "roles" children.
func extractChildRoles(doc *document.DynamicDoc) []map[string]any {
	children := doc.GetChild("roles")
	var roles []map[string]any
	for _, child := range children {
		role, _ := child.Get("role").(string)
		if role != "" {
			roles = append(roles, map[string]any{"role": role})
		}
	}
	return roles
}

// roleNames extracts just the role name strings from role maps.
func roleNames(roles []map[string]any) []string {
	names := make([]string, 0, len(roles))
	for _, r := range roles {
		if name, ok := r["role"].(string); ok {
			names = append(names, name)
		}
	}
	return names
}

// roleMapsFromStrings converts a slice of role name strings to HasRole child row maps.
func roleMapsFromStrings(roles []string) []any {
	result := make([]any, 0, len(roles))
	for _, r := range roles {
		r = strings.TrimSpace(r)
		if r != "" {
			result = append(result, map[string]any{"role": r})
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// user add
// ---------------------------------------------------------------------------

func newUserAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add EMAIL",
		Short: "Create a new user on a site",
		Long:  "Create a new user account with optional roles and password.",
		Args:  cobra.ExactArgs(1),
		RunE:  runUserAdd,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.String("first-name", "", "First name")
	f.String("last-name", "", "Last name")
	f.String("password", "", "Password (prompted if not provided)")
	f.StringSlice("roles", nil, "Roles to assign (comma-separated)")

	return cmd
}

func runUserAdd(cmd *cobra.Command, args []string) error {
	w, docCtx, svc, err := setupUserCommand(cmd)
	if err != nil {
		return err
	}
	defer svc.Close()

	email := args[0]

	firstName, _ := cmd.Flags().GetString("first-name")
	lastName, _ := cmd.Flags().GetString("last-name")
	password, _ := cmd.Flags().GetString("password")
	roles, _ := cmd.Flags().GetStringSlice("roles")

	if password == "" {
		password, err = readPassword("Password: ")
		if err != nil {
			return err
		}
		if password == "" {
			return output.NewCLIError("Password cannot be empty").
				WithFix("Provide a password via --password or interactive prompt.")
		}
	}

	fullName := strings.TrimSpace(firstName + " " + lastName)

	values := map[string]any{
		"email":    email,
		"password": password,
		"enabled":  1,
	}
	if fullName != "" {
		values["full_name"] = fullName
	}
	if len(roles) > 0 {
		values["roles"] = roleMapsFromStrings(roles)
	}

	s := w.NewSpinner(fmt.Sprintf("Creating user %q...", email))
	s.Start()

	doc, err := svc.DocManager.Insert(docCtx, "User", values)
	if err != nil {
		s.Stop("Failed")
		return output.NewCLIError(fmt.Sprintf("Failed to create user %q", email)).
			WithErr(err).
			WithCause(err.Error())
	}
	s.Stop(fmt.Sprintf("User %q created", email))

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"email":     email,
			"full_name": fullName,
			"roles":     roles,
			"name":      doc.Name(),
		})
	}

	w.PrintSuccess(fmt.Sprintf("User %q created successfully", email))
	if len(roles) > 0 {
		w.Print("  Roles: %s", strings.Join(roles, ", "))
	}
	return nil
}

// ---------------------------------------------------------------------------
// user remove
// ---------------------------------------------------------------------------

func newUserRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove EMAIL",
		Short: "Remove a user from a site",
		Long:  "Delete a user account from the specified site.",
		Args:  cobra.ExactArgs(1),
		RunE:  runUserRemove,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.Bool("force", false, "Skip confirmation")

	return cmd
}

func runUserRemove(cmd *cobra.Command, args []string) error {
	w, docCtx, svc, err := setupUserCommand(cmd)
	if err != nil {
		return err
	}
	defer svc.Close()

	email := args[0]
	force, _ := cmd.Flags().GetBool("force")

	if !force {
		ok, err := confirmPrompt(fmt.Sprintf("Remove user %q?", email))
		if err != nil {
			return err
		}
		if !ok {
			w.Print("Aborted.")
			return nil
		}
	}

	s := w.NewSpinner(fmt.Sprintf("Removing user %q...", email))
	s.Start()

	if err := svc.DocManager.Delete(docCtx, "User", email); err != nil {
		s.Stop("Failed")
		return output.NewCLIError(fmt.Sprintf("Failed to remove user %q", email)).
			WithErr(err).
			WithCause(err.Error())
	}
	s.Stop(fmt.Sprintf("User %q removed", email))

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"email": email, "removed": true})
	}

	w.PrintSuccess(fmt.Sprintf("User %q removed successfully", email))
	return nil
}

// ---------------------------------------------------------------------------
// user set-password
// ---------------------------------------------------------------------------

func newUserSetPasswordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-password EMAIL",
		Short: "Set user password",
		Long:  "Set or reset a user's password.",
		Args:  cobra.ExactArgs(1),
		RunE:  runUserSetPassword,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.String("password", "", "New password (prompted securely if not provided)")

	return cmd
}

func runUserSetPassword(cmd *cobra.Command, args []string) error {
	w, docCtx, svc, err := setupUserCommand(cmd)
	if err != nil {
		return err
	}
	defer svc.Close()

	email := args[0]
	password, _ := cmd.Flags().GetString("password")

	if password == "" {
		password, err = readPassword("New password: ")
		if err != nil {
			return err
		}
		if password == "" {
			return output.NewCLIError("Password cannot be empty").
				WithFix("Provide a password via --password or interactive prompt.")
		}
	}

	s := w.NewSpinner(fmt.Sprintf("Updating password for %q...", email))
	s.Start()

	_, err = svc.DocManager.Update(docCtx, "User", email, map[string]any{
		"password": password,
	})
	if err != nil {
		s.Stop("Failed")
		return output.NewCLIError(fmt.Sprintf("Failed to set password for %q", email)).
			WithErr(err).
			WithCause(err.Error())
	}
	s.Stop("Password updated")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"email": email, "password_updated": true})
	}

	w.PrintSuccess(fmt.Sprintf("Password updated for %q", email))
	return nil
}

// ---------------------------------------------------------------------------
// user set-admin-password
// ---------------------------------------------------------------------------

func newUserSetAdminPasswordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-admin-password",
		Short: "Set Administrator password",
		Long:  "Set the Administrator password for a site.",
		Args:  cobra.NoArgs,
		RunE:  runUserSetAdminPassword,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.String("password", "", "New password (prompted securely if not provided)")

	return cmd
}

func runUserSetAdminPassword(cmd *cobra.Command, args []string) error {
	w, docCtx, svc, err := setupUserCommand(cmd)
	if err != nil {
		return err
	}
	defer svc.Close()

	password, _ := cmd.Flags().GetString("password")

	if password == "" {
		password, err = readPassword("New Administrator password: ")
		if err != nil {
			return err
		}
		if password == "" {
			return output.NewCLIError("Password cannot be empty").
				WithFix("Provide a password via --password or interactive prompt.")
		}
	}

	s := w.NewSpinner("Updating Administrator password...")
	s.Start()

	_, err = svc.DocManager.Update(docCtx, "User", "Administrator", map[string]any{
		"password": password,
	})
	if err != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to set Administrator password").
			WithErr(err).
			WithCause(err.Error())
	}
	s.Stop("Administrator password updated")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"email": "Administrator", "password_updated": true})
	}

	w.PrintSuccess("Administrator password updated")
	return nil
}

// ---------------------------------------------------------------------------
// user add-role
// ---------------------------------------------------------------------------

func newUserAddRoleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-role EMAIL ROLE",
		Short: "Assign a role to a user",
		Long:  "Add a role assignment to a user account.",
		Args:  cobra.ExactArgs(2),
		RunE:  runUserAddRole,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")

	return cmd
}

func runUserAddRole(cmd *cobra.Command, args []string) error {
	w, docCtx, svc, err := setupUserCommand(cmd)
	if err != nil {
		return err
	}
	defer svc.Close()

	email := args[0]
	role := args[1]

	doc, err := svc.DocManager.Get(docCtx, "User", email)
	if err != nil {
		return output.NewCLIError(fmt.Sprintf("User %q not found", email)).
			WithErr(err).
			WithCause(err.Error())
	}

	existing := extractChildRoles(doc)
	for _, r := range existing {
		if r["role"] == role {
			if w.Mode() == output.ModeJSON {
				return w.PrintJSON(map[string]any{"email": email, "role": role, "already_assigned": true})
			}
			w.PrintWarning(fmt.Sprintf("User %q already has role %q", email, role))
			return nil
		}
	}

	existing = append(existing, map[string]any{"role": role})
	var rolesAny []any
	for _, r := range existing {
		rolesAny = append(rolesAny, r)
	}

	_, err = svc.DocManager.Update(docCtx, "User", email, map[string]any{
		"roles": rolesAny,
	})
	if err != nil {
		return output.NewCLIError(fmt.Sprintf("Failed to add role %q to user %q", role, email)).
			WithErr(err).
			WithCause(err.Error())
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"email": email, "role": role, "added": true})
	}

	w.PrintSuccess(fmt.Sprintf("Role %q added to user %q", role, email))
	return nil
}

// ---------------------------------------------------------------------------
// user remove-role
// ---------------------------------------------------------------------------

func newUserRemoveRoleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove-role EMAIL ROLE",
		Short: "Remove a role from a user",
		Long:  "Remove a role assignment from a user account.",
		Args:  cobra.ExactArgs(2),
		RunE:  runUserRemoveRole,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")

	return cmd
}

func runUserRemoveRole(cmd *cobra.Command, args []string) error {
	w, docCtx, svc, err := setupUserCommand(cmd)
	if err != nil {
		return err
	}
	defer svc.Close()

	email := args[0]
	role := args[1]

	doc, err := svc.DocManager.Get(docCtx, "User", email)
	if err != nil {
		return output.NewCLIError(fmt.Sprintf("User %q not found", email)).
			WithErr(err).
			WithCause(err.Error())
	}

	existing := extractChildRoles(doc)
	found := false
	var updated []any
	for _, r := range existing {
		if r["role"] == role {
			found = true
			continue
		}
		updated = append(updated, r)
	}

	if !found {
		if w.Mode() == output.ModeJSON {
			return w.PrintJSON(map[string]any{"email": email, "role": role, "not_found": true})
		}
		w.PrintWarning(fmt.Sprintf("User %q does not have role %q", email, role))
		return nil
	}

	_, err = svc.DocManager.Update(docCtx, "User", email, map[string]any{
		"roles": updated,
	})
	if err != nil {
		return output.NewCLIError(fmt.Sprintf("Failed to remove role %q from user %q", role, email)).
			WithErr(err).
			WithCause(err.Error())
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"email": email, "role": role, "removed": true})
	}

	w.PrintSuccess(fmt.Sprintf("Role %q removed from user %q", role, email))
	return nil
}

// ---------------------------------------------------------------------------
// user list
// ---------------------------------------------------------------------------

func newUserListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all users on a site",
		Long:  "List all user accounts on a site with optional filters.",
		Args:  cobra.NoArgs,
		RunE:  runUserList,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.String("role", "", "Filter by role")
	f.String("status", "active", `Filter: "active", "disabled", "all"`)

	return cmd
}

func runUserList(cmd *cobra.Command, args []string) error {
	w, docCtx, svc, err := setupUserCommand(cmd)
	if err != nil {
		return err
	}
	defer svc.Close()

	roleFilter, _ := cmd.Flags().GetString("role")
	status, _ := cmd.Flags().GetString("status")

	opts := document.ListOptions{
		Limit:   100,
		OrderBy: "creation",
	}

	switch status {
	case "active":
		opts.Filters = map[string]any{"enabled": 1}
	case "disabled":
		opts.Filters = map[string]any{"enabled": 0}
	case "all":
		// no filter
	default:
		return output.NewCLIError(fmt.Sprintf("Unknown status %q", status)).
			WithFix(`Use one of: "active", "disabled", "all"`)
	}

	docs, total, err := svc.DocManager.GetList(docCtx, "User", opts)
	if err != nil {
		return output.NewCLIError("Failed to list users").
			WithErr(err).
			WithCause(err.Error())
	}

	// Post-filter by role if specified (role is in child table, not filterable via GetList).
	if roleFilter != "" {
		var filtered []*document.DynamicDoc
		for _, doc := range docs {
			// GetList doesn't load children, so we need to load each user individually.
			full, err := svc.DocManager.Get(docCtx, "User", doc.Name())
			if err != nil {
				continue
			}
			roles := extractChildRoles(full)
			for _, r := range roles {
				if r["role"] == roleFilter {
					filtered = append(filtered, full)
					break
				}
			}
		}
		docs = filtered
		total = len(filtered)
	}

	if w.Mode() == output.ModeJSON {
		users := make([]map[string]any, 0, len(docs))
		for _, doc := range docs {
			u := map[string]any{
				"email":     doc.Get("email"),
				"full_name": doc.Get("full_name"),
				"enabled":   doc.Get("enabled"),
			}
			if roleFilter != "" {
				u["roles"] = roleNames(extractChildRoles(doc))
			}
			users = append(users, u)
		}
		return w.PrintJSON(map[string]any{"users": users, "total": total})
	}

	if len(docs) == 0 {
		w.Print("No users found.")
		return nil
	}

	verbose, _ := cmd.Flags().GetBool("verbose")

	headers := []string{"Email", "Full Name", "Enabled"}
	if verbose {
		headers = append(headers, "User Type", "Roles")
	}

	var rows [][]string
	for _, doc := range docs {
		enabled := "Yes"
		if e, ok := doc.Get("enabled").(int64); ok && e == 0 {
			enabled = "No"
		}
		row := []string{
			fmt.Sprintf("%v", doc.Get("email")),
			fmt.Sprintf("%v", doc.Get("full_name")),
			enabled,
		}
		if verbose {
			row = append(row, fmt.Sprintf("%v", doc.Get("user_type")))
			if roleFilter != "" {
				row = append(row, strings.Join(roleNames(extractChildRoles(doc)), ", "))
			} else {
				row = append(row, "")
			}
		}
		rows = append(rows, row)
	}

	w.Print("Users (%d):", total)
	return w.PrintTable(headers, rows)
}

// ---------------------------------------------------------------------------
// user disable
// ---------------------------------------------------------------------------

func newUserDisableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disable EMAIL",
		Short: "Disable a user account",
		Long:  "Disable a user account. The user will not be able to log in.",
		Args:  cobra.ExactArgs(1),
		RunE:  runUserDisable,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")

	return cmd
}

func runUserDisable(cmd *cobra.Command, args []string) error {
	w, docCtx, svc, err := setupUserCommand(cmd)
	if err != nil {
		return err
	}
	defer svc.Close()

	email := args[0]

	_, err = svc.DocManager.Update(docCtx, "User", email, map[string]any{
		"enabled": 0,
	})
	if err != nil {
		return output.NewCLIError(fmt.Sprintf("Failed to disable user %q", email)).
			WithErr(err).
			WithCause(err.Error())
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"email": email, "enabled": false})
	}

	w.PrintSuccess(fmt.Sprintf("User %q disabled", email))
	return nil
}

// ---------------------------------------------------------------------------
// user enable
// ---------------------------------------------------------------------------

func newUserEnableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enable EMAIL",
		Short: "Enable a user account",
		Long:  "Re-enable a previously disabled user account.",
		Args:  cobra.ExactArgs(1),
		RunE:  runUserEnable,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")

	return cmd
}

func runUserEnable(cmd *cobra.Command, args []string) error {
	w, docCtx, svc, err := setupUserCommand(cmd)
	if err != nil {
		return err
	}
	defer svc.Close()

	email := args[0]

	_, err = svc.DocManager.Update(docCtx, "User", email, map[string]any{
		"enabled": 1,
	})
	if err != nil {
		return output.NewCLIError(fmt.Sprintf("Failed to enable user %q", email)).
			WithErr(err).
			WithCause(err.Error())
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"email": email, "enabled": true})
	}

	w.PrintSuccess(fmt.Sprintf("User %q enabled", email))
	return nil
}

// ---------------------------------------------------------------------------
// user impersonate
// ---------------------------------------------------------------------------

func newUserImpersonateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "impersonate EMAIL",
		Short: "Generate login URL as any user (dev only)",
		Long:  "Generate a one-time login URL for any user. Requires Redis and is intended for development use only.",
		Args:  cobra.ExactArgs(1),
		RunE:  runUserImpersonate,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.String("ttl", "5m", "URL validity duration")

	return cmd
}

func runUserImpersonate(cmd *cobra.Command, args []string) error {
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

	email := args[0]
	ttlStr, _ := cmd.Flags().GetString("ttl")

	ttl, err := time.ParseDuration(ttlStr)
	if err != nil {
		return output.NewCLIError(fmt.Sprintf("Invalid TTL %q", ttlStr)).
			WithErr(err).
			WithFix("Use a valid Go duration like 5m, 1h, 30s.")
	}

	// Verify user exists.
	docCtx, err := buildDocContext(cmd, svc, siteName)
	if err != nil {
		return err
	}
	_, err = svc.DocManager.Get(docCtx, "User", email)
	if err != nil {
		return output.NewCLIError(fmt.Sprintf("User %q not found", email)).
			WithErr(err).
			WithCause(err.Error())
	}

	// Check Redis is available.
	if svc.Redis == nil || svc.Redis.Cache == nil {
		return output.NewCLIError("Redis is required for impersonation").
			WithFix("Ensure Redis is running and configured in moca.yaml.")
	}

	// Generate random token.
	tokenBytes := make([]byte, 32)
	if _, randErr := rand.Read(tokenBytes); randErr != nil {
		return fmt.Errorf("generate token: %w", randErr)
	}
	token := hex.EncodeToString(tokenBytes)

	// Store token in Redis.
	redisKey := siteName + ":impersonate:" + token
	err = svc.Redis.Cache.Set(cmd.Context(), redisKey, email, ttl).Err()
	if err != nil {
		return output.NewCLIError("Failed to store impersonation token").
			WithErr(err).
			WithCause(err.Error())
	}

	// Build URL.
	port := ctx.Project.Development.Port
	if port == 0 {
		port = 8000
	}
	loginURL := fmt.Sprintf("http://%s:%d/api/v1/method/login?token=%s", siteName, port, token)

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"email":     email,
			"token":     token,
			"url":       loginURL,
			"ttl":       ttlStr,
			"expires_at": time.Now().Add(ttl).Format(time.RFC3339),
		})
	}

	w.PrintSuccess(fmt.Sprintf("Impersonation URL for %q (valid for %s):", email, ttlStr))
	w.Print("")
	w.Print("  %s", loginURL)
	w.Print("")
	return nil
}

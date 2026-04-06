package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/pkg/api"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// NewAPICommand returns the "moca api" command group with all subcommands.
// Includes nested "keys" and "webhooks" subgroups.
func NewAPICommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api",
		Short: "API management",
		Long:  "List endpoints, test APIs, generate docs, and manage API keys and webhooks.",
	}

	// Nested: moca api keys {create,revoke,list,rotate}
	keys := &cobra.Command{
		Use:   "keys",
		Short: "Manage API keys",
	}
	keys.AddCommand(
		newAPIKeysCreateCmd(),
		newAPIKeysRevokeCmd(),
		newAPIKeysListCmd(),
		newAPIKeysRotateCmd(),
	)

	// Nested: moca api webhooks {list,test,logs}
	webhooks := &cobra.Command{
		Use:   "webhooks",
		Short: "Manage webhooks",
	}
	webhooks.AddCommand(
		newAPIWebhooksListCmd(),
		newAPIWebhooksTestCmd(),
		newAPIWebhooksLogsCmd(),
	)

	cmd.AddCommand(
		newAPIListCmd(),
		newAPITestCmd(),
		newAPIDocsCmd(),
		keys,
		webhooks,
	)

	return cmd
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// apiCommandContext bundles the common bootstrap result for API commands.
type apiCommandContext struct { //nolint:govet // field order matches logical grouping
	w      *output.Writer
	pool   *pgxpool.Pool
	schema string
	svc    *Services
}

// setupAPICommand performs the common bootstrap for API subcommands:
// project detection, site resolution, service construction, and DB pool.
// The caller must defer acc.svc.Close().
func setupAPICommand(cmd *cobra.Command) (*apiCommandContext, error) {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return nil, err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return nil, err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		svc.Close()
		return nil, err
	}

	pool, err := svc.DB.ForSite(cmd.Context(), siteName)
	if err != nil {
		svc.Close()
		return nil, output.NewCLIError("Cannot connect to site database").
			WithErr(err).
			WithFix(fmt.Sprintf("Ensure site '%s' exists. Run 'moca site list' to check.", siteName))
	}

	schema := tenancy.SchemaNameForSite(siteName)

	return &apiCommandContext{w: w, pool: pool, schema: schema, svc: svc}, nil
}

// parseExpiresDuration parses an expiry string into an absolute time.
// Supports "90d", "1y", "never", or standard Go durations (e.g. "24h").
// Returns nil for "never" or empty string (no expiry).
func parseExpiresDuration(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "never" {
		return nil, nil
	}

	var d time.Duration
	switch {
	case strings.HasSuffix(s, "y"):
		years, err := strconv.Atoi(strings.TrimSuffix(s, "y"))
		if err != nil {
			return nil, fmt.Errorf("invalid year duration %q: %w", s, err)
		}
		d = time.Duration(years) * 365 * 24 * time.Hour
	case strings.HasSuffix(s, "d"):
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return nil, fmt.Errorf("invalid day duration %q: %w", s, err)
		}
		d = time.Duration(days) * 24 * time.Hour
	default:
		var err error
		d, err = time.ParseDuration(s)
		if err != nil {
			return nil, fmt.Errorf("invalid duration %q: %w", s, err)
		}
	}

	t := time.Now().Add(d)
	return &t, nil
}

// parseScopesFlag converts CLI scope strings (e.g. "orders:read") into APIScopePerm slices.
func parseScopesFlag(scopes []string) []meta.APIScopePerm {
	var perms []meta.APIScopePerm
	for _, s := range scopes {
		perm := meta.APIScopePerm{Scope: s}
		if parts := strings.SplitN(s, ":", 2); len(parts) == 2 {
			perm.DocTypes = []string{parts[0]}
			perm.Operations = []string{parts[1]}
		}
		perms = append(perms, perm)
	}
	return perms
}

// formatScopesList returns a comma-separated string of scope names.
func formatScopesList(scopes []meta.APIScopePerm) string {
	if len(scopes) == 0 {
		return "-"
	}
	names := make([]string, len(scopes))
	for i, s := range scopes {
		names[i] = s.Scope
	}
	return strings.Join(names, ", ")
}

// formatRelativeTime converts a time to a human-friendly relative string.
func formatRelativeTime(t *time.Time) string {
	if t == nil {
		return "never"
	}
	return formatRelativeDuration(time.Since(*t))
}

// formatRelativeFuture converts a future time to a human-friendly relative string.
func formatRelativeFuture(t *time.Time) string {
	if t == nil {
		return "never"
	}
	d := time.Until(*t)
	if d < 0 {
		return "expired"
	}
	return formatRelativeDuration(d)
}

func formatRelativeDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	}
}

// timingStats holds computed timing statistics for repeated requests.
type timingStats struct {
	Min time.Duration
	Max time.Duration
	Avg time.Duration
	P95 time.Duration
}

// computeTimingStats calculates min, max, avg, and p95 from a slice of durations.
func computeTimingStats(durations []time.Duration) timingStats {
	if len(durations) == 0 {
		return timingStats{}
	}

	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var total time.Duration
	for _, d := range sorted {
		total += d
	}

	p95idx := int(math.Ceil(float64(len(sorted))*0.95)) - 1
	if p95idx < 0 {
		p95idx = 0
	}

	return timingStats{
		Min: sorted[0],
		Max: sorted[len(sorted)-1],
		Avg: total / time.Duration(len(sorted)),
		P95: sorted[p95idx],
	}
}

// newAPIKeyStore constructs an APIKeyStore suitable for CLI CRUD operations.
// loadUser is nil (only needed for Validate, not CRUD).
func newAPIKeyStore(svc *Services) *api.APIKeyStore {
	var redisClient = svc.Redis.Cache
	if svc.Redis == nil {
		redisClient = nil
	}
	return api.NewAPIKeyStore(nil, redisClient, svc.Logger)
}

// ---------------------------------------------------------------------------
// moca api keys create
// ---------------------------------------------------------------------------

func newAPIKeysCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new API key",
		Long: `Create a new API key for machine-to-machine authentication.
The secret is displayed only once — store it securely.`,
		RunE: runAPIKeysCreate,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.String("user", "", "Associated user (required)")
	f.String("label", "", "Human-readable label (required)")
	f.StringSlice("scopes", nil, "Permission scopes (e.g. orders:read,orders:write)")
	f.String("expires", "never", "Expiry (e.g. 90d, 1y, never)")
	f.StringSlice("ip-allow", nil, "IP allowlist (CIDR or plain IP)")
	f.Bool("json", false, "Output as JSON")

	_ = cmd.MarkFlagRequired("user")
	_ = cmd.MarkFlagRequired("label")

	return cmd
}

func runAPIKeysCreate(cmd *cobra.Command, _ []string) error {
	acc, err := setupAPICommand(cmd)
	if err != nil {
		return err
	}
	defer acc.svc.Close()

	user, _ := cmd.Flags().GetString("user")
	label, _ := cmd.Flags().GetString("label")
	scopes, _ := cmd.Flags().GetStringSlice("scopes")
	expiresStr, _ := cmd.Flags().GetString("expires")
	ipAllow, _ := cmd.Flags().GetStringSlice("ip-allow")

	expiresAt, err := parseExpiresDuration(expiresStr)
	if err != nil {
		return output.NewCLIError("Invalid --expires value").
			WithErr(err).
			WithFix("Use a duration like '90d', '1y', '24h', or 'never'.")
	}

	store := newAPIKeyStore(acc.svc)
	keySecret, err := store.Create(cmd.Context(), acc.pool, acc.schema, api.APIKeyCreateOpts{
		Label:       label,
		UserID:      user,
		Scopes:      parseScopesFlag(scopes),
		IPAllowlist: ipAllow,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		return output.NewCLIError("Failed to create API key").
			WithErr(err).
			WithCause(err.Error())
	}

	parts := strings.SplitN(keySecret, ":", 2)
	keyID := parts[0]
	secret := ""
	if len(parts) == 2 {
		secret = parts[1]
	}

	if acc.w.Mode() == output.ModeJSON {
		return acc.w.PrintJSON(map[string]any{
			"key_id":  keyID,
			"secret":  secret,
			"label":   label,
			"user":    user,
			"scopes":  scopes,
			"expires": expiresStr,
		})
	}

	acc.w.PrintSuccess("API key created")
	acc.w.Print("")
	acc.w.Print("  Key ID:  %s", keyID)
	acc.w.Print("  Secret:  %s", secret)
	acc.w.Print("  Token:   %s", keySecret)
	acc.w.Print("")
	acc.w.PrintWarning("Store the secret now — it will not be shown again.")

	return nil
}

// ---------------------------------------------------------------------------
// moca api keys revoke
// ---------------------------------------------------------------------------

func newAPIKeysRevokeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke KEY_ID",
		Short: "Revoke an API key",
		Long:  "Revoke an API key immediately. All requests using this key will be rejected.",
		Args:  cobra.ExactArgs(1),
		RunE:  runAPIKeysRevoke,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.Bool("force", false, "Skip confirmation prompt")

	return cmd
}

func runAPIKeysRevoke(cmd *cobra.Command, args []string) error {
	keyID := args[0]
	force, _ := cmd.Flags().GetBool("force")

	if !force {
		ok, err := confirmPrompt(fmt.Sprintf("Revoke API key %q?", keyID))
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
	}

	acc, err := setupAPICommand(cmd)
	if err != nil {
		return err
	}
	defer acc.svc.Close()

	store := newAPIKeyStore(acc.svc)
	if err := store.Revoke(cmd.Context(), acc.pool, acc.schema, keyID); err != nil {
		return output.NewCLIError("Failed to revoke API key").
			WithErr(err).
			WithCause(err.Error()).
			WithFix("Check that the key ID is correct. Run 'moca api keys list --status all' to see all keys.")
	}

	acc.w.PrintSuccess(fmt.Sprintf("API key %s revoked", keyID))
	return nil
}

// ---------------------------------------------------------------------------
// moca api keys list
// ---------------------------------------------------------------------------

func newAPIKeysListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all API keys",
		RunE:  runAPIKeysList,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.String("user", "", "Filter by associated user")
	f.String("status", "active", "Filter: active, revoked, expired, all")
	f.Bool("json", false, "Output as JSON")

	return cmd
}

func runAPIKeysList(cmd *cobra.Command, _ []string) error {
	acc, err := setupAPICommand(cmd)
	if err != nil {
		return err
	}
	defer acc.svc.Close()

	user, _ := cmd.Flags().GetString("user")
	status, _ := cmd.Flags().GetString("status")

	store := newAPIKeyStore(acc.svc)
	keys, err := store.List(cmd.Context(), acc.pool, acc.schema, api.APIKeyListFilter{
		UserID: user,
		Status: status,
	})
	if err != nil {
		return output.NewCLIError("Failed to list API keys").
			WithErr(err).
			WithCause(err.Error())
	}

	if acc.w.Mode() == output.ModeJSON {
		return acc.w.PrintJSON(keys)
	}

	if len(keys) == 0 {
		acc.w.PrintInfo("No API keys found.")
		return nil
	}

	headers := []string{"KEY ID", "LABEL", "USER", "SCOPES", "LAST USED", "EXPIRES"}
	rows := make([][]string, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, []string{
			k.KeyID,
			k.Label,
			k.UserID,
			formatScopesList(k.Scopes),
			formatRelativeTime(k.LastUsedAt),
			formatRelativeFuture(k.ExpiresAt),
		})
	}

	return acc.w.PrintTable(headers, rows)
}

// ---------------------------------------------------------------------------
// moca api keys rotate
// ---------------------------------------------------------------------------

func newAPIKeysRotateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rotate KEY_ID",
		Short: "Rotate an API key's secret",
		Long: `Generate a new secret for an existing API key.
The old secret is immediately invalidated unless --grace-period is set.`,
		Args: cobra.ExactArgs(1),
		RunE: runAPIKeysRotate,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.String("grace-period", "", "Keep old secret valid for duration (e.g. 1h, 7d)")
	f.Bool("json", false, "Output as JSON")

	return cmd
}

func runAPIKeysRotate(cmd *cobra.Command, args []string) error {
	keyID := args[0]

	acc, err := setupAPICommand(cmd)
	if err != nil {
		return err
	}
	defer acc.svc.Close()

	gracePeriodStr, _ := cmd.Flags().GetString("grace-period")
	var gracePeriod time.Duration
	if gracePeriodStr != "" {
		gracePeriod, err = parseDuration(gracePeriodStr)
		if err != nil {
			return output.NewCLIError("Invalid --grace-period value").
				WithErr(err).
				WithFix("Use a duration like '1h', '7d', or '30m'.")
		}
	}

	store := newAPIKeyStore(acc.svc)
	keySecret, err := store.Rotate(cmd.Context(), acc.pool, acc.schema, keyID, gracePeriod)
	if err != nil {
		return output.NewCLIError("Failed to rotate API key").
			WithErr(err).
			WithCause(err.Error()).
			WithFix("Check that the key ID is correct and active. Run 'moca api keys list' to verify.")
	}

	parts := strings.SplitN(keySecret, ":", 2)
	newKeyID := parts[0]
	newSecret := ""
	if len(parts) == 2 {
		newSecret = parts[1]
	}

	if acc.w.Mode() == output.ModeJSON {
		return acc.w.PrintJSON(map[string]any{
			"key_id": newKeyID,
			"secret": newSecret,
		})
	}

	acc.w.PrintSuccess(fmt.Sprintf("API key %s rotated", keyID))
	acc.w.Print("")
	acc.w.Print("  Key ID:  %s", newKeyID)
	acc.w.Print("  Secret:  %s", newSecret)
	acc.w.Print("  Token:   %s", keySecret)
	if gracePeriod > 0 {
		acc.w.Print("  Grace:   old secret valid for %s", gracePeriodStr)
	}
	acc.w.Print("")
	acc.w.PrintWarning("Store the new secret now — it will not be shown again.")

	return nil
}

// ---------------------------------------------------------------------------
// moca api webhooks list
// ---------------------------------------------------------------------------

// webhookInfo holds enumerated webhook information for display.
type webhookInfo struct {
	Name    string `json:"name"`
	DocType string `json:"doctype"`
	Event   string `json:"event"`
	URL     string `json:"url"`
	Status  string `json:"status"`
}

func newAPIWebhooksListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured webhooks",
		RunE:  runAPIWebhooksList,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.String("doctype", "", "Filter by DocType")
	f.Bool("json", false, "Output as JSON")

	return cmd
}

func runAPIWebhooksList(cmd *cobra.Command, _ []string) error {
	acc, err := setupAPICommand(cmd)
	if err != nil {
		return err
	}
	defer acc.svc.Close()

	doctypeFilter, _ := cmd.Flags().GetString("doctype")

	// Load all MetaTypes from tab_doctype.
	metatypes, err := loadAllMetaTypes(cmd, acc.pool)
	if err != nil {
		return err
	}

	var webhooks []webhookInfo
	for _, mt := range metatypes {
		if mt.APIConfig == nil || len(mt.APIConfig.Webhooks) == 0 {
			continue
		}
		if doctypeFilter != "" && !strings.EqualFold(mt.Name, doctypeFilter) {
			continue
		}
		for _, wh := range mt.APIConfig.Webhooks {
			name := mt.Name + ":" + wh.Event
			status := webhookStatus(cmd, acc.pool, wh.URL)
			webhooks = append(webhooks, webhookInfo{
				Name:    name,
				DocType: mt.Name,
				Event:   wh.Event,
				URL:     wh.URL,
				Status:  status,
			})
		}
	}

	if acc.w.Mode() == output.ModeJSON {
		return acc.w.PrintJSON(webhooks)
	}

	if len(webhooks) == 0 {
		acc.w.PrintInfo("No webhooks configured.")
		return nil
	}

	headers := []string{"NAME", "DOCTYPE", "EVENT", "URL", "STATUS"}
	rows := make([][]string, 0, len(webhooks))
	for _, wh := range webhooks {
		rows = append(rows, []string{wh.Name, wh.DocType, wh.Event, wh.URL, wh.Status})
	}

	return acc.w.PrintTable(headers, rows)
}

// webhookStatus determines if a webhook is active or failing by checking the most recent delivery log.
func webhookStatus(cmd *cobra.Command, pool *pgxpool.Pool, url string) string {
	var statusCode *int
	err := pool.QueryRow(cmd.Context(),
		`SELECT status_code FROM tab_webhook_log WHERE webhook_url = $1 ORDER BY created_at DESC LIMIT 1`,
		url,
	).Scan(&statusCode)
	if err != nil {
		return "active" // no logs yet
	}
	if statusCode != nil && *statusCode >= 200 && *statusCode < 300 {
		return "active"
	}
	return "failing"
}

// ---------------------------------------------------------------------------
// moca api webhooks test
// ---------------------------------------------------------------------------

func newAPIWebhooksTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test WEBHOOK_NAME",
		Short: "Send a test webhook",
		Long: `Send a test payload to a configured webhook endpoint.
WEBHOOK_NAME is in doctype:event format (e.g. SalesOrder:after_insert).`,
		Args: cobra.ExactArgs(1),
		RunE: runAPIWebhooksTest,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")

	return cmd
}

func runAPIWebhooksTest(cmd *cobra.Command, args []string) error {
	webhookName := args[0]

	acc, err := setupAPICommand(cmd)
	if err != nil {
		return err
	}
	defer acc.svc.Close()

	// Parse webhook name as doctype:event.
	parts := strings.SplitN(webhookName, ":", 2)
	if len(parts) != 2 {
		return output.NewCLIError("Invalid webhook name format").
			WithFix("Use doctype:event format, e.g. SalesOrder:after_insert")
	}
	doctype, event := parts[0], parts[1]

	// Find the webhook config from MetaType.
	metatypes, err := loadAllMetaTypes(cmd, acc.pool)
	if err != nil {
		return err
	}

	var found *meta.WebhookConfig
	for _, mt := range metatypes {
		if !strings.EqualFold(mt.Name, doctype) || mt.APIConfig == nil {
			continue
		}
		for i := range mt.APIConfig.Webhooks {
			if mt.APIConfig.Webhooks[i].Event == event {
				found = &mt.APIConfig.Webhooks[i]
				break
			}
		}
	}
	if found == nil {
		return output.NewCLIError(fmt.Sprintf("Webhook %q not found", webhookName)).
			WithFix("Run 'moca api webhooks list' to see configured webhooks.")
	}

	// Build test payload.
	payload := map[string]any{
		"event":         event,
		"doctype":       doctype,
		"document_name": "TEST-0001",
		"data":          map[string]any{"_test": true},
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
		"site":          "test",
	}
	payloadBytes, _ := json.Marshal(payload)

	// Sign and send.
	signature := api.SignPayload(payloadBytes, found.Secret)

	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, found.URL, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return output.NewCLIError("Failed to build request").WithErr(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Moca-Signature-256", signature)
	for k, v := range found.Headers {
		req.Header.Set(k, v)
	}

	s := acc.w.NewSpinner(fmt.Sprintf("Testing webhook %s...", webhookName))
	s.Start()

	client := &http.Client{Timeout: 30 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)

	s.Stop("")

	if err != nil {
		acc.w.PrintError(fmt.Sprintf("Request failed: %s", err.Error()))
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	acc.w.Print("  Status:   %s", resp.Status)
	acc.w.Print("  Time:     %s", elapsed.Round(time.Millisecond))
	acc.w.Print("  Size:     %d bytes", len(body))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		acc.w.PrintSuccess("Webhook test passed")
	} else {
		acc.w.PrintWarning("Webhook returned non-success status")
		if len(body) > 0 {
			acc.w.Print("")
			preview := string(body)
			if len(preview) > 500 {
				preview = preview[:500] + "..."
			}
			acc.w.Print("  Response: %s", preview)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// moca api webhooks logs
// ---------------------------------------------------------------------------

// webhookLogEntry holds a single delivery log row for display.
type webhookLogEntry struct { //nolint:govet // field order matches JSON API contract
	Timestamp    time.Time `json:"timestamp"`
	WebhookURL   string    `json:"webhook_url"`
	Event        string    `json:"event"`
	DocType      string    `json:"doctype"`
	DocumentName string    `json:"document_name"`
	StatusCode   *int      `json:"status_code"`
	DurationMs   *int      `json:"duration_ms"`
	Attempt      int       `json:"attempt"`
	ErrorMessage *string   `json:"error_message,omitempty"`
}

func newAPIWebhooksLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs [WEBHOOK_NAME]",
		Short: "Show webhook delivery logs",
		Long: `Show webhook delivery history and responses.
Optionally filter by webhook name (doctype:event format).`,
		Args: cobra.MaximumNArgs(1),
		RunE: runAPIWebhooksLogs,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.String("status", "all", "Filter: success, failed, all")
	f.Int("limit", 50, "Maximum entries to show")
	f.Bool("json", false, "Output as JSON")

	return cmd
}

func runAPIWebhooksLogs(cmd *cobra.Command, args []string) error {
	acc, err := setupAPICommand(cmd)
	if err != nil {
		return err
	}
	defer acc.svc.Close()

	statusFilter, _ := cmd.Flags().GetString("status")
	limit, _ := cmd.Flags().GetInt("limit")

	var conditions []string
	var queryArgs []any
	argIdx := 1

	// Filter by webhook name (doctype:event → filter by doctype).
	if len(args) > 0 {
		webhookName := args[0]
		if parts := strings.SplitN(webhookName, ":", 2); len(parts) == 2 {
			conditions = append(conditions, fmt.Sprintf("doctype = $%d", argIdx))
			queryArgs = append(queryArgs, parts[0])
			argIdx++
			conditions = append(conditions, fmt.Sprintf("webhook_event = $%d", argIdx))
			queryArgs = append(queryArgs, parts[1])
			argIdx++
		} else {
			conditions = append(conditions, fmt.Sprintf("doctype = $%d", argIdx))
			queryArgs = append(queryArgs, webhookName)
			argIdx++
		}
	}

	switch statusFilter {
	case "success":
		conditions = append(conditions, "status_code BETWEEN 200 AND 299")
	case "failed":
		conditions = append(conditions, "(status_code IS NULL OR status_code NOT BETWEEN 200 AND 299)")
	case "all":
		// no filter
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	query := fmt.Sprintf(
		`SELECT webhook_event, webhook_url, doctype, document_name, status_code,
		        duration_ms, attempt, error_message, created_at
		 FROM tab_webhook_log %s
		 ORDER BY created_at DESC LIMIT $%d`,
		where, argIdx,
	)
	queryArgs = append(queryArgs, limit)

	rows, err := acc.pool.Query(cmd.Context(), query, queryArgs...)
	if err != nil {
		return output.NewCLIError("Failed to query webhook logs").
			WithErr(err).
			WithCause(err.Error())
	}
	defer rows.Close()

	var logs []webhookLogEntry
	for rows.Next() {
		var entry webhookLogEntry
		if err := rows.Scan(
			&entry.Event, &entry.WebhookURL, &entry.DocType, &entry.DocumentName,
			&entry.StatusCode, &entry.DurationMs, &entry.Attempt, &entry.ErrorMessage,
			&entry.Timestamp,
		); err != nil {
			return fmt.Errorf("scan webhook log row: %w", err)
		}
		logs = append(logs, entry)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate webhook logs: %w", err)
	}

	if acc.w.Mode() == output.ModeJSON {
		return acc.w.PrintJSON(logs)
	}

	if len(logs) == 0 {
		acc.w.PrintInfo("No webhook delivery logs found.")
		return nil
	}

	headers := []string{"TIMESTAMP", "WEBHOOK", "EVENT", "STATUS", "RESPONSE", "DURATION"}
	tableRows := make([][]string, 0, len(logs))
	for _, entry := range logs {
		status := "pending"
		if entry.StatusCode != nil {
			sc := *entry.StatusCode
			if sc >= 200 && sc < 300 {
				status = fmt.Sprintf("%d OK", sc)
			} else {
				status = strconv.Itoa(sc)
			}
		} else if entry.ErrorMessage != nil {
			status = "error"
		}

		duration := "-"
		if entry.DurationMs != nil {
			duration = fmt.Sprintf("%dms", *entry.DurationMs)
		}

		response := "-"
		if entry.ErrorMessage != nil && *entry.ErrorMessage != "" {
			response = *entry.ErrorMessage
			if len(response) > 40 {
				response = response[:40] + "..."
			}
		}

		tableRows = append(tableRows, []string{
			entry.Timestamp.Format("2006-01-02 15:04:05"),
			entry.DocType + ":" + entry.Event,
			entry.Event,
			status,
			response,
			duration,
		})
	}

	return acc.w.PrintTable(headers, tableRows)
}

// ---------------------------------------------------------------------------
// moca api list
// ---------------------------------------------------------------------------

// endpointInfo holds enumerated endpoint information for display.
type endpointInfo struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Source string `json:"source"`
	Auth   string `json:"auth"`
}

func newAPIListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all registered API endpoints",
		Long:  "Enumerate all auto-generated CRUD and custom endpoints from MetaType definitions.",
		RunE:  runAPIList,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.String("doctype", "", "Filter by DocType")
	f.String("method", "", "Filter by HTTP method (GET, POST, PUT, DELETE)")
	f.Bool("json", false, "Output as JSON")

	return cmd
}

func runAPIList(cmd *cobra.Command, _ []string) error {
	acc, err := setupAPICommand(cmd)
	if err != nil {
		return err
	}
	defer acc.svc.Close()

	doctypeFilter, _ := cmd.Flags().GetString("doctype")
	methodFilter, _ := cmd.Flags().GetString("method")
	methodFilter = strings.ToUpper(methodFilter)

	metatypes, err := loadAllMetaTypes(cmd, acc.pool)
	if err != nil {
		return err
	}

	var endpoints []endpointInfo
	for _, mt := range metatypes {
		cfg := mt.APIConfig
		if cfg == nil || !cfg.Enabled {
			continue
		}
		if doctypeFilter != "" && !strings.EqualFold(mt.Name, doctypeFilter) {
			continue
		}

		auth := "session/jwt/key"

		// Meta endpoint (always available for enabled doctypes).
		endpoints = append(endpoints, endpointInfo{
			Method: "GET",
			Path:   fmt.Sprintf("/api/v1/meta/%s", mt.Name),
			Source: "meta",
			Auth:   auth,
		})

		// CRUD endpoints based on Allow* flags.
		if cfg.AllowList {
			endpoints = append(endpoints, endpointInfo{
				Method: "GET",
				Path:   fmt.Sprintf("/api/v1/resource/%s", mt.Name),
				Source: "auto",
				Auth:   auth,
			})
		}
		if cfg.AllowGet {
			endpoints = append(endpoints, endpointInfo{
				Method: "GET",
				Path:   fmt.Sprintf("/api/v1/resource/%s/{name}", mt.Name),
				Source: "auto",
				Auth:   auth,
			})
		}
		if cfg.AllowCreate && !mt.IsSingle {
			endpoints = append(endpoints, endpointInfo{
				Method: "POST",
				Path:   fmt.Sprintf("/api/v1/resource/%s", mt.Name),
				Source: "auto",
				Auth:   auth,
			})
		}
		if cfg.AllowUpdate {
			endpoints = append(endpoints, endpointInfo{
				Method: "PUT",
				Path:   fmt.Sprintf("/api/v1/resource/%s/{name}", mt.Name),
				Source: "auto",
				Auth:   auth,
			})
		}
		if cfg.AllowDelete && !mt.IsSingle {
			endpoints = append(endpoints, endpointInfo{
				Method: "DELETE",
				Path:   fmt.Sprintf("/api/v1/resource/%s/{name}", mt.Name),
				Source: "auto",
				Auth:   auth,
			})
		}

		// Custom endpoints.
		for _, ce := range cfg.CustomEndpoints {
			endpoints = append(endpoints, endpointInfo{
				Method: strings.ToUpper(ce.Method),
				Path:   fmt.Sprintf("/api/v1/custom/%s/%s", mt.Name, ce.Path),
				Source: "custom",
				Auth:   auth,
			})
		}
	}

	// Apply method filter.
	if methodFilter != "" {
		filtered := endpoints[:0]
		for _, ep := range endpoints {
			if ep.Method == methodFilter {
				filtered = append(filtered, ep)
			}
		}
		endpoints = filtered
	}

	if acc.w.Mode() == output.ModeJSON {
		return acc.w.PrintJSON(endpoints)
	}

	if len(endpoints) == 0 {
		acc.w.PrintInfo("No API endpoints found.")
		return nil
	}

	headers := []string{"METHOD", "PATH", "SOURCE", "AUTH"}
	rows := make([][]string, 0, len(endpoints))
	for _, ep := range endpoints {
		rows = append(rows, []string{ep.Method, ep.Path, ep.Source, ep.Auth})
	}

	acc.w.Print("Endpoints: %d", len(endpoints))
	acc.w.Print("")
	return acc.w.PrintTable(headers, rows)
}

// ---------------------------------------------------------------------------
// moca api test
// ---------------------------------------------------------------------------

func newAPITestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test ENDPOINT",
		Short: "Test an API endpoint",
		Long: `Make an HTTP request to a running moca-server and display the response.
The server URL is resolved from moca.yaml development config.`,
		Args:    cobra.ExactArgs(1),
		RunE:    runAPITest,
		Example: `  moca api test /api/v1/resource/SalesOrder --user admin
  moca api test /api/v1/resource/User --method POST --data '{"email":"test@example.com"}'
  moca api test /api/v1/resource/SalesOrder --repeat 10 --verbose`,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.String("method", "GET", "HTTP method")
	f.String("user", "Administrator", "Authenticate as user")
	f.String("api-key", "", "Authenticate with API key (key_id:secret)")
	f.String("data", "", "Request body (JSON)")
	f.Int("repeat", 0, "Repeat N times and show timing stats")
	f.Bool("verbose", false, "Show full request/response headers")
	f.Bool("json", false, "Output as JSON")

	return cmd
}

func runAPITest(cmd *cobra.Command, args []string) error {
	endpoint := args[0]
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	site, _ := cmd.Flags().GetString("site")
	method, _ := cmd.Flags().GetString("method")
	user, _ := cmd.Flags().GetString("user")
	apiKey, _ := cmd.Flags().GetString("api-key")
	data, _ := cmd.Flags().GetString("data")
	repeat, _ := cmd.Flags().GetInt("repeat")
	verbose, _ := cmd.Flags().GetBool("verbose")

	method = strings.ToUpper(method)
	fullURL := resolveRequestURL(endpoint, cliCtx)

	doRequest := func() (*http.Response, []byte, time.Duration, error) {
		var body io.Reader
		if data != "" {
			body = strings.NewReader(data)
		}

		req, reqErr := http.NewRequestWithContext(cmd.Context(), method, fullURL, body)
		if reqErr != nil {
			return nil, nil, 0, reqErr
		}

		// Set auth.
		if apiKey != "" {
			req.Header.Set("Authorization", "token "+apiKey)
		} else {
			req.Header.Set("X-Moca-Dev-User", user)
		}
		if site != "" {
			req.Header.Set("X-Moca-Site", site)
		}
		if data != "" {
			req.Header.Set("Content-Type", "application/json")
		}

		if verbose && repeat <= 1 {
			printRequestHeaders(w, req)
		}

		client := &http.Client{}
		start := time.Now()
		resp, doErr := client.Do(req)
		elapsed := time.Since(start)
		if doErr != nil {
			return nil, nil, elapsed, doErr
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return resp, nil, elapsed, readErr
		}

		return resp, respBody, elapsed, nil
	}

	// Repeated requests for timing stats.
	if repeat > 1 {
		var durations []time.Duration
		var lastStatus int
		var lastSize int

		s := w.NewSpinner(fmt.Sprintf("Running %d requests...", repeat))
		s.Start()

		for i := 0; i < repeat; i++ {
			resp, body, elapsed, reqErr := doRequest()
			if reqErr != nil {
				s.Stop("")
				return output.NewCLIError("Request failed").
					WithErr(reqErr).
					WithFix("Ensure moca-server is running (moca serve).")
			}
			durations = append(durations, elapsed)
			lastStatus = resp.StatusCode
			lastSize = len(body)
		}

		s.Stop("")

		stats := computeTimingStats(durations)

		if w.Mode() == output.ModeJSON {
			return w.PrintJSON(map[string]any{
				"status":   lastStatus,
				"size":     lastSize,
				"requests": repeat,
				"timing": map[string]string{
					"min": stats.Min.Round(time.Millisecond).String(),
					"max": stats.Max.Round(time.Millisecond).String(),
					"avg": stats.Avg.Round(time.Millisecond).String(),
					"p95": stats.P95.Round(time.Millisecond).String(),
				},
			})
		}

		w.Print("Completed %d requests to %s %s", repeat, method, endpoint)
		w.Print("")
		w.Print("  Status:  %d", lastStatus)
		w.Print("  Size:    %d bytes", lastSize)
		w.Print("  Min:     %s", stats.Min.Round(time.Millisecond))
		w.Print("  Max:     %s", stats.Max.Round(time.Millisecond))
		w.Print("  Avg:     %s", stats.Avg.Round(time.Millisecond))
		w.Print("  P95:     %s", stats.P95.Round(time.Millisecond))
		return nil
	}

	// Single request.
	resp, respBody, elapsed, err := doRequest()
	if err != nil {
		return output.NewCLIError("Request failed").
			WithErr(err).
			WithCause(err.Error()).
			WithFix("Ensure moca-server is running (moca serve).")
	}

	if w.Mode() == output.ModeJSON {
		return printResponseJSON(w, resp, respBody)
	}

	if verbose {
		printResponseHeaders(w, resp)
	}

	w.Print("%s %s  (%s, %d bytes)", resp.Proto, resp.Status,
		elapsed.Round(time.Millisecond), len(respBody))

	if len(respBody) > 0 {
		w.Print("")
		printPrettyBody(w, respBody)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Shared MetaType loader
// ---------------------------------------------------------------------------

// loadAllMetaTypes queries tab_doctype and returns all parsed MetaTypes.
func loadAllMetaTypes(cmd *cobra.Command, pool *pgxpool.Pool) ([]meta.MetaType, error) {
	rows, err := pool.Query(cmd.Context(), `SELECT name, definition FROM tab_doctype`)
	if err != nil {
		// Table may not exist yet.
		if isTableNotFound(err) {
			return nil, nil
		}
		return nil, output.NewCLIError("Failed to query MetaTypes").
			WithErr(err).
			WithCause(err.Error())
	}
	defer rows.Close()

	var metatypes []meta.MetaType
	for rows.Next() {
		var name string
		var definition json.RawMessage
		if err := rows.Scan(&name, &definition); err != nil {
			return nil, fmt.Errorf("scan metatype row: %w", err)
		}
		var mt meta.MetaType
		if err := json.Unmarshal(definition, &mt); err != nil {
			continue // skip unparseable definitions
		}
		metatypes = append(metatypes, mt)
	}
	return metatypes, rows.Err()
}

// isTableNotFound checks if a pgx error indicates a missing table.
func isTableNotFound(err error) bool {
	if err == nil {
		return false
	}
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "42P01" // undefined_table
	}
	return false
}

// ---------------------------------------------------------------------------
// moca api docs
// ---------------------------------------------------------------------------

func newAPIDocsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Generate OpenAPI/Swagger spec",
		Long: `Generate an OpenAPI 3.0 specification from MetaType and APIConfig definitions.
The spec includes all auto-generated CRUD endpoints, custom endpoints, and whitelisted methods.`,
		RunE: runAPIDocs,
		Example: `  moca api docs --site mysite
  moca api docs --format yaml --output openapi.yaml
  moca api docs --serve --port 8002`,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.String("output", "", "Output file path (default: stdout)")
	f.String("format", "json", "Output format: json or yaml")
	f.Bool("serve", false, "Start a Swagger UI server")
	f.Int("port", 8002, "Swagger UI server port (used with --serve)")

	return cmd
}

func runAPIDocs(cmd *cobra.Command, _ []string) error {
	acc, err := setupAPICommand(cmd)
	if err != nil {
		return err
	}
	defer acc.svc.Close()

	formatFlag, _ := cmd.Flags().GetString("format")
	outputFlag, _ := cmd.Flags().GetString("output")
	serve, _ := cmd.Flags().GetBool("serve")
	port, _ := cmd.Flags().GetInt("port")

	formatFlag = strings.ToLower(formatFlag)
	if formatFlag != "json" && formatFlag != "yaml" {
		return output.NewCLIError("Invalid format").
			WithFix("Use --format json or --format yaml.")
	}

	metatypes, err := loadAllMetaTypes(cmd, acc.pool)
	if err != nil {
		return err
	}

	// Collect whitelisted method names (methods are not stored in DB,
	// so we cannot discover them from the CLI context alone — include
	// an empty list; the spec will still contain all MetaType endpoints).
	var methods []string

	spec := api.GenerateSpec(metatypes, methods, api.SpecOptions{
		Title:       "Moca API",
		Description: "Auto-generated API documentation from MetaType definitions.",
		Version:     "1.0.0",
	})

	var data []byte
	switch formatFlag {
	case "yaml":
		data, err = yaml.Marshal(spec)
	default:
		data, err = json.MarshalIndent(spec, "", "  ")
	}
	if err != nil {
		return output.NewCLIError("Failed to marshal spec").WithErr(err)
	}

	if serve {
		return serveSwaggerUI(cmd, data, port)
	}

	if outputFlag != "" {
		if writeErr := os.WriteFile(outputFlag, data, 0o644); writeErr != nil {
			return output.NewCLIError("Failed to write output file").
				WithErr(writeErr).
				WithFix(fmt.Sprintf("Ensure the path '%s' is writable.", outputFlag))
		}
		acc.w.PrintSuccess(fmt.Sprintf("OpenAPI spec written to %s", outputFlag))
		return nil
	}

	// Write to stdout.
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return err
}

// serveSwaggerUI starts a local HTTP server serving the generated OpenAPI spec
// and a Swagger UI page using CDN-hosted assets.
func serveSwaggerUI(cmd *cobra.Command, specData []byte, port int) error {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, _ = w.Write(specData)
	})

	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, swaggerUIHTML)
	})

	addr := fmt.Sprintf(":%d", port)
	w := output.NewWriter(cmd)
	w.PrintSuccess(fmt.Sprintf("Swagger UI available at http://localhost%s", addr))
	w.Print("Spec served at http://localhost%s/openapi.json", addr)
	w.Print("Press Ctrl+C to stop.")

	srv := &http.Server{Addr: addr, Handler: mux}
	return srv.ListenAndServe()
}

// swaggerUIHTML is a minimal HTML page that loads Swagger UI from CDN.
const swaggerUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>Moca API - Swagger UI</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({ url: '/openapi.json', dom_id: '#swagger-ui' });
  </script>
</body>
</html>
`

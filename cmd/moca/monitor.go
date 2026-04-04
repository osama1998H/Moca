package main

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/osama1998H/moca/internal/output"
)

// NewMonitorCommand returns the "moca monitor" command group with all subcommands.
func NewMonitorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Monitoring",
		Long:  "Live dashboards, Prometheus metrics, and audit log queries.",
	}

	cmd.AddCommand(
		newMonitorLiveCmd(),
		newMonitorMetricsCmd(),
		newMonitorAuditCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// monitor live (deferred)
// ---------------------------------------------------------------------------

func newMonitorLiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "live",
		Short: "Live dashboard showing requests, workers, queues",
		Long:  "Launch an interactive TUI dashboard showing real-time metrics.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := output.NewWriter(cmd)
			w.PrintWarning("TUI dashboard is deferred and not yet implemented.")
			w.Print("")
			w.Print("Use these alternatives:")
			w.Print("  %s  Prometheus metrics snapshot", w.Color().Bold("moca monitor metrics"))
			w.Print("  %s  Continuously refresh queue status", w.Color().Bold("moca queue status --watch"))
			w.Print("  %s  Query audit trail", w.Color().Bold("moca monitor audit --site <name>"))
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// monitor metrics
// ---------------------------------------------------------------------------

func newMonitorMetricsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Dump current Prometheus metrics",
		Long: `Fetch Prometheus metrics from the running Moca server's /metrics endpoint.
Displays raw Prometheus text format by default.`,
		RunE: runMonitorMetrics,
	}

	f := cmd.Flags()
	f.Int("port", 8000, "Server port to query")

	return cmd
}

func runMonitorMetrics(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	port, _ := cmd.Flags().GetInt("port")
	url := fmt.Sprintf("http://localhost:%d/metrics", port)

	s := w.NewSpinner("Fetching metrics...")
	s.Start()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		s.Stop("Failed")
		return output.NewCLIError("Cannot reach Moca server").
			WithErr(err).
			WithFix(fmt.Sprintf("Ensure the server is running on port %d. Start with 'moca serve'.", port))
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to read metrics response").WithErr(err)
	}

	s.Stop("Metrics fetched")

	if resp.StatusCode != http.StatusOK {
		return output.NewCLIError(fmt.Sprintf("Server returned HTTP %d", resp.StatusCode)).
			WithFix("The /metrics endpoint may not be enabled. Check server configuration.")
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"endpoint":    url,
			"status_code": resp.StatusCode,
			"body":        string(body),
		})
	}

	w.Print("%s", string(body))
	return nil
}

// ---------------------------------------------------------------------------
// monitor audit
// ---------------------------------------------------------------------------

func newMonitorAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Query audit log",
		Long: `Query the audit log for a site. Displays recent audit trail entries
with optional filters for user, doctype, action, and time range.`,
		RunE: runMonitorAudit,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site (required)")
	f.String("user", "", "Filter by user")
	f.String("doctype", "", "Filter by DocType")
	f.String("action", "", "Filter by action (Create, Update, Submit, etc.)")
	f.String("since", "", "Time filter: duration (e.g., \"1h\", \"2d\") or timestamp (e.g., \"2026-01-01\")")
	f.Int("limit", 50, "Max results")

	return cmd
}

func runMonitorAudit(cmd *cobra.Command, _ []string) error {
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

	pool, poolErr := svc.DB.ForSite(cmd.Context(), siteName)
	if poolErr != nil {
		return output.NewCLIError("Cannot connect to site database").
			WithErr(poolErr).
			WithContext("site: " + siteName)
	}

	// Build query with filters.
	query := `SELECT timestamp, "user", action, doctype, docname FROM tab_audit_log`
	var conditions []string
	var args []any
	argIdx := 1

	if user, _ := cmd.Flags().GetString("user"); user != "" {
		conditions = append(conditions, fmt.Sprintf(`"user" = $%d`, argIdx))
		args = append(args, user)
		argIdx++
	}

	if doctype, _ := cmd.Flags().GetString("doctype"); doctype != "" {
		conditions = append(conditions, fmt.Sprintf("doctype = $%d", argIdx))
		args = append(args, doctype)
		argIdx++
	}

	if action, _ := cmd.Flags().GetString("action"); action != "" {
		conditions = append(conditions, fmt.Sprintf("action = $%d", argIdx))
		args = append(args, action)
		argIdx++
	}

	if since, _ := cmd.Flags().GetString("since"); since != "" {
		sinceTime, parseErr := parseSinceTime(since)
		if parseErr != nil {
			return output.NewCLIError(fmt.Sprintf("Invalid --since value: %q", since)).
				WithErr(parseErr).
				WithFix("Use a duration like \"1h\" or \"2d\", or a timestamp like \"2026-01-01\".")
		}
		conditions = append(conditions, fmt.Sprintf("timestamp >= $%d", argIdx))
		args = append(args, sinceTime)
		argIdx++
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY timestamp DESC"

	limit, _ := cmd.Flags().GetInt("limit")
	if limit <= 0 {
		limit = 50
	}
	query += fmt.Sprintf(" LIMIT $%d", argIdx)
	args = append(args, limit)

	s := w.NewSpinner("Querying audit log...")
	s.Start()

	rows, queryErr := pool.Query(cmd.Context(), query, args...)
	if queryErr != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to query audit log").
			WithErr(queryErr).
			WithCause(queryErr.Error()).
			WithContext("site: " + siteName)
	}
	defer rows.Close()

	type auditEntry struct {
		Timestamp string `json:"timestamp"`
		User      string `json:"user"`
		Action    string `json:"action"`
		DocType   string `json:"doctype"`
		DocName   string `json:"docname"`
	}
	var entries []auditEntry

	for rows.Next() {
		var ts time.Time
		var user, action, doctype, docname string
		if scanErr := rows.Scan(&ts, &user, &action, &doctype, &docname); scanErr != nil {
			s.Stop("Failed")
			return output.NewCLIError("Failed to scan audit entry").WithErr(scanErr)
		}
		entries = append(entries, auditEntry{
			Timestamp: ts.Format("2006-01-02 15:04:05"),
			User:      user,
			Action:    action,
			DocType:   doctype,
			DocName:   docname,
		})
	}
	if rows.Err() != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to iterate audit entries").WithErr(rows.Err())
	}

	s.Stop("Audit log retrieved")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"site":    siteName,
			"count":   len(entries),
			"entries": entries,
		})
	}

	if len(entries) == 0 {
		w.PrintInfo("No audit log entries found matching the filters.")
		return nil
	}

	headers := []string{"TIMESTAMP", "USER", "ACTION", "DOCTYPE", "DOCNAME"}
	var tableRows [][]string
	for _, e := range entries {
		tableRows = append(tableRows, []string{
			e.Timestamp,
			e.User,
			e.Action,
			e.DocType,
			e.DocName,
		})
	}

	if err := w.PrintTable(headers, tableRows); err != nil {
		return err
	}

	w.Print("")
	w.Print("%s", w.Color().Muted(fmt.Sprintf("Showing %d entr%s for site '%s'",
		len(entries), pluralY(len(entries)), siteName)))

	return nil
}

// parseSinceTime parses a --since value as either a Go duration relative to now,
// a day-based duration (e.g., "7d"), or an absolute timestamp.
func parseSinceTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)

	// Try day-based duration first (e.g., "7d").
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err == nil && days > 0 {
			return time.Now().Add(-time.Duration(days) * 24 * time.Hour), nil
		}
	}

	// Try standard Go duration (e.g., "1h", "30m").
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d), nil
	}

	// Try absolute timestamps.
	for _, layout := range []string{
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("cannot parse %q as duration or timestamp", s)
}

// pluralY returns "y" for count==1, "ies" otherwise (for "entry/entries").
func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

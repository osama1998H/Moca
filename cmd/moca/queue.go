package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"

	clicontext "github.com/osama1998H/moca/internal/context"
	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/pkg/queue"
)

// NewQueueCommand returns the "moca queue" command group with all subcommands.
// Includes nested "dead-letter" subgroup.
func NewQueueCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "Queue management",
		Long:  "Monitor queues, inspect jobs, and manage dead letter entries.",
	}

	// Nested: moca queue dead-letter {list,retry,purge}
	deadLetter := &cobra.Command{
		Use:   "dead-letter",
		Short: "Manage dead letter queue",
	}
	deadLetter.AddCommand(
		newQueueDLListCmd(),
		newQueueDLRetryCmd(),
		newQueueDLPurgeCmd(),
	)

	cmd.AddCommand(
		newQueueStatusCmd(),
		newQueueListCmd(),
		newQueueInspectCmd(),
		newQueueRetryCmd(),
		newQueuePurgeCmd(),
		deadLetter,
	)

	return cmd
}

// ---------------------------------------------------------------------------
// queue status
// ---------------------------------------------------------------------------

func newQueueStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show queue depths and worker status",
		Long: `Show pending message counts and consumer information for all queue types.
Use --watch to continuously refresh the display.`,
		RunE: runQueueStatus,
	}

	f := cmd.Flags()
	f.String("site", "", "Filter by site (default: all active sites)")
	f.Bool("watch", false, "Refresh continuously (every 2 seconds)")

	return cmd
}

func runQueueStatus(cmd *cobra.Command, _ []string) error {
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

	if svc.Redis == nil || svc.Redis.Queue == nil {
		return output.NewCLIError("Redis is not available").
			WithFix("Ensure Redis is running and configured in moca.yaml.")
	}

	sites, err := resolveQueueSites(cmd, ctx, svc)
	if err != nil {
		return err
	}

	watch, _ := cmd.Flags().GetBool("watch")
	if watch {
		// Set up signal handling for Ctrl+C.
		sigCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()

		for {
			// Clear screen.
			_, _ = fmt.Fprint(cmd.OutOrStdout(), "\033[H\033[2J")
			if err := printQueueStatus(cmd, w, svc, sites); err != nil {
				return err
			}
			w.Print("\n%s", w.Color().Muted("Refreshing every 2s. Press Ctrl+C to stop."))

			select {
			case <-sigCtx.Done():
				return nil
			case <-time.After(2 * time.Second):
			}
		}
	}

	return printQueueStatus(cmd, w, svc, sites)
}

func printQueueStatus(cmd *cobra.Command, w *output.Writer, svc *Services, sites []string) error {
	rdb := svc.Redis.Queue

	type queueRow struct {
		Site      string `json:"site"`
		Queue     string `json:"queue"`
		Pending   int64  `json:"pending"`
		Consumers int64  `json:"consumers"`
		PELCount  int64  `json:"pel_count"`
	}

	var rows []queueRow
	var dlqTotal int64

	for _, site := range sites {
		for _, qt := range queue.AllQueueTypes {
			stream := queue.StreamKey(site, qt)

			pending, err := rdb.XLen(cmd.Context(), stream).Result()
			if err != nil {
				pending = 0
			}

			var consumers int64
			var pelCount int64
			groups, err := rdb.XInfoGroups(cmd.Context(), stream).Result()
			if err == nil {
				for _, g := range groups {
					consumers += int64(g.Consumers)
					pelCount += g.Pending
				}
			}

			rows = append(rows, queueRow{
				Site:      site,
				Queue:     string(qt),
				Pending:   pending,
				Consumers: consumers,
				PELCount:  pelCount,
			})
		}

		// DLQ count.
		dlqLen, err := rdb.XLen(cmd.Context(), queue.DLQKey(site)).Result()
		if err == nil {
			dlqTotal += dlqLen
		}
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"queues":      rows,
			"dead_letter": dlqTotal,
		})
	}

	headers := []string{"SITE", "QUEUE", "PENDING", "IN-FLIGHT", "CONSUMERS"}
	var tableRows [][]string
	for _, r := range rows {
		tableRows = append(tableRows, []string{
			r.Site,
			r.Queue,
			strconv.FormatInt(r.Pending, 10),
			strconv.FormatInt(r.PELCount, 10),
			strconv.FormatInt(r.Consumers, 10),
		})
	}
	if err := w.PrintTable(headers, tableRows); err != nil {
		return err
	}

	w.Print("")
	if dlqTotal > 0 {
		w.PrintWarning(fmt.Sprintf("Dead letter: %d item(s)", dlqTotal))
	} else {
		w.Print("  %s Dead letter: empty", w.Color().Muted("-"))
	}

	return nil
}

// resolveQueueSites returns sites based on --site flag or all active sites.
func resolveQueueSites(cmd *cobra.Command, ctx *cliCtx, svc *Services) ([]string, error) {
	if site, _ := cmd.Flags().GetString("site"); site != "" {
		return []string{site}, nil
	}
	sites, err := listActiveSites(cmd.Context(), svc)
	if err != nil {
		return nil, output.NewCLIError("Failed to list active sites").
			WithErr(err).
			WithFix("Pass --site <name> explicitly.")
	}
	if len(sites) == 0 {
		return nil, output.NewCLIError("No active sites found").
			WithFix("Create a site with 'moca site create' first.")
	}
	return sites, nil
}

// ---------------------------------------------------------------------------
// queue list
// ---------------------------------------------------------------------------

func newQueueListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pending/active/failed jobs",
		Long: `List jobs in a queue with their status and details.
By default lists pending jobs across all queue types.`,
		RunE: runQueueList,
	}

	f := cmd.Flags()
	f.String("queue", "all", `Queue name: "default", "long", "critical", "scheduler", "all"`)
	f.String("site", "", "Filter by site")
	f.Int64("limit", 50, "Max results")

	return cmd
}

func runQueueList(cmd *cobra.Command, _ []string) error {
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

	if svc.Redis == nil || svc.Redis.Queue == nil {
		return output.NewCLIError("Redis is not available").
			WithFix("Ensure Redis is running and configured in moca.yaml.")
	}

	sites, err := resolveQueueSites(cmd, ctx, svc)
	if err != nil {
		return err
	}

	queueFlag, _ := cmd.Flags().GetString("queue")
	limit, _ := cmd.Flags().GetInt64("limit")

	queueTypes := resolveQueueTypes(queueFlag)

	rdb := svc.Redis.Queue

	type jobEntry struct {
		Job      *queue.Job     `json:"job,omitempty"`
		Raw      map[string]any `json:"raw,omitempty"`
		StreamID string         `json:"stream_id"`
		Queue    string         `json:"queue"`
		Type     string         `json:"type"`
		Site     string         `json:"site"`
		Age      string         `json:"age"`
	}

	var entries []jobEntry
	var remaining = limit

	for _, site := range sites {
		if remaining <= 0 {
			break
		}
		for _, qt := range queueTypes {
			if remaining <= 0 {
				break
			}
			stream := queue.StreamKey(site, qt)
			msgs, err := rdb.XRange(cmd.Context(), stream, "-", "+").Result()
			if err != nil {
				continue
			}

			for _, msg := range msgs {
				if remaining <= 0 {
					break
				}
				j, jErr := queue.ValuesToJob(msg.Values)
				entry := jobEntry{
					StreamID: msg.ID,
					Queue:    string(qt),
				}
				if jErr == nil {
					entry.Type = j.Type
					entry.Site = j.Site
					entry.Age = time.Since(j.CreatedAt).Truncate(time.Second).String()
					entry.Job = &j
				} else {
					entry.Type = stringFromValues(msg.Values, "type")
					entry.Site = stringFromValues(msg.Values, "site")
					entry.Age = "?"
					entry.Raw = msg.Values
				}
				entries = append(entries, entry)
				remaining--
			}
		}
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"jobs":  entries,
			"count": len(entries),
		})
	}

	if len(entries) == 0 {
		w.PrintInfo("No jobs found.")
		return nil
	}

	headers := []string{"ID", "QUEUE", "TYPE", "SITE", "AGE"}
	var rows [][]string
	for _, e := range entries {
		rows = append(rows, []string{e.StreamID, e.Queue, e.Type, e.Site, e.Age})
	}
	return w.PrintTable(headers, rows)
}

// ---------------------------------------------------------------------------
// queue inspect
// ---------------------------------------------------------------------------

func newQueueInspectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect JOB_ID",
		Short: "Inspect a specific job's payload/history",
		Long:  `Show detailed information about a specific job by its stream entry ID.`,
		Args:  cobra.ExactArgs(1),
		RunE:  runQueueInspect,
	}

	f := cmd.Flags()
	f.String("site", "", "Limit search to a specific site")

	return cmd
}

func runQueueInspect(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	jobID := args[0]

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

	if svc.Redis == nil || svc.Redis.Queue == nil {
		return output.NewCLIError("Redis is not available").
			WithFix("Ensure Redis is running and configured in moca.yaml.")
	}

	sites, err := resolveQueueSites(cmd, ctx, svc)
	if err != nil {
		return err
	}

	rdb := svc.Redis.Queue

	// Search all streams for this message ID.
	for _, site := range sites {
		for _, qt := range queue.AllQueueTypes {
			stream := queue.StreamKey(site, qt)
			msgs, err := rdb.XRangeN(cmd.Context(), stream, jobID, jobID, 1).Result()
			if err != nil || len(msgs) == 0 {
				continue
			}

			msg := msgs[0]
			j, jErr := queue.ValuesToJob(msg.Values)

			if w.Mode() == output.ModeJSON {
				result := map[string]any{
					"stream_id": msg.ID,
					"stream":    stream,
					"queue":     string(qt),
					"site":      site,
					"values":    msg.Values,
				}
				if jErr == nil {
					result["job"] = j
				}
				return w.PrintJSON(result)
			}

			w.Print("  %s  %s", w.Color().Bold("Job ID:"), msg.ID)
			w.Print("  %s  %s", w.Color().Bold("Stream:"), stream)
			w.Print("  %s   %s", w.Color().Bold("Queue:"), string(qt))
			if jErr == nil {
				w.Print("  %s    %s", w.Color().Bold("Type:"), j.Type)
				w.Print("  %s    %s", w.Color().Bold("Site:"), j.Site)
				w.Print("  %s %s", w.Color().Bold("Created:"), j.CreatedAt.Format(time.RFC3339))
				w.Print("  %s %s/%d", w.Color().Bold("Retries:"), strconv.Itoa(j.Retries), j.MaxRetries)
				w.Print("  %s %s", w.Color().Bold("Timeout:"), j.Timeout)
				if j.Payload != nil {
					payloadJSON, _ := json.MarshalIndent(j.Payload, "           ", "  ")
					w.Print("  %s\n           %s", w.Color().Bold("Payload:"), string(payloadJSON))
				}
			} else {
				w.Print("  %s", w.Color().Bold("Raw Values:"))
				for k, v := range msg.Values {
					w.Print("    %s: %v", k, v)
				}
			}
			return nil
		}

		// Also check DLQ.
		dlqStream := queue.DLQKey(site)
		msgs, err := rdb.XRangeN(cmd.Context(), dlqStream, jobID, jobID, 1).Result()
		if err == nil && len(msgs) > 0 {
			msg := msgs[0]
			if w.Mode() == output.ModeJSON {
				return w.PrintJSON(map[string]any{
					"stream_id": msg.ID,
					"stream":    dlqStream,
					"queue":     "dead-letter",
					"values":    msg.Values,
				})
			}

			w.Print("  %s  %s", w.Color().Bold("Job ID:"), msg.ID)
			w.Print("  %s  %s", w.Color().Bold("Stream:"), dlqStream)
			w.Print("  %s   %s", w.Color().Bold("Queue:"), "dead-letter")
			for k, v := range msg.Values {
				w.Print("    %s: %v", k, v)
			}
			return nil
		}
	}

	return output.NewCLIError(fmt.Sprintf("Job %q not found", jobID)).
		WithFix("Check the job ID and try again. Use 'moca queue list' to find valid job IDs.")
}

// ---------------------------------------------------------------------------
// queue retry
// ---------------------------------------------------------------------------

func newQueueRetryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "retry JOB_ID",
		Short: "Retry a failed job",
		Long: `Retry a failed job by re-enqueuing it to its original queue.
Use --all-failed to retry all failed jobs in a specific queue.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runQueueRetry,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.Bool("all-failed", false, "Retry all failed jobs")
	f.String("queue", "", "Queue for --all-failed (default/long/critical/scheduler)")
	f.Bool("force", false, "Skip confirmation")

	return cmd
}

func runQueueRetry(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)

	allFailed, _ := cmd.Flags().GetBool("all-failed")
	if !allFailed && len(args) == 0 {
		return output.NewCLIError("JOB_ID required").
			WithFix("Provide a job ID or use --all-failed.")
	}

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

	if svc.Redis == nil || svc.Redis.Queue == nil {
		return output.NewCLIError("Redis is not available").
			WithFix("Ensure Redis is running and configured in moca.yaml.")
	}

	rdb := svc.Redis.Queue
	producer := newQueueProducer(svc)

	if allFailed {
		return retryAllFailed(cmd, w, rdb, producer, siteName)
	}

	jobID := args[0]
	return retrySingleJob(cmd, w, rdb, producer, siteName, jobID)
}

func retrySingleJob(cmd *cobra.Command, w *output.Writer, rdb *redis.Client, producer *queue.Producer, site, jobID string) error {
	// Search all streams for the job in the PEL.
	group := queue.GroupName(site)

	for _, qt := range queue.AllQueueTypes {
		stream := queue.StreamKey(site, qt)

		// Try to read the message directly.
		msgs, err := rdb.XRangeN(cmd.Context(), stream, jobID, jobID, 1).Result()
		if err != nil || len(msgs) == 0 {
			continue
		}

		msg := msgs[0]
		j, jErr := queue.ValuesToJob(msg.Values)
		if jErr != nil {
			return output.NewCLIError("Failed to parse job data").WithErr(jErr)
		}

		// Reset retries and re-enqueue.
		j.Retries = 0
		newID, err := producer.Enqueue(cmd.Context(), site, qt, j)
		if err != nil {
			return output.NewCLIError("Failed to re-enqueue job").WithErr(err)
		}

		// Acknowledge the original message to remove from PEL.
		_ = rdb.XAck(cmd.Context(), stream, group, jobID).Err()

		if w.Mode() == output.ModeJSON {
			return w.PrintJSON(map[string]any{
				"retried":     jobID,
				"new_id":      newID,
				"queue":       string(qt),
				"site":        site,
			})
		}

		w.PrintSuccess(fmt.Sprintf("Retried job %s → new ID: %s", jobID, newID))
		return nil
	}

	return output.NewCLIError(fmt.Sprintf("Job %q not found in any queue for site %q", jobID, site)).
		WithFix("Check the job ID with 'moca queue list'.")
}

func retryAllFailed(cmd *cobra.Command, w *output.Writer, rdb *redis.Client, producer *queue.Producer, site string) error {
	queueFlag, _ := cmd.Flags().GetString("queue")
	force, _ := cmd.Flags().GetBool("force")

	queueTypes := resolveQueueTypes(queueFlag)
	if len(queueTypes) == 0 {
		queueTypes = queue.AllQueueTypes
	}

	group := queue.GroupName(site)
	var totalRetried int

	for _, qt := range queueTypes {
		stream := queue.StreamKey(site, qt)

		pending, err := rdb.XPendingExt(cmd.Context(), &redis.XPendingExtArgs{
			Stream: stream,
			Group:  group,
			Start:  "-",
			End:    "+",
			Count:  1000,
		}).Result()
		if err != nil {
			continue
		}

		// Filter to messages with retries > 0 (have been delivered at least once).
		var failedIDs []string
		for _, p := range pending {
			if p.RetryCount > 1 {
				failedIDs = append(failedIDs, p.ID)
			}
		}

		if len(failedIDs) == 0 {
			continue
		}

		if !force {
			confirmed, err := confirmPrompt(
				fmt.Sprintf("Retry %d failed job(s) in %s queue?", len(failedIDs), string(qt)))
			if err != nil {
				return err
			}
			if !confirmed {
				w.PrintInfo("Skipped.")
				continue
			}
		}

		for _, fid := range failedIDs {
			msgs, err := rdb.XRangeN(cmd.Context(), stream, fid, fid, 1).Result()
			if err != nil || len(msgs) == 0 {
				continue
			}

			j, jErr := queue.ValuesToJob(msgs[0].Values)
			if jErr != nil {
				continue
			}

			j.Retries = 0
			if _, err := producer.Enqueue(cmd.Context(), site, qt, j); err != nil {
				continue
			}
			_ = rdb.XAck(cmd.Context(), stream, group, fid).Err()
			totalRetried++
		}
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"retried": totalRetried,
			"site":    site,
		})
	}

	w.PrintSuccess(fmt.Sprintf("Retried %d failed job(s) for site %q", totalRetried, site))
	return nil
}

// ---------------------------------------------------------------------------
// queue purge
// ---------------------------------------------------------------------------

func newQueuePurgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "purge",
		Short: "Purge all pending jobs",
		Long: `Purge pending jobs from a queue. Requires --queue or --all.
This is a destructive operation — use --force to skip confirmation.`,
		RunE: runQueuePurge,
	}

	f := cmd.Flags()
	f.String("queue", "", "Queue to purge (default/long/critical/scheduler)")
	f.Bool("all", false, "Purge all queues")
	f.String("site", "", "Target site")
	f.Bool("force", false, "Skip confirmation")

	return cmd
}

func runQueuePurge(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	queueFlag, _ := cmd.Flags().GetString("queue")
	allFlag, _ := cmd.Flags().GetBool("all")
	force, _ := cmd.Flags().GetBool("force")

	if queueFlag == "" && !allFlag {
		return output.NewCLIError("Must specify --queue or --all").
			WithFix("Use --queue default or --all to purge queues.")
	}

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

	if svc.Redis == nil || svc.Redis.Queue == nil {
		return output.NewCLIError("Redis is not available").
			WithFix("Ensure Redis is running and configured in moca.yaml.")
	}

	var queueTypes []queue.QueueType
	if allFlag {
		queueTypes = queue.AllQueueTypes
	} else {
		queueTypes = resolveQueueTypes(queueFlag)
		if len(queueTypes) == 0 {
			return output.NewCLIError(fmt.Sprintf("Unknown queue type: %q", queueFlag)).
				WithFix("Valid types: default, long, critical, scheduler, all")
		}
	}

	if !force {
		names := make([]string, len(queueTypes))
		for i, qt := range queueTypes {
			names[i] = string(qt)
		}
		confirmed, err := confirmPrompt(
			fmt.Sprintf("Purge queue(s) [%s] for site %q?", strings.Join(names, ", "), siteName))
		if err != nil {
			return err
		}
		if !confirmed {
			w.PrintInfo("Purge cancelled.")
			return nil
		}
	}

	rdb := svc.Redis.Queue
	results := make(map[string]int64)

	for _, qt := range queueTypes {
		stream := queue.StreamKey(siteName, qt)

		// Get length before trimming.
		length, _ := rdb.XLen(cmd.Context(), stream).Result()

		err := rdb.XTrimMaxLen(cmd.Context(), stream, 0).Err()
		if err != nil {
			return output.NewCLIError(fmt.Sprintf("Failed to purge %s queue", string(qt))).
				WithErr(err)
		}
		results[string(qt)] = length
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"site":   siteName,
			"purged": results,
		})
	}

	var total int64
	for qt, count := range results {
		w.Print("  %s %s: %d job(s) purged", w.Color().Success("✓"), qt, count)
		total += count
	}
	w.PrintSuccess(fmt.Sprintf("Purged %d total job(s) for site %q", total, siteName))

	return nil
}

// ---------------------------------------------------------------------------
// queue dead-letter list
// ---------------------------------------------------------------------------

func newQueueDLListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List dead letter entries",
		Long:  `List jobs in the dead letter queue (jobs that exhausted all retries).`,
		RunE:  runQueueDLList,
	}

	f := cmd.Flags()
	f.String("site", "", "Filter by site")
	f.Int64("limit", 50, "Max results")

	return cmd
}

func runQueueDLList(cmd *cobra.Command, _ []string) error {
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

	if svc.Redis == nil || svc.Redis.Queue == nil {
		return output.NewCLIError("Redis is not available").
			WithFix("Ensure Redis is running and configured in moca.yaml.")
	}

	sites, err := resolveQueueSites(cmd, ctx, svc)
	if err != nil {
		return err
	}

	limit, _ := cmd.Flags().GetInt64("limit")
	rdb := svc.Redis.Queue

	type dlqEntry struct {
		StreamID   string `json:"stream_id"`
		OriginalID string `json:"original_id"`
		Type       string `json:"type"`
		Site       string `json:"site"`
		Retries    string `json:"retries"`
		MovedAt    string `json:"moved_at"`
	}

	var entries []dlqEntry
	remaining := limit

	for _, site := range sites {
		if remaining <= 0 {
			break
		}
		dlqStream := queue.DLQKey(site)
		msgs, err := rdb.XRangeN(cmd.Context(), dlqStream, "-", "+", remaining).Result()
		if err != nil {
			continue
		}

		for _, msg := range msgs {
			if remaining <= 0 {
				break
			}
			entries = append(entries, dlqEntry{
				StreamID:   msg.ID,
				OriginalID: stringFromValues(msg.Values, "dlq_original_id"),
				Type:       stringFromValues(msg.Values, "type"),
				Site:       stringFromValues(msg.Values, "site"),
				Retries:    stringFromValues(msg.Values, "dlq_retry_count"),
				MovedAt:    stringFromValues(msg.Values, "dlq_moved_at"),
			})
			remaining--
		}
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"entries": entries,
			"count":   len(entries),
		})
	}

	if len(entries) == 0 {
		w.PrintInfo("Dead letter queue is empty.")
		return nil
	}

	headers := []string{"ID", "ORIGINAL_ID", "TYPE", "SITE", "RETRIES", "MOVED_AT"}
	var rows [][]string
	for _, e := range entries {
		movedAt := e.MovedAt
		if t, err := time.Parse(time.RFC3339Nano, movedAt); err == nil {
			movedAt = time.Since(t).Truncate(time.Second).String() + " ago"
		}
		rows = append(rows, []string{e.StreamID, e.OriginalID, e.Type, e.Site, e.Retries, movedAt})
	}
	return w.PrintTable(headers, rows)
}

// ---------------------------------------------------------------------------
// queue dead-letter retry
// ---------------------------------------------------------------------------

func newQueueDLRetryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "retry JOB_ID",
		Short: "Retry a dead letter job",
		Long:  `Move a dead letter job back to its original queue for retry.`,
		Args:  cobra.MaximumNArgs(1),
		RunE:  runQueueDLRetry,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.Bool("all", false, "Retry all dead letter items")
	f.Bool("force", false, "Skip confirmation")

	return cmd
}

func runQueueDLRetry(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)

	allFlag, _ := cmd.Flags().GetBool("all")
	if !allFlag && len(args) == 0 {
		return output.NewCLIError("JOB_ID required").
			WithFix("Provide a DLQ entry ID or use --all.")
	}

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

	if svc.Redis == nil || svc.Redis.Queue == nil {
		return output.NewCLIError("Redis is not available").
			WithFix("Ensure Redis is running and configured in moca.yaml.")
	}

	rdb := svc.Redis.Queue
	producer := newQueueProducer(svc)
	dlqStream := queue.DLQKey(siteName)

	if allFlag {
		return retryAllDLQ(cmd, w, rdb, producer, siteName, dlqStream)
	}

	jobID := args[0]
	return retrySingleDLQ(cmd, w, rdb, producer, siteName, dlqStream, jobID)
}

func retrySingleDLQ(cmd *cobra.Command, w *output.Writer, rdb *redis.Client, producer *queue.Producer, site, dlqStream, jobID string) error {
	msgs, err := rdb.XRangeN(cmd.Context(), dlqStream, jobID, jobID, 1).Result()
	if err != nil || len(msgs) == 0 {
		return output.NewCLIError(fmt.Sprintf("DLQ entry %q not found", jobID)).
			WithFix("Use 'moca queue dead-letter list' to find valid IDs.")
	}

	msg := msgs[0]
	j, jErr := queue.ValuesToJob(msg.Values)
	if jErr != nil {
		return output.NewCLIError("Failed to parse DLQ entry").WithErr(jErr)
	}

	// Reset retries for re-enqueue.
	j.Retries = 0

	// Default to QueueDefault if we can't determine original queue.
	qt := queue.QueueDefault
	newID, err := producer.Enqueue(cmd.Context(), site, qt, j)
	if err != nil {
		return output.NewCLIError("Failed to re-enqueue DLQ entry").WithErr(err)
	}

	// Remove from DLQ.
	_ = rdb.XDel(cmd.Context(), dlqStream, jobID).Err()

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"retried": jobID,
			"new_id":  newID,
			"queue":   string(qt),
			"site":    site,
		})
	}

	w.PrintSuccess(fmt.Sprintf("Retried DLQ entry %s → new ID: %s (queue: %s)", jobID, newID, string(qt)))
	return nil
}

func retryAllDLQ(cmd *cobra.Command, w *output.Writer, rdb *redis.Client, producer *queue.Producer, site, dlqStream string) error {
	force, _ := cmd.Flags().GetBool("force")

	// Count entries first.
	count, _ := rdb.XLen(cmd.Context(), dlqStream).Result()
	if count == 0 {
		w.PrintInfo("Dead letter queue is empty.")
		return nil
	}

	if !force {
		confirmed, err := confirmPrompt(
			fmt.Sprintf("Retry all %d dead letter entries for site %q?", count, site))
		if err != nil {
			return err
		}
		if !confirmed {
			w.PrintInfo("Cancelled.")
			return nil
		}
	}

	msgs, err := rdb.XRange(cmd.Context(), dlqStream, "-", "+").Result()
	if err != nil {
		return output.NewCLIError("Failed to read DLQ").WithErr(err)
	}

	var retried int
	for _, msg := range msgs {
		j, jErr := queue.ValuesToJob(msg.Values)
		if jErr != nil {
			continue
		}
		j.Retries = 0

		qt := queue.QueueDefault
		if _, err := producer.Enqueue(cmd.Context(), site, qt, j); err != nil {
			continue
		}
		_ = rdb.XDel(cmd.Context(), dlqStream, msg.ID).Err()
		retried++
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"retried": retried,
			"total":   count,
			"site":    site,
		})
	}

	w.PrintSuccess(fmt.Sprintf("Retried %d/%d dead letter entries for site %q", retried, count, site))
	return nil
}

// ---------------------------------------------------------------------------
// queue dead-letter purge
// ---------------------------------------------------------------------------

func newQueueDLPurgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "purge",
		Short: "Purge dead letter queue",
		Long: `Delete all items from the dead letter queue.
Use --older-than to only purge entries older than a duration (e.g., "7d").`,
		RunE: runQueueDLPurge,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.String("older-than", "", "Purge only items older than duration (e.g., 7d, 24h)")
	f.Bool("force", false, "Skip confirmation")

	return cmd
}

func runQueueDLPurge(cmd *cobra.Command, _ []string) error {
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

	if svc.Redis == nil || svc.Redis.Queue == nil {
		return output.NewCLIError("Redis is not available").
			WithFix("Ensure Redis is running and configured in moca.yaml.")
	}

	rdb := svc.Redis.Queue
	dlqStream := queue.DLQKey(siteName)
	force, _ := cmd.Flags().GetBool("force")
	olderThan, _ := cmd.Flags().GetString("older-than")

	if olderThan != "" {
		return purgeDLQOlderThan(cmd, w, rdb, dlqStream, siteName, olderThan, force)
	}

	// Full purge.
	count, _ := rdb.XLen(cmd.Context(), dlqStream).Result()
	if count == 0 {
		w.PrintInfo("Dead letter queue is already empty.")
		return nil
	}

	if !force {
		confirmed, confirmErr := confirmPrompt(
			fmt.Sprintf("Purge all %d dead letter entries for site %q?", count, siteName))
		if confirmErr != nil {
			return confirmErr
		}
		if !confirmed {
			w.PrintInfo("Purge cancelled.")
			return nil
		}
	}

	err = rdb.XTrimMaxLen(cmd.Context(), dlqStream, 0).Err()
	if err != nil {
		return output.NewCLIError("Failed to purge DLQ").WithErr(err)
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"site":   siteName,
			"purged": count,
		})
	}

	w.PrintSuccess(fmt.Sprintf("Purged %d dead letter entries for site %q", count, siteName))
	return nil
}

func purgeDLQOlderThan(cmd *cobra.Command, w *output.Writer, rdb *redis.Client, dlqStream, site, olderThanStr string, force bool) error {
	dur, err := parseDuration(olderThanStr)
	if err != nil {
		return output.NewCLIError(fmt.Sprintf("Invalid duration: %q", olderThanStr)).
			WithFix("Use a duration like 7d, 24h, 1h30m.")
	}

	cutoff := time.Now().Add(-dur)

	msgs, err := rdb.XRange(cmd.Context(), dlqStream, "-", "+").Result()
	if err != nil {
		return output.NewCLIError("Failed to read DLQ").WithErr(err)
	}

	var toDelete []string
	for _, msg := range msgs {
		movedAtStr := stringFromValues(msg.Values, "dlq_moved_at")
		if movedAtStr == "" {
			continue
		}
		movedAt, err := time.Parse(time.RFC3339Nano, movedAtStr)
		if err != nil {
			continue
		}
		if movedAt.Before(cutoff) {
			toDelete = append(toDelete, msg.ID)
		}
	}

	if len(toDelete) == 0 {
		w.PrintInfo(fmt.Sprintf("No DLQ entries older than %s.", olderThanStr))
		return nil
	}

	if !force {
		confirmed, err := confirmPrompt(
			fmt.Sprintf("Purge %d DLQ entries older than %s for site %q?", len(toDelete), olderThanStr, site))
		if err != nil {
			return err
		}
		if !confirmed {
			w.PrintInfo("Purge cancelled.")
			return nil
		}
	}

	deleted := 0
	for _, id := range toDelete {
		if err := rdb.XDel(cmd.Context(), dlqStream, id).Err(); err == nil {
			deleted++
		}
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"site":       site,
			"purged":     deleted,
			"older_than": olderThanStr,
		})
	}

	w.PrintSuccess(fmt.Sprintf("Purged %d DLQ entries older than %s for site %q", deleted, olderThanStr, site))
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// cliCtx is a type alias to keep import paths cleaner.
type cliCtx = clicontext.CLIContext

// resolveQueueTypes converts a queue flag value to a slice of QueueType.
func resolveQueueTypes(flag string) []queue.QueueType {
	flag = strings.ToLower(strings.TrimSpace(flag))
	if flag == "" || flag == "all" {
		return queue.AllQueueTypes
	}
	switch queue.QueueType(flag) {
	case queue.QueueDefault, queue.QueueLong, queue.QueueCritical, queue.QueueScheduler:
		return []queue.QueueType{queue.QueueType(flag)}
	default:
		return nil
	}
}

// stringFromValues extracts a string from a Redis stream values map.
func stringFromValues(values map[string]interface{}, key string) string {
	v, ok := values[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// parseDuration parses a duration string supporting day suffix (e.g., "7d", "24h").
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

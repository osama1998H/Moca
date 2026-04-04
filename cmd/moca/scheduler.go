package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/internal/process"
	"github.com/osama1998H/moca/internal/serve"
	"github.com/osama1998H/moca/pkg/queue"
)

// NewSchedulerCommand returns the "moca scheduler" command group with all subcommands.
func NewSchedulerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scheduler",
		Short: "Scheduler management",
		Long:  "Start, stop, and manage the cron scheduler and scheduled jobs.",
	}

	cmd.AddCommand(
		newSchedulerStartCmd(),
		newSchedulerStopCmd(),
		newSchedulerStatusCmd(),
		newSchedulerEnableCmd(),
		newSchedulerDisableCmd(),
		newSchedulerTriggerCmd(),
		newSchedulerListJobsCmd(),
		newSchedulerPurgeJobsCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// scheduler start
// ---------------------------------------------------------------------------

func newSchedulerStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the scheduler process",
		Long: `Start the cron scheduler with leader election.

Use --foreground to run in the current terminal (primary mode).
Without --foreground, the command advises using foreground mode or a process manager.`,
		RunE: runSchedulerStart,
	}

	cmd.Flags().Bool("foreground", false, "Run in the foreground (attached to terminal)")

	return cmd
}

func runSchedulerStart(cmd *cobra.Command, _ []string) error {
	foreground, _ := cmd.Flags().GetBool("foreground")
	if !foreground {
		return output.NewCLIError("Background daemonization is not yet supported").
			WithFix("Use 'moca scheduler start --foreground' or a process manager (systemd, supervisord).")
	}

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}
	projectRoot := cliCtx.ProjectRoot
	cfg := cliCtx.Project

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), cfg, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	if svc.Redis == nil || svc.Redis.Queue == nil {
		return output.NewCLIError("Redis is not available").
			WithFix("Ensure Redis is running and configured in moca.yaml.")
	}

	// Check for existing scheduler process.
	if pid, running, pidErr := processStatus(projectRoot, "scheduler"); pidErr == nil && running {
		return output.NewCLIError("Scheduler is already running").
			WithContext(fmt.Sprintf("PID %d", pid)).
			WithFix("Run 'moca scheduler stop' first.")
	}

	// Write PID file.
	if err := writePIDFile(projectRoot, "scheduler"); err != nil {
		return output.NewCLIError("Failed to write scheduler PID file").
			WithErr(err).
			WithFix("Check permissions on the .moca directory.")
	}
	defer func() { _ = removePIDFile(projectRoot, "scheduler") }()

	// Construct scheduler subsystem using the same factory as moca serve.
	schedulerRun := serve.SchedulerSubsystem(
		svc.Redis,
		cfg.Scheduler,
		svc.Logger,
	)

	sup := process.NewSupervisor(svc.Logger, process.WithShutdownTimeout(30*time.Second))
	sup.Add(process.Subsystem{
		Name:     "scheduler",
		Run:      schedulerRun,
		Critical: true,
	})

	// Signal handling for graceful shutdown.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	w := output.NewWriter(cmd)
	w.Print("Moca Scheduler")
	w.Print("==============")
	w.Print("  PID:     %d", os.Getpid())
	w.Print("  Enabled: %v", cfg.Scheduler.Enabled)
	w.Print("")
	w.Print("Press Ctrl+C to stop.")
	w.Print("")

	if err := sup.Run(ctx); err != nil {
		return output.NewCLIError("Scheduler exited with error").
			WithErr(err).
			WithCause(err.Error())
	}

	w.Print("Scheduler stopped.")
	return nil
}

// ---------------------------------------------------------------------------
// scheduler stop
// ---------------------------------------------------------------------------

func newSchedulerStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the scheduler",
		Long:  "Send a graceful shutdown signal to the running scheduler process.",
		RunE:  runSchedulerStop,
	}
}

func runSchedulerStop(cmd *cobra.Command, _ []string) error {
	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	w := output.NewWriter(cmd)

	if err := stopProcess(cliCtx.ProjectRoot, "scheduler"); err != nil {
		return err
	}

	w.PrintSuccess("Scheduler process stopped.")
	return nil
}

// ---------------------------------------------------------------------------
// scheduler status
// ---------------------------------------------------------------------------

func newSchedulerStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show scheduler status",
		Long:  "Display the scheduler leader, process status, and next scheduled run times.",
		RunE:  runSchedulerStatus,
	}

	cmd.Flags().String("site", "", "Filter by site")

	return cmd
}

func runSchedulerStatus(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), cliCtx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	if svc.Redis == nil || svc.Redis.Queue == nil {
		return output.NewCLIError("Redis is not available").
			WithFix("Ensure Redis is running and configured in moca.yaml.")
	}

	rdb := svc.Redis.Queue

	// Process status.
	pid, running, _ := processStatus(cliCtx.ProjectRoot, "scheduler")

	// Leader info.
	leaderID, leaderErr := rdb.Get(cmd.Context(), queue.DefaultLeaderKey).Result()
	leaderTTL, _ := rdb.TTL(cmd.Context(), queue.DefaultLeaderKey).Result()

	// Disabled sites.
	disabledSites, _ := rdb.SMembers(cmd.Context(), "moca:scheduler:disabled-sites").Result()

	if w.Mode() == output.ModeJSON {
		result := map[string]any{
			"pid":            pid,
			"running":        running,
			"disabled_sites": disabledSites,
		}
		if leaderErr == nil {
			result["leader"] = map[string]any{
				"instance_id": leaderID,
				"ttl_seconds": int(leaderTTL.Seconds()),
			}
		}
		return w.PrintJSON(result)
	}

	// Process status.
	if running {
		w.Print("Scheduler process: %s (PID %d)", w.Color().Success("running"), pid)
	} else {
		w.Print("Scheduler process: %s", w.Color().Warning("not running"))
	}

	// Leader info.
	if leaderErr == nil {
		w.Print("Leader: %s (TTL: %s)", w.Color().Info(leaderID), leaderTTL.Truncate(time.Second))
	} else {
		w.Print("Leader: %s", w.Color().Muted("none"))
	}

	// Disabled sites.
	if len(disabledSites) > 0 {
		w.Print("")
		w.Print("Disabled sites:")
		for _, s := range disabledSites {
			w.Print("  - %s", s)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// scheduler enable
// ---------------------------------------------------------------------------

func newSchedulerEnableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable scheduler for a site",
		Long:  "Remove a site from the disabled-sites set, allowing the scheduler to process it.",
		RunE:  runSchedulerEnable,
	}

	cmd.Flags().String("site", "", "Site to enable (required)")
	_ = cmd.MarkFlagRequired("site")

	return cmd
}

func runSchedulerEnable(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), cliCtx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	if svc.Redis == nil || svc.Redis.Queue == nil {
		return output.NewCLIError("Redis is not available").
			WithFix("Ensure Redis is running and configured in moca.yaml.")
	}

	site, _ := cmd.Flags().GetString("site")
	removed, err := svc.Redis.Queue.SRem(cmd.Context(), "moca:scheduler:disabled-sites", site).Result()
	if err != nil {
		return fmt.Errorf("remove site from disabled set: %w", err)
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"site": site, "enabled": true, "was_disabled": removed > 0})
	}

	if removed > 0 {
		w.PrintSuccess(fmt.Sprintf("Scheduler enabled for site %q.", site))
	} else {
		w.PrintInfo(fmt.Sprintf("Site %q was not disabled.", site))
	}
	return nil
}

// ---------------------------------------------------------------------------
// scheduler disable
// ---------------------------------------------------------------------------

func newSchedulerDisableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disable",
		Short: "Disable scheduler for a site",
		Long: `Add a site to the disabled-sites set.
Background jobs will stop being enqueued for this site, but already-enqueued jobs complete.`,
		RunE: runSchedulerDisable,
	}

	cmd.Flags().String("site", "", "Site to disable (required)")
	_ = cmd.MarkFlagRequired("site")

	return cmd
}

func runSchedulerDisable(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), cliCtx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	if svc.Redis == nil || svc.Redis.Queue == nil {
		return output.NewCLIError("Redis is not available").
			WithFix("Ensure Redis is running and configured in moca.yaml.")
	}

	site, _ := cmd.Flags().GetString("site")
	added, err := svc.Redis.Queue.SAdd(cmd.Context(), "moca:scheduler:disabled-sites", site).Result()
	if err != nil {
		return fmt.Errorf("add site to disabled set: %w", err)
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{"site": site, "disabled": true, "was_enabled": added > 0})
	}

	if added > 0 {
		w.PrintSuccess(fmt.Sprintf("Scheduler disabled for site %q.", site))
	} else {
		w.PrintInfo(fmt.Sprintf("Site %q was already disabled.", site))
	}
	return nil
}

// ---------------------------------------------------------------------------
// scheduler trigger
// ---------------------------------------------------------------------------

func newSchedulerTriggerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trigger EVENT",
		Short: "Manually trigger a scheduled event",
		Long: `Manually enqueue a scheduled event job for immediate processing.

EVENT is the job type name (e.g. hourly, daily, weekly, monthly, or a custom event name).`,
		RunE: runSchedulerTrigger,
		Args: cobra.ExactArgs(1),
	}

	cmd.Flags().String("site", "", "Target site (required unless --all-sites)")
	cmd.Flags().Bool("all-sites", false, "Trigger for all active sites")

	return cmd
}

func runSchedulerTrigger(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	eventName := args[0]

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), cliCtx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	if svc.Redis == nil || svc.Redis.Queue == nil {
		return output.NewCLIError("Redis is not available").
			WithFix("Ensure Redis is running and configured in moca.yaml.")
	}

	allSites, _ := cmd.Flags().GetBool("all-sites")
	var sites []string
	if allSites {
		sites, err = listActiveSites(cmd.Context(), svc)
		if err != nil {
			return err
		}
		if len(sites) == 0 {
			return output.NewCLIError("No active sites found").
				WithFix("Create a site with 'moca site create <name>'.")
		}
	} else {
		site, siteErr := resolveSiteName(cmd, cliCtx)
		if siteErr != nil {
			return siteErr
		}
		sites = []string{site}
	}

	producer := newQueueProducer(svc)
	var triggered []map[string]string

	for _, site := range sites {
		job := queue.Job{
			ID:         fmt.Sprintf("manual-%s-%s-%d", eventName, site, time.Now().UnixNano()),
			Site:       site,
			Type:       eventName,
			Payload:    map[string]any{"trigger": "manual"},
			CreatedAt:  time.Now().UTC(),
			MaxRetries: 3,
			Timeout:    5 * time.Minute,
		}
		msgID, enqErr := producer.Enqueue(cmd.Context(), site, queue.QueueScheduler, job)
		if enqErr != nil {
			w.PrintError(fmt.Sprintf("Failed to trigger %q for site %q: %v", eventName, site, enqErr))
			continue
		}
		triggered = append(triggered, map[string]string{
			"site":    site,
			"event":   eventName,
			"job_id":  job.ID,
			"msg_id":  msgID,
		})
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(triggered)
	}

	for _, t := range triggered {
		w.PrintSuccess(fmt.Sprintf("Triggered %q for site %q (job: %s)", t["event"], t["site"], t["job_id"]))
	}
	return nil
}

// ---------------------------------------------------------------------------
// scheduler list-jobs
// ---------------------------------------------------------------------------

func newSchedulerListJobsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-jobs",
		Short: "List registered scheduled jobs",
		Long: `List all registered cron jobs for a site.

Jobs are stored in Redis by the scheduler on startup. If no entries are found,
the scheduler may not have started yet or no cron jobs are registered by apps.`,
		RunE: runSchedulerListJobs,
	}

	f := cmd.Flags()
	f.String("site", "", "Site to list jobs for (required)")
	_ = cmd.MarkFlagRequired("site")
	f.String("app", "", "Filter by app name")

	return cmd
}

func runSchedulerListJobs(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), cliCtx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	if svc.Redis == nil || svc.Redis.Queue == nil {
		return output.NewCLIError("Redis is not available").
			WithFix("Ensure Redis is running and configured in moca.yaml.")
	}

	site, _ := cmd.Flags().GetString("site")
	appFilter, _ := cmd.Flags().GetString("app")

	key := fmt.Sprintf("moca:scheduler:entries:%s", site)
	entries, err := svc.Redis.Queue.HGetAll(cmd.Context(), key).Result()
	if err != nil {
		return fmt.Errorf("read scheduler entries: %w", err)
	}

	type jobEntry struct {
		Name      string `json:"name"`
		CronExpr  string `json:"cron_expr"`
		JobType   string `json:"job_type"`
		QueueType string `json:"queue_type"`
		App       string `json:"app,omitempty"`
	}

	var jobs []jobEntry
	for name, raw := range entries {
		var entry jobEntry
		if jsonErr := json.Unmarshal([]byte(raw), &entry); jsonErr != nil {
			// Fallback: treat value as cron expression.
			entry = jobEntry{Name: name, CronExpr: raw}
		}
		if entry.Name == "" {
			entry.Name = name
		}
		if appFilter != "" && entry.App != appFilter {
			continue
		}
		jobs = append(jobs, entry)
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(jobs)
	}

	if len(jobs) == 0 {
		w.PrintInfo(fmt.Sprintf("No registered cron jobs found for site %q.", site))
		w.Print("%s", w.Color().Muted("Jobs are populated when the scheduler starts with registered app cron entries."))
		return nil
	}

	headers := []string{"NAME", "CRON", "JOB TYPE", "QUEUE"}
	var rows [][]string
	for _, j := range jobs {
		qt := j.QueueType
		if qt == "" {
			qt = "scheduler"
		}
		rows = append(rows, []string{j.Name, j.CronExpr, j.JobType, qt})
	}

	return w.PrintTable(headers, rows)
}

// ---------------------------------------------------------------------------
// scheduler purge-jobs
// ---------------------------------------------------------------------------

func newSchedulerPurgeJobsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "purge-jobs",
		Short: "Purge pending jobs from scheduler queue",
		Long: `Truncate the scheduler queue stream for a site.
Requires --force or interactive confirmation.`,
		RunE: runSchedulerPurgeJobs,
	}

	f := cmd.Flags()
	f.String("site", "", "Site to purge (required unless --all)")
	f.String("event", "", "Filter by event/job type")
	f.Bool("all", false, "Purge scheduler queues for all active sites")
	f.Bool("force", false, "Skip confirmation prompt")

	return cmd
}

func runSchedulerPurgeJobs(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), cliCtx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	if svc.Redis == nil || svc.Redis.Queue == nil {
		return output.NewCLIError("Redis is not available").
			WithFix("Ensure Redis is running and configured in moca.yaml.")
	}

	allFlag, _ := cmd.Flags().GetBool("all")
	force, _ := cmd.Flags().GetBool("force")

	var sites []string
	if allFlag {
		sites, err = listActiveSites(cmd.Context(), svc)
		if err != nil {
			return err
		}
	} else {
		site, siteErr := resolveSiteName(cmd, cliCtx)
		if siteErr != nil {
			return output.NewCLIError("No site specified").
				WithFix("Pass --site <name> or --all to purge all sites.")
		}
		sites = []string{site}
	}

	if !force {
		msg := fmt.Sprintf("Purge scheduler queue for %d site(s)?", len(sites))
		ok, promptErr := confirmPrompt(msg)
		if promptErr != nil {
			return promptErr
		}
		if !ok {
			w.Print("Aborted.")
			return nil
		}
	}

	rdb := svc.Redis.Queue
	var totalPurged int64

	for _, site := range sites {
		stream := queue.StreamKey(site, queue.QueueScheduler)
		before, _ := rdb.XLen(cmd.Context(), stream).Result()
		if err := rdb.XTrimMaxLen(cmd.Context(), stream, 0).Err(); err != nil {
			w.PrintError(fmt.Sprintf("Failed to purge %s: %v", stream, err))
			continue
		}
		totalPurged += before
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"sites_purged": len(sites),
			"jobs_purged":  totalPurged,
		})
	}

	w.PrintSuccess(fmt.Sprintf("Purged %d job(s) from %d site(s).", totalPurged, len(sites)))
	return nil
}

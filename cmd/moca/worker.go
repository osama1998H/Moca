package main

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/internal/process"
	"github.com/osama1998H/moca/internal/serve"
	"github.com/osama1998H/moca/pkg/queue"
)

// NewWorkerCommand returns the "moca worker" command group with all subcommands.
func NewWorkerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Background worker management",
		Long:  "Start, stop, and monitor background job workers.",
	}

	cmd.AddCommand(
		newWorkerStartCmd(),
		newWorkerStopCmd(),
		newWorkerStatusCmd(),
		newWorkerScaleCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// worker start
// ---------------------------------------------------------------------------

func newWorkerStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start background workers",
		Long: `Start the background worker pool that processes jobs from Redis Streams.

Use --foreground to run in the current terminal (primary mode).
Without --foreground, the command advises using foreground mode or a process manager.`,
		RunE: runWorkerStart,
	}

	cmd.Flags().Bool("foreground", false, "Run in the foreground (attached to terminal)")

	return cmd
}

func runWorkerStart(cmd *cobra.Command, _ []string) error {
	foreground, _ := cmd.Flags().GetBool("foreground")
	if !foreground {
		return output.NewCLIError("Background daemonization is not yet supported").
			WithFix("Use 'moca worker start --foreground' or a process manager (systemd, supervisord).")
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

	// Check for existing worker process.
	if pid, running, pidErr := processStatus(projectRoot, "worker"); pidErr == nil && running {
		return output.NewCLIError("Worker is already running").
			WithContext(fmt.Sprintf("PID %d", pid)).
			WithFix("Run 'moca worker stop' first.")
	}

	// Write PID file.
	if err := writePIDFile(projectRoot, "worker"); err != nil {
		return output.NewCLIError("Failed to write worker PID file").
			WithErr(err).
			WithFix("Check permissions on the .moca directory.")
	}
	defer func() { _ = removePIDFile(projectRoot, "worker") }()

	// Construct worker subsystem using the same factory as moca serve.
	workerRun := serve.WorkerSubsystem(
		svc.DB,
		svc.Redis,
		svc.Registry,
		cfg.Infrastructure.Kafka,
		cfg.Infrastructure.Search,
		svc.Logger,
	)

	sup := process.NewSupervisor(svc.Logger, process.WithShutdownTimeout(30*time.Second))
	sup.Add(process.Subsystem{
		Name:     "worker",
		Run:      workerRun,
		Critical: true,
	})

	// Signal handling for graceful shutdown.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	w := output.NewWriter(cmd)
	w.Print("Moca Worker")
	w.Print("===========")
	w.Print("  PID: %d", os.Getpid())
	w.Print("")
	w.Print("Press Ctrl+C to stop.")
	w.Print("")

	if err := sup.Run(ctx); err != nil {
		return output.NewCLIError("Worker exited with error").
			WithErr(err).
			WithCause(err.Error())
	}

	w.Print("Worker stopped.")
	return nil
}

// ---------------------------------------------------------------------------
// worker stop
// ---------------------------------------------------------------------------

func newWorkerStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop background workers",
		Long:  "Send a graceful shutdown signal to the running worker process.",
		RunE:  runWorkerStop,
	}
}

func runWorkerStop(cmd *cobra.Command, _ []string) error {
	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	w := output.NewWriter(cmd)

	if err := stopProcess(cliCtx.ProjectRoot, "worker"); err != nil {
		return err
	}

	w.PrintSuccess("Worker process stopped.")
	return nil
}

// ---------------------------------------------------------------------------
// worker status
// ---------------------------------------------------------------------------

func newWorkerStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show worker pool status",
		Long:  "Display active consumers, pending message counts, and idle times per queue.",
		RunE:  runWorkerStatus,
	}

	cmd.Flags().String("site", "", "Filter by site (default: all active sites)")

	return cmd
}

func runWorkerStatus(cmd *cobra.Command, _ []string) error {
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

	// Show process status.
	pid, running, _ := processStatus(cliCtx.ProjectRoot, "worker")
	if running {
		w.Print("Worker process: %s (PID %d)", w.Color().Success("running"), pid)
	} else {
		w.Print("Worker process: %s", w.Color().Warning("not running"))
	}
	w.Print("")

	// Resolve sites.
	sites, err := resolveWorkerSites(cmd, svc)
	if err != nil {
		return err
	}

	rdb := svc.Redis.Queue

	type consumerRow struct {
		Site      string `json:"site"`
		Queue     string `json:"queue"`
		Group     string `json:"group"`
		Idle      string `json:"last_delivered"`
		Consumers int64  `json:"consumers"`
		Pending   int64  `json:"pending"`
	}

	var rows []consumerRow

	for _, site := range sites {
		for _, qt := range queue.AllQueueTypes {
			stream := queue.StreamKey(site, qt)
			groups, err := rdb.XInfoGroups(cmd.Context(), stream).Result()
			if err != nil {
				continue // stream may not exist
			}
			for _, g := range groups {
				rows = append(rows, consumerRow{
					Site:      site,
					Queue:     string(qt),
					Group:     g.Name,
					Consumers: int64(g.Consumers),
					Pending:   g.Pending,
					Idle:      g.LastDeliveredID,
				})
			}
		}
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"pid":       pid,
			"running":   running,
			"consumers": rows,
		})
	}

	if len(rows) == 0 {
		w.PrintInfo("No active consumer groups found.")
		return nil
	}

	headers := []string{"SITE", "QUEUE", "GROUP", "CONSUMERS", "PENDING", "LAST DELIVERED"}
	var tableRows [][]string
	for _, r := range rows {
		tableRows = append(tableRows, []string{
			r.Site,
			r.Queue,
			r.Group,
			strconv.FormatInt(r.Consumers, 10),
			strconv.FormatInt(r.Pending, 10),
			r.Idle,
		})
	}

	return w.PrintTable(headers, tableRows)
}

func resolveWorkerSites(cmd *cobra.Command, svc *Services) ([]string, error) {
	if site, _ := cmd.Flags().GetString("site"); site != "" {
		return []string{site}, nil
	}
	sites, err := listActiveSites(cmd.Context(), svc)
	if err != nil {
		return nil, err
	}
	if len(sites) == 0 {
		return nil, output.NewCLIError("No active sites found").
			WithFix("Create a site with 'moca site create <name>'.")
	}
	return sites, nil
}

// ---------------------------------------------------------------------------
// worker scale
// ---------------------------------------------------------------------------

func newWorkerScaleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scale QUEUE COUNT",
		Short: "Adjust worker pool size at runtime",
		Long: `Set the desired number of consumers for a queue type.

The value is written to Redis. The running worker pool does not currently
support dynamic scaling — a restart is required for the change to take effect.`,
		RunE: runWorkerScale,
		Args: cobra.ExactArgs(2),
	}

	return cmd
}

func runWorkerScale(cmd *cobra.Command, args []string) error {
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

	queueName := args[0]
	// Validate queue type.
	valid := false
	for _, qt := range queue.AllQueueTypes {
		if string(qt) == queueName {
			valid = true
			break
		}
	}
	if !valid {
		return output.NewCLIError(fmt.Sprintf("Unknown queue type %q", queueName)).
			WithFix("Valid types: default, long, critical, scheduler.")
	}

	count, err := strconv.Atoi(args[1])
	if err != nil || count < 0 {
		return output.NewCLIError(fmt.Sprintf("Invalid count %q", args[1])).
			WithFix("Provide a non-negative integer.")
	}

	key := fmt.Sprintf("moca:worker:scale:%s", queueName)
	if err := svc.Redis.Queue.Set(cmd.Context(), key, count, 0).Err(); err != nil {
		return fmt.Errorf("set worker scale: %w", err)
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"queue":   queueName,
			"count":   count,
			"applied": false,
		})
	}

	w.PrintSuccess(fmt.Sprintf("Desired scale for %q set to %d.", queueName, count))
	w.PrintWarning("The worker pool does not support dynamic scaling. Restart workers for this to take effect.")
	return nil
}

package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/moca-framework/moca/internal/output"
	"github.com/moca-framework/moca/internal/process"
	"github.com/moca-framework/moca/internal/serve"
	"github.com/moca-framework/moca/pkg/meta"
	"github.com/moca-framework/moca/pkg/observe"
)

// NewServeCommand returns the "moca serve" command (and its "start" alias).
func NewServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "serve",
		Aliases: []string{"start"},
		Short:   "Start the development server",
		Long:    "Start all Moca processes (HTTP server, workers, scheduler) for local development.",
		RunE:    runServe,
	}
	f := cmd.Flags()
	f.Int("port", 8000, "HTTP server port")
	f.String("host", "0.0.0.0", "HTTP server bind address")
	f.Int("workers", 2, "Number of background worker goroutines")
	f.Bool("no-workers", false, "Disable background workers")
	f.Bool("no-scheduler", false, "Disable cron scheduler")
	f.Bool("no-watch", false, "Disable file watcher for hot reload")
	f.Bool("profile", false, "Enable pprof profiling endpoints")
	return cmd
}

func runServe(cmd *cobra.Command, _ []string) error {
	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}
	projectRoot := cliCtx.ProjectRoot
	cfg := cliCtx.Project

	// ── Read flags ──────────────────────────────────────────────────────
	port, _ := cmd.Flags().GetInt("port")
	host, _ := cmd.Flags().GetString("host")
	noWorkers, _ := cmd.Flags().GetBool("no-workers")
	noScheduler, _ := cmd.Flags().GetBool("no-scheduler")
	noWatch, _ := cmd.Flags().GetBool("no-watch")

	// Respect config defaults when flag not explicitly set.
	if !cmd.Flags().Changed("port") && cfg.Development.Port != 0 {
		port = cfg.Development.Port
	}

	// ── PID check ───────────────────────────────────────────────────────
	if pid, pidErr := process.ReadPID(projectRoot); pidErr == nil {
		if process.IsRunning(pid) {
			return output.NewCLIError("Moca server is already running").
				WithContext(fmt.Sprintf("PID %d (file: %s)", pid, process.PIDPath(projectRoot))).
				WithFix("Run 'moca stop' first, or 'moca restart'.")
		}
		// Stale PID — clean up.
		_ = process.RemovePID(projectRoot)
	}

	// ── Logger ──────────────────────────────────────────────────────────
	logger := observe.NewLogger(slog.LevelInfo)

	// ── Server ──────────────────────────────────────────────────────────
	srv, err := serve.NewServer(cmd.Context(), serve.ServerConfig{
		Config:  cfg,
		Logger:  logger,
		Host:    host,
		Port:    port,
		Version: Version,
	})
	if err != nil {
		return output.NewCLIError("Failed to start server").
			WithErr(err).
			WithCause(err.Error()).
			WithFix("Check database and Redis connection settings in moca.yaml.")
	}
	defer srv.Close()

	// ── Supervisor ──────────────────────────────────────────────────────
	sup := process.NewSupervisor(logger, process.WithShutdownTimeout(30*time.Second))

	sup.Add(process.Subsystem{Name: "http", Run: srv.Run, Critical: true})

	if !noWorkers {
		sup.Add(process.Subsystem{Name: "worker", Run: serve.WorkerStub(logger)})
	}
	if !noScheduler {
		sup.Add(process.Subsystem{Name: "scheduler", Run: serve.SchedulerStub(logger)})
	}
	sup.Add(process.Subsystem{Name: "outbox", Run: serve.OutboxStub(logger)})

	if !noWatch {
		watcher := meta.NewWatcher(
			srv.Registry(),
			&meta.DBSiteLister{DB: srv.DBManager()},
			logger,
			meta.WatcherConfig{AppsDir: filepath.Join(projectRoot, "apps")},
		)
		sup.Add(process.Subsystem{Name: "watcher", Run: watcher.Run})
	}

	// ── PID file ────────────────────────────────────────────────────────
	if err := process.WritePID(projectRoot); err != nil {
		return output.NewCLIError("Failed to write PID file").
			WithErr(err).
			WithFix("Check permissions on the .moca directory.")
	}
	defer func() { _ = process.RemovePID(projectRoot) }()

	// ── Signal handling ─────────────────────────────────────────────────
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ── Startup banner ──────────────────────────────────────────────────
	w := cmd.OutOrStdout()
	_, _ = fmt.Fprintln(w, "Moca Development Server")
	_, _ = fmt.Fprintln(w, "=======================")
	_, _ = fmt.Fprintf(w, "  URL:       http://%s\n", srv.Addr())
	_, _ = fmt.Fprintf(w, "  PID:       %d\n", os.Getpid())
	_, _ = fmt.Fprintf(w, "  Workers:   %s\n", enabledStr(!noWorkers))
	_, _ = fmt.Fprintf(w, "  Scheduler: %s\n", enabledStr(!noScheduler))
	_, _ = fmt.Fprintf(w, "  Watcher:   %s\n", enabledStr(!noWatch))
	_, _ = fmt.Fprintf(w, "  Outbox:    enabled (stub)\n")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Press Ctrl+C to stop.")
	_, _ = fmt.Fprintln(w)

	// ── Run (blocks until shutdown) ─────────────────────────────────────
	if err := sup.Run(ctx); err != nil {
		return output.NewCLIError("Server exited with error").
			WithErr(err).
			WithCause(err.Error())
	}

	_, _ = fmt.Fprintln(w, "Server stopped.")
	return nil
}

func enabledStr(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

// stopServer stops a running Moca server by sending a signal and polling.
// Returns nil if no server was running (stale PID cleaned up).
// Returns a sentinel errNoServer if no PID file exists.
func stopServer(projectRoot string, timeout time.Duration, force bool) error {
	pid, err := process.ReadPID(projectRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errNoServer
		}
		// Could be a parse error — treat as no server.
		return errNoServer
	}

	if !process.IsRunning(pid) {
		_ = process.RemovePID(projectRoot)
		return nil // stale PID cleaned up
	}

	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	if err := syscall.Kill(pid, sig); err != nil {
		return fmt.Errorf("send signal to PID %d: %w", pid, err)
	}

	// Poll until process exits or timeout.
	deadline := time.After(timeout)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !process.IsRunning(pid) {
				_ = process.RemovePID(projectRoot)
				return nil
			}
		case <-deadline:
			if !force {
				// Escalate to SIGKILL.
				_ = syscall.Kill(pid, syscall.SIGKILL)
				time.Sleep(1 * time.Second)
				_ = process.RemovePID(projectRoot)
				return nil
			}
			return fmt.Errorf("process %d did not stop within %s", pid, timeout)
		}
	}
}

var errNoServer = errors.New("no running server")

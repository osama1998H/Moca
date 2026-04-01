package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// NewRestartCommand returns the "moca restart" command.
func NewRestartCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart all running Moca processes",
		Long:  "Restart all Moca processes (server, workers, scheduler).",
		RunE:  runRestart,
	}
	f := cmd.Flags()
	// Stop-phase flags.
	f.Bool("graceful", true, "Wait for in-flight requests before stopping")
	// Serve-phase flags (same as serve).
	f.Int("port", 8000, "HTTP server port")
	f.String("host", "0.0.0.0", "HTTP server bind address")
	f.Int("workers", 2, "Number of background worker goroutines")
	f.Bool("no-workers", false, "Disable background workers")
	f.Bool("no-scheduler", false, "Disable cron scheduler")
	f.Bool("no-watch", false, "Disable file watcher for hot reload")
	f.Bool("profile", false, "Enable pprof profiling endpoints")
	return cmd
}

func runRestart(cmd *cobra.Command, args []string) error {
	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	graceful, _ := cmd.Flags().GetBool("graceful")
	force := !graceful

	w := cmd.OutOrStdout()

	// ── Stop phase ──────────────────────────────────────────────────────
	if stopErr := stopServer(cliCtx.ProjectRoot, 30*time.Second, force); stopErr != nil {
		if errors.Is(stopErr, errNoServer) {
			_, _ = fmt.Fprintln(w, "No running server found, starting fresh.")
		} else {
			_, _ = fmt.Fprintf(w, "Warning: stop phase: %s\n", stopErr)
		}
	} else {
		_, _ = fmt.Fprintln(w, "Server stopped, restarting...")
	}

	// ── Serve phase ─────────────────────────────────────────────────────
	return runServe(cmd, args)
}

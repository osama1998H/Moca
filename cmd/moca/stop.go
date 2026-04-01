package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/moca-framework/moca/internal/output"
	"github.com/moca-framework/moca/internal/process"
)

// NewStopCommand returns the "moca stop" command.
func NewStopCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop all running Moca processes",
		Long:  "Gracefully stop all Moca processes (server, workers, scheduler).",
		RunE:  runStop,
	}
	f := cmd.Flags()
	f.Bool("graceful", true, "Wait for in-flight requests to complete")
	f.Duration("timeout", 30*time.Second, "Graceful shutdown timeout")
	f.Bool("force", false, "Force kill immediately (SIGKILL)")
	return cmd
}

func runStop(cmd *cobra.Command, _ []string) error {
	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}
	projectRoot := cliCtx.ProjectRoot

	force, _ := cmd.Flags().GetBool("force")
	timeout, _ := cmd.Flags().GetDuration("timeout")

	pid, readErr := process.ReadPID(projectRoot)
	if readErr != nil {
		return output.NewCLIError("No running Moca server found").
			WithContext("PID file not found at " + process.PIDPath(projectRoot)).
			WithFix("Start the server with 'moca serve'.")
	}

	w := cmd.OutOrStdout()

	if !process.IsRunning(pid) {
		_ = process.RemovePID(projectRoot)
		_, _ = fmt.Fprintf(w, "Stale PID file removed (process %d was not running).\n", pid)
		return nil
	}

	if err := stopServer(projectRoot, timeout, force); err != nil {
		if errors.Is(err, errNoServer) {
			return output.NewCLIError("No running Moca server found").
				WithFix("Start the server with 'moca serve'.")
		}
		return output.NewCLIError("Failed to stop server").
			WithErr(err).
			WithCause(err.Error())
	}

	if force {
		_, _ = fmt.Fprintf(w, "Moca server killed (PID %d).\n", pid)
	} else {
		_, _ = fmt.Fprintf(w, "Moca server stopped (PID %d).\n", pid)
	}
	return nil
}

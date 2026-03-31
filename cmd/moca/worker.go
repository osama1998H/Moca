package main

import "github.com/spf13/cobra"

// NewWorkerCommand returns the "moca worker" command group with all subcommands.
func NewWorkerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Background worker management",
		Long:  "Start, stop, and monitor background job workers.",
	}

	cmd.AddCommand(
		newSubcommand("start", "Start background workers"),
		newSubcommand("stop", "Stop background workers"),
		newSubcommand("status", "Show worker pool status"),
		newSubcommand("scale", "Adjust worker pool size at runtime"),
	)

	return cmd
}

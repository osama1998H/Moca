package main

import "github.com/spf13/cobra"

// NewMonitorCommand returns the "moca monitor" command group with all subcommands.
func NewMonitorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Monitoring",
		Long:  "Live dashboards, Prometheus metrics, and audit log queries.",
	}

	cmd.AddCommand(
		newSubcommand("live", "Live dashboard showing requests, workers, queues"),
		newSubcommand("metrics", "Dump current Prometheus metrics"),
		newSubcommand("audit", "Query audit log"),
	)

	return cmd
}

package main

import "github.com/spf13/cobra"

// NewSchedulerCommand returns the "moca scheduler" command group with all subcommands.
func NewSchedulerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scheduler",
		Short: "Scheduler management",
		Long:  "Start, stop, and manage the cron scheduler and scheduled jobs.",
	}

	cmd.AddCommand(
		newSubcommand("start", "Start the scheduler process"),
		newSubcommand("stop", "Stop the scheduler"),
		newSubcommand("status", "Show scheduler status"),
		newSubcommand("enable", "Enable scheduler for a site"),
		newSubcommand("disable", "Disable scheduler for a site"),
		newSubcommand("trigger", "Manually trigger a scheduled event"),
		newSubcommand("list-jobs", "List registered scheduled jobs"),
		newSubcommand("purge-jobs", "Purge pending jobs from queue"),
	)

	return cmd
}

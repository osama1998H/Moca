package main

import "github.com/spf13/cobra"

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
		newSubcommand("list", "List dead letter entries"),
		newSubcommand("retry", "Retry a dead letter job"),
		newSubcommand("purge", "Purge dead letter queue"),
	)

	cmd.AddCommand(
		newSubcommand("status", "Show queue depths and worker status"),
		newSubcommand("list", "List pending/active/failed jobs"),
		newSubcommand("retry", "Retry a failed job"),
		newSubcommand("purge", "Purge all pending jobs"),
		newSubcommand("inspect", "Inspect a specific job's payload/history"),
		deadLetter,
	)

	return cmd
}

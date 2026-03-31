package main

import "github.com/spf13/cobra"

// NewEventsCommand returns the "moca events" command group with all subcommands.
func NewEventsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Kafka event management",
		Long:  "List topics, tail events, publish test events, and manage consumers.",
	}

	cmd.AddCommand(
		newSubcommand("list-topics", "List all Kafka topics"),
		newSubcommand("tail", "Tail events from a topic in real-time"),
		newSubcommand("publish", "Publish a test event"),
		newSubcommand("consumer-status", "Show consumer group lag"),
		newSubcommand("replay", "Replay events from a time offset"),
	)

	return cmd
}

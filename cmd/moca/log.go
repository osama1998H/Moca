package main

import "github.com/spf13/cobra"

// NewLogCommand returns the "moca log" command group with all subcommands.
func NewLogCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Log viewing",
		Long:  "Tail, search, and export application logs.",
	}

	cmd.AddCommand(
		newSubcommand("tail", "Tail logs in real-time (with filters)"),
		newSubcommand("search", "Search through log files"),
		newSubcommand("export", "Export logs for a time range"),
	)

	return cmd
}

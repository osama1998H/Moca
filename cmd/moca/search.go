package main

import "github.com/spf13/cobra"

// NewSearchCommand returns the "moca search" command group with all subcommands.
func NewSearchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search index management",
		Long:  "Rebuild, query, and monitor Meilisearch indexes.",
	}

	cmd.AddCommand(
		newSubcommand("rebuild", "Rebuild search index for a site/doctype"),
		newSubcommand("status", "Show search index status"),
		newSubcommand("query", "Query search index from CLI"),
	)

	return cmd
}

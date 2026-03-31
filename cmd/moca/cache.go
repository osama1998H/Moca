package main

import "github.com/spf13/cobra"

// NewCacheCommand returns the "moca cache" command group with all subcommands.
func NewCacheCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Cache management",
		Long:  "Clear caches, view statistics, and pre-warm cache entries.",
	}

	cmd.AddCommand(
		newSubcommand("clear", "Clear all caches for a site"),
		newSubcommand("clear-meta", "Clear metadata cache only"),
		newSubcommand("clear-sessions", "Clear all sessions (logout all users)"),
		newSubcommand("stats", "Show cache hit/miss statistics"),
		newSubcommand("warm", "Pre-warm caches (metadata, hot docs)"),
	)

	return cmd
}

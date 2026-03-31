package main

import "github.com/spf13/cobra"

// NewDevCommand returns the "moca dev" command group with all subcommands.
func NewDevCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Developer tools",
		Long:  "Interactive consoles, benchmarks, profiling, and development utilities.",
	}

	cmd.AddCommand(
		newSubcommand("console", "Interactive Go REPL with framework loaded"),
		newSubcommand("shell", "Open a shell with Moca env vars set"),
		newSubcommand("execute", "Run a one-off Go function/expression"),
		newSubcommand("request", "Make an HTTP request as a user"),
		newSubcommand("bench", "Run microbenchmarks on queries/operations"),
		newSubcommand("profile", "Profile a request or operation"),
		newSubcommand("watch", "Watch and rebuild assets on change"),
		newSubcommand("playground", "Start interactive API playground"),
	)

	return cmd
}

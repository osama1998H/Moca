package main

import "github.com/spf13/cobra"

// NewBuildCommand returns the "moca build" command group with all subcommands.
func NewBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build operations",
		Long:  "Build frontend assets, compile apps, and produce server binaries.",
	}

	cmd.AddCommand(
		newSubcommand("desk", "Build React Desk frontend"),
		newSubcommand("portal", "Build portal/website assets"),
		newSubcommand("assets", "Build all static assets"),
		newSubcommand("app", "Verify an app's Go code compiles"),
		newSubcommand("server", "Compile server binary with all installed apps"),
	)

	return cmd
}

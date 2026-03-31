package main

import "github.com/spf13/cobra"

// NewAppCommand returns the "moca app" command group with all subcommands.
func NewAppCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Application management",
		Long:  "Scaffold, install, update, and manage Moca applications.",
	}

	cmd.AddCommand(
		newSubcommand("new", "Scaffold a new Moca app"),
		newSubcommand("get", "Download and install an app"),
		newSubcommand("remove", "Remove an app from project"),
		newSubcommand("install", "Install an app on a site"),
		newSubcommand("uninstall", "Uninstall an app from a site"),
		newSubcommand("list", "List apps (project or site level)"),
		newSubcommand("update", "Update apps (all or specific)"),
		newSubcommand("resolve", "Resolve and lock dependency versions"),
		newSubcommand("publish", "Publish app to registry"),
		newSubcommand("info", "Show app manifest details"),
		newSubcommand("diff", "Show changes since last install"),
		newSubcommand("pin", "Pin app to exact version/commit"),
	)

	return cmd
}

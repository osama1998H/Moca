package main

import "github.com/spf13/cobra"

// NewSiteCommand returns the "moca site" command group with all subcommands.
func NewSiteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "site",
		Short: "Site management",
		Long:  "Create, manage, and maintain Moca sites (tenants).",
	}

	cmd.AddCommand(
		newSubcommand("create", "Create a new site"),
		newSubcommand("drop", "Delete a site"),
		newSubcommand("list", "List all sites"),
		newSubcommand("use", "Set active site"),
		newSubcommand("info", "Show site details"),
		newSubcommand("browse", "Open site in browser"),
		newSubcommand("reinstall", "Reset site to fresh state"),
		newSubcommand("migrate", "Run pending migrations"),
		newSubcommand("enable", "Enable a disabled site"),
		newSubcommand("disable", "Disable a site (maintenance mode)"),
		newSubcommand("rename", "Rename a site"),
		newSubcommand("clone", "Clone a site (schema + data)"),
	)

	return cmd
}

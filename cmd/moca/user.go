package main

import "github.com/spf13/cobra"

// NewUserCommand returns the "moca user" command group with all subcommands.
func NewUserCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "User management",
		Long:  "Create, manage, and configure user accounts and roles on a site.",
	}

	cmd.AddCommand(
		newSubcommand("add", "Create a new user on a site"),
		newSubcommand("remove", "Remove a user from a site"),
		newSubcommand("set-password", "Set user password"),
		newSubcommand("set-admin-password", "Set Administrator password"),
		newSubcommand("add-role", "Assign a role to a user"),
		newSubcommand("remove-role", "Remove a role from a user"),
		newSubcommand("list", "List all users on a site"),
		newSubcommand("disable", "Disable a user account"),
		newSubcommand("enable", "Enable a user account"),
		newSubcommand("impersonate", "Generate login URL as any user (dev only)"),
	)

	return cmd
}

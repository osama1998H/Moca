package main

import "github.com/spf13/cobra"

// NewDeployCommand returns the "moca deploy" command group with all subcommands.
func NewDeployCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deployment operations",
		Long:  "Set up, update, rollback, and monitor production deployments.",
	}

	cmd.AddCommand(
		newSubcommand("setup", "One-command production setup"),
		newSubcommand("update", "Production update (backup, pull, migrate, restart)"),
		newSubcommand("rollback", "Rollback to previous deployment"),
		newSubcommand("promote", "Promote staging to production"),
		newSubcommand("status", "Show deployment status"),
		newSubcommand("history", "Show deployment history"),
	)

	return cmd
}

package main

import "github.com/spf13/cobra"

// NewBackupCommand returns the "moca backup" command group with all subcommands.
func NewBackupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Backup operations",
		Long:  "Create, restore, and manage site backups.",
	}

	cmd.AddCommand(
		newSubcommand("create", "Backup a site (or all sites)"),
		newSubcommand("restore", "Restore a site from backup"),
		newSubcommand("list", "List available backups"),
		newSubcommand("schedule", "Configure automated backups"),
		newSubcommand("verify", "Verify backup integrity"),
		newSubcommand("upload", "Upload backup to remote storage"),
		newSubcommand("download", "Download backup from remote storage"),
		newSubcommand("prune", "Delete old backups per retention policy"),
	)

	return cmd
}

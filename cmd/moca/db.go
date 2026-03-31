package main

import "github.com/spf13/cobra"

// NewDBCommand returns the "moca db" command group with all subcommands.
func NewDBCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database operations",
		Long:  "Manage database schema, migrations, seeds, and fixtures.",
	}

	cmd.AddCommand(
		newSubcommand("console", "Open interactive psql session"),
		newSubcommand("migrate", "Run pending schema migrations"),
		newSubcommand("rollback", "Rollback last migration batch"),
		newSubcommand("diff", "Show schema diff (meta vs actual DB)"),
		newSubcommand("snapshot", "Save current schema as snapshot"),
		newSubcommand("seed", "Load seed/fixture data"),
		newSubcommand("trim-tables", "Remove orphaned columns"),
		newSubcommand("trim-database", "Remove orphaned tables"),
		newSubcommand("export-fixtures", "Export data as fixture files"),
		newSubcommand("reset", "Drop and recreate site schema"),
	)

	return cmd
}

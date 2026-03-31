package main

import "github.com/spf13/cobra"

// NewConfigCommand returns the "moca config" command group with all subcommands.
// File is named config_cmd.go to avoid collision with internal/config package name.
func NewConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configuration management",
		Long:  "Get, set, compare, and manage project and site configuration.",
	}

	cmd.AddCommand(
		newSubcommand("get", "Get a config value"),
		newSubcommand("set", "Set a config value"),
		newSubcommand("remove", "Remove a config key"),
		newSubcommand("list", "List all effective config (merged)"),
		newSubcommand("diff", "Compare config between environments"),
		newSubcommand("export", "Export full config as YAML/JSON"),
		newSubcommand("import", "Import config from file"),
		newSubcommand("edit", "Open config in $EDITOR"),
	)

	return cmd
}

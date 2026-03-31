package main

import "github.com/spf13/cobra"

// NewTranslateCommand returns the "moca translate" command group with all subcommands.
func NewTranslateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "translate",
		Short: "Translation management",
		Long:  "Export, import, and compile translations for internationalization.",
	}

	cmd.AddCommand(
		newSubcommand("export", "Export translatable strings"),
		newSubcommand("import", "Import translations"),
		newSubcommand("status", "Show translation coverage"),
		newSubcommand("compile", "Compile translations to binary format"),
	)

	return cmd
}

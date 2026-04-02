package main

import (
	"github.com/osama1998H/moca/internal/output"
	"github.com/spf13/cobra"
)

// notImplemented returns a RunE function that returns a CLIError indicating
// the command is not yet implemented. Used for all placeholder subcommands
// in the command scaffold (MS-07-T4).
func notImplemented() func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		return output.NewCLIError("not implemented").
			WithFix("This command will be available in a future release.")
	}
}

// newSubcommand creates a placeholder subcommand with a not-implemented RunE.
func newSubcommand(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE:  notImplemented(),
	}
}

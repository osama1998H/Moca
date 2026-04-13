package main

import "github.com/spf13/cobra"

// NewTestCommand returns the "moca test" command group with all subcommands.
// File is named test_cmd.go to avoid Go's reserved "test" package name.
func NewTestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Testing",
		Long:  "Run tests, generate coverage reports, and manage test fixtures.",
	}

	cmd.AddCommand(
		newTestRunCmd(),
		newSubcommand("run-ui", "Run frontend/Playwright tests"),
		newTestCoverageCmd(),
		newSubcommand("fixtures", "Load test fixture data"),
		newTestFactoryCmd(),
	)

	return cmd
}

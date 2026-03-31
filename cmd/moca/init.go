package main

import "github.com/spf13/cobra"

// NewInitCommand returns the "moca init" command.
func NewInitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize a new Moca project",
		Long:  "Create a new Moca project with moca.yaml, directory structure, and initial configuration.",
		RunE:  notImplemented(),
	}
}

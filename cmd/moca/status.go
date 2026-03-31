package main

import "github.com/spf13/cobra"

// NewStatusCommand returns the "moca status" command.
func NewStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show project/site/service status",
		Long:  "Display the current project, active site, running services, and environment information.",
		RunE:  notImplemented(),
	}
}

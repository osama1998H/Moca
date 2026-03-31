package main

import "github.com/spf13/cobra"

// NewRestartCommand returns the "moca restart" command.
func NewRestartCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart all running Moca processes",
		Long:  "Restart all Moca processes (server, workers, scheduler).",
		RunE:  notImplemented(),
	}
}

package main

import "github.com/spf13/cobra"

// NewStopCommand returns the "moca stop" command.
func NewStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop all running Moca processes",
		Long:  "Gracefully stop all Moca processes (server, workers, scheduler).",
		RunE:  notImplemented(),
	}
}

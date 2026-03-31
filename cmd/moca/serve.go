package main

import "github.com/spf13/cobra"

// NewServeCommand returns the "moca serve" command (and its "start" alias).
func NewServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "serve",
		Aliases: []string{"start"},
		Short:   "Start the development server",
		Long:    "Start all Moca processes (HTTP server, workers, scheduler) for local development.",
		RunE:    notImplemented(),
	}
}

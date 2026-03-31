package main

import "github.com/spf13/cobra"

// allCommands returns all framework-internal commands for registration on the
// root command. Each command group uses the explicit constructor pattern
// (NewXxxCommand()); the init() pattern is reserved for app-contributed commands
// per ADR-005.
//
// Design ref: MOCA_CLI_SYSTEM_DESIGN.md §4.1 (lines 349–559)
func allCommands() []*cobra.Command {
	return []*cobra.Command{
		// Top-level commands
		NewInitCommand(),
		NewStatusCommand(),
		NewDoctorCommand(),
		NewServeCommand(),
		NewStopCommand(),
		NewRestartCommand(),

		// Command groups
		NewSiteCommand(),
		NewAppCommand(),
		NewWorkerCommand(),
		NewSchedulerCommand(),
		NewDBCommand(),
		NewBackupCommand(),
		NewConfigCommand(),
		NewDeployCommand(),
		NewGenerateCommand(),
		NewDevCommand(),
		NewTestCommand(),
		NewBuildCommand(),
		NewUserCommand(),
		NewAPICommand(),
		NewSearchCommand(),
		NewCacheCommand(),
		NewQueueCommand(),
		NewEventsCommand(),
		NewTranslateCommand(),
		NewLogCommand(),
		NewMonitorCommand(),
	}
}

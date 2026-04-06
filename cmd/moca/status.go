package main

import (
	"fmt"

	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/internal/process"
	"github.com/spf13/cobra"
)

// NewStatusCommand returns the "moca status" command.
func NewStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show project/site/service status",
		Long:  "Display the current project, active site, running services, and environment information.",
		RunE:  runStatus,
	}
}

type serviceStatus struct {
	Name    string `json:"name"`
	PID     int    `json:"pid,omitempty"`
	Running bool   `json:"running"`
	State   string `json:"state"`
}

type statusReport struct {
	ProjectName string          `json:"project_name"`
	ProjectRoot string          `json:"project_root"`
	ActiveSite  string          `json:"active_site"`
	Server      serviceStatus   `json:"server"`
	Worker      serviceStatus   `json:"worker"`
	Scheduler   serviceStatus   `json:"scheduler"`
	Services    []serviceStatus `json:"services"`
}

func runStatus(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	report := statusReport{
		ProjectName: ctx.Project.Project.Name,
		ProjectRoot: ctx.ProjectRoot,
		ActiveSite:  "none",
		Server:      readServerStatus(ctx.ProjectRoot),
		Worker:      readNamedStatus(ctx.ProjectRoot, "worker"),
		Scheduler:   readNamedStatus(ctx.ProjectRoot, "scheduler"),
	}
	if ctx.Site != "" {
		report.ActiveSite = ctx.Site
	}
	report.Services = []serviceStatus{report.Server, report.Worker, report.Scheduler}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(report)
	}

	if w.Mode() == output.ModeTable {
		rows := [][]string{
			{"Project", report.ProjectName},
			{"Root", report.ProjectRoot},
			{"Active site", report.ActiveSite},
			{"Server", formatServiceStatus(report.Server)},
			{"Worker", formatServiceStatus(report.Worker)},
			{"Scheduler", formatServiceStatus(report.Scheduler)},
		}
		return w.PrintTable([]string{"KEY", "VALUE"}, rows)
	}

	w.Print("Project: %s", report.ProjectName)
	w.Print("Root: %s", report.ProjectRoot)
	w.Print("Active site: %s", report.ActiveSite)
	w.Print("Server: %s", formatServiceStatus(report.Server))
	w.Print("Worker: %s", formatServiceStatus(report.Worker))
	w.Print("Scheduler: %s", formatServiceStatus(report.Scheduler))
	return nil
}

func readServerStatus(projectRoot string) serviceStatus {
	status := serviceStatus{Name: "server", State: "stopped"}
	pid, err := process.ReadPID(projectRoot)
	if err != nil {
		return status
	}
	status.PID = pid
	status.Running = process.IsRunning(pid)
	if status.Running {
		status.State = "running"
	} else {
		status.State = "stale"
	}
	return status
}

func readNamedStatus(projectRoot, name string) serviceStatus {
	status := serviceStatus{Name: name, State: "stopped"}
	pid, running, err := processStatus(projectRoot, name)
	if err != nil {
		return status
	}
	status.PID = pid
	status.Running = running
	if running {
		status.State = "running"
	} else {
		status.State = "stale"
	}
	return status
}

func formatServiceStatus(status serviceStatus) string {
	switch {
	case status.Running:
		return fmt.Sprintf("running (PID %d)", status.PID)
	case status.PID > 0:
		return fmt.Sprintf("stale PID %d", status.PID)
	default:
		return "stopped"
	}
}

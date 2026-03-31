package main

import (
	clicontext "github.com/moca-framework/moca/internal/context"
	"github.com/moca-framework/moca/internal/output"
	"github.com/spf13/cobra"
)

// DoctorCheck is the interface for health checks run by "moca doctor".
// Each check tests one aspect of the system and returns a result.
type DoctorCheck interface {
	Name() string
	Run(ctx *clicontext.CLIContext) DoctorResult
}

// DoctorStatus represents the outcome of a health check.
type DoctorStatus string

const (
	DoctorPass DoctorStatus = "pass"
	DoctorWarn DoctorStatus = "warn"
	DoctorFail DoctorStatus = "fail"
	DoctorSkip DoctorStatus = "skip"
)

// DoctorResult holds the outcome of a single health check.
type DoctorResult struct {
	Name    string       `json:"name"`
	Status  DoctorStatus `json:"status"`
	Message string       `json:"message"`
}

// NewDoctorCommand returns the "moca doctor" command.
func NewDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose system health",
		Long:  "Run health checks to verify that the project, configuration, and external services are working correctly.",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.NewWriter(cmd)
			ctx := clicontext.FromCommand(cmd)

			checks := defaultChecks()
			results := make([]DoctorResult, 0, len(checks))
			for _, check := range checks {
				results = append(results, check.Run(ctx))
			}

			if w.Mode() == output.ModeJSON {
				return w.PrintJSON(results)
			}

			w.Print("System Health Check\n")

			headers := []string{"CHECK", "STATUS", "MESSAGE"}
			rows := make([][]string, 0, len(results))
			for _, r := range results {
				rows = append(rows, []string{r.Name, formatStatus(r.Status, w.Color()), r.Message})
			}
			return w.PrintTable(headers, rows)
		},
	}
}

// defaultChecks returns the skeleton health checks for the doctor command.
func defaultChecks() []DoctorCheck {
	return []DoctorCheck{
		&projectCheck{},
		&configCheck{},
		&postgresCheck{},
		&redisCheck{},
	}
}

func formatStatus(s DoctorStatus, cc *output.ColorConfig) string {
	switch s {
	case DoctorPass:
		return cc.Success("PASS")
	case DoctorWarn:
		return cc.Warning("WARN")
	case DoctorFail:
		return cc.Error("FAIL")
	case DoctorSkip:
		return cc.Muted("SKIP")
	default:
		return string(s)
	}
}

// projectCheck verifies that a Moca project was detected.
type projectCheck struct{}

func (c *projectCheck) Name() string { return "Project detected" }
func (c *projectCheck) Run(ctx *clicontext.CLIContext) DoctorResult {
	if ctx == nil || ctx.Project == nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: "No moca.yaml found in current or parent directories",
		}
	}
	return DoctorResult{
		Name:    c.Name(),
		Status:  DoctorPass,
		Message: "Found project at " + ctx.ProjectRoot,
	}
}

// configCheck verifies that the project configuration is valid.
type configCheck struct{}

func (c *configCheck) Name() string { return "Config valid" }
func (c *configCheck) Run(ctx *clicontext.CLIContext) DoctorResult {
	if ctx == nil || ctx.Project == nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorSkip,
			Message: "No project detected",
		}
	}
	return DoctorResult{
		Name:    c.Name(),
		Status:  DoctorPass,
		Message: "moca.yaml parsed and validated successfully",
	}
}

// postgresCheck is a placeholder for PostgreSQL connectivity checking.
type postgresCheck struct{}

func (c *postgresCheck) Name() string { return "PostgreSQL reachable" }
func (c *postgresCheck) Run(_ *clicontext.CLIContext) DoctorResult {
	return DoctorResult{
		Name:    c.Name(),
		Status:  DoctorSkip,
		Message: "Driver check not yet implemented",
	}
}

// redisCheck is a placeholder for Redis connectivity checking.
type redisCheck struct{}

func (c *redisCheck) Name() string { return "Redis reachable" }
func (c *redisCheck) Run(_ *clicontext.CLIContext) DoctorResult {
	return DoctorResult{
		Name:    c.Name(),
		Status:  DoctorSkip,
		Message: "Driver check not yet implemented",
	}
}

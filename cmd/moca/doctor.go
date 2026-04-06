package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/internal/config"
	clicontext "github.com/osama1998H/moca/internal/context"
	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/pkg/search"
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

const doctorTimeout = 3 * time.Second

var (
	doctorCheckPostgres = func(ctx context.Context, cfg config.DatabaseConfig) error {
		ctx, cancel := context.WithTimeout(ctx, doctorTimeout)
		defer cancel()

		pool, err := pgxpool.New(ctx, buildInitDSN(cfg))
		if err != nil {
			return err
		}
		defer pool.Close()

		return pool.Ping(ctx)
	}
	doctorCheckRedis = func(ctx context.Context, cfg config.RedisConfig) error {
		ctx, cancel := context.WithTimeout(ctx, doctorTimeout)
		defer cancel()

		client := goredis.NewClient(&goredis.Options{
			Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
			Password: cfg.Password,
			DB:       cfg.DbCache,
		})
		defer client.Close() //nolint:errcheck

		return client.Ping(ctx).Err()
	}
	doctorCheckMeilisearch = func(ctx context.Context, cfg config.SearchConfig) error {
		ctx, cancel := context.WithTimeout(ctx, doctorTimeout)
		defer cancel()

		client, err := search.NewClient(cfg)
		if err != nil {
			return err
		}
		defer client.Close()

		_, err = client.ListIndexes(ctx, "")
		return err
	}
)

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
			hasFailure := false
			for _, check := range checks {
				result := check.Run(ctx)
				results = append(results, result)
				if result.Status == DoctorFail {
					hasFailure = true
				}
			}

			if w.Mode() == output.ModeJSON {
				if err := w.PrintJSON(results); err != nil {
					return err
				}
				if hasFailure {
					return errors.New("one or more health checks failed")
				}
				return nil
			}

			w.Print("System Health Check\n")

			headers := []string{"CHECK", "STATUS", "MESSAGE"}
			rows := make([][]string, 0, len(results))
			for _, r := range results {
				rows = append(rows, []string{r.Name, formatStatus(r.Status, w.Color()), r.Message})
			}
			if err := w.PrintTable(headers, rows); err != nil {
				return err
			}
			if hasFailure {
				return errors.New("one or more health checks failed")
			}
			return nil
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
		&meiliCheck{},
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
func (c *postgresCheck) Run(ctx *clicontext.CLIContext) DoctorResult {
	if ctx == nil || ctx.Project == nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorSkip,
			Message: "No project detected",
		}
	}
	if err := doctorCheckPostgres(context.Background(), ctx.Project.Infrastructure.Database); err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: err.Error(),
		}
	}
	return DoctorResult{
		Name:    c.Name(),
		Status:  DoctorPass,
		Message: "Connected successfully",
	}
}

// redisCheck verifies Redis connectivity using the cache database.
type redisCheck struct{}

func (c *redisCheck) Name() string { return "Redis reachable" }
func (c *redisCheck) Run(ctx *clicontext.CLIContext) DoctorResult {
	if ctx == nil || ctx.Project == nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorSkip,
			Message: "No project detected",
		}
	}
	if err := doctorCheckRedis(context.Background(), ctx.Project.Infrastructure.Redis); err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: err.Error(),
		}
	}
	return DoctorResult{
		Name:    c.Name(),
		Status:  DoctorPass,
		Message: "Connected successfully",
	}
}

// meiliCheck verifies Meilisearch connectivity when configured.
type meiliCheck struct{}

func (c *meiliCheck) Name() string { return "Meilisearch reachable" }
func (c *meiliCheck) Run(ctx *clicontext.CLIContext) DoctorResult {
	if ctx == nil || ctx.Project == nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorSkip,
			Message: "No project detected",
		}
	}

	cfg := ctx.Project.Infrastructure.Search
	if cfg.Engine == "" || cfg.Host == "" {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorSkip,
			Message: "Search not configured",
		}
	}

	err := doctorCheckMeilisearch(context.Background(), cfg)
	if err == nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorPass,
			Message: "Connected successfully",
		}
	}
	if errors.Is(err, search.ErrUnavailable) {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorSkip,
			Message: "Search not configured for Meilisearch",
		}
	}
	return DoctorResult{
		Name:    c.Name(),
		Status:  DoctorFail,
		Message: err.Error(),
	}
}

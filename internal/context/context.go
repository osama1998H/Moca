package clicontext

import (
	"context"

	"github.com/moca-framework/moca/internal/config"
	"github.com/spf13/cobra"
)

// contextKey is an unexported type for context keys in this package,
// preventing collisions with keys defined in other packages.
// Pattern: pkg/api/context.go
type contextKey int

const (
	cliContextKey contextKey = iota
)

// CLIContext carries the resolved project, site, and environment information
// determined by the 6-level priority pipeline. It is stored in the command's
// context.Context via WithCLIContext and retrieved via FromContext/FromCommand.
type CLIContext struct {
	// ProjectRoot is the absolute path to the directory containing moca.yaml.
	// Empty when no project was detected.
	ProjectRoot string

	// Project is the parsed and validated project configuration.
	// nil when no project was detected (e.g. running "moca version" outside a project).
	Project *config.ProjectConfig

	// Site is the resolved target site name.
	// Empty when no site was determined.
	Site string

	// Environment is the resolved target environment (e.g. "development", "production").
	// Always non-empty — defaults to "development".
	Environment string
}

// Resolve runs the context detection pipeline and returns a populated CLIContext.
// It never errors on a missing project — only on a project that exists but has
// invalid configuration. Commands that require a project check ctx.Project != nil.
func Resolve(cmd *cobra.Command) (*CLIContext, error) {
	projectRoot, project, err := resolveProject(cmd)
	if err != nil {
		return nil, err
	}

	site := resolveSite(cmd, projectRoot)
	environment := resolveEnvironment(cmd, projectRoot)

	return &CLIContext{
		ProjectRoot: projectRoot,
		Project:     project,
		Site:        site,
		Environment: environment,
	}, nil
}

// WithCLIContext stores the CLIContext in a context.Context value.
func WithCLIContext(ctx context.Context, cc *CLIContext) context.Context {
	return context.WithValue(ctx, cliContextKey, cc)
}

// FromContext retrieves the *CLIContext stored by WithCLIContext.
// Returns nil if no CLIContext is present.
func FromContext(ctx context.Context) *CLIContext {
	cc, _ := ctx.Value(cliContextKey).(*CLIContext)
	return cc
}

// FromCommand is a convenience helper that extracts the CLIContext from a
// Cobra command's context. Returns nil if no CLIContext is present.
func FromCommand(cmd *cobra.Command) *CLIContext {
	return FromContext(cmd.Context())
}

package config

import (
	"fmt"
	"strings"
)

// LoadAndResolve is the canonical pipeline for loading a moca.yaml file:
//
//  1. Parse the file (with env-var expansion).
//  2. Validate all required and format-constrained fields.
//  3. Resolve staging inheritance.
//
// If parsing fails, a *ConfigError is returned.
// If validation fails, a *ValidationErrors error is returned listing every
// field-level problem.
// On success, the fully-resolved *ProjectConfig is returned.
func LoadAndResolve(path string) (*ProjectConfig, error) {
	cfg, err := ParseFile(path)
	if err != nil {
		return nil, err
	}

	if errs := Validate(cfg); len(errs) > 0 {
		return nil, &ValidationErrors{Errors: errs}
	}

	ResolveInheritance(cfg)
	setObservabilityDefaults(cfg)

	return cfg, nil
}

// setObservabilityDefaults fills in zero-value defaults for the observability
// configuration section. Called after inheritance resolution.
func setObservabilityDefaults(cfg *ProjectConfig) {
	if cfg.Observability.Metrics.Path == "" {
		cfg.Observability.Metrics.Path = "/metrics"
	}
	if cfg.Observability.Tracing.Exporter == "" {
		cfg.Observability.Tracing.Exporter = "otlp"
	}
	if cfg.Observability.Tracing.Endpoint == "" {
		cfg.Observability.Tracing.Endpoint = "localhost:4317"
	}
	if cfg.Observability.Tracing.SampleRate == 0 {
		cfg.Observability.Tracing.SampleRate = 1.0
	}
}

// ValidationErrors wraps a slice of ValidationError values so that the
// load pipeline can return them as a single error value.
type ValidationErrors struct {
	Errors []ValidationError
}

// Error implements the error interface. It formats every field-level error on
// its own line, prefixed with "config validation failed:".
func (e *ValidationErrors) Error() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "config validation failed (%d error(s)):\n", len(e.Errors))
	for _, ve := range e.Errors {
		fmt.Fprintf(&sb, "  %s: %s\n", ve.Field, ve.Message)
	}
	return sb.String()
}

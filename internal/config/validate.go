package config

import (
	"regexp"
	"time"
)

// semverPattern matches a basic semantic version: MAJOR.MINOR.PATCH with optional
// pre-release (-alpha.1) and build metadata (+build.1) suffixes. An optional "v"
// prefix is accepted (e.g., "v1.0.0").
var semverPattern = regexp.MustCompile(
	`^v?\d+\.\d+\.\d+(-[a-zA-Z0-9.]+)?(\+[a-zA-Z0-9.]+)?$`,
)

// semverConstraintPattern accepts common semver constraint expressions used in
// moca.yaml. Full constraint evaluation is deferred to MS-09; this is a basic
// format check for MS-01.
//
// Accepted forms: "^1.0.0", "~2.1.0", ">=1.0.0", "<=2.0.0", ">1.0.0",
// "<2.0.0", "=1.0.0", "1.0.0", "*", ">=1.0.0, <2.0.0".
var semverConstraintPattern = regexp.MustCompile(
	`^(\*|([~^]|[><=!]=?)\s*v?\d+(\.\d+(\.\d+)?)?(-[a-zA-Z0-9.]+)?` +
		`(\s*,\s*([~^]|[><=!]=?)\s*v?\d+(\.\d+(\.\d+)?)?)*)$`,
)

// Validate walks cfg and returns all validation errors found.
// An empty (or nil) slice means the config is valid.
// Errors are accumulated — all fields are checked even after the first failure.
func Validate(cfg *ProjectConfig) []ValidationError {
	v := &validator{}

	// moca: required, basic semver constraint format
	v.requireNonEmpty("moca", cfg.Moca)
	if cfg.Moca != "" && !semverConstraintPattern.MatchString(cfg.Moca) {
		v.addError("moca", `must be a valid semver constraint (e.g., "^1.0.0", "~2.1.0", ">=1.0.0")`)
	}

	// project.name: required
	v.requireNonEmpty("project.name", cfg.Project.Name)

	// project.version: required, valid semver
	v.requireNonEmpty("project.version", cfg.Project.Version)
	if cfg.Project.Version != "" && !semverPattern.MatchString(cfg.Project.Version) {
		v.addError("project.version", `must be a valid semantic version (e.g., "1.0.0")`)
	}

	// apps.core: the "core" app must be present
	if _, ok := cfg.Apps["core"]; !ok {
		v.addError("apps.core", "required: core app must be defined")
	}

	// infrastructure.database
	v.requireNonEmpty("infrastructure.database.host", cfg.Infrastructure.Database.Host)
	v.validatePort("infrastructure.database.port", cfg.Infrastructure.Database.Port)

	// infrastructure.redis
	v.requireNonEmpty("infrastructure.redis.host", cfg.Infrastructure.Redis.Host)
	v.validatePort("infrastructure.redis.port", cfg.Infrastructure.Redis.Port)

	// Optional ports — validate range only if non-zero (field was set)
	v.validatePortIfSet("infrastructure.search.port", cfg.Infrastructure.Search.Port)
	v.validatePortIfSet("development.port", cfg.Development.Port)
	v.validatePortIfSet("development.desk_port", cfg.Development.DeskPort)
	v.validatePortIfSet("production.port", cfg.Production.Port)

	// scheduler.tick_interval: must be a valid Go duration if set
	if cfg.Scheduler.TickInterval != "" {
		if _, err := time.ParseDuration(cfg.Scheduler.TickInterval); err != nil {
			v.addError("scheduler.tick_interval",
				`must be a valid Go duration (e.g., "5m", "30s", "1h")`)
		}
	}

	// staging.inherits: only "production" is a valid target
	if cfg.Staging.Inherits != "" && cfg.Staging.Inherits != "production" {
		v.addError("staging.inherits", `must be "production" or empty`)
	}

	return v.errs
}

// validator accumulates ValidationErrors during a single Validate call.
type validator struct {
	errs []ValidationError
}

func (v *validator) addError(field, message string) {
	v.errs = append(v.errs, ValidationError{Field: field, Message: message})
}

// requireNonEmpty adds an error if value is the empty string.
func (v *validator) requireNonEmpty(field, value string) {
	if value == "" {
		v.addError(field, "required")
	}
}

// validatePort adds an error if port is 0 (missing) or outside 1–65535.
// Use this for required port fields.
func (v *validator) validatePort(field string, port int) {
	switch {
	case port == 0:
		v.addError(field, "required")
	case port < 1 || port > 65535:
		v.addError(field, "must be between 1 and 65535")
	}
}

// validatePortIfSet adds an error if port is outside 1–65535.
// A value of 0 is treated as "not set" and is not an error.
func (v *validator) validatePortIfSet(field string, port int) {
	if port != 0 && (port < 1 || port > 65535) {
		v.addError(field, "must be between 1 and 65535")
	}
}

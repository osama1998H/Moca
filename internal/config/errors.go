package config

import "fmt"

// ConfigError represents a configuration error with optional file path and line number context.
// It is used by the parser, env expansion, and validator to produce user-friendly messages.
type ConfigError struct {
	Err     error
	File    string
	Message string
	Line    int
}

// Error implements the error interface.
// Format: "path/to/moca.yaml:12: message" or "config: message" when file/line are absent.
func (e *ConfigError) Error() string {
	switch {
	case e.File != "" && e.Line > 0:
		return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Message)
	case e.File != "":
		return fmt.Sprintf("%s: %s", e.File, e.Message)
	default:
		return fmt.Sprintf("config: %s", e.Message)
	}
}

// Unwrap returns the underlying error for use with errors.Is / errors.As.
func (e *ConfigError) Unwrap() error {
	return e.Err
}

// ValidationError represents a single field-level validation failure.
// Field is the dot-path to the offending field (e.g., "infrastructure.database.port").
// Message describes what is wrong (e.g., "required", "must be between 1 and 65535").
type ValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// EnvExpandError is returned when one or more environment variables referenced in the
// config file are not set. It lists every missing variable name.
type EnvExpandError struct {
	// Missing is the list of undefined variable names found in the config.
	Missing []string
}

// Error implements the error interface.
func (e *EnvExpandError) Error() string {
	if len(e.Missing) == 1 {
		return fmt.Sprintf("config: environment variable %q is not set", e.Missing[0])
	}
	return fmt.Sprintf("config: %d environment variables are not set: %v", len(e.Missing), e.Missing)
}

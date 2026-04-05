package config

import (
	"errors"
	"strings"
	"testing"
)

func TestConfigError_WithFileAndLine(t *testing.T) {
	err := &ConfigError{File: "moca.yaml", Line: 12, Message: "invalid syntax"}
	want := "moca.yaml:12: invalid syntax"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestConfigError_WithFileOnly(t *testing.T) {
	err := &ConfigError{File: "moca.yaml", Message: "could not parse"}
	want := "moca.yaml: could not parse"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestConfigError_NoFileNoLine(t *testing.T) {
	err := &ConfigError{Message: "unknown error"}
	want := "config: unknown error"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestConfigError_Unwrap(t *testing.T) {
	inner := errors.New("underlying")
	err := &ConfigError{Err: inner, Message: "wrapper"}
	if !errors.Is(err, inner) {
		t.Error("Unwrap should return inner error")
	}
}

func TestConfigError_Unwrap_Nil(t *testing.T) {
	err := &ConfigError{Message: "no inner"}
	if err.Unwrap() != nil {
		t.Error("expected nil Unwrap when no inner error")
	}
}

func TestValidationError(t *testing.T) {
	err := &ValidationError{Field: "infrastructure.database.port", Message: "required"}
	want := "infrastructure.database.port: required"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestEnvExpandError_Single(t *testing.T) {
	err := &EnvExpandError{Missing: []string{"DB_HOST"}}
	msg := err.Error()
	if !strings.Contains(msg, "DB_HOST") {
		t.Errorf("error = %q, want DB_HOST", msg)
	}
	if !strings.Contains(msg, "is not set") {
		t.Errorf("error = %q, want 'is not set'", msg)
	}
}

func TestEnvExpandError_Multiple(t *testing.T) {
	err := &EnvExpandError{Missing: []string{"DB_HOST", "DB_PORT", "DB_NAME"}}
	msg := err.Error()
	if !strings.Contains(msg, "3 environment variables") {
		t.Errorf("error = %q", msg)
	}
}

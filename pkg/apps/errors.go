package apps

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors for app installation operations.
var (
	ErrAppNotFound         = errors.New("app not found")
	ErrAppAlreadyInstalled = errors.New("app already installed on site")
	ErrAppNotInstalled     = errors.New("app not installed on site")
)

// ManifestError represents a manifest parsing or loading error with file path context.
type ManifestError struct {
	Err     error
	File    string
	Message string
}

// Error implements the error interface.
// Format: "path/to/manifest.yaml: message" or "manifest: message" when file is absent.
func (e *ManifestError) Error() string {
	if e.File != "" {
		return fmt.Sprintf("%s: %s", e.File, e.Message)
	}
	return fmt.Sprintf("manifest: %s", e.Message)
}

// Unwrap returns the underlying error for use with errors.Is / errors.As.
func (e *ManifestError) Unwrap() error {
	return e.Err
}

// ValidationError represents a single field-level validation failure.
// Field is the dot-path to the offending field (e.g., "name", "modules[0].name").
type ValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors collects multiple ValidationError values as a single error.
type ValidationErrors []ValidationError

// Error implements the error interface.
func (e ValidationErrors) Error() string {
	msgs := make([]string, len(e))
	for i, ve := range e {
		msgs[i] = ve.Error()
	}
	return fmt.Sprintf("manifest validation failed:\n  %s", strings.Join(msgs, "\n  "))
}

// DependencyError represents an error in inter-app dependency resolution.
type DependencyError struct {
	Message string
	// Cycle contains the app names forming the cycle, if applicable.
	Cycle []string
}

// Error implements the error interface.
func (e *DependencyError) Error() string {
	if len(e.Cycle) > 0 {
		return fmt.Sprintf("circular app dependency: %s", strings.Join(e.Cycle, " -> "))
	}
	return e.Message
}

package output

import (
	"fmt"
	"io"
	"strings"
)

// CLIError is a rich error type for user-facing CLI error messages.
// It implements the error interface and provides structured formatting
// per MOCA_CLI_SYSTEM_DESIGN.md §7 (lines 3297–3359).
//
// All fields except Message are optional. Format omits empty sections.
type CLIError struct {
	Err       error  // Wrapped error for errors.Is/As support
	Message   string // What happened (required)
	Context   string // Relevant state that caused the error
	Cause     string // Why it happened, if determinable
	Fix       string // Exactly what command to run or action to take
	Reference string // Link to docs if applicable
}

// Error satisfies the error interface. Returns Message, appending the
// wrapped error if present.
func (e *CLIError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

// Unwrap returns the wrapped error for errors.Is/As chain support.
func (e *CLIError) Unwrap() error {
	return e.Err
}

// Format writes the full rich error to w with optional color.
// Empty sections are omitted entirely.
func (e *CLIError) Format(w io.Writer, cc *ColorConfig) {
	_, _ = fmt.Fprintf(w, "%s %s\n", cc.Bold(cc.Error("Error:")), e.Message)

	if e.Context != "" {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, cc.Muted("Context:"))
		writeIndented(w, e.Context)
	}

	cause := e.Cause
	if cause == "" && e.Err != nil {
		cause = e.Err.Error()
	}
	if cause != "" {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, cc.Muted("Cause:"))
		writeIndented(w, cause)
	}

	if e.Fix != "" {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, cc.Muted("Fix:"))
		writeIndented(w, e.Fix)
	}

	if e.Reference != "" {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, cc.Muted("Reference:"))
		_, _ = fmt.Fprintf(w, "  %s\n", cc.Info(e.Reference))
	}
}

// FormatPlain writes the rich error without color codes.
func (e *CLIError) FormatPlain(w io.Writer) {
	e.Format(w, &ColorConfig{enabled: false})
}

// writeIndented writes each line of text prefixed with two spaces.
func writeIndented(w io.Writer, text string) {
	for _, line := range strings.Split(text, "\n") {
		_, _ = fmt.Fprintf(w, "  %s\n", line)
	}
}

// NewCLIError creates a CLIError with the given message.
// Use the With* methods for optional fields.
func NewCLIError(message string) *CLIError {
	return &CLIError{Message: message}
}

// WithContext sets the Context field and returns e for chaining.
func (e *CLIError) WithContext(ctx string) *CLIError {
	e.Context = ctx
	return e
}

// WithCause sets the Cause field and returns e for chaining.
func (e *CLIError) WithCause(cause string) *CLIError {
	e.Cause = cause
	return e
}

// WithFix sets the Fix field and returns e for chaining.
func (e *CLIError) WithFix(fix string) *CLIError {
	e.Fix = fix
	return e
}

// WithReference sets the Reference field and returns e for chaining.
func (e *CLIError) WithReference(ref string) *CLIError {
	e.Reference = ref
	return e
}

// WithErr sets the wrapped error and returns e for chaining.
func (e *CLIError) WithErr(err error) *CLIError {
	e.Err = err
	return e
}

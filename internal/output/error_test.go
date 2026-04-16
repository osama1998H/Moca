package output

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestCLIError_Error(t *testing.T) {
	e := &CLIError{Message: "something broke"}
	if got := e.Error(); got != "something broke" {
		t.Errorf("Error() = %q, want %q", got, "something broke")
	}
}

func TestCLIError_ErrorWithWrapped(t *testing.T) {
	inner := errors.New("connection refused")
	e := &CLIError{Message: "cannot connect", Err: inner}
	got := e.Error()
	want := "cannot connect: connection refused"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestCLIError_Unwrap(t *testing.T) {
	inner := errors.New("root cause")
	e := &CLIError{Message: "wrapper", Err: inner}
	if !errors.Is(e, inner) {
		t.Error("errors.Is should find the wrapped error")
	}
}

func TestCLIError_UnwrapNil(t *testing.T) {
	e := &CLIError{Message: "no wrap"}
	if e.Unwrap() != nil {
		t.Error("Unwrap() should return nil when Err is not set")
	}
}

func TestCLIError_FormatAllFields(t *testing.T) {
	e := &CLIError{
		Message:   "Cannot connect to PostgreSQL",
		Context:   "Host: localhost:5432\nDatabase: moca_system",
		Cause:     "Connection refused — PostgreSQL may not be running.",
		Fix:       "1. Start PostgreSQL: sudo systemctl start postgresql",
		Reference: "https://docs.moca.dev/troubleshooting/database-connection",
	}

	var buf bytes.Buffer
	cc := &ColorConfig{enabled: false}
	e.Format(&buf, cc)
	out := buf.String()

	// Verify all section labels are present.
	for _, label := range []string{"Error:", "Context:", "Cause:", "Fix:", "Reference:"} {
		if !strings.Contains(out, label) {
			t.Errorf("Format output missing %q label", label)
		}
	}

	// Verify content is present.
	for _, content := range []string{
		"Cannot connect to PostgreSQL",
		"localhost:5432",
		"Connection refused",
		"systemctl start postgresql",
		"https://docs.moca.dev",
	} {
		if !strings.Contains(out, content) {
			t.Errorf("Format output missing content %q", content)
		}
	}
}

func TestCLIError_FormatOptionalFieldsOmitted(t *testing.T) {
	e := &CLIError{Message: "something failed"}

	var buf bytes.Buffer
	cc := &ColorConfig{enabled: false}
	e.Format(&buf, cc)
	out := buf.String()

	if !strings.Contains(out, "Error:") {
		t.Error("Format output missing Error: label")
	}

	// Optional sections should NOT appear.
	for _, label := range []string{"Context:", "Cause:", "Fix:", "Reference:"} {
		if strings.Contains(out, label) {
			t.Errorf("Format output should not contain %q when field is empty", label)
		}
	}
}

func TestCLIError_FormatWithColor(t *testing.T) {
	e := &CLIError{
		Message: "test error",
		Fix:     "try again",
	}

	var buf bytes.Buffer
	cc := &ColorConfig{enabled: true}
	e.Format(&buf, cc)
	out := buf.String()

	// Check for ANSI escape codes.
	if !strings.Contains(out, "\033[") {
		t.Error("colored Format output should contain ANSI escape codes")
	}
}

func TestCLIError_FormatPlain(t *testing.T) {
	e := &CLIError{
		Message: "test error",
		Fix:     "try again",
	}

	var buf bytes.Buffer
	e.FormatPlain(&buf)
	out := buf.String()

	if strings.Contains(out, "\033[") {
		t.Error("FormatPlain output should not contain ANSI escape codes")
	}
}

func TestCLIError_Builder(t *testing.T) {
	e := NewCLIError("migration failed").
		WithContext("site: acme.localhost").
		WithCause("column already exists").
		WithFix("moca db diff --site acme.localhost").
		WithReference("https://docs.moca.dev/migrations").
		WithErr(errors.New("SQL error"))

	if e.Message != "migration failed" {
		t.Errorf("Message = %q", e.Message)
	}
	if e.Context != "site: acme.localhost" {
		t.Errorf("Context = %q", e.Context)
	}
	if e.Cause != "column already exists" {
		t.Errorf("Cause = %q", e.Cause)
	}
	if e.Fix != "moca db diff --site acme.localhost" {
		t.Errorf("Fix = %q", e.Fix)
	}
	if e.Reference != "https://docs.moca.dev/migrations" {
		t.Errorf("Reference = %q", e.Reference)
	}
	if e.Err == nil || e.Err.Error() != "SQL error" {
		t.Errorf("Err = %v", e.Err)
	}
}

func TestCLIError_FormatIndentsMultilineContent(t *testing.T) {
	e := &CLIError{
		Message: "failed",
		Context: "line1\nline2\nline3",
	}

	var buf bytes.Buffer
	e.FormatPlain(&buf)
	out := buf.String()

	// Each context line should be indented.
	if !strings.Contains(out, "  line1\n") {
		t.Error("multiline context should indent each line")
	}
	if !strings.Contains(out, "  line3\n") {
		t.Error("multiline context should indent each line")
	}
}

func TestCLIError_FormatWrappedErrAsCause(t *testing.T) {
	e := NewCLIError("operation failed").WithErr(errors.New("boom"))

	var buf bytes.Buffer
	e.FormatPlain(&buf)
	out := buf.String()

	if !strings.Contains(out, "Cause:\n  boom") {
		t.Errorf("expected wrapped err to appear as Cause, got:\n%s", out)
	}
}

func TestCLIError_FormatExplicitCauseWinsOverErr(t *testing.T) {
	e := NewCLIError("operation failed").
		WithErr(errors.New("wrapped")).
		WithCause("explicit cause text")

	var buf bytes.Buffer
	e.FormatPlain(&buf)
	out := buf.String()

	if !strings.Contains(out, "Cause:\n  explicit cause text") {
		t.Errorf("expected explicit Cause to win, got:\n%s", out)
	}
	if strings.Contains(out, "wrapped") {
		t.Errorf("wrapped err should not appear when Cause is explicit, got:\n%s", out)
	}
}

func TestCLIError_FormatNoErrNoCauseOmitsBlock(t *testing.T) {
	e := NewCLIError("only a message")

	var buf bytes.Buffer
	e.FormatPlain(&buf)
	out := buf.String()

	if strings.Contains(out, "Cause:") {
		t.Errorf("no Cause block expected when Err and Cause are both empty, got:\n%s", out)
	}
}

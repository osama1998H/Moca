//go:build integration

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLI_NotifyCommandExists verifies the notify command is registered.
func TestCLI_NotifyCommandExists(t *testing.T) {
	stdout, _, err := executeWithContext(t, t.TempDir(), "", "notify", "--help")
	if err != nil {
		t.Fatalf("notify --help: %v", err)
	}
	if !strings.Contains(stdout, "test-email") {
		t.Error("notify --help should mention test-email subcommand")
	}
	if !strings.Contains(stdout, "config") {
		t.Error("notify --help should mention config subcommand")
	}
}

// TestCLI_NotifyConfigJSON verifies that `notify config --json` outputs the
// current notification configuration as JSON.
func TestCLI_NotifyConfigJSON(t *testing.T) {
	// Create a temporary project with notification config.
	dir := t.TempDir()
	mocaYAML := `moca: dev
project:
  name: integ-test
  version: "0.1.0"
notification:
  email:
    provider: smtp
    smtp:
      host: smtp.test.dev
      port: 587
      from_addr: noreply@test.dev
`
	if err := os.WriteFile(filepath.Join(dir, "moca.yaml"), []byte(mocaYAML), 0o644); err != nil {
		t.Fatalf("write moca.yaml: %v", err)
	}

	stdout, _, err := executeWithContext(t, dir, "", "notify", "config", "--json")
	if err != nil {
		t.Fatalf("notify config --json: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("parse JSON output: %v\nraw: %s", err, stdout)
	}

	// The output should contain the email config.
	emailSection, ok := result["email"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'email' section in output, got: %v", result)
	}

	if provider, _ := emailSection["provider"].(string); provider != "smtp" {
		t.Errorf("provider = %q, want %q", provider, "smtp")
	}

	smtpSection, ok := emailSection["smtp"].(map[string]any)
	if !ok {
		t.Fatal("expected 'smtp' section in email config")
	}

	if host, _ := smtpSection["host"].(string); host != "smtp.test.dev" {
		t.Errorf("smtp.host = %q, want %q", host, "smtp.test.dev")
	}
}

// TestCLI_NotifyConfigSet verifies that `notify config --set` updates moca.yaml.
func TestCLI_NotifyConfigSet(t *testing.T) {
	dir := t.TempDir()
	initialYAML := `moca: dev
project:
  name: integ-test
  version: "0.1.0"
`
	if err := os.WriteFile(filepath.Join(dir, "moca.yaml"), []byte(initialYAML), 0o644); err != nil {
		t.Fatalf("write moca.yaml: %v", err)
	}

	_, _, err := executeWithContext(t, dir, "",
		"notify", "config",
		"--set", "smtp.host=smtp.updated.dev",
		"--set", "smtp.port=465",
		"--set", "provider=smtp",
	)
	if err != nil {
		t.Fatalf("notify config --set: %v", err)
	}

	// Read back the file and verify.
	updated, err := os.ReadFile(filepath.Join(dir, "moca.yaml"))
	if err != nil {
		t.Fatalf("read updated moca.yaml: %v", err)
	}

	content := string(updated)
	if !strings.Contains(content, "smtp.updated.dev") {
		t.Errorf("moca.yaml should contain smtp.updated.dev after set, got:\n%s", content)
	}
	if !strings.Contains(content, "notification") {
		t.Errorf("moca.yaml should contain notification section, got:\n%s", content)
	}
}

// TestCLI_NotifyTestEmail_NoConfig verifies that test-email without SMTP
// config returns a descriptive error.
func TestCLI_NotifyTestEmail_NoConfig(t *testing.T) {
	_, _, err := executeWithContext(t, t.TempDir(), "",
		"notify", "test-email", "--to", "test@test.com")
	if err == nil {
		t.Fatal("expected error when no email config is present")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "configured") && !strings.Contains(errMsg, "provider") {
		t.Errorf("error should mention missing config, got: %s", errMsg)
	}
}

// TestCLI_NotifyTestEmail_MissingTo verifies that test-email without --to
// returns a descriptive error.
func TestCLI_NotifyTestEmail_MissingTo(t *testing.T) {
	_, _, err := executeWithContext(t, t.TempDir(), "",
		"notify", "test-email")
	if err == nil {
		t.Fatal("expected error when --to is missing")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "to") && !strings.Contains(errMsg, "Recipient") {
		t.Errorf("error should mention missing --to flag, got: %s", errMsg)
	}
}

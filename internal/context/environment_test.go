package clicontext

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestResolveEnvironment_FallbackDefault(t *testing.T) {
	t.Setenv("MOCA_ENV", "")
	cmd := &cobra.Command{}

	got := resolveEnvironment(cmd, "")
	if got != "development" {
		t.Errorf("got %q, want 'development'", got)
	}
}

func TestResolveEnvironment_FromEnvVar(t *testing.T) {
	t.Setenv("MOCA_ENV", "production")
	cmd := &cobra.Command{}

	got := resolveEnvironment(cmd, "")
	if got != "production" {
		t.Errorf("got %q, want 'production'", got)
	}
}

func TestResolveEnvironment_FromStateFile(t *testing.T) {
	t.Setenv("MOCA_ENV", "")

	dir := t.TempDir()
	mocaDir := filepath.Join(dir, ".moca")
	if err := os.MkdirAll(mocaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mocaDir, "environment"), []byte("staging\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	got := resolveEnvironment(cmd, dir)
	if got != "staging" {
		t.Errorf("got %q, want 'staging'", got)
	}
}

func TestResolveEnvironment_EmptyStateFileFallback(t *testing.T) {
	t.Setenv("MOCA_ENV", "")

	dir := t.TempDir()
	mocaDir := filepath.Join(dir, ".moca")
	if err := os.MkdirAll(mocaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mocaDir, "environment"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	got := resolveEnvironment(cmd, dir)
	if got != "development" {
		t.Errorf("got %q, want 'development' (default)", got)
	}
}

func TestResolveEnvironment_EnvVarBeatsStateFile(t *testing.T) {
	t.Setenv("MOCA_ENV", "production")

	// Even with a state file present, env var should win.
	dir := t.TempDir()
	mocaDir := filepath.Join(dir, ".moca")
	if err := os.MkdirAll(mocaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mocaDir, "environment"), []byte("staging"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	got := resolveEnvironment(cmd, dir)
	if got != "production" {
		t.Errorf("got %q, want 'production' (env var wins)", got)
	}
}

package clicontext

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestResolveSite_EmptyContext(t *testing.T) {
	t.Setenv("MOCA_SITE", "")
	cmd := &cobra.Command{}

	got := resolveSite(cmd, "")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestResolveSite_FromEnvVar(t *testing.T) {
	t.Setenv("MOCA_SITE", "acme")
	cmd := &cobra.Command{}

	got := resolveSite(cmd, "")
	if got != "acme" {
		t.Errorf("got %q, want 'acme'", got)
	}
}

func TestResolveSite_FromStateFile(t *testing.T) {
	t.Setenv("MOCA_SITE", "")

	dir := t.TempDir()
	mocaDir := filepath.Join(dir, ".moca")
	if err := os.MkdirAll(mocaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mocaDir, "current_site"), []byte("testsite\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	got := resolveSite(cmd, dir)
	if got != "testsite" {
		t.Errorf("got %q, want 'testsite'", got)
	}
}

func TestResolveSite_EnvVarBeatsStateFile(t *testing.T) {
	t.Setenv("MOCA_SITE", "env-site")

	dir := t.TempDir()
	mocaDir := filepath.Join(dir, ".moca")
	if err := os.MkdirAll(mocaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mocaDir, "current_site"), []byte("file-site"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	got := resolveSite(cmd, dir)
	if got != "env-site" {
		t.Errorf("got %q, want 'env-site' (env var wins)", got)
	}
}

func TestReadStateFile(t *testing.T) {
	dir := t.TempDir()

	// File exists with content.
	path := filepath.Join(dir, "state")
	if err := os.WriteFile(path, []byte("  value  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readStateFile(path); got != "value" {
		t.Errorf("got %q, want 'value'", got)
	}

	// File does not exist.
	if got := readStateFile(filepath.Join(dir, "nonexistent")); got != "" {
		t.Errorf("got %q, want empty for nonexistent file", got)
	}

	// Empty file.
	emptyPath := filepath.Join(dir, "empty")
	if err := os.WriteFile(emptyPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readStateFile(emptyPath); got != "" {
		t.Errorf("got %q, want empty for empty file", got)
	}
}

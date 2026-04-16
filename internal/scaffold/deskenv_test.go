package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateDeskEnvSite_NoDeskDirectory(t *testing.T) {
	tmp := t.TempDir()
	if err := UpdateDeskEnvSite(tmp, "acme", false); err != nil {
		t.Fatalf("UpdateDeskEnvSite: %v", err)
	}
	// No desk/ was created — helper is a silent no-op.
	if _, err := os.Stat(filepath.Join(tmp, "desk")); !os.IsNotExist(err) {
		t.Errorf("expected desk/ absent, stat err=%v", err)
	}
}

func TestUpdateDeskEnvSite_CreatesEnvWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, "desk"), 0o755); err != nil {
		t.Fatalf("mkdir desk: %v", err)
	}

	if err := UpdateDeskEnvSite(tmp, "acme", false); err != nil {
		t.Fatalf("UpdateDeskEnvSite: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmp, "desk", ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if strings.TrimSpace(string(data)) != "VITE_MOCA_SITE=acme" {
		t.Errorf(".env = %q, want 'VITE_MOCA_SITE=acme'", string(data))
	}
}

func TestUpdateDeskEnvSite_ReplacesEmptyValue(t *testing.T) {
	tmp := t.TempDir()
	deskDir := filepath.Join(tmp, "desk")
	if err := os.Mkdir(deskDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	initial := "VITE_OTHER=keepme\nVITE_MOCA_SITE=\n"
	if err := os.WriteFile(filepath.Join(deskDir, ".env"), []byte(initial), 0o644); err != nil {
		t.Fatalf("seed .env: %v", err)
	}

	if err := UpdateDeskEnvSite(tmp, "acme", false); err != nil {
		t.Fatalf("UpdateDeskEnvSite: %v", err)
	}

	out, err := os.ReadFile(filepath.Join(deskDir, ".env"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "VITE_MOCA_SITE=acme") {
		t.Errorf("expected VITE_MOCA_SITE=acme, got:\n%s", got)
	}
	if !strings.Contains(got, "VITE_OTHER=keepme") {
		t.Errorf("expected VITE_OTHER preserved, got:\n%s", got)
	}
}

func TestUpdateDeskEnvSite_PreservesNonEmptyUnlessForce(t *testing.T) {
	tmp := t.TempDir()
	deskDir := filepath.Join(tmp, "desk")
	if err := os.Mkdir(deskDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	initial := "VITE_MOCA_SITE=existing\n"
	if err := os.WriteFile(filepath.Join(deskDir, ".env"), []byte(initial), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// force=false: leave existing non-empty value alone.
	if err := UpdateDeskEnvSite(tmp, "acme", false); err != nil {
		t.Fatalf("UpdateDeskEnvSite: %v", err)
	}
	out, _ := os.ReadFile(filepath.Join(deskDir, ".env"))
	if !strings.Contains(string(out), "VITE_MOCA_SITE=existing") {
		t.Errorf("non-empty value should be preserved, got:\n%s", string(out))
	}

	// force=true: replace.
	if err := UpdateDeskEnvSite(tmp, "acme", true); err != nil {
		t.Fatalf("UpdateDeskEnvSite force=true: %v", err)
	}
	out, _ = os.ReadFile(filepath.Join(deskDir, ".env"))
	if !strings.Contains(string(out), "VITE_MOCA_SITE=acme") {
		t.Errorf("force=true should replace value, got:\n%s", string(out))
	}
	if strings.Contains(string(out), "VITE_MOCA_SITE=existing") {
		t.Errorf("old value should be gone after force, got:\n%s", string(out))
	}
}

func TestUpdateDeskEnvSite_AppendsWhenKeyAbsent(t *testing.T) {
	tmp := t.TempDir()
	deskDir := filepath.Join(tmp, "desk")
	if err := os.Mkdir(deskDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	initial := "VITE_OTHER=1\n"
	if err := os.WriteFile(filepath.Join(deskDir, ".env"), []byte(initial), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := UpdateDeskEnvSite(tmp, "acme", false); err != nil {
		t.Fatalf("UpdateDeskEnvSite: %v", err)
	}

	out, _ := os.ReadFile(filepath.Join(deskDir, ".env"))
	got := string(out)
	if !strings.Contains(got, "VITE_OTHER=1") {
		t.Errorf("preserved key lost, got:\n%s", got)
	}
	if !strings.Contains(got, "VITE_MOCA_SITE=acme") {
		t.Errorf("expected appended VITE_MOCA_SITE=acme, got:\n%s", got)
	}
}

func TestUpdateDeskEnvSite_NoTempFilesRemain(t *testing.T) {
	tmp := t.TempDir()
	deskDir := filepath.Join(tmp, "desk")
	if err := os.Mkdir(deskDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := UpdateDeskEnvSite(tmp, "acme", false); err != nil {
		t.Fatalf("UpdateDeskEnvSite: %v", err)
	}

	entries, err := os.ReadDir(deskDir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if e.Name() == ".env" {
			continue
		}
		if strings.HasPrefix(e.Name(), ".env") {
			t.Errorf("stray temp file: %s", e.Name())
		}
	}
}

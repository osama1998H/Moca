package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/osama1998H/moca/internal/scaffold"
)

func TestScaffoldDeskWithCleanup_RemovesPartialStateOnFailure(t *testing.T) {
	tmp := t.TempDir()

	// Pre-seed desk/ with a partial subpath so ScaffoldDesk fails with
	// "desk/ directory already exists" at scaffold.go:40.
	if err := os.MkdirAll(filepath.Join(tmp, "desk", "src"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := scaffoldDeskWithCleanup(scaffold.DeskScaffoldOptions{
		ProjectRoot: tmp,
		ProjectName: "demo",
	})
	if err == nil {
		t.Fatal("expected scaffold to fail, got nil")
	}

	if _, statErr := os.Stat(filepath.Join(tmp, "desk")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("expected desk/ removed after failure, got stat err=%v", statErr)
	}
}

func TestScaffoldDeskWithCleanup_SuccessLeavesDirectory(t *testing.T) {
	tmp := t.TempDir()

	err := scaffoldDeskWithCleanup(scaffold.DeskScaffoldOptions{
		ProjectRoot: tmp,
		ProjectName: "demo",
	})
	if err != nil {
		t.Fatalf("scaffold: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(tmp, "desk", "package.json")); statErr != nil {
		t.Errorf("expected desk/package.json to exist after success, got %v", statErr)
	}
}

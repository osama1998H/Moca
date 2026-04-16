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

	// Seed a desk/ directory so ScaffoldDesk fails fast ("desk/ already
	// exists"). The purpose of this helper-level test is to confirm
	// that whenever ScaffoldDesk errors, the helper removes the desk/
	// it finds. The full runInit flow prevents this destructive case
	// via a guard earlier in runInit; this test exercises the helper
	// in isolation as defense-in-depth for future call sites.
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

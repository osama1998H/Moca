package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_AcceptsBuiltinCoreLayout(t *testing.T) {
	dir := t.TempDir()
	rootGoMod, goWork, builtinCoreDir, legacyCoreGoMod := writeValidRepoLayout(t, dir)

	if err := run(rootGoMod, goWork, builtinCoreDir, legacyCoreGoMod); err != nil {
		t.Fatalf("run() error = %v", err)
	}
}

func TestRun_FailsWhenLegacyCoreModuleRequired(t *testing.T) {
	dir := t.TempDir()
	rootGoMod, goWork, builtinCoreDir, legacyCoreGoMod := writeValidRepoLayout(t, dir)
	overwriteFile(t, rootGoMod, "module github.com/osama1998H/moca\n\ngo 1.26.1\n\nrequire github.com/osama1998H/moca/apps/core v0.1.1-alpha.7\n")

	err := run(rootGoMod, goWork, builtinCoreDir, legacyCoreGoMod)
	if err == nil {
		t.Fatal("run() error = nil, want legacy require failure")
	}
	if !strings.Contains(err.Error(), "must not require legacy core module") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_FailsWhenLegacyCoreModuleReplaced(t *testing.T) {
	dir := t.TempDir()
	rootGoMod, goWork, builtinCoreDir, legacyCoreGoMod := writeValidRepoLayout(t, dir)
	overwriteFile(t, rootGoMod, "module github.com/osama1998H/moca\n\ngo 1.26.1\n\nreplace github.com/osama1998H/moca/apps/core => ./apps/core\n")

	err := run(rootGoMod, goWork, builtinCoreDir, legacyCoreGoMod)
	if err == nil {
		t.Fatal("run() error = nil, want legacy replace failure")
	}
	if !strings.Contains(err.Error(), "must not replace legacy core module") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_FailsWhenLegacyCoreGoModExists(t *testing.T) {
	dir := t.TempDir()
	rootGoMod, goWork, builtinCoreDir, legacyCoreGoMod := writeValidRepoLayout(t, dir)
	overwriteFile(t, legacyCoreGoMod, "module github.com/osama1998H/moca/apps/core\n")

	err := run(rootGoMod, goWork, builtinCoreDir, legacyCoreGoMod)
	if err == nil {
		t.Fatal("run() error = nil, want legacy go.mod failure")
	}
	if !strings.Contains(err.Error(), "legacy core go.mod must not exist") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_FailsWhenBuiltinCoreDirMissing(t *testing.T) {
	dir := t.TempDir()
	rootGoMod, goWork, builtinCoreDir, legacyCoreGoMod := writeValidRepoLayout(t, dir)

	if err := os.RemoveAll(builtinCoreDir); err != nil {
		t.Fatalf("remove builtin core dir: %v", err)
	}

	err := run(rootGoMod, goWork, builtinCoreDir, legacyCoreGoMod)
	if err == nil {
		t.Fatal("run() error = nil, want missing builtin core dir failure")
	}
	if !strings.Contains(err.Error(), "builtin core package") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_FailsWhenBuiltinCoreContainsNestedGoMod(t *testing.T) {
	dir := t.TempDir()
	rootGoMod, goWork, builtinCoreDir, legacyCoreGoMod := writeValidRepoLayout(t, dir)
	overwriteFile(t, filepath.Join(builtinCoreDir, "go.mod"), "module github.com/osama1998H/moca/pkg/builtin/core\n")

	err := run(rootGoMod, goWork, builtinCoreDir, legacyCoreGoMod)
	if err == nil {
		t.Fatal("run() error = nil, want nested builtin go.mod failure")
	}
	if !strings.Contains(err.Error(), "must not contain nested go.mod") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_FailsWhenGoWorkUsesLegacyCore(t *testing.T) {
	dir := t.TempDir()
	rootGoMod, goWork, builtinCoreDir, legacyCoreGoMod := writeValidRepoLayout(t, dir)
	overwriteFile(t, goWork, "go 1.26.1\n\nuse (\n\t.\n\t./apps/core\n)\n")

	err := run(rootGoMod, goWork, builtinCoreDir, legacyCoreGoMod)
	if err == nil {
		t.Fatal("run() error = nil, want legacy go.work entry failure")
	}
	if !strings.Contains(err.Error(), "go.work must not reference legacy core workspace entry") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeValidRepoLayout(t *testing.T, dir string) (string, string, string, string) {
	t.Helper()

	rootGoMod := filepath.Join(dir, "go.mod")
	goWork := filepath.Join(dir, "go.work")
	builtinCoreDir := filepath.Join(dir, "pkg", "builtin", "core")
	legacyCoreGoMod := filepath.Join(dir, "apps", "core", "go.mod")

	if err := os.MkdirAll(builtinCoreDir, 0o755); err != nil {
		t.Fatalf("mkdir builtin core dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(legacyCoreGoMod), 0o755); err != nil {
		t.Fatalf("mkdir legacy core dir: %v", err)
	}

	overwriteFile(t, rootGoMod, "module github.com/osama1998H/moca\n\ngo 1.26.1\n")
	overwriteFile(t, goWork, "go 1.26.1\n\nuse (\n\t.\n)\n")
	overwriteFile(t, filepath.Join(builtinCoreDir, "manifest.yaml"), "name: core\nversion: \"0.1.0\"\nmoca_version: \">=0.1.0\"\nmodules: []\n")

	return rootGoMod, goWork, builtinCoreDir, legacyCoreGoMod
}

func overwriteFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

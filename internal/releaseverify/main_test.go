package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_VerifiesSymmetricReleaseVersioning(t *testing.T) {
	dir := t.TempDir()
	rootGoMod, coreGoMod := writeModuleFixtures(t, dir, "v0.1.1-alpha.7", true)

	if err := run("v0.1.1-alpha.7", rootGoMod, coreGoMod); err != nil {
		t.Fatalf("run() error = %v", err)
	}
}

func TestRun_FailsOnVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	rootGoMod, coreGoMod := writeModuleFixtures(t, dir, "v0.1.1-alpha.7", true)
	overwriteGoMod(t, rootGoMod, strings.ReplaceAll(readFile(t, rootGoMod), "v0.1.1-alpha.7", "v0.1.1-alpha.6"))

	err := run("v0.1.1-alpha.7", rootGoMod, coreGoMod)
	if err == nil {
		t.Fatal("run() error = nil, want mismatch error")
	}
	if !strings.Contains(err.Error(), `must require "github.com/osama1998H/moca/apps/core" at "v0.1.1-alpha.7"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_FailsWhenReplaceIsMissing(t *testing.T) {
	dir := t.TempDir()
	rootGoMod, coreGoMod := writeModuleFixtures(t, dir, "v0.1.1-alpha.7", false)

	err := run("v0.1.1-alpha.7", rootGoMod, coreGoMod)
	if err == nil {
		t.Fatal("run() error = nil, want missing replace error")
	}
	if !strings.Contains(err.Error(), `must replace "github.com/osama1998H/moca/apps/core" with local path "./apps/core"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeReleaseVersion_RejectsWrongTagInput(t *testing.T) {
	cases := []string{
		"",
		"1.2.3",
		"refs/tags/apps/core/v0.1.1-alpha.7",
		"apps/core/v0.1.1-alpha.7",
	}

	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			if _, err := normalizeReleaseVersion(input); err == nil {
				t.Fatalf("normalizeReleaseVersion(%q) error = nil, want error", input)
			}
		})
	}
}

func writeModuleFixtures(t *testing.T, dir, version string, includeReplace bool) (string, string) {
	t.Helper()

	rootGoMod := filepath.Join(dir, "go.mod")
	coreDir := filepath.Join(dir, "apps", "core")
	coreGoMod := filepath.Join(coreDir, "go.mod")

	if err := os.MkdirAll(coreDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", coreDir, err)
	}

	rootReplace := ""
	coreReplace := ""
	if includeReplace {
		rootReplace = "\nreplace github.com/osama1998H/moca/apps/core => ./apps/core\n"
		coreReplace = "\nreplace github.com/osama1998H/moca => ../..\n"
	}

	overwriteGoMod(t, rootGoMod, "module github.com/osama1998H/moca\n\ngo 1.26.1\n\nrequire github.com/osama1998H/moca/apps/core "+version+"\n"+rootReplace)
	overwriteGoMod(t, coreGoMod, "module github.com/osama1998H/moca/apps/core\n\ngo 1.26.1\n\nrequire github.com/osama1998H/moca "+version+"\n"+coreReplace)

	return rootGoMod, coreGoMod
}

func overwriteGoMod(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}

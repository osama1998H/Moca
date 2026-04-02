package apps

import (
	"testing"

	"golang.org/x/mod/modfile"
)

// parseMod is a test helper that parses a go.mod from a string.
func parseMod(t *testing.T, name, content string) *modfile.File {
	t.Helper()
	f, err := modfile.Parse(name, []byte(content), nil)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return f
}

func TestValidateAppDependencies_NoConflict(t *testing.T) {
	app := parseMod(t, "app/go.mod", `
module github.com/example/newapp
go 1.26
require github.com/stretchr/testify v1.9.0
`)

	existing := parseMod(t, "existing/go.mod", `
module github.com/example/existingapp
go 1.26
require github.com/stretchr/testify v1.8.0
`)

	conflicts := ValidateAppDependencies(app, []*modfile.File{existing})
	if len(conflicts) != 0 {
		t.Errorf("expected 0 conflicts, got %d: %+v", len(conflicts), conflicts)
	}
}

func TestValidateAppDependencies_MajorConflict(t *testing.T) {
	// Use different module paths for v4 vs v5 to make modfile.Parse happy,
	// but they share the same base package path stripped of major suffix.
	// In practice, major version conflicts manifest as different module paths
	// (e.g., pgx/v4 vs pgx/v5). We test the version comparison logic directly
	// by using a single-major-version module with differing major in the version string.
	// Since modfile enforces version/path consistency, we test with a package
	// that has no /vN suffix (major v0 or v1) with different major version strings.
	app := parseMod(t, "app/go.mod", `
module github.com/example/newapp
go 1.26
require github.com/stretchr/testify v1.9.0
`)

	existing := parseMod(t, "existing/go.mod", `
module github.com/example/existingapp
go 1.26
require github.com/stretchr/testify v0.8.0
`)

	conflicts := ValidateAppDependencies(app, []*modfile.File{existing})
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}

	c := conflicts[0]
	if c.Package != "github.com/stretchr/testify" {
		t.Errorf("Package = %q", c.Package)
	}
	if c.NewVersion != "v1.9.0" {
		t.Errorf("NewVersion = %q", c.NewVersion)
	}
	if c.OldVersion != "v0.8.0" {
		t.Errorf("OldVersion = %q", c.OldVersion)
	}
	if !c.IsMajor {
		t.Error("expected IsMajor = true")
	}
}

func TestValidateAppDependencies_SkipsIndirect(t *testing.T) {
	// Indirect deps with differing major versions should be ignored.
	app := parseMod(t, "app/go.mod", `
module github.com/example/newapp
go 1.26
require github.com/stretchr/testify v0.8.0 // indirect
`)

	existing := parseMod(t, "existing/go.mod", `
module github.com/example/existingapp
go 1.26
require github.com/stretchr/testify v1.9.0
`)

	conflicts := ValidateAppDependencies(app, []*modfile.File{existing})
	if len(conflicts) != 0 {
		t.Errorf("expected 0 conflicts for indirect dep, got %d", len(conflicts))
	}
}

func TestValidateAppDependencies_EmptyWorkspace(t *testing.T) {
	app := parseMod(t, "app/go.mod", `
module github.com/example/newapp
go 1.26
require github.com/stretchr/testify v1.9.0
`)

	conflicts := ValidateAppDependencies(app, nil)
	if len(conflicts) != 0 {
		t.Errorf("expected 0 conflicts for empty workspace, got %d", len(conflicts))
	}
}

func TestValidateAppDependencies_MultipleConflicts(t *testing.T) {
	// Use v0 vs v1 to create valid major version conflicts.
	app := parseMod(t, "app/go.mod", `
module github.com/example/newapp
go 1.26
require (
	github.com/pkg/errors v0.9.0
	github.com/sirupsen/logrus v0.11.0
)
`)

	existing := parseMod(t, "existing/go.mod", `
module github.com/example/existingapp
go 1.26
require (
	github.com/pkg/errors v1.0.0
	github.com/sirupsen/logrus v1.9.0
)
`)

	conflicts := ValidateAppDependencies(app, []*modfile.File{existing})
	if len(conflicts) != 2 {
		t.Errorf("expected 2 conflicts, got %d: %+v", len(conflicts), conflicts)
	}
}

func TestMajorVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v1.9.0", "v1"},
		{"v2.3.0", "v2"},
		{"v0.22.0", "v0"},
		{"v1", "v1"},
		{"", ""},
	}

	for _, tt := range tests {
		got := majorVersion(tt.input)
		if got != tt.want {
			t.Errorf("majorVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

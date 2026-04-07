package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScaffoldDesk_CreatesAllFiles(t *testing.T) {
	tmpDir := t.TempDir()

	err := ScaffoldDesk(DeskScaffoldOptions{
		ProjectRoot:     tmpDir,
		ProjectName:     "test-project",
		MocaDeskVersion: "0.1.0",
		MocaDeskSpec:    "^0.1.0",
	})
	if err != nil {
		t.Fatalf("ScaffoldDesk() error: %v", err)
	}

	expectedFiles := []string{
		"desk/package.json",
		"desk/index.html",
		"desk/vite.config.ts",
		"desk/tsconfig.json",
		"desk/tsconfig.app.json",
		"desk/tsconfig.node.json",
		"desk/src/main.tsx",
		"desk/src/overrides/index.ts",
		"desk/src/overrides/theme.ts",
		"desk/.gitignore",
		"desk/.moca-extensions.ts",
	}

	for _, f := range expectedFiles {
		path := filepath.Join(tmpDir, f)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file %s to exist, got error: %v", f, err)
		}
	}
}

func TestScaffoldDesk_PackageJSONContent(t *testing.T) {
	tmpDir := t.TempDir()

	err := ScaffoldDesk(DeskScaffoldOptions{
		ProjectRoot:     tmpDir,
		ProjectName:     "my-erp",
		MocaDeskVersion: "0.1.0",
		MocaDeskSpec:    "^0.1.0",
	})
	if err != nil {
		t.Fatalf("ScaffoldDesk() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "desk", "package.json"))
	if err != nil {
		t.Fatalf("read package.json: %v", err)
	}
	content := string(data)

	checks := []struct {
		name    string
		want    string
	}{
		{"project name", `"name": "my-erp-desk"`},
		{"private flag", `"private": true`},
		{"moca desk dep", `"@moca/desk": "^0.1.0"`},
		{"react dep", `"react": "^19.0.0"`},
		{"react-dom dep", `"react-dom": "^19.0.0"`},
		{"vite dev dep", `"vite": "^8.0.0"`},
		{"typescript dev dep", `"typescript": "~6.0.0"`},
	}

	for _, c := range checks {
		if !strings.Contains(content, c.want) {
			t.Errorf("package.json missing %s: expected to contain %q", c.name, c.want)
		}
	}
}

func TestScaffoldDesk_FileProtocolSpec(t *testing.T) {
	tmpDir := t.TempDir()

	err := ScaffoldDesk(DeskScaffoldOptions{
		ProjectRoot:     tmpDir,
		ProjectName:     "demo",
		MocaDeskVersion: "0.1.0",
		MocaDeskSpec:    "file:../../desk",
	})
	if err != nil {
		t.Fatalf("ScaffoldDesk() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "desk", "package.json"))
	if err != nil {
		t.Fatalf("read package.json: %v", err)
	}
	if !strings.Contains(string(data), `"@moca/desk": "file:../../desk"`) {
		t.Error("package.json should use file: protocol spec")
	}
}

func TestScaffoldDesk_MainTsxContent(t *testing.T) {
	tmpDir := t.TempDir()

	err := ScaffoldDesk(DeskScaffoldOptions{
		ProjectRoot: tmpDir,
		ProjectName: "test",
	})
	if err != nil {
		t.Fatalf("ScaffoldDesk() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "desk", "src", "main.tsx"))
	if err != nil {
		t.Fatalf("read main.tsx: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, `from "@moca/desk"`) {
		t.Error("main.tsx should import from @moca/desk")
	}
	if !strings.Contains(content, "createDeskApp") {
		t.Error("main.tsx should use createDeskApp")
	}
	if !strings.Contains(content, `.mount("#root")`) {
		t.Error("main.tsx should mount to #root")
	}
	if !strings.Contains(content, ".moca-extensions") {
		t.Error("main.tsx should import .moca-extensions")
	}
}

func TestScaffoldDesk_ViteConfigContent(t *testing.T) {
	tmpDir := t.TempDir()

	err := ScaffoldDesk(DeskScaffoldOptions{
		ProjectRoot: tmpDir,
		ProjectName: "test",
	})
	if err != nil {
		t.Fatalf("ScaffoldDesk() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "desk", "vite.config.ts"))
	if err != nil {
		t.Fatalf("read vite.config.ts: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, `from "@moca/desk/vite"`) {
		t.Error("vite.config.ts should import from @moca/desk/vite")
	}
	if !strings.Contains(content, "mocaDeskPlugin") {
		t.Error("vite.config.ts should use mocaDeskPlugin")
	}
}

func TestScaffoldDesk_ErrorWhenDeskExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Pre-create desk/ directory.
	if err := os.MkdirAll(filepath.Join(tmpDir, "desk"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := ScaffoldDesk(DeskScaffoldOptions{
		ProjectRoot: tmpDir,
		ProjectName: "test",
	})
	if err == nil {
		t.Fatal("expected error when desk/ already exists, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

func TestScaffoldDesk_DefaultSpec(t *testing.T) {
	tmpDir := t.TempDir()

	err := ScaffoldDesk(DeskScaffoldOptions{
		ProjectRoot:     tmpDir,
		ProjectName:     "test",
		MocaDeskVersion: "0.2.0",
		// MocaDeskSpec left empty — should default to "^0.2.0"
	})
	if err != nil {
		t.Fatalf("ScaffoldDesk() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "desk", "package.json"))
	if err != nil {
		t.Fatalf("read package.json: %v", err)
	}
	if !strings.Contains(string(data), `"@moca/desk": "^0.2.0"`) {
		t.Error("package.json should default spec to ^{version}")
	}
}

func TestScaffoldDesk_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		opts DeskScaffoldOptions
		want string
	}{
		{
			name: "missing project root",
			opts: DeskScaffoldOptions{ProjectName: "test"},
			want: "project root is required",
		},
		{
			name: "missing project name",
			opts: DeskScaffoldOptions{ProjectRoot: "/tmp/test"},
			want: "project name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ScaffoldDesk(tt.opts)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.want)
			}
		})
	}
}

func TestScaffoldDesk_GitignoreContent(t *testing.T) {
	tmpDir := t.TempDir()

	err := ScaffoldDesk(DeskScaffoldOptions{
		ProjectRoot: tmpDir,
		ProjectName: "test",
	})
	if err != nil {
		t.Fatalf("ScaffoldDesk() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "desk", ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	content := string(data)

	for _, entry := range []string{"node_modules/", "dist/", ".moca-extensions.ts"} {
		if !strings.Contains(content, entry) {
			t.Errorf(".gitignore should contain %q", entry)
		}
	}
}

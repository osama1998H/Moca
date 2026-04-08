package scaffold

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/osama1998H/moca/pkg/apps"
)

// helper to create a minimal apps dir inside a temp dir.
func setupTestDir(t *testing.T) (appsDir string, projectRoot string) {
	t.Helper()
	projectRoot = t.TempDir()
	appsDir = filepath.Join(projectRoot, "apps")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a minimal go.mod so readGoModulePath works.
	gomod := "module github.com/test/project\n\ngo 1.26.1\n"
	if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	return appsDir, projectRoot
}

func baseOpts(appsDir, projectRoot string) ScaffoldOptions {
	return ScaffoldOptions{
		AppName:          "my_app",
		AppsDir:          appsDir,
		ProjectRoot:      projectRoot,
		SkipGoModTidy:    true,
		SkipGoWorkUpdate: true,
	}
}

func TestScaffoldApp_Standard(t *testing.T) {
	appsDir, projectRoot := setupTestDir(t)
	opts := baseOpts(appsDir, projectRoot)
	opts.Template = TemplateStandard
	opts.Publisher = "Test Corp"

	if err := ScaffoldApp(opts); err != nil {
		t.Fatalf("ScaffoldApp: %v", err)
	}

	appDir := filepath.Join(appsDir, "my_app")

	// Verify directories exist.
	expectedDirs := []string{
		"modules/my_app/doctypes",
		"modules/my_app/pages",
		"modules/my_app/reports",
		"fixtures",
		"migrations",
		"templates/portal",
		"public",
		"tests",
	}
	for _, d := range expectedDirs {
		p := filepath.Join(appDir, d)
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("expected directory %s to exist: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %s to be a directory", d)
		}
	}

	// Verify files exist.
	expectedFiles := []string{
		"manifest.yaml",
		"hooks.go",
		"go.mod",
		"README.md",
		"migrations/001_initial.sql",
		"tests/setup_test.go",
		"modules/my_app/doctypes/.gitkeep",
		"modules/my_app/pages/.gitkeep",
		"modules/my_app/reports/.gitkeep",
	}
	for _, f := range expectedFiles {
		p := filepath.Join(appDir, f)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected file %s to exist: %v", f, err)
		}
	}

	// Verify manifest is parseable and valid.
	manifest, err := apps.ParseManifest(filepath.Join(appDir, "manifest.yaml"))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if err := apps.ValidateManifest(manifest); err != nil {
		t.Fatalf("ValidateManifest: %v", err)
	}
	if manifest.Name != "my_app" {
		t.Errorf("manifest.Name = %q, want %q", manifest.Name, "my_app")
	}
	if manifest.Publisher != "Test Corp" {
		t.Errorf("manifest.Publisher = %q, want %q", manifest.Publisher, "Test Corp")
	}
	if manifest.License != "MIT" {
		t.Errorf("manifest.License = %q, want %q", manifest.License, "MIT")
	}
	if len(manifest.Modules) != 1 || manifest.Modules[0].Name != "MyApp" {
		t.Errorf("manifest.Modules = %+v, want single module named MyApp", manifest.Modules)
	}

	// Verify hooks.go has correct package name.
	hooksContent, _ := os.ReadFile(filepath.Join(appDir, "hooks.go"))
	if !strings.Contains(string(hooksContent), "package my_app") {
		t.Error("hooks.go should contain 'package my_app'")
	}

	// Verify go.mod has correct module path.
	gomodContent, _ := os.ReadFile(filepath.Join(appDir, "go.mod"))
	if !strings.Contains(string(gomodContent), "module github.com/test/project/apps/my_app") {
		t.Errorf("go.mod should contain correct module path, got:\n%s", gomodContent)
	}
	if !strings.Contains(string(gomodContent), "github.com/osama1998H/moca v0.0.0") {
		t.Errorf("go.mod should contain local framework version, got:\n%s", gomodContent)
	}
	if !strings.Contains(string(gomodContent), "replace github.com/osama1998H/moca => ../..") {
		t.Error("go.mod should contain replace directive")
	}
}

func TestScaffoldApp_StandaloneFrameworkDependency(t *testing.T) {
	appsDir, projectRoot := setupTestDir(t)
	opts := baseOpts(appsDir, projectRoot)
	opts.FrameworkModuleVersion = "v0.1.1-alpha.7"
	opts.FrameworkReplacePath = ""

	if err := ScaffoldApp(opts); err != nil {
		t.Fatalf("ScaffoldApp: %v", err)
	}

	gomodContent, err := os.ReadFile(filepath.Join(appsDir, "my_app", "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}

	if !strings.Contains(string(gomodContent), "github.com/osama1998H/moca v0.1.1-alpha.7") {
		t.Errorf("go.mod should contain release framework version, got:\n%s", gomodContent)
	}
	if strings.Contains(string(gomodContent), "replace github.com/osama1998H/moca =>") {
		t.Errorf("go.mod should not contain replace directive in standalone mode, got:\n%s", gomodContent)
	}
}

func TestScaffoldApp_Minimal(t *testing.T) {
	appsDir, projectRoot := setupTestDir(t)
	opts := baseOpts(appsDir, projectRoot)
	opts.Template = TemplateMinimal

	if err := ScaffoldApp(opts); err != nil {
		t.Fatalf("ScaffoldApp: %v", err)
	}

	appDir := filepath.Join(appsDir, "my_app")

	// Verify core files exist.
	for _, f := range []string{"manifest.yaml", "hooks.go", "go.mod", "README.md"} {
		if _, err := os.Stat(filepath.Join(appDir, f)); err != nil {
			t.Errorf("expected file %s to exist: %v", f, err)
		}
	}

	// Verify module doctypes dir exists.
	if _, err := os.Stat(filepath.Join(appDir, "modules", "my_app", "doctypes")); err != nil {
		t.Error("expected modules/my_app/doctypes to exist")
	}

	// Verify standard-only dirs do NOT exist.
	shouldNotExist := []string{"fixtures", "migrations", "templates", "public", "tests"}
	for _, d := range shouldNotExist {
		if _, err := os.Stat(filepath.Join(appDir, d)); err == nil {
			t.Errorf("directory %s should NOT exist in minimal template", d)
		}
	}

	// Verify pages and reports dirs do NOT exist.
	for _, d := range []string{"modules/my_app/pages", "modules/my_app/reports"} {
		if _, err := os.Stat(filepath.Join(appDir, d)); err == nil {
			t.Errorf("directory %s should NOT exist in minimal template", d)
		}
	}
}

func TestScaffoldApp_APIOnly(t *testing.T) {
	appsDir, projectRoot := setupTestDir(t)
	opts := baseOpts(appsDir, projectRoot)
	opts.Template = TemplateAPIOnly

	if err := ScaffoldApp(opts); err != nil {
		t.Fatalf("ScaffoldApp: %v", err)
	}

	appDir := filepath.Join(appsDir, "my_app")

	// Verify api.go exists in the module directory.
	apiPath := filepath.Join(appDir, "modules", "my_app", "api.go")
	content, err := os.ReadFile(apiPath)
	if err != nil {
		t.Fatalf("expected api.go to exist: %v", err)
	}
	if !strings.Contains(string(content), "package my_app") {
		t.Error("api.go should contain correct package name")
	}
	if !strings.Contains(string(content), "HandleList") {
		t.Error("api.go should contain HandleList function")
	}

	// Verify pages and reports dirs do NOT exist.
	for _, d := range []string{"modules/my_app/pages", "modules/my_app/reports"} {
		if _, err := os.Stat(filepath.Join(appDir, d)); err == nil {
			t.Errorf("directory %s should NOT exist in api-only template", d)
		}
	}
}

func TestScaffoldApp_WithDocType(t *testing.T) {
	appsDir, projectRoot := setupTestDir(t)
	opts := baseOpts(appsDir, projectRoot)
	opts.DocType = "Task"

	if err := ScaffoldApp(opts); err != nil {
		t.Fatalf("ScaffoldApp: %v", err)
	}

	appDir := filepath.Join(appsDir, "my_app")

	// Verify doctype JSON exists.
	dtPath := filepath.Join(appDir, "modules", "my_app", "doctypes", "task", "task.json")
	data, err := os.ReadFile(dtPath)
	if err != nil {
		t.Fatalf("expected doctype JSON to exist: %v", err)
	}

	// Verify valid JSON.
	var parsed map[string]any
	if unmarshalErr := json.Unmarshal(data, &parsed); unmarshalErr != nil {
		t.Fatalf("doctype JSON is not valid: %v", unmarshalErr)
	}
	if parsed["name"] != "Task" {
		t.Errorf("doctype name = %v, want Task", parsed["name"])
	}
	if parsed["module"] != "MyApp" {
		t.Errorf("doctype module = %v, want MyApp", parsed["module"])
	}

	// Verify .gitkeep was removed from doctypes dir (since it now has content).
	gitkeep := filepath.Join(appDir, "modules", "my_app", "doctypes", ".gitkeep")
	if _, statErr := os.Stat(gitkeep); statErr == nil {
		t.Error(".gitkeep should be removed from doctypes dir when a doctype is scaffolded")
	}

	// Verify manifest includes the doctype.
	manifest, err := apps.ParseManifest(filepath.Join(appDir, "manifest.yaml"))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if len(manifest.Modules) == 0 {
		t.Fatal("expected at least one module")
	}
	found := false
	for _, dt := range manifest.Modules[0].DocTypes {
		if dt == "Task" {
			found = true
			break
		}
	}
	if !found {
		t.Error("manifest should list Task in module doctypes")
	}
}

func TestScaffoldApp_InvalidName(t *testing.T) {
	appsDir, projectRoot := setupTestDir(t)

	cases := []struct {
		name    string
		wantErr bool
	}{
		{"my_app", false},
		{"crm", false},
		{"app123", false},
		{"MyApp", true},  // uppercase
		{"123app", true}, // starts with digit
		{"", true},       // empty
		{"my-app", true}, // hyphen
		{"my app", true}, // space
		{"my.app", true}, // dot
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := baseOpts(appsDir, projectRoot)
			opts.AppName = tc.name
			err := ScaffoldApp(opts)

			if tc.wantErr && err == nil {
				t.Errorf("expected error for name %q", tc.name)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error for name %q: %v", tc.name, err)
			}

			// Clean up successful scaffolds for next iteration.
			if err == nil {
				_ = os.RemoveAll(filepath.Join(appsDir, tc.name))
			}
		})
	}
}

func TestScaffoldApp_DirectoryExists(t *testing.T) {
	appsDir, projectRoot := setupTestDir(t)

	// Pre-create the directory.
	if err := os.MkdirAll(filepath.Join(appsDir, "my_app"), 0o755); err != nil {
		t.Fatal(err)
	}

	opts := baseOpts(appsDir, projectRoot)
	err := ScaffoldApp(opts)
	if err == nil {
		t.Fatal("expected error when directory already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

func TestScaffoldApp_DefaultDerivation(t *testing.T) {
	appsDir, projectRoot := setupTestDir(t)
	opts := baseOpts(appsDir, projectRoot)
	// Leave ModuleName and Title empty to test defaults.

	if err := ScaffoldApp(opts); err != nil {
		t.Fatalf("ScaffoldApp: %v", err)
	}

	appDir := filepath.Join(appsDir, "my_app")
	manifest, err := apps.ParseManifest(filepath.Join(appDir, "manifest.yaml"))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	if manifest.Title != "My App" {
		t.Errorf("manifest.Title = %q, want %q", manifest.Title, "My App")
	}
	if len(manifest.Modules) != 1 || manifest.Modules[0].Name != "MyApp" {
		t.Errorf("manifest.Modules[0].Name = %v, want MyApp", manifest.Modules)
	}
}

func TestDeriveModuleName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"my_app", "MyApp"},
		{"crm", "Crm"},
		{"hr_management", "HrManagement"},
		{"a", "A"},
		{"sales_order_app", "SalesOrderApp"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := deriveModuleName(tc.input)
			if got != tc.want {
				t.Errorf("deriveModuleName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestDeriveTitle(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"my_app", "My App"},
		{"crm", "Crm"},
		{"hr_management", "Hr Management"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := deriveTitle(tc.input)
			if got != tc.want {
				t.Errorf("deriveTitle(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestToSnakeCase(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"MyApp", "my_app"},
		{"Crm", "crm"},
		{"HrManagement", "hr_management"},
		{"", ""},
		{"already_snake", "already_snake"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := toSnakeCase(tc.input)
			if got != tc.want {
				t.Errorf("toSnakeCase(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestAddToGoWork(t *testing.T) {
	// Skip if go binary is not available.
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go binary not available")
	}

	dir := t.TempDir()

	// Create a minimal go.work file.
	gowork := "go 1.26.1\n\nuse .\n"
	if err := os.WriteFile(filepath.Join(dir, "go.work"), []byte(gowork), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a minimal go.mod for the root.
	gomod := "module example.com/test\n\ngo 1.26.1\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create the app directory with its own go.mod.
	appDir := filepath.Join(dir, "apps", "test_app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	appMod := "module example.com/test/apps/test_app\n\ngo 1.26.1\n"
	if err := os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(appMod), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := AddToGoWork(dir, "./apps/test_app"); err != nil {
		t.Fatalf("AddToGoWork: %v", err)
	}

	// Read go.work and verify it contains the new entry.
	content, err := os.ReadFile(filepath.Join(dir, "go.work"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "apps/test_app") {
		t.Errorf("go.work should contain apps/test_app, got:\n%s", content)
	}
}

func TestReadGoModulePath(t *testing.T) {
	dir := t.TempDir()

	// With a go.mod file.
	gomod := "module github.com/my-org/my-project\n\ngo 1.26.1\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}

	got := readGoModulePath(dir)
	if got != "github.com/my-org/my-project" {
		t.Errorf("readGoModulePath = %q, want github.com/my-org/my-project", got)
	}

	// Without a go.mod file.
	emptyDir := t.TempDir()
	got = readGoModulePath(emptyDir)
	if got != "github.com/osama1998H/moca" {
		t.Errorf("readGoModulePath (fallback) = %q, want github.com/osama1998H/moca", got)
	}
}

// ---------------------------------------------------------------------------
// Desk scaffold tests (Task 13)
// ---------------------------------------------------------------------------

func TestScaffoldApp_WithDesk(t *testing.T) {
	appsDir, projectRoot := setupTestDir(t)
	opts := baseOpts(appsDir, projectRoot)
	opts.IncludeDesk = true

	if err := ScaffoldApp(opts); err != nil {
		t.Fatalf("ScaffoldApp with desk: %v", err)
	}

	appDir := filepath.Join(appsDir, "my_app")

	// Verify desk directories exist.
	for _, d := range []string{"desk", "desk/fields"} {
		p := filepath.Join(appDir, d)
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("expected directory %s to exist: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %s to be a directory", d)
		}
	}

	// Verify desk-manifest.json exists and is valid JSON.
	manifestPath := filepath.Join(appDir, "desk", "desk-manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("desk-manifest.json should exist: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("desk-manifest.json is not valid JSON: %v", err)
	}
	if parsed["app"] != "my_app" {
		t.Errorf("desk-manifest.json app = %v, want my_app", parsed["app"])
	}

	// Verify example field file exists.
	examplePath := filepath.Join(appDir, "desk", "fields", "example.ts")
	if _, err := os.Stat(examplePath); err != nil {
		t.Errorf("expected desk/fields/example.ts to exist: %v", err)
	}

	// Verify README mentions desk.
	readmeData, _ := os.ReadFile(filepath.Join(appDir, "README.md"))
	if !strings.Contains(string(readmeData), "desk/") {
		t.Error("README should mention desk/ when IncludeDesk is true")
	}
}

func TestScaffoldApp_WithoutDesk(t *testing.T) {
	appsDir, projectRoot := setupTestDir(t)
	opts := baseOpts(appsDir, projectRoot)
	opts.IncludeDesk = false

	if err := ScaffoldApp(opts); err != nil {
		t.Fatalf("ScaffoldApp: %v", err)
	}

	appDir := filepath.Join(appsDir, "my_app")

	// Verify desk directory does NOT exist.
	if _, err := os.Stat(filepath.Join(appDir, "desk")); err == nil {
		t.Error("desk/ directory should NOT exist when IncludeDesk is false")
	}

	// Verify README does not mention desk.
	readmeData, _ := os.ReadFile(filepath.Join(appDir, "README.md"))
	if strings.Contains(string(readmeData), "desk/") {
		t.Error("README should NOT mention desk/ when IncludeDesk is false")
	}
}

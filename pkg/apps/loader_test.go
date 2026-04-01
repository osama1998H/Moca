package apps

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeManifest is a test helper that creates a manifest.yaml in the given directory.
func writeManifest(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func TestScanApps_FindsAllApps(t *testing.T) {
	root := t.TempDir()

	writeManifest(t, filepath.Join(root, "alpha"), `
name: alpha
version: 1.0.0
moca_version: ">=0.1.0"
`)
	writeManifest(t, filepath.Join(root, "beta"), `
name: beta
version: 2.0.0
moca_version: ">=0.1.0"
`)

	apps, err := ScanApps(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(apps) != 2 {
		t.Fatalf("apps count = %d, want 2", len(apps))
	}
	// Should be sorted by name.
	if apps[0].Name != "alpha" {
		t.Errorf("apps[0].Name = %q, want %q", apps[0].Name, "alpha")
	}
	if apps[1].Name != "beta" {
		t.Errorf("apps[1].Name = %q, want %q", apps[1].Name, "beta")
	}
}

func TestScanApps_SkipsDirsWithoutManifest(t *testing.T) {
	root := t.TempDir()

	writeManifest(t, filepath.Join(root, "validapp"), `
name: validapp
version: 1.0.0
moca_version: ">=0.1.0"
`)
	// Create a directory without manifest.yaml.
	if err := os.MkdirAll(filepath.Join(root, "nomanifest"), 0o755); err != nil {
		t.Fatal(err)
	}

	apps, err := ScanApps(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(apps) != 1 {
		t.Fatalf("apps count = %d, want 1", len(apps))
	}
	if apps[0].Name != "validapp" {
		t.Errorf("apps[0].Name = %q, want %q", apps[0].Name, "validapp")
	}
}

func TestScanApps_EmptyDirectory(t *testing.T) {
	root := t.TempDir()

	apps, err := ScanApps(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(apps) != 0 {
		t.Errorf("apps count = %d, want 0", len(apps))
	}
}

func TestScanApps_InvalidManifestReturnsError(t *testing.T) {
	root := t.TempDir()

	writeManifest(t, filepath.Join(root, "bad"), `
name: Bad-App
version: 1.0.0
moca_version: ">=0.1.0"
`)

	_, err := ScanApps(root)
	if err == nil {
		t.Fatal("expected error for invalid manifest, got nil")
	}
}

func TestScanApps_NonexistentDirectory(t *testing.T) {
	_, err := ScanApps("/nonexistent/path/to/apps")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestLoadApp_Valid(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "myapp")

	writeManifest(t, appDir, `
name: myapp
version: 0.1.0
moca_version: ">=0.1.0"
modules:
  - name: Core
    label: Core
    doctypes:
      - User
`)

	info, err := LoadApp(appDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.Name != "myapp" {
		t.Errorf("Name = %q, want %q", info.Name, "myapp")
	}
	if info.Manifest.Version != "0.1.0" {
		t.Errorf("Version = %q, want %q", info.Manifest.Version, "0.1.0")
	}
	// Path should be absolute.
	if !filepath.IsAbs(info.Path) {
		t.Errorf("Path = %q, expected absolute path", info.Path)
	}
}

func TestLoadApp_InvalidManifest(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "bad")

	writeManifest(t, appDir, `
version: 1.0.0
moca_version: ">=0.1.0"
`)

	_, err := LoadApp(appDir)
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
}

func TestLoadApp_MissingManifestFile(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "nofile")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := LoadApp(appDir)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var me *ManifestError
	if !errors.As(err, &me) {
		t.Fatalf("expected *ManifestError, got %T", err)
	}
}

func TestValidateDependencies_AllSatisfied(t *testing.T) {
	apps := []AppInfo{
		{Name: "core", Manifest: &AppManifest{Name: "core", Version: "1.0.0"}},
		{Name: "crm", Manifest: &AppManifest{
			Name: "crm", Version: "1.0.0",
			Dependencies: []AppDep{{App: "core", MinVersion: ">=0.5.0"}},
		}},
	}

	if err := ValidateDependencies(apps); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateDependencies_MissingDep(t *testing.T) {
	apps := []AppInfo{
		{Name: "crm", Manifest: &AppManifest{
			Name: "crm", Version: "1.0.0",
			Dependencies: []AppDep{{App: "core", MinVersion: ">=0.5.0"}},
		}},
	}

	err := ValidateDependencies(apps)
	if err == nil {
		t.Fatal("expected error for missing dependency, got nil")
	}

	var de *DependencyError
	if !errors.As(err, &de) {
		t.Fatalf("expected *DependencyError, got %T", err)
	}
}

func TestValidateDependencies_VersionMismatch(t *testing.T) {
	apps := []AppInfo{
		{Name: "core", Manifest: &AppManifest{Name: "core", Version: "0.1.0"}},
		{Name: "crm", Manifest: &AppManifest{
			Name: "crm", Version: "1.0.0",
			Dependencies: []AppDep{{App: "core", MinVersion: ">=1.0.0"}},
		}},
	}

	err := ValidateDependencies(apps)
	if err == nil {
		t.Fatal("expected error for version mismatch, got nil")
	}

	var de *DependencyError
	if !errors.As(err, &de) {
		t.Fatalf("expected *DependencyError, got %T", err)
	}
}

func TestValidateDependencies_CircularDependency(t *testing.T) {
	apps := []AppInfo{
		{Name: "alpha", Manifest: &AppManifest{
			Name: "alpha", Version: "1.0.0",
			Dependencies: []AppDep{{App: "beta"}},
		}},
		{Name: "beta", Manifest: &AppManifest{
			Name: "beta", Version: "1.0.0",
			Dependencies: []AppDep{{App: "alpha"}},
		}},
	}

	err := ValidateDependencies(apps)
	if err == nil {
		t.Fatal("expected error for circular dependency, got nil")
	}

	var de *DependencyError
	if !errors.As(err, &de) {
		t.Fatalf("expected *DependencyError, got %T", err)
	}
	if len(de.Cycle) == 0 {
		t.Error("expected cycle path in error, got empty")
	}
}

func TestValidateDependencies_ThreeNodeCycle(t *testing.T) {
	apps := []AppInfo{
		{Name: "a", Manifest: &AppManifest{
			Name: "a", Version: "1.0.0",
			Dependencies: []AppDep{{App: "b"}},
		}},
		{Name: "b", Manifest: &AppManifest{
			Name: "b", Version: "1.0.0",
			Dependencies: []AppDep{{App: "c"}},
		}},
		{Name: "c", Manifest: &AppManifest{
			Name: "c", Version: "1.0.0",
			Dependencies: []AppDep{{App: "a"}},
		}},
	}

	err := ValidateDependencies(apps)
	if err == nil {
		t.Fatal("expected error for circular dependency, got nil")
	}

	var de *DependencyError
	if !errors.As(err, &de) {
		t.Fatalf("expected *DependencyError, got %T", err)
	}
	if len(de.Cycle) < 3 {
		t.Errorf("expected cycle with at least 3 nodes, got %v", de.Cycle)
	}
}

func TestValidateDependencies_NoDeps(t *testing.T) {
	apps := []AppInfo{
		{Name: "core", Manifest: &AppManifest{Name: "core", Version: "1.0.0"}},
		{Name: "other", Manifest: &AppManifest{Name: "other", Version: "1.0.0"}},
	}

	if err := ValidateDependencies(apps); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateDependencies_EmptySlice(t *testing.T) {
	if err := ValidateDependencies(nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateDependencies_DiamondDependency(t *testing.T) {
	// core <- alpha, core <- beta, alpha <- app, beta <- app
	apps := []AppInfo{
		{Name: "core", Manifest: &AppManifest{Name: "core", Version: "1.0.0"}},
		{Name: "alpha", Manifest: &AppManifest{
			Name: "alpha", Version: "1.0.0",
			Dependencies: []AppDep{{App: "core"}},
		}},
		{Name: "beta", Manifest: &AppManifest{
			Name: "beta", Version: "1.0.0",
			Dependencies: []AppDep{{App: "core"}},
		}},
		{Name: "app", Manifest: &AppManifest{
			Name: "app", Version: "1.0.0",
			Dependencies: []AppDep{{App: "alpha"}, {App: "beta"}},
		}},
	}

	if err := ValidateDependencies(apps); err != nil {
		t.Errorf("unexpected error for diamond dependency: %v", err)
	}
}

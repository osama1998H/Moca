package lockfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRead_YAML(t *testing.T) {
	lf, err := Read(filepath.Join("testdata", "valid.yaml"))
	if err != nil {
		t.Fatalf("Read valid YAML: %v", err)
	}

	if lf.MocaVersion != "1.0.0" {
		t.Errorf("MocaVersion = %q, want %q", lf.MocaVersion, "1.0.0")
	}

	if len(lf.Apps) != 2 {
		t.Fatalf("len(Apps) = %d, want 2", len(lf.Apps))
	}

	core, ok := lf.Apps["core"]
	if !ok {
		t.Fatal("missing app 'core'")
	}
	if core.Version != "1.0.0" {
		t.Errorf("core.Version = %q, want %q", core.Version, "1.0.0")
	}
	if core.Source != "builtin" {
		t.Errorf("core.Source = %q, want %q", core.Source, "builtin")
	}
	if core.Checksum != "sha256:abc123def456" {
		t.Errorf("core.Checksum = %q, want %q", core.Checksum, "sha256:abc123def456")
	}

	crm, ok := lf.Apps["crm"]
	if !ok {
		t.Fatal("missing app 'crm'")
	}
	if crm.Version != "1.2.3" {
		t.Errorf("crm.Version = %q, want %q", crm.Version, "1.2.3")
	}
	if crm.Source != "github.com/moca-apps/crm" {
		t.Errorf("crm.Source = %q, want %q", crm.Source, "github.com/moca-apps/crm")
	}
	if crm.Ref != "a1b2c3d4e5f6" {
		t.Errorf("crm.Ref = %q, want %q", crm.Ref, "a1b2c3d4e5f6")
	}
	if dep, ok := crm.Dependencies["core"]; !ok || dep != ">=1.0.0" {
		t.Errorf("crm.Dependencies[core] = %q, want %q", dep, ">=1.0.0")
	}
}

func TestRead_JSON(t *testing.T) {
	lf, err := Read(filepath.Join("testdata", "valid.json"))
	if err != nil {
		t.Fatalf("Read valid JSON: %v", err)
	}

	core, ok := lf.Apps["core"]
	if !ok {
		t.Fatal("missing app 'core'")
	}
	if core.Version != "0.1.0" {
		t.Errorf("core.Version = %q, want %q", core.Version, "0.1.0")
	}
	if core.Source != "builtin" {
		t.Errorf("core.Source = %q, want %q", core.Source, "builtin")
	}
}

func TestRead_Malformed(t *testing.T) {
	_, err := Read(filepath.Join("testdata", "malformed.yaml"))
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
}

func TestRead_Nonexistent(t *testing.T) {
	_, err := Read(filepath.Join("testdata", "does_not_exist.yaml"))
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestWrite_ProducesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "moca.lock")

	lf := &Lockfile{
		MocaVersion: "0.1.0",
		Apps: map[string]AppLock{
			"core": {
				Version:  "0.1.0",
				Source:   "builtin",
				Checksum: "sha256:test123",
			},
		},
	}

	if err := Write(path, lf); err != nil {
		t.Fatalf("Write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	content := string(data)
	if !strings.HasPrefix(content, "# Auto-generated") {
		t.Error("missing header comment")
	}
	if !strings.Contains(content, "moca_version") {
		t.Error("missing moca_version field")
	}
	if !strings.Contains(content, "generated_at") {
		t.Error("missing generated_at field")
	}
}

func TestWrite_SetsGeneratedAt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "moca.lock")

	before := time.Now().UTC()
	lf := &Lockfile{
		MocaVersion: "0.1.0",
		Apps:        map[string]AppLock{},
	}

	if err := Write(path, lf); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if lf.GeneratedAt.Before(before) {
		t.Errorf("GeneratedAt %v is before write time %v", lf.GeneratedAt, before)
	}
}

func TestWrite_PreservesExistingGeneratedAt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "moca.lock")

	fixedTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	lf := &Lockfile{
		GeneratedAt: fixedTime,
		MocaVersion: "0.1.0",
		Apps:        map[string]AppLock{},
	}

	if err := Write(path, lf); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if !lf.GeneratedAt.Equal(fixedTime) {
		t.Errorf("GeneratedAt changed from %v to %v", fixedTime, lf.GeneratedAt)
	}
}

func TestRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "moca.lock")

	original := &Lockfile{
		GeneratedAt: time.Date(2026, 3, 29, 14, 30, 0, 0, time.UTC),
		MocaVersion: "1.0.0",
		Apps: map[string]AppLock{
			"core": {
				Version:  "1.0.0",
				Source:   "builtin",
				Checksum: "sha256:abc123",
			},
			"crm": {
				Version:  "1.2.3",
				Source:   "github.com/moca-apps/crm",
				Ref:      "a1b2c3",
				Checksum: "sha256:def456",
				Dependencies: map[string]string{
					"core": ">=1.0.0",
				},
			},
		},
	}

	if err := Write(path, original); err != nil {
		t.Fatalf("Write: %v", err)
	}

	loaded, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if loaded.MocaVersion != original.MocaVersion {
		t.Errorf("MocaVersion = %q, want %q", loaded.MocaVersion, original.MocaVersion)
	}
	if len(loaded.Apps) != len(original.Apps) {
		t.Fatalf("len(Apps) = %d, want %d", len(loaded.Apps), len(original.Apps))
	}

	for name, orig := range original.Apps {
		got, ok := loaded.Apps[name]
		if !ok {
			t.Errorf("missing app %q", name)
			continue
		}
		if got.Version != orig.Version {
			t.Errorf("%s.Version = %q, want %q", name, got.Version, orig.Version)
		}
		if got.Source != orig.Source {
			t.Errorf("%s.Source = %q, want %q", name, got.Source, orig.Source)
		}
		if got.Ref != orig.Ref {
			t.Errorf("%s.Ref = %q, want %q", name, got.Ref, orig.Ref)
		}
		if got.Checksum != orig.Checksum {
			t.Errorf("%s.Checksum = %q, want %q", name, got.Checksum, orig.Checksum)
		}
	}

	crmDep := loaded.Apps["crm"].Dependencies["core"]
	if crmDep != ">=1.0.0" {
		t.Errorf("crm.Dependencies[core] = %q, want %q", crmDep, ">=1.0.0")
	}
}

func TestComputeChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	checksum, err := ComputeChecksum(path)
	if err != nil {
		t.Fatalf("ComputeChecksum: %v", err)
	}

	if !strings.HasPrefix(checksum, "sha256:") {
		t.Errorf("checksum %q does not start with 'sha256:'", checksum)
	}

	// Verify consistency.
	checksum2, err := ComputeChecksum(path)
	if err != nil {
		t.Fatalf("ComputeChecksum (second): %v", err)
	}
	if checksum != checksum2 {
		t.Errorf("checksums differ: %q vs %q", checksum, checksum2)
	}
}

func TestComputeChecksum_Nonexistent(t *testing.T) {
	_, err := ComputeChecksum("/nonexistent/file.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestIsJSON(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{"json object", `{"key": "value"}`, true},
		{"json with whitespace", `  { "key": "value" }`, true},
		{"yaml", "key: value\n", false},
		{"yaml comment", "# comment\nkey: value\n", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isJSON([]byte(tt.data))
			if got != tt.want {
				t.Errorf("isJSON(%q) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}

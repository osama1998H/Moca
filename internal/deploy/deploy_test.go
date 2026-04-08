package deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/osama1998H/moca/internal/config"
)

// mockCommander records all calls and returns preset outputs/errors.
type mockCommander struct {
	errors map[string]error
	calls  []string
}

func newMockCommander() *mockCommander {
	return &mockCommander{errors: make(map[string]error)}
}

func (m *mockCommander) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	m.calls = append(m.calls, call)
	if err, ok := m.errors[name]; ok {
		return nil, err
	}
	return []byte("ok"), nil
}

func (m *mockCommander) RunWithSudo(ctx context.Context, name string, args ...string) ([]byte, error) {
	return m.Run(ctx, name, args...)
}

func testConfig() *config.ProjectConfig {
	return &config.ProjectConfig{
		Project: config.ProjectInfo{
			Name: "testproject",
		},
		Infrastructure: config.InfrastructureConfig{
			Database: config.DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "moca",
				Password: "moca_test",
				SystemDB: "moca_test",
			},
			Redis: config.RedisConfig{
				Host: "localhost",
				Port: 6379,
			},
		},
		Production: config.ProductionConfig{
			Port:     8000,
			Workers:  "4",
			LogLevel: "warn",
			TLS: config.TLSConfig{
				Provider: "acme",
				Email:    "admin@example.com",
			},
			Proxy: config.ProxyConfig{
				Engine: "caddy",
			},
			ProcessManager: "systemd",
		},
		Backup: config.BackupConfig{
			Schedule: "0 2 * * *",
		},
	}
}

// --- GenerateID tests ---

func TestGenerateID_Format(t *testing.T) {
	id := GenerateID()
	re := regexp.MustCompile(`^dp_\d{8}_\d{6}$`)
	if !re.MatchString(id) {
		t.Errorf("GenerateID() = %q, want format dp_YYYYMMDD_HHMMSS", id)
	}
}

func TestGenerateIDAt(t *testing.T) {
	ts := time.Date(2026, 4, 8, 12, 30, 45, 0, time.UTC)
	got := generateIDAt(ts)
	want := "dp_20260408_123045"
	if got != want {
		t.Errorf("generateIDAt() = %q, want %q", got, want)
	}
}

// --- History CRUD tests ---

func TestLoadHistory_Empty(t *testing.T) {
	dir := t.TempDir()
	h, err := LoadHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Records) != 0 {
		t.Errorf("expected empty history, got %d records", len(h.Records))
	}
}

func TestLoadHistory_Roundtrip(t *testing.T) {
	dir := t.TempDir()

	h := &DeploymentHistory{
		Records: []DeploymentRecord{
			{
				ID:        "dp_20260408_120000",
				Type:      TypeSetup,
				Status:    StatusSuccess,
				StartedAt: time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC),
				Domain:    "example.com",
			},
		},
	}

	if err := SaveHistory(dir, h); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadHistory(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(loaded.Records))
	}
	if loaded.Records[0].ID != "dp_20260408_120000" {
		t.Errorf("ID = %q, want dp_20260408_120000", loaded.Records[0].ID)
	}
	if loaded.Records[0].Domain != "example.com" {
		t.Errorf("Domain = %q, want example.com", loaded.Records[0].Domain)
	}
}

func TestRecordDeployment(t *testing.T) {
	dir := t.TempDir()

	r1 := DeploymentRecord{ID: "dp_20260408_100000", Type: TypeSetup, Status: StatusSuccess}
	r2 := DeploymentRecord{ID: "dp_20260408_120000", Type: TypeUpdate, Status: StatusSuccess}

	if err := RecordDeployment(dir, r1); err != nil {
		t.Fatal(err)
	}
	if err := RecordDeployment(dir, r2); err != nil {
		t.Fatal(err)
	}

	h, err := LoadHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(h.Records))
	}
	if h.Records[1].ID != "dp_20260408_120000" {
		t.Errorf("second record ID = %q, want dp_20260408_120000", h.Records[1].ID)
	}
}

func TestFindDeployment(t *testing.T) {
	dir := t.TempDir()
	_ = RecordDeployment(dir, DeploymentRecord{ID: "dp_20260408_100000", Status: StatusSuccess})
	_ = RecordDeployment(dir, DeploymentRecord{ID: "dp_20260408_120000", Status: StatusSuccess})

	r, err := FindDeployment(dir, "dp_20260408_120000")
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "dp_20260408_120000" {
		t.Errorf("found ID = %q, want dp_20260408_120000", r.ID)
	}

	_, err = FindDeployment(dir, "dp_nonexistent")
	if err == nil {
		t.Error("expected error for missing deployment")
	}
}

func TestFindByStep(t *testing.T) {
	dir := t.TempDir()
	_ = RecordDeployment(dir, DeploymentRecord{ID: "dp_first", Status: StatusSuccess})
	_ = RecordDeployment(dir, DeploymentRecord{ID: "dp_second", Status: StatusSuccess})
	_ = RecordDeployment(dir, DeploymentRecord{ID: "dp_third", Status: StatusSuccess})

	// Step 1 = one before latest.
	r, err := FindByStep(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "dp_second" {
		t.Errorf("step 1 ID = %q, want dp_second", r.ID)
	}

	// Step 2 = two before latest.
	r, err = FindByStep(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "dp_first" {
		t.Errorf("step 2 ID = %q, want dp_first", r.ID)
	}

	// Step too far.
	_, err = FindByStep(dir, 10)
	if err == nil {
		t.Error("expected error for step too far")
	}
}

// --- Snapshot tests ---

func TestCreateSnapshot_RestoreSnapshot(t *testing.T) {
	dir := t.TempDir()
	deployID := "dp_20260408_120000"

	// Create project files.
	if err := os.WriteFile(filepath.Join(dir, "moca.yaml"), []byte("project:\n  name: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "moca.lock"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "moca-server"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create snapshot.
	if err := CreateSnapshot(dir, deployID); err != nil {
		t.Fatal(err)
	}

	// Verify snapshot directory exists.
	snapDir := snapshotDir(dir, deployID)
	if _, err := os.Stat(snapDir); err != nil {
		t.Fatalf("snapshot dir not created: %v", err)
	}

	// Verify files were copied.
	if _, err := os.Stat(filepath.Join(snapDir, "moca.yaml")); err != nil {
		t.Error("moca.yaml not in snapshot")
	}
	if _, err := os.Stat(filepath.Join(snapDir, "moca.lock")); err != nil {
		t.Error("moca.lock not in snapshot")
	}
	if _, err := os.Stat(filepath.Join(snapDir, checksumsFileName)); err != nil {
		t.Error("checksums not in snapshot")
	}

	// Modify original files.
	if err := os.WriteFile(filepath.Join(dir, "moca.yaml"), []byte("project:\n  name: modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Restore snapshot.
	if err := RestoreSnapshot(dir, deployID); err != nil {
		t.Fatal(err)
	}

	// Verify restoration.
	data, err := os.ReadFile(filepath.Join(dir, "moca.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: test") {
		t.Errorf("moca.yaml not restored, got %q", string(data))
	}
}

func TestSnapshotExists(t *testing.T) {
	dir := t.TempDir()
	if SnapshotExists(dir, "dp_nonexistent") {
		t.Error("expected false for nonexistent snapshot")
	}

	snapDir := snapshotDir(dir, "dp_test")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !SnapshotExists(dir, "dp_test") {
		t.Error("expected true for existing snapshot")
	}
}

// --- LatestDeployment tests ---

func TestLatestDeployment_Empty(t *testing.T) {
	dir := t.TempDir()
	r, err := LatestDeployment(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		t.Error("expected nil for empty history")
	}
}

func TestLatestDeployment(t *testing.T) {
	dir := t.TempDir()
	_ = RecordDeployment(dir, DeploymentRecord{ID: "dp_first"})
	_ = RecordDeployment(dir, DeploymentRecord{ID: "dp_latest"})

	r, err := LatestDeployment(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "dp_latest" {
		t.Errorf("latest ID = %q, want dp_latest", r.ID)
	}
}

// --- ListDeployments tests ---

func TestListDeployments_Limit(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for i := 0; i < 5; i++ {
		_ = RecordDeployment(dir, DeploymentRecord{
			ID:        fmt.Sprintf("dp_%d", i),
			StartedAt: now.Add(time.Duration(i) * time.Hour),
		})
	}

	records, err := ListDeployments(dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Errorf("expected 3 records, got %d", len(records))
	}
	// Should be newest first.
	if records[0].ID != "dp_4" {
		t.Errorf("first record = %q, want dp_4", records[0].ID)
	}
}

// --- Setup dry-run tests ---

func TestSetup_DryRun(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()
	cmd := newMockCommander()

	opts := SetupOptions{
		Domain:      "example.com",
		Proxy:       "caddy",
		Process:     "systemd",
		ProjectRoot: dir,
		DryRun:      true,
		Firewall:    true,
		Fail2ban:    true,
		Logrotate:   true,
	}

	record, results, err := Setup(context.Background(), opts, cfg, cmd)
	if err != nil {
		t.Fatal(err)
	}

	// Should not have executed anything.
	if len(cmd.calls) != 0 {
		t.Errorf("expected 0 commands in dry-run, got %d: %v", len(cmd.calls), cmd.calls)
	}

	// All 14 steps should be returned.
	if len(results) != 14 {
		t.Errorf("expected 14 steps, got %d", len(results))
	}

	// Record should not be recorded (dry-run).
	if record.Status != StatusInProgress {
		t.Errorf("record status = %q in dry-run, want in_progress", record.Status)
	}
}

func TestSetup_DryRun_OptionalStepsSkipped(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()
	cmd := newMockCommander()

	opts := SetupOptions{
		Domain:      "example.com",
		ProjectRoot: dir,
		DryRun:      true,
		Firewall:    false,
		Fail2ban:    false,
		Logrotate:   false,
	}

	_, results, err := Setup(context.Background(), opts, cfg, cmd)
	if err != nil {
		t.Fatal(err)
	}

	skippedCount := 0
	for _, r := range results {
		if r.Skipped {
			skippedCount++
		}
	}

	// 3 optional steps: logrotate, firewall, fail2ban.
	if skippedCount != 3 {
		t.Errorf("expected 3 skipped steps, got %d", skippedCount)
	}
}

// --- Update dry-run tests ---

func TestUpdate_DryRun(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()
	cmd := newMockCommander()

	opts := UpdateOptions{
		ProjectRoot: dir,
		DryRun:      true,
		Parallel:    2,
	}

	record, results, err := Update(context.Background(), opts, cfg, cmd)
	if err != nil {
		t.Fatal(err)
	}

	if len(cmd.calls) != 0 {
		t.Errorf("expected 0 commands in dry-run, got %d", len(cmd.calls))
	}

	// 4 phases.
	if len(results) != 4 {
		t.Errorf("expected 4 phase results, got %d", len(results))
	}

	if record.Status != StatusInProgress {
		t.Errorf("record status = %q in dry-run, want in_progress", record.Status)
	}
}

// --- Status tests ---

func TestStatus_NoDeployment(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()

	result, err := Status(context.Background(), dir, cfg)
	if err != nil {
		t.Fatal(err)
	}

	if result.CurrentDeployment != "" {
		t.Errorf("expected empty deployment, got %q", result.CurrentDeployment)
	}
	if result.SiteCount != 0 {
		t.Errorf("expected 0 sites, got %d", result.SiteCount)
	}
	if len(result.Processes) != 5 {
		t.Errorf("expected 5 processes, got %d", len(result.Processes))
	}
}

func TestStatus_WithHistory(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()

	_ = RecordDeployment(dir, DeploymentRecord{
		ID:          "dp_20260408_120000",
		Status:      StatusSuccess,
		CompletedAt: time.Now(),
	})

	// Create a site directory.
	if err := os.MkdirAll(filepath.Join(dir, "sites", "acme"), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := Status(context.Background(), dir, cfg)
	if err != nil {
		t.Fatal(err)
	}

	if result.CurrentDeployment != "dp_20260408_120000" {
		t.Errorf("current deployment = %q, want dp_20260408_120000", result.CurrentDeployment)
	}
	if result.SiteCount != 1 {
		t.Errorf("site count = %d, want 1", result.SiteCount)
	}
}

// --- Promote env diff tests ---

func TestComputeEnvDiff(t *testing.T) {
	cfg := testConfig()
	stagingPort := 8443
	stagingLogLevel := "info"
	cfg.Staging = config.StagingConfig{
		Port:     &stagingPort,
		LogLevel: &stagingLogLevel,
		Inherits: "production",
	}

	diffs := computeEnvDiff(cfg, "staging", "production")

	portDiff := findDiff(diffs, "port")
	if portDiff == nil {
		t.Fatal("no port diff found")
	}
	if !portDiff.Modified {
		t.Error("expected port to be modified")
	}
	if portDiff.Source != "8443" {
		t.Errorf("source port = %q, want 8443", portDiff.Source)
	}
	if portDiff.Target != "8000" {
		t.Errorf("target port = %q, want 8000", portDiff.Target)
	}

	logDiff := findDiff(diffs, "log_level")
	if logDiff == nil {
		t.Fatal("no log_level diff found")
	}
	if !logDiff.Modified {
		t.Error("expected log_level to be modified")
	}
}

func findDiff(diffs []EnvDiff, field string) *EnvDiff {
	for i := range diffs {
		if diffs[i].Field == field {
			return &diffs[i]
		}
	}
	return nil
}

// --- formatDuration tests ---

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		want string
		d    time.Duration
	}{
		{want: "just now", d: 30 * time.Second},
		{want: "5m", d: 5 * time.Minute},
		{want: "1 hour 30 mins", d: 90 * time.Minute},
		{want: "1 day", d: 25 * time.Hour},
		{want: "3 days", d: 72 * time.Hour},
	}

	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

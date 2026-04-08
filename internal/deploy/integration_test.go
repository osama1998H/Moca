//go:build integration

package deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/osama1998H/moca/internal/config"
)

// TestIntegration_SetupDryRun_AllSteps validates that a full 14-step dry-run
// returns all step descriptions without executing any commands.
func TestIntegration_SetupDryRun_AllSteps(t *testing.T) {
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
		t.Fatalf("Setup(dry-run) returned error: %v", err)
	}

	// No commands should have been executed.
	if len(cmd.calls) != 0 {
		t.Errorf("expected 0 commands in dry-run, got %d: %v", len(cmd.calls), cmd.calls)
	}

	// All 14 steps must be returned.
	if len(results) != 14 {
		t.Fatalf("expected 14 steps, got %d", len(results))
	}

	// Each step must have a Name and Description.
	for i, sr := range results {
		if sr.Name == "" {
			t.Errorf("step %d: Name is empty", i+1)
		}
		if sr.Description == "" {
			t.Errorf("step %d (%s): Description is empty", i+1, sr.Name)
		}
		if sr.Number != i+1 {
			t.Errorf("step %d (%s): Number = %d, want %d", i+1, sr.Name, sr.Number, i+1)
		}
	}

	// Optional steps (firewall, fail2ban, logrotate) must NOT be skipped when enabled.
	optionalSteps := map[string]bool{"firewall": false, "fail2ban": false, "logrotate": false}
	for _, sr := range results {
		if _, ok := optionalSteps[sr.Name]; ok {
			optionalSteps[sr.Name] = sr.Skipped
		}
	}
	for name, skipped := range optionalSteps {
		if skipped {
			t.Errorf("optional step %q was skipped even though it was enabled", name)
		}
	}

	// Record should remain in-progress (not persisted during dry-run).
	if record.Status != StatusInProgress {
		t.Errorf("record.Status = %q, want %q", record.Status, StatusInProgress)
	}
}

// TestIntegration_UpdateAutoRollback verifies that a migration failure during
// Update triggers automatic rollback, restoring the config snapshot.
func TestIntegration_UpdateAutoRollback(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()

	// Create required project files.
	originalYAML := "project:\n  name: original\n"
	if err := os.WriteFile(filepath.Join(dir, "moca.yaml"), []byte(originalYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "moca.lock"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "moca-server"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a site directory so phasePrepare succeeds.
	if err := os.MkdirAll(filepath.Join(dir, "sites", "testsite"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Pre-create a deployment ID and its snapshot so auto-rollback can find it.
	// We need to know what ID Update() will generate. Since GenerateID() uses time.Now(),
	// we create the snapshot after Update starts. Instead, we manually create a snapshot
	// for a known ID, then use NoBackup=true (which skips phaseBackup) and NoBuild=true
	// (which skips build). The mock will fail on the moca migrate command.
	//
	// However, the deployment ID is generated inside Update() and we cannot predict it.
	// So we rely on a different approach: set NoBackup=false so phaseBackup runs and
	// creates the snapshot, but skip actual pg_dump by having no sites to back up.
	// Actually, there IS a site (testsite), so backup.Create will fail trying to
	// run pg_dump.
	//
	// Simplest approach: create a snapshot for every possible deployment ID prefix.
	// But that's not feasible either.
	//
	// Best approach: Use NoBackup=true and NoBuild=true. The migration will fail.
	// performAutoRollback will report "no snapshot found" since NoBackup skipped
	// snapshot creation. The error should contain both "migration failed" and
	// "rollback also failed".

	mocaBin := filepath.Join(dir, "bin", "moca")
	cmd := newMockCommander()
	cmd.errors[mocaBin] = fmt.Errorf("migration error: column conflict")

	record, _, err := Update(context.Background(), UpdateOptions{
		ProjectRoot: dir,
		NoBackup:    true,
		NoBuild:     true,
		Parallel:    1,
	}, cfg, cmd)

	// Update should return an error because migration failed.
	if err == nil {
		t.Fatal("expected error from Update, got nil")
	}

	// The error should mention migration failure.
	errMsg := err.Error()
	if !strings.Contains(errMsg, "migration failed") {
		t.Errorf("error should mention 'migration failed', got: %s", errMsg)
	}

	// Since NoBackup=true means no snapshot was created, auto-rollback should
	// also fail, and the error should mention both failures.
	if !strings.Contains(errMsg, "rollback also failed") {
		t.Errorf("error should mention 'rollback also failed', got: %s", errMsg)
	}

	// The record status should be failed.
	if record.Status != StatusFailed {
		t.Errorf("record.Status = %q, want %q", record.Status, StatusFailed)
	}
}

// TestIntegration_UpdateAutoRollback_WithSnapshot verifies that when a snapshot
// exists, auto-rollback actually restores the original config files.
func TestIntegration_UpdateAutoRollback_WithSnapshot(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()

	// Create required project files.
	originalYAML := "project:\n  name: original\n"
	if err := os.WriteFile(filepath.Join(dir, "moca.yaml"), []byte(originalYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "moca.lock"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create sites directory (needed for phasePrepare).
	if err := os.MkdirAll(filepath.Join(dir, "sites", "testsite"), 0o755); err != nil {
		t.Fatal(err)
	}

	// We need to pre-create a snapshot that matches the deployment ID that Update()
	// will generate. Since the ID is time-based, we create snapshots for a range of
	// seconds around now.
	now := time.Now().UTC()
	for offset := -2; offset <= 2; offset++ {
		ts := now.Add(time.Duration(offset) * time.Second)
		deployID := generateIDAt(ts)
		if err := CreateSnapshot(dir, deployID); err != nil {
			t.Fatalf("pre-create snapshot %s: %v", deployID, err)
		}
	}

	// Mutate moca.yaml so we can verify rollback restores the original.
	if err := os.WriteFile(filepath.Join(dir, "moca.yaml"), []byte("project:\n  name: mutated\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mocaBin := filepath.Join(dir, "bin", "moca")
	cmd := newMockCommander()
	cmd.errors[mocaBin] = fmt.Errorf("migration error: column conflict")

	_, _, err := Update(context.Background(), UpdateOptions{
		ProjectRoot: dir,
		NoBackup:    true,
		NoBuild:     true,
		Parallel:    1,
	}, cfg, cmd)

	if err == nil {
		t.Fatal("expected error from Update, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "migration failed") {
		t.Errorf("error should mention 'migration failed', got: %s", errMsg)
	}

	// The error should indicate rollback succeeded (no "rollback also failed").
	if !strings.Contains(errMsg, "rolled back") {
		t.Errorf("error should mention 'rolled back', got: %s", errMsg)
	}

	// Verify moca.yaml was restored to the original content.
	data, err := os.ReadFile(filepath.Join(dir, "moca.yaml"))
	if err != nil {
		t.Fatalf("read moca.yaml: %v", err)
	}
	if !strings.Contains(string(data), "name: original") {
		t.Errorf("moca.yaml should be restored to original, got: %s", string(data))
	}
}

// TestIntegration_PromoteDryRun validates that a promote dry-run computes the
// environment diff correctly and populates the record metadata.
func TestIntegration_PromoteDryRun(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()
	cmd := newMockCommander()

	// Configure staging overrides.
	stagingPort := 8443
	stagingLogLevel := "info"
	cfg.Staging = config.StagingConfig{
		Port:     &stagingPort,
		LogLevel: &stagingLogLevel,
		Inherits: "production",
	}

	opts := PromoteOptions{
		SourceEnv:   "staging",
		TargetEnv:   "production",
		ProjectRoot: dir,
		DryRun:      true,
	}

	record, diffs, err := Promote(context.Background(), opts, cfg, cmd)
	if err != nil {
		t.Fatalf("Promote(dry-run) returned error: %v", err)
	}

	// Record metadata must reflect promote operation.
	if record.Type != TypePromote {
		t.Errorf("record.Type = %q, want %q", record.Type, TypePromote)
	}
	if record.PromotedFrom != "staging" {
		t.Errorf("record.PromotedFrom = %q, want %q", record.PromotedFrom, "staging")
	}
	if record.PromotedTo != "production" {
		t.Errorf("record.PromotedTo = %q, want %q", record.PromotedTo, "production")
	}

	// Port diff: staging=8443, production=8000 -- must be Modified.
	portDiff := findDiff(diffs, "port")
	if portDiff == nil {
		t.Fatal("no port diff found")
	}
	if !portDiff.Modified {
		t.Error("expected port diff to be modified")
	}
	if portDiff.Source != "8443" {
		t.Errorf("port source = %q, want %q", portDiff.Source, "8443")
	}
	if portDiff.Target != "8000" {
		t.Errorf("port target = %q, want %q", portDiff.Target, "8000")
	}

	// log_level diff: staging="info", production="warn" -- must be Modified.
	logDiff := findDiff(diffs, "log_level")
	if logDiff == nil {
		t.Fatal("no log_level diff found")
	}
	if !logDiff.Modified {
		t.Error("expected log_level diff to be modified")
	}
	if logDiff.Source != "info" {
		t.Errorf("log_level source = %q, want %q", logDiff.Source, "info")
	}
	if logDiff.Target != "warn" {
		t.Errorf("log_level target = %q, want %q", logDiff.Target, "warn")
	}

	// No commands should have been executed (dry-run).
	if len(cmd.calls) != 0 {
		t.Errorf("expected 0 commands in dry-run, got %d: %v", len(cmd.calls), cmd.calls)
	}
}

// TestIntegration_DeploymentHistoryCRUD exercises end-to-end history operations:
// record, list, find, and error handling for nonexistent IDs.
func TestIntegration_DeploymentHistoryCRUD(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// Record 5 deployments with ascending timestamps.
	ids := make([]string, 5)
	for i := 0; i < 5; i++ {
		ids[i] = fmt.Sprintf("dp_test_%d", i)
		r := DeploymentRecord{
			ID:        ids[i],
			Type:      TypeUpdate,
			Status:    StatusSuccess,
			StartedAt: now.Add(time.Duration(i) * time.Hour),
		}
		if err := RecordDeployment(dir, r); err != nil {
			t.Fatalf("RecordDeployment(%s): %v", ids[i], err)
		}
	}

	// ListDeployments(3) should return 3 records, newest-first.
	listed, err := ListDeployments(dir, 3)
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("ListDeployments(3) returned %d records, want 3", len(listed))
	}
	// Newest is dp_test_4.
	if listed[0].ID != "dp_test_4" {
		t.Errorf("listed[0].ID = %q, want %q", listed[0].ID, "dp_test_4")
	}
	if listed[1].ID != "dp_test_3" {
		t.Errorf("listed[1].ID = %q, want %q", listed[1].ID, "dp_test_3")
	}
	if listed[2].ID != "dp_test_2" {
		t.Errorf("listed[2].ID = %q, want %q", listed[2].ID, "dp_test_2")
	}

	// FindDeployment by known ID should succeed.
	found, err := FindDeployment(dir, "dp_test_2")
	if err != nil {
		t.Fatalf("FindDeployment(dp_test_2): %v", err)
	}
	if found.ID != "dp_test_2" {
		t.Errorf("found.ID = %q, want %q", found.ID, "dp_test_2")
	}
	if found.Status != StatusSuccess {
		t.Errorf("found.Status = %q, want %q", found.Status, StatusSuccess)
	}

	// FindDeployment by nonexistent ID should return error.
	_, err = FindDeployment(dir, "dp_nonexistent_99")
	if err == nil {
		t.Error("expected error for nonexistent deployment ID, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %s", err.Error())
	}

	// LatestDeployment should return the most recently appended record (dp_test_4).
	latest, err := LatestDeployment(dir)
	if err != nil {
		t.Fatalf("LatestDeployment: %v", err)
	}
	if latest.ID != "dp_test_4" {
		t.Errorf("latest.ID = %q, want %q", latest.ID, "dp_test_4")
	}

	// Full list (n=0) should return all 5.
	all, err := ListDeployments(dir, 0)
	if err != nil {
		t.Fatalf("ListDeployments(0): %v", err)
	}
	if len(all) != 5 {
		t.Errorf("ListDeployments(0) returned %d records, want 5", len(all))
	}
}

// TestIntegration_SetupGeneratesProxy verifies that stepGenerateProxy creates
// the expected Caddyfile for the caddy proxy engine.
func TestIntegration_SetupGeneratesProxy(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()

	opts := SetupOptions{
		Domain:      "example.com",
		Proxy:       "caddy",
		ProjectRoot: dir,
	}

	if err := stepGenerateProxy(cfg, opts); err != nil {
		t.Fatalf("stepGenerateProxy: %v", err)
	}

	caddyfile := filepath.Join(dir, "config", "caddy", "Caddyfile")
	if _, err := os.Stat(caddyfile); err != nil {
		t.Fatalf("Caddyfile not created at %s: %v", caddyfile, err)
	}

	data, err := os.ReadFile(caddyfile)
	if err != nil {
		t.Fatalf("read Caddyfile: %v", err)
	}
	content := string(data)

	// The Caddyfile should reference the domain.
	if !strings.Contains(content, "example.com") {
		t.Error("Caddyfile does not contain the domain 'example.com'")
	}

	// Test nginx variant as well.
	opts.Proxy = "nginx"
	if err := stepGenerateProxy(cfg, opts); err != nil {
		t.Fatalf("stepGenerateProxy(nginx): %v", err)
	}

	nginxConf := filepath.Join(dir, "config", "nginx", "moca.conf")
	if _, err := os.Stat(nginxConf); err != nil {
		t.Fatalf("nginx config not created at %s: %v", nginxConf, err)
	}
}

// TestIntegration_SetupGeneratesProcessMgr verifies that stepGenerateProcessMgr
// creates systemd unit files in the expected directory.
func TestIntegration_SetupGeneratesProcessMgr(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()

	opts := SetupOptions{
		Process:     "systemd",
		ProjectRoot: dir,
	}

	if err := stepGenerateProcessMgr(cfg, opts); err != nil {
		t.Fatalf("stepGenerateProcessMgr: %v", err)
	}

	systemdDir := filepath.Join(dir, "config", "systemd")
	entries, err := os.ReadDir(systemdDir)
	if err != nil {
		t.Fatalf("read systemd dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no systemd unit files generated")
	}

	// Verify key unit files exist.
	expectedUnits := []string{
		"moca-server@.service",
		"moca-worker@.service",
		"moca-scheduler.service",
		"moca.target",
	}
	for _, unit := range expectedUnits {
		path := filepath.Join(systemdDir, unit)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected systemd unit %s not found: %v", unit, err)
		}
	}

	// Test docker variant as well.
	opts.Process = "docker"
	if err := stepGenerateProcessMgr(cfg, opts); err != nil {
		t.Fatalf("stepGenerateProcessMgr(docker): %v", err)
	}

	composePath := filepath.Join(dir, "config", "docker", "docker-compose.yml")
	if _, err := os.Stat(composePath); err != nil {
		t.Fatalf("docker-compose.yml not created: %v", err)
	}
}

// TestIntegration_StatusWithConfig verifies that Status correctly detects
// existing proxy and process-manager configurations.
func TestIntegration_StatusWithConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()

	// Create proxy config (Caddyfile).
	caddyDir := filepath.Join(dir, "config", "caddy")
	if err := os.MkdirAll(caddyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caddyDir, "Caddyfile"), []byte("example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create process-manager config (systemd unit).
	systemdDir := filepath.Join(dir, "config", "systemd")
	if err := os.MkdirAll(systemdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(systemdDir, "moca-server@.service"), []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a site directory to verify site count.
	if err := os.MkdirAll(filepath.Join(dir, "sites", "acme"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sites", "globex"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Record a deployment so CurrentDeployment is populated.
	if err := RecordDeployment(dir, DeploymentRecord{
		ID:          "dp_20260408_150000",
		Status:      StatusSuccess,
		CompletedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	result, err := Status(context.Background(), dir, cfg)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if !result.Config.ProxyConfigured {
		t.Error("expected ProxyConfigured = true")
	}
	if !result.Config.ProcessMgrConfigured {
		t.Error("expected ProcessMgrConfigured = true")
	}
	if result.SiteCount != 2 {
		t.Errorf("SiteCount = %d, want 2", result.SiteCount)
	}
	if result.CurrentDeployment != "dp_20260408_150000" {
		t.Errorf("CurrentDeployment = %q, want %q", result.CurrentDeployment, "dp_20260408_150000")
	}
	if result.Uptime <= 0 {
		t.Error("expected positive Uptime for a successful deployment")
	}

	// All 5 processes should be reported (even if stopped).
	if len(result.Processes) != 5 {
		t.Errorf("expected 5 processes, got %d", len(result.Processes))
	}
}

// TestIntegration_SnapshotCreateAndRestore exercises the full snapshot lifecycle:
// create, verify existence, modify files, restore, and verify restoration.
func TestIntegration_SnapshotCreateAndRestore(t *testing.T) {
	dir := t.TempDir()
	deployID := "dp_integration_snap"

	// Set up project files.
	if err := os.WriteFile(filepath.Join(dir, "moca.yaml"), []byte("project:\n  name: snap-test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "moca.lock"), []byte("version: 42\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "moca-server"), []byte("server-v1"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Snapshot should not exist yet.
	if SnapshotExists(dir, deployID) {
		t.Fatal("snapshot should not exist before creation")
	}

	// Create snapshot.
	if err := CreateSnapshot(dir, deployID); err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	// Snapshot should now exist.
	if !SnapshotExists(dir, deployID) {
		t.Fatal("snapshot should exist after creation")
	}

	// Verify snapshot contains expected files.
	snapDir := snapshotDir(dir, deployID)
	for _, name := range []string{"moca.yaml", "moca.lock", checksumsFileName} {
		if _, err := os.Stat(filepath.Join(snapDir, name)); err != nil {
			t.Errorf("snapshot missing %s: %v", name, err)
		}
	}

	// Mutate project files.
	if err := os.WriteFile(filepath.Join(dir, "moca.yaml"), []byte("project:\n  name: mutated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "moca.lock"), []byte("version: 999\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Restore snapshot.
	if err := RestoreSnapshot(dir, deployID); err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}

	// Verify files are restored.
	yamlData, err := os.ReadFile(filepath.Join(dir, "moca.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(yamlData), "name: snap-test") {
		t.Errorf("moca.yaml not restored, got: %s", string(yamlData))
	}

	lockData, err := os.ReadFile(filepath.Join(dir, "moca.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lockData), "version: 42") {
		t.Errorf("moca.lock not restored, got: %s", string(lockData))
	}
}

// TestIntegration_SetupStepRedisConfig verifies that step 6 (redis) generates
// a Redis production configuration file.
func TestIntegration_SetupStepRedisConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig()

	if err := stepRedisConfig(dir, cfg); err != nil {
		t.Fatalf("stepRedisConfig: %v", err)
	}

	redisConf := filepath.Join(dir, "config", "redis", "redis-production.conf")
	if _, err := os.Stat(redisConf); err != nil {
		t.Fatalf("redis config not created: %v", err)
	}

	data, err := os.ReadFile(redisConf)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "localhost") {
		t.Error("redis config should contain bind address 'localhost'")
	}
	if !strings.Contains(content, "6379") {
		t.Error("redis config should contain port 6379")
	}
	if !strings.Contains(content, "maxmemory-policy") {
		t.Error("redis config should contain maxmemory-policy directive")
	}
}

// TestIntegration_SetupSwitchProdMode verifies that step 2 creates the
// environment marker file.
func TestIntegration_SetupSwitchProdMode(t *testing.T) {
	dir := t.TempDir()

	if err := stepSwitchProdMode(dir); err != nil {
		t.Fatalf("stepSwitchProdMode: %v", err)
	}

	envFile := filepath.Join(dir, ".moca", "environment")
	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("environment file not created: %v", err)
	}
	if strings.TrimSpace(string(data)) != "production" {
		t.Errorf("environment file = %q, want 'production'", strings.TrimSpace(string(data)))
	}
}

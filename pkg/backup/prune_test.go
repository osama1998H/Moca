package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/osama1998H/moca/internal/config"
)

var testNow = time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)

func makeBackup(id, site string, age time.Duration) BackupInfo {
	return BackupInfo{
		ID:        id,
		Site:      site,
		Type:      "full",
		CreatedAt: testNow.Add(-age),
	}
}

func TestClassifyBackups(t *testing.T) {
	backups := []BackupInfo{
		makeBackup("bk_1h", "s", 1*time.Hour),      // daily bucket (< 7d)
		makeBackup("bk_3d", "s", 3*24*time.Hour),    // daily
		makeBackup("bk_6d", "s", 6*24*time.Hour),    // daily
		makeBackup("bk_10d", "s", 10*24*time.Hour),  // weekly (7d-30d)
		makeBackup("bk_20d", "s", 20*24*time.Hour),  // weekly
		makeBackup("bk_35d", "s", 35*24*time.Hour),  // monthly (>30d)
		makeBackup("bk_90d", "s", 90*24*time.Hour),  // monthly
		makeBackup("bk_180d", "s", 180*24*time.Hour), // monthly
	}

	daily, weekly, monthly := classifyBackups(backups, testNow)

	if len(daily) != 3 {
		t.Errorf("daily: got %d, want 3", len(daily))
	}
	if len(weekly) != 2 {
		t.Errorf("weekly: got %d, want 2", len(weekly))
	}
	if len(monthly) != 3 {
		t.Errorf("monthly: got %d, want 3", len(monthly))
	}

	// Verify sorted newest-first within each bucket.
	if daily[0].ID != "bk_1h" {
		t.Errorf("daily[0] = %s, want bk_1h", daily[0].ID)
	}
	if weekly[0].ID != "bk_10d" {
		t.Errorf("weekly[0] = %s, want bk_10d", weekly[0].ID)
	}
	if monthly[0].ID != "bk_35d" {
		t.Errorf("monthly[0] = %s, want bk_35d", monthly[0].ID)
	}
}

func TestSelectPruneCandidates(t *testing.T) {
	daily := []BackupInfo{
		makeBackup("d1", "s", 1*24*time.Hour),
		makeBackup("d2", "s", 2*24*time.Hour),
		makeBackup("d3", "s", 3*24*time.Hour),
		makeBackup("d4", "s", 4*24*time.Hour),
		makeBackup("d5", "s", 5*24*time.Hour),
	}
	weekly := []BackupInfo{
		makeBackup("w1", "s", 8*24*time.Hour),
		makeBackup("w2", "s", 14*24*time.Hour),
		makeBackup("w3", "s", 21*24*time.Hour),
	}
	monthly := []BackupInfo{
		makeBackup("m1", "s", 35*24*time.Hour),
		makeBackup("m2", "s", 60*24*time.Hour),
		makeBackup("m3", "s", 90*24*time.Hour),
		makeBackup("m4", "s", 120*24*time.Hour),
	}

	ret := config.RetentionConfig{Daily: 3, Weekly: 2, Monthly: 2}
	candidates := selectPruneCandidates(daily, weekly, monthly, ret)

	// daily: keep 3, prune d4+d5 = 2
	// weekly: keep 2, prune w3 = 1
	// monthly: keep 2, prune m3+m4 = 2
	if len(candidates) != 5 {
		t.Fatalf("candidates: got %d, want 5", len(candidates))
	}

	ids := make(map[string]bool)
	for _, c := range candidates {
		ids[c.ID] = true
	}
	for _, want := range []string{"d4", "d5", "w3", "m3", "m4"} {
		if !ids[want] {
			t.Errorf("expected %s in prune candidates", want)
		}
	}
	for _, notWant := range []string{"d1", "d2", "d3", "w1", "w2", "m1", "m2"} {
		if ids[notWant] {
			t.Errorf("did not expect %s in prune candidates", notWant)
		}
	}
}

func TestSelectPruneCandidates_AllZeroRetention(t *testing.T) {
	daily := []BackupInfo{makeBackup("d1", "s", 2*24*time.Hour)}
	weekly := []BackupInfo{makeBackup("w1", "s", 10*24*time.Hour)}
	monthly := []BackupInfo{makeBackup("m1", "s", 35*24*time.Hour)}

	ret := config.RetentionConfig{Daily: 0, Weekly: 0, Monthly: 0}
	candidates := selectPruneCandidates(daily, weekly, monthly, ret)

	// All-zero means keep nothing → all are candidates.
	if len(candidates) != 3 {
		t.Errorf("candidates: got %d, want 3", len(candidates))
	}
}

func TestPrune_SafetyWindow(t *testing.T) {
	// Create a temp directory with backup files.
	dir := t.TempDir()
	site := "acme.localhost"
	backupDir := filepath.Join(dir, "sites", site, "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a backup file with a timestamp that's 1 hour old.
	// This should be protected by the 24h safety window even with Daily=0.
	recentFile := filepath.Join(backupDir, "bk_acme_20260408_110000.sql.gz")
	if err := os.WriteFile(recentFile, []byte("recent"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create an older backup that's 3 days old.
	oldFile := filepath.Join(backupDir, "bk_acme_20260405_120000.sql.gz")
	if err := os.WriteFile(oldFile, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Prune(context.Background(), PruneOptions{
		Site:        site,
		ProjectRoot: dir,
		Retention:   config.RetentionConfig{Daily: 0, Weekly: 0, Monthly: 0},
		DryRun:      true,
		Now:         testNow,
	})
	if err != nil {
		t.Fatal(err)
	}

	// The recent backup (1h old) should be in Kept, not Deleted.
	for _, d := range result.Deleted {
		if d.ID == "bk_acme_20260408_110000" {
			t.Error("backup within 24h safety window should not be pruned")
		}
	}
	// The 3-day-old backup should be in Deleted (Daily=0 means keep nothing).
	found := false
	for _, d := range result.Deleted {
		if d.ID == "bk_acme_20260405_120000" {
			found = true
		}
	}
	if !found {
		t.Error("3-day-old backup should be a prune candidate with Daily=0")
	}
}

func TestPrune_DryRun(t *testing.T) {
	dir := t.TempDir()
	site := "acme.localhost"
	backupDir := filepath.Join(dir, "sites", site, "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create some old backup files.
	for _, name := range []string{
		"bk_acme_20260401_120000.sql.gz",
		"bk_acme_20260402_120000.sql.gz",
		"bk_acme_20260403_120000.sql.gz",
	} {
		p := filepath.Join(backupDir, name)
		if err := os.WriteFile(p, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Prune(context.Background(), PruneOptions{
		Site:        site,
		ProjectRoot: dir,
		Retention:   config.RetentionConfig{Daily: 1, Weekly: 0, Monthly: 0},
		DryRun:      true,
		Now:         testNow,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !result.DryRun {
		t.Error("expected DryRun=true")
	}
	// With Daily=1, keep newest (Apr 3), prune Apr 1 and Apr 2.
	if len(result.Deleted) != 2 {
		t.Errorf("deleted: got %d, want 2", len(result.Deleted))
	}

	// Files should still exist because it's dry-run.
	for _, name := range []string{
		"bk_acme_20260401_120000.sql.gz",
		"bk_acme_20260402_120000.sql.gz",
	} {
		p := filepath.Join(backupDir, name)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			t.Errorf("file %s should still exist in dry-run mode", name)
		}
	}
}

func TestPrune_ActualDelete(t *testing.T) {
	dir := t.TempDir()
	site := "acme.localhost"
	backupDir := filepath.Join(dir, "sites", site, "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}

	files := []string{
		"bk_acme_20260401_120000.sql.gz", // 7 days old
		"bk_acme_20260402_120000.sql.gz", // 6 days old
		"bk_acme_20260403_120000.sql.gz", // 5 days old
	}
	for _, name := range files {
		p := filepath.Join(backupDir, name)
		if err := os.WriteFile(p, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Prune(context.Background(), PruneOptions{
		Site:        site,
		ProjectRoot: dir,
		Retention:   config.RetentionConfig{Daily: 1, Weekly: 0, Monthly: 0},
		DryRun:      false,
		Now:         testNow,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.DryRun {
		t.Error("expected DryRun=false")
	}
	if len(result.Deleted) != 2 {
		t.Errorf("deleted: got %d, want 2", len(result.Deleted))
	}
	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}

	// Deleted files should be gone.
	for _, name := range []string{
		"bk_acme_20260401_120000.sql.gz",
		"bk_acme_20260402_120000.sql.gz",
	} {
		p := filepath.Join(backupDir, name)
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("file %s should have been deleted", name)
		}
	}

	// Kept file should still exist.
	keptPath := filepath.Join(backupDir, "bk_acme_20260403_120000.sql.gz")
	if _, err := os.Stat(keptPath); os.IsNotExist(err) {
		t.Error("kept file should still exist")
	}
}

func TestPrune_EmptyList(t *testing.T) {
	dir := t.TempDir()
	site := "acme.localhost"
	backupDir := filepath.Join(dir, "sites", site, "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := Prune(context.Background(), PruneOptions{
		Site:        site,
		ProjectRoot: dir,
		Retention:   config.RetentionConfig{Daily: 7, Weekly: 4, Monthly: 3},
		DryRun:      false,
		Now:         testNow,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Deleted) != 0 {
		t.Errorf("deleted: got %d, want 0", len(result.Deleted))
	}
	if len(result.Kept) != 0 {
		t.Errorf("kept: got %d, want 0", len(result.Kept))
	}
}

func TestPrune_RetentionKeepsCorrectSet(t *testing.T) {
	// Validate the specific acceptance criterion:
	// Prune with retention {Daily: 7, Weekly: 4, Monthly: 3} keeps correct set.
	dir := t.TempDir()
	site := "acme.localhost"
	backupDir := filepath.Join(dir, "sites", site, "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create backups across all three age buckets.
	// Daily bucket (< 7 days): 10 backups
	// Weekly bucket (7-30 days): 6 backups
	// Monthly bucket (> 30 days): 5 backups
	type entry struct {
		name   string
		ageDays int
	}
	entries := []entry{
		// Daily: days 1-6 (within 7 days)
		{"bk_acme_20260407_120000.sql.gz", 1},
		{"bk_acme_20260406_120000.sql.gz", 2},
		{"bk_acme_20260405_120000.sql.gz", 3},
		{"bk_acme_20260404_120000.sql.gz", 4},
		{"bk_acme_20260403_120000.sql.gz", 5},
		{"bk_acme_20260402_120000.sql.gz", 6},
		// Weekly: days 8-28
		{"bk_acme_20260331_120000.sql.gz", 8},
		{"bk_acme_20260325_120000.sql.gz", 14},
		{"bk_acme_20260318_120000.sql.gz", 21},
		{"bk_acme_20260312_120000.sql.gz", 27},
		// Monthly: days 35-120
		{"bk_acme_20260304_120000.sql.gz", 35},
		{"bk_acme_20260207_120000.sql.gz", 60},
		{"bk_acme_20260109_120000.sql.gz", 90},
		{"bk_acme_20251210_120000.sql.gz", 120},
	}

	for _, e := range entries {
		p := filepath.Join(backupDir, e.name)
		if err := os.WriteFile(p, []byte("backup-data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Prune(context.Background(), PruneOptions{
		Site:        site,
		ProjectRoot: dir,
		Retention:   config.RetentionConfig{Daily: 7, Weekly: 4, Monthly: 3},
		DryRun:      true,
		Now:         testNow,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Daily: 6 backups, keep 7 → prune 0 (only 6, all kept)
	// Weekly: 4 backups, keep 4 → prune 0 (exactly 4, all kept)
	// Monthly: 4 backups, keep 3 → prune 1 (the oldest: bk_acme_20251210)
	if len(result.Deleted) != 1 {
		t.Errorf("deleted: got %d, want 1", len(result.Deleted))
		for _, d := range result.Deleted {
			t.Logf("  deleted: %s (age: %s)", d.ID, testNow.Sub(d.CreatedAt))
		}
	}
	if len(result.Kept) != 13 {
		t.Errorf("kept: got %d, want 13", len(result.Kept))
	}

	// The pruned one should be the oldest monthly backup.
	if len(result.Deleted) == 1 && result.Deleted[0].ID != "bk_acme_20251210_120000" {
		t.Errorf("expected bk_acme_20251210_120000 to be pruned, got %s", result.Deleted[0].ID)
	}
}

func TestExcessBackups(t *testing.T) {
	backups := []BackupInfo{
		{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"},
	}

	tests := []struct {
		name     string
		keep     int
		wantLen  int
	}{
		{"keep 3 of 5", 3, 2},
		{"keep 0 of 5", 0, 5},
		{"keep 10 of 5", 10, 0},
		{"keep 5 of 5", 5, 0},
		{"keep 1 of 5", 1, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := excessBackups(backups, tt.keep)
			if len(got) != tt.wantLen {
				t.Errorf("excessBackups(5, %d) = %d items, want %d", tt.keep, len(got), tt.wantLen)
			}
		})
	}
}

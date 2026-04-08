//go:build integration

package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/pkg/storage"
)

var testS3Client *storage.S3Storage

func TestMain(m *testing.M) {
	endpoint := os.Getenv("MINIO_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:9000"
	}

	client, err := storage.NewS3Storage(config.StorageConfig{
		Driver:    "s3",
		Endpoint:  endpoint,
		Bucket:    "moca-backup-test",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot create S3 client: %v\n", err)
		os.Exit(0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.EnsureBucket(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: MinIO unreachable: %v\n", err)
		os.Exit(0)
	}

	testS3Client = client
	code := m.Run()

	// Clean up: remove all objects in the test bucket.
	cleanCtx := context.Background()
	objects, _ := testS3Client.ListObjects(cleanCtx, "", true)
	for _, obj := range objects {
		_ = testS3Client.Delete(cleanCtx, obj.Key)
	}

	os.Exit(code)
}

// createFakeBackupFile writes a file with known content to the specified path
// and returns the SHA-256 checksum of its content.
func createFakeBackupFile(t *testing.T, dir, filename string, content []byte) (string, string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	h := sha256.Sum256(content)
	return path, hex.EncodeToString(h[:])
}

// cleanupRemoteKey registers a t.Cleanup to delete a remote key after the test.
func cleanupRemoteKey(t *testing.T, rs *RemoteStorage, key string) {
	t.Helper()
	t.Cleanup(func() {
		_ = rs.DeleteRemote(context.Background(), key)
	})
}

func TestIntegration_BackupUploadDownload(t *testing.T) {
	ctx := context.Background()
	rs := NewRemoteStorage(testS3Client, "test")

	// Create a local fake backup file with known content.
	projectRoot := t.TempDir()
	backupDir := filepath.Join(projectRoot, "sites", "testsite", "backups")
	content := []byte("CREATE TABLE tab_user (name TEXT PRIMARY KEY);\nINSERT INTO tab_user VALUES ('admin');")
	filePath, expectedChecksum := createFakeBackupFile(t, backupDir, "bk_testsite_20260408_120000.sql.gz", content)

	info := BackupInfo{
		ID:         "bk_testsite_20260408_120000",
		Site:       "testsite",
		Type:       "full",
		Path:       filePath,
		Size:       int64(len(content)),
		CreatedAt:  time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC),
		Compressed: true,
	}

	// Upload the backup.
	remoteKey, err := rs.Upload(ctx, info)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	cleanupRemoteKey(t, rs, remoteKey)

	if remoteKey == "" {
		t.Fatal("expected non-empty remote key")
	}

	// Verify the file exists on S3.
	exists, err := testS3Client.Exists(ctx, remoteKey)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("uploaded file does not exist on S3")
	}

	// Download to a different temp dir.
	downloadDir := t.TempDir()
	localPath, checksum, err := rs.Download(ctx, remoteKey, downloadDir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	// Verify downloaded file content matches original.
	downloaded, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", localPath, err)
	}
	if string(downloaded) != string(content) {
		t.Errorf("downloaded content mismatch: got %d bytes, want %d bytes", len(downloaded), len(content))
	}

	// Verify checksum matches.
	if checksum != expectedChecksum {
		t.Errorf("checksum = %q, want %q", checksum, expectedChecksum)
	}
}

func TestIntegration_BackupListRemote(t *testing.T) {
	ctx := context.Background()
	rs := NewRemoteStorage(testS3Client, "listtest")

	projectRoot := t.TempDir()
	backupDir := filepath.Join(projectRoot, "sites", "testsite", "backups")

	// Upload 3 fake backup files with different timestamps.
	timestamps := []struct {
		filename  string
		id        string
		createdAt time.Time
	}{
		{"bk_testsite_20260406_100000.sql.gz", "bk_testsite_20260406_100000", time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC)},
		{"bk_testsite_20260407_120000.sql.gz", "bk_testsite_20260407_120000", time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)},
		{"bk_testsite_20260408_080000.sql.gz", "bk_testsite_20260408_080000", time.Date(2026, 4, 8, 8, 0, 0, 0, time.UTC)},
	}

	var remoteKeys []string
	for i, ts := range timestamps {
		content := []byte(fmt.Sprintf("backup content %d", i))
		filePath, _ := createFakeBackupFile(t, backupDir, ts.filename, content)

		info := BackupInfo{
			ID:         ts.id,
			Site:       "testsite",
			Type:       "full",
			Path:       filePath,
			Size:       int64(len(content)),
			CreatedAt:  ts.createdAt,
			Compressed: true,
		}

		key, err := rs.Upload(ctx, info)
		if err != nil {
			t.Fatalf("Upload(%s): %v", ts.filename, err)
		}
		remoteKeys = append(remoteKeys, key)
	}

	// Clean up remote keys after the test.
	t.Cleanup(func() {
		for _, key := range remoteKeys {
			_ = rs.DeleteRemote(context.Background(), key)
		}
	})

	// List remote backups.
	backups, err := rs.ListRemote(ctx, "testsite")
	if err != nil {
		t.Fatalf("ListRemote: %v", err)
	}

	// Verify count is 3.
	if len(backups) != 3 {
		t.Fatalf("expected 3 backups, got %d", len(backups))
	}

	// Verify sorted newest-first.
	if backups[0].ID != "bk_testsite_20260408_080000" {
		t.Errorf("expected newest first, got %s", backups[0].ID)
	}
	if backups[1].ID != "bk_testsite_20260407_120000" {
		t.Errorf("expected second newest, got %s", backups[1].ID)
	}
	if backups[2].ID != "bk_testsite_20260406_100000" {
		t.Errorf("expected oldest last, got %s", backups[2].ID)
	}

	// Verify metadata.
	for _, b := range backups {
		if b.Site != "testsite" {
			t.Errorf("expected site %q, got %q", "testsite", b.Site)
		}
		if !b.Compressed {
			t.Errorf("expected compressed=true for %s", b.ID)
		}
		if b.RemoteKey == "" {
			t.Errorf("expected non-empty RemoteKey for %s", b.ID)
		}
	}
}

func TestIntegration_BackupPruneWithRemote(t *testing.T) {
	ctx := context.Background()
	rs := NewRemoteStorage(testS3Client, "prunetest")

	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	site := "prunesite"

	projectRoot := t.TempDir()
	backupDir := filepath.Join(projectRoot, "sites", site, "backups")

	// Create local backup files spanning different age buckets.
	// All older than 24h safety window.
	type backupEntry struct {
		filename string
		age      time.Duration
	}
	entries := []backupEntry{
		// Daily bucket (1-6 days old): 2 backups, keep 1
		{"bk_prunesite_20260406_120000.sql.gz", 2 * 24 * time.Hour},
		{"bk_prunesite_20260404_120000.sql.gz", 4 * 24 * time.Hour},
		// Weekly bucket (8-25 days old): 3 backups, keep 1
		{"bk_prunesite_20260331_120000.sql.gz", 8 * 24 * time.Hour},
		{"bk_prunesite_20260322_120000.sql.gz", 17 * 24 * time.Hour},
		{"bk_prunesite_20260314_120000.sql.gz", 25 * 24 * time.Hour},
		// Monthly bucket (35-60 days old): 2 backups, keep 1
		{"bk_prunesite_20260304_120000.sql.gz", 35 * 24 * time.Hour},
		{"bk_prunesite_20260207_120000.sql.gz", 60 * 24 * time.Hour},
	}

	var remoteKeys []string
	for i, e := range entries {
		content := []byte(fmt.Sprintf("prune-backup-%d", i))
		filePath, _ := createFakeBackupFile(t, backupDir, e.filename, content)

		ts := now.Add(-e.age)
		info := BackupInfo{
			ID:         e.filename[:len(e.filename)-len(".sql.gz")],
			Site:       site,
			Type:       "full",
			Path:       filePath,
			Size:       int64(len(content)),
			CreatedAt:  ts,
			Compressed: true,
		}

		key, err := rs.Upload(ctx, info)
		if err != nil {
			t.Fatalf("Upload(%s): %v", e.filename, err)
		}
		remoteKeys = append(remoteKeys, key)
	}

	// Clean up any remaining remote keys after the test.
	t.Cleanup(func() {
		for _, key := range remoteKeys {
			_ = rs.DeleteRemote(context.Background(), key)
		}
	})

	// Call Prune with Retention{Daily: 1, Weekly: 1, Monthly: 1}, DryRun=false.
	result, err := Prune(ctx, PruneOptions{
		Remote:      rs,
		Now:         now,
		Site:        site,
		ProjectRoot: projectRoot,
		Retention:   config.RetentionConfig{Daily: 1, Weekly: 1, Monthly: 1},
		DryRun:      false,
	})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	// Expect 4 deleted: 1 daily + 2 weekly + 1 monthly.
	if len(result.Deleted) != 4 {
		t.Errorf("deleted: got %d, want 4", len(result.Deleted))
		for _, d := range result.Deleted {
			t.Logf("  deleted: %s (age: %s)", d.ID, now.Sub(d.CreatedAt))
		}
	}

	// Expect 3 kept: 1 daily + 1 weekly + 1 monthly.
	if len(result.Kept) != 3 {
		t.Errorf("kept: got %d, want 3", len(result.Kept))
		for _, k := range result.Kept {
			t.Logf("  kept: %s (age: %s)", k.ID, now.Sub(k.CreatedAt))
		}
	}

	if len(result.Errors) != 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}

	// Verify local files were actually deleted.
	for _, d := range result.Deleted {
		if d.Path != "" {
			if _, err := os.Stat(d.Path); !os.IsNotExist(err) {
				t.Errorf("local file %s should have been deleted", d.Path)
			}
		}
	}

	// Verify remote files actually deleted by checking with ListRemote.
	remaining, err := rs.ListRemote(ctx, site)
	if err != nil {
		t.Fatalf("ListRemote after prune: %v", err)
	}
	if len(remaining) != 3 {
		t.Errorf("remaining remote backups: got %d, want 3", len(remaining))
		for _, r := range remaining {
			t.Logf("  remaining: %s", r.ID)
		}
	}
}

func TestIntegration_BackupPruneDryRun(t *testing.T) {
	ctx := context.Background()
	rs := NewRemoteStorage(testS3Client, "dryruntest")

	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	site := "dryrunsite"

	projectRoot := t.TempDir()
	backupDir := filepath.Join(projectRoot, "sites", site, "backups")

	// Same age structure as prune test.
	type backupEntry struct {
		filename string
		age      time.Duration
	}
	entries := []backupEntry{
		// Daily bucket: 2 backups
		{"bk_dryrunsite_20260406_120000.sql.gz", 2 * 24 * time.Hour},
		{"bk_dryrunsite_20260404_120000.sql.gz", 4 * 24 * time.Hour},
		// Weekly bucket: 3 backups
		{"bk_dryrunsite_20260331_120000.sql.gz", 8 * 24 * time.Hour},
		{"bk_dryrunsite_20260322_120000.sql.gz", 17 * 24 * time.Hour},
		{"bk_dryrunsite_20260314_120000.sql.gz", 25 * 24 * time.Hour},
		// Monthly bucket: 2 backups
		{"bk_dryrunsite_20260304_120000.sql.gz", 35 * 24 * time.Hour},
		{"bk_dryrunsite_20260207_120000.sql.gz", 60 * 24 * time.Hour},
	}

	var remoteKeys []string
	var localPaths []string
	for i, e := range entries {
		content := []byte(fmt.Sprintf("dryrun-backup-%d", i))
		filePath, _ := createFakeBackupFile(t, backupDir, e.filename, content)
		localPaths = append(localPaths, filePath)

		ts := now.Add(-e.age)
		info := BackupInfo{
			ID:         e.filename[:len(e.filename)-len(".sql.gz")],
			Site:       site,
			Type:       "full",
			Path:       filePath,
			Size:       int64(len(content)),
			CreatedAt:  ts,
			Compressed: true,
		}

		key, err := rs.Upload(ctx, info)
		if err != nil {
			t.Fatalf("Upload(%s): %v", e.filename, err)
		}
		remoteKeys = append(remoteKeys, key)
	}

	// Clean up remote keys after the test.
	t.Cleanup(func() {
		for _, key := range remoteKeys {
			_ = rs.DeleteRemote(context.Background(), key)
		}
	})

	// Call Prune with DryRun=true.
	result, err := Prune(ctx, PruneOptions{
		Remote:      rs,
		Now:         now,
		Site:        site,
		ProjectRoot: projectRoot,
		Retention:   config.RetentionConfig{Daily: 1, Weekly: 1, Monthly: 1},
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	// Verify result.DryRun is true.
	if !result.DryRun {
		t.Error("expected DryRun=true in result")
	}

	// Should still identify 4 candidates for deletion.
	if len(result.Deleted) != 4 {
		t.Errorf("deleted candidates: got %d, want 4", len(result.Deleted))
	}

	// Verify all local files still exist.
	for _, p := range localPaths {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			t.Errorf("local file %s should still exist in dry-run mode", filepath.Base(p))
		}
	}

	// Verify all remote files still exist.
	remaining, err := rs.ListRemote(ctx, site)
	if err != nil {
		t.Fatalf("ListRemote after dry-run prune: %v", err)
	}
	if len(remaining) != 7 {
		t.Errorf("remote backups after dry-run: got %d, want 7", len(remaining))
	}
}

func TestIntegration_BackupScheduleInstallShow(t *testing.T) {
	ctx := context.Background()

	// Override readCrontab and writeCrontab with mock functions.
	var mockContent string
	oldRead := readCrontab
	oldWrite := writeCrontab
	readCrontab = func(_ context.Context) (string, error) { return mockContent, nil }
	writeCrontab = func(_ context.Context, content string) error {
		mockContent = content
		return nil
	}
	defer func() {
		readCrontab = oldRead
		writeCrontab = oldWrite
	}()

	projectName := "integ-test-project"
	projectRoot := "/srv/integ-test-project"

	// Install a cron schedule.
	if err := InstallCronSchedule(ctx, "0 2 * * *", projectName, projectRoot); err != nil {
		t.Fatalf("InstallCronSchedule: %v", err)
	}

	// Show schedule and verify.
	info, err := ShowSchedule(ctx, projectName)
	if err != nil {
		t.Fatalf("ShowSchedule after install: %v", err)
	}
	if !info.Installed {
		t.Error("expected Installed=true after install")
	}
	if !info.Enabled {
		t.Error("expected Enabled=true after install")
	}
	if info.CronExpr != "0 2 * * *" {
		t.Errorf("CronExpr = %q, want %q", info.CronExpr, "0 2 * * *")
	}
	if info.ProjectName != projectName {
		t.Errorf("ProjectName = %q, want %q", info.ProjectName, projectName)
	}
	if info.ProjectRoot != projectRoot {
		t.Errorf("ProjectRoot = %q, want %q", info.ProjectRoot, projectRoot)
	}

	// Remove the schedule.
	if err := RemoveCronSchedule(ctx, projectName); err != nil {
		t.Fatalf("RemoveCronSchedule: %v", err)
	}

	// Show schedule and verify removed.
	info, err = ShowSchedule(ctx, projectName)
	if err != nil {
		t.Fatalf("ShowSchedule after remove: %v", err)
	}
	if info.Installed {
		t.Error("expected Installed=false after remove")
	}
}

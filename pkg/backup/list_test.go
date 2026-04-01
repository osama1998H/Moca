package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestList(t *testing.T) {
	// Create a temp project structure with backup files.
	projectRoot := t.TempDir()
	site := "test.localhost"
	backupDir := filepath.Join(projectRoot, "sites", site, "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create test backup files with known names.
	files := []struct {
		name string
		size int
	}{
		{"bk_test_localhost_20260401_100000.sql.gz", 1024},
		{"bk_test_localhost_20260402_120000.sql.gz", 2048},
		{"bk_test_localhost_20260403_080000.sql", 4096},
		{"not_a_backup.txt", 512},
		{"random.sql.gz", 256},
	}

	for _, f := range files {
		data := make([]byte, f.size)
		if err := os.WriteFile(filepath.Join(backupDir, f.name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	backups, err := List(context.Background(), site, projectRoot)
	if err != nil {
		t.Fatal(err)
	}

	// Should find 3 backup files (not the .txt or random.sql.gz).
	if len(backups) != 3 {
		t.Fatalf("expected 3 backups, got %d", len(backups))
	}

	// Should be sorted newest first.
	if backups[0].ID != "bk_test_localhost_20260403_080000" {
		t.Errorf("expected newest backup first, got %s", backups[0].ID)
	}
	if backups[1].ID != "bk_test_localhost_20260402_120000" {
		t.Errorf("expected second newest, got %s", backups[1].ID)
	}
	if backups[2].ID != "bk_test_localhost_20260401_100000" {
		t.Errorf("expected oldest last, got %s", backups[2].ID)
	}

	// Verify metadata.
	if backups[0].Site != site {
		t.Errorf("expected site %q, got %q", site, backups[0].Site)
	}
	if backups[0].Compressed {
		t.Error("expected uncompressed for .sql file")
	}
	if !backups[1].Compressed {
		t.Error("expected compressed for .sql.gz file")
	}
	if backups[0].Size != 4096 {
		t.Errorf("expected size 4096, got %d", backups[0].Size)
	}
}

func TestListEmptyDirectory(t *testing.T) {
	projectRoot := t.TempDir()
	site := "empty.localhost"

	// No backup directory exists.
	backups, err := List(context.Background(), site, projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("expected 0 backups, got %d", len(backups))
	}
}

func TestParseTimestampFromID(t *testing.T) {
	tests := []struct {
		expected time.Time
		id       string
	}{
		{
			time.Date(2026, 4, 2, 14, 30, 22, 0, time.UTC),
			"bk_acme_20260402_143022",
		},
		{
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			"bk_test_localhost_20260101_000000",
		},
		{
			time.Time{},
			"invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got := parseTimestampFromID(tt.id)
			if !got.Equal(tt.expected) {
				t.Errorf("parseTimestampFromID(%q) = %v, want %v", tt.id, got, tt.expected)
			}
		})
	}
}

package backup

import (
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func createGzipFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	_, err = gz.Write([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if err = gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVerifyValidGzip(t *testing.T) {
	dir := t.TempDir()
	sqlContent := "SET search_path = tenant_test;\nCREATE TABLE tab_user (name text);\nINSERT INTO tab_user VALUES ('admin');\n"
	path := createGzipFile(t, dir, "bk_test_20260402_120000.sql.gz", sqlContent)

	result, err := Verify(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}

	if !result.Valid {
		t.Errorf("expected valid, got error: %s", result.Error)
	}
	if result.Checksum == "" {
		t.Error("expected non-empty checksum")
	}
	if result.BackupID != "bk_test_20260402_120000" {
		t.Errorf("backup ID = %q, want %q", result.BackupID, "bk_test_20260402_120000")
	}
}

func TestVerifyDeep(t *testing.T) {
	dir := t.TempDir()
	sqlContent := "CREATE TABLE tab_user (name text);\nCREATE TABLE tab_role (name text);\nCREATE INDEX idx_user_name ON tab_user (name);\nINSERT INTO tab_user VALUES ('admin');\nALTER TABLE tab_user ADD COLUMN email text;\n"
	path := createGzipFile(t, dir, "bk_test_20260402_120000.sql.gz", sqlContent)

	result, err := Verify(context.Background(), path, true)
	if err != nil {
		t.Fatal(err)
	}

	if !result.Valid {
		t.Errorf("expected valid, got error: %s", result.Error)
	}
	// 2 CREATE TABLE + 1 CREATE INDEX + 1 INSERT INTO + 1 ALTER TABLE = 5
	if result.ObjectCount != 5 {
		t.Errorf("object count = %d, want 5", result.ObjectCount)
	}
}

func TestVerifyMissingFile(t *testing.T) {
	result, err := Verify(context.Background(), "/nonexistent/file.sql.gz", false)
	if err != nil {
		t.Fatal(err)
	}

	if result.Valid {
		t.Error("expected invalid for missing file")
	}
	if result.Error == "" {
		t.Error("expected error message for missing file")
	}
}

func TestVerifyCorruptGzip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.sql.gz")

	// Write non-gzip data with .gz extension.
	if err := os.WriteFile(path, []byte("not gzip data"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Verify(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}

	if result.Valid {
		t.Error("expected invalid for corrupt gzip")
	}
	if result.Error == "" {
		t.Error("expected error message for corrupt gzip")
	}
}

func TestVerifyPlainSQL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bk_test_20260402_120000.sql")

	sqlContent := "CREATE TABLE tab_user (name text);\n"
	if err := os.WriteFile(path, []byte(sqlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Verify(context.Background(), path, true)
	if err != nil {
		t.Fatal(err)
	}

	if !result.Valid {
		t.Errorf("expected valid, got error: %s", result.Error)
	}
	if result.ObjectCount != 1 {
		t.Errorf("object count = %d, want 1", result.ObjectCount)
	}
}

func TestBackupIDFromPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/path/to/bk_acme_20260402_120000.sql.gz", "bk_acme_20260402_120000"},
		{"/path/to/bk_acme_20260402_120000.sql", "bk_acme_20260402_120000"},
		{"backup.sql.gz", "backup"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := backupIDFromPath(tt.path)
			if got != tt.expected {
				t.Errorf("backupIDFromPath(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

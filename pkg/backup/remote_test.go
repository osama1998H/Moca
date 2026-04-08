package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/osama1998H/moca/pkg/storage"
)

// mockRemoteClient implements RemoteClient with an in-memory map.
type mockRemoteClient struct {
	objects map[string][]byte // key -> content
	mu      sync.Mutex
}

func newMockRemoteClient() *mockRemoteClient {
	return &mockRemoteClient{objects: make(map[string][]byte)}
}

func (m *mockRemoteClient) Upload(_ context.Context, key string, reader io.Reader, _ int64, _ string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = data
	return nil
}

func (m *mockRemoteClient) Download(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, io.EOF
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *mockRemoteClient) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *mockRemoteClient) Exists(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[key]
	return ok, nil
}

func (m *mockRemoteClient) ListObjects(_ context.Context, prefix string, _ bool) ([]storage.ObjectInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var objects []storage.ObjectInfo
	for k, v := range m.objects {
		if strings.HasPrefix(k, prefix) {
			objects = append(objects, storage.ObjectInfo{
				Key:  k,
				Size: int64(len(v)),
			})
		}
	}
	return objects, nil
}

func (m *mockRemoteClient) EnsureBucket(_ context.Context) error {
	return nil
}

func TestRemoteKey(t *testing.T) {
	rs := NewRemoteStorage(newMockRemoteClient(), "project1")

	tests := []struct {
		site     string
		filename string
		expected string
	}{
		{"acme.localhost", "bk_acme_20260402_120000.sql.gz", "project1/backups/acme.localhost/bk_acme_20260402_120000.sql.gz"},
		{"test.localhost", "bk_test_20260401_100000.sql", "project1/backups/test.localhost/bk_test_20260401_100000.sql"},
		{"site1", "bk_site1_20260101_000000.sql.gz", "project1/backups/site1/bk_site1_20260101_000000.sql.gz"},
	}

	for _, tt := range tests {
		t.Run(tt.site+"/"+tt.filename, func(t *testing.T) {
			got := rs.remoteKey(tt.site, tt.filename)
			if got != tt.expected {
				t.Errorf("remoteKey(%q, %q) = %q, want %q", tt.site, tt.filename, got, tt.expected)
			}
		})
	}
}

func TestRemoteStorageUpload(t *testing.T) {
	mock := newMockRemoteClient()
	rs := NewRemoteStorage(mock, "proj")

	// Create a temp backup file with known content.
	dir := t.TempDir()
	content := []byte("CREATE TABLE tab_user (name text);")
	filePath := filepath.Join(dir, "bk_acme_20260402_120000.sql.gz")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	info := BackupInfo{
		ID:   "bk_acme_20260402_120000",
		Site: "acme.localhost",
		Path: filePath,
	}

	key, err := rs.Upload(context.Background(), info)
	if err != nil {
		t.Fatal(err)
	}

	expectedKey := "proj/backups/acme.localhost/bk_acme_20260402_120000.sql.gz"
	if key != expectedKey {
		t.Errorf("key = %q, want %q", key, expectedKey)
	}

	// Verify mock received the content.
	mock.mu.Lock()
	data, ok := mock.objects[expectedKey]
	mock.mu.Unlock()
	if !ok {
		t.Fatal("expected object in mock storage")
	}
	if !bytes.Equal(data, content) {
		t.Errorf("uploaded content mismatch: got %d bytes, want %d bytes", len(data), len(content))
	}
}

func TestRemoteStorageDownload(t *testing.T) {
	mock := newMockRemoteClient()
	rs := NewRemoteStorage(mock, "proj")

	// Seed mock with known content.
	content := []byte("SET search_path = tenant_acme;\nCREATE TABLE tab_user (name text);")
	remoteKey := "proj/backups/acme.localhost/bk_acme_20260402_120000.sql"
	mock.mu.Lock()
	mock.objects[remoteKey] = content
	mock.mu.Unlock()

	outputDir := t.TempDir()
	localPath, checksum, err := rs.Download(context.Background(), remoteKey, outputDir)
	if err != nil {
		t.Fatal(err)
	}

	// Verify file was written to disk.
	expectedPath := filepath.Join(outputDir, "bk_acme_20260402_120000.sql")
	if localPath != expectedPath {
		t.Errorf("localPath = %q, want %q", localPath, expectedPath)
	}

	downloaded, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(downloaded, content) {
		t.Errorf("downloaded content mismatch: got %d bytes, want %d bytes", len(downloaded), len(content))
	}

	// Verify checksum matches SHA-256 of the content.
	h := sha256.Sum256(content)
	expectedChecksum := hex.EncodeToString(h[:])
	if checksum != expectedChecksum {
		t.Errorf("checksum = %q, want %q", checksum, expectedChecksum)
	}
}

func TestRemoteStorageListRemote(t *testing.T) {
	mock := newMockRemoteClient()
	rs := NewRemoteStorage(mock, "proj")

	// Seed mock with backup-like keys.
	mock.mu.Lock()
	mock.objects["proj/backups/acme.localhost/bk_acme_20260401_100000.sql.gz"] = make([]byte, 1024)
	mock.objects["proj/backups/acme.localhost/bk_acme_20260403_080000.sql"] = make([]byte, 4096)
	mock.objects["proj/backups/acme.localhost/bk_acme_20260402_120000.sql.gz"] = make([]byte, 2048)
	mock.objects["proj/backups/acme.localhost/not_a_backup.txt"] = make([]byte, 100)
	mock.objects["proj/backups/other.localhost/bk_other_20260401_100000.sql"] = make([]byte, 512)
	mock.mu.Unlock()

	backups, err := rs.ListRemote(context.Background(), "acme.localhost")
	if err != nil {
		t.Fatal(err)
	}

	// Should find 3 backup files (not the .txt, not the other site).
	if len(backups) != 3 {
		t.Fatalf("expected 3 backups, got %d", len(backups))
	}

	// Should be sorted newest first.
	if backups[0].ID != "bk_acme_20260403_080000" {
		t.Errorf("expected newest first, got %s", backups[0].ID)
	}
	if backups[1].ID != "bk_acme_20260402_120000" {
		t.Errorf("expected second newest, got %s", backups[1].ID)
	}
	if backups[2].ID != "bk_acme_20260401_100000" {
		t.Errorf("expected oldest last, got %s", backups[2].ID)
	}

	// Verify metadata.
	if backups[0].Site != "acme.localhost" {
		t.Errorf("expected site %q, got %q", "acme.localhost", backups[0].Site)
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

	// Verify RemoteKey is set.
	expectedKey := "proj/backups/acme.localhost/bk_acme_20260403_080000.sql"
	if backups[0].RemoteKey != expectedKey {
		t.Errorf("RemoteKey = %q, want %q", backups[0].RemoteKey, expectedKey)
	}
}

func TestRemoteStorageDeleteRemote(t *testing.T) {
	mock := newMockRemoteClient()
	rs := NewRemoteStorage(mock, "proj")

	key := "proj/backups/acme.localhost/bk_acme_20260402_120000.sql.gz"
	mock.mu.Lock()
	mock.objects[key] = []byte("data")
	mock.mu.Unlock()

	if err := rs.DeleteRemote(context.Background(), key); err != nil {
		t.Fatal(err)
	}

	// Verify object was removed.
	mock.mu.Lock()
	_, exists := mock.objects[key]
	mock.mu.Unlock()
	if exists {
		t.Error("expected object to be deleted from mock")
	}
}

func TestRemoteStorageUploadMissingFile(t *testing.T) {
	mock := newMockRemoteClient()
	rs := NewRemoteStorage(mock, "proj")

	info := BackupInfo{
		ID:   "bk_missing_20260402_120000",
		Site: "acme.localhost",
		Path: "/nonexistent/bk_missing_20260402_120000.sql.gz",
	}

	_, err := rs.Upload(context.Background(), info)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "backup/remote: upload") {
		t.Errorf("expected error prefix 'backup/remote: upload', got: %s", err.Error())
	}
}

func TestRemoteStorageListRemoteEmpty(t *testing.T) {
	mock := newMockRemoteClient()
	rs := NewRemoteStorage(mock, "proj")

	backups, err := rs.ListRemote(context.Background(), "empty.localhost")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("expected 0 backups, got %d", len(backups))
	}
}

func TestRemoteStorageUploadContentType(t *testing.T) {
	// Verify that .gz files get "application/gzip" and .sql get "application/sql".
	// We test this by checking that upload succeeds for both extensions
	// (the mock doesn't validate content type, but the code path is exercised).
	mock := newMockRemoteClient()
	rs := NewRemoteStorage(mock, "proj")
	dir := t.TempDir()

	// Test .sql.gz file.
	gzPath := filepath.Join(dir, "bk_acme_20260402_120000.sql.gz")
	if err := os.WriteFile(gzPath, []byte("gzip content"), 0o644); err != nil {
		t.Fatal(err)
	}
	key, err := rs.Upload(context.Background(), BackupInfo{
		ID: "bk_acme_20260402_120000", Site: "acme.localhost", Path: gzPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(key, ".sql.gz") {
		t.Errorf("expected key ending in .sql.gz, got %q", key)
	}

	// Test plain .sql file.
	sqlPath := filepath.Join(dir, "bk_acme_20260402_120001.sql")
	if writeErr := os.WriteFile(sqlPath, []byte("sql content"), 0o644); writeErr != nil {
		t.Fatal(writeErr)
	}
	key, err = rs.Upload(context.Background(), BackupInfo{
		ID: "bk_acme_20260402_120001", Site: "acme.localhost", Path: sqlPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(key, ".sql") {
		t.Errorf("expected key ending in .sql, got %q", key)
	}
}

func TestRemoteKeyFormat(t *testing.T) {
	// Verify path.Join normalizes correctly (no double slashes, etc.).
	rs := NewRemoteStorage(newMockRemoteClient(), "my-project")

	key := rs.remoteKey("site1.localhost", "bk_site1_20260101_000000.sql")
	parts := strings.Split(key, "/")
	if len(parts) != 4 {
		t.Errorf("expected 4 path segments, got %d: %q", len(parts), key)
	}
	if parts[0] != "my-project" {
		t.Errorf("expected prefix 'my-project', got %q", parts[0])
	}
	if parts[1] != "backups" {
		t.Errorf("expected 'backups' segment, got %q", parts[1])
	}

	// Verify path.Base extracts filename correctly for Download.
	filename := path.Base(key)
	if filename != "bk_site1_20260101_000000.sql" {
		t.Errorf("path.Base(%q) = %q, want filename", key, filename)
	}
}

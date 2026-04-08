package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/osama1998H/moca/pkg/storage"
)

// RemoteClient is a narrow facade over S3-compatible storage for testability.
type RemoteClient interface {
	Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	ListObjects(ctx context.Context, prefix string, recursive bool) ([]storage.ObjectInfo, error)
	EnsureBucket(ctx context.Context) error
}

// RemoteStorage wraps an S3-compatible client for backup operations.
type RemoteStorage struct {
	client RemoteClient
	prefix string // key prefix from BackupDestination.Prefix
}

// NewRemoteStorage creates a RemoteStorage with the given client and key prefix.
func NewRemoteStorage(client RemoteClient, prefix string) *RemoteStorage {
	return &RemoteStorage{client: client, prefix: prefix}
}

// remoteKey builds "{prefix}/backups/{site}/{filename}".
func (r *RemoteStorage) remoteKey(site, filename string) string {
	return path.Join(r.prefix, "backups", site, filename)
}

// Upload opens the local backup file at info.Path, uploads it to S3, and
// returns the remote key. Content type is "application/gzip" for .gz files
// and "application/sql" otherwise.
func (r *RemoteStorage) Upload(ctx context.Context, info BackupInfo) (string, error) {
	f, err := os.Open(info.Path)
	if err != nil {
		return "", fmt.Errorf("backup/remote: upload %s: %w", info.ID, err)
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("backup/remote: upload %s: %w", info.ID, err)
	}

	filename := filepath.Base(info.Path)
	key := r.remoteKey(info.Site, filename)

	contentType := "application/sql"
	if strings.HasSuffix(filename, ".gz") {
		contentType = "application/gzip"
	}

	if err := r.client.Upload(ctx, key, f, stat.Size(), contentType); err != nil {
		return "", fmt.Errorf("backup/remote: upload %s: %w", info.ID, err)
	}

	return key, nil
}

// Download fetches a backup from S3 and writes it to outputDir. It computes
// the SHA-256 checksum of the downloaded file. Returns the local path and
// checksum.
func (r *RemoteStorage) Download(ctx context.Context, remoteKey, outputDir string) (string, string, error) {
	reader, err := r.client.Download(ctx, remoteKey)
	if err != nil {
		return "", "", fmt.Errorf("backup/remote: download %s: %w", remoteKey, err)
	}
	defer func() { _ = reader.Close() }()

	filename := path.Base(remoteKey)
	localPath := filepath.Join(outputDir, filename)

	f, err := os.Create(localPath)
	if err != nil {
		return "", "", fmt.Errorf("backup/remote: download %s: %w", remoteKey, err)
	}

	h := sha256.New()
	w := io.MultiWriter(f, h)

	if _, err := io.Copy(w, reader); err != nil {
		_ = f.Close()
		return "", "", fmt.Errorf("backup/remote: download %s: %w", remoteKey, err)
	}

	if err := f.Close(); err != nil {
		return "", "", fmt.Errorf("backup/remote: download %s: %w", remoteKey, err)
	}

	checksum := hex.EncodeToString(h.Sum(nil))
	return localPath, checksum, nil
}

// ListRemote lists all backup files at the site prefix. Keys are parsed using
// the backup filename pattern. Results are sorted newest-first.
func (r *RemoteStorage) ListRemote(ctx context.Context, site string) ([]RemoteBackupInfo, error) {
	prefix := r.prefix + "/backups/" + site + "/"

	objects, err := r.client.ListObjects(ctx, prefix, true)
	if err != nil {
		return nil, fmt.Errorf("backup/remote: list %s: %w", site, err)
	}

	var backups []RemoteBackupInfo
	for _, obj := range objects {
		filename := path.Base(obj.Key)
		matches := backupFilePattern.FindStringSubmatch(filename)
		if matches == nil {
			continue
		}

		backupID := matches[1]
		compressed := matches[2] == ".gz"
		createdAt := parseTimestampFromID(backupID)

		backups = append(backups, RemoteBackupInfo{
			BackupInfo: BackupInfo{
				ID:         backupID,
				Site:       site,
				Type:       "full",
				Size:       obj.Size,
				CreatedAt:  createdAt,
				Compressed: compressed,
			},
			RemoteKey: obj.Key,
		})
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	return backups, nil
}

// DeleteRemote deletes a backup from remote storage.
func (r *RemoteStorage) DeleteRemote(ctx context.Context, remoteKey string) error {
	if err := r.client.Delete(ctx, remoteKey); err != nil {
		return fmt.Errorf("backup/remote: delete %s: %w", remoteKey, err)
	}
	return nil
}

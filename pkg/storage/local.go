package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/osama1998H/moca/internal/config"
)

// LocalStorage implements Storage using the local filesystem.
// Intended for development mode only.
type LocalStorage struct {
	baseDir string
}

// NewLocalStorage creates a LocalStorage. If cfg.Endpoint is set, it is used as
// the base directory; otherwise "sites" in the current directory is used.
func NewLocalStorage(cfg config.StorageConfig) (*LocalStorage, error) {
	dir := cfg.Endpoint
	if dir == "" {
		dir = "sites"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("storage/local: create base dir %q: %w", dir, err)
	}
	return &LocalStorage{baseDir: dir}, nil
}

func (l *LocalStorage) Upload(_ context.Context, key string, reader io.Reader, _ int64, _ string) error {
	fullPath := filepath.Join(l.baseDir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return fmt.Errorf("storage/local: mkdir for %q: %w", key, err)
	}
	f, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("storage/local: create %q: %w", key, err)
	}
	defer f.Close()
	if _, err := io.Copy(f, reader); err != nil {
		return fmt.Errorf("storage/local: write %q: %w", key, err)
	}
	return nil
}

func (l *LocalStorage) Download(_ context.Context, key string) (io.ReadCloser, error) {
	fullPath := filepath.Join(l.baseDir, filepath.FromSlash(key))
	f, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("storage/local: open %q: %w", key, err)
	}
	return f, nil
}

func (l *LocalStorage) Delete(_ context.Context, key string) error {
	fullPath := filepath.Join(l.baseDir, filepath.FromSlash(key))
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage/local: delete %q: %w", key, err)
	}
	return nil
}

// PresignedGetURL is not supported by LocalStorage; it returns the file path.
func (l *LocalStorage) PresignedGetURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return filepath.Join(l.baseDir, filepath.FromSlash(key)), nil
}

// PresignedPutURL is not supported by LocalStorage; it returns the file path.
func (l *LocalStorage) PresignedPutURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return filepath.Join(l.baseDir, filepath.FromSlash(key)), nil
}

func (l *LocalStorage) Exists(_ context.Context, key string) (bool, error) {
	fullPath := filepath.Join(l.baseDir, filepath.FromSlash(key))
	_, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("storage/local: stat %q: %w", key, err)
	}
	return true, nil
}

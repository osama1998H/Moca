package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/osama1998H/moca/internal/config"
)

// Storage abstracts object storage backends (S3, local filesystem).
// All operations use site-scoped object keys to enforce tenant isolation.
type Storage interface {
	// Upload stores the content from reader under the given key.
	Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error

	// Download retrieves the object at key and returns a ReadCloser.
	// The caller must close the returned reader.
	Download(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete removes the object at key. It is not an error if the key does not exist.
	Delete(ctx context.Context, key string) error

	// PresignedGetURL returns a time-limited URL for downloading the object.
	PresignedGetURL(ctx context.Context, key string, expiry time.Duration) (string, error)

	// PresignedPutURL returns a time-limited URL for uploading an object.
	PresignedPutURL(ctx context.Context, key string, expiry time.Duration) (string, error)

	// Exists returns true if an object exists at the given key.
	Exists(ctx context.Context, key string) (bool, error)
}

// FileMeta holds metadata about an uploaded file, matching the tab_file schema.
type FileMeta struct {
	Name              string    `json:"name"`
	FileName          string    `json:"file_name"`
	FileURL           string    `json:"file_url"`
	FileSize          int64     `json:"file_size"`
	ContentType       string    `json:"content_type"`
	AttachedToDocType string    `json:"attached_to_doctype,omitempty"`
	AttachedToName    string    `json:"attached_to_name,omitempty"`
	IsPrivate         bool      `json:"is_private"`
	Owner             string    `json:"owner"`
	Creation          time.Time `json:"creation"`
	ThumbnailURL      string    `json:"thumbnail_url,omitempty"`
}

// AttachmentRef identifies the document a file is attached to.
type AttachmentRef struct {
	DocType string
	DocName string
}

// NewStorage creates a Storage backend based on the driver in the config.
// Supported drivers: "s3" (S3/MinIO) and "local" (filesystem).
func NewStorage(cfg config.StorageConfig) (Storage, error) {
	switch cfg.Driver {
	case "s3":
		return NewS3Storage(cfg)
	case "local", "":
		return NewLocalStorage(cfg)
	default:
		return nil, fmt.Errorf("storage: unsupported driver %q (expected \"s3\" or \"local\")", cfg.Driver)
	}
}

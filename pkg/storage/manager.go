package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/osama1998H/moca/pkg/orm"
)

// Default and limit constants for file uploads.
const (
	// DefaultMaxUpload is the default maximum upload size (25 MiB).
	DefaultMaxUpload int64 = 25 << 20
)

// allowedContentTypes is the set of MIME types permitted for upload.
var allowedContentTypes = map[string]bool{
	"image/jpeg":    true,
	"image/png":     true,
	"image/gif":     true,
	"image/webp":    true,
	"image/svg+xml": true,

	"application/pdf": true,

	"text/plain": true,
	"text/csv":   true,

	"application/json": true,

	// Microsoft Office / OpenXML
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":    true,
	"application/vnd.openxmlformats-officedocument.presentationml.presentation":  true,
	"application/vnd.ms-excel": true,
	"application/msword":       true,

	"video/mp4":  true,
	"video/webm": true,

	"audio/mpeg": true,
	"audio/wav":  true,

	"application/zip": true,

	// Catch-all for generic binary streams (e.g. when browser doesn't detect type).
	"application/octet-stream": true,
}

// FileUploadHeader carries metadata about the file being uploaded.
type FileUploadHeader struct {
	FileName    string
	ContentType string
	Size        int64
}

// UploadOptions holds optional parameters for file upload.
type UploadOptions struct {
	AttachedToDocType string
	AttachedToName    string
	IsPrivate         bool
}

// FileNotFoundError is returned when a file record does not exist in tab_file.
type FileNotFoundError struct {
	Name string
}

func (e *FileNotFoundError) Error() string {
	return fmt.Sprintf("file %q not found", e.Name)
}

// FileTooLargeError is returned when the upload exceeds the size limit.
type FileTooLargeError struct {
	Size int64
	Max  int64
}

func (e *FileTooLargeError) Error() string {
	return fmt.Sprintf("file size %d exceeds maximum %d bytes", e.Size, e.Max)
}

// InvalidContentTypeError is returned when the file's MIME type is not in the allowlist.
type InvalidContentTypeError struct {
	ContentType string
}

func (e *InvalidContentTypeError) Error() string {
	return fmt.Sprintf("content type %q is not allowed", e.ContentType)
}

// FileManager orchestrates the file lifecycle: upload, download, delete, and
// signed URL generation. It coordinates the Storage backend with the tab_file
// database table.
type FileManager struct {
	storage   Storage
	db        *orm.DBManager
	logger    *slog.Logger
	maxUpload int64
}

// NewFileManager creates a FileManager. If maxUpload is 0, DefaultMaxUpload is used.
func NewFileManager(storage Storage, db *orm.DBManager, logger *slog.Logger, maxUpload int64) *FileManager {
	if maxUpload <= 0 {
		maxUpload = DefaultMaxUpload
	}
	return &FileManager{
		storage:   storage,
		db:        db,
		logger:    logger,
		maxUpload: maxUpload,
	}
}

// MaxUpload returns the configured maximum upload size in bytes.
func (fm *FileManager) MaxUpload() int64 {
	return fm.maxUpload
}

// Upload stores a file in object storage and creates a tab_file record.
// Thumbnail generation for images is attempted but failures are non-fatal.
func (fm *FileManager) Upload(ctx context.Context, site, owner string, file io.Reader, header FileUploadHeader, opts UploadOptions) (*FileMeta, error) {
	// Validate size.
	if header.Size > fm.maxUpload {
		return nil, &FileTooLargeError{Size: header.Size, Max: fm.maxUpload}
	}

	// Validate content type.
	if !allowedContentTypes[header.ContentType] {
		return nil, &InvalidContentTypeError{ContentType: header.ContentType}
	}

	uid := uuid.NewString()
	name := "FILE-" + uid
	key := objectKey(site, opts.IsPrivate, uid, header.FileName)

	// Upload to object storage.
	if err := fm.storage.Upload(ctx, key, file, header.Size, header.ContentType); err != nil {
		return nil, fmt.Errorf("storage/manager: upload %q: %w", key, err)
	}

	// Insert DB record.
	pool, err := fm.db.ForSite(ctx, site)
	if err != nil {
		// Best-effort cleanup of the uploaded object.
		if delErr := fm.storage.Delete(ctx, key); delErr != nil {
			fm.logger.ErrorContext(ctx, "orphaned file after DB pool error",
				slog.String("key", key), slog.String("error", delErr.Error()))
		}
		return nil, fmt.Errorf("storage/manager: db pool for %q: %w", site, err)
	}

	_, err = pool.Exec(ctx, insertFileSQL,
		name, header.FileName, key, header.Size, header.ContentType,
		opts.AttachedToDocType, opts.AttachedToName, opts.IsPrivate, owner,
	)
	if err != nil {
		if delErr := fm.storage.Delete(ctx, key); delErr != nil {
			fm.logger.ErrorContext(ctx, "orphaned file after DB insert error",
				slog.String("key", key), slog.String("error", delErr.Error()))
		}
		return nil, fmt.Errorf("storage/manager: insert tab_file %q: %w", name, err)
	}

	meta := &FileMeta{
		Name:              name,
		FileName:          header.FileName,
		FileURL:           key,
		FileSize:          header.Size,
		ContentType:       header.ContentType,
		AttachedToDocType: opts.AttachedToDocType,
		AttachedToName:    opts.AttachedToName,
		IsPrivate:         opts.IsPrivate,
		Owner:             owner,
		Creation:          time.Now(),
	}

	// Attempt thumbnail generation for images (non-fatal).
	if IsImageContentType(header.ContentType) {
		fm.tryGenerateThumbnail(ctx, site, key, header.ContentType, name)
	}

	return meta, nil
}

// tryGenerateThumbnail attempts to download the uploaded image, generate a
// thumbnail, and upload it. Errors are logged but do not propagate.
func (fm *FileManager) tryGenerateThumbnail(ctx context.Context, site, key, contentType, name string) {
	reader, err := fm.storage.Download(ctx, key)
	if err != nil {
		fm.logger.WarnContext(ctx, "thumbnail: failed to download source",
			slog.String("key", key), slog.String("error", err.Error()))
		return
	}
	defer reader.Close() //nolint:errcheck

	thumbReader, thumbSize, err := GenerateThumbnail(reader, DefaultThumbWidth, DefaultThumbHeight)
	if err != nil {
		fm.logger.WarnContext(ctx, "thumbnail: generation failed",
			slog.String("key", key), slog.String("error", err.Error()))
		return
	}

	thumbKey := ThumbnailKey(key)
	if err := fm.storage.Upload(ctx, thumbKey, thumbReader, thumbSize, "image/jpeg"); err != nil {
		fm.logger.WarnContext(ctx, "thumbnail: upload failed",
			slog.String("key", thumbKey), slog.String("error", err.Error()))
	}
}

// Download retrieves a file from storage. The caller must close the returned reader.
// Access control is NOT enforced here — callers are responsible for permission checks.
func (fm *FileManager) Download(ctx context.Context, site, name string) (io.ReadCloser, *FileMeta, error) {
	meta, err := fm.GetFileMeta(ctx, site, name)
	if err != nil {
		return nil, nil, err
	}

	reader, err := fm.storage.Download(ctx, meta.FileURL)
	if err != nil {
		return nil, nil, fmt.Errorf("storage/manager: download %q: %w", meta.FileURL, err)
	}
	return reader, meta, nil
}

// Delete removes a file from both storage and the database.
func (fm *FileManager) Delete(ctx context.Context, site, name string) error {
	meta, err := fm.GetFileMeta(ctx, site, name)
	if err != nil {
		return err
	}

	// Delete from object storage (main file + thumbnail).
	if delErr := fm.storage.Delete(ctx, meta.FileURL); delErr != nil {
		return fmt.Errorf("storage/manager: delete object %q: %w", meta.FileURL, delErr)
	}
	thumbKey := ThumbnailKey(meta.FileURL)
	if thumbErr := fm.storage.Delete(ctx, thumbKey); thumbErr != nil {
		// Thumbnail may not exist; log but don't fail.
		fm.logger.WarnContext(ctx, "thumbnail delete failed (may not exist)",
			slog.String("key", thumbKey), slog.String("error", thumbErr.Error()))
	}

	// Delete from DB.
	pool, err := fm.db.ForSite(ctx, site)
	if err != nil {
		return fmt.Errorf("storage/manager: db pool for %q: %w", site, err)
	}
	if _, err := pool.Exec(ctx, deleteFileByNameSQL, name); err != nil {
		return fmt.Errorf("storage/manager: delete tab_file %q: %w", name, err)
	}
	return nil
}

// GetFileMeta retrieves file metadata from tab_file by name.
func (fm *FileManager) GetFileMeta(ctx context.Context, site, name string) (*FileMeta, error) {
	pool, err := fm.db.ForSite(ctx, site)
	if err != nil {
		return nil, fmt.Errorf("storage/manager: db pool for %q: %w", site, err)
	}

	var m FileMeta
	err = pool.QueryRow(ctx, selectFileByNameSQL, name).Scan(
		&m.Name, &m.FileName, &m.FileURL, &m.FileSize, &m.ContentType,
		&m.AttachedToDocType, &m.AttachedToName, &m.IsPrivate, &m.Owner, &m.Creation,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, &FileNotFoundError{Name: name}
		}
		return nil, fmt.Errorf("storage/manager: query tab_file %q: %w", name, err)
	}
	return &m, nil
}

// GetSignedURL returns a time-limited download URL for the file.
func (fm *FileManager) GetSignedURL(ctx context.Context, site, name string, expiry time.Duration) (string, error) {
	meta, err := fm.GetFileMeta(ctx, site, name)
	if err != nil {
		return "", err
	}
	return fm.storage.PresignedGetURL(ctx, meta.FileURL, expiry)
}

// ListByAttachment returns all files attached to a specific document.
func (fm *FileManager) ListByAttachment(ctx context.Context, site, doctype, docname string) ([]FileMeta, error) {
	pool, err := fm.db.ForSite(ctx, site)
	if err != nil {
		return nil, fmt.Errorf("storage/manager: db pool for %q: %w", site, err)
	}

	rows, err := pool.Query(ctx, selectFilesByRefSQL, doctype, docname)
	if err != nil {
		return nil, fmt.Errorf("storage/manager: list files for %s/%s: %w", doctype, docname, err)
	}
	defer rows.Close()

	var files []FileMeta
	for rows.Next() {
		var m FileMeta
		if err := rows.Scan(
			&m.Name, &m.FileName, &m.FileURL, &m.FileSize, &m.ContentType,
			&m.AttachedToDocType, &m.AttachedToName, &m.IsPrivate, &m.Owner, &m.Creation,
		); err != nil {
			return nil, fmt.Errorf("storage/manager: scan file row: %w", err)
		}
		files = append(files, m)
	}
	return files, rows.Err()
}

// objectKey builds the storage object key with site-scoped path namespacing.
// Format: {site}/{private|public}/{uuid}/{filename}
func objectKey(site string, isPrivate bool, uid, filename string) string {
	visibility := "public"
	if isPrivate {
		visibility = "private"
	}
	return site + "/" + visibility + "/" + uid + "/" + filename
}

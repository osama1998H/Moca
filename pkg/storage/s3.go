package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/osama1998H/moca/internal/config"
)

// S3Storage implements Storage using an S3-compatible backend (MinIO, AWS S3).
type S3Storage struct {
	client *minio.Client
	bucket string
}

// NewS3Storage creates an S3Storage from the given config.
func NewS3Storage(cfg config.StorageConfig) (*S3Storage, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("storage/s3: endpoint is required")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("storage/s3: bucket is required")
	}

	// Parse endpoint to determine TLS usage.
	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("storage/s3: invalid endpoint %q: %w", cfg.Endpoint, err)
	}
	useSSL := u.Scheme == "https"
	host := u.Host

	client, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("storage/s3: init client: %w", err)
	}

	return &S3Storage{client: client, bucket: cfg.Bucket}, nil
}

func (s *S3Storage) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	opts := minio.PutObjectOptions{ContentType: contentType}
	_, err := s.client.PutObject(ctx, s.bucket, key, reader, size, opts)
	if err != nil {
		return fmt.Errorf("storage/s3: upload %q: %w", key, err)
	}
	return nil
}

func (s *S3Storage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("storage/s3: download %q: %w", key, err)
	}
	// Verify the object exists by stat-ing it. GetObject doesn't error on missing keys
	// until the first Read; StatObject gives us an immediate error.
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		return nil, fmt.Errorf("storage/s3: download %q: %w", key, err)
	}
	return obj, nil
}

func (s *S3Storage) Delete(ctx context.Context, key string) error {
	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("storage/s3: delete %q: %w", key, err)
	}
	return nil
}

func (s *S3Storage) PresignedGetURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	u, err := s.client.PresignedGetObject(ctx, s.bucket, key, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("storage/s3: presigned get %q: %w", key, err)
	}
	return u.String(), nil
}

func (s *S3Storage) PresignedPutURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	u, err := s.client.PresignedPutObject(ctx, s.bucket, key, expiry)
	if err != nil {
		return "", fmt.Errorf("storage/s3: presigned put %q: %w", key, err)
	}
	return u.String(), nil
}

func (s *S3Storage) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" {
			return false, nil
		}
		return false, fmt.Errorf("storage/s3: exists %q: %w", key, err)
	}
	return true, nil
}

// EnsureBucket creates the bucket if it does not exist. Call during server startup.
func (s *S3Storage) EnsureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("storage/s3: check bucket %q: %w", s.bucket, err)
	}
	if !exists {
		if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("storage/s3: create bucket %q: %w", s.bucket, err)
		}
	}
	return nil
}

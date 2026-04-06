package storage

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

// mockStorage implements Storage for unit testing.
type mockStorage struct {
	uploaded   map[string][]byte
	uploadErr  error
	downloadErr error
	deleteErr  error
	presignErr error
	existsVal  bool
}

func newMockStorage() *mockStorage {
	return &mockStorage{uploaded: make(map[string][]byte)}
}

func (m *mockStorage) Upload(_ context.Context, key string, reader io.Reader, _ int64, _ string) error {
	if m.uploadErr != nil {
		return m.uploadErr
	}
	data, _ := io.ReadAll(reader)
	m.uploaded[key] = data
	return nil
}

func (m *mockStorage) Download(_ context.Context, key string) (io.ReadCloser, error) {
	if m.downloadErr != nil {
		return nil, m.downloadErr
	}
	data, ok := m.uploaded[key]
	if !ok {
		return nil, &FileNotFoundError{Name: key}
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *mockStorage) Delete(_ context.Context, key string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.uploaded, key)
	return nil
}

func (m *mockStorage) PresignedGetURL(_ context.Context, key string, _ time.Duration) (string, error) {
	if m.presignErr != nil {
		return "", m.presignErr
	}
	return "https://example.com/signed/" + key, nil
}

func (m *mockStorage) PresignedPutURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://example.com/put/" + key, nil
}

func (m *mockStorage) Exists(_ context.Context, _ string) (bool, error) {
	return m.existsVal, nil
}

func TestObjectKey_Private(t *testing.T) {
	key := objectKey("mysite", true, "abc-123", "photo.jpg")
	if key != "mysite/private/abc-123/photo.jpg" {
		t.Errorf("unexpected key: %s", key)
	}
}

func TestObjectKey_Public(t *testing.T) {
	key := objectKey("mysite", false, "abc-123", "photo.jpg")
	if key != "mysite/public/abc-123/photo.jpg" {
		t.Errorf("unexpected key: %s", key)
	}
}

func TestFileTooLargeError(t *testing.T) {
	e := &FileTooLargeError{Size: 100, Max: 50}
	if !strings.Contains(e.Error(), "100") || !strings.Contains(e.Error(), "50") {
		t.Errorf("unexpected error message: %s", e.Error())
	}
}

func TestFileNotFoundError(t *testing.T) {
	e := &FileNotFoundError{Name: "FILE-abc"}
	if !strings.Contains(e.Error(), "FILE-abc") {
		t.Errorf("unexpected error message: %s", e.Error())
	}
}

func TestInvalidContentTypeError(t *testing.T) {
	e := &InvalidContentTypeError{ContentType: "application/exe"}
	if !strings.Contains(e.Error(), "application/exe") {
		t.Errorf("unexpected error message: %s", e.Error())
	}
}

func TestNewFileManager_DefaultMaxUpload(t *testing.T) {
	fm := NewFileManager(newMockStorage(), nil, nil, 0)
	if fm.MaxUpload() != DefaultMaxUpload {
		t.Errorf("expected default max upload %d, got %d", DefaultMaxUpload, fm.MaxUpload())
	}
}

func TestNewFileManager_CustomMaxUpload(t *testing.T) {
	fm := NewFileManager(newMockStorage(), nil, nil, 10<<20)
	if fm.MaxUpload() != 10<<20 {
		t.Errorf("expected 10MiB, got %d", fm.MaxUpload())
	}
}

func TestAllowedContentTypes(t *testing.T) {
	allowed := []string{
		"image/jpeg", "image/png", "application/pdf",
		"text/plain", "text/csv", "application/json",
	}
	for _, ct := range allowed {
		if !allowedContentTypes[ct] {
			t.Errorf("expected %q to be allowed", ct)
		}
	}

	disallowed := []string{
		"application/x-executable", "application/x-msdownload",
		"application/x-sh", "",
	}
	for _, ct := range disallowed {
		if allowedContentTypes[ct] {
			t.Errorf("expected %q to be disallowed", ct)
		}
	}
}

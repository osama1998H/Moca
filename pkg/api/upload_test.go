package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/storage"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// ── Helpers ──────────────────────────────────────────────────────────────────

func uploadCtx(r *http.Request) *http.Request {
	ctx := WithSite(r.Context(), &tenancy.SiteContext{Name: "test_site"})
	ctx = WithUser(ctx, &auth.User{Email: "admin@test.com", FullName: "Admin", Roles: []string{"Administrator"}})
	return r.WithContext(ctx)
}

// ── Helper: build multipart request ─────────────────────────────────────────

func buildMultipartUpload(t *testing.T, filename, contentType string, content []byte, fields map[string]string) (*http.Request, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write file content: %v", err)
	}

	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			t.Fatalf("write field %s: %v", k, err)
		}
	}
	_ = writer.Close()

	r := httptest.NewRequest(http.MethodPost, "/api/v1/file/upload", &buf)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	return r, writer.FormDataContentType()
}

// ── Tests ───────────────────────────────────────────────────────────────────

func TestHandleUpload_NoSite(t *testing.T) {
	h := &UploadHandler{perm: AllowAllPermissionChecker{}}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r, _ := buildMultipartUpload(t, "test.txt", "text/plain", []byte("hello"), nil)
	// Don't set site context.
	ctx := WithUser(r.Context(), &auth.User{Email: "u@test.com"})
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	assertErrorCode(t, w, "SITE_REQUIRED")
}

func TestHandleUpload_NoAuth(t *testing.T) {
	h := &UploadHandler{perm: AllowAllPermissionChecker{}}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r, _ := buildMultipartUpload(t, "test.txt", "text/plain", []byte("hello"), nil)
	// Set site but no user.
	ctx := WithSite(r.Context(), &tenancy.SiteContext{Name: "test_site"})
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	assertErrorCode(t, w, "AUTH_REQUIRED")
}

func TestHandleUpload_NoFile(t *testing.T) {
	fm := storage.NewFileManager(newNullStorage(), nil, nil, 25<<20)
	h := &UploadHandler{files: fm, perm: AllowAllPermissionChecker{}}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	// Empty multipart form (no file field).
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.Close()

	r := httptest.NewRequest(http.MethodPost, "/api/v1/file/upload", &buf)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r = uploadCtx(r)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	assertErrorCode(t, w, "FILE_REQUIRED")
}

func TestHandleDownload_NoSite(t *testing.T) {
	h := &UploadHandler{perm: AllowAllPermissionChecker{}}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest(http.MethodGet, "/api/v1/file/FILE-123", nil)
	ctx := WithUser(r.Context(), &auth.User{Email: "u@test.com"})
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	assertErrorCode(t, w, "SITE_REQUIRED")
}

func TestHandleDownload_NoAuth(t *testing.T) {
	h := &UploadHandler{perm: AllowAllPermissionChecker{}}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest(http.MethodGet, "/api/v1/file/FILE-123", nil)
	ctx := WithSite(r.Context(), &tenancy.SiteContext{Name: "test_site"})
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestHandleDelete_NoAuth(t *testing.T) {
	h := &UploadHandler{perm: AllowAllPermissionChecker{}}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest(http.MethodDelete, "/api/v1/file/FILE-123", nil)
	ctx := WithSite(r.Context(), &tenancy.SiteContext{Name: "test_site"})
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestHandleSignedURL_NoSite(t *testing.T) {
	h := &UploadHandler{perm: AllowAllPermissionChecker{}}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest(http.MethodGet, "/api/v1/file/FILE-123/url", nil)
	ctx := WithUser(r.Context(), &auth.User{Email: "u@test.com"})
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCheckReadAccess_OwnerAllowed(t *testing.T) {
	h := &UploadHandler{perm: AllowAllPermissionChecker{}}
	user := &auth.User{Email: "owner@test.com"}
	meta := &storage.FileMeta{
		IsPrivate: true,
		Owner:     "owner@test.com",
	}

	err := h.checkReadAccess(context.Background(), user, meta)
	if err != nil {
		t.Errorf("expected owner to be allowed, got: %v", err)
	}
}

func TestCheckReadAccess_NonOwnerDenied(t *testing.T) {
	h := &UploadHandler{perm: AllowAllPermissionChecker{}}
	user := &auth.User{Email: "other@test.com"}
	meta := &storage.FileMeta{
		IsPrivate: true,
		Owner:     "owner@test.com",
		// No attached doctype — owner only.
	}

	err := h.checkReadAccess(context.Background(), user, meta)
	if err == nil {
		t.Error("expected non-owner to be denied")
	}
}

func TestCheckReadAccess_WithDocType_PermGranted(t *testing.T) {
	h := &UploadHandler{perm: AllowAllPermissionChecker{}}
	user := &auth.User{Email: "other@test.com"}
	meta := &storage.FileMeta{
		IsPrivate:         true,
		Owner:             "owner@test.com",
		AttachedToDocType: "SalesOrder",
	}

	err := h.checkReadAccess(context.Background(), user, meta)
	if err != nil {
		t.Errorf("expected AllowAll to permit, got: %v", err)
	}
}

func TestCheckReadAccess_WithDocType_PermDenied(t *testing.T) {
	h := &UploadHandler{perm: uploadDenyPerm{}}
	user := &auth.User{Email: "other@test.com"}
	meta := &storage.FileMeta{
		IsPrivate:         true,
		Owner:             "owner@test.com",
		AttachedToDocType: "SalesOrder",
	}

	err := h.checkReadAccess(context.Background(), user, meta)
	if err == nil {
		t.Error("expected permission denied")
	}
}

func TestMapErrorResponse_FileNotFound(t *testing.T) {
	w := httptest.NewRecorder()
	handled := mapErrorResponse(w, &storage.FileNotFoundError{Name: "FILE-abc"})
	if !handled {
		t.Fatal("expected FileNotFoundError to be handled")
	}
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestMapErrorResponse_FileTooLarge(t *testing.T) {
	w := httptest.NewRecorder()
	handled := mapErrorResponse(w, &storage.FileTooLargeError{Size: 100, Max: 50})
	if !handled {
		t.Fatal("expected FileTooLargeError to be handled")
	}
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d", w.Code)
	}
}

func TestMapErrorResponse_InvalidContentType(t *testing.T) {
	w := httptest.NewRecorder()
	handled := mapErrorResponse(w, &storage.InvalidContentTypeError{ContentType: "application/exe"})
	if !handled {
		t.Fatal("expected InvalidContentTypeError to be handled")
	}
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("expected 415, got %d", w.Code)
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────────

type uploadDenyPerm struct{}

func (uploadDenyPerm) CheckDocPerm(_ context.Context, user *auth.User, doctype, perm string) error {
	return &PermissionDeniedError{User: user.Email, Doctype: doctype, Perm: perm}
}

func assertErrorCode(t *testing.T, w *httptest.ResponseRecorder, code string) {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if env.Error.Code != code {
		t.Errorf("expected error code %q, got %q", code, env.Error.Code)
	}
}

// nullStorage is a no-op Storage implementation for tests that don't exercise storage.
type nullStorage struct{}

func newNullStorage() *nullStorage { return &nullStorage{} }

func (nullStorage) Upload(_ context.Context, _ string, _ io.Reader, _ int64, _ string) error {
	return nil
}
func (nullStorage) Download(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (nullStorage) Delete(_ context.Context, _ string) error                            { return nil }
func (nullStorage) PresignedGetURL(_ context.Context, _ string, _ time.Duration) (string, error) {
	return "https://example.com/signed", nil
}
func (nullStorage) PresignedPutURL(_ context.Context, _ string, _ time.Duration) (string, error) {
	return "https://example.com/put", nil
}
func (nullStorage) Exists(_ context.Context, _ string) (bool, error) { return false, nil }

// Verify nullStorage implements storage.Storage.
var _ storage.Storage = nullStorage{}

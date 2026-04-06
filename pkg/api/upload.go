package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/storage"
)

// maxSignedURLExpiry is the maximum allowed expiry duration for signed URLs.
const maxSignedURLExpiry = 24 * time.Hour

// defaultSignedURLExpiry is used when no expiry query parameter is provided.
const defaultSignedURLExpiry = 1 * time.Hour

// UploadHandler provides HTTP endpoints for file upload, download, delete,
// and signed URL generation.
type UploadHandler struct {
	files  *storage.FileManager
	perm   PermissionChecker
	logger *slog.Logger
}

// NewUploadHandler creates an UploadHandler.
func NewUploadHandler(files *storage.FileManager, perm PermissionChecker, logger *slog.Logger) *UploadHandler {
	return &UploadHandler{
		files:  files,
		perm:   perm,
		logger: logger,
	}
}

// RegisterRoutes registers file management endpoints on the given mux.
func (h *UploadHandler) RegisterRoutes(mux *http.ServeMux, version string) {
	p := "/api/" + version
	mux.HandleFunc("POST "+p+"/file/upload", h.handleUpload)
	mux.HandleFunc("GET "+p+"/file/{name}", h.handleDownload)
	mux.HandleFunc("DELETE "+p+"/file/{name}", h.handleDelete)
	mux.HandleFunc("GET "+p+"/file/{name}/url", h.handleSignedURL)
}

func (h *UploadHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	site := SiteFromContext(r.Context())
	if site == nil {
		writeError(w, http.StatusBadRequest, "SITE_REQUIRED", "site context is required")
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication is required")
		return
	}

	// Limit request body to the configured max upload size.
	r.Body = http.MaxBytesReader(w, r.Body, h.files.MaxUpload())

	if err := r.ParseMultipartForm(h.files.MaxUpload()); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE",
				fmt.Sprintf("upload exceeds maximum size of %d bytes", h.files.MaxUpload()))
			return
		}
		writeError(w, http.StatusBadRequest, "INVALID_FORM", "failed to parse multipart form: "+err.Error())
		return
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "FILE_REQUIRED", "multipart field 'file' is required")
		return
	}
	defer file.Close() //nolint:errcheck

	// Read optional form fields.
	attachedDocType := r.FormValue("attached_to_doctype")
	attachedDocName := r.FormValue("attached_to_name")
	isPrivate := r.FormValue("is_private") != "0" // default true

	// Permission check: if attaching to a document, user needs write permission.
	if attachedDocType != "" {
		if permErr := h.perm.CheckDocPerm(r.Context(), user, attachedDocType, "write"); permErr != nil {
			writeError(w, http.StatusForbidden, "PERMISSION_DENIED", "no write permission on "+attachedDocType)
			return
		}
	}

	header := storage.FileUploadHeader{
		FileName:    fileHeader.Filename,
		Size:        fileHeader.Size,
		ContentType: fileHeader.Header.Get("Content-Type"),
	}
	opts := storage.UploadOptions{
		AttachedToDocType: attachedDocType,
		AttachedToName:    attachedDocName,
		IsPrivate:         isPrivate,
	}

	meta, err := h.files.Upload(r.Context(), site.Name, user.Email, file, header, opts)
	if err != nil {
		if !mapErrorResponse(w, err) {
			writeError(w, http.StatusInternalServerError, "UPLOAD_ERROR", err.Error())
		}
		return
	}

	writeSuccess(w, http.StatusCreated, meta)
}

func (h *UploadHandler) handleDownload(w http.ResponseWriter, r *http.Request) {
	site := SiteFromContext(r.Context())
	if site == nil {
		writeError(w, http.StatusBadRequest, "SITE_REQUIRED", "site context is required")
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication is required")
		return
	}

	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "NAME_REQUIRED", "file name is required")
		return
	}

	fileMeta, err := h.files.GetFileMeta(r.Context(), site.Name, name)
	if err != nil {
		if !mapErrorResponse(w, err) {
			writeError(w, http.StatusInternalServerError, "FILE_ERROR", err.Error())
		}
		return
	}

	// Access control for private files.
	if fileMeta.IsPrivate {
		if accessErr := h.checkReadAccess(r.Context(), user, fileMeta); accessErr != nil {
			writeError(w, http.StatusForbidden, "PERMISSION_DENIED", accessErr.Error())
			return
		}
	}

	reader, _, err := h.files.Download(r.Context(), site.Name, name)
	if err != nil {
		if !mapErrorResponse(w, err) {
			writeError(w, http.StatusInternalServerError, "DOWNLOAD_ERROR", err.Error())
		}
		return
	}
	defer reader.Close() //nolint:errcheck

	w.Header().Set("Content-Type", fileMeta.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileMeta.FileName))
	if fileMeta.FileSize > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(fileMeta.FileSize, 10))
	}
	w.WriteHeader(http.StatusOK)
	io.Copy(w, reader) //nolint:errcheck
}

func (h *UploadHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	site := SiteFromContext(r.Context())
	if site == nil {
		writeError(w, http.StatusBadRequest, "SITE_REQUIRED", "site context is required")
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication is required")
		return
	}

	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "NAME_REQUIRED", "file name is required")
		return
	}

	fileMeta, err := h.files.GetFileMeta(r.Context(), site.Name, name)
	if err != nil {
		if !mapErrorResponse(w, err) {
			writeError(w, http.StatusInternalServerError, "FILE_ERROR", err.Error())
		}
		return
	}

	// Access control: owner can always delete; non-owner needs write perm on attached doctype.
	if user.Email != fileMeta.Owner {
		if fileMeta.AttachedToDocType != "" {
			if permErr := h.perm.CheckDocPerm(r.Context(), user, fileMeta.AttachedToDocType, "write"); permErr != nil {
				writeError(w, http.StatusForbidden, "PERMISSION_DENIED", "no write permission to delete this file")
				return
			}
		} else {
			writeError(w, http.StatusForbidden, "PERMISSION_DENIED", "only the owner can delete this file")
			return
		}
	}

	if err := h.files.Delete(r.Context(), site.Name, name); err != nil {
		if !mapErrorResponse(w, err) {
			writeError(w, http.StatusInternalServerError, "DELETE_ERROR", err.Error())
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *UploadHandler) handleSignedURL(w http.ResponseWriter, r *http.Request) {
	site := SiteFromContext(r.Context())
	if site == nil {
		writeError(w, http.StatusBadRequest, "SITE_REQUIRED", "site context is required")
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication is required")
		return
	}

	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "NAME_REQUIRED", "file name is required")
		return
	}

	fileMeta, err := h.files.GetFileMeta(r.Context(), site.Name, name)
	if err != nil {
		if !mapErrorResponse(w, err) {
			writeError(w, http.StatusInternalServerError, "FILE_ERROR", err.Error())
		}
		return
	}

	// Same access control as download.
	if fileMeta.IsPrivate {
		if accessErr := h.checkReadAccess(r.Context(), user, fileMeta); accessErr != nil {
			writeError(w, http.StatusForbidden, "PERMISSION_DENIED", accessErr.Error())
			return
		}
	}

	expiry := defaultSignedURLExpiry
	if expiryStr := r.URL.Query().Get("expiry"); expiryStr != "" {
		parsed, parseErr := time.ParseDuration(expiryStr)
		if parseErr != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "INVALID_EXPIRY", "expiry must be a positive duration (e.g. 1h, 30m)")
			return
		}
		if parsed > maxSignedURLExpiry {
			parsed = maxSignedURLExpiry
		}
		expiry = parsed
	}

	signedURL, err := h.files.GetSignedURL(r.Context(), site.Name, name, expiry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SIGNED_URL_ERROR", err.Error())
		return
	}

	writeSuccess(w, http.StatusOK, map[string]string{"url": signedURL})
}

// checkReadAccess enforces access control for private files.
// Private files with an attached doctype require read permission on that doctype.
// Private files without an attached doctype are accessible only by the owner.
func (h *UploadHandler) checkReadAccess(ctx context.Context, user *auth.User, fileMeta *storage.FileMeta) error {
	if fileMeta.AttachedToDocType != "" {
		return h.perm.CheckDocPerm(ctx, user, fileMeta.AttachedToDocType, "read")
	}
	// No attached doctype — owner only.
	if user.Email != fileMeta.Owner {
		return fmt.Errorf("only the file owner can access this private file")
	}
	return nil
}

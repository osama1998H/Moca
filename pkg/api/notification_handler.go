package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/osama1998H/moca/pkg/notify"
	"github.com/osama1998H/moca/pkg/orm"
)

// NotificationHandler serves notification-related API endpoints.
type NotificationHandler struct {
	notifier *notify.InAppNotifier
	db       *orm.DBManager
	logger   *slog.Logger
}

// NewNotificationHandler creates a NotificationHandler.
func NewNotificationHandler(notifier *notify.InAppNotifier, db *orm.DBManager, logger *slog.Logger) *NotificationHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &NotificationHandler{
		notifier: notifier,
		db:       db,
		logger:   logger,
	}
}

// RegisterRoutes registers notification endpoints on the mux.
func (h *NotificationHandler) RegisterRoutes(mux *http.ServeMux, version string) {
	p := "/api/" + version
	mux.HandleFunc("GET "+p+"/notifications", h.handleList)
	mux.HandleFunc("GET "+p+"/notifications/count", h.handleCount)
	mux.HandleFunc("PUT "+p+"/notifications/mark-read", h.handleMarkRead)
}

// handleList returns unread notifications for the authenticated user.
func (h *NotificationHandler) handleList(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	site := SiteFromContext(r.Context())
	if site == nil || site.Pool == nil {
		writeError(w, http.StatusBadRequest, "NO_SITE", "site context required")
		return
	}

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	notifications, total, err := h.notifier.GetUnread(r.Context(), site.Pool, site.DBSchema, user.Email, limit)
	if err != nil {
		h.logger.Error("notification list failed",
			slog.String("user", user.Email),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch notifications")
		return
	}

	if notifications == nil {
		notifications = []notify.Notification{}
	}
	writeListSuccess(w, notifications, total, limit, 0)
}

// handleCount returns the unread notification count for the authenticated user.
func (h *NotificationHandler) handleCount(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	site := SiteFromContext(r.Context())
	if site == nil || site.Pool == nil {
		writeError(w, http.StatusBadRequest, "NO_SITE", "site context required")
		return
	}

	_, total, err := h.notifier.GetUnread(r.Context(), site.Pool, site.DBSchema, user.Email, 0)
	if err != nil {
		h.logger.Error("notification count failed",
			slog.String("user", user.Email),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to count notifications")
		return
	}

	writeSuccess(w, http.StatusOK, map[string]int{"count": total})
}

// markReadRequest is the expected body for PUT /api/v1/notifications/mark-read.
type markReadRequest struct {
	Names []string `json:"names"` // notification IDs, or ["*"] for all
}

// handleMarkRead marks specific notifications as read.
func (h *NotificationHandler) handleMarkRead(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	site := SiteFromContext(r.Context())
	if site == nil || site.Pool == nil {
		writeError(w, http.StatusBadRequest, "NO_SITE", "site context required")
		return
	}

	var req markReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
		return
	}
	if len(req.Names) == 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "names field is required")
		return
	}

	if err := h.notifier.MarkRead(r.Context(), site.Pool, site.DBSchema, user.Email, req.Names...); err != nil {
		h.logger.Error("notification mark-read failed",
			slog.String("user", user.Email),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to mark notifications as read")
		return
	}

	writeSuccess(w, http.StatusOK, map[string]string{"message": "ok"})
}

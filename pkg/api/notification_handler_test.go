package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/notify"
	"github.com/osama1998H/moca/pkg/tenancy"
)

func TestNotificationHandler_RegisterRoutes(t *testing.T) {
	h := NewNotificationHandler(nil, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")
	// No panic = success (routes registered).
}

func TestNotificationHandler_HandleList_NoAuth(t *testing.T) {
	h := NewNotificationHandler(notify.NewInAppNotifier(nil), nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	req := httptest.NewRequest("GET", "/api/v1/notifications", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestNotificationHandler_HandleList_NoSite(t *testing.T) {
	h := NewNotificationHandler(notify.NewInAppNotifier(nil), nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	req := httptest.NewRequest("GET", "/api/v1/notifications", nil)
	// Add user context but no site.
	ctx := WithUser(req.Context(), &auth.User{Email: "test@example.com"})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestNotificationHandler_HandleCount_NoAuth(t *testing.T) {
	h := NewNotificationHandler(notify.NewInAppNotifier(nil), nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	req := httptest.NewRequest("GET", "/api/v1/notifications/count", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestNotificationHandler_HandleMarkRead_NoAuth(t *testing.T) {
	h := NewNotificationHandler(notify.NewInAppNotifier(nil), nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	body, _ := json.Marshal(map[string]any{"names": []string{"id1"}})
	req := httptest.NewRequest("PUT", "/api/v1/notifications/mark-read", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestNotificationHandler_HandleMarkRead_InvalidBody(t *testing.T) {
	h := NewNotificationHandler(notify.NewInAppNotifier(nil), nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	req := httptest.NewRequest("PUT", "/api/v1/notifications/mark-read", bytes.NewReader([]byte("not json")))
	ctx := WithUser(req.Context(), &auth.User{Email: "test@example.com"})
	ctx = WithSite(ctx, &tenancy.SiteContext{Name: "test", DBSchema: "test"})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestNotificationHandler_HandleMarkRead_EmptyNames(t *testing.T) {
	h := NewNotificationHandler(notify.NewInAppNotifier(nil), nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	body, _ := json.Marshal(map[string]any{"names": []string{}})
	req := httptest.NewRequest("PUT", "/api/v1/notifications/mark-read", bytes.NewReader(body))
	ctx := WithUser(req.Context(), &auth.User{Email: "test@example.com"})
	ctx = WithSite(ctx, &tenancy.SiteContext{Name: "test", DBSchema: "test"})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

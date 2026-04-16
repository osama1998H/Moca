package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/osama1998H/moca/pkg/api"
	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/tenancy"
)

func setupDevMux(t *testing.T) (*http.ServeMux, string) {
	t.Helper()
	dir := t.TempDir()
	h := api.NewDevHandler(dir, nil, nil)
	mux := http.NewServeMux()
	h.RegisterDevRoutes(mux, "v1", api.DevAuthMiddleware())
	return mux, dir
}

func devRequest(method, path string, body any) *http.Request {
	var req *http.Request
	if body != nil {
		data, _ := json.Marshal(body)
		req = httptest.NewRequest(method, path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	return req
}

func withAdminCtx(r *http.Request) *http.Request {
	ctx := api.WithUser(r.Context(), &auth.User{
		Email: "admin@test.com",
		Roles: []string{"Administrator"},
	})
	return r.WithContext(ctx)
}

func withGuestCtx(r *http.Request) *http.Request {
	ctx := api.WithUser(r.Context(), &auth.User{
		Email: "Guest",
		Roles: []string{"Guest"},
	})
	return r.WithContext(ctx)
}

func validDocTypeBody() map[string]any {
	return map[string]any{
		"name":   "IntegTest",
		"app":    "testapp",
		"module": "core",
		"layout": map[string]any{"tabs": []any{}},
		"fields": map[string]any{
			"title": map[string]any{"field_type": "Data", "name": "title"},
		},
		"settings":    map[string]any{},
		"permissions": []any{},
	}
}

func TestIntegration_DevAPI_NoUser_Returns403(t *testing.T) {
	mux, _ := setupDevMux(t)
	req := devRequest("POST", "/api/v1/dev/doctype", validDocTypeBody())
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for no user, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIntegration_DevAPI_GuestUser_Returns403(t *testing.T) {
	mux, _ := setupDevMux(t)
	req := withGuestCtx(devRequest("POST", "/api/v1/dev/doctype", validDocTypeBody()))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for Guest, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIntegration_DevAPI_Admin_Creates(t *testing.T) {
	mux, dir := setupDevMux(t)
	req := withAdminCtx(devRequest("POST", "/api/v1/dev/doctype", validDocTypeBody()))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for admin, got %d: %s", w.Code, w.Body.String())
	}
	jsonPath := filepath.Join(dir, "testapp", "modules", "core", "doctypes", "integ_test", "integ_test.json")
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		t.Fatal("expected doctype file to be created on disk")
	}
}

func TestIntegration_DevAPI_PathTraversal_Returns400(t *testing.T) {
	mux, _ := setupDevMux(t)
	body := validDocTypeBody()
	body["app"] = "../../etc"
	req := withAdminCtx(devRequest("POST", "/api/v1/dev/doctype", body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIntegration_DevAPI_Delete_NonAdmin_Returns403(t *testing.T) {
	mux, _ := setupDevMux(t)
	req := withGuestCtx(devRequest("DELETE", "/api/v1/dev/doctype/SomeType", nil))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for Guest delete, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIntegration_DevAPI_FullRoundTrip(t *testing.T) {
	mux, _ := setupDevMux(t)

	// Create
	req := withAdminCtx(devRequest("POST", "/api/v1/dev/doctype", validDocTypeBody()))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Get
	req = withAdminCtx(devRequest("GET", "/api/v1/dev/doctype/IntegTest", nil))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Update
	updateBody := validDocTypeBody()
	updateBody["fields"] = map[string]any{
		"title":       map[string]any{"field_type": "Data", "name": "title"},
		"description": map[string]any{"field_type": "Text", "name": "description"},
	}
	req = withAdminCtx(devRequest("PUT", "/api/v1/dev/doctype/IntegTest", updateBody))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Delete
	req = withAdminCtx(devRequest("DELETE", "/api/v1/dev/doctype/IntegTest", nil))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify deleted
	req = withAdminCtx(devRequest("GET", "/api/v1/dev/doctype/IntegTest", nil))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get after delete: expected 404, got %d", w.Code)
	}
}

func TestIntegration_DevAPI_WithoutSiteContext_Returns400(t *testing.T) {
	dir := t.TempDir()
	reg := &mockDevRegisterer{}
	h := api.NewDevHandlerWithRegisterer(dir, reg, nil)
	mux := http.NewServeMux()
	h.RegisterDevRoutes(mux, "v1", api.DevAuthMiddleware())

	req := withAdminCtx(devRequest("POST", "/api/v1/dev/doctype", validDocTypeBody()))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing site context, got %d: %s", w.Code, w.Body.String())
	}
	if len(reg.calls) != 0 {
		t.Errorf("registry should not be called without site, got %d call(s)", len(reg.calls))
	}
}

func TestIntegration_DevAPI_WithSiteContext_PassesSiteToRegistry(t *testing.T) {
	dir := t.TempDir()
	reg := &mockDevRegisterer{}
	h := api.NewDevHandlerWithRegisterer(dir, reg, nil)
	mux := http.NewServeMux()
	h.RegisterDevRoutes(mux, "v1", api.DevAuthMiddleware())

	req := withAdminCtx(devRequest("POST", "/api/v1/dev/doctype", validDocTypeBody()))
	req = req.WithContext(api.WithSite(req.Context(), &tenancy.SiteContext{Name: "acme"}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if len(reg.calls) != 1 || reg.calls[0].site != "acme" {
		t.Errorf("expected Register called with site=acme, got %+v", reg.calls)
	}
}

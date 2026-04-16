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
)

func TestDevHandler_ListApps(t *testing.T) {
	h := api.NewDevHandler(t.TempDir(), nil, nil)
	req := httptest.NewRequest("GET", "/api/v1/dev/apps", nil)
	w := httptest.NewRecorder()

	h.HandleListApps(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Data []struct {
			Name    string   `json:"name"`
			Modules []string `json:"modules"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data == nil {
		t.Fatal("expected non-nil data array")
	}
	if len(resp.Data) != 0 {
		t.Fatalf("expected empty array, got %v", resp.Data)
	}
}

func TestDevHandler_ListApps_WithApps(t *testing.T) {
	dir := t.TempDir()

	// Create two app directories: one with manifest.yaml, one without.
	appWithManifest := filepath.Join(dir, "myapp")
	if err := os.MkdirAll(appWithManifest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appWithManifest, "manifest.yaml"), []byte("name: myapp\nmodules:\n  - name: Selling\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	appWithoutManifest := filepath.Join(dir, "noapp")
	if err := os.MkdirAll(appWithoutManifest, 0o755); err != nil {
		t.Fatal(err)
	}

	h := api.NewDevHandler(dir, nil, nil)
	req := httptest.NewRequest("GET", "/api/v1/dev/apps", nil)
	w := httptest.NewRecorder()

	h.HandleListApps(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Data []struct {
			Name    string   `json:"name"`
			Modules []string `json:"modules"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 app, got %d: %v", len(resp.Data), resp.Data)
	}
	if resp.Data[0].Name != "myapp" {
		t.Fatalf("expected 'myapp', got %q", resp.Data[0].Name)
	}
	if len(resp.Data[0].Modules) != 1 || resp.Data[0].Modules[0] != "Selling" {
		t.Fatalf("expected modules [Selling], got %v", resp.Data[0].Modules)
	}
}

func TestDevHandler_CreateDocType(t *testing.T) {
	dir := t.TempDir()
	h := api.NewDevHandler(dir, nil, nil)

	body := map[string]any{
		"name":   "SalesOrder",
		"app":    "testapp",
		"module": "selling",
		"layout": map[string]any{
			"tabs": []any{
				map[string]any{
					"label": "Details",
					"sections": []any{
						map[string]any{
							"columns": []any{
								map[string]any{
									"fields": []any{"customer_name"},
								},
							},
						},
					},
				},
			},
		},
		"fields": map[string]any{
			"customer_name": map[string]any{
				"field_type": "Data",
				"label":      "Customer Name",
				"name":       "customer_name",
				"required":   true,
			},
		},
		"settings":    map[string]any{},
		"permissions": []any{},
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/v1/dev/doctype", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleCreateDocType(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Verify directory structure was created
	dtDir := filepath.Join(dir, "testapp", "modules", "selling", "doctypes", "sales_order")
	jsonPath := filepath.Join(dtDir, "sales_order.json")
	goPath := filepath.Join(dtDir, "sales_order.go")

	if _, statErr := os.Stat(jsonPath); os.IsNotExist(statErr) {
		t.Fatal("expected sales_order.json to be created")
	}
	if _, statErr := os.Stat(goPath); os.IsNotExist(statErr) {
		t.Fatal("expected sales_order.go stub to be created")
	}

	// Verify JSON content
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var docDef map[string]any
	if err := json.Unmarshal(data, &docDef); err != nil {
		t.Fatalf("invalid JSON on disk: %v", err)
	}
	if docDef["name"] != "SalesOrder" {
		t.Fatalf("expected name 'SalesOrder', got %v", docDef["name"])
	}
}

func TestDevHandler_CreateDocType_InvalidName(t *testing.T) {
	dir := t.TempDir()
	h := api.NewDevHandler(dir, nil, nil)

	body := map[string]any{
		"name":        "sales_order", // invalid: must start with uppercase, no underscores
		"app":         "testapp",
		"module":      "Selling",
		"layout":      map[string]any{"tabs": []any{}},
		"fields":      map[string]any{},
		"settings":    map[string]any{},
		"permissions": []any{},
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/v1/dev/doctype", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleCreateDocType(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDevHandler_UpdateDocType_NotFound(t *testing.T) {
	dir := t.TempDir()
	h := api.NewDevHandler(dir, nil, nil)

	body := map[string]any{
		"app":         "testapp",
		"module":      "selling",
		"layout":      map[string]any{"tabs": []any{}},
		"fields":      map[string]any{},
		"settings":    map[string]any{},
		"permissions": []any{},
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	// Use a mux so PathValue works
	mux := http.NewServeMux()
	h.RegisterDevRoutes(mux, "v1")

	req := httptest.NewRequest("PUT", "/api/v1/dev/doctype/NonExistent", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDevHandler_GetDocType_NotFound(t *testing.T) {
	dir := t.TempDir()
	h := api.NewDevHandler(dir, nil, nil)

	// Use a mux so PathValue works
	mux := http.NewServeMux()
	h.RegisterDevRoutes(mux, "v1")

	req := httptest.NewRequest("GET", "/api/v1/dev/doctype/NonExistent", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDevAuthMiddleware_RejectsNilUser(t *testing.T) {
	mw := api.DevAuthMiddleware()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})
	handler := mw(inner)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestDevAuthMiddleware_RejectsGuestUser(t *testing.T) {
	mw := api.DevAuthMiddleware()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})
	handler := mw(inner)
	req := httptest.NewRequest("GET", "/", nil)
	ctx := api.WithUser(req.Context(), &auth.User{Email: "Guest", Roles: []string{"Guest"}})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestDevAuthMiddleware_RejectsNonAdmin(t *testing.T) {
	mw := api.DevAuthMiddleware()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})
	handler := mw(inner)
	req := httptest.NewRequest("GET", "/", nil)
	ctx := api.WithUser(req.Context(), &auth.User{Email: "user@test.com", Roles: []string{"Editor"}})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestDevAuthMiddleware_AllowsAdmin(t *testing.T) {
	mw := api.DevAuthMiddleware()
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := mw(inner)
	req := httptest.NewRequest("GET", "/", nil)
	ctx := api.WithUser(req.Context(), &auth.User{Email: "admin@test.com", Roles: []string{"Administrator"}})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !called {
		t.Fatal("expected inner handler to be called")
	}
}

func TestDevHandler_CreateDocType_PathTraversal_App(t *testing.T) {
	dir := t.TempDir()
	h := api.NewDevHandler(dir, nil, nil)
	body := map[string]any{
		"name": "Exploit", "app": "../../etc", "module": "core",
		"layout": map[string]any{"tabs": []any{}},
		"fields": map[string]any{"title": map[string]any{"field_type": "Data", "name": "title"}},
		"settings": map[string]any{}, "permissions": []any{},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/dev/doctype", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleCreateDocType(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal in app, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDevHandler_CreateDocType_PathTraversal_Module(t *testing.T) {
	dir := t.TempDir()
	h := api.NewDevHandler(dir, nil, nil)
	body := map[string]any{
		"name": "Exploit", "app": "testapp", "module": "../../../etc",
		"layout": map[string]any{"tabs": []any{}},
		"fields": map[string]any{"title": map[string]any{"field_type": "Data", "name": "title"}},
		"settings": map[string]any{}, "permissions": []any{},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/dev/doctype", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleCreateDocType(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal in module, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDevHandler_UpdateDocType_ValidatesNameFromURL(t *testing.T) {
	dir := t.TempDir()
	h := api.NewDevHandler(dir, nil, nil)
	body := map[string]any{
		"app": "testapp", "module": "core",
		"layout": map[string]any{"tabs": []any{}},
		"fields": map[string]any{}, "settings": map[string]any{}, "permissions": []any{},
	}
	bodyBytes, _ := json.Marshal(body)
	mux := http.NewServeMux()
	h.RegisterDevRoutes(mux, "v1")
	req := httptest.NewRequest("PUT", "/api/v1/dev/doctype/bad_name", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid name from URL, got %d: %s", w.Code, w.Body.String())
	}
}

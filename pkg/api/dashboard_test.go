package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// ── Registry tests ──────────────────────────────────────────────────────────

func TestDashboardRegistry_RegisterAndGet(t *testing.T) {
	reg := NewDashboardRegistry()
	def := DashDef{
		Name:  "sales_overview",
		Label: "Sales Overview",
		Widgets: []DashWidget{
			{Type: "number_card", Config: map[string]any{"doctype": "Order", "aggregate": "count"}},
			{Type: "shortcut", Config: map[string]any{"label": "Orders", "url": "/desk/app/Order"}},
		},
	}

	if err := reg.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, ok := reg.Get("sales_overview")
	if !ok {
		t.Fatal("Get returned false for registered dashboard")
	}
	if got.Name != "sales_overview" || got.Label != "Sales Overview" {
		t.Fatalf("Get returned wrong def: %+v", got)
	}
	if len(got.Widgets) != 2 {
		t.Fatalf("expected 2 widgets, got %d", len(got.Widgets))
	}

	// Duplicate registration.
	if err := reg.Register(def); err == nil {
		t.Fatal("expected error on duplicate registration")
	}

	// Empty name.
	if err := reg.Register(DashDef{}); err == nil {
		t.Fatal("expected error on empty name")
	}
}

func TestDashboardRegistry_GetMissing(t *testing.T) {
	reg := NewDashboardRegistry()
	_, ok := reg.Get("nonexistent")
	if ok {
		t.Fatal("expected false for missing dashboard")
	}
}

// ── Handler helpers ─────────────────────────────────────────────────────────

func newTestDashboardRegistry() *DashboardRegistry {
	reg := NewDashboardRegistry()
	_ = reg.Register(DashDef{
		Name:  "test_dash",
		Label: "Test Dashboard",
		Widgets: []DashWidget{
			{Type: "number_card", Config: map[string]any{"doctype": "Order", "aggregate": "count", "label": "Total Orders"}},
			{Type: "shortcut", Config: map[string]any{"label": "Orders", "url": "/desk/app/Order"}},
			{Type: "list", Config: map[string]any{"doctype": "Order", "limit": float64(5)}},
		},
	})
	return reg
}

func newTestDashboardHandler() *DashboardHandler {
	return &DashboardHandler{
		dashboards: newTestDashboardRegistry(),
		perm:       AllowAllPermissionChecker{},
		logger:     slog.Default(),
	}
}

func dashRequest(path string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	return req
}

// ── Handler: GetDashboard ───────────────────────────────────────────────────

func TestDashboardHandler_GetDashboard_RequiresSite(t *testing.T) {
	h := newTestDashboardHandler()
	req := dashRequest("/api/v1/dashboard/test_dash")
	req.SetPathValue("name", "test_dash")
	rr := httptest.NewRecorder()
	h.handleGetDashboard(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDashboardHandler_GetDashboard_RequiresAuth(t *testing.T) {
	h := newTestDashboardHandler()
	req := dashRequest("/api/v1/dashboard/test_dash")
	req.SetPathValue("name", "test_dash")
	ctx := WithSite(req.Context(), &tenancy.SiteContext{Name: "testsite"})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.handleGetDashboard(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestDashboardHandler_GetDashboard_NotFound(t *testing.T) {
	h := newTestDashboardHandler()
	req := dashRequest("/api/v1/dashboard/nonexistent")
	req.SetPathValue("name", "nonexistent")
	req = withSiteAndUser(req)
	rr := httptest.NewRecorder()
	h.handleGetDashboard(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestDashboardHandler_GetDashboard_Success(t *testing.T) {
	h := newTestDashboardHandler()
	req := dashRequest("/api/v1/dashboard/test_dash")
	req.SetPathValue("name", "test_dash")
	req = withSiteAndUser(req)
	rr := httptest.NewRecorder()
	h.handleGetDashboard(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data DashDef `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Data.Name != "test_dash" {
		t.Fatalf("expected name test_dash, got %s", resp.Data.Name)
	}
	if resp.Data.Label != "Test Dashboard" {
		t.Fatalf("expected label Test Dashboard, got %s", resp.Data.Label)
	}
	if len(resp.Data.Widgets) != 3 {
		t.Fatalf("expected 3 widgets, got %d", len(resp.Data.Widgets))
	}
}

// ── Handler: GetWidget ──────────────────────────────────────────────────────

func TestDashboardHandler_GetWidget_RequiresSite(t *testing.T) {
	h := newTestDashboardHandler()
	req := dashRequest("/api/v1/dashboard/test_dash/widget/0")
	req.SetPathValue("name", "test_dash")
	req.SetPathValue("idx", "0")
	rr := httptest.NewRecorder()
	h.handleGetWidget(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDashboardHandler_GetWidget_DashboardNotFound(t *testing.T) {
	h := newTestDashboardHandler()
	req := dashRequest("/api/v1/dashboard/nonexistent/widget/0")
	req.SetPathValue("name", "nonexistent")
	req.SetPathValue("idx", "0")
	req = withSiteAndUser(req)
	rr := httptest.NewRecorder()
	h.handleGetWidget(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestDashboardHandler_GetWidget_OutOfBounds(t *testing.T) {
	h := newTestDashboardHandler()
	req := dashRequest("/api/v1/dashboard/test_dash/widget/99")
	req.SetPathValue("name", "test_dash")
	req.SetPathValue("idx", "99")
	req = withSiteAndUser(req)
	rr := httptest.NewRecorder()
	h.handleGetWidget(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDashboardHandler_GetWidget_InvalidIndex(t *testing.T) {
	h := newTestDashboardHandler()
	req := dashRequest("/api/v1/dashboard/test_dash/widget/abc")
	req.SetPathValue("name", "test_dash")
	req.SetPathValue("idx", "abc")
	req = withSiteAndUser(req)
	rr := httptest.NewRecorder()
	h.handleGetWidget(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDashboardHandler_GetWidget_Shortcut(t *testing.T) {
	h := newTestDashboardHandler()
	req := dashRequest("/api/v1/dashboard/test_dash/widget/1")
	req.SetPathValue("name", "test_dash")
	req.SetPathValue("idx", "1") // shortcut widget
	req = withSiteAndUser(req)
	rr := httptest.NewRecorder()
	h.handleGetWidget(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Data["type"] != "shortcut" {
		t.Fatalf("expected type shortcut, got %v", resp.Data["type"])
	}
}

func TestDashboardHandler_GetWidget_NumberCard_NoDB(t *testing.T) {
	h := newTestDashboardHandler()
	// db is nil — number_card widget needs DB, so we expect 500
	req := dashRequest("/api/v1/dashboard/test_dash/widget/0")
	req.SetPathValue("name", "test_dash")
	req.SetPathValue("idx", "0") // number_card widget
	req = withSiteAndUser(req)
	rr := httptest.NewRecorder()
	h.handleGetWidget(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 (no db), got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDashboardHandler_GetWidget_PermissionDenied(t *testing.T) {
	h := &DashboardHandler{
		dashboards: newTestDashboardRegistry(),
		perm:       denyPermChecker{},
		logger:     slog.Default(),
	}
	req := dashRequest("/api/v1/dashboard/test_dash/widget/0")
	req.SetPathValue("name", "test_dash")
	req.SetPathValue("idx", "0") // number_card widget
	req = withSiteAndUser(req)
	rr := httptest.NewRecorder()
	h.handleGetWidget(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ensure packages are imported
var _ = (*auth.User)(nil)
var _ = (*tenancy.SiteContext)(nil)

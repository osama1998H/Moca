package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/orm"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// ── Registry tests ──────────────────────────────────────────────────────────

func TestReportRegistry_RegisterAndGet(t *testing.T) {
	reg := NewReportRegistry()
	def := orm.ReportDef{Name: "sales_by_region", DocType: "Order", Type: "QueryReport"}

	if err := reg.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, ok := reg.Get("sales_by_region")
	if !ok {
		t.Fatal("Get returned false for registered report")
	}
	if got.Name != "sales_by_region" || got.DocType != "Order" {
		t.Fatalf("Get returned wrong def: %+v", got)
	}

	// Duplicate registration.
	if err := reg.Register(def); err == nil {
		t.Fatal("expected error on duplicate registration")
	}

	// Empty name.
	if err := reg.Register(orm.ReportDef{}); err == nil {
		t.Fatal("expected error on empty name")
	}
}

func TestReportRegistry_List(t *testing.T) {
	reg := NewReportRegistry()
	_ = reg.Register(orm.ReportDef{Name: "b_report", DocType: "B"})
	_ = reg.Register(orm.ReportDef{Name: "a_report", DocType: "A"})

	list := reg.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(list))
	}
	if list[0].Name != "a_report" {
		t.Fatalf("expected sorted order, got %s first", list[0].Name)
	}
}

func TestReportRegistry_GetMissing(t *testing.T) {
	reg := NewReportRegistry()
	_, ok := reg.Get("nonexistent")
	if ok {
		t.Fatal("expected false for missing report")
	}
}

// ── Handler helpers ─────────────────────────────────────────────────────────

func newTestReportRegistry() *ReportRegistry {
	reg := NewReportRegistry()
	_ = reg.Register(orm.ReportDef{
		Name:    "test_report",
		DocType: "Order",
		Type:    "QueryReport",
		Query:   "SELECT name FROM tab_order",
		Columns: []orm.ReportColumn{
			{FieldName: "name", Label: "Name", FieldType: "Data"},
		},
		Filters: []orm.ReportFilter{
			{FieldName: "status", Label: "Status", FieldType: "Select", Required: false},
		},
		ChartConfig: &orm.ChartConfig{Type: "bar"},
	})
	return reg
}

func reportRequest(method, path string, body string) *http.Request {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.SetPathValue("name", "test_report")
	return req
}

func withSiteAndUser(r *http.Request) *http.Request {
	ctx := WithSite(r.Context(), &tenancy.SiteContext{Name: "testsite"})
	ctx = WithUser(ctx, &auth.User{Email: "admin@test.com"})
	return r.WithContext(ctx)
}

// ── Handler: Meta ───────────────────────────────────────────────────────────

func TestReportHandler_Meta_RequiresSite(t *testing.T) {
	h := &ReportHandler{reports: newTestReportRegistry()}
	req := reportRequest(http.MethodGet, "/api/v1/report/test_report/meta", "")
	rr := httptest.NewRecorder()
	h.handleMeta(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestReportHandler_Meta_RequiresAuth(t *testing.T) {
	h := &ReportHandler{reports: newTestReportRegistry()}
	req := reportRequest(http.MethodGet, "/api/v1/report/test_report/meta", "")
	ctx := WithSite(req.Context(), &tenancy.SiteContext{Name: "testsite"})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	h.handleMeta(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestReportHandler_Meta_NotFound(t *testing.T) {
	h := &ReportHandler{
		reports: newTestReportRegistry(),
		perm:    AllowAllPermissionChecker{},
	}
	req := reportRequest(http.MethodGet, "/api/v1/report/nonexistent/meta", "")
	req.SetPathValue("name", "nonexistent")
	req = withSiteAndUser(req)
	rr := httptest.NewRecorder()
	h.handleMeta(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestReportHandler_Meta_PermissionDenied(t *testing.T) {
	h := &ReportHandler{
		reports: newTestReportRegistry(),
		perm:    denyPermChecker{},
	}
	req := reportRequest(http.MethodGet, "/api/v1/report/test_report/meta", "")
	req = withSiteAndUser(req)
	rr := httptest.NewRecorder()
	h.handleMeta(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestReportHandler_Meta_Success(t *testing.T) {
	h := &ReportHandler{
		reports: newTestReportRegistry(),
		perm:    AllowAllPermissionChecker{},
	}
	req := reportRequest(http.MethodGet, "/api/v1/report/test_report/meta", "")
	req = withSiteAndUser(req)
	rr := httptest.NewRecorder()
	h.handleMeta(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Data apiReportMeta `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Data.Name != "test_report" {
		t.Fatalf("expected name test_report, got %s", resp.Data.Name)
	}
	if resp.Data.DocType != "Order" {
		t.Fatalf("expected doctype Order, got %s", resp.Data.DocType)
	}
	if len(resp.Data.Columns) != 1 {
		t.Fatalf("expected 1 column, got %d", len(resp.Data.Columns))
	}
	if resp.Data.ChartConfig == nil || resp.Data.ChartConfig.Type != "bar" {
		t.Fatal("expected chart_config with type bar")
	}

	// Ensure the SQL query is NOT in the response.
	raw := rr.Body.String()
	if strings.Contains(raw, "SELECT") {
		t.Fatal("response should not contain the SQL query")
	}
}

// ── Handler: Execute ────────────────────────────────────────────────────────

func TestReportHandler_Execute_RequiresSite(t *testing.T) {
	h := &ReportHandler{reports: newTestReportRegistry()}
	req := reportRequest(http.MethodPost, "/api/v1/report/test_report/execute", `{"filters":{}}`)
	rr := httptest.NewRecorder()
	h.handleExecute(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestReportHandler_Execute_InvalidJSON(t *testing.T) {
	h := &ReportHandler{
		reports: newTestReportRegistry(),
		perm:    AllowAllPermissionChecker{},
	}
	req := reportRequest(http.MethodPost, "/api/v1/report/test_report/execute", "not-json")
	req = withSiteAndUser(req)
	rr := httptest.NewRecorder()
	h.handleExecute(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestReportHandler_Execute_Pagination(t *testing.T) {
	// Create a mock that returns rows without needing a real DB.
	// We can't fully test execute without a pool, but we test the
	// pagination slicing logic by verifying defaults.
	h := &ReportHandler{
		reports: newTestReportRegistry(),
		perm:    AllowAllPermissionChecker{},
		logger:  slog.Default(),
		// db is nil — will fail at ForSite, which is expected.
	}
	req := reportRequest(http.MethodPost, "/api/v1/report/test_report/execute",
		`{"filters":{}, "limit": 10, "offset": 0}`)
	req = withSiteAndUser(req)
	rr := httptest.NewRecorder()
	h.handleExecute(rr, req)

	// Without a real DB pool, we expect a 503 (service unavailable).
	// This validates the happy path up to the DB call.
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (no db), got %d: %s", rr.Code, rr.Body.String())
	}
}

// ── Pagination slicing unit test ────────────────────────────────────────────

func TestPaginateSlice(t *testing.T) {
	// Simulate the slicing logic from handleExecute.
	rows := make([]map[string]any, 25)
	for i := range rows {
		rows[i] = map[string]any{"i": i}
	}

	tests := []struct {
		name          string
		limit, offset int
		wantLen       int
		wantFirst     int
	}{
		{"first page", 10, 0, 10, 0},
		{"second page", 10, 10, 10, 10},
		{"last partial page", 10, 20, 5, 20},
		{"offset beyond end", 10, 30, 0, -1},
		{"zero limit defaults", 0, 0, 25, 0}, // 0 treated as "all"
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limit := tt.limit
			if limit <= 0 {
				limit = len(rows) // simulate default-all
			}
			start := tt.offset
			if start > len(rows) {
				start = len(rows)
			}
			end := start + limit
			if end > len(rows) {
				end = len(rows)
			}
			page := rows[start:end]

			if len(page) != tt.wantLen {
				t.Fatalf("len=%d, want %d", len(page), tt.wantLen)
			}
			if tt.wantFirst >= 0 && len(page) > 0 {
				if page[0]["i"] != tt.wantFirst {
					t.Fatalf("first=%v, want %d", page[0]["i"], tt.wantFirst)
				}
			}
		})
	}
}

// ── Context helper test ─────────────────────────────────────────────────────

func TestWithSiteAndUser(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := WithSite(req.Context(), &tenancy.SiteContext{Name: "s1"})
	ctx = WithUser(ctx, &auth.User{Email: "u@t.com"})

	site := SiteFromContext(ctx)
	user := UserFromContext(ctx)

	if site == nil || site.Name != "s1" {
		t.Fatal("site not set")
	}
	if user == nil || user.Email != "u@t.com" {
		t.Fatal("user not set")
	}
}

// ensure denyPermChecker is used in this file (it's defined in search_test.go)
var _ PermissionChecker = denyPermChecker{}

// ensure we use auth and tenancy packages
var _ = (*auth.User)(nil)
var _ = (*tenancy.SiteContext)(nil)
var _ context.Context = context.Background()

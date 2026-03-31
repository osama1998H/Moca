package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/moca-framework/moca/pkg/document"
	"github.com/moca-framework/moca/pkg/meta"
)

// ── extractVersionFromPath ─────────────────────────────────────────────────

func TestExtractVersionFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/api/v1/resource/Item", "v1"},
		{"/api/v2/resource/Item/ITEM-001", "v2"},
		{"/api/v10/meta/Item", "v10"},
		{"/api/v1/", "v1"},
		{"/health", ""},
		{"/api/", ""},
		{"/other/v1/foo", ""},
		{"/api/resource/Item", ""},
	}
	for _, tc := range tests {
		got := extractVersionFromPath(tc.path)
		if got != tc.want {
			t.Errorf("extractVersionFromPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// ── VersionRouter.Middleware ────────────────────────────────────────────────

func TestVersionMiddleware_SetsContext(t *testing.T) {
	handler := newHandler(nil, defaultMockMeta())
	vr := NewVersionRouter(handler, nil)

	var gotVersion string
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotVersion = APIVersionFromContext(r.Context())
	})

	mw := vr.Middleware()(inner)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/resource/TestItem", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, r)

	if gotVersion != "v1" {
		t.Errorf("version = %q, want v1", gotVersion)
	}
}

func TestVersionMiddleware_DeprecatedHeaders(t *testing.T) {
	handler := newHandler(nil, defaultMockMeta())
	vr := NewVersionRouter(handler, nil)

	sunset := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	vr.ConfigureVersions([]meta.APIVersion{
		{Version: "v1", Status: "deprecated", SunsetDate: &sunset},
		{Version: "v2", Status: "active"},
	})

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := vr.Middleware()(inner)

	// v1 is deprecated — expect headers.
	r := httptest.NewRequest(http.MethodGet, "/api/v1/resource/TestItem", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if w.Header().Get("Deprecation") != "true" {
		t.Error("expected Deprecation: true header")
	}
	if w.Header().Get("Sunset") == "" {
		t.Error("expected Sunset header")
	}

	// v2 is active — no deprecation headers.
	r = httptest.NewRequest(http.MethodGet, "/api/v2/resource/TestItem", nil)
	w = httptest.NewRecorder()
	mw.ServeHTTP(w, r)

	if w.Header().Get("Deprecation") != "" {
		t.Error("v2 should not have Deprecation header")
	}
}

func TestVersionMiddleware_SunsetReturns410(t *testing.T) {
	handler := newHandler(nil, defaultMockMeta())
	vr := NewVersionRouter(handler, nil)

	vr.ConfigureVersions([]meta.APIVersion{
		{Version: "v1", Status: "sunset"},
		{Version: "v2", Status: "active"},
	})

	called := false
	inner := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	})

	mw := vr.Middleware()(inner)

	r := httptest.NewRequest(http.MethodGet, "/api/v1/resource/TestItem", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, r)

	if w.Code != http.StatusGone {
		t.Fatalf("status = %d, want 410", w.Code)
	}
	if called {
		t.Error("handler should not be called for sunset version")
	}

	var env errorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Error.Code != "API_VERSION_SUNSET" {
		t.Errorf("error code = %q, want API_VERSION_SUNSET", env.Error.Code)
	}
}

func TestVersionMiddleware_DefaultV1(t *testing.T) {
	handler := newHandler(nil, defaultMockMeta())
	vr := NewVersionRouter(handler, nil) // default: v1 only

	var gotVersion string
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotVersion = APIVersionFromContext(r.Context())
	})

	mw := vr.Middleware()(inner)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/resource/TestItem", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, r)

	if gotVersion != "v1" {
		t.Errorf("version = %q, want v1", gotVersion)
	}
	if w.Header().Get("Deprecation") != "" {
		t.Error("default v1 should not have Deprecation header")
	}
}

func TestVersionMiddleware_NonAPIPathPassesThrough(t *testing.T) {
	handler := newHandler(nil, defaultMockMeta())
	vr := NewVersionRouter(handler, nil)

	called := false
	inner := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	})

	mw := vr.Middleware()(inner)
	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, r)

	if !called {
		t.Error("non-API paths should pass through to next handler")
	}
}

// ── VersionRouter.RegisterRoutes ───────────────────────────────────────────

func TestVersionRouter_RegistersMultipleVersionRoutes(t *testing.T) {
	crud := &mockCRUD{
		getFn: func(_ *document.DocContext, _, _ string) (*document.DynamicDoc, error) {
			return testDoc(), nil
		},
	}
	handler := newHandler(crud, defaultMockMeta())
	vr := NewVersionRouter(handler, nil)
	vr.ConfigureVersions([]meta.APIVersion{
		{Version: "v1", Status: "active"},
		{Version: "v2", Status: "active"},
	})

	mux := http.NewServeMux()
	vr.RegisterRoutes(mux)

	// Both v1 and v2 should have routes registered.
	for _, version := range []string{"v1", "v2"} {
		r := httptest.NewRequest(http.MethodGet, "/api/"+version+"/resource/TestItem/ITEM-001", nil)
		r = contextWithSiteAndUser(r)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", version, w.Code)
		}
	}
}

// ── ConfigureVersions ──────────────────────────────────────────────────────

func TestConfigureVersions_EmptyKeepsDefault(t *testing.T) {
	handler := newHandler(nil, defaultMockMeta())
	vr := NewVersionRouter(handler, nil)
	vr.ConfigureVersions(nil) // empty

	if _, ok := vr.versions["v1"]; !ok {
		t.Error("empty ConfigureVersions should keep default v1")
	}
}

func TestConfigureVersions_ReplacesDefault(t *testing.T) {
	handler := newHandler(nil, defaultMockMeta())
	vr := NewVersionRouter(handler, nil)
	vr.ConfigureVersions([]meta.APIVersion{
		{Version: "v2", Status: "active"},
		{Version: "v3", Status: "active"},
	})

	if _, ok := vr.versions["v1"]; ok {
		t.Error("ConfigureVersions should replace default v1")
	}
	if _, ok := vr.versions["v2"]; !ok {
		t.Error("v2 should be configured")
	}
	if _, ok := vr.versions["v3"]; !ok {
		t.Error("v3 should be configured")
	}
}

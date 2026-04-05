package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/observe"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// --- requestIDMiddleware ---

func TestRequestIDMiddleware_GeneratesID(t *testing.T) {
	logger := observe.NewLogger(0)
	handler := requestIDMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := RequestIDFromContext(r.Context())
		if id == "" {
			t.Error("expected non-empty request ID in context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("X-Request-ID"); got == "" {
		t.Error("expected X-Request-ID response header")
	}
}

func TestRequestIDMiddleware_ReusesExisting(t *testing.T) {
	logger := observe.NewLogger(0)
	handler := requestIDMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := RequestIDFromContext(r.Context())
		if id != "custom-id-123" {
			t.Errorf("RequestID = %q, want %q", id, "custom-id-123")
		}
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "custom-id-123")
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("X-Request-ID"); got != "custom-id-123" {
		t.Errorf("X-Request-ID = %q, want %q", got, "custom-id-123")
	}
}

// --- corsMiddleware ---

func TestCORSMiddleware_SetsHeaders(t *testing.T) {
	cfg := CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		MaxAge:         3600,
	}
	handler := corsMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("Allow-Origin = %q, want %q", got, "https://app.example.com")
	}
	if got := rr.Header().Get("Access-Control-Max-Age"); got != "3600" {
		t.Errorf("Max-Age = %q, want %q", got, "3600")
	}
}

func TestCORSMiddleware_DisallowedOrigin(t *testing.T) {
	cfg := CORSConfig{AllowedOrigins: []string{"https://allowed.com"}}
	handler := corsMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no Allow-Origin header, got %q", got)
	}
}

func TestCORSMiddleware_Preflight(t *testing.T) {
	cfg := CORSConfig{AllowedOrigins: []string{"*"}}
	handler := corsMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for preflight")
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://any.com")
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusNoContent)
	}
}

// --- tenantMiddleware ---

type mockSiteResolver struct {
	sites  map[string]*tenancy.SiteContext
	errors map[string]error // optional per-site errors
}

func (m *mockSiteResolver) ResolveSite(_ context.Context, siteID string) (*tenancy.SiteContext, error) {
	if m.errors != nil {
		if err, ok := m.errors[siteID]; ok {
			return nil, err
		}
	}
	s, ok := m.sites[siteID]
	if !ok {
		return nil, errors.New("not found")
	}
	return s, nil
}

func TestTenantMiddleware_FromHeader(t *testing.T) {
	resolver := &mockSiteResolver{
		sites: map[string]*tenancy.SiteContext{
			"acme": {Name: "acme"},
		},
	}
	handler := tenantMiddleware(resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := SiteFromContext(r.Context())
		if s == nil || s.Name != "acme" {
			t.Errorf("site = %v, want acme", s)
		}
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Moca-Site", "acme")
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestTenantMiddleware_FromSubdomain(t *testing.T) {
	resolver := &mockSiteResolver{
		sites: map[string]*tenancy.SiteContext{
			"acme": {Name: "acme"},
		},
	}
	handler := tenantMiddleware(resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := SiteFromContext(r.Context())
		if s == nil || s.Name != "acme" {
			t.Errorf("site = %v, want acme", s)
		}
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://acme.example.com/api", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestTenantMiddleware_MissingSite(t *testing.T) {
	resolver := &mockSiteResolver{sites: map[string]*tenancy.SiteContext{}}
	handler := tenantMiddleware(resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestTenantMiddleware_SiteNotFound(t *testing.T) {
	resolver := &mockSiteResolver{sites: map[string]*tenancy.SiteContext{}}
	handler := tenantMiddleware(resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Moca-Site", "nonexistent")
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

// --- authMiddleware ---

func TestAuthMiddleware_SetsUser(t *testing.T) {
	handler := authMiddleware(NoopAuthenticator{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil || u.Email != "Guest" {
			t.Errorf("user = %v, want Guest", u)
		}
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// Set a request ID so the re-enrichment works.
	req = req.WithContext(WithRequestID(req.Context(), "test-id"))
	handler.ServeHTTP(rr, req)
}

type failingAuth struct{}

func (failingAuth) Authenticate(_ *http.Request) (*auth.User, error) {
	return nil, errors.New("bad token")
}

func TestAuthMiddleware_Failure(t *testing.T) {
	handler := authMiddleware(failingAuth{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

// --- subdomainFromHost ---

func TestSubdomainFromHost(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{"acme.example.com", "acme"},
		{"acme.example.com:8080", "acme"},
		{"example.com", ""},
		{"localhost", ""},
		{"localhost:8000", ""},
		{"a.b.c.example.com", "a"},
		// *.localhost special case for local development.
		{"acme.localhost", "acme"},
		{"acme.localhost:8000", "acme"},
	}
	for _, tt := range tests {
		got := subdomainFromHost(tt.host)
		if got != tt.want {
			t.Errorf("subdomainFromHost(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}

// --- siteFromPath ---

func TestSiteFromPath(t *testing.T) {
	tests := []struct {
		path      string
		wantSite  string
		wantStrip string
	}{
		{"/sites/acme/api/v1/resource/SalesOrder", "acme", "/api/v1/resource/SalesOrder"},
		{"/sites/globex/api/v1/resource/X", "globex", "/api/v1/resource/X"},
		{"/sites/acme", "acme", "/"},
		{"/sites/", "", ""},
		{"/api/v1/resource/X", "", ""},
		{"/other/path", "", ""},
		{"", "", ""},
	}
	for _, tt := range tests {
		site, stripped := siteFromPath(tt.path)
		if site != tt.wantSite || stripped != tt.wantStrip {
			t.Errorf("siteFromPath(%q) = (%q, %q), want (%q, %q)",
				tt.path, site, stripped, tt.wantSite, tt.wantStrip)
		}
	}
}

// --- tenantMiddleware: path-based ---

func TestTenantMiddleware_PathBased(t *testing.T) {
	resolver := &mockSiteResolver{
		sites: map[string]*tenancy.SiteContext{
			"acme": {Name: "acme"},
		},
	}
	handler := tenantMiddleware(resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := SiteFromContext(r.Context())
		if s == nil || s.Name != "acme" {
			t.Errorf("site = %v, want acme", s)
		}
		// Verify path was rewritten.
		if r.URL.Path != "/api/v1/resource/SalesOrder" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/api/v1/resource/SalesOrder")
		}
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sites/acme/api/v1/resource/SalesOrder", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

// --- tenantMiddleware: disabled site ---

func TestTenantMiddleware_DisabledSite503(t *testing.T) {
	resolver := &mockSiteResolver{
		sites: map[string]*tenancy.SiteContext{},
		errors: map[string]error{
			"maintenance": tenancy.ErrSiteDisabled,
		},
	}
	handler := tenantMiddleware(resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for disabled site")
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Moca-Site", "maintenance")
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
	if body := rr.Body.String(); !strings.Contains(body, "SITE_DISABLED") {
		t.Errorf("body = %q, want SITE_DISABLED error code", body)
	}
}

// --- tenantMiddleware: resolution priority ---

func TestTenantMiddleware_ResolutionPriority(t *testing.T) {
	resolver := &mockSiteResolver{
		sites: map[string]*tenancy.SiteContext{
			"header-site":    {Name: "header-site"},
			"path-site":      {Name: "path-site"},
			"subdomain-site": {Name: "subdomain-site"},
		},
	}

	// Header beats path and subdomain.
	t.Run("header beats path and subdomain", func(t *testing.T) {
		handler := tenantMiddleware(resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s := SiteFromContext(r.Context())
			if s.Name != "header-site" {
				t.Errorf("site = %q, want %q", s.Name, "header-site")
			}
		}))
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/sites/path-site/api/v1/resource/X", nil)
		req.Host = "subdomain-site.example.com"
		req.Header.Set("X-Moca-Site", "header-site")
		handler.ServeHTTP(rr, req)
	})

	// Path beats subdomain.
	t.Run("path beats subdomain", func(t *testing.T) {
		handler := tenantMiddleware(resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s := SiteFromContext(r.Context())
			if s.Name != "path-site" {
				t.Errorf("site = %q, want %q", s.Name, "path-site")
			}
		}))
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/sites/path-site/api/v1/resource/X", nil)
		req.Host = "subdomain-site.example.com"
		handler.ServeHTTP(rr, req)
	})
}

// --- tenantMiddleware: skip paths ---

func TestTenantMiddleware_SkipsDeskPath(t *testing.T) {
	resolver := &mockSiteResolver{sites: map[string]*tenancy.SiteContext{}}
	var called bool
	handler := tenantMiddleware(resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		// No site header, no subdomain — should still reach handler.
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/desk/index.html", nil)
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("handler should be called for /desk/ paths")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestTenantMiddleware_SkipsHealthPath(t *testing.T) {
	resolver := &mockSiteResolver{sites: map[string]*tenancy.SiteContext{}}
	var called bool
	handler := tenantMiddleware(resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("handler should be called for /health")
	}
}

func TestTenantMiddleware_SkipsWebSocketPath(t *testing.T) {
	resolver := &mockSiteResolver{sites: map[string]*tenancy.SiteContext{}}
	var called bool
	handler := tenantMiddleware(resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("handler should be called for /ws")
	}
}

// --- authMiddleware: skip paths ---

func TestAuthMiddleware_SkipsDeskPath(t *testing.T) {
	var called bool
	handler := authMiddleware(failingAuth{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/desk/app.js", nil)
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("handler should be called for /desk/ paths even with failing auth")
	}
}

func TestAuthMiddleware_SkipsHealthPath(t *testing.T) {
	var called bool
	handler := authMiddleware(failingAuth{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("handler should be called for /health even with failing auth")
	}
}

// --- siteFromPath edge cases ---

func TestSiteFromPath_EdgeCases(t *testing.T) {
	tests := []struct {
		path      string
		wantSite  string
		wantStrip string
	}{
		// Sites with special characters in name.
		{"/sites/acme-corp/api/v1/resource/X", "acme-corp", "/api/v1/resource/X"},
		{"/sites/site_123/api", "site_123", "/api"},
		// Double slashes.
		{"/sites/acme//api", "acme", "//api"},
		// Only site name, no trailing slash.
		{"/sites/acme", "acme", "/"},
		// Prefix but missing site name.
		{"/sites/", "", ""},
		// Not the prefix at all.
		{"/api/sites/acme", "", ""},
		// Case sensitivity.
		{"/Sites/acme/api", "", ""},
		{"/SITES/acme/api", "", ""},
	}
	for _, tt := range tests {
		site, stripped := siteFromPath(tt.path)
		if site != tt.wantSite || stripped != tt.wantStrip {
			t.Errorf("siteFromPath(%q) = (%q, %q), want (%q, %q)",
				tt.path, site, stripped, tt.wantSite, tt.wantStrip)
		}
	}
}

// --- subdomainFromHost edge cases ---

func TestSubdomainFromHost_EdgeCases(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		// IP address.
		{"192.168.1.1", "192"},
		{"192.168.1.1:8080", "192"},
		// Empty string.
		{"", ""},
		// Only port.
		{":8080", ""},
		// Multi-level subdomains.
		{"a.b.c.d.example.com", "a"},
		// Localhost variations.
		{"localhost", ""},
		{"localhost:3000", ""},
		{"site1.localhost", "site1"},
		{"site1.localhost:3000", "site1"},
		// Two-part domain (no subdomain).
		{"example.com", ""},
		{"example.com:443", ""},
	}
	for _, tt := range tests {
		got := subdomainFromHost(tt.host)
		if got != tt.want {
			t.Errorf("subdomainFromHost(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}

// --- CORS edge cases ---

func TestCORSMiddleware_WildcardOrigin(t *testing.T) {
	cfg := CORSConfig{AllowedOrigins: []string{"*"}}
	handler := corsMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://anything.example.com")
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://anything.example.com" {
		t.Errorf("Allow-Origin = %q, want %q", got, "https://anything.example.com")
	}
}

func TestCORSMiddleware_NoOriginHeader(t *testing.T) {
	cfg := CORSConfig{AllowedOrigins: []string{"*"}}
	handler := corsMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No Origin header.
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no Allow-Origin header without Origin, got %q", got)
	}
}

func TestCORSMiddleware_MaxAgeZero(t *testing.T) {
	cfg := CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		MaxAge:         0, // should omit header
	}
	handler := corsMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Max-Age"); got != "" {
		t.Errorf("expected no Max-Age header for 0 value, got %q", got)
	}
}

func TestCORSMiddleware_CustomMethodsAndHeaders(t *testing.T) {
	cfg := CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "PATCH"},
		AllowedHeaders: []string{"X-Custom"},
	}
	handler := corsMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Methods"); got != "GET, PATCH" {
		t.Errorf("Allow-Methods = %q, want %q", got, "GET, PATCH")
	}
	if got := rr.Header().Get("Access-Control-Allow-Headers"); got != "X-Custom" {
		t.Errorf("Allow-Headers = %q, want %q", got, "X-Custom")
	}
}

// --- originAllowed ---

func TestOriginAllowed(t *testing.T) {
	tests := []struct {
		origin  string
		allowed []string
		want    bool
	}{
		{"https://example.com", []string{"https://example.com"}, true},
		{"https://evil.com", []string{"https://example.com"}, false},
		{"https://any.com", []string{"*"}, true},
		{"https://example.com", nil, false},
		{"https://example.com", []string{}, false},
		{"", []string{"*"}, true},
	}
	for _, tt := range tests {
		got := originAllowed(tt.origin, tt.allowed)
		if got != tt.want {
			t.Errorf("originAllowed(%q, %v) = %v, want %v", tt.origin, tt.allowed, got, tt.want)
		}
	}
}

// --- generateRequestID ---

func TestGenerateRequestID_Unique(t *testing.T) {
	ids := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		id := generateRequestID()
		if len(id) != 32 { // 16 bytes = 32 hex chars
			t.Errorf("ID length = %d, want 32", len(id))
		}
		if _, exists := ids[id]; exists {
			t.Errorf("duplicate ID generated: %s", id)
		}
		ids[id] = struct{}{}
	}
}

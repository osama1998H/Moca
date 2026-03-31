package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moca-framework/moca/pkg/tenancy"
)

func TestNewGateway_Defaults(t *testing.T) {
	g := NewGateway()
	if g.mux == nil {
		t.Error("mux should not be nil")
	}
	if g.auth == nil {
		t.Error("auth should default to NoopAuthenticator")
	}
	if g.perm == nil {
		t.Error("perm should default to AllowAllPermissionChecker")
	}
}

func TestNewGateway_WithOptions(t *testing.T) {
	logger := slog.Default()
	g := NewGateway(
		WithLogger(logger),
		WithCORS(CORSConfig{AllowedOrigins: []string{"*"}}),
	)
	if g.logger != logger {
		t.Error("expected custom logger")
	}
	if len(g.cors.AllowedOrigins) != 1 {
		t.Error("expected CORS config applied")
	}
}

func TestGateway_Handler_MiddlewareChain(t *testing.T) {
	resolver := &mockSiteResolver{
		sites: map[string]*tenancy.SiteContext{
			"test-site": {Name: "test-site"},
		},
	}

	g := NewGateway(
		WithSiteResolver(resolver),
		WithCORS(CORSConfig{AllowedOrigins: []string{"*"}}),
		WithLogger(slog.Default()),
	)

	// Register a handler that verifies context is populated.
	g.Mux().HandleFunc("GET /test", func(w http.ResponseWriter, r *http.Request) {
		if RequestIDFromContext(r.Context()) == "" {
			t.Error("expected request ID in context")
		}
		if SiteFromContext(r.Context()) == nil {
			t.Error("expected site in context")
		}
		if UserFromContext(r.Context()) == nil {
			t.Error("expected user in context")
		}
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/test", nil)
	req.Header.Set("X-Moca-Site", "test-site")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if resp.Header.Get("X-Request-ID") == "" {
		t.Error("expected X-Request-ID response header")
	}
}

func TestGateway_Handler_MissingSiteResolver(t *testing.T) {
	// Gateway with nil SiteResolver should panic-safe — tenantMiddleware
	// receives nil resolver, so any request without a site returns error.
	// Actually, we need a resolver. Let's test that missing tenant returns error.
	resolver := &mockSiteResolver{sites: map[string]*tenancy.SiteContext{}}
	g := NewGateway(
		WithSiteResolver(resolver),
		WithLogger(slog.Default()),
	)
	g.Mux().HandleFunc("GET /test", func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	g.Handler().ServeHTTP(rr, req)

	// No site header and localhost has no subdomain → 400
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestGateway_Mux_RouteRegistration(t *testing.T) {
	g := NewGateway()
	called := false
	g.Mux().HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	g.Mux().ServeHTTP(rr, req)

	if !called {
		t.Error("expected /ping handler to be called")
	}
}

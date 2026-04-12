//go:build integration

package auth_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/pkg/api"
	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// mockOAuth2IdP creates a test OAuth2 identity provider that serves token and
// userinfo endpoints.
func mockOAuth2IdP(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "mock-access-token-123",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})

	mux.HandleFunc("GET /userinfo", func(w http.ResponseWriter, r *http.Request) {
		bearer := r.Header.Get("Authorization")
		if bearer != "Bearer mock-access-token-123" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"email": "sso-user@acme.com",
			"name":  "SSO User",
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// mockSSOConfigLoaderForIdP returns a SSOConfigLoadFunc that resolves a test
// OAuth2 provider pointing at the given httptest IdP server.
func mockSSOConfigLoaderForIdP(idpURL string) auth.SSOConfigLoadFunc {
	return func(_ context.Context, _ *tenancy.SiteContext, name string) (*auth.SSOProviderConfig, error) {
		if name != "test-oauth2" {
			return nil, fmt.Errorf("unknown SSO provider %q", name)
		}
		return &auth.SSOProviderConfig{
			ProviderName:   "test-oauth2",
			ProviderType:   "OAuth2",
			ClientID:       "test-client-id",
			ClientSecret:   "test-client-secret",
			AuthorizeURL:   idpURL + "/authorize",
			TokenURL:       idpURL + "/token",
			UserInfoURL:    idpURL + "/userinfo",
			Scopes:         "email profile",
			DefaultRole:    "Sales User",
			AutoCreateUser: true,
		}, nil
	}
}

// buildSSOTestServer creates a minimal HTTP test server with the SSO handler
// registered and a site-resolution middleware that injects the auth test site.
// It creates tab_user and tab_has_role tables needed by the UserProvisioner.
func buildSSOTestServer(t *testing.T, idpURL string) *httptest.Server {
	t.Helper()

	pool := authSitePool
	ctx := context.Background()

	// Create the tables the UserProvisioner needs (these are normally created
	// by the core bootstrap, which isn't run in this test).
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS "tab_user" (
			"name"        TEXT PRIMARY KEY,
			"full_name"   TEXT NOT NULL DEFAULT '',
			"password"    TEXT NOT NULL DEFAULT '',
			"enabled"     BOOLEAN NOT NULL DEFAULT true,
			"owner"       TEXT NOT NULL DEFAULT '',
			"creation"    TIMESTAMPTZ NOT NULL DEFAULT now(),
			"modified"    TIMESTAMPTZ NOT NULL DEFAULT now(),
			"modified_by" TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS "tab_has_role" (
			"name"        TEXT PRIMARY KEY,
			"parent"      TEXT NOT NULL,
			"parenttype"  TEXT NOT NULL DEFAULT 'User',
			"parentfield" TEXT NOT NULL DEFAULT 'roles',
			"role"        TEXT NOT NULL,
			"owner"       TEXT NOT NULL DEFAULT '',
			"creation"    TIMESTAMPTZ NOT NULL DEFAULT now(),
			"modified"    TIMESTAMPTZ NOT NULL DEFAULT now(),
			"modified_by" TEXT NOT NULL DEFAULT ''
		)`,
	} {
		if _, err := pool.Exec(ctx, ddl); err != nil {
			t.Fatalf("create SSO test table: %v", err)
		}
	}

	// Clean up SSO-provisioned users after test.
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM "tab_has_role" WHERE "owner" = 'SSO'`)
		_, _ = pool.Exec(ctx, `DELETE FROM "tab_user" WHERE "owner" = 'SSO'`)
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	sessions := auth.NewSessionManager(
		redis.NewClient(&redis.Options{Addr: "localhost:6380", DB: 2}),
		24*time.Hour,
	)
	provisioner := auth.NewUserProvisioner(logger)

	site := &tenancy.SiteContext{
		Name: authSiteName,
		Pool: pool,
	}

	ssoHandler := api.NewSSOHandler(
		sessions, provisioner,
		mockSSOConfigLoaderForIdP(idpURL),
		nil, // no field encryption in test
		redis.NewClient(&redis.Options{Addr: "localhost:6380", DB: 2}),
		logger,
	)

	mux := http.NewServeMux()
	ssoHandler.RegisterRoutes(mux, "v1")

	// Wrap with a middleware that injects the SiteContext.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		siteHeader := r.Header.Get("X-Moca-Site")
		if siteHeader == authSiteName {
			r = r.WithContext(api.WithSite(r.Context(), site))
		}
		mux.ServeHTTP(w, r)
	})

	return httptest.NewServer(handler)
}

// TestSSOInteg_OAuth2AuthorizeRedirect verifies that the authorize endpoint
// redirects to the IdP with a state parameter and correct query params.
func TestSSOInteg_OAuth2AuthorizeRedirect(t *testing.T) {
	idp := mockOAuth2IdP(t)

	// We need an SSO handler registered on the test server. Since authTestServer
	// was built without SSO routes in TestMain, we build a minimal one here.
	ssoServer := buildSSOTestServer(t, idp.URL)
	defer ssoServer.Close()

	// Call the authorize endpoint.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse // don't follow redirects
	}}

	req, err := http.NewRequest(http.MethodGet,
		ssoServer.URL+"/api/v1/auth/sso/authorize?provider=test-oauth2", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("X-Moca-Site", authSiteName)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("authorize request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("authorize: expected 302, got %d", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	if location == "" {
		t.Fatal("authorize: no Location header")
	}

	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse redirect URL: %v", err)
	}

	// The redirect should go to the IdP's authorize URL.
	if u.Host != "" && u.Path != "/authorize" {
		// Check it starts with the IdP URL
		if location[:len(idp.URL)] != idp.URL {
			t.Errorf("redirect should point to IdP, got %q", location)
		}
	}

	// State parameter must be present.
	state := u.Query().Get("state")
	if state == "" {
		t.Error("redirect URL missing state parameter")
	}

	// client_id should be present.
	clientID := u.Query().Get("client_id")
	if clientID != "test-client-id" {
		t.Errorf("client_id = %q, want %q", clientID, "test-client-id")
	}
}

// TestSSOInteg_OAuth2CallbackCreatesSession tests the full OAuth2 flow:
// authorize → extract state → callback with code → session cookie set.
func TestSSOInteg_OAuth2CallbackCreatesSession(t *testing.T) {
	idp := mockOAuth2IdP(t)
	ssoServer := buildSSOTestServer(t, idp.URL)
	defer ssoServer.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// Step 1: Authorize — get the state token.
	req, _ := http.NewRequest(http.MethodGet,
		ssoServer.URL+"/api/v1/auth/sso/authorize?provider=test-oauth2", nil)
	req.Header.Set("X-Moca-Site", authSiteName)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("authorize: expected 302, got %d", resp.StatusCode)
	}

	location, _ := url.Parse(resp.Header.Get("Location"))
	state := location.Query().Get("state")
	if state == "" {
		t.Fatal("no state parameter in authorize redirect")
	}

	// Step 2: Callback — simulate IdP redirecting back with a code.
	callbackURL := fmt.Sprintf("%s/api/v1/auth/sso/callback?state=%s&code=test-auth-code", ssoServer.URL, state)
	req, _ = http.NewRequest(http.MethodGet, callbackURL, nil)
	req.Header.Set("X-Moca-Site", authSiteName)

	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	resp.Body.Close()

	// The callback should redirect to /desk with a session cookie.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback: expected 302, got %d", resp.StatusCode)
	}

	// Check for moca_sid cookie.
	var sidCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "moca_sid" {
			sidCookie = c
			break
		}
	}
	if sidCookie == nil {
		t.Error("callback: expected moca_sid cookie in response")
	}
}

// TestSSOInteg_OAuth2InvalidState verifies that a callback with an invalid
// state parameter is rejected.
func TestSSOInteg_OAuth2InvalidState(t *testing.T) {
	idp := mockOAuth2IdP(t)
	ssoServer := buildSSOTestServer(t, idp.URL)
	defer ssoServer.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	callbackURL := ssoServer.URL + "/api/v1/auth/sso/callback?state=bogus-state&code=test-code"
	req, _ := http.NewRequest(http.MethodGet, callbackURL, nil)
	req.Header.Set("X-Moca-Site", authSiteName)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	resp.Body.Close()

	// Should redirect to an error page, not set a cookie.
	for _, c := range resp.Cookies() {
		if c.Name == "moca_sid" {
			t.Error("invalid state should not produce a session cookie")
		}
	}
}

// TestSSOInteg_UnknownProvider verifies that requesting an unknown provider
// returns a 404 error.
func TestSSOInteg_UnknownProvider(t *testing.T) {
	idp := mockOAuth2IdP(t)
	ssoServer := buildSSOTestServer(t, idp.URL)
	defer ssoServer.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	req, _ := http.NewRequest(http.MethodGet,
		ssoServer.URL+"/api/v1/auth/sso/authorize?provider=nonexistent", nil)
	req.Header.Set("X-Moca-Site", authSiteName)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown provider: expected 404, got %d", resp.StatusCode)
	}
}

// TestSSOInteg_MissingProvider verifies that authorize without provider param
// returns 400.
func TestSSOInteg_MissingProvider(t *testing.T) {
	idp := mockOAuth2IdP(t)
	ssoServer := buildSSOTestServer(t, idp.URL)
	defer ssoServer.Close()

	req, _ := http.NewRequest(http.MethodGet,
		ssoServer.URL+"/api/v1/auth/sso/authorize", nil)
	req.Header.Set("X-Moca-Site", authSiteName)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing provider: expected 400, got %d", resp.StatusCode)
	}
}

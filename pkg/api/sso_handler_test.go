package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// ssoTestEnv sets up a minimal test environment for SSO handler tests.
type ssoTestEnv struct {
	handler    *SSOHandler
	mux        *http.ServeMux
	mini       *miniredis.Miniredis
	sessionMgr *auth.SessionManager
	site       *tenancy.SiteContext
}

func newSSOTestEnv(t *testing.T, loadConfig auth.SSOConfigLoadFunc) *ssoTestEnv {
	t.Helper()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mini.Close)

	redisClient := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	sessionMgr := auth.NewSessionManager(redisClient, 24*time.Hour)
	provisioner := auth.NewUserProvisioner(slog.Default())

	handler := NewSSOHandler(
		sessionMgr,
		provisioner,
		loadConfig,
		nil, // no encryptor in tests
		redisClient,
		slog.Default(),
	)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, "v1")

	site := &tenancy.SiteContext{Name: "test-site"}

	return &ssoTestEnv{
		handler:    handler,
		mux:        mux,
		mini:       mini,
		sessionMgr: sessionMgr,
		site:       site,
	}
}

// makeRequest creates an HTTP request with the site context injected.
func (env *ssoTestEnv) makeRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	ctx := WithSite(req.Context(), env.site)
	return req.WithContext(ctx)
}

// makePostRequest creates a POST request with form body and site context.
func (env *ssoTestEnv) makePostRequest(path string, form url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := WithSite(req.Context(), env.site)
	return req.WithContext(ctx)
}

// mockConfigLoader returns a config loader that returns the given config.
func mockConfigLoader(configs map[string]*auth.SSOProviderConfig) auth.SSOConfigLoadFunc {
	return func(ctx context.Context, site *tenancy.SiteContext, name string) (*auth.SSOProviderConfig, error) {
		cfg, ok := configs[name]
		if !ok {
			return nil, auth.ErrSSOProviderNotFound
		}
		return cfg, nil
	}
}

// --- Tests ---

func TestSSOHandler_RegisterRoutes(t *testing.T) {
	configs := map[string]*auth.SSOProviderConfig{
		"test": {
			ProviderName: "test",
			ProviderType: "OAuth2",
			ClientID:     "id",
			AuthorizeURL: "https://idp.example.com/auth",
		},
	}
	env := newSSOTestEnv(t, mockConfigLoader(configs))

	// Authorize route: should redirect (302) when provider exists.
	req := env.makeRequest("GET", "/api/v1/auth/sso/authorize?provider=test")
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("authorize: status = %d, want %d", w.Code, http.StatusFound)
	}

	// Callback route: returns redirect (state invalid, but route is registered).
	req = env.makeRequest("GET", "/api/v1/auth/sso/callback?state=x&code=y")
	w = httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("callback: status = %d, want %d (error redirect)", w.Code, http.StatusFound)
	}

	// Metadata route: should return 200 with XML for a SAML provider.
	samlConfigs := map[string]*auth.SSOProviderConfig{
		"saml-test": {
			ProviderName: "saml-test",
			ProviderType: "SAML",
			IdPEntityID:  "https://idp.example.com",
			IdPSSOURL:    "https://idp.example.com/sso",
		},
	}
	env2 := newSSOTestEnv(t, mockConfigLoader(samlConfigs))
	req = env2.makeRequest("GET", "/api/v1/auth/saml/metadata?provider=saml-test")
	w = httptest.NewRecorder()
	env2.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("metadata: status = %d, want %d", w.Code, http.StatusOK)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/xml" {
		t.Errorf("metadata Content-Type = %q, want application/xml", ct)
	}
}

func TestSSOHandler_Authorize_MissingProvider(t *testing.T) {
	env := newSSOTestEnv(t, mockConfigLoader(nil))

	req := env.makeRequest("GET", "/api/v1/auth/sso/authorize")
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestSSOHandler_Authorize_ProviderNotFound(t *testing.T) {
	env := newSSOTestEnv(t, mockConfigLoader(nil))

	req := env.makeRequest("GET", "/api/v1/auth/sso/authorize?provider=nonexistent")
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusFound)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "sso_failed") {
		t.Errorf("expected error redirect, got Location: %s", loc)
	}
}

func TestSSOHandler_Authorize_OAuth2_Redirect(t *testing.T) {
	configs := map[string]*auth.SSOProviderConfig{
		"google": {
			ProviderName: "google",
			ProviderType: "OAuth2",
			ClientID:     "google-client-id",
			AuthorizeURL: "https://accounts.google.com/o/oauth2/auth",
			TokenURL:     "https://oauth2.googleapis.com/token",
			UserInfoURL:  "https://www.googleapis.com/oauth2/v3/userinfo",
			Scopes:       "openid email profile",
		},
	}
	env := newSSOTestEnv(t, mockConfigLoader(configs))

	req := env.makeRequest("GET", "/api/v1/auth/sso/authorize?provider=google&redirect_to=/app/todo")
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}

	location := w.Header().Get("Location")
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("invalid redirect URL: %v", err)
	}

	if parsed.Host != "accounts.google.com" {
		t.Errorf("redirect host = %q, want accounts.google.com", parsed.Host)
	}

	q := parsed.Query()
	if q.Get("client_id") != "google-client-id" {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q", q.Get("response_type"))
	}
	state := q.Get("state")
	if state == "" {
		t.Error("missing state parameter")
	}

	// Verify state is stored in Redis by validating it.
	redisClient := redis.NewClient(&redis.Options{Addr: env.mini.Addr()})
	defer redisClient.Close() //nolint:errcheck

	key := "sso_state:test-site:" + state
	val, err := redisClient.Get(context.Background(), key).Result()
	if err != nil {
		t.Fatalf("state not found in Redis: %v", err)
	}

	var payload ssoStatePayload
	if err := json.Unmarshal([]byte(val), &payload); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	if payload.Provider != "google" {
		t.Errorf("state.Provider = %q", payload.Provider)
	}
	if payload.RedirectTo != "/app/todo" {
		t.Errorf("state.RedirectTo = %q", payload.RedirectTo)
	}
}

func TestSSOHandler_Authorize_AbsoluteRedirect_Rejected(t *testing.T) {
	configs := map[string]*auth.SSOProviderConfig{
		"test": {ProviderName: "test", ProviderType: "OAuth2", AuthorizeURL: "https://idp.example.com/auth"},
	}
	env := newSSOTestEnv(t, mockConfigLoader(configs))

	req := env.makeRequest("GET", "/api/v1/auth/sso/authorize?provider=test&redirect_to=https://evil.com")
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (open redirect prevention)", w.Code, http.StatusBadRequest)
	}
}

func TestSSOHandler_Authorize_ProtocolRelativeRedirect_Rejected(t *testing.T) {
	configs := map[string]*auth.SSOProviderConfig{
		"test": {ProviderName: "test", ProviderType: "OAuth2", AuthorizeURL: "https://idp.example.com/auth"},
	}
	env := newSSOTestEnv(t, mockConfigLoader(configs))

	req := env.makeRequest("GET", "/api/v1/auth/sso/authorize?provider=test&redirect_to=//evil.com")
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (protocol-relative redirect prevention)", w.Code, http.StatusBadRequest)
	}
}

func TestSSOHandler_Callback_MissingParams(t *testing.T) {
	env := newSSOTestEnv(t, mockConfigLoader(nil))

	req := env.makeRequest("GET", "/api/v1/auth/sso/callback")
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	// Should redirect to login with error.
	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusFound)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "error=sso_failed") {
		t.Errorf("expected error redirect, got Location: %s", loc)
	}
}

func TestSSOHandler_Callback_InvalidState(t *testing.T) {
	env := newSSOTestEnv(t, mockConfigLoader(nil))

	req := env.makeRequest("GET", "/api/v1/auth/sso/callback?state=bogus&code=test-code")
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusFound)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "error=sso_failed") {
		t.Errorf("expected error redirect, got Location: %s", loc)
	}
}

func TestSSOHandler_Callback_OAuth2_FullFlow(t *testing.T) {
	// Mock IdP server.
	idpMux := http.NewServeMux()
	idpMux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"access_token": "mock-at",
			"token_type":   "Bearer",
		})
	})
	idpMux.HandleFunc("GET /userinfo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"email": "sso-test@example.com",
			"name":  "SSO Test User",
		})
	})
	idpSrv := httptest.NewServer(idpMux)
	defer idpSrv.Close()

	configs := map[string]*auth.SSOProviderConfig{
		"mock-oauth2": {
			ProviderName:   "mock-oauth2",
			ProviderType:   "OAuth2",
			ClientID:       "cid",
			AuthorizeURL:   idpSrv.URL + "/authorize",
			TokenURL:       idpSrv.URL + "/token",
			UserInfoURL:    idpSrv.URL + "/userinfo",
			Scopes:         "openid email",
			AutoCreateUser: true,
			DefaultRole:    "Website User",
			EmailClaim:     "email",
			FullNameClaim:  "name",
		},
	}
	env := newSSOTestEnv(t, mockConfigLoader(configs))

	// Step 1: Initiate authorize to get a state token.
	req := env.makeRequest("GET", "/api/v1/auth/sso/authorize?provider=mock-oauth2")
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("authorize status = %d, want %d", w.Code, http.StatusFound)
	}

	loc, _ := url.Parse(w.Header().Get("Location"))
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatal("missing state in authorize redirect")
	}

	// Step 2: Simulate callback with the state and a mock code.
	// Note: completeSSO will fail because we don't have a real DB for user
	// provisioning. We verify the flow reaches the exchange step.
	callbackURL := "/api/v1/auth/sso/callback?state=" + state + "&code=mock-auth-code"
	req = env.makeRequest("GET", callbackURL)
	w = httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	// The callback will redirect — either to /desk (success) or to login (error).
	// Since we don't have a real DB for user provisioning, it will error.
	// But we can verify the state was consumed (single-use).
	if w.Code != http.StatusFound {
		t.Errorf("callback status = %d, want %d", w.Code, http.StatusFound)
	}

	// Verify state was consumed (replay should fail).
	replayReq := env.makeRequest("GET", callbackURL)
	replayW := httptest.NewRecorder()
	env.mux.ServeHTTP(replayW, replayReq)

	replayLoc := replayW.Header().Get("Location")
	if !strings.Contains(replayLoc, "error=sso_failed") {
		t.Error("replayed state should be rejected")
	}
}

func TestSSOHandler_SAML_Metadata_MissingProvider(t *testing.T) {
	env := newSSOTestEnv(t, mockConfigLoader(nil))

	req := env.makeRequest("GET", "/api/v1/auth/saml/metadata")
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestSSOHandler_SAML_ACS_MissingRelayState(t *testing.T) {
	env := newSSOTestEnv(t, mockConfigLoader(nil))

	form := url.Values{"SAMLResponse": {"base64data"}}
	req := env.makePostRequest("/api/v1/auth/saml/acs", form)
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusFound)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "error=sso_failed") {
		t.Errorf("expected error redirect, got Location: %s", loc)
	}
}

func TestIsRelativePath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/desk", true},
		{"/app/todo", true},
		{"", false},
		{"https://evil.com", false},
		{"//evil.com", false},
		{"javascript:alert(1)", false},
		{"/", true},
	}
	for _, tt := range tests {
		got := isRelativePath(tt.path)
		if got != tt.want {
			t.Errorf("isRelativePath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestBuildCallbackURL(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/auth/sso/authorize", nil)
	req.Host = "app.example.com"

	got := buildCallbackURL(req)
	want := "http://app.example.com/api/v1/auth/sso/callback"
	if got != want {
		t.Errorf("buildCallbackURL = %q, want %q", got, want)
	}
}

func TestBuildCallbackURL_WithForwardedProto(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/auth/sso/authorize", nil)
	req.Host = "app.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")

	got := buildCallbackURL(req)
	want := "https://app.example.com/api/v1/auth/sso/callback"
	if got != want {
		t.Errorf("buildCallbackURL = %q, want %q", got, want)
	}
}

func TestGenerateSSOStateToken(t *testing.T) {
	t1, err := generateSSOStateToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(t1) != 64 { // 32 bytes = 64 hex chars
		t.Errorf("token length = %d, want 64", len(t1))
	}

	t2, err := generateSSOStateToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if t1 == t2 {
		t.Error("tokens should be unique")
	}
}

func TestSSOHandler_StateStore_Expiry(t *testing.T) {
	env := newSSOTestEnv(t, mockConfigLoader(nil))

	// Store a state token.
	token, err := env.handler.storeState(context.Background(), env.site, ssoStatePayload{
		Provider: "test",
	})
	if err != nil {
		t.Fatalf("storeState: %v", err)
	}

	// Fast-forward Redis time past the TTL.
	env.mini.FastForward(ssoStateTTL + time.Second)

	// Validate should fail (expired).
	_, err = env.handler.validateState(context.Background(), env.site, token)
	if err != auth.ErrSSOStateInvalid {
		t.Errorf("expected ErrSSOStateInvalid for expired state, got %v", err)
	}
}

func TestSSOHandler_StateStore_SingleUse(t *testing.T) {
	env := newSSOTestEnv(t, mockConfigLoader(nil))

	token, err := env.handler.storeState(context.Background(), env.site, ssoStatePayload{
		Provider: "test",
	})
	if err != nil {
		t.Fatalf("storeState: %v", err)
	}

	// First validation should succeed.
	payload, err := env.handler.validateState(context.Background(), env.site, token)
	if err != nil {
		t.Fatalf("first validateState: %v", err)
	}
	if payload.Provider != "test" {
		t.Errorf("Provider = %q", payload.Provider)
	}

	// Second validation should fail (consumed).
	_, err = env.handler.validateState(context.Background(), env.site, token)
	if err != auth.ErrSSOStateInvalid {
		t.Errorf("expected ErrSSOStateInvalid for consumed state, got %v", err)
	}
}

func TestSSOHandler_LoadAndDecryptConfig_RejectsPlaintextWithoutEncryptor(t *testing.T) {
	configs := map[string]*auth.SSOProviderConfig{
		"google": {
			ProviderName: "google",
			ProviderType: "OAuth2",
			ClientID:     "id",
			ClientSecret: "plaintext-secret",
			AuthorizeURL: "https://accounts.google.com/o/oauth2/auth",
			TokenURL:     "https://oauth2.googleapis.com/token",
		},
	}
	env := newSSOTestEnv(t, mockConfigLoader(configs))

	req := env.makeRequest("GET", "/api/v1/auth/sso/authorize?provider=google")
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	// Should fail — encryptor is nil but secrets exist. Handler redirects to login with error.
	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "sso_failed") {
		t.Fatalf("expected error redirect, got Location: %s", loc)
	}
}

func TestSSOHandler_LoadAndDecryptConfig_RejectsUnencryptedWithEncryptor(t *testing.T) {
	configs := map[string]*auth.SSOProviderConfig{
		"google": {
			ProviderName: "google",
			ProviderType: "OAuth2",
			ClientID:     "id",
			ClientSecret: "plaintext-not-encrypted",
			AuthorizeURL: "https://accounts.google.com/o/oauth2/auth",
			TokenURL:     "https://oauth2.googleapis.com/token",
		},
	}

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mini.Close)

	redisClient := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	sessionMgr := auth.NewSessionManager(redisClient, 24*time.Hour)
	provisioner := auth.NewUserProvisioner(slog.Default())

	encryptor, err := auth.NewFieldEncryptor(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatalf("NewFieldEncryptor: %v", err)
	}

	handler := NewSSOHandler(sessionMgr, provisioner,
		mockConfigLoader(configs), encryptor, redisClient, slog.Default())

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, "v1")

	site := &tenancy.SiteContext{Name: "test-site"}
	req := httptest.NewRequest("GET", "/api/v1/auth/sso/authorize?provider=google", nil)
	ctx := WithSite(req.Context(), site)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "sso_failed") {
		t.Fatalf("expected error redirect, got Location: %s", loc)
	}
}

package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/observe"
)

// ── extractTokenAuth tests ────────────────────────────────────────────────────

func TestExtractTokenAuth(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		wantKeyID  string
		wantSecret string
	}{
		{
			name:       "valid token",
			header:     "token moca_abc123:secretvalue",
			wantKeyID:  "moca_abc123",
			wantSecret: "secretvalue",
		},
		{
			name:       "case insensitive prefix",
			header:     "Token moca_key1:mysecret",
			wantKeyID:  "moca_key1",
			wantSecret: "mysecret",
		},
		{
			name:       "TOKEN uppercase",
			header:     "TOKEN moca_key1:secret123",
			wantKeyID:  "moca_key1",
			wantSecret: "secret123",
		},
		{
			name:       "missing header",
			header:     "",
			wantKeyID:  "",
			wantSecret: "",
		},
		{
			name:       "bearer prefix not matched",
			header:     "Bearer eyJhbGciOi...",
			wantKeyID:  "",
			wantSecret: "",
		},
		{
			name:       "no colon separator",
			header:     "token moca_abc123_nosecret",
			wantKeyID:  "",
			wantSecret: "",
		},
		{
			name:       "empty key id",
			header:     "token :secretvalue",
			wantKeyID:  "",
			wantSecret: "",
		},
		{
			name:       "empty secret",
			header:     "token moca_abc123:",
			wantKeyID:  "",
			wantSecret: "",
		},
		{
			name:       "extra spaces",
			header:     "token   moca_key1:secret1",
			wantKeyID:  "moca_key1",
			wantSecret: "secret1",
		},
		{
			name:       "colon in secret is ok (only split on first colon)",
			header:     "token moca_key1:secret:with:colons",
			wantKeyID:  "moca_key1",
			wantSecret: "secret:with:colons",
		},
		{
			name:       "just the word token",
			header:     "token",
			wantKeyID:  "",
			wantSecret: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			gotKey, gotSecret := extractTokenAuth(r)
			if gotKey != tt.wantKeyID {
				t.Errorf("keyID = %q, want %q", gotKey, tt.wantKeyID)
			}
			if gotSecret != tt.wantSecret {
				t.Errorf("secret = %q, want %q", gotSecret, tt.wantSecret)
			}
		})
	}
}

// ── clientIP tests ────────────────────────────────────────────────────────────

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		want       string
	}{
		{
			name:       "remote addr with port",
			remoteAddr: "192.168.1.100:54321",
			want:       "192.168.1.100",
		},
		{
			name:       "remote addr without port",
			remoteAddr: "192.168.1.100",
			want:       "192.168.1.100",
		},
		{
			name:       "x-forwarded-for single IP",
			remoteAddr: "10.0.0.1:80",
			xff:        "203.0.113.50",
			want:       "203.0.113.50",
		},
		{
			name:       "x-forwarded-for multiple IPs takes first",
			remoteAddr: "10.0.0.1:80",
			xff:        "203.0.113.50, 70.41.3.18, 150.172.238.178",
			want:       "203.0.113.50",
		},
		{
			name:       "x-forwarded-for takes priority over remote addr",
			remoteAddr: "10.0.0.1:80",
			xff:        "1.2.3.4",
			want:       "1.2.3.4",
		},
		{
			name:       "ipv6 remote addr",
			remoteAddr: "[::1]:8080",
			want:       "::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			got := clientIP(r)
			if got != tt.want {
				t.Errorf("clientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ── matchIPAllowlist tests ────────────────────────────────────────────────────

func TestMatchIPAllowlist(t *testing.T) {
	tests := []struct {
		name  string
		ip    string
		cidrs []string
		want  bool
	}{
		{
			name:  "empty allowlist allows all",
			ip:    "192.168.1.1",
			cidrs: nil,
			want:  true,
		},
		{
			name:  "exact IP match",
			ip:    "10.0.0.5",
			cidrs: []string{"10.0.0.5"},
			want:  true,
		},
		{
			name:  "CIDR match",
			ip:    "10.0.0.55",
			cidrs: []string{"10.0.0.0/24"},
			want:  true,
		},
		{
			name:  "CIDR no match",
			ip:    "10.0.1.55",
			cidrs: []string{"10.0.0.0/24"},
			want:  false,
		},
		{
			name:  "multiple CIDRs second matches",
			ip:    "172.16.0.10",
			cidrs: []string{"10.0.0.0/8", "172.16.0.0/16"},
			want:  true,
		},
		{
			name:  "no match",
			ip:    "8.8.8.8",
			cidrs: []string{"10.0.0.0/8", "172.16.0.0/12"},
			want:  false,
		},
		{
			name:  "invalid IP returns false",
			ip:    "not-an-ip",
			cidrs: []string{"10.0.0.0/8"},
			want:  false,
		},
		{
			name:  "invalid CIDR is skipped",
			ip:    "10.0.0.1",
			cidrs: []string{"invalid-cidr", "10.0.0.0/24"},
			want:  true,
		},
		{
			name:  "ipv6 CIDR match",
			ip:    "2001:db8::1",
			cidrs: []string{"2001:db8::/32"},
			want:  true,
		},
		{
			name:  "ipv6 no match",
			ip:    "2001:db9::1",
			cidrs: []string{"2001:db8::/32"},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchIPAllowlist(tt.ip, tt.cidrs)
			if got != tt.want {
				t.Errorf("matchIPAllowlist(%q, %v) = %v, want %v", tt.ip, tt.cidrs, got, tt.want)
			}
		})
	}
}

// ── Auth middleware integration tests ─────────────────────────────────────────

// mockAPIKeyValidator is a test double for APIKeyValidator.
type mockAPIKeyValidator struct {
	identity *APIKeyIdentity
	err      error
}

func (m *mockAPIKeyValidator) ValidateRequest(_ context.Context, _ *http.Request) (*APIKeyIdentity, error) {
	return m.identity, m.err
}

// mockAuthenticator is a test double for the regular Authenticator.
type mockAuthenticator struct {
	user *auth.User
	err  error
}

func (m *mockAuthenticator) Authenticate(_ *http.Request) (*auth.User, error) {
	return m.user, m.err
}

func TestAuthMiddleware_APIKey_Success(t *testing.T) {
	apiUser := &auth.User{Email: "api@example.com", Roles: []string{"API"}}
	scopes := []meta.APIScopePerm{{Scope: "all", DocTypes: nil, Operations: nil}}
	rateLimit := &meta.RateLimitConfig{Window: time.Minute, MaxRequests: 100}

	validator := &mockAPIKeyValidator{
		identity: &APIKeyIdentity{
			User:      apiUser,
			KeyID:     "moca_testkey",
			Scopes:    scopes,
			RateLimit: rateLimit,
		},
	}
	regularAuth := &mockAuthenticator{
		user: &auth.User{Email: "Guest", Roles: []string{"Guest"}},
	}

	var capturedCtx context.Context
	handler := authMiddleware(regularAuth, validator)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/v1/resource/Test", nil)
	r.Header.Set("Authorization", "token moca_testkey:thesecret")
	// Seed logger context (normally done by requestIDMiddleware).
	ctx := observe.ContextWithLogger(r.Context(), observe.NewLogger(slog.LevelDebug))
	ctx = WithRequestID(ctx, "req-123")
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify context enrichment.
	if u := UserFromContext(capturedCtx); u == nil || u.Email != "api@example.com" {
		t.Errorf("user not set correctly in context")
	}
	if keyID := APIKeyIDFromContext(capturedCtx); keyID != "moca_testkey" {
		t.Errorf("API key ID = %q, want %q", keyID, "moca_testkey")
	}
	gotScopes := APIScopesFromContext(capturedCtx)
	if len(gotScopes) != 1 || gotScopes[0].Scope != "all" {
		t.Errorf("scopes not set correctly in context: %v", gotScopes)
	}
	if cfg := APIRateLimitFromContext(capturedCtx); cfg == nil || cfg.MaxRequests != 100 {
		t.Errorf("rate limit not set correctly in context")
	}
}

func TestAuthMiddleware_APIKey_FailNoFallthrough(t *testing.T) {
	validator := &mockAPIKeyValidator{err: ErrAPIKeySecret}
	regularAuth := &mockAuthenticator{
		user: &auth.User{Email: "Guest", Roles: []string{"Guest"}},
	}

	handlerCalled := false
	handler := authMiddleware(regularAuth, validator)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		handlerCalled = true
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/v1/resource/Test", nil)
	r.Header.Set("Authorization", "token moca_badkey:wrongsecret")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	if handlerCalled {
		t.Error("handler should not be called on API key auth failure")
	}
}

func TestAuthMiddleware_APIKey_IPDenied(t *testing.T) {
	validator := &mockAPIKeyValidator{err: ErrIPNotAllowed}

	handler := authMiddleware(&mockAuthenticator{}, validator)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called")
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/v1/resource/Test", nil)
	r.Header.Set("Authorization", "token moca_key:secret")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestAuthMiddleware_APIKey_Expired(t *testing.T) {
	validator := &mockAPIKeyValidator{err: ErrAPIKeyExpired}

	handler := authMiddleware(&mockAuthenticator{}, validator)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called")
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/v1/resource/Test", nil)
	r.Header.Set("Authorization", "token moca_key:secret")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthMiddleware_NoToken_FallsThrough(t *testing.T) {
	validator := &mockAPIKeyValidator{
		err: errors.New("should not be called"),
	}
	regularUser := &auth.User{Email: "alice@example.com", Roles: []string{"User"}}
	regularAuth := &mockAuthenticator{user: regularUser}

	var capturedCtx context.Context
	handler := authMiddleware(regularAuth, validator)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/v1/resource/Test", nil)
	r.Header.Set("Authorization", "Bearer some-jwt-token")
	ctx := observe.ContextWithLogger(r.Context(), observe.NewLogger(slog.LevelDebug))
	ctx = WithRequestID(ctx, "req-456")
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if u := UserFromContext(capturedCtx); u == nil || u.Email != "alice@example.com" {
		t.Errorf("regular auth user not set")
	}
	if keyID := APIKeyIDFromContext(capturedCtx); keyID != "" {
		t.Errorf("API key ID should be empty for non-token auth, got %q", keyID)
	}
}

func TestAuthMiddleware_NoToken_NoAuthHeader(t *testing.T) {
	guestUser := &auth.User{Email: "Guest", Roles: []string{"Guest"}}
	regularAuth := &mockAuthenticator{user: guestUser}

	var capturedUser *auth.User
	handler := authMiddleware(regularAuth, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedUser = UserFromContext(r.Context())
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/v1/resource/Test", nil)
	ctx := observe.ContextWithLogger(r.Context(), observe.NewLogger(slog.LevelDebug))
	ctx = WithRequestID(ctx, "req-789")
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if capturedUser == nil || capturedUser.Email != "Guest" {
		t.Errorf("expected Guest user, got %v", capturedUser)
	}
}

func TestAuthMiddleware_SkipsNonAPIPaths(t *testing.T) {
	handlerCalled := false
	handler := authMiddleware(&mockAuthenticator{err: errors.New("should not be called")}, nil)(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			handlerCalled = true
		}),
	)

	for _, path := range []string{"/desk/index.html", "/health", "/ws"} {
		handlerCalled = false
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if !handlerCalled {
			t.Errorf("handler should be called for excluded path %s", path)
		}
	}
}

// ── Rate limit middleware with API key tests ──────────────────────────────────

func TestRateLimitMiddleware_APIKey_UsesKeyPattern(t *testing.T) {
	// This test verifies that when API key context is set, the middleware
	// uses the per-key pattern and config. We test by ensuring the request
	// passes through (no rate limiting) with a nil rate limiter.
	var capturedCtx context.Context
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
	})

	mw := rateLimitMiddleware(nil, nil)(inner)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := WithAPIKeyID(r.Context(), "moca_testkey")
	ctx = WithAPIRateLimit(ctx, &meta.RateLimitConfig{Window: time.Minute, MaxRequests: 50})
	r = r.WithContext(ctx)

	w := httptest.NewRecorder()
	mw.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// Context should pass through unchanged.
	if APIKeyIDFromContext(capturedCtx) != "moca_testkey" {
		t.Error("API key context not preserved")
	}
}

// ── Context helper tests ──────────────────────────────────────────────────────

func TestAPIKeyContextHelpers(t *testing.T) {
	ctx := context.Background()

	// Initially empty.
	if id := APIKeyIDFromContext(ctx); id != "" {
		t.Errorf("expected empty, got %q", id)
	}
	if s := APIScopesFromContext(ctx); s != nil {
		t.Errorf("expected nil, got %v", s)
	}
	if c := APIRateLimitFromContext(ctx); c != nil {
		t.Errorf("expected nil, got %v", c)
	}

	// Set and retrieve.
	scopes := []meta.APIScopePerm{{Scope: "test"}}
	rlCfg := &meta.RateLimitConfig{MaxRequests: 42}

	ctx = WithAPIKeyID(ctx, "moca_key1")
	ctx = WithAPIScopes(ctx, scopes)
	ctx = WithAPIRateLimit(ctx, rlCfg)

	if id := APIKeyIDFromContext(ctx); id != "moca_key1" {
		t.Errorf("got %q, want %q", id, "moca_key1")
	}
	if s := APIScopesFromContext(ctx); len(s) != 1 || s[0].Scope != "test" {
		t.Errorf("scopes = %v, want [{Scope: test}]", s)
	}
	if c := APIRateLimitFromContext(ctx); c == nil || c.MaxRequests != 42 {
		t.Errorf("rate limit = %v, want {MaxRequests: 42}", c)
	}
}

// ── Key generation tests ──────────────────────────────────────────────────────

func TestGenerateKeyID(t *testing.T) {
	id, err := generateKeyID()
	if err != nil {
		t.Fatal(err)
	}
	if len(id) == 0 {
		t.Fatal("empty key id")
	}
	if id[:5] != "moca_" {
		t.Errorf("key id should start with 'moca_', got %q", id)
	}
	// 16 bytes = 32 hex chars + "moca_" prefix = 37 total
	if len(id) != 37 {
		t.Errorf("key id length = %d, want 37", len(id))
	}

	// Ensure uniqueness.
	id2, _ := generateKeyID()
	if id == id2 {
		t.Error("two generated key IDs should not be equal")
	}
}

func TestGenerateSecret(t *testing.T) {
	secret, err := generateSecret()
	if err != nil {
		t.Fatal(err)
	}
	// 32 bytes = 64 hex chars.
	if len(secret) != 64 {
		t.Errorf("secret length = %d, want 64", len(secret))
	}

	secret2, _ := generateSecret()
	if secret == secret2 {
		t.Error("two generated secrets should not be equal")
	}
}

// ── writeAPIKeyError tests ────────────────────────────────────────────────────

func TestWriteAPIKeyError(t *testing.T) {
	tests := []struct { //nolint:govet // test struct alignment is fine
		name     string
		err      error
		wantCode int
	}{
		{"ip not allowed", ErrIPNotAllowed, http.StatusForbidden},
		{"expired", ErrAPIKeyExpired, http.StatusUnauthorized},
		{"revoked", ErrAPIKeyRevoked, http.StatusUnauthorized},
		{"not found", ErrAPIKeyNotFound, http.StatusUnauthorized},
		{"bad secret", ErrAPIKeySecret, http.StatusUnauthorized},
		{"generic error", errors.New("something"), http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeAPIKeyError(w, tt.err)
			if w.Code != tt.wantCode {
				t.Errorf("got %d, want %d", w.Code, tt.wantCode)
			}
		})
	}
}

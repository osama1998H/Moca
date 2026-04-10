package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// mockUserLoader is a test helper that simulates user loading without a database.
type mockUserLoader struct {
	users map[string]mockUser
}

type mockUser struct {
	user         *auth.User
	passwordHash string
}

func (m *mockUserLoader) LoadByEmail(_ context.Context, _ *tenancy.SiteContext, email string) (*auth.User, string, error) {
	u, ok := m.users[email]
	if !ok {
		return nil, "", auth.ErrUserNotFound
	}
	return u.user, u.passwordHash, nil
}

// authHandlerTestEnv holds test infrastructure.
type authHandlerTestEnv struct {
	handler  *AuthHandler
	mux      *http.ServeMux
	sessions *auth.SessionManager
	site     *tenancy.SiteContext
	jwtCfg   auth.JWTConfig
}

func newAuthHandlerTestEnv(t *testing.T) *authHandlerTestEnv {
	t.Helper()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	jwtCfg := auth.JWTConfig{
		Secret:          "test-secret-key-for-jwt-signing-32b",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 7 * 24 * time.Hour,
		Issuer:          "moca-test",
	}
	sessions := auth.NewSessionManager(client, 1*time.Hour)

	// Create a test user with bcrypt password.
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	loader := &mockUserLoader{
		users: map[string]mockUser{
			"admin@example.com": {
				user: &auth.User{
					Email:    "admin@example.com",
					FullName: "Admin User",
					Roles:    []string{"Administrator"},
				},
				passwordHash: string(hash),
			},
		},
	}

	handler := NewAuthHandlerWithLoader(jwtCfg, sessions, loader.LoadByEmail, nil)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, "v1")

	site := &tenancy.SiteContext{Name: "test-site"}

	return &authHandlerTestEnv{
		handler:  handler,
		mux:      mux,
		jwtCfg:   jwtCfg,
		sessions: sessions,
		site:     site,
	}
}

func responseCookie(t *testing.T, rr *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rr.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("expected cookie %q to be set", name)
	return nil
}

func TestRefreshTokenFromRequest_UsesCookieFallback(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/auth/refresh", http.NoBody)
	req.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "cookie-refresh-token"})

	token, err := refreshTokenFromRequest(req)
	if err != nil {
		t.Fatalf("refreshTokenFromRequest: %v", err)
	}
	if token != "cookie-refresh-token" {
		t.Fatalf("token = %q, want %q", token, "cookie-refresh-token")
	}
}

func TestRefreshTokenFromRequest_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/auth/refresh", strings.NewReader("{"))

	_, err := refreshTokenFromRequest(req)
	if err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func TestResponseTokenPair_ExcludesRefreshTokenFromJSON(t *testing.T) {
	body, err := json.Marshal(responseTokenPair(&auth.TokenPair{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		ExpiresIn:    900,
	}))
	if err != nil {
		t.Fatalf("Marshal(responseTokenPair): %v", err)
	}

	if strings.Contains(string(body), "refresh_token") {
		t.Fatalf("expected JSON body %s to omit refresh_token", body)
	}
}

func TestAuthCookie_IsHttpOnly(t *testing.T) {
	cookie := authCookie(refreshCookieName, "refresh-token", 60)
	if !cookie.HttpOnly {
		t.Fatal("expected auth cookie to be HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite = %v, want %v", cookie.SameSite, http.SameSiteLaxMode)
	}
}

// serveWithSite wraps the mux to inject SiteContext into the request context.
func (env *authHandlerTestEnv) serveWithSite(req *http.Request) *httptest.ResponseRecorder {
	ctx := WithSite(req.Context(), env.site)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	env.mux.ServeHTTP(rr, req)
	return rr
}

func TestLogin_ValidCredentials(t *testing.T) {
	env := newAuthHandlerTestEnv(t)

	body := `{"email":"admin@example.com","password":"correct-password"}`
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := env.serveWithSite(req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp struct {
		Data struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token,omitempty"`
			ExpiresIn    int64  `json:"expires_in"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Data.AccessToken == "" {
		t.Error("expected non-empty access_token")
	}
	if resp.Data.RefreshToken != "" {
		t.Error("refresh_token should not be returned in the login response body")
	}
	if resp.Data.ExpiresIn <= 0 {
		t.Error("expected positive expires_in")
	}

	if sessionCookie := responseCookie(t, rr, sessionCookieName); !sessionCookie.HttpOnly {
		t.Error("moca_sid cookie should be HttpOnly")
	}
	refreshCookie := responseCookie(t, rr, refreshCookieName)
	if !refreshCookie.HttpOnly {
		t.Error("moca_rid cookie should be HttpOnly")
	}
	if refreshCookie.Value == "" {
		t.Error("moca_rid cookie should carry the refresh token")
	}
}

func TestRefresh_UsesCookieTokenWhenBodyMissing(t *testing.T) {
	env := newAuthHandlerTestEnv(t)
	ctx := context.Background()

	user := &auth.User{Email: "admin@example.com", FullName: "Admin", Roles: []string{"Administrator"}}
	pair, err := auth.IssueTokenPair(env.jwtCfg, user, "test-site")
	if err != nil {
		t.Fatalf("IssueTokenPair: %v", err)
	}

	claims, _ := auth.ValidateRefreshToken(env.jwtCfg, pair.RefreshToken)
	if storeErr := env.sessions.StoreRefreshTokenID(ctx, claims.ID, env.jwtCfg.RefreshTokenTTL); storeErr != nil {
		t.Fatalf("StoreRefreshTokenID: %v", storeErr)
	}

	req := httptest.NewRequest("POST", "/api/v1/auth/refresh", http.NoBody)
	req.AddCookie(&http.Cookie{Name: refreshCookieName, Value: pair.RefreshToken})

	rr := env.serveWithSite(req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp struct {
		Data struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token,omitempty"`
			ExpiresIn    int64  `json:"expires_in"`
		} `json:"data"`
	}
	if decErr := json.NewDecoder(rr.Body).Decode(&resp); decErr != nil {
		t.Fatalf("decode: %v", decErr)
	}
	if resp.Data.AccessToken == "" {
		t.Error("expected non-empty access_token in refresh response")
	}
	if resp.Data.RefreshToken != "" {
		t.Error("refresh_token should not be returned in the refresh response body")
	}

	rotatedCookie := responseCookie(t, rr, refreshCookieName)
	if rotatedCookie.Value == "" {
		t.Fatal("expected rotated refresh cookie value")
	}
	newClaims, err := auth.ValidateRefreshToken(env.jwtCfg, rotatedCookie.Value)
	if err != nil {
		t.Fatalf("ValidateRefreshToken(new cookie): %v", err)
	}
	if newClaims.ID == claims.ID {
		t.Error("expected refresh token rotation to issue a new jti")
	}
}

func TestLogin_InvalidPassword(t *testing.T) {
	env := newAuthHandlerTestEnv(t)

	body := `{"email":"admin@example.com","password":"wrong-password"}`
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := env.serveWithSite(req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}

	var resp errorEnvelope
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Error.Code != "AUTH_FAILED" {
		t.Errorf("error code = %q, want AUTH_FAILED", resp.Error.Code)
	}
}

func TestLogin_UnknownUser(t *testing.T) {
	env := newAuthHandlerTestEnv(t)

	body := `{"email":"unknown@example.com","password":"password"}`
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := env.serveWithSite(req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestLogin_MissingFields(t *testing.T) {
	env := newAuthHandlerTestEnv(t)

	body := `{"email":"admin@example.com"}`
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := env.serveWithSite(req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestLogout_DestroysCookieAndSession(t *testing.T) {
	env := newAuthHandlerTestEnv(t)
	ctx := context.Background()

	// Create a session first.
	user := &auth.User{Email: "admin@example.com", Roles: []string{"Admin"}}
	sid, err := env.sessions.Create(ctx, user, "test-site")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sid})
	req.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "stale-refresh-token"})

	rr := env.serveWithSite(req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	// Cookie should be cleared.
	if sessionCookie := responseCookie(t, rr, sessionCookieName); sessionCookie.MaxAge != -1 {
		t.Errorf("moca_sid MaxAge = %d, want -1", sessionCookie.MaxAge)
	}
	if refreshCookie := responseCookie(t, rr, refreshCookieName); refreshCookie.MaxAge != -1 {
		t.Errorf("moca_rid MaxAge = %d, want -1", refreshCookie.MaxAge)
	}

	// Session should be destroyed.
	_, err = env.sessions.Get(ctx, sid)
	if err != auth.ErrSessionNotFound {
		t.Errorf("Get after logout: got err=%v, want ErrSessionNotFound", err)
	}
}

func TestRefresh_RotatesTokens(t *testing.T) {
	env := newAuthHandlerTestEnv(t)
	ctx := context.Background()

	// Login first to get a valid refresh token with stored jti.
	user := &auth.User{Email: "admin@example.com", FullName: "Admin", Roles: []string{"Administrator"}}
	pair, err := auth.IssueTokenPair(env.jwtCfg, user, "test-site")
	if err != nil {
		t.Fatalf("IssueTokenPair: %v", err)
	}

	// Store the refresh token's jti.
	claims, _ := auth.ValidateRefreshToken(env.jwtCfg, pair.RefreshToken)
	err = env.sessions.StoreRefreshTokenID(ctx, claims.ID, env.jwtCfg.RefreshTokenTTL)
	if err != nil {
		t.Fatalf("StoreRefreshTokenID: %v", err)
	}

	body := `{"refresh_token":"` + pair.RefreshToken + `"}`
	req := httptest.NewRequest("POST", "/api/v1/auth/refresh", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := env.serveWithSite(req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp struct {
		Data struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token,omitempty"`
			ExpiresIn    int64  `json:"expires_in"`
		} `json:"data"`
	}
	decErr := json.NewDecoder(rr.Body).Decode(&resp)
	if decErr != nil {
		t.Fatalf("decode: %v", decErr)
	}
	if resp.Data.AccessToken == "" {
		t.Error("expected non-empty access_token in refresh response")
	}
	if resp.Data.RefreshToken != "" {
		t.Error("refresh_token should not be returned in the refresh response body")
	}
	if refreshCookie := responseCookie(t, rr, refreshCookieName); refreshCookie.Value == "" {
		t.Error("expected rotated refresh cookie in refresh response")
	}

	// Old jti should be revoked.
	used, err := env.sessions.IsRefreshTokenUsed(ctx, claims.ID)
	if err != nil {
		t.Fatalf("IsRefreshTokenUsed: %v", err)
	}
	if !used {
		t.Error("old refresh token jti should be revoked after rotation")
	}
}

func TestRefresh_ReplayDetected(t *testing.T) {
	env := newAuthHandlerTestEnv(t)
	ctx := context.Background()

	user := &auth.User{Email: "admin@example.com", FullName: "Admin", Roles: []string{"Administrator"}}
	pair, _ := auth.IssueTokenPair(env.jwtCfg, user, "test-site")

	// Store then immediately revoke (simulating first use).
	claims, _ := auth.ValidateRefreshToken(env.jwtCfg, pair.RefreshToken)
	_ = env.sessions.StoreRefreshTokenID(ctx, claims.ID, env.jwtCfg.RefreshTokenTTL)
	_ = env.sessions.RevokeRefreshToken(ctx, claims.ID)

	// Try to use the same refresh token again.
	body := `{"refresh_token":"` + pair.RefreshToken + `"}`
	req := httptest.NewRequest("POST", "/api/v1/auth/refresh", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := env.serveWithSite(req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusUnauthorized, rr.Body.String())
	}
}

func TestRefresh_MissingTokenAndCookie(t *testing.T) {
	env := newAuthHandlerTestEnv(t)

	req := httptest.NewRequest("POST", "/api/v1/auth/refresh", http.NoBody)

	rr := env.serveWithSite(req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}

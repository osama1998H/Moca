package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/pkg/tenancy"
)

// stubSiteExtractor returns a SiteExtractor that always returns nil.
// The authenticator only needs SiteContext for session-based auth where it
// calls userLoader, which is not exercised in these unit tests.
func stubSiteExtractor(_ context.Context) *tenancy.SiteContext {
	return nil
}

func newTestAuthenticator(t *testing.T) (*MocaAuthenticator, JWTConfig, *SessionManager) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	cfg := testJWTConfig()
	sessions := NewSessionManager(client, 1*time.Hour)
	userLoader := NewUserLoader(nil) // no DB needed for unit tests

	authn := NewMocaAuthenticator(cfg, sessions, userLoader, stubSiteExtractor, nil)
	return authn, cfg, sessions
}

func TestMocaAuthenticator_BearerToken(t *testing.T) {
	authn, cfg, _ := newTestAuthenticator(t)

	user := &User{
		Email:    "admin@example.com",
		FullName: "Admin User",
		Roles:    []string{"Administrator"},
	}
	pair, err := IssueTokenPair(cfg, user, "test-site")
	if err != nil {
		t.Fatalf("IssueTokenPair: %v", err)
	}

	req, _ := http.NewRequest("GET", "/api/v1/resource/SalesOrder", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)

	got, err := authn.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.Email != user.Email {
		t.Errorf("Email = %q, want %q", got.Email, user.Email)
	}
	if got.FullName != user.FullName {
		t.Errorf("FullName = %q, want %q", got.FullName, user.FullName)
	}
	if len(got.Roles) != 1 || got.Roles[0] != "Administrator" {
		t.Errorf("Roles = %v, want [Administrator]", got.Roles)
	}
}

func TestMocaAuthenticator_ExpiredBearer_ReturnsError(t *testing.T) {
	authn, cfg, _ := newTestAuthenticator(t)

	// Issue an already-expired token.
	expiredCfg := cfg
	expiredCfg.AccessTokenTTL = -1 * time.Minute
	user := &User{Email: "user@example.com", Roles: []string{"Guest"}}
	pair, err := IssueTokenPair(expiredCfg, user, "test-site")
	if err != nil {
		t.Fatalf("IssueTokenPair: %v", err)
	}

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)

	_, err = authn.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for expired Bearer token")
	}
}

func TestMocaAuthenticator_SessionCookie(t *testing.T) {
	authn, _, sessions := newTestAuthenticator(t)
	ctx := context.Background()

	user := &User{
		Email:    "session-user@example.com",
		FullName: "Session User",
		Roles:    []string{"Sales User"},
	}
	sid, err := sessions.Create(ctx, user, "test-site")
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}

	req, _ := http.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "moca_sid", Value: sid})

	got, err := authn.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.Email != user.Email {
		t.Errorf("Email = %q, want %q", got.Email, user.Email)
	}
	if got.FullName != user.FullName {
		t.Errorf("FullName = %q, want %q", got.FullName, user.FullName)
	}
}

func TestMocaAuthenticator_NoCredentials_ReturnsGuest(t *testing.T) {
	authn, _, _ := newTestAuthenticator(t)

	req, _ := http.NewRequest("GET", "/", nil)

	got, err := authn.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.Email != "Guest" {
		t.Errorf("Email = %q, want %q", got.Email, "Guest")
	}
	if len(got.Roles) != 1 || got.Roles[0] != "Guest" {
		t.Errorf("Roles = %v, want [Guest]", got.Roles)
	}
}

func TestMocaAuthenticator_BearerTakesPrecedence(t *testing.T) {
	authn, cfg, sessions := newTestAuthenticator(t)
	ctx := context.Background()

	// Create session for a different user.
	sessionUser := &User{Email: "session@example.com", Roles: []string{"Guest"}}
	sid, _ := sessions.Create(ctx, sessionUser, "test-site")

	// Issue Bearer for a different user.
	bearerUser := &User{
		Email:    "bearer@example.com",
		FullName: "Bearer User",
		Roles:    []string{"Administrator"},
	}
	pair, _ := IssueTokenPair(cfg, bearerUser, "test-site")

	// Send both.
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	req.AddCookie(&http.Cookie{Name: "moca_sid", Value: sid})

	got, err := authn.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.Email != bearerUser.Email {
		t.Errorf("Email = %q, want %q (Bearer should take precedence)", got.Email, bearerUser.Email)
	}
}

func TestMocaAuthenticator_InvalidCookie_FallsToGuest(t *testing.T) {
	authn, _, _ := newTestAuthenticator(t)

	req, _ := http.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "moca_sid", Value: "nonexistent-session"})

	got, err := authn.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.Email != "Guest" {
		t.Errorf("Email = %q, want Guest (invalid cookie should fall to Guest)", got.Email)
	}
}

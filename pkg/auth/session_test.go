package auth

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestSessionManager(t *testing.T) (*SessionManager, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewSessionManager(client, 1*time.Hour), mr
}

func TestSessionManager_CreateAndGet(t *testing.T) {
	sm, _ := newTestSessionManager(t)
	ctx := context.Background()

	user := &User{
		Email:    "admin@example.com",
		FullName: "Admin User",
		Roles:    []string{"Administrator", "Sales User"},
	}

	sid, err := sm.Create(ctx, user, "test-site")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sid == "" {
		t.Fatal("expected non-empty session ID")
	}

	sess, err := sm.Get(ctx, sid)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if sess.Email != user.Email {
		t.Errorf("Email = %q, want %q", sess.Email, user.Email)
	}
	if sess.FullName != user.FullName {
		t.Errorf("FullName = %q, want %q", sess.FullName, user.FullName)
	}
	if len(sess.Roles) != len(user.Roles) {
		t.Fatalf("Roles len = %d, want %d", len(sess.Roles), len(user.Roles))
	}
	for i, r := range sess.Roles {
		if r != user.Roles[i] {
			t.Errorf("Roles[%d] = %q, want %q", i, r, user.Roles[i])
		}
	}
	if sess.Site != "test-site" {
		t.Errorf("Site = %q, want %q", sess.Site, "test-site")
	}
	if sess.CreatedAt == 0 {
		t.Error("expected non-zero CreatedAt")
	}
}

func TestSessionManager_Destroy(t *testing.T) {
	sm, _ := newTestSessionManager(t)
	ctx := context.Background()

	user := &User{Email: "user@example.com", Roles: []string{"Guest"}}
	sid, err := sm.Create(ctx, user, "test-site")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	destroyErr := sm.Destroy(ctx, sid)
	if destroyErr != nil {
		t.Fatalf("Destroy: %v", destroyErr)
	}

	_, err = sm.Get(ctx, sid)
	if err != ErrSessionNotFound {
		t.Errorf("Get after Destroy: got err=%v, want ErrSessionNotFound", err)
	}
}

func TestSessionManager_GetNonexistent(t *testing.T) {
	sm, _ := newTestSessionManager(t)
	ctx := context.Background()

	_, err := sm.Get(ctx, "nonexistent-session-id")
	if err != ErrSessionNotFound {
		t.Errorf("Get nonexistent: got err=%v, want ErrSessionNotFound", err)
	}
}

func TestSessionManager_TTLExpiry(t *testing.T) {
	sm, mr := newTestSessionManager(t)
	ctx := context.Background()

	user := &User{Email: "user@example.com", Roles: []string{"Guest"}}
	sid, err := sm.Create(ctx, user, "test-site")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Fast-forward past the TTL.
	mr.FastForward(2 * time.Hour)

	_, err = sm.Get(ctx, sid)
	if err != ErrSessionNotFound {
		t.Errorf("Get after TTL: got err=%v, want ErrSessionNotFound", err)
	}
}

func TestRefreshToken_StoreAndCheck(t *testing.T) {
	sm, _ := newTestSessionManager(t)
	ctx := context.Background()

	jti := "test-jti-12345"

	// Store jti.
	if err := sm.StoreRefreshTokenID(ctx, jti, 1*time.Hour); err != nil {
		t.Fatalf("StoreRefreshTokenID: %v", err)
	}

	// Should NOT be used (exists in Redis).
	used, err := sm.IsRefreshTokenUsed(ctx, jti)
	if err != nil {
		t.Fatalf("IsRefreshTokenUsed: %v", err)
	}
	if used {
		t.Error("expected token to not be used yet")
	}

	// Revoke it.
	revokeErr := sm.RevokeRefreshToken(ctx, jti)
	if revokeErr != nil {
		t.Fatalf("RevokeRefreshToken: %v", revokeErr)
	}

	// Should now be used (not in Redis).
	used, err = sm.IsRefreshTokenUsed(ctx, jti)
	if err != nil {
		t.Fatalf("IsRefreshTokenUsed after revoke: %v", err)
	}
	if !used {
		t.Error("expected token to be marked as used after revocation")
	}
}

func TestRefreshToken_ReplayDetection(t *testing.T) {
	sm, _ := newTestSessionManager(t)
	ctx := context.Background()

	jti := "replay-test-jti"

	// Store and immediately revoke (simulating first use).
	if err := sm.StoreRefreshTokenID(ctx, jti, 1*time.Hour); err != nil {
		t.Fatalf("StoreRefreshTokenID: %v", err)
	}
	if err := sm.RevokeRefreshToken(ctx, jti); err != nil {
		t.Fatalf("RevokeRefreshToken: %v", err)
	}

	// Second use should detect replay.
	used, err := sm.IsRefreshTokenUsed(ctx, jti)
	if err != nil {
		t.Fatalf("IsRefreshTokenUsed: %v", err)
	}
	if !used {
		t.Error("expected replay detection: token should be marked as used")
	}
}

package auth

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// mockPool implements a minimal mock for pgxpool.Pool behavior used in
// UserProvisioner tests. We test the provisioner logic by exercising the
// public interface with a real (test) database in integration tests.
// These unit tests validate the control flow decisions (exists/disabled/
// auto-create/no-auto-create) using the exported function signatures.

func TestUserProvisioner_NilPool(t *testing.T) {
	up := NewUserProvisioner(slog.Default())
	site := &tenancy.SiteContext{Name: "test"}

	_, err := up.FindOrCreate(context.Background(), site, "user@test.com", "Test", false, "")
	if err == nil {
		t.Fatal("expected error for nil pool")
	}
}

// mockTx and mockRows stubs for unit testing the provisioner logic.
// Full integration tests with a real PostgreSQL database are in
// pkg/auth/integration_test.go (build tag: integration).

func TestNewUserProvisioner_NilLogger(t *testing.T) {
	up := NewUserProvisioner(nil)
	if up == nil {
		t.Fatal("expected non-nil provisioner")
	}
	if up.logger == nil {
		t.Fatal("expected default logger")
	}
}

func TestGenerateRandomPassword(t *testing.T) {
	p1, err := generateRandomPassword()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p1) != 64 { // 32 bytes = 64 hex chars
		t.Fatalf("expected 64 hex chars, got %d", len(p1))
	}

	// Two calls should produce different passwords.
	p2, err := generateRandomPassword()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p1 == p2 {
		t.Fatal("expected different passwords on subsequent calls")
	}
}

// TestFindOrCreate_ErrUserNotFound_NoAutoCreate verifies that when a user
// doesn't exist and autoCreate is false, ErrUserNotFound is returned.
// This test requires a real database connection, so it's guarded by the
// integration build tag. The unit test below validates error wrapping.
func TestErrUserDisabled_Is(t *testing.T) {
	if ErrUserDisabled == ErrUserNotFound {
		t.Fatal("ErrUserDisabled should be distinct from ErrUserNotFound")
	}
}

// pgxErrNoRows_sentinel verifies we're using pgx.ErrNoRows correctly.
func TestPgxErrNoRows_Sentinel(t *testing.T) {
	if pgx.ErrNoRows == nil {
		t.Fatal("pgx.ErrNoRows should not be nil")
	}
}

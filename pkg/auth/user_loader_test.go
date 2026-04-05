package auth

import (
	"context"
	"log/slog"
	"testing"

	"github.com/osama1998H/moca/pkg/tenancy"
)

func TestNewUserLoader(t *testing.T) {
	logger := slog.Default()
	ul := NewUserLoader(logger)
	if ul == nil {
		t.Fatal("expected non-nil UserLoader")
	}
	if ul.logger != logger {
		t.Error("expected logger to be set")
	}
}

func TestUserLoader_LoadByEmail_NilPool(t *testing.T) {
	ul := NewUserLoader(slog.Default())
	site := &tenancy.SiteContext{Name: "testsite"} // Pool is nil

	_, _, err := ul.LoadByEmail(context.Background(), site, "user@example.com")
	if err == nil {
		t.Fatal("expected error for nil pool")
	}
	if got := err.Error(); got != `auth: site "testsite" has no database pool` {
		t.Errorf("error = %q, want nil pool error", got)
	}
}

func TestUserLoadFunc_TypeSignature(t *testing.T) {
	// Verify UserLoadFunc can be satisfied by a simple function.
	var fn UserLoadFunc = func(ctx context.Context, site *tenancy.SiteContext, email string) (*User, string, error) {
		return &User{Email: email}, "hashed", nil
	}

	user, hash, err := fn(context.Background(), &tenancy.SiteContext{Name: "test"}, "test@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.Email != "test@example.com" {
		t.Errorf("Email = %q", user.Email)
	}
	if hash != "hashed" {
		t.Errorf("hash = %q", hash)
	}
}

package api

import (
	"context"
	"testing"

	"github.com/moca-framework/moca/pkg/auth"
	"github.com/moca-framework/moca/pkg/tenancy"
)

func TestUserContext_RoundTrip(t *testing.T) {
	u := &auth.User{Email: "alice@example.com", FullName: "Alice", Roles: []string{"Admin"}}
	ctx := WithUser(context.Background(), u)

	got := UserFromContext(ctx)
	if got == nil {
		t.Fatal("expected user, got nil")
	}
	if got.Email != u.Email {
		t.Errorf("Email = %q, want %q", got.Email, u.Email)
	}
}

func TestUserFromContext_Missing(t *testing.T) {
	if got := UserFromContext(context.Background()); got != nil {
		t.Errorf("expected nil user, got %+v", got)
	}
}

func TestSiteContext_RoundTrip(t *testing.T) {
	s := &tenancy.SiteContext{Name: "acme"}
	ctx := WithSite(context.Background(), s)

	got := SiteFromContext(ctx)
	if got == nil {
		t.Fatal("expected site, got nil")
	}
	if got.Name != "acme" {
		t.Errorf("Name = %q, want %q", got.Name, "acme")
	}
}

func TestSiteFromContext_Missing(t *testing.T) {
	if got := SiteFromContext(context.Background()); got != nil {
		t.Errorf("expected nil site, got %+v", got)
	}
}

func TestRequestIDContext_RoundTrip(t *testing.T) {
	ctx := WithRequestID(context.Background(), "req-123")

	got := RequestIDFromContext(ctx)
	if got != "req-123" {
		t.Errorf("RequestID = %q, want %q", got, "req-123")
	}
}

func TestRequestIDFromContext_Missing(t *testing.T) {
	if got := RequestIDFromContext(context.Background()); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

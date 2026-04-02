package api

import (
	"context"
	"testing"

	"github.com/osama1998H/moca/pkg/tenancy"
)

func TestRateLimitKey(t *testing.T) {
	ctx := context.Background()
	if got := rateLimitKey(ctx); got != "rl:unknown:anonymous" {
		t.Errorf("empty context key = %q, want rl:unknown:anonymous", got)
	}

	ctx = WithSite(ctx, &tenancy.SiteContext{Name: "acme"})
	if got := rateLimitKey(ctx); got != "rl:acme:anonymous" {
		t.Errorf("site-only key = %q, want rl:acme:anonymous", got)
	}
}

func TestRateLimiter_NilConfig(t *testing.T) {
	// Allow should return true with nil config (no rate limiting).
	rl := &RateLimiter{}
	allowed, _, err := rl.Allow(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected allowed with nil config")
	}
}

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/meta"
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

func TestRateLimiter_ZeroMaxRequests(t *testing.T) {
	rl := &RateLimiter{}
	cfg := &meta.RateLimitConfig{MaxRequests: 0}
	allowed, _, err := rl.Allow(context.Background(), "test", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected allowed with zero MaxRequests")
	}
}

func TestRateLimiter_NegativeMaxRequests(t *testing.T) {
	rl := &RateLimiter{}
	cfg := &meta.RateLimitConfig{MaxRequests: -1}
	allowed, _, err := rl.Allow(context.Background(), "test", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected allowed with negative MaxRequests")
	}
}

func TestRateLimitKey_FullContext(t *testing.T) {
	ctx := context.Background()
	ctx = WithSite(ctx, &tenancy.SiteContext{Name: "acme"})
	ctx = WithUser(ctx, &auth.User{Email: "admin@test.com"})

	got := rateLimitKey(ctx)
	if got != "rl:acme:admin@test.com" {
		t.Errorf("key = %q, want rl:acme:admin@test.com", got)
	}
}

func TestRateLimitKey_EmptyEmail(t *testing.T) {
	ctx := context.Background()
	ctx = WithSite(ctx, &tenancy.SiteContext{Name: "acme"})
	ctx = WithUser(ctx, &auth.User{Email: ""})

	got := rateLimitKey(ctx)
	if got != "rl:acme:anonymous" {
		t.Errorf("key = %q, want rl:acme:anonymous", got)
	}
}

func TestRateLimitMiddleware_NilLimiter(t *testing.T) {
	var called bool
	handler := rateLimitMiddleware(nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("handler should be called when limiter is nil")
	}
}

func TestRateLimitMiddleware_NilConfig(t *testing.T) {
	rl := &RateLimiter{}
	var called bool
	handler := rateLimitMiddleware(rl, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("handler should be called when config is nil")
	}
}

func TestRateLimitMiddleware_ZeroMaxRequests(t *testing.T) {
	rl := &RateLimiter{}
	cfg := &meta.RateLimitConfig{MaxRequests: 0}
	var called bool
	handler := rateLimitMiddleware(rl, cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("handler should be called when MaxRequests is 0")
	}
}

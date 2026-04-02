package tenancy

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsActive(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{"active status", "active", true},
		{"empty status (backwards compat)", "", true},
		{"disabled status", "disabled", false},
		{"suspended status", "suspended", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := &SiteContext{Name: "test", Status: tt.status}
			if got := sc.IsActive(); got != tt.want {
				t.Errorf("IsActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPrefixRedisKey(t *testing.T) {
	sc := &SiteContext{Name: "acme", RedisPrefix: "acme:"}
	got := sc.PrefixRedisKey("meta:SalesOrder")
	want := "acme:meta:SalesOrder"
	if got != want {
		t.Errorf("PrefixRedisKey() = %q, want %q", got, want)
	}
}

func TestPrefixSearchIndex(t *testing.T) {
	sc := &SiteContext{Name: "acme"}
	got := sc.PrefixSearchIndex("SalesOrder")
	want := "acme_SalesOrder"
	if got != want {
		t.Errorf("PrefixSearchIndex() = %q, want %q", got, want)
	}
}

func TestErrSiteDisabled(t *testing.T) {
	wrapped := errors.New("resolve failed: " + ErrSiteDisabled.Error())
	_ = wrapped // basic sentinel check

	// Verify errors.Is works with wrapped errors.
	err := fmt.Errorf("resolve site %q: %w", "acme", ErrSiteDisabled)
	if !errors.Is(err, ErrSiteDisabled) {
		t.Error("errors.Is(wrappedErr, ErrSiteDisabled) should be true")
	}
}

package document

import (
	"context"
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/tenancy"
)

func TestNewDocContext(t *testing.T) {
	site := &tenancy.SiteContext{Name: "testsite"}
	user := &auth.User{Email: "user@test.com", Roles: []string{"Admin"}}

	dc := NewDocContext(context.Background(), site, user)
	if dc == nil {
		t.Fatal("expected non-nil DocContext")
	}
	if dc.Site != site {
		t.Error("Site not set")
	}
	if dc.User != user {
		t.Error("User not set")
	}
	if dc.Flags == nil {
		t.Error("Flags should be initialized")
	}
	if len(dc.Flags) != 0 {
		t.Errorf("Flags should be empty, got %d entries", len(dc.Flags))
	}
	if dc.EventBus == nil {
		t.Error("EventBus should be initialized")
	}
	if dc.TX != nil {
		t.Error("TX should be nil initially")
	}
	if dc.RequestID != "" {
		t.Errorf("RequestID should be empty, got %q", dc.RequestID)
	}
}

func TestNewDocContext_NilUser(t *testing.T) {
	site := &tenancy.SiteContext{Name: "testsite"}
	dc := NewDocContext(context.Background(), site, nil)
	if dc == nil {
		t.Fatal("expected non-nil DocContext")
	}
	if dc.User != nil {
		t.Error("User should be nil")
	}
}

func TestNewDocContext_NilSite(t *testing.T) {
	user := &auth.User{Email: "test@test.com"}
	dc := NewDocContext(context.Background(), nil, user)
	if dc == nil {
		t.Fatal("expected non-nil DocContext")
	}
	if dc.Site != nil {
		t.Error("Site should be nil")
	}
}

func TestDocContext_FlagsUsable(t *testing.T) {
	dc := NewDocContext(context.Background(), nil, nil)
	dc.Flags["skip_validation"] = true
	dc.Flags["silent"] = true

	if v, ok := dc.Flags["skip_validation"]; !ok || v != true {
		t.Error("expected skip_validation flag")
	}
	if v, ok := dc.Flags["silent"]; !ok || v != true {
		t.Error("expected silent flag")
	}
}

func TestDocContext_ImplementsContext(t *testing.T) {
	dc := NewDocContext(context.Background(), nil, nil)
	// DocContext embeds context.Context, so it should be usable as a context.
	var ctx context.Context = dc
	if ctx.Err() != nil {
		t.Error("context should not be cancelled")
	}
}

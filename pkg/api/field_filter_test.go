package api

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// ── Test helpers ────────────────────────────────────────────────────────────

func fieldNullLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(fieldNullWriter{}, nil))
}

type fieldNullWriter struct{}

func (fieldNullWriter) Write(p []byte) (int, error) { return len(p), nil }

func newFieldTestResolver(site, doctype string, perms []meta.PermRule) *auth.CachedPermissionResolver {
	reg := meta.NewRegistry(nil, nil, fieldNullLogger())
	mt := &meta.MetaType{Name: doctype, Module: "core", Permissions: perms}
	reg.SeedL1ForTest(site, doctype, mt)
	return auth.NewCachedPermissionResolver(reg, nil, nil, nil)
}

func fieldTestCtx(user *auth.User, site *tenancy.SiteContext) context.Context {
	ctx := context.Background()
	ctx = WithUser(ctx, user)
	ctx = WithSite(ctx, site)
	return ctx
}

// ── FieldLevelTransformer Response Tests ────────────────────────────────────

func TestFieldLevelTransformer_Response_Restricted(t *testing.T) {
	resolver := newFieldTestResolver("test_site", "SalesOrder", []meta.PermRule{
		{
			Role:           "Sales User",
			DocTypePerm:    int(auth.PermRead),
			FieldLevelRead: []string{"customer_name", "grand_total"},
		},
	})
	transformer := NewFieldLevelTransformer(resolver)

	user := &auth.User{Email: "bob@test.com", Roles: []string{"Sales User"}}
	ctx := fieldTestCtx(user, testSite)

	body := map[string]any{
		"name":          "SO-0001",
		"creation":      "2025-01-01",
		"modified":      "2025-01-02",
		"owner":         "admin@test.com",
		"docstatus":     0,
		"customer_name": "Acme Corp",
		"grand_total":   1000.00,
		"territory":     "US West",
		"company":       "Test Inc",
	}

	mt := &meta.MetaType{Name: "SalesOrder"}
	result, err := transformer.TransformResponse(ctx, mt, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"name", "creation", "modified", "owner", "docstatus", "customer_name", "grand_total"}
	sort.Strings(expected)
	var got []string
	for k := range result {
		got = append(got, k)
	}
	sort.Strings(got)

	if len(got) != len(expected) {
		t.Fatalf("expected %d fields %v, got %d fields %v", len(expected), expected, len(got), got)
	}
	for i, k := range expected {
		if got[i] != k {
			t.Errorf("field mismatch at %d: expected %q, got %q", i, k, got[i])
		}
	}

	if _, ok := result["territory"]; ok {
		t.Error("expected territory to be stripped")
	}
	if _, ok := result["company"]; ok {
		t.Error("expected company to be stripped")
	}
}

func TestFieldLevelTransformer_Response_Unrestricted(t *testing.T) {
	resolver := newFieldTestResolver("test_site", "Item", []meta.PermRule{
		{Role: "User", DocTypePerm: int(auth.PermRead)},
	})
	transformer := NewFieldLevelTransformer(resolver)

	user := &auth.User{Email: "bob@test.com", Roles: []string{"User"}}
	ctx := fieldTestCtx(user, testSite)

	body := map[string]any{
		"name":        "ITEM-001",
		"description": "A test item",
		"price":       42.0,
	}

	mt := &meta.MetaType{Name: "Item"}
	result, err := transformer.TransformResponse(ctx, mt, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != len(body) {
		t.Errorf("expected all %d fields returned, got %d", len(body), len(result))
	}
}

func TestFieldLevelTransformer_Response_AdminBypass(t *testing.T) {
	resolver := newFieldTestResolver("test_site", "SalesOrder", []meta.PermRule{
		{
			Role:           "Sales User",
			DocTypePerm:    int(auth.PermRead),
			FieldLevelRead: []string{"customer_name"},
		},
	})
	transformer := NewFieldLevelTransformer(resolver)

	admin := &auth.User{Email: "admin@test.com", Roles: []string{"Administrator"}}
	ctx := fieldTestCtx(admin, testSite)

	body := map[string]any{
		"name":          "SO-0001",
		"customer_name": "Acme",
		"grand_total":   1000,
		"territory":     "US",
	}

	mt := &meta.MetaType{Name: "SalesOrder"}
	result, err := transformer.TransformResponse(ctx, mt, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != len(body) {
		t.Errorf("expected Administrator to see all %d fields, got %d", len(body), len(result))
	}
}

func TestFieldLevelTransformer_Response_NilUser(t *testing.T) {
	resolver := newFieldTestResolver("test_site", "Item", []meta.PermRule{
		{Role: "User", DocTypePerm: int(auth.PermRead), FieldLevelRead: []string{"name"}},
	})
	transformer := NewFieldLevelTransformer(resolver)

	ctx := context.Background() // no user in context

	body := map[string]any{"name": "X", "price": 10}
	mt := &meta.MetaType{Name: "Item"}
	result, err := transformer.TransformResponse(ctx, mt, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != len(body) {
		t.Errorf("expected pass-through when no user, got filtered result")
	}
}

// ── FieldLevelTransformer Request Tests ─────────────────────────────────────

func TestFieldLevelTransformer_Request_WriteRejected(t *testing.T) {
	resolver := newFieldTestResolver("test_site", "SalesOrder", []meta.PermRule{
		{
			Role:            "Sales User",
			DocTypePerm:     int(auth.PermWrite),
			FieldLevelWrite: []string{"status"},
		},
	})
	transformer := NewFieldLevelTransformer(resolver)

	user := &auth.User{Email: "bob@test.com", Roles: []string{"Sales User"}}
	ctx := fieldTestCtx(user, testSite)

	body := map[string]any{
		"status":      "Submitted",
		"grand_total": 9999,
	}

	mt := &meta.MetaType{Name: "SalesOrder"}
	_, err := transformer.TransformRequest(ctx, mt, body)
	if err == nil {
		t.Fatal("expected error for unauthorized field write")
	}

	var permErr *PermissionDeniedError
	if !errors.As(err, &permErr) {
		t.Fatalf("expected *PermissionDeniedError, got %T: %v", err, err)
	}
}

func TestFieldLevelTransformer_Request_WriteAllowed(t *testing.T) {
	resolver := newFieldTestResolver("test_site", "SalesOrder", []meta.PermRule{
		{
			Role:            "Sales User",
			DocTypePerm:     int(auth.PermWrite),
			FieldLevelWrite: []string{"status", "priority"},
		},
	})
	transformer := NewFieldLevelTransformer(resolver)

	user := &auth.User{Email: "bob@test.com", Roles: []string{"Sales User"}}
	ctx := fieldTestCtx(user, testSite)

	body := map[string]any{
		"status":   "Draft",
		"priority": "High",
	}

	mt := &meta.MetaType{Name: "SalesOrder"}
	result, err := transformer.TransformRequest(ctx, mt, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 fields, got %d", len(result))
	}
}

func TestFieldLevelTransformer_Request_SystemFieldsAllowed(t *testing.T) {
	resolver := newFieldTestResolver("test_site", "SalesOrder", []meta.PermRule{
		{
			Role:            "Sales User",
			DocTypePerm:     int(auth.PermWrite),
			FieldLevelWrite: []string{"status"},
		},
	})
	transformer := NewFieldLevelTransformer(resolver)

	user := &auth.User{Email: "bob@test.com", Roles: []string{"Sales User"}}
	ctx := fieldTestCtx(user, testSite)

	body := map[string]any{
		"name":   "SO-0001",
		"status": "Draft",
	}

	mt := &meta.MetaType{Name: "SalesOrder"}
	_, err := transformer.TransformRequest(ctx, mt, body)
	if err != nil {
		t.Fatalf("expected system field 'name' to be allowed, got: %v", err)
	}
}

func TestFieldLevelTransformer_Request_Unrestricted(t *testing.T) {
	resolver := newFieldTestResolver("test_site", "Item", []meta.PermRule{
		{Role: "User", DocTypePerm: int(auth.PermWrite)},
	})
	transformer := NewFieldLevelTransformer(resolver)

	user := &auth.User{Email: "bob@test.com", Roles: []string{"User"}}
	ctx := fieldTestCtx(user, testSite)

	body := map[string]any{
		"anything": "should work",
		"price":    42,
	}

	mt := &meta.MetaType{Name: "Item"}
	result, err := transformer.TransformRequest(ctx, mt, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != len(body) {
		t.Errorf("expected all fields allowed, got filtered")
	}
}

func TestFieldLevelTransformer_Request_AdminBypass(t *testing.T) {
	resolver := newFieldTestResolver("test_site", "SalesOrder", []meta.PermRule{
		{
			Role:            "Sales User",
			DocTypePerm:     int(auth.PermWrite),
			FieldLevelWrite: []string{"status"},
		},
	})
	transformer := NewFieldLevelTransformer(resolver)

	admin := &auth.User{Email: "admin@test.com", Roles: []string{"Administrator"}}
	ctx := fieldTestCtx(admin, testSite)

	body := map[string]any{
		"status":      "Draft",
		"grand_total": 9999,
	}

	mt := &meta.MetaType{Name: "SalesOrder"}
	_, err := transformer.TransformRequest(ctx, mt, body)
	if err != nil {
		t.Fatalf("expected Administrator to bypass write restriction, got: %v", err)
	}
}

// ── FieldLevelTransformer Multi-Role Union ──────────────────────────────────

func TestFieldLevelTransformer_Response_MultiRoleUnion(t *testing.T) {
	resolver := newFieldTestResolver("test_site", "SalesOrder", []meta.PermRule{
		{
			Role:           "Sales User",
			DocTypePerm:    int(auth.PermRead),
			FieldLevelRead: []string{"customer_name"},
		},
		{
			Role:           "Finance User",
			DocTypePerm:    int(auth.PermRead),
			FieldLevelRead: []string{"grand_total"},
		},
	})
	transformer := NewFieldLevelTransformer(resolver)

	user := &auth.User{Email: "bob@test.com", Roles: []string{"Sales User", "Finance User"}}
	ctx := fieldTestCtx(user, testSite)

	body := map[string]any{
		"name":          "SO-0001",
		"customer_name": "Acme",
		"grand_total":   1000,
		"territory":     "US",
	}

	mt := &meta.MetaType{Name: "SalesOrder"}
	result, err := transformer.TransformResponse(ctx, mt, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result["customer_name"]; !ok {
		t.Error("expected customer_name from Sales User role")
	}
	if _, ok := result["grand_total"]; !ok {
		t.Error("expected grand_total from Finance User role")
	}
	if _, ok := result["territory"]; ok {
		t.Error("expected territory to be stripped")
	}
}

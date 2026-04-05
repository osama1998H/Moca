package api

import (
	"context"
	"errors"
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/meta"
)

// ── scopeAllows tests ─────────────────────────────────────────────────────────

func TestScopeAllows(t *testing.T) {
	tests := []struct { //nolint:govet // test struct alignment is fine
		name      string
		scopes    []meta.APIScopePerm
		doctype   string
		operation string
		want      bool
	}{
		{
			name: "exact match",
			scopes: []meta.APIScopePerm{
				{Scope: "orders:read", DocTypes: []string{"SalesOrder"}, Operations: []string{"read"}},
			},
			doctype: "SalesOrder", operation: "read",
			want: true,
		},
		{
			name: "wrong doctype",
			scopes: []meta.APIScopePerm{
				{Scope: "orders:read", DocTypes: []string{"SalesOrder"}, Operations: []string{"read"}},
			},
			doctype: "PurchaseOrder", operation: "read",
			want: false,
		},
		{
			name: "wrong operation",
			scopes: []meta.APIScopePerm{
				{Scope: "orders:read", DocTypes: []string{"SalesOrder"}, Operations: []string{"read"}},
			},
			doctype: "SalesOrder", operation: "create",
			want: false,
		},
		{
			name: "wildcard doctype (empty list)",
			scopes: []meta.APIScopePerm{
				{Scope: "all:read", DocTypes: nil, Operations: []string{"read"}},
			},
			doctype: "AnyDocType", operation: "read",
			want: true,
		},
		{
			name: "wildcard operation (empty list)",
			scopes: []meta.APIScopePerm{
				{Scope: "orders:all", DocTypes: []string{"SalesOrder"}, Operations: nil},
			},
			doctype: "SalesOrder", operation: "delete",
			want: true,
		},
		{
			name: "full wildcard",
			scopes: []meta.APIScopePerm{
				{Scope: "admin", DocTypes: nil, Operations: nil},
			},
			doctype: "User", operation: "write",
			want: true,
		},
		{
			name: "multiple scopes, second matches",
			scopes: []meta.APIScopePerm{
				{Scope: "orders:read", DocTypes: []string{"SalesOrder"}, Operations: []string{"read"}},
				{Scope: "items:write", DocTypes: []string{"Item"}, Operations: []string{"write", "create"}},
			},
			doctype: "Item", operation: "create",
			want: true,
		},
		{
			name: "no scopes at all",
			scopes: nil,
			doctype: "SalesOrder", operation: "read",
			want: false,
		},
		{
			name: "empty scopes slice",
			scopes: []meta.APIScopePerm{},
			doctype: "SalesOrder", operation: "read",
			want: false,
		},
		{
			name: "case insensitive doctype",
			scopes: []meta.APIScopePerm{
				{Scope: "orders:read", DocTypes: []string{"salesorder"}, Operations: []string{"read"}},
			},
			doctype: "SalesOrder", operation: "read",
			want: true,
		},
		{
			name: "case insensitive operation",
			scopes: []meta.APIScopePerm{
				{Scope: "orders:read", DocTypes: []string{"SalesOrder"}, Operations: []string{"READ"}},
			},
			doctype: "SalesOrder", operation: "read",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scopeAllows(tt.scopes, tt.doctype, tt.operation)
			if got != tt.want {
				t.Errorf("scopeAllows() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ── ScopeEnforcer tests ───────────────────────────────────────────────────────

// mockPermChecker is a configurable mock for PermissionChecker.
type mockPermChecker struct {
	err error
}

func (m *mockPermChecker) CheckDocPerm(_ context.Context, _ *auth.User, _ string, _ string) error {
	return m.err
}

func TestScopeEnforcer_WithScopes_Allowed(t *testing.T) {
	inner := &mockPermChecker{err: nil}
	enforcer := NewScopeEnforcer(inner)

	scopes := []meta.APIScopePerm{
		{Scope: "orders:read", DocTypes: []string{"SalesOrder"}, Operations: []string{"read"}},
	}
	ctx := WithAPIScopes(context.Background(), scopes)
	user := &auth.User{Email: "test@example.com", Roles: []string{"User"}}

	err := enforcer.CheckDocPerm(ctx, user, "SalesOrder", "read")
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestScopeEnforcer_WithScopes_Denied(t *testing.T) {
	inner := &mockPermChecker{err: nil}
	enforcer := NewScopeEnforcer(inner)

	scopes := []meta.APIScopePerm{
		{Scope: "orders:read", DocTypes: []string{"SalesOrder"}, Operations: []string{"read"}},
	}
	ctx := WithAPIScopes(context.Background(), scopes)
	user := &auth.User{Email: "test@example.com", Roles: []string{"User"}}

	// Try to create — should be denied by scope.
	err := enforcer.CheckDocPerm(ctx, user, "SalesOrder", "create")
	if err == nil {
		t.Fatal("expected scope denial error, got nil")
	}
	var permErr *PermissionDeniedError
	if !errors.As(err, &permErr) {
		t.Fatalf("expected *PermissionDeniedError, got %T: %v", err, err)
	}
	if permErr.Perm != "scope:create" {
		t.Errorf("expected perm 'scope:create', got %q", permErr.Perm)
	}
}

func TestScopeEnforcer_NoScopes_PassThrough(t *testing.T) {
	inner := &mockPermChecker{err: nil}
	enforcer := NewScopeEnforcer(inner)

	// No scopes in context — should pass through to inner checker.
	ctx := context.Background()
	user := &auth.User{Email: "test@example.com", Roles: []string{"User"}}

	err := enforcer.CheckDocPerm(ctx, user, "SalesOrder", "create")
	if err != nil {
		t.Errorf("expected pass-through nil, got %v", err)
	}
}

func TestScopeEnforcer_NoScopes_InnerDenied(t *testing.T) {
	inner := &mockPermChecker{err: &PermissionDeniedError{User: "test", Doctype: "SalesOrder", Perm: "create"}}
	enforcer := NewScopeEnforcer(inner)

	ctx := context.Background()
	user := &auth.User{Email: "test@example.com", Roles: []string{"User"}}

	err := enforcer.CheckDocPerm(ctx, user, "SalesOrder", "create")
	if err == nil {
		t.Fatal("expected inner denial error, got nil")
	}
}

func TestScopeEnforcer_ScopeAllowed_InnerDenied(t *testing.T) {
	// Scope allows the operation, but inner checker denies.
	inner := &mockPermChecker{err: &PermissionDeniedError{User: "test", Doctype: "SalesOrder", Perm: "read"}}
	enforcer := NewScopeEnforcer(inner)

	scopes := []meta.APIScopePerm{
		{Scope: "orders:read", DocTypes: []string{"SalesOrder"}, Operations: []string{"read"}},
	}
	ctx := WithAPIScopes(context.Background(), scopes)
	user := &auth.User{Email: "test@example.com", Roles: []string{"User"}}

	err := enforcer.CheckDocPerm(ctx, user, "SalesOrder", "read")
	if err == nil {
		t.Fatal("expected inner denial error even though scope allows, got nil")
	}
}

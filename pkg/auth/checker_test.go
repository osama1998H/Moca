package auth_test

import (
	"context"
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/tenancy"
)

func seedCheckerRegistry(t *testing.T, site, doctype string, perms []meta.PermRule) *meta.Registry {
	t.Helper()
	r := meta.NewRegistry(nil, nil, nullLogger())
	mt := &meta.MetaType{Name: doctype, Module: "core", Permissions: perms}
	r.SeedL1ForTest(site, doctype, mt)
	return r
}

func newTestResolver(t *testing.T, site, doctype string, perms []meta.PermRule) *auth.CachedPermissionResolver {
	t.Helper()
	reg := seedCheckerRegistry(t, site, doctype, perms)
	return auth.NewCachedPermissionResolver(reg, nil, nil, nil)
}

var checkerTestSite = &tenancy.SiteContext{Name: "test_site"}

func siteExtractor(_ context.Context) *tenancy.SiteContext {
	return checkerTestSite
}

func nilSiteExtractor(_ context.Context) *tenancy.SiteContext {
	return nil
}

func TestRoleBasedPermChecker_AdminBypass(t *testing.T) {
	resolver := newTestResolver(t, "test_site", "SalesOrder", []meta.PermRule{
		{Role: "Guest", DocTypePerm: 0},
	})
	checker := auth.NewRoleBasedPermChecker(resolver, siteExtractor, nil)

	admin := &auth.User{Email: "admin@test.com", Roles: []string{"Administrator"}}
	if err := checker.CheckDocPerm(context.Background(), admin, "SalesOrder", "delete"); err != nil {
		t.Errorf("expected Administrator to bypass, got: %v", err)
	}
}

func TestRoleBasedPermChecker_PermGranted(t *testing.T) {
	resolver := newTestResolver(t, "test_site", "SalesOrder", []meta.PermRule{
		{Role: "Sales User", DocTypePerm: int(auth.PermRead | auth.PermCreate)},
	})
	checker := auth.NewRoleBasedPermChecker(resolver, siteExtractor, nil)

	user := &auth.User{Email: "bob@test.com", Roles: []string{"Sales User"}}

	if err := checker.CheckDocPerm(context.Background(), user, "SalesOrder", "read"); err != nil {
		t.Errorf("expected read to be granted, got: %v", err)
	}
	if err := checker.CheckDocPerm(context.Background(), user, "SalesOrder", "create"); err != nil {
		t.Errorf("expected create to be granted, got: %v", err)
	}
}

func TestRoleBasedPermChecker_PermDenied(t *testing.T) {
	resolver := newTestResolver(t, "test_site", "SalesOrder", []meta.PermRule{
		{Role: "Sales User", DocTypePerm: int(auth.PermRead | auth.PermCreate)},
	})
	checker := auth.NewRoleBasedPermChecker(resolver, siteExtractor, nil)

	user := &auth.User{Email: "bob@test.com", Roles: []string{"Sales User"}}

	err := checker.CheckDocPerm(context.Background(), user, "SalesOrder", "delete")
	if err == nil {
		t.Fatal("expected delete to be denied")
	}

	var permErr *auth.PermDeniedError
	if e, ok := err.(*auth.PermDeniedError); !ok {
		t.Fatalf("expected *PermDeniedError, got: %T: %v", err, err)
	} else {
		permErr = e
	}
	if permErr.User != "bob@test.com" {
		t.Errorf("expected user bob@test.com, got %q", permErr.User)
	}
	if permErr.Perm != "delete" {
		t.Errorf("expected perm delete, got %q", permErr.Perm)
	}
}

func TestRoleBasedPermChecker_MissingSite(t *testing.T) {
	resolver := newTestResolver(t, "test_site", "Item", []meta.PermRule{
		{Role: "User", DocTypePerm: int(auth.PermRead)},
	})
	checker := auth.NewRoleBasedPermChecker(resolver, nilSiteExtractor, nil)

	user := &auth.User{Email: "bob@test.com", Roles: []string{"User"}}
	err := checker.CheckDocPerm(context.Background(), user, "Item", "read")
	if err == nil {
		t.Fatal("expected error when site is nil")
	}
}

func TestRoleBasedPermChecker_MultiRole(t *testing.T) {
	resolver := newTestResolver(t, "test_site", "SalesOrder", []meta.PermRule{
		{Role: "Sales User", DocTypePerm: int(auth.PermRead | auth.PermCreate)},
		{Role: "Customer User", DocTypePerm: int(auth.PermRead)},
	})
	checker := auth.NewRoleBasedPermChecker(resolver, siteExtractor, nil)

	user := &auth.User{Email: "bob@test.com", Roles: []string{"Sales User", "Customer User"}}

	if err := checker.CheckDocPerm(context.Background(), user, "SalesOrder", "create"); err != nil {
		t.Errorf("expected create allowed via Sales User role, got: %v", err)
	}
	if err := checker.CheckDocPerm(context.Background(), user, "SalesOrder", "delete"); err == nil {
		t.Error("expected delete denied for Sales User + Customer User")
	}
}

func TestIsAdministrator(t *testing.T) {
	cases := []struct {
		roles []string
		want  bool
	}{
		{[]string{"Administrator"}, true},
		{[]string{"User", "Administrator"}, true},
		{[]string{"User", "Guest"}, false},
		{nil, false},
	}
	for _, tc := range cases {
		user := &auth.User{Roles: tc.roles}
		if got := auth.IsAdministrator(user); got != tc.want {
			t.Errorf("IsAdministrator(%v) = %v, want %v", tc.roles, got, tc.want)
		}
	}
}

package auth_test

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/moca-framework/moca/pkg/auth"
	"github.com/moca-framework/moca/pkg/meta"
)

// ── ResolvePermissions ───────────────────────────────────────────────────────

func TestResolvePermissions_MultiRole(t *testing.T) {
	rules := []meta.PermRule{
		{Role: "Sales User", DocTypePerm: int(auth.PermRead | auth.PermCreate)},
		{Role: "Customer User", DocTypePerm: int(auth.PermRead)},
	}
	ep := auth.ResolvePermissions(rules, []string{"Sales User", "Customer User"})

	if !ep.HasPerm("read") {
		t.Error("expected read=true")
	}
	if !ep.HasPerm("create") {
		t.Error("expected create=true")
	}
	if ep.HasPerm("delete") {
		t.Error("expected delete=false")
	}
	if ep.HasPerm("write") {
		t.Error("expected write=false")
	}
}

func TestResolvePermissions_NoMatchingRoles(t *testing.T) {
	rules := []meta.PermRule{
		{Role: "Admin", DocTypePerm: int(auth.PermRead | auth.PermWrite)},
	}
	ep := auth.ResolvePermissions(rules, []string{"Guest"})

	if ep.DocTypePerm != 0 {
		t.Errorf("expected zero bitmask, got %d", ep.DocTypePerm)
	}
	if ep.HasPerm("read") {
		t.Error("expected read=false")
	}
}

func TestResolvePermissions_EmptyRoles(t *testing.T) {
	rules := []meta.PermRule{
		{Role: "Admin", DocTypePerm: int(auth.PermRead)},
	}
	ep := auth.ResolvePermissions(rules, nil)
	if ep.DocTypePerm != 0 {
		t.Errorf("expected zero bitmask, got %d", ep.DocTypePerm)
	}
}

func TestResolvePermissions_EmptyRules(t *testing.T) {
	ep := auth.ResolvePermissions(nil, []string{"Admin"})
	if ep.DocTypePerm != 0 {
		t.Errorf("expected zero bitmask, got %d", ep.DocTypePerm)
	}
}

func TestResolvePermissions_BitmaskUnion(t *testing.T) {
	rules := []meta.PermRule{
		{Role: "A", DocTypePerm: int(auth.PermRead)},
		{Role: "B", DocTypePerm: int(auth.PermWrite)},
		{Role: "C", DocTypePerm: int(auth.PermDelete)},
	}
	ep := auth.ResolvePermissions(rules, []string{"A", "B", "C"})

	want := auth.PermRead | auth.PermWrite | auth.PermDelete
	if ep.DocTypePerm != want {
		t.Errorf("expected bitmask %d, got %d", want, ep.DocTypePerm)
	}
}

func TestResolvePermissions_FieldLevelUnion(t *testing.T) {
	rules := []meta.PermRule{
		{Role: "A", DocTypePerm: int(auth.PermRead), FieldLevelRead: []string{"name", "email"}},
		{Role: "B", DocTypePerm: int(auth.PermRead), FieldLevelRead: []string{"email", "phone"}},
	}
	ep := auth.ResolvePermissions(rules, []string{"A", "B"})

	got := make(map[string]bool)
	for _, f := range ep.FieldLevelRead {
		got[f] = true
	}
	for _, want := range []string{"name", "email", "phone"} {
		if !got[want] {
			t.Errorf("expected field %q in FieldLevelRead", want)
		}
	}
	if len(ep.FieldLevelRead) != 3 {
		t.Errorf("expected 3 fields, got %d", len(ep.FieldLevelRead))
	}
}

func TestResolvePermissions_FieldLevelEmpty(t *testing.T) {
	rules := []meta.PermRule{
		{Role: "A", DocTypePerm: int(auth.PermRead)},
	}
	ep := auth.ResolvePermissions(rules, []string{"A"})
	if ep.FieldLevelRead != nil {
		t.Errorf("expected nil FieldLevelRead (unrestricted), got %v", ep.FieldLevelRead)
	}
	if ep.FieldLevelWrite != nil {
		t.Errorf("expected nil FieldLevelWrite (unrestricted), got %v", ep.FieldLevelWrite)
	}
}

func TestResolvePermissions_MatchConditions(t *testing.T) {
	rules := []meta.PermRule{
		{Role: "A", DocTypePerm: int(auth.PermRead), MatchField: "company", MatchValue: "company"},
		{Role: "B", DocTypePerm: int(auth.PermRead), MatchField: "territory", MatchValue: "territory"},
	}
	ep := auth.ResolvePermissions(rules, []string{"A", "B"})

	if len(ep.MatchConditions) != 2 {
		t.Fatalf("expected 2 match conditions, got %d", len(ep.MatchConditions))
	}
	if ep.MatchConditions[0].Field != "company" || ep.MatchConditions[0].Value != "company" {
		t.Errorf("unexpected match condition[0]: %+v", ep.MatchConditions[0])
	}
	if ep.MatchConditions[1].Field != "territory" || ep.MatchConditions[1].Value != "territory" {
		t.Errorf("unexpected match condition[1]: %+v", ep.MatchConditions[1])
	}
}

func TestResolvePermissions_MatchConditionIgnoredWhenPartial(t *testing.T) {
	rules := []meta.PermRule{
		{Role: "A", DocTypePerm: int(auth.PermRead), MatchField: "company"},     // no MatchValue
		{Role: "B", DocTypePerm: int(auth.PermRead), MatchValue: "territory"},    // no MatchField
	}
	ep := auth.ResolvePermissions(rules, []string{"A", "B"})
	if len(ep.MatchConditions) != 0 {
		t.Errorf("expected 0 match conditions for partial fields, got %d", len(ep.MatchConditions))
	}
}

func TestResolvePermissions_CustomRulesDedup(t *testing.T) {
	rules := []meta.PermRule{
		{Role: "A", DocTypePerm: int(auth.PermRead), CustomRule: "require_active_subscription"},
		{Role: "B", DocTypePerm: int(auth.PermRead), CustomRule: "require_active_subscription"},
		{Role: "C", DocTypePerm: int(auth.PermRead), CustomRule: "check_quota"},
	}
	ep := auth.ResolvePermissions(rules, []string{"A", "B", "C"})

	sort.Strings(ep.CustomRules)
	if len(ep.CustomRules) != 2 {
		t.Fatalf("expected 2 custom rules, got %d: %v", len(ep.CustomRules), ep.CustomRules)
	}
}

func TestResolvePermissions_FieldLevelWriteUnion(t *testing.T) {
	rules := []meta.PermRule{
		{Role: "A", DocTypePerm: int(auth.PermWrite), FieldLevelWrite: []string{"status"}},
		{Role: "B", DocTypePerm: int(auth.PermWrite), FieldLevelWrite: []string{"status", "priority"}},
	}
	ep := auth.ResolvePermissions(rules, []string{"A", "B"})
	got := make(map[string]bool)
	for _, f := range ep.FieldLevelWrite {
		got[f] = true
	}
	if len(got) != 2 || !got["status"] || !got["priority"] {
		t.Errorf("expected {status, priority}, got %v", ep.FieldLevelWrite)
	}
}

// ── HasPerm ──────────────────────────────────────────────────────────────────

func TestHasPerm_InvalidName(t *testing.T) {
	ep := &auth.EffectivePerms{DocTypePerm: auth.PermRead | auth.PermWrite}
	if ep.HasPerm("nonexistent") {
		t.Error("expected false for invalid perm name")
	}
}

// ── PermFromString ───────────────────────────────────────────────────────────

func TestPermFromString(t *testing.T) {
	cases := []struct {
		name string
		want auth.Perm
		ok   bool
	}{
		{"read", auth.PermRead, true},
		{"write", auth.PermWrite, true},
		{"create", auth.PermCreate, true},
		{"delete", auth.PermDelete, true},
		{"submit", auth.PermSubmit, true},
		{"cancel", auth.PermCancel, true},
		{"amend", auth.PermAmend, true},
		{"invalid", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		got, ok := auth.PermFromString(tc.name)
		if got != tc.want || ok != tc.ok {
			t.Errorf("PermFromString(%q) = (%d, %v), want (%d, %v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

// ── CustomRuleRegistry ───────────────────────────────────────────────────────

func TestCustomRuleRegistry_RegisterAndEvaluate(t *testing.T) {
	reg := auth.NewCustomRuleRegistry()
	called := false
	err := reg.Register("require_active_subscription", func(_ context.Context, _ *auth.User, _ string) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	user := &auth.User{Email: "test@example.com", Roles: []string{"User"}}
	err = reg.Evaluate(context.Background(), "require_active_subscription", user, "SalesOrder")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !called {
		t.Error("expected custom rule to be called")
	}
}

func TestCustomRuleRegistry_DuplicateRegister(t *testing.T) {
	reg := auth.NewCustomRuleRegistry()
	noop := func(_ context.Context, _ *auth.User, _ string) error { return nil }
	if err := reg.Register("rule1", noop); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := reg.Register("rule1", noop); err == nil {
		t.Fatal("expected error on duplicate registration")
	}
}

func TestCustomRuleRegistry_EvaluateUnregistered(t *testing.T) {
	reg := auth.NewCustomRuleRegistry()
	user := &auth.User{Email: "test@example.com"}
	err := reg.Evaluate(context.Background(), "nonexistent", user, "DocType")
	if err == nil {
		t.Fatal("expected error for unregistered rule")
	}
}

func TestCustomRuleRegistry_EvaluateAll_FailFast(t *testing.T) {
	reg := auth.NewCustomRuleRegistry()
	order := []string{}

	_ = reg.Register("pass", func(_ context.Context, _ *auth.User, _ string) error {
		order = append(order, "pass")
		return nil
	})
	_ = reg.Register("deny", func(_ context.Context, _ *auth.User, _ string) error {
		order = append(order, "deny")
		return errors.New("subscription expired")
	})
	_ = reg.Register("never", func(_ context.Context, _ *auth.User, _ string) error {
		order = append(order, "never")
		return nil
	})

	user := &auth.User{Email: "test@example.com"}
	err := reg.EvaluateAll(context.Background(), []string{"pass", "deny", "never"}, user, "DocType")
	if err == nil {
		t.Fatal("expected error from EvaluateAll")
	}
	if len(order) != 2 || order[0] != "pass" || order[1] != "deny" {
		t.Errorf("expected [pass, deny], got %v", order)
	}
}

func TestCustomRuleRegistry_EvaluateAll_AllPass(t *testing.T) {
	reg := auth.NewCustomRuleRegistry()
	noop := func(_ context.Context, _ *auth.User, _ string) error { return nil }
	_ = reg.Register("a", noop)
	_ = reg.Register("b", noop)

	user := &auth.User{Email: "test@example.com"}
	err := reg.EvaluateAll(context.Background(), []string{"a", "b"}, user, "DocType")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

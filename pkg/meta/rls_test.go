package meta

import (
	"strings"
	"testing"
)

func TestGenerateRLSPolicies_BasicMatch(t *testing.T) {
	mt := &MetaType{
		Name: "SalesOrder",
		Permissions: []PermRule{
			{
				Role:        "Sales User",
				DocTypePerm: 1, // read
				MatchField:  "company",
				MatchValue:  "company",
			},
		},
	}

	stmts := GenerateRLSPolicies(mt)
	if len(stmts) == 0 {
		t.Fatal("expected DDL statements")
	}

	// Should contain: ENABLE RLS, FORCE RLS, admin bypass (drop+create), match policy (drop+create) = 6
	if len(stmts) != 6 {
		t.Fatalf("expected 6 statements, got %d", len(stmts))
	}

	// Verify ENABLE RLS.
	if !strings.Contains(stmts[0].SQL, "ENABLE ROW LEVEL SECURITY") {
		t.Errorf("stmt[0] should enable RLS: %s", stmts[0].SQL)
	}

	// Verify FORCE RLS.
	if !strings.Contains(stmts[1].SQL, "FORCE ROW LEVEL SECURITY") {
		t.Errorf("stmt[1] should force RLS: %s", stmts[1].SQL)
	}

	// Verify admin bypass policy creation (stmt[3] after drop at stmt[2]).
	if !strings.Contains(stmts[3].SQL, "moca.is_admin") {
		t.Errorf("stmt[3] should be admin bypass policy: %s", stmts[3].SQL)
	}

	// Verify match policy (stmt[5] after drop at stmt[4]).
	matchSQL := stmts[5].SQL
	if !strings.Contains(matchSQL, `"company"`) {
		t.Errorf("match policy should reference company column: %s", matchSQL)
	}
	if !strings.Contains(matchSQL, "moca.current_user_company") {
		t.Errorf("match policy should use moca.current_user_company GUC: %s", matchSQL)
	}
	if !strings.Contains(matchSQL, "FOR ALL") {
		t.Errorf("match policy should be FOR ALL: %s", matchSQL)
	}
}

func TestGenerateRLSPolicies_MultipleRoles(t *testing.T) {
	mt := &MetaType{
		Name: "SalesOrder",
		Permissions: []PermRule{
			{
				Role:        "Sales User",
				DocTypePerm: 1,
				MatchField:  "company",
				MatchValue:  "company",
			},
			{
				Role:        "Territory Manager",
				DocTypePerm: 1,
				MatchField:  "territory",
				MatchValue:  "territory",
			},
		},
	}

	stmts := GenerateRLSPolicies(mt)
	// ENABLE + FORCE + admin(drop+create) + match0(drop+create) + match1(drop+create) = 8
	if len(stmts) != 8 {
		t.Fatalf("expected 8 statements for 2 match conditions, got %d", len(stmts))
	}

	// Verify both match policies exist.
	var foundCompany, foundTerritory bool
	for _, s := range stmts {
		if strings.Contains(s.SQL, "moca.current_user_company") {
			foundCompany = true
		}
		if strings.Contains(s.SQL, "moca.current_user_territory") {
			foundTerritory = true
		}
	}
	if !foundCompany {
		t.Error("expected company match policy")
	}
	if !foundTerritory {
		t.Error("expected territory match policy")
	}
}

func TestGenerateRLSPolicies_DeduplicatesConditions(t *testing.T) {
	mt := &MetaType{
		Name: "SalesOrder",
		Permissions: []PermRule{
			{
				Role:        "Sales User",
				DocTypePerm: 1,
				MatchField:  "company",
				MatchValue:  "company",
			},
			{
				Role:        "Sales Manager",
				DocTypePerm: 3,
				MatchField:  "company",
				MatchValue:  "company",
			},
		},
	}

	stmts := GenerateRLSPolicies(mt)
	// Same (match_field, match_value) → only 1 match policy.
	// ENABLE + FORCE + admin(drop+create) + match0(drop+create) = 6
	if len(stmts) != 6 {
		t.Fatalf("expected 6 statements (deduplicated), got %d", len(stmts))
	}
}

func TestGenerateRLSPolicies_AdminExcluded(t *testing.T) {
	mt := &MetaType{
		Name: "SalesOrder",
		Permissions: []PermRule{
			{
				Role:        "Administrator",
				DocTypePerm: 127, // all perms
				MatchField:  "company",
				MatchValue:  "company",
			},
		},
	}

	stmts := GenerateRLSPolicies(mt)
	// Administrator match rules are excluded → no match policies.
	// ENABLE + FORCE + admin(drop+create) = 4
	if len(stmts) != 4 {
		t.Fatalf("expected 4 statements (no match policies for Administrator), got %d", len(stmts))
	}
}

func TestGenerateRLSPolicies_VirtualSkipped(t *testing.T) {
	mt := &MetaType{
		Name:      "VirtualType",
		IsVirtual: true,
		Permissions: []PermRule{
			{Role: "Sales User", MatchField: "company", MatchValue: "company"},
		},
	}

	if stmts := GenerateRLSPolicies(mt); stmts != nil {
		t.Errorf("expected nil for virtual MetaType, got %d statements", len(stmts))
	}
}

func TestGenerateRLSPolicies_SingleSkipped(t *testing.T) {
	mt := &MetaType{
		Name:     "SystemSettings",
		IsSingle: true,
		Permissions: []PermRule{
			{Role: "Sales User", MatchField: "company", MatchValue: "company"},
		},
	}

	if stmts := GenerateRLSPolicies(mt); stmts != nil {
		t.Errorf("expected nil for single MetaType, got %d statements", len(stmts))
	}
}

func TestGenerateRLSPolicies_ChildTableSkipped(t *testing.T) {
	mt := &MetaType{
		Name:         "SalesOrderItem",
		IsChildTable: true,
		Permissions: []PermRule{
			{Role: "Sales User", MatchField: "company", MatchValue: "company"},
		},
	}

	if stmts := GenerateRLSPolicies(mt); stmts != nil {
		t.Errorf("expected nil for child MetaType, got %d statements", len(stmts))
	}
}

func TestGenerateRLSPolicies_NoMatchRules(t *testing.T) {
	mt := &MetaType{
		Name: "SimpleType",
		Permissions: []PermRule{
			{Role: "Sales User", DocTypePerm: 1},
		},
	}

	stmts := GenerateRLSPolicies(mt)
	// ENABLE + FORCE + admin(drop+create) = 4 (no match policies)
	if len(stmts) != 4 {
		t.Fatalf("expected 4 statements (enable+force+admin), got %d", len(stmts))
	}
}

func TestGenerateRLSPolicies_DeterministicNaming(t *testing.T) {
	mt := &MetaType{
		Name: "SalesOrder",
		Permissions: []PermRule{
			{Role: "Sales User", MatchField: "company", MatchValue: "company"},
		},
	}

	stmts1 := GenerateRLSPolicies(mt)
	stmts2 := GenerateRLSPolicies(mt)

	if len(stmts1) != len(stmts2) {
		t.Fatal("non-deterministic statement count")
	}
	for i := range stmts1 {
		if stmts1[i].SQL != stmts2[i].SQL {
			t.Errorf("stmt[%d] not deterministic:\n  run1: %s\n  run2: %s", i, stmts1[i].SQL, stmts2[i].SQL)
		}
	}
}

func TestGenerateDropRLSPolicies(t *testing.T) {
	mt := &MetaType{
		Name: "SalesOrder",
		Permissions: []PermRule{
			{Role: "Sales User", MatchField: "company", MatchValue: "company"},
			{Role: "Territory Manager", MatchField: "territory", MatchValue: "territory"},
		},
	}

	stmts := GenerateDropRLSPolicies(mt)
	// admin drop + match0 drop + match1 drop + DISABLE = 4
	if len(stmts) != 4 {
		t.Fatalf("expected 4 drop statements, got %d", len(stmts))
	}

	// All should be DROP POLICY or DISABLE.
	for _, s := range stmts[:3] {
		if !strings.Contains(s.SQL, "DROP POLICY IF EXISTS") {
			t.Errorf("expected DROP POLICY: %s", s.SQL)
		}
	}
	if !strings.Contains(stmts[3].SQL, "DISABLE ROW LEVEL SECURITY") {
		t.Errorf("last stmt should DISABLE RLS: %s", stmts[3].SQL)
	}
}

func TestGenerateDropRLSPolicies_VirtualReturnsNil(t *testing.T) {
	mt := &MetaType{Name: "V", IsVirtual: true}
	if stmts := GenerateDropRLSPolicies(mt); stmts != nil {
		t.Errorf("expected nil, got %d", len(stmts))
	}
}

func TestRoleToSnake(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Sales User", "sales_user"},
		{"Administrator", "administrator"},
		{"Territory Manager", "territory_manager"},
		{"HR-Admin", "hr_admin"},
		{"role_with_underscore", "role_with_underscore"},
		{"CamelCase", "camelcase"},
		{"", ""},
	}
	for _, tt := range tests {
		got := roleToSnake(tt.input)
		if got != tt.want {
			t.Errorf("roleToSnake(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSanitizeGUCKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"company", "company"},
		{"user_company", "user_company"},
		{"Company", "company"},
		{"my-key", "mykey"},
		{"key with spaces", "keywithspaces"},
		{"key!@#$%", "key"},
		{"", ""},
	}
	for _, tt := range tests {
		got := sanitizeGUCKey(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeGUCKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

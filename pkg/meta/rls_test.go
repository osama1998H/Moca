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
		// Edge cases: inputs that become empty after sanitization.
		{"!@#$%^&*()", ""},
		{"---", ""},
		{"   ", ""},
		// Unicode characters (non-ASCII letters are preserved by unicode.IsLetter).
		{"café", "café"},
		{"über", "über"},
		{"日本語", "日本語"},
		// Mixed special + valid.
		{"a!b@c#d", "abcd"},
		// Underscores preserved.
		{"__double__", "__double__"},
		// Numbers.
		{"field123", "field123"},
		{"123start", "123start"},
	}
	for _, tt := range tests {
		got := sanitizeGUCKey(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeGUCKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRoleToSnake_EdgeCases(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Unicode letters are lowercased.
		{"Ünter Manager", "ünter_manager"},
		// Multiple consecutive spaces become multiple underscores.
		{"Sales  User", "sales__user"},
		// Only special chars → empty.
		{"!@#$%", ""},
		// Digits preserved.
		{"Role 123", "role_123"},
		// Mix of spaces, hyphens, underscores.
		{"Sales-User_Admin Team", "sales_user_admin_team"},
	}
	for _, tt := range tests {
		got := roleToSnake(tt.input)
		if got != tt.want {
			t.Errorf("roleToSnake(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGenerateRLSPolicies_EmptyPermissions(t *testing.T) {
	mt := &MetaType{
		Name:        "EmptyPerms",
		Permissions: nil,
	}
	stmts := GenerateRLSPolicies(mt)
	// ENABLE + FORCE + admin(drop+create) = 4 (no match policies)
	if len(stmts) != 4 {
		t.Fatalf("expected 4 statements for nil permissions, got %d", len(stmts))
	}
}

func TestGenerateRLSPolicies_EmptyMatchFieldOrValue(t *testing.T) {
	mt := &MetaType{
		Name: "PartialMatch",
		Permissions: []PermRule{
			{Role: "User", MatchField: "company", MatchValue: ""},    // empty value
			{Role: "User", MatchField: "", MatchValue: "company"},    // empty field
			{Role: "User", MatchField: "", MatchValue: ""},           // both empty
			{Role: "User", MatchField: "company", MatchValue: "company"}, // valid
		},
	}
	stmts := GenerateRLSPolicies(mt)
	// Only 1 valid match rule → 6 statements.
	if len(stmts) != 6 {
		t.Fatalf("expected 6 statements (only 1 valid match), got %d", len(stmts))
	}
}

func TestGenerateRLSPolicies_SpecialCharsInMatchValue(t *testing.T) {
	mt := &MetaType{
		Name: "SpecialChars",
		Permissions: []PermRule{
			{Role: "User", MatchField: "company", MatchValue: "my-company!@#"},
		},
	}
	stmts := GenerateRLSPolicies(mt)
	// Should sanitize the GUC key.
	var foundGUC bool
	for _, s := range stmts {
		if strings.Contains(s.SQL, "moca.current_user_mycompany") {
			foundGUC = true
		}
	}
	if !foundGUC {
		t.Error("expected sanitized GUC key 'moca.current_user_mycompany'")
		for _, s := range stmts {
			t.Logf("  SQL: %s", s.SQL)
		}
	}
}

func TestGenerateDropRLSPolicies_EmptyPermissions(t *testing.T) {
	mt := &MetaType{
		Name:        "EmptyPerms",
		Permissions: nil,
	}
	stmts := GenerateDropRLSPolicies(mt)
	// admin drop + DISABLE = 2 (no match policies to drop)
	if len(stmts) != 2 {
		t.Fatalf("expected 2 statements for nil permissions, got %d", len(stmts))
	}
	if !strings.Contains(stmts[0].SQL, "DROP POLICY IF EXISTS") {
		t.Errorf("expected DROP POLICY for admin, got: %s", stmts[0].SQL)
	}
	if !strings.Contains(stmts[1].SQL, "DISABLE ROW LEVEL SECURITY") {
		t.Errorf("expected DISABLE RLS, got: %s", stmts[1].SQL)
	}
}

func TestGenerateDropRLSPolicies_SingleSkipped(t *testing.T) {
	mt := &MetaType{Name: "S", IsSingle: true}
	if stmts := GenerateDropRLSPolicies(mt); stmts != nil {
		t.Errorf("expected nil for single, got %d", len(stmts))
	}
}

func TestGenerateDropRLSPolicies_ChildTableSkipped(t *testing.T) {
	mt := &MetaType{Name: "C", IsChildTable: true}
	if stmts := GenerateDropRLSPolicies(mt); stmts != nil {
		t.Errorf("expected nil for child table, got %d", len(stmts))
	}
}

func TestGenerateDropRLSPolicies_Deterministic(t *testing.T) {
	mt := &MetaType{
		Name: "SalesOrder",
		Permissions: []PermRule{
			{Role: "Sales User", MatchField: "company", MatchValue: "company"},
			{Role: "Territory Manager", MatchField: "territory", MatchValue: "territory"},
		},
	}
	stmts1 := GenerateDropRLSPolicies(mt)
	stmts2 := GenerateDropRLSPolicies(mt)
	if len(stmts1) != len(stmts2) {
		t.Fatal("non-deterministic statement count")
	}
	for i := range stmts1 {
		if stmts1[i].SQL != stmts2[i].SQL {
			t.Errorf("stmt[%d] not deterministic:\n  run1: %s\n  run2: %s", i, stmts1[i].SQL, stmts2[i].SQL)
		}
	}
}

func TestRlsPolicyName(t *testing.T) {
	got := rlsPolicyName("tab_sales_order", "admin")
	if got != "moca_tab_sales_order_admin" {
		t.Errorf("rlsPolicyName = %q, want %q", got, "moca_tab_sales_order_admin")
	}
	got = rlsPolicyName("tab_x", "match_0")
	if got != "moca_tab_x_match_0" {
		t.Errorf("rlsPolicyName = %q, want %q", got, "moca_tab_x_match_0")
	}
}

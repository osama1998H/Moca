package auth_test

import (
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/orm"
)

func TestRowLevelFilters_NoMatchConditions(t *testing.T) {
	ep := &auth.EffectivePerms{
		DocTypePerm: auth.PermRead,
	}
	user := &auth.User{Email: "user@test.com", Roles: []string{"Sales User"}}
	filters := auth.RowLevelFilters(ep, user)
	if filters != nil {
		t.Fatalf("expected nil filters, got %v", filters)
	}
}

func TestRowLevelFilters_SingleMatchCondition(t *testing.T) {
	ep := &auth.EffectivePerms{
		DocTypePerm: auth.PermRead,
		MatchConditions: []auth.MatchCondition{
			{Field: "company", Value: "company"},
		},
	}
	user := &auth.User{
		Email:        "user@test.com",
		Roles:        []string{"Sales User"},
		UserDefaults: map[string]string{"company": "Acme Corp"},
	}

	filters := auth.RowLevelFilters(ep, user)
	if len(filters) != 1 {
		t.Fatalf("expected 1 filter, got %d", len(filters))
	}
	if filters[0].Field != "company" {
		t.Errorf("expected field 'company', got %q", filters[0].Field)
	}
	if filters[0].Operator != orm.OpEqual {
		t.Errorf("expected operator '=', got %q", filters[0].Operator)
	}
	if filters[0].Value != "Acme Corp" {
		t.Errorf("expected value 'Acme Corp', got %v", filters[0].Value)
	}
}

func TestRowLevelFilters_MultipleMatchConditions_DifferentRoles(t *testing.T) {
	ep := &auth.EffectivePerms{
		DocTypePerm: auth.PermRead,
		MatchConditions: []auth.MatchCondition{
			{Field: "company", Value: "company"},
			{Field: "territory", Value: "territory"},
		},
	}
	user := &auth.User{
		Email: "user@test.com",
		Roles: []string{"Sales User", "Territory Manager"},
		UserDefaults: map[string]string{
			"company":   "Acme Corp",
			"territory": "West",
		},
	}

	filters := auth.RowLevelFilters(ep, user)
	if len(filters) != 2 {
		t.Fatalf("expected 2 filters, got %d", len(filters))
	}
	// First filter: company = "Acme Corp"
	if filters[0].Field != "company" || filters[0].Value != "Acme Corp" {
		t.Errorf("filter[0]: expected company=Acme Corp, got %s=%v", filters[0].Field, filters[0].Value)
	}
	// Second filter: territory = "West"
	if filters[1].Field != "territory" || filters[1].Value != "West" {
		t.Errorf("filter[1]: expected territory=West, got %s=%v", filters[1].Field, filters[1].Value)
	}
}

func TestRowLevelFilters_MissingUserDefault(t *testing.T) {
	ep := &auth.EffectivePerms{
		DocTypePerm: auth.PermRead,
		MatchConditions: []auth.MatchCondition{
			{Field: "company", Value: "company"},
		},
	}
	user := &auth.User{
		Email:        "user@test.com",
		Roles:        []string{"Sales User"},
		UserDefaults: map[string]string{}, // no "company" default
	}

	filters := auth.RowLevelFilters(ep, user)
	if len(filters) != 1 {
		t.Fatalf("expected 1 filter, got %d", len(filters))
	}
	// Missing default → empty string match (impossible condition)
	if filters[0].Value != "" {
		t.Errorf("expected empty value for missing default, got %v", filters[0].Value)
	}
}

func TestRowLevelFilters_NilUserDefaults(t *testing.T) {
	ep := &auth.EffectivePerms{
		DocTypePerm: auth.PermRead,
		MatchConditions: []auth.MatchCondition{
			{Field: "company", Value: "company"},
		},
	}
	user := &auth.User{
		Email: "user@test.com",
		Roles: []string{"Sales User"},
		// UserDefaults is nil
	}

	filters := auth.RowLevelFilters(ep, user)
	if len(filters) != 1 {
		t.Fatalf("expected 1 filter, got %d", len(filters))
	}
	if filters[0].Value != "" {
		t.Errorf("expected empty value for nil defaults, got %v", filters[0].Value)
	}
}

func TestCheckRowLevelAccess_NoConditions(t *testing.T) {
	ep := &auth.EffectivePerms{DocTypePerm: auth.PermRead}
	user := &auth.User{Email: "user@test.com"}
	doc := map[string]any{"company": "Acme Corp"}

	if !auth.CheckRowLevelAccess(ep, user, doc) {
		t.Error("expected access allowed when no match conditions")
	}
}

func TestCheckRowLevelAccess_MatchingCondition(t *testing.T) {
	ep := &auth.EffectivePerms{
		DocTypePerm: auth.PermRead,
		MatchConditions: []auth.MatchCondition{
			{Field: "company", Value: "company"},
		},
	}
	user := &auth.User{
		Email:        "user@test.com",
		UserDefaults: map[string]string{"company": "Acme Corp"},
	}
	doc := map[string]any{"company": "Acme Corp", "name": "SO-001"}

	if !auth.CheckRowLevelAccess(ep, user, doc) {
		t.Error("expected access allowed when company matches")
	}
}

func TestCheckRowLevelAccess_NonMatchingCondition(t *testing.T) {
	ep := &auth.EffectivePerms{
		DocTypePerm: auth.PermRead,
		MatchConditions: []auth.MatchCondition{
			{Field: "company", Value: "company"},
		},
	}
	user := &auth.User{
		Email:        "user@test.com",
		UserDefaults: map[string]string{"company": "Acme Corp"},
	}
	doc := map[string]any{"company": "Other Corp", "name": "SO-002"}

	if auth.CheckRowLevelAccess(ep, user, doc) {
		t.Error("expected access denied when company doesn't match")
	}
}

func TestCheckRowLevelAccess_MultipleConditions_ORSemantics(t *testing.T) {
	ep := &auth.EffectivePerms{
		DocTypePerm: auth.PermRead,
		MatchConditions: []auth.MatchCondition{
			{Field: "company", Value: "company"},
			{Field: "territory", Value: "territory"},
		},
	}
	user := &auth.User{
		Email: "user@test.com",
		UserDefaults: map[string]string{
			"company":   "Acme Corp",
			"territory": "West",
		},
	}

	// Document matches territory but not company → allowed (OR semantics)
	doc := map[string]any{"company": "Other Corp", "territory": "West"}
	if !auth.CheckRowLevelAccess(ep, user, doc) {
		t.Error("expected access allowed when at least one condition matches (OR)")
	}

	// Document matches neither → denied
	doc2 := map[string]any{"company": "Other Corp", "territory": "East"}
	if auth.CheckRowLevelAccess(ep, user, doc2) {
		t.Error("expected access denied when no conditions match")
	}
}

func TestCheckRowLevelAccess_NilUserDefaults(t *testing.T) {
	ep := &auth.EffectivePerms{
		DocTypePerm: auth.PermRead,
		MatchConditions: []auth.MatchCondition{
			{Field: "company", Value: "company"},
		},
	}
	user := &auth.User{Email: "user@test.com"} // nil UserDefaults
	doc := map[string]any{"company": "Acme Corp"}

	if auth.CheckRowLevelAccess(ep, user, doc) {
		t.Error("expected access denied when user has nil defaults but conditions exist")
	}
}

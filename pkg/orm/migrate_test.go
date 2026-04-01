package orm

import (
	"testing"
)

// ── Topological Sort Tests ──────────────────────────────────────────────────────

func TestTopoSortMigrations_LinearChain(t *testing.T) {
	// A → B → C (B depends on A, C depends on B)
	migrations := []AppMigration{
		{AppName: "app", Version: "003", DependsOn: []string{"app:002"}},
		{AppName: "app", Version: "001"},
		{AppName: "app", Version: "002", DependsOn: []string{"app:001"}},
	}

	sorted, err := topoSortMigrations(migrations, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sorted) != 3 {
		t.Fatalf("expected 3 migrations, got %d", len(sorted))
	}

	// 001 must come before 002, 002 must come before 003.
	idx := make(map[string]int)
	for i, m := range sorted {
		idx[m.migrationKey()] = i
	}
	if idx["app:001"] >= idx["app:002"] {
		t.Errorf("app:001 (pos %d) should come before app:002 (pos %d)", idx["app:001"], idx["app:002"])
	}
	if idx["app:002"] >= idx["app:003"] {
		t.Errorf("app:002 (pos %d) should come before app:003 (pos %d)", idx["app:002"], idx["app:003"])
	}
}

func TestTopoSortMigrations_Diamond(t *testing.T) {
	// D depends on B and C; B and C depend on A.
	migrations := []AppMigration{
		{AppName: "app", Version: "D", DependsOn: []string{"app:B", "app:C"}},
		{AppName: "app", Version: "B", DependsOn: []string{"app:A"}},
		{AppName: "app", Version: "C", DependsOn: []string{"app:A"}},
		{AppName: "app", Version: "A"},
	}

	sorted, err := topoSortMigrations(migrations, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sorted) != 4 {
		t.Fatalf("expected 4 migrations, got %d", len(sorted))
	}

	idx := make(map[string]int)
	for i, m := range sorted {
		idx[m.migrationKey()] = i
	}
	if idx["app:A"] >= idx["app:B"] {
		t.Errorf("A should come before B")
	}
	if idx["app:A"] >= idx["app:C"] {
		t.Errorf("A should come before C")
	}
	if idx["app:B"] >= idx["app:D"] {
		t.Errorf("B should come before D")
	}
	if idx["app:C"] >= idx["app:D"] {
		t.Errorf("C should come before D")
	}
}

func TestTopoSortMigrations_CycleError(t *testing.T) {
	migrations := []AppMigration{
		{AppName: "app", Version: "A", DependsOn: []string{"app:B"}},
		{AppName: "app", Version: "B", DependsOn: []string{"app:A"}},
	}

	_, err := topoSortMigrations(migrations, nil)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !contains(err.Error(), "circular") {
		t.Errorf("expected error to mention 'circular', got: %v", err)
	}
}

func TestTopoSortMigrations_MissingDependency(t *testing.T) {
	migrations := []AppMigration{
		{AppName: "app", Version: "A", DependsOn: []string{"other:001"}},
	}

	_, err := topoSortMigrations(migrations, nil)
	if err == nil {
		t.Fatal("expected missing dependency error, got nil")
	}
	if !contains(err.Error(), "neither pending nor applied") {
		t.Errorf("expected 'neither pending nor applied' error, got: %v", err)
	}
}

func TestTopoSortMigrations_NoDeps(t *testing.T) {
	// Independent migrations should be returned in deterministic sorted order.
	migrations := []AppMigration{
		{AppName: "crm", Version: "001"},
		{AppName: "app", Version: "002"},
		{AppName: "billing", Version: "001"},
	}

	sorted, err := topoSortMigrations(migrations, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sorted) != 3 {
		t.Fatalf("expected 3 migrations, got %d", len(sorted))
	}

	// With no deps, Kahn's seeds are sorted alphabetically by key.
	expected := []string{"app:002", "billing:001", "crm:001"}
	for i, m := range sorted {
		if m.migrationKey() != expected[i] {
			t.Errorf("position %d: expected %s, got %s", i, expected[i], m.migrationKey())
		}
	}
}

func TestTopoSortMigrations_AlreadyApplied(t *testing.T) {
	// Migration B depends on A, but A is already applied (not in pending set).
	migrations := []AppMigration{
		{AppName: "app", Version: "B", DependsOn: []string{"app:A"}},
	}
	applied := map[string]bool{"app:A": true}

	sorted, err := topoSortMigrations(migrations, applied)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sorted) != 1 {
		t.Fatalf("expected 1 migration, got %d", len(sorted))
	}
	if sorted[0].migrationKey() != "app:B" {
		t.Errorf("expected app:B, got %s", sorted[0].migrationKey())
	}
}

func TestTopoSortMigrations_CrossAppDeps(t *testing.T) {
	// CRM migration depends on core migration.
	migrations := []AppMigration{
		{AppName: "crm", Version: "001", DependsOn: []string{"core:001"}},
		{AppName: "core", Version: "001"},
	}

	sorted, err := topoSortMigrations(migrations, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sorted) != 2 {
		t.Fatalf("expected 2 migrations, got %d", len(sorted))
	}
	if sorted[0].migrationKey() != "core:001" {
		t.Errorf("expected core:001 first, got %s", sorted[0].migrationKey())
	}
	if sorted[1].migrationKey() != "crm:001" {
		t.Errorf("expected crm:001 second, got %s", sorted[1].migrationKey())
	}
}

func TestTopoSortMigrations_Empty(t *testing.T) {
	sorted, err := topoSortMigrations(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sorted != nil {
		t.Errorf("expected nil, got %v", sorted)
	}
}

// ── DependsOn Validation Tests ──────────────────────────────────────────────────

func TestValidateDependsOnKey_Valid(t *testing.T) {
	valid := []string{"app:001", "core:v1.0.0", "my_app:migrate-2"}
	for _, key := range valid {
		if err := validateDependsOnKey(key); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", key, err)
		}
	}
}

func TestValidateDependsOnKey_Invalid(t *testing.T) {
	invalid := []string{"", "noversion", ":001", "app:", "::"}
	for _, key := range invalid {
		if err := validateDependsOnKey(key); err == nil {
			t.Errorf("expected %q to be invalid, got nil", key)
		}
	}
}

// ── applySkip Tests ─────────────────────────────────────────────────────────────

func TestApplySkip(t *testing.T) {
	migrations := []AppMigration{
		{AppName: "app", Version: "001"},
		{AppName: "app", Version: "002"},
		{AppName: "app", Version: "003"},
	}

	remaining, skipped := applySkip(migrations, "app:002")
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining, got %d", len(remaining))
	}
	if len(skipped) != 1 {
		t.Fatalf("expected 1 skipped, got %d", len(skipped))
	}
	if skipped[0].migrationKey() != "app:002" {
		t.Errorf("expected skipped app:002, got %s", skipped[0].migrationKey())
	}
}

func TestApplySkip_NoMatch(t *testing.T) {
	migrations := []AppMigration{
		{AppName: "app", Version: "001"},
	}

	remaining, skipped := applySkip(migrations, "app:999")
	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining, got %d", len(remaining))
	}
	if len(skipped) != 0 {
		t.Fatalf("expected 0 skipped, got %d", len(skipped))
	}
}

// ── MigrationKey Tests ──────────────────────────────────────────────────────────

func TestMigrationKey(t *testing.T) {
	m := AppMigration{AppName: "core", Version: "001"}
	if got := m.migrationKey(); got != "core:001" {
		t.Errorf("expected core:001, got %s", got)
	}
}

// ── Helper ──────────────────────────────────────────────────────────────────────

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

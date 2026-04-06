package document

import (
	"encoding/json"
	"regexp"
	"testing"
)

func TestGenerateVersionUUID_Format(t *testing.T) {
	uuid, err := generateVersionUUID()
	if err != nil {
		t.Fatalf("generateVersionUUID() error: %v", err)
	}

	// RFC 4122 v4: 8-4-4-4-12 hex with version=4 and variant=10.
	pattern := `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`
	if !regexp.MustCompile(pattern).MatchString(uuid) {
		t.Fatalf("UUID %q does not match RFC 4122 v4 pattern", uuid)
	}
}

func TestGenerateVersionUUID_Uniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		uuid, err := generateVersionUUID()
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if _, ok := seen[uuid]; ok {
			t.Fatalf("duplicate UUID at iteration %d: %s", i, uuid)
		}
		seen[uuid] = struct{}{}
	}
}

func TestBuildVersionDiff_BasicFields(t *testing.T) {
	doc := &DynamicDoc{
		original: map[string]any{"customer": "Alice", "amount": 100.0},
		values:   map[string]any{"customer": "Bob", "amount": 200.0},
	}
	diff := buildVersionDiff(doc, []string{"customer", "amount"})
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if len(diff) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(diff))
	}

	customerDiff, ok := diff["customer"].(map[string]any)
	if !ok {
		t.Fatal("expected customer diff to be a map")
	}
	if customerDiff["old"] != "Alice" || customerDiff["new"] != "Bob" {
		t.Fatalf("customer diff = %v, want old=Alice new=Bob", customerDiff)
	}
	amountDiff, ok := diff["amount"].(map[string]any)
	if !ok {
		t.Fatal("expected amount diff to be a map")
	}
	if amountDiff["old"] != 100.0 || amountDiff["new"] != 200.0 {
		t.Fatalf("amount diff = %v, want old=100 new=200", amountDiff)
	}
}

func TestBuildVersionDiff_ExcludesSystemFields(t *testing.T) {
	doc := &DynamicDoc{
		original: map[string]any{"customer": "Alice", "modified": "old", "modified_by": "old"},
		values:   map[string]any{"customer": "Bob", "modified": "new", "modified_by": "new"},
	}
	diff := buildVersionDiff(doc, []string{"customer", "modified", "modified_by"})
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if len(diff) != 1 {
		t.Fatalf("expected 1 field (system fields excluded), got %d", len(diff))
	}
	if _, ok := diff["modified"]; ok {
		t.Fatal("modified should be excluded")
	}
	if _, ok := diff["modified_by"]; ok {
		t.Fatal("modified_by should be excluded")
	}
}

func TestBuildVersionDiff_Empty(t *testing.T) {
	doc := &DynamicDoc{
		original: map[string]any{},
		values:   map[string]any{},
	}
	if diff := buildVersionDiff(doc, nil); diff != nil {
		t.Fatalf("expected nil diff for empty modifiedFields, got %v", diff)
	}
	if diff := buildVersionDiff(doc, []string{}); diff != nil {
		t.Fatalf("expected nil diff for empty slice, got %v", diff)
	}
}

func TestBuildVersionDiff_OnlySystemFields(t *testing.T) {
	doc := &DynamicDoc{
		original: map[string]any{"modified": "old", "modified_by": "old"},
		values:   map[string]any{"modified": "new", "modified_by": "new"},
	}
	diff := buildVersionDiff(doc, []string{"modified", "modified_by"})
	if diff != nil {
		t.Fatalf("expected nil diff when only system fields modified, got %v", diff)
	}
}

func TestBuildVersionData_InsertCase(t *testing.T) {
	snapshot := map[string]any{"customer": "Alice", "amount": 100}
	data, err := buildVersionData(nil, snapshot)
	if err != nil {
		t.Fatalf("buildVersionData error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if parsed["changed"] != nil {
		t.Fatalf("expected changed=nil for insert, got %v", parsed["changed"])
	}
	snap, ok := parsed["snapshot"].(map[string]any)
	if !ok {
		t.Fatal("expected snapshot to be a map")
	}
	if snap["customer"] != "Alice" {
		t.Fatalf("snapshot.customer = %v, want Alice", snap["customer"])
	}
}

func TestBuildVersionData_UpdateCase(t *testing.T) {
	changed := map[string]any{
		"customer": map[string]any{"old": "Alice", "new": "Bob"},
	}
	snapshot := map[string]any{"customer": "Bob", "amount": 100}
	data, err := buildVersionData(changed, snapshot)
	if err != nil {
		t.Fatalf("buildVersionData error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	ch, ok := parsed["changed"].(map[string]any)
	if !ok {
		t.Fatal("expected changed to be a map")
	}
	custChange, ok := ch["customer"].(map[string]any)
	if !ok {
		t.Fatal("expected changed.customer to be a map")
	}
	if custChange["old"] != "Alice" || custChange["new"] != "Bob" {
		t.Fatalf("changed.customer = %v, want old=Alice new=Bob", custChange)
	}
	snap, ok := parsed["snapshot"].(map[string]any)
	if !ok {
		t.Fatal("expected snapshot to be a map")
	}
	if snap["customer"] != "Bob" {
		t.Fatalf("snapshot.customer = %v, want Bob", snap["customer"])
	}
}

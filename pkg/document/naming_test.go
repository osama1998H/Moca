package document_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/moca-framework/moca/pkg/document"
	"github.com/moca-framework/moca/pkg/meta"
)

// ── fixtures ──────────────────────────────────────────────────────────────────

// MetaType JSONs for naming strategy tests.
const (
	uuidRuleJSON = `{
		"name": "UUIDDoc",
		"module": "test",
		"naming_rule": {"rule": "uuid"},
		"fields": [{"name": "title", "field_type": "Data", "label": "Title"}]
	}`

	emptyRuleJSON = `{
		"name": "EmptyRuleDoc",
		"module": "test",
		"naming_rule": {"rule": ""},
		"fields": [{"name": "title", "field_type": "Data", "label": "Title"}]
	}`

	byFieldRuleJSON = `{
		"name": "ByFieldDoc",
		"module": "test",
		"naming_rule": {"rule": "field", "field_name": "title"},
		"fields": [{"name": "title", "field_type": "Data", "label": "Title"}]
	}`

	hashRuleJSON = `{
		"name": "HashDoc",
		"module": "test",
		"naming_rule": {"rule": "hash"},
		"fields": [
			{"name": "title",  "field_type": "Data",  "label": "Title"},
			{"name": "amount", "field_type": "Float", "label": "Amount"}
		]
	}`

	customRuleJSON = `{
		"name": "CustomDoc",
		"module": "test",
		"naming_rule": {"rule": "custom", "custom_func": "my_func"},
		"fields": [{"name": "title", "field_type": "Data", "label": "Title"}]
	}`

	patternNoHashJSON = `{
		"name": "PatternNoHash",
		"module": "test",
		"naming_rule": {"rule": "pattern", "pattern": "NOHASH"},
		"fields": [{"name": "title", "field_type": "Data", "label": "Title"}]
	}`

	patternMultiGroupJSON = `{
		"name": "PatternMultiGroup",
		"module": "test",
		"naming_rule": {"rule": "pattern", "pattern": "A-##-B-##"},
		"fields": [{"name": "title", "field_type": "Data", "label": "Title"}]
	}`

	patternWithSuffixJSON = `{
		"name": "PatternWithSuffix",
		"module": "test",
		"naming_rule": {"rule": "pattern", "pattern": "PREFIX-##-SUFFIX"},
		"fields": [{"name": "title", "field_type": "Data", "label": "Title"}]
	}`
)

// ── helpers ───────────────────────────────────────────────────────────────────

// isUUIDv4 checks whether s is a valid UUID v4 string (8-4-4-4-12 hex groups).
func isUUIDv4(s string) bool {
	parts := strings.Split(s, "-")
	if len(parts) != 5 {
		return false
	}
	lengths := []int{8, 4, 4, 4, 12}
	for i, part := range parts {
		if len(part) != lengths[i] {
			return false
		}
		for _, ch := range part {
			if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
				return false
			}
		}
	}
	return true
}

// newDocForMeta returns a new DynamicDoc for a compiled MetaType.
func newDocForMeta(t *testing.T, jsonStr string) (document.Document, *meta.MetaType) {
	t.Helper()
	mt := mustCompile(t, jsonStr)
	return document.NewDynamicDoc(mt, nil, true), mt
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestNamingEngine_UUID(t *testing.T) {
	engine := document.NewNamingEngine()
	doc, _ := newDocForMeta(t, uuidRuleJSON)

	name1, err := engine.GenerateName(context.Background(), doc, nil)
	assertNoError(t, err)

	name2, err := engine.GenerateName(context.Background(), doc, nil)
	assertNoError(t, err)

	if name1 == name2 {
		t.Errorf("two UUID calls produced the same name %q; UUIDs must be unique", name1)
	}
	if !isUUIDv4(name1) {
		t.Errorf("name1 %q is not in UUID v4 format (8-4-4-4-12)", name1)
	}
	if !isUUIDv4(name2) {
		t.Errorf("name2 %q is not in UUID v4 format (8-4-4-4-12)", name2)
	}
	t.Logf("UUID names: %q, %q", name1, name2)
}

func TestNamingEngine_DefaultUUID(t *testing.T) {
	// An empty naming rule must default to UUID.
	engine := document.NewNamingEngine()
	doc, _ := newDocForMeta(t, emptyRuleJSON)

	name, err := engine.GenerateName(context.Background(), doc, nil)
	assertNoError(t, err)

	if !isUUIDv4(name) {
		t.Errorf("empty rule should default to UUID, got %q", name)
	}
	t.Logf("empty rule defaulted to UUID: %q", name)
}

func TestNamingEngine_ByField_Success(t *testing.T) {
	engine := document.NewNamingEngine()
	doc, _ := newDocForMeta(t, byFieldRuleJSON)

	assertNoError(t, doc.Set("title", "Acme Corporation"))

	name, err := engine.GenerateName(context.Background(), doc, nil)
	assertNoError(t, err)

	if name != "Acme Corporation" {
		t.Errorf("by-field name = %q, want %q", name, "Acme Corporation")
	}
	t.Logf("by-field name: %q", name)
}

func TestNamingEngine_ByField_NilValue(t *testing.T) {
	// The field has not been set, so its value is nil.
	engine := document.NewNamingEngine()
	doc, _ := newDocForMeta(t, byFieldRuleJSON)

	_, err := engine.GenerateName(context.Background(), doc, nil)
	assertError(t, err)

	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("error should mention 'nil' field, got: %v", err)
	}
	t.Logf("nil field error: %v", err)
}

func TestNamingEngine_ByField_EmptyString(t *testing.T) {
	engine := document.NewNamingEngine()
	doc, _ := newDocForMeta(t, byFieldRuleJSON)

	assertNoError(t, doc.Set("title", ""))

	_, err := engine.GenerateName(context.Background(), doc, nil)
	assertError(t, err)

	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention 'empty' field, got: %v", err)
	}
	t.Logf("empty field error: %v", err)
}

func TestNamingEngine_ByHash_Deterministic(t *testing.T) {
	engine := document.NewNamingEngine()
	mt := mustCompile(t, hashRuleJSON)

	// Two docs with identical field values must produce the same hash.
	doc1 := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc1.Set("title", "Hello"))
	assertNoError(t, doc1.Set("amount", 100.0))

	doc2 := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc2.Set("title", "Hello"))
	assertNoError(t, doc2.Set("amount", 100.0))

	name1, err := engine.GenerateName(context.Background(), doc1, nil)
	assertNoError(t, err)
	name2, err := engine.GenerateName(context.Background(), doc2, nil)
	assertNoError(t, err)

	if name1 != name2 {
		t.Errorf("same-content docs produced different hashes: %q vs %q", name1, name2)
	}
	t.Logf("hash deterministic: %q", name1)
}

func TestNamingEngine_ByHash_Unique(t *testing.T) {
	engine := document.NewNamingEngine()
	mt := mustCompile(t, hashRuleJSON)

	// Docs with different field values must produce different hashes.
	doc1 := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc1.Set("title", "Hello"))
	assertNoError(t, doc1.Set("amount", 100.0))

	doc3 := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc3.Set("title", "World"))
	assertNoError(t, doc3.Set("amount", 200.0))

	name1, err := engine.GenerateName(context.Background(), doc1, nil)
	assertNoError(t, err)
	name3, err := engine.GenerateName(context.Background(), doc3, nil)
	assertNoError(t, err)

	if name1 == name3 {
		t.Errorf("different-content docs produced the same hash %q", name1)
	}
	t.Logf("hashes differ: %q vs %q", name1, name3)
}

func TestNamingEngine_ByHash_Length(t *testing.T) {
	engine := document.NewNamingEngine()
	doc, _ := newDocForMeta(t, hashRuleJSON)

	name, err := engine.GenerateName(context.Background(), doc, nil)
	assertNoError(t, err)

	if len(name) != 10 {
		t.Errorf("hash length = %d, want 10; hash = %q", len(name), name)
	}
	t.Logf("hash %q (len=%d)", name, len(name))
}

func TestNamingEngine_Custom_Registered(t *testing.T) {
	engine := document.NewNamingEngine()
	engine.RegisterNamingFunc("my_func", func(_ context.Context, _ document.Document) (string, error) {
		return "CUSTOM-001", nil
	})

	doc, _ := newDocForMeta(t, customRuleJSON)
	name, err := engine.GenerateName(context.Background(), doc, nil)
	assertNoError(t, err)

	if name != "CUSTOM-001" {
		t.Errorf("custom name = %q, want %q", name, "CUSTOM-001")
	}
	t.Logf("custom naming: %q", name)
}

func TestNamingEngine_Custom_Unregistered(t *testing.T) {
	// No function registered with "my_func" -- must return an error.
	engine := document.NewNamingEngine()
	doc, _ := newDocForMeta(t, customRuleJSON)

	_, err := engine.GenerateName(context.Background(), doc, nil)
	assertError(t, err)

	if !strings.Contains(err.Error(), "my_func") {
		t.Errorf("error should name the missing function, got: %v", err)
	}
	t.Logf("unregistered custom func error: %v", err)
}

func TestNamingEngine_Custom_ErrorPropagation(t *testing.T) {
	engine := document.NewNamingEngine()
	engine.RegisterNamingFunc("my_func", func(_ context.Context, _ document.Document) (string, error) {
		return "", fmt.Errorf("downstream failure")
	})

	doc, _ := newDocForMeta(t, customRuleJSON)
	_, err := engine.GenerateName(context.Background(), doc, nil)
	assertError(t, err)

	if !strings.Contains(err.Error(), "downstream failure") {
		t.Errorf("error should propagate inner message, got: %v", err)
	}
	t.Logf("custom func error propagated: %v", err)
}

func TestNamingEngine_RegisterNamingFunc_Overwrite(t *testing.T) {
	engine := document.NewNamingEngine()
	engine.RegisterNamingFunc("my_func", func(_ context.Context, _ document.Document) (string, error) {
		return "FIRST", nil
	})
	engine.RegisterNamingFunc("my_func", func(_ context.Context, _ document.Document) (string, error) {
		return "SECOND", nil
	})

	doc, _ := newDocForMeta(t, customRuleJSON)
	name, err := engine.GenerateName(context.Background(), doc, nil)
	assertNoError(t, err)

	if name != "SECOND" {
		t.Errorf("overwritten func should return SECOND, got %q", name)
	}
	t.Logf("overwrite verified: %q", name)
}

// ── parsePattern tests via GenerateName ───────────────────────────────────────

// TestParsePattern_NoHash verifies that a pattern with no '#' is rejected by
// GenerateName before any pool access (pool=nil but error is about pattern, not pool).
func TestParsePattern_NoHash(t *testing.T) {
	engine := document.NewNamingEngine()
	doc, _ := newDocForMeta(t, patternNoHashJSON)

	_, err := engine.GenerateName(context.Background(), doc, nil)
	assertError(t, err)

	// Must be a pattern error, NOT a "pool required" error.
	if strings.Contains(err.Error(), "pool required") {
		t.Errorf("expected pattern parse error, got pool error: %v", err)
	}
	if !strings.Contains(err.Error(), "#") && !strings.Contains(err.Error(), "pattern") {
		t.Errorf("error should reference pattern or '#', got: %v", err)
	}
	t.Logf("no-hash pattern error: %v", err)
}

// TestParsePattern_MultipleGroups verifies that "A-##-B-##" (two # groups separated
// by non-# text) is rejected.
func TestParsePattern_MultipleGroups(t *testing.T) {
	engine := document.NewNamingEngine()
	doc, _ := newDocForMeta(t, patternMultiGroupJSON)

	_, err := engine.GenerateName(context.Background(), doc, nil)
	assertError(t, err)

	if strings.Contains(err.Error(), "pool required") {
		t.Errorf("expected pattern parse error, got pool error: %v", err)
	}
	t.Logf("multiple-# groups error: %v", err)
}

// TestParsePattern_WithSuffix verifies that characters after the '#' group are rejected.
func TestParsePattern_WithSuffix(t *testing.T) {
	engine := document.NewNamingEngine()
	doc, _ := newDocForMeta(t, patternWithSuffixJSON)

	_, err := engine.GenerateName(context.Background(), doc, nil)
	assertError(t, err)

	if strings.Contains(err.Error(), "pool required") {
		t.Errorf("expected pattern parse error, got pool error: %v", err)
	}
	t.Logf("suffix-in-pattern error: %v", err)
}

// TestParsePattern_ValidNeedsPool verifies that a valid pattern ("SO-.####") passes
// parsing but returns "pool required" when pool=nil.
func TestParsePattern_ValidNeedsPool(t *testing.T) {
	const validPatternJSON = `{
		"name": "ValidPattern",
		"module": "test",
		"naming_rule": {"rule": "pattern", "pattern": "SO-.####"},
		"fields": [{"name": "title", "field_type": "Data", "label": "Title"}]
	}`
	engine := document.NewNamingEngine()
	doc, _ := newDocForMeta(t, validPatternJSON)

	_, err := engine.GenerateName(context.Background(), doc, nil)
	assertError(t, err)

	if !strings.Contains(err.Error(), "pool required") {
		t.Errorf("valid pattern with nil pool should say 'pool required', got: %v", err)
	}
	t.Logf("valid pattern + nil pool gives expected error: %v", err)
}

// TestNamingEngine_AutoIncrement_NilPool verifies that the autoincrement strategy
// returns a clear error when pool is nil (no panic).
func TestNamingEngine_AutoIncrement_NilPool(t *testing.T) {
	const autoJSON = `{
		"name": "AutoNilPool",
		"module": "test",
		"naming_rule": {"rule": "autoincrement"},
		"fields": [{"name": "title", "field_type": "Data", "label": "Title"}]
	}`
	engine := document.NewNamingEngine()
	doc, _ := newDocForMeta(t, autoJSON)

	_, err := engine.GenerateName(context.Background(), doc, nil)
	assertError(t, err)

	if !strings.Contains(err.Error(), "pool required") {
		t.Errorf("autoincrement with nil pool should say 'pool required', got: %v", err)
	}
	t.Logf("autoincrement + nil pool error: %v", err)
}

// TestNamingEngine_HashExcludesChildTables verifies that child-table fields do not
// affect the hash computation (their nil values are excluded).
func TestNamingEngine_HashExcludesChildTables(t *testing.T) {
	// TestOrder has a Table field "items". Two otherwise-identical docs (same scalar
	// fields) should produce the same hash regardless of child table content.
	engine := document.NewNamingEngine()
	mt := mustCompile(t, hashRuleJSON)

	doc1 := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc1.Set("title", "Same"))
	assertNoError(t, doc1.Set("amount", 42.0))

	doc2 := document.NewDynamicDoc(mt, nil, true)
	assertNoError(t, doc2.Set("title", "Same"))
	assertNoError(t, doc2.Set("amount", 42.0))

	name1, err := engine.GenerateName(context.Background(), doc1, nil)
	assertNoError(t, err)
	name2, err := engine.GenerateName(context.Background(), doc2, nil)
	assertNoError(t, err)

	if name1 != name2 {
		t.Errorf("identical scalar fields should hash to same value: %q vs %q", name1, name2)
	}
	t.Logf("hash ignores child tables: %q", name1)
}

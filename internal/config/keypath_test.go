package config

import (
	"testing"
)

func TestGetByPath_NestedKey(t *testing.T) {
	data := map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"c": 42,
			},
		},
	}

	val, ok := GetByPath(data, "a.b.c")
	if !ok {
		t.Fatal("expected key to exist")
	}
	if val != 42 {
		t.Fatalf("expected 42, got %v", val)
	}
}

func TestGetByPath_SingleSegment(t *testing.T) {
	data := map[string]any{"key": "value"}

	val, ok := GetByPath(data, "key")
	if !ok {
		t.Fatal("expected key to exist")
	}
	if val != "value" {
		t.Fatalf("expected 'value', got %v", val)
	}
}

func TestGetByPath_Missing(t *testing.T) {
	data := map[string]any{
		"a": map[string]any{"b": 1},
	}

	_, ok := GetByPath(data, "a.c")
	if ok {
		t.Fatal("expected key to not exist")
	}

	_, ok = GetByPath(data, "x.y.z")
	if ok {
		t.Fatal("expected key to not exist")
	}
}

func TestGetByPath_EmptyKey(t *testing.T) {
	data := map[string]any{"a": 1}
	_, ok := GetByPath(data, "")
	if ok {
		t.Fatal("expected false for empty key")
	}
}

func TestGetByPath_NilData(t *testing.T) {
	_, ok := GetByPath(nil, "a")
	if ok {
		t.Fatal("expected false for nil data")
	}
}

func TestGetByPath_IntermediateNotMap(t *testing.T) {
	data := map[string]any{
		"a": "string_value",
	}
	_, ok := GetByPath(data, "a.b")
	if ok {
		t.Fatal("expected false when intermediate is not a map")
	}
}

func TestSetByPath_CreatesIntermediates(t *testing.T) {
	data := make(map[string]any)
	SetByPath(data, "a.b.c", "hello")

	val, ok := GetByPath(data, "a.b.c")
	if !ok {
		t.Fatal("expected key to exist after set")
	}
	if val != "hello" {
		t.Fatalf("expected 'hello', got %v", val)
	}
}

func TestSetByPath_OverwritesExisting(t *testing.T) {
	data := map[string]any{
		"a": map[string]any{"b": "old"},
	}
	SetByPath(data, "a.b", "new")

	val, _ := GetByPath(data, "a.b")
	if val != "new" {
		t.Fatalf("expected 'new', got %v", val)
	}
}

func TestSetByPath_OverwritesNonMap(t *testing.T) {
	data := map[string]any{
		"a": "not_a_map",
	}
	SetByPath(data, "a.b", 99)

	val, ok := GetByPath(data, "a.b")
	if !ok {
		t.Fatal("expected key to exist")
	}
	if val != 99 {
		t.Fatalf("expected 99, got %v", val)
	}
}

func TestSetByPath_SingleSegment(t *testing.T) {
	data := make(map[string]any)
	SetByPath(data, "key", true)

	if data["key"] != true {
		t.Fatalf("expected true, got %v", data["key"])
	}
}

func TestSetByPath_EmptyKeyNoop(t *testing.T) {
	data := make(map[string]any)
	SetByPath(data, "", "val")
	if len(data) != 0 {
		t.Fatal("expected no modification for empty key")
	}
}

func TestRemoveByPath_Existing(t *testing.T) {
	data := map[string]any{
		"a": map[string]any{
			"b": 1,
			"c": 2,
		},
	}
	ok := RemoveByPath(data, "a.b")
	if !ok {
		t.Fatal("expected true for existing key")
	}
	if _, exists := GetByPath(data, "a.b"); exists {
		t.Fatal("key should be removed")
	}
	// Sibling should remain.
	if _, exists := GetByPath(data, "a.c"); !exists {
		t.Fatal("sibling key should still exist")
	}
}

func TestRemoveByPath_Missing(t *testing.T) {
	data := map[string]any{"a": 1}
	ok := RemoveByPath(data, "b")
	if ok {
		t.Fatal("expected false for missing key")
	}
}

func TestRemoveByPath_DeepMissing(t *testing.T) {
	data := map[string]any{
		"a": map[string]any{"b": 1},
	}
	ok := RemoveByPath(data, "a.x.y")
	if ok {
		t.Fatal("expected false for missing intermediate")
	}
}

func TestFlattenMap_Nested(t *testing.T) {
	data := map[string]any{
		"a": map[string]any{
			"b": 1,
			"c": map[string]any{
				"d": "hello",
			},
		},
		"e": true,
	}
	flat := FlattenMap(data, "")

	expected := map[string]any{
		"a.b":   1,
		"a.c.d": "hello",
		"e":     true,
	}

	if len(flat) != len(expected) {
		t.Fatalf("expected %d keys, got %d", len(expected), len(flat))
	}
	for k, v := range expected {
		if flat[k] != v {
			t.Errorf("key %s: expected %v, got %v", k, v, flat[k])
		}
	}
}

func TestFlattenMap_Empty(t *testing.T) {
	flat := FlattenMap(map[string]any{}, "")
	if len(flat) != 0 {
		t.Fatalf("expected empty map, got %d keys", len(flat))
	}
}

func TestFlattenMap_WithPrefix(t *testing.T) {
	data := map[string]any{"b": 1}
	flat := FlattenMap(data, "a")
	if flat["a.b"] != 1 {
		t.Fatalf("expected a.b=1, got %v", flat["a.b"])
	}
}

func TestMergeMaps_OverlayWins(t *testing.T) {
	base := map[string]any{"a": 1, "b": 2}
	overlay := map[string]any{"b": 99, "c": 3}
	result := MergeMaps(base, overlay)

	if result["a"] != 1 {
		t.Errorf("expected a=1, got %v", result["a"])
	}
	if result["b"] != 99 {
		t.Errorf("expected b=99, got %v", result["b"])
	}
	if result["c"] != 3 {
		t.Errorf("expected c=3, got %v", result["c"])
	}
}

func TestMergeMaps_DeepMerge(t *testing.T) {
	base := map[string]any{
		"infra": map[string]any{
			"db":    map[string]any{"host": "localhost", "port": 5432},
			"redis": map[string]any{"host": "localhost"},
		},
	}
	overlay := map[string]any{
		"infra": map[string]any{
			"db": map[string]any{"port": 5433},
		},
	}
	result := MergeMaps(base, overlay)

	infra, ok := result["infra"].(map[string]any)
	if !ok {
		t.Fatal("expected infra to be a map")
	}
	db, ok := infra["db"].(map[string]any)
	if !ok {
		t.Fatal("expected db to be a map")
	}

	if db["host"] != "localhost" {
		t.Errorf("expected host=localhost, got %v", db["host"])
	}
	if db["port"] != 5433 {
		t.Errorf("expected port=5433, got %v", db["port"])
	}
	// redis should remain from base.
	redisMap, ok := infra["redis"].(map[string]any)
	if !ok {
		t.Fatal("expected redis to be a map")
	}
	if redisMap["host"] != "localhost" {
		t.Errorf("expected redis host=localhost, got %v", redisMap["host"])
	}
}

func TestMergeMaps_BaseUnmodified(t *testing.T) {
	base := map[string]any{"a": 1}
	overlay := map[string]any{"a": 2}
	_ = MergeMaps(base, overlay)

	if base["a"] != 1 {
		t.Fatal("base should not be modified")
	}
}

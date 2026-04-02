package meta_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

// nullLogger returns a no-op slog.Logger suitable for unit tests.
func nullLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nullWriter{}, nil))
}

// nullWriter discards all bytes written to it.
type nullWriter struct{}

func (nullWriter) Write(p []byte) (int, error) { return len(p), nil }

// ── Sentinel error ────────────────────────────────────────────────────────────

// TestErrMetaTypeNotFound_Sentinel verifies that ErrMetaTypeNotFound works
// correctly with errors.Is when wrapped by fmt.Errorf.
func TestErrMetaTypeNotFound_Sentinel(t *testing.T) {
	wrapped := fmt.Errorf("outer: %w", meta.ErrMetaTypeNotFound)
	if !errors.Is(wrapped, meta.ErrMetaTypeNotFound) {
		t.Error("errors.Is did not unwrap ErrMetaTypeNotFound through one level of wrapping")
	}

	doubleWrapped := fmt.Errorf("outer: %w", wrapped)
	if !errors.Is(doubleWrapped, meta.ErrMetaTypeNotFound) {
		t.Error("errors.Is did not unwrap ErrMetaTypeNotFound through two levels of wrapping")
	}
}

// ── Register with invalid input ───────────────────────────────────────────────

// TestRegister_MalformedJSON returns a parse error without touching DB or Redis.
// Registry is created with nil db and nil redis; if Register accessed either,
// it would panic on a nil pointer, failing the test immediately.
func TestRegister_MalformedJSON(t *testing.T) {
	r := meta.NewRegistry(nil, nil, nullLogger())
	_, err := r.Register(context.Background(), "test_site", []byte(`{not valid json`))
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

// TestRegister_MissingName returns a CompileErrors for a MetaType with no name.
// Again, no DB or Redis access occurs because Compile runs before any I/O.
func TestRegister_MissingName(t *testing.T) {
	r := meta.NewRegistry(nil, nil, nullLogger())
	_, err := r.Register(context.Background(), "test_site", []byte(`{"module":"core"}`))
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
	var compileErrs *meta.CompileErrors
	if !errors.As(err, &compileErrs) {
		t.Fatalf("expected *CompileErrors, got %T: %v", err, err)
	}
	if len(compileErrs.Errors) == 0 {
		t.Error("expected at least one compile error")
	}
}

// TestRegister_InvalidFieldType returns a CompileErrors listing the bad field type.
func TestRegister_InvalidFieldType(t *testing.T) {
	r := meta.NewRegistry(nil, nil, nullLogger())
	_, err := r.Register(context.Background(), "test_site", []byte(`{
		"name": "Widget",
		"module": "core",
		"fields": [{"name": "color", "field_type": "Rainbow"}]
	}`))
	if err == nil {
		t.Fatal("expected error for invalid field type, got nil")
	}
	var compileErrs *meta.CompileErrors
	if !errors.As(err, &compileErrs) {
		t.Fatalf("expected *CompileErrors, got %T: %v", err, err)
	}
}

// ── L1 cache hit ─────────────────────────────────────────────────────────────

// TestGet_L1Hit verifies that Get returns from L1 without touching Redis or DB.
// The Registry is created with nil redis and nil db; accessing either would panic.
func TestGet_L1Hit(t *testing.T) {
	r := meta.NewRegistry(nil, nil, nullLogger())
	ctx := context.Background()

	mt := &meta.MetaType{
		Name:   "Widget",
		Module: "core",
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData},
		},
	}

	r.SeedL1ForTest("site_a", "Widget", mt)

	got, err := r.Get(ctx, "site_a", "Widget")
	if err != nil {
		t.Fatalf("Get: unexpected error: %v", err)
	}
	if got.Name != "Widget" {
		t.Errorf("Get returned Name=%q; want %q", got.Name, "Widget")
	}
}

// TestGet_L1Miss_NoInfra returns ErrMetaTypeNotFound when L1 is empty and
// both L2 (Redis) and L3 (DB) are unavailable (nil).
func TestGet_L1Miss_NoInfra(t *testing.T) {
	r := meta.NewRegistry(nil, nil, nullLogger())
	_, err := r.Get(context.Background(), "site_a", "Widget")
	if err == nil {
		t.Fatal("expected ErrMetaTypeNotFound, got nil")
	}
	if !errors.Is(err, meta.ErrMetaTypeNotFound) {
		t.Errorf("expected ErrMetaTypeNotFound in error chain, got: %v", err)
	}
}

// ── Invalidate ────────────────────────────────────────────────────────────────

// TestInvalidate_ClearsL1 verifies that Invalidate evicts the L1 entry so
// subsequent Get calls can no longer find it (returns ErrMetaTypeNotFound with
// nil db and nil redis).
func TestInvalidate_ClearsL1(t *testing.T) {
	r := meta.NewRegistry(nil, nil, nullLogger())
	ctx := context.Background()

	mt := &meta.MetaType{Name: "Task", Module: "core"}
	r.SeedL1ForTest("site_b", "Task", mt)

	// Confirm L1 hit.
	if _, err := r.Get(ctx, "site_b", "Task"); err != nil {
		t.Fatalf("Get before Invalidate: %v", err)
	}

	if err := r.Invalidate(ctx, "site_b", "Task"); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	// After eviction, with no DB or Redis, expect ErrMetaTypeNotFound.
	_, err := r.Get(ctx, "site_b", "Task")
	if !errors.Is(err, meta.ErrMetaTypeNotFound) {
		t.Errorf("expected ErrMetaTypeNotFound after Invalidate, got: %v", err)
	}
}

// TestInvalidateAll_ClearsL1ForSite verifies that InvalidateAll removes all
// L1 entries belonging to the given site while leaving other sites untouched.
func TestInvalidateAll_ClearsL1ForSite(t *testing.T) {
	r := meta.NewRegistry(nil, nil, nullLogger())
	ctx := context.Background()

	r.SeedL1ForTest("site_x", "DocA", &meta.MetaType{Name: "DocA", Module: "core"})
	r.SeedL1ForTest("site_x", "DocB", &meta.MetaType{Name: "DocB", Module: "core"})
	r.SeedL1ForTest("site_y", "DocC", &meta.MetaType{Name: "DocC", Module: "core"})

	if err := r.InvalidateAll(ctx, "site_x"); err != nil {
		t.Fatalf("InvalidateAll: %v", err)
	}

	// site_x entries should be gone.
	for _, doctype := range []string{"DocA", "DocB"} {
		if _, err := r.Get(ctx, "site_x", doctype); !errors.Is(err, meta.ErrMetaTypeNotFound) {
			t.Errorf("site_x/%s: expected ErrMetaTypeNotFound after InvalidateAll, got: %v", doctype, err)
		}
	}

	// site_y entry should be untouched.
	if _, err := r.Get(ctx, "site_y", "DocC"); err != nil {
		t.Errorf("site_y/DocC: expected L1 hit after InvalidateAll for other site, got: %v", err)
	}
}

// TestSchemaVersion_NilRedis returns 0 without error when Redis is nil.
func TestSchemaVersion_NilRedis(t *testing.T) {
	r := meta.NewRegistry(nil, nil, nullLogger())
	v, err := r.SchemaVersion(context.Background(), "any_site")
	if err != nil {
		t.Fatalf("SchemaVersion with nil redis: %v", err)
	}
	if v != 0 {
		t.Errorf("expected version 0, got %d", v)
	}
}

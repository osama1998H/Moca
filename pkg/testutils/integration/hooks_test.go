//go:build integration

package integration

import (
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/testutils"
)

func TestHookRegistryAndDocManager(t *testing.T) {
	env := testutils.NewTestEnv(t)

	// Verify DocManager has a hook dispatcher set.
	dm := env.DocManager()
	if dm == nil {
		t.Fatal("DocManager should not be nil")
	}

	// Register a simple doctype and verify lifecycle works with hooks.
	mt := &meta.MetaType{
		Name:   "HookDoc",
		Module: "test",
		NamingRule: meta.NamingStrategy{
			Rule: meta.NamingUUID,
		},
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData, Label: "Title", Required: true, InAPI: true},
		},
	}
	env.RegisterMetaType(t, mt)

	// Create should succeed (hooks are dispatched even if no handlers registered).
	doc := env.NewTestDoc(t, "HookDoc", map[string]any{"title": "Hook Test"})
	if doc.Name() == "" {
		t.Fatal("doc name should not be empty")
	}

	// Update should also succeed.
	ctx := env.DocContext()
	_, err := env.DocManager().Update(ctx, "HookDoc", doc.Name(), map[string]any{
		"title": "Updated Hook Test",
	})
	if err != nil {
		t.Fatalf("update with hooks: %v", err)
	}

	// Delete should also succeed.
	err = env.DocManager().Delete(ctx, "HookDoc", doc.Name())
	if err != nil {
		t.Fatalf("delete with hooks: %v", err)
	}
}

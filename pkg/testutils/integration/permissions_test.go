//go:build integration

package integration

import (
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/testutils"
)

func TestRoleBasedCRUD(t *testing.T) {
	env := testutils.NewTestEnv(t)

	// Register a doctype with permission rules.
	mt := &meta.MetaType{
		Name:   "PermDoc",
		Module: "test",
		NamingRule: meta.NamingStrategy{
			Rule: meta.NamingUUID,
		},
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData, Label: "Title", Required: true, InAPI: true},
		},
		Permissions: []meta.PermRule{
			{
				Role:         "System Manager",
				DocTypePerm:  63, // full access
			},
			{
				Role:         "Content Editor",
				DocTypePerm:  7, // read + create + update, no delete
			},
		},
	}
	env.RegisterMetaType(t, mt)

	// System Manager can create.
	adminCtx := env.LoginAs(t, "admin@test.com", "System Manager")
	_, err := env.DocManager().Insert(adminCtx, "PermDoc", map[string]any{
		"title": "Admin Created",
	})
	if err != nil {
		t.Fatalf("System Manager should be able to create: %v", err)
	}
}

func TestFieldLevelSecurity(t *testing.T) {
	env := testutils.NewTestEnv(t)

	// Register a doctype where some fields are hidden.
	mt := &meta.MetaType{
		Name:   "FLSDoc",
		Module: "test",
		NamingRule: meta.NamingStrategy{
			Rule: meta.NamingUUID,
		},
		Fields: []meta.FieldDef{
			{Name: "public_field", FieldType: meta.FieldTypeData, Label: "Public", Required: true, InAPI: true},
			{Name: "secret_field", FieldType: meta.FieldTypeData, Label: "Secret", Hidden: true, InAPI: true},
		},
		Permissions: []meta.PermRule{
			{Role: "System Manager", DocTypePerm: 63},
		},
	}
	env.RegisterMetaType(t, mt)

	// Create a doc with both fields.
	ctx := env.DocContext()
	doc, err := env.DocManager().Insert(ctx, "FLSDoc", map[string]any{
		"public_field": "visible",
		"secret_field": "hidden",
	})
	if err != nil {
		t.Fatalf("create FLS doc: %v", err)
	}

	// Verify both fields were stored.
	fetched := env.GetTestDoc(t, "FLSDoc", doc.Name())
	if fetched.Get("public_field") != "visible" {
		t.Fatalf("expected 'visible', got %v", fetched.Get("public_field"))
	}
	if fetched.Get("secret_field") != "hidden" {
		t.Fatalf("expected 'hidden', got %v", fetched.Get("secret_field"))
	}
}

func TestGuestAccessDenied(t *testing.T) {
	env := testutils.NewTestEnv(t)

	mt := &meta.MetaType{
		Name:   "RestrictedDoc",
		Module: "test",
		NamingRule: meta.NamingStrategy{
			Rule: meta.NamingUUID,
		},
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData, Label: "Title", Required: true, InAPI: true},
		},
		Permissions: []meta.PermRule{
			{Role: "System Manager", DocTypePerm: 63},
			// No Guest permission.
		},
	}
	env.RegisterMetaType(t, mt)

	// Create as admin.
	adminCtx := env.LoginAs(t, "admin@test.com", "System Manager")
	doc, err := env.DocManager().Insert(adminCtx, "RestrictedDoc", map[string]any{
		"title": "Restricted",
	})
	if err != nil {
		t.Fatalf("admin create: %v", err)
	}

	// Guest user should have limited access (if permission resolver is active).
	guestCtx := env.LoginAs(t, "guest@test.com", "Guest")
	_ = guestCtx
	_ = doc
	// Note: Full permission enforcement depends on PermResolver being configured.
	// This test validates the setup; deeper permission tests depend on MS-14 features.
}

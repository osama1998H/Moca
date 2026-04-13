//go:build integration

package integration

import (
	"testing"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/testutils"
	"github.com/osama1998H/moca/pkg/testutils/factory"
)

func TestDocumentCRUD(t *testing.T) {
	env := testutils.NewTestEnv(t)
	mt := factory.SimpleDocType("CRUDDoc")
	env.RegisterMetaType(t, mt)

	// Create.
	doc := env.NewTestDoc(t, "CRUDDoc", map[string]any{
		"customer_name": "CRUD Test Customer",
		"status":        "Open",
		"grand_total":   500.50,
	})
	name := doc.Name()
	if name == "" {
		t.Fatal("created doc should have a name")
	}

	// Read.
	fetched := env.GetTestDoc(t, "CRUDDoc", name)
	if fetched.Get("customer_name") != "CRUD Test Customer" {
		t.Fatalf("expected 'CRUD Test Customer', got %v", fetched.Get("customer_name"))
	}

	// Update.
	ctx := env.DocContext()
	updated, err := env.DocManager().Update(ctx, "CRUDDoc", name, map[string]any{
		"status": "Closed",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Get("status") != "Closed" {
		t.Fatalf("expected status 'Closed', got %v", updated.Get("status"))
	}

	// Delete.
	env.DeleteTestDoc(t, "CRUDDoc", name)

	// Verify deleted.
	_, err = env.DocManager().Get(ctx, "CRUDDoc", name)
	if err == nil {
		t.Fatal("expected error getting deleted document")
	}
}

func TestDocumentNamingUUID(t *testing.T) {
	env := testutils.NewTestEnv(t)
	mt := &meta.MetaType{
		Name:   "UUIDNamed",
		Module: "test",
		NamingRule: meta.NamingStrategy{
			Rule: meta.NamingUUID,
		},
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData, Label: "Title", Required: true, InAPI: true},
		},
	}
	env.RegisterMetaType(t, mt)

	doc := env.NewTestDoc(t, "UUIDNamed", map[string]any{"title": "Test"})
	// UUID format: 8-4-4-4-12 hex
	if len(doc.Name()) < 32 {
		t.Fatalf("expected UUID-style name, got %q", doc.Name())
	}
}

func TestDocumentNamingByField(t *testing.T) {
	env := testutils.NewTestEnv(t)
	mt := &meta.MetaType{
		Name:   "FieldNamed",
		Module: "test",
		NamingRule: meta.NamingStrategy{
			Rule:      meta.NamingByField,
			FieldName: "code",
		},
		Fields: []meta.FieldDef{
			{Name: "code", FieldType: meta.FieldTypeData, Label: "Code", Required: true, Unique: true, InAPI: true},
			{Name: "title", FieldType: meta.FieldTypeData, Label: "Title", InAPI: true},
		},
	}
	env.RegisterMetaType(t, mt)

	doc := env.NewTestDoc(t, "FieldNamed", map[string]any{
		"code":  "FIELD-001",
		"title": "Test",
	})
	if doc.Name() != "FIELD-001" {
		t.Fatalf("expected name 'FIELD-001', got %q", doc.Name())
	}
}

func TestDocumentValidationRequired(t *testing.T) {
	env := testutils.NewTestEnv(t)
	mt := factory.SimpleDocType("ValRequired")
	env.RegisterMetaType(t, mt)

	// Missing required field should fail.
	ctx := env.DocContext()
	_, err := env.DocManager().Insert(ctx, "ValRequired", map[string]any{
		// customer_name is required but omitted.
		"status": "Open",
	})
	if err == nil {
		t.Fatal("expected validation error for missing required field")
	}
}

func TestDocumentValidationMaxLength(t *testing.T) {
	env := testutils.NewTestEnv(t)
	mt := &meta.MetaType{
		Name:   "ValMaxLen",
		Module: "test",
		NamingRule: meta.NamingStrategy{
			Rule: meta.NamingUUID,
		},
		Fields: []meta.FieldDef{
			{Name: "code", FieldType: meta.FieldTypeData, Label: "Code", Required: true, MaxLength: 10, InAPI: true},
		},
	}
	env.RegisterMetaType(t, mt)

	ctx := env.DocContext()
	// Value exceeding max length should fail.
	_, err := env.DocManager().Insert(ctx, "ValMaxLen", map[string]any{
		"code": "THIS_IS_WAY_TOO_LONG_FOR_10_CHARS",
	})
	if err == nil {
		t.Fatal("expected validation error for max length violation")
	}
}

func TestDocumentChildTables(t *testing.T) {
	env := testutils.NewTestEnv(t)

	childMT := factory.ChildDocType("LifecycleItem")
	env.RegisterMetaType(t, childMT)

	parentMT := factory.ComplexDocType("LifecycleOrder", "LifecycleItem")
	env.RegisterMetaType(t, parentMT)

	// Create with child rows.
	doc := env.NewTestDoc(t, "LifecycleOrder", factory.ComplexDocValues(1))
	name := doc.Name()

	// Fetch and verify children.
	fetched := env.GetTestDoc(t, "LifecycleOrder", name)
	children := fetched.GetChild("items")
	if len(children) != 5 {
		t.Fatalf("expected 5 child rows, got %d", len(children))
	}

	// Each child should have required fields.
	for i, child := range children {
		if child.Get("item_code") == nil || child.Get("item_code") == "" {
			t.Fatalf("child %d: item_code should not be empty", i)
		}
	}
}

func TestDocumentGetList(t *testing.T) {
	env := testutils.NewTestEnv(t)
	mt := factory.SimpleDocType("ListDoc")
	env.RegisterMetaType(t, mt)

	// Create multiple documents.
	for i := 1; i <= 5; i++ {
		env.NewTestDoc(t, "ListDoc", factory.SimpleDocValues(i))
	}

	// List all.
	ctx := env.DocContext()
	docs, total, err := env.DocManager().GetList(ctx, "ListDoc", document.ListOptions{
		Limit: 100,
	})
	if err != nil {
		t.Fatalf("GetList: %v", err)
	}
	if len(docs) != 5 {
		t.Fatalf("expected 5 docs, got %d", len(docs))
	}
	if total < 5 {
		t.Fatalf("expected total >= 5, got %d", total)
	}
}

func TestDocumentGetListWithFilters(t *testing.T) {
	env := testutils.NewTestEnv(t)
	mt := factory.SimpleDocType("FilterDoc")
	env.RegisterMetaType(t, mt)

	// Create docs with different statuses.
	env.NewTestDoc(t, "FilterDoc", map[string]any{
		"customer_name": "Alpha",
		"status":        "Open",
	})
	env.NewTestDoc(t, "FilterDoc", map[string]any{
		"customer_name": "Beta",
		"status":        "Closed",
	})
	env.NewTestDoc(t, "FilterDoc", map[string]any{
		"customer_name": "Gamma",
		"status":        "Open",
	})

	// Filter by status.
	ctx := env.DocContext()
	docs, _, err := env.DocManager().GetList(ctx, "FilterDoc", document.ListOptions{
		Filters: map[string]any{"status": "Open"},
		Limit:   100,
	})
	if err != nil {
		t.Fatalf("GetList with filter: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs with status=Open, got %d", len(docs))
	}
}

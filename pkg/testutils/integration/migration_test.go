//go:build integration

package integration

import (
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/testutils"
)

func TestSchemaMigrationAddField(t *testing.T) {
	env := testutils.NewTestEnv(t)

	// Register initial doctype.
	mt := &meta.MetaType{
		Name:   "MigrateDoc",
		Module: "test",
		NamingRule: meta.NamingStrategy{
			Rule: meta.NamingUUID,
		},
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData, Label: "Title", Required: true, InAPI: true},
		},
	}
	env.RegisterMetaType(t, mt)

	// Create a document.
	doc := env.NewTestDoc(t, "MigrateDoc", map[string]any{"title": "Before Migration"})

	// Add a new field.
	mt.Fields = append(mt.Fields, meta.FieldDef{
		Name:      "description",
		FieldType: meta.FieldTypeText,
		Label:     "Description",
		InAPI:     true,
	})
	env.RegisterMetaType(t, mt)

	// The old document should still be readable.
	fetched := env.GetTestDoc(t, "MigrateDoc", doc.Name())
	if fetched.Get("title") != "Before Migration" {
		t.Fatalf("expected 'Before Migration', got %v", fetched.Get("title"))
	}

	// New documents can use the new field.
	doc2 := env.NewTestDoc(t, "MigrateDoc", map[string]any{
		"title":       "After Migration",
		"description": "New field works",
	})
	fetched2 := env.GetTestDoc(t, "MigrateDoc", doc2.Name())
	if fetched2.Get("description") != "New field works" {
		t.Fatalf("expected 'New field works', got %v", fetched2.Get("description"))
	}
}

func TestDDLGeneration(t *testing.T) {
	env := testutils.NewTestEnv(t)

	// Register a doctype with various field types.
	mt := &meta.MetaType{
		Name:   "DDLDoc",
		Module: "test",
		NamingRule: meta.NamingStrategy{
			Rule: meta.NamingUUID,
		},
		Fields: []meta.FieldDef{
			{Name: "text_col", FieldType: meta.FieldTypeData, Label: "Text", InAPI: true},
			{Name: "int_col", FieldType: meta.FieldTypeInt, Label: "Int", InAPI: true},
			{Name: "float_col", FieldType: meta.FieldTypeFloat, Label: "Float", InAPI: true},
			{Name: "bool_col", FieldType: meta.FieldTypeCheck, Label: "Check", InAPI: true},
			{Name: "date_col", FieldType: meta.FieldTypeDate, Label: "Date", InAPI: true},
			{Name: "json_col", FieldType: meta.FieldTypeJSON, Label: "JSON", InAPI: true},
		},
	}
	env.RegisterMetaType(t, mt)

	// Verify table was created by inserting a row.
	doc := env.NewTestDoc(t, "DDLDoc", map[string]any{
		"text_col":  "hello",
		"int_col":   42,
		"float_col": 3.14,
		"bool_col":  true,
		"date_col":  "2024-01-15",
		"json_col":  `{"key": "value"}`,
	})

	fetched := env.GetTestDoc(t, "DDLDoc", doc.Name())
	if fetched.Get("text_col") != "hello" {
		t.Fatalf("expected 'hello', got %v", fetched.Get("text_col"))
	}
}

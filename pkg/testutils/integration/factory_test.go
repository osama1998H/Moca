//go:build integration

package integration

import (
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/testutils"
	"github.com/osama1998H/moca/pkg/testutils/factory"
)

func TestFactoryGenerateSimple(t *testing.T) {
	env := testutils.NewTestEnv(t)
	mt := factory.SimpleDocType("FactoryOrder")
	env.RegisterMetaType(t, mt)

	f := factory.New(env.Registry(), factory.WithSeed(42))

	values, err := f.Generate(env.Ctx, env.SiteName, "FactoryOrder", 5)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(values) != 5 {
		t.Fatalf("expected 5 documents, got %d", len(values))
	}

	// Each doc should have the required fields.
	for i, v := range values {
		if v["customer_name"] == nil || v["customer_name"] == "" {
			t.Fatalf("doc %d: customer_name should not be empty", i)
		}
		if v["status"] == nil || v["status"] == "" {
			t.Fatalf("doc %d: status should not be empty", i)
		}
	}
}

func TestFactoryReproducibility(t *testing.T) {
	env := testutils.NewTestEnv(t)
	mt := factory.SimpleDocType("ReproOrder")
	env.RegisterMetaType(t, mt)

	// Generate with same seed twice.
	f1 := factory.New(env.Registry(), factory.WithSeed(12345))
	f2 := factory.New(env.Registry(), factory.WithSeed(12345))

	v1, err := f1.Generate(env.Ctx, env.SiteName, "ReproOrder", 3)
	if err != nil {
		t.Fatalf("Generate 1: %v", err)
	}

	v2, err := f2.Generate(env.Ctx, env.SiteName, "ReproOrder", 3)
	if err != nil {
		t.Fatalf("Generate 2: %v", err)
	}

	// Same seed should produce identical output.
	for i := range v1 {
		if v1[i]["customer_name"] != v2[i]["customer_name"] {
			t.Fatalf("doc %d: customer_name differs: %v vs %v",
				i, v1[i]["customer_name"], v2[i]["customer_name"])
		}
		if v1[i]["status"] != v2[i]["status"] {
			t.Fatalf("doc %d: status differs: %v vs %v",
				i, v1[i]["status"], v2[i]["status"])
		}
	}
}

func TestFactoryGenerateAndInsert(t *testing.T) {
	env := testutils.NewTestEnv(t)
	mt := factory.SimpleDocType("InsertOrder")
	env.RegisterMetaType(t, mt)

	f := factory.New(env.Registry(), factory.WithSeed(42))

	docs, err := f.GenerateAndInsert(env.Ctx, env, "InsertOrder", 3)
	if err != nil {
		t.Fatalf("GenerateAndInsert: %v", err)
	}
	if len(docs) != 3 {
		t.Fatalf("expected 3 docs, got %d", len(docs))
	}

	// Each doc should have a persisted name.
	for i, doc := range docs {
		if doc.Name() == "" {
			t.Fatalf("doc %d: name should not be empty", i)
		}
		// Should be fetchable.
		fetched := env.GetTestDoc(t, "InsertOrder", doc.Name())
		if fetched.Get("customer_name") == nil {
			t.Fatalf("doc %d: fetched customer_name should not be nil", i)
		}
	}
}

func TestFactoryWithOverrides(t *testing.T) {
	env := testutils.NewTestEnv(t)
	mt := factory.SimpleDocType("OverrideOrder")
	env.RegisterMetaType(t, mt)

	f := factory.New(env.Registry(), factory.WithSeed(42))

	docs, err := f.GenerateAndInsert(env.Ctx, env, "OverrideOrder", 2,
		factory.WithOverrides(map[string]any{
			"status":        "Approved",
			"customer_name": "FixedCustomer",
		}),
	)
	if err != nil {
		t.Fatalf("GenerateAndInsert with overrides: %v", err)
	}

	for i, doc := range docs {
		fetched := env.GetTestDoc(t, "OverrideOrder", doc.Name())
		if fetched.Get("status") != "Approved" {
			t.Fatalf("doc %d: expected status 'Approved', got %v", i, fetched.Get("status"))
		}
		if fetched.Get("customer_name") != "FixedCustomer" {
			t.Fatalf("doc %d: expected customer_name 'FixedCustomer', got %v", i, fetched.Get("customer_name"))
		}
	}
}

func TestFactoryWithChildTable(t *testing.T) {
	env := testutils.NewTestEnv(t)

	childMT := factory.ChildDocType("OrderItem")
	env.RegisterMetaType(t, childMT)

	parentMT := factory.ComplexDocType("ComplexOrder", "OrderItem")
	env.RegisterMetaType(t, parentMT)

	f := factory.New(env.Registry(), factory.WithSeed(42))

	docs, err := f.GenerateAndInsert(env.Ctx, env, "ComplexOrder", 1,
		factory.WithChildCount(2, 3),
	)
	if err != nil {
		t.Fatalf("GenerateAndInsert complex: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}

	// Fetch and check child rows exist.
	fetched := env.GetTestDoc(t, "ComplexOrder", docs[0].Name())
	children := fetched.GetChild("items")
	if len(children) < 2 {
		t.Fatalf("expected at least 2 child rows, got %d", len(children))
	}
}

func TestFactoryAllFieldTypes(t *testing.T) {
	env := testutils.NewTestEnv(t)

	mt := factory.AllFieldTypesDocType("AllFields")
	env.RegisterMetaType(t, mt)

	f := factory.New(env.Registry(), factory.WithSeed(42))

	docs, err := f.GenerateAndInsert(env.Ctx, env, "AllFields", 1)
	if err != nil {
		t.Fatalf("GenerateAndInsert all field types: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}

	doc := docs[0]
	// Required field should be non-empty.
	if doc.Get("data_field") == nil || doc.Get("data_field") == "" {
		t.Fatal("data_field should not be empty (required)")
	}

	// Check a few typed fields.
	if doc.Get("check_field") == nil {
		t.Fatal("check_field should not be nil")
	}
	if doc.Get("select_field") == nil {
		t.Fatal("select_field should not be nil")
	}
	selectVal, ok := doc.Get("select_field").(string)
	if ok && selectVal != "" {
		validOptions := map[string]bool{"Draft": true, "Open": true, "Closed": true}
		if !validOptions[selectVal] {
			t.Fatalf("select_field value %q not in valid options", selectVal)
		}
	}
}

func TestFactoryLinkResolution(t *testing.T) {
	env := testutils.NewTestEnv(t)

	// Register a target doctype.
	targetMT := &meta.MetaType{
		Name:   "Customer",
		Module: "test",
		NamingRule: meta.NamingStrategy{
			Rule: meta.NamingUUID,
		},
		APIConfig: &meta.APIConfig{Enabled: true, AllowCreate: true, AllowGet: true},
		Fields: []meta.FieldDef{
			{Name: "customer_name", FieldType: meta.FieldTypeData, Label: "Name", Required: true, InAPI: true},
		},
	}
	env.RegisterMetaType(t, targetMT)

	// Register a doctype with a Link field pointing to Customer.
	sourceMT := &meta.MetaType{
		Name:   "Invoice",
		Module: "test",
		NamingRule: meta.NamingStrategy{
			Rule: meta.NamingUUID,
		},
		APIConfig: &meta.APIConfig{Enabled: true, AllowCreate: true, AllowGet: true},
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData, Label: "Title", Required: true, InAPI: true},
			{Name: "customer", FieldType: meta.FieldTypeLink, Label: "Customer", Options: "Customer", Required: true, InAPI: true},
			{Name: "amount", FieldType: meta.FieldTypeCurrency, Label: "Amount", InAPI: true},
		},
	}
	env.RegisterMetaType(t, sourceMT)

	f := factory.New(env.Registry(), factory.WithSeed(42))

	// GenerateAndInsert should auto-create Customer dependencies.
	docs, err := f.GenerateAndInsert(env.Ctx, env, "Invoice", 2)
	if err != nil {
		t.Fatalf("GenerateAndInsert with Link: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}

	// Each invoice should have a valid customer reference.
	for i, doc := range docs {
		customerName := doc.Get("customer")
		if customerName == nil || customerName == "" {
			t.Fatalf("doc %d: customer Link field should be filled", i)
		}
		// The linked customer should exist.
		_ = env.GetTestDoc(t, "Customer", customerName.(string))
	}
}

func TestFactoryNoChildren(t *testing.T) {
	env := testutils.NewTestEnv(t)

	childMT := factory.ChildDocType("NoChildItem")
	env.RegisterMetaType(t, childMT)

	parentMT := factory.ComplexDocType("NoChildOrder", "NoChildItem")
	env.RegisterMetaType(t, parentMT)

	f := factory.New(env.Registry(), factory.WithSeed(42))

	docs, err := f.GenerateAndInsert(env.Ctx, env, "NoChildOrder", 1,
		factory.WithChildren(false),
	)
	if err != nil {
		t.Fatalf("GenerateAndInsert without children: %v", err)
	}

	// Document should have no child rows.
	fetched := env.GetTestDoc(t, "NoChildOrder", docs[0].Name())
	children := fetched.GetChild("items")
	if len(children) != 0 {
		t.Fatalf("expected 0 child rows, got %d", len(children))
	}
}

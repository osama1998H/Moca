package api

import (
	"encoding/json"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

func TestFieldTypeToOpenAPI(t *testing.T) {
	tests := []struct {
		ft       meta.FieldType
		wantType string
		wantFmt  string
	}{
		// String types
		{meta.FieldTypeData, "string", ""},
		{meta.FieldTypeText, "string", ""},
		{meta.FieldTypeLongText, "string", ""},
		{meta.FieldTypeCode, "string", ""},
		{meta.FieldTypeMarkdown, "string", ""},
		{meta.FieldTypeHTMLEditor, "string", ""},

		// Integer
		{meta.FieldTypeInt, "integer", "int64"},

		// Number types
		{meta.FieldTypeFloat, "number", "double"},
		{meta.FieldTypeCurrency, "number", "double"},
		{meta.FieldTypePercent, "number", "double"},

		// Boolean
		{meta.FieldTypeCheck, "boolean", ""},

		// Date/time
		{meta.FieldTypeDate, "string", "date"},
		{meta.FieldTypeDatetime, "string", "date-time"},
		{meta.FieldTypeTime, "string", "time"},
		{meta.FieldTypeDuration, "string", "duration"},

		// Select
		{meta.FieldTypeSelect, "string", ""},

		// References
		{meta.FieldTypeLink, "string", ""},
		{meta.FieldTypeDynamicLink, "string", ""},

		// Files
		{meta.FieldTypeAttach, "string", "uri"},
		{meta.FieldTypeAttachImage, "string", "uri"},

		// Tables
		{meta.FieldTypeTable, "array", ""},
		{meta.FieldTypeTableMultiSelect, "array", ""},

		// Structured
		{meta.FieldTypeJSON, "object", ""},
		{meta.FieldTypeGeolocation, "object", ""},

		// Special
		{meta.FieldTypePassword, "string", "password"},
		{meta.FieldTypeColor, "string", ""},
		{meta.FieldTypeRating, "number", ""},
		{meta.FieldTypeSignature, "string", ""},
		{meta.FieldTypeBarcode, "string", ""},

		// Layout-only types return empty
		{meta.FieldTypeSectionBreak, "", ""},
		{meta.FieldTypeColumnBreak, "", ""},
		{meta.FieldTypeTabBreak, "", ""},
		{meta.FieldTypeHTML, "", ""},
		{meta.FieldTypeButton, "", ""},
		{meta.FieldTypeHeading, "", ""},
	}

	for _, tt := range tests {
		gotType, gotFmt := FieldTypeToOpenAPI(tt.ft)
		if gotType != tt.wantType || gotFmt != tt.wantFmt {
			t.Errorf("FieldTypeToOpenAPI(%s) = (%q, %q), want (%q, %q)",
				tt.ft, gotType, gotFmt, tt.wantType, tt.wantFmt)
		}
	}
}

func TestSchemaFromMetaType(t *testing.T) {
	mt := &meta.MetaType{
		Name: "SalesOrder",
		APIConfig: &meta.APIConfig{
			Enabled:       true,
			ExcludeFields: []string{"internal_note"},
			AlwaysInclude: []string{"always_field"},
			ComputedFields: []meta.ComputedField{
				{Name: "total_display", Type: "string"},
				{Name: "item_count", Type: "integer"},
			},
		},
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData, Label: "Title", Required: true, InAPI: true, MaxLength: 140},
			{Name: "amount", FieldType: meta.FieldTypeCurrency, Label: "Amount", MinValue: ptrFloat(0)},
			{Name: "status", FieldType: meta.FieldTypeSelect, Options: "Draft\nSubmitted\nCancelled"},
			{Name: "customer", FieldType: meta.FieldTypeLink, Options: "Customer"},
			{Name: "items", FieldType: meta.FieldTypeTable, Options: "SalesOrderItem"},
			{Name: "internal_note", FieldType: meta.FieldTypeText},           // excluded
			{Name: "always_field", FieldType: meta.FieldTypeData, InAPI: true}, // always included
			{Name: "readonly_field", FieldType: meta.FieldTypeData, APIReadOnly: true},
			// Layout-only field — should be skipped.
			{Name: "section1", FieldType: meta.FieldTypeSectionBreak},
		},
	}

	schema := SchemaFromMetaType(mt, mt.APIConfig.ExcludeFields, mt.APIConfig.AlwaysInclude)

	// Type check.
	if schema.Type != "object" {
		t.Fatalf("schema type = %q, want object", schema.Type)
	}

	// Check that standard fields exist.
	for _, sf := range []string{"name", "owner", "creation", "modified", "modified_by"} {
		if _, ok := schema.Properties[sf]; !ok {
			t.Errorf("missing standard field %q", sf)
		}
	}

	// title should exist with maxLength.
	if p, ok := schema.Properties["title"]; !ok {
		t.Error("missing field 'title'")
	} else {
		if p.Type != "string" {
			t.Errorf("title type = %q, want string", p.Type)
		}
		if p.MaxLength == nil || *p.MaxLength != 140 {
			t.Error("title maxLength should be 140")
		}
	}

	// amount should have minimum.
	if p, ok := schema.Properties["amount"]; !ok {
		t.Error("missing field 'amount'")
	} else {
		if p.Type != "number" {
			t.Errorf("amount type = %q, want number", p.Type)
		}
		if p.Minimum == nil || *p.Minimum != 0 {
			t.Error("amount minimum should be 0")
		}
	}

	// status should have enum.
	if p, ok := schema.Properties["status"]; !ok {
		t.Error("missing field 'status'")
	} else {
		if len(p.Enum) != 3 {
			t.Errorf("status enum count = %d, want 3", len(p.Enum))
		}
	}

	// customer should be Link description.
	if p, ok := schema.Properties["customer"]; !ok {
		t.Error("missing field 'customer'")
	} else {
		if p.Description != "Link to Customer" {
			t.Errorf("customer desc = %q, want 'Link to Customer'", p.Description)
		}
	}

	// items should be array with $ref.
	if p, ok := schema.Properties["items"]; !ok {
		t.Error("missing field 'items'")
	} else {
		if p.Type != "array" {
			t.Errorf("items type = %q, want array", p.Type)
		}
		if p.Items == nil || p.Items.Ref != "#/components/schemas/SalesOrderItem" {
			t.Error("items.$ref should reference SalesOrderItem")
		}
	}

	// internal_note should be excluded.
	if _, ok := schema.Properties["internal_note"]; ok {
		t.Error("excluded field 'internal_note' should not appear")
	}

	// always_field should be included.
	if _, ok := schema.Properties["always_field"]; !ok {
		t.Error("always_field should be included")
	}

	// readonly_field should be readOnly.
	if p, ok := schema.Properties["readonly_field"]; !ok {
		t.Error("missing field 'readonly_field'")
	} else if !p.ReadOnly {
		t.Error("readonly_field should be readOnly")
	}

	// Layout-only section should not appear.
	if _, ok := schema.Properties["section1"]; ok {
		t.Error("layout-only field 'section1' should not appear")
	}

	// Required should contain "title".
	found := false
	for _, r := range schema.Required {
		if r == "title" {
			found = true
		}
	}
	if !found {
		t.Error("'title' should be in required list")
	}

	// ComputedFields.
	if p, ok := schema.Properties["total_display"]; !ok {
		t.Error("missing computed field 'total_display'")
	} else if !p.ReadOnly || p.Type != "string" {
		t.Errorf("total_display: readOnly=%v type=%s, want true/string", p.ReadOnly, p.Type)
	}
	if p, ok := schema.Properties["item_count"]; !ok {
		t.Error("missing computed field 'item_count'")
	} else if p.Type != "integer" {
		t.Errorf("item_count type = %q, want integer", p.Type)
	}
}

func TestSchemaFromMetaTypeAPIAlias(t *testing.T) {
	mt := &meta.MetaType{
		Name: "Test",
		Fields: []meta.FieldDef{
			{Name: "first_name", FieldType: meta.FieldTypeData, APIAlias: "firstName"},
		},
	}

	schema := SchemaFromMetaType(mt, nil, nil)
	if _, ok := schema.Properties["firstName"]; !ok {
		t.Error("APIAlias field 'firstName' should appear instead of 'first_name'")
	}
	if _, ok := schema.Properties["first_name"]; ok {
		t.Error("original field name 'first_name' should not appear when APIAlias is set")
	}
}

func TestGenerateSpec(t *testing.T) {
	metatypes := []meta.MetaType{
		{
			Name: "SalesOrder",
			APIConfig: &meta.APIConfig{
				Enabled:     true,
				AllowList:   true,
				AllowGet:    true,
				AllowCreate: true,
				AllowUpdate: true,
				AllowDelete: true,
				CustomEndpoints: []meta.CustomEndpoint{
					{Method: "POST", Path: "submit", Handler: "handleSubmit"},
				},
			},
			Fields: []meta.FieldDef{
				{Name: "title", FieldType: meta.FieldTypeData},
			},
		},
	}

	methods := []string{"send_email", "get_count"}
	spec := GenerateSpec(metatypes, methods, SpecOptions{Title: "Test API"})

	// Basic spec structure.
	if spec.OpenAPI != "3.0.3" {
		t.Errorf("openapi = %q, want 3.0.3", spec.OpenAPI)
	}
	if spec.Info.Title != "Test API" {
		t.Errorf("title = %q, want 'Test API'", spec.Info.Title)
	}

	// Security schemes.
	if spec.Components.SecuritySchemes == nil {
		t.Fatal("missing security schemes")
	}
	for _, name := range []string{"bearerAuth", "apiKeyAuth", "sessionAuth"} {
		if _, ok := spec.Components.SecuritySchemes[name]; !ok {
			t.Errorf("missing security scheme %q", name)
		}
	}

	// Component schema.
	if _, ok := spec.Components.Schemas["SalesOrder"]; !ok {
		t.Error("missing SalesOrder component schema")
	}

	// CRUD paths.
	assertPathOp(t, spec, "/api/v1/resource/SalesOrder", "GET")
	assertPathOp(t, spec, "/api/v1/resource/SalesOrder/{name}", "GET")
	assertPathOp(t, spec, "/api/v1/resource/SalesOrder", "POST")
	assertPathOp(t, spec, "/api/v1/resource/SalesOrder/{name}", "PUT")
	assertPathOp(t, spec, "/api/v1/resource/SalesOrder/{name}", "DELETE")

	// Meta path.
	assertPathOp(t, spec, "/api/v1/meta/SalesOrder", "GET")

	// Custom endpoint.
	assertPathOp(t, spec, "/api/v1/custom/SalesOrder/submit", "POST")

	// Whitelisted methods.
	assertPathOp(t, spec, "/api/v1/method/send_email", "GET")
	assertPathOp(t, spec, "/api/v1/method/send_email", "POST")
	assertPathOp(t, spec, "/api/v1/method/get_count", "GET")
	assertPathOp(t, spec, "/api/v1/method/get_count", "POST")

	// Error schema.
	if _, ok := spec.Components.Schemas["ErrorResponse"]; !ok {
		t.Error("missing ErrorResponse schema")
	}

	// Verify spec serializes to valid JSON.
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if len(data) == 0 {
		t.Error("empty JSON output")
	}
}

func TestGenerateSpecDisabledEndpoints(t *testing.T) {
	metatypes := []meta.MetaType{
		{
			Name: "ReadOnlyType",
			APIConfig: &meta.APIConfig{
				Enabled:     true,
				AllowList:   true,
				AllowGet:    true,
				AllowCreate: false,
				AllowUpdate: false,
				AllowDelete: false,
			},
			Fields: []meta.FieldDef{
				{Name: "value", FieldType: meta.FieldTypeData},
			},
		},
	}

	spec := GenerateSpec(metatypes, nil, SpecOptions{})

	// GET should exist.
	assertPathOp(t, spec, "/api/v1/resource/ReadOnlyType", "GET")
	assertPathOp(t, spec, "/api/v1/resource/ReadOnlyType/{name}", "GET")

	// POST, PUT, DELETE should not exist.
	assertNoPathOp(t, spec, "/api/v1/resource/ReadOnlyType", "POST")
	assertNoPathOp(t, spec, "/api/v1/resource/ReadOnlyType/{name}", "PUT")
	assertNoPathOp(t, spec, "/api/v1/resource/ReadOnlyType/{name}", "DELETE")
}

func TestGenerateSpecSingleDocType(t *testing.T) {
	metatypes := []meta.MetaType{
		{
			Name:     "SystemSettings",
			IsSingle: true,
			APIConfig: &meta.APIConfig{
				Enabled:     true,
				AllowGet:    true,
				AllowUpdate: true,
				AllowCreate: true, // should be ignored for IsSingle
				AllowDelete: true, // should be ignored for IsSingle
			},
			Fields: []meta.FieldDef{
				{Name: "app_name", FieldType: meta.FieldTypeData},
			},
		},
	}

	spec := GenerateSpec(metatypes, nil, SpecOptions{})

	// GET and PUT should exist.
	assertPathOp(t, spec, "/api/v1/resource/SystemSettings/{name}", "GET")
	assertPathOp(t, spec, "/api/v1/resource/SystemSettings/{name}", "PUT")

	// Create and Delete should NOT exist for single types.
	assertNoPathOp(t, spec, "/api/v1/resource/SystemSettings", "POST")
	assertNoPathOp(t, spec, "/api/v1/resource/SystemSettings/{name}", "DELETE")
}

func TestGenerateSpecDisabledDocType(t *testing.T) {
	metatypes := []meta.MetaType{
		{
			Name:      "Disabled",
			APIConfig: &meta.APIConfig{Enabled: false},
		},
		{
			Name: "NoConfig",
		},
	}

	spec := GenerateSpec(metatypes, nil, SpecOptions{})

	if len(spec.Paths) != 0 {
		t.Errorf("expected 0 paths for disabled/no-config doctypes, got %d", len(spec.Paths))
	}
}

func TestGenerateSpecChildTable(t *testing.T) {
	metatypes := []meta.MetaType{
		{
			Name: "Parent",
			APIConfig: &meta.APIConfig{
				Enabled:  true,
				AllowGet: true,
			},
			Fields: []meta.FieldDef{
				{Name: "children", FieldType: meta.FieldTypeTable, Options: "ChildItem"},
			},
		},
	}

	spec := GenerateSpec(metatypes, nil, SpecOptions{})
	schema := spec.Components.Schemas["Parent"]
	if schema == nil {
		t.Fatal("missing Parent schema")
	}
	children, ok := schema.Properties["children"]
	if !ok {
		t.Fatal("missing children property")
	}
	if children.Type != "array" {
		t.Errorf("children type = %q, want array", children.Type)
	}
	if children.Items == nil || children.Items.Ref != "#/components/schemas/ChildItem" {
		t.Error("children items should $ref ChildItem")
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func assertPathOp(t *testing.T, spec *OpenAPISpec, path, method string) {
	t.Helper()
	pi, ok := spec.Paths[path]
	if !ok {
		t.Errorf("path %q not found", path)
		return
	}
	if getOp(pi, method) == nil {
		t.Errorf("path %q has no %s operation", path, method)
	}
}

func assertNoPathOp(t *testing.T, spec *OpenAPISpec, path, method string) {
	t.Helper()
	pi, ok := spec.Paths[path]
	if !ok {
		return // path not existing is fine
	}
	if getOp(pi, method) != nil {
		t.Errorf("path %q should NOT have %s operation", path, method)
	}
}

func getOp(pi *PathItem, method string) *Operation {
	switch method {
	case "GET":
		return pi.Get
	case "POST":
		return pi.Post
	case "PUT":
		return pi.Put
	case "DELETE":
		return pi.Delete
	case "PATCH":
		return pi.Patch
	}
	return nil
}

func ptrFloat(v float64) *float64 {
	return &v
}

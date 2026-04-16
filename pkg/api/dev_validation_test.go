package api

import (
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

// ── ValidateDocTypeName ──────────────────────────────────────────────────────

func TestValidateDocTypeName_Valid(t *testing.T) {
	cases := []string{
		"Book",
		"SalesOrder",
		"HTTPConfig",
		"A",
		"Item123",
		"DocType",
	}
	for _, name := range cases {
		if err := ValidateDocTypeName(name); err != nil {
			t.Errorf("ValidateDocTypeName(%q) returned unexpected error: %v", name, err)
		}
	}
}

func TestValidateDocTypeName_Invalid(t *testing.T) {
	cases := []struct {
		name string
		desc string
	}{
		{"", "empty name"},
		{"book", "lowercase start"},
		{"sales_order", "contains underscore"},
		{"Sales Order", "contains space"},
		{"Sales-Order", "contains hyphen"},
		{"123Book", "starts with digit"},
		{"1", "single digit"},
	}
	for _, tc := range cases {
		if err := ValidateDocTypeName(tc.name); err == nil {
			t.Errorf("ValidateDocTypeName(%q) expected error for %s, got nil", tc.name, tc.desc)
		}
	}
}

// ── ValidateFieldName ────────────────────────────────────────────────────────

func TestValidateFieldName_Valid(t *testing.T) {
	cases := []string{
		"title",
		"first_name",
		"item2",
		"a",
		"field_123",
		"order_total",
	}
	for _, name := range cases {
		if err := ValidateFieldName(name); err != nil {
			t.Errorf("ValidateFieldName(%q) returned unexpected error: %v", name, err)
		}
	}
}

func TestValidateFieldName_Invalid(t *testing.T) {
	cases := []struct {
		name string
		desc string
	}{
		{"", "empty name"},
		{"Title", "starts with uppercase"},
		{"first-name", "contains hyphen"},
		{"first name", "contains space"},
		{"_extra", "reserved name starting with underscore"},
		{"1field", "starts with digit"},
		// reserved names
		{"name", "reserved: name"},
		{"created_at", "reserved: created_at"},
		{"modified_at", "reserved: modified_at"},
		{"owner", "reserved: owner"},
		{"modified_by", "reserved: modified_by"},
		{"creation", "reserved: creation"},
		{"modified", "reserved: modified"},
		{"docstatus", "reserved: docstatus"},
		{"idx", "reserved: idx"},
		{"workflow_state", "reserved: workflow_state"},
		{"parent", "reserved: parent"},
		{"parenttype", "reserved: parenttype"},
		{"parentfield", "reserved: parentfield"},
	}
	for _, tc := range cases {
		if err := ValidateFieldName(tc.name); err == nil {
			t.Errorf("ValidateFieldName(%q) expected error for %s, got nil", tc.name, tc.desc)
		}
	}
}

// ── ValidateFieldDefs ──────────────────────────────────────────────────────

func TestValidateFieldDefs_Valid(t *testing.T) {
	fields := map[string]meta.FieldDef{
		"title":     {FieldType: "Data", Name: "title"},
		"full_name": {FieldType: "Text", Name: "full_name"},
	}
	if err := ValidateFieldDefs(fields); err != nil {
		t.Errorf("ValidateFieldDefs returned unexpected error: %v", err)
	}
}

func TestValidateFieldDefs_EmptyFieldType(t *testing.T) {
	fields := map[string]meta.FieldDef{
		"title":     {FieldType: "Data", Name: "title"},
		"full_name": {FieldType: "", Name: "full_name"},
	}
	err := ValidateFieldDefs(fields)
	if err == nil {
		t.Error("ValidateFieldDefs expected error for empty field_type, got nil")
	}
}

func TestValidateFieldDefs_EmptyMap(t *testing.T) {
	if err := ValidateFieldDefs(map[string]meta.FieldDef{}); err != nil {
		t.Errorf("ValidateFieldDefs returned unexpected error for empty map: %v", err)
	}
}

func TestValidateFieldDefs_UnrecognizedFieldType(t *testing.T) {
	fields := map[string]meta.FieldDef{
		"title": {FieldType: "Data", Name: "title"},
		"bad":   {FieldType: "NotAType", Name: "bad"},
	}
	err := ValidateFieldDefs(fields)
	if err == nil {
		t.Error("ValidateFieldDefs expected error for unrecognized field_type, got nil")
	}
}

func TestValidateFieldDefs_AllValidTypes(t *testing.T) {
	fields := map[string]meta.FieldDef{
		"f1": {FieldType: "Data", Name: "f1"},
		"f2": {FieldType: "Int", Name: "f2"},
		"f3": {FieldType: "Currency", Name: "f3"},
		"f4": {FieldType: "Date", Name: "f4"},
		"f5": {FieldType: "Link", Name: "f5"},
	}
	if err := ValidateFieldDefs(fields); err != nil {
		t.Errorf("ValidateFieldDefs returned unexpected error: %v", err)
	}
}

// ── ValidateAppName ─────────────────────────────────────────────────────────

func TestValidateAppName_Valid(t *testing.T) {
	cases := []string{"core", "my-app", "app_v2", "a", "test123"}
	for _, name := range cases {
		if err := ValidateAppName(name); err != nil {
			t.Errorf("ValidateAppName(%q) returned unexpected error: %v", name, err)
		}
	}
}

func TestValidateAppName_Invalid(t *testing.T) {
	cases := []struct {
		name string
		desc string
	}{
		{"", "empty"},
		{"../../etc", "path traversal"},
		{"foo/bar", "contains slash"},
		{".hidden", "starts with dot"},
		{"MyApp", "contains uppercase"},
		{"123app", "starts with digit"},
		{"app name", "contains space"},
	}
	for _, tc := range cases {
		if err := ValidateAppName(tc.name); err == nil {
			t.Errorf("ValidateAppName(%q) expected error for %s, got nil", tc.name, tc.desc)
		}
	}
}

// ── ValidateModuleName ──────────────────────────────────────────────────────

func TestValidateModuleName_Valid(t *testing.T) {
	cases := []string{"core", "selling", "hr_module", "mod-1"}
	for _, name := range cases {
		if err := ValidateModuleName(name); err != nil {
			t.Errorf("ValidateModuleName(%q) returned unexpected error: %v", name, err)
		}
	}
}

func TestValidateModuleName_Invalid(t *testing.T) {
	cases := []struct {
		name string
		desc string
	}{
		{"", "empty"},
		{"../etc", "path traversal"},
		{"foo/bar", "contains slash"},
		{".hidden", "starts with dot"},
		{"Selling", "contains uppercase"},
	}
	for _, tc := range cases {
		if err := ValidateModuleName(tc.name); err == nil {
			t.Errorf("ValidateModuleName(%q) expected error for %s, got nil", tc.name, tc.desc)
		}
	}
}

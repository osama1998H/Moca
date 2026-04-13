package api

import "testing"

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

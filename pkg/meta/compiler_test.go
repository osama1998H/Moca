package meta_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// minimalValidJSON returns the smallest JSON snippet that passes all 12 rules.
// Callers may override individual fields by unmarshalling on top of this base.
func minimalValidJSON(name, module string) []byte {
	return []byte(`{
		"name": "` + name + `",
		"module": "` + module + `",
		"fields": [
			{"name": "title", "field_type": "Data", "label": "Title"}
		]
	}`)
}

// findError searches a *meta.CompileErrors for an entry whose Field equals field.
// Returns nil if the error is not a *meta.CompileErrors or if no entry matches.
func findError(t *testing.T, err error, field string) *meta.CompileError {
	t.Helper()
	var ce *meta.CompileErrors
	if !errors.As(err, &ce) {
		return nil
	}
	for i := range ce.Errors {
		if ce.Errors[i].Field == field {
			return &ce.Errors[i]
		}
	}
	return nil
}

// assertCompileError fails the test if err does not contain a CompileError
// for the given field.
func assertCompileError(t *testing.T, err error, field string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a compile error on field %q but got nil", field)
	}
	if ce := findError(t, err, field); ce == nil {
		t.Errorf("expected compile error on field %q; got: %v", field, err)
	}
}

// assertNoError fails the test if err is non-nil.
func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── happy-path tests ─────────────────────────────────────────────────────────

func TestCompile_ValidSalesOrder(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "SalesOrder.json"))
	if err != nil {
		t.Fatalf("read testdata/SalesOrder.json: %v", err)
	}

	mt, err := meta.Compile(data)
	assertNoError(t, err)

	if mt.Name != "SalesOrder" {
		t.Errorf("Name: got %q, want %q", mt.Name, "SalesOrder")
	}
	if mt.Module != "selling" {
		t.Errorf("Module: got %q, want %q", mt.Module, "selling")
	}
	if mt.NamingRule.Rule != meta.NamingByPattern {
		t.Errorf("NamingRule.Rule: got %q, want %q", mt.NamingRule.Rule, meta.NamingByPattern)
	}
	if mt.NamingRule.Pattern != "SO-.####" {
		t.Errorf("NamingRule.Pattern: got %q, want %q", mt.NamingRule.Pattern, "SO-.####")
	}
	if mt.TitleField != "customer_name" {
		t.Errorf("TitleField: got %q, want %q", mt.TitleField, "customer_name")
	}
	if mt.SortField != "creation" {
		t.Errorf("SortField: got %q, want %q", mt.SortField, "creation")
	}
	if len(mt.SearchFields) != 2 {
		t.Errorf("SearchFields: got %d entries, want 2", len(mt.SearchFields))
	}
	if len(mt.Fields) != 8 {
		t.Errorf("Fields: got %d, want 8", len(mt.Fields))
	}
	t.Logf("SalesOrder compiled successfully: %d fields, naming rule %q", len(mt.Fields), mt.NamingRule.Rule)
}

func TestCompile_DefaultNamingRule(t *testing.T) {
	// When NamingRule.Rule is omitted, it should default to UUID after compilation.
	data := []byte(`{
		"name": "Invoice",
		"module": "accounts",
		"fields": [{"name": "amount", "field_type": "Currency", "label": "Amount"}]
	}`)

	mt, err := meta.Compile(data)
	assertNoError(t, err)

	if mt.NamingRule.Rule != meta.NamingUUID {
		t.Errorf("NamingRule.Rule: got %q, want %q (default)", mt.NamingRule.Rule, meta.NamingUUID)
	}
	t.Logf("empty NamingRule.Rule defaulted to %q", mt.NamingRule.Rule)
}

func TestCompile_EmptyOptionalFields(t *testing.T) {
	// Minimal MetaType with no SearchFields, TitleField, or SortField — all valid.
	data := minimalValidJSON("Customer", "crm")
	mt, err := meta.Compile(data)
	assertNoError(t, err)

	if len(mt.SearchFields) != 0 {
		t.Errorf("SearchFields should be empty; got %v", mt.SearchFields)
	}
	if mt.TitleField != "" {
		t.Errorf("TitleField should be empty; got %q", mt.TitleField)
	}
	t.Logf("minimal MetaType %q compiled without errors", mt.Name)
}

// ── rule-1 ────────────────────────────────────────────────────────────────────

func TestCompile_MissingName(t *testing.T) {
	data := []byte(`{
		"name": "",
		"module": "core",
		"fields": [{"name": "x", "field_type": "Data", "label": "X"}]
	}`)
	_, err := meta.Compile(data)
	assertCompileError(t, err, "name")
	t.Logf("missing name error: %v", err)
}

// ── rule-2 ────────────────────────────────────────────────────────────────────

func TestCompile_MissingModule(t *testing.T) {
	data := []byte(`{
		"name": "Ticket",
		"module": "",
		"fields": [{"name": "subject", "field_type": "Data", "label": "Subject"}]
	}`)
	_, err := meta.Compile(data)
	assertCompileError(t, err, "module")
	t.Logf("missing module error: %v", err)
}

// ── rule-3 ────────────────────────────────────────────────────────────────────

func TestCompile_UnknownFieldType(t *testing.T) {
	data := []byte(`{
		"name": "Widget",
		"module": "core",
		"fields": [{"name": "value", "field_type": "FakeType", "label": "Value"}]
	}`)
	_, err := meta.Compile(data)
	assertCompileError(t, err, "fields[0].field_type")
	t.Logf("unknown field type error: %v", err)
}

// ── rule-4 ────────────────────────────────────────────────────────────────────

func TestCompile_DuplicateFieldNames(t *testing.T) {
	data := []byte(`{
		"name": "Item",
		"module": "stock",
		"fields": [
			{"name": "amount", "field_type": "Currency", "label": "Amount"},
			{"name": "amount", "field_type": "Currency", "label": "Amount Copy"}
		]
	}`)
	_, err := meta.Compile(data)
	assertCompileError(t, err, "fields[1].name")
	t.Logf("duplicate field name error: %v", err)
}

// ── rule-5 ────────────────────────────────────────────────────────────────────

func TestCompile_LinkWithoutOptions(t *testing.T) {
	data := []byte(`{
		"name": "Task",
		"module": "project",
		"fields": [{"name": "project", "field_type": "Link", "label": "Project", "options": ""}]
	}`)
	_, err := meta.Compile(data)
	assertCompileError(t, err, "fields[0].options")
	t.Logf("Link without Options error: %v", err)
}

func TestCompile_DynamicLinkWithoutOptions(t *testing.T) {
	data := []byte(`{
		"name": "Attachment",
		"module": "core",
		"fields": [{"name": "ref_doc", "field_type": "DynamicLink", "label": "Ref Doc"}]
	}`)
	_, err := meta.Compile(data)
	assertCompileError(t, err, "fields[0].options")
	t.Logf("DynamicLink without Options error: %v", err)
}

func TestCompile_LinkWithOptions(t *testing.T) {
	// Positive case: Link with a non-empty Options should not produce an error.
	data := []byte(`{
		"name": "Task",
		"module": "project",
		"fields": [{"name": "project", "field_type": "Link", "label": "Project", "options": "Project"}]
	}`)
	_, err := meta.Compile(data)
	assertNoError(t, err)
	t.Logf("Link with Options compiled successfully")
}

// ── rule-6 ────────────────────────────────────────────────────────────────────

func TestCompile_TableWithoutOptions(t *testing.T) {
	data := []byte(`{
		"name": "Order",
		"module": "selling",
		"fields": [{"name": "lines", "field_type": "Table", "label": "Lines"}]
	}`)
	_, err := meta.Compile(data)
	assertCompileError(t, err, "fields[0].options")
	t.Logf("Table without Options error: %v", err)
}

func TestCompile_TableMultiSelectWithoutOptions(t *testing.T) {
	data := []byte(`{
		"name": "Doc",
		"module": "core",
		"fields": [{"name": "tags", "field_type": "TableMultiSelect", "label": "Tags"}]
	}`)
	_, err := meta.Compile(data)
	assertCompileError(t, err, "fields[0].options")
	t.Logf("TableMultiSelect without Options error: %v", err)
}

// ── rule-7 ────────────────────────────────────────────────────────────────────

func TestCompile_SearchFieldUnknown(t *testing.T) {
	data := []byte(`{
		"name": "Lead",
		"module": "crm",
		"search_fields": ["nonexistent"],
		"fields": [{"name": "email", "field_type": "Data", "label": "Email"}]
	}`)
	_, err := meta.Compile(data)
	assertCompileError(t, err, "search_fields")
	t.Logf("unknown search_field error: %v", err)
}

// ── rule-8 ────────────────────────────────────────────────────────────────────

func TestCompile_TitleFieldUnknown(t *testing.T) {
	data := []byte(`{
		"name": "Event",
		"module": "hr",
		"title_field": "ghost_field",
		"fields": [{"name": "description", "field_type": "Text", "label": "Description"}]
	}`)
	_, err := meta.Compile(data)
	assertCompileError(t, err, "title_field")
	t.Logf("unknown title_field error: %v", err)
}

// ── rule-9 ────────────────────────────────────────────────────────────────────

func TestCompile_SortFieldUnknown(t *testing.T) {
	data := []byte(`{
		"name": "Report",
		"module": "core",
		"sort_field": "ghost_column",
		"fields": [{"name": "name", "field_type": "Data", "label": "Name"}]
	}`)
	_, err := meta.Compile(data)
	assertCompileError(t, err, "sort_field")
	t.Logf("unknown sort_field error: %v", err)
}

func TestCompile_SortFieldStandardColumn(t *testing.T) {
	// "creation" is a standard column — should be accepted even without a matching field.
	data := []byte(`{
		"name": "Report",
		"module": "core",
		"sort_field": "creation",
		"fields": [{"name": "title", "field_type": "Data", "label": "Title"}]
	}`)
	_, err := meta.Compile(data)
	assertNoError(t, err)
	t.Logf("sort_field=creation (standard column) accepted without error")
}

func TestCompile_SortFieldDefinedField(t *testing.T) {
	// "amount" is a user-defined field — should be accepted.
	data := []byte(`{
		"name": "Invoice",
		"module": "accounts",
		"sort_field": "amount",
		"fields": [{"name": "amount", "field_type": "Currency", "label": "Amount"}]
	}`)
	_, err := meta.Compile(data)
	assertNoError(t, err)
	t.Logf("sort_field pointing to defined field accepted without error")
}

// ── rule-10 ───────────────────────────────────────────────────────────────────

func TestCompile_InvalidNamingRule(t *testing.T) {
	data := []byte(`{
		"name": "Doc",
		"module": "core",
		"naming_rule": {"rule": "invalid_rule"},
		"fields": [{"name": "x", "field_type": "Data", "label": "X"}]
	}`)
	_, err := meta.Compile(data)
	assertCompileError(t, err, "naming_rule.rule")
	t.Logf("invalid naming rule error: %v", err)
}

func TestCompile_AllValidNamingRules(t *testing.T) {
	cases := []struct {
		name    string
		ruleDoc string
	}{
		{"autoincrement", `{"rule": "autoincrement"}`},
		{"hash", `{"rule": "hash"}`},
		{"uuid", `{"rule": "uuid"}`},
		{"custom", `{"rule": "custom"}`},
		{"field", `{"rule": "field", "field_name": "title"}`},
		{"pattern", `{"rule": "pattern", "pattern": "DOC-.####"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(`{
				"name": "Doc",
				"module": "core",
				"naming_rule": ` + tc.ruleDoc + `,
				"fields": [{"name": "title", "field_type": "Data", "label": "Title"}]
			}`)
			_, err := meta.Compile(data)
			assertNoError(t, err)
			t.Logf("naming rule %q accepted", tc.name)
		})
	}
}

// ── rule-11 ───────────────────────────────────────────────────────────────────

func TestCompile_NamingFieldMissingFieldName(t *testing.T) {
	data := []byte(`{
		"name": "Doc",
		"module": "core",
		"naming_rule": {"rule": "field", "field_name": ""},
		"fields": [{"name": "title", "field_type": "Data", "label": "Title"}]
	}`)
	_, err := meta.Compile(data)
	assertCompileError(t, err, "naming_rule.field_name")
	t.Logf("NamingByField missing FieldName error: %v", err)
}

func TestCompile_NamingFieldInvalidFieldName(t *testing.T) {
	data := []byte(`{
		"name": "Doc",
		"module": "core",
		"naming_rule": {"rule": "field", "field_name": "nonexistent"},
		"fields": [{"name": "title", "field_type": "Data", "label": "Title"}]
	}`)
	_, err := meta.Compile(data)
	assertCompileError(t, err, "naming_rule.field_name")
	t.Logf("NamingByField invalid FieldName error: %v", err)
}

// ── rule-12 ───────────────────────────────────────────────────────────────────

func TestCompile_NamingPatternMissingPattern(t *testing.T) {
	data := []byte(`{
		"name": "Doc",
		"module": "core",
		"naming_rule": {"rule": "pattern", "pattern": ""},
		"fields": [{"name": "title", "field_type": "Data", "label": "Title"}]
	}`)
	_, err := meta.Compile(data)
	assertCompileError(t, err, "naming_rule.pattern")
	t.Logf("NamingByPattern missing Pattern error: %v", err)
}

// ── error accumulation ────────────────────────────────────────────────────────

func TestCompile_MultipleErrors(t *testing.T) {
	// Name empty (rule 1), Module empty (rule 2), field with invalid type (rule 3).
	// All three should be collected in a single *CompileErrors — not short-circuited.
	data := []byte(`{
		"name": "",
		"module": "",
		"fields": [{"name": "x", "field_type": "BOGUS", "label": "X"}]
	}`)
	_, err := meta.Compile(data)
	if err == nil {
		t.Fatal("expected compile errors but got nil")
	}

	var ce *meta.CompileErrors
	if !errors.As(err, &ce) {
		t.Fatalf("expected *meta.CompileErrors; got %T: %v", err, err)
	}
	if len(ce.Errors) < 3 {
		t.Errorf("expected ≥3 errors; got %d: %v", len(ce.Errors), ce.Errors)
	}

	assertCompileError(t, err, "name")
	assertCompileError(t, err, "module")
	assertCompileError(t, err, "fields[0].field_type")
	t.Logf("accumulated %d errors as expected: %v", len(ce.Errors), err)
}

// ── JSON parse failures ───────────────────────────────────────────────────────

func TestCompile_InvalidJSON(t *testing.T) {
	_, err := meta.Compile([]byte("{not json}"))
	if err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
	// Must NOT be a *CompileErrors — it is a JSON parse error.
	var ce *meta.CompileErrors
	if errors.As(err, &ce) {
		t.Error("invalid JSON should not return *CompileErrors; got compile validation errors")
	}
	t.Logf("invalid JSON error (not CompileErrors): %v", err)
}

func TestCompile_EmptyInput(t *testing.T) {
	_, err := meta.Compile([]byte(""))
	if err == nil {
		t.Fatal("expected an error for empty input")
	}
	var ce *meta.CompileErrors
	if errors.As(err, &ce) {
		t.Error("empty input should not return *CompileErrors")
	}
	t.Logf("empty input error: %v", err)
}

// ── TableName ─────────────────────────────────────────────────────────────────

func TestTableName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"SalesOrder", "tab_sales_order"},
		{"HTTPConfig", "tab_http_config"},
		{"XMLParser", "tab_xml_parser"},
		{"User", "tab_user"},
		{"userID", "tab_user_id"},
		{"ID", "tab_id"},
		{"getHTTPSClient", "tab_get_https_client"},
		{"Simple", "tab_simple"},
		{"ABC", "tab_abc"},
		{"A", "tab_a"},
		{"", "tab_"},
		{"already_snake", "tab_already_snake"},
		{"SalesOrderItem", "tab_sales_order_item"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := meta.TableName(tc.input)
			if got != tc.want {
				t.Errorf("TableName(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
	t.Logf("all %d TableName cases passed", len(cases))
}

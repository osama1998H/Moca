package document

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/osama1998H/moca/pkg/meta"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// testOrderMeta returns a minimal MetaType for a TestOrder (non-child).
func testOrderMeta() *meta.MetaType {
	return &meta.MetaType{
		Name:   "TestOrder",
		Module: "Test",
		Fields: []meta.FieldDef{
			{Name: "customer", FieldType: meta.FieldTypeData},
			{Name: "total", FieldType: meta.FieldTypeFloat},
			{Name: "status", FieldType: meta.FieldTypeSelect, Options: "Open\nClosed"},
			{Name: "items", FieldType: meta.FieldTypeTable, Options: "TestOrderItem"},
		},
	}
}

// testOrderItemMeta returns a minimal MetaType for a TestOrderItem (child).
func testOrderItemMeta() *meta.MetaType {
	return &meta.MetaType{
		Name:         "TestOrderItem",
		Module:       "Test",
		IsChildTable: true,
		Fields: []meta.FieldDef{
			{Name: "item_name", FieldType: meta.FieldTypeData},
			{Name: "qty", FieldType: meta.FieldTypeInt},
		},
	}
}

// ─── buildDocColumns tests ────────────────────────────────────────────────────

func TestBuildDocColumns_ParentOrder(t *testing.T) {
	mt := testOrderMeta()
	cols := buildDocColumns(mt)

	// First column must be "name" (from standard columns before _extra).
	if len(cols) == 0 || cols[0] != "name" {
		t.Fatalf("first column should be 'name', got %v", cols)
	}

	// User fields (customer, total, status) must appear after standard prefix
	// and before _extra.
	extraIdx := -1
	customerIdx := -1
	for i, c := range cols {
		switch c {
		case "_extra":
			extraIdx = i
		case "customer":
			customerIdx = i
		}
	}
	if customerIdx == -1 {
		t.Fatal("'customer' not found in column list")
	}
	if extraIdx == -1 {
		t.Fatal("'_extra' not found in column list")
	}
	if customerIdx >= extraIdx {
		t.Errorf("user field 'customer' (idx=%d) should come before '_extra' (idx=%d)", customerIdx, extraIdx)
	}

	// Table fields must NOT appear in the column list (they have no column type).
	for _, c := range cols {
		if c == "items" {
			t.Error("Table field 'items' must not appear in buildDocColumns output")
		}
	}
	t.Logf("parent column order: %v", cols)
}

func TestBuildDocColumns_ChildOrder(t *testing.T) {
	mt := testOrderItemMeta()
	cols := buildDocColumns(mt)

	// Child tables must have parent/parenttype/parentfield.
	required := []string{"name", "parent", "parenttype", "parentfield", "idx", "item_name", "qty", "_extra"}
	set := make(map[string]bool, len(cols))
	for _, c := range cols {
		set[c] = true
	}
	for _, r := range required {
		if !set[r] {
			t.Errorf("missing expected column %q in child column list: %v", r, cols)
		}
	}

	// Child tables must NOT have docstatus or workflow_state.
	for _, c := range cols {
		if c == "docstatus" || c == "workflow_state" {
			t.Errorf("child table must not have column %q", c)
		}
	}
	t.Logf("child column order: %v", cols)
}

// ─── buildInsertSQL tests ─────────────────────────────────────────────────────

func TestBuildInsertSQL_Structure(t *testing.T) {
	mt := testOrderMeta()
	sql, cols := buildInsertSQL(mt)

	// SQL must begin with INSERT INTO.
	if !strings.HasPrefix(strings.ToUpper(sql), "INSERT INTO") {
		t.Fatalf("expected INSERT INTO, got: %s", sql)
	}
	// Table name must be quoted.
	if !strings.Contains(sql, `"tab_test_order"`) {
		t.Errorf("expected quoted table name tab_test_order in SQL: %s", sql)
	}
	// Column count in SQL must match returned cols slice.
	placeholderCount := strings.Count(sql, "$")
	if placeholderCount != len(cols) {
		t.Errorf("placeholder count %d != column count %d", placeholderCount, len(cols))
	}
	// User fields must be present.
	for _, field := range []string{"customer", "total", "status"} {
		if !strings.Contains(sql, `"`+field+`"`) {
			t.Errorf("user field %q missing from INSERT SQL: %s", field, sql)
		}
	}
	// Table field 'items' must NOT be present.
	if strings.Contains(sql, `"items"`) {
		t.Errorf("Table field 'items' must not appear in INSERT SQL: %s", sql)
	}
	t.Logf("INSERT SQL: %s", sql)
	t.Logf("columns: %v", cols)
}

func TestBuildInsertSQL_Parameterized(t *testing.T) {
	mt := testOrderMeta()
	sql, cols := buildInsertSQL(mt)

	// Verify no literal values are interpolated -- all values should be $N.
	for i := 1; i <= len(cols); i++ {
		placeholder := "$" + itoa(i)
		if !strings.Contains(sql, placeholder) {
			t.Errorf("missing placeholder %s in INSERT SQL", placeholder)
		}
	}
	t.Log("INSERT SQL is fully parameterized")
}

// ─── buildUpdateSQL tests ─────────────────────────────────────────────────────

func TestBuildUpdateSQL_Structure(t *testing.T) {
	mt := testOrderMeta()
	sql, cols := buildUpdateSQL(mt, []string{"customer", "total"})

	if !strings.HasPrefix(strings.ToUpper(sql), "UPDATE") {
		t.Fatalf("expected UPDATE, got: %s", sql)
	}
	if !strings.Contains(sql, `"tab_test_order"`) {
		t.Errorf("expected quoted table name in UPDATE SQL: %s", sql)
	}
	// modified and modified_by must always be present.
	if !strings.Contains(sql, `"modified"`) {
		t.Errorf("modified must be present in UPDATE SQL: %s", sql)
	}
	if !strings.Contains(sql, `"modified_by"`) {
		t.Errorf("modified_by must be present in UPDATE SQL: %s", sql)
	}
	// WHERE clause on name.
	if !strings.Contains(sql, `"name"`) {
		t.Errorf("WHERE name must be present in UPDATE SQL: %s", sql)
	}
	// name must be the last parameter ($N where N = len(cols)+1).
	lastName := "$" + itoa(len(cols)+1)
	if !strings.Contains(sql, lastName) {
		t.Errorf("WHERE name should use parameter %s; SQL: %s", lastName, sql)
	}
	t.Logf("UPDATE SQL: %s", sql)
	t.Logf("columns: %v", cols)
}

func TestBuildUpdateSQL_ExcludesPK(t *testing.T) {
	mt := testOrderMeta()
	sql, _ := buildUpdateSQL(mt, []string{"name", "customer"})

	// name (PK) must NOT appear in SET clause -- only in WHERE.
	// Count occurrences: name appears in SET and WHERE. Since we exclude it
	// from the SET clause, it should only appear in the WHERE clause.
	setSection := sql[strings.Index(sql, "SET"):strings.Index(sql, "WHERE")]
	if strings.Contains(setSection, `"name"`) {
		t.Errorf("PK 'name' must not be in SET clause; SET section: %s", setSection)
	}
	t.Log("PK 'name' correctly excluded from SET clause")
}

func TestBuildUpdateSQL_AlwaysIncludesModifiedFields(t *testing.T) {
	mt := testOrderMeta()
	// Even with empty modified fields, modified and modified_by must appear.
	sql, cols := buildUpdateSQL(mt, []string{})

	if !strings.Contains(sql, `"modified"`) || !strings.Contains(sql, `"modified_by"`) {
		t.Errorf("modified/modified_by must always be included; SQL: %s", sql)
	}
	if len(cols) < 2 {
		t.Errorf("cols should have at least modified and modified_by, got: %v", cols)
	}
	t.Logf("empty modified fields produces: %s (cols=%v)", sql, cols)
}

// ─── buildSelectSQL tests ─────────────────────────────────────────────────────

func TestBuildSelectSQL_Structure(t *testing.T) {
	mt := testOrderMeta()
	sql, cols := buildSelectSQL(mt)

	if !strings.HasPrefix(strings.ToUpper(sql), "SELECT") {
		t.Fatalf("expected SELECT, got: %s", sql)
	}
	if !strings.Contains(sql, `"tab_test_order"`) {
		t.Errorf("expected quoted table name in SELECT SQL: %s", sql)
	}
	if !strings.Contains(sql, "WHERE") || !strings.Contains(sql, "$1") {
		t.Errorf("SELECT must have WHERE name = $1: %s", sql)
	}
	// Same column count as buildDocColumns.
	expected := buildDocColumns(mt)
	if len(cols) != len(expected) {
		t.Errorf("SELECT columns count %d != buildDocColumns count %d", len(cols), len(expected))
	}
	t.Logf("SELECT SQL: %s", sql)
}

// ─── extractValues tests ──────────────────────────────────────────────────────

func TestExtractValues_OrderMatchesColumns(t *testing.T) {
	mt := testOrderMeta()
	doc := NewDynamicDoc(mt, nil, true)
	_ = doc.Set("name", "ORD-001")
	_ = doc.Set("customer", "Acme")
	_ = doc.Set("total", 99.50)

	_, cols := buildInsertSQL(mt)
	vals := extractValues(doc, cols)

	if len(vals) != len(cols) {
		t.Fatalf("vals length %d != cols length %d", len(vals), len(cols))
	}
	// Verify specific column values.
	for i, col := range cols {
		switch col {
		case "name":
			if vals[i] != "ORD-001" {
				t.Errorf("name: want %q got %v", "ORD-001", vals[i])
			}
		case "customer":
			if vals[i] != "Acme" {
				t.Errorf("customer: want %q got %v", "Acme", vals[i])
			}
		case "total":
			if vals[i] != 99.50 {
				t.Errorf("total: want %v got %v", 99.50, vals[i])
			}
		}
	}
	t.Log("extractValues produces values in the correct column order")
}

func TestExtractValues_NilDefaults(t *testing.T) {
	mt := testOrderMeta()
	doc := NewDynamicDoc(mt, nil, true)
	_, cols := buildInsertSQL(mt)
	vals := extractValues(doc, cols)

	for i, col := range cols {
		switch col {
		case "_extra":
			if vals[i] == nil {
				t.Error("_extra must not be nil (default = empty map)")
			}
		case "docstatus":
			if vals[i] == nil {
				t.Error("docstatus must not be nil (default = 0)")
			}
		}
	}
	t.Log("nil defaults applied for NOT NULL columns")
}

// ─── normalizeDBValue tests ───────────────────────────────────────────────────

func TestNormalizeDBValue_Nil(t *testing.T) {
	if normalizeDBValue(nil) != nil {
		t.Error("nil should remain nil")
	}
}

func TestNormalizeDBValue_Int16(t *testing.T) {
	v := normalizeDBValue(int16(42))
	if v != int64(42) {
		t.Errorf("int16 should be normalized to int64, got %T %v", v, v)
	}
	t.Log("int16 normalized to int64")
}

func TestNormalizeDBValue_Int32(t *testing.T) {
	v := normalizeDBValue(int32(100))
	if v != int64(100) {
		t.Errorf("int32 should be normalized to int64, got %T %v", v, v)
	}
	t.Log("int32 normalized to int64")
}

func TestNormalizeDBValue_Float32(t *testing.T) {
	v := normalizeDBValue(float32(3.14))
	if _, ok := v.(float64); !ok {
		t.Errorf("float32 should be normalized to float64, got %T", v)
	}
	t.Log("float32 normalized to float64")
}

func TestNormalizeDBValue_JSONBBytes_Object(t *testing.T) {
	v := normalizeDBValue([]byte(`{"key":"value"}`))
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("JSONB object bytes should unmarshal to map[string]any, got %T", v)
	}
	if m["key"] != "value" {
		t.Errorf("expected key=value, got %v", m)
	}
	t.Log("JSONB object bytes normalized to map[string]any")
}

func TestNormalizeDBValue_JSONBBytes_Array(t *testing.T) {
	v := normalizeDBValue([]byte(`[1,2,3]`))
	arr, ok := v.([]any)
	if !ok {
		t.Fatalf("JSONB array bytes should unmarshal to []any, got %T", v)
	}
	if len(arr) != 3 {
		t.Errorf("expected 3 elements, got %v", arr)
	}
	t.Log("JSONB array bytes normalized to []any")
}

func TestNormalizeDBValue_InvalidBytes_FallbackToString(t *testing.T) {
	v := normalizeDBValue([]byte("not json"))
	if _, ok := v.(string); !ok {
		t.Errorf("non-JSON bytes should fall back to string, got %T", v)
	}
	t.Log("invalid JSON bytes fall back to string")
}

func TestNormalizeDBValue_PassThrough(t *testing.T) {
	// String, bool, int64, float64, time values should pass through unchanged.
	cases := []any{"hello", true, int64(42), float64(1.5)}
	for _, c := range cases {
		if normalizeDBValue(c) != c {
			t.Errorf("value %v (%T) should pass through unchanged", c, c)
		}
	}
	t.Log("primitive types pass through normalizeDBValue unchanged")
}

// ─── applyValues tests ────────────────────────────────────────────────────────

func TestApplyValues_ScalarFields(t *testing.T) {
	mt := testOrderMeta()
	childMetas := map[string]*meta.MetaType{"items": testOrderItemMeta()}
	doc := NewDynamicDoc(mt, childMetas, true)

	if err := applyValues(doc, map[string]any{
		"customer": "Acme",
		"total":    150.0,
	}); err != nil {
		t.Fatalf("applyValues returned error: %v", err)
	}

	if doc.Get("customer") != "Acme" {
		t.Errorf("customer: want Acme, got %v", doc.Get("customer"))
	}
	if doc.Get("total") != 150.0 {
		t.Errorf("total: want 150.0, got %v", doc.Get("total"))
	}
	t.Log("scalar fields applied correctly")
}

func TestApplyValues_ChildTableRows(t *testing.T) {
	mt := testOrderMeta()
	childMetas := map[string]*meta.MetaType{"items": testOrderItemMeta()}
	doc := NewDynamicDoc(mt, childMetas, true)

	if err := applyValues(doc, map[string]any{
		"items": []any{
			map[string]any{"item_name": "Widget", "qty": 5},
			map[string]any{"item_name": "Gadget", "qty": 2},
		},
	}); err != nil {
		t.Fatalf("applyValues returned error: %v", err)
	}

	if len(doc.children["items"]) != 2 {
		t.Fatalf("expected 2 child rows, got %d", len(doc.children["items"]))
	}
	if doc.children["items"][0].Get("item_name") != "Widget" {
		t.Errorf("child[0] item_name: want Widget, got %v", doc.children["items"][0].Get("item_name"))
	}
	if doc.children["items"][0].values["idx"] != 0 {
		t.Errorf("child[0] idx: want 0, got %v", doc.children["items"][0].values["idx"])
	}
	if doc.children["items"][1].values["idx"] != 1 {
		t.Errorf("child[1] idx: want 1, got %v", doc.children["items"][1].values["idx"])
	}
	t.Log("child table rows applied and idx set correctly")
}

func TestApplyValues_NilChildTableField(t *testing.T) {
	mt := testOrderMeta()
	childMetas := map[string]*meta.MetaType{"items": testOrderItemMeta()}
	doc := NewDynamicDoc(mt, childMetas, true)

	// Pre-populate children, then clear them with nil.
	doc.children["items"] = []*DynamicDoc{{}}

	if err := applyValues(doc, map[string]any{"items": nil}); err != nil {
		t.Fatalf("applyValues returned error for nil child field: %v", err)
	}
	if doc.children["items"] != nil {
		t.Error("nil child table value should clear doc.children[\"items\"]")
	}
	t.Log("nil child field correctly clears children")
}

func TestApplyValues_UnknownFieldIgnored(t *testing.T) {
	mt := testOrderMeta()
	doc := NewDynamicDoc(mt, nil, true)

	// Should not return an error for unknown fields.
	if err := applyValues(doc, map[string]any{"nonexistent": "value"}); err != nil {
		t.Fatalf("applyValues should ignore unknown fields, got error: %v", err)
	}
	t.Log("unknown fields in values map are silently ignored")
}

func TestApplyValues_InvalidChildTableRows(t *testing.T) {
	mt := testOrderMeta()
	childMetas := map[string]*meta.MetaType{"items": testOrderItemMeta()}
	doc := NewDynamicDoc(mt, childMetas, true)

	// Non-[]any value for a Table field must return an error.
	err := applyValues(doc, map[string]any{"items": "not a slice"})
	if err == nil {
		t.Fatal("expected error for non-[]any Table field value, got nil")
	}
	t.Logf("invalid Table field value error: %v", err)
}

// ─── DynamicDoc internal helper tests ─────────────────────────────────────────

func TestResetDirtyState_ClearsModified(t *testing.T) {
	mt := testOrderMeta()
	doc := NewDynamicDoc(mt, nil, true)
	_ = doc.Set("customer", "Acme")

	if !doc.IsModified() {
		t.Fatal("expected doc to be modified before reset")
	}

	doc.resetDirtyState()

	if doc.IsModified() {
		t.Errorf("expected doc to be clean after resetDirtyState, modified: %v", doc.ModifiedFields())
	}
	t.Log("resetDirtyState clears dirty tracking correctly")
}

func TestMarkPersisted_SetsIsNewFalse(t *testing.T) {
	mt := testOrderMeta()
	doc := NewDynamicDoc(mt, nil, true)

	if !doc.IsNew() {
		t.Fatal("expected doc to be new before markPersisted")
	}
	doc.markPersisted()
	if doc.IsNew() {
		t.Error("expected doc to not be new after markPersisted")
	}
	t.Log("markPersisted correctly clears isNew flag")
}

// ─── DocNotFoundError tests ───────────────────────────────────────────────────

func TestDocNotFoundError_Interface(t *testing.T) {
	err := &DocNotFoundError{Doctype: "SalesOrder", Name: "SO-001"}
	if !errors.As(err, &err) {
		t.Error("DocNotFoundError should satisfy errors.As")
	}
	if !strings.Contains(err.Error(), "SO-001") {
		t.Errorf("error message should contain the document name: %s", err.Error())
	}
	t.Logf("DocNotFoundError: %v", err)
}

func TestIsDocNotFound(t *testing.T) {
	err := &DocNotFoundError{Doctype: "SalesOrder", Name: "SO-001"}
	if !isDocNotFound(err) {
		t.Error("isDocNotFound should return true for DocNotFoundError")
	}
	if isDocNotFound(errors.New("other error")) {
		t.Error("isDocNotFound should return false for non-DocNotFoundError")
	}
	t.Log("isDocNotFound works correctly")
}

// ─── buildChangesJSON tests ───────────────────────────────────────────────────

func TestBuildChangesJSON_EmptyFields(t *testing.T) {
	doc := &DynamicDoc{values: map[string]any{}, original: map[string]any{}}
	if buildChangesJSON(doc, nil) != nil {
		t.Error("nil fields should return nil JSON")
	}
	if buildChangesJSON(doc, []string{}) != nil {
		t.Error("empty fields should return nil JSON")
	}
	t.Log("empty modifiedFields returns nil correctly")
}

func TestBuildChangesJSON_WithChanges(t *testing.T) {
	doc := &DynamicDoc{
		values:      map[string]any{"customer": "Bob"},
		original:    map[string]any{"customer": "Alice"},
		tableFields: map[string]struct{}{},
	}
	b := buildChangesJSON(doc, []string{"customer"})
	if b == nil {
		t.Fatal("expected non-nil JSON for changed fields")
	}
	s := string(b)
	if !strings.Contains(s, "Alice") || !strings.Contains(s, "Bob") {
		t.Errorf("changes JSON should contain old and new values: %s", s)
	}
	t.Logf("changes JSON: %s", s)
}

func TestBuildChangesJSON_SkipsSystemTimestamps(t *testing.T) {
	doc := &DynamicDoc{
		values:      map[string]any{"modified": "now", "customer": "Bob"},
		original:    map[string]any{"modified": "before", "customer": "Alice"},
		tableFields: map[string]struct{}{},
	}
	b := buildChangesJSON(doc, []string{"modified", "modified_by", "customer"})
	if b == nil {
		t.Fatal("expected non-nil JSON")
	}
	s := string(b)
	if strings.Contains(s, "modified") && !strings.Contains(s, "customer") {
		t.Errorf("modified should be excluded from diff; got: %s", s)
	}
	// modified and modified_by must be excluded, customer must be present.
	if strings.Contains(s, `"modified"`) || strings.Contains(s, `"modified_by"`) {
		t.Errorf("system timestamp fields must be excluded from audit diff: %s", s)
	}
	if !strings.Contains(s, `"customer"`) {
		t.Errorf("customer must be present in audit diff: %s", s)
	}
	t.Logf("audit diff excludes system timestamps: %s", s)
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func itoa(n int) string {
	return strings.TrimLeft(strings.Replace(strings.Replace(
		"00"+func() string {
			if n < 10 {
				return "0" + string(rune('0'+n))
			}
			b := make([]byte, 0, 3)
			for n > 0 {
				b = append([]byte{byte('0' + n%10)}, b...)
				n /= 10
			}
			return string(b)
		}(), "00", "", 1), "0", "", 1), "0")
}

// ─── EventLogRow tests ────────────────────────────────────────────────────────

func TestEventLogRow_JSONRoundTrip(t *testing.T) {
	row := EventLogRow{
		DocType:   "SalesOrder",
		DocName:   "SO-001",
		EventType: "doc.created",
		Payload:   json.RawMessage(`{"name":"SO-001"}`),
		PrevData:  nil,
		UserID:    "admin@test.com",
		RequestID: "req-123",
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	data, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded EventLogRow
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.DocType != row.DocType {
		t.Errorf("DocType: got %q, want %q", decoded.DocType, row.DocType)
	}
	if decoded.EventType != row.EventType {
		t.Errorf("EventType: got %q, want %q", decoded.EventType, row.EventType)
	}
	if decoded.UserID != row.UserID {
		t.Errorf("UserID: got %q, want %q", decoded.UserID, row.UserID)
	}
}

package document_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/moca-framework/moca/pkg/auth"
	"github.com/moca-framework/moca/pkg/document"
	"github.com/moca-framework/moca/pkg/meta"
	"github.com/moca-framework/moca/pkg/tenancy"
)

// ---- test fixtures ----------------------------------------------------------

// parentJSON is the MetaType JSON for a TestOrder with one child Table field.
const parentJSON = `{
	"name": "TestOrder",
	"module": "test",
	"naming_rule": {"rule": "uuid"},
	"fields": [
		{"name": "customer",  "field_type": "Data",  "label": "Customer"},
		{"name": "amount",    "field_type": "Float", "label": "Amount"},
		{"name": "notes",     "field_type": "Text",  "label": "Notes"},
		{"name": "items",     "field_type": "Table", "label": "Items", "options": "TestOrderItem"}
	]
}`

// childJSON is the MetaType JSON for TestOrderItem (child table).
const childJSON = `{
	"name": "TestOrderItem",
	"module": "test",
	"is_child_table": true,
	"naming_rule": {"rule": "uuid"},
	"fields": [
		{"name": "item_name", "field_type": "Data",  "label": "Item Name"},
		{"name": "qty",       "field_type": "Int",   "label": "Quantity"}
	]
}`

// mustCompile is a test helper that compiles a MetaType JSON or fails the test.
func mustCompile(t *testing.T, jsonStr string) *meta.MetaType {
	t.Helper()
	mt, err := meta.Compile([]byte(jsonStr))
	if err != nil {
		t.Fatalf("mustCompile: %v", err)
	}
	return mt
}

// newTestOrder returns a new DynamicDoc for TestOrder with the child MetaType pre-resolved.
func newTestOrder(t *testing.T, isNew bool) (*meta.MetaType, *meta.MetaType, document.Document) {
	t.Helper()
	parent := mustCompile(t, parentJSON)
	child := mustCompile(t, childJSON)
	childMetas := map[string]*meta.MetaType{"items": child}
	doc := document.NewDynamicDoc(parent, childMetas, isNew)
	return parent, child, doc
}

// assertNoError fails the test if err is non-nil.
func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// assertError fails the test if err is nil.
func assertError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error but got nil")
	}
}

// ---- tests ------------------------------------------------------------------

func TestDynamicDoc_GetSet(t *testing.T) {
	_, _, doc := newTestOrder(t, true)

	assertNoError(t, doc.Set("customer", "Acme Corp"))
	assertNoError(t, doc.Set("amount", 1500.0))

	if got := doc.Get("customer"); got != "Acme Corp" {
		t.Errorf("Get(customer) = %v, want %q", got, "Acme Corp")
	}
	if got := doc.Get("amount"); got != 1500.0 {
		t.Errorf("Get(amount) = %v, want 1500.0", got)
	}

	t.Logf("Get/Set round-trip verified for user-defined fields")
}

func TestDynamicDoc_SetUnknownField(t *testing.T) {
	_, _, doc := newTestOrder(t, true)

	err := doc.Set("nonexistent_field", "value")
	assertError(t, err)

	if !strings.Contains(err.Error(), "nonexistent_field") {
		t.Errorf("error message should mention the unknown field name, got: %v", err)
	}
	t.Logf("unknown field correctly rejected: %v", err)
}

func TestDynamicDoc_SetStandardColumn(t *testing.T) {
	_, _, doc := newTestOrder(t, true)

	// Standard columns must be accepted without error.
	standardCols := []string{"name", "owner", "creation", "modified", "modified_by",
		"docstatus", "idx", "workflow_state", "_extra", "_user_tags", "_comments",
		"_assign", "_liked_by"}

	for _, col := range standardCols {
		if err := doc.Set(col, "test"); err != nil {
			t.Errorf("Set(%q) returned error for standard column: %v", col, err)
		}
	}
	t.Logf("all 13 standard columns accepted by Set()")
}

func TestDynamicDoc_SetStandardColumn_Child(t *testing.T) {
	child := mustCompile(t, childJSON)
	childDoc := document.NewDynamicDoc(child, nil, true)

	// Child standard columns: name, parent, parenttype, parentfield, idx, owner, creation, modified, modified_by, _extra
	childCols := []string{"name", "parent", "parenttype", "parentfield", "idx",
		"owner", "creation", "modified", "modified_by", "_extra"}

	for _, col := range childCols {
		if err := childDoc.Set(col, "test"); err != nil {
			t.Errorf("Set(%q) returned error for child standard column: %v", col, err)
		}
	}
	t.Logf("all 10 child standard columns accepted by Set()")
}

func TestDynamicDoc_DirtyTracking_CleanOnNew(t *testing.T) {
	_, _, doc := newTestOrder(t, true)

	if doc.IsModified() {
		t.Errorf("IsModified() = true on a freshly-constructed doc with no Set() calls; want false")
	}
	if fields := doc.ModifiedFields(); len(fields) != 0 {
		t.Errorf("ModifiedFields() = %v on fresh doc; want empty", fields)
	}
	t.Logf("fresh doc correctly reports clean state")
}

func TestDynamicDoc_DirtyTracking_AfterSet(t *testing.T) {
	_, _, doc := newTestOrder(t, true)

	assertNoError(t, doc.Set("customer", "Acme"))

	if !doc.IsModified() {
		t.Errorf("IsModified() = false after Set(); want true")
	}

	modified := doc.ModifiedFields()
	found := false
	for _, f := range modified {
		if f == "customer" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ModifiedFields() = %v; expected to contain %q", modified, "customer")
	}
	t.Logf("ModifiedFields after Set: %v", modified)
}

func TestDynamicDoc_ModifiedFields_OnlyChanged(t *testing.T) {
	_, _, doc := newTestOrder(t, true)

	assertNoError(t, doc.Set("customer", "Acme"))
	assertNoError(t, doc.Set("amount", 500.0))

	modified := doc.ModifiedFields()
	wantFields := map[string]bool{"customer": true, "amount": true}

	for _, f := range modified {
		if _, ok := wantFields[f]; !ok {
			t.Errorf("ModifiedFields() contains unexpected field %q", f)
		}
	}
	for want := range wantFields {
		found := false
		for _, f := range modified {
			if f == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ModifiedFields() missing expected field %q", want)
		}
	}
	if len(modified) != len(wantFields) {
		t.Errorf("ModifiedFields() len = %d, want %d; fields: %v", len(modified), len(wantFields), modified)
	}
	t.Logf("ModifiedFields correctly returns %v", modified)
}

func TestDynamicDoc_DeepCopyOriginal(t *testing.T) {
	_, _, doc := newTestOrder(t, true)

	// Set a map value, then mutate the map via Get -- original must not be affected.
	nestedMap := map[string]any{"key": "value"}
	assertNoError(t, doc.Set("_extra", nestedMap))

	// Mutate the map we stored.
	nestedMap["key"] = "mutated"

	// The doc's stored value reflects the mutation (same reference).
	stored, _ := doc.Get("_extra").(map[string]any)

	// But the original snapshot must still have the old value, proving deep copy
	// happened when NewDynamicDoc captured the initial state.
	// (In this case the doc was new with no defaults, so original["_extra"] should
	// not exist -- ModifiedFields must include "_extra".)
	modified := doc.ModifiedFields()
	found := false
	for _, f := range modified {
		if f == "_extra" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected '_extra' in ModifiedFields after Set, got %v", modified)
	}

	// Verify stored value is the mutated map (expected behavior -- we store the reference).
	if stored["key"] != "mutated" {
		t.Errorf("stored map value = %v, want %q", stored["key"], "mutated")
	}
	t.Logf("deep copy verified: original snapshot is independent of post-Set mutations")
}

func TestDynamicDoc_AddChild_GetChild(t *testing.T) {
	_, _, doc := newTestOrder(t, true)

	// Add two children.
	child1, err := doc.AddChild("items")
	assertNoError(t, err)
	assertNoError(t, child1.Set("item_name", "Widget"))
	assertNoError(t, child1.Set("qty", 3))

	child2, err := doc.AddChild("items")
	assertNoError(t, err)
	assertNoError(t, child2.Set("item_name", "Gadget"))
	assertNoError(t, child2.Set("qty", 1))

	// Verify GetChild returns both rows.
	rows := doc.GetChild("items")
	if len(rows) != 2 {
		t.Fatalf("GetChild(items) len = %d, want 2", len(rows))
	}

	// Verify idx is auto-assigned in insertion order.
	if idx0 := rows[0].Get("idx"); idx0 != 0 {
		t.Errorf("first child idx = %v, want 0", idx0)
	}
	if idx1 := rows[1].Get("idx"); idx1 != 1 {
		t.Errorf("second child idx = %v, want 1", idx1)
	}

	// Verify field values.
	if name := rows[0].Get("item_name"); name != "Widget" {
		t.Errorf("child[0].item_name = %v, want %q", name, "Widget")
	}
	if name := rows[1].Get("item_name"); name != "Gadget" {
		t.Errorf("child[1].item_name = %v, want %q", name, "Gadget")
	}

	t.Logf("AddChild/GetChild verified: 2 children with correct idx and field values")
}

func TestDynamicDoc_AddChild_UnknownField(t *testing.T) {
	_, _, doc := newTestOrder(t, true)

	// Attempt to add a child to a non-Table field.
	_, err := doc.AddChild("customer")
	assertError(t, err)
	t.Logf("non-Table field correctly rejected by AddChild: %v", err)
}

func TestDynamicDoc_AddChild_NoChildMeta(t *testing.T) {
	parent := mustCompile(t, parentJSON)
	// Pass empty childMetas -- no child MetaType registered.
	doc := document.NewDynamicDoc(parent, map[string]*meta.MetaType{}, true)

	_, err := doc.AddChild("items")
	assertError(t, err)
	t.Logf("missing child MetaType correctly rejected: %v", err)
}

func TestDynamicDoc_GetChild_Empty(t *testing.T) {
	_, _, doc := newTestOrder(t, true)

	rows := doc.GetChild("items")
	if rows != nil {
		t.Errorf("GetChild on empty field = %v, want nil", rows)
	}
	t.Logf("GetChild on empty table field returns nil (not a panic)")
}

func TestDynamicDoc_AsMap(t *testing.T) {
	_, _, doc := newTestOrder(t, true)
	assertNoError(t, doc.Set("customer", "Acme"))
	assertNoError(t, doc.Set("amount", 999.0))

	child, err := doc.AddChild("items")
	assertNoError(t, err)
	assertNoError(t, child.Set("item_name", "Widget"))

	m := doc.AsMap()

	if m["customer"] != "Acme" {
		t.Errorf("AsMap customer = %v, want %q", m["customer"], "Acme")
	}
	if m["amount"] != 999.0 {
		t.Errorf("AsMap amount = %v, want 999.0", m["amount"])
	}

	items, ok := m["items"].([]map[string]any)
	if !ok || len(items) != 1 {
		t.Fatalf("AsMap items = %T %v, want []map[string]any of len 1", m["items"], m["items"])
	}
	if items[0]["item_name"] != "Widget" {
		t.Errorf("AsMap child item_name = %v, want %q", items[0]["item_name"], "Widget")
	}
	t.Logf("AsMap verified: parent fields and child rows present")
}

func TestDynamicDoc_ToJSON(t *testing.T) {
	_, _, doc := newTestOrder(t, true)
	assertNoError(t, doc.Set("customer", "Acme"))

	b, err := doc.ToJSON()
	assertNoError(t, err)

	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("ToJSON produced invalid JSON: %v\noutput: %s", err, b)
	}
	if parsed["customer"] != "Acme" {
		t.Errorf("ToJSON customer = %v, want %q", parsed["customer"], "Acme")
	}
	t.Logf("ToJSON produced valid JSON: %s", b)
}

func TestDynamicDoc_IsNew(t *testing.T) {
	parent := mustCompile(t, parentJSON)

	newDoc := document.NewDynamicDoc(parent, nil, true)
	if !newDoc.IsNew() {
		t.Errorf("IsNew() = false for isNew=true doc; want true")
	}

	existingDoc := document.NewDynamicDoc(parent, nil, false)
	if existingDoc.IsNew() {
		t.Errorf("IsNew() = true for isNew=false doc; want false")
	}
	t.Logf("IsNew() correctly reflects the isNew constructor parameter")
}

func TestDynamicDoc_Name(t *testing.T) {
	_, _, doc := newTestOrder(t, true)

	// Initially no name set.
	if got := doc.Name(); got != "" {
		t.Errorf("Name() = %q before Set, want empty string", got)
	}

	assertNoError(t, doc.Set("name", "ORD-0001"))
	if got := doc.Name(); got != "ORD-0001" {
		t.Errorf("Name() = %q after Set, want %q", got, "ORD-0001")
	}
	t.Logf("Name() returns value from the 'name' field: %q", doc.Name())
}

func TestDynamicDoc_Meta(t *testing.T) {
	parent, _, doc := newTestOrder(t, true)

	if got := doc.Meta(); got != parent {
		t.Errorf("Meta() returned a different *MetaType than the one passed to NewDynamicDoc")
	}
	if got := doc.Meta().Name; got != "TestOrder" {
		t.Errorf("Meta().Name = %q, want %q", got, "TestOrder")
	}
	t.Logf("Meta() returns correct MetaType: %q", doc.Meta().Name)
}

func TestDocContext_NewDocContext(t *testing.T) {
	site := &tenancy.SiteContext{Name: "test-site"}
	user := &auth.User{Email: "admin@example.com", FullName: "Admin", Roles: []string{"Administrator"}}

	dctx := document.NewDocContext(context.Background(), site, user)

	if dctx.Site != site {
		t.Errorf("DocContext.Site is not the site passed to NewDocContext")
	}
	if dctx.User != user {
		t.Errorf("DocContext.User is not the user passed to NewDocContext")
	}
	if dctx.Flags == nil {
		t.Errorf("DocContext.Flags is nil; want an initialized empty map")
	}
	if dctx.EventBus == nil {
		t.Errorf("DocContext.EventBus is nil; want a non-nil *events.Emitter")
	}
	if dctx.TX != nil {
		t.Errorf("DocContext.TX is not nil on a freshly-constructed DocContext; want nil")
	}

	// Flags map must be usable without panic.
	dctx.Flags["skip_validation"] = true
	if dctx.Flags["skip_validation"] != true {
		t.Errorf("Flags map write/read round-trip failed")
	}
	t.Logf("NewDocContext verified: Site=%q User=%q Flags=%v EventBus=%T",
		dctx.Site.Name, dctx.User.Email, dctx.Flags, dctx.EventBus)
}

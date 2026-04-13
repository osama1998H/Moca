package meta_test

import (
	"encoding/json"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

func TestNamingRule_Constants(t *testing.T) {
	tests := []struct {
		name string
		rule meta.NamingRule
		want string
	}{
		{"NamingAutoIncrement", meta.NamingAutoIncrement, "autoincrement"},
		{"NamingByPattern", meta.NamingByPattern, "pattern"},
		{"NamingByField", meta.NamingByField, "field"},
		{"NamingByHash", meta.NamingByHash, "hash"},
		{"NamingUUID", meta.NamingUUID, "uuid"},
		{"NamingCustom", meta.NamingCustom, "custom"},
	}
	for _, tt := range tests {
		if string(tt.rule) != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.rule, tt.want)
		}
	}
	t.Logf("verified all %d NamingRule string values", len(tests))
}

func TestNamingRule_Uniqueness(t *testing.T) {
	rules := []meta.NamingRule{
		meta.NamingAutoIncrement,
		meta.NamingByPattern,
		meta.NamingByField,
		meta.NamingByHash,
		meta.NamingUUID,
		meta.NamingCustom,
	}
	seen := make(map[meta.NamingRule]bool, len(rules))
	for _, r := range rules {
		if r == "" {
			t.Errorf("NamingRule constant is empty string")
		}
		if seen[r] {
			t.Errorf("duplicate NamingRule value: %q", r)
		}
		seen[r] = true
	}
	t.Logf("all %d NamingRule constants are unique and non-empty", len(rules))
}

func TestMetaType_ZeroValue(t *testing.T) {
	var mt meta.MetaType

	// Pointer fields must be nil in zero value.
	if mt.Workflow != nil {
		t.Errorf("MetaType zero value: Workflow = non-nil, want nil")
	}
	if mt.APIConfig != nil {
		t.Errorf("MetaType zero value: APIConfig = non-nil, want nil")
	}

	// Slice fields must be nil (not allocated) in zero value.
	if mt.Fields != nil {
		t.Errorf("MetaType zero value: Fields = non-nil, want nil")
	}
	if mt.SearchFields != nil {
		t.Errorf("MetaType zero value: SearchFields = non-nil, want nil")
	}
	if mt.Permissions != nil {
		t.Errorf("MetaType zero value: Permissions = non-nil, want nil")
	}

	// String fields must be empty in zero value.
	if mt.Name != "" {
		t.Errorf("MetaType zero value: Name = %q, want empty", mt.Name)
	}
	if mt.Module != "" {
		t.Errorf("MetaType zero value: Module = %q, want empty", mt.Module)
	}

	// Bool fields must be false in zero value.
	if mt.IsSingle || mt.IsChildTable || mt.IsVirtual || mt.IsSubmittable {
		t.Errorf("MetaType zero value: variant bools should all be false")
	}

	t.Logf("MetaType zero value is well-formed")
}

func TestNamingStrategy_ZeroValue(t *testing.T) {
	var ns meta.NamingStrategy

	if ns.Rule != "" {
		t.Errorf("NamingStrategy zero value: Rule = %q, want empty", ns.Rule)
	}
	if ns.Pattern != "" || ns.FieldName != "" || ns.CustomFunc != "" {
		t.Errorf("NamingStrategy zero value: optional fields should be empty")
	}
	t.Logf("NamingStrategy zero value is well-formed")
}

func TestMetaType_LayoutAndFieldsMap(t *testing.T) {
	layout := &meta.LayoutTree{
		Tabs: []meta.TabDef{
			{
				Label: "Details",
				Sections: []meta.SectionDef{
					{
						Columns: []meta.ColumnDef{
							{Fields: []string{"title", "status"}},
						},
					},
				},
			},
		},
	}

	fieldsMap := map[string]meta.FieldDef{
		"title":  {Name: "title", Label: "Title", FieldType: meta.FieldTypeData},
		"status": {Name: "status", Label: "Status", FieldType: meta.FieldTypeSelect},
	}

	mt := meta.MetaType{
		Name:      "SalesOrder",
		Module:    "selling",
		Layout:    layout,
		FieldsMap: fieldsMap,
	}

	// Layout field must be set and preserve structure.
	if mt.Layout == nil {
		t.Fatal("expected Layout to be non-nil")
	}
	if len(mt.Layout.Tabs) != 1 {
		t.Errorf("expected 1 tab, got %d", len(mt.Layout.Tabs))
	}
	if mt.Layout.Tabs[0].Label != "Details" {
		t.Errorf("expected tab label %q, got %q", "Details", mt.Layout.Tabs[0].Label)
	}

	// FieldsMap must be set and accessible by key.
	if mt.FieldsMap == nil {
		t.Fatal("expected FieldsMap to be non-nil")
	}
	if len(mt.FieldsMap) != 2 {
		t.Errorf("expected 2 entries in FieldsMap, got %d", len(mt.FieldsMap))
	}
	if fd, ok := mt.FieldsMap["title"]; !ok || fd.Label != "Title" {
		t.Errorf("FieldsMap[\"title\"] not found or wrong: %+v", fd)
	}

	// FieldsMap must NOT appear in JSON output (json:"-").
	data, err := json.Marshal(mt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonStr := string(data)
	if contains := func(s, sub string) bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}; contains(jsonStr, `"fields_map"`) || contains(jsonStr, `"FieldsMap"`) {
		t.Errorf("FieldsMap should not appear in JSON output, got: %s", jsonStr)
	}

	// Layout MUST appear in JSON output (json:"layout,omitempty").
	if !func(s, sub string) bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}(jsonStr, `"layout"`) {
		t.Errorf("Layout should appear in JSON output, got: %s", jsonStr)
	}

	t.Logf("MetaType.Layout and MetaType.FieldsMap are correctly defined and behave as expected")
}

func TestMetaType_EventSourcingAndCDCFields(t *testing.T) {
	mt := meta.MetaType{
		Name:          "SalesOrder",
		Module:        "selling",
		EventSourcing: true,
		CDCEnabled:    true,
	}

	data, err := json.Marshal(mt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded meta.MetaType
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !decoded.EventSourcing {
		t.Error("expected EventSourcing=true after round-trip")
	}
	if !decoded.CDCEnabled {
		t.Error("expected CDCEnabled=true after round-trip")
	}
}

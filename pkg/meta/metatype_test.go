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

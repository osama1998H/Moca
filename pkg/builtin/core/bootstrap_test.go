package core

import (
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

func TestBootstrapCoreMeta(t *testing.T) {
	mts, err := BootstrapCoreMeta()
	if err != nil {
		t.Fatalf("BootstrapCoreMeta() error: %v", err)
	}

	if len(mts) != 11 {
		t.Fatalf("expected 11 MetaTypes, got %d", len(mts))
	}

	// DocType must be first (bootstrap ordering).
	if mts[0].Name != "DocType" {
		t.Errorf("expected first MetaType to be DocType, got %q", mts[0].Name)
	}

	// All must have valid Name and Module.
	for i, mt := range mts {
		if mt.Name == "" {
			t.Errorf("MetaType[%d] has empty Name", i)
		}
		if mt.Module == "" {
			t.Errorf("MetaType[%d] (%s) has empty Module", i, mt.Name)
		}
	}

	// Verify all expected doctypes are present.
	expected := map[string]bool{
		"DocType":              true,
		"User":                 true,
		"Role":                 true,
		"ModuleDef":            true,
		"SystemSettings":       true,
		"HasRole":              true,
		"DocField":             true,
		"DocPerm":              true,
		"SSOProvider":          true,
		"Notification":         true,
		"NotificationSettings": true,
	}
	for _, mt := range mts {
		delete(expected, mt.Name)
	}
	if len(expected) > 0 {
		missing := make([]string, 0, len(expected))
		for name := range expected {
			missing = append(missing, name)
		}
		t.Errorf("missing MetaTypes: %v", missing)
	}
}

func TestBootstrapCoreMetaFieldsNotEmpty(t *testing.T) {
	mts, err := BootstrapCoreMeta()
	if err != nil {
		t.Fatalf("BootstrapCoreMeta() error: %v", err)
	}

	for _, mt := range mts {
		if len(mt.Fields) == 0 {
			t.Errorf("MetaType %q has no fields", mt.Name)
		}
	}
}

func TestDocTypeMetaTypeMatchesJSON(t *testing.T) {
	// Compile the doctype.json file to verify it stays in sync with the
	// hard-coded Go definition.
	data, err := doctypeFS.ReadFile("modules/core/doctypes/doctype/doctype.json")
	if err != nil {
		t.Fatalf("read doctype.json: %v", err)
	}

	jsonMT, err := meta.Compile(data)
	if err != nil {
		t.Fatalf("compile doctype.json: %v", err)
	}

	goMT := buildDocTypeMetaType()

	// Compare key properties.
	if jsonMT.Name != goMT.Name {
		t.Errorf("Name mismatch: JSON=%q Go=%q", jsonMT.Name, goMT.Name)
	}
	if jsonMT.Module != goMT.Module {
		t.Errorf("Module mismatch: JSON=%q Go=%q", jsonMT.Module, goMT.Module)
	}
	if len(jsonMT.Fields) != len(goMT.Fields) {
		t.Errorf("Fields count mismatch: JSON=%d Go=%d", len(jsonMT.Fields), len(goMT.Fields))
	}

	// Compare field names and types.
	jsonFields := make(map[string]meta.FieldType)
	for _, f := range jsonMT.Fields {
		jsonFields[f.Name] = f.FieldType
	}
	for _, f := range goMT.Fields {
		jft, ok := jsonFields[f.Name]
		if !ok {
			t.Errorf("Go field %q not found in JSON definition", f.Name)
			continue
		}
		if jft != f.FieldType {
			t.Errorf("field %q type mismatch: JSON=%q Go=%q", f.Name, jft, f.FieldType)
		}
	}
}

func TestSystemSettingsIsSingle(t *testing.T) {
	mts, err := BootstrapCoreMeta()
	if err != nil {
		t.Fatalf("BootstrapCoreMeta() error: %v", err)
	}

	for _, mt := range mts {
		if mt.Name == "SystemSettings" {
			if !mt.IsSingle {
				t.Error("SystemSettings should have IsSingle=true")
			}
			return
		}
	}
	t.Error("SystemSettings not found in bootstrap result")
}

func TestChildTablesMarkedCorrectly(t *testing.T) {
	mts, err := BootstrapCoreMeta()
	if err != nil {
		t.Fatalf("BootstrapCoreMeta() error: %v", err)
	}

	childTypes := map[string]bool{
		"HasRole":  true,
		"DocField": true,
		"DocPerm":  true,
	}

	for _, mt := range mts {
		if childTypes[mt.Name] {
			if !mt.IsChildTable {
				t.Errorf("%s should have IsChildTable=true", mt.Name)
			}
		}
	}
}

func TestUserNamingRule(t *testing.T) {
	mts, err := BootstrapCoreMeta()
	if err != nil {
		t.Fatalf("BootstrapCoreMeta() error: %v", err)
	}

	for _, mt := range mts {
		if mt.Name == "User" {
			if mt.NamingRule.Rule != meta.NamingByField {
				t.Errorf("User naming rule: got %q, want %q", mt.NamingRule.Rule, meta.NamingByField)
			}
			if mt.NamingRule.FieldName != "email" {
				t.Errorf("User naming field: got %q, want %q", mt.NamingRule.FieldName, "email")
			}
			return
		}
	}
	t.Error("User not found in bootstrap result")
}

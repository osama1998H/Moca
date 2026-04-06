package meta_test

import (
	"sync"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

func TestRegisterCustomFieldType_IsValid(t *testing.T) {
	t.Cleanup(meta.ResetCustomFieldTypes)

	ft := meta.FieldType("TreeSelect")
	if ft.IsValid() {
		t.Fatal("TreeSelect should not be valid before registration")
	}

	meta.RegisterCustomFieldType(ft)

	if !ft.IsValid() {
		t.Error("TreeSelect should be valid after registration")
	}
}

func TestRegisterCustomFieldType_IsCustom(t *testing.T) {
	t.Cleanup(meta.ResetCustomFieldTypes)

	ft := meta.FieldType("KanbanStatus")
	meta.RegisterCustomFieldType(ft)

	if !ft.IsCustom() {
		t.Error("KanbanStatus should be custom after registration")
	}
}

func TestBuiltinFieldType_IsCustomFalse(t *testing.T) {
	if meta.FieldTypeData.IsCustom() {
		t.Error("built-in FieldTypeData should not be custom")
	}
	if meta.FieldTypeCheck.IsCustom() {
		t.Error("built-in FieldTypeCheck should not be custom")
	}
	if meta.FieldTypeSectionBreak.IsCustom() {
		t.Error("built-in FieldTypeSectionBreak should not be custom")
	}
}

func TestCustomFieldType_IsStorable(t *testing.T) {
	t.Cleanup(meta.ResetCustomFieldTypes)

	ft := meta.FieldType("TreeSelect")
	meta.RegisterCustomFieldType(ft)

	if !ft.IsStorable() {
		t.Error("custom field type TreeSelect should be storable (not a layout type)")
	}
}

func TestCustomFieldType_ColumnType(t *testing.T) {
	t.Cleanup(meta.ResetCustomFieldTypes)

	ft := meta.FieldType("TreeSelect")
	meta.RegisterCustomFieldType(ft)

	got := meta.ColumnType(ft)
	if got != "TEXT" {
		t.Errorf("ColumnType(%q) = %q; want %q", ft, got, "TEXT")
	}
}

func TestUnregisteredType_StillInvalid(t *testing.T) {
	// Verify that unregistered types remain invalid (no regression).
	invalid := []meta.FieldType{"FakeType", "NotReal", ""}
	for _, ft := range invalid {
		if ft.IsValid() {
			t.Errorf("unregistered FieldType %q should not be valid", ft)
		}
		if ft.IsCustom() {
			t.Errorf("unregistered FieldType %q should not be custom", ft)
		}
		if ft.IsStorable() {
			t.Errorf("unregistered FieldType %q should not be storable", ft)
		}
		if meta.ColumnType(ft) != "" {
			t.Errorf("ColumnType(%q) should return empty for unregistered type", ft)
		}
	}
}

func TestCustomFieldType_ConcurrentRegistration(t *testing.T) {
	t.Cleanup(meta.ResetCustomFieldTypes)

	const count = 50
	var wg sync.WaitGroup
	wg.Add(count)

	for i := range count {
		go func(n int) {
			defer wg.Done()
			ft := meta.FieldType("Custom" + string(rune('A'+n)))
			meta.RegisterCustomFieldType(ft)
			// Read while others write.
			_ = ft.IsValid()
			_ = ft.IsCustom()
			_ = ft.IsStorable()
			_ = meta.ColumnType(ft)
		}(i)
	}

	wg.Wait()
}

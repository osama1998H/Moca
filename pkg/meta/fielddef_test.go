package meta_test

import (
	"testing"

	"github.com/moca-framework/moca/pkg/meta"
)

// storageFieldTypes lists all 29 FieldType values that map to database columns.
var storageFieldTypes = []meta.FieldType{
	meta.FieldTypeData,
	meta.FieldTypeText,
	meta.FieldTypeLongText,
	meta.FieldTypeMarkdown,
	meta.FieldTypeCode,
	meta.FieldTypeInt,
	meta.FieldTypeFloat,
	meta.FieldTypeCurrency,
	meta.FieldTypePercent,
	meta.FieldTypeDate,
	meta.FieldTypeDatetime,
	meta.FieldTypeTime,
	meta.FieldTypeDuration,
	meta.FieldTypeSelect,
	meta.FieldTypeLink,
	meta.FieldTypeDynamicLink,
	meta.FieldTypeTable,
	meta.FieldTypeTableMultiSelect,
	meta.FieldTypeAttach,
	meta.FieldTypeAttachImage,
	meta.FieldTypeCheck,
	meta.FieldTypeColor,
	meta.FieldTypeGeolocation,
	meta.FieldTypeJSON,
	meta.FieldTypePassword,
	meta.FieldTypeRating,
	meta.FieldTypeSignature,
	meta.FieldTypeBarcode,
	meta.FieldTypeHTMLEditor,
}

// layoutFieldTypes lists all 6 FieldType values that are layout-only (no column).
var layoutFieldTypes = []meta.FieldType{
	meta.FieldTypeSectionBreak,
	meta.FieldTypeColumnBreak,
	meta.FieldTypeTabBreak,
	meta.FieldTypeHTML,
	meta.FieldTypeButton,
	meta.FieldTypeHeading,
}

func TestFieldType_IsStorable_StorageTypes(t *testing.T) {
	for _, ft := range storageFieldTypes {
		if !ft.IsStorable() {
			t.Errorf("FieldType %q: IsStorable() = false, want true", ft)
		}
	}
	t.Logf("verified IsStorable() = true for all %d storage types", len(storageFieldTypes))
}

func TestFieldType_IsStorable_LayoutTypes(t *testing.T) {
	for _, ft := range layoutFieldTypes {
		if ft.IsStorable() {
			t.Errorf("FieldType %q: IsStorable() = true, want false", ft)
		}
	}
	t.Logf("verified IsStorable() = false for all %d layout-only types", len(layoutFieldTypes))
}

func TestFieldType_IsStorable_InvalidType(t *testing.T) {
	invalid := []meta.FieldType{"", "FakeType", "data", "TEXT"}
	for _, ft := range invalid {
		if ft.IsStorable() {
			t.Errorf("FieldType %q: IsStorable() = true, want false for invalid type", ft)
		}
	}
	t.Logf("verified IsStorable() = false for invalid/empty types")
}

func TestFieldType_IsValid_AllTypes(t *testing.T) {
	allTypes := append(storageFieldTypes, layoutFieldTypes...)
	for _, ft := range allTypes {
		if !ft.IsValid() {
			t.Errorf("FieldType %q: IsValid() = false, want true", ft)
		}
	}
	t.Logf("verified IsValid() = true for all %d recognized types", len(allTypes))
}

func TestFieldType_IsValid_InvalidTypes(t *testing.T) {
	invalid := []meta.FieldType{"", "FakeType", "data", "TEXT", "BOOLEAN"}
	for _, ft := range invalid {
		if ft.IsValid() {
			t.Errorf("FieldType %q: IsValid() = true, want false for unrecognized type", ft)
		}
	}
	t.Logf("verified IsValid() = false for unrecognized types")
}

func TestValidFieldTypes_Length(t *testing.T) {
	const wantCount = 35 // 29 storage + 6 layout
	got := len(meta.ValidFieldTypes)
	if got != wantCount {
		t.Errorf("len(ValidFieldTypes) = %d, want %d", got, wantCount)
	}
	t.Logf("ValidFieldTypes contains %d entries", got)
}

func TestValidFieldTypes_NoDuplicates(t *testing.T) {
	// Verify that storageFieldTypes and layoutFieldTypes together cover
	// exactly the entries in ValidFieldTypes with no overlaps.
	allTypes := append(storageFieldTypes, layoutFieldTypes...)
	if len(allTypes) != len(meta.ValidFieldTypes) {
		t.Errorf("test type list length %d != ValidFieldTypes length %d", len(allTypes), len(meta.ValidFieldTypes))
	}
	seen := make(map[meta.FieldType]bool, len(allTypes))
	for _, ft := range allTypes {
		if seen[ft] {
			t.Errorf("duplicate FieldType in test list: %q", ft)
		}
		seen[ft] = true
	}
	t.Logf("no duplicate FieldType constants detected")
}

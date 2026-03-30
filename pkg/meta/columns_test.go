package meta_test

import (
	"testing"

	"github.com/moca-framework/moca/pkg/meta"
)

// TestColumnType_AllStorableTypes verifies that every storage FieldType maps to
// the expected PostgreSQL column type string.
func TestColumnType_AllStorableTypes(t *testing.T) {
	cases := []struct {
		ft   meta.FieldType
		want string
	}{
		// TEXT group
		{meta.FieldTypeData, "TEXT"},
		{meta.FieldTypeText, "TEXT"},
		{meta.FieldTypeLongText, "TEXT"},
		{meta.FieldTypeMarkdown, "TEXT"},
		{meta.FieldTypeCode, "TEXT"},
		{meta.FieldTypeHTMLEditor, "TEXT"},
		{meta.FieldTypeSelect, "TEXT"},
		{meta.FieldTypeColor, "TEXT"},
		{meta.FieldTypeBarcode, "TEXT"},
		{meta.FieldTypeSignature, "TEXT"},
		{meta.FieldTypePassword, "TEXT"},
		{meta.FieldTypeLink, "TEXT"},
		{meta.FieldTypeDynamicLink, "TEXT"},
		{meta.FieldTypeAttach, "TEXT"},
		{meta.FieldTypeAttachImage, "TEXT"},
		// Numeric types
		{meta.FieldTypeInt, "INTEGER"},
		{meta.FieldTypeFloat, "NUMERIC(18,6)"},
		{meta.FieldTypeCurrency, "NUMERIC(18,6)"},
		{meta.FieldTypePercent, "NUMERIC(18,6)"},
		{meta.FieldTypeRating, "NUMERIC(18,6)"},
		{meta.FieldTypeDuration, "NUMERIC(18,6)"},
		// Temporal types
		{meta.FieldTypeDate, "DATE"},
		{meta.FieldTypeDatetime, "TIMESTAMPTZ"},
		{meta.FieldTypeTime, "TIME"},
		// Boolean
		{meta.FieldTypeCheck, "BOOLEAN"},
		// JSONB
		{meta.FieldTypeJSON, "JSONB"},
		{meta.FieldTypeGeolocation, "JSONB"},
		// Child-table types (no column)
		{meta.FieldTypeTable, ""},
		{meta.FieldTypeTableMultiSelect, ""},
	}

	for _, tc := range cases {
		t.Run(string(tc.ft), func(t *testing.T) {
			got := meta.ColumnType(tc.ft)
			if got != tc.want {
				t.Errorf("ColumnType(%q) = %q; want %q", tc.ft, got, tc.want)
			}
		})
	}
	t.Logf("verified %d storable field type mappings", len(cases))
}

// TestColumnType_LayoutTypes verifies that all 6 layout-only types return "".
func TestColumnType_LayoutTypes(t *testing.T) {
	layoutTypes := []meta.FieldType{
		meta.FieldTypeSectionBreak,
		meta.FieldTypeColumnBreak,
		meta.FieldTypeTabBreak,
		meta.FieldTypeHTML,
		meta.FieldTypeButton,
		meta.FieldTypeHeading,
	}

	for _, ft := range layoutTypes {
		t.Run(string(ft), func(t *testing.T) {
			got := meta.ColumnType(ft)
			if got != "" {
				t.Errorf("ColumnType(%q) = %q; want empty string (layout type)", ft, got)
			}
		})
	}
}

// TestColumnType_InvalidTypes verifies that unknown and empty FieldType values
// return an empty string rather than panicking or returning a default.
func TestColumnType_InvalidTypes(t *testing.T) {
	cases := []meta.FieldType{
		"",
		"FakeType",
		"data",     // wrong case
		"INTEGER",  // SQL type, not a FieldType name
		"NotAType",
	}

	for _, ft := range cases {
		t.Run(string(ft)+"_invalid", func(t *testing.T) {
			got := meta.ColumnType(ft)
			if got != "" {
				t.Errorf("ColumnType(%q) = %q; want empty string for invalid type", ft, got)
			}
		})
	}
}

// TestStandardColumns_Count verifies that StandardColumns returns exactly 13 entries.
func TestStandardColumns_Count(t *testing.T) {
	cols := meta.StandardColumns()
	if len(cols) != 13 {
		t.Errorf("StandardColumns() returned %d columns; want 13", len(cols))
	}

	// Verify key columns are present.
	names := make(map[string]bool, len(cols))
	for _, c := range cols {
		names[c.Name] = true
	}

	required := []string{"name", "owner", "creation", "modified", "modified_by",
		"docstatus", "idx", "workflow_state", "_extra", "_user_tags", "_comments",
		"_assign", "_liked_by"}
	for _, r := range required {
		if !names[r] {
			t.Errorf("StandardColumns() missing column %q", r)
		}
	}
}

// TestStandardColumns_NameIsPrimaryKey verifies that the "name" column has
// PRIMARY KEY in its DDL definition.
func TestStandardColumns_NameIsPrimaryKey(t *testing.T) {
	cols := meta.StandardColumns()
	for _, c := range cols {
		if c.Name == "name" {
			if c.DDL != "TEXT PRIMARY KEY" {
				t.Errorf("name column DDL = %q; want %q", c.DDL, "TEXT PRIMARY KEY")
			}
			return
		}
	}
	t.Error("StandardColumns() did not contain 'name' column")
}

// TestChildStandardColumns_ContainsParentFields verifies that child standard
// columns include parent, parenttype, and parentfield.
func TestChildStandardColumns_ContainsParentFields(t *testing.T) {
	cols := meta.ChildStandardColumns()
	names := make(map[string]bool, len(cols))
	for _, c := range cols {
		names[c.Name] = true
	}

	required := []string{"parent", "parenttype", "parentfield"}
	for _, r := range required {
		if !names[r] {
			t.Errorf("ChildStandardColumns() missing column %q", r)
		}
	}
}

// TestChildStandardColumns_OmitsRegularOnlyColumns verifies that child tables
// do not include columns that are exclusive to regular document tables.
func TestChildStandardColumns_OmitsRegularOnlyColumns(t *testing.T) {
	cols := meta.ChildStandardColumns()
	names := make(map[string]bool, len(cols))
	for _, c := range cols {
		names[c.Name] = true
	}

	excluded := []string{"docstatus", "workflow_state", "_user_tags", "_comments", "_assign", "_liked_by"}
	for _, ex := range excluded {
		if names[ex] {
			t.Errorf("ChildStandardColumns() should not contain %q (regular-table-only column)", ex)
		}
	}
}

// TestChildStandardColumns_Count verifies that ChildStandardColumns returns exactly 10 entries.
func TestChildStandardColumns_Count(t *testing.T) {
	cols := meta.ChildStandardColumns()
	if len(cols) != 10 {
		t.Errorf("ChildStandardColumns() returned %d columns; want 10", len(cols))
	}
}

package meta

import (
	"encoding/json"
	"testing"
)

// TestLayoutTree_JSON_RoundTrip verifies that a LayoutTree with tabs, sections,
// and columns survives JSON marshal/unmarshal without data loss.
func TestLayoutTree_JSON_RoundTrip(t *testing.T) {
	original := &LayoutTree{
		Tabs: []TabDef{
			{
				Label: "Details",
				Sections: []SectionDef{
					{
						Label:       "Basic Info",
						Collapsible: true,
						Columns: []ColumnDef{
							{Width: 6, Fields: []string{"first_name", "last_name"}},
							{Width: 6, Fields: []string{"email"}},
						},
					},
					{
						Label:              "Advanced",
						Collapsible:        true,
						CollapsedByDefault: true,
						Columns: []ColumnDef{
							{Fields: []string{"notes"}},
						},
					},
				},
			},
			{
				Label: "Settings",
				Sections: []SectionDef{
					{
						Columns: []ColumnDef{
							{Fields: []string{"enabled", "role"}},
						},
					},
				},
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored LayoutTree
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify structure matches
	if len(restored.Tabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(restored.Tabs))
	}

	tab0 := restored.Tabs[0]
	if tab0.Label != "Details" {
		t.Errorf("tab[0].Label = %q, want %q", tab0.Label, "Details")
	}
	if len(tab0.Sections) != 2 {
		t.Fatalf("tab[0]: expected 2 sections, got %d", len(tab0.Sections))
	}

	sec0 := tab0.Sections[0]
	if sec0.Label != "Basic Info" {
		t.Errorf("sec[0].Label = %q, want %q", sec0.Label, "Basic Info")
	}
	if !sec0.Collapsible {
		t.Error("sec[0].Collapsible = false, want true")
	}
	if sec0.CollapsedByDefault {
		t.Error("sec[0].CollapsedByDefault = true, want false")
	}
	if len(sec0.Columns) != 2 {
		t.Fatalf("sec[0]: expected 2 columns, got %d", len(sec0.Columns))
	}
	if sec0.Columns[0].Width != 6 {
		t.Errorf("sec[0].col[0].Width = %d, want 6", sec0.Columns[0].Width)
	}
	assertStringSlice(t, "sec[0].col[0].Fields", sec0.Columns[0].Fields, []string{"first_name", "last_name"})
	assertStringSlice(t, "sec[0].col[1].Fields", sec0.Columns[1].Fields, []string{"email"})

	sec1 := tab0.Sections[1]
	if !sec1.Collapsible {
		t.Error("sec[1].Collapsible = false, want true")
	}
	if !sec1.CollapsedByDefault {
		t.Error("sec[1].CollapsedByDefault = false, want true")
	}

	tab1 := restored.Tabs[1]
	if tab1.Label != "Settings" {
		t.Errorf("tab[1].Label = %q, want %q", tab1.Label, "Settings")
	}
	assertStringSlice(t, "tab[1].sec[0].col[0].Fields",
		tab1.Sections[0].Columns[0].Fields, []string{"enabled", "role"})
}

// TestFlatToTree_NoBreaks verifies that a flat field list with no break types
// produces a default layout: one "Details" tab, one section, one column.
func TestFlatToTree_NoBreaks(t *testing.T) {
	fields := []FieldDef{
		{Name: "title", FieldType: FieldTypeData, Label: "Title"},
		{Name: "description", FieldType: FieldTypeText, Label: "Description"},
		{Name: "status", FieldType: FieldTypeSelect, Label: "Status"},
	}

	tree, fieldsMap := FlatToTree(fields)

	if len(tree.Tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tree.Tabs))
	}
	if tree.Tabs[0].Label != "Details" {
		t.Errorf("tab label = %q, want %q", tree.Tabs[0].Label, "Details")
	}
	if len(tree.Tabs[0].Sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(tree.Tabs[0].Sections))
	}
	if len(tree.Tabs[0].Sections[0].Columns) != 1 {
		t.Fatalf("expected 1 column, got %d", len(tree.Tabs[0].Sections[0].Columns))
	}

	col := tree.Tabs[0].Sections[0].Columns[0]
	assertStringSlice(t, "column fields", col.Fields, []string{"title", "description", "status"})

	// All storable fields should be in the map
	if len(fieldsMap) != 3 {
		t.Fatalf("fieldsMap: expected 3 entries, got %d", len(fieldsMap))
	}
	for _, name := range []string{"title", "description", "status"} {
		if _, ok := fieldsMap[name]; !ok {
			t.Errorf("fieldsMap missing field %q", name)
		}
	}
}

// TestFlatToTree_WithBreaks verifies that flat fields WITH TabBreak,
// SectionBreak, and ColumnBreak delimiters produce a properly nested tree.
func TestFlatToTree_WithBreaks(t *testing.T) {
	fields := []FieldDef{
		// First tab (implicit "Details" since no tab break before first field)
		{Name: "title", FieldType: FieldTypeData, Label: "Title"},
		{Name: "sb_basic", FieldType: FieldTypeSectionBreak, Label: "Basic Info", LayoutHint: LayoutHint{
			Label:       "Basic Information",
			Collapsible: true,
		}},
		{Name: "first_name", FieldType: FieldTypeData, Label: "First Name"},
		{Name: "cb_1", FieldType: FieldTypeColumnBreak},
		{Name: "last_name", FieldType: FieldTypeData, Label: "Last Name"},
		// Second tab
		{Name: "tb_settings", FieldType: FieldTypeTabBreak, Label: "Settings", LayoutHint: LayoutHint{
			Label: "Settings",
		}},
		{Name: "enabled", FieldType: FieldTypeCheck, Label: "Enabled"},
		{Name: "sb_advanced", FieldType: FieldTypeSectionBreak, LayoutHint: LayoutHint{
			Label:              "Advanced",
			Collapsible:        true,
			CollapsedByDefault: true,
		}},
		{Name: "role", FieldType: FieldTypeLink, Label: "Role", Options: "Role"},
	}

	tree, fieldsMap := FlatToTree(fields)

	// Should have 2 tabs
	if len(tree.Tabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(tree.Tabs))
	}

	// Tab 0: "Details" (implicit)
	tab0 := tree.Tabs[0]
	if tab0.Label != "Details" {
		t.Errorf("tab[0].Label = %q, want %q", tab0.Label, "Details")
	}

	// Tab 0 should have 2 sections:
	// Section 0: default (contains "title")
	// Section 1: "Basic Information" (contains "first_name", "last_name" in 2 columns)
	if len(tab0.Sections) < 2 {
		t.Fatalf("tab[0]: expected at least 2 sections, got %d", len(tab0.Sections))
	}

	// First section: default, one column with "title"
	assertStringSlice(t, "tab0.sec0.col0", tab0.Sections[0].Columns[0].Fields, []string{"title"})

	// Second section: "Basic Information", collapsible, two columns
	sec1 := tab0.Sections[1]
	if sec1.Label != "Basic Information" {
		t.Errorf("tab0.sec1.Label = %q, want %q", sec1.Label, "Basic Information")
	}
	if !sec1.Collapsible {
		t.Error("tab0.sec1.Collapsible = false, want true")
	}
	if len(sec1.Columns) != 2 {
		t.Fatalf("tab0.sec1: expected 2 columns, got %d", len(sec1.Columns))
	}
	assertStringSlice(t, "tab0.sec1.col0", sec1.Columns[0].Fields, []string{"first_name"})
	assertStringSlice(t, "tab0.sec1.col1", sec1.Columns[1].Fields, []string{"last_name"})

	// Tab 1: "Settings"
	tab1 := tree.Tabs[1]
	if tab1.Label != "Settings" {
		t.Errorf("tab[1].Label = %q, want %q", tab1.Label, "Settings")
	}
	if len(tab1.Sections) < 2 {
		t.Fatalf("tab[1]: expected at least 2 sections, got %d", len(tab1.Sections))
	}

	// First section: default, one column with "enabled"
	assertStringSlice(t, "tab1.sec0.col0", tab1.Sections[0].Columns[0].Fields, []string{"enabled"})

	// Second section: "Advanced", collapsible, collapsed by default
	advSec := tab1.Sections[1]
	if advSec.Label != "Advanced" {
		t.Errorf("tab1.sec1.Label = %q, want %q", advSec.Label, "Advanced")
	}
	if !advSec.Collapsible {
		t.Error("tab1.sec1.Collapsible = false, want true")
	}
	if !advSec.CollapsedByDefault {
		t.Error("tab1.sec1.CollapsedByDefault = false, want true")
	}
	assertStringSlice(t, "tab1.sec1.col0", advSec.Columns[0].Fields, []string{"role"})

	// fieldsMap should contain only storable/non-break fields
	expectedFields := []string{"title", "first_name", "last_name", "enabled", "role"}
	if len(fieldsMap) != len(expectedFields) {
		t.Fatalf("fieldsMap: expected %d entries, got %d", len(expectedFields), len(fieldsMap))
	}
	for _, name := range expectedFields {
		if _, ok := fieldsMap[name]; !ok {
			t.Errorf("fieldsMap missing field %q", name)
		}
	}

	// Break fields should NOT be in the map
	breakFields := []string{"sb_basic", "cb_1", "tb_settings", "sb_advanced"}
	for _, name := range breakFields {
		if _, ok := fieldsMap[name]; ok {
			t.Errorf("fieldsMap should not contain break field %q", name)
		}
	}
}

// TestExtractFieldsOrdered verifies that walking a LayoutTree produces the
// flat field list in correct layout order.
func TestExtractFieldsOrdered(t *testing.T) {
	tree := &LayoutTree{
		Tabs: []TabDef{
			{
				Label: "Details",
				Sections: []SectionDef{
					{
						Columns: []ColumnDef{
							{Fields: []string{"title"}},
						},
					},
					{
						Label: "Names",
						Columns: []ColumnDef{
							{Fields: []string{"first_name"}},
							{Fields: []string{"last_name"}},
						},
					},
				},
			},
			{
				Label: "Settings",
				Sections: []SectionDef{
					{
						Columns: []ColumnDef{
							{Fields: []string{"enabled", "role"}},
						},
					},
				},
			},
		},
	}

	fieldsMap := map[string]FieldDef{
		"title":      {Name: "title", FieldType: FieldTypeData, Label: "Title"},
		"first_name": {Name: "first_name", FieldType: FieldTypeData, Label: "First Name"},
		"last_name":  {Name: "last_name", FieldType: FieldTypeData, Label: "Last Name"},
		"enabled":    {Name: "enabled", FieldType: FieldTypeCheck, Label: "Enabled"},
		"role":       {Name: "role", FieldType: FieldTypeLink, Label: "Role"},
	}

	result := ExtractFieldsOrdered(tree, fieldsMap)

	expected := []string{"title", "first_name", "last_name", "enabled", "role"}
	if len(result) != len(expected) {
		t.Fatalf("expected %d fields, got %d", len(expected), len(result))
	}
	for i, name := range expected {
		if result[i].Name != name {
			t.Errorf("result[%d].Name = %q, want %q", i, result[i].Name, name)
		}
	}
}

// TestExtractFieldsOrdered_SkipsMissing verifies that fields in the tree but
// missing from the fieldsMap are silently skipped.
func TestExtractFieldsOrdered_SkipsMissing(t *testing.T) {
	tree := &LayoutTree{
		Tabs: []TabDef{
			{
				Label: "Details",
				Sections: []SectionDef{
					{
						Columns: []ColumnDef{
							{Fields: []string{"title", "missing_field", "status"}},
						},
					},
				},
			},
		},
	}

	fieldsMap := map[string]FieldDef{
		"title":  {Name: "title", FieldType: FieldTypeData, Label: "Title"},
		"status": {Name: "status", FieldType: FieldTypeSelect, Label: "Status"},
	}

	result := ExtractFieldsOrdered(tree, fieldsMap)

	if len(result) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(result))
	}
	if result[0].Name != "title" {
		t.Errorf("result[0].Name = %q, want %q", result[0].Name, "title")
	}
	if result[1].Name != "status" {
		t.Errorf("result[1].Name = %q, want %q", result[1].Name, "status")
	}
}

// TestFlatToTree_RoundTrip verifies that flat->tree->extract preserves field order.
func TestFlatToTree_RoundTrip(t *testing.T) {
	original := []FieldDef{
		{Name: "title", FieldType: FieldTypeData, Label: "Title"},
		{Name: "sb_basic", FieldType: FieldTypeSectionBreak, Label: "Basic"},
		{Name: "first_name", FieldType: FieldTypeData, Label: "First Name"},
		{Name: "cb_1", FieldType: FieldTypeColumnBreak},
		{Name: "last_name", FieldType: FieldTypeData, Label: "Last Name"},
		{Name: "tb_settings", FieldType: FieldTypeTabBreak, Label: "Settings"},
		{Name: "enabled", FieldType: FieldTypeCheck, Label: "Enabled"},
		{Name: "role", FieldType: FieldTypeLink, Label: "Role", Options: "Role"},
	}

	tree, fieldsMap := FlatToTree(original)
	extracted := ExtractFieldsOrdered(tree, fieldsMap)

	// Should get back only the non-break fields, in order
	expectedNames := []string{"title", "first_name", "last_name", "enabled", "role"}
	if len(extracted) != len(expectedNames) {
		t.Fatalf("expected %d fields, got %d", len(expectedNames), len(extracted))
	}
	for i, name := range expectedNames {
		if extracted[i].Name != name {
			t.Errorf("extracted[%d].Name = %q, want %q", i, extracted[i].Name, name)
		}
	}
}

// TestFlatToTree_EmptyFields verifies that empty input produces a single empty
// "Details" tab.
func TestFlatToTree_EmptyFields(t *testing.T) {
	tree, fieldsMap := FlatToTree(nil)

	if len(tree.Tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tree.Tabs))
	}
	if tree.Tabs[0].Label != "Details" {
		t.Errorf("tab label = %q, want %q", tree.Tabs[0].Label, "Details")
	}
	if len(fieldsMap) != 0 {
		t.Errorf("fieldsMap: expected 0 entries, got %d", len(fieldsMap))
	}
}

// TestFlatToTree_HTMLButtonHeadingInColumns verifies that non-break layout types
// (HTML, Button, Heading) go into columns like data fields.
func TestFlatToTree_HTMLButtonHeadingInColumns(t *testing.T) {
	fields := []FieldDef{
		{Name: "heading_1", FieldType: FieldTypeHeading, Label: "My Heading"},
		{Name: "title", FieldType: FieldTypeData, Label: "Title"},
		{Name: "html_1", FieldType: FieldTypeHTML, Label: "Info Block"},
		{Name: "submit_btn", FieldType: FieldTypeButton, Label: "Submit"},
	}

	tree, fieldsMap := FlatToTree(fields)

	col := tree.Tabs[0].Sections[0].Columns[0]
	assertStringSlice(t, "column fields", col.Fields, []string{"heading_1", "title", "html_1", "submit_btn"})

	// All non-break layout types should be in the fieldsMap
	if len(fieldsMap) != 4 {
		t.Fatalf("fieldsMap: expected 4 entries, got %d", len(fieldsMap))
	}
}

// TestDefaultLayout verifies that DefaultLayout wraps fields in a single
// Details tab with one section and one column.
func TestDefaultLayout(t *testing.T) {
	fields := []FieldDef{
		{Name: "a", FieldType: FieldTypeData, Label: "A"},
		{Name: "b", FieldType: FieldTypeInt, Label: "B"},
	}

	tree, fieldsMap := DefaultLayout(fields)

	if len(tree.Tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tree.Tabs))
	}
	if tree.Tabs[0].Label != "Details" {
		t.Errorf("tab label = %q, want %q", tree.Tabs[0].Label, "Details")
	}
	if len(tree.Tabs[0].Sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(tree.Tabs[0].Sections))
	}
	if len(tree.Tabs[0].Sections[0].Columns) != 1 {
		t.Fatalf("expected 1 column, got %d", len(tree.Tabs[0].Sections[0].Columns))
	}

	assertStringSlice(t, "fields", tree.Tabs[0].Sections[0].Columns[0].Fields, []string{"a", "b"})

	if len(fieldsMap) != 2 {
		t.Fatalf("fieldsMap: expected 2 entries, got %d", len(fieldsMap))
	}
}

// TestFlatToTree_CleanupEmptyColumns verifies that empty columns are removed
// during cleanup, but at least one column per section is kept.
func TestFlatToTree_CleanupEmptyColumns(t *testing.T) {
	fields := []FieldDef{
		{Name: "title", FieldType: FieldTypeData, Label: "Title"},
		{Name: "cb_empty", FieldType: FieldTypeColumnBreak},
		// No fields after column break => empty column
		{Name: "sb_next", FieldType: FieldTypeSectionBreak, Label: "Next"},
		{Name: "status", FieldType: FieldTypeData, Label: "Status"},
	}

	tree, _ := FlatToTree(fields)

	// First section should have 1 column (empty one cleaned up)
	sec0 := tree.Tabs[0].Sections[0]
	if len(sec0.Columns) != 1 {
		t.Errorf("sec0: expected 1 column after cleanup, got %d", len(sec0.Columns))
	}
	assertStringSlice(t, "sec0.col0", sec0.Columns[0].Fields, []string{"title"})
}

// TestFlatToTree_TabBreakLabelFallback verifies that TabBreak uses
// LayoutHint.Label, then field.Label as fallback.
func TestFlatToTree_TabBreakLabelFallback(t *testing.T) {
	fields := []FieldDef{
		{Name: "f1", FieldType: FieldTypeData, Label: "Field 1"},
		// Tab with LayoutHint.Label
		{Name: "tb1", FieldType: FieldTypeTabBreak, Label: "Fallback", LayoutHint: LayoutHint{Label: "Primary"}},
		{Name: "f2", FieldType: FieldTypeData, Label: "Field 2"},
		// Tab with only field.Label
		{Name: "tb2", FieldType: FieldTypeTabBreak, Label: "Only Label"},
		{Name: "f3", FieldType: FieldTypeData, Label: "Field 3"},
	}

	tree, _ := FlatToTree(fields)

	if len(tree.Tabs) != 3 {
		t.Fatalf("expected 3 tabs, got %d", len(tree.Tabs))
	}
	if tree.Tabs[0].Label != "Details" {
		t.Errorf("tab[0].Label = %q, want %q", tree.Tabs[0].Label, "Details")
	}
	if tree.Tabs[1].Label != "Primary" {
		t.Errorf("tab[1].Label = %q, want %q", tree.Tabs[1].Label, "Primary")
	}
	if tree.Tabs[2].Label != "Only Label" {
		t.Errorf("tab[2].Label = %q, want %q", tree.Tabs[2].Label, "Only Label")
	}
}

// TestFlatToTree_CleanupEmptyTabs verifies that completely empty tabs are
// removed, but at least one tab is always preserved.
func TestFlatToTree_CleanupEmptyTabs(t *testing.T) {
	fields := []FieldDef{
		// First tab: "Details" with a field
		{Name: "title", FieldType: FieldTypeData, Label: "Title"},
		// Second tab: completely empty (no fields after break)
		{Name: "tb_empty", FieldType: FieldTypeTabBreak, Label: "Empty Tab"},
		// Third tab: has a field
		{Name: "tb_settings", FieldType: FieldTypeTabBreak, Label: "Settings"},
		{Name: "enabled", FieldType: FieldTypeCheck, Label: "Enabled"},
	}

	tree, _ := FlatToTree(fields)

	// Empty tab should be removed; only "Details" and "Settings" remain
	if len(tree.Tabs) != 2 {
		t.Fatalf("expected 2 tabs after cleanup, got %d", len(tree.Tabs))
	}
	if tree.Tabs[0].Label != "Details" {
		t.Errorf("tab[0].Label = %q, want %q", tree.Tabs[0].Label, "Details")
	}
	if tree.Tabs[1].Label != "Settings" {
		t.Errorf("tab[1].Label = %q, want %q", tree.Tabs[1].Label, "Settings")
	}
}

// TestFlatToTree_AllEmptyTabsKeepsFirst verifies that when all tabs are empty,
// at least the first tab is preserved.
func TestFlatToTree_AllEmptyTabsKeepsFirst(t *testing.T) {
	fields := []FieldDef{
		{Name: "tb1", FieldType: FieldTypeTabBreak, Label: "Tab 1"},
		{Name: "tb2", FieldType: FieldTypeTabBreak, Label: "Tab 2"},
	}

	tree, _ := FlatToTree(fields)

	if len(tree.Tabs) < 1 {
		t.Fatal("expected at least 1 tab, got 0")
	}
	// At least one tab must survive
	if len(tree.Tabs) > 1 {
		t.Errorf("expected 1 tab (all empty -> keep first), got %d", len(tree.Tabs))
	}
}

// ── helpers ──────────────────────────────────────────────────────────────

func assertStringSlice(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: length %d, want %d; got %v", name, len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s[%d] = %q, want %q", name, i, got[i], want[i])
		}
	}
}

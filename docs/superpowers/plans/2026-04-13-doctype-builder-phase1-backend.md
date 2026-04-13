# DocType Builder — Phase 1: Backend Storage & API

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add tree-native layout storage to MetaType, update the meta API to serve layout data, create a flat-to-tree migration utility, and implement dev-mode API endpoints for the DocType Builder UI.

**Architecture:** Extend `MetaType` with optional `Layout *LayoutTree` and derived `FieldsMap`. The compiler auto-detects format: if JSON has `"layout"` key → tree-native path; otherwise → legacy flat path. After compilation, both `Fields []FieldDef` (flat) and `Layout` (tree) are always populated. Downstream code (DDL, validation, search, API) continues reading `mt.Fields` unchanged.

**Tech Stack:** Go 1.26+, PostgreSQL 16, pgx v5, golangci-lint v2

**Spec:** `docs/superpowers/specs/2026-04-13-doctype-builder-design.md`

---

## File Structure

### New Files
| File | Purpose |
|------|---------|
| `pkg/meta/layout.go` | LayoutTree, TabDef, SectionDef, ColumnDef structs + ExtractFields + FlatToTree |
| `pkg/meta/layout_test.go` | Unit tests for layout types, extraction, and conversion |
| `pkg/meta/migrate_layout.go` | CLI-callable function to migrate flat JSON files to tree-native |
| `pkg/meta/migrate_layout_test.go` | Migration round-trip tests |
| `pkg/api/dev_handler.go` | Dev-mode API handler (CRUD for DocType files on disk) |
| `pkg/api/dev_handler_test.go` | Dev API unit tests |
| `pkg/api/dev_validation.go` | Shared DocType definition validation rules |
| `pkg/api/dev_validation_test.go` | Validation rule tests |

### Modified Files
| File | Change |
|------|--------|
| `pkg/meta/metatype.go` | Add `Layout *LayoutTree` and `FieldsMap map[string]FieldDef` fields |
| `pkg/meta/compiler.go` | Dual-format detection: tree-native vs flat. Populate all three representations. |
| `pkg/meta/compiler_test.go` | Add tests for tree-native compilation |
| `pkg/api/rest.go` | Update `buildMetaResponse` to include layout tree + fields_ordered |
| `pkg/api/rest.go` | Add dev routes in `RegisterRoutes` |
| `desk/src/api/types.ts` | Add LayoutTree, TabDef, SectionDef, ColumnDef types; update MetaType |

---

### Task 1: Layout Tree Types

**Files:**
- Create: `pkg/meta/layout.go`
- Test: `pkg/meta/layout_test.go`

- [ ] **Step 1: Write the failing test for layout struct serialization**

Create `pkg/meta/layout_test.go`:

```go
package meta_test

import (
	"encoding/json"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

func TestLayoutTree_JSON_RoundTrip(t *testing.T) {
	layout := meta.LayoutTree{
		Tabs: []meta.TabDef{
			{
				Label: "Details",
				Sections: []meta.SectionDef{
					{
						Label:       "Basic Info",
						Collapsible: false,
						Columns: []meta.ColumnDef{
							{Width: 2, Fields: []string{"title", "author"}},
							{Width: 1, Fields: []string{"isbn", "price"}},
						},
					},
				},
			},
		},
	}

	data, err := json.Marshal(layout)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got meta.LayoutTree
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(got.Tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(got.Tabs))
	}
	if got.Tabs[0].Label != "Details" {
		t.Fatalf("expected tab label 'Details', got %q", got.Tabs[0].Label)
	}
	sec := got.Tabs[0].Sections[0]
	if len(sec.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(sec.Columns))
	}
	if sec.Columns[0].Width != 2 {
		t.Fatalf("expected column width 2, got %d", sec.Columns[0].Width)
	}
	if len(sec.Columns[0].Fields) != 2 {
		t.Fatalf("expected 2 fields in column 0, got %d", len(sec.Columns[0].Fields))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/osamamuhammed/Moca && go test -run TestLayoutTree_JSON_RoundTrip ./pkg/meta/...`
Expected: FAIL — `meta.LayoutTree` type not found

- [ ] **Step 3: Implement layout types**

Create `pkg/meta/layout.go`:

```go
package meta

// LayoutTree describes the visual arrangement of fields in a DocType form.
// The hierarchy is: Tabs > Sections > Columns > field references (by name).
// Field definitions themselves live in MetaType.FieldsMap keyed by name.
type LayoutTree struct {
	Tabs []TabDef `json:"tabs"`
}

// TabDef represents a single tab in the form layout.
type TabDef struct {
	Label    string       `json:"label"`
	Sections []SectionDef `json:"sections"`
}

// SectionDef represents a section within a tab.
type SectionDef struct {
	Label              string      `json:"label,omitempty"`
	Collapsible        bool        `json:"collapsible,omitempty"`
	CollapsedByDefault bool        `json:"collapsed_by_default,omitempty"`
	Columns            []ColumnDef `json:"columns"`
}

// ColumnDef represents a column within a section. Width is relative (flex-like).
type ColumnDef struct {
	Width  int      `json:"width"`
	Fields []string `json:"fields"` // field names referencing FieldsMap
}

// ExtractFieldsOrdered walks the layout tree and returns field definitions
// in layout order. This is the flat list needed by DDL, validation, API, search.
func ExtractFieldsOrdered(layout *LayoutTree, fieldsMap map[string]FieldDef) []FieldDef {
	if layout == nil {
		return nil
	}
	var result []FieldDef
	for _, tab := range layout.Tabs {
		for _, section := range tab.Sections {
			for _, column := range section.Columns {
				for _, name := range column.Fields {
					if fd, ok := fieldsMap[name]; ok {
						result = append(result, fd)
					}
				}
			}
		}
	}
	return result
}

// FlatToTree converts a flat []FieldDef (with SectionBreak/ColumnBreak/TabBreak
// delimiter pseudo-fields) into a LayoutTree and a FieldsMap. This is used for
// migrating legacy JSON files to tree-native format.
func FlatToTree(fields []FieldDef) (*LayoutTree, map[string]FieldDef) {
	fieldsMap := make(map[string]FieldDef)
	layout := &LayoutTree{}

	// Current state during iteration
	var currentTab *TabDef
	var currentSection *SectionDef
	var currentColumn *ColumnDef

	finishColumn := func() {
		if currentColumn != nil && currentSection != nil {
			currentSection.Columns = append(currentSection.Columns, *currentColumn)
			currentColumn = nil
		}
	}
	finishSection := func() {
		finishColumn()
		if currentSection != nil && currentTab != nil {
			currentTab.Sections = append(currentTab.Sections, *currentSection)
			currentSection = nil
		}
	}
	finishTab := func() {
		finishSection()
		if currentTab != nil {
			layout.Tabs = append(layout.Tabs, *currentTab)
			currentTab = nil
		}
	}

	ensureTab := func() {
		if currentTab == nil {
			currentTab = &TabDef{Label: "Details"}
		}
	}
	ensureSection := func() {
		ensureTab()
		if currentSection == nil {
			currentSection = &SectionDef{}
		}
	}
	ensureColumn := func() {
		ensureSection()
		if currentColumn == nil {
			currentColumn = &ColumnDef{Width: 1}
		}
	}

	for _, f := range fields {
		switch f.FieldType {
		case FieldTypeTabBreak:
			finishTab()
			label := f.LayoutHint.Label
			if label == "" {
				label = f.Label
			}
			if label == "" {
				label = "Tab"
			}
			currentTab = &TabDef{Label: label}
		case FieldTypeSectionBreak:
			finishSection()
			ensureTab()
			label := f.LayoutHint.Label
			if label == "" {
				label = f.Label
			}
			currentSection = &SectionDef{
				Label:              label,
				Collapsible:        f.LayoutHint.Collapsible,
				CollapsedByDefault: f.LayoutHint.CollapsedByDefault,
			}
		case FieldTypeColumnBreak:
			finishColumn()
			ensureSection()
			currentColumn = &ColumnDef{Width: 1}
		default:
			// Regular field — add to current column and fields map
			ensureColumn()
			currentColumn.Fields = append(currentColumn.Fields, f.Name)
			fieldsMap[f.Name] = f
		}
	}

	// Finalize remaining state
	finishTab()

	// If no fields at all, return a minimal layout
	if len(layout.Tabs) == 0 {
		layout.Tabs = []TabDef{{
			Label: "Details",
			Sections: []SectionDef{{
				Columns: []ColumnDef{{Width: 1}},
			}},
		}}
	}

	// Cleanup: remove empty sections and tabs
	for i := range layout.Tabs {
		var nonEmpty []SectionDef
		for _, s := range layout.Tabs[i].Sections {
			hasFields := false
			for _, c := range s.Columns {
				if len(c.Fields) > 0 {
					hasFields = true
					break
				}
			}
			if hasFields || s.Label != "" {
				nonEmpty = append(nonEmpty, s)
			}
		}
		if len(nonEmpty) > 0 {
			layout.Tabs[i].Sections = nonEmpty
		}
	}

	return layout, fieldsMap
}

// DefaultLayout creates a minimal layout wrapping all fields in a single
// tab > section > column. Used when legacy JSON has no break fields.
func DefaultLayout(fields []FieldDef) (*LayoutTree, map[string]FieldDef) {
	fieldsMap := make(map[string]FieldDef, len(fields))
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		if !f.FieldType.IsStorable() && f.FieldType != FieldTypeHTML &&
			f.FieldType != FieldTypeButton && f.FieldType != FieldTypeHeading {
			continue // skip layout delimiter types
		}
		fieldsMap[f.Name] = f
		names = append(names, f.Name)
	}

	return &LayoutTree{
		Tabs: []TabDef{{
			Label: "Details",
			Sections: []SectionDef{{
				Columns: []ColumnDef{{
					Width:  1,
					Fields: names,
				}},
			}},
		}},
	}, fieldsMap
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/osamamuhammed/Moca && go test -run TestLayoutTree_JSON_RoundTrip ./pkg/meta/...`
Expected: PASS

- [ ] **Step 5: Add tests for FlatToTree and ExtractFieldsOrdered**

Append to `pkg/meta/layout_test.go`:

```go
func TestFlatToTree_NoBreaks(t *testing.T) {
	fields := []meta.FieldDef{
		{Name: "title", FieldType: meta.FieldTypeData, Label: "Title"},
		{Name: "status", FieldType: meta.FieldTypeSelect, Label: "Status"},
	}

	layout, fm := meta.FlatToTree(fields)

	if len(layout.Tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(layout.Tabs))
	}
	if layout.Tabs[0].Label != "Details" {
		t.Fatalf("expected default tab 'Details', got %q", layout.Tabs[0].Label)
	}
	if len(fm) != 2 {
		t.Fatalf("expected 2 fields in map, got %d", len(fm))
	}
	if _, ok := fm["title"]; !ok {
		t.Fatal("expected 'title' in fields map")
	}
}

func TestFlatToTree_WithBreaks(t *testing.T) {
	fields := []meta.FieldDef{
		{Name: "tab1", FieldType: meta.FieldTypeTabBreak, Label: "General"},
		{Name: "sec1", FieldType: meta.FieldTypeSectionBreak, Label: "Info", LayoutHint: meta.LayoutHint{Label: "Info"}},
		{Name: "col1", FieldType: meta.FieldTypeColumnBreak},
		{Name: "title", FieldType: meta.FieldTypeData, Label: "Title"},
		{Name: "col2", FieldType: meta.FieldTypeColumnBreak},
		{Name: "price", FieldType: meta.FieldTypeCurrency, Label: "Price"},
		{Name: "tab2", FieldType: meta.FieldTypeTabBreak, Label: "Details"},
		{Name: "sec2", FieldType: meta.FieldTypeSectionBreak, Label: "More"},
		{Name: "col3", FieldType: meta.FieldTypeColumnBreak},
		{Name: "desc", FieldType: meta.FieldTypeText, Label: "Description"},
	}

	layout, fm := meta.FlatToTree(fields)

	if len(layout.Tabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(layout.Tabs))
	}
	if layout.Tabs[0].Label != "General" {
		t.Fatalf("expected tab 0 'General', got %q", layout.Tabs[0].Label)
	}
	if layout.Tabs[1].Label != "Details" {
		t.Fatalf("expected tab 1 'Details', got %q", layout.Tabs[1].Label)
	}
	if len(fm) != 3 {
		t.Fatalf("expected 3 fields in map, got %d", len(fm))
	}

	// Verify section in first tab
	sec := layout.Tabs[0].Sections[0]
	if len(sec.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(sec.Columns))
	}
	if sec.Columns[0].Fields[0] != "title" {
		t.Fatalf("expected 'title' in col 0, got %q", sec.Columns[0].Fields[0])
	}
	if sec.Columns[1].Fields[0] != "price" {
		t.Fatalf("expected 'price' in col 1, got %q", sec.Columns[1].Fields[0])
	}
}

func TestExtractFieldsOrdered(t *testing.T) {
	fm := map[string]meta.FieldDef{
		"title":  {Name: "title", FieldType: meta.FieldTypeData},
		"price":  {Name: "price", FieldType: meta.FieldTypeCurrency},
		"author": {Name: "author", FieldType: meta.FieldTypeLink},
	}
	layout := &meta.LayoutTree{
		Tabs: []meta.TabDef{{
			Label: "Details",
			Sections: []meta.SectionDef{{
				Columns: []meta.ColumnDef{
					{Width: 2, Fields: []string{"title", "author"}},
					{Width: 1, Fields: []string{"price"}},
				},
			}},
		}},
	}

	ordered := meta.ExtractFieldsOrdered(layout, fm)
	if len(ordered) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(ordered))
	}
	expected := []string{"title", "author", "price"}
	for i, name := range expected {
		if ordered[i].Name != name {
			t.Errorf("field %d: expected %q, got %q", i, name, ordered[i].Name)
		}
	}
}

func TestFlatToTree_RoundTrip(t *testing.T) {
	// Create flat fields, convert to tree, extract back to flat, verify same order
	fields := []meta.FieldDef{
		{Name: "a", FieldType: meta.FieldTypeData, Label: "A"},
		{Name: "b", FieldType: meta.FieldTypeInt, Label: "B"},
		{Name: "c", FieldType: meta.FieldTypeCheck, Label: "C"},
	}

	layout, fm := meta.FlatToTree(fields)
	extracted := meta.ExtractFieldsOrdered(layout, fm)

	if len(extracted) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(extracted))
	}
	for i, f := range fields {
		if extracted[i].Name != f.Name {
			t.Errorf("field %d: expected %q, got %q", i, f.Name, extracted[i].Name)
		}
	}
}
```

- [ ] **Step 6: Run all layout tests**

Run: `cd /Users/osamamuhammed/Moca && go test -run TestLayout -run TestFlatToTree -run TestExtract ./pkg/meta/...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add pkg/meta/layout.go pkg/meta/layout_test.go
git commit -m "feat(meta): add tree-native layout types and FlatToTree conversion"
```

---

### Task 2: Extend MetaType with Layout Fields

**Files:**
- Modify: `pkg/meta/metatype.go:34-61`
- Test: `pkg/meta/metatype_test.go`

- [ ] **Step 1: Write test for MetaType with Layout field**

Add to `pkg/meta/metatype_test.go` (or create it):

```go
func TestMetaType_LayoutAndFieldsMap(t *testing.T) {
	mt := meta.MetaType{
		Name:   "Book",
		Module: "Library",
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData},
		},
		Layout: &meta.LayoutTree{
			Tabs: []meta.TabDef{{
				Label: "Details",
				Sections: []meta.SectionDef{{
					Columns: []meta.ColumnDef{{
						Width:  1,
						Fields: []string{"title"},
					}},
				}},
			}},
		},
		FieldsMap: map[string]meta.FieldDef{
			"title": {Name: "title", FieldType: meta.FieldTypeData},
		},
	}

	if mt.Layout == nil {
		t.Fatal("expected Layout to be set")
	}
	if len(mt.FieldsMap) != 1 {
		t.Fatalf("expected 1 field in FieldsMap, got %d", len(mt.FieldsMap))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/osamamuhammed/Moca && go test -run TestMetaType_LayoutAndFieldsMap ./pkg/meta/...`
Expected: FAIL — `mt.Layout` and `mt.FieldsMap` fields unknown

- [ ] **Step 3: Add Layout and FieldsMap to MetaType**

In `pkg/meta/metatype.go`, add two fields to the MetaType struct:

```go
type MetaType struct {
	// ... existing fields unchanged ...
	Fields        []FieldDef              `json:"fields"`
	Layout        *LayoutTree             `json:"layout,omitempty"`     // tree-native layout
	FieldsMap     map[string]FieldDef     `json:"-"`                    // derived, not serialized
	// ... rest unchanged ...
}
```

Add `Layout` after `Fields` and `FieldsMap` after `Layout`. The `FieldsMap` uses `json:"-"` because it's derived during compilation, not stored.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/osamamuhammed/Moca && go test -run TestMetaType_LayoutAndFieldsMap ./pkg/meta/...`
Expected: PASS

- [ ] **Step 5: Run full test suite to verify no regressions**

Run: `cd /Users/osamamuhammed/Moca && go test -race ./pkg/meta/...`
Expected: All existing tests PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/meta/metatype.go pkg/meta/metatype_test.go
git commit -m "feat(meta): add Layout and FieldsMap fields to MetaType"
```

---

### Task 3: Dual-Format Compiler

**Files:**
- Modify: `pkg/meta/compiler.go:73-181`
- Test: `pkg/meta/compiler_test.go`

- [ ] **Step 1: Write test for tree-native JSON compilation**

Add to `pkg/meta/compiler_test.go`:

```go
func TestCompile_TreeNativeFormat(t *testing.T) {
	input := []byte(`{
		"name": "Book",
		"module": "Library",
		"layout": {
			"tabs": [{
				"label": "Details",
				"sections": [{
					"label": "Basic",
					"columns": [{
						"width": 1,
						"fields": ["title", "isbn"]
					}]
				}]
			}]
		},
		"fields": {
			"title": {"field_type": "Data", "label": "Title", "required": true},
			"isbn": {"field_type": "Data", "label": "ISBN"}
		}
	}`)

	mt, err := meta.Compile(input)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Layout should be populated
	if mt.Layout == nil {
		t.Fatal("expected Layout to be set")
	}
	if len(mt.Layout.Tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(mt.Layout.Tabs))
	}

	// FieldsMap should be populated
	if len(mt.FieldsMap) != 2 {
		t.Fatalf("expected 2 fields in FieldsMap, got %d", len(mt.FieldsMap))
	}
	if _, ok := mt.FieldsMap["title"]; !ok {
		t.Fatal("expected 'title' in FieldsMap")
	}

	// Fields (flat slice) should be derived from layout order
	if len(mt.Fields) != 2 {
		t.Fatalf("expected 2 flat fields, got %d", len(mt.Fields))
	}
	if mt.Fields[0].Name != "title" {
		t.Errorf("expected first field 'title', got %q", mt.Fields[0].Name)
	}
	if !mt.Fields[0].Required {
		t.Error("expected title to be required")
	}
}

func TestCompile_TreeNative_FieldNameInMap(t *testing.T) {
	// Verify that field Name is populated from the map key
	input := []byte(`{
		"name": "Test",
		"module": "Core",
		"layout": {"tabs": [{"label": "D", "sections": [{"columns": [{"width": 1, "fields": ["my_field"]}]}]}]},
		"fields": {"my_field": {"field_type": "Data", "label": "My Field"}}
	}`)

	mt, err := meta.Compile(input)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if mt.Fields[0].Name != "my_field" {
		t.Errorf("expected Name 'my_field', got %q", mt.Fields[0].Name)
	}
}

func TestCompile_LegacyFlat_StillWorks(t *testing.T) {
	// Ensure existing flat format still compiles correctly
	input := []byte(`{
		"name": "OldDocType",
		"module": "Core",
		"fields": [
			{"name": "title", "field_type": "Data", "label": "Title"}
		]
	}`)

	mt, err := meta.Compile(input)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Fields should be present
	if len(mt.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(mt.Fields))
	}

	// Layout should be auto-generated
	if mt.Layout == nil {
		t.Fatal("expected Layout to be auto-generated for flat format")
	}
	if len(mt.Layout.Tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(mt.Layout.Tabs))
	}

	// FieldsMap should be populated
	if len(mt.FieldsMap) != 1 {
		t.Fatalf("expected 1 field in FieldsMap, got %d", len(mt.FieldsMap))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/osamamuhammed/Moca && go test -run "TestCompile_TreeNative|TestCompile_LegacyFlat_StillWorks" ./pkg/meta/...`
Expected: FAIL

- [ ] **Step 3: Update compiler for dual-format detection**

The compiler needs to detect whether the JSON has a `"layout"` key. Update `pkg/meta/compiler.go`:

First, add a raw intermediate struct for format detection at the top of `Compile()`:

```go
func Compile(jsonBytes []byte) (*MetaType, error) {
	// Detect format: tree-native (has "layout" + "fields" as object) vs flat (has "fields" as array)
	var probe struct {
		Layout json.RawMessage `json:"layout"`
		Fields json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(jsonBytes, &probe); err != nil {
		return nil, fmt.Errorf("compile: invalid JSON: %w", err)
	}

	isTreeNative := len(probe.Layout) > 0 && probe.Layout[0] == '{'

	if isTreeNative {
		return compileTreeNative(jsonBytes)
	}
	return compileFlatLegacy(jsonBytes)
}
```

Extract the current `Compile` body into `compileFlatLegacy`, then add layout auto-generation at the end:

```go
func compileFlatLegacy(jsonBytes []byte) (*MetaType, error) {
	// ... existing Compile body (Rules 1-12) ...
	// After validation passes, auto-generate Layout and FieldsMap:
	mt.Layout, mt.FieldsMap = FlatToTree(mt.Fields)
	return &mt, nil
}
```

Add the tree-native compiler:

```go
// treeNativeInput is the JSON shape for tree-native MetaType definitions.
type treeNativeInput struct {
	MetaType
	FieldsObj map[string]FieldDef `json:"fields"`
	LayoutObj LayoutTree          `json:"layout"`
}

func compileTreeNative(jsonBytes []byte) (*MetaType, error) {
	// First unmarshal everything except fields (which is an object, not array)
	var raw struct {
		Name          string         `json:"name"`
		Module        string         `json:"module"`
		Label         string         `json:"label"`
		Description   string         `json:"description"`
		NamingRule    NamingStrategy `json:"naming_rule"`
		TitleField    string         `json:"title_field"`
		ImageField    string         `json:"image_field"`
		SortField     string         `json:"sort_field"`
		SortOrder     string         `json:"sort_order"`
		SearchFields  []string       `json:"search_fields"`
		Permissions   []PermRule     `json:"permissions"`
		APIConfig     *APIConfig     `json:"api_config,omitempty"`
		IsSubmittable bool           `json:"is_submittable"`
		IsVirtual     bool           `json:"is_virtual"`
		IsChildTable  bool           `json:"is_child_table"`
		IsSingle      bool           `json:"is_single"`
		TrackChanges  bool           `json:"track_changes"`
		EventSourcing bool           `json:"event_sourcing"`
		CDCEnabled    bool           `json:"cdc_enabled"`
		Layout        LayoutTree          `json:"layout"`
		Fields        map[string]FieldDef `json:"fields"`
	}
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		return nil, fmt.Errorf("compile: invalid JSON: %w", err)
	}

	// Populate field Name from map key
	for name, fd := range raw.Fields {
		fd.Name = name
		raw.Fields[name] = fd
	}

	// Build MetaType
	mt := MetaType{
		Name:          raw.Name,
		Module:        raw.Module,
		Label:         raw.Label,
		Description:   raw.Description,
		NamingRule:    raw.NamingRule,
		TitleField:    raw.TitleField,
		ImageField:    raw.ImageField,
		SortField:     raw.SortField,
		SortOrder:     raw.SortOrder,
		SearchFields:  raw.SearchFields,
		Permissions:   raw.Permissions,
		APIConfig:     raw.APIConfig,
		IsSubmittable: raw.IsSubmittable,
		IsVirtual:     raw.IsVirtual,
		IsChildTable:  raw.IsChildTable,
		IsSingle:      raw.IsSingle,
		TrackChanges:  raw.TrackChanges,
		EventSourcing: raw.EventSourcing,
		CDCEnabled:    raw.CDCEnabled,
		Layout:        &raw.Layout,
		FieldsMap:     raw.Fields,
	}

	// Derive flat Fields from layout order
	mt.Fields = ExtractFieldsOrdered(&raw.Layout, raw.Fields)

	// Default InAPI to true for storage fields
	for i := range mt.Fields {
		if mt.Fields[i].FieldType.IsStorable() && !mt.Fields[i].inAPIPresent {
			mt.Fields[i].InAPI = true
		}
	}
	// Also update FieldsMap
	for name, fd := range mt.FieldsMap {
		if fd.FieldType.IsStorable() && !fd.inAPIPresent {
			fd.InAPI = true
			mt.FieldsMap[name] = fd
		}
	}

	// Run the same validation rules
	var errs []CompileError
	add := func(field, message string) {
		errs = append(errs, CompileError{Field: field, Message: message})
	}

	if mt.Name == "" {
		add("name", "required")
	}
	if mt.Module == "" {
		add("module", "required")
	}

	fieldNames := make(map[string]bool, len(mt.Fields))
	for i, f := range mt.Fields {
		if !f.FieldType.IsValid() {
			add(fmt.Sprintf("fields.%s.field_type", f.Name),
				fmt.Sprintf("invalid field type: %q", f.FieldType))
		}
		if fieldNames[f.Name] {
			add(fmt.Sprintf("fields.%s", f.Name),
				fmt.Sprintf("duplicate field name: %q", f.Name))
		} else {
			fieldNames[f.Name] = true
		}
		if (f.FieldType == FieldTypeLink || f.FieldType == FieldTypeDynamicLink) && f.Options == "" {
			add(fmt.Sprintf("fields.%s.options", f.Name),
				fmt.Sprintf("required for %s field type", f.FieldType))
		}
		if (f.FieldType == FieldTypeTable || f.FieldType == FieldTypeTableMultiSelect) && f.Options == "" {
			add(fmt.Sprintf("fields[%d].options", i),
				fmt.Sprintf("required for %s field type", f.FieldType))
		}
	}

	for _, sf := range mt.SearchFields {
		if !fieldNames[sf] {
			add("search_fields", fmt.Sprintf("references unknown field: %q", sf))
		}
	}
	if mt.TitleField != "" && !fieldNames[mt.TitleField] {
		add("title_field", fmt.Sprintf("references unknown field: %q", mt.TitleField))
	}
	if mt.SortField != "" && !fieldNames[mt.SortField] && !standardColumns[mt.SortField] {
		add("sort_field", fmt.Sprintf("references unknown field: %q", mt.SortField))
	}

	if mt.NamingRule.Rule == "" {
		mt.NamingRule.Rule = NamingUUID
	} else if !validNamingRules[mt.NamingRule.Rule] {
		add("naming_rule.rule", fmt.Sprintf("invalid naming rule: %q", mt.NamingRule.Rule))
	}
	if validNamingRules[mt.NamingRule.Rule] {
		if mt.NamingRule.Rule == NamingByField {
			if mt.NamingRule.FieldName == "" {
				add("naming_rule.field_name", `required when rule is "field"`)
			} else if !fieldNames[mt.NamingRule.FieldName] {
				add("naming_rule.field_name",
					fmt.Sprintf("references unknown field: %q", mt.NamingRule.FieldName))
			}
		}
		if mt.NamingRule.Rule == NamingByPattern && mt.NamingRule.Pattern == "" {
			add("naming_rule.pattern", `required when rule is "pattern"`)
		}
	}

	// Layout-specific validation
	if len(mt.Layout.Tabs) == 0 {
		add("layout.tabs", "at least one tab is required")
	}
	for ti, tab := range mt.Layout.Tabs {
		if len(tab.Sections) == 0 {
			add(fmt.Sprintf("layout.tabs[%d].sections", ti), "at least one section is required")
		}
		for si, sec := range tab.Sections {
			for ci, col := range sec.Columns {
				if col.Width < 1 {
					add(fmt.Sprintf("layout.tabs[%d].sections[%d].columns[%d].width", ti, si, ci),
						"must be a positive integer")
				}
			}
		}
	}

	if len(errs) > 0 {
		return nil, &CompileErrors{Errors: errs}
	}
	return &mt, nil
}
```

Also update `compileFlatLegacy` (rename from existing `Compile` body) to populate Layout and FieldsMap at the end:

After the existing `return &mt, nil`, add before the return:

```go
	// Auto-generate Layout and FieldsMap for legacy flat format
	mt.Layout, mt.FieldsMap = FlatToTree(mt.Fields)

	return &mt, nil
```

- [ ] **Step 4: Run all compiler tests**

Run: `cd /Users/osamamuhammed/Moca && go test -race ./pkg/meta/...`
Expected: All tests PASS (new + existing)

- [ ] **Step 5: Commit**

```bash
git add pkg/meta/compiler.go pkg/meta/compiler_test.go
git commit -m "feat(meta): dual-format compiler supports tree-native and flat JSON"
```

---

### Task 4: Update Meta API Response

**Files:**
- Modify: `pkg/api/rest.go` (lines around `buildMetaResponse` and `apiMetaResponse`)
- Modify: `desk/src/api/types.ts`

- [ ] **Step 1: Update apiMetaResponse struct to include layout**

In `pkg/api/rest.go`, add layout fields to `apiMetaResponse`:

```go
type apiMetaResponse struct {
	// ... existing fields ...
	Layout        *meta.LayoutTree           `json:"layout,omitempty"`
	FieldsMap     map[string]apiFieldDef     `json:"fields_map,omitempty"`
	// existing Fields becomes fields_ordered for backward compat
}
```

- [ ] **Step 2: Update buildMetaResponse to populate layout**

Add layout population at the end of `buildMetaResponse()`:

```go
	// Add tree-native layout if available
	if mt.Layout != nil {
		resp.Layout = mt.Layout
	}

	// Build fields_map for tree-native consumers
	if mt.FieldsMap != nil {
		resp.FieldsMap = make(map[string]apiFieldDef, len(mt.FieldsMap))
		for name, f := range mt.FieldsMap {
			resp.FieldsMap[name] = apiFieldDef{
				Name:      name,
				FieldType: string(f.FieldType),
				Label:     f.Label,
				// ... same field mapping as the loop above ...
			}
		}
	}
```

- [ ] **Step 3: Update TypeScript types**

In `desk/src/api/types.ts`, add layout types and update MetaType:

```typescript
// Tree-native layout types
export interface LayoutTree {
  tabs: TabDef[];
}

export interface TabDef {
  label: string;
  sections: SectionDef[];
}

export interface SectionDef {
  label?: string;
  collapsible?: boolean;
  collapsed_by_default?: boolean;
  columns: ColumnDef[];
}

export interface ColumnDef {
  width: number;
  fields: string[]; // field names referencing fields_map
}

// Update MetaType to include optional layout
export interface MetaType {
  // ... existing fields ...
  layout?: LayoutTree;
  fields_map?: Record<string, FieldDef>;
}
```

- [ ] **Step 4: Run backend tests**

Run: `cd /Users/osamamuhammed/Moca && go test -race ./pkg/api/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/api/rest.go desk/src/api/types.ts
git commit -m "feat(api): include layout tree in meta endpoint response"
```

---

### Task 5: Dev-Mode Validation Rules

**Files:**
- Create: `pkg/api/dev_validation.go`
- Create: `pkg/api/dev_validation_test.go`

- [ ] **Step 1: Write validation tests**

Create `pkg/api/dev_validation_test.go`:

```go
package api_test

import (
	"testing"

	"github.com/osama1998H/moca/pkg/api"
)

func TestValidateDocTypeName_Valid(t *testing.T) {
	for _, name := range []string{"Book", "SalesOrder", "HTTPConfig"} {
		if err := api.ValidateDocTypeName(name); err != nil {
			t.Errorf("expected %q to be valid, got: %v", name, err)
		}
	}
}

func TestValidateDocTypeName_Invalid(t *testing.T) {
	for _, name := range []string{"", "book", "sales_order", "123Book"} {
		if err := api.ValidateDocTypeName(name); err == nil {
			t.Errorf("expected %q to be invalid", name)
		}
	}
}

func TestValidateFieldName_Valid(t *testing.T) {
	for _, name := range []string{"title", "first_name", "item2"} {
		if err := api.ValidateFieldName(name); err != nil {
			t.Errorf("expected %q to be valid, got: %v", name, err)
		}
	}
}

func TestValidateFieldName_Invalid(t *testing.T) {
	for _, name := range []string{"", "Title", "first-name", "name", "created_at", "modified_at", "owner", "_extra"} {
		if err := api.ValidateFieldName(name); err == nil {
			t.Errorf("expected %q to be invalid", name)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/osamamuhammed/Moca && go test -run "TestValidateDocType|TestValidateField" ./pkg/api/...`
Expected: FAIL

- [ ] **Step 3: Implement validation rules**

Create `pkg/api/dev_validation.go`:

```go
package api

import (
	"fmt"
	"regexp"
	"unicode"
)

var (
	fieldNameRegex = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	reservedFieldNames = map[string]bool{
		"name": true, "created_at": true, "modified_at": true,
		"owner": true, "_extra": true, "modified_by": true,
		"creation": true, "modified": true, "docstatus": true,
		"idx": true, "workflow_state": true, "parent": true,
		"parenttype": true, "parentfield": true,
	}
)

// ValidateDocTypeName checks that a DocType name is TitleCase (starts with uppercase).
func ValidateDocTypeName(name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	runes := []rune(name)
	if !unicode.IsUpper(runes[0]) {
		return fmt.Errorf("name must start with an uppercase letter, got %q", name)
	}
	for _, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return fmt.Errorf("name must contain only letters and digits, got %q", name)
		}
	}
	return nil
}

// ValidateFieldName checks that a field name is snake_case and not reserved.
func ValidateFieldName(name string) error {
	if name == "" {
		return fmt.Errorf("field name is required")
	}
	if !fieldNameRegex.MatchString(name) {
		return fmt.Errorf("field name must be snake_case (a-z, 0-9, _), got %q", name)
	}
	if reservedFieldNames[name] {
		return fmt.Errorf("field name %q is reserved", name)
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/osamamuhammed/Moca && go test -run "TestValidateDocType|TestValidateField" ./pkg/api/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/api/dev_validation.go pkg/api/dev_validation_test.go
git commit -m "feat(api): add DocType and field name validation rules"
```

---

### Task 6: Dev-Mode API Handler

**Files:**
- Create: `pkg/api/dev_handler.go`
- Create: `pkg/api/dev_handler_test.go`
- Modify: `pkg/api/rest.go` (register dev routes)

- [ ] **Step 1: Write test for dev handler list apps**

Create `pkg/api/dev_handler_test.go`:

```go
package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/osama1998H/moca/pkg/api"
)

func TestDevHandler_ListApps(t *testing.T) {
	h := api.NewDevHandler(t.TempDir(), nil, nil)
	req := httptest.NewRequest("GET", "/api/v1/dev/apps", nil)
	w := httptest.NewRecorder()

	h.HandleListApps(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Data []string `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Empty apps dir should return empty list
	if resp.Data == nil {
		t.Fatal("expected non-nil data array")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/osamamuhammed/Moca && go test -run TestDevHandler_ListApps ./pkg/api/...`
Expected: FAIL

- [ ] **Step 3: Implement DevHandler**

Create `pkg/api/dev_handler.go`:

```go
package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/osama1998H/moca/pkg/meta"
)

// DevHandler serves dev-mode API endpoints for creating/editing DocType
// definition files on disk. Only available when developer mode is enabled.
type DevHandler struct {
	appsDir  string
	registry *meta.Registry
	logger   *slog.Logger
}

// NewDevHandler creates a DevHandler that reads/writes DocType files
// under the given apps directory.
func NewDevHandler(appsDir string, registry *meta.Registry, logger *slog.Logger) *DevHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &DevHandler{appsDir: appsDir, registry: registry, logger: logger}
}

// RegisterDevRoutes registers dev-mode routes on the given mux.
func (h *DevHandler) RegisterDevRoutes(mux *http.ServeMux, version string) {
	p := "/api/" + version + "/dev"
	mux.HandleFunc("GET "+p+"/apps", h.HandleListApps)
	mux.HandleFunc("POST "+p+"/doctype", h.HandleCreateDocType)
	mux.HandleFunc("PUT "+p+"/doctype/{name}", h.HandleUpdateDocType)
	mux.HandleFunc("GET "+p+"/doctype/{name}", h.HandleGetDocType)
	mux.HandleFunc("DELETE "+p+"/doctype/{name}", h.HandleDeleteDocType)
}

// HandleListApps returns the list of installed apps.
func (h *DevHandler) HandleListApps(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(h.appsDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]any{"data": []string{}})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	apps := make([]string, 0)
	for _, e := range entries {
		if e.IsDir() {
			// Check for manifest.yaml
			mPath := filepath.Join(h.appsDir, e.Name(), "manifest.yaml")
			if _, err := os.Stat(mPath); err == nil {
				apps = append(apps, e.Name())
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": apps})
}

// DevDocTypeRequest is the request body for creating/updating a DocType.
type DevDocTypeRequest struct {
	Name        string                    `json:"name"`
	App         string                    `json:"app"`
	Module      string                    `json:"module"`
	Layout      meta.LayoutTree           `json:"layout"`
	Fields      map[string]meta.FieldDef  `json:"fields"`
	Settings    DevDocTypeSettings        `json:"settings"`
	Permissions []meta.PermRule           `json:"permissions"`
}

// DevDocTypeSettings holds DocType configuration.
type DevDocTypeSettings struct {
	NamingRule    meta.NamingStrategy `json:"naming_rule"`
	TitleField    string              `json:"title_field"`
	SortField     string              `json:"sort_field"`
	SortOrder     string              `json:"sort_order"`
	SearchFields  []string            `json:"search_fields"`
	ImageField    string              `json:"image_field"`
	IsSubmittable bool                `json:"is_submittable"`
	IsSingle      bool                `json:"is_single"`
	IsChildTable  bool                `json:"is_child_table"`
	IsVirtual     bool                `json:"is_virtual"`
	TrackChanges  bool                `json:"track_changes"`
}

// HandleCreateDocType creates a new DocType definition on disk.
func (h *DevHandler) HandleCreateDocType(w http.ResponseWriter, r *http.Request) {
	var req DevDocTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if err := ValidateDocTypeName(req.Name); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.App == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "app is required"})
		return
	}
	if req.Module == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "module is required"})
		return
	}

	// Validate field names
	for name := range req.Fields {
		if err := ValidateFieldName(name); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}

	// Build the tree-native JSON for disk
	docDef := buildDocTypeJSON(req)

	// Write to disk
	moduleSnake := toSnakeCaseDev(req.Module)
	dtSnake := toSnakeCaseDev(req.Name)
	dtDir := filepath.Join(h.appsDir, req.App, "modules", moduleSnake, "doctypes", dtSnake)
	if err := os.MkdirAll(dtDir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create directory: " + err.Error()})
		return
	}

	jsonPath := filepath.Join(dtDir, dtSnake+".json")
	data, err := json.MarshalIndent(docDef, "", "  ")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "marshal: " + err.Error()})
		return
	}
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "write: " + err.Error()})
		return
	}

	// Write controller stub if it doesn't exist
	goPath := filepath.Join(dtDir, dtSnake+".go")
	if _, err := os.Stat(goPath); os.IsNotExist(err) {
		stub := fmt.Sprintf("package %s\n\n// %s controller.\n// Add lifecycle hooks here.\ntype %s struct{}\n", dtSnake, req.Name, req.Name)
		if err := os.WriteFile(goPath, []byte(stub), 0o644); err != nil {
			h.logger.Warn("failed to write controller stub", "error", err)
		}
	}

	// Register in registry if available (triggers DDL)
	if h.registry != nil {
		if _, err := h.registry.Register(r.Context(), siteFromContext(r), data); err != nil {
			h.logger.Warn("registry registration failed (non-fatal)", "error", err)
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{"data": docDef})
}

// HandleUpdateDocType updates an existing DocType JSON file (does NOT overwrite .go).
func (h *DevHandler) HandleUpdateDocType(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req DevDocTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	req.Name = name

	for fieldName := range req.Fields {
		if err := ValidateFieldName(fieldName); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}

	docDef := buildDocTypeJSON(req)
	moduleSnake := toSnakeCaseDev(req.Module)
	dtSnake := toSnakeCaseDev(req.Name)
	jsonPath := filepath.Join(h.appsDir, req.App, "modules", moduleSnake, "doctypes", dtSnake, dtSnake+".json")

	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "doctype not found at " + jsonPath})
		return
	}

	data, err := json.MarshalIndent(docDef, "", "  ")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "marshal: " + err.Error()})
		return
	}
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "write: " + err.Error()})
		return
	}

	if h.registry != nil {
		if _, err := h.registry.Register(r.Context(), siteFromContext(r), data); err != nil {
			h.logger.Warn("registry re-registration failed", "error", err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": docDef})
}

// HandleGetDocType reads a DocType definition from disk.
func (h *DevHandler) HandleGetDocType(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	// Search all apps for this doctype
	entries, _ := os.ReadDir(h.appsDir)
	for _, app := range entries {
		if !app.IsDir() {
			continue
		}
		modulesDir := filepath.Join(h.appsDir, app.Name(), "modules")
		modules, _ := os.ReadDir(modulesDir)
		for _, mod := range modules {
			if !mod.IsDir() {
				continue
			}
			dtSnake := toSnakeCaseDev(name)
			jsonPath := filepath.Join(modulesDir, mod.Name(), "doctypes", dtSnake, dtSnake+".json")
			data, err := os.ReadFile(jsonPath)
			if err != nil {
				continue
			}
			var docDef map[string]any
			if err := json.Unmarshal(data, &docDef); err != nil {
				continue
			}
			docDef["_app"] = app.Name()
			docDef["_module_dir"] = mod.Name()
			writeJSON(w, http.StatusOK, map[string]any{"data": docDef})
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "doctype not found: " + name})
}

// HandleDeleteDocType removes a DocType directory from disk.
func (h *DevHandler) HandleDeleteDocType(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	// Search and delete
	entries, _ := os.ReadDir(h.appsDir)
	for _, app := range entries {
		if !app.IsDir() {
			continue
		}
		modulesDir := filepath.Join(h.appsDir, app.Name(), "modules")
		modules, _ := os.ReadDir(modulesDir)
		for _, mod := range modules {
			if !mod.IsDir() {
				continue
			}
			dtSnake := toSnakeCaseDev(name)
			dtDir := filepath.Join(modulesDir, mod.Name(), "doctypes", dtSnake)
			if _, err := os.Stat(dtDir); err == nil {
				if err := os.RemoveAll(dtDir); err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
					return
				}
				writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
				return
			}
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "doctype not found: " + name})
}

func buildDocTypeJSON(req DevDocTypeRequest) map[string]any {
	fieldsObj := make(map[string]any, len(req.Fields))
	for name, fd := range req.Fields {
		fieldsObj[name] = fd
	}
	return map[string]any{
		"name":           req.Name,
		"module":         req.Module,
		"layout":         req.Layout,
		"fields":         fieldsObj,
		"naming_rule":    req.Settings.NamingRule,
		"title_field":    req.Settings.TitleField,
		"sort_field":     req.Settings.SortField,
		"sort_order":     req.Settings.SortOrder,
		"search_fields":  req.Settings.SearchFields,
		"image_field":    req.Settings.ImageField,
		"is_submittable": req.Settings.IsSubmittable,
		"is_single":      req.Settings.IsSingle,
		"is_child_table": req.Settings.IsChildTable,
		"is_virtual":     req.Settings.IsVirtual,
		"track_changes":  req.Settings.TrackChanges,
		"permissions":    req.Permissions,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func siteFromContext(r *http.Request) string {
	// Extract site from request context (set by tenant middleware)
	if site, ok := r.Context().Value("site").(string); ok {
		return site
	}
	return "default"
}

func toSnakeCaseDev(s string) string {
	// Reuse meta.TableName logic without the "tab_" prefix
	if s == "" {
		return ""
	}
	tn := meta.TableName(s) // "tab_sales_order"
	return tn[4:]           // "sales_order"
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/osamamuhammed/Moca && go test -run TestDevHandler ./pkg/api/...`
Expected: PASS

- [ ] **Step 5: Register dev routes in ResourceHandler**

In `pkg/api/rest.go`, add dev route registration in `RegisterRoutes`:

```go
func (h *ResourceHandler) RegisterRoutes(mux *http.ServeMux, version string) {
	// ... existing routes ...
	
	// Dev-mode routes are registered separately via DevHandler.RegisterDevRoutes()
}
```

The actual wiring happens at the Gateway level where the DevHandler is created and routes registered on the mux.

- [ ] **Step 6: Commit**

```bash
git add pkg/api/dev_handler.go pkg/api/dev_handler_test.go pkg/api/rest.go
git commit -m "feat(api): add dev-mode DocType CRUD handler"
```

---

### Task 7: Flat-to-Tree Migration Utility

**Files:**
- Create: `pkg/meta/migrate_layout.go`
- Create: `pkg/meta/migrate_layout_test.go`

- [ ] **Step 1: Write migration test**

Create `pkg/meta/migrate_layout_test.go`:

```go
package meta_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

func TestMigrateFile_FlatToTree(t *testing.T) {
	dir := t.TempDir()
	flatJSON := `{
		"name": "Book",
		"module": "Library",
		"fields": [
			{"name": "title", "field_type": "Data", "label": "Title"},
			{"name": "isbn", "field_type": "Data", "label": "ISBN"}
		]
	}`
	path := filepath.Join(dir, "book.json")
	if err := os.WriteFile(path, []byte(flatJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	migrated, err := meta.MigrateFileToTree(path)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !migrated {
		t.Fatal("expected file to be migrated")
	}

	// Re-read and verify tree structure
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if _, ok := result["layout"]; !ok {
		t.Fatal("expected 'layout' key in migrated file")
	}

	// Verify it compiles
	_, err = meta.Compile(data)
	if err != nil {
		t.Fatalf("migrated file should compile: %v", err)
	}
}

func TestMigrateFile_AlreadyTree(t *testing.T) {
	dir := t.TempDir()
	treeJSON := `{
		"name": "Book",
		"module": "Library",
		"layout": {"tabs": [{"label": "D", "sections": [{"columns": [{"width": 1, "fields": ["title"]}]}]}]},
		"fields": {"title": {"field_type": "Data", "label": "Title"}}
	}`
	path := filepath.Join(dir, "book.json")
	if err := os.WriteFile(path, []byte(treeJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	migrated, err := meta.MigrateFileToTree(path)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if migrated {
		t.Fatal("expected file NOT to be migrated (already tree)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/osamamuhammed/Moca && go test -run TestMigrateFile ./pkg/meta/...`
Expected: FAIL

- [ ] **Step 3: Implement migration function**

Create `pkg/meta/migrate_layout.go`:

```go
package meta

import (
	"encoding/json"
	"fmt"
	"os"
)

// MigrateFileToTree reads a DocType JSON file and converts it from flat format
// to tree-native format if needed. Returns true if the file was migrated,
// false if it was already tree-native. The file is overwritten in place.
func MigrateFileToTree(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	// Check if already tree-native
	var probe struct {
		Layout json.RawMessage `json:"layout"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(probe.Layout) > 0 && probe.Layout[0] == '{' {
		return false, nil // already tree-native
	}

	// Parse as flat format
	var flat struct {
		Name          string         `json:"name"`
		Module        string         `json:"module"`
		Label         string         `json:"label,omitempty"`
		Description   string         `json:"description,omitempty"`
		NamingRule    NamingStrategy `json:"naming_rule,omitempty"`
		TitleField    string         `json:"title_field,omitempty"`
		ImageField    string         `json:"image_field,omitempty"`
		SortField     string         `json:"sort_field,omitempty"`
		SortOrder     string         `json:"sort_order,omitempty"`
		SearchFields  []string       `json:"search_fields,omitempty"`
		Fields        []FieldDef     `json:"fields"`
		Permissions   []PermRule     `json:"permissions,omitempty"`
		APIConfig     *APIConfig     `json:"api_config,omitempty"`
		IsSubmittable bool           `json:"is_submittable,omitempty"`
		IsVirtual     bool           `json:"is_virtual,omitempty"`
		IsChildTable  bool           `json:"is_child_table,omitempty"`
		IsSingle      bool           `json:"is_single,omitempty"`
		TrackChanges  bool           `json:"track_changes,omitempty"`
	}
	if err := json.Unmarshal(data, &flat); err != nil {
		return false, fmt.Errorf("parse flat %s: %w", path, err)
	}

	// Convert to tree
	layout, fieldsMap := FlatToTree(flat.Fields)

	// Build output
	out := map[string]any{
		"name":   flat.Name,
		"module": flat.Module,
		"layout": layout,
		"fields": fieldsMap,
	}
	if flat.Label != "" {
		out["label"] = flat.Label
	}
	if flat.Description != "" {
		out["description"] = flat.Description
	}
	if flat.NamingRule.Rule != "" {
		out["naming_rule"] = flat.NamingRule
	}
	if flat.TitleField != "" {
		out["title_field"] = flat.TitleField
	}
	if flat.ImageField != "" {
		out["image_field"] = flat.ImageField
	}
	if flat.SortField != "" {
		out["sort_field"] = flat.SortField
	}
	if flat.SortOrder != "" {
		out["sort_order"] = flat.SortOrder
	}
	if len(flat.SearchFields) > 0 {
		out["search_fields"] = flat.SearchFields
	}
	if len(flat.Permissions) > 0 {
		out["permissions"] = flat.Permissions
	}
	if flat.APIConfig != nil {
		out["api_config"] = flat.APIConfig
	}
	if flat.IsSubmittable {
		out["is_submittable"] = true
	}
	if flat.IsVirtual {
		out["is_virtual"] = true
	}
	if flat.IsChildTable {
		out["is_child_table"] = true
	}
	if flat.IsSingle {
		out["is_single"] = true
	}
	if flat.TrackChanges {
		out["track_changes"] = true
	}

	outBytes, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return false, fmt.Errorf("marshal %s: %w", path, err)
	}
	outBytes = append(outBytes, '\n')

	if err := os.WriteFile(path, outBytes, 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/osamamuhammed/Moca && go test -run TestMigrateFile ./pkg/meta/...`
Expected: PASS

- [ ] **Step 5: Run full test suite**

Run: `cd /Users/osamamuhammed/Moca && go test -race ./pkg/meta/...`
Expected: All tests PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/meta/migrate_layout.go pkg/meta/migrate_layout_test.go
git commit -m "feat(meta): add flat-to-tree JSON migration utility"
```

---

### Task 8: Migrate Existing Builtin JSON Files

**Files:**
- Modify: `pkg/builtin/core/modules/core/doctypes/*//*.json` (all builtin DocType JSONs)

- [ ] **Step 1: Write a quick Go script to migrate all builtins**

This can be done by calling `MigrateFileToTree` on each file. Create a temporary test that does the migration:

```go
func TestMigrate_AllBuiltins(t *testing.T) {
	root := "../../pkg/builtin/core/modules/core/doctypes"
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Skipf("builtin dir not found: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		jsonPath := filepath.Join(root, e.Name(), e.Name()+".json")
		if _, err := os.Stat(jsonPath); err != nil {
			continue
		}
		migrated, err := meta.MigrateFileToTree(jsonPath)
		if err != nil {
			t.Errorf("migrate %s: %v", e.Name(), err)
			continue
		}
		if migrated {
			t.Logf("migrated: %s", e.Name())
		} else {
			t.Logf("already tree-native: %s", e.Name())
		}
		// Verify migrated file compiles
		data, _ := os.ReadFile(jsonPath)
		if _, err := meta.Compile(data); err != nil {
			t.Errorf("compile %s after migration: %v", e.Name(), err)
		}
	}
}
```

- [ ] **Step 2: Run migration**

Run the migration by calling `MigrateFileToTree` on each builtin JSON file. Verify each compiles afterward.

- [ ] **Step 3: Run full test suite to verify no regressions**

Run: `cd /Users/osamamuhammed/Moca && make test`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add pkg/builtin/core/modules/core/doctypes/
git commit -m "chore(meta): migrate builtin DocType JSONs to tree-native format"
```

package meta

// LayoutTree is a tree-native representation of a MetaType form layout.
// It organizes fields into a hierarchy of tabs, sections, and columns.
// The tree is lightweight: columns store field names (not full FieldDefs),
// while a companion map[string]FieldDef holds the actual definitions.
type LayoutTree struct {
	Tabs []TabDef `json:"tabs"`
}

// TabDef represents a single tab in a form layout.
type TabDef struct {
	Label    string       `json:"label"`
	Sections []SectionDef `json:"sections"`
}

// SectionDef represents a section within a tab. Sections can be collapsible
// and optionally collapsed by default.
type SectionDef struct {
	Label              string      `json:"label,omitempty"`
	Columns            []ColumnDef `json:"columns"`
	Collapsible        bool        `json:"collapsible,omitempty"`
	CollapsedByDefault bool        `json:"collapsed_by_default,omitempty"`
}

// ColumnDef represents a column within a section. Width is a grid span
// hint (e.g., 6 for half-width in a 12-column grid). Fields contains
// field names in display order.
type ColumnDef struct {
	Fields []string `json:"fields"`
	Width  int      `json:"width,omitempty"`
}

// breakFieldType returns true for the three structural break types that
// drive layout nesting but are never placed into columns.
func breakFieldType(ft FieldType) bool {
	return ft == FieldTypeTabBreak || ft == FieldTypeSectionBreak || ft == FieldTypeColumnBreak
}

// breakLabel resolves the label for a break field. LayoutHint.Label takes
// priority; field.Label is the fallback. Matches the TS parser's
// `field.layout_label || field.label` pattern.
func breakLabel(f FieldDef) string {
	if f.LayoutHint.Label != "" {
		return f.LayoutHint.Label
	}
	return f.Label
}

// FlatToTree converts a flat, delimiter-based field list into a nested
// LayoutTree plus a map of field name to FieldDef (excluding break fields).
//
// The algorithm matches desk/src/utils/layoutParser.ts:
//   - TabBreak   -> finalize current hierarchy, start new tab
//   - SectionBreak -> finalize current section, start new section
//   - ColumnBreak  -> finalize current column, start new column
//   - Other        -> add field name to current column and field to map
//
// If no TabBreak is encountered, a default "Details" tab is used.
// At least one tab is always returned, even for empty input.
func FlatToTree(fields []FieldDef) (*LayoutTree, map[string]FieldDef) {
	fieldsMap := make(map[string]FieldDef, len(fields))

	if len(fields) == 0 {
		tree, _ := DefaultLayout(nil)
		return tree, fieldsMap
	}

	tabs := make([]TabDef, 0, 4)
	currentTab := makeTabDef("Details")
	currentSection := &currentTab.Sections[0]
	currentColumn := &currentSection.Columns[0]

	for _, field := range fields {
		switch field.FieldType {
		case FieldTypeTabBreak:
			// Finalize current tab and start a new one.
			tabs = append(tabs, currentTab)
			currentTab = makeTabDef(breakLabel(field))
			currentSection = &currentTab.Sections[0]
			currentColumn = &currentSection.Columns[0]

		case FieldTypeSectionBreak:
			// Finalize current section; start a new one within the current tab.
			newSection := SectionDef{
				Label:              breakLabel(field),
				Collapsible:        field.LayoutHint.Collapsible,
				CollapsedByDefault: field.LayoutHint.CollapsedByDefault,
				Columns:            []ColumnDef{makeColumnDef()},
			}
			currentTab.Sections = append(currentTab.Sections, newSection)
			currentSection = &currentTab.Sections[len(currentTab.Sections)-1]
			currentColumn = &currentSection.Columns[0]

		case FieldTypeColumnBreak:
			// Start a new column within the current section.
			currentSection.Columns = append(currentSection.Columns, makeColumnDef())
			currentColumn = &currentSection.Columns[len(currentSection.Columns)-1]

		default:
			// Data or non-break layout field -> add to current column.
			currentColumn.Fields = append(currentColumn.Fields, field.Name)
			fieldsMap[field.Name] = field
		}
	}

	// Finalize the last tab.
	tabs = append(tabs, currentTab)

	// Cleanup pass: match TS parser behavior.
	// 1 & 2: Remove empty columns and empty unlabeled sections.
	cleanupTabs(tabs)

	// 3: Remove completely empty tabs, keeping at least the first one.
	nonEmpty := make([]TabDef, 0, len(tabs))
	for _, tab := range tabs {
		if tabHasFields(tab) {
			nonEmpty = append(nonEmpty, tab)
		}
	}
	if len(nonEmpty) > 0 {
		tabs = nonEmpty
	} else {
		tabs = tabs[:1]
	}

	tree := &LayoutTree{Tabs: tabs}
	return tree, fieldsMap
}

// DefaultLayout wraps all fields into a single "Details" tab with one
// section and one column. Only non-break fields are included.
func DefaultLayout(fields []FieldDef) (*LayoutTree, map[string]FieldDef) {
	fieldsMap := make(map[string]FieldDef, len(fields))
	names := make([]string, 0, len(fields))

	for _, f := range fields {
		if !breakFieldType(f.FieldType) {
			names = append(names, f.Name)
			fieldsMap[f.Name] = f
		}
	}

	tree := &LayoutTree{
		Tabs: []TabDef{
			{
				Label: "Details",
				Sections: []SectionDef{
					{
						Columns: []ColumnDef{
							{Fields: names},
						},
					},
				},
			},
		},
	}
	return tree, fieldsMap
}

// ExtractFieldsOrdered walks a LayoutTree in tab -> section -> column -> field
// order and returns a flat slice of FieldDefs. Fields present in the tree but
// missing from fieldsMap are silently skipped.
func ExtractFieldsOrdered(layout *LayoutTree, fieldsMap map[string]FieldDef) []FieldDef {
	var result []FieldDef
	for _, tab := range layout.Tabs {
		for _, sec := range tab.Sections {
			for _, col := range sec.Columns {
				for _, name := range col.Fields {
					if fd, ok := fieldsMap[name]; ok {
						result = append(result, fd)
					}
				}
			}
		}
	}
	return result
}

// ── internal helpers ────────────────────────────────────────────────────

func makeTabDef(label string) TabDef {
	return TabDef{
		Label: label,
		Sections: []SectionDef{
			{Columns: []ColumnDef{makeColumnDef()}},
		},
	}
}

func makeColumnDef() ColumnDef {
	return ColumnDef{Fields: []string{}}
}

// columnHasFields reports whether a column contains at least one field.
func columnHasFields(col ColumnDef) bool {
	return len(col.Fields) > 0
}

// sectionHasFields reports whether a section has at least one non-empty column.
func sectionHasFields(sec SectionDef) bool {
	for _, col := range sec.Columns {
		if columnHasFields(col) {
			return true
		}
	}
	return false
}

// tabHasFields reports whether a tab has at least one section with fields.
func tabHasFields(tab TabDef) bool {
	for _, sec := range tab.Sections {
		if sectionHasFields(sec) {
			return true
		}
	}
	return false
}

// cleanupTabs removes empty columns, empty sections, and empty tabs,
// matching the TS parser's cleanup logic.
func cleanupTabs(tabs []TabDef) {
	for i := range tabs {
		// Remove empty columns within each section, keeping at least one.
		for j := range tabs[i].Sections {
			nonEmpty := make([]ColumnDef, 0, len(tabs[i].Sections[j].Columns))
			for _, col := range tabs[i].Sections[j].Columns {
				if columnHasFields(col) {
					nonEmpty = append(nonEmpty, col)
				}
			}
			if len(nonEmpty) > 0 {
				tabs[i].Sections[j].Columns = nonEmpty
			}
			// If all columns were empty, keep the original (at least one).
		}

		// Remove empty sections that have no label and no fields.
		meaningful := make([]SectionDef, 0, len(tabs[i].Sections))
		for _, sec := range tabs[i].Sections {
			if sectionHasFields(sec) || sec.Label != "" {
				meaningful = append(meaningful, sec)
			}
		}
		if len(meaningful) > 0 {
			tabs[i].Sections = meaningful
		}
		// If all sections were empty/unlabeled, keep the original.
	}
}

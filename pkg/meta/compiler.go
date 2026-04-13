package meta

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// CompileError represents a single validation failure found during MetaType compilation.
// Field is a dot-path to the offending element (e.g., "fields[2].field_type").
// Message describes the violation (e.g., `invalid field type: "FakeType"`).
type CompileError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *CompileError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// CompileErrors is returned by Compile when one or more validation rules fail.
// All rules are checked before returning, so the slice may contain multiple errors.
// Use errors.As to retrieve it from the returned error value.
type CompileErrors struct {
	Errors []CompileError
}

// Error implements the error interface. It formats all accumulated errors.
func (e *CompileErrors) Error() string {
	if len(e.Errors) == 1 {
		return fmt.Sprintf("compile: 1 validation error: %s", e.Errors[0].Error())
	}
	var b strings.Builder
	fmt.Fprintf(&b, "compile: %d validation errors:", len(e.Errors))
	for _, ce := range e.Errors {
		fmt.Fprintf(&b, "\n  - %s", ce.Error())
	}
	return b.String()
}

// validNamingRules is the set of recognized NamingRule values for validation.
var validNamingRules = map[NamingRule]bool{
	NamingAutoIncrement: true,
	NamingByPattern:     true,
	NamingByField:       true,
	NamingByHash:        true,
	NamingUUID:          true,
	NamingCustom:        true,
}

// standardColumns are the column names present in every document table.
// SortField may reference these in addition to user-defined field names.
// See MOCA_SYSTEM_DESIGN.md section 4.3 for the full column list.
var standardColumns = map[string]bool{
	"name":           true,
	"owner":          true,
	"creation":       true,
	"modified":       true,
	"modified_by":    true,
	"docstatus":      true,
	"idx":            true,
	"workflow_state": true,
}

// Compile parses JSON bytes into a MetaType, validates against all business
// rules, and returns the compiled result. It auto-detects the input format:
//
//   - Tree-native: "layout" key present with an object value — fields are a
//     map[string]FieldDef keyed by field name.
//   - Legacy flat: "fields" is a JSON array of FieldDef objects.
//
// After compilation ALL THREE representations are always populated:
//   - Fields []FieldDef — flat ordered list (used by DDL, validation, API, search)
//   - Layout *LayoutTree — tree structure
//   - FieldsMap map[string]FieldDef — fields keyed by name
//
// Validation errors are accumulated (not short-circuited): all rules are
// checked even after the first failure.
//
// Returns *CompileErrors (retrievable via errors.As) if any validation rules
// fail. Returns a plain wrapped error if the input is not valid JSON.
func Compile(jsonBytes []byte) (*MetaType, error) {
	// Detect format by probing the JSON structure.
	var probe struct {
		Layout json.RawMessage `json:"layout"`
	}
	if err := json.Unmarshal(jsonBytes, &probe); err != nil {
		return nil, fmt.Errorf("compile: invalid JSON: %w", err)
	}

	// Tree-native: "layout" key exists and starts with '{'.
	if len(probe.Layout) > 0 && probe.Layout[0] == '{' {
		return compileTreeNative(jsonBytes)
	}
	return compileFlatLegacy(jsonBytes)
}

// compileFlatLegacy handles the original flat format where "fields" is a
// JSON array. After validation it derives Layout and FieldsMap from the
// flat field list via FlatToTree.
func compileFlatLegacy(jsonBytes []byte) (*MetaType, error) {
	var mt MetaType
	if err := json.Unmarshal(jsonBytes, &mt); err != nil {
		return nil, fmt.Errorf("compile: invalid JSON: %w", err)
	}

	// Default InAPI and run 12 validation rules.
	if errs := validateAndDefault(&mt); len(errs) > 0 {
		return nil, &CompileErrors{Errors: errs}
	}

	// Derive tree layout and fields map from flat fields.
	mt.Layout, mt.FieldsMap = FlatToTree(mt.Fields)

	return &mt, nil
}

// compileTreeNative handles the tree-native format where "layout" is a
// LayoutTree object and "fields" is a map[string]FieldDef (keyed by name).
func compileTreeNative(jsonBytes []byte) (*MetaType, error) {
	// Step 1: Parse raw JSON to separate layout, fields map, and everything else.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		return nil, fmt.Errorf("compile: invalid JSON: %w", err)
	}

	rawLayout, hasLayout := raw["layout"]
	rawFields, hasFields := raw["fields"]

	// Remove layout and fields so the remainder can be unmarshalled into MetaType.
	delete(raw, "layout")
	delete(raw, "fields")

	// Step 2: Re-marshal the remainder and unmarshal into MetaType to populate
	// all scalar fields (Name, Module, NamingRule, etc.).
	remainder, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("compile: marshal remainder: %w", err)
	}
	var mt MetaType
	if err := json.Unmarshal(remainder, &mt); err != nil {
		return nil, fmt.Errorf("compile: invalid JSON: %w", err)
	}

	// Step 3: Parse layout.
	if hasLayout {
		var layout LayoutTree
		if err := json.Unmarshal(rawLayout, &layout); err != nil {
			return nil, fmt.Errorf("compile: invalid layout JSON: %w", err)
		}
		mt.Layout = &layout
	}

	// Step 4: Parse fields map. Each field's Name is set from the map key.
	mt.FieldsMap = make(map[string]FieldDef)
	if hasFields {
		var fieldsRaw map[string]json.RawMessage
		if err := json.Unmarshal(rawFields, &fieldsRaw); err != nil {
			return nil, fmt.Errorf("compile: invalid fields JSON (expected object): %w", err)
		}
		for key, fieldJSON := range fieldsRaw {
			var fd FieldDef
			if err := json.Unmarshal(fieldJSON, &fd); err != nil {
				return nil, fmt.Errorf("compile: invalid field %q: %w", key, err)
			}
			fd.Name = key
			mt.FieldsMap[key] = fd
		}
	}

	// Step 5: Derive flat field list from layout tree.
	if mt.Layout != nil {
		mt.Fields = ExtractFieldsOrdered(mt.Layout, mt.FieldsMap)
	}

	// Step 6: Default InAPI and run 12 validation rules (shared with flat path).
	errs := validateAndDefault(&mt)

	// Sync FieldsMap with changes made by validateAndDefault (e.g., InAPI defaults).
	for i := range mt.Fields {
		mt.FieldsMap[mt.Fields[i].Name] = mt.Fields[i]
	}

	// Step 7: Layout-specific validation.
	errs = append(errs, validateLayout(mt.Layout)...)

	if len(errs) > 0 {
		return nil, &CompileErrors{Errors: errs}
	}
	return &mt, nil
}

// validateAndDefault applies InAPI defaulting for storage fields and runs
// the 12 standard validation rules. It returns all accumulated errors.
func validateAndDefault(mt *MetaType) []CompileError {
	var errs []CompileError
	add := func(field, message string) {
		errs = append(errs, CompileError{Field: field, Message: message})
	}

	// Rule 1: Name required.
	if mt.Name == "" {
		add("name", "required")
	}

	// Rule 2: Module required.
	if mt.Module == "" {
		add("module", "required")
	}

	// Default InAPI to true for storage (non-layout) fields only when the
	// input JSON omitted in_api. An explicit false must be preserved.
	for i := range mt.Fields {
		if mt.Fields[i].FieldType.IsStorable() && !mt.Fields[i].inAPIPresent {
			mt.Fields[i].InAPI = true
		}
	}

	// Build a set of valid field names during field walk for use in rules 7–9, 11.
	fieldNames := make(map[string]bool, len(mt.Fields))

	// Rules 3–6: Per-field validations.
	for i, f := range mt.Fields {
		// Rule 3: FieldType must be one of the 35 recognized values.
		if !f.FieldType.IsValid() {
			add(fmt.Sprintf("fields[%d].field_type", i),
				fmt.Sprintf("invalid field type: %q", f.FieldType))
		}

		// Rule 4: No duplicate field names.
		if fieldNames[f.Name] {
			add(fmt.Sprintf("fields[%d].name", i),
				fmt.Sprintf("duplicate field name: %q", f.Name))
		} else {
			// Register only the first occurrence; duplicates will not shadow it.
			fieldNames[f.Name] = true
		}

		// Rule 5: Link and DynamicLink require non-empty Options (target DocType).
		if (f.FieldType == FieldTypeLink || f.FieldType == FieldTypeDynamicLink) && f.Options == "" {
			add(fmt.Sprintf("fields[%d].options", i),
				fmt.Sprintf("required for %s field type", f.FieldType))
		}

		// Rule 6: Table and TableMultiSelect require non-empty Options (child DocType).
		if (f.FieldType == FieldTypeTable || f.FieldType == FieldTypeTableMultiSelect) && f.Options == "" {
			add(fmt.Sprintf("fields[%d].options", i),
				fmt.Sprintf("required for %s field type", f.FieldType))
		}
	}

	// Rule 7: SearchFields must reference existing field names.
	for _, sf := range mt.SearchFields {
		if !fieldNames[sf] {
			add("search_fields", fmt.Sprintf("references unknown field: %q", sf))
		}
	}

	// Rule 8: TitleField must reference an existing field name.
	if mt.TitleField != "" && !fieldNames[mt.TitleField] {
		add("title_field", fmt.Sprintf("references unknown field: %q", mt.TitleField))
	}

	// Rule 9: SortField must reference an existing field name or a standard column.
	if mt.SortField != "" && !fieldNames[mt.SortField] && !standardColumns[mt.SortField] {
		add("sort_field", fmt.Sprintf("references unknown field: %q", mt.SortField))
	}

	// Rule 10: NamingRule.Rule must be valid; empty defaults to UUID.
	if mt.NamingRule.Rule == "" {
		mt.NamingRule.Rule = NamingUUID
	} else if !validNamingRules[mt.NamingRule.Rule] {
		add("naming_rule.rule", fmt.Sprintf("invalid naming rule: %q", mt.NamingRule.Rule))
	}

	// Rules 11–12 only apply when the naming rule is valid (avoid cascading errors).
	if validNamingRules[mt.NamingRule.Rule] {
		// Rule 11: NamingByField requires a non-empty FieldName that exists in Fields.
		if mt.NamingRule.Rule == NamingByField {
			if mt.NamingRule.FieldName == "" {
				add("naming_rule.field_name", `required when rule is "field"`)
			} else if !fieldNames[mt.NamingRule.FieldName] {
				add("naming_rule.field_name",
					fmt.Sprintf("references unknown field: %q", mt.NamingRule.FieldName))
			}
		}

		// Rule 12: NamingByPattern requires a non-empty Pattern.
		if mt.NamingRule.Rule == NamingByPattern && mt.NamingRule.Pattern == "" {
			add("naming_rule.pattern", `required when rule is "pattern"`)
		}
	}

	return errs
}

// validateLayout checks layout-specific constraints that only apply to
// tree-native input. Returns errors for empty tabs, missing sections,
// and invalid column widths.
func validateLayout(layout *LayoutTree) []CompileError {
	if layout == nil {
		return nil
	}
	var errs []CompileError
	add := func(field, message string) {
		errs = append(errs, CompileError{Field: field, Message: message})
	}

	if len(layout.Tabs) == 0 {
		add("layout.tabs", "at least one tab is required")
	}

	for i, tab := range layout.Tabs {
		if len(tab.Sections) == 0 {
			add(fmt.Sprintf("layout.tabs[%d].sections", i),
				"at least one section is required per tab")
		}
		for j, sec := range tab.Sections {
			for k, col := range sec.Columns {
				if col.Width < 0 {
					add(fmt.Sprintf("layout.tabs[%d].sections[%d].columns[%d].width", i, j, k),
						fmt.Sprintf("column width must not be negative: %d", col.Width))
				}
			}
		}
	}

	return errs
}

// TableName returns the PostgreSQL table name for a MetaType.
// It converts the PascalCase or camelCase doctype name to snake_case
// and prefixes with "tab_".
//
// Examples:
//
//	"SalesOrder"   → "tab_sales_order"
//	"HTTPConfig"   → "tab_http_config"
//	"XMLParser"    → "tab_xml_parser"
func TableName(doctypeName string) string {
	return "tab_" + toSnakeCase(doctypeName)
}

// toSnakeCase converts a PascalCase or camelCase string to snake_case.
// It correctly handles consecutive uppercase sequences (acronyms) by
// inserting an underscore only at the acronym-to-word boundary.
//
// Examples:
//
//	"SalesOrder"     → "sales_order"
//	"HTTPConfig"     → "http_config"
//	"XMLParser"      → "xml_parser"
//	"userID"         → "user_id"
//	"getHTTPSClient" → "get_https_client"
func toSnakeCase(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s) + 4) // pre-allocate with room for a few underscores

	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := runes[i-1]
				if prev != '_' && prev != ' ' && prev != '-' {
					if unicode.IsUpper(prev) {
						// We are inside a consecutive uppercase run (e.g., "HTTP").
						// Insert underscore only when this uppercase letter begins a
						// new word — i.e., when the next character is lowercase.
						// "HTTPConfig": at 'C', prev='P' (upper), next='o' (lower) → insert '_'
						// "HTTP" at end: no next character → do not insert '_'
						if i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
							b.WriteRune('_')
						}
					} else {
						// Previous char is a lowercase letter or digit: always insert '_'.
						b.WriteRune('_')
					}
				}
			}
			b.WriteRune(unicode.ToLower(r))
		} else if r == ' ' || r == '-' {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

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
// rules, and returns the compiled result. Validation errors are accumulated
// (not short-circuited): all 12 rules are checked even after the first failure.
//
// Returns *CompileErrors (retrievable via errors.As) if any validation rules
// fail. Returns a plain wrapped error if the input is not valid JSON.
func Compile(jsonBytes []byte) (*MetaType, error) {
	var mt MetaType
	if err := json.Unmarshal(jsonBytes, &mt); err != nil {
		return nil, fmt.Errorf("compile: invalid JSON: %w", err)
	}

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

	// Default InAPI to true for storage (non-layout) fields. JSON cannot
	// distinguish "not set" from "set to false" for booleans, so we apply
	// the default unconditionally. Layout-only fields are never part of the
	// API response, so they stay false.
	for i := range mt.Fields {
		if mt.Fields[i].FieldType.IsStorable() {
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

	if len(errs) > 0 {
		return nil, &CompileErrors{Errors: errs}
	}
	return &mt, nil
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
				if prev != '_' {
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
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

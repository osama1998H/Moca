package meta

// FieldType identifies the data type and rendering behavior of a field.
// There are 29 storage types (persisted as database columns) and 6 layout-only
// types (used for UI form layout only, never stored in the database).
type FieldType string

// Storage field types. Each maps to a specific PostgreSQL column type.
// See pkg/meta/columns.go (MS-03-T3) for the full FieldType -> column type mapping.
const (
	FieldTypeData             FieldType = "Data"
	FieldTypeText             FieldType = "Text"
	FieldTypeLongText         FieldType = "LongText"
	FieldTypeMarkdown         FieldType = "Markdown"
	FieldTypeCode             FieldType = "Code"
	FieldTypeInt              FieldType = "Int"
	FieldTypeFloat            FieldType = "Float"
	FieldTypeCurrency         FieldType = "Currency"
	FieldTypePercent          FieldType = "Percent"
	FieldTypeDate             FieldType = "Date"
	FieldTypeDatetime         FieldType = "Datetime"
	FieldTypeTime             FieldType = "Time"
	FieldTypeDuration         FieldType = "Duration"
	FieldTypeSelect           FieldType = "Select"
	FieldTypeLink             FieldType = "Link"
	FieldTypeDynamicLink      FieldType = "DynamicLink"
	FieldTypeTable            FieldType = "Table"            // Child table (no column; separate table)
	FieldTypeTableMultiSelect FieldType = "TableMultiSelect" // Child table for multi-select
	FieldTypeAttach           FieldType = "Attach"
	FieldTypeAttachImage      FieldType = "AttachImage"
	FieldTypeCheck            FieldType = "Check" // Boolean
	FieldTypeColor            FieldType = "Color"
	FieldTypeGeolocation      FieldType = "Geolocation"
	FieldTypeJSON             FieldType = "JSON"
	FieldTypePassword         FieldType = "Password"
	FieldTypeRating           FieldType = "Rating"
	FieldTypeSignature        FieldType = "Signature"
	FieldTypeBarcode          FieldType = "Barcode"
	FieldTypeHTMLEditor       FieldType = "HTMLEditor"

	// Layout-only types. These control UI form rendering and are never
	// stored as database columns. IsStorable() returns false for all of them.
	FieldTypeSectionBreak FieldType = "SectionBreak"
	FieldTypeColumnBreak  FieldType = "ColumnBreak"
	FieldTypeTabBreak     FieldType = "TabBreak"
	FieldTypeHTML         FieldType = "HTML" // Static HTML content in form
	FieldTypeButton       FieldType = "Button"
	FieldTypeHeading      FieldType = "Heading"
)

// ValidFieldTypes is a lookup table for all 35 recognized FieldType values.
// Use FieldType.IsValid() for a more convenient boolean check.
var ValidFieldTypes = map[FieldType]bool{
	FieldTypeData:             true,
	FieldTypeText:             true,
	FieldTypeLongText:         true,
	FieldTypeMarkdown:         true,
	FieldTypeCode:             true,
	FieldTypeInt:              true,
	FieldTypeFloat:            true,
	FieldTypeCurrency:         true,
	FieldTypePercent:          true,
	FieldTypeDate:             true,
	FieldTypeDatetime:         true,
	FieldTypeTime:             true,
	FieldTypeDuration:         true,
	FieldTypeSelect:           true,
	FieldTypeLink:             true,
	FieldTypeDynamicLink:      true,
	FieldTypeTable:            true,
	FieldTypeTableMultiSelect: true,
	FieldTypeAttach:           true,
	FieldTypeAttachImage:      true,
	FieldTypeCheck:            true,
	FieldTypeColor:            true,
	FieldTypeGeolocation:      true,
	FieldTypeJSON:             true,
	FieldTypePassword:         true,
	FieldTypeRating:           true,
	FieldTypeSignature:        true,
	FieldTypeBarcode:          true,
	FieldTypeHTMLEditor:       true,
	FieldTypeSectionBreak:     true,
	FieldTypeColumnBreak:      true,
	FieldTypeTabBreak:         true,
	FieldTypeHTML:             true,
	FieldTypeButton:           true,
	FieldTypeHeading:          true,
}

// layoutTypes is the unexported set of layout-only FieldTypes.
// Used by IsStorable to decide whether a field produces a database column.
var layoutTypes = map[FieldType]struct{}{
	FieldTypeSectionBreak: {},
	FieldTypeColumnBreak:  {},
	FieldTypeTabBreak:     {},
	FieldTypeHTML:         {},
	FieldTypeButton:       {},
	FieldTypeHeading:      {},
}

// IsValid reports whether ft is one of the 35 recognized FieldType values.
func (ft FieldType) IsValid() bool {
	return ValidFieldTypes[ft]
}

// IsStorable reports whether ft represents a type that is persisted as a
// database column. Returns true for the 29 storage types and false for the
// 6 layout-only types (SectionBreak, ColumnBreak, TabBreak, HTML, Button, Heading).
// Also returns false for unknown or empty FieldType values.
func (ft FieldType) IsStorable() bool {
	if !ft.IsValid() {
		return false
	}
	_, isLayout := layoutTypes[ft]
	return !isLayout
}

// FieldDef defines a single field within a MetaType schema, including its
// storage type, validation rules, API exposure settings, and UI layout hints.
type FieldDef struct {
	LayoutHint         LayoutHint `json:"layout_hint"`
	Default            any        `json:"default,omitempty"`
	MaxValue           *float64   `json:"max_value,omitempty"`
	MinValue           *float64   `json:"min_value,omitempty"`
	APIAlias           string     `json:"api_alias"`
	Width              string     `json:"width,omitempty"`
	CustomValidator    string     `json:"custom_validator,omitempty"`
	Name               string     `json:"name"`
	Options            string     `json:"options"`
	DependsOn          string     `json:"depends_on"`
	MandatoryDependsOn string     `json:"mandatory_depends_on"`
	FieldType          FieldType  `json:"field_type"`
	Label              string     `json:"label"`
	ValidationRegex    string     `json:"validation_regex,omitempty"`
	MaxLength          int        `json:"max_length,omitempty"`
	Hidden             bool       `json:"hidden"`
	InAPI              bool       `json:"in_api"`
	APIReadOnly        bool       `json:"api_read_only"`
	ReadOnly           bool       `json:"read_only"`
	Searchable         bool       `json:"searchable"`
	Filterable         bool       `json:"filterable"`
	InListView         bool       `json:"in_list_view"`
	InFilter           bool       `json:"in_filter"`
	InPreview          bool       `json:"in_preview"`
	Unique             bool       `json:"unique"`
	Required           bool       `json:"required"`
	DBIndex            bool       `json:"db_index"`
	FullTextIndex      bool       `json:"full_text_index"`
}

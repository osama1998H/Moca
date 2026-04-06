package meta

// ColumnType returns the PostgreSQL column type string for a FieldType.
// Returns an empty string for layout-only types (SectionBreak, ColumnBreak, etc.),
// Table/TableMultiSelect (which produce separate child tables, not columns),
// and unrecognized or empty FieldType values.
//
// Callers should skip column generation when ColumnType returns "":
//
//	colType := ColumnType(f.FieldType)
//	if colType == "" {
//	    continue
//	}
func ColumnType(ft FieldType) string {
	switch ft {
	case FieldTypeData, FieldTypeText, FieldTypeLongText, FieldTypeMarkdown,
		FieldTypeCode, FieldTypeHTMLEditor, FieldTypeSelect, FieldTypeColor,
		FieldTypeBarcode, FieldTypeSignature, FieldTypePassword,
		FieldTypeLink, FieldTypeDynamicLink,
		FieldTypeAttach, FieldTypeAttachImage:
		return "TEXT"

	case FieldTypeInt:
		return "INTEGER"

	case FieldTypeFloat, FieldTypeCurrency, FieldTypePercent,
		FieldTypeRating, FieldTypeDuration:
		return "NUMERIC(18,6)"

	case FieldTypeDate:
		return "DATE"

	case FieldTypeDatetime:
		return "TIMESTAMPTZ"

	case FieldTypeTime:
		return "TIME"

	case FieldTypeCheck:
		return "BOOLEAN"

	case FieldTypeJSON, FieldTypeGeolocation:
		return "JSONB"

	// Table and TableMultiSelect produce separate child tables, never a column.
	case FieldTypeTable, FieldTypeTableMultiSelect:
		return ""

	// Layout-only types produce no column.
	case FieldTypeSectionBreak, FieldTypeColumnBreak, FieldTypeTabBreak,
		FieldTypeHTML, FieldTypeButton, FieldTypeHeading:
		return ""

	default:
		// Custom field types are stored as TEXT columns.
		if ft.IsCustom() {
			return "TEXT"
		}
		return ""
	}
}

// StandardColumnDef describes a standard column that is present on every
// document table regardless of its MetaType definition.
type StandardColumnDef struct {
	// Name is the unquoted column name (e.g. "owner").
	Name string
	// DDL is the full inline column definition used inside CREATE TABLE,
	// including type and constraints (e.g. "TEXT NOT NULL").
	DDL string
}

// StandardColumns returns the 13 standard columns for regular document tables
// in definition order. These columns are always present before and after the
// user-defined field columns generated from a MetaType's Fields list.
//
// See MOCA_SYSTEM_DESIGN.md section 4.3 (lines 898-923) for the canonical list.
func StandardColumns() []StandardColumnDef {
	return []StandardColumnDef{
		{Name: "name", DDL: "TEXT PRIMARY KEY"},
		{Name: "owner", DDL: "TEXT NOT NULL"},
		{Name: "creation", DDL: "TIMESTAMPTZ NOT NULL DEFAULT NOW()"},
		{Name: "modified", DDL: "TIMESTAMPTZ NOT NULL DEFAULT NOW()"},
		{Name: "modified_by", DDL: "TEXT NOT NULL"},
		{Name: "docstatus", DDL: "SMALLINT NOT NULL DEFAULT 0"},
		{Name: "idx", DDL: "INTEGER NOT NULL DEFAULT 0"},
		{Name: "workflow_state", DDL: "TEXT"},
		// User-defined field columns are inserted at this position by GenerateTableDDL.
		{Name: "_extra", DDL: "JSONB NOT NULL DEFAULT '{}'"},
		{Name: "_user_tags", DDL: "TEXT"},
		{Name: "_comments", DDL: "TEXT"},
		{Name: "_assign", DDL: "TEXT"},
		{Name: "_liked_by", DDL: "TEXT"},
	}
}

// ChildStandardColumns returns the 10 standard columns for child document tables
// (MetaType.IsChildTable == true). Child tables add parent/parenttype/parentfield
// and omit docstatus, workflow_state, _user_tags, _comments, _assign, _liked_by.
//
// See MOCA_SYSTEM_DESIGN.md section 4.3 (lines 927-944) for the canonical list.
func ChildStandardColumns() []StandardColumnDef {
	return []StandardColumnDef{
		{Name: "name", DDL: "TEXT PRIMARY KEY"},
		{Name: "parent", DDL: "TEXT NOT NULL"},
		{Name: "parenttype", DDL: "TEXT NOT NULL"},
		{Name: "parentfield", DDL: "TEXT NOT NULL"},
		{Name: "idx", DDL: "INTEGER NOT NULL DEFAULT 0"},
		{Name: "owner", DDL: "TEXT NOT NULL"},
		{Name: "creation", DDL: "TIMESTAMPTZ NOT NULL DEFAULT NOW()"},
		{Name: "modified", DDL: "TIMESTAMPTZ NOT NULL DEFAULT NOW()"},
		{Name: "modified_by", DDL: "TEXT NOT NULL"},
		// User-defined field columns are inserted at this position by GenerateTableDDL.
		{Name: "_extra", DDL: "JSONB NOT NULL DEFAULT '{}'"},
	}
}

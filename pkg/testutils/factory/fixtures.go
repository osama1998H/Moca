package factory

import (
	"fmt"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
)

// EmptyListOptions returns a ListOptions with default values, useful for
// list operations in tests that need all documents.
func EmptyListOptions() document.ListOptions {
	return document.ListOptions{Limit: 100}
}

// SimpleDocType returns a small API-enabled doctype for use in tests.
func SimpleDocType(doctype string) *meta.MetaType {
	return &meta.MetaType{
		Name:   doctype,
		Module: "test",
		NamingRule: meta.NamingStrategy{
			Rule: meta.NamingUUID,
		},
		TitleField:   "customer_name",
		SortField:    "modified",
		SortOrder:    "DESC",
		SearchFields: []string{"customer_name", "external_ref"},
		APIConfig: &meta.APIConfig{
			Enabled:       true,
			AllowGet:      true,
			AllowCreate:   true,
			AllowUpdate:   true,
			AllowDelete:   true,
			AllowList:     true,
			DefaultFields: []string{"name", "customer_name", "status", "grand_total"},
			AlwaysInclude: []string{"name"},
		},
		Fields: []meta.FieldDef{
			{Name: "customer_name", Label: "Customer", FieldType: meta.FieldTypeData, Required: true, InAPI: true, Filterable: true, Searchable: true},
			{Name: "status", Label: "Status", FieldType: meta.FieldTypeData, Required: true, InAPI: true, Filterable: true},
			{Name: "grand_total", Label: "Grand Total", FieldType: meta.FieldTypeCurrency, InAPI: true, Filterable: true},
			{Name: "notes", Label: "Notes", FieldType: meta.FieldTypeLongText, InAPI: true},
			{Name: "external_ref", Label: "External Ref", FieldType: meta.FieldTypeData, InAPI: true, Searchable: true},
		},
	}
}

// ChildDocType returns a child-table doctype for use in tests.
func ChildDocType(doctype string) *meta.MetaType {
	return &meta.MetaType{
		Name:         doctype,
		Module:       "test",
		IsChildTable: true,
		NamingRule: meta.NamingStrategy{
			Rule: meta.NamingUUID,
		},
		Fields: []meta.FieldDef{
			{Name: "item_code", Label: "Item Code", FieldType: meta.FieldTypeData, Required: true, InAPI: true},
			{Name: "qty", Label: "Qty", FieldType: meta.FieldTypeInt, Required: true, InAPI: true},
			{Name: "rate", Label: "Rate", FieldType: meta.FieldTypeCurrency, Required: true, InAPI: true},
			{Name: "amount", Label: "Amount", FieldType: meta.FieldTypeCurrency, Required: true, InAPI: true},
		},
	}
}

// ComplexDocType returns a 50-field parent doctype with a child table field.
func ComplexDocType(doctype, childDocType string) *meta.MetaType {
	fields := []meta.FieldDef{
		{Name: "customer_name", Label: "Customer", FieldType: meta.FieldTypeData, Required: true, InAPI: true, Filterable: true},
		{Name: "status", Label: "Status", FieldType: meta.FieldTypeData, Required: true, InAPI: true, Filterable: true},
		{Name: "grand_total", Label: "Grand Total", FieldType: meta.FieldTypeCurrency, InAPI: true},
	}

	for i := 1; len(fields) < 49; i++ {
		name := fmt.Sprintf("field_%02d", i)
		field := meta.FieldDef{
			Name:      name,
			Label:     fmt.Sprintf("Field %02d", i),
			InAPI:     true,
			FieldType: meta.FieldTypeData,
		}
		switch i % 3 {
		case 1:
			field.FieldType = meta.FieldTypeData
		case 2:
			field.FieldType = meta.FieldTypeInt
		default:
			field.FieldType = meta.FieldTypeCheck
		}
		fields = append(fields, field)
	}

	fields = append(fields, meta.FieldDef{
		Name:      "items",
		Label:     "Items",
		FieldType: meta.FieldTypeTable,
		Options:   childDocType,
		InAPI:     true,
	})

	return &meta.MetaType{
		Name:   doctype,
		Module: "test",
		NamingRule: meta.NamingStrategy{
			Rule: meta.NamingUUID,
		},
		TitleField:   "customer_name",
		SortField:    "modified",
		SortOrder:    "DESC",
		SearchFields: []string{"customer_name"},
		APIConfig: &meta.APIConfig{
			Enabled:       true,
			AllowGet:      true,
			AllowCreate:   true,
			AllowUpdate:   true,
			AllowDelete:   true,
			AllowList:     true,
			DefaultFields: []string{"name", "customer_name", "status", "grand_total"},
			AlwaysInclude: []string{"name"},
		},
		Fields: fields,
	}
}

// SimpleDocValues returns a fresh simple document payload.
func SimpleDocValues(seq int) map[string]any {
	return map[string]any{
		"customer_name": fmt.Sprintf("Customer-%06d", seq),
		"status":        "Open",
		"grand_total":   float64(seq%1000) + 100.25,
		"notes":         fmt.Sprintf("Test document %06d", seq),
		"external_ref":  fmt.Sprintf("EXT-%06d", seq),
	}
}

// ComplexDocValues returns a fresh complex document payload with five child rows.
func ComplexDocValues(seq int) map[string]any {
	values := map[string]any{
		"customer_name": fmt.Sprintf("Complex Customer-%06d", seq),
		"status":        "Draft",
		"grand_total":   float64(500 + seq%1000),
	}

	for i := 1; i <= 46; i++ {
		name := fmt.Sprintf("field_%02d", i)
		switch i % 3 {
		case 1:
			values[name] = fmt.Sprintf("value-%06d-%02d", seq, i)
		case 2:
			values[name] = seq + i
		default:
			values[name] = i%2 == 0
		}
	}

	items := make([]any, 0, 5)
	for i := 0; i < 5; i++ {
		qty := i + 1
		rate := float64(25 * (i + 1))
		items = append(items, map[string]any{
			"item_code": fmt.Sprintf("ITEM-%06d-%02d", seq, i),
			"qty":       qty,
			"rate":      rate,
			"amount":    float64(qty) * rate,
		})
	}
	values["items"] = items
	return values
}

// AllFieldTypesDocType returns a MetaType with one field of every storable
// field type, useful for testing factory generation coverage.
func AllFieldTypesDocType(doctype string) *meta.MetaType {
	minVal := 0.0
	maxVal := 100.0
	return &meta.MetaType{
		Name:   doctype,
		Module: "test",
		NamingRule: meta.NamingStrategy{
			Rule: meta.NamingUUID,
		},
		APIConfig: &meta.APIConfig{Enabled: true, AllowCreate: true, AllowGet: true},
		Fields: []meta.FieldDef{
			{Name: "data_field", FieldType: meta.FieldTypeData, Label: "Data", Required: true, InAPI: true},
			{Name: "text_field", FieldType: meta.FieldTypeText, Label: "Text", InAPI: true},
			{Name: "long_text_field", FieldType: meta.FieldTypeLongText, Label: "LongText", InAPI: true},
			{Name: "markdown_field", FieldType: meta.FieldTypeMarkdown, Label: "Markdown", InAPI: true},
			{Name: "code_field", FieldType: meta.FieldTypeCode, Label: "Code", InAPI: true},
			{Name: "html_editor_field", FieldType: meta.FieldTypeHTMLEditor, Label: "HTMLEditor", InAPI: true},
			{Name: "int_field", FieldType: meta.FieldTypeInt, Label: "Int", InAPI: true},
			{Name: "float_field", FieldType: meta.FieldTypeFloat, Label: "Float", InAPI: true},
			{Name: "currency_field", FieldType: meta.FieldTypeCurrency, Label: "Currency", InAPI: true},
			{Name: "percent_field", FieldType: meta.FieldTypePercent, Label: "Percent", InAPI: true, MinValue: &minVal, MaxValue: &maxVal},
			{Name: "date_field", FieldType: meta.FieldTypeDate, Label: "Date", InAPI: true},
			{Name: "datetime_field", FieldType: meta.FieldTypeDatetime, Label: "Datetime", InAPI: true},
			{Name: "time_field", FieldType: meta.FieldTypeTime, Label: "Time", InAPI: true},
			{Name: "duration_field", FieldType: meta.FieldTypeDuration, Label: "Duration", InAPI: true},
			{Name: "select_field", FieldType: meta.FieldTypeSelect, Label: "Select", Options: "Draft\nOpen\nClosed", InAPI: true},
			{Name: "check_field", FieldType: meta.FieldTypeCheck, Label: "Check", InAPI: true},
			{Name: "color_field", FieldType: meta.FieldTypeColor, Label: "Color", InAPI: true},
			{Name: "rating_field", FieldType: meta.FieldTypeRating, Label: "Rating", InAPI: true},
			{Name: "json_field", FieldType: meta.FieldTypeJSON, Label: "JSON", InAPI: true},
			{Name: "geolocation_field", FieldType: meta.FieldTypeGeolocation, Label: "Geolocation", InAPI: true},
			{Name: "attach_field", FieldType: meta.FieldTypeAttach, Label: "Attach", InAPI: true},
			{Name: "attach_image_field", FieldType: meta.FieldTypeAttachImage, Label: "AttachImage", InAPI: true},
			{Name: "password_field", FieldType: meta.FieldTypePassword, Label: "Password", InAPI: true},
			{Name: "signature_field", FieldType: meta.FieldTypeSignature, Label: "Signature", InAPI: true},
			{Name: "barcode_field", FieldType: meta.FieldTypeBarcode, Label: "Barcode", InAPI: true},
		},
	}
}

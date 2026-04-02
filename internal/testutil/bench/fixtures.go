package bench

import (
	"fmt"

	"github.com/osama1998H/moca/pkg/meta"
)

// SimpleDocType returns a small API-enabled doctype used by document and API
// benchmarks.
func SimpleDocType(doctype string) *meta.MetaType {
	return &meta.MetaType{
		Name:   doctype,
		Module: "bench",
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

// ChildDocType returns a child-table doctype used by complex insert benchmarks.
func ChildDocType(doctype string) *meta.MetaType {
	return &meta.MetaType{
		Name:         doctype,
		Module:       "bench",
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
		Module: "bench",
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
		"notes":         fmt.Sprintf("Simple benchmark document %06d", seq),
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

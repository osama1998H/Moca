package search

import (
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

func TestIndexName(t *testing.T) {
	tests := []struct {
		site    string
		doctype string
		name    string
	}{
		{"acme", "SalesOrder", "basic"},
		{"test_site", "User", "underscore"},
		{"site1", "DocType", "short"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IndexName(tt.site, tt.doctype)
			if got == "" {
				t.Error("expected non-empty index name")
			}
		})
	}
}

func TestNewIndexer(t *testing.T) {
	idx := NewIndexer(nil)
	if idx == nil {
		t.Fatal("expected non-nil Indexer")
	}
}

func TestHasSearchableFields(t *testing.T) {
	tests := []struct {
		mt   *meta.MetaType
		name string
		want bool
	}{
		{
			name: "with_searchable_field",
			mt: &meta.MetaType{
				Name: "SalesOrder",
				Fields: []meta.FieldDef{
					{Name: "customer", FieldType: meta.FieldTypeData, Searchable: true},
				},
			},
			want: true,
		},
		{
			name: "no_searchable_fields",
			mt: &meta.MetaType{
				Name: "SalesOrder",
				Fields: []meta.FieldDef{
					{Name: "section", FieldType: meta.FieldTypeSectionBreak},
				},
			},
			want: false,
		},
		{
			name: "empty_fields",
			mt: &meta.MetaType{
				Name:   "EmptyType",
				Fields: nil,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasSearchableFields(tt.mt)
			if got != tt.want {
				t.Errorf("hasSearchableFields = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSearchableAttributes(t *testing.T) {
	mt := &meta.MetaType{
		Name: "SalesOrder",
		Fields: []meta.FieldDef{
			{Name: "customer", FieldType: meta.FieldTypeData, Searchable: true},
			{Name: "description", FieldType: meta.FieldTypeText, Searchable: true},
			{Name: "section", FieldType: meta.FieldTypeSectionBreak},        // not searchable
			{Name: "total", FieldType: meta.FieldTypeFloat, Searchable: false}, // explicitly not searchable
		},
	}

	attrs := searchableAttributes(mt)
	found := map[string]bool{}
	for _, a := range attrs {
		found[a] = true
	}
	// "name" is always included as a base searchable attribute.
	if !found["name"] {
		t.Error("expected 'name' in searchable attributes (always included)")
	}
	if !found["customer"] {
		t.Error("expected 'customer' in searchable attributes")
	}
	if !found["description"] {
		t.Error("expected 'description' in searchable attributes")
	}
	if found["section"] {
		t.Error("section should not be searchable")
	}
	if found["total"] {
		t.Error("total should not be searchable")
	}
}

func TestFilterableAttributes(t *testing.T) {
	mt := &meta.MetaType{
		Name: "SalesOrder",
		Fields: []meta.FieldDef{
			{Name: "customer", FieldType: meta.FieldTypeLink, Options: "Customer", Filterable: true},
			{Name: "status", FieldType: meta.FieldTypeSelect, Filterable: true},
			{Name: "total", FieldType: meta.FieldTypeFloat},
			{Name: "section", FieldType: meta.FieldTypeSectionBreak},
		},
	}

	attrs := filterableAttributes(mt)
	if len(attrs) == 0 {
		t.Fatal("expected non-empty filterable attributes")
	}

	found := map[string]bool{}
	for _, a := range attrs {
		found[a] = true
	}
	// Base attributes should always be included.
	for _, base := range baseFilterableAttributes {
		if !found[base] {
			t.Errorf("expected base attribute %q in filterable", base)
		}
	}
	// User-defined filterable fields.
	if !found["customer"] {
		t.Error("expected 'customer' in filterable attributes")
	}
	if !found["status"] {
		t.Error("expected 'status' in filterable attributes")
	}
	// Non-filterable fields should not be included (unless in base set).
	if found["total"] {
		t.Error("'total' should not be filterable (not marked)")
	}
}

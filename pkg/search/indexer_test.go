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
			name: "with_searchable_data",
			mt: &meta.MetaType{
				Name: "SalesOrder",
				Fields: []meta.FieldDef{
					{FieldName: "customer", FieldType: meta.FieldTypeData, InListView: true},
				},
			},
			want: true,
		},
		{
			name: "with_searchable_text",
			mt: &meta.MetaType{
				Name: "SalesOrder",
				Fields: []meta.FieldDef{
					{FieldName: "description", FieldType: meta.FieldTypeText},
				},
			},
			want: true,
		},
		{
			name: "only_non_searchable",
			mt: &meta.MetaType{
				Name: "SalesOrder",
				Fields: []meta.FieldDef{
					{FieldName: "section", FieldType: meta.FieldTypeSectionBreak},
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
			{FieldName: "customer", FieldType: meta.FieldTypeData},
			{FieldName: "description", FieldType: meta.FieldTypeText},
			{FieldName: "section", FieldType: meta.FieldTypeSectionBreak}, // not searchable
			{FieldName: "total", FieldType: meta.FieldTypeFloat},         // not searchable
		},
	}

	attrs := searchableAttributes(mt)
	// Should include text-searchable fields.
	found := map[string]bool{}
	for _, a := range attrs {
		found[a] = true
	}
	if !found["customer"] {
		t.Error("expected 'customer' in searchable attributes")
	}
	if !found["description"] {
		t.Error("expected 'description' in searchable attributes")
	}
	if found["section"] {
		t.Error("section_break should not be searchable")
	}
}

func TestFilterableAttributes(t *testing.T) {
	mt := &meta.MetaType{
		Name: "SalesOrder",
		Fields: []meta.FieldDef{
			{FieldName: "customer", FieldType: meta.FieldTypeLink, Options: "Customer"},
			{FieldName: "status", FieldType: meta.FieldTypeSelect},
			{FieldName: "total", FieldType: meta.FieldTypeFloat},
			{FieldName: "section", FieldType: meta.FieldTypeSectionBreak},
		},
	}

	attrs := filterableAttributes(mt)
	if len(attrs) == 0 {
		t.Fatal("expected non-empty filterable attributes")
	}

	// Base attributes should always be included.
	found := map[string]bool{}
	for _, a := range attrs {
		found[a] = true
	}
	for _, base := range baseFilterableAttributes {
		if !found[base] {
			t.Errorf("expected base attribute %q in filterable", base)
		}
	}
}

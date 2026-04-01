package core

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/moca-framework/moca/pkg/meta"
)

//go:embed modules/core/doctypes/*/*.json
var doctypeFS embed.FS

// BootstrapCoreMeta returns the compiled MetaType definitions for all core
// doctypes. The DocType MetaType is constructed in Go code to solve the
// self-referential bootstrap problem; all other doctypes are loaded from
// embedded JSON files and compiled via meta.Compile.
//
// The returned slice has DocType first (so it can be seeded into the registry
// before the others), followed by the remaining doctypes in walk order.
func BootstrapCoreMeta() ([]*meta.MetaType, error) {
	result := make([]*meta.MetaType, 0, 8)

	// Hard-coded DocType MetaType (self-referential bootstrap).
	result = append(result, buildDocTypeMetaType())

	// Load all other doctypes from embedded JSON files.
	err := fs.WalkDir(doctypeFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		// Skip doctype.json -- already hard-coded above.
		if strings.HasSuffix(path, "doctype/doctype.json") {
			return nil
		}
		data, readErr := doctypeFS.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("bootstrap: read %s: %w", path, readErr)
		}
		mt, compileErr := meta.Compile(data)
		if compileErr != nil {
			return fmt.Errorf("bootstrap: compile %s: %w", path, compileErr)
		}
		result = append(result, mt)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// buildDocTypeMetaType constructs the DocType MetaType in Go code.
// This solves the bootstrap chicken-and-egg: you need the DocType definition
// to compile DocType definitions, but DocType's own definition IS a DocType.
func buildDocTypeMetaType() *meta.MetaType {
	return &meta.MetaType{
		Name:         "DocType",
		Module:       "Core",
		Label:        "DocType",
		Description:  "Metadata definition for a document type.",
		TrackChanges: true,
		NamingRule: meta.NamingStrategy{
			Rule: meta.NamingUUID,
		},
		Fields: []meta.FieldDef{
			{Name: "dt_module", FieldType: meta.FieldTypeData, Label: "Module", InListView: true},
			{Name: "dt_label", FieldType: meta.FieldTypeData, Label: "Label"},
			{Name: "dt_description", FieldType: meta.FieldTypeText, Label: "Description"},
			{Name: "fields", FieldType: meta.FieldTypeTable, Label: "Fields", Options: "DocField"},
			{Name: "permissions", FieldType: meta.FieldTypeTable, Label: "Permissions", Options: "DocPerm"},
			{Name: "is_submittable", FieldType: meta.FieldTypeCheck, Label: "Is Submittable"},
			{Name: "is_single", FieldType: meta.FieldTypeCheck, Label: "Is Single"},
			{Name: "is_child_table", FieldType: meta.FieldTypeCheck, Label: "Is Child Table"},
			{Name: "is_virtual", FieldType: meta.FieldTypeCheck, Label: "Is Virtual"},
			{Name: "dt_track_changes", FieldType: meta.FieldTypeCheck, Label: "Track Changes"},
			{Name: "dt_naming_rule", FieldType: meta.FieldTypeData, Label: "Naming Rule"},
			{Name: "dt_title_field", FieldType: meta.FieldTypeData, Label: "Title Field"},
			{Name: "dt_sort_field", FieldType: meta.FieldTypeData, Label: "Sort Field"},
			{Name: "dt_sort_order", FieldType: meta.FieldTypeSelect, Label: "Sort Order", Options: "asc\ndesc"},
			{Name: "dt_search_fields", FieldType: meta.FieldTypeData, Label: "Search Fields"},
			{Name: "dt_image_field", FieldType: meta.FieldTypeData, Label: "Image Field"},
		},
		Permissions: []meta.PermRule{
			{Role: "System Manager", DocTypePerm: 63},
		},
	}
}

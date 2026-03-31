package meta

import (
	"context"
	"fmt"

	"github.com/moca-framework/moca/pkg/orm"
)

// QueryMetaAdapter adapts a Registry to satisfy orm.MetaProvider, bridging
// the meta and orm packages without creating an import cycle.
type QueryMetaAdapter struct {
	registry *Registry
}

// NewQueryMetaAdapter creates an adapter that resolves QueryMeta from a Registry.
func NewQueryMetaAdapter(r *Registry) *QueryMetaAdapter {
	return &QueryMetaAdapter{registry: r}
}

// QueryMeta resolves a DocType into the query metadata needed by QueryBuilder.
func (a *QueryMetaAdapter) QueryMeta(ctx context.Context, site, doctype string) (*orm.QueryMeta, error) {
	mt, err := a.registry.Get(ctx, site, doctype)
	if err != nil {
		return nil, fmt.Errorf("query meta adapter: %w", err)
	}

	var stdCols []StandardColumnDef
	if mt.IsChildTable {
		stdCols = ChildStandardColumns()
	} else {
		stdCols = StandardColumns()
	}

	validCols := make(map[string]struct{}, len(stdCols)+len(mt.Fields))
	for _, c := range stdCols {
		validCols[c.Name] = struct{}{}
	}

	fieldTypes := make(map[string]string, len(mt.Fields))
	nonQueryableFields := make(map[string]struct{})
	linkFields := make(map[string]string)
	dynamicLinkFields := make(map[string]struct{})

	for _, f := range mt.Fields {
		colType := ColumnType(f.FieldType)
		if colType != "" {
			validCols[f.Name] = struct{}{}
			fieldTypes[f.Name] = colType
		} else if f.FieldType == FieldTypeTable || f.FieldType == FieldTypeTableMultiSelect {
			nonQueryableFields[f.Name] = struct{}{}
		}
		switch f.FieldType {
		case FieldTypeLink:
			linkFields[f.Name] = f.Options
		case FieldTypeDynamicLink:
			dynamicLinkFields[f.Name] = struct{}{}
		}
	}

	return &orm.QueryMeta{
		Name:               mt.Name,
		IsChildTable:       mt.IsChildTable,
		TableName:          TableName(doctype),
		ValidColumns:       validCols,
		FieldTypes:         fieldTypes,
		NonQueryableFields: nonQueryableFields,
		LinkFields:         linkFields,
		DynamicLinkFields:  dynamicLinkFields,
	}, nil
}

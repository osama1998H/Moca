package document

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/osama1998H/moca/pkg/meta"
)

// Document is the core runtime abstraction for a single record managed by MOCA.
// Every downstream milestone (API layer, query engine, hook system, search,
// workflows) interacts with records through this interface.
//
// The primary implementation is DynamicDoc (map-backed). VirtualDoc (deferred
// to MS-28) wraps external data sources behind this same interface.
type Document interface {
	// Meta returns the MetaType definition that governs this document.
	Meta() *meta.MetaType

	// Name returns the document's primary key / unique identifier.
	Name() string

	// Get returns the value of a field by name.
	// Returns nil for unknown fields without error.
	Get(field string) any

	// Set assigns a value to a field by name.
	// Returns an error if the field name does not exist in the MetaType definition
	// or in the set of standard system columns.
	Set(field string, value any) error

	// GetChild returns the child-table rows for a Table field.
	// Returns an empty slice if the field has no children or does not exist.
	GetChild(tableField string) []Document

	// AddChild creates and appends a new empty child row to a Table field,
	// auto-assigning the next idx value. Returns an error if tableField is not a
	// Table-type field or if the child MetaType is not available.
	AddChild(tableField string) (Document, error)

	// IsNew reports whether this document has never been persisted to the database.
	IsNew() bool

	// IsModified reports whether any field value has changed since the document
	// was constructed or last loaded.
	IsModified() bool

	// ModifiedFields returns the names of fields whose values have changed since
	// construction or last load. Returns nil when nothing has changed.
	ModifiedFields() []string

	// AsMap serializes the document to a plain map, including child rows.
	// Child table rows appear as []map[string]any under the table field name.
	AsMap() map[string]any

	// ToJSON serializes the document to JSON bytes via AsMap.
	ToJSON() ([]byte, error)
}

// DynamicDoc is the default map-backed Document implementation. It stores field
// values in a Go map, tracks dirty state by comparing against a snapshot taken
// at construction time, and manages child-table rows in a nested map.
type DynamicDoc struct {
	metaDef    *meta.MetaType
	values     map[string]any
	original   map[string]any           // deep-copied snapshot for dirty tracking
	children   map[string][]*DynamicDoc // keyed by Table field name
	childMetas map[string]*meta.MetaType
	// validFields is built once at construction: all accepted field names for Set().
	validFields map[string]struct{}
	// tableFields is the set of field names with type Table or TableMultiSelect.
	tableFields map[string]struct{}
	isNew       bool
}

// NewDynamicDoc constructs a DynamicDoc for the given MetaType.
//
//   - metaDef: the compiled MetaType for this document
//   - childMetas: pre-resolved MetaType definitions for each Table field, keyed by
//     field name (e.g. "items" -> *MetaType for the child table). May be nil when
//     the MetaType has no Table fields.
//   - isNew: true when the document is being created (not loaded from DB)
func NewDynamicDoc(metaDef *meta.MetaType, childMetas map[string]*meta.MetaType, isNew bool) *DynamicDoc {
	if childMetas == nil {
		childMetas = make(map[string]*meta.MetaType)
	}

	// Build the set of valid field names accepted by Set().
	validFields := make(map[string]struct{})
	tableFields := make(map[string]struct{})

	// Standard system columns are always valid.
	if metaDef.IsChildTable {
		for _, col := range meta.ChildStandardColumns() {
			validFields[col.Name] = struct{}{}
		}
	} else {
		for _, col := range meta.StandardColumns() {
			validFields[col.Name] = struct{}{}
		}
	}

	// User-defined fields from the MetaType.
	for _, f := range metaDef.Fields {
		if !f.FieldType.IsStorable() {
			continue
		}
		validFields[f.Name] = struct{}{}
		if f.FieldType == meta.FieldTypeTable || f.FieldType == meta.FieldTypeTableMultiSelect {
			tableFields[f.Name] = struct{}{}
		}
	}

	values := make(map[string]any)
	// Seed defaults from FieldDef.Default.
	for _, f := range metaDef.Fields {
		if f.Default != nil {
			values[f.Name] = f.Default
		}
	}

	return &DynamicDoc{
		metaDef:     metaDef,
		values:      values,
		original:    deepCopyMap(values),
		children:    make(map[string][]*DynamicDoc),
		childMetas:  childMetas,
		isNew:       isNew,
		validFields: validFields,
		tableFields: tableFields,
	}
}

// Meta returns the MetaType definition for this document.
func (d *DynamicDoc) Meta() *meta.MetaType { return d.metaDef }

// Name returns the document's primary key value.
func (d *DynamicDoc) Name() string {
	v, ok := d.values["name"]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// Get returns the value of a field by name. Returns nil for unknown fields.
func (d *DynamicDoc) Get(field string) any {
	return d.values[field]
}

// Set assigns a value to field. Returns an error if field is not a recognized
// user-defined field name or standard column name for this document's MetaType.
func (d *DynamicDoc) Set(field string, value any) error {
	if _, ok := d.validFields[field]; !ok {
		return fmt.Errorf("document.Set: unknown field %q on doctype %q", field, d.metaDef.Name)
	}
	d.values[field] = value
	return nil
}

// GetChild returns the child rows for a Table field as a []Document slice.
// Returns an empty slice if no children exist for the field.
func (d *DynamicDoc) GetChild(tableField string) []Document {
	rows := d.children[tableField]
	if len(rows) == 0 {
		return nil
	}
	docs := make([]Document, len(rows))
	for i, r := range rows {
		docs[i] = r
	}
	return docs
}

// AddChild creates a new child row for a Table field, sets its idx to the next
// available index, and appends it to the internal children slice.
// Returns an error if tableField is not a Table-type field or the child MetaType
// is not present in childMetas.
func (d *DynamicDoc) AddChild(tableField string) (Document, error) {
	if _, ok := d.tableFields[tableField]; !ok {
		return nil, fmt.Errorf("document.AddChild: %q is not a Table field on doctype %q", tableField, d.metaDef.Name)
	}
	childMeta, ok := d.childMetas[tableField]
	if !ok {
		return nil, fmt.Errorf("document.AddChild: no child MetaType registered for field %q on doctype %q", tableField, d.metaDef.Name)
	}

	child := NewDynamicDoc(childMeta, nil, true)
	idx := len(d.children[tableField])
	// idx is set directly to avoid a second lookup through validFields.
	child.values["idx"] = idx

	d.children[tableField] = append(d.children[tableField], child)
	return child, nil
}

// IsNew reports whether this document has never been persisted.
func (d *DynamicDoc) IsNew() bool { return d.isNew }

// IsModified reports whether any field value has changed since construction.
func (d *DynamicDoc) IsModified() bool {
	return len(d.ModifiedFields()) > 0
}

// ModifiedFields returns the names of fields that have changed since construction.
func (d *DynamicDoc) ModifiedFields() []string {
	var changed []string
	// Check fields present in current values.
	for k, v := range d.values {
		orig, exists := d.original[k]
		if !exists || !reflect.DeepEqual(v, orig) {
			changed = append(changed, k)
		}
	}
	// Check fields removed from values that existed in original.
	for k := range d.original {
		if _, exists := d.values[k]; !exists {
			changed = append(changed, k)
		}
	}
	return changed
}

// AsMap returns a plain map representation of the document.
// Child rows are included as []map[string]any under each Table field name.
func (d *DynamicDoc) AsMap() map[string]any {
	out := make(map[string]any, len(d.values)+len(d.children))
	for k, v := range d.values {
		out[k] = v
	}
	for field, rows := range d.children {
		childMaps := make([]map[string]any, len(rows))
		for i, r := range rows {
			childMaps[i] = r.AsMap()
		}
		out[field] = childMaps
	}
	return out
}

// ToJSON serializes the document to JSON via AsMap.
func (d *DynamicDoc) ToJSON() ([]byte, error) {
	return json.Marshal(d.AsMap())
}

// deepCopyMap returns a deep copy of a map[string]any, recursively cloning
// nested maps and slices to prevent aliasing between values and original.
func deepCopyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = deepCopyValue(v)
	}
	return dst
}

func deepCopyValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return deepCopyMap(val)
	case []any:
		clone := make([]any, len(val))
		for i, elem := range val {
			clone[i] = deepCopyValue(elem)
		}
		return clone
	default:
		// Primitives (string, int, float64, bool, nil, time.Time, etc.) are
		// safe to copy by value; no aliasing possible.
		return v
	}
}

// compile-time assertion that *DynamicDoc satisfies Document.
var _ Document = (*DynamicDoc)(nil)

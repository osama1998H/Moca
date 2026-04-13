package document

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/osama1998H/moca/pkg/meta"
)

// ErrVirtualReadOnly is returned by VirtualSource write methods when the
// source does not support mutations.
var ErrVirtualReadOnly = errors.New("virtual document source is read-only")

// VirtualSource is the adapter interface for virtual doctypes (MetaType.IsVirtual).
type VirtualSource interface {
	GetList(ctx context.Context, opts ListOptions) ([]map[string]any, int, error)
	GetOne(ctx context.Context, name string) (map[string]any, error)
	Insert(ctx context.Context, values map[string]any) (string, error)
	Update(ctx context.Context, name string, values map[string]any) error
	Delete(ctx context.Context, name string) error
}

// ReadOnlySource provides default ErrVirtualReadOnly returns for write methods.
type ReadOnlySource struct{}

func (ReadOnlySource) Insert(_ context.Context, _ map[string]any) (string, error) {
	return "", ErrVirtualReadOnly
}
func (ReadOnlySource) Update(_ context.Context, _ string, _ map[string]any) error {
	return ErrVirtualReadOnly
}
func (ReadOnlySource) Delete(_ context.Context, _ string) error {
	return ErrVirtualReadOnly
}

// VirtualDoc implements the Document interface backed by a VirtualSource.
type VirtualDoc struct {
	metaDef  *meta.MetaType
	source   VirtualSource
	values   map[string]any
	original map[string]any
	isNew    bool
}

// NewVirtualDoc wraps values from a VirtualSource as a Document.
func NewVirtualDoc(metaDef *meta.MetaType, source VirtualSource, values map[string]any, isNew bool) *VirtualDoc {
	original := make(map[string]any, len(values))
	for k, v := range values {
		original[k] = v
	}
	return &VirtualDoc{
		metaDef:  metaDef,
		source:   source,
		values:   values,
		original: original,
		isNew:    isNew,
	}
}

func (d *VirtualDoc) Meta() *meta.MetaType { return d.metaDef }

func (d *VirtualDoc) Name() string {
	if n, ok := d.values["name"].(string); ok {
		return n
	}
	return ""
}

func (d *VirtualDoc) Get(field string) any { return d.values[field] }
func (d *VirtualDoc) Set(field string, value any) error {
	d.values[field] = value
	return nil
}

func (d *VirtualDoc) GetChild(_ string) []Document { return nil }
func (d *VirtualDoc) AddChild(_ string) (Document, error) {
	return nil, errors.New("virtual documents do not support child tables")
}

func (d *VirtualDoc) IsNew() bool      { return d.isNew }
func (d *VirtualDoc) IsModified() bool { return len(d.ModifiedFields()) > 0 }

func (d *VirtualDoc) ModifiedFields() []string {
	var modified []string
	for k, v := range d.values {
		if orig, ok := d.original[k]; !ok || orig != v {
			modified = append(modified, k)
		}
	}
	return modified
}

func (d *VirtualDoc) AsMap() map[string]any {
	result := make(map[string]any, len(d.values))
	for k, v := range d.values {
		result[k] = v
	}
	return result
}

func (d *VirtualDoc) ToJSON() ([]byte, error) { return json.Marshal(d.AsMap()) }

// Source returns the underlying VirtualSource.
func (d *VirtualDoc) Source() VirtualSource { return d.source }

// compile-time assertion that *VirtualDoc satisfies Document.
var _ Document = (*VirtualDoc)(nil)

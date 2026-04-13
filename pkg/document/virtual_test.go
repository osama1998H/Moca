package document

import (
	"context"
	"errors"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

func TestReadOnlySource_ReturnsErrVirtualReadOnly(t *testing.T) {
	var src ReadOnlySource

	_, err := src.Insert(context.Background(), nil)
	if !errors.Is(err, ErrVirtualReadOnly) {
		t.Errorf("Insert: got %v, want ErrVirtualReadOnly", err)
	}

	err = src.Update(context.Background(), "x", nil)
	if !errors.Is(err, ErrVirtualReadOnly) {
		t.Errorf("Update: got %v, want ErrVirtualReadOnly", err)
	}

	err = src.Delete(context.Background(), "x")
	if !errors.Is(err, ErrVirtualReadOnly) {
		t.Errorf("Delete: got %v, want ErrVirtualReadOnly", err)
	}
}

// testReadOnlySource embeds ReadOnlySource and adds the read methods,
// satisfying the full VirtualSource interface for testing purposes.
type testReadOnlySource struct {
	ReadOnlySource
}

func (testReadOnlySource) GetList(_ context.Context, _ ListOptions) ([]map[string]any, int, error) {
	return nil, 0, nil
}
func (testReadOnlySource) GetOne(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

func TestVirtualDoc_ImplementsDocumentInterface(t *testing.T) {
	mt := &meta.MetaType{Name: "ExternalInvoice", IsVirtual: true}
	values := map[string]any{"name": "INV-001", "amount": 100.0}
	doc := NewVirtualDoc(mt, testReadOnlySource{}, values, false)

	var _ Document = doc

	if doc.Name() != "INV-001" {
		t.Errorf("Name: got %q, want %q", doc.Name(), "INV-001")
	}
	if doc.Get("amount") != 100.0 {
		t.Errorf("Get(amount): got %v, want 100.0", doc.Get("amount"))
	}
	if doc.IsNew() {
		t.Error("IsNew: expected false")
	}
	if doc.IsModified() {
		t.Error("IsModified: expected false before changes")
	}

	_ = doc.Set("amount", 200.0)
	if !doc.IsModified() {
		t.Error("IsModified: expected true after Set")
	}

	fields := doc.ModifiedFields()
	if len(fields) != 1 || fields[0] != "amount" {
		t.Errorf("ModifiedFields: got %v, want [amount]", fields)
	}

	m := doc.AsMap()
	if m["amount"] != 200.0 {
		t.Errorf("AsMap[amount]: got %v, want 200.0", m["amount"])
	}

	children := doc.GetChild("items")
	if len(children) != 0 {
		t.Errorf("GetChild: expected empty, got %d", len(children))
	}

	_, err := doc.AddChild("items")
	if err == nil {
		t.Error("AddChild: expected error for virtual doc")
	}

	data, err := doc.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	if len(data) == 0 {
		t.Error("ToJSON: expected non-empty")
	}
}

func TestVirtualDoc_Source(t *testing.T) {
	src := testReadOnlySource{}
	mt := &meta.MetaType{Name: "Test"}
	doc := NewVirtualDoc(mt, src, map[string]any{"name": "x"}, false)

	if doc.Source() == nil {
		t.Error("Source: expected non-nil")
	}
}

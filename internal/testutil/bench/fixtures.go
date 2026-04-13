package bench

import (
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/testutils/factory"
)

// SimpleDocType returns a small API-enabled doctype used by document and API
// benchmarks. Delegates to factory.SimpleDocType.
func SimpleDocType(doctype string) *meta.MetaType {
	return factory.SimpleDocType(doctype)
}

// ChildDocType returns a child-table doctype used by complex insert benchmarks.
// Delegates to factory.ChildDocType.
func ChildDocType(doctype string) *meta.MetaType {
	return factory.ChildDocType(doctype)
}

// ComplexDocType returns a 50-field parent doctype with a child table field.
// Delegates to factory.ComplexDocType.
func ComplexDocType(doctype, childDocType string) *meta.MetaType {
	return factory.ComplexDocType(doctype, childDocType)
}

// SimpleDocValues returns a fresh simple document payload.
// Delegates to factory.SimpleDocValues.
func SimpleDocValues(seq int) map[string]any {
	return factory.SimpleDocValues(seq)
}

// ComplexDocValues returns a fresh complex document payload with five child rows.
// Delegates to factory.ComplexDocValues.
func ComplexDocValues(seq int) map[string]any {
	return factory.ComplexDocValues(seq)
}

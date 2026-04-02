package meta_test

import (
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

// baseMetaType returns a simple MetaType suitable as a base for diff tests.
func baseMetaType() *meta.MetaType {
	return &meta.MetaType{
		Name:   "Invoice",
		Module: "accounts",
		Fields: []meta.FieldDef{
			{Name: "customer", FieldType: meta.FieldTypeData},
			{Name: "amount", FieldType: meta.FieldTypeCurrency},
			{Name: "notes", FieldType: meta.FieldTypeText},
		},
	}
}

// diffContains reports whether any DDLStatement in stmts has SQL containing sub
// (case-insensitive).
func diffContains(stmts []meta.DDLStatement, sub string) bool {
	return containsSQL(stmts, sub)
}

// ── Diff: nil current ────────────────────────────────────────────────────────

func TestDiff_NilCurrent_DelegatesToGenerateTableDDL(t *testing.T) {
	mt := baseMetaType()

	diffStmts := meta.NewMigrator(nil, nil).Diff(nil, mt)
	directStmts := meta.GenerateTableDDL(mt)

	if len(diffStmts) != len(directStmts) {
		t.Errorf("Diff(nil, mt) returned %d stmts; GenerateTableDDL returned %d stmts",
			len(diffStmts), len(directStmts))
	}

	for i := range diffStmts {
		if diffStmts[i].SQL != directStmts[i].SQL {
			t.Errorf("stmts[%d] differ:\n  Diff:   %s\n  Direct: %s",
				i, diffStmts[i].SQL, directStmts[i].SQL)
		}
	}
	t.Logf("Diff(nil, mt) produced %d statements matching GenerateTableDDL", len(diffStmts))
}

// ── Diff: no changes ─────────────────────────────────────────────────────────

func TestDiff_NoChanges_EmptyResult(t *testing.T) {
	mt := baseMetaType()
	stmts := meta.NewMigrator(nil, nil).Diff(mt, mt)
	if len(stmts) != 0 {
		t.Errorf("Diff with identical MetaTypes should return empty slice; got %d stmts: %+v",
			len(stmts), stmts)
	}
}

// ── Diff: add field ──────────────────────────────────────────────────────────

func TestDiff_AddField_ProducesAddColumn(t *testing.T) {
	current := baseMetaType()
	desired := baseMetaType()
	desired.Fields = append(desired.Fields, meta.FieldDef{
		Name:      "due_date",
		FieldType: meta.FieldTypeDate,
	})

	stmts := meta.NewMigrator(nil, nil).Diff(current, desired)
	if len(stmts) == 0 {
		t.Fatal("expected DDL statements for added field")
	}

	if !diffContains(stmts, "add column") {
		t.Errorf("expected ADD COLUMN statement; got: %+v", stmts)
	}
	if !diffContains(stmts, "due_date") {
		t.Errorf("expected column name 'due_date' in statement; got: %+v", stmts)
	}
	if !diffContains(stmts, "DATE") {
		t.Errorf("expected DATE column type; got: %+v", stmts)
	}
}

// ── Diff: remove field ───────────────────────────────────────────────────────

func TestDiff_RemoveField_ProducesDropColumn(t *testing.T) {
	current := baseMetaType()
	desired := &meta.MetaType{
		Name:   current.Name,
		Module: current.Module,
		Fields: []meta.FieldDef{
			{Name: "customer", FieldType: meta.FieldTypeData},
			{Name: "amount", FieldType: meta.FieldTypeCurrency},
			// "notes" removed
		},
	}

	stmts := meta.NewMigrator(nil, nil).Diff(current, desired)
	if len(stmts) == 0 {
		t.Fatal("expected DDL statements for removed field")
	}

	if !diffContains(stmts, "drop column") {
		t.Errorf("expected DROP COLUMN statement; got: %+v", stmts)
	}
	if !diffContains(stmts, "notes") {
		t.Errorf("expected column name 'notes' in DROP COLUMN; got: %+v", stmts)
	}
}

// ── Diff: change type ────────────────────────────────────────────────────────

func TestDiff_ChangeType_ProducesAlterColumnType(t *testing.T) {
	current := &meta.MetaType{
		Name:   "Doc",
		Module: "core",
		Fields: []meta.FieldDef{
			{Name: "value", FieldType: meta.FieldTypeData}, // TEXT
		},
	}
	desired := &meta.MetaType{
		Name:   "Doc",
		Module: "core",
		Fields: []meta.FieldDef{
			{Name: "value", FieldType: meta.FieldTypeInt}, // INTEGER
		},
	}

	stmts := meta.NewMigrator(nil, nil).Diff(current, desired)
	if len(stmts) == 0 {
		t.Fatal("expected DDL statements for type change")
	}

	if !diffContains(stmts, "alter column") {
		t.Errorf("expected ALTER COLUMN statement; got: %+v", stmts)
	}
	if !diffContains(stmts, "INTEGER") {
		t.Errorf("expected INTEGER in ALTER COLUMN TYPE; got: %+v", stmts)
	}
	if !diffContains(stmts, "using") {
		t.Errorf("expected USING clause in ALTER COLUMN TYPE; got: %+v", stmts)
	}
}

// ── Diff: add index ──────────────────────────────────────────────────────────

func TestDiff_AddIndex_ProducesCreateIndex(t *testing.T) {
	current := &meta.MetaType{
		Name:   "Lead",
		Module: "crm",
		Fields: []meta.FieldDef{
			{Name: "email", FieldType: meta.FieldTypeData, DBIndex: false},
		},
	}
	desired := &meta.MetaType{
		Name:   "Lead",
		Module: "crm",
		Fields: []meta.FieldDef{
			{Name: "email", FieldType: meta.FieldTypeData, DBIndex: true},
		},
	}

	stmts := meta.NewMigrator(nil, nil).Diff(current, desired)
	if !diffContains(stmts, "create index") {
		t.Errorf("expected CREATE INDEX for newly indexed field; got: %+v", stmts)
	}
	if !diffContains(stmts, "email") {
		t.Errorf("expected field name 'email' in index statement; got: %+v", stmts)
	}
}

// ── Diff: remove index ───────────────────────────────────────────────────────

func TestDiff_RemoveIndex_ProducesDropIndex(t *testing.T) {
	current := &meta.MetaType{
		Name:   "Lead",
		Module: "crm",
		Fields: []meta.FieldDef{
			{Name: "email", FieldType: meta.FieldTypeData, DBIndex: true},
		},
	}
	desired := &meta.MetaType{
		Name:   "Lead",
		Module: "crm",
		Fields: []meta.FieldDef{
			{Name: "email", FieldType: meta.FieldTypeData, DBIndex: false},
		},
	}

	stmts := meta.NewMigrator(nil, nil).Diff(current, desired)
	if !diffContains(stmts, "drop index") {
		t.Errorf("expected DROP INDEX for de-indexed field; got: %+v", stmts)
	}
}

// ── Diff: full-text index changes ────────────────────────────────────────────

func TestDiff_AddFullTextIndex(t *testing.T) {
	current := &meta.MetaType{
		Name:   "Post",
		Module: "cms",
		Fields: []meta.FieldDef{
			{Name: "body", FieldType: meta.FieldTypeText, FullTextIndex: false},
		},
	}
	desired := &meta.MetaType{
		Name:   "Post",
		Module: "cms",
		Fields: []meta.FieldDef{
			{Name: "body", FieldType: meta.FieldTypeText, FullTextIndex: true},
		},
	}

	stmts := meta.NewMigrator(nil, nil).Diff(current, desired)
	if !diffContains(stmts, "gin") || !diffContains(stmts, "tsvector") {
		t.Errorf("expected GIN tsvector index; got: %+v", stmts)
	}
}

func TestDiff_RemoveFullTextIndex(t *testing.T) {
	current := &meta.MetaType{
		Name:   "Post",
		Module: "cms",
		Fields: []meta.FieldDef{
			{Name: "body", FieldType: meta.FieldTypeText, FullTextIndex: true},
		},
	}
	desired := &meta.MetaType{
		Name:   "Post",
		Module: "cms",
		Fields: []meta.FieldDef{
			{Name: "body", FieldType: meta.FieldTypeText, FullTextIndex: false},
		},
	}

	stmts := meta.NewMigrator(nil, nil).Diff(current, desired)
	if !diffContains(stmts, "drop index") {
		t.Errorf("expected DROP INDEX for removed full-text index; got: %+v", stmts)
	}
	if !diffContains(stmts, "_fts") {
		t.Errorf("expected _fts suffix in drop index name; got: %+v", stmts)
	}
}

// ── Diff: virtual and single no-ops ─────────────────────────────────────────

func TestDiff_VirtualDesired_NoOp(t *testing.T) {
	current := baseMetaType()
	desired := baseMetaType()
	desired.IsVirtual = true

	stmts := meta.NewMigrator(nil, nil).Diff(current, desired)
	if len(stmts) != 0 {
		t.Errorf("Diff with virtual desired should return nil; got %d stmts", len(stmts))
	}
}

func TestDiff_SingleDesired_NoOp(t *testing.T) {
	current := baseMetaType()
	desired := baseMetaType()
	desired.IsSingle = true

	stmts := meta.NewMigrator(nil, nil).Diff(current, desired)
	if len(stmts) != 0 {
		t.Errorf("Diff with single desired should return nil; got %d stmts", len(stmts))
	}
}

// ── Diff: table/layout fields are ignored ────────────────────────────────────

func TestDiff_TableFieldsNotDiffed(t *testing.T) {
	// Adding a Table field should produce no column-related DDL.
	current := &meta.MetaType{
		Name:   "Order",
		Module: "selling",
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData},
		},
	}
	desired := &meta.MetaType{
		Name:   "Order",
		Module: "selling",
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData},
			{Name: "items", FieldType: meta.FieldTypeTable, Options: "OrderItem"},
		},
	}

	stmts := meta.NewMigrator(nil, nil).Diff(current, desired)
	// Table fields produce no columns, so no DDL should be emitted.
	if len(stmts) != 0 {
		t.Errorf("Table field addition should produce no DDL; got: %+v", stmts)
	}
}

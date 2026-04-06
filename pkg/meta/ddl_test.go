package meta_test

import (
	"strings"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

// containsSQL is a helper that checks whether any DDLStatement in stmts
// has SQL containing the given substring (case-insensitive).
func containsSQL(stmts []meta.DDLStatement, sub string) bool {
	sub = strings.ToLower(sub)
	for _, s := range stmts {
		if strings.Contains(strings.ToLower(s.SQL), sub) {
			return true
		}
	}
	return false
}

// findStmtByComment returns the first DDLStatement whose Comment contains sub.
func findStmtByComment(stmts []meta.DDLStatement, sub string) (meta.DDLStatement, bool) {
	sub = strings.ToLower(sub)
	for _, s := range stmts {
		if strings.Contains(strings.ToLower(s.Comment), sub) {
			return s, true
		}
	}
	return meta.DDLStatement{}, false
}

// ── GenerateTableDDL ─────────────────────────────────────────────────────────

func TestGenerateTableDDL_RegularTable(t *testing.T) {
	mt := &meta.MetaType{
		Name:   "SalesOrder",
		Module: "selling",
		Fields: []meta.FieldDef{
			{Name: "customer_name", FieldType: meta.FieldTypeData},
			{Name: "grand_total", FieldType: meta.FieldTypeCurrency},
			{Name: "transaction_date", FieldType: meta.FieldTypeDate},
			{Name: "is_return", FieldType: meta.FieldTypeCheck},
		},
	}

	stmts := meta.GenerateTableDDL(mt)
	if len(stmts) == 0 {
		t.Fatal("GenerateTableDDL returned no statements for regular table")
	}

	createStmt := stmts[0]

	// Table name should be tab_sales_order.
	if !strings.Contains(createStmt.SQL, "tab_sales_order") {
		t.Errorf("SQL missing table name; got:\n%s", createStmt.SQL)
	}

	// Must be CREATE TABLE IF NOT EXISTS.
	lc := strings.ToLower(createStmt.SQL)
	if !strings.Contains(lc, "create table if not exists") {
		t.Errorf("SQL should use CREATE TABLE IF NOT EXISTS; got:\n%s", createStmt.SQL)
	}

	// Standard columns must be present.
	for _, col := range []string{"name", "owner", "creation", "modified", "modified_by",
		"docstatus", "idx", "workflow_state", "_extra", "_user_tags", "_comments", "_assign", "_liked_by"} {
		if !strings.Contains(createStmt.SQL, col) {
			t.Errorf("SQL missing standard column %q; got:\n%s", col, createStmt.SQL)
		}
	}

	// User columns must be present with correct types.
	if !containsSQL(stmts[:1], "TEXT") {
		t.Errorf("expected TEXT column for Data field; got:\n%s", createStmt.SQL)
	}
	if !containsSQL(stmts[:1], "NUMERIC(18,6)") {
		t.Errorf("expected NUMERIC(18,6) column for Currency field; got:\n%s", createStmt.SQL)
	}
	if !containsSQL(stmts[:1], "DATE") {
		t.Errorf("expected DATE column for Date field; got:\n%s", createStmt.SQL)
	}
	if !containsSQL(stmts[:1], "BOOLEAN") {
		t.Errorf("expected BOOLEAN column for Check field; got:\n%s", createStmt.SQL)
	}

	t.Logf("GenerateTableDDL regular table:\n%s", createStmt.SQL)
}

func TestGenerateTableDDL_ChildTable(t *testing.T) {
	mt := &meta.MetaType{
		Name:         "SalesOrderItem",
		Module:       "selling",
		IsChildTable: true,
		Fields: []meta.FieldDef{
			{Name: "item_code", FieldType: meta.FieldTypeData},
			{Name: "qty", FieldType: meta.FieldTypeFloat},
		},
	}

	stmts := meta.GenerateTableDDL(mt)
	if len(stmts) == 0 {
		t.Fatal("GenerateTableDDL returned no statements for child table")
	}

	createStmt := stmts[0]

	// Child columns must be present.
	for _, col := range []string{"parent", "parenttype", "parentfield"} {
		if !strings.Contains(createStmt.SQL, col) {
			t.Errorf("child table SQL missing column %q;\n%s", col, createStmt.SQL)
		}
	}

	// Regular-table-only columns must NOT be present.
	for _, col := range []string{"docstatus", "workflow_state", "_user_tags", "_comments", "_assign", "_liked_by"} {
		if strings.Contains(createStmt.SQL, col) {
			t.Errorf("child table SQL should not contain %q;\n%s", col, createStmt.SQL)
		}
	}

	// _extra should still be present.
	if !strings.Contains(createStmt.SQL, "_extra") {
		t.Errorf("child table SQL missing _extra column;\n%s", createStmt.SQL)
	}

	// Automatic parent index must be generated.
	if !containsSQL(stmts, "create index") || !containsSQL(stmts, "parent") {
		t.Errorf("child table should have parent index; stmts: %+v", stmts)
	}

	t.Logf("GenerateTableDDL child table:\n%s", createStmt.SQL)
}

func TestGenerateTableDDL_VirtualTable(t *testing.T) {
	mt := &meta.MetaType{
		Name:      "VirtualDoc",
		Module:    "core",
		IsVirtual: true,
		Fields:    []meta.FieldDef{{Name: "x", FieldType: meta.FieldTypeData}},
	}

	stmts := meta.GenerateTableDDL(mt)
	if len(stmts) != 0 {
		t.Errorf("GenerateTableDDL for virtual MetaType should return nil; got %d statements", len(stmts))
	}
}

func TestGenerateTableDDL_SingleTable(t *testing.T) {
	mt := &meta.MetaType{
		Name:     "SystemSettings",
		Module:   "core",
		IsSingle: true,
		Fields:   []meta.FieldDef{{Name: "x", FieldType: meta.FieldTypeData}},
	}

	stmts := meta.GenerateTableDDL(mt)
	if len(stmts) != 0 {
		t.Errorf("GenerateTableDDL for single MetaType should return nil; got %d statements", len(stmts))
	}
}

func TestGenerateTableDDL_SkipsTableAndLayoutFields(t *testing.T) {
	mt := &meta.MetaType{
		Name:   "Order",
		Module: "selling",
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData},
			{Name: "items", FieldType: meta.FieldTypeTable, Options: "OrderItem"},
			{Name: "tags", FieldType: meta.FieldTypeTableMultiSelect, Options: "Tag"},
			{Name: "sec1", FieldType: meta.FieldTypeSectionBreak},
			{Name: "col1", FieldType: meta.FieldTypeColumnBreak},
		},
	}

	stmts := meta.GenerateTableDDL(mt)
	if len(stmts) == 0 {
		t.Fatal("expected at least one DDL statement")
	}

	createSQL := stmts[0].SQL

	// Table/layout field names must not appear as column definitions.
	// (They may appear in comments, so check for column-like patterns.)
	for _, name := range []string{"items", "tags", "sec1", "col1"} {
		// Check that the field name does not appear as a quoted column identifier
		// in the CREATE TABLE body. The identifier would appear as "items" or items.
		quotedName := `"` + name + `"`
		if strings.Contains(createSQL, quotedName) {
			t.Errorf("Table/layout field %q should not produce a column; SQL:\n%s", name, createSQL)
		}
	}

	t.Logf("Table/layout fields correctly omitted from DDL")
}

func TestGenerateTableDDL_DBIndexGeneration(t *testing.T) {
	mt := &meta.MetaType{
		Name:   "Invoice",
		Module: "accounts",
		Fields: []meta.FieldDef{
			{Name: "customer", FieldType: meta.FieldTypeData, DBIndex: true},
			{Name: "amount", FieldType: meta.FieldTypeCurrency},
		},
	}

	stmts := meta.GenerateTableDDL(mt)

	// Should have a CREATE INDEX statement for "customer".
	found := false
	for _, s := range stmts[1:] { // skip the CREATE TABLE
		lc := strings.ToLower(s.SQL)
		if strings.Contains(lc, "create index") && strings.Contains(lc, "customer") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected CREATE INDEX for DBIndex field 'customer'; stmts: %+v", stmts)
	}

	// "amount" has no index.
	for _, s := range stmts[1:] {
		lc := strings.ToLower(s.SQL)
		if strings.Contains(lc, "amount") {
			t.Errorf("unexpected index reference for non-indexed field 'amount': %s", s.SQL)
		}
	}
}

func TestGenerateTableDDL_FullTextIndexGeneration(t *testing.T) {
	mt := &meta.MetaType{
		Name:   "Article",
		Module: "cms",
		Fields: []meta.FieldDef{
			{Name: "body", FieldType: meta.FieldTypeText, FullTextIndex: true},
		},
	}

	stmts := meta.GenerateTableDDL(mt)

	found := false
	for _, s := range stmts {
		lc := strings.ToLower(s.SQL)
		if strings.Contains(lc, "gin") && strings.Contains(lc, "tsvector") && strings.Contains(lc, "body") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected GIN tsvector index for FullTextIndex field 'body'; stmts: %+v", stmts)
	}
}

func TestGenerateTableDDL_ColumnsSanitized(t *testing.T) {
	mt := &meta.MetaType{
		Name:   "Customer",
		Module: "crm",
		Fields: []meta.FieldDef{
			{Name: "email", FieldType: meta.FieldTypeData},
		},
	}

	stmts := meta.GenerateTableDDL(mt)
	if len(stmts) == 0 {
		t.Fatal("no statements returned")
	}

	// Sanitized identifiers use double-quotes in PostgreSQL.
	if !strings.Contains(stmts[0].SQL, `"tab_customer"`) {
		t.Errorf("table name should be double-quoted; SQL:\n%s", stmts[0].SQL)
	}
}

// ── GenerateSystemTablesDDL ──────────────────────────────────────────────────

func TestGenerateSystemTablesDDL_AllTablesPresent(t *testing.T) {
	stmts := meta.GenerateSystemTablesDDL()

	// The base system tables plus outbox compatibility alters / indexes must
	// all be present.
	if len(stmts) != 23 {
		t.Errorf("GenerateSystemTablesDDL() returned %d statements; want 23", len(stmts))
		for i, s := range stmts {
			t.Logf("  [%d] %s", i, s.Comment)
		}
	}

	// Verify each expected table/index is present.
	expectedComments := []string{
		"tab_doctype",
		"tab_singles",
		"tab_version",
		"idx_version_ref",
		"tab_audit_log",
		"tab_audit_log_default",
		"tab_outbox",
		"status column to tab_outbox",
		"retry_count column to tab_outbox",
		"published_at column to tab_outbox",
		"processed column to tab_outbox",
		"backfill outbox status",
		"idx_outbox_pending",
		"tab_migration_log",
		"idx_migration_log_batch",
		"tab_webhook_log",
		"idx_webhook_log_doctype",
		"idx_webhook_log_event",
		"tab_file",
		"idx_file_attached",
		"tab_translation",
		"idx_translation_app",
		"idx_translation_lang",
	}
	for _, ec := range expectedComments {
		if _, ok := findStmtByComment(stmts, ec); !ok {
			t.Errorf("missing DDL statement for %q", ec)
		}
	}
}

func TestGenerateSystemTablesDDL_AuditLogPartitioned(t *testing.T) {
	stmts := meta.GenerateSystemTablesDDL()

	s, ok := findStmtByComment(stmts, "tab_audit_log")
	if !ok {
		t.Fatal("tab_audit_log DDL not found")
	}

	lc := strings.ToLower(s.SQL)
	if !strings.Contains(lc, "partition by range") {
		t.Errorf("tab_audit_log should be partitioned; SQL:\n%s", s.SQL)
	}
}

func TestGenerateSystemTablesDDL_DefaultPartitionPresent(t *testing.T) {
	stmts := meta.GenerateSystemTablesDDL()

	s, ok := findStmtByComment(stmts, "tab_audit_log_default")
	if !ok {
		t.Fatal("default partition DDL not found")
	}

	lc := strings.ToLower(s.SQL)
	if !strings.Contains(lc, "partition of tab_audit_log") {
		t.Errorf("default partition should reference tab_audit_log; SQL:\n%s", s.SQL)
	}
	if !strings.Contains(lc, "default") {
		t.Errorf("default partition should have DEFAULT keyword; SQL:\n%s", s.SQL)
	}
}

func TestGenerateSystemTablesDDL_VersionRefIndex(t *testing.T) {
	stmts := meta.GenerateSystemTablesDDL()

	s, ok := findStmtByComment(stmts, "idx_version_ref")
	if !ok {
		t.Fatal("idx_version_ref DDL not found")
	}

	lc := strings.ToLower(s.SQL)
	if !strings.Contains(lc, "idx_version_ref") {
		t.Errorf("index name should be idx_version_ref; SQL:\n%s", s.SQL)
	}
	if !strings.Contains(lc, "tab_version") {
		t.Errorf("index should be on tab_version; SQL:\n%s", s.SQL)
	}
}

func TestGenerateSystemTablesDDL_OutboxPresent(t *testing.T) {
	stmts := meta.GenerateSystemTablesDDL()

	s, ok := findStmtByComment(stmts, "tab_outbox")
	if !ok {
		t.Fatal("tab_outbox DDL not found in GenerateSystemTablesDDL()")
	}

	lc := strings.ToLower(s.SQL)
	for _, col := range []string{"event_type", "topic", "partition_key", "payload", "created_at", "status", "retry_count", "published_at", "processed"} {
		if !strings.Contains(lc, col) {
			t.Errorf("tab_outbox DDL missing expected column %q;\nSQL:\n%s", col, s.SQL)
		}
	}
	if !strings.Contains(lc, "jsonb") {
		t.Errorf("tab_outbox payload column should be JSONB; SQL:\n%s", s.SQL)
	}
}

func TestGenerateSystemTablesDDL_IdempotentStatements(t *testing.T) {
	stmts := meta.GenerateSystemTablesDDL()
	for _, s := range stmts {
		lc := strings.ToLower(s.SQL)
		// Each statement must be safe to run multiple times.
		isIdempotent := strings.Contains(lc, "if not exists") || strings.Contains(lc, "partition of") || strings.HasPrefix(lc, "update tab_outbox")
		if !isIdempotent {
			t.Errorf("non-idempotent DDL statement %q:\n%s", s.Comment, s.SQL)
		}
	}
}

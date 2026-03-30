package meta

import (
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// DDLStatement represents a single DDL statement along with a human-readable
// comment for logging and auditing purposes.
type DDLStatement struct {
	// SQL is the DDL statement to execute (e.g. "CREATE TABLE IF NOT EXISTS ...").
	SQL string
	// Comment describes the operation (e.g. "create table tab_sales_order").
	Comment string
}

// sanitizeIdent sanitizes a single PostgreSQL identifier using pgx.Identifier.
// This prevents SQL injection in dynamically generated DDL.
func sanitizeIdent(name string) string {
	return pgx.Identifier{name}.Sanitize()
}

// GenerateTableDDL generates CREATE TABLE IF NOT EXISTS DDL for the given MetaType,
// along with any necessary index statements. Returns nil for virtual and single
// MetaTypes (they do not require a document table).
//
// For child tables (IsChildTable == true), the column set differs from regular
// tables: it includes parent/parenttype/parentfield and omits docstatus,
// workflow_state, and the metadata suffix columns (_user_tags, etc.).
//
// Index statements are generated for:
//   - Fields with DBIndex == true: a regular B-tree index
//   - Fields with FullTextIndex == true: a GIN tsvector index
//   - Child tables: an automatic index on the parent column
func GenerateTableDDL(mt *MetaType) []DDLStatement {
	if mt.IsVirtual || mt.IsSingle {
		return nil
	}

	tableName := TableName(mt.Name)
	quotedTable := sanitizeIdent(tableName)

	// Select the appropriate standard column set.
	var stdCols []StandardColumnDef
	if mt.IsChildTable {
		stdCols = ChildStandardColumns()
	} else {
		stdCols = StandardColumns()
	}

	// Split standard columns into those before and after the user field insertion point.
	// The insertion point is the _extra column — user fields go immediately before it.
	var before, after []StandardColumnDef
	foundExtra := false
	for _, col := range stdCols {
		if col.Name == "_extra" {
			foundExtra = true
		}
		if !foundExtra {
			before = append(before, col)
		} else {
			after = append(after, col)
		}
	}

	// Build the full column list.
	var cols []string

	// 1. Standard prefix columns (before _extra).
	for _, col := range before {
		cols = append(cols, fmt.Sprintf("\t%s\t%s", sanitizeIdent(col.Name), col.DDL))
	}

	// 2. User-defined field columns. Skip Table/TableMultiSelect and layout types.
	for _, f := range mt.Fields {
		colType := ColumnType(f.FieldType)
		if colType == "" {
			continue
		}
		cols = append(cols, fmt.Sprintf("\t%s\t%s", sanitizeIdent(f.Name), colType))
	}

	// 3. Standard suffix columns (_extra and anything after it).
	for _, col := range after {
		cols = append(cols, fmt.Sprintf("\t%s\t%s", sanitizeIdent(col.Name), col.DDL))
	}

	createSQL := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (\n%s\n)",
		quotedTable,
		strings.Join(cols, ",\n"),
	)

	stmts := []DDLStatement{
		{
			SQL:     createSQL,
			Comment: fmt.Sprintf("create table %s", tableName),
		},
	}

	// Generate index statements.

	// Child tables get an automatic index on the parent column for fast child lookups.
	if mt.IsChildTable {
		idxName := fmt.Sprintf("idx_%s_parent", tableName)
		stmts = append(stmts, DDLStatement{
			SQL: fmt.Sprintf(
				"CREATE INDEX IF NOT EXISTS %s ON %s (%s)",
				sanitizeIdent(idxName), quotedTable, sanitizeIdent("parent"),
			),
			Comment: fmt.Sprintf("create parent index on %s", tableName),
		})
	}

	// Per-field indexes.
	for _, f := range mt.Fields {
		colType := ColumnType(f.FieldType)
		if colType == "" {
			continue
		}

		if f.DBIndex {
			idxName := fmt.Sprintf("idx_%s_%s", tableName, f.Name)
			stmts = append(stmts, DDLStatement{
				SQL: fmt.Sprintf(
					"CREATE INDEX IF NOT EXISTS %s ON %s (%s)",
					sanitizeIdent(idxName), quotedTable, sanitizeIdent(f.Name),
				),
				Comment: fmt.Sprintf("create index on %s.%s", tableName, f.Name),
			})
		}

		if f.FullTextIndex {
			idxName := fmt.Sprintf("idx_%s_%s_fts", tableName, f.Name)
			stmts = append(stmts, DDLStatement{
				SQL: fmt.Sprintf(
					"CREATE INDEX IF NOT EXISTS %s ON %s USING GIN (to_tsvector('english', %s))",
					sanitizeIdent(idxName), quotedTable, sanitizeIdent(f.Name),
				),
				Comment: fmt.Sprintf("create full-text index on %s.%s", tableName, f.Name),
			})
		}
	}

	return stmts
}

// GenerateSystemTablesDDL generates DDL for the 4 per-tenant system tables:
//   - tab_doctype: stores MetaType definitions as JSONB
//   - tab_singles: key-value store for Single DocTypes
//   - tab_version: change history for tracked documents
//   - tab_audit_log: immutable append-only audit trail (partitioned by timestamp)
//
// Also generates:
//   - idx_version_ref: index on tab_version(ref_doctype, docname)
//   - tab_audit_log_default: default partition for tab_audit_log so inserts work immediately
//
// All statements use CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS for idempotency.
//
// See MOCA_SYSTEM_DESIGN.md section 4.3 (lines 947-1005) for canonical DDL.
func GenerateSystemTablesDDL() []DDLStatement {
	return []DDLStatement{
		{
			SQL: `CREATE TABLE IF NOT EXISTS tab_doctype (
	"name"       TEXT PRIMARY KEY,
	"module"     TEXT NOT NULL,
	"definition" JSONB NOT NULL,
	"version"    INTEGER NOT NULL DEFAULT 1,
	"is_custom"  BOOLEAN DEFAULT false,
	"owner"      TEXT NOT NULL,
	"creation"   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	"modified"   TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`,
			Comment: "create system table tab_doctype",
		},
		{
			SQL: `CREATE TABLE IF NOT EXISTS tab_singles (
	"doctype" TEXT NOT NULL,
	"field"   TEXT NOT NULL,
	"value"   TEXT,
	PRIMARY KEY ("doctype", "field")
)`,
			Comment: "create system table tab_singles",
		},
		{
			SQL: `CREATE TABLE IF NOT EXISTS tab_version (
	"name"        TEXT PRIMARY KEY,
	"ref_doctype" TEXT NOT NULL,
	"docname"     TEXT NOT NULL,
	"data"        JSONB NOT NULL,
	"owner"       TEXT NOT NULL,
	"creation"    TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`,
			Comment: "create system table tab_version",
		},
		{
			SQL:     `CREATE INDEX IF NOT EXISTS idx_version_ref ON tab_version ("ref_doctype", "docname")`,
			Comment: "create index idx_version_ref on tab_version",
		},
		{
			// tab_audit_log is partitioned by RANGE(timestamp).
			// PostgreSQL requires the partition key to be part of the primary key,
			// so the PK is (id, timestamp) rather than just id.
			// Monthly partitions are automated in MS-12; a default partition is created
			// here so inserts succeed immediately.
			SQL: `CREATE TABLE IF NOT EXISTS tab_audit_log (
	"id"         BIGINT GENERATED ALWAYS AS IDENTITY,
	"doctype"    TEXT NOT NULL,
	"docname"    TEXT NOT NULL,
	"action"     TEXT NOT NULL,
	"user_id"    TEXT NOT NULL,
	"timestamp"  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	"ip_address" INET,
	"user_agent" TEXT,
	"changes"    JSONB,
	"request_id" TEXT,
	PRIMARY KEY ("id", "timestamp")
) PARTITION BY RANGE ("timestamp")`,
			Comment: "create system table tab_audit_log (partitioned)",
		},
		{
			SQL:     `CREATE TABLE IF NOT EXISTS tab_audit_log_default PARTITION OF tab_audit_log DEFAULT`,
			Comment: "create default partition tab_audit_log_default",
		},
	}
}

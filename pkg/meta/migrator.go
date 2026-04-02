package meta

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/osama1998H/moca/pkg/orm"
)

// Migrator handles schema evolution for MetaType-generated tables.
// It diffs MetaType definitions to produce ALTER TABLE DDL statements and
// applies them transactionally against a tenant's PostgreSQL database.
type Migrator struct {
	db     *orm.DBManager
	logger *slog.Logger
}

// NewMigrator creates a Migrator backed by the given DBManager and logger.
func NewMigrator(db *orm.DBManager, logger *slog.Logger) *Migrator {
	return &Migrator{db: db, logger: logger}
}

// Diff compares current and desired MetaType definitions and returns the DDL
// statements needed to migrate the database schema from current to desired.
//
// When current is nil, Diff delegates to GenerateTableDDL(desired) to produce
// a full CREATE TABLE statement.
//
// For virtual or single MetaTypes, Diff returns nil (no table to migrate).
//
// Diff detects the following changes in user-defined storable fields only
// (standard columns such as name, owner, creation are never diffed):
//   - Field added in desired but absent in current → ADD COLUMN IF NOT EXISTS
//   - Field removed from desired but present in current → DROP COLUMN IF EXISTS
//   - Field present in both but PostgreSQL column type changed → ALTER COLUMN TYPE
//   - DBIndex or FullTextIndex changed → CREATE/DROP INDEX IF NOT/EXISTS
//
// Note: field renames are not detected — they appear as a drop + add.
// Note: ALTER COLUMN TYPE for incompatible types (e.g. TEXT → INTEGER) will fail
// at Apply time if existing rows contain incompatible data; data migration is
// the caller's responsibility.
func (m *Migrator) Diff(current, desired *MetaType) []DDLStatement {
	if current == nil {
		return GenerateTableDDL(desired)
	}
	if desired.IsVirtual || desired.IsSingle {
		return nil
	}

	tableName := TableName(desired.Name)
	quotedTable := sanitizeIdent(tableName)

	// Build field maps for storable user fields only (ColumnType != "").
	currentFields := storableFieldMap(current)
	desiredFields := storableFieldMap(desired)

	var stmts []DDLStatement

	// ADD COLUMN: fields in desired but not in current.
	for name, f := range desiredFields {
		if _, exists := currentFields[name]; !exists {
			colType := ColumnType(f.FieldType)
			stmts = append(stmts, DDLStatement{
				SQL: fmt.Sprintf(
					"ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s",
					quotedTable, sanitizeIdent(f.Name), colType,
				),
				Comment: fmt.Sprintf("add column %s to %s", f.Name, tableName),
			})
			// Also create any indexes for the new field.
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
	}

	// DROP COLUMN: fields in current but not in desired.
	for name, f := range currentFields {
		if _, exists := desiredFields[name]; !exists {
			stmts = append(stmts, DDLStatement{
				SQL: fmt.Sprintf(
					"ALTER TABLE %s DROP COLUMN IF EXISTS %s",
					quotedTable, sanitizeIdent(f.Name),
				),
				Comment: fmt.Sprintf("drop column %s from %s", f.Name, tableName),
			})
		}
	}

	// ALTER COLUMN TYPE and index changes: fields present in both.
	for name, df := range desiredFields {
		cf, exists := currentFields[name]
		if !exists {
			continue
		}

		// Type change.
		desiredType := ColumnType(df.FieldType)
		currentType := ColumnType(cf.FieldType)
		if desiredType != currentType {
			stmts = append(stmts, DDLStatement{
				SQL: fmt.Sprintf(
					"ALTER TABLE %s ALTER COLUMN %s TYPE %s USING %s::%s",
					quotedTable, sanitizeIdent(df.Name), desiredType,
					sanitizeIdent(df.Name), desiredType,
				),
				Comment: fmt.Sprintf("change type of %s.%s from %s to %s",
					tableName, df.Name, currentType, desiredType),
			})
		}

		// Index changes.
		idxName := fmt.Sprintf("idx_%s_%s", tableName, name)
		if df.DBIndex && !cf.DBIndex {
			stmts = append(stmts, DDLStatement{
				SQL: fmt.Sprintf(
					"CREATE INDEX IF NOT EXISTS %s ON %s (%s)",
					sanitizeIdent(idxName), quotedTable, sanitizeIdent(name),
				),
				Comment: fmt.Sprintf("create index on %s.%s", tableName, name),
			})
		} else if !df.DBIndex && cf.DBIndex {
			stmts = append(stmts, DDLStatement{
				SQL:     fmt.Sprintf("DROP INDEX IF EXISTS %s", sanitizeIdent(idxName)),
				Comment: fmt.Sprintf("drop index on %s.%s", tableName, name),
			})
		}

		ftsIdxName := fmt.Sprintf("idx_%s_%s_fts", tableName, name)
		if df.FullTextIndex && !cf.FullTextIndex {
			stmts = append(stmts, DDLStatement{
				SQL: fmt.Sprintf(
					"CREATE INDEX IF NOT EXISTS %s ON %s USING GIN (to_tsvector('english', %s))",
					sanitizeIdent(ftsIdxName), quotedTable, sanitizeIdent(name),
				),
				Comment: fmt.Sprintf("create full-text index on %s.%s", tableName, name),
			})
		} else if !df.FullTextIndex && cf.FullTextIndex {
			stmts = append(stmts, DDLStatement{
				SQL:     fmt.Sprintf("DROP INDEX IF EXISTS %s", sanitizeIdent(ftsIdxName)),
				Comment: fmt.Sprintf("drop full-text index on %s.%s", tableName, name),
			})
		}
	}

	return stmts
}

// Apply executes all provided DDL statements inside a single transaction on the
// given tenant site's database pool. All statements commit atomically; if any
// statement fails the entire transaction is rolled back.
//
// Returns nil immediately when statements is empty.
func (m *Migrator) Apply(ctx context.Context, site string, statements []DDLStatement) error {
	if len(statements) == 0 {
		return nil
	}

	pool, err := m.db.ForSite(ctx, site)
	if err != nil {
		return fmt.Errorf("migrator apply: get pool for site %q: %w", site, err)
	}

	return applyStatements(ctx, pool, statements, m.logger)
}

// EnsureMetaTables creates the per-tenant system tables (tab_doctype, tab_singles,
// tab_version, tab_audit_log) and their required indexes if they do not already
// exist. This is idempotent and safe to call on every startup.
func (m *Migrator) EnsureMetaTables(ctx context.Context, site string) error {
	stmts := GenerateSystemTablesDDL()
	if err := m.Apply(ctx, site, stmts); err != nil {
		return fmt.Errorf("ensure meta tables for site %q: %w", site, err)
	}
	return nil
}

// EnsureRLSPolicies drops any existing Moca-managed RLS policies on the
// MetaType's table and recreates them from the current PermRule definitions.
// This is idempotent and safe to call on every MetaType registration or
// permission change.
//
// For MetaTypes that have no row-level match rules, the function still enables
// RLS and creates the admin bypass policy (defense-in-depth). For virtual,
// single, or child MetaTypes, the function is a no-op.
func (m *Migrator) EnsureRLSPolicies(ctx context.Context, site string, mt *MetaType) error {
	dropStmts := GenerateDropRLSPolicies(mt)
	createStmts := GenerateRLSPolicies(mt)

	if len(dropStmts) == 0 && len(createStmts) == 0 {
		return nil
	}

	stmts := make([]DDLStatement, 0, len(dropStmts)+len(createStmts))
	stmts = append(stmts, dropStmts...)
	stmts = append(stmts, createStmts...)

	if err := m.Apply(ctx, site, stmts); err != nil {
		return fmt.Errorf("ensure RLS policies for %q on site %q: %w", mt.Name, site, err)
	}
	return nil
}

// storableFieldMap builds a map of field name → FieldDef for all storable
// user-defined fields (ColumnType != "") in mt. Standard columns are not included.
func storableFieldMap(mt *MetaType) map[string]FieldDef {
	m := make(map[string]FieldDef, len(mt.Fields))
	for _, f := range mt.Fields {
		if ColumnType(f.FieldType) != "" {
			m[f.Name] = f
		}
	}
	return m
}

// applyStatements executes DDL statements inside a single transaction on pool.
// Each statement is logged at Debug level before execution.
func applyStatements(ctx context.Context, pool *pgxpool.Pool, statements []DDLStatement, logger *slog.Logger) error {
	return orm.WithTransaction(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		for _, stmt := range statements {
			logger.DebugContext(ctx, "apply DDL", "sql", stmt.SQL, "comment", stmt.Comment)
			if _, err := tx.Exec(ctx, stmt.SQL); err != nil {
				return fmt.Errorf("execute DDL %q: %w", stmt.Comment, err)
			}
		}
		return nil
	})
}

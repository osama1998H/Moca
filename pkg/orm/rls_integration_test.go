//go:build integration

package orm_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/orm"
)

// ── RLS test infrastructure ─────────────────────────────────────────────────

const (
	rlsTestSchema = "tenant_rls_integ"
	// rlsAppRole is a non-superuser role for RLS testing.
	// Superusers always bypass RLS in PostgreSQL, so we need a regular role.
	rlsAppRole     = "moca_rls_test"
	rlsAppPassword = "rls_test_pw"
)

// rlsFixtureMT is the MetaType used for RLS testing.
var rlsFixtureMT = &meta.MetaType{
	Name: "RLSTestOrder",
	Permissions: []meta.PermRule{
		{
			Role:        "Sales User",
			DocTypePerm: 1,
			MatchField:  "company",
			MatchValue:  "company",
		},
		{
			Role:        "Territory Manager",
			DocTypePerm: 1,
			MatchField:  "territory",
			MatchValue:  "territory",
		},
	},
	Fields: []meta.FieldDef{
		{Name: "company", FieldType: "Data"},
		{Name: "territory", FieldType: "Data"},
		{Name: "title", FieldType: "Data"},
	},
}

// setupRLS creates the RLS test schema, a non-superuser role, tables, policies,
// and seed data. Returns a pool connected as the non-superuser (so RLS applies).
// Uses the shared adminPool from postgres_test.go TestMain.
func setupRLS(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	schemaQ := pgx.Identifier{rlsTestSchema}.Sanitize()

	// Create non-superuser role for RLS testing.
	// DROP/CREATE is idempotent via IF NOT EXISTS.
	adminPool.Exec(ctx, fmt.Sprintf(
		"DROP ROLE IF EXISTS %s", pgx.Identifier{rlsAppRole}.Sanitize()))
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(
		"CREATE ROLE %s WITH LOGIN PASSWORD '%s'",
		pgx.Identifier{rlsAppRole}.Sanitize(), rlsAppPassword)); err != nil {
		t.Fatalf("create RLS test role: %v", err)
	}

	// Create schema.
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+schemaQ); err != nil {
		t.Fatalf("create RLS schema: %v", err)
	}

	// Grant schema usage and table creation to the app role.
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(
		"GRANT USAGE ON SCHEMA %s TO %s",
		schemaQ, pgx.Identifier{rlsAppRole}.Sanitize())); err != nil {
		t.Fatalf("grant schema usage: %v", err)
	}

	tableName := meta.TableName(rlsFixtureMT.Name)
	quotedTable := pgx.Identifier{tableName}.Sanitize()

	// Use adminPool to set search_path and create/populate table.
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(`
		SET search_path TO %s, public;
		CREATE TABLE IF NOT EXISTS %s (
			"name"      TEXT PRIMARY KEY,
			"company"   TEXT,
			"territory" TEXT,
			"title"     TEXT
		)
	`, schemaQ, quotedTable)); err != nil {
		t.Fatalf("create RLS test table: %v", err)
	}

	// Grant table access to the app role.
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(
		"GRANT SELECT, INSERT, UPDATE, DELETE ON %s.%s TO %s",
		schemaQ, quotedTable, pgx.Identifier{rlsAppRole}.Sanitize())); err != nil {
		t.Fatalf("grant table access: %v", err)
	}

	// Apply RLS policies (as superuser).
	stmts := meta.GenerateRLSPolicies(rlsFixtureMT)
	if err := adminPool.AcquireFunc(ctx, func(conn *pgxpool.Conn) error {
		// Set search_path for this connection.
		if _, err := conn.Exec(ctx, fmt.Sprintf("SET search_path TO %s, public", schemaQ)); err != nil {
			return err
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		for _, stmt := range stmts {
			if _, execErr := tx.Exec(ctx, stmt.SQL); execErr != nil {
				return fmt.Errorf("apply RLS %q: %w", stmt.Comment, execErr)
			}
		}
		return tx.Commit(ctx)
	}); err != nil {
		t.Fatalf("apply RLS policies: %v", err)
	}

	// Insert seed data.
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(`
		SET search_path TO %s, public;
		INSERT INTO %s ("name", "company", "territory", "title") VALUES
			('SO-001', 'Acme Corp', 'West', 'Acme Order 1'),
			('SO-002', 'Acme Corp', 'West', 'Acme Order 2'),
			('SO-003', 'Beta Inc', 'East', 'Beta Order 1'),
			('SO-004', 'Beta Inc', 'North', 'Beta Order 2'),
			('SO-005', 'Gamma LLC', 'West', 'Gamma Order 1')
		ON CONFLICT DO NOTHING
	`, schemaQ, quotedTable)); err != nil {
		t.Fatalf("insert RLS test rows: %v", err)
	}

	// Create a connection pool as the non-superuser role.
	host := os.Getenv("PG_HOST")
	if host == "" {
		host = defaultHost
	}
	connStr := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		rlsAppRole, rlsAppPassword, host, defaultPort, defaultDB,
	)
	poolCfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Fatalf("parse RLS pool config: %v", err)
	}
	poolCfg.MaxConns = 5
	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx,
			fmt.Sprintf("SET search_path TO %s, public", schemaQ))
		return err
	}
	rlsPool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatalf("create RLS test pool: %v", err)
	}

	// Register cleanup.
	t.Cleanup(func() {
		rlsPool.Close()
		cleanupCtx := context.Background()
		adminPool.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+schemaQ+" CASCADE")
		adminPool.Exec(cleanupCtx, fmt.Sprintf(
			"DROP ROLE IF EXISTS %s", pgx.Identifier{rlsAppRole}.Sanitize()))
	})

	return rlsPool
}

// rlsRowCount counts rows visible in the RLS test table within a transaction.
func rlsRowCount(t *testing.T, tx pgx.Tx) int {
	t.Helper()
	tableName := meta.TableName(rlsFixtureMT.Name)
	var count int
	err := tx.QueryRow(context.Background(),
		fmt.Sprintf("SELECT COUNT(*) FROM %s", pgx.Identifier{tableName}.Sanitize()),
	).Scan(&count)
	if err != nil {
		t.Fatalf("query count: %v", err)
	}
	return count
}

// ── RLS Integration Tests ───────────────────────────────────────────────────

func TestRLS_SelectFiltering(t *testing.T) {
	pool := setupRLS(t)
	ctx := context.Background()

	err := orm.WithTransaction(ctx, pool, func(txCtx context.Context, tx pgx.Tx) error {
		if err := orm.SetUserSessionVars(txCtx, tx, "alice@acme.com",
			[]string{"Sales User"},
			map[string]string{"company": "Acme Corp"},
		); err != nil {
			return err
		}
		count := rlsRowCount(t, tx)
		if count != 2 {
			t.Errorf("expected 2 Acme rows, got %d", count)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

func TestRLS_AdminBypass(t *testing.T) {
	pool := setupRLS(t)
	ctx := context.Background()

	err := orm.WithTransaction(ctx, pool, func(txCtx context.Context, tx pgx.Tx) error {
		if err := orm.SetUserSessionVars(txCtx, tx, "admin@example.com",
			[]string{"Administrator"},
			map[string]string{"company": "Acme Corp"},
		); err != nil {
			return err
		}
		count := rlsRowCount(t, tx)
		if count != 5 {
			t.Errorf("admin should see all 5 rows, got %d", count)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

func TestRLS_NoSessionVars(t *testing.T) {
	pool := setupRLS(t)
	ctx := context.Background()

	err := orm.WithTransaction(ctx, pool, func(txCtx context.Context, tx pgx.Tx) error {
		count := rlsRowCount(t, tx)
		if count != 0 {
			t.Errorf("expected 0 rows with no session vars, got %d", count)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

func TestRLS_UpdateRestricted(t *testing.T) {
	pool := setupRLS(t)
	ctx := context.Background()
	tableName := meta.TableName(rlsFixtureMT.Name)
	quotedTable := pgx.Identifier{tableName}.Sanitize()

	err := orm.WithTransaction(ctx, pool, func(txCtx context.Context, tx pgx.Tx) error {
		if err := orm.SetUserSessionVars(txCtx, tx, "alice@acme.com",
			[]string{"Sales User"},
			map[string]string{"company": "Acme Corp"},
		); err != nil {
			return err
		}

		// Update own row — should succeed.
		tag, err := tx.Exec(txCtx, fmt.Sprintf(
			`UPDATE %s SET "title" = 'Updated' WHERE "name" = 'SO-001'`, quotedTable))
		if err != nil {
			return fmt.Errorf("update own row: %w", err)
		}
		if tag.RowsAffected() != 1 {
			t.Errorf("expected 1 row updated, got %d", tag.RowsAffected())
		}

		// Update other company's row — 0 rows (RLS hides it).
		tag, err = tx.Exec(txCtx, fmt.Sprintf(
			`UPDATE %s SET "title" = 'Hacked' WHERE "name" = 'SO-003'`, quotedTable))
		if err != nil {
			return fmt.Errorf("update other row: %w", err)
		}
		if tag.RowsAffected() != 0 {
			t.Errorf("expected 0 rows updated for other company, got %d", tag.RowsAffected())
		}

		return nil
	})
	if err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

func TestRLS_DeleteRestricted(t *testing.T) {
	pool := setupRLS(t)
	ctx := context.Background()
	tableName := meta.TableName(rlsFixtureMT.Name)
	quotedTable := pgx.Identifier{tableName}.Sanitize()

	err := orm.WithTransaction(ctx, pool, func(txCtx context.Context, tx pgx.Tx) error {
		if err := orm.SetUserSessionVars(txCtx, tx, "bob@beta.com",
			[]string{"Sales User"},
			map[string]string{"company": "Beta Inc"},
		); err != nil {
			return err
		}

		// Delete other company's row — 0 rows.
		tag, err := tx.Exec(txCtx, fmt.Sprintf(
			`DELETE FROM %s WHERE "name" = 'SO-001'`, quotedTable))
		if err != nil {
			return fmt.Errorf("delete other row: %w", err)
		}
		if tag.RowsAffected() != 0 {
			t.Errorf("expected 0 rows deleted for other company, got %d", tag.RowsAffected())
		}

		return nil
	})
	if err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

func TestRLS_MultiPolicy_OR(t *testing.T) {
	pool := setupRLS(t)
	ctx := context.Background()

	err := orm.WithTransaction(ctx, pool, func(txCtx context.Context, tx pgx.Tx) error {
		// Territory Manager with territory=West sees all West rows across companies.
		if err := orm.SetUserSessionVars(txCtx, tx, "charlie@example.com",
			[]string{"Territory Manager"},
			map[string]string{"territory": "West"},
		); err != nil {
			return err
		}
		count := rlsRowCount(t, tx)
		// SO-001 (Acme, West), SO-002 (Acme, West), SO-005 (Gamma, West) = 3
		if count != 3 {
			t.Errorf("expected 3 rows for territory=West, got %d", count)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

func TestRLS_CombinedRoles(t *testing.T) {
	pool := setupRLS(t)
	ctx := context.Background()

	err := orm.WithTransaction(ctx, pool, func(txCtx context.Context, tx pgx.Tx) error {
		// User with both company=Acme AND territory=East.
		// Company policy: SO-001, SO-002. Territory policy: SO-003.
		// OR across policies → 3 rows.
		if err := orm.SetUserSessionVars(txCtx, tx, "dave@acme.com",
			[]string{"Sales User", "Territory Manager"},
			map[string]string{"company": "Acme Corp", "territory": "East"},
		); err != nil {
			return err
		}
		count := rlsRowCount(t, tx)
		if count != 3 {
			t.Errorf("expected 3 rows (2 Acme + 1 East), got %d", count)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

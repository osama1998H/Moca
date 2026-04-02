//go:build integration

package orm_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/osama1998H/moca/pkg/orm"
)

// Note: adminPool and TestMain are defined in postgres_test.go.
// All schema tests reuse the same adminPool and docker-compose PostgreSQL instance.

// ── Test 1: Idempotency ───────────────────────────────────────────────────────

// TestEnsureSystemSchema_Idempotent calls EnsureSystemSchema twice with the same
// schema name and verifies the second call succeeds (no error due to IF NOT EXISTS).
func TestEnsureSystemSchema_Idempotent(t *testing.T) {
	ctx := context.Background()
	schema := "moca_system_test_idempotent"

	t.Cleanup(func() {
		_, _ = adminPool.Exec(ctx, fmt.Sprintf(
			"DROP SCHEMA IF EXISTS %s CASCADE",
			pgx.Identifier{schema}.Sanitize(),
		))
	})

	if err := orm.EnsureSystemSchema(ctx, adminPool, schema); err != nil {
		t.Fatalf("first EnsureSystemSchema: %v", err)
	}

	if err := orm.EnsureSystemSchema(ctx, adminPool, schema); err != nil {
		t.Fatalf("second EnsureSystemSchema (idempotency check): %v", err)
	}

	t.Logf("TestEnsureSystemSchema_Idempotent: two consecutive calls succeeded")
}

// ── Test 2: Table and Column Existence ───────────────────────────────────────

// TestEnsureSystemSchema_TablesExist verifies that all three system tables are
// created with the expected structure.
func TestEnsureSystemSchema_TablesExist(t *testing.T) {
	ctx := context.Background()
	schema := "moca_system_test_tables"

	t.Cleanup(func() {
		_, _ = adminPool.Exec(ctx, fmt.Sprintf(
			"DROP SCHEMA IF EXISTS %s CASCADE",
			pgx.Identifier{schema}.Sanitize(),
		))
	})

	if err := orm.EnsureSystemSchema(ctx, adminPool, schema); err != nil {
		t.Fatalf("EnsureSystemSchema: %v", err)
	}

	// Verify all three tables exist via information_schema.
	tables := []string{"sites", "apps", "site_apps"}
	for _, tbl := range tables {
		var exists bool
		if err := adminPool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = $1 AND table_name = $2
			)`, schema, tbl).Scan(&exists); err != nil {
			t.Fatalf("query table %s existence: %v", tbl, err)
		}
		if !exists {
			t.Errorf("table %s.%s does not exist", schema, tbl)
		}
	}

	// Verify all expected columns exist on the sites table.
	expectedSitesCols := []string{
		"name", "db_schema", "status", "plan",
		"config", "admin_email", "created_at", "modified_at",
	}
	for _, col := range expectedSitesCols {
		var exists bool
		if err := adminPool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = $1 AND table_name = 'sites' AND column_name = $2
			)`, schema, col).Scan(&exists); err != nil {
			t.Fatalf("query column sites.%s: %v", col, err)
		}
		if !exists {
			t.Errorf("column %s.sites.%s does not exist", schema, col)
		}
	}

	// Verify all expected columns exist on the apps table.
	expectedAppsCols := []string{
		"name", "version", "title", "description",
		"publisher", "dependencies", "manifest",
	}
	for _, col := range expectedAppsCols {
		var exists bool
		if err := adminPool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = $1 AND table_name = 'apps' AND column_name = $2
			)`, schema, col).Scan(&exists); err != nil {
			t.Fatalf("query column apps.%s: %v", col, err)
		}
		if !exists {
			t.Errorf("column %s.apps.%s does not exist", schema, col)
		}
	}

	t.Logf("TestEnsureSystemSchema_TablesExist: all 3 tables and all columns verified")
}

// ── Test 3: FK Constraints ────────────────────────────────────────────────────

// TestEnsureSystemSchema_FKConstraints verifies that the site_apps foreign key
// constraints are enforced: insertions with non-existent parent rows must fail,
// and insertions with valid parents must succeed.
func TestEnsureSystemSchema_FKConstraints(t *testing.T) {
	ctx := context.Background()
	schema := "moca_system_test_fk"
	quotedSchema := pgx.Identifier{schema}.Sanitize()

	t.Cleanup(func() {
		_, _ = adminPool.Exec(ctx, fmt.Sprintf(
			"DROP SCHEMA IF EXISTS %s CASCADE", quotedSchema,
		))
	})

	if err := orm.EnsureSystemSchema(ctx, adminPool, schema); err != nil {
		t.Fatalf("EnsureSystemSchema: %v", err)
	}

	// Inserting into site_apps with non-existent references must violate the FK.
	_, err := adminPool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s.site_apps (site_name, app_name, app_version)
		VALUES ('nonexistent_site', 'nonexistent_app', '1.0.0')`,
		quotedSchema,
	))
	if err == nil {
		t.Fatal("expected FK violation inserting into site_apps with nonexistent references, got nil")
	}
	t.Logf("FK constraint enforced (expected): %v", err)

	// Insert a valid site row.
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s.sites (name, db_schema, admin_email)
		VALUES ('test-site', 'tenant_test', 'admin@test.com')`,
		quotedSchema,
	)); err != nil {
		t.Fatalf("insert site: %v", err)
	}

	// Insert a valid app row.
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s.apps (name, version, manifest)
		VALUES ('core', '1.0.0', '{}')`,
		quotedSchema,
	)); err != nil {
		t.Fatalf("insert app: %v", err)
	}

	// Insert a valid site_apps row referencing existing parents — must succeed.
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s.site_apps (site_name, app_name, app_version)
		VALUES ('test-site', 'core', '1.0.0')`,
		quotedSchema,
	)); err != nil {
		t.Fatalf("insert site_app with valid FKs: %v", err)
	}

	t.Log("TestEnsureSystemSchema_FKConstraints: FK enforcement verified")
}

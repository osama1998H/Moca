package orm

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EnsureSystemSchema creates the system schema and its three core tables
// (sites, apps, site_apps) if they do not already exist. The entire operation
// runs inside a single transaction via WithTransaction.
//
// The function is idempotent: calling it multiple times has no effect beyond
// the first successful run. All DDL uses CREATE ... IF NOT EXISTS.
//
// The systemSchema name is sanitized via pgx.Identifier to prevent SQL
// injection. All table names and FK REFERENCES use the sanitized name so the
// function works correctly with any valid PostgreSQL schema name.
//
// See MOCA_SYSTEM_DESIGN.md §4.2 (lines 852-887) for the canonical DDL.
func EnsureSystemSchema(ctx context.Context, pool *pgxpool.Pool, systemSchema string) error {
	return WithTransaction(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		schema := pgx.Identifier{systemSchema}.Sanitize()

		// 1. Create the schema itself.
		if _, err := tx.Exec(ctx,
			fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schema),
		); err != nil {
			return fmt.Errorf("create schema %s: %w", systemSchema, err)
		}

		// 2. Create the sites table.
		// %[1]s references the first (and only) argument, used for the schema name.
		if _, err := tx.Exec(ctx, fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %[1]s.sites (
				name        TEXT PRIMARY KEY,
				db_schema   TEXT NOT NULL UNIQUE,
				status      TEXT NOT NULL DEFAULT 'active',
				plan        TEXT,
				config      JSONB NOT NULL DEFAULT '{}',
				admin_email TEXT NOT NULL,
				created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)`, schema),
		); err != nil {
			return fmt.Errorf("create %s.sites: %w", systemSchema, err)
		}

		// 3. Create the apps table.
		if _, err := tx.Exec(ctx, fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %[1]s.apps (
				name         TEXT PRIMARY KEY,
				version      TEXT NOT NULL,
				title        TEXT,
				description  TEXT,
				publisher    TEXT,
				dependencies JSONB NOT NULL DEFAULT '[]',
				manifest     JSONB NOT NULL
			)`, schema),
		); err != nil {
			return fmt.Errorf("create %s.apps: %w", systemSchema, err)
		}

		// 4. Create the site_apps junction table.
		// FK REFERENCES use the same sanitized schema name so the function works
		// with any schema name, not just the default "moca_system".
		if _, err := tx.Exec(ctx, fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %[1]s.site_apps (
				site_name    TEXT REFERENCES %[1]s.sites(name) ON UPDATE CASCADE,
				app_name     TEXT REFERENCES %[1]s.apps(name),
				installed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				app_version  TEXT NOT NULL,
				PRIMARY KEY (site_name, app_name)
			)`, schema),
		); err != nil {
			return fmt.Errorf("create %s.site_apps: %w", systemSchema, err)
		}

		return nil
	})
}

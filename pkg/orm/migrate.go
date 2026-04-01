package orm

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AppMigration represents a single SQL migration belonging to an app.
// DependsOn entries use "appName:version" format for cross-app dependencies.
type AppMigration struct {
	AppName   string
	Version   string
	UpSQL     string
	DownSQL   string
	DependsOn []string
}

// migrationKey returns a unique identifier for this migration in "app:version" format.
func (m AppMigration) migrationKey() string {
	return m.AppName + ":" + m.Version
}

// MigrateOptions controls migration execution behavior.
type MigrateOptions struct {
	Skip   string // "app:version" to skip
	Step   int    // max migrations to apply (0 = all)
	DryRun bool
}

// RollbackOptions controls rollback behavior.
type RollbackOptions struct {
	DryRun bool
	Step   int // number of batches to rollback (default: 1)
}

// MigrateResult reports what happened during a migrate or rollback operation.
type MigrateResult struct {
	Applied []AppMigration
	Skipped []AppMigration
	Batch   int
	DryRun  bool
}

// DDLPreview represents a migration that would be executed in dry-run mode.
type DDLPreview struct {
	AppName string
	Version string
	SQL     string
}

// MigrationRunner executes SQL migrations against a tenant's database with
// version tracking in tab_migration_log. Migrations are grouped into batches
// for batch-level rollback and ordered by DependsOn declarations using
// topological sort.
type MigrationRunner struct {
	db     *DBManager
	logger *slog.Logger
}

// NewMigrationRunner creates a MigrationRunner backed by the given DBManager.
func NewMigrationRunner(db *DBManager, logger *slog.Logger) *MigrationRunner {
	return &MigrationRunner{db: db, logger: logger}
}

// Pending returns the subset of migrations that have not yet been applied to the
// given site, topologically sorted by DependsOn. Already-applied migrations are
// treated as satisfied dependencies.
func (r *MigrationRunner) Pending(ctx context.Context, site string, migrations []AppMigration) ([]AppMigration, error) {
	pool, err := r.db.ForSite(ctx, site)
	if err != nil {
		return nil, fmt.Errorf("pending: get pool for site %q: %w", site, err)
	}

	applied, err := r.loadApplied(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("pending: load applied migrations: %w", err)
	}

	var pending []AppMigration
	for _, m := range migrations {
		if !applied[m.migrationKey()] {
			pending = append(pending, m)
		}
	}

	sorted, err := topoSortMigrations(pending, applied)
	if err != nil {
		return nil, fmt.Errorf("pending: %w", err)
	}

	return sorted, nil
}

// Apply executes pending migrations in topological order within a single
// transaction. An advisory lock is acquired to prevent concurrent migration
// runs on the same site. Each applied migration is recorded in
// tab_migration_log with a shared batch number.
func (r *MigrationRunner) Apply(ctx context.Context, site string, migrations []AppMigration, opts MigrateOptions) (*MigrateResult, error) {
	pending, err := r.Pending(ctx, site, migrations)
	if err != nil {
		return nil, err
	}

	result := &MigrateResult{DryRun: opts.DryRun}

	// Apply skip filter.
	if opts.Skip != "" {
		pending, result.Skipped = applySkip(pending, opts.Skip)
	}

	// Apply step limit.
	if opts.Step > 0 && opts.Step < len(pending) {
		pending = pending[:opts.Step]
	}

	if opts.DryRun {
		result.Applied = pending
		return result, nil
	}

	if len(pending) == 0 {
		return result, nil
	}

	pool, err := r.db.ForSite(ctx, site)
	if err != nil {
		return nil, fmt.Errorf("apply: get pool for site %q: %w", site, err)
	}

	txErr := WithTransaction(ctx, pool, func(txCtx context.Context, tx pgx.Tx) error {
		// Acquire advisory lock to serialize concurrent migrations on this site.
		if _, execErr := tx.Exec(txCtx, "SELECT pg_advisory_xact_lock(hashtext($1))", site); execErr != nil {
			return fmt.Errorf("acquire advisory lock: %w", execErr)
		}

		// Determine next batch number.
		var batch int
		if scanErr := tx.QueryRow(txCtx,
			"SELECT COALESCE(MAX(batch), 0) + 1 FROM tab_migration_log",
		).Scan(&batch); scanErr != nil {
			return fmt.Errorf("determine batch number: %w", scanErr)
		}
		result.Batch = batch

		for _, m := range pending {
			r.logger.DebugContext(txCtx, "apply migration",
				slog.String("app", m.AppName),
				slog.String("version", m.Version),
			)

			if _, execErr := tx.Exec(txCtx, m.UpSQL); execErr != nil {
				return fmt.Errorf("execute migration %s:%s UP: %w", m.AppName, m.Version, execErr)
			}

			if _, execErr := tx.Exec(txCtx, `
				INSERT INTO tab_migration_log (app, version, batch, up_sql, down_sql)
				VALUES ($1, $2, $3, $4, $5)`,
				m.AppName, m.Version, batch, m.UpSQL, m.DownSQL,
			); execErr != nil {
				return fmt.Errorf("record migration %s:%s: %w", m.AppName, m.Version, execErr)
			}

			result.Applied = append(result.Applied, m)
		}

		return nil
	})
	if txErr != nil {
		return nil, fmt.Errorf("apply migrations: %w", txErr)
	}

	return result, nil
}

// Rollback reverses the most recent migration batch(es) by executing DOWN SQL
// in reverse application order. Returns an error if any migration lacks DOWN SQL.
func (r *MigrationRunner) Rollback(ctx context.Context, site string, opts RollbackOptions) (*MigrateResult, error) {
	pool, err := r.db.ForSite(ctx, site)
	if err != nil {
		return nil, fmt.Errorf("rollback: get pool for site %q: %w", site, err)
	}

	step := opts.Step
	if step <= 0 {
		step = 1
	}

	// Find the migrations to rollback: latest N batches in reverse order.
	toRollback, queryErr := r.loadBatchesForRollback(ctx, pool, step)
	if queryErr != nil {
		return nil, queryErr
	}

	result := &MigrateResult{DryRun: opts.DryRun}

	if opts.DryRun {
		result.Applied = toRollback
		return result, nil
	}

	if len(toRollback) == 0 {
		return result, nil
	}

	// Verify all migrations have DOWN SQL before starting.
	for _, m := range toRollback {
		if m.DownSQL == "" {
			return nil, fmt.Errorf("rollback: migration %s:%s has no DOWN SQL", m.AppName, m.Version)
		}
	}

	txErr := WithTransaction(ctx, pool, func(txCtx context.Context, tx pgx.Tx) error {
		if _, execErr := tx.Exec(txCtx, "SELECT pg_advisory_xact_lock(hashtext($1))", site); execErr != nil {
			return fmt.Errorf("acquire advisory lock: %w", execErr)
		}

		for _, m := range toRollback {
			r.logger.DebugContext(txCtx, "rollback migration",
				slog.String("app", m.AppName),
				slog.String("version", m.Version),
			)

			if _, execErr := tx.Exec(txCtx, m.DownSQL); execErr != nil {
				return fmt.Errorf("execute migration %s:%s DOWN: %w", m.AppName, m.Version, execErr)
			}

			if _, execErr := tx.Exec(txCtx,
				"DELETE FROM tab_migration_log WHERE app = $1 AND version = $2",
				m.AppName, m.Version,
			); execErr != nil {
				return fmt.Errorf("delete migration log %s:%s: %w", m.AppName, m.Version, execErr)
			}

			result.Applied = append(result.Applied, m)
		}

		return nil
	})
	if txErr != nil {
		return nil, fmt.Errorf("rollback migrations: %w", txErr)
	}

	return result, nil
}

// DryRun returns previews of pending migrations without executing them.
func (r *MigrationRunner) DryRun(ctx context.Context, site string, migrations []AppMigration) ([]DDLPreview, error) {
	pending, err := r.Pending(ctx, site, migrations)
	if err != nil {
		return nil, err
	}

	previews := make([]DDLPreview, len(pending))
	for i, m := range pending {
		previews[i] = DDLPreview{
			AppName: m.AppName,
			Version: m.Version,
			SQL:     m.UpSQL,
		}
	}
	return previews, nil
}

// loadBatchesForRollback queries the latest N batches from tab_migration_log
// in reverse application order for rollback.
func (r *MigrationRunner) loadBatchesForRollback(ctx context.Context, pool *pgxpool.Pool, step int) ([]AppMigration, error) {
	rows, err := pool.Query(ctx, `
		SELECT app, version, batch, up_sql, down_sql
		FROM tab_migration_log
		WHERE batch > (SELECT COALESCE(MAX(batch), 0) - $1 FROM tab_migration_log)
		ORDER BY id DESC`, step)
	if err != nil {
		return nil, fmt.Errorf("rollback: query batches: %w", err)
	}
	defer rows.Close()

	var migrations []AppMigration
	for rows.Next() {
		var m AppMigration
		var batch int
		if scanErr := rows.Scan(&m.AppName, &m.Version, &batch, &m.UpSQL, &m.DownSQL); scanErr != nil {
			return nil, fmt.Errorf("rollback: scan row: %w", scanErr)
		}
		migrations = append(migrations, m)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rollback: iterate rows: %w", err)
	}
	return migrations, nil
}

// loadApplied queries tab_migration_log and returns a set of "app:version" keys.
func (r *MigrationRunner) loadApplied(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	rows, err := pool.Query(ctx, "SELECT app, version FROM tab_migration_log")
	if err != nil {
		return nil, fmt.Errorf("query tab_migration_log: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var app, version string
		if err := rows.Scan(&app, &version); err != nil {
			return nil, fmt.Errorf("scan migration log: %w", err)
		}
		applied[app+":"+version] = true
	}
	return applied, rows.Err()
}

// applySkip filters out the migration matching skipKey and returns the remaining
// migrations and the skipped ones separately.
func applySkip(migrations []AppMigration, skipKey string) (remaining, skipped []AppMigration) {
	for _, m := range migrations {
		if m.migrationKey() == skipKey {
			skipped = append(skipped, m)
		} else {
			remaining = append(remaining, m)
		}
	}
	return remaining, skipped
}

// topoSortMigrations sorts migrations in dependency order using Kahn's algorithm.
// Dependencies that are already applied (in the applied set) are treated as
// satisfied. Returns an error if a cycle is detected or a dependency is missing.
func topoSortMigrations(migrations []AppMigration, applied map[string]bool) ([]AppMigration, error) {
	if len(migrations) == 0 {
		return nil, nil
	}

	// Build lookup of pending migrations by key.
	byKey := make(map[string]*AppMigration, len(migrations))
	for i := range migrations {
		byKey[migrations[i].migrationKey()] = &migrations[i]
	}

	// Build in-degree and adjacency list.
	inDegree := make(map[string]int, len(migrations))
	// dependents maps a key to the keys that depend on it.
	dependents := make(map[string][]string, len(migrations))

	for _, m := range migrations {
		key := m.migrationKey()
		if _, ok := inDegree[key]; !ok {
			inDegree[key] = 0
		}

		for _, dep := range m.DependsOn {
			if err := validateDependsOnKey(dep); err != nil {
				return nil, err
			}
			// If already applied, this dependency is satisfied — skip it.
			if applied[dep] {
				continue
			}
			// If not in the pending set, it's a missing dependency.
			if _, ok := byKey[dep]; !ok {
				return nil, fmt.Errorf("migration %q depends on %q which is neither pending nor applied", key, dep)
			}
			dependents[dep] = append(dependents[dep], key)
			inDegree[key]++
		}
	}

	// Seed queue with zero-in-degree nodes (sorted for determinism).
	var queue []string
	for key, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, key)
		}
	}
	sort.Strings(queue)

	var sorted []AppMigration
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		sorted = append(sorted, *byKey[key])

		deps := dependents[key]
		sort.Strings(deps) // deterministic
		for _, dep := range deps {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(sorted) < len(migrations) {
		// Cycle detected — collect cycle members.
		var cycle []string
		for key, deg := range inDegree {
			if deg > 0 {
				cycle = append(cycle, key)
			}
		}
		sort.Strings(cycle)
		return nil, fmt.Errorf("circular migration dependency detected: %s", strings.Join(cycle, " -> "))
	}

	return sorted, nil
}

// validateDependsOnKey checks that a DependsOn value is in "app:version" format.
func validateDependsOnKey(key string) error {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid DependsOn format %q: expected \"appName:version\"", key)
	}
	return nil
}

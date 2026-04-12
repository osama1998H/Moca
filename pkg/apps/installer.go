package apps

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/orm"
)

// InstalledApp holds information about an app installed on a site.
type InstalledApp struct {
	InstalledAt time.Time `json:"installed_at"`
	AppName     string    `json:"app_name"`
	AppVersion  string    `json:"app_version"`
}

// UninstallOptions controls Uninstall behavior.
type UninstallOptions struct {
	DropTables bool // if true, DROP TABLE for all app's doctypes
	Force      bool // skip reverse-dependency check
}

// AppInstaller handles app installation and removal on tenant sites.
// It orchestrates dependency validation, MetaType registration, migration
// execution, fixture loading, and system table bookkeeping.
type AppInstaller struct {
	db       *orm.DBManager
	migrator *meta.Migrator
	registry *meta.Registry
	runner   *orm.MigrationRunner
	redis    *redis.Client
	logger   *slog.Logger
}

// NewAppInstaller creates an AppInstaller.
// redis may be nil; Redis-dependent operations degrade gracefully.
func NewAppInstaller(
	db *orm.DBManager,
	migrator *meta.Migrator,
	registry *meta.Registry,
	runner *orm.MigrationRunner,
	redisCache *redis.Client,
	logger *slog.Logger,
) *AppInstaller {
	return &AppInstaller{
		db:       db,
		migrator: migrator,
		registry: registry,
		runner:   runner,
		redis:    redisCache,
		logger:   logger,
	}
}

// Install installs an app on a site by executing the full installation workflow:
//  1. Load and validate the app
//  2. Check the app is not already installed
//  3. Validate dependencies are satisfied on this site
//  4. Load and compile app MetaTypes from module directories
//  5. Register MetaTypes (creates tables, upserts tab_doctype, warms cache)
//  6. Run app SQL migrations
//  7. Load fixture data
//  8. Stub: register app hooks
//  9. Register in moca_system.site_apps
//  10. Clear caches
func (inst *AppInstaller) Install(ctx context.Context, site, appName, appsDir string) error {
	// Step 1: Load the app.
	apps, err := ScanApps(appsDir)
	if err != nil {
		return fmt.Errorf("install app: scan apps: %w", err)
	}
	app := findApp(apps, appName)
	if app == nil {
		return fmt.Errorf("install app %q: %w", appName, ErrAppNotFound)
	}

	// Step 2: Check not already installed.
	installed, err := inst.isInstalled(ctx, site, appName)
	if err != nil {
		return fmt.Errorf("install app: check installed: %w", err)
	}
	if installed {
		return fmt.Errorf("install app %q on site %q: %w", appName, site, ErrAppAlreadyInstalled)
	}

	// Step 3: Validate dependencies are installed on this site.
	if depErr := inst.validateSiteDeps(ctx, site, app.Manifest); depErr != nil {
		return fmt.Errorf("install app %q: %w", appName, depErr)
	}

	// Step 4: Load MetaTypes from module directories.
	inst.logger.InfoContext(ctx, "loading app MetaTypes", slog.String("app", appName), slog.String("site", site))
	metaTypes, err := loadAppMetaTypes(app)
	if err != nil {
		return fmt.Errorf("install app %q: %w", appName, err)
	}

	// Step 5: Register MetaTypes (creates tables + tab_doctype + cache).
	inst.logger.InfoContext(ctx, "registering MetaTypes", slog.String("app", appName), slog.Int("count", len(metaTypes)))
	ordered := reorderChildrenFirst(metaTypes)
	for _, mt := range ordered {
		jsonBytes, merr := json.Marshal(mt)
		if merr != nil {
			return fmt.Errorf("install app %q: marshal %q: %w", appName, mt.Name, merr)
		}
		if _, rerr := inst.registry.Register(ctx, site, jsonBytes); rerr != nil {
			return fmt.Errorf("install app %q: register %q: %w", appName, mt.Name, rerr)
		}
	}

	// Step 5b: Seed tab_doc_type and tab_doc_perm records so app doctypes
	// appear in the resource API (desk sidebar) and pass permission checks.
	inst.logger.InfoContext(ctx, "seeding DocType and DocPerm records", slog.String("app", appName))
	pool, poolErr := inst.db.ForSite(ctx, site)
	if poolErr != nil {
		return fmt.Errorf("install app %q: get site pool: %w", appName, poolErr)
	}
	if seedErr := inst.seedDocTypeAndPermRecords(ctx, pool, metaTypes); seedErr != nil {
		return fmt.Errorf("install app %q: seed doc records: %w", appName, seedErr)
	}

	// Step 6: Run app SQL migrations.
	if len(app.Manifest.Migrations) > 0 {
		inst.logger.InfoContext(ctx, "running app migrations", slog.String("app", appName))
		migrations := convertMigrations(appName, app.Manifest.Migrations)
		if _, applyErr := inst.runner.Apply(ctx, site, migrations, orm.MigrateOptions{}); applyErr != nil {
			return fmt.Errorf("install app %q: run migrations: %w", appName, applyErr)
		}
	}

	// Step 7: Load fixtures.
	if len(app.Manifest.Fixtures) > 0 {
		inst.logger.InfoContext(ctx, "loading fixtures", slog.String("app", appName))
		if err := inst.loadFixtures(ctx, site, app); err != nil {
			return fmt.Errorf("install app %q: load fixtures: %w", appName, err)
		}
	}

	// Step 8: Stub — hook registration.
	inst.logger.WarnContext(ctx, "dynamic hook registration not yet implemented", slog.String("app", appName))

	// Step 9: Register in moca_system.
	inst.logger.InfoContext(ctx, "registering app in system tables", slog.String("app", appName), slog.String("site", site))
	if err := inst.registerAppInSystem(ctx, site, app); err != nil {
		return fmt.Errorf("install app %q: register in system: %w", appName, err)
	}

	// Step 10: Clear caches.
	if err := inst.registry.InvalidateAll(ctx, site); err != nil {
		inst.logger.WarnContext(ctx, "cache invalidation failed", slog.Any("error", err))
	}

	inst.logger.InfoContext(ctx, "app installed successfully", slog.String("app", appName), slog.String("site", site))
	return nil
}

// Uninstall removes an app from a site. It checks for reverse dependencies,
// optionally drops the app's tables, and removes the site_apps entry.
func (inst *AppInstaller) Uninstall(ctx context.Context, site, appName string, opts UninstallOptions) error {
	// Check the app is installed.
	installed, err := inst.isInstalled(ctx, site, appName)
	if err != nil {
		return fmt.Errorf("uninstall app: check installed: %w", err)
	}
	if !installed {
		return fmt.Errorf("uninstall app %q from site %q: %w", appName, site, ErrAppNotInstalled)
	}

	// Check reverse dependencies unless Force.
	if !opts.Force {
		dependents, checkErr := inst.findReverseDeps(ctx, site, appName)
		if checkErr != nil {
			return fmt.Errorf("uninstall app %q: check reverse deps: %w", appName, checkErr)
		}
		if len(dependents) > 0 {
			return &DependencyError{
				Message: fmt.Sprintf("cannot uninstall %q: apps %v depend on it", appName, dependents),
			}
		}
	}

	// Optionally drop tables.
	if opts.DropTables {
		if err := inst.dropAppTables(ctx, site, appName); err != nil {
			return fmt.Errorf("uninstall app %q: drop tables: %w", appName, err)
		}
	}

	// Remove from site_apps.
	if _, err := inst.db.SystemPool().Exec(ctx,
		"DELETE FROM site_apps WHERE site_name = $1 AND app_name = $2",
		site, appName,
	); err != nil {
		return fmt.Errorf("uninstall app %q: delete site_apps: %w", appName, err)
	}

	// Clear caches.
	if invalidErr := inst.registry.InvalidateAll(ctx, site); invalidErr != nil {
		inst.logger.WarnContext(ctx, "cache invalidation failed", slog.Any("error", invalidErr))
	}

	inst.logger.InfoContext(ctx, "app uninstalled", slog.String("app", appName), slog.String("site", site))
	return nil
}

// ListInstalled returns the apps installed on a site.
func (inst *AppInstaller) ListInstalled(ctx context.Context, site string) ([]InstalledApp, error) {
	rows, err := inst.db.SystemPool().Query(ctx, `
		SELECT app_name, app_version, installed_at
		FROM site_apps
		WHERE site_name = $1
		ORDER BY installed_at`, site)
	if err != nil {
		return nil, fmt.Errorf("list installed apps: %w", err)
	}
	defer rows.Close()

	var result []InstalledApp
	for rows.Next() {
		var ia InstalledApp
		if err := rows.Scan(&ia.AppName, &ia.AppVersion, &ia.InstalledAt); err != nil {
			return nil, fmt.Errorf("list installed apps: scan: %w", err)
		}
		result = append(result, ia)
	}
	return result, rows.Err()
}

// seedDocTypeAndPermRecords inserts document records into tab_doc_type and
// permission records into tab_doc_perm for each installed MetaType. This mirrors
// SiteManager.seedDocTypeRecords (pkg/tenancy/manager.go) so that app doctypes
// are visible in the resource API (desk sidebar) and pass permission checks.
func (inst *AppInstaller) seedDocTypeAndPermRecords(ctx context.Context, pool *pgxpool.Pool, metaTypes []*meta.MetaType) error {
	return orm.WithTransaction(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		permIdx := 0
		for _, mt := range metaTypes {
			tableName := meta.TableName(mt.Name)
			// Skip if the document table doesn't exist yet.
			var tableExists bool
			if err := tx.QueryRow(ctx,
				"SELECT EXISTS(SELECT 1 FROM pg_tables WHERE tablename = $1)",
				tableName,
			).Scan(&tableExists); err != nil {
				return fmt.Errorf("check table %s: %w", tableName, err)
			}
			if !tableExists {
				continue
			}

			// Insert the DocType document record.
			if _, err := tx.Exec(ctx, `
				INSERT INTO tab_doc_type (
					name, dt_module, dt_label, dt_description,
					is_submittable, is_single, is_child_table, is_virtual,
					dt_track_changes, dt_naming_rule,
					docstatus, idx, owner, creation, modified, modified_by, _extra
				) VALUES (
					$1, $2, $3, $4,
					$5, $6, $7, $8,
					$9, $10,
					0, 0, 'System', NOW(), NOW(), 'System', '{}'
				) ON CONFLICT (name) DO NOTHING`,
				mt.Name, mt.Module, mt.Label, mt.Description,
				mt.IsSubmittable, mt.IsSingle, mt.IsChildTable, mt.IsVirtual,
				mt.TrackChanges, string(mt.NamingRule.Rule),
			); err != nil {
				return fmt.Errorf("seed DocType record %q: %w", mt.Name, err)
			}

			// Seed DocPerm child records for this MetaType's permission rules.
			for _, perm := range mt.Permissions {
				permName := fmt.Sprintf("docperm-%s-%s-%d", mt.Name, perm.Role, permIdx)
				if _, err := tx.Exec(ctx, `
					INSERT INTO tab_doc_perm (
						name, parent, parenttype, parentfield, idx,
						role, doctype_perm, match_field, match_value,
						owner, creation, modified, modified_by, _extra
					) VALUES (
						$1, $2, 'DocType', 'permissions', $3,
						$4, $5, $6, $7,
						'System', NOW(), NOW(), 'System', '{}'
					) ON CONFLICT (name) DO NOTHING`,
					permName, mt.Name, permIdx,
					perm.Role, perm.DocTypePerm, perm.MatchField, perm.MatchValue,
				); err != nil {
					return fmt.Errorf("seed DocPerm for %q role %q: %w", mt.Name, perm.Role, err)
				}
				permIdx++
			}
		}
		return nil
	})
}

// ── private helpers ─────────────────────────────────────────────────────────

func findApp(apps []AppInfo, name string) *AppInfo {
	for i := range apps {
		if apps[i].Name == name {
			return &apps[i]
		}
	}
	return nil
}

func (inst *AppInstaller) isInstalled(ctx context.Context, site, appName string) (bool, error) {
	var exists bool
	err := inst.db.SystemPool().QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM site_apps WHERE site_name = $1 AND app_name = $2)",
		site, appName,
	).Scan(&exists)
	return exists, err
}

func (inst *AppInstaller) validateSiteDeps(ctx context.Context, site string, manifest *AppManifest) error {
	if len(manifest.Dependencies) == 0 {
		return nil
	}

	// Load installed apps with versions.
	rows, err := inst.db.SystemPool().Query(ctx,
		"SELECT app_name, app_version FROM site_apps WHERE site_name = $1", site)
	if err != nil {
		return fmt.Errorf("query installed apps: %w", err)
	}
	defer rows.Close()

	installed := make(map[string]string) // app_name -> version
	for rows.Next() {
		var name, version string
		if err := rows.Scan(&name, &version); err != nil {
			return fmt.Errorf("scan installed apps: %w", err)
		}
		installed[name] = version
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate installed apps: %w", err)
	}

	for _, dep := range manifest.Dependencies {
		installedVersion, ok := installed[dep.App]
		if !ok {
			return &DependencyError{
				Message: fmt.Sprintf("dependency %q is not installed on site %q", dep.App, site),
			}
		}

		if dep.MinVersion != "" {
			constraint, cerr := semver.NewConstraint(dep.MinVersion)
			if cerr != nil {
				return &DependencyError{
					Message: fmt.Sprintf("invalid version constraint %q for dependency %q: %v", dep.MinVersion, dep.App, cerr),
				}
			}
			ver, verr := semver.NewVersion(installedVersion)
			if verr != nil {
				return &DependencyError{
					Message: fmt.Sprintf("installed app %q has invalid version %q: %v", dep.App, installedVersion, verr),
				}
			}
			if !constraint.Check(ver) {
				return &DependencyError{
					Message: fmt.Sprintf("dependency %q requires %s, but version %s is installed",
						dep.App, dep.MinVersion, installedVersion),
				}
			}
		}
	}

	return nil
}

// loadAppMetaTypes reads and compiles MetaType definitions from the app's
// module directories. For each module, it looks for JSON files in
// {appDir}/modules/{moduleName}/doctypes/{doctype_name}/{doctype_name}.json.
func loadAppMetaTypes(app *AppInfo) ([]*meta.MetaType, error) {
	var result []*meta.MetaType

	for _, mod := range app.Manifest.Modules {
		modDir := filepath.Join(app.Path, "modules", toSnakeCase(mod.Name), "doctypes")

		for _, dtName := range mod.DocTypes {
			snake := toSnakeCase(dtName)
			jsonPath := filepath.Join(modDir, snake, snake+".json")

			data, err := os.ReadFile(jsonPath)
			if err != nil {
				return nil, fmt.Errorf("read doctype %q from module %q: %w", dtName, mod.Name, err)
			}

			mt, compileErr := meta.Compile(data)
			if compileErr != nil {
				return nil, fmt.Errorf("compile doctype %q from module %q: %w", dtName, mod.Name, compileErr)
			}

			result = append(result, mt)
		}
	}

	return result, nil
}

// reorderChildrenFirst returns a copy with IsChildTable MetaTypes before others.
func reorderChildrenFirst(mts []*meta.MetaType) []*meta.MetaType {
	var children, parents []*meta.MetaType
	for _, mt := range mts {
		if mt.IsChildTable {
			children = append(children, mt)
		} else {
			parents = append(parents, mt)
		}
	}
	return append(children, parents...)
}

func convertMigrations(appName string, migrations []Migration) []orm.AppMigration {
	result := make([]orm.AppMigration, len(migrations))
	for i, m := range migrations {
		result[i] = orm.AppMigration{
			AppName:   appName,
			Version:   m.Version,
			UpSQL:     m.Up,
			DownSQL:   m.Down,
			DependsOn: m.DependsOn,
		}
	}
	return result
}

// loadFixtures reads fixture JSON files and inserts them via direct SQL.
// Each fixture file contains an array of documents with a "doctype" field.
func (inst *AppInstaller) loadFixtures(ctx context.Context, site string, app *AppInfo) error {
	pool, err := inst.db.ForSite(ctx, site)
	if err != nil {
		return fmt.Errorf("get site pool: %w", err)
	}

	for _, fix := range app.Manifest.Fixtures {
		fixPath := filepath.Join(app.Path, fix.Path)
		data, readErr := os.ReadFile(fixPath)
		if readErr != nil {
			return fmt.Errorf("read fixture %q: %w", fix.Path, readErr)
		}

		var docs []map[string]any
		if err := json.Unmarshal(data, &docs); err != nil {
			return fmt.Errorf("parse fixture %q: %w", fix.Path, err)
		}

		for _, doc := range docs {
			doctype, ok := doc["doctype"].(string)
			if !ok || doctype == "" {
				return fmt.Errorf("fixture document in %q missing 'doctype' field", fix.Path)
			}

			if err := insertFixtureDoc(ctx, pool, doctype, doc); err != nil {
				return fmt.Errorf("insert fixture doc (doctype=%q) from %q: %w", doctype, fix.Path, err)
			}
		}

		inst.logger.DebugContext(ctx, "loaded fixture",
			slog.String("path", fix.Path), slog.Int("docs", len(docs)))
	}

	return nil
}

// insertFixtureDoc inserts a single fixture document via dynamic SQL.
// It converts the document map to column-value pairs for INSERT.
func insertFixtureDoc(ctx context.Context, pool *pgxpool.Pool, doctype string, doc map[string]any) error {
	tableName := "tab_" + toSnakeCase(doctype)

	// Remove the "doctype" meta field — it's not a column.
	delete(doc, "doctype")

	if len(doc) == 0 {
		return nil
	}

	cols := make([]string, 0, len(doc))
	placeholders := make([]string, 0, len(doc))
	vals := make([]any, 0, len(doc))
	i := 1

	for k, v := range doc {
		cols = append(cols, pgx.Identifier{k}.Sanitize())
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		vals = append(vals, v)
		i++
	}

	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT DO NOTHING",
		pgx.Identifier{tableName}.Sanitize(),
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
	)

	_, err := pool.Exec(ctx, sql, vals...)
	return err
}

func (inst *AppInstaller) registerAppInSystem(ctx context.Context, site string, app *AppInfo) error {
	manifestJSON, err := json.Marshal(app.Manifest)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	depsJSON, err := json.Marshal(app.Manifest.Dependencies)
	if err != nil {
		depsJSON = []byte("[]")
	}

	return orm.WithTransaction(ctx, inst.db.SystemPool(), func(ctx context.Context, tx pgx.Tx) error {
		// Upsert app in apps table.
		if _, execErr := tx.Exec(ctx, `
			INSERT INTO apps (name, version, title, description, publisher, dependencies, manifest)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (name) DO UPDATE SET
				version = EXCLUDED.version,
				manifest = EXCLUDED.manifest`,
			app.Name, app.Manifest.Version, app.Manifest.Title,
			app.Manifest.Description, app.Manifest.Publisher,
			depsJSON, manifestJSON,
		); execErr != nil {
			return fmt.Errorf("upsert app: %w", execErr)
		}

		// Insert into site_apps.
		if _, execErr := tx.Exec(ctx, `
			INSERT INTO site_apps (site_name, app_name, app_version)
			VALUES ($1, $2, $3)`,
			site, app.Name, app.Manifest.Version,
		); execErr != nil {
			return fmt.Errorf("insert site_apps: %w", execErr)
		}

		return nil
	})
}

func (inst *AppInstaller) findReverseDeps(ctx context.Context, site, appName string) ([]string, error) {
	// Load all installed apps' manifests to check if any depends on the target.
	rows, err := inst.db.SystemPool().Query(ctx, `
		SELECT a.name, a.manifest
		FROM site_apps sa
		JOIN apps a ON sa.app_name = a.name
		WHERE sa.site_name = $1 AND sa.app_name != $2`, site, appName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dependents []string
	for rows.Next() {
		var name string
		var manifestJSON []byte
		if err := rows.Scan(&name, &manifestJSON); err != nil {
			return nil, err
		}

		var manifest AppManifest
		if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
			continue // skip unparseable manifests
		}

		for _, dep := range manifest.Dependencies {
			if dep.App == appName {
				dependents = append(dependents, name)
				break
			}
		}
	}

	return dependents, rows.Err()
}

func (inst *AppInstaller) dropAppTables(ctx context.Context, site, appName string) error {
	// Load the app's manifest to find its doctypes.
	var manifestJSON []byte
	err := inst.db.SystemPool().QueryRow(ctx,
		"SELECT manifest FROM apps WHERE name = $1", appName,
	).Scan(&manifestJSON)
	if err != nil {
		return fmt.Errorf("load manifest for %q: %w", appName, err)
	}

	var manifest AppManifest
	if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
		return fmt.Errorf("parse manifest for %q: %w", appName, err)
	}

	pool, poolErr := inst.db.ForSite(ctx, site)
	if poolErr != nil {
		return fmt.Errorf("get site pool: %w", poolErr)
	}

	return orm.WithTransaction(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		for _, mod := range manifest.Modules {
			for _, dtName := range mod.DocTypes {
				tableName := "tab_" + toSnakeCase(dtName)
				quotedTable := pgx.Identifier{tableName}.Sanitize()
				if _, execErr := tx.Exec(ctx,
					fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", quotedTable),
				); execErr != nil {
					return fmt.Errorf("drop table %s: %w", tableName, execErr)
				}
			}
		}
		return nil
	})
}

// toSnakeCase converts PascalCase or camelCase to snake_case.
// Spaces and dashes are converted to underscores.
func toSnakeCase(s string) string {
	runes := []rune(s)
	var result strings.Builder
	for i, r := range runes {
		switch {
		case r >= 'A' && r <= 'Z':
			if i > 0 && runes[i-1] != ' ' && runes[i-1] != '-' && runes[i-1] != '_' {
				result.WriteByte('_')
			}
			result.WriteRune(r + ('a' - 'A'))
		case r == ' ' || r == '-':
			result.WriteByte('_')
		default:
			result.WriteRune(r)
		}
	}
	return result.String()
}

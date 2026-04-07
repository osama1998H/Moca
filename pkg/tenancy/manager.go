package tenancy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/orm"
)

// BootstrapFunc returns the compiled MetaType definitions for bootstrap.
// The canonical implementation is core.BootstrapCoreMeta.
// Injected to avoid import cycles (apps/core → pkg/document → pkg/tenancy).
type BootstrapFunc func() ([]*meta.MetaType, error)

// Sentinel errors for site operations.
var (
	ErrSiteExists   = errors.New("site already exists")
	ErrSiteNotFound = errors.New("site not found")
)

// SiteCreateConfig holds all parameters for the 9-step site creation lifecycle.
type SiteCreateConfig struct {
	Config        map[string]any // optional; timezone, language, currency
	Name          string         // required; site identifier (e.g. "acme.localhost")
	AdminEmail    string         // required; admin user email
	AdminPassword string         // required; plaintext, bcrypt-hashed internally
	Plan          string         // optional; e.g. "free", "business"
}

// SiteDropOptions controls DropSite behavior.
type SiteDropOptions struct {
	Force bool // CLI layer uses this; service layer always drops
}

// SiteInfo holds information about a registered site.
type SiteInfo struct {
	Name        string         `json:"name"`
	DBSchema    string         `json:"db_schema"`
	Status      string         `json:"status"`
	Plan        string         `json:"plan,omitempty"`
	AdminEmail  string         `json:"admin_email"`
	CreatedAt   time.Time      `json:"created_at"`
	ModifiedAt  time.Time      `json:"modified_at"`
	Config      map[string]any `json:"config,omitempty"`
	Apps        []string       `json:"apps,omitempty"`
	DBSizeBytes int64          `json:"db_size_bytes"`
}

// CloneOptions controls CloneSite behavior.
type CloneOptions struct {
	Exclude   []string // DocType table names to exclude from clone
	Anonymize bool     // Anonymize PII data in the clone
}

// SiteManager orchestrates site lifecycle operations: creation, deletion,
// listing, and active-site selection. It composes lower-level primitives
// (DBManager, Migrator, Registry) into the 9-step creation workflow.
type SiteManager struct {
	db          *orm.DBManager
	migrator    *meta.Migrator
	registry    *meta.Registry
	redis       *redis.Client // Cache DB (0), may be nil
	redisPubSub *redis.Client // PubSub DB (3), may be nil
	logger      *slog.Logger
	bootstrapFn BootstrapFunc
}

// NewSiteManager creates a SiteManager.
// redisCache and redisPubSub may be nil; Redis-dependent steps degrade gracefully.
// bootstrapFn should be core.BootstrapCoreMeta (injected to avoid import cycles).
func NewSiteManager(
	db *orm.DBManager,
	migrator *meta.Migrator,
	registry *meta.Registry,
	redisCache *redis.Client,
	redisPubSub *redis.Client,
	logger *slog.Logger,
	bootstrapFn BootstrapFunc,
) *SiteManager {
	return &SiteManager{
		db:          db,
		migrator:    migrator,
		registry:    registry,
		redis:       redisCache,
		redisPubSub: redisPubSub,
		logger:      logger,
		bootstrapFn: bootstrapFn,
	}
}

// CreateSite executes the 10-step site creation lifecycle:
//  1. Create PostgreSQL schema
//  2. Create per-tenant system tables
//  3. Bootstrap core MetaType tables
//  4. Create Administrator user
//  5. Create Redis config namespace
//  6. Stub: Meilisearch index
//  7. Stub: S3 storage prefix
//  8. Register site in moca_system
//  9. Warm metadata cache
//  10. Seed DocType document records for core doctypes
func (m *SiteManager) CreateSite(ctx context.Context, cfg SiteCreateConfig) (retErr error) {
	if err := validateSiteConfig(cfg); err != nil {
		return err
	}

	siteName := cfg.Name
	schemaName := SchemaNameForSite(siteName)

	// Check for duplicate before starting.
	exists, err := m.siteExists(ctx, siteName)
	if err != nil {
		return fmt.Errorf("create site: check existence: %w", err)
	}
	if exists {
		return fmt.Errorf("create site %q: %w", siteName, ErrSiteExists)
	}

	// Cleanup on failure: best-effort rollback of partial creation.
	defer func() {
		if retErr != nil {
			m.cleanupPartialSite(context.Background(), siteName, schemaName)
		}
	}()

	// Step 1: Create PostgreSQL schema.
	m.logger.InfoContext(ctx, "step 1/10: creating schema", slog.String("schema", schemaName))
	if schemaErr := m.createSchema(ctx, schemaName); schemaErr != nil {
		return fmt.Errorf("create site step 1 (schema): %w", schemaErr)
	}

	// Step 2: Create per-tenant system tables.
	m.logger.InfoContext(ctx, "step 2/10: creating system tables", slog.String("site", siteName))
	if metaErr := m.migrator.EnsureMetaTables(ctx, siteName); metaErr != nil {
		return fmt.Errorf("create site step 2 (meta tables): %w", metaErr)
	}

	// Step 3: Bootstrap core MetaType document tables.
	m.logger.InfoContext(ctx, "step 3/10: bootstrapping core tables", slog.String("site", siteName))
	coreMetaTypes, err := m.bootstrapFn()
	if err != nil {
		return fmt.Errorf("create site step 3 (bootstrap): %w", err)
	}
	ordered := reorderChildrenFirst(coreMetaTypes)
	for _, mt := range ordered {
		stmts := m.migrator.Diff(nil, mt)
		if applyErr := m.migrator.Apply(ctx, siteName, stmts); applyErr != nil {
			return fmt.Errorf("create site step 3 (create table %q): %w", mt.Name, applyErr)
		}
	}

	// Step 4: Create Administrator user.
	m.logger.InfoContext(ctx, "step 4/10: creating admin user", slog.String("site", siteName))
	if adminErr := m.createAdminUser(ctx, siteName, cfg.AdminEmail, cfg.AdminPassword); adminErr != nil {
		return fmt.Errorf("create site step 4 (admin user): %w", adminErr)
	}

	// Step 5: Create Redis config namespace.
	m.logger.InfoContext(ctx, "step 5/10: setting up Redis config", slog.String("site", siteName))
	if redisErr := m.setupRedisConfig(ctx, siteName, cfg.Config); redisErr != nil {
		return fmt.Errorf("create site step 5 (redis config): %w", redisErr)
	}

	// Step 6: Stub — Meilisearch index.
	m.logger.WarnContext(ctx, "step 6/10: meilisearch index creation not yet implemented", slog.String("site", siteName))

	// Step 7: Storage uses shared bucket with site-scoped key prefix ({site}/private/..., {site}/public/...).
	// No per-site bucket creation needed — prefix is created implicitly on first upload.
	m.logger.InfoContext(ctx, "step 7/10: storage uses shared bucket with site-scoped key prefix", slog.String("site", siteName))

	// Step 8: Register site in moca_system.
	m.logger.InfoContext(ctx, "step 8/10: registering site", slog.String("site", siteName))
	if regErr := m.registerSiteInSystem(ctx, cfg, schemaName); regErr != nil {
		return fmt.Errorf("create site step 8 (register): %w", regErr)
	}

	// Step 9: Warm metadata cache via Registry.Register.
	m.logger.InfoContext(ctx, "step 9/10: warming metadata cache", slog.String("site", siteName))
	for _, mt := range ordered {
		jsonBytes, merr := json.Marshal(mt)
		if merr != nil {
			return fmt.Errorf("create site step 9 (marshal %q): %w", mt.Name, merr)
		}
		if _, rerr := m.registry.Register(ctx, siteName, jsonBytes); rerr != nil {
			return fmt.Errorf("create site step 9 (register %q): %w", mt.Name, rerr)
		}
	}

	// Step 10: Seed DocType document records into tab_doc_type so the resource
	// API can list them (sidebar, list views, etc.). Also seed DocPerm child
	// records for each MetaType's permission rules.
	m.logger.InfoContext(ctx, "step 10/10: seeding DocType document records", slog.String("site", siteName))
	tenantPool, poolErr := m.db.ForSite(ctx, siteName)
	if poolErr != nil {
		return fmt.Errorf("create site step 10 (tenant pool): %w", poolErr)
	}
	if seedErr := m.seedDocTypeRecords(ctx, tenantPool, ordered); seedErr != nil {
		return fmt.Errorf("create site step 10 (seed): %w", seedErr)
	}

	m.logger.InfoContext(ctx, "site created successfully", slog.String("site", siteName))
	return nil
}

// DropSite removes a site: drops the PostgreSQL schema, deletes Redis keys,
// and removes the site from moca_system.
func (m *SiteManager) DropSite(ctx context.Context, name string, _ SiteDropOptions) error {
	schemaName := SchemaNameForSite(name)

	// Drop the PostgreSQL schema.
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err := m.db.SystemPool().Exec(ctx,
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", quotedSchema),
	); err != nil {
		return fmt.Errorf("drop site %q: drop schema: %w", name, err)
	}

	// Delete Redis keys for this site.
	m.deleteRedisKeys(ctx, name)

	// Remove from moca_system (site_apps first due to FK, then sites).
	err := orm.WithTransaction(ctx, m.db.SystemPool(), func(ctx context.Context, tx pgx.Tx) error {
		if _, execErr := tx.Exec(ctx,
			"DELETE FROM site_apps WHERE site_name = $1", name,
		); execErr != nil {
			return fmt.Errorf("delete site_apps: %w", execErr)
		}

		tag, execErr := tx.Exec(ctx,
			"DELETE FROM sites WHERE name = $1", name,
		)
		if execErr != nil {
			return fmt.Errorf("delete sites: %w", execErr)
		}
		if tag.RowsAffected() == 0 {
			return ErrSiteNotFound
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("drop site %q: %w", name, err)
	}

	m.logger.InfoContext(ctx, "site dropped", slog.String("site", name))
	return nil
}

// ListSites returns all registered sites with their installed apps.
func (m *SiteManager) ListSites(ctx context.Context) ([]SiteInfo, error) {
	rows, err := m.db.SystemPool().Query(ctx, `
		SELECT s.name, s.db_schema, s.status, COALESCE(s.plan, ''),
		       s.admin_email, s.config, s.created_at, s.modified_at,
		       COALESCE(array_agg(sa.app_name) FILTER (WHERE sa.app_name IS NOT NULL), '{}')
		FROM sites s
		LEFT JOIN site_apps sa ON s.name = sa.site_name
		GROUP BY s.name
		ORDER BY s.created_at`)
	if err != nil {
		return nil, fmt.Errorf("list sites: %w", err)
	}
	defer rows.Close()

	var sites []SiteInfo
	for rows.Next() {
		var si SiteInfo
		var configJSON []byte
		var apps []string

		if err := rows.Scan(
			&si.Name, &si.DBSchema, &si.Status, &si.Plan,
			&si.AdminEmail, &configJSON, &si.CreatedAt, &si.ModifiedAt,
			&apps,
		); err != nil {
			return nil, fmt.Errorf("list sites: scan: %w", err)
		}

		if len(configJSON) > 0 {
			if err := json.Unmarshal(configJSON, &si.Config); err != nil {
				return nil, fmt.Errorf("list sites: unmarshal config: %w", err)
			}
		}
		si.Apps = apps
		sites = append(sites, si)
	}
	return sites, rows.Err()
}

// GetSiteInfo returns detailed information about a specific site.
func (m *SiteManager) GetSiteInfo(ctx context.Context, name string) (*SiteInfo, error) {
	si := &SiteInfo{}
	var configJSON []byte
	var apps []string

	err := m.db.SystemPool().QueryRow(ctx, `
		SELECT s.name, s.db_schema, s.status, COALESCE(s.plan, ''),
		       s.admin_email, s.config, s.created_at, s.modified_at,
		       COALESCE(array_agg(sa.app_name) FILTER (WHERE sa.app_name IS NOT NULL), '{}')
		FROM sites s
		LEFT JOIN site_apps sa ON s.name = sa.site_name
		WHERE s.name = $1
		GROUP BY s.name`, name,
	).Scan(
		&si.Name, &si.DBSchema, &si.Status, &si.Plan,
		&si.AdminEmail, &configJSON, &si.CreatedAt, &si.ModifiedAt,
		&apps,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("get site info %q: %w", name, ErrSiteNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get site info %q: %w", name, err)
	}

	if len(configJSON) > 0 {
		if err := json.Unmarshal(configJSON, &si.Config); err != nil {
			return nil, fmt.Errorf("get site info %q: unmarshal config: %w", name, err)
		}
	}
	si.Apps = apps

	// Fetch schema size.
	var sizeBytes int64
	_ = m.db.SystemPool().QueryRow(ctx, `
		SELECT COALESCE(SUM(pg_total_relation_size(
			quote_ident(schemaname) || '.' || quote_ident(tablename)
		)), 0)
		FROM pg_tables WHERE schemaname = $1`, si.DBSchema,
	).Scan(&sizeBytes)
	si.DBSizeBytes = sizeBytes

	return si, nil
}

// SetActiveSite writes the given site name to .moca/current_site under projectRoot.
func (m *SiteManager) SetActiveSite(projectRoot, siteName string) error {
	dotMoca := filepath.Join(projectRoot, ".moca")
	if err := os.MkdirAll(dotMoca, 0o755); err != nil {
		return fmt.Errorf("set active site: create .moca dir: %w", err)
	}

	path := filepath.Join(dotMoca, "current_site")
	if err := os.WriteFile(path, []byte(siteName), 0o644); err != nil {
		return fmt.Errorf("set active site: write file: %w", err)
	}
	return nil
}

// EnableSite re-enables a disabled site by setting status to "active",
// clearing maintenance metadata from config, and publishing a config.changed event.
func (m *SiteManager) EnableSite(ctx context.Context, name string) error {
	exists, err := m.siteExists(ctx, name)
	if err != nil {
		return fmt.Errorf("enable site: check existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("enable site %q: %w", name, ErrSiteNotFound)
	}

	tag, err := m.db.SystemPool().Exec(ctx, `
		UPDATE sites
		SET status = 'active',
		    config = config - 'maintenance_message' - 'maintenance_allow_ips',
		    modified_at = NOW()
		WHERE name = $1`, name,
	)
	if err != nil {
		return fmt.Errorf("enable site %q: update status: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("enable site %q: %w", name, ErrSiteNotFound)
	}

	// Clear maintenance Redis key and publish config event.
	if m.redis != nil {
		_ = m.redis.Del(ctx, fmt.Sprintf("maintenance:%s", name)).Err()
	}
	m.publishConfigEvent(ctx, name)

	m.logger.InfoContext(ctx, "site enabled", slog.String("site", name))
	return nil
}

// DisableSite puts a site into maintenance mode by setting status to "disabled",
// storing the maintenance message and allowed IPs in config, and publishing
// a config.changed event.
func (m *SiteManager) DisableSite(ctx context.Context, name, message string, allowIPs []string) error {
	exists, err := m.siteExists(ctx, name)
	if err != nil {
		return fmt.Errorf("disable site: check existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("disable site %q: %w", name, ErrSiteNotFound)
	}

	// Build maintenance metadata to merge into config.
	maintenance := map[string]any{}
	if message != "" {
		maintenance["maintenance_message"] = message
	}
	if len(allowIPs) > 0 {
		maintenance["maintenance_allow_ips"] = allowIPs
	}
	maintenanceJSON, err := json.Marshal(maintenance)
	if err != nil {
		return fmt.Errorf("disable site: marshal maintenance: %w", err)
	}

	tag, execErr := m.db.SystemPool().Exec(ctx, `
		UPDATE sites
		SET status = 'disabled',
		    config = config || $1::jsonb,
		    modified_at = NOW()
		WHERE name = $2`, maintenanceJSON, name,
	)
	if execErr != nil {
		return fmt.Errorf("disable site %q: update status: %w", name, execErr)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("disable site %q: %w", name, ErrSiteNotFound)
	}

	// Set maintenance Redis key and publish config event.
	if m.redis != nil {
		_ = m.redis.Set(ctx, fmt.Sprintf("maintenance:%s", name), maintenanceJSON, 0).Err()
	}
	m.publishConfigEvent(ctx, name)

	m.logger.InfoContext(ctx, "site disabled",
		slog.String("site", name),
		slog.String("message", message),
	)
	return nil
}

// RenameSite renames a site by altering the PostgreSQL schema, updating system
// tables (in a single transaction), and performing best-effort Redis key migration
// and filesystem directory rename.
func (m *SiteManager) RenameSite(ctx context.Context, oldName, newName, projectRoot string) error {
	if newName == "" {
		return fmt.Errorf("rename site: new name is required")
	}

	// Check target doesn't already exist.
	exists, err := m.siteExists(ctx, newName)
	if err != nil {
		return fmt.Errorf("rename site: check target: %w", err)
	}
	if exists {
		return fmt.Errorf("rename site %q to %q: target %w", oldName, newName, ErrSiteExists)
	}

	// Check source exists.
	exists, err = m.siteExists(ctx, oldName)
	if err != nil {
		return fmt.Errorf("rename site: check source: %w", err)
	}
	if !exists {
		return fmt.Errorf("rename site %q: %w", oldName, ErrSiteNotFound)
	}

	oldSchema := SchemaNameForSite(oldName)
	newSchema := SchemaNameForSite(newName)

	// Transactional: rename schema + update system tables.
	err = orm.WithTransaction(ctx, m.db.SystemPool(), func(ctx context.Context, tx pgx.Tx) error {
		quotedOld := pgx.Identifier{oldSchema}.Sanitize()
		quotedNew := pgx.Identifier{newSchema}.Sanitize()

		if _, execErr := tx.Exec(ctx,
			fmt.Sprintf("ALTER SCHEMA %s RENAME TO %s", quotedOld, quotedNew),
		); execErr != nil {
			return fmt.Errorf("alter schema: %w", execErr)
		}

		if _, execErr := tx.Exec(ctx,
			"UPDATE sites SET name = $1, db_schema = $2, modified_at = NOW() WHERE name = $3",
			newName, newSchema, oldName,
		); execErr != nil {
			return fmt.Errorf("update sites: %w", execErr)
		}

		if _, execErr := tx.Exec(ctx,
			"UPDATE site_apps SET site_name = $1 WHERE site_name = $2",
			newName, oldName,
		); execErr != nil {
			return fmt.Errorf("update site_apps: %w", execErr)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("rename site %q to %q: %w", oldName, newName, err)
	}

	// Best-effort: migrate Redis keys.
	m.renameRedisKeys(ctx, oldName, newName)

	// Best-effort: rename filesystem directory.
	if projectRoot != "" {
		oldDir := filepath.Join(projectRoot, "sites", oldName)
		newDir := filepath.Join(projectRoot, "sites", newName)
		if _, statErr := os.Stat(oldDir); statErr == nil {
			if renameErr := os.Rename(oldDir, newDir); renameErr != nil {
				m.logger.WarnContext(ctx, "rename site: filesystem rename failed",
					slog.String("old", oldDir),
					slog.String("new", newDir),
					slog.Any("error", renameErr),
				)
			}
		}
	}

	// Best-effort: update .moca/current_site if it points to the old name.
	if projectRoot != "" {
		currentSitePath := filepath.Join(projectRoot, ".moca", "current_site")
		if data, readErr := os.ReadFile(currentSitePath); readErr == nil {
			if strings.TrimSpace(string(data)) == oldName {
				_ = os.WriteFile(currentSitePath, []byte(newName), 0o644)
			}
		}
	}

	m.logger.InfoContext(ctx, "site renamed",
		slog.String("old", oldName),
		slog.String("new", newName),
	)
	return nil
}

// CloneSite creates a copy of a site's schema and data, optionally anonymizing PII.
// Uses SQL-based schema cloning (no external pg_dump binary required).
func (m *SiteManager) CloneSite(ctx context.Context, source, target string, opts CloneOptions) error {
	// Verify source exists.
	sourceInfo, err := m.GetSiteInfo(ctx, source)
	if err != nil {
		return fmt.Errorf("clone site: %w", err)
	}

	// Verify target doesn't exist.
	exists, err := m.siteExists(ctx, target)
	if err != nil {
		return fmt.Errorf("clone site: check target: %w", err)
	}
	if exists {
		return fmt.Errorf("clone site: target %q: %w", target, ErrSiteExists)
	}

	sourceSchema := SchemaNameForSite(source)
	targetSchema := SchemaNameForSite(target)

	// Step 1: Create target schema.
	m.logger.InfoContext(ctx, "clone: creating target schema", slog.String("target", targetSchema))
	if schemaErr := m.createSchema(ctx, targetSchema); schemaErr != nil {
		return fmt.Errorf("clone site: create schema: %w", schemaErr)
	}

	// Cleanup on failure.
	cleanup := true
	defer func() {
		if cleanup {
			m.cleanupPartialSite(context.Background(), target, targetSchema)
		}
	}()

	// Step 2: Get all tables in source schema.
	tables, err := m.listSchemaTables(ctx, sourceSchema)
	if err != nil {
		return fmt.Errorf("clone site: list tables: %w", err)
	}

	// Build exclude set.
	excludeSet := make(map[string]bool, len(opts.Exclude))
	for _, e := range opts.Exclude {
		excludeSet["tab_"+sanitizeForSchema(strings.ToLower(e))] = true
	}

	// Step 3: Clone each table (structure + data).
	pool := m.db.SystemPool()
	for _, tableName := range tables {
		if excludeSet[tableName] {
			m.logger.InfoContext(ctx, "clone: skipping excluded table", slog.String("table", tableName))
			continue
		}

		quotedSource := pgx.Identifier{sourceSchema, tableName}.Sanitize()
		quotedTarget := pgx.Identifier{targetSchema, tableName}.Sanitize()

		// Create table with same structure.
		if _, execErr := pool.Exec(ctx,
			fmt.Sprintf("CREATE TABLE %s (LIKE %s INCLUDING DEFAULTS INCLUDING GENERATED INCLUDING IDENTITY INCLUDING INDEXES)",
				quotedTarget, quotedSource),
		); execErr != nil {
			return fmt.Errorf("clone site: create table %q: %w", tableName, execErr)
		}

		// Copy data. OVERRIDING SYSTEM VALUE allows copying GENERATED ALWAYS
		// AS IDENTITY columns (e.g. tab_audit_log.id) with their original values.
		if _, execErr := pool.Exec(ctx,
			fmt.Sprintf("INSERT INTO %s OVERRIDING SYSTEM VALUE SELECT * FROM %s", quotedTarget, quotedSource),
		); execErr != nil {
			return fmt.Errorf("clone site: copy data %q: %w", tableName, execErr)
		}
	}

	// Step 4: Register target in moca_system.
	m.logger.InfoContext(ctx, "clone: registering target site", slog.String("target", target))
	if regErr := m.registerClonedSite(ctx, sourceInfo, target, targetSchema); regErr != nil {
		return fmt.Errorf("clone site: register: %w", regErr)
	}

	// Step 5: Setup Redis config for target.
	if sourceInfo.Config != nil {
		if redisErr := m.setupRedisConfig(ctx, target, sourceInfo.Config); redisErr != nil {
			m.logger.WarnContext(ctx, "clone: redis config setup failed", slog.Any("error", redisErr))
		}
	}

	// Step 6: Anonymize if requested.
	if opts.Anonymize {
		m.logger.InfoContext(ctx, "clone: anonymizing target data", slog.String("target", target))
		if anonErr := m.anonymizeSite(ctx, target, sourceInfo.AdminEmail); anonErr != nil {
			return fmt.Errorf("clone site: anonymize: %w", anonErr)
		}
	}

	cleanup = false
	m.logger.InfoContext(ctx, "site cloned",
		slog.String("source", source),
		slog.String("target", target),
		slog.Bool("anonymized", opts.Anonymize),
	)
	return nil
}

// ReinstallSite drops and recreates a site, preserving its configuration.
// Returns the list of previously installed apps so the caller can reinstall them.
// Backup creation (if desired) should be done by the caller before calling this.
func (m *SiteManager) ReinstallSite(ctx context.Context, name string, adminPassword string) (previousApps []string, retErr error) {
	// Get site info before dropping.
	siteInfo, err := m.GetSiteInfo(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("reinstall site: %w", err)
	}

	// Get installed apps.
	rows, err := m.db.SystemPool().Query(ctx,
		"SELECT app_name FROM site_apps WHERE site_name = $1 AND app_name != 'core' ORDER BY installed_at",
		name,
	)
	if err != nil {
		return nil, fmt.Errorf("reinstall site: list apps: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var appName string
		if scanErr := rows.Scan(&appName); scanErr != nil {
			return nil, fmt.Errorf("reinstall site: scan app: %w", scanErr)
		}
		previousApps = append(previousApps, appName)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reinstall site: rows: %w", err)
	}

	// Drop site.
	if dropErr := m.DropSite(ctx, name, SiteDropOptions{Force: true}); dropErr != nil {
		return nil, fmt.Errorf("reinstall site: drop: %w", dropErr)
	}

	// Recreate with preserved config.
	if createErr := m.CreateSite(ctx, SiteCreateConfig{
		Name:          name,
		AdminEmail:    siteInfo.AdminEmail,
		AdminPassword: adminPassword,
		Plan:          siteInfo.Plan,
		Config:        siteInfo.Config,
	}); createErr != nil {
		return previousApps, fmt.Errorf("reinstall site: recreate: %w", createErr)
	}

	m.logger.InfoContext(ctx, "site reinstalled",
		slog.String("site", name),
		slog.Int("previous_apps", len(previousApps)),
	)
	return previousApps, nil
}

// ── private helpers ─────────────────────────────────────────────────────────

func validateSiteConfig(cfg SiteCreateConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("site name is required")
	}
	if cfg.AdminEmail == "" {
		return fmt.Errorf("admin email is required")
	}
	if cfg.AdminPassword == "" {
		return fmt.Errorf("admin password is required")
	}
	return nil
}

// sanitizeForSchema converts a site name into a safe PostgreSQL schema suffix.
// Lowercase, replace dots/dashes/spaces with underscores, strip non-alphanumeric.
func sanitizeForSchema(name string) string {
	s := strings.ToLower(name)
	s = strings.NewReplacer(".", "_", "-", "_", " ", "_").Replace(s)
	s = regexp.MustCompile(`[^a-z0-9_]`).ReplaceAllString(s, "")
	// Schema names must start with a letter.
	if len(s) > 0 && s[0] >= '0' && s[0] <= '9' {
		s = "s" + s
	}
	if s == "" {
		s = "site"
	}
	return s
}

// SchemaNameForSite returns the full PostgreSQL schema name for a site.
// The schema name is "tenant_" followed by a sanitized version of the site name.
func SchemaNameForSite(siteName string) string {
	return "tenant_" + sanitizeForSchema(siteName)
}

func (m *SiteManager) siteExists(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := m.db.SystemPool().QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM sites WHERE name = $1)", name,
	).Scan(&exists)
	return exists, err
}

func (m *SiteManager) createSchema(ctx context.Context, schemaName string) error {
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	_, err := m.db.SystemPool().Exec(ctx,
		fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", quotedSchema),
	)
	return err
}

func (m *SiteManager) createAdminUser(ctx context.Context, siteName, email, password string) error {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	pool, err := m.db.ForSite(ctx, siteName)
	if err != nil {
		return fmt.Errorf("get site pool: %w", err)
	}

	return orm.WithTransaction(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		// Insert admin user (name = email, per User naming rule "field:email").
		if _, execErr := tx.Exec(ctx, `
			INSERT INTO tab_user
				(name, email, full_name, password, enabled, user_type, owner, modified_by, docstatus, idx)
			VALUES ($1, $1, 'Administrator', $2, TRUE, 'System', 'System', 'System', 0, 0)`,
			email, string(hashed),
		); execErr != nil {
			return fmt.Errorf("insert admin user: %w", execErr)
		}

		// Insert "System Manager" role.
		if _, execErr := tx.Exec(ctx, `
			INSERT INTO tab_role (name, role_name, disabled, owner, modified_by, docstatus, idx)
			VALUES ('System Manager', 'System Manager', FALSE, 'System', 'System', 0, 0)
			ON CONFLICT (name) DO NOTHING`,
		); execErr != nil {
			return fmt.Errorf("insert system manager role: %w", execErr)
		}

		// Link admin user to "System Manager" role via HasRole child table.
		childName, genErr := randomID("hasrole")
		if genErr != nil {
			return genErr
		}
		if _, execErr := tx.Exec(ctx, `
			INSERT INTO tab_has_role
				(name, parent, parenttype, parentfield, role, owner, modified_by, idx)
			VALUES ($1, $2, 'User', 'roles', 'System Manager', 'System', 'System', 0)`,
			childName, email,
		); execErr != nil {
			return fmt.Errorf("insert has_role: %w", execErr)
		}

		return nil
	})
}

func (m *SiteManager) setupRedisConfig(ctx context.Context, siteName string, userConfig map[string]any) error {
	if m.redis == nil {
		m.logger.WarnContext(ctx, "redis unavailable, skipping config namespace", slog.String("site", siteName))
		return nil
	}

	cfg := map[string]any{
		"timezone": "UTC",
		"language": "en",
		"currency": "USD",
	}
	for k, v := range userConfig {
		cfg[k] = v
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	key := fmt.Sprintf("config:%s", siteName)
	return m.redis.Set(ctx, key, data, 0).Err()
}

func (m *SiteManager) registerSiteInSystem(ctx context.Context, cfg SiteCreateConfig, schemaName string) error {
	cfgMap := cfg.Config
	if cfgMap == nil {
		cfgMap = map[string]any{}
	}
	configJSON, err := json.Marshal(cfgMap)
	if err != nil {
		configJSON = []byte("{}")
	}

	return orm.WithTransaction(ctx, m.db.SystemPool(), func(ctx context.Context, tx pgx.Tx) error {
		// Register site.
		if _, execErr := tx.Exec(ctx, `
			INSERT INTO sites (name, db_schema, status, plan, config, admin_email)
			VALUES ($1, $2, 'active', $3, $4, $5)`,
			cfg.Name, schemaName, cfg.Plan, configJSON, cfg.AdminEmail,
		); execErr != nil {
			return fmt.Errorf("insert site: %w", execErr)
		}

		// Register core app in the apps table.
		coreManifest := map[string]any{
			"name": "core", "version": "0.1.0", "title": "Moca Core",
		}
		manifestJSON, _ := json.Marshal(coreManifest)
		if _, execErr := tx.Exec(ctx, `
			INSERT INTO apps (name, version, title, description, publisher, dependencies, manifest)
			VALUES ('core', '0.1.0', 'Moca Core', 'Core framework doctypes and system configuration',
			        'Moca Framework', '[]', $1)
			ON CONFLICT (name) DO NOTHING`, manifestJSON,
		); execErr != nil {
			return fmt.Errorf("insert core app: %w", execErr)
		}

		// Link core app to site.
		if _, execErr := tx.Exec(ctx, `
			INSERT INTO site_apps (site_name, app_name, app_version)
			VALUES ($1, 'core', '0.1.0')`,
			cfg.Name,
		); execErr != nil {
			return fmt.Errorf("insert site_apps: %w", execErr)
		}

		return nil
	})
}

// seedDocTypeRecords inserts document records into tab_doc_type for each core
// MetaType so they are queryable via the resource API (used by the desk sidebar).
// Also seeds tab_doc_perm child records for each MetaType's permission rules.
func (m *SiteManager) seedDocTypeRecords(ctx context.Context, pool *pgxpool.Pool, metatypes []*meta.MetaType) error {
	return orm.WithTransaction(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		permIdx := 0
		for _, mt := range metatypes {
			tableName := meta.TableName(mt.Name)
			// Skip if the document table doesn't exist (child tables like DocField
			// have their own tables but are not listed standalone).
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

func (m *SiteManager) cleanupPartialSite(ctx context.Context, siteName, schemaName string) {
	m.logger.WarnContext(ctx, "cleaning up partial site creation", slog.String("site", siteName))

	// Drop schema.
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err := m.db.SystemPool().Exec(ctx,
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", quotedSchema),
	); err != nil {
		m.logger.ErrorContext(ctx, "cleanup: drop schema failed", slog.Any("error", err))
	}

	// Remove system entries.
	if _, err := m.db.SystemPool().Exec(ctx,
		"DELETE FROM site_apps WHERE site_name = $1", siteName,
	); err != nil {
		m.logger.ErrorContext(ctx, "cleanup: delete site_apps failed", slog.Any("error", err))
	}
	if _, err := m.db.SystemPool().Exec(ctx,
		"DELETE FROM sites WHERE name = $1", siteName,
	); err != nil {
		m.logger.ErrorContext(ctx, "cleanup: delete sites failed", slog.Any("error", err))
	}

	// Delete Redis config key.
	if m.redis != nil {
		key := fmt.Sprintf("config:%s", siteName)
		if err := m.redis.Del(ctx, key).Err(); err != nil {
			m.logger.ErrorContext(ctx, "cleanup: redis del failed", slog.Any("error", err))
		}
	}
}

func (m *SiteManager) deleteRedisKeys(ctx context.Context, siteName string) {
	if m.redis == nil {
		return
	}
	// Delete the config key and any meta/schema keys for this site.
	patterns := []string{
		fmt.Sprintf("config:%s", siteName),
		fmt.Sprintf("meta:%s:*", siteName),
		fmt.Sprintf("schema:%s:*", siteName),
	}
	for _, pattern := range patterns {
		if strings.Contains(pattern, "*") {
			keys, err := m.redis.Keys(ctx, pattern).Result()
			if err != nil {
				m.logger.WarnContext(ctx, "drop site: redis keys scan failed", slog.Any("error", err))
				continue
			}
			if len(keys) > 0 {
				_ = m.redis.Del(ctx, keys...).Err()
			}
		} else {
			_ = m.redis.Del(ctx, pattern).Err()
		}
	}
}

// reorderChildrenFirst returns a copy with IsChildTable MetaTypes before others.
// This ensures child tables exist before parent tables that reference them.
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

// randomID generates a short random identifier with a prefix.
func randomID(prefix string) (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random id: %w", err)
	}
	return prefix + "-" + hex.EncodeToString(b), nil
}

// publishConfigEvent sends a config.changed event on the PubSub channel for a site.
func (m *SiteManager) publishConfigEvent(ctx context.Context, siteName string) {
	if m.redisPubSub == nil {
		return
	}
	channel := fmt.Sprintf("pubsub:config:%s", siteName)
	payload := fmt.Sprintf(`{"event":"config.changed","site":"%s"}`, siteName)
	if err := m.redisPubSub.Publish(ctx, channel, payload).Err(); err != nil {
		m.logger.WarnContext(ctx, "failed to publish config.changed event",
			slog.String("site", siteName),
			slog.String("channel", channel),
			slog.Any("error", err),
		)
	}
}

// renameRedisKeys migrates Redis keys from oldName to newName (best-effort).
func (m *SiteManager) renameRedisKeys(ctx context.Context, oldName, newName string) {
	if m.redis == nil {
		return
	}

	// Copy config key.
	oldKey := fmt.Sprintf("config:%s", oldName)
	newKey := fmt.Sprintf("config:%s", newName)
	if val, err := m.redis.Get(ctx, oldKey).Result(); err == nil {
		_ = m.redis.Set(ctx, newKey, val, 0).Err()
	}

	// Delete all old keys.
	m.deleteRedisKeys(ctx, oldName)
}

// listSchemaTables returns all table names in the given PostgreSQL schema.
func (m *SiteManager) listSchemaTables(ctx context.Context, schemaName string) ([]string, error) {
	rows, err := m.db.SystemPool().Query(ctx, `
		SELECT tablename FROM pg_tables
		WHERE schemaname = $1
		ORDER BY tablename`, schemaName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr != nil {
			return nil, scanErr
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

// registerClonedSite inserts system records for a cloned site, copying app
// associations from the source.
func (m *SiteManager) registerClonedSite(ctx context.Context, source *SiteInfo, targetName, targetSchema string) error {
	configJSON, err := json.Marshal(source.Config)
	if err != nil {
		configJSON = []byte("{}")
	}

	return orm.WithTransaction(ctx, m.db.SystemPool(), func(ctx context.Context, tx pgx.Tx) error {
		if _, execErr := tx.Exec(ctx, `
			INSERT INTO sites (name, db_schema, status, plan, config, admin_email)
			VALUES ($1, $2, 'active', $3, $4, $5)`,
			targetName, targetSchema, source.Plan, configJSON, source.AdminEmail,
		); execErr != nil {
			return fmt.Errorf("insert cloned site: %w", execErr)
		}

		// Copy app associations from source.
		for _, appName := range source.Apps {
			if _, execErr := tx.Exec(ctx, `
				INSERT INTO site_apps (site_name, app_name, app_version)
				SELECT $1, app_name, app_version FROM site_apps
				WHERE site_name = $2 AND app_name = $3`,
				targetName, source.Name, appName,
			); execErr != nil {
				return fmt.Errorf("copy site_app %q: %w", appName, execErr)
			}
		}

		return nil
	})
}

// anonymizeSite runs anonymization queries on core DocType tables.
func (m *SiteManager) anonymizeSite(ctx context.Context, siteName, adminEmail string) error {
	pool, err := m.db.ForSite(ctx, siteName)
	if err != nil {
		return fmt.Errorf("get site pool: %w", err)
	}

	schema := SchemaNameForSite(siteName)

	// Anonymize tab_user: randomize email, hash password, clear full_name.
	// Preserve the admin user.
	if m.tableExistsInSchema(ctx, schema, "tab_user") {
		dummyHash, _ := bcrypt.GenerateFromPassword([]byte("anonymized"), bcrypt.DefaultCost)
		if _, execErr := pool.Exec(ctx, `
			UPDATE tab_user
			SET email = 'user_' || substr(md5(name), 1, 8) || '@example.com',
			    full_name = 'User ' || substr(md5(name), 1, 6),
			    password = $1
			WHERE name != $2`,
			string(dummyHash), adminEmail,
		); execErr != nil {
			m.logger.WarnContext(ctx, "anonymize: tab_user update failed", slog.Any("error", execErr))
		}
	}

	// Anonymize tab_contact if it exists.
	if m.tableExistsInSchema(ctx, schema, "tab_contact") {
		if _, execErr := pool.Exec(ctx, `
			UPDATE tab_contact
			SET email_id = 'contact_' || substr(md5(name), 1, 8) || '@example.com',
			    phone = '555-0000'`); execErr != nil {
			m.logger.WarnContext(ctx, "anonymize: tab_contact update failed", slog.Any("error", execErr))
		}
	}

	// Anonymize tab_address if it exists.
	if m.tableExistsInSchema(ctx, schema, "tab_address") {
		if _, execErr := pool.Exec(ctx, `
			UPDATE tab_address
			SET address_line1 = '123 Anonymized St'`); execErr != nil {
			m.logger.WarnContext(ctx, "anonymize: tab_address update failed", slog.Any("error", execErr))
		}
	}

	return nil
}

// tableExistsInSchema checks if a table exists in the given PostgreSQL schema.
func (m *SiteManager) tableExistsInSchema(ctx context.Context, schemaName, tableName string) bool {
	var exists bool
	_ = m.db.SystemPool().QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2)",
		schemaName, tableName,
	).Scan(&exists)
	return exists
}

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
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"github.com/moca-framework/moca/pkg/meta"
	"github.com/moca-framework/moca/pkg/orm"
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

// SiteManager orchestrates site lifecycle operations: creation, deletion,
// listing, and active-site selection. It composes lower-level primitives
// (DBManager, Migrator, Registry) into the 9-step creation workflow.
type SiteManager struct {
	db          *orm.DBManager
	migrator    *meta.Migrator
	registry    *meta.Registry
	redis       *redis.Client
	logger      *slog.Logger
	bootstrapFn BootstrapFunc
}

// NewSiteManager creates a SiteManager.
// redis may be nil; Redis-dependent steps degrade gracefully.
// bootstrapFn should be core.BootstrapCoreMeta (injected to avoid import cycles).
func NewSiteManager(
	db *orm.DBManager,
	migrator *meta.Migrator,
	registry *meta.Registry,
	redisCache *redis.Client,
	logger *slog.Logger,
	bootstrapFn BootstrapFunc,
) *SiteManager {
	return &SiteManager{
		db:          db,
		migrator:    migrator,
		registry:    registry,
		redis:       redisCache,
		logger:      logger,
		bootstrapFn: bootstrapFn,
	}
}

// CreateSite executes the 9-step site creation lifecycle:
//  1. Create PostgreSQL schema
//  2. Create per-tenant system tables
//  3. Bootstrap core MetaType tables
//  4. Create Administrator user
//  5. Create Redis config namespace
//  6. Stub: Meilisearch index
//  7. Stub: S3 storage prefix
//  8. Register site in moca_system
//  9. Warm metadata cache
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
	m.logger.InfoContext(ctx, "step 1/9: creating schema", slog.String("schema", schemaName))
	if schemaErr := m.createSchema(ctx, schemaName); schemaErr != nil {
		return fmt.Errorf("create site step 1 (schema): %w", schemaErr)
	}

	// Step 2: Create per-tenant system tables.
	m.logger.InfoContext(ctx, "step 2/9: creating system tables", slog.String("site", siteName))
	if metaErr := m.migrator.EnsureMetaTables(ctx, siteName); metaErr != nil {
		return fmt.Errorf("create site step 2 (meta tables): %w", metaErr)
	}

	// Step 3: Bootstrap core MetaType document tables.
	m.logger.InfoContext(ctx, "step 3/9: bootstrapping core tables", slog.String("site", siteName))
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
	m.logger.InfoContext(ctx, "step 4/9: creating admin user", slog.String("site", siteName))
	if adminErr := m.createAdminUser(ctx, siteName, cfg.AdminEmail, cfg.AdminPassword); adminErr != nil {
		return fmt.Errorf("create site step 4 (admin user): %w", adminErr)
	}

	// Step 5: Create Redis config namespace.
	m.logger.InfoContext(ctx, "step 5/9: setting up Redis config", slog.String("site", siteName))
	if redisErr := m.setupRedisConfig(ctx, siteName, cfg.Config); redisErr != nil {
		return fmt.Errorf("create site step 5 (redis config): %w", redisErr)
	}

	// Step 6: Stub — Meilisearch index.
	m.logger.WarnContext(ctx, "step 6/9: meilisearch index creation not yet implemented", slog.String("site", siteName))

	// Step 7: Stub — S3 storage prefix.
	m.logger.WarnContext(ctx, "step 7/9: S3 storage prefix creation not yet implemented", slog.String("site", siteName))

	// Step 8: Register site in moca_system.
	m.logger.InfoContext(ctx, "step 8/9: registering site", slog.String("site", siteName))
	if regErr := m.registerSiteInSystem(ctx, cfg, schemaName); regErr != nil {
		return fmt.Errorf("create site step 8 (register): %w", regErr)
	}

	// Step 9: Warm metadata cache via Registry.Register.
	m.logger.InfoContext(ctx, "step 9/9: warming metadata cache", slog.String("site", siteName))
	for _, mt := range ordered {
		jsonBytes, merr := json.Marshal(mt)
		if merr != nil {
			return fmt.Errorf("create site step 9 (marshal %q): %w", mt.Name, merr)
		}
		if _, rerr := m.registry.Register(ctx, siteName, jsonBytes); rerr != nil {
			return fmt.Errorf("create site step 9 (register %q): %w", mt.Name, rerr)
		}
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
			_ = json.Unmarshal(configJSON, &si.Config)
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
		_ = json.Unmarshal(configJSON, &si.Config)
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
	configJSON, err := json.Marshal(cfg.Config)
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

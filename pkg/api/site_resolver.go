package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/moca-framework/moca/pkg/orm"
	"github.com/moca-framework/moca/pkg/tenancy"
)

// siteMetaCacheTTL is the TTL for cached site metadata in Redis.
const siteMetaCacheTTL = 5 * time.Minute

// siteMetadata holds the JSON-serializable subset of SiteContext for Redis caching.
// Pool is excluded because it cannot be serialized.
type siteMetadata struct {
	DBSchema      string         `json:"db_schema"`
	Status        string         `json:"status"`
	Config        map[string]any `json:"config,omitempty"`
	InstalledApps []string       `json:"installed_apps,omitempty"`
}

// siteMetaCacheKey returns the Redis cache key for site metadata.
func siteMetaCacheKey(siteID string) string {
	return "site_meta:" + siteID
}

// siteInfoQuerier abstracts the database query for site metadata,
// enabling unit testing without a real DBManager.
type siteInfoQuerier interface {
	QuerySiteInfo(ctx context.Context, siteID string) (*siteMetadata, error)
}

// sitePoolProvider abstracts the pool acquisition for a site.
type sitePoolProvider interface {
	ForSite(ctx context.Context, siteName string) (*pgxpool.Pool, error)
}

// dbSiteInfoQuerier implements siteInfoQuerier using the DBManager's system pool.
type dbSiteInfoQuerier struct {
	db *orm.DBManager
}

func (q *dbSiteInfoQuerier) QuerySiteInfo(ctx context.Context, siteID string) (*siteMetadata, error) {
	var meta siteMetadata
	var configJSON []byte
	var apps []string

	err := q.db.SystemPool().QueryRow(ctx, `
		SELECT s.db_schema, s.status,
		       s.config,
		       COALESCE(array_agg(sa.app_name) FILTER (WHERE sa.app_name IS NOT NULL), '{}')
		FROM sites s
		LEFT JOIN site_apps sa ON s.name = sa.site_name
		WHERE s.name = $1
		GROUP BY s.name`, siteID,
	).Scan(&meta.DBSchema, &meta.Status, &configJSON, &apps)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, tenancy.ErrSiteNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query site info %q: %w", siteID, err)
	}

	if len(configJSON) > 0 {
		_ = json.Unmarshal(configJSON, &meta.Config)
	}
	meta.InstalledApps = apps
	return &meta, nil
}

// DBSiteResolver implements SiteResolver by looking up tenant metadata
// from the database (with Redis caching) and obtaining connection pools
// through orm.DBManager. It is the default resolver used by moca-server.
type DBSiteResolver struct {
	querier siteInfoQuerier
	pools   sitePoolProvider
	redis   *redis.Client
	logger  *slog.Logger
}

// dbManagerPoolProvider wraps DBManager to implement sitePoolProvider.
type dbManagerPoolProvider struct {
	db *orm.DBManager
}

func (p *dbManagerPoolProvider) ForSite(ctx context.Context, siteName string) (*pgxpool.Pool, error) {
	return p.db.ForSite(ctx, siteName)
}

// NewDBSiteResolver creates a SiteResolver backed by the given DBManager,
// with optional Redis caching. If redisClient is nil, all lookups go to DB.
func NewDBSiteResolver(db *orm.DBManager, redisClient *redis.Client, logger *slog.Logger) *DBSiteResolver {
	return &DBSiteResolver{
		querier: &dbSiteInfoQuerier{db: db},
		pools:   &dbManagerPoolProvider{db: db},
		redis:   redisClient,
		logger:  logger,
	}
}

// ResolveSite resolves a site identifier to a fully-populated SiteContext.
// It checks Redis cache first, falls back to DB on miss or Redis failure,
// and returns ErrSiteDisabled for disabled sites.
func (r *DBSiteResolver) ResolveSite(ctx context.Context, siteID string) (*tenancy.SiteContext, error) {
	meta, err := r.cachedSiteInfo(ctx, siteID)
	if err != nil {
		return nil, err
	}

	// Check site status before acquiring a pool.
	if meta.Status == "disabled" {
		return nil, fmt.Errorf("resolve site %q: %w", siteID, tenancy.ErrSiteDisabled)
	}

	pool, err := r.pools.ForSite(ctx, siteID)
	if err != nil {
		return nil, fmt.Errorf("resolve site %q: %w", siteID, err)
	}

	return &tenancy.SiteContext{
		Pool:          pool,
		Name:          siteID,
		DBSchema:      tenancy.SchemaNameForSite(siteID),
		Status:        meta.Status,
		Config:        meta.Config,
		InstalledApps: meta.InstalledApps,
		RedisPrefix:   siteID + ":",
		StorageBucket: siteID + "/",
	}, nil
}

// cachedSiteInfo returns site metadata, checking Redis cache first.
// On cache miss or Redis failure, it queries the database and caches the result.
func (r *DBSiteResolver) cachedSiteInfo(ctx context.Context, siteID string) (*siteMetadata, error) {
	// Try Redis cache.
	if r.redis != nil {
		meta, err := r.fromCache(ctx, siteID)
		if err == nil {
			return meta, nil
		}
		// Log Redis errors but continue to DB (fail-open).
		if !errors.Is(err, redis.Nil) {
			r.logger.Warn("redis cache read failed, falling back to DB",
				slog.String("site_id", siteID),
				slog.String("error", err.Error()),
			)
		}
	}

	// Query DB.
	meta, err := r.querier.QuerySiteInfo(ctx, siteID)
	if err != nil {
		return nil, err
	}

	// Write to cache (best-effort).
	if r.redis != nil {
		r.writeCache(ctx, siteID, meta)
	}

	return meta, nil
}

// fromCache reads site metadata from Redis.
func (r *DBSiteResolver) fromCache(ctx context.Context, siteID string) (*siteMetadata, error) {
	data, err := r.redis.Get(ctx, siteMetaCacheKey(siteID)).Bytes()
	if err != nil {
		return nil, err
	}
	var meta siteMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal cached site meta: %w", err)
	}
	return &meta, nil
}

// writeCache stores site metadata in Redis with TTL.
func (r *DBSiteResolver) writeCache(ctx context.Context, siteID string, meta *siteMetadata) {
	data, err := json.Marshal(meta)
	if err != nil {
		r.logger.Warn("failed to marshal site meta for cache",
			slog.String("site_id", siteID),
			slog.String("error", err.Error()),
		)
		return
	}
	if err := r.redis.Set(ctx, siteMetaCacheKey(siteID), data, siteMetaCacheTTL).Err(); err != nil {
		r.logger.Warn("failed to write site meta cache",
			slog.String("site_id", siteID),
			slog.String("error", err.Error()),
		)
	}
}

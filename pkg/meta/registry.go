package meta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/pkg/orm"
)

// ErrMetaTypeNotFound is returned by Get when the requested MetaType is absent
// from all three cache tiers (L1 in-memory, L2 Redis, L3 PostgreSQL).
// Use errors.Is to check for this sentinel.
var ErrMetaTypeNotFound = errors.New("metatype not found")

// Key format strings for Redis keys used by the Registry.
// These mirror the constants in internal/drivers/redis.go.
const (
	metaKeyFmt          = "meta:%s:%s"        // meta:{site}:{doctype}
	schemaVersionKeyFmt = "schema:%s:version" // schema:{site}:version
)

// Registry is the central access point for MetaType lookups in MOCA.
// It uses a three-tier caching strategy to minimise latency:
//
//   - L1: in-process sync.Map — sub-microsecond, process-local
//   - L2: Redis — shared across processes, key: meta:{site}:{doctype}
//   - L3: PostgreSQL tab_doctype — source of truth, durable
//
// On a cache miss at tier N, Get queries tier N+1 and promotes the result
// back into all higher tiers (write-through on miss). Invalidation is
// explicit via Invalidate or InvalidateAll; L3 is never invalidated.
//
// Both redisCache and db may be nil; the Registry degrades gracefully:
// nil Redis skips L2, nil db skips L3 and returns ErrMetaTypeNotFound.
type Registry struct {
	redis    *redis.Client  // L2 cache (db 0); nil means L2 unavailable
	migrator *Migrator      // DDL diff and apply for schema evolution
	db       *orm.DBManager // L3 reads; nil means DB unavailable
	logger   *slog.Logger
	l1       sync.Map // "site:doctype" -> *MetaType; placed last for optimal GC scan
}

// NewRegistry creates a Registry backed by the given DBManager and Redis cache client.
// redisCache should be the Cache client (db 0) from RedisClients.
// Both db and redisCache may be nil; Registry handles their absence gracefully.
func NewRegistry(db *orm.DBManager, redisCache *redis.Client, logger *slog.Logger) *Registry {
	return &Registry{
		redis:    redisCache,
		migrator: NewMigrator(db, logger),
		db:       db,
		logger:   logger,
	}
}

// Get returns the MetaType for the given doctype in the given site.
//
// Lookup order: L1 (sync.Map) → L2 (Redis) → L3 (PostgreSQL tab_doctype).
// On a lower-tier hit, the result is promoted into all higher tiers.
//
// Returns ErrMetaTypeNotFound (unwrappable via errors.Is) if the MetaType
// is absent from all tiers. Returns a wrapped error for infrastructure failures.
func (r *Registry) Get(ctx context.Context, site, doctype string) (*MetaType, error) {
	key := l1Key(site, doctype)

	// L1: in-process cache (hot path — no network I/O).
	if v, ok := r.l1.Load(key); ok {
		if mt, ok := v.(*MetaType); ok {
			return mt, nil
		}
		// Corrupted entry (unexpected type) — evict and fall through to lower tiers.
		r.l1.Delete(key)
	}

	// L2: Redis (shared cross-process cache).
	if r.redis != nil {
		data, err := r.redis.Get(ctx, metaRedisKey(site, doctype)).Bytes()
		switch {
		case errors.Is(err, redis.Nil):
			// Cache miss — fall through to L3.
		case err != nil:
			r.logger.WarnContext(ctx, "registry: L2 redis get failed, falling back to L3",
				slog.String("site", site), slog.String("doctype", doctype),
				slog.Any("error", err))
		default:
			mt, merr := unmarshalMetaType(data)
			if merr != nil {
				r.logger.WarnContext(ctx, "registry: L2 unmarshal failed, falling back to L3",
					slog.String("site", site), slog.String("doctype", doctype),
					slog.Any("error", merr))
			} else {
				r.l1.Store(key, mt)
				return mt, nil
			}
		}
	}

	// L3: PostgreSQL — source of truth.
	if r.db == nil {
		return nil, fmt.Errorf("registry get %s/%s: %w", site, doctype, ErrMetaTypeNotFound)
	}

	pool, err := r.db.ForSite(ctx, site)
	if err != nil {
		return nil, fmt.Errorf("registry get %s/%s: get pool: %w", site, doctype, err)
	}

	var defJSON []byte
	err = pool.QueryRow(ctx,
		`SELECT definition FROM tab_doctype WHERE name = $1`, doctype,
	).Scan(&defJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("registry get %s/%s: %w", site, doctype, ErrMetaTypeNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("registry get %s/%s: query tab_doctype: %w", site, doctype, err)
	}

	mt, err := unmarshalMetaType(defJSON)
	if err != nil {
		return nil, fmt.Errorf("registry get %s/%s: unmarshal definition: %w", site, doctype, err)
	}

	// Promote to L1 and L2 (write-through on L3 hit).
	r.l1.Store(key, mt)
	if r.redis != nil {
		rkey := metaRedisKey(site, doctype)
		if serr := r.redis.Set(ctx, rkey, defJSON, 0).Err(); serr != nil {
			r.logger.WarnContext(ctx, "registry: L2 set failed after L3 hit",
				slog.String("site", site), slog.String("doctype", doctype),
				slog.Any("error", serr))
		}
	}

	return mt, nil
}

// Register compiles jsonBytes into a MetaType, evolves the database schema
// via DDL migration, upserts the definition into tab_doctype, and populates
// the L1 and L2 caches. It also increments schema:{site}:version in Redis.
//
// The DDL migration and tab_doctype upsert run atomically inside a single
// PostgreSQL transaction. Cache updates are best-effort post-commit.
//
// Returns *CompileErrors (retrievable via errors.As) if JSON validation fails.
// The MetaType version is incremented by 1 on each successful Register call.
func (r *Registry) Register(ctx context.Context, site string, jsonBytes []byte) (*MetaType, error) {
	// Step 1: Compile — pure validation, no side effects.
	desired, err := Compile(jsonBytes)
	if err != nil {
		return nil, fmt.Errorf("registry register: %w", err)
	}

	if r.db == nil {
		return nil, fmt.Errorf("registry register %s/%s: db unavailable", site, desired.Name)
	}

	pool, err := r.db.ForSite(ctx, site)
	if err != nil {
		return nil, fmt.Errorf("registry register %s/%s: get pool: %w", site, desired.Name, err)
	}

	// Step 2: Fetch current definition from L3 for schema diffing.
	var current *MetaType
	var currentVersion int
	{
		var existingJSON []byte
		qerr := pool.QueryRow(ctx,
			`SELECT definition, version FROM tab_doctype WHERE name = $1`, desired.Name,
		).Scan(&existingJSON, &currentVersion)
		if qerr != nil && !errors.Is(qerr, pgx.ErrNoRows) {
			return nil, fmt.Errorf("registry register %s/%s: query current: %w", site, desired.Name, qerr)
		}
		if existingJSON != nil {
			current, err = unmarshalMetaType(existingJSON)
			if err != nil {
				return nil, fmt.Errorf("registry register %s/%s: unmarshal current: %w", site, desired.Name, err)
			}
		}
	}

	// Step 3: Compute DDL diff (nil current → full CREATE TABLE).
	stmts := r.migrator.Diff(current, desired)

	// Step 4: Marshal the compiled desired MetaType for persistence.
	// Use the compiled form (with defaults applied) rather than raw input.
	defJSON, err := json.Marshal(desired)
	if err != nil {
		return nil, fmt.Errorf("registry register %s/%s: marshal definition: %w", site, desired.Name, err)
	}
	newVersion := currentVersion + 1

	// Step 5: Execute DDL + upsert atomically inside one transaction.
	txErr := orm.WithTransaction(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		for _, stmt := range stmts {
			r.logger.DebugContext(ctx, "registry: apply DDL",
				slog.String("comment", stmt.Comment), slog.String("sql", stmt.SQL))
			if _, execErr := tx.Exec(ctx, stmt.SQL); execErr != nil {
				return fmt.Errorf("execute DDL %q: %w", stmt.Comment, execErr)
			}
		}
		_, execErr := tx.Exec(ctx, `
			INSERT INTO tab_doctype (name, module, definition, version, is_custom, owner)
			VALUES ($1, $2, $3, $4, false, 'System')
			ON CONFLICT (name) DO UPDATE SET
				module     = EXCLUDED.module,
				definition = EXCLUDED.definition,
				version    = EXCLUDED.version,
				modified   = NOW()
		`, desired.Name, desired.Module, json.RawMessage(defJSON), newVersion)
		return execErr
	})
	if txErr != nil {
		return nil, fmt.Errorf("registry register %s/%s: transaction: %w", site, desired.Name, txErr)
	}

	// Step 6: Post-commit cache update (best-effort — DB is source of truth).
	r.l1.Store(l1Key(site, desired.Name), desired)

	if r.redis != nil {
		rkey := metaRedisKey(site, desired.Name)
		if serr := r.redis.Set(ctx, rkey, defJSON, 0).Err(); serr != nil {
			r.logger.WarnContext(ctx, "registry: L2 set failed after register",
				slog.String("site", site), slog.String("doctype", desired.Name),
				slog.Any("error", serr))
		}
		if _, incrErr := r.redis.Incr(ctx, schemaVersionKey(site)).Result(); incrErr != nil {
			r.logger.WarnContext(ctx, "registry: schema version increment failed",
				slog.String("site", site), slog.Any("error", incrErr))
		}
	}

	return desired, nil
}

// Invalidate removes the MetaType for the given doctype from L1 and L2 caches.
// L3 (tab_doctype) is never modified — it is the source of truth.
// Subsequent Get calls will re-hydrate from L3 and repopulate L1/L2.
func (r *Registry) Invalidate(ctx context.Context, site, doctype string) error {
	r.l1.Delete(l1Key(site, doctype))
	if r.redis != nil {
		if err := r.redis.Del(ctx, metaRedisKey(site, doctype)).Err(); err != nil {
			r.logger.WarnContext(ctx, "registry: L2 del failed during invalidate",
				slog.String("site", site), slog.String("doctype", doctype),
				slog.Any("error", err))
		}
	}
	return nil
}

// InvalidateAll removes all MetaType entries for the given site from L1 and L2.
// L3 (tab_doctype) is never modified. Subsequent Get calls re-hydrate from L3.
func (r *Registry) InvalidateAll(ctx context.Context, site string) error {
	// L1: range and delete all keys with the site prefix.
	prefix := site + ":"
	r.l1.Range(func(k, _ any) bool {
		key, ok := k.(string)
		if ok && strings.HasPrefix(key, prefix) {
			r.l1.Delete(k)
		}
		return true
	})

	// L2: delete all matching Redis keys for this site.
	if r.redis != nil {
		pattern := fmt.Sprintf(metaKeyFmt, site, "*")
		keys, err := r.redis.Keys(ctx, pattern).Result()
		if err != nil {
			r.logger.WarnContext(ctx, "registry: redis keys failed during invalidate-all",
				slog.String("site", site), slog.Any("error", err))
			return nil
		}
		if len(keys) > 0 {
			if err := r.redis.Del(ctx, keys...).Err(); err != nil {
				r.logger.WarnContext(ctx, "registry: redis del failed during invalidate-all",
					slog.String("site", site), slog.Any("error", err))
			}
		}
	}
	return nil
}

// SchemaVersion returns the current schema version counter for the given site.
// The counter lives in Redis at schema:{site}:version and is incremented by
// Register on each successful registration. Returns 0 if no version exists yet
// or if Redis is unavailable.
func (r *Registry) SchemaVersion(ctx context.Context, site string) (int64, error) {
	if r.redis == nil {
		return 0, nil
	}
	val, err := r.redis.Get(ctx, schemaVersionKey(site)).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("registry schema version for site %q: %w", site, err)
	}
	return val, nil
}

// ListAll returns every MetaType for the given site from L3 (PostgreSQL).
// Results are promoted into L1 and L2 caches. Rows that fail to unmarshal
// are logged and skipped rather than failing the entire call.
//
// Returns an empty (non-nil) slice if the database is unavailable or the
// site has no registered MetaTypes.
func (r *Registry) ListAll(ctx context.Context, site string) ([]*MetaType, error) {
	if r.db == nil {
		return []*MetaType{}, nil
	}

	pool, err := r.db.ForSite(ctx, site)
	if err != nil {
		return nil, fmt.Errorf("registry listall %s: get pool: %w", site, err)
	}

	rows, err := pool.Query(ctx, `SELECT name, definition FROM tab_doctype`)
	if err != nil {
		return nil, fmt.Errorf("registry listall %s: query: %w", site, err)
	}
	defer rows.Close()

	var results []*MetaType
	for rows.Next() {
		var name string
		var defJSON []byte
		if err := rows.Scan(&name, &defJSON); err != nil {
			return nil, fmt.Errorf("registry listall %s: scan: %w", site, err)
		}

		mt, merr := unmarshalMetaType(defJSON)
		if merr != nil {
			r.logger.WarnContext(ctx, "registry: unmarshal failed during listall",
				slog.String("site", site), slog.String("doctype", name),
				slog.Any("error", merr))
			continue
		}

		// Promote to L1.
		r.l1.Store(l1Key(site, name), mt)

		// Promote to L2 (best-effort).
		if r.redis != nil {
			rkey := metaRedisKey(site, name)
			if serr := r.redis.Set(ctx, rkey, defJSON, 0).Err(); serr != nil {
				r.logger.WarnContext(ctx, "registry: L2 set failed during listall",
					slog.String("site", site), slog.String("doctype", name),
					slog.Any("error", serr))
			}
		}

		results = append(results, mt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("registry listall %s: rows: %w", site, err)
	}

	if results == nil {
		results = []*MetaType{}
	}
	return results, nil
}

// SeedL1ForTest stores a MetaType directly into the L1 cache.
// Intended for unit tests that verify L1 behavior without Redis or PostgreSQL.
// Not for use in production code.
func (r *Registry) SeedL1ForTest(site, doctype string, mt *MetaType) {
	r.l1.Store(l1Key(site, doctype), mt)
}

// l1Key returns the sync.Map key for a (site, doctype) pair.
func l1Key(site, doctype string) string {
	return site + ":" + doctype
}

// metaRedisKey returns the Redis key for a MetaType cache entry.
func metaRedisKey(site, doctype string) string {
	return fmt.Sprintf(metaKeyFmt, site, doctype)
}

// schemaVersionKey returns the Redis key for a site's schema version counter.
func schemaVersionKey(site string) string {
	return fmt.Sprintf(schemaVersionKeyFmt, site)
}

// unmarshalMetaType decodes JSON or JSONB bytes into a MetaType value.
func unmarshalMetaType(data []byte) (*MetaType, error) {
	var mt MetaType
	if err := json.Unmarshal(data, &mt); err != nil {
		return nil, err
	}
	return &mt, nil
}

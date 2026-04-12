package drivers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/internal/config"
)

// Key pattern format strings for Redis keys used across the Moca framework.
// Callers use fmt.Sprintf with these constants to build concrete keys.
// See MOCA_SYSTEM_DESIGN.md §5.1 for TTL and invalidation details.
const (
	KeyMeta    = "meta:%s:%s"        // meta:{site}:{doctype}      — compiled MetaType cache
	KeyDoc     = "doc:%s:%s:%s"      // doc:{site}:{doctype}:{name} — hot document cache
	KeyPerm    = "perm:%s:%s:%s"     // perm:{site}:{user}:{doctype} — resolved permissions
	KeySession = "session:%s"        // session:{token}             — user session data
	KeyConfig  = "config:%s"         // config:{site}               — site configuration
	KeySchema  = "schema:%s:version" // schema:{site}:version       — schema version counter
	KeyI18n    = "i18n:%s:%s"        // i18n:{site}:{language}      — translation hash map
)

// RedisClients holds four Redis client instances, each targeting a distinct
// logical database for isolation between cache, queue, session, and pub/sub
// workloads. See MOCA_SYSTEM_DESIGN.md §5.1 for the 4-DB layout rationale.
//
// The four clients are intentionally exported: downstream packages (pkg/queue,
// pkg/auth, etc.) accept individual *redis.Client values directly.
type RedisClients struct {
	Cache   *redis.Client // db_cache   (default 0): metadata, document, config caches
	Queue   *redis.Client // db_queue   (default 1): Redis Streams for background jobs
	Session *redis.Client // db_session (default 2): user session storage
	PubSub  *redis.Client // db_pubsub  (default 3): pub/sub for WebSocket + events
	logger  *slog.Logger
}

// NewRedisClients creates four Redis client instances from the given config.
// Each client targets a different logical Redis database as specified by
// cfg.DbCache, cfg.DbQueue, cfg.DbSession, and cfg.DbPubSub.
//
// The constructor does not ping Redis — it is lazy by design. Call Ping
// separately to verify connectivity with a context and timeout of your choice.
func NewRedisClients(cfg config.RedisConfig, logger *slog.Logger) *RedisClients {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	newClient := func(db int) *redis.Client {
		return redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: cfg.Password,
			DB:       db,
		})
	}

	rc := &RedisClients{
		Cache:   newClient(cfg.DbCache),
		Queue:   newClient(cfg.DbQueue),
		Session: newClient(cfg.DbSession),
		PubSub:  newClient(cfg.DbPubSub),
		logger:  logger,
	}
	hook := RedisTracingHook{}
	rc.Cache.AddHook(hook)
	rc.Queue.AddHook(hook)
	rc.Session.AddHook(hook)
	rc.PubSub.AddHook(hook)
	return rc
}

// Ping verifies connectivity to all four Redis databases by issuing a PING
// command to each client. All clients are checked before returning; errors
// from multiple clients are combined via errors.Join.
func (rc *RedisClients) Ping(ctx context.Context) error {
	type namedClient struct {
		client *redis.Client
		name   string
	}
	clients := []namedClient{
		{rc.Cache, "cache"},
		{rc.Queue, "queue"},
		{rc.Session, "session"},
		{rc.PubSub, "pubsub"},
	}

	var errs []error
	for _, nc := range clients {
		if err := nc.client.Ping(ctx).Err(); err != nil {
			errs = append(errs, fmt.Errorf("redis %s (db %d): %w",
				nc.name, nc.client.Options().DB, err))
		} else {
			rc.logger.Info("redis client ready",
				slog.String("role", nc.name),
				slog.Int("db", nc.client.Options().DB),
			)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("redis ping: %w", errors.Join(errs...))
	}
	return nil
}

// Close shuts down all four Redis client connections. Errors from individual
// Close calls are collected and returned via errors.Join.
func (rc *RedisClients) Close() error {
	var errs []error
	if err := rc.Cache.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close cache: %w", err))
	}
	if err := rc.Queue.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close queue: %w", err))
	}
	if err := rc.Session.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close session: %w", err))
	}
	if err := rc.PubSub.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close pubsub: %w", err))
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	rc.logger.Info("redis clients closed")
	return nil
}

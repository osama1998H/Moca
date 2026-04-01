package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// RedisSync holds the Redis clients needed for config sync operations.
// Cache stores the config:{site} key; PubSub publishes change events.
// Either or both may be nil when Redis is unavailable.
type RedisSync struct {
	Cache  *redis.Client
	PubSub *redis.Client
}

// SyncToDatabase writes the resolved config to the moca_system.sites table,
// updates the Redis cache key, and publishes a config.changed event.
//
// Redis failure is non-fatal (logged as warning). DB failure returns an error.
// This implements the Config Sync Contract (System Design §5.1.1, rule 2).
func SyncToDatabase(ctx context.Context, siteName string, cfg map[string]any,
	systemPool *pgxpool.Pool, rs *RedisSync, logger *slog.Logger) error {

	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Update database (fatal on error).
	tag, err := systemPool.Exec(ctx,
		"UPDATE sites SET config = $1, modified_at = NOW() WHERE name = $2",
		configJSON, siteName,
	)
	if err != nil {
		return fmt.Errorf("update sites config: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("site %q not found in database", siteName)
	}

	// Update Redis cache + publish event (non-fatal on error).
	if rs != nil {
		syncRedis(ctx, siteName, configJSON, rs, logger)
	}

	return nil
}

func syncRedis(ctx context.Context, siteName string, configJSON []byte,
	rs *RedisSync, logger *slog.Logger) {

	cacheKey := fmt.Sprintf("config:%s", siteName)
	if rs.Cache != nil {
		if err := rs.Cache.Set(ctx, cacheKey, configJSON, 0).Err(); err != nil {
			logger.Warn("failed to update config cache in Redis",
				slog.String("site", siteName),
				slog.String("error", err.Error()),
			)
		}
	}

	pubsubChannel := fmt.Sprintf("pubsub:config:%s", siteName)
	if rs.PubSub != nil {
		if err := rs.PubSub.Publish(ctx, pubsubChannel, configJSON).Err(); err != nil {
			logger.Warn("failed to publish config.changed event",
				slog.String("site", siteName),
				slog.String("channel", pubsubChannel),
				slog.String("error", err.Error()),
			)
		}
	}
}

// LoadRuntimeConfig queries the live config from the moca_system.sites table.
// Used by `moca config get --runtime` to read the server's active config.
func LoadRuntimeConfig(ctx context.Context, siteName string, systemPool *pgxpool.Pool) (map[string]any, error) {
	var configJSON []byte
	err := systemPool.QueryRow(ctx,
		"SELECT config FROM sites WHERE name = $1", siteName,
	).Scan(&configJSON)
	if err != nil {
		return nil, fmt.Errorf("query site config: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(configJSON, &result); err != nil {
		return nil, fmt.Errorf("unmarshal site config: %w", err)
	}

	return result, nil
}

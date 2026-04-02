package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/pkg/meta"
)

const (
	// permCacheTTL is the TTL for cached permission entries.
	permCacheTTL = 2 * time.Minute

	// permKeyFmt is the Redis key format for cached permissions.
	// Pattern: perm:{site}:{user}:{doctype}
	permKeyFmt = "perm:%s:%s:%s"
)

// CachedPermissionResolver resolves effective permissions for a user on a
// doctype, caching results in Redis. Custom rules are evaluated on every
// call (not cached) since they may depend on dynamic state.
type CachedPermissionResolver struct {
	registry    *meta.Registry
	redisCache  *redis.Client
	customRules *CustomRuleRegistry
	logger      *slog.Logger
}

// NewCachedPermissionResolver creates a resolver. All parameters are nil-safe:
// nil redisCache skips caching, nil customRules skips custom rule evaluation.
func NewCachedPermissionResolver(
	registry *meta.Registry,
	redisCache *redis.Client,
	customRules *CustomRuleRegistry,
	logger *slog.Logger,
) *CachedPermissionResolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &CachedPermissionResolver{
		registry:    registry,
		redisCache:  redisCache,
		customRules: customRules,
		logger:      logger,
	}
}

// Resolve returns the effective permissions for a user on a doctype within
// a site. It checks Redis cache first, then resolves from the MetaType
// registry on cache miss. Custom rules are always re-evaluated.
func (cpr *CachedPermissionResolver) Resolve(
	ctx context.Context,
	site string,
	user *User,
	doctype string,
) (*EffectivePerms, error) {
	if cpr.registry == nil {
		return nil, fmt.Errorf("permission resolver: registry is nil")
	}

	key := fmt.Sprintf(permKeyFmt, site, user.Email, doctype)

	// Try Redis cache.
	if cpr.redisCache != nil {
		data, err := cpr.redisCache.Get(ctx, key).Bytes()
		if err == nil {
			var ep EffectivePerms
			if unmarshalErr := json.Unmarshal(data, &ep); unmarshalErr == nil {
				return cpr.evalCustomRules(ctx, &ep, user, doctype)
			}
			cpr.logger.Warn("perm cache: unmarshal failed, resolving fresh",
				slog.String("key", key))
		} else if err != redis.Nil {
			cpr.logger.Warn("perm cache: redis GET failed",
				slog.String("key", key), slog.String("error", err.Error()))
		}
	}

	// Cache miss — resolve from registry.
	mt, err := cpr.registry.Get(ctx, site, doctype)
	if err != nil {
		return nil, fmt.Errorf("permission resolver: %w", err)
	}

	ep := ResolvePermissions(mt.Permissions, user.Roles)

	// Cache the static result (before custom rule evaluation).
	if cpr.redisCache != nil {
		data, err := json.Marshal(ep)
		if err == nil {
			if setErr := cpr.redisCache.Set(ctx, key, data, permCacheTTL).Err(); setErr != nil {
				cpr.logger.Warn("perm cache: redis SET failed",
					slog.String("key", key), slog.String("error", setErr.Error()))
			}
		}
	}

	return cpr.evalCustomRules(ctx, ep, user, doctype)
}

// evalCustomRules evaluates custom rules referenced in the effective permissions.
// Returns the EffectivePerms unchanged if all rules pass, or an error on denial.
func (cpr *CachedPermissionResolver) evalCustomRules(
	ctx context.Context,
	ep *EffectivePerms,
	user *User,
	doctype string,
) (*EffectivePerms, error) {
	if cpr.customRules == nil || len(ep.CustomRules) == 0 {
		return ep, nil
	}
	if err := cpr.customRules.EvaluateAll(ctx, ep.CustomRules, user, doctype); err != nil {
		return nil, fmt.Errorf("permission resolver: %w", err)
	}
	return ep, nil
}

// InvalidatePermCache removes the cached permission entry for a specific
// user and doctype on a site.
func (cpr *CachedPermissionResolver) InvalidatePermCache(
	ctx context.Context,
	site, userEmail, doctype string,
) error {
	if cpr.redisCache == nil {
		return nil
	}
	key := fmt.Sprintf(permKeyFmt, site, userEmail, doctype)
	return cpr.redisCache.Del(ctx, key).Err()
}

// InvalidateUserPermCache removes all cached permission entries for a user
// on a site, using pattern scanning.
func (cpr *CachedPermissionResolver) InvalidateUserPermCache(
	ctx context.Context,
	site, userEmail string,
) error {
	if cpr.redisCache == nil {
		return nil
	}
	pattern := fmt.Sprintf("perm:%s:%s:*", site, userEmail)

	var cursor uint64
	for {
		keys, next, err := cpr.redisCache.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("perm cache scan: %w", err)
		}
		if len(keys) > 0 {
			if err := cpr.redisCache.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("perm cache del: %w", err)
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return nil
}

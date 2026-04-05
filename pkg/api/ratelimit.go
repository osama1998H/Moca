package api

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/osama1998H/moca/pkg/meta"
	"github.com/redis/go-redis/v9"
)

// RateLimiter implements a sliding window rate limiter backed by Redis sorted sets.
// Each unique key gets a sorted set where members are request timestamps (as scores).
// The window slides by removing entries older than (now - window) before counting.
type RateLimiter struct {
	redis  *redis.Client
	logger *slog.Logger
}

// NewRateLimiter creates a RateLimiter using the given Redis client (typically
// RedisClients.Cache) and logger.
func NewRateLimiter(rdb *redis.Client, logger *slog.Logger) *RateLimiter {
	return &RateLimiter{redis: rdb, logger: logger}
}

// Allow checks whether a request identified by key is permitted under the given
// rate limit config. It returns whether the request is allowed, the duration
// until the next request would be allowed (if denied), and any Redis error.
//
// Algorithm (sliding window log):
//  1. ZREMRANGEBYSCORE key -inf (now - window) — prune expired entries
//  2. ZCARD key — count current entries
//  3. If count < maxRequests+burstSize: ZADD key now now — record this request
//  4. EXPIRE key window — auto-cleanup
func (rl *RateLimiter) Allow(ctx context.Context, key string, cfg *meta.RateLimitConfig) (allowed bool, retryAfter time.Duration, err error) {
	if cfg == nil || cfg.MaxRequests <= 0 {
		return true, 0, nil
	}

	now := time.Now()
	nowNano := float64(now.UnixNano())
	windowStart := float64(now.Add(-cfg.Window).UnixNano())
	limit := cfg.MaxRequests + cfg.BurstSize

	pipe := rl.redis.Pipeline()

	// Remove expired entries.
	pipe.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatFloat(windowStart, 'f', 0, 64))

	// Count remaining entries.
	countCmd := pipe.ZCard(ctx, key)

	_, err = pipe.Exec(ctx)
	if err != nil {
		rl.logger.Error("rate limiter pipeline failed", slog.String("error", err.Error()))
		// Fail open: allow the request if Redis is unavailable.
		return true, 0, err
	}

	count := countCmd.Val()
	if count >= int64(limit) {
		// Denied. Estimate retry-after from the oldest entry in the window.
		oldest, err2 := rl.redis.ZRangeWithScores(ctx, key, 0, 0).Result()
		if err2 == nil && len(oldest) > 0 {
			oldestTime := time.Unix(0, int64(oldest[0].Score))
			retryAfter = oldestTime.Add(cfg.Window).Sub(now)
			if retryAfter < 0 {
				retryAfter = time.Second
			}
		} else {
			retryAfter = cfg.Window
		}
		return false, retryAfter, nil
	}

	// Allowed: record this request.
	member := fmt.Sprintf("%d", now.UnixNano())
	pipe2 := rl.redis.Pipeline()
	pipe2.ZAdd(ctx, key, redis.Z{Score: nowNano, Member: member})
	pipe2.Expire(ctx, key, cfg.Window+time.Second) // TTL slightly beyond window for safety
	_, err = pipe2.Exec(ctx)
	if err != nil {
		rl.logger.Error("rate limiter record failed", slog.String("error", err.Error()))
		// Request was already allowed logically; don't revoke.
	}

	return true, 0, nil
}

// rateLimitMiddleware wraps requests with rate limiting. When an API key is
// present in context, it uses the key-specific rate limit config and a Redis
// key pattern of "rl:{site}:apikey:{keyID}". Otherwise it falls back to the
// default per-user pattern "rl:{site}:{user}".
func rateLimitMiddleware(rl *RateLimiter, defaultCfg *meta.RateLimitConfig) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Determine rate limit key and config.
			var key string
			cfg := defaultCfg

			if apiKeyID := APIKeyIDFromContext(r.Context()); apiKeyID != "" {
				// Per-key rate limiting: use key-specific config if set.
				site := "unknown"
				if s := SiteFromContext(r.Context()); s != nil {
					site = s.Name
				}
				key = fmt.Sprintf("rl:%s:apikey:%s", site, apiKeyID)
				if apiCfg := APIRateLimitFromContext(r.Context()); apiCfg != nil {
					cfg = apiCfg
				}
			} else {
				key = rateLimitKey(r.Context())
			}

			if rl == nil || cfg == nil || cfg.MaxRequests <= 0 {
				next.ServeHTTP(w, r)
				return
			}

			allowed, retryAfter, _ := rl.Allow(r.Context(), key, cfg)
			if !allowed {
				retrySeconds := int(math.Ceil(retryAfter.Seconds()))
				if retrySeconds < 1 {
					retrySeconds = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
				http.Error(w, `{"error":{"code":"RATE_LIMITED","message":"too many requests"}}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// rateLimitKey builds a Redis key from the site and user in context.
// Format: "rl:{site}:{user}" or "rl:unknown:{ip}" when context is empty.
func rateLimitKey(ctx context.Context) string {
	site := "unknown"
	user := "anonymous"

	if s := SiteFromContext(ctx); s != nil {
		site = s.Name
	}
	if u := UserFromContext(ctx); u != nil && u.Email != "" {
		user = u.Email
	}

	return fmt.Sprintf("rl:%s:%s", site, user)
}

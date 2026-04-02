//go:build integration

package api_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/osama1998H/moca/pkg/api"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/observe"
)

var (
	rateLimiterAllowedSink    bool
	rateLimiterRetryAfterSink time.Duration
)

func BenchmarkRateLimiterAllow_SingleKey(b *testing.B) {
	if apiRedisClient == nil {
		b.Skip("Redis unavailable")
	}

	ctx := context.Background()
	key := "bench:ratelimit:single"
	rl := api.NewRateLimiter(apiRedisClient, observe.NewLogger(slog.LevelWarn))
	cfg := &meta.RateLimitConfig{
		MaxRequests: 1,
		Window:      time.Second,
	}

	b.Cleanup(func() {
		_ = apiRedisClient.Del(ctx, key).Err()
	})

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		if err := apiRedisClient.Del(ctx, key).Err(); err != nil {
			b.Fatalf("reset rate limit key: %v", err)
		}
		b.StartTimer()

		allowed, retryAfter, err := rl.Allow(ctx, key, cfg)
		if err != nil {
			b.Fatalf("Allow single key: %v", err)
		}
		if !allowed {
			b.Fatalf("Allow single key denied with retry_after=%s", retryAfter)
		}
		rateLimiterAllowedSink = allowed
		rateLimiterRetryAfterSink = retryAfter
	}
}

func BenchmarkRateLimiterAllow_Parallel(b *testing.B) {
	if apiRedisClient == nil {
		b.Skip("Redis unavailable")
	}

	ctx := context.Background()
	key := "bench:ratelimit:parallel"
	rl := api.NewRateLimiter(apiRedisClient, observe.NewLogger(slog.LevelWarn))
	cfg := &meta.RateLimitConfig{
		MaxRequests: 1_000_000,
		Window:      time.Nanosecond,
	}

	if err := apiRedisClient.Del(ctx, key).Err(); err != nil {
		b.Fatalf("reset parallel rate limit key: %v", err)
	}
	b.Cleanup(func() {
		_ = apiRedisClient.Del(ctx, key).Err()
	})

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			allowed, retryAfter, err := rl.Allow(ctx, key, cfg)
			if err != nil {
				b.Fatalf("Allow parallel key: %v", err)
			}
			if !allowed {
				b.Fatalf("Allow parallel key denied with retry_after=%s", retryAfter)
			}
		}
	})
}

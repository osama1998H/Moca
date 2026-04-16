//go:build integration

package drivers_test

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/osama1998H/moca/internal/drivers"
	"github.com/osama1998H/moca/pkg/observe"
)

var redisBenchmarkStringSink string

// newBenchClients creates a RedisClients for benchmarks.
// Separate from newTestClients (which accepts *testing.T) because
// *testing.B is a distinct type even though both satisfy testing.TB.
func newBenchClients(b *testing.B) *drivers.RedisClients {
	b.Helper()
	logger := observe.NewLogger(slog.LevelWarn)
	rc := drivers.NewRedisClients(testRedisConfig(), logger)
	b.Cleanup(func() { _ = rc.Close() })
	return rc
}

func BenchmarkRedisGetSet_SingleKey(b *testing.B) {
	rc := newBenchClients(b)
	ctx := context.Background()
	key := "bench:single"
	value := "benchmark-payload-64bytes-padded-to-simulate-real-world-usage!!"

	b.Cleanup(func() { rc.Cache.Del(ctx, key) })

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := rc.Cache.Set(ctx, key, value, 10*time.Second).Err(); err != nil {
			b.Fatalf("SET: %v", err)
		}
		got, err := rc.Cache.Get(ctx, key).Result()
		if err != nil {
			b.Fatalf("GET: %v", err)
		}
		redisBenchmarkStringSink = got
	}
}

func BenchmarkRedisGetSet_Pipeline(b *testing.B) {
	rc := newBenchClients(b)
	ctx := context.Background()

	keys := make([]string, 10)
	for i := range keys {
		keys[i] = fmt.Sprintf("bench:pipe:%d", i)
	}
	b.Cleanup(func() {
		for _, k := range keys {
			rc.Cache.Del(ctx, k)
		}
	})

	value := "benchmark-payload-64bytes-padded-to-simulate-real-world-usage!!"

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pipe := rc.Cache.Pipeline()
		for _, k := range keys {
			pipe.Set(ctx, k, value, 10*time.Second)
		}
		for _, k := range keys {
			pipe.Get(ctx, k)
		}
		cmds, err := pipe.Exec(ctx)
		if err != nil {
			b.Fatalf("Pipeline exec: %v", err)
		}
		_ = cmds
	}
}

func BenchmarkRedisGetSet_Parallel(b *testing.B) {
	rc := newBenchClients(b)
	ctx := context.Background()
	value := "benchmark-payload-64bytes-padded-to-simulate-real-world-usage!!"
	var seq atomic.Uint64

	b.Cleanup(func() {
		iter := rc.Cache.Scan(ctx, 0, "bench:par:*", 1000).Iterator()
		for iter.Next(ctx) {
			rc.Cache.Del(ctx, iter.Val())
		}
	})

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			n := seq.Add(1)
			key := fmt.Sprintf("bench:par:%d", n)
			if err := rc.Cache.Set(ctx, key, value, 10*time.Second).Err(); err != nil {
				b.Fatalf("SET: %v", err)
			}
			got, err := rc.Cache.Get(ctx, key).Result()
			if err != nil {
				b.Fatalf("GET: %v", err)
			}
			redisBenchmarkStringSink = got
		}
	})
}

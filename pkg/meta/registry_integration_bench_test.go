//go:build integration

package meta_test

import (
	"encoding/json"
	"testing"

	"github.com/osama1998H/moca/internal/testutil/bench"
	"github.com/osama1998H/moca/pkg/meta"
)

var registryIntegrationBenchmarkSink *meta.MetaType

func BenchmarkRegistryGet_L2Hit(b *testing.B) {
	env := bench.NewIntegrationEnv(b, "meta_registry_l2")
	redisClient := env.RequireRedis(b)

	mt := env.RegisterMetaType(b, bench.SimpleDocType("BenchRegistryDoc"))
	registry := env.Registry()
	payload, err := json.Marshal(mt)
	if err != nil {
		b.Fatalf("marshal registered MetaType: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		if err := registry.Invalidate(env.Ctx, env.SiteName, mt.Name); err != nil {
			b.Fatalf("invalidate registry cache: %v", err)
		}
		if err := redisClient.Set(env.Ctx, env.MetaRedisKey(mt.Name), payload, 0).Err(); err != nil {
			b.Fatalf("seed Redis cache: %v", err)
		}
		b.StartTimer()

		got, err := registry.Get(env.Ctx, env.SiteName, mt.Name)
		if err != nil {
			b.Fatalf("Registry.Get L2 hit: %v", err)
		}
		registryIntegrationBenchmarkSink = got
	}
}

func BenchmarkRegistryGet_L3Miss(b *testing.B) {
	env := bench.NewIntegrationEnv(b, "meta_registry_l3")
	env.RequireRedis(b)

	mt := env.RegisterMetaType(b, bench.SimpleDocType("BenchRegistryDoc"))
	registry := env.Registry()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		if err := registry.Invalidate(env.Ctx, env.SiteName, mt.Name); err != nil {
			b.Fatalf("invalidate registry cache: %v", err)
		}
		b.StartTimer()

		got, err := registry.Get(env.Ctx, env.SiteName, mt.Name)
		if err != nil {
			b.Fatalf("Registry.Get L3 miss: %v", err)
		}
		registryIntegrationBenchmarkSink = got
	}
}

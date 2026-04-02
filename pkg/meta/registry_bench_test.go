package meta

import (
	"context"
	"log/slog"
	"testing"

	"github.com/osama1998H/moca/pkg/observe"
)

var registryBenchmarkSink *MetaType

func BenchmarkRegistryGet_L1Hit(b *testing.B) {
	registry := NewRegistry(nil, nil, observe.NewLogger(slog.LevelWarn))
	ctx := context.Background()
	mt := &MetaType{
		Name:   "BenchRegistryDoc",
		Module: "bench",
	}
	registry.l1.Store(l1Key("bench_site", mt.Name), mt)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		got, err := registry.Get(ctx, "bench_site", mt.Name)
		if err != nil {
			b.Fatalf("Registry.Get L1 hit: %v", err)
		}
		registryBenchmarkSink = got
	}
}

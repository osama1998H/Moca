//go:build !race

package hooks

import (
	"testing"

	"github.com/osama1998H/moca/pkg/document"
)

func TestHookRegistryResolve_10Hooks_Budget(t *testing.T) {
	bench := func(b *testing.B) {
		registry := NewHookRegistry()
		for i := 0; i < 6; i++ {
			registry.Register("BenchHookDoc", document.EventBeforeSave, PrioritizedHandler{
				Handler:  func(_ *document.DocContext, _ document.Document) error { return nil },
				AppName:  string(rune('a' + i)),
				Priority: 100 + i*10,
			})
		}
		for i := 0; i < 4; i++ {
			registry.RegisterGlobal(document.EventBeforeSave, PrioritizedHandler{
				Handler:  func(_ *document.DocContext, _ document.Document) error { return nil },
				AppName:  string(rune('g' + i)),
				Priority: 50 + i*10,
			})
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			hookRegistryBenchmarkSink, _ = registry.Resolve("BenchHookDoc", document.EventBeforeSave)
		}
	}

	result := testing.Benchmark(bench)
	nsPerOp := result.NsPerOp()
	budget := int64(5_000) // 5 µs
	if nsPerOp > budget {
		t.Errorf("HookRegistry.Resolve 10 hooks: %d ns/op exceeds budget of %d ns/op", nsPerOp, budget)
	}
	t.Logf("HookRegistry.Resolve 10 hooks: %d ns/op (budget: %d ns/op, used: %d%%)", nsPerOp, budget, nsPerOp*100/budget)
}

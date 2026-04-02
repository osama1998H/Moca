package hooks

import (
	"testing"

	"github.com/osama1998H/moca/pkg/document"
)

var hookRegistryBenchmarkSink []PrioritizedHandler

func BenchmarkHookRegistryResolve_NoHooks(b *testing.B) {
	registry := NewHookRegistry()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		handlers, err := registry.Resolve("BenchHookDoc", document.EventBeforeSave)
		if err != nil {
			b.Fatalf("Resolve no hooks: %v", err)
		}
		hookRegistryBenchmarkSink = handlers
	}
}

func BenchmarkHookRegistryResolve_10Hooks(b *testing.B) {
	registry := NewHookRegistry()
	for i := 0; i < 6; i++ {
		registry.Register("BenchHookDoc", document.EventBeforeSave, PrioritizedHandler{
			Handler:  benchmarkHookHandler,
			AppName:  "local_app_" + string(rune('a'+i)),
			Priority: 100 + i*10,
		})
	}
	for i := 0; i < 4; i++ {
		registry.RegisterGlobal(document.EventBeforeSave, PrioritizedHandler{
			Handler:  benchmarkHookHandler,
			AppName:  "global_app_" + string(rune('a'+i)),
			Priority: 50 + i*10,
		})
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		handlers, err := registry.Resolve("BenchHookDoc", document.EventBeforeSave)
		if err != nil {
			b.Fatalf("Resolve 10 hooks: %v", err)
		}
		hookRegistryBenchmarkSink = handlers
	}
}

func BenchmarkHookRegistryResolve_WithDeps(b *testing.B) {
	registry := NewHookRegistry()
	registry.RegisterGlobal(document.EventBeforeSave, PrioritizedHandler{
		Handler:  benchmarkHookHandler,
		AppName:  "audit",
		Priority: 300,
	})
	registry.Register("BenchHookDoc", document.EventBeforeSave, PrioritizedHandler{
		Handler:   benchmarkHookHandler,
		AppName:   "crm",
		Priority:  200,
		DependsOn: []string{"audit"},
	})
	registry.Register("BenchHookDoc", document.EventBeforeSave, PrioritizedHandler{
		Handler:   benchmarkHookHandler,
		AppName:   "pricing",
		Priority:  100,
		DependsOn: []string{"crm"},
	})
	registry.Register("BenchHookDoc", document.EventBeforeSave, PrioritizedHandler{
		Handler:   benchmarkHookHandler,
		AppName:   "billing",
		Priority:  50,
		DependsOn: []string{"crm", "pricing"},
	})
	registry.Register("BenchHookDoc", document.EventBeforeSave, PrioritizedHandler{
		Handler:  benchmarkHookHandler,
		AppName:  "notifications",
		Priority: 400,
	})
	registry.RegisterGlobal(document.EventBeforeSave, PrioritizedHandler{
		Handler:   benchmarkHookHandler,
		AppName:   "analytics",
		Priority:  250,
		DependsOn: []string{"audit"},
	})

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		handlers, err := registry.Resolve("BenchHookDoc", document.EventBeforeSave)
		if err != nil {
			b.Fatalf("Resolve with deps: %v", err)
		}
		hookRegistryBenchmarkSink = handlers
	}
}

func benchmarkHookHandler(_ *document.DocContext, _ document.Document) error {
	return nil
}

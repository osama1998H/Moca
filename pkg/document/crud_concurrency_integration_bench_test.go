//go:build integration

package document_test

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/osama1998H/moca/internal/testutil/bench"
	"github.com/osama1998H/moca/pkg/document"
)

var concurrencyBenchmarkSink atomic.Pointer[document.DynamicDoc]

func BenchmarkDocManagerInsert_Concurrency(b *testing.B) {
	for _, goroutines := range []int{1, 10, 50, 100, 500} {
		b.Run(fmt.Sprintf("Goroutines_%d", goroutines), func(b *testing.B) {
			env := bench.NewIntegrationEnv(b, fmt.Sprintf("conc_%d", goroutines))
			mt := env.RegisterMetaType(b, bench.SimpleDocType("BenchOrder"))
			dm := env.DocManager()
			var seq atomic.Uint64

			b.SetParallelism(goroutines)
			b.ReportAllocs()
			b.ResetTimer()

			b.RunParallel(func(pb *testing.PB) {
				docCtx := env.DocContext()
				for pb.Next() {
					n := int(seq.Add(1))
					inserted, err := dm.Insert(docCtx, mt.Name, bench.SimpleDocValues(n))
					if err != nil {
						b.Fatalf("DocManager.Insert concurrency (%d goroutines): %v", goroutines, err)
					}
					concurrencyBenchmarkSink.Store(inserted)
				}
			})
		})
	}
}

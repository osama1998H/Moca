//go:build integration

package document_test

import (
	"sync/atomic"
	"testing"

	"github.com/osama1998H/moca/internal/testutil/bench"
	"github.com/osama1998H/moca/pkg/document"
)

var (
	docManagerBenchmarkSink      *document.DynamicDoc
	docManagerBenchmarkListSink  []*document.DynamicDoc
	docManagerBenchmarkTotalSink int
)

func BenchmarkDocManagerGet_SimpleDoc(b *testing.B) {
	env := bench.NewIntegrationEnv(b, "document_get")
	mt := env.RegisterMetaType(b, bench.SimpleDocType("BenchOrder"))
	dm := env.DocManager()
	docCtx := env.DocContext()

	seeded, err := dm.Insert(docCtx, mt.Name, bench.SimpleDocValues(1))
	if err != nil {
		b.Fatalf("seed simple document: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		got, err := dm.Get(docCtx, mt.Name, seeded.Name())
		if err != nil {
			b.Fatalf("DocManager.Get: %v", err)
		}
		docManagerBenchmarkSink = got
	}
}

func BenchmarkDocManagerGetList_SimpleFilter(b *testing.B) {
	env := bench.NewIntegrationEnv(b, "document_get_list")
	mt := env.RegisterMetaType(b, bench.SimpleDocType("BenchOrder"))
	dm := env.DocManager()
	docCtx := env.DocContext()

	for i := 0; i < 50; i++ {
		values := bench.SimpleDocValues(i)
		if i%2 == 0 {
			values["status"] = "Closed"
		}
		if _, err := dm.Insert(docCtx, mt.Name, values); err != nil {
			b.Fatalf("seed list document %d: %v", i, err)
		}
	}

	opts := document.ListOptions{
		Filters:  map[string]any{"status": "Open"},
		OrderBy:  "modified",
		OrderDir: "DESC",
		Limit:    20,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		docs, total, err := dm.GetList(docCtx, mt.Name, opts)
		if err != nil {
			b.Fatalf("DocManager.GetList: %v", err)
		}
		docManagerBenchmarkListSink = docs
		docManagerBenchmarkTotalSink = total
	}
}

func BenchmarkDocManagerInsert_SimpleDoc(b *testing.B) {
	env := bench.NewIntegrationEnv(b, "document_insert_simple")
	mt := env.RegisterMetaType(b, bench.SimpleDocType("BenchOrder"))
	dm := env.DocManager()
	docCtx := env.DocContext()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		inserted, err := dm.Insert(docCtx, mt.Name, bench.SimpleDocValues(i))
		if err != nil {
			b.Fatalf("DocManager.Insert simple: %v", err)
		}
		docManagerBenchmarkSink = inserted
	}
}

func BenchmarkDocManagerInsert_ComplexDoc(b *testing.B) {
	env := bench.NewIntegrationEnv(b, "document_insert_complex")
	child := env.RegisterMetaType(b, bench.ChildDocType("BenchOrderItem"))
	parent := env.RegisterMetaType(b, bench.ComplexDocType("BenchComplexOrder", child.Name))
	dm := env.DocManager()
	docCtx := env.DocContext()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		inserted, err := dm.Insert(docCtx, parent.Name, bench.ComplexDocValues(i))
		if err != nil {
			b.Fatalf("DocManager.Insert complex: %v", err)
		}
		docManagerBenchmarkSink = inserted
	}
}

func BenchmarkDocManagerInsert_Parallel(b *testing.B) {
	env := bench.NewIntegrationEnv(b, "document_insert_parallel")
	mt := env.RegisterMetaType(b, bench.SimpleDocType("BenchOrder"))
	dm := env.DocManager()
	var seq atomic.Uint64
	var last atomic.Pointer[document.DynamicDoc]

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		docCtx := env.DocContext()
		for pb.Next() {
			n := int(seq.Add(1))
			inserted, err := dm.Insert(docCtx, mt.Name, bench.SimpleDocValues(n))
			if err != nil {
				b.Fatalf("DocManager.Insert parallel: %v", err)
			}
			last.Store(inserted)
		}
	})

	docManagerBenchmarkSink = last.Load()
}

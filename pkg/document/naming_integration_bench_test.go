//go:build integration

package document_test

import (
	"context"
	"testing"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
)

var namingIntegrationBenchmarkSink string

func BenchmarkNamingEngineGenerateName_AutoIncrement(b *testing.B) {
	engine := document.NewNamingEngine()
	doc := document.NewDynamicDoc(&meta.MetaType{
		Name:   "BenchAutoIncrementNaming",
		Module: "bench",
		NamingRule: meta.NamingStrategy{
			Rule: meta.NamingAutoIncrement,
		},
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData},
		},
	}, nil, true)

	if _, err := engine.GenerateName(context.Background(), doc, namingTestPool); err != nil {
		b.Fatalf("warm auto-increment naming sequence: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		name, err := engine.GenerateName(context.Background(), doc, namingTestPool)
		if err != nil {
			b.Fatalf("GenerateName autoincrement: %v", err)
		}
		namingIntegrationBenchmarkSink = name
	}
}

func BenchmarkNamingEngineGenerateName_Pattern(b *testing.B) {
	engine := document.NewNamingEngine()
	doc := document.NewDynamicDoc(&meta.MetaType{
		Name:   "BenchPatternNaming",
		Module: "bench",
		NamingRule: meta.NamingStrategy{
			Rule:    meta.NamingByPattern,
			Pattern: "BN-.#####",
		},
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData},
		},
	}, nil, true)

	if _, err := engine.GenerateName(context.Background(), doc, namingTestPool); err != nil {
		b.Fatalf("warm pattern naming sequence: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		name, err := engine.GenerateName(context.Background(), doc, namingTestPool)
		if err != nil {
			b.Fatalf("GenerateName pattern: %v", err)
		}
		namingIntegrationBenchmarkSink = name
	}
}

func BenchmarkNamingEngineGenerateName_PatternParallel(b *testing.B) {
	engine := document.NewNamingEngine()
	doc := document.NewDynamicDoc(&meta.MetaType{
		Name:   "BenchPatternParallelNaming",
		Module: "bench",
		NamingRule: meta.NamingStrategy{
			Rule:    meta.NamingByPattern,
			Pattern: "BP-.#####",
		},
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData},
		},
	}, nil, true)

	if _, err := engine.GenerateName(context.Background(), doc, namingTestPool); err != nil {
		b.Fatalf("warm parallel pattern naming sequence: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		last := ""
		for pb.Next() {
			name, err := engine.GenerateName(context.Background(), doc, namingTestPool)
			if err != nil {
				b.Fatalf("GenerateName pattern parallel: %v", err)
			}
			last = name
		}
		namingIntegrationBenchmarkSink = last
	})
}

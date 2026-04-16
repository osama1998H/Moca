package meta

import "testing"

func TestRegistryGet_L1Hit_Budget(t *testing.T) {
	result := testing.Benchmark(BenchmarkRegistryGet_L1Hit)
	nsPerOp := result.NsPerOp()
	if nsPerOp > 200 {
		t.Errorf("Registry.Get L1 hit: %d ns/op exceeds budget of 200 ns/op", nsPerOp)
	}
	t.Logf("Registry.Get L1 hit: %d ns/op (budget: 200 ns/op, used: %d%%)", nsPerOp, nsPerOp*100/200)
}

func TestGenerateTableDDL_10Fields_Budget(t *testing.T) {
	bench := func(b *testing.B) {
		mt := benchmarkDDLMetaType(10)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ddlBenchmarkSink = GenerateTableDDL(mt)
		}
	}
	result := testing.Benchmark(bench)
	nsPerOp := result.NsPerOp()
	budget := int64(50_000) // 50 µs
	if nsPerOp > budget {
		t.Errorf("GenerateTableDDL 10 fields: %d ns/op exceeds budget of %d ns/op", nsPerOp, budget)
	}
	t.Logf("GenerateTableDDL 10 fields: %d ns/op (budget: %d ns/op, used: %d%%)", nsPerOp, budget, nsPerOp*100/budget)
}

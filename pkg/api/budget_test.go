package api

import (
	"testing"
)

func TestTransformerChain_Response_Budget(t *testing.T) {
	result := testing.Benchmark(BenchmarkTransformerChain_Response)
	nsPerOp := result.NsPerOp()
	budget := int64(20_000) // 20 µs
	if nsPerOp > budget {
		t.Errorf("TransformerChain Response: %d ns/op exceeds budget of %d ns/op", nsPerOp, budget)
	}
	t.Logf("TransformerChain Response: %d ns/op (budget: %d ns/op, used: %d%%)", nsPerOp, budget, nsPerOp*100/budget)
}

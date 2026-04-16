//go:build integration

package orm_test

import (
	"context"
	"fmt"
	"testing"
)

var poolSaturationBenchmarkSink int

func BenchmarkPoolSaturation(b *testing.B) {
	if adminPool == nil {
		b.Skip("PostgreSQL integration fixtures unavailable")
	}

	ctx := context.Background()
	if err := adminPool.QueryRow(ctx, "SELECT 1").Scan(&poolSaturationBenchmarkSink); err != nil {
		b.Fatalf("warm pool: %v", err)
	}

	for _, goroutines := range []int{1, 10, 50, 100, 500} {
		b.Run(fmt.Sprintf("Goroutines_%d", goroutines), func(b *testing.B) {
			b.SetParallelism(goroutines)
			b.ReportAllocs()
			b.ResetTimer()

			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					var n int
					if err := adminPool.QueryRow(ctx, "SELECT 1").Scan(&n); err != nil {
						b.Fatalf("SELECT 1: %v", err)
					}
					poolSaturationBenchmarkSink = n
				}
			})
		})
	}
}

//go:build integration

package orm_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/osama1998H/moca/pkg/orm"
)

var transactionBenchmarkIntSink int

func BenchmarkWithTransaction_Select1(b *testing.B) {
	if adminPool == nil {
		b.Skip("PostgreSQL integration fixtures unavailable")
	}

	ctx := context.Background()

	if err := orm.WithTransaction(ctx, adminPool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT 1").Scan(&transactionBenchmarkIntSink)
	}); err != nil {
		b.Fatalf("warm transaction benchmark: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := orm.WithTransaction(ctx, adminPool, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT 1").Scan(&transactionBenchmarkIntSink)
		}); err != nil {
			b.Fatalf("WithTransaction SELECT 1: %v", err)
		}
	}
}

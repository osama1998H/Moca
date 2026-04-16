//go:build integration

package orm_test

import (
	"context"
	"fmt"
	"testing"
)

var pgRoundTripBenchmarkIntSink int64
var pgRoundTripBenchmarkStrSink string

func BenchmarkPGRoundTrip_Insert(b *testing.B) {
	if adminPool == nil {
		b.Skip("PostgreSQL integration fixtures unavailable")
	}

	ctx := context.Background()

	_, err := adminPool.Exec(ctx, `CREATE TABLE IF NOT EXISTS bench_pg_roundtrip (
		id BIGSERIAL PRIMARY KEY,
		name TEXT NOT NULL,
		value JSONB,
		created_at TIMESTAMPTZ DEFAULT now()
	)`)
	if err != nil {
		b.Fatalf("create bench table: %v", err)
	}
	b.Cleanup(func() {
		adminPool.Exec(context.Background(), "DROP TABLE IF EXISTS bench_pg_roundtrip")
	})

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := adminPool.Exec(ctx,
			`INSERT INTO bench_pg_roundtrip (name, value) VALUES ($1, $2)`,
			fmt.Sprintf("bench-%d", i),
			fmt.Sprintf(`{"seq":%d}`, i),
		)
		if err != nil {
			b.Fatalf("INSERT: %v", err)
		}
	}
}

func BenchmarkPGRoundTrip_Select(b *testing.B) {
	if adminPool == nil {
		b.Skip("PostgreSQL integration fixtures unavailable")
	}

	ctx := context.Background()

	_, err := adminPool.Exec(ctx, `CREATE TABLE IF NOT EXISTS bench_pg_roundtrip_sel (
		id BIGSERIAL PRIMARY KEY,
		name TEXT NOT NULL,
		value JSONB,
		created_at TIMESTAMPTZ DEFAULT now()
	)`)
	if err != nil {
		b.Fatalf("create bench table: %v", err)
	}
	b.Cleanup(func() {
		adminPool.Exec(context.Background(), "DROP TABLE IF EXISTS bench_pg_roundtrip_sel")
	})

	var seededID int64
	err = adminPool.QueryRow(ctx,
		`INSERT INTO bench_pg_roundtrip_sel (name, value) VALUES ($1, $2) RETURNING id`,
		"seed-row", `{"seed":true}`,
	).Scan(&seededID)
	if err != nil {
		b.Fatalf("seed row: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var name string
		err := adminPool.QueryRow(ctx,
			`SELECT name FROM bench_pg_roundtrip_sel WHERE id = $1`, seededID,
		).Scan(&name)
		if err != nil {
			b.Fatalf("SELECT: %v", err)
		}
		pgRoundTripBenchmarkStrSink = name
	}
}

func BenchmarkPGRoundTrip_InsertSelect(b *testing.B) {
	if adminPool == nil {
		b.Skip("PostgreSQL integration fixtures unavailable")
	}

	ctx := context.Background()

	_, err := adminPool.Exec(ctx, `CREATE TABLE IF NOT EXISTS bench_pg_roundtrip_is (
		id BIGSERIAL PRIMARY KEY,
		name TEXT NOT NULL,
		value JSONB,
		created_at TIMESTAMPTZ DEFAULT now()
	)`)
	if err != nil {
		b.Fatalf("create bench table: %v", err)
	}
	b.Cleanup(func() {
		adminPool.Exec(context.Background(), "DROP TABLE IF EXISTS bench_pg_roundtrip_is")
	})

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var id int64
		err := adminPool.QueryRow(ctx,
			`INSERT INTO bench_pg_roundtrip_is (name, value) VALUES ($1, $2) RETURNING id`,
			fmt.Sprintf("bench-%d", i),
			fmt.Sprintf(`{"seq":%d}`, i),
		).Scan(&id)
		if err != nil {
			b.Fatalf("INSERT RETURNING: %v", err)
		}

		var name string
		err = adminPool.QueryRow(ctx,
			`SELECT name FROM bench_pg_roundtrip_is WHERE id = $1`, id,
		).Scan(&name)
		if err != nil {
			b.Fatalf("SELECT: %v", err)
		}
		pgRoundTripBenchmarkStrSink = name
		pgRoundTripBenchmarkIntSink = id
	}
}

# Tier 3 Infrastructure Benchmarks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the Moca benchmarking suite with Tier 3 infrastructure benchmarks, performance budget tests, concurrency scaling, enhanced CI PR comments, wiki documentation, and cleanup.

**Architecture:** Nine new test/benchmark files following existing conventions (build tags, `b.ReportAllocs()`, sink variables), a bash formatting script for CI, Makefile/workflow edits for `BENCH_PKGS`, a wiki guide, and removal of the fulfilled design doc.

**Tech Stack:** Go 1.26 testing/benchmarking, pgxpool v5, go-redis v9, benchstat, bash, GitHub Actions, GitHub Wiki (submodule)

---

## File Map

| # | File | Responsibility |
|---|------|---------------|
| 1 | `pkg/meta/ddl_bench_test.go` | Tier 3: DDL generation scaling benchmark |
| 2 | `pkg/meta/compiler_bench_test.go` | Tier 3: JSON-to-MetaType compilation benchmark |
| 3 | `pkg/meta/budget_test.go` | Budget: hard ns/op assertions for Registry.Get L1 + DDL |
| 4 | `pkg/hooks/budget_test.go` | Budget: hard ns/op assertion for HookRegistry.Resolve |
| 5 | `pkg/api/budget_test.go` | Budget: hard ns/op assertion for TransformerChain |
| 6 | `internal/drivers/redis_integration_bench_test.go` | Tier 3: raw Redis GET/SET/pipeline latency |
| 7 | `pkg/orm/pg_roundtrip_integration_bench_test.go` | Tier 3: raw PostgreSQL INSERT/SELECT latency |
| 8 | `pkg/orm/pool_saturation_integration_bench_test.go` | Tier 3: pool saturation at 1-500 goroutines |
| 9 | `pkg/document/crud_concurrency_integration_bench_test.go` | Cross-cutting: DocManager concurrency scaling |
| 10 | `Makefile` | Edit: add `./internal/drivers` to BENCH_PKGS |
| 11 | `.github/scripts/format-bench-comment.sh` | New: parse benchstat into structured PR comment |
| 12 | `.github/workflows/benchmark.yml` | Edit: BENCH_PKGS + use format script for PR comment |
| 13 | `wiki/Performance-Benchmarking-Guide.md` | New: user-facing benchmarking wiki page |
| 14 | `wiki/_Sidebar.md` | Edit: add Performance section |
| 15 | `docs/BENCHMARKING.md` | Delete: fulfilled design spec |

---

## Task 1: DDL Generation Benchmark

**Files:**
- Create: `pkg/meta/ddl_bench_test.go`

- [ ] **Step 1: Write the benchmark file**

```go
package meta

import (
	"fmt"
	"testing"
)

var ddlBenchmarkSink []DDLStatement

func BenchmarkGenerateTableDDL(b *testing.B) {
	for _, fieldCount := range []int{10, 50, 100} {
		b.Run(fmt.Sprintf("Fields_%d", fieldCount), func(b *testing.B) {
			mt := benchmarkDDLMetaType(fieldCount)

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				stmts := GenerateTableDDL(mt)
				if len(stmts) == 0 {
					b.Fatal("GenerateTableDDL returned no statements")
				}
				ddlBenchmarkSink = stmts
			}
		})
	}
}

func benchmarkDDLMetaType(fieldCount int) *MetaType {
	fields := make([]FieldDef, 0, fieldCount)
	for i := 0; i < fieldCount; i++ {
		name := fmt.Sprintf("field_%03d", i)
		switch i % 6 {
		case 0:
			fields = append(fields, FieldDef{Name: name, Label: "Data", FieldType: FieldTypeData})
		case 1:
			fields = append(fields, FieldDef{Name: name, Label: "Int", FieldType: FieldTypeInt, DBIndex: true})
		case 2:
			fields = append(fields, FieldDef{Name: name, Label: "Currency", FieldType: FieldTypeCurrency})
		case 3:
			fields = append(fields, FieldDef{Name: name, Label: "Date", FieldType: FieldTypeDate})
		case 4:
			fields = append(fields, FieldDef{Name: name, Label: "Select", FieldType: FieldTypeSelect, Options: "A\nB\nC"})
		default:
			fields = append(fields, FieldDef{Name: name, Label: "JSON", FieldType: FieldTypeJSON})
		}
	}

	return &MetaType{
		Name:   fmt.Sprintf("BenchDDL_%d", fieldCount),
		Module: "bench",
		NamingRule: NamingStrategy{
			Rule: NamingAutoIncrement,
		},
		Fields: fields,
	}
}
```

- [ ] **Step 2: Run to verify it works**

Run: `go test -run=^$ -bench=BenchmarkGenerateTableDDL -benchmem -count=1 ./pkg/meta/...`

Expected: three sub-benchmarks pass with increasing ns/op. Output shows `Fields_10`, `Fields_50`, `Fields_100`.

- [ ] **Step 3: Commit**

```bash
git add pkg/meta/ddl_bench_test.go
git commit -m "bench: add Tier 3 DDL generation scaling benchmark"
```

---

## Task 2: Schema Compiler Benchmark

**Files:**
- Create: `pkg/meta/compiler_bench_test.go`

- [ ] **Step 1: Write the benchmark file**

```go
package meta_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

var compilerBenchmarkSink *meta.MetaType

func BenchmarkCompile(b *testing.B) {
	for _, tc := range []struct {
		name       string
		fieldCount int
	}{
		{"Simple", 5},
		{"Complex", 50},
		{"Large", 100},
	} {
		b.Run(tc.name, func(b *testing.B) {
			jsonBytes := benchmarkCompilerJSON(tc.name, tc.fieldCount)
			b.SetBytes(int64(len(jsonBytes)))

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				mt, err := meta.Compile(jsonBytes)
				if err != nil {
					b.Fatalf("Compile(%s): %v", tc.name, err)
				}
				compilerBenchmarkSink = mt
			}
		})
	}
}

func benchmarkCompilerJSON(scenario string, fieldCount int) []byte {
	fields := make([]map[string]any, 0, fieldCount)
	for i := 0; i < fieldCount; i++ {
		f := map[string]any{
			"name":       fmt.Sprintf("field_%03d", i),
			"label":      fmt.Sprintf("Field %d", i),
			"field_type": "Data",
		}
		switch i % 8 {
		case 0:
			f["field_type"] = "Data"
			f["required"] = true
			f["max_length"] = 140
		case 1:
			f["field_type"] = "Int"
			f["required"] = true
		case 2:
			f["field_type"] = "Currency"
		case 3:
			f["field_type"] = "Date"
			f["required"] = true
		case 4:
			f["field_type"] = "Select"
			f["options"] = "Draft\nSubmitted\nCancelled"
		case 5:
			f["field_type"] = "Link"
			f["options"] = "Customer"
			f["db_index"] = true
		case 6:
			f["field_type"] = "LongText"
		default:
			f["field_type"] = "JSON"
		}
		fields = append(fields, f)
	}

	doc := map[string]any{
		"name":         fmt.Sprintf("BenchCompile_%s", scenario),
		"module":       "bench",
		"label":        fmt.Sprintf("Bench Compile %s", scenario),
		"naming_rule":  map[string]any{"rule": "uuid"},
		"title_field":  "field_000",
		"sort_field":   "creation",
		"sort_order":   "desc",
		"fields":       fields,
	}

	data, err := json.Marshal(doc)
	if err != nil {
		panic(fmt.Sprintf("benchmarkCompilerJSON: %v", err))
	}
	return data
}
```

- [ ] **Step 2: Run to verify it works**

Run: `go test -run=^$ -bench=BenchmarkCompile -benchmem -count=1 ./pkg/meta/...`

Expected: three sub-benchmarks pass (`Simple`, `Complex`, `Large`) with increasing ns/op.

- [ ] **Step 3: Commit**

```bash
git add pkg/meta/compiler_bench_test.go
git commit -m "bench: add Tier 3 schema compiler benchmark"
```

---

## Task 3: Performance Budget Tests

**Files:**
- Create: `pkg/meta/budget_test.go`
- Create: `pkg/hooks/budget_test.go`
- Create: `pkg/api/budget_test.go`

- [ ] **Step 1: Write `pkg/meta/budget_test.go`**

```go
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
```

- [ ] **Step 2: Write `pkg/hooks/budget_test.go`**

```go
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
```

- [ ] **Step 3: Write `pkg/api/budget_test.go`**

```go
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
```

- [ ] **Step 4: Run budget tests**

Run: `go test -run=Test.*Budget -v ./pkg/meta/... ./pkg/hooks/... ./pkg/api/...`

Expected: all 4 budget tests pass with log output showing ns/op and percentage used.

- [ ] **Step 5: Commit**

```bash
git add pkg/meta/budget_test.go pkg/hooks/budget_test.go pkg/api/budget_test.go
git commit -m "test: add performance budget assertions for 4 critical benchmarks"
```

---

## Task 4: Redis Driver Benchmark

**Files:**
- Create: `internal/drivers/redis_integration_bench_test.go`

- [ ] **Step 1: Write the benchmark file**

```go
//go:build integration

package drivers_test

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/osama1998H/moca/internal/drivers"
	"github.com/osama1998H/moca/pkg/observe"
)

var redisBenchmarkStringSink string

// newBenchClients creates a RedisClients for benchmarks.
// Separate from newTestClients (which accepts *testing.T) because
// *testing.B is a distinct type even though both satisfy testing.TB.
func newBenchClients(b *testing.B) *drivers.RedisClients {
	b.Helper()
	logger := observe.NewLogger(slog.LevelWarn)
	rc := drivers.NewRedisClients(testRedisConfig(), logger)
	b.Cleanup(func() { _ = rc.Close() })
	return rc
}

func BenchmarkRedisGetSet_SingleKey(b *testing.B) {
	rc := newBenchClients(b)
	ctx := context.Background()
	key := "bench:single"
	value := "benchmark-payload-64bytes-padded-to-simulate-real-world-usage!!"

	b.Cleanup(func() { rc.Cache.Del(ctx, key) })

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := rc.Cache.Set(ctx, key, value, 10*time.Second).Err(); err != nil {
			b.Fatalf("SET: %v", err)
		}
		got, err := rc.Cache.Get(ctx, key).Result()
		if err != nil {
			b.Fatalf("GET: %v", err)
		}
		redisBenchmarkStringSink = got
	}
}

func BenchmarkRedisGetSet_Pipeline(b *testing.B) {
	rc := newBenchClients(b)
	ctx := context.Background()

	keys := make([]string, 10)
	for i := range keys {
		keys[i] = fmt.Sprintf("bench:pipe:%d", i)
	}
	b.Cleanup(func() {
		for _, k := range keys {
			rc.Cache.Del(ctx, k)
		}
	})

	value := "benchmark-payload-64bytes-padded-to-simulate-real-world-usage!!"

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pipe := rc.Cache.Pipeline()
		for _, k := range keys {
			pipe.Set(ctx, k, value, 10*time.Second)
		}
		for _, k := range keys {
			pipe.Get(ctx, k)
		}
		cmds, err := pipe.Exec(ctx)
		if err != nil {
			b.Fatalf("Pipeline exec: %v", err)
		}
		_ = cmds
	}
}

func BenchmarkRedisGetSet_Parallel(b *testing.B) {
	rc := newBenchClients(b)
	ctx := context.Background()
	value := "benchmark-payload-64bytes-padded-to-simulate-real-world-usage!!"
	var seq atomic.Uint64

	b.Cleanup(func() {
		// Best-effort cleanup of bench keys
		iter := rc.Cache.Scan(ctx, 0, "bench:par:*", 1000).Iterator()
		for iter.Next(ctx) {
			rc.Cache.Del(ctx, iter.Val())
		}
	})

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			n := seq.Add(1)
			key := fmt.Sprintf("bench:par:%d", n)
			if err := rc.Cache.Set(ctx, key, value, 10*time.Second).Err(); err != nil {
				b.Fatalf("SET: %v", err)
			}
			got, err := rc.Cache.Get(ctx, key).Result()
			if err != nil {
				b.Fatalf("GET: %v", err)
			}
			redisBenchmarkStringSink = got
		}
	})
}
```

Note: This file shares the `//go:build integration` tag and `drivers_test` package with `redis_test.go`. The `TestMain` in `redis_test.go` handles Redis connectivity probing for all test/bench files in this package. The `testRedisConfig()` helper is also reused from `redis_test.go`. A separate `newBenchClients` is needed because `newTestClients` accepts `*testing.T` and `*testing.B` is a distinct type.

- [ ] **Step 2: Run to verify it works**

Run: `docker compose up -d --wait && go test -run=^$ -tags=integration -bench=BenchmarkRedisGetSet -benchmem -count=1 ./internal/drivers/...`

Expected: three benchmarks pass (`SingleKey`, `Pipeline`, `Parallel`).

- [ ] **Step 3: Commit**

```bash
git add internal/drivers/redis_integration_bench_test.go
git commit -m "bench: add Tier 3 Redis driver latency benchmarks"
```

---

## Task 5: PostgreSQL Round-Trip Benchmark

**Files:**
- Create: `pkg/orm/pg_roundtrip_integration_bench_test.go`

- [ ] **Step 1: Write the benchmark file**

```go
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

	// Setup: create temp table
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

	// Setup: create table and seed one row
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
```

Note: This file reuses `adminPool` from the existing `postgres_test.go` TestMain in the same `orm_test` package. Both files share the `//go:build integration` tag.

- [ ] **Step 2: Run to verify it works**

Run: `go test -run=^$ -tags=integration -bench=BenchmarkPGRoundTrip -benchmem -count=1 ./pkg/orm/...`

Expected: three benchmarks pass (`Insert`, `Select`, `InsertSelect`).

- [ ] **Step 3: Commit**

```bash
git add pkg/orm/pg_roundtrip_integration_bench_test.go
git commit -m "bench: add Tier 3 PostgreSQL round-trip latency benchmarks"
```

---

## Task 6: Pool Saturation Benchmark

**Files:**
- Create: `pkg/orm/pool_saturation_integration_bench_test.go`

- [ ] **Step 1: Write the benchmark file**

```go
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

	// Warm the pool with a single query.
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
```

- [ ] **Step 2: Run to verify it works**

Run: `go test -run=^$ -tags=integration -bench=BenchmarkPoolSaturation -benchmem -count=1 ./pkg/orm/...`

Expected: five sub-benchmarks pass (`Goroutines_1` through `Goroutines_500`). Higher goroutine counts show higher ns/op.

- [ ] **Step 3: Commit**

```bash
git add pkg/orm/pool_saturation_integration_bench_test.go
git commit -m "bench: add Tier 3 pool saturation scaling benchmark"
```

---

## Task 7: Document Manager Concurrency Scaling Benchmark

**Files:**
- Create: `pkg/document/crud_concurrency_integration_bench_test.go`

- [ ] **Step 1: Write the benchmark file**

```go
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
```

- [ ] **Step 2: Run to verify it works**

Run: `go test -run=^$ -tags=integration -bench=BenchmarkDocManagerInsert_Concurrency -benchmem -count=1 -timeout=10m ./pkg/document/...`

Expected: five sub-benchmarks pass. Higher goroutine counts show higher ns/op.

- [ ] **Step 3: Commit**

```bash
git add pkg/document/crud_concurrency_integration_bench_test.go
git commit -m "bench: add DocManager concurrency scaling benchmark (1-500 goroutines)"
```

---

## Task 8: Update Makefile and Workflow BENCH_PKGS

**Files:**
- Modify: `Makefile:12`
- Modify: `.github/workflows/benchmark.yml:58`

- [ ] **Step 1: Edit Makefile**

Change line 12 from:
```
BENCH_PKGS := ./pkg/meta ./pkg/document ./pkg/orm ./pkg/api ./pkg/hooks
```
to:
```
BENCH_PKGS := ./pkg/meta ./pkg/document ./pkg/orm ./pkg/api ./pkg/hooks ./internal/drivers
```

- [ ] **Step 2: Edit `.github/workflows/benchmark.yml`**

Change line 58 from:
```yaml
      BENCH_PKGS: ./pkg/meta ./pkg/document ./pkg/orm ./pkg/api ./pkg/hooks
```
to:
```yaml
      BENCH_PKGS: ./pkg/meta ./pkg/document ./pkg/orm ./pkg/api ./pkg/hooks ./internal/drivers
```

- [ ] **Step 3: Run `make bench` to verify Makefile change**

Run: `make bench`

Expected: benchmarks from all 6 packages run (including `internal/drivers` — which will have 0 non-integration benchmarks, so it outputs nothing for that package but doesn't error).

- [ ] **Step 4: Commit**

```bash
git add Makefile .github/workflows/benchmark.yml
git commit -m "ci: add internal/drivers to BENCH_PKGS for Redis benchmarks"
```

---

## Task 9: Enhanced PR Comment Script

**Files:**
- Create: `.github/scripts/format-bench-comment.sh`

- [ ] **Step 1: Create the script directory**

Run: `mkdir -p .github/scripts`

- [ ] **Step 2: Write the formatting script**

```bash
#!/usr/bin/env bash
# format-bench-comment.sh — Parse benchstat output into a structured PR comment.
#
# Usage: ./format-bench-comment.sh <benchstat-output> <pr-bench-raw>
#   $1 = path to benchstat comparison output
#   $2 = path to PR branch raw benchmark output (for budget proximity)
#
# Outputs: Markdown to stdout

set -euo pipefail

BENCHSTAT_FILE="${1:?Usage: format-bench-comment.sh <benchstat-output> <pr-bench-raw>}"
PR_RAW_FILE="${2:?Usage: format-bench-comment.sh <benchstat-output> <pr-bench-raw>}"

# ── Tier mapping ──────────────────────────────────────────────────────────────
tier_for() {
  local name="$1"
  case "$name" in
    RegistryGet*|DocManager{Get,GetList,Insert}_*|DocManagerGet*|DocManagerGetList*|DocManagerInsert_Simple*|DocManagerInsert_Complex*|DocManagerInsert_Parallel*|QueryBuilderBuild*|GatewayHandler*)
      echo "1";;
    ValidateDoc*|NamingEngine*|DispatchEvent*|HookRegistry*|RateLimiter*|TransformerChain*|WithTransaction*)
      echo "2";;
    PGRoundTrip*|RedisGetSet*|PoolSaturation*|GenerateTableDDL*|Compile*|DocManagerInsert_Concurrency*)
      echo "3";;
    *)
      echo "0";;
  esac
}

tier_label() {
  case "$1" in
    1) echo "Tier 1 -- Critical Hot Path";;
    2) echo "Tier 2 -- Per-Request Components";;
    3) echo "Tier 3 -- Infrastructure";;
    *) echo "Other";;
  esac
}

status_icon() {
  local delta="$1"
  # Remove leading +/- and % for comparison
  local num
  num=$(echo "$delta" | sed 's/[+%]//g; s/^-//')
  local sign
  sign=$(echo "$delta" | head -c1)

  if [ "$sign" = "-" ]; then
    echo ":green_circle:"
  elif awk "BEGIN{exit(!($num >= 10))}"; then
    echo ":red_circle:"
  elif awk "BEGIN{exit(!($num >= 5))}"; then
    echo ":yellow_circle:"
  else
    echo ":white_circle:"
  fi
}

# ── Budget definitions (ns/op) ────────────────────────────────────────────────
declare -A BUDGETS=(
  ["RegistryGet_L1Hit"]=200
  ["GenerateTableDDL/Fields_10"]=50000
  ["TransformerChain_Response"]=20000
  ["HookRegistryResolve_10Hooks"]=5000
)

# ── Parse benchstat output ────────────────────────────────────────────────────
# Lines look like:
#   RegistryGet_L1Hit-8    45.2ns ± 2%    44.8ns ± 1%   -0.88%  (p=0.421 n=10+10)
#   DocManagerInsert-8     1.23ms ± 3%    1.58ms ± 2%  +28.46%  (p=0.000 n=10+10)
# Or with ~ for no significant change

declare -A TIER1_ROWS TIER2_ROWS TIER3_ROWS OTHER_ROWS
declare -A ALLOC_ROWS
has_changes=false

while IFS= read -r line; do
  # Skip header and empty lines
  [[ "$line" =~ ^name ]] && continue
  [[ -z "$line" ]] && continue
  # Skip lines with ~ (no significant change)
  [[ "$line" =~ "~" ]] && continue
  # Skip alloc lines (handled separately)
  [[ "$line" =~ "B/op" ]] && continue
  [[ "$line" =~ "allocs/op" ]] && continue

  # Extract benchmark name (strip -N suffix)
  bench_name=$(echo "$line" | awk '{print $1}' | sed 's/-[0-9]*$//')

  # Extract old, new, delta
  old_val=$(echo "$line" | awk '{print $2 " " $3}' | sed 's/±.*//')
  new_val=$(echo "$line" | awk '{print $4 " " $5}' | sed 's/±.*//')
  delta=$(echo "$line" | grep -oE '[+-][0-9]+\.[0-9]+%' | head -1)

  [ -z "$delta" ] && continue
  has_changes=true

  icon=$(status_icon "$delta")
  tier=$(tier_for "$bench_name")
  row="| ${icon} | ${bench_name} | ${old_val} | ${new_val} | ${delta} |"

  case "$tier" in
    1) TIER1_ROWS["$bench_name"]="$row";;
    2) TIER2_ROWS["$bench_name"]="$row";;
    3) TIER3_ROWS["$bench_name"]="$row";;
    *) OTHER_ROWS["$bench_name"]="$row";;
  esac
done < "$BENCHSTAT_FILE"

# ── Parse allocation changes from benchstat ───────────────────────────────────
in_alloc_section=false
while IFS= read -r line; do
  if [[ "$line" =~ "B/op" ]] && [[ "$line" =~ "name" ]]; then
    in_alloc_section=true
    continue
  fi
  if [[ "$line" =~ "allocs/op" ]] && [[ "$line" =~ "name" ]]; then
    in_alloc_section=true
    continue
  fi
  [ "$in_alloc_section" != true ] && continue
  [[ -z "$line" ]] && { in_alloc_section=false; continue; }
  [[ "$line" =~ "~" ]] && continue

  bench_name=$(echo "$line" | awk '{print $1}' | sed 's/-[0-9]*$//')
  old_val=$(echo "$line" | awk '{print $2}')
  new_val=$(echo "$line" | awk '{print $4}')
  delta=$(echo "$line" | grep -oE '[+-][0-9]+\.[0-9]+%' | head -1)

  [ -z "$delta" ] && continue
  ALLOC_ROWS["$bench_name"]="| ${bench_name} | ${old_val} | ${new_val} | ${delta} |"
done < "$BENCHSTAT_FILE"

# ── Parse PR raw output for budget proximity ──────────────────────────────────
declare -A BUDGET_CURRENT
while IFS= read -r line; do
  [[ "$line" =~ ^Benchmark ]] || continue
  for budget_name in "${!BUDGETS[@]}"; do
    if [[ "$line" =~ "$budget_name" ]]; then
      # Extract ns/op value
      ns=$(echo "$line" | grep -oE '[0-9]+(\.[0-9]+)? ns/op' | head -1 | awk '{print $1}')
      [ -n "$ns" ] && BUDGET_CURRENT["$budget_name"]="$ns"
    fi
  done
done < "$PR_RAW_FILE"

# ── Output markdown ───────────────────────────────────────────────────────────
echo "## Benchmark Results"
echo ""

if [ "$has_changes" != true ]; then
  echo "> All benchmarks within noise margin. No statistically significant changes detected."
  echo ""
else
  print_tier_table() {
    local -n rows=$1
    local tier_num=$2
    [ ${#rows[@]} -eq 0 ] && return
    echo "### $(tier_label "$tier_num")"
    echo ""
    echo "| Status | Benchmark | Base | PR | Delta |"
    echo "|--------|-----------|------|----|-------|"
    for key in "${!rows[@]}"; do
      echo "${rows[$key]}"
    done
    echo ""
  }

  print_tier_table TIER1_ROWS 1
  print_tier_table TIER2_ROWS 2
  print_tier_table TIER3_ROWS 3
  if [ ${#OTHER_ROWS[@]} -gt 0 ]; then
    print_tier_table OTHER_ROWS 0
  fi
fi

# Allocation changes
if [ ${#ALLOC_ROWS[@]} -gt 0 ]; then
  echo "### Allocation Changes"
  echo ""
  echo "| Benchmark | Base | PR | Delta |"
  echo "|-----------|------|----|-------|"
  for key in "${!ALLOC_ROWS[@]}"; do
    echo "${ALLOC_ROWS[$key]}"
  done
  echo ""
fi

# Budget proximity
budget_found=false
for budget_name in "${!BUDGETS[@]}"; do
  if [ -n "${BUDGET_CURRENT[$budget_name]:-}" ]; then
    budget_found=true
    break
  fi
done

if [ "$budget_found" = true ]; then
  echo "### Performance Budgets"
  echo ""
  echo "| Benchmark | Current | Budget | Used |"
  echo "|-----------|---------|--------|------|"
  for budget_name in "${!BUDGETS[@]}"; do
    current="${BUDGET_CURRENT[$budget_name]:-}"
    [ -z "$current" ] && continue
    budget_ns="${BUDGETS[$budget_name]}"
    if [ "$budget_ns" -ge 1000 ]; then
      budget_display="$(echo "scale=1; $budget_ns / 1000" | bc) us/op"
    else
      budget_display="${budget_ns} ns/op"
    fi
    if awk "BEGIN{exit(!(${current} >= 1000))}"; then
      current_display="$(echo "scale=1; ${current} / 1000" | bc) us/op"
    else
      current_display="${current} ns/op"
    fi
    pct=$(awk "BEGIN{printf \"%.0f\", (${current} / ${budget_ns}) * 100}")
    echo "| ${budget_name} | ${current_display} | ${budget_display} | ${pct}% |"
  done
  echo ""
fi

# Raw output
echo "<details>"
echo "<summary>Full benchstat output</summary>"
echo ""
echo '```'
cat "$BENCHSTAT_FILE"
echo '```'
echo ""
echo "</details>"
```

- [ ] **Step 3: Make the script executable**

Run: `chmod +x .github/scripts/format-bench-comment.sh`

- [ ] **Step 4: Commit**

```bash
git add .github/scripts/format-bench-comment.sh
git commit -m "ci: add benchmark PR comment formatting script"
```

---

## Task 10: Update Workflow to Use Format Script

**Files:**
- Modify: `.github/workflows/benchmark.yml`

- [ ] **Step 1: Update the Compare and Comment steps**

Replace the existing "Compare benchmarks" and "Comment on PR" steps in `.github/workflows/benchmark.yml`. The new Compare step runs benchstat then calls the format script. The Comment step uses the formatted output.

Find the step `- name: Compare benchmarks` (starting around line 95) and replace everything from that step through the end of the `- name: Comment on PR` step with:

```yaml
      - name: Compare benchmarks
        id: compare
        run: |
          if ! grep -q '^Benchmark' /tmp/bench-pr.txt; then
            echo "status=missing-pr-data" >> "$GITHUB_OUTPUT"
            echo "regression=false" >> "$GITHUB_OUTPUT"
            {
              echo "comment<<EOF"
              echo "## Benchmark Results"
              echo ""
              echo "> No benchmark output was produced on the PR branch."
              echo "EOF"
            } >> "$GITHUB_OUTPUT"
          elif ! grep -q '^Benchmark' /tmp/bench-base.txt; then
            echo "status=missing-base-data" >> "$GITHUB_OUTPUT"
            echo "regression=false" >> "$GITHUB_OUTPUT"
            {
              echo "comment<<EOF"
              echo "## Benchmark Results"
              echo ""
              echo "> Baseline unavailable on the base branch, so regression gating was skipped for this PR."
              echo "EOF"
            } >> "$GITHUB_OUTPUT"
          else
            benchstat /tmp/bench-base.txt /tmp/bench-pr.txt > /tmp/benchstat-output.txt
            cat /tmp/benchstat-output.txt

            FORMATTED=$(bash .github/scripts/format-bench-comment.sh /tmp/benchstat-output.txt /tmp/bench-pr.txt)
            {
              echo "comment<<EOF"
              echo "$FORMATTED"
              echo "EOF"
            } >> "$GITHUB_OUTPUT"

            echo "status=compared" >> "$GITHUB_OUTPUT"

            if printf '%s\n' "$(cat /tmp/benchstat-output.txt)" | awk '
              /^name[[:space:]]/ { next }
              /^[[:space:]]*$/ { next }
              /p=/ {
                if ($0 ~ /~/) {
                  next
                }
                if (match($0, /\+[0-9]+(\.[0-9]+)?%/)) {
                  delta = substr($0, RSTART + 1, RLENGTH - 2) + 0
                  if (delta >= 10) {
                    found = 1
                  }
                }
              }
              END { exit(found ? 0 : 1) }
            '; then
              echo "regression=true" >> "$GITHUB_OUTPUT"
            else
              echo "regression=false" >> "$GITHUB_OUTPUT"
            fi
          fi

      - name: Comment on PR
        if: github.event.pull_request.head.repo.full_name == github.repository
        uses: marocchino/sticky-pull-request-comment@v2
        with:
          header: benchmark
          message: |
            ${{ steps.compare.outputs.comment }}
```

Keep the existing "Fail on regression" step unchanged.

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/benchmark.yml
git commit -m "ci: use structured format script for benchmark PR comments"
```

---

## Task 11: Wiki Benchmarking Guide

**Files:**
- Create: `wiki/Performance-Benchmarking-Guide.md`

- [ ] **Step 1: Write the wiki page**

```markdown
# Benchmarking Guide

Moca has a three-tier benchmarking suite that measures individual functions, composed request pipelines, and underlying infrastructure. CI automatically detects performance regressions on every pull request.

---

## The Three Tiers

### Tier 1 -- Critical Hot Path

Benchmarks for functions that execute on **every single API request**. A regression here affects every user of every tenant.

| What's Tested | Why It Matters |
|--------------|----------------|
| MetaType registry lookup (sync.Map, Redis, PostgreSQL fallback) | Called on every document operation. L1 cache hit should be near-instant. |
| Document CRUD (Get, GetList, Insert) | The core read and write operations behind every API endpoint. |
| SQL query builder | Generates parameterized queries for every list view and document fetch. |
| HTTP middleware chain | RequestID, CORS, tenant resolution, auth, rate limiting -- every request traverses this. |

### Tier 2 -- Per-Request Components

Benchmarks for functions called **within** the hot path but not necessarily on every request.

| What's Tested | Why It Matters |
|--------------|----------------|
| Field validation | Type coercion and validation rules per field. Cost scales with field count. |
| Document naming | Pattern-based naming uses PostgreSQL sequences. Contention risk under concurrency. |
| Lifecycle event dispatch | Switch dispatch across 14 lifecycle events during document writes. |
| Hook resolution | Topological sort of hooks by dependency and priority. Cost grows with installed apps. |
| Rate limiting | Redis sliding-window commands. Network-bound. |
| Request/response transformation | Field filtering and aliasing. Cost scales with field and transformer count. |
| Database transactions | Begin, execute, commit/rollback overhead measurement. |

### Tier 3 -- Infrastructure

Benchmarks that measure the **underlying systems in isolation**. These establish baselines so that when a Tier 1 or Tier 2 benchmark regresses, you can determine whether the cause is in the application code or the infrastructure layer.

| What's Tested | Why It Matters |
|--------------|----------------|
| PostgreSQL round-trip (INSERT, SELECT) | Raw database latency baseline. Helps isolate DB vs app-layer regressions. |
| Redis GET/SET (single, pipeline, parallel) | Raw cache driver latency. Helps isolate Redis vs business-logic regressions. |
| Connection pool under load (1-500 goroutines) | Detects pool exhaustion and lock contention. Key for tuning `max_conns`. |
| DDL generation (10, 50, 100 fields) | Runs during migrations. Super-linear scaling indicates algorithmic problems. |
| Schema compilation (5, 50, 100 fields) | Runs during app install and cache rebuild. Regression slows cold starts. |
| Document insert concurrency (1-500 goroutines) | Detects bottlenecks in the full write path (naming, validation, transaction, hooks). |

---

## How to Run Benchmarks

| Command | What It Does | Docker Required? |
|---------|-------------|-----------------|
| `make bench` | Run all benchmarks that don't need external services (5 iterations) | No |
| `make bench-integration` | Start Docker services, run all benchmarks including DB/Redis (10 iterations) | Yes |
| `make bench-compare` | Run benchmarks and compare against saved baseline using benchstat | No |
| `make bench-save-baseline` | Run benchmarks and save results as the comparison baseline | No |
| `make bench-profile` | Capture CPU and memory profiles for a specific benchmark | No |

---

## How to Read Results

A typical benchmark output line looks like:

```
BenchmarkRegistryGet_L1Hit-8    26492102    45.2 ns/op    0 B/op    0 allocs/op
```

| Column | Meaning |
|--------|---------|
| `BenchmarkRegistryGet_L1Hit-8` | Benchmark name. `-8` means it ran with `GOMAXPROCS=8`. |
| `26492102` | Number of iterations the benchmark ran. More iterations = more statistical confidence. |
| `45.2 ns/op` | **Nanoseconds per operation.** The primary performance metric. Lower is better. |
| `0 B/op` | **Bytes allocated per operation.** Tracks memory pressure. Lower is better. |
| `0 allocs/op` | **Heap allocations per operation.** Each allocation adds GC pressure. Zero is ideal for hot paths. |

### What "good" looks like

- **Tier 1 (L1 cache hits):** < 200 ns/op, 0 allocs/op
- **Tier 1 (database operations):** < 5 ms/op
- **Tier 2 (CPU-only):** < 50 us/op
- **Tier 3 (infrastructure):** Stable across runs. Used as a baseline, not judged in isolation.

### Reading benchstat comparison output

When comparing two runs, benchstat shows:

```
name                old time/op    new time/op    delta
RegistryGet_L1Hit   45.2ns +- 2%   44.8ns +- 1%    ~  (p=0.421 n=10+10)
DocManagerInsert    1.23ms +- 3%   1.58ms +- 2%  +28.46%  (p=0.000 n=10+10)
```

- `~` means no statistically significant change (good).
- `+28.46%` means a 28% regression (investigate).
- `p=0.000` means the change is statistically significant (not noise).
- `n=10+10` means 10 samples from each run were compared.

---

## CI Regression Detection

Every pull request that changes code in `pkg/`, `internal/`, or `cmd/` triggers the benchmark workflow:

1. **Base branch benchmarks** run in a git worktree at the PR's base commit (10 iterations)
2. **PR branch benchmarks** run in a separate worktree at the PR's head commit (10 iterations)
3. **benchstat** compares the two runs for statistically significant changes
4. A **structured PR comment** is posted with:
   - Summary table grouped by tier (only changed benchmarks shown)
   - Status icons: :red_circle: regression >= 10%, :yellow_circle: regression 5-10%, :green_circle: improvement
   - Allocation change table (if any B/op or allocs/op changed)
   - Performance budget proximity table (how close critical paths are to their hard limits)
   - Full raw benchstat output in a collapsed section
5. The check **fails** if any benchmark regresses by 10% or more

---

## Performance Budgets

Four critical benchmarks have hard performance limits enforced as normal tests (run by `make test`, no Docker needed):

| Benchmark | Budget | What It Guards |
|-----------|--------|---------------|
| `RegistryGet_L1Hit` | 200 ns/op | sync.Map cache lookup must stay near-instant |
| `GenerateTableDDL` (10 fields) | 50 us/op | DDL generation must not slow migrations |
| `TransformerChain_Response` | 20 us/op | Response transformation must not add API latency |
| `HookRegistryResolve_10Hooks` | 5 us/op | Hook resolution must stay fast as apps are installed |

If any budget is exceeded, `make test` fails. These catch absolute performance violations regardless of relative change -- even if a regression is small per-PR, repeated small regressions that cross a budget are caught.

---

## Profiling a Regression

When a benchmark regresses, use profiling to find the cause:

```bash
# Interactive: prompts for benchmark pattern and package
make bench-profile

# Or run directly:
go test -run=^$ -bench=BenchmarkDocManagerInsert -cpuprofile=cpu.prof -memprofile=mem.prof -benchmem ./pkg/document/...

# View CPU profile in browser
go tool pprof -http=:8080 cpu.prof

# View memory profile in browser
go tool pprof -http=:8080 mem.prof
```

In the flame graph, look for:
- **Wide bars** at the top = functions consuming the most time
- **Unexpected function calls** in hot paths (e.g., reflection, JSON marshaling where there shouldn't be any)
- **Allocation-heavy functions** in the memory profile = GC pressure sources

For concurrency issues, use the trace tool:

```bash
go test -run=^$ -bench=BenchmarkDocManagerInsert_Parallel -trace=trace.out ./pkg/document/...
go tool trace trace.out
```

Look for goroutine blocking, mutex contention, and scheduler delays in the trace viewer.
```

- [ ] **Step 2: Commit**

```bash
git -C wiki add Performance-Benchmarking-Guide.md
git -C wiki commit -m "docs: add Performance Benchmarking Guide"
```

---

## Task 12: Update Wiki Sidebar

**Files:**
- Modify: `wiki/_Sidebar.md`

- [ ] **Step 1: Add Performance section between Operations and Project**

Find the line `**Project**` and insert before it:

```markdown
**Performance**
- [Benchmarking Guide](Performance-Benchmarking-Guide)

```

The sidebar should read:

```
...
- [Security](Operations-Security)

**Performance**
- [Benchmarking Guide](Performance-Benchmarking-Guide)

**Project**
- [Roadmap](Roadmap-Overview)
...
```

- [ ] **Step 2: Commit**

```bash
git -C wiki add _Sidebar.md
git -C wiki commit -m "docs: add Performance section to sidebar"
```

---

## Task 13: Delete Design Doc and Final Commit

**Files:**
- Delete: `docs/BENCHMARKING.md`

- [ ] **Step 1: Delete the fulfilled design spec**

Run: `rm docs/BENCHMARKING.md`

- [ ] **Step 2: Commit**

```bash
git add -A docs/BENCHMARKING.md
git commit -m "chore: remove BENCHMARKING.md design spec (replaced by wiki guide)"
```

- [ ] **Step 3: Verify all benchmarks compile**

Run: `go test -run=^$ -bench=. -benchmem -count=1 -timeout=5m ./pkg/meta/... ./pkg/hooks/... ./pkg/api/...`

Expected: all pure-CPU benchmarks (DDL, Compiler, Transformer, Hooks, Registry L1, Validator, Lifecycle) pass. Budget tests pass.

- [ ] **Step 4: Verify integration benchmarks compile (if Docker is available)**

Run: `make bench-integration`

Expected: all benchmarks across all 6 packages run including the new Tier 3 and concurrency benchmarks.

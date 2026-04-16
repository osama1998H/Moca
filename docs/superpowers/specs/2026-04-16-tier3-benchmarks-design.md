# Tier 3 Infrastructure Benchmarks + Cross-cutting Additions

> Design spec for completing the Moca benchmarking suite: Tier 3 infrastructure benchmarks, performance budget tests, concurrency scaling, wiki documentation, and CI updates.

---

## Context

The `docs/BENCHMARKING.md` design spec defined three tiers of benchmarks. An audit of the codebase on 2026-04-16 confirmed:

- **Tier 1 (Critical Hot Path):** 12/12 benchmarks implemented across 5 packages
- **Tier 2 (Per-Request Components):** 13/13 benchmarks implemented across 5 packages
- **Tier 3 (Infrastructure):** 0/5 targets implemented
- **Cross-cutting (Budget tests):** 0 implemented
- **Cross-cutting (Concurrency scaling):** 0 implemented

All existing benchmarks follow spec conventions: `b.ReportAllocs()`, `b.ResetTimer()`, sink variables, sub-benchmarks for scaling. Makefile targets and GitHub Actions workflow are in place.

This spec covers the remaining work to fully close the benchmarking strategy.

---

## Scope

### In Scope

1. Five Tier 3 infrastructure benchmark files
2. Four performance budget test files (pure-CPU benchmarks only)
3. One concurrency scaling benchmark file
4. Makefile and GitHub Actions `BENCH_PKGS` update
5. New wiki page: `Performance-Benchmarking-Guide.md`
6. Wiki sidebar update with Performance section
7. Delete `docs/BENCHMARKING.md` (design spec fulfilled)

### Out of Scope

- Nightly benchmark workflow (future work)
- Third-party benchmark dashboards (Codspeed, BenchCI)
- Budget tests for integration-tagged benchmarks (require Docker, can't run in `make test`)

---

## Tier 3 Benchmark Files

### 1. `pkg/orm/pg_roundtrip_integration_bench_test.go`

**Build tag:** `//go:build integration`

**Benchmarks:**
- `BenchmarkPGRoundTrip_Insert` — Raw `pool.Exec(ctx, INSERT)` into a temp table. Measures PostgreSQL write latency through pgxpool.
- `BenchmarkPGRoundTrip_Select` — `pool.QueryRow(ctx, SELECT ... WHERE name=$1)` for single-row PK lookup. Measures read latency.
- `BenchmarkPGRoundTrip_InsertSelect` — Combined INSERT then immediate SELECT. Measures full write-read round-trip.

**Setup:** Create a temporary table (`bench_pg_roundtrip`) with columns `(id BIGSERIAL PRIMARY KEY, name TEXT, value JSONB, created_at TIMESTAMPTZ)` in `TestMain` or `b.Cleanup`. Use `DBManager.SystemPool()` for the connection.

**Why it matters:** Establishes the PostgreSQL baseline. When a Tier 1 `DocManager` benchmark regresses, comparing against this benchmark tells you whether the regression is in the database or in the application layer above it.

### 2. `internal/drivers/redis_integration_bench_test.go`

**Build tag:** `//go:build integration`

**Benchmarks:**
- `BenchmarkRedisGetSet_SingleKey` — `SET key value` then `GET key`. Measures single-command round-trip.
- `BenchmarkRedisGetSet_Pipeline` — Pipelined batch of 10 `SET`+`GET` pairs. Measures pipeline amortization.
- `BenchmarkRedisGetSet_Parallel` — `b.RunParallel` with concurrent `SET`/`GET` on unique keys. Measures driver concurrency handling.

**Setup:** Create a go-redis client pointing at `localhost:6380` (docker-compose Redis). Clean up keys in `b.Cleanup`.

**Why it matters:** Isolates raw Redis driver latency from Registry business logic. When `BenchmarkRegistryGet_L2Hit` regresses, cross-reference this benchmark to determine if it's a Redis problem or a deserialization/cache-logic problem.

### 3. `pkg/orm/pool_saturation_integration_bench_test.go`

**Build tag:** `//go:build integration`

**Benchmarks:**
- `BenchmarkPoolSaturation/Goroutines_{1,10,50,100,500}` — Each sub-benchmark calls `b.SetParallelism(n)` then `b.RunParallel`. Each goroutine acquires a connection via `pool.Exec(ctx, "SELECT 1")` and releases it.

**Setup:** Use `DBManager.SystemPool()` with default pool size. The benchmark intentionally exceeds pool capacity at higher goroutine counts.

**Diagnostic value:** If `Goroutines_500` ns/op is >10x `Goroutines_1`, there is pool exhaustion or lock contention. This is the primary benchmark for tuning `max_conns` in production.

### 4. `pkg/meta/ddl_bench_test.go`

**Build tag:** None (pure CPU, no Docker)

**Benchmarks:**
- `BenchmarkGenerateTableDDL/Fields_{10,50,100}` — Constructs a `MetaType` with N fields of mixed types (Data, Int, Currency, Date, Select, JSON — cycling through types), then benchmarks `GenerateTableDDL(mt)`.

**Why it matters:** DDL generation runs during migrations and schema sync. If it scales super-linearly with field count, complex MetaTypes will slow down deployment pipelines.

### 5. `pkg/meta/compiler_bench_test.go`

**Build tag:** None (pure CPU, no Docker)

**Benchmarks:**
- `BenchmarkCompile/Simple` — 5-field JSON MetaType definition with basic field types.
- `BenchmarkCompile/Complex` — 50-field definition including child tables, validation rules, select options.
- `BenchmarkCompile/Large` — 100-field definition.

**Why it matters:** `Compile` runs when MetaTypes are loaded from JSON (app installation, cache rebuild). Regression here slows app install and cold-start times.

---

## Cross-cutting Additions

### 6. Performance Budget Tests

Budget tests use `testing.Benchmark()` to run a benchmark function, then assert that `NsPerOp()` does not exceed a hard limit. They run as normal tests in `make test` (no Docker required).

**Only pure-CPU benchmarks get budget tests.** Integration-tagged benchmarks require Docker and would fail or be skipped in the standard test suite.

| File | Test | Budget | Benchmarks |
|------|------|--------|------------|
| `pkg/meta/budget_test.go` | `TestRegistryGet_L1Hit_Budget` | 200 ns/op | `BenchmarkRegistryGet_L1Hit` |
| `pkg/meta/budget_test.go` | `TestGenerateTableDDL_10Fields_Budget` | 50 µs/op | `BenchmarkGenerateTableDDL/Fields_10` |
| `pkg/api/budget_test.go` | `TestTransformerChain_Response_Budget` | 20 µs/op | `BenchmarkTransformerChain_Response` |
| `pkg/hooks/budget_test.go` | `TestHookRegistryResolve_10Hooks_Budget` | 5 µs/op | `BenchmarkHookRegistryResolve_10Hooks` |

**Pattern:**
```go
func TestRegistryGet_L1Hit_Budget(t *testing.T) {
    result := testing.Benchmark(BenchmarkRegistryGet_L1Hit)
    nsPerOp := result.NsPerOp()
    if nsPerOp > 200 {
        t.Errorf("Registry.Get L1 hit: %d ns/op, budget is 200 ns/op", nsPerOp)
    }
}
```

### 7. Concurrency Scaling — `pkg/document/crud_concurrency_integration_bench_test.go`

**Build tag:** `//go:build integration`

**Benchmark:**
- `BenchmarkDocManagerInsert_Concurrency/Goroutines_{1,10,50,100,500}`

**Pattern:**
```go
for _, goroutines := range []int{1, 10, 50, 100, 500} {
    b.Run(fmt.Sprintf("Goroutines_%d", goroutines), func(b *testing.B) {
        dm := setupDocManager(b)
        b.SetParallelism(goroutines)
        b.ResetTimer()
        b.ReportAllocs()
        b.RunParallel(func(pb *testing.PB) {
            i := 0
            for pb.Next() {
                _, _ = dm.Insert(testDocCtx(), "SalesOrder", testValues(i))
                i++
            }
        })
    })
}
```

**Diagnostic value:** Identifies lock contention and pool exhaustion in the full document write path (naming, validation, transaction, hooks). If throughput drops sharply at a concurrency level, the bottleneck is in that stage.

---

## Infrastructure Updates

### Makefile

Add `./internal/drivers` to `BENCH_PKGS`:

```makefile
BENCH_PKGS := ./pkg/meta ./pkg/document ./pkg/orm ./pkg/api ./pkg/hooks ./internal/drivers
```

### GitHub Actions `benchmark.yml`

Same change to the `env` block:

```yaml
env:
  BENCH_PKGS: ./pkg/meta ./pkg/document ./pkg/orm ./pkg/api ./pkg/hooks ./internal/drivers
```

### Enhanced PR Comment

The current PR comment dumps raw benchstat output behind a collapsed `<details>` tag with a one-line pass/fail verdict. This requires reviewers to manually expand and parse dense terminal output. The workflow will be updated with a post-processing script that parses benchstat output into a structured, actionable comment.

**New comment structure:**

#### 1. Top-level summary table (always visible)

Only benchmarks with statistically significant changes appear here. Each row shows: status icon, benchmark name, old value, new value, delta. Icons: a red circle for regression >= 10%, a yellow circle for regression 5-10%, a green circle for improvement, a dash for no significant change.

Example:

```
| Status | Benchmark | Base | PR | Delta |
|--------|-----------|------|----|-------|
| :red_circle: | DocManagerInsert_SimpleDoc | 1.23 ms/op | 1.58 ms/op | +28.5% |
| :yellow_circle: | QueryBuilderBuild_ComplexJoins | 8.1 µs/op | 8.9 µs/op | +9.8% |
| :green_circle: | RegistryGet_L1Hit | 48 ns/op | 41 ns/op | -14.6% |
```

Benchmarks with no significant change (`~` in benchstat) are omitted from this table. If all benchmarks are unchanged, the table is replaced with: "All benchmarks within noise margin."

#### 2. Tier grouping

The summary table is grouped by tier with headers:

```
### Tier 1 — Critical Hot Path
| ... |

### Tier 2 — Per-Request Components
| ... |

### Tier 3 — Infrastructure
| ... |
```

Empty tiers are omitted.

#### 3. Allocation changes

A second table shows allocation deltas for benchmarks where `B/op` or `allocs/op` changed significantly:

```
| Benchmark | Base B/op | PR B/op | Base allocs | PR allocs |
|-----------|-----------|---------|-------------|-----------|
| DocManagerInsert_SimpleDoc | 4096 | 8192 | 24 | 48 |
```

#### 4. Performance budget proximity

For the 4 budgeted benchmarks, show current value vs budget:

```
### Performance Budgets
| Benchmark | Current | Budget | Used |
|-----------|---------|--------|------|
| RegistryGet_L1Hit | 41 ns/op | 200 ns/op | 20% |
| GenerateTableDDL_10Fields | 12 µs/op | 50 µs/op | 24% |
| TransformerChain_Response | 8 µs/op | 20 µs/op | 40% |
| HookRegistryResolve_10Hooks | 1.8 µs/op | 5 µs/op | 36% |
```

#### 5. Raw output preserved

Full benchstat output stays in a collapsed `<details>` block at the bottom for anyone who needs the raw data.

**Implementation:** A shell script (`.github/scripts/format-bench-comment.sh`) that:
1. Receives benchstat output and PR benchmark raw output as inputs
2. Parses benchmark names into tiers using a name-to-tier mapping
3. Extracts ns/op, B/op, allocs/op values and delta percentages
4. Formats the structured markdown comment
5. Outputs the formatted comment for the workflow step

The tier mapping is a simple associative array in the script:
- Names starting with `RegistryGet`, `DocManager{Get,GetList,Insert}`, `QueryBuilderBuild`, `GatewayHandler` -> Tier 1
- Names starting with `ValidateDoc`, `NamingEngine`, `DispatchEvent`, `HookRegistry`, `RateLimiter`, `TransformerChain`, `WithTransaction` -> Tier 2
- Names starting with `PGRoundTrip`, `RedisGetSet`, `PoolSaturation`, `GenerateTableDDL`, `Compile` -> Tier 3

Budget values are hardcoded in the script (same source of truth as the `Test*_Budget` functions).

### Delete `docs/BENCHMARKING.md`

The design spec has served its purpose. Implementation is complete (Tier 1/2) or specified here (Tier 3). The wiki guide replaces it as the living reference.

---

## Wiki: `Performance-Benchmarking-Guide.md`

**Location:** `wiki/Performance-Benchmarking-Guide.md`

**Sidebar addition** (between Operations and Project):
```markdown
**Performance**
- [Benchmarking Guide](Performance-Benchmarking-Guide)
```

**Content structure:**

### 1. Overview
One paragraph: Moca has a three-tier benchmarking suite that measures individual functions, composed pipelines, and infrastructure. CI automatically detects regressions on every PR.

### 2. The Three Tiers

**Tier 1 — Critical Hot Path:** Benchmarks for functions that execute on every single API request. A regression here affects every user of every tenant. Covers: MetaType registry lookup, document CRUD, SQL query building, HTTP middleware chain.

**Tier 2 — Per-Request Components:** Benchmarks for functions called within the hot path but not on every request. Covers: field validation, document naming, lifecycle event dispatch, hook resolution, rate limiting, request/response transformation, database transactions.

**Tier 3 — Infrastructure:** Benchmarks that measure the underlying systems (PostgreSQL, Redis, connection pools) in isolation. These establish baselines so that when a Tier 1 or Tier 2 benchmark regresses, you can determine whether the cause is in the application code or the infrastructure layer. Covers: PG round-trip latency, Redis GET/SET, pool saturation under load, DDL generation, schema compilation.

### 3. How to Run Benchmarks
Table with the 5 make targets, what each does, and whether Docker is required.

### 4. How to Read Results
Explain `ns/op`, `B/op`, `allocs/op`. Show a sample output and annotate what "good" vs "bad" looks like. Explain benchstat output format.

### 5. CI Regression Detection
How the PR workflow works: runs benchmarks on base and PR branch, compares with benchstat, posts a structured PR comment (summary table grouped by tier, allocation changes, budget proximity), fails the check if any benchmark regresses >= 10%. Include an annotated screenshot or example of what the PR comment looks like.

### 6. Performance Budgets
Table of the 4 budgeted benchmarks with their hard limits. Explain that these run as normal tests and fail `make test` if exceeded.

### 7. Profiling a Regression
Quick guide: `make bench-profile`, then `go tool pprof -http=:8080 cpu.prof`. What to look for in the flame graph.

---

## File Summary

| # | File | Type | Build Tag |
|---|------|------|-----------|
| 1 | `pkg/orm/pg_roundtrip_integration_bench_test.go` | Tier 3 bench | integration |
| 2 | `internal/drivers/redis_integration_bench_test.go` | Tier 3 bench | integration |
| 3 | `pkg/orm/pool_saturation_integration_bench_test.go` | Tier 3 bench | integration |
| 4 | `pkg/meta/ddl_bench_test.go` | Tier 3 bench | none |
| 5 | `pkg/meta/compiler_bench_test.go` | Tier 3 bench | none |
| 6 | `pkg/meta/budget_test.go` | Budget test | none |
| 7 | `pkg/api/budget_test.go` | Budget test | none |
| 8 | `pkg/hooks/budget_test.go` | Budget test | none |
| 9 | `pkg/document/crud_concurrency_integration_bench_test.go` | Concurrency bench | integration |
| 10 | `wiki/Performance-Benchmarking-Guide.md` | Wiki doc | n/a |
| 11 | `wiki/_Sidebar.md` | Wiki sidebar edit | n/a |
| 12 | `Makefile` | Edit BENCH_PKGS | n/a |
| 13 | `.github/workflows/benchmark.yml` | Edit BENCH_PKGS + comment format | n/a |
| 14 | `.github/scripts/format-bench-comment.sh` | New script | n/a |
| 15 | `docs/BENCHMARKING.md` | Delete | n/a |

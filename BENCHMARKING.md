# Benchmarking Strategy

> How to measure Moca's performance, what to benchmark, and how to catch regressions before they ship.

---

## Philosophy

Moca handles every tenant's request through a common hot path: resolve tenant, authenticate, check rate limits, load MetaType from cache, run document lifecycle, build and execute SQL, transform the response. A regression in any of these stages multiplies across every request for every tenant. The benchmarking strategy therefore focuses on **per-function microbenchmarks** for the core runtime and API layer, **end-to-end request benchmarks** for the composed pipeline, and **CI-integrated regression detection** to catch degradation automatically.

---

## What to Benchmark

### Tier 1 — Critical Hot Path (every request touches these)

These functions are called on every API request. A 10% regression here affects every user of every tenant.

| Function | File | Why It Matters |
|----------|------|----------------|
| `Registry.Get(ctx, site, doctype)` | `pkg/meta/registry.go` | Three-tier lookup (sync.Map → Redis → PostgreSQL). Called on every document operation. L1 hit should be <100ns. |
| `DocManager.Get(ctx, doctype, name)` | `pkg/document/crud.go` | Single-document fetch with child row loading and permission check. The most common read operation. |
| `DocManager.GetList(ctx, doctype, opts)` | `pkg/document/crud.go` | List with filtering and pagination. Drives every list view in the Desk UI. |
| `DocManager.Insert(ctx, doctype, values)` | `pkg/document/crud.go` | Full lifecycle: naming → validation → DB insert → hook dispatch. The write hot path. |
| `QueryBuilder.Build(ctx)` | `pkg/orm/query.go` | SQL generation with field validation, MetaType resolution, parameterized output. Called by every list/get. |
| `Gateway.Handler()` middleware chain | `pkg/api/gateway.go` | RequestID → CORS → Tenant → Auth → RateLimit → Version → route. Every HTTP request traverses this. |

### Tier 2 — Per-Request Components (called within the hot path)

| Function | File | Why It Matters |
|----------|------|----------------|
| `Validator.ValidateDoc(ctx, doc, pool)` | `pkg/document/validator.go` | Type coercion + 8 validation rules per field. Cost scales with field count. |
| `NamingEngine.GenerateName(ctx, doc, pool)` | `pkg/document/naming.go` | Pattern-based naming hits PostgreSQL sequences. Contention risk under concurrency. |
| `dispatchEvent(ctrl, event, ctx, doc)` | `pkg/document/lifecycle.go` | Switch dispatch across 14 lifecycle events. Called multiple times per write. |
| `HookRegistry.Resolve(doctype, event)` | `pkg/hooks/registry.go` | Topological sort of hooks by dependency + priority. Cost grows with installed apps. |
| `RateLimiter.Allow(ctx, key, cfg)` | `pkg/api/ratelimit.go` | Redis sliding-window: ZREMRANGEBYSCORE + ZCARD + ZADD. Network-bound. |
| `TransformerChain.TransformRequest/Response` | `pkg/api/transformer.go` | Field filtering + aliasing. Cost scales with field count and transformer count. |
| `WithTransaction(ctx, pool, fn)` | `pkg/orm/transaction.go` | Begin → execute → commit/rollback. Measures transaction overhead. |

### Tier 3 — Infrastructure (benchmarked with integration tag)

| Target | What to Measure |
|--------|----------------|
| PostgreSQL round-trip | Simple INSERT + SELECT latency for a document table |
| Redis cache hit/miss | `Registry.Get` with warm vs cold L1/L2 cache |
| Connection pool under load | Pool saturation behavior at 50, 100, 500 concurrent goroutines |
| Schema DDL generation | `GenerateTableDDL` for MetaTypes with 10, 50, 100 fields |
| `Compile` (schema compiler) | JSON → MetaType compilation for varying complexity definitions |

---

## Benchmark Design

### Naming Convention

All benchmark files live alongside the code they test, using Go's standard `_test.go` suffix and `Benchmark` prefix:

```
pkg/meta/registry_bench_test.go
pkg/document/crud_bench_test.go
pkg/orm/query_bench_test.go
pkg/api/gateway_bench_test.go
pkg/api/ratelimit_bench_test.go
pkg/hooks/registry_bench_test.go
```

### Function Naming Pattern

```go
// Format: Benchmark{Function}_{Scenario}
func BenchmarkRegistryGet_L1Hit(b *testing.B)           // sync.Map hit
func BenchmarkRegistryGet_L2Hit(b *testing.B)           // Redis hit, sync.Map miss
func BenchmarkRegistryGet_L3Miss(b *testing.B)          // Full DB fallback

func BenchmarkDocManagerInsert_SimpleDoc(b *testing.B)   // 5-field document
func BenchmarkDocManagerInsert_ComplexDoc(b *testing.B)  // 50-field document with children
func BenchmarkDocManagerInsert_Parallel(b *testing.B)    // b.RunParallel for concurrency

func BenchmarkQueryBuilderBuild_SimpleFilter(b *testing.B)
func BenchmarkQueryBuilderBuild_ComplexJoins(b *testing.B)
func BenchmarkQueryBuilderBuild_JSONBFilter(b *testing.B)

func BenchmarkGatewayHandler_FullChain(b *testing.B)     // httptest through entire middleware
func BenchmarkRateLimiterAllow_SingleKey(b *testing.B)
func BenchmarkRateLimiterAllow_Parallel(b *testing.B)

func BenchmarkHookRegistryResolve_NoHooks(b *testing.B)
func BenchmarkHookRegistryResolve_10Hooks(b *testing.B)
func BenchmarkHookRegistryResolve_WithDeps(b *testing.B)

func BenchmarkValidateDoc_10Fields(b *testing.B)
func BenchmarkValidateDoc_50Fields(b *testing.B)
func BenchmarkValidateDoc_100Fields(b *testing.B)

func BenchmarkTransformerChain_Request(b *testing.B)
func BenchmarkTransformerChain_Response(b *testing.B)
```

### Benchmark Structure

Every benchmark should follow this pattern:

```go
func BenchmarkRegistryGet_L1Hit(b *testing.B) {
    // Setup: create registry, pre-populate cache
    reg := setupTestRegistry(b)
    ctx := context.Background()

    // Reset timer after setup
    b.ResetTimer()

    // Report allocations (critical for GC pressure tracking)
    b.ReportAllocs()

    for i := 0; i < b.N; i++ {
        mt, err := reg.Get(ctx, "testsite", "SalesOrder")
        if err != nil {
            b.Fatal(err)
        }
        // Prevent compiler optimization
        _ = mt
    }
}
```

For concurrency benchmarks:

```go
func BenchmarkDocManagerInsert_Parallel(b *testing.B) {
    dm := setupDocManager(b)

    b.ResetTimer()
    b.ReportAllocs()

    b.RunParallel(func(pb *testing.PB) {
        i := 0
        for pb.Next() {
            values := map[string]any{
                "customer_name": fmt.Sprintf("Customer-%d", i),
                "grand_total":   float64(i * 100),
            }
            _, err := dm.Insert(testDocCtx(), "SalesOrder", values)
            if err != nil {
                b.Fatal(err)
            }
            i++
        }
    })
}
```

### Sub-Benchmarks for Scaling Behavior

Use `b.Run` to measure how performance scales with input size:

```go
func BenchmarkValidateDoc(b *testing.B) {
    for _, fieldCount := range []int{5, 10, 25, 50, 100} {
        b.Run(fmt.Sprintf("Fields_%d", fieldCount), func(b *testing.B) {
            doc := generateDocWithNFields(fieldCount)
            v := document.NewValidator(registry)

            b.ResetTimer()
            b.ReportAllocs()

            for i := 0; i < b.N; i++ {
                _ = v.ValidateDoc(testCtx(), doc, pool)
            }
        })
    }
}
```

This produces output like:

```
BenchmarkValidateDoc/Fields_5     500000    2340 ns/op    1024 B/op    12 allocs/op
BenchmarkValidateDoc/Fields_50     50000   23100 ns/op   10240 B/op   120 allocs/op
BenchmarkValidateDoc/Fields_100    25000   47200 ns/op   20480 B/op   240 allocs/op
```

If the relationship is not linear, there is an algorithmic problem.

---

## Running Benchmarks

### Locally

```bash
# All benchmarks (unit-level, no Docker required)
go test -bench=. -benchmem -count=5 ./pkg/...

# Specific package
go test -bench=. -benchmem -count=5 ./pkg/meta/...

# Specific benchmark
go test -bench=BenchmarkRegistryGet -benchmem -count=5 ./pkg/meta/...

# Integration benchmarks (requires Docker for PG + Redis)
go test -tags=integration -bench=. -benchmem -count=5 ./pkg/...

# Save results for comparison
go test -bench=. -benchmem -count=10 ./pkg/... | tee bench-$(git rev-parse --short HEAD).txt
```

### With CPU and Memory Profiles

```bash
# CPU profile
go test -bench=BenchmarkDocManagerInsert -cpuprofile=cpu.prof ./pkg/document/...
go tool pprof -http=:8080 cpu.prof

# Memory profile
go test -bench=BenchmarkDocManagerInsert -memprofile=mem.prof ./pkg/document/...
go tool pprof -http=:8080 mem.prof

# Trace (for concurrency analysis)
go test -bench=BenchmarkDocManagerInsert_Parallel -trace=trace.out ./pkg/document/...
go tool trace trace.out
```

---

## Detecting Regressions Over Time

### Tool: benchstat

Go's official `benchstat` tool compares benchmark runs statistically. It requires multiple samples (`-count=10`) to compute confidence intervals and reject noise.

```bash
# Install
go install golang.org/x/perf/cmd/benchstat@latest

# Compare two runs
benchstat bench-old.txt bench-new.txt
```

Output:

```
name                          old time/op    new time/op    delta
RegistryGet_L1Hit-8             45.2ns ± 2%    44.8ns ± 1%     ~  (p=0.421 n=10+10)
DocManagerInsert_SimpleDoc-8    1.23ms ± 3%    1.58ms ± 2%  +28.46%  (p=0.000 n=10+10)
                                                             ^^^^^^ REGRESSION
```

Any delta with `p < 0.05` and magnitude `> 5%` is a real regression.

### CI Pipeline: GitHub Actions

The workflow below runs benchmarks on every pull request, compares against the base branch, and fails if any benchmark regresses beyond the threshold.

```yaml
# .github/workflows/benchmark.yml
name: Benchmark Regression Check

on:
  pull_request:
    branches: [main, develop]
    paths:
      - 'pkg/**'
      - 'internal/**'
      - 'cmd/**'

permissions:
  pull-requests: write
  contents: read

jobs:
  benchmark:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:16
        env:
          POSTGRES_PASSWORD: bench
          POSTGRES_DB: moca_bench
        ports: ['5433:5432']
        options: >-
          --health-cmd pg_isready
          --health-interval 5s
          --health-timeout 5s
          --health-retries 5
      redis:
        image: redis:7
        ports: ['6379:6379']
        options: >-
          --health-cmd "redis-cli ping"
          --health-interval 5s
          --health-timeout 5s
          --health-retries 5

    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'

      - name: Install benchstat
        run: go install golang.org/x/perf/cmd/benchstat@latest

      # Run benchmarks on the base branch
      - name: Benchmark base branch
        run: |
          git checkout ${{ github.event.pull_request.base.sha }}
          go test -tags=integration -bench=. -benchmem -count=10 -timeout=20m \
            ./pkg/... 2>/dev/null | tee /tmp/bench-base.txt || true

      # Run benchmarks on the PR branch
      - name: Benchmark PR branch
        run: |
          git checkout ${{ github.event.pull_request.head.sha }}
          go test -tags=integration -bench=. -benchmem -count=10 -timeout=20m \
            ./pkg/... 2>/dev/null | tee /tmp/bench-pr.txt

      # Compare and check for regressions
      - name: Compare benchmarks
        id: compare
        run: |
          RESULT=$(benchstat /tmp/bench-base.txt /tmp/bench-pr.txt)
          echo "$RESULT"
          echo "result<<EOF" >> $GITHUB_OUTPUT
          echo "$RESULT" >> $GITHUB_OUTPUT
          echo "EOF" >> $GITHUB_OUTPUT

          # Fail if any benchmark regressed more than 10%
          if echo "$RESULT" | grep -E '\+[1-9][0-9]\.[0-9]+%' | grep -v '~'; then
            echo "regression=true" >> $GITHUB_OUTPUT
          else
            echo "regression=false" >> $GITHUB_OUTPUT
          fi

      # Post results as PR comment
      - name: Comment on PR
        uses: marocchino/sticky-pull-request-comment@v2
        with:
          header: benchmark
          message: |
            ## Benchmark Results

            <details>
            <summary>Click to expand</summary>

            ```
            ${{ steps.compare.outputs.result }}
            ```

            </details>

            ${{ steps.compare.outputs.regression == 'true'
              && '> **Warning:** One or more benchmarks regressed by more than 10%. Please investigate before merging.'
              || '> All benchmarks within acceptable range.' }}

      - name: Fail on regression
        if: steps.compare.outputs.regression == 'true'
        run: |
          echo "::error::Benchmark regression detected. See PR comment for details."
          exit 1
```

### Historical Tracking

For long-term trend analysis beyond PR-level comparison, store benchmark results over time:

**Option A: Git-based storage (simple)**

Commit benchmark results to a `benchmarks/` directory on a dedicated `bench-results` branch. A scheduled workflow runs benchmarks nightly on `main` and commits the output:

```yaml
# .github/workflows/bench-nightly.yml
name: Nightly Benchmark
on:
  schedule:
    - cron: '0 2 * * *'  # 2 AM UTC daily
  workflow_dispatch:

jobs:
  nightly:
    runs-on: ubuntu-latest
    # ... same services block as above ...
    steps:
      - uses: actions/checkout@v4
        with:
          ref: main

      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'

      - name: Run benchmarks
        run: |
          go test -tags=integration -bench=. -benchmem -count=10 -timeout=20m \
            ./pkg/... 2>/dev/null | tee bench-results.txt

      - name: Store results
        run: |
          git checkout bench-results 2>/dev/null || git checkout --orphan bench-results
          mkdir -p results
          DATE=$(date +%Y-%m-%d)
          COMMIT=$(git rev-parse --short main)
          cp bench-results.txt "results/${DATE}-${COMMIT}.txt"
          git add results/
          git commit -m "bench: ${DATE} (${COMMIT})"
          git push origin bench-results
```

Then compare any two dates:

```bash
benchstat results/2026-03-01-abc1234.txt results/2026-04-01-def5678.txt
```

**Option B: benchstat dashboard with `golang.org/x/perf/cmd/benchseries`**

For richer visualization, use `benchseries` to parse stored results into a time series and plot trends. This is useful once you have 30+ nightly data points and want to spot gradual drift.

**Option C: Third-party (Conbench, Codspeed, BenchCI)**

If the project grows to need a hosted dashboard with alerting, services like Codspeed or BenchCI integrate directly with GitHub Actions and provide trend graphs, regression alerts, and team notifications out of the box.

---

## Performance Budgets

Define explicit budgets for the critical paths. The CI pipeline should enforce these as hard failures, not just regression deltas:

| Benchmark | Budget | Rationale |
|-----------|--------|-----------|
| `RegistryGet_L1Hit` | < 200 ns/op | sync.Map lookup; should be near-zero |
| `RegistryGet_L2Hit` | < 500 µs/op | Redis GET; network-bound |
| `DocManagerGet_SimpleDoc` | < 2 ms/op | Single SELECT + child rows |
| `DocManagerInsert_SimpleDoc` | < 5 ms/op | Full lifecycle with transaction |
| `QueryBuilderBuild_SimpleFilter` | < 10 µs/op | Pure CPU, no I/O |
| `ValidateDoc_10Fields` | < 50 µs/op | CPU-only validation |
| `RateLimiterAllow` | < 1 ms/op | 3 Redis commands |
| `TransformerChain_Response` | < 20 µs/op | In-memory map operations |
| `HookRegistryResolve_10Hooks` | < 5 µs/op | Topological sort, small graph |
| `GatewayHandler_FullChain` | < 10 ms/op | End-to-end HTTP through all middleware |

Enforce absolute budgets alongside relative regression checks:

```go
func BenchmarkRegistryGet_L1Hit(b *testing.B) {
    // ... benchmark code ...
}

func TestRegistryGet_L1Hit_Budget(t *testing.T) {
    result := testing.Benchmark(BenchmarkRegistryGet_L1Hit)
    nsPerOp := result.NsPerOp()
    if nsPerOp > 200 {
        t.Errorf("Registry.Get L1 hit took %d ns/op, budget is 200 ns/op", nsPerOp)
    }
}
```

---

## Allocation Tracking

Go's GC is a major source of latency variance. Every benchmark must use `b.ReportAllocs()` and allocation counts should be tracked as strictly as execution time. Key targets:

| Path | Allocation Budget | Notes |
|------|-------------------|-------|
| `Registry.Get` (L1 hit) | 0 allocs/op | Must return cached pointer, no copying |
| `QueryBuilder.Build` | ≤ 5 allocs/op | String builder + params slice |
| `TransformerChain` | ≤ 2 allocs/op per transformer | Reuse maps where possible |
| `dispatchEvent` | 0 allocs/op | Switch dispatch, no reflection |
| `HookRegistry.Resolve` | ≤ 1 alloc/op | Cache sorted result after first resolution |

When `benchstat` shows an allocation regression, investigate with:

```bash
go test -bench=BenchmarkRegistryGet -memprofile=mem.prof -memprofilerate=1 ./pkg/meta/...
go tool pprof -alloc_objects mem.prof
```

---

## Concurrency Stress Tests

Beyond `b.RunParallel`, dedicated stress benchmarks should verify that performance scales linearly (or at least sub-quadratically) with goroutine count:

```go
func BenchmarkDocManagerInsert_Concurrency(b *testing.B) {
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
}
```

If throughput plateaus or drops sharply at a certain concurrency level, it indicates lock contention or pool exhaustion. Compare the `ns/op` at each level — if `Goroutines_500` is more than 10x `Goroutines_1`, there is a bottleneck.

---

## Makefile Integration

Add these targets to the project `Makefile`:

```makefile
.PHONY: bench bench-integration bench-compare bench-profile

# Unit-level benchmarks (no Docker)
bench:
	go test -bench=. -benchmem -count=5 -timeout=10m ./pkg/... | tee bench-latest.txt

# Integration benchmarks (requires Docker)
bench-integration:
	docker-compose up -d
	go test -tags=integration -bench=. -benchmem -count=10 -timeout=20m ./pkg/... | tee bench-latest.txt
	docker-compose down

# Compare current results against a saved baseline
bench-compare: bench
	@if [ ! -f bench-baseline.txt ]; then \
		echo "No baseline found. Run 'make bench-save-baseline' first."; exit 1; \
	fi
	benchstat bench-baseline.txt bench-latest.txt

# Save current results as baseline
bench-save-baseline: bench
	cp bench-latest.txt bench-baseline.txt
	@echo "Baseline saved."

# CPU + memory profile for a specific benchmark
bench-profile:
	@read -p "Benchmark pattern (e.g. BenchmarkDocManagerInsert): " PATTERN; \
	read -p "Package (e.g. ./pkg/document/...): " PKG; \
	go test -bench=$$PATTERN -cpuprofile=cpu.prof -memprofile=mem.prof -benchmem $$PKG; \
	echo "Profiles saved: cpu.prof, mem.prof"; \
	echo "View with: go tool pprof -http=:8080 cpu.prof"
```

---

## When to Add New Benchmarks

Add a benchmark whenever you:

1. **Implement a new milestone** — every new `pkg/` function in the request hot path gets a benchmark.
2. **Fix a performance bug** — the benchmark proves the fix and prevents regression.
3. **Add a new field type** — benchmark `ValidateDoc` with that field type to catch expensive coercion.
4. **Add a new middleware** — benchmark the full `Gateway.Handler()` chain to measure the added cost.
5. **Change a data structure** — before/after benchmarks prove the change was worth it.

The PR template should include a checkbox: "If this touches `pkg/meta`, `pkg/document`, `pkg/orm`, `pkg/api`, or `pkg/hooks` — have you added or updated benchmarks?"

---

## Summary

| Layer | What | How | Where |
|-------|------|-----|-------|
| Microbenchmark | Individual functions | `go test -bench` with `b.ReportAllocs()` | `*_bench_test.go` alongside source |
| Scaling benchmark | Same function, varying input size | `b.Run` with field count / goroutine count | Same files, sub-benchmarks |
| Integration benchmark | Full DB/Redis round-trip | `go test -tags=integration -bench` | Same files, build-tagged |
| PR regression check | Compare base vs PR | `benchstat` in GitHub Actions | `.github/workflows/benchmark.yml` |
| Nightly trend | Track drift over time | Scheduled CI → commit results to branch | `.github/workflows/bench-nightly.yml` |
| Absolute budget | Hard performance limits | `testing.Benchmark` + assertion in `Test*` | Same files, test functions |
| Profiling | Investigate regressions | `go tool pprof` / `go tool trace` | On-demand via `make bench-profile` |

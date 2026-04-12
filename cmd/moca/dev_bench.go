package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/osama1998H/moca/internal/output"
	"github.com/spf13/cobra"
)

// newDevBenchCmd creates the "moca dev bench" command for running microbenchmarks
// against a live site's infrastructure (PostgreSQL and Redis).
func newDevBenchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Run microbenchmarks on queries/operations",
		Long: `Run microbenchmarks against a live site to measure operation latency.

Executes synthetic operations against the site's PostgreSQL and Redis
connections, then reports latency statistics (min, max, mean, percentiles,
and operations/second).

Operations:
  read   - Simple SELECT query against the site's DB pool
  write  - Advisory lock/unlock cycle simulating a write pattern
  query  - Filtered SELECT against information_schema
  cache  - Redis SET/GET/DEL cycle
  all    - All of the above (default)`,
		RunE: runDevBench,
		Example: `  moca dev bench --site mysite.localhost
  moca dev bench --site mysite.localhost --operation read --iterations 5000
  moca dev bench --site mysite.localhost --concurrent 20 --json`,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site (required)")
	f.String("operation", "all", "Operation to benchmark: read, write, query, cache, all")
	f.Int("iterations", 1000, "Number of iterations per operation")
	f.Int("concurrent", 10, "Number of concurrent goroutines")
	f.Bool("json", false, "Output in JSON format")
	_ = cmd.MarkFlagRequired("site")

	return cmd
}

// benchResult holds computed statistics for a single benchmark operation.
type benchResult struct {
	Operation string
	Min       time.Duration
	Max       time.Duration
	Mean      time.Duration
	P50       time.Duration
	P95       time.Duration
	P99       time.Duration
	OpsPerSec float64
	Errors    int
}

func runDevBench(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	site, _ := cmd.Flags().GetString("site")
	operation, _ := cmd.Flags().GetString("operation")
	iterations, _ := cmd.Flags().GetInt("iterations")
	concurrent, _ := cmd.Flags().GetInt("concurrent")

	// Validate operation flag.
	validOps := map[string]bool{"read": true, "write": true, "query": true, "cache": true, "all": true}
	if !validOps[operation] {
		return output.NewCLIError("Invalid operation").
			WithCause(fmt.Sprintf("Unknown operation %q", operation)).
			WithFix("Use one of: read, write, query, cache, all")
	}

	if iterations <= 0 {
		return output.NewCLIError("Invalid iterations").
			WithFix("Iterations must be a positive integer.")
	}
	if concurrent <= 0 {
		return output.NewCLIError("Invalid concurrency").
			WithFix("Concurrent must be a positive integer.")
	}

	// Connect to infrastructure.
	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), cliCtx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	// Acquire the site's database pool.
	pool, poolErr := svc.DB.ForSite(cmd.Context(), site)
	if poolErr != nil {
		return output.NewCLIError("Cannot connect to site database").
			WithErr(poolErr).
			WithContext("site: " + site).
			WithFix("Ensure the site exists and PostgreSQL is running.")
	}

	// Verify Redis is available for cache benchmarks.
	redisAvailable := svc.Redis != nil && svc.Redis.Cache != nil
	if (operation == "cache" || operation == "all") && !redisAvailable {
		if operation == "cache" {
			return output.NewCLIError("Redis is not available").
				WithFix("Ensure Redis is running and configured in moca.yaml.")
		}
		w.PrintWarning("Redis unavailable — skipping cache benchmark")
	}

	w.Print("Benchmark Results (site: %s)", site)
	w.Print("Iterations: %d | Concurrency: %d", iterations, concurrent)
	w.Print("")

	// Determine which operations to run.
	ops := []string{operation}
	if operation == "all" {
		ops = []string{"read", "write", "query", "cache"}
	}

	var results []benchResult

	for _, op := range ops {
		if op == "cache" && !redisAvailable {
			continue
		}

		var benchFn func(context.Context) error
		switch op {
		case "read":
			benchFn = func(ctx context.Context) error {
				_, err := pool.Exec(ctx, "SELECT 1")
				return err
			}
		case "write":
			benchFn = func(ctx context.Context) error {
				tx, err := pool.Begin(ctx)
				if err != nil {
					return err
				}
				_, _ = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", fmt.Sprintf("bench-%d", time.Now().UnixNano()))
				return tx.Commit(ctx)
			}
		case "query":
			benchFn = func(ctx context.Context) error {
				rows, err := pool.Query(ctx, "SELECT column_name, data_type FROM information_schema.columns LIMIT 10")
				if err != nil {
					return err
				}
				rows.Close()
				return nil
			}
		case "cache":
			benchFn = func(ctx context.Context) error {
				key := fmt.Sprintf("moca:bench:%d", time.Now().UnixNano())
				if err := svc.Redis.Cache.Set(ctx, key, "benchmark-value", 10*time.Second).Err(); err != nil {
					return err
				}
				if err := svc.Redis.Cache.Get(ctx, key).Err(); err != nil {
					return err
				}
				return svc.Redis.Cache.Del(ctx, key).Err()
			}
		}

		w.Debugf("Running %s benchmark (%d iterations, %d concurrent)...", op, iterations, concurrent)
		durations, errCount := runBenchmark(cmd.Context(), iterations, concurrent, benchFn)
		result := computeStats(op, durations, errCount)
		results = append(results, result)
	}

	// Output results.
	jsonMode, _ := cmd.Flags().GetBool("json")
	if jsonMode || w.Mode() == output.ModeJSON {
		return printBenchJSON(w, site, iterations, concurrent, results)
	}
	return printBenchTable(w, results)
}

// runBenchmark executes fn concurrently and collects timing results.
func runBenchmark(ctx context.Context, iterations, concurrent int, fn func(context.Context) error) ([]time.Duration, int) {
	results := make([]time.Duration, 0, iterations)
	sem := make(chan struct{}, concurrent)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errCount int

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			start := time.Now()
			if err := fn(ctx); err != nil {
				mu.Lock()
				errCount++
				mu.Unlock()
				return
			}
			elapsed := time.Since(start)
			mu.Lock()
			results = append(results, elapsed)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return results, errCount
}

// computeStats calculates benchmark statistics from a sorted duration slice.
func computeStats(operation string, durations []time.Duration, errCount int) benchResult {
	if len(durations) == 0 {
		return benchResult{Operation: operation, Errors: errCount}
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })

	var total time.Duration
	for _, d := range durations {
		total += d
	}

	mean := total / time.Duration(len(durations))
	opsPerSec := float64(len(durations)) / total.Seconds()

	return benchResult{
		Operation: operation,
		Min:       durations[0],
		Max:       durations[len(durations)-1],
		Mean:      mean,
		P50:       percentile(durations, 0.50),
		P95:       percentile(durations, 0.95),
		P99:       percentile(durations, 0.99),
		OpsPerSec: opsPerSec,
		Errors:    errCount,
	}
}

// percentile returns the value at the given percentile from a sorted duration slice.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

// formatDuration formats a duration as milliseconds with 2 decimal places.
func formatDuration(d time.Duration) string {
	return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000.0)
}

func printBenchTable(w *output.Writer, results []benchResult) error {
	headers := []string{"OPERATION", "MIN", "MAX", "MEAN", "P50", "P95", "P99", "OPS/SEC", "ERRORS"}
	var rows [][]string
	for _, r := range results {
		rows = append(rows, []string{
			r.Operation,
			formatDuration(r.Min),
			formatDuration(r.Max),
			formatDuration(r.Mean),
			formatDuration(r.P50),
			formatDuration(r.P95),
			formatDuration(r.P99),
			fmt.Sprintf("%.1f", r.OpsPerSec),
			fmt.Sprintf("%d", r.Errors),
		})
	}
	return w.PrintTable(headers, rows)
}

func printBenchJSON(w *output.Writer, site string, iterations, concurrent int, results []benchResult) error {
	type jsonResult struct {
		Operation string  `json:"operation"`
		MinMs     float64 `json:"min_ms"`
		MaxMs     float64 `json:"max_ms"`
		MeanMs    float64 `json:"mean_ms"`
		P50Ms     float64 `json:"p50_ms"`
		P95Ms     float64 `json:"p95_ms"`
		P99Ms     float64 `json:"p99_ms"`
		OpsPerSec float64 `json:"ops_per_sec"`
		Errors    int     `json:"errors"`
	}

	jsonResults := make([]jsonResult, len(results))
	for i, r := range results {
		jsonResults[i] = jsonResult{
			Operation: r.Operation,
			MinMs:     float64(r.Min.Microseconds()) / 1000.0,
			MaxMs:     float64(r.Max.Microseconds()) / 1000.0,
			MeanMs:    float64(r.Mean.Microseconds()) / 1000.0,
			P50Ms:     float64(r.P50.Microseconds()) / 1000.0,
			P95Ms:     float64(r.P95.Microseconds()) / 1000.0,
			P99Ms:     float64(r.P99.Microseconds()) / 1000.0,
			OpsPerSec: r.OpsPerSec,
			Errors:    r.Errors,
		}
	}

	return w.PrintJSON(map[string]any{
		"site":        site,
		"iterations":  iterations,
		"concurrent":  concurrent,
		"results":     jsonResults,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"description": strings.Join(operationNames(results), ", ") + " benchmark",
	})
}

func operationNames(results []benchResult) []string {
	names := make([]string, len(results))
	for i, r := range results {
		names[i] = r.Operation
	}
	return names
}

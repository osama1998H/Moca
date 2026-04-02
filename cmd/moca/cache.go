package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"

	"github.com/osama1998H/moca/internal/output"
)

// NewCacheCommand returns the "moca cache" command group with all subcommands.
func NewCacheCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Cache management",
		Long:  "Clear caches, view statistics, and pre-warm cache entries.",
	}

	cmd.AddCommand(
		newCacheClearCmd(),
		newCacheWarmCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// cache clear
// ---------------------------------------------------------------------------

func newCacheClearCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear caches for a site",
		Long: `Clear Redis caches for a site. By default clears all cache types.
Use --type to selectively clear specific cache categories.`,
		RunE: runCacheClear,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")
	f.String("type", "all", "Cache type to clear: meta, doc, config, perm, session, schema, all")

	return cmd
}

// cacheTypePatterns maps cache type names to their Redis key patterns.
// Session keys use a separate Redis client (db 2).
var cacheTypePatterns = map[string]struct {
	pattern   string
	isSession bool // true = use Session client (db 2), false = use Cache client (db 0)
}{
	"meta":    {pattern: "meta:%s:*", isSession: false},
	"doc":     {pattern: "doc:%s:*", isSession: false},
	"config":  {pattern: "config:%s", isSession: false},
	"perm":    {pattern: "perm:%s:*", isSession: false},
	"schema":  {pattern: "schema:%s:version", isSession: false},
	"session": {pattern: "session:%s:*", isSession: true},
}

func runCacheClear(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	if svc.Redis == nil {
		return output.NewCLIError("Redis is not available").
			WithFix("Ensure Redis is running and configured in moca.yaml.")
	}

	cacheType, _ := cmd.Flags().GetString("type")
	cacheType = strings.ToLower(strings.TrimSpace(cacheType))

	// Determine which types to clear.
	var typesToClear []string
	if cacheType == "all" {
		typesToClear = []string{"meta", "doc", "config", "perm", "schema", "session"}
	} else {
		if _, ok := cacheTypePatterns[cacheType]; !ok {
			return output.NewCLIError(fmt.Sprintf("Unknown cache type: %q", cacheType)).
				WithFix("Valid types: meta, doc, config, perm, session, schema, all")
		}
		typesToClear = []string{cacheType}
	}

	s := w.NewSpinner("Clearing caches...")
	s.Start()

	results := make(map[string]int64)
	for _, ct := range typesToClear {
		info := cacheTypePatterns[ct]
		pattern := fmt.Sprintf(info.pattern, siteName)

		var client *redis.Client
		if info.isSession {
			client = svc.Redis.Session
		} else {
			client = svc.Redis.Cache
		}

		deleted, delErr := deleteRedisPattern(cmd.Context(), client, pattern)
		if delErr != nil {
			s.Stop("Failed")
			return output.NewCLIError(fmt.Sprintf("Failed to clear %s cache", ct)).
				WithErr(delErr).
				WithCause(delErr.Error())
		}
		results[ct] = deleted
	}

	s.Stop("Caches cleared")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"site":    siteName,
			"cleared": results,
		})
	}

	var total int64
	for ct, count := range results {
		if count > 0 {
			w.Print("  %s %s: %d key(s)", w.Color().Success("✓"), ct, count)
		} else {
			w.Print("  %s %s: no keys found", w.Color().Muted("-"), ct)
		}
		total += count
	}
	w.Print("")
	w.PrintSuccess(fmt.Sprintf("Cleared %d key(s) for site '%s'", total, siteName))

	return nil
}

// deleteRedisPattern deletes all keys matching a pattern. If the pattern
// contains no wildcards, it deletes the single key directly.
func deleteRedisPattern(ctx context.Context, client *redis.Client, pattern string) (int64, error) {
	if !strings.Contains(pattern, "*") {
		// Exact key — delete directly.
		n, err := client.Del(ctx, pattern).Result()
		return n, err
	}

	// Wildcard pattern — scan and delete.
	keys, err := client.Keys(ctx, pattern).Result()
	if err != nil {
		return 0, err
	}
	if len(keys) == 0 {
		return 0, nil
	}

	n, err := client.Del(ctx, keys...).Result()
	return n, err
}

// ---------------------------------------------------------------------------
// cache warm
// ---------------------------------------------------------------------------

func newCacheWarmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "warm",
		Short: "Pre-warm caches (metadata)",
		Long: `Load all registered MetaTypes from the database into L1 (in-memory)
and L2 (Redis) caches. Useful after a deployment or cache clear.`,
		RunE: runCacheWarm,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")

	return cmd
}

func runCacheWarm(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	s := w.NewSpinner("Warming caches...")
	s.Start()

	// Query all registered DocType names from tab_doctype.
	pool, poolErr := svc.DB.ForSite(cmd.Context(), siteName)
	if poolErr != nil {
		s.Stop("Failed")
		return output.NewCLIError("Cannot connect to site database").
			WithErr(poolErr).
			WithContext("site: " + siteName)
	}

	rows, queryErr := pool.Query(cmd.Context(), `SELECT name FROM tab_doctype ORDER BY name`)
	if queryErr != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to query registered MetaTypes").
			WithErr(queryErr).
			WithCause(queryErr.Error())
	}
	defer rows.Close()

	var doctypes []string
	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr != nil {
			s.Stop("Failed")
			return output.NewCLIError("Failed to scan doctype name").WithErr(scanErr)
		}
		doctypes = append(doctypes, name)
	}
	if rows.Err() != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to iterate doctypes").WithErr(rows.Err())
	}

	// Warm each MetaType via Registry.Get (triggers L3→L2→L1 promotion).
	warmed := 0
	var failed []string
	for _, dt := range doctypes {
		if _, getErr := svc.Registry.Get(cmd.Context(), siteName, dt); getErr != nil {
			failed = append(failed, dt)
			w.Debugf("failed to warm %s: %v", dt, getErr)
		} else {
			warmed++
		}
	}

	s.Stop("Cache warm complete")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"site":   siteName,
			"warmed": warmed,
			"failed": len(failed),
			"total":  len(doctypes),
		})
	}

	w.PrintSuccess(fmt.Sprintf("Warmed %d MetaType(s) for site '%s'", warmed, siteName))
	if len(failed) > 0 {
		w.PrintWarning(fmt.Sprintf("Failed to warm %d MetaType(s): %s", len(failed), strings.Join(failed, ", ")))
	}

	return nil
}

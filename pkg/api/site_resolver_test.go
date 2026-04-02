package api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/moca-framework/moca/pkg/observe"
	"github.com/moca-framework/moca/pkg/tenancy"
)

// --- test doubles ---

type mockQuerier struct {
	sites map[string]*siteMetadata
}

func (m *mockQuerier) QuerySiteInfo(_ context.Context, siteID string) (*siteMetadata, error) {
	meta, ok := m.sites[siteID]
	if !ok {
		return nil, tenancy.ErrSiteNotFound
	}
	return meta, nil
}

type mockPoolProvider struct {
	pools map[string]*pgxpool.Pool
}

func (m *mockPoolProvider) ForSite(_ context.Context, siteName string) (*pgxpool.Pool, error) {
	p, ok := m.pools[siteName]
	if !ok {
		return nil, errors.New("pool not found")
	}
	return p, nil
}

// countingQuerier wraps a querier and counts calls.
type countingQuerier struct {
	inner siteInfoQuerier
	calls int
}

func (c *countingQuerier) QuerySiteInfo(ctx context.Context, siteID string) (*siteMetadata, error) {
	c.calls++
	return c.inner.QuerySiteInfo(ctx, siteID)
}

func newTestResolver(querier siteInfoQuerier, pools sitePoolProvider, redisClient *redis.Client) *DBSiteResolver {
	return &DBSiteResolver{
		querier: querier,
		pools:   pools,
		redis:   redisClient,
		logger:  observe.NewLogger(0),
	}
}

// --- tests ---

func TestDBSiteResolver_PopulatesFullContext(t *testing.T) {
	querier := &mockQuerier{
		sites: map[string]*siteMetadata{
			"acme": {
				DBSchema:      "tenant_acme",
				Status:        "active",
				Config:        map[string]any{"timezone": "UTC"},
				InstalledApps: []string{"core", "crm"},
			},
		},
	}
	pools := &mockPoolProvider{pools: map[string]*pgxpool.Pool{"acme": nil}}
	resolver := newTestResolver(querier, pools, nil)

	site, err := resolver.ResolveSite(context.Background(), "acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if site.Name != "acme" {
		t.Errorf("Name = %q, want %q", site.Name, "acme")
	}
	if site.DBSchema != "tenant_acme" {
		t.Errorf("DBSchema = %q, want %q", site.DBSchema, "tenant_acme")
	}
	if site.Status != "active" {
		t.Errorf("Status = %q, want %q", site.Status, "active")
	}
	if site.RedisPrefix != "acme:" {
		t.Errorf("RedisPrefix = %q, want %q", site.RedisPrefix, "acme:")
	}
	if site.StorageBucket != "acme/" {
		t.Errorf("StorageBucket = %q, want %q", site.StorageBucket, "acme/")
	}
	if len(site.InstalledApps) != 2 {
		t.Errorf("InstalledApps len = %d, want 2", len(site.InstalledApps))
	}
	if site.Config["timezone"] != "UTC" {
		t.Errorf("Config[timezone] = %v, want UTC", site.Config["timezone"])
	}
}

func TestDBSiteResolver_DisabledSite(t *testing.T) {
	querier := &mockQuerier{
		sites: map[string]*siteMetadata{
			"down": {DBSchema: "tenant_down", Status: "disabled"},
		},
	}
	pools := &mockPoolProvider{pools: map[string]*pgxpool.Pool{"down": nil}}
	resolver := newTestResolver(querier, pools, nil)

	_, err := resolver.ResolveSite(context.Background(), "down")
	if !errors.Is(err, tenancy.ErrSiteDisabled) {
		t.Errorf("err = %v, want ErrSiteDisabled", err)
	}
}

func TestDBSiteResolver_NotFound(t *testing.T) {
	querier := &mockQuerier{sites: map[string]*siteMetadata{}}
	pools := &mockPoolProvider{pools: map[string]*pgxpool.Pool{}}
	resolver := newTestResolver(querier, pools, nil)

	_, err := resolver.ResolveSite(context.Background(), "noexist")
	if !errors.Is(err, tenancy.ErrSiteNotFound) {
		t.Errorf("err = %v, want ErrSiteNotFound", err)
	}
}

func TestDBSiteResolver_RedisCacheHit(t *testing.T) {
	// Use a real Redis client for this test. Skip if Redis is not available.
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available, skipping cache test")
	}
	defer func() { _ = client.Close() }()

	// Pre-populate cache.
	meta := &siteMetadata{
		DBSchema:      "tenant_cached",
		Status:        "active",
		Config:        map[string]any{"lang": "en"},
		InstalledApps: []string{"core"},
	}
	data, _ := json.Marshal(meta)
	client.Set(ctx, siteMetaCacheKey("cached"), data, siteMetaCacheTTL)
	defer client.Del(ctx, siteMetaCacheKey("cached"))

	// Querier should NOT be called (cache hit).
	counting := &countingQuerier{inner: &mockQuerier{sites: map[string]*siteMetadata{}}}
	pools := &mockPoolProvider{pools: map[string]*pgxpool.Pool{"cached": nil}}
	resolver := newTestResolver(counting, pools, client)

	site, err := resolver.ResolveSite(ctx, "cached")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if counting.calls != 0 {
		t.Errorf("DB querier called %d times, want 0 (cache hit)", counting.calls)
	}
	if site.Name != "cached" {
		t.Errorf("Name = %q, want %q", site.Name, "cached")
	}
	if site.Status != "active" {
		t.Errorf("Status = %q, want %q", site.Status, "active")
	}
}

func TestDBSiteResolver_RedisCacheMiss(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available, skipping cache test")
	}
	defer func() { _ = client.Close() }()

	// Ensure cache is empty.
	client.Del(ctx, siteMetaCacheKey("fresh"))

	querier := &mockQuerier{
		sites: map[string]*siteMetadata{
			"fresh": {DBSchema: "tenant_fresh", Status: "active"},
		},
	}
	counting := &countingQuerier{inner: querier}
	pools := &mockPoolProvider{pools: map[string]*pgxpool.Pool{"fresh": nil}}
	resolver := newTestResolver(counting, pools, client)

	site, err := resolver.ResolveSite(ctx, "fresh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if counting.calls != 1 {
		t.Errorf("DB querier called %d times, want 1 (cache miss)", counting.calls)
	}
	if site.Name != "fresh" {
		t.Errorf("Name = %q, want %q", site.Name, "fresh")
	}

	// Verify cache was written.
	data, err := client.Get(ctx, siteMetaCacheKey("fresh")).Bytes()
	if err != nil {
		t.Fatalf("cache not written: %v", err)
	}
	defer client.Del(ctx, siteMetaCacheKey("fresh"))

	var cached siteMetadata
	if err := json.Unmarshal(data, &cached); err != nil {
		t.Fatalf("unmarshal cached data: %v", err)
	}
	if cached.Status != "active" {
		t.Errorf("cached status = %q, want %q", cached.Status, "active")
	}
}

func TestDBSiteResolver_RedisUnavailable(t *testing.T) {
	// Point Redis at an unreachable address to simulate unavailability.
	client := redis.NewClient(&redis.Options{Addr: "localhost:19999"})
	defer func() { _ = client.Close() }()

	querier := &mockQuerier{
		sites: map[string]*siteMetadata{
			"fallback": {DBSchema: "tenant_fallback", Status: "active"},
		},
	}
	pools := &mockPoolProvider{pools: map[string]*pgxpool.Pool{"fallback": nil}}
	resolver := newTestResolver(querier, pools, client)

	// Should fall back to DB despite Redis being down.
	site, err := resolver.ResolveSite(context.Background(), "fallback")
	if err != nil {
		t.Fatalf("unexpected error (should fallback to DB): %v", err)
	}
	if site.Name != "fallback" {
		t.Errorf("Name = %q, want %q", site.Name, "fallback")
	}
	if site.Status != "active" {
		t.Errorf("Status = %q, want %q", site.Status, "active")
	}
}

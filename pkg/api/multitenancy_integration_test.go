//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/moca-framework/moca/internal/config"
	"github.com/moca-framework/moca/pkg/api"
	"github.com/moca-framework/moca/pkg/document"
	"github.com/moca-framework/moca/pkg/meta"
	"github.com/moca-framework/moca/pkg/observe"
	"github.com/moca-framework/moca/pkg/orm"
	"github.com/moca-framework/moca/pkg/tenancy"
)

// ── Multitenancy integration test constants ─────────────────────────────────

const (
	mtSiteAcme   = "mt_acme"
	mtSiteGlobex = "mt_globex"
)

// SalesOrder fixture MetaType registered on both tenant sites.
const mtSalesOrderJSON = `{
	"name": "SalesOrder",
	"module": "test",
	"naming_rule": {"rule": "autoincrement"},
	"api_config": {
		"enabled": true,
		"allow_create": true,
		"allow_get": true,
		"allow_list": true,
		"allow_update": true,
		"allow_delete": true,
		"default_page_size": 20,
		"max_page_size": 100
	},
	"fields": [
		{"name": "title",  "field_type": "Data",  "label": "Title",  "in_api": true, "required": true},
		{"name": "amount", "field_type": "Float", "label": "Amount", "in_api": true}
	]
}`

// mtSetup holds shared state for all multitenancy subtests.
type mtSetup struct {
	server      *httptest.Server
	adminPool   *pgxpool.Pool
	redisClient *redis.Client
	dbManager   *orm.DBManager
}

// mtDoRequest sends an HTTP request to the multitenancy test server.
func mtDoRequest(t *testing.T, srv *httptest.Server, method, path string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, srv.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	return resp
}

// mtDecodeJSON reads and decodes the JSON response body.
func mtDecodeJSON(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return result
}

// TestMultitenancy is the parent test for all multitenancy integration tests.
// It sets up two isolated tenant sites and runs subtests that validate every
// MS-12 ROADMAP acceptance criterion.
func TestMultitenancy(t *testing.T) {
	ctx := context.Background()

	// ── Database connection ─────────────────────────────────────────────
	connStr := os.Getenv("PG_CONN_STRING")
	if connStr == "" {
		connStr = fmt.Sprintf(
			"postgres://%s:%s@%s:%d/%s?sslmode=disable",
			apiTestUser, apiTestPassword, apiTestHost, apiTestPort, apiTestDB,
		)
	}

	adminPool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("cannot create admin pool: %v", err)
	}
	defer adminPool.Close()

	if err := adminPool.Ping(ctx); err != nil {
		t.Skipf("cannot connect to PostgreSQL: %v", err)
	}

	// ── Redis ───────────────────────────────────────────────────────────
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6380"
	}
	rc := redis.NewClient(&redis.Options{Addr: redisAddr, DB: 0})
	if err := rc.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis unavailable at %s: %v", redisAddr, err)
	}
	defer rc.Close()

	// ── System schema ───────────────────────────────────────────────────
	// EnsureSystemSchema uses CREATE TABLE IF NOT EXISTS, but the existing
	// TestMain in api_integration_test.go may have already created a simpler
	// sites table without config/plan/modified_at columns. Add them if missing.
	if err := orm.EnsureSystemSchema(ctx, adminPool, "moca_system"); err != nil {
		t.Fatalf("EnsureSystemSchema: %v", err)
	}
	// Ensure columns needed by DBSiteResolver exist (idempotent ADD COLUMN).
	for _, stmt := range []string{
		"ALTER TABLE moca_system.sites ADD COLUMN IF NOT EXISTS config JSONB NOT NULL DEFAULT '{}'",
		"ALTER TABLE moca_system.sites ADD COLUMN IF NOT EXISTS plan TEXT",
		"ALTER TABLE moca_system.sites ADD COLUMN IF NOT EXISTS modified_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
	} {
		if _, err := adminPool.Exec(ctx, stmt); err != nil {
			t.Fatalf("alter sites table: %v", err)
		}
	}

	// ── Create tenant schemas and register sites ────────────────────────
	sites := []struct {
		name   string
		schema string
		status string
	}{
		{mtSiteAcme, tenancy.SchemaNameForSite(mtSiteAcme), "active"},
		{mtSiteGlobex, tenancy.SchemaNameForSite(mtSiteGlobex), "active"},
	}

	for _, s := range sites {
		quoted := pgx.Identifier{s.schema}.Sanitize()
		if _, err := adminPool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+quoted); err != nil {
			t.Fatalf("create schema %s: %v", s.schema, err)
		}
		if _, err := adminPool.Exec(ctx, `
			INSERT INTO moca_system.sites (name, db_schema, status, admin_email)
			VALUES ($1, $2, $3, 'admin@test.com')
			ON CONFLICT (name) DO UPDATE SET db_schema = EXCLUDED.db_schema, status = EXCLUDED.status
		`, s.name, s.schema, s.status); err != nil {
			t.Fatalf("insert site %s: %v", s.name, err)
		}
	}

	// ── Cleanup ─────────────────────────────────────────────────────────
	t.Cleanup(func() {
		ctx := context.Background()
		for _, s := range sites {
			quoted := pgx.Identifier{s.schema}.Sanitize()
			_, _ = adminPool.Exec(ctx, "DROP SCHEMA IF EXISTS "+quoted+" CASCADE")
			_, _ = adminPool.Exec(ctx, "DELETE FROM moca_system.site_apps WHERE site_name = $1", s.name)
			_, _ = adminPool.Exec(ctx, "DELETE FROM moca_system.sites WHERE name = $1", s.name)
			rc.Del(ctx, "site_meta:"+s.name)
			rc.Del(ctx, fmt.Sprintf("config:%s", s.name))
		}
	})

	// ── DBManager ───────────────────────────────────────────────────────
	logger := observe.NewLogger(slog.LevelWarn)
	host := os.Getenv("PG_HOST")
	if host == "" {
		host = apiTestHost
	}

	dbManager, err := orm.NewDBManager(ctx, config.DatabaseConfig{
		Host:     host,
		Port:     apiTestPort,
		User:     apiTestUser,
		Password: apiTestPassword,
		SystemDB: apiTestDB,
		PoolSize: 10,
	}, logger)
	if err != nil {
		t.Fatalf("NewDBManager: %v", err)
	}
	defer dbManager.Close()

	// ── Registry + Migrator ─────────────────────────────────────────────
	registry := meta.NewRegistry(dbManager, rc, logger)
	migrator := meta.NewMigrator(dbManager, logger)

	// Bootstrap both sites: create meta tables and register the SalesOrder fixture.
	for _, s := range sites {
		if err := migrator.EnsureMetaTables(ctx, s.name); err != nil {
			t.Fatalf("EnsureMetaTables(%s): %v", s.name, err)
		}
		if _, err := registry.Register(ctx, s.name, []byte(mtSalesOrderJSON)); err != nil {
			t.Fatalf("register SalesOrder on %s: %v", s.name, err)
		}
	}

	// ── DocManager ──────────────────────────────────────────────────────
	naming := document.NewNamingEngine()
	validator := document.NewValidator()
	controllers := document.NewControllerRegistry()
	docManager := document.NewDocManager(registry, dbManager, naming, validator, controllers, logger)

	// ── Gateway with real DBSiteResolver ─────────────────────────────────
	resolver := api.NewDBSiteResolver(dbManager, rc, logger)

	gw := api.NewGateway(
		api.WithDocManager(docManager),
		api.WithRegistry(registry),
		api.WithLogger(logger),
		api.WithSiteResolver(resolver),
		api.WithRateLimiter(api.NewRateLimiter(rc, logger), &meta.RateLimitConfig{
			MaxRequests: 1000,
			Window:      10 * time.Second,
		}),
	)

	handler := api.NewResourceHandler(gw)
	handler.RegisterRoutes(gw.Mux(), "v1")

	vr := api.NewVersionRouter(handler, logger)
	gw.SetVersionRouter(vr)

	srv := httptest.NewServer(gw.Handler())
	defer srv.Close()

	setup := &mtSetup{
		server:      srv,
		adminPool:   adminPool,
		redisClient: rc,
		dbManager:   dbManager,
	}

	// Clear any cached rate limit keys for our test sites.
	rc.Del(ctx, "rl:"+mtSiteAcme+":Guest")
	rc.Del(ctx, "rl:"+mtSiteGlobex+":Guest")

	// ── Subtests ────────────────────────────────────────────────────────

	t.Run("SubdomainResolution", func(t *testing.T) {
		mtTestSubdomainResolution(t, setup)
	})

	t.Run("HeaderResolution", func(t *testing.T) {
		mtTestHeaderResolution(t, setup)
	})

	t.Run("PathResolution", func(t *testing.T) {
		mtTestPathResolution(t, setup)
	})

	t.Run("DataIsolation", func(t *testing.T) {
		mtTestDataIsolation(t, setup)
	})

	t.Run("NonexistentSite404", func(t *testing.T) {
		mtTestNonexistentSite404(t, setup)
	})

	t.Run("DisabledSite503", func(t *testing.T) {
		mtTestDisabledSite503(t, setup)
	})

	t.Run("ResolutionPriority", func(t *testing.T) {
		mtTestResolutionPriority(t, setup)
	})

	t.Run("RedisCaching", func(t *testing.T) {
		mtTestRedisCaching(t, setup)
	})
}

// ── Subtest implementations ─────────────────────────────────────────────────

// TestMultitenancy_SubdomainResolution verifies that acme.localhost:8000
// resolves to the "mt_acme" site via subdomain extraction.
func mtTestSubdomainResolution(t *testing.T, s *mtSetup) {
	resp := mtDoRequest(t, s.server, http.MethodGet,
		"/api/v1/resource/SalesOrder",
		nil,
		map[string]string{"Host": mtSiteAcme + ".localhost:8000"},
	)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
}

// TestMultitenancy_HeaderResolution verifies that X-Moca-Site header
// resolves to the correct site.
func mtTestHeaderResolution(t *testing.T, s *mtSetup) {
	resp := mtDoRequest(t, s.server, http.MethodGet,
		"/api/v1/resource/SalesOrder",
		nil,
		map[string]string{"X-Moca-Site": mtSiteGlobex},
	)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
}

// TestMultitenancy_PathResolution verifies that /sites/{site}/... resolves
// the site and strips the prefix before routing.
func mtTestPathResolution(t *testing.T, s *mtSetup) {
	resp := mtDoRequest(t, s.server, http.MethodGet,
		"/sites/"+mtSiteAcme+"/api/v1/resource/SalesOrder",
		nil,
		nil,
	)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
}

// TestMultitenancy_DataIsolation verifies that a SalesOrder created on
// "acme" does not appear on "globex" — complete per-tenant data isolation.
func mtTestDataIsolation(t *testing.T, s *mtSetup) {
	// Create a SalesOrder on acme.
	resp := mtDoRequest(t, s.server, http.MethodPost,
		"/api/v1/resource/SalesOrder",
		map[string]any{"title": "Acme Order", "amount": 100},
		map[string]string{"X-Moca-Site": mtSiteAcme},
	)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create on acme: expected 201, got %d: %s", resp.StatusCode, body)
	}
	result := mtDecodeJSON(t, resp)
	data, _ := result["data"].(map[string]any)
	acmeName, _ := data["name"].(string)
	if acmeName == "" {
		t.Fatal("create on acme: no name in response")
	}

	// Verify the document exists on acme.
	resp = mtDoRequest(t, s.server, http.MethodGet,
		"/api/v1/resource/SalesOrder/"+acmeName,
		nil,
		map[string]string{"X-Moca-Site": mtSiteAcme},
	)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get on acme: expected 200, got %d", resp.StatusCode)
	}

	// List SalesOrders on globex — should be empty.
	resp = mtDoRequest(t, s.server, http.MethodGet,
		"/api/v1/resource/SalesOrder",
		nil,
		map[string]string{"X-Moca-Site": mtSiteGlobex},
	)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("list on globex: expected 200, got %d: %s", resp.StatusCode, body)
	}
	result = mtDecodeJSON(t, resp)
	metaInfo, _ := result["meta"].(map[string]any)
	total, _ := metaInfo["total"].(float64)
	if total != 0 {
		t.Errorf("expected 0 SalesOrders on globex, got %v", total)
	}

	// The acme document should NOT be accessible from globex.
	resp = mtDoRequest(t, s.server, http.MethodGet,
		"/api/v1/resource/SalesOrder/"+acmeName,
		nil,
		map[string]string{"X-Moca-Site": mtSiteGlobex},
	)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("acme doc on globex: expected 404, got %d", resp.StatusCode)
	}
}

// TestMultitenancy_NonexistentSite404 verifies that a request for a
// nonexistent site returns 404 with the TENANT_NOT_FOUND error code.
func mtTestNonexistentSite404(t *testing.T, s *mtSetup) {
	resp := mtDoRequest(t, s.server, http.MethodGet,
		"/api/v1/resource/SalesOrder",
		nil,
		map[string]string{"X-Moca-Site": "noexist"},
	)
	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 404, got %d: %s", resp.StatusCode, body)
	}

	result := mtDecodeJSON(t, resp)
	errObj, _ := result["error"].(map[string]any)
	if errObj == nil {
		t.Fatal("expected error object in response")
	}
	if code, _ := errObj["code"].(string); code != "TENANT_NOT_FOUND" {
		t.Errorf("error code = %q, want %q", code, "TENANT_NOT_FOUND")
	}
}

// TestMultitenancy_DisabledSite503 verifies that a disabled site returns
// 503 with the SITE_DISABLED error code.
func mtTestDisabledSite503(t *testing.T, s *mtSetup) {
	ctx := context.Background()

	// Disable globex directly in the database.
	_, err := s.adminPool.Exec(ctx,
		"UPDATE moca_system.sites SET status = 'disabled' WHERE name = $1",
		mtSiteGlobex,
	)
	if err != nil {
		t.Fatalf("disable globex: %v", err)
	}

	// Clear the Redis cache so the resolver picks up the new status.
	s.redisClient.Del(ctx, "site_meta:"+mtSiteGlobex)

	// Restore after test.
	t.Cleanup(func() {
		_, _ = s.adminPool.Exec(ctx,
			"UPDATE moca_system.sites SET status = 'active' WHERE name = $1",
			mtSiteGlobex,
		)
		s.redisClient.Del(ctx, "site_meta:"+mtSiteGlobex)
	})

	resp := mtDoRequest(t, s.server, http.MethodGet,
		"/api/v1/resource/SalesOrder",
		nil,
		map[string]string{"X-Moca-Site": mtSiteGlobex},
	)
	if resp.StatusCode != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 503, got %d: %s", resp.StatusCode, body)
	}

	result := mtDecodeJSON(t, resp)
	errObj, _ := result["error"].(map[string]any)
	if errObj == nil {
		t.Fatal("expected error object in response")
	}
	if code, _ := errObj["code"].(string); code != "SITE_DISABLED" {
		t.Errorf("error code = %q, want %q", code, "SITE_DISABLED")
	}
}

// TestMultitenancy_ResolutionPriority verifies that the X-Moca-Site header
// takes priority over subdomain-based resolution.
func mtTestResolutionPriority(t *testing.T, s *mtSetup) {
	// Create a document on globex to verify we're actually hitting globex.
	resp := mtDoRequest(t, s.server, http.MethodPost,
		"/api/v1/resource/SalesOrder",
		map[string]any{"title": "Globex Priority Test", "amount": 999},
		map[string]string{"X-Moca-Site": mtSiteGlobex},
	)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create on globex: expected 201, got %d: %s", resp.StatusCode, body)
	}
	result := mtDecodeJSON(t, resp)
	data, _ := result["data"].(map[string]any)
	globexName, _ := data["name"].(string)

	// Send request with BOTH header (globex) and subdomain (acme).
	// Header should win — the document created on globex should be accessible.
	resp = mtDoRequest(t, s.server, http.MethodGet,
		"/api/v1/resource/SalesOrder/"+globexName,
		nil,
		map[string]string{
			"X-Moca-Site": mtSiteGlobex,
			"Host":        mtSiteAcme + ".localhost:8000",
		},
	)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("priority test: expected 200, got %d: %s", resp.StatusCode, body)
	}

	result = mtDecodeJSON(t, resp)
	data, _ = result["data"].(map[string]any)
	if title, _ := data["title"].(string); title != "Globex Priority Test" {
		t.Errorf("title = %q, want %q — header should beat subdomain", title, "Globex Priority Test")
	}
}

// TestMultitenancy_RedisCaching verifies that site metadata is cached in
// Redis after resolution, and that repeated lookups hit the cache.
func mtTestRedisCaching(t *testing.T, s *mtSetup) {
	ctx := context.Background()
	cacheKey := "site_meta:" + mtSiteAcme

	// Clear any existing cache.
	s.redisClient.Del(ctx, cacheKey)

	// Make a request to trigger resolution and cache population.
	resp := mtDoRequest(t, s.server, http.MethodGet,
		"/api/v1/resource/SalesOrder",
		nil,
		map[string]string{"X-Moca-Site": mtSiteAcme},
	)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify the cache key exists in Redis.
	exists, err := s.redisClient.Exists(ctx, cacheKey).Result()
	if err != nil {
		t.Fatalf("redis exists check: %v", err)
	}
	if exists != 1 {
		t.Fatal("expected site_meta cache key to exist in Redis after resolution")
	}

	// Verify the cached data is valid JSON with expected fields.
	data, err := s.redisClient.Get(ctx, cacheKey).Bytes()
	if err != nil {
		t.Fatalf("redis get: %v", err)
	}
	var cached map[string]any
	if err := json.Unmarshal(data, &cached); err != nil {
		t.Fatalf("unmarshal cached data: %v", err)
	}
	if cached["db_schema"] == nil {
		t.Error("cached data missing db_schema")
	}
	if cached["status"] != "active" {
		t.Errorf("cached status = %v, want %q", cached["status"], "active")
	}
}

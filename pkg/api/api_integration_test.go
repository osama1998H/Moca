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

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/pkg/api"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/observe"
	"github.com/osama1998H/moca/pkg/orm"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// ── connection defaults ──────────────────────────────────────────────────────

const (
	apiTestHost     = "localhost"
	apiTestPort     = 5433
	apiTestUser     = "moca"
	apiTestPassword = "moca_test"
	apiTestDB       = "moca_test"
	apiTestSchema   = "tenant_api_integ"
	apiSiteName     = "api_integ"
)

// ── shared test infrastructure ───────────────────────────────────────────────

var (
	apiTestServer  *httptest.Server
	apiTestPool    *pgxpool.Pool // direct pool for audit log queries
	apiRedisClient *redis.Client
)

// staticSiteResolver always returns the pre-built test SiteContext.
type staticSiteResolver struct {
	site *tenancy.SiteContext
}

func (r *staticSiteResolver) ResolveSite(_ context.Context, siteID string) (*tenancy.SiteContext, error) {
	if siteID == apiSiteName {
		return r.site, nil
	}
	return nil, fmt.Errorf("unknown site %q", siteID)
}

// TestMain sets up the full API integration test infrastructure.
func TestMain(m *testing.M) {
	connStr := os.Getenv("PG_CONN_STRING")
	if connStr == "" {
		connStr = fmt.Sprintf(
			"postgres://%s:%s@%s:%d/%s?sslmode=disable",
			apiTestUser, apiTestPassword,
			apiTestHost, apiTestPort, apiTestDB,
		)
	}

	ctx := context.Background()

	// Admin pool for schema setup/teardown.
	adminPool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot create admin pool: %v\n", err)
		fmt.Fprintf(os.Stderr, "  Start PostgreSQL: docker compose up -d\n")
		os.Exit(0)
	}
	defer adminPool.Close()

	if err := adminPool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot connect to PostgreSQL: %v\n", err)
		os.Exit(0)
	}

	// Create test schema.
	schema := pgx.Identifier{apiTestSchema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+schema); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: create schema: %v\n", err)
		os.Exit(1)
	}

	// Create moca_system schema + sites table.
	if _, err := adminPool.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS moca_system;
		CREATE TABLE IF NOT EXISTS moca_system.sites (
			name        TEXT PRIMARY KEY,
			db_schema   TEXT NOT NULL,
			status      TEXT NOT NULL DEFAULT 'active',
			admin_email TEXT NOT NULL,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: create moca_system: %v\n", err)
		os.Exit(1)
	}

	// Insert site row.
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO moca_system.sites (name, db_schema, admin_email)
		VALUES ($1, $2, 'test@test.dev')
		ON CONFLICT (name) DO UPDATE SET db_schema = EXCLUDED.db_schema
	`, apiSiteName, apiTestSchema); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: insert site row: %v\n", err)
		os.Exit(1)
	}

	logger := observe.NewLogger(slog.LevelWarn)

	// Create DBManager.
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
		fmt.Fprintf(os.Stderr, "FATAL: NewDBManager: %v\n", err)
		os.Exit(1)
	}
	defer dbManager.Close()

	// Probe Redis.
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6380"
	}
	rc := redis.NewClient(&redis.Options{Addr: redisAddr, DB: 0})
	if err := rc.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "INFO: Redis unavailable at %s: %v — rate limit tests will be skipped\n", redisAddr, err)
		rc.Close()
		rc = nil
	} else {
		apiRedisClient = rc
		defer func() { apiRedisClient.Close(); apiRedisClient = nil }()
	}

	// Create Registry.
	registry := meta.NewRegistry(dbManager, apiRedisClient, logger)

	// EnsureMetaTables (tab_doctype, tab_singles, tab_outbox, tab_audit_log, tab_version).
	migrator := meta.NewMigrator(dbManager, logger)
	if err := migrator.EnsureMetaTables(ctx, apiSiteName); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: EnsureMetaTables: %v\n", err)
		os.Exit(1)
	}

	// Register fixture MetaTypes.
	for _, js := range []string{apiTestItemJSON, apiNoDeleteJSON} {
		if _, err := registry.Register(ctx, apiSiteName, []byte(js)); err != nil {
			fmt.Fprintf(os.Stderr, "FATAL: register fixture: %v\n", err)
			os.Exit(1)
		}
	}

	// Create DocManager.
	naming := document.NewNamingEngine()
	validator := document.NewValidator()
	controllers := document.NewControllerRegistry()
	docManager := document.NewDocManager(registry, dbManager, naming, validator, controllers, logger)

	// Build SiteContext.
	sitePool, err := dbManager.ForSite(ctx, apiSiteName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: ForSite: %v\n", err)
		os.Exit(1)
	}
	apiTestPool = sitePool

	site := &tenancy.SiteContext{
		Name: apiSiteName,
		Pool: sitePool,
	}

	// Build Gateway.
	opts := []api.GatewayOption{
		api.WithDocManager(docManager),
		api.WithRegistry(registry),
		api.WithLogger(logger),
		api.WithSiteResolver(&staticSiteResolver{site: site}),
	}

	// Add rate limiter if Redis available.
	// Use a high limit for the default gateway so test setup (createDoc calls)
	// doesn't exhaust it. TestRateLimiting uses a dedicated low-limit server.
	if apiRedisClient != nil {
		rl := api.NewRateLimiter(apiRedisClient, logger)
		opts = append(opts, api.WithRateLimiter(rl, &meta.RateLimitConfig{
			MaxRequests: 1000,
			Window:      10 * time.Second,
		}))
	}

	gw := api.NewGateway(opts...)

	handler := api.NewResourceHandler(gw)
	handler.RegisterRoutes(gw.Mux(), "v1")

	vr := api.NewVersionRouter(handler, logger)
	gw.SetVersionRouter(vr)

	apiTestServer = httptest.NewServer(gw.Handler())
	defer apiTestServer.Close()

	exitCode := m.Run()

	// Teardown: drop schema.
	if _, err := adminPool.Exec(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
		fmt.Fprintf(os.Stderr, "teardown warning: drop schema: %v\n", err)
	}

	os.Exit(exitCode)
}

// ── Fixture MetaTypes ────────────────────────────────────────────────────────

const apiTestItemJSON = `{
	"name": "APITestItem",
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
		{"name": "title",         "field_type": "Data",   "label": "Title",         "in_api": true, "required": true},
		{"name": "amount",        "field_type": "Float",  "label": "Amount",        "in_api": true},
		{"name": "status",        "field_type": "Select", "label": "Status",        "in_api": true, "options": "Draft\nSubmitted\nCancelled", "default_value": "Draft"},
		{"name": "internal_code", "field_type": "Data",   "label": "Internal Code", "in_api": false},
		{"name": "ext_id",        "field_type": "Data",   "label": "External ID",   "in_api": true, "api_alias": "external_id"},
		{"name": "auto_stamp",    "field_type": "Data",   "label": "Auto Stamp",    "in_api": true, "api_read_only": true}
	]
}`

const apiNoDeleteJSON = `{
	"name": "APINoDelete",
	"module": "test",
	"naming_rule": {"rule": "autoincrement"},
	"api_config": {
		"enabled": true,
		"allow_create": true,
		"allow_get": true,
		"allow_list": true,
		"allow_update": true,
		"allow_delete": false,
		"default_page_size": 20,
		"max_page_size": 100
	},
	"fields": [
		{"name": "title", "field_type": "Data", "label": "Title", "in_api": true}
	]
}`

// ── Test helpers ─────────────────────────────────────────────────────────────

// doRequest sends an HTTP request with the test site header.
func doRequest(t *testing.T, method, url string, body any) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("X-Moca-Site", apiSiteName)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, url, err)
	}
	return resp
}

// decodeJSON reads and decodes the JSON response body into a map.
func decodeJSON(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return result
}

// createDoc creates a document via POST and returns its name.
func createDoc(t *testing.T, doctype string, values map[string]any) string {
	t.Helper()
	resp := doRequest(t, http.MethodPost, apiTestServer.URL+"/api/v1/resource/"+doctype, values)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("createDoc %s: expected 201, got %d: %s", doctype, resp.StatusCode, body)
	}
	result := decodeJSON(t, resp)
	data, _ := result["data"].(map[string]any)
	name, _ := data["name"].(string)
	if name == "" {
		t.Fatalf("createDoc %s: no name in response: %v", doctype, result)
	}
	return name
}

// flushRateLimit removes the rate limit key for the test user/site.
func flushRateLimit(t *testing.T) {
	t.Helper()
	if apiRedisClient == nil {
		return
	}
	// The rate limit key format is "rl:{site}:{user}" where user is "Guest"
	// from NoopAuthenticator.
	apiRedisClient.Del(context.Background(), "rl:api_integ:Guest")
}

// queryAuditLog checks whether an audit log entry exists.
func queryAuditLog(t *testing.T, doctype, docname, action string) bool {
	t.Helper()
	var count int
	err := apiTestPool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM tab_audit_log WHERE "doctype" = $1 AND "docname" = $2 AND "action" = $3`,
		doctype, docname, action,
	).Scan(&count)
	if err != nil {
		t.Fatalf("queryAuditLog(%q, %q, %q): %v", doctype, docname, action, err)
	}
	return count > 0
}

// ── Integration Tests ────────────────────────────────────────────────────────

func TestCreateDocument(t *testing.T) {
	flushRateLimit(t)

	resp := doRequest(t, http.MethodPost, apiTestServer.URL+"/api/v1/resource/APITestItem", map[string]any{
		"title":  "Test Create",
		"amount": 42.5,
	})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, body)
	}

	result := decodeJSON(t, resp)
	data, _ := result["data"].(map[string]any)
	if data == nil {
		t.Fatal("expected data in response")
	}
	if name, _ := data["name"].(string); name == "" {
		t.Error("expected non-empty name in response")
	}
	if title, _ := data["title"].(string); title != "Test Create" {
		t.Errorf("title = %q, want %q", title, "Test Create")
	}
}

func TestGetDocument(t *testing.T) {
	flushRateLimit(t)

	name := createDoc(t, "APITestItem", map[string]any{
		"title":  "Test Get",
		"amount": 99.9,
	})

	resp := doRequest(t, http.MethodGet, apiTestServer.URL+"/api/v1/resource/APITestItem/"+name, nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	result := decodeJSON(t, resp)
	data, _ := result["data"].(map[string]any)
	if data == nil {
		t.Fatal("expected data in response")
	}
	if title, _ := data["title"].(string); title != "Test Get" {
		t.Errorf("title = %q, want %q", title, "Test Get")
	}
	if amount, _ := data["amount"].(float64); amount != 99.9 {
		t.Errorf("amount = %v, want %v", amount, 99.9)
	}
}

func TestListDocuments(t *testing.T) {
	flushRateLimit(t)

	// Create 3 documents.
	for i := 0; i < 3; i++ {
		createDoc(t, "APITestItem", map[string]any{
			"title":  fmt.Sprintf("List Item %d", i),
			"amount": float64(i * 10),
		})
	}

	resp := doRequest(t, http.MethodGet, apiTestServer.URL+"/api/v1/resource/APITestItem?limit=2", nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	result := decodeJSON(t, resp)
	data, _ := result["data"].([]any)
	if len(data) != 2 {
		t.Errorf("expected 2 items, got %d", len(data))
	}
	metaInfo, _ := result["meta"].(map[string]any)
	if metaInfo == nil {
		t.Fatal("expected meta in response")
	}
	total, _ := metaInfo["total"].(float64)
	if total < 3 {
		t.Errorf("expected total >= 3, got %v", total)
	}
}

func TestUpdateDocument(t *testing.T) {
	flushRateLimit(t)

	name := createDoc(t, "APITestItem", map[string]any{
		"title":  "Before Update",
		"amount": 10,
	})

	resp := doRequest(t, http.MethodPut, apiTestServer.URL+"/api/v1/resource/APITestItem/"+name, map[string]any{
		"title": "After Update",
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	result := decodeJSON(t, resp)
	data, _ := result["data"].(map[string]any)
	if title, _ := data["title"].(string); title != "After Update" {
		t.Errorf("title = %q, want %q", title, "After Update")
	}
}

func TestDeleteDocument(t *testing.T) {
	flushRateLimit(t)

	name := createDoc(t, "APITestItem", map[string]any{
		"title": "To Delete",
	})

	// DELETE.
	resp := doRequest(t, http.MethodDelete, apiTestServer.URL+"/api/v1/resource/APITestItem/"+name, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	// GET should return 404.
	resp = doRequest(t, http.MethodGet, apiTestServer.URL+"/api/v1/resource/APITestItem/"+name, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", resp.StatusCode)
	}
}

func TestFieldExclusion(t *testing.T) {
	flushRateLimit(t)

	name := createDoc(t, "APITestItem", map[string]any{
		"title":         "Field Exclusion Test",
		"internal_code": "SECRET-123",
	})

	resp := doRequest(t, http.MethodGet, apiTestServer.URL+"/api/v1/resource/APITestItem/"+name, nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	result := decodeJSON(t, resp)
	data, _ := result["data"].(map[string]any)
	if _, exists := data["internal_code"]; exists {
		t.Error("internal_code (in_api: false) should not appear in API response")
	}
}

func TestAliasMapping(t *testing.T) {
	flushRateLimit(t)

	// POST with alias name.
	name := createDoc(t, "APITestItem", map[string]any{
		"title":       "Alias Test",
		"external_id": "EXT-001",
	})

	// GET and verify alias is used in response.
	resp := doRequest(t, http.MethodGet, apiTestServer.URL+"/api/v1/resource/APITestItem/"+name, nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	result := decodeJSON(t, resp)
	data, _ := result["data"].(map[string]any)

	// The response should use the alias "external_id" (not the internal "ext_id").
	if val, ok := data["external_id"]; !ok {
		t.Error("expected external_id alias in response")
	} else if val != "EXT-001" {
		t.Errorf("external_id = %v, want %q", val, "EXT-001")
	}
}

func TestReadOnlyEnforcement(t *testing.T) {
	flushRateLimit(t)

	name := createDoc(t, "APITestItem", map[string]any{
		"title": "ReadOnly Test",
	})

	// Try to update a read-only field.
	resp := doRequest(t, http.MethodPut, apiTestServer.URL+"/api/v1/resource/APITestItem/"+name, map[string]any{
		"title":      "Updated Title",
		"auto_stamp": "HACKED",
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	// GET and verify auto_stamp was not overwritten.
	resp = doRequest(t, http.MethodGet, apiTestServer.URL+"/api/v1/resource/APITestItem/"+name, nil)
	result := decodeJSON(t, resp)
	data, _ := result["data"].(map[string]any)

	if stamp, _ := data["auto_stamp"].(string); stamp == "HACKED" {
		t.Error("auto_stamp (api_read_only) should not be writable via API")
	}
}

func TestRateLimiting(t *testing.T) {
	if apiRedisClient == nil {
		t.Skip("Redis unavailable — skipping rate limit test")
	}

	// Use a dedicated rate limit key by flushing before this test.
	flushRateLimit(t)

	// Build a dedicated server with a very low rate limit (MaxRequests=3).
	rl := api.NewRateLimiter(apiRedisClient, observe.NewLogger(slog.LevelWarn))
	gw := api.NewGateway(
		api.WithDocManager(nil), // not needed — we only hit rate limit
		api.WithLogger(observe.NewLogger(slog.LevelWarn)),
		api.WithSiteResolver(&staticSiteResolver{site: &tenancy.SiteContext{Name: apiSiteName}}),
		api.WithRateLimiter(rl, &meta.RateLimitConfig{
			MaxRequests: 3,
			Window:      10 * time.Second,
		}),
	)
	// Register a dummy handler so requests pass through middleware.
	gw.Mux().HandleFunc("GET /api/v1/resource/{doctype}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(gw.Handler())
	defer srv.Close()

	url := srv.URL + "/api/v1/resource/APITestItem"
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-Moca-Site", apiSiteName)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			t.Fatalf("request %d unexpectedly rate limited", i+1)
		}
	}

	// 4th request should be denied.
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Moca-Site", apiSiteName)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request 4: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected 429 on 4th request, got %d", resp.StatusCode)
	}
	if ra := resp.Header.Get("Retry-After"); ra == "" {
		t.Error("expected Retry-After header on 429 response")
	}
}

func TestAuditLog(t *testing.T) {
	flushRateLimit(t)

	name := createDoc(t, "APITestItem", map[string]any{
		"title": "Audit Test",
	})

	// Update.
	resp := doRequest(t, http.MethodPut, apiTestServer.URL+"/api/v1/resource/APITestItem/"+name, map[string]any{
		"title": "Audit Updated",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: expected 200, got %d", resp.StatusCode)
	}

	// Delete.
	resp = doRequest(t, http.MethodDelete, apiTestServer.URL+"/api/v1/resource/APITestItem/"+name, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", resp.StatusCode)
	}

	// Verify audit log entries.
	if !queryAuditLog(t, "APITestItem", name, "Create") {
		t.Error("expected Create audit log entry")
	}
	if !queryAuditLog(t, "APITestItem", name, "Update") {
		t.Error("expected Update audit log entry")
	}
	if !queryAuditLog(t, "APITestItem", name, "Delete") {
		t.Error("expected Delete audit log entry")
	}
}

func TestMethodNotAllowed(t *testing.T) {
	flushRateLimit(t)

	// Create a doc in APINoDelete first.
	name := createDoc(t, "APINoDelete", map[string]any{
		"title": "No Delete Test",
	})

	resp := doRequest(t, http.MethodDelete, apiTestServer.URL+"/api/v1/resource/APINoDelete/"+name, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestMetaEndpoint(t *testing.T) {
	flushRateLimit(t)

	resp := doRequest(t, http.MethodGet, apiTestServer.URL+"/api/v1/meta/APITestItem", nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	result := decodeJSON(t, resp)
	data, _ := result["data"].(map[string]any)
	if data == nil {
		t.Fatal("expected data in response")
	}
	if name, _ := data["name"].(string); name != "APITestItem" {
		t.Errorf("name = %q, want %q", name, "APITestItem")
	}

	// Verify fields array exists.
	fields, _ := data["fields"].([]any)
	if len(fields) == 0 {
		t.Fatal("expected non-empty fields array")
	}

	// Verify internal_code is not in the meta fields (filtered by FieldFilter).
	for _, f := range fields {
		fd, _ := f.(map[string]any)
		if fd["name"] == "internal_code" {
			t.Error("internal_code (in_api: false) should not appear in meta endpoint")
		}
	}

	// Verify allow flags.
	if ac, _ := data["allow_create"].(bool); !ac {
		t.Error("expected allow_create = true")
	}
}

func TestNotFound(t *testing.T) {
	flushRateLimit(t)

	resp := doRequest(t, http.MethodGet, apiTestServer.URL+"/api/v1/resource/NonExistentType/anything", nil)
	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Errorf("expected 404, got %d: %s", resp.StatusCode, body)
		return
	}

	result := decodeJSON(t, resp)
	errObj, _ := result["error"].(map[string]any)
	if errObj == nil {
		t.Fatal("expected error object in response")
	}
	if code, _ := errObj["code"].(string); code != "DOCTYPE_NOT_FOUND" {
		t.Errorf("error code = %q, want %q", code, "DOCTYPE_NOT_FOUND")
	}
}

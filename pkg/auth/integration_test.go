//go:build integration

package auth_test

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
	"golang.org/x/crypto/bcrypt"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/pkg/api"
	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/observe"
	"github.com/osama1998H/moca/pkg/orm"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// ── connection defaults ──────────────────────────────────────────────────────

const (
	authTestHost     = "localhost"
	authTestPort     = 5433
	authTestUser     = "moca"
	authTestPassword = "moca_test"
	authTestDB       = "moca_test"
	authTestSchema   = "tenant_auth_integ"
	authSiteName     = "auth_integ"
)

// ── test users ───────────────────────────────────────────────────────────────

var (
	salesUserPassword, _  = bcrypt.GenerateFromPassword([]byte("sales123"), bcrypt.MinCost)
	adminUserPassword, _  = bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.MinCost)
	sales2UserPassword, _ = bcrypt.GenerateFromPassword([]byte("sales2pw"), bcrypt.MinCost)

	testUsers = map[string]struct {
		user         *auth.User
		passwordHash string
	}{
		"alice@acme.com": {
			user: &auth.User{
				Email:        "alice@acme.com",
				FullName:     "Alice",
				Roles:        []string{"Sales User"},
				UserDefaults: map[string]string{"company": "Acme Corp"},
			},
			passwordHash: string(salesUserPassword),
		},
		"bob@beta.com": {
			user: &auth.User{
				Email:        "bob@beta.com",
				FullName:     "Bob",
				Roles:        []string{"Sales User"},
				UserDefaults: map[string]string{"company": "Beta Inc"},
			},
			passwordHash: string(sales2UserPassword),
		},
		"admin@example.com": {
			user: &auth.User{
				Email:        "admin@example.com",
				FullName:     "Admin",
				Roles:        []string{"Administrator"},
				UserDefaults: map[string]string{"company": "Acme Corp"},
			},
			passwordHash: string(adminUserPassword),
		},
	}
)

// mockUserLoader returns a UserLoadFunc that resolves from testUsers.
func mockUserLoader() auth.UserLoadFunc {
	return func(_ context.Context, _ *tenancy.SiteContext, email string) (*auth.User, string, error) {
		u, ok := testUsers[email]
		if !ok {
			return nil, "", auth.ErrUserNotFound
		}
		return u.user, u.passwordHash, nil
	}
}

// ── shared infrastructure ────────────────────────────────────────────────────

var (
	authTestServer *httptest.Server
	authSitePool   *pgxpool.Pool
	authJWTCfg     auth.JWTConfig
)

// staticAuthSiteResolver implements api.SiteResolver.
type staticAuthSiteResolver struct {
	site *tenancy.SiteContext
}

func (r *staticAuthSiteResolver) ResolveSite(_ context.Context, siteID string) (*tenancy.SiteContext, error) {
	if siteID == authSiteName {
		return r.site, nil
	}
	return nil, fmt.Errorf("unknown site %q", siteID)
}

// fixture MetaType with permissions and row-level matching.
const authTestDocJSON = `{
	"name": "AuthTestOrder",
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
	"permissions": [
		{
			"role": "Sales User",
			"doctype_perm": 7,
			"match_field": "company",
			"match_value": "company",
			"field_level_read": ["title", "company", "amount"]
		},
		{
			"role": "Administrator",
			"doctype_perm": 127
		}
	],
	"fields": [
		{"name": "title",    "field_type": "Data",  "label": "Title",   "in_api": true, "required": true},
		{"name": "company",  "field_type": "Data",  "label": "Company", "in_api": true},
		{"name": "amount",   "field_type": "Float", "label": "Amount",  "in_api": true},
		{"name": "internal", "field_type": "Data",  "label": "Internal","in_api": true}
	]
}`

func TestMain(m *testing.M) {
	connStr := os.Getenv("PG_CONN_STRING")
	if connStr == "" {
		connStr = fmt.Sprintf(
			"postgres://%s:%s@%s:%d/%s?sslmode=disable",
			authTestUser, authTestPassword,
			authTestHost, authTestPort, authTestDB,
		)
	}

	ctx := context.Background()

	// Admin pool.
	adminPool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot create admin pool: %v\n", err)
		os.Exit(0)
	}
	defer adminPool.Close()

	if err := adminPool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot connect to PostgreSQL: %v\n", err)
		os.Exit(0)
	}

	schema := pgx.Identifier{authTestSchema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+schema); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: create schema: %v\n", err)
		os.Exit(1)
	}

	if _, err := adminPool.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS moca_system;
		CREATE TABLE IF NOT EXISTS moca_system.sites (
			name        TEXT PRIMARY KEY,
			db_schema   TEXT NOT NULL,
			status      TEXT NOT NULL DEFAULT 'active',
			admin_email TEXT NOT NULL DEFAULT '',
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: create moca_system: %v\n", err)
		os.Exit(1)
	}

	if _, err := adminPool.Exec(ctx, `
		INSERT INTO moca_system.sites (name, db_schema)
		VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE SET db_schema = EXCLUDED.db_schema
	`, authSiteName, authTestSchema); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: insert site row: %v\n", err)
		os.Exit(1)
	}

	logger := observe.NewLogger(slog.LevelWarn)

	host := os.Getenv("PG_HOST")
	if host == "" {
		host = authTestHost
	}
	dbManager, err := orm.NewDBManager(ctx, config.DatabaseConfig{
		Host:     host,
		Port:     authTestPort,
		User:     authTestUser,
		Password: authTestPassword,
		SystemDB: authTestDB,
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
		fmt.Fprintf(os.Stderr, "SKIP: Redis unavailable at %s: %v\n", redisAddr, err)
		rc.Close()
		os.Exit(0)
	}
	defer rc.Close()

	// Session Redis client (DB 2).
	sessionRC := redis.NewClient(&redis.Options{Addr: redisAddr, DB: 2})
	defer sessionRC.Close()

	// Registry.
	registry := meta.NewRegistry(dbManager, rc, logger)

	// Meta tables.
	migrator := meta.NewMigrator(dbManager, logger)
	if err := migrator.EnsureMetaTables(ctx, authSiteName); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: EnsureMetaTables: %v\n", err)
		os.Exit(1)
	}

	// Register fixture MetaType.
	if _, err := registry.Register(ctx, authSiteName, []byte(authTestDocJSON)); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: register fixture: %v\n", err)
		os.Exit(1)
	}

	// DocManager.
	naming := document.NewNamingEngine()
	validator := document.NewValidator()
	controllers := document.NewControllerRegistry()
	docManager := document.NewDocManager(registry, dbManager, naming, validator, controllers, logger)

	// Permission resolver.
	permResolver := auth.NewCachedPermissionResolver(registry, rc, nil, logger)
	docManager.SetPermResolver(permResolver)

	// Site context.
	sitePool, err := dbManager.ForSite(ctx, authSiteName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: ForSite: %v\n", err)
		os.Exit(1)
	}
	authSitePool = sitePool

	site := &tenancy.SiteContext{
		Name: authSiteName,
		Pool: sitePool,
	}

	// JWT config.
	authJWTCfg = auth.JWTConfig{
		Secret:          "test-secret-for-auth-integration",
		Issuer:          "moca-test",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 1 * time.Hour,
	}

	// Session manager.
	sessions := auth.NewSessionManager(sessionRC, 24*time.Hour)

	// Authenticator.
	siteExtractor := func(ctx context.Context) *tenancy.SiteContext {
		return api.SiteFromContext(ctx)
	}
	userLoader := auth.NewUserLoader(logger)
	authenticator := auth.NewMocaAuthenticator(authJWTCfg, sessions, userLoader, siteExtractor, logger)

	// Permission checker.
	permChecker := auth.NewRoleBasedPermChecker(permResolver, siteExtractor, logger)

	// Field-level transformer.
	fieldTransformer := api.NewFieldLevelTransformer(permResolver)

	// Build Gateway.
	gw := api.NewGateway(
		api.WithDocManager(docManager),
		api.WithRegistry(registry),
		api.WithLogger(logger),
		api.WithSiteResolver(&staticAuthSiteResolver{site: site}),
		api.WithAuthenticator(authenticator),
		api.WithPermissionChecker(permChecker),
		api.WithFieldLevelTransformer(fieldTransformer),
	)

	handler := api.NewResourceHandler(gw)
	handler.RegisterRoutes(gw.Mux(), "v1")

	// Auth endpoints use a mock user loader (bypasses tabUser).
	authHandler := api.NewAuthHandlerWithLoader(authJWTCfg, sessions, mockUserLoader(), logger)
	authHandler.RegisterRoutes(gw.Mux(), "v1")

	vr := api.NewVersionRouter(handler, logger)
	gw.SetVersionRouter(vr)

	authTestServer = httptest.NewServer(gw.Handler())
	defer authTestServer.Close()

	exitCode := m.Run()

	// Teardown.
	if _, err := adminPool.Exec(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
		fmt.Fprintf(os.Stderr, "teardown warning: drop schema: %v\n", err)
	}

	os.Exit(exitCode)
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func doAuthRequest(t *testing.T, method, url string, body any, token string) *http.Response {
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
	req.Header.Set("X-Moca-Site", authSiteName)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, url, err)
	}
	return resp
}

func decodeBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return result
}

func login(t *testing.T, email, password string) string {
	t.Helper()
	resp := doAuthRequest(t, http.MethodPost,
		authTestServer.URL+"/api/v1/auth/login",
		map[string]string{"email": email, "password": password}, "")
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("login %s: expected 200, got %d: %s", email, resp.StatusCode, body)
	}
	result := decodeBody(t, resp)
	data, _ := result["data"].(map[string]any)
	token, _ := data["access_token"].(string)
	if token == "" {
		t.Fatalf("login %s: no access_token in response", email)
	}
	return token
}

func createAuthDoc(t *testing.T, token string, values map[string]any) string {
	t.Helper()
	resp := doAuthRequest(t, http.MethodPost,
		authTestServer.URL+"/api/v1/resource/AuthTestOrder", values, token)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("createDoc: expected 201, got %d: %s", resp.StatusCode, body)
	}
	result := decodeBody(t, resp)
	data, _ := result["data"].(map[string]any)
	name, _ := data["name"].(string)
	if name == "" {
		t.Fatalf("createDoc: no name in response")
	}
	return name
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestAuthInteg_LoginAndCRUD(t *testing.T) {
	token := login(t, "alice@acme.com", "sales123")

	// Create.
	name := createAuthDoc(t, token, map[string]any{
		"title":   "Test Order",
		"company": "Acme Corp",
		"amount":  100.0,
	})

	// Get.
	resp := doAuthRequest(t, http.MethodGet,
		authTestServer.URL+"/api/v1/resource/AuthTestOrder/"+name, nil, token)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("GET: expected 200, got %d: %s", resp.StatusCode, body)
	}
	result := decodeBody(t, resp)
	data, _ := result["data"].(map[string]any)
	if title, _ := data["title"].(string); title != "Test Order" {
		t.Errorf("title = %q, want %q", title, "Test Order")
	}

	// Update.
	resp = doAuthRequest(t, http.MethodPut,
		authTestServer.URL+"/api/v1/resource/AuthTestOrder/"+name,
		map[string]any{"title": "Updated Order"}, token)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("PUT: expected 200, got %d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()
}

func TestAuthInteg_PermissionDenied(t *testing.T) {
	// Sales User has read|write|create (7) but NOT delete (8).
	token := login(t, "alice@acme.com", "sales123")

	name := createAuthDoc(t, token, map[string]any{
		"title":   "No Delete",
		"company": "Acme Corp",
	})

	// DELETE should return 403.
	resp := doAuthRequest(t, http.MethodDelete,
		authTestServer.URL+"/api/v1/resource/AuthTestOrder/"+name, nil, token)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("DELETE: expected 403, got %d", resp.StatusCode)
	}
}

func TestAuthInteg_FieldFiltering(t *testing.T) {
	// Sales User has field_level_read: ["title", "company", "amount"].
	// "internal" field should be stripped from response.
	token := login(t, "alice@acme.com", "sales123")

	name := createAuthDoc(t, token, map[string]any{
		"title":    "Field Filter Test",
		"company":  "Acme Corp",
		"amount":   50.0,
		"internal": "secret-data",
	})

	resp := doAuthRequest(t, http.MethodGet,
		authTestServer.URL+"/api/v1/resource/AuthTestOrder/"+name, nil, token)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("GET: expected 200, got %d: %s", resp.StatusCode, body)
	}

	result := decodeBody(t, resp)
	data, _ := result["data"].(map[string]any)

	// Should have title, company, amount + system fields.
	if _, ok := data["title"]; !ok {
		t.Error("expected title in response")
	}
	if _, ok := data["company"]; !ok {
		t.Error("expected company in response")
	}
	if _, ok := data["name"]; !ok {
		t.Error("expected name (system field) in response")
	}
	// "internal" should be filtered out by field-level read.
	if _, ok := data["internal"]; ok {
		t.Error("internal field should be filtered by field_level_read")
	}
}

func TestAuthInteg_RowFiltering(t *testing.T) {
	// Alice (Acme Corp) creates docs, Bob (Beta Inc) creates docs.
	// Each should only see their own via list.
	aliceToken := login(t, "alice@acme.com", "sales123")
	bobToken := login(t, "bob@beta.com", "sales2pw")

	// Alice creates 2 orders.
	createAuthDoc(t, aliceToken, map[string]any{
		"title": "Alice Order 1", "company": "Acme Corp",
	})
	createAuthDoc(t, aliceToken, map[string]any{
		"title": "Alice Order 2", "company": "Acme Corp",
	})

	// Bob creates 1 order.
	createAuthDoc(t, bobToken, map[string]any{
		"title": "Bob Order 1", "company": "Beta Inc",
	})

	// Alice lists — should see only her own (Acme Corp).
	resp := doAuthRequest(t, http.MethodGet,
		authTestServer.URL+"/api/v1/resource/AuthTestOrder?limit=100", nil, aliceToken)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("list: expected 200, got %d: %s", resp.StatusCode, body)
	}
	result := decodeBody(t, resp)
	items, _ := result["data"].([]any)

	for _, item := range items {
		doc, _ := item.(map[string]any)
		company, _ := doc["company"].(string)
		if company != "Acme Corp" {
			t.Errorf("Alice should only see Acme Corp docs, got company=%q", company)
		}
	}

	// Bob lists — should see only Beta Inc.
	resp = doAuthRequest(t, http.MethodGet,
		authTestServer.URL+"/api/v1/resource/AuthTestOrder?limit=100", nil, bobToken)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("list: expected 200, got %d: %s", resp.StatusCode, body)
	}
	result = decodeBody(t, resp)
	items, _ = result["data"].([]any)

	for _, item := range items {
		doc, _ := item.(map[string]any)
		company, _ := doc["company"].(string)
		if company != "Beta Inc" {
			t.Errorf("Bob should only see Beta Inc docs, got company=%q", company)
		}
	}
}

func TestAuthInteg_CrossUserIsolation(t *testing.T) {
	aliceToken := login(t, "alice@acme.com", "sales123")
	bobToken := login(t, "bob@beta.com", "sales2pw")

	// Alice creates a doc.
	name := createAuthDoc(t, aliceToken, map[string]any{
		"title": "Cross User Test", "company": "Acme Corp",
	})

	// Bob tries to GET Alice's doc → should get 404 (row-level filtering).
	resp := doAuthRequest(t, http.MethodGet,
		authTestServer.URL+"/api/v1/resource/AuthTestOrder/"+name, nil, bobToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Bob GET of Alice's doc: expected 404, got %d", resp.StatusCode)
	}
}

func TestAuthInteg_AdminBypass(t *testing.T) {
	aliceToken := login(t, "alice@acme.com", "sales123")
	adminToken := login(t, "admin@example.com", "admin123")

	// Alice creates a doc.
	name := createAuthDoc(t, aliceToken, map[string]any{
		"title": "Admin Bypass Test", "company": "Acme Corp",
	})

	// Admin can see Alice's doc (bypasses row-level filtering).
	resp := doAuthRequest(t, http.MethodGet,
		authTestServer.URL+"/api/v1/resource/AuthTestOrder/"+name, nil, adminToken)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("admin GET: expected 200, got %d: %s", resp.StatusCode, body)
	}

	result := decodeBody(t, resp)
	data, _ := result["data"].(map[string]any)

	// Admin should also see ALL fields (no field-level filtering).
	if _, ok := data["internal"]; !ok {
		t.Error("admin should see internal field (bypasses field-level filtering)")
	}
	if _, ok := data["title"]; !ok {
		t.Error("admin should see title field")
	}
}

func TestAuthInteg_GuestDenied(t *testing.T) {
	// Request without auth token → Guest user → should be denied (no Guest role in permissions).
	resp := doAuthRequest(t, http.MethodGet,
		authTestServer.URL+"/api/v1/resource/AuthTestOrder", nil, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Guest list: expected 403, got %d", resp.StatusCode)
	}
}

func TestAuthInteg_InvalidCredentials(t *testing.T) {
	resp := doAuthRequest(t, http.MethodPost,
		authTestServer.URL+"/api/v1/auth/login",
		map[string]string{"email": "alice@acme.com", "password": "wrong"}, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad login: expected 401, got %d", resp.StatusCode)
	}
}

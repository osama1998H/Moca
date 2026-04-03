//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/internal/config"
	clicontext "github.com/osama1998H/moca/internal/context"
	"github.com/osama1998H/moca/pkg/cli"
	"github.com/osama1998H/moca/pkg/orm"
)

// ── connection defaults ─────────────────────────────────────────────────────

const (
	cliTestHost     = "localhost"
	cliTestPort     = 5433
	cliTestUser     = "moca"
	cliTestPassword = "moca_test"
	cliTestDB       = "moca_test"
	cliRedisPort    = 6380
)

// ── shared infrastructure ───────────────────────────────────────────────────

var (
	cliAdminPool   *pgxpool.Pool
	cliRedisClient *redis.Client
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	host := os.Getenv("PG_HOST")
	if host == "" {
		host = cliTestHost
	}

	connStr := os.Getenv("PG_CONN_STRING")
	if connStr == "" {
		connStr = fmt.Sprintf(
			"postgres://%s:%s@%s:%d/%s?sslmode=disable",
			cliTestUser, cliTestPassword, host, cliTestPort, cliTestDB,
		)
	}

	var err error
	cliAdminPool, err = pgxpool.New(ctx, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot create admin pool: %v\n", err)
		os.Exit(0)
	}
	defer cliAdminPool.Close()

	if err := cliAdminPool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot connect to PostgreSQL: %v\n", err)
		os.Exit(0)
	}

	// Ensure system schema exists.
	if err := orm.EnsureSystemSchema(ctx, cliAdminPool, "moca_system"); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: EnsureSystemSchema: %v\n", err)
		os.Exit(1)
	}

	// Redis.
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisHost := os.Getenv("REDIS_HOST")
		if redisHost == "" {
			redisHost = "localhost"
		}
		redisAddr = fmt.Sprintf("%s:%d", redisHost, cliRedisPort)
	}
	rc := redis.NewClient(&redis.Options{Addr: redisAddr, DB: 0})
	if err := rc.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: Redis unavailable at %s: %v\n", redisAddr, err)
		rc.Close()
		os.Exit(0)
	}
	cliRedisClient = rc
	defer cliRedisClient.Close()

	exitCode := m.Run()

	// Safety-net teardown.
	rows, _ := cliAdminPool.Query(ctx,
		"SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant_cliinteg_%'")
	if rows != nil {
		for rows.Next() {
			var schema string
			if err := rows.Scan(&schema); err == nil {
				_, _ = cliAdminPool.Exec(ctx, fmt.Sprintf(
					"DROP SCHEMA IF EXISTS %s CASCADE",
					pgx.Identifier{schema}.Sanitize(),
				))
			}
		}
		rows.Close()
	}
	_, _ = cliAdminPool.Exec(ctx,
		"DELETE FROM moca_system.site_apps WHERE site_name LIKE 'cliinteg_%'")
	_, _ = cliAdminPool.Exec(ctx,
		"DELETE FROM moca_system.sites WHERE name LIKE 'cliinteg_%'")

	os.Exit(exitCode)
}

// ── helpers ─────────────────────────────────────────────────────────────────

func testProjectConfig() *config.ProjectConfig {
	host := os.Getenv("PG_HOST")
	if host == "" {
		host = cliTestHost
	}
	redisHost := os.Getenv("REDIS_HOST")
	if redisHost == "" {
		redisHost = "localhost"
	}

	return &config.ProjectConfig{
		Moca: "dev",
		Project: config.ProjectInfo{
			Name:    "integ-test",
			Version: "0.1.0",
		},
		Infrastructure: config.InfrastructureConfig{
			Database: config.DatabaseConfig{
				Driver:   "postgres",
				Host:     host,
				Port:     cliTestPort,
				User:     cliTestUser,
				Password: cliTestPassword,
				SystemDB: cliTestDB,
				PoolSize: 10,
			},
			Redis: config.RedisConfig{
				Host:      redisHost,
				Port:      cliRedisPort,
				DbCache:   0,
				DbQueue:   1,
				DbSession: 2,
				DbPubSub:  3,
			},
		},
		Apps: map[string]config.AppConfig{
			"core": {Source: "builtin", Version: "*"},
		},
	}
}

// executeWithContext executes a CLI command with an injected CLIContext.
// Returns stdout, stderr, and any error from execution.
func executeWithContext(t *testing.T, projectRoot, site string, args ...string) (string, string, error) {
	t.Helper()

	cli.ResetForTesting()
	root := cli.RootCommand()
	root.AddCommand(allCommands()...)

	// Disable normal context resolution; inject our own.
	root.PersistentPreRunE = nil

	cfg := testProjectConfig()
	cctx := &clicontext.CLIContext{
		ProjectRoot: projectRoot,
		Project:     cfg,
		Site:        site,
		Environment: "development",
	}
	ctx := clicontext.WithCLIContext(context.Background(), cctx)
	root.SetContext(ctx)

	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)

	err := root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// executeInit executes the init command without injected CLIContext (init creates the project).
func executeInit(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	cli.ResetForTesting()
	root := cli.RootCommand()
	root.AddCommand(NewInitCommand())

	// Init doesn't need a pre-existing CLIContext.
	root.PersistentPreRunE = nil

	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)

	err := root.Execute()
	return outBuf.String(), errBuf.String(), err
}

func uniqueCLISiteName(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return "cliinteg_" + hex.EncodeToString(b)
}

func cleanupCLISite(t *testing.T, name string) {
	t.Helper()
	t.Cleanup(func() {
		bgCtx := context.Background()
		schema := "tenant_" + name
		_, _ = cliAdminPool.Exec(bgCtx, fmt.Sprintf(
			"DROP SCHEMA IF EXISTS %s CASCADE",
			pgx.Identifier{schema}.Sanitize(),
		))
		_, _ = cliAdminPool.Exec(bgCtx,
			"DELETE FROM moca_system.site_apps WHERE site_name = $1", name)
		_, _ = cliAdminPool.Exec(bgCtx,
			"DELETE FROM moca_system.sites WHERE name = $1", name)
		if cliRedisClient != nil {
			cliRedisClient.Del(bgCtx, fmt.Sprintf("config:%s", name))
		}
	})
}

func cliSchemaExists(t *testing.T, schemaName string) bool {
	t.Helper()
	var count int
	err := cliAdminPool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = $1`,
		schemaName,
	).Scan(&count)
	if err != nil {
		t.Fatalf("schemaExists(%s): %v", schemaName, err)
	}
	return count > 0
}

func cliSiteRowExists(t *testing.T, siteName string) bool {
	t.Helper()
	var exists bool
	err := cliAdminPool.QueryRow(
		context.Background(),
		"SELECT EXISTS(SELECT 1 FROM moca_system.sites WHERE name = $1)",
		siteName,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("siteRowExists(%s): %v", siteName, err)
	}
	return exists
}

// ── tests ───────────────────────────────────────────────────────────────────

func TestCLI_InitCreatesProject(t *testing.T) {
	tmpDir := t.TempDir()
	targetDir := filepath.Join(tmpDir, "myproject")

	host := os.Getenv("PG_HOST")
	if host == "" {
		host = cliTestHost
	}
	redisHost := os.Getenv("REDIS_HOST")
	if redisHost == "" {
		redisHost = "localhost"
	}

	_, _, err := executeInit(t,
		"init", targetDir,
		"--db-host", host,
		"--db-port", fmt.Sprintf("%d", cliTestPort),
		"--db-user", cliTestUser,
		"--db-password", cliTestPassword,
		"--redis-host", redisHost,
		"--redis-port", fmt.Sprintf("%d", cliRedisPort),
	)
	if err != nil {
		t.Fatalf("moca init: %v", err)
	}

	// Verify moca.yaml exists.
	if _, err := os.Stat(filepath.Join(targetDir, "moca.yaml")); err != nil {
		t.Errorf("moca.yaml not found: %v", err)
	}

	// Verify moca.lock exists.
	if _, err := os.Stat(filepath.Join(targetDir, "moca.lock")); err != nil {
		t.Errorf("moca.lock not found: %v", err)
	}

	// Verify directory structure.
	for _, dir := range []string{"apps", ".moca", "sites"} {
		if _, err := os.Stat(filepath.Join(targetDir, dir)); err != nil {
			t.Errorf("directory %q not found: %v", dir, err)
		}
	}

	// Verify core app registered in the system database configured by moca init.
	projectCfg, err := config.ParseFile(filepath.Join(targetDir, "moca.yaml"))
	if err != nil {
		t.Fatalf("parse generated moca.yaml: %v", err)
	}
	systemPool, err := pgxpool.New(context.Background(), buildInitDSN(projectCfg.Infrastructure.Database))
	if err != nil {
		t.Fatalf("open system DB pool: %v", err)
	}
	defer systemPool.Close()

	var coreExists bool
	if err := systemPool.QueryRow(context.Background(),
		"SELECT EXISTS(SELECT 1 FROM moca_system.apps WHERE name = 'core')",
	).Scan(&coreExists); err != nil {
		t.Fatalf("query core app registration: %v", err)
	}
	if !coreExists {
		t.Error("core app not registered in moca_system.apps after init")
	}
}

func TestCLI_InitExistingProject_Error(t *testing.T) {
	tmpDir := t.TempDir()

	// Create an existing moca.yaml.
	if err := os.WriteFile(filepath.Join(tmpDir, "moca.yaml"), []byte("moca: dev"), 0o644); err != nil {
		t.Fatalf("write moca.yaml: %v", err)
	}

	_, _, err := executeInit(t, "init", tmpDir)
	if err == nil {
		t.Fatal("expected error for init in existing project, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestCLI_SiteWorkflow(t *testing.T) {
	tmpDir := t.TempDir()
	name := uniqueCLISiteName(t)
	cleanupCLISite(t, name)

	// 1. Site Create.
	_, _, err := executeWithContext(t, tmpDir, "",
		"site", "create", name,
		"--admin-password", "secret123",
		"--admin-email", "admin@test.dev",
	)
	if err != nil {
		t.Fatalf("site create: %v", err)
	}

	schema := "tenant_" + name
	if !cliSchemaExists(t, schema) {
		t.Fatal("schema not created after site create")
	}
	if !cliSiteRowExists(t, name) {
		t.Fatal("site not registered in moca_system after site create")
	}

	// 2. Site List (JSON mode).
	stdout, _, err := executeWithContext(t, tmpDir, "",
		"site", "list", "--json",
	)
	if err != nil {
		t.Fatalf("site list: %v", err)
	}
	if !strings.Contains(stdout, name) {
		t.Errorf("site list output does not contain %q:\n%s", name, stdout)
	}

	// 3. Site Use.
	_, _, err = executeWithContext(t, tmpDir, "",
		"site", "use", name,
	)
	if err != nil {
		t.Fatalf("site use: %v", err)
	}
	data, readErr := os.ReadFile(filepath.Join(tmpDir, ".moca", "current_site"))
	if readErr != nil {
		t.Fatalf("read current_site: %v", readErr)
	}
	if strings.TrimSpace(string(data)) != name {
		t.Errorf("current_site: got %q, want %q", strings.TrimSpace(string(data)), name)
	}

	// 4. Site Info.
	stdout, _, err = executeWithContext(t, tmpDir, name,
		"site", "info", name, "--json",
	)
	if err != nil {
		t.Fatalf("site info: %v", err)
	}
	if !strings.Contains(stdout, name) {
		t.Errorf("site info output does not contain %q:\n%s", name, stdout)
	}

	// 5. Site Drop.
	_, _, err = executeWithContext(t, tmpDir, "",
		"site", "drop", name, "--force",
	)
	if err != nil {
		t.Fatalf("site drop: %v", err)
	}

	if cliSchemaExists(t, schema) {
		t.Error("schema still exists after site drop")
	}
	if cliSiteRowExists(t, name) {
		t.Error("site row still exists after site drop")
	}
}

func TestCLI_DuplicateSiteCreate_Error(t *testing.T) {
	tmpDir := t.TempDir()
	name := uniqueCLISiteName(t)
	cleanupCLISite(t, name)

	// First create — should succeed.
	_, _, err := executeWithContext(t, tmpDir, "",
		"site", "create", name,
		"--admin-password", "secret123",
		"--admin-email", "admin@test.dev",
	)
	if err != nil {
		t.Fatalf("first site create: %v", err)
	}

	// Second create — should fail.
	_, _, err = executeWithContext(t, tmpDir, "",
		"site", "create", name,
		"--admin-password", "secret123",
		"--admin-email", "admin@test.dev",
	)
	if err == nil {
		t.Fatal("expected error for duplicate site create, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

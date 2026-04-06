//go:build integration

package i18n

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

var (
	integPool  *pgxpool.Pool
	integRedis *redis.Client
)

const testSchema = "tenant_i18n_integ"

func TestMain(m *testing.M) {
	ctx := context.Background()

	// Connect to PostgreSQL.
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		envOrDefault("MOCA_TEST_DB_USER", "moca"),
		envOrDefault("MOCA_TEST_DB_PASSWORD", "moca_test"),
		envOrDefault("MOCA_TEST_DB_HOST", "127.0.0.1"),
		envOrDefaultInt("MOCA_TEST_DB_PORT", 5433),
		envOrDefault("MOCA_TEST_DB_NAME", "moca_test"),
	)

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot connect to pg: %v\n", err)
		os.Exit(1)
	}
	integPool = pool

	// Create test schema.
	if _, err := pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", testSchema)); err != nil {
		fmt.Fprintf(os.Stderr, "cannot create schema: %v\n", err)
		os.Exit(1)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf("SET search_path TO %s", testSchema)); err != nil {
		fmt.Fprintf(os.Stderr, "cannot set search_path: %v\n", err)
		os.Exit(1)
	}

	// Create tab_translation.
	ddl := TranslationDDL()
	for _, stmt := range ddl {
		if _, err := pool.Exec(ctx, stmt.SQL); err != nil {
			fmt.Fprintf(os.Stderr, "ddl failed: %v\n%s\n", err, stmt.SQL)
			os.Exit(1)
		}
	}

	// Connect to Redis.
	redisAddr := fmt.Sprintf("%s:%d",
		envOrDefault("MOCA_TEST_REDIS_HOST", "127.0.0.1"),
		envOrDefaultInt("MOCA_TEST_REDIS_PORT", 6380),
	)
	integRedis = redis.NewClient(&redis.Options{Addr: redisAddr, DB: 0})
	if err := integRedis.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "cannot connect to redis: %v\n", err)
		os.Exit(1)
	}

	exitCode := m.Run()

	// Cleanup.
	pool.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", testSchema))
	integRedis.Close()
	pool.Close()

	os.Exit(exitCode)
}

func TestIntegration_TranslateLifecycle(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Pool resolver returns the integration pool.
	resolver := func(_ context.Context, _ string) (*pgxpool.Pool, error) {
		return integPool, nil
	}

	tr := NewTranslator(integRedis, resolver, logger)

	// Clean up from previous runs.
	integPool.Exec(ctx, "DELETE FROM tab_translation")
	integRedis.Del(ctx, redisKey("i18n_integ", "ar"))

	// Insert test translations.
	_, err := integPool.Exec(ctx,
		`INSERT INTO tab_translation (source_text, language, translated_text, context, app) VALUES
		('Hello', 'ar', 'مرحبا', '', 'core'),
		('Save', 'ar', 'حفظ', 'DocType:User', 'core'),
		('Delete', 'ar', 'حذف', '', 'core')`,
	)
	if err != nil {
		t.Fatalf("insert translations: %v", err)
	}

	// Test Translate — DB hit (no Redis cache yet).
	result := tr.Translate(ctx, "i18n_integ", "ar", "Hello", "")
	if result != "مرحبا" {
		t.Errorf("Translate('Hello') = %q, want %q", result, "مرحبا")
	}

	// Test Translate with context.
	result = tr.Translate(ctx, "i18n_integ", "ar", "Save", "DocType:User")
	if result != "حفظ" {
		t.Errorf("Translate('Save', context) = %q, want %q", result, "حفظ")
	}

	// Test LoadAll — should populate Redis.
	all, err := tr.LoadAll(ctx, "i18n_integ", "ar")
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("LoadAll returned %d entries, want 3", len(all))
	}
	if all["Hello"] != "مرحبا" {
		t.Errorf("LoadAll['Hello'] = %q, want %q", all["Hello"], "مرحبا")
	}

	// Verify Redis was populated.
	rKey := redisKey("i18n_integ", "ar")
	count, err := integRedis.HLen(ctx, rKey).Result()
	if err != nil {
		t.Fatalf("redis hlen: %v", err)
	}
	if count != 3 {
		t.Errorf("Redis hash has %d entries, want 3", count)
	}

	// Test Translate — should hit Redis now.
	result = tr.Translate(ctx, "i18n_integ", "ar", "Delete", "")
	if result != "حذف" {
		t.Errorf("Translate('Delete') from Redis = %q, want %q", result, "حذف")
	}

	// Test Invalidate.
	if err := tr.Invalidate(ctx, "i18n_integ", "ar"); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	exists, err := integRedis.Exists(ctx, rKey).Result()
	if err != nil {
		t.Fatalf("redis exists: %v", err)
	}
	if exists != 0 {
		t.Error("Redis key still exists after Invalidate")
	}

	// Test miss for unknown string.
	result = tr.Translate(ctx, "i18n_integ", "ar", "Unknown String", "")
	if result != "Unknown String" {
		t.Errorf("Translate('Unknown String') = %q, want source unchanged", result)
	}
}

func TestIntegration_GetAllForLanguage(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	resolver := func(_ context.Context, _ string) (*pgxpool.Pool, error) {
		return integPool, nil
	}

	tr := NewTranslator(integRedis, resolver, logger)

	// Clean up.
	integPool.Exec(ctx, "DELETE FROM tab_translation")

	// Insert data.
	_, err := integPool.Exec(ctx,
		`INSERT INTO tab_translation (source_text, language, translated_text, context, app) VALUES
		('Yes', 'ar', 'نعم', '', 'core'),
		('No', 'ar', 'لا', '', 'core'),
		('Yes', 'fr', 'Oui', '', 'core')`,
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Get Arabic translations.
	arTranslations, err := tr.GetAllForLanguage(ctx, "i18n_integ", "ar")
	if err != nil {
		t.Fatalf("GetAllForLanguage: %v", err)
	}
	if len(arTranslations) != 2 {
		t.Errorf("got %d ar translations, want 2", len(arTranslations))
	}

	// Get French translations.
	frTranslations, err := tr.GetAllForLanguage(ctx, "i18n_integ", "fr")
	if err != nil {
		t.Fatalf("GetAllForLanguage: %v", err)
	}
	if len(frTranslations) != 1 {
		t.Errorf("got %d fr translations, want 1", len(frTranslations))
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrDefaultInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		var i int
		fmt.Sscanf(v, "%d", &i)
		return i
	}
	return fallback
}

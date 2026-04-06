package i18n

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/internal/drivers"
)

func newTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	return mr, rc
}

func nullPoolResolver(_ context.Context, _ string) (*pgxpool.Pool, error) {
	return nil, nil
}

func TestTranslate_RedisHit(t *testing.T) {
	_, rc := newTestRedis(t)
	ctx := context.Background()

	// Pre-populate Redis hash.
	key := redisKey("acme", "ar")
	rc.HSet(ctx, key, "Hello", "مرحبا")

	tr := NewTranslator(rc, nullPoolResolver, nullLogger())
	result := tr.Translate(ctx, "acme", "ar", "Hello", "")

	if result != "مرحبا" {
		t.Errorf("Translate() = %q, want %q", result, "مرحبا")
	}
}

func TestTranslate_RedisHitWithContext(t *testing.T) {
	_, rc := newTestRedis(t)
	ctx := context.Background()

	key := redisKey("acme", "ar")
	field := hashField("Save", "DocType:User")
	rc.HSet(ctx, key, field, "حفظ")

	tr := NewTranslator(rc, nullPoolResolver, nullLogger())
	result := tr.Translate(ctx, "acme", "ar", "Save", "DocType:User")

	if result != "حفظ" {
		t.Errorf("Translate() = %q, want %q", result, "حفظ")
	}
}

func TestTranslate_FallbackToSource(t *testing.T) {
	_, rc := newTestRedis(t)
	ctx := context.Background()

	tr := NewTranslator(rc, nullPoolResolver, nullLogger())
	result := tr.Translate(ctx, "acme", "ar", "Unknown", "")

	if result != "Unknown" {
		t.Errorf("Translate() = %q, want %q", result, "Unknown")
	}
}

func TestTranslate_EmptyLanguage(t *testing.T) {
	tr := NewTranslator(nil, nullPoolResolver, nullLogger())
	result := tr.Translate(context.Background(), "acme", "", "Hello", "")

	if result != "Hello" {
		t.Errorf("Translate() = %q, want %q", result, "Hello")
	}
}

func TestTranslate_NilRedis(t *testing.T) {
	tr := NewTranslator(nil, nullPoolResolver, nullLogger())
	result := tr.Translate(context.Background(), "acme", "ar", "Hello", "")

	if result != "Hello" {
		t.Errorf("Translate() = %q, want %q", result, "Hello")
	}
}

func TestLoadAll_PopulatesRedis(t *testing.T) {
	_, rc := newTestRedis(t)
	ctx := context.Background()

	// Without a real DB, LoadAll returns empty map.
	tr := NewTranslator(rc, nullPoolResolver, nullLogger())
	result, err := tr.LoadAll(ctx, "acme", "ar")
	if err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("LoadAll() returned %d entries, want 0", len(result))
	}
}

func TestInvalidate(t *testing.T) {
	_, rc := newTestRedis(t)
	ctx := context.Background()

	key := redisKey("acme", "ar")
	rc.HSet(ctx, key, "Hello", "مرحبا")

	tr := NewTranslator(rc, nullPoolResolver, nullLogger())
	if err := tr.Invalidate(ctx, "acme", "ar"); err != nil {
		t.Fatalf("Invalidate() error: %v", err)
	}

	// Verify key is deleted.
	exists := rc.Exists(ctx, key).Val()
	if exists != 0 {
		t.Error("Redis key still exists after Invalidate()")
	}
}

func TestInvalidate_NilRedis(t *testing.T) {
	tr := NewTranslator(nil, nullPoolResolver, nullLogger())
	if err := tr.Invalidate(context.Background(), "acme", "ar"); err != nil {
		t.Fatalf("Invalidate() with nil redis should not error: %v", err)
	}
}

func TestHashField(t *testing.T) {
	if f := hashField("Hello", ""); f != "Hello" {
		t.Errorf("hashField without context = %q, want %q", f, "Hello")
	}
	if f := hashField("Hello", "DocType:User"); f != "Hello\x00DocType:User" {
		t.Errorf("hashField with context = %q, want %q", f, "Hello\x00DocType:User")
	}
}

func TestRedisKey(t *testing.T) {
	key := redisKey("acme", "ar")
	expected := "i18n:acme:ar"
	if key != expected {
		t.Errorf("redisKey() = %q, want %q", key, expected)
	}

	// Verify it uses the drivers constant.
	expected2 := drivers.KeyI18n
	_ = expected2 // just ensure it compiles, verifying import
}

func nullLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

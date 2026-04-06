package i18n

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/pkg/api"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/tenancy"
)

func testRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	return rc
}

func testTranslator(t *testing.T, rc *redis.Client) *Translator {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewTranslator(rc, nullPoolResolver, logger)
}

func TestI18nTransformer_TransformRequest_Noop(t *testing.T) {
	tr := NewI18nTransformer(testTranslator(t, nil))
	body := map[string]any{"name": "test"}

	result, err := tr.TransformRequest(context.Background(), &meta.MetaType{Name: "User"}, body)
	if err != nil {
		t.Fatalf("TransformRequest error: %v", err)
	}
	if result["name"] != "test" {
		t.Errorf("TransformRequest modified body unexpectedly")
	}
}

func TestI18nTransformer_NoLanguage(t *testing.T) {
	tr := NewI18nTransformer(testTranslator(t, nil))

	body := map[string]any{
		"_meta": map[string]any{"label": "User"},
	}

	result, err := tr.TransformResponse(context.Background(), &meta.MetaType{Name: "User"}, body)
	if err != nil {
		t.Fatalf("TransformResponse error: %v", err)
	}

	metaVal, ok := result["_meta"].(map[string]any)
	if !ok {
		t.Fatal("_meta not a map")
	}
	if metaVal["label"] != "User" {
		t.Error("TransformResponse should not translate without language in context")
	}
}

func TestI18nTransformer_TranslatesLabels(t *testing.T) {
	rc := testRedisClient(t)
	ctx := context.Background()

	// Populate Redis with translations.
	key := redisKey("acme", "ar")
	rc.HSet(ctx, key, hashField("User", "DocType:User"), "مستخدم")
	rc.HSet(ctx, key, hashField("Full Name", "DocType:User:field:full_name"), "الاسم الكامل")

	tr := NewI18nTransformer(testTranslator(t, rc))

	// Set up context with language and site.
	ctx = api.WithLanguage(ctx, "ar")
	ctx = api.WithSite(ctx, &tenancy.SiteContext{Name: "acme"})

	body := map[string]any{
		"name": "admin",
		"_meta": map[string]any{
			"label": "User",
			"fields": []any{
				map[string]any{
					"name":  "full_name",
					"label": "Full Name",
				},
			},
		},
	}

	result, err := tr.TransformResponse(ctx, &meta.MetaType{Name: "User"}, body)
	if err != nil {
		t.Fatalf("TransformResponse error: %v", err)
	}

	metaMap, ok := result["_meta"].(map[string]any)
	if !ok {
		t.Fatal("_meta not a map")
	}
	if metaMap["label"] != "مستخدم" {
		t.Errorf("MetaType label = %q, want %q", metaMap["label"], "مستخدم")
	}

	fields, ok := metaMap["fields"].([]any)
	if !ok {
		t.Fatal("fields not a slice")
	}
	field, ok := fields[0].(map[string]any)
	if !ok {
		t.Fatal("field not a map")
	}
	if field["label"] != "الاسم الكامل" {
		t.Errorf("Field label = %q, want %q", field["label"], "الاسم الكامل")
	}
}

func TestI18nTransformer_NoMetaSection(t *testing.T) {
	rc := testRedisClient(t)
	tr := NewI18nTransformer(testTranslator(t, rc))

	ctx := api.WithLanguage(context.Background(), "ar")
	ctx = api.WithSite(ctx, &tenancy.SiteContext{Name: "acme"})

	// Body without _meta — should be a no-op.
	body := map[string]any{"name": "test"}
	result, err := tr.TransformResponse(ctx, &meta.MetaType{Name: "User"}, body)
	if err != nil {
		t.Fatalf("TransformResponse error: %v", err)
	}
	if result["name"] != "test" {
		t.Error("TransformResponse modified body without _meta section")
	}
}

func TestI18nTransformer_NilMetaType(t *testing.T) {
	tr := NewI18nTransformer(testTranslator(t, nil))

	ctx := api.WithLanguage(context.Background(), "ar")

	body := map[string]any{"name": "test"}
	result, err := tr.TransformResponse(ctx, nil, body)
	if err != nil {
		t.Fatalf("TransformResponse error: %v", err)
	}
	if result["name"] != "test" {
		t.Error("TransformResponse should be no-op with nil MetaType")
	}
}

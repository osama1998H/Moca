package i18n

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/internal/drivers"
)

// PoolResolver returns the tenant-scoped pgxpool.Pool for the given site.
// Typically backed by orm.DBManager.ForSite.
type PoolResolver func(ctx context.Context, site string) (*pgxpool.Pool, error)

// Translator provides runtime translation lookups with a two-tier cache:
//
//	L1: Redis hash (i18n:{site}:{language}) — shared across processes
//	L2: PostgreSQL tab_translation — source of truth
//
// If Redis is nil the translator degrades gracefully to DB-only lookups.
type Translator struct {
	redis        *redis.Client
	poolResolver PoolResolver
	logger       *slog.Logger
}

// NewTranslator creates a Translator. redis may be nil (L1 cache skipped).
func NewTranslator(redis *redis.Client, poolResolver PoolResolver, logger *slog.Logger) *Translator {
	return &Translator{
		redis:        redis,
		poolResolver: poolResolver,
		logger:       logger,
	}
}

// hashField builds the Redis hash field key for a translation entry.
// When context is non-empty, it is appended with a NUL separator.
func hashField(source, msgContext string) string {
	if msgContext == "" {
		return source
	}
	return source + "\x00" + msgContext
}

// redisKey returns the Redis key for a site+language translation hash.
func redisKey(site, lang string) string {
	return fmt.Sprintf(drivers.KeyI18n, site, lang)
}

// Translate returns the translated text for the given source string.
// Fallback chain: Redis HGET → PostgreSQL SELECT → source unchanged.
func (t *Translator) Translate(ctx context.Context, site, lang, source, msgContext string) string {
	if lang == "" {
		return source
	}

	field := hashField(source, msgContext)

	// L1: Redis hash lookup.
	if t.redis != nil {
		val, err := t.redis.HGet(ctx, redisKey(site, lang), field).Result()
		if err == nil && val != "" {
			return val
		}
		if err != nil && err != redis.Nil {
			t.logger.Warn("i18n redis hget failed", slog.String("error", err.Error()))
		}
	}

	// L2: PostgreSQL lookup.
	if t.poolResolver != nil {
		pool, err := t.poolResolver(ctx, site)
		if err != nil || pool == nil {
			if err != nil {
				t.logger.Warn("i18n pool resolver failed", slog.String("error", err.Error()))
			}
			return source
		}

		var translated string
		err = pool.QueryRow(ctx,
			`SELECT translated_text FROM tab_translation WHERE source_text = $1 AND language = $2 AND context = $3`,
			source, lang, msgContext,
		).Scan(&translated)
		if err == nil && translated != "" {
			// Promote into Redis.
			if t.redis != nil {
				if herr := t.redis.HSet(ctx, redisKey(site, lang), field, translated).Err(); herr != nil {
					t.logger.Warn("i18n redis hset failed", slog.String("error", herr.Error()))
				}
			}
			return translated
		}
	}

	return source
}

// LoadAll bulk-loads all translations for a site+language from PostgreSQL
// into a map and populates the Redis hash cache. Returns the translation
// map keyed by source text (simple key, no context encoding for the map).
func (t *Translator) LoadAll(ctx context.Context, site, lang string) (map[string]string, error) {
	result := make(map[string]string)

	if t.poolResolver == nil {
		return result, nil
	}

	pool, err := t.poolResolver(ctx, site)
	if err != nil || pool == nil {
		if err != nil {
			return nil, fmt.Errorf("i18n loadall: pool: %w", err)
		}
		return result, nil
	}

	rows, err := pool.Query(ctx,
		`SELECT source_text, translated_text, context FROM tab_translation WHERE language = $1`,
		lang,
	)
	if err != nil {
		return nil, fmt.Errorf("i18n loadall: query: %w", err)
	}
	defer rows.Close()

	redisData := make(map[string]any)
	for rows.Next() {
		var source, translated, msgCtx string
		if err := rows.Scan(&source, &translated, &msgCtx); err != nil {
			t.logger.Warn("i18n loadall: scan row", slog.String("error", err.Error()))
			continue
		}
		result[source] = translated
		redisData[hashField(source, msgCtx)] = translated
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("i18n loadall: rows: %w", err)
	}

	// Populate Redis hash.
	if t.redis != nil && len(redisData) > 0 {
		if err := t.redis.HSet(ctx, redisKey(site, lang), redisData).Err(); err != nil {
			t.logger.Warn("i18n loadall: redis hset", slog.String("error", err.Error()))
		}
	}

	return result, nil
}

// GetAllForLanguage returns all Translation records for a site+language
// from the database. Used by the CLI status command and API endpoint.
func (t *Translator) GetAllForLanguage(ctx context.Context, site, lang string) ([]Translation, error) {
	if t.poolResolver == nil {
		return nil, nil
	}

	pool, err := t.poolResolver(ctx, site)
	if err != nil || pool == nil {
		if err != nil {
			return nil, fmt.Errorf("i18n getall: pool: %w", err)
		}
		return nil, nil
	}

	rows, err := pool.Query(ctx,
		`SELECT source_text, language, translated_text, context, COALESCE(app, '') FROM tab_translation WHERE language = $1`,
		lang,
	)
	if err != nil {
		return nil, fmt.Errorf("i18n getall: query: %w", err)
	}
	defer rows.Close()

	var translations []Translation
	for rows.Next() {
		var tr Translation
		if err := rows.Scan(&tr.SourceText, &tr.Language, &tr.TranslatedText, &tr.Context, &tr.App); err != nil {
			return nil, fmt.Errorf("i18n getall: scan: %w", err)
		}
		translations = append(translations, tr)
	}
	return translations, rows.Err()
}

// Invalidate clears the Redis cache for a site+language pair.
func (t *Translator) Invalidate(ctx context.Context, site, lang string) error {
	if t.redis == nil {
		return nil
	}
	return t.redis.Del(ctx, redisKey(site, lang)).Err()
}

// LookupDirection returns the text direction ("ltr" or "rtl") for the given
// language by querying tab_language. Falls back to "ltr" if not found.
func (t *Translator) LookupDirection(ctx context.Context, site, lang string) string {
	if t.poolResolver == nil {
		return "ltr"
	}
	pool, err := t.poolResolver(ctx, site)
	if err != nil || pool == nil {
		return "ltr"
	}
	var dir string
	if scanErr := pool.QueryRow(ctx,
		`SELECT direction FROM tab_language WHERE name = $1`, lang,
	).Scan(&dir); scanErr == nil && dir != "" {
		return dir
	}
	return "ltr"
}

package i18n

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/osama1998H/moca/pkg/api"
)

// I18nMiddleware returns an HTTP middleware that resolves the user's preferred
// language and stores it in the request context via api.WithLanguage.
//
// Language resolution priority:
//  1. ?lang= query parameter (explicit override)
//  2. Accept-Language header (standard HTTP negotiation)
//
// If no language can be resolved the request proceeds without a language in context.
func I18nMiddleware() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lang := resolveLanguage(r)
			if lang != "" {
				ctx := api.WithLanguage(r.Context(), lang)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// resolveLanguage determines the preferred language for the request.
func resolveLanguage(r *http.Request) string {
	// 1. Explicit query parameter.
	if lang := r.URL.Query().Get("lang"); lang != "" {
		return normalizeLanguage(lang)
	}

	// 2. Accept-Language header.
	if accept := r.Header.Get("Accept-Language"); accept != "" {
		if lang := parseAcceptLanguage(accept); lang != "" {
			return lang
		}
	}

	return ""
}

// langQuality pairs a language tag with its quality value.
type langQuality struct {
	lang    string
	quality float64
}

// parseAcceptLanguage parses the Accept-Language header and returns the
// highest-quality language tag. Example: "en-US,en;q=0.9,ar;q=0.8" → "en".
func parseAcceptLanguage(header string) string {
	var langs []langQuality

	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		lq := langQuality{quality: 1.0}
		if idx := strings.Index(part, ";"); idx >= 0 {
			lq.lang = strings.TrimSpace(part[:idx])
			qStr := strings.TrimSpace(part[idx+1:])
			if strings.HasPrefix(qStr, "q=") {
				if q, err := strconv.ParseFloat(qStr[2:], 64); err == nil {
					lq.quality = q
				}
			}
		} else {
			lq.lang = part
		}

		lq.lang = normalizeLanguage(lq.lang)
		if lq.lang != "" && lq.lang != "*" {
			langs = append(langs, lq)
		}
	}

	if len(langs) == 0 {
		return ""
	}

	sort.Slice(langs, func(i, j int) bool {
		return langs[i].quality > langs[j].quality
	})

	return langs[0].lang
}

// normalizeLanguage extracts the primary language subtag (e.g. "en-US" → "en").
func normalizeLanguage(lang string) string {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return ""
	}
	// Take primary subtag only.
	if idx := strings.IndexAny(lang, "-_"); idx >= 0 {
		lang = lang[:idx]
	}
	return strings.ToLower(lang)
}

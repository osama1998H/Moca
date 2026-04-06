package i18n

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/osama1998H/moca/pkg/api"
)

func TestI18nMiddleware_AcceptLanguage(t *testing.T) {
	var gotLang string

	handler := I18nMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLang = api.LanguageFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "ar")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotLang != "ar" {
		t.Errorf("expected language %q, got %q", "ar", gotLang)
	}
}

func TestI18nMiddleware_QueryParam(t *testing.T) {
	var gotLang string

	handler := I18nMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLang = api.LanguageFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/?lang=fr", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotLang != "fr" {
		t.Errorf("expected language %q, got %q", "fr", gotLang)
	}
}

func TestI18nMiddleware_QueryOverridesHeader(t *testing.T) {
	var gotLang string

	handler := I18nMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLang = api.LanguageFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/?lang=fr", nil)
	req.Header.Set("Accept-Language", "ar")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotLang != "fr" {
		t.Errorf("expected query param 'fr' to override header 'ar', got %q", gotLang)
	}
}

func TestI18nMiddleware_QualityValues(t *testing.T) {
	var gotLang string

	handler := I18nMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLang = api.LanguageFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,ar;q=0.8")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotLang != "en" {
		t.Errorf("expected language %q (highest quality), got %q", "en", gotLang)
	}
}

func TestI18nMiddleware_NoLanguage(t *testing.T) {
	var gotLang string

	handler := I18nMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLang = api.LanguageFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotLang != "" {
		t.Errorf("expected empty language, got %q", gotLang)
	}
}

func TestI18nMiddleware_NormalizesRegion(t *testing.T) {
	var gotLang string

	handler := I18nMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLang = api.LanguageFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "en-US")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotLang != "en" {
		t.Errorf("expected 'en-US' normalized to %q, got %q", "en", gotLang)
	}
}

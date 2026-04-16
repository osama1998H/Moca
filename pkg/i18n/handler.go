package i18n

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/osama1998H/moca/pkg/api"
)

// TranslationHandler serves the translation bundle API endpoint.
type TranslationHandler struct {
	translator *Translator
	logger     *slog.Logger
}

// NewTranslationHandler creates a handler backed by the given Translator.
func NewTranslationHandler(translator *Translator, logger *slog.Logger) *TranslationHandler {
	return &TranslationHandler{
		translator: translator,
		logger:     logger,
	}
}

// RegisterRoutes registers the translation API endpoint on the given ServeMux.
func (h *TranslationHandler) RegisterRoutes(mux *http.ServeMux, version string) {
	mux.HandleFunc("GET /api/"+version+"/translations/{lang}", h.handleGetTranslations)
}

// handleGetTranslations returns all translations for a language as a JSON object.
func (h *TranslationHandler) handleGetTranslations(w http.ResponseWriter, r *http.Request) {
	lang := r.PathValue("lang")
	if lang == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_LANGUAGE", "language path parameter is required")
		return
	}

	site := api.SiteFromContext(r.Context())
	if site == nil {
		writeJSONError(w, http.StatusBadRequest, "MISSING_SITE", "site context is required")
		return
	}

	translations, err := h.translator.LoadAll(r.Context(), site.Name, lang)
	if err != nil {
		h.logger.Error("failed to load translations",
			slog.String("site", site.Name),
			slog.String("lang", lang),
			slog.String("error", err.Error()),
		)
		writeJSONError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load translations")
		return
	}

	direction := h.translator.LookupDirection(r.Context(), site.Name, lang)
	writeJSON(w, http.StatusOK, map[string]any{
		"data":      translations,
		"direction": direction,
	})
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data) //nolint:errcheck
}

// writeJSONError writes an error response in the standard Moca error envelope.
func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

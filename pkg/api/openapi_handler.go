package api

import (
	_ "embed"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/osama1998H/moca/pkg/meta"
)

//go:embed swagger_ui.html
var swaggerUIHTML []byte

// OpenAPIHandler serves the auto-generated OpenAPI 3.0.3 specification
// and a Swagger UI documentation page.
type OpenAPIHandler struct {
	metaList MetaListResolver
	methods  *MethodRegistry
	logger   *slog.Logger
	version  string
}

// NewOpenAPIHandler creates an OpenAPIHandler wired to the given Gateway.
func NewOpenAPIHandler(gw *Gateway, version string) *OpenAPIHandler {
	return &OpenAPIHandler{
		metaList: gw.Registry(),
		methods:  gw.MethodRegistry(),
		logger:   gw.Logger(),
		version:  version,
	}
}

// RegisterRoutes registers the OpenAPI spec and Swagger UI endpoints.
func (h *OpenAPIHandler) RegisterRoutes(mux *http.ServeMux, apiVersion string) {
	mux.HandleFunc("GET /api/"+apiVersion+"/openapi.json", h.handleSpec)
	mux.HandleFunc("GET /api/docs", h.handleDocs)
}

// handleSpec generates and serves the OpenAPI 3.0.3 specification as JSON.
func (h *OpenAPIHandler) handleSpec(w http.ResponseWriter, r *http.Request) {
	var metatypes []meta.MetaType

	site := SiteFromContext(r.Context())
	if site != nil {
		mts, err := h.metaList.ListAll(r.Context(), site.Name)
		if err != nil {
			h.logger.ErrorContext(r.Context(), "openapi: failed to list MetaTypes",
				slog.String("site", site.Name), slog.Any("error", err))
			writeError(w, http.StatusInternalServerError, "SPEC_ERROR", "failed to generate OpenAPI spec")
			return
		}
		metatypes = make([]meta.MetaType, len(mts))
		for i, mt := range mts {
			metatypes[i] = *mt
		}
	}

	var methods []string
	if h.methods != nil {
		methods = h.methods.Names()
	}

	spec := GenerateSpec(metatypes, methods, SpecOptions{
		Title:   "Moca API",
		Version: h.version,
	})

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(spec); err != nil {
		h.logger.ErrorContext(r.Context(), "openapi: failed to encode spec",
			slog.Any("error", err))
	}
}

// handleDocs serves the embedded Swagger UI HTML page.
func (h *OpenAPIHandler) handleDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(swaggerUIHTML)
}

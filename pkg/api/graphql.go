package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/graphql-go/graphql"

	"github.com/osama1998H/moca/pkg/meta"
)

// MetaListResolver returns all MetaType definitions for a site and the current
// schema version. *meta.Registry satisfies this interface.
type MetaListResolver interface {
	ListAll(ctx context.Context, site string) ([]*meta.MetaType, error)
	SchemaVersion(ctx context.Context, site string) (int64, error)
}

// GraphQLHandler exposes an auto-generated GraphQL API from MetaType definitions.
// The schema is built dynamically and cached per site, keyed by schema version.
type GraphQLHandler struct {
	crud      CRUDService
	metaList  MetaListResolver
	meta      MetaResolver
	perm      PermissionChecker
	fieldPerm Transformer
	logger    *slog.Logger
	schemas   sync.Map // site name -> *cachedSchema
}

// cachedSchema stores a built GraphQL schema along with the version it was built from.
type cachedSchema struct {
	schema  *graphql.Schema
	version int64
}

// graphqlRequest represents the JSON body of a GraphQL HTTP request.
type graphqlRequest struct {
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables"`
	OperationName string         `json:"operationName"`
}

// NewGraphQLHandler creates a GraphQLHandler wired to the given Gateway.
func NewGraphQLHandler(gw *Gateway) *GraphQLHandler {
	return &GraphQLHandler{
		crud:      gw.DocManager(),
		metaList:  gw.Registry(),
		meta:      gw.Registry(),
		perm:      gw.PermChecker(),
		fieldPerm: gw.FieldLevelTransformer(),
		logger:    gw.Logger(),
	}
}

// RegisterRoutes registers the GraphQL endpoint and playground on the mux.
// GraphQL uses a single unversioned endpoint.
func (h *GraphQLHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/graphql", h.handleQuery)
	mux.HandleFunc("GET /api/graphql/playground", h.handlePlayground)
}

// handleQuery processes a GraphQL query or mutation.
func (h *GraphQLHandler) handleQuery(w http.ResponseWriter, r *http.Request) {
	site := SiteFromContext(r.Context())
	if site == nil {
		writeError(w, http.StatusBadRequest, "MISSING_SITE", "X-Moca-Site header is required")
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}

	var req graphqlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}

	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "EMPTY_QUERY", "query field is required")
		return
	}

	schema, err := h.getOrBuildSchema(r.Context(), site.Name)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "graphql: schema build failed",
			slog.String("site", site.Name), slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "SCHEMA_ERROR", "failed to build GraphQL schema")
		return
	}

	// Attach DataLoaders for batched link resolution.
	ctx := withDataLoaders(r.Context())

	result := graphql.Do(graphql.Params{
		Schema:         *schema,
		RequestString:  req.Query,
		VariableValues: req.Variables,
		OperationName:  req.OperationName,
		Context:        ctx,
	})

	w.Header().Set("Content-Type", "application/json")
	// GraphQL always returns 200 per spec; errors are in the response body.
	if err := json.NewEncoder(w).Encode(result); err != nil {
		h.logger.ErrorContext(r.Context(), "graphql: failed to encode response",
			slog.Any("error", err))
	}
}

// handlePlayground serves the GraphiQL interactive IDE.
func (h *GraphQLHandler) handlePlayground(w http.ResponseWriter, r *http.Request) {
	servePlayground(w, r)
}

// getOrBuildSchema returns a cached schema if the version matches, or builds
// a new one from the current MetaType registry.
func (h *GraphQLHandler) getOrBuildSchema(ctx context.Context, site string) (*graphql.Schema, error) {
	// Check current version.
	currentVersion, err := h.metaList.SchemaVersion(ctx, site)
	if err != nil {
		h.logger.WarnContext(ctx, "graphql: schema version check failed, rebuilding",
			slog.String("site", site), slog.Any("error", err))
	}

	// Check cache.
	if cached, ok := h.schemas.Load(site); ok {
		cs, _ := cached.(*cachedSchema)
		if cs != nil && cs.version == currentVersion && currentVersion > 0 {
			return cs.schema, nil
		}
	}

	// Build new schema.
	metatypes, err := h.metaList.ListAll(ctx, site)
	if err != nil {
		return nil, err
	}

	rf := &resolverFactory{handler: h}
	schema, err := buildSchema(metatypes, rf)
	if err != nil {
		return nil, err
	}

	h.schemas.Store(site, &cachedSchema{
		schema:  schema,
		version: currentVersion,
	})

	return schema, nil
}

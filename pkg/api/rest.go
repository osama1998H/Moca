package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/observe"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// maxRequestBody is the maximum allowed request body size (1 MiB).
const maxRequestBody = 1 << 20

// CRUDService abstracts the document CRUD operations needed by REST handlers.
// *document.DocManager satisfies this interface.
type CRUDService interface {
	Insert(ctx *document.DocContext, doctype string, values map[string]any) (*document.DynamicDoc, error)
	Update(ctx *document.DocContext, doctype, name string, values map[string]any) (*document.DynamicDoc, error)
	Delete(ctx *document.DocContext, doctype, name string) error
	Get(ctx *document.DocContext, doctype, name string) (*document.DynamicDoc, error)
	GetList(ctx *document.DocContext, doctype string, opts document.ListOptions) ([]*document.DynamicDoc, int, error)
	GetSingle(ctx *document.DocContext, doctype string) (*document.DynamicDoc, error)
	GetVersions(ctx *document.DocContext, doctype, docname string, limit, offset int) ([]document.VersionRecord, int, error)
}

// MetaResolver looks up a MetaType by site and doctype name.
// *meta.Registry satisfies this interface.
type MetaResolver interface {
	Get(ctx context.Context, site, doctype string) (*meta.MetaType, error)
}

// ResourceHandler bridges HTTP requests to DocManager CRUD operations.
type ResourceHandler struct {
	crud        CRUDService
	meta        MetaResolver
	perm        PermissionChecker
	fieldPerm   Transformer
	rateLimiter *RateLimiter
	mwRegistry  *MiddlewareRegistry
	logger      *slog.Logger
}

func newDocContext(ctx context.Context, site *tenancy.SiteContext, user *auth.User) *document.DocContext {
	docCtx := document.NewDocContext(ctx, site, user)
	docCtx.RequestID = RequestIDFromContext(ctx)
	return docCtx
}

// NewResourceHandler creates a ResourceHandler wired to the given Gateway.
func NewResourceHandler(gw *Gateway) *ResourceHandler {
	return &ResourceHandler{
		crud:        gw.DocManager(),
		meta:        gw.Registry(),
		perm:        gw.PermChecker(),
		fieldPerm:   gw.FieldLevelTransformer(),
		rateLimiter: gw.RateLimiter(),
		mwRegistry:  gw.MiddlewareRegistry(),
		logger:      gw.Logger(),
	}
}

// RegisterRoutes registers all REST resource and meta routes on the mux.
// version is a literal string like "v1".
func (h *ResourceHandler) RegisterRoutes(mux *http.ServeMux, version string) {
	p := "/api/" + version
	mux.HandleFunc("GET "+p+"/resource/{doctype}", h.handleList)
	mux.HandleFunc("POST "+p+"/resource/{doctype}", h.handleCreate)
	mux.HandleFunc("GET "+p+"/resource/{doctype}/{name}", h.handleGet)
	mux.HandleFunc("PUT "+p+"/resource/{doctype}/{name}", h.handleUpdate)
	mux.HandleFunc("DELETE "+p+"/resource/{doctype}/{name}", h.handleDelete)
	mux.HandleFunc("GET "+p+"/resource/{doctype}/{name}/versions", h.handleVersions)
	mux.HandleFunc("GET "+p+"/resource/{doctype}/{name}/events", h.handleGetEvents)
	mux.HandleFunc("GET "+p+"/meta/{doctype}", h.handleMeta)
}

// ── Handlers ────────────────────────────────────────────────────────────────

func (h *ResourceHandler) handleList(w http.ResponseWriter, r *http.Request) {
	mt, site, user, ok := h.resolveRequest(w, r, "read", func(ac *meta.APIConfig) bool { return ac.AllowList })
	if !ok {
		return
	}

	h.applyDocTypeMiddleware(mt, func(w http.ResponseWriter, r *http.Request) {
		opts, err := parseListParams(r, mt.APIConfig)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_PARAMS", err.Error())
			return
		}

		docCtx := newDocContext(r.Context(), site, user)
		docs, total, err := h.crud.GetList(docCtx, mt.Name, opts)
		if err != nil {
			h.handleCRUDError(w, r, err)
			return
		}

		chain := buildTransformerChain(r.Context(), mt, h.fieldPerm)
		ctx := WithOperationType(r.Context(), OpList)
		data := make([]map[string]any, len(docs))
		for i, d := range docs {
			item := d.AsMap()
			item, err = chain.TransformResponse(ctx, mt, item)
			if err != nil {
				h.handleCRUDError(w, r, transformError("response", err))
				return
			}
			data[i] = item
		}
		writeListSuccess(w, data, total, opts.Limit, opts.Offset)
	})(w, r)
}

func (h *ResourceHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	mt, site, user, ok := h.resolveRequest(w, r, "read", func(ac *meta.APIConfig) bool { return ac.AllowGet })
	if !ok {
		return
	}

	h.applyDocTypeMiddleware(mt, func(w http.ResponseWriter, r *http.Request) {
		docCtx := newDocContext(r.Context(), site, user)

		var doc *document.DynamicDoc
		var err error
		if mt.IsSingle {
			doc, err = h.crud.GetSingle(docCtx, mt.Name)
		} else {
			name := r.PathValue("name")
			if name == "" {
				writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "document name is required")
				return
			}
			doc, err = h.crud.Get(docCtx, mt.Name, name)
		}
		if err != nil {
			h.handleCRUDError(w, r, err)
			return
		}

		chain := buildTransformerChain(r.Context(), mt, h.fieldPerm)
		ctx := WithOperationType(r.Context(), OpGet)
		data := doc.AsMap()
		data, err = chain.TransformResponse(ctx, mt, data)
		if err != nil {
			h.handleCRUDError(w, r, transformError("response", err))
			return
		}
		writeSuccess(w, http.StatusOK, data)
	})(w, r)
}

func (h *ResourceHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	mt, site, user, ok := h.resolveRequest(w, r, "create", func(ac *meta.APIConfig) bool { return ac.AllowCreate })
	if !ok {
		return
	}
	if mt.IsSingle {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "single doctypes do not support create")
		return
	}

	h.applyDocTypeMiddleware(mt, func(w http.ResponseWriter, r *http.Request) {
		values, err := h.decodeBody(w, r)
		if err != nil {
			return // error already written
		}

		chain := buildTransformerChain(r.Context(), mt, h.fieldPerm)
		ctx := WithOperationType(r.Context(), OpCreate)
		values, err = chain.TransformRequest(ctx, mt, values)
		if err != nil {
			if !mapErrorResponse(w, err) {
				writeError(w, http.StatusBadRequest, "TRANSFORM_ERROR", err.Error())
			}
			return
		}

		docCtx := newDocContext(r.Context(), site, user)
		doc, err := h.crud.Insert(docCtx, mt.Name, values)
		if err != nil {
			h.handleCRUDError(w, r, err)
			return
		}

		data := doc.AsMap()
		data, err = chain.TransformResponse(ctx, mt, data)
		if err != nil {
			h.handleCRUDError(w, r, transformError("response", err))
			return
		}
		writeSuccess(w, http.StatusCreated, data)
	})(w, r)
}

func (h *ResourceHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	mt, site, user, ok := h.resolveRequest(w, r, "write", func(ac *meta.APIConfig) bool { return ac.AllowUpdate })
	if !ok {
		return
	}

	h.applyDocTypeMiddleware(mt, func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" && !mt.IsSingle {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "document name is required")
			return
		}

		values, err := h.decodeBody(w, r)
		if err != nil {
			return
		}

		chain := buildTransformerChain(r.Context(), mt, h.fieldPerm)
		ctx := WithOperationType(r.Context(), OpUpdate)
		values, err = chain.TransformRequest(ctx, mt, values)
		if err != nil {
			if !mapErrorResponse(w, err) {
				writeError(w, http.StatusBadRequest, "TRANSFORM_ERROR", err.Error())
			}
			return
		}

		docCtx := newDocContext(r.Context(), site, user)
		doc, err := h.crud.Update(docCtx, mt.Name, name, values)
		if err != nil {
			h.handleCRUDError(w, r, err)
			return
		}

		data := doc.AsMap()
		data, err = chain.TransformResponse(ctx, mt, data)
		if err != nil {
			h.handleCRUDError(w, r, transformError("response", err))
			return
		}
		writeSuccess(w, http.StatusOK, data)
	})(w, r)
}

func (h *ResourceHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	mt, site, user, ok := h.resolveRequest(w, r, "delete", func(ac *meta.APIConfig) bool { return ac.AllowDelete })
	if !ok {
		return
	}
	if mt.IsSingle {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "single doctypes do not support delete")
		return
	}

	h.applyDocTypeMiddleware(mt, func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "document name is required")
			return
		}

		docCtx := newDocContext(r.Context(), site, user)
		if err := h.crud.Delete(docCtx, mt.Name, name); err != nil {
			h.handleCRUDError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})(w, r)
}

func (h *ResourceHandler) handleMeta(w http.ResponseWriter, r *http.Request) {
	mt, _, _, ok := h.resolveRequest(w, r, "read", nil) // no AllowX gate for meta
	if !ok {
		return
	}

	h.applyDocTypeMiddleware(mt, func(w http.ResponseWriter, r *http.Request) {
		resp := filterMetaFields(buildMetaResponse(mt), mt, r.Context())
		writeSuccess(w, http.StatusOK, resp)
	})(w, r)
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// resolveRequest performs the common validation steps shared by all handlers:
//  1. Extract site and user from context
//  2. Look up MetaType via registry
//  3. Check APIConfig.Enabled
//  4. Check the operation-specific AllowX flag (via allowCheck, may be nil)
//  5. Check permissions
//
// Returns (MetaType, site, user, true) on success, or writes an error and
// returns (nil, nil, nil, false).
func (h *ResourceHandler) resolveRequest(
	w http.ResponseWriter,
	r *http.Request,
	perm string,
	allowCheck func(*meta.APIConfig) bool,
) (*meta.MetaType, *tenancy.SiteContext, *auth.User, bool) {
	site := SiteFromContext(r.Context())
	if site == nil {
		writeError(w, http.StatusBadRequest, "TENANT_REQUIRED", "X-Moca-Site header or subdomain required")
		return nil, nil, nil, false
	}

	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
		return nil, nil, nil, false
	}

	doctype := r.PathValue("doctype")
	mt, err := h.meta.Get(r.Context(), site.Name, doctype)
	if err != nil {
		if !mapErrorResponse(w, err) {
			h.logError(r, err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		}
		return nil, nil, nil, false
	}

	if mt.APIConfig == nil || !mt.APIConfig.Enabled {
		writeError(w, http.StatusNotFound, "DOCTYPE_NOT_FOUND", "doctype not found")
		return nil, nil, nil, false
	}

	if allowCheck != nil && !allowCheck(mt.APIConfig) {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "this operation is not allowed for "+doctype)
		return nil, nil, nil, false
	}

	if err := h.perm.CheckDocPerm(r.Context(), user, doctype, perm); err != nil {
		if !mapErrorResponse(w, err) {
			writeError(w, http.StatusForbidden, "PERMISSION_DENIED", "permission denied")
		}
		return nil, nil, nil, false
	}

	// Per-DocType rate limiting (additional to the global per-user limit).
	if h.rateLimiter != nil && mt.APIConfig.RateLimit != nil {
		userID := "anonymous"
		if user.Email != "" {
			userID = user.Email
		}
		key := fmt.Sprintf("rl:%s:%s:%s", site.Name, userID, doctype)
		allowed, retryAfter, _ := h.rateLimiter.Allow(r.Context(), key, mt.APIConfig.RateLimit)
		if !allowed {
			retrySeconds := int(math.Ceil(retryAfter.Seconds()))
			if retrySeconds < 1 {
				retrySeconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests for "+doctype)
			return nil, nil, nil, false
		}
	}

	return mt, site, user, true
}

// applyDocTypeMiddleware wraps handler with per-DocType middleware from
// APIConfig.Middleware, resolved through the MiddlewareRegistry.
// Returns the original handler unchanged if no middleware is configured.
func (h *ResourceHandler) applyDocTypeMiddleware(mt *meta.MetaType, handler http.HandlerFunc) http.HandlerFunc {
	if h.mwRegistry == nil || mt.APIConfig == nil || len(mt.APIConfig.Middleware) == 0 {
		return handler
	}
	composed, err := h.mwRegistry.Chain(mt.APIConfig.Middleware)
	if err != nil {
		if h.logger != nil {
			h.logger.Error("per-doctype middleware resolution failed",
				slog.String("doctype", mt.Name),
				slog.String("error", err.Error()),
			)
		}
		return handler // degrade gracefully: skip unresolvable middleware
	}
	return composed(handler).ServeHTTP
}

// decodeBody reads and decodes the JSON request body into a map.
// Writes an error response and returns nil on failure.
func (h *ResourceHandler) decodeBody(w http.ResponseWriter, r *http.Request) (map[string]any, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var values map[string]any
	if err := json.NewDecoder(r.Body).Decode(&values); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "malformed JSON request body")
		return nil, err
	}
	if len(values) == 0 {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "request body must not be empty")
		return nil, errEmptyBody
	}
	return values, nil
}

// handleCRUDError maps a DocManager error to an HTTP response. Unrecognised
// errors are logged and returned as 500.
func (h *ResourceHandler) handleCRUDError(w http.ResponseWriter, r *http.Request, err error) {
	if mapErrorResponse(w, err) {
		return
	}
	h.logError(r, err)
	writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
}

func (h *ResourceHandler) logError(r *http.Request, err error) {
	logger := observe.LoggerFromContext(r.Context())
	logger.Error("request failed",
		slog.String("error", err.Error()),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)
}

// errEmptyBody is a package-level sentinel for empty request bodies.
var errEmptyBody = &emptyBodyError{}

type emptyBodyError struct{}

func (e *emptyBodyError) Error() string { return "empty request body" }

// ── Meta response builder ───────────────────────────────────────────────────

// apiFieldDef is the API-safe representation of a field definition.
type apiFieldDef struct {
	Default            any      `json:"default,omitempty"`
	MaxValue           *float64 `json:"max_value,omitempty"`
	MinValue           *float64 `json:"min_value,omitempty"`
	Name               string   `json:"name"`
	FieldType          string   `json:"field_type"`
	Label              string   `json:"label"`
	APIAlias           string   `json:"api_alias,omitempty"`
	Options            string   `json:"options,omitempty"`
	DependsOn          string   `json:"depends_on,omitempty"`
	MandatoryDependsOn string   `json:"mandatory_depends_on,omitempty"`
	Width              string   `json:"width,omitempty"`
	LayoutLabel        string   `json:"layout_label,omitempty"`
	MaxLength          int      `json:"max_length,omitempty"`
	ColSpan            int      `json:"col_span,omitempty"`
	Required           bool     `json:"required"`
	ReadOnly           bool     `json:"read_only"`
	APIReadOnly        bool     `json:"api_read_only,omitempty"`
	InAPI              bool     `json:"in_api"`
	InListView         bool     `json:"in_list_view,omitempty"`
	InFilter           bool     `json:"in_filter,omitempty"`
	InPreview          bool     `json:"in_preview,omitempty"`
	Hidden             bool     `json:"hidden,omitempty"`
	Searchable         bool     `json:"searchable,omitempty"`
	Filterable         bool     `json:"filterable,omitempty"`
	Unique             bool     `json:"unique,omitempty"`
	Collapsible        bool     `json:"collapsible,omitempty"`
	CollapsedByDefault bool     `json:"collapsed_by_default,omitempty"`
}

// apiMetaResponse is the API-safe representation of a MetaType.
type apiMetaResponse struct {
	Layout        *meta.LayoutTree       `json:"layout,omitempty"`
	FieldsMap     map[string]apiFieldDef `json:"fields_map,omitempty"`
	NamingRule    meta.NamingStrategy    `json:"naming_rule"`
	Label         string                 `json:"label,omitempty"`
	Description   string                 `json:"description,omitempty"`
	Module        string                 `json:"module,omitempty"`
	TitleField    string                 `json:"title_field,omitempty"`
	ImageField    string                 `json:"image_field,omitempty"`
	SortField     string                 `json:"sort_field,omitempty"`
	SortOrder     string                 `json:"sort_order,omitempty"`
	Name          string                 `json:"name"`
	Fields        []apiFieldDef          `json:"fields"`
	SearchFields  []string               `json:"search_fields,omitempty"`
	IsSingle      bool                   `json:"is_single"`
	IsSubmittable bool                   `json:"is_submittable"`
	IsChildTable  bool                   `json:"is_child_table"`
	TrackChanges  bool                   `json:"track_changes,omitempty"`
	AllowGet      bool                   `json:"allow_get"`
	AllowCreate   bool                   `json:"allow_create"`
	AllowUpdate   bool                   `json:"allow_update"`
	AllowDelete   bool                   `json:"allow_delete"`
	AllowList     bool                   `json:"allow_list"`
}

// buildApiFieldDef maps a meta.FieldDef to its API-safe representation.
func buildApiFieldDef(f meta.FieldDef) apiFieldDef {
	return apiFieldDef{
		Name:               f.Name,
		FieldType:          string(f.FieldType),
		Label:              f.Label,
		Required:           f.Required,
		ReadOnly:           f.ReadOnly,
		APIReadOnly:        f.APIReadOnly,
		APIAlias:           f.APIAlias,
		InAPI:              f.InAPI,
		Options:            f.Options,
		DependsOn:          f.DependsOn,
		MandatoryDependsOn: f.MandatoryDependsOn,
		Default:            f.Default,
		MaxLength:          f.MaxLength,
		MaxValue:           f.MaxValue,
		MinValue:           f.MinValue,
		Width:              f.Width,
		InListView:         f.InListView,
		InFilter:           f.InFilter,
		InPreview:          f.InPreview,
		Hidden:             f.Hidden,
		Searchable:         f.Searchable,
		Filterable:         f.Filterable,
		Unique:             f.Unique,
		ColSpan:            f.LayoutHint.ColSpan,
		Collapsible:        f.LayoutHint.Collapsible,
		CollapsedByDefault: f.LayoutHint.CollapsedByDefault,
		LayoutLabel:        f.LayoutHint.Label,
	}
}

func buildMetaResponse(mt *meta.MetaType) apiMetaResponse {
	resp := apiMetaResponse{
		Name:          mt.Name,
		Label:         mt.Label,
		Description:   mt.Description,
		Module:        mt.Module,
		NamingRule:    mt.NamingRule,
		TitleField:    mt.TitleField,
		ImageField:    mt.ImageField,
		SortField:     mt.SortField,
		SortOrder:     mt.SortOrder,
		SearchFields:  mt.SearchFields,
		TrackChanges:  mt.TrackChanges,
		IsSingle:      mt.IsSingle,
		IsSubmittable: mt.IsSubmittable,
		IsChildTable:  mt.IsChildTable,
	}
	if mt.APIConfig != nil {
		resp.AllowGet = mt.APIConfig.AllowGet
		resp.AllowCreate = mt.APIConfig.AllowCreate
		resp.AllowUpdate = mt.APIConfig.AllowUpdate
		resp.AllowDelete = mt.APIConfig.AllowDelete
		resp.AllowList = mt.APIConfig.AllowList
	}
	resp.Fields = make([]apiFieldDef, 0, len(mt.Fields))
	for _, f := range mt.Fields {
		resp.Fields = append(resp.Fields, buildApiFieldDef(f))
	}
	if mt.Layout != nil {
		resp.Layout = mt.Layout
	}
	if mt.FieldsMap != nil {
		resp.FieldsMap = make(map[string]apiFieldDef, len(mt.FieldsMap))
		for name, f := range mt.FieldsMap {
			resp.FieldsMap[name] = buildApiFieldDef(f)
		}
	}
	return resp
}

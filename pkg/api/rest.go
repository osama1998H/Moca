package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/moca-framework/moca/pkg/auth"
	"github.com/moca-framework/moca/pkg/document"
	"github.com/moca-framework/moca/pkg/meta"
	"github.com/moca-framework/moca/pkg/observe"
	"github.com/moca-framework/moca/pkg/tenancy"
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
}

// MetaResolver looks up a MetaType by site and doctype name.
// *meta.Registry satisfies this interface.
type MetaResolver interface {
	Get(ctx context.Context, site, doctype string) (*meta.MetaType, error)
}

// ResourceHandler bridges HTTP requests to DocManager CRUD operations.
type ResourceHandler struct {
	crud   CRUDService
	meta   MetaResolver
	perm   PermissionChecker
	logger *slog.Logger
}

// NewResourceHandler creates a ResourceHandler wired to the given Gateway.
func NewResourceHandler(gw *Gateway) *ResourceHandler {
	return &ResourceHandler{
		crud:   gw.DocManager(),
		meta:   gw.Registry(),
		perm:   gw.PermChecker(),
		logger: gw.Logger(),
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
	mux.HandleFunc("GET "+p+"/meta/{doctype}", h.handleMeta)
}

// ── Handlers ────────────────────────────────────────────────────────────────

func (h *ResourceHandler) handleList(w http.ResponseWriter, r *http.Request) {
	mt, site, user, ok := h.resolveRequest(w, r, "read", func(ac *meta.APIConfig) bool { return ac.AllowList })
	if !ok {
		return
	}

	opts, err := parseListParams(r, mt.APIConfig)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PARAMS", err.Error())
		return
	}

	docCtx := document.NewDocContext(r.Context(), site, user)
	docs, total, err := h.crud.GetList(docCtx, mt.Name, opts)
	if err != nil {
		h.handleCRUDError(w, r, err)
		return
	}

	chain := buildTransformerChain(r.Context(), mt)
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
}

func (h *ResourceHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	mt, site, user, ok := h.resolveRequest(w, r, "read", func(ac *meta.APIConfig) bool { return ac.AllowGet })
	if !ok {
		return
	}

	docCtx := document.NewDocContext(r.Context(), site, user)

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

	chain := buildTransformerChain(r.Context(), mt)
	ctx := WithOperationType(r.Context(), OpGet)
	data := doc.AsMap()
	data, err = chain.TransformResponse(ctx, mt, data)
	if err != nil {
		h.handleCRUDError(w, r, transformError("response", err))
		return
	}
	writeSuccess(w, http.StatusOK, data)
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

	values, err := h.decodeBody(w, r)
	if err != nil {
		return // error already written
	}

	chain := buildTransformerChain(r.Context(), mt)
	ctx := WithOperationType(r.Context(), OpCreate)
	values, err = chain.TransformRequest(ctx, mt, values)
	if err != nil {
		writeError(w, http.StatusBadRequest, "TRANSFORM_ERROR", err.Error())
		return
	}

	docCtx := document.NewDocContext(r.Context(), site, user)
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
}

func (h *ResourceHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	mt, site, user, ok := h.resolveRequest(w, r, "write", func(ac *meta.APIConfig) bool { return ac.AllowUpdate })
	if !ok {
		return
	}

	name := r.PathValue("name")
	if name == "" && !mt.IsSingle {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "document name is required")
		return
	}

	values, err := h.decodeBody(w, r)
	if err != nil {
		return
	}

	chain := buildTransformerChain(r.Context(), mt)
	ctx := WithOperationType(r.Context(), OpUpdate)
	values, err = chain.TransformRequest(ctx, mt, values)
	if err != nil {
		writeError(w, http.StatusBadRequest, "TRANSFORM_ERROR", err.Error())
		return
	}

	docCtx := document.NewDocContext(r.Context(), site, user)
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

	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "document name is required")
		return
	}

	docCtx := document.NewDocContext(r.Context(), site, user)
	if err := h.crud.Delete(docCtx, mt.Name, name); err != nil {
		h.handleCRUDError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ResourceHandler) handleMeta(w http.ResponseWriter, r *http.Request) {
	mt, _, _, ok := h.resolveRequest(w, r, "read", nil) // no AllowX gate for meta
	if !ok {
		return
	}
	resp := filterMetaFields(buildMetaResponse(mt), mt, r.Context())
	writeSuccess(w, http.StatusOK, resp)
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

	return mt, site, user, true
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
	Name        string `json:"name"`
	FieldType   string `json:"field_type"`
	Label       string `json:"label"`
	APIAlias    string `json:"api_alias,omitempty"`
	Options     string `json:"options,omitempty"`
	Required    bool   `json:"required"`
	ReadOnly    bool   `json:"read_only"`
	APIReadOnly bool   `json:"api_read_only,omitempty"`
	InAPI       bool   `json:"in_api"`
	Searchable  bool   `json:"searchable,omitempty"`
	Filterable  bool   `json:"filterable,omitempty"`
	Unique      bool   `json:"unique,omitempty"`
}

// apiMetaResponse is the API-safe representation of a MetaType.
type apiMetaResponse struct {
	Name          string        `json:"name"`
	Label         string        `json:"label,omitempty"`
	Description   string        `json:"description,omitempty"`
	Module        string        `json:"module,omitempty"`
	Fields        []apiFieldDef `json:"fields"`
	IsSingle      bool          `json:"is_single"`
	IsSubmittable bool          `json:"is_submittable"`
	IsChildTable  bool          `json:"is_child_table"`
	AllowGet      bool          `json:"allow_get"`
	AllowCreate   bool          `json:"allow_create"`
	AllowUpdate   bool          `json:"allow_update"`
	AllowDelete   bool          `json:"allow_delete"`
	AllowList     bool          `json:"allow_list"`
}

func buildMetaResponse(mt *meta.MetaType) apiMetaResponse {
	resp := apiMetaResponse{
		Name:          mt.Name,
		Label:         mt.Label,
		Description:   mt.Description,
		Module:        mt.Module,
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
		resp.Fields = append(resp.Fields, apiFieldDef{
			Name:        f.Name,
			FieldType:   string(f.FieldType),
			Label:       f.Label,
			Required:    f.Required,
			ReadOnly:    f.ReadOnly,
			APIReadOnly: f.APIReadOnly,
			APIAlias:    f.APIAlias,
			InAPI:       f.InAPI,
			Options:     f.Options,
			Searchable:  f.Searchable,
			Filterable:  f.Filterable,
			Unique:      f.Unique,
		})
	}
	return resp
}

package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/orm"
	pkgsearch "github.com/osama1998H/moca/pkg/search"
	"github.com/osama1998H/moca/pkg/tenancy"
)

type SearchHandler struct {
	search    SearchService
	meta      MetaResolver
	perm      PermissionChecker
	fieldPerm Transformer
}

func NewSearchHandler(gw *Gateway) *SearchHandler {
	return &SearchHandler{
		search:    gw.SearchService(),
		meta:      gw.Registry(),
		perm:      gw.PermChecker(),
		fieldPerm: gw.FieldLevelTransformer(),
	}
}

func (h *SearchHandler) RegisterRoutes(mux *http.ServeMux, version string) {
	mux.HandleFunc("GET /api/"+version+"/search", h.handleSearch)
}

func (h *SearchHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "INVALID_PARAMS", "q is required")
		return
	}

	doctype := strings.TrimSpace(r.URL.Query().Get("doctype"))
	if doctype == "" {
		writeError(w, http.StatusBadRequest, "INVALID_PARAMS", "doctype is required")
		return
	}

	mt, site, _, ok := h.resolveSearchRequest(w, r, doctype)
	if !ok {
		return
	}

	page, limit, filters, err := parseSearchParams(r, mt.APIConfig)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PARAMS", err.Error())
		return
	}

	if h.search == nil {
		writeError(w, http.StatusServiceUnavailable, "SEARCH_UNAVAILABLE", "search backend is unavailable")
		return
	}

	results, total, err := h.search.Search(r.Context(), site.Name, mt, query, filters, page, limit)
	if err != nil {
		var filterErr *pkgsearch.FilterError
		var notSearchable *pkgsearch.NotSearchableError
		switch {
		case errors.Is(err, pkgsearch.ErrUnavailable):
			writeError(w, http.StatusServiceUnavailable, "SEARCH_UNAVAILABLE", "search backend is unavailable")
			return
		case errors.As(err, &filterErr):
			writeError(w, http.StatusBadRequest, "INVALID_PARAMS", filterErr.Error())
			return
		case errors.As(err, &notSearchable):
			writeError(w, http.StatusBadRequest, "DOCTYPE_NOT_SEARCHABLE", notSearchable.Error())
			return
		default:
			if mapErrorResponse(w, err) {
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
			return
		}
	}

	chain := buildTransformerChain(r.Context(), mt, h.fieldPerm)
	ctx := WithOperationType(r.Context(), OpList)
	data := make([]map[string]any, 0, len(results))
	for _, result := range results {
		item := result.Fields
		if item == nil {
			item = map[string]any{
				"name":    result.Name,
				"doctype": result.DocType,
			}
		}
		transformed, err := chain.TransformResponse(ctx, mt, item)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
			return
		}
		transformed["_score"] = result.Score
		data = append(data, transformed)
	}

	offset := (page - 1) * limit
	writeListSuccess(w, data, total, limit, offset)
}

func (h *SearchHandler) resolveSearchRequest(
	w http.ResponseWriter,
	r *http.Request,
	doctype string,
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

	mt, err := h.meta.Get(r.Context(), site.Name, doctype)
	if err != nil {
		if !mapErrorResponse(w, err) {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		}
		return nil, nil, nil, false
	}

	if mt.APIConfig == nil || !mt.APIConfig.Enabled {
		writeError(w, http.StatusNotFound, "DOCTYPE_NOT_FOUND", "doctype not found")
		return nil, nil, nil, false
	}
	if !mt.APIConfig.AllowList {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "this operation is not allowed for "+doctype)
		return nil, nil, nil, false
	}
	if err := h.perm.CheckDocPerm(r.Context(), user, doctype, "read"); err != nil {
		if !mapErrorResponse(w, err) {
			writeError(w, http.StatusForbidden, "PERMISSION_DENIED", "permission denied")
		}
		return nil, nil, nil, false
	}

	return mt, site, user, true
}

func parseSearchParams(r *http.Request, apiCfg *meta.APIConfig) (int, int, []orm.Filter, error) {
	page := 1
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			return 0, 0, nil, fmt.Errorf("page: must be an integer >= 1")
		}
		page = value
	}

	limit := defaultPageSize
	maxLimit := defaultMaxPageSize
	if apiCfg != nil {
		if apiCfg.DefaultPageSize > 0 {
			limit = apiCfg.DefaultPageSize
		}
		if apiCfg.MaxPageSize > 0 {
			maxLimit = apiCfg.MaxPageSize
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return 0, 0, nil, fmt.Errorf("limit: must be a non-negative integer")
		}
		limit = value
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if limit == 0 {
		limit = defaultPageSize
	}

	filters, err := parseFilters(r.URL.Query().Get("filters"))
	if err != nil {
		return 0, 0, nil, err
	}

	return page, limit, filters, nil
}

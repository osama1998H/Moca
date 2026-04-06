package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/osama1998H/moca/pkg/meta"
)

const (
	defaultVersionLimit = 20
	maxVersionLimit     = 100
)

// handleVersions returns paginated version history for a document.
// GET /api/v1/resource/{doctype}/{name}/versions?limit=20&offset=0
func (h *ResourceHandler) handleVersions(w http.ResponseWriter, r *http.Request) {
	mt, site, user, ok := h.resolveRequest(w, r, "read", func(ac *meta.APIConfig) bool { return ac.AllowGet })
	if !ok {
		return
	}

	h.applyDocTypeMiddleware(mt, func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "document name is required")
			return
		}

		if !mt.TrackChanges {
			writeError(w, http.StatusBadRequest, "VERSION_TRACKING_DISABLED",
				fmt.Sprintf("version tracking is not enabled for %s", mt.Name))
			return
		}

		limit, offset, err := parseVersionParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_PARAMS", err.Error())
			return
		}

		docCtx := newDocContext(r.Context(), site, user)
		versions, total, err := h.crud.GetVersions(docCtx, mt.Name, name, limit, offset)
		if err != nil {
			h.handleCRUDError(w, r, err)
			return
		}

		data := make([]map[string]any, len(versions))
		for i, v := range versions {
			data[i] = map[string]any{
				"name":        v.Name,
				"ref_doctype": v.RefDoctype,
				"docname":     v.DocName,
				"data":        v.Data,
				"owner":       v.Owner,
				"creation":    v.Creation,
			}
		}
		writeListSuccess(w, data, total, limit, offset)
	})(w, r)
}

// parseVersionParams extracts limit and offset query parameters for the
// version history endpoint. Defaults: limit=20, offset=0. Max limit=100.
func parseVersionParams(r *http.Request) (limit, offset int, err error) {
	limit = defaultVersionLimit
	offset = 0

	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid limit: %w", err)
		}
		if limit < 1 {
			limit = defaultVersionLimit
		}
		if limit > maxVersionLimit {
			limit = maxVersionLimit
		}
	}

	if v := r.URL.Query().Get("offset"); v != "" {
		offset, err = strconv.Atoi(v)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid offset: %w", err)
		}
		if offset < 0 {
			offset = 0
		}
	}

	return limit, offset, nil
}

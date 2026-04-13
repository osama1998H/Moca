package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
)

const (
	defaultEventLimit = 100
	maxEventLimit     = 500
)

// handleGetEvents returns the event sourcing history for a document.
// GET /api/v1/resource/{doctype}/{name}/events?limit=100&offset=0&event_type=<type>
//
// The endpoint is only available when the doctype's MetaType has EventSourcing enabled.
func (h *ResourceHandler) handleGetEvents(w http.ResponseWriter, r *http.Request) {
	mt, site, _, ok := h.resolveRequest(w, r, "read", func(ac *meta.APIConfig) bool { return ac.AllowGet })
	if !ok {
		return
	}

	h.applyDocTypeMiddleware(mt, func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "document name is required")
			return
		}

		if !mt.EventSourcing {
			writeError(w, http.StatusBadRequest, "EVENT_SOURCING_DISABLED",
				fmt.Sprintf("event sourcing is not enabled for %s", mt.Name))
			return
		}

		if site.Pool == nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
			return
		}

		opts, err := parseEventParams(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_PARAMS", err.Error())
			return
		}

		events, err := document.GetHistory(r.Context(), site.Pool, mt.Name, name, opts)
		if err != nil {
			h.handleCRUDError(w, r, err)
			return
		}

		data := make([]map[string]any, len(events))
		for i, ev := range events {
			data[i] = map[string]any{
				"id":         ev.ID,
				"doctype":    ev.DocType,
				"docname":    ev.DocName,
				"event_type": ev.EventType,
				"payload":    ev.Payload,
				"prev_data":  ev.PrevData,
				"user_id":    ev.UserID,
				"request_id": ev.RequestID,
				"created_at": ev.CreatedAt,
			}
		}
		writeListSuccess(w, data, len(events), opts.Limit, opts.Offset)
	})(w, r)
}

// parseEventParams extracts limit, offset, and event_type query parameters for
// the event history endpoint. Defaults: limit=100, offset=0. Max limit=500.
func parseEventParams(r *http.Request) (document.EventLogQueryOpts, error) {
	opts := document.EventLogQueryOpts{
		Limit:  defaultEventLimit,
		Offset: 0,
	}

	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return opts, fmt.Errorf("invalid limit: %w", err)
		}
		if n < 1 {
			n = defaultEventLimit
		}
		if n > maxEventLimit {
			n = maxEventLimit
		}
		opts.Limit = n
	}

	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return opts, fmt.Errorf("invalid offset: %w", err)
		}
		if n < 0 {
			n = 0
		}
		opts.Offset = n
	}

	if v := r.URL.Query().Get("event_type"); v != "" {
		opts.EventType = v
	}

	return opts, nil
}

package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/orm"
)

// apiReportMeta is the API-safe view of a ReportDef. The Query field is
// intentionally omitted to avoid leaking server-side SQL to the client.
type apiReportMeta struct { //nolint:govet // JSON field order matters for API consistency
	Name        string             `json:"name"`
	DocType     string             `json:"doc_type"`
	Type        string             `json:"type"`
	Columns     []orm.ReportColumn `json:"columns"`
	Filters     []orm.ReportFilter `json:"filters"`
	ChartConfig *orm.ChartConfig   `json:"chart_config,omitempty"`
}

// executeReportRequest is the JSON body for POST /report/{name}/execute.
type executeReportRequest struct {
	Filters map[string]any `json:"filters"`
	Limit   int            `json:"limit"`
	Offset  int            `json:"offset"`
}

const (
	defaultReportLimit = 100
	maxReportLimit     = 1000
)

// ReportHandler serves report metadata and execution endpoints.
type ReportHandler struct {
	reports  *ReportRegistry
	db       *orm.DBManager
	registry *meta.Registry
	perm     PermissionChecker
	logger   *slog.Logger
}

// NewReportHandler creates a ReportHandler with the given dependencies.
func NewReportHandler(reports *ReportRegistry, db *orm.DBManager, registry *meta.Registry, perm PermissionChecker, logger *slog.Logger) *ReportHandler {
	return &ReportHandler{
		reports:  reports,
		db:       db,
		registry: registry,
		perm:     perm,
		logger:   logger,
	}
}

// RegisterRoutes registers report endpoints on the mux.
func (h *ReportHandler) RegisterRoutes(mux *http.ServeMux, version string) {
	p := "/api/" + version
	mux.HandleFunc("GET "+p+"/report/{name}/meta", h.handleMeta)
	mux.HandleFunc("POST "+p+"/report/{name}/execute", h.handleExecute)
}

// handleMeta returns the report definition (columns, filters, chart config).
func (h *ReportHandler) handleMeta(w http.ResponseWriter, r *http.Request) {
	site := SiteFromContext(r.Context())
	if site == nil {
		writeError(w, http.StatusBadRequest, "SITE_REQUIRED", "site context is required")
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication is required")
		return
	}

	name := r.PathValue("name")
	def, ok := h.reports.Get(name)
	if !ok {
		writeError(w, http.StatusNotFound, "REPORT_NOT_FOUND", "report "+name+" not found")
		return
	}

	if err := h.perm.CheckDocPerm(r.Context(), user, def.DocType, "read"); err != nil {
		writeError(w, http.StatusForbidden, "PERMISSION_DENIED", "no read permission on "+def.DocType)
		return
	}

	resp := apiReportMeta{
		Name:        def.Name,
		DocType:     def.DocType,
		Type:        def.Type,
		Columns:     def.Columns,
		Filters:     def.Filters,
		ChartConfig: def.ChartConfig,
	}
	writeSuccess(w, http.StatusOK, resp)
}

// handleExecute runs a query report with the given filter parameters.
func (h *ReportHandler) handleExecute(w http.ResponseWriter, r *http.Request) {
	site := SiteFromContext(r.Context())
	if site == nil {
		writeError(w, http.StatusBadRequest, "SITE_REQUIRED", "site context is required")
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication is required")
		return
	}

	name := r.PathValue("name")
	def, ok := h.reports.Get(name)
	if !ok {
		writeError(w, http.StatusNotFound, "REPORT_NOT_FOUND", "report "+name+" not found")
		return
	}

	if err := h.perm.CheckDocPerm(r.Context(), user, def.DocType, "read"); err != nil {
		writeError(w, http.StatusForbidden, "PERMISSION_DENIED", "no read permission on "+def.DocType)
		return
	}

	var req executeReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}

	// Defaults and caps.
	if req.Limit <= 0 {
		req.Limit = defaultReportLimit
	}
	if req.Limit > maxReportLimit {
		req.Limit = maxReportLimit
	}
	if req.Offset < 0 {
		req.Offset = 0
	}
	if req.Filters == nil {
		req.Filters = make(map[string]any)
	}

	if h.db == nil {
		writeError(w, http.StatusServiceUnavailable, "DB_UNAVAILABLE", "database is unavailable")
		return
	}

	pool, err := h.db.ForSite(r.Context(), site.Name)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "report: failed to get site pool",
			slog.String("site", site.Name), slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}

	allRows, err := orm.ExecuteQueryReport(r.Context(), pool, def, req.Filters)
	if err != nil {
		if !mapErrorResponse(w, err) {
			h.logger.ErrorContext(r.Context(), "report: execution failed",
				slog.String("report", name), slog.Any("error", err))
			writeError(w, http.StatusBadRequest, "REPORT_ERROR", err.Error())
		}
		return
	}

	// Apply pagination by slicing.
	total := len(allRows)
	start := req.Offset
	if start > total {
		start = total
	}
	end := start + req.Limit
	if end > total {
		end = total
	}
	page := allRows[start:end]

	writeListSuccess(w, page, total, req.Limit, req.Offset)
}

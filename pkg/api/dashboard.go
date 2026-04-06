package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/orm"
)

// allowedAggregates is the whitelist of SQL aggregate functions allowed in
// dashboard widget queries. Only these are permitted to prevent injection.
var allowedAggregates = map[string]bool{
	"count": true,
	"sum":   true,
	"avg":   true,
	"min":   true,
	"max":   true,
}

// identRe validates that a SQL identifier contains only word characters.
var identRe = regexp.MustCompile(`^\w+$`)

// DashboardHandler serves dashboard definition and widget data endpoints.
type DashboardHandler struct {
	dashboards *DashboardRegistry
	db         *orm.DBManager
	registry   *meta.Registry
	crud       CRUDService
	perm       PermissionChecker
	logger     *slog.Logger
}

// NewDashboardHandler creates a DashboardHandler using dependencies from the Gateway.
func NewDashboardHandler(gw *Gateway) *DashboardHandler {
	return &DashboardHandler{
		dashboards: gw.DashboardRegistry(),
		db:         nil, // set via SetDBManager
		registry:   gw.Registry(),
		crud:       gw.DocManager(),
		perm:       gw.PermChecker(),
		logger:     gw.Logger(),
	}
}

// SetDBManager sets the database manager for aggregate widget queries.
func (h *DashboardHandler) SetDBManager(db *orm.DBManager) {
	h.db = db
}

// RegisterRoutes registers dashboard endpoints on the mux.
func (h *DashboardHandler) RegisterRoutes(mux *http.ServeMux, version string) {
	p := "/api/" + version
	mux.HandleFunc("GET "+p+"/dashboard/{name}", h.handleGetDashboard)
	mux.HandleFunc("GET "+p+"/dashboard/{name}/widget/{idx}", h.handleGetWidget)
}

// handleGetDashboard returns the full dashboard definition.
func (h *DashboardHandler) handleGetDashboard(w http.ResponseWriter, r *http.Request) {
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
	def, ok := h.dashboards.Get(name)
	if !ok {
		writeError(w, http.StatusNotFound, "DASHBOARD_NOT_FOUND", "dashboard "+name+" not found")
		return
	}

	writeSuccess(w, http.StatusOK, def)
}

// handleGetWidget returns computed data for a specific dashboard widget.
func (h *DashboardHandler) handleGetWidget(w http.ResponseWriter, r *http.Request) {
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

	dashName := r.PathValue("name")
	def, ok := h.dashboards.Get(dashName)
	if !ok {
		writeError(w, http.StatusNotFound, "DASHBOARD_NOT_FOUND", "dashboard "+dashName+" not found")
		return
	}

	idxStr := r.PathValue("idx")
	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 0 || idx >= len(def.Widgets) {
		writeError(w, http.StatusBadRequest, "INVALID_WIDGET_INDEX",
			fmt.Sprintf("widget index must be 0-%d", len(def.Widgets)-1))
		return
	}

	widget := def.Widgets[idx]

	switch widget.Type {
	case "number_card":
		h.handleNumberCard(w, r, site.Name, user, widget)
	case "chart":
		h.handleChartWidget(w, r, site.Name, user, widget)
	case "list":
		h.handleListWidget(w, r, site.Name, user, widget)
	case "shortcut":
		writeSuccess(w, http.StatusOK, map[string]any{"type": "shortcut"})
	default:
		writeError(w, http.StatusBadRequest, "UNKNOWN_WIDGET_TYPE", "unknown widget type: "+widget.Type)
	}
}

// handleNumberCard computes an aggregate value (count, sum, avg, etc.)
// for a number card widget.
func (h *DashboardHandler) handleNumberCard(w http.ResponseWriter, r *http.Request, siteName string, user *auth.User, widget DashWidget) {
	doctype, _ := widget.Config["doctype"].(string)
	if doctype == "" {
		writeError(w, http.StatusBadRequest, "INVALID_CONFIG", "number_card requires doctype in config")
		return
	}

	if err := h.perm.CheckDocPerm(r.Context(), user, doctype, "read"); err != nil {
		writeError(w, http.StatusForbidden, "PERMISSION_DENIED", "no read permission on "+doctype)
		return
	}

	aggregate, _ := widget.Config["aggregate"].(string)
	aggregate = strings.ToLower(aggregate)
	if aggregate == "" {
		aggregate = "count"
	}
	if !allowedAggregates[aggregate] {
		writeError(w, http.StatusBadRequest, "INVALID_CONFIG", "unsupported aggregate: "+aggregate)
		return
	}

	field, _ := widget.Config["field"].(string)

	// Build the aggregate expression.
	var expr string
	if aggregate == "count" {
		expr = "COUNT(*)"
	} else {
		if field == "" {
			writeError(w, http.StatusBadRequest, "INVALID_CONFIG", aggregate+" requires field in config")
			return
		}
		if !identRe.MatchString(field) {
			writeError(w, http.StatusBadRequest, "INVALID_CONFIG", "invalid field name")
			return
		}
		expr = fmt.Sprintf("%s(%s)", strings.ToUpper(aggregate), field)
	}

	value, err := h.executeAggregate(r.Context(), siteName, doctype, expr, widget.Config)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "dashboard: number_card query failed",
			slog.String("doctype", doctype), slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "WIDGET_ERROR", "failed to compute widget data")
		return
	}

	label, _ := widget.Config["label"].(string)
	if label == "" {
		label = doctype
	}

	writeSuccess(w, http.StatusOK, map[string]any{
		"type":  "number_card",
		"value": value,
		"label": label,
	})
}

// handleChartWidget computes time-series aggregate data for a chart widget.
func (h *DashboardHandler) handleChartWidget(w http.ResponseWriter, r *http.Request, siteName string, user *auth.User, widget DashWidget) {
	doctype, _ := widget.Config["doctype"].(string)
	if doctype == "" {
		writeError(w, http.StatusBadRequest, "INVALID_CONFIG", "chart requires doctype in config")
		return
	}

	if err := h.perm.CheckDocPerm(r.Context(), user, doctype, "read"); err != nil {
		writeError(w, http.StatusForbidden, "PERMISSION_DENIED", "no read permission on "+doctype)
		return
	}

	timeField, _ := widget.Config["time_field"].(string)
	if timeField == "" {
		timeField = "creation"
	}
	if !identRe.MatchString(timeField) {
		writeError(w, http.StatusBadRequest, "INVALID_CONFIG", "invalid time_field name")
		return
	}

	aggregate, _ := widget.Config["aggregate"].(string)
	aggregate = strings.ToLower(aggregate)
	if aggregate == "" {
		aggregate = "count"
	}
	if !allowedAggregates[aggregate] {
		writeError(w, http.StatusBadRequest, "INVALID_CONFIG", "unsupported aggregate: "+aggregate)
		return
	}

	period, _ := widget.Config["period"].(string)
	if period == "" {
		period = "day"
	}
	truncUnit := "day"
	switch period {
	case "day", "week", "month", "year":
		truncUnit = period
	default:
		writeError(w, http.StatusBadRequest, "INVALID_CONFIG", "unsupported period: "+period)
		return
	}

	field, _ := widget.Config["field"].(string)
	var aggExpr string
	if aggregate == "count" {
		aggExpr = "COUNT(*)"
	} else {
		if field == "" || !identRe.MatchString(field) {
			writeError(w, http.StatusBadRequest, "INVALID_CONFIG", aggregate+" requires a valid field in config")
			return
		}
		aggExpr = fmt.Sprintf("%s(%s)", strings.ToUpper(aggregate), field)
	}

	if h.db == nil {
		writeError(w, http.StatusServiceUnavailable, "DB_UNAVAILABLE", "database is unavailable")
		return
	}

	pool, err := h.db.ForSite(r.Context(), siteName)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "dashboard: failed to get site pool",
			slog.String("site", siteName), slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}

	sql := fmt.Sprintf(
		"SELECT date_trunc('%s', %s) AS period, %s AS value FROM tab_%s GROUP BY period ORDER BY period",
		truncUnit, timeField, aggExpr, doctype,
	)

	rows, err := pool.Query(r.Context(), sql)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "dashboard: chart query failed",
			slog.String("doctype", doctype), slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "WIDGET_ERROR", "failed to compute chart data")
		return
	}
	defer rows.Close()

	var data []map[string]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			h.logger.ErrorContext(r.Context(), "dashboard: chart row scan failed",
				slog.Any("error", err))
			writeError(w, http.StatusInternalServerError, "WIDGET_ERROR", "failed to read chart data")
			return
		}
		data = append(data, map[string]any{
			"period": vals[0],
			"value":  vals[1],
		})
	}
	if err := rows.Err(); err != nil {
		h.logger.ErrorContext(r.Context(), "dashboard: chart iterate failed",
			slog.Any("error", err))
		writeError(w, http.StatusInternalServerError, "WIDGET_ERROR", "failed to read chart data")
		return
	}

	if data == nil {
		data = []map[string]any{}
	}

	writeSuccess(w, http.StatusOK, map[string]any{
		"type": "chart",
		"data": data,
	})
}

// handleListWidget returns recent documents for a list widget.
func (h *DashboardHandler) handleListWidget(w http.ResponseWriter, r *http.Request, siteName string, user *auth.User, widget DashWidget) {
	doctype, _ := widget.Config["doctype"].(string)
	if doctype == "" {
		writeError(w, http.StatusBadRequest, "INVALID_CONFIG", "list requires doctype in config")
		return
	}

	if err := h.perm.CheckDocPerm(r.Context(), user, doctype, "read"); err != nil {
		writeError(w, http.StatusForbidden, "PERMISSION_DENIED", "no read permission on "+doctype)
		return
	}

	limit := 5
	if l, ok := widget.Config["limit"].(float64); ok && l > 0 {
		limit = int(l)
		if limit > 20 {
			limit = 20
		}
	}

	var fields []string
	if fs, ok := widget.Config["fields"].([]any); ok {
		for _, f := range fs {
			if s, ok := f.(string); ok {
				fields = append(fields, s)
			}
		}
	}
	if len(fields) == 0 {
		fields = []string{"name", "modified"}
	}

	site := SiteFromContext(r.Context())
	docCtx := document.NewDocContext(r.Context(), site, user)
	docCtx.RequestID = RequestIDFromContext(r.Context())

	docs, total, err := h.crud.GetList(docCtx, doctype, document.ListOptions{
		Fields:   fields,
		Limit:    limit,
		OrderBy:  "modified",
		OrderDir: "DESC",
	})
	if err != nil {
		if !mapErrorResponse(w, err) {
			h.logger.ErrorContext(r.Context(), "dashboard: list widget query failed",
				slog.String("doctype", doctype), slog.Any("error", err))
			writeError(w, http.StatusInternalServerError, "WIDGET_ERROR", "failed to fetch list data")
		}
		return
	}

	data := make([]map[string]any, len(docs))
	for i, d := range docs {
		data[i] = d.AsMap()
	}

	writeSuccess(w, http.StatusOK, map[string]any{
		"type":  "list",
		"data":  data,
		"total": total,
	})
}

// executeAggregate runs a single aggregate query against a DocType table.
func (h *DashboardHandler) executeAggregate(ctx context.Context, siteName, doctype, expr string, config map[string]any) (any, error) {
	if h.db == nil {
		return nil, fmt.Errorf("database is unavailable")
	}

	pool, err := h.db.ForSite(ctx, siteName)
	if err != nil {
		return nil, fmt.Errorf("get site pool: %w", err)
	}

	sql := fmt.Sprintf("SELECT %s FROM tab_%s", expr, doctype)

	// Apply simple equality filters from config if present.
	var args []any
	if filters, ok := config["filters"].(map[string]any); ok && len(filters) > 0 {
		var conditions []string
		idx := 0
		for k, v := range filters {
			if !identRe.MatchString(k) {
				continue
			}
			idx++
			conditions = append(conditions, fmt.Sprintf("%s = $%d", k, idx))
			args = append(args, v)
		}
		if len(conditions) > 0 {
			sql += " WHERE " + strings.Join(conditions, " AND ")
		}
	}

	var value any
	if err := pool.QueryRow(ctx, sql, args...).Scan(&value); err != nil {
		return nil, fmt.Errorf("aggregate query: %w", err)
	}

	return value, nil
}

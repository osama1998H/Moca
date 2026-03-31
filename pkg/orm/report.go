package orm

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ── ReportDef types ─────────────────────────────────────────────────────────

// ReportDef declares a query report. Matches MOCA_SYSTEM_DESIGN.md §10.2.
type ReportDef struct { //nolint:govet // field order matches MOCA_SYSTEM_DESIGN.md §10.2
	Name        string         `json:"name"`
	DocType     string         `json:"doc_type"`
	Type        string         `json:"type"` // "QueryReport" or "ScriptReport"
	Columns     []ReportColumn `json:"columns"`
	Filters     []ReportFilter `json:"filters"`
	Query       string         `json:"query,omitempty"`       // SQL template for QueryReport
	DataSource  string         `json:"data_source,omitempty"` // Go func name for ScriptReport
	ChartConfig *ChartConfig   `json:"chart_config,omitempty"`
	IsCacheable bool           `json:"is_cacheable"`
	CacheTTL    time.Duration  `json:"cache_ttl"`
}

// ReportColumn describes a result column in a report.
type ReportColumn struct {
	FieldName string `json:"field_name"`
	Label     string `json:"label"`
	FieldType string `json:"field_type"`
	Width     int    `json:"width,omitempty"`
}

// ReportFilter describes a user-configurable filter parameter for a report.
type ReportFilter struct {
	Default   any    `json:"default,omitempty"`
	FieldName string `json:"field_name"`
	Label     string `json:"label"`
	FieldType string `json:"field_type"`
	Required  bool   `json:"required"`
}

// ChartConfig holds optional chart configuration for a report.
type ChartConfig struct {
	Type string `json:"type"`
}

// ── SQL template parsing ────────────────────────────────────────────────────

// namedParamRe matches %(param_name)s placeholders in SQL templates.
var namedParamRe = regexp.MustCompile(`%\((\w+)\)s`)

// ddlRe detects DDL/DML keywords that are forbidden in QueryReport SQL.
// Word boundaries prevent false positives like "updated_at" matching "UPDATE".
var ddlRe = regexp.MustCompile(`(?i)\b(DROP|ALTER|TRUNCATE|DELETE|UPDATE|INSERT)\b`)

// ── ExecuteQueryReport ──────────────────────────────────────────────────────

// ExecuteQueryReport runs a QueryReport definition against the given pool.
// It validates the report type, checks required parameters, rejects DDL,
// parses %(param)s placeholders into positional $N, and returns rows as maps.
func ExecuteQueryReport(ctx context.Context, pool *pgxpool.Pool, def ReportDef, params map[string]any) ([]map[string]any, error) {
	// 1. Type validation.
	if def.Type != "QueryReport" {
		return nil, fmt.Errorf("report: type %q not supported (only QueryReport is supported until MS-28)", def.Type)
	}

	if def.Query == "" {
		return nil, fmt.Errorf("report: %q has empty query", def.Name)
	}

	// 2. Required parameter validation.
	if err := validateReportParams(def.Filters, params); err != nil {
		return nil, err
	}

	// 3. DDL keyword rejection (defense-in-depth).
	if ddlRe.MatchString(def.Query) {
		return nil, fmt.Errorf("report: %q query contains forbidden DDL/DML keyword", def.Name)
	}

	// 4. Parse %(param)s → $N and build args.
	sql, args, err := parseReportSQL(def.Query, params)
	if err != nil {
		return nil, fmt.Errorf("report: %q: %w", def.Name, err)
	}

	// 5. Execute.
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("report: %q: execute: %w", def.Name, err)
	}
	defer rows.Close()

	// 6. Scan into []map[string]any.
	fieldDescs := rows.FieldDescriptions()
	colNames := make([]string, len(fieldDescs))
	for i, fd := range fieldDescs {
		colNames[i] = fd.Name
	}

	var results []map[string]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("report: %q: scan row: %w", def.Name, err)
		}
		row := make(map[string]any, len(colNames))
		for i, col := range colNames {
			row[col] = vals[i]
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("report: %q: iterate rows: %w", def.Name, err)
	}

	return results, nil
}

// validateReportParams checks that all required filter parameters are present.
func validateReportParams(filters []ReportFilter, params map[string]any) error {
	var missing []string
	for _, f := range filters {
		if !f.Required {
			continue
		}
		if _, ok := params[f.FieldName]; !ok {
			missing = append(missing, f.FieldName)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("report: missing required parameters: %s", strings.Join(missing, ", "))
	}
	return nil
}

// parseReportSQL converts %(param)s placeholders to positional $N placeholders
// and builds the corresponding argument slice. Each occurrence of a placeholder
// gets its own $N (even repeated params) which is standard pgx behavior.
func parseReportSQL(query string, params map[string]any) (string, []any, error) {
	var args []any
	var parseErr error
	idx := 0

	result := namedParamRe.ReplaceAllStringFunc(query, func(match string) string {
		if parseErr != nil {
			return match
		}
		sub := namedParamRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			parseErr = fmt.Errorf("malformed placeholder %q", match)
			return match
		}
		name := sub[1]
		val, ok := params[name]
		if !ok {
			parseErr = fmt.Errorf("unknown parameter %q in query template", name)
			return match
		}
		idx++
		args = append(args, val)
		return fmt.Sprintf("$%d", idx)
	})

	if parseErr != nil {
		return "", nil, parseErr
	}
	return result, args, nil
}

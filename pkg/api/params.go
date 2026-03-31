package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/moca-framework/moca/pkg/document"
	"github.com/moca-framework/moca/pkg/meta"
	"github.com/moca-framework/moca/pkg/orm"
)

// validOperators is the set of operators accepted in filter expressions.
// Mirrors the constants in pkg/orm/query.go.
var validOperators = map[orm.Operator]struct{}{
	orm.OpEqual:        {},
	orm.OpNotEqual:     {},
	orm.OpGreater:      {},
	orm.OpLess:         {},
	orm.OpGreaterOrEq:  {},
	orm.OpLessOrEq:     {},
	orm.OpLike:         {},
	orm.OpNotLike:      {},
	orm.OpIn:           {},
	orm.OpNotIn:        {},
	orm.OpBetween:      {},
	orm.OpIsNull:       {},
	orm.OpIsNotNull:    {},
	orm.OpJSONContains: {},
}

// defaultPageSize is used when APIConfig.DefaultPageSize is zero.
const defaultPageSize = 20

// defaultMaxPageSize is used when APIConfig.MaxPageSize is zero.
const defaultMaxPageSize = 100

// parseFilters parses a Frappe-style filter string: [["field","op","value"], ...].
// An empty string returns nil, nil.
func parseFilters(raw string) ([]orm.Filter, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	// Unmarshal into raw JSON arrays for flexible value types.
	var outer []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &outer); err != nil {
		return nil, fmt.Errorf("filters: invalid JSON: %w", err)
	}

	filters := make([]orm.Filter, 0, len(outer))
	for i, raw := range outer {
		var triple [3]json.RawMessage
		if err := json.Unmarshal(raw, &triple); err != nil {
			return nil, fmt.Errorf("filters[%d]: expected 3-element array: %w", i, err)
		}

		// field (string)
		var field string
		if err := json.Unmarshal(triple[0], &field); err != nil {
			return nil, fmt.Errorf("filters[%d]: field must be a string: %w", i, err)
		}
		if field == "" {
			return nil, fmt.Errorf("filters[%d]: field must not be empty", i)
		}

		// operator (string)
		var opStr string
		if err := json.Unmarshal(triple[1], &opStr); err != nil {
			return nil, fmt.Errorf("filters[%d]: operator must be a string: %w", i, err)
		}
		op := orm.Operator(strings.ToLower(opStr))
		if _, ok := validOperators[op]; !ok {
			return nil, fmt.Errorf("filters[%d]: unsupported operator %q", i, opStr)
		}

		// value (any)
		var value any
		if err := json.Unmarshal(triple[2], &value); err != nil {
			return nil, fmt.Errorf("filters[%d]: invalid value: %w", i, err)
		}

		filters = append(filters, orm.Filter{
			Field:    field,
			Operator: op,
			Value:    value,
		})
	}

	return filters, nil
}

// parseListParams extracts ListOptions from the HTTP request query string.
// apiCfg may be nil; defaults are used for page size limits.
func parseListParams(r *http.Request, apiCfg *meta.APIConfig) (document.ListOptions, error) {
	q := r.URL.Query()
	var opts document.ListOptions

	// Determine page-size bounds.
	pageSize := defaultPageSize
	maxPage := defaultMaxPageSize
	if apiCfg != nil {
		if apiCfg.DefaultPageSize > 0 {
			pageSize = apiCfg.DefaultPageSize
		}
		if apiCfg.MaxPageSize > 0 {
			maxPage = apiCfg.MaxPageSize
		}
	}

	// limit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return opts, fmt.Errorf("limit: must be an integer")
		}
		if n < 0 {
			return opts, fmt.Errorf("limit: must be non-negative")
		}
		pageSize = n
	}
	if pageSize > maxPage {
		pageSize = maxPage
	}
	opts.Limit = pageSize

	// offset
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return opts, fmt.Errorf("offset: must be an integer")
		}
		if n < 0 {
			return opts, fmt.Errorf("offset: must be non-negative")
		}
		opts.Offset = n
	}

	// order_by — e.g. "modified desc" or "name asc"
	if v := q.Get("order_by"); v != "" {
		parts := strings.Fields(v)
		opts.OrderBy = parts[0]
		if len(parts) >= 2 {
			dir := strings.ToUpper(parts[1])
			if dir != "ASC" && dir != "DESC" {
				return opts, fmt.Errorf("order_by: direction must be ASC or DESC")
			}
			opts.OrderDir = dir
		} else {
			opts.OrderDir = "DESC"
		}
	}

	// fields — comma-separated or JSON array
	if v := q.Get("fields"); v != "" {
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, "[") {
			var arr []string
			if err := json.Unmarshal([]byte(v), &arr); err != nil {
				return opts, fmt.Errorf("fields: invalid JSON array: %w", err)
			}
			opts.Fields = arr
		} else {
			for _, f := range strings.Split(v, ",") {
				f = strings.TrimSpace(f)
				if f != "" {
					opts.Fields = append(opts.Fields, f)
				}
			}
		}
	}

	// filters — Frappe-style JSON
	if v := q.Get("filters"); v != "" {
		filters, err := parseFilters(v)
		if err != nil {
			return opts, err
		}
		opts.AdvancedFilters = filters
	}

	return opts, nil
}

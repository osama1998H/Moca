package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/meilisearch/meilisearch-go"

	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/orm"
)

type NotSearchableError struct {
	Doctype string
}

func (e *NotSearchableError) Error() string {
	return fmt.Sprintf("doctype %q is not searchable", e.Doctype)
}

type FilterError struct {
	Message string
}

func (e *FilterError) Error() string {
	return e.Message
}

// SearchResult is the normalized search hit returned to the API layer.
type SearchResult struct {
	Fields  map[string]any `json:"fields"`
	Name    string         `json:"name"`
	DocType string         `json:"doctype"`
	Score   float64        `json:"score,omitempty"`
}

type QueryService struct {
	client *Client
}

func NewQueryService(client *Client) *QueryService {
	return &QueryService{client: client}
}

func (s *QueryService) Search(
	ctx context.Context,
	site string,
	mt *meta.MetaType,
	query string,
	filters []orm.Filter,
	page, limit int,
) ([]SearchResult, int, error) {
	if s == nil || !s.client.available() {
		return nil, 0, ErrUnavailable
	}
	if !hasSearchableFields(mt) {
		return nil, 0, &NotSearchableError{Doctype: mt.Name}
	}
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}

	filterExpr, err := buildFilterExpression(mt, filters)
	if err != nil {
		return nil, 0, err
	}

	req := &meilisearch.SearchRequest{
		Limit:            int64(limit),
		Offset:           int64((page - 1) * limit),
		ShowRankingScore: true,
	}
	if filterExpr != nil {
		req.Filter = filterExpr
	}

	resp, err := s.client.svc.Index(IndexName(site, mt.Name)).SearchWithContext(ctx, query, req)
	if err != nil {
		return nil, 0, fmt.Errorf("search %q: %w", mt.Name, err)
	}

	results := make([]SearchResult, 0, len(resp.Hits))
	for _, hit := range resp.Hits {
		fields := make(map[string]any)
		if err := hit.DecodeInto(&fields); err != nil {
			return nil, 0, fmt.Errorf("decode search hit: %w", err)
		}

		result := SearchResult{
			Fields:  fields,
			Name:    stringFromMap(fields, "name"),
			DocType: stringFromMap(fields, "doctype"),
		}
		if rawScore, ok := hit["_rankingScore"]; ok {
			_ = json.Unmarshal(rawScore, &result.Score)
		}
		if result.DocType == "" {
			result.DocType = mt.Name
			result.Fields["doctype"] = mt.Name
		}
		if result.Name != "" {
			result.Fields["name"] = result.Name
		}
		results = append(results, result)
	}

	total := int(resp.EstimatedTotalHits)
	if total == 0 && resp.TotalHits > 0 {
		total = int(resp.TotalHits)
	}

	return results, total, nil
}

func buildFilterExpression(mt *meta.MetaType, filters []orm.Filter) (any, error) {
	if len(filters) == 0 {
		return nil, nil
	}

	allowed := make(map[string]struct{}, len(filterableAttributes(mt)))
	for _, field := range filterableAttributes(mt) {
		allowed[field] = struct{}{}
	}

	parts := make([]string, 0, len(filters))
	for _, filter := range filters {
		if _, ok := allowed[filter.Field]; !ok {
			return nil, &FilterError{Message: fmt.Sprintf("filters: field %q is not filterable", filter.Field)}
		}
		part, err := buildFilterPart(filter)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}

	return parts, nil
}

func buildFilterPart(filter orm.Filter) (string, error) {
	field := filter.Field

	switch filter.Operator {
	case orm.OpEqual, orm.OpNotEqual, orm.OpGreater, orm.OpLess, orm.OpGreaterOrEq, orm.OpLessOrEq:
		return fmt.Sprintf("%s %s %s", field, string(filter.Operator), formatFilterValue(filter.Value)), nil
	case orm.OpIn:
		values, err := sliceValues(filter.Value)
		if err != nil {
			return "", &FilterError{Message: fmt.Sprintf("filters: %s on %q: %v", filter.Operator, filter.Field, err)}
		}
		return fmt.Sprintf("%s IN [%s]", field, joinFilterValues(values)), nil
	case orm.OpNotIn:
		values, err := sliceValues(filter.Value)
		if err != nil {
			return "", &FilterError{Message: fmt.Sprintf("filters: %s on %q: %v", filter.Operator, filter.Field, err)}
		}
		return fmt.Sprintf("%s NOT IN [%s]", field, joinFilterValues(values)), nil
	case orm.OpBetween:
		values, err := sliceValues(filter.Value)
		if err != nil || len(values) != 2 {
			return "", &FilterError{Message: fmt.Sprintf("filters: between on %q requires exactly two values", filter.Field)}
		}
		return fmt.Sprintf("%s %s TO %s", field, formatFilterValue(values[0]), formatFilterValue(values[1])), nil
	case orm.OpIsNull:
		return fmt.Sprintf("%s IS NULL", field), nil
	case orm.OpIsNotNull:
		return fmt.Sprintf("%s IS NOT NULL", field), nil
	default:
		return "", &FilterError{Message: fmt.Sprintf("filters: unsupported operator %q", filter.Operator)}
	}
}

func formatFilterValue(value any) string {
	switch typed := value.(type) {
	case string:
		return quoteFilterString(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32)
	case int:
		return strconv.Itoa(typed)
	case int8, int16, int32, int64:
		return fmt.Sprintf("%d", typed)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case nil:
		return "null"
	default:
		return quoteFilterString(fmt.Sprintf("%v", typed))
	}
}

func quoteFilterString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

func joinFilterValues(values []any) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, formatFilterValue(value))
	}
	return strings.Join(parts, ", ")
}

func sliceValues(value any) ([]any, error) {
	switch typed := value.(type) {
	case []any:
		return typed, nil
	case []string:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result, nil
	case []int:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result, nil
	default:
		return nil, errors.New("value must be an array")
	}
}

func stringFromMap(fields map[string]any, key string) string {
	value, ok := fields[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprintf("%v", typed)
	}
}

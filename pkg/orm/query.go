package orm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ── Operator type and constants ──────────────────────────────────────────────

// Operator represents a comparison operator in a query filter.
type Operator string

const (
	OpEqual        Operator = "="
	OpNotEqual     Operator = "!="
	OpGreater      Operator = ">"
	OpLess         Operator = "<"
	OpGreaterOrEq  Operator = ">="
	OpLessOrEq     Operator = "<="
	OpLike         Operator = "like"
	OpNotLike      Operator = "not like"
	OpIn           Operator = "in"
	OpNotIn        Operator = "not in"
	OpBetween      Operator = "between"
	OpIsNull       Operator = "is"
	OpIsNotNull    Operator = "is not"
	OpJSONContains Operator = "@>"
	OpFullText     Operator = "@@"
)

// validOperators is the exhaustive set of supported operators.
var validOperators = map[Operator]struct{}{
	OpEqual: {}, OpNotEqual: {}, OpGreater: {}, OpLess: {},
	OpGreaterOrEq: {}, OpLessOrEq: {},
	OpLike: {}, OpNotLike: {},
	OpIn: {}, OpNotIn: {},
	OpBetween: {},
	OpIsNull:  {}, OpIsNotNull: {},
	OpJSONContains: {},
	OpFullText:     {},
}

// ── Filter and OrderClause ───────────────────────────────────────────────────

// Filter represents a single WHERE condition.
type Filter struct {
	Value    any
	Field    string
	Operator Operator
}

// OrderClause represents a single ORDER BY expression.
type OrderClause struct {
	Field     string
	Direction string // "ASC" or "DESC"
}

// ── MetaProvider interface ───────────────────────────────────────────────────

// QueryMeta holds the MetaType information needed by the QueryBuilder to
// validate fields and generate SQL. This decouples the QueryBuilder from
// the meta package to avoid an import cycle (meta → orm → meta).
type QueryMeta struct {
	// ValidColumns is the set of column names that are valid for queries.
	// Includes standard columns and user-defined storable fields.
	ValidColumns map[string]struct{}
	// Name is the DocType name (e.g. "SalesOrder").
	Name string
	// TableName is the PostgreSQL table name (e.g. "tab_sales_order").
	TableName string
	// IsChildTable indicates whether this is a child document table.
	IsChildTable bool
}

// MetaProvider resolves a DocType into query metadata. The meta.Registry
// satisfies this interface via an adapter (see QueryMetaAdapter).
type MetaProvider interface {
	QueryMeta(ctx context.Context, site, doctype string) (*QueryMeta, error)
}

// ── QueryBuilder ─────────────────────────────────────────────────────────────

// QueryBuilder constructs parameterized SQL queries driven by MetaType field
// definitions. It validates field names, generates safe $N placeholders, and
// supports 15 filter operators.
//
// Usage:
//
//	sql, args, err := orm.NewQueryBuilder(provider, "site1").
//	    For("SalesOrder").
//	    Fields("name", "customer", "total").
//	    Where(orm.Filter{Field: "status", Operator: orm.OpEqual, Value: "Draft"}).
//	    OrderBy("creation", "DESC").
//	    Limit(50).
//	    Build(ctx)
type QueryBuilder struct {
	provider MetaProvider
	err      error // first error wins; subsequent fluent calls are no-ops
	site     string
	doctype  string
	fields   []string
	filters  []Filter
	orderBy  []OrderClause
	groupBy  []string
	limit    int
	offset   int
}

// NewQueryBuilder creates a QueryBuilder for the given site.
// The default limit is 20 (matching GetList defaults).
func NewQueryBuilder(provider MetaProvider, site string) *QueryBuilder {
	return &QueryBuilder{
		provider: provider,
		site:     site,
		limit:    20,
	}
}

// For sets the target DocType. MetaType resolution is deferred to Build().
func (qb *QueryBuilder) For(doctype string) *QueryBuilder {
	if qb.err != nil {
		return qb
	}
	if doctype == "" {
		qb.err = errors.New("querybuilder: doctype must not be empty")
		return qb
	}
	qb.doctype = doctype
	return qb
}

// Fields specifies which columns to SELECT. If never called, Build() defaults
// to SELECT *. Field names are validated against the MetaType at Build() time.
func (qb *QueryBuilder) Fields(fields ...string) *QueryBuilder {
	if qb.err != nil {
		return qb
	}
	qb.fields = append(qb.fields, fields...)
	return qb
}

// Where appends one or more filter conditions. Operator constants are validated
// immediately; field names are validated at Build() time against the MetaType.
func (qb *QueryBuilder) Where(filters ...Filter) *QueryBuilder {
	if qb.err != nil {
		return qb
	}
	for _, f := range filters {
		if _, ok := validOperators[f.Operator]; !ok {
			qb.err = fmt.Errorf("querybuilder: unknown operator %q", f.Operator)
			return qb
		}
		if f.Operator == OpFullText {
			qb.err = errors.New("querybuilder: full-text search (@@) not available until MS-15")
			return qb
		}
	}
	qb.filters = append(qb.filters, filters...)
	return qb
}

// OrderBy appends a sort clause. Direction is normalized to uppercase and
// defaults to "ASC" if empty. Only "ASC" and "DESC" are accepted.
func (qb *QueryBuilder) OrderBy(field, dir string) *QueryBuilder {
	if qb.err != nil {
		return qb
	}
	d := strings.ToUpper(strings.TrimSpace(dir))
	if d == "" {
		d = "ASC"
	}
	if d != "ASC" && d != "DESC" {
		qb.err = fmt.Errorf("querybuilder: invalid order direction %q", dir)
		return qb
	}
	qb.orderBy = append(qb.orderBy, OrderClause{Field: field, Direction: d})
	return qb
}

// GroupBy specifies GROUP BY columns. Validated at Build() time.
func (qb *QueryBuilder) GroupBy(fields ...string) *QueryBuilder {
	if qb.err != nil {
		return qb
	}
	qb.groupBy = append(qb.groupBy, fields...)
	return qb
}

// Limit sets the maximum number of rows. Clamped to [1, 100]; values ≤ 0
// reset to the default of 20.
func (qb *QueryBuilder) Limit(n int) *QueryBuilder {
	if qb.err != nil {
		return qb
	}
	switch {
	case n <= 0:
		qb.limit = 20
	case n > 100:
		qb.limit = 100
	default:
		qb.limit = n
	}
	return qb
}

// Offset sets the number of rows to skip. Negative values are clamped to 0.
func (qb *QueryBuilder) Offset(n int) *QueryBuilder {
	if qb.err != nil {
		return qb
	}
	if n < 0 {
		n = 0
	}
	qb.offset = n
	return qb
}

// Build resolves the MetaType, validates all fields, and generates a
// parameterized SELECT query. Returns the SQL string and positional arguments.
func (qb *QueryBuilder) Build(ctx context.Context) (string, []any, error) {
	rq, err := qb.resolve(ctx)
	if err != nil {
		return "", nil, err
	}

	// SELECT clause.
	selectSQL, err := qb.buildSelectClause(rq)
	if err != nil {
		return "", nil, err
	}

	// ORDER BY clause.
	orderSQL, err := qb.buildOrderByClause(rq)
	if err != nil {
		return "", nil, err
	}

	// LIMIT $N OFFSET $N+1
	argIdx := len(rq.whereArgs) + 1
	limitOffsetSQL := fmt.Sprintf("LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args := make([]any, 0, len(rq.whereArgs)+2)
	args = append(args, rq.whereArgs...)
	args = append(args, qb.limit, qb.offset)

	// Assemble.
	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(selectSQL)
	sb.WriteString(" FROM ")
	sb.WriteString(rq.quotedTable)
	if rq.whereSQL != "" {
		sb.WriteString(" WHERE ")
		sb.WriteString(rq.whereSQL)
	}
	if rq.groupBySQL != "" {
		sb.WriteString(" GROUP BY ")
		sb.WriteString(rq.groupBySQL)
	}
	sb.WriteString(" ORDER BY ")
	sb.WriteString(orderSQL)
	sb.WriteByte(' ')
	sb.WriteString(limitOffsetSQL)

	return sb.String(), args, nil
}

// BuildCount generates a SELECT COUNT(*) query with the same WHERE and
// GROUP BY clauses as Build(), but without ORDER BY, LIMIT, or OFFSET.
func (qb *QueryBuilder) BuildCount(ctx context.Context) (string, []any, error) {
	rq, err := qb.resolve(ctx)
	if err != nil {
		return "", nil, err
	}

	var sb strings.Builder
	sb.WriteString("SELECT COUNT(*) FROM ")
	sb.WriteString(rq.quotedTable)
	if rq.whereSQL != "" {
		sb.WriteString(" WHERE ")
		sb.WriteString(rq.whereSQL)
	}
	if rq.groupBySQL != "" {
		sb.WriteString(" GROUP BY ")
		sb.WriteString(rq.groupBySQL)
	}

	return sb.String(), rq.whereArgs, nil
}

// ── internal helpers ─────────────────────────────────────────────────────────

// resolvedQuery holds the validated, pre-built query components shared by
// Build() and BuildCount().
type resolvedQuery struct {
	validCols   map[string]struct{}
	quotedTable string
	groupBySQL  string
	whereSQL    string
	whereArgs   []any
}

// resolve performs MetaType lookup, field validation, and WHERE/GROUP BY
// generation. It is called by both Build() and BuildCount().
func (qb *QueryBuilder) resolve(ctx context.Context) (*resolvedQuery, error) {
	if qb.err != nil {
		return nil, qb.err
	}
	if qb.doctype == "" {
		return nil, errors.New("querybuilder: doctype not set (call For())")
	}

	qm, err := qb.provider.QueryMeta(ctx, qb.site, qb.doctype)
	if err != nil {
		return nil, fmt.Errorf("querybuilder: load MetaType %q: %w", qb.doctype, err)
	}

	quotedTable := pgx.Identifier{qm.TableName}.Sanitize()

	// WHERE clause.
	whereSQL, whereArgs, err := qb.buildWhereClause(qm.ValidColumns)
	if err != nil {
		return nil, err
	}

	// GROUP BY clause.
	groupBySQL, err := qb.buildGroupByClause(qm.ValidColumns)
	if err != nil {
		return nil, err
	}

	return &resolvedQuery{
		quotedTable: quotedTable,
		validCols:   qm.ValidColumns,
		whereSQL:    whereSQL,
		whereArgs:   whereArgs,
		groupBySQL:  groupBySQL,
	}, nil
}

// buildSelectClause returns the SELECT column list or "*".
// Field names are validated against the MetaType's valid columns.
func (qb *QueryBuilder) buildSelectClause(rq *resolvedQuery) (string, error) {
	if len(qb.fields) == 0 {
		return "*", nil
	}
	// Deduplicate while preserving order.
	seen := make(map[string]struct{}, len(qb.fields))
	parts := make([]string, 0, len(qb.fields))
	for _, f := range qb.fields {
		if _, dup := seen[f]; dup {
			continue
		}
		if _, ok := rq.validCols[f]; !ok {
			return "", fmt.Errorf("querybuilder: unknown select field %q for doctype %q", f, qb.doctype)
		}
		seen[f] = struct{}{}
		parts = append(parts, pgx.Identifier{f}.Sanitize())
	}
	return strings.Join(parts, ", "), nil
}

// buildWhereClause generates the WHERE SQL and args from the builder's filters.
func (qb *QueryBuilder) buildWhereClause(validCols map[string]struct{}) (string, []any, error) {
	if len(qb.filters) == 0 {
		return "", nil, nil
	}

	argIdx := 1
	var parts []string
	var args []any

	for _, f := range qb.filters {
		if _, ok := validCols[f.Field]; !ok {
			return "", nil, fmt.Errorf("querybuilder: unknown filter field %q for doctype %q", f.Field, qb.doctype)
		}
		sql, newArgs, err := buildFilterSQL(f, &argIdx)
		if err != nil {
			return "", nil, err
		}
		parts = append(parts, sql)
		args = append(args, newArgs...)
	}

	return strings.Join(parts, " AND "), args, nil
}

// buildGroupByClause validates and quotes GROUP BY fields.
func (qb *QueryBuilder) buildGroupByClause(validCols map[string]struct{}) (string, error) {
	if len(qb.groupBy) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(qb.groupBy))
	for _, f := range qb.groupBy {
		if _, ok := validCols[f]; !ok {
			return "", fmt.Errorf("querybuilder: unknown group_by field %q for doctype %q", f, qb.doctype)
		}
		parts = append(parts, pgx.Identifier{f}.Sanitize())
	}
	return strings.Join(parts, ", "), nil
}

// buildOrderByClause validates and quotes ORDER BY fields. Defaults to
// "modified" DESC when no OrderBy() calls have been made.
func (qb *QueryBuilder) buildOrderByClause(rq *resolvedQuery) (string, error) {
	if len(qb.orderBy) == 0 {
		return pgx.Identifier{"modified"}.Sanitize() + " DESC", nil
	}
	parts := make([]string, 0, len(qb.orderBy))
	for _, o := range qb.orderBy {
		if _, ok := rq.validCols[o.Field]; !ok {
			return "", fmt.Errorf("querybuilder: unknown order_by field %q for doctype %q", o.Field, qb.doctype)
		}
		parts = append(parts, pgx.Identifier{o.Field}.Sanitize()+" "+o.Direction)
	}
	return strings.Join(parts, ", "), nil
}

// buildFilterSQL generates the SQL fragment and args for a single Filter.
// argIdx is advanced past any consumed placeholder positions.
func buildFilterSQL(f Filter, argIdx *int) (string, []any, error) {
	quoted := pgx.Identifier{f.Field}.Sanitize()

	switch f.Operator {
	// Simple comparison operators.
	case OpEqual, OpNotEqual, OpGreater, OpLess, OpGreaterOrEq, OpLessOrEq:
		sql := fmt.Sprintf("%s %s $%d", quoted, string(f.Operator), *argIdx)
		*argIdx++
		return sql, []any{f.Value}, nil

	case OpLike:
		sql := fmt.Sprintf("%s LIKE $%d", quoted, *argIdx)
		*argIdx++
		return sql, []any{f.Value}, nil

	case OpNotLike:
		sql := fmt.Sprintf("%s NOT LIKE $%d", quoted, *argIdx)
		*argIdx++
		return sql, []any{f.Value}, nil

	case OpIn, OpNotIn:
		elems, err := toAnySlice(f.Value)
		if err != nil {
			return "", nil, fmt.Errorf("querybuilder: %s operator on field %q: %w", f.Operator, f.Field, err)
		}
		if len(elems) == 0 {
			return "", nil, fmt.Errorf("querybuilder: %s operator on field %q: empty slice", f.Operator, f.Field)
		}
		placeholders := make([]string, len(elems))
		for i := range elems {
			placeholders[i] = fmt.Sprintf("$%d", *argIdx)
			*argIdx++
		}
		keyword := "IN"
		if f.Operator == OpNotIn {
			keyword = "NOT IN"
		}
		sql := fmt.Sprintf("%s %s (%s)", quoted, keyword, strings.Join(placeholders, ", "))
		return sql, elems, nil

	case OpBetween:
		elems, err := toAnySlice(f.Value)
		if err != nil {
			return "", nil, fmt.Errorf("querybuilder: between operator on field %q: %w", f.Field, err)
		}
		if len(elems) != 2 {
			return "", nil, fmt.Errorf("querybuilder: between operator on field %q: requires exactly 2 values, got %d", f.Field, len(elems))
		}
		sql := fmt.Sprintf("%s BETWEEN $%d AND $%d", quoted, *argIdx, *argIdx+1)
		*argIdx += 2
		return sql, elems, nil

	case OpIsNull:
		return fmt.Sprintf("%s IS NULL", quoted), nil, nil

	case OpIsNotNull:
		return fmt.Sprintf("%s IS NOT NULL", quoted), nil, nil

	case OpJSONContains:
		data, err := json.Marshal(f.Value)
		if err != nil {
			return "", nil, fmt.Errorf("querybuilder: @> operator on field %q: json marshal: %w", f.Field, err)
		}
		sql := fmt.Sprintf("%s @> $%d::jsonb", quoted, *argIdx)
		*argIdx++
		return sql, []any{string(data)}, nil

	case OpFullText:
		return "", nil, errors.New("querybuilder: full-text search (@@) not available until MS-15")

	default:
		return "", nil, fmt.Errorf("querybuilder: unsupported operator %q", f.Operator)
	}
}

// toAnySlice converts a typed slice to []any. Supports common Go slice types
// directly and falls back to reflect for others.
func toAnySlice(v any) ([]any, error) {
	switch s := v.(type) {
	case []any:
		return s, nil
	case []string:
		out := make([]any, len(s))
		for i, e := range s {
			out[i] = e
		}
		return out, nil
	case []int:
		out := make([]any, len(s))
		for i, e := range s {
			out[i] = e
		}
		return out, nil
	case []int64:
		out := make([]any, len(s))
		for i, e := range s {
			out[i] = e
		}
		return out, nil
	case []float64:
		out := make([]any, len(s))
		for i, e := range s {
			out[i] = e
		}
		return out, nil
	default:
		rv := reflect.ValueOf(v)
		if rv.Kind() != reflect.Slice {
			return nil, fmt.Errorf("value must be a slice, got %T", v)
		}
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = rv.Index(i).Interface()
		}
		return out, nil
	}
}

package orm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// extraFieldNameRe validates _extra field names: lowercase alphanumeric + underscore,
// must start with a letter or underscore. This regex ensures SQL injection safety
// when embedding field names in _extra->>'...' expressions.
var extraFieldNameRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// maxJoinDepth is the maximum number of Link hops allowed in dot-notation.
// e.g., "customer.territory" = depth 1, "customer.territory.region" = depth 2.
const maxJoinDepth = 2

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
	// FieldTypes maps field names to their PostgreSQL column type (e.g. "TEXT",
	// "INTEGER"). When non-nil, enables _extra JSONB transparency: fields not
	// found in ValidColumns or NonQueryableFields are treated as _extra fields.
	// When nil, unknown fields produce errors (backward compat with T1).
	FieldTypes map[string]string
	// NonQueryableFields is the set of field names that exist in the MetaType
	// but cannot be queried (Table, TableMultiSelect, layout-only types).
	NonQueryableFields map[string]struct{}
	// LinkFields maps Link field names to their target DocType name.
	// e.g., "customer" → "Customer". Only populated for FieldTypeLink fields.
	LinkFields map[string]string
	// DynamicLinkFields is the set of field names that are FieldTypeDynamicLink.
	// Used to produce clear errors when dot-notation is attempted on them.
	DynamicLinkFields map[string]struct{}
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
	rq.writeJoinSQL(&sb)
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
	rq.writeJoinSQL(&sb)
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

// ── field access resolution ──────────────────────────────────────────────────

// fieldAccess describes how a field maps to SQL — either as a direct column
// reference or as a _extra JSONB extraction expression.
type fieldAccess struct {
	// expr is the SQL expression for the field:
	//   real column: `"status"`
	//   _extra field: `_extra->>'custom_color'`
	//   aliased real: `"t0"."status"`
	//   aliased _extra: `"t1"._extra->>'custom_color'`
	expr string
	// name is the original field name (without dot-notation prefix).
	name string
	// tableAlias is the table alias ("t0", "t1", etc.) or empty for unaliased.
	tableAlias string
	// isExtra is true when the field lives in the _extra JSONB column.
	isExtra bool
}

// resolveFieldAccess classifies a field name and returns the appropriate SQL
// expression. The classification order is:
//  1. Known real column (in ValidColumns) → quoted identifier
//  2. Known non-queryable field (Table, layout) → error
//  3. If FieldTypes is nil (no _extra support) → error (backward compat)
//  4. Valid _extra field name (regex check) → _extra->>'field'
//  5. Invalid name → error
func resolveFieldAccess(fieldName string, qm *QueryMeta, doctype string) (*fieldAccess, error) {
	// 1. Real column.
	if _, ok := qm.ValidColumns[fieldName]; ok {
		return &fieldAccess{
			expr: pgx.Identifier{fieldName}.Sanitize(),
			name: fieldName,
		}, nil
	}

	// 2. Non-queryable field (Table, layout types).
	if qm.NonQueryableFields != nil {
		if _, ok := qm.NonQueryableFields[fieldName]; ok {
			return nil, fmt.Errorf("querybuilder: field %q is not queryable for doctype %q", fieldName, doctype)
		}
	}

	// 3. No _extra support (backward compat).
	if qm.FieldTypes == nil {
		return nil, fmt.Errorf("querybuilder: unknown field %q for doctype %q", fieldName, doctype)
	}

	// 4. Validate as _extra field name.
	if !extraFieldNameRe.MatchString(fieldName) {
		return nil, fmt.Errorf("querybuilder: invalid _extra field name %q for doctype %q", fieldName, doctype)
	}

	return &fieldAccess{
		expr:    fmt.Sprintf("_extra->>'%s'", fieldName),
		name:    fieldName,
		isExtra: true,
	}, nil
}

// extraFilterExpr returns the SQL expression for an _extra field in a WHERE
// clause, applying type casts as needed for the given operator and value type.
// tableAlias is the table alias (e.g. "t0", "t1") or empty for unaliased queries.
//
// Rules:
//   - Equality/LIKE/IN: text comparison (no cast)
//   - Numeric comparisons (>, <, >=, <=, BETWEEN): cast based on Go value type
//   - IS NULL / IS NOT NULL: no cast
//   - @> JSONB contains: operates on the _extra column itself
func extraFilterExpr(fieldName string, op Operator, value any, tableAlias string) string {
	prefix := ""
	if tableAlias != "" {
		prefix = fmt.Sprintf("%q.", tableAlias)
	}
	base := fmt.Sprintf("%s_extra->>'%s'", prefix, fieldName)

	switch op {
	case OpJSONContains:
		// @> operates on the _extra column itself, not the extracted text.
		return prefix + pgx.Identifier{"_extra"}.Sanitize()

	case OpIsNull, OpIsNotNull:
		return base

	case OpEqual, OpNotEqual, OpLike, OpNotLike, OpIn, OpNotIn:
		return base

	case OpGreater, OpLess, OpGreaterOrEq, OpLessOrEq, OpBetween:
		cast := inferCast(value)
		if cast == "" {
			return base
		}
		return fmt.Sprintf("(%s)::%s", base, cast)

	default:
		return base
	}
}

// inferCast returns the PostgreSQL cast suffix for a Go value used in numeric
// comparisons on _extra JSONB fields. For BETWEEN, the value is a slice — we
// inspect the first element.
func inferCast(value any) string {
	v := value
	// For BETWEEN, inspect the first element of the slice.
	if s, ok := value.([]any); ok && len(s) > 0 {
		v = s[0]
	}

	switch v.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return "NUMERIC"
	case bool:
		return "BOOLEAN"
	case time.Time:
		return "TIMESTAMPTZ"
	default:
		return ""
	}
}

// ── Link field auto-join resolution ─────────────────────────────────────────

// joinEntry represents a single LEFT JOIN in the query, generated from a
// Link field dot-notation path.
type joinEntry struct {
	alias       string // "t1", "t2", etc.
	targetTable string // unquoted table name, e.g. "tab_customer"
	targetQM    *QueryMeta
	joinOnCol   string // source column that holds the link value, e.g. "customer"
	sourceAlias string // "t0" or "t1" — the alias of the table being joined FROM
}

// resolvedJoinField stores the pre-resolved field access for a dotted field path.
type resolvedJoinField struct {
	fa    *fieldAccess // resolved on the target QueryMeta
	alias string       // which join alias owns the final field
}

// joinState accumulates joins discovered during field resolution. It is shared
// across SELECT, WHERE, ORDER BY, and GROUP BY clause building.
type joinState struct {
	entries  map[string]*joinEntry         // key = dot-prefix, e.g. "customer"
	resolved map[string]*resolvedJoinField // key = full dotted path, e.g. "customer.territory"
	ordered  []*joinEntry                  // insertion order for deterministic SQL
	nextIdx  int                           // next alias index (starts at 1)
}

func newJoinState() *joinState {
	return &joinState{
		entries:  make(map[string]*joinEntry),
		resolved: make(map[string]*resolvedJoinField),
		nextIdx:  1,
	}
}

// hasJoins reports whether any joins have been registered.
func (js *joinState) hasJoins() bool {
	return js != nil && len(js.ordered) > 0
}

// resolveJoinField resolves a dot-notated field name (e.g. "customer.territory")
// by walking the Link chain, registering joins, and returning the aliased
// fieldAccess for the final (leaf) field.
func (qb *QueryBuilder) resolveJoinField(
	ctx context.Context,
	dottedField string,
	sourceQM *QueryMeta,
	js *joinState,
) (*fieldAccess, error) {
	parts := strings.Split(dottedField, ".")

	// Validate depth: parts includes the final field, so len(parts)-1 is the
	// number of Link hops. Max depth is 2 (e.g. a.b.c = 2 hops + leaf).
	if len(parts) < 2 {
		return nil, fmt.Errorf("querybuilder: invalid dot-notation %q: at least two segments required", dottedField)
	}
	hops := len(parts) - 1
	if hops > maxJoinDepth {
		return nil, fmt.Errorf("querybuilder: dot-notation %q exceeds maximum join depth of %d", dottedField, maxJoinDepth)
	}

	// Validate no empty segments.
	for _, p := range parts {
		if p == "" {
			return nil, fmt.Errorf("querybuilder: invalid dot-notation %q: empty segment", dottedField)
		}
	}

	currentQM := sourceQM
	currentAlias := "t0"

	// Walk intermediate segments (all except the last).
	for i := 0; i < hops; i++ {
		segment := parts[i]

		// Build dedup key: join of all segments up to and including this one.
		dedupKey := strings.Join(parts[:i+1], ".")

		if existing, ok := js.entries[dedupKey]; ok {
			// Reuse existing join.
			currentQM = existing.targetQM
			currentAlias = existing.alias
			continue
		}

		// Check for DynamicLink — cannot auto-join.
		if currentQM.DynamicLinkFields != nil {
			if _, ok := currentQM.DynamicLinkFields[segment]; ok {
				return nil, fmt.Errorf("querybuilder: DynamicLink field %q does not support auto-joins", segment)
			}
		}

		// Must be a Link field.
		targetDocType, ok := currentQM.LinkFields[segment]
		if !ok {
			return nil, fmt.Errorf("querybuilder: field %q is not a Link field in doctype %q", segment, currentQM.Name)
		}

		// Load target DocType metadata.
		targetQM, err := qb.provider.QueryMeta(ctx, qb.site, targetDocType)
		if err != nil {
			return nil, fmt.Errorf("querybuilder: load linked MetaType %q: %w", targetDocType, err)
		}

		alias := fmt.Sprintf("t%d", js.nextIdx)
		js.nextIdx++

		je := &joinEntry{
			alias:       alias,
			targetTable: targetQM.TableName,
			targetQM:    targetQM,
			joinOnCol:   segment,
			sourceAlias: currentAlias,
		}
		js.entries[dedupKey] = je
		js.ordered = append(js.ordered, je)

		currentQM = targetQM
		currentAlias = alias
	}

	// Resolve the final (leaf) field on the target MetaType.
	leafField := parts[len(parts)-1]
	fa, err := resolveFieldAccess(leafField, currentQM, currentQM.Name)
	if err != nil {
		return nil, err
	}

	// Build aliased expression.
	fa.tableAlias = currentAlias
	if fa.isExtra {
		fa.expr = fmt.Sprintf("%q._extra->>'%s'", currentAlias, fa.name)
	} else {
		fa.expr = fmt.Sprintf("%q.%s", currentAlias, pgx.Identifier{leafField}.Sanitize())
	}

	// Store the resolved join field for later lookup.
	js.resolved[dottedField] = &resolvedJoinField{
		alias: currentAlias,
		fa:    fa,
	}

	return fa, nil
}

// resolveFieldForClause resolves a field name in the context of a query that
// may or may not have joins. For dotted fields, it returns the pre-resolved
// aliased expression. For non-dotted fields, it resolves via resolveFieldAccess
// and prefixes with "t0". when joins are present.
func (qb *QueryBuilder) resolveFieldForClause(fieldName string, sourceQM *QueryMeta, js *joinState) (*fieldAccess, error) {
	// Dotted field: look up from pre-resolved join state.
	if strings.Contains(fieldName, ".") {
		rjf, ok := js.resolved[fieldName]
		if !ok {
			return nil, fmt.Errorf("querybuilder: internal error: join field %q not pre-resolved", fieldName)
		}
		return rjf.fa, nil
	}

	// Non-dotted field: resolve normally.
	fa, err := resolveFieldAccess(fieldName, sourceQM, qb.doctype)
	if err != nil {
		return nil, err
	}

	// If joins are present, qualify with source table alias "t0".
	if js.hasJoins() {
		fa.tableAlias = "t0"
		if fa.isExtra {
			fa.expr = fmt.Sprintf("\"t0\"._extra->>'%s'", fa.name)
		} else {
			fa.expr = "\"t0\"." + pgx.Identifier{fa.name}.Sanitize()
		}
	}

	return fa, nil
}

// ── internal helpers ─────────────────────────────────────────────────────────

// resolvedQuery holds the validated, pre-built query components shared by
// Build() and BuildCount().
type resolvedQuery struct {
	validCols   map[string]struct{}
	qm          *QueryMeta
	joins       *joinState
	quotedTable string
	groupBySQL  string
	whereSQL    string
	whereArgs   []any
}

// writeJoinSQL appends the table alias and LEFT JOIN clauses to sb when joins
// are present. When no joins exist, it is a no-op (preserving existing behavior).
func (rq *resolvedQuery) writeJoinSQL(sb *strings.Builder) {
	if !rq.joins.hasJoins() {
		return
	}
	sb.WriteString(" AS \"t0\"")
	for _, je := range rq.joins.ordered {
		fmt.Fprintf(sb, " LEFT JOIN %s AS %q ON %q.%s = %q.%s",
			pgx.Identifier{je.targetTable}.Sanitize(),
			je.alias,
			je.alias,
			pgx.Identifier{"name"}.Sanitize(),
			je.sourceAlias,
			pgx.Identifier{je.joinOnCol}.Sanitize(),
		)
	}
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

	// Pre-scan: resolve all dotted fields to populate the join state.
	js := newJoinState()
	if joinErr := qb.resolveAllJoins(ctx, qm, js); joinErr != nil {
		return nil, joinErr
	}

	// WHERE clause.
	whereSQL, whereArgs, err := qb.buildWhereClause(qm, js)
	if err != nil {
		return nil, err
	}

	// GROUP BY clause.
	groupBySQL, err := qb.buildGroupByClause(qm, js)
	if err != nil {
		return nil, err
	}

	return &resolvedQuery{
		qm:          qm,
		quotedTable: quotedTable,
		validCols:   qm.ValidColumns,
		whereSQL:    whereSQL,
		whereArgs:   whereArgs,
		groupBySQL:  groupBySQL,
		joins:       js,
	}, nil
}

// resolveAllJoins scans all field references (fields, filters, orderBy, groupBy)
// for dot-notation and pre-resolves their join chains.
func (qb *QueryBuilder) resolveAllJoins(ctx context.Context, sourceQM *QueryMeta, js *joinState) error {
	// Collect all unique dotted field names.
	seen := make(map[string]struct{})
	var dotted []string

	collect := func(name string) {
		if strings.Contains(name, ".") {
			if _, ok := seen[name]; !ok {
				seen[name] = struct{}{}
				dotted = append(dotted, name)
			}
		}
	}

	for _, f := range qb.fields {
		collect(f)
	}
	for _, f := range qb.filters {
		collect(f.Field)
	}
	for _, o := range qb.orderBy {
		collect(o.Field)
	}
	for _, g := range qb.groupBy {
		collect(g)
	}

	// Resolve each dotted field.
	for _, df := range dotted {
		if _, err := qb.resolveJoinField(ctx, df, sourceQM, js); err != nil {
			return err
		}
	}

	return nil
}

// buildSelectClause returns the SELECT column list or "*".
// Field names are resolved via resolveFieldForClause; _extra and joined fields
// are aliased appropriately.
func (qb *QueryBuilder) buildSelectClause(rq *resolvedQuery) (string, error) {
	if len(qb.fields) == 0 {
		if rq.joins.hasJoins() {
			return "\"t0\".*", nil
		}
		return "*", nil
	}
	// Deduplicate while preserving order.
	seen := make(map[string]struct{}, len(qb.fields))
	parts := make([]string, 0, len(qb.fields))
	for _, f := range qb.fields {
		if _, dup := seen[f]; dup {
			continue
		}
		fa, err := qb.resolveFieldForClause(f, rq.qm, rq.joins)
		if err != nil {
			return "", err
		}
		seen[f] = struct{}{}

		// Dotted fields and _extra fields need an alias.
		if strings.Contains(f, ".") {
			parts = append(parts, fmt.Sprintf("%s AS %s", fa.expr, pgx.Identifier{f}.Sanitize()))
		} else if fa.isExtra {
			parts = append(parts, fmt.Sprintf("%s AS %s", fa.expr, pgx.Identifier{f}.Sanitize()))
		} else {
			parts = append(parts, fa.expr)
		}
	}
	return strings.Join(parts, ", "), nil
}

// buildWhereClause generates the WHERE SQL and args from the builder's filters.
func (qb *QueryBuilder) buildWhereClause(qm *QueryMeta, js *joinState) (string, []any, error) {
	if len(qb.filters) == 0 {
		return "", nil, nil
	}

	argIdx := 1
	var parts []string
	var args []any

	for _, f := range qb.filters {
		fa, err := qb.resolveFieldForClause(f.Field, qm, js)
		if err != nil {
			return "", nil, err
		}
		sql, newArgs, err := buildFilterSQL(f, fa, &argIdx)
		if err != nil {
			return "", nil, err
		}
		parts = append(parts, sql)
		args = append(args, newArgs...)
	}

	return strings.Join(parts, " AND "), args, nil
}

// buildGroupByClause validates and quotes GROUP BY fields.
func (qb *QueryBuilder) buildGroupByClause(qm *QueryMeta, js *joinState) (string, error) {
	if len(qb.groupBy) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(qb.groupBy))
	for _, f := range qb.groupBy {
		fa, err := qb.resolveFieldForClause(f, qm, js)
		if err != nil {
			return "", err
		}
		parts = append(parts, fa.expr)
	}
	return strings.Join(parts, ", "), nil
}

// buildOrderByClause validates and quotes ORDER BY fields. Defaults to
// "modified" DESC when no OrderBy() calls have been made.
func (qb *QueryBuilder) buildOrderByClause(rq *resolvedQuery) (string, error) {
	if len(qb.orderBy) == 0 {
		if rq.joins.hasJoins() {
			return "\"t0\"." + pgx.Identifier{"modified"}.Sanitize() + " DESC", nil
		}
		return pgx.Identifier{"modified"}.Sanitize() + " DESC", nil
	}
	parts := make([]string, 0, len(qb.orderBy))
	for _, o := range qb.orderBy {
		fa, err := qb.resolveFieldForClause(o.Field, rq.qm, rq.joins)
		if err != nil {
			return "", err
		}
		parts = append(parts, fa.expr+" "+o.Direction)
	}
	return strings.Join(parts, ", "), nil
}

// buildFilterSQL generates the SQL fragment and args for a single Filter.
// fa provides the resolved field expression. argIdx is advanced past any
// consumed placeholder positions.
func buildFilterSQL(f Filter, fa *fieldAccess, argIdx *int) (string, []any, error) {
	// Determine the field expression for this filter.
	// For _extra fields, the expression may include type casts.
	fieldExpr := fa.expr
	if fa.isExtra {
		fieldExpr = extraFilterExpr(fa.name, f.Operator, f.Value, fa.tableAlias)
	}

	switch f.Operator {
	// Simple comparison operators.
	case OpEqual, OpNotEqual, OpGreater, OpLess, OpGreaterOrEq, OpLessOrEq:
		sql := fmt.Sprintf("%s %s $%d", fieldExpr, string(f.Operator), *argIdx)
		*argIdx++
		return sql, []any{f.Value}, nil

	case OpLike:
		sql := fmt.Sprintf("%s LIKE $%d", fieldExpr, *argIdx)
		*argIdx++
		return sql, []any{f.Value}, nil

	case OpNotLike:
		sql := fmt.Sprintf("%s NOT LIKE $%d", fieldExpr, *argIdx)
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
		sql := fmt.Sprintf("%s %s (%s)", fieldExpr, keyword, strings.Join(placeholders, ", "))
		return sql, elems, nil

	case OpBetween:
		elems, err := toAnySlice(f.Value)
		if err != nil {
			return "", nil, fmt.Errorf("querybuilder: between operator on field %q: %w", f.Field, err)
		}
		if len(elems) != 2 {
			return "", nil, fmt.Errorf("querybuilder: between operator on field %q: requires exactly 2 values, got %d", f.Field, len(elems))
		}
		// For _extra fields with BETWEEN, re-resolve with the actual slice
		// so inferCast can inspect the element types.
		if fa.isExtra {
			fieldExpr = extraFilterExpr(fa.name, f.Operator, elems, fa.tableAlias)
		}
		sql := fmt.Sprintf("%s BETWEEN $%d AND $%d", fieldExpr, *argIdx, *argIdx+1)
		*argIdx += 2
		return sql, elems, nil

	case OpIsNull:
		return fmt.Sprintf("%s IS NULL", fieldExpr), nil, nil

	case OpIsNotNull:
		return fmt.Sprintf("%s IS NOT NULL", fieldExpr), nil, nil

	case OpJSONContains:
		data, err := json.Marshal(f.Value)
		if err != nil {
			return "", nil, fmt.Errorf("querybuilder: @> operator on field %q: json marshal: %w", f.Field, err)
		}
		sql := fmt.Sprintf("%s @> $%d::jsonb", fieldExpr, *argIdx)
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

package orm

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ── test MetaProvider ────────────────────────────────────────────────────────

// stubMetaProvider implements MetaProvider for unit tests.
type stubMetaProvider struct {
	metas map[string]*QueryMeta // keyed by "site:doctype"
}

func (s *stubMetaProvider) QueryMeta(_ context.Context, site, doctype string) (*QueryMeta, error) {
	key := site + ":" + doctype
	qm, ok := s.metas[key]
	if !ok {
		return nil, fmt.Errorf("metatype %q not found for site %q", doctype, site)
	}
	return qm, nil
}

// salesOrderMeta returns a QueryMeta fixture for "SalesOrder" on "site1".
func salesOrderMeta() *QueryMeta {
	cols := map[string]struct{}{
		// Standard columns (subset matching StandardColumns).
		"name": {}, "owner": {}, "creation": {}, "modified": {},
		"modified_by": {}, "docstatus": {}, "idx": {}, "workflow_state": {},
		"_extra": {}, "_user_tags": {}, "_comments": {}, "_assign": {}, "_liked_by": {},
		// User-defined fields.
		"customer": {}, "total": {}, "status": {}, "tags": {}, "order_date": {},
	}
	return &QueryMeta{
		Name:         "SalesOrder",
		IsChildTable: false,
		TableName:    "tab_sales_order",
		ValidColumns: cols,
	}
}

func childItemMeta() *QueryMeta {
	cols := map[string]struct{}{
		"name": {}, "parent": {}, "parenttype": {}, "parentfield": {},
		"idx": {}, "owner": {}, "creation": {}, "modified": {}, "modified_by": {},
		"_extra": {},
		"item_code": {}, "qty": {}, "rate": {},
	}
	return &QueryMeta{
		Name:         "SalesOrderItem",
		IsChildTable: true,
		TableName:    "tab_sales_order_item",
		ValidColumns: cols,
	}
}

func testProvider() *stubMetaProvider {
	return &stubMetaProvider{
		metas: map[string]*QueryMeta{
			"site1:SalesOrder":     salesOrderMeta(),
			"site1:SalesOrderItem": childItemMeta(),
		},
	}
}

// ── Constructor tests ────────────────────────────────────────────────────────

func TestNewQueryBuilder_Defaults(t *testing.T) {
	qb := NewQueryBuilder(testProvider(), "site1")
	if qb.limit != 20 {
		t.Errorf("default limit = %d, want 20", qb.limit)
	}
	if qb.offset != 0 {
		t.Errorf("default offset = %d, want 0", qb.offset)
	}
	if qb.err != nil {
		t.Errorf("unexpected initial error: %v", qb.err)
	}
}

func TestFor_EmptyDoctype(t *testing.T) {
	qb := NewQueryBuilder(testProvider(), "site1").For("")
	_, _, err := qb.Build(context.Background())
	if err == nil {
		t.Fatal("expected error for empty doctype")
	}
	if !strings.Contains(err.Error(), "doctype must not be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuild_DoctypeNotSet(t *testing.T) {
	qb := NewQueryBuilder(testProvider(), "site1")
	_, _, err := qb.Build(context.Background())
	if err == nil {
		t.Fatal("expected error when doctype not set")
	}
	if !strings.Contains(err.Error(), "doctype not set") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuild_UnknownDoctype(t *testing.T) {
	qb := NewQueryBuilder(testProvider(), "site1").For("Unknown")
	_, _, err := qb.Build(context.Background())
	if err == nil {
		t.Fatal("expected error for unknown doctype")
	}
	if !strings.Contains(err.Error(), "load MetaType") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── SELECT clause tests ─────────────────────────────────────────────────────

func TestBuild_DefaultSelectStar(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(sql, "SELECT * FROM") {
		t.Errorf("expected SELECT * FROM, got: %s", sql)
	}
	// Default: 2 args (limit, offset).
	if len(args) != 2 {
		t.Errorf("args count = %d, want 2", len(args))
	}
}

func TestBuild_ExplicitFields(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Fields("name", "customer").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"name", "customer"`) {
		t.Errorf("expected quoted fields in SELECT, got: %s", sql)
	}
}

func TestBuild_DuplicateFields(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Fields("name", "name", "customer").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "name" should appear only once.
	selectPart := sql[:strings.Index(sql, " FROM")]
	if strings.Count(selectPart, `"name"`) != 1 {
		t.Errorf("expected deduplicated fields, got: %s", selectPart)
	}
}

func TestBuild_UnknownSelectField(t *testing.T) {
	_, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Fields("name", "nonexistent").
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for unknown select field")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── Simple operator tests (table-driven) ─────────────────────────────────────

func TestBuild_SimpleOperators(t *testing.T) {
	tests := []struct {
		value any
		name  string
		op    Operator
		sqlOp string
	}{
		{"ACME", "equal", OpEqual, "="},
		{"ACME", "not_equal", OpNotEqual, "!="},
		{100, "greater", OpGreater, ">"},
		{100, "less", OpLess, "<"},
		{100, "gte", OpGreaterOrEq, ">="},
		{100, "lte", OpLessOrEq, "<="},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := NewQueryBuilder(testProvider(), "site1").
				For("SalesOrder").
				Where(Filter{Field: "customer", Operator: tt.op, Value: tt.value}).
				Build(ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			expected := fmt.Sprintf(`"customer" %s $1`, tt.sqlOp)
			if !strings.Contains(sql, expected) {
				t.Errorf("expected %q in SQL, got: %s", expected, sql)
			}
			// 3 args: filter value + limit + offset.
			if len(args) != 3 {
				t.Errorf("args count = %d, want 3", len(args))
			}
			if args[0] != tt.value {
				t.Errorf("arg[0] = %v, want %v", args[0], tt.value)
			}
		})
	}
}

// ── LIKE operator tests ─────────────────────────────────────────────────────

func TestBuild_Like(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "customer", Operator: OpLike, Value: "%ACME%"}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"customer" LIKE $1`) {
		t.Errorf("expected LIKE clause, got: %s", sql)
	}
	if args[0] != "%ACME%" {
		t.Errorf("arg[0] = %v, want %%ACME%%", args[0])
	}
}

func TestBuild_NotLike(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "customer", Operator: OpNotLike, Value: "%TEST%"}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"customer" NOT LIKE $1`) {
		t.Errorf("expected NOT LIKE clause, got: %s", sql)
	}
}

// ── IN / NOT IN tests ───────────────────────────────────────────────────────

func TestBuild_In_StringSlice(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "status", Operator: OpIn, Value: []string{"Draft", "Open", "Closed"}}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"status" IN ($1, $2, $3)`) {
		t.Errorf("expected IN clause, got: %s", sql)
	}
	// 5 args: 3 IN values + limit + offset.
	if len(args) != 5 {
		t.Errorf("args count = %d, want 5", len(args))
	}
}

func TestBuild_In_AnySlice(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "total", Operator: OpIn, Value: []any{100, 200}}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"total" IN ($1, $2)`) {
		t.Errorf("expected IN clause, got: %s", sql)
	}
}

func TestBuild_In_EmptySlice(t *testing.T) {
	_, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "status", Operator: OpIn, Value: []string{}}).
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for empty IN slice")
	}
	if !strings.Contains(err.Error(), "empty slice") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuild_In_NonSlice(t *testing.T) {
	_, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "status", Operator: OpIn, Value: "single_value"}).
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for non-slice IN value")
	}
	if !strings.Contains(err.Error(), "must be a slice") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuild_NotIn(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "status", Operator: OpNotIn, Value: []string{"Cancelled"}}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"status" NOT IN ($1)`) {
		t.Errorf("expected NOT IN clause, got: %s", sql)
	}
}

// ── BETWEEN tests ───────────────────────────────────────────────────────────

func TestBuild_Between(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "total", Operator: OpBetween, Value: []any{100, 500}}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"total" BETWEEN $1 AND $2`) {
		t.Errorf("expected BETWEEN clause, got: %s", sql)
	}
	if len(args) != 4 { // 2 between + limit + offset
		t.Errorf("args count = %d, want 4", len(args))
	}
}

func TestBuild_Between_WrongLength(t *testing.T) {
	_, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "total", Operator: OpBetween, Value: []any{100}}).
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for BETWEEN with 1 value")
	}
	if !strings.Contains(err.Error(), "requires exactly 2 values") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuild_Between_ThreeValues(t *testing.T) {
	_, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "total", Operator: OpBetween, Value: []any{1, 2, 3}}).
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for BETWEEN with 3 values")
	}
	if !strings.Contains(err.Error(), "requires exactly 2 values, got 3") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── IS NULL / IS NOT NULL tests ─────────────────────────────────────────────

func TestBuild_IsNull(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "workflow_state", Operator: OpIsNull}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"workflow_state" IS NULL`) {
		t.Errorf("expected IS NULL clause, got: %s", sql)
	}
	// Only 2 args: limit + offset (IS NULL consumes no args).
	if len(args) != 2 {
		t.Errorf("args count = %d, want 2", len(args))
	}
}

func TestBuild_IsNotNull(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "workflow_state", Operator: OpIsNotNull}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"workflow_state" IS NOT NULL`) {
		t.Errorf("expected IS NOT NULL clause, got: %s", sql)
	}
}

// ── JSONB contains tests ────────────────────────────────────────────────────

func TestBuild_JSONContains(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "tags", Operator: OpJSONContains, Value: map[string]any{"key": "val"}}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"tags" @> $1::jsonb`) {
		t.Errorf("expected @> clause, got: %s", sql)
	}
	// First arg should be a JSON string.
	jsonStr, ok := args[0].(string)
	if !ok {
		t.Fatalf("arg[0] type = %T, want string", args[0])
	}
	if !strings.Contains(jsonStr, `"key"`) || !strings.Contains(jsonStr, `"val"`) {
		t.Errorf("expected JSON-encoded arg, got: %s", jsonStr)
	}
}

// ── Full-text search error test ─────────────────────────────────────────────

func TestBuild_FullText_Error(t *testing.T) {
	qb := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "customer", Operator: OpFullText, Value: "search"})

	_, _, err := qb.Build(context.Background())
	if err == nil {
		t.Fatal("expected error for @@ operator")
	}
	if !strings.Contains(err.Error(), "not available until MS-15") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── Unknown operator test ───────────────────────────────────────────────────

func TestWhere_UnknownOperator(t *testing.T) {
	qb := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "customer", Operator: "INVALID", Value: "x"})

	_, _, err := qb.Build(context.Background())
	if err == nil {
		t.Fatal("expected error for unknown operator")
	}
	if !strings.Contains(err.Error(), "unknown operator") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── Multiple filters and parameter numbering ────────────────────────────────

func TestBuild_MultipleFilters(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(
			Filter{Field: "customer", Operator: OpEqual, Value: "ACME"},
			Filter{Field: "status", Operator: OpEqual, Value: "Draft"},
		).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"customer" = $1`) {
		t.Errorf("expected $1 for first filter, got: %s", sql)
	}
	if !strings.Contains(sql, `"status" = $2`) {
		t.Errorf("expected $2 for second filter, got: %s", sql)
	}
	if !strings.Contains(sql, " AND ") {
		t.Errorf("expected AND between filters, got: %s", sql)
	}
	// 4 args: 2 filter values + limit + offset.
	if len(args) != 4 {
		t.Errorf("args count = %d, want 4", len(args))
	}
}

func TestBuild_ParameterNumbering_Complex(t *testing.T) {
	// equality ($1) + IN with 3 elements ($2,$3,$4) + between ($5,$6)
	// → LIMIT $7 OFFSET $8
	sql, args, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(
			Filter{Field: "customer", Operator: OpEqual, Value: "ACME"},
			Filter{Field: "status", Operator: OpIn, Value: []string{"Draft", "Open", "Closed"}},
			Filter{Field: "total", Operator: OpBetween, Value: []any{100, 500}},
		).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"customer" = $1`) {
		t.Errorf("expected $1, got: %s", sql)
	}
	if !strings.Contains(sql, `"status" IN ($2, $3, $4)`) {
		t.Errorf("expected IN ($2, $3, $4), got: %s", sql)
	}
	if !strings.Contains(sql, `"total" BETWEEN $5 AND $6`) {
		t.Errorf("expected BETWEEN $5 AND $6, got: %s", sql)
	}
	if !strings.Contains(sql, "LIMIT $7 OFFSET $8") {
		t.Errorf("expected LIMIT $7 OFFSET $8, got: %s", sql)
	}
	// 8 args total.
	if len(args) != 8 {
		t.Errorf("args count = %d, want 8", len(args))
	}
}

func TestBuild_UnknownFilterField(t *testing.T) {
	_, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "nonexistent", Operator: OpEqual, Value: "x"}).
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for unknown filter field")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── ORDER BY tests ──────────────────────────────────────────────────────────

func TestBuild_OrderBy_Default(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `ORDER BY "modified" DESC`) {
		t.Errorf("expected default ORDER BY, got: %s", sql)
	}
}

func TestBuild_OrderBy_Custom(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		OrderBy("creation", "ASC").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `ORDER BY "creation" ASC`) {
		t.Errorf("expected custom ORDER BY, got: %s", sql)
	}
}

func TestBuild_OrderBy_MultiColumn(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		OrderBy("status", "ASC").
		OrderBy("creation", "DESC").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `ORDER BY "status" ASC, "creation" DESC`) {
		t.Errorf("expected multi-column ORDER BY, got: %s", sql)
	}
}

func TestBuild_OrderBy_InvalidField(t *testing.T) {
	_, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		OrderBy("nonexistent", "ASC").
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for unknown order_by field")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOrderBy_InvalidDirection(t *testing.T) {
	qb := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		OrderBy("creation", "SIDEWAYS")

	_, _, err := qb.Build(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid direction")
	}
	if !strings.Contains(err.Error(), "invalid order direction") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOrderBy_EmptyDirectionDefaultsASC(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		OrderBy("creation", "").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `ORDER BY "creation" ASC`) {
		t.Errorf("expected ASC default, got: %s", sql)
	}
}

// ── GROUP BY tests ──────────────────────────────────────────────────────────

func TestBuild_GroupBy(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Fields("status").
		GroupBy("status").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `GROUP BY "status"`) {
		t.Errorf("expected GROUP BY clause, got: %s", sql)
	}
}

func TestBuild_GroupBy_UnknownField(t *testing.T) {
	_, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		GroupBy("nonexistent").
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for unknown group_by field")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── LIMIT / OFFSET tests ───────────────────────────────────────────────────

func TestBuild_Limit_Zero(t *testing.T) {
	qb := NewQueryBuilder(testProvider(), "site1").For("SalesOrder").Limit(0)
	sql, args, err := qb.Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// limit arg is second-to-last.
	limitArg := args[len(args)-2]
	if limitArg != 20 {
		t.Errorf("limit = %v, want 20 (clamped from 0)", limitArg)
	}
	_ = sql
}

func TestBuild_Limit_Over100(t *testing.T) {
	qb := NewQueryBuilder(testProvider(), "site1").For("SalesOrder").Limit(200)
	_, args, err := qb.Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	limitArg := args[len(args)-2]
	if limitArg != 100 {
		t.Errorf("limit = %v, want 100 (clamped from 200)", limitArg)
	}
}

func TestBuild_Limit_Valid(t *testing.T) {
	qb := NewQueryBuilder(testProvider(), "site1").For("SalesOrder").Limit(50)
	_, args, err := qb.Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	limitArg := args[len(args)-2]
	if limitArg != 50 {
		t.Errorf("limit = %v, want 50", limitArg)
	}
}

func TestBuild_Offset_Negative(t *testing.T) {
	qb := NewQueryBuilder(testProvider(), "site1").For("SalesOrder").Offset(-5)
	_, args, err := qb.Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	offsetArg := args[len(args)-1]
	if offsetArg != 0 {
		t.Errorf("offset = %v, want 0 (clamped from -5)", offsetArg)
	}
}

func TestBuild_Offset_Valid(t *testing.T) {
	qb := NewQueryBuilder(testProvider(), "site1").For("SalesOrder").Offset(40)
	_, args, err := qb.Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	offsetArg := args[len(args)-1]
	if offsetArg != 40 {
		t.Errorf("offset = %v, want 40", offsetArg)
	}
}

// ── BuildCount tests ────────────────────────────────────────────────────────

func TestBuildCount_Basic(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		BuildCount(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(sql, "SELECT COUNT(*) FROM") {
		t.Errorf("expected SELECT COUNT(*), got: %s", sql)
	}
	// No ORDER BY, LIMIT, or OFFSET.
	if strings.Contains(sql, "ORDER BY") {
		t.Errorf("BuildCount should not have ORDER BY, got: %s", sql)
	}
	if strings.Contains(sql, "LIMIT") {
		t.Errorf("BuildCount should not have LIMIT, got: %s", sql)
	}
	// No args for an unfiltered count.
	if len(args) != 0 {
		t.Errorf("args count = %d, want 0", len(args))
	}
}

func TestBuildCount_WithFilters(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "status", Operator: OpEqual, Value: "Draft"}).
		BuildCount(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "WHERE") {
		t.Errorf("expected WHERE in count query, got: %s", sql)
	}
	if !strings.Contains(sql, `"status" = $1`) {
		t.Errorf("expected filter in count query, got: %s", sql)
	}
	if len(args) != 1 {
		t.Errorf("args count = %d, want 1", len(args))
	}
}

func TestBuildCount_WithGroupBy(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		GroupBy("status").
		BuildCount(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `GROUP BY "status"`) {
		t.Errorf("expected GROUP BY in count query, got: %s", sql)
	}
}

// ── Error accumulation tests ────────────────────────────────────────────────

func TestErrorAccumulation_FirstErrorWins(t *testing.T) {
	qb := NewQueryBuilder(testProvider(), "site1").
		For("").                                       // first error: empty doctype
		Where(Filter{Field: "x", Operator: "BAD"}).    // should be skipped
		OrderBy("y", "INVALID")                        // should be skipped

	_, _, err := qb.Build(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	// Should be the first error (empty doctype), not subsequent ones.
	if !strings.Contains(err.Error(), "doctype must not be empty") {
		t.Errorf("expected first error, got: %v", err)
	}
}

func TestErrorAccumulation_SubsequentCallsNoOp(t *testing.T) {
	qb := NewQueryBuilder(testProvider(), "site1").
		For("")  // error

	// These should all be no-ops.
	qb.Fields("name").
		Where(Filter{Field: "x", Operator: OpEqual, Value: 1}).
		OrderBy("name", "ASC").
		GroupBy("name").
		Limit(50).
		Offset(10)

	// Verify the builder state was not modified after the error.
	if len(qb.fields) != 0 {
		t.Errorf("fields should be empty after error, got %d", len(qb.fields))
	}
	if len(qb.filters) != 0 {
		t.Errorf("filters should be empty after error, got %d", len(qb.filters))
	}
}

// ── Child table tests ───────────────────────────────────────────────────────

func TestBuild_ChildTable(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrderItem").
		Fields("name", "item_code", "qty").
		Where(Filter{Field: "parent", Operator: OpEqual, Value: "SO-001"}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"tab_sales_order_item"`) {
		t.Errorf("expected child table name, got: %s", sql)
	}
	if !strings.Contains(sql, `"parent" = $1`) {
		t.Errorf("expected parent filter, got: %s", sql)
	}
}

func TestBuild_ChildTable_RejectsNonChildColumn(t *testing.T) {
	// _liked_by is a standard column for parent tables but not child tables.
	_, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrderItem").
		Where(Filter{Field: "_liked_by", Operator: OpEqual, Value: "x"}).
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for non-child column on child table")
	}
}

// ── Table name in FROM clause ───────────────────────────────────────────────

func TestBuild_TableName(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `FROM "tab_sales_order"`) {
		t.Errorf("expected quoted table name, got: %s", sql)
	}
}

// ── Empty filters edge case ─────────────────────────────────────────────────

func TestBuild_NoFilters(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(sql, "WHERE") {
		t.Errorf("expected no WHERE clause, got: %s", sql)
	}
}

// ── IN with various slice types ─────────────────────────────────────────────

func TestBuild_In_IntSlice(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "total", Operator: OpIn, Value: []int{100, 200, 300}}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"total" IN ($1, $2, $3)`) {
		t.Errorf("expected IN clause, got: %s", sql)
	}
	if args[0] != 100 || args[1] != 200 || args[2] != 300 {
		t.Errorf("unexpected args: %v", args[:3])
	}
}

func TestBuild_In_Int64Slice(t *testing.T) {
	_, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "total", Operator: OpIn, Value: []int64{1, 2}}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuild_In_Float64Slice(t *testing.T) {
	_, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "total", Operator: OpIn, Value: []float64{1.5, 2.5}}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── toAnySlice reflect fallback ─────────────────────────────────────────────

func TestToAnySlice_ReflectFallback(t *testing.T) {
	type custom string
	input := []custom{"a", "b"}
	result, err := toAnySlice(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if result[0] != custom("a") {
		t.Errorf("result[0] = %v, want a", result[0])
	}
}

func TestToAnySlice_NonSlice(t *testing.T) {
	_, err := toAnySlice(42)
	if err == nil {
		t.Fatal("expected error for non-slice value")
	}
}

// ── Full SQL structure verification ─────────────────────────────────────────

func TestBuild_FullSQLStructure(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Fields("name", "customer", "total").
		Where(Filter{Field: "status", Operator: OpEqual, Value: "Draft"}).
		OrderBy("creation", "DESC").
		Limit(50).
		Offset(10).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `SELECT "name", "customer", "total" FROM "tab_sales_order" WHERE "status" = $1 ORDER BY "creation" DESC LIMIT $2 OFFSET $3`
	if sql != expected {
		t.Errorf("SQL mismatch.\ngot:  %s\nwant: %s", sql, expected)
	}
	if len(args) != 3 {
		t.Fatalf("args count = %d, want 3", len(args))
	}
	if args[0] != "Draft" || args[1] != 50 || args[2] != 10 {
		t.Errorf("args = %v, want [Draft 50 10]", args)
	}
}

// ── _extra JSONB transparency tests (MS-05-T2) ─────────────────────────────

// salesOrderMetaWithExtra returns a QueryMeta fixture with FieldTypes and
// NonQueryableFields populated, enabling _extra JSONB transparency.
func salesOrderMetaWithExtra() *QueryMeta {
	cols := map[string]struct{}{
		// Standard columns.
		"name": {}, "owner": {}, "creation": {}, "modified": {},
		"modified_by": {}, "docstatus": {}, "idx": {}, "workflow_state": {},
		"_extra": {}, "_user_tags": {}, "_comments": {}, "_assign": {}, "_liked_by": {},
		// User-defined real columns.
		"customer": {}, "total": {}, "status": {}, "tags": {}, "order_date": {},
	}
	fieldTypes := map[string]string{
		"customer":   "TEXT",
		"total":      "NUMERIC(18,6)",
		"status":     "TEXT",
		"tags":       "JSONB",
		"order_date": "DATE",
	}
	nonQueryable := map[string]struct{}{
		"items":     {}, // Table type
		"addresses": {}, // TableMultiSelect type
	}
	linkFields := map[string]string{
		"customer": "Customer",
	}
	dynamicLinkFields := map[string]struct{}{
		"dynamic_ref": {},
	}
	return &QueryMeta{
		Name:               "SalesOrder",
		IsChildTable:       false,
		TableName:          "tab_sales_order",
		ValidColumns:       cols,
		FieldTypes:         fieldTypes,
		NonQueryableFields: nonQueryable,
		LinkFields:         linkFields,
		DynamicLinkFields:  dynamicLinkFields,
	}
}

func testProviderWithExtra() *stubMetaProvider {
	return &stubMetaProvider{
		metas: map[string]*QueryMeta{
			"site1:SalesOrder": salesOrderMetaWithExtra(),
		},
	}
}

// ── _extra: SELECT clause ──────────────────────────────────────────────────

func TestExtra_Select_RealColumnUnchanged(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Fields("name", "status").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"name", "status"`) {
		t.Errorf("expected quoted real columns, got: %s", sql)
	}
}

func TestExtra_Select_ExtraField(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Fields("custom_color").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := `_extra->>'custom_color' AS "custom_color"`
	if !strings.Contains(sql, expected) {
		t.Errorf("expected %q in SELECT, got: %s", expected, sql)
	}
}

func TestExtra_Select_MixedRealAndExtra(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Fields("name", "custom_color", "status").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"name"`) {
		t.Errorf("expected real column in SELECT, got: %s", sql)
	}
	if !strings.Contains(sql, `_extra->>'custom_color' AS "custom_color"`) {
		t.Errorf("expected _extra field in SELECT, got: %s", sql)
	}
	if !strings.Contains(sql, `"status"`) {
		t.Errorf("expected real column in SELECT, got: %s", sql)
	}
}

// ── _extra: WHERE equality (no cast) ───────────────────────────────────────

func TestExtra_Filter_Equality(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "custom_color", Operator: OpEqual, Value: "red"}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `_extra->>'custom_color' = $1`) {
		t.Errorf("expected _extra equality filter, got: %s", sql)
	}
	if args[0] != "red" {
		t.Errorf("arg[0] = %v, want red", args[0])
	}
}

func TestExtra_Filter_NotEqual(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "custom_color", Operator: OpNotEqual, Value: "blue"}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `_extra->>'custom_color' != $1`) {
		t.Errorf("expected _extra != filter, got: %s", sql)
	}
}

// ── _extra: numeric comparisons (::NUMERIC cast) ───────────────────────────

func TestExtra_Filter_NumericGreater_Int(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "custom_age", Operator: OpGreater, Value: 30}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `(_extra->>'custom_age')::NUMERIC > $1`) {
		t.Errorf("expected NUMERIC cast, got: %s", sql)
	}
	if args[0] != 30 {
		t.Errorf("arg[0] = %v, want 30", args[0])
	}
}

func TestExtra_Filter_NumericLess_Float64(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "custom_score", Operator: OpLess, Value: 99.5}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `(_extra->>'custom_score')::NUMERIC < $1`) {
		t.Errorf("expected NUMERIC cast for float64, got: %s", sql)
	}
}

func TestExtra_Filter_NumericGTE_NoCastForString(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "custom_rank", Operator: OpGreaterOrEq, Value: "A"}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// String value → no cast, text comparison.
	if !strings.Contains(sql, `_extra->>'custom_rank' >= $1`) {
		t.Errorf("expected no cast for string value, got: %s", sql)
	}
}

// ── _extra: BOOLEAN and TIMESTAMPTZ casts ──────────────────────────────────

func TestExtra_Filter_BoolCast(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "custom_active", Operator: OpEqual, Value: true}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Equality with bool → no cast (equality uses text comparison).
	if !strings.Contains(sql, `_extra->>'custom_active' = $1`) {
		t.Errorf("expected no cast for equality, got: %s", sql)
	}
}

func TestExtra_Filter_BoolCast_Comparison(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "custom_active", Operator: OpGreater, Value: false}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `(_extra->>'custom_active')::BOOLEAN > $1`) {
		t.Errorf("expected BOOLEAN cast, got: %s", sql)
	}
}

func TestExtra_Filter_TimestamptzCast(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	sql, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "custom_deadline", Operator: OpLessOrEq, Value: ts}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `(_extra->>'custom_deadline')::TIMESTAMPTZ <= $1`) {
		t.Errorf("expected TIMESTAMPTZ cast, got: %s", sql)
	}
}

// ── _extra: LIKE / NOT LIKE (no cast) ──────────────────────────────────────

func TestExtra_Filter_Like(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "custom_note", Operator: OpLike, Value: "%urgent%"}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `_extra->>'custom_note' LIKE $1`) {
		t.Errorf("expected _extra LIKE without cast, got: %s", sql)
	}
}

func TestExtra_Filter_NotLike(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "custom_note", Operator: OpNotLike, Value: "%draft%"}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `_extra->>'custom_note' NOT LIKE $1`) {
		t.Errorf("expected _extra NOT LIKE without cast, got: %s", sql)
	}
}

// ── _extra: IS NULL / IS NOT NULL ──────────────────────────────────────────

func TestExtra_Filter_IsNull(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "custom_color", Operator: OpIsNull}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `_extra->>'custom_color' IS NULL`) {
		t.Errorf("expected _extra IS NULL, got: %s", sql)
	}
	// Only limit + offset args.
	if len(args) != 2 {
		t.Errorf("args count = %d, want 2", len(args))
	}
}

func TestExtra_Filter_IsNotNull(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "custom_color", Operator: OpIsNotNull}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `_extra->>'custom_color' IS NOT NULL`) {
		t.Errorf("expected _extra IS NOT NULL, got: %s", sql)
	}
}

// ── _extra: IN / NOT IN (text, no cast) ────────────────────────────────────

func TestExtra_Filter_In(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "custom_color", Operator: OpIn, Value: []string{"red", "blue"}}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `_extra->>'custom_color' IN ($1, $2)`) {
		t.Errorf("expected _extra IN, got: %s", sql)
	}
}

// ── _extra: BETWEEN with numeric cast ──────────────────────────────────────

func TestExtra_Filter_Between_Numeric(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "custom_age", Operator: OpBetween, Value: []any{18, 65}}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `(_extra->>'custom_age')::NUMERIC BETWEEN $1 AND $2`) {
		t.Errorf("expected NUMERIC cast BETWEEN, got: %s", sql)
	}
	if len(args) != 4 { // 2 between + limit + offset
		t.Errorf("args count = %d, want 4", len(args))
	}
}

func TestExtra_Filter_Between_String(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "custom_grade", Operator: OpBetween, Value: []any{"A", "C"}}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// String values → no cast.
	if !strings.Contains(sql, `_extra->>'custom_grade' BETWEEN $1 AND $2`) {
		t.Errorf("expected no cast for string BETWEEN, got: %s", sql)
	}
}

// ── _extra: @> JSONB contains (on _extra column itself) ────────────────────

func TestExtra_Filter_JSONContains(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "custom_meta", Operator: OpJSONContains, Value: map[string]any{"key": "val"}}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// @> on _extra field operates on the _extra column itself.
	if !strings.Contains(sql, `"_extra" @> $1::jsonb`) {
		t.Errorf("expected _extra @> on column, got: %s", sql)
	}
	jsonStr, ok := args[0].(string)
	if !ok {
		t.Fatalf("arg[0] type = %T, want string", args[0])
	}
	if !strings.Contains(jsonStr, `"key"`) {
		t.Errorf("expected JSON-encoded arg, got: %s", jsonStr)
	}
}

// ── _extra: ORDER BY ───────────────────────────────────────────────────────

func TestExtra_OrderBy(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		OrderBy("custom_color", "ASC").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `ORDER BY _extra->>'custom_color' ASC`) {
		t.Errorf("expected _extra ORDER BY, got: %s", sql)
	}
}

func TestExtra_OrderBy_MixedRealAndExtra(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		OrderBy("status", "ASC").
		OrderBy("custom_priority", "DESC").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"status" ASC, _extra->>'custom_priority' DESC`) {
		t.Errorf("expected mixed ORDER BY, got: %s", sql)
	}
}

// ── _extra: GROUP BY ───────────────────────────────────────────────────────

func TestExtra_GroupBy(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Fields("custom_color").
		GroupBy("custom_color").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `GROUP BY _extra->>'custom_color'`) {
		t.Errorf("expected _extra GROUP BY, got: %s", sql)
	}
}

// ── _extra: error cases ────────────────────────────────────────────────────

func TestExtra_NonQueryableField(t *testing.T) {
	_, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "items", Operator: OpEqual, Value: "x"}).
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for non-queryable field")
	}
	if !strings.Contains(err.Error(), "not queryable") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExtra_NonQueryableField_Select(t *testing.T) {
	_, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Fields("items").
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for non-queryable field in SELECT")
	}
	if !strings.Contains(err.Error(), "not queryable") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExtra_InvalidFieldName(t *testing.T) {
	_, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "INVALID-FIELD", Operator: OpEqual, Value: "x"}).
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid _extra field name")
	}
	if !strings.Contains(err.Error(), "invalid _extra field name") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExtra_InvalidFieldName_Uppercase(t *testing.T) {
	_, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Fields("CamelCase").
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for uppercase _extra field name")
	}
	if !strings.Contains(err.Error(), "invalid _extra field name") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExtra_InvalidFieldName_Hyphen(t *testing.T) {
	_, _, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		OrderBy("field-with-hyphens", "ASC").
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for hyphenated _extra field name")
	}
	if !strings.Contains(err.Error(), "invalid _extra field name") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── _extra: mixed real + _extra filter parameter numbering ─────────────────

func TestExtra_MixedFilters_ParameterNumbering(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(
			Filter{Field: "customer", Operator: OpEqual, Value: "ACME"},
			Filter{Field: "custom_color", Operator: OpEqual, Value: "red"},
			Filter{Field: "status", Operator: OpIn, Value: []string{"Draft", "Open"}},
			Filter{Field: "custom_age", Operator: OpGreater, Value: 25},
		).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"customer" = $1`) {
		t.Errorf("expected $1 for real column, got: %s", sql)
	}
	if !strings.Contains(sql, `_extra->>'custom_color' = $2`) {
		t.Errorf("expected $2 for _extra equality, got: %s", sql)
	}
	if !strings.Contains(sql, `"status" IN ($3, $4)`) {
		t.Errorf("expected $3,$4 for IN, got: %s", sql)
	}
	if !strings.Contains(sql, `(_extra->>'custom_age')::NUMERIC > $5`) {
		t.Errorf("expected $5 for _extra numeric, got: %s", sql)
	}
	if !strings.Contains(sql, "LIMIT $6 OFFSET $7") {
		t.Errorf("expected LIMIT $6 OFFSET $7, got: %s", sql)
	}
	if len(args) != 7 {
		t.Errorf("args count = %d, want 7", len(args))
	}
}

// ── _extra: backward compat (nil FieldTypes) ───────────────────────────────

func TestExtra_BackwardCompat_NilFieldTypes(t *testing.T) {
	// Using the original testProvider which has nil FieldTypes.
	_, _, err := NewQueryBuilder(testProvider(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "custom_color", Operator: OpEqual, Value: "red"}).
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for unknown field with nil FieldTypes")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("expected 'unknown field' error, got: %v", err)
	}
}

// ── _extra: BuildCount with _extra field ───────────────────────────────────

func TestExtra_BuildCount_WithExtraFilter(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "custom_color", Operator: OpEqual, Value: "red"}).
		BuildCount(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(sql, "SELECT COUNT(*) FROM") {
		t.Errorf("expected COUNT query, got: %s", sql)
	}
	if !strings.Contains(sql, `_extra->>'custom_color' = $1`) {
		t.Errorf("expected _extra filter in COUNT, got: %s", sql)
	}
	if len(args) != 1 {
		t.Errorf("args count = %d, want 1", len(args))
	}
}

// ── _extra: full SQL structure ─────────────────────────────────────────────

func TestExtra_FullSQLStructure(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProviderWithExtra(), "site1").
		For("SalesOrder").
		Fields("name", "custom_color").
		Where(Filter{Field: "custom_color", Operator: OpEqual, Value: "red"}).
		OrderBy("custom_color", "DESC").
		Limit(10).
		Offset(5).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `SELECT "name", _extra->>'custom_color' AS "custom_color" FROM "tab_sales_order" WHERE _extra->>'custom_color' = $1 ORDER BY _extra->>'custom_color' DESC LIMIT $2 OFFSET $3`
	if sql != expected {
		t.Errorf("SQL mismatch.\ngot:  %s\nwant: %s", sql, expected)
	}
	if len(args) != 3 {
		t.Fatalf("args count = %d, want 3", len(args))
	}
	if args[0] != "red" || args[1] != 10 || args[2] != 5 {
		t.Errorf("args = %v, want [red 10 5]", args)
	}
}

// ── Link field auto-join tests (MS-05-T3) ──────────────────────────────────

// customerMeta returns a QueryMeta fixture for "Customer" on "site1".
func customerMeta() *QueryMeta {
	cols := map[string]struct{}{
		"name": {}, "owner": {}, "creation": {}, "modified": {},
		"modified_by": {}, "docstatus": {}, "idx": {}, "workflow_state": {},
		"_extra": {}, "_user_tags": {}, "_comments": {}, "_assign": {}, "_liked_by": {},
		"territory": {}, "customer_group": {}, "customer_name": {},
	}
	return &QueryMeta{
		Name:         "Customer",
		IsChildTable: false,
		TableName:    "tab_customer",
		ValidColumns: cols,
		FieldTypes: map[string]string{
			"territory":      "TEXT",
			"customer_group": "TEXT",
			"customer_name":  "TEXT",
		},
		LinkFields: map[string]string{
			"territory": "Territory",
		},
		DynamicLinkFields: map[string]struct{}{},
	}
}

// territoryMeta returns a QueryMeta fixture for "Territory" on "site1".
func territoryMeta() *QueryMeta {
	cols := map[string]struct{}{
		"name": {}, "owner": {}, "creation": {}, "modified": {},
		"modified_by": {}, "docstatus": {}, "idx": {}, "workflow_state": {},
		"_extra": {}, "_user_tags": {}, "_comments": {}, "_assign": {}, "_liked_by": {},
		"region": {}, "parent_territory": {},
	}
	return &QueryMeta{
		Name:         "Territory",
		IsChildTable: false,
		TableName:    "tab_territory",
		ValidColumns: cols,
		FieldTypes: map[string]string{
			"region":           "TEXT",
			"parent_territory": "TEXT",
		},
	}
}

func testProviderWithJoins() *stubMetaProvider {
	return &stubMetaProvider{
		metas: map[string]*QueryMeta{
			"site1:SalesOrder": salesOrderMetaWithExtra(),
			"site1:Customer":   customerMeta(),
			"site1:Territory":  territoryMeta(),
		},
	}
}

// ── Join: SELECT clause ────────────────────────────────────────────────────

func TestJoin_SingleLevel_Select(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Fields("name", "customer.territory").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Source table should be aliased as t0.
	if !strings.Contains(sql, `FROM "tab_sales_order" AS "t0"`) {
		t.Errorf("expected aliased source table, got: %s", sql)
	}
	// LEFT JOIN for customer → Customer.
	if !strings.Contains(sql, `LEFT JOIN "tab_customer" AS "t1" ON "t1"."name" = "t0"."customer"`) {
		t.Errorf("expected LEFT JOIN clause, got: %s", sql)
	}
	// SELECT should have aliased fields.
	if !strings.Contains(sql, `"t0"."name"`) {
		t.Errorf("expected t0-qualified name field, got: %s", sql)
	}
	if !strings.Contains(sql, `"t1"."territory" AS "customer.territory"`) {
		t.Errorf("expected t1-qualified territory with alias, got: %s", sql)
	}
}

func TestJoin_SelectStar_WithJoinFilter(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "customer.territory", Operator: OpEqual, Value: "West"}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// SELECT * becomes "t0".* when joins present.
	if !strings.Contains(sql, `SELECT "t0".*`) {
		t.Errorf("expected SELECT \"t0\".*, got: %s", sql)
	}
	if !strings.Contains(sql, `LEFT JOIN "tab_customer" AS "t1"`) {
		t.Errorf("expected LEFT JOIN, got: %s", sql)
	}
}

// ── Join: WHERE clause ─────────────────────────────────────────────────────

func TestJoin_SingleLevel_Where(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "customer.territory", Operator: OpEqual, Value: "West"}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"t1"."territory" = $1`) {
		t.Errorf("expected aliased WHERE filter, got: %s", sql)
	}
	if len(args) < 1 || args[0] != "West" {
		t.Errorf("expected first arg 'West', got: %v", args)
	}
}

func TestJoin_Where_MixedLocalAndJoin(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Where(
			Filter{Field: "status", Operator: OpEqual, Value: "Draft"},
			Filter{Field: "customer.territory", Operator: OpEqual, Value: "West"},
		).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Local field should be qualified with t0.
	if !strings.Contains(sql, `"t0"."status" = $1`) {
		t.Errorf("expected t0-qualified status, got: %s", sql)
	}
	// Joined field with t1.
	if !strings.Contains(sql, `"t1"."territory" = $2`) {
		t.Errorf("expected t1-qualified territory, got: %s", sql)
	}
	if len(args) != 4 { // 2 filter + limit + offset
		t.Errorf("args count = %d, want 4", len(args))
	}
}

// ── Join: ORDER BY clause ──────────────────────────────────────────────────

func TestJoin_SingleLevel_OrderBy(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "customer.territory", Operator: OpEqual, Value: "West"}).
		OrderBy("customer.territory", "ASC").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `ORDER BY "t1"."territory" ASC`) {
		t.Errorf("expected aliased ORDER BY, got: %s", sql)
	}
}

func TestJoin_OrderBy_DefaultModified(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "customer.territory", Operator: OpEqual, Value: "West"}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default ORDER BY should be qualified with t0.
	if !strings.Contains(sql, `ORDER BY "t0"."modified" DESC`) {
		t.Errorf("expected t0-qualified default ORDER BY, got: %s", sql)
	}
}

// ── Join: GROUP BY clause ──────────────────────────────────────────────────

func TestJoin_SingleLevel_GroupBy(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Fields("customer.territory").
		GroupBy("customer.territory").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `GROUP BY "t1"."territory"`) {
		t.Errorf("expected aliased GROUP BY, got: %s", sql)
	}
}

// ── Join: depth-2 ──────────────────────────────────────────────────────────

func TestJoin_Depth2(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Fields("customer.territory.region").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Two LEFT JOINs.
	if !strings.Contains(sql, `LEFT JOIN "tab_customer" AS "t1" ON "t1"."name" = "t0"."customer"`) {
		t.Errorf("expected first JOIN, got: %s", sql)
	}
	if !strings.Contains(sql, `LEFT JOIN "tab_territory" AS "t2" ON "t2"."name" = "t1"."territory"`) {
		t.Errorf("expected second JOIN, got: %s", sql)
	}
	if !strings.Contains(sql, `"t2"."region" AS "customer.territory.region"`) {
		t.Errorf("expected t2-qualified region, got: %s", sql)
	}
}

// ── Join: depth > 2 error ──────────────────────────────────────────────────

func TestJoin_Depth3_Error(t *testing.T) {
	_, _, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Fields("customer.territory.region.subregion").
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for depth > 2")
	}
	if !strings.Contains(err.Error(), "exceeds maximum join depth") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── Join: deduplication ────────────────────────────────────────────────────

func TestJoin_Dedup_SameLink(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Fields("customer.territory", "customer.customer_name").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should only have ONE LEFT JOIN to Customer.
	count := strings.Count(sql, "LEFT JOIN")
	if count != 1 {
		t.Errorf("expected 1 LEFT JOIN (dedup), got %d in: %s", count, sql)
	}
	// Both fields should use same alias t1.
	if !strings.Contains(sql, `"t1"."territory" AS "customer.territory"`) {
		t.Errorf("expected t1.territory, got: %s", sql)
	}
	if !strings.Contains(sql, `"t1"."customer_name" AS "customer.customer_name"`) {
		t.Errorf("expected t1.customer_name, got: %s", sql)
	}
}

func TestJoin_Dedup_SelectAndWhere(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Fields("customer.territory").
		Where(Filter{Field: "customer.customer_name", Operator: OpEqual, Value: "ACME"}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	count := strings.Count(sql, "LEFT JOIN")
	if count != 1 {
		t.Errorf("expected 1 LEFT JOIN (dedup across SELECT+WHERE), got %d in: %s", count, sql)
	}
}

// ── Join: DynamicLink error ────────────────────────────────────────────────

func TestJoin_DynamicLink_Error(t *testing.T) {
	_, _, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Fields("dynamic_ref.something").
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for DynamicLink auto-join")
	}
	if !strings.Contains(err.Error(), "DynamicLink") {
		t.Errorf("expected DynamicLink error, got: %v", err)
	}
}

// ── Join: non-Link field error ─────────────────────────────────────────────

func TestJoin_NonLinkField_Error(t *testing.T) {
	_, _, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Fields("status.something").
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for non-Link field in dot notation")
	}
	if !strings.Contains(err.Error(), "not a Link field") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── Join: unknown field on target ──────────────────────────────────────────

func TestJoin_UnknownTargetField_TreatedAsExtra(t *testing.T) {
	// When the target has FieldTypes set, unknown fields that pass the _extra
	// name regex are treated as _extra fields (consistent with T2 behavior).
	sql, _, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Fields("customer.nonexistent_column").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"t1"._extra->>'nonexistent_column' AS "customer.nonexistent_column"`) {
		t.Errorf("expected _extra treatment for unknown field, got: %s", sql)
	}
}

func TestJoin_InvalidTargetFieldName(t *testing.T) {
	_, _, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Fields("customer.INVALID").
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid field name on target")
	}
	if !strings.Contains(err.Error(), "invalid _extra field name") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── Join: _extra field on target ───────────────────────────────────────────

func TestJoin_ExtraOnTarget(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Fields("customer.custom_note").
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"t1"._extra->>'custom_note' AS "customer.custom_note"`) {
		t.Errorf("expected joined _extra field expression, got: %s", sql)
	}
}

func TestJoin_ExtraOnTarget_Filter(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "customer.custom_note", Operator: OpEqual, Value: "vip"}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"t1"._extra->>'custom_note' = $1`) {
		t.Errorf("expected joined _extra filter, got: %s", sql)
	}
}

func TestJoin_ExtraOnTarget_NumericCast(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "customer.custom_score", Operator: OpGreater, Value: 100}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `("t1"._extra->>'custom_score')::NUMERIC > $1`) {
		t.Errorf("expected joined _extra numeric cast, got: %s", sql)
	}
}

// ── Join: no-join queries are unaffected ───────────────────────────────────

func TestJoin_NoJoin_NoAlias(t *testing.T) {
	sql, _, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Fields("name", "customer").
		Where(Filter{Field: "status", Operator: OpEqual, Value: "Draft"}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No aliases or JOINs.
	if strings.Contains(sql, "AS \"t0\"") {
		t.Errorf("expected no alias without joins, got: %s", sql)
	}
	if strings.Contains(sql, "LEFT JOIN") {
		t.Errorf("expected no JOIN without dots, got: %s", sql)
	}
	// Unqualified field names.
	if !strings.Contains(sql, `"status" = $1`) {
		t.Errorf("expected unqualified filter, got: %s", sql)
	}
}

// ── Join: BuildCount ───────────────────────────────────────────────────────

func TestJoin_BuildCount(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Where(Filter{Field: "customer.territory", Operator: OpEqual, Value: "West"}).
		BuildCount(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(sql, "SELECT COUNT(*)") {
		t.Errorf("expected COUNT query, got: %s", sql)
	}
	if !strings.Contains(sql, `LEFT JOIN "tab_customer" AS "t1"`) {
		t.Errorf("expected LEFT JOIN in count query, got: %s", sql)
	}
	if !strings.Contains(sql, `"t1"."territory" = $1`) {
		t.Errorf("expected aliased filter in count, got: %s", sql)
	}
	if len(args) != 1 {
		t.Errorf("args count = %d, want 1", len(args))
	}
}

// ── Join: empty segment error ──────────────────────────────────────────────

func TestJoin_EmptySegment(t *testing.T) {
	_, _, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Fields("customer.").
		Build(context.Background())
	if err == nil {
		t.Fatal("expected error for empty segment")
	}
	if !strings.Contains(err.Error(), "empty segment") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── Join: parameter numbering with joins ───────────────────────────────────

func TestJoin_ParameterNumbering(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Where(
			Filter{Field: "status", Operator: OpEqual, Value: "Draft"},
			Filter{Field: "customer.territory", Operator: OpIn, Value: []string{"West", "East"}},
		).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, `"t0"."status" = $1`) {
		t.Errorf("expected $1 for local field, got: %s", sql)
	}
	if !strings.Contains(sql, `"t1"."territory" IN ($2, $3)`) {
		t.Errorf("expected $2,$3 for joined IN, got: %s", sql)
	}
	if !strings.Contains(sql, "LIMIT $4 OFFSET $5") {
		t.Errorf("expected LIMIT $4 OFFSET $5, got: %s", sql)
	}
	if len(args) != 5 {
		t.Errorf("args count = %d, want 5", len(args))
	}
}

// ── Join: full SQL structure ───────────────────────────────────────────────

func TestJoin_FullSQLStructure(t *testing.T) {
	sql, args, err := NewQueryBuilder(testProviderWithJoins(), "site1").
		For("SalesOrder").
		Fields("name", "customer.territory").
		Where(Filter{Field: "customer.territory", Operator: OpEqual, Value: "West"}).
		OrderBy("customer.territory", "ASC").
		Limit(10).
		Offset(5).
		Build(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `SELECT "t0"."name", "t1"."territory" AS "customer.territory" FROM "tab_sales_order" AS "t0" LEFT JOIN "tab_customer" AS "t1" ON "t1"."name" = "t0"."customer" WHERE "t1"."territory" = $1 ORDER BY "t1"."territory" ASC LIMIT $2 OFFSET $3`
	if sql != expected {
		t.Errorf("SQL mismatch.\ngot:  %s\nwant: %s", sql, expected)
	}
	if len(args) != 3 {
		t.Fatalf("args count = %d, want 3", len(args))
	}
	if args[0] != "West" || args[1] != 10 || args[2] != 5 {
		t.Errorf("args = %v, want [West 10 5]", args)
	}
}

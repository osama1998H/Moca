package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/orm"
)

// ── parseFilters ────────────────────────────────────────────────────────────

func TestParseFilters_EmptyString(t *testing.T) {
	f, err := parseFilters("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f != nil {
		t.Errorf("expected nil, got %v", f)
	}
}

func TestParseFilters_SingleFilter(t *testing.T) {
	f, err := parseFilters(`[["status","=","Draft"]]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f) != 1 {
		t.Fatalf("len = %d, want 1", len(f))
	}
	if f[0].Field != "status" {
		t.Errorf("field = %q, want status", f[0].Field)
	}
	if f[0].Operator != orm.OpEqual {
		t.Errorf("operator = %q, want =", f[0].Operator)
	}
	if f[0].Value != "Draft" {
		t.Errorf("value = %v, want Draft", f[0].Value)
	}
}

func TestParseFilters_MultipleFilters(t *testing.T) {
	f, err := parseFilters(`[["status","=","Draft"],["amount",">",1000]]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f) != 2 {
		t.Fatalf("len = %d, want 2", len(f))
	}
	if f[1].Operator != orm.OpGreater {
		t.Errorf("operator = %q, want >", f[1].Operator)
	}
	// JSON numbers decode as float64.
	if v, ok := f[1].Value.(float64); !ok || v != 1000 {
		t.Errorf("value = %v (%T), want 1000", f[1].Value, f[1].Value)
	}
}

func TestParseFilters_InOperator(t *testing.T) {
	f, err := parseFilters(`[["status","in",["Draft","Submitted"]]]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f[0].Operator != orm.OpIn {
		t.Errorf("operator = %q, want in", f[0].Operator)
	}
	arr, ok := f[0].Value.([]any)
	if !ok {
		t.Fatalf("value type = %T, want []any", f[0].Value)
	}
	if len(arr) != 2 {
		t.Errorf("value length = %d, want 2", len(arr))
	}
}

func TestParseFilters_LikeOperator(t *testing.T) {
	f, err := parseFilters(`[["name","like","%test%"]]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f[0].Operator != orm.OpLike {
		t.Errorf("operator = %q, want like", f[0].Operator)
	}
}

func TestParseFilters_CaseInsensitiveOperator(t *testing.T) {
	f, err := parseFilters(`[["name","LIKE","%test%"]]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f[0].Operator != orm.OpLike {
		t.Errorf("operator = %q, want like", f[0].Operator)
	}
}

func TestParseFilters_MalformedJSON(t *testing.T) {
	_, err := parseFilters(`not json`)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseFilters_WrongArrayLength(t *testing.T) {
	_, err := parseFilters(`[["status","="]]`)
	if err == nil {
		t.Fatal("expected error for 2-element array")
	}
}

func TestParseFilters_InvalidOperator(t *testing.T) {
	_, err := parseFilters(`[["status","INVALID","Draft"]]`)
	if err == nil {
		t.Fatal("expected error for invalid operator")
	}
}

func TestParseFilters_EmptyField(t *testing.T) {
	_, err := parseFilters(`[["","=","value"]]`)
	if err == nil {
		t.Fatal("expected error for empty field")
	}
}

// ── parseListParams ─────────────────────────────────────────────────────────

func newRequest(query string) *http.Request {
	return httptest.NewRequest(http.MethodGet, "/resource/Item?"+query, nil)
}

func TestParseListParams_Defaults(t *testing.T) {
	opts, err := parseListParams(newRequest(""), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.Limit != 20 {
		t.Errorf("limit = %d, want 20", opts.Limit)
	}
	if opts.Offset != 0 {
		t.Errorf("offset = %d, want 0", opts.Offset)
	}
}

func TestParseListParams_WithAPIConfig(t *testing.T) {
	cfg := &meta.APIConfig{DefaultPageSize: 50, MaxPageSize: 200}
	opts, err := parseListParams(newRequest(""), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.Limit != 50 {
		t.Errorf("limit = %d, want 50", opts.Limit)
	}
}

func TestParseListParams_LimitCapped(t *testing.T) {
	cfg := &meta.APIConfig{MaxPageSize: 50}
	opts, err := parseListParams(newRequest("limit=999"), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.Limit != 50 {
		t.Errorf("limit = %d, want 50 (capped)", opts.Limit)
	}
}

func TestParseListParams_ExplicitLimit(t *testing.T) {
	opts, err := parseListParams(newRequest("limit=5"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.Limit != 5 {
		t.Errorf("limit = %d, want 5", opts.Limit)
	}
}

func TestParseListParams_InvalidLimit(t *testing.T) {
	_, err := parseListParams(newRequest("limit=abc"), nil)
	if err == nil {
		t.Fatal("expected error for non-numeric limit")
	}
}

func TestParseListParams_NegativeLimit(t *testing.T) {
	_, err := parseListParams(newRequest("limit=-1"), nil)
	if err == nil {
		t.Fatal("expected error for negative limit")
	}
}

func TestParseListParams_Offset(t *testing.T) {
	opts, err := parseListParams(newRequest("offset=30"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.Offset != 30 {
		t.Errorf("offset = %d, want 30", opts.Offset)
	}
}

func TestParseListParams_NegativeOffset(t *testing.T) {
	_, err := parseListParams(newRequest("offset=-5"), nil)
	if err == nil {
		t.Fatal("expected error for negative offset")
	}
}

func TestParseListParams_OrderBy(t *testing.T) {
	opts, err := parseListParams(newRequest("order_by=name+asc"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.OrderBy != "name" {
		t.Errorf("order_by = %q, want name", opts.OrderBy)
	}
	if opts.OrderDir != "ASC" {
		t.Errorf("order_dir = %q, want ASC", opts.OrderDir)
	}
}

func TestParseListParams_OrderByDefaultDir(t *testing.T) {
	opts, err := parseListParams(newRequest("order_by=modified"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.OrderDir != "DESC" {
		t.Errorf("order_dir = %q, want DESC", opts.OrderDir)
	}
}

func TestParseListParams_OrderByInvalidDir(t *testing.T) {
	_, err := parseListParams(newRequest("order_by=name+SIDEWAYS"), nil)
	if err == nil {
		t.Fatal("expected error for invalid direction")
	}
}

func TestParseListParams_FieldsCSV(t *testing.T) {
	opts, err := parseListParams(newRequest("fields=name,status,amount"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts.Fields) != 3 {
		t.Fatalf("fields length = %d, want 3", len(opts.Fields))
	}
	if opts.Fields[0] != "name" || opts.Fields[1] != "status" || opts.Fields[2] != "amount" {
		t.Errorf("fields = %v", opts.Fields)
	}
}

func TestParseListParams_FieldsJSON(t *testing.T) {
	opts, err := parseListParams(newRequest(`fields=["name","status"]`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts.Fields) != 2 {
		t.Fatalf("fields length = %d, want 2", len(opts.Fields))
	}
}

func TestParseListParams_Filters(t *testing.T) {
	opts, err := parseListParams(newRequest(`filters=[["status","=","Draft"]]`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts.AdvancedFilters) != 1 {
		t.Fatalf("advanced filters length = %d, want 1", len(opts.AdvancedFilters))
	}
	if opts.AdvancedFilters[0].Field != "status" {
		t.Errorf("filter field = %q, want status", opts.AdvancedFilters[0].Field)
	}
}

func TestParseListParams_AllParams(t *testing.T) {
	q := `limit=10&offset=5&order_by=name+asc&fields=name,status&filters=[["status","=","Draft"]]`
	opts, err := parseListParams(newRequest(q), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.Limit != 10 {
		t.Errorf("limit = %d", opts.Limit)
	}
	if opts.Offset != 5 {
		t.Errorf("offset = %d", opts.Offset)
	}
	if opts.OrderBy != "name" {
		t.Errorf("order_by = %q", opts.OrderBy)
	}
	if opts.OrderDir != "ASC" {
		t.Errorf("order_dir = %q", opts.OrderDir)
	}
	if len(opts.Fields) != 2 {
		t.Errorf("fields = %v", opts.Fields)
	}
	if len(opts.AdvancedFilters) != 1 {
		t.Errorf("filters = %v", opts.AdvancedFilters)
	}
}

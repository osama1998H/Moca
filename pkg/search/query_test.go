package search

import (
	"testing"

	"github.com/osama1998H/moca/pkg/orm"
)

func TestNotSearchableError(t *testing.T) {
	err := &NotSearchableError{Doctype: "VirtualType"}
	want := `doctype "VirtualType" is not searchable`
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestFilterError(t *testing.T) {
	err := &FilterError{Message: "field not filterable"}
	if got := err.Error(); got != "field not filterable" {
		t.Errorf("Error() = %q", got)
	}
}

func TestFormatFilterValue(t *testing.T) {
	tests := []struct {
		input any
		name  string
		want  string
	}{
		{"hello", "string", `"hello"`},
		{`has "quotes"`, "escaped_quotes", `"has \"quotes\""`},
		{`has \backslash`, "escaped_backslash", `"has \\backslash"`},
		{42, "int", "42"},
		{int64(99), "int64", "99"},
		{uint(5), "uint", "5"},
		{3.14, "float64", "3.14"},
		{float32(2.5), "float32", "2.5"},
		{true, "bool_true", "true"},
		{false, "bool_false", "false"},
		{nil, "nil", "null"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatFilterValue(tt.input)
			if got != tt.want {
				t.Errorf("formatFilterValue(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestQuoteFilterString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", `"hello"`},
		{`with "quotes"`, `"with \"quotes\""`},
		{`back\slash`, `"back\\slash"`},
		{"", `""`},
		{`both " and \`, `"both \" and \\"`},
	}
	for _, tt := range tests {
		got := quoteFilterString(tt.input)
		if got != tt.want {
			t.Errorf("quoteFilterString(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildFilterPart(t *testing.T) {
	tests := []struct {
		filter  orm.Filter
		name    string
		want    string
		wantErr bool
	}{
		{
			name:   "equal",
			filter: orm.Filter{Field: "status", Operator: orm.OpEqual, Value: "Draft"},
			want:   `status = "Draft"`,
		},
		{
			name:   "not_equal",
			filter: orm.Filter{Field: "status", Operator: orm.OpNotEqual, Value: "Closed"},
			want:   `status != "Closed"`,
		},
		{
			name:   "greater",
			filter: orm.Filter{Field: "total", Operator: orm.OpGreater, Value: 100},
			want:   "total > 100",
		},
		{
			name:   "less_or_eq",
			filter: orm.Filter{Field: "total", Operator: orm.OpLessOrEq, Value: 500},
			want:   "total <= 500",
		},
		{
			name:   "in_string",
			filter: orm.Filter{Field: "status", Operator: orm.OpIn, Value: []string{"Draft", "Open"}},
			want:   `status IN ["Draft", "Open"]`,
		},
		{
			name:   "not_in",
			filter: orm.Filter{Field: "status", Operator: orm.OpNotIn, Value: []string{"Cancelled"}},
			want:   `status NOT IN ["Cancelled"]`,
		},
		{
			name:   "between",
			filter: orm.Filter{Field: "total", Operator: orm.OpBetween, Value: []any{100, 500}},
			want:   "total 100 TO 500",
		},
		{
			name:   "is_null",
			filter: orm.Filter{Field: "status", Operator: orm.OpIsNull},
			want:   "status IS NULL",
		},
		{
			name:   "is_not_null",
			filter: orm.Filter{Field: "status", Operator: orm.OpIsNotNull},
			want:   "status IS NOT NULL",
		},
		{
			name:    "unsupported_operator",
			filter:  orm.Filter{Field: "x", Operator: orm.OpFullText, Value: "search"},
			wantErr: true,
		},
		{
			name:    "in_non_slice",
			filter:  orm.Filter{Field: "status", Operator: orm.OpIn, Value: "not-a-slice"},
			wantErr: true,
		},
		{
			name:    "between_wrong_count",
			filter:  orm.Filter{Field: "total", Operator: orm.OpBetween, Value: []any{100}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildFilterPart(tt.filter)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("buildFilterPart = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSliceValues(t *testing.T) {
	tests := []struct {
		input   any
		name    string
		wantLen int
		wantErr bool
	}{
		{name: "any_slice", input: []any{1, "two", 3.0}, wantLen: 3, wantErr: false},
		{name: "string_slice", input: []string{"a", "b"}, wantLen: 2, wantErr: false},
		{name: "int_slice", input: []int{1, 2, 3}, wantLen: 3, wantErr: false},
		{name: "not_a_slice", input: "string", wantLen: 0, wantErr: true},
		{name: "int_not_slice", input: 42, wantLen: 0, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := sliceValues(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(result), tt.wantLen)
			}
		})
	}
}

func TestStringFromMap(t *testing.T) {
	m := map[string]any{
		"name":   "SO-001",
		"total":  42,
		"nested": map[string]any{"key": "val"},
	}

	if got := stringFromMap(m, "name"); got != "SO-001" {
		t.Errorf("stringFromMap(name) = %q", got)
	}
	if got := stringFromMap(m, "total"); got != "42" {
		t.Errorf("stringFromMap(total) = %q", got)
	}
	if got := stringFromMap(m, "missing"); got != "" {
		t.Errorf("stringFromMap(missing) = %q", got)
	}
}

func TestSearchResult_ZeroValue(t *testing.T) {
	var sr SearchResult
	if sr.Name != "" || sr.DocType != "" || sr.Score != 0 || sr.Fields != nil {
		t.Error("zero value should have empty fields")
	}
}

func TestNewQueryService(t *testing.T) {
	qs := NewQueryService(nil)
	if qs == nil {
		t.Fatal("expected non-nil QueryService")
	}
}

func TestJoinFilterValues(t *testing.T) {
	got := joinFilterValues([]any{"hello", 42, true})
	want := `"hello", 42, true`
	if got != want {
		t.Errorf("joinFilterValues = %q, want %q", got, want)
	}
}

package orm

import (
	"context"
	"strings"
	"testing"
)

// ── validateReportParams tests ──────────────────────────────────────────────

func TestValidateReportParams_AllPresent(t *testing.T) {
	filters := []ReportFilter{
		{FieldName: "status", Required: true},
		{FieldName: "date", Required: true},
		{FieldName: "optional_field", Required: false},
	}
	params := map[string]any{"status": "Draft", "date": "2024-01-01"}
	if err := validateReportParams(filters, params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateReportParams_MissingRequired(t *testing.T) {
	filters := []ReportFilter{
		{FieldName: "status", Required: true},
		{FieldName: "date", Required: true},
	}
	params := map[string]any{"status": "Draft"}
	err := validateReportParams(filters, params)
	if err == nil {
		t.Fatal("expected error for missing required param")
	}
	if !strings.Contains(err.Error(), "date") {
		t.Errorf("error should mention 'date': %v", err)
	}
}

func TestValidateReportParams_OptionalMissing(t *testing.T) {
	filters := []ReportFilter{
		{FieldName: "status", Required: false},
	}
	if err := validateReportParams(filters, nil); err != nil {
		t.Fatalf("optional fields should not cause error: %v", err)
	}
}

func TestValidateReportParams_NoFilters(t *testing.T) {
	if err := validateReportParams(nil, nil); err != nil {
		t.Fatalf("no filters should pass: %v", err)
	}
}

// ── parseReportSQL tests ────────────────────────────────────────────────────

func TestParseReportSQL_SingleParam(t *testing.T) {
	sql, args, err := parseReportSQL(
		"SELECT * FROM tab_order WHERE status = %(status)s",
		map[string]any{"status": "Draft"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "$1") {
		t.Errorf("expected $1 placeholder, got: %s", sql)
	}
	if strings.Contains(sql, "%(status)s") {
		t.Errorf("named placeholder should be replaced: %s", sql)
	}
	if len(args) != 1 || args[0] != "Draft" {
		t.Errorf("args = %v, want [Draft]", args)
	}
}

func TestParseReportSQL_MultipleParams(t *testing.T) {
	sql, args, err := parseReportSQL(
		"SELECT * FROM tab_order WHERE status = %(status)s AND total > %(min_total)s",
		map[string]any{"status": "Draft", "min_total": 100},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "$1") || !strings.Contains(sql, "$2") {
		t.Errorf("expected $1 and $2, got: %s", sql)
	}
	if len(args) != 2 {
		t.Errorf("args length = %d, want 2", len(args))
	}
}

func TestParseReportSQL_RepeatedParam(t *testing.T) {
	sql, args, err := parseReportSQL(
		"SELECT * FROM t WHERE a = %(val)s OR b = %(val)s",
		map[string]any{"val": "x"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "$1") || !strings.Contains(sql, "$2") {
		t.Errorf("each occurrence should get its own $N: %s", sql)
	}
	if len(args) != 2 {
		t.Errorf("args length = %d, want 2 (one per occurrence)", len(args))
	}
}

func TestParseReportSQL_UnknownParam(t *testing.T) {
	_, _, err := parseReportSQL(
		"SELECT * FROM t WHERE a = %(missing)s",
		map[string]any{},
	)
	if err == nil {
		t.Fatal("expected error for unknown parameter")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error should mention param name: %v", err)
	}
}

func TestParseReportSQL_NoPlaceholders(t *testing.T) {
	sql, args, err := parseReportSQL("SELECT 1", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sql != "SELECT 1" {
		t.Errorf("sql = %q, want %q", sql, "SELECT 1")
	}
	if len(args) != 0 {
		t.Errorf("args should be empty: %v", args)
	}
}

// ── DDL rejection tests ────────────────────────────────────────────────────

func TestDDLRe_RejectsDDLKeywords(t *testing.T) {
	keywords := []string{
		"DROP TABLE foo",
		"ALTER TABLE foo ADD COLUMN bar TEXT",
		"TRUNCATE tab_foo",
		"DELETE FROM tab_foo",
		"UPDATE tab_foo SET x = 1",
		"INSERT INTO tab_foo VALUES (1)",
	}
	for _, q := range keywords {
		if !ddlRe.MatchString(q) {
			t.Errorf("should reject: %q", q)
		}
	}
}

func TestDDLRe_AllowsSelectWithSimilarWords(t *testing.T) {
	safe := []string{
		"SELECT updated_at, inserted_by FROM tab_order",
		"SELECT * FROM tab_order WHERE deleted_flag = false",
		"SELECT name FROM tab_alterations",
		"SELECT dropoff_location FROM tab_delivery",
	}
	for _, q := range safe {
		if ddlRe.MatchString(q) {
			t.Errorf("should allow: %q", q)
		}
	}
}

// ── ExecuteQueryReport type validation tests ────────────────────────────────

func TestExecuteQueryReport_ScriptReportRejected(t *testing.T) {
	def := ReportDef{
		Name: "test",
		Type: "ScriptReport",
	}
	_, err := ExecuteQueryReport(context.Background(), nil, def, nil)
	if err == nil {
		t.Fatal("expected error for ScriptReport")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error should mention 'not supported': %v", err)
	}
}

func TestExecuteQueryReport_EmptyQuery(t *testing.T) {
	def := ReportDef{
		Name: "test",
		Type: "QueryReport",
	}
	_, err := ExecuteQueryReport(context.Background(), nil, def, nil)
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	if !strings.Contains(err.Error(), "empty query") {
		t.Errorf("error should mention 'empty query': %v", err)
	}
}

func TestExecuteQueryReport_DDLRejected(t *testing.T) {
	def := ReportDef{
		Name:  "bad",
		Type:  "QueryReport",
		Query: "DROP TABLE tab_foo",
	}
	_, err := ExecuteQueryReport(context.Background(), nil, def, nil)
	if err == nil {
		t.Fatal("expected error for DDL in query")
	}
	if !strings.Contains(err.Error(), "forbidden DDL") {
		t.Errorf("error should mention 'forbidden DDL': %v", err)
	}
}

func TestExecuteQueryReport_MissingRequiredParams(t *testing.T) {
	def := ReportDef{
		Name:  "test",
		Type:  "QueryReport",
		Query: "SELECT * FROM tab_order WHERE status = %(status)s",
		Filters: []ReportFilter{
			{FieldName: "status", Required: true},
		},
	}
	_, err := ExecuteQueryReport(context.Background(), nil, def, map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing required param")
	}
	if !strings.Contains(err.Error(), "status") {
		t.Errorf("error should mention 'status': %v", err)
	}
}

//go:build integration

package orm_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/osama1998H/moca/pkg/orm"
)

// setupReportFixtures creates a simple test table and seeds data for report tests.
func setupReportFixtures(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	if _, err := adminPool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %[1]s.tab_report_test (
			name   TEXT PRIMARY KEY,
			status TEXT NOT NULL DEFAULT '',
			amount NUMERIC(18,6) NOT NULL DEFAULT 0
		);
		DELETE FROM %[1]s.tab_report_test;
		INSERT INTO %[1]s.tab_report_test (name, status, amount) VALUES
			('R-001', 'Draft',     100.00),
			('R-002', 'Submitted', 250.50),
			('R-003', 'Draft',      75.00),
			('R-004', 'Cancelled', 300.00),
			('R-005', 'Draft',      50.00);
	`, queryTestSchema)); err != nil {
		t.Fatalf("setup report fixtures: %v", err)
	}

	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), fmt.Sprintf(
			"DROP TABLE IF EXISTS %s.tab_report_test CASCADE", queryTestSchema,
		))
	})
}

// ── AC6: QueryReport executes with parameter binding ────────────────────────

func TestReportInteg_QueryReportWithParam(t *testing.T) {
	setupReportFixtures(t)
	mgr := newTestManager(t)
	ctx := context.Background()
	pool, err := mgr.ForSite(ctx, tenantSiteName(1))
	if err != nil {
		t.Fatalf("ForSite: %v", err)
	}

	def := orm.ReportDef{
		Name:    "Orders by Status",
		DocType: "ReportTest",
		Type:    "QueryReport",
		Query:   "SELECT name, status, amount FROM tab_report_test WHERE status = %(status)s",
		Filters: []orm.ReportFilter{
			{FieldName: "status", Label: "Status", FieldType: "Select", Required: true},
		},
	}

	results, err := orm.ExecuteQueryReport(ctx, pool, def, map[string]any{"status": "Draft"})
	if err != nil {
		t.Fatalf("ExecuteQueryReport: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("expected 3 Draft rows, got %d", len(results))
	}
	for _, row := range results {
		if row["status"] != "Draft" {
			t.Errorf("expected status=Draft, got %v", row["status"])
		}
	}
}

func TestReportInteg_QueryReportMultipleParams(t *testing.T) {
	setupReportFixtures(t)
	mgr := newTestManager(t)
	ctx := context.Background()
	pool, err := mgr.ForSite(ctx, tenantSiteName(1))
	if err != nil {
		t.Fatalf("ForSite: %v", err)
	}

	def := orm.ReportDef{
		Name:    "Filtered Orders",
		DocType: "ReportTest",
		Type:    "QueryReport",
		Query:   "SELECT name, amount FROM tab_report_test WHERE status = %(status)s AND amount > %(min_amount)s",
		Filters: []orm.ReportFilter{
			{FieldName: "status", Required: true},
			{FieldName: "min_amount", Required: true},
		},
	}

	results, err := orm.ExecuteQueryReport(ctx, pool, def, map[string]any{
		"status":     "Draft",
		"min_amount": 60.0,
	})
	if err != nil {
		t.Fatalf("ExecuteQueryReport: %v", err)
	}

	// Draft with amount > 60: R-001 (100), R-003 (75)
	if len(results) != 2 {
		t.Errorf("expected 2 rows, got %d: %v", len(results), results)
	}
}

func TestReportInteg_DDLRejectedBeforeExecution(t *testing.T) {
	def := orm.ReportDef{
		Name:  "Bad Report",
		Type:  "QueryReport",
		Query: "DELETE FROM tab_report_test",
	}

	_, err := orm.ExecuteQueryReport(context.Background(), nil, def, nil)
	if err == nil {
		t.Fatal("expected error for DDL in query")
	}
	if !strings.Contains(err.Error(), "forbidden DDL") {
		t.Errorf("error should mention DDL: %v", err)
	}
}

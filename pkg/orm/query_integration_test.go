//go:build integration

package orm_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/moca-framework/moca/pkg/orm"
)

// ── Query integration test schema ───────────────────────────────────────────

const queryTestSchema = "tenant_01"

// queryStubProvider implements orm.MetaProvider with hardcoded QueryMeta
// matching the fixture tables created below.
type queryStubProvider struct{}

func (p *queryStubProvider) QueryMeta(_ context.Context, _, doctype string) (*orm.QueryMeta, error) {
	switch doctype {
	case "QueryOrder":
		return &orm.QueryMeta{
			Name:      "QueryOrder",
			TableName: "tab_query_order",
			ValidColumns: map[string]struct{}{
				"name": {}, "customer": {}, "status": {}, "total": {},
				"creation": {}, "modified": {}, "owner": {}, "modified_by": {},
				"docstatus": {}, "idx": {}, "workflow_state": {},
				"_extra": {}, "_user_tags": {}, "_comments": {}, "_assign": {}, "_liked_by": {},
			},
			FieldTypes: map[string]string{
				"customer": "TEXT",
				"status":   "TEXT",
				"total":    "NUMERIC(18,6)",
			},
			NonQueryableFields: map[string]struct{}{},
			LinkFields:         map[string]string{"customer": "QueryCustomer"},
			DynamicLinkFields:  map[string]struct{}{},
		}, nil
	case "QueryCustomer":
		return &orm.QueryMeta{
			Name:      "QueryCustomer",
			TableName: "tab_query_customer",
			ValidColumns: map[string]struct{}{
				"name": {}, "territory": {},
				"creation": {}, "modified": {}, "owner": {}, "modified_by": {},
				"docstatus": {}, "idx": {}, "workflow_state": {},
				"_extra": {}, "_user_tags": {}, "_comments": {}, "_assign": {}, "_liked_by": {},
			},
			FieldTypes: map[string]string{
				"territory": "TEXT",
			},
			NonQueryableFields: map[string]struct{}{},
			LinkFields:         map[string]string{},
			DynamicLinkFields:  map[string]struct{}{},
		}, nil
	default:
		return nil, fmt.Errorf("unknown doctype %q", doctype)
	}
}

// setupQueryFixtures creates test tables and seed data in tenant_01 schema.
func setupQueryFixtures(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	// Create tables.
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %[1]s.tab_query_customer (
			name           TEXT PRIMARY KEY,
			territory      TEXT NOT NULL DEFAULT '',
			creation       TIMESTAMPTZ NOT NULL DEFAULT now(),
			modified       TIMESTAMPTZ NOT NULL DEFAULT now(),
			owner          TEXT NOT NULL DEFAULT '',
			modified_by    TEXT NOT NULL DEFAULT '',
			docstatus      INTEGER NOT NULL DEFAULT 0,
			idx            INTEGER NOT NULL DEFAULT 0,
			workflow_state TEXT NOT NULL DEFAULT '',
			_extra         JSONB NOT NULL DEFAULT '{}',
			_user_tags     TEXT NOT NULL DEFAULT '',
			_comments      TEXT NOT NULL DEFAULT '',
			_assign        TEXT NOT NULL DEFAULT '',
			_liked_by      TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS %[1]s.tab_query_order (
			name           TEXT PRIMARY KEY,
			customer       TEXT NOT NULL DEFAULT '',
			status         TEXT NOT NULL DEFAULT '',
			total          NUMERIC(18,6) NOT NULL DEFAULT 0,
			creation       TIMESTAMPTZ NOT NULL DEFAULT now(),
			modified       TIMESTAMPTZ NOT NULL DEFAULT now(),
			owner          TEXT NOT NULL DEFAULT '',
			modified_by    TEXT NOT NULL DEFAULT '',
			docstatus      INTEGER NOT NULL DEFAULT 0,
			idx            INTEGER NOT NULL DEFAULT 0,
			workflow_state TEXT NOT NULL DEFAULT '',
			_extra         JSONB NOT NULL DEFAULT '{}',
			_user_tags     TEXT NOT NULL DEFAULT '',
			_comments      TEXT NOT NULL DEFAULT '',
			_assign        TEXT NOT NULL DEFAULT '',
			_liked_by      TEXT NOT NULL DEFAULT ''
		);
	`, queryTestSchema)); err != nil {
		t.Fatalf("create query test tables: %v", err)
	}

	// Clear and seed data.
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(`
		DELETE FROM %[1]s.tab_query_order;
		DELETE FROM %[1]s.tab_query_customer;

		INSERT INTO %[1]s.tab_query_customer (name, territory) VALUES
			('CUST-001', 'East'),
			('CUST-002', 'West'),
			('CUST-003', 'East');

		INSERT INTO %[1]s.tab_query_order (name, customer, status, total, _extra) VALUES
			('ORD-001', 'CUST-001', 'Draft',     100.00, '{"custom_color": "red"}'),
			('ORD-002', 'CUST-001', 'Submitted',  250.50, '{"custom_color": "blue"}'),
			('ORD-003', 'CUST-002', 'Draft',      75.00,  '{"custom_color": "red"}'),
			('ORD-004', 'CUST-002', 'Cancelled',  300.00, '{"custom_color": "green"}'),
			('ORD-005', 'CUST-003', 'Draft',      50.00,  '{}'),
			('ORD-006', 'CUST-003', 'Submitted', 1000.00, '{"custom_color": "red"}'),
			('ORD-007', 'CUST-001', 'Draft',      10.00,  '{}'),
			('ORD-008', 'CUST-002', 'Submitted',  500.00, '{"custom_color": "blue"}'),
			('ORD-009', 'CUST-003', 'Cancelled',  999.99, '{}'),
			('ORD-010', 'CUST-001', 'Draft',       25.00, '{"custom_color": "red"}');
	`, queryTestSchema)); err != nil {
		t.Fatalf("seed query test data: %v", err)
	}

	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), fmt.Sprintf(`
			DROP TABLE IF EXISTS %[1]s.tab_query_order CASCADE;
			DROP TABLE IF EXISTS %[1]s.tab_query_customer CASCADE;
		`, queryTestSchema))
	})
}

// tenantPool returns a pool for the tenant_01 schema via DBManager.
func tenantPool(t *testing.T) *orm.DBManager {
	t.Helper()
	return newTestManager(t)
}

// ── AC1: Simple equality filter ─────────────────────────────────────────────

func TestQueryInteg_SimpleEqualityFilter(t *testing.T) {
	setupQueryFixtures(t)
	mgr := tenantPool(t)
	ctx := context.Background()
	pool, err := mgr.ForSite(ctx, tenantSiteName(1))
	if err != nil {
		t.Fatalf("ForSite: %v", err)
	}

	provider := &queryStubProvider{}
	sql, args, err := orm.NewQueryBuilder(provider, tenantSiteName(1)).
		For("QueryOrder").
		Fields("name", "status").
		Where(orm.Filter{Field: "status", Operator: orm.OpEqual, Value: "Draft"}).
		Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		names = append(names, vals[0].(string))
	}

	// ORD-001, ORD-003, ORD-005, ORD-007, ORD-010 are Draft
	if len(names) != 5 {
		t.Errorf("expected 5 Draft orders, got %d: %v", len(names), names)
	}
}

// ── AC2: _extra JSONB field filter ──────────────────────────────────────────

func TestQueryInteg_ExtraFieldFilter(t *testing.T) {
	setupQueryFixtures(t)
	mgr := tenantPool(t)
	ctx := context.Background()
	pool, err := mgr.ForSite(ctx, tenantSiteName(1))
	if err != nil {
		t.Fatalf("ForSite: %v", err)
	}

	provider := &queryStubProvider{}
	sql, args, err := orm.NewQueryBuilder(provider, tenantSiteName(1)).
		For("QueryOrder").
		Fields("name").
		Where(orm.Filter{Field: "custom_color", Operator: orm.OpEqual, Value: "red"}).
		Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		names = append(names, vals[0].(string))
	}

	// ORD-001, ORD-003, ORD-006, ORD-010 have custom_color=red
	if len(names) != 4 {
		t.Errorf("expected 4 orders with custom_color=red, got %d: %v", len(names), names)
	}
}

// ── AC3: Link field auto-join filter ────────────────────────────────────────

func TestQueryInteg_LinkAutoJoin(t *testing.T) {
	setupQueryFixtures(t)
	mgr := tenantPool(t)
	ctx := context.Background()
	pool, err := mgr.ForSite(ctx, tenantSiteName(1))
	if err != nil {
		t.Fatalf("ForSite: %v", err)
	}

	provider := &queryStubProvider{}
	sql, args, err := orm.NewQueryBuilder(provider, tenantSiteName(1)).
		For("QueryOrder").
		Fields("name", "customer.territory").
		Where(orm.Filter{Field: "customer.territory", Operator: orm.OpEqual, Value: "East"}).
		Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		names = append(names, vals[0].(string))
	}

	// CUST-001 (East) → ORD-001, ORD-002, ORD-007, ORD-010
	// CUST-003 (East) → ORD-005, ORD-006, ORD-009
	if len(names) != 7 {
		t.Errorf("expected 7 orders with territory=East, got %d: %v", len(names), names)
	}
}

// ── AC4: IN operator ────────────────────────────────────────────────────────

func TestQueryInteg_InOperator(t *testing.T) {
	setupQueryFixtures(t)
	mgr := tenantPool(t)
	ctx := context.Background()
	pool, err := mgr.ForSite(ctx, tenantSiteName(1))
	if err != nil {
		t.Fatalf("ForSite: %v", err)
	}

	provider := &queryStubProvider{}
	sql, args, err := orm.NewQueryBuilder(provider, tenantSiteName(1)).
		For("QueryOrder").
		Fields("name").
		Where(orm.Filter{Field: "status", Operator: orm.OpIn, Value: []string{"Draft", "Cancelled"}}).
		Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		names = append(names, vals[0].(string))
	}

	// 5 Draft + 2 Cancelled = 7
	if len(names) != 7 {
		t.Errorf("expected 7 orders (Draft+Cancelled), got %d: %v", len(names), names)
	}
}

// ── AC5: Non-existent field returns error ───────────────────────────────────

func TestQueryInteg_NonExistentFieldError(t *testing.T) {
	ctx := context.Background()
	provider := &queryStubProvider{}

	_, _, err := orm.NewQueryBuilder(provider, tenantSiteName(1)).
		For("QueryOrder").
		Where(orm.Filter{Field: "bogus_field_!!", Operator: orm.OpEqual, Value: "x"}).
		Build(ctx)
	if err == nil {
		t.Fatal("expected error for invalid field name")
	}
}

// ── Pagination test ─────────────────────────────────────────────────────────

func TestQueryInteg_Pagination(t *testing.T) {
	setupQueryFixtures(t)
	mgr := tenantPool(t)
	ctx := context.Background()
	pool, err := mgr.ForSite(ctx, tenantSiteName(1))
	if err != nil {
		t.Fatalf("ForSite: %v", err)
	}

	provider := &queryStubProvider{}

	// Count.
	countSQL, countArgs, err := orm.NewQueryBuilder(provider, tenantSiteName(1)).
		For("QueryOrder").
		BuildCount(ctx)
	if err != nil {
		t.Fatalf("BuildCount: %v", err)
	}
	var total int
	if err := pool.QueryRow(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if total != 10 {
		t.Errorf("total = %d, want 10", total)
	}

	// Page: limit 3, offset 2.
	sql, args, err := orm.NewQueryBuilder(provider, tenantSiteName(1)).
		For("QueryOrder").
		Fields("name").
		Limit(3).Offset(2).
		Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		count++
		if _, err := rows.Values(); err != nil {
			t.Fatalf("scan: %v", err)
		}
	}
	if count != 3 {
		t.Errorf("page size = %d, want 3", count)
	}
}

// ── BuildCount with filter ──────────────────────────────────────────────────

func TestQueryInteg_BuildCountWithFilter(t *testing.T) {
	setupQueryFixtures(t)
	mgr := tenantPool(t)
	ctx := context.Background()
	pool, err := mgr.ForSite(ctx, tenantSiteName(1))
	if err != nil {
		t.Fatalf("ForSite: %v", err)
	}

	provider := &queryStubProvider{}
	countSQL, countArgs, err := orm.NewQueryBuilder(provider, tenantSiteName(1)).
		For("QueryOrder").
		Where(orm.Filter{Field: "status", Operator: orm.OpEqual, Value: "Draft"}).
		BuildCount(ctx)
	if err != nil {
		t.Fatalf("BuildCount: %v", err)
	}

	var total int
	if err := pool.QueryRow(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if total != 5 {
		t.Errorf("total Draft = %d, want 5", total)
	}
}

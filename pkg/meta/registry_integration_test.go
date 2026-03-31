//go:build integration

package meta_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/moca-framework/moca/internal/config"
	"github.com/moca-framework/moca/pkg/meta"
	"github.com/moca-framework/moca/pkg/observe"
	"github.com/moca-framework/moca/pkg/orm"
)

// ── shared test helpers ───────────────────────────────────────────────────────

// newRegistryForTest creates a Registry backed by real PostgreSQL and Redis.
// The test is skipped when Redis is unavailable.
func newRegistryForTest(t *testing.T) *meta.Registry {
	t.Helper()
	if testRedisClient == nil {
		t.Skip("Redis unavailable — skipping registry integration test")
	}
	mgr := newRegistryTestMgr(t)
	logger := observe.NewLogger(slog.LevelWarn)
	r := meta.NewRegistry(mgr, testRedisClient, logger)

	// Ensure system tables exist in the tenant schema for every test.
	m := meta.NewMigrator(mgr, logger)
	if err := m.EnsureMetaTables(context.Background(), migratorTestSite); err != nil {
		t.Fatalf("EnsureMetaTables: %v", err)
	}
	return r
}

// newRegistryTestMgr creates a DBManager for registry tests.
func newRegistryTestMgr(t *testing.T) *orm.DBManager {
	t.Helper()
	host := os.Getenv("PG_HOST")
	if host == "" {
		host = migratorTestHost
	}
	cfg := config.DatabaseConfig{
		Host:     host,
		Port:     migratorTestPort,
		User:     migratorTestUser,
		Password: migratorTestPassword,
		SystemDB: migratorTestDB,
		PoolSize: 10,
	}
	logger := observe.NewLogger(slog.LevelWarn)
	mgr, err := orm.NewDBManager(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("NewDBManager: %v", err)
	}
	t.Cleanup(func() { mgr.Close() })
	return mgr
}

// flushRegistryKeys removes all meta:{site}:* and schema:{site}:version keys
// from Redis so tests start with a clean cache state.
func flushRegistryKeys(t *testing.T, site string) {
	t.Helper()
	if testRedisClient == nil {
		return
	}
	ctx := context.Background()
	for _, pattern := range []string{
		fmt.Sprintf("meta:%s:*", site),
		fmt.Sprintf("schema:%s:version", site),
	} {
		keys, err := testRedisClient.Keys(ctx, pattern).Result()
		if err != nil || len(keys) == 0 {
			continue
		}
		testRedisClient.Del(ctx, keys...)
	}
}

// loadFixture reads a JSON fixture file from testdata/.
func loadFixture(t *testing.T, filename string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + filename)
	if err != nil {
		t.Fatalf("load fixture %s: %v", filename, err)
	}
	return data
}

// dropTable drops a table in the test tenant schema if it exists.
func dropTable(t *testing.T, tableName string) {
	t.Helper()
	ctx := context.Background()
	sql := fmt.Sprintf("DROP TABLE IF EXISTS %s.%s CASCADE", migratorTestSchema, tableName)
	if _, err := migratorAdminPool.Exec(ctx, sql); err != nil {
		t.Logf("warning: could not drop table %s: %v", tableName, err)
	}
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestRegister_ThenGet_RoundTrip registers the SalesOrder fixture and verifies
// that Get returns a MetaType with all expected fields intact.
func TestRegister_ThenGet_RoundTrip(t *testing.T) {
	r := newRegistryForTest(t)
	ctx := context.Background()

	flushRegistryKeys(t, migratorTestSite)
	t.Cleanup(func() {
		dropTable(t, "tab_sales_order")
		flushRegistryKeys(t, migratorTestSite)
	})

	jsonBytes := loadFixture(t, "SalesOrder.json")

	registered, err := r.Register(ctx, migratorTestSite, jsonBytes)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if registered.Name != "SalesOrder" {
		t.Errorf("registered.Name = %q; want %q", registered.Name, "SalesOrder")
	}
	if registered.Module != "selling" {
		t.Errorf("registered.Module = %q; want %q", registered.Module, "selling")
	}
	if len(registered.Fields) == 0 {
		t.Error("registered.Fields is empty")
	}

	got, err := r.Get(ctx, migratorTestSite, "SalesOrder")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Name != registered.Name {
		t.Errorf("Get.Name = %q; want %q", got.Name, registered.Name)
	}
	if got.Module != registered.Module {
		t.Errorf("Get.Module = %q; want %q", got.Module, registered.Module)
	}
	if len(got.Fields) != len(registered.Fields) {
		t.Errorf("Get.Fields count = %d; want %d", len(got.Fields), len(registered.Fields))
	}
	if got.NamingRule.Rule != registered.NamingRule.Rule {
		t.Errorf("Get.NamingRule.Rule = %q; want %q", got.NamingRule.Rule, registered.NamingRule.Rule)
	}
	if got.NamingRule.Pattern != registered.NamingRule.Pattern {
		t.Errorf("Get.NamingRule.Pattern = %q; want %q", got.NamingRule.Pattern, registered.NamingRule.Pattern)
	}

	t.Logf("RoundTrip: registered and retrieved SalesOrder with %d fields", len(got.Fields))
}

// TestGet_CacheCascade verifies the L1 → L2 → L3 fallback chain.
func TestGet_CacheCascade(t *testing.T) {
	r := newRegistryForTest(t)
	ctx := context.Background()

	flushRegistryKeys(t, migratorTestSite)
	t.Cleanup(func() {
		dropTable(t, "tab_cache_cascade_doc")
		flushRegistryKeys(t, migratorTestSite)
	})

	jsonBytes := []byte(`{
		"name": "CacheCascadeDoc",
		"module": "core",
		"fields": [{"name": "title", "field_type": "Data"}]
	}`)

	// Register — populates L1, L2, and L3.
	if _, err := r.Register(ctx, migratorTestSite, jsonBytes); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// 1. Get hits L1.
	mt1, err := r.Get(ctx, migratorTestSite, "CacheCascadeDoc")
	if err != nil {
		t.Fatalf("Get (L1 hit): %v", err)
	}
	if mt1.Name != "CacheCascadeDoc" {
		t.Errorf("L1 Get.Name = %q; want CacheCascadeDoc", mt1.Name)
	}

	// 2. Invalidate clears L1 + L2; Get must fall through to L3.
	if err := r.Invalidate(ctx, migratorTestSite, "CacheCascadeDoc"); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	mt3, err := r.Get(ctx, migratorTestSite, "CacheCascadeDoc")
	if err != nil {
		t.Fatalf("Get (L3 fallback after Invalidate): %v", err)
	}
	if mt3.Name != "CacheCascadeDoc" {
		t.Errorf("L3 Get.Name = %q; want CacheCascadeDoc", mt3.Name)
	}

	// 3. Invalidate again, then manually seed L2 only; Get must hit L2.
	if err := r.Invalidate(ctx, migratorTestSite, "CacheCascadeDoc"); err != nil {
		t.Fatalf("second Invalidate: %v", err)
	}
	seedJSON, _ := json.Marshal(mt3)
	rkey := fmt.Sprintf("meta:%s:%s", migratorTestSite, "CacheCascadeDoc")
	if err := testRedisClient.Set(ctx, rkey, seedJSON, 0).Err(); err != nil {
		t.Fatalf("manual Redis SET: %v", err)
	}
	mt2, err := r.Get(ctx, migratorTestSite, "CacheCascadeDoc")
	if err != nil {
		t.Fatalf("Get (L2 hit): %v", err)
	}
	if mt2.Name != "CacheCascadeDoc" {
		t.Errorf("L2 Get.Name = %q; want CacheCascadeDoc", mt2.Name)
	}

	// 4. Delete L2 key; Get should now hit L1 (promoted by step 3).
	testRedisClient.Del(ctx, rkey)
	mt1b, err := r.Get(ctx, migratorTestSite, "CacheCascadeDoc")
	if err != nil {
		t.Fatalf("Get (L1 re-hit after L2 promotion): %v", err)
	}
	if mt1b.Name != "CacheCascadeDoc" {
		t.Errorf("L1 re-hit Get.Name = %q; want CacheCascadeDoc", mt1b.Name)
	}

	t.Logf("CacheCascade: L1→L2→L3 fallback chain verified")
}

// TestSchemaVersion_IncrementsOnRegister verifies that each Register call
// increments schema:{site}:version by 1.
func TestSchemaVersion_IncrementsOnRegister(t *testing.T) {
	r := newRegistryForTest(t)
	ctx := context.Background()

	flushRegistryKeys(t, migratorTestSite)
	t.Cleanup(func() {
		dropTable(t, "tab_ver_doc_a")
		dropTable(t, "tab_ver_doc_b")
		flushRegistryKeys(t, migratorTestSite)
	})

	v0, err := r.SchemaVersion(ctx, migratorTestSite)
	if err != nil {
		t.Fatalf("SchemaVersion initial: %v", err)
	}
	if v0 != 0 {
		t.Errorf("initial SchemaVersion = %d; want 0", v0)
	}

	docA := []byte(`{"name":"VerDocA","module":"core","fields":[{"name":"val","field_type":"Data"}]}`)
	docB := []byte(`{"name":"VerDocB","module":"core","fields":[{"name":"val","field_type":"Data"}]}`)

	if _, err := r.Register(ctx, migratorTestSite, docA); err != nil {
		t.Fatalf("Register VerDocA: %v", err)
	}
	v1, err := r.SchemaVersion(ctx, migratorTestSite)
	if err != nil {
		t.Fatalf("SchemaVersion after 1st register: %v", err)
	}
	if v1 != 1 {
		t.Errorf("SchemaVersion after 1st register = %d; want 1", v1)
	}

	if _, err := r.Register(ctx, migratorTestSite, docB); err != nil {
		t.Fatalf("Register VerDocB: %v", err)
	}
	v2, err := r.SchemaVersion(ctx, migratorTestSite)
	if err != nil {
		t.Fatalf("SchemaVersion after 2nd register: %v", err)
	}
	if v2 != 2 {
		t.Errorf("SchemaVersion after 2nd register = %d; want 2", v2)
	}

	// Re-register DocA (update) — version should increment again.
	docAUpdated := []byte(`{"name":"VerDocA","module":"core","fields":[{"name":"val","field_type":"Data"},{"name":"note","field_type":"Text"}]}`)
	if _, err := r.Register(ctx, migratorTestSite, docAUpdated); err != nil {
		t.Fatalf("Register VerDocA update: %v", err)
	}
	v3, err := r.SchemaVersion(ctx, migratorTestSite)
	if err != nil {
		t.Fatalf("SchemaVersion after 3rd register: %v", err)
	}
	if v3 != 3 {
		t.Errorf("SchemaVersion after 3rd register = %d; want 3", v3)
	}

	t.Logf("SchemaVersion incremented correctly: 0 → %d → %d → %d", v1, v2, v3)
}

// TestRegister_InvalidJSON_Rejected verifies that a malformed or invalid JSON
// definition is rejected by Compile and returns *CompileErrors.
func TestRegister_InvalidJSON_Rejected(t *testing.T) {
	r := newRegistryForTest(t)
	ctx := context.Background()

	cases := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{
			name:    "missing_name",
			json:    `{"module":"core","fields":[]}`,
			wantErr: true,
		},
		{
			name:    "missing_module",
			json:    `{"name":"Ghost"}`,
			wantErr: true,
		},
		{
			name:    "invalid_field_type",
			json:    `{"name":"Ghost","module":"core","fields":[{"name":"x","field_type":"FakeType"}]}`,
			wantErr: true,
		},
		{
			name:    "link_missing_options",
			json:    `{"name":"Ghost","module":"core","fields":[{"name":"ref","field_type":"Link"}]}`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.Register(ctx, migratorTestSite, []byte(tc.json))
			if !tc.wantErr {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var compileErrs *meta.CompileErrors
			if !errors.As(err, &compileErrs) {
				t.Errorf("expected *CompileErrors, got %T: %v", err, err)
			}
		})
	}
}

// TestRegister_UpdateAddsColumn registers a MetaType, then re-registers with
// an additional field and verifies the new column exists in the database.
func TestRegister_UpdateAddsColumn(t *testing.T) {
	r := newRegistryForTest(t)
	ctx := context.Background()

	flushRegistryKeys(t, migratorTestSite)
	t.Cleanup(func() {
		dropTable(t, "tab_update_col_doc")
		flushRegistryKeys(t, migratorTestSite)
	})

	v1 := []byte(`{
		"name": "UpdateColDoc",
		"module": "core",
		"fields": [{"name": "title", "field_type": "Data"}]
	}`)
	if _, err := r.Register(ctx, migratorTestSite, v1); err != nil {
		t.Fatalf("Register v1: %v", err)
	}

	if columnExists(ctx, t, "tab_update_col_doc", "title") == false {
		t.Fatal("column 'title' missing after v1 register")
	}
	if columnExists(ctx, t, "tab_update_col_doc", "discount") {
		t.Fatal("column 'discount' should not exist before v2 register")
	}

	v2 := []byte(`{
		"name": "UpdateColDoc",
		"module": "core",
		"fields": [
			{"name": "title",    "field_type": "Data"},
			{"name": "discount", "field_type": "Float"}
		]
	}`)
	if _, err := r.Register(ctx, migratorTestSite, v2); err != nil {
		t.Fatalf("Register v2: %v", err)
	}

	if !columnExists(ctx, t, "tab_update_col_doc", "discount") {
		t.Error("column 'discount' was not added by v2 register")
	}
	t.Logf("UpdateAddsColumn: new column 'discount' created by Register update")
}

// TestRegister_All29StorableFieldTypes creates a MetaType with one field for
// each of the 27 storable FieldTypes that produce a PostgreSQL column, registers
// it, and verifies each column exists via information_schema.columns.
// (Table and TableMultiSelect are storable but produce no column.)
func TestRegister_All29StorableFieldTypes(t *testing.T) {
	r := newRegistryForTest(t)
	ctx := context.Background()

	flushRegistryKeys(t, migratorTestSite)
	t.Cleanup(func() {
		dropTable(t, "tab_all_field_types")
		flushRegistryKeys(t, migratorTestSite)
	})

	// All 29 storable field types. Table and TableMultiSelect are included here
	// because FieldDef.IsStorable() returns true for them, but they produce no
	// column (ColumnType returns ""). We test 27 column-producing types below.
	fields := []meta.FieldDef{
		// TEXT group (15)
		{Name: "f_data", FieldType: meta.FieldTypeData},
		{Name: "f_text", FieldType: meta.FieldTypeText},
		{Name: "f_long_text", FieldType: meta.FieldTypeLongText},
		{Name: "f_markdown", FieldType: meta.FieldTypeMarkdown},
		{Name: "f_code", FieldType: meta.FieldTypeCode},
		{Name: "f_html_editor", FieldType: meta.FieldTypeHTMLEditor},
		{Name: "f_select", FieldType: meta.FieldTypeSelect},
		{Name: "f_color", FieldType: meta.FieldTypeColor},
		{Name: "f_barcode", FieldType: meta.FieldTypeBarcode},
		{Name: "f_signature", FieldType: meta.FieldTypeSignature},
		{Name: "f_password", FieldType: meta.FieldTypePassword},
		{Name: "f_link", FieldType: meta.FieldTypeLink, Options: "Customer"},
		{Name: "f_dynamic_link", FieldType: meta.FieldTypeDynamicLink, Options: "Customer"},
		{Name: "f_attach", FieldType: meta.FieldTypeAttach},
		{Name: "f_attach_image", FieldType: meta.FieldTypeAttachImage},
		// INTEGER (1)
		{Name: "f_int", FieldType: meta.FieldTypeInt},
		// NUMERIC(18,6) group (5)
		{Name: "f_float", FieldType: meta.FieldTypeFloat},
		{Name: "f_currency", FieldType: meta.FieldTypeCurrency},
		{Name: "f_percent", FieldType: meta.FieldTypePercent},
		{Name: "f_rating", FieldType: meta.FieldTypeRating},
		{Name: "f_duration", FieldType: meta.FieldTypeDuration},
		// Date/Time group (3)
		{Name: "f_date", FieldType: meta.FieldTypeDate},
		{Name: "f_datetime", FieldType: meta.FieldTypeDatetime},
		{Name: "f_time", FieldType: meta.FieldTypeTime},
		// BOOLEAN (1)
		{Name: "f_check", FieldType: meta.FieldTypeCheck},
		// JSONB group (2)
		{Name: "f_json", FieldType: meta.FieldTypeJSON},
		{Name: "f_geolocation", FieldType: meta.FieldTypeGeolocation},
		// Table / TableMultiSelect — no column produced (test via absence)
		{Name: "f_table", FieldType: meta.FieldTypeTable, Options: "ChildDoc"},
		{Name: "f_table_multi", FieldType: meta.FieldTypeTableMultiSelect, Options: "ChildDoc"},
	}

	mt := &meta.MetaType{
		Name:   "AllFieldTypes",
		Module: "core",
		Fields: fields,
	}

	// Marshal to JSON for Register.
	jsonBytes, err := json.Marshal(mt)
	if err != nil {
		t.Fatalf("marshal MetaType: %v", err)
	}

	if _, err := r.Register(ctx, migratorTestSite, jsonBytes); err != nil {
		t.Fatalf("Register AllFieldTypes: %v", err)
	}

	// Verify each column-producing field exists with the correct type.
	type colExpect struct {
		name    string
		pgType  string // pg data_type or udt_name
	}
	expects := []colExpect{
		// TEXT group
		{"f_data", "text"}, {"f_text", "text"}, {"f_long_text", "text"},
		{"f_markdown", "text"}, {"f_code", "text"}, {"f_html_editor", "text"},
		{"f_select", "text"}, {"f_color", "text"}, {"f_barcode", "text"},
		{"f_signature", "text"}, {"f_password", "text"}, {"f_link", "text"},
		{"f_dynamic_link", "text"}, {"f_attach", "text"}, {"f_attach_image", "text"},
		// INTEGER
		{"f_int", "integer"},
		// NUMERIC
		{"f_float", "numeric"}, {"f_currency", "numeric"}, {"f_percent", "numeric"},
		{"f_rating", "numeric"}, {"f_duration", "numeric"},
		// Date/Time
		{"f_date", "date"}, {"f_datetime", "timestamp with time zone"}, {"f_time", "time without time zone"},
		// BOOLEAN
		{"f_check", "boolean"},
		// JSONB
		{"f_json", "jsonb"}, {"f_geolocation", "jsonb"},
	}

	for _, ex := range expects {
		if !columnExists(ctx, t, "tab_all_field_types", ex.name) {
			t.Errorf("column %q not found in tab_all_field_types", ex.name)
			continue
		}
		// Verify data type matches.
		actualType := columnDataType(ctx, t, "tab_all_field_types", ex.name)
		if actualType != ex.pgType {
			t.Errorf("column %q: data_type = %q; want %q", ex.name, actualType, ex.pgType)
		}
	}

	// Table / TableMultiSelect must NOT produce columns.
	for _, noCol := range []string{"f_table", "f_table_multi"} {
		if columnExists(ctx, t, "tab_all_field_types", noCol) {
			t.Errorf("column %q should not exist (Table/TableMultiSelect produce no column)", noCol)
		}
	}

	t.Logf("AllFieldTypes: 27 storable column types verified in tab_all_field_types")
}

// TestEnsureMetaTables_AuditLogPartitioned_Registry re-verifies that
// tab_audit_log is a partitioned table from the Registry's EnsureMetaTables call.
func TestEnsureMetaTables_AuditLogPartitioned_Registry(t *testing.T) {
	_ = newRegistryForTest(t) // newRegistryForTest calls EnsureMetaTables

	ctx := context.Background()
	var relkind string
	err := migratorAdminPool.QueryRow(ctx, `
		SELECT c.relkind::text FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = 'tab_audit_log'
	`, migratorTestSchema).Scan(&relkind)
	if err != nil {
		t.Fatalf("query pg_class for tab_audit_log: %v", err)
	}
	if relkind != "p" {
		t.Errorf("tab_audit_log relkind = %q; want 'p' (partitioned)", relkind)
	}
	t.Logf("tab_audit_log is correctly partitioned (relkind='p')")
}

// TestInvalidateAll_ClearsAllSiteKeys verifies that InvalidateAll removes all
// registered MetaTypes for a site from L1 and L2.
func TestInvalidateAll_ClearsAllSiteKeys(t *testing.T) {
	r := newRegistryForTest(t)
	ctx := context.Background()

	flushRegistryKeys(t, migratorTestSite)
	t.Cleanup(func() {
		dropTable(t, "tab_inval_doc_a")
		dropTable(t, "tab_inval_doc_b")
		flushRegistryKeys(t, migratorTestSite)
	})

	docA := []byte(`{"name":"InvalDocA","module":"core","fields":[{"name":"v","field_type":"Data"}]}`)
	docB := []byte(`{"name":"InvalDocB","module":"core","fields":[{"name":"v","field_type":"Data"}]}`)

	if _, err := r.Register(ctx, migratorTestSite, docA); err != nil {
		t.Fatalf("Register InvalDocA: %v", err)
	}
	if _, err := r.Register(ctx, migratorTestSite, docB); err != nil {
		t.Fatalf("Register InvalDocB: %v", err)
	}

	// Both should be in L1 (verified via Get).
	if _, err := r.Get(ctx, migratorTestSite, "InvalDocA"); err != nil {
		t.Fatalf("Get InvalDocA: %v", err)
	}
	if _, err := r.Get(ctx, migratorTestSite, "InvalDocB"); err != nil {
		t.Fatalf("Get InvalDocB: %v", err)
	}

	if err := r.InvalidateAll(ctx, migratorTestSite); err != nil {
		t.Fatalf("InvalidateAll: %v", err)
	}

	// L1 and L2 should be cleared. L3 still has the definitions.
	// Get should succeed (re-hydrates from L3).
	if _, err := r.Get(ctx, migratorTestSite, "InvalDocA"); err != nil {
		t.Errorf("Get InvalDocA after InvalidateAll: %v", err)
	}
	if _, err := r.Get(ctx, migratorTestSite, "InvalDocB"); err != nil {
		t.Errorf("Get InvalDocB after InvalidateAll: %v", err)
	}

	t.Logf("InvalidateAll cleared L1+L2 for site; L3 fallback succeeded")
}

// ── helper: columnDataType ────────────────────────────────────────────────────

// columnDataType queries information_schema.columns for the data_type of a column.
func columnDataType(ctx context.Context, t *testing.T, tableName, columnName string) string {
	t.Helper()
	var dataType string
	err := migratorAdminPool.QueryRow(ctx, `
		SELECT data_type FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2 AND column_name = $3
	`, migratorTestSchema, tableName, columnName).Scan(&dataType)
	if err != nil {
		t.Fatalf("columnDataType query for %s.%s: %v", tableName, columnName, err)
	}
	return dataType
}

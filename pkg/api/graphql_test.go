package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// ── Mock types ──────────────────────────────────────────────────────────────

type gqlMockMetaList struct {
	listAllFn       func(ctx context.Context, site string) ([]*meta.MetaType, error)
	schemaVersionFn func(ctx context.Context, site string) (int64, error)
}

func (m *gqlMockMetaList) ListAll(ctx context.Context, site string) ([]*meta.MetaType, error) {
	return m.listAllFn(ctx, site)
}

func (m *gqlMockMetaList) SchemaVersion(ctx context.Context, site string) (int64, error) {
	if m.schemaVersionFn != nil {
		return m.schemaVersionFn(ctx, site)
	}
	return 1, nil
}

// ── Schema building tests ───────────────────────────────────────────────────

func TestFieldTypeToGraphQL_AllStorableTypes(t *testing.T) {
	storable := []meta.FieldType{
		meta.FieldTypeData, meta.FieldTypeText, meta.FieldTypeLongText,
		meta.FieldTypeCode, meta.FieldTypeMarkdown, meta.FieldTypeHTMLEditor,
		meta.FieldTypeInt, meta.FieldTypeFloat, meta.FieldTypeCurrency,
		meta.FieldTypePercent, meta.FieldTypeCheck,
		meta.FieldTypeDate, meta.FieldTypeDatetime, meta.FieldTypeTime,
		meta.FieldTypeDuration, meta.FieldTypeSelect,
		meta.FieldTypeLink, meta.FieldTypeDynamicLink,
		meta.FieldTypeAttach, meta.FieldTypeAttachImage,
		meta.FieldTypeJSON, meta.FieldTypeGeolocation,
		meta.FieldTypeColor, meta.FieldTypeSignature, meta.FieldTypeBarcode,
		meta.FieldTypeRating,
	}
	for _, ft := range storable {
		gqlType := fieldTypeToGraphQL(ft)
		if gqlType == nil {
			t.Errorf("fieldTypeToGraphQL(%q) = nil, want non-nil", ft)
		}
	}
}

func TestFieldTypeToGraphQL_PasswordExcluded(t *testing.T) {
	if got := fieldTypeToGraphQL(meta.FieldTypePassword); got != nil {
		t.Errorf("fieldTypeToGraphQL(Password) = %v, want nil", got)
	}
}

func TestFieldTypeToGraphQL_LayoutExcluded(t *testing.T) {
	layouts := []meta.FieldType{
		meta.FieldTypeSectionBreak, meta.FieldTypeColumnBreak,
		meta.FieldTypeTabBreak, meta.FieldTypeHTML,
		meta.FieldTypeButton, meta.FieldTypeHeading,
	}
	for _, ft := range layouts {
		if got := fieldTypeToGraphQL(ft); got != nil {
			t.Errorf("fieldTypeToGraphQL(%q) = %v, want nil", ft, got)
		}
	}
}

func TestBuildObjectType_BasicFields(t *testing.T) {
	mt := &meta.MetaType{
		Name: "TestItem",
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData, Label: "Title", InAPI: true},
			{Name: "count", FieldType: meta.FieldTypeInt, Label: "Count", InAPI: true},
			{Name: "amount", FieldType: meta.FieldTypeFloat, Label: "Amount", InAPI: true},
			{Name: "active", FieldType: meta.FieldTypeCheck, Label: "Active", InAPI: true},
		},
	}
	registry := map[string]*meta.MetaType{mt.Name: mt}
	typeReg := buildTypeRegistry(registry)

	objType := typeReg[mt.Name]
	fields := objType.Fields()

	// Standard fields
	for _, f := range []string{"name", "owner", "creation", "modified", "modified_by", "docstatus"} {
		if _, ok := fields[f]; !ok {
			t.Errorf("missing standard field %q", f)
		}
	}

	// Custom fields
	for _, f := range []string{"title", "count", "amount", "active"} {
		if _, ok := fields[f]; !ok {
			t.Errorf("missing field %q", f)
		}
	}
}

func TestBuildObjectType_SelectEnum(t *testing.T) {
	mt := &meta.MetaType{
		Name: "TestOrder",
		Fields: []meta.FieldDef{
			{Name: "status", FieldType: meta.FieldTypeSelect, Label: "Status", InAPI: true,
				Options: "Draft\nSubmitted\nCancelled"},
		},
	}
	registry := map[string]*meta.MetaType{mt.Name: mt}
	typeReg := buildTypeRegistry(registry)

	objType := typeReg[mt.Name]
	fields := objType.Fields()

	statusField, ok := fields["status"]
	if !ok {
		t.Fatal("missing field status")
	}

	// The type should be an enum, not a plain String.
	typeName := statusField.Type.Name()
	if !strings.Contains(typeName, "enum") {
		t.Errorf("status type name = %q, want to contain 'enum'", typeName)
	}
}

func TestBuildObjectType_LinkField(t *testing.T) {
	customer := &meta.MetaType{
		Name: "Customer",
		Fields: []meta.FieldDef{
			{Name: "customer_name", FieldType: meta.FieldTypeData, Label: "Name", InAPI: true},
		},
	}
	order := &meta.MetaType{
		Name: "SalesOrder",
		Fields: []meta.FieldDef{
			{Name: "customer", FieldType: meta.FieldTypeLink, Label: "Customer",
				InAPI: true, Options: "Customer"},
		},
	}
	registry := map[string]*meta.MetaType{
		customer.Name: customer,
		order.Name:    order,
	}
	typeReg := buildTypeRegistry(registry)

	objType := typeReg[order.Name]
	fields := objType.Fields()

	// Base field should exist as String.
	if _, ok := fields["customer"]; !ok {
		t.Error("missing base link field 'customer'")
	}

	// Companion field should exist.
	if _, ok := fields["customer_data"]; !ok {
		t.Error("missing companion field 'customer_data'")
	}
}

func TestBuildObjectType_TableField(t *testing.T) {
	item := &meta.MetaType{
		Name:         "OrderItem",
		IsChildTable: true,
		Fields: []meta.FieldDef{
			{Name: "item_code", FieldType: meta.FieldTypeData, Label: "Item Code", InAPI: true},
			{Name: "qty", FieldType: meta.FieldTypeInt, Label: "Qty", InAPI: true},
		},
	}
	order := &meta.MetaType{
		Name: "SalesOrder",
		Fields: []meta.FieldDef{
			{Name: "items", FieldType: meta.FieldTypeTable, Label: "Items",
				InAPI: true, Options: "OrderItem"},
		},
	}
	registry := map[string]*meta.MetaType{
		item.Name:  item,
		order.Name: order,
	}
	typeReg := buildTypeRegistry(registry)

	objType := typeReg[order.Name]
	fields := objType.Fields()

	if _, ok := fields["items"]; !ok {
		t.Error("missing table field 'items'")
	}
}

func TestBuildSchema_QueriesAndMutations(t *testing.T) {
	fullCRUD := &meta.MetaType{
		Name: "SalesOrder",
		APIConfig: &meta.APIConfig{
			Enabled: true, AllowGet: true, AllowList: true,
			AllowCreate: true, AllowUpdate: true, AllowDelete: true,
		},
		Fields: []meta.FieldDef{
			{Name: "customer", FieldType: meta.FieldTypeData, InAPI: true},
		},
	}
	readOnly := &meta.MetaType{
		Name: "ReadOnlyDoc",
		APIConfig: &meta.APIConfig{
			Enabled: true, AllowGet: true, AllowList: true,
		},
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData, InAPI: true},
		},
	}

	schema, err := buildSchema([]*meta.MetaType{fullCRUD, readOnly}, nil)
	if err != nil {
		t.Fatalf("buildSchema: %v", err)
	}

	queryType := schema.QueryType()
	queryFields := queryType.Fields()

	// Full CRUD type should have get + list.
	if _, ok := queryFields["sales_order"]; !ok {
		t.Error("missing query field sales_order")
	}
	if _, ok := queryFields["all_sales_order"]; !ok {
		t.Error("missing query field all_sales_order")
	}

	// ReadOnly should also have get + list.
	if _, ok := queryFields["read_only_doc"]; !ok {
		t.Error("missing query field read_only_doc")
	}

	mutType := schema.MutationType()
	if mutType == nil {
		t.Fatal("mutation type is nil")
	}
	mutFields := mutType.Fields()

	// Full CRUD should have create, update, delete.
	if _, ok := mutFields["create_sales_order"]; !ok {
		t.Error("missing mutation create_sales_order")
	}
	if _, ok := mutFields["update_sales_order"]; !ok {
		t.Error("missing mutation update_sales_order")
	}
	if _, ok := mutFields["delete_sales_order"]; !ok {
		t.Error("missing mutation delete_sales_order")
	}

	// ReadOnly should have NO mutations.
	if _, ok := mutFields["create_read_only_doc"]; ok {
		t.Error("unexpected mutation create_read_only_doc")
	}
}

func TestBuildSchema_ChildTableExcluded(t *testing.T) {
	child := &meta.MetaType{
		Name:         "OrderItem",
		IsChildTable: true,
		APIConfig: &meta.APIConfig{
			Enabled: true, AllowGet: true, AllowList: true,
			AllowCreate: true,
		},
		Fields: []meta.FieldDef{
			{Name: "item_code", FieldType: meta.FieldTypeData, InAPI: true},
		},
	}

	schema, err := buildSchema([]*meta.MetaType{child}, nil)
	if err != nil {
		t.Fatalf("buildSchema: %v", err)
	}

	queryFields := schema.QueryType().Fields()
	if _, ok := queryFields["order_item"]; ok {
		t.Error("child table should not have top-level query")
	}
}

func TestBuildSchema_SingleDoctypeNoCreateDelete(t *testing.T) {
	single := &meta.MetaType{
		Name:     "SystemSettings",
		IsSingle: true,
		APIConfig: &meta.APIConfig{
			Enabled: true, AllowGet: true, AllowUpdate: true,
			AllowCreate: true, AllowDelete: true,
		},
		Fields: []meta.FieldDef{
			{Name: "app_name", FieldType: meta.FieldTypeData, InAPI: true},
		},
	}

	schema, err := buildSchema([]*meta.MetaType{single}, nil)
	if err != nil {
		t.Fatalf("buildSchema: %v", err)
	}

	mutType := schema.MutationType()
	if mutType == nil {
		// Only update should exist, so mutation type should not be nil.
		t.Fatal("expected mutation type for update")
	}
	mutFields := mutType.Fields()

	if _, ok := mutFields["create_system_settings"]; ok {
		t.Error("Single doctype should not have create mutation")
	}
	if _, ok := mutFields["delete_system_settings"]; ok {
		t.Error("Single doctype should not have delete mutation")
	}
	if _, ok := mutFields["update_system_settings"]; !ok {
		t.Error("Single doctype should have update mutation")
	}
}

func TestBuildInputType_ExcludesReadOnlyFields(t *testing.T) {
	mt := &meta.MetaType{
		Name: "TestDoc",
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData, InAPI: true},
			{Name: "created_by", FieldType: meta.FieldTypeData, InAPI: true, ReadOnly: true},
			{Name: "api_ref", FieldType: meta.FieldTypeData, InAPI: true, APIReadOnly: true},
		},
	}

	inputType := buildInputType(mt)
	fields := inputType.Fields()

	if _, ok := fields["title"]; !ok {
		t.Error("writable field 'title' should be in input type")
	}
	if _, ok := fields["created_by"]; ok {
		t.Error("ReadOnly field 'created_by' should NOT be in input type")
	}
	if _, ok := fields["api_ref"]; ok {
		t.Error("APIReadOnly field 'api_ref' should NOT be in input type")
	}
}

// ── Handler tests ───────────────────────────────────────────────────────────

func newTestGraphQLHandler(crud CRUDService, ml MetaListResolver, mr MetaResolver, perm PermissionChecker) *GraphQLHandler {
	return &GraphQLHandler{
		crud:     crud,
		metaList: ml,
		meta:     mr,
		perm:     perm,
		logger:   slog.Default(),
	}
}

func TestHandleQuery_NoSite(t *testing.T) {
	h := newTestGraphQLHandler(nil, nil, nil, AllowAllPermissionChecker{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"query": "{ __typename }"}`
	r := httptest.NewRequest(http.MethodPost, "/api/graphql", strings.NewReader(body))
	// No site context set.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleQuery_NoUser(t *testing.T) {
	h := newTestGraphQLHandler(nil, nil, nil, AllowAllPermissionChecker{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"query": "{ __typename }"}`
	r := httptest.NewRequest(http.MethodPost, "/api/graphql", strings.NewReader(body))
	ctx := WithSite(r.Context(), testSite)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleQuery_EmptyQuery(t *testing.T) {
	h := newTestGraphQLHandler(nil, nil, nil, AllowAllPermissionChecker{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"query": ""}`
	r := httptest.NewRequest(http.MethodPost, "/api/graphql", strings.NewReader(body))
	ctx := WithSite(r.Context(), testSite)
	ctx = WithUser(ctx, testUser)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleQuery_IntrospectionSuccess(t *testing.T) {
	ml := &gqlMockMetaList{
		listAllFn: func(_ context.Context, _ string) ([]*meta.MetaType, error) {
			return []*meta.MetaType{
				{
					Name: "TestItem",
					APIConfig: &meta.APIConfig{
						Enabled: true, AllowGet: true, AllowList: true,
					},
					Fields: []meta.FieldDef{
						{Name: "title", FieldType: meta.FieldTypeData, InAPI: true},
					},
				},
			}, nil
		},
	}
	mr := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			return testMetaType(), nil
		},
	}

	h := newTestGraphQLHandler(nil, ml, mr, AllowAllPermissionChecker{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"query": "{ __schema { queryType { name } } }"}`
	r := httptest.NewRequest(http.MethodPost, "/api/graphql", strings.NewReader(body))
	ctx := WithSite(r.Context(), testSite)
	ctx = WithUser(ctx, testUser)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := result["data"]; !ok {
		t.Errorf("response missing 'data' key: %v", result)
	}
}

func TestHandleQuery_GetResolver(t *testing.T) {
	testMT := &meta.MetaType{
		Name: "TestItem",
		APIConfig: &meta.APIConfig{
			Enabled: true, AllowGet: true, AllowList: true,
			AllowCreate: true, AllowUpdate: true, AllowDelete: true,
		},
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData, InAPI: true},
		},
	}
	crud := &mockCRUD{
		getFn: func(_ *document.DocContext, _, _ string) (*document.DynamicDoc, error) {
			doc := document.NewDynamicDoc(testMT, nil, false)
			doc.Set("name", "ITEM-001")  //nolint:errcheck
			doc.Set("title", "Test Doc") //nolint:errcheck
			return doc, nil
		},
	}
	ml := &gqlMockMetaList{
		listAllFn: func(_ context.Context, _ string) ([]*meta.MetaType, error) {
			return []*meta.MetaType{testMT}, nil
		},
	}
	mr := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			return testMT, nil
		},
	}

	h := newTestGraphQLHandler(crud, ml, mr, AllowAllPermissionChecker{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"query": "{ test_item(name: \"ITEM-001\") { name title } }"}`
	r := httptest.NewRequest(http.MethodPost, "/api/graphql", strings.NewReader(body))
	ctx := WithSite(r.Context(), testSite)
	ctx = WithUser(ctx, testUser)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}

	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result) //nolint:errcheck

	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing data: %v", result)
	}
	item, ok := data["test_item"].(map[string]any)
	if !ok {
		t.Fatalf("missing test_item in data: %v", data)
	}
	if item["name"] != "ITEM-001" {
		t.Errorf("name = %v, want ITEM-001", item["name"])
	}
	if item["title"] != "Test Doc" {
		t.Errorf("title = %v, want Test Doc", item["title"])
	}
}

func TestHandleQuery_PermissionDenied(t *testing.T) {
	testMT := &meta.MetaType{
		Name: "SecretDoc",
		APIConfig: &meta.APIConfig{
			Enabled: true, AllowGet: true,
		},
		Fields: []meta.FieldDef{
			{Name: "data", FieldType: meta.FieldTypeData, InAPI: true},
		},
	}
	ml := &gqlMockMetaList{
		listAllFn: func(_ context.Context, _ string) ([]*meta.MetaType, error) {
			return []*meta.MetaType{testMT}, nil
		},
	}
	mr := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			return testMT, nil
		},
	}
	perm := &mockPermChecker{
		err: &PermissionDeniedError{User: "user@test.com", Doctype: "SecretDoc", Perm: "read"},
	}

	h := newTestGraphQLHandler(nil, ml, mr, perm)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"query": "{ secret_doc(name: \"DOC-001\") { name } }"}`
	r := httptest.NewRequest(http.MethodPost, "/api/graphql", strings.NewReader(body))
	ctx := WithSite(r.Context(), testSite)
	ctx = WithUser(ctx, &auth.User{Email: "user@test.com", Roles: []string{"User"}})
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	// GraphQL always returns 200; errors in body.
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result) //nolint:errcheck
	errs, ok := result["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Errorf("expected errors in response: %v", result)
	}
}

func TestHandleQuery_CreateMutation(t *testing.T) {
	testMT := &meta.MetaType{
		Name: "TestItem",
		APIConfig: &meta.APIConfig{
			Enabled: true, AllowGet: true, AllowCreate: true,
		},
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData, InAPI: true},
		},
	}
	crud := &mockCRUD{
		insertFn: func(_ *document.DocContext, _ string, values map[string]any) (*document.DynamicDoc, error) {
			doc := document.NewDynamicDoc(testMT, nil, false)
			doc.Set("name", "NEW-001")                  //nolint:errcheck
			doc.Set("title", values["title"].(string))   //nolint:errcheck
			return doc, nil
		},
	}
	ml := &gqlMockMetaList{
		listAllFn: func(_ context.Context, _ string) ([]*meta.MetaType, error) {
			return []*meta.MetaType{testMT}, nil
		},
	}
	mr := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			return testMT, nil
		},
	}

	h := newTestGraphQLHandler(crud, ml, mr, AllowAllPermissionChecker{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"query": "mutation { create_test_item(input: { title: \"New Item\" }) { name title } }"}`
	r := httptest.NewRequest(http.MethodPost, "/api/graphql", strings.NewReader(body))
	ctx := WithSite(r.Context(), testSite)
	ctx = WithUser(ctx, testUser)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}

	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result) //nolint:errcheck
	data, _ := result["data"].(map[string]any)
	created, _ := data["create_test_item"].(map[string]any)
	if created["name"] != "NEW-001" {
		t.Errorf("name = %v, want NEW-001", created["name"])
	}
}

func TestHandleQuery_DeleteMutation(t *testing.T) {
	testMT := &meta.MetaType{
		Name: "TestItem",
		APIConfig: &meta.APIConfig{
			Enabled: true, AllowGet: true, AllowDelete: true,
		},
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData, InAPI: true},
		},
	}
	crud := &mockCRUD{
		deleteFn: func(_ *document.DocContext, _, _ string) error {
			return nil
		},
	}
	ml := &gqlMockMetaList{
		listAllFn: func(_ context.Context, _ string) ([]*meta.MetaType, error) {
			return []*meta.MetaType{testMT}, nil
		},
	}
	mr := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			return testMT, nil
		},
	}

	h := newTestGraphQLHandler(crud, ml, mr, AllowAllPermissionChecker{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"query": "mutation { delete_test_item(name: \"ITEM-001\") }"}`
	r := httptest.NewRequest(http.MethodPost, "/api/graphql", strings.NewReader(body))
	ctx := WithSite(r.Context(), testSite)
	ctx = WithUser(ctx, testUser)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}

	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result) //nolint:errcheck
	data, _ := result["data"].(map[string]any)
	if data["delete_test_item"] != true {
		t.Errorf("delete result = %v, want true", data["delete_test_item"])
	}
}

func TestHandlePlayground(t *testing.T) {
	h := newTestGraphQLHandler(nil, nil, nil, AllowAllPermissionChecker{})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	r := httptest.NewRequest(http.MethodGet, "/api/graphql/playground", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(w.Body.String(), "GraphiQL") {
		t.Error("playground response should contain GraphiQL")
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"SalesOrder", "SalesOrder"},
		{"my field", "my_field"},
		{"123bad", "_123bad"},
		{"hello-world", "hello_world"},
		{"", "_"},
	}
	for _, tt := range tests {
		got := sanitizeName(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"SalesOrder", "sales_order"},
		{"TestItem", "test_item"},
		{"HTTPConfig", "http_config"},
		{"SystemSettings", "system_settings"},
	}
	for _, tt := range tests {
		got := toSnakeCase(tt.input)
		if got != tt.want {
			t.Errorf("toSnakeCase(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSchemaCache_RebuildOnVersionChange(t *testing.T) {
	version := int64(1)
	ml := &gqlMockMetaList{
		listAllFn: func(_ context.Context, _ string) ([]*meta.MetaType, error) {
			return []*meta.MetaType{
				{
					Name: "TestItem",
					APIConfig: &meta.APIConfig{Enabled: true, AllowGet: true},
					Fields:    []meta.FieldDef{{Name: "title", FieldType: meta.FieldTypeData, InAPI: true}},
				},
			}, nil
		},
		schemaVersionFn: func(_ context.Context, _ string) (int64, error) {
			return version, nil
		},
	}
	mr := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			return nil, nil
		},
	}

	h := newTestGraphQLHandler(nil, ml, mr, AllowAllPermissionChecker{})

	ctx := context.Background()
	s1, err := h.getOrBuildSchema(ctx, "site1")
	if err != nil {
		t.Fatal(err)
	}

	// Same version → should return cached.
	s2, err := h.getOrBuildSchema(ctx, "site1")
	if err != nil {
		t.Fatal(err)
	}
	if s1 != s2 {
		t.Error("expected same schema pointer for same version")
	}

	// Increment version → should rebuild.
	version = 2
	s3, err := h.getOrBuildSchema(ctx, "site1")
	if err != nil {
		t.Fatal(err)
	}
	if s1 == s3 {
		t.Error("expected different schema pointer after version change")
	}
}

// ── Test helpers ────────────────────────────────────────────────────────────

// buildTypeRegistry builds object types from a map of MetaTypes (for testing).
func buildTypeRegistry(mts map[string]*meta.MetaType) map[string]*graphql.Object {
	typeReg := make(map[string]*graphql.Object, len(mts))
	for _, mt := range mts {
		typeReg[mt.Name] = buildObjectType(mt, typeReg, nil)
	}
	return typeReg
}

// Reuse existing test fixtures from rest_test.go for site/user.
var _ = testSite
var _ = testUser
var _ = &tenancy.SiteContext{}

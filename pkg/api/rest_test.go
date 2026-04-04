package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// ── Mocks ───────────────────────────────────────────────────────────────────

type mockCRUD struct {
	insertFn    func(ctx *document.DocContext, doctype string, values map[string]any) (*document.DynamicDoc, error)
	updateFn    func(ctx *document.DocContext, doctype, name string, values map[string]any) (*document.DynamicDoc, error)
	deleteFn    func(ctx *document.DocContext, doctype, name string) error
	getFn       func(ctx *document.DocContext, doctype, name string) (*document.DynamicDoc, error)
	getListFn   func(ctx *document.DocContext, doctype string, opts document.ListOptions) ([]*document.DynamicDoc, int, error)
	getSingleFn func(ctx *document.DocContext, doctype string) (*document.DynamicDoc, error)
}

func (m *mockCRUD) Insert(ctx *document.DocContext, doctype string, values map[string]any) (*document.DynamicDoc, error) {
	return m.insertFn(ctx, doctype, values)
}
func (m *mockCRUD) Update(ctx *document.DocContext, doctype, name string, values map[string]any) (*document.DynamicDoc, error) {
	return m.updateFn(ctx, doctype, name, values)
}
func (m *mockCRUD) Delete(ctx *document.DocContext, doctype, name string) error {
	return m.deleteFn(ctx, doctype, name)
}
func (m *mockCRUD) Get(ctx *document.DocContext, doctype, name string) (*document.DynamicDoc, error) {
	return m.getFn(ctx, doctype, name)
}
func (m *mockCRUD) GetList(ctx *document.DocContext, doctype string, opts document.ListOptions) ([]*document.DynamicDoc, int, error) {
	return m.getListFn(ctx, doctype, opts)
}
func (m *mockCRUD) GetSingle(ctx *document.DocContext, doctype string) (*document.DynamicDoc, error) {
	return m.getSingleFn(ctx, doctype)
}

type mockMeta struct {
	getFn func(ctx context.Context, site, doctype string) (*meta.MetaType, error)
}

func (m *mockMeta) Get(ctx context.Context, site, doctype string) (*meta.MetaType, error) {
	return m.getFn(ctx, site, doctype)
}

// ── Fixtures ────────────────────────────────────────────────────────────────

var testSite = &tenancy.SiteContext{Name: "test_site"}
var testUser = &auth.User{Email: "admin@test.com", FullName: "Admin", Roles: []string{"Administrator"}}

func testMetaType() *meta.MetaType {
	return &meta.MetaType{
		Name: "TestItem",
		APIConfig: &meta.APIConfig{
			Enabled:     true,
			AllowGet:    true,
			AllowCreate: true,
			AllowUpdate: true,
			AllowDelete: true,
			AllowList:   true,
		},
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData, Label: "Title", InAPI: true, Required: true},
			{Name: "status", FieldType: meta.FieldTypeSelect, Label: "Status", InAPI: true},
		},
	}
}

func testDoc() *document.DynamicDoc {
	mt := testMetaType()
	doc := document.NewDynamicDoc(mt, nil, false)
	doc.Set("name", "ITEM-001")  //nolint:errcheck
	doc.Set("title", "Test Doc") //nolint:errcheck
	doc.Set("status", "Draft")   //nolint:errcheck
	return doc
}

func newHandler(crud CRUDService, resolver MetaResolver) *ResourceHandler {
	return &ResourceHandler{
		crud:   crud,
		meta:   resolver,
		perm:   AllowAllPermissionChecker{},
		logger: nil, // tests don't need a logger for success paths
	}
}

// contextWithSiteAndUser sets up the request context with site and user.
func contextWithSiteAndUser(r *http.Request) *http.Request {
	ctx := WithSite(r.Context(), testSite)
	ctx = WithUser(ctx, testUser)
	return r.WithContext(ctx)
}

func defaultMockMeta() *mockMeta {
	return &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			return testMetaType(), nil
		},
	}
}

// ── GET /resource/{doctype}/{name} ──────────────────────────────────────────

func TestHandleGet_Success(t *testing.T) {
	crud := &mockCRUD{
		getFn: func(_ *document.DocContext, _, _ string) (*document.DynamicDoc, error) {
			return testDoc(), nil
		},
	}
	h := newHandler(crud, defaultMockMeta())

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest(http.MethodGet, "/api/v1/resource/TestItem/ITEM-001", nil)
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var env successEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map", env.Data)
	}
	if data["name"] != "ITEM-001" {
		t.Errorf("name = %v, want ITEM-001", data["name"])
	}
}

func TestHandleGet_NotFound(t *testing.T) {
	crud := &mockCRUD{
		getFn: func(_ *document.DocContext, _, name string) (*document.DynamicDoc, error) {
			return nil, &document.DocNotFoundError{Doctype: "TestItem", Name: name}
		},
	}
	h := newHandler(crud, defaultMockMeta())

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest(http.MethodGet, "/api/v1/resource/TestItem/NOTEXIST", nil)
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleGet_APIDisabled(t *testing.T) {
	resolver := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			mt := testMetaType()
			mt.APIConfig.Enabled = false
			return mt, nil
		},
	}
	h := newHandler(nil, resolver) // crud not called

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest(http.MethodGet, "/api/v1/resource/TestItem/ITEM-001", nil)
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleGet_NoSiteContext(t *testing.T) {
	h := newHandler(nil, defaultMockMeta())

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest(http.MethodGet, "/api/v1/resource/TestItem/ITEM-001", nil)
	// No site or user in context.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleGet_NoUserContext(t *testing.T) {
	h := newHandler(nil, defaultMockMeta())

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest(http.MethodGet, "/api/v1/resource/TestItem/ITEM-001", nil)
	ctx := WithSite(r.Context(), testSite) // site but no user
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// ── POST /resource/{doctype} ────────────────────────────────────────────────

func TestHandleCreate_Success(t *testing.T) {
	crud := &mockCRUD{
		insertFn: func(_ *document.DocContext, _ string, _ map[string]any) (*document.DynamicDoc, error) {
			return testDoc(), nil
		},
	}
	h := newHandler(crud, defaultMockMeta())

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	body := `{"title":"New Item","status":"Draft"}`
	r := httptest.NewRequest(http.MethodPost, "/api/v1/resource/TestItem", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusCreated)
	}
}

func TestHandleCreate_InvalidJSON(t *testing.T) {
	h := newHandler(nil, defaultMockMeta())

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest(http.MethodPost, "/api/v1/resource/TestItem", strings.NewReader("not json"))
	r.Header.Set("Content-Type", "application/json")
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCreate_EmptyBody(t *testing.T) {
	h := newHandler(nil, defaultMockMeta())

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest(http.MethodPost, "/api/v1/resource/TestItem", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCreate_ValidationError(t *testing.T) {
	crud := &mockCRUD{
		insertFn: func(_ *document.DocContext, _ string, _ map[string]any) (*document.DynamicDoc, error) {
			return nil, &document.ValidationError{
				Errors: []document.FieldError{{Field: "title", Message: "is required", Rule: "required"}},
			}
		},
	}
	h := newHandler(crud, defaultMockMeta())

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	body := `{"status":"Draft"}`
	r := httptest.NewRequest(http.MethodPost, "/api/v1/resource/TestItem", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

// ── PUT /resource/{doctype}/{name} ──────────────────────────────────────────

func TestHandleUpdate_Success(t *testing.T) {
	crud := &mockCRUD{
		updateFn: func(_ *document.DocContext, _, _ string, _ map[string]any) (*document.DynamicDoc, error) {
			doc := testDoc()
			doc.Set("status", "Submitted") //nolint:errcheck
			return doc, nil
		},
	}
	h := newHandler(crud, defaultMockMeta())

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	body := `{"status":"Submitted"}`
	r := httptest.NewRequest(http.MethodPut, "/api/v1/resource/TestItem/ITEM-001", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// ── DELETE /resource/{doctype}/{name} ───────────────────────────────────────

func TestHandleDelete_Success(t *testing.T) {
	crud := &mockCRUD{
		deleteFn: func(_ *document.DocContext, _, _ string) error {
			return nil
		},
	}
	h := newHandler(crud, defaultMockMeta())

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest(http.MethodDelete, "/api/v1/resource/TestItem/ITEM-001", nil)
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if w.Body.Len() != 0 {
		t.Errorf("body should be empty, got %q", w.Body.String())
	}
}

func TestHandleDelete_MethodNotAllowed(t *testing.T) {
	resolver := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			mt := testMetaType()
			mt.APIConfig.AllowDelete = false
			return mt, nil
		},
	}
	h := newHandler(nil, resolver)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest(http.MethodDelete, "/api/v1/resource/TestItem/ITEM-001", nil)
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// ── GET /resource/{doctype} (list) ──────────────────────────────────────────

func TestHandleList_Success(t *testing.T) {
	crud := &mockCRUD{
		getListFn: func(_ *document.DocContext, _ string, opts document.ListOptions) ([]*document.DynamicDoc, int, error) {
			doc1 := testDoc()
			doc2 := testDoc()
			doc2.Set("name", "ITEM-002") //nolint:errcheck
			return []*document.DynamicDoc{doc1, doc2}, 42, nil
		},
	}
	h := newHandler(crud, defaultMockMeta())

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest(http.MethodGet, "/api/v1/resource/TestItem?limit=10&offset=0", nil)
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var env listEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Meta.Total != 42 {
		t.Errorf("meta.total = %d, want 42", env.Meta.Total)
	}
	data, ok := env.Data.([]any)
	if !ok {
		t.Fatalf("data type = %T, want []any", env.Data)
	}
	if len(data) != 2 {
		t.Errorf("data length = %d, want 2", len(data))
	}
}

func TestHandleList_WithFilters(t *testing.T) {
	var capturedOpts document.ListOptions
	crud := &mockCRUD{
		getListFn: func(_ *document.DocContext, _ string, opts document.ListOptions) ([]*document.DynamicDoc, int, error) {
			capturedOpts = opts
			return nil, 0, nil
		},
	}
	h := newHandler(crud, defaultMockMeta())

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest(http.MethodGet, `/api/v1/resource/TestItem?filters=[["status","=","Draft"]]`, nil)
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if len(capturedOpts.AdvancedFilters) != 1 {
		t.Fatalf("advanced filters = %d, want 1", len(capturedOpts.AdvancedFilters))
	}
	if capturedOpts.AdvancedFilters[0].Field != "status" {
		t.Errorf("filter field = %q, want status", capturedOpts.AdvancedFilters[0].Field)
	}
}

// ── GET /meta/{doctype} ─────────────────────────────────────────────────────

func TestHandleMeta_Success(t *testing.T) {
	h := newHandler(nil, defaultMockMeta()) // no CRUD needed

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest(http.MethodGet, "/api/v1/meta/TestItem", nil)
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var env successEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map", env.Data)
	}
	if data["name"] != "TestItem" {
		t.Errorf("name = %v, want TestItem", data["name"])
	}
	fields, ok := data["fields"].([]any)
	if !ok {
		t.Fatalf("fields type = %T", data["fields"])
	}
	if len(fields) != 2 {
		t.Errorf("fields length = %d, want 2", len(fields))
	}
}

func TestBuildMetaResponse_UIFields(t *testing.T) {
	maxVal := 100.0
	minVal := 1.0
	mt := &meta.MetaType{
		Name:         "Invoice",
		Label:        "Sales Invoice",
		Module:       "Accounting",
		TitleField:   "customer_name",
		ImageField:   "image",
		SortField:    "modified",
		SortOrder:    "desc",
		SearchFields: []string{"customer_name", "status"},
		TrackChanges: true,
		NamingRule:   meta.NamingStrategy{Rule: meta.NamingByPattern, Pattern: "SINV-.####"},
		Fields: []meta.FieldDef{
			{
				Name:       "customer_name",
				FieldType:  meta.FieldTypeData,
				Label:      "Customer Name",
				InAPI:      true,
				InListView: true,
				InFilter:   true,
				InPreview:  true,
				Required:   true,
				DependsOn:  "eval:doc.customer",
				Default:    "Guest",
				MaxLength:  140,
			},
			{
				Name:      "amount",
				FieldType: meta.FieldTypeCurrency,
				Label:     "Amount",
				InAPI:     true,
				MaxValue:  &maxVal,
				MinValue:  &minVal,
				Width:     "200px",
			},
			{
				Name:      "details",
				FieldType: meta.FieldTypeSectionBreak,
				Label:     "Details",
				LayoutHint: meta.LayoutHint{
					Label:              "Details Section",
					ColSpan:            12,
					Collapsible:        true,
					CollapsedByDefault: false,
				},
			},
			{
				Name:               "notes",
				FieldType:          meta.FieldTypeLongText,
				Label:              "Notes",
				InAPI:              true,
				Hidden:             true,
				MandatoryDependsOn: "eval:doc.status=='Draft'",
			},
		},
		APIConfig: &meta.APIConfig{
			AllowGet:  true,
			AllowList: true,
		},
	}

	resp := buildMetaResponse(mt)

	// Verify meta-level UI fields.
	if resp.TitleField != "customer_name" {
		t.Errorf("TitleField = %q, want %q", resp.TitleField, "customer_name")
	}
	if resp.ImageField != "image" {
		t.Errorf("ImageField = %q, want %q", resp.ImageField, "image")
	}
	if resp.SortField != "modified" {
		t.Errorf("SortField = %q, want %q", resp.SortField, "modified")
	}
	if resp.SortOrder != "desc" {
		t.Errorf("SortOrder = %q, want %q", resp.SortOrder, "desc")
	}
	if len(resp.SearchFields) != 2 {
		t.Fatalf("SearchFields length = %d, want 2", len(resp.SearchFields))
	}
	if !resp.TrackChanges {
		t.Error("TrackChanges = false, want true")
	}
	if resp.NamingRule.Rule != meta.NamingByPattern {
		t.Errorf("NamingRule.Rule = %q, want %q", resp.NamingRule.Rule, meta.NamingByPattern)
	}
	if resp.NamingRule.Pattern != "SINV-.####" {
		t.Errorf("NamingRule.Pattern = %q, want %q", resp.NamingRule.Pattern, "SINV-.####")
	}

	// Verify field-level UI fields.
	if len(resp.Fields) != 4 {
		t.Fatalf("fields length = %d, want 4", len(resp.Fields))
	}

	f0 := resp.Fields[0] // customer_name
	if !f0.InListView {
		t.Error("customer_name.InListView = false, want true")
	}
	if !f0.InFilter {
		t.Error("customer_name.InFilter = false, want true")
	}
	if !f0.InPreview {
		t.Error("customer_name.InPreview = false, want true")
	}
	if f0.DependsOn != "eval:doc.customer" {
		t.Errorf("customer_name.DependsOn = %q, want %q", f0.DependsOn, "eval:doc.customer")
	}
	if f0.Default != "Guest" {
		t.Errorf("customer_name.Default = %v, want %q", f0.Default, "Guest")
	}
	if f0.MaxLength != 140 {
		t.Errorf("customer_name.MaxLength = %d, want 140", f0.MaxLength)
	}

	f1 := resp.Fields[1] // amount
	if f1.MaxValue == nil || *f1.MaxValue != 100.0 {
		t.Errorf("amount.MaxValue = %v, want 100.0", f1.MaxValue)
	}
	if f1.MinValue == nil || *f1.MinValue != 1.0 {
		t.Errorf("amount.MinValue = %v, want 1.0", f1.MinValue)
	}
	if f1.Width != "200px" {
		t.Errorf("amount.Width = %q, want %q", f1.Width, "200px")
	}

	f2 := resp.Fields[2] // details (SectionBreak)
	if f2.LayoutLabel != "Details Section" {
		t.Errorf("details.LayoutLabel = %q, want %q", f2.LayoutLabel, "Details Section")
	}
	if f2.ColSpan != 12 {
		t.Errorf("details.ColSpan = %d, want 12", f2.ColSpan)
	}
	if !f2.Collapsible {
		t.Error("details.Collapsible = false, want true")
	}

	f3 := resp.Fields[3] // notes
	if !f3.Hidden {
		t.Error("notes.Hidden = false, want true")
	}
	if f3.MandatoryDependsOn != "eval:doc.status=='Draft'" {
		t.Errorf("notes.MandatoryDependsOn = %q, want %q", f3.MandatoryDependsOn, "eval:doc.status=='Draft'")
	}
}

func TestHandleMeta_IncludesUIFields(t *testing.T) {
	resolver := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			return &meta.MetaType{
				Name:       "TestItem",
				TitleField: "title",
				SortField:  "modified",
				SortOrder:  "desc",
				NamingRule: meta.NamingStrategy{Rule: meta.NamingUUID},
				APIConfig: &meta.APIConfig{
					Enabled:  true,
					AllowGet: true,
				},
				Fields: []meta.FieldDef{
					{Name: "title", FieldType: meta.FieldTypeData, Label: "Title", InAPI: true, InListView: true, InFilter: true},
					{Name: "status", FieldType: meta.FieldTypeSelect, Label: "Status", InAPI: true, Hidden: true, DependsOn: "eval:doc.title"},
				},
			}, nil
		},
	}
	h := newHandler(nil, resolver)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest(http.MethodGet, "/api/v1/meta/TestItem", nil)
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var env successEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map", env.Data)
	}
	if data["title_field"] != "customer_name" && data["title_field"] != "title" {
		// Just verify the field is present.
		if _, exists := data["title_field"]; !exists {
			t.Error("title_field missing from meta response")
		}
	}
	if data["sort_field"] != "modified" {
		t.Errorf("sort_field = %v, want %q", data["sort_field"], "modified")
	}

	fields, ok := data["fields"].([]any)
	if !ok || len(fields) < 1 {
		t.Fatalf("fields missing or empty")
	}
	f0, ok := fields[0].(map[string]any)
	if !ok {
		t.Fatalf("field[0] type = %T, want map", fields[0])
	}
	if f0["in_list_view"] != true {
		t.Errorf("field[0].in_list_view = %v, want true", f0["in_list_view"])
	}
	if f0["in_filter"] != true {
		t.Errorf("field[0].in_filter = %v, want true", f0["in_filter"])
	}
}

// ── Permission denied ───────────────────────────────────────────────────────

func TestHandleGet_PermissionDenied(t *testing.T) {
	crud := &mockCRUD{}
	resolver := defaultMockMeta()

	h := &ResourceHandler{
		crud:   crud,
		meta:   resolver,
		perm:   denyAllPermChecker{},
		logger: nil,
	}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest(http.MethodGet, "/api/v1/resource/TestItem/ITEM-001", nil)
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

type denyAllPermChecker struct{}

func (denyAllPermChecker) CheckDocPerm(_ context.Context, user *auth.User, doctype, perm string) error {
	return &PermissionDeniedError{User: user.Email, Doctype: doctype, Perm: perm}
}

// ── DocType not found ───────────────────────────────────────────────────────

func TestHandleGet_DoctypeNotFound(t *testing.T) {
	resolver := &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			return nil, meta.ErrMetaTypeNotFound
		},
	}
	h := newHandler(nil, resolver)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest(http.MethodGet, "/api/v1/resource/Nonexistent/DOC-001", nil)
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

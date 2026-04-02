package api

import (
	"context"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

// ── ReadOnlyEnforcer ───────────────────────────────────────────────────────

func TestReadOnlyEnforcer_StripsReadOnlyFields(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "title", InAPI: true},
			{Name: "created_by", InAPI: true, ReadOnly: true},
			{Name: "status", InAPI: true, APIReadOnly: true},
		},
	}
	e := NewReadOnlyEnforcer(mt)

	body := map[string]any{
		"title":      "Hello",
		"created_by": "admin",
		"status":     "Draft",
	}
	got, err := e.TransformRequest(context.Background(), mt, body)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["created_by"]; ok {
		t.Error("expected created_by to be stripped")
	}
	if _, ok := got["status"]; ok {
		t.Error("expected status to be stripped")
	}
	if got["title"] != "Hello" {
		t.Errorf("title = %v, want Hello", got["title"])
	}
}

func TestReadOnlyEnforcer_PreservesNonReadOnly(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "title", InAPI: true},
			{Name: "description", InAPI: true},
		},
	}
	e := NewReadOnlyEnforcer(mt)

	body := map[string]any{"title": "A", "description": "B"}
	got, err := e.TransformRequest(context.Background(), mt, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d keys, want 2", len(got))
	}
}

func TestReadOnlyEnforcer_ResponsePassthrough(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "created_by", InAPI: true, ReadOnly: true},
		},
	}
	e := NewReadOnlyEnforcer(mt)

	body := map[string]any{"created_by": "admin"}
	got, err := e.TransformResponse(context.Background(), mt, body)
	if err != nil {
		t.Fatal(err)
	}
	if got["created_by"] != "admin" {
		t.Error("response should pass through read-only fields")
	}
}

// ── AliasRemapper ──────────────────────────────────────────────────────────

func TestAliasRemapper_RequestAliasToInternal(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "customer_name", InAPI: true, APIAlias: "customer"},
			{Name: "grand_total", InAPI: true, APIAlias: "total"},
			{Name: "status", InAPI: true},
		},
	}
	a := NewAliasRemapper(mt, nil)

	body := map[string]any{
		"customer": "ACME",
		"total":    100.0,
		"status":   "Draft",
	}
	got, err := a.TransformRequest(context.Background(), mt, body)
	if err != nil {
		t.Fatal(err)
	}
	if got["customer_name"] != "ACME" {
		t.Errorf("customer_name = %v, want ACME", got["customer_name"])
	}
	if got["grand_total"] != 100.0 {
		t.Errorf("grand_total = %v, want 100", got["grand_total"])
	}
	if got["status"] != "Draft" {
		t.Errorf("status = %v, want Draft", got["status"])
	}
	if _, ok := got["customer"]; ok {
		t.Error("alias key 'customer' should not be in output")
	}
}

func TestAliasRemapper_ResponseInternalToAlias(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "customer_name", InAPI: true, APIAlias: "customer"},
			{Name: "status", InAPI: true},
		},
	}
	a := NewAliasRemapper(mt, nil)

	body := map[string]any{
		"customer_name": "ACME",
		"status":        "Draft",
	}
	got, err := a.TransformResponse(context.Background(), mt, body)
	if err != nil {
		t.Fatal(err)
	}
	if got["customer"] != "ACME" {
		t.Errorf("customer = %v, want ACME", got["customer"])
	}
	if got["status"] != "Draft" {
		t.Errorf("status = %v, want Draft", got["status"])
	}
	if _, ok := got["customer_name"]; ok {
		t.Error("internal key 'customer_name' should not be in output")
	}
}

func TestAliasRemapper_RoundTrip(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "customer_name", InAPI: true, APIAlias: "customer"},
		},
	}
	a := NewAliasRemapper(mt, nil)

	original := map[string]any{"customer": "ACME"}
	internal, err := a.TransformRequest(context.Background(), mt, original)
	if err != nil {
		t.Fatal(err)
	}
	back, err := a.TransformResponse(context.Background(), mt, internal)
	if err != nil {
		t.Fatal(err)
	}
	if back["customer"] != "ACME" {
		t.Errorf("round-trip failed: got %v", back)
	}
}

func TestAliasRemapper_VersionMappingOverridesBase(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "grand_total", InAPI: true, APIAlias: "total"},
		},
	}
	// Version mapping overrides "total" alias with "total_amount".
	versionMapping := map[string]string{"total_amount": "grand_total"}
	a := NewAliasRemapper(mt, versionMapping)

	body := map[string]any{"grand_total": 500.0}
	got, err := a.TransformResponse(context.Background(), mt, body)
	if err != nil {
		t.Fatal(err)
	}
	// Version mapping takes precedence.
	if got["total_amount"] != 500.0 {
		t.Errorf("total_amount = %v, want 500", got["total_amount"])
	}
}

func TestAliasRemapper_NoAliases(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "title", InAPI: true},
		},
	}
	a := NewAliasRemapper(mt, nil)

	body := map[string]any{"title": "Test"}
	got, err := a.TransformRequest(context.Background(), mt, body)
	if err != nil {
		t.Fatal(err)
	}
	if got["title"] != "Test" {
		t.Errorf("title = %v, want Test", got["title"])
	}
}

// ── FieldFilter ────────────────────────────────────────────────────────────

func TestFieldFilter_RemovesNonAPIFields(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "title", InAPI: true},
			{Name: "internal_code", InAPI: false},
			{Name: "status", InAPI: true},
		},
	}
	f := NewFieldFilter(mt, nil)

	body := map[string]any{
		"title":         "Doc",
		"internal_code": "X123",
		"status":        "Draft",
	}
	got, err := f.TransformResponse(context.Background(), mt, body)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["internal_code"]; ok {
		t.Error("internal_code (InAPI=false) should be removed")
	}
	if got["title"] != "Doc" {
		t.Errorf("title = %v, want Doc", got["title"])
	}
	if got["status"] != "Draft" {
		t.Errorf("status = %v, want Draft", got["status"])
	}
}

func TestFieldFilter_RemovesExcludeFields(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "title", InAPI: true},
			{Name: "docstatus", InAPI: true},
		},
		APIConfig: &meta.APIConfig{
			ExcludeFields: []string{"docstatus"},
		},
	}
	f := NewFieldFilter(mt, nil)

	body := map[string]any{"title": "Doc", "docstatus": 0}
	got, err := f.TransformResponse(context.Background(), mt, body)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["docstatus"]; ok {
		t.Error("docstatus should be excluded")
	}
}

func TestFieldFilter_VersionExcludeFieldsMerged(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "title", InAPI: true},
			{Name: "modified_by", InAPI: true},
			{Name: "user_tags", InAPI: true},
		},
		APIConfig: &meta.APIConfig{
			ExcludeFields: []string{"modified_by"},
		},
	}
	f := NewFieldFilter(mt, []string{"user_tags"})

	body := map[string]any{"title": "Doc", "modified_by": "admin", "user_tags": "[]"}
	got, err := f.TransformResponse(context.Background(), mt, body)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["modified_by"]; ok {
		t.Error("modified_by should be excluded (base)")
	}
	if _, ok := got["user_tags"]; ok {
		t.Error("user_tags should be excluded (version)")
	}
}

func TestFieldFilter_AlwaysIncludeOverridesExclude(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "name", InAPI: true},
			{Name: "title", InAPI: true},
		},
		APIConfig: &meta.APIConfig{
			ExcludeFields: []string{"name"},
			AlwaysInclude: []string{"name"},
		},
	}
	f := NewFieldFilter(mt, nil)

	body := map[string]any{"name": "DOC-001", "title": "Doc"}
	got, err := f.TransformResponse(context.Background(), mt, body)
	if err != nil {
		t.Fatal(err)
	}
	if got["name"] != "DOC-001" {
		t.Error("name should be included via AlwaysInclude even if in ExcludeFields")
	}
}

func TestFieldFilter_DefaultFieldsOnList(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "name", InAPI: true},
			{Name: "title", InAPI: true},
			{Name: "description", InAPI: true},
			{Name: "status", InAPI: true},
		},
		APIConfig: &meta.APIConfig{
			DefaultFields: []string{"name", "title"},
			AlwaysInclude: []string{"name"},
		},
	}
	f := NewFieldFilter(mt, nil)

	ctx := WithOperationType(context.Background(), OpList)
	body := map[string]any{
		"name":        "DOC-001",
		"title":       "Doc",
		"description": "Long text",
		"status":      "Draft",
	}
	got, err := f.TransformResponse(ctx, mt, body)
	if err != nil {
		t.Fatal(err)
	}
	if got["name"] != "DOC-001" {
		t.Error("name should be in default fields")
	}
	if got["title"] != "Doc" {
		t.Error("title should be in default fields")
	}
	if _, ok := got["description"]; ok {
		t.Error("description should be excluded (not in default fields)")
	}
	if _, ok := got["status"]; ok {
		t.Error("status should be excluded (not in default fields)")
	}
}

func TestFieldFilter_DefaultFieldsNotAppliedOnGet(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "name", InAPI: true},
			{Name: "title", InAPI: true},
			{Name: "description", InAPI: true},
		},
		APIConfig: &meta.APIConfig{
			DefaultFields: []string{"name", "title"},
		},
	}
	f := NewFieldFilter(mt, nil)

	ctx := WithOperationType(context.Background(), OpGet)
	body := map[string]any{"name": "DOC-001", "title": "Doc", "description": "Long"}
	got, err := f.TransformResponse(ctx, mt, body)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["description"]; !ok {
		t.Error("description should be present on OpGet (DefaultFields only apply to OpList)")
	}
}

func TestFieldFilter_RequestPassthrough(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "internal_code", InAPI: false},
		},
	}
	f := NewFieldFilter(mt, nil)

	body := map[string]any{"internal_code": "X"}
	got, err := f.TransformRequest(context.Background(), mt, body)
	if err != nil {
		t.Fatal(err)
	}
	if got["internal_code"] != "X" {
		t.Error("request passthrough should not filter fields")
	}
}

func TestFieldFilter_AlwaysIncludeOnList(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "name", InAPI: true},
			{Name: "owner", InAPI: true},
		},
		APIConfig: &meta.APIConfig{
			DefaultFields: []string{"name"},
			AlwaysInclude: []string{"owner"},
		},
	}
	f := NewFieldFilter(mt, nil)

	ctx := WithOperationType(context.Background(), OpList)
	body := map[string]any{"name": "DOC-001", "owner": "admin"}
	got, err := f.TransformResponse(ctx, mt, body)
	if err != nil {
		t.Fatal(err)
	}
	if got["owner"] != "admin" {
		t.Error("owner should be present via AlwaysInclude even on list")
	}
}

// ── TransformerChain ───────────────────────────────────────────────────────

func TestTransformerChain_AppliesToRequestAndResponse(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "customer_name", InAPI: true, APIAlias: "customer"},
			{Name: "created_by", InAPI: true, ReadOnly: true},
			{Name: "internal", InAPI: false},
		},
		APIConfig: &meta.APIConfig{Enabled: true},
	}

	chain := NewTransformerChain(mt, nil)

	// Request: alias→internal, strip read-only.
	reqBody := map[string]any{
		"customer":   "ACME",
		"created_by": "admin",
	}
	got, err := chain.TransformRequest(context.Background(), mt, reqBody)
	if err != nil {
		t.Fatal(err)
	}
	if got["customer_name"] != "ACME" {
		t.Errorf("expected customer_name=ACME, got %v", got["customer_name"])
	}
	if _, ok := got["created_by"]; ok {
		t.Error("created_by should be stripped from request")
	}

	// Response: internal→alias, filter non-API fields.
	respBody := map[string]any{
		"customer_name": "ACME",
		"internal":      "secret",
		"created_by":    "admin",
	}
	got, err = chain.TransformResponse(context.Background(), mt, respBody)
	if err != nil {
		t.Fatal(err)
	}
	if got["customer"] != "ACME" {
		t.Errorf("expected customer=ACME, got %v", got["customer"])
	}
	if _, ok := got["internal"]; ok {
		t.Error("internal (InAPI=false) should be filtered from response")
	}
	// created_by is ReadOnly but should appear in response (ReadOnlyEnforcer is no-op on response).
	if got["created_by"] != "admin" {
		t.Errorf("created_by should be in response, got %v", got["created_by"])
	}
}

func TestTransformerChain_WithVersion(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "grand_total", InAPI: true, APIAlias: "total"},
			{Name: "modified_by", InAPI: true},
		},
		APIConfig: &meta.APIConfig{
			Enabled: true,
			Versions: []meta.APIVersion{
				{
					Version:       "v2",
					Status:        "active",
					FieldMapping:  map[string]string{"total_amount": "grand_total"},
					ExcludeFields: []string{"modified_by"},
				},
			},
		},
	}

	v2 := &mt.APIConfig.Versions[0]
	chain := NewTransformerChain(mt, v2)

	respBody := map[string]any{
		"grand_total": 500.0,
		"modified_by": "admin",
	}
	got, err := chain.TransformResponse(context.Background(), mt, respBody)
	if err != nil {
		t.Fatal(err)
	}
	// Version mapping overrides base alias: "total_amount" instead of "total".
	if got["total_amount"] != 500.0 {
		t.Errorf("expected total_amount=500, got %v", got["total_amount"])
	}
	// Version excludes modified_by.
	if _, ok := got["modified_by"]; ok {
		t.Error("modified_by should be excluded in v2")
	}
}

// ── buildTransformerChain ──────────────────────────────────────────────────

func TestBuildTransformerChain_ResolvesVersion(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "title", InAPI: true},
		},
		APIConfig: &meta.APIConfig{
			Enabled: true,
			Versions: []meta.APIVersion{
				{Version: "v1", Status: "active"},
				{Version: "v2", Status: "active", ExcludeFields: []string{"title"}},
			},
		},
	}

	// v1 keeps title.
	ctx := WithAPIVersion(context.Background(), "v1")
	chain := buildTransformerChain(ctx, mt)
	body := map[string]any{"title": "Hello"}
	got, err := chain.TransformResponse(ctx, mt, body)
	if err != nil {
		t.Fatal(err)
	}
	if got["title"] != "Hello" {
		t.Error("v1 should keep title")
	}

	// v2 excludes title.
	ctx = WithAPIVersion(context.Background(), "v2")
	chain = buildTransformerChain(ctx, mt)
	body = map[string]any{"title": "Hello"}
	got, err = chain.TransformResponse(ctx, mt, body)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["title"]; ok {
		t.Error("v2 should exclude title")
	}
}

func TestBuildTransformerChain_NoVersionConfig(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "title", InAPI: true},
		},
	}
	ctx := WithAPIVersion(context.Background(), "v1")
	chain := buildTransformerChain(ctx, mt)
	if len(chain) != 3 {
		t.Errorf("expected 3 transformers in chain, got %d", len(chain))
	}
}

// ── filterMetaFields ───────────────────────────────────────────────────────

func TestFilterMetaFields_FiltersAndAliases(t *testing.T) {
	mt := &meta.MetaType{
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData, InAPI: true, Label: "Title"},
			{Name: "internal", FieldType: meta.FieldTypeData, InAPI: false, Label: "Internal"},
			{Name: "customer_name", FieldType: meta.FieldTypeData, InAPI: true, Label: "Customer", APIAlias: "customer"},
		},
		APIConfig: &meta.APIConfig{Enabled: true},
	}
	resp := buildMetaResponse(mt)
	ctx := WithAPIVersion(context.Background(), "v1")
	filtered := filterMetaFields(resp, mt, ctx)

	if len(filtered.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(filtered.Fields))
	}
	// customer_name should be renamed to "customer" via alias.
	found := false
	for _, f := range filtered.Fields {
		if f.Name == "customer" {
			found = true
		}
		if f.Name == "internal" {
			t.Error("internal field should be filtered out")
		}
	}
	if !found {
		t.Error("expected customer alias in filtered meta fields")
	}
}

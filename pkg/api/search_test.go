package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/orm"
	pkgsearch "github.com/osama1998H/moca/pkg/search"
	"github.com/osama1998H/moca/pkg/tenancy"
)

type searchMetaResolverStub struct {
	mt  *meta.MetaType
	err error
}

func (s searchMetaResolverStub) Get(context.Context, string, string) (*meta.MetaType, error) {
	return s.mt, s.err
}

//nolint:govet // Test stub layout is not performance-sensitive.
type searchServiceStub struct {
	total   int
	results []pkgsearch.SearchResult
	filters []orm.Filter
	err     error
}

func (s *searchServiceStub) Search(_ context.Context, _ string, _ *meta.MetaType, _ string, filters []orm.Filter, _ int, _ int) ([]pkgsearch.SearchResult, int, error) {
	s.filters = filters
	return s.results, s.total, s.err
}

type denyPermChecker struct{}

func (denyPermChecker) CheckDocPerm(_ context.Context, user *auth.User, doctype string, perm string) error {
	return &PermissionDeniedError{User: user.Email, Doctype: doctype, Perm: perm}
}

type stripSecretTransformer struct{}

func (stripSecretTransformer) TransformRequest(_ context.Context, _ *meta.MetaType, body map[string]any) (map[string]any, error) {
	return body, nil
}

func (stripSecretTransformer) TransformResponse(_ context.Context, _ *meta.MetaType, body map[string]any) (map[string]any, error) {
	delete(body, "secret")
	return body, nil
}

func TestSearchHandlerMissingQueryReturnsBadRequest(t *testing.T) {
	handler := &SearchHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?doctype=Order", nil)
	rr := httptest.NewRecorder()

	handler.handleSearch(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestSearchHandlerRequiresTenant(t *testing.T) {
	mt := searchableMetaType()
	handler := &SearchHandler{
		meta: searchMetaResolverStub{mt: mt},
		perm: AllowAllPermissionChecker{},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=alpha&doctype=Order", nil)
	req = req.WithContext(WithUser(req.Context(), &auth.User{Email: "user@example.com"}))
	rr := httptest.NewRecorder()

	handler.handleSearch(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestSearchHandlerPermissionDenied(t *testing.T) {
	mt := searchableMetaType()
	handler := &SearchHandler{
		meta: searchMetaResolverStub{mt: mt},
		perm: denyPermChecker{},
	}
	req := newSearchRequest("/api/v1/search?q=alpha&doctype=Order")
	rr := httptest.NewRecorder()

	handler.handleSearch(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
}

func TestSearchHandlerReturnsUnavailableWhenSearchBackendMissing(t *testing.T) {
	mt := searchableMetaType()
	handler := &SearchHandler{
		meta: searchMetaResolverStub{mt: mt},
		perm: AllowAllPermissionChecker{},
	}
	req := newSearchRequest("/api/v1/search?q=alpha&doctype=Order")
	rr := httptest.NewRecorder()

	handler.handleSearch(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
}

func TestSearchHandlerSuccessFiltersResponseFields(t *testing.T) {
	mt := searchableMetaType()
	searchSvc := &searchServiceStub{
		results: []pkgsearch.SearchResult{
			{
				Name:    "ORD-1",
				DocType: "Order",
				Fields: map[string]any{
					"name":    "ORD-1",
					"doctype": "Order",
					"title":   "Alpha",
					"secret":  "hidden",
				},
			},
		},
		total: 1,
	}
	handler := &SearchHandler{
		search:    searchSvc,
		meta:      searchMetaResolverStub{mt: mt},
		perm:      AllowAllPermissionChecker{},
		fieldPerm: stripSecretTransformer{},
	}
	req := newSearchRequest(`/api/v1/search?q=alpha&doctype=Order&page=2&limit=5&filters=[["status","=","Draft"]]`)
	rr := httptest.NewRecorder()

	handler.handleSearch(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var payload struct {
		Data []map[string]any `json:"data"`
		Meta struct {
			Total  int `json:"total"`
			Limit  int `json:"limit"`
			Offset int `json:"offset"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.Meta.Total != 1 || payload.Meta.Limit != 5 || payload.Meta.Offset != 5 {
		t.Fatalf("meta = %#v", payload.Meta)
	}
	if _, ok := payload.Data[0]["secret"]; ok {
		t.Fatalf("response field filtering failed: %#v", payload.Data[0])
	}
	if len(searchSvc.filters) != 1 || searchSvc.filters[0].Field != "status" {
		t.Fatalf("filters = %#v", searchSvc.filters)
	}
}

func TestSearchHandlerMapsFilterErrorsToBadRequest(t *testing.T) {
	mt := searchableMetaType()
	handler := &SearchHandler{
		search: &searchServiceStub{
			err: &pkgsearch.FilterError{Message: "filters: unsupported operator \"like\""},
		},
		meta: searchMetaResolverStub{mt: mt},
		perm: AllowAllPermissionChecker{},
	}
	req := newSearchRequest(`/api/v1/search?q=alpha&doctype=Order&filters=[["status","like","%draft%"]]`)
	rr := httptest.NewRecorder()

	handler.handleSearch(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func newSearchRequest(target string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	ctx := WithSite(req.Context(), &tenancy.SiteContext{Name: "acme"})
	ctx = WithUser(ctx, &auth.User{Email: "user@example.com"})
	return req.WithContext(ctx)
}

func searchableMetaType() *meta.MetaType {
	return &meta.MetaType{
		Name: "Order",
		APIConfig: &meta.APIConfig{
			Enabled:   true,
			AllowList: true,
		},
		Fields: []meta.FieldDef{
			{Name: "title", Searchable: true, InAPI: true},
		},
	}
}

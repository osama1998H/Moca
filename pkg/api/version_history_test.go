package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
)

func testVersionMetaType() *meta.MetaType {
	mt := testMetaType()
	mt.TrackChanges = true
	return mt
}

func versionMockMeta(mt *meta.MetaType) *mockMeta {
	return &mockMeta{
		getFn: func(_ context.Context, _, _ string) (*meta.MetaType, error) {
			return mt, nil
		},
	}
}

func TestHandleVersions_Success(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	crud := &mockCRUD{
		getVersionsFn: func(_ *document.DocContext, _, _ string, _, _ int) ([]document.VersionRecord, int, error) {
			return []document.VersionRecord{
				{
					Name:       "ver-002",
					RefDoctype: "TestItem",
					DocName:    "ITEM-001",
					Data:       map[string]any{"changed": map[string]any{"title": map[string]any{"old": "A", "new": "B"}}, "snapshot": map[string]any{"title": "B"}},
					Owner:      "admin@test.com",
					Creation:   now,
				},
				{
					Name:       "ver-001",
					RefDoctype: "TestItem",
					DocName:    "ITEM-001",
					Data:       map[string]any{"changed": nil, "snapshot": map[string]any{"title": "A"}},
					Owner:      "admin@test.com",
					Creation:   now.Add(-time.Hour),
				},
			}, 2, nil
		},
	}

	mt := testVersionMetaType()
	handler := newHandler(crud, versionMockMeta(mt))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest("GET", "/api/v1/resource/TestItem/ITEM-001/versions", nil)
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp listEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := resp.Data.([]any)
	if !ok {
		t.Fatalf("expected data to be an array, got %T", resp.Data)
	}
	if len(data) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(data))
	}
	if resp.Meta.Total != 2 {
		t.Fatalf("expected total=2, got %d", resp.Meta.Total)
	}
}

func TestHandleVersions_EmptyHistory(t *testing.T) {
	crud := &mockCRUD{
		getVersionsFn: func(_ *document.DocContext, _, _ string, _, _ int) ([]document.VersionRecord, int, error) {
			return []document.VersionRecord{}, 0, nil
		},
	}

	mt := testVersionMetaType()
	handler := newHandler(crud, versionMockMeta(mt))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest("GET", "/api/v1/resource/TestItem/ITEM-001/versions", nil)
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp listEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Meta.Total != 0 {
		t.Fatalf("expected total=0, got %d", resp.Meta.Total)
	}
}

func TestHandleVersions_TrackChangesDisabled(t *testing.T) {
	mt := testMetaType() // TrackChanges defaults to false
	handler := newHandler(&mockCRUD{}, versionMockMeta(mt))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest("GET", "/api/v1/resource/TestItem/ITEM-001/versions", nil)
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}

	var resp errorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error.Code != "VERSION_TRACKING_DISABLED" {
		t.Fatalf("error code = %q, want VERSION_TRACKING_DISABLED", resp.Error.Code)
	}
}

func TestHandleVersions_Pagination(t *testing.T) {
	var gotLimit, gotOffset int
	crud := &mockCRUD{
		getVersionsFn: func(_ *document.DocContext, _, _ string, limit, offset int) ([]document.VersionRecord, int, error) {
			gotLimit = limit
			gotOffset = offset
			return []document.VersionRecord{}, 0, nil
		},
	}

	mt := testVersionMetaType()
	handler := newHandler(crud, versionMockMeta(mt))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest("GET", "/api/v1/resource/TestItem/ITEM-001/versions?limit=5&offset=10", nil)
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if gotLimit != 5 {
		t.Fatalf("limit = %d, want 5", gotLimit)
	}
	if gotOffset != 10 {
		t.Fatalf("offset = %d, want 10", gotOffset)
	}
}

func TestHandleVersions_InvalidLimit(t *testing.T) {
	mt := testVersionMetaType()
	handler := newHandler(&mockCRUD{}, versionMockMeta(mt))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, "v1")

	r := httptest.NewRequest("GET", "/api/v1/resource/TestItem/ITEM-001/versions?limit=abc", nil)
	r = contextWithSiteAndUser(r)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestParseVersionParams_Defaults(t *testing.T) {
	r := httptest.NewRequest("GET", "/test", nil)
	limit, offset, err := parseVersionParams(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if limit != 20 {
		t.Fatalf("default limit = %d, want 20", limit)
	}
	if offset != 0 {
		t.Fatalf("default offset = %d, want 0", offset)
	}
}

func TestParseVersionParams_MaxLimit(t *testing.T) {
	r := httptest.NewRequest("GET", "/test?limit=500", nil)
	limit, _, err := parseVersionParams(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if limit != 100 {
		t.Fatalf("capped limit = %d, want 100", limit)
	}
}

package search

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/meilisearch/meilisearch-go"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/pkg/events"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/orm"
	"github.com/osama1998H/moca/pkg/queue"
)

type fakeRawService struct {
	indexes       map[string]*fakeRawIndex
	getIndexErr   map[string]error
	createConfigs []*meilisearch.IndexConfig
	waited        []int64
}

func (s *fakeRawService) Index(uid string) rawIndex {
	if s.indexes == nil {
		s.indexes = make(map[string]*fakeRawIndex)
	}
	if _, ok := s.indexes[uid]; !ok {
		s.indexes[uid] = &fakeRawIndex{}
	}
	return s.indexes[uid]
}

func (s *fakeRawService) CreateIndexWithContext(_ context.Context, cfg *meilisearch.IndexConfig) (*meilisearch.TaskInfo, error) {
	s.createConfigs = append(s.createConfigs, cfg)
	return &meilisearch.TaskInfo{TaskUID: 1}, nil
}

func (s *fakeRawService) DeleteIndexWithContext(_ context.Context, _ string) (*meilisearch.TaskInfo, error) {
	return &meilisearch.TaskInfo{TaskUID: 2}, nil
}

func (s *fakeRawService) GetIndexWithContext(_ context.Context, uid string) (*meilisearch.IndexResult, error) {
	if err := s.getIndexErr[uid]; err != nil {
		return nil, err
	}
	return &meilisearch.IndexResult{UID: uid}, nil
}

func (s *fakeRawService) WaitForTaskWithContext(_ context.Context, taskUID int64, _ time.Duration) (*meilisearch.Task, error) {
	s.waited = append(s.waited, taskUID)
	return &meilisearch.Task{TaskUID: taskUID, Status: meilisearch.TaskStatusSucceeded}, nil
}

func (s *fakeRawService) Close() {}

//nolint:govet // Test double layout is not performance-sensitive.
type fakeRawIndex struct {
	addDocs      interface{}
	searchQuery  string
	filterable   []interface{}
	deleteIDs    []string
	searchable   []string
	waited       []int64
	searchResp   *meilisearch.SearchResponse
	searchReq    *meilisearch.SearchRequest
	addBatchSize int
}

func (i *fakeRawIndex) AddDocumentsInBatchesWithContext(_ context.Context, documentsPtr interface{}, batchSize int, _ *meilisearch.DocumentOptions) ([]meilisearch.TaskInfo, error) {
	i.addBatchSize = batchSize
	i.addDocs = documentsPtr
	return []meilisearch.TaskInfo{{TaskUID: 11}, {TaskUID: 12}}, nil
}

func (i *fakeRawIndex) DeleteDocumentWithContext(_ context.Context, identifier string, _ *meilisearch.DocumentOptions) (*meilisearch.TaskInfo, error) {
	i.deleteIDs = append(i.deleteIDs, identifier)
	return &meilisearch.TaskInfo{TaskUID: 13}, nil
}

func (i *fakeRawIndex) UpdateDisplayedAttributesWithContext(_ context.Context, _ *[]string) (*meilisearch.TaskInfo, error) {
	return &meilisearch.TaskInfo{TaskUID: 0}, nil
}

func (i *fakeRawIndex) UpdateFilterableAttributesWithContext(_ context.Context, request *[]interface{}) (*meilisearch.TaskInfo, error) {
	i.filterable = append([]interface{}(nil), (*request)...)
	return &meilisearch.TaskInfo{TaskUID: 10}, nil
}

func (i *fakeRawIndex) UpdateSearchableAttributesWithContext(_ context.Context, request *[]string) (*meilisearch.TaskInfo, error) {
	i.searchable = append([]string(nil), (*request)...)
	return &meilisearch.TaskInfo{TaskUID: 9}, nil
}

func (i *fakeRawIndex) SearchWithContext(_ context.Context, query string, request *meilisearch.SearchRequest) (*meilisearch.SearchResponse, error) {
	i.searchQuery = query
	i.searchReq = request
	if i.searchResp == nil {
		i.searchResp = &meilisearch.SearchResponse{}
	}
	return i.searchResp, nil
}

func (i *fakeRawIndex) WaitForTaskWithContext(_ context.Context, taskUID int64, _ time.Duration) (*meilisearch.Task, error) {
	i.waited = append(i.waited, taskUID)
	return &meilisearch.Task{TaskUID: taskUID, Status: meilisearch.TaskStatusSucceeded}, nil
}

type fakeMetaResolver struct {
	mt  *meta.MetaType
	err error
}

func (r fakeMetaResolver) Get(context.Context, string, string) (*meta.MetaType, error) {
	return r.mt, r.err
}

func TestFilterableAttributesIncludeBaseAndCustomFields(t *testing.T) {
	mt := &meta.MetaType{
		Name: "Order",
		Fields: []meta.FieldDef{
			{Name: "status", Filterable: true},
			{Name: "priority", Filterable: true},
		},
	}

	got := filterableAttributes(mt)
	want := []string{"category", "doctype", "name", "priority", "status", "tenant_id"}
	if !slices.Equal(got, want) {
		t.Fatalf("filterableAttributes = %v, want %v", got, want)
	}
}

func TestIndexerIndexDocumentsUses250DocumentBatches(t *testing.T) {
	index := &fakeRawIndex{}
	svc := &fakeRawService{
		indexes:     map[string]*fakeRawIndex{IndexName("acme", "Order"): index},
		getIndexErr: map[string]error{IndexName("acme", "Order"): &meilisearch.Error{StatusCode: 404}},
	}
	client := &Client{svc: svc}
	indexer := NewIndexer(client)

	mt := &meta.MetaType{
		Name: "Order",
		Fields: []meta.FieldDef{
			{Name: "title", Searchable: true},
			{Name: "priority", Filterable: true},
		},
	}

	docs := make([]map[string]any, 251)
	for i := range docs {
		docs[i] = map[string]any{"name": "doc"}
	}

	if err := indexer.IndexDocuments(context.Background(), "acme", mt, docs); err != nil {
		t.Fatalf("IndexDocuments: %v", err)
	}

	if index.addBatchSize != defaultBatchSize {
		t.Fatalf("batch size = %d, want %d", index.addBatchSize, defaultBatchSize)
	}
	if got := svc.createConfigs[0].Uid; got != IndexName("acme", "Order") {
		t.Fatalf("created index UID = %q", got)
	}
	if !slices.Equal(index.searchable, []string{"name", "title"}) {
		t.Fatalf("searchable attrs = %v", index.searchable)
	}
	if len(index.filterable) == 0 {
		t.Fatal("filterable attrs not configured")
	}
	if !slices.Equal(svc.waited, []int64{1}) {
		t.Fatalf("service waited = %v, want [1]", svc.waited)
	}
	if !slices.Equal(index.waited, []int64{9, 10, 11, 12}) {
		t.Fatalf("index waited = %v, want [9 10 11 12]", index.waited)
	}
}

func TestQueryServiceSearchBuildsFiltersAndPagination(t *testing.T) {
	index := &fakeRawIndex{
		searchResp: &meilisearch.SearchResponse{
			Hits: meilisearch.Hits{
				mustHit(t, map[string]any{
					"name":      "ORD-1",
					"doctype":   "Order",
					"title":     "Alpha",
					"tenant_id": "acme",
				}),
			},
			EstimatedTotalHits: 7,
		},
	}
	client := &Client{svc: &fakeRawService{
		indexes: map[string]*fakeRawIndex{IndexName("acme", "Order"): index},
	}}
	service := NewQueryService(client)
	mt := &meta.MetaType{
		Name: "Order",
		Fields: []meta.FieldDef{
			{Name: "title", Searchable: true},
			{Name: "priority", Filterable: true},
		},
	}

	results, total, err := service.Search(context.Background(), "acme", mt, "alpha", []orm.Filter{
		{Field: "status", Operator: orm.OpEqual, Value: "Draft"},
		{Field: "priority", Operator: orm.OpBetween, Value: []any{1, 3}},
		{Field: "tenant_id", Operator: orm.OpIsNotNull},
	}, 2, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if total != 7 {
		t.Fatalf("total = %d, want 7", total)
	}
	if len(results) != 1 || results[0].Name != "ORD-1" {
		t.Fatalf("results = %#v", results)
	}
	if index.searchQuery != "alpha" {
		t.Fatalf("query = %q, want alpha", index.searchQuery)
	}
	if index.searchReq.Offset != 10 || index.searchReq.Limit != 10 {
		t.Fatalf("offset/limit = %d/%d, want 10/10", index.searchReq.Offset, index.searchReq.Limit)
	}
	wantFilters := []string{
		`status = "Draft"`,
		`priority 1 TO 3`,
		`tenant_id IS NOT NULL`,
	}
	gotFilters, ok := index.searchReq.Filter.([]string)
	if !ok || !slices.Equal(gotFilters, wantFilters) {
		t.Fatalf("filters = %#v, want %#v", index.searchReq.Filter, wantFilters)
	}
}

func TestQueryServiceRejectsUnsupportedOperators(t *testing.T) {
	client := &Client{svc: &fakeRawService{indexes: map[string]*fakeRawIndex{IndexName("acme", "Order"): {}}}}
	service := NewQueryService(client)
	mt := &meta.MetaType{
		Name:   "Order",
		Fields: []meta.FieldDef{{Name: "title", Searchable: true}},
	}

	_, _, err := service.Search(context.Background(), "acme", mt, "alpha", []orm.Filter{
		{Field: "status", Operator: orm.OpLike, Value: "%draft%"},
	}, 1, 20)
	var filterErr *FilterError
	if !errors.As(err, &filterErr) {
		t.Fatalf("expected FilterError, got %v", err)
	}
}

func TestSyncerJobHandlerIndexesAndDeletesDocuments(t *testing.T) {
	index := &fakeRawIndex{}
	client := &Client{svc: &fakeRawService{
		indexes: map[string]*fakeRawIndex{IndexName("acme", "Order"): index},
	}}
	syncer := NewSyncer(client, fakeMetaResolver{mt: &meta.MetaType{
		Name:   "Order",
		Fields: []meta.FieldDef{{Name: "title", Searchable: true}},
	}}, config.KafkaConfig{}, nil)

	job := queue.Job{
		Payload: map[string]any{
			"event": map[string]any{
				"event_type": "doc.updated",
				"site":       "acme",
				"doctype":    "Order",
				"docname":    "ORD-1",
				"data": map[string]any{
					"name":  "ORD-1",
					"title": "Alpha",
				},
			},
		},
	}

	if err := syncer.JobHandler(context.Background(), job); err != nil {
		t.Fatalf("JobHandler: %v", err)
	}

	docs, ok := index.addDocs.([]map[string]any)
	if !ok || len(docs) != 1 {
		t.Fatalf("indexed docs = %#v", index.addDocs)
	}
	if docs[0]["tenant_id"] != "acme" || docs[0]["doctype"] != "Order" {
		t.Fatalf("indexed doc normalization = %#v", docs[0])
	}

	if err := syncer.HandleEvent(context.Background(), events.DocumentEvent{
		EventType: events.EventTypeDocDeleted,
		Site:      "acme",
		DocType:   "Order",
		DocName:   "ORD-1",
	}); err != nil {
		t.Fatalf("HandleEvent(delete): %v", err)
	}
	if !slices.Equal(index.deleteIDs, []string{"ORD-1"}) {
		t.Fatalf("deleted IDs = %v, want [ORD-1]", index.deleteIDs)
	}
}

func mustHit(t *testing.T, values map[string]any) meilisearch.Hit {
	t.Helper()
	hit := make(meilisearch.Hit, len(values))
	for key, value := range values {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal hit value %q: %v", key, err)
		}
		hit[key] = raw
	}
	return hit
}

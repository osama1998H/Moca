//go:build integration

package search_test

import (
	"context"
	"testing"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/pkg/events"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/orm"
	"github.com/osama1998H/moca/pkg/search"
)

func TestIntegration_IndexQueryAndRemoveDocument(t *testing.T) {
	ctx := context.Background()

	client, err := search.NewClient(config.SearchConfig{
		Engine: "meilisearch",
		Host:   "http://localhost",
		Port:   7700,
		APIKey: "moca_test",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(client.Close)

	indexer := search.NewIndexer(client)
	query := search.NewQueryService(client)

	mt := &meta.MetaType{
		Name: "SearchIntArticle",
		Fields: []meta.FieldDef{
			{Name: "title", Searchable: true, InAPI: true},
			{Name: "status", Filterable: true, InAPI: true},
		},
	}

	site := "searchint"
	t.Cleanup(func() {
		_ = indexer.DeleteIndex(ctx, site, mt.Name)
	})
	_ = indexer.DeleteIndex(ctx, site, mt.Name)

	if err := indexer.IndexDocuments(ctx, site, mt, []map[string]any{
		{"name": "ART-1", "title": "hello search", "status": "Published"},
	}); err != nil {
		t.Fatalf("IndexDocuments: %v", err)
	}

	results, total, err := query.Search(ctx, site, mt, "hello", []orm.Filter{
		{Field: "status", Operator: orm.OpEqual, Value: "Published"},
	}, 1, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total < 1 || len(results) == 0 {
		t.Fatalf("expected indexed document, total=%d len=%d", total, len(results))
	}
	if results[0].Name != "ART-1" {
		t.Fatalf("result name = %q, want %q", results[0].Name, "ART-1")
	}

	if err := indexer.RemoveDocument(ctx, site, mt.Name, "ART-1"); err != nil {
		t.Fatalf("RemoveDocument: %v", err)
	}

	results, total, err = query.Search(ctx, site, mt, "hello", []orm.Filter{
		{Field: "status", Operator: orm.OpEqual, Value: "Published"},
	}, 1, 10)
	if err != nil {
		t.Fatalf("Search after delete: %v", err)
	}
	if total != 0 || len(results) != 0 {
		t.Fatalf("expected no results after delete, total=%d len=%d", total, len(results))
	}
}

func TestIntegration_SearchSyncPipeline(t *testing.T) {
	ctx := context.Background()

	client, err := search.NewClient(config.SearchConfig{
		Engine: "meilisearch",
		Host:   "http://localhost",
		Port:   7700,
		APIKey: "moca_test",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(client.Close)

	mt := &meta.MetaType{
		Name: "SyncPipelineDoc",
		Fields: []meta.FieldDef{
			{Name: "title", Searchable: true, InAPI: true},
			{Name: "status", Filterable: true, InAPI: true},
		},
	}

	site := "syncpipeline"
	indexer := search.NewIndexer(client)
	query := search.NewQueryService(client)

	// Clean up any leftover index.
	t.Cleanup(func() {
		_ = indexer.DeleteIndex(ctx, site, mt.Name)
	})
	_ = indexer.DeleteIndex(ctx, site, mt.Name)

	// Simulate the search sync pipeline: an event triggers indexing.
	syncer := search.NewSyncer(client, &staticMetaResolver{mt: mt}, config.KafkaConfig{}, nil)

	// Simulate doc.created event → HandleEvent → index the document.
	event := events.DocumentEvent{
		EventType: events.EventTypeDocCreated,
		Site:      site,
		DocType:   mt.Name,
		DocName:   "SYNC-001",
		Data:      map[string]any{"title": "pipeline test doc", "status": "Draft"},
	}
	if err := events.EnsureDocumentEventDefaults(&event); err != nil {
		t.Fatalf("EnsureDefaults: %v", err)
	}
	if err := syncer.HandleEvent(ctx, event); err != nil {
		t.Fatalf("HandleEvent (create): %v", err)
	}

	// Search for the indexed document.
	results, total, err := query.Search(ctx, site, mt, "pipeline", nil, 1, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total < 1 || len(results) == 0 {
		t.Fatalf("expected at least one result, total=%d len=%d", total, len(results))
	}
	if results[0].Name != "SYNC-001" {
		t.Errorf("result name = %q, want %q", results[0].Name, "SYNC-001")
	}

	// Simulate doc.deleted event → HandleEvent → remove from index.
	deleteEvent := events.DocumentEvent{
		EventType: events.EventTypeDocDeleted,
		Site:      site,
		DocType:   mt.Name,
		DocName:   "SYNC-001",
	}
	if err := events.EnsureDocumentEventDefaults(&deleteEvent); err != nil {
		t.Fatalf("EnsureDefaults: %v", err)
	}
	if err := syncer.HandleEvent(ctx, deleteEvent); err != nil {
		t.Fatalf("HandleEvent (delete): %v", err)
	}

	// Verify deleted from search.
	results, total, err = query.Search(ctx, site, mt, "pipeline", nil, 1, 10)
	if err != nil {
		t.Fatalf("Search after delete: %v", err)
	}
	if total != 0 || len(results) != 0 {
		t.Fatalf("expected no results after delete, total=%d len=%d", total, len(results))
	}
}

// staticMetaResolver returns a fixed MetaType for any site/doctype query.
type staticMetaResolver struct {
	mt *meta.MetaType
}

func (r *staticMetaResolver) Get(_ context.Context, _, doctype string) (*meta.MetaType, error) {
	if doctype == r.mt.Name {
		return r.mt, nil
	}
	return nil, meta.ErrMetaTypeNotFound
}

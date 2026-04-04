//go:build integration

package search_test

import (
	"context"
	"testing"

	"github.com/osama1998H/moca/internal/config"
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

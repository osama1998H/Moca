package search

import (
	"context"
	"fmt"
	"slices"

	"github.com/meilisearch/meilisearch-go"

	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/tenancy"
)

const (
	defaultBatchSize = 250
)

var baseFilterableAttributes = []string{
	"name",
	"doctype",
	"tenant_id",
	"status",
	"category",
}

// IndexName returns the Meilisearch index UID for a site+doctype pair.
func IndexName(site, doctype string) string {
	return (&tenancy.SiteContext{Name: site}).PrefixSearchIndex(doctype)
}

// Indexer manages Meilisearch indexes for MOCA doctypes.
type Indexer struct {
	client *Client
}

func NewIndexer(client *Client) *Indexer {
	return &Indexer{client: client}
}

func (i *Indexer) EnsureIndex(ctx context.Context, site string, mt *meta.MetaType) error {
	if i == nil || !i.client.available() {
		return ErrUnavailable
	}

	indexUID := IndexName(site, mt.Name)
	if _, err := i.client.svc.GetIndexWithContext(ctx, indexUID); err != nil {
		if !isSearchNotFound(err) {
			return fmt.Errorf("get index %q: %w", indexUID, err)
		}
		task, err := i.client.svc.CreateIndexWithContext(ctx, &meilisearch.IndexConfig{
			Uid:        indexUID,
			PrimaryKey: "name",
		})
		if err != nil && !isSearchConflict(err) {
			return fmt.Errorf("create index %q: %w", indexUID, err)
		}
		if err == nil {
			if err := i.client.waitForTask(ctx, task.TaskUID); err != nil {
				return fmt.Errorf("create index %q task: %w", indexUID, err)
			}
		}
	}

	index := i.client.svc.Index(indexUID)

	searchable := searchableAttributes(mt)
	searchTask, err := index.UpdateSearchableAttributesWithContext(ctx, &searchable)
	if err != nil {
		return fmt.Errorf("update searchable attributes for %q: %w", indexUID, err)
	}
	waitErr := i.client.waitForIndexTask(ctx, index, searchTask.TaskUID)
	if waitErr != nil {
		return fmt.Errorf("wait searchable attributes task for %q: %w", indexUID, waitErr)
	}

	filterable := interfaceSlice(filterableAttributes(mt))
	filterTask, err := index.UpdateFilterableAttributesWithContext(ctx, &filterable)
	if err != nil {
		return fmt.Errorf("update filterable attributes for %q: %w", indexUID, err)
	}
	waitErr = i.client.waitForIndexTask(ctx, index, filterTask.TaskUID)
	if waitErr != nil {
		return fmt.Errorf("wait filterable attributes task for %q: %w", indexUID, waitErr)
	}

	return nil
}

func (i *Indexer) DeleteIndex(ctx context.Context, site, doctype string) error {
	if i == nil || !i.client.available() {
		return ErrUnavailable
	}

	indexUID := IndexName(site, doctype)
	task, err := i.client.svc.DeleteIndexWithContext(ctx, indexUID)
	if err != nil {
		if isSearchNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete index %q: %w", indexUID, err)
	}
	if err := i.client.waitForTask(ctx, task.TaskUID); err != nil {
		return fmt.Errorf("wait delete index task for %q: %w", indexUID, err)
	}
	return nil
}

func (i *Indexer) IndexDocuments(ctx context.Context, site string, mt *meta.MetaType, docs []map[string]any) error {
	if i == nil || !i.client.available() {
		return ErrUnavailable
	}
	if len(docs) == 0 {
		return nil
	}
	if err := i.EnsureIndex(ctx, site, mt); err != nil {
		return err
	}

	indexUID := IndexName(site, mt.Name)
	index := i.client.svc.Index(indexUID)
	taskInfos, err := index.AddDocumentsInBatchesWithContext(ctx, docs, defaultBatchSize, nil)
	if err != nil {
		return fmt.Errorf("index documents into %q: %w", indexUID, err)
	}
	for _, task := range taskInfos {
		if err := i.client.waitForIndexTask(ctx, index, task.TaskUID); err != nil {
			return fmt.Errorf("wait indexing task for %q: %w", indexUID, err)
		}
	}
	return nil
}

func (i *Indexer) RemoveDocument(ctx context.Context, site, doctype, docName string) error {
	if i == nil || !i.client.available() {
		return ErrUnavailable
	}

	indexUID := IndexName(site, doctype)
	if _, err := i.client.svc.GetIndexWithContext(ctx, indexUID); err != nil {
		if isSearchNotFound(err) {
			return nil
		}
		return fmt.Errorf("get index %q: %w", indexUID, err)
	}

	index := i.client.svc.Index(indexUID)
	task, err := index.DeleteDocumentWithContext(ctx, docName, nil)
	if err != nil {
		return fmt.Errorf("delete document %q from %q: %w", docName, indexUID, err)
	}
	if err := i.client.waitForIndexTask(ctx, index, task.TaskUID); err != nil {
		return fmt.Errorf("wait delete document task for %q: %w", indexUID, err)
	}
	return nil
}

func hasSearchableFields(mt *meta.MetaType) bool {
	for _, field := range mt.Fields {
		if field.Searchable {
			return true
		}
	}
	return false
}

func searchableAttributes(mt *meta.MetaType) []string {
	attrs := []string{"name"}
	for _, field := range mt.Fields {
		if field.Searchable {
			attrs = append(attrs, field.Name)
		}
	}
	return dedupeStrings(attrs)
}

func filterableAttributes(mt *meta.MetaType) []string {
	attrs := append([]string(nil), baseFilterableAttributes...)
	for _, field := range mt.Fields {
		if field.Filterable {
			attrs = append(attrs, field.Name)
		}
	}
	return dedupeStrings(attrs)
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func interfaceSlice(values []string) []interface{} {
	result := make([]interface{}, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

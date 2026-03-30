// Package search implements full-text search for MOCA using Meilisearch.
//
// Each MetaType that declares searchable fields gets a corresponding
// Meilisearch index. Documents are indexed asynchronously via Kafka
// consumer or direct push. Search queries are proxied through this
// package with per-tenant index scoping.
//
// Key components:
//   - Indexer: Meilisearch index management (create, configure, delete)
//   - Sync: Kafka-to-Meilisearch document sync consumer
//   - Query: search query builder and result mapping
package search

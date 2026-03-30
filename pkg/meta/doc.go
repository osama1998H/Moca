// Package meta implements the MetaType registry, schema compiler, and migrator.
//
// MetaType is the central abstraction of the MOCA framework. A single MetaType
// definition drives database schema generation, CRUD API routes, GraphQL schema,
// Meilisearch index configuration, and React form/list views.
//
// Key components:
//   - Registry: in-memory + Redis-backed metadata registry for MetaType lookups
//   - Compiler: validates and compiles raw JSON definitions to typed MetaType structs
//   - Migrator: diffs current MetaType schema against live DB schema and emits ALTER TABLE DDL
package meta

// Package orm implements the MOCA database adapter layer for PostgreSQL.
//
// MOCA uses PostgreSQL 16+ with schema-per-tenant isolation. Each tenant
// gets its own PostgreSQL schema (e.g. tenant_acme); the moca_system schema
// holds global tables. Every document table includes a _extra JSONB column
// for dynamic/custom fields that avoids schema migrations for customizations.
//
// Key components:
//   - Postgres: pgxpool connection management with BeforeAcquire search_path reset
//   - Query: dynamic query builder driven by MetaType field definitions
//   - Transaction: transaction helpers with context propagation
//   - Schema: DDL generation from MetaType definitions (CREATE TABLE, ALTER TABLE)
//   - Migrate: schema migration orchestration
package orm

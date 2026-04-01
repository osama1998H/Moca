// Package backup provides per-tenant PostgreSQL backup and restore operations
// for the Moca framework.
//
// It wraps pg_dump and psql to create and restore compressed SQL dumps
// scoped to individual tenant schemas. Backups are stored as gzip-compressed
// SQL files in the project's sites/{site}/backups/ directory.
//
// Key operations:
//   - Create: pg_dump --schema=tenant_{site} producing timestamped .sql.gz files
//   - Restore: drop + recreate schema, pipe .sql.gz through psql
//   - List: scan backup directory for existing backups with metadata
//   - Verify: checksum validation and SQL syntax checking
//
// All functions require pg_dump and psql to be available on PATH.
// Use CheckDependencies to verify before calling other functions.
package backup

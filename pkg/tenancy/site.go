package tenancy

import "github.com/jackc/pgx/v5/pgxpool"

// SiteContext holds the identity and database pool for a single tenant site.
// It is carried through the document lifecycle via DocContext.
type SiteContext struct {
	// Pool is the pgxpool bound to this site's PostgreSQL schema.
	// May be nil for tests or when no DB access is needed.
	Pool *pgxpool.Pool
	// Name is the site identifier (e.g. "acme"), used to resolve the tenant schema.
	Name string
}

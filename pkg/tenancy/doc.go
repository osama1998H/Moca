// Package tenancy implements MOCA's multitenancy layer.
//
// Each MOCA deployment manages multiple sites (tenants). Every HTTP request
// is resolved to a specific site via hostname matching, and database connections
// are scoped to that site's PostgreSQL schema via search_path.
//
// Key components:
//   - Resolver: HTTP middleware that identifies the current site from the request
//   - Context: SiteContext propagated through the request context
//   - Manager: site lifecycle management (create, delete, migrate, list)
//   - Config: per-site configuration loading and merging
package tenancy

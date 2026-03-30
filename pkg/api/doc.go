// Package api implements the MOCA API gateway layer.
//
// All external HTTP traffic enters through this package. It provides
// auto-generated REST endpoints and GraphQL schema derived from MetaType
// definitions, plus cross-cutting concerns like rate limiting, API versioning,
// request/response transformation, API key management, and outbound webhooks.
//
// Key components:
//   - Gateway: main router and middleware chain
//   - REST: auto-generated CRUD endpoints per MetaType
//   - GraphQL: auto-generated queries and mutations per MetaType
//   - Versioning: API version negotiation and routing
//   - RateLimit: per-tenant and per-key rate limiting
//   - Webhook: outbound webhook dispatch and retry
package api

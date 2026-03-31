# MS-06 — REST API Layer Plan

## Milestone Summary
- **ID:** MS-06
- **Name:** REST API Layer — Auto-Generated CRUD, Middleware, Rate Limiting
- **Roadmap Reference:** `ROADMAP.md` lines 421–463
- **Goal:** Implement auto-generated REST API for any MetaType, middleware chain, request/response transformers, API versioning, and rate limiting. First externally-usable surface of the framework.
- **Why it matters:** After this milestone, a developer defines a MetaType JSON and the framework generates a full REST API. This unblocks the React frontend (MS-17) and is the gateway for all downstream milestones — 10+ milestones depend on MS-06.
- **Position in roadmap:** Critical path — MS-00 → MS-01 → MS-02 → MS-03 → MS-04 → **MS-06** → MS-12 → MS-15 → MS-23 → MS-25 → MS-26 → v1.0
- **Upstream dependencies:** MS-04 (Document Runtime — fully implemented), MS-05 (Query Engine — fully implemented)
- **Downstream dependencies:** MS-10, MS-12, MS-14, MS-15, MS-17, MS-20, MS-24, MS-27

## Vision Alignment

MS-06 transforms Moca from a Go library into a runnable HTTP server. It fulfills the core framework promise: define a MetaType and get a full REST API automatically. This is the "first externally-usable surface" — everything before MS-06 was infrastructure; everything after builds on the API layer. The design follows the Frappe model (auto-generated `/api/resource/{doctype}` endpoints) but adds what Frappe lacks: field-level transformers, API versioning, per-doctype rate limiting, and pluggable middleware.

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| `ROADMAP.md` | MS-06 | 421–463 | Milestone spec, deliverables, acceptance criteria |
| `MOCA_SYSTEM_DESIGN.md` | §3.3 Customizable API Layer | 466–679 | Full API architecture: APIConfig, Transformer interface, pipeline |
| `MOCA_SYSTEM_DESIGN.md` | §14 Complete Request Lifecycle | 1884–1920 | 14-step request lifecycle from TCP to response |
| `pkg/meta/stubs.go` | APIConfig, RateLimitConfig | 99–175 | Existing stub types to populate |
| `pkg/document/crud.go` | DocManager | 371–1311 | CRUD entry points the handlers will call |
| `pkg/document/context.go` | DocContext | all | Request context the handlers will construct |
| `pkg/observe/health.go` | HealthChecker | all | Existing stdlib routing pattern to follow |
| `internal/drivers/redis.go` | RedisClients | all | Redis infrastructure for rate limiting |

## Research Notes

No web research was needed. The design documents are comprehensive and the implementation patterns are well-established in the existing codebase. Key findings from codebase exploration:

1. **Audit log is already handled** — `DocManager.Insert/Update/Delete` already writes to `tab_audit_log` inside the same transaction (`pkg/document/crud.go:469-478`). MS-06 does NOT need to implement audit logging; it only needs integration tests that verify audit entries appear after HTTP mutations.

2. **No external HTTP framework** — The project uses Go 1.26+ stdlib `http.ServeMux` with method+path routing (e.g., `mux.HandleFunc("GET /health", handler)` in `pkg/observe/health.go`). MS-06 should continue this pattern.

3. **RateLimitConfig is an empty stub** — `pkg/meta/stubs.go:103` defines `type RateLimitConfig struct{}`. MS-06 must populate it with `Window`, `MaxRequests`, `BurstSize`.

4. **APIConfig already has AllowGet/AllowCreate/etc. bools** — The struct exists at `pkg/meta/stubs.go:109-130` with all endpoint control fields. No new fields needed for basic CRUD gating.

5. **FieldDef already has InAPI, APIAlias, APIReadOnly** — `pkg/meta/fielddef.go:138-140`. The transformer layer reads these to shape responses.

6. **DocContext requires Site, User, TX, EventBus** — The API layer must construct a `DocContext` from the HTTP request by resolving tenant and user via middleware.

---

## Milestone Plan

### Task 1

- **Task ID:** MS-06-T1
- **Title:** Gateway, Middleware Chain, Rate Limiter, Auth Interfaces
- **Description:** Build the core HTTP gateway (`pkg/api/gateway.go`) that owns the `http.ServeMux`, the middleware chain, and placeholder auth/permission interfaces. The middleware chain executes: Request ID → CORS → Tenant Resolution → Rate Limit → Auth → Handler. Also implement the Redis sliding window rate limiter and populate `RateLimitConfig` fields.
- **Why this task exists:** Everything else depends on the gateway and middleware. The auth/permission interfaces must be defined first so CRUD handlers (T2) and MS-14 can code against them. Rate limiting is a middleware concern and belongs here.
- **Dependencies:** None — this is the foundation.
- **Inputs / References:**
  - `pkg/observe/health.go` — existing `RegisterRoutes(mux)` pattern
  - `pkg/observe/logging.go` — `WithRequest()`, `ContextWithLogger()`
  - `pkg/tenancy/site.go` — `SiteContext` for tenant resolution
  - `pkg/auth/user.go` — `User` struct
  - `internal/drivers/redis.go` — `RedisClients.Cache` for rate limiter
  - `pkg/meta/stubs.go:99-103` — `RateLimitConfig` to populate
  - `MOCA_SYSTEM_DESIGN.md` §3.3 and §14
- **Deliverable:**
  - `pkg/api/gateway.go` — `Gateway` struct (holds ServeMux, DocManager, Registry, RedisClients, Authenticator, PermissionChecker, logger). `NewGateway(...)` constructor. `Handler() http.Handler` returning composed middleware chain.
  - `pkg/api/middleware.go` — `requestIDMiddleware`, `corsMiddleware`, `tenantMiddleware` (resolves site from `X-Moca-Site` header or subdomain)
  - `pkg/api/auth.go` — `Authenticator` interface (`Authenticate(*http.Request) (*auth.User, error)`), `PermissionChecker` interface (`CheckDocPerm(ctx, user, doctype, perm) error`), `NoopAuthenticator` (returns guest user), `AllowAllPermissionChecker`
  - `pkg/api/ratelimit.go` — `RateLimiter` struct using Redis sorted sets (ZADD/ZREMRANGEBYSCORE/ZCARD sliding window). `Allow(ctx, key, config) (allowed bool, retryAfter time.Duration, err error)`. `rateLimitMiddleware` wrapping it.
  - `pkg/api/context.go` — `APIContext` type, context key helpers (`UserFromContext`, `SiteFromContext`, `RequestIDFromContext`)
  - Update `pkg/meta/stubs.go` — populate `RateLimitConfig` with `Window time.Duration`, `MaxRequests int`, `BurstSize int`
  - Unit tests for each middleware and the rate limiter
- **Risks / Unknowns:**
  - Tenant resolution: subdomain vs `X-Moca-Site` header. Recommend supporting both; header for dev, subdomain for production.
  - CORS configuration must be flexible for MS-17 React frontend but locked down by default.

---

### Task 2

- **Task ID:** MS-06-T2
- **Title:** REST CRUD Handlers, Query Parameter Parsing, Response Envelope
- **Description:** Implement `pkg/api/rest.go` with auto-generated REST handlers that bridge HTTP requests to `DocManager`. Includes query-string parsing for filters/pagination (Frappe-style `[["field","op","value"]]` JSON), a standard response envelope, error mapping, and the `/api/v1/meta/{doctype}` introspection endpoint.
- **Why this task exists:** This is the core value proposition — define a MetaType and get REST endpoints. The handlers translate HTTP verbs and query params into `DocManager` calls and `ListOptions`.
- **Dependencies:** T1 (Gateway, middleware, context helpers)
- **Inputs / References:**
  - `pkg/document/crud.go:580+` — `DocManager.Insert/Update/Delete/Get/GetList` signatures
  - `pkg/document/crud.go:64-98` — `ListOptions` struct
  - `pkg/orm/query.go:28-46` — `Operator` constants, `Filter` struct
  - `pkg/meta/stubs.go:109-130` — `APIConfig.AllowGet`, etc.
  - `pkg/meta/metatype.go` — `MetaType` for meta endpoint
  - `pkg/meta/fielddef.go:119-150` — `FieldDef.InAPI`, `APIAlias`, `APIReadOnly`
- **Deliverable:**
  - `pkg/api/rest.go` — `ResourceHandler` struct. Methods:
    - `handleList` — `GET /api/{version}/resource/{doctype}` — parses `filters`, `fields`, `order_by`, `limit`, `offset` from query string; calls `DocManager.GetList`; returns JSON array with pagination metadata
    - `handleGet` — `GET /api/{version}/resource/{doctype}/{name}` — calls `DocManager.Get`; returns 200
    - `handleCreate` — `POST /api/{version}/resource/{doctype}` — decodes JSON body; calls `DocManager.Insert`; returns 201
    - `handleUpdate` — `PUT /api/{version}/resource/{doctype}/{name}` — decodes JSON body; calls `DocManager.Update`; returns 200
    - `handleDelete` — `DELETE /api/{version}/resource/{doctype}/{name}` — calls `DocManager.Delete`; returns 204
    - `handleMeta` — `GET /api/{version}/meta/{doctype}` — returns MetaType JSON (API-safe fields only)
    - Each handler checks `APIConfig.AllowX` (405 if disabled) and calls `PermissionChecker.CheckDocPerm` (403 if denied)
  - `pkg/api/params.go` — `parseFilters(raw string) ([]orm.Filter, error)` parses `[["status","=","Draft"]]`. `parseListParams(r, apiCfg) (ListOptions, error)` extracts limit (capped by MaxPageSize), offset, order_by, fields, filters.
  - `pkg/api/response.go` — Standard response envelope:
    - `writeSuccess(w, status, data)` → `{"data": ...}`
    - `writeListSuccess(w, data, total, limit, offset)` → `{"data": [...], "meta": {"total": N, "limit": N, "offset": N}}`
    - `writeError(w, status, code, message)` → `{"error": {"code": "...", "message": "..."}}`
    - Error mapping: `DocNotFoundError` → 404, validation errors → 422, permission errors → 403
  - Route registration: `ResourceHandler.RegisterRoutes(mux, version)` using Go 1.26 patterns
  - Unit tests for filter parsing, parameter extraction, error mapping, handler logic with mocked DocManager
- **Risks / Unknowns:**
  - Go 1.26 `{doctype}` wildcard: verify it captures names with hyphens/dots correctly.
  - Frappe-style `filters` JSON syntax must handle malformed input gracefully (400 error).

---

### Task 3

- **Task ID:** MS-06-T3
- **Title:** Transformer Pipeline and API Version Router
- **Description:** Implement the `Transformer` interface and built-in transformers (field filtering, alias remapping, read-only enforcement). Implement the version router that dispatches to version-specific transformer chains. Each API version has its own transformer chain derived from `APIVersion.FieldMapping` and `APIVersion.ExcludeFields`.
- **Why this task exists:** Transformers ensure no internal fields leak, aliases work, and read-only fields can't be written. Versioning enables backwards-compatible API evolution. These are tightly coupled — each version has its own transformer chain.
- **Dependencies:** T1 (Gateway), T2 (CRUD handlers call transformers)
- **Inputs / References:**
  - `pkg/meta/fielddef.go:138-140` — `InAPI`, `APIAlias`, `APIReadOnly`
  - `pkg/meta/stubs.go:109-130` — `APIConfig.ExcludeFields`, `DefaultFields`, `AlwaysInclude`, `ComputedFields`
  - `pkg/meta/stubs.go:136-143` — `APIVersion.FieldMapping`, `ExcludeFields`, `AddedFields`
  - `MOCA_SYSTEM_DESIGN.md` §3.3 — Transformer interface, built-in transformers list
- **Deliverable:**
  - `pkg/api/transformer.go` — `Transformer` interface with `TransformRequest(ctx, mt, body) (map[string]any, error)` and `TransformResponse(ctx, mt, body) (map[string]any, error)`. `TransformerChain` (ordered slice). Factory: `NewTransformerChain(mt, version)`.
  - Built-in transformers:
    - `FieldFilter` — removes `InAPI==false` fields and `ExcludeFields` from response; applies `DefaultFields` on list responses; always includes `AlwaysInclude` fields
    - `AliasRemapper` — bidirectional mapping via `FieldDef.APIAlias` (request: alias→internal, response: internal→alias)
    - `ReadOnlyEnforcer` — strips `APIReadOnly`/`ReadOnly` fields from update requests (silent strip by default)
  - `pkg/api/version.go` — `VersionRouter` routing `/api/v1/`, `/api/v2/` to version-specific sub-muxes with per-version transformer chains. Adds `Deprecation` and `Sunset` headers when `APIVersion.Status == "deprecated"`. Default: single `v1` with base transformer chain when no versions configured.
  - Unit tests for each transformer (alias round-trip, field exclusion, read-only enforcement) and version routing
- **Risks / Unknowns:**
  - Copy-on-write transformers (same handlers, different chains per version) is simpler but limits v2 handler logic changes. Sufficient for MS-06.
  - `ComputedFields` require expression evaluation. For MS-06, only support registered Go functions (not arbitrary expressions).

---

### Task 4

- **Task ID:** MS-06-T4
- **Title:** moca-server HTTP Startup and Full-Stack Integration Tests
- **Description:** Wire the Gateway into `cmd/moca-server/main.go` so the binary starts an HTTP server with graceful shutdown. Write comprehensive integration tests exercising the full stack: HTTP request → middleware → handler → DocManager → PostgreSQL → response.
- **Why this task exists:** Transforms the framework from a library into a runnable server. Integration tests are the acceptance criteria proof — they verify the entire request lifecycle.
- **Dependencies:** T1, T2, T3 (all prior tasks)
- **Inputs / References:**
  - `cmd/moca-server/main.go` — existing scaffold to extend
  - `pkg/document/integration_test.go:67-79` — `queryAuditLog` helper pattern
  - `pkg/document/naming_integration_test.go` — TestMain infrastructure pattern
  - `docker-compose.yml` — PG on 5433, Redis on 6380
  - `internal/config/types.go` — config structures
  - `pkg/observe/health.go` — `HealthChecker.RegisterRoutes` to integrate
- **Deliverable:**
  - Updated `cmd/moca-server/main.go`:
    - Initialize DBManager, RedisClients, Registry, DocManager
    - Construct Gateway with all dependencies
    - Register health routes via `HealthChecker.RegisterRoutes`
    - Start `http.Server` with graceful shutdown (os.Signal, `server.Shutdown` with 30s timeout)
    - Read port from config (default 8000)
    - Startup banner with listen address
  - `pkg/api/api_integration_test.go` (`//go:build integration`):
    - TestMain: set up PG, Redis, register test MetaType ("TestItem" with varied fields including `InAPI: false`, `APIAlias`, `APIReadOnly`), create DocManager, create Gateway, start `httptest.Server`
    - Test cases:
      - `TestCreateDocument` — POST valid JSON → 201 with name
      - `TestGetDocument` — create then GET → 200 with matching fields
      - `TestListDocuments` — create multiple, GET with `filters=[["status","=","Draft"]]&limit=10` → paginated response
      - `TestUpdateDocument` — create then PUT → 200 with updated fields
      - `TestDeleteDocument` — create then DELETE → 204, then GET → 404
      - `TestFieldExclusion` — GET response omits `InAPI: false` fields
      - `TestAliasMapping` — POST/GET with alias field names
      - `TestReadOnlyEnforcement` — PUT with read-only field → stripped or 422
      - `TestRateLimiting` — exceed limit → 429 with `Retry-After`
      - `TestAuditLog` — POST+PUT+DELETE → query `tab_audit_log` directly, assert entries
      - `TestMethodNotAllowed` — `AllowDelete: false` + DELETE → 405
      - `TestMetaEndpoint` — GET `/api/v1/meta/TestItem` → MetaType JSON
      - `TestNotFound` — GET nonexistent doctype → 404
  - Makefile: add `test-api-integration` target running `go test -tags integration ./pkg/api/...`
- **Risks / Unknowns:**
  - Integration test must create `tab_audit_log` table in test schema. Existing `EnsureMetaTables` should handle this — verify.
  - Graceful shutdown must drain in-flight requests (30s timeout).

---

## Recommended Execution Order

1. **MS-06-T1** — Gateway + Middleware + Rate Limiter + Auth Interfaces (foundation)
2. **MS-06-T2** — CRUD Handlers + Meta Endpoint + Query Parsing (depends on T1)
3. **MS-06-T3** — Transformers + Versioning (depends on T1; can partially parallel T2 if Transformer interface defined early in T1)
4. **MS-06-T4** — Server Wiring + Integration Tests (depends on T1+T2+T3)

## Files Created/Modified

| File | Task | Action |
|------|------|--------|
| `pkg/api/gateway.go` | T1 | Create |
| `pkg/api/middleware.go` | T1 | Create |
| `pkg/api/auth.go` | T1 | Create |
| `pkg/api/ratelimit.go` | T1 | Create |
| `pkg/api/context.go` | T1 | Create |
| `pkg/meta/stubs.go` | T1 | Modify (populate `RateLimitConfig`) |
| `pkg/api/rest.go` | T2 | Create |
| `pkg/api/params.go` | T2 | Create |
| `pkg/api/response.go` | T2 | Create |
| `pkg/api/transformer.go` | T3 | Create |
| `pkg/api/version.go` | T3 | Create |
| `cmd/moca-server/main.go` | T4 | Modify (add HTTP server) |
| `pkg/api/api_integration_test.go` | T4 | Create |
| `Makefile` | T4 | Modify (add test target) |

## Open Questions

1. **Tenant resolution strategy**: Should the default be `X-Moca-Site` header, subdomain-based, or both? Recommend: support both, configurable via Gateway options. Header for development, subdomain for production.
2. **Read-only field enforcement behavior**: Should writing a read-only field silently strip it or return 422? Recommend: strip silently by default, configurable per MetaType.
3. **Default API behavior for new MetaTypes**: Should `APIConfig.Enabled` default to `true` or `false`? If `true`, every MetaType gets an API automatically (Frappe model). If `false`, explicit opt-in. Recommend: `true` (matching Frappe conventions and the "define MetaType, get API" promise).

## Out of Scope for This Milestone

- **GraphQL** — MS-20
- **API Keys** — MS-18
- **OAuth2 / SSO** — MS-22
- **Webhook dispatch** — MS-15 (config struct exists but dispatch not implemented)
- **Real authentication** — MS-14 (placeholder Authenticator interface only)
- **Real permissions** — MS-14 (placeholder PermissionChecker interface only)
- **Bulk operations** — deferred (`AllowBulk` field exists but handler not implemented)
- **Custom endpoints** — handler registration exists in types but execution deferred to MS-08
- **WebSocket / real-time** — MS-19

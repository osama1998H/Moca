# MS-18 — API Keys, Webhooks, Custom Endpoints, APIConfig per DocType Plan

## Milestone Summary

- **ID:** MS-18
- **Name:** API Keys, Webhooks, Custom Endpoints, APIConfig per DocType
- **Roadmap Reference:** ROADMAP.md → MS-18 section (lines 981-1012)
- **Goal:** API key management, webhook dispatch, per-DocType APIConfig (custom middleware, custom endpoints), whitelisted API methods, and CLI tooling for API management.
- **Why it matters:** External integrations need API keys and webhooks. Per-DocType API customization is a key differentiator over Frappe's rigid API.
- **Position in roadmap:** Beta phase (MS-18 through MS-23), order #19 of 30 milestones, effort: 3 weeks
- **Upstream dependencies:** MS-14 (Permission Engine), MS-15 (Jobs, Events, Search)
- **Downstream dependencies:** MS-22 (Security Hardening) references "no API keys (MS-18)" as out-of-scope

## Vision Alignment

MS-18 delivers the "Customizable API Layer" — the single biggest architectural improvement Moca has over Frappe (§3.3 of MOCA_SYSTEM_DESIGN.md). While MS-06 built the auto-generated REST API foundation, MS-18 extends it with three capabilities that make Moca viable for production integrations: API key authentication for machine-to-machine access, webhook dispatch for event-driven architectures, and per-DocType API customization for fine-grained control.

The API key system bridges the auth layer (MS-14) with the rate limiter (MS-06), giving each external consumer its own identity, scope restrictions, and throughput limits. Webhooks leverage the hook registry (MS-08) and background job system (MS-15) to push document events to external services reliably. Per-DocType APIConfig enforcement and whitelisted methods complete the customization story, letting each DocType control which endpoints exist, what middleware runs, and how custom actions are exposed.

The CLI commands (`moca api keys/webhooks/list/test/docs`) make the entire API surface discoverable and testable during development, with OpenAPI 3.0 generation enabling standard API documentation workflows.

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| `ROADMAP.md` | MS-18 | 981-1012 | Milestone definition, scope, acceptance criteria |
| `MOCA_SYSTEM_DESIGN.md` | §3.3 Customizable API Layer | 466-679 | APIConfig, WebhookConfig, CustomEndpoint structs, pipeline architecture |
| `MOCA_SYSTEM_DESIGN.md` | §13.2 API Key System | 1853-1868 | APIKey struct definition |
| `MOCA_CLI_SYSTEM_DESIGN.md` | §4.2.13 API Management | 2303-2481 | All CLI commands: list, test, docs, keys, webhooks |
| `pkg/meta/stubs.go` | APIConfig, WebhookConfig, etc. | 118-192 | Existing struct definitions |
| `pkg/api/rest.go` | resolveRequest | 274-320 | Endpoint allow/deny enforcement point |
| `pkg/auth/authenticator.go` | MocaAuthenticator.Authenticate | 60-118 | Auth chain: JWT → session → Guest |
| `pkg/hooks/registry.go` | HookRegistry | full file | Hook registration and dispatch infrastructure |
| `pkg/document/lifecycle.go` | DocEvent constants | full file | 14 lifecycle events for webhook triggers |
| `cmd/moca/api.go` | NewAPICommand | full file | CLI command placeholders |
| `internal/serve/server.go` | NewServer | full file | Server wiring and component injection |

## Research Notes

No web research was needed. All implementation patterns are well-defined in the design docs and existing codebase:
- bcrypt for API key secret hashing (`golang.org/x/crypto/bcrypt`)
- HMAC-SHA256 for webhook signing (Go stdlib `crypto/hmac`, `crypto/sha256`)
- Redis sliding-window rate limiting already implemented in `pkg/api/ratelimit.go`
- Background job dispatch via Redis Streams already available from MS-15

## Milestone Plan

### Task 1

- **Task ID:** MS-18-T1
- **Title:** API Key Authentication System
- **Status:** Completed
- **Description:**
  Implement the full API key lifecycle: database table (`tab_api_key`), CRUD operations (create with bcrypt-hashed secret, validate, revoke, rotate with optional grace period, list), scope enforcement via `APIScopePerm`, per-key rate limiting, and IP allowlist validation.

  **Auth chain integration:** Extend `MocaAuthenticator.Authenticate()` in `pkg/auth/authenticator.go` (lines 60-104) to insert a new step between Bearer JWT and session cookie that checks for the `Authorization: token KEY:SECRET` format. Currently `extractBearerToken()` (line 108) only handles `Bearer` prefix — add a parallel `extractTokenAuth()` that parses `token KEY:SECRET`. When an API key is authenticated, store `APIKeyID` and `APIScopes` in request context (extend `pkg/api/context.go`).

  **Scope enforcement:** Create a `ScopeEnforcer` that wraps the existing `PermissionChecker` interface. Before role-based permission checks, verify the API key's `APIScopePerm` entries (defined in `pkg/meta/stubs.go` lines 24-29) permit the requested doctype + operation. A key with `orders:read` scope should be able to GET SalesOrder but rejected on POST.

  **Per-key rate limiting:** Extend `pkg/api/ratelimit.go` to check for API key context — when present, use key-specific `RateLimitConfig` and a Redis key pattern `rl:{site}:apikey:{keyID}` instead of the default per-user key.

  **IP allowlist:** During authentication, compare `r.RemoteAddr` (or `X-Forwarded-For`) against the key's `IPAllowlist` CIDRs using `net.ParseCIDR`. Reject with 403 if not matched.

  **Database schema:**
  ```sql
  CREATE TABLE {schema}.tab_api_key (
      key_id        TEXT PRIMARY KEY,
      secret_hash   TEXT NOT NULL,
      label         TEXT NOT NULL,
      user_id       TEXT NOT NULL REFERENCES {schema}.tab_user(name),
      scopes        JSONB NOT NULL DEFAULT '[]',
      rate_limit    JSONB,
      ip_allowlist  JSONB DEFAULT '[]',
      expires_at    TIMESTAMPTZ,
      last_used_at  TIMESTAMPTZ,
      is_active     BOOLEAN NOT NULL DEFAULT true,
      created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      revoked_at    TIMESTAMPTZ
  );
  ```

- **Why this task exists:** API keys are the primary mechanism for machine-to-machine authentication. Without them, external integrations cannot authenticate to Moca's API. Directly satisfies AC1 and AC2.
- **Dependencies:** None
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §13.2 (lines 1853-1868) — APIKey struct
  - `pkg/meta/stubs.go` lines 20-29 — APIScopePerm struct
  - `pkg/auth/authenticator.go` lines 60-118 — current auth chain
  - `pkg/api/ratelimit.go` — existing rate limiter
  - `pkg/api/context.go` — context value helpers
- **Deliverable:**
  - `pkg/api/apikey.go` — APIKeyStore (CRUD), APIKeyValidator, ScopeEnforcer
  - `pkg/api/apikey_test.go` — unit tests
  - Extension to `pkg/auth/authenticator.go` — token KEY:SECRET auth step
  - Extension to `pkg/api/ratelimit.go` — per-key rate limit support
  - Extension to `pkg/api/context.go` — APIKeyID, APIScopes context keys
  - DDL migration for `tab_api_key` table
- **Acceptance Criteria:**
  - `Authorization: token KEY:SECRET` authenticates and returns the associated user with scope restrictions (AC1)
  - API key with `orders:read` scope can GET but not POST (AC2)
  - Expired or revoked keys are rejected with 401
  - IP allowlist blocks requests from non-allowed IPs with 403
  - Per-key rate limiting works independently of per-user limits
- **Risks / Unknowns:**
  - Grace period during key rotation requires tracking two valid secrets temporarily — consider storing `prev_secret_hash` + `prev_expires_at` columns or using Redis TTL on old hash

### Task 2

- **Task ID:** MS-18-T2
- **Title:** Webhook Dispatch Engine with Background Delivery
- **Status:** Completed
- **Description:**
  Implement webhook matching, payload construction, HMAC-SHA256 signing, background dispatch via Redis Streams, retry with exponential backoff, and delivery logging.

  **Webhook matching:** Register a global hook via `HookRegistry.RegisterGlobal()` (in `pkg/hooks/registry.go`) for events that webhooks care about (`after_insert`, `on_update`, `on_submit`, `on_cancel`, `on_trash`, `after_delete`). When a document event fires, look up the document's MetaType `APIConfig.Webhooks` (from `pkg/meta/stubs.go` lines 127, 184-192), filter by matching `Event` and `Filters` (document field conditions), and enqueue a delivery job.

  **Payload construction:** Build a JSON payload:
  ```json
  {
    "event": "after_insert",
    "doctype": "SalesOrder",
    "document_name": "SO-0001",
    "data": { ... },
    "timestamp": "2026-04-04T12:00:00Z",
    "site": "acme.localhost"
  }
  ```
  Sign with HMAC-SHA256 using `WebhookConfig.Secret` and include signature in `X-Moca-Signature-256` header.

  **Background dispatch:** Publish `WebhookDeliveryJob` to a Redis Stream topic (`moca:webhooks:dispatch`), leveraging the MS-15 job infrastructure. The worker consumes jobs and makes HTTP calls with 30s timeout. On failure, re-enqueue with exponential backoff (5s, 10s, 20s) up to `RetryCount` (default 3).

  **Delivery logging:**
  ```sql
  CREATE TABLE {schema}.tab_webhook_log (
      name            TEXT PRIMARY KEY,
      webhook_event   TEXT NOT NULL,
      webhook_url     TEXT NOT NULL,
      doctype         TEXT NOT NULL,
      document_name   TEXT NOT NULL,
      status_code     INT,
      response_body   TEXT,
      duration_ms     INT,
      attempt         INT NOT NULL DEFAULT 1,
      error_message   TEXT,
      created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );
  ```

- **Why this task exists:** Webhooks are the primary mechanism for pushing document events to external systems. This is the event-driven integration layer that makes Moca useful in heterogeneous architectures. Directly satisfies AC3.
- **Dependencies:** None (can proceed in parallel with T1 and T3)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §3.3.3 (lines 587-595) — WebhookConfig struct
  - `pkg/meta/stubs.go` lines 180-192 — WebhookConfig definition
  - `pkg/hooks/registry.go` — HookRegistry.RegisterGlobal()
  - `pkg/hooks/docevents.go` — DocEventDispatcher
  - `pkg/document/lifecycle.go` — DocEvent constants
  - MS-15 background job infrastructure (Redis Streams producer/consumer)
- **Deliverable:**
  - `pkg/api/webhook.go` — WebhookDispatcher, HMAC signing, payload builder, delivery executor
  - `pkg/api/webhook_test.go` — unit tests (with `net/http/httptest` server for delivery testing)
  - DDL migration for `tab_webhook_log` table
  - Integration with HookRegistry in `internal/serve/server.go`
- **Acceptance Criteria:**
  - Webhook fires HTTP POST on document creation (AC3)
  - Retries 3x on failure with exponential backoff (AC3)
  - HMAC-SHA256 signature is correct and verifiable by receiver
  - Delivery logs are persisted and queryable by webhook name, status, and time
  - Webhook filters correctly match document field conditions
- **Risks / Unknowns:**
  - Payload canonicalization must be deterministic for HMAC verification — use `json.Marshal` which sorts map keys in Go
  - Background job infrastructure from MS-15 must be stable and available

### Task 3

- **Task ID:** MS-18-T3
- **Title:** Per-DocType APIConfig Enforcement, Custom Endpoints, and Whitelisted Methods
- **Status:** Completed
- **Description:**
  Implement the remaining APIConfig enforcement features: per-DocType custom middleware, custom endpoint routing, whitelisted API methods, and per-DocType rate limiting.

  **Disabled endpoints:** Already enforced in `resolveRequest()` at `pkg/api/rest.go:302-309` via `allowCheck` callbacks against `APIConfig.AllowGet/AllowCreate/AllowUpdate/AllowDelete`. Add comprehensive test coverage to verify correct 405 responses.

  **Per-DocType custom middleware:** Create a `MiddlewareRegistry` mapping string names to `func(http.Handler) http.Handler`. During request handling, after MetaType resolution, look up `APIConfig.Middleware` names and apply them. Apps register middleware by name during initialization via the app loader (`pkg/apps/`).

  **Custom endpoint routing:** For each MetaType with `CustomEndpoints` (defined in `pkg/meta/stubs.go` lines 172-178), register routes on the mux as `{METHOD} /api/{version}/resource/{doctype}/{path}`. Create a `HandlerRegistry` mapping handler name strings (from `CustomEndpoint.Handler`) to `http.HandlerFunc`. Apps register handlers during initialization. Each custom endpoint gets its own rate limit and middleware chain per `CustomEndpoint` config.

  **Whitelisted API methods:** Implement a `MethodHandler` serving `POST /api/v1/method/{name}` and `GET /api/v1/method/{name}`. A `MethodRegistry` maps method names to handler functions. Only explicitly registered methods are accessible — no reflection-based discovery. This enables patterns like `/api/method/send_email`.

  **Per-DocType rate limiting:** In the request pipeline, after MetaType resolution, apply `APIConfig.RateLimit` using the existing `RateLimiter` with key pattern `rl:{site}:{user}:{doctype}`.

- **Why this task exists:** Per-DocType API customization is what differentiates Moca from Frappe's rigid API. Custom endpoints and whitelisted methods enable app-specific API surfaces. Directly satisfies AC4.
- **Dependencies:** None (can proceed in parallel with T1 and T2)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §3.3.3 (lines 522-596) — APIConfig, CustomEndpoint structs
  - `pkg/meta/stubs.go` lines 118-178 — APIConfig, CustomEndpoint definitions
  - `pkg/api/rest.go` lines 274-320 — resolveRequest() integration point
  - `pkg/api/gateway.go` — Gateway struct and route registration
  - `pkg/api/middleware.go` — existing middleware chain
- **Deliverable:**
  - `pkg/api/middleware_registry.go` — Named middleware registry
  - `pkg/api/custom_endpoint.go` — Custom endpoint handler registry and route registration
  - `pkg/api/method.go` — Whitelisted method handler and registry
  - Tests: `pkg/api/middleware_registry_test.go`, `pkg/api/custom_endpoint_test.go`, `pkg/api/method_test.go`
  - Extension to `pkg/api/rest.go` — per-DocType rate limiting and middleware application
  - Integration wiring in `internal/serve/server.go`
- **Acceptance Criteria:**
  - DocType with `AllowDelete: false` (i.e., `DisabledEndpoints: ["delete"]`) has no DELETE endpoint, returns 405 (AC4)
  - Custom endpoints are routable and invoke registered handlers
  - Whitelisted methods respond at `/api/v1/method/{name}`
  - Per-DocType middleware executes in declared order
  - Per-DocType rate limiting works independently of global limits
  - Unregistered handler names produce a clear error at startup, not a runtime panic
- **Risks / Unknowns:**
  - Custom endpoint handler registration relies on apps registering functions by name — need a clean initialization sequence via the app loader that doesn't introduce import cycles

### Task 4

- **Task ID:** MS-18-T4
- **Title:** CLI Commands for API Management
- **Status:** Not Started
- **Description:**
  Replace all placeholder `newSubcommand()` calls in `cmd/moca/api.go` with real implementations:

  **API Key commands** (per `MOCA_CLI_SYSTEM_DESIGN.md` lines 2367-2427):
  - `moca api keys create` — Flags: --user, --label, --scopes, --expires, --ip-allow. Display `key_id:secret` pair (shown only once).
  - `moca api keys revoke KEY_ID` — Revoke immediately. --force skips confirmation prompt.
  - `moca api keys list` — Table: KEY_ID, LABEL, USER, SCOPES, LAST_USED, EXPIRES. Filters: --user, --status (active/revoked/expired/all).
  - `moca api keys rotate KEY_ID` — Generate new secret, show new key:secret. --grace-period keeps old secret valid.

  **Webhook commands** (per `MOCA_CLI_SYSTEM_DESIGN.md` lines 2429-2480):
  - `moca api webhooks list` — Table: NAME, DOCTYPE, EVENT, URL, STATUS. Filter: --doctype.
  - `moca api webhooks test WEBHOOK_NAME` — Send test payload to endpoint, show response status and timing.
  - `moca api webhooks logs [WEBHOOK_NAME]` — Delivery history: TIMESTAMP, WEBHOOK, EVENT, STATUS, RESPONSE, DURATION. Filters: --status, --limit.

  **API inspection commands** (per `MOCA_CLI_SYSTEM_DESIGN.md` lines 2303-2350):
  - `moca api list` — Build an `EndpointEnumerator` that walks all MetaTypes, enumerating auto-generated CRUD + custom endpoints + whitelisted methods. Table: METHOD, PATH, SOURCE, AUTH. Filters: --doctype, --method. This enumerator is reused by T5 for OpenAPI generation.
  - `moca api test ENDPOINT` — Make real HTTP request to running server. Flags: --method, --user, --api-key, --data, --repeat N (timing stats: min/max/avg/p95), --verbose (full headers).

  All commands use `internal/output` formatters (TTY table, JSON) and connect to site DB/Redis via existing `config.ProjectConfig` and `internal/context` CLI context resolver.

- **Why this task exists:** CLI commands make the API surface discoverable, testable, and manageable during development. They are a key part of the developer experience. Satisfies AC5 and AC6.
- **Dependencies:** MS-18-T1 (API key CRUD), MS-18-T2 (webhook logs query), MS-18-T3 (endpoint enumeration for `api list`)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.13 (lines 2303-2481) — all command specs with flags and example output
  - `cmd/moca/api.go` — existing command structure with placeholders
  - `internal/output/` — CLI output formatters (TTY, JSON, Table, Progress)
  - `internal/context/` — CLI context resolver (project/site/env detection)
- **Deliverable:**
  - Updated `cmd/moca/api.go` — real implementations for all 10 subcommands
  - `cmd/moca/api_test.go` — CLI integration tests
- **Acceptance Criteria:**
  - `moca api list` shows all registered endpoints with method, path, source, auth type (AC5)
  - `moca api test /api/v1/resource/SalesOrder --user admin` returns valid response with timing (AC6)
  - All key and webhook commands produce correct table/JSON output matching the format in the CLI design doc
  - --json flag works on all list commands
- **Risks / Unknowns:**
  - `moca api test` requires a running server — may need to bootstrap a lightweight server instance or connect to an existing one via the site config's server address

### Task 5

- **Task ID:** MS-18-T5
- **Title:** OpenAPI 3.0 Specification Generator
- **Status:** Not Started
- **Description:**
  Implement a struct-based OpenAPI 3.0.3 spec generator that walks all enabled MetaTypes and produces a complete API specification. Implement the `moca api docs` CLI command.

  **FieldType → OpenAPI mapping** (referencing `pkg/meta/fielddef.go` for all FieldType constants):
  | Moca FieldType | OpenAPI type | format |
  |---|---|---|
  | Data, Text, SmallText, LongText, Code, HTML, Markdown | string | — |
  | Int | integer | int64 |
  | Float, Currency, Percent | number | double |
  | Check | boolean | — |
  | Date | string | date |
  | Datetime | string | date-time |
  | Time | string | time |
  | Select | string | enum (from options) |
  | Link | string | — (description refs linked DocType) |
  | DynamicLink | string | — |
  | Attach, AttachImage | string | uri |
  | Table | array | items: $ref to child schema |
  | JSON | object | — |
  | Password | string | password |
  | Color | string | — |
  | Rating | number | — |

  **Spec generation:** For each enabled MetaType:
  - Generate component schema from `Fields` (respecting `InAPI`, `ExcludeFields`, `AlwaysInclude`)
  - Generate path items for enabled operations (list, get, create, update, delete, count, bulk) based on `APIConfig.Allow*` flags
  - Generate path items for `CustomEndpoints`
  - Generate path items for whitelisted methods
  - Define security schemes: Bearer JWT (`bearerAuth`), Token API Key (`apiKeyAuth`), Session Cookie (`sessionAuth`)

  **CLI command** (per `MOCA_CLI_SYSTEM_DESIGN.md` lines 2352-2365):
  - `moca api docs` — Flags: --site, --output (file path, default stdout), --format (json/yaml), --serve (start Swagger UI server), --port (default 8002)
  - `--serve` starts a local HTTP server embedding Swagger UI HTML pointing to CDN-hosted assets, serving the generated spec at `/openapi.json`

- **Why this task exists:** OpenAPI 3.0 generation makes the API self-documenting and enables standard tooling (Swagger UI, client generators, contract testing). Directly satisfies AC7.
- **Dependencies:** MS-18-T3 (custom endpoints and methods for complete spec), MS-18-T1 (API key security scheme)
- **Inputs / References:**
  - `MOCA_CLI_SYSTEM_DESIGN.md` §4.2.13 `moca api docs` (lines 2352-2365)
  - `MOCA_SYSTEM_DESIGN.md` §3.3.3 (lines 522-596) — APIConfig controlling endpoint exposure
  - `pkg/meta/fielddef.go` — FieldType constants for type mapping
  - `pkg/meta/stubs.go` lines 118-143 — APIConfig endpoint controls
- **Deliverable:**
  - `pkg/api/openapi.go` — OpenAPI 3.0 spec builder, FieldType mapping, schema generation
  - `pkg/api/openapi_test.go` — unit tests validating generated spec structure and field type mapping
  - Extension to `cmd/moca/api.go` — `moca api docs` command implementation
- **Acceptance Criteria:**
  - `moca api docs` generates valid OpenAPI 3.0 spec (AC7)
  - All enabled MetaType endpoints appear in the spec with correct paths, methods, and request/response schemas
  - Field types map correctly to OpenAPI types per the mapping table above
  - Security schemes are defined for JWT, API Key, and Session
  - Both JSON and YAML output formats work
  - `--serve` starts a browsable Swagger UI server
- **Risks / Unknowns:**
  - Table fields with nested child schemas require recursive schema generation
  - ComputedFields may need special handling (type from expression return type)
  - YAML output may require `gopkg.in/yaml.v3` — verify it's already in go.mod or add it

## Recommended Execution Order

1. **MS-18-T1** (API Keys), **MS-18-T2** (Webhooks), **MS-18-T3** (APIConfig/Custom Endpoints) — in parallel, no interdependencies
2. **MS-18-T4** (CLI Commands) — after T1, T2, T3 complete, as it consumes all their APIs
3. **MS-18-T5** (OpenAPI Generator) — can overlap with T4, depends on T1 and T3 for complete spec

```
T1 (API Keys) ──────────┐
                         ├──> T4 (CLI) ──> T5 (OpenAPI)
T2 (Webhooks) ───────────┤         ↑
                         │         │
T3 (Custom Endpoints) ───┘─────────┘
```

## Open Questions

- **Webhook dead-letter queue:** Should failed webhooks (after exhausting retries) go to a DLQ or just log the final failure? Design doc doesn't specify DLQ for webhooks specifically. MS-15 has general job DLQ — may be sufficient.
- **Custom endpoint handler registration:** What is the exact initialization sequence for apps to register handlers by name? Likely via `pkg/apps/` app loader calling a registration function during `Install()` — needs to be defined.
- **API key secret display:** Secret is shown only once at creation. Should `rotate` also display the new secret? CLI design doc implies yes.
- **Grace period storage:** During key rotation with `--grace-period`, both old and new secrets must be valid. Two options: (a) store `prev_secret_hash` + `prev_expires_at` columns in DB, or (b) cache old hash in Redis with TTL. DB approach is simpler and survives restarts.

## Out of Scope for This Milestone

- OAuth2 provider (deferred to MS-22)
- GraphQL API (deferred to MS-20)
- API versioning implementation beyond what already exists in VersionRouter (MS-06)
- Frontend/Desk UI for API key management or webhook configuration
- Webhook management via DocType (database-stored webhook configs) — MS-18 uses MetaType-defined webhooks only

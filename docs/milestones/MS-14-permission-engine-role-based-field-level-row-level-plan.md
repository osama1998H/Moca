# MS-14 — Permission Engine: Role-Based, Field-Level, Row-Level Plan

## Milestone Summary

- **ID:** MS-14
- **Name:** Permission Engine — Role-Based, Field-Level, Row-Level
- **Roadmap Reference:** ROADMAP.md → MS-14 section (lines 798–842)
- **Goal:** Implement complete permission resolution (role-based DocType perms, field-level read/write, row-level matching, custom rules, PostgreSQL RLS) and session/JWT authentication, replacing all placeholder stubs from MS-06.
- **Why it matters:** The API layer is functional but entirely unprotected — every endpoint returns data to Guest users. Before Beta, every endpoint must enforce permissions. This milestone is the security foundation for the entire framework.
- **Position in roadmap:** Order #15 of 30 milestones (4th Alpha milestone). Estimated 4 weeks.
- **Upstream dependencies:** MS-06 (REST API Layer — provides `Authenticator`/`PermissionChecker` interfaces, middleware chain), MS-08 (Hook Registry & App System — provides User/Role DocTypes, hook infrastructure)
- **Downstream dependencies:** MS-17 (React Desk — needs auth context for UI), MS-18 (API Keys/Webhooks — extends permission engine with API scoping), MS-22 (Security Hardening — adds OAuth2/SAML/OIDC), MS-23 (Workflow Engine — respects permissions)

## Vision Alignment

MS-14 implements Layers 3–5 of Moca's Defense-in-Depth model (§13.3): Auth validation, application-layer permission engine, and database-layer RLS policies. It transforms Moca from a development prototype into a system that can safely serve multiple users with different access levels on the same tenant.

The permission engine follows Moca's metadata-driven philosophy: permissions are declared as `PermRule` entries on MetaType definitions, and the engine automatically enforces them at the API, document, query, and database layers. This mirrors how MetaType already drives DDL, CRUD routes, and validation — permissions are just another facet of the same metadata.

The 5-step resolution order (API scope → role-based → field-level → row-level → custom rule → RLS) provides defense-in-depth within the permission system itself. Each layer narrows access further, and RLS ensures that even direct SQL access respects tenant/role boundaries.

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| `MOCA_SYSTEM_DESIGN.md` | §3.4 Permission Engine | 682–711 | Core `PermRule` struct, `APIScopePerm`, 5-step resolution order |
| `MOCA_SYSTEM_DESIGN.md` | §13.1 Authentication Methods | 1843–1851 | Session cookie, JWT, API key methods table |
| `MOCA_SYSTEM_DESIGN.md` | §13.2 API Key System | 1853–1868 | `APIKey` struct (OUT of scope — MS-18) |
| `MOCA_SYSTEM_DESIGN.md` | §13.3 Defense in Depth | 1870–1880 | 7-layer security model |
| `MOCA_SYSTEM_DESIGN.md` | §14 Complete Request Lifecycle | 1884–1920 | Auth at step 4, permission check at step 8, field-level at step 9f |
| `MOCA_SYSTEM_DESIGN.md` | §5.1 Redis Caching | 1030–1042 | `perm:{site}:{user}:{doctype}` and `session:{token}` cache keys |
| `ROADMAP.md` | MS-14 | 798–842 | Milestone definition, scope, deliverables, acceptance criteria |
| `pkg/api/auth.go` | Interfaces | 1–55 | `Authenticator`, `PermissionChecker`, `NoopAuthenticator`, `AllowAllPermissionChecker` |
| `pkg/api/middleware.go` | authMiddleware | 206–228 | Current auth middleware calling `Authenticate()` |
| `pkg/api/gateway.go` | Handler() | 49–70 | Middleware chain order |
| `pkg/api/rest.go` | resolveRequest | 253–308 | Existing `CheckDocPerm()` call site (line 300) |
| `pkg/meta/stubs.go` | PermRule | 10–18 | Permission rule struct with bitmask, field-level, match fields |
| `pkg/auth/user.go` | User | 1–12 | Current user struct (Email, FullName, Roles only) |
| `pkg/document/context.go` | DocContext | 18–40 | User carried through document lifecycle |
| `pkg/builtin/core/user_controller.go` | — | 1–54 | Existing bcrypt password hashing |
| `pkg/builtin/core/modules/core/doctypes/` | user, role, has_role, doc_perm | — | Core doctype definitions |

## Research Notes

No web research was needed. The design documents and existing codebase provide sufficient detail for implementation. Key observations from codebase exploration:

- **Redis infrastructure ready:** `internal/drivers/redis.go` already defines `KeyPerm` and `KeySession` constants and provides a 4-DB client factory (DB 2 reserved for sessions).
- **Middleware chain ready:** `pkg/api/gateway.go` already chains Tenant → Auth → RateLimit. The auth middleware calls `Authenticator.Authenticate()` and stores the user in context. Just needs a real implementation.
- **Permission check already wired:** `pkg/api/rest.go:300` already calls `h.perm.CheckDocPerm(...)` — the integration point exists, it just calls `AllowAllPermissionChecker`.
- **Gateway options ready:** `WithAuthenticator()` and `WithPermissionChecker()` options already exist on the Gateway.
- **Password hashing exists:** `pkg/builtin/core/user_controller.go` already implements bcrypt hashing in a BeforeSave hook.

## Milestone Plan

### Task 1

- **Task ID:** MS-14-T1
- **Title:** Permission Resolution Engine with Redis Caching and Custom Rules
- **Status:** Completed
- **Description:** Build `ResolvePermissions(user, doctype) -> EffectivePerms` — the core function that evaluates all `PermRule` entries on a MetaType against a user's roles, producing a merged effective permission set. Includes: bitmask constants (`PermRead=1`, `PermWrite=2`, `PermCreate=4`, `PermDelete=8`, `PermSubmit=16`, `PermCancel=32`, `PermAmend=64`), role merging logic (union across all matching roles), Redis caching with `perm:{site}:{user}:{doctype}` key pattern (TTL 2min), cache invalidation helper, and the custom rule registry (`map[string]CustomRuleFunc` for Go functions referenced by `PermRule.CustomRule`).
- **Why this task exists:** Every other task depends on the permission resolution engine. Auth middleware needs it to populate effective permissions; field-level and row-level filtering read from the resolved permissions; RLS mirrors these rules in SQL. Custom rules are part of the 5-step resolution (step 5) and naturally belong here.
- **Dependencies:** None (first task)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §3.4 lines 682–711 (5-step resolution, PermRule struct)
  - `pkg/meta/stubs.go` lines 10–18 (PermRule definition)
  - `internal/drivers/redis.go` (`KeyPerm` constant, Cache client)
  - ROADMAP.md lines 806–807 (deliverables 1, 7)
- **Deliverable:**
  - `pkg/auth/permission.go` — `EffectivePerms` struct, bitmask constants, `ResolvePermissions()`, `HasPerm()` method
  - `pkg/auth/permission_cache.go` — `CachedPermissionResolver` wrapping Redis, cache invalidation
  - `pkg/auth/custom_rules.go` — `CustomRuleRegistry` with `Register()` and `Evaluate()`
  - `pkg/auth/permission_test.go`, `pkg/auth/permission_cache_test.go` — unit tests
- **Acceptance Criteria:**
  - User with roles `["Sales User", "Customer User"]` and MetaType with PermRules granting `read|create` to "Sales User" and `read` to "Customer User" → `HasPerm("create")` true, `HasPerm("delete")` false
  - Second call within 2min hits Redis cache (no MetaType lookup)
  - Cache key follows `perm:{site}:{user}:{doctype}` pattern
  - Custom rule "require_active_subscription" can be registered and evaluated during resolution
- **Risks / Unknowns:**
  - Bitmask positions must be stable once released — define and document clearly
  - Custom rule registry must be lookup-only (no arbitrary code execution)

### Task 2

- **Task ID:** MS-14-T2
- **Title:** JWT Authentication, Session Management, and Login/Logout API
- **Status:** Completed
- **Description:** Implement the full authentication stack: (1) JWT token issuing and validation (`pkg/auth/jwt.go` — access token + refresh token pair, configurable TTLs, refresh rotation), (2) Redis-backed session management (`pkg/auth/session.go` — using Redis DB 2 via `drivers.RedisClients.Session`, session CRUD with TTL), (3) `MocaAuthenticator` implementing `api.Authenticator` that checks Bearer header first then `moca_sid` session cookie, replacing `NoopAuthenticator`, (4) Login/logout HTTP endpoints (`POST /api/v1/auth/login`, `POST /api/v1/auth/logout`, `POST /api/v1/auth/refresh`). Wire into Gateway via existing `WithAuthenticator()` option.
- **Why this task exists:** Authentication is a single coherent concern — JWT issuing, session storage, token validation, and login/logout are tightly coupled and must ship as an atomic testable unit. This replaces the Guest-always placeholder.
- **Dependencies:** MS-14-T1 (authenticator loads user and populates `auth.User` with roles for permission resolution)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §13.1 lines 1843–1851 (Session Cookie, JWT Bearer methods)
  - `MOCA_SYSTEM_DESIGN.md` §14 line 1892 (step 4: "AUTH: JWT decoded → user loaded → session validated")
  - `pkg/api/auth.go` lines 12–15 (`Authenticator` interface)
  - `pkg/api/middleware.go` lines 206–228 (authMiddleware calling `Authenticate()`)
  - `internal/drivers/redis.go` (`KeySession` constant, Session client on DB 2)
  - `pkg/builtin/core/user_controller.go` (bcrypt password hashing)
  - ROADMAP.md lines 808–815 (deliverables 2, 3, 4, 9)
- **Deliverable:**
  - `pkg/auth/jwt.go` — `JWTConfig`, `IssueTokenPair()`, `ValidateAccessToken()`, `RotateRefreshToken()`
  - `pkg/auth/session.go` — `SessionManager` with `Create/Get/Destroy` using Redis DB 2
  - `pkg/auth/authenticator.go` — `MocaAuthenticator` (Bearer → session cookie → Guest fallback)
  - `pkg/api/auth_handler.go` — Login, logout, refresh HTTP handlers
  - `pkg/auth/jwt_test.go`, `pkg/auth/session_test.go`, `pkg/auth/authenticator_test.go`
- **Acceptance Criteria:**
  - `POST /api/v1/auth/login` with valid credentials returns `{"access_token", "refresh_token", "expires_in"}` and sets `moca_sid` HttpOnly cookie
  - Bearer token in subsequent requests resolves correct user (not Guest)
  - Expired JWT returns 401; refresh rotation issues new pair and invalidates old refresh token
  - `POST /api/v1/auth/logout` destroys session and clears cookie
  - Invalid credentials return 401 with `AUTH_FAILED` error code
- **Risks / Unknowns:**
  - JWT secret must be configurable per-site (via `SiteContext` or env config)
  - Refresh token rotation must be atomic (Redis transaction or single-key swap) to prevent reuse attacks
  - `User` struct may need extension with `UserDefaults map[string]string` for row-level matching in T4 — the user-loading logic in the authenticator should fetch these from the User document

### Task 3

- **Task ID:** MS-14-T3
- **Title:** Permission Checker Implementation and Field-Level Filtering
- **Status:** Completed
- **Description:** Implement `RoleBasedPermChecker` satisfying `api.PermissionChecker` interface, using `CachedPermissionResolver` from T1 to check doctype-level permissions. Then implement field-level filtering: on read responses, strip fields not in `field_level_read`; on write requests, reject fields not in `field_level_write`. Field-level filtering integrates into the existing `TransformerChain` in `pkg/api/transformer.go`. Replace `AllowAllPermissionChecker` default in Gateway.
- **Why this task exists:** DocType-level checking and field-level filtering are tightly coupled — both consult the same `EffectivePerms`. Together they deliver two key acceptance criteria: "Sales User can read but not delete" and "only sees customer_name and grand_total".
- **Dependencies:** MS-14-T1 (needs `ResolvePermissions` and `EffectivePerms`), MS-14-T2 (needs real authenticator for non-Guest users)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §3.4 lines 704–709 (steps 2 and 3: role-based, field-level)
  - `MOCA_SYSTEM_DESIGN.md` §14 line 1903 (step 9f: field-level write check)
  - `pkg/api/auth.go` lines 20–22 (`PermissionChecker` interface)
  - `pkg/api/rest.go` line 300 (existing `CheckDocPerm()` call site)
  - `pkg/api/transformer.go` (`Transformer` interface)
  - ROADMAP.md lines 811–812 (deliverable 5)
- **Deliverable:**
  - `pkg/auth/checker.go` — `RoleBasedPermChecker` implementing `api.PermissionChecker`
  - `pkg/api/field_filter.go` — `FieldLevelTransformer` for response stripping and write rejection
  - Modify `pkg/api/rest.go` — integrate `FieldLevelTransformer` into transformer chain
  - Modify `pkg/api/gateway.go` — wire `RoleBasedPermChecker` as default
  - `pkg/auth/checker_test.go`, `pkg/api/field_filter_test.go`
- **Acceptance Criteria:**
  - "Sales User" with `read|create` on SalesOrder can GET and POST but gets 403 on DELETE
  - `field_level_read: ["customer_name", "grand_total"]` returns only those fields plus system fields (`name`, `creation`, `modified`, `owner`, `docstatus`)
  - PUT with field not in `field_level_write` returns 403
  - Empty/nil `field_level_read` means all fields returned (no restriction)
  - Administrator role bypasses field-level filtering
- **Risks / Unknowns:**
  - System fields allowlist must be well-defined (name, creation, modified, owner, docstatus)
  - Multiple PermRules for different roles → effective `field_level_read` is the union across all matching roles

### Task 4

- **Task ID:** MS-14-T4
- **Title:** Row-Level Permission Matching via QueryBuilder WHERE Injection
- **Status:** Completed
- **Description:** Implement row-level matching: when a PermRule has `match_field`/`match_value`, inject WHERE clauses into QueryBuilder so list queries only return documents where `doc.{match_field} = user.{match_value}`. Extend `User` struct with `UserDefaults map[string]string` to carry user attributes (company, territory, etc.). Hook into `DocManager.GetList` and enforce on single-document Get/Update/Delete. Multiple match conditions from different roles are OR-ed.
- **Why this task exists:** Row-level filtering is architecturally distinct — it affects the query layer (`pkg/orm/query.go`) rather than the API layer. It must be applied consistently across all CRUD operations and is the last application-layer defense before RLS.
- **Dependencies:** MS-14-T1 (needs `EffectivePerms` with match data), MS-14-T3 (should follow basic permission checking)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §3.4 line 708 (step 4: row-level match)
  - `pkg/orm/query.go` (`QueryBuilder` struct and `Where()` method)
  - `pkg/document/crud.go` (`GetList` builds QueryBuilder — injection point)
  - `pkg/meta/stubs.go` lines 12–13 (`MatchField`, `MatchValue` on PermRule)
  - `pkg/auth/user.go` (User struct needs `UserDefaults`)
  - ROADMAP.md line 812 (deliverable 6)
- **Deliverable:**
  - `pkg/auth/row_level.go` — `RowLevelFilters(effectivePerms, user) -> []orm.Filter`
  - Modify `pkg/auth/user.go` — add `UserDefaults map[string]string` to `User`
  - Modify `pkg/document/crud.go` — inject row-level filters in `GetList`, add post-fetch check in `Get`/`Update`/`Delete`
  - `pkg/auth/row_level_test.go`
- **Acceptance Criteria:**
  - User with `match_field: "company"` on SalesOrder only sees documents where `company` matches user's company attribute
  - `GetList` SQL includes additional WHERE clause
  - Single-doc Get for document of different company returns 404 (not 403 — avoid info leakage)
  - Multiple match conditions from different roles are OR-ed
  - Administrator role bypasses row-level filtering
- **Risks / Unknowns:**
  - Performance: additional WHERE clauses on every query — match_field columns must be indexed
  - User defaults must be loaded during authentication (T2 user-loading logic needs to fetch these from User document)
  - OR-ing multiple match conditions could produce complex queries — test with realistic role combinations

### Task 5

- **Task ID:** MS-14-T5
- **Title:** PostgreSQL RLS Policy Generation and End-to-End Integration Tests
- **Status:** Completed
- **Description:** Generate `CREATE POLICY` statements from PermRule definitions to mirror application-layer restrictions in PostgreSQL. For each DocType with row-level match rules, produce `ALTER TABLE ... ENABLE ROW LEVEL SECURITY` and `CREATE POLICY ... USING (...)` SQL. Use `current_setting('moca.current_user_company')` (or equivalent session variables) set via the per-tenant pool's `AfterConnect` callback. Integrate into migration flow. Write comprehensive integration tests covering the full stack: login → CRUD → permission denial → field filtering → row filtering → RLS enforcement.
- **Why this task exists:** RLS is Layer 5 (database defense) — even direct SQL bypassing the application layer respects restrictions. Integration tests validate the entire MS-14 stack end-to-end.
- **Dependencies:** MS-14-T1, MS-14-T2, MS-14-T3, MS-14-T4 (capstone task)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §13.3 line 1877 (Layer 5: PostgreSQL RLS policies)
  - `MOCA_SYSTEM_DESIGN.md` §3.4 line 710 (step 6: RLS defense in depth)
  - `pkg/orm/migrate.go` (existing migration infrastructure)
  - `pkg/orm/postgres.go` (DBManager with per-tenant pools and `AfterConnect`)
  - ROADMAP.md line 814 (deliverable 8)
- **Deliverable:**
  - `pkg/orm/rls.go` — `GenerateRLSPolicies()`, `ApplyRLSPolicies()`, `DropRLSPolicies()`
  - Modify `pkg/orm/migrate.go` — hook RLS into schema migration flow
  - Modify `pkg/orm/postgres.go` — extend `AfterConnect` to set `moca.current_user_*` session variables
  - `pkg/orm/rls_test.go` — unit tests for SQL generation
  - `pkg/auth/integration_test.go` — full-stack integration tests (login, CRUD, permissions, field/row filtering)
  - `pkg/orm/rls_integration_test.go` — direct SQL restricted by RLS policy
- **Acceptance Criteria:**
  - DocType with `match_field: "company"` generates correct `CREATE POLICY` using session variable
  - Direct SQL `SELECT * FROM tab_sales_order` returns only user's company documents
  - Integration test: login → create → list (sees own) → switch user → list (doesn't see other's)
  - Migration adds/removes RLS policies when DocType permissions change
- **Risks / Unknowns:**
  - RLS performance overhead — needs benchmarking with realistic data volumes
  - `AfterConnect` callback modification must not break existing search_path behavior
  - Policy naming convention must be deterministic and idempotent (e.g., `moca_{table}_{role}_{idx}`)
  - Testing RLS requires actual PostgreSQL — must use existing docker-compose integration test infrastructure

## Recommended Execution Order

1. **MS-14-T1** — Foundation. Everything depends on the permission resolution engine.
2. **MS-14-T2** — Authentication. Enables non-Guest users for all subsequent testing.
3. **MS-14-T3** — Permission enforcement. Delivers the core "protected API" experience.
4. **MS-14-T4** — Row-level filtering. Extends protection to document ownership.
5. **MS-14-T5** — RLS + integration tests. Database-layer defense and full-stack validation.

Note: T1 and T2 could be parallelized by two developers since T2's dependency on T1 is limited to needing `EffectivePerms` for the permission checker (T3), not for authentication itself.

## Open Questions

- **JWT secret storage:** Should JWT secrets be stored per-site in the database (SystemSettings doctype), in the YAML config file, or via environment variables? Per-site is more flexible but adds complexity.
- **User defaults loading:** When the authenticator loads a user, should it eagerly fetch all user defaults (company, territory, etc.) or lazily load them when row-level filtering needs them? Eager is simpler but adds overhead to every request.
- **RLS session variable naming:** Should we use `moca.current_user` and `moca.current_user_company` as custom GUC variables, or use `SET LOCAL` with application-specific names? Custom GUCs require `postgresql.conf` changes or `ALTER SYSTEM SET`.

## Out of Scope for This Milestone

- **OAuth2 / SAML / OIDC** — deferred to MS-22 (Security Hardening)
- **API Key authentication** — deferred to MS-18 (API Keys, Webhooks, Custom Endpoints)
- **API Scope permissions** (`APIScopePerm`) — part of API key system, deferred to MS-18
- **Audit logging** — Layer 6 in defense-in-depth, separate milestone concern
- **Field encryption at rest** — Layer 7, separate milestone concern
- **CLI user management commands** — already implemented in MS-13

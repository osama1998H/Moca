# MS-19 — Desk Real-Time, Custom Field Types, Version Tracking Plan

## Milestone Summary

- **ID:** MS-19
- **Name:** Desk Real-Time, Custom Field Types, Version Tracking
- **Roadmap Reference:** ROADMAP.md → MS-19 section (lines 1016–1046)
- **Goal:** WebSocket real-time updates, custom field type registry for app extensions, and document version tracking.
- **Why it matters:** The Desk (from MS-17) is functional but static. Real-time prevents stale-data conflicts when multiple users edit the same document. Custom fields enable app UI extensions. Version tracking provides audit-grade change history. Together, these turn the Desk from a basic CRUD UI into a collaborative, extensible platform.
- **Position in roadmap:** Order #20 of 30 milestones (Beta release phase, Stream C: Frontend)
- **Upstream dependencies:** MS-17 (React Desk Foundation), MS-15 (Background Jobs, Scheduler, Kafka/Redis Events, Search Sync)
- **Downstream dependencies:** MS-20 (GraphQL, Dashboard, Report, i18n), MS-23 (Workflow Engine)

## Vision Alignment

MS-19 bridges the gap between a functional-but-static Desk UI and a collaborative real-time platform. The Moca design (§9.4) envisions that when User A saves a SalesOrder, User B's open FormView instantly shows a notification — preventing stale-data overwrites that plague traditional CRUD systems. This is a core differentiator over Frappe, where real-time is bolted on via Socket.IO rather than being architecturally integrated.

The custom field type registry (§9.3) is the extensibility cornerstone of the three-layer Desk composition model (§17.2). Without `registerFieldType()`, apps cannot contribute UI components — meaning the Desk is locked to the 35 built-in field types. This milestone enables the ecosystem: third-party apps can ship TreeSelect, KanbanBoard, or any custom component that the FormView renders seamlessly.

Version tracking closes the audit loop. While `tab_audit_log` (from MS-15) records *that* changes happened, `tab_version` records *what* changed at the field level. Business users need to see "who changed the customer name from Alice to Bob, and when" — not just "an update occurred." This is table-stakes for ERP/business applications.

## Source References

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| `ROADMAP.md` | MS-19 | 1016–1046 | Milestone definition, scope, deliverables, acceptance criteria |
| `MOCA_SYSTEM_DESIGN.md` | §9.4 Real-Time Updates via WebSocket | 1582–1600 | WebSocket hub architecture, Redis pub/sub bridge, client event flow |
| `MOCA_SYSTEM_DESIGN.md` | §9.3 Custom Field Type Registry | 1570–1580 | `registerFieldType()` API, app extension pattern |
| `MOCA_SYSTEM_DESIGN.md` | §9.2 Metadata-Driven Rendering | 1509–1568 | FormView rendering, FieldRenderer, FIELD_TYPE_MAP |
| `MOCA_SYSTEM_DESIGN.md` | §17.2 Desk Composition Model | 2134–2143 | Three-layer composition, `moca build desk`, app extensions |
| `docs/moca-cross-doc-mismatch-report.md` | MISMATCH-019 | 663–690 | Redis DB 3 for pub/sub, channel pattern `pubsub:doc:{site}:...` |
| `docs/moca-cross-doc-mismatch-report.md` | MISMATCH-025 | 874–907 | `@osama1998h/desk` is workspace-local npm package |
| `docs/moca-cross-doc-mismatch-report.md` | MISMATCH-021 | 737–769 | "Desk Extension" terminology (not "plugin") |

## Research Notes

No external web research was needed. Key implementation decisions derived from codebase analysis:

- **WebSocket library**: `nhooyr.io/websocket` (now `coder/websocket`) is the best fit. The codebase uses `context.Context` pervasively (server shutdown in `server.go:216-262`, transaction management in `crud.go`). nhooyr's context-native `Accept()` and read/write methods integrate cleanly. gorilla/websocket would require manual lifecycle management that the codebase doesn't use.

- **Version diff format**: Store both a field-level diff and a full snapshot in `tab_version.data` JSONB: `{"changed": {"field": {"old": X, "new": Y}}, "snapshot": {...}}`. The diff enables fast display; the snapshot enables any-to-any comparison. `buildChangesJSON()` at `crud.go:1452-1475` already computes the diff format — reuse it. PostgreSQL TOAST compression keeps storage reasonable.

- **WebSocket auth**: Browser `WebSocket` constructor cannot set `Authorization` headers. Use `?token=JWT` query parameter on upgrade. Extract and validate using existing `auth.JWTConfig` from the server. The token is short-lived (access token TTL).

- **Event bridge gap identified**: The current event flow is `crud.go → insertOutbox → OutboxPoller → Producer.Publish()` to `moca.doc.events` topic. But the WebSocket hub needs events on `pubsub:doc:{site}:{doctype}:{name}` channels (Redis DB 3). The `AfterPublishHook` in the outbox poller (type at `outbox.go:47`) is the insertion point — compose it with the existing search sync hook to also publish to WebSocket pub/sub channels.

- **Subscription granularity**: Per-doctype on Redis side (PSUBSCRIBE `pubsub:doc:{site}:{doctype}:*`), with client-side filtering by document name. This keeps Redis subscriptions to ~dozens (one per active doctype), not thousands (one per open document).

## Milestone Plan

### Task 1

- **Task ID:** MS-19-T1
- **Title:** WebSocket Hub & Redis Pub/Sub Bridge (Backend)
- **Status:** Completed
- **Description:**
  Replace the WebSocket stub (`internal/serve/websocket.go`) with a production WebSocket hub that bridges Redis pub/sub events to connected browser clients. This is the real-time backbone that all frontend features depend on.

  **1. WebSocket Hub** (`internal/serve/hub.go`):
  - `Hub` struct with connection registry: `map[string]map[*Connection]struct{}` keyed by `{site}:{doctype}`.
  - `Connection` struct wrapping `*websocket.Conn` with a dedicated write goroutine (single writer per connection to avoid concurrent write panics) and a buffered send channel.
  - Methods: `Register(conn)`, `Unregister(conn)`, `Subscribe(conn, site, doctype)`, `Unsubscribe(conn, site, doctype)`, `Broadcast(site, doctype, message)`.
  - Thread-safe via `sync.RWMutex` on the connection map.

  **2. WebSocket Endpoint** (`internal/serve/websocket.go` — full replacement):
  - `GET /ws?token={JWT}` handler using `coder/websocket` library.
  - On upgrade: parse JWT from `token` query param, validate with existing `auth.JWTConfig`, extract site + user from claims.
  - Read loop: parse JSON messages from client — `{"type": "subscribe", "doctype": "SalesOrder"}` and `{"type": "unsubscribe", "doctype": "SalesOrder"}`. Route to hub Subscribe/Unsubscribe.
  - Write loop: drain per-connection send channel, write JSON to client.
  - On disconnect: unregister from hub, clean up subscriptions.
  - Honor `ctx.Done()` for graceful server shutdown.

  **3. Redis Pub/Sub Bridge** (`internal/serve/pubsub_bridge.go`):
  - Long-running goroutine started as a server subsystem.
  - Uses `redisClients.PubSub` (DB 3, already available on `Server` struct at `server.go:45`).
  - Dynamically manages Redis PSUBSCRIBE patterns based on which doctypes have active connections (hub notifies bridge when subscription count changes for a doctype).
  - When a Redis message arrives on `pubsub:doc:{site}:{doctype}:{name}`, parse the channel to extract site/doctype/name, construct a JSON event, and call `hub.Broadcast(site, doctype, message)`.

  **4. AfterPublishHook Composition** (modify `internal/serve/subsystems.go`):
  - The outbox poller's `AfterPublishHook` (type `func(ctx, DocumentEvent) error` at `outbox.go:47`) is currently used only for search sync enqueue (`subsystems.go:166-168`).
  - Add a WebSocket pub/sub hook that publishes the `DocumentEvent` to Redis channel `pubsub:doc:{site}:{doctype}:{name}` using `redisClients.PubSub.Publish()`.
  - Compose with the existing search sync hook: create a `composeHooks(hooks ...AfterPublishHook) AfterPublishHook` helper that runs all hooks sequentially.
  - Pass `redisClients` into `OutboxSubsystem` to enable the WebSocket hook.

  **5. Server Integration** (modify `internal/serve/server.go`):
  - Replace `registerWebSocketStub(gw.Mux())` at line 182 with real WebSocket handler registration.
  - Inject Hub and RedisClients into the WebSocket handler.
  - Start the pub/sub bridge goroutine in `Server.Run()`.

- **Why this task exists:** The WebSocket hub is the foundation for all real-time features (T4's frontend). Without it, User B cannot be notified when User A saves a document. The Redis pub/sub bridge connects the existing event pipeline (outbox → producer → topic) to WebSocket clients.
- **Dependencies:** None (pure backend work, builds on existing infrastructure from MS-15)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §9.4 lines 1582–1600 (WebSocket architecture, Redis pub/sub bridge diagram)
  - `internal/serve/websocket.go` lines 1–18 (stub to replace)
  - `internal/serve/server.go` lines 170–212 (server initialization, line 182 stub registration)
  - `internal/serve/subsystems.go` lines 129–207 (OutboxSubsystem, AfterPublishHook at line 148/166)
  - `pkg/events/outbox.go` lines 46–47 (AfterPublishHook type)
  - `pkg/events/event.go` lines 30–44 (DocumentEvent struct)
  - `internal/drivers/redis.go` line 36 (RedisClients.PubSub on DB 3)
  - `docs/moca-cross-doc-mismatch-report.md` MISMATCH-019 lines 663–690 (channel pattern: `pubsub:doc:{site}:{doctype}:{name}`)
- **Deliverable:**
  - `internal/serve/hub.go` — WebSocket hub with connection registry and subscription management
  - `internal/serve/websocket.go` — Real WebSocket upgrade handler (replaces stub)
  - `internal/serve/pubsub_bridge.go` — Redis PSUBSCRIBE ↔ Hub bridge
  - Modified `internal/serve/subsystems.go` — Composed AfterPublishHook (search sync + WS pub)
  - Modified `internal/serve/server.go` — Hub initialization and WS endpoint registration
  - Unit tests for hub subscribe/unsubscribe/broadcast, integration test for end-to-end event flow
- **Acceptance Criteria:**
  - `GET /ws?token={valid_jwt}` upgrades to WebSocket (101 Switching Protocols)
  - `GET /ws` without token returns 401
  - Client sends `{"type":"subscribe","doctype":"SalesOrder"}` — server acknowledges
  - When a document is saved via REST API, the outbox poller publishes to Redis pub/sub channel, and connected WebSocket clients subscribed to that doctype receive `{"type":"doc_update","doctype":"SalesOrder","name":"SO-001","user":"admin@example.com"}` 
  - WebSocket connections tear down cleanly on server shutdown (context cancellation)
  - Hub handles concurrent subscribe/unsubscribe without races (race detector passes)
- **Risks / Unknowns:**
  - WebSocket scaling at 50k connections (noted in ROADMAP.md risks): mitigate with per-connection write goroutine and buffered channels. If fan-out becomes a bottleneck, batch broadcasts. This is a future optimization, not a blocker for initial implementation.
  - `AfterPublishHook` is currently a single function, not a slice. Composing hooks requires either changing the type to `[]AfterPublishHook` (broader change) or wrapping with a combiner function (simpler). Recommend the combiner approach to minimize blast radius.

---

### Task 2

- **Task ID:** MS-19-T2
- **Title:** Version Tracking Engine (Backend)
- **Status:** Completed
- **Description:**
  Implement field-level version tracking for documents where `MetaType.TrackChanges` is true. On every Insert and Update, record a version snapshot in `tab_version`. Add a REST API endpoint to retrieve version history.

  **1. Version Insertion Logic** (`pkg/document/version.go` — new file):
  - `insertVersion(ctx context.Context, tx pgx.Tx, doctype, docname, uid string, changed map[string]any, snapshot map[string]any) error`
    - Generates a UUID `name` for the version record.
    - Builds `data` JSONB: `{"changed": {field: {old, new}}, "snapshot": {full doc values}}`.
    - INSERTs into `tab_version` using the existing schema (`ddl.go:190-203`): `(name, ref_doctype, docname, data, owner, creation)`.
    - Called within the same database transaction as the document write.
  - `fetchVersions(ctx context.Context, pool *pgxpool.Pool, site, doctype, docname string, limit, offset int) ([]VersionRecord, int, error)`
    - Queries `tab_version` WHERE `ref_doctype=$1 AND docname=$2` ORDER BY `creation DESC` with pagination.
    - Returns `VersionRecord` structs with: Name, RefDoctype, DocName, Data (parsed), Owner, Creation.

  **2. Hook into Insert** (modify `pkg/document/crud.go` lines 765–779):
  - After `insertOutbox` (line 775) and before `insertAuditLog` (line 779), add:
    ```go
    if mt.TrackChanges {
        insertVersion(txCtx, tx, doctype, name, uid, nil, doc.AsMap())
    }
    ```
  - For Insert, `changed` is nil (first version, no previous state). The snapshot captures the initial document state.

  **3. Hook into Update** (modify `pkg/document/crud.go` lines 912–942):
  - After `insertOutbox` (line 939) and before `insertAuditLog` (line 942), add:
    ```go
    if mt.TrackChanges {
        changed := buildVersionDiff(doc, modifiedBeforeHooks)
        insertVersion(txCtx, tx, doctype, name, uid, changed, doc.AsMap())
    }
    ```
  - `buildVersionDiff()` reuses the same logic as `buildChangesJSON()` (`crud.go:1452-1475`) but returns a `map[string]any` instead of `[]byte`. The diff format is `{fieldName: {"old": value, "new": value}}`, excluding system fields (`modified`, `modified_by`).
  - `prevData` is already captured at line 838 (`prevData := doc.AsMap()` before `applyValues`). The "after" snapshot is `doc.AsMap()` post-hooks.

  **4. REST Endpoint** (add to `pkg/api/`):
  - `GET /api/v1/resource/{doctype}/{name}/versions` → returns version history.
  - Query params: `limit` (default 20), `offset` (default 0).
  - Response: `{"data": [{"name": "uuid", "owner": "user", "creation": "timestamp", "data": {...}}], "meta": {"total": N}}`.
  - Requires read permission on the doctype (uses existing `PermissionChecker`).
  - Register on the resource handler alongside existing CRUD routes.

- **Why this task exists:** Version tracking is a core business requirement for ERP-grade applications. Users need to see who changed what and when. The `tab_version` table already exists in the DDL (`ddl.go:190-203`) and `MetaType.TrackChanges` is already defined (`metatype.go:57`) — this task wires them together.
- **Dependencies:** None (independent of T1; both are backend tasks that can run in parallel)
- **Inputs / References:**
  - `pkg/meta/ddl.go` lines 190–203 (`tab_version` schema with `idx_version_ref` index)
  - `pkg/meta/metatype.go` line 57 (`TrackChanges bool` field)
  - `pkg/document/crud.go` lines 756–779 (Insert transaction block — insertion point after line 775)
  - `pkg/document/crud.go` lines 838, 901, 906, 912–942 (Update flow — prevData capture, changesJSON, outboxEvent, transaction block)
  - `pkg/document/crud.go` lines 1449–1475 (`buildChangesJSON` — reusable diff logic)
  - `pkg/api/rest.go` (ResourceHandler route registration pattern)
  - `apps/core/modules/core/doctypes/doctype/doctype.json` (example with `track_changes: true`)
- **Deliverable:**
  - `pkg/document/version.go` — `insertVersion()`, `buildVersionDiff()`, `fetchVersions()`, `VersionRecord` struct
  - Modified `pkg/document/crud.go` — version insertion in Insert (after line 775) and Update (after line 939) transactions
  - Version history API endpoint in `pkg/api/`
  - Unit tests: version diff computation, version record insertion
  - Integration test: save document with `track_changes: true`, verify version record created, fetch via API
- **Acceptance Criteria:**
  - Save a document with `track_changes: true` → `tab_version` contains a record with the correct `ref_doctype`, `docname`, and field-level diff in `data`
  - Update the same document → second version record created with `changed` diff showing old/new values
  - `GET /api/v1/resource/User/Administrator/versions` returns version history ordered by creation DESC
  - Documents with `track_changes: false` → no version records created
  - Version insertion is transactional (rolls back with the document write if either fails)
  - Insert (first save) creates a version with null `changed` and full `snapshot`
- **Risks / Unknowns:**
  - Storage growth: `tab_version` with full snapshots on every save could grow large for frequently-modified documents. Mitigation: add a configurable `max_versions_per_doc` (with cleanup job) as a follow-up. Not a blocker for initial implementation.
  - Child table versioning: should child table rows be included in the snapshot? Yes — `doc.AsMap()` already includes child data. The diff should at minimum capture parent-level changes; child-level field diffs are a nice-to-have.

---

### Task 3

- **Task ID:** MS-19-T3
- **Title:** Custom Field Type Registry & @osama1998h/desk Package Structure
- **Status:** Completed
- **Description:**
  Enable apps to register custom field types that the Desk renders, and restructure the desk exports as a workspace-local `@osama1998h/desk` npm package.

  **1. Backend: Custom Field Type Support** (modify `pkg/meta/fielddef.go`):
  - Currently `FieldType.IsValid()` (line 102) checks against the hardcoded `ValidFieldTypes` map (lines 53–89). Unknown field types fail validation, preventing apps from defining custom types.
  - Add a `CustomFieldTypes` registry (a `sync.RWMutex`-protected `map[FieldType]struct{}`) at the package level.
  - Add `RegisterCustomFieldType(ft FieldType)` function for apps to register custom types at startup via hooks.
  - Modify `IsValid()` to also check the custom registry.
  - Add `IsCustom() bool` method that returns true for types not in the built-in `ValidFieldTypes`.
  - In `columns.go` `ColumnType()` function (lines 14–53): custom field types default to `TEXT` column type (same storage as `Data`). The field_type string is purely metadata for frontend rendering — the backend just needs to know the SQL column type.

  **2. Frontend: Field Type Registry** (`desk/src/lib/fieldTypeRegistry.ts` — new file):
  - Module-level `Map<string, React.ComponentType<FieldProps>>` for custom field type components.
  - `registerFieldType(name: string, component: React.ComponentType<FieldProps>): void` — the public API that apps call.
  - `getCustomFieldType(name: string): React.ComponentType<FieldProps> | undefined` — internal lookup.
  - Import validation: warn in dev mode if a type name collides with a built-in type.

  **3. Frontend: FieldRenderer Fallback** (modify `desk/src/components/fields/FieldRenderer.tsx`):
  - At the component lookup (line 20), add fallback to custom registry:
    ```tsx
    const Component = FIELD_TYPE_MAP[fieldDef.field_type] ?? getCustomFieldType(fieldDef.field_type);
    ```
  - If neither built-in nor custom type found, render a `StubField` (already exists) with the type name.

  **4. Frontend: TypeScript Type Widening** (modify `desk/src/api/types.ts`):
  - The `FieldType` union (lines 4–41) is a closed set of 35 string literals. Widen to accept custom types:
    ```typescript
    type FieldType = "Data" | "Text" | ... | "Heading" | (string & {});
    ```
  - This preserves autocomplete for built-in types while accepting any string for custom types.

  **5. @osama1998h/desk Barrel Export** (`desk/src/index.ts` — new file):
  - Re-export the public API for app desk extensions:
    ```typescript
    export { registerFieldType } from './lib/fieldTypeRegistry';
    export type { FieldProps, LayoutFieldProps } from './components/fields/types';
    export { FieldRenderer } from './components/fields/FieldRenderer';
    // Hooks and providers that app extensions may need
    export { useAuth } from './providers/AuthProvider';
    export { useMetaType } from './providers/MetaProvider';
    export { useDocument, useDocList } from './providers/DocProvider';
    ```
  - Update `desk/package.json`: add `"name": "@osama1998h/desk"`, add `"exports": { ".": "./src/index.ts" }`.
  - Add a workspace root `package.json` (if not already present) with `"workspaces": ["desk"]` so that app directories can depend on `@osama1998h/desk` as a workspace package.

  **6. Build Pipeline Update** (modify `cmd/moca/build.go`):
  - Enhance `moca build desk` to discover app desk extensions:
    - Scan installed app directories for `desk/setup.ts` (or `desk/index.ts`) files.
    - Generate a synthetic entry file that imports each app's setup module before importing the main app.
    - This ensures `registerFieldType()` calls execute before the React app mounts.
  - The existing `moca build desk` runs `npx vite build` — the enhancement adds the app discovery and entry generation step before the Vite build.

- **Why this task exists:** Without `registerFieldType()`, the Desk is locked to 35 built-in field types. Apps cannot extend the UI, which breaks the three-layer composition model (§17.2). The `@osama1998h/desk` package structure is the import contract that all app extensions depend on.
- **Dependencies:** None (independent of T1 and T2)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §9.3 lines 1570–1580 (`registerFieldType` API)
  - `MOCA_SYSTEM_DESIGN.md` §17.2 lines 2134–2143 (three-layer composition, `moca build desk`)
  - `pkg/meta/fielddef.go` lines 53–117 (`ValidFieldTypes` map, `IsValid()`, `IsStorable()`)
  - `pkg/meta/columns.go` lines 14–53 (`ColumnType()` switch)
  - `desk/src/components/fields/types.ts` lines 37–85 (`FIELD_TYPE_MAP`)
  - `desk/src/components/fields/FieldRenderer.tsx` lines 1–45 (component dispatch)
  - `desk/src/api/types.ts` lines 4–41 (`FieldType` union)
  - `desk/package.json` (current package config)
  - `cmd/moca/build.go` (existing `moca build desk` command)
  - `docs/moca-cross-doc-mismatch-report.md` MISMATCH-025 lines 874–907 (`@osama1998h/desk` workspace-local)
- **Deliverable:**
  - Modified `pkg/meta/fielddef.go` — `RegisterCustomFieldType()`, updated `IsValid()`, `IsCustom()`
  - Modified `pkg/meta/columns.go` — custom type default to TEXT
  - `desk/src/lib/fieldTypeRegistry.ts` — runtime custom field type registry
  - Modified `desk/src/components/fields/FieldRenderer.tsx` — custom registry fallback
  - Modified `desk/src/api/types.ts` — widened `FieldType` union
  - `desk/src/index.ts` — barrel export for `@osama1998h/desk`
  - Modified `desk/package.json` — renamed to `@osama1998h/desk`, added exports
  - Modified `cmd/moca/build.go` — app extension discovery
  - Tests: register custom field type, verify it renders in FieldRenderer; backend custom type validation
- **Acceptance Criteria:**
  - `registerFieldType('TreeSelect', TreeSelectComponent)` makes TreeSelect available in FormView (per ROADMAP acceptance criteria)
  - `FieldRenderer` renders the custom component when `field_type: "TreeSelect"` in field definition
  - Backend accepts MetaType definitions with custom field types (validation passes)
  - Custom field types get `TEXT` SQL columns
  - `import { registerFieldType } from '@osama1998h/desk'` resolves correctly in app extension files
  - `moca build desk` discovers and includes app desk extensions in the build
- **Risks / Unknowns:**
  - App extension discovery at build time: apps must ship `.tsx` files found by `moca build desk`. The discovery convention (scanning for `desk/setup.ts` in app directories) needs to be documented.
  - TypeScript type widening with `(string & {})` trick: works in TypeScript 4.9+ (current project uses recent TS). Verify it doesn't break existing type checks.

---

### Task 4

- **Task ID:** MS-19-T4
- **Title:** Desk Real-Time UI & Version History Sidebar
- **Status:** Completed
- **Description:**
  Build the frontend WebSocket integration, real-time document update notifications, and the version history viewer.

  **1. WebSocketProvider** (`desk/src/providers/WebSocketProvider.tsx` — new file):
  - React context wrapping a WebSocket connection to `ws://{host}/ws?token={accessToken}`.
  - Get access token from `getAccessToken()` in `client.ts:42`.
  - Auto-reconnect with exponential backoff: 1s → 2s → 4s → 8s → max 30s. Reset backoff on successful connection.
  - Connection states: `connecting`, `connected`, `disconnected`, `reconnecting`.
  - Subscription API: `subscribe(doctype: string, callback: (event: DocEvent) => void): () => void` — returns unsubscribe function.
  - On message receive: parse JSON, dispatch to registered callbacks matching `event.doctype`.
  - Send `{"type":"subscribe","doctype":"X"}` when first callback registered for a doctype; `{"type":"unsubscribe","doctype":"X"}` when last callback removed.
  - Place in provider tree in `main.tsx` after `AuthProvider` (needs auth token), before route rendering.
  - Only connect when authenticated (`useAuth().isAuthenticated`).

  **2. useRealtimeDoc Hook** (`desk/src/hooks/useRealtimeDoc.ts` — new file):
  - `useRealtimeDoc(doctype: string, name: string)` — subscribes to real-time updates for a specific document.
  - Consumes `WebSocketProvider`'s subscribe API.
  - Filters events by `event.docname === name`.
  - Returns `{ lastEvent: DocEvent | null, isStale: boolean }`.

  **3. FormView Real-Time Integration** (modify `desk/src/pages/FormView.tsx`):
  - Use `useRealtimeDoc(doctype, name)` when viewing an existing document.
  - When a `doc_update` event arrives for the current document:
    - If the form is **clean** (no unsaved changes, from existing `useDirtyTracking`): auto-invalidate the document query via `queryClient.invalidateQueries(["doc", doctype, name])`. Show a brief toast: "Updated by {event.user}".
    - If the form is **dirty** (unsaved changes): show a persistent banner: "This document was modified by {event.user}. Reload to see changes." with a "Reload" button that discards local changes and refetches.
  - This satisfies the acceptance criterion: "User A saves SalesOrder; User B sees real-time refresh notification."

  **4. ListView Real-Time Integration** (modify `desk/src/pages/ListView.tsx`):
  - Subscribe to the current doctype via WebSocketProvider.
  - On any `doc_update`, `doc_created`, or `doc_deleted` event: invalidate the list query via `queryClient.invalidateQueries(["docList", doctype])`.
  - No toast needed for list view — just silent refresh.

  **5. useDocVersions Hook** (`desk/src/hooks/useDocVersions.ts` — new file):
  - `useDocVersions(doctype: string, name: string)` — fetches version history from T2's endpoint.
  - Uses TanStack Query: `GET /api/v1/resource/{doctype}/{name}/versions`.
  - Returns `{ versions: VersionRecord[], total: number, isLoading, error }`.
  - Only fetches when `metaType.track_changes === true`.

  **6. Version History Sidebar** (`desk/src/components/version/VersionHistory.tsx` — new file):
  - Slide-out panel triggered by a "History" button in FormView's title bar.
  - Only shown when `metaType.track_changes === true`.
  - Timeline view: each entry shows user avatar/name, timestamp (relative: "2 hours ago"), and a summary of changed fields.
  - Click to expand: shows the full field-level diff (`changed` from version data) — field name, old value, new value.

  **7. Field Diff Viewer** (`desk/src/components/version/FieldDiff.tsx` — new file):
  - Renders a single field's old → new value change.
  - Uses the field's label (from MetaType) for display.
  - Color-coded: red for old (removed), green for new (added).
  - Handles different value types: strings (text diff), numbers, booleans, dates.

  **8. Vite Dev Proxy** (modify `desk/vite.config.ts`):
  - Add `/ws` proxy for WebSocket in dev mode:
    ```typescript
    "/ws": { target: "ws://localhost:8000", ws: true }
    ```

- **Why this task exists:** This is the integration task that delivers the user-facing real-time and version tracking experience. Without this, the backend work from T1 and T2 has no visible impact.
- **Dependencies:** MS-19-T1 (WebSocket endpoint must exist), MS-19-T2 (version API must exist)
- **Inputs / References:**
  - `MOCA_SYSTEM_DESIGN.md` §9.4 lines 1582–1600 (client WebSocket flow)
  - `desk/src/providers/AuthProvider.tsx` (token access via `getAccessToken`)
  - `desk/src/providers/DocProvider.tsx` (React Query patterns to follow)
  - `desk/src/pages/FormView.tsx` lines 60–97 (existing hooks: useMetaType, useDocument, useDirtyTracking)
  - `desk/src/pages/ListView.tsx` lines 126–180 (existing hooks: useMetaType, useDocList)
  - `desk/src/hooks/useDirtyTracking.ts` (dirty state for conditional real-time behavior)
  - `desk/src/main.tsx` (provider tree — add WebSocketProvider)
  - `desk/src/api/client.ts` line 42 (`getAccessToken()`)
  - `desk/vite.config.ts` (add /ws proxy)
- **Deliverable:**
  - `desk/src/providers/WebSocketProvider.tsx` — WebSocket context with auto-reconnect
  - `desk/src/hooks/useRealtimeDoc.ts` — per-document real-time subscription
  - `desk/src/hooks/useDocVersions.ts` — version history data hook
  - `desk/src/components/version/VersionHistory.tsx` — version timeline sidebar
  - `desk/src/components/version/FieldDiff.tsx` — field-level diff viewer
  - Modified `desk/src/pages/FormView.tsx` — real-time notification + history button
  - Modified `desk/src/pages/ListView.tsx` — real-time list refresh
  - Modified `desk/src/main.tsx` — WebSocketProvider in provider tree
  - Modified `desk/vite.config.ts` — /ws proxy
- **Acceptance Criteria:**
  - User A saves SalesOrder; User B (viewing the same SalesOrder in FormView) sees a notification within 2 seconds
  - If User B has unsaved changes, they see a "modified by User A" banner with reload option (not auto-refresh)
  - If User B has no unsaved changes, the form auto-refreshes
  - WebSocket reconnects after network interruption (disconnect WiFi → reconnect → subscription resumes)
  - Version history sidebar shows timeline of changes for documents with `track_changes: true`
  - Each version entry shows who changed it, when, and which fields changed (with old/new values)
  - Version history button is hidden for doctypes with `track_changes: false`
  - ListView silently refreshes when documents are created/updated/deleted by other users
- **Risks / Unknowns:**
  - React Query cache invalidation timing: `invalidateQueries` triggers a refetch, but there may be a brief flicker. Use `setQueryData` for optimistic updates if the WebSocket event includes the full document data.
  - WebSocket reconnection UX: need to show a subtle indicator when disconnected (e.g., a small dot in the topbar). Don't block the UI.
  - Version history for documents with many versions: paginate the sidebar (load 20 at a time, "Load more" button).

## Recommended Execution Order

1. **MS-19-T1 and MS-19-T2 in parallel** — Both are independent backend tasks. T1 (WebSocket hub) and T2 (version tracking) have no code overlap. Running them in parallel cuts the critical path.
2. **MS-19-T3 concurrently** — Custom field registry (backend + frontend) is independent of T1 and T2. The backend part (fielddef.go) is small. The frontend part (@osama1998h/desk package) can start immediately.
3. **MS-19-T4 last** — Depends on T1 (WebSocket endpoint) and T2 (version API). This is the integration task that ties everything together and delivers the user-visible features.

```
Week 1-2:  T1 (WebSocket Hub)  ──────────────┐
           T2 (Version Tracking) ─────────────┤
           T3 (Custom Fields + @osama1998h/desk) ───┤
                                              ▼
Week 3:    T4 (Real-Time UI + Version History)
```

## Open Questions

- **Child table version diffs**: Should version tracking include field-level diffs for child table rows (e.g., "Row 3 of Items: qty changed from 5 to 10"), or only parent-level fields? Parent-only is simpler; child diffs require tracking row identity across saves. Recommend starting with parent-only and adding child diffs as a follow-up.

- **Max versions per document**: Should there be a configurable limit on how many version records are kept per document? Recommend adding a `max_versions` config (default: 100) with a cleanup job, but implement this as a follow-up — not a blocker for MS-19.

- **WebSocket event payload size**: Should the WebSocket event include the full document data (enabling optimistic cache updates without a refetch), or just the document name + changed field names? Full data is more efficient (saves a round-trip) but increases WebSocket bandwidth. Recommend: include changed field names in the event, and let the client decide whether to refetch or use the data.

## Out of Scope for This Milestone

- Typing indicators / collaborative editing (explicitly excluded in ROADMAP scope)
- WebSocket scaling optimizations for 50k+ connections (future, if needed)
- Version comparison between two arbitrary versions (nice-to-have, follow-up)
- Child table field-level version diffs (follow-up)
- Max version cleanup job (follow-up)
- Portal/SSR real-time (MS-27)
- Dashboard/Report real-time (MS-20)

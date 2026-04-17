# DocType Builder Entry Flow — Design

**Date:** 2026-04-17
**Author:** osama1998H (+ Claude)
**Status:** Draft — awaiting implementation plan
**Related:** [`2026-04-13-doctype-builder-design.md`](2026-04-13-doctype-builder-design.md)

## Problem

Opening `/desk/app/doctype-builder` drops the user directly onto an empty editing canvas. There is no prompt to choose whether they want to **create a new** DocType or **edit an existing** one, and no list of what exists to edit. Deciding what to do first happens implicitly by typing into the toolbar's name input — an awkward onboarding experience that also makes it easy to accidentally start editing nothing.

## Goal

Replace the current "empty canvas on arrival" behavior with an entry modal that makes the two primary intents explicit:

1. **Create a new DocType** — collect name, app, module, and kind up-front; drop into a canvas pre-populated with that metadata.
2. **Edit an existing DocType** — pick from a searchable list of all doctypes; deep-link to the canvas for that doctype.

Deep-links (`/doctype-builder/:name`) continue to bypass the modal entirely — they're a decisive action.

## Non-goals

- No change to the canvas itself (fields, drawers, permissions, preview mode).
- No change to the backend write endpoints (`POST`/`PUT`/`DELETE /dev/doctype/{name}`).
- No filtering of "system" doctypes from the list — search is the only way to narrow results for v1.
- No autosave; no creation of doctypes before the user completes the canvas save.

## User flow

### Entry paths

| Entry | Behavior |
|---|---|
| Sidebar "DocType Builder" link → `/desk/app/doctype-builder` | Blank canvas in background, **non-dismissible** `DocTypeEntryDialog` modal on top. |
| Deep-link `/desk/app/doctype-builder/:name` | Modal is **not** shown. Canvas hydrates the named doctype. (Unchanged from today.) |

### Modal state machine

```
             ┌──────────────────────┐
             │   Landing            │
             │  ┌─────┐   ┌─────┐   │
             │  │ New │   │Open │   │
             │  └──┬──┘   └──┬──┘   │
             └─────┼─────────┼──────┘
                   │         │
         ┌─────────▼──┐   ┌──▼────────┐
         │ Create form│   │ Open list │
         │ [← back]   │   │ [← back]  │
         │ [Save]     │   │ [click] → │
         └─────┬──────┘   └────┬──────┘
               │               │
               ▼               ▼
         store.stageNew(   navigate(
         {name, app,       `/doctype-builder/
          module,           ${name}`)
          settings});
         close modal
```

The modal is **non-dismissible**: no X button, Escape and overlay-click are no-ops. Exit is via sidebar navigation (which unmounts the page).

### Happy path A — create

1. User clicks **DocType Builder** in the sidebar.
2. Modal opens on **Landing**.
3. User clicks **Create New DocType** card → modal switches to Create form.
4. User enters Name (inline availability check), picks App, picks Module, picks Type.
5. User clicks **Save & Continue** → `stageNew()` fires, modal closes.
6. Canvas is now pre-populated with name/app/module/flags; fields panel is empty.
7. User drags fields, edits settings, eventually hits Cmd+S (toolbar **Save**) → normal POST flow.

### Happy path B — open existing

1. User clicks **DocType Builder** in the sidebar.
2. Modal opens on **Landing**.
3. User clicks **Edit Existing DocType** card → modal switches to Open list.
4. List fetches from `GET /api/v1/dev/doctype`.
5. User types in the search field; list filters client-side.
6. User clicks a row → `navigate("/desk/app/doctype-builder/{name}")` → page re-renders with `:name` in URL → deep-link path hydrates the canvas; modal unmounts.

### Deep-link

`/desk/app/doctype-builder/:name` skips the modal entirely and hydrates the canvas, as today.

## Components

### New frontend files (under `desk/src/components/doctype-builder/entry/`)

| File | Responsibility |
|---|---|
| `DocTypeEntryDialog.tsx` | Top-level modal. Owns view state `"landing" \| "create" \| "open"`. Wraps shadcn `Dialog` with Escape/click-outside disabled. |
| `EntryLanding.tsx` | Two-card screen. Two buttons → set view. |
| `CreateDocTypeForm.tsx` | Create form (fields below). Calls `onStageNew(payload)` on Save & Continue. |
| `DocTypeList.tsx` | Fetches and renders list. Calls `onOpen(name)` on row click. |

### Changed files

| File | Change |
|---|---|
| `desk/src/pages/DocTypeBuilder.tsx` | Add local `showEntryDialog` state initialized to `!urlName`; sync with URL via `useEffect(() => { if (urlName) setShowEntryDialog(false); }, [urlName])` so clicking a list row (which calls `navigate(/doctype-builder/:name)`) closes the modal on the next render; render `<DocTypeEntryDialog />` when true; wire `onStageNew` → `store.stageNew()` + close; wire `onOpen` → `navigate()`. |
| `desk/src/stores/doctype-builder-store.ts` | New action `stageNew(payload)` — see Store below. |
| `desk/src/api/types.ts` | Add `DocTypeListItem` type. |
| `pkg/api/dev_handler.go` | New `HandleListDocTypes` + registration in `RegisterDevRoutes`. |

## Create form

| Field | Control | Validation | Default |
|---|---|---|---|
| Name | text input | Required. Must satisfy `ValidateDocTypeName` — starts with an uppercase ASCII letter, contains only ASCII letters and digits (no spaces, underscores, or hyphens). Inline availability check: debounced 300 ms `GET /api/v1/dev/doctype/{name}` — 404 = available ✓, 200 = taken ✗, other → no indicator. | empty |
| App | `<select>` | Required. Populated from `GET /api/v1/dev/apps`. | unset |
| Module | `<select>` | Required. Disabled until App selected. Populated from the selected app's `modules`. Cleared when App changes. | unset |
| Type | `<select>` (single-select dropdown) | Required. Options: `Normal`, `Submittable`, `Single`, `Child Table`, `Virtual`. Maps to exactly one of the four type flags being `true` (Normal = all `false`). Mutually exclusive by construction. | `Normal` |

Short help text under Type explains each option (one line per kind).

**Buttons:**
- `← Back` (top-left) — returns to Landing, clears form state.
- `Save & Continue` (primary, bottom-right) — disabled until Name + App + Module are valid. Fires `onStageNew({name, app, module, settings})` where settings is:
  ```ts
  {
    naming_rule: { rule: "uuid" },
    title_field: "",
    sort_field: "",
    sort_order: "desc",
    search_fields: [],
    image_field: "",
    is_submittable: type === "Submittable",
    is_single:      type === "Single",
    is_child_table: type === "Child Table",
    is_virtual:     type === "Virtual",
    track_changes:  true,
  }
  ```

## Open list

### Layout

```
┌────────────────────────────────────────┐
│ ← Back        Open Existing DocType    │
├────────────────────────────────────────┤
│  🔍 Search...                          │
├────────────────────────────────────────┤
│  User                       · core · frappe │
│  Customer       Submittable · crm  · sales  │
│  Order Line     Child Table · crm  · sales  │
│  Invoice                    · billing · acc │
│  ...                                        │
└────────────────────────────────────────┘
```

### List item

- Primary: `name`
- Kind badge (muted pill) if `is_submittable` / `is_single` / `is_child_table` / `is_virtual` is true
- Secondary (dimmed): `module · app`
- Entire row is clickable

### Search

- Single `<input>`, debounced 150 ms
- Client-side filter — case-insensitive substring match across `name`, `module`, `app`
- Empty query = show all rows
- No matches = `"No doctypes match \"{query}\""` placeholder
- Search input autofocuses on list mount

### Empty state (no doctypes exist at all)

- Message: `"No DocTypes yet. Create your first one."`
- Inline **Create New** button that returns to Landing

### Keyboard

- Arrow keys navigate rows
- Enter selects
- Escape is a no-op (modal is non-dismissible)

## Store changes (`doctype-builder-store.ts`)

New action:

```ts
stageNew: (payload: {
  name: string;
  app: string | null;
  module: string;
  settings: DocTypeSettings;
}) => void;
```

Semantics:
- Internally calls `reset()` to wipe any prior state.
- Sets `name`, `app`, `module`, `settings` from payload.
- `isNew = true` (distinguishes from `hydrate()`, which sets `isNew = false`).
- `isDirty = false` — user hasn't made changes yet. `isDirty` flips to `true` on the first canvas edit, which is the correct signal for the beforeunload warning.
- Layout stays at default (one "Details" tab, one section, one column, no fields).
- `fields = {}`, `permissions = [DEFAULT_PERMISSION]`.

## Backend changes (`pkg/api/dev_handler.go`)

New handler `HandleListDocTypes`:
- Walks `{appsDir}/*/modules/*/doctypes/*/*.json` (mirrors how `HandleCreateDocType` writes files).
- For each file, unmarshal just the fields needed for the response shape:
  ```json
  {
    "name": "User",
    "app": "frappe",
    "module": "core",
    "is_submittable": false,
    "is_single": false,
    "is_child_table": false,
    "is_virtual": false
  }
  ```
- Malformed JSON files are skipped (logged at debug, not surfaced to the user).
- Empty directory tree returns `{ "data": [] }`.
- Returns `{ "data": [...] }`.

New registration in `RegisterDevRoutes`:

```go
mux.Handle("GET "+p+"/doctype", wrap(h.HandleListDocTypes))
```

Sits alongside the existing `POST`/`GET {name}`/`PUT {name}`/`DELETE {name}` routes. Admin-only via the already-applied `DevAuthMiddleware`.

New type:

```go
type DocTypeListItem struct {
    Name          string `json:"name"`
    App           string `json:"app"`
    Module        string `json:"module"`
    IsSubmittable bool   `json:"is_submittable"`
    IsSingle      bool   `json:"is_single"`
    IsChildTable  bool   `json:"is_child_table"`
    IsVirtual     bool   `json:"is_virtual"`
}
```

## Frontend API types (`desk/src/api/types.ts`)

```ts
export interface DocTypeListItem {
  name: string;
  app: string;
  module: string;
  is_submittable: boolean;
  is_single: boolean;
  is_child_table: boolean;
  is_virtual: boolean;
}
```

## Error handling

| Condition | Behavior |
|---|---|
| `GET /dev/doctype` fails | Show inline error card in the list view: `"Couldn't load doctypes. Retry"` with a retry button. No toast — the modal is already in focus. |
| `GET /dev/apps` fails on Create form | Disable App dropdown, show small error message beneath it. User cannot proceed. |
| Name availability check errors (network, non-404/200 response) | Show no indicator. Don't block submit — server-side validation runs at final canvas save. |
| `onStageNew` called twice due to double-click | Idempotent — `reset()` then overwrite. Second click is effectively a no-op after modal closes. |
| User navigates away mid-creation (sidebar link) | Modal unmounts. `store.reset()` runs via the existing `!urlName` effect on next mount. No persistence of in-progress form state. |

## Testing strategy

### Backend — `pkg/api/dev_handler_test.go` + `dev_api_integration_test.go`

- **Unit** (`dev_handler_test.go`): fake `appsDir` tmp tree with 2 valid doctypes in 2 modules in 2 apps — verify response shape, guest → 403, admin → 200.
- **Edge cases**: empty `appsDir` → `{data: []}`; malformed JSON file in one doctype dir → that one skipped, rest returned; doctype with missing `name` field → skipped.
- **Integration** (`//go:build integration`): create via `POST`, list via `GET /dev/doctype`, assert it appears; delete, assert it disappears.

### Frontend store — `desk/src/stores/doctype-builder-store.test.ts` (new)

- `stageNew()` resets prior state, populates name/app/module/settings, `isNew: true`, `isDirty: false`.
- `stageNew()` leaves layout at default (one tab, one section, one column, zero fields).
- `stageNew()` followed by `addField()` preserves `isNew: true` — the first canvas save is a POST.

### Frontend components (co-located `.test.tsx`)

- `CreateDocTypeForm.test.tsx`:
  - Save button disabled until Name + App + Module are all valid.
  - Type dropdown: `"Submittable"` → `{is_submittable: true, is_single: false, is_child_table: false, is_virtual: false}`; `"Normal"` → all four false.
  - Name availability: mock 404 → ✓ indicator; 200 → ✗; button disabled when taken.
  - Changing App clears the Module value.
- `DocTypeList.test.tsx`:
  - Renders list from mocked API response; shows kind badges for flagged rows.
  - Search filters client-side across name/module/app (case-insensitive).
  - Empty-results placeholder renders for non-matching query.
  - Empty-state placeholder renders when API returns `[]`.
  - Row click calls `navigate` with `/desk/app/doctype-builder/{name}`.
- `DocTypeEntryDialog.test.tsx`:
  - Escape and overlay-click do nothing (non-dismissible).
  - Back arrow returns to Landing from both Create and Open views.
  - View-switching via Landing cards updates internal state.

### Manual / e2e verification notes (for reviewer)

- **Happy path A**: sidebar → builder → Create New → fill form → Save & Continue → canvas populated → add one field → Cmd+S → verify JSON file written under `apps/{app}/modules/{module}/doctypes/{slug}/{slug}.json` and doctype registered.
- **Happy path B**: sidebar → builder → Open Existing → search → click → canvas hydrated with correct fields.
- **Deep-link**: paste `/desk/app/doctype-builder/User` into address bar → modal does not appear, canvas loads User.
- **Abandon**: open builder, click Create New, fill form, hit sidebar "Home" → no disk write; reopen builder → fresh blank modal.

## Documentation updates

Per `CLAUDE.md`, every code change requires documentation updates:

- Update `docs/superpowers/specs/2026-04-13-doctype-builder-design.md` with a cross-reference to this entry-flow design.
- Update `docs/MOCA_SYSTEM_DESIGN.md` DocType Builder section if it describes the "direct to canvas" flow.
- Update `wiki/` — the page covering the DocType Builder (if present) needs the new entry flow documented; commit inside the submodule and update the submodule pointer.
- No `CLAUDE.md` update needed — this change doesn't touch project structure, build commands, tech stack, or top-level architecture.

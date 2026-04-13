# DocType Builder — Design Specification

**Date:** 2026-04-13
**Status:** Draft
**Scope:** Visual drag-and-drop DocType Builder for Moca Desk, tree-native MetaType storage model, reusable BuilderKit framework

---

## 1. Overview

The DocType Builder is a canvas-first, drag-and-drop visual editor in the Moca Desk that enables developers and power-user admins to create and edit DocTypes without writing JSON by hand. It provides a schematic editing mode for building (showing field names, types, and structure at a glance) and a live preview mode that renders the actual form.

The feature comprises three layers:
1. **Tree-native storage model** — a new JSON format for MetaType field layout, replacing the flat delimiter-based field list
2. **BuilderKit** — a reusable shell framework (panels, drawers, undo/redo, DnD primitives) consumed by this builder and future builders (workflow, report)
3. **DocType Builder UI** — the builder page itself, the first consumer of BuilderKit

### Design Decisions Summary

| Decision | Choice | Rationale |
|---|---|---|
| Target users | Developer + power-user/admin | Developers create DocTypes for apps; admins customize on deployed sites |
| Storage model | Tree-native | Eliminates Frappe's flat-list nesting limitations; Moca is pre-v1.0 so now is the time |
| Builder scope | All-in-one | Layout + settings + permissions unified in one builder experience |
| Reusable foundation | Shared BuilderKit shell | Canvas, sidebar, undo/redo, DnD primitives shared; canvas content is builder-specific |
| Canvas mode | Dual (schematic + live preview) | Schematic optimized for editing; preview closes WYSIWYG gap |
| UI approach | Canvas-first with slide-out drawers | Maximizes canvas space; icon rail provides quick access to field palette, settings, permissions |
| Component library | shadcn/ui first | Use existing shadcn/ui primitives (Tabs, Sheet, Button, Input, Select, etc.); custom only for builder-specific interactions |

### Frappe Form Builder — Lessons Learned

Research on Frappe Framework v15/v16 Form Builder (the visual DocType field editor):

**What works well:**
- Split-pane WYSIWYG with property sidebar
- Inline label editing via double-click
- Undo/redo via debounced ref history
- Keyboard shortcuts (Ctrl+Shift+N to add, Backspace to delete)

**Root-cause limitations (from flat-list storage):**
- No nested sections — flat delimiter model cannot represent nesting
- No column width control — columns are always equal width
- Max ~4 columns per section before unexpected behavior
- Layout tree must be reconstructed at render time from flat list, and flattened back on save

**Stability issues:**
- Silent save failures (`TypeError: doc.parentfield is undefined`)
- Freeze on save when adding custom fields
- `depends_on` only works for first tab, not subsequent tabs
- `fetch_from` breaks when fetching 2 fields from the same DocType

**Moca's advantage:** By adopting tree-native storage, we eliminate the entire class of flat-list limitations. By building on React 19 + dnd-kit (vs Frappe's Vue 3 + vuedraggable), we get better accessibility and React 19 compatibility.

---

## 2. Two-Phase Delivery

| Phase | Scope | Dependency |
|---|---|---|
| **Phase 1: Storage & Backend** | Tree-native MetaType storage format, flat field extraction, flat-to-tree migration utility, dev-mode file-write API, meta endpoint changes | None — backend work |
| **Phase 2: Builder UI** | BuilderKit framework, DocType Builder page, schematic canvas, property panels, live preview, permissions editor | Phase 1 APIs must be stable |

---

## 3. Tree-Native Storage Model

### Current Format (flat with delimiters)

```json
{
  "name": "Book",
  "fields": [
    { "name": "tab_details", "field_type": "TabBreak", "label": "Details" },
    { "name": "sec_basic", "field_type": "SectionBreak", "label": "Basic Info" },
    { "name": "col_1", "field_type": "ColumnBreak" },
    { "name": "title", "field_type": "Data", "label": "Title", "required": true },
    { "name": "author", "field_type": "Link", "options": "Author" },
    { "name": "col_2", "field_type": "ColumnBreak" },
    { "name": "isbn", "field_type": "Data", "label": "ISBN" },
    { "name": "price", "field_type": "Currency", "label": "Price" }
  ]
}
```

### New Tree-Native Format

```json
{
  "name": "Book",
  "layout": {
    "tabs": [
      {
        "label": "Details",
        "sections": [
          {
            "label": "Basic Info",
            "collapsible": false,
            "columns": [
              {
                "width": 2,
                "fields": ["title", "author"]
              },
              {
                "width": 1,
                "fields": ["isbn", "price"]
              }
            ]
          }
        ]
      }
    ]
  },
  "fields": {
    "title": { "field_type": "Data", "label": "Title", "required": true },
    "author": { "field_type": "Link", "label": "Author", "options": "Author" },
    "isbn": { "field_type": "Data", "label": "ISBN" },
    "price": { "field_type": "Currency", "label": "Price" }
  }
}
```

### Key Design Decisions

1. **`layout` and `fields` are separate.** The `layout` tree describes visual arrangement (tabs > sections > columns > field references by name). The `fields` map holds field definitions keyed by name. This separation means:
   - Layout changes (reordering, adding sections) never touch field definitions
   - Field property changes never touch layout
   - The flat field list is trivially derived: iterate the fields map

2. **Column `width` is relative.** Width values are proportional (like CSS flex). `[{width: 2}, {width: 1}]` renders as 2/3 + 1/3. `[{width: 1}, {width: 1}]` renders as 1/2 + 1/2. This solves Frappe's equal-width-only limitation.

3. **No more delimiter pseudo-fields.** SectionBreak, ColumnBreak, TabBreak are eliminated from the field type enum. They exist only as structural nodes in the `layout` tree. The 35 field types become 32 storage types + 3 non-storage display types (HTML, Button, Heading).

### Backend Flat Extraction

Tree-native storage does NOT eliminate the need for flat field iteration. The backend needs a flat list for DDL generation, validation, API serialization, search indexing, and query filters. The `ExtractFields()` method derives this:

```go
func (mt *MetaType) ExtractFields() []FieldDef {
    var fields []FieldDef
    for _, tab := range mt.Layout.Tabs {
        for _, section := range tab.Sections {
            for _, column := range section.Columns {
                for _, fieldName := range column.Fields {
                    if fd, ok := mt.Fields[fieldName]; ok {
                        fields = append(fields, fd)
                    }
                }
            }
        }
    }
    return fields
}
```

Existing backend code (DDL in `pkg/orm`, validation in `pkg/document`, API in `pkg/api`, search in `pkg/search`) calls `ExtractFields()` instead of reading the field slice directly. This is the inverse of Frappe's approach: Frappe stores flat and reconstructs tree at render time; Moca stores tree and extracts flat when the backend needs it.

### Migration Utility

A `moca migrate-fields` CLI command converts existing flat JSON files to tree-native format:
- Walks all `*.json` DocType files in `pkg/builtin/core/` and `apps/*/`
- Parses flat `[]FieldDef` using the same logic as `desk/src/utils/layoutParser.ts` (ported to Go)
- Writes back as tree-native JSON
- Idempotent — running on already-migrated files is a no-op

### Meta API Response

`GET /api/v1/meta/{doctype}` returns both formats for backward compatibility:

```json
{
  "name": "Book",
  "layout": { "tabs": [...] },
  "fields": { "title": {...}, "author": {...} },
  "fields_ordered": [{ "name": "title", ... }, { "name": "author", ... }]
}
```

- `layout` + `fields`: used by builder and FormView for tree-native rendering
- `fields_ordered`: flat list in layout order, backward compatible for ListView, filters, search

---

## 4. BuilderKit Framework

A reusable shell framework that provides structural UI and shared systems. The DocType Builder is its first consumer; future builders (workflow, report) plug their own canvas into the same shell.

### What BuilderKit Provides

| Component | Purpose | Implementation |
|---|---|---|
| `BuilderShell` | Top bar + icon rail + drawer system + main canvas area | shadcn `Sheet` for drawers, custom icon rail |
| `PropertyPanel` | Right-side slide-out panel, renders property forms from a schema definition | shadcn `Sheet` + `Input`/`Select`/`Switch`/`Checkbox` |
| `DrawerPanel` | Left-side slide-out drawers triggered by icon rail buttons | shadcn `Sheet` |
| `CommandHistory` | Undo/redo stack using the command pattern | Custom hook: `useCommandHistory()` |
| `DragPrimitives` | Hooks wrapping `@dnd-kit/core` for drag-and-drop interactions | `@dnd-kit/core` + `@dnd-kit/sortable` |
| `BuilderToolbar` | Top bar with name, breadcrumbs, save/preview buttons | shadcn `Button`, `Input`, `DropdownMenu` |

### What BuilderKit Does NOT Provide

| Component | Reason |
|---|---|
| Canvas content | DocType canvas is hierarchical (tabs > sections > columns). Workflow canvas is a directed graph. Completely different rendering models. |
| Node types | DocType has fields, sections, tabs. Workflow has states, transitions. Different schemas. |
| Validation logic | DocType validates field names and types. Workflow validates state machine rules. |
| Persistence layer | DocType saves to MetaType API. Workflow saves to Workflow API. |

### Shell Layout

```
+--------------------------------------------------+
|  BuilderToolbar                                  |
|  [icon] Name          [Preview] [Save] [...]     |
+----+---------------------------+-----------------+
|    |                           |                 |
| I  |                           |  PropertyPanel  |
| c  |  children (canvas)        |  (slide-out)    |
| o  |                           |                 |
| n  |  <- provided by consumer  |  <- schema-     |
|    |                           |     driven      |
| R  |                           |                 |
| a  |                           |                 |
| i  |                           |                 |
| l  |                           |                 |
|    |                           |                 |
+----+---------------------------+-----------------+
|  Status bar: unsaved changes, field count        |
+--------------------------------------------------+
```

### Command History (Undo/Redo)

Uses the command pattern. Every mutation is an object with `execute()` and `undo()`:

```typescript
interface Command {
  id: string;
  label: string;          // "Add field: title", "Move section: Basic Info"
  execute(): void;
  undo(): void;
}

interface CommandHistory {
  execute(cmd: Command): void;
  undo(): void;
  redo(): void;
  canUndo: boolean;
  canRedo: boolean;
  history: Command[];
}
```

Each builder creates domain-specific commands (e.g., `AddFieldCommand`, `MoveFieldCommand` for DocType; `AddStateCommand`, `AddTransitionCommand` for Workflow). The history stack is generic.

### PropertyPanel — Schema-Driven

The PropertyPanel renders forms based on a property schema provided by the builder:

```typescript
interface PropertySchema {
  sections: PropertySection[];
}

interface PropertySection {
  label: string;
  collapsed?: boolean;
  properties: PropertyDef[];
}

interface PropertyDef {
  key: string;            // "label", "required", "options"
  label: string;
  type: "text" | "number" | "boolean" | "select" | "code" | "link";
  options?: string[];     // For select type
  dependsOn?: (values: Record<string, any>) => boolean;
  description?: string;
}
```

Each builder supplies different schemas for its node types. The panel is fully reusable.

### DnD Library: @dnd-kit/core

- Modern React DnD library, actively maintained
- Keyboard accessibility out of the box
- Supports nested sortable containers (tabs > sections > columns > fields)
- Tree-friendly — designed for hierarchical drag targets
- Better React 19 compatibility than deprecated alternatives (react-beautiful-dnd)

---

## 5. DocType Builder UI

### Route

`/desk/app/doctype-builder/{name?}` — new route. No `name` parameter for creating a new DocType.

### Navigation Entry Points

- **Sidebar:** Under the "Core" module, a "DocType Builder" link appears alongside existing DocType entries
- **ListView:** The DocType ListView gets a "New DocType" button that navigates to `/desk/app/doctype-builder` (the builder) instead of the standard FormView
- **FormView:** When viewing an existing DocType document, an "Edit in Builder" button in the toolbar navigates to `/desk/app/doctype-builder/{name}`
- **Command Palette:** Cmd+K search includes "New DocType" and "DocType Builder" as actions

### Top Bar (BuilderToolbar)

- DocType name (editable inline)
- App selector dropdown (which app to save to — dev mode only)
- Module selector
- Mode toggle: `Schematic | Preview`
- Save button with unsaved-changes indicator

### Icon Rail — Three Drawers

| Icon | Drawer | Contents |
|---|---|---|
| `+` Fields | **Field Palette** — Categorized grid of draggable field types. Categories: Text (Data, Text, LongText, Markdown, Code), Number (Int, Float, Currency, Percent), Date/Time (Date, Datetime, Time, Duration), Selection (Select, Link, DynamicLink), Relations (Table, TableMultiSelect), Media (Attach, AttachImage, Barcode, Color, Signature), Interactive (Check, Rating, Button), Display (HTML, Heading, JSON, HTMLEditor). Drag a field type onto the canvas to add it. |
| Settings | **DocType Settings** — Form for naming rule (+ pattern/field companion), title field, sort field/order, search fields, image field, is_submittable, is_single, is_child_table, is_virtual, track_changes. Standard shadcn form components. |
| Shield | **Permissions** — Permission matrix editor. Rows = roles, columns = read/write/create/delete/submit/cancel. Add role button. Per-field permissions (FLS) accessible from individual field properties. |

### Canvas — Schematic Mode (Default)

The canvas renders the layout tree directly. Each level of the tree is a visual container:

**Tab bar:** Horizontal tabs at the top of the canvas. Each tab is a sortable item. `+ Tab` button at the end. Tabs use shadcn `Tabs` component.

**Sections:** Bordered containers within a tab. Each section has:
- Header: label (inline editable), collapsible toggle, context menu (delete, duplicate, move to tab)
- Body: contains columns side by side
- Footer: `+ Add Column` button
- Sections are sortable within and across tabs via drag-and-drop

**Columns:** Flex containers within a section. Each column has:
- Width indicator showing relative proportion
- Resize handle between adjacent columns (drag to adjust width ratio)
- Contains field cards in a vertical sortable list
- Footer: `+ Add Field` button

**Field cards:** Compact cards showing:
- Drag handle (grip icon)
- Field name
- Required indicator (*)
- Field type badge
- Link target (for Link/Table fields, e.g., "Link -> Author")
- Click to select -> opens PropertyPanel on the right

### Canvas — Preview Mode

Toggles the canvas to render the actual FormView using existing field components (`FieldRenderer`, `DataField`, `SelectField`, etc.) with placeholder/demo data. Reads the layout tree directly — no flat-to-tree conversion needed.

### Drag-and-Drop Operations

| Action | Mechanism |
|---|---|
| Add field from palette | Drag field type card from left drawer -> drop into column |
| Add field inline | Click `+ Add Field` -> field type picker popover (shadcn `Command`) |
| Reorder fields | Drag field card within or across columns |
| Move sections | Drag section header within or across tabs |
| Resize columns | Drag divider between columns to adjust width ratio |
| Add tab/section/column | Click `+` buttons at each level |
| Delete | Select -> Backspace key, or context menu |
| Duplicate | Context menu on field/section |

### Property Panel (Right Side)

When a field is selected, shows grouped properties:

- **Basic**: Label, Field Name (auto-generated from label, editable), Field Type selector
- **Data**: Options, Default Value, Max Length, Min/Max Value
- **Validation**: Required, Unique, Validation Regex
- **Display**: Read Only, Hidden, Depends On, In List View, In Filter, In Preview
- **Search**: Searchable, Filterable, DB Index, Full Text Index
- **API**: In API, API Read Only, API Alias
- **Advanced**: Width hint, Custom Validator

Properties are filtered by field type:
- "Options" shows a DocType Link picker for Link/Table fields
- "Options" shows a newline-separated textarea for Select fields
- "Options" is hidden for Data/Int/etc. fields
- "Max Length" only shows for text-based fields
- "Min/Max Value" only shows for numeric fields

When a section is selected: label, collapsible, collapsed by default.
When a column is selected: width.
When a tab is selected: label.

### Keyboard Shortcuts

| Shortcut | Action |
|---|---|
| `Ctrl+S` / `Cmd+S` | Save |
| `Ctrl+Z` / `Cmd+Z` | Undo |
| `Ctrl+Shift+Z` / `Cmd+Shift+Z` | Redo |
| `Backspace` / `Delete` | Delete selected node |
| `Ctrl+D` / `Cmd+D` | Duplicate selected node |
| `Escape` | Deselect / close drawer |
| `Tab` | Move selection to next field |
| `Shift+Tab` | Move selection to previous field |

---

## 6. Backend API

### Dev-Mode API (New Endpoints)

These endpoints write DocType definitions to app files on disk. Only available when developer mode is enabled.

```
POST   /api/v1/dev/doctype           Create new DocType
PUT    /api/v1/dev/doctype/{name}    Update existing DocType
GET    /api/v1/dev/doctype/{name}    Read DocType definition from file
DELETE /api/v1/dev/doctype/{name}    Delete DocType files
GET    /api/v1/dev/apps              List installed apps (for app selector)
```

**POST /api/v1/dev/doctype** — Create:

Request body:
```json
{
  "name": "Book",
  "app": "library_app",
  "module": "Library",
  "layout": { "tabs": [...] },
  "fields": { "title": {...}, "author": {...} },
  "settings": {
    "naming_rule": "autoincrement",
    "is_submittable": false,
    "is_single": false,
    "is_child_table": false,
    "track_changes": true,
    "title_field": "title",
    "sort_field": "created_at",
    "sort_order": "desc"
  },
  "permissions": [
    { "role": "System Manager", "read": true, "write": true, "create": true, "delete": true }
  ]
}
```

Backend actions:
1. Validate the definition
2. Create directory `apps/{app}/modules/{module}/doctypes/{name}/`
3. Write `{name}.json` — the tree-native MetaType definition
4. Write `{name}.go` — Go controller stub with lifecycle hook placeholders
5. Register the MetaType in the registry (hot reload picks it up)
6. Run DDL to create/migrate the database table
7. Return the saved definition

**PUT /api/v1/dev/doctype/{name}** — Update:
1. Validate changes
2. Overwrite `{name}.json`
3. Do NOT overwrite `{name}.go` (preserves developer's controller code)
4. Re-register in registry
5. Run DDL migration (add new columns, never drop existing)
6. Return updated definition

### Runtime API (Existing Endpoints)

For admin users on deployed sites creating DocTypes without file access:

```
POST   /api/v1/document/DocType           Create
PUT    /api/v1/document/DocType/{name}    Update
GET    /api/v1/document/DocType/{name}    Read
DELETE /api/v1/document/DocType/{name}    Delete
```

Uses the existing document CRUD in `pkg/api`. The DocType itself is a DocType — its definition is stored in the `tab_doctype` table. When saved, the registry reloads and DDL runs automatically via existing hooks.

### Shared Validation Rules

Applied by both dev-mode and runtime save paths:

- Field names must be unique within the DocType
- Field names must be snake_case, matching `^[a-z][a-z0-9_]*$`
- Field names must not be reserved SQL keywords or system column names (`name`, `created_at`, `modified_at`, `owner`, `_extra`)
- Field types must be valid members of the FieldType enum
- Link/Table field `options` must reference an existing DocType (warning for forward references, not error)
- At least one tab with at least one section must exist in the layout
- Column widths must be positive integers
- Naming rule must have required companion values (pattern rule needs pattern string, field rule needs field name)
- DocType name must be TitleCase, matching `^[A-Z][a-zA-Z0-9]*$`

---

## 7. Data Flow & State Management

### Frontend State (Zustand Store)

```typescript
interface BuilderState {
  // DocType identity
  name: string;
  app: string | null;           // null in runtime mode
  module: string;
  isNew: boolean;

  // Layout tree (the canvas model)
  layout: LayoutTree;           // tabs -> sections -> columns -> field refs

  // Field definitions (keyed by name)
  fields: Record<string, FieldDef>;

  // DocType settings
  settings: DocTypeSettings;

  // Permissions
  permissions: PermRule[];

  // UI state
  selectedNode: SelectedNode | null;  // { type, id }
  activeDrawer: "fields" | "settings" | "permissions" | null;
  mode: "schematic" | "preview";
  isDirty: boolean;

  // Command history (undo/redo)
  history: CommandHistory;
}
```

### Flow: Adding a Field from Palette

1. DnD event fires with `fieldType="Data"`, target = column reference
2. Builder creates `AddFieldCommand`:
   - `execute()`: generate unique name, add to `fields` map, insert ref into column, select field, mark dirty
   - `undo()`: remove from `fields` map, remove ref from column, clear selection
3. `CommandHistory.execute(cmd)` pushes to undo stack, clears redo stack
4. React re-renders canvas with new field card
5. PropertyPanel opens for the new field

### Flow: Saving (Dev Mode)

1. Client-side validation runs (unique names, required settings, valid layout)
2. Serialize state to API payload
3. `POST`/`PUT` to `/api/v1/dev/doctype/{name}` via TanStack Query mutation
4. Backend validates, writes files, registers MetaType, runs DDL
5. On success: `isDirty = false`, history bookmark, toast notification, URL update if new
6. On error: validation errors shown inline, toast with error, state unchanged

### Flow: Loading Existing DocType

1. Navigate to `/desk/app/doctype-builder/Book`
2. TanStack Query fetches `GET /api/v1/meta/Book` (includes layout tree)
3. Hydrate BuilderState from response
4. Determine save mode: dev API (if dev mode + app on disk) or runtime API
5. Render canvas from layout tree directly

### Dirty Tracking

Compares current state against last saved snapshot. Drives:
- Save button enabled/disabled
- "Unsaved changes" indicator in toolbar
- Browser `beforeunload` warning
- Navigation prompt when switching DocTypes

---

## 8. Testing Strategy

### Frontend Tests (Vitest + React Testing Library)

| Layer | What's Tested |
|---|---|
| BuilderKit unit | Command history operations, property schema rendering, drawer state management |
| Canvas unit | Field card rendering, section/column structure, selection state |
| DnD integration | Drag from palette adds field, reorder updates layout, cross-column moves |
| State management | Zustand store actions produce correct state transitions |
| Serialization | State <-> API payload round-trips produce identical state |
| Validation | Client-side catches duplicate names, missing settings, empty layout |

### Backend Tests (Go)

| Layer | What's Tested |
|---|---|
| Tree storage | Parse tree-native JSON, `ExtractFields()` produces correct ordered `[]FieldDef` |
| Migration utility | Flat JSON -> tree-native conversion preserves all field data and layout |
| Dev API | POST creates correct file structure, PUT updates JSON without clobbering controller |
| Validation | Server-side rules reject invalid definitions |
| DDL integration | Tree-native MetaType generates same DDL as flat equivalent |

### E2E Tests (Playwright)

1. Create new DocType -> add fields -> save -> verify JSON file written correctly
2. Open existing DocType -> reorder fields -> save -> verify changes persisted
3. Drag field from palette -> drop in column -> verify canvas updates
4. Undo/redo cycle -> verify state consistency
5. Schematic <-> preview toggle -> verify preview renders correctly
6. Permission editor -> add role -> set permissions -> save -> verify in API

### Not Tested (Covered by Existing Tests)

- Individual field components (MS-17 tests)
- FormView rendering from MetaType (MS-17 tests)
- Document CRUD API (MS-06 tests)
- shadcn/ui component behavior (upstream tests)

---

## 9. Technology Summary

| Layer | Technology |
|---|---|
| DnD library | `@dnd-kit/core` + `@dnd-kit/sortable` |
| State management | Zustand |
| Server state | TanStack Query (already in Desk) |
| Component library | shadcn/ui (already in Desk) |
| Styling | TailwindCSS (already in Desk) |
| Unit testing | Vitest + React Testing Library |
| E2E testing | Playwright |
| Backend | Go (existing `pkg/meta`, `pkg/api`, `pkg/orm` packages) |

---

## 10. Out of Scope

- **Customize Form** (customizing existing DocTypes without modifying the source app) — future feature that reuses the builder in a restricted mode
- **Workflow Builder** — future consumer of BuilderKit, not part of this spec
- **Report Builder** — future consumer of BuilderKit, not part of this spec
- **Mobile-responsive layout controls** — layout is desktop-first; mobile rendering is handled by the FormView's responsive CSS
- **Custom field type creation** — registering new field types via the builder UI (developers use code for this)
- **Import/export DocTypes** — CLI command for sharing DocType definitions between projects

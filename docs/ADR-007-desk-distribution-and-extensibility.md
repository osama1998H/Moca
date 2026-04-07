# ADR-007: Desk Frontend Distribution, Scaffolding & App Extensibility

**Status:** Proposed
**Date:** 2026-04-07
**Author:** Osama Muhammed
**Deciders:** Osama Muhammed (framework lead)
**Supersedes:** None
**Related:** ADR-001 (pg-tenant), MS-17 (React Desk Foundation), MS-19 (Real-time & Custom Fields), MS-21 (Deployment)

---

## Context

### The Problem

When a developer runs `moca init` to create a new Moca project, the scaffolded project contains **no frontend**. The `desk/` React application lives exclusively in the main Moca framework repository at `/desk/`. A developer creating a project (e.g., `demo/`) gets:

```
demo/
├── moca.yaml
├── moca.lock
├── apps/
├── sites/
└── .moca/
```

There is no `desk/` directory. The developer must manually point to or copy the framework's desk to get a working UI.

### Why This Matters

Moca's entire value proposition is **metadata-driven UI generation** — you define a MetaType, and the desk renders forms, lists, dashboards, and reports automatically. Without the desk in the project, a newly scaffolded Moca project is a headless API with no UI. This is the equivalent of installing Frappe without the `frappe` app — it simply doesn't work as intended.

### How Frappe Solves This (And Why We Can't Copy It)

In Frappe/ERPNext, the framework itself (`frappe`) is a Python app installed into every bench:

```
my-bench/
├── apps/
│   ├── frappe/          ← framework IS an app (includes desk UI)
│   └── erpnext/
├── sites/
└── env/                 ← Python virtualenv
```

Frappe's desk lives inside `frappe/frappe/public/` and gets built with `bench build`. Every bench always has the frontend because the framework is an app.

Moca made a deliberate architectural choice to **decouple** the frontend. The system design document states: *"Tightly coupled Desk UI → Decoupled React frontend consuming a metadata API"* as an explicit improvement over Frappe. This is the right call — but the current implementation is incomplete. We decoupled the frontend from the backend runtime but forgot to design how it reaches developer projects.

### Current State of Affairs

| Component | Location | Included in `moca init`? |
|-----------|----------|--------------------------|
| Go backend (pkg/, cmd/) | Framework repo | Yes (via Go modules) |
| Core app (apps/core/) | Framework repo | Yes (via `go.work`) |
| Desk frontend (desk/) | Framework repo root | **No** |
| App desk extensions | Per-app `desk/` dirs | **No mechanism exists** |

The `moca serve` command already handles two modes:
1. **Production:** serves static files from `{project}/desk/dist/`
2. **Development:** proxies `/desk/*` to Vite dev server on port 3000

And `moca build desk` already generates `.moca-extensions.ts` from discovered app extensions. The serving and build infrastructure exists — but there's no way for the desk source to get into a project.

### What Already Works (Building Blocks)

1. **`desk/src/index.ts`** — A public API module already exports `registerFieldType`, hooks (`useAuth`, `useWebSocket`, `useMetaType`, `useDocument`, etc.), types (`FieldProps`, `MetaType`, `DocRecord`), and components (`FieldRenderer`). This is the embryo of an npm package API.

2. **`.moca-extensions.ts`** — Auto-generated import manifest. `moca build desk` scans apps for desk extensions and writes this file. Currently produces "No app desk extensions discovered" but the mechanism exists.

3. **`Architecture.md`** already states the intended three-layer composition model: *"The build composes three layers: framework desk (`@osama1998h/desk`), app desk extensions, and optional project-level overrides."*

4. **`DevelopmentConfig`** already has `desk_port` and `desk_dev_server` fields.

5. **MS-19 plan** already specifies re-exporting public API for app desk extensions and enhancing `moca build desk` to discover them.

---

## Decision

Distribute the Moca Desk as an **npm package (`@osama1998h/desk`)** with a **thin project-level scaffold** created by `moca init`, and implement a **build-time plugin system** for app desk extensions.

---

## Options Considered

### Option A: npm Package + Thin Scaffold (Recommended)

| Dimension | Assessment |
|-----------|------------|
| Complexity | Medium |
| Developer Experience | Excellent — familiar npm workflow |
| Customizability | High — project overrides, app plugins |
| Update Safety | High — SemVer, no overwrites |
| Node.js Requirement | Required for dev, optional for production |
| Team Familiarity | High (React/npm ecosystem) |

**How it works:**

1. The Moca team publishes `@osama1998h/desk` to npm (or a private registry). This package contains the core desk application: shell, providers, field components, pages, public API.

2. `moca init` scaffolds a thin `desk/` directory in the project:
   ```
   my-project/desk/
   ├── package.json          ← depends on @osama1998h/desk
   ├── vite.config.ts        ← pre-configured, imports @osama1998h/desk/vite
   ├── index.html            ← entry point, loads main.tsx
   ├── src/
   │   ├── main.tsx          ← imports createDeskApp() from @osama1998h/desk
   │   └── overrides/        ← project-level customizations (empty initially)
   ├── tsconfig.json
   └── .gitignore
   ```

3. The project's `main.tsx` is ~10 lines:
   ```tsx
   import { createDeskApp } from "@osama1998h/desk";
   import "./overrides"; // project-level customizations

   createDeskApp({
     // Project-level config overrides
   }).mount("#root");
   ```

4. Updates: `npm update @osama1998h/desk` or `moca desk update`. SemVer ensures breaking changes are explicit. Project files in `src/overrides/` are never touched.

5. App extensions: Each Moca app can have a `desk/` directory with a `desk-manifest.json` declaring custom field types, pages, sidebar items. `moca build desk` discovers these and generates `.moca-extensions.ts`.

**Pros:**
- Clean separation: framework code in `node_modules/`, project customizations in `src/`
- SemVer updates — developer controls when to upgrade
- Familiar workflow for any React/TypeScript developer
- App extensions compose cleanly via build-time discovery
- `desk/src/index.ts` (public API) already exists — just needs packaging
- Production deployment can use pre-built assets (no Node.js on server)
- Aligns with Architecture.md's stated three-layer model

**Cons:**
- Requires npm registry (public npm, GitHub Packages, or Verdaccio for private)
- Developers need Node.js for desk development
- One more thing to version and publish alongside Go releases

---

### Option B: Desk Embedded in Go Binary (Go embed)

| Dimension | Assessment |
|-----------|------------|
| Complexity | Low |
| Developer Experience | Limited — no customization without rebuilding |
| Customizability | Very Low |
| Update Safety | High — tied to binary version |
| Node.js Requirement | None |
| Team Familiarity | Medium (Go embed) |

**How it works:**

Pre-build desk assets and embed them into `moca-server` using `//go:embed desk/dist/*`. The server serves these embedded files. No Node.js needed anywhere.

**Pros:**
- True single-binary deployment
- Zero frontend dependencies for developers
- Always in sync with backend version

**Cons:**
- **Cannot customize desk without rebuilding the Go binary** — this kills the framework's extensibility story
- Binary size grows significantly (~5-15MB of JS/CSS assets)
- No app desk extensions possible (or requires complex runtime injection)
- No hot reload for desk development
- Contradicts the "decoupled frontend" design principle
- Developers building business apps on Moca can't add custom pages, field types, or branding

---

### Option C: Desk as Part of Core App (Frappe-Style)

| Dimension | Assessment |
|-----------|------------|
| Complexity | Medium-High |
| Developer Experience | Familiar to Frappe devs |
| Customizability | Medium — full source available but merge conflicts |
| Update Safety | Low — git pull can conflict |
| Node.js Requirement | Required |
| Team Familiarity | High (Frappe developers) |

**How it works:**

Make the desk source code part of `apps/core/desk/`. When `moca init` clones/installs the core app, the desk comes with it. Updates via `moca app update core`.

**Pros:**
- Always present — core app is mandatory
- Full source code available for inspection
- Familiar to anyone coming from Frappe

**Cons:**
- Mixes Go modules and Node.js source in the same app directory
- `go.work` doesn't understand npm — two dependency systems in one app
- Updates require git merge, risking conflicts with customizations
- No clean separation between "framework code" and "project overrides"
- Developers tempted to edit core desk source directly, then can't update

---

### Option D: Git Submodule / CLI Clone

| Dimension | Assessment |
|-----------|------------|
| Complexity | Low |
| Developer Experience | Poor — git submodules are widely disliked |
| Customizability | Full source, but update conflicts |
| Update Safety | Low |
| Node.js Requirement | Required |
| Team Familiarity | Low (submodule pain) |

**How it works:**

`moca init` clones the desk repository (or adds it as a git submodule) into `desk/`. Version pinned in `moca.lock`.

**Pros:**
- Simple to implement
- Full source available

**Cons:**
- Git submodules are notoriously painful
- Clone approach means no clean update path
- Developer edits get overwritten on update
- No separation between framework code and customizations

---

## Trade-off Analysis

| Criterion | A: npm Package | B: Go Embed | C: Core App | D: Git Clone |
|-----------|---------------|-------------|-------------|-------------|
| Zero-config for new projects | Yes | Yes | Yes | Yes |
| App desk extensions | Yes (build-time) | No | Difficult | No |
| Safe updates | Yes (SemVer) | Yes (binary) | No (merge) | No (overwrite) |
| Project-level customization | Yes (overrides/) | No | Risky | Risky |
| No Node.js requirement | Prod only | Full | No | No |
| Framework/project separation | Clean | N/A | Blurred | Blurred |
| Aligns with existing design | Yes | No | Partial | No |

**Option A is the clear winner.** It's the only option that supports all three requirements: safe updates, app extensibility, and project-level customization — while maintaining the decoupled architecture that Moca was designed around.

---

## Detailed Design: Option A

### 1. Package Structure — `@osama1998h/desk`

The current `desk/` directory in the framework repo becomes the source for the npm package:

```
desk/                              ← framework repo (source of truth)
├── package.json                   ← name: "@osama1998h/desk"
├── src/
│   ├── index.ts                   ← public API (already exists)
│   ├── app.ts                     ← NEW: createDeskApp() factory
│   ├── vite-plugin.ts             ← NEW: Vite plugin for Moca desk builds
│   ├── components/                ← all desk components
│   ├── providers/                 ← all providers
│   ├── pages/                     ← all page components
│   ├── hooks/                     ← all hooks
│   ├── layouts/                   ← desk layout
│   ├── lib/                       ← utilities, field registry
│   ├── api/                       ← API client
│   └── router.tsx                 ← route definitions
├── dist/                          ← built package output
└── tsconfig.json
```

**Public API (`src/index.ts`) — expanded:**

```typescript
// App factory
export { createDeskApp } from "./app";
export type { DeskAppConfig } from "./app";

// Vite plugin
export { mocaDeskPlugin } from "./vite-plugin";

// Extension registration
export { registerFieldType } from "./lib/fieldTypeRegistry";
export { registerPage } from "./lib/pageRegistry";         // NEW
export { registerSidebarItem } from "./lib/sidebarRegistry"; // NEW
export { registerDashboardWidget } from "./lib/widgetRegistry"; // NEW

// Types
export type { FieldProps, LayoutFieldProps } from "./components/fields/types";
export type { FieldType, FieldDef, MetaType, DocRecord } from "./api/types";
export type { DeskExtension, AppDeskManifest } from "./types/extensions"; // NEW

// Components (for app extensions to compose with)
export { FieldRenderer } from "./components/fields/FieldRenderer";
export { FormView } from "./pages/FormView";
export { ListView } from "./pages/ListView";

// Hooks
export { useAuth } from "./providers/AuthProvider";
export { useWebSocket } from "./providers/WebSocketProvider";
export { useMetaType } from "./providers/MetaProvider";
export { useDocument, useDocList } from "./providers/DocProvider";
export { usePermission } from "./providers/PermissionProvider";
export { useRealtimeDoc } from "./hooks/useRealtimeDoc";
export { useDocVersions } from "./hooks/useDocVersions";

// Real-time types
export type { DocUpdateEvent, WsConnectionState, VersionRecord } from "./api/ws-types";
```

### 2. Project Scaffold — What `moca init` Creates

```
my-project/
├── desk/
│   ├── package.json
│   ├── index.html
│   ├── vite.config.ts
│   ├── tsconfig.json
│   ├── tsconfig.app.json
│   ├── .gitignore
│   └── src/
│       ├── main.tsx               ← entry point (~10 lines)
│       └── overrides/
│           ├── index.ts           ← barrel export for project overrides
│           ├── theme.ts           ← theme/branding overrides (colors, logo)
│           └── README.md          ← explains how to add overrides
├── apps/
├── sites/
├── moca.yaml
└── ...
```

**Scaffolded `desk/package.json`:**

```json
{
  "name": "my-project-desk",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "typecheck": "tsc --noEmit"
  },
  "dependencies": {
    "@osama1998h/desk": "^0.1.0"
  },
  "devDependencies": {
    "@types/react": "^19.0.0",
    "@types/react-dom": "^19.0.0",
    "typescript": "^6.0.0",
    "vite": "^8.0.0"
  }
}
```

**Scaffolded `desk/src/main.tsx`:**

```tsx
import { createDeskApp } from "@osama1998h/desk";
import "./overrides";

const app = createDeskApp({
  // Project-level configuration
  // theme: { ... },
  // logo: "/path/to/logo.svg",
  // defaultLocale: "en",
});

app.mount("#root");
```

**Scaffolded `desk/vite.config.ts`:**

```typescript
import { defineConfig } from "vite";
import { mocaDeskPlugin } from "@osama1998h/desk/vite";

export default defineConfig({
  plugins: [mocaDeskPlugin()],
  // The plugin handles: React, TailwindCSS, base path (/desk/),
  // API proxy, WebSocket proxy, app extension discovery.
  // Override any setting here if needed.
});
```

### 3. App Desk Extension System

Each Moca app can include a `desk/` directory with UI extensions:

```
apps/
├── core/                          ← framework core
│   └── desk/                      ← core desk extensions (if any)
├── crm/                           ← example: CRM app
│   ├── go.mod
│   ├── manifest.yaml
│   ├── modules/
│   └── desk/                      ← CRM desk extensions
│       ├── desk-manifest.json     ← declares what this app extends
│       ├── fields/
│       │   └── PhoneField.tsx     ← custom field type
│       ├── pages/
│       │   └── CRMDashboard.tsx   ← custom page
│       └── sidebar/
│           └── crm-items.ts       ← sidebar additions
```

**`desk-manifest.json`:**

```json
{
  "app": "crm",
  "version": "1.0.0",
  "extensions": {
    "field_types": {
      "Phone": "./fields/PhoneField.tsx"
    },
    "pages": [
      {
        "path": "/desk/app/crm-dashboard",
        "component": "./pages/CRMDashboard.tsx",
        "label": "CRM Dashboard",
        "icon": "Phone"
      }
    ],
    "sidebar_items": [
      {
        "label": "CRM",
        "icon": "Phone",
        "children": [
          { "label": "Dashboard", "path": "/desk/app/crm-dashboard" },
          { "label": "Leads", "path": "/desk/app/Lead" },
          { "label": "Opportunities", "path": "/desk/app/Opportunity" }
        ]
      }
    ],
    "dashboard_widgets": [
      {
        "name": "crm_pipeline",
        "component": "./widgets/PipelineWidget.tsx"
      }
    ]
  }
}
```

**Build-time discovery (`moca build desk`):**

1. Scan all `apps/*/desk/desk-manifest.json` files
2. Validate manifests against schema
3. Generate `.moca-extensions.ts`:
   ```typescript
   // Auto-generated by 'moca build desk'. Do not edit.
   import { registerFieldType, registerPage, registerSidebarItem } from "@osama1998h/desk";

   // === crm app extensions ===
   import CrmPhoneField from "../apps/crm/desk/fields/PhoneField";
   registerFieldType("Phone", CrmPhoneField);

   import CrmDashboard from "../apps/crm/desk/pages/CRMDashboard";
   registerPage("/desk/app/crm-dashboard", CrmDashboard, {
     label: "CRM Dashboard",
     icon: "Phone"
   });

   registerSidebarItem({ label: "CRM", icon: "Phone", children: [
     { label: "Dashboard", path: "/desk/app/crm-dashboard" },
     { label: "Leads", path: "/desk/app/Lead" },
     { label: "Opportunities", path: "/desk/app/Opportunity" },
   ]});
   ```

4. Vite resolves these imports at build time — tree-shaken, code-split, optimized.

### 4. Three-Layer Composition Model

```
┌─────────────────────────────────────────────────┐
│  Layer 3: Project Overrides                     │
│  desk/src/overrides/                            │
│  Theme, branding, custom pages, locale config   │
│  ↓ HIGHEST PRIORITY (overrides layers below)    │
├─────────────────────────────────────────────────┤
│  Layer 2: App Extensions                        │
│  apps/*/desk/ (discovered by moca build desk)   │
│  Custom field types, pages, sidebar items,      │
│  dashboard widgets per-app                      │
│  ↓ MEDIUM PRIORITY                              │
├─────────────────────────────────────────────────┤
│  Layer 1: @osama1998h/desk (npm package)              │
│  Core shell, providers, standard fields,        │
│  FormView, ListView, routing, API client        │
│  ↓ LOWEST PRIORITY (framework defaults)         │
└─────────────────────────────────────────────────┘
```

### 5. Update Flow

```
Developer wants to update desk:

1. cd my-project/desk
2. npm update @osama1998h/desk        # or: moca desk update
3. npm run build                # or: moca build desk

What happens:
- node_modules/@osama1998h/desk/ gets updated (SemVer)
- desk/src/main.tsx         → UNTOUCHED (developer's file)
- desk/src/overrides/       → UNTOUCHED (developer's files)
- apps/*/desk/              → UNTOUCHED (app extension files)
- .moca-extensions.ts       → REGENERATED (auto-generated, gitignored)
- desk/dist/                → REBUILT (production bundle)

Breaking changes:
- Major version bump (1.x → 2.x) = migration guide published
- @osama1998h/desk follows React ecosystem SemVer conventions
- Public API (index.ts exports) covered by SemVer guarantees
```

### 6. CLI Commands

| Command | Description |
|---------|-------------|
| `moca init` | Scaffolds project **including** `desk/` with `package.json` depending on `@osama1998h/desk` |
| `moca desk install` | Runs `npm install` in `desk/` directory (convenience wrapper) |
| `moca desk update` | Runs `npm update @osama1998h/desk` + regenerates extensions + rebuilds |
| `moca desk dev` | Starts Vite dev server (port 3000) for desk development |
| `moca build desk` | Discovers app extensions → generates `.moca-extensions.ts` → runs Vite production build |
| `moca serve` | Serves built desk from `desk/dist/` (prod) or proxies to Vite (dev) — **unchanged** |

### 7. Production Deployment (No Node.js)

For production servers that don't have Node.js:

```bash
# On build machine (CI/CD):
cd my-project/desk
npm ci
moca build desk           # produces desk/dist/

# Deploy desk/dist/ alongside Go binaries
# moca-server serves desk/dist/ as static files — no Node.js needed at runtime
```

Alternatively, the `moca deploy` command (MS-21) can:
1. Build desk assets as part of the deployment pipeline
2. Include `desk/dist/` in the deployment artifact
3. Or upload to CDN and configure `moca-server` to redirect `/desk/*` to CDN

### 8. Package Registry Strategy

| Stage | Registry | Rationale |
|-------|----------|-----------|
| **Development** | `npm link` or `file:../../../desk` | Local development of framework + project |
| **Alpha/Beta** | GitHub Packages (npm) | Free for public packages, integrates with existing GitHub CI |
| **v1.0+** | Public npm registry | Maximum discoverability, standard workflow |
| **Enterprise** | Verdaccio or Artifactory | Private registry for proprietary extensions |

### 9. Handling the Transition (Demo Project Fix)

For the existing `demo/` project at `/Users/osamamuhammed/Moca/demo/`:

```bash
# Immediate fix (before npm package is published):
cd demo
mkdir -p desk
cat > desk/package.json << 'EOF'
{
  "name": "demo-desk",
  "private": true,
  "dependencies": {
    "@osama1998h/desk": "file:../../desk"
  }
}
EOF
cd desk && npm install

# This uses npm's file: protocol to link directly to the framework's desk/
# Works immediately, no publishing needed
```

---

## Impact Analysis: Current Implementations Affected

This section catalogs every file in the existing codebase — both frontend (desk) and backend (Go) — that will be modified, created, or affected by this change.

### Backend Go Files (16 files impacted)

#### Critical — Must Modify

| File | What Changes | Why |
|------|-------------|-----|
| `cmd/moca/init.go` | Add desk scaffold step to `runInit()` | Currently creates no `desk/` directory; must render desk templates and optionally run `npm install` |
| `cmd/moca/build.go` | Update `generateDeskExtensions()` (lines 347-398) | Currently looks for `desk/setup.ts` in apps; must also handle `desk-manifest.json` schema and generate richer `.moca-extensions.ts` with page/sidebar/widget registrations |
| `cmd/moca/serve.go` | Update `StaticDir` resolution (line 81) | Path `filepath.Join(projectRoot, "desk", "dist")` remains the same, but init must guarantee this path exists |
| `internal/serve/server.go` | Minor — add fallback/error for missing desk/dist | Lines 217-225: if neither `DeskDevServer` nor `desk/dist/` exists, serve a helpful "desk not built" page instead of 404 |
| `internal/config/types.go` | Add new config fields | Lines 122-124: add `DeskPackage string` (default `@osama1998h/desk`), `DeskAutoInstall bool` to `DevelopmentConfig` |
| `internal/config/validate.go` | Validate new fields | Add validation for `DeskPackage` format |
| `internal/config/merge.go` | Merge new fields | Lines 144-145: add merge logic for new desk config fields |

#### New Files to Create

| File | Purpose |
|------|---------|
| `internal/scaffold/desk_templates.go` | Embedded Go templates for desk scaffold files (package.json, main.tsx, vite.config.ts, etc.) |
| `cmd/moca/desk.go` | New `moca desk` command group: `desk install`, `desk update`, `desk dev` subcommands |

#### Test Files to Update

| File | What Changes |
|------|-------------|
| `internal/config/parse_test.go` | Add tests for new desk config fields |
| `internal/config/validate_test.go` | Add validation tests |
| `cmd/moca/build_test.go` | Update desk build tests for new extension manifest format |
| `cmd/moca/init_test.go` | Add tests verifying desk scaffold output |
| `internal/serve/server_test.go` | Test "desk not built" fallback behavior |

#### Unchanged (verified safe)

| File | Why No Change |
|------|--------------|
| `internal/serve/static.go` | SPA handler logic is path-agnostic; works with any `desk/dist/` contents |
| `internal/serve/proxy.go` | Dev proxy to Vite is unchanged; port config already dynamic |

---

### Frontend Desk Files (181 TypeScript files)

#### Critical — Must Modify (9 files)

| File | Current State | Required Change |
|------|--------------|-----------------|
| `desk/src/api/client.ts` | Hardcodes `/api/v1/` prefix (line 92) and `/api/v1/auth/refresh` (line 62); reads `VITE_MOCA_SITE` env var (line 28) | Parameterize API base URL via `DeskConfig` context; accept site name from config, not just env var |
| `desk/src/router.tsx` | Hardcodes `path: "/desk"` (line 24) as root route | Accept `basePath` from config; all route paths derived from configurable base |
| `desk/src/providers/WebSocketProvider.tsx` | Hardcodes `${protocol}://${location.host}/ws` (line 65) | Accept WS endpoint from config; support custom protocol/host/path |
| `desk/src/pages/Login.tsx` | Hardcodes fallback `"/desk/app"` (line 18) | Use config's `basePath` for login redirect |
| `desk/src/components/shell/Sidebar.tsx` | Hardcodes `to="/desk/app"` brand link (line 91) | Use config's `basePath` |
| `desk/src/main.tsx` | Directly creates providers, QueryClient, renders `<App/>` (40 lines) | Becomes the `createDeskApp()` factory inside the package; project's `main.tsx` becomes a thin 10-line consumer |
| `desk/src/index.ts` | Exports 15 items (field registry, hooks, types, components) | Expand to export `createDeskApp`, `mocaDeskPlugin`, `DeskConfig`, page/sidebar/widget registries |
| `desk/vite.config.ts` | Hardcodes `base: "/desk/"`, proxy targets `http://localhost:8000` | Extract shared config into `mocaDeskPlugin()` Vite plugin; proxy targets become configurable |
| `desk/package.json` | `name: "desk"`, all deps as `dependencies` | Change to `name: "@osama1998h/desk"`, move `react`/`react-dom` to `peerDependencies`, add `exports` field and library build config |

#### New Files to Create (3 files)

| File | Purpose |
|------|---------|
| `desk/src/config.ts` | `DeskConfig` interface and React context provider; stores API base, WS endpoint, base path, site name, theme overrides |
| `desk/src/app.ts` | `createDeskApp(config)` factory function; composes all providers, router, and mounts the application |
| `desk/src/vite-plugin.ts` | `mocaDeskPlugin(options)` Vite plugin; encapsulates React, TailwindCSS, base path, proxy config, extension resolution |

#### No Changes Needed (140+ files)

The vast majority of desk source files require **zero modifications**:

- **All 40+ field components** (`IntField.tsx`, `SelectField.tsx`, `DateField.tsx`, `TableField.tsx`, etc.) — these consume providers via hooks and don't reference paths directly
- **All layout components** (`SectionBreak.tsx`, `ColumnBreak.tsx`, `TabBreak.tsx`, etc.) — pure presentational
- **All UI components** (`button.tsx`, `input.tsx`, `select.tsx`, etc.) — shadcn primitives, no Moca-specific logic
- **All hooks** (`useRealtimeDoc.ts`, `useDocVersions.ts`, `useDirtyTracking.ts`) — consume providers, path-agnostic
- **All providers except WebSocketProvider** — `AuthProvider`, `MetaProvider`, `DocProvider`, `PermissionProvider`, `I18nProvider`, `DashboardProvider`, `ReportProvider` all use `api/client.ts` which will be parameterized at one point
- **Page components** (`FormView.tsx`, `ListView.tsx`, `DeskHome.tsx`, `DashboardView.tsx`, `ReportView.tsx`) — use `useNavigate()` with relative paths, no hardcoded `/desk` references
- **Utility files** (`fieldTypeRegistry.ts`, `utils.ts`, `expressionEval.ts`, `layoutParser.ts`) — pure logic, no path dependencies

#### Configuration Files to Update (3 files)

| File | Change |
|------|--------|
| `desk/tsconfig.app.json` | Add `composite: true` and `declaration: true` for package builds |
| `desk/.gitignore` | Already ignores `.moca-extensions.ts`; add `dist/` for library output |
| `desk/components.json` | No change (shadcn config is package-internal) |

---

### Documentation Files to Update

| File | Change |
|------|--------|
| `MOCA_CLI_SYSTEM_DESIGN.md` | Section 3 (Project Structure): update to show `desk/` with `@osama1998h/desk` dependency; add `moca desk` command group to command tree |
| `MOCA_SYSTEM_DESIGN.md` | Section 15 (Framework Package Layout): add desk package structure; frontend architecture notes |
| `Architecture.md` | "Frontend Architecture" section: expand three-layer composition model with concrete examples |
| `ROADMAP.md` | Note desk distribution as part of current milestone work |
| `docs/MS-17-react-desk-foundation-plan.md` | Add desk distribution deliverables |
| `CLAUDE.md` | Update project structure section with desk package info |

---

### Summary: Change Magnitude

| Category | Files Modified | Files Created | Files Unchanged |
|----------|---------------|---------------|-----------------|
| Backend Go (cmd/) | 3 | 1 | 0 |
| Backend Go (internal/) | 4 | 1 | 2 |
| Backend Go (tests) | 5 | 0 | — |
| Frontend (critical) | 9 | 3 | 140+ |
| Frontend (config) | 3 | 0 | — |
| Documentation | 6 | 0 | — |
| **Total** | **30** | **5** | **142+** |

The change is **surgically targeted**: 78% of the frontend codebase (140+ files) requires zero modifications. The refactoring primarily affects configuration plumbing (9 frontend files) and CLI scaffolding (4 backend files), not business logic.

---

## Consequences

### What Becomes Easier

- **New projects work out of the box** — `moca init` + `npm install` = full working UI
- **Safe updates** — npm SemVer protects against breaking changes; developer's code never overwritten
- **App ecosystem** — third-party apps can ship desk UI extensions with a well-defined manifest contract
- **Project customization** — theme, branding, custom pages live in `overrides/` with clear separation from framework code
- **CI/CD** — standard npm build pipeline, cacheable `node_modules`, reproducible builds via lockfile
- **Developer onboarding** — familiar React + npm workflow, no Moca-specific knowledge needed for UI work

### What Becomes Harder

- **Release coordination** — npm package version must be released alongside Go module versions; need CI automation
- **Monorepo management** — framework repo now publishes both Go modules and an npm package; may need a release workflow for `desk/`
- **First-time setup** — developers need Node.js installed for desk development (but NOT for production deployment)
- **Version matrix** — `@osama1998h/desk@0.2.0` must be compatible with `moca-server@0.2.x`; need compatibility table

### What We'll Revisit

- **Desk extension hot reload** — currently `moca build desk` must be re-run when app extensions change; Vite plugin could watch `apps/*/desk/` for changes
- **Server-side rendering (SSR)** — MS-27 (Portal) may need `@osama1998h/desk` to support SSR; the `createDeskApp()` factory should be designed with this in mind
- **Micro-frontends** — if Moca needs per-app code splitting at scale, we may evolve toward Module Federation
- **Visual theme editor** — a future desk feature could generate `overrides/theme.ts` via a UI, reducing the need for manual editing

---

## Implementation Plan

### Phase 1: Package Extraction (1-2 weeks)

**Goal:** Extract `@osama1998h/desk` as a publishable npm package from the existing `desk/` code.

1. [x] Create `createDeskApp()` factory function in `desk/src/createApp.tsx`
   - Accepts config (theme, locale, extensions)
   - Initializes providers, router, renders app
   - Replaces the hardcoded `main.tsx` setup

2. [x] Create `mocaDeskPlugin()` Vite plugin in `desk/src/vite-plugin.ts`
   - Encapsulates: React plugin, Tailwind, base path, API proxy, WebSocket proxy
   - Accepts overrides for all settings
   - Handles `.moca-extensions.ts` import resolution

3. [x] Expand `desk/src/index.ts` public API
   - Add `registerPage`, `registerSidebarItem`, `registerDashboardWidget`
   - Create registration registries (similar to existing `fieldTypeRegistry`)
   - Export all types needed by app extensions

4. [x] Configure `desk/package.json` for npm publishing
   - Set `name: "@osama1998h/desk"`, configure `exports`, `types`, `files`
   - Move react/react-dom to peerDependencies
   - Add `./vite` export entry for Vite plugin

5. [x] Verify existing `desk/` still works as-is during transition

### Phase 2: Scaffold Integration (1 week)

**Goal:** `moca init` creates a working `desk/` in new projects.

6. [x] Add desk scaffold templates to `internal/scaffold/`
   - `desk/package.json.tmpl`
   - `desk/index.html.tmpl`
   - `desk/vite.config.ts.tmpl`
   - `desk/tsconfig.json.tmpl`
   - `desk/tsconfig.app.json.tmpl`
   - `desk/tsconfig.node.json.tmpl`
   - `desk/src/main.tsx.tmpl`
   - `desk/src/overrides/index.ts.tmpl`
   - `desk/src/overrides/theme.ts.tmpl`
   - `desk/.gitignore.tmpl`
   - `desk/.moca-extensions.ts.tmpl`

7. [x] Update `cmd/moca/init.go` to scaffold `desk/` directory
   - Render templates with project name, Moca version
   - Handle `--skip-desk` flag for headless API-only projects
   - Auto-detect `file:` protocol when framework desk is nearby (development)
   - Print `moca desk install` in next steps

8. [x] Add `moca desk install` and `moca desk update` CLI commands
   - Thin wrappers around npm operations + extension regeneration

9. [x] Add `moca desk dev` CLI command
   - Starts Vite dev server with configurable port
   - Regenerates extensions before starting
   - Signal forwarding for clean shutdown

### Phase 3: App Extension System (1-2 weeks)

**Goal:** Third-party Moca apps can ship desk UI extensions.

10. [x] Define `desk-manifest.json` schema
    - Field types, pages, sidebar items, dashboard widgets
    - JSON Schema for validation

11. [x] Update `moca build desk` (in `cmd/moca/build.go`)
    - Scan `apps/*/desk/desk-manifest.json`
    - Validate against schema
    - Generate `.moca-extensions.ts` with proper imports and registrations
    - Handle TypeScript compilation of app extension files

12. [x] Implement registration registries in `@osama1998h/desk`
    - `pageRegistry` — custom route registration
    - `sidebarRegistry` — sidebar item injection
    - `widgetRegistry` — dashboard widget registration
    - All registries consumed by the desk app at render time

13. [x] Update `moca app new` scaffold to include optional `desk/` directory
    - Generate `desk-manifest.json` template
    - Create example custom field type stub

14. [x] Add app extension section to `moca app scaffold` templates

### Phase 4: CI/CD & Publishing (1 week)

**Goal:** Automated publishing of `@osama1998h/desk` alongside Go releases.

15. [x] Add npm publish step to `.github/workflows/release.yml`
    - On `v*` tag: build `@osama1998h/desk` → publish to GitHub Packages
    - Version synced with Go release tags
    - Frontend validation (typecheck + build) in both `ci.yml` and `release.yml`

16. [x] Add compatibility matrix documentation
    - `@osama1998h/desk@0.x` ↔ `moca-server@0.x` compatibility table
    - Located at `docs/desk-compatibility.md`

17. [x] Update `ROADMAP.md` to reflect desk distribution changes
    - Added to MS-17 scope, deliverables, and acceptance criteria

18. [x] End-to-end validation: `ci.yml` desk job validates `npm ci` → `typecheck` → `build` → verify output

### Phase 5: Migration & Documentation (1 week)

**Goal:** Migrate existing projects and document everything.

19. [x] Migrate `demo/` project to use `@osama1998h/desk` package
20. [x] Write developer documentation: "Getting Started with Moca Desk"
21. [x] Write guide: "Creating Desk Extensions for Your Moca App"
22. [x] Write guide: "Customizing Your Project's Desk Theme"
23. [x] Update `MOCA_CLI_SYSTEM_DESIGN.md` project structure section
24. [x] Update `MOCA_SYSTEM_DESIGN.md` frontend section

---

## Open Questions

1. **Package scope:** Should we use `@osama1998h/desk` (scoped) or `moca-desk` (unscoped)? Scoped is cleaner and leaves room for `@moca/cli`, `@moca/sdk`, etc.

2. **Monorepo tooling:** Should we adopt a monorepo tool (Turborepo, Nx) to manage the Go framework + npm package in one repo? Or keep it simple with a Makefile target?

3. **Version coupling:** Should `@osama1998h/desk` version exactly match the Go module version, or can they diverge? Exact matching is simpler but means a desk-only fix forces a full release.

4. **CDN option for production:** Should `moca deploy` support uploading `desk/dist/` to a CDN (CloudFront, Cloudflare R2) instead of serving from the Go process? This is a performance optimization for later.

---

## Action Items

1. [ ] **Immediate:** Fix demo project with `file:` protocol link (unblocks development today)
2. [ ] **This sprint:** Phase 1 — extract `@osama1998h/desk` package with `createDeskApp()` factory
3. [ ] **Next sprint:** Phase 2 — scaffold integration into `moca init`
4. [ ] **Following sprint:** Phase 3 — app extension system
5. [ ] **Before v1.0:** Phase 4 + 5 — CI/CD publishing and documentation

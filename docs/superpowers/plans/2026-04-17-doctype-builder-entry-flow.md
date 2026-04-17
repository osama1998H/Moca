# DocType Builder Entry Flow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the DocType Builder's "empty canvas on arrival" behavior with a non-dismissible entry modal that lets users Create New or Open Existing, while preserving the deep-link (`/doctype-builder/:name`) fast path.

**Architecture:** One new backend list endpoint (`GET /api/v1/dev/doctype`). One new Zustand store action (`stageNew`). Four new frontend components under `desk/src/components/doctype-builder/entry/`. One page edit to mount the dialog. Client-side search; non-dismissible modal; staged creation (no backend write until canvas Save).

**Tech Stack:** Go 1.26 (`pkg/api`, stdlib `net/http`, `httptest`), React 19 + TypeScript, Zustand, shadcn Dialog (Radix), Vitest + `@testing-library/react` + jsdom.

**Spec:** [`docs/superpowers/specs/2026-04-17-doctype-builder-entry-flow-design.md`](../specs/2026-04-17-doctype-builder-entry-flow-design.md) (commit `abd2211`).

---

## File Structure

### New files

| Path | Responsibility |
|---|---|
| `desk/src/components/doctype-builder/entry/DocTypeEntryDialog.tsx` | Non-dismissible modal; owns view state (`landing`/`create`/`open`); Escape + overlay-click no-ops. |
| `desk/src/components/doctype-builder/entry/EntryLanding.tsx` | Two-card landing UI. |
| `desk/src/components/doctype-builder/entry/CreateDocTypeForm.tsx` | Create form (name/app/module/type) with inline availability check. |
| `desk/src/components/doctype-builder/entry/DocTypeList.tsx` | Fetches `GET /dev/doctype`; searchable, clickable rows. |
| `desk/src/components/doctype-builder/entry/DocTypeEntryDialog.test.tsx` | Component tests for dialog. |
| `desk/src/components/doctype-builder/entry/CreateDocTypeForm.test.tsx` | Component tests for create form. |
| `desk/src/components/doctype-builder/entry/DocTypeList.test.tsx` | Component tests for list. |
| `desk/src/stores/doctype-builder-store.test.ts` | Store tests (covers `stageNew`). |

### Modified files

| Path | What changes |
|---|---|
| `pkg/api/dev_handler.go` | Add `DocTypeListItem` struct, `HandleListDocTypes` handler, route registration in `RegisterDevRoutes`. |
| `pkg/api/dev_handler_test.go` | Add unit tests for new handler. |
| `pkg/api/dev_api_integration_test.go` | Add integration test for list endpoint. |
| `desk/src/api/types.ts` | Add `DocTypeListItem` TS type. |
| `desk/src/stores/doctype-builder-store.ts` | Add `stageNew` action. |
| `desk/src/pages/DocTypeBuilder.tsx` | Add `showEntryDialog` state + useEffect sync; render dialog. |
| `docs/superpowers/specs/2026-04-13-doctype-builder-design.md` | Cross-reference the entry-flow spec. |
| `docs/MOCA_SYSTEM_DESIGN.md` | Update DocType Builder section (if it describes direct-to-canvas flow). |
| `wiki/` (submodule) | Update DocType Builder page if present. |

---

## Task 1: Backend — `HandleListDocTypes` endpoint (TDD)

**Files:**
- Modify: `pkg/api/dev_handler.go`
- Test: `pkg/api/dev_handler_test.go`

- [ ] **Step 1.1: Write the first failing test — empty apps directory**

Add to `pkg/api/dev_handler_test.go`:

```go
func TestDevHandler_ListDocTypes_Empty(t *testing.T) {
    h := api.NewDevHandler(t.TempDir(), nil, nil)
    req := httptest.NewRequest("GET", "/api/v1/dev/doctype", nil)
    w := httptest.NewRecorder()

    h.HandleListDocTypes(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d", w.Code)
    }

    var resp struct {
        Data []api.DocTypeListItem `json:"data"`
    }
    if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
        t.Fatalf("decode: %v", err)
    }
    if resp.Data == nil {
        t.Fatal("expected non-nil data array")
    }
    if len(resp.Data) != 0 {
        t.Fatalf("expected empty array, got %v", resp.Data)
    }
}
```

- [ ] **Step 1.2: Run test, verify it fails**

Run: `cd /Users/osamamuhammed/Moca && go test -run TestDevHandler_ListDocTypes_Empty ./pkg/api/...`
Expected: FAIL — `api.DocTypeListItem` undefined and `h.HandleListDocTypes` undefined.

- [ ] **Step 1.3: Add the type and minimal handler**

In `pkg/api/dev_handler.go`, after the `DevDocTypeSettings` struct (around line 180), add:

```go
// DocTypeListItem is one entry in the dev-mode DocType list response.
// Returned by GET /api/v1/dev/doctype for the builder entry dialog.
type DocTypeListItem struct {
    Name          string `json:"name"`
    App           string `json:"app"`
    Module        string `json:"module"`
    IsSubmittable bool   `json:"is_submittable"`
    IsSingle      bool   `json:"is_single"`
    IsChildTable  bool   `json:"is_child_table"`
    IsVirtual     bool   `json:"is_virtual"`
}

// HandleListDocTypes returns every DocType JSON file found under
// {appsDir}/*/modules/*/doctypes/*/{slug}.json. Malformed files are skipped.
func (h *DevHandler) HandleListDocTypes(w http.ResponseWriter, r *http.Request) {
    items := make([]DocTypeListItem, 0)

    appEntries, err := os.ReadDir(h.appsDir)
    if err != nil {
        if os.IsNotExist(err) {
            writeJSON(w, http.StatusOK, map[string]any{"data": items})
            return
        }
        h.logger.Debug("read apps directory failed", slog.String("error", err.Error()))
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
        return
    }

    for _, app := range appEntries {
        if !app.IsDir() {
            continue
        }
        modulesDir := filepath.Join(h.appsDir, app.Name(), "modules")
        modEntries, err := os.ReadDir(modulesDir)
        if err != nil {
            continue
        }
        for _, mod := range modEntries {
            if !mod.IsDir() {
                continue
            }
            doctypesDir := filepath.Join(modulesDir, mod.Name(), "doctypes")
            dtEntries, err := os.ReadDir(doctypesDir)
            if err != nil {
                continue
            }
            for _, dt := range dtEntries {
                if !dt.IsDir() {
                    continue
                }
                jsonPath := filepath.Join(doctypesDir, dt.Name(), dt.Name()+".json")
                data, err := os.ReadFile(jsonPath)
                if err != nil {
                    continue
                }
                var parsed struct {
                    Name          string `json:"name"`
                    Module        string `json:"module"`
                    IsSubmittable bool   `json:"is_submittable"`
                    IsSingle      bool   `json:"is_single"`
                    IsChildTable  bool   `json:"is_child_table"`
                    IsVirtual     bool   `json:"is_virtual"`
                }
                if err := json.Unmarshal(data, &parsed); err != nil {
                    h.logger.Debug("skip malformed doctype json", slog.String("path", jsonPath), slog.String("error", err.Error()))
                    continue
                }
                if parsed.Name == "" {
                    continue
                }
                items = append(items, DocTypeListItem{
                    Name:          parsed.Name,
                    App:           app.Name(),
                    Module:        parsed.Module,
                    IsSubmittable: parsed.IsSubmittable,
                    IsSingle:      parsed.IsSingle,
                    IsChildTable:  parsed.IsChildTable,
                    IsVirtual:     parsed.IsVirtual,
                })
            }
        }
    }

    writeJSON(w, http.StatusOK, map[string]any{"data": items})
}
```

- [ ] **Step 1.4: Run the first test — expect PASS**

Run: `go test -run TestDevHandler_ListDocTypes_Empty ./pkg/api/...`
Expected: PASS.

- [ ] **Step 1.5: Add the populated-tree test**

Append to `pkg/api/dev_handler_test.go`:

```go
func TestDevHandler_ListDocTypes_Populated(t *testing.T) {
    dir := t.TempDir()

    // apps/acme/modules/crm/doctypes/customer/customer.json — a plain Submittable doctype
    customerDir := filepath.Join(dir, "acme", "modules", "crm", "doctypes", "customer")
    if err := os.MkdirAll(customerDir, 0o755); err != nil {
        t.Fatal(err)
    }
    customerJSON := []byte(`{"name":"Customer","module":"crm","is_submittable":true,"is_single":false,"is_child_table":false,"is_virtual":false}`)
    if err := os.WriteFile(filepath.Join(customerDir, "customer.json"), customerJSON, 0o644); err != nil {
        t.Fatal(err)
    }

    // apps/acme/modules/crm/doctypes/order_line/order_line.json — a Child Table
    orderLineDir := filepath.Join(dir, "acme", "modules", "crm", "doctypes", "order_line")
    if err := os.MkdirAll(orderLineDir, 0o755); err != nil {
        t.Fatal(err)
    }
    orderLineJSON := []byte(`{"name":"Order Line","module":"crm","is_submittable":false,"is_single":false,"is_child_table":true,"is_virtual":false}`)
    if err := os.WriteFile(filepath.Join(orderLineDir, "order_line.json"), orderLineJSON, 0o644); err != nil {
        t.Fatal(err)
    }

    h := api.NewDevHandler(dir, nil, nil)
    req := httptest.NewRequest("GET", "/api/v1/dev/doctype", nil)
    w := httptest.NewRecorder()
    h.HandleListDocTypes(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d", w.Code)
    }

    var resp struct {
        Data []api.DocTypeListItem `json:"data"`
    }
    if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
        t.Fatalf("decode: %v", err)
    }
    if len(resp.Data) != 2 {
        t.Fatalf("expected 2 doctypes, got %d: %+v", len(resp.Data), resp.Data)
    }

    byName := map[string]api.DocTypeListItem{}
    for _, it := range resp.Data {
        byName[it.Name] = it
    }
    cust, ok := byName["Customer"]
    if !ok {
        t.Fatalf("missing Customer: %+v", resp.Data)
    }
    if cust.App != "acme" || cust.Module != "crm" || !cust.IsSubmittable {
        t.Fatalf("unexpected Customer: %+v", cust)
    }
    ol, ok := byName["Order Line"]
    if !ok {
        t.Fatalf("missing Order Line")
    }
    if !ol.IsChildTable {
        t.Fatalf("Order Line not flagged as child table: %+v", ol)
    }
}
```

- [ ] **Step 1.6: Run populated test — expect PASS**

Run: `go test -run TestDevHandler_ListDocTypes_Populated ./pkg/api/...`
Expected: PASS.

- [ ] **Step 1.7: Add malformed-JSON skip test**

Append to `pkg/api/dev_handler_test.go`:

```go
func TestDevHandler_ListDocTypes_SkipsMalformed(t *testing.T) {
    dir := t.TempDir()

    // Valid doctype
    validDir := filepath.Join(dir, "acme", "modules", "crm", "doctypes", "good")
    if err := os.MkdirAll(validDir, 0o755); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(validDir, "good.json"),
        []byte(`{"name":"Good","module":"crm"}`), 0o644); err != nil {
        t.Fatal(err)
    }

    // Malformed doctype (broken JSON)
    badDir := filepath.Join(dir, "acme", "modules", "crm", "doctypes", "bad")
    if err := os.MkdirAll(badDir, 0o755); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(badDir, "bad.json"),
        []byte(`{not valid json`), 0o644); err != nil {
        t.Fatal(err)
    }

    // Doctype with empty name — should also be skipped
    noNameDir := filepath.Join(dir, "acme", "modules", "crm", "doctypes", "noname")
    if err := os.MkdirAll(noNameDir, 0o755); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(noNameDir, "noname.json"),
        []byte(`{"module":"crm"}`), 0o644); err != nil {
        t.Fatal(err)
    }

    h := api.NewDevHandler(dir, nil, nil)
    req := httptest.NewRequest("GET", "/api/v1/dev/doctype", nil)
    w := httptest.NewRecorder()
    h.HandleListDocTypes(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d", w.Code)
    }

    var resp struct {
        Data []api.DocTypeListItem `json:"data"`
    }
    if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
        t.Fatalf("decode: %v", err)
    }
    if len(resp.Data) != 1 || resp.Data[0].Name != "Good" {
        t.Fatalf("expected only Good, got %+v", resp.Data)
    }
}
```

- [ ] **Step 1.8: Run skip test — expect PASS**

Run: `go test -run TestDevHandler_ListDocTypes ./pkg/api/...`
Expected: all three PASS.

- [ ] **Step 1.9: Register the route**

In `pkg/api/dev_handler.go`, find `RegisterDevRoutes` (starts line 90). Add after the existing `mux.Handle("GET "+p+"/apps", ...)` line:

```go
mux.Handle("GET "+p+"/doctype", wrap(h.HandleListDocTypes))
```

Final block should look like:

```go
p := "/api/" + version + "/dev"
mux.Handle("GET "+p+"/apps", wrap(h.HandleListApps))
mux.Handle("GET "+p+"/doctype", wrap(h.HandleListDocTypes))
mux.Handle("POST "+p+"/doctype", wrap(h.HandleCreateDocType))
mux.Handle("PUT "+p+"/doctype/{name}", wrap(h.HandleUpdateDocType))
mux.Handle("GET "+p+"/doctype/{name}", wrap(h.HandleGetDocType))
mux.Handle("DELETE "+p+"/doctype/{name}", wrap(h.HandleDeleteDocType))
```

- [ ] **Step 1.10: Build to verify compilation**

Run: `go build ./pkg/api/...`
Expected: no errors.

- [ ] **Step 1.11: Commit**

```bash
cd /Users/osamamuhammed/Moca
git add pkg/api/dev_handler.go pkg/api/dev_handler_test.go
git commit -m "$(cat <<'EOF'
feat(dev-api): add GET /api/v1/dev/doctype list endpoint

Walks {appsDir}/*/modules/*/doctypes/*/{slug}.json and returns
{name, app, module, is_submittable, is_single, is_child_table,
is_virtual} for each. Malformed JSON and missing name are skipped.

Used by the new DocType Builder entry dialog to list existing
doctypes for editing.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Backend — Integration test for list endpoint

**Files:**
- Test: `pkg/api/dev_api_integration_test.go`

- [ ] **Step 2.1: Add integration test that creates, lists, then verifies**

Append to `pkg/api/dev_api_integration_test.go` (this file is guarded by `//go:build integration`). It uses the existing helpers in that file: `setupDevMux(t)` (returns `*http.ServeMux` + tempdir), `devRequest`, `withAdminCtx`, `withGuestCtx`, and `validDocTypeBody()` (which POSTs a DocType named `IntegTest`):

```go
func TestIntegration_DevAPI_ListDocTypes(t *testing.T) {
    mux, _ := setupDevMux(t)

    // Precondition: create a doctype so the list has something to return.
    postReq := withAdminCtx(devRequest("POST", "/api/v1/dev/doctype", validDocTypeBody()))
    postW := httptest.NewRecorder()
    mux.ServeHTTP(postW, postReq)
    if postW.Code != http.StatusOK && postW.Code != http.StatusCreated {
        t.Fatalf("setup POST returned %d: %s", postW.Code, postW.Body.String())
    }

    // List with admin — should succeed and include the newly-created IntegTest.
    listReq := withAdminCtx(devRequest("GET", "/api/v1/dev/doctype", nil))
    listW := httptest.NewRecorder()
    mux.ServeHTTP(listW, listReq)
    if listW.Code != http.StatusOK {
        t.Fatalf("list returned %d: %s", listW.Code, listW.Body.String())
    }

    var resp struct {
        Data []api.DocTypeListItem `json:"data"`
    }
    if err := json.NewDecoder(listW.Body).Decode(&resp); err != nil {
        t.Fatalf("decode: %v", err)
    }

    found := false
    for _, it := range resp.Data {
        if it.Name == "IntegTest" {
            found = true
            if it.App != "testapp" || it.Module != "core" {
                t.Fatalf("unexpected IntegTest entry: %+v", it)
            }
            break
        }
    }
    if !found {
        t.Fatalf("expected IntegTest in list, got: %+v", resp.Data)
    }

    // Guest should be rejected by DevAuthMiddleware.
    guestReq := withGuestCtx(devRequest("GET", "/api/v1/dev/doctype", nil))
    guestW := httptest.NewRecorder()
    mux.ServeHTTP(guestW, guestReq)
    if guestW.Code != http.StatusForbidden {
        t.Fatalf("expected guest to get 403, got %d", guestW.Code)
    }
}
```

- [ ] **Step 2.2: Run integration test**

Run: `cd /Users/osamamuhammed/Moca && go test -tags=integration -run TestIntegration_DevAPI_ListDocTypes ./pkg/api/...`
Expected: PASS.

- [ ] **Step 2.3: Commit**

```bash
git add pkg/api/dev_api_integration_test.go
git commit -m "$(cat <<'EOF'
test(dev-api): integration test for list doctypes

Covers the create→list→verify flow and guest-rejection for the new
GET /api/v1/dev/doctype endpoint.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Frontend types — Add `DocTypeListItem`

**Files:**
- Modify: `desk/src/api/types.ts`

- [ ] **Step 3.1: Read existing types file**

Run: `head -60 /Users/osamamuhammed/Moca/desk/src/api/types.ts`
Expected: understand naming conventions and where to insert.

- [ ] **Step 3.2: Add the type**

Append to `desk/src/api/types.ts`:

```ts
/**
 * One entry returned by GET /api/v1/dev/doctype, used by the DocType
 * Builder entry dialog to list existing doctypes.
 */
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

- [ ] **Step 3.3: Type-check the desk package**

Run: `cd /Users/osamamuhammed/Moca/desk && npx tsc --noEmit`
Expected: no new errors (pre-existing errors, if any, are unchanged).

- [ ] **Step 3.4: Commit**

```bash
cd /Users/osamamuhammed/Moca
git add desk/src/api/types.ts
git commit -m "$(cat <<'EOF'
feat(desk): add DocTypeListItem type

Mirrors the shape returned by GET /api/v1/dev/doctype.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Store — Add `stageNew` action (TDD)

**Files:**
- Modify: `desk/src/stores/doctype-builder-store.ts`
- Create: `desk/src/stores/doctype-builder-store.test.ts`

- [ ] **Step 4.1: Write failing test — `stageNew` populates store**

Create `desk/src/stores/doctype-builder-store.test.ts`:

```ts
import { beforeEach, describe, expect, it } from "vitest";

import { useDocTypeBuilderStore } from "./doctype-builder-store";

const baseSettings = {
  naming_rule: { rule: "uuid" as const },
  title_field: "",
  sort_field: "",
  sort_order: "desc" as const,
  search_fields: [],
  image_field: "",
  is_submittable: false,
  is_single: false,
  is_child_table: false,
  is_virtual: false,
  track_changes: true,
};

describe("doctype-builder-store.stageNew", () => {
  beforeEach(() => {
    useDocTypeBuilderStore.getState().reset();
  });

  it("populates name/app/module/settings and marks the doctype new", () => {
    useDocTypeBuilderStore.getState().stageNew({
      name: "Customer",
      app: "acme",
      module: "crm",
      settings: { ...baseSettings, is_submittable: true },
    });

    const s = useDocTypeBuilderStore.getState();
    expect(s.name).toBe("Customer");
    expect(s.app).toBe("acme");
    expect(s.module).toBe("crm");
    expect(s.settings.is_submittable).toBe(true);
    expect(s.isNew).toBe(true);
    expect(s.isDirty).toBe(false);
    expect(Object.keys(s.fields)).toHaveLength(0);
    expect(s.layout.tabs).toHaveLength(1);
    expect(s.layout.tabs[0]?.label).toBe("Details");
  });

  it("resets any prior state before staging", () => {
    // Pretend the store has stale data from a previous edit.
    useDocTypeBuilderStore.getState().hydrate({
      name: "Old",
      app: "old-app",
      module: "old",
      layout: {
        tabs: [{ label: "Old Tab", sections: [{ columns: [{ fields: [] }] }] }],
      },
      fields: {},
      settings: baseSettings,
    });
    expect(useDocTypeBuilderStore.getState().isNew).toBe(false);

    useDocTypeBuilderStore.getState().stageNew({
      name: "Fresh",
      app: "acme",
      module: "crm",
      settings: baseSettings,
    });

    const s = useDocTypeBuilderStore.getState();
    expect(s.name).toBe("Fresh");
    expect(s.isNew).toBe(true);
    expect(s.layout.tabs[0]?.label).toBe("Details");
  });

  it("keeps isNew=true after adding a field (first save is POST)", () => {
    useDocTypeBuilderStore.getState().stageNew({
      name: "Customer",
      app: "acme",
      module: "crm",
      settings: baseSettings,
    });
    useDocTypeBuilderStore.getState().addField("Data", 0, 0, 0);

    expect(useDocTypeBuilderStore.getState().isNew).toBe(true);
    expect(useDocTypeBuilderStore.getState().isDirty).toBe(true);
  });
});
```

- [ ] **Step 4.2: Run test to verify it fails**

Run: `cd /Users/osamamuhammed/Moca/desk && npx vitest run src/stores/doctype-builder-store.test.ts`
Expected: FAIL — `stageNew` is not a function.

- [ ] **Step 4.3: Add `stageNew` to the store interface**

In `desk/src/stores/doctype-builder-store.ts`, inside the `DocTypeBuilderState` interface, add a new method signature next to `hydrate`/`reset`:

```ts
  // ── Persistence ─────────────────────────────────────────────────────────
  markClean: () => void;
  hydrate: (data: HydratePayload) => void;
  stageNew: (payload: StageNewPayload) => void;
  reset: () => void;
```

Above `HydratePayload`, add the new payload type:

```ts
/** Payload accepted by stageNew() to start a new (not-yet-persisted) doctype. */
export interface StageNewPayload {
  name: string;
  app: string | null;
  module: string;
  settings: DocTypeSettings;
}
```

In the store factory body, after the `hydrate: (data) => ...` definition and before `reset`, add the implementation:

```ts
    stageNew: (payload) => {
      const fresh = initialState();
      set({
        ...fresh,
        name: payload.name,
        app: payload.app,
        module: payload.module,
        settings: payload.settings,
        isNew: true,
        isDirty: false,
      });
    },
```

- [ ] **Step 4.4: Run tests — expect PASS**

Run: `npx vitest run src/stores/doctype-builder-store.test.ts`
Expected: all three PASS.

- [ ] **Step 4.5: Commit**

```bash
cd /Users/osamamuhammed/Moca
git add desk/src/stores/doctype-builder-store.ts desk/src/stores/doctype-builder-store.test.ts
git commit -m "$(cat <<'EOF'
feat(desk): add stageNew action to doctype-builder-store

Used by the entry dialog's Create New form to populate the builder
with name/app/module/settings before the user enters the canvas.
Unlike hydrate(), stageNew sets isNew=true so the first canvas save
becomes a POST, not a PUT.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Component — `DocTypeList` (TDD)

**Files:**
- Create: `desk/src/components/doctype-builder/entry/DocTypeList.tsx`
- Create: `desk/src/components/doctype-builder/entry/DocTypeList.test.tsx`

- [ ] **Step 5.1: Write the failing test — renders list from API response**

Create `desk/src/components/doctype-builder/entry/DocTypeList.test.tsx`:

```tsx
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { DocTypeList } from "./DocTypeList";
import * as client from "@/api/client";

const sample = {
  data: [
    {
      name: "Customer",
      app: "acme",
      module: "crm",
      is_submittable: true,
      is_single: false,
      is_child_table: false,
      is_virtual: false,
    },
    {
      name: "Order Line",
      app: "acme",
      module: "crm",
      is_submittable: false,
      is_single: false,
      is_child_table: true,
      is_virtual: false,
    },
    {
      name: "User",
      app: "frappe",
      module: "core",
      is_submittable: false,
      is_single: false,
      is_child_table: false,
      is_virtual: false,
    },
  ],
};

describe("DocTypeList", () => {
  beforeEach(() => {
    vi.spyOn(client, "get").mockResolvedValue(sample);
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders each doctype from the API", async () => {
    render(<DocTypeList onOpen={() => {}} onBack={() => {}} />);
    await waitFor(() => expect(screen.getByText("Customer")).toBeTruthy());
    expect(screen.getByText("Order Line")).toBeTruthy();
    expect(screen.getByText("User")).toBeTruthy();
  });

  it("filters client-side by search query", async () => {
    render(<DocTypeList onOpen={() => {}} onBack={() => {}} />);
    await waitFor(() => expect(screen.getByText("Customer")).toBeTruthy());

    const input = screen.getByPlaceholderText(/search/i) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "order" } });

    await waitFor(() => {
      expect(screen.queryByText("Customer")).toBeNull();
      expect(screen.getByText("Order Line")).toBeTruthy();
      expect(screen.queryByText("User")).toBeNull();
    });
  });

  it("searches across name, module, and app", async () => {
    render(<DocTypeList onOpen={() => {}} onBack={() => {}} />);
    await waitFor(() => expect(screen.getByText("User")).toBeTruthy());

    const input = screen.getByPlaceholderText(/search/i) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "frappe" } });

    await waitFor(() => {
      expect(screen.queryByText("Customer")).toBeNull();
      expect(screen.getByText("User")).toBeTruthy();
    });
  });

  it("shows a no-match placeholder when nothing matches", async () => {
    render(<DocTypeList onOpen={() => {}} onBack={() => {}} />);
    await waitFor(() => expect(screen.getByText("Customer")).toBeTruthy());

    const input = screen.getByPlaceholderText(/search/i) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "zzzznothing" } });

    await waitFor(() => {
      expect(screen.getByText(/no doctypes match/i)).toBeTruthy();
    });
  });

  it("shows the empty-state placeholder when the API returns []", async () => {
    vi.spyOn(client, "get").mockResolvedValue({ data: [] });
    render(<DocTypeList onOpen={() => {}} onBack={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText(/no doctypes yet/i)).toBeTruthy();
    });
  });

  it("invokes onOpen with the doctype name on row click", async () => {
    const onOpen = vi.fn();
    render(<DocTypeList onOpen={onOpen} onBack={() => {}} />);
    await waitFor(() => expect(screen.getByText("Customer")).toBeTruthy());

    fireEvent.click(screen.getByText("Customer"));
    expect(onOpen).toHaveBeenCalledWith("Customer");
  });

  it("invokes onBack when the back button is clicked", async () => {
    const onBack = vi.fn();
    render(<DocTypeList onOpen={() => {}} onBack={onBack} />);
    await waitFor(() => expect(screen.getByText("Customer")).toBeTruthy());

    fireEvent.click(screen.getByLabelText(/back/i));
    expect(onBack).toHaveBeenCalled();
  });
});
```

- [ ] **Step 5.2: Run tests — expect FAIL**

Run: `cd /Users/osamamuhammed/Moca/desk && npx vitest run src/components/doctype-builder/entry/DocTypeList.test.tsx`
Expected: FAIL — module `./DocTypeList` not found.

- [ ] **Step 5.3: Implement `DocTypeList`**

Create `desk/src/components/doctype-builder/entry/DocTypeList.tsx`:

```tsx
import { useEffect, useMemo, useState } from "react";
import { ArrowLeft, Search } from "lucide-react";

import { get } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { ApiResponse, DocTypeListItem } from "@/api/types";

interface DocTypeListProps {
  onOpen: (name: string) => void;
  onBack: () => void;
}

export function DocTypeList({ onOpen, onBack }: DocTypeListProps) {
  const [items, setItems] = useState<DocTypeListItem[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");

  useEffect(() => {
    let cancelled = false;
    get<ApiResponse<DocTypeListItem[]>>("dev/doctype")
      .then((res) => {
        if (!cancelled) setItems(res.data);
      })
      .catch((e: unknown) => {
        if (cancelled) return;
        setError(e instanceof Error ? e.message : "Failed to load");
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const filtered = useMemo(() => {
    if (!items) return [];
    const q = query.trim().toLowerCase();
    if (!q) return items;
    return items.filter(
      (it) =>
        it.name.toLowerCase().includes(q) ||
        it.module.toLowerCase().includes(q) ||
        it.app.toLowerCase().includes(q),
    );
  }, [items, query]);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Back"
          onClick={onBack}
        >
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <h3 className="text-base font-medium">Open Existing DocType</h3>
      </div>

      <div className="relative">
        <Search className="absolute start-2 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          autoFocus
          placeholder="Search doctypes…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="ps-8"
        />
      </div>

      {error && (
        <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
          Couldn&apos;t load doctypes: {error}
        </div>
      )}

      {items === null && !error && (
        <div className="py-8 text-center text-sm text-muted-foreground">
          Loading…
        </div>
      )}

      {items !== null && items.length === 0 && (
        <div className="flex flex-col items-center gap-2 py-8 text-center">
          <p className="text-sm text-muted-foreground">
            No DocTypes yet. Create your first one.
          </p>
        </div>
      )}

      {items !== null && items.length > 0 && filtered.length === 0 && (
        <div className="py-8 text-center text-sm text-muted-foreground">
          No doctypes match &quot;{query}&quot;
        </div>
      )}

      {filtered.length > 0 && (
        <ul className="max-h-80 divide-y overflow-y-auto rounded-md border">
          {filtered.map((it) => (
            <li
              key={`${it.app}.${it.module}.${it.name}`}
              onClick={() => onOpen(it.name)}
              className="flex cursor-pointer items-center justify-between px-3 py-2 hover:bg-accent"
            >
              <div className="flex items-center gap-2">
                <span className="font-medium">{it.name}</span>
                {it.is_submittable && (
                  <span className="rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
                    Submittable
                  </span>
                )}
                {it.is_single && (
                  <span className="rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
                    Single
                  </span>
                )}
                {it.is_child_table && (
                  <span className="rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
                    Child Table
                  </span>
                )}
                {it.is_virtual && (
                  <span className="rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
                    Virtual
                  </span>
                )}
              </div>
              <span className="text-xs text-muted-foreground">
                {it.module} · {it.app}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
```

- [ ] **Step 5.4: Run tests — expect PASS**

Run: `npx vitest run src/components/doctype-builder/entry/DocTypeList.test.tsx`
Expected: all seven PASS.

- [ ] **Step 5.5: Commit**

```bash
cd /Users/osamamuhammed/Moca
git add desk/src/components/doctype-builder/entry/DocTypeList.tsx desk/src/components/doctype-builder/entry/DocTypeList.test.tsx
git commit -m "$(cat <<'EOF'
feat(desk): add DocTypeList component for entry dialog

Fetches GET /api/v1/dev/doctype and renders a searchable list
with kind badges. Client-side filter across name/module/app.
Empty-state and no-match placeholders included.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Component — `CreateDocTypeForm` (TDD)

**Files:**
- Create: `desk/src/components/doctype-builder/entry/CreateDocTypeForm.tsx`
- Create: `desk/src/components/doctype-builder/entry/CreateDocTypeForm.test.tsx`

- [ ] **Step 6.1: Write failing tests**

Create `desk/src/components/doctype-builder/entry/CreateDocTypeForm.test.tsx`:

```tsx
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { CreateDocTypeForm } from "./CreateDocTypeForm";
import * as client from "@/api/client";

const appsResponse = {
  data: [
    { name: "acme", modules: ["crm", "sales"] },
    { name: "frappe", modules: ["core"] },
  ],
};

describe("CreateDocTypeForm", () => {
  beforeEach(() => {
    vi.spyOn(client, "get").mockImplementation(async (path: string) => {
      if (path === "dev/apps") return appsResponse;
      if (path.startsWith("dev/doctype/")) {
        // Default: name is available (404 → MocaApiError with status 404).
        throw new client.MocaApiError(404, { code: "NOT_FOUND", message: "not found" });
      }
      throw new Error(`unexpected path ${path}`);
    });
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  async function fillName(value: string) {
    const name = screen.getByLabelText(/name/i) as HTMLInputElement;
    fireEvent.change(name, { target: { value } });
    // Wait for debounced availability check
    await new Promise((r) => setTimeout(r, 400));
  }

  it("disables Save & Continue until name, app, and module are chosen", async () => {
    render(<CreateDocTypeForm onStage={() => {}} onBack={() => {}} />);
    await waitFor(() => expect(screen.getByLabelText(/app/i)).toBeTruthy());

    const save = screen.getByRole("button", { name: /save & continue/i });
    expect(save).toBeDisabled();

    await fillName("Customer");
    fireEvent.change(screen.getByLabelText(/app/i), { target: { value: "acme" } });
    fireEvent.change(screen.getByLabelText(/module/i), { target: { value: "crm" } });

    await waitFor(() => expect(save).not.toBeDisabled());
  });

  it("changing App clears the Module selection", async () => {
    render(<CreateDocTypeForm onStage={() => {}} onBack={() => {}} />);
    await waitFor(() => expect(screen.getByLabelText(/app/i)).toBeTruthy());

    fireEvent.change(screen.getByLabelText(/app/i), { target: { value: "acme" } });
    fireEvent.change(screen.getByLabelText(/module/i), { target: { value: "crm" } });
    expect((screen.getByLabelText(/module/i) as HTMLSelectElement).value).toBe("crm");

    fireEvent.change(screen.getByLabelText(/app/i), { target: { value: "frappe" } });
    expect((screen.getByLabelText(/module/i) as HTMLSelectElement).value).toBe("");
  });

  it("shows taken indicator when name-availability GET returns 200", async () => {
    vi.spyOn(client, "get").mockImplementation(async (path: string) => {
      if (path === "dev/apps") return appsResponse;
      if (path.startsWith("dev/doctype/")) return { data: {} };
      throw new Error("x");
    });

    render(<CreateDocTypeForm onStage={() => {}} onBack={() => {}} />);
    await waitFor(() => expect(screen.getByLabelText(/app/i)).toBeTruthy());

    await fillName("Existing");
    await waitFor(() =>
      expect(screen.getByText(/name is taken/i)).toBeTruthy(),
    );
  });

  it("Submittable type maps to correct flags in staged payload", async () => {
    const onStage = vi.fn();
    render(<CreateDocTypeForm onStage={onStage} onBack={() => {}} />);
    await waitFor(() => expect(screen.getByLabelText(/app/i)).toBeTruthy());

    await fillName("Customer");
    fireEvent.change(screen.getByLabelText(/app/i), { target: { value: "acme" } });
    fireEvent.change(screen.getByLabelText(/module/i), { target: { value: "crm" } });
    fireEvent.change(screen.getByLabelText(/type/i), { target: { value: "Submittable" } });

    fireEvent.click(screen.getByRole("button", { name: /save & continue/i }));

    expect(onStage).toHaveBeenCalledTimes(1);
    const payload = onStage.mock.calls[0]![0];
    expect(payload.name).toBe("Customer");
    expect(payload.app).toBe("acme");
    expect(payload.module).toBe("crm");
    expect(payload.settings.is_submittable).toBe(true);
    expect(payload.settings.is_single).toBe(false);
    expect(payload.settings.is_child_table).toBe(false);
    expect(payload.settings.is_virtual).toBe(false);
    expect(payload.settings.track_changes).toBe(true);
    expect(payload.settings.naming_rule).toEqual({ rule: "uuid" });
  });

  it("Normal type leaves all kind flags false", async () => {
    const onStage = vi.fn();
    render(<CreateDocTypeForm onStage={onStage} onBack={() => {}} />);
    await waitFor(() => expect(screen.getByLabelText(/app/i)).toBeTruthy());

    await fillName("Thing");
    fireEvent.change(screen.getByLabelText(/app/i), { target: { value: "acme" } });
    fireEvent.change(screen.getByLabelText(/module/i), { target: { value: "crm" } });
    // Type default is Normal; don't change it.

    fireEvent.click(screen.getByRole("button", { name: /save & continue/i }));

    const payload = onStage.mock.calls[0]![0];
    expect(payload.settings.is_submittable).toBe(false);
    expect(payload.settings.is_single).toBe(false);
    expect(payload.settings.is_child_table).toBe(false);
    expect(payload.settings.is_virtual).toBe(false);
  });

  it("invokes onBack when back button clicked", async () => {
    const onBack = vi.fn();
    render(<CreateDocTypeForm onStage={() => {}} onBack={onBack} />);
    await waitFor(() => expect(screen.getByLabelText(/app/i)).toBeTruthy());

    fireEvent.click(screen.getByLabelText(/back/i));
    expect(onBack).toHaveBeenCalled();
  });
});
```

- [ ] **Step 6.2: Run tests — expect FAIL**

Run: `cd /Users/osamamuhammed/Moca/desk && npx vitest run src/components/doctype-builder/entry/CreateDocTypeForm.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 6.3: Implement `CreateDocTypeForm`**

Create `desk/src/components/doctype-builder/entry/CreateDocTypeForm.tsx`:

```tsx
import { useEffect, useMemo, useRef, useState } from "react";
import { ArrowLeft, Check, X as XIcon } from "lucide-react";

import { get, MocaApiError } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { ApiResponse } from "@/api/types";
import type { StageNewPayload } from "@/stores/doctype-builder-store";
import { DEFAULT_SETTINGS } from "@/components/doctype-builder/types";

type TypeOption =
  | "Normal"
  | "Submittable"
  | "Single"
  | "Child Table"
  | "Virtual";

const TYPE_OPTIONS: { value: TypeOption; help: string }[] = [
  { value: "Normal", help: "A standard doctype with a table and list view." },
  { value: "Submittable", help: "Has a submit/cancel lifecycle." },
  { value: "Single", help: "Exactly one row, no list view (settings-like)." },
  { value: "Child Table", help: "Embedded inside parent doctypes via Table fields." },
  { value: "Virtual", help: "No database table — all logic is code." },
];

function validateName(name: string): string | null {
  if (!name) return null;
  if (!/^[A-Z]/.test(name)) return "Name must start with an uppercase letter";
  if (!/^[A-Za-z0-9]+$/.test(name))
    return "Name must contain only letters and digits (no spaces or underscores)";
  return null;
}

function flagsFor(type: TypeOption) {
  return {
    is_submittable: type === "Submittable",
    is_single: type === "Single",
    is_child_table: type === "Child Table",
    is_virtual: type === "Virtual",
  };
}

interface CreateDocTypeFormProps {
  onStage: (payload: StageNewPayload) => void;
  onBack: () => void;
}

export function CreateDocTypeForm({ onStage, onBack }: CreateDocTypeFormProps) {
  const [name, setName] = useState("");
  const [app, setApp] = useState("");
  const [moduleName, setModuleName] = useState("");
  const [type, setType] = useState<TypeOption>("Normal");
  const [apps, setApps] = useState<{ name: string; modules: string[] }[]>([]);
  const [appsError, setAppsError] = useState<string | null>(null);
  const [availability, setAvailability] = useState<
    "idle" | "checking" | "available" | "taken"
  >("idle");

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    get<ApiResponse<{ name: string; modules: string[] }[]>>("dev/apps")
      .then((res) => setApps(res.data))
      .catch((e: unknown) => {
        setAppsError(e instanceof Error ? e.message : "Failed to load apps");
      });
  }, []);

  // Inline name availability check (debounced 300 ms)
  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    const err = validateName(name);
    if (!name || err) {
      setAvailability("idle");
      return;
    }
    setAvailability("checking");
    debounceRef.current = setTimeout(() => {
      get(`dev/doctype/${name}`)
        .then(() => setAvailability("taken"))
        .catch((e: unknown) => {
          if (e instanceof MocaApiError && e.status === 404) {
            setAvailability("available");
          } else {
            setAvailability("idle");
          }
        });
    }, 300);
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, [name]);

  const moduleOptions = useMemo(
    () => apps.find((a) => a.name === app)?.modules ?? [],
    [apps, app],
  );

  const nameError = validateName(name);
  const canSubmit =
    !!name &&
    !nameError &&
    availability !== "taken" &&
    !!app &&
    !!moduleName;

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    onStage({
      name,
      app,
      module: moduleName,
      settings: {
        ...DEFAULT_SETTINGS,
        search_fields: [],
        ...flagsFor(type),
      },
    });
  }

  return (
    <form className="flex flex-col gap-3" onSubmit={handleSubmit}>
      <div className="flex items-center gap-2">
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label="Back"
          onClick={onBack}
        >
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <h3 className="text-base font-medium">Create New DocType</h3>
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor="new-doctype-name">Name</Label>
        <div className="relative">
          <Input
            id="new-doctype-name"
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Customer"
          />
          {availability === "available" && (
            <Check className="absolute end-2 top-1/2 h-4 w-4 -translate-y-1/2 text-green-600" />
          )}
          {availability === "taken" && (
            <XIcon className="absolute end-2 top-1/2 h-4 w-4 -translate-y-1/2 text-destructive" />
          )}
        </div>
        {nameError && (
          <span className="text-xs text-destructive">{nameError}</span>
        )}
        {availability === "taken" && (
          <span className="text-xs text-destructive">Name is taken</span>
        )}
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor="new-doctype-app">App</Label>
        <select
          id="new-doctype-app"
          value={app}
          onChange={(e) => {
            setApp(e.target.value);
            setModuleName("");
          }}
          className="h-9 rounded-md border bg-background px-2 text-sm"
        >
          <option value="">Select an app…</option>
          {apps.map((a) => (
            <option key={a.name} value={a.name}>
              {a.name}
            </option>
          ))}
        </select>
        {appsError && (
          <span className="text-xs text-destructive">{appsError}</span>
        )}
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor="new-doctype-module">Module</Label>
        <select
          id="new-doctype-module"
          value={moduleName}
          onChange={(e) => setModuleName(e.target.value)}
          disabled={!app}
          className="h-9 rounded-md border bg-background px-2 text-sm disabled:opacity-50"
        >
          <option value="">Select a module…</option>
          {moduleOptions.map((m) => (
            <option key={m} value={m}>
              {m}
            </option>
          ))}
        </select>
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor="new-doctype-type">Type</Label>
        <select
          id="new-doctype-type"
          value={type}
          onChange={(e) => setType(e.target.value as TypeOption)}
          className="h-9 rounded-md border bg-background px-2 text-sm"
        >
          {TYPE_OPTIONS.map((o) => (
            <option key={o.value} value={o.value}>
              {o.value}
            </option>
          ))}
        </select>
        <span className="text-xs text-muted-foreground">
          {TYPE_OPTIONS.find((o) => o.value === type)?.help}
        </span>
      </div>

      <div className="mt-2 flex justify-end">
        <Button type="submit" disabled={!canSubmit}>
          Save &amp; Continue
        </Button>
      </div>
    </form>
  );
}
```

- [ ] **Step 6.4: Run tests — expect PASS**

Run: `npx vitest run src/components/doctype-builder/entry/CreateDocTypeForm.test.tsx`
Expected: all six PASS.

- [ ] **Step 6.5: Commit**

```bash
cd /Users/osamamuhammed/Moca
git add desk/src/components/doctype-builder/entry/CreateDocTypeForm.tsx desk/src/components/doctype-builder/entry/CreateDocTypeForm.test.tsx
git commit -m "$(cat <<'EOF'
feat(desk): add CreateDocTypeForm for entry dialog

Collects name (with debounced availability check), app/module
(fetched from GET /dev/apps), and a Type dropdown that maps to
the four kind flags. Save & Continue calls onStage with the
populated StageNewPayload.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Component — `EntryLanding`

**Files:**
- Create: `desk/src/components/doctype-builder/entry/EntryLanding.tsx`

Note: `EntryLanding` is a pure presentational component (two buttons that call props). No test file for this one — its behavior is exercised through `DocTypeEntryDialog.test.tsx`.

- [ ] **Step 7.1: Implement `EntryLanding`**

Create `desk/src/components/doctype-builder/entry/EntryLanding.tsx`:

```tsx
import { FileEdit, Plus } from "lucide-react";

interface EntryLandingProps {
  onCreate: () => void;
  onOpen: () => void;
}

export function EntryLanding({ onCreate, onOpen }: EntryLandingProps) {
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-1">
        <h2 className="text-base font-medium">DocType Builder</h2>
        <p className="text-sm text-muted-foreground">
          Create a new DocType or edit an existing one.
        </p>
      </div>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <button
          type="button"
          onClick={onCreate}
          className="flex flex-col items-center gap-2 rounded-lg border-2 border-dashed p-6 transition hover:border-primary hover:bg-accent"
        >
          <Plus className="h-8 w-8 text-muted-foreground" />
          <span className="font-medium">Create New DocType</span>
          <span className="text-xs text-muted-foreground">
            Start from scratch
          </span>
        </button>

        <button
          type="button"
          onClick={onOpen}
          className="flex flex-col items-center gap-2 rounded-lg border-2 border-dashed p-6 transition hover:border-primary hover:bg-accent"
        >
          <FileEdit className="h-8 w-8 text-muted-foreground" />
          <span className="font-medium">Edit Existing DocType</span>
          <span className="text-xs text-muted-foreground">
            Pick from your doctypes
          </span>
        </button>
      </div>
    </div>
  );
}
```

- [ ] **Step 7.2: Type-check**

Run: `cd /Users/osamamuhammed/Moca/desk && npx tsc --noEmit`
Expected: no new errors.

- [ ] **Step 7.3: Commit**

```bash
cd /Users/osamamuhammed/Moca
git add desk/src/components/doctype-builder/entry/EntryLanding.tsx
git commit -m "$(cat <<'EOF'
feat(desk): add EntryLanding component for entry dialog

Two-card landing that switches the dialog to Create or Open view.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Component — `DocTypeEntryDialog` (TDD)

**Files:**
- Create: `desk/src/components/doctype-builder/entry/DocTypeEntryDialog.tsx`
- Create: `desk/src/components/doctype-builder/entry/DocTypeEntryDialog.test.tsx`

- [ ] **Step 8.1: Write failing tests**

Create `desk/src/components/doctype-builder/entry/DocTypeEntryDialog.test.tsx`:

```tsx
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { DocTypeEntryDialog } from "./DocTypeEntryDialog";
import * as client from "@/api/client";

describe("DocTypeEntryDialog", () => {
  beforeEach(() => {
    vi.spyOn(client, "get").mockImplementation(async (path: string) => {
      if (path === "dev/apps") return { data: [] };
      if (path === "dev/doctype") return { data: [] };
      throw new client.MocaApiError(404, { code: "NOT_FOUND", message: "x" });
    });
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("shows the landing by default", () => {
    render(
      <DocTypeEntryDialog open onStage={() => {}} onOpenExisting={() => {}} />,
    );
    expect(screen.getByText(/create new doctype/i)).toBeTruthy();
    expect(screen.getByText(/edit existing doctype/i)).toBeTruthy();
  });

  it("does not close on Escape", () => {
    render(
      <DocTypeEntryDialog open onStage={() => {}} onOpenExisting={() => {}} />,
    );
    fireEvent.keyDown(document.body, { key: "Escape" });
    // Landing still visible
    expect(screen.getByText(/create new doctype/i)).toBeTruthy();
  });

  it("switches to Create view when the Create card is clicked", async () => {
    render(
      <DocTypeEntryDialog open onStage={() => {}} onOpenExisting={() => {}} />,
    );
    fireEvent.click(screen.getByText(/create new doctype/i));
    await waitFor(() =>
      expect(screen.getByLabelText(/^name$/i)).toBeTruthy(),
    );
  });

  it("switches to Open view when the Open card is clicked", async () => {
    render(
      <DocTypeEntryDialog open onStage={() => {}} onOpenExisting={() => {}} />,
    );
    fireEvent.click(screen.getByText(/edit existing doctype/i));
    await waitFor(() =>
      expect(screen.getByPlaceholderText(/search/i)).toBeTruthy(),
    );
  });

  it("Back from Create view returns to Landing", async () => {
    render(
      <DocTypeEntryDialog open onStage={() => {}} onOpenExisting={() => {}} />,
    );
    fireEvent.click(screen.getByText(/create new doctype/i));
    await waitFor(() => expect(screen.getByLabelText(/^name$/i)).toBeTruthy());

    fireEvent.click(screen.getByLabelText(/back/i));
    await waitFor(() =>
      expect(screen.getByText(/edit existing doctype/i)).toBeTruthy(),
    );
  });

  it("Back from Open view returns to Landing", async () => {
    render(
      <DocTypeEntryDialog open onStage={() => {}} onOpenExisting={() => {}} />,
    );
    fireEvent.click(screen.getByText(/edit existing doctype/i));
    await waitFor(() =>
      expect(screen.getByPlaceholderText(/search/i)).toBeTruthy(),
    );

    fireEvent.click(screen.getByLabelText(/back/i));
    await waitFor(() =>
      expect(screen.getByText(/create new doctype/i)).toBeTruthy(),
    );
  });
});
```

- [ ] **Step 8.2: Run tests — expect FAIL**

Run: `cd /Users/osamamuhammed/Moca/desk && npx vitest run src/components/doctype-builder/entry/DocTypeEntryDialog.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 8.3: Implement `DocTypeEntryDialog`**

Create `desk/src/components/doctype-builder/entry/DocTypeEntryDialog.tsx`:

```tsx
import { useState } from "react";
import { Dialog as DialogPrimitive } from "radix-ui";

import { cn } from "@/lib/utils";
import { CreateDocTypeForm } from "./CreateDocTypeForm";
import { DocTypeList } from "./DocTypeList";
import { EntryLanding } from "./EntryLanding";
import type { StageNewPayload } from "@/stores/doctype-builder-store";

type View = "landing" | "create" | "open";

interface DocTypeEntryDialogProps {
  open: boolean;
  onStage: (payload: StageNewPayload) => void;
  onOpenExisting: (name: string) => void;
}

export function DocTypeEntryDialog({
  open,
  onStage,
  onOpenExisting,
}: DocTypeEntryDialogProps) {
  const [view, setView] = useState<View>("landing");

  // Swallow both Escape and overlay-click so the dialog is non-dismissible.
  // The only way out is to pick an action or navigate via the sidebar.
  return (
    <DialogPrimitive.Root open={open}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay
          className={cn(
            "fixed inset-0 isolate z-50 bg-black/10 duration-100",
            "supports-backdrop-filter:backdrop-blur-xs",
            "data-open:animate-in data-open:fade-in-0",
          )}
        />
        <DialogPrimitive.Content
          onEscapeKeyDown={(e) => e.preventDefault()}
          onPointerDownOutside={(e) => e.preventDefault()}
          onInteractOutside={(e) => e.preventDefault()}
          className={cn(
            "fixed top-1/2 start-1/2 z-50 grid w-full max-w-[calc(100%-2rem)] -translate-x-1/2 rtl:translate-x-1/2 -translate-y-1/2 gap-4 rounded-xl bg-popover p-4 text-sm text-popover-foreground ring-1 ring-foreground/10 duration-100 outline-none sm:max-w-md",
            "data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95",
          )}
        >
          <DialogPrimitive.Title className="sr-only">
            DocType Builder
          </DialogPrimitive.Title>
          <DialogPrimitive.Description className="sr-only">
            Create a new DocType or edit an existing one.
          </DialogPrimitive.Description>

          {view === "landing" && (
            <EntryLanding
              onCreate={() => setView("create")}
              onOpen={() => setView("open")}
            />
          )}
          {view === "create" && (
            <CreateDocTypeForm
              onStage={onStage}
              onBack={() => setView("landing")}
            />
          )}
          {view === "open" && (
            <DocTypeList
              onOpen={onOpenExisting}
              onBack={() => setView("landing")}
            />
          )}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
```

- [ ] **Step 8.4: Run tests — expect PASS**

Run: `npx vitest run src/components/doctype-builder/entry/DocTypeEntryDialog.test.tsx`
Expected: all six PASS.

- [ ] **Step 8.5: Commit**

```bash
cd /Users/osamamuhammed/Moca
git add desk/src/components/doctype-builder/entry/DocTypeEntryDialog.tsx desk/src/components/doctype-builder/entry/DocTypeEntryDialog.test.tsx
git commit -m "$(cat <<'EOF'
feat(desk): add DocTypeEntryDialog non-dismissible modal

Owns view state (landing/create/open) and composes EntryLanding,
CreateDocTypeForm, and DocTypeList. Escape and overlay-click are
no-ops — the only exit is sidebar navigation or picking an action.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Wire into `DocTypeBuilder` page

**Files:**
- Modify: `desk/src/pages/DocTypeBuilder.tsx`

- [ ] **Step 9.1: Read the current page**

Run: `head -80 /Users/osamamuhammed/Moca/desk/src/pages/DocTypeBuilder.tsx`
Expected: confirm `useParams`, `useNavigate`, and `store.reset()` effect.

- [ ] **Step 9.2: Add entry-dialog state + handlers**

In `desk/src/pages/DocTypeBuilder.tsx`, add the import near the other `@/components` imports:

```ts
import { DocTypeEntryDialog } from "@/components/doctype-builder/entry/DocTypeEntryDialog";
```

Inside the `DocTypeBuilder` function component, after the `useParams` + `useNavigate` lines (and alongside other local state), add:

```ts
  const [showEntryDialog, setShowEntryDialog] = useState<boolean>(!urlName);
```

**Remove** the existing `useEffect` that resets the store on `!urlName`:

```ts
  // DELETE THIS BLOCK (existing, around line 109):
  useEffect(() => {
    if (!urlName) {
      store.reset();
    }
  }, [urlName]);
```

**Replace** it with a single effect that handles both URL directions — closes the dialog and hydrates when a name appears, resets and re-opens when the name disappears:

```ts
  // Drive the dialog from the URL: deep-link with :name closes the dialog,
  // navigation back to /doctype-builder (no name) reopens it and resets the
  // store. Calls to setShowEntryDialog(false) inside onStage do NOT trigger
  // this effect because urlName did not change.
  useEffect(() => {
    if (urlName) {
      setShowEntryDialog(false);
    } else {
      store.reset();
      setShowEntryDialog(true);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [urlName]);
```

In the render section, wrap the existing `<DndContext>…</DndContext>` in a fragment and render the dialog beside it:

```tsx
  return (
    <>
      <DocTypeEntryDialog
        open={showEntryDialog}
        onStage={(payload) => {
          store.stageNew(payload);
          setShowEntryDialog(false);
        }}
        onOpenExisting={(name) => {
          navigate(`/desk/app/doctype-builder/${name}`);
          // The URL change triggers the effect above, which closes the dialog.
        }}
      />
      <DndContext collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
        {/* ...existing BuilderShell... */}
      </DndContext>
    </>
  );
```

(The existing JSX inside `<DndContext>…</DndContext>` stays unchanged.)

- [ ] **Step 9.3: Type-check + dev-build**

Run: `cd /Users/osamamuhammed/Moca/desk && npx tsc --noEmit`
Expected: no new errors.

Run: `npx vitest run`
Expected: all tests pass (store, all three new components, and the existing `AuthProvider.test.tsx`).

- [ ] **Step 9.4: Manual smoke test**

Run the dev server and verify in a browser:

```bash
cd /Users/osamamuhammed/Moca/desk && npm run dev
```

Then navigate to:

1. `/desk/app/doctype-builder` — modal must appear, blank canvas behind it.
2. Click **Create New DocType** → form appears; fill in Name/App/Module → Save & Continue → modal closes and canvas shows the name in the toolbar. Add one field; hit `Cmd+S` → doctype should be written to `apps/{app}/modules/{module}/doctypes/{slug}/{slug}.json` and registered in the registry (check the server logs).
3. Back to `/desk/app/doctype-builder` → click **Edit Existing DocType** → list appears → click a row → URL changes to `/desk/app/doctype-builder/{name}` → modal disappears → canvas hydrates.
4. Navigate directly to `/desk/app/doctype-builder/User` (or any existing doctype) → **no modal** should appear; canvas loads hydrated.
5. In the modal, press **Escape** — nothing happens (non-dismissible). Click outside the modal — also nothing.

- [ ] **Step 9.5: Commit**

```bash
git add desk/src/pages/DocTypeBuilder.tsx
git commit -m "$(cat <<'EOF'
feat(desk): mount DocTypeEntryDialog on the builder page

Modal appears when the URL has no :name; deep-links skip it.
Create form calls store.stageNew and closes; list row click
navigates to /doctype-builder/:name, which closes the modal
on the next render via useEffect.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Documentation updates

**Files:**
- Modify: `docs/superpowers/specs/2026-04-13-doctype-builder-design.md`
- Modify: `docs/MOCA_SYSTEM_DESIGN.md` (if relevant section exists)
- Modify: `wiki/` pages (if relevant)

- [ ] **Step 10.1: Cross-reference the entry-flow spec**

Open `docs/superpowers/specs/2026-04-13-doctype-builder-design.md`. At the end of the overview/intro section (or in a "Related" block at the top), add one line:

```markdown
> **Related:** The builder's entry flow (what happens when the user opens `/desk/app/doctype-builder`) is specified separately in [`2026-04-17-doctype-builder-entry-flow-design.md`](2026-04-17-doctype-builder-entry-flow-design.md).
```

- [ ] **Step 10.2: Update the system design doc if it describes the old flow**

Run: `grep -n "doctype-builder\|DocType Builder" /Users/osamamuhammed/Moca/docs/MOCA_SYSTEM_DESIGN.md | head -10`

If any section describes the "direct to canvas on arrival" flow, update that passage to describe the new modal-first flow and link to the entry-flow spec. If no such section exists, skip this step.

- [ ] **Step 10.3: Update the wiki page if present**

Run: `ls /Users/osamamuhammed/Moca/wiki/ | grep -i "doctype\|builder" 2>/dev/null || echo "no relevant wiki page"`

If a page exists, edit it to describe the new entry flow (a short paragraph with the state-machine diagram from the spec is enough). Commit inside the submodule:

```bash
cd /Users/osamamuhammed/Moca/wiki
git add <page>.md
git commit -m "docs: DocType Builder entry flow"
cd /Users/osamamuhammed/Moca
git add wiki
```

If no page exists, skip this step.

- [ ] **Step 10.4: Commit docs updates**

```bash
cd /Users/osamamuhammed/Moca
git add docs/superpowers/specs/2026-04-13-doctype-builder-design.md docs/MOCA_SYSTEM_DESIGN.md wiki 2>/dev/null || true
git status  # verify what's staged
git commit -m "$(cat <<'EOF'
docs: update DocType Builder docs for entry-flow feature

Cross-references the entry-flow spec from the builder's main
design doc; updates system design and wiki (if applicable) to
describe the modal-first arrival flow.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: Final verification

- [ ] **Step 11.1: Full backend test run**

Run: `cd /Users/osamamuhammed/Moca && make test`
Expected: all tests pass (including the new `TestDevHandler_ListDocTypes_*` tests).

- [ ] **Step 11.2: Lint**

Run: `make lint`
Expected: no new warnings from the files touched.

- [ ] **Step 11.3: Full frontend test run**

Run: `cd /Users/osamamuhammed/Moca/desk && npx vitest run`
Expected: all tests pass (store + 3 component files + pre-existing `AuthProvider.test.tsx`).

- [ ] **Step 11.4: Integration test run (optional but recommended)**

Run: `cd /Users/osamamuhammed/Moca && make test-api-integration`
Expected: all integration tests pass (including the new `TestDevHandler_ListDocTypes_Integration`).

- [ ] **Step 11.5: Announce completion**

The feature is complete. All acceptance criteria from the spec are covered:
- Non-dismissible modal on `/doctype-builder` (Tasks 8, 9)
- Deep-link bypass on `/doctype-builder/:name` (Task 9)
- Create New form with Name/App/Module/Type (Task 6)
- Staged creation — no backend write until canvas Save (Task 4)
- Open Existing list with search (Task 5)
- New backend endpoint (Tasks 1, 2)
- Docs updated (Task 10)

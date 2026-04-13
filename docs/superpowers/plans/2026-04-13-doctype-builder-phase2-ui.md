# DocType Builder — Phase 2: Builder UI

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the DocType Builder UI — a canvas-first, drag-and-drop visual editor in the Moca Desk for creating and editing DocTypes. Includes the reusable BuilderKit shell framework and schematic/preview dual-mode canvas.

**Architecture:** BuilderKit provides the shell (toolbar, drawers, property panel, undo/redo). The DocType Builder plugs its own canvas, field palette, and persistence logic into BuilderKit. State managed by Zustand store. DnD via @dnd-kit. All standard UI primitives from shadcn/ui.

**Tech Stack:** React 19, TypeScript, Vite, TailwindCSS, shadcn/ui, @dnd-kit/core, @dnd-kit/sortable, Zustand, TanStack Query

**Spec:** `docs/superpowers/specs/2026-04-13-doctype-builder-design.md`
**Depends on:** Phase 1 (backend storage & API) must be complete.

---

## File Structure

### New Files — BuilderKit (reusable)
| File | Purpose |
|------|---------|
| `desk/src/components/builder-kit/types.ts` | Shared types: Command, CommandHistory, PropertySchema, PropertyDef |
| `desk/src/components/builder-kit/useCommandHistory.ts` | Generic undo/redo hook (command pattern) |
| `desk/src/components/builder-kit/useCommandHistory.test.ts` | Tests for command history |
| `desk/src/components/builder-kit/BuilderShell.tsx` | Shell layout: toolbar + icon rail + canvas area + drawer slots |
| `desk/src/components/builder-kit/BuilderToolbar.tsx` | Top bar: name, actions, mode toggle |
| `desk/src/components/builder-kit/DrawerPanel.tsx` | Left-side slide-out drawer (wraps shadcn Sheet) |
| `desk/src/components/builder-kit/PropertyPanel.tsx` | Right-side slide-out property editor (schema-driven) |

### New Files — DocType Builder
| File | Purpose |
|------|---------|
| `desk/src/pages/DocTypeBuilder.tsx` | Main builder page component |
| `desk/src/stores/doctype-builder-store.ts` | Zustand store for builder state |
| `desk/src/components/doctype-builder/types.ts` | Builder-specific types (BuilderState, SelectedNode, etc.) |
| `desk/src/components/doctype-builder/field-type-categories.ts` | Field type groupings for the palette |
| `desk/src/components/doctype-builder/property-schemas.ts` | PropertySchema definitions per field type |
| `desk/src/components/doctype-builder/commands.ts` | Command implementations (AddField, MoveField, etc.) |
| `desk/src/components/doctype-builder/validation.ts` | Client-side validation rules |
| `desk/src/components/doctype-builder/FieldPalette.tsx` | Draggable field type grid in left drawer |
| `desk/src/components/doctype-builder/SchematicCanvas.tsx` | Main canvas rendering the layout tree |
| `desk/src/components/doctype-builder/TabBar.tsx` | Tab bar with sortable tabs + add button |
| `desk/src/components/doctype-builder/SectionNode.tsx` | Section container with header + columns |
| `desk/src/components/doctype-builder/ColumnNode.tsx` | Column container with sortable field cards |
| `desk/src/components/doctype-builder/FieldCard.tsx` | Compact field card (drag handle, name, type badge) |
| `desk/src/components/doctype-builder/SettingsDrawer.tsx` | DocType settings form |
| `desk/src/components/doctype-builder/PermissionsDrawer.tsx` | Permission matrix editor |
| `desk/src/components/doctype-builder/PreviewMode.tsx` | Live preview using existing FormView components |

### Modified Files
| File | Change |
|------|--------|
| `desk/src/router.tsx` | Add `/desk/app/doctype-builder/:name?` route |
| `desk/src/components/shell/Sidebar.tsx` | Add "DocType Builder" link under Core module |
| `desk/src/components/shell/CommandPalette.tsx` | Add "New DocType" action |
| `desk/package.json` | Add @dnd-kit/core, @dnd-kit/sortable, zustand |

---

### Task 1: Install Dependencies and Missing shadcn Components

**Files:**
- Modify: `desk/package.json`

- [ ] **Step 1: Install npm packages**

```bash
cd /Users/osamamuhammed/Moca/desk && npm install @dnd-kit/core @dnd-kit/sortable @dnd-kit/utilities zustand
```

- [ ] **Step 2: Add missing shadcn components**

The builder needs `Tabs`, `Switch`, and `ToggleGroup` which aren't installed yet.

Use the shadcn skill to add these components:

```bash
cd /Users/osamamuhammed/Moca/desk && npx shadcn@latest add tabs switch toggle-group
```

- [ ] **Step 3: Verify install**

```bash
cd /Users/osamamuhammed/Moca/desk && ls src/components/ui/tabs.tsx src/components/ui/switch.tsx src/components/ui/toggle-group.tsx
```
Expected: All three files exist

- [ ] **Step 4: Commit**

```bash
git add desk/package.json desk/package-lock.json desk/src/components/ui/tabs.tsx desk/src/components/ui/switch.tsx desk/src/components/ui/toggle-group.tsx
git commit -m "chore(desk): add dnd-kit, zustand, and missing shadcn components"
```

---

### Task 2: BuilderKit Types and useCommandHistory Hook

**Files:**
- Create: `desk/src/components/builder-kit/types.ts`
- Create: `desk/src/components/builder-kit/useCommandHistory.ts`
- Create: `desk/src/components/builder-kit/useCommandHistory.test.ts`

- [ ] **Step 1: Create shared types**

Create `desk/src/components/builder-kit/types.ts`:

```typescript
/** A reversible mutation. Every state change in a builder goes through a Command. */
export interface Command {
  id: string;
  label: string;
  execute(): void;
  undo(): void;
}

/** Schema for the PropertyPanel — each builder supplies its own per node type. */
export interface PropertySchema {
  sections: PropertySection[];
}

export interface PropertySection {
  label: string;
  collapsed?: boolean;
  properties: PropertyDef[];
}

export interface PropertyDef {
  key: string;
  label: string;
  type: "text" | "number" | "boolean" | "select" | "code" | "link" | "textarea";
  options?: string[];
  dependsOn?: (values: Record<string, unknown>) => boolean;
  description?: string;
  placeholder?: string;
}

/** Drawer identifiers — each builder defines its own set. */
export type DrawerId = string;

/** Selection state for the canvas. */
export interface SelectedNode {
  type: string;
  id: string;
}
```

- [ ] **Step 2: Write failing test for useCommandHistory**

Create `desk/src/components/builder-kit/useCommandHistory.test.ts`:

```typescript
import { describe, it, expect, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useCommandHistory } from "./useCommandHistory";

describe("useCommandHistory", () => {
  it("executes a command and adds to undo stack", () => {
    const { result } = renderHook(() => useCommandHistory());
    const exec = vi.fn();
    const undo = vi.fn();

    act(() => {
      result.current.execute({ id: "1", label: "test", execute: exec, undo });
    });

    expect(exec).toHaveBeenCalledOnce();
    expect(result.current.canUndo).toBe(true);
    expect(result.current.canRedo).toBe(false);
  });

  it("undoes the last command", () => {
    const { result } = renderHook(() => useCommandHistory());
    const undo = vi.fn();

    act(() => {
      result.current.execute({ id: "1", label: "test", execute: vi.fn(), undo });
    });
    act(() => {
      result.current.undo();
    });

    expect(undo).toHaveBeenCalledOnce();
    expect(result.current.canUndo).toBe(false);
    expect(result.current.canRedo).toBe(true);
  });

  it("redoes after undo", () => {
    const { result } = renderHook(() => useCommandHistory());
    const exec = vi.fn();

    act(() => {
      result.current.execute({ id: "1", label: "test", execute: exec, undo: vi.fn() });
    });
    act(() => {
      result.current.undo();
    });
    act(() => {
      result.current.redo();
    });

    expect(exec).toHaveBeenCalledTimes(2); // initial + redo
    expect(result.current.canRedo).toBe(false);
  });

  it("clears redo stack on new execute", () => {
    const { result } = renderHook(() => useCommandHistory());

    act(() => {
      result.current.execute({ id: "1", label: "a", execute: vi.fn(), undo: vi.fn() });
    });
    act(() => {
      result.current.undo();
    });
    expect(result.current.canRedo).toBe(true);

    act(() => {
      result.current.execute({ id: "2", label: "b", execute: vi.fn(), undo: vi.fn() });
    });
    expect(result.current.canRedo).toBe(false);
  });
});
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd /Users/osamamuhammed/Moca/desk && npx vitest run --reporter=verbose src/components/builder-kit/useCommandHistory.test.ts`
Expected: FAIL — module not found

- [ ] **Step 4: Implement useCommandHistory**

Create `desk/src/components/builder-kit/useCommandHistory.ts`:

```typescript
import { useCallback, useRef, useState } from "react";
import type { Command } from "./types";

export interface CommandHistoryAPI {
  execute(cmd: Command): void;
  undo(): void;
  redo(): void;
  canUndo: boolean;
  canRedo: boolean;
  history: Command[];
}

export function useCommandHistory(): CommandHistoryAPI {
  const undoStack = useRef<Command[]>([]);
  const redoStack = useRef<Command[]>([]);
  const [, forceUpdate] = useState(0);

  const rerender = () => forceUpdate((n) => n + 1);

  const execute = useCallback((cmd: Command) => {
    cmd.execute();
    undoStack.current.push(cmd);
    redoStack.current = [];
    rerender();
  }, []);

  const undo = useCallback(() => {
    const cmd = undoStack.current.pop();
    if (cmd) {
      cmd.undo();
      redoStack.current.push(cmd);
      rerender();
    }
  }, []);

  const redo = useCallback(() => {
    const cmd = redoStack.current.pop();
    if (cmd) {
      cmd.execute();
      undoStack.current.push(cmd);
      rerender();
    }
  }, []);

  return {
    execute,
    undo,
    redo,
    canUndo: undoStack.current.length > 0,
    canRedo: redoStack.current.length > 0,
    history: undoStack.current,
  };
}
```

- [ ] **Step 5: Run test**

Run: `cd /Users/osamamuhammed/Moca/desk && npx vitest run --reporter=verbose src/components/builder-kit/useCommandHistory.test.ts`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add desk/src/components/builder-kit/
git commit -m "feat(desk): add BuilderKit types and useCommandHistory hook"
```

---

### Task 3: BuilderKit Shell Components

**Files:**
- Create: `desk/src/components/builder-kit/BuilderShell.tsx`
- Create: `desk/src/components/builder-kit/BuilderToolbar.tsx`
- Create: `desk/src/components/builder-kit/DrawerPanel.tsx`
- Create: `desk/src/components/builder-kit/PropertyPanel.tsx`

- [ ] **Step 1: Create BuilderToolbar**

Create `desk/src/components/builder-kit/BuilderToolbar.tsx`:

```typescript
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";

interface BuilderToolbarProps {
  name: string;
  onNameChange: (name: string) => void;
  mode: "schematic" | "preview";
  onModeChange: (mode: "schematic" | "preview") => void;
  onSave: () => void;
  isSaving?: boolean;
  isDirty?: boolean;
  children?: React.ReactNode; // extra toolbar items (app selector, etc.)
}

export function BuilderToolbar({
  name,
  onNameChange,
  mode,
  onModeChange,
  onSave,
  isSaving,
  isDirty,
  children,
}: BuilderToolbarProps) {
  return (
    <div className="flex h-12 items-center justify-between border-b px-3 bg-background">
      <div className="flex items-center gap-3">
        <Input
          value={name}
          onChange={(e) => onNameChange(e.target.value)}
          className="h-8 w-48 font-semibold"
          placeholder="DocType Name"
        />
        {children}
      </div>
      <div className="flex items-center gap-2">
        <ToggleGroup type="single" value={mode} onValueChange={(v) => v && onModeChange(v as "schematic" | "preview")}>
          <ToggleGroupItem value="schematic" className="h-7 text-xs px-3">Schematic</ToggleGroupItem>
          <ToggleGroupItem value="preview" className="h-7 text-xs px-3">Preview</ToggleGroupItem>
        </ToggleGroup>
        <Button size="sm" onClick={onSave} disabled={isSaving || !isDirty}>
          {isSaving ? "Saving..." : "Save"}
        </Button>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Create DrawerPanel**

Create `desk/src/components/builder-kit/DrawerPanel.tsx`:

```typescript
import { Sheet, SheetContent } from "@/components/ui/sheet";

interface DrawerPanelProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  side?: "left" | "right";
  width?: string;
  title?: string;
  children: React.ReactNode;
}

export function DrawerPanel({
  open,
  onOpenChange,
  side = "left",
  width = "280px",
  title,
  children,
}: DrawerPanelProps) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side={side}
        className="p-0 overflow-y-auto"
        style={{ width, maxWidth: width }}
      >
        {title && (
          <div className="sticky top-0 z-10 bg-background border-b px-3 py-2">
            <h3 className="text-sm font-medium">{title}</h3>
          </div>
        )}
        <div className="p-3">{children}</div>
      </SheetContent>
    </Sheet>
  );
}
```

- [ ] **Step 3: Create PropertyPanel**

Create `desk/src/components/builder-kit/PropertyPanel.tsx`:

```typescript
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { ChevronDown } from "lucide-react";
import type { PropertySchema, PropertyDef } from "./types";

interface PropertyPanelProps {
  schema: PropertySchema;
  values: Record<string, unknown>;
  onChange: (key: string, value: unknown) => void;
  title?: string;
}

export function PropertyPanel({ schema, values, onChange, title }: PropertyPanelProps) {
  return (
    <div className="space-y-4">
      {title && <h3 className="text-sm font-medium px-1">{title}</h3>}
      {schema.sections.map((section) => (
        <Collapsible key={section.label} defaultOpen={!section.collapsed}>
          <CollapsibleTrigger className="flex w-full items-center justify-between text-xs font-medium text-muted-foreground uppercase tracking-wide px-1 py-1 hover:text-foreground">
            {section.label}
            <ChevronDown className="h-3 w-3" />
          </CollapsibleTrigger>
          <CollapsibleContent className="space-y-3 mt-2">
            {section.properties.map((prop) => {
              if (prop.dependsOn && !prop.dependsOn(values)) return null;
              return (
                <PropertyField
                  key={prop.key}
                  def={prop}
                  value={values[prop.key]}
                  onChange={(v) => onChange(prop.key, v)}
                />
              );
            })}
          </CollapsibleContent>
        </Collapsible>
      ))}
    </div>
  );
}

function PropertyField({
  def,
  value,
  onChange,
}: {
  def: PropertyDef;
  value: unknown;
  onChange: (value: unknown) => void;
}) {
  return (
    <div className="space-y-1 px-1">
      <Label className="text-xs">{def.label}</Label>
      {def.type === "text" && (
        <Input
          value={(value as string) ?? ""}
          onChange={(e) => onChange(e.target.value)}
          placeholder={def.placeholder}
          className="h-7 text-xs"
        />
      )}
      {def.type === "textarea" && (
        <Textarea
          value={(value as string) ?? ""}
          onChange={(e) => onChange(e.target.value)}
          placeholder={def.placeholder}
          className="text-xs min-h-[60px]"
        />
      )}
      {def.type === "number" && (
        <Input
          type="number"
          value={(value as number) ?? ""}
          onChange={(e) => onChange(e.target.value ? Number(e.target.value) : undefined)}
          className="h-7 text-xs"
        />
      )}
      {def.type === "boolean" && (
        <Switch
          checked={!!value}
          onCheckedChange={onChange}
        />
      )}
      {def.type === "select" && (
        <Select value={(value as string) ?? ""} onValueChange={onChange}>
          <SelectTrigger className="h-7 text-xs">
            <SelectValue placeholder="Select..." />
          </SelectTrigger>
          <SelectContent>
            {def.options?.map((opt) => (
              <SelectItem key={opt} value={opt} className="text-xs">{opt}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      {def.description && (
        <p className="text-xs text-muted-foreground">{def.description}</p>
      )}
    </div>
  );
}
```

- [ ] **Step 4: Create BuilderShell**

Create `desk/src/components/builder-kit/BuilderShell.tsx`:

```typescript
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

export interface IconRailItem {
  id: string;
  icon: React.ReactNode;
  label: string;
}

interface BuilderShellProps {
  toolbar: React.ReactNode;
  iconRailItems: IconRailItem[];
  activeDrawer: string | null;
  onDrawerToggle: (id: string) => void;
  leftDrawer?: React.ReactNode;
  rightPanel?: React.ReactNode;
  statusBar?: React.ReactNode;
  children: React.ReactNode; // canvas
}

export function BuilderShell({
  toolbar,
  iconRailItems,
  activeDrawer,
  onDrawerToggle,
  leftDrawer,
  rightPanel,
  statusBar,
  children,
}: BuilderShellProps) {
  return (
    <div className="flex h-screen flex-col">
      {toolbar}
      <div className="flex flex-1 overflow-hidden">
        {/* Icon rail */}
        <div className="flex w-10 flex-col items-center gap-1 border-r bg-muted/30 py-2">
          {iconRailItems.map((item) => (
            <Tooltip key={item.id}>
              <TooltipTrigger asChild>
                <button
                  onClick={() => onDrawerToggle(item.id)}
                  className={cn(
                    "flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors",
                    activeDrawer === item.id && "bg-accent text-accent-foreground"
                  )}
                >
                  {item.icon}
                </button>
              </TooltipTrigger>
              <TooltipContent side="right">{item.label}</TooltipContent>
            </Tooltip>
          ))}
        </div>

        {/* Left drawer (slides out from icon rail) */}
        {leftDrawer && activeDrawer && (
          <div className="w-64 border-r overflow-y-auto bg-background animate-in slide-in-from-left-2">
            {leftDrawer}
          </div>
        )}

        {/* Canvas */}
        <div className="flex-1 overflow-y-auto bg-muted/10 p-4">
          {children}
        </div>

        {/* Right property panel */}
        {rightPanel && (
          <div className="w-64 border-l overflow-y-auto bg-background">
            {rightPanel}
          </div>
        )}
      </div>

      {/* Status bar */}
      {statusBar && (
        <div className="flex h-6 items-center border-t px-3 text-xs text-muted-foreground">
          {statusBar}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 5: Create barrel export**

Create `desk/src/components/builder-kit/index.ts`:

```typescript
export { BuilderShell, type IconRailItem } from "./BuilderShell";
export { BuilderToolbar } from "./BuilderToolbar";
export { DrawerPanel } from "./DrawerPanel";
export { PropertyPanel } from "./PropertyPanel";
export { useCommandHistory } from "./useCommandHistory";
export type { Command, PropertySchema, PropertySection, PropertyDef, SelectedNode, DrawerId } from "./types";
```

- [ ] **Step 6: Verify build**

Run: `cd /Users/osamamuhammed/Moca/desk && npx tsc --noEmit`
Expected: No type errors

- [ ] **Step 7: Commit**

```bash
git add desk/src/components/builder-kit/
git commit -m "feat(desk): add BuilderKit shell components (toolbar, drawers, property panel)"
```

---

### Task 4: Builder Types and Zustand Store

**Files:**
- Create: `desk/src/components/doctype-builder/types.ts`
- Create: `desk/src/stores/doctype-builder-store.ts`

- [ ] **Step 1: Create builder-specific types**

Create `desk/src/components/doctype-builder/types.ts`:

```typescript
import type { FieldDef, FieldType, LayoutTree, TabDef, SectionDef, ColumnDef } from "@/api/types";

export type { LayoutTree, TabDef, SectionDef, ColumnDef };

export interface DocTypeSettings {
  naming_rule: { rule: string; pattern?: string; field_name?: string };
  title_field: string;
  sort_field: string;
  sort_order: string;
  search_fields: string[];
  image_field: string;
  is_submittable: boolean;
  is_single: boolean;
  is_child_table: boolean;
  is_virtual: boolean;
  track_changes: boolean;
}

export interface BuilderSelection {
  type: "field" | "section" | "column" | "tab";
  /** For field: field name. For section: "tabIdx-sectionIdx". For column: "tabIdx-sectionIdx-colIdx". For tab: "tabIdx" */
  id: string;
}

export type ActiveDrawer = "fields" | "settings" | "permissions" | null;

export interface DocTypeBuilderState {
  // Identity
  name: string;
  app: string | null;
  module: string;
  isNew: boolean;

  // Data
  layout: LayoutTree;
  fields: Record<string, FieldDef>;
  settings: DocTypeSettings;
  permissions: Array<{
    role: string;
    read: boolean;
    write: boolean;
    create: boolean;
    delete: boolean;
    submit: boolean;
    cancel: boolean;
  }>;

  // UI
  selection: BuilderSelection | null;
  activeDrawer: ActiveDrawer;
  mode: "schematic" | "preview";
  isDirty: boolean;

  // Actions
  setName: (name: string) => void;
  setApp: (app: string | null) => void;
  setModule: (module: string) => void;
  setMode: (mode: "schematic" | "preview") => void;
  setSelection: (sel: BuilderSelection | null) => void;
  setActiveDrawer: (drawer: ActiveDrawer) => void;
  toggleDrawer: (drawer: Exclude<ActiveDrawer, null>) => void;
  
  // Field operations
  addField: (fieldName: string, fieldType: FieldType, tabIdx: number, sectionIdx: number, colIdx: number, insertIdx?: number) => void;
  removeField: (fieldName: string) => void;
  updateField: (fieldName: string, updates: Partial<FieldDef>) => void;
  moveField: (fieldName: string, toTabIdx: number, toSectionIdx: number, toColIdx: number, toInsertIdx: number) => void;

  // Layout operations
  addTab: (label: string) => void;
  removeTab: (tabIdx: number) => void;
  updateTab: (tabIdx: number, updates: Partial<TabDef>) => void;
  addSection: (tabIdx: number, label?: string) => void;
  removeSection: (tabIdx: number, sectionIdx: number) => void;
  updateSection: (tabIdx: number, sectionIdx: number, updates: Partial<SectionDef>) => void;
  addColumn: (tabIdx: number, sectionIdx: number) => void;
  removeColumn: (tabIdx: number, sectionIdx: number, colIdx: number) => void;
  updateColumnWidth: (tabIdx: number, sectionIdx: number, colIdx: number, width: number) => void;

  // Settings & permissions
  updateSettings: (updates: Partial<DocTypeSettings>) => void;
  setPermissions: (perms: DocTypeBuilderState["permissions"]) => void;

  // Persistence
  markClean: () => void;
  hydrate: (data: {
    name: string;
    app?: string;
    module: string;
    layout: LayoutTree;
    fields: Record<string, FieldDef>;
    settings: DocTypeSettings;
    permissions: DocTypeBuilderState["permissions"];
  }) => void;
  reset: () => void;
}

/** Generates a unique field name for a given type. */
export function generateFieldName(fieldType: FieldType, existingNames: Set<string>): string {
  const base = fieldType.toLowerCase().replace(/([A-Z])/g, "_$1").replace(/^_/, "");
  let name = base;
  let i = 1;
  while (existingNames.has(name)) {
    name = `${base}_${i}`;
    i++;
  }
  return name;
}
```

- [ ] **Step 2: Create Zustand store**

Create `desk/src/stores/doctype-builder-store.ts`:

```typescript
import { create } from "zustand";
import type { DocTypeBuilderState, DocTypeSettings } from "@/components/doctype-builder/types";
import { generateFieldName } from "@/components/doctype-builder/types";
import type { FieldDef, FieldType } from "@/api/types";

const DEFAULT_SETTINGS: DocTypeSettings = {
  naming_rule: { rule: "uuid" },
  title_field: "",
  sort_field: "",
  sort_order: "desc",
  search_fields: [],
  image_field: "",
  is_submittable: false,
  is_single: false,
  is_child_table: false,
  is_virtual: false,
  track_changes: true,
};

export const useDocTypeBuilderStore = create<DocTypeBuilderState>((set, get) => ({
  // Identity
  name: "",
  app: null,
  module: "",
  isNew: true,

  // Data
  layout: { tabs: [{ label: "Details", sections: [{ columns: [{ width: 1, fields: [] }] }] }] },
  fields: {},
  settings: { ...DEFAULT_SETTINGS },
  permissions: [{ role: "System Manager", read: true, write: true, create: true, delete: true, submit: false, cancel: false }],

  // UI
  selection: null,
  activeDrawer: null,
  mode: "schematic",
  isDirty: false,

  // Simple setters
  setName: (name) => set({ name, isDirty: true }),
  setApp: (app) => set({ app }),
  setModule: (module) => set({ module, isDirty: true }),
  setMode: (mode) => set({ mode }),
  setSelection: (selection) => set({ selection }),
  setActiveDrawer: (activeDrawer) => set({ activeDrawer }),
  toggleDrawer: (drawer) => set((s) => ({ activeDrawer: s.activeDrawer === drawer ? null : drawer })),

  // Field operations
  addField: (fieldName, fieldType, tabIdx, sectionIdx, colIdx, insertIdx) => set((s) => {
    const layout = structuredClone(s.layout);
    const col = layout.tabs[tabIdx]?.sections[sectionIdx]?.columns[colIdx];
    if (!col) return s;
    const idx = insertIdx ?? col.fields.length;
    col.fields.splice(idx, 0, fieldName);
    const fields = { ...s.fields, [fieldName]: { name: fieldName, field_type: fieldType, label: fieldName.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase()), required: false, read_only: false, in_api: true } as FieldDef };
    return { layout, fields, isDirty: true, selection: { type: "field", id: fieldName } };
  }),

  removeField: (fieldName) => set((s) => {
    const layout = structuredClone(s.layout);
    for (const tab of layout.tabs) {
      for (const sec of tab.sections) {
        for (const col of sec.columns) {
          col.fields = col.fields.filter((f) => f !== fieldName);
        }
      }
    }
    const fields = { ...s.fields };
    delete fields[fieldName];
    return { layout, fields, isDirty: true, selection: null };
  }),

  updateField: (fieldName, updates) => set((s) => ({
    fields: { ...s.fields, [fieldName]: { ...s.fields[fieldName], ...updates } },
    isDirty: true,
  })),

  moveField: (fieldName, toTabIdx, toSectionIdx, toColIdx, toInsertIdx) => set((s) => {
    const layout = structuredClone(s.layout);
    // Remove from current position
    for (const tab of layout.tabs) {
      for (const sec of tab.sections) {
        for (const col of sec.columns) {
          col.fields = col.fields.filter((f) => f !== fieldName);
        }
      }
    }
    // Insert at new position
    const col = layout.tabs[toTabIdx]?.sections[toSectionIdx]?.columns[toColIdx];
    if (col) col.fields.splice(toInsertIdx, 0, fieldName);
    return { layout, isDirty: true };
  }),

  // Layout operations
  addTab: (label) => set((s) => {
    const layout = structuredClone(s.layout);
    layout.tabs.push({ label, sections: [{ columns: [{ width: 1, fields: [] }] }] });
    return { layout, isDirty: true };
  }),
  removeTab: (tabIdx) => set((s) => {
    const layout = structuredClone(s.layout);
    if (layout.tabs.length <= 1) return s; // keep at least one
    layout.tabs.splice(tabIdx, 1);
    return { layout, isDirty: true, selection: null };
  }),
  updateTab: (tabIdx, updates) => set((s) => {
    const layout = structuredClone(s.layout);
    layout.tabs[tabIdx] = { ...layout.tabs[tabIdx], ...updates };
    return { layout, isDirty: true };
  }),
  addSection: (tabIdx, label) => set((s) => {
    const layout = structuredClone(s.layout);
    layout.tabs[tabIdx]?.sections.push({ label, columns: [{ width: 1, fields: [] }] });
    return { layout, isDirty: true };
  }),
  removeSection: (tabIdx, sectionIdx) => set((s) => {
    const layout = structuredClone(s.layout);
    const tab = layout.tabs[tabIdx];
    if (!tab || tab.sections.length <= 1) return s;
    tab.sections.splice(sectionIdx, 1);
    return { layout, isDirty: true, selection: null };
  }),
  updateSection: (tabIdx, sectionIdx, updates) => set((s) => {
    const layout = structuredClone(s.layout);
    const sec = layout.tabs[tabIdx]?.sections[sectionIdx];
    if (sec) Object.assign(sec, updates);
    return { layout, isDirty: true };
  }),
  addColumn: (tabIdx, sectionIdx) => set((s) => {
    const layout = structuredClone(s.layout);
    layout.tabs[tabIdx]?.sections[sectionIdx]?.columns.push({ width: 1, fields: [] });
    return { layout, isDirty: true };
  }),
  removeColumn: (tabIdx, sectionIdx, colIdx) => set((s) => {
    const layout = structuredClone(s.layout);
    const sec = layout.tabs[tabIdx]?.sections[sectionIdx];
    if (!sec || sec.columns.length <= 1) return s;
    sec.columns.splice(colIdx, 1);
    return { layout, isDirty: true, selection: null };
  }),
  updateColumnWidth: (tabIdx, sectionIdx, colIdx, width) => set((s) => {
    const layout = structuredClone(s.layout);
    const col = layout.tabs[tabIdx]?.sections[sectionIdx]?.columns[colIdx];
    if (col) col.width = Math.max(1, width);
    return { layout, isDirty: true };
  }),

  // Settings & permissions
  updateSettings: (updates) => set((s) => ({ settings: { ...s.settings, ...updates }, isDirty: true })),
  setPermissions: (permissions) => set({ permissions, isDirty: true }),

  // Persistence
  markClean: () => set({ isDirty: false }),
  hydrate: (data) => set({
    name: data.name,
    app: data.app ?? null,
    module: data.module,
    layout: data.layout,
    fields: data.fields,
    settings: data.settings,
    permissions: data.permissions,
    isNew: false,
    isDirty: false,
    selection: null,
    activeDrawer: null,
    mode: "schematic",
  }),
  reset: () => set({
    name: "",
    app: null,
    module: "",
    isNew: true,
    layout: { tabs: [{ label: "Details", sections: [{ columns: [{ width: 1, fields: [] }] }] }] },
    fields: {},
    settings: { ...DEFAULT_SETTINGS },
    permissions: [{ role: "System Manager", read: true, write: true, create: true, delete: true, submit: false, cancel: false }],
    selection: null,
    activeDrawer: null,
    mode: "schematic",
    isDirty: false,
  }),
}));
```

- [ ] **Step 3: Verify types compile**

Run: `cd /Users/osamamuhammed/Moca/desk && npx tsc --noEmit`
Expected: No type errors

- [ ] **Step 4: Commit**

```bash
git add desk/src/components/doctype-builder/types.ts desk/src/stores/doctype-builder-store.ts
git commit -m "feat(desk): add DocType Builder types and Zustand store"
```

---

### Task 5: Field Palette and Field Card Components

**Files:**
- Create: `desk/src/components/doctype-builder/field-type-categories.ts`
- Create: `desk/src/components/doctype-builder/FieldPalette.tsx`
- Create: `desk/src/components/doctype-builder/FieldCard.tsx`

- [ ] **Step 1: Create field type categories**

Create `desk/src/components/doctype-builder/field-type-categories.ts`:

```typescript
import type { FieldType } from "@/api/types";

export interface FieldTypeCategory {
  label: string;
  types: { type: FieldType; label: string }[];
}

export const FIELD_TYPE_CATEGORIES: FieldTypeCategory[] = [
  {
    label: "Text",
    types: [
      { type: "Data", label: "Data" },
      { type: "Text", label: "Text" },
      { type: "LongText", label: "Long Text" },
      { type: "Markdown", label: "Markdown" },
      { type: "Code", label: "Code" },
      { type: "HTMLEditor", label: "HTML Editor" },
    ],
  },
  {
    label: "Number",
    types: [
      { type: "Int", label: "Integer" },
      { type: "Float", label: "Float" },
      { type: "Currency", label: "Currency" },
      { type: "Percent", label: "Percent" },
      { type: "Rating", label: "Rating" },
    ],
  },
  {
    label: "Date & Time",
    types: [
      { type: "Date", label: "Date" },
      { type: "Datetime", label: "Date Time" },
      { type: "Time", label: "Time" },
      { type: "Duration", label: "Duration" },
    ],
  },
  {
    label: "Selection",
    types: [
      { type: "Select", label: "Select" },
      { type: "Link", label: "Link" },
      { type: "DynamicLink", label: "Dynamic Link" },
    ],
  },
  {
    label: "Relations",
    types: [
      { type: "Table", label: "Table" },
      { type: "TableMultiSelect", label: "Table MultiSelect" },
    ],
  },
  {
    label: "Media",
    types: [
      { type: "Attach", label: "Attach" },
      { type: "AttachImage", label: "Attach Image" },
      { type: "Color", label: "Color" },
      { type: "Signature", label: "Signature" },
      { type: "Barcode", label: "Barcode" },
    ],
  },
  {
    label: "Interactive",
    types: [
      { type: "Check", label: "Checkbox" },
      { type: "Password", label: "Password" },
    ],
  },
  {
    label: "Display",
    types: [
      { type: "HTML", label: "HTML" },
      { type: "Heading", label: "Heading" },
      { type: "Button", label: "Button" },
    ],
  },
];
```

- [ ] **Step 2: Create FieldPalette**

Create `desk/src/components/doctype-builder/FieldPalette.tsx`:

```typescript
import { useDraggable } from "@dnd-kit/core";
import { Badge } from "@/components/ui/badge";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { ChevronDown, GripVertical } from "lucide-react";
import { FIELD_TYPE_CATEGORIES } from "./field-type-categories";
import type { FieldType } from "@/api/types";

export function FieldPalette() {
  return (
    <div className="space-y-2">
      <h3 className="text-sm font-medium px-1">Field Types</h3>
      {FIELD_TYPE_CATEGORIES.map((cat) => (
        <Collapsible key={cat.label} defaultOpen>
          <CollapsibleTrigger className="flex w-full items-center justify-between text-xs font-medium text-muted-foreground px-1 py-1 hover:text-foreground">
            {cat.label}
            <ChevronDown className="h-3 w-3" />
          </CollapsibleTrigger>
          <CollapsibleContent className="grid grid-cols-2 gap-1 mt-1">
            {cat.types.map((ft) => (
              <DraggableFieldType key={ft.type} type={ft.type} label={ft.label} />
            ))}
          </CollapsibleContent>
        </Collapsible>
      ))}
    </div>
  );
}

function DraggableFieldType({ type, label }: { type: FieldType; label: string }) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id: `palette-${type}`,
    data: { type: "palette-field", fieldType: type },
  });

  return (
    <div
      ref={setNodeRef}
      {...listeners}
      {...attributes}
      className={`flex items-center gap-1 rounded border px-2 py-1.5 text-xs cursor-grab hover:bg-accent transition-colors ${isDragging ? "opacity-50" : ""}`}
    >
      <GripVertical className="h-3 w-3 text-muted-foreground shrink-0" />
      <span className="truncate">{label}</span>
    </div>
  );
}
```

- [ ] **Step 3: Create FieldCard**

Create `desk/src/components/doctype-builder/FieldCard.tsx`:

```typescript
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Badge } from "@/components/ui/badge";
import { GripVertical } from "lucide-react";
import { cn } from "@/lib/utils";
import type { FieldDef } from "@/api/types";

interface FieldCardProps {
  fieldDef: FieldDef;
  isSelected: boolean;
  onClick: () => void;
}

export function FieldCard({ fieldDef, isSelected, onClick }: FieldCardProps) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: fieldDef.name,
    data: { type: "field", fieldName: fieldDef.name },
  });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  };

  return (
    <div
      ref={setNodeRef}
      style={style}
      onClick={onClick}
      className={cn(
        "flex items-center gap-2 rounded border px-2 py-1.5 text-xs cursor-pointer transition-colors",
        isSelected ? "border-primary bg-primary/5 ring-1 ring-primary" : "hover:bg-accent",
        isDragging && "opacity-50 shadow-lg"
      )}
    >
      <span {...attributes} {...listeners} className="cursor-grab">
        <GripVertical className="h-3 w-3 text-muted-foreground" />
      </span>
      <span className="font-medium truncate flex-1">
        {fieldDef.name}
        {fieldDef.required && <span className="text-destructive ml-0.5">*</span>}
      </span>
      <Badge variant="outline" className="text-[10px] px-1 py-0 h-4 shrink-0">
        {fieldDef.field_type}
      </Badge>
      {fieldDef.options && (fieldDef.field_type === "Link" || fieldDef.field_type === "Table") && (
        <span className="text-muted-foreground truncate text-[10px]">
          &rarr; {fieldDef.options}
        </span>
      )}
    </div>
  );
}
```

- [ ] **Step 4: Verify build**

Run: `cd /Users/osamamuhammed/Moca/desk && npx tsc --noEmit`
Expected: No type errors

- [ ] **Step 5: Commit**

```bash
git add desk/src/components/doctype-builder/field-type-categories.ts desk/src/components/doctype-builder/FieldPalette.tsx desk/src/components/doctype-builder/FieldCard.tsx
git commit -m "feat(desk): add FieldPalette and FieldCard components"
```

---

### Task 6: Schematic Canvas (TabBar, SectionNode, ColumnNode)

**Files:**
- Create: `desk/src/components/doctype-builder/TabBar.tsx`
- Create: `desk/src/components/doctype-builder/SectionNode.tsx`
- Create: `desk/src/components/doctype-builder/ColumnNode.tsx`
- Create: `desk/src/components/doctype-builder/SchematicCanvas.tsx`

- [ ] **Step 1: Create TabBar**

Create `desk/src/components/doctype-builder/TabBar.tsx`:

```typescript
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import { Plus } from "lucide-react";
import { useDocTypeBuilderStore } from "@/stores/doctype-builder-store";

export function TabBar() {
  const layout = useDocTypeBuilderStore((s) => s.layout);
  const addTab = useDocTypeBuilderStore((s) => s.addTab);
  const [activeTab, setActiveTab] = React.useState("0");

  return (
    <div className="flex items-center gap-1 mb-4">
      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          {layout.tabs.map((tab, i) => (
            <TabsTrigger key={i} value={String(i)} className="text-xs">
              {tab.label || `Tab ${i + 1}`}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <Button
        variant="ghost"
        size="sm"
        className="h-7 w-7 p-0"
        onClick={() => addTab(`Tab ${layout.tabs.length + 1}`)}
      >
        <Plus className="h-3 w-3" />
      </Button>
    </div>
  );
}

import React from "react";
```

**Note:** The TabBar will be refactored to pass `activeTab` via props from the canvas. This is the initial scaffold.

- [ ] **Step 2: Create SectionNode**

Create `desk/src/components/doctype-builder/SectionNode.tsx`:

```typescript
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { MoreHorizontal, Plus, Trash2 } from "lucide-react";
import { cn } from "@/lib/utils";
import { ColumnNode } from "./ColumnNode";
import type { SectionDef } from "./types";

interface SectionNodeProps {
  section: SectionDef;
  tabIdx: number;
  sectionIdx: number;
  onAddColumn: () => void;
  onRemoveSection: () => void;
  onUpdateLabel: (label: string) => void;
}

export function SectionNode({
  section,
  tabIdx,
  sectionIdx,
  onAddColumn,
  onRemoveSection,
  onUpdateLabel,
}: SectionNodeProps) {
  return (
    <div className="rounded-lg border bg-background p-3 mb-3">
      {/* Section header */}
      <div className="flex items-center justify-between mb-2">
        <input
          value={section.label ?? ""}
          onChange={(e) => onUpdateLabel(e.target.value)}
          placeholder="Section label..."
          className="text-xs font-medium bg-transparent border-none outline-none text-muted-foreground hover:text-foreground focus:text-foreground placeholder:text-muted-foreground/50 w-full"
        />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="sm" className="h-6 w-6 p-0">
              <MoreHorizontal className="h-3 w-3" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={onRemoveSection} className="text-destructive">
              <Trash2 className="h-3 w-3 mr-2" />Delete Section
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {/* Columns */}
      <div className="flex gap-2">
        {section.columns.map((col, colIdx) => (
          <div key={colIdx} style={{ flex: col.width }} className="min-w-0">
            <ColumnNode
              column={col}
              tabIdx={tabIdx}
              sectionIdx={sectionIdx}
              colIdx={colIdx}
            />
          </div>
        ))}
      </div>

      {/* Add column */}
      <div className="flex justify-center mt-2">
        <Button variant="ghost" size="sm" onClick={onAddColumn} className="h-6 text-xs text-muted-foreground">
          <Plus className="h-3 w-3 mr-1" /> Add Column
        </Button>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Create ColumnNode**

Create `desk/src/components/doctype-builder/ColumnNode.tsx`:

```typescript
import { useDroppable } from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { Button } from "@/components/ui/button";
import { Plus } from "lucide-react";
import { FieldCard } from "./FieldCard";
import { useDocTypeBuilderStore } from "@/stores/doctype-builder-store";
import type { ColumnDef } from "./types";

interface ColumnNodeProps {
  column: ColumnDef;
  tabIdx: number;
  sectionIdx: number;
  colIdx: number;
}

export function ColumnNode({ column, tabIdx, sectionIdx, colIdx }: ColumnNodeProps) {
  const fields = useDocTypeBuilderStore((s) => s.fields);
  const selection = useDocTypeBuilderStore((s) => s.selection);
  const setSelection = useDocTypeBuilderStore((s) => s.setSelection);
  const toggleDrawer = useDocTypeBuilderStore((s) => s.toggleDrawer);

  const droppableId = `col-${tabIdx}-${sectionIdx}-${colIdx}`;
  const { setNodeRef, isOver } = useDroppable({
    id: droppableId,
    data: { type: "column", tabIdx, sectionIdx, colIdx },
  });

  return (
    <div
      ref={setNodeRef}
      className={`min-h-[60px] rounded border border-dashed p-1.5 space-y-1 transition-colors ${isOver ? "border-primary bg-primary/5" : "border-transparent"}`}
    >
      <SortableContext items={column.fields} strategy={verticalListSortingStrategy}>
        {column.fields.map((fieldName) => {
          const fd = fields[fieldName];
          if (!fd) return null;
          return (
            <FieldCard
              key={fieldName}
              fieldDef={fd}
              isSelected={selection?.type === "field" && selection.id === fieldName}
              onClick={() => setSelection({ type: "field", id: fieldName })}
            />
          );
        })}
      </SortableContext>

      <Button
        variant="ghost"
        size="sm"
        className="w-full h-6 text-xs text-muted-foreground border border-dashed"
        onClick={() => toggleDrawer("fields")}
      >
        <Plus className="h-3 w-3 mr-1" /> Add Field
      </Button>
    </div>
  );
}
```

- [ ] **Step 4: Create SchematicCanvas**

Create `desk/src/components/doctype-builder/SchematicCanvas.tsx`:

```typescript
import { useState } from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import { Plus } from "lucide-react";
import { useDocTypeBuilderStore } from "@/stores/doctype-builder-store";
import { SectionNode } from "./SectionNode";

export function SchematicCanvas() {
  const layout = useDocTypeBuilderStore((s) => s.layout);
  const addTab = useDocTypeBuilderStore((s) => s.addTab);
  const addSection = useDocTypeBuilderStore((s) => s.addSection);
  const addColumn = useDocTypeBuilderStore((s) => s.addColumn);
  const removeSection = useDocTypeBuilderStore((s) => s.removeSection);
  const updateSection = useDocTypeBuilderStore((s) => s.updateSection);
  const [activeTab, setActiveTab] = useState("0");

  const tabIdx = parseInt(activeTab, 10);

  return (
    <div>
      {/* Tab bar */}
      <div className="flex items-center gap-1 mb-4">
        <Tabs value={activeTab} onValueChange={setActiveTab} className="flex-1">
          <TabsList>
            {layout.tabs.map((tab, i) => (
              <TabsTrigger key={i} value={String(i)} className="text-xs">
                {tab.label || `Tab ${i + 1}`}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        <Button
          variant="ghost"
          size="sm"
          className="h-7 w-7 p-0"
          onClick={() => addTab(`Tab ${layout.tabs.length + 1}`)}
        >
          <Plus className="h-3 w-3" />
        </Button>
      </div>

      {/* Sections for active tab */}
      {layout.tabs[tabIdx]?.sections.map((section, sIdx) => (
        <SectionNode
          key={sIdx}
          section={section}
          tabIdx={tabIdx}
          sectionIdx={sIdx}
          onAddColumn={() => addColumn(tabIdx, sIdx)}
          onRemoveSection={() => removeSection(tabIdx, sIdx)}
          onUpdateLabel={(label) => updateSection(tabIdx, sIdx, { label })}
        />
      ))}

      {/* Add section */}
      <div className="flex justify-center mt-2">
        <Button variant="ghost" size="sm" className="text-xs text-muted-foreground">
          <Plus className="h-3 w-3 mr-1" onClick={() => addSection(tabIdx)} /> Add Section
        </Button>
      </div>
    </div>
  );
}
```

- [ ] **Step 5: Verify build**

Run: `cd /Users/osamamuhammed/Moca/desk && npx tsc --noEmit`
Expected: No type errors

- [ ] **Step 6: Commit**

```bash
git add desk/src/components/doctype-builder/TabBar.tsx desk/src/components/doctype-builder/SectionNode.tsx desk/src/components/doctype-builder/ColumnNode.tsx desk/src/components/doctype-builder/SchematicCanvas.tsx
git commit -m "feat(desk): add schematic canvas components (TabBar, Section, Column)"
```

---

### Task 7: DocType Builder Page with DnD and Routing

**Files:**
- Create: `desk/src/pages/DocTypeBuilder.tsx`
- Create: `desk/src/components/doctype-builder/SettingsDrawer.tsx`
- Create: `desk/src/components/doctype-builder/PermissionsDrawer.tsx`
- Modify: `desk/src/router.tsx`

- [ ] **Step 1: Create SettingsDrawer**

Create `desk/src/components/doctype-builder/SettingsDrawer.tsx`:

```typescript
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { useDocTypeBuilderStore } from "@/stores/doctype-builder-store";

export function SettingsDrawer() {
  const settings = useDocTypeBuilderStore((s) => s.settings);
  const updateSettings = useDocTypeBuilderStore((s) => s.updateSettings);

  return (
    <div className="space-y-4 p-3">
      <h3 className="text-sm font-medium">DocType Settings</h3>

      <div className="space-y-1">
        <Label className="text-xs">Naming Rule</Label>
        <Select value={settings.naming_rule.rule} onValueChange={(v) => updateSettings({ naming_rule: { ...settings.naming_rule, rule: v } })}>
          <SelectTrigger className="h-7 text-xs"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="uuid">UUID</SelectItem>
            <SelectItem value="autoincrement">Auto Increment</SelectItem>
            <SelectItem value="pattern">Pattern</SelectItem>
            <SelectItem value="field">Field</SelectItem>
            <SelectItem value="hash">Hash</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {settings.naming_rule.rule === "pattern" && (
        <div className="space-y-1">
          <Label className="text-xs">Pattern</Label>
          <Input value={settings.naming_rule.pattern ?? ""} onChange={(e) => updateSettings({ naming_rule: { ...settings.naming_rule, pattern: e.target.value } })} placeholder="e.g. SO-.####" className="h-7 text-xs" />
        </div>
      )}

      <div className="space-y-1">
        <Label className="text-xs">Title Field</Label>
        <Input value={settings.title_field} onChange={(e) => updateSettings({ title_field: e.target.value })} className="h-7 text-xs" />
      </div>

      <div className="space-y-1">
        <Label className="text-xs">Sort Field</Label>
        <Input value={settings.sort_field} onChange={(e) => updateSettings({ sort_field: e.target.value })} className="h-7 text-xs" />
      </div>

      <div className="space-y-1">
        <Label className="text-xs">Sort Order</Label>
        <Select value={settings.sort_order} onValueChange={(v) => updateSettings({ sort_order: v })}>
          <SelectTrigger className="h-7 text-xs"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="asc">Ascending</SelectItem>
            <SelectItem value="desc">Descending</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div className="flex items-center justify-between">
        <Label className="text-xs">Submittable</Label>
        <Switch checked={settings.is_submittable} onCheckedChange={(v) => updateSettings({ is_submittable: v })} />
      </div>
      <div className="flex items-center justify-between">
        <Label className="text-xs">Track Changes</Label>
        <Switch checked={settings.track_changes} onCheckedChange={(v) => updateSettings({ track_changes: v })} />
      </div>
      <div className="flex items-center justify-between">
        <Label className="text-xs">Single</Label>
        <Switch checked={settings.is_single} onCheckedChange={(v) => updateSettings({ is_single: v })} />
      </div>
      <div className="flex items-center justify-between">
        <Label className="text-xs">Child Table</Label>
        <Switch checked={settings.is_child_table} onCheckedChange={(v) => updateSettings({ is_child_table: v })} />
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Create PermissionsDrawer**

Create `desk/src/components/doctype-builder/PermissionsDrawer.tsx`:

```typescript
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Plus, Trash2 } from "lucide-react";
import { useDocTypeBuilderStore } from "@/stores/doctype-builder-store";

export function PermissionsDrawer() {
  const permissions = useDocTypeBuilderStore((s) => s.permissions);
  const setPermissions = useDocTypeBuilderStore((s) => s.setPermissions);

  const addRole = () => {
    setPermissions([...permissions, { role: "", read: true, write: false, create: false, delete: false, submit: false, cancel: false }]);
  };

  const updatePerm = (idx: number, key: string, value: unknown) => {
    const updated = permissions.map((p, i) => i === idx ? { ...p, [key]: value } : p);
    setPermissions(updated);
  };

  const removePerm = (idx: number) => {
    setPermissions(permissions.filter((_, i) => i !== idx));
  };

  return (
    <div className="space-y-3 p-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium">Permissions</h3>
        <Button variant="ghost" size="sm" onClick={addRole} className="h-6 text-xs">
          <Plus className="h-3 w-3 mr-1" /> Add Role
        </Button>
      </div>

      {permissions.map((perm, idx) => (
        <div key={idx} className="rounded border p-2 space-y-2">
          <div className="flex items-center gap-2">
            <Input
              value={perm.role}
              onChange={(e) => updatePerm(idx, "role", e.target.value)}
              placeholder="Role name"
              className="h-7 text-xs flex-1"
            />
            <Button variant="ghost" size="sm" className="h-6 w-6 p-0" onClick={() => removePerm(idx)}>
              <Trash2 className="h-3 w-3 text-destructive" />
            </Button>
          </div>
          <div className="grid grid-cols-3 gap-2">
            {(["read", "write", "create", "delete", "submit", "cancel"] as const).map((key) => (
              <label key={key} className="flex items-center gap-1 text-xs">
                <Checkbox
                  checked={perm[key]}
                  onCheckedChange={(v) => updatePerm(idx, key, !!v)}
                  className="h-3 w-3"
                />
                {key}
              </label>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
```

- [ ] **Step 3: Create DocTypeBuilder page**

Create `desk/src/pages/DocTypeBuilder.tsx`:

```typescript
import { useCallback, useEffect } from "react";
import { useParams, useNavigate } from "react-router";
import { DndContext, DragOverlay, closestCenter, type DragEndEvent, type DragStartEvent } from "@dnd-kit/core";
import { Plus, Settings, Shield } from "lucide-react";
import { toast } from "sonner";
import { useMetaType } from "@/providers/MetaProvider";
import { BuilderShell, BuilderToolbar, type IconRailItem } from "@/components/builder-kit";
import { PropertyPanel } from "@/components/builder-kit/PropertyPanel";
import { useDocTypeBuilderStore } from "@/stores/doctype-builder-store";
import { SchematicCanvas } from "@/components/doctype-builder/SchematicCanvas";
import { FieldPalette } from "@/components/doctype-builder/FieldPalette";
import { SettingsDrawer } from "@/components/doctype-builder/SettingsDrawer";
import { PermissionsDrawer } from "@/components/doctype-builder/PermissionsDrawer";
import { generateFieldName } from "@/components/doctype-builder/types";
import type { FieldType } from "@/api/types";

const ICON_RAIL: IconRailItem[] = [
  { id: "fields", icon: <Plus className="h-4 w-4" />, label: "Field Palette" },
  { id: "settings", icon: <Settings className="h-4 w-4" />, label: "Settings" },
  { id: "permissions", icon: <Shield className="h-4 w-4" />, label: "Permissions" },
];

export default function DocTypeBuilder() {
  const { name: paramName } = useParams<{ name?: string }>();
  const navigate = useNavigate();
  const store = useDocTypeBuilderStore();

  // Load existing DocType if editing
  const { data: meta } = useMetaType(paramName ?? "");
  useEffect(() => {
    if (meta && meta.layout && meta.fields_map) {
      store.hydrate({
        name: meta.name,
        module: meta.module ?? "",
        layout: meta.layout,
        fields: meta.fields_map,
        settings: {
          naming_rule: meta.naming_rule,
          title_field: meta.title_field ?? "",
          sort_field: meta.sort_field ?? "",
          sort_order: meta.sort_order ?? "desc",
          search_fields: meta.search_fields ?? [],
          image_field: meta.image_field ?? "",
          is_submittable: meta.is_submittable,
          is_single: meta.is_single,
          is_child_table: meta.is_child_table,
          track_changes: meta.track_changes ?? true,
          is_virtual: false,
        },
        permissions: [{ role: "System Manager", read: true, write: true, create: true, delete: true, submit: false, cancel: false }],
      });
    }
  }, [meta]);

  // Reset store when creating new
  useEffect(() => {
    if (!paramName) store.reset();
  }, [paramName]);

  // DnD handler
  const handleDragEnd = useCallback((event: DragEndEvent) => {
    const { active, over } = event;
    if (!over) return;

    const activeData = active.data.current;
    const overData = over.data.current;

    if (activeData?.type === "palette-field" && overData?.type === "column") {
      const fieldType = activeData.fieldType as FieldType;
      const existingNames = new Set(Object.keys(store.fields));
      const fieldName = generateFieldName(fieldType, existingNames);
      store.addField(fieldName, fieldType, overData.tabIdx, overData.sectionIdx, overData.colIdx);
    }
  }, [store]);

  // Drawer content based on active drawer
  const drawerContent = (() => {
    switch (store.activeDrawer) {
      case "fields": return <FieldPalette />;
      case "settings": return <SettingsDrawer />;
      case "permissions": return <PermissionsDrawer />;
      default: return null;
    }
  })();

  // Property panel for selected field
  const propertyPanel = store.selection?.type === "field" && store.fields[store.selection.id] ? (
    <div className="p-3">
      <h3 className="text-sm font-medium mb-3">{store.selection.id}</h3>
      {/* Property fields rendered inline for now — Task 8 adds full schema-driven panel */}
      <p className="text-xs text-muted-foreground">Select a field to edit its properties.</p>
    </div>
  ) : null;

  return (
    <DndContext collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
      <BuilderShell
        toolbar={
          <BuilderToolbar
            name={store.name}
            onNameChange={store.setName}
            mode={store.mode}
            onModeChange={store.setMode}
            onSave={() => toast.info("Save not yet implemented")}
            isDirty={store.isDirty}
          />
        }
        iconRailItems={ICON_RAIL}
        activeDrawer={store.activeDrawer}
        onDrawerToggle={store.toggleDrawer}
        leftDrawer={drawerContent}
        rightPanel={propertyPanel}
        statusBar={
          <span>{Object.keys(store.fields).length} fields{store.isDirty && " (unsaved changes)"}</span>
        }
      >
        <SchematicCanvas />
      </BuilderShell>
    </DndContext>
  );
}
```

- [ ] **Step 4: Add route to router**

In `desk/src/router.tsx`, add the builder route BEFORE the `:doctype` catch-all routes:

```typescript
// Add import at top:
const DocTypeBuilder = lazy(() => import("./pages/DocTypeBuilder"));

// Add routes inside the "app" children, BEFORE the :doctype routes:
{
  path: "doctype-builder",
  element: <Suspense fallback={null}><DocTypeBuilder /></Suspense>,
},
{
  path: "doctype-builder/:name",
  element: <Suspense fallback={null}><DocTypeBuilder /></Suspense>,
},
```

- [ ] **Step 5: Verify build**

Run: `cd /Users/osamamuhammed/Moca/desk && npx tsc --noEmit`
Expected: No type errors

- [ ] **Step 6: Commit**

```bash
git add desk/src/pages/DocTypeBuilder.tsx desk/src/components/doctype-builder/SettingsDrawer.tsx desk/src/components/doctype-builder/PermissionsDrawer.tsx desk/src/router.tsx
git commit -m "feat(desk): add DocTypeBuilder page with DnD, drawers, and routing"
```

---

### Task 8: Property Panel Schemas and Field Property Editing

**Files:**
- Create: `desk/src/components/doctype-builder/property-schemas.ts`
- Modify: `desk/src/pages/DocTypeBuilder.tsx` (wire up PropertyPanel with schemas)

- [ ] **Step 1: Create property schemas per field type**

Create `desk/src/components/doctype-builder/property-schemas.ts`:

```typescript
import type { PropertySchema } from "@/components/builder-kit/types";
import type { FieldType } from "@/api/types";

const BASIC_SECTION = {
  label: "Basic",
  properties: [
    { key: "label", label: "Label", type: "text" as const },
    { key: "name", label: "Field Name", type: "text" as const, description: "snake_case identifier" },
    { key: "field_type", label: "Field Type", type: "select" as const, options: [
      "Data", "Text", "LongText", "Markdown", "Code", "HTMLEditor",
      "Int", "Float", "Currency", "Percent", "Rating",
      "Date", "Datetime", "Time", "Duration",
      "Select", "Link", "DynamicLink",
      "Table", "TableMultiSelect",
      "Attach", "AttachImage", "Color", "Signature", "Barcode",
      "Check", "Password", "JSON",
      "HTML", "Heading", "Button",
    ]},
  ],
};

const DATA_SECTION = {
  label: "Data",
  properties: [
    { key: "options", label: "Options", type: "textarea" as const, dependsOn: (v: Record<string, unknown>) => ["Select"].includes(v.field_type as string), description: "One option per line" },
    { key: "options", label: "Target DocType", type: "text" as const, dependsOn: (v: Record<string, unknown>) => ["Link", "Table", "DynamicLink", "TableMultiSelect"].includes(v.field_type as string) },
    { key: "default", label: "Default Value", type: "text" as const },
    { key: "max_length", label: "Max Length", type: "number" as const, dependsOn: (v: Record<string, unknown>) => ["Data", "Text", "LongText", "Password"].includes(v.field_type as string) },
    { key: "min_value", label: "Min Value", type: "number" as const, dependsOn: (v: Record<string, unknown>) => ["Int", "Float", "Currency", "Percent"].includes(v.field_type as string) },
    { key: "max_value", label: "Max Value", type: "number" as const, dependsOn: (v: Record<string, unknown>) => ["Int", "Float", "Currency", "Percent"].includes(v.field_type as string) },
  ],
};

const VALIDATION_SECTION = {
  label: "Validation",
  properties: [
    { key: "required", label: "Required", type: "boolean" as const },
    { key: "unique", label: "Unique", type: "boolean" as const },
    { key: "validation_regex", label: "Validation Regex", type: "text" as const },
  ],
};

const DISPLAY_SECTION = {
  label: "Display",
  properties: [
    { key: "read_only", label: "Read Only", type: "boolean" as const },
    { key: "hidden", label: "Hidden", type: "boolean" as const },
    { key: "depends_on", label: "Depends On", type: "text" as const, description: "JS expression" },
    { key: "in_list_view", label: "In List View", type: "boolean" as const },
    { key: "in_filter", label: "In Filter", type: "boolean" as const },
    { key: "in_preview", label: "In Preview", type: "boolean" as const },
  ],
};

const SEARCH_SECTION = {
  label: "Search",
  collapsed: true,
  properties: [
    { key: "searchable", label: "Searchable", type: "boolean" as const },
    { key: "filterable", label: "Filterable", type: "boolean" as const },
    { key: "db_index", label: "DB Index", type: "boolean" as const },
    { key: "full_text_index", label: "Full Text Index", type: "boolean" as const },
  ],
};

const API_SECTION = {
  label: "API",
  collapsed: true,
  properties: [
    { key: "in_api", label: "In API", type: "boolean" as const },
    { key: "api_read_only", label: "API Read Only", type: "boolean" as const },
    { key: "api_alias", label: "API Alias", type: "text" as const },
  ],
};

export function getFieldPropertySchema(_fieldType: FieldType): PropertySchema {
  return {
    sections: [BASIC_SECTION, DATA_SECTION, VALIDATION_SECTION, DISPLAY_SECTION, SEARCH_SECTION, API_SECTION],
  };
}

export const SECTION_PROPERTY_SCHEMA: PropertySchema = {
  sections: [{
    label: "Section",
    properties: [
      { key: "label", label: "Label", type: "text" as const },
      { key: "collapsible", label: "Collapsible", type: "boolean" as const },
      { key: "collapsed_by_default", label: "Collapsed by Default", type: "boolean" as const },
    ],
  }],
};

export const TAB_PROPERTY_SCHEMA: PropertySchema = {
  sections: [{
    label: "Tab",
    properties: [
      { key: "label", label: "Label", type: "text" as const },
    ],
  }],
};

export const COLUMN_PROPERTY_SCHEMA: PropertySchema = {
  sections: [{
    label: "Column",
    properties: [
      { key: "width", label: "Width (relative)", type: "number" as const },
    ],
  }],
};
```

- [ ] **Step 2: Wire PropertyPanel in DocTypeBuilder page**

Update the `propertyPanel` section in `desk/src/pages/DocTypeBuilder.tsx` to use the schema-driven panel:

Replace the placeholder property panel with:

```typescript
import { PropertyPanel } from "@/components/builder-kit/PropertyPanel";
import { getFieldPropertySchema } from "@/components/doctype-builder/property-schemas";

// In the component body, replace the propertyPanel const:
const propertyPanel = (() => {
  if (!store.selection) return null;
  if (store.selection.type === "field") {
    const fd = store.fields[store.selection.id];
    if (!fd) return null;
    const schema = getFieldPropertySchema(fd.field_type);
    return (
      <div className="p-3">
        <PropertyPanel
          schema={schema}
          values={fd as unknown as Record<string, unknown>}
          onChange={(key, value) => store.updateField(store.selection!.id, { [key]: value })}
          title={fd.name}
        />
      </div>
    );
  }
  return null;
})();
```

- [ ] **Step 3: Verify build**

Run: `cd /Users/osamamuhammed/Moca/desk && npx tsc --noEmit`
Expected: No type errors

- [ ] **Step 4: Commit**

```bash
git add desk/src/components/doctype-builder/property-schemas.ts desk/src/pages/DocTypeBuilder.tsx
git commit -m "feat(desk): add schema-driven property panel for field editing"
```

---

### Task 9: Keyboard Shortcuts, Navigation Entry Points, and Save Integration

**Files:**
- Modify: `desk/src/pages/DocTypeBuilder.tsx` (keyboard shortcuts, save logic)
- Modify: `desk/src/components/shell/Sidebar.tsx` (builder link)
- Modify: `desk/src/components/shell/CommandPalette.tsx` (builder action)

- [ ] **Step 1: Add keyboard shortcuts to DocTypeBuilder**

Add this `useEffect` inside the `DocTypeBuilder` component:

```typescript
useEffect(() => {
  const handler = (e: KeyboardEvent) => {
    const mod = e.metaKey || e.ctrlKey;
    if (mod && e.key === "s") {
      e.preventDefault();
      handleSave();
    }
    if (mod && e.key === "z" && !e.shiftKey) {
      e.preventDefault();
      // undo — will be wired when CommandHistory is integrated
    }
    if (mod && e.key === "z" && e.shiftKey) {
      e.preventDefault();
      // redo
    }
    if (e.key === "Backspace" && store.selection?.type === "field" && document.activeElement?.tagName !== "INPUT" && document.activeElement?.tagName !== "TEXTAREA") {
      e.preventDefault();
      store.removeField(store.selection.id);
    }
    if (e.key === "Escape") {
      store.setSelection(null);
      store.setActiveDrawer(null);
    }
  };
  window.addEventListener("keydown", handler);
  return () => window.removeEventListener("keydown", handler);
}, [store.selection]);
```

- [ ] **Step 2: Add save function**

Add save logic using the dev API:

```typescript
import { post, put } from "@/api/client";

const handleSave = useCallback(async () => {
  try {
    const payload = {
      name: store.name,
      app: store.app,
      module: store.module,
      layout: store.layout,
      fields: store.fields,
      settings: store.settings,
      permissions: store.permissions,
    };
    if (store.isNew) {
      await post("dev/doctype", payload);
      store.markClean();
      navigate(`/desk/app/doctype-builder/${store.name}`, { replace: true });
      toast.success(`${store.name} created`);
    } else {
      await put(`dev/doctype/${store.name}`, payload);
      store.markClean();
      toast.success(`${store.name} saved`);
    }
  } catch (err: any) {
    toast.error(err?.message ?? "Save failed");
  }
}, [store, navigate]);
```

- [ ] **Step 3: Add beforeunload warning**

```typescript
useEffect(() => {
  const handler = (e: BeforeUnloadEvent) => {
    if (store.isDirty) {
      e.preventDefault();
    }
  };
  window.addEventListener("beforeunload", handler);
  return () => window.removeEventListener("beforeunload", handler);
}, [store.isDirty]);
```

- [ ] **Step 4: Add Sidebar entry for DocType Builder**

In `desk/src/components/shell/Sidebar.tsx`, add a link in the Core module section or as a custom sidebar item. Add to the navigation items:

```typescript
// Add alongside existing sidebar items, before or after home link:
<SidebarItem
  label="DocType Builder"
  path="/desk/app/doctype-builder"
  icon={<Blocks className="h-4 w-4" />}
/>
```

- [ ] **Step 5: Add Command Palette action**

In `desk/src/components/shell/CommandPalette.tsx`, add a "New DocType" command:

```typescript
// Inside the Navigation command group, add:
<CommandItem onSelect={() => { navigate("/desk/app/doctype-builder"); setOpen(false); }}>
  New DocType
</CommandItem>
```

- [ ] **Step 6: Verify build**

Run: `cd /Users/osamamuhammed/Moca/desk && npx tsc --noEmit`
Expected: No type errors

- [ ] **Step 7: Commit**

```bash
git add desk/src/pages/DocTypeBuilder.tsx desk/src/components/shell/Sidebar.tsx desk/src/components/shell/CommandPalette.tsx
git commit -m "feat(desk): add keyboard shortcuts, save integration, and navigation entry points"
```

---

### Task 10: Preview Mode

**Files:**
- Create: `desk/src/components/doctype-builder/PreviewMode.tsx`
- Modify: `desk/src/pages/DocTypeBuilder.tsx` (toggle between schematic and preview)

- [ ] **Step 1: Create PreviewMode component**

Create `desk/src/components/doctype-builder/PreviewMode.tsx`:

```typescript
import { useMemo } from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { FieldRenderer } from "@/components/fields/FieldRenderer";
import { SectionBreak } from "@/components/layout/SectionBreak";
import { useDocTypeBuilderStore } from "@/stores/doctype-builder-store";
import type { FieldDef } from "@/api/types";

export function PreviewMode() {
  const layout = useDocTypeBuilderStore((s) => s.layout);
  const fields = useDocTypeBuilderStore((s) => s.fields);

  return (
    <div className="max-w-4xl mx-auto">
      <Tabs defaultValue="0">
        <TabsList>
          {layout.tabs.map((tab, i) => (
            <TabsTrigger key={i} value={String(i)}>{tab.label}</TabsTrigger>
          ))}
        </TabsList>
        {layout.tabs.map((tab, tabIdx) => (
          <TabsContent key={tabIdx} value={String(tabIdx)} className="space-y-4 mt-4">
            {tab.sections.map((section, secIdx) => (
              <div key={secIdx}>
                {section.label && (
                  <SectionBreak
                    fieldDef={{ name: `sec_${secIdx}`, field_type: "SectionBreak", label: section.label, layout_label: section.label, collapsible: section.collapsible, collapsed_by_default: section.collapsed_by_default } as FieldDef}
                  >
                    <div className={`grid gap-4`} style={{ gridTemplateColumns: section.columns.map((c) => `${c.width}fr`).join(" ") }}>
                      {section.columns.map((col, colIdx) => (
                        <div key={colIdx} className="space-y-3">
                          {col.fields.map((fieldName) => {
                            const fd = fields[fieldName];
                            if (!fd) return null;
                            return (
                              <FieldRenderer
                                key={fieldName}
                                fieldDef={fd}
                                value={fd.default ?? ""}
                                onChange={() => {}}
                                readOnly
                              />
                            );
                          })}
                        </div>
                      ))}
                    </div>
                  </SectionBreak>
                )}
                {!section.label && (
                  <div className={`grid gap-4`} style={{ gridTemplateColumns: section.columns.map((c) => `${c.width}fr`).join(" ") }}>
                    {section.columns.map((col, colIdx) => (
                      <div key={colIdx} className="space-y-3">
                        {col.fields.map((fieldName) => {
                          const fd = fields[fieldName];
                          if (!fd) return null;
                          return (
                            <FieldRenderer
                              key={fieldName}
                              fieldDef={fd}
                              value={fd.default ?? ""}
                              onChange={() => {}}
                              readOnly
                            />
                          );
                        })}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            ))}
          </TabsContent>
        ))}
      </Tabs>
    </div>
  );
}
```

- [ ] **Step 2: Toggle between modes in DocTypeBuilder**

In `desk/src/pages/DocTypeBuilder.tsx`, update the canvas area to switch between modes:

```typescript
import { PreviewMode } from "@/components/doctype-builder/PreviewMode";

// In the BuilderShell children:
{store.mode === "schematic" ? <SchematicCanvas /> : <PreviewMode />}
```

- [ ] **Step 3: Verify build and test in browser**

Run: `cd /Users/osamamuhammed/Moca/desk && npx tsc --noEmit`
Start dev server: `cd /Users/osamamuhammed/Moca/desk && npm run dev`
Navigate to `http://localhost:5173/desk/app/doctype-builder` and test:
- Add fields from palette via drag
- Toggle between Schematic and Preview modes
- Verify preview renders field components

- [ ] **Step 4: Commit**

```bash
git add desk/src/components/doctype-builder/PreviewMode.tsx desk/src/pages/DocTypeBuilder.tsx
git commit -m "feat(desk): add live preview mode for DocType Builder"
```

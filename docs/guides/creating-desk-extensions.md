# Creating Desk Extensions for Your Moca App

Moca apps can extend the Desk UI by declaring custom field types, pages, sidebar items, and dashboard widgets. Extensions are declared in a `desk-manifest.json` file and wired in at build time.

## Overview

The extension system supports four types:

| Type | Purpose | Registration API |
|------|---------|------------------|
| **Field types** | Custom field components for document forms | `registerFieldType()` |
| **Pages** | Custom routes in the desk app | `registerPage()` |
| **Sidebar items** | Navigation entries in the desk sidebar | `registerSidebarItem()` |
| **Dashboard widgets** | Custom widgets on the desk dashboard | `registerDashboardWidget()` |

## Setting Up App Desk Extensions

Create a `desk/` directory in your app with a `desk-manifest.json`:

```
apps/
└── crm/
    ├── go.mod
    ├── manifest.yaml
    ├── modules/
    └── desk/
        ├── desk-manifest.json    # Extension declarations
        ├── fields/
        │   └── PhoneField.tsx
        ├── pages/
        │   └── CRMDashboard.tsx
        ├── sidebar/
        │   └── crm-items.ts
        └── widgets/
            └── PipelineWidget.tsx
```

When scaffolding a new app with desk extensions:

```bash
moca app new crm --desk
```

## desk-manifest.json

The manifest declares what your app extends. All fields are validated against the JSON Schema at `docs/schemas/desk-manifest.schema.json`.

### Full example

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
        "order": 10,
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
        "component": "./widgets/PipelineWidget.tsx",
        "label": "Sales Pipeline"
      }
    ]
  }
}
```

### Required fields

- **`app`** -- App identifier, must match `^[a-z][a-z0-9_]*$` (e.g., `crm`, `hr_module`)
- **`version`** -- Semantic version (e.g., `1.0.0`)

### Extension rules

- Component paths must start with `./` (relative to the app's `desk/` directory)
- Page paths must start with `/desk/app/`
- Widget names must be unique across all apps
- Page paths must be unique across all apps

## Custom Field Types

Create a React component that implements the `FieldProps` interface:

```tsx
// apps/crm/desk/fields/PhoneField.tsx
import type { FieldProps } from "@moca/desk";

export default function PhoneField({ value, onChange, field, readOnly }: FieldProps) {
  const formatted = formatPhone(value as string);

  if (readOnly) {
    return <a href={`tel:${value}`} className="text-blue-600 underline">{formatted}</a>;
  }

  return (
    <input
      type="tel"
      value={(value as string) ?? ""}
      onChange={(e) => onChange(e.target.value)}
      placeholder={field.label}
      className="w-full rounded border px-3 py-2"
    />
  );
}

function formatPhone(phone: string): string {
  if (!phone) return "";
  const digits = phone.replace(/\D/g, "");
  if (digits.length === 10) {
    return `(${digits.slice(0, 3)}) ${digits.slice(3, 6)}-${digits.slice(6)}`;
  }
  return phone;
}
```

Register it in `desk-manifest.json`:

```json
{
  "field_types": {
    "Phone": "./fields/PhoneField.tsx"
  }
}
```

The field type name (`Phone`) must match the `fieldtype` value in your MetaType field definitions.

## Custom Pages

Create a page component and register its route:

```tsx
// apps/crm/desk/pages/CRMDashboard.tsx
import { useDocList } from "@moca/desk";

export default function CRMDashboard() {
  const { data: leads } = useDocList("Lead", { filters: { status: "Open" } });

  return (
    <div className="p-6">
      <h1 className="text-2xl font-bold mb-4">CRM Dashboard</h1>
      <div className="grid grid-cols-3 gap-4">
        <div className="rounded-lg border p-4">
          <h2 className="text-sm text-muted-foreground">Open Leads</h2>
          <p className="text-3xl font-bold">{leads?.length ?? 0}</p>
        </div>
      </div>
    </div>
  );
}
```

Register in `desk-manifest.json`:

```json
{
  "pages": [
    {
      "path": "/desk/app/crm-dashboard",
      "component": "./pages/CRMDashboard.tsx",
      "label": "CRM Dashboard",
      "icon": "LayoutDashboard"
    }
  ]
}
```

## Sidebar Navigation

Add navigation entries to the desk sidebar:

```json
{
  "sidebar_items": [
    {
      "label": "CRM",
      "icon": "Phone",
      "order": 10,
      "children": [
        { "label": "Dashboard", "path": "/desk/app/crm-dashboard" },
        { "label": "Leads", "path": "/desk/app/Lead" }
      ]
    }
  ]
}
```

The `order` property controls position among custom sidebar items (lower = higher in the sidebar). Default is 999.

## Dashboard Widgets

Create a widget component:

```tsx
// apps/crm/desk/widgets/PipelineWidget.tsx
export default function PipelineWidget() {
  return (
    <div className="rounded-lg border p-4">
      <h3 className="font-semibold mb-2">Sales Pipeline</h3>
      {/* Widget content */}
    </div>
  );
}
```

Register in `desk-manifest.json`:

```json
{
  "dashboard_widgets": [
    {
      "name": "crm_pipeline",
      "component": "./widgets/PipelineWidget.tsx",
      "label": "Sales Pipeline"
    }
  ]
}
```

## Build Process

When you run `moca build desk`, the following happens:

1. Scans all `apps/*/desk/desk-manifest.json` files
2. Validates each manifest against the JSON Schema
3. Generates `.moca-extensions.ts` with typed imports and registration calls:

```typescript
// Auto-generated by 'moca build desk'. Do not edit.
import { registerFieldType, registerPage, registerSidebarItem, registerDashboardWidget } from "@moca/desk";

// === crm ===
import CrmFieldPhoneField from "../apps/crm/desk/fields/PhoneField";
registerFieldType("Phone", CrmFieldPhoneField);

import CrmPageCRMDashboard from "../apps/crm/desk/pages/CRMDashboard";
registerPage("/desk/app/crm-dashboard", CrmPageCRMDashboard, { label: "CRM Dashboard", icon: "Phone" });

registerSidebarItem({ label: "CRM", icon: "Phone", order: 10, children: [
  { label: "Dashboard", path: "/desk/app/crm-dashboard" },
  { label: "Leads", path: "/desk/app/Lead" },
]});

import CrmWidgetPipelineWidget from "../apps/crm/desk/widgets/PipelineWidget";
registerDashboardWidget("crm_pipeline", CrmWidgetPipelineWidget, { label: "Sales Pipeline" });
```

4. Vite builds the project, tree-shaking and code-splitting the output

## Legacy Mode

For apps not yet using `desk-manifest.json`, the build system falls back to looking for:

1. `desk/setup.ts`
2. `desk/setup.tsx`
3. `desk/index.ts`

If found, it generates a bare side-effect import:

```typescript
import "../apps/legacy_app/desk/setup";
```

In legacy mode, you register extensions manually:

```typescript
// apps/legacy_app/desk/setup.ts
import { registerFieldType } from "@moca/desk";
import MyField from "./fields/MyField";
registerFieldType("MyCustom", MyField);
```

The manifest-based approach is preferred. When both a manifest and legacy file exist, the manifest takes precedence.

## Testing Extensions

During development, `moca desk dev` regenerates extensions before starting the Vite dev server:

```bash
moca desk dev
```

Changes to your extension components are picked up by Vite's hot module replacement. If you add or remove entries in `desk-manifest.json`, restart the dev server to regenerate `.moca-extensions.ts`.

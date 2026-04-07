# Customizing Your Project's Desk Theme

Moca Desk uses a three-layer composition model. Project-level customizations are applied last, giving you full control over appearance and behavior without modifying framework code.

## Three-Layer Composition

```
┌─────────────────────────────────────────────────┐
│  Layer 3: Project Overrides  (HIGHEST PRIORITY) │
│  desk/src/overrides/                            │
│  Theme, branding, custom pages, locale config   │
├─────────────────────────────────────────────────┤
│  Layer 2: App Extensions                        │
│  apps/*/desk/ (discovered by moca build desk)   │
│  Custom field types, pages, sidebar items       │
├─────────────────────────────────────────────────┤
│  Layer 1: @moca/desk (npm package)              │
│  Core shell, providers, standard fields,        │
│  FormView, ListView, routing, API client        │
└─────────────────────────────────────────────────┘
```

Project overrides run after app extensions and can override any framework default.

## Project-Level Overrides

Your project's `desk/src/overrides/` directory is the entry point for customizations:

```
desk/src/overrides/
├── index.ts    # Import all overrides here
└── theme.ts    # Theme and branding configuration
```

### `overrides/index.ts`

This file is imported by `main.tsx` as a side-effect. Register custom field types, pages, or sidebar items here:

```typescript
import { registerFieldType, registerSidebarItem } from "@moca/desk";
import BrandedCurrencyField from "./BrandedCurrencyField";

// Override the default currency field with project-specific formatting
registerFieldType("Currency", BrandedCurrencyField);

// Add project-specific navigation
registerSidebarItem({
  label: "Reports",
  icon: "BarChart",
  order: 5,
  children: [
    { label: "Revenue", path: "/desk/app/revenue-report" },
    { label: "Inventory", path: "/desk/app/inventory-report" },
  ],
});
```

### `overrides/theme.ts`

Use this file for visual customization. Theme values are passed to `createDeskApp()` in `main.tsx`.

## Configuring createDeskApp()

The `createDeskApp()` factory accepts a configuration object:

```tsx
// desk/src/main.tsx
import { createDeskApp } from "@moca/desk";
import "../.moca-extensions";
import "./overrides";

const app = createDeskApp({
  // Base path for all desk routes (default: "/desk")
  basePath: "/desk",

  // API base URL prefix (default: "/api/v1")
  apiBaseUrl: "/api/v1",

  // Site name for X-Moca-Site header (default: from VITE_MOCA_SITE env)
  siteName: "my-site.localhost",

  // WebSocket endpoint override (default: auto-derived from location)
  wsEndpoint: "ws://localhost:8000/ws",

  // Toast notification position (default: "bottom-right")
  toasterPosition: "top-right",
});

app.mount("#root");
```

### DeskConfig Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `basePath` | `string` | `"/desk"` | Base path for all desk routes |
| `apiBaseUrl` | `string` | `"/api/v1"` | API URL prefix |
| `siteName` | `string` | `VITE_MOCA_SITE` env | Site name sent via `X-Moca-Site` header |
| `wsEndpoint` | `string?` | auto-derived | WebSocket endpoint override |
| `toasterPosition` | `string?` | `"bottom-right"` | Toast position (`top-left`, `top-right`, `bottom-left`, `bottom-right`, `top-center`, `bottom-center`) |

## Tailwind CSS Customization

Moca Desk uses Tailwind CSS v4 with the CSS-based configuration model. The `@tailwindcss/vite` plugin is included by `mocaDeskPlugin()`.

To add custom design tokens, create a CSS file in your overrides:

```css
/* desk/src/overrides/custom.css */
@import "tailwindcss";

@theme {
  --color-brand: #1a73e8;
  --color-brand-light: #e8f0fe;
  --font-display: "Inter", sans-serif;
}
```

Import it in `overrides/index.ts`:

```typescript
import "./custom.css";
export {};
```

## Custom Field Types as Branding Elements

Register project-specific field components that match your brand:

```tsx
// desk/src/overrides/BrandedCurrencyField.tsx
import type { FieldProps } from "@moca/desk";

export default function BrandedCurrencyField({ value, onChange, readOnly }: FieldProps) {
  const formatted = new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
  }).format(Number(value) || 0);

  if (readOnly) {
    return <span className="font-mono text-brand">{formatted}</span>;
  }

  return (
    <input
      type="number"
      step="0.01"
      value={(value as number) ?? 0}
      onChange={(e) => onChange(parseFloat(e.target.value))}
      className="w-full rounded border px-3 py-2 font-mono"
    />
  );
}
```

## Environment-Specific Configuration

Use `.env` files for environment-specific settings:

```bash
# desk/.env.development
VITE_MOCA_SITE=devsite
```

```bash
# desk/.env.production
VITE_MOCA_SITE=production.example.com
```

The `VITE_MOCA_SITE` variable is read by `DeskConfig` and sent as the `X-Moca-Site` header with every API request.

## Vite Plugin Options

The `mocaDeskPlugin()` accepts options to override defaults:

```typescript
// desk/vite.config.ts
import { defineConfig } from "vite";
import { mocaDeskPlugin } from "@moca/desk/vite";

export default defineConfig({
  plugins: [
    mocaDeskPlugin({
      basePath: "/admin/",           // Change base path
      apiTarget: "http://api:8000",  // Backend API target
      wsTarget: "http://api:8000",   // WebSocket target
      port: 4000,                    // Dev server port
    }),
  ],
});
```

| Option | Default | Description |
|--------|---------|-------------|
| `basePath` | `"/desk/"` | Base path for the desk app |
| `apiTarget` | `"http://localhost:8000"` | Backend API target for dev proxy |
| `wsTarget` | `"http://localhost:8000"` | Backend WebSocket target for dev proxy |
| `port` | `3000` | Vite dev server port |

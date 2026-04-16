# I18n End-to-End Implementation Design

**Date:** 2026-04-16
**Status:** Approved
**Scope:** Complete the translation system — backend DocTypes, frontend wiring, RTL, language selector, translation management UI

## Context

The Moca backend has a fully implemented i18n system in `pkg/i18n/` (Translator, I18nMiddleware, I18nTransformer, Extractor, CLI commands, `tab_translation` table). The frontend desk has an I18nProvider with a `t()` function that is wired into the provider stack but never actually used in any component. There is no Translation or Language DocType, no language selector UI, no RTL support, and the API client does not send language headers.

This design completes the i18n story end-to-end.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Scope | Both — desk UI translates AND apps inherit i18n | Moca is a framework; both paths are needed |
| RTL | Day-one requirement | Arabic is a primary target language |
| Translation management | DocType + dedicated bulk editing page | Standard CRUD for single edits, spreadsheet grid for bulk work |
| Language selector | Topbar globe icon with popover | Discoverable without cluttering the user dropdown |
| Login page | Browser `Accept-Language` only, no selector | Simplest approach; user preference takes over after login |
| Field label translation | Server-side via I18nTransformer | Leverages existing backend; client `t()` only for desk chrome |

## Section 1: Data Model

### Language DocType

Location: `pkg/builtin/core/modules/core/doctypes/language/`

| Field | Type | Notes |
|-------|------|-------|
| `name` | Data (PK) | Language code, e.g. `en`, `ar`, `fr`. Manual naming rule. |
| `language_name` | Data | Display name, e.g. "English", "العربية" |
| `direction` | Select (`ltr`, `rtl`) | Text direction |
| `enabled` | Check | Whether available for selection in the desk |

Seeded on site creation with:

| code | language_name | direction | enabled |
|------|--------------|-----------|---------|
| `en` | English | ltr | 1 |
| `ar` | العربية | rtl | 1 |
| `fr` | Francais | ltr | 0 |
| `es` | Espanol | ltr | 0 |
| `de` | Deutsch | ltr | 0 |

### Translation DocType

Location: `pkg/builtin/core/modules/core/doctypes/translation/`

Maps to the existing `tab_translation` system table. No schema migration needed.

| Field | Type | Notes |
|-------|------|-------|
| `source_text` | Small Text | Original string |
| `language` | Link (Language) | Target language code |
| `translated_text` | Small Text | Translated string |
| `context` | Data | Disambiguation context (e.g. `DocType:User:field:email`) |
| `app` | Data | Originating app (optional) |

Composite uniqueness: `(source_text, language, context)` — matches the existing PK on `tab_translation`.

## Section 2: Frontend Wiring

### 2a. API Client Sends `Accept-Language` Header

In `desk/src/api/client.ts`, add a module-level language getter so the API client (outside React) can access the current language:

```typescript
let getLanguage: () => string = () => "en";
export function setLanguageGetter(fn: () => string) { getLanguage = fn; }

// In request() — add to headers:
headers["Accept-Language"] = getLanguage();
```

The I18nProvider calls `setLanguageGetter` on mount with a function returning the current language.

### 2b. MetaType Cache Becomes Language-Aware

`useMetaType` cache key changes from `["meta", doctype]` to `["meta", doctype, language]`.

When the user switches language, React Query automatically refetches because the cache key changed. No manual invalidation needed.

### 2c. Language Change Flow

1. User clicks globe icon, selects Arabic
2. I18nProvider updates language state -> refetches translation bundle for desk chrome
3. All `useMetaType` queries get new cache keys -> refetch with `Accept-Language: ar` -> server returns translated field labels via I18nTransformer
4. `document.documentElement.dir` set to `"rtl"`, `document.documentElement.lang` set to `"ar"`
5. User's language preference saved to User document server-side

## Section 3: `t()` Wiring for Desk Chrome

Only desk UI chrome strings need `t()` wrapping. Field labels are NOT wrapped — they arrive pre-translated from the server via I18nTransformer.

### Strings to Wrap

| Location | Examples |
|----------|---------|
| Topbar | "Profile", "Settings", "Log out", "Home" |
| Sidebar | "Search...", "Home", "DocType Builder", module group names |
| FormView | "Save", "Cancel", "Delete", "Submit", "Amend", "New {doctype}" |
| ListView | "New", "Refresh", "No records found", "Filters" |
| Login page | "Email", "Password", "Login", "Invalid credentials" |
| Command Palette | "Type a command or search...", "Recent", "DocTypes", "Actions", "No results found" |
| Common | "Saved successfully", "Are you sure?", "Loading..." |

### Usage Pattern

```tsx
const { t } = useI18n();
<Button>{t("Save")}</Button>
```

Estimated scope: ~50-80 unique strings across the desk.

## Section 4: Language Selector — Topbar Globe Icon

### Component: `LanguageSwitcher`

Positioned in the Topbar between the connection status indicator and the user dropdown.

- **Trigger:** `Languages` icon from lucide-react with current language code badge (e.g. "EN", "AR")
- **Popover:** Lists enabled languages fetched via `GET /api/v1/resource/Language?filters=[["enabled","=",1]]`
- **Each item:** `language_name` in native script (e.g. "العربية") with checkmark on active language
- **On select:**
  1. Update user's language via `PUT /api/v1/resource/User/{name}` with `{ language: "ar" }`
  2. I18nProvider picks up change -> refetches translation bundle
  3. MetaType cache keys change -> refetch with new `Accept-Language`
  4. Set `dir` attribute on `<html>` based on Language's `direction` field

### Persistence

Language preference is saved to the User document server-side. Survives across sessions and devices. JWT `user_defaults.language` updates on next token refresh.

## Section 5: RTL Support

### 5a. shadcn Migration

The desk uses `radix-nova` style with `components.json` already containing `"rtl": false`. The migration is:

1. Set `"rtl": true` in `components.json`
2. Run `pnpm dlx shadcn@latest migrate rtl` to convert all shadcn component classes

This auto-converts physical CSS classes to logical equivalents: `ml-*` -> `ms-*`, `left-*` -> `start-*`, `text-left` -> `text-start`, etc.

### 5b. `dir` Attribute on `<html>`

Handled in I18nProvider. When language state changes, look up the Language record's `direction` field and set:

```typescript
document.documentElement.dir = direction; // "rtl" or "ltr"
document.documentElement.lang = language; // "ar", "en", etc.
```

Tailwind's `rtl:` variant activates automatically based on this attribute.

### 5c. Manual Component Fixes

Per shadcn docs, 3 components need manual RTL attention:
- **Sidebar** — collapse/expand direction flip
- **Calendar** — date picker navigation arrows
- **Pagination** — prev/next arrow direction

### 5d. Custom CSS Pass

Non-shadcn code (Topbar, FormView, ListView, page components) that uses physical Tailwind properties (`ml-`, `mr-`, `pl-`, `pr-`, `left-`, `right-`, `text-left`, `text-right`) must be manually converted to logical equivalents (`ms-`, `me-`, `ps-`, `pe-`, `start-`, `end-`, `text-start`, `text-end`).

The `shadcn migrate rtl` command only handles files in the `ui` directory.

### 5e. What We Don't Build

- No custom RTL CSS framework — Tailwind + shadcn handles it
- No CSS-in-JS direction flipping — logical properties handle it natively
- No separate RTL stylesheet — single codebase works for both directions

## Section 6: Translation Management UI

### 6a. Translation DocType — Standard CRUD

Once registered as a DocType, Translation automatically gets ListView and FormView through existing desk infrastructure. No custom code needed for single-record management.

### 6b. Translation Tool — Dedicated Bulk Page

Route: `/desk/app/translation-tool`

**Layout:**
- **Top bar:** Language selector dropdown + App filter + Coverage progress bar (e.g. "142/203 translated — 70%")
- **Main area:** Spreadsheet-like grid with columns: Source Text | Context | Translated Text (editable inline)
- **Filters:** Untranslated only toggle, context prefix filter (e.g. `DocType:User`), source text search

**Functionality:**
- Loads all Translation records for the selected language via `GET /api/v1/resource/Translation?filters=[["language","=","{lang}"]]` and all extractable source strings via the existing `pkg/i18n` Extractor (exposed through a new `GET /api/v1/translations/{lang}/coverage` endpoint that returns source strings with their translation status)
- Inline editing — click Translated Text cell, type, tab to next row
- Bulk save — collects dirty rows, batch creates/updates Translation records
- Untranslated strings highlighted with subtle background color
- Coverage indicator updates in real-time as translations are added

**Access:**
- Sidebar under a "Tools" or "Settings" module group
- Command Palette via "Translation Tool"

## Section 7: Backend Additions

### 7a. Register DocTypes

Add Language and Translation JSON definitions in `pkg/builtin/core/modules/core/doctypes/` and register them in the core module loader. They appear in the desk sidebar, get API routes, and are manageable through standard CRUD.

### 7b. Seed Languages

`moca site create` seeds the Language table with the base set (en, ar, fr, es, de). Only en and ar enabled by default.

### 7c. Extend Translations Endpoint

Extend `GET /api/v1/translations/{lang}` response to include direction:

```json
{
  "data": { "Save": "حفظ", "Cancel": "إلغاء" },
  "direction": "rtl"
}
```

The I18nProvider already fetches this endpoint on language change, so direction info comes without an extra round trip.

### 7d. Translation DocType Table Mapping

The Translation DocType definition uses `table_name: "tab_translation"` to map to the existing system table. No migration needed. The existing `pkg/i18n` Translator, CLI commands, and Redis cache all continue to work unchanged.

## Out of Scope

- Login page language selector (browser `Accept-Language` handles pre-login)
- Third-party i18n library (custom I18nProvider already exists)
- Server-side rendering of desk chrome translations
- Automatic machine translation
- Translation memory / TM suggestions
- Pluralization rules (can be added later)

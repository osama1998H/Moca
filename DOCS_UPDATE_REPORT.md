# Docs Update Report

Date: 2026-04-12

## Reviewed Commit Range

- Main repo default branch: `main`
- Main repo commits reviewed from the last 24 hours:
  - `f8f25231312c7cf4e1872492d4f7a1c41df39cc2` `update docs`

## Reviewed Submodule Commit Ranges

- Desk submodule:
  - pointer change on `main` in last 24 hours: none
  - locally available current commit: `dff635ed6e391000f44470077e3f803bafa484e9`
  - reviewed commit range: none
- Wiki submodule pointer change on `main`:
  - old: `8bd9fb6fdbbbd57d87b38b55b4b12ed5b0985452`
  - new: `1538d6a4492f8d63915f40c0ea8fa6427f9f4b76`
  - reviewed upstream wiki commit(s):
    - `1538d6a4492f8d63915f40c0ea8fa6427f9f4b76` `add docx`

## Impacted Features / Modules

- Builtin core DocTypes: `Notification` and `NotificationSettings`
- Desk bootstrap and `X-Moca-Site` propagation
- `moca app install` seeding of `DocType` and `DocPerm` records
- Interactive API docs: `/api/docs` and `/api/v1/openapi.json`
- Browser auth cookie flow: `moca_sid` and `moca_rid`
- Notification delivery configuration
- Notification REST endpoints

## Wiki Sections Touched

- Core Concepts / App System
- Desk / Getting Started
- Guides / Creating Your First App
- Guides / REST API Usage
- Operations / Security
- Reference / Configuration
- Reference / REST API

## Files Changed

- Upstream wiki files reviewed in `8bd9fb6..1538d6a`:
  - `wiki/Concepts-App-System.md`
  - `wiki/Desk-Getting-Started.md`
  - `wiki/Guide-Creating-Your-First-App.md`
  - `wiki/Guide-REST-API-Usage.md`
  - `wiki/Operations-Security.md`
  - `wiki/Reference-Configuration.md`
  - `wiki/Reference-REST-API.md`
- Additional local documentation correction applied this run:
  - `wiki/Desk-Getting-Started.md`
- Report artifact updated:
  - `DOCS_UPDATE_REPORT.md`

## New Docs Created

- None

## Documentation Decisions

- Kept the upstream wiki changes because they matched the current backend and framework implementation.
- Applied one follow-up correction to `Desk-Getting-Started.md` because its `desk/src/main.tsx` example no longer matched the actual scaffolded starter. The page now shows `createDeskApp().mount("#root")` as the default bootstrap, while keeping explicit `siteName` passing as an optional override pattern.

## Validation

- Ran `git -C /Users/osamamuhammed/Moca/wiki diff --check`
- Checked all local wiki links referenced by the touched pages; all targets resolved
- Searched for repository-provided markdown or docs lint tasks; none were present in the main repo or wiki

## Skipped Items and Reasons

- Desk submodule commit review
  - Reason: no desk pointer change landed on `main` in the last 24 hours
- Additional wiki rewrites outside the reviewed topics
  - Reason: no in-scope code changes required broader documentation churn
- Full `Reference-Configuration.md` schema rewrite
  - Reason: broader pre-existing cleanup, not required by this 24-hour change window

## Uncertainties

- The backend now documents cookie-first refresh behavior, but the current desk submodule still stores and posts `refresh_token` in its client code. I kept the framework docs aligned with the backend implementation and treated the desk mismatch as a runtime follow-up, not a reason to rewrite the API docs back to stale behavior.
- The current automation worktree could not initialize the `wiki` submodule path because of a local worktree gitdir layout issue, so the source-of-truth wiki edit was applied in the existing local clone at `/Users/osamamuhammed/Moca/wiki`.

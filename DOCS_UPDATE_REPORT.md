# Docs Update Report

Date: 2026-04-09

## Reviewed Commit Range

- Main repo default branch: `main`
- Main repo commits reviewed from last 24 hours:
  - `124456efb6996a90bd6bdfa6b8837abc614f3127` `shit fixed`
  - `14ac154312621f46f5120233b0ffe2c9fc94e8ac` `shit fixed`
  - `9e6d94451dcb159589f1f0b8de4cedfe5beefe45` `Create stale.yml`
  - `861c72a27dcb7a1787142d5376578433cec673c2` `MS-22-T1`

## Reviewed Submodule Commit Ranges

- Desk submodule pointer change:
  - old: `7b235b5c5737d2185ceab0a631206c6b2eedc201`
  - new: `c4c1f745ec5dfa7b2675ec98f4eb10577239d211`
  - reviewed commit(s):
    - `c4c1f745ec5dfa7b2675ec98f4eb10577239d211` `same shit fixed`
- Wiki submodule upstream range reviewed before editing:
  - current upstream commit: `dbaca8a2b1dd54e157f4ea6ae17a2e9a53783cf0`
- Local wiki documentation update commit:
  - branch: `docs/update-last-24h-2026-04-09`
  - commit: `462ec03793574ffd940df7369754aae3550b952d`

## Impacted Features / Modules

- App initialization and server composition for project apps
- `moca build server` generated app imports
- `moca serve` / server startup app initialization
- Field encryption for `Password` document fields
- Encrypted backup create / restore flow
- Desk Vite plugin support for extension files under `apps/*/desk/**`

## Wiki Sections Touched

- App system concept docs
- App scaffolding and hook authoring guides
- Backup / restore operations
- Security operations
- CLI reference
- Configuration reference
- Field type reference
- Desk extension development notes

## Files Changed

- `wiki/Concepts-App-System.md`
- `wiki/Desk-Extensions.md`
- `wiki/Guide-Creating-Your-First-App.md`
- `wiki/Guide-Writing-Hooks.md`
- `wiki/Operations-Backup-and-Restore.md`
- `wiki/Operations-Security.md`
- `wiki/Reference-CLI-Commands.md`
- `wiki/Reference-Configuration.md`
- `wiki/Reference-Field-Types.md`

## New Docs Created

- None

## Navigation or Structure Changes

- None

## Areas Reviewed But Intentionally Not Documented

- `.github/workflows/stale.yml`
  - Reason: repository maintenance workflow only; no wiki-facing product or contributor behavior changed enough to warrant a page update
- `SECURITY_REVIEW.md`
  - Reason: run artifact, not long-lived source-of-truth documentation
- `docs/MS-22-security-hardening-oauth2-saml-oidc-encryption-notifications-plan.md`
  - Reason: planning artifact; documented only the implemented encryption behavior, not future milestone scope
- `docs/dx-test-session-report.md`
  - Reason: transient engineering report; underlying runtime behavior was documented in the wiki instead

## Skipped Items and Reasons

- Full rewrite of `Reference-Configuration.md`
  - Reason: current task was limited to last-24-hours changes; only the new backup encryption key resolution was updated
- New standalone page for encryption
  - Reason: existing `Operations-Security.md`, `Operations-Backup-and-Restore.md`, and `Reference-Field-Types.md` already covered the right audience/placement

## Uncertainties

- `moca backup verify` does not currently decrypt `.enc` backups before validation. The wiki documents encrypted-backup restore support and explicitly notes the verifier limitation instead of claiming full `.enc` verification support.
- Remote PR creation is currently blocked in this environment:
  - shell network access cannot reach GitHub
  - the GitHub connector can access `osama1998H/Moca` and `osama1998H/moca-desk`
  - the same connector returns `404` for `osama1998H/Moca.wiki`, so the wiki repository could not be pushed or opened as a PR from this run

## PR URL

- Not created; blocked because `osama1998H/Moca.wiki` was not accessible through the available GitHub connector and shell networking is restricted

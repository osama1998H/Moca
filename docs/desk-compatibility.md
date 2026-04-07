# @osama1998h/desk Compatibility Matrix

This document tracks version compatibility between the `@osama1998h/desk` npm package and the Moca server/CLI Go binaries.

## Version Compatibility

| @osama1998h/desk | moca CLI / moca-server | Status  | Notes                    |
|------------|------------------------|---------|--------------------------|
| 0.1.x      | 0.1.x                 | Current | Initial release. API v1. |

## Versioning Policy

- `@osama1998h/desk` versions are synchronized with the Go release tags. When a `v0.2.0` Go release is tagged, `@osama1998h/desk@0.2.0` is published automatically.
- Patch versions (0.1.1, 0.1.2) may be published independently for desk-only fixes that do not require a Go binary update.
- Major version bumps indicate breaking changes to the desk public API (exports from `@osama1998h/desk`).
- The `peerDependencies` in scaffolded project `package.json` files specify the compatible `@osama1998h/desk` range (e.g., `"@osama1998h/desk": "^0.1.0"`).

## How to Check Compatibility

1. Check your moca CLI version: `moca version`
2. Check your desk package version: `cat desk/package.json | grep '"@osama1998h/desk"'`
3. Ensure both are in the same row of the table above.

## Upgrade Guide

When upgrading:

1. Update Go binaries first (download from GitHub Releases).
2. Update desk: `cd desk && npm update @osama1998h/desk` (or `moca desk update`).
3. Rebuild: `moca build desk`.

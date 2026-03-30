// Package hooks implements the MOCA hook registry and extension system.
//
// Apps extend framework behavior by registering hooks at declared priority
// levels. The registry resolves execution order at startup, ensuring
// deterministic hook invocation across all installed apps.
//
// Key components:
//   - Registry: HookRegistry with priority-based ordering and dependency resolution
//   - DocEvents: document lifecycle event hooks (before_save, after_save, etc.)
//   - Middleware: API middleware hook system for request/response interception
package hooks

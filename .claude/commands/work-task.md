# Work on Milestone Task

You are a senior Go engineer implementing a task from a Moca milestone plan.

**Arguments:** `$ARGUMENTS`

Parse the arguments:
- **First argument ($0):** The milestone plan file path (e.g., `docs/milestones/MS-09-cli-project-init-site-and-app-commands-plan.md`)
- **Second argument ($1):** The task ID to implement (e.g., `MS-09-T1`)

If only one argument is provided, treat it as the task ID and search for the matching plan file in `docs/milestones/` by extracting the milestone prefix (e.g., `MS-09-T1` → find `docs/milestones/MS-09-*-plan.md`).

---

## Phase 1 — Understand the Task

1. **Read the milestone plan file** (`$0`) in full.
2. **Locate the specific task** (`$1`) and read every field: Description, Why, Dependencies, Inputs/References, Deliverable, Acceptance Criteria, Risks/Unknowns.
3. **Read all source references** listed in the task — follow every file path, section, and line range to the actual design document content. Do not skip any reference.
4. **Read `ROADMAP.md`** — find the milestone this task belongs to and understand its full scope, deliverables, and acceptance criteria.
5. **Read `CLAUDE.md`** for project conventions, build commands, and technology stack.

## Phase 2 — Gather Context from Codebase

1. **Check task dependencies:** If this task depends on other tasks in the same milestone, verify those are completed (marked as `Completed` in the plan file). If not, warn and ask whether to proceed.
2. **Read all relevant existing code** that this task builds upon or integrates with. Spawn multiple agents to read in parallel:
   - The packages this task modifies or extends
   - Related test files
   - Any imports or interfaces this task needs to implement
3. **Understand the current state:** What exists already? What's stubbed? What needs to be created from scratch?

## Phase 3 — Implement

Work carefully and methodically:

1. **Plan your implementation** before writing code — identify all files to create/modify.
2. **Implement the task** following:
   - The design doc specifications exactly (struct fields, function signatures, behaviors)
   - Existing project conventions (naming, error handling, package layout)
   - Go best practices (error wrapping, context propagation, interface design)
3. **Write tests** as specified in the task's deliverables and acceptance criteria.
4. **Handle edge cases and errors** as described in the design docs.
5. **Spawn multiple agents** for parallel work when implementing independent components.

## Phase 4 — Validate

Run all validation checks and capture their output:

```bash
# Build
go build ./...

# Vet
go vet ./...

# Lint
make lint

# Run all tests
make test

# Run specific tests for the packages you modified
go test -race -v ./pkg/[modified-package]/...
go test -race -v ./internal/[modified-package]/...
```

If any check fails:
1. Diagnose the root cause
2. Fix the issue
3. Re-run validation
4. Repeat until all checks pass

## Phase 5 — Completion Check

Before marking the task as done, verify **every single acceptance criterion** from the task:

1. Re-read the task's acceptance criteria from the plan file.
2. For each criterion, confirm it is met — cite the specific code, test, or output that proves it.
3. If any criterion is NOT met, go back and fix it.

## Phase 6 — Mark Task as Completed

After all validations pass and all acceptance criteria are met:

1. **Update the milestone plan file** (`$0`): Change the task's `**Status:**` from `Not Started` to `Completed`.
2. Print a completion summary:

```
## Task Completion Summary

- **Task:** $1 — [title]
- **Status:** ✅ Completed
- **Files created/modified:**
  - [list every file]
- **Tests added:**
  - [list test files and test function names]
- **Acceptance criteria:**
  - [criterion 1] — ✅ Met ([evidence])
  - [criterion 2] — ✅ Met ([evidence])
  - ...
- **Build:** ✅ Pass
- **Vet:** ✅ Pass
- **Lint:** ✅ Pass
- **Tests:** ✅ Pass ([X] passed, [Y] total)
- **Risks encountered:** [any issues hit during implementation]
- **Next task:** [the next task ID in the recommended execution order, if any]
```

---

## Rules

1. **Be precise, avoid assumptions.** When in doubt, read the design doc again.
2. **Keep traceability** to the plan file and design doc references — every major implementation decision should trace back to a spec.
3. **Do not skip tests.** If the task deliverables include tests, write them.
4. **Do not modify code outside the task's scope** unless absolutely necessary for the task to work. If you must, document why.
5. **Mark the task as Completed** in the plan file only after ALL validations pass.
6. **If blocked**, explain what's blocking you and what needs to happen before this task can be completed. Do NOT mark it as Completed.
7. **Spawn multiple agents** for parallel research, validation, or independent implementation work.

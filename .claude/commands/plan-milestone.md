# Plan Milestone

You are acting as a senior technical architect, technical product manager, and implementation planner.

The selected milestone ID is: **$ARGUMENTS**

---

## Context

The Moca project has a `ROADMAP.md` file containing multiple milestones. Each milestone includes a milestone ID, title, description/scope, deliverables, acceptance criteria, and references back to the original design documents with file paths, sections, and line ranges.

## Primary Objective

Take the selected milestone from `ROADMAP.md`, understand it fully in the context of the overall project vision and original design references, research anything necessary, and break it into 3 to 5 actionable implementation tasks. Produce a single markdown file as the deliverable.

## Workflow

Follow these steps in order. Spawn multiple agents to parallelize reading where possible.

### Step 1 — Read and Understand Context

1. Read `ROADMAP.md` fully enough to understand the project vision, milestone ordering, dependencies, and release groupings.
2. Locate the selected milestone **$ARGUMENTS** by its ID.
3. Read **all design document sections** referenced by that milestone — follow every `Design References` entry to the exact file, section, and line range. Actually read those lines using the Read tool.
4. Read `CLAUDE.md` for project conventions.
5. Read `docs/blocker-resolution-strategies.md` and `docs/moca-cross-doc-mismatch-report.md` for any items relevant to this milestone.

### Step 2 — Understand the Milestone in Context

Analyze:
- **Why it exists** — what part of the system vision it supports
- **What prerequisites it assumes** — which upstream milestones and what code they produced
- **What downstream work depends on it** — what later milestones need from this one
- **What the codebase looks like now** — read relevant existing source files under `pkg/`, `internal/`, `cmd/`, `apps/` to understand the current state and what to build upon

### Step 3 — Research (if needed)

If any referenced area is ambiguous or underdefined:
1. Investigate the codebase and design docs first.
2. Research the web **only** when needed for implementation-relevant clarification (libraries, patterns, protocols, best practices).

### Step 4 — Produce the Plan

Break the milestone into **3 to 5 tasks**. Each task must be:
- Implementation-oriented and realistically scoped
- Dependency-aware (both within the milestone and to external milestones)
- Traceable back to specific design doc sections
- Concrete enough that an engineer can start implementation from it

**Do not produce generic tasks.** Tasks must reflect the actual milestone intent and referenced design sections.

If the milestone is too large or internally inconsistent, say so clearly and propose the cleanest task split anyway.

---

## Task Requirements

For each task include:

- **Task ID:** (e.g., $ARGUMENTS-T1, $ARGUMENTS-T2, ...)
- **Title**
- **Status:** Not Started
- **Description:** What to implement, with specifics
- **Why this task exists:** How it contributes to the milestone goal
- **Dependencies:** Which other tasks in this milestone it depends on (if any)
- **Inputs / Source References:** Exact file paths, section headings, and line ranges from design docs
- **Expected Output / Deliverable:** What files/packages/tests will be produced
- **Acceptance Criteria:** Concrete, verifiable criteria (derived from the milestone's acceptance criteria in the roadmap)
- **Key Risks or Unknowns**

---

## Output File

Create exactly one Markdown file for the selected milestone.

**File path:** `docs/milestones/<milestone-id>-<milestone-name>-plan.md`

**Naming rules:**
- Convert title to lowercase
- Replace spaces with hyphens
- Remove special characters

**Examples:**
- `docs/milestones/MS-01-project-structure-configuration-plan.md`
- `docs/milestones/MS-09-cli-project-init-site-and-app-commands-plan.md`

---

## Required File Structure

```markdown
# <Milestone ID> — <Milestone Name> Plan

## Milestone Summary

- **ID:** $ARGUMENTS
- **Name:**
- **Roadmap Reference:** ROADMAP.md → $ARGUMENTS section
- **Goal:**
- **Why it matters:**
- **Position in roadmap:** Order #X of 30 milestones
- **Upstream dependencies:**
- **Downstream dependencies:**

## Vision Alignment

[Explain how this milestone supports the broader Moca vision and architecture. 2-3 paragraphs.]

## Source References

[List ALL roadmap and design-document references used:]

| File | Section | Lines | Relevance |
|------|---------|-------|-----------|
| docs/MOCA_SYSTEM_DESIGN.md | §X.Y Title | L-L | ... |
| docs/MOCA_CLI_SYSTEM_DESIGN.md | §X.Y Title | L-L | ... |
| ROADMAP.md | $ARGUMENTS | L-L | Milestone definition |

## Research Notes

[Summarize important implementation research findings. Only include web research if it was actually needed. If no web research was needed, explicitly say so.]

## Milestone Plan

### Task 1

- **Task ID:** $ARGUMENTS-T1
- **Title:**
- **Status:** Not Started
- **Description:**
- **Why this task exists:**
- **Dependencies:** None / $ARGUMENTS-TX
- **Inputs / References:**
- **Deliverable:**
- **Acceptance Criteria:**
- **Risks / Unknowns:**

### Task 2
...

(continue until 3 to 5 tasks total)

## Recommended Execution Order

1. $ARGUMENTS-T1 — [reason]
2. $ARGUMENTS-T2 — [reason]
3. ...

## Open Questions

- [Any unresolved questions that surfaced during planning]

## Out of Scope for This Milestone

- [Items explicitly excluded per the ROADMAP scope]
```

---

## Rules

1. **Stay focused on one milestone only.**
2. **Use the roadmap and design references as the source of truth.**
3. **Preserve traceability:** every task must map back to milestone references with exact file paths and line numbers.
4. **Keep the plan concrete** enough that an engineer can start implementation from it.
5. **Avoid bloated task lists:** produce only 3 to 5 tasks.
6. **Prefer tasks that represent meaningful engineering work packages**, not tiny subtasks.
7. **If the milestone contains a hidden blocker**, call it out explicitly.
8. **If a research spike is needed**, make it one of the 3 to 5 tasks.
9. **Spawn multiple agents** to read design docs and codebase in parallel.

---

## Final Output

After writing the plan file, print a short summary:

- **Milestone analyzed:** $ARGUMENTS — [title]
- **Total tasks created:** X
- **Biggest risk:** [one sentence]
- **Output file:** docs/milestones/[filename].md

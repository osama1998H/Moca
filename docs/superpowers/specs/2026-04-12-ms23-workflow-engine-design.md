# MS-23: Workflow Engine — Design Specification

**Date:** 2026-04-12
**Status:** Approved
**Dependencies:** MS-04 (Document Runtime), MS-14 (Permission Engine), MS-15 (Background Jobs/Events), MS-17 (React Desk)

## Overview

A metadata-driven workflow engine for Moca that provides state machines over documents with role-based transitions, parallel branch execution (AND-splits/joins), approval chains with quorum logic, SLA timers with escalation, and a Desk UI built entirely from shadcn components.

### Architecture: Engine + Hook Bridge

`pkg/workflow` is a standalone engine with its own interfaces, independently testable. A thin bridge layer registers hooks on the document lifecycle so the engine integrates with `DocManager` operations. Both API paths (dedicated endpoint and bundled save) converge on the same engine methods.

```
Dedicated API  ──>  WorkflowEngine.Transition()
                         ^
DocManager.Save() -> Hook Bridge -> WorkflowEngine.Transition()
```

### Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Workflow storage | Database DocType | CRUD via API, editable at runtime without redeployment |
| Expression evaluator | `expr-lang/expr` | Lightweight, sandboxed, Go-native, fits document field conditions |
| Approval records | Standalone `WorkflowAction` DocType | Easy to query "My Pending Approvals", clean separation |
| SLA timers | Hybrid (delayed job + cron sweep) | Precise timing via delayed jobs, cron sweep as safety net |
| Parallel states | Full AND-splits/joins | Document can be in multiple states simultaneously across branches |
| State tracking | Separate `WorkflowStateTracker` table | One row per active branch per document, queryable per-branch |
| API design | Dedicated endpoint + bundled save | Clean API for programmatic use, convenient UX for Desk |
| Desk UI | Progressive (minimal first) | Ship functional bar + timeline, architected for richer visualization later |
| UI components | shadcn only | No custom primitives, compose from Card, Badge, Button, Dialog, Sheet, Avatar, etc. |

---

## 1. Data Model

### 1.1 Workflow DocType

A `Workflow` document defines the state machine for a target DocType. Stored in the database, fully CRUD-able via the API.

```go
type Workflow struct {
    Name        string            // PK, e.g., "Sales Order Approval"
    DocType     string            // Target: "Sales Order"
    IsActive    bool              // Only one active workflow per DocType
    States      []WorkflowState   // Child table
    Transitions []Transition      // Child table
    SLARules    []SLARule         // Child table
}
```

### 1.2 WorkflowState (child table)

```go
type WorkflowState struct {
    Name        string // "Draft", "Pending Finance", "Pending Legal"
    Style       string // "Success", "Warning", "Danger", "Info"
    DocStatus   int    // 0=Draft, 1=Submitted, 2=Cancelled
    AllowEdit   string // Role allowed to edit in this state
    UpdateField string // Field to set on entry (e.g., "status")
    UpdateValue string // Value to set (e.g., "Pending")
    IsFork      bool   // AND-split: entering this state forks into branches
    JoinTarget  string // AND-join: branch converges at this state
    BranchName  string // Label for parallel branch (e.g., "Finance", "Legal")
}
```

### 1.3 Transition (child table)

```go
type Transition struct {
    From           string   // Source state
    To             string   // Target state
    Action         string   // Button label: "Approve", "Reject", "Submit"
    AllowedRoles   []string // Roles that can trigger this (Link to Role)
    Condition      string   // expr expression: "doc.grand_total > 1000"
    AutoAction     string   // Registered handler name to run on transition
    RequireComment bool     // Mandatory comment on this transition
    QuorumCount    int      // Number of approvals needed (0 = single approval)
    QuorumRoles    []string // Pool of roles that can approve (defaults to AllowedRoles)
}
```

### 1.4 SLARule (child table)

```go
type SLARule struct {
    State            string        // Tracked state: "Pending Approval"
    MaxDuration      time.Duration // e.g., 24h
    EscalationRole   string        // Notify this role on breach
    EscalationAction string        // Handler to trigger
}
```

### 1.5 WorkflowStateTracker (standalone table)

One row per active branch per document. Enables parallel state tracking.

```go
type WorkflowStateTracker struct {
    Name           string     // PK (auto-generated)
    ReferenceType  string     // "Sales Order"
    ReferenceName  string     // "SO-0001"
    WorkflowName   string     // "Sales Order Approval"
    BranchName     string     // "Finance", "Legal", or "" for linear
    CurrentState   string     // "Pending Finance Approval"
    EnteredAt      time.Time  // When this state was entered
    IsActive       bool       // False when branch completes
    SLADeadline    *time.Time // Computed: EnteredAt + SLARule.MaxDuration
}
```

### 1.6 WorkflowAction (standalone DocType)

Records every workflow action for audit trail and approval tracking.

```go
type WorkflowAction struct {
    Name           string    // PK
    ReferenceType  string    // "Sales Order"
    ReferenceName  string    // "SO-0001"
    WorkflowName   string    // "Sales Order Approval"
    Action         string    // "Approve", "Reject", "Submit", "Delegate"
    FromState      string    // "Pending Finance"
    ToState        string    // "Finance Approved"
    BranchName     string    // "Finance" or ""
    User           string    // Who performed the action
    Comment        string    // Optional comment
    Timestamp      time.Time
}
```

### 1.7 Parallel State Flow

```
Draft -> [Submit] -> Fork State (IsFork=true)
                       |-- Branch "Finance": Pending Finance -> [Approve] -> Finance Approved
                       |-- Branch "Legal":   Pending Legal   -> [Approve] -> Legal Approved
                                                                               |
                                                            Join State (both branches done)
                                                                               |
                                                                           Approved
```

When a state has `IsFork=true`, entering it creates one `WorkflowStateTracker` row per branch. Branches are enumerated by finding all `WorkflowState` entries whose `JoinTarget` matches a common join state — each such state is one branch (identified by its `BranchName`). Each branch transitions independently. When all branches for a join target are inactive (completed), the join target state activates automatically.

---

## 2. Engine Architecture

### 2.1 Package Structure

```
pkg/workflow/
  engine.go          # WorkflowEngine - core state machine logic
  approval.go        # ApprovalManager - quorum, delegation
  sla.go             # SLAManager - timer scheduling, breach detection
  evaluator.go       # ConditionEvaluator - expr-lang/expr wrapper
  bridge.go          # Hook bridge - connects engine to document lifecycle
  registry.go        # WorkflowRegistry - caches active workflows per DocType
  errors.go          # Typed errors (ErrTransitionBlocked, ErrNoPermission, etc.)
  events.go          # Workflow event construction and publishing
  doc.go             # Package documentation
```

### 2.2 Core Types

```go
type WorkflowEngine struct {
    registry    *WorkflowRegistry
    approvals   *ApprovalManager
    sla         *SLAManager
    evaluator   *ConditionEvaluator
    events      *events.Emitter
    hooks       *hooks.HookRegistry
    queue       *queue.Producer
}

func (e *WorkflowEngine) Transition(ctx *document.DocContext, doc document.Document, action string, opts TransitionOpts) error
func (e *WorkflowEngine) CanTransition(ctx *document.DocContext, doc document.Document, action string) (bool, error)
func (e *WorkflowEngine) GetState(ctx *document.DocContext, doc document.Document) (*WorkflowStatus, error)
func (e *WorkflowEngine) GetAvailableActions(ctx *document.DocContext, doc document.Document) ([]AvailableAction, error)
```

```go
type TransitionOpts struct {
    Comment    string // Optional comment (required if RequireComment=true)
    BranchName string // Target branch for parallel workflows ("" for linear)
}

type WorkflowStatus struct {
    WorkflowName string
    Branches     []BranchStatus
    IsParallel   bool
}

type BranchStatus struct {
    BranchName   string
    CurrentState string
    Style        string
    IsActive     bool
    EnteredAt    time.Time
    SLADeadline  *time.Time
}

type AvailableAction struct {
    Action         string
    ToState        string
    BranchName     string
    RequireComment bool
    Style          string
}
```

### 2.3 WorkflowRegistry

Caches active workflows per DocType. Loaded from database on first access, invalidated on workflow document update.

```go
type WorkflowRegistry struct {
    cache map[string]*Workflow // key: "{site}:{doctype}"
    mu    sync.RWMutex
}

func (r *WorkflowRegistry) Get(ctx context.Context, site, doctype string) (*Workflow, error)
func (r *WorkflowRegistry) Invalidate(site, doctype string)
```

### 2.4 Transition Flow

```
Transition(ctx, doc, "Approve", opts)
  |
  |-- 1. Registry.Get() -> load workflow definition
  |-- 2. GetState() -> read WorkflowStateTracker rows
  |-- 3. Find matching transition (from current state + action + branch)
  |-- 4. Validate role: user.Roles intersect transition.AllowedRoles != empty
  |-- 5. Evaluate condition: evaluator.Eval(transition.Condition, doc)
  |-- 6. If RequireComment && opts.Comment == "" -> error
  |-- 7. Begin transaction (or use ctx.TX if bundled with save)
  |      |-- Update WorkflowStateTracker (current state -> new state)
  |      |-- If new state IsFork -> create branch tracker rows
  |      |-- If branch completed + all branches done -> activate join target
  |      |-- Update doc.workflow_state (primary state for simple queries)
  |      |-- Update doc.docstatus if state defines DocStatus change
  |      |-- Apply UpdateField/UpdateValue if defined
  |      |-- Insert WorkflowAction record
  |      |-- Schedule/cancel SLA jobs as needed
  |-- 8. Execute AutoAction handler if defined
  |-- 9. Publish workflow event to TopicWorkflowTransitions
  |-- 10. Return
```

### 2.5 Hook Bridge

Thin layer connecting the standalone engine to the document lifecycle.

```go
type WorkflowBridge struct {
    engine *WorkflowEngine
}

func (b *WorkflowBridge) Register(hookRegistry *hooks.HookRegistry)
```

The bridge registers hooks for:
- `BeforeSave`: If `workflow_action` flag is present in `DocContext.Flags`, validates the transition
- `AfterSave`: If `workflow_action` flag is present, executes the transition
- `BeforeSubmit`: Validates workflow allows submission from current state
- `BeforeCancel`: Validates workflow allows cancellation from current state

The API handler sets `workflow_action` and `workflow_comment` in `DocContext.Flags` when the request includes these fields. The bridge reads them during the save lifecycle.

---

## 3. Approval Manager

### 3.1 Quorum Logic

Approvals are tracked through `WorkflowAction` records. No separate approval table.

```go
type ApprovalManager struct {
    engine *WorkflowEngine
}

func (a *ApprovalManager) CheckQuorum(ctx *document.DocContext,
    doctype, docname, fromState, action, branchName string,
    transition *Transition) (*QuorumResult, error)

type QuorumResult struct {
    Required    int
    Received    int
    IsMet       bool
    ApprovedBy  []string
    PendingFrom []string
}
```

### 3.2 Quorum Configuration

Quorum is defined on the `Transition`:
- `QuorumCount`: Number of approvals needed (0 = single approval, immediate transition)
- `QuorumRoles`: Pool of roles that can approve (defaults to `AllowedRoles`)

### 3.3 Quorum Flow

```
User clicks "Approve" on doc in "Pending Approval"
  |
  |-- 1. Engine finds transition: "Pending Approval" -> "Approved" (QuorumCount=2)
  |-- 2. Validate: user has required role
  |-- 3. Validate: user hasn't already approved this transition
  |-- 4. Insert WorkflowAction record (action="Approve", from="Pending Approval")
  |-- 5. ApprovalManager.CheckQuorum()
  |      |-- Count WorkflowAction records matching (docname, fromState, action)
  |      |-- If count >= QuorumCount -> quorum met
  |      |-- If count < QuorumCount -> quorum not met
  |-- 6a. Quorum MET -> complete transition to "Approved"
  |-- 6b. Quorum NOT MET -> stay in "Pending Approval", return success
                            (action recorded, waiting for more approvals)
```

### 3.4 Delegation

```go
func (a *ApprovalManager) Delegate(ctx *document.DocContext,
    doctype, docname string, fromUser, toUser string) error
```

Inserts a `WorkflowAction` with `Action: "Delegate"`. The delegate can then approve as if they were the original approver.

---

## 4. SLA Manager

### 4.1 Hybrid Timer Implementation

**Per-document delayed job (precision):** When a document enters an SLA-tracked state, compute the deadline and enqueue a delayed job.

```go
type SLAManager struct {
    queue     *queue.Producer
    scheduler *queue.Scheduler
}

func (s *SLAManager) StartTimer(ctx context.Context, rule *SLARule,
    tracker *WorkflowStateTracker) error
func (s *SLAManager) CancelTimer(ctx context.Context,
    doctype, docname, branchName string) error
```

On state entry:
```go
deadline := time.Now().Add(rule.MaxDuration)
tracker.SLADeadline = &deadline

job := queue.Job{
    Type:     "workflow.sla.check",
    Site:     ctx.Site.Name,
    Payload:  map[string]any{
        "doctype":  doctype,
        "docname":  docname,
        "branch":   branchName,
        "state":    rule.State,
        "workflow": workflowName,
        "deadline": deadline,
    },
    RunAfter: &deadline,
    Queue:    queue.QueueCritical,
}
```

When the job fires, the handler checks if the document is still in that state. If yes, escalate. If the document already transitioned, no-op.

**Cron sweep (safety net):** Runs every 5 minutes scanning for breached SLAs.

```go
CronEntry{
    Name:     "workflow.sla.sweep",
    CronExpr: "*/5 * * * *",
    JobType:  "workflow.sla.sweep",
    Queue:    queue.QueueDefault,
}
```

The sweep queries `WorkflowStateTracker WHERE is_active=true AND sla_deadline IS NOT NULL AND sla_deadline < NOW()` and triggers escalation for any breached entries not yet escalated.

### 4.2 Escalation Actions

On SLA breach:
1. Publish `workflow.sla.breached` event to `TopicWorkflowTransitions`
2. If `EscalationRole` is set, create notification for all users with that role
3. If `EscalationAction` is set, execute the registered handler
4. Mark the tracker as escalated to prevent duplicate escalations

### 4.3 Timer Cancellation

When a document transitions out of an SLA-tracked state, `CancelTimer` is called. Since Redis Streams jobs can't be directly cancelled, the handler checks if the document is still in the tracked state before escalating (effectively a no-op for cancelled timers).

---

## 5. Expression Evaluator

### 5.1 Implementation

Wrapper around `expr-lang/expr` with compiled expression caching.

```go
type ConditionEvaluator struct {
    cache map[string]*vm.Program
    mu    sync.RWMutex
}

func (e *ConditionEvaluator) Eval(condition string,
    doc document.Document, ctx *document.DocContext) (bool, error)
```

### 5.2 Expression Environment

```go
env := map[string]any{
    "doc":      doc.AsMap(),
    "user":     ctx.User.Email,
    "roles":    ctx.User.Roles,
    "now":      time.Now(),
    "has_role": func(role string) bool { ... },
}
```

### 5.3 Example Expressions

```
doc.grand_total > 1000
doc.status == "Active" && doc.grand_total > 0
has_role("Finance Manager")
doc.department == "Engineering" || doc.priority == "Urgent"
```

### 5.4 Safety

- `expr-lang/expr` is sandboxed: no access to Go runtime, filesystem, or network
- Expressions compiled once and cached
- Compilation errors returned as typed `ErrInvalidCondition`
- Evaluation timeout: 100ms (configurable)

---

## 6. API Layer

### 6.1 Dedicated Workflow Endpoint

```
POST /api/v1/workflow/{doctype}/{name}/transition
```

Request:
```json
{
    "action": "Approve",
    "comment": "Looks good, budget is within limits",
    "branch": "Finance"
}
```

Response:
```json
{
    "status": "ok",
    "state": {
        "workflow_name": "Sales Order Approval",
        "is_parallel": true,
        "branches": [
            {"branch": "Finance", "state": "Finance Approved", "is_active": false},
            {"branch": "Legal", "state": "Pending Legal", "is_active": true}
        ]
    }
}
```

### 6.2 Bundled with Save

```
PUT /api/v1/resource/{doctype}/{name}
```

```json
{
    "grand_total": 50000,
    "workflow_action": "Submit",
    "workflow_comment": "Ready for review"
}
```

The API handler detects `workflow_action`, sets it in `DocContext.Flags`, and the hook bridge picks it up during save. Response includes both the saved document and the new workflow state.

### 6.3 Query Endpoints

```
GET /api/v1/workflow/{doctype}/{name}/state
```
Returns current `WorkflowStatus` (branches, available actions for the user).

```
GET /api/v1/workflow/{doctype}/{name}/history
```
Returns `WorkflowAction` records (timeline data).

```
GET /api/v1/workflow/pending?user=jane@acme.com
```
Returns all documents with pending actions for this user (powers "My Pending Approvals").

### 6.4 Error Responses

```json
{
    "error": "transition_blocked",
    "message": "Role 'Finance Approver' required for action 'Approve'",
    "details": {
        "action": "Approve",
        "required_roles": ["Finance Approver"],
        "user_roles": ["Sales User"]
    }
}
```

Error codes: `transition_blocked`, `condition_failed`, `quorum_pending`, `comment_required`, `invalid_action`, `no_active_workflow`, `sla_breached`.

---

## 7. Desk UI

All UI composed exclusively from shadcn components. No custom primitives.

### 7.1 Component-to-shadcn Mapping

| UI Need | shadcn Component | Status |
|---------|-----------------|--------|
| Workflow bar container | `Card` + `CardHeader` + `CardContent` | Installed |
| State indicator | `Badge` (variant per style) | Installed |
| Action buttons | `Button` (variant per action type) | Installed |
| Transition confirmation | `AlertDialog` | To install |
| Comment input | `Dialog` + `Textarea` + `Field` | Installed |
| Timeline entries | `Avatar` + `Separator` + `Card` | Installed |
| Timeline panel | `Sheet` (side panel) | Installed |
| SLA countdown | `Badge` + `Tooltip` | Installed |
| SLA visual indicator | `Progress` | To install |
| Branch sections | `Collapsible` | Installed |
| Loading states | `Skeleton` + `Spinner` | Installed |
| Scrollable timeline | `ScrollArea` | Installed |

Components to install: `AlertDialog`, `Progress`.

### 7.2 File Structure

```
desk/src/components/workflow/
  WorkflowBar.tsx          # Card + Badge + Button composition
  WorkflowBranch.tsx       # Single branch row (Collapsible)
  WorkflowActionButton.tsx # Button + Dialog/AlertDialog logic
  WorkflowTimeline.tsx     # Sheet + ScrollArea + Avatar entries
  useWorkflow.ts           # React Query hooks for workflow API
```

### 7.3 WorkflowBar

Sits between the document title and form body in FormView. Composed from `Card` + `Badge` + `Button`.

**Linear workflow:**
```
+-----------------------------------------------------------+
|  [Badge: Pending Approval]          [Reject] [Approve]    |
|  [Badge outline: SLA: 18h 32m]                            |
+-----------------------------------------------------------+
```

**Parallel workflow:** Uses `Collapsible` inside `CardContent` with `Separator` between branches. Each branch shows its own state `Badge`, `Progress` bar for SLA, and action `Button` set.

### 7.4 Comment Dialog

When `RequireComment` is true: `Dialog` + `DialogContent` + `FieldGroup` + `Field` + `FieldLabel` + `Textarea` + `DialogFooter`.

### 7.5 Transition Confirmation

For destructive transitions (Reject, Cancel): `AlertDialog` + `AlertDialogContent` + `AlertDialogTitle` + `AlertDialogDescription` + `AlertDialogFooter` + `AlertDialogCancel` + `AlertDialogAction`.

### 7.6 WorkflowTimeline

Opens as `Sheet` side panel. `ScrollArea` containing entries composed from `Avatar` + `AvatarFallback` + `Badge` (action/branch) + `Separator` between entries. Each entry shows user, action, branch (if parallel), comment, and relative timestamp.

### 7.7 FormView Integration

```tsx
{meta.has_workflow && <WorkflowBar doctype={doctype} name={name} />}
```

`useWorkflow.ts` hook follows the existing DocProvider pattern with React Query, cache invalidation on mutation.

---

## 8. Event Publishing

### 8.1 WorkflowEvent Envelope

```go
type WorkflowEvent struct {
    EventID      string    `json:"event_id"`
    EventType    string    `json:"event_type"`
    Timestamp    time.Time `json:"timestamp"`
    Source       string    `json:"source"`
    Site         string    `json:"site"`
    DocType      string    `json:"doc_type"`
    DocName      string    `json:"doc_name"`
    WorkflowName string    `json:"workflow_name"`
    Action       string    `json:"action"`
    FromState    string    `json:"from_state"`
    ToState      string    `json:"to_state"`
    BranchName   string    `json:"branch_name,omitempty"`
    User         string    `json:"user"`
    Comment      string    `json:"comment,omitempty"`
    RequestID    string    `json:"request_id"`
}
```

### 8.2 Event Types

| Event Type | When |
|------------|------|
| `workflow.transition` | Any state transition |
| `workflow.fork` | Document enters parallel branches |
| `workflow.join` | All parallel branches converge |
| `workflow.quorum.vote` | Approval recorded but quorum not yet met |
| `workflow.quorum.met` | Quorum threshold reached |
| `workflow.sla.started` | SLA timer begins for a state |
| `workflow.sla.breached` | SLA deadline exceeded |
| `workflow.sla.cancelled` | SLA timer cancelled (state left before deadline) |
| `workflow.delegated` | Approval delegated to another user |

### 8.3 Publishing Pattern

Uses the existing `Emitter.Emit()` pattern. Non-blocking, routed through whichever backend is configured (Kafka primary, Redis pub/sub fallback).

```go
ctx.EventBus.Emit(events.TopicWorkflowTransitions, evt)
```

---

## 9. Acceptance Criteria

From ROADMAP.md, validated against this design:

1. Document in "Draft" -> "Approve" by Approver role -> "Approved" -- via `WorkflowEngine.Transition()` with role check
2. Transition blocked without required role -- `AllowedRoles` validation in step 4 of transition flow
3. Transition blocked if condition expression fails -- `ConditionEvaluator.Eval()` in step 5
4. SLA: "Pending Approval" > 24h triggers escalation notification -- `SLAManager` hybrid timer + escalation handler
5. Quorum: 3 approvers, 2 of 3 needed -- `ApprovalManager.CheckQuorum()` with `QuorumCount=2`
6. Transitions published to event stream -- `WorkflowEvent` to `TopicWorkflowTransitions`

Additional criteria from this design:

7. Parallel branches execute independently and converge at join states
8. Workflow definitions are CRUD-able via API at runtime
9. "My Pending Approvals" endpoint returns all documents awaiting user action
10. Desk workflow bar renders state + actions using shadcn components only
11. Desk workflow timeline shows full transition history with user, action, comment

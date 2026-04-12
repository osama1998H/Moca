# MS-23: Workflow Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a metadata-driven workflow engine with parallel state machines, approval chains, SLA timers, and Desk UI.

**Architecture:** Standalone `pkg/workflow` engine with a thin hook bridge to integrate with document lifecycle. Dedicated + bundled-save API endpoints. React UI composed entirely from shadcn components.

**Tech Stack:** Go 1.26+ (expr-lang/expr for conditions), React 19 + TypeScript + Tailwind + shadcn/ui + React Query

**Design Spec:** `docs/superpowers/specs/2026-04-12-ms23-workflow-engine-design.md`

---

## File Map

### Backend — New Files

| File | Responsibility |
|------|---------------|
| `pkg/workflow/errors.go` | Typed error sentinel values |
| `pkg/workflow/evaluator.go` | expr-lang/expr wrapper with compiled cache |
| `pkg/workflow/evaluator_test.go` | Tests for expression evaluation |
| `pkg/workflow/registry.go` | In-memory workflow cache per site:doctype |
| `pkg/workflow/registry_test.go` | Tests for registry cache/invalidation |
| `pkg/workflow/engine.go` | Core state machine: Transition, GetState, GetAvailableActions |
| `pkg/workflow/engine_test.go` | Tests for linear + parallel transitions, role checks, conditions |
| `pkg/workflow/approval.go` | Quorum counting, delegation |
| `pkg/workflow/approval_test.go` | Tests for quorum logic |
| `pkg/workflow/sla.go` | Timer scheduling, breach detection, cron sweep |
| `pkg/workflow/sla_test.go` | Tests for SLA timer lifecycle |
| `pkg/workflow/events.go` | WorkflowEvent construction + publishing helpers |
| `pkg/workflow/events_test.go` | Tests for event construction |
| `pkg/workflow/bridge.go` | Hook bridge connecting engine to DocManager lifecycle |
| `pkg/workflow/bridge_test.go` | Tests for hook bridge |
| `pkg/api/workflow_handler.go` | HTTP handler for workflow endpoints |
| `pkg/api/workflow_handler_test.go` | Tests for workflow API |

### Backend — Modified Files

| File | Change |
|------|--------|
| `pkg/meta/stubs.go` | Add parallel fields to WorkflowState, quorum fields to Transition |
| `go.mod` | Add `github.com/expr-lang/expr` dependency |
| `internal/serve/server.go` | Register workflow handler routes |

### Frontend — New Files

| File | Responsibility |
|------|---------------|
| `desk/src/components/workflow/useWorkflow.ts` | React Query hooks for workflow API |
| `desk/src/components/workflow/WorkflowBar.tsx` | Main workflow status bar (Card + Badge + Button) |
| `desk/src/components/workflow/WorkflowBranch.tsx` | Single parallel branch row |
| `desk/src/components/workflow/WorkflowActionButton.tsx` | Action button with comment Dialog / AlertDialog |
| `desk/src/components/workflow/WorkflowTimeline.tsx` | Transition history Sheet |

### Frontend — Modified Files

| File | Change |
|------|--------|
| `desk/src/pages/FormView.tsx` | Import and render WorkflowBar |
| `desk/src/api/types.ts` | Add workflow-related TypeScript types |

---

## Task 1: Extend MetaType Structs for Parallel Workflows

**Files:**
- Modify: `pkg/meta/stubs.go`

- [ ] **Step 1: Write test for new struct fields**

Create `pkg/meta/stubs_test.go`:

```go
package meta

import (
	"encoding/json"
	"testing"
	"time"
)

func TestWorkflowState_ParallelFields(t *testing.T) {
	raw := `{
		"name": "Pending Finance",
		"style": "Warning",
		"doc_status": 0,
		"allow_edit": "Finance Manager",
		"is_fork": true,
		"join_target": "Approved",
		"branch_name": "Finance"
	}`
	var ws WorkflowState
	if err := json.Unmarshal([]byte(raw), &ws); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !ws.IsFork {
		t.Error("expected IsFork=true")
	}
	if ws.JoinTarget != "Approved" {
		t.Errorf("JoinTarget = %q, want %q", ws.JoinTarget, "Approved")
	}
	if ws.BranchName != "Finance" {
		t.Errorf("BranchName = %q, want %q", ws.BranchName, "Finance")
	}
}

func TestTransition_QuorumFields(t *testing.T) {
	raw := `{
		"from": "Pending Approval",
		"to": "Approved",
		"action": "Approve",
		"allowed_roles": ["Approver"],
		"quorum_count": 2,
		"quorum_roles": ["Finance Approver", "Legal Approver"]
	}`
	var tr Transition
	if err := json.Unmarshal([]byte(raw), &tr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tr.QuorumCount != 2 {
		t.Errorf("QuorumCount = %d, want 2", tr.QuorumCount)
	}
	if len(tr.QuorumRoles) != 2 {
		t.Errorf("QuorumRoles len = %d, want 2", len(tr.QuorumRoles))
	}
}

func TestSLARule_JSONRoundTrip(t *testing.T) {
	rule := SLARule{
		State:            "Pending Approval",
		MaxDuration:      24 * time.Hour,
		EscalationRole:   "Manager",
		EscalationAction: "escalate_approval",
	}
	data, err := json.Marshal(rule)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded SLARule
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.MaxDuration != 24*time.Hour {
		t.Errorf("MaxDuration = %v, want 24h", decoded.MaxDuration)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestWorkflowState_ParallelFields ./pkg/meta/...`
Expected: FAIL — `IsFork`, `JoinTarget`, `BranchName` fields don't exist on WorkflowState.

- [ ] **Step 3: Add parallel fields to WorkflowState and quorum fields to Transition**

In `pkg/meta/stubs.go`, replace the existing structs:

```go
// WorkflowState represents a single state in a workflow state machine.
type WorkflowState struct {
	Name        string `json:"name"`
	Style       string `json:"style"`
	AllowEdit   string `json:"allow_edit"`
	UpdateField string `json:"update_field"`
	UpdateValue string `json:"update_value"`
	DocStatus   int    `json:"doc_status"`
	IsFork      bool   `json:"is_fork,omitempty"`      // AND-split: entering forks into branches
	JoinTarget  string `json:"join_target,omitempty"`   // AND-join: branch converges at this state
	BranchName  string `json:"branch_name,omitempty"`   // Label for this parallel branch
}

// Transition represents a directed edge between two workflow states.
type Transition struct {
	From           string   `json:"from"`
	To             string   `json:"to"`
	Action         string   `json:"action"`
	Condition      string   `json:"condition"`
	AutoAction     string   `json:"auto_action"`
	AllowedRoles   []string `json:"allowed_roles"`
	RequireComment bool     `json:"require_comment"`
	QuorumCount    int      `json:"quorum_count,omitempty"`  // Approvals needed (0 = immediate)
	QuorumRoles    []string `json:"quorum_roles,omitempty"`  // Pool of approver roles
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run "TestWorkflowState_ParallelFields|TestTransition_QuorumFields|TestSLARule_JSONRoundTrip" ./pkg/meta/...`
Expected: PASS

- [ ] **Step 5: Run full meta test suite to check for regressions**

Run: `go test -race ./pkg/meta/...`
Expected: All tests PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/meta/stubs.go pkg/meta/stubs_test.go
git commit -m "feat(workflow): add parallel state and quorum fields to MetaType structs"
```

---

## Task 2: Add expr-lang/expr Dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/expr-lang/expr@latest`

- [ ] **Step 2: Tidy modules**

Run: `go mod tidy`

- [ ] **Step 3: Verify the dependency is present**

Run: `grep expr-lang go.mod`
Expected: Line containing `github.com/expr-lang/expr`

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add expr-lang/expr for workflow condition evaluation"
```

---

## Task 3: Workflow Errors

**Files:**
- Create: `pkg/workflow/errors.go`

- [ ] **Step 1: Write the errors file**

```go
package workflow

import "errors"

var (
	// ErrNoActiveWorkflow is returned when a doctype has no active workflow.
	ErrNoActiveWorkflow = errors.New("workflow: no active workflow for this doctype")

	// ErrTransitionBlocked is returned when a transition is not valid from the current state.
	ErrTransitionBlocked = errors.New("workflow: transition blocked")

	// ErrNoPermission is returned when the user lacks the required role.
	ErrNoPermission = errors.New("workflow: user does not have required role")

	// ErrConditionFailed is returned when a transition condition evaluates to false.
	ErrConditionFailed = errors.New("workflow: transition condition not met")

	// ErrCommentRequired is returned when a transition requires a comment but none was provided.
	ErrCommentRequired = errors.New("workflow: comment required for this transition")

	// ErrInvalidAction is returned when the action does not match any transition from the current state.
	ErrInvalidAction = errors.New("workflow: invalid action for current state")

	// ErrQuorumPending is returned when an approval was recorded but quorum is not yet met.
	ErrQuorumPending = errors.New("workflow: approval recorded, quorum pending")

	// ErrAlreadyApproved is returned when a user tries to approve a transition they already approved.
	ErrAlreadyApproved = errors.New("workflow: user has already approved this transition")

	// ErrInvalidCondition is returned when an expression fails to compile.
	ErrInvalidCondition = errors.New("workflow: invalid condition expression")

	// ErrBranchNotFound is returned when a specified branch does not exist.
	ErrBranchNotFound = errors.New("workflow: branch not found")

	// ErrSLABreached is returned when a document has exceeded its SLA deadline.
	ErrSLABreached = errors.New("workflow: SLA deadline exceeded")
)
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./pkg/workflow/...`
Expected: Success (no errors)

- [ ] **Step 3: Commit**

```bash
git add pkg/workflow/errors.go
git commit -m "feat(workflow): add typed error sentinels"
```

---

## Task 4: Condition Evaluator

**Files:**
- Create: `pkg/workflow/evaluator.go`
- Create: `pkg/workflow/evaluator_test.go`

- [ ] **Step 1: Write tests**

```go
package workflow

import (
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
)

func newTestDoc(fields map[string]any) document.Document {
	mt := &meta.MetaType{Name: "Test"}
	doc := document.NewDynamicDoc(mt, true)
	for k, v := range fields {
		_ = doc.Set(k, v)
	}
	return doc
}

func newTestDocCtx(email string, roles []string) *document.DocContext {
	ctx := document.NewDocContext(nil, nil, &auth.User{
		Email: email,
		Roles: roles,
	})
	return ctx
}

func TestConditionEvaluator_SimpleComparison(t *testing.T) {
	eval := NewConditionEvaluator()
	doc := newTestDoc(map[string]any{"grand_total": 5000})
	ctx := newTestDocCtx("test@example.com", []string{"User"})

	result, err := eval.Eval("doc.grand_total > 1000", doc, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected true for grand_total(5000) > 1000")
	}
}

func TestConditionEvaluator_FalseCondition(t *testing.T) {
	eval := NewConditionEvaluator()
	doc := newTestDoc(map[string]any{"grand_total": 500})
	ctx := newTestDocCtx("test@example.com", []string{"User"})

	result, err := eval.Eval("doc.grand_total > 1000", doc, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result {
		t.Error("expected false for grand_total(500) > 1000")
	}
}

func TestConditionEvaluator_HasRole(t *testing.T) {
	eval := NewConditionEvaluator()
	doc := newTestDoc(nil)
	ctx := newTestDocCtx("test@example.com", []string{"Finance Manager", "User"})

	result, err := eval.Eval(`has_role("Finance Manager")`, doc, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected true, user has Finance Manager role")
	}
}

func TestConditionEvaluator_EmptyCondition(t *testing.T) {
	eval := NewConditionEvaluator()
	doc := newTestDoc(nil)
	ctx := newTestDocCtx("test@example.com", nil)

	result, err := eval.Eval("", doc, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("empty condition should return true")
	}
}

func TestConditionEvaluator_InvalidExpression(t *testing.T) {
	eval := NewConditionEvaluator()
	doc := newTestDoc(nil)
	ctx := newTestDocCtx("test@example.com", nil)

	_, err := eval.Eval("invalid %%% syntax", doc, ctx)
	if err == nil {
		t.Error("expected error for invalid expression")
	}
}

func TestConditionEvaluator_CachesCompiled(t *testing.T) {
	eval := NewConditionEvaluator()
	doc := newTestDoc(map[string]any{"x": 1})
	ctx := newTestDocCtx("test@example.com", nil)

	// Eval twice — second call should use cached program
	_, _ = eval.Eval("doc.x == 1", doc, ctx)
	eval.mu.RLock()
	cached := len(eval.cache)
	eval.mu.RUnlock()
	if cached != 1 {
		t.Errorf("cache size = %d, want 1", cached)
	}

	result, err := eval.Eval("doc.x == 1", doc, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected true")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -race -run TestConditionEvaluator ./pkg/workflow/...`
Expected: FAIL — `NewConditionEvaluator` doesn't exist.

- [ ] **Step 3: Implement the evaluator**

```go
package workflow

import (
	"fmt"
	"sync"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"github.com/osama1998H/moca/pkg/document"
)

// ConditionEvaluator compiles and evaluates expr-lang expressions against
// document state. Compiled programs are cached for reuse.
type ConditionEvaluator struct {
	cache map[string]*vm.Program
	mu    sync.RWMutex
}

// NewConditionEvaluator creates a ConditionEvaluator with an empty cache.
func NewConditionEvaluator() *ConditionEvaluator {
	return &ConditionEvaluator{
		cache: make(map[string]*vm.Program),
	}
}

// Eval evaluates a condition expression against a document and context.
// An empty condition always returns true. Results are boolean.
func (e *ConditionEvaluator) Eval(condition string, doc document.Document, ctx *document.DocContext) (bool, error) {
	if condition == "" {
		return true, nil
	}

	program, err := e.getOrCompile(condition)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrInvalidCondition, err)
	}

	env := e.buildEnv(doc, ctx)
	output, err := expr.Run(program, env)
	if err != nil {
		return false, fmt.Errorf("workflow: condition evaluation failed: %w", err)
	}

	result, ok := output.(bool)
	if !ok {
		return false, fmt.Errorf("workflow: condition must return bool, got %T", output)
	}
	return result, nil
}

func (e *ConditionEvaluator) getOrCompile(condition string) (*vm.Program, error) {
	e.mu.RLock()
	if prog, ok := e.cache[condition]; ok {
		e.mu.RUnlock()
		return prog, nil
	}
	e.mu.RUnlock()

	prog, err := expr.Compile(condition, expr.AsBool())
	if err != nil {
		return nil, err
	}

	e.mu.Lock()
	e.cache[condition] = prog
	e.mu.Unlock()
	return prog, nil
}

func (e *ConditionEvaluator) buildEnv(doc document.Document, ctx *document.DocContext) map[string]any {
	var email string
	var roles []string
	if ctx != nil && ctx.User != nil {
		email = ctx.User.Email
		roles = ctx.User.Roles
	}

	return map[string]any{
		"doc":   doc.AsMap(),
		"user":  email,
		"roles": roles,
		"now":   time.Now(),
		"has_role": func(role string) bool {
			for _, r := range roles {
				if r == role {
					return true
				}
			}
			return false
		},
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run TestConditionEvaluator ./pkg/workflow/...`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/workflow/evaluator.go pkg/workflow/evaluator_test.go
git commit -m "feat(workflow): implement expr-lang condition evaluator with caching"
```

---

## Task 5: Workflow Registry

**Files:**
- Create: `pkg/workflow/registry.go`
- Create: `pkg/workflow/registry_test.go`

- [ ] **Step 1: Write tests**

```go
package workflow

import (
	"context"
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

func testWorkflowMeta() *meta.WorkflowMeta {
	return &meta.WorkflowMeta{
		Name:     "Sales Order Approval",
		DocType:  "Sales Order",
		IsActive: true,
		States: []meta.WorkflowState{
			{Name: "Draft", Style: "Info", DocStatus: 0},
			{Name: "Pending Approval", Style: "Warning", DocStatus: 0},
			{Name: "Approved", Style: "Success", DocStatus: 1},
		},
		Transitions: []meta.Transition{
			{From: "Draft", To: "Pending Approval", Action: "Submit", AllowedRoles: []string{"Sales User"}},
			{From: "Pending Approval", To: "Approved", Action: "Approve", AllowedRoles: []string{"Approver"}},
		},
	}
}

func TestWorkflowRegistry_GetAndInvalidate(t *testing.T) {
	reg := NewWorkflowRegistry()

	// Manually seed cache for unit testing (in production, loaded from DB via loader func)
	reg.Set("site1", "Sales Order", testWorkflowMeta())

	wf, err := reg.Get(context.Background(), "site1", "Sales Order")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.Name != "Sales Order Approval" {
		t.Errorf("Name = %q, want %q", wf.Name, "Sales Order Approval")
	}

	// Cache miss for unknown doctype
	_, err = reg.Get(context.Background(), "site1", "Unknown")
	if err == nil {
		t.Error("expected error for unknown doctype")
	}

	// Invalidate
	reg.Invalidate("site1", "Sales Order")
	_, err = reg.Get(context.Background(), "site1", "Sales Order")
	if err == nil {
		t.Error("expected error after invalidation")
	}
}

func TestWorkflowRegistry_FindTransition(t *testing.T) {
	wf := testWorkflowMeta()

	tr := FindTransition(wf, "Draft", "Submit", "")
	if tr == nil {
		t.Fatal("expected to find Draft->Submit transition")
	}
	if tr.To != "Pending Approval" {
		t.Errorf("To = %q, want %q", tr.To, "Pending Approval")
	}

	// No match
	tr = FindTransition(wf, "Draft", "Approve", "")
	if tr != nil {
		t.Error("expected nil for non-existent transition")
	}
}

func TestWorkflowRegistry_FindState(t *testing.T) {
	wf := testWorkflowMeta()

	st := FindState(wf, "Pending Approval")
	if st == nil {
		t.Fatal("expected to find state")
	}
	if st.Style != "Warning" {
		t.Errorf("Style = %q, want %q", st.Style, "Warning")
	}

	st = FindState(wf, "Nonexistent")
	if st != nil {
		t.Error("expected nil for nonexistent state")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -race -run "TestWorkflowRegistry|TestWorkflowRegistry_Find" ./pkg/workflow/...`
Expected: FAIL — types don't exist.

- [ ] **Step 3: Implement the registry**

```go
package workflow

import (
	"context"
	"fmt"
	"sync"

	"github.com/osama1998H/moca/pkg/meta"
)

// WorkflowRegistry caches active workflow definitions per site:doctype.
type WorkflowRegistry struct {
	cache map[string]*meta.WorkflowMeta // key: "site:doctype"
	mu    sync.RWMutex
}

// NewWorkflowRegistry creates an empty registry.
func NewWorkflowRegistry() *WorkflowRegistry {
	return &WorkflowRegistry{
		cache: make(map[string]*meta.WorkflowMeta),
	}
}

func cacheKey(site, doctype string) string {
	return site + ":" + doctype
}

// Set stores a workflow definition in the cache.
func (r *WorkflowRegistry) Set(site, doctype string, wf *meta.WorkflowMeta) {
	r.mu.Lock()
	r.cache[cacheKey(site, doctype)] = wf
	r.mu.Unlock()
}

// Get returns the cached workflow for a site:doctype pair.
// Returns ErrNoActiveWorkflow if not found.
func (r *WorkflowRegistry) Get(_ context.Context, site, doctype string) (*meta.WorkflowMeta, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	wf, ok := r.cache[cacheKey(site, doctype)]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoActiveWorkflow, doctype)
	}
	return wf, nil
}

// Invalidate removes the cached workflow for a site:doctype pair.
func (r *WorkflowRegistry) Invalidate(site, doctype string) {
	r.mu.Lock()
	delete(r.cache, cacheKey(site, doctype))
	r.mu.Unlock()
}

// FindTransition finds a transition from the given state with the given action.
// For parallel workflows, branchName filters transitions relevant to a branch.
// Returns nil if no matching transition is found.
func FindTransition(wf *meta.WorkflowMeta, fromState, action, branchName string) *meta.Transition {
	for i := range wf.Transitions {
		tr := &wf.Transitions[i]
		if tr.From == fromState && tr.Action == action {
			return tr
		}
	}
	return nil
}

// FindState finds a workflow state by name. Returns nil if not found.
func FindState(wf *meta.WorkflowMeta, name string) *meta.WorkflowState {
	for i := range wf.States {
		if wf.States[i].Name == name {
			return &wf.States[i]
		}
	}
	return nil
}

// FindBranches returns all states that have a JoinTarget matching joinState.
// These are the parallel branches that converge at joinState.
func FindBranches(wf *meta.WorkflowMeta, joinState string) []meta.WorkflowState {
	var branches []meta.WorkflowState
	for _, s := range wf.States {
		if s.JoinTarget == joinState {
			branches = append(branches, s)
		}
	}
	return branches
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run "TestWorkflowRegistry|TestWorkflowRegistry_Find" ./pkg/workflow/...`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/workflow/registry.go pkg/workflow/registry_test.go
git commit -m "feat(workflow): implement workflow registry with cache and lookup helpers"
```

---

## Task 6: Workflow Event Types

**Files:**
- Create: `pkg/workflow/events.go`
- Create: `pkg/workflow/events_test.go`

- [ ] **Step 1: Write tests**

```go
package workflow

import (
	"testing"
	"time"
)

func TestNewWorkflowEvent(t *testing.T) {
	evt := NewWorkflowEvent(
		EventTypeTransition,
		"site1",
		"Sales Order",
		"SO-0001",
		"Sales Order Approval",
		"Submit",
		"Draft",
		"Pending Approval",
		"",
		"user@example.com",
		"Ready for review",
		"req-123",
	)

	if evt.EventID == "" {
		t.Error("EventID should be generated")
	}
	if evt.EventType != EventTypeTransition {
		t.Errorf("EventType = %q, want %q", evt.EventType, EventTypeTransition)
	}
	if evt.DocType != "Sales Order" {
		t.Errorf("DocType = %q, want %q", evt.DocType, "Sales Order")
	}
	if evt.FromState != "Draft" {
		t.Errorf("FromState = %q, want %q", evt.FromState, "Draft")
	}
	if evt.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
}

func TestWorkflowEventTypes(t *testing.T) {
	types := []string{
		EventTypeTransition,
		EventTypeFork,
		EventTypeJoin,
		EventTypeQuorumVote,
		EventTypeQuorumMet,
		EventTypeSLAStarted,
		EventTypeSLABreached,
		EventTypeSLACancelled,
		EventTypeDelegated,
	}
	seen := make(map[string]bool)
	for _, et := range types {
		if seen[et] {
			t.Errorf("duplicate event type: %s", et)
		}
		seen[et] = true
	}
	if len(types) != 9 {
		t.Errorf("expected 9 event types, got %d", len(types))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -race -run TestNewWorkflowEvent ./pkg/workflow/...`
Expected: FAIL

- [ ] **Step 3: Implement events**

```go
package workflow

import (
	"time"

	"github.com/google/uuid"
	"github.com/osama1998H/moca/pkg/events"
)

// Workflow event type constants.
const (
	EventTypeTransition  = "workflow.transition"
	EventTypeFork        = "workflow.fork"
	EventTypeJoin        = "workflow.join"
	EventTypeQuorumVote  = "workflow.quorum.vote"
	EventTypeQuorumMet   = "workflow.quorum.met"
	EventTypeSLAStarted  = "workflow.sla.started"
	EventTypeSLABreached = "workflow.sla.breached"
	EventTypeSLACancelled = "workflow.sla.cancelled"
	EventTypeDelegated   = "workflow.delegated"
)

// WorkflowEvent is the envelope for all workflow-related events.
type WorkflowEvent struct {
	EventID      string    `json:"event_id"`
	EventType    string    `json:"event_type"`
	Timestamp    time.Time `json:"timestamp"`
	Source       string    `json:"source"`
	Site         string    `json:"site"`
	DocType      string    `json:"doc_type"`
	DocName      string    `json:"doc_name"`
	WorkflowName string   `json:"workflow_name"`
	Action       string    `json:"action"`
	FromState    string    `json:"from_state"`
	ToState      string    `json:"to_state"`
	BranchName   string    `json:"branch_name,omitempty"`
	User         string    `json:"user"`
	Comment      string    `json:"comment,omitempty"`
	RequestID    string    `json:"request_id"`
}

// NewWorkflowEvent constructs a WorkflowEvent with generated ID and timestamp.
func NewWorkflowEvent(
	eventType, site, docType, docName, workflowName,
	action, fromState, toState, branchName, user, comment, requestID string,
) WorkflowEvent {
	return WorkflowEvent{
		EventID:      uuid.NewString(),
		EventType:    eventType,
		Timestamp:    time.Now(),
		Source:       events.EventSourceMocaCore,
		Site:         site,
		DocType:      docType,
		DocName:      docName,
		WorkflowName: workflowName,
		Action:       action,
		FromState:    fromState,
		ToState:      toState,
		BranchName:   branchName,
		User:         user,
		Comment:      comment,
		RequestID:    requestID,
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run "TestNewWorkflowEvent|TestWorkflowEventTypes" ./pkg/workflow/...`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/workflow/events.go pkg/workflow/events_test.go
git commit -m "feat(workflow): add WorkflowEvent envelope and event type constants"
```

---

## Task 7: Core Workflow Engine — Linear Transitions

**Files:**
- Create: `pkg/workflow/engine.go`
- Create: `pkg/workflow/engine_test.go`

This is the largest task. The engine handles transition validation, role checking, condition evaluation, state updates, and event publishing. This task covers linear (non-parallel) workflows. Parallel support is added in Task 8.

- [ ] **Step 1: Write tests for linear workflow transitions**

```go
package workflow

import (
	"context"
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/events"
	"github.com/osama1998H/moca/pkg/meta"
)

func simpleWorkflow() *meta.WorkflowMeta {
	return &meta.WorkflowMeta{
		Name:     "Simple Approval",
		DocType:  "Task",
		IsActive: true,
		States: []meta.WorkflowState{
			{Name: "Draft", Style: "Info", DocStatus: 0},
			{Name: "Pending Approval", Style: "Warning", DocStatus: 0},
			{Name: "Approved", Style: "Success", DocStatus: 1},
			{Name: "Rejected", Style: "Danger", DocStatus: 0},
		},
		Transitions: []meta.Transition{
			{From: "Draft", To: "Pending Approval", Action: "Submit", AllowedRoles: []string{"User"}},
			{From: "Pending Approval", To: "Approved", Action: "Approve", AllowedRoles: []string{"Approver"}},
			{From: "Pending Approval", To: "Rejected", Action: "Reject", AllowedRoles: []string{"Approver"}, RequireComment: true},
		},
	}
}

func setupEngine(wf *meta.WorkflowMeta) *WorkflowEngine {
	reg := NewWorkflowRegistry()
	reg.Set("test-site", wf.DocType, wf)
	return NewWorkflowEngine(
		WithRegistry(reg),
		WithEvaluator(NewConditionEvaluator()),
	)
}

func setupDocCtx(email string, roles []string) *document.DocContext {
	return document.NewDocContext(
		context.Background(),
		nil,
		&auth.User{Email: email, Roles: roles},
	)
}

func TestEngine_Transition_Linear(t *testing.T) {
	engine := setupEngine(simpleWorkflow())
	doc := newTestDoc(map[string]any{"workflow_state": "Draft"})
	ctx := setupDocCtx("user@example.com", []string{"User"})
	ctx.Site = &tenancy.SiteContext{Name: "test-site"}

	err := engine.Transition(ctx, doc, "Submit", TransitionOpts{})
	if err != nil {
		t.Fatalf("transition failed: %v", err)
	}

	state := doc.Get("workflow_state")
	if state != "Pending Approval" {
		t.Errorf("workflow_state = %v, want %q", state, "Pending Approval")
	}
}

func TestEngine_Transition_BlockedWithoutRole(t *testing.T) {
	engine := setupEngine(simpleWorkflow())
	doc := newTestDoc(map[string]any{"workflow_state": "Pending Approval"})
	ctx := setupDocCtx("user@example.com", []string{"User"}) // Missing "Approver"
	ctx.Site = &tenancy.SiteContext{Name: "test-site"}

	err := engine.Transition(ctx, doc, "Approve", TransitionOpts{})
	if err == nil {
		t.Fatal("expected error for missing role")
	}
	if !errors.Is(err, ErrNoPermission) {
		t.Errorf("error = %v, want ErrNoPermission", err)
	}
}

func TestEngine_Transition_CommentRequired(t *testing.T) {
	engine := setupEngine(simpleWorkflow())
	doc := newTestDoc(map[string]any{"workflow_state": "Pending Approval"})
	ctx := setupDocCtx("approver@example.com", []string{"Approver"})
	ctx.Site = &tenancy.SiteContext{Name: "test-site"}

	// No comment — should fail
	err := engine.Transition(ctx, doc, "Reject", TransitionOpts{})
	if !errors.Is(err, ErrCommentRequired) {
		t.Errorf("error = %v, want ErrCommentRequired", err)
	}

	// With comment — should succeed
	err = engine.Transition(ctx, doc, "Reject", TransitionOpts{Comment: "Not ready"})
	if err != nil {
		t.Fatalf("transition with comment failed: %v", err)
	}
}

func TestEngine_Transition_ConditionBlocked(t *testing.T) {
	wf := simpleWorkflow()
	wf.Transitions[0].Condition = "doc.grand_total > 0"
	engine := setupEngine(wf)
	doc := newTestDoc(map[string]any{"workflow_state": "Draft", "grand_total": 0})
	ctx := setupDocCtx("user@example.com", []string{"User"})
	ctx.Site = &tenancy.SiteContext{Name: "test-site"}

	err := engine.Transition(ctx, doc, "Submit", TransitionOpts{})
	if !errors.Is(err, ErrConditionFailed) {
		t.Errorf("error = %v, want ErrConditionFailed", err)
	}
}

func TestEngine_Transition_InvalidAction(t *testing.T) {
	engine := setupEngine(simpleWorkflow())
	doc := newTestDoc(map[string]any{"workflow_state": "Draft"})
	ctx := setupDocCtx("user@example.com", []string{"User"})
	ctx.Site = &tenancy.SiteContext{Name: "test-site"}

	err := engine.Transition(ctx, doc, "Approve", TransitionOpts{})
	if !errors.Is(err, ErrInvalidAction) {
		t.Errorf("error = %v, want ErrInvalidAction", err)
	}
}

func TestEngine_GetAvailableActions(t *testing.T) {
	engine := setupEngine(simpleWorkflow())
	doc := newTestDoc(map[string]any{"workflow_state": "Pending Approval"})
	ctx := setupDocCtx("approver@example.com", []string{"Approver"})
	ctx.Site = &tenancy.SiteContext{Name: "test-site"}

	actions, err := engine.GetAvailableActions(ctx, doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions (Approve, Reject), got %d", len(actions))
	}
}

func TestEngine_GetState_Linear(t *testing.T) {
	engine := setupEngine(simpleWorkflow())
	doc := newTestDoc(map[string]any{"workflow_state": "Draft"})
	ctx := setupDocCtx("user@example.com", []string{"User"})
	ctx.Site = &tenancy.SiteContext{Name: "test-site"}

	status, err := engine.GetState(ctx, doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.IsParallel {
		t.Error("expected linear workflow")
	}
	if len(status.Branches) != 1 {
		t.Fatalf("expected 1 branch, got %d", len(status.Branches))
	}
	if status.Branches[0].CurrentState != "Draft" {
		t.Errorf("state = %q, want %q", status.Branches[0].CurrentState, "Draft")
	}
}
```

**Note:** The test imports `tenancy` for `SiteContext`. Add the import:
```go
import "github.com/osama1998H/moca/pkg/tenancy"
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -race -run "TestEngine_" ./pkg/workflow/...`
Expected: FAIL — `WorkflowEngine`, `NewWorkflowEngine`, `TransitionOpts` don't exist.

- [ ] **Step 3: Implement the engine**

```go
package workflow

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/events"
	"github.com/osama1998H/moca/pkg/meta"
)

// TransitionOpts holds optional parameters for a workflow transition.
type TransitionOpts struct {
	Comment    string
	BranchName string
}

// WorkflowStatus represents the current state of a document's workflow.
type WorkflowStatus struct {
	WorkflowName string
	Branches     []BranchStatus
	IsParallel   bool
}

// BranchStatus represents the state of a single workflow branch.
type BranchStatus struct {
	BranchName   string
	CurrentState string
	Style        string
	IsActive     bool
	EnteredAt    time.Time
	SLADeadline  *time.Time
}

// AvailableAction represents a transition the current user can perform.
type AvailableAction struct {
	Action         string
	ToState        string
	BranchName     string
	RequireComment bool
	Style          string
}

// AutoActionHandler is a function invoked after a transition completes.
type AutoActionHandler func(ctx *document.DocContext, doc document.Document) error

// WorkflowEngine is the core state machine. It validates and executes
// transitions, checks permissions, evaluates conditions, and publishes events.
type WorkflowEngine struct {
	registry    *WorkflowRegistry
	evaluator   *ConditionEvaluator
	emitter     *events.Emitter
	autoActions map[string]AutoActionHandler
	logger      *slog.Logger
}

// EngineOption configures a WorkflowEngine.
type EngineOption func(*WorkflowEngine)

// WithRegistry sets the workflow registry.
func WithRegistry(r *WorkflowRegistry) EngineOption {
	return func(e *WorkflowEngine) { e.registry = r }
}

// WithEvaluator sets the condition evaluator.
func WithEvaluator(ev *ConditionEvaluator) EngineOption {
	return func(e *WorkflowEngine) { e.evaluator = ev }
}

// WithEmitter sets the event emitter.
func WithEmitter(em *events.Emitter) EngineOption {
	return func(e *WorkflowEngine) { e.emitter = em }
}

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) EngineOption {
	return func(e *WorkflowEngine) { e.logger = l }
}

// NewWorkflowEngine creates a WorkflowEngine with the given options.
func NewWorkflowEngine(opts ...EngineOption) *WorkflowEngine {
	e := &WorkflowEngine{
		autoActions: make(map[string]AutoActionHandler),
		logger:      slog.Default(),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// RegisterAutoAction registers a named handler for transition auto-actions.
func (e *WorkflowEngine) RegisterAutoAction(name string, handler AutoActionHandler) {
	e.autoActions[name] = handler
}

// Transition executes a workflow transition on a document.
func (e *WorkflowEngine) Transition(ctx *document.DocContext, doc document.Document, action string, opts TransitionOpts) error {
	siteName := ""
	if ctx.Site != nil {
		siteName = ctx.Site.Name
	}
	doctype := doc.Meta().Name

	wf, err := e.registry.Get(ctx, siteName, doctype)
	if err != nil {
		return err
	}

	currentState, _ := doc.Get("workflow_state").(string)
	if currentState == "" {
		if len(wf.States) > 0 {
			currentState = wf.States[0].Name
		}
	}

	tr := FindTransition(wf, currentState, action, opts.BranchName)
	if tr == nil {
		return fmt.Errorf("%w: no transition from %q with action %q", ErrInvalidAction, currentState, action)
	}

	// Role check
	if err := e.checkRoles(ctx, tr); err != nil {
		return err
	}

	// Condition check
	if tr.Condition != "" && e.evaluator != nil {
		ok, err := e.evaluator.Eval(tr.Condition, doc, ctx)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: condition %q evaluated to false", ErrConditionFailed, tr.Condition)
		}
	}

	// Comment check
	if tr.RequireComment && opts.Comment == "" {
		return ErrCommentRequired
	}

	// Apply transition
	_ = doc.Set("workflow_state", tr.To)

	toState := FindState(wf, tr.To)
	if toState != nil {
		if toState.DocStatus > 0 {
			_ = doc.Set("docstatus", toState.DocStatus)
		}
		if toState.UpdateField != "" {
			_ = doc.Set(toState.UpdateField, toState.UpdateValue)
		}
	}

	// Auto-action
	if tr.AutoAction != "" {
		if handler, ok := e.autoActions[tr.AutoAction]; ok {
			if err := handler(ctx, doc); err != nil {
				return fmt.Errorf("workflow: auto-action %q failed: %w", tr.AutoAction, err)
			}
		}
	}

	// Publish event
	e.publishEvent(ctx, EventTypeTransition, doctype, doc.Name(),
		wf.Name, action, currentState, tr.To, opts.BranchName, opts.Comment)

	return nil
}

// CanTransition checks whether a transition is valid without executing it.
func (e *WorkflowEngine) CanTransition(ctx *document.DocContext, doc document.Document, action string) (bool, error) {
	siteName := ""
	if ctx.Site != nil {
		siteName = ctx.Site.Name
	}
	wf, err := e.registry.Get(ctx, siteName, doc.Meta().Name)
	if err != nil {
		return false, err
	}

	currentState, _ := doc.Get("workflow_state").(string)
	if currentState == "" && len(wf.States) > 0 {
		currentState = wf.States[0].Name
	}

	tr := FindTransition(wf, currentState, action, "")
	if tr == nil {
		return false, nil
	}

	if err := e.checkRoles(ctx, tr); err != nil {
		return false, nil
	}

	if tr.Condition != "" && e.evaluator != nil {
		ok, err := e.evaluator.Eval(tr.Condition, doc, ctx)
		if err != nil || !ok {
			return false, nil
		}
	}

	return true, nil
}

// GetState returns the current workflow status for a document.
func (e *WorkflowEngine) GetState(ctx *document.DocContext, doc document.Document) (*WorkflowStatus, error) {
	siteName := ""
	if ctx.Site != nil {
		siteName = ctx.Site.Name
	}
	wf, err := e.registry.Get(ctx, siteName, doc.Meta().Name)
	if err != nil {
		return nil, err
	}

	currentState, _ := doc.Get("workflow_state").(string)
	if currentState == "" && len(wf.States) > 0 {
		currentState = wf.States[0].Name
	}

	st := FindState(wf, currentState)
	style := ""
	if st != nil {
		style = st.Style
	}

	return &WorkflowStatus{
		WorkflowName: wf.Name,
		IsParallel:   false,
		Branches: []BranchStatus{{
			CurrentState: currentState,
			Style:        style,
			IsActive:     true,
			EnteredAt:    time.Now(),
		}},
	}, nil
}

// GetAvailableActions returns transitions the current user can perform.
func (e *WorkflowEngine) GetAvailableActions(ctx *document.DocContext, doc document.Document) ([]AvailableAction, error) {
	siteName := ""
	if ctx.Site != nil {
		siteName = ctx.Site.Name
	}
	wf, err := e.registry.Get(ctx, siteName, doc.Meta().Name)
	if err != nil {
		return nil, err
	}

	currentState, _ := doc.Get("workflow_state").(string)
	if currentState == "" && len(wf.States) > 0 {
		currentState = wf.States[0].Name
	}

	var actions []AvailableAction
	for _, tr := range wf.Transitions {
		if tr.From != currentState {
			continue
		}
		if e.checkRoles(ctx, &tr) != nil {
			continue
		}
		if tr.Condition != "" && e.evaluator != nil {
			ok, _ := e.evaluator.Eval(tr.Condition, doc, ctx)
			if !ok {
				continue
			}
		}
		toState := FindState(wf, tr.To)
		style := ""
		if toState != nil {
			style = toState.Style
		}
		actions = append(actions, AvailableAction{
			Action:         tr.Action,
			ToState:        tr.To,
			RequireComment: tr.RequireComment,
			Style:          style,
		})
	}
	return actions, nil
}

func (e *WorkflowEngine) checkRoles(ctx *document.DocContext, tr *meta.Transition) error {
	if len(tr.AllowedRoles) == 0 {
		return nil
	}
	if ctx.User == nil {
		return ErrNoPermission
	}
	for _, allowed := range tr.AllowedRoles {
		for _, userRole := range ctx.User.Roles {
			if allowed == userRole {
				return nil
			}
		}
	}
	return fmt.Errorf("%w: requires one of %v", ErrNoPermission, tr.AllowedRoles)
}

func (e *WorkflowEngine) publishEvent(ctx *document.DocContext, eventType, doctype, docname, workflowName, action, from, to, branch, comment string) {
	if e.emitter == nil {
		return
	}
	siteName := ""
	if ctx.Site != nil {
		siteName = ctx.Site.Name
	}
	userEmail := ""
	if ctx.User != nil {
		userEmail = ctx.User.Email
	}
	evt := NewWorkflowEvent(eventType, siteName, doctype, docname, workflowName,
		action, from, to, branch, userEmail, comment, ctx.RequestID)
	e.emitter.Emit(events.TopicWorkflowTransitions, evt)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run "TestEngine_" ./pkg/workflow/...`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/workflow/engine.go pkg/workflow/engine_test.go
git commit -m "feat(workflow): implement core engine with linear transitions and role/condition checks"
```

---

## Task 8: Parallel State Machine (AND-splits/joins)

**Files:**
- Modify: `pkg/workflow/engine.go`
- Modify: `pkg/workflow/engine_test.go`

This task extends the engine with `WorkflowStateTracker` in-memory management for parallel branches. Full database persistence of trackers comes with the API handler (Task 12). For now, the engine tracks branch state in memory and the tests validate the fork/join logic.

- [ ] **Step 1: Write tests for parallel transitions**

Add to `pkg/workflow/engine_test.go`:

```go
func parallelWorkflow() *meta.WorkflowMeta {
	return &meta.WorkflowMeta{
		Name:     "Dual Approval",
		DocType:  "Purchase Order",
		IsActive: true,
		States: []meta.WorkflowState{
			{Name: "Draft", Style: "Info", DocStatus: 0},
			{Name: "Review", Style: "Warning", DocStatus: 0, IsFork: true},
			{Name: "Pending Finance", Style: "Warning", DocStatus: 0, BranchName: "Finance", JoinTarget: "Approved"},
			{Name: "Finance Approved", Style: "Success", DocStatus: 0, BranchName: "Finance", JoinTarget: "Approved"},
			{Name: "Pending Legal", Style: "Warning", DocStatus: 0, BranchName: "Legal", JoinTarget: "Approved"},
			{Name: "Legal Approved", Style: "Success", DocStatus: 0, BranchName: "Legal", JoinTarget: "Approved"},
			{Name: "Approved", Style: "Success", DocStatus: 1},
		},
		Transitions: []meta.Transition{
			{From: "Draft", To: "Review", Action: "Submit", AllowedRoles: []string{"User"}},
			{From: "Pending Finance", To: "Finance Approved", Action: "Approve", AllowedRoles: []string{"Finance Approver"}},
			{From: "Pending Legal", To: "Legal Approved", Action: "Approve", AllowedRoles: []string{"Legal Approver"}},
		},
	}
}

func TestEngine_Transition_Fork(t *testing.T) {
	engine := setupEngine(parallelWorkflow())
	doc := newTestDoc(map[string]any{"workflow_state": "Draft"})
	ctx := setupDocCtx("user@example.com", []string{"User"})
	ctx.Site = &tenancy.SiteContext{Name: "test-site"}

	err := engine.Transition(ctx, doc, "Submit", TransitionOpts{})
	if err != nil {
		t.Fatalf("transition to fork state failed: %v", err)
	}

	status, err := engine.GetState(ctx, doc)
	if err != nil {
		t.Fatalf("get state failed: %v", err)
	}
	if !status.IsParallel {
		t.Error("expected parallel workflow after fork")
	}
	if len(status.Branches) != 2 {
		t.Fatalf("expected 2 branches, got %d", len(status.Branches))
	}
}

func TestEngine_Transition_ParallelBranch(t *testing.T) {
	engine := setupEngine(parallelWorkflow())
	doc := newTestDoc(map[string]any{"workflow_state": "Draft"})
	ctx := setupDocCtx("user@example.com", []string{"User", "Finance Approver"})
	ctx.Site = &tenancy.SiteContext{Name: "test-site"}

	// Fork
	_ = engine.Transition(ctx, doc, "Submit", TransitionOpts{})

	// Approve Finance branch
	err := engine.Transition(ctx, doc, "Approve", TransitionOpts{BranchName: "Finance"})
	if err != nil {
		t.Fatalf("finance approval failed: %v", err)
	}

	status, _ := engine.GetState(ctx, doc)
	for _, b := range status.Branches {
		if b.BranchName == "Finance" && b.CurrentState != "Finance Approved" {
			t.Errorf("Finance branch state = %q, want %q", b.CurrentState, "Finance Approved")
		}
		if b.BranchName == "Legal" && b.CurrentState != "Pending Legal" {
			t.Errorf("Legal branch state = %q, want %q", b.CurrentState, "Pending Legal")
		}
	}
}

func TestEngine_Transition_Join(t *testing.T) {
	engine := setupEngine(parallelWorkflow())
	doc := newTestDoc(map[string]any{"workflow_state": "Draft"})
	ctx := setupDocCtx("user@example.com", []string{"User", "Finance Approver", "Legal Approver"})
	ctx.Site = &tenancy.SiteContext{Name: "test-site"}

	// Fork
	_ = engine.Transition(ctx, doc, "Submit", TransitionOpts{})

	// Approve both branches
	_ = engine.Transition(ctx, doc, "Approve", TransitionOpts{BranchName: "Finance"})
	err := engine.Transition(ctx, doc, "Approve", TransitionOpts{BranchName: "Legal"})
	if err != nil {
		t.Fatalf("legal approval failed: %v", err)
	}

	// After both branches complete, should auto-join to "Approved"
	state := doc.Get("workflow_state")
	if state != "Approved" {
		t.Errorf("workflow_state = %v, want %q (should have auto-joined)", state, "Approved")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -race -run "TestEngine_Transition_Fork|TestEngine_Transition_ParallelBranch|TestEngine_Transition_Join" ./pkg/workflow/...`
Expected: FAIL — fork/join logic not implemented.

- [ ] **Step 3: Add branch state tracking and fork/join logic to engine**

Add to `engine.go` — a `branchStates` map on the engine for in-memory tracking, and extend `Transition` to handle forks and parallel branches:

```go
// Add to WorkflowEngine struct:
// branchStates tracks parallel branch state per document.
// Key: "site:doctype:docname", Value: map[branchName]BranchStatus
type WorkflowEngine struct {
	// ... existing fields ...
	branchStates map[string]map[string]*BranchStatus
	bsMu         sync.RWMutex
}

// Update NewWorkflowEngine to initialize:
// e.branchStates = make(map[string]map[string]*BranchStatus)
```

Then extend the `Transition` method to detect fork states, create branch trackers, handle per-branch transitions, and detect join completion. The key logic:

1. On entering a fork state: enumerate branches via `FindBranches`, create branch status entries, set each to its initial state.
2. On per-branch transition: look up branch by `opts.BranchName`, find the matching transition from that branch's current state, update the branch state.
3. After each branch transition: check if all branches for the join target are complete. If so, activate the join state and clear branch tracking.

This involves roughly 80-100 lines of additional logic in `engine.go`. The implementation should:
- Store branch states keyed by `branchStateKey(siteName, doctype, docName)`.
- On fork: set `doc.workflow_state` to the fork state name, create `BranchStatus` entries for each branch.
- On branch transition: update the specific branch, check join readiness.
- On join: set `doc.workflow_state` to the join target, clear branch map.
- Update `GetState` to return parallel branches from `branchStates`.
- Update `GetAvailableActions` to iterate per-branch actions.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run "TestEngine_" ./pkg/workflow/...`
Expected: All PASS (both linear and parallel tests)

- [ ] **Step 5: Commit**

```bash
git add pkg/workflow/engine.go pkg/workflow/engine_test.go
git commit -m "feat(workflow): add parallel state machine with AND-split/join support"
```

---

## Task 9: Approval Manager

**Files:**
- Create: `pkg/workflow/approval.go`
- Create: `pkg/workflow/approval_test.go`

- [ ] **Step 1: Write tests**

```go
package workflow

import (
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
)

func TestApprovalManager_SingleApproval(t *testing.T) {
	am := NewApprovalManager()
	tr := &meta.Transition{
		From:   "Pending",
		To:     "Approved",
		Action: "Approve",
	}

	am.RecordAction("Task", "T-001", "Pending", "Approve", "", "alice@example.com", "Looks good")
	result := am.CheckQuorum("Task", "T-001", "Pending", "Approve", "", tr)

	if !result.IsMet {
		t.Error("single approval should meet quorum with QuorumCount=0")
	}
	if result.Received != 1 {
		t.Errorf("Received = %d, want 1", result.Received)
	}
}

func TestApprovalManager_QuorumNotMet(t *testing.T) {
	am := NewApprovalManager()
	tr := &meta.Transition{
		From:         "Pending",
		To:           "Approved",
		Action:       "Approve",
		QuorumCount:  3,
	}

	am.RecordAction("Task", "T-001", "Pending", "Approve", "", "alice@example.com", "")
	am.RecordAction("Task", "T-001", "Pending", "Approve", "", "bob@example.com", "")

	result := am.CheckQuorum("Task", "T-001", "Pending", "Approve", "", tr)
	if result.IsMet {
		t.Error("quorum should not be met with 2 of 3")
	}
	if result.Required != 3 {
		t.Errorf("Required = %d, want 3", result.Required)
	}
	if result.Received != 2 {
		t.Errorf("Received = %d, want 2", result.Received)
	}
}

func TestApprovalManager_QuorumMet(t *testing.T) {
	am := NewApprovalManager()
	tr := &meta.Transition{
		From:        "Pending",
		To:          "Approved",
		Action:      "Approve",
		QuorumCount: 2,
	}

	am.RecordAction("Task", "T-001", "Pending", "Approve", "", "alice@example.com", "")
	am.RecordAction("Task", "T-001", "Pending", "Approve", "", "bob@example.com", "")

	result := am.CheckQuorum("Task", "T-001", "Pending", "Approve", "", tr)
	if !result.IsMet {
		t.Error("quorum should be met with 2 of 2")
	}
}

func TestApprovalManager_DuplicateApproval(t *testing.T) {
	am := NewApprovalManager()
	am.RecordAction("Task", "T-001", "Pending", "Approve", "", "alice@example.com", "")

	if !am.HasAlreadyActed("Task", "T-001", "Pending", "Approve", "", "alice@example.com") {
		t.Error("should detect duplicate approval")
	}
	if am.HasAlreadyActed("Task", "T-001", "Pending", "Approve", "", "bob@example.com") {
		t.Error("should not detect action from different user")
	}
}

func TestApprovalManager_Delegate(t *testing.T) {
	am := NewApprovalManager()
	am.Delegate("Task", "T-001", "alice@example.com", "charlie@example.com")

	actions := am.GetActions("Task", "T-001")
	if len(actions) != 1 {
		t.Fatalf("expected 1 delegation action, got %d", len(actions))
	}
	if actions[0].Action != "Delegate" {
		t.Errorf("action = %q, want %q", actions[0].Action, "Delegate")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -race -run TestApprovalManager ./pkg/workflow/...`
Expected: FAIL

- [ ] **Step 3: Implement the approval manager**

```go
package workflow

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/osama1998H/moca/pkg/meta"
)

// ActionRecord represents a recorded workflow action (approval, rejection, delegation).
type ActionRecord struct {
	ID            string
	ReferenceType string
	ReferenceName string
	WorkflowName  string
	Action        string
	FromState     string
	ToState       string
	BranchName    string
	User          string
	Comment       string
	Timestamp     time.Time
}

// ApprovalManager tracks approval actions and evaluates quorum.
type ApprovalManager struct {
	// actions keyed by "doctype:docname"
	actions map[string][]ActionRecord
	mu      sync.RWMutex
}

// QuorumResult holds the result of a quorum check.
type QuorumResult struct {
	Required    int
	Received    int
	IsMet       bool
	ApprovedBy  []string
	PendingFrom []string
}

// NewApprovalManager creates an ApprovalManager.
func NewApprovalManager() *ApprovalManager {
	return &ApprovalManager{
		actions: make(map[string][]ActionRecord),
	}
}

func actionKey(doctype, docname string) string {
	return doctype + ":" + docname
}

// RecordAction stores an action record.
func (a *ApprovalManager) RecordAction(doctype, docname, fromState, action, branchName, user, comment string) ActionRecord {
	rec := ActionRecord{
		ID:            uuid.NewString(),
		ReferenceType: doctype,
		ReferenceName: docname,
		Action:        action,
		FromState:     fromState,
		BranchName:    branchName,
		User:          user,
		Comment:       comment,
		Timestamp:     time.Now(),
	}
	a.mu.Lock()
	key := actionKey(doctype, docname)
	a.actions[key] = append(a.actions[key], rec)
	a.mu.Unlock()
	return rec
}

// CheckQuorum counts matching actions and returns whether quorum is met.
func (a *ApprovalManager) CheckQuorum(doctype, docname, fromState, action, branchName string, tr *meta.Transition) QuorumResult {
	required := tr.QuorumCount
	if required <= 0 {
		required = 1
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	var approved []string
	for _, rec := range a.actions[actionKey(doctype, docname)] {
		if rec.FromState == fromState && rec.Action == action && rec.BranchName == branchName {
			approved = append(approved, rec.User)
		}
	}

	return QuorumResult{
		Required:    required,
		Received:    len(approved),
		IsMet:       len(approved) >= required,
		ApprovedBy:  approved,
		PendingFrom: nil, // populated by caller when assignees are known
	}
}

// HasAlreadyActed checks if a user already performed the given action.
func (a *ApprovalManager) HasAlreadyActed(doctype, docname, fromState, action, branchName, user string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, rec := range a.actions[actionKey(doctype, docname)] {
		if rec.FromState == fromState && rec.Action == action && rec.BranchName == branchName && rec.User == user {
			return true
		}
	}
	return false
}

// Delegate records a delegation action.
func (a *ApprovalManager) Delegate(doctype, docname, fromUser, toUser string) {
	a.RecordAction(doctype, docname, "", "Delegate", "", fromUser, "Delegated to "+toUser)
}

// GetActions returns all recorded actions for a document.
func (a *ApprovalManager) GetActions(doctype, docname string) []ActionRecord {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.actions[actionKey(doctype, docname)]
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run TestApprovalManager ./pkg/workflow/...`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/workflow/approval.go pkg/workflow/approval_test.go
git commit -m "feat(workflow): implement approval manager with quorum and delegation"
```

---

## Task 10: SLA Manager

**Files:**
- Create: `pkg/workflow/sla.go`
- Create: `pkg/workflow/sla_test.go`

- [ ] **Step 1: Write tests**

```go
package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/osama1998H/moca/pkg/meta"
)

func TestSLAManager_StartTimer(t *testing.T) {
	sm := NewSLAManager(nil) // nil producer — just test state management
	rule := &meta.SLARule{
		State:            "Pending Approval",
		MaxDuration:      24 * time.Hour,
		EscalationRole:   "Manager",
		EscalationAction: "escalate",
	}

	tracker := &BranchStatus{
		BranchName:   "",
		CurrentState: "Pending Approval",
		IsActive:     true,
		EnteredAt:    time.Now(),
	}

	err := sm.StartTimer(context.Background(), "site1", "Task", "T-001", rule, tracker)
	if err != nil {
		t.Fatalf("start timer: %v", err)
	}

	if tracker.SLADeadline == nil {
		t.Fatal("SLADeadline should be set")
	}
	expectedDeadline := tracker.EnteredAt.Add(24 * time.Hour)
	if tracker.SLADeadline.Sub(expectedDeadline) > time.Second {
		t.Errorf("deadline = %v, expected ~%v", tracker.SLADeadline, expectedDeadline)
	}

	timers := sm.ActiveTimers()
	if len(timers) != 1 {
		t.Errorf("active timers = %d, want 1", len(timers))
	}
}

func TestSLAManager_CancelTimer(t *testing.T) {
	sm := NewSLAManager(nil)
	rule := &meta.SLARule{State: "Pending", MaxDuration: time.Hour}
	tracker := &BranchStatus{CurrentState: "Pending", IsActive: true, EnteredAt: time.Now()}

	_ = sm.StartTimer(context.Background(), "site1", "Task", "T-001", rule, tracker)
	sm.CancelTimer("Task", "T-001", "")

	timers := sm.ActiveTimers()
	if len(timers) != 0 {
		t.Errorf("active timers = %d, want 0 after cancel", len(timers))
	}
}

func TestSLAManager_CheckBreaches(t *testing.T) {
	sm := NewSLAManager(nil)
	rule := &meta.SLARule{State: "Pending", MaxDuration: -1 * time.Hour} // Already breached
	tracker := &BranchStatus{CurrentState: "Pending", IsActive: true, EnteredAt: time.Now()}

	_ = sm.StartTimer(context.Background(), "site1", "Task", "T-001", rule, tracker)

	breached := sm.CheckBreaches()
	if len(breached) != 1 {
		t.Fatalf("breached = %d, want 1", len(breached))
	}
	if breached[0].DocName != "T-001" {
		t.Errorf("DocName = %q, want %q", breached[0].DocName, "T-001")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -race -run TestSLAManager ./pkg/workflow/...`
Expected: FAIL

- [ ] **Step 3: Implement SLA manager**

```go
package workflow

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/queue"
)

// SLATimer tracks an active SLA deadline for a document/branch.
type SLATimer struct {
	Site       string
	DocType    string
	DocName    string
	BranchName string
	State      string
	Deadline   time.Time
	Rule       *meta.SLARule
	Escalated  bool
}

// SLAEscalation represents a detected SLA breach.
type SLAEscalation struct {
	Site        string
	DocType     string
	DocName     string
	BranchName  string
	State       string
	Deadline    time.Time
	BreachedAt  time.Time
	BreachDelta time.Duration
	Rule        *meta.SLARule
}

// SLAManager handles SLA timer lifecycle.
type SLAManager struct {
	producer *queue.Producer
	timers   map[string]*SLATimer // key: "doctype:docname:branch"
	mu       sync.RWMutex
	logger   *slog.Logger
}

// NewSLAManager creates an SLAManager. producer may be nil for testing.
func NewSLAManager(producer *queue.Producer) *SLAManager {
	return &SLAManager{
		producer: producer,
		timers:   make(map[string]*SLATimer),
		logger:   slog.Default(),
	}
}

func slaKey(doctype, docname, branch string) string {
	return doctype + ":" + docname + ":" + branch
}

// StartTimer registers an SLA timer and optionally enqueues a delayed job.
func (s *SLAManager) StartTimer(ctx context.Context, site, doctype, docname string, rule *meta.SLARule, tracker *BranchStatus) error {
	deadline := tracker.EnteredAt.Add(rule.MaxDuration)
	tracker.SLADeadline = &deadline

	timer := &SLATimer{
		Site:       site,
		DocType:    doctype,
		DocName:    docname,
		BranchName: tracker.BranchName,
		State:      rule.State,
		Deadline:   deadline,
		Rule:       rule,
	}

	s.mu.Lock()
	s.timers[slaKey(doctype, docname, tracker.BranchName)] = timer
	s.mu.Unlock()

	// Enqueue delayed job if producer is available
	if s.producer != nil {
		job := queue.Job{
			Type: "workflow.sla.check",
			Site: site,
			Payload: map[string]any{
				"doctype":  doctype,
				"docname":  docname,
				"branch":   tracker.BranchName,
				"state":    rule.State,
				"deadline": deadline.Format(time.RFC3339),
			},
			RunAfter:   &deadline,
			MaxRetries: 1,
		}
		_, err := s.producer.Enqueue(ctx, site, queue.QueueCritical, job)
		if err != nil {
			s.logger.Warn("failed to enqueue SLA job", slog.String("error", err.Error()))
		}
	}

	return nil
}

// CancelTimer removes an active SLA timer.
func (s *SLAManager) CancelTimer(doctype, docname, branch string) {
	s.mu.Lock()
	delete(s.timers, slaKey(doctype, docname, branch))
	s.mu.Unlock()
}

// CheckBreaches returns all active timers that have exceeded their deadline.
func (s *SLAManager) CheckBreaches() []SLAEscalation {
	now := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()

	var breached []SLAEscalation
	for _, timer := range s.timers {
		if !timer.Escalated && now.After(timer.Deadline) {
			breached = append(breached, SLAEscalation{
				Site:        timer.Site,
				DocType:     timer.DocType,
				DocName:     timer.DocName,
				BranchName:  timer.BranchName,
				State:       timer.State,
				Deadline:    timer.Deadline,
				BreachedAt:  now,
				BreachDelta: now.Sub(timer.Deadline),
				Rule:        timer.Rule,
			})
		}
	}
	return breached
}

// MarkEscalated marks a timer as having been escalated.
func (s *SLAManager) MarkEscalated(doctype, docname, branch string) {
	s.mu.Lock()
	if timer, ok := s.timers[slaKey(doctype, docname, branch)]; ok {
		timer.Escalated = true
	}
	s.mu.Unlock()
}

// ActiveTimers returns a snapshot of all active (non-escalated) timers.
func (s *SLAManager) ActiveTimers() []SLATimer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []SLATimer
	for _, t := range s.timers {
		if !t.Escalated {
			result = append(result, *t)
		}
	}
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run TestSLAManager ./pkg/workflow/...`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/workflow/sla.go pkg/workflow/sla_test.go
git commit -m "feat(workflow): implement SLA manager with hybrid timer and breach detection"
```

---

## Task 11: Hook Bridge

**Files:**
- Create: `pkg/workflow/bridge.go`
- Create: `pkg/workflow/bridge_test.go`

- [ ] **Step 1: Write tests**

```go
package workflow

import (
	"context"
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/hooks"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/tenancy"
)

func TestBridge_RegistersHooks(t *testing.T) {
	engine := setupEngine(simpleWorkflow())
	hookRegistry := hooks.NewHookRegistry()
	bridge := NewWorkflowBridge(engine)
	bridge.Register(hookRegistry)

	// Verify hooks were registered by resolving them
	handlers, err := hookRegistry.ResolveGlobal(document.EventBeforeSave)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(handlers) == 0 {
		t.Error("expected at least one global BeforeSave handler from bridge")
	}
}

func TestBridge_BundledSave_ExecutesTransition(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)
	hookRegistry := hooks.NewHookRegistry()
	bridge := NewWorkflowBridge(engine)
	bridge.Register(hookRegistry)

	doc := newTestDoc(map[string]any{"workflow_state": "Draft"})
	ctx := document.NewDocContext(
		context.Background(),
		&tenancy.SiteContext{Name: "test-site"},
		&auth.User{Email: "user@example.com", Roles: []string{"User"}},
	)
	ctx.Flags["workflow_action"] = "Submit"

	dispatcher := hooks.NewDocEventDispatcher(hookRegistry)
	err := dispatcher.Dispatch(ctx, doc, "Task", document.EventAfterSave)
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}

	state := doc.Get("workflow_state")
	if state != "Pending Approval" {
		t.Errorf("workflow_state = %v, want %q", state, "Pending Approval")
	}
}

func TestBridge_NoWorkflowAction_Noop(t *testing.T) {
	wf := simpleWorkflow()
	engine := setupEngine(wf)
	hookRegistry := hooks.NewHookRegistry()
	bridge := NewWorkflowBridge(engine)
	bridge.Register(hookRegistry)

	doc := newTestDoc(map[string]any{"workflow_state": "Draft"})
	ctx := document.NewDocContext(
		context.Background(),
		&tenancy.SiteContext{Name: "test-site"},
		&auth.User{Email: "user@example.com", Roles: []string{"User"}},
	)
	// No workflow_action flag

	dispatcher := hooks.NewDocEventDispatcher(hookRegistry)
	err := dispatcher.Dispatch(ctx, doc, "Task", document.EventAfterSave)
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}

	// State should remain unchanged
	state := doc.Get("workflow_state")
	if state != "Draft" {
		t.Errorf("workflow_state = %v, want %q (should be unchanged)", state, "Draft")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -race -run TestBridge ./pkg/workflow/...`
Expected: FAIL

- [ ] **Step 3: Implement the bridge**

```go
package workflow

import (
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/hooks"
)

// WorkflowBridge connects the WorkflowEngine to the document lifecycle
// by registering global hooks.
type WorkflowBridge struct {
	engine *WorkflowEngine
}

// NewWorkflowBridge creates a bridge for the given engine.
func NewWorkflowBridge(engine *WorkflowEngine) *WorkflowBridge {
	return &WorkflowBridge{engine: engine}
}

// Register adds workflow hooks to the HookRegistry.
func (b *WorkflowBridge) Register(r *hooks.HookRegistry) {
	// BeforeSave: validate the transition if workflow_action is present
	r.RegisterGlobal(document.EventBeforeSave, hooks.PrioritizedHandler{
		Handler:  b.handleBeforeSave,
		AppName:  "moca-workflow",
		Priority: 100, // Run early
	})

	// AfterSave: execute the transition if workflow_action is present
	r.RegisterGlobal(document.EventAfterSave, hooks.PrioritizedHandler{
		Handler:  b.handleAfterSave,
		AppName:  "moca-workflow",
		Priority: 100,
	})
}

func (b *WorkflowBridge) handleBeforeSave(ctx *document.DocContext, doc document.Document) error {
	action, ok := ctx.Flags["workflow_action"].(string)
	if !ok || action == "" {
		return nil
	}

	// Validate that the transition is possible
	can, err := b.engine.CanTransition(ctx, doc, action)
	if err != nil {
		return err
	}
	if !can {
		return ErrTransitionBlocked
	}
	return nil
}

func (b *WorkflowBridge) handleAfterSave(ctx *document.DocContext, doc document.Document) error {
	action, ok := ctx.Flags["workflow_action"].(string)
	if !ok || action == "" {
		return nil
	}

	comment, _ := ctx.Flags["workflow_comment"].(string)
	branch, _ := ctx.Flags["workflow_branch"].(string)

	return b.engine.Transition(ctx, doc, action, TransitionOpts{
		Comment:    comment,
		BranchName: branch,
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run TestBridge ./pkg/workflow/...`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/workflow/bridge.go pkg/workflow/bridge_test.go
git commit -m "feat(workflow): implement hook bridge connecting engine to document lifecycle"
```

---

## Task 12: Workflow API Handler

**Files:**
- Create: `pkg/api/workflow_handler.go`
- Create: `pkg/api/workflow_handler_test.go`
- Modify: `internal/serve/server.go`

- [ ] **Step 1: Write tests**

```go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/osama1998H/moca/pkg/workflow"
)

func TestWorkflowHandler_RegisterRoutes(t *testing.T) {
	mux := http.NewServeMux()
	h := &WorkflowHandler{}
	h.RegisterRoutes(mux, "v1")

	// Verify routes are registered by sending requests
	routes := []struct {
		method string
		path   string
	}{
		{"POST", "/api/v1/workflow/Task/T-001/transition"},
		{"GET", "/api/v1/workflow/Task/T-001/state"},
		{"GET", "/api/v1/workflow/Task/T-001/history"},
		{"GET", "/api/v1/workflow/pending"},
	}
	for _, r := range routes {
		req := httptest.NewRequest(r.method, r.path, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		// 404 means the route wasn't registered; anything else means it was
		// (even 500 or 401 means the handler was called)
		if rr.Code == http.StatusNotFound {
			t.Errorf("route %s %s not registered", r.method, r.path)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestWorkflowHandler_RegisterRoutes ./pkg/api/...`
Expected: FAIL — `WorkflowHandler` doesn't exist.

- [ ] **Step 3: Implement the handler**

```go
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/osama1998H/moca/pkg/workflow"
)

// WorkflowHandler serves workflow API endpoints.
type WorkflowHandler struct {
	engine *workflow.WorkflowEngine
	logger *slog.Logger
}

// NewWorkflowHandler creates a WorkflowHandler.
func NewWorkflowHandler(engine *workflow.WorkflowEngine, logger *slog.Logger) *WorkflowHandler {
	return &WorkflowHandler{engine: engine, logger: logger}
}

// RegisterRoutes registers workflow endpoints on the mux.
func (h *WorkflowHandler) RegisterRoutes(mux *http.ServeMux, version string) {
	p := "/api/" + version
	mux.HandleFunc("POST "+p+"/workflow/{doctype}/{name}/transition", h.handleTransition)
	mux.HandleFunc("GET "+p+"/workflow/{doctype}/{name}/state", h.handleGetState)
	mux.HandleFunc("GET "+p+"/workflow/{doctype}/{name}/history", h.handleGetHistory)
	mux.HandleFunc("GET "+p+"/workflow/pending", h.handleGetPending)
}

type transitionRequest struct {
	Action  string `json:"action"`
	Comment string `json:"comment"`
	Branch  string `json:"branch"`
}

func (h *WorkflowHandler) handleTransition(w http.ResponseWriter, r *http.Request) {
	doctype := r.PathValue("doctype")
	name := r.PathValue("name")

	var req transitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "message": err.Error()})
		return
	}

	ctx := docContextFromRequest(r)
	if ctx == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	// Load the document — the engine needs a Document interface
	// This is a stub: in production, integrate with DocManager
	_ = doctype
	_ = name
	_ = ctx

	// TODO: integrate with DocManager.Get() to load the actual document,
	// then call h.engine.Transition(ctx, doc, req.Action, opts)
	// For now, return method-not-allowed to indicate the route is registered

	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"error":   "not_implemented",
		"message": "workflow transition endpoint registered, integration pending",
	})
}

func (h *WorkflowHandler) handleGetState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"error":   "not_implemented",
		"message": "workflow state endpoint registered, integration pending",
	})
}

func (h *WorkflowHandler) handleGetHistory(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"error":   "not_implemented",
		"message": "workflow history endpoint registered, integration pending",
	})
}

func (h *WorkflowHandler) handleGetPending(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"error":   "not_implemented",
		"message": "pending approvals endpoint registered, integration pending",
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// docContextFromRequest extracts a DocContext from the request.
// Returns nil if auth context is missing.
func docContextFromRequest(r *http.Request) *document.DocContext {
	site, _ := SiteFromContext(r.Context())
	user, _ := UserFromContext(r.Context())
	if user == nil {
		return nil
	}
	ctx := document.NewDocContext(r.Context(), site, user)
	ctx.RequestID, _ = RequestIDFromContext(r.Context())
	return ctx
}
```

**Note:** The handler stubs return 501 for now. The full DocManager integration (loading documents, executing transitions, persisting state tracker rows) is Task 13. This task focuses on route registration and request parsing.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run TestWorkflowHandler_RegisterRoutes ./pkg/api/...`
Expected: PASS

- [ ] **Step 5: Wire up in server.go**

Add to `internal/serve/server.go` after the notification handler registration (around line 272):

```go
// Workflow engine and API handler.
workflowEngine := workflow.NewWorkflowEngine(
	workflow.WithRegistry(workflow.NewWorkflowRegistry()),
	workflow.WithEvaluator(workflow.NewConditionEvaluator()),
	workflow.WithEmitter(eventEmitter),
	workflow.WithLogger(logger),
)
workflowBridge := workflow.NewWorkflowBridge(workflowEngine)
workflowBridge.Register(hookRegistry)
workflowHandler := api.NewWorkflowHandler(workflowEngine, logger)
workflowHandler.RegisterRoutes(gw.Mux(), "v1")
```

- [ ] **Step 6: Verify build**

Run: `go build ./cmd/moca-server/...`
Expected: Success

- [ ] **Step 7: Commit**

```bash
git add pkg/api/workflow_handler.go pkg/api/workflow_handler_test.go internal/serve/server.go
git commit -m "feat(workflow): add workflow API handler with route registration"
```

---

## Task 13: Full API Handler Integration with DocManager

**Files:**
- Modify: `pkg/api/workflow_handler.go`
- Modify: `pkg/api/workflow_handler_test.go`

This task replaces the 501 stubs with full DocManager integration: loading documents, executing transitions, returning workflow state and history.

**Important:** Tasks 7-11 use in-memory state for unit testability. This task bridges to database persistence. The `WorkflowStateTracker` and `WorkflowAction` types map to database tables via the existing MetaType DDL system — register them as DocTypes so the ORM auto-generates their tables. The engine's in-memory `branchStates` and `ApprovalManager.actions` maps become backed by DB reads/writes through `DocManager.Get`/`Insert`/`GetList`.

- [ ] **Step 1: Update the handler to accept DocManager**

Add `docManager *document.DocManager` and `approvals *workflow.ApprovalManager` to `WorkflowHandler`. Update the constructor.

- [ ] **Step 2: Implement handleTransition fully**

Load the document via `h.docManager.Get()`, call `h.engine.Transition()`, record the action via `h.approvals.RecordAction()`, return the new workflow state as JSON.

- [ ] **Step 3: Implement handleGetState**

Load the document, call `h.engine.GetState()` and `h.engine.GetAvailableActions()`, return the combined response.

- [ ] **Step 4: Implement handleGetHistory**

Call `h.approvals.GetActions()` for the document, return as JSON array.

- [ ] **Step 5: Implement handleGetPending**

Query `WorkflowStateTracker` (or in-memory branch states) for documents where the user has available actions. This requires iterating active trackers and filtering by user roles.

- [ ] **Step 6: Write integration-style tests**

Test the full flow: create a handler with mock DocManager, execute transitions via HTTP, verify state changes and history.

- [ ] **Step 7: Run all API tests**

Run: `go test -race ./pkg/api/...`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add pkg/api/workflow_handler.go pkg/api/workflow_handler_test.go
git commit -m "feat(workflow): integrate API handler with DocManager for full transition flow"
```

---

## Task 14: Frontend — TypeScript Types and API Hooks

**Files:**
- Modify: `desk/src/api/types.ts`
- Create: `desk/src/components/workflow/useWorkflow.ts`

- [ ] **Step 1: Add workflow types to types.ts**

Add to `desk/src/api/types.ts`:

```ts
// ── Workflow Types ──────────────────────────────────────────

export interface WorkflowBranchStatus {
  branch: string;
  state: string;
  style: string;
  is_active: boolean;
  entered_at: string;
  sla_deadline?: string;
}

export interface WorkflowStatus {
  workflow_name: string;
  is_parallel: boolean;
  branches: WorkflowBranchStatus[];
}

export interface WorkflowAvailableAction {
  action: string;
  to_state: string;
  branch_name: string;
  require_comment: boolean;
  style: string;
}

export interface WorkflowStateResponse {
  status: WorkflowStatus;
  actions: WorkflowAvailableAction[];
}

export interface WorkflowActionRecord {
  id: string;
  action: string;
  from_state: string;
  to_state: string;
  branch_name: string;
  user: string;
  comment: string;
  timestamp: string;
}

export interface WorkflowTransitionRequest {
  action: string;
  comment?: string;
  branch?: string;
}

export interface WorkflowTransitionResponse {
  status: string;
  state: WorkflowStatus;
}
```

- [ ] **Step 2: Create useWorkflow hook**

Create `desk/src/components/workflow/useWorkflow.ts`:

```ts
import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from "@tanstack/react-query";
import { get, post } from "../../api/client";
import type {
  WorkflowActionRecord,
  WorkflowStateResponse,
  WorkflowTransitionRequest,
  WorkflowTransitionResponse,
} from "../../api/types";

export function useWorkflowState(
  doctype: string,
  name: string,
): UseQueryResult<WorkflowStateResponse, Error> {
  return useQuery({
    queryKey: ["workflowState", doctype, name],
    queryFn: () =>
      get<WorkflowStateResponse>(
        `workflow/${doctype}/${encodeURIComponent(name)}/state`,
      ),
    enabled: doctype.length > 0 && name.length > 0,
  });
}

export function useWorkflowHistory(
  doctype: string,
  name: string,
): UseQueryResult<WorkflowActionRecord[], Error> {
  return useQuery({
    queryKey: ["workflowHistory", doctype, name],
    queryFn: async () => {
      const res = await get<{ data: WorkflowActionRecord[] }>(
        `workflow/${doctype}/${encodeURIComponent(name)}/history`,
      );
      return res.data;
    },
    enabled: doctype.length > 0 && name.length > 0,
  });
}

export function useWorkflowTransition(
  doctype: string,
  name: string,
): UseMutationResult<WorkflowTransitionResponse, Error, WorkflowTransitionRequest> {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: WorkflowTransitionRequest) =>
      post<WorkflowTransitionResponse>(
        `workflow/${doctype}/${encodeURIComponent(name)}/transition`,
        req,
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["workflowState", doctype, name] });
      void qc.invalidateQueries({ queryKey: ["workflowHistory", doctype, name] });
      void qc.invalidateQueries({ queryKey: ["doc", doctype, name] });
    },
  });
}
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd desk && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add desk/src/api/types.ts desk/src/components/workflow/useWorkflow.ts
git commit -m "feat(workflow): add TypeScript types and React Query hooks for workflow API"
```

---

## Task 15: Frontend — Install Missing shadcn Components

**Files:**
- Modified by CLI: `desk/src/components/ui/alert-dialog.tsx`, `desk/src/components/ui/progress.tsx`

- [ ] **Step 1: Install AlertDialog**

Run: `cd desk && npx shadcn@latest add alert-dialog`

- [ ] **Step 2: Install Progress**

Run: `cd desk && npx shadcn@latest add progress`

- [ ] **Step 3: Verify components exist**

Run: `ls desk/src/components/ui/alert-dialog.tsx desk/src/components/ui/progress.tsx`
Expected: Both files exist

- [ ] **Step 4: Verify build**

Run: `cd desk && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add desk/src/components/ui/alert-dialog.tsx desk/src/components/ui/progress.tsx
git commit -m "feat(workflow): install AlertDialog and Progress shadcn components"
```

---

## Task 16: Frontend — WorkflowActionButton

**Files:**
- Create: `desk/src/components/workflow/WorkflowActionButton.tsx`

- [ ] **Step 1: Create WorkflowActionButton**

```tsx
import { useState } from "react";
import { CheckIcon, XIcon, Loader2Icon } from "lucide-react";
import { Button } from "../ui/button";
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogFooter,
  DialogClose,
} from "../ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTitle,
} from "../ui/alert-dialog";
import { Textarea } from "../ui/textarea";
import { Field, FieldLabel } from "../ui/field";
import type { WorkflowAvailableAction } from "../../api/types";

interface WorkflowActionButtonProps {
  action: WorkflowAvailableAction;
  onExecute: (action: string, comment: string, branch: string) => void;
  isLoading: boolean;
  doctype: string;
  name: string;
}

const DESTRUCTIVE_ACTIONS = new Set(["Reject", "Cancel", "Decline"]);

export function WorkflowActionButton({
  action,
  onExecute,
  isLoading,
  doctype,
  name,
}: WorkflowActionButtonProps) {
  const [commentOpen, setCommentOpen] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [comment, setComment] = useState("");

  const isDestructive = DESTRUCTIVE_ACTIONS.has(action.action);

  function handleClick() {
    if (action.require_comment) {
      setCommentOpen(true);
    } else if (isDestructive) {
      setConfirmOpen(true);
    } else {
      onExecute(action.action, "", action.branch_name);
    }
  }

  function handleCommentSubmit() {
    setCommentOpen(false);
    onExecute(action.action, comment, action.branch_name);
    setComment("");
  }

  function handleConfirm() {
    setConfirmOpen(false);
    onExecute(action.action, "", action.branch_name);
  }

  return (
    <>
      <Button
        variant={isDestructive ? "destructive" : "default"}
        size="sm"
        onClick={handleClick}
        disabled={isLoading}
      >
        {isLoading ? (
          <Loader2Icon data-icon="inline-start" className="animate-spin" />
        ) : isDestructive ? (
          <XIcon data-icon="inline-start" />
        ) : (
          <CheckIcon data-icon="inline-start" />
        )}
        {action.action}
      </Button>

      <Dialog open={commentOpen} onOpenChange={setCommentOpen}>
        <DialogContent>
          <DialogTitle>
            {action.action} — {doctype} {name}
          </DialogTitle>
          <DialogDescription>This action requires a comment.</DialogDescription>
          <Field>
            <FieldLabel htmlFor="workflow-comment">Comment</FieldLabel>
            <Textarea
              id="workflow-comment"
              value={comment}
              onChange={(e) => setComment(e.target.value)}
              placeholder="Enter your comment..."
            />
          </Field>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline">Cancel</Button>
            </DialogClose>
            <Button onClick={handleCommentSubmit} disabled={comment.trim() === ""}>
              Confirm
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogTitle>{action.action} {doctype}?</AlertDialogTitle>
          <AlertDialogDescription>
            This will move the document to &quot;{action.to_state}&quot; state.
          </AlertDialogDescription>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleConfirm}>
              {action.action}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd desk && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add desk/src/components/workflow/WorkflowActionButton.tsx
git commit -m "feat(workflow): add WorkflowActionButton with comment dialog and confirmation"
```

---

## Task 17: Frontend — WorkflowBranch

**Files:**
- Create: `desk/src/components/workflow/WorkflowBranch.tsx`

- [ ] **Step 1: Create WorkflowBranch**

```tsx
import { Badge } from "../ui/badge";
import { Progress } from "../ui/progress";
import { Tooltip, TooltipContent, TooltipTrigger } from "../ui/tooltip";
import { WorkflowActionButton } from "./WorkflowActionButton";
import type {
  WorkflowAvailableAction,
  WorkflowBranchStatus,
} from "../../api/types";

interface WorkflowBranchProps {
  branch: WorkflowBranchStatus;
  actions: WorkflowAvailableAction[];
  onExecute: (action: string, comment: string, branch: string) => void;
  isLoading: boolean;
  doctype: string;
  name: string;
}

const STYLE_VARIANT: Record<string, "default" | "secondary" | "destructive" | "outline"> = {
  Success: "default",
  Warning: "secondary",
  Danger: "destructive",
  Info: "outline",
};

function slaProgress(deadline: string | undefined): { value: number; label: string } | null {
  if (!deadline) return null;
  const now = Date.now();
  const dl = new Date(deadline).getTime();
  const remaining = dl - now;
  if (remaining <= 0) return { value: 100, label: "SLA breached" };
  const hours = Math.floor(remaining / 3600000);
  const minutes = Math.floor((remaining % 3600000) / 60000);
  const label = hours > 0 ? `${hours}h ${minutes}m remaining` : `${minutes}m remaining`;
  // Assume max SLA is 48h for progress calculation
  const maxMs = 48 * 3600000;
  const elapsed = maxMs - remaining;
  const value = Math.min(100, Math.max(0, (elapsed / maxMs) * 100));
  return { value, label };
}

export function WorkflowBranch({
  branch,
  actions,
  onExecute,
  isLoading,
  doctype,
  name,
}: WorkflowBranchProps) {
  const variant = STYLE_VARIANT[branch.style] ?? "outline";
  const sla = slaProgress(branch.sla_deadline);
  const branchActions = actions.filter(
    (a) => a.branch_name === branch.branch || a.branch_name === "",
  );

  return (
    <div className="flex items-center gap-2">
      {branch.branch && (
        <Badge variant="secondary">{branch.branch}</Badge>
      )}
      <Badge variant={variant}>{branch.state}</Badge>
      {sla && (
        <Tooltip>
          <TooltipTrigger asChild>
            <div className="flex items-center gap-1.5">
              <Progress value={sla.value} className="h-1.5 w-16" />
              <Badge variant={sla.value >= 100 ? "destructive" : "outline"}>
                {sla.label}
              </Badge>
            </div>
          </TooltipTrigger>
          <TooltipContent>
            Deadline: {branch.sla_deadline ? new Date(branch.sla_deadline).toLocaleString() : "None"}
          </TooltipContent>
        </Tooltip>
      )}
      <div className="ml-auto flex gap-2">
        {branchActions.map((action) => (
          <WorkflowActionButton
            key={action.action + action.branch_name}
            action={action}
            onExecute={onExecute}
            isLoading={isLoading}
            doctype={doctype}
            name={name}
          />
        ))}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd desk && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add desk/src/components/workflow/WorkflowBranch.tsx
git commit -m "feat(workflow): add WorkflowBranch component with SLA progress"
```

---

## Task 18: Frontend — WorkflowBar

**Files:**
- Create: `desk/src/components/workflow/WorkflowBar.tsx`

- [ ] **Step 1: Create WorkflowBar**

```tsx
import { Card, CardContent, CardHeader } from "../ui/card";
import { Separator } from "../ui/separator";
import { Skeleton } from "../ui/skeleton";
import { WorkflowBranch } from "./WorkflowBranch";
import { useWorkflowState, useWorkflowTransition } from "./useWorkflow";

interface WorkflowBarProps {
  doctype: string;
  name: string;
}

export function WorkflowBar({ doctype, name }: WorkflowBarProps) {
  const { data, isLoading, error } = useWorkflowState(doctype, name);
  const transition = useWorkflowTransition(doctype, name);

  if (error) return null; // No workflow or error — don't render
  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Skeleton className="h-5 w-32" />
            <div className="ml-auto flex gap-2">
              <Skeleton className="h-7 w-20" />
              <Skeleton className="h-7 w-20" />
            </div>
          </div>
        </CardHeader>
      </Card>
    );
  }

  if (!data) return null;

  const { status, actions } = data;

  function handleExecute(action: string, comment: string, branch: string) {
    transition.mutate({
      action,
      comment: comment || undefined,
      branch: branch || undefined,
    });
  }

  if (!status.is_parallel) {
    // Linear workflow — single branch
    const branch = status.branches[0];
    if (!branch) return null;
    return (
      <Card>
        <CardHeader>
          <WorkflowBranch
            branch={branch}
            actions={actions}
            onExecute={handleExecute}
            isLoading={transition.isPending}
            doctype={doctype}
            name={name}
          />
        </CardHeader>
      </Card>
    );
  }

  // Parallel workflow — multiple branches
  return (
    <Card>
      <CardContent className="flex flex-col gap-3 pt-4">
        {status.branches.map((branch, idx) => (
          <div key={branch.branch}>
            {idx > 0 && <Separator className="mb-3" />}
            <WorkflowBranch
              branch={branch}
              actions={actions}
              onExecute={handleExecute}
              isLoading={transition.isPending}
              doctype={doctype}
              name={name}
            />
          </div>
        ))}
      </CardContent>
    </Card>
  );
}
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd desk && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add desk/src/components/workflow/WorkflowBar.tsx
git commit -m "feat(workflow): add WorkflowBar component for linear and parallel workflows"
```

---

## Task 19: Frontend — WorkflowTimeline

**Files:**
- Create: `desk/src/components/workflow/WorkflowTimeline.tsx`

- [ ] **Step 1: Create WorkflowTimeline**

```tsx
import { formatDistanceToNow } from "date-fns";
import { HistoryIcon } from "lucide-react";
import { Avatar, AvatarFallback } from "../ui/avatar";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { ScrollArea } from "../ui/scroll-area";
import { Separator } from "../ui/separator";
import {
  Sheet,
  SheetContent,
  SheetTitle,
  SheetTrigger,
} from "../ui/sheet";
import { Skeleton } from "../ui/skeleton";
import { useWorkflowHistory } from "./useWorkflow";

interface WorkflowTimelineProps {
  doctype: string;
  name: string;
}

function initials(email: string): string {
  const parts = email.split("@")[0].split(/[._-]/);
  return parts
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase() ?? "")
    .join("");
}

export function WorkflowTimeline({ doctype, name }: WorkflowTimelineProps) {
  const { data, isLoading } = useWorkflowHistory(doctype, name);

  return (
    <Sheet>
      <SheetTrigger asChild>
        <Button variant="outline" size="sm">
          <HistoryIcon data-icon="inline-start" />
          Timeline
        </Button>
      </SheetTrigger>
      <SheetContent>
        <SheetTitle>Workflow Timeline</SheetTitle>
        <ScrollArea className="h-[calc(100vh-6rem)]">
          {isLoading && (
            <div className="flex flex-col gap-4 p-4">
              {[1, 2, 3].map((i) => (
                <div key={i} className="flex gap-3">
                  <Skeleton className="size-8 rounded-full" />
                  <div className="flex flex-col gap-1">
                    <Skeleton className="h-4 w-32" />
                    <Skeleton className="h-3 w-24" />
                  </div>
                </div>
              ))}
            </div>
          )}
          {data && (
            <div className="flex flex-col gap-4 p-4">
              {data.map((entry, idx) => (
                <div key={entry.id}>
                  {idx > 0 && <Separator className="mb-4" />}
                  <div className="flex gap-3">
                    <Avatar className="size-8">
                      <AvatarFallback>{initials(entry.user)}</AvatarFallback>
                    </Avatar>
                    <div className="flex flex-col gap-1">
                      <span className="text-sm font-medium">{entry.user}</span>
                      <div className="flex items-center gap-2">
                        <Badge variant="outline">{entry.action}</Badge>
                        {entry.branch_name && (
                          <Badge variant="secondary">{entry.branch_name}</Badge>
                        )}
                      </div>
                      {entry.comment && (
                        <p className="text-sm text-muted-foreground">
                          &ldquo;{entry.comment}&rdquo;
                        </p>
                      )}
                      <span className="text-xs text-muted-foreground">
                        {formatDistanceToNow(new Date(entry.timestamp), {
                          addSuffix: true,
                        })}
                      </span>
                    </div>
                  </div>
                </div>
              ))}
              {data.length === 0 && (
                <p className="text-sm text-muted-foreground">No workflow actions yet.</p>
              )}
            </div>
          )}
        </ScrollArea>
      </SheetContent>
    </Sheet>
  );
}
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd desk && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add desk/src/components/workflow/WorkflowTimeline.tsx
git commit -m "feat(workflow): add WorkflowTimeline Sheet component with action history"
```

---

## Task 20: Frontend — Integrate WorkflowBar into FormView

**Files:**
- Modify: `desk/src/pages/FormView.tsx`

- [ ] **Step 1: Add imports to FormView.tsx**

Add at the top of the imports section:

```tsx
import { WorkflowBar } from "../components/workflow/WorkflowBar";
import { WorkflowTimeline } from "../components/workflow/WorkflowTimeline";
```

- [ ] **Step 2: Add WorkflowBar between title bar and form body**

Insert after the title/button bar (after the `</div>` closing the header section, before the stale doc banner). The exact insertion point is after the header `<div>` that contains the Save/Cancel buttons (~line 258) and before the `{isStale && ...}` block (~line 261):

```tsx
{/* Workflow Bar — renders only if doctype has an active workflow */}
{!isNew && name && meta?.is_submittable && (
  <WorkflowBar doctype={doctype} name={name} />
)}
```

- [ ] **Step 3: Add WorkflowTimeline button alongside the History button**

Find the History button (~line 223-231) and add the timeline button next to it:

```tsx
{!isNew && name && meta?.is_submittable && (
  <WorkflowTimeline doctype={doctype} name={name} />
)}
```

- [ ] **Step 4: Verify TypeScript compiles**

Run: `cd desk && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 5: Start dev server and verify visually**

Run: `cd desk && npm run dev`
Navigate to a document form. Verify:
- WorkflowBar appears if the doctype is submittable
- Timeline button opens the Sheet panel
- Loading skeletons appear while data loads

- [ ] **Step 6: Commit**

```bash
git add desk/src/pages/FormView.tsx
git commit -m "feat(workflow): integrate WorkflowBar and WorkflowTimeline into FormView"
```

---

## Task 21: Full Test Suite Run

**Files:** None (verification only)

- [ ] **Step 1: Run all Go tests**

Run: `make test`
Expected: All tests PASS

- [ ] **Step 2: Run Go linter**

Run: `make lint`
Expected: No errors

- [ ] **Step 3: Run frontend type check**

Run: `cd desk && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 4: Build all binaries**

Run: `make build`
Expected: All 5 binaries build successfully

- [ ] **Step 5: Commit any fixes if needed**

If any test or lint issues were found, fix and commit:

```bash
git add -A
git commit -m "fix(workflow): resolve test/lint issues from full suite run"
```

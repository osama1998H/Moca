package workflow

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/events"
	"github.com/osama1998H/moca/pkg/meta"
)

// TransitionOpts carries optional parameters for a workflow transition.
type TransitionOpts struct {
	Comment    string
	BranchName string
}

// WorkflowStatus describes the current state of a document's workflow,
// including all active branches.
type WorkflowStatus struct {
	WorkflowName string
	Branches     []BranchStatus
	IsParallel   bool
}

// BranchStatus describes the state of a single branch within a workflow.
type BranchStatus struct {
	EnteredAt    time.Time
	SLADeadline  *time.Time
	BranchName   string
	CurrentState string
	Style        string
	IsActive     bool
}

// AvailableAction describes a single action the current user can take
// on a document in its current workflow state.
type AvailableAction struct {
	Action         string
	ToState        string
	BranchName     string
	Style          string
	RequireComment bool
}

// AutoActionHandler is a callback invoked after a transition when the
// transition's AutoAction field matches a registered handler name.
type AutoActionHandler func(ctx *document.DocContext, doc document.Document) error

// WorkflowEngine is the central workflow state machine. It validates transitions,
// checks role-based permissions, evaluates guard conditions, applies state updates,
// executes auto-actions, and publishes transition events.
//
// For parallel workflows (AND-split/join), the engine tracks per-document branch
// states in branchStates. When a fork state is entered, branches are created.
// Each branch transitions independently. When all branches for a join target
// complete, the join state is auto-activated.
type WorkflowEngine struct {
	registry    *WorkflowRegistry
	evaluator   *ConditionEvaluator
	emitter     *events.Emitter
	autoActions map[string]AutoActionHandler
	logger      *slog.Logger

	// branchStates tracks active parallel branches per document.
	// Key: "site:doctype:docname" -> map[branchName]*BranchStatus
	branchStates map[string]map[string]*BranchStatus
	bsMu         sync.RWMutex
}

// branchStateKey returns the canonical key for branch state lookups.
func branchStateKey(site, doctype, docname string) string {
	return site + ":" + doctype + ":" + docname
}

// EngineOption is a functional option for configuring a WorkflowEngine.
type EngineOption func(*WorkflowEngine)

// WithRegistry sets the workflow registry on the engine.
func WithRegistry(r *WorkflowRegistry) EngineOption {
	return func(e *WorkflowEngine) {
		e.registry = r
	}
}

// WithEvaluator sets the condition evaluator on the engine.
func WithEvaluator(ev *ConditionEvaluator) EngineOption {
	return func(e *WorkflowEngine) {
		e.evaluator = ev
	}
}

// WithEmitter sets the event emitter on the engine.
func WithEmitter(em *events.Emitter) EngineOption {
	return func(e *WorkflowEngine) {
		e.emitter = em
	}
}

// WithLogger sets the structured logger on the engine.
func WithLogger(l *slog.Logger) EngineOption {
	return func(e *WorkflowEngine) {
		e.logger = l
	}
}

// NewWorkflowEngine constructs a WorkflowEngine with the given options.
// Defaults are used for any option not explicitly provided:
//   - registry: new empty WorkflowRegistry
//   - evaluator: new ConditionEvaluator
//   - logger: slog.Default()
func NewWorkflowEngine(opts ...EngineOption) *WorkflowEngine {
	e := &WorkflowEngine{
		autoActions:  make(map[string]AutoActionHandler),
		branchStates: make(map[string]map[string]*BranchStatus),
		logger:       slog.Default(),
	}
	for _, opt := range opts {
		opt(e)
	}
	// Apply defaults for nil fields.
	if e.registry == nil {
		e.registry = NewWorkflowRegistry()
	}
	if e.evaluator == nil {
		e.evaluator = NewConditionEvaluator()
	}
	return e
}

// RegisterAutoAction registers a named handler that is invoked after a
// transition when the transition's AutoAction field matches name.
func (e *WorkflowEngine) RegisterAutoAction(name string, handler AutoActionHandler) {
	e.autoActions[name] = handler
}

// Transition executes a workflow transition on doc. It validates the action
// against the current state, checks role permissions, evaluates guard
// conditions, requires comments when configured, applies state changes,
// and publishes a transition event.
//
// Transition flow:
//  1. Get workflow from registry (site from ctx.Site.Name, doctype from doc.Meta().Name)
//  2. Get current state from doc.Get("workflow_state") -- default to first state if empty
//  3. FindTransition(wf, currentState, action, opts.BranchName) -- error ErrInvalidAction if nil
//  4. Check roles: user must have at least one of tr.AllowedRoles (skip if empty)
//  5. Evaluate condition: evaluator.Eval(tr.Condition, doc, ctx) -- error ErrConditionFailed if false
//  6. Check comment: if tr.RequireComment && opts.Comment == "" -> error ErrCommentRequired
//  7. Apply: doc.Set("workflow_state", tr.To)
//  8. If toState.DocStatus > 0: doc.Set("docstatus", toState.DocStatus)
//  9. If toState.UpdateField != "": doc.Set(toState.UpdateField, toState.UpdateValue)
//  10. Execute auto-action if registered
//  11. Publish event via emitter
func (e *WorkflowEngine) Transition(ctx *document.DocContext, doc document.Document, action string, opts TransitionOpts) error {
	// Step 1: Get workflow from registry.
	site := e.siteName(ctx)
	doctype := doc.Meta().Name

	wf, err := e.registry.Get(ctx, site, doctype)
	if err != nil {
		return err
	}

	// Determine the branch state key for parallel tracking.
	bsKey := branchStateKey(site, doctype, doc.Name())

	// Branch-aware transition: if BranchName is set, delegate to branch transition logic.
	if opts.BranchName != "" {
		return e.transitionBranch(ctx, doc, wf, bsKey, action, opts)
	}

	// Step 2: Get current state; default to first state if empty.
	currentState := e.currentState(doc, wf)

	// Step 3: Find the matching transition.
	tr := FindTransition(wf, currentState, action, opts.BranchName)
	if tr == nil {
		return fmt.Errorf("%w: action %q not valid from state %q", ErrInvalidAction, action, currentState)
	}

	// Step 4: Check roles.
	if err := e.checkRoles(ctx, tr); err != nil {
		return err
	}

	// Step 5: Evaluate condition.
	if tr.Condition != "" {
		ok, err := e.evaluator.Eval(tr.Condition, doc, ctx)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrConditionFailed, err)
		}
		if !ok {
			return fmt.Errorf("%w: condition %q evaluated to false", ErrConditionFailed, tr.Condition)
		}
	}

	// Step 6: Check comment requirement.
	if tr.RequireComment && opts.Comment == "" {
		return ErrCommentRequired
	}

	// Step 7: Apply workflow_state.
	if err := doc.Set("workflow_state", tr.To); err != nil {
		return fmt.Errorf("workflow: failed to set workflow_state: %w", err)
	}

	// Step 8: Check if the target state is a fork — create parallel branches.
	toState := FindState(wf, tr.To)
	if toState != nil && toState.IsFork {
		e.initBranches(wf, bsKey)
		e.publishEvent(ctx, doc, wf, action, currentState, tr.To, opts)
		return nil
	}

	// Step 9: Set docstatus if target state specifies one.
	if toState != nil {
		if toState.DocStatus > 0 {
			if err := doc.Set("docstatus", toState.DocStatus); err != nil {
				return fmt.Errorf("workflow: failed to set docstatus: %w", err)
			}
		}

		// Step 10: Set update field if specified.
		if toState.UpdateField != "" {
			if err := doc.Set(toState.UpdateField, toState.UpdateValue); err != nil {
				return fmt.Errorf("workflow: failed to set update field %q: %w", toState.UpdateField, err)
			}
		}
	}

	// Step 11: Execute auto-action if registered.
	if tr.AutoAction != "" {
		if handler, ok := e.autoActions[tr.AutoAction]; ok {
			if err := handler(ctx, doc); err != nil {
				return fmt.Errorf("workflow: auto-action %q failed: %w", tr.AutoAction, err)
			}
		}
	}

	// Step 12: Publish event.
	e.publishEvent(ctx, doc, wf, action, currentState, tr.To, opts)

	return nil
}

// initBranches creates branch trackers for a fork state. For each unique
// BranchName among states with a JoinTarget, the first state (by order in
// wf.States) with that BranchName is the initial state for that branch.
func (e *WorkflowEngine) initBranches(wf *meta.WorkflowMeta, bsKey string) {
	e.bsMu.Lock()
	defer e.bsMu.Unlock()

	branches := make(map[string]*BranchStatus)
	for _, s := range wf.States {
		if s.BranchName == "" || s.JoinTarget == "" {
			continue
		}
		// Only take the first state per branch (initial state).
		if _, exists := branches[s.BranchName]; !exists {
			branches[s.BranchName] = &BranchStatus{
				BranchName:   s.BranchName,
				CurrentState: s.Name,
				Style:        s.Style,
				IsActive:     true,
				EnteredAt:    time.Now(),
			}
		}
	}
	e.branchStates[bsKey] = branches
}

// transitionBranch handles a transition within a specific parallel branch.
// It looks up the branch's current state, finds the matching transition,
// validates it, applies the state change, and checks for join completion.
func (e *WorkflowEngine) transitionBranch(
	ctx *document.DocContext,
	doc document.Document,
	wf *meta.WorkflowMeta,
	bsKey, action string,
	opts TransitionOpts,
) error {
	e.bsMu.Lock()
	defer e.bsMu.Unlock()

	docBranches, ok := e.branchStates[bsKey]
	if !ok {
		return fmt.Errorf("%w: no active branches for this document", ErrBranchNotFound)
	}

	bs, ok := docBranches[opts.BranchName]
	if !ok {
		return fmt.Errorf("%w: branch %q not found", ErrBranchNotFound, opts.BranchName)
	}
	if !bs.IsActive {
		return fmt.Errorf("%w: branch %q is no longer active", ErrInvalidAction, opts.BranchName)
	}

	// Find transition from the branch's current state (not doc.workflow_state).
	branchCurrentState := bs.CurrentState
	tr := FindTransition(wf, branchCurrentState, action, opts.BranchName)
	if tr == nil {
		return fmt.Errorf("%w: action %q not valid from branch state %q", ErrInvalidAction, action, branchCurrentState)
	}

	// Check roles.
	if err := e.checkRoles(ctx, tr); err != nil {
		return err
	}

	// Evaluate condition.
	if tr.Condition != "" {
		ok, err := e.evaluator.Eval(tr.Condition, doc, ctx)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrConditionFailed, err)
		}
		if !ok {
			return fmt.Errorf("%w: condition %q evaluated to false", ErrConditionFailed, tr.Condition)
		}
	}

	// Check comment requirement.
	if tr.RequireComment && opts.Comment == "" {
		return ErrCommentRequired
	}

	// Update branch state.
	bs.CurrentState = tr.To
	if toState := FindState(wf, tr.To); toState != nil {
		bs.Style = toState.Style
	}
	bs.EnteredAt = time.Now()

	// Determine if this branch is terminal: no further transitions from the new state
	// within this branch.
	if e.isBranchTerminal(wf, tr.To, opts.BranchName) {
		bs.IsActive = false
	}

	// Execute auto-action if registered.
	if tr.AutoAction != "" {
		if handler, found := e.autoActions[tr.AutoAction]; found {
			if err := handler(ctx, doc); err != nil {
				return fmt.Errorf("workflow: auto-action %q failed: %w", tr.AutoAction, err)
			}
		}
	}

	// Publish event for the branch transition.
	e.publishEvent(ctx, doc, wf, action, branchCurrentState, tr.To, opts)

	// Check join: if ALL branches for the same join target are inactive, auto-activate the join.
	joinTarget := e.findJoinTarget(wf, opts.BranchName)
	if joinTarget != "" && e.allBranchesComplete(docBranches, joinTarget, wf) {
		return e.activateJoin(ctx, doc, wf, bsKey, joinTarget)
	}

	return nil
}

// isBranchTerminal returns true if there are no outgoing transitions from the
// given state that belong to the same branch (i.e., the state is terminal
// within this branch).
func (e *WorkflowEngine) isBranchTerminal(wf *meta.WorkflowMeta, stateName, branchName string) bool {
	for i := range wf.Transitions {
		tr := &wf.Transitions[i]
		if tr.From != stateName {
			continue
		}
		// Check if the target state is in the same branch.
		toState := FindState(wf, tr.To)
		if toState != nil && toState.BranchName == branchName {
			return false // There is a further transition within this branch.
		}
	}
	return true
}

// findJoinTarget returns the JoinTarget for a given branch name by looking at
// the workflow states.
func (e *WorkflowEngine) findJoinTarget(wf *meta.WorkflowMeta, branchName string) string {
	for _, s := range wf.States {
		if s.BranchName == branchName && s.JoinTarget != "" {
			return s.JoinTarget
		}
	}
	return ""
}

// allBranchesComplete returns true if all branches that share the given join
// target are inactive (complete).
func (e *WorkflowEngine) allBranchesComplete(docBranches map[string]*BranchStatus, joinTarget string, wf *meta.WorkflowMeta) bool {
	// Find all branch names that target this join.
	targetBranches := make(map[string]bool)
	for _, s := range wf.States {
		if s.JoinTarget == joinTarget && s.BranchName != "" {
			targetBranches[s.BranchName] = true
		}
	}

	for branchName := range targetBranches {
		bs, ok := docBranches[branchName]
		if !ok || bs.IsActive {
			return false
		}
	}
	return true
}

// activateJoin auto-activates the join state when all branches are complete.
// It sets the doc's workflow_state to the join target, applies DocStatus and
// UpdateField from the join state, and clears the branch tracking.
func (e *WorkflowEngine) activateJoin(
	ctx *document.DocContext,
	doc document.Document,
	wf *meta.WorkflowMeta,
	bsKey, joinTarget string,
) error {
	// Set workflow_state to the join target.
	if err := doc.Set("workflow_state", joinTarget); err != nil {
		return fmt.Errorf("workflow: failed to set workflow_state to join %q: %w", joinTarget, err)
	}

	// Apply join state properties.
	joinState := FindState(wf, joinTarget)
	if joinState != nil {
		if joinState.DocStatus > 0 {
			if err := doc.Set("docstatus", joinState.DocStatus); err != nil {
				return fmt.Errorf("workflow: failed to set docstatus on join: %w", err)
			}
		}
		if joinState.UpdateField != "" {
			if err := doc.Set(joinState.UpdateField, joinState.UpdateValue); err != nil {
				return fmt.Errorf("workflow: failed to set update field %q on join: %w", joinState.UpdateField, err)
			}
		}
	}

	// Clear branch states for this document.
	delete(e.branchStates, bsKey)

	return nil
}

// CanTransition checks whether a transition is valid without executing it.
// Returns (true, nil) if the user can perform the action from the current state,
// (false, nil) if blocked by role/condition/etc., or (false, error) for
// infrastructure failures.
func (e *WorkflowEngine) CanTransition(ctx *document.DocContext, doc document.Document, action string) (bool, error) {
	site := e.siteName(ctx)
	doctype := doc.Meta().Name

	wf, err := e.registry.Get(ctx, site, doctype)
	if err != nil {
		return false, err
	}

	currentState := e.currentState(doc, wf)

	tr := FindTransition(wf, currentState, action, "")
	if tr == nil {
		return false, nil
	}

	// Check roles.
	if err := e.checkRoles(ctx, tr); err != nil {
		return false, nil
	}

	// Evaluate condition.
	if tr.Condition != "" {
		ok, err := e.evaluator.Eval(tr.Condition, doc, ctx)
		if err != nil {
			return false, nil
		}
		if !ok {
			return false, nil
		}
	}

	return true, nil
}

// GetState returns the current workflow status for a document, including
// all branch states for parallel workflows.
func (e *WorkflowEngine) GetState(ctx *document.DocContext, doc document.Document) (*WorkflowStatus, error) {
	site := e.siteName(ctx)
	doctype := doc.Meta().Name

	wf, err := e.registry.Get(ctx, site, doctype)
	if err != nil {
		return nil, err
	}

	currentState := e.currentState(doc, wf)
	stateObj := FindState(wf, currentState)

	// Check if there are active parallel branches for this document.
	bsKey := branchStateKey(site, doctype, doc.Name())
	e.bsMu.RLock()
	docBranches, hasActiveBranches := e.branchStates[bsKey]
	e.bsMu.RUnlock()

	if hasActiveBranches && len(docBranches) > 0 {
		// Parallel workflow with active branch tracking.
		status := &WorkflowStatus{
			WorkflowName: wf.Name,
			IsParallel:   true,
		}
		branches := make([]BranchStatus, 0, len(docBranches))
		for _, bs := range docBranches {
			branches = append(branches, *bs)
		}
		status.Branches = branches
		return status, nil
	}

	// No active branches — either linear workflow or branches already joined.
	status := &WorkflowStatus{
		WorkflowName: wf.Name,
		IsParallel:   false,
	}

	branch := BranchStatus{
		BranchName:   "main",
		CurrentState: currentState,
		IsActive:     true,
		EnteredAt:    time.Now(),
	}
	if stateObj != nil {
		branch.Style = stateObj.Style
	}
	status.Branches = []BranchStatus{branch}

	return status, nil
}

// GetAvailableActions returns the list of actions the current user can perform
// on the document from its current workflow state. For parallel workflows with
// active branches, actions from each active branch's current state are included
// with the BranchName field set.
func (e *WorkflowEngine) GetAvailableActions(ctx *document.DocContext, doc document.Document) ([]AvailableAction, error) {
	site := e.siteName(ctx)
	doctype := doc.Meta().Name

	wf, err := e.registry.Get(ctx, site, doctype)
	if err != nil {
		return nil, err
	}

	// Check for active parallel branches.
	bsKey := branchStateKey(site, doctype, doc.Name())
	e.bsMu.RLock()
	docBranches, hasActiveBranches := e.branchStates[bsKey]
	e.bsMu.RUnlock()

	if hasActiveBranches && len(docBranches) > 0 {
		return e.getParallelActions(ctx, doc, wf, docBranches)
	}

	// Linear workflow: use the document's current state.
	currentState := e.currentState(doc, wf)
	return e.getActionsForState(ctx, doc, wf, currentState, "")
}

// getParallelActions returns available actions across all active branches.
func (e *WorkflowEngine) getParallelActions(
	ctx *document.DocContext,
	doc document.Document,
	wf *meta.WorkflowMeta,
	docBranches map[string]*BranchStatus,
) ([]AvailableAction, error) {
	var actions []AvailableAction
	for _, bs := range docBranches {
		if !bs.IsActive {
			continue
		}
		branchActions, err := e.getActionsForState(ctx, doc, wf, bs.CurrentState, bs.BranchName)
		if err != nil {
			return nil, err
		}
		actions = append(actions, branchActions...)
	}
	return actions, nil
}

// getActionsForState returns available actions for a given state and optional branch.
func (e *WorkflowEngine) getActionsForState(
	ctx *document.DocContext,
	doc document.Document,
	wf *meta.WorkflowMeta,
	currentState, branchName string,
) ([]AvailableAction, error) {
	var actions []AvailableAction
	for i := range wf.Transitions {
		tr := &wf.Transitions[i]
		if tr.From != currentState {
			continue
		}

		// Check role access.
		if err := e.checkRoles(ctx, tr); err != nil {
			continue
		}

		// Evaluate condition (skip actions whose conditions fail).
		if tr.Condition != "" {
			ok, err := e.evaluator.Eval(tr.Condition, doc, ctx)
			if err != nil || !ok {
				continue
			}
		}

		// Look up target state for style info.
		var style string
		if toState := FindState(wf, tr.To); toState != nil {
			style = toState.Style
		}

		actions = append(actions, AvailableAction{
			Action:         tr.Action,
			ToState:        tr.To,
			BranchName:     branchName,
			RequireComment: tr.RequireComment,
			Style:          style,
		})
	}
	return actions, nil
}

// checkRoles verifies that the context user has at least one of the transition's
// allowed roles. If AllowedRoles is empty, the check passes for any user.
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

// currentState returns the document's current workflow state. If the field is
// empty or nil, defaults to the first state defined in the workflow.
func (e *WorkflowEngine) currentState(doc document.Document, wf *meta.WorkflowMeta) string {
	if v := doc.Get("workflow_state"); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	// Default to the first state.
	if len(wf.States) > 0 {
		return wf.States[0].Name
	}
	return ""
}

// siteName extracts the site name from the document context.
func (e *WorkflowEngine) siteName(ctx *document.DocContext) string {
	if ctx != nil && ctx.Site != nil {
		return ctx.Site.Name
	}
	return ""
}

// publishEvent publishes a workflow transition event via the emitter.
// If no emitter is configured, the call is silently skipped.
func (e *WorkflowEngine) publishEvent(
	ctx *document.DocContext,
	doc document.Document,
	wf *meta.WorkflowMeta,
	action, fromState, toState string,
	opts TransitionOpts,
) {
	if e.emitter == nil {
		return
	}

	var user string
	if ctx != nil && ctx.User != nil {
		user = ctx.User.Email
	}

	var requestID string
	if ctx != nil {
		requestID = ctx.RequestID
	}

	evt := NewWorkflowEvent(
		EventTypeTransition,
		e.siteName(ctx),
		doc.Meta().Name,
		doc.Name(),
		wf.Name,
		action,
		fromState,
		toState,
		opts.BranchName,
		user,
		opts.Comment,
		requestID,
	)

	e.emitter.Emit(events.TopicWorkflowTransitions, evt)
}


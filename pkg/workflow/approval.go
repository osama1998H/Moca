package workflow

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/osama1998H/moca/pkg/meta"
)

// ActionRecord records a single workflow approval action taken by a user.
type ActionRecord struct {
	Timestamp     time.Time
	ID            string
	ReferenceType string
	ReferenceName string
	WorkflowName  string
	Action        string // "Approve", "Reject", "Submit", "Delegate"
	FromState     string
	ToState       string
	BranchName    string
	User          string
	Comment       string
}

// QuorumResult describes the outcome of a quorum check for a workflow transition.
type QuorumResult struct {
	ApprovedBy  []string
	PendingFrom []string
	Required    int
	Received    int
	IsMet       bool
}

// ApprovalManager is an in-memory tracker for workflow approval actions.
// It records actions and evaluates whether quorum requirements are met.
type ApprovalManager struct {
	// actions maps "doctype:docname" -> slice of recorded actions.
	actions map[string][]ActionRecord
	mu      sync.RWMutex
}

// NewApprovalManager returns an initialised ApprovalManager.
func NewApprovalManager() *ApprovalManager {
	return &ApprovalManager{
		actions: make(map[string][]ActionRecord),
	}
}

// actionKey returns the canonical map key for a doctype/docname pair.
func actionKey(doctype, docname string) string {
	return doctype + ":" + docname
}

// RecordAction records an approval action and returns the persisted ActionRecord.
// A UUID is generated and the Timestamp is set to time.Now().
func (a *ApprovalManager) RecordAction(doctype, docname, fromState, action, branchName, user, comment string) ActionRecord {
	rec := ActionRecord{
		ID:            uuid.NewString(),
		ReferenceType: doctype,
		ReferenceName: docname,
		FromState:     fromState,
		Action:        action,
		BranchName:    branchName,
		User:          user,
		Comment:       comment,
		Timestamp:     time.Now(),
	}

	key := actionKey(doctype, docname)
	a.mu.Lock()
	a.actions[key] = append(a.actions[key], rec)
	a.mu.Unlock()

	return rec
}

// CheckQuorum evaluates whether the required number of matching approvals has
// been reached for the given transition. If tr.QuorumCount <= 0 it defaults to 1.
func (a *ApprovalManager) CheckQuorum(doctype, docname, fromState, action, branchName string, tr *meta.Transition) QuorumResult {
	required := tr.QuorumCount
	if required <= 0 {
		required = 1
	}

	key := actionKey(doctype, docname)
	a.mu.RLock()
	recs := a.actions[key]
	a.mu.RUnlock()

	var approvedBy []string
	for _, r := range recs {
		if r.FromState == fromState && r.Action == action && r.BranchName == branchName {
			approvedBy = append(approvedBy, r.User)
		}
	}

	received := len(approvedBy)
	return QuorumResult{
		Required:   required,
		Received:   received,
		IsMet:      received >= required,
		ApprovedBy: approvedBy,
	}
}

// HasAlreadyActed returns true if the user has already recorded the specified
// action from the given state on the given branch.
func (a *ApprovalManager) HasAlreadyActed(doctype, docname, fromState, action, branchName, user string) bool {
	key := actionKey(doctype, docname)
	a.mu.RLock()
	recs := a.actions[key]
	a.mu.RUnlock()

	for _, r := range recs {
		if r.User == user && r.FromState == fromState && r.Action == action && r.BranchName == branchName {
			return true
		}
	}
	return false
}

// Delegate records a Delegate action from fromUser, with a comment naming toUser.
func (a *ApprovalManager) Delegate(doctype, docname, fromUser, toUser string) {
	a.RecordAction(doctype, docname, "", "Delegate", "", fromUser, "Delegated to "+toUser)
}

// GetActions returns all recorded actions for the given doctype/docname.
func (a *ApprovalManager) GetActions(doctype, docname string) []ActionRecord {
	key := actionKey(doctype, docname)
	a.mu.RLock()
	recs := a.actions[key]
	a.mu.RUnlock()

	if len(recs) == 0 {
		return nil
	}
	// Return a copy to prevent external mutation.
	out := make([]ActionRecord, len(recs))
	copy(out, recs)
	return out
}

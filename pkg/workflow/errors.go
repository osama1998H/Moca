package workflow

import "errors"

var (
	ErrNoActiveWorkflow  = errors.New("workflow: no active workflow for this doctype")
	ErrTransitionBlocked = errors.New("workflow: transition blocked")
	ErrNoPermission      = errors.New("workflow: user does not have required role")
	ErrConditionFailed   = errors.New("workflow: transition condition not met")
	ErrCommentRequired   = errors.New("workflow: comment required for this transition")
	ErrInvalidAction     = errors.New("workflow: invalid action for current state")
	ErrQuorumPending     = errors.New("workflow: approval recorded, quorum pending")
	ErrAlreadyApproved   = errors.New("workflow: user has already approved this transition")
	ErrInvalidCondition  = errors.New("workflow: invalid condition expression")
	ErrBranchNotFound    = errors.New("workflow: branch not found")
	ErrSLABreached       = errors.New("workflow: SLA deadline exceeded")
)

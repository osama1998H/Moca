package workflow

import (
	"time"

	"github.com/google/uuid"

	"github.com/osama1998H/moca/pkg/events"
)

// Workflow event type constants. Each constant identifies a distinct step in
// the workflow lifecycle and is used as the EventType field of WorkflowEvent.
const (
	EventTypeTransition   = "workflow.transition"
	EventTypeFork         = "workflow.fork"
	EventTypeJoin         = "workflow.join"
	EventTypeQuorumVote   = "workflow.quorum.vote"
	EventTypeQuorumMet    = "workflow.quorum.met"
	EventTypeSLAStarted   = "workflow.sla.started"
	EventTypeSLABreached  = "workflow.sla.breached"
	EventTypeSLACancelled = "workflow.sla.cancelled"
	EventTypeDelegated    = "workflow.delegated"
)

// WorkflowEvent is the canonical event envelope published to
// events.TopicWorkflowTransitions for every workflow state change.
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

// NewWorkflowEvent constructs a WorkflowEvent with a freshly generated UUID
// EventID and a UTC timestamp. Source is set to events.EventSourceMocaCore.
func NewWorkflowEvent(
	eventType, site, docType, docName, workflowName,
	action, fromState, toState, branchName, user, comment, requestID string,
) WorkflowEvent {
	return WorkflowEvent{
		EventID:      uuid.NewString(),
		EventType:    eventType,
		Timestamp:    time.Now().UTC(),
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

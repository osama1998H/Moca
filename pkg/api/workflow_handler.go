package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/workflow"
)

// WorkflowHandler serves workflow-related API endpoints including transitions,
// state queries, history, and pending approvals.
type WorkflowHandler struct {
	engine     *workflow.WorkflowEngine
	approvals  *workflow.ApprovalManager
	docManager *document.DocManager
	registry   *meta.Registry
	logger     *slog.Logger
}

// NewWorkflowHandler creates a WorkflowHandler with the given dependencies.
func NewWorkflowHandler(
	engine *workflow.WorkflowEngine,
	approvals *workflow.ApprovalManager,
	docManager *document.DocManager,
	registry *meta.Registry,
	logger *slog.Logger,
) *WorkflowHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &WorkflowHandler{
		engine:     engine,
		approvals:  approvals,
		docManager: docManager,
		registry:   registry,
		logger:     logger,
	}
}

// RegisterRoutes registers workflow endpoints on the mux.
func (h *WorkflowHandler) RegisterRoutes(mux *http.ServeMux, version string) {
	p := "/api/" + version
	mux.HandleFunc("POST "+p+"/workflow/{doctype}/{name}/transition", h.handleTransition)
	mux.HandleFunc("GET "+p+"/workflow/{doctype}/{name}/state", h.handleGetState)
	mux.HandleFunc("GET "+p+"/workflow/{doctype}/{name}/history", h.handleGetHistory)
	mux.HandleFunc("GET "+p+"/workflow/pending", h.handleGetPending)
}

// transitionRequest is the expected JSON body for POST .../transition.
type transitionRequest struct {
	Action  string `json:"action"`
	Comment string `json:"comment"`
	Branch  string `json:"branch"`
}

// transitionResponse is the JSON response for a successful transition.
type transitionResponse struct {
	State  *workflow.WorkflowStatus `json:"state"`
	Status string                  `json:"status"`
}

// stateResponse is the JSON response for GET .../state.
type stateResponse struct {
	State   *workflow.WorkflowStatus   `json:"state"`
	Actions []workflow.AvailableAction `json:"actions"`
}

// handleTransition executes a workflow transition on the specified document.
func (h *WorkflowHandler) handleTransition(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	site := SiteFromContext(r.Context())
	if site == nil {
		writeError(w, http.StatusBadRequest, "NO_SITE", "site context required")
		return
	}

	doctype := r.PathValue("doctype")
	name := r.PathValue("name")

	var req transitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
		return
	}
	if req.Action == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "action field is required")
		return
	}

	// Build DocContext from API context.
	docCtx := newDocContext(r.Context(), site, user)

	// Load the document.
	if h.docManager == nil {
		h.logger.Error("workflow transition failed: docManager is nil")
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "document manager not configured")
		return
	}
	doc, err := h.docManager.Get(docCtx, doctype, name)
	if err != nil {
		h.logger.Error("workflow transition: load document failed",
			slog.String("doctype", doctype),
			slog.String("name", name),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusNotFound, "NOT_FOUND", "document not found")
		return
	}

	// Record the prior state for the approval action record.
	priorState := ""
	if v := doc.Get("workflow_state"); v != nil {
		priorState, _ = v.(string)
	}

	// Execute the transition.
	err = h.engine.Transition(docCtx, doc, req.Action, workflow.TransitionOpts{
		Comment:    req.Comment,
		BranchName: req.Branch,
	})
	if err != nil {
		h.handleWorkflowError(w, err, doctype, name)
		return
	}

	// Get the new state after transition.
	state, err := h.engine.GetState(docCtx, doc)
	if err != nil {
		h.logger.Error("workflow transition: get state after transition failed",
			slog.String("doctype", doctype),
			slog.String("name", name),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "transition succeeded but failed to retrieve new state")
		return
	}

	// Record the approval action.
	if h.approvals != nil {
		h.approvals.RecordAction(doctype, name, priorState, req.Action, req.Branch, user.Email, req.Comment)
	}

	writeSuccess(w, http.StatusOK, transitionResponse{
		Status: "transitioned",
		State:  state,
	})
}

// handleGetState returns the current workflow state and available actions for a document.
func (h *WorkflowHandler) handleGetState(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	site := SiteFromContext(r.Context())
	if site == nil {
		writeError(w, http.StatusBadRequest, "NO_SITE", "site context required")
		return
	}

	doctype := r.PathValue("doctype")
	name := r.PathValue("name")

	docCtx := newDocContext(r.Context(), site, user)

	// Load the document.
	if h.docManager == nil {
		h.logger.Error("workflow get state failed: docManager is nil")
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "document manager not configured")
		return
	}
	doc, err := h.docManager.Get(docCtx, doctype, name)
	if err != nil {
		h.logger.Error("workflow get state: load document failed",
			slog.String("doctype", doctype),
			slog.String("name", name),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusNotFound, "NOT_FOUND", "document not found")
		return
	}

	state, err := h.engine.GetState(docCtx, doc)
	if err != nil {
		h.handleWorkflowError(w, err, doctype, name)
		return
	}

	actions, err := h.engine.GetAvailableActions(docCtx, doc)
	if err != nil {
		h.logger.Error("workflow get state: get available actions failed",
			slog.String("doctype", doctype),
			slog.String("name", name),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to retrieve available actions")
		return
	}
	if actions == nil {
		actions = []workflow.AvailableAction{}
	}

	writeSuccess(w, http.StatusOK, stateResponse{
		State:   state,
		Actions: actions,
	})
}

// handleGetHistory returns the workflow action history for a document.
func (h *WorkflowHandler) handleGetHistory(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	site := SiteFromContext(r.Context())
	if site == nil {
		writeError(w, http.StatusBadRequest, "NO_SITE", "site context required")
		return
	}

	doctype := r.PathValue("doctype")
	name := r.PathValue("name")

	_ = site // site validated above; used for tenant context in future persistence

	records := h.approvals.GetActions(doctype, name)
	if records == nil {
		records = []workflow.ActionRecord{}
	}

	writeSuccess(w, http.StatusOK, records)
}

// handleGetPending returns pending workflow items for the authenticated user.
// Currently returns an empty array; full implementation requires cross-document
// querying which is deferred.
func (h *WorkflowHandler) handleGetPending(w http.ResponseWriter, _ *http.Request) {
	writeSuccess(w, http.StatusOK, []any{})
}

// handleWorkflowError maps workflow package errors to appropriate HTTP status codes.
func (h *WorkflowHandler) handleWorkflowError(w http.ResponseWriter, err error, doctype, name string) {
	switch {
	case errors.Is(err, workflow.ErrNoActiveWorkflow):
		writeError(w, http.StatusNotFound, "NO_WORKFLOW", "no active workflow for this doctype")

	case errors.Is(err, workflow.ErrTransitionBlocked):
		writeError(w, http.StatusForbidden, "TRANSITION_BLOCKED", err.Error())

	case errors.Is(err, workflow.ErrNoPermission):
		writeError(w, http.StatusForbidden, "NO_PERMISSION", err.Error())

	case errors.Is(err, workflow.ErrConditionFailed):
		writeError(w, http.StatusBadRequest, "CONDITION_FAILED", err.Error())

	case errors.Is(err, workflow.ErrCommentRequired):
		writeError(w, http.StatusBadRequest, "COMMENT_REQUIRED", "a comment is required for this transition")

	case errors.Is(err, workflow.ErrInvalidAction):
		writeError(w, http.StatusBadRequest, "INVALID_ACTION", err.Error())

	case errors.Is(err, workflow.ErrQuorumPending):
		writeError(w, http.StatusAccepted, "QUORUM_PENDING", "approval recorded, waiting for quorum")

	case errors.Is(err, workflow.ErrBranchNotFound):
		writeError(w, http.StatusBadRequest, "BRANCH_NOT_FOUND", err.Error())

	default:
		h.logger.Error("workflow operation failed",
			slog.String("doctype", doctype),
			slog.String("name", name),
			slog.String("error", err.Error()),
		)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "workflow operation failed")
	}
}

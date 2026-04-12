package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/tenancy"
	"github.com/osama1998H/moca/pkg/workflow"
)

// ── Mock DocLoader ──────────────────────────────────────────────────────────

// mockDocLoader implements DocLoader for unit tests. It stores documents keyed
// by "doctype:name" and returns them on Get.
type mockDocLoader struct {
	docs map[string]*document.DynamicDoc
}

func newMockDocLoader() *mockDocLoader {
	return &mockDocLoader{docs: make(map[string]*document.DynamicDoc)}
}

func (m *mockDocLoader) Put(doctype, name string, doc *document.DynamicDoc) {
	m.docs[doctype+":"+name] = doc
}

func (m *mockDocLoader) Get(_ *document.DocContext, doctype, name string) (*document.DynamicDoc, error) {
	doc, ok := m.docs[doctype+":"+name]
	if !ok {
		return nil, fmt.Errorf("document %s/%s not found", doctype, name)
	}
	return doc, nil
}

// ── Mock DocSaver ──────────────────────────────────────────────────────────

// mockDocSaver implements DocSaver for unit tests. It records the values passed
// to Update keyed by "doctype:name".
type mockDocSaver struct {
	updated map[string]map[string]any
}

func newMockDocSaver() *mockDocSaver {
	return &mockDocSaver{updated: make(map[string]map[string]any)}
}

func (m *mockDocSaver) Update(_ *document.DocContext, doctype, name string, values map[string]any) (*document.DynamicDoc, error) {
	m.updated[doctype+":"+name] = values
	return nil, nil
}

// ── Helper to build an authenticated request context ────────────────────────

func workflowCtx(t *testing.T, siteName, email string, roles []string) context.Context {
	t.Helper()
	ctx := context.Background()
	ctx = WithUser(ctx, &auth.User{Email: email, Roles: roles})
	ctx = WithSite(ctx, &tenancy.SiteContext{Name: siteName})
	ctx = WithRequestID(ctx, "test-req-1")
	return ctx
}

// ── Task MetaType fixture ───────────────────────────────────────────────────

func taskMetaType() *meta.MetaType {
	return &meta.MetaType{
		Name:   "Task",
		Module: "Projects",
		Fields: []meta.FieldDef{
			{Name: "subject", FieldType: meta.FieldTypeData},
			{Name: "workflow_state", FieldType: meta.FieldTypeData},
		},
	}
}

// ── Route Registration ─────────────────────────────────────────────────────

func TestWorkflowHandler_RegisterRoutes(t *testing.T) {
	mux := http.NewServeMux()
	h := NewWorkflowHandler(nil, nil, nil, nil, nil, slog.Default())
	h.RegisterRoutes(mux, "v1")

	routes := []struct{ method, path string }{
		{"POST", "/api/v1/workflow/Task/T-001/transition"},
		{"GET", "/api/v1/workflow/Task/T-001/state"},
		{"GET", "/api/v1/workflow/Task/T-001/history"},
		{"GET", "/api/v1/workflow/pending"},
	}
	for _, r := range routes {
		req := httptest.NewRequest(r.method, r.path, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code == http.StatusNotFound {
			t.Errorf("route %s %s not registered (got 404)", r.method, r.path)
		}
	}
}

// ── Auth / Site Guard Tests ────────────────────────────────────────────────

func TestWorkflowHandler_HandleTransition_NoAuth(t *testing.T) {
	engine := workflow.NewWorkflowEngine()
	approvals := workflow.NewApprovalManager()
	h := NewWorkflowHandler(engine, approvals, nil, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	body := `{"action":"Approve"}`
	req := httptest.NewRequest("POST", "/api/v1/workflow/Task/T-001/transition", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestWorkflowHandler_HandleGetState_NoAuth(t *testing.T) {
	h := NewWorkflowHandler(nil, nil, nil, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	req := httptest.NewRequest("GET", "/api/v1/workflow/Task/T-001/state", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestWorkflowHandler_HandleGetHistory_NoAuth(t *testing.T) {
	h := NewWorkflowHandler(nil, nil, nil, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	req := httptest.NewRequest("GET", "/api/v1/workflow/Task/T-001/history", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestWorkflowHandler_GetState_NoSite(t *testing.T) {
	h := NewWorkflowHandler(nil, nil, nil, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	ctx := context.Background()
	ctx = WithUser(ctx, &auth.User{Email: "test@test.com"})

	req := httptest.NewRequest("GET", "/api/v1/workflow/Task/T-001/state", nil)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing site, got %d", rec.Code)
	}
}

func TestWorkflowHandler_HandleTransition_NoSite(t *testing.T) {
	engine := workflow.NewWorkflowEngine()
	approvals := workflow.NewApprovalManager()
	h := NewWorkflowHandler(engine, approvals, nil, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	ctx := context.Background()
	ctx = WithUser(ctx, &auth.User{Email: "test@test.com"})

	body := `{"action":"Approve"}`
	req := httptest.NewRequest("POST", "/api/v1/workflow/Task/T-001/transition", bytes.NewBufferString(body))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing site, got %d", rec.Code)
	}
}

// ── Pending (stub) ─────────────────────────────────────────────────────────

func TestWorkflowHandler_HandleGetPending(t *testing.T) {
	h := NewWorkflowHandler(nil, nil, nil, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	req := httptest.NewRequest("GET", "/api/v1/workflow/pending", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp successEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	items, ok := resp.Data.([]any)
	if !ok {
		t.Fatalf("expected data to be array, got %T", resp.Data)
	}
	if len(items) != 0 {
		t.Errorf("expected empty array, got %d items", len(items))
	}
}

// ── Validation Tests ───────────────────────────────────────────────────────

func TestWorkflowHandler_HandleTransition_InvalidJSON(t *testing.T) {
	engine := workflow.NewWorkflowEngine()
	approvals := workflow.NewApprovalManager()
	h := NewWorkflowHandler(engine, approvals, nil, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	ctx := workflowCtx(t, "site1", "test@test.com", []string{"System Manager"})
	req := httptest.NewRequest("POST", "/api/v1/workflow/Task/T-001/transition", bytes.NewBufferString("not json"))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestWorkflowHandler_HandleTransition_MissingAction(t *testing.T) {
	engine := workflow.NewWorkflowEngine()
	approvals := workflow.NewApprovalManager()
	h := NewWorkflowHandler(engine, approvals, nil, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	ctx := workflowCtx(t, "site1", "test@test.com", []string{"System Manager"})
	body := `{"comment":"some comment"}`
	req := httptest.NewRequest("POST", "/api/v1/workflow/Task/T-001/transition", bytes.NewBufferString(body))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing action, got %d", rec.Code)
	}
}

func TestWorkflowHandler_HandleTransition_NilDocLoader(t *testing.T) {
	engine := workflow.NewWorkflowEngine()
	approvals := workflow.NewApprovalManager()
	h := NewWorkflowHandler(engine, approvals, nil, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	ctx := workflowCtx(t, "site1", "test@test.com", []string{"System Manager"})
	body := `{"action":"Approve","comment":"LGTM"}`
	req := httptest.NewRequest("POST", "/api/v1/workflow/Task/T-001/transition", bytes.NewBufferString(body))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 with nil doc loader, got %d", rec.Code)
	}
}

// ── History Tests ──────────────────────────────────────────────────────────

func TestWorkflowHandler_HandleHistory_WithRecords(t *testing.T) {
	approvals := workflow.NewApprovalManager()
	approvals.RecordAction("Task", "T-001", "Open", "Approve", "", "admin@test.com", "Looks good")
	approvals.RecordAction("Task", "T-001", "Approved", "Submit", "", "admin@test.com", "")

	h := NewWorkflowHandler(nil, approvals, nil, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	ctx := workflowCtx(t, "site1", "admin@test.com", []string{"System Manager"})
	req := httptest.NewRequest("GET", "/api/v1/workflow/Task/T-001/history", nil)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp successEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	items, ok := resp.Data.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", resp.Data)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 history records, got %d", len(items))
	}
}

func TestWorkflowHandler_HandleHistory_Empty(t *testing.T) {
	approvals := workflow.NewApprovalManager()
	h := NewWorkflowHandler(nil, approvals, nil, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	ctx := workflowCtx(t, "site1", "admin@test.com", []string{"System Manager"})
	req := httptest.NewRequest("GET", "/api/v1/workflow/Task/T-001/history", nil)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp successEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	items, ok := resp.Data.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", resp.Data)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 history records, got %d", len(items))
	}
}

// ── Full Integration: Transition with Mock DocLoader ───────────────────────

func TestWorkflowHandler_FullTransitionFlow(t *testing.T) {
	// 1. Set up workflow definition.
	wfRegistry := workflow.NewWorkflowRegistry()
	wfRegistry.Set("site1", "Task", &meta.WorkflowMeta{
		Name:     "Task Approval",
		DocType:  "Task",
		IsActive: true,
		States: []meta.WorkflowState{
			{Name: "Open", Style: "warning"},
			{Name: "Approved", Style: "success", DocStatus: 1},
		},
		Transitions: []meta.Transition{
			{From: "Open", To: "Approved", Action: "Approve"},
		},
	})

	engine := workflow.NewWorkflowEngine(
		workflow.WithRegistry(wfRegistry),
		workflow.WithLogger(slog.Default()),
	)
	approvals := workflow.NewApprovalManager()

	// 2. Create a mock doc loader with a Task document.
	mt := taskMetaType()
	doc := document.NewDynamicDoc(mt, nil, false)
	_ = doc.Set("name", "T-001")
	_ = doc.Set("workflow_state", "Open")

	loader := newMockDocLoader()
	loader.Put("Task", "T-001", doc)

	// 3. Build handler and mux.
	h := NewWorkflowHandler(engine, approvals, loader, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	// 4. Execute the transition via HTTP.
	ctx := workflowCtx(t, "site1", "manager@test.com", []string{"System Manager"})
	body := `{"action":"Approve","comment":"Looks good to me"}`
	req := httptest.NewRequest("POST", "/api/v1/workflow/Task/T-001/transition", bytes.NewBufferString(body))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// 5. Verify HTTP 200.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// 6. Parse and verify the response.
	var resp successEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	dataMap, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be object, got %T", resp.Data)
	}
	if dataMap["status"] != "transitioned" {
		t.Errorf("expected status 'transitioned', got %v", dataMap["status"])
	}

	// 7. Verify the state in the response.
	stateObj, ok := dataMap["state"].(map[string]any)
	if !ok {
		t.Fatalf("expected state to be object, got %T", dataMap["state"])
	}
	if stateObj["workflow_name"] != "Task Approval" {
		t.Errorf("expected workflow_name 'Task Approval', got %v", stateObj["workflow_name"])
	}

	// 8. Verify the document was actually updated.
	currentState := doc.Get("workflow_state")
	if currentState != "Approved" {
		t.Errorf("expected document workflow_state to be 'Approved', got %v", currentState)
	}

	// 9. Verify approval action was recorded.
	actions := approvals.GetActions("Task", "T-001")
	if len(actions) != 1 {
		t.Fatalf("expected 1 approval record, got %d", len(actions))
	}
	if actions[0].Action != "Approve" {
		t.Errorf("expected action 'Approve', got %s", actions[0].Action)
	}
	if actions[0].User != "manager@test.com" {
		t.Errorf("expected user 'manager@test.com', got %s", actions[0].User)
	}
	if actions[0].Comment != "Looks good to me" {
		t.Errorf("expected comment 'Looks good to me', got %s", actions[0].Comment)
	}
	if actions[0].FromState != "Open" {
		t.Errorf("expected fromState 'Open', got %s", actions[0].FromState)
	}
}

// TestWorkflowHandler_TransitionPersistsState verifies that the transition
// endpoint calls DocSaver.Update with the new workflow_state.
func TestWorkflowHandler_TransitionPersistsState(t *testing.T) {
	wfRegistry := workflow.NewWorkflowRegistry()
	wfRegistry.Set("site1", "Task", &meta.WorkflowMeta{
		Name:     "Task Approval",
		DocType:  "Task",
		IsActive: true,
		States: []meta.WorkflowState{
			{Name: "Open", Style: "warning"},
			{Name: "Approved", Style: "success", DocStatus: 1},
		},
		Transitions: []meta.Transition{
			{From: "Open", To: "Approved", Action: "Approve"},
		},
	})

	engine := workflow.NewWorkflowEngine(
		workflow.WithRegistry(wfRegistry),
		workflow.WithLogger(slog.Default()),
	)
	approvals := workflow.NewApprovalManager()

	mt := taskMetaType()
	doc := document.NewDynamicDoc(mt, nil, false)
	_ = doc.Set("name", "T-001")
	_ = doc.Set("workflow_state", "Open")

	loader := newMockDocLoader()
	loader.Put("Task", "T-001", doc)

	saver := newMockDocSaver()

	h := NewWorkflowHandler(engine, approvals, loader, saver, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	ctx := workflowCtx(t, "site1", "manager@test.com", []string{"System Manager"})
	body := `{"action":"Approve"}`
	req := httptest.NewRequest("POST", "/api/v1/workflow/Task/T-001/transition", bytes.NewBufferString(body))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Verify that saver.Update was called with the correct values.
	saved, ok := saver.updated["Task:T-001"]
	if !ok {
		t.Fatal("expected DocSaver.Update to be called for Task:T-001")
	}
	if saved["workflow_state"] != "Approved" {
		t.Errorf("expected saved workflow_state 'Approved', got %v", saved["workflow_state"])
	}
}

// ── Full Integration: Get State with Mock DocLoader ────────────────────────

func TestWorkflowHandler_FullGetStateFlow(t *testing.T) {
	wfRegistry := workflow.NewWorkflowRegistry()
	wfRegistry.Set("site1", "Task", &meta.WorkflowMeta{
		Name:     "Task Approval",
		DocType:  "Task",
		IsActive: true,
		States: []meta.WorkflowState{
			{Name: "Open", Style: "warning"},
			{Name: "Approved", Style: "success", DocStatus: 1},
		},
		Transitions: []meta.Transition{
			{From: "Open", To: "Approved", Action: "Approve"},
		},
	})

	engine := workflow.NewWorkflowEngine(
		workflow.WithRegistry(wfRegistry),
		workflow.WithLogger(slog.Default()),
	)

	mt := taskMetaType()
	doc := document.NewDynamicDoc(mt, nil, false)
	_ = doc.Set("name", "T-002")
	_ = doc.Set("workflow_state", "Open")

	loader := newMockDocLoader()
	loader.Put("Task", "T-002", doc)

	h := NewWorkflowHandler(engine, workflow.NewApprovalManager(), loader, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	ctx := workflowCtx(t, "site1", "user@test.com", []string{"System Manager"})
	req := httptest.NewRequest("GET", "/api/v1/workflow/Task/T-002/state", nil)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp successEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	dataMap, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be object, got %T", resp.Data)
	}

	// Verify state.
	stateObj, ok := dataMap["state"].(map[string]any)
	if !ok {
		t.Fatalf("expected state to be object, got %T", dataMap["state"])
	}
	branches, ok := stateObj["branches"].([]any)
	if !ok || len(branches) == 0 {
		t.Fatalf("expected branches array with at least 1 entry")
	}
	branch, ok := branches[0].(map[string]any)
	if !ok {
		t.Fatalf("expected branch to be object, got %T", branches[0])
	}
	if branch["current_state"] != "Open" {
		t.Errorf("expected current_state 'Open', got %v", branch["current_state"])
	}

	// Verify available actions.
	actionsRaw, ok := dataMap["actions"].([]any)
	if !ok {
		t.Fatalf("expected actions to be array, got %T", dataMap["actions"])
	}
	if len(actionsRaw) != 1 {
		t.Fatalf("expected 1 available action, got %d", len(actionsRaw))
	}
	actionObj, ok := actionsRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("expected action to be object, got %T", actionsRaw[0])
	}
	if actionObj["action"] != "Approve" {
		t.Errorf("expected action 'Approve', got %v", actionObj["action"])
	}
	if actionObj["to_state"] != "Approved" {
		t.Errorf("expected to_state 'Approved', got %v", actionObj["to_state"])
	}
}

// ── Error Mapping Tests ────────────────────────────────────────────────────

func TestWorkflowHandler_TransitionError_NoWorkflow(t *testing.T) {
	// Engine with empty registry -- no workflow defined for this doctype.
	engine := workflow.NewWorkflowEngine(
		workflow.WithLogger(slog.Default()),
	)
	approvals := workflow.NewApprovalManager()

	mt := taskMetaType()
	doc := document.NewDynamicDoc(mt, nil, false)
	_ = doc.Set("name", "T-003")

	loader := newMockDocLoader()
	loader.Put("Task", "T-003", doc)

	h := NewWorkflowHandler(engine, approvals, loader, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	ctx := workflowCtx(t, "site1", "user@test.com", []string{"System Manager"})
	body := `{"action":"Approve"}`
	req := httptest.NewRequest("POST", "/api/v1/workflow/Task/T-003/transition", bytes.NewBufferString(body))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// Should return 404 because no workflow is configured.
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for no active workflow, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestWorkflowHandler_TransitionError_InvalidAction(t *testing.T) {
	wfRegistry := workflow.NewWorkflowRegistry()
	wfRegistry.Set("site1", "Task", &meta.WorkflowMeta{
		Name:     "Task Approval",
		DocType:  "Task",
		IsActive: true,
		States: []meta.WorkflowState{
			{Name: "Open", Style: "warning"},
			{Name: "Approved", Style: "success"},
		},
		Transitions: []meta.Transition{
			{From: "Open", To: "Approved", Action: "Approve"},
		},
	})

	engine := workflow.NewWorkflowEngine(
		workflow.WithRegistry(wfRegistry),
		workflow.WithLogger(slog.Default()),
	)
	approvals := workflow.NewApprovalManager()

	mt := taskMetaType()
	doc := document.NewDynamicDoc(mt, nil, false)
	_ = doc.Set("name", "T-004")
	_ = doc.Set("workflow_state", "Open")

	loader := newMockDocLoader()
	loader.Put("Task", "T-004", doc)

	h := NewWorkflowHandler(engine, approvals, loader, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	ctx := workflowCtx(t, "site1", "user@test.com", []string{"System Manager"})
	// "Reject" is not a valid action from "Open".
	body := `{"action":"Reject"}`
	req := httptest.NewRequest("POST", "/api/v1/workflow/Task/T-004/transition", bytes.NewBufferString(body))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid action, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestWorkflowHandler_TransitionError_CommentRequired(t *testing.T) {
	wfRegistry := workflow.NewWorkflowRegistry()
	wfRegistry.Set("site1", "Task", &meta.WorkflowMeta{
		Name:     "Task Approval",
		DocType:  "Task",
		IsActive: true,
		States: []meta.WorkflowState{
			{Name: "Open", Style: "warning"},
			{Name: "Approved", Style: "success"},
		},
		Transitions: []meta.Transition{
			{From: "Open", To: "Approved", Action: "Approve", RequireComment: true},
		},
	})

	engine := workflow.NewWorkflowEngine(
		workflow.WithRegistry(wfRegistry),
		workflow.WithLogger(slog.Default()),
	)
	approvals := workflow.NewApprovalManager()

	mt := taskMetaType()
	doc := document.NewDynamicDoc(mt, nil, false)
	_ = doc.Set("name", "T-005")
	_ = doc.Set("workflow_state", "Open")

	loader := newMockDocLoader()
	loader.Put("Task", "T-005", doc)

	h := NewWorkflowHandler(engine, approvals, loader, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	ctx := workflowCtx(t, "site1", "user@test.com", []string{"System Manager"})
	// No comment provided for a transition that requires one.
	body := `{"action":"Approve"}`
	req := httptest.NewRequest("POST", "/api/v1/workflow/Task/T-005/transition", bytes.NewBufferString(body))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for comment required, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestWorkflowHandler_TransitionError_NoPermission(t *testing.T) {
	wfRegistry := workflow.NewWorkflowRegistry()
	wfRegistry.Set("site1", "Task", &meta.WorkflowMeta{
		Name:     "Task Approval",
		DocType:  "Task",
		IsActive: true,
		States: []meta.WorkflowState{
			{Name: "Open", Style: "warning"},
			{Name: "Approved", Style: "success"},
		},
		Transitions: []meta.Transition{
			{From: "Open", To: "Approved", Action: "Approve", AllowedRoles: []string{"Director"}},
		},
	})

	engine := workflow.NewWorkflowEngine(
		workflow.WithRegistry(wfRegistry),
		workflow.WithLogger(slog.Default()),
	)
	approvals := workflow.NewApprovalManager()

	mt := taskMetaType()
	doc := document.NewDynamicDoc(mt, nil, false)
	_ = doc.Set("name", "T-006")
	_ = doc.Set("workflow_state", "Open")

	loader := newMockDocLoader()
	loader.Put("Task", "T-006", doc)

	h := NewWorkflowHandler(engine, approvals, loader, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	// User has "Employee" role but transition requires "Director".
	ctx := workflowCtx(t, "site1", "employee@test.com", []string{"Employee"})
	body := `{"action":"Approve"}`
	req := httptest.NewRequest("POST", "/api/v1/workflow/Task/T-006/transition", bytes.NewBufferString(body))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for no permission, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestWorkflowHandler_TransitionError_DocNotFound(t *testing.T) {
	engine := workflow.NewWorkflowEngine(
		workflow.WithLogger(slog.Default()),
	)
	approvals := workflow.NewApprovalManager()

	// Empty loader -- doc does not exist.
	loader := newMockDocLoader()

	h := NewWorkflowHandler(engine, approvals, loader, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	ctx := workflowCtx(t, "site1", "user@test.com", []string{"System Manager"})
	body := `{"action":"Approve"}`
	req := httptest.NewRequest("POST", "/api/v1/workflow/Task/T-999/transition", bytes.NewBufferString(body))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing document, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

// ── Multi-step Transition Test ─────────────────────────────────────────────

func TestWorkflowHandler_MultiStepTransition(t *testing.T) {
	wfRegistry := workflow.NewWorkflowRegistry()
	wfRegistry.Set("site1", "Task", &meta.WorkflowMeta{
		Name:     "Task Lifecycle",
		DocType:  "Task",
		IsActive: true,
		States: []meta.WorkflowState{
			{Name: "Draft", Style: "info"},
			{Name: "Open", Style: "warning"},
			{Name: "Closed", Style: "success", DocStatus: 1},
		},
		Transitions: []meta.Transition{
			{From: "Draft", To: "Open", Action: "Submit"},
			{From: "Open", To: "Closed", Action: "Close"},
		},
	})

	engine := workflow.NewWorkflowEngine(
		workflow.WithRegistry(wfRegistry),
		workflow.WithLogger(slog.Default()),
	)
	approvals := workflow.NewApprovalManager()

	mt := taskMetaType()
	doc := document.NewDynamicDoc(mt, nil, false)
	_ = doc.Set("name", "T-010")
	_ = doc.Set("workflow_state", "Draft")

	loader := newMockDocLoader()
	loader.Put("Task", "T-010", doc)

	h := NewWorkflowHandler(engine, approvals, loader, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, "v1")

	ctx := workflowCtx(t, "site1", "user@test.com", []string{"System Manager"})

	// Step 1: Draft -> Open
	body := `{"action":"Submit"}`
	req := httptest.NewRequest("POST", "/api/v1/workflow/Task/T-010/transition", bytes.NewBufferString(body))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("step 1: expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	if doc.Get("workflow_state") != "Open" {
		t.Fatalf("step 1: expected state 'Open', got %v", doc.Get("workflow_state"))
	}

	// Step 2: Open -> Closed
	body = `{"action":"Close","comment":"Done"}`
	req = httptest.NewRequest("POST", "/api/v1/workflow/Task/T-010/transition", bytes.NewBufferString(body))
	req = req.WithContext(ctx)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("step 2: expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	if doc.Get("workflow_state") != "Closed" {
		t.Fatalf("step 2: expected state 'Closed', got %v", doc.Get("workflow_state"))
	}

	// Verify approval history has 2 records.
	actions := approvals.GetActions("Task", "T-010")
	if len(actions) != 2 {
		t.Fatalf("expected 2 approval records, got %d", len(actions))
	}
	if actions[0].Action != "Submit" {
		t.Errorf("expected first action 'Submit', got %s", actions[0].Action)
	}
	if actions[1].Action != "Close" {
		t.Errorf("expected second action 'Close', got %s", actions[1].Action)
	}

	// Step 3: verify state endpoint shows Closed with no available actions.
	req = httptest.NewRequest("GET", "/api/v1/workflow/Task/T-010/state", nil)
	req = req.WithContext(ctx)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("state check: expected 200, got %d", rec.Code)
	}

	var stateResp successEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&stateResp); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	stateData, ok := stateResp.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected state data to be object, got %T", stateResp.Data)
	}
	actionsArr, ok := stateData["actions"].([]any)
	if !ok {
		t.Fatalf("expected actions to be array, got %T", stateData["actions"])
	}
	if len(actionsArr) != 0 {
		t.Errorf("expected 0 available actions after final state, got %d", len(actionsArr))
	}
}

// ── DocLoader interface compliance ─────────────────────────────────────────

// Verify *document.DocManager satisfies DocLoader at compile time.
var _ DocLoader = (*document.DocManager)(nil)

// ── DocContext construction ────────────────────────────────────────────────

func TestNewDocContextFromAPI(t *testing.T) {
	user := &auth.User{Email: "test@test.com", Roles: []string{"Admin"}}
	site := &tenancy.SiteContext{Name: "testsite"}

	ctx := context.Background()
	ctx = WithUser(ctx, user)
	ctx = WithSite(ctx, site)
	ctx = WithRequestID(ctx, "req-123")

	docCtx := newDocContext(ctx, site, user)
	if docCtx.User.Email != "test@test.com" {
		t.Errorf("expected user email test@test.com, got %s", docCtx.User.Email)
	}
	if docCtx.Site.Name != "testsite" {
		t.Errorf("expected site name testsite, got %s", docCtx.Site.Name)
	}
	if docCtx.RequestID != "req-123" {
		t.Errorf("expected request ID req-123, got %s", docCtx.RequestID)
	}
}

// Verify error import is used (compile guard).
var _ = errors.New

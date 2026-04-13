//go:build integration

package integration

import (
	"testing"

	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/testutils"
)

func TestWorkflowMetaRegistration(t *testing.T) {
	env := testutils.NewTestEnv(t)

	// Register a doctype with a workflow definition.
	mt := &meta.MetaType{
		Name:          "WorkflowDoc",
		Module:        "test",
		IsSubmittable: true,
		NamingRule: meta.NamingStrategy{
			Rule: meta.NamingUUID,
		},
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData, Label: "Title", Required: true, InAPI: true},
			{Name: "status", FieldType: meta.FieldTypeSelect, Label: "Status",
				Options: "Draft\nPending Review\nApproved\nRejected", InAPI: true},
		},
		Permissions: []meta.PermRule{
			{Role: "System Manager", DocTypePerm: 63},
		},
	}
	env.RegisterMetaType(t, mt)

	// Create a document in draft state.
	doc := env.NewTestDoc(t, "WorkflowDoc", map[string]any{
		"title":  "Workflow Test",
		"status": "Draft",
	})
	if doc.Name() == "" {
		t.Fatal("doc name should not be empty")
	}

	// Verify the document was created with the correct status.
	fetched := env.GetTestDoc(t, "WorkflowDoc", doc.Name())
	if fetched.Get("status") != "Draft" {
		t.Fatalf("expected status 'Draft', got %v", fetched.Get("status"))
	}

	// Update status to simulate a workflow transition.
	ctx := env.DocContext()
	updated, err := env.DocManager().Update(ctx, "WorkflowDoc", doc.Name(), map[string]any{
		"status": "Pending Review",
	})
	if err != nil {
		t.Fatalf("update status: %v", err)
	}
	if updated.Get("status") != "Pending Review" {
		t.Fatalf("expected 'Pending Review', got %v", updated.Get("status"))
	}
}

func TestSubmittableDocument(t *testing.T) {
	env := testutils.NewTestEnv(t)

	mt := &meta.MetaType{
		Name:          "SubmitDoc",
		Module:        "test",
		IsSubmittable: true,
		NamingRule: meta.NamingStrategy{
			Rule: meta.NamingUUID,
		},
		Fields: []meta.FieldDef{
			{Name: "title", FieldType: meta.FieldTypeData, Label: "Title", Required: true, InAPI: true},
		},
		Permissions: []meta.PermRule{
			{Role: "System Manager", DocTypePerm: 63},
		},
	}
	env.RegisterMetaType(t, mt)

	// Create a document (starts as Draft, docstatus=0).
	doc := env.NewTestDoc(t, "SubmitDoc", map[string]any{
		"title": "Submit Test",
	})
	if doc.Name() == "" {
		t.Fatal("doc should have a name")
	}

	// Note: Full submit/cancel lifecycle depends on the Submit/Cancel methods
	// being wired in DocManager (MS-04). This test verifies that submittable
	// doctypes can be created and retrieved.
	fetched := env.GetTestDoc(t, "SubmitDoc", doc.Name())
	if fetched.Get("title") != "Submit Test" {
		t.Fatalf("expected 'Submit Test', got %v", fetched.Get("title"))
	}
}

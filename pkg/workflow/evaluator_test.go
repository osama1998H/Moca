package workflow

import (
	"context"
	"testing"

	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
)

// orderJSON is a minimal MetaType for Sales Order documents used in evaluator tests.
const orderJSON = `{
	"name": "SalesOrder",
	"module": "test",
	"naming_rule": {"rule": "uuid"},
	"fields": [
		{"name": "grand_total", "field_type": "Float", "label": "Grand Total"},
		{"name": "status",      "field_type": "Data",  "label": "Status"}
	]
}`

// mustCompileOrder compiles the test MetaType or fails the test.
func mustCompileOrder(t *testing.T) *meta.MetaType {
	t.Helper()
	mt, err := meta.Compile([]byte(orderJSON))
	if err != nil {
		t.Fatalf("mustCompileOrder: %v", err)
	}
	return mt
}

// newOrderDoc creates a DynamicDoc for SalesOrder with the given grand_total value.
func newOrderDoc(t *testing.T, grandTotal float64) document.Document {
	t.Helper()
	mt := mustCompileOrder(t)
	doc := document.NewDynamicDoc(mt, nil, true)
	if err := doc.Set("grand_total", grandTotal); err != nil {
		t.Fatalf("newOrderDoc: Set grand_total: %v", err)
	}
	return doc
}

// newDocCtx creates a DocContext with the given user email and roles.
func newDocCtx(email string, roles []string) *document.DocContext {
	user := &auth.User{
		Email: email,
		Roles: roles,
	}
	return document.NewDocContext(context.Background(), nil, user)
}

func TestConditionEvaluator_SimpleComparison(t *testing.T) {
	e := NewConditionEvaluator()
	doc := newOrderDoc(t, 5000)
	ctx := newDocCtx("user@example.com", []string{"User"})

	result, err := e.Eval("doc.grand_total > 1000", doc, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected true for doc.grand_total=5000 > 1000, got false")
	}
}

func TestConditionEvaluator_FalseCondition(t *testing.T) {
	e := NewConditionEvaluator()
	doc := newOrderDoc(t, 500)
	ctx := newDocCtx("user@example.com", []string{"User"})

	result, err := e.Eval("doc.grand_total > 1000", doc, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result {
		t.Error("expected false for doc.grand_total=500 > 1000, got true")
	}
}

func TestConditionEvaluator_HasRole(t *testing.T) {
	e := NewConditionEvaluator()
	doc := newOrderDoc(t, 0)
	ctx := newDocCtx("finance@example.com", []string{"Finance Manager", "User"})

	result, err := e.Eval(`has_role("Finance Manager")`, doc, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected true for has_role(\"Finance Manager\") with roles=[Finance Manager, User], got false")
	}
}

func TestConditionEvaluator_HasRole_False(t *testing.T) {
	e := NewConditionEvaluator()
	doc := newOrderDoc(t, 0)
	ctx := newDocCtx("user@example.com", []string{"User"})

	result, err := e.Eval(`has_role("Finance Manager")`, doc, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result {
		t.Error("expected false for has_role(\"Finance Manager\") with roles=[User], got true")
	}
}

func TestConditionEvaluator_EmptyCondition(t *testing.T) {
	e := NewConditionEvaluator()
	doc := newOrderDoc(t, 0)
	ctx := newDocCtx("user@example.com", nil)

	result, err := e.Eval("", doc, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected true for empty condition, got false")
	}
}

func TestConditionEvaluator_InvalidExpression(t *testing.T) {
	e := NewConditionEvaluator()
	doc := newOrderDoc(t, 0)
	ctx := newDocCtx("user@example.com", nil)

	_, err := e.Eval("invalid %%% syntax", doc, ctx)
	if err == nil {
		t.Fatal("expected error for invalid expression, got nil")
	}
}

func TestConditionEvaluator_CachesCompiled(t *testing.T) {
	e := NewConditionEvaluator()
	doc := newOrderDoc(t, 5000)
	ctx := newDocCtx("user@example.com", []string{"User"})

	condition := "doc.grand_total > 1000"

	// Evaluate the same expression twice.
	_, err := e.Eval(condition, doc, ctx)
	if err != nil {
		t.Fatalf("first eval error: %v", err)
	}
	_, err = e.Eval(condition, doc, ctx)
	if err != nil {
		t.Fatalf("second eval error: %v", err)
	}

	// Verify exactly one entry in the cache.
	e.mu.RLock()
	cacheLen := len(e.cache)
	e.mu.RUnlock()

	if cacheLen != 1 {
		t.Errorf("expected 1 cached program, got %d", cacheLen)
	}
}

func TestConditionEvaluator_NilCtx(t *testing.T) {
	e := NewConditionEvaluator()
	doc := newOrderDoc(t, 5000)

	// nil context should not panic; user/roles should be treated as empty.
	result, err := e.Eval("doc.grand_total > 1000", doc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected true for doc.grand_total=5000 > 1000 with nil ctx, got false")
	}
}

package hooks

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestResolveOrder_EmptySlice(t *testing.T) {
	got, err := resolveOrder(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestResolveOrder_SingleHandler(t *testing.T) {
	h := PrioritizedHandler{Priority: 100, AppName: "app"}
	got, err := resolveOrder([]PrioritizedHandler{h})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].AppName != "app" || got[0].Priority != 100 {
		t.Fatalf("expected single handler returned unchanged, got %+v", got)
	}
}

func TestResolveOrder_PriorityOnly_NoDeps(t *testing.T) {
	handlers := []PrioritizedHandler{
		{Priority: 300, AppName: "c"},
		{Priority: 100, AppName: "a"},
		{Priority: 200, AppName: "b"},
	}

	got, err := resolveOrder(handlers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"a", "b", "c"}
	assertAppOrder(t, got, want)
}

func TestResolveOrder_DependencyOverridesPriority(t *testing.T) {
	handlers := []PrioritizedHandler{
		{Priority: 100, AppName: "a", DependsOn: []string{"b"}},
		{Priority: 200, AppName: "b"},
	}

	got, err := resolveOrder(handlers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// "b" must come first despite higher priority number because "a" depends on "b".
	want := []string{"b", "a"}
	assertAppOrder(t, got, want)
}

func TestResolveOrder_DiamondDependency(t *testing.T) {
	handlers := []PrioritizedHandler{
		{Priority: 500, AppName: "top", DependsOn: []string{"left", "right"}},
		{Priority: 100, AppName: "left", DependsOn: []string{"base"}},
		{Priority: 200, AppName: "right", DependsOn: []string{"base"}},
		{Priority: 500, AppName: "base"},
	}

	got, err := resolveOrder(handlers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 4 {
		t.Fatalf("expected 4 handlers, got %d", len(got))
	}
	// "base" must be first, "top" must be last.
	if got[0].AppName != "base" {
		t.Errorf("expected base first, got %s", got[0].AppName)
	}
	if got[3].AppName != "top" {
		t.Errorf("expected top last, got %s", got[3].AppName)
	}
	// "left" (p=100) before "right" (p=200) due to priority tie-breaking.
	if got[1].AppName != "left" || got[2].AppName != "right" {
		t.Errorf("expected left then right, got %s then %s", got[1].AppName, got[2].AppName)
	}
}

func TestResolveOrder_TieBreaking_AlphabeticalAppName(t *testing.T) {
	handlers := []PrioritizedHandler{
		{Priority: 500, AppName: "zebra"},
		{Priority: 500, AppName: "alpha"},
		{Priority: 500, AppName: "middle"},
	}

	got, err := resolveOrder(handlers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"alpha", "middle", "zebra"}
	assertAppOrder(t, got, want)
}

func TestResolveOrder_CircularDependency_TwoNodes(t *testing.T) {
	handlers := []PrioritizedHandler{
		{Priority: 100, AppName: "a", DependsOn: []string{"b"}},
		{Priority: 100, AppName: "b", DependsOn: []string{"a"}},
	}

	_, err := resolveOrder(handlers)
	if err == nil {
		t.Fatal("expected error for circular dependency")
	}

	var cycleErr *CircularDependencyError
	if !errors.As(err, &cycleErr) {
		t.Fatalf("expected CircularDependencyError, got %T: %v", err, err)
	}
	if len(cycleErr.Cycle) < 2 {
		t.Errorf("expected cycle with at least 2 entries, got %v", cycleErr.Cycle)
	}
}

func TestResolveOrder_CircularDependency_ThreeNodes(t *testing.T) {
	handlers := []PrioritizedHandler{
		{Priority: 100, AppName: "a", DependsOn: []string{"c"}},
		{Priority: 100, AppName: "b", DependsOn: []string{"a"}},
		{Priority: 100, AppName: "c", DependsOn: []string{"b"}},
	}

	_, err := resolveOrder(handlers)
	if err == nil {
		t.Fatal("expected error for circular dependency")
	}

	var cycleErr *CircularDependencyError
	if !errors.As(err, &cycleErr) {
		t.Fatalf("expected CircularDependencyError, got %T: %v", err, err)
	}
}

func TestResolveOrder_CircularDependency_ErrorMessage(t *testing.T) {
	handlers := []PrioritizedHandler{
		{Priority: 100, AppName: "x", DependsOn: []string{"y"}},
		{Priority: 100, AppName: "y", DependsOn: []string{"x"}},
	}

	_, err := resolveOrder(handlers)
	if err == nil {
		t.Fatal("expected error")
	}

	msg := err.Error()
	if !strings.Contains(msg, "circular dependency detected") {
		t.Errorf("error message should contain 'circular dependency detected', got: %s", msg)
	}
	if !strings.Contains(msg, "->") {
		t.Errorf("error message should contain '->' cycle path, got: %s", msg)
	}
}

func TestResolveOrder_MissingDependency_Ignored(t *testing.T) {
	handlers := []PrioritizedHandler{
		{Priority: 100, AppName: "a", DependsOn: []string{"nonexistent"}},
		{Priority: 200, AppName: "b"},
	}

	got, err := resolveOrder(handlers)
	if err != nil {
		t.Fatalf("missing dependency should be silently ignored: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(got))
	}
	// "a" has lower priority and no effective dependency, so it should come first.
	want := []string{"a", "b"}
	assertAppOrder(t, got, want)
}

func TestResolveOrder_AnonymousHandlers_Independent(t *testing.T) {
	handlers := []PrioritizedHandler{
		{Priority: 300},
		{Priority: 100},
		{Priority: 200},
	}

	got, err := resolveOrder(handlers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 handlers, got %d", len(got))
	}
	// Sorted by priority.
	if got[0].Priority != 100 || got[1].Priority != 200 || got[2].Priority != 300 {
		t.Errorf("expected priority order 100,200,300 got %d,%d,%d",
			got[0].Priority, got[1].Priority, got[2].Priority)
	}
}

func TestResolveOrder_MultipleHandlersSameApp_InternalSort(t *testing.T) {
	handlers := []PrioritizedHandler{
		{Priority: 300, AppName: "crm"},
		{Priority: 100, AppName: "crm"},
		{Priority: 200, AppName: "crm"},
	}

	got, err := resolveOrder(handlers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
	if got[0].Priority != 100 || got[1].Priority != 200 || got[2].Priority != 300 {
		t.Errorf("expected within-app sort 100,200,300 got %d,%d,%d",
			got[0].Priority, got[1].Priority, got[2].Priority)
	}
}

func TestResolveOrder_LargeGraph(t *testing.T) {
	// 100 apps in a linear chain: app_0 <- app_1 <- ... <- app_99
	const n = 100
	handlers := make([]PrioritizedHandler, n)
	for i := range n {
		h := PrioritizedHandler{
			Priority: 500,
			AppName:  fmt.Sprintf("app_%03d", i),
		}
		if i > 0 {
			h.DependsOn = []string{fmt.Sprintf("app_%03d", i-1)}
		}
		handlers[i] = h
	}

	got, err := resolveOrder(handlers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != n {
		t.Fatalf("expected %d handlers, got %d", n, len(got))
	}

	for i, h := range got {
		want := fmt.Sprintf("app_%03d", i)
		if h.AppName != want {
			t.Errorf("position %d: expected %s, got %s", i, want, h.AppName)
			break
		}
	}
}

func TestResolveOrder_DuplicateDependency(t *testing.T) {
	// Handler lists same dep twice -- should not cause double in-degree.
	handlers := []PrioritizedHandler{
		{Priority: 200, AppName: "billing", DependsOn: []string{"crm", "crm"}},
		{Priority: 100, AppName: "crm"},
	}

	got, err := resolveOrder(handlers)
	if err != nil {
		t.Fatalf("duplicate dependency should be deduplicated: %v", err)
	}

	want := []string{"crm", "billing"}
	assertAppOrder(t, got, want)
}

// --- helpers ---

func assertAppOrder(t *testing.T, got []PrioritizedHandler, wantApps []string) {
	t.Helper()
	if len(got) != len(wantApps) {
		t.Fatalf("expected %d handlers, got %d", len(wantApps), len(got))
	}
	for i, want := range wantApps {
		if got[i].AppName != want {
			t.Errorf("position %d: expected app %q, got %q", i, want, got[i].AppName)
		}
	}
}

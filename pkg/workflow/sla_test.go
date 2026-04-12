package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/osama1998H/moca/pkg/meta"
)

func TestSLAManager_StartTimer(t *testing.T) {
	sm := NewSLAManager(nil)

	rule := &meta.SLARule{
		State:       "Pending",
		MaxDuration: 2 * time.Hour,
	}
	tracker := &BranchStatus{
		BranchName:   "main",
		CurrentState: "Pending",
		EnteredAt:    time.Now(),
	}

	err := sm.StartTimer(context.Background(), "mysite", "PurchaseOrder", "PO-001", rule, tracker)
	if err != nil {
		t.Fatalf("StartTimer returned error: %v", err)
	}

	if tracker.SLADeadline == nil {
		t.Fatal("SLADeadline not set on tracker")
	}

	timers := sm.ActiveTimers()
	if len(timers) != 1 {
		t.Fatalf("ActiveTimers = %d, want 1", len(timers))
	}
	if timers[0].DocName != "PO-001" {
		t.Errorf("DocName = %q, want %q", timers[0].DocName, "PO-001")
	}
}

func TestSLAManager_CancelTimer(t *testing.T) {
	sm := NewSLAManager(nil)

	rule := &meta.SLARule{
		State:       "Pending",
		MaxDuration: time.Hour,
	}
	tracker := &BranchStatus{
		BranchName: "main",
		EnteredAt:  time.Now(),
	}

	if err := sm.StartTimer(context.Background(), "mysite", "PurchaseOrder", "PO-002", rule, tracker); err != nil {
		t.Fatalf("StartTimer: %v", err)
	}

	sm.CancelTimer("PurchaseOrder", "PO-002", "main")

	timers := sm.ActiveTimers()
	if len(timers) != 0 {
		t.Fatalf("ActiveTimers = %d, want 0", len(timers))
	}
}

func TestSLAManager_CheckBreaches(t *testing.T) {
	sm := NewSLAManager(nil)

	rule := &meta.SLARule{
		State:       "Pending",
		MaxDuration: -1 * time.Hour, // already breached
	}
	tracker := &BranchStatus{
		BranchName: "main",
		EnteredAt:  time.Now(),
	}

	if err := sm.StartTimer(context.Background(), "mysite", "PurchaseOrder", "PO-003", rule, tracker); err != nil {
		t.Fatalf("StartTimer: %v", err)
	}

	breaches := sm.CheckBreaches()
	if len(breaches) != 1 {
		t.Fatalf("CheckBreaches = %d, want 1", len(breaches))
	}
	if breaches[0].DocName != "PO-003" {
		t.Errorf("DocName = %q, want %q", breaches[0].DocName, "PO-003")
	}
}

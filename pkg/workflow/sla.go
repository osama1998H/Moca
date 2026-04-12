package workflow

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/queue"
)

// SLATimer tracks an active SLA deadline for a document branch.
type SLATimer struct {
	Deadline   time.Time
	Rule       *meta.SLARule
	Site       string
	DocType    string
	DocName    string
	BranchName string
	State      string
	Escalated  bool
}

// SLAEscalation describes a timer that has breached its deadline.
type SLAEscalation struct {
	Deadline    time.Time
	BreachedAt  time.Time
	Rule        *meta.SLARule
	Site        string
	DocType     string
	DocName     string
	BranchName  string
	State       string
	BreachDelta time.Duration
}

// SLAManager tracks SLA timers for document branches and detects deadline breaches.
// The producer field may be nil (e.g. in tests); when set, a delayed job is enqueued
// to prompt breach checking at the deadline.
type SLAManager struct {
	producer *queue.Producer
	logger   *slog.Logger
	// timers maps "doctype:docname:branch" -> *SLATimer.
	timers map[string]*SLATimer
	mu     sync.RWMutex
}

// NewSLAManager constructs an SLAManager. producer may be nil.
func NewSLAManager(producer *queue.Producer) *SLAManager {
	return &SLAManager{
		producer: producer,
		timers:   make(map[string]*SLATimer),
		logger:   slog.Default(),
	}
}

// slaKey returns the canonical map key for a timer.
func slaKey(doctype, docname, branch string) string {
	return doctype + ":" + docname + ":" + branch
}

// StartTimer records an SLA timer for the given branch. It computes the
// deadline from tracker.EnteredAt + rule.MaxDuration, sets tracker.SLADeadline,
// and stores the timer. If the producer is non-nil a delayed job of type
// "workflow.sla.check" is enqueued to fire at the deadline.
func (s *SLAManager) StartTimer(ctx context.Context, site, doctype, docname string, rule *meta.SLARule, tracker *BranchStatus) error {
	deadline := tracker.EnteredAt.Add(rule.MaxDuration)
	tracker.SLADeadline = &deadline

	timer := &SLATimer{
		Site:       site,
		DocType:    doctype,
		DocName:    docname,
		BranchName: tracker.BranchName,
		State:      tracker.CurrentState,
		Deadline:   deadline,
		Rule:       rule,
		Escalated:  false,
	}

	key := slaKey(doctype, docname, tracker.BranchName)
	s.mu.Lock()
	s.timers[key] = timer
	s.mu.Unlock()

	if s.producer != nil {
		job := queue.Job{
			ID:         uuid.NewString(),
			Site:       site,
			Type:       "workflow.sla.check",
			RunAfter:   &deadline,
			MaxRetries: 3,
			Payload: map[string]any{
				"doctype": doctype,
				"docname": docname,
				"branch":  tracker.BranchName,
				"state":   tracker.CurrentState,
			},
		}
		if _, err := s.producer.Enqueue(ctx, site, queue.QueueCritical, job); err != nil {
			s.logger.Warn("sla: failed to enqueue delayed check job",
				slog.String("doctype", doctype),
				slog.String("docname", docname),
				slog.String("branch", tracker.BranchName),
				slog.Any("err", err),
			)
		}
	}

	return nil
}

// CancelTimer removes the SLA timer for the given doctype/docname/branch.
func (s *SLAManager) CancelTimer(doctype, docname, branch string) {
	key := slaKey(doctype, docname, branch)
	s.mu.Lock()
	delete(s.timers, key)
	s.mu.Unlock()
}

// CheckBreaches returns all active, non-escalated timers whose deadline has passed.
func (s *SLAManager) CheckBreaches() []SLAEscalation {
	now := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()

	var breaches []SLAEscalation
	for _, t := range s.timers {
		if t.Escalated || !now.After(t.Deadline) {
			continue
		}
		delta := now.Sub(t.Deadline)
		breaches = append(breaches, SLAEscalation{
			Site:        t.Site,
			DocType:     t.DocType,
			DocName:     t.DocName,
			BranchName:  t.BranchName,
			State:       t.State,
			Deadline:    t.Deadline,
			BreachedAt:  now,
			BreachDelta: delta,
			Rule:        t.Rule,
		})
	}
	return breaches
}

// MarkEscalated marks the timer for the given key as escalated so it is not
// returned by subsequent CheckBreaches calls.
func (s *SLAManager) MarkEscalated(doctype, docname, branch string) {
	key := slaKey(doctype, docname, branch)
	s.mu.Lock()
	if t, ok := s.timers[key]; ok {
		t.Escalated = true
	}
	s.mu.Unlock()
}

// ActiveTimers returns a snapshot of all currently registered SLA timers.
func (s *SLAManager) ActiveTimers() []SLATimer {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]SLATimer, 0, len(s.timers))
	for _, t := range s.timers {
		out = append(out, *t)
	}
	return out
}

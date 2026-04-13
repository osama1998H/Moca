package document

import (
	"testing"
	"time"
)

func TestEventLogQueryOpts_Defaults(t *testing.T) {
	opts := EventLogQueryOpts{}
	if opts.Limit != 0 {
		t.Errorf("default Limit: got %d, want 0", opts.Limit)
	}
	if !opts.Since.IsZero() {
		t.Error("default Since should be zero")
	}
	if !opts.Until.IsZero() {
		t.Error("default Until should be zero")
	}
	if opts.EventType != "" {
		t.Error("default EventType should be empty")
	}
}

func TestEventLogQueryOpts_WithValues(t *testing.T) {
	now := time.Now()
	opts := EventLogQueryOpts{
		Limit:     50,
		Offset:    10,
		Since:     now.Add(-time.Hour),
		Until:     now,
		EventType: "doc.created",
	}
	if opts.Limit != 50 {
		t.Errorf("Limit: got %d, want 50", opts.Limit)
	}
	if opts.Offset != 10 {
		t.Errorf("Offset: got %d, want 10", opts.Offset)
	}
	if opts.EventType != "doc.created" {
		t.Errorf("EventType: got %q, want doc.created", opts.EventType)
	}
	if !opts.Since.Equal(now.Add(-time.Hour)) {
		t.Errorf("Since: got %v, want %v", opts.Since, now.Add(-time.Hour))
	}
	if !opts.Until.Equal(now) {
		t.Errorf("Until: got %v, want %v", opts.Until, now)
	}
}

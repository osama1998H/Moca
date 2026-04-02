package queue

import (
	"testing"
	"time"
)

func TestStreamKey(t *testing.T) {
	tests := []struct {
		site string
		qt   QueueType
		want string
	}{
		{"acme", QueueDefault, "moca:queue:acme:default"},
		{"acme", QueueLong, "moca:queue:acme:long"},
		{"acme", QueueCritical, "moca:queue:acme:critical"},
		{"corp", QueueScheduler, "moca:queue:corp:scheduler"},
	}
	for _, tt := range tests {
		if got := StreamKey(tt.site, tt.qt); got != tt.want {
			t.Errorf("StreamKey(%q, %q) = %q, want %q", tt.site, tt.qt, got, tt.want)
		}
	}
}

func TestDLQKey(t *testing.T) {
	if got := DLQKey("acme"); got != "moca:deadletter:acme" {
		t.Errorf("DLQKey(acme) = %q, want moca:deadletter:acme", got)
	}
}

func TestDelayedKey(t *testing.T) {
	if got := DelayedKey("acme"); got != "moca:delayed:acme" {
		t.Errorf("DelayedKey(acme) = %q, want moca:delayed:acme", got)
	}
}

func TestGroupName(t *testing.T) {
	if got := GroupName("acme"); got != "acme-workers" {
		t.Errorf("GroupName(acme) = %q, want acme-workers", got)
	}
}

func TestJobToValuesRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Nanosecond)
	job := Job{
		ID:         "job-123",
		Site:       "acme",
		Type:       "send_email",
		Payload:    map[string]any{"to": "user@example.com", "subject": "Hello"},
		Priority:   1,
		MaxRetries: 3,
		Retries:    0,
		CreatedAt:  now,
		Timeout:    30 * time.Second,
	}

	values, err := jobToValues(job)
	if err != nil {
		t.Fatalf("jobToValues: %v", err)
	}

	got, err := valuesToJob(values)
	if err != nil {
		t.Fatalf("valuesToJob: %v", err)
	}

	if got.ID != job.ID {
		t.Errorf("ID = %q, want %q", got.ID, job.ID)
	}
	if got.Site != job.Site {
		t.Errorf("Site = %q, want %q", got.Site, job.Site)
	}
	if got.Type != job.Type {
		t.Errorf("Type = %q, want %q", got.Type, job.Type)
	}
	if got.Priority != job.Priority {
		t.Errorf("Priority = %d, want %d", got.Priority, job.Priority)
	}
	if got.MaxRetries != job.MaxRetries {
		t.Errorf("MaxRetries = %d, want %d", got.MaxRetries, job.MaxRetries)
	}
	if got.Retries != job.Retries {
		t.Errorf("Retries = %d, want %d", got.Retries, job.Retries)
	}
	if !got.CreatedAt.Equal(job.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, job.CreatedAt)
	}
	if got.Timeout != job.Timeout {
		t.Errorf("Timeout = %v, want %v", got.Timeout, job.Timeout)
	}
	if got.RunAfter != nil {
		t.Errorf("RunAfter = %v, want nil", got.RunAfter)
	}
	if got.Payload["to"] != "user@example.com" {
		t.Errorf("Payload[to] = %v, want user@example.com", got.Payload["to"])
	}
}

func TestJobToValuesWithRunAfter(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Nanosecond)
	ra := now.Add(1 * time.Hour)
	job := Job{
		ID:        "job-456",
		Site:      "acme",
		Type:      "generate_report",
		Payload:   map[string]any{},
		CreatedAt: now,
		RunAfter:  &ra,
		Timeout:   5 * time.Minute,
	}

	values, err := jobToValues(job)
	if err != nil {
		t.Fatalf("jobToValues: %v", err)
	}

	if _, ok := values["run_after"]; !ok {
		t.Fatal("expected run_after field in values")
	}

	got, err := valuesToJob(values)
	if err != nil {
		t.Fatalf("valuesToJob: %v", err)
	}

	if got.RunAfter == nil {
		t.Fatal("RunAfter is nil, want non-nil")
	}
	if !got.RunAfter.Equal(ra) {
		t.Errorf("RunAfter = %v, want %v", got.RunAfter, ra)
	}
}

func TestValuesToJobMalformed(t *testing.T) {
	tests := []struct {
		values map[string]interface{}
		name   string
	}{
		{
			name:   "missing created_at",
			values: map[string]interface{}{"id": "x", "timeout": "1s"},
		},
		{
			name:   "invalid timeout",
			values: map[string]interface{}{"id": "x", "created_at": "2024-01-01T00:00:00Z", "timeout": "not-a-duration"},
		},
		{
			name:   "invalid run_after",
			values: map[string]interface{}{"id": "x", "created_at": "2024-01-01T00:00:00Z", "timeout": "1s", "run_after": "bad"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := valuesToJob(tt.values)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

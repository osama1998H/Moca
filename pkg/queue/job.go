package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// QueueType classifies a Redis Streams queue by workload priority.
type QueueType string

const (
	QueueDefault   QueueType = "default"   // General background tasks
	QueueLong      QueueType = "long"      // Long-running tasks (reports, exports)
	QueueCritical  QueueType = "critical"  // High-priority tasks (webhooks, emails)
	QueueScheduler QueueType = "scheduler" // Cron-triggered tasks
)

// AllQueueTypes is the canonical list of queue types.
var AllQueueTypes = []QueueType{QueueDefault, QueueLong, QueueCritical, QueueScheduler}

// Job represents a background task enqueued to a Redis Stream.
// Field layout matches MOCA_SYSTEM_DESIGN.md §5.2.
type Job struct {
	CreatedAt  time.Time      `json:"created_at"`
	RunAfter   *time.Time     `json:"run_after,omitempty"`
	Payload    map[string]any `json:"payload"`
	ID         string         `json:"id"`
	Site       string         `json:"site"`
	Type       string         `json:"type"`
	Timeout    time.Duration  `json:"timeout"`
	Priority   int            `json:"priority"`
	MaxRetries int            `json:"max_retries"`
	Retries    int            `json:"retries"`
}

// JobHandler is the callback type for processing a consumed job.
// Return nil to acknowledge. Return an error to leave unacknowledged
// (stays in the Pending Entries List for redelivery or DLQ processing).
type JobHandler func(ctx context.Context, job Job) error

// StreamKey returns the Redis stream key for a site and queue type.
// Format: moca:queue:{site}:{queueType}
func StreamKey(site string, qt QueueType) string {
	return fmt.Sprintf("moca:queue:%s:%s", site, string(qt))
}

// DLQKey returns the dead-letter queue stream key for a site.
// Format: moca:deadletter:{site}
func DLQKey(site string) string {
	return fmt.Sprintf("moca:deadletter:%s", site)
}

// DelayedKey returns the sorted-set key for delayed jobs for a site.
// Format: moca:delayed:{site}
func DelayedKey(site string) string {
	return fmt.Sprintf("moca:delayed:%s", site)
}

// GroupName returns the consumer group name for a site.
// Format: {site}-workers
func GroupName(site string) string {
	return site + "-workers"
}

// jobToValues converts a Job to a flat map suitable for XAdd.
// Each Job field becomes a separate stream entry field. Only the Payload
// is JSON-encoded because it is map[string]any.
func jobToValues(j Job) (map[string]interface{}, error) {
	payloadJSON, err := json.Marshal(j.Payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	m := map[string]interface{}{
		"id":          j.ID,
		"site":        j.Site,
		"type":        j.Type,
		"payload":     string(payloadJSON),
		"priority":    strconv.Itoa(j.Priority),
		"max_retries": strconv.Itoa(j.MaxRetries),
		"retries":     strconv.Itoa(j.Retries),
		"created_at":  j.CreatedAt.UTC().Format(time.RFC3339Nano),
		"timeout":     j.Timeout.String(),
	}
	if j.RunAfter != nil {
		m["run_after"] = j.RunAfter.UTC().Format(time.RFC3339Nano)
	}
	return m, nil
}

// valuesToJob reconstructs a Job from a Redis stream message's Values map.
func valuesToJob(values map[string]interface{}) (Job, error) {
	getString := func(key string) string {
		v := values[key]
		s, _ := v.(string)
		return s
	}

	createdAt, err := time.Parse(time.RFC3339Nano, getString("created_at"))
	if err != nil {
		return Job{}, fmt.Errorf("parse created_at: %w", err)
	}

	priority, _ := strconv.Atoi(getString("priority"))
	maxRetries, _ := strconv.Atoi(getString("max_retries"))
	retries, _ := strconv.Atoi(getString("retries"))

	timeout, err := time.ParseDuration(getString("timeout"))
	if err != nil {
		return Job{}, fmt.Errorf("parse timeout: %w", err)
	}

	var payload map[string]any
	if p := getString("payload"); p != "" {
		if err := json.Unmarshal([]byte(p), &payload); err != nil {
			return Job{}, fmt.Errorf("unmarshal payload: %w", err)
		}
	}

	j := Job{
		ID:         getString("id"),
		Site:       getString("site"),
		Type:       getString("type"),
		Payload:    payload,
		Priority:   priority,
		MaxRetries: maxRetries,
		Retries:    retries,
		CreatedAt:  createdAt,
		Timeout:    timeout,
	}

	if ra := getString("run_after"); ra != "" {
		t, err := time.Parse(time.RFC3339Nano, ra)
		if err != nil {
			return Job{}, fmt.Errorf("parse run_after: %w", err)
		}
		j.RunAfter = &t
	}

	return j, nil
}

// ValuesToJob reconstructs a Job from a Redis stream message's Values map.
// This is the exported wrapper around valuesToJob for use by CLI commands.
func ValuesToJob(values map[string]interface{}) (Job, error) {
	return valuesToJob(values)
}

// delayedEntry is the serialization format for sorted-set members
// in the delayed job set. It wraps both the job and its target queue type.
type delayedEntry struct {
	QueueType QueueType `json:"queue_type"`
	Job       Job       `json:"job"`
}

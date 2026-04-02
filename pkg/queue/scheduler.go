package queue

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// CronEntry defines a scheduled job that fires on a cron expression.
type CronEntry struct {
	// Name is a human-readable label for the cron entry (e.g. "daily-cleanup").
	Name string

	// CronExpr is a standard cron expression (5-field or with optional seconds).
	// Examples: "*/5 * * * *" (every 5 min), "@every 1h", "@daily".
	CronExpr string

	// Site is the tenant site this job targets.
	Site string

	// JobType is the handler key the worker pool dispatches to (e.g. "cleanup").
	JobType string

	// Payload is arbitrary data passed to the job handler.
	Payload map[string]any

	// QueueType determines which Redis Stream receives the job.
	// Defaults to QueueScheduler if empty.
	QueueType QueueType
}

// registeredEntry is an internal representation of a CronEntry
// paired with its parsed cron schedule.
type registeredEntry struct {
	schedule cron.Schedule
	CronEntry
}

// Scheduler runs cron entries on a tick loop, enqueuing jobs via a Producer
// when their cron expressions match the current time. It is designed to be
// run behind a LeaderElection so that only one instance fires jobs.
type Scheduler struct {
	producer     *Producer
	logger       *slog.Logger
	entries      []registeredEntry
	parser       cron.Parser
	mu           sync.Mutex
	tickInterval time.Duration
}

// SchedulerOption configures the Scheduler.
type SchedulerOption func(*Scheduler)

// WithTickInterval sets the scheduler tick interval. Default: 1s.
func WithTickInterval(d time.Duration) SchedulerOption {
	return func(s *Scheduler) {
		if d > 0 {
			s.tickInterval = d
		}
	}
}

// WithSchedulerLogger sets the logger. Default: slog.Default().
func WithSchedulerLogger(l *slog.Logger) SchedulerOption {
	return func(s *Scheduler) {
		if l != nil {
			s.logger = l
		}
	}
}

// NewScheduler creates a Scheduler that enqueues jobs via the given Producer.
func NewScheduler(producer *Producer, opts ...SchedulerOption) *Scheduler {
	s := &Scheduler{
		producer:     producer,
		tickInterval: 1 * time.Second,
		logger:       slog.Default(),
		parser: cron.NewParser(
			cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
		),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Register adds a cron entry. The cron expression is validated at
// registration time. Returns an error if the expression is invalid.
func (s *Scheduler) Register(entry CronEntry) error {
	sched, err := s.parser.Parse(entry.CronExpr)
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", entry.CronExpr, err)
	}
	if entry.QueueType == "" {
		entry.QueueType = QueueScheduler
	}

	s.mu.Lock()
	s.entries = append(s.entries, registeredEntry{
		CronEntry: entry,
		schedule:  sched,
	})
	s.mu.Unlock()

	s.logger.Info("scheduler: registered cron entry",
		slog.String("name", entry.Name),
		slog.String("cron", entry.CronExpr),
		slog.String("site", entry.Site),
		slog.String("job_type", entry.JobType),
	)
	return nil
}

// Entries returns the number of registered cron entries.
func (s *Scheduler) Entries() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// Run starts the scheduler tick loop. It checks all registered cron entries
// each tick and enqueues matching jobs via the Producer.
//
// Run blocks until ctx is cancelled. Signature matches process.Subsystem.Run.
func (s *Scheduler) Run(ctx context.Context) error {
	s.logger.Info("scheduler: started",
		slog.Int("entries", s.Entries()),
		slog.Duration("tick_interval", s.tickInterval),
	)

	ticker := time.NewTicker(s.tickInterval)
	defer ticker.Stop()

	lastTick := time.Now().Truncate(time.Second)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("scheduler: stopped")
			return nil
		case now := <-ticker.C:
			now = now.Truncate(time.Second)
			s.fireDueEntries(ctx, lastTick, now)
			lastTick = now
		}
	}
}

// RunWithLeader wraps Run behind a LeaderElection. The scheduler only
// fires jobs while this instance holds the leader lock.
func (s *Scheduler) RunWithLeader(ctx context.Context, le *LeaderElection) error {
	return le.Run(ctx, s.Run)
}

// fireDueEntries checks each registered entry and enqueues a job if the
// entry's next fire time falls within (lastTick, now].
func (s *Scheduler) fireDueEntries(ctx context.Context, lastTick, now time.Time) {
	s.mu.Lock()
	entries := make([]registeredEntry, len(s.entries))
	copy(entries, s.entries)
	s.mu.Unlock()

	for _, e := range entries {
		next := e.schedule.Next(lastTick)
		if !next.After(now) {
			s.enqueueEntry(ctx, e)
		}
	}
}

// enqueueEntry creates and enqueues a Job for the given cron entry.
func (s *Scheduler) enqueueEntry(ctx context.Context, e registeredEntry) {
	job := Job{
		ID:         generateJobID(),
		Site:       e.Site,
		Type:       e.JobType,
		Payload:    e.Payload,
		Priority:   0,
		MaxRetries: 3,
		CreatedAt:  time.Now().UTC(),
		Timeout:    5 * time.Minute,
	}

	_, err := s.producer.Enqueue(ctx, e.Site, e.QueueType, job)
	if err != nil {
		s.logger.Error("scheduler: failed to enqueue cron job",
			slog.String("name", e.Name),
			slog.String("job_type", e.JobType),
			slog.String("site", e.Site),
			slog.String("error", err.Error()),
		)
		return
	}

	s.logger.Info("scheduler: fired cron job",
		slog.String("name", e.Name),
		slog.String("job_type", e.JobType),
		slog.String("site", e.Site),
		slog.String("job_id", job.ID),
	)
}

// generateJobID returns a random UUID v4 string.
func generateJobID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

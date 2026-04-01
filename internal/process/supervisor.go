package process

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const defaultShutdownTimeout = 30 * time.Second

// Subsystem represents a long-running goroutine managed by the Supervisor.
type Subsystem struct {
	// Run is the long-running function. It should block until ctx is cancelled
	// or an error occurs. Returning nil signals clean shutdown.
	Run func(ctx context.Context) error
	// Name is a human-readable identifier for logging.
	Name string
	// Critical indicates that failure of this subsystem should cascade
	// shutdown to all other subsystems.
	Critical bool
}

// SupervisorOption configures a Supervisor.
type SupervisorOption func(*Supervisor)

// WithShutdownTimeout sets the maximum time to wait for subsystems to stop
// after shutdown is initiated. Defaults to 30 seconds.
func WithShutdownTimeout(d time.Duration) SupervisorOption {
	return func(s *Supervisor) {
		s.shutdownTimeout = d
	}
}

// Supervisor manages a set of subsystems, starting them concurrently and
// coordinating graceful shutdown.
type Supervisor struct {
	logger          *slog.Logger
	subsystems      []Subsystem
	shutdownTimeout time.Duration
}

// NewSupervisor creates a Supervisor with the given logger and options.
func NewSupervisor(logger *slog.Logger, opts ...SupervisorOption) *Supervisor {
	s := &Supervisor{
		shutdownTimeout: defaultShutdownTimeout,
		logger:          logger,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Add registers a subsystem to be managed by the supervisor.
func (s *Supervisor) Add(sub Subsystem) {
	s.subsystems = append(s.subsystems, sub)
}

// subResult captures the outcome of a subsystem's Run function.
type subResult struct {
	err      error
	name     string
	critical bool
}

// Run starts all registered subsystems concurrently and blocks until all have
// exited. If a critical subsystem fails, all others are cancelled. Non-critical
// failures are logged but do not cascade. When the parent context is cancelled,
// all subsystems are given shutdownTimeout to exit gracefully.
func (s *Supervisor) Run(ctx context.Context) error {
	if len(s.subsystems) == 0 {
		return nil
	}

	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan subResult, len(s.subsystems))

	for _, sub := range s.subsystems {
		wg.Add(1)
		go func(sub Subsystem) {
			defer wg.Done()
			s.logger.Info("subsystem started", slog.String("subsystem", sub.Name))
			err := sub.Run(childCtx)
			errCh <- subResult{name: sub.Name, critical: sub.Critical, err: err}
		}(sub)
	}

	var firstCritical error
	remaining := len(s.subsystems)
	shutdownInitiated := false
	var timeoutCh <-chan time.Time

	for remaining > 0 {
		select {
		case result := <-errCh:
			remaining--
			if result.err != nil && !errors.Is(result.err, context.Canceled) {
				if result.critical {
					s.logger.Error("critical subsystem failed",
						slog.String("subsystem", result.name),
						slog.String("error", result.err.Error()),
					)
					if firstCritical == nil {
						firstCritical = fmt.Errorf("process: subsystem %s: %w", result.name, result.err)
					}
					if !shutdownInitiated {
						cancel()
						shutdownInitiated = true
						timeoutCh = time.After(s.shutdownTimeout)
					}
				} else {
					s.logger.Warn("non-critical subsystem failed",
						slog.String("subsystem", result.name),
						slog.String("error", result.err.Error()),
					)
				}
			} else {
				s.logger.Info("subsystem stopped", slog.String("subsystem", result.name))
			}

		case <-childCtx.Done():
			if !shutdownInitiated {
				shutdownInitiated = true
				timeoutCh = time.After(s.shutdownTimeout)
			}

		case <-timeoutCh:
			s.logger.Error("shutdown timeout exceeded, abandoning remaining subsystems",
				slog.Int("remaining", remaining),
				slog.Duration("timeout", s.shutdownTimeout),
			)
			return firstCritical
		}
	}

	return firstCritical
}

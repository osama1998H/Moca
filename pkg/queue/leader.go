package queue

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// DefaultLeaderKey is the Redis key used for scheduler leader election.
	DefaultLeaderKey = "moca:scheduler:leader"

	// DefaultLeaderTTL is the lock expiry. If the leader fails to renew
	// within this window, another instance can acquire leadership.
	DefaultLeaderTTL = 30 * time.Second

	// DefaultRenewInterval is how often the leader refreshes the lock TTL.
	DefaultRenewInterval = 10 * time.Second

	// DefaultPollInterval is how often a non-leader polls to acquire the lock.
	DefaultPollInterval = 5 * time.Second
)

// Lua script: extend TTL only if we still own the lock.
// KEYS[1] = lock key, ARGV[1] = instance ID, ARGV[2] = TTL seconds.
// Returns 1 if renewed, 0 if not owner.
var luaRenew = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("SET", KEYS[1], ARGV[1], "XX", "EX", ARGV[2])
end
return 0
`)

// Lua script: delete lock only if we still own it.
// KEYS[1] = lock key, ARGV[1] = instance ID.
// Returns 1 if deleted, 0 if not owner.
var luaRelease = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)

// LeaderElectionConfig configures the leader election behaviour.
type LeaderElectionConfig struct {
	// Logger for structured logging. Falls back to slog.Default() if nil.
	Logger *slog.Logger

	// Key is the Redis key for the lock. Default: "moca:scheduler:leader".
	Key string

	// InstanceID uniquely identifies this process. Default: hostname.
	InstanceID string

	// TTL is the lock expiry duration. Default: 30s.
	TTL time.Duration

	// RenewInterval is how often the leader refreshes the lock. Default: 10s.
	RenewInterval time.Duration

	// PollInterval is how often a non-leader polls for acquisition. Default: 5s.
	PollInterval time.Duration
}

func (c *LeaderElectionConfig) withDefaults() {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.Key == "" {
		c.Key = DefaultLeaderKey
	}
	if c.InstanceID == "" {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "scheduler"
		}
		c.InstanceID = fmt.Sprintf("%s-%d", hostname, os.Getpid())
	}
	if c.TTL <= 0 {
		c.TTL = DefaultLeaderTTL
	}
	if c.RenewInterval <= 0 {
		c.RenewInterval = DefaultRenewInterval
	}
	if c.PollInterval <= 0 {
		c.PollInterval = DefaultPollInterval
	}
}

// LeaderElection implements a Redis-based distributed leader election.
// Only one instance across replicas will be elected leader at a time.
// When elected, it calls the provided callback. If the lock is lost,
// the callback's context is cancelled and the instance reverts to polling.
type LeaderElection struct {
	rdb    *redis.Client
	logger *slog.Logger
	config LeaderElectionConfig
}

// NewLeaderElection creates a new leader election instance.
func NewLeaderElection(rdb *redis.Client, cfg LeaderElectionConfig) *LeaderElection {
	cfg.withDefaults()
	return &LeaderElection{
		rdb:    rdb,
		config: cfg,
		logger: cfg.Logger,
	}
}

// Run enters the leader election loop. When this instance becomes leader,
// onElected is called with a context that is cancelled if leadership is lost.
// onElected should perform work (e.g. run the scheduler loop) and return
// when its context is cancelled. Run blocks until ctx is cancelled.
func (le *LeaderElection) Run(ctx context.Context, onElected func(ctx context.Context) error) error {
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		acquired, err := le.tryAcquire(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			le.logger.Error("leader election: acquire failed",
				slog.String("error", err.Error()),
			)
			if !le.sleep(ctx, le.config.PollInterval) {
				return nil
			}
			continue
		}

		if acquired {
			le.logger.Info("leader election: acquired leadership",
				slog.String("instance", le.config.InstanceID),
				slog.String("key", le.config.Key),
			)
			le.runAsLeader(ctx, onElected)
			le.logger.Info("leader election: lost or released leadership",
				slog.String("instance", le.config.InstanceID),
			)
			continue
		}

		le.logger.Debug("leader election: waiting for leader",
			slog.String("instance", le.config.InstanceID),
		)
		if !le.sleep(ctx, le.config.PollInterval) {
			return nil
		}
	}
}

// tryAcquire attempts to acquire the leader lock via SET NX EX.
func (le *LeaderElection) tryAcquire(ctx context.Context) (bool, error) {
	result, err := le.rdb.SetArgs(ctx, le.config.Key, le.config.InstanceID, redis.SetArgs{
		Mode: "NX",
		TTL:  le.config.TTL,
	}).Result()
	if err != nil {
		if err == redis.Nil {
			return false, nil
		}
		return false, fmt.Errorf("SET NX: %w", err)
	}
	return result == "OK", nil
}

// runAsLeader runs the onElected callback and a heartbeat goroutine.
// Returns when leadership is lost or ctx is cancelled.
func (le *LeaderElection) runAsLeader(ctx context.Context, onElected func(ctx context.Context) error) {
	leaderCtx, cancelLeader := context.WithCancel(ctx)
	defer cancelLeader()

	// Heartbeat goroutine: renew the lock periodically.
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		le.heartbeat(leaderCtx, cancelLeader)
	}()

	// Run the callback (e.g. the scheduler loop).
	if err := onElected(leaderCtx); err != nil {
		le.logger.Error("leader election: onElected returned error",
			slog.String("error", err.Error()),
		)
	}

	// Cancel heartbeat and wait for it to finish.
	cancelLeader()
	<-heartbeatDone

	// Release the lock if we still own it.
	le.release(context.Background())
}

// heartbeat renews the lock at RenewInterval. If renewal fails (we lost
// the lock), it cancels the leader context.
func (le *LeaderElection) heartbeat(ctx context.Context, cancelLeader context.CancelFunc) {
	ticker := time.NewTicker(le.config.RenewInterval)
	defer ticker.Stop()

	ttlSec := int(le.config.TTL.Seconds())

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result, err := luaRenew.Run(ctx, le.rdb,
				[]string{le.config.Key},
				le.config.InstanceID,
				ttlSec,
			).Result()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				le.logger.Error("leader election: renew failed",
					slog.String("error", err.Error()),
				)
				cancelLeader()
				return
			}
			// luaRenew returns "OK" on success, 0 if not owner.
			if result != "OK" {
				le.logger.Debug("leader election: lock lost, will re-acquire",
					slog.String("key", le.config.Key),
					slog.String("instance", le.config.InstanceID),
				)
				cancelLeader()
				return
			}
			le.logger.Debug("leader election: heartbeat renewed",
				slog.String("instance", le.config.InstanceID),
			)
		}
	}
}

// release attempts to delete the lock if we still own it.
func (le *LeaderElection) release(ctx context.Context) {
	_, err := luaRelease.Run(ctx, le.rdb,
		[]string{le.config.Key},
		le.config.InstanceID,
	).Result()
	if err != nil {
		le.logger.Debug("leader election: release failed",
			slog.String("error", err.Error()),
		)
	}
}

// sleep returns true if the full duration elapsed, false if ctx was cancelled.
func (le *LeaderElection) sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/internal/drivers"
	"github.com/osama1998H/moca/internal/process"
	"github.com/osama1998H/moca/pkg/observe"
	"github.com/osama1998H/moca/pkg/queue"
)

// Build-time variables injected via -ldflags.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	const configFile = "moca.yaml"

	if _, err := os.Stat(configFile); errors.Is(err, os.ErrNotExist) {
		fmt.Println("no moca.yaml found in current directory")
		return nil
	}

	cfg, err := config.LoadAndResolve(configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := observe.NewLogger(slog.LevelInfo)

	logger.Info("starting moca-scheduler",
		slog.String("version", Version),
		slog.String("commit", Commit),
		slog.String("built", BuildDate),
		slog.String("go", runtime.Version()),
	)

	if !cfg.Scheduler.Enabled {
		logger.Info("scheduler disabled in config (scheduler.enabled = false)")
		return nil
	}

	// Connect Redis.
	redisClients := drivers.NewRedisClients(cfg.Infrastructure.Redis, logger)
	ctx := context.Background()
	if err := redisClients.Ping(ctx); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	defer func() { _ = redisClients.Close() }()

	// Create producer for enqueuing cron jobs.
	producer := queue.NewProducer(redisClients.Queue, logger)

	// Create scheduler with configurable tick interval.
	var opts []queue.SchedulerOption
	opts = append(opts, queue.WithSchedulerLogger(logger))
	if cfg.Scheduler.TickInterval != "" {
		d, err := parseTickInterval(cfg.Scheduler.TickInterval)
		if err != nil {
			return fmt.Errorf("invalid scheduler.tick_interval: %w", err)
		}
		opts = append(opts, queue.WithTickInterval(d))
	}
	scheduler := queue.NewScheduler(producer, opts...)

	// Register cron entries from environment or config.
	// In future milestones, apps will register cron entries via the hook registry.
	// For now, sites are read from MOCA_SCHEDULER_SITES for completeness.
	sites := sitesFromEnv()
	if len(sites) == 0 {
		logger.Warn("no sites configured (set MOCA_SCHEDULER_SITES=site1,site2)")
	}

	logger.Info("moca-scheduler configured",
		slog.Int("sites", len(sites)),
		slog.Int("cron_entries", scheduler.Entries()),
	)

	// Create leader election.
	le := queue.NewLeaderElection(redisClients.Queue, queue.LeaderElectionConfig{
		Logger: logger,
	})

	// Run scheduler under leader election, managed by supervisor.
	sup := process.NewSupervisor(logger)
	sup.Add(process.Subsystem{
		Name: "scheduler",
		Run: func(ctx context.Context) error {
			return scheduler.RunWithLeader(ctx, le)
		},
		Critical: true,
	})

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	logger.Info("moca-scheduler running (waiting for leader election)")

	return sup.Run(sigCtx)
}

// sitesFromEnv reads comma-separated site names from MOCA_SCHEDULER_SITES.
func sitesFromEnv() []string {
	v := os.Getenv("MOCA_SCHEDULER_SITES")
	if v == "" {
		return nil
	}
	var sites []string
	for _, s := range strings.Split(v, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			sites = append(sites, s)
		}
	}
	return sites
}

// parseTickInterval parses a duration string like "1s", "500ms", "2s".
func parseTickInterval(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}

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

	logger.Info("starting moca-worker",
		slog.String("version", Version),
		slog.String("commit", Commit),
		slog.String("built", BuildDate),
		slog.String("go", runtime.Version()),
	)

	// Connect Redis.
	redisClients := drivers.NewRedisClients(cfg.Infrastructure.Redis, logger)
	ctx := context.Background()
	if err := redisClients.Ping(ctx); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	defer func() { _ = redisClients.Close() }()

	// Determine sites to consume from.
	sites := sitesFromEnv()
	if len(sites) == 0 {
		logger.Warn("no sites configured (set MOCA_WORKER_SITES=site1,site2)")
	}

	// Configure worker pool.
	wpCfg := queue.DefaultWorkerPoolConfig()
	wpCfg.Sites = sites
	wpCfg.Logger = logger

	wp := queue.NewWorkerPool(redisClients.Queue, wpCfg)

	// Register a default logging handler for unhandled job types.
	// Real handlers will be registered by T5 when integrating with the app system.
	wp.Handle("_default", func(_ context.Context, job queue.Job) error {
		logger.Info("processed job",
			slog.String("type", job.Type),
			slog.String("id", job.ID),
			slog.String("site", job.Site),
		)
		return nil
	})

	// Run under supervisor.
	sup := process.NewSupervisor(logger)
	sup.Add(process.Subsystem{
		Name:     "worker-pool",
		Run:      wp.Run,
		Critical: true,
	})

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	logger.Info("moca-worker running",
		slog.Int("sites", len(sites)),
	)

	return sup.Run(sigCtx)
}

// sitesFromEnv reads comma-separated site names from MOCA_WORKER_SITES.
func sitesFromEnv() []string {
	v := os.Getenv("MOCA_WORKER_SITES")
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

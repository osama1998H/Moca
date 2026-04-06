package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/internal/drivers"
	"github.com/osama1998H/moca/internal/process"
	"github.com/osama1998H/moca/pkg/events"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/observe"
	"github.com/osama1998H/moca/pkg/orm"
	"github.com/osama1998H/moca/pkg/queue"
	"github.com/osama1998H/moca/pkg/search"
)

// Build-time variables injected via -ldflags.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

const outboxLeaderKey = "moca:outbox:leader"

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
	logger.Info("starting moca-outbox",
		slog.String("version", Version),
		slog.String("commit", Commit),
		slog.String("built", BuildDate),
		slog.String("go", runtime.Version()),
	)

	ctx := context.Background()

	redisClients := drivers.NewRedisClients(cfg.Infrastructure.Redis, logger)
	err = redisClients.Ping(ctx)
	if err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	defer func() { _ = redisClients.Close() }()

	dbManager, err := orm.NewDBManager(ctx, cfg.Infrastructure.Database, logger)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer dbManager.Close()

	producer, err := events.NewProducer(cfg.Infrastructure.Kafka, redisClients)
	if err != nil {
		return fmt.Errorf("event producer: %w", err)
	}
	defer func() { _ = producer.Close() }()

	registry := meta.NewRegistry(dbManager, redisClients.Cache, logger)
	store := events.NewDBOutboxStore(dbManager)
	siteLister := &meta.DBSiteLister{DB: dbManager}

	var searchClient *search.Client
	var syncer *search.Syncer
	var afterPublish events.AfterPublishHook

	searchClient, err = search.NewClient(cfg.Infrastructure.Search)
	switch {
	case err == nil:
		defer searchClient.Close()
		syncer = search.NewSyncer(searchClient, registry, cfg.Infrastructure.Kafka, logger)
	case errors.Is(err, search.ErrUnavailable):
		logger.Info("search backend not configured; search sync disabled")
	default:
		logger.Warn("search backend unavailable; search sync disabled",
			slog.String("error", err.Error()),
		)
	}

	if syncer != nil && (cfg.Infrastructure.Kafka.Enabled == nil || !*cfg.Infrastructure.Kafka.Enabled) {
		queueProducer := queue.NewProducer(redisClients.Queue, logger)
		afterPublish = func(ctx context.Context, event events.DocumentEvent) error {
			return enqueueSearchSync(ctx, queueProducer, event)
		}
	}

	// Compose the WebSocket pub/sub relay hook (best-effort).
	wsHook := events.WebSocketPublishHook(redisClients.PubSub, logger)
	afterPublish = events.ComposeHooks(afterPublish, wsHook)

	poller, err := events.NewOutboxPoller(events.OutboxPollerConfig{
		Store:        store,
		Sites:        siteLister,
		Producer:     producer,
		Logger:       logger,
		PollInterval: events.DefaultOutboxPollInterval,
		BatchSize:    events.DefaultOutboxBatchSize,
		MaxRetries:   events.DefaultOutboxMaxRetries,
		AfterPublish: afterPublish,
	})
	if err != nil {
		return err
	}

	leader := queue.NewLeaderElection(redisClients.Queue, queue.LeaderElectionConfig{
		Logger: logger,
		Key:    outboxLeaderKey,
	})

	sup := process.NewSupervisor(logger)
	sup.Add(process.Subsystem{
		Name: "outbox-poller",
		Run: func(ctx context.Context) error {
			return leader.Run(ctx, poller.Run)
		},
		Critical: true,
	})

	if syncer != nil && cfg.Infrastructure.Kafka.Enabled != nil && *cfg.Infrastructure.Kafka.Enabled {
		sup.Add(process.Subsystem{
			Name: "search-sync",
			Run:  syncer.RunKafka,
		})
	}

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("moca-outbox running")
	return sup.Run(sigCtx)
}

func enqueueSearchSync(ctx context.Context, producer *queue.Producer, event events.DocumentEvent) error {
	payload, err := eventPayload(event)
	if err != nil {
		return err
	}

	job := queue.Job{
		ID:         event.EventID,
		Site:       event.Site,
		Type:       search.JobTypeSearchSync,
		Payload:    map[string]any{"event": payload},
		CreatedAt:  time.Now().UTC(),
		MaxRetries: queue.DefaultWorkerPoolConfig().MaxRetries,
		Timeout:    30 * time.Second,
	}
	_, err = producer.Enqueue(ctx, event.Site, queue.QueueDefault, job)
	if err != nil {
		return fmt.Errorf("enqueue search.sync job: %w", err)
	}
	return nil
}

func eventPayload(event events.DocumentEvent) (map[string]any, error) {
	raw, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("marshal event payload: %w", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal event payload: %w", err)
	}
	return payload, nil
}

package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/internal/drivers"
	"github.com/osama1998H/moca/pkg/api"
	"github.com/osama1998H/moca/pkg/events"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/orm"
	"github.com/osama1998H/moca/pkg/queue"
	"github.com/osama1998H/moca/pkg/search"
)

// WorkerSubsystem creates a supervisor-compatible run function for the
// background worker pool. It discovers active sites from the database and
// registers a search.sync handler when Kafka is disabled and search is available.
func WorkerSubsystem(
	dbManager *orm.DBManager,
	redisClients *drivers.RedisClients,
	registry *meta.Registry,
	kafkaCfg config.KafkaConfig,
	searchCfg config.SearchConfig,
	logger *slog.Logger,
) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		// Discover active tenant sites from the system database.
		siteLister := &meta.DBSiteLister{DB: dbManager}
		sites, err := siteLister.ListActiveSites(ctx)
		if err != nil {
			logger.Warn("worker: failed to list active sites", slog.String("error", err.Error()))
		}
		if len(sites) == 0 {
			logger.Warn("worker: no active sites found")
		}

		wpCfg := queue.DefaultWorkerPoolConfig()
		wpCfg.Sites = sites
		wpCfg.Logger = logger

		wp := queue.NewWorkerPool(redisClients.Queue, wpCfg)

		// Register search.sync handler when running in Redis-only mode.
		if kafkaCfg.Enabled == nil || !*kafkaCfg.Enabled {
			searchClient, err := search.NewClient(searchCfg)
			if err == nil {
				defer searchClient.Close()
				syncer := search.NewSyncer(searchClient, registry, kafkaCfg, logger)
				wp.Handle(search.JobTypeSearchSync, syncer.JobHandler)
				logger.Info("worker: registered search.sync handler (Kafka disabled)")
			} else if !errors.Is(err, search.ErrUnavailable) {
				logger.Warn("worker: search sync disabled", slog.String("error", err.Error()))
			}
		}

		// Webhook delivery handler.
		webhookDispatcher := api.NewWebhookDispatcher(
			queue.NewProducer(redisClients.Queue, logger),
			dbManager,
			logger,
		)
		wp.Handle(api.JobTypeWebhookDelivery, webhookDispatcher.DeliveryHandler)

		// Default handler for unrecognised job types.
		wp.Handle("_default", func(_ context.Context, job queue.Job) error {
			logger.Info("worker: processed job",
				slog.String("type", job.Type),
				slog.String("id", job.ID),
				slog.String("site", job.Site),
			)
			return nil
		})

		logger.Info("worker pool started")
		return wp.Run(ctx)
	}
}

// SchedulerSubsystem creates a supervisor-compatible run function for the
// cron scheduler with leader election.
func SchedulerSubsystem(
	redisClients *drivers.RedisClients,
	schedulerCfg config.SchedulerConfig,
	logger *slog.Logger,
) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		if !schedulerCfg.Enabled {
			logger.Info("scheduler: disabled in config")
			<-ctx.Done()
			return nil
		}

		producer := queue.NewProducer(redisClients.Queue, logger)

		var opts []queue.SchedulerOption
		opts = append(opts, queue.WithSchedulerLogger(logger))
		if schedulerCfg.TickInterval != "" {
			d, err := time.ParseDuration(schedulerCfg.TickInterval)
			if err != nil {
				return fmt.Errorf("scheduler: invalid tick_interval: %w", err)
			}
			opts = append(opts, queue.WithTickInterval(d))
		}
		scheduler := queue.NewScheduler(producer, opts...)

		// Cron entries will be registered by apps via the hook registry in
		// future milestones. For now the scheduler starts with zero entries,
		// matching the standalone moca-scheduler binary behaviour.

		le := queue.NewLeaderElection(redisClients.Queue, queue.LeaderElectionConfig{
			Logger: logger,
		})

		logger.Info("scheduler started (waiting for leader election)",
			slog.Int("cron_entries", scheduler.Entries()),
		)
		return scheduler.RunWithLeader(ctx, le)
	}
}

// OutboxSubsystem creates a supervisor-compatible run function for the
// transactional outbox poller with leader election and optional search sync.
func OutboxSubsystem(
	dbManager *orm.DBManager,
	redisClients *drivers.RedisClients,
	kafkaCfg config.KafkaConfig,
	searchCfg config.SearchConfig,
	registry *meta.Registry,
	logger *slog.Logger,
) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		producer, err := events.NewProducer(kafkaCfg, redisClients)
		if err != nil {
			return fmt.Errorf("outbox: create event producer: %w", err)
		}
		defer func() { _ = producer.Close() }()

		store := events.NewDBOutboxStore(dbManager)
		siteLister := &meta.DBSiteLister{DB: dbManager}

		var syncer *search.Syncer
		var afterPublish events.AfterPublishHook

		searchClient, searchErr := search.NewClient(searchCfg)
		switch {
		case searchErr == nil:
			defer searchClient.Close()
			syncer = search.NewSyncer(searchClient, registry, kafkaCfg, logger)
		case errors.Is(searchErr, search.ErrUnavailable):
			logger.Info("outbox: search backend not configured; search sync disabled")
		default:
			logger.Warn("outbox: search backend unavailable; search sync disabled",
				slog.String("error", searchErr.Error()),
			)
		}

		// When Kafka is disabled, route search sync through the job queue.
		if syncer != nil && (kafkaCfg.Enabled == nil || !*kafkaCfg.Enabled) {
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
			return fmt.Errorf("outbox: create poller: %w", err)
		}

		leader := queue.NewLeaderElection(redisClients.Queue, queue.LeaderElectionConfig{
			Logger: logger,
			Key:    "moca:outbox:leader",
		})

		logger.Info("outbox poller started")

		// If Kafka + search are both enabled, run the Kafka search sync consumer
		// concurrently with the outbox poller.
		if syncer != nil && kafkaCfg.Enabled != nil && *kafkaCfg.Enabled {
			errCh := make(chan error, 2)
			go func() {
				errCh <- leader.Run(ctx, poller.Run)
			}()
			go func() {
				errCh <- syncer.RunKafka(ctx)
			}()
			// Return when either finishes (context cancellation propagates).
			return <-errCh
		}

		return leader.Run(ctx, poller.Run)
	}
}

// enqueueSearchSync converts a document event into a search.sync job.
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

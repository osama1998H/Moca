package queue

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// WorkerPool manages consumer goroutines, the DLQ processor, and the
// delayed job promoter. Its Run method matches process.Subsystem.Run.
type WorkerPool struct {
	rdb      *redis.Client
	handlers map[string]JobHandler
	logger   *slog.Logger
	config   WorkerPoolConfig
}

// NewWorkerPool creates a WorkerPool. Call Handle() to register handlers
// before calling Run().
func NewWorkerPool(rdb *redis.Client, cfg WorkerPoolConfig) *WorkerPool {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if len(cfg.QueueTypes) == 0 {
		cfg.QueueTypes = AllQueueTypes
	}
	if cfg.ConsumersPerQueue <= 0 {
		cfg.ConsumersPerQueue = 2
	}
	return &WorkerPool{
		rdb:      rdb,
		config:   cfg,
		handlers: make(map[string]JobHandler),
		logger:   cfg.Logger,
	}
}

// Handle registers a handler for a job type. Must be called before Run().
func (wp *WorkerPool) Handle(jobType string, handler JobHandler) {
	wp.handlers[jobType] = handler
}

// Run starts all consumer goroutines, the DLQ processor, and the delayed
// promoter. Blocks until ctx is cancelled, then performs graceful shutdown.
//
// Signature matches process.Subsystem.Run.
func (wp *WorkerPool) Run(ctx context.Context) error {
	if len(wp.config.Sites) == 0 {
		wp.logger.Warn("worker pool started with no sites, waiting for shutdown")
		<-ctx.Done()
		return nil
	}

	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "worker"
	}

	// Start consumer goroutines.
	for _, site := range wp.config.Sites {
		group := GroupName(site)
		for _, qt := range wp.config.QueueTypes {
			stream := StreamKey(site, qt)
			for i := range wp.config.ConsumersPerQueue {
				name := fmt.Sprintf("%s-%s-%s-%d", hostname, site, string(qt), i)
				c := &consumer{
					rdb:           wp.rdb,
					stream:        stream,
					group:         group,
					name:          name,
					blockDuration: wp.config.BlockDuration,
					handlers:      wp.handlers,
					logger:        wp.logger,
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := c.run(childCtx); err != nil {
						wp.logger.Error("consumer exited with error",
							slog.String("consumer", c.name),
							slog.String("error", err.Error()),
						)
					}
				}()
			}
		}
	}

	// Start DLQ processor.
	dlq := &dlqProcessor{
		rdb:        wp.rdb,
		sites:      wp.config.Sites,
		queueTypes: wp.config.QueueTypes,
		maxRetries: wp.config.MaxRetries,
		interval:   wp.config.DLQInterval,
		logger:     wp.logger,
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := dlq.run(childCtx); err != nil {
			wp.logger.Error("DLQ processor exited with error",
				slog.String("error", err.Error()),
			)
		}
	}()

	// Start delayed job promoter.
	dp := &delayedPromoter{
		rdb:      wp.rdb,
		sites:    wp.config.Sites,
		maxLen:   wp.config.MaxLen,
		interval: wp.config.DelayedPollInterval,
		logger:   wp.logger,
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := dp.run(childCtx); err != nil {
			wp.logger.Error("delayed promoter exited with error",
				slog.String("error", err.Error()),
			)
		}
	}()

	// Start claimer for recovering orphaned messages.
	cl := &claimer{
		rdb:        wp.rdb,
		sites:      wp.config.Sites,
		queueTypes: wp.config.QueueTypes,
		minIdle:    wp.config.ClaimMinIdle,
		interval:   wp.config.ClaimInterval,
		handlers:   wp.handlers,
		logger:     wp.logger,
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := cl.run(childCtx); err != nil {
			wp.logger.Error("claimer exited with error",
				slog.String("error", err.Error()),
			)
		}
	}()

	totalConsumers := len(wp.config.Sites) * len(wp.config.QueueTypes) * wp.config.ConsumersPerQueue
	wp.logger.Info("worker pool started",
		slog.Int("consumers", totalConsumers),
		slog.Int("sites", len(wp.config.Sites)),
		slog.Int("queue_types", len(wp.config.QueueTypes)),
	)

	// Wait for shutdown signal.
	<-ctx.Done()
	wp.logger.Info("worker pool shutting down")
	cancel()
	wg.Wait()
	wp.logger.Info("worker pool stopped")
	return nil
}

// claimer periodically runs XAutoClaim to recover pending messages
// from dead consumers.
type claimer struct {
	rdb        *redis.Client
	handlers   map[string]JobHandler
	logger     *slog.Logger
	sites      []string
	queueTypes []QueueType
	minIdle    time.Duration
	interval   time.Duration
}

func (cl *claimer) run(ctx context.Context) error {
	ticker := time.NewTicker(cl.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			cl.sweep(ctx)
		}
	}
}

func (cl *claimer) sweep(ctx context.Context) {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "claimer"
	}

	for _, site := range cl.sites {
		group := GroupName(site)
		for _, qt := range cl.queueTypes {
			stream := StreamKey(site, qt)
			messages, _, err := cl.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
				Stream:   stream,
				Group:    group,
				Consumer: hostname + "-claimer",
				MinIdle:  cl.minIdle,
				Start:    "0-0",
				Count:    100,
			}).Result()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				cl.logger.Debug("XAutoClaim error",
					slog.String("stream", stream),
					slog.String("error", err.Error()),
				)
				continue
			}

			for _, msg := range messages {
				job, err := valuesToJob(msg.Values)
				if err != nil {
					_ = cl.rdb.XAck(ctx, stream, group, msg.ID).Err()
					continue
				}
				handler, ok := cl.handlers[job.Type]
				if !ok {
					_ = cl.rdb.XAck(ctx, stream, group, msg.ID).Err()
					continue
				}
				if err := handler(ctx, job); err == nil {
					_ = cl.rdb.XAck(ctx, stream, group, msg.ID).Err()
				}
			}

			if len(messages) > 0 {
				cl.logger.Info("reclaimed pending messages",
					slog.String("stream", stream),
					slog.Int("count", len(messages)),
				)
			}
		}
	}
}

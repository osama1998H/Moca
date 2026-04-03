package events

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/internal/drivers"
)

var (
	kafkaProducerFactory = func(cfg config.KafkaConfig) (Producer, error) {
		return newKafkaProducer(cfg)
	}
	redisProducerFactory = func(redisClients *drivers.RedisClients) (Producer, error) {
		return newRedisProducer(redisClients)
	}
)

// NewProducer returns a Kafka producer when Kafka is enabled, or a Redis
// pub/sub producer when Kafka is disabled.
func NewProducer(cfg config.KafkaConfig, redisClients *drivers.RedisClients) (Producer, error) {
	if cfg.Enabled != nil && *cfg.Enabled {
		producer, err := kafkaProducerFactory(cfg)
		if err != nil {
			return nil, err
		}
		return producer, nil
	}

	logMinimalModeWarning(slog.Default())

	producer, err := redisProducerFactory(redisClients)
	if err != nil {
		return nil, err
	}
	return producer, nil
}

func logMinimalModeWarning(logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}

	logger.Warn(
		"MINIMAL MODE: Kafka disabled. Feature impact: UNAVAILABLE: CDC, Event Replay; DEGRADED: Document events (no persistence), Webhooks (no ordering), Search sync (Redis fallback), Audit (DB only), Workflow transitions and notifications (fire-and-forget); UNCHANGED: MetaType cache flush. Set kafka.enabled=true to restore full functionality.",
	)
}

func marshalEvent(event DocumentEvent) ([]byte, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("events: marshal document event: %w", err)
	}
	return payload, nil
}

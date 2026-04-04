package events

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/internal/drivers"
)

type redisProducer struct {
	client *redis.Client
}

func newRedisProducer(redisClients *drivers.RedisClients) (*redisProducer, error) {
	if redisClients == nil || redisClients.PubSub == nil {
		return nil, fmt.Errorf("events: redis pubsub client is required")
	}
	return &redisProducer{client: redisClients.PubSub}, nil
}

func (p *redisProducer) Publish(ctx context.Context, topic string, event DocumentEvent) error {
	if topic == "" {
		return fmt.Errorf("events: topic is required")
	}
	if p == nil || p.client == nil {
		return fmt.Errorf("events: redis producer is not configured")
	}

	payload, err := marshalEvent(event)
	if err != nil {
		return err
	}
	if err := p.client.Publish(ctx, topic, payload).Err(); err != nil {
		return fmt.Errorf("events: redis publish topic %q: %w", topic, err)
	}
	return nil
}

func (p *redisProducer) Close() error {
	return nil
}

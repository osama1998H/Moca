package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/osama1998H/moca/internal/config"
)

type kafkaPublisher interface {
	ProduceSync(ctx context.Context, record *kgo.Record) error
	Close()
}

type franzPublisher struct {
	client *kgo.Client
}

func (p *franzPublisher) ProduceSync(ctx context.Context, record *kgo.Record) error {
	return p.client.ProduceSync(ctx, record).FirstErr()
}

func (p *franzPublisher) Close() {
	p.client.Close()
}

type kafkaProducer struct {
	client kafkaPublisher
}

func newKafkaProducer(cfg config.KafkaConfig) (*kafkaProducer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("events: kafka enabled but no brokers configured")
	}

	client, err := kgo.NewClient(kgo.SeedBrokers(cfg.Brokers...))
	if err != nil {
		return nil, fmt.Errorf("events: create kafka client: %w", err)
	}

	return newKafkaProducerWithClient(&franzPublisher{client: client}), nil
}

func newKafkaProducerWithClient(client kafkaPublisher) *kafkaProducer {
	return &kafkaProducer{client: client}
}

func (p *kafkaProducer) Publish(ctx context.Context, topic string, event DocumentEvent) error {
	if topic == "" {
		return fmt.Errorf("events: topic is required")
	}
	if p == nil || p.client == nil {
		return fmt.Errorf("events: kafka producer is not configured")
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("events: marshal document event: %w", err)
	}

	record := &kgo.Record{
		Topic: topic,
		Key:   []byte(PartitionKey(event.Site, event.DocType)),
		Value: payload,
	}
	if err := p.client.ProduceSync(ctx, record); err != nil {
		return fmt.Errorf("events: kafka publish topic %q: %w", topic, err)
	}

	return nil
}

func (p *kafkaProducer) Close() error {
	if p == nil || p.client == nil {
		return nil
	}
	p.client.Close()
	return nil
}

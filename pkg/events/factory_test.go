package events

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/internal/drivers"
)

type stubProducer struct{}

func (p *stubProducer) Publish(context.Context, string, DocumentEvent) error { return nil }
func (p *stubProducer) Close() error                                         { return nil }

func TestNewProducerSelectsKafkaWhenEnabled(t *testing.T) {
	oldKafkaFactory := kafkaProducerFactory
	oldRedisFactory := redisProducerFactory
	t.Cleanup(func() {
		kafkaProducerFactory = oldKafkaFactory
		redisProducerFactory = oldRedisFactory
	})

	want := &stubProducer{}
	kafkaProducerFactory = func(cfg config.KafkaConfig) (Producer, error) {
		if len(cfg.Brokers) != 1 || cfg.Brokers[0] != "kafka:9092" {
			t.Fatalf("unexpected brokers: %v", cfg.Brokers)
		}
		return want, nil
	}
	redisProducerFactory = func(_ *drivers.RedisClients) (Producer, error) {
		t.Fatal("redis factory should not be called")
		return nil, nil
	}

	enabled := true
	got, err := NewProducer(config.KafkaConfig{
		Enabled: &enabled,
		Brokers: []string{"kafka:9092"},
	}, nil)
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}
	if got != want {
		t.Fatalf("producer = %#v, want %#v", got, want)
	}
}

func TestNewProducerSelectsRedisWhenDisabled(t *testing.T) {
	oldKafkaFactory := kafkaProducerFactory
	oldRedisFactory := redisProducerFactory
	oldLogger := slog.Default()
	t.Cleanup(func() {
		kafkaProducerFactory = oldKafkaFactory
		redisProducerFactory = oldRedisFactory
		slog.SetDefault(oldLogger)
	})

	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))

	want := &stubProducer{}
	kafkaProducerFactory = func(config.KafkaConfig) (Producer, error) {
		t.Fatal("kafka factory should not be called")
		return nil, nil
	}
	redisProducerFactory = func(_ *drivers.RedisClients) (Producer, error) {
		return want, nil
	}

	disabled := false
	got, err := NewProducer(config.KafkaConfig{Enabled: &disabled}, nil)
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}
	if got != want {
		t.Fatalf("producer = %#v, want %#v", got, want)
	}

	logOutput := buf.String()
	if !containsAll(logOutput, "MINIMAL MODE", "CDC", "Event Replay", "MetaType cache flush") {
		t.Fatalf("expected minimal-mode warning, got %q", logOutput)
	}
}

func TestNewProducerNilKafkaEnabledFallsBackToRedis(t *testing.T) {
	oldKafkaFactory := kafkaProducerFactory
	oldRedisFactory := redisProducerFactory
	t.Cleanup(func() {
		kafkaProducerFactory = oldKafkaFactory
		redisProducerFactory = oldRedisFactory
	})

	want := &stubProducer{}
	kafkaProducerFactory = func(config.KafkaConfig) (Producer, error) {
		t.Fatal("kafka factory should not be called")
		return nil, nil
	}
	redisProducerFactory = func(_ *drivers.RedisClients) (Producer, error) {
		return want, nil
	}

	got, err := NewProducer(config.KafkaConfig{}, nil)
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}
	if got != want {
		t.Fatalf("producer = %#v, want %#v", got, want)
	}
}

func TestNewProducerKafkaEnabledWithoutBrokersErrors(t *testing.T) {
	enabled := true
	_, err := NewProducer(config.KafkaConfig{Enabled: &enabled}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !containsAll(got, "kafka enabled", "no brokers") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewProducerRedisRequiresPubSubClient(t *testing.T) {
	disabled := false
	_, err := NewProducer(config.KafkaConfig{Enabled: &disabled}, &drivers.RedisClients{})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !containsAll(got, "redis pubsub client", "required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewProducerRedisWithLiveClient(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), DB: 3})
	t.Cleanup(func() { _ = client.Close() })

	disabled := false
	producer, err := NewProducer(config.KafkaConfig{Enabled: &disabled}, &drivers.RedisClients{
		PubSub: client,
	})
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}
	if _, ok := producer.(*redisProducer); !ok {
		t.Fatalf("producer type = %T, want *redisProducer", producer)
	}
}

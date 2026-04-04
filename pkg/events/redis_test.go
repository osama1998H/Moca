package events

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisProducerPublish(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
		DB:   3,
	})
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	sub := client.Subscribe(ctx, TopicDocumentEvents)
	defer func() { _ = sub.Close() }()
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	producer := &redisProducer{client: client}
	event := DocumentEvent{
		EventID:   "evt-1",
		EventType: EventTypeDocCreated,
		Site:      "acme",
		DocType:   "SalesOrder",
		DocName:   "SO-0001",
		Action:    "insert",
	}
	if err := producer.Publish(ctx, TopicDocumentEvents, event); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	msg, err := sub.ReceiveMessage(ctx)
	if err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}
	if msg.Channel != TopicDocumentEvents {
		t.Fatalf("channel = %q, want %q", msg.Channel, TopicDocumentEvents)
	}

	var got DocumentEvent
	if err := json.Unmarshal([]byte(msg.Payload), &got); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}
	if got.EventID != event.EventID || got.DocName != event.DocName {
		t.Fatalf("got event = %+v, want %+v", got, event)
	}
}

package events

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestComposeHooks_AllNil(t *testing.T) {
	hook := ComposeHooks(nil, nil, nil)
	if hook != nil {
		t.Fatal("expected nil hook when all inputs are nil")
	}
}

func TestComposeHooks_SingleHook(t *testing.T) {
	called := false
	h := func(ctx context.Context, event DocumentEvent) error {
		called = true
		return nil
	}
	hook := ComposeHooks(nil, h, nil)
	if hook == nil {
		t.Fatal("expected non-nil hook")
	}
	if err := hook(context.Background(), DocumentEvent{}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("hook was not called")
	}
}

func TestComposeHooks_MultipleHooks_RunsInOrder(t *testing.T) {
	var order []int
	h1 := func(ctx context.Context, event DocumentEvent) error {
		order = append(order, 1)
		return nil
	}
	h2 := func(ctx context.Context, event DocumentEvent) error {
		order = append(order, 2)
		return nil
	}
	h3 := func(ctx context.Context, event DocumentEvent) error {
		order = append(order, 3)
		return nil
	}

	hook := ComposeHooks(h1, nil, h2, h3)
	if err := hook(context.Background(), DocumentEvent{}); err != nil {
		t.Fatal(err)
	}
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Fatalf("unexpected execution order: %v", order)
	}
}

func TestComposeHooks_ErrorStopsChain(t *testing.T) {
	sentinel := errors.New("hook error")
	h1 := func(ctx context.Context, event DocumentEvent) error {
		return sentinel
	}
	called := false
	h2 := func(ctx context.Context, event DocumentEvent) error {
		called = true
		return nil
	}

	hook := ComposeHooks(h1, h2)
	err := hook(context.Background(), DocumentEvent{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got: %v", err)
	}
	if called {
		t.Fatal("second hook should not have been called")
	}
}

func TestWebSocketPublishHook_NilClient(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hook := WebSocketPublishHook(nil, logger)
	if hook != nil {
		t.Fatal("expected nil hook for nil client")
	}
}

func TestWebSocketPublishHook_PublishesChannel(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hook := WebSocketPublishHook(client, logger)
	if hook == nil {
		t.Fatal("expected non-nil hook")
	}

	// Subscribe to the expected channel before publishing.
	ctx := context.Background()
	sub := client.Subscribe(ctx, "pubsub:doc:acme:SalesOrder:SO-001")
	defer func() { _ = sub.Close() }()

	// Wait for subscription to be active.
	ch := sub.Channel()

	event := DocumentEvent{
		EventID:   "evt-1",
		EventType: "doc.updated",
		Timestamp: time.Date(2026, 4, 6, 12, 0, 0, 0, time.UTC),
		Site:      "acme",
		DocType:   "SalesOrder",
		DocName:   "SO-001",
		Action:    "Update",
		User:      "admin@example.com",
	}

	if err := hook(ctx, event); err != nil {
		t.Fatal(err)
	}

	// Read the published message.
	select {
	case msg := <-ch:
		var got DocumentEvent
		if err := json.Unmarshal([]byte(msg.Payload), &got); err != nil {
			t.Fatal(err)
		}
		if got.Site != "acme" || got.DocType != "SalesOrder" || got.DocName != "SO-001" {
			t.Fatalf("unexpected event: %+v", got)
		}
		if msg.Channel != "pubsub:doc:acme:SalesOrder:SO-001" {
			t.Fatalf("unexpected channel: %s", msg.Channel)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for published message")
	}
}

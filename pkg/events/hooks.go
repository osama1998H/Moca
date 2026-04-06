package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

// ComposeHooks returns an AfterPublishHook that runs all non-nil hooks
// sequentially. If any hook returns an error, it is returned immediately
// and subsequent hooks are not called. Returns nil if no hooks are non-nil.
func ComposeHooks(hooks ...AfterPublishHook) AfterPublishHook {
	var active []AfterPublishHook
	for _, h := range hooks {
		if h != nil {
			active = append(active, h)
		}
	}
	switch len(active) {
	case 0:
		return nil
	case 1:
		return active[0]
	default:
		return func(ctx context.Context, event DocumentEvent) error {
			for _, h := range active {
				if err := h(ctx, event); err != nil {
					return err
				}
			}
			return nil
		}
	}
}

// WebSocketPublishHook returns an AfterPublishHook that publishes document
// events to Redis pub/sub channels for WebSocket relay. The channel format
// is pubsub:doc:{site}:{doctype}:{name} on Redis DB 3 (PubSub client).
//
// Errors are logged but not propagated — WebSocket delivery is best-effort
// and should never block the outbox pipeline.
// Returns nil if pubsubClient is nil.
func WebSocketPublishHook(pubsubClient *redis.Client, logger *slog.Logger) AfterPublishHook {
	if pubsubClient == nil {
		return nil
	}
	return func(ctx context.Context, event DocumentEvent) error {
		channel := fmt.Sprintf("pubsub:doc:%s:%s:%s", event.Site, event.DocType, event.DocName)
		payload, err := json.Marshal(event)
		if err != nil {
			logger.Warn("ws publish hook: marshal event failed",
				slog.String("error", err.Error()),
			)
			return nil
		}
		if err := pubsubClient.Publish(ctx, channel, payload).Err(); err != nil {
			logger.Warn("ws publish hook: redis publish failed",
				slog.String("channel", channel),
				slog.String("error", err.Error()),
			)
			return nil
		}
		return nil
	}
}

package serve

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"

	"github.com/osama1998H/moca/pkg/events"
	"github.com/redis/go-redis/v9"
)

// PubSubBridge subscribes to Redis pub/sub channels on DB 3 and relays
// document events to the WebSocket Hub. It dynamically manages PSUBSCRIBE
// patterns based on which doctypes have active WebSocket connections.
type PubSubBridge struct {
	hub         *Hub
	redisClient *redis.Client
	logger      *slog.Logger
	patterns    map[string]bool
	redisPubSub *redis.PubSub
	mu          sync.Mutex
}

// NewPubSubBridge creates a bridge between Redis pub/sub and the WebSocket hub.
func NewPubSubBridge(hub *Hub, redisClient *redis.Client, logger *slog.Logger) *PubSubBridge {
	return &PubSubBridge{
		hub:         hub,
		redisClient: redisClient,
		logger:      logger,
		patterns:    make(map[string]bool),
	}
}

// Run starts the bridge. It blocks until ctx is cancelled.
func (b *PubSubBridge) Run(ctx context.Context) error {
	b.mu.Lock()
	b.redisPubSub = b.redisClient.PSubscribe(ctx)
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		_ = b.redisPubSub.Close()
		b.redisPubSub = nil
		b.mu.Unlock()
	}()

	b.logger.Info("pubsub bridge started")

	ch := b.redisPubSub.Channel()
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			b.handleMessage(msg)
		case <-ctx.Done():
			return nil
		}
	}
}

// OnSubscriptionChange is the callback registered with the Hub. It dynamically
// adds or removes Redis PSUBSCRIBE patterns when doctype subscriptions change.
// key is "{site}:{doctype}".
func (b *PubSubBridge) OnSubscriptionChange(key string, active bool) {
	pattern := "pubsub:doc:" + key + ":*"

	b.mu.Lock()
	defer b.mu.Unlock()

	if active {
		if !b.patterns[pattern] {
			b.patterns[pattern] = true
			if b.redisPubSub != nil {
				_ = b.redisPubSub.PSubscribe(context.Background(), pattern)
			}
			b.logger.Debug("pubsub bridge: subscribed to pattern",
				slog.String("pattern", pattern),
			)
		}
	} else {
		if b.patterns[pattern] {
			delete(b.patterns, pattern)
			if b.redisPubSub != nil {
				_ = b.redisPubSub.PUnsubscribe(context.Background(), pattern)
			}
			b.logger.Debug("pubsub bridge: unsubscribed from pattern",
				slog.String("pattern", pattern),
			)
		}
	}
}

func (b *PubSubBridge) handleMessage(msg *redis.Message) {
	site, doctype, name, ok := parseDocChannel(msg.Channel)
	if !ok {
		b.logger.Warn("pubsub bridge: unparseable channel",
			slog.String("channel", msg.Channel),
		)
		return
	}

	envelope, err := buildBroadcastMessage(site, doctype, name, []byte(msg.Payload))
	if err != nil {
		b.logger.Warn("pubsub bridge: build message failed",
			slog.String("channel", msg.Channel),
			slog.String("error", err.Error()),
		)
		return
	}

	b.hub.Broadcast(site, doctype, envelope)
}

// parseDocChannel splits a Redis channel name of the form
// "pubsub:doc:{site}:{doctype}:{name}" into its components.
func parseDocChannel(channel string) (site, doctype, name string, ok bool) {
	const prefix = "pubsub:doc:"
	if !strings.HasPrefix(channel, prefix) {
		return "", "", "", false
	}
	rest := channel[len(prefix):]
	parts := strings.SplitN(rest, ":", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// buildBroadcastMessage constructs a client-facing JSON envelope from a raw
// DocumentEvent payload received via Redis pub/sub.
func buildBroadcastMessage(site, doctype, name string, payload []byte) ([]byte, error) {
	var event events.DocumentEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, err
	}

	msg := serverMessage{
		Type:      "doc_update",
		Site:      site,
		DocType:   doctype,
		Name:      name,
		EventType: event.EventType,
		User:      event.User,
		Timestamp: event.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	}
	return json.Marshal(msg)
}

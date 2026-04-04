package events

import "context"

// Producer publishes DocumentEvents to a concrete transport backend.
type Producer interface {
	Publish(ctx context.Context, topic string, event DocumentEvent) error
	Close() error
}

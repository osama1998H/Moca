package events

// Emitter publishes domain events to subscribers.
// The current implementation is a no-op placeholder; a full Kafka-backed
// implementation will be provided in MS-15.
type Emitter struct{}

// Emit publishes an event with the given topic and payload.
// This no-op implementation silently discards all events until MS-15.
func (e *Emitter) Emit(topic string, payload any) {}

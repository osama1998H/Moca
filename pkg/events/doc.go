// Package events implements MOCA's event producer layer.
//
// Events are published to Kafka when enabled, or to Redis pub/sub when Kafka
// is disabled for smaller deployments. The transactional outbox poller added
// later in MS-15 uses this package to publish durable document events after
// database commits.
//
// Key components:
//   - DocumentEvent: canonical event envelope for document lifecycle messages
//   - Producer: publish abstraction with Kafka and Redis implementations
//   - Emitter: backward-compatible shim used by current document runtime code
package events

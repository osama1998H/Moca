// Package events implements the MOCA Kafka event system with transactional outbox.
//
// To guarantee consistency between database writes and event publishing,
// MOCA writes events to an outbox table in the same transaction as the
// document save. The moca-outbox binary polls this table and publishes
// to Kafka, providing at-least-once delivery guarantees.
//
// Key components:
//   - Producer: Kafka message producer with topic routing
//   - Consumer: Kafka consumer with consumer group management
//   - Outbox: transactional outbox poller (used by cmd/moca-outbox)
//   - Schema: event schema definitions and serialization
package events

// Package queue implements the MOCA background job system using Redis Streams.
//
// Jobs are enqueued as Redis Stream entries and consumed by worker processes
// in consumer groups. Failed jobs are moved to a dead-letter queue (DLQ)
// for inspection and retry.
//
// Key components:
//   - Producer: enqueue jobs to named Redis Streams
//   - Consumer: worker pool with consumer group semantics and graceful shutdown
//   - Scheduler: cron-based job scheduling that enqueues jobs at defined intervals
//   - DeadLetter: DLQ management with configurable retry policies
package queue

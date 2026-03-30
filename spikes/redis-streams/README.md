# Spike 2: Redis Streams Consumer Groups

**Status:** Not started
**Task:** MS-00-T3
**Design Reference:** `MOCA_SYSTEM_DESIGN.md` §5.2 (lines 1073-1107), ADR-002

## Objective

Validate Redis Streams as a job queue replacement for dedicated brokers (RabbitMQ/SQS).
Use go-redis v9. Confirm consumer group semantics, at-least-once delivery, and
a dead-letter queue pattern are sufficient for MOCA's background job workloads.

> Note: The ROADMAP mentions "franz-go or go-redis" (line 112). Franz-go is a Kafka
> client, not a Redis client. This spike uses go-redis v9 exclusively.

## Key Questions to Answer

1. Do consumer groups correctly load-balance messages across multiple consumers?
2. Is at-least-once delivery reliable — are unacked messages re-delivered after consumer failure?
3. Can a dead-letter queue be implemented at the application level (XPendingExt + XClaim)?
4. How should delayed jobs (RunAfter) be handled — consumer-side skip-and-requeue vs ZADD?
5. How should streams be trimmed to prevent unbounded memory growth?

## Expected Deliverables

- `main.go` — producer/consumer implementation with DLQ pattern
- `main_test.go` — test suite covering all 7 scenarios
- `docker-compose.yml` — Redis 7 container
- `ADR-002-redis-streams-queue.md` — architecture decision record

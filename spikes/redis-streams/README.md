# Spike 2: Redis Streams Consumer Groups

[![CI](https://github.com/osama1998H/Moca/actions/workflows/ci.yml/badge.svg?branch=main&event=push)](https://github.com/osama1998H/Moca/actions/workflows/ci.yml)

**Status:** Completed
**Task:** MS-00-T3
**Design Reference:** `MOCA_SYSTEM_DESIGN.md` §5.2 (lines 1073-1107), ADR-002
**ADR:** `ADR-002-redis-streams-queue.md`

## Result

All 7 tests pass with the `-race` flag. 100 jobs enqueued, consumed with a consumer
group, all acknowledged. Dead-letter queue receives failed jobs after 3 retries.
Redis Streams with consumer groups are sufficient for MOCA's background job workloads.

## Key Findings

| Question | Answer |
|----------|--------|
| Consumer group load balancing? | Yes — Redis distributes messages naturally across consumers. 3 workers split 100 jobs 30/30/40 with zero duplicates. |
| At-least-once delivery reliable? | Yes — `XAutoClaim` reclaims unacked messages from crashed consumers. |
| DLQ via XPendingExt + XClaim? | Yes — messages with `RetryCount > maxRetries` move cleanly to the DLQ stream with metadata preserved. |
| RunAfter pattern viable? | Yes for short delays. Use ZADD sorted set for long delays to avoid PEL accumulation. |
| Stream memory management? | `MAXLEN ~N` trimming works. Target of 150 trimmed 200 entries to ≈156 (approximate by design). |

## ROADMAP Correction

ROADMAP.md line 112 says "verify franz-go or go-redis." **Franz-go is a Kafka client,
not a Redis client.** This spike uses go-redis v9 exclusively. See ADR for details.

## Deliverables

- `main.go` — `Job` struct, `Enqueue`, `Consume`, `ProcessDLQ`, `ClaimPending`
- `main_test.go` — 7 tests: `TestStreamNaming`, `TestJobProducer`, `TestConsumerGroup`, `TestMultipleConsumers`, `TestAtLeastOnceDelivery`, `TestDeadLetterQueue`, `TestDelayedExecution`
- `docker-compose.yml` — Redis 7 container on port 6380
- `ADR-002-redis-streams-queue.md` — architecture decision record

## Running the Spike

```bash
cd spikes/redis-streams
docker compose up -d
GOWORK=off go test -v -count=1 -race ./...
docker compose down
```

Or from the repo root:

```bash
make spike-redis
```

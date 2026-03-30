# ADR-002: Redis Streams as Job Queue over Dedicated Broker (RabbitMQ/SQS)

**Status:** Accepted
**Spike:** MS-00-T3
**Date:** 2026-03-30
**Validated by:** `GOWORK=off go test -v -count=1 -race ./...` — all 7 tests pass

---

## Context

MOCA needs a background job queue for tasks like email sending, report generation,
webhook delivery, and scheduled cron jobs. The design chose Redis Streams over
RabbitMQ/SQS because Redis is already required for caching (§5.1) and sessions.
Adding Streams avoids a second infrastructure dependency.

The fundamental risk: Redis Streams are less feature-rich than dedicated brokers.
This spike validates that consumer groups, at-least-once delivery, load balancing,
and a dead-letter queue pattern are sufficient for MOCA's workloads.

**Note:** ROADMAP.md line 112 mentions "verify franz-go or go-redis supports needed
semantics." Franz-go is a **Kafka** client, not a Redis client. This spike uses
**go-redis v9** exclusively. The ROADMAP reference is a documentation error.

---

## Decision

Use Redis Streams with consumer groups for all background job queuing.
Implement DLQ as an application-level pattern using `XPendingExt` + `XClaim` + `XAdd`.

### Stream Naming Convention

All streams are namespaced per-site, matching `MOCA_SYSTEM_DESIGN.md §5.2`:

| Stream Key                      | Purpose                              |
|---------------------------------|--------------------------------------|
| `moca:queue:{site}:default`     | General background tasks             |
| `moca:queue:{site}:long`        | Long-running tasks (reports, exports)|
| `moca:queue:{site}:critical`    | High-priority tasks (webhooks, email)|
| `moca:queue:{site}:scheduler`   | Cron-triggered tasks                 |
| `moca:deadletter:{site}`        | Failed tasks after max retries       |

### Job Serialization

Each Job field is stored as a separate Redis stream entry field (flat-map), not
a single JSON blob. The `Payload` field (which is `map[string]any`) is JSON-encoded.

**Rationale:** Flat-map serialization lets consumers inspect individual fields
(`type`, `site`, `run_after`) without full deserialization. This is valuable for
lightweight routing or monitoring consumers that only need to read one field.

### Dead-Letter Queue Pattern

1. `XPendingExt` returns per-message delivery counts from the Pending Entries List (PEL).
2. Messages with `RetryCount > maxRetries` are claimed via `XClaim`.
3. Job data plus DLQ metadata (`dlq_original_id`, `dlq_retry_count`) is written
   to the DLQ stream via `XAdd`.
4. The message is `XAck`'d in the original stream to remove it from the PEL.

### Delayed Execution

Two patterns were evaluated:

**Pattern A — Consumer-side RunAfter filtering (implemented):**
- Job carries a `RunAfter *time.Time` field in the stream entry.
- Consumer handler checks `time.Now().Before(*RunAfter)` and returns an error
  (no-ack) if the job is not yet ready, leaving it in the PEL.
- A second pass re-reads pending messages using `Streams: []string{stream, "0"}`.
- **Trade-off:** Simple code, but delayed jobs accumulate in the PEL and are
  re-delivered repeatedly, incrementing their delivery count. This can trigger
  false DLQ moves for jobs with long delays.

**Pattern B — ZADD sorted-set scheduler (validated in TestDelayedExecution):**
- Delayed jobs are enqueued to a sorted set `moca:delayed:{site}` with
  `score = RunAfter.UnixNano()`.
- A scheduler goroutine polls with `ZRANGEBYSCORE -inf <now>`, promotes ready
  jobs to the stream via `XAdd`, and removes them from the sorted set via `ZREM`.
  In production, this should use a Lua script for atomicity.
- **Trade-off:** Cleaner separation of concerns, no PEL accumulation. Adds a
  polling loop and a second data structure. Better for long delays (hours/days).

**Recommendation:** Use Pattern B (ZADD) for `moca-scheduler` (cron-triggered jobs
with known future times). Use Pattern A (RunAfter) only for short delays (< 30s)
where PEL accumulation is not a concern. The `moca:queue:{site}:scheduler` stream
feeds jobs that have already been promoted by the scheduler, so they have no RunAfter.

---

## Alternatives Considered

### Option A: RabbitMQ (Rejected)

- Provides native delayed queues, topic exchanges, priority queues, and dead-letter
  exchanges with zero application code.
- **Why rejected:** Adds a second required infrastructure dependency. Redis is already
  in the stack. Consumer group semantics are sufficient for MOCA's workloads. The
  incremental complexity of application-level DLQ is acceptable given the infrastructure
  savings.

### Option B: Amazon SQS (Rejected)

- Managed service with infinite scale, native DLQ, and message visibility timeout.
- **Why rejected:** Cloud-vendor lock-in. MOCA targets self-hosted deployments where
  SQS is not available.

### Option C: Kafka for job queuing (Rejected)

- Already in the stack for event streaming (ADR-003).
- **Why rejected:** Kafka is optimised for event fan-out and replay, not for
  dequeue-and-ack job processing. Consumer groups in Kafka use partition-level
  offsets, not per-message acknowledgments. Message replay and compaction work
  against typical job queue semantics (once consumed successfully, discard).
  Redis Streams are better suited for low-latency job consumption with per-message
  acknowledgment.

### Option D: Shared Pool with Per-Message SET vs Per-Consumer Group

Not applicable (Redis Streams do not have this distinction). Consumer groups
are the correct primitive — each group maintains an independent offset and PEL.

---

## Validation Results

| Test                     | Result | Key Observation                                               |
|--------------------------|--------|---------------------------------------------------------------|
| `TestStreamNaming`       | PASS   | Key format matches design spec; acme/globex streams independent|
| `TestJobProducer`        | PASS   | 100 jobs enqueued; MAXLEN ~150 trimmed 200 to ≈156 (approx)  |
| `TestConsumerGroup`      | PASS   | 50 jobs consumed + acked; XPENDING=0 after; XINFO shows group|
| `TestMultipleConsumers`  | PASS   | 100 jobs distributed: 30/30/40 across 3 workers, 0 duplicates|
| `TestAtLeastOnceDelivery`| PASS   | Worker-2 claimed all 10 unacked msgs via XAutoClaim; XPENDING=0|
| `TestDeadLetterQueue`    | PASS   | 5 msgs with RetryCount=4 moved to DLQ; PEL cleared; data intact|
| `TestDelayedExecution`   | PASS   | 3 immediate acked on pass 1; 2 delayed acked on pass 2; ZADD validated|

All tests passed with `-race`. No data races detected.

### Key Observations

**Load balancing is correct and natural.** Consumer groups distribute messages
across consumers without any application-level coordination. The distribution was
30/30/40 across 3 consumers for 100 jobs — not perfectly equal, but that is expected
with Redis's round-robin-ish distribution when consumers race.

**At-least-once delivery is reliable.** `XAutoClaim` with `MinIdle: 0` immediately
reclaimed all 10 unacknowledged messages from a "crashed" consumer. In production,
use `MinIdle: 30s` to avoid reclaiming messages that are still being processed by
a slow but alive consumer.

**DLQ pattern works cleanly.** `XPendingExt` provides the `RetryCount` field needed
to identify over-retried messages. The pattern of XClaim → XAdd to DLQ → XAck
is atomic enough for the spike's purposes; production should consider a Lua script
for true atomicity.

**MAXLEN approximate trimming is inexact by design.** Redis's approximate trimming
(`APPROX`) leaves some buffer for efficiency. A target of 150 resulted in 156 entries
after 200 enqueues. This is acceptable — `APPROX` is recommended over exact trimming
(`=`) for performance.

**ZADD sorted-set scheduler validated.** After 200ms sleep, `ZRANGEBYSCORE` correctly
identified all 3 jobs that had passed their scheduled time. The pattern is sound for
scheduled job promotion.

---

## Consequences for Production

### Stream Memory Management

- Enable approximate trimming (`MAXLEN ~N`) on every `XAdd` call.
- Recommended defaults: `default` and `critical` queues → 10,000 entries;
  `long` queue → 1,000 entries; `scheduler` queue → 1,000 entries.
- Consider `MINID`-based trimming keyed to the oldest pending message, which
  prevents trimming messages that are still in the PEL.

### Consumer Group Lifecycle

- Create groups at server startup with `XGroupCreateMkStream`. The `MKSTREAM`
  flag creates the stream if absent.
- Use `$` as the initial ID to consume only new messages (not historical).
  Use `0` during recovery or testing to process from the beginning.

### Consumer Failure Recovery

- Run a background "reclaimer" goroutine that calls `XAutoClaim` with
  `MinIdle: 30s` periodically to reclaim messages from dead consumers.
- Pair with `ProcessDLQ` (run less frequently, e.g., every minute) to move
  genuinely poisoned messages to the DLQ.

### Delayed Jobs

- Use `moca:delayed:{site}` sorted set + `moca-scheduler` poller for jobs
  with `RunAfter` times (all scheduled tasks).
- Avoid the consumer-side RunAfter filtering for delays longer than 30s to
  prevent false DLQ triggers from delivery count accumulation.

### Limitations vs Dedicated Brokers

| Feature             | Redis Streams                        | RabbitMQ                     |
|---------------------|--------------------------------------|------------------------------|
| Native delayed msgs | No (ZADD workaround)                 | Yes (DLE + x-message-ttl)    |
| Topic routing       | No (use separate streams)            | Yes (exchanges + routing keys)|
| Priority queues     | No (use separate streams)            | Yes (x-max-priority)         |
| Message TTL         | No (app must discard expired)        | Yes (per-message TTL)        |
| Persistence         | AOF/RDB (same as cache Redis)        | Durable queues + mirroring   |
| Max throughput      | ~1M msgs/s (single node)             | ~50K msgs/s typical          |

These limitations are acceptable for MOCA's workloads. If priority queues are
needed, use separate streams (`critical` > `default` > `long`) and let workers
poll in priority order. This is already in the stream naming design.

---

## References

- `MOCA_SYSTEM_DESIGN.md` §5.2 lines 1073-1107 — Queue Layer design (Job struct, WorkerPool, stream naming)
- `MOCA_SYSTEM_DESIGN.md` ADR-002 lines 2069-2073 — Decision rationale
- `ROADMAP.md` line 120 — Acceptance criteria for Spike 2
- go-redis v9.18.0 docs: `XAdd`, `XReadGroup`, `XAck`, `XPendingExt`, `XAutoClaim`, `XClaim`
- Redis Streams documentation: https://redis.io/docs/data-types/streams/

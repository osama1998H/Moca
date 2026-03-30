# Critical Blocker Resolution Strategies

This document provides the technical resolution strategy for each critical blocker identified in `ROADMAP.md`. Each solution is traced to the source design documents.

---

## Blocker 1: Go Workspace (`go.work`) Multi-App Composition

### Source
- `MOCA_SYSTEM_DESIGN.md` lines 1380-1384 (build composition model)
- `MOCA_SYSTEM_DESIGN.md` line 2056 (`go.work` in package layout)
- `MOCA_CLI_SYSTEM_DESIGN.md` lines 2287-2296 (`moca build server`)

### Problem
Each Moca app is a separate Go module with its own `go.mod`. The project root `go.work` composes all modules into a single `moca-server` binary. Go's Minimal Version Selection (MVS) resolves dependency versions globally across all workspace modules:

- **Minor/patch conflicts:** Go picks the highest version. If `apps/crm` needs `pkg v1.10.0` and `apps/accounting` needs `pkg v1.11.0`, Go uses `v1.11.0` for both. This is usually safe but can cause subtle runtime bugs if the newer version changed internal behavior.
- **Major version conflicts:** If two apps require different major versions of the same module (e.g., `v1.x` vs `v2.x`), the build fails because Go treats major versions as distinct module paths (`pkg` vs `pkg/v2`). This is actually handled correctly by Go modules.
- **Transitive explosion:** With 10+ apps, the probability of version conflicts increases exponentially.

### Solution Strategy

**Phase 1 (MS-00 Spike 3): Validate and document**
1. Create `apps/stub-a/go.mod` requiring `github.com/stretchr/testify v1.8.0`
2. Create `apps/stub-b/go.mod` requiring `github.com/stretchr/testify v1.9.0` (intentional minor conflict)
3. Verify `go build ./...` resolves to `v1.9.0` (MVS behavior)
4. Create a major version conflict test and document the failure mode
5. Write ADR documenting the resolution policy

**Phase 2 (MS-13 `moca app get`): Pre-install validation**
```go
// Before adding app to go.work, check compatibility:
func ValidateAppDependencies(appMod *modfile.File, workspaceMods []*modfile.File) []Conflict {
    conflicts := []Conflict{}
    for _, dep := range appMod.Require {
        for _, existing := range workspaceMods {
            for _, existingDep := range existing.Require {
                if dep.Mod.Path == existingDep.Mod.Path {
                    if majorVersion(dep.Mod.Version) != majorVersion(existingDep.Mod.Version) {
                        conflicts = append(conflicts, Conflict{
                            Package:    dep.Mod.Path,
                            NewVersion: dep.Mod.Version,
                            OldVersion: existingDep.Mod.Version,
                            App:        existing.Module.Mod.Path,
                        })
                    }
                }
            }
        }
    }
    return conflicts
}
```

**Phase 3 (escape hatch): `go.work` replace directives**
- `moca app get` can add `replace` directives to `go.work` when conflicts are detected
- Operator is warned and can accept or reject the resolution

### Acceptance Criteria for MS-00
- Happy path: Two modules with compatible deps compile into one binary
- Conflict path: Intentional conflict produces documented failure + resolution ADR
- Concurrent builds: `go build -race ./...` with multiple app modules passes

---

## Blocker 2: PostgreSQL Schema-Per-Tenant Isolation Under Concurrent Access

### Source
- `MOCA_SYSTEM_DESIGN.md` lines 830-832 (schema-per-tenant architecture)
- `MOCA_SYSTEM_DESIGN.md` line 1414 (`DBPool *pgxpool.Pool` in SiteContext)
- `MOCA_SYSTEM_DESIGN.md` lines 2063-2067 (ADR-001: Schema-Per-Tenant)
- `MOCA_SYSTEM_DESIGN.md` line 1962 (`postgres.go` in package layout)

### Problem
`SET search_path` is a PostgreSQL **session-level** setting. pgxpool reuses connections across goroutines. Three failure modes:

1. **Cross-tenant data leak:** Goroutine A sets `search_path = tenant_acme`, releases connection. Goroutine B acquires same connection (still set to `tenant_acme`) but should query `tenant_globex`. Without explicit reset, B reads A's data.

2. **Prepared statement cache poisoning:** pgx caches prepared statements per connection. A statement prepared against `tenant_acme` schema may be reused for `tenant_globex` if the SQL text matches, returning wrong-schema results.

3. **Race condition:** Two goroutines acquire the same connection simultaneously (shouldn't happen with pgxpool, but `search_path` state persists across acquisitions).

### Solution Strategy

**Architecture: Per-site pool registry (NOT a shared pool)**

The design already specifies `DBPool *pgxpool.Pool` per SiteContext (line 1414). This means each tenant gets its own pool with a fixed `search_path`. This is the safest approach:

```go
// pkg/orm/postgres.go

type DBManager struct {
    systemPool *pgxpool.Pool              // for moca_system queries
    sitePools  map[string]*pgxpool.Pool   // tenant_name -> dedicated pool
    mu         sync.RWMutex
}

// ForSite returns a pool permanently configured for this tenant's schema
func (m *DBManager) ForSite(ctx context.Context, site *SiteContext) (*pgxpool.Pool, error) {
    m.mu.RLock()
    if pool, ok := m.sitePools[site.Name]; ok {
        m.mu.RUnlock()
        return pool, nil
    }
    m.mu.RUnlock()

    // Lazy-create pool for this tenant
    m.mu.Lock()
    defer m.mu.Unlock()

    // Double-check after acquiring write lock
    if pool, ok := m.sitePools[site.Name]; ok {
        return pool, nil
    }

    config, err := pgxpool.ParseConfig(m.connString)
    if err != nil {
        return nil, err
    }

    // Set search_path permanently for ALL connections in this pool
    config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
        _, err := conn.Exec(ctx,
            fmt.Sprintf("SET search_path TO %s, public",
                pgx.Identifier{site.DBSchema}.Sanitize()))
        return err
    }

    // Pool size per tenant (configurable, with sane defaults)
    config.MaxConns = int32(m.perTenantMaxConns)

    pool, err := pgxpool.NewWithConfig(ctx, config)
    if err != nil {
        return nil, fmt.Errorf("create pool for site %s: %w", site.Name, err)
    }

    m.sitePools[site.Name] = pool
    return pool, nil
}
```

**Defense-in-depth: Connection-level assertion**
```go
// Every query execution asserts the correct schema
func assertSchema(ctx context.Context, conn *pgxpool.Conn, expected string) error {
    var actual string
    err := conn.QueryRow(ctx, "SELECT current_schema()").Scan(&actual)
    if err != nil {
        return err
    }
    if actual != expected {
        return fmt.Errorf("CRITICAL: schema mismatch: expected %s, got %s", expected, actual)
    }
    return nil
}
```

**Idle pool eviction (for 10,000+ tenant scale):**
```go
// Evict pools for tenants with no activity in the last 30 minutes
func (m *DBManager) EvictIdlePools(maxIdle time.Duration) {
    m.mu.Lock()
    defer m.mu.Unlock()
    for name, pool := range m.sitePools {
        if pool.Stat().IdleDuration() > maxIdle {
            pool.Close()
            delete(m.sitePools, name)
        }
    }
}
```

### Acceptance Criteria for MS-00 Spike 1
- 100 concurrent goroutines, 10 different tenant schemas, each reading/writing -- zero cross-contamination
- Prepared statements do not leak across per-tenant pools (separate pools = separate caches)
- Connection created for tenant A, released, re-acquired -- still has correct `search_path`
- Pool eviction works: idle tenant pool is closed after timeout; next request creates fresh pool

---

## Blocker 3: Config Sync Contract (YAML vs DB)

### Source
- `MOCA_SYSTEM_DESIGN.md` lines 1060-1072 (§5.1.1 Config Sync Contract)
- `MOCA_SYSTEM_DESIGN.md` lines 1050-1055 (pub/sub channel: `pubsub:config:{site}`)
- `MOCA_SYSTEM_DESIGN.md` lines 1205-1233 (transactional outbox pattern)
- `MOCA_CLI_SYSTEM_DESIGN.md` lines 1659-1692 (config get/set commands)
- `MOCA_CLI_SYSTEM_DESIGN.md` lines 1669-1674 (5-tier config resolution order)

### Problem
The contract states:
- **At rest:** YAML files on disk are the CLI's source of truth
- **At runtime:** PostgreSQL `moca_system.sites.config` JSONB is the server's source of truth
- `moca config set` must update **both** atomically

Five data-loss scenarios identified:

| # | Scenario | Root Cause | Impact |
|---|----------|-----------|--------|
| 1 | Server crash between YAML write and DB commit | Non-atomic dual-write | Config appears saved in YAML but server uses old DB value |
| 2 | DB commits but cache invalidation event fails | Event publication decoupled from TX | Server instances use stale cached config |
| 3 | Two CLI sessions run `moca config set` concurrently | Filesystem race condition | Last writer wins; first writer's change silently lost |
| 4 | `moca deploy update` syncs site A but crashes before site B | Partial multi-site sync | Mixed config state across sites |
| 5 | Manual YAML edit creates invalid syntax | No validation on raw file edits | CLI config commands fail; server unaffected (reads DB) |

### Solution Strategy

**Principle: DB-first, YAML-second, with distributed lock**

```
moca config set KEY VALUE --site acme
  1. Acquire Redis lock: moca:config:lock:acme (30s TTL)
  2. Read current config from DB (source of truth)
  3. Apply change in memory
  4. BEGIN PostgreSQL transaction
     a. UPDATE moca_system.sites SET config = $1 WHERE name = $2
     b. INSERT INTO moca_system.outbox (event_type, topic, payload)
        VALUES ('config_changed', 'pubsub:config:acme', '{"site":"acme"}')
  5. COMMIT transaction
  6. Write updated config to YAML (temp file + atomic rename)
  7. Release Redis lock

  On failure at step 5 (DB commit fails):
    → YAML was never written (step 6 not reached). No inconsistency.

  On failure at step 6 (YAML write fails after DB commit):
    → DB has new value, YAML has old value.
    → Recovery: moca config verify --site acme detects divergence and
      overwrites YAML from DB (DB is authoritative at runtime).

  Cache invalidation (step 4b):
    → Transactional outbox ensures event is published even if process
      crashes after COMMIT. moca-outbox poller retries until published.
```

**Multi-site deploy sync (for `moca deploy update`):**
```
moca deploy update
  1. Load ALL site configs from YAML files
  2. BEGIN single PostgreSQL transaction
     a. For each site: UPDATE moca_system.sites SET config = $1 WHERE name = $2
     b. For each site: INSERT outbox event
  3. COMMIT (all-or-nothing)
  4. Publish cache invalidation events (outbox poller handles retries)
```

**Recovery command:**
```
moca config verify --site acme
  1. Read config from DB
  2. Read config from YAML
  3. Compare checksums
  4. If different: overwrite YAML from DB (DB is authoritative)
  5. Report: "Config synchronized" or "No divergence detected"
```

### Implementation Location
- `internal/config/sync.go` (new file in MS-11)
- Redis lock: `moca:config:lock:{site}` with 30-second TTL
- Outbox table: reuse existing `tab_outbox` from §6.4 (line 1221)

### Acceptance Criteria for MS-11
- `moca config set` with simulated DB failure does NOT corrupt YAML
- `moca config set` with simulated YAML failure is recoverable via `moca config verify`
- Two concurrent `moca config set` calls are serialized (second waits or fails with "operation in progress")
- `moca deploy update` with 5 sites syncs all atomically (simulate crash after site 3 -- no partial state)
- `moca config get --resolved` matches `moca config get --runtime` after any successful `moca config set`

---

## Blocker 4: Kafka-Optional Fallback (No Silent Feature Loss)

### Source
- `MOCA_SYSTEM_DESIGN.md` lines 1235-1260 (§6.5 Kafka-Optional Architecture)
- `MOCA_SYSTEM_DESIGN.md` lines 1125-1150 (§6.1 Topic Design -- 8 topics)
- `MOCA_SYSTEM_DESIGN.md` lines 1205-1233 (§6.4 Transactional Outbox)
- `MOCA_SYSTEM_DESIGN.md` lines 1829-1837 (§12.3 Process Types)
- `MOCA_CLI_SYSTEM_DESIGN.md` lines 585-586 (`--no-kafka` flag)
- `MOCA_CLI_SYSTEM_DESIGN.md` lines 246-251 (moca.yaml kafka config)

### Problem
When `kafka.enabled=false`, the system must fall back to Redis pub/sub. The design document (§6.5) defines the feature-by-feature fallback table but does NOT specify how to prevent **silent** feature loss -- the scenario where an operator deploys with `--no-kafka` and never realizes that webhooks lost ordering guarantees, audit streaming is gone, or CDC is completely unavailable.

### Feature Classification

| Category | Features | Behavior Without Kafka |
|----------|---------|----------------------|
| **Unavailable** | CDC (`moca.cdc.*`), Event Replay, Multi-consumer fan-out | Completely disabled; no fallback possible |
| **Degraded (no persistence)** | Document events, Workflow transitions, Notifications | Redis pub/sub is fire-and-forget; events lost on Redis restart |
| **Degraded (no ordering)** | Webhook delivery | Synchronous dispatch from outbox poller; ordering not guaranteed |
| **Degraded (blocking)** | Search indexing | Synchronous on document save (adds latency to every write) |
| **Degraded (no streaming)** | Audit log | DB table only; no Kafka consumer for streaming analytics |
| **Unchanged** | MetaType cache flush | Redis pub/sub broadcast (same semantics as Kafka broadcast) |

### Solution Strategy: Three-Layer Detection

**Layer 1: Startup-time validation (in each process `main.go`)**

```go
// moca-search-sync: MUST NOT START without Kafka
func main() {
    cfg := loadConfig()
    if !cfg.Kafka.Enabled {
        log.Fatal("moca-search-sync requires Kafka. " +
            "When kafka.enabled=false, search sync is handled " +
            "synchronously by moca-server. Do not start this process.")
    }
}

// moca-server: Print feature matrix on startup
func main() {
    cfg := loadConfig()
    if !cfg.Kafka.Enabled {
        log.Warn("MINIMAL MODE: Kafka disabled. Feature impact:\n" +
            "  UNAVAILABLE: CDC, Event Replay\n" +
            "  DEGRADED: Document events (no persistence), " +
            "Webhooks (no ordering), Search (synchronous), " +
            "Audit (DB only)\n" +
            "  UNCHANGED: MetaType cache flush\n" +
            "  Set kafka.enabled=true to restore full functionality.")
    }
}
```

**Layer 2: Request-time feature gates**

```go
// When an API consumer tries to use CDC without Kafka:
func (api *CDCHandler) Subscribe(w http.ResponseWriter, r *http.Request) {
    if !api.kafkaEnabled {
        http.Error(w, "CDC requires Kafka. Set kafka.enabled=true in moca.yaml.",
            http.StatusServiceUnavailable)
        return
    }
}

// Webhook dispatch logs the mode:
func (w *WebhookDispatcher) Dispatch(ctx context.Context, hook *Webhook) {
    if !w.kafkaEnabled {
        log.Info("webhook_mode=synchronous_redis ordering=not_guaranteed",
            "url", hook.URL, "event", hook.Event)
    }
}
```

**Layer 3: Observable metrics (Prometheus)**

```go
// Expose kafka mode in metrics for alerting
var kafkaEnabledGauge = prometheus.NewGauge(prometheus.GaugeOpts{
    Name: "moca_kafka_enabled",
    Help: "Whether Kafka is enabled (1) or disabled (0)",
})

var featureAvailability = prometheus.NewGaugeVec(prometheus.GaugeOpts{
    Name: "moca_feature_available",
    Help: "Whether a feature is fully available (1) or degraded/unavailable (0)",
}, []string{"feature"})

// On startup:
func registerKafkaMetrics(enabled bool) {
    if enabled {
        kafkaEnabledGauge.Set(1)
        featureAvailability.WithLabelValues("cdc").Set(1)
        featureAvailability.WithLabelValues("event_replay").Set(1)
        featureAvailability.WithLabelValues("durable_events").Set(1)
    } else {
        kafkaEnabledGauge.Set(0)
        featureAvailability.WithLabelValues("cdc").Set(0)
        featureAvailability.WithLabelValues("event_replay").Set(0)
        featureAvailability.WithLabelValues("durable_events").Set(0)
    }
}
```

**Layer 4: `moca init --no-kafka` warning**

The CLI must print a clear feature-impact summary when `--no-kafka` is used, listing what becomes unavailable and degraded.

### Acceptance Criteria for MS-15
- `moca-search-sync` exits immediately with fatal error when `kafka.enabled=false`
- `moca-server` prints feature-matrix warning on startup when Kafka is disabled
- `GET /api/v1/cdc/subscribe` returns 503 with explanation when Kafka is disabled
- Prometheus metric `moca_kafka_enabled` is 0 when Kafka is disabled
- `moca init --no-kafka` prints feature-impact summary before proceeding
- All 8 Kafka topics from §6.1 have a documented Redis fallback behavior or "unavailable" status

---

## Summary

| Blocker | Resolution Milestone | Key Technical Decision | Risk After Resolution |
|---------|---------------------|----------------------|----------------------|
| Go workspace | MS-00 (Spike 3) | MVS for minor versions + `replace` for major + pre-install validation | Low (standard Go tooling) |
| PostgreSQL isolation | MS-00 (Spike 1), MS-02 | Per-tenant pool with `AfterConnect` search_path + idle eviction | Low (proven pgxpool pattern) |
| Config sync | MS-11 | DB-first write + Redis lock + transactional outbox for events + recovery command | Medium (complex multi-step, needs thorough testing) |
| Kafka fallback | MS-15 | Three-layer detection (startup, request-time, metrics) + explicit feature gates | Low (well-documented degradation) |

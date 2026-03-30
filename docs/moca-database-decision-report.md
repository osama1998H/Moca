# MOCA Database Decision Report

## Executive Summary

- **Recommended database:** PostgreSQL 16+
- **Confidence level:** High
- **Short reason:** The MOCA architecture is PostgreSQL-native by design. Every foundational pattern -- schema-per-tenant multitenancy, RLS defense-in-depth, sequence-based naming, range partitioning, JSONB dynamic fields, and transactional hook execution -- is built on PostgreSQL-specific capabilities. CockroachDB lacks critical features (RLS, schemas, declarative partitioning), introduces mandatory complexity (serializable-only isolation with forced retry logic), and would add ~16 weeks to the critical path with no v1.0 benefit. Multi-region distribution, CockroachDB's primary advantage, is explicitly deferred to post-v1.0 in both design documents.

---

## Project Requirements Extracted from Repo

| Requirement | Evidence | Why It Matters |
|---|---|---|
| Schema-per-tenant multitenancy | `MOCA_SYSTEM_DESIGN.md` ADR-001, section 4.1, section 8; `blocker-resolution-strategies.md` Blocker 2 | Foundational isolation primitive. Every tenant gets a PostgreSQL schema (`tenant_acme`). `SET search_path` via pgxpool `AfterConnect` callback. 10,000 tenants per cluster target. |
| Row-Level Security as defense-in-depth | `MOCA_SYSTEM_DESIGN.md` section 13.3 (Layer 5), line 710, ADR-001 | Named security layer in 7-layer defense model. Catches application-level bugs that might leak data across tenants. |
| PostgreSQL sequences for document naming | `MOCA_SYSTEM_DESIGN.md` section 3.2.3, line 462; `ROADMAP.md` MS-04 | Pattern naming (SO-0001, SO-0002) uses `nextval()` per tenant/doctype. Core business UX requirement inherited from Frappe. |
| Transactional outbox with BIGSERIAL ordering | `MOCA_SYSTEM_DESIGN.md` section 6.4, ADR-004 | Document save + Kafka event in single transaction. Outbox poller relies on monotonic BIGSERIAL ordering. |
| Range-partitioned audit log | `MOCA_SYSTEM_DESIGN.md` section 4.3, lines 967-979 | `tab_audit_log PARTITION BY RANGE (timestamp)` for efficient retention and query pruning. Immutable, append-only. |
| JSONB `_extra` column on every table | `MOCA_SYSTEM_DESIGN.md` section 4.4, ADR-005 | Dynamic custom fields without ALTER TABLE. GIN indexes for containment queries. Hot path for every document read/write. |
| 14+ lifecycle hooks executing inside DB transactions | `MOCA_SYSTEM_DESIGN.md` section 3.2.2, section 14 | `DocContext.TX *sql.Tx` passed through BeforeInsert, Validate, BeforeSave hooks. Arbitrary user-defined Go logic runs within open transaction. |
| Hot-reload MetaType triggering runtime DDL | `MOCA_SYSTEM_DESIGN.md` section 3.1.3 | ALTER TABLE, CREATE INDEX at runtime when MetaType definitions change. Must be fast and transactional. |
| Multi-table document saves | `MOCA_SYSTEM_DESIGN.md` section 14, lines 1905-1909 | BEGIN -> INSERT parent -> INSERT N children -> INSERT outbox -> COMMIT. 22+ statements per save for complex documents. |
| Workflow engine with cascading transitions | `MOCA_SYSTEM_DESIGN.md` section 3.6 | Auto-actions can create linked documents within the same transaction. Cascading multi-table writes. |
| v1.0 scale targets | `MOCA_SYSTEM_DESIGN.md` section 17 | 10K HTTP/instance, 5K writes/sec, 10K tenants, 50K WebSocket. All within single-node PostgreSQL capacity. |
| Multi-region explicitly deferred | `MOCA_SYSTEM_DESIGN.md` section 18, line 2152; `ROADMAP.md` line 1252 | "Investigate CockroachDB or Citus for distributed PostgreSQL" listed as post-v1.0 future revisit. |

---

## Agent Findings

### Agent 1 -- Architecture and Domain Model

**Focus:** Business model, document lifecycle, consistency requirements, hooks/events, tenancy, runtime patterns

**Key Findings:**

1. **Multi-table transactional writes are the norm.** A single SalesOrder save produces 22+ INSERT/UPDATE statements (parent + children + outbox) in one transaction. PostgreSQL handles these as local, single-node operations. CockroachDB would coordinate these across distributed ranges, adding latency on every document save.

2. **Lifecycle hooks execute inside open transactions.** The `DocContext.TX *sql.Tx` is passed through 14+ hooks (BeforeInsert, Validate, BeforeSave, etc.). These hooks run arbitrary user-defined logic -- querying other tables, validating business rules, computing fields. CockroachDB's mandatory serializable isolation turns every in-transaction read into a potential conflict point. PostgreSQL's READ COMMITTED avoids this entirely for read-validate patterns.

3. **Three patterns are individually disqualifying for CockroachDB:**
   - **No RLS**: Removes Layer 5 from the 7-layer defense-in-depth security model
   - **Sequence bottleneck**: The naming engine depends on fast, sequential `nextval()`. CockroachDB sequences require distributed consensus and are explicitly discouraged at scale
   - **Mandatory serializable isolation**: Conflicts with the hook system's assumption that reads within transactions are cheap and non-contentious

4. **Transactional outbox relies on BIGSERIAL monotonic ordering.** CockroachDB's `unique_rowid()` (what SERIAL maps to) is globally unique but not sequentially ordered, breaking the outbox poller's ordering assumption.

5. **Hot-reload MetaType triggers ALTER TABLE across all tenant schemas.** With 10,000 tenants, a core MetaType change generates 10,000 distributed schema change jobs in CockroachDB. PostgreSQL's `ADD COLUMN DEFAULT NULL` is near-instant metadata-only operation.

6. **Workflow cascading transitions** create the most complex transactional pattern: approval auto-actions creating linked documents, each triggering their own lifecycle hooks, all within one transaction. CockroachDB's distributed coordination and serializable isolation would cause unacceptable latency and high restart rates.

**Favored option:** PostgreSQL (strongly, across all 7 sub-dimensions)

**Repo evidence:** `MOCA_SYSTEM_DESIGN.md` sections 3.1-3.6 (subsystems), 4.1-4.4 (data architecture), 6.4 (outbox), 8 (multitenancy), 13.3 (security layers), 14 (request lifecycle), 18 (future revisit); `blocker-resolution-strategies.md` Blocker 2

---

### Agent 2 -- Persistence and SQL Compatibility

**Focus:** Schema design, joins, constraints, migrations, transactions, indexing, advanced SQL assumptions

**Key Findings:**

1. **Sequences and naming (Critical).** Line 462: "The naming engine uses PostgreSQL sequences for pattern-based naming (per tenant, per DocType)." With 10,000 tenants x 50 doctypes = 500,000 sequences, CockroachDB's distributed sequence model (consensus per `nextval()`) is a serious bottleneck. PostgreSQL sequences are near-zero overhead.

2. **Row-Level Security (Critical).** CockroachDB does NOT support `CREATE POLICY` or `ENABLE ROW LEVEL SECURITY`. The MOCA design names RLS in three critical locations: security architecture Layer 5, ADR-001, and permission resolution step 6. This is not a nice-to-have; it is an architecturally specified security control.

3. **Table partitioning (Critical).** `tab_audit_log` uses `PARTITION BY RANGE (timestamp)`. CockroachDB's partitioning model is for geo-distribution, not query optimization. No equivalent to PostgreSQL's declarative range partitioning. Audit log retention strategy (DROP old partitions) becomes row-by-row TTL deletion.

4. **Transactions and isolation (Critical).** CockroachDB enforces SERIALIZABLE isolation exclusively. The MOCA design shows no retry logic for serialization errors (40001). The `DocContext.TX` is a plain `*sql.Tx` with no retry wrapper. Adding retry logic would be a pervasive architectural change affecting every transaction path.

5. **JSONB and GIN indexes.** Both databases support JSONB and inverted/GIN indexes, but PostgreSQL's GIN implementation is more mature with `jsonb_path_ops` and `jsonb_ops` operator classes. The `_extra` column is queried on every table for every custom field access -- this is a hot path.

6. **Joins and query complexity.** MOCA's query engine with 15 operators, Link auto-joins, and permission filters assumes co-located tenant data (schema-per-tenant). CockroachDB distributes data across nodes regardless of schema, turning every Link auto-join into a potentially distributed join.

7. **Migrations and DDL.** PostgreSQL supports transactional DDL with instant `ADD COLUMN` and `CREATE INDEX CONCURRENTLY`. CockroachDB's distributed schema changes are slower, and mixing DDL with DML in the same transaction has restrictions.

8. **Full-text search.** The query engine uses `@@` (tsvector/tsquery) and trigram similarity (`pg_trgm`). CockroachDB's full-text support is newer and less complete; `pg_trgm` is not natively supported.

**Favored option:** PostgreSQL (strongly, in all 10 dimensions reviewed)

**Repo evidence:** `MOCA_SYSTEM_DESIGN.md` sections 3.2.3 (naming engine), 4.3 (table definitions), 4.4 (JSONB _extra), 10.1 (query engine), ADR-001, ADR-005; `MOCA_CLI_SYSTEM_DESIGN.md` section 4.2.5 (database operations); `blocker-resolution-strategies.md` Blocker 2

---

### Agent 3 -- Scalability and Distributed Systems

**Focus:** Horizontal scale, multi-region potential, failure domains, future scale assumptions

**Key Findings:**

1. **v1.0 scale targets do not require a distributed database.** 5,000 document writes/sec across all tenants, with aggressive Redis caching, is well within single-node PostgreSQL 16 capacity (10K-30K simple INSERT/sec on modern hardware). CockroachDB's distributed consensus overhead would actually reduce single-cluster write throughput.

2. **Horizontal scaling is at the application tier, not database tier.** The design scales horizontally via moca-server and moca-worker replicas (section 12.2). The database is a shared backend. This architecture works with PostgreSQL + read replicas; CockroachDB's distributed nodes are not needed.

3. **Multi-region is a legitimate future concern but explicitly deferred.** Section 18 line 2152: "investigate CockroachDB or Citus for distributed PostgreSQL." ROADMAP line 1252: "Post-v1.0." This carries ~10-15% weight in the decision -- real but not overriding.

4. **Failure domains are not uniquely a database problem.** MOCA depends on PostgreSQL, Redis, Kafka, and Meilisearch. PostgreSQL HA (Patroni, pg_auto_failover, managed services) provides near-zero RPO and seconds RTO. CockroachDB's built-in multi-node fault tolerance is genuinely better for DB HA, but the marginal improvement is small given the broader system topology.

5. **10,000 schemas in PostgreSQL is feasible with operational discipline.** The design already includes the escape valve: "large tenants get dedicated DB." PgBouncer + tenant migration to dedicated DBs keeps schema count manageable.

6. **Write patterns favor PostgreSQL.** Sequential naming (sequences), outbox polling (`WHERE status = 'pending'`), and BIGSERIAL auto-increment are all adversarial to CockroachDB's distributed consensus model.

7. **"PostgreSQL now, CockroachDB later" is a realistic path.** CockroachDB is wire-protocol compatible. The `pkg/orm/` abstraction layer provides the right seam for a future migration. The key is isolating PostgreSQL-specific features (sequences, RLS, partitioning) behind adapter interfaces.

**Favored option:** PostgreSQL for v1.0 (with abstraction boundaries to keep CockroachDB path open)

**Repo evidence:** `MOCA_SYSTEM_DESIGN.md` sections 12 (deployment), 17 (scalability targets), 18 (future revisit); `ROADMAP.md` MS-00 Spike 1, MS-02, post-v1.0 deferred items

---

### Agent 4 -- Developer Experience and Operations

**Focus:** Local development, testing, observability, backups, tooling maturity, deployment simplicity, migration workflows

**Key Findings:**

1. **Critical finding: CockroachDB does not support PostgreSQL schemas.** Agent 4 identified that CockroachDB uses a flat database model. `SET search_path` is irrelevant. MOCA's entire multitenancy model (schema-per-tenant, per-site pool with `AfterConnect` callback, `assertSchema()` defense) is incompatible with CockroachDB. This alone is disqualifying.

2. **Local development.** PostgreSQL: `brew install postgresql`, ~100 MB RAM, starts in seconds. CockroachDB: 2-4 GB RAM minimum, 3-node recommended deployment is 6-12 GB -- far too heavy alongside React dev server, Redis, Kafka, Meilisearch, MinIO.

3. **Testing.** Creating/dropping PostgreSQL schemas is near-instant, enabling schema-per-test isolation. CockroachDB would require database-per-test, which is substantially slower. The Go test ecosystem (testcontainers-go, dockertest) has first-class PostgreSQL support.

4. **Backup and restore.** `pg_dump --schema=tenant_acme` perfectly aligns with MOCA's per-tenant backup needs. CockroachDB backups are full-cluster in a proprietary binary format -- no per-tenant backup, no SQL syntax verification (`moca backup verify` step 3).

5. **Tooling maturity.** pgx is the gold-standard Go PostgreSQL driver. psql, pgAdmin, pg_stat_statements are battle-tested. CockroachDB compatibility via pgx works but with significant caveats (no schema support, different DDL behavior).

6. **Deployment simplicity.** PostgreSQL: single process, ~10 lines of Docker Compose. Managed services on every cloud at $15/mo. CockroachDB: minimum 3-node cluster, ~60 lines Docker Compose, managed services start at ~$300/mo.

7. **Migration workflows.** `moca db migrate` generates PostgreSQL-specific DDL. `moca db console` opens psql. `moca db diff` introspects PostgreSQL schemas. All of these assume PostgreSQL.

**Favored option:** PostgreSQL (strongly, across all 7 dimensions)

**Repo evidence:** `MOCA_CLI_SYSTEM_DESIGN.md` sections 4.2.5 (database ops), 4.2.6 (backup/restore), 4.2.9-4.2.10 (infra generation, dev tools), 4.2.11 (testing), ADR-CLI-006; `MOCA_SYSTEM_DESIGN.md` ADR-001, section 4.1, section 11 (observability), section 12 (deployment); `ROADMAP.md` MS-00 Spike 1, MS-02, MS-25

---

### Agent 5 -- Roadmap and Delivery Risk

**Focus:** Milestone impact, critical path, blocker resolution, open questions, team expertise, delivery timeline, migration risk

**Key Findings:**

1. **7 of 30 milestones require significant redesign for CockroachDB.** MS-00 (spike rewrite), MS-02 (connection pool + retry logic), MS-03 (DDL generation + partitioning), MS-04 (naming engine), MS-05 (query builder), MS-12 (entire multitenancy model), MS-14 (RLS removal). Five of these are on the critical path.

2. **~9.5 weeks added to critical path.** Critical path milestones affected: MS-00 (+1w), MS-02 (+2.5w), MS-03 (+1.5w), MS-04 (+1.5w), MS-12 (+3w). Total critical path extension: 9.5 weeks, from 72 to 81.5 weeks.

3. **~16 weeks total added to timeline.** Including non-critical-path items (RLS removal, query builder, learning curve, additional integration testing). Timeline increases from 72 to ~88 weeks (12-14 months -> 17-19 months). A **22% schedule increase.**

4. **Blocker 2 is not resolved, it is replaced.** CockroachDB eliminates the `search_path` cross-contamination blocker but introduces a worse one: "How to ensure every query includes tenant_id without RLS as a safety net?" The PostgreSQL blocker has a proven solution; the CockroachDB alternative is harder.

5. **Hiring risk is materially higher with CockroachDB.** Go + PostgreSQL is an extremely common hiring profile. Go + CockroachDB is niche. A 2-4 person team cannot afford the learning curve overhead. Debugging CockroachDB issues requires understanding distributed systems internals (consensus, ranges, leases).

6. **PostgreSQL risk (multi-region later) is more manageable than CockroachDB risk (serialization performance now).** A deferred migration is a planning problem with known solutions (Citus, read replicas, application sharding). Serialization conflicts are an immediate engineering crisis that could block v1.0 release.

7. **Post-v1.0 migration to CockroachDB is feasible** if the data access layer is properly abstracted. Key: define `TenantIsolator`, `SequenceProvider`, and `Dialect` interfaces. Adds ~1-2 weeks to PostgreSQL timeline but saves months if migration is later needed.

**Favored option:** PostgreSQL (strongly, across all 8 dimensions)

**Repo evidence:** `ROADMAP.md` (all 30 milestones, critical path, open questions OQ-1/OQ-2); `ROADMAP_VALIDATION_REPORT.md`; `docs/blocker-resolution-strategies.md` (all 4 blockers); `docs/roadmap-gap-fix-summary.md`

---

## Head-to-Head Comparison for MOCA

| Dimension | PostgreSQL | CockroachDB | Winner for MOCA |
|---|---|---|---|
| **Transaction semantics** | READ COMMITTED default. No forced retries. Multi-table writes are local. Hooks execute within TX with minimal contention. | SERIALIZABLE only. Mandatory application-level retry logic (40001). Every in-transaction read is a conflict point. Distributed coordination for multi-table writes. | **PostgreSQL** |
| **Schema/migrations** | Transactional DDL. Instant `ADD COLUMN DEFAULT NULL`. `CREATE INDEX CONCURRENTLY`. Schema introspection via `information_schema`. | Distributed schema changes (slower). Limited DDL in transactions. No `CREATE INDEX CONCURRENTLY` (uses own online mechanism). | **PostgreSQL** |
| **SQL compatibility** | Native: JSONB with GIN, `@@` full-text, `pg_trgm` trigrams, range partitioning, sequences, RLS, `SET search_path`. | Supports: JSONB with inverted indexes. Missing: RLS, PostgreSQL schemas, `pg_trgm`, declarative range partitioning. Different: sequences (distributed, slow), SERIAL (unique_rowid). | **PostgreSQL** |
| **Multi-tenancy** | Schema-per-tenant with `SET search_path`. RLS as defense-in-depth. Per-tenant `pg_dump`. Per-tenant connection pools. Proven, validated in design (Blocker 2 resolution). | No PostgreSQL schemas. Must use database-per-tenant (operational overhead) or row-level tenant_id (no RLS safety net). Per-tenant backup not natively supported. | **PostgreSQL** |
| **Horizontal scaling** | Single node + read replicas. Application tier scales horizontally. Schema-per-tenant provides natural data partitioning. Sufficient for v1.0 targets. | Built-in distributed SQL across nodes. Automatic data distribution. Better for multi-region. Overkill for v1.0 targets and adds latency. | **CockroachDB** (but not needed for v1.0) |
| **Local dev** | ~100 MB RAM. `brew install postgresql`. Single process. Starts in seconds. Ubiquitous Docker support. | 2-4 GB RAM minimum. 3-node recommended. Heavy for developer laptops alongside full MOCA stack. | **PostgreSQL** |
| **Operational burden** | Single node to manage. pg_dump/pg_restore for backups. WAL archiving for PITR. Mature managed services on every cloud ($15/mo entry). Well-understood by ops teams. | Minimum 3-node cluster. Proprietary backup format. Certificate management between nodes. Fewer managed options ($300+/mo). Requires distributed systems expertise. | **PostgreSQL** |
| **Failure modes** | Single node = single point of failure. Mitigated by streaming replication + automatic failover (Patroni, pg_auto_failover, managed HA). Near-zero RPO, seconds RTO. | Built-in multi-node fault tolerance via Raft. No single point of failure. Genuinely better for DB HA. But marginal improvement in a system with Redis, Kafka, Meilisearch dependencies. | **CockroachDB** (marginal advantage) |
| **Cost/complexity** | Low. Single process, low resource requirements. Extensive free tooling. Huge community. | High. 3+ nodes, higher resource requirements. Vendor-specific tooling. Smaller community. Higher managed service costs. | **PostgreSQL** |
| **Future flexibility** | Can migrate to CockroachDB or Citus for multi-region. `pkg/orm/` abstraction layer provides the seam. Known migration path. | Already distributed, but locked into serializable-only isolation, no RLS, no schemas. Harder to add these features than to add distribution to PostgreSQL. | **PostgreSQL** (with abstraction) |

---

## Final Recommendation

### PostgreSQL 16+

This is the right database for MOCA at every stage from development through v1.0 and beyond, until a specific customer requirement demands multi-region active-active writes.

**Why PostgreSQL is the best fit for MOCA specifically:**

1. **The architecture was designed for PostgreSQL.** This is not a database-agnostic design that happens to mention PostgreSQL. The system design documents make 15+ PostgreSQL-specific architectural decisions: schema-per-tenant (ADR-001), JSONB `_extra` (ADR-005), RLS defense-in-depth, sequence-based naming, range partitioning, transactional outbox with BIGSERIAL, `SET search_path` via pgxpool, `pg_trgm` for search, transactional DDL for hot-reload, and more. These are not preferences; they are load-bearing architectural pillars.

2. **CockroachDB lacks three features that MOCA architecturally requires.** Row-Level Security (security Layer 5), PostgreSQL schemas (multitenancy foundation), and declarative range partitioning (audit log). These are not workarounds or nice-to-haves -- they are named in the architecture as specific mechanisms the system depends on.

3. **The v1.0 scale targets are comfortably within PostgreSQL's capacity.** 5,000 writes/sec across 10,000 tenants with Redis caching is a moderate PostgreSQL workload. CockroachDB's distributed SQL adds latency without providing needed capacity.

4. **CockroachDB would add ~16 weeks (22%) to the delivery timeline.** For a pre-revenue framework project with 2-4 developers, 4-5 additional months of development is potentially existential.

5. **The multi-region path remains open.** By abstracting tenant isolation, sequence generation, and DDL dialect behind interfaces, MOCA can migrate to CockroachDB or Citus when (and if) a specific customer requires geo-distribution.

---

## Rejected Alternative

### CockroachDB

**Why CockroachDB is the weaker choice for MOCA:**

1. **Architectural incompatibility.** CockroachDB does not support PostgreSQL schemas, which is the foundation of MOCA's multitenancy model. Adopting CockroachDB would require replacing schema-per-tenant with either database-per-tenant (operational overhead at 10,000 tenants) or row-level tenant_id discrimination (no RLS safety net, bug-prone).

2. **Missing security layer.** No Row-Level Security eliminates Layer 5 of MOCA's 7-layer defense-in-depth model. This is not a feature gap; it is a security control removal with no database-level equivalent.

3. **Serializable isolation friction.** CockroachDB's mandatory SERIALIZABLE isolation conflicts with MOCA's transaction model, where 14+ lifecycle hooks execute arbitrary logic within open transactions. Every in-transaction read becomes a potential conflict point. The design includes no retry logic for serialization errors, and adding it would be a pervasive architectural change.

4. **Sequence performance at scale.** 500,000 sequences (10,000 tenants x 50 doctypes) with distributed consensus per `nextval()` is a known CockroachDB bottleneck. The naming engine would need redesign.

5. **Operational overhead.** Minimum 3-node cluster, higher resource requirements, fewer managed service options, higher cost, smaller community, niche hiring pool -- all for distributed capabilities not needed at v1.0.

6. **The design itself rejects CockroachDB for v1.0.** Section 18 lists it as a future investigation. The ROADMAP defers it to post-v1.0. ADR-001 chose PostgreSQL schemas. MS-00 Spike 1 validates PostgreSQL-specific behavior. Every CLI command assumes PostgreSQL.

---

## Decision Risks

### Risk 1: Multi-region demand arrives before v2.0
- **Probability:** Low-Medium
- **Impact:** Would require accelerating the CockroachDB/Citus migration
- **Mitigation:** Build abstraction interfaces (TenantIsolator, SequenceProvider, Dialect) now. Evaluate Citus (PostgreSQL extension) as a lower-friction path to distribution. PostgreSQL read replicas in multiple regions may suffice for read-heavy geo-distribution.

### Risk 2: 10,000 schemas exceed PostgreSQL catalog performance
- **Probability:** Low-Medium
- **Impact:** DDL operations slow down; `pg_catalog` queries become expensive
- **Mitigation:** Design already includes "large tenants get dedicated DB" escape valve. Implement tenant tier routing early. Monitor catalog size. PgBouncer for connection pooling.

### Risk 3: PostgreSQL HA not sufficient for enterprise SLA
- **Probability:** Low
- **Impact:** Enterprise customer requires zero-downtime guarantee
- **Mitigation:** PostgreSQL HA with synchronous replication provides RPO=0, RTO<30s. Patroni or cloud-managed HA (RDS Multi-AZ, Cloud SQL HA) is battle-tested. If insufficient, CockroachDB migration is the escape path.

### Risk 4: CockroachDB improves PostgreSQL compatibility significantly
- **Probability:** Medium (CockroachDB actively adds PostgreSQL features)
- **Impact:** Positive -- makes future migration easier
- **Mitigation:** Not a risk to the PostgreSQL decision; it validates the "PostgreSQL now, CockroachDB later" strategy.

---

## Trigger Points for Re-evaluation

Re-evaluate this decision if any of the following occur:

1. **A signed enterprise customer requires multi-region active-active writes** with data sovereignty constraints (e.g., EU data must stay in EU, US data in US, with cross-region queries).

2. **Tenant count exceeds 5,000 on a single cluster** and PostgreSQL catalog performance degrades despite optimization (tenant tiering, PgBouncer, dedicated DB offloading).

3. **CockroachDB adds Row-Level Security and PostgreSQL schema support**, eliminating the two largest compatibility gaps.

4. **A competitor framework on CockroachDB demonstrates meaningful market advantage** from built-in multi-region capability.

5. **PostgreSQL HA proves insufficient** for a specific deployment scenario that Patroni/managed HA cannot address.

None of these trigger points are expected before v1.0. The earliest realistic trigger is #1, which depends on enterprise customer acquisition.

---

## Actionable Next Steps

### Immediate (during v1.0 development)

1. **Define a `Dialect` interface in `pkg/orm/`** that abstracts database-specific SQL generation (DDL syntax, sequence operations, partitioning, full-text search operators). The PostgreSQL implementation is the only one needed for v1.0, but the interface preserves the migration path.

2. **Define a `TenantIsolator` interface** that abstracts the mechanism of tenant data isolation. The PostgreSQL implementation uses schema-per-tenant with `SET search_path`. A future CockroachDB implementation could use database-per-tenant or tenant_id column injection.

3. **Define a `SequenceProvider` interface** for the naming engine. The PostgreSQL implementation uses `CREATE SEQUENCE` / `nextval()`. A future distributed implementation could use Redis-backed atomic counters or snowflake-style generators.

4. **Do not scatter PostgreSQL-specific SQL throughout business logic.** Confine all database-specific operations to `pkg/orm/` and `pkg/meta/migrator.go`. The query builder, transaction manager, and migration runner should be the only code that emits raw SQL.

5. **Test security at the application layer, not just RLS.** Ensure the permission engine (MS-14) is tested independently of RLS policies. This way, removing RLS for a future CockroachDB migration does not break security tests.

### At scale (post-v1.0, if needed)

6. **Evaluate Citus before CockroachDB** for multi-region needs. Citus is a PostgreSQL extension that provides distributed queries while maintaining full PostgreSQL compatibility (schemas, RLS, sequences, partitioning). It may provide the distribution benefits without the compatibility costs.

7. **Conduct a formal migration spike** before committing to CockroachDB. Test the actual MOCA workload (document saves with hooks, naming sequences, outbox polling, permission filters) on a CockroachDB cluster to measure real latency and conflict rates.

8. **Consider tenant-level database routing** as a simpler alternative to full CockroachDB migration. Route tenants to region-specific PostgreSQL clusters based on their geographic location. This provides data sovereignty without changing the database engine.

---

*Report generated: 2026-03-30*
*Based on analysis of: MOCA_SYSTEM_DESIGN.md, MOCA_CLI_SYSTEM_DESIGN.md, ROADMAP.md, ROADMAP_VALIDATION_REPORT.md, docs/blocker-resolution-strategies.md, docs/roadmap-gap-fix-summary.md*
*Review methodology: 5 independent analysis tracks (Architecture/Domain, Persistence/SQL, Scalability/Distributed, DevEx/Operations, Roadmap/Risk) with evidence-based scoring*

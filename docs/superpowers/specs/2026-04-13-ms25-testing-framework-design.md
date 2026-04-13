# MS-25: Testing Framework, Coverage, and Test Data Generation

## Context

Before Moca reaches v1.0, all critical paths must be tested comprehensively. Currently, testing infrastructure exists in `internal/testutil/bench/` (IntegrationEnv, fixture builders) but is private and benchmark-focused. App developers have no public test helpers, no way to generate realistic test data from MetaType definitions, and no CLI commands for test orchestration.

MS-25 delivers: a public `pkg/testutils/` library, a MetaType-driven document factory, CLI test commands (`moca test run`, `moca test factory`, `moca test coverage`), and a comprehensive integration test suite covering document lifecycle, permissions, API, multitenancy, search, events, and workflow.

**Dependencies:** Assumes MS-00 through MS-24 are complete (including MS-23 Workflow Engine).

---

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Test isolation | Schema-per-test | Safest isolation, matches existing IntegrationEnv pattern |
| Data generation | `gofakeit` library | Locale-aware realistic data, reduces maintenance |
| Internal reuse | Promote `internal/testutil/bench/` into `pkg/testutils/` | Avoids duplication, builds on proven code |
| Test runner | Wrap `go test` | Standard Go tests, no proprietary framework |
| Approach | Library-first | Build pkg/testutils → CLI commands → integration tests |

---

## Deliverables

### 1. `pkg/testutils/` — Core Test Environment

**Files:** `env.go`, `options.go`, `helpers.go`, `services.go`

Promotes and extends `internal/testutil/bench/IntegrationEnv` into a public `TestEnv`.

**TestEnv struct:**
```go
type TestEnv struct {
    Ctx       context.Context
    Logger    *slog.Logger
    AdminPool *pgxpool.Pool
    DBManager *orm.DBManager
    Redis     *redis.Client
    Site      *tenancy.SiteContext
    User      *auth.User
    SiteName  string
    Schema    string
    // private: registry, docManager, siteManager, meilisearch (lazy init)
}
```

**Constructor:**
```go
func NewTestEnv(t testing.TB, opts ...EnvOption) *TestEnv
```
- Creates unique schema (`test_{prefix}_{counter}_{nanotime}`)
- Provisions PostgreSQL schema + `moca_system.sites` row
- Opens DB pool with `search_path` set to test schema
- Optional Redis/Meilisearch (skip-on-unavailable)
- Registers `t.Cleanup()` for automatic teardown (schema drop, Redis flush, pool close)

**EnvOption functions:**
```go
func WithPrefix(prefix string) EnvOption          // site name prefix (default: test name)
func WithUser(email, fullName string, roles ...string) EnvOption  // override default user
func WithBootstrap() EnvOption                     // run core.BootstrapCoreMeta on the test site
func WithApps(apps ...string) EnvOption            // install apps in test site
func WithMeilisearch() EnvOption                   // require Meilisearch client
```

**Helper methods:**
| Method | Signature | Purpose |
|--------|-----------|---------|
| `CreateTestUser` | `(t, email, fullName string, roles ...string) *auth.User` | Insert user doc with bcrypt password, assign roles |
| `LoginAs` | `(t, email string) *document.DocContext` | Return DocContext authenticated as given user |
| `NewTestDoc` | `(t, doctype string, values map[string]any) document.Document` | Create + insert doc via DocManager (full lifecycle) |
| `RegisterMetaType` | `(t, mt *meta.MetaType) *meta.MetaType` | Compile + register MetaType (promoted from bench) |
| `EnsureMetaTables` | `(t)` | Create per-tenant system tables (promoted from bench) |
| `Registry` | `() *meta.Registry` | Lazy accessor |
| `DocManager` | `() *document.DocManager` | Lazy accessor |
| `SiteManager` | `() *tenancy.SiteManager` | Lazy accessor |
| `RequireRedis` | `(t) *redis.Client` | Skip test if Redis unavailable |
| `RequireMeilisearch` | `(t) *meilisearch.Client` | Skip test if Meilisearch unavailable |
| `FlushRedis` | `(t, patterns ...string)` | Delete Redis keys matching patterns |
| `NewGatewayBundle` | `(t, rate *meta.RateLimitConfig) *GatewayBundle` | Build API gateway for HTTP tests |
| `ServicesAvailable` | `() bool` | Check if Docker services (PG/Redis) are reachable |

**Migration from bench:** Update `internal/testutil/bench/` to be a thin package that re-exports from `pkg/testutils` with benchmark-specific defaults. Existing benchmark files continue to compile.

### 2. `pkg/testutils/factory/` — Document Factory

**Files:** `factory.go`, `generators.go`, `deps.go`, `options.go`

MetaType-driven document generator using `gofakeit` for realistic values.

**DocFactory:**
```go
type DocFactory struct {
    registry  *meta.Registry
    faker     *gofakeit.Faker
    linkCache map[string][]string  // doctype -> cached document names
    depGraph  map[string][]string  // doctype -> Link field target doctypes
}

func New(registry *meta.Registry, opts ...Option) *DocFactory

type Option func(*config)
func WithSeed(seed int64) Option
func WithLocale(locale string) Option
```

**Generation API:**
```go
// Generate produces count valid document value maps.
func (f *DocFactory) Generate(ctx context.Context, site, doctype string, count int, opts ...GenOption) ([]map[string]any, error)

// GenerateAndInsert generates + inserts via DocManager.
func (f *DocFactory) GenerateAndInsert(ctx context.Context, env *testutils.TestEnv, doctype string, count int, opts ...GenOption) ([]document.Document, error)

type GenOption func(*genConfig)
func WithOverrides(overrides map[string]any) GenOption
func WithChildren(enabled bool) GenOption
func WithChildCount(min, max int) GenOption
```

**Field type → generator mapping:**

| FieldType | Generator |
|-----------|-----------|
| Data | Field-name heuristic: email→`Email()`, phone→`Phone()`, name→`Name()`, default→`Word()` |
| Text, LongText, Markdown, HTMLEditor | `Paragraph()` truncated to MaxLength |
| Code | `LoremIpsumSentence()` |
| Int | `IntRange(min, max)` from MinValue/MaxValue or (1, 10000) |
| Float, Currency | `Float64Range(min, max)` |
| Percent | `Float64Range(0, 100)` |
| Date | `DateRange(now-2y, now+1y)` → `"2006-01-02"` |
| Datetime | `DateRange()` → `time.RFC3339` |
| Time | `Date().Format("15:04:05")` |
| Duration | `Float64Range(0, 86400)` (seconds) |
| Select | Random pick from `strings.Split(Options, "\n")` |
| Link | Lookup linkCache or create referenced doc first (see dependency resolution) |
| DynamicLink | Generate matching doctype + name pair |
| Check | `Bool()` |
| Color | `HexColor()` |
| Rating | `Float64Range(0, 5)` |
| JSON | `{"key": "value", "num": 42}` template |
| Geolocation | `{"latitude": Latitude(), "longitude": Longitude()}` |
| Table | Recurse: generate 1-5 child rows from child MetaType |
| TableMultiSelect | Similar to Table |
| Attach, AttachImage | `/files/test/{doctype}/{uuid}.{ext}` placeholder |
| Password | `Password(true, true, true, true, false, 12)` |
| Signature | Base64 placeholder string |
| Barcode | `Numerify("############")` |

**Validation constraint enforcement:**
- `Required`: always generate a value (never nil/empty)
- `MaxLength`: truncate generated string
- `MinValue/MaxValue`: clamp numeric range
- `ValidationRegex`: retry generation up to 10 times or use `Regex()` generator
- `Unique`: append counter suffix if collision detected
- `Options` (Select): pick only from valid options

**Link field dependency resolution (`deps.go`):**
1. Walk all fields of target doctype, collect Link field → referenced doctype pairs
2. Build directed dependency graph (doctype A depends on doctype B if A has Link to B)
3. Detect cycles (skip circular Link fields or use existing cached docs)
4. Topological sort: generate leaf doctypes first
5. Cache created document names per doctype in `linkCache`
6. For Link fields, pick random name from cache

### 3. CLI Commands

**File: `cmd/moca/test_run.go`** — `moca test run`

```
Usage: moca test run [flags]

Flags:
  --site string       Test site name (auto-created if not specified)
  --app string        Test specific app
  --module string     Test specific module
  --doctype string    Test specific DocType's tests
  --parallel int      Parallel execution (default: GOMAXPROCS)
  --verbose           Verbose output (-v)
  --coverage          Generate coverage report
  --failfast          Stop on first failure
  --filter string     Run tests matching pattern (-run)
  --keep-site         Don't cleanup test site after run
```

Implementation:
1. `requireProject()` → get project config
2. Create ephemeral test site `testsite_{unix_timestamp}` via `SiteManager.CreateSite()`
3. Bootstrap core meta + install apps from project
4. Set env vars: `MOCA_TEST_SITE`, `MOCA_TEST_PG_HOST`, `MOCA_TEST_PG_PORT`, `MOCA_TEST_REDIS_ADDR`
5. Discover test packages: scan `apps/{app}/` for `*_test.go` files, or use `--app` filter
6. Build `go test` command with flags: `-tags integration`, `-race`, `-count=1`, `-run`, `-v`, `-failfast`, `-coverprofile`
7. Execute via `os/exec`, stream stdout/stderr to terminal
8. On exit: `SiteManager.DropSite()` unless `--keep-site`
9. Exit with `go test` exit code

**File: `cmd/moca/test_factory.go`** — `moca test factory`

```
Usage: moca test factory DOCTYPE [COUNT] [flags]

Flags:
  --site string       Target site (required)
  --locale string     Locale (default: "en")
  --seed int          Random seed (default: time-based)
  --with-children     Generate child data (default: true)
  --dry-run           Print JSON without inserting
  --batch-size int    Insert batch size (default: 50)
```

Implementation:
1. `requireProject()` + `resolveSiteName()` → get site
2. `newServices()` → construct service graph
3. Fetch MetaType from registry
4. Create `factory.New(registry, WithSeed(), WithLocale())`
5. If `--dry-run`: `factory.Generate()` → JSON to stdout
6. Else: `factory.GenerateAndInsert()` in batches of `--batch-size`
7. Print summary with count and elapsed time

**File: `cmd/moca/test_coverage.go`** — `moca test coverage`

```
Usage: moca test coverage [flags]

Flags:
  --app string        Coverage for specific app
  --output string     Output format: "text" (default), "html", "json"
  --threshold float   Minimum coverage % (default: 0)
  --packages string   Package patterns (default: "./pkg/...")
```

Implementation:
1. Run `go test -coverprofile=coverage.out -covermode=atomic` across target packages
2. Parse `coverage.out` using `golang.org/x/tools/cover` or manual parsing
3. Aggregate per-package: statements, covered, percentage
4. Display as table (using `internal/output` formatters)
5. If `--output html`: exec `go tool cover -html=coverage.out -o coverage.html`
6. If `--output json`: marshal per-package stats as JSON
7. If `--threshold > 0` and any package below: exit code 1 with warning

### 4. Integration Test Suite

**Location:** `pkg/testutils/integration/`
**Build tag:** `//go:build integration`

All tests follow the pattern:
```go
func TestXxx(t *testing.T) {
    env := testutils.NewTestEnv(t, testutils.WithBootstrap())
    // ... test logic using env helpers
}
```

**Test files and key test functions:**

| File | Key Tests |
|------|-----------|
| `document_lifecycle_test.go` | TestDocumentCRUD, TestSubmitCancel, TestNamingStrategies (all 6), TestChildTables, TestValidationErrors, TestLifecycleHookOrder (14 events) |
| `permissions_test.go` | TestRoleBasedCRUD, TestFieldLevelSecurity, TestRowLevelSecurity, TestPermissionEscalation, TestGuestAccess |
| `api_test.go` | TestRESTCrud, TestPagination, TestFilters, TestRateLimiting, TestBulkOperations, TestAPIVersioning, TestWebhooks |
| `multitenancy_test.go` | TestCrossTenantIsolation, TestSiteCreateDropCycle, TestSiteClone, TestSiteDisableEnable |
| `search_test.go` | TestMeilisearchIndexing, TestSearchQuery, TestSearchSync, TestMultiTenantSearch |
| `events_test.go` | TestEventPublish (Redis + Kafka), TestOutboxConsistency, TestEventOrdering, TestEventSubscription |
| `workflow_test.go` | TestLinearWorkflow, TestParallelBranches, TestQuorumApproval, TestSLATimerBreach, TestWorkflowHookBridge |
| `hooks_test.go` | TestHookPriority, TestHookDependencyOrder, TestHookErrorHandling, TestAppHookIsolation |
| `factory_test.go` | TestFactoryAllFieldTypes, TestFactoryLinkResolution, TestFactoryReproducibility, TestFactoryValidation, TestFactoryChildTables |
| `auth_test.go` | TestPasswordAuth, TestSSOFlow, TestAPIKeyAuth, TestSessionManagement, TestTokenRefresh |
| `queue_test.go` | TestJobEnqueueConsume, TestDLQ, TestScheduledJobs, TestLeaderElection, TestWorkerPool |
| `backup_test.go` | TestBackupRestoreCycle, TestIncrementalBackup |
| `migration_test.go` | TestSchemaMigration, TestFieldAddRemove, TestDDLGeneration, TestMigrationRollback |
| `helpers_test.go` | TestNewTestEnv, TestEnvCleanup, TestCreateTestUser, TestLoginAs, TestNewTestDoc |

### 5. Bench Migration

Update `internal/testutil/bench/` to import `pkg/testutils`:
- `IntegrationEnv` becomes a type alias or thin wrapper around `testutils.TestEnv`
- `SimpleDocType`, `ChildDocType`, `ComplexDocType` move to `pkg/testutils/factory/fixtures.go` as exported helpers
- `SimpleDocValues`, `ComplexDocValues` move similarly
- Existing benchmark files in `pkg/meta/`, `pkg/document/`, `pkg/orm/`, `pkg/api/`, `pkg/hooks/` updated to import from new locations

### 6. New Dependency

```
github.com/brianvoe/gofakeit/v7
```

Added to `go.mod` for the factory package.

---

## Files to Create

| File | Purpose |
|------|---------|
| `pkg/testutils/env.go` | TestEnv struct, NewTestEnv, cleanup logic |
| `pkg/testutils/options.go` | EnvOption type and With* functions |
| `pkg/testutils/helpers.go` | CreateTestUser, LoginAs, NewTestDoc, ServicesAvailable |
| `pkg/testutils/services.go` | GatewayBundle, StaticSiteResolver (promoted from bench) |
| `pkg/testutils/factory/factory.go` | DocFactory struct, Generate, GenerateAndInsert |
| `pkg/testutils/factory/generators.go` | Per-FieldType generator functions |
| `pkg/testutils/factory/deps.go` | Link field dependency graph + topological sort |
| `pkg/testutils/factory/options.go` | Option, GenOption types |
| `pkg/testutils/factory/fixtures.go` | SimpleDocType, ChildDocType, ComplexDocType (from bench) |
| `cmd/moca/test_run.go` | `moca test run` command implementation |
| `cmd/moca/test_factory.go` | `moca test factory` command implementation |
| `cmd/moca/test_coverage.go` | `moca test coverage` command implementation |
| `pkg/testutils/integration/*.go` | 14 integration test files |

## Files to Modify

| File | Change |
|------|--------|
| `cmd/moca/test_cmd.go` | Replace stub subcommands with real implementations |
| `internal/testutil/bench/integration.go` | Refactor to import from `pkg/testutils` |
| `internal/testutil/bench/fixtures.go` | Move fixtures to `pkg/testutils/factory/fixtures.go`, re-export |
| `go.mod` | Add `gofakeit/v7` dependency |

---

## Implementation Order

### Phase 1: Core Library (`pkg/testutils/`)
1. Create `pkg/testutils/env.go` — TestEnv with schema-per-test provisioning
2. Create `pkg/testutils/options.go` — EnvOption functions
3. Create `pkg/testutils/helpers.go` — CreateTestUser, LoginAs, NewTestDoc, ServicesAvailable
4. Create `pkg/testutils/services.go` — GatewayBundle, StaticSiteResolver
5. Write unit tests for TestEnv lifecycle (helpers_test.go stub)

### Phase 2: Document Factory (`pkg/testutils/factory/`)
6. Create `pkg/testutils/factory/options.go` — Option and GenOption types
7. Create `pkg/testutils/factory/generators.go` — Per-FieldType generators with gofakeit
8. Create `pkg/testutils/factory/deps.go` — Dependency graph and topological sort
9. Create `pkg/testutils/factory/factory.go` — DocFactory with Generate/GenerateAndInsert
10. Create `pkg/testutils/factory/fixtures.go` — Promoted bench fixtures
11. Add `gofakeit/v7` to `go.mod`

### Phase 3: CLI Commands
12. Implement `cmd/moca/test_run.go` — ephemeral site + go test wrapper
13. Implement `cmd/moca/test_factory.go` — factory CLI with dry-run
14. Implement `cmd/moca/test_coverage.go` — coverage aggregation
15. Update `cmd/moca/test_cmd.go` — wire real subcommands

### Phase 4: Integration Test Suite
16. Create `pkg/testutils/integration/helpers_test.go` — TestEnv self-tests
17. Create `pkg/testutils/integration/factory_test.go` — Factory validation tests
18. Create `pkg/testutils/integration/document_lifecycle_test.go` — CRUD + lifecycle
19. Create `pkg/testutils/integration/permissions_test.go` — RBAC/FLS/RLS
20. Create `pkg/testutils/integration/api_test.go` — REST API endpoints
21. Create `pkg/testutils/integration/multitenancy_test.go` — Tenant isolation
22. Create `pkg/testutils/integration/search_test.go` — Meilisearch
23. Create `pkg/testutils/integration/events_test.go` — Event publishing
24. Create `pkg/testutils/integration/workflow_test.go` — State machine + approvals
25. Create `pkg/testutils/integration/hooks_test.go` — Hook execution
26. Create `pkg/testutils/integration/auth_test.go` — Authentication flows
27. Create `pkg/testutils/integration/queue_test.go` — Job processing
28. Create `pkg/testutils/integration/backup_test.go` — Backup/restore
29. Create `pkg/testutils/integration/migration_test.go` — Schema migration

### Phase 5: Bench Migration + Cleanup
30. Refactor `internal/testutil/bench/integration.go` to re-export from `pkg/testutils`
31. Refactor `internal/testutil/bench/fixtures.go` to re-export from `pkg/testutils/factory`
32. Update existing benchmark imports across `pkg/meta/`, `pkg/document/`, `pkg/orm/`, `pkg/api/`, `pkg/hooks/`
33. Verify all existing tests and benchmarks still pass

---

## Verification

1. **Unit tests:** `go test -race ./pkg/testutils/...` passes
2. **Integration tests:** `make test-integration` runs new integration suite
3. **CLI commands:** 
   - `moca test run --app core --verbose` creates test site, runs tests, cleans up
   - `moca test factory User 10 --site testsite --dry-run` outputs valid JSON
   - `moca test coverage --packages ./pkg/...` shows per-package coverage table
4. **Factory validation:** Generated docs pass `Validator.ValidateDoc()` for all field types
5. **Bench compatibility:** `make bench` and `make bench-integration` still pass
6. **Lint:** `make lint` passes with no new warnings
7. **Coverage target:** `moca test coverage --threshold 70` exits 0 for pkg/

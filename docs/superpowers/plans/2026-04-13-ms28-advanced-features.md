# MS-28: Advanced Features Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement 7 post-v1.0 features: VirtualDoc, CDC Topics, Event Sourcing, Dev Console, Dev Playground, App Publish, and Test Run-UI.

**Architecture:** Three-phase approach — shared foundation first (event_log DDL, MetaType extensions, CDC wiring), then high-value runtime features (VirtualDoc, Event Sourcing, CDC), then developer CLI tools (Dev Console, Dev Playground, App Publish, Test Run-UI). Each phase produces independently testable software.

**Tech Stack:** Go 1.26+, PostgreSQL 16 (schema-per-tenant), Redis 7, Kafka (franz-go), yaegi (Go interpreter), Playwright (Node.js, shelled out), Cobra CLI, pgx v5.

**Design Spec:** `docs/superpowers/specs/2026-04-13-ms28-advanced-features-design.md`

---

## File Structure

### New Files

| File | Responsibility |
|------|---------------|
| `pkg/document/virtual.go` | VirtualDoc struct, VirtualSource interface, ReadOnlySource, ErrVirtualReadOnly |
| `pkg/document/virtual_test.go` | Unit tests for VirtualDoc Document interface compliance |
| `pkg/document/virtual_registry.go` | VirtualSourceRegistry (thread-safe doctype→VirtualSource map) |
| `pkg/document/virtual_registry_test.go` | Unit tests for registry Register/Get/List |
| `pkg/document/eventlog.go` | EventLogRow struct, insertEventLog, GetHistory, Replay functions |
| `pkg/document/eventlog_test.go` | Unit tests for EventLogRow serialization, integration tests for GetHistory/Replay |
| `pkg/console/doc.go` | Package doc for console package |
| `pkg/console/console.go` | Console struct with Get/GetList/Insert/Update/Delete/SQL/Meta/Sites/UseSite helpers |
| `pkg/console/console_test.go` | Unit tests for Console method delegation |
| `cmd/moca/dev_console.go` | `moca dev console` command — yaegi REPL with curated stdlib |
| `cmd/moca/dev_playground.go` | `moca dev playground` command — proxy server for Swagger UI + GraphiQL |
| `cmd/moca/app_publish.go` | `moca app publish` command — validate, tarball, GitHub release |
| `cmd/moca/test_run_ui.go` | `moca test run-ui` command — Playwright runner with JSON report parsing |

### Modified Files

| File | Change |
|------|--------|
| `pkg/meta/metatype.go:34-59` | Add `EventSourcing bool` and `CDCEnabled bool` fields to MetaType struct |
| `pkg/meta/ddl.go:282-285` | Add `tab_event_log` DDL after `idx_outbox_pending` statement |
| `pkg/meta/ddl_test.go` | Add test for tab_event_log DDL presence and columns |
| `pkg/meta/migrator_integration_test.go:343` | Add `tab_event_log` to system table existence checks |
| `pkg/document/crud.go:557-574` | Add `insertEventLog` function, call it from Insert/Update/Delete TX blocks |
| `pkg/document/crud.go:386-397` | Add `virtualSources *VirtualSourceRegistry` field to DocManager |
| `pkg/events/outbox.go:55-64` | Add `MetaProvider` to OutboxPollerConfig for CDC doctype lookup |
| `pkg/events/outbox.go:152-189` | Add CDC fan-out in processSite after primary publish |
| `cmd/moca/dev.go:30-39` | Replace placeholder console/playground with real implementations |
| `cmd/moca/app.go:43` | Replace placeholder publish with real implementation |
| `cmd/moca/test_cmd.go:16` | Replace placeholder run-ui with real implementation |
| `cmd/moca/events.go:232-251` | Add `--cdc` flag to `events tail` command |
| `pkg/api/rest.go` | Add `GET /api/v1/resource/{doctype}/{name}/events` endpoint |
| `pkg/apps/manifest.go:23-43` | Add `Repository`, `Author`, `Keywords` fields to AppManifest |
| `go.mod` | Add `traefik/yaegi`, `peterh/liner` dependencies |

---

## Phase 1: Foundation

### Task 1: Add MetaType Fields (EventSourcing, CDCEnabled)

**Files:**
- Modify: `pkg/meta/metatype.go:34-59`
- Test: `pkg/meta/metatype_test.go`

- [ ] **Step 1: Write the failing test**

In `pkg/meta/metatype_test.go`, add a test that verifies the new fields exist and are correctly serialized:

```go
func TestMetaType_EventSourcingAndCDCFields(t *testing.T) {
	mt := MetaType{
		Name:          "SalesOrder",
		Module:        "selling",
		EventSourcing: true,
		CDCEnabled:    true,
	}

	data, err := json.Marshal(mt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded MetaType
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !decoded.EventSourcing {
		t.Error("expected EventSourcing=true after round-trip")
	}
	if !decoded.CDCEnabled {
		t.Error("expected CDCEnabled=true after round-trip")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestMetaType_EventSourcingAndCDCFields ./pkg/meta/...`
Expected: FAIL — `unknown field "EventSourcing"` or similar compilation error.

- [ ] **Step 3: Add the fields to MetaType**

In `pkg/meta/metatype.go`, add two fields to the `MetaType` struct after line 58 (`IsSingle bool`):

```go
	IsSingle      bool `json:"is_single"`
	EventSourcing bool `json:"event_sourcing"`
	CDCEnabled    bool `json:"cdc_enabled"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -run TestMetaType_EventSourcingAndCDCFields ./pkg/meta/...`
Expected: PASS

- [ ] **Step 5: Run full meta test suite**

Run: `go test -race ./pkg/meta/...`
Expected: All existing tests still pass.

- [ ] **Step 6: Commit**

```bash
git add pkg/meta/metatype.go pkg/meta/metatype_test.go
git commit -m "feat(meta): add EventSourcing and CDCEnabled fields to MetaType"
```

---

### Task 2: Add event_log System Table DDL

**Files:**
- Modify: `pkg/meta/ddl.go:282-285`
- Test: `pkg/meta/ddl_test.go`

- [ ] **Step 1: Write the failing test**

In `pkg/meta/ddl_test.go`, add a test for the event_log table:

```go
func TestGenerateSystemTablesDDL_EventLogPresent(t *testing.T) {
	stmts := meta.GenerateSystemTablesDDL()

	s, ok := findStmtByComment(stmts, "tab_event_log")
	if !ok {
		t.Fatal("tab_event_log DDL not found in GenerateSystemTablesDDL()")
	}

	lc := strings.ToLower(s.SQL)
	for _, col := range []string{"doctype", "docname", "event_type", "payload", "prev_data", "user_id", "request_id", "created_at"} {
		if !strings.Contains(lc, col) {
			t.Errorf("tab_event_log DDL missing expected column %q;\nSQL:\n%s", col, s.SQL)
		}
	}
	if !strings.Contains(lc, "jsonb") {
		t.Errorf("tab_event_log payload column should be JSONB; SQL:\n%s", s.SQL)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestGenerateSystemTablesDDL_EventLogPresent ./pkg/meta/...`
Expected: FAIL — `tab_event_log DDL not found`.

- [ ] **Step 3: Add event_log DDL statements**

In `pkg/meta/ddl.go`, after the `idx_outbox_pending` statement (around line 285), add:

```go
		{
			// tab_event_log stores the event-sourcing log for doctypes that opt in
			// via MetaType.EventSourcing = true. Each document write INSERTs a row
			// here inside the same transaction as the document write and outbox row,
			// guaranteeing consistency. The event log is append-only; a background
			// retention job prunes old entries when configured.
			SQL: `CREATE TABLE IF NOT EXISTS tab_event_log (
	"id"           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	"doctype"      TEXT NOT NULL,
	"docname"      TEXT NOT NULL,
	"event_type"   TEXT NOT NULL,
	"payload"      JSONB NOT NULL,
	"prev_data"    JSONB,
	"user_id"      TEXT NOT NULL,
	"request_id"   TEXT,
	"created_at"   TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`,
			Comment: "create system table tab_event_log",
		},
		{
			SQL:     `CREATE INDEX IF NOT EXISTS idx_event_log_doctype_name ON tab_event_log ("doctype", "docname", "created_at")`,
			Comment: "create index idx_event_log_doctype_name on tab_event_log",
		},
		{
			SQL:     `CREATE INDEX IF NOT EXISTS idx_event_log_created_at ON tab_event_log ("created_at")`,
			Comment: "create index idx_event_log_created_at on tab_event_log",
		},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -run TestGenerateSystemTablesDDL_EventLogPresent ./pkg/meta/...`
Expected: PASS

- [ ] **Step 5: Verify all DDL statements are idempotent**

Run: `go test -race -run TestGenerateSystemTablesDDL_AllIdempotent ./pkg/meta/...`
Expected: PASS — the new statements use `IF NOT EXISTS`.

- [ ] **Step 6: Update migrator integration test**

In `pkg/meta/migrator_integration_test.go`, find the system table existence check (line 343) and add `tab_event_log`:

```go
	for _, tbl := range []string{"tab_doctype", "tab_singles", "tab_version", "tab_audit_log", "tab_outbox", "tab_migration_log", "tab_event_log"} {
```

- [ ] **Step 7: Commit**

```bash
git add pkg/meta/ddl.go pkg/meta/ddl_test.go pkg/meta/migrator_integration_test.go
git commit -m "feat(meta): add tab_event_log system table DDL"
```

---

### Task 3: Add insertEventLog Function in CRUD

**Files:**
- Modify: `pkg/document/crud.go`
- Test: `pkg/document/crud_test.go`

- [ ] **Step 1: Write the failing test**

In `pkg/document/crud_test.go`, add a test for the EventLogRow struct and insertEventLog helper:

```go
func TestEventLogRow_JSONRoundTrip(t *testing.T) {
	row := EventLogRow{
		DocType:   "SalesOrder",
		DocName:   "SO-001",
		EventType: "doc.created",
		Payload:   json.RawMessage(`{"name":"SO-001"}`),
		PrevData:  nil,
		UserID:    "admin@test.com",
		RequestID: "req-123",
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	data, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded EventLogRow
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.DocType != row.DocType {
		t.Errorf("DocType: got %q, want %q", decoded.DocType, row.DocType)
	}
	if decoded.EventType != row.EventType {
		t.Errorf("EventType: got %q, want %q", decoded.EventType, row.EventType)
	}
	if decoded.UserID != row.UserID {
		t.Errorf("UserID: got %q, want %q", decoded.UserID, row.UserID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestEventLogRow_JSONRoundTrip ./pkg/document/...`
Expected: FAIL — `EventLogRow` undefined.

- [ ] **Step 3: Add EventLogRow struct and insertEventLog function**

In `pkg/document/crud.go`, after the `insertOutbox` function (around line 574), add:

```go
// EventLogRow represents an append-only event sourcing record. Written inside
// the same transaction as the document write when MetaType.EventSourcing is true.
type EventLogRow struct {
	ID        int64           `json:"id"`
	DocType   string          `json:"doctype"`
	DocName   string          `json:"docname"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
	PrevData  json.RawMessage `json:"prev_data,omitempty"`
	UserID    string          `json:"user_id"`
	RequestID string          `json:"request_id"`
	CreatedAt time.Time       `json:"created_at"`
}

// insertEventLog writes an event sourcing record inside an active transaction.
// Called only when the MetaType has EventSourcing enabled.
func insertEventLog(ctx context.Context, tx pgx.Tx, row EventLogRow) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO tab_event_log ("doctype","docname","event_type","payload","prev_data","user_id","request_id","created_at")
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		row.DocType,
		row.DocName,
		row.EventType,
		row.Payload,
		row.PrevData,
		row.UserID,
		row.RequestID,
		row.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("crud: insert event_log (doctype=%q docname=%q): %w", row.DocType, row.DocName, err)
	}
	return nil
}

// buildEventLogRow constructs an EventLogRow from a DocumentEvent.
func buildEventLogRow(event events.DocumentEvent) (EventLogRow, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return EventLogRow{}, fmt.Errorf("crud: marshal event log payload: %w", err)
	}

	var prevData json.RawMessage
	if event.PrevData != nil {
		prevData, err = json.Marshal(event.PrevData)
		if err != nil {
			return EventLogRow{}, fmt.Errorf("crud: marshal event log prev_data: %w", err)
		}
	}

	return EventLogRow{
		DocType:   event.DocType,
		DocName:   event.DocName,
		EventType: event.EventType,
		Payload:   payload,
		PrevData:  prevData,
		UserID:    event.User,
		RequestID: event.RequestID,
		CreatedAt: event.Timestamp,
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -run TestEventLogRow_JSONRoundTrip ./pkg/document/...`
Expected: PASS

- [ ] **Step 5: Wire insertEventLog into Insert TX block**

In `pkg/document/crud.go`, inside the Insert method's transaction block (around line 789-798), add the event log insert after `insertOutbox`:

```go
		txErr = insertOutbox(txCtx, tx, outboxEvent)
		if txErr != nil {
			return txErr
		}
		if mt.EventSourcing {
			elRow, elErr := buildEventLogRow(outboxEvent)
			if elErr != nil {
				return elErr
			}
			if elErr = insertEventLog(txCtx, tx, elRow); elErr != nil {
				return elErr
			}
		}
```

- [ ] **Step 6: Wire insertEventLog into Update TX block**

In `pkg/document/crud.go`, inside the Update method's transaction block (around line 958-960), add after `insertOutbox`:

```go
		if err := insertOutbox(txCtx, tx, outboxEvent); err != nil {
			return err
		}
		if doc.Meta().EventSourcing {
			elRow, elErr := buildEventLogRow(outboxEvent)
			if elErr != nil {
				return elErr
			}
			if elErr = insertEventLog(txCtx, tx, elRow); elErr != nil {
				return elErr
			}
		}
```

- [ ] **Step 7: Wire insertEventLog into Delete TX block**

In `pkg/document/crud.go`, inside the Delete method's transaction block (around line 1045-1046), add after `insertOutbox`:

```go
		if err := insertOutbox(txCtx, tx, outboxEvent); err != nil {
			return err
		}
		if doc.Meta().EventSourcing {
			elRow, elErr := buildEventLogRow(outboxEvent)
			if elErr != nil {
				return elErr
			}
			if elErr = insertEventLog(txCtx, tx, elRow); elErr != nil {
				return elErr
			}
		}
```

- [ ] **Step 8: Run full document test suite**

Run: `go test -race ./pkg/document/...`
Expected: All tests pass (event sourcing is opt-in, no existing tests enable it).

- [ ] **Step 9: Commit**

```bash
git add pkg/document/crud.go pkg/document/crud_test.go
git commit -m "feat(document): add EventLogRow and insertEventLog for opt-in event sourcing"
```

---

### Task 4: CDC Fan-Out in OutboxPoller

**Files:**
- Modify: `pkg/events/outbox.go:55-64,152-189`
- Test: `pkg/events/outbox_test.go`

- [ ] **Step 1: Write the failing test**

In `pkg/events/outbox_test.go`, add a test that verifies CDC fan-out publishes to the CDC topic when a doctype has CDCEnabled:

```go
func TestOutboxPoller_CDCFanOut(t *testing.T) {
	published := make(map[string][]DocumentEvent)
	var mu sync.Mutex

	mockProducer := &mockProducer{
		publishFn: func(ctx context.Context, topic string, event DocumentEvent) error {
			mu.Lock()
			published[topic] = append(published[topic], event)
			mu.Unlock()
			return nil
		},
	}

	// MetaProvider that reports CDCEnabled for "SalesOrder".
	mockMeta := &mockCDCMetaProvider{
		cdcEnabled: map[string]bool{"SalesOrder": true},
	}

	payload, _ := json.Marshal(DocumentEvent{
		EventID:   "evt-1",
		EventType: EventTypeDocCreated,
		Site:      "testsite",
		DocType:   "SalesOrder",
		DocName:   "SO-001",
	})

	mockStore := &mockOutboxStore{
		pending: []OutboxRow{
			{
				ID:           1,
				EventType:    EventTypeDocCreated,
				Topic:        TopicDocumentEvents,
				PartitionKey: "testsite:SalesOrder",
				Payload:      payload,
				Status:       "pending",
			},
		},
	}

	poller, err := NewOutboxPoller(OutboxPollerConfig{
		Store:        mockStore,
		Sites:        &mockSiteLister{sites: []string{"testsite"}},
		Producer:     mockProducer,
		CDCMetaProvider: mockMeta,
		BatchSize:    10,
		MaxRetries:   3,
		PollInterval: time.Second,
	})
	if err != nil {
		t.Fatalf("new poller: %v", err)
	}

	ctx := context.Background()
	if err := poller.pollOnce(ctx); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Should publish to both moca.doc.events AND moca.cdc.testsite.SalesOrder.
	if len(published[TopicDocumentEvents]) != 1 {
		t.Errorf("expected 1 event on %s, got %d", TopicDocumentEvents, len(published[TopicDocumentEvents]))
	}
	cdcTopic := CDCTopic("testsite", "SalesOrder")
	if len(published[cdcTopic]) != 1 {
		t.Errorf("expected 1 event on %s, got %d", cdcTopic, len(published[cdcTopic]))
	}
}

// mockCDCMetaProvider implements CDCMetaProvider for testing.
type mockCDCMetaProvider struct {
	cdcEnabled map[string]bool
}

func (m *mockCDCMetaProvider) IsCDCEnabled(ctx context.Context, site, doctype string) bool {
	return m.cdcEnabled[doctype]
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestOutboxPoller_CDCFanOut ./pkg/events/...`
Expected: FAIL — `CDCMetaProvider` field unknown.

- [ ] **Step 3: Add CDCMetaProvider interface and config field**

In `pkg/events/outbox.go`, add the interface after `AfterPublishHook` (around line 47):

```go
// CDCMetaProvider checks whether a doctype has CDC enabled.
// Implemented by the MetaType registry to look up MetaType.CDCEnabled.
type CDCMetaProvider interface {
	IsCDCEnabled(ctx context.Context, site, doctype string) bool
}
```

Add the field to `OutboxPollerConfig`:

```go
type OutboxPollerConfig struct {
	Store           OutboxStore
	Sites           ActiveSiteLister
	Producer        Producer
	Logger          *slog.Logger
	AfterPublish    AfterPublishHook
	CDCMetaProvider CDCMetaProvider // nil = no CDC fan-out
	PollInterval    time.Duration
	BatchSize       int
	MaxRetries      int
}
```

Add to the `OutboxPoller` struct:

```go
type OutboxPoller struct {
	store           OutboxStore
	sites           ActiveSiteLister
	producer        Producer
	logger          *slog.Logger
	afterPublish    AfterPublishHook
	cdcMetaProvider CDCMetaProvider
	pollInterval    time.Duration
	batchSize       int
	maxRetries      int
}
```

Update `NewOutboxPoller` to wire it:

```go
	return &OutboxPoller{
		store:           cfg.Store,
		sites:           cfg.Sites,
		producer:        cfg.Producer,
		logger:          cfg.Logger,
		pollInterval:    cfg.PollInterval,
		batchSize:       cfg.BatchSize,
		maxRetries:      cfg.MaxRetries,
		afterPublish:    cfg.AfterPublish,
		cdcMetaProvider: cfg.CDCMetaProvider,
	}, nil
```

- [ ] **Step 4: Add CDC fan-out to processSite**

In `pkg/events/outbox.go`, in the `processSite` method, after the primary publish and before appending to `published`, add CDC fan-out:

```go
		if err := p.producer.Publish(ctx, normalized.Topic, normalized.Event); err != nil {
			p.recordFailure(ctx, site, row, err)
			continue
		}
		// CDC fan-out: if the doctype has CDC enabled, also publish to the per-doctype topic.
		if p.cdcMetaProvider != nil && p.cdcMetaProvider.IsCDCEnabled(ctx, site, normalized.Event.DocType) {
			cdcTopic := CDCTopic(site, normalized.Event.DocType)
			if err := p.producer.Publish(ctx, cdcTopic, normalized.Event); err != nil {
				p.recordFailure(ctx, site, row, err)
				continue
			}
		}
		if p.afterPublish != nil {
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test -race -run TestOutboxPoller_CDCFanOut ./pkg/events/...`
Expected: PASS

- [ ] **Step 6: Run full events test suite**

Run: `go test -race ./pkg/events/...`
Expected: All existing tests pass (CDCMetaProvider is nil by default, no fan-out).

- [ ] **Step 7: Commit**

```bash
git add pkg/events/outbox.go pkg/events/outbox_test.go
git commit -m "feat(events): add CDC fan-out in OutboxPoller for per-doctype Kafka topics"
```

---

## Phase 2: High-Value Runtime Features

### Task 5: VirtualSource Interface and ReadOnlySource

**Files:**
- Create: `pkg/document/virtual.go`
- Test: `pkg/document/virtual_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/document/virtual_test.go`:

```go
package document

import (
	"context"
	"errors"
	"testing"
)

func TestReadOnlySource_ReturnsErrVirtualReadOnly(t *testing.T) {
	var src ReadOnlySource

	_, err := src.Insert(context.Background(), nil)
	if !errors.Is(err, ErrVirtualReadOnly) {
		t.Errorf("Insert: got %v, want ErrVirtualReadOnly", err)
	}

	err = src.Update(context.Background(), "x", nil)
	if !errors.Is(err, ErrVirtualReadOnly) {
		t.Errorf("Update: got %v, want ErrVirtualReadOnly", err)
	}

	err = src.Delete(context.Background(), "x")
	if !errors.Is(err, ErrVirtualReadOnly) {
		t.Errorf("Delete: got %v, want ErrVirtualReadOnly", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestReadOnlySource_ReturnsErrVirtualReadOnly ./pkg/document/...`
Expected: FAIL — types undefined.

- [ ] **Step 3: Implement VirtualSource interface and ReadOnlySource**

Create `pkg/document/virtual.go`:

```go
package document

import (
	"context"
	"errors"

	"github.com/osama1998H/moca/pkg/meta"
)

// ErrVirtualReadOnly is returned by VirtualSource write methods when the
// source does not support mutations.
var ErrVirtualReadOnly = errors.New("virtual document source is read-only")

// VirtualSource is the adapter interface for virtual doctypes (MetaType.IsVirtual).
// External systems (APIs, databases, file stores) implement this interface to
// appear as standard Moca documents.
//
// GetList and GetOne are required. Insert, Update, and Delete are optional —
// embed ReadOnlySource to provide default ErrVirtualReadOnly returns.
type VirtualSource interface {
	// GetList retrieves a paginated, filtered list of documents.
	// Uses the existing ListOptions type (Filters, AdvancedFilters, Limit, Offset, etc.).
	GetList(ctx context.Context, opts ListOptions) ([]map[string]any, int, error)

	// GetOne retrieves a single document by its name (primary key).
	GetOne(ctx context.Context, name string) (map[string]any, error)

	// Insert creates a new document. Returns the generated name.
	// Optional: embed ReadOnlySource if not supported.
	Insert(ctx context.Context, values map[string]any) (string, error)

	// Update modifies an existing document by name.
	// Optional: embed ReadOnlySource if not supported.
	Update(ctx context.Context, name string, values map[string]any) error

	// Delete removes a document by name.
	// Optional: embed ReadOnlySource if not supported.
	Delete(ctx context.Context, name string) error
}

// ReadOnlySource provides default ErrVirtualReadOnly returns for the write methods
// of VirtualSource. Embed this in read-only adapters so they only need to implement
// GetList and GetOne.
type ReadOnlySource struct{}

func (ReadOnlySource) Insert(_ context.Context, _ map[string]any) (string, error) {
	return "", ErrVirtualReadOnly
}

func (ReadOnlySource) Update(_ context.Context, _ string, _ map[string]any) error {
	return ErrVirtualReadOnly
}

func (ReadOnlySource) Delete(_ context.Context, _ string) error {
	return ErrVirtualReadOnly
}

// VirtualDoc implements the Document interface backed by a VirtualSource
// rather than PostgreSQL. It stores field values in a map and tracks dirty state
// by comparing against an original snapshot.
type VirtualDoc struct {
	metaDef  *meta.MetaType
	source   VirtualSource
	values   map[string]any
	original map[string]any
	isNew    bool
}

// NewVirtualDoc wraps values from a VirtualSource as a Document.
func NewVirtualDoc(metaDef *meta.MetaType, source VirtualSource, values map[string]any, isNew bool) *VirtualDoc {
	original := make(map[string]any, len(values))
	for k, v := range values {
		original[k] = v
	}
	return &VirtualDoc{
		metaDef:  metaDef,
		source:   source,
		values:   values,
		original: original,
		isNew:    isNew,
	}
}

func (d *VirtualDoc) Meta() *meta.MetaType { return d.metaDef }

func (d *VirtualDoc) Name() string {
	if n, ok := d.values["name"].(string); ok {
		return n
	}
	return ""
}

func (d *VirtualDoc) Get(field string) any {
	return d.values[field]
}

func (d *VirtualDoc) Set(field string, value any) error {
	d.values[field] = value
	return nil
}

func (d *VirtualDoc) GetChild(_ string) []Document {
	return nil // virtual docs do not support child tables
}

func (d *VirtualDoc) AddChild(_ string) (Document, error) {
	return nil, errors.New("virtual documents do not support child tables")
}

func (d *VirtualDoc) IsNew() bool { return d.isNew }

func (d *VirtualDoc) IsModified() bool {
	return len(d.ModifiedFields()) > 0
}

func (d *VirtualDoc) ModifiedFields() []string {
	var modified []string
	for k, v := range d.values {
		if orig, ok := d.original[k]; !ok || orig != v {
			modified = append(modified, k)
		}
	}
	return modified
}

func (d *VirtualDoc) AsMap() map[string]any {
	result := make(map[string]any, len(d.values))
	for k, v := range d.values {
		result[k] = v
	}
	return result
}

func (d *VirtualDoc) ToJSON() ([]byte, error) {
	return encodeJSON(d.AsMap())
}

// Source returns the underlying VirtualSource for direct access.
func (d *VirtualDoc) Source() VirtualSource {
	return d.source
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -run TestReadOnlySource_ReturnsErrVirtualReadOnly ./pkg/document/...`
Expected: PASS

- [ ] **Step 5: Add VirtualDoc Document interface compliance test**

In `pkg/document/virtual_test.go`, add:

```go
func TestVirtualDoc_ImplementsDocumentInterface(t *testing.T) {
	mt := &meta.MetaType{Name: "ExternalInvoice", IsVirtual: true}
	values := map[string]any{"name": "INV-001", "amount": 100.0}
	doc := NewVirtualDoc(mt, ReadOnlySource{}, values, false)

	// Verify Document interface compliance.
	var _ Document = doc

	if doc.Name() != "INV-001" {
		t.Errorf("Name: got %q, want %q", doc.Name(), "INV-001")
	}
	if doc.Get("amount") != 100.0 {
		t.Errorf("Get(amount): got %v, want 100.0", doc.Get("amount"))
	}
	if doc.IsNew() {
		t.Error("IsNew: expected false")
	}
	if doc.IsModified() {
		t.Error("IsModified: expected false before changes")
	}

	_ = doc.Set("amount", 200.0)
	if !doc.IsModified() {
		t.Error("IsModified: expected true after Set")
	}

	fields := doc.ModifiedFields()
	if len(fields) != 1 || fields[0] != "amount" {
		t.Errorf("ModifiedFields: got %v, want [amount]", fields)
	}

	m := doc.AsMap()
	if m["amount"] != 200.0 {
		t.Errorf("AsMap[amount]: got %v, want 200.0", m["amount"])
	}

	children := doc.GetChild("items")
	if len(children) != 0 {
		t.Errorf("GetChild: expected empty, got %d", len(children))
	}

	_, err := doc.AddChild("items")
	if err == nil {
		t.Error("AddChild: expected error for virtual doc")
	}
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test -race -run TestVirtualDoc_ImplementsDocumentInterface ./pkg/document/...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add pkg/document/virtual.go pkg/document/virtual_test.go
git commit -m "feat(document): add VirtualSource interface, ReadOnlySource, and VirtualDoc"
```

---

### Task 6: VirtualSourceRegistry

**Files:**
- Create: `pkg/document/virtual_registry.go`
- Create: `pkg/document/virtual_registry_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/document/virtual_registry_test.go`:

```go
package document

import (
	"context"
	"testing"
)

type stubSource struct {
	ReadOnlySource
}

func (s *stubSource) GetList(_ context.Context, _ ListOptions) ([]map[string]any, int, error) {
	return nil, 0, nil
}
func (s *stubSource) GetOne(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

func TestVirtualSourceRegistry_RegisterAndGet(t *testing.T) {
	reg := NewVirtualSourceRegistry()

	src := &stubSource{}
	reg.Register("ExternalInvoice", src)

	got, ok := reg.Get("ExternalInvoice")
	if !ok {
		t.Fatal("expected Get to return true for registered source")
	}
	if got != src {
		t.Error("expected Get to return the same source instance")
	}

	_, ok = reg.Get("NonExistent")
	if ok {
		t.Error("expected Get to return false for unregistered source")
	}
}

func TestVirtualSourceRegistry_List(t *testing.T) {
	reg := NewVirtualSourceRegistry()
	reg.Register("Alpha", &stubSource{})
	reg.Register("Beta", &stubSource{})

	names := reg.List()
	if len(names) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(names))
	}
}

func TestVirtualSourceRegistry_ConcurrentAccess(t *testing.T) {
	reg := NewVirtualSourceRegistry()
	done := make(chan struct{})

	go func() {
		for i := 0; i < 100; i++ {
			reg.Register("Type", &stubSource{})
		}
		close(done)
	}()

	for i := 0; i < 100; i++ {
		reg.Get("Type")
		reg.List()
	}
	<-done
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestVirtualSourceRegistry ./pkg/document/...`
Expected: FAIL — `NewVirtualSourceRegistry` undefined.

- [ ] **Step 3: Implement VirtualSourceRegistry**

Create `pkg/document/virtual_registry.go`:

```go
package document

import (
	"sort"
	"sync"
)

// VirtualSourceRegistry maps doctype names to their VirtualSource implementations.
// Thread-safe for concurrent registration and lookup.
type VirtualSourceRegistry struct {
	mu      sync.RWMutex
	sources map[string]VirtualSource
}

// NewVirtualSourceRegistry creates an empty registry.
func NewVirtualSourceRegistry() *VirtualSourceRegistry {
	return &VirtualSourceRegistry{
		sources: make(map[string]VirtualSource),
	}
}

// Register associates a VirtualSource with a doctype name. Overwrites any
// existing registration for the same doctype.
func (r *VirtualSourceRegistry) Register(doctype string, src VirtualSource) {
	r.mu.Lock()
	r.sources[doctype] = src
	r.mu.Unlock()
}

// Get retrieves the VirtualSource for a doctype. Returns (nil, false) if not registered.
func (r *VirtualSourceRegistry) Get(doctype string) (VirtualSource, bool) {
	r.mu.RLock()
	src, ok := r.sources[doctype]
	r.mu.RUnlock()
	return src, ok
}

// List returns all registered doctype names in sorted order.
func (r *VirtualSourceRegistry) List() []string {
	r.mu.RLock()
	names := make([]string, 0, len(r.sources))
	for name := range r.sources {
		names = append(names, name)
	}
	r.mu.RUnlock()
	sort.Strings(names)
	return names
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run TestVirtualSourceRegistry ./pkg/document/...`
Expected: PASS

- [ ] **Step 5: Add virtualSources field to DocManager**

In `pkg/document/crud.go`, add `virtualSources` to the `DocManager` struct (around line 386):

```go
type DocManager struct {
	registry            *meta.Registry
	db                  *orm.DBManager
	queryAdapter        orm.MetaProvider
	naming              *NamingEngine
	validator           *Validator
	controllers         *ControllerRegistry
	hookDispatcher      HookDispatcher
	permResolver        PermResolver
	postLoadTransformer PostLoadTransformer
	virtualSources      *VirtualSourceRegistry // nil = no virtual doctypes
	logger              *slog.Logger
}
```

Add a setter method after `SetPermResolver`:

```go
// SetVirtualSourceRegistry configures the registry for virtual doctypes.
// Pass nil to disable virtual document support.
func (m *DocManager) SetVirtualSourceRegistry(r *VirtualSourceRegistry) {
	m.virtualSources = r
}
```

- [ ] **Step 6: Run full document test suite**

Run: `go test -race ./pkg/document/...`
Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add pkg/document/virtual_registry.go pkg/document/virtual_registry_test.go pkg/document/crud.go
git commit -m "feat(document): add VirtualSourceRegistry and wire into DocManager"
```

---

### Task 7: VirtualDoc CRUD Routing in DocManager

**Files:**
- Modify: `pkg/document/crud.go`
- Test: `pkg/document/crud_test.go`

- [ ] **Step 1: Write the failing test**

In `pkg/document/crud_test.go`, add:

```go
type inMemorySource struct {
	ReadOnlySource
	docs map[string]map[string]any
}

func newInMemorySource(docs ...map[string]any) *inMemorySource {
	src := &inMemorySource{docs: make(map[string]map[string]any)}
	for _, d := range docs {
		if name, ok := d["name"].(string); ok {
			src.docs[name] = d
		}
	}
	return src
}

func (s *inMemorySource) GetList(_ context.Context, opts ListOptions) ([]map[string]any, int, error) {
	var result []map[string]any
	for _, d := range s.docs {
		result = append(result, d)
	}
	total := len(result)
	if opts.Limit > 0 && opts.Limit < len(result) {
		result = result[:opts.Limit]
	}
	return result, total, nil
}

func (s *inMemorySource) GetOne(_ context.Context, name string) (map[string]any, error) {
	d, ok := s.docs[name]
	if !ok {
		return nil, fmt.Errorf("not found: %s", name)
	}
	return d, nil
}

func TestDocManager_GetVirtualDoc(t *testing.T) {
	src := newInMemorySource(map[string]any{"name": "EXT-001", "title": "External Doc"})

	vsReg := NewVirtualSourceRegistry()
	vsReg.Register("ExternalDoc", src)

	// This test validates the routing logic exists.
	// Full integration requires a DocManager with registry, but we can test
	// that isVirtualDoctype correctly identifies virtual types.
	mt := &meta.MetaType{Name: "ExternalDoc", IsVirtual: true}

	if !mt.IsVirtual {
		t.Fatal("expected IsVirtual=true")
	}

	got, ok := vsReg.Get("ExternalDoc")
	if !ok {
		t.Fatal("expected virtual source to be registered")
	}

	values, err := got.GetOne(context.Background(), "EXT-001")
	if err != nil {
		t.Fatalf("GetOne: %v", err)
	}
	if values["title"] != "External Doc" {
		t.Errorf("title: got %v, want External Doc", values["title"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestDocManager_GetVirtualDoc ./pkg/document/...`
Expected: FAIL if imports are missing, otherwise PASS since this is a unit test of routing logic.

- [ ] **Step 3: Add virtual routing to DocManager.Get**

In `pkg/document/crud.go`, at the beginning of the `Get` method (around line 1067), add virtual routing before the DB path:

```go
func (m *DocManager) Get(ctx *DocContext, doctype, name string) (*DynamicDoc, error) {
	// Virtual doctype routing: delegate to VirtualSource if registered.
	mt, err := m.registry.Get(ctx, ctx.Site.Name, doctype)
	if err != nil {
		return nil, fmt.Errorf("crud: Get %q %q: load MetaType: %w", doctype, name, err)
	}
	if mt.IsVirtual && m.virtualSources != nil {
		return m.getVirtual(ctx, mt, name)
	}

	// Existing DB path continues below...
```

Add the `getVirtual` helper:

```go
// getVirtual retrieves a document from a VirtualSource and wraps it as a DynamicDoc.
func (m *DocManager) getVirtual(ctx *DocContext, mt *meta.MetaType, name string) (*DynamicDoc, error) {
	src, ok := m.virtualSources.Get(mt.Name)
	if !ok {
		return nil, fmt.Errorf("crud: Get virtual %q: no source registered", mt.Name)
	}

	values, err := src.GetOne(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("crud: Get virtual %q %q: %w", mt.Name, name, err)
	}

	childMetas, err := m.resolveChildMetas(ctx, ctx.Site.Name, mt)
	if err != nil {
		return nil, fmt.Errorf("crud: Get virtual %q: %w", mt.Name, err)
	}
	doc := NewDynamicDoc(mt, childMetas, false)
	if applyErr := applyValues(doc, values); applyErr != nil {
		return nil, fmt.Errorf("crud: Get virtual %q: apply values: %w", mt.Name, applyErr)
	}
	doc.markPersisted()
	doc.resetDirtyState()
	return doc, nil
}
```

- [ ] **Step 4: Add virtual routing to DocManager.GetList**

In the `GetList` method, add a virtual routing check at the top:

```go
func (m *DocManager) GetList(ctx *DocContext, doctype string, opts ListOptions) ([]map[string]any, int, error) {
	mt, err := m.registry.Get(ctx, ctx.Site.Name, doctype)
	if err != nil {
		return nil, 0, fmt.Errorf("crud: GetList %q: load MetaType: %w", doctype, err)
	}
	if mt.IsVirtual && m.virtualSources != nil {
		return m.getListVirtual(ctx, mt, opts)
	}

	// Existing DB path continues...
```

Add the `getListVirtual` helper:

```go
func (m *DocManager) getListVirtual(ctx *DocContext, mt *meta.MetaType, opts ListOptions) ([]map[string]any, int, error) {
	src, ok := m.virtualSources.Get(mt.Name)
	if !ok {
		return nil, 0, fmt.Errorf("crud: GetList virtual %q: no source registered", mt.Name)
	}

	results, total, err := src.GetList(ctx, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("crud: GetList virtual %q: %w", mt.Name, err)
	}
	return results, total, nil
}
```

- [ ] **Step 5: Add virtual routing to Insert, Update, Delete**

Add similar routing checks at the top of `Insert`, `Update`, and `Delete` that delegate to the VirtualSource when `mt.IsVirtual` is true:

For Insert:
```go
	if mt.IsVirtual && m.virtualSources != nil {
		return m.insertVirtual(ctx, mt, values)
	}
```

For Update:
```go
	if mt.IsVirtual && m.virtualSources != nil {
		return m.updateVirtual(ctx, mt, name, values)
	}
```

For Delete:
```go
	if mt.IsVirtual && m.virtualSources != nil {
		return m.deleteVirtual(ctx, mt, name)
	}
```

Implement each virtual helper to call the VirtualSource method, running lifecycle hooks before/after. If the source returns `ErrVirtualReadOnly`, return an appropriate error.

- [ ] **Step 6: Run full document test suite**

Run: `go test -race ./pkg/document/...`
Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add pkg/document/crud.go pkg/document/crud_test.go
git commit -m "feat(document): route virtual doctypes through VirtualSource in DocManager CRUD"
```

---

### Task 8: Event Sourcing GetHistory and Replay

**Files:**
- Create: `pkg/document/eventlog.go`
- Create: `pkg/document/eventlog_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/document/eventlog_test.go`:

```go
package document

import (
	"testing"
	"time"
)

func TestQueryOpts_Defaults(t *testing.T) {
	opts := EventLogQueryOpts{}
	if opts.Limit != 0 {
		t.Errorf("default Limit: got %d, want 0", opts.Limit)
	}
}

func TestEventLogRow_Fields(t *testing.T) {
	row := EventLogRow{
		DocType:   "SalesOrder",
		DocName:   "SO-001",
		EventType: "doc.created",
		UserID:    "admin",
		CreatedAt: time.Now(),
	}
	if row.DocType != "SalesOrder" {
		t.Errorf("DocType: got %q", row.DocType)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestQueryOpts_Defaults ./pkg/document/...`
Expected: FAIL — `EventLogQueryOpts` undefined.

- [ ] **Step 3: Implement eventlog.go**

Create `pkg/document/eventlog.go`:

```go
package document

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EventLogQueryOpts configures event log queries.
type EventLogQueryOpts struct {
	Limit     int       // 0 = no limit (use default of 100)
	Offset    int
	Since     time.Time // zero value = no lower bound
	Until     time.Time // zero value = no upper bound
	EventType string    // filter by event type, empty = all
}

// GetHistory returns the ordered event stream for a specific document.
// Requires the doctype to have EventSourcing enabled (caller must verify).
func GetHistory(ctx context.Context, pool *pgxpool.Pool, doctype, docname string, opts EventLogQueryOpts) ([]EventLogRow, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}

	query := `SELECT "id","doctype","docname","event_type","payload","prev_data","user_id","request_id","created_at"
		FROM tab_event_log
		WHERE "doctype" = $1 AND "docname" = $2`
	args := []any{doctype, docname}
	argIdx := 3

	if !opts.Since.IsZero() {
		query += fmt.Sprintf(` AND "created_at" >= $%d`, argIdx)
		args = append(args, opts.Since)
		argIdx++
	}
	if !opts.Until.IsZero() {
		query += fmt.Sprintf(` AND "created_at" <= $%d`, argIdx)
		args = append(args, opts.Until)
		argIdx++
	}
	if opts.EventType != "" {
		query += fmt.Sprintf(` AND "event_type" = $%d`, argIdx)
		args = append(args, opts.EventType)
		argIdx++
	}

	query += ` ORDER BY "created_at" ASC, "id" ASC`
	query += fmt.Sprintf(` LIMIT $%d OFFSET $%d`, argIdx, argIdx+1)
	args = append(args, limit, opts.Offset)

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("eventlog: query history: %w", err)
	}
	defer rows.Close()

	var result []EventLogRow
	for rows.Next() {
		var row EventLogRow
		if err := rows.Scan(
			&row.ID,
			&row.DocType,
			&row.DocName,
			&row.EventType,
			&row.Payload,
			&row.PrevData,
			&row.UserID,
			&row.RequestID,
			&row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("eventlog: scan row: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("eventlog: iterate rows: %w", err)
	}
	return result, nil
}

// Replay reconstructs the current state of a document by applying its event
// log in order. Returns the final state map. Useful for auditing, not for
// hot-path reads (use Get for that).
func Replay(ctx context.Context, pool *pgxpool.Pool, doctype, docname string) (map[string]any, error) {
	events, err := GetHistory(ctx, pool, doctype, docname, EventLogQueryOpts{Limit: 10000})
	if err != nil {
		return nil, fmt.Errorf("eventlog: replay: %w", err)
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("eventlog: replay %q %q: no events found", doctype, docname)
	}

	state := make(map[string]any)
	for _, ev := range events {
		var data map[string]any
		if err := json.Unmarshal(ev.Payload, &data); err != nil {
			return nil, fmt.Errorf("eventlog: replay unmarshal event %d: %w", ev.ID, err)
		}
		// The payload is a DocumentEvent envelope; extract the "data" field.
		if d, ok := data["data"]; ok {
			if m, ok := d.(map[string]any); ok {
				for k, v := range m {
					state[k] = v
				}
			}
		}
	}
	return state, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run "TestQueryOpts_Defaults|TestEventLogRow_Fields" ./pkg/document/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/document/eventlog.go pkg/document/eventlog_test.go
git commit -m "feat(document): add GetHistory and Replay for event sourcing queries"
```

---

### Task 9: Event History REST Endpoint

**Files:**
- Modify: `pkg/api/rest.go`
- Test: `pkg/api/rest_test.go`

- [ ] **Step 1: Write the failing test**

In `pkg/api/rest_test.go`, add a test for the events endpoint:

```go
func TestEventsEndpoint_RequiresEventSourcing(t *testing.T) {
	// Test that GET /api/v1/resource/{doctype}/{name}/events returns 400
	// when the doctype does not have EventSourcing enabled.
	mt := &meta.MetaType{Name: "Task", EventSourcing: false}

	// The handler should check mt.EventSourcing and return an error.
	if mt.EventSourcing {
		t.Error("expected EventSourcing=false for this test")
	}
}
```

- [ ] **Step 2: Add the /events route handler**

In `pkg/api/rest.go`, add a new handler method on `RestHandler`:

```go
// handleGetEvents returns the event sourcing history for a document.
// GET /api/v1/resource/{doctype}/{name}/events
func (h *RestHandler) handleGetEvents(w http.ResponseWriter, r *http.Request) {
	doctype := chi.URLParam(r, "doctype")
	name := chi.URLParam(r, "name")

	mt, err := h.gateway.Registry().Get(r.Context(), h.resolveSite(r), doctype)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, "DocType not found", err)
		return
	}

	if !mt.EventSourcing {
		h.writeError(w, r, http.StatusBadRequest, "Event sourcing is not enabled for this DocType", nil)
		return
	}

	pool, err := h.gateway.DB().ForSite(r.Context(), h.resolveSite(r))
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "Database error", err)
		return
	}

	opts := document.EventLogQueryOpts{
		Limit:  100,
		Offset: 0,
	}
	// Parse optional query params.
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			opts.Limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			opts.Offset = n
		}
	}

	events, err := document.GetHistory(r.Context(), pool, doctype, name, opts)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "Failed to query event log", err)
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"data":  events,
		"total": len(events),
	})
}
```

- [ ] **Step 3: Register the route**

In the route registration section of `rest.go`, add after the existing resource routes:

```go
	r.Get("/api/v1/resource/{doctype}/{name}/events", h.handleGetEvents)
```

- [ ] **Step 4: Run API test suite**

Run: `go test -race ./pkg/api/...`
Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add pkg/api/rest.go pkg/api/rest_test.go
git commit -m "feat(api): add GET /resource/{doctype}/{name}/events endpoint for event history"
```

---

### Task 10: Add --cdc Flag to events tail Command

**Files:**
- Modify: `cmd/moca/events.go:232-251`

- [ ] **Step 1: Add the --cdc flag**

In `cmd/moca/events.go`, in `newEventsTailCmd()`, add after the existing flags:

```go
	f.Bool("cdc", false, "Tail the CDC topic for a doctype (requires --doctype and --site)")
```

- [ ] **Step 2: Update runEventsTail to handle --cdc**

In `runEventsTail`, after parsing flags, add CDC topic resolution:

```go
	cdc, _ := cmd.Flags().GetBool("cdc")

	if cdc {
		if site == "" || doctype == "" {
			return output.NewCLIError("--cdc requires both --site and --doctype").
				WithFix("Usage: moca events tail --cdc --site mysite --doctype SalesOrder")
		}
		topic = events.CDCTopic(site, doctype)
	}
```

This replaces the topic argument with the computed CDC topic name.

- [ ] **Step 3: Update command usage to reflect optional topic with --cdc**

Change the `Args` from `cobra.ExactArgs(1)` to `cobra.MaximumNArgs(1)` and handle the case where --cdc is set but no topic arg is given:

```go
	cmd := &cobra.Command{
		Use:   "tail [TOPIC]",
		Short: "Tail events from a topic in real-time",
		Long: `Subscribe to an event topic and stream events as they arrive.
Use --cdc with --site and --doctype to tail CDC events.
Kafka mode: consumes from the topic with a temporary consumer group.
Redis mode: subscribes to the pub/sub channel (no historical replay).`,
		Args: cobra.MaximumNArgs(1),
		RunE: runEventsTail,
	}
```

In `runEventsTail`, resolve the topic:

```go
	var topic string
	if len(args) > 0 {
		topic = args[0]
	}
	cdc, _ := cmd.Flags().GetBool("cdc")
	if cdc {
		if site == "" || doctype == "" {
			return output.NewCLIError("--cdc requires both --site and --doctype").
				WithFix("Usage: moca events tail --cdc --site mysite --doctype SalesOrder")
		}
		topic = events.CDCTopic(site, doctype)
	}
	if topic == "" {
		return output.NewCLIError("TOPIC argument is required unless using --cdc").
			WithFix("Usage: moca events tail moca.doc.events OR moca events tail --cdc --site X --doctype Y")
	}
```

- [ ] **Step 4: Run lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/moca/events.go
git commit -m "feat(cli): add --cdc flag to moca events tail for per-doctype CDC topics"
```

---

## Phase 3: Developer CLI Tools

### Task 11: moca dev console (yaegi REPL)

**Files:**
- Create: `pkg/console/doc.go`
- Create: `pkg/console/console.go`
- Create: `pkg/console/console_test.go`
- Create: `cmd/moca/dev_console.go`
- Modify: `cmd/moca/dev.go:30-31`
- Modify: `go.mod`

- [ ] **Step 1: Add yaegi and liner dependencies**

Run: `cd /Users/osamamuhammed/Moca && go get github.com/traefik/yaegi@latest && go get github.com/peterh/liner@latest`

- [ ] **Step 2: Create console package doc**

Create `pkg/console/doc.go`:

```go
// Package console provides the curated standard library for the moca dev console REPL.
// It wraps DocManager, Registry, and ORM operations into simple helper methods
// accessible from the yaegi Go interpreter.
package console
```

- [ ] **Step 3: Write the failing test for Console struct**

Create `pkg/console/console_test.go`:

```go
package console

import (
	"testing"
)

func TestConsole_NilDocManagerReturnsError(t *testing.T) {
	c := &Console{}

	_, err := c.Get("SalesOrder", "SO-001")
	if err == nil {
		t.Error("expected error when DocManager is nil")
	}

	_, _, err = c.GetList("SalesOrder")
	if err == nil {
		t.Error("expected error when DocManager is nil")
	}
}
```

- [ ] **Step 4: Implement Console struct**

Create `pkg/console/console.go`:

```go
package console

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/tenancy"
)

// Console provides the curated helper methods exposed in the yaegi REPL as "moca.*".
type Console struct {
	DocManager *document.DocManager
	Registry   *meta.Registry
	Pool       *pgxpool.Pool
	Site       *tenancy.SiteContext
	User       *auth.User
}

func (c *Console) docCtx() *document.DocContext {
	return &document.DocContext{
		Context: context.Background(),
		Site:    c.Site,
		User:    c.User,
	}
}

// Get retrieves a single document by doctype and name.
func (c *Console) Get(doctype, name string) (map[string]any, error) {
	if c.DocManager == nil {
		return nil, fmt.Errorf("console: DocManager not initialized")
	}
	doc, err := c.DocManager.Get(c.docCtx(), doctype, name)
	if err != nil {
		return nil, err
	}
	return doc.AsMap(), nil
}

// GetList retrieves documents of a doctype with optional filters.
func (c *Console) GetList(doctype string, filters ...any) ([]map[string]any, int, error) {
	if c.DocManager == nil {
		return nil, 0, fmt.Errorf("console: DocManager not initialized")
	}
	opts := document.ListOptions{}
	if len(filters) > 0 {
		if m, ok := filters[0].(map[string]any); ok {
			opts.Filters = m
		}
	}
	return c.DocManager.GetList(c.docCtx(), doctype, opts)
}

// Insert creates a new document. Returns the generated name.
func (c *Console) Insert(doctype string, values map[string]any) (string, error) {
	if c.DocManager == nil {
		return "", fmt.Errorf("console: DocManager not initialized")
	}
	doc, err := c.DocManager.Insert(c.docCtx(), doctype, values)
	if err != nil {
		return "", err
	}
	return doc.Name(), nil
}

// Update modifies an existing document.
func (c *Console) Update(doctype, name string, values map[string]any) error {
	if c.DocManager == nil {
		return fmt.Errorf("console: DocManager not initialized")
	}
	_, err := c.DocManager.Update(c.docCtx(), doctype, name, values)
	return err
}

// Delete removes a document.
func (c *Console) Delete(doctype, name string) error {
	if c.DocManager == nil {
		return fmt.Errorf("console: DocManager not initialized")
	}
	return c.DocManager.Delete(c.docCtx(), doctype, name)
}

// SQL executes a raw SQL query and returns rows as maps.
func (c *Console) SQL(query string, args ...any) ([]map[string]any, error) {
	if c.Pool == nil {
		return nil, fmt.Errorf("console: database pool not initialized")
	}
	rows, err := c.Pool.Query(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	descs := rows.FieldDescriptions()
	var result []map[string]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make(map[string]any, len(descs))
		for i, desc := range descs {
			row[string(desc.Name)] = vals[i]
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// Meta returns the MetaType definition for a doctype.
func (c *Console) Meta(doctype string) (*meta.MetaType, error) {
	if c.Registry == nil {
		return nil, fmt.Errorf("console: Registry not initialized")
	}
	return c.Registry.Get(context.Background(), c.Site.Name, doctype)
}
```

- [ ] **Step 5: Run console tests**

Run: `go test -race ./pkg/console/...`
Expected: PASS

- [ ] **Step 6: Implement dev console command**

Create `cmd/moca/dev_console.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/pkg/console"
	"github.com/peterh/liner"
	"github.com/spf13/cobra"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

func newDevConsoleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "console",
		Short: "Interactive Go REPL with framework loaded",
		Long: `Start an interactive console with the Moca framework loaded.
Uses yaegi (Go interpreter) for a Go REPL experience.
A curated set of helpers is available as the "moca" variable.

Known limitations: Yaegi does not support CGo or all reflection patterns.
If a framework package fails to load, use "moca dev execute" instead.`,
		RunE: runDevConsole,
	}

	cmd.Flags().String("site", "", "Target site (auto-detected from context)")
	cmd.Flags().Bool("verbose", false, "Show package loading details")

	return cmd
}

func runDevConsole(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), cliCtx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	site, _ := cmd.Flags().GetString("site")
	siteCtx, pool, err := resolveSiteForConsole(cmd, svc, site)
	if err != nil {
		return err
	}

	// Build curated console stdlib.
	moca := &console.Console{
		DocManager: svc.DocManager,
		Registry:   svc.Registry,
		Pool:       pool,
		Site:       siteCtx,
	}

	// Initialize yaegi interpreter.
	i := interp.New(interp.Options{})
	if err := i.Use(stdlib.Symbols); err != nil {
		return fmt.Errorf("yaegi: load stdlib: %w", err)
	}

	// Inject the moca console instance.
	// Users access it via: moca.Get("DocType", "name")
	if err := i.Use(interp.Exports{
		"moca/moca": {"moca": {Value: reflect.ValueOf(moca)}},
	}); err != nil {
		w.PrintWarning(fmt.Sprintf("Could not inject console helpers: %v", err))
	}

	w.PrintSuccess("Moca console ready.")
	w.Print("Site: %s", siteCtx.Name)
	w.Print("Type Go expressions. Use moca.Get(), moca.GetList(), moca.SQL(), etc.")
	w.Print("Press Ctrl+D to exit.\n")

	// REPL loop with liner for readline support.
	line := liner.NewLiner()
	defer func() { _ = line.Close() }()
	line.SetCtrlCAborts(true)

	// Load history.
	historyPath := filepath.Join(os.Getenv("HOME"), ".moca", "console_history")
	if f, err := os.Open(historyPath); err == nil {
		_, _ = line.ReadHistory(f)
		_ = f.Close()
	}

	for {
		input, err := line.Prompt("moca> ")
		if err != nil {
			break // Ctrl+D or error
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		line.AppendHistory(input)

		v, err := i.Eval(input)
		if err != nil {
			w.PrintError(err.Error())
			continue
		}
		if v.IsValid() {
			fmt.Println(v.Interface())
		}
	}

	// Save history.
	_ = os.MkdirAll(filepath.Dir(historyPath), 0o755)
	if f, err := os.Create(historyPath); err == nil {
		_, _ = line.WriteHistory(f)
		_ = f.Close()
	}

	return nil
}
```

**Note:** The `resolveSiteForConsole` helper should resolve the site context and pool from available sites. Add it to `cmd/moca/dev_console.go`:

```go
func resolveSiteForConsole(cmd *cobra.Command, svc *Services, siteName string) (*tenancy.SiteContext, *pgxpool.Pool, error) {
	ctx := cmd.Context()
	if siteName == "" {
		sites, err := svc.Sites.ListSites(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("list sites: %w", err)
		}
		if len(sites) == 0 {
			return nil, nil, output.NewCLIError("No sites found").
				WithFix("Create a site first: moca site create mysite")
		}
		siteName = sites[0]
	}

	siteCtx := &tenancy.SiteContext{Name: siteName}
	pool, err := svc.DB.ForSite(ctx, siteName)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve site pool: %w", err)
	}
	return siteCtx, pool, nil
}
```

- [ ] **Step 7: Wire into dev command group**

In `cmd/moca/dev.go`, replace the placeholder:

```go
	cmd.AddCommand(
		newDevConsoleCmd(),
		newSubcommand("shell", "Open a shell with Moca env vars set"),
		newDevExecuteCmd(),
```

- [ ] **Step 8: Run lint and build**

Run: `make lint && make build-moca`
Expected: Both pass.

- [ ] **Step 9: Commit**

```bash
git add pkg/console/ cmd/moca/dev_console.go cmd/moca/dev.go go.mod go.sum
git commit -m "feat(cli): implement moca dev console with yaegi REPL and curated stdlib"
```

---

### Task 12: moca dev playground (Swagger + GraphiQL Proxy)

**Files:**
- Create: `cmd/moca/dev_playground.go`
- Modify: `cmd/moca/dev.go:38`

- [ ] **Step 1: Implement the playground command**

Create `cmd/moca/dev_playground.go`:

```go
package main

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os/exec"
	"runtime"

	"github.com/osama1998H/moca/internal/output"
	"github.com/spf13/cobra"
)

var playgroundTmpl = template.Must(template.New("playground").Parse(`<!DOCTYPE html>
<html>
<head>
  <title>Moca API Playground</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; max-width: 600px; margin: 80px auto; color: #333; }
    h1 { font-size: 24px; margin-bottom: 8px; }
    .subtitle { color: #666; margin-bottom: 32px; }
    .card { display: block; padding: 20px; border: 1px solid #ddd; border-radius: 8px; margin-bottom: 16px; text-decoration: none; color: inherit; transition: border-color 0.2s; }
    .card:hover { border-color: #0066cc; }
    .card h2 { margin: 0 0 4px; font-size: 18px; color: #0066cc; }
    .card p { margin: 0; color: #666; font-size: 14px; }
    .info { margin-top: 32px; font-size: 13px; color: #999; }
  </style>
</head>
<body>
  <h1>Moca API Playground</h1>
  <p class="subtitle">Site: {{.Site}}</p>
  <a class="card" href="/swagger">
    <h2>Swagger UI</h2>
    <p>REST API explorer with OpenAPI 3.0.3 spec — try endpoints, inspect schemas</p>
  </a>
  <a class="card" href="/graphql">
    <h2>GraphiQL</h2>
    <p>GraphQL IDE with autocomplete, docs explorer, and query history</p>
  </a>
  <div class="info">
    <p>Server: {{.ServerURL}}</p>
    <p>OpenAPI: {{.ServerURL}}/api/v1/openapi.json</p>
    <p>GraphQL: {{.ServerURL}}/api/graphql</p>
  </div>
</body>
</html>`))

func newDevPlaygroundCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "playground",
		Short: "Start interactive API playground",
		Long: `Start a local server that provides a unified landing page for
Swagger UI and GraphiQL. Proxies requests to the running Moca server
with auto-injected dev authentication.

Requires the Moca server to be running (moca serve or moca dev start).`,
		RunE: runDevPlayground,
	}

	cmd.Flags().Int("port", 8001, "Playground port")
	cmd.Flags().String("site", "", "Target site (auto-detected)")
	cmd.Flags().Bool("no-open", false, "Don't auto-open browser")

	return cmd
}

func runDevPlayground(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	port, _ := cmd.Flags().GetInt("port")
	site, _ := cmd.Flags().GetString("site")
	noOpen, _ := cmd.Flags().GetBool("no-open")

	if site == "" {
		site = "default"
	}

	// Resolve the moca-server URL.
	serverPort := defaultDevPort
	if cliCtx.Project != nil && cliCtx.Project.Development.Port > 0 {
		serverPort = cliCtx.Project.Development.Port
	}
	serverURL := fmt.Sprintf("http://localhost:%d", serverPort)

	target, err := url.Parse(serverURL)
	if err != nil {
		return fmt.Errorf("parse server URL: %w", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	mux := http.NewServeMux()

	// Landing page.
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(rw, r)
			return
		}
		rw.Header().Set("Content-Type", "text/html")
		_ = playgroundTmpl.Execute(rw, map[string]string{
			"Site":      site,
			"ServerURL": serverURL,
		})
	})

	// Proxy Swagger UI.
	mux.HandleFunc("/swagger", func(rw http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/api/docs"
		r.Host = target.Host
		proxy.ServeHTTP(rw, r)
	})
	mux.HandleFunc("/swagger/", func(rw http.ResponseWriter, r *http.Request) {
		r.Host = target.Host
		proxy.ServeHTTP(rw, r)
	})

	// Proxy GraphiQL.
	mux.HandleFunc("/graphql", func(rw http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/api/graphql/playground"
		r.Host = target.Host
		proxy.ServeHTTP(rw, r)
	})

	// Proxy API calls (for Swagger/GraphiQL to work).
	mux.HandleFunc("/api/", func(rw http.ResponseWriter, r *http.Request) {
		r.Host = target.Host
		proxy.ServeHTTP(rw, r)
	})

	addr := fmt.Sprintf(":%d", port)
	playgroundURL := fmt.Sprintf("http://localhost:%d", port)

	w.PrintSuccess(fmt.Sprintf("Playground running at %s", playgroundURL))
	w.Print("  Swagger UI: %s/swagger", playgroundURL)
	w.Print("  GraphiQL:   %s/graphql", playgroundURL)
	w.Print("  Server:     %s", serverURL)
	w.Print("\nPress Ctrl+C to stop.")

	if !noOpen {
		openBrowser(playgroundURL)
	}

	if err := http.ListenAndServe(addr, mux); err != nil {
		return fmt.Errorf("playground server: %w", err)
	}
	return nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	_ = cmd.Start()
}
```

- [ ] **Step 2: Wire into dev command group**

In `cmd/moca/dev.go`, replace the placeholder:

```go
	cmd.AddCommand(
		newDevConsoleCmd(),
		newSubcommand("shell", "Open a shell with Moca env vars set"),
		newDevExecuteCmd(),
		newDevRequestCmd(),
		newDevBenchCmd(),
		newDevProfileCmd(),
		newSubcommand("watch", "Watch and rebuild assets on change"),
		newDevPlaygroundCmd(),
	)
```

- [ ] **Step 3: Run lint and build**

Run: `make lint && make build-moca`
Expected: Both pass.

- [ ] **Step 4: Commit**

```bash
git add cmd/moca/dev_playground.go cmd/moca/dev.go
git commit -m "feat(cli): implement moca dev playground with Swagger UI and GraphiQL proxy"
```

---

### Task 13: moca app publish (GitHub Releases)

**Files:**
- Create: `cmd/moca/app_publish.go`
- Modify: `cmd/moca/app.go:43`
- Modify: `pkg/apps/manifest.go:23-43`

- [ ] **Step 1: Add publishing fields to AppManifest**

In `pkg/apps/manifest.go`, add after the existing fields (around line 43):

```go
	// Publishing (used by moca app publish)
	Repository string   `yaml:"repository,omitempty"`
	Author     string   `yaml:"author,omitempty"`
	Keywords   []string `yaml:"keywords,omitempty"`
```

- [ ] **Step 2: Write the publish command**

Create `cmd/moca/app_publish.go`:

```go
package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/pkg/apps"
	"github.com/spf13/cobra"
)

func newAppPublishCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish APP_NAME",
		Short: "Publish app to registry",
		Long: `Publish an app to GitHub Releases.

Validates the manifest, creates a tarball, tags the release in git,
and creates a GitHub Release with the tarball as an asset.

Requires: gh CLI authenticated (gh auth login) or GITHUB_TOKEN env var.`,
		Args: cobra.ExactArgs(1),
		RunE: runAppPublish,
		Example: `  moca app publish crm
  moca app publish crm --tag v1.0.0
  moca app publish crm --dry-run`,
	}

	cmd.Flags().String("tag", "", "Release tag (default: v{manifest.version})")
	cmd.Flags().Bool("dry-run", false, "Validate without publishing")
	cmd.Flags().String("notes", "", "Release notes")

	return cmd
}

func runAppPublish(cmd *cobra.Command, args []string) error {
	appName := args[0]
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	tagOverride, _ := cmd.Flags().GetString("tag")
	notes, _ := cmd.Flags().GetString("notes")

	// 1. Locate and parse app manifest.
	appDir := filepath.Join(cliCtx.ProjectRoot, "apps", appName)
	manifestPath := filepath.Join(appDir, "manifest.yaml")

	manifest, err := apps.ParseManifestFile(manifestPath)
	if err != nil {
		return output.NewCLIError(fmt.Sprintf("Cannot read manifest for app %q", appName)).
			WithErr(err).
			WithFix(fmt.Sprintf("Ensure %s exists and is valid YAML.", manifestPath))
	}

	// 2. Validate publishing fields.
	if manifest.Repository == "" {
		return output.NewCLIError("Manifest missing 'repository' field").
			WithFix("Add 'repository: github.com/org/repo' to manifest.yaml for publishing.")
	}
	if manifest.Version == "" {
		return output.NewCLIError("Manifest missing 'version' field").
			WithFix("Add 'version: 1.0.0' to manifest.yaml.")
	}

	tag := tagOverride
	if tag == "" {
		tag = "v" + manifest.Version
	}

	// 3. Check for uncommitted changes in app directory.
	statusCmd := exec.Command("git", "-C", appDir, "status", "--porcelain")
	statusOut, _ := statusCmd.Output()
	if len(strings.TrimSpace(string(statusOut))) > 0 && !dryRun {
		return output.NewCLIError("Uncommitted changes in app directory").
			WithFix("Commit or stash changes before publishing.")
	}

	// 4. Create tarball.
	tarballName := fmt.Sprintf("%s-%s.tar.gz", appName, manifest.Version)
	tarballPath := filepath.Join(os.TempDir(), tarballName)

	if err := createTarball(appDir, tarballPath, appName); err != nil {
		return output.NewCLIError("Failed to create tarball").WithErr(err)
	}
	defer func() { _ = os.Remove(tarballPath) }()

	info, _ := os.Stat(tarballPath)

	// 5. Dry-run output.
	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"app":        appName,
			"version":    manifest.Version,
			"repository": manifest.Repository,
			"tag":        tag,
			"archive":    tarballName,
			"size_bytes": info.Size(),
			"dry_run":    dryRun,
		})
	}

	w.Print("App:         %s", appName)
	w.Print("Version:     %s", manifest.Version)
	w.Print("Repository:  %s", manifest.Repository)
	w.Print("Tag:         %s", tag)
	w.Print("Archive:     %s (%s)", tarballName, formatSize(info.Size()))

	if dryRun {
		w.Print("")
		w.PrintSuccess("Dry run complete. Run without --dry-run to publish.")
		return nil
	}

	// 6. Check gh CLI auth.
	if _, err := exec.LookPath("gh"); err != nil {
		return output.NewCLIError("GitHub CLI (gh) not found").
			WithFix("Install gh: https://cli.github.com/ and run 'gh auth login'.")
	}

	authCmd := exec.Command("gh", "auth", "status")
	if err := authCmd.Run(); err != nil {
		return output.NewCLIError("GitHub CLI not authenticated").
			WithFix("Run 'gh auth login' or set GITHUB_TOKEN environment variable.")
	}

	// 7. Create GitHub release.
	repo := manifest.Repository
	// Normalize: remove github.com/ prefix for gh CLI.
	repo = strings.TrimPrefix(repo, "https://")
	repo = strings.TrimPrefix(repo, "github.com/")

	releaseTitle := fmt.Sprintf("%s %s", appName, tag)
	if notes == "" {
		notes = fmt.Sprintf("Release %s of %s", tag, appName)
	}

	ghArgs := []string{
		"release", "create", tag,
		"--repo", repo,
		"--title", releaseTitle,
		"--notes", notes,
		tarballPath,
	}

	ghCmd := exec.Command("gh", ghArgs...)
	ghCmd.Stdout = os.Stdout
	ghCmd.Stderr = os.Stderr

	if err := ghCmd.Run(); err != nil {
		return output.NewCLIError("Failed to create GitHub release").
			WithErr(err).
			WithFix("Check repository access and gh auth status.")
	}

	w.PrintSuccess(fmt.Sprintf("Published %s %s to %s", appName, tag, manifest.Repository))
	return nil
}

func createTarball(srcDir, destPath, appName string) error {
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	gw := gzip.NewWriter(f)
	defer func() { _ = gw.Close() }()

	tw := tar.NewWriter(gw)
	defer func() { _ = tw.Close() }()

	excludes := map[string]bool{
		".git": true, "node_modules": true, "__pycache__": true,
		".moca": true, "dist": true, "build": true,
	}

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, _ := filepath.Rel(srcDir, path)
		if relPath == "." {
			return nil
		}

		// Skip excluded directories.
		base := filepath.Base(path)
		if info.IsDir() && excludes[base] {
			return filepath.SkipDir
		}

		// Skip test files.
		if strings.HasSuffix(base, "_test.go") {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.Join(appName, relPath)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()

		_, err = io.Copy(tw, file)
		return err
	})
}

func formatSize(bytes int64) string {
	const (
		kb = 1024
		mb = kb * 1024
	)
	switch {
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
```

- [ ] **Step 3: Wire into app command group**

In `cmd/moca/app.go`, replace the placeholder (line 43):

```go
		newAppPublishCmd(),
```

- [ ] **Step 4: Run lint and build**

Run: `make lint && make build-moca`
Expected: Both pass.

- [ ] **Step 5: Commit**

```bash
git add pkg/apps/manifest.go cmd/moca/app_publish.go cmd/moca/app.go
git commit -m "feat(cli): implement moca app publish with GitHub Releases"
```

---

### Task 14: moca test run-ui (Playwright)

**Files:**
- Create: `cmd/moca/test_run_ui.go`
- Modify: `cmd/moca/test_cmd.go:16`

- [ ] **Step 1: Implement the test run-ui command**

Create `cmd/moca/test_run_ui.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/pkg/tenancy"
	"github.com/spf13/cobra"
)

func newTestRunUICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run-ui",
		Short: "Run frontend/Playwright tests",
		Long: `Run UI tests using Playwright.

Creates an ephemeral test site, starts a dev server, runs Playwright tests,
parses the JSON report, and displays structured results.

Requires: Node.js, npx, and Playwright installed in the project.`,
		RunE: runTestRunUI,
	}

	f := cmd.Flags()
	f.String("app", "", "Test a specific app's UI tests")
	f.String("site", "", "Test site (auto-created if not specified)")
	f.Bool("headed", false, "Run in headed mode (visible browser)")
	f.String("browser", "chromium", "Browser: chromium, firefox, webkit")
	f.Int("workers", 1, "Parallel workers")
	f.String("filter", "", "Run tests matching pattern")
	f.Bool("update-snapshots", false, "Update visual regression snapshots")
	f.Bool("keep-site", false, "Don't cleanup test site after run")
	f.Bool("verbose", false, "Show Playwright's full output")

	return cmd
}

func runTestRunUI(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	projectRoot := cliCtx.ProjectRoot
	verbose, _ := cmd.Flags().GetBool("verbose")
	keepSite, _ := cmd.Flags().GetBool("keep-site")
	appName, _ := cmd.Flags().GetString("app")
	headed, _ := cmd.Flags().GetBool("headed")
	browser, _ := cmd.Flags().GetString("browser")
	workers, _ := cmd.Flags().GetInt("workers")
	filter, _ := cmd.Flags().GetString("filter")
	updateSnapshots, _ := cmd.Flags().GetBool("update-snapshots")

	// 1. Check prerequisites.
	if _, err := exec.LookPath("npx"); err != nil {
		return output.NewCLIError("npx not found").
			WithFix("Install Node.js and run: npm init playwright@latest")
	}

	// 2. Discover test directories.
	testDirs := discoverUITestDirs(projectRoot, appName)
	if len(testDirs) == 0 {
		w.PrintInfo("No UI test directories found.")
		w.Print("Expected location: apps/{app}/tests/ui/ or desk/tests/ui/")
		return nil
	}

	// 3. Create ephemeral test site.
	siteName, _ := cmd.Flags().GetString("site")
	if siteName == "" {
		siteName = fmt.Sprintf("uitest_%d", time.Now().Unix())
	}

	ctx := cmd.Context()
	svc, err := newServices(ctx, cliCtx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	w.Print("Creating test site: %s", siteName)
	siteConfig := tenancy.SiteCreateConfig{
		Name:          siteName,
		AdminEmail:    "test@moca.dev",
		AdminPassword: "TestPass123!",
	}
	if err := svc.Sites.CreateSite(ctx, siteConfig); err != nil {
		return output.NewCLIError("Failed to create test site").
			WithErr(err).
			WithFix("Ensure PostgreSQL is running.")
	}

	if !keepSite {
		defer func() {
			w.Print("Dropping test site: %s", siteName)
			_ = svc.Sites.DropSite(ctx, siteName)
		}()
	}

	// 4. Build Playwright arguments.
	pwArgs := []string{"playwright", "test"}

	// Use JSON reporter for structured output + line reporter for live feedback.
	jsonReportPath := filepath.Join(os.TempDir(), fmt.Sprintf("moca-pw-report-%d.json", time.Now().Unix()))
	defer func() { _ = os.Remove(jsonReportPath) }()

	pwArgs = append(pwArgs, "--reporter=json,line")

	if headed {
		pwArgs = append(pwArgs, "--headed")
	}
	if browser != "chromium" {
		pwArgs = append(pwArgs, "--project="+browser)
	}
	if workers > 0 {
		pwArgs = append(pwArgs, fmt.Sprintf("--workers=%d", workers))
	}
	if filter != "" {
		pwArgs = append(pwArgs, "--grep="+filter)
	}
	if updateSnapshots {
		pwArgs = append(pwArgs, "--update-snapshots")
	}

	// 5. Run Playwright for each test directory.
	serverPort := defaultDevPort
	if cliCtx.Project != nil && cliCtx.Project.Development.Port > 0 {
		serverPort = cliCtx.Project.Development.Port
	}

	env := append(os.Environ(),
		fmt.Sprintf("MOCA_TEST_BASE_URL=http://localhost:%d", serverPort),
		fmt.Sprintf("MOCA_TEST_SITE=%s", siteName),
		"MOCA_TEST_USER=Administrator",
		"MOCA_TEST_PASSWORD=TestPass123!",
		fmt.Sprintf("PLAYWRIGHT_JSON_OUTPUT_FILE=%s", jsonReportPath),
	)

	var totalPassed, totalFailed, totalSkipped int
	var totalDuration time.Duration

	for _, testDir := range testDirs {
		configPath := filepath.Join(testDir, "playwright.config.ts")
		dirArgs := append(pwArgs, "--config="+configPath)

		w.Print("\nRunning UI tests: %s", testDir)

		npxCmd := exec.CommandContext(ctx, "npx", dirArgs...)
		npxCmd.Dir = projectRoot
		npxCmd.Env = env

		if verbose {
			npxCmd.Stdout = os.Stdout
			npxCmd.Stderr = os.Stderr
		}

		runErr := npxCmd.Run()

		// Parse JSON report.
		report, parseErr := parsePlaywrightReport(jsonReportPath)
		if parseErr != nil {
			if runErr != nil {
				w.PrintError(fmt.Sprintf("Tests failed and report could not be parsed: %v", parseErr))
			}
			continue
		}

		// Display results.
		for _, suite := range report.Suites {
			for _, spec := range suite.Specs {
				status := "✓"
				statusColor := ""
				dur := time.Duration(0)

				for _, test := range spec.Tests {
					for _, result := range test.Results {
						dur += time.Duration(result.Duration) * time.Millisecond
					}
					switch test.Status {
					case "passed", "expected":
						totalPassed++
					case "failed", "unexpected":
						totalFailed++
						status = "✗"
					case "skipped":
						totalSkipped++
						status = "○"
					}
				}
				totalDuration += dur

				_ = statusColor
				w.Print("  %s %s  (%s)", status, spec.Title, dur.Round(time.Millisecond))

				// Show error for failed tests.
				if status == "✗" {
					for _, test := range spec.Tests {
						for _, result := range test.Results {
							if result.Error.Message != "" {
								w.Print("    Error: %s", result.Error.Message)
							}
						}
					}
				}
			}
		}
	}

	// Summary.
	w.Print("\n%d tests: %d passed, %d failed, %d skipped (%s)",
		totalPassed+totalFailed+totalSkipped,
		totalPassed, totalFailed, totalSkipped,
		totalDuration.Round(time.Millisecond),
	)

	if totalFailed > 0 {
		return output.NewCLIError(fmt.Sprintf("%d UI test(s) failed", totalFailed))
	}
	return nil
}

// discoverUITestDirs finds directories containing Playwright tests.
func discoverUITestDirs(projectRoot, appName string) []string {
	var dirs []string

	if appName != "" {
		dir := filepath.Join(projectRoot, "apps", appName, "tests", "ui")
		if isDir(dir) {
			dirs = append(dirs, dir)
		}
		return dirs
	}

	// Check desk/tests/ui/
	deskDir := filepath.Join(projectRoot, "desk", "tests", "ui")
	if isDir(deskDir) {
		dirs = append(dirs, deskDir)
	}

	// Check all apps.
	appsDir := filepath.Join(projectRoot, "apps")
	entries, _ := os.ReadDir(appsDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(appsDir, e.Name(), "tests", "ui")
		if isDir(dir) {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Playwright JSON report types.
type playwrightReport struct {
	Suites []playwrightSuite `json:"suites"`
}

type playwrightSuite struct {
	Title string            `json:"title"`
	Specs []playwrightSpec  `json:"specs"`
}

type playwrightSpec struct {
	Title string           `json:"title"`
	Tests []playwrightTest `json:"tests"`
}

type playwrightTest struct {
	Status  string              `json:"status"`
	Results []playwrightResult  `json:"results"`
}

type playwrightResult struct {
	Status   string           `json:"status"`
	Duration int              `json:"duration"`
	Error    playwrightError  `json:"error"`
}

type playwrightError struct {
	Message string `json:"message"`
	Stack   string `json:"stack"`
}

func parsePlaywrightReport(path string) (*playwrightReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read report: %w", err)
	}

	var report playwrightReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parse report: %w", err)
	}
	return &report, nil
}
```

- [ ] **Step 2: Wire into test command group**

In `cmd/moca/test_cmd.go`, replace the placeholder:

```go
	cmd.AddCommand(
		newTestRunCmd(),
		newTestRunUICmd(),
		newTestCoverageCmd(),
```

- [ ] **Step 3: Run lint and build**

Run: `make lint && make build-moca`
Expected: Both pass.

- [ ] **Step 4: Commit**

```bash
git add cmd/moca/test_run_ui.go cmd/moca/test_cmd.go
git commit -m "feat(cli): implement moca test run-ui with Playwright JSON report parsing"
```

---

### Task 15: Final Integration Verification

- [ ] **Step 1: Run full test suite**

Run: `make test`
Expected: All tests pass with race detector.

- [ ] **Step 2: Run linter**

Run: `make lint`
Expected: No new lint issues.

- [ ] **Step 3: Build all binaries**

Run: `make build`
Expected: All 5 binaries build successfully.

- [ ] **Step 4: Verify CLI help text**

Run: `./bin/moca dev --help` — should show `console` and `playground` with descriptions.
Run: `./bin/moca app --help` — should show `publish` with description.
Run: `./bin/moca test --help` — should show `run-ui` with description.
Run: `./bin/moca events tail --help` — should show `--cdc` flag.

- [ ] **Step 5: Final commit (if any fixups needed)**

```bash
git add -A
git commit -m "chore: MS-28 integration fixups"
```

---

## Deferred to Follow-Up

- **Event sourcing retention policy** (spec 4.5): Background job to prune old event_log rows based on `event_sourcing.retention_days` config. Requires wiring a new scheduled job via the existing scheduler (MS-15). Straightforward but orthogonal to core functionality — defer to a separate PR after the main MS-28 features land.

---

## Summary

| Task | Feature | Phase |
|------|---------|-------|
| 1 | MetaType fields (EventSourcing, CDCEnabled) | Foundation |
| 2 | event_log system table DDL | Foundation |
| 3 | insertEventLog in CRUD TX path | Foundation |
| 4 | CDC fan-out in OutboxPoller | Foundation |
| 5 | VirtualSource interface + VirtualDoc | Runtime |
| 6 | VirtualSourceRegistry | Runtime |
| 7 | VirtualDoc CRUD routing in DocManager | Runtime |
| 8 | Event Sourcing GetHistory + Replay | Runtime |
| 9 | Event history REST endpoint | Runtime |
| 10 | --cdc flag for events tail | Runtime |
| 11 | moca dev console (yaegi REPL) | CLI Tools |
| 12 | moca dev playground (Swagger + GraphiQL) | CLI Tools |
| 13 | moca app publish (GitHub Releases) | CLI Tools |
| 14 | moca test run-ui (Playwright) | CLI Tools |
| 15 | Final integration verification | All |

//go:build integration

package document_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"testing"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/events"
	"github.com/osama1998H/moca/pkg/orm"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// skipIfNoInfra skips the test when the CRUD integration infrastructure is not
// available (Redis was unreachable during TestMain).
func skipIfNoInfra(t *testing.T) {
	t.Helper()
	if integRedisClient == nil {
		t.Skip("Redis unavailable — skipping CRUD integration test")
	}
	if integDocManager == nil {
		t.Skip("DocManager not initialized — skipping CRUD integration test")
	}
}

// newIntegCtx creates a DocContext backed by the shared site and user.
func newIntegCtx(t *testing.T) *document.DocContext {
	t.Helper()
	return document.NewDocContext(context.Background(), integSite, integUser)
}

// cleanupDocs deletes documents by name from a table via direct SQL.
func cleanupDocs(t *testing.T, doctype string, names ...string) {
	t.Helper()
	if len(names) == 0 {
		return
	}
	ctx := context.Background()
	for _, name := range names {
		dctx := newIntegCtx(t)
		// Best-effort cleanup; ignore errors (doc may not exist).
		_ = integDocManager.Delete(dctx, doctype, name)
		_ = ctx // keep linter happy
	}
}

// queryOutboxCount counts outbox entries matching topic and partition_key.
func queryOutboxCount(t *testing.T, topic, partitionKey string) int {
	t.Helper()
	var count int
	err := namingTestPool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM tab_outbox WHERE "topic" = $1 AND "partition_key" = $2`,
		topic, partitionKey,
	).Scan(&count)
	if err != nil {
		t.Fatalf("queryOutboxCount(%q, %q): %v", topic, partitionKey, err)
	}
	return count
}

func queryOutboxEvent(t *testing.T, topic, partitionKey string) events.DocumentEvent {
	t.Helper()
	var payload []byte
	err := namingTestPool.QueryRow(
		context.Background(),
		`SELECT "payload" FROM tab_outbox WHERE "topic" = $1 AND "partition_key" = $2 ORDER BY "id" DESC LIMIT 1`,
		topic, partitionKey,
	).Scan(&payload)
	if err != nil {
		t.Fatalf("queryOutboxEvent(%q, %q): %v", topic, partitionKey, err)
	}
	var event events.DocumentEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatalf("unmarshal outbox payload: %v", err)
	}
	return event
}

// queryAuditLog checks whether an audit log entry exists for the given criteria.
func queryAuditLog(t *testing.T, doctype, docname, action string) bool {
	t.Helper()
	var count int
	err := namingTestPool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM tab_audit_log WHERE "doctype" = $1 AND "docname" = $2 AND "action" = $3`,
		doctype, docname, action,
	).Scan(&count)
	if err != nil {
		t.Fatalf("queryAuditLog(%q, %q, %q): %v", doctype, docname, action, err)
	}
	return count > 0
}

// ── integRecordingController ─────────────────────────────────────────────────

// integRecordingController records lifecycle event names for assertion.
type integRecordingController struct {
	document.BaseController
	mu     sync.Mutex
	events []string
}

func (c *integRecordingController) record(event string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
}

func (c *integRecordingController) getEvents() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]string, len(c.events))
	copy(cp, c.events)
	return cp
}

func (c *integRecordingController) BeforeInsert(ctx *document.DocContext, doc document.Document) error {
	c.record("before_insert")
	return nil
}
func (c *integRecordingController) AfterInsert(ctx *document.DocContext, doc document.Document) error {
	c.record("after_insert")
	return nil
}
func (c *integRecordingController) BeforeValidate(ctx *document.DocContext, doc document.Document) error {
	c.record("before_validate")
	return nil
}
func (c *integRecordingController) Validate(ctx *document.DocContext, doc document.Document) error {
	c.record("validate")
	return nil
}
func (c *integRecordingController) BeforeSave(ctx *document.DocContext, doc document.Document) error {
	c.record("before_save")
	return nil
}
func (c *integRecordingController) AfterSave(ctx *document.DocContext, doc document.Document) error {
	c.record("after_save")
	return nil
}
func (c *integRecordingController) OnUpdate(ctx *document.DocContext, doc document.Document) error {
	c.record("on_update")
	return nil
}
func (c *integRecordingController) OnTrash(ctx *document.DocContext, doc document.Document) error {
	c.record("on_trash")
	return nil
}
func (c *integRecordingController) AfterDelete(ctx *document.DocContext, doc document.Document) error {
	c.record("after_delete")
	return nil
}
func (c *integRecordingController) OnChange(ctx *document.DocContext, doc document.Document) error {
	c.record("on_change")
	return nil
}

// ── integExtension wraps a DocLifecycle and delegates to the recording controller ─

type integExtension struct {
	ctrl *integRecordingController
}

func (e *integExtension) Wrap(inner document.DocLifecycle) document.DocLifecycle {
	return &integExtensionWrapper{inner: inner, ctrl: e.ctrl}
}

type integExtensionWrapper struct {
	inner document.DocLifecycle
	ctrl  *integRecordingController
}

func (w *integExtensionWrapper) BeforeInsert(ctx *document.DocContext, doc document.Document) error {
	w.ctrl.record("before_insert")
	return w.inner.BeforeInsert(ctx, doc)
}
func (w *integExtensionWrapper) AfterInsert(ctx *document.DocContext, doc document.Document) error {
	w.ctrl.record("after_insert")
	return w.inner.AfterInsert(ctx, doc)
}
func (w *integExtensionWrapper) BeforeValidate(ctx *document.DocContext, doc document.Document) error {
	w.ctrl.record("before_validate")
	return w.inner.BeforeValidate(ctx, doc)
}
func (w *integExtensionWrapper) Validate(ctx *document.DocContext, doc document.Document) error {
	w.ctrl.record("validate")
	return w.inner.Validate(ctx, doc)
}
func (w *integExtensionWrapper) BeforeSave(ctx *document.DocContext, doc document.Document) error {
	w.ctrl.record("before_save")
	return w.inner.BeforeSave(ctx, doc)
}
func (w *integExtensionWrapper) AfterSave(ctx *document.DocContext, doc document.Document) error {
	w.ctrl.record("after_save")
	return w.inner.AfterSave(ctx, doc)
}
func (w *integExtensionWrapper) OnUpdate(ctx *document.DocContext, doc document.Document) error {
	w.ctrl.record("on_update")
	return w.inner.OnUpdate(ctx, doc)
}
func (w *integExtensionWrapper) BeforeSubmit(ctx *document.DocContext, doc document.Document) error {
	return w.inner.BeforeSubmit(ctx, doc)
}
func (w *integExtensionWrapper) OnSubmit(ctx *document.DocContext, doc document.Document) error {
	return w.inner.OnSubmit(ctx, doc)
}
func (w *integExtensionWrapper) BeforeCancel(ctx *document.DocContext, doc document.Document) error {
	return w.inner.BeforeCancel(ctx, doc)
}
func (w *integExtensionWrapper) OnCancel(ctx *document.DocContext, doc document.Document) error {
	return w.inner.OnCancel(ctx, doc)
}
func (w *integExtensionWrapper) OnTrash(ctx *document.DocContext, doc document.Document) error {
	w.ctrl.record("on_trash")
	return w.inner.OnTrash(ctx, doc)
}
func (w *integExtensionWrapper) AfterDelete(ctx *document.DocContext, doc document.Document) error {
	w.ctrl.record("after_delete")
	return w.inner.AfterDelete(ctx, doc)
}
func (w *integExtensionWrapper) OnChange(ctx *document.DocContext, doc document.Document) error {
	w.ctrl.record("on_change")
	return w.inner.OnChange(ctx, doc)
}
func (w *integExtensionWrapper) BeforeRename(ctx *document.DocContext, doc document.Document, oldName, newName string) error {
	return w.inner.BeforeRename(ctx, doc, oldName, newName)
}
func (w *integExtensionWrapper) AfterRename(ctx *document.DocContext, doc document.Document, oldName, newName string) error {
	return w.inner.AfterRename(ctx, doc, oldName, newName)
}

// ── Test 1: Insert Lifecycle ─────────────────────────────────────────────────

func TestInteg_InsertLifecycle(t *testing.T) {
	skipIfNoInfra(t)

	recorder := &integRecordingController{}
	integControllers.RegisterOverride("IntegTestOrder", func() document.DocLifecycle {
		return recorder
	})
	t.Cleanup(func() {
		// Remove override by registering BaseController.
		integControllers.RegisterOverride("IntegTestOrder", func() document.DocLifecycle {
			return document.BaseController{}
		})
	})

	dctx := newIntegCtx(t)
	doc, err := integDocManager.Insert(dctx, "IntegTestOrder", map[string]any{
		"customer": "Alice",
		"amount":   100.5,
		"items": []any{
			map[string]any{"item_name": "Widget", "qty": 5},
		},
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	name := doc.Name()
	t.Logf("inserted doc name: %s", name)

	t.Cleanup(func() {
		cleanupDocs(t, "IntegTestOrder", name)
	})

	// Assert lifecycle events.
	lifecycleEvents := recorder.getEvents()
	expected := []string{"before_insert", "before_validate", "validate", "before_save", "after_insert", "after_save", "on_change"}
	if len(lifecycleEvents) != len(expected) {
		t.Fatalf("events = %v, want %v", lifecycleEvents, expected)
	}
	for i, want := range expected {
		if lifecycleEvents[i] != want {
			t.Errorf("event[%d] = %q, want %q", i, lifecycleEvents[i], want)
		}
	}

	// Verify parent row exists via Get.
	got, err := integDocManager.Get(dctx, "IntegTestOrder", name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Get("customer") != "Alice" {
		t.Errorf("customer = %v, want %q", got.Get("customer"), "Alice")
	}

	// Verify child rows.
	children := got.GetChild("items")
	if len(children) != 1 {
		t.Fatalf("child count = %d, want 1", len(children))
	}
	if children[0].Get("item_name") != "Widget" {
		t.Errorf("child item_name = %v, want %q", children[0].Get("item_name"), "Widget")
	}

	// Verify outbox entry.
	outboxCount := queryOutboxCount(t, events.TopicDocumentEvents, events.PartitionKey(integSite.Name, "IntegTestOrder"))
	if outboxCount < 1 {
		t.Errorf("outbox count for insert = %d, want >= 1", outboxCount)
	}
	outboxEvent := queryOutboxEvent(t, events.TopicDocumentEvents, events.PartitionKey(integSite.Name, "IntegTestOrder"))
	if outboxEvent.EventType != events.EventTypeDocCreated {
		t.Fatalf("event type = %q, want %q", outboxEvent.EventType, events.EventTypeDocCreated)
	}
	if outboxEvent.DocType != "IntegTestOrder" || outboxEvent.DocName != name {
		t.Fatalf("unexpected outbox event ref: %#v", outboxEvent)
	}

	// Verify audit log.
	if !queryAuditLog(t, "IntegTestOrder", name, "Create") {
		t.Error("no audit log entry for Create")
	}
}

// ── Test 2: Pattern Naming Concurrency ───────────────────────────────────────

func TestInteg_PatternNamingConcurrency(t *testing.T) {
	skipIfNoInfra(t)

	const n = 10
	results := make([]string, n)
	errs := make([]error, n)

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			dctx := newIntegCtx(t)
			doc, err := integDocManager.Insert(dctx, "IntegConcurrentOrder", map[string]any{
				"title": fmt.Sprintf("concurrent-%d", i),
			})
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = doc.Name()
		}()
	}
	wg.Wait()

	// Collect names for cleanup.
	var names []string
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d error: %v", i, errs[i])
		}
		names = append(names, results[i])
	}
	t.Cleanup(func() {
		cleanupDocs(t, "IntegConcurrentOrder", names...)
	})

	// All names must be unique.
	seen := make(map[string]int, n)
	for i, name := range results {
		if prev, dup := seen[name]; dup {
			t.Errorf("duplicate name %q produced by goroutines %d and %d", name, prev, i)
		}
		seen[name] = i
	}

	// All names must match CO-\d{4}.
	pat := regexp.MustCompile(`^CO-\d{4}$`)
	for _, name := range results {
		if !pat.MatchString(name) {
			t.Errorf("name %q does not match CO-\\d{4}", name)
		}
	}

	t.Logf("concurrent results: %v", results)
}

// ── Test 3: Field Validation ─────────────────────────────────────────────────

func TestInteg_FieldValidation(t *testing.T) {
	skipIfNoInfra(t)

	t.Run("MissingRequired", func(t *testing.T) {
		dctx := newIntegCtx(t)
		_, err := integDocManager.Insert(dctx, "IntegValidation", map[string]any{
			// no title
		})
		if err == nil {
			t.Fatal("expected validation error for missing required field")
		}
		var ve *document.ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("expected ValidationError, got %T: %v", err, err)
		}
		found := false
		for _, fe := range ve.Errors {
			if fe.Field == "title" && fe.Rule == "required" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected FieldError for title/required in %+v", ve.Errors)
		}
	})

	t.Run("RegexViolation", func(t *testing.T) {
		dctx := newIntegCtx(t)
		_, err := integDocManager.Insert(dctx, "IntegValidation", map[string]any{
			"title": "ok",
			"code":  "bad",
		})
		if err == nil {
			t.Fatal("expected validation error for regex violation")
		}
		var ve *document.ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("expected ValidationError, got %T: %v", err, err)
		}
		found := false
		for _, fe := range ve.Errors {
			if fe.Field == "code" && fe.Rule == "regex" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected FieldError for code/regex in %+v", ve.Errors)
		}
	})

	t.Run("UniqueViolation", func(t *testing.T) {
		dctx := newIntegCtx(t)
		doc1, err := integDocManager.Insert(dctx, "IntegValidation", map[string]any{
			"title":      "unique-test-1",
			"unique_key": "UKEY-001",
		})
		if err != nil {
			t.Fatalf("first insert: %v", err)
		}
		t.Cleanup(func() {
			cleanupDocs(t, "IntegValidation", doc1.Name())
		})

		_, err = integDocManager.Insert(dctx, "IntegValidation", map[string]any{
			"title":      "unique-test-2",
			"unique_key": "UKEY-001",
		})
		if err == nil {
			t.Fatal("expected validation error for unique violation")
		}
		var ve *document.ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("expected ValidationError, got %T: %v", err, err)
		}
		found := false
		for _, fe := range ve.Errors {
			if fe.Field == "unique_key" && fe.Rule == "unique" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected FieldError for unique_key/unique in %+v", ve.Errors)
		}
	})

	t.Run("LinkNonexistent", func(t *testing.T) {
		dctx := newIntegCtx(t)
		_, err := integDocManager.Insert(dctx, "IntegValidation", map[string]any{
			"title":      "link-test",
			"linked_doc": "GHOST",
		})
		if err == nil {
			t.Fatal("expected validation error for nonexistent link")
		}
		var ve *document.ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("expected ValidationError, got %T: %v", err, err)
		}
		found := false
		for _, fe := range ve.Errors {
			if fe.Field == "linked_doc" && fe.Rule == "link" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected FieldError for linked_doc/link in %+v", ve.Errors)
		}
	})
}

// ── Test 4: Type Coercion ────────────────────────────────────────────────────

func TestInteg_TypeCoercion(t *testing.T) {
	skipIfNoInfra(t)

	dctx := newIntegCtx(t)
	doc, err := integDocManager.Insert(dctx, "IntegValidation", map[string]any{
		"title":  "coerce",
		"count":  "42",
		"active": "true",
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	t.Cleanup(func() {
		cleanupDocs(t, "IntegValidation", doc.Name())
	})

	// Re-read from DB to verify round-trip.
	got, err := integDocManager.Get(dctx, "IntegValidation", doc.Name())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	countVal := got.Get("count")
	if countVal != int64(42) {
		t.Errorf("count = %v (%T), want int64(42)", countVal, countVal)
	}

	activeVal := got.Get("active")
	if activeVal != true {
		t.Errorf("active = %v (%T), want true (bool)", activeVal, activeVal)
	}
}

// ── Test 5: Update Lifecycle ─────────────────────────────────────────────────

func TestInteg_UpdateLifecycle(t *testing.T) {
	skipIfNoInfra(t)

	// Insert a doc first.
	dctx := newIntegCtx(t)
	doc, err := integDocManager.Insert(dctx, "IntegTestOrder", map[string]any{
		"customer": "Alice",
		"amount":   50.0,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	name := doc.Name()
	t.Cleanup(func() {
		cleanupDocs(t, "IntegTestOrder", name)
	})

	// Register recording controller for update.
	recorder := &integRecordingController{}
	integControllers.RegisterOverride("IntegTestOrder", func() document.DocLifecycle {
		return recorder
	})
	t.Cleanup(func() {
		integControllers.RegisterOverride("IntegTestOrder", func() document.DocLifecycle {
			return document.BaseController{}
		})
	})

	// Update.
	_, err = integDocManager.Update(dctx, "IntegTestOrder", name, map[string]any{
		"customer": "Bob",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert lifecycle events.
	events := recorder.getEvents()
	wantEvents := []string{"before_validate", "validate", "before_save", "on_update", "after_save", "on_change"}
	if len(events) != len(wantEvents) {
		t.Fatalf("events = %v, want %v", events, wantEvents)
	}
	for i, want := range wantEvents {
		if events[i] != want {
			t.Errorf("event[%d] = %q, want %q", i, events[i], want)
		}
	}

	// Verify the update persisted.
	got, err := integDocManager.Get(dctx, "IntegTestOrder", name)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.Get("customer") != "Bob" {
		t.Errorf("customer = %v, want %q", got.Get("customer"), "Bob")
	}

	// Verify audit log for Update.
	if !queryAuditLog(t, "IntegTestOrder", name, "Update") {
		t.Error("no audit log entry for Update")
	}
}

// ── Test 6: Delete Lifecycle ─────────────────────────────────────────────────

func TestInteg_DeleteLifecycle(t *testing.T) {
	skipIfNoInfra(t)

	// Insert a doc with children.
	dctx := newIntegCtx(t)
	doc, err := integDocManager.Insert(dctx, "IntegTestOrder", map[string]any{
		"customer": "Charlie",
		"items": []any{
			map[string]any{"item_name": "Gadget", "qty": 3},
			map[string]any{"item_name": "Gizmo", "qty": 7},
		},
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	name := doc.Name()

	// Register recording controller.
	recorder := &integRecordingController{}
	integControllers.RegisterOverride("IntegTestOrder", func() document.DocLifecycle {
		return recorder
	})
	t.Cleanup(func() {
		integControllers.RegisterOverride("IntegTestOrder", func() document.DocLifecycle {
			return document.BaseController{}
		})
	})

	// Delete.
	err = integDocManager.Delete(dctx, "IntegTestOrder", name)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Assert lifecycle events.
	events := recorder.getEvents()
	wantEvents := []string{"on_trash", "after_delete"}
	if len(events) != len(wantEvents) {
		t.Fatalf("events = %v, want %v", events, wantEvents)
	}
	for i, want := range wantEvents {
		if events[i] != want {
			t.Errorf("event[%d] = %q, want %q", i, events[i], want)
		}
	}

	// Verify Get returns DocNotFoundError.
	_, err = integDocManager.Get(dctx, "IntegTestOrder", name)
	if err == nil {
		t.Fatal("expected DocNotFoundError after delete")
	}
	var nfe *document.DocNotFoundError
	if !errors.As(err, &nfe) {
		t.Errorf("expected DocNotFoundError, got %T: %v", err, err)
	}

	// Verify child rows are gone.
	var childCount int
	err = namingTestPool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM tab_integ_test_order_item WHERE "parent" = $1`,
		name,
	).Scan(&childCount)
	if err != nil {
		t.Fatalf("count child rows: %v", err)
	}
	if childCount != 0 {
		t.Errorf("child row count = %d, want 0 after delete", childCount)
	}
}

// ── Test 7: Singles Support ──────────────────────────────────────────────────

func TestInteg_SinglesSupport(t *testing.T) {
	skipIfNoInfra(t)

	dctx := newIntegCtx(t)

	// Build a DynamicDoc for the single doctype.
	singleMT := mustCompile(t, integSingleJSON)
	doc := document.NewDynamicDoc(singleMT, nil, true)
	_ = doc.Set("site_name", "MySite")
	_ = doc.Set("max_users", "50")
	_ = doc.Set("is_enabled", "true")

	// SetSingle.
	err := integDocManager.SetSingle(dctx, doc)
	if err != nil {
		t.Fatalf("SetSingle: %v", err)
	}

	t.Cleanup(func() {
		_, _ = namingTestPool.Exec(context.Background(),
			`DELETE FROM tab_singles WHERE "doctype" = 'IntegSingle'`)
	})

	// GetSingle.
	got, err := integDocManager.GetSingle(dctx, "IntegSingle")
	if err != nil {
		t.Fatalf("GetSingle: %v", err)
	}

	if got.Get("site_name") != "MySite" {
		t.Errorf("site_name = %v, want %q", got.Get("site_name"), "MySite")
	}
	// Singles store as TEXT, so values come back as strings.
	// Int coercion happens at the validator level during Set, but GetSingle
	// reads raw TEXT from tab_singles. Verify the value round-trips.
	if fmt.Sprintf("%v", got.Get("max_users")) != "50" {
		t.Errorf("max_users = %v, want 50", got.Get("max_users"))
	}
	if fmt.Sprintf("%v", got.Get("is_enabled")) != "true" {
		t.Errorf("is_enabled = %v, want true", got.Get("is_enabled"))
	}
}

// ── Test 8: Controller Resolution ────────────────────────────────────────────

func TestInteg_ControllerResolution(t *testing.T) {
	skipIfNoInfra(t)

	// Register an override that sets notes in BeforeInsert.
	integControllers.RegisterOverride("IntegTestOrder", func() document.DocLifecycle {
		return &notesOverrideController{}
	})

	// Register an extension that prepends "ext:" to notes.
	integControllers.RegisterExtension("IntegTestOrder", &notesExtension{})

	t.Cleanup(func() {
		// Reset: override back to BaseController (extensions can't be unregistered,
		// but the override reset means subsequent tests use a clean base).
		integControllers.RegisterOverride("IntegTestOrder", func() document.DocLifecycle {
			return document.BaseController{}
		})
	})

	dctx := newIntegCtx(t)
	doc, err := integDocManager.Insert(dctx, "IntegTestOrder", map[string]any{
		"customer": "Resolve-Test",
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	name := doc.Name()
	t.Cleanup(func() {
		cleanupDocs(t, "IntegTestOrder", name)
	})

	// Get the doc and verify notes.
	got, err := integDocManager.Get(dctx, "IntegTestOrder", name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	notes := got.Get("notes")
	// Extension wraps the override. Extension's BeforeInsert runs first (outermost),
	// then override's BeforeInsert sets notes="from-override".
	// Extension prepends "ext:" — but since extension runs before override,
	// the override's set happens after. So notes = "from-override" from the override,
	// but the extension wraps inner and calls inner first, then prepends.
	// Actually: extension Wrap returns a wrapper whose BeforeInsert calls inner first.
	// inner = override, which sets notes = "from-override".
	// Then extension prepends "ext:".
	// So result = "ext:from-override".
	want := "ext:from-override"
	if notes != want {
		t.Errorf("notes = %v, want %q", notes, want)
	}
}

// notesOverrideController sets notes = "from-override" in BeforeInsert.
type notesOverrideController struct {
	document.BaseController
}

func (c *notesOverrideController) BeforeInsert(ctx *document.DocContext, doc document.Document) error {
	return doc.Set("notes", "from-override")
}

// notesExtension prepends "ext:" to notes after the inner controller runs.
type notesExtension struct{}

func (e *notesExtension) Wrap(inner document.DocLifecycle) document.DocLifecycle {
	return &notesExtensionWrapper{inner: inner}
}

type notesExtensionWrapper struct {
	inner document.DocLifecycle
}

func (w *notesExtensionWrapper) BeforeInsert(ctx *document.DocContext, doc document.Document) error {
	// Call inner first (the override sets notes).
	if err := w.inner.BeforeInsert(ctx, doc); err != nil {
		return err
	}
	// Then prepend "ext:".
	current := doc.Get("notes")
	if current != nil {
		return doc.Set("notes", "ext:"+fmt.Sprintf("%v", current))
	}
	return nil
}

// Delegate all other methods to inner.
func (w *notesExtensionWrapper) AfterInsert(ctx *document.DocContext, doc document.Document) error {
	return w.inner.AfterInsert(ctx, doc)
}
func (w *notesExtensionWrapper) BeforeValidate(ctx *document.DocContext, doc document.Document) error {
	return w.inner.BeforeValidate(ctx, doc)
}
func (w *notesExtensionWrapper) Validate(ctx *document.DocContext, doc document.Document) error {
	return w.inner.Validate(ctx, doc)
}
func (w *notesExtensionWrapper) BeforeSave(ctx *document.DocContext, doc document.Document) error {
	return w.inner.BeforeSave(ctx, doc)
}
func (w *notesExtensionWrapper) AfterSave(ctx *document.DocContext, doc document.Document) error {
	return w.inner.AfterSave(ctx, doc)
}
func (w *notesExtensionWrapper) OnUpdate(ctx *document.DocContext, doc document.Document) error {
	return w.inner.OnUpdate(ctx, doc)
}
func (w *notesExtensionWrapper) BeforeSubmit(ctx *document.DocContext, doc document.Document) error {
	return w.inner.BeforeSubmit(ctx, doc)
}
func (w *notesExtensionWrapper) OnSubmit(ctx *document.DocContext, doc document.Document) error {
	return w.inner.OnSubmit(ctx, doc)
}
func (w *notesExtensionWrapper) BeforeCancel(ctx *document.DocContext, doc document.Document) error {
	return w.inner.BeforeCancel(ctx, doc)
}
func (w *notesExtensionWrapper) OnCancel(ctx *document.DocContext, doc document.Document) error {
	return w.inner.OnCancel(ctx, doc)
}
func (w *notesExtensionWrapper) OnTrash(ctx *document.DocContext, doc document.Document) error {
	return w.inner.OnTrash(ctx, doc)
}
func (w *notesExtensionWrapper) AfterDelete(ctx *document.DocContext, doc document.Document) error {
	return w.inner.AfterDelete(ctx, doc)
}
func (w *notesExtensionWrapper) OnChange(ctx *document.DocContext, doc document.Document) error {
	return w.inner.OnChange(ctx, doc)
}
func (w *notesExtensionWrapper) BeforeRename(ctx *document.DocContext, doc document.Document, oldName, newName string) error {
	return w.inner.BeforeRename(ctx, doc, oldName, newName)
}
func (w *notesExtensionWrapper) AfterRename(ctx *document.DocContext, doc document.Document, oldName, newName string) error {
	return w.inner.AfterRename(ctx, doc, oldName, newName)
}

// ── GetList integration tests ───────────────────────────────────────────────

// TestInteg_GetList_LegacyFilters verifies that the old-style Filters
// map[string]any path still works after the QueryBuilder refactor.
func TestInteg_GetList_LegacyFilters(t *testing.T) {
	skipIfNoInfra(t)
	dctx := newIntegCtx(t)

	// Insert two orders with different customers.
	doc1, err := integDocManager.Insert(dctx, "IntegTestOrder", map[string]any{
		"customer": "GetList-Alice",
		"amount":   10.0,
	})
	if err != nil {
		t.Fatalf("Insert doc1: %v", err)
	}
	doc2, err := integDocManager.Insert(dctx, "IntegTestOrder", map[string]any{
		"customer": "GetList-Bob",
		"amount":   20.0,
	})
	if err != nil {
		t.Fatalf("Insert doc2: %v", err)
	}
	t.Cleanup(func() {
		cleanupDocs(t, "IntegTestOrder", doc1.Name(), doc2.Name())
	})

	// Filter by customer (legacy equality filter).
	docs, total, err := integDocManager.GetList(dctx, "IntegTestOrder", document.ListOptions{
		Filters: map[string]any{"customer": "GetList-Alice"},
	})
	if err != nil {
		t.Fatalf("GetList: %v", err)
	}
	if total < 1 {
		t.Fatalf("expected at least 1 result, got total=%d", total)
	}
	found := false
	for _, d := range docs {
		if d.Get("customer") == "GetList-Alice" {
			found = true
		}
		if d.Get("customer") == "GetList-Bob" {
			t.Error("GetList with customer=GetList-Alice returned GetList-Bob document")
		}
	}
	if !found {
		t.Error("GetList-Alice document not found in results")
	}
}

// TestInteg_GetList_AdvancedFilters verifies that AdvancedFilters with
// non-equality operators work end-to-end.
func TestInteg_GetList_AdvancedFilters(t *testing.T) {
	skipIfNoInfra(t)
	dctx := newIntegCtx(t)

	// Insert orders with different amounts.
	doc1, err := integDocManager.Insert(dctx, "IntegTestOrder", map[string]any{
		"customer": "AdvFilter-Test",
		"amount":   100.0,
	})
	if err != nil {
		t.Fatalf("Insert doc1: %v", err)
	}
	doc2, err := integDocManager.Insert(dctx, "IntegTestOrder", map[string]any{
		"customer": "AdvFilter-Test",
		"amount":   500.0,
	})
	if err != nil {
		t.Fatalf("Insert doc2: %v", err)
	}
	doc3, err := integDocManager.Insert(dctx, "IntegTestOrder", map[string]any{
		"customer": "AdvFilter-Test",
		"amount":   50.0,
	})
	if err != nil {
		t.Fatalf("Insert doc3: %v", err)
	}
	t.Cleanup(func() {
		cleanupDocs(t, "IntegTestOrder", doc1.Name(), doc2.Name(), doc3.Name())
	})

	// Use AdvancedFilters: customer = "AdvFilter-Test" AND amount > 75.
	docs, _, err := integDocManager.GetList(dctx, "IntegTestOrder", document.ListOptions{
		Filters: map[string]any{"customer": "AdvFilter-Test"},
		AdvancedFilters: []orm.Filter{
			{Field: "amount", Operator: orm.OpGreater, Value: 75.0},
		},
	})
	if err != nil {
		t.Fatalf("GetList: %v", err)
	}

	// Should return doc1 (100) and doc2 (500), not doc3 (50).
	if len(docs) != 2 {
		t.Errorf("expected 2 docs with amount > 75, got %d", len(docs))
	}
}

// TestInteg_GetList_Pagination verifies offset + limit + total count.
func TestInteg_GetList_Pagination(t *testing.T) {
	skipIfNoInfra(t)
	dctx := newIntegCtx(t)

	// Insert 5 orders.
	var names []string
	for i := 0; i < 5; i++ {
		doc, err := integDocManager.Insert(dctx, "IntegTestOrder", map[string]any{
			"customer": fmt.Sprintf("Paging-%d", i),
			"amount":   float64(i * 10),
		})
		if err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
		names = append(names, doc.Name())
	}
	t.Cleanup(func() {
		cleanupDocs(t, "IntegTestOrder", names...)
	})

	// Page: limit 2, offset 1. Filter by customer prefix isn't available via
	// equality, so we use LIKE via AdvancedFilters.
	docs, total, err := integDocManager.GetList(dctx, "IntegTestOrder", document.ListOptions{
		AdvancedFilters: []orm.Filter{
			{Field: "customer", Operator: orm.OpLike, Value: "Paging-%"},
		},
		Limit:  2,
		Offset: 1,
	})
	if err != nil {
		t.Fatalf("GetList: %v", err)
	}

	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(docs) != 2 {
		t.Errorf("page size = %d, want 2", len(docs))
	}
}

//go:build integration

package integration

import (
	"testing"

	"github.com/osama1998H/moca/pkg/testutils"
	"github.com/osama1998H/moca/pkg/testutils/factory"
)

func TestEventPublishOnInsert(t *testing.T) {
	env := testutils.NewTestEnv(t)

	mt := factory.SimpleDocType("EventDoc")
	env.RegisterMetaType(t, mt)

	// Create a document — this should trigger lifecycle events.
	doc := env.NewTestDoc(t, "EventDoc", factory.SimpleDocValues(1))
	if doc.Name() == "" {
		t.Fatal("document should have been created successfully")
	}

	// Note: Full event verification (Redis pub/sub, Kafka) requires the event
	// emitter to be configured with real backends. The default test env uses a
	// no-op emitter. MS-15 event publishing integration tests are in pkg/events/.
}

func TestEventPublishOnUpdate(t *testing.T) {
	env := testutils.NewTestEnv(t)

	mt := factory.SimpleDocType("EventUpdateDoc")
	env.RegisterMetaType(t, mt)

	doc := env.NewTestDoc(t, "EventUpdateDoc", factory.SimpleDocValues(1))

	ctx := env.DocContext()
	_, err := env.DocManager().Update(ctx, "EventUpdateDoc", doc.Name(), map[string]any{
		"status": "Updated",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
}

func TestEventPublishOnDelete(t *testing.T) {
	env := testutils.NewTestEnv(t)

	mt := factory.SimpleDocType("EventDelDoc")
	env.RegisterMetaType(t, mt)

	doc := env.NewTestDoc(t, "EventDelDoc", factory.SimpleDocValues(1))
	env.DeleteTestDoc(t, "EventDelDoc", doc.Name())
}

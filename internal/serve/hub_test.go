package serve

import (
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestConnection(hub *Hub, site, email string) *Connection {
	return &Connection{
		hub:    hub,
		send:   make(chan []byte, sendBufferSize),
		site:   site,
		email:  email,
		subs:   make(map[string]bool),
		logger: testLogger(),
	}
}

func TestHub_RegisterUnregister(t *testing.T) {
	hub := NewHub(testLogger())

	c := newTestConnection(hub, "acme", "user@test.com")
	hub.Register(c)
	if hub.ConnectionCount() != 1 {
		t.Fatalf("expected 1 connection, got %d", hub.ConnectionCount())
	}

	hub.Unregister(c)
	if hub.ConnectionCount() != 0 {
		t.Fatalf("expected 0 connections, got %d", hub.ConnectionCount())
	}
}

func TestHub_UnregisterIdempotent(t *testing.T) {
	hub := NewHub(testLogger())
	c := newTestConnection(hub, "acme", "user@test.com")
	hub.Register(c)
	hub.Unregister(c)
	hub.Unregister(c) // should not panic
	if hub.ConnectionCount() != 0 {
		t.Fatalf("expected 0 connections, got %d", hub.ConnectionCount())
	}
}

func TestHub_SubscribeBroadcast(t *testing.T) {
	hub := NewHub(testLogger())
	c := newTestConnection(hub, "acme", "user@test.com")
	hub.Register(c)
	defer hub.Unregister(c)

	hub.Subscribe(c, "SalesOrder")
	hub.Broadcast("acme", "SalesOrder", []byte(`{"type":"doc_update"}`))

	select {
	case msg := <-c.send:
		if string(msg) != `{"type":"doc_update"}` {
			t.Fatalf("unexpected message: %s", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
}

func TestHub_UnsubscribePreventsReceive(t *testing.T) {
	hub := NewHub(testLogger())
	c := newTestConnection(hub, "acme", "user@test.com")
	hub.Register(c)
	defer hub.Unregister(c)

	hub.Subscribe(c, "SalesOrder")
	hub.Unsubscribe(c, "SalesOrder")
	hub.Broadcast("acme", "SalesOrder", []byte(`{"type":"doc_update"}`))

	select {
	case <-c.send:
		t.Fatal("should not have received message after unsubscribe")
	case <-time.After(50 * time.Millisecond):
		// expected — no message
	}
}

func TestHub_MultipleConnections(t *testing.T) {
	hub := NewHub(testLogger())
	c1 := newTestConnection(hub, "acme", "user1@test.com")
	c2 := newTestConnection(hub, "acme", "user2@test.com")
	hub.Register(c1)
	hub.Register(c2)
	defer hub.Unregister(c1)
	defer hub.Unregister(c2)

	hub.Subscribe(c1, "SalesOrder")
	hub.Subscribe(c2, "SalesOrder")
	hub.Broadcast("acme", "SalesOrder", []byte(`{"type":"doc_update"}`))

	for i, c := range []*Connection{c1, c2} {
		select {
		case msg := <-c.send:
			if string(msg) != `{"type":"doc_update"}` {
				t.Fatalf("conn %d: unexpected message: %s", i, msg)
			}
		case <-time.After(time.Second):
			t.Fatalf("conn %d: timed out waiting for broadcast", i)
		}
	}
}

func TestHub_BroadcastDropsSlowClient(t *testing.T) {
	hub := NewHub(testLogger())

	// Create connection with a tiny buffer to test backpressure.
	c := &Connection{
		hub:    hub,
		send:   make(chan []byte, 1), // buffer of 1
		site:   "acme",
		email:  "slow@test.com",
		subs:   make(map[string]bool),
		logger: testLogger(),
	}
	hub.Register(c)
	defer hub.Unregister(c)
	hub.Subscribe(c, "SalesOrder")

	// Fill the buffer.
	c.send <- []byte("fill")

	// This broadcast should not block even though the channel is full.
	done := make(chan struct{})
	go func() {
		hub.Broadcast("acme", "SalesOrder", []byte(`{"dropped":"true"}`))
		close(done)
	}()

	select {
	case <-done:
		// expected — broadcast did not block
	case <-time.After(time.Second):
		t.Fatal("broadcast blocked on slow client")
	}
}

func TestHub_UnregisterCleansUpSubscriptions(t *testing.T) {
	hub := NewHub(testLogger())
	c := newTestConnection(hub, "acme", "user@test.com")
	hub.Register(c)

	hub.Subscribe(c, "SalesOrder")
	hub.Subscribe(c, "PurchaseOrder")
	hub.Unregister(c)

	// Broadcast should reach no one.
	hub.Broadcast("acme", "SalesOrder", []byte(`test`))
	hub.Broadcast("acme", "PurchaseOrder", []byte(`test`))

	hub.mu.RLock()
	defer hub.mu.RUnlock()
	if len(hub.subs) != 0 {
		t.Fatalf("expected empty subscription map, got %d entries", len(hub.subs))
	}
}

func TestHub_OnSubscriptionChangeCallback(t *testing.T) {
	hub := NewHub(testLogger())

	var mu sync.Mutex
	var changes []struct {
		key    string
		active bool
	}
	hub.SetOnSubscriptionChange(func(key string, active bool) {
		mu.Lock()
		changes = append(changes, struct {
			key    string
			active bool
		}{key, active})
		mu.Unlock()
	})

	c1 := newTestConnection(hub, "acme", "user1@test.com")
	c2 := newTestConnection(hub, "acme", "user2@test.com")
	hub.Register(c1)
	hub.Register(c2)

	// First subscriber triggers active=true.
	hub.Subscribe(c1, "SalesOrder")
	mu.Lock()
	if len(changes) != 1 || changes[0].key != "acme:SalesOrder" || !changes[0].active {
		t.Fatalf("expected active callback, got %v", changes)
	}
	mu.Unlock()

	// Second subscriber does NOT trigger callback.
	hub.Subscribe(c2, "SalesOrder")
	mu.Lock()
	if len(changes) != 1 {
		t.Fatalf("expected no additional callback, got %d", len(changes))
	}
	mu.Unlock()

	// First unsubscribe — still one subscriber left, no callback.
	hub.Unsubscribe(c1, "SalesOrder")
	mu.Lock()
	if len(changes) != 1 {
		t.Fatalf("expected no callback after partial unsub, got %d", len(changes))
	}
	mu.Unlock()

	// Last unsubscribe triggers active=false.
	hub.Unsubscribe(c2, "SalesOrder")
	mu.Lock()
	if len(changes) != 2 || changes[1].key != "acme:SalesOrder" || changes[1].active {
		t.Fatalf("expected inactive callback, got %v", changes)
	}
	mu.Unlock()

	hub.Unregister(c1)
	hub.Unregister(c2)
}

func TestHub_DifferentSitesIsolated(t *testing.T) {
	hub := NewHub(testLogger())
	c1 := newTestConnection(hub, "acme", "user@acme.com")
	c2 := newTestConnection(hub, "globex", "user@globex.com")
	hub.Register(c1)
	hub.Register(c2)
	defer hub.Unregister(c1)
	defer hub.Unregister(c2)

	hub.Subscribe(c1, "SalesOrder")
	hub.Subscribe(c2, "SalesOrder")

	// Broadcast to acme only.
	hub.Broadcast("acme", "SalesOrder", []byte(`acme-msg`))

	select {
	case msg := <-c1.send:
		if string(msg) != "acme-msg" {
			t.Fatalf("unexpected message: %s", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("acme conn did not receive message")
	}

	select {
	case <-c2.send:
		t.Fatal("globex conn should not receive acme broadcast")
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

func TestHub_ConcurrentOperations(t *testing.T) {
	hub := NewHub(testLogger())

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c := newTestConnection(hub, "acme", "user@test.com")
			hub.Register(c)
			hub.Subscribe(c, "SalesOrder")
			hub.Broadcast("acme", "SalesOrder", []byte(`msg`))
			hub.Unsubscribe(c, "SalesOrder")
			hub.Unregister(c)
		}(i)
	}
	wg.Wait()

	if hub.ConnectionCount() != 0 {
		t.Fatalf("expected 0 connections after concurrent ops, got %d", hub.ConnectionCount())
	}
}

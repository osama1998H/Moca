package serve

import (
	"log/slog"
	"sync"

	"github.com/coder/websocket"
)

const sendBufferSize = 256

// Hub manages WebSocket connections and their doctype subscriptions.
// Connections subscribe to keys of the form "{site}:{doctype}" and receive
// broadcast messages for matching document events.
type Hub struct {
	subs        map[string]map[*Connection]struct{} // key: "{site}:{doctype}"
	conns       map[*Connection]struct{}
	onSubChange func(key string, active bool)
	logger      *slog.Logger
	mu          sync.RWMutex
}

// Connection wraps a single WebSocket client with a dedicated send channel.
// The write goroutine drains the send channel; the read goroutine processes
// incoming subscribe/unsubscribe messages.
type Connection struct {
	conn   *websocket.Conn
	hub    *Hub
	send   chan []byte
	subs   map[string]bool // doctype -> subscribed
	logger *slog.Logger
	site   string // from JWT claims
	email  string // from JWT claims
}

// NewHub creates a new WebSocket hub.
func NewHub(logger *slog.Logger) *Hub {
	return &Hub{
		subs:   make(map[string]map[*Connection]struct{}),
		conns:  make(map[*Connection]struct{}),
		logger: logger,
	}
}

// SetOnSubscriptionChange registers a callback that fires when the first
// connection subscribes to a key (active=true) or the last connection
// unsubscribes from a key (active=false). Called under the hub's write lock.
func (h *Hub) SetOnSubscriptionChange(fn func(key string, active bool)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onSubChange = fn
}

// Register adds a connection to the hub's global connection set.
func (h *Hub) Register(c *Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns[c] = struct{}{}
}

// Unregister removes a connection from the hub and all its subscriptions,
// then closes the send channel. Safe to call multiple times.
func (h *Hub) Unregister(c *Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.conns[c]; !ok {
		return // already unregistered
	}
	delete(h.conns, c)

	for doctype := range c.subs {
		key := c.site + ":" + doctype
		if conns, ok := h.subs[key]; ok {
			delete(conns, c)
			if len(conns) == 0 {
				delete(h.subs, key)
				if h.onSubChange != nil {
					h.onSubChange(key, false)
				}
			}
		}
	}
	c.subs = nil
	close(c.send)
}

// Subscribe registers a connection for broadcasts on the given doctype.
func (h *Hub) Subscribe(c *Connection, doctype string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	key := c.site + ":" + doctype
	if c.subs[doctype] {
		return // already subscribed
	}

	conns, ok := h.subs[key]
	if !ok {
		conns = make(map[*Connection]struct{})
		h.subs[key] = conns
	}
	conns[c] = struct{}{}
	c.subs[doctype] = true

	if len(conns) == 1 && h.onSubChange != nil {
		h.onSubChange(key, true)
	}
}

// Unsubscribe removes a connection from broadcasts on the given doctype.
func (h *Hub) Unsubscribe(c *Connection, doctype string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !c.subs[doctype] {
		return // not subscribed
	}

	key := c.site + ":" + doctype
	if conns, ok := h.subs[key]; ok {
		delete(conns, c)
		if len(conns) == 0 {
			delete(h.subs, key)
			if h.onSubChange != nil {
				h.onSubChange(key, false)
			}
		}
	}
	delete(c.subs, doctype)
}

// Broadcast sends a message to all connections subscribed to the given
// site:doctype key. Uses a non-blocking send: if a connection's send buffer
// is full, the message is dropped for that connection (backpressure).
func (h *Hub) Broadcast(site, doctype string, message []byte) {
	key := site + ":" + doctype

	h.mu.RLock()
	defer h.mu.RUnlock()

	conns, ok := h.subs[key]
	if !ok {
		return
	}
	for c := range conns {
		select {
		case c.send <- message:
		default:
			h.logger.Warn("ws hub: dropping message for slow client",
				slog.String("site", site),
				slog.String("doctype", doctype),
				slog.String("email", c.email),
			)
		}
	}
}

// ConnectionCount returns the number of active connections.
func (h *Hub) ConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns)
}

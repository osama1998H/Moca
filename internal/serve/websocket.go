package serve

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"

	"github.com/osama1998H/moca/pkg/auth"
)

// clientMessage is the JSON envelope sent by WebSocket clients.
type clientMessage struct {
	Type    string `json:"type"`    // "subscribe" | "unsubscribe"
	DocType string `json:"doctype"` // e.g. "SalesOrder"
}

// serverMessage is the JSON envelope sent to WebSocket clients.
type serverMessage struct {
	Type      string `json:"type"`                // "doc_update" | "subscribed" | "unsubscribed" | "error"
	Site      string `json:"site,omitempty"`       // tenant site
	DocType   string `json:"doctype,omitempty"`    // document type
	Name      string `json:"name,omitempty"`       // document name
	EventType string `json:"event_type,omitempty"` // e.g. "doc.updated"
	User      string `json:"user,omitempty"`       // who triggered the event
	Timestamp string `json:"timestamp,omitempty"`  // ISO 8601
	Message   string `json:"message,omitempty"`    // for error type
}

// registerWebSocket registers the real WebSocket endpoint on GET /ws.
// It replaces the previous 501 stub.
func registerWebSocket(mux *http.ServeMux, hub *Hub, jwtCfg auth.JWTConfig, logger *slog.Logger) {
	mux.HandleFunc("GET /ws", handleWebSocket(hub, jwtCfg, logger))
}

// handleWebSocket returns an HTTP handler that upgrades to WebSocket.
// Authentication is via JWT token in the ?token query parameter.
func handleWebSocket(hub *Hub, jwtCfg auth.JWTConfig, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "missing token query parameter",
			})
			return
		}

		claims, err := auth.ValidateAccessToken(jwtCfg, token)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "invalid or expired token",
			})
			return
		}

		wsConn, err := websocket.Accept(w, r, nil)
		if err != nil {
			logger.Warn("ws: accept failed", slog.String("error", err.Error()))
			return
		}

		conn := &Connection{
			conn:   wsConn,
			hub:    hub,
			send:   make(chan []byte, sendBufferSize),
			site:   claims.Site,
			email:  claims.Email,
			subs:   make(map[string]bool),
			logger: logger,
		}

		hub.Register(conn)
		defer hub.Unregister(conn)

		logger.Debug("ws: client connected",
			slog.String("site", claims.Site),
			slog.String("email", claims.Email),
		)

		ctx := r.Context()
		go conn.writePump(ctx)
		conn.readPump(ctx)
	}
}

// readPump processes incoming messages from the WebSocket client.
// It runs in the HTTP handler goroutine and exits on error or context cancel.
func (c *Connection) readPump(ctx context.Context) {
	defer func() { _ = c.conn.CloseNow() }()

	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}

		var msg clientMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			c.sendJSON(serverMessage{Type: "error", Message: "invalid message format"})
			continue
		}

		switch msg.Type {
		case "subscribe":
			if msg.DocType == "" {
				c.sendJSON(serverMessage{Type: "error", Message: "missing doctype"})
				continue
			}
			c.hub.Subscribe(c, msg.DocType)
			c.sendJSON(serverMessage{Type: "subscribed", DocType: msg.DocType})
		case "unsubscribe":
			if msg.DocType == "" {
				c.sendJSON(serverMessage{Type: "error", Message: "missing doctype"})
				continue
			}
			c.hub.Unsubscribe(c, msg.DocType)
			c.sendJSON(serverMessage{Type: "unsubscribed", DocType: msg.DocType})
		default:
			c.sendJSON(serverMessage{Type: "error", Message: "unknown message type: " + msg.Type})
		}
	}
}

// writePump drains the send channel and writes messages to the WebSocket.
// It exits when the send channel is closed or the context is cancelled.
func (c *Connection) writePump(ctx context.Context) {
	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				_ = c.conn.Close(websocket.StatusNormalClosure, "")
				return
			}
			if err := c.conn.Write(ctx, websocket.MessageText, msg); err != nil {
				return
			}
		case <-ctx.Done():
			_ = c.conn.Close(websocket.StatusGoingAway, "server shutting down")
			return
		}
	}
}

// sendJSON marshals a serverMessage and sends it via the connection's send channel.
func (c *Connection) sendJSON(msg serverMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	select {
	case c.send <- data:
	default:
		// Send buffer full — drop the message.
	}
}

package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/osama1998H/moca/pkg/auth"
)

func testJWTConfig() auth.JWTConfig {
	return auth.JWTConfig{
		Secret:         "test-secret-for-ws",
		AccessTokenTTL: 15 * time.Minute,
		Issuer:         "moca",
	}
}

func issueTestToken(t *testing.T, cfg auth.JWTConfig, site, email string) string {
	t.Helper()
	pair, err := auth.IssueTokenPair(cfg, &auth.User{
		Email:    email,
		FullName: "Test User",
		Roles:    []string{"System Manager"},
	}, site)
	if err != nil {
		t.Fatalf("issue test token: %v", err)
	}
	return pair.AccessToken
}

func setupTestServer(t *testing.T) (*httptest.Server, *Hub, auth.JWTConfig) {
	t.Helper()
	hub := NewHub(testLogger())
	cfg := testJWTConfig()
	mux := http.NewServeMux()
	registerWebSocket(mux, hub, cfg, testLogger())
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, hub, cfg
}

func wsURL(ts *httptest.Server, token string) string {
	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	if token != "" {
		u += "?token=" + token
	}
	return u
}

func dialWS(t *testing.T, ctx context.Context, ts *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.Dial(ctx, wsURL(ts, token), nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func wsWrite(t *testing.T, ctx context.Context, conn *websocket.Conn, msg string) {
	t.Helper()
	if err := conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatalf("ws write: %v", err)
	}
}

func wsRead(t *testing.T, ctx context.Context, conn *websocket.Conn) serverMessage {
	t.Helper()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	var msg serverMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal ws message: %v", err)
	}
	return msg
}

func TestWebSocket_NoToken_401(t *testing.T) {
	ts, _, _ := setupTestServer(t)

	resp, err := http.Get(ts.URL + "/ws")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "missing token query parameter" {
		t.Fatalf("unexpected error: %v", body)
	}
}

func TestWebSocket_InvalidToken_401(t *testing.T) {
	ts, _, _ := setupTestServer(t)

	resp, err := http.Get(ts.URL + "/ws?token=invalid-jwt-token")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestWebSocket_ValidToken_Upgrades(t *testing.T) {
	ts, hub, cfg := setupTestServer(t)
	token := issueTestToken(t, cfg, "acme", "user@test.com")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialWS(t, ctx, ts, token)

	// Give the server a moment to register the connection.
	time.Sleep(50 * time.Millisecond)
	if hub.ConnectionCount() != 1 {
		t.Fatalf("expected 1 connection, got %d", hub.ConnectionCount())
	}

	_ = conn.Close(websocket.StatusNormalClosure, "")
	time.Sleep(50 * time.Millisecond)
	if hub.ConnectionCount() != 0 {
		t.Fatalf("expected 0 connections after close, got %d", hub.ConnectionCount())
	}
}

func TestWebSocket_SubscribeAck(t *testing.T) {
	ts, _, cfg := setupTestServer(t)
	token := issueTestToken(t, cfg, "acme", "user@test.com")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialWS(t, ctx, ts, token)
	wsWrite(t, ctx, conn, `{"type":"subscribe","doctype":"SalesOrder"}`)

	ack := wsRead(t, ctx, conn)
	if ack.Type != "subscribed" || ack.DocType != "SalesOrder" {
		t.Fatalf("unexpected ack: %+v", ack)
	}
}

func TestWebSocket_UnsubscribeAck(t *testing.T) {
	ts, _, cfg := setupTestServer(t)
	token := issueTestToken(t, cfg, "acme", "user@test.com")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialWS(t, ctx, ts, token)

	wsWrite(t, ctx, conn, `{"type":"subscribe","doctype":"SalesOrder"}`)
	_ = wsRead(t, ctx, conn) // consume subscribe ack

	wsWrite(t, ctx, conn, `{"type":"unsubscribe","doctype":"SalesOrder"}`)
	ack := wsRead(t, ctx, conn)
	if ack.Type != "unsubscribed" || ack.DocType != "SalesOrder" {
		t.Fatalf("unexpected ack: %+v", ack)
	}
}

func TestWebSocket_ReceivesBroadcast(t *testing.T) {
	ts, hub, cfg := setupTestServer(t)
	token := issueTestToken(t, cfg, "acme", "user@test.com")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialWS(t, ctx, ts, token)

	wsWrite(t, ctx, conn, `{"type":"subscribe","doctype":"SalesOrder"}`)
	_ = wsRead(t, ctx, conn) // consume subscribe ack

	broadcastMsg := serverMessage{
		Type:    "doc_update",
		DocType: "SalesOrder",
		Name:    "SO-001",
		User:    "admin@example.com",
	}
	data, _ := json.Marshal(broadcastMsg)
	hub.Broadcast("acme", "SalesOrder", data)

	got := wsRead(t, ctx, conn)
	if got.Type != "doc_update" || got.Name != "SO-001" {
		t.Fatalf("unexpected broadcast message: %+v", got)
	}
}

func TestWebSocket_InvalidMessage_ReturnsError(t *testing.T) {
	ts, _, cfg := setupTestServer(t)
	token := issueTestToken(t, cfg, "acme", "user@test.com")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialWS(t, ctx, ts, token)
	wsWrite(t, ctx, conn, "not json")

	msg := wsRead(t, ctx, conn)
	if msg.Type != "error" || msg.Message != "invalid message format" {
		t.Fatalf("unexpected error response: %+v", msg)
	}
}

func TestWebSocket_MissingDoctype_ReturnsError(t *testing.T) {
	ts, _, cfg := setupTestServer(t)
	token := issueTestToken(t, cfg, "acme", "user@test.com")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialWS(t, ctx, ts, token)
	wsWrite(t, ctx, conn, `{"type":"subscribe"}`)

	msg := wsRead(t, ctx, conn)
	if msg.Type != "error" || msg.Message != "missing doctype" {
		t.Fatalf("unexpected error response: %+v", msg)
	}
}

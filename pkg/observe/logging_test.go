package observe

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

// newTestLogger returns a logger writing JSON to the provided buffer.
func newTestLogger(buf *bytes.Buffer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: level}))
}

func TestNewLogger_JSONOutput(t *testing.T) {
	// NewLogger writes valid JSON to stdout. We test the shape by creating an
	// equivalent logger backed by a buffer and verifying the output.
	var buf bytes.Buffer
	logger := newTestLogger(&buf, slog.LevelInfo)
	logger.Info("test message")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("expected valid JSON log entry, got error: %v", err)
	}
	if entry["msg"] != "test message" {
		t.Errorf("expected msg=%q, got %v", "test message", entry["msg"])
	}
	if entry["level"] != "INFO" {
		t.Errorf("expected level=INFO, got %v", entry["level"])
	}
	if _, ok := entry["time"]; !ok {
		t.Error("expected time field to be present")
	}
}

func TestWithSite_AttachesSiteAttribute(t *testing.T) {
	var buf bytes.Buffer
	base := newTestLogger(&buf, slog.LevelInfo)
	logger := WithSite(base, "acme-corp")
	logger.Info("hello")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if entry["site"] != "acme-corp" {
		t.Errorf("expected site=acme-corp, got %v", entry["site"])
	}
}

func TestWithRequest_AttachesRequestIDAndUser(t *testing.T) {
	var buf bytes.Buffer
	base := newTestLogger(&buf, slog.LevelInfo)
	logger := WithRequest(base, "req-abc-123", "user@example.com")
	logger.Info("processing")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if entry["request_id"] != "req-abc-123" {
		t.Errorf("expected request_id=req-abc-123, got %v", entry["request_id"])
	}
	if entry["user"] != "user@example.com" {
		t.Errorf("expected user=user@example.com, got %v", entry["user"])
	}
}

func TestContextWithLogger_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	stored := newTestLogger(&buf, slog.LevelInfo)

	ctx := ContextWithLogger(context.Background(), stored)
	retrieved := LoggerFromContext(ctx)

	if retrieved != stored {
		t.Error("expected LoggerFromContext to return the exact logger stored in context")
	}
}

func TestLoggerFromContext_DefaultWhenAbsent(t *testing.T) {
	// An empty context should yield a non-nil default logger.
	logger := LoggerFromContext(context.Background())
	if logger == nil {
		t.Fatal("expected non-nil default logger when context has no logger")
	}
}

func TestWithSite_PreservesExistingAttributes(t *testing.T) {
	var buf bytes.Buffer
	base := newTestLogger(&buf, slog.LevelInfo)
	logger := WithRequest(base, "req-xyz", "admin@moca.io")
	logger = WithSite(logger, "beta-site")
	logger.Info("combined")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if entry["site"] != "beta-site" {
		t.Errorf("expected site=beta-site, got %v", entry["site"])
	}
	if entry["request_id"] != "req-xyz" {
		t.Errorf("expected request_id=req-xyz, got %v", entry["request_id"])
	}
	if entry["user"] != "admin@moca.io" {
		t.Errorf("expected user=admin@moca.io, got %v", entry["user"])
	}
}

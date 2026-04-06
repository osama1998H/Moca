package serve

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterStaticFiles_ServesFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hello</h1>"), 0644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registerStaticFiles(mux, dir, logger)

	// Use a full httptest.Server to follow redirects from the file server.
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/desk/index.html")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "<h1>hello</h1>" {
		t.Fatalf("unexpected body: %q", string(body))
	}
}

func TestRegisterStaticFiles_NoDir(t *testing.T) {
	mux := http.NewServeMux()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Empty dir — should not panic or register anything.
	registerStaticFiles(mux, "", logger)

	// Non-existent dir — should not panic.
	registerStaticFiles(mux, "/tmp/does-not-exist-moca-test", logger)
}

// TestRegisterWebSocketStub was removed — the stub has been replaced by the
// real WebSocket handler. See websocket_test.go for the replacement tests.


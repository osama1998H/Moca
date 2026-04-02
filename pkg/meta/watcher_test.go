package meta_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/osama1998H/moca/pkg/meta"
)

// validDocJSON is a minimal valid MetaType JSON for testing.
const validDocJSON = `{
  "name": "TestHotReload",
  "module": "Core",
  "fields": [
    {"name": "title", "field_type": "Data", "label": "Title"}
  ]
}`

// --- Mock helpers ---

type mockRegisterer struct {
	calls []registerCall
	mu    sync.Mutex
}

type registerCall struct {
	Site string
	JSON []byte
}

func (m *mockRegisterer) Register(_ context.Context, site string, jsonBytes []byte) (*meta.MetaType, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, registerCall{Site: site, JSON: jsonBytes})
	// Compile to return a valid MetaType.
	mt, err := meta.Compile(jsonBytes)
	if err != nil {
		return nil, err
	}
	return mt, nil
}

func (m *mockRegisterer) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

type mockSiteLister struct {
	Sites []string
}

func (m *mockSiteLister) ListActiveSites(_ context.Context) ([]string, error) {
	return m.Sites, nil
}

// setupWatchedDir creates a temp directory tree matching
// apps/{app}/modules/{module}/doctypes/{doctype}/ and returns the apps dir
// and the doctype directory path.
func setupWatchedDir(t *testing.T) (appsDir, doctypeDir string) {
	t.Helper()
	appsDir = t.TempDir()
	doctypeDir = filepath.Join(appsDir, "testapp", "modules", "core", "doctypes", "test_hot_reload")
	if err := os.MkdirAll(doctypeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return appsDir, doctypeDir
}

// Reuses nullLogger() and nullWriter{} from registry_test.go.

// --- Tests ---

func TestWatcher_DetectsJSONChanges(t *testing.T) {
	appsDir, doctypeDir := setupWatchedDir(t)
	reg := &mockRegisterer{}
	sites := &mockSiteLister{Sites: []string{"site1", "site2"}}

	w := meta.NewWatcher(reg, sites, nullLogger(), meta.WatcherConfig{
		AppsDir:  appsDir,
		Debounce: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	// Give fsnotify time to start watching.
	time.Sleep(100 * time.Millisecond)

	// Write a valid JSON file into the watched directory.
	target := filepath.Join(doctypeDir, "test_hot_reload.json")
	if err := os.WriteFile(target, []byte(validDocJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	// Wait for debounce + reload.
	time.Sleep(300 * time.Millisecond)

	count := reg.callCount()
	if count != 2 {
		t.Errorf("expected Register called for 2 sites, got %d", count)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run returned error: %v", err)
	}
}

func TestWatcher_DebounceCoalesces(t *testing.T) {
	appsDir, doctypeDir := setupWatchedDir(t)
	reg := &mockRegisterer{}
	sites := &mockSiteLister{Sites: []string{"site1"}}

	w := meta.NewWatcher(reg, sites, nullLogger(), meta.WatcherConfig{
		AppsDir:  appsDir,
		Debounce: 100 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)

	target := filepath.Join(doctypeDir, "test_hot_reload.json")

	// Write the same file 5 times rapidly.
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(target, []byte(validDocJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for debounce to fire (100ms after last write + some buffer).
	time.Sleep(300 * time.Millisecond)

	count := reg.callCount()
	if count != 1 {
		t.Errorf("expected 1 coalesced Register call, got %d", count)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run returned error: %v", err)
	}
}

func TestWatcher_IgnoresNonJSON(t *testing.T) {
	appsDir, doctypeDir := setupWatchedDir(t)
	reg := &mockRegisterer{}
	sites := &mockSiteLister{Sites: []string{"site1"}}

	w := meta.NewWatcher(reg, sites, nullLogger(), meta.WatcherConfig{
		AppsDir:  appsDir,
		Debounce: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)

	// Write a .txt file — should be ignored.
	if err := os.WriteFile(filepath.Join(doctypeDir, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)

	if count := reg.callCount(); count != 0 {
		t.Errorf("expected 0 Register calls for non-JSON file, got %d", count)
	}

	cancel()
	<-done
}

func TestWatcher_IgnoresDotfiles(t *testing.T) {
	appsDir, doctypeDir := setupWatchedDir(t)
	reg := &mockRegisterer{}
	sites := &mockSiteLister{Sites: []string{"site1"}}

	w := meta.NewWatcher(reg, sites, nullLogger(), meta.WatcherConfig{
		AppsDir:  appsDir,
		Debounce: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)

	// Write a dotfile (vim swap) — should be ignored.
	if err := os.WriteFile(filepath.Join(doctypeDir, ".test.json.swp"), []byte("swap"), 0o644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)

	if count := reg.callCount(); count != 0 {
		t.Errorf("expected 0 Register calls for dotfile, got %d", count)
	}

	cancel()
	<-done
}

func TestWatcher_InvalidJSONLogsError(t *testing.T) {
	appsDir, doctypeDir := setupWatchedDir(t)
	reg := &mockRegisterer{}
	sites := &mockSiteLister{Sites: []string{"site1"}}

	var loggedError atomic.Bool
	handler := &captureHandler{
		Handler: slog.NewTextHandler(nullWriter{}, &slog.HandlerOptions{Level: slog.LevelDebug}),
		onError: func() { loggedError.Store(true) },
	}
	logger := slog.New(handler)

	w := meta.NewWatcher(reg, sites, logger, meta.WatcherConfig{
		AppsDir:  appsDir,
		Debounce: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)

	// Write invalid JSON.
	target := filepath.Join(doctypeDir, "bad.json")
	if err := os.WriteFile(target, []byte(`{not valid json`), 0o644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(300 * time.Millisecond)

	// Register should NOT have been called.
	if count := reg.callCount(); count != 0 {
		t.Errorf("expected 0 Register calls for invalid JSON, got %d", count)
	}

	// Error should have been logged.
	if !loggedError.Load() {
		t.Error("expected compile error to be logged")
	}

	// Server (Run) should still be alive — cancel cleanly.
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run returned error: %v", err)
	}
}

func TestWatcher_RunBlocksUntilCancelled(t *testing.T) {
	appsDir, _ := setupWatchedDir(t)
	reg := &mockRegisterer{}
	sites := &mockSiteLister{}

	w := meta.NewWatcher(reg, sites, nullLogger(), meta.WatcherConfig{
		AppsDir:  appsDir,
		Debounce: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	// Verify it's still running after 100ms.
	select {
	case <-done:
		t.Fatal("Run returned before context was cancelled")
	case <-time.After(100 * time.Millisecond):
		// expected
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestWatcher_VimStyleRename(t *testing.T) {
	appsDir, doctypeDir := setupWatchedDir(t)
	reg := &mockRegisterer{}
	sites := &mockSiteLister{Sites: []string{"site1"}}

	w := meta.NewWatcher(reg, sites, nullLogger(), meta.WatcherConfig{
		AppsDir:  appsDir,
		Debounce: 100 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)

	// Simulate vim save: write to temp file, then rename to target.
	target := filepath.Join(doctypeDir, "test_hot_reload.json")
	tmp := filepath.Join(doctypeDir, "test_hot_reload.json~")

	if err := os.WriteFile(tmp, []byte(validDocJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, target); err != nil {
		t.Fatal(err)
	}

	// Wait for debounce + reload.
	time.Sleep(400 * time.Millisecond)

	count := reg.callCount()
	if count < 1 {
		t.Errorf("expected at least 1 Register call after vim-style rename, got %d", count)
	}

	cancel()
	<-done
}

// captureHandler wraps a slog.Handler and calls onError when an Error-level
// record is handled.
type captureHandler struct {
	slog.Handler
	onError func()
}

func (h *captureHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		h.onError()
	}
	return h.Handler.Handle(ctx, r)
}

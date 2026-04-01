package meta

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/moca-framework/moca/pkg/orm"
)

// Registerer is the interface for registering MetaType definitions.
// *Registry satisfies this interface.
type Registerer interface {
	Register(ctx context.Context, site string, jsonBytes []byte) (*MetaType, error)
}

// SiteLister provides the list of active site names for hot reload.
// Defined as an interface to decouple the Watcher from direct database
// access and enable unit testing with mocks.
type SiteLister interface {
	ListActiveSites(ctx context.Context) ([]string, error)
}

// DBSiteLister implements SiteLister by querying the system database.
type DBSiteLister struct {
	DB *orm.DBManager
}

// ListActiveSites returns the names of all active sites.
func (d *DBSiteLister) ListActiveSites(ctx context.Context) ([]string, error) {
	rows, err := d.DB.SystemPool().Query(ctx, "SELECT name FROM sites WHERE status = 'active'")
	if err != nil {
		return nil, fmt.Errorf("list active sites: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("list active sites: scan: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// WatcherConfig configures the filesystem watcher for hot-reloading MetaType definitions.
type WatcherConfig struct {
	AppsDir  string        // root directory containing app directories (e.g., "./apps")
	Debounce time.Duration // delay after last event before triggering reload; default 500ms
}

// Watcher monitors doctype JSON files on disk and hot-reloads them into the
// Registry for all tenant sites when changes are detected. Its Run method
// matches the process.Subsystem signature for use with the Supervisor.
type Watcher struct {
	registry Registerer
	sites    SiteLister
	logger   *slog.Logger
	timers   map[string]*time.Timer
	cfg      WatcherConfig
	mu       sync.Mutex
}

// NewWatcher creates a Watcher that watches doctype JSON files under cfg.AppsDir.
func NewWatcher(registry Registerer, sites SiteLister, logger *slog.Logger, cfg WatcherConfig) *Watcher {
	if cfg.Debounce <= 0 {
		cfg.Debounce = 500 * time.Millisecond
	}
	return &Watcher{
		registry: registry,
		sites:    sites,
		logger:   logger,
		cfg:      cfg,
		timers:   make(map[string]*time.Timer),
	}
}

// Run starts the filesystem watcher and blocks until ctx is cancelled.
// It discovers all apps/*/modules/*/doctypes/* directories under AppsDir,
// watches them for JSON file changes, and triggers hot reload via Registry.Register.
func (w *Watcher) Run(ctx context.Context) error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("meta watcher: create fsnotify: %w", err)
	}
	defer func() { _ = fsw.Close() }()

	dirs, err := discoverDoctypeDirs(w.cfg.AppsDir)
	if err != nil {
		return fmt.Errorf("meta watcher: discover dirs: %w", err)
	}
	if len(dirs) == 0 {
		w.logger.Warn("meta watcher: no doctype directories found", "apps_dir", w.cfg.AppsDir)
	}

	for _, dir := range dirs {
		if err := fsw.Add(dir); err != nil {
			w.logger.Error("meta watcher: failed to watch directory", "dir", dir, "error", err)
			continue
		}
		w.logger.Info("meta watcher: watching", "dir", dir)
	}

	for {
		select {
		case event, ok := <-fsw.Events:
			if !ok {
				return nil
			}
			if !event.Has(fsnotify.Create) && !event.Has(fsnotify.Write) && !event.Has(fsnotify.Rename) {
				continue
			}
			base := filepath.Base(event.Name)
			if strings.HasPrefix(base, ".") {
				continue
			}
			if filepath.Ext(event.Name) != ".json" {
				continue
			}
			w.debounce(ctx, event.Name)

		case err, ok := <-fsw.Errors:
			if !ok {
				return nil
			}
			w.logger.Error("meta watcher: fsnotify error", "error", err)

		case <-ctx.Done():
			w.stopAllTimers()
			return nil
		}
	}
}

// debounce resets or creates a timer for the given file path. When the timer
// fires after the configured debounce duration, reload is called.
func (w *Watcher) debounce(ctx context.Context, filePath string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if t, ok := w.timers[filePath]; ok {
		t.Reset(w.cfg.Debounce)
		return
	}
	w.timers[filePath] = time.AfterFunc(w.cfg.Debounce, func() {
		w.reload(ctx, filePath)
	})
}

// reload reads the JSON file, validates it, and registers it with all active sites.
func (w *Watcher) reload(ctx context.Context, filePath string) {
	w.mu.Lock()
	delete(w.timers, filePath)
	w.mu.Unlock()

	if ctx.Err() != nil {
		return
	}

	start := time.Now()

	jsonBytes, err := os.ReadFile(filePath)
	if err != nil {
		w.logger.Warn("meta watcher: read file failed", "path", filePath, "error", err)
		return
	}

	if _, compileErr := Compile(jsonBytes); compileErr != nil {
		w.logger.Error("meta watcher: compile failed", "path", filePath, "error", compileErr)
		return
	}

	sites, err := w.sites.ListActiveSites(ctx)
	if err != nil {
		w.logger.Error("meta watcher: list sites failed", "error", err)
		return
	}
	if len(sites) == 0 {
		w.logger.Warn("meta watcher: no active sites, skipping reload", "path", filePath)
		return
	}

	var registered int
	for _, site := range sites {
		if _, err := w.registry.Register(ctx, site, jsonBytes); err != nil {
			w.logger.Error("meta watcher: register failed",
				"path", filePath, "site", site, "error", err)
			continue
		}
		registered++
	}

	doctype := strings.TrimSuffix(filepath.Base(filePath), ".json")
	w.logger.Info("meta watcher: hot reload complete",
		"doctype", doctype,
		"sites", registered,
		"duration", time.Since(start).Round(time.Millisecond),
	)
}

// stopAllTimers cancels all pending debounce timers.
func (w *Watcher) stopAllTimers() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for path, t := range w.timers {
		t.Stop()
		delete(w.timers, path)
	}
}

// discoverDoctypeDirs finds all doctype directories under appsDir matching
// the pattern: {appsDir}/*/modules/*/doctypes/*
func discoverDoctypeDirs(appsDir string) ([]string, error) {
	pattern := filepath.Join(appsDir, "*", "modules", "*", "doctypes", "*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob %s: %w", pattern, err)
	}

	var dirs []string
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		if info.IsDir() {
			dirs = append(dirs, m)
		}
	}
	return dirs, nil
}

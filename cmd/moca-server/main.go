package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/internal/serve"
	"github.com/osama1998H/moca/pkg/observe"

	// Blank-import builtin core so its init() registers controllers/hooks.
	_ "github.com/osama1998H/moca/pkg/builtin/core"
)

// Build-time variables injected via -ldflags.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// ── Load configuration ──────────────────────────────────────────────
	const configFile = "moca.yaml"
	if _, err := os.Stat(configFile); errors.Is(err, os.ErrNotExist) {
		fmt.Printf("No %s found in current directory — nothing to serve.\n", configFile)
		return nil
	}

	cfg, err := config.LoadAndResolve(configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := observe.NewLogger(slog.LevelInfo)

	// ── Create server ───────────────────────────────────────────────────
	srv, err := serve.NewServer(ctx, serve.ServerConfig{
		Config:  cfg,
		Logger:  logger,
		Port:    cfg.Development.Port,
		Version: Version,
	})
	if err != nil {
		return err
	}
	defer srv.Close()

	// ── Startup banner ──────────────────────────────────────────────────
	fmt.Println("MOCA Framework Server")
	fmt.Println("=====================")
	fmt.Printf("Version:    %s\n", Version)
	fmt.Printf("Commit:     %s\n", Commit)
	fmt.Printf("Built:      %s\n", BuildDate)
	fmt.Printf("Go version: %s\n", runtime.Version())
	fmt.Printf("OS/Arch:    %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Project:    %s %s\n", cfg.Project.Name, cfg.Project.Version)
	fmt.Printf("Listen:     http://%s\n", srv.Addr())
	fmt.Println()

	// ── Run (blocks until shutdown) ─────────────────────────────────────
	return srv.Run(ctx)
}

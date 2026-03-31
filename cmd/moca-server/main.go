package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/moca-framework/moca/internal/config"
	"github.com/moca-framework/moca/internal/drivers"
	"github.com/moca-framework/moca/pkg/api"
	"github.com/moca-framework/moca/pkg/document"
	"github.com/moca-framework/moca/pkg/meta"
	"github.com/moca-framework/moca/pkg/observe"
	"github.com/moca-framework/moca/pkg/orm"
)

// Build-time variables injected via -ldflags.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// shutdownTimeout is the maximum time to wait for in-flight requests to finish.
const shutdownTimeout = 30 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
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

	// ── Initialize infrastructure ───────────────────────────────────────
	dbManager, err := orm.NewDBManager(ctx, cfg.Infrastructure.Database, logger)
	if err != nil {
		return fmt.Errorf("init database: %w", err)
	}
	defer dbManager.Close()

	redisClients := drivers.NewRedisClients(cfg.Infrastructure.Redis, logger)
	defer func() { _ = redisClients.Close() }()

	if err := redisClients.Ping(ctx); err != nil {
		logger.Warn("Redis not reachable at startup — rate limiting and caching will be degraded",
			slog.String("error", err.Error()),
		)
	}

	// ── Initialize application layer ────────────────────────────────────
	registry := meta.NewRegistry(dbManager, redisClients.Cache, logger)

	naming := document.NewNamingEngine()
	validator := document.NewValidator()
	controllers := document.NewControllerRegistry()
	docManager := document.NewDocManager(registry, dbManager, naming, validator, controllers, logger)

	// ── Build API gateway ───────────────────────────────────────────────
	rateLimiter := api.NewRateLimiter(redisClients.Cache, logger)
	siteResolver := api.NewDBSiteResolver(dbManager)

	gw := api.NewGateway(
		api.WithDocManager(docManager),
		api.WithRegistry(registry),
		api.WithRedis(redisClients),
		api.WithLogger(logger),
		api.WithSiteResolver(siteResolver),
		api.WithRateLimiter(rateLimiter, nil), // no default rate limit; per-doctype limits apply
	)

	handler := api.NewResourceHandler(gw)
	handler.RegisterRoutes(gw.Mux(), "v1")

	vr := api.NewVersionRouter(handler, logger)
	gw.SetVersionRouter(vr)

	// ── Health check endpoints ──────────────────────────────────────────
	hc := observe.NewHealthChecker(dbManager.SystemPool(), redisClients, Version, logger)
	hc.RegisterRoutes(gw.Mux())

	// ── Start HTTP server ───────────────────────────────────────────────
	port := cfg.Development.Port
	if port == 0 {
		port = 8000
	}
	addr := fmt.Sprintf(":%d", port)

	srv := &http.Server{
		Addr:    addr,
		Handler: gw.Handler(),
	}

	// Print startup banner.
	fmt.Println("MOCA Framework Server")
	fmt.Println("=====================")
	fmt.Printf("Version:    %s\n", Version)
	fmt.Printf("Commit:     %s\n", Commit)
	fmt.Printf("Built:      %s\n", BuildDate)
	fmt.Printf("Go version: %s\n", runtime.Version())
	fmt.Printf("OS/Arch:    %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Project:    %s %s\n", cfg.Project.Name, cfg.Project.Version)
	fmt.Printf("Listen:     http://0.0.0.0%s\n", addr)
	fmt.Println()

	// Start listening in a separate goroutine.
	listenErr := make(chan error, 1)
	go func() {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			listenErr <- fmt.Errorf("listen %s: %w", addr, err)
			return
		}
		logger.Info("server started", slog.String("addr", addr))
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			listenErr <- err
		}
	}()

	// Wait for shutdown signal or fatal listen error.
	select {
	case err := <-listenErr:
		return err
	case <-ctx.Done():
		stop() // release signal handler
		logger.Info("shutting down gracefully", slog.Duration("timeout", shutdownTimeout))
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}
	logger.Info("server stopped")
	return nil
}

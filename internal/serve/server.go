package serve

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/internal/drivers"
	"github.com/osama1998H/moca/pkg/api"
	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/hooks"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/observe"
	"github.com/osama1998H/moca/pkg/orm"
)

// shutdownTimeout is the maximum time to wait for in-flight requests to finish.
const shutdownTimeout = 30 * time.Second

// ServerConfig holds the parameters needed to construct a Server.
type ServerConfig struct {
	Config    *config.ProjectConfig
	Logger    *slog.Logger
	Host      string
	Version   string
	StaticDir string
	Port      int
}

// Server owns all components of the Moca HTTP server. Its Run method matches
// the process.Subsystem.Run signature so it can be used as a supervisor subsystem.
type Server struct {
	httpServer   *http.Server
	gateway      *api.Gateway
	registry     *meta.Registry
	dbManager    *orm.DBManager
	redisClients *drivers.RedisClients
	docManager   *document.DocManager
	hookRegistry *hooks.HookRegistry
	logger       *slog.Logger
}

// NewServer constructs a fully-wired Moca HTTP server from the given config.
// It initialises infrastructure (DB, Redis), application layer (registry, doc
// manager, hooks), and the API gateway with the full middleware chain.
func NewServer(ctx context.Context, cfg ServerConfig) (*Server, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// ── Infrastructure ──────────────────────────────────────────────────
	dbManager, err := orm.NewDBManager(ctx, cfg.Config.Infrastructure.Database, logger)
	if err != nil {
		return nil, fmt.Errorf("init database: %w", err)
	}

	redisClients := drivers.NewRedisClients(cfg.Config.Infrastructure.Redis, logger)
	if err := redisClients.Ping(ctx); err != nil {
		logger.Warn("Redis not reachable at startup — rate limiting and caching will be degraded",
			slog.String("error", err.Error()),
		)
	}

	// ── Application layer ───────────────────────────────────────────────
	registry := meta.NewRegistry(dbManager, redisClients.Cache, logger)

	naming := document.NewNamingEngine()
	validator := document.NewValidator()
	controllers := document.NewControllerRegistry()
	docManager := document.NewDocManager(registry, dbManager, naming, validator, controllers, logger)

	hookRegistry := hooks.NewHookRegistry()
	docManager.SetHookDispatcher(hooks.NewDocEventDispatcher(hookRegistry))

	// ── Authentication ──────────────────────────────────────────────────
	jwtCfg := auth.DefaultJWTConfig()
	sessionMgr := auth.NewSessionManager(redisClients.Session, 24*time.Hour)
	userLoader := auth.NewUserLoader(logger)
	authenticator := auth.NewMocaAuthenticator(jwtCfg, sessionMgr, userLoader, api.SiteFromContext, logger)

	// ── Permissions ─────────────────────────────────────────────────────
	permResolver := auth.NewCachedPermissionResolver(registry, redisClients.Cache, nil, logger)
	permChecker := auth.NewRoleBasedPermChecker(permResolver, api.SiteFromContext, logger)
	fieldLevelTransformer := api.NewFieldLevelTransformer(permResolver)

	// ── API gateway ─────────────────────────────────────────────────────
	rateLimiter := api.NewRateLimiter(redisClients.Cache, logger)
	siteResolver := api.NewDBSiteResolver(dbManager, redisClients.Cache, logger)

	gw := api.NewGateway(
		api.WithDocManager(docManager),
		api.WithRegistry(registry),
		api.WithRedis(redisClients),
		api.WithLogger(logger),
		api.WithSiteResolver(siteResolver),
		api.WithRateLimiter(rateLimiter, nil),
		api.WithAuthenticator(authenticator),
		api.WithPermissionChecker(permChecker),
		api.WithFieldLevelTransformer(fieldLevelTransformer),
	)

	handler := api.NewResourceHandler(gw)
	handler.RegisterRoutes(gw.Mux(), "v1")

	authHandler := api.NewAuthHandler(jwtCfg, sessionMgr, userLoader, logger)
	authHandler.RegisterRoutes(gw.Mux(), "v1")

	vr := api.NewVersionRouter(handler, logger)
	gw.SetVersionRouter(vr)

	// ── Health checks ───────────────────────────────────────────────────
	version := cfg.Version
	if version == "" {
		version = "dev"
	}
	hc := observe.NewHealthChecker(dbManager.SystemPool(), redisClients, version, logger)
	hc.RegisterRoutes(gw.Mux())

	// ── Static files & WebSocket stub ───────────────────────────────────
	registerStaticFiles(gw.Mux(), cfg.StaticDir, logger)
	registerWebSocketStub(gw.Mux())

	// ── HTTP server ─────────────────────────────────────────────────────
	host := cfg.Host
	if host == "" {
		host = "0.0.0.0"
	}
	port := cfg.Port
	if port == 0 {
		port = 8000
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	srv := &http.Server{
		Addr:    addr,
		Handler: gw.Handler(),
	}

	return &Server{
		httpServer:   srv,
		gateway:      gw,
		registry:     registry,
		dbManager:    dbManager,
		redisClients: redisClients,
		docManager:   docManager,
		hookRegistry: hookRegistry,
		logger:       logger,
	}, nil
}

// Run starts the HTTP listener and blocks until ctx is cancelled, then
// performs graceful shutdown. Its signature matches process.Subsystem.Run.
func (s *Server) Run(ctx context.Context) error {
	listenErr := make(chan error, 1)
	go func() {
		ln, err := net.Listen("tcp", s.httpServer.Addr)
		if err != nil {
			listenErr <- fmt.Errorf("listen %s: %w", s.httpServer.Addr, err)
			return
		}
		s.logger.Info("server started", slog.String("addr", s.httpServer.Addr))
		if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			listenErr <- err
		}
	}()

	// Start background pool eviction to reclaim connections from idle tenants.
	evictTicker := time.NewTicker(5 * time.Minute)
	defer evictTicker.Stop()
	go func() {
		for {
			select {
			case <-evictTicker.C:
				n := s.dbManager.EvictIdlePools(30 * time.Minute)
				if n > 0 {
					s.logger.Info("evicted idle tenant pools", slog.Int("count", n))
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	select {
	case err := <-listenErr:
		return err
	case <-ctx.Done():
		s.logger.Info("shutting down gracefully", slog.Duration("timeout", shutdownTimeout))
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}
	s.logger.Info("server stopped")
	return nil
}

// Addr returns the configured listen address (host:port).
func (s *Server) Addr() string {
	return s.httpServer.Addr
}

// Registry returns the metadata registry for use by the file watcher.
func (s *Server) Registry() *meta.Registry {
	return s.registry
}

// DBManager returns the database manager for use by the file watcher.
func (s *Server) DBManager() *orm.DBManager {
	return s.dbManager
}

// Close releases database and Redis connections.
func (s *Server) Close() {
	if s.redisClients != nil {
		_ = s.redisClients.Close()
	}
	if s.dbManager != nil {
		s.dbManager.Close()
	}
}

package serve

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/internal/drivers"
	"github.com/osama1998H/moca/pkg/api"
	"github.com/osama1998H/moca/pkg/apps"
	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/encryption"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/hooks"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/i18n"
	"github.com/osama1998H/moca/pkg/notify"
	"github.com/osama1998H/moca/pkg/observe"
	"github.com/osama1998H/moca/pkg/orm"
	"github.com/osama1998H/moca/pkg/queue"
	"github.com/osama1998H/moca/pkg/search"
	"github.com/osama1998H/moca/pkg/storage"
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
	fileStorage  storage.Storage
	searchClient *search.Client
	hub          *Hub
	config       *config.ProjectConfig
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
	if pingErr := redisClients.Ping(ctx); pingErr != nil {
		logger.Warn("Redis not reachable at startup — rate limiting and caching will be degraded",
			slog.String("error", pingErr.Error()),
		)
	}

	// ── File storage ────────────────────────────────────────────────────
	stor, err := storage.NewStorage(cfg.Config.Infrastructure.Storage)
	if err != nil {
		return nil, fmt.Errorf("init storage: %w", err)
	}
	if s3stor, ok := stor.(*storage.S3Storage); ok {
		if bucketErr := s3stor.EnsureBucket(ctx); bucketErr != nil {
			logger.Warn("S3 bucket ensure failed — file uploads may not work",
				slog.String("error", bucketErr.Error()),
			)
		}
	}

	// ── Application layer ───────────────────────────────────────────────
	registry := meta.NewRegistry(dbManager, redisClients.Cache, logger)

	naming := document.NewNamingEngine()
	validator := document.NewValidator()
	controllers := document.NewControllerRegistry()
	hookRegistry := hooks.NewHookRegistry()

	// Initialize all apps that registered via init() (blank imports).
	if err = apps.InitializeAll(controllers, hookRegistry); err != nil {
		return nil, fmt.Errorf("init apps: %w", err)
	}

	docManager := document.NewDocManager(registry, dbManager, naming, validator, controllers, logger)
	docManager.SetHookDispatcher(hooks.NewDocEventDispatcher(hookRegistry))

	// Webhook dispatch engine — enqueues delivery jobs on document events.
	queueProducer := queue.NewProducer(redisClients.Queue, logger)
	webhookDispatcher := api.NewWebhookDispatcher(queueProducer, dbManager, logger)
	webhookDispatcher.RegisterHooks(hookRegistry)

	// ── Notifications ───────────────────────────────────────────────────
	inAppNotifier := notify.NewInAppNotifier(logger)
	templateRenderer, err := notify.NewTemplateRenderer()
	if err != nil {
		return nil, fmt.Errorf("init notification templates: %w", err)
	}
	var emailSender notify.EmailSender
	if cfg.Config != nil {
		emailSender, err = notify.NewEmailSender(cfg.Config.Notification.Email)
		if err != nil {
			return nil, fmt.Errorf("init email sender: %w", err)
		}
	}
	if emailSender != nil {
		logger.Info("email sender configured",
			slog.String("provider", cfg.Config.Notification.Email.Provider))
	} else {
		logger.Info("email sender not configured — in-app notifications only")
	}
	notifDispatcher := notify.NewNotificationDispatcher(
		queueProducer, dbManager, inAppNotifier, templateRenderer,
		redisClients.PubSub, emailSender, logger,
	)
	notifDispatcher.RegisterHooks(hookRegistry)

	// ── Authentication ──────────────────────────────────────────────────
	jwtCfg := auth.DefaultJWTConfig()
	sessionMgr := auth.NewSessionManager(redisClients.Session, 24*time.Hour)
	userLoader := auth.NewUserLoader(logger)
	authenticator := auth.NewMocaAuthenticator(jwtCfg, sessionMgr, userLoader, api.SiteFromContext, logger)

	// ── API Key Authentication ───────────────────────────────────────────
	apiKeyStore := api.NewAPIKeyStore(userLoader.LoadByEmail, redisClients.Cache, logger)

	// ── Permissions ─────────────────────────────────────────────────────
	permResolver := auth.NewCachedPermissionResolver(registry, redisClients.Cache, nil, logger)
	permChecker := auth.NewRoleBasedPermChecker(permResolver, api.SiteFromContext, logger)
	scopeEnforcer := api.NewScopeEnforcer(permChecker)
	fieldLevelTransformer := api.NewFieldLevelTransformer(permResolver)

	// ── Field Encryption ────────────────────────────────────────────────
	// fieldEncryptor is declared at outer scope so SSO handler can reuse it
	// for decrypting client_secret and sp_private_key loaded via direct SQL.
	var fieldEncryptor *auth.FieldEncryptor
	if encKey := os.Getenv("MOCA_ENCRYPTION_KEY"); encKey != "" {
		var encErr error
		fieldEncryptor, encErr = auth.NewFieldEncryptor(encKey)
		if encErr != nil {
			return nil, fmt.Errorf("init field encryption: %w", encErr)
		}
		encHook := encryption.NewFieldEncryptionHook(fieldEncryptor)
		hookRegistry.RegisterGlobal(document.EventBeforeSave, hooks.PrioritizedHandler{
			Handler:  encHook.EncryptBeforeSave,
			AppName:  "moca_core",
			Priority: 100, // Run early — encrypt before other hooks see the data
		})
		docManager.SetPostLoadTransformer(encHook)
		logger.Info("field encryption enabled")
	} else {
		logger.Info("field encryption disabled (MOCA_ENCRYPTION_KEY not set)")
	}

	// ── Search ──────────────────────────────────────────────────────────
	var searchClient *search.Client
	var searchService *search.QueryService
	if client, err := search.NewClient(cfg.Config.Infrastructure.Search); err == nil {
		searchClient = client
		searchService = search.NewQueryService(client)
	} else if !errors.Is(err, search.ErrUnavailable) {
		logger.Warn("search unavailable at startup",
			slog.String("error", err.Error()),
		)
	}

	// ── API gateway ─────────────────────────────────────────────────────
	rateLimiter := api.NewRateLimiter(redisClients.Cache, logger)
	siteResolver := api.NewDBSiteResolver(dbManager, redisClients.Cache, logger)

	// Registries for custom middleware, endpoint handlers, and whitelisted methods.
	mwRegistry := api.NewMiddlewareRegistry()
	handlerRegistry := api.NewHandlerRegistry()
	methodRegistry := api.NewMethodRegistry()
	reportRegistry := api.NewReportRegistry()
	dashboardRegistry := api.NewDashboardRegistry()

	gw := api.NewGateway(
		api.WithDocManager(docManager),
		api.WithRegistry(registry),
		api.WithRedis(redisClients),
		api.WithLogger(logger),
		api.WithSiteResolver(siteResolver),
		api.WithRateLimiter(rateLimiter, nil),
		api.WithSearchService(searchService),
		api.WithAuthenticator(authenticator),
		api.WithPermissionChecker(scopeEnforcer),
		api.WithAPIKeyStore(apiKeyStore),
		api.WithFieldLevelTransformer(fieldLevelTransformer),
		api.WithMiddlewareRegistry(mwRegistry),
		api.WithHandlerRegistry(handlerRegistry),
		api.WithMethodRegistry(methodRegistry),
		api.WithReportRegistry(reportRegistry),
		api.WithDashboardRegistry(dashboardRegistry),
		api.WithI18nMiddleware(i18n.I18nMiddleware()),
	)

	handler := api.NewResourceHandler(gw)
	handler.RegisterRoutes(gw.Mux(), "v1")

	searchHandler := api.NewSearchHandler(gw)
	searchHandler.RegisterRoutes(gw.Mux(), "v1")

	authHandler := api.NewAuthHandler(jwtCfg, sessionMgr, userLoader, logger)
	authHandler.RegisterRoutes(gw.Mux(), "v1")

	// ── SSO Authentication ───────────────────────────────────────────
	userProvisioner := auth.NewUserProvisioner(logger)
	ssoConfigLoader := auth.NewSSOConfigLoader(logger)
	// Reuse the FieldEncryptor created above for decrypting SSO secrets
	// (client_secret, sp_private_key). If MOCA_ENCRYPTION_KEY is not set,
	// fieldEncryptor will be nil and SSO handler skips decryption.
	ssoHandler := api.NewSSOHandler(sessionMgr, userProvisioner,
		ssoConfigLoader.LoadFunc(), fieldEncryptor, redisClients.Session, logger)
	ssoHandler.RegisterRoutes(gw.Mux(), "v1")

	// File upload/download handler.
	fileManager := storage.NewFileManager(stor, dbManager, logger, 0)
	uploadHandler := api.NewUploadHandler(fileManager, scopeEnforcer, logger)
	uploadHandler.RegisterRoutes(gw.Mux(), "v1")

	// Custom endpoint router for per-DocType custom endpoints.
	customRouter := api.NewCustomEndpointRouter(registry, handlerRegistry, mwRegistry, scopeEnforcer, rateLimiter, logger)
	customRouter.RegisterRoutes(gw.Mux(), "v1")

	// Whitelisted method handler for /api/v1/method/{name}.
	methodHandler := api.NewMethodHandler(methodRegistry, logger)
	methodHandler.RegisterRoutes(gw.Mux(), "v1")

	// GraphQL handler — auto-generated schema from MetaType definitions.
	graphqlHandler := api.NewGraphQLHandler(gw)
	graphqlHandler.RegisterRoutes(gw.Mux())

	// Translation/i18n handler — serves translation bundles for the Desk UI.
	translator := i18n.NewTranslator(redisClients.Cache, dbManager.ForSite, logger)
	translationHandler := i18n.NewTranslationHandler(translator, logger)
	translationHandler.RegisterRoutes(gw.Mux(), "v1")

	// Report and Dashboard handlers.
	reportHandler := api.NewReportHandler(reportRegistry, dbManager, registry, scopeEnforcer, logger)
	reportHandler.RegisterRoutes(gw.Mux(), "v1")
	dashboardHandler := api.NewDashboardHandler(gw)
	dashboardHandler.SetDBManager(dbManager)
	dashboardHandler.RegisterRoutes(gw.Mux(), "v1")

	// Notification API endpoints.
	notifHandler := api.NewNotificationHandler(inAppNotifier, dbManager, logger)
	notifHandler.RegisterRoutes(gw.Mux(), "v1")

	vr := api.NewVersionRouter(handler, logger)
	gw.SetVersionRouter(vr)

	// ── Health checks ───────────────────────────────────────────────────
	version := cfg.Version
	if version == "" {
		version = "dev"
	}
	hc := observe.NewHealthChecker(dbManager.SystemPool(), redisClients, version, logger)
	hc.RegisterRoutes(gw.Mux())

	// ── Desk frontend (dev proxy or static files) & WebSocket stub ──────
	if cfg.Config != nil && cfg.Config.Development.DeskDevServer {
		deskPort := cfg.Config.Development.DeskPort
		if deskPort == 0 {
			deskPort = 3000
		}
		registerDeskDevProxy(gw.Mux(), deskPort, logger)
	} else {
		registerStaticFiles(gw.Mux(), cfg.StaticDir, logger)
	}
	hub := NewHub(logger)
	registerWebSocket(gw.Mux(), hub, jwtCfg, logger)

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
		fileStorage:  stor,
		searchClient: searchClient,
		hub:          hub,
		config:       cfg.Config,
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

	// Start the Redis pub/sub → WebSocket bridge.
	bridge := NewPubSubBridge(s.hub, s.redisClients.PubSub, s.logger)
	s.hub.SetOnSubscriptionChange(bridge.OnSubscriptionChange)
	go func() {
		if err := bridge.Run(ctx); err != nil && ctx.Err() == nil {
			s.logger.Error("pubsub bridge failed", slog.String("error", err.Error()))
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

// RedisClients returns the Redis client set for use by subsystems.
func (s *Server) RedisClients() *drivers.RedisClients {
	return s.redisClients
}

// Config returns the project configuration.
func (s *Server) Config() *config.ProjectConfig {
	return s.config
}

// SearchClient returns the Meilisearch client, or nil if unavailable.
func (s *Server) SearchClient() *search.Client {
	return s.searchClient
}

// Close releases database and Redis connections.
func (s *Server) Close() {
	if s.redisClients != nil {
		_ = s.redisClients.Close()
	}
	if s.searchClient != nil {
		s.searchClient.Close()
	}
	if s.dbManager != nil {
		s.dbManager.Close()
	}
}

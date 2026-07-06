package apiserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/auth"
	"github.com/huynle/brain-api/internal/blobstore"
	"github.com/huynle/brain-api/internal/bridge"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/logbuffer"
	mcppkg "github.com/huynle/brain-api/internal/mcp"
	"github.com/huynle/brain-api/internal/oauth"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/webui"
	"github.com/huynle/brain-api/pkg/pathutil"
)

// ServerOptions holds configuration for running the Brain API server.
type ServerOptions struct {
	Host            string
	Port            int
	BrainDir        string
	EnableAuth      bool
	LogLevel        string
	CORSOrigin      string
	OAuthPIN        string
	JWTSecret       string
	TaskDefaults    config.TaskDefaultsConfig
	FeatureCheckout config.FeatureCheckoutConfig
	Embedding       config.EmbeddingConfig
	Attachments     config.AttachmentConfig

	AttachmentExtraction config.AttachmentExtractionConfig
	Assistant            config.AssistantConfig
}

const defaultAttachmentMaxUploadSizeBytes int64 = 100 * 1024 * 1024

func normalizeAttachmentConfig(brainDir string, cfg config.AttachmentConfig) config.AttachmentConfig {
	if cfg.StorageRoot == "" {
		cfg.StorageRoot = filepath.Join(brainDir, "attachments")
	}
	if cfg.MaxUploadSizeBytes <= 0 {
		cfg.MaxUploadSizeBytes = defaultAttachmentMaxUploadSizeBytes
	}
	return cfg
}

// RunServer starts the Brain API HTTP server and blocks until context is cancelled.
// Returns error if server fails to start or encounters an error during shutdown.
func RunServer(ctx context.Context, opts ServerOptions) error {
	// Configure structured logging
	var logLevel slog.Level
	switch opts.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	})))

	router, dbPath, cleanup, err := buildHTTPHandler(ctx, opts)
	if err != nil {
		return err
	}
	defer cleanup()

	// ─── HTTP Server ────────────────────────────────────────────────
	addr := fmt.Sprintf("%s:%d", opts.Host, opts.Port)
	corsOrigin := opts.CORSOrigin
	if corsOrigin == "" {
		corsOrigin = "*"
	}
	srv := &http.Server{
		Addr:        addr,
		Handler:     router,
		ReadTimeout: 15 * time.Second,
		// WriteTimeout disabled (0) to support SSE long-lived streaming connections.
		// Heartbeats at 15s intervals keep SSE connections alive and detect dead clients.
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in background
	errCh := make(chan error, 1)
	go func() {
		slog.Info("starting brain-api",
			"addr", addr,
			"brain_dir", opts.BrainDir,
			"db_path", dbPath,
			"auth_enabled", opts.EnableAuth,
			"oauth_enabled", true,
			"cors_origin", corsOrigin,
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		slog.Info("shutting down server")
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	// Graceful shutdown with 10s timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}

	slog.Info("server stopped")
	return nil
}

func buildHTTPHandler(ctx context.Context, opts ServerOptions) (http.Handler, string, func(), error) {
	// ─── Storage Layer ──────────────────────────────────────────────
	// Expand ~ to home directory (Go does not do this automatically)
	opts.BrainDir = pathutil.ExpandTilde(opts.BrainDir)
	dataDir := config.MigrateDataDir(opts.BrainDir)
	dbPath := filepath.Join(dataDir, "brain.db")

	// Ensure the data directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, "", nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	store, err := storage.New(dbPath)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to open database at %s: %w", dbPath, err)
	}
	cleanup := func() { _ = store.Close() }

	// ─── Indexer ────────────────────────────────────────────────────
	idx := indexer.NewIndexer(opts.BrainDir, store)

	// Run incremental index on startup (fast for unchanged files)
	slog.Info("indexing brain directory", "dir", opts.BrainDir)
	result, err := idx.IndexChanged()
	if err != nil {
		slog.Warn("indexing failed, continuing with stale index", "error", err)
	} else {
		slog.Info("indexing complete",
			"added", result.Added,
			"updated", result.Updated,
			"deleted", result.Deleted,
			"skipped", result.Skipped,
			"errors", len(result.Errors),
			"duration", result.Duration,
		)
	}

	// ─── Build Config ───────────────────────────────────────────────
	corsOrigin := opts.CORSOrigin
	if corsOrigin == "" {
		corsOrigin = "*" // Match standalone brain-api default
	}
	cfg := config.Config{
		BrainDir:        opts.BrainDir,
		Host:            opts.Host,
		Port:            opts.Port,
		EnableAuth:      opts.EnableAuth,
		CORSOrigin:      corsOrigin,
		OAuthPIN:        opts.OAuthPIN,
		JWTSecret:       opts.JWTSecret,
		TaskDefaults:    opts.TaskDefaults,
		FeatureCheckout: opts.FeatureCheckout,
		Embedding:       opts.Embedding,
		Attachments:     normalizeAttachmentConfig(opts.BrainDir, opts.Attachments),

		AttachmentExtraction: opts.AttachmentExtraction,
		Assistant:            opts.Assistant,
	}

	// ─── Services ───────────────────────────────────────────────────
	// Create embedding client if enabled
	var embeddingClient service.EmbeddingClient
	if cfg.Embedding.Enabled {
		var err error
		embeddingClient, err = service.NewAiFactoryEmbeddingClient(cfg.Embedding)
		if err != nil {
			slog.Warn("Failed to create embedding client, semantic search disabled", "error", err)
			embeddingClient = nil
		}
	}

	brainSvc := service.NewBrainService(&cfg, store, idx, nil, embeddingClient)
	if err := service.EnsureBuiltInFeatureCheckoutAutomation(ctx, brainSvc, service.BuiltInFeatureCheckoutConfig{
		Enabled:            cfg.FeatureCheckout.Enabled,
		Agent:              cfg.TaskDefaults.Agent,
		Model:              cfg.TaskDefaults.Model,
		Executor:           cfg.TaskDefaults.Executor,
		ExecutionMode:      cfg.TaskDefaults.ExecutionMode,
		TargetWorkdir:      cfg.TaskDefaults.TargetWorkdir,
		MergeTargetBranch:  cfg.TaskDefaults.MergeTargetBranch,
		MergePolicy:        cfg.TaskDefaults.MergePolicy,
		MergeStrategy:      cfg.TaskDefaults.MergeStrategy,
		RemoteBranchPolicy: cfg.TaskDefaults.RemoteBranchPolicy,
		OpenPRBeforeMerge:  cfg.TaskDefaults.OpenPRBeforeMerge,
	}); err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("failed to ensure built-in feature checkout automation: %w", err)
	}
	blobStore, err := blobstore.NewFilesystemStore(cfg.Attachments.StorageRoot, cfg.Attachments.MaxUploadSizeBytes)
	if err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("failed to initialize attachment blob store: %w", err)
	}
	attachmentExtractor := service.NewOpenRouterAttachmentExtractor(cfg.AttachmentExtraction)
	attachmentSvc := service.NewAttachmentService(
		store,
		blobStore,
		brainSvc,
		cfg.Attachments.MaxUploadSizeBytes,
		service.WithAttachmentMIMEPolicy(cfg.Attachments.AllowedMIMETypes, cfg.Attachments.BlockedMIMETypes),
		service.WithAttachmentExtractor(attachmentExtractor),
		service.WithAttachmentDerivedChangeHook(brainSvc),
	)
	taskSvc := service.NewTaskService(&cfg, store)
	runnerSvc := service.NewRunnerServiceWithStorage(store)
	runnerRegistrySvc := service.NewRunnerRegistryService(store)
	clientContextSvc := service.NewClientContextService(store)
	placementSvc := service.NewProjectPlacementService(store)
	monitorSvc := service.NewMonitorService(brainSvc)
	webhookSvc := service.NewWebhookService(store)

	// ─── Realtime Hub ───────────────────────────────────────────────
	hub := realtime.NewHub()

	// ─── Background Claim Cleanup ──────────────────────────────────
	taskSvc.StartClaimCleanup(ctx, service.DefaultClaimCleanupInterval)

	// ─── Runner Lifecycle Management ───────────────────────────────
	runnerRegistrySvc.SetHub(hub)
	runnerRegistrySvc.StartLifecycleManager(ctx, service.DefaultLifecycleInterval)

	// ─── Scheduler Lifecycle ────────────────────────────────────────
	schedulerSvc := service.NewSchedulerService(taskSvc, runnerSvc, runnerRegistrySvc, placementSvc, store, hub)
	schedulerSvc.Start(ctx, service.DefaultSchedulerInterval)

	// ─── Event Hub & Services ──────────────────────────────────────
	eventHub := realtime.NewEventHub()
	eventSvc := service.NewEventService(eventHub)
	eventSvc.SetFeatureTaskLister(taskSvc)
	eventSvc.SetFeatureAssignmentCleaner(store)

	// ─── Feature Cascade ───────────────────────────────────────────
	// Manual "Run feature now" workflow needs the cascade to drain queued
	// tasks as in-flight ones complete — even while the project is paused.
	// Wire here so SchedulerService can register cascades from RunFeatureNow
	// and the cascade can call back via the FeatureRunner interface.
	featureCascade := service.NewFeatureCascadeService(eventHub, schedulerSvc)
	schedulerSvc.SetFeatureCascade(featureCascade)
	featureCascade.Start(ctx)
	automationSvc := service.NewAutomationService(brainSvc)
	automationSvc.SetPauseChecker(runnerSvc)
	go automationSvc.Start(ctx, eventHub)

	// ─── Goal Reconcile Handler ────────────────────────────────────
	// GoalService subscribes to the EventHub and drives the deterministic
	// in-process reconcile for goal automations when their linked task/feature
	// lifecycle events fire.
	goalSvc := service.NewGoalService(brainSvc, taskSvc, store)
	assistantSvc := api.NewAssistantService(api.AssistantServiceOptions{
		Enabled:   cfg.Assistant.Enabled,
		Provider:  cfg.Assistant.Provider,
		BaseURL:   cfg.Assistant.BaseURL,
		APIKeyEnv: cfg.Assistant.APIKeyEnv,
		Model:     cfg.Assistant.Model,
		Timeout:   time.Duration(cfg.Assistant.TimeoutMs) * time.Millisecond,
		Brain:     brainSvc,
		Goals:     goalSvc,
		Tasks:     taskSvc,
		Runner:    runnerSvc,
		Runners:   runnerRegistrySvc,
		Events:    eventSvc,
	})
	go goalSvc.Start(ctx, eventHub)

	// ─── Webhook Dispatcher ────────────────────────────────────────
	// Subscribe to all EventHub events and deliver to matching webhooks.
	webhookDispatcher := realtime.NewWebhookDispatcher(eventHub, webhookSvc)
	go webhookDispatcher.Start(ctx)

	// ─── Trigger Dispatcher ────────────────────────────────────────
	// Subscribe to all EventHub events and evaluate task triggers.
	// When events match a task's trigger config, the task is activated (set to pending).
	triggerStore := service.NewTriggerTaskStoreAdapter(store)
	triggerSvc := service.NewTriggerService(triggerStore)
	triggerDispatcher := realtime.NewTriggerDispatcher(eventHub, triggerSvc)
	go triggerDispatcher.Start(ctx)

	// ─── Runner Bridge Hub (remote control) ────────────────────────
	bridgeHub := bridge.NewHub(hub)

	// ─── Log Buffer ─────────────────────────────────────────────────
	logBuf := logbuffer.New(logbuffer.DefaultMaxLines)

	// ─── API Handler & Router ───────────────────────────────────────
	// Shared operator credential verifier (password login + OAuth consent).
	credVerifier := auth.NewVerifierFromEnv()

	handler := api.NewHandler(
		brainSvc,
		api.WithAttachmentService(attachmentSvc),
		api.WithTaskService(taskSvc),
		api.WithRunnerService(runnerSvc),
		api.WithRunnerRegistryService(runnerRegistrySvc),
		api.WithClientContextService(clientContextSvc),
		api.WithProjectPlacementService(placementSvc),
		api.WithSchedulerService(schedulerSvc),
		api.WithSchedulerVisibilityService(store),
		api.WithRunTaskService(schedulerSvc),
		api.WithRunFeatureService(schedulerSvc),
		api.WithMonitorService(monitorSvc),
		api.WithTokenService(store),
		api.WithHub(hub),
		api.WithEventService(eventSvc),
		api.WithWebhookService(webhookSvc),
		api.WithGoalService(goalSvc),
		api.WithAutomationRunService(automationSvc),
		api.WithAssistantService(assistantSvc),
		api.WithBridgeService(bridgeHub),
		api.WithLogBuffer(logBuf),
		api.WithTaskDefaults(cfg.TaskDefaults),
		api.WithCredentialVerifier(credVerifier),
		api.WithPasswordTokenStore(store),
	)

	// ─── Rate Limiting ─────────────────────────────────────────────
	var rateLimiter *api.RateLimiter
	if cfg.RateLimitPerMinute > 0 {
		burst := cfg.RateLimitBurst
		if burst <= 0 {
			burst = cfg.RateLimitPerMinute
		}
		rateLimiter = api.NewRateLimiter(api.RateLimitConfig{
			RequestsPerMinute: cfg.RateLimitPerMinute,
			BurstSize:         burst,
			CleanupInterval:   5 * time.Minute,
		})
		defer rateLimiter.Stop()
		slog.Info("rate limiting enabled",
			"requests_per_minute", cfg.RateLimitPerMinute,
			"burst", burst,
		)
	}

	routerOpts := []api.RouterOption{
		api.WithHandler(handler),
		api.WithDualAuth(store, store),
		api.WithEmbeddingReady(!cfg.Embedding.Enabled || embeddingClient != nil),
	}
	if rateLimiter != nil {
		routerOpts = append(routerOpts, api.WithRateLimiter(rateLimiter))
	}
	router := api.NewRouter(cfg, routerOpts...)

	// ─── OAuth ─────────────────────────────────────────────────────
	// Persist OAuth flow state (clients, auth codes, refresh tokens) in SQLite
	// so registered clients survive restarts — otherwise every restart yields
	// "unknown client_id" for the Claude connector and the PWA.
	oauthStore := oauth.NewPersistentStore(store)
	oauthHandler := oauth.NewHandler(oauthStore,
		oauth.WithAccessTokenStore(store),
		oauth.WithCredentialVerifier(credVerifier),
	)
	oauth.RegisterRoutes(router, oauthHandler)

	// ─── MCP Streamable HTTP Transport ──────────────────────────────
	mcpClient := mcppkg.NewAPIClient(fmt.Sprintf("http://localhost:%d", opts.Port))
	mcpHTTP := mcppkg.NewHTTPHandler(mcpClient)
	authValidator := &api.CompositeValidator{
		APIValidator:   store,
		OAuthValidator: store,
	}
	router.Route("/mcp", func(r chi.Router) {
		r.Use(api.Auth(opts.EnableAuth, authValidator, opts.JWTSecret))
		r.Post("/", mcpHTTP.ServeHTTP)
		r.Get("/", mcpHTTP.ServeHTTP)
		r.Delete("/", mcpHTTP.ServeHTTP)
	})
	// MCP at root / (for clients that use the base URL as the MCP endpoint).
	// Note: GET / is intentionally omitted here — the embedded web UI (see
	// webui.Handler below) owns browser navigations to "/", while MCP clients
	// use POST/DELETE at the root. Clients that need a GET stream use /mcp.
	router.Group(func(r chi.Router) {
		r.Use(api.Auth(opts.EnableAuth, authValidator, opts.JWTSecret))
		r.Post("/", mcpHTTP.ServeHTTP)
		r.Delete("/", mcpHTTP.ServeHTTP)
	})

	// Wrap the API router with the embedded Brain PWA. The webui handler serves
	// the SPA + static assets for browser navigations and delegates all API,
	// OAuth, MCP, and well-known routes back to the router untouched.
	return webui.Handler(router), dbPath, cleanup, nil
}

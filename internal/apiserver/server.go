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
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/indexer"
	mcppkg "github.com/huynle/brain-api/internal/mcp"
	"github.com/huynle/brain-api/internal/oauth"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/pkg/pathutil"
)

// ServerOptions holds configuration for running the Brain API server.
type ServerOptions struct {
	Host         string
	Port         int
	BrainDir     string
	EnableAuth   bool
	LogLevel     string
	CORSOrigin   string
	OAuthPIN     string
	TaskDefaults config.TaskDefaultsConfig
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

	// ─── Storage Layer ──────────────────────────────────────────────
	// Expand ~ to home directory (Go does not do this automatically)
	opts.BrainDir = pathutil.ExpandTilde(opts.BrainDir)
	dataDir := config.MigrateDataDir(opts.BrainDir)
	dbPath := filepath.Join(dataDir, "brain.db")

	// Ensure the data directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return fmt.Errorf("failed to create database directory: %w", err)
	}

	store, err := storage.New(dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database at %s: %w", dbPath, err)
	}
	defer store.Close()

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
		BrainDir:     opts.BrainDir,
		Host:         opts.Host,
		Port:         opts.Port,
		EnableAuth:   opts.EnableAuth,
		CORSOrigin:   corsOrigin,
		OAuthPIN:     opts.OAuthPIN,
		TaskDefaults: opts.TaskDefaults,
	}

	// ─── Services ───────────────────────────────────────────────────
	brainSvc := service.NewBrainService(&cfg, store, idx)
	taskSvc := service.NewTaskService(&cfg, store)
	runnerSvc := service.NewRunnerService()
	runnerRegistrySvc := service.NewRunnerRegistryService(store)
	monitorSvc := service.NewMonitorService(brainSvc)

	// ─── Background Claim Cleanup ──────────────────────────────────
	taskSvc.StartClaimCleanup(ctx, service.DefaultClaimCleanupInterval)

	// ─── Runner Lifecycle Management ───────────────────────────────
	runnerRegistrySvc.StartLifecycleManager(ctx, service.DefaultLifecycleInterval)

	// ─── Realtime Hub ───────────────────────────────────────────────
	hub := realtime.NewHub()

	// ─── API Handler & Router ───────────────────────────────────────
	handler := api.NewHandler(
		brainSvc,
		api.WithTaskService(taskSvc),
		api.WithRunnerService(runnerSvc),
		api.WithRunnerRegistryService(runnerRegistrySvc),
		api.WithMonitorService(monitorSvc),
		api.WithTokenService(store),
		api.WithHub(hub),
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
	}
	if rateLimiter != nil {
		routerOpts = append(routerOpts, api.WithRateLimiter(rateLimiter))
	}
	router := api.NewRouter(cfg, routerOpts...)

	// ─── OAuth ─────────────────────────────────────────────────────
	oauthStore := oauth.NewStore()
	oauthHandler := oauth.NewHandler(oauthStore, oauth.WithAccessTokenStore(store))
	oauth.RegisterRoutes(router, oauthHandler)

	// ─── MCP Streamable HTTP Transport ──────────────────────────────
	mcpClient := mcppkg.NewAPIClient(fmt.Sprintf("http://localhost:%d", opts.Port))
	mcpHTTP := mcppkg.NewHTTPHandler(mcpClient)
	authValidator := &api.CompositeValidator{
		APIValidator:   store,
		OAuthValidator: store,
	}
	router.Route("/mcp", func(r chi.Router) {
		r.Use(api.Auth(opts.EnableAuth, authValidator))
		r.Post("/", mcpHTTP.ServeHTTP)
		r.Get("/", mcpHTTP.ServeHTTP)
		r.Delete("/", mcpHTTP.ServeHTTP)
	})
	// MCP at root / (for clients that use the base URL as the MCP endpoint)
	router.Group(func(r chi.Router) {
		r.Use(api.Auth(opts.EnableAuth, authValidator))
		r.Post("/", mcpHTTP.ServeHTTP)
		r.Get("/", mcpHTTP.ServeHTTP)
		r.Delete("/", mcpHTTP.ServeHTTP)
	})

	// ─── HTTP Server ────────────────────────────────────────────────
	addr := fmt.Sprintf("%s:%d", opts.Host, opts.Port)
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
			"cors_origin", cfg.CORSOrigin,
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

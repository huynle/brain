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
	"github.com/huynle/brain-api/internal/embeddings"
	"github.com/huynle/brain-api/internal/events"
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
	Host        string
	Port        int
	BrainDir    string
	EnableAuth  bool
	LogLevel    string
	CORSOrigin  string
	OAuthPIN    string
	Embeddings  config.EmbeddingConfig
	FileWatcher config.FileWatcherConfig
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
	embeddingCfg := opts.Embeddings.Normalize()
	indexerOptions, err := indexerOptionsFromEmbeddingConfig(embeddingCfg)
	if err != nil {
		return err
	}
	if embeddingCfg.Enabled {
		slog.Info("semantic embeddings configured",
			"provider", embeddingCfg.Provider,
			"model", embeddingCfg.Model,
			"base_url", embeddingCfg.BaseURL,
			"batch_size", embeddingCfg.BatchSize,
		)
	}
	idx := indexer.NewIndexer(opts.BrainDir, store, indexerOptions...)

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
	if embeddingCfg.Enabled && err == nil {
		backfilled, err := idx.BackfillMissingOrStaleEmbeddings(ctx)
		if err != nil {
			slog.Warn("embedding backfill failed, continuing startup", "backfilled", backfilled, "error", err)
		} else if backfilled > 0 {
			slog.Info("embedding backfill complete", "backfilled", backfilled)
		}
	}

	fileWatcher, err := startConfiguredFileWatcher(opts.BrainDir, idx, opts.FileWatcher)
	if err != nil {
		return err
	}
	if fileWatcher != nil {
		defer fileWatcher.Stop()
	}

	// ─── Build Config ───────────────────────────────────────────────
	corsOrigin := opts.CORSOrigin
	if corsOrigin == "" {
		corsOrigin = "*" // Match standalone brain-api default
	}
	cfg := config.Config{
		BrainDir:    opts.BrainDir,
		Host:        opts.Host,
		Port:        opts.Port,
		EnableAuth:  opts.EnableAuth,
		CORSOrigin:  corsOrigin,
		OAuthPIN:    opts.OAuthPIN,
		Embeddings:  embeddingCfg,
		FileWatcher: opts.FileWatcher.Normalize(),
	}

	// ─── Event Bus ──────────────────────────────────────────────────
	eventBus := events.NewMemoryBus()
	defer eventBus.Close()

	// ─── Services ───────────────────────────────────────────────────
	brainSvc := service.NewBrainService(&cfg, store, idx, eventBus)
	taskSvc := service.NewTaskService(&cfg, store)
	runnerSvc := service.NewRunnerService()
	runnersSvc := service.NewRunnersService(store, taskSvc)
	monitorSvc := service.NewMonitorService(brainSvc)

	// ─── Realtime Hub ───────────────────────────────────────────────
	hub := realtime.NewHub()

	// Bridge: subscribe Hub to EventBus entry events for backward-compatible SSE
	realtime.BridgeBusToHub(eventBus, hub, taskSvc)

	// ─── API Handler & Router ───────────────────────────────────────
	handler := api.NewHandler(
		brainSvc,
		api.WithTaskService(taskSvc),
		api.WithRunnerService(runnerSvc),
		api.WithRunnersService(runnersSvc),
		api.WithMonitorService(monitorSvc),
		api.WithTokenService(store),
		api.WithHub(hub),
		api.WithEventBus(eventBus),
	)

	router := api.NewRouter(cfg, api.WithHandler(handler), api.WithDualAuth(store, store))

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

	// ─── Stale Runner Detection ────────────────────────────────────
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				staleIDs, err := runnersSvc.MarkStaleAndRelease(ctx)
				if err != nil {
					slog.Debug("stale runner check failed", "error", err)
				} else if len(staleIDs) > 0 {
					slog.Info("stale runners detected and tasks released", "runners", staleIDs)
				}
			}
		}
	}()

	// ─── Cron Emitter ──────────────────────────────────────────────
	cronSource := events.NewStorageScheduleSource(store)
	cronEmitter := events.NewCronEmitter(events.CronEmitterConfig{
		Bus:    eventBus,
		Source: cronSource,
		// Default 30s tick interval
	})
	go cronEmitter.Start(ctx)

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

func startConfiguredFileWatcher(brainDir string, idx *indexer.Indexer, cfg config.FileWatcherConfig) (*indexer.FileWatcher, error) {
	cfg = cfg.Normalize()
	if !cfg.Enabled {
		return nil, nil
	}

	fw, err := indexer.NewFileWatcher(brainDir, idx, &indexer.FileWatcherOptions{
		DebounceMs:     cfg.DebounceMS,
		IgnorePatterns: cfg.IgnorePatterns,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize file watcher: %w", err)
	}
	if err := fw.Start(); err != nil {
		return nil, fmt.Errorf("start file watcher: %w", err)
	}
	slog.Info("file watcher started", "dir", brainDir)
	return fw, nil
}

func indexerOptionsFromEmbeddingConfig(cfg config.EmbeddingConfig) ([]indexer.Option, error) {
	cfg = cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, nil
	}

	switch cfg.Provider {
	case "ollama":
		embedder, err := embeddings.NewOllamaEmbedder(embeddings.OllamaConfig{
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
			Timeout: time.Duration(cfg.TimeoutMS) * time.Millisecond,
		})
		if err != nil {
			return nil, fmt.Errorf("initialize ollama embedder: %w", err)
		}
		return []indexer.Option{indexer.WithEmbeddings(embedder, cfg.Model, cfg.BatchSize)}, nil
	default:
		return nil, fmt.Errorf("unsupported embedding provider %q", cfg.Provider)
	}
}

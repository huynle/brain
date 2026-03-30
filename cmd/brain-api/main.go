// Package main is the entry point for the Brain API server.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
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
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Configure structured logging
	var logLevel slog.Level
	switch cfg.LogLevel {
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
	dataDir := config.MigrateDataDir(cfg.BrainDir)
	dbPath := filepath.Join(dataDir, "brain.db")

	// Ensure the data directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		slog.Error("failed to create database directory", "error", err)
		os.Exit(1)
	}

	store, err := storage.New(dbPath)
	if err != nil {
		slog.Error("failed to open database", "error", err, "path", dbPath)
		os.Exit(1)
	}
	defer store.Close()

	// ─── Indexer ────────────────────────────────────────────────────
	idx := indexer.NewIndexer(cfg.BrainDir, store)

	// Run incremental index on startup (fast for unchanged files)
	slog.Info("indexing brain directory", "dir", cfg.BrainDir)
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

	// ─── Services ───────────────────────────────────────────────────
	brainSvc := service.NewBrainService(&cfg, store, idx)
	taskSvc := service.NewTaskService(&cfg, store)
	runnerSvc := service.NewRunnerService()
	monitorSvc := service.NewMonitorService(brainSvc)

	// ─── Realtime Hub ───────────────────────────────────────────────
	hub := realtime.NewHub()

	// ─── OAuth Store ────────────────────────────────────────────────
	oauthStore := oauth.NewStore()

	// ─── API Handler & Router ───────────────────────────────────────
	handler := api.NewHandler(
		brainSvc,
		api.WithTaskService(taskSvc),
		api.WithRunnerService(runnerSvc),
		api.WithMonitorService(monitorSvc),
		api.WithTokenService(store),
		api.WithHub(hub),
	)

	router := api.NewRouter(cfg, api.WithHandler(handler), api.WithDualAuth(store, store))

	// ─── OAuth Routes ───────────────────────────────────────────────
	oauthHandler := oauth.NewHandler(oauthStore, oauth.WithAccessTokenStore(store))
	oauth.RegisterRoutes(router, oauthHandler)

	// ─── MCP Streamable HTTP Transport ──────────────────────────────
	// Create an MCP HTTP handler that creates per-request MCP servers.
	// Each request gets an API client with the caller's auth token forwarded.
	mcpClient := mcppkg.NewAPIClient(fmt.Sprintf("http://localhost:%d", cfg.Port))
	mcpHTTP := mcppkg.NewHTTPHandler(mcpClient)

	// Register MCP endpoint with auth middleware.
	// The auth middleware validates the Bearer token, then the MCP handler
	// forwards that token to the internal API client for tool execution.
	authValidator := &api.CompositeValidator{
		APIValidator:   store,
		OAuthValidator: store,
	}
	// MCP at /mcp/ (canonical path, referenced in protected resource metadata)
	router.Route("/mcp", func(r chi.Router) {
		r.Use(api.Auth(cfg.EnableAuth, authValidator))
		r.Post("/", mcpHTTP.ServeHTTP)
		r.Get("/", mcpHTTP.ServeHTTP)
		r.Delete("/", mcpHTTP.ServeHTTP)
	})
	// MCP at root / (for clients that use the base URL as the MCP endpoint)
	router.Group(func(r chi.Router) {
		r.Use(api.Auth(cfg.EnableAuth, authValidator))
		r.Post("/", mcpHTTP.ServeHTTP)
		r.Get("/", mcpHTTP.ServeHTTP)
		r.Delete("/", mcpHTTP.ServeHTTP)
	})

	// ─── HTTP Server ────────────────────────────────────────────────
	srv := &http.Server{
		Addr:        cfg.Addr(),
		Handler:     router,
		ReadTimeout: 15 * time.Second,
		// WriteTimeout disabled (0) to support SSE long-lived streaming connections.
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in background
	go func() {
		slog.Info("starting brain-api",
			"addr", cfg.Addr(),
			"brain_dir", cfg.BrainDir,
			"db_path", dbPath,
			"auth_enabled", cfg.EnableAuth,
			"oauth_enabled", true,
			"cors_origin", cfg.CORSOrigin,
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown on SIGINT/SIGTERM
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("shutting down", "signal", sig.String())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
}

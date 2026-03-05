package apiserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/storage"
)

// ServerOptions holds configuration for running the Brain API server.
type ServerOptions struct {
	Host       string
	Port       int
	BrainDir   string
	EnableAuth bool
	APIKey     string
	LogLevel   string
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
	dbPath := filepath.Join(opts.BrainDir, ".zk", "brain.db")

	// Ensure the .zk directory exists
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
	cfg := config.Config{
		BrainDir:   opts.BrainDir,
		Host:       opts.Host,
		Port:       opts.Port,
		EnableAuth: opts.EnableAuth,
		APIKey:     opts.APIKey,
	}

	// ─── Services ───────────────────────────────────────────────────
	brainSvc := service.NewBrainService(&cfg, store, idx)
	taskSvc := service.NewTaskService(&cfg, store)
	runnerSvc := service.NewRunnerService()
	monitorSvc := service.NewMonitorService(brainSvc)

	// ─── Realtime Hub ───────────────────────────────────────────────
	hub := realtime.NewHub()

	// ─── API Handler & Router ───────────────────────────────────────
	handler := api.NewHandler(
		brainSvc,
		api.WithTaskService(taskSvc),
		api.WithRunnerService(runnerSvc),
		api.WithMonitorService(monitorSvc),
		api.WithHub(hub),
	)

	router := api.NewRouter(cfg, api.WithHandler(handler))

	// ─── HTTP Server ────────────────────────────────────────────────
	addr := fmt.Sprintf("%s:%d", opts.Host, opts.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in background
	errCh := make(chan error, 1)
	go func() {
		slog.Info("starting brain-api",
			"addr", addr,
			"brain_dir", opts.BrainDir,
			"db_path", dbPath,
			"auth_enabled", opts.EnableAuth,
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

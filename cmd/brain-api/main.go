// Package main is the entry point for the Brain API server.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/indexer"
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
	dbPath := filepath.Join(cfg.BrainDir, ".zk", "brain.db")

	// Ensure the .zk directory exists
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
	srv := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in background
	go func() {
		slog.Info("starting brain-api",
			"addr", cfg.Addr(),
			"brain_dir", cfg.BrainDir,
			"db_path", dbPath,
			"auth_enabled", cfg.EnableAuth,
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

package apiserver

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/config"
)

// TestRunServer_BasicStartup tests that RunServer can start and stop gracefully.
func TestRunServer_BasicStartup(t *testing.T) {
	tempDir := t.TempDir()
	brainDir := filepath.Join(tempDir, "brain")
	if err := os.MkdirAll(brainDir, 0o755); err != nil {
		t.Fatalf("failed to create brain dir: %v", err)
	}

	opts := ServerOptions{
		Host:     "localhost",
		Port:     0, // Let OS assign a port
		BrainDir: brainDir,
		LogLevel: "error", // Quiet during tests
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Run server in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunServer(ctx, opts)
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Cancel context to trigger shutdown
	cancel()

	// Wait for server to stop
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled && err != http.ErrServerClosed {
			t.Fatalf("RunServer failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop within timeout")
	}
}

// TestRunServer_ContextCancellation tests that the server respects context cancellation.
func TestRunServer_ContextCancellation(t *testing.T) {
	tempDir := t.TempDir()
	brainDir := filepath.Join(tempDir, "brain")
	if err := os.MkdirAll(brainDir, 0o755); err != nil {
		t.Fatalf("failed to create brain dir: %v", err)
	}

	opts := ServerOptions{
		Host:     "localhost",
		Port:     0,
		BrainDir: brainDir,
		LogLevel: "error",
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start server
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunServer(ctx, opts)
	}()

	// Wait a bit for server to start
	time.Sleep(100 * time.Millisecond)

	// Cancel immediately
	cancel()

	// Server should stop within shutdown timeout
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled && err != http.ErrServerClosed {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(12 * time.Second): // 10s shutdown timeout + 2s buffer
		t.Fatal("server did not respect context cancellation")
	}
}

// TestRunServer_InvalidBrainDir tests error handling for invalid brain directory.
func TestRunServer_InvalidBrainDir(t *testing.T) {
	opts := ServerOptions{
		Host:     "localhost",
		Port:     0,
		BrainDir: "/nonexistent/path/that/does/not/exist",
		LogLevel: "error",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := RunServer(ctx, opts)
	if err == nil {
		t.Fatal("expected error for invalid brain dir, got nil")
	}
}

// TestRunServer_PortAlreadyInUse tests handling when port is already bound.
func TestRunServer_PortAlreadyInUse(t *testing.T) {
	tempDir := t.TempDir()
	brainDir := filepath.Join(tempDir, "brain")
	if err := os.MkdirAll(brainDir, 0o755); err != nil {
		t.Fatalf("failed to create brain dir: %v", err)
	}

	// Start a dummy server to occupy a port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to start dummy listener: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	opts := ServerOptions{
		Host:     "localhost",
		Port:     port, // Use the occupied port
		BrainDir: brainDir,
		LogLevel: "error",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = RunServer(ctx, opts)
	if err == nil {
		t.Fatal("expected error for port already in use, got nil")
	}
}

func TestIndexerOptionsFromEmbeddingConfigDisabledIgnoresInvalidProvider(t *testing.T) {
	options, err := indexerOptionsFromEmbeddingConfig(config.EmbeddingConfig{Provider: "unknown"})
	if err != nil {
		t.Fatalf("indexerOptionsFromEmbeddingConfig returned error for disabled config: %v", err)
	}
	if len(options) != 0 {
		t.Fatalf("options length = %d, want 0", len(options))
	}
}

func TestIndexerOptionsFromEmbeddingConfigEnabledRequiresValidProvider(t *testing.T) {
	_, err := indexerOptionsFromEmbeddingConfig(config.EmbeddingConfig{
		Enabled:  true,
		Provider: "unknown",
		Model:    "test-model",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported embedding provider") {
		t.Fatalf("error = %v, want unsupported provider", err)
	}
}

func TestIndexerOptionsFromEmbeddingConfigEnabledBuildsOllamaOption(t *testing.T) {
	options, err := indexerOptionsFromEmbeddingConfig(config.EmbeddingConfig{
		Enabled:   true,
		Provider:  "ollama",
		Model:     "test-model",
		TimeoutMS: 1000,
	})
	if err != nil {
		t.Fatalf("indexerOptionsFromEmbeddingConfig returned error: %v", err)
	}
	if len(options) != 1 {
		t.Fatalf("options length = %d, want 1", len(options))
	}
}

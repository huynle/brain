package mcpserver

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

// TestRunMCPServer_BasicStartup tests basic MCP server lifecycle.
func TestRunMCPServer_BasicStartup(t *testing.T) {
	opts := MCPOptions{
		APIURL: "http://localhost:3333",
	}

	// Create stdin/stdout pipes
	stdin := strings.NewReader("")
	stdout := &bytes.Buffer{}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should respect context cancellation and handle EOF gracefully
	err := RunMCPServer(ctx, opts, stdin, stdout)
	// EOF or context timeout are expected
	if err != nil && err != io.EOF && err != context.DeadlineExceeded {
		// Check if it's just EOF error message
		if !strings.Contains(err.Error(), "EOF") {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

// TestRunMCPServer_EmptyAPIURL tests error handling for missing API URL.
func TestRunMCPServer_EmptyAPIURL(t *testing.T) {
	opts := MCPOptions{
		APIURL: "", // Empty URL should error
	}

	stdin := strings.NewReader("")
	stdout := &bytes.Buffer{}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := RunMCPServer(ctx, opts, stdin, stdout)
	if err == nil {
		t.Fatal("expected error for empty API URL, got nil")
	}
}

// TestRunMCPServer_ContextCancellation tests that server respects context.
func TestRunMCPServer_ContextCancellation(t *testing.T) {
	opts := MCPOptions{
		APIURL: "http://localhost:3333",
	}

	stdin := strings.NewReader("")
	stdout := &bytes.Buffer{}

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- RunMCPServer(ctx, opts, stdin, stdout)
	}()

	// Cancel immediately
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Should stop quickly
	select {
	case err := <-errCh:
		// EOF or cancellation are expected
		if err != nil && err != io.EOF && err != context.Canceled && !strings.Contains(err.Error(), "EOF") {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop after context cancellation")
	}
}

package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestRunMCPServer_ToolsListIncludesProjectTools(t *testing.T) {
	opts := MCPOptions{APIURL: "http://localhost:3333"}
	// MCP stdio transport is newline-delimited JSON (NDJSON): each JSON-RPC
	// message is one line terminated by '\n', no Content-Length header.
	message := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	stdin := strings.NewReader(message + "\n")
	stdout := &bytes.Buffer{}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := RunMCPServer(ctx, opts, stdin, stdout); err != nil && err != io.EOF && !strings.Contains(err.Error(), "EOF") {
		t.Fatalf("RunMCPServer error: %v", err)
	}

	// Response is one JSON object on one line.
	payload := strings.TrimRight(stdout.String(), "\n")
	if payload == "" {
		t.Fatalf("empty stdout")
	}
	if strings.Contains(payload, "\n") {
		t.Fatalf("expected a single line, got multiple:\n%q", stdout.String())
	}

	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(payload), &resp); err != nil {
		t.Fatalf("decode tools/list response: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range resp.Result.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"context_resolve", "project_placement_get", "project_placement_put"} {
		if !names[want] {
			t.Fatalf("tools/list response missing %q", want)
		}
	}
}

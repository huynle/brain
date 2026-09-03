package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// Constructor Tests
// =============================================================================

func TestNewClient(t *testing.T) {
	c := NewClient("http://localhost:3333", "token123", "test-project")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.apiURL != "http://localhost:3333" {
		t.Errorf("apiURL = %q, want %q", c.apiURL, "http://localhost:3333")
	}
	if c.apiToken != "token123" {
		t.Errorf("apiToken = %q, want %q", c.apiToken, "token123")
	}
	if c.projectID != "test-project" {
		t.Errorf("projectID = %q, want %q", c.projectID, "test-project")
	}
}

func TestNewClient_TrailingSlashTrimmed(t *testing.T) {
	c := NewClient("http://localhost:3333/", "", "proj")
	if c.apiURL != "http://localhost:3333" {
		t.Errorf("apiURL = %q, want trailing slash trimmed", c.apiURL)
	}
}

// =============================================================================
// Connect + Event Channel Tests
// =============================================================================

func TestClient_ConnectReturnsEventChannel(t *testing.T) {
	server := newSSETestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSEEvent(w, "connected", `{"type":"connected","projectId":"test-project"}`)
		<-r.Context().Done()
	})
	defer server.Close()

	c := NewClient(server.URL, "", "test-project")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch := c.Connect(ctx)
	if ch == nil {
		t.Fatal("expected non-nil channel from Connect")
	}

	select {
	case event := <-ch:
		if event.Type != "connected" {
			t.Errorf("expected type 'connected', got %q", event.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for connected event")
	}

	c.Close()
}

func TestClient_ReceivesTasksSnapshot(t *testing.T) {
	server := newSSETestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSEEvent(w, "connected", `{"type":"connected","projectId":"test-project"}`)
		writeSSEEvent(w, "tasks_snapshot", `{"type":"tasks_snapshot","tasks":[{"id":"t1"}],"count":1}`)
		<-r.Context().Done()
	})
	defer server.Close()

	c := NewClient(server.URL, "", "test-project")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch := c.Connect(ctx)

	// Collect 2 events
	var events []Event
	for len(events) < 2 {
		select {
		case event := <-ch:
			events = append(events, event)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out: got %d events, want 2", len(events))
		}
	}

	if events[0].Type != "connected" {
		t.Errorf("first event type = %q, want 'connected'", events[0].Type)
	}
	if events[1].Type != "tasks_snapshot" {
		t.Errorf("second event type = %q, want 'tasks_snapshot'", events[1].Type)
	}

	c.Close()
}

func TestClient_HeartbeatIsSkipped(t *testing.T) {
	server := newSSETestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSEEvent(w, "connected", `{"type":"connected","projectId":"test-project"}`)
		writeSSEEvent(w, "heartbeat", `{"type":"heartbeat"}`)
		writeSSEEvent(w, "tasks_snapshot", `{"type":"tasks_snapshot","tasks":[],"count":0}`)
		<-r.Context().Done()
	})
	defer server.Close()

	c := NewClient(server.URL, "", "test-project")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch := c.Connect(ctx)

	// Should get connected + tasks_snapshot (heartbeat skipped)
	var events []Event
	for len(events) < 2 {
		select {
		case event := <-ch:
			events = append(events, event)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out: got %d events, want 2", len(events))
		}
	}

	if events[0].Type != "connected" {
		t.Errorf("first event = %q, want 'connected'", events[0].Type)
	}
	if events[1].Type != "tasks_snapshot" {
		t.Errorf("second event = %q, want 'tasks_snapshot' (heartbeat should be skipped)", events[1].Type)
	}

	c.Close()
}

func TestClient_DisconnectOnServerClose(t *testing.T) {
	server := newSSETestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSEEvent(w, "connected", `{"type":"connected","projectId":"test-project"}`)
		// Server closes immediately after connected event
	})
	defer server.Close()

	c := NewClient(server.URL, "", "test-project")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch := c.Connect(ctx)

	// Should get connected, then a disconnect event
	var events []Event
	for len(events) < 2 {
		select {
		case event, ok := <-ch:
			if !ok {
				// Channel closed — that's also acceptable for disconnect
				goto done
			}
			events = append(events, event)
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out: got %d events, want 2", len(events))
		}
	}
done:

	if len(events) < 1 {
		t.Fatal("expected at least 1 event")
	}
	if events[0].Type != "connected" {
		t.Errorf("first event = %q, want 'connected'", events[0].Type)
	}

	// Second event should be disconnect
	if len(events) >= 2 {
		if events[1].Type != "disconnected" {
			t.Errorf("second event = %q, want 'disconnected'", events[1].Type)
		}
	}
}

func TestClient_ConnectionRefused(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "", "test-project")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch := c.Connect(ctx)

	select {
	case event, ok := <-ch:
		if !ok {
			// Channel closed — acceptable
			return
		}
		if event.Type != "disconnected" {
			t.Errorf("expected 'disconnected' event on connection refused, got %q", event.Type)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for disconnect event")
	}
}

func TestClient_ContextCancellation(t *testing.T) {
	server := newSSETestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Headers already set by newSSETestServer wrapper
		<-r.Context().Done()
	})
	defer server.Close()

	c := NewClient(server.URL, "", "test-project")
	ctx, cancel := context.WithCancel(context.Background())

	ch := c.Connect(ctx)

	// Cancel context after a short delay
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Channel should eventually close or we should get no more events
	select {
	case <-ch:
		// Channel closed or event received — both acceptable
	case <-time.After(3 * time.Second):
		t.Fatal("channel not closed after context cancellation")
	}
}

func TestClient_Close_StopsConnection(t *testing.T) {
	server := newSSETestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSEEvent(w, "connected", `{"type":"connected","projectId":"test-project"}`)
		<-r.Context().Done()
	})
	defer server.Close()

	c := NewClient(server.URL, "", "test-project")
	ctx := context.Background()

	ch := c.Connect(ctx)

	// Wait for connected event
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for connected event")
	}

	// Close should stop the connection
	c.Close()

	// Channel should eventually close
	select {
	case <-ch:
		// Closed, or a disconnect event — both acceptable
	case <-time.After(3 * time.Second):
		t.Fatal("channel not closed after Close()")
	}
}

func TestClient_Connected_ReflectsState(t *testing.T) {
	server := newSSETestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSEEvent(w, "connected", `{"type":"connected","projectId":"test-project"}`)
		<-r.Context().Done()
	})
	defer server.Close()

	c := NewClient(server.URL, "", "test-project")

	// Before connect
	if c.Connected() {
		t.Error("should not be connected before Connect()")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch := c.Connect(ctx)

	// Wait for connected event
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for connected event")
	}

	// After receiving connected event, should be connected
	if !c.Connected() {
		t.Error("should be connected after receiving connected event")
	}

	c.Close()

	// After close, should not be connected
	// Give a moment for the goroutine to process
	time.Sleep(100 * time.Millisecond)
	if c.Connected() {
		t.Error("should not be connected after Close()")
	}
}

func TestClient_SetsCorrectHeaders(t *testing.T) {
	var receivedHeaders http.Header
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedHeaders = r.Header.Clone()
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Close immediately
	}))
	defer server.Close()

	c := NewClient(server.URL, "my-secret-token", "test-project")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch := c.Connect(ctx)

	// Wait for the request to arrive
	time.Sleep(300 * time.Millisecond)

	// Drain channel
	go func() {
		for range ch {
		}
	}()

	mu.Lock()
	headers := receivedHeaders
	mu.Unlock()

	if headers == nil {
		t.Fatal("no request received")
	}

	accept := headers.Get("Accept")
	if accept != "text/event-stream" {
		t.Errorf("Accept = %q, want 'text/event-stream'", accept)
	}

	cacheControl := headers.Get("Cache-Control")
	if cacheControl != "no-cache" {
		t.Errorf("Cache-Control = %q, want 'no-cache'", cacheControl)
	}

	auth := headers.Get("Authorization")
	if auth != "Bearer my-secret-token" {
		t.Errorf("Authorization = %q, want 'Bearer my-secret-token'", auth)
	}
}

func TestClient_NoAuthHeaderWhenNoToken(t *testing.T) {
	var receivedHeaders http.Header
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedHeaders = r.Header.Clone()
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewClient(server.URL, "", "test-project")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch := c.Connect(ctx)
	time.Sleep(300 * time.Millisecond)

	go func() {
		for range ch {
		}
	}()

	mu.Lock()
	headers := receivedHeaders
	mu.Unlock()

	if headers == nil {
		t.Fatal("no request received")
	}

	auth := headers.Get("Authorization")
	if auth != "" {
		t.Errorf("Authorization should be empty when no token, got %q", auth)
	}
}

func TestClient_StreamURL(t *testing.T) {
	c := NewClient("http://localhost:3333", "", "my-project")
	url := c.streamURL()
	expected := "http://localhost:3333/api/v1/tasks/my-project/stream"
	if url != expected {
		t.Errorf("streamURL() = %q, want %q", url, expected)
	}
}

func TestNewClientWithURL(t *testing.T) {
	c := NewClientWithURL("http://localhost:3333", "token", "http://localhost:3333/api/v1/runners/runner_abc/stream")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	// Direct URL mode: streamURL returns the full URL
	url := c.streamURL()
	expected := "http://localhost:3333/api/v1/runners/runner_abc/stream"
	if url != expected {
		t.Errorf("streamURL() = %q, want %q", url, expected)
	}
}

func TestNewClientWithURL_ConnectsToExplicitURL(t *testing.T) {
	server := newSSETestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSEEvent(w, "connected", `{"type":"connected","runnerId":"runner_abc"}`)
		<-r.Context().Done()
	})
	defer server.Close()

	// Use the server URL directly via NewClientWithURL
	c := NewClientWithURL("", "", server.URL+"/api/v1/tasks/ignored/stream")
	// Override to just use server.URL (the test server handles any path)
	c2 := NewClientWithURL("", "", server.URL+"/")
	_ = c

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch := c2.Connect(ctx)

	select {
	case event := <-ch:
		if event.Type != "connected" {
			t.Errorf("expected type 'connected', got %q", event.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for connected event from NewClientWithURL")
	}

	c2.Close()
}

// =============================================================================
// Test Helpers
// =============================================================================

func newSSETestServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		handler(w, r)
	}))
}

func writeSSEEvent(w http.ResponseWriter, eventType, data string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
	flusher.Flush()
}

// Suppress unused import warning
var _ = json.Marshal

// =============================================================================
// Reconnect Tests
//
// A Client outlives its connections: the runner's SSE listeners construct one
// client and call Connect in a loop forever. These pin that contract. The
// client was single-use until 2026-09-03 — the event channel was allocated in
// the constructor and closed on the first disconnect — so a runner that lost
// its stream once went permanently deaf to dispatch commands while still
// opening real sockets the server counted as delivered subscribers.
// =============================================================================

func TestClient_ReconnectDeliversEventsOnNewChannel(t *testing.T) {
	var conns int32
	var mu sync.Mutex
	server := newSSETestServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		conns++
		n := conns
		mu.Unlock()

		writeSSEEvent(w, "connected", `{"type":"connected"}`)
		if n == 1 {
			// First connection drops, the way an API restart or a proxy
			// blip drops it.
			return
		}
		writeSSEEvent(w, "command", `{"command":"dispatch"}`)
		<-r.Context().Done()
	})
	defer server.Close()

	c := NewClient(server.URL, "", "test-project")
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Drain the first generation until the server hangs up and it closes.
	for range c.Connect(ctx) {
	}

	// Reconnect. Before the fix this handed back the SAME already-closed
	// channel, so the range below returned instantly and every event the
	// client went on to read off the wire was discarded.
	second := c.Connect(ctx)

	deadline := time.After(5 * time.Second)
	for {
		select {
		case event, ok := <-second:
			if !ok {
				t.Fatal("second Connect returned a closed channel: the client is single-use again")
			}
			if event.Type == "command" {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for an event on the reconnected stream")
		}
	}
}

func TestClient_ReconnectRetiresPreviousConnection(t *testing.T) {
	released := make(chan struct{}, 4)
	server := newSSETestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSEEvent(w, "connected", `{"type":"connected"}`)
		<-r.Context().Done() // hold it open until the client lets go
		released <- struct{}{}
	})
	defer server.Close()

	c := NewClient(server.URL, "", "test-project")
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	awaitConnected(t, c.Connect(ctx))

	// The second Connect must cancel the first connection. Without that its
	// listen goroutine stays parked in scanner.Scan() holding the socket for
	// the life of the process, and every reconnect leaks another one.
	awaitConnected(t, c.Connect(ctx))

	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("previous connection was not retired by the second Connect")
	}
}

// awaitConnected reads until the stream reports "connected".
func awaitConnected(t *testing.T, ch <-chan Event) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				t.Fatal("channel closed before a connected event arrived")
			}
			if event.Type == "connected" {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for the connected event")
		}
	}
}

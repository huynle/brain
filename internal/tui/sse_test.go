package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// SSE Line Parsing Tests
// =============================================================================

func TestParseSSEEvent_ConnectedEvent(t *testing.T) {
	data := types.SSEConnectedData{
		SSEEventData: types.SSEEventData{
			Type:      types.SSEEventConnected,
			Transport: "sse",
			Timestamp: "2025-01-01T00:00:00Z",
			ProjectID: "test-project",
		},
	}
	jsonData, _ := json.Marshal(data)

	lines := []string{
		"event: connected",
		fmt.Sprintf("data: %s", string(jsonData)),
		"",
	}

	msg, err := parseSSEEvent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := msg.(SSEConnectedMsg); !ok {
		t.Errorf("expected SSEConnectedMsg, got %T", msg)
	}
}

func TestParseSSEEvent_TasksSnapshotEvent(t *testing.T) {
	tasks := []types.ResolvedTask{
		{
			ID:             "abc12345",
			Title:          "Test Task",
			Priority:       "high",
			Status:         "pending",
			Classification: "ready",
		},
		{
			ID:             "def67890",
			Title:          "Another Task",
			Priority:       "medium",
			Status:         "pending",
			Classification: "waiting",
		},
	}
	stats := &types.TaskStats{
		Total:      2,
		Ready:      1,
		Waiting:    1,
		Blocked:    0,
		NotPending: 0,
	}
	data := types.SSETasksSnapshotData{
		SSEEventData: types.SSEEventData{
			Type:      types.SSEEventTasksSnapshot,
			Transport: "sse",
			Timestamp: "2025-01-01T00:00:00Z",
			ProjectID: "test-project",
		},
		Tasks:  tasks,
		Count:  2,
		Stats:  stats,
		Cycles: nil,
	}
	jsonData, _ := json.Marshal(data)

	lines := []string{
		"event: tasks_snapshot",
		fmt.Sprintf("data: %s", string(jsonData)),
		"",
	}

	msg, err := parseSSEEvent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tasksMsg, ok := msg.(TasksUpdatedMsg)
	if !ok {
		t.Fatalf("expected TasksUpdatedMsg, got %T", msg)
	}

	if len(tasksMsg.Tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasksMsg.Tasks))
	}
	if tasksMsg.Tasks[0].ID != "abc12345" {
		t.Errorf("expected first task ID 'abc12345', got '%s'", tasksMsg.Tasks[0].ID)
	}
	if tasksMsg.Stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if tasksMsg.Stats.Ready != 1 {
		t.Errorf("expected 1 ready, got %d", tasksMsg.Stats.Ready)
	}
}

func TestParseSSEEvent_HeartbeatReturnsNil(t *testing.T) {
	data := types.SSEEventData{
		Type:      types.SSEEventHeartbeat,
		Transport: "sse",
		Timestamp: "2025-01-01T00:00:00Z",
		ProjectID: "test-project",
	}
	jsonData, _ := json.Marshal(data)

	lines := []string{
		"event: heartbeat",
		fmt.Sprintf("data: %s", string(jsonData)),
		"",
	}

	msg, err := parseSSEEvent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Heartbeat should return nil (ignored)
	if msg != nil {
		t.Errorf("expected nil for heartbeat, got %T", msg)
	}
}

func TestParseSSEEvent_ErrorEvent(t *testing.T) {
	data := types.SSEErrorData{
		SSEEventData: types.SSEEventData{
			Type:      types.SSEEventError,
			Transport: "sse",
			Timestamp: "2025-01-01T00:00:00Z",
			ProjectID: "test-project",
		},
		Message: "something went wrong",
	}
	jsonData, _ := json.Marshal(data)

	lines := []string{
		"event: error",
		fmt.Sprintf("data: %s", string(jsonData)),
		"",
	}

	msg, err := parseSSEEvent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	errMsg, ok := msg.(SSEErrorMsg)
	if !ok {
		t.Fatalf("expected SSEErrorMsg, got %T", msg)
	}
	if errMsg.Err == nil {
		t.Fatal("expected non-nil error")
	}
	if !strings.Contains(errMsg.Err.Error(), "something went wrong") {
		t.Errorf("expected error to contain 'something went wrong', got '%s'", errMsg.Err.Error())
	}
}

func TestParseSSEEvent_UnknownEventReturnsNil(t *testing.T) {
	lines := []string{
		"event: unknown_event_type",
		"data: {}",
		"",
	}

	msg, err := parseSSEEvent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg != nil {
		t.Errorf("expected nil for unknown event, got %T", msg)
	}
}

func TestParseSSEEvent_MissingEventLine(t *testing.T) {
	lines := []string{
		"data: {}",
		"",
	}

	msg, err := parseSSEEvent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg != nil {
		t.Errorf("expected nil for missing event line, got %T", msg)
	}
}

func TestParseSSEEvent_MissingDataLine(t *testing.T) {
	lines := []string{
		"event: connected",
		"",
	}

	msg, err := parseSSEEvent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg != nil {
		t.Errorf("expected nil for missing data line, got %T", msg)
	}
}

func TestParseSSEEvent_InvalidJSON(t *testing.T) {
	lines := []string{
		"event: tasks_snapshot",
		"data: {invalid json",
		"",
	}

	_, err := parseSSEEvent(lines)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

// =============================================================================
// SSEClient Constructor Tests
// =============================================================================

func TestNewSSEClient(t *testing.T) {
	client := NewSSEClient("http://localhost:3333", "test-project")

	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.apiURL != "http://localhost:3333" {
		t.Errorf("expected apiURL 'http://localhost:3333', got '%s'", client.apiURL)
	}
	if client.projectID != "test-project" {
		t.Errorf("expected projectID 'test-project', got '%s'", client.projectID)
	}
}

func TestSSEClient_StreamURL(t *testing.T) {
	client := NewSSEClient("http://localhost:3333", "my-project")
	url := client.streamURL()

	expected := "http://localhost:3333/api/v1/tasks/my-project/stream"
	if url != expected {
		t.Errorf("expected URL '%s', got '%s'", expected, url)
	}
}

func TestSSEClient_StreamURL_TrailingSlash(t *testing.T) {
	client := NewSSEClient("http://localhost:3333/", "my-project")
	url := client.streamURL()

	expected := "http://localhost:3333/api/v1/tasks/my-project/stream"
	if url != expected {
		t.Errorf("expected URL '%s', got '%s'", expected, url)
	}
}

// =============================================================================
// SSE HTTP Integration Tests (with httptest)
// =============================================================================

func TestSSEClient_ConnectsAndReceivesConnectedEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		data := types.SSEConnectedData{
			SSEEventData: types.SSEEventData{
				Type:      types.SSEEventConnected,
				Transport: "sse",
				Timestamp: "2025-01-01T00:00:00Z",
				ProjectID: "test-project",
			},
		}
		jsonData, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: connected\ndata: %s\n\n", jsonData)
		flusher.Flush()

		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewSSEClient(server.URL, "test-project")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Connect returns a tea.Cmd that yields the first message
	cmd := client.Connect(ctx)
	if cmd == nil {
		t.Fatal("expected non-nil command from Connect")
	}

	msg := cmd()
	if _, ok := msg.(SSEConnectedMsg); !ok {
		t.Errorf("expected SSEConnectedMsg, got %T", msg)
	}
}

func TestSSEClient_ReceivesTasksSnapshot(t *testing.T) {
	tasks := []types.ResolvedTask{
		{
			ID:             "task1",
			Title:          "First Task",
			Priority:       "high",
			Status:         "pending",
			Classification: "ready",
		},
	}
	stats := &types.TaskStats{
		Total:      1,
		Ready:      1,
		Waiting:    0,
		Blocked:    0,
		NotPending: 0,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send connected event first
		connData := types.SSEConnectedData{
			SSEEventData: types.SSEEventData{
				Type:      types.SSEEventConnected,
				Transport: "sse",
				Timestamp: "2025-01-01T00:00:00Z",
				ProjectID: "test-project",
			},
		}
		connJSON, _ := json.Marshal(connData)
		fmt.Fprintf(w, "event: connected\ndata: %s\n\n", connJSON)
		flusher.Flush()

		// Send tasks_snapshot
		snapData := types.SSETasksSnapshotData{
			SSEEventData: types.SSEEventData{
				Type:      types.SSEEventTasksSnapshot,
				Transport: "sse",
				Timestamp: "2025-01-01T00:00:01Z",
				ProjectID: "test-project",
			},
			Tasks:  tasks,
			Count:  1,
			Stats:  stats,
			Cycles: nil,
		}
		snapJSON, _ := json.Marshal(snapData)
		fmt.Fprintf(w, "event: tasks_snapshot\ndata: %s\n\n", snapJSON)
		flusher.Flush()

		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewSSEClient(server.URL, "test-project")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Use listenSSE to collect multiple messages
	msgCh := make(chan tea.Msg, 10)
	go client.listenSSE(ctx, msgCh)

	var msgs []tea.Msg
	timeout := time.After(2 * time.Second)
	for len(msgs) < 2 {
		select {
		case msg := <-msgCh:
			msgs = append(msgs, msg)
		case <-timeout:
			t.Fatalf("timed out: expected 2 messages, got %d", len(msgs))
		}
	}

	// First message should be SSEConnectedMsg
	if _, ok := msgs[0].(SSEConnectedMsg); !ok {
		t.Errorf("expected first message SSEConnectedMsg, got %T", msgs[0])
	}

	// Second message should be TasksUpdatedMsg
	tasksMsg, ok := msgs[1].(TasksUpdatedMsg)
	if !ok {
		t.Fatalf("expected second message TasksUpdatedMsg, got %T", msgs[1])
	}
	if len(tasksMsg.Tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasksMsg.Tasks))
	}
	if tasksMsg.Tasks[0].ID != "task1" {
		t.Errorf("expected task ID 'task1', got '%s'", tasksMsg.Tasks[0].ID)
	}
	if tasksMsg.Stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if tasksMsg.Stats.Ready != 1 {
		t.Errorf("expected 1 ready, got %d", tasksMsg.Stats.Ready)
	}
}

func TestSSEClient_HeartbeatIsSkipped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send connected
		connData := types.SSEConnectedData{
			SSEEventData: types.SSEEventData{
				Type:      types.SSEEventConnected,
				Transport: "sse",
				Timestamp: "2025-01-01T00:00:00Z",
				ProjectID: "test-project",
			},
		}
		connJSON, _ := json.Marshal(connData)
		fmt.Fprintf(w, "event: connected\ndata: %s\n\n", connJSON)
		flusher.Flush()

		// Send heartbeat (should be ignored)
		hbData := types.SSEEventData{
			Type:      types.SSEEventHeartbeat,
			Transport: "sse",
			Timestamp: "2025-01-01T00:00:01Z",
			ProjectID: "test-project",
		}
		hbJSON, _ := json.Marshal(hbData)
		fmt.Fprintf(w, "event: heartbeat\ndata: %s\n\n", hbJSON)
		flusher.Flush()

		// Send tasks_snapshot after heartbeat
		snapData := types.SSETasksSnapshotData{
			SSEEventData: types.SSEEventData{
				Type:      types.SSEEventTasksSnapshot,
				Transport: "sse",
				Timestamp: "2025-01-01T00:00:02Z",
				ProjectID: "test-project",
			},
			Tasks: []types.ResolvedTask{
				{ID: "t1", Title: "Task", Classification: "ready"},
			},
			Count: 1,
			Stats: &types.TaskStats{Ready: 1, Total: 1},
		}
		snapJSON, _ := json.Marshal(snapData)
		fmt.Fprintf(w, "event: tasks_snapshot\ndata: %s\n\n", snapJSON)
		flusher.Flush()

		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewSSEClient(server.URL, "test-project")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	msgCh := make(chan tea.Msg, 10)
	go client.listenSSE(ctx, msgCh)

	// Collect 2 messages - heartbeat should NOT appear
	var msgs []tea.Msg
	timeout := time.After(2 * time.Second)
	for len(msgs) < 2 {
		select {
		case msg := <-msgCh:
			msgs = append(msgs, msg)
		case <-timeout:
			t.Fatalf("timed out: expected 2 messages, got %d", len(msgs))
		}
	}

	// Should get connected + tasks_snapshot (heartbeat skipped)
	if _, ok := msgs[0].(SSEConnectedMsg); !ok {
		t.Errorf("expected SSEConnectedMsg, got %T", msgs[0])
	}
	if _, ok := msgs[1].(TasksUpdatedMsg); !ok {
		t.Errorf("expected TasksUpdatedMsg, got %T", msgs[1])
	}
}

func TestSSEClient_ErrorEventProducesSSEErrorMsg(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		errData := types.SSEErrorData{
			SSEEventData: types.SSEEventData{
				Type:      types.SSEEventError,
				Transport: "sse",
				Timestamp: "2025-01-01T00:00:00Z",
				ProjectID: "test-project",
			},
			Message: "server error occurred",
		}
		errJSON, _ := json.Marshal(errData)
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", errJSON)
		flusher.Flush()

		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewSSEClient(server.URL, "test-project")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	msgCh := make(chan tea.Msg, 10)
	go client.listenSSE(ctx, msgCh)

	select {
	case msg := <-msgCh:
		errMsg, ok := msg.(SSEErrorMsg)
		if !ok {
			t.Fatalf("expected SSEErrorMsg, got %T", msg)
		}
		if !strings.Contains(errMsg.Err.Error(), "server error occurred") {
			t.Errorf("expected error to contain 'server error occurred', got '%s'", errMsg.Err.Error())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSEErrorMsg")
	}
}

func TestSSEClient_DisconnectOnServerClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		connData := types.SSEConnectedData{
			SSEEventData: types.SSEEventData{
				Type:      types.SSEEventConnected,
				Transport: "sse",
				Timestamp: "2025-01-01T00:00:00Z",
				ProjectID: "test-project",
			},
		}
		connJSON, _ := json.Marshal(connData)
		fmt.Fprintf(w, "event: connected\ndata: %s\n\n", connJSON)
		flusher.Flush()

		// Close immediately (simulates server disconnect)
	}))
	defer server.Close()

	client := NewSSEClient(server.URL, "test-project")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	msgCh := make(chan tea.Msg, 10)
	go client.listenSSE(ctx, msgCh)

	var msgs []tea.Msg
	timeout := time.After(3 * time.Second)
	for len(msgs) < 2 {
		select {
		case msg := <-msgCh:
			msgs = append(msgs, msg)
		case <-timeout:
			t.Fatalf("timed out: expected 2 messages, got %d", len(msgs))
		}
	}

	if _, ok := msgs[0].(SSEConnectedMsg); !ok {
		t.Errorf("expected SSEConnectedMsg, got %T", msgs[0])
	}
	if _, ok := msgs[1].(SSEDisconnectedMsg); !ok {
		t.Errorf("expected SSEDisconnectedMsg, got %T", msgs[1])
	}
}

func TestSSEClient_ConnectionRefused(t *testing.T) {
	client := NewSSEClient("http://127.0.0.1:1", "test-project")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msgCh := make(chan tea.Msg, 10)
	go client.listenSSE(ctx, msgCh)

	select {
	case msg := <-msgCh:
		if _, ok := msg.(SSEDisconnectedMsg); !ok {
			t.Errorf("expected SSEDisconnectedMsg on connection refused, got %T", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for disconnect message")
	}
}

func TestSSEClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewSSEClient(server.URL, "test-project")
	ctx, cancel := context.WithCancel(context.Background())

	msgCh := make(chan tea.Msg, 10)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		client.listenSSE(ctx, msgCh)
	}()

	// Cancel context after a short delay
	time.Sleep(100 * time.Millisecond)
	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Good - goroutine exited
	case <-time.After(3 * time.Second):
		t.Fatal("listenSSE did not exit after context cancellation")
	}
}

// =============================================================================
// Reconnect Tests
// =============================================================================

func TestSSEClient_ReconnectReturnsCmd(t *testing.T) {
	client := NewSSEClient("http://localhost:3333", "test-project")
	cmd := client.Reconnect(100 * time.Millisecond)

	if cmd == nil {
		t.Fatal("expected non-nil command from Reconnect")
	}

	done := make(chan tea.Msg, 1)
	go func() {
		done <- cmd()
	}()

	select {
	case msg := <-done:
		if _, ok := msg.(reconnectMsg); !ok {
			t.Errorf("expected reconnectMsg, got %T", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for reconnect command")
	}
}

// =============================================================================
// SSE Request Header Tests
// =============================================================================

func TestSSEClient_SetsCorrectHeaders(t *testing.T) {
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

	client := NewSSEClient(server.URL, "test-project")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	msgCh := make(chan tea.Msg, 10)
	go client.listenSSE(ctx, msgCh)

	// Wait for the request to arrive
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	headers := receivedHeaders
	mu.Unlock()

	if headers == nil {
		t.Fatal("no request received")
	}

	accept := headers.Get("Accept")
	if accept != "text/event-stream" {
		t.Errorf("expected Accept header 'text/event-stream', got '%s'", accept)
	}

	cacheControl := headers.Get("Cache-Control")
	if cacheControl != "no-cache" {
		t.Errorf("expected Cache-Control header 'no-cache', got '%s'", cacheControl)
	}
}

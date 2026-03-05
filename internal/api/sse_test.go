package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

func newSSETestRouter(taskMock *mockTaskService, hub *realtime.Hub) *chi.Mux {
	h := NewHandler(
		&mockBrainService{},
		WithTaskService(taskMock),
		WithHub(hub),
	)
	r := chi.NewRouter()
	r.Get("/tasks/{projectId}/stream", h.HandleSSEStream)
	return r
}

// parseSSEEvents reads SSE events from a response body until context is cancelled.
func parseSSEEvents(t *testing.T, resp *http.Response, count int, timeout time.Duration) []sseEvent {
	t.Helper()
	var events []sseEvent
	scanner := bufio.NewScanner(resp.Body)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		var currentEvent sseEvent
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				currentEvent.Event = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				currentEvent.Data = strings.TrimPrefix(line, "data: ")
			} else if line == "" && currentEvent.Event != "" {
				events = append(events, currentEvent)
				currentEvent = sseEvent{}
				if len(events) >= count {
					return
				}
			}
		}
	}()

	select {
	case <-done:
	case <-ctx.Done():
	}

	return events
}

type sseEvent struct {
	Event string
	Data  string
}

func TestSSEConnectedEvent(t *testing.T) {
	hub := realtime.NewHub()
	taskMock := &mockTaskService{
		getTasksFunc: func(ctx context.Context, projectId string) (*types.TaskListResponse, error) {
			return &types.TaskListResponse{
				Tasks:  []types.ResolvedTask{},
				Count:  0,
				Stats:  &types.TaskStats{},
				Cycles: [][]string{},
			}, nil
		},
	}
	router := newSSETestRouter(taskMock, hub)
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/tasks/my-project/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Check SSE headers
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if resp.Header.Get("Cache-Control") != "no-cache, no-transform" {
		t.Errorf("Cache-Control = %q, want %q", resp.Header.Get("Cache-Control"), "no-cache, no-transform")
	}

	// Read first two events: connected + tasks_snapshot
	events := parseSSEEvents(t, resp, 2, 2*time.Second)
	if len(events) < 1 {
		t.Fatal("expected at least 1 SSE event")
	}

	// First event should be "connected"
	if events[0].Event != "connected" {
		t.Errorf("event[0] = %q, want %q", events[0].Event, "connected")
	}

	// Parse connected data
	var connData types.SSEConnectedData
	if err := json.Unmarshal([]byte(events[0].Data), &connData); err != nil {
		t.Fatalf("failed to parse connected data: %v", err)
	}
	if connData.ProjectID != "my-project" {
		t.Errorf("projectId = %q, want %q", connData.ProjectID, "my-project")
	}
	if connData.Transport != "sse" {
		t.Errorf("transport = %q, want %q", connData.Transport, "sse")
	}
}

func TestSSETasksSnapshot(t *testing.T) {
	hub := realtime.NewHub()
	taskMock := &mockTaskService{
		getTasksFunc: func(ctx context.Context, projectId string) (*types.TaskListResponse, error) {
			return &types.TaskListResponse{
				Tasks: []types.ResolvedTask{
					{ID: "task1", Title: "Test Task"},
				},
				Count:  1,
				Stats:  &types.TaskStats{Total: 1, Ready: 1},
				Cycles: [][]string{},
			}, nil
		},
	}
	router := newSSETestRouter(taskMock, hub)
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/tasks/my-project/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read connected + tasks_snapshot
	events := parseSSEEvents(t, resp, 2, 2*time.Second)
	if len(events) < 2 {
		t.Fatalf("expected at least 2 SSE events, got %d", len(events))
	}

	// Second event should be "tasks_snapshot"
	if events[1].Event != "tasks_snapshot" {
		t.Errorf("event[1] = %q, want %q", events[1].Event, "tasks_snapshot")
	}

	var snapshot types.SSETasksSnapshotData
	if err := json.Unmarshal([]byte(events[1].Data), &snapshot); err != nil {
		t.Fatalf("failed to parse snapshot data: %v", err)
	}
	if snapshot.Count != 1 {
		t.Errorf("count = %d, want 1", snapshot.Count)
	}
}

func TestSSEHeartbeat(t *testing.T) {
	// Use a very short heartbeat interval for testing
	origInterval := DefaultHeartbeatInterval
	DefaultHeartbeatInterval = 100 * time.Millisecond
	defer func() { DefaultHeartbeatInterval = origInterval }()

	hub := realtime.NewHub()
	taskMock := &mockTaskService{
		getTasksFunc: func(ctx context.Context, projectId string) (*types.TaskListResponse, error) {
			return &types.TaskListResponse{
				Tasks:  []types.ResolvedTask{},
				Count:  0,
				Stats:  &types.TaskStats{},
				Cycles: [][]string{},
			}, nil
		},
	}
	router := newSSETestRouter(taskMock, hub)
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/tasks/my-project/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read connected + tasks_snapshot + heartbeat = 3 events
	events := parseSSEEvents(t, resp, 3, 2*time.Second)
	if len(events) < 3 {
		t.Fatalf("expected at least 3 SSE events, got %d", len(events))
	}

	// Third event should be "heartbeat"
	if events[2].Event != "heartbeat" {
		t.Errorf("event[2] = %q, want %q", events[2].Event, "heartbeat")
	}
}

func TestSSEHubMessage(t *testing.T) {
	hub := realtime.NewHub()
	taskMock := &mockTaskService{
		getTasksFunc: func(ctx context.Context, projectId string) (*types.TaskListResponse, error) {
			return &types.TaskListResponse{
				Tasks:  []types.ResolvedTask{},
				Count:  0,
				Stats:  &types.TaskStats{},
				Cycles: [][]string{},
			}, nil
		},
	}
	router := newSSETestRouter(taskMock, hub)
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/tasks/my-project/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read connected + tasks_snapshot first
	events := parseSSEEvents(t, resp, 2, 2*time.Second)
	if len(events) < 2 {
		t.Fatalf("expected at least 2 initial events, got %d", len(events))
	}

	// Now publish a project_dirty event through the hub
	hub.PublishProjectDirty("my-project")

	// Read the next event
	events = parseSSEEvents(t, resp, 1, 2*time.Second)
	if len(events) < 1 {
		t.Fatal("expected to receive hub message")
	}

	if events[0].Event != "project_dirty" {
		t.Errorf("event = %q, want %q", events[0].Event, "project_dirty")
	}
}

func TestSSEClientDisconnect(t *testing.T) {
	hub := realtime.NewHub()
	taskMock := &mockTaskService{
		getTasksFunc: func(ctx context.Context, projectId string) (*types.TaskListResponse, error) {
			return &types.TaskListResponse{
				Tasks:  []types.ResolvedTask{},
				Count:  0,
				Stats:  &types.TaskStats{},
				Cycles: [][]string{},
			}, nil
		},
	}
	router := newSSETestRouter(taskMock, hub)
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/tasks/my-project/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	// Read initial events
	_ = parseSSEEvents(t, resp, 2, 2*time.Second)

	// Cancel context to simulate client disconnect
	cancel()
	resp.Body.Close()

	// Publishing after disconnect should not panic
	hub.PublishProjectDirty("my-project")

	// Give time for cleanup
	time.Sleep(50 * time.Millisecond)

	// If we get here without panic, the test passes
}

func TestSSEGetTasksError(t *testing.T) {
	hub := realtime.NewHub()
	taskMock := &mockTaskService{
		getTasksFunc: func(ctx context.Context, projectId string) (*types.TaskListResponse, error) {
			return nil, fmt.Errorf("database error")
		},
	}
	router := newSSETestRouter(taskMock, hub)
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/tasks/my-project/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Should still get connected event even if GetTasks fails
	events := parseSSEEvents(t, resp, 1, 2*time.Second)
	if len(events) < 1 {
		t.Fatal("expected at least 1 SSE event")
	}
	if events[0].Event != "connected" {
		t.Errorf("event[0] = %q, want %q", events[0].Event, "connected")
	}
}

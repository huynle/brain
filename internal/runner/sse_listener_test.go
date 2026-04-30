package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// =============================================================================
// SSEListener Tests
// =============================================================================

func TestNewSSEListener(t *testing.T) {
	wakeCh := make(chan struct{}, 1)
	listener := NewSSEListener("http://localhost:3333", "token", "runner-1", []string{"proj-a", "proj-b"}, wakeCh, nil)

	if listener == nil {
		t.Fatal("expected non-nil listener")
	}
}

func TestSSEListener_WakesOnTasksSnapshot(t *testing.T) {
	// Create a test SSE server that sends connected + tasks_snapshot
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		// Send connected
		connData, _ := json.Marshal(map[string]interface{}{
			"type":      "connected",
			"projectId": "proj-a",
		})
		fmt.Fprintf(w, "event: connected\ndata: %s\n\n", connData)
		flusher.Flush()

		// Send tasks_snapshot
		snapData, _ := json.Marshal(map[string]interface{}{
			"type":      "tasks_snapshot",
			"projectId": "proj-a",
			"tasks":     []interface{}{},
			"count":     0,
		})
		fmt.Fprintf(w, "event: tasks_snapshot\ndata: %s\n\n", snapData)
		flusher.Flush()

		<-r.Context().Done()
	}))
	defer server.Close()

	wakeCh := make(chan struct{}, 10)
	listener := NewSSEListener(server.URL, "", "", []string{"proj-a"}, wakeCh, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go listener.Start(ctx)

	// Should receive a wake signal from the tasks_snapshot event
	select {
	case <-wakeCh:
		// Good — received wake signal
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for wake signal from tasks_snapshot")
	}

	listener.Stop()
}

func TestSSEListener_NonBlockingWake(t *testing.T) {
	// Wake channel is buffered to 1 — multiple snapshots should not block
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		connData, _ := json.Marshal(map[string]interface{}{
			"type": "connected", "projectId": "proj-a",
		})
		fmt.Fprintf(w, "event: connected\ndata: %s\n\n", connData)
		flusher.Flush()

		// Send multiple snapshots rapidly
		for i := 0; i < 5; i++ {
			snapData, _ := json.Marshal(map[string]interface{}{
				"type": "tasks_snapshot", "projectId": "proj-a", "tasks": []interface{}{}, "count": 0,
			})
			fmt.Fprintf(w, "event: tasks_snapshot\ndata: %s\n\n", snapData)
			flusher.Flush()
		}

		<-r.Context().Done()
	}))
	defer server.Close()

	// Buffered to 1 — should not deadlock even with multiple snapshots
	wakeCh := make(chan struct{}, 1)
	listener := NewSSEListener(server.URL, "", "", []string{"proj-a"}, wakeCh, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go listener.Start(ctx)

	// Should get at least one wake signal
	select {
	case <-wakeCh:
		// Good
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for wake signal")
	}

	listener.Stop()
}

func TestSSEListener_HeartbeatDoesNotWake(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		connData, _ := json.Marshal(map[string]interface{}{
			"type": "connected", "projectId": "proj-a",
		})
		fmt.Fprintf(w, "event: connected\ndata: %s\n\n", connData)
		flusher.Flush()

		// Send only heartbeats
		for i := 0; i < 3; i++ {
			hbData, _ := json.Marshal(map[string]interface{}{
				"type": "heartbeat", "projectId": "proj-a",
			})
			fmt.Fprintf(w, "event: heartbeat\ndata: %s\n\n", hbData)
			flusher.Flush()
		}

		<-r.Context().Done()
	}))
	defer server.Close()

	wakeCh := make(chan struct{}, 10)
	listener := NewSSEListener(server.URL, "", "", []string{"proj-a"}, wakeCh, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	go listener.Start(ctx)

	// Should NOT receive a wake signal from heartbeats
	select {
	case <-wakeCh:
		t.Error("heartbeat should not trigger wake signal")
	case <-time.After(500 * time.Millisecond):
		// Good — no wake signal
	}

	listener.Stop()
}

func TestSSEListener_StopClosesConnections(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		<-r.Context().Done()
	}))
	defer server.Close()

	wakeCh := make(chan struct{}, 1)
	listener := NewSSEListener(server.URL, "", "", []string{"proj-a"}, wakeCh, nil)

	ctx := context.Background()
	go listener.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	// Stop should not hang
	done := make(chan struct{})
	go func() {
		listener.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Good
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() hung")
	}
}

// =============================================================================
// TaskRunner Wake Channel Integration Tests
// =============================================================================

func TestTaskRunner_HasWakeChannel(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	if tr.wakeCh == nil {
		t.Error("TaskRunner should have a wake channel")
	}
}

func TestTaskRunner_WakeChannelTriggersPoll(t *testing.T) {
	client := newMockClient()
	task := testTask("task1", "proj-a")
	client.nextTask["proj-a"] = task
	client.claimResult = ClaimResult{Success: true}

	executor := newMockExecutor()
	proc := newMockProcess(100)
	executor.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/test"}

	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	ctx, cancel := context.WithCancel(context.Background())

	// Start runner in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- tr.Start(ctx)
	}()

	// Wait for initial poll to complete
	time.Sleep(100 * time.Millisecond)

	// Clear the task so the initial poll doesn't pick it up again
	client.mu.Lock()
	delete(client.nextTask, "proj-a")
	client.mu.Unlock()

	// Set a new task
	newTask := testTask("task2", "proj-a")
	client.mu.Lock()
	client.nextTask["proj-a"] = newTask
	client.mu.Unlock()

	// Send wake signal
	select {
	case tr.wakeCh <- struct{}{}:
	default:
	}

	// Wait for the wake-triggered poll to execute
	time.Sleep(200 * time.Millisecond)

	cancel()
	<-errCh

	// Should have spawned task2 from the wake-triggered poll
	spawns := executor.getSpawnCalls()
	foundTask2 := false
	for _, s := range spawns {
		if s.TaskID == "task2" {
			foundTask2 = true
		}
	}
	if !foundTask2 {
		t.Error("wake channel should trigger a poll that picks up task2")
	}
}

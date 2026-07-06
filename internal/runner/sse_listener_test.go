package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/sse"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// SSEListener Tests
// =============================================================================

func TestNewSSEListener(t *testing.T) {
	wakeCh := make(chan struct{}, 1)
	listener := NewSSEListener("http://localhost:3333", "token", []string{"proj-a", "proj-b"}, wakeCh)

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
	listener := NewSSEListener(server.URL, "", []string{"proj-a"}, wakeCh)

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
	listener := NewSSEListener(server.URL, "", []string{"proj-a"}, wakeCh)

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
	listener := NewSSEListener(server.URL, "", []string{"proj-a"}, wakeCh)

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
	listener := NewSSEListener(server.URL, "", []string{"proj-a"}, wakeCh)

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

// =============================================================================
// Runner Command Channel Tests
// =============================================================================

func TestSSEListener_RunnerStream_CommandEvents(t *testing.T) {
	// SSE server that serves a runner stream with command events
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		// Send connected event
		connData, _ := json.Marshal(map[string]interface{}{
			"type":     "connected",
			"runnerId": "runner_test",
		})
		fmt.Fprintf(w, "event: connected\ndata: %s\n\n", connData)
		flusher.Flush()

		// Send a dispatch command
		cmdData, _ := json.Marshal(map[string]interface{}{
			"type":      "dispatch",
			"taskId":    "task-123",
			"projectId": "proj-a",
		})
		fmt.Fprintf(w, "event: command\ndata: %s\n\n", cmdData)
		flusher.Flush()

		// Send an affinity_updated command
		affinityData, _ := json.Marshal(map[string]interface{}{
			"type":       "affinity_updated",
			"featureIds": []string{"feature-a", "feature-b"},
		})
		fmt.Fprintf(w, "event: command\ndata: %s\n\n", affinityData)
		flusher.Flush()

		<-r.Context().Done()
	}))
	defer server.Close()

	wakeCh := make(chan struct{}, 10)
	commandCh := make(chan RunnerCommand, 10)

	listener := NewSSEListener(server.URL, "", nil, wakeCh)
	listener.SetRunnerStream("runner_test", commandCh)

	// Override the API URL so the runner stream URL resolves to our test server
	listener.mu.Lock()
	listener.apiURL = server.URL
	listener.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go listener.Start(ctx)

	// Collect commands
	var commands []RunnerCommand
	timeout := time.After(3 * time.Second)
	for len(commands) < 2 {
		select {
		case cmd := <-commandCh:
			commands = append(commands, cmd)
		case <-timeout:
			t.Fatalf("timed out waiting for commands, got %d", len(commands))
		}
	}

	listener.Stop()

	// Verify dispatch command
	if commands[0].Type != CommandDispatch {
		t.Errorf("first command type = %q, want %q", commands[0].Type, CommandDispatch)
	}
	if commands[0].TaskID != "task-123" {
		t.Errorf("dispatch taskId = %q, want %q", commands[0].TaskID, "task-123")
	}

	// Verify affinity_updated command
	if commands[1].Type != CommandAffinityUpdated {
		t.Errorf("second command type = %q, want %q", commands[1].Type, CommandAffinityUpdated)
	}
	if len(commands[1].FeatureIDs) != 2 {
		t.Errorf("affinity featureIds len = %d, want 2", len(commands[1].FeatureIDs))
	}
}

func TestSSEListener_RunnerStream_CommandEnvelope(t *testing.T) {
	wakeCh := make(chan struct{}, 10)
	commandCh := make(chan RunnerCommand, 10)
	listener := NewSSEListener("http://example.invalid", "", nil, wakeCh)
	listener.SetRunnerStream("runner_test", commandCh)

	data, err := json.Marshal(map[string]interface{}{
		"command": "dispatch",
		"payload": map[string]string{
			"taskId":    "task-123",
			"projectId": "proj-a",
		},
	})
	if err != nil {
		t.Fatalf("marshal command envelope: %v", err)
	}

	listener.handleCommandEvent(sse.Event{Data: data}, "runner_test")

	select {
	case cmd := <-commandCh:
		if cmd.Type != CommandDispatch {
			t.Fatalf("command type = %q, want %q", cmd.Type, CommandDispatch)
		}
		if cmd.TaskID != "task-123" || cmd.ProjectID != "proj-a" {
			t.Fatalf("command payload = task:%q project:%q", cmd.TaskID, cmd.ProjectID)
		}
	default:
		t.Fatal("expected command from envelope")
	}
}

func TestSSEListener_RunnerStream_CommandEnvelopePreservesLeaseObject(t *testing.T) {
	wakeCh := make(chan struct{}, 10)
	commandCh := make(chan RunnerCommand, 10)
	listener := NewSSEListener("http://example.invalid", "", nil, wakeCh)
	listener.SetRunnerStream("runner_test", commandCh)

	data, err := json.Marshal(map[string]interface{}{
		"command": "dispatch",
		"payload": map[string]interface{}{
			"taskId":    "task-123",
			"projectId": "proj-a",
			"lease": map[string]interface{}{
				"id":         "lease-abc",
				"expires_at": float64(time.Now().Add(time.Minute).UnixMilli()),
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal command envelope: %v", err)
	}

	listener.handleCommandEvent(sse.Event{Data: data}, "runner_test")

	select {
	case cmd := <-commandCh:
		if cmd.LeaseID != "lease-abc" {
			t.Fatalf("lease id = %q, want lease-abc", cmd.LeaseID)
		}
	default:
		t.Fatal("expected command from envelope")
	}
}

func TestSSEListener_RunnerStream_TasksChangedWakes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		connData, _ := json.Marshal(map[string]interface{}{"type": "connected"})
		fmt.Fprintf(w, "event: connected\ndata: %s\n\n", connData)
		flusher.Flush()

		// Send tasks_changed event
		tcData, _ := json.Marshal(map[string]interface{}{
			"type":      "tasks_changed",
			"projectId": "proj-a",
		})
		fmt.Fprintf(w, "event: tasks_changed\ndata: %s\n\n", tcData)
		flusher.Flush()

		<-r.Context().Done()
	}))
	defer server.Close()

	wakeCh := make(chan struct{}, 10)
	commandCh := make(chan RunnerCommand, 10)

	listener := NewSSEListener(server.URL, "", nil, wakeCh)
	listener.SetRunnerStream("runner_test", commandCh)
	listener.mu.Lock()
	listener.apiURL = server.URL
	listener.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go listener.Start(ctx)

	// Should receive wake signal from tasks_changed
	select {
	case <-wakeCh:
		// Good
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for wake signal from tasks_changed")
	}

	listener.Stop()
}

func TestSSEListener_SetRunnerStream(t *testing.T) {
	wakeCh := make(chan struct{}, 1)
	commandCh := make(chan RunnerCommand, 1)

	listener := NewSSEListener("http://localhost:3333", "", nil, wakeCh)

	// Initially no runner stream
	listener.mu.Lock()
	if listener.runnerID != "" {
		t.Error("runnerID should be empty before SetRunnerStream")
	}
	listener.mu.Unlock()

	// Set runner stream
	listener.SetRunnerStream("runner_abc", commandCh)

	listener.mu.Lock()
	if listener.runnerID != "runner_abc" {
		t.Errorf("runnerID = %q, want %q", listener.runnerID, "runner_abc")
	}
	if listener.commandCh == nil {
		t.Error("commandCh should not be nil after SetRunnerStream")
	}
	listener.mu.Unlock()
}

// =============================================================================
// HandleCommand Tests
// =============================================================================

func TestTaskRunner_HandleCommand_AffinityUpdated(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	cmd := RunnerCommand{
		Type:       CommandAffinityUpdated,
		FeatureIDs: []string{"feat-1", "feat-2"},
	}

	ctx := context.Background()
	tr.handleCommand(ctx, cmd)

	tr.mu.RLock()
	featureIDs := tr.config.FeatureIDs
	tr.mu.RUnlock()

	if len(featureIDs) != 2 {
		t.Fatalf("expected 2 feature IDs, got %d", len(featureIDs))
	}
	if featureIDs[0] != "feat-1" || featureIDs[1] != "feat-2" {
		t.Errorf("feature IDs = %v, want [feat-1, feat-2]", featureIDs)
	}
}

func TestTaskRunner_HandleCommand_ConfigUpdated_MaxParallel(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	maxP := 5
	cmd := RunnerCommand{
		Type:        CommandConfigUpdated,
		MaxParallel: &maxP,
	}

	ctx := context.Background()
	tr.handleCommand(ctx, cmd)

	if tr.getMaxParallel() != 5 {
		t.Errorf("maxParallel = %d, want 5", tr.getMaxParallel())
	}
}

func TestTaskRunner_HandleCommand_ConfigUpdated_Model(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	cmd := RunnerCommand{
		Type:  CommandConfigUpdated,
		Model: "claude-sonnet-4-20250514",
	}

	ctx := context.Background()
	tr.handleCommand(ctx, cmd)

	tr.mu.RLock()
	model := tr.config.Opencode.Model
	tr.mu.RUnlock()

	if model != "claude-sonnet-4-20250514" {
		t.Errorf("model = %q, want %q", model, "claude-sonnet-4-20250514")
	}
}

func TestTaskRunner_HandleCommand_ConfigUpdated_Agent(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	cmd := RunnerCommand{
		Type:  CommandConfigUpdated,
		Agent: "tdd-dev",
	}

	ctx := context.Background()
	tr.handleCommand(ctx, cmd)

	tr.mu.RLock()
	agent := tr.config.Opencode.Agent
	tr.mu.RUnlock()

	if agent != "tdd-dev" {
		t.Errorf("agent = %q, want %q", agent, "tdd-dev")
	}
}

func TestTaskRunner_HandleCommand_DispatchRejectsMissingLeaseID(t *testing.T) {
	client := newMockClient()
	tr := newTestRunner(client, newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	cmd := RunnerCommand{
		Type:      CommandDispatch,
		TaskID:    "task-99",
		ProjectID: "proj-a",
	}

	tr.handleCommand(context.Background(), cmd)

	rejects := client.getRejectCalls()
	if len(rejects) != 1 {
		t.Fatalf("reject calls = %d, want 1", len(rejects))
	}
	if rejects[0].Reason.Code != "missing_fields" {
		t.Fatalf("reject reason code = %q, want missing_fields", rejects[0].Reason.Code)
	}
	if got := client.getAckCalls(); len(got) != 0 {
		t.Fatalf("ack calls = %d, want 0", len(got))
	}
}

func TestTaskRunner_HandleCommand_DispatchAcksAndSpawnsTargetTask(t *testing.T) {
	client := newMockClient()
	task := testTask("task-99", "proj-a")
	task.ProjectID = "proj-a"
	client.readyTasks["proj-a"] = []types.ResolvedTask{*task}
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	tr := newTestRunner(client, executor, processMgr, newMockStateMgr())

	cmd := RunnerCommand{
		Type:      CommandDispatch,
		TaskID:    "task-99",
		ProjectID: "proj-a",
		LeaseID:   "lease-abc",
	}

	tr.handleCommand(context.Background(), cmd)

	acks := client.getAckCalls()
	if len(acks) != 1 {
		t.Fatalf("ack calls = %d, want 1", len(acks))
	}
	if acks[0].RunnerID != tr.runnerID || acks[0].ProjectID != "proj-a" || acks[0].TaskID != "task-99" || acks[0].LeaseID != "lease-abc" {
		t.Fatalf("ack call = %+v, want runner/project/task/lease", acks[0])
	}
	if got := client.getRejectCalls(); len(got) != 0 {
		t.Fatalf("reject calls = %d, want 0", len(got))
	}
	spawnCalls := executor.getSpawnCalls()
	if len(spawnCalls) != 1 {
		t.Fatalf("spawn calls = %d, want 1", len(spawnCalls))
	}
	if spawnCalls[0].TaskID != "task-99" || spawnCalls[0].ProjectID != "proj-a" {
		t.Fatalf("spawn call = %+v", spawnCalls[0])
	}
}

func TestTaskRunner_HandleCommand_DispatchRejectsWhenCannotAcceptLocally(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(*mockClient, *mockExecutor, *mockProcessMgr, *TaskRunner)
		wantCode string
	}{
		{
			name: "unsupported project",
			setup: func(client *mockClient, executor *mockExecutor, processMgr *mockProcessMgr, tr *TaskRunner) {
				task := testTask("task-99", "other-proj")
				client.readyTasks["other-proj"] = []types.ResolvedTask{*task}
			},
			wantCode: "project_unsupported",
		},
		{
			name: "paused",
			setup: func(client *mockClient, executor *mockExecutor, processMgr *mockProcessMgr, tr *TaskRunner) {
				task := testTask("task-99", "proj-a")
				client.readyTasks["proj-a"] = []types.ResolvedTask{*task}
				tr.PauseAll()
			},
			wantCode: "runner_paused",
		},
		{
			name: "capacity unavailable",
			setup: func(client *mockClient, executor *mockExecutor, processMgr *mockProcessMgr, tr *TaskRunner) {
				task := testTask("task-99", "proj-a")
				client.readyTasks["proj-a"] = []types.ResolvedTask{*task}
				tr.SetMaxParallel(1)
				_ = processMgr.Add("running-1", RunningTask{ID: "running-1"}, &mockProcess{exited: false})
			},
			wantCode: "capacity_unavailable",
		},
		{
			name: "unsupported executor",
			setup: func(client *mockClient, executor *mockExecutor, processMgr *mockProcessMgr, tr *TaskRunner) {
				task := testTask("task-99", "proj-a")
				task.Executor = "pi"
				client.readyTasks["proj-a"] = []types.ResolvedTask{*task}
			},
			wantCode: "executor_unsupported",
		},
		{
			name: "workdir unavailable",
			setup: func(client *mockClient, executor *mockExecutor, processMgr *mockProcessMgr, tr *TaskRunner) {
				task := testTask("task-99", "proj-a")
				client.readyTasks["proj-a"] = []types.ResolvedTask{*task}
				executor.resolveWorkdirErr = errors.New("no workdir")
			},
			wantCode: "workdir_unavailable",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			client := newMockClient()
			executor := newMockExecutor()
			processMgr := newMockProcessMgr()
			tr := newTestRunner(client, executor, processMgr, newMockStateMgr())
			projectID := "proj-a"
			if tt.name == "unsupported project" {
				projectID = "other-proj"
			}
			tt.setup(client, executor, processMgr, tr)

			tr.handleCommand(context.Background(), RunnerCommand{Type: CommandDispatch, ProjectID: projectID, TaskID: "task-99", LeaseID: "lease-abc"})

			if got := client.getAckCalls(); len(got) != 0 {
				t.Fatalf("ack calls = %d, want 0", len(got))
			}
			rejects := client.getRejectCalls()
			if len(rejects) != 1 {
				t.Fatalf("reject calls = %d, want 1", len(rejects))
			}
			if rejects[0].Reason.Code != tt.wantCode {
				t.Fatalf("reject code = %q, want %q", rejects[0].Reason.Code, tt.wantCode)
			}
		})
	}
}

func TestTaskRunner_HandleCommand_DispatchRejectsExpiredLeaseBeforeAck(t *testing.T) {
	client := newMockClient()
	task := testTask("task-99", "proj-a")
	client.readyTasks["proj-a"] = []types.ResolvedTask{*task}
	tr := newTestRunner(client, newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	tr.handleCommand(context.Background(), RunnerCommand{
		Type:      CommandDispatch,
		ProjectID: "proj-a",
		TaskID:    "task-99",
		LeaseID:   "lease-abc",
		ExpiresAt: fmt.Sprintf("%d", time.Now().Add(-time.Minute).UnixMilli()),
	})

	if got := client.getAckCalls(); len(got) != 0 {
		t.Fatalf("ack calls = %d, want 0", len(got))
	}
	rejects := client.getRejectCalls()
	if len(rejects) != 1 {
		t.Fatalf("reject calls = %d, want 1", len(rejects))
	}
	if rejects[0].Reason.Code != "lease_expired" {
		t.Fatalf("reject code = %q, want lease_expired", rejects[0].Reason.Code)
	}
}

func TestTaskRunner_HandleCommand_Shutdown(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	// Set up a cancel function
	ctx, cancel := context.WithCancel(context.Background())
	tr.cancel = cancel

	var shutdownEvent *RunnerEvent
	tr.OnEvent(func(event RunnerEvent) {
		if event.Type == EventShutdown {
			e := event
			shutdownEvent = &e
		}
	})

	cmd := RunnerCommand{
		Type:   CommandShutdown,
		Reason: "server maintenance",
	}

	tr.handleCommand(ctx, cmd)

	if shutdownEvent == nil {
		t.Fatal("shutdown command should emit EventShutdown")
	}
	if shutdownEvent.Reason != "server maintenance" {
		t.Errorf("shutdown reason = %q, want %q", shutdownEvent.Reason, "server maintenance")
	}

	// Context should be cancelled
	if ctx.Err() == nil {
		t.Error("shutdown command should cancel context")
	}
}

func TestTaskRunner_HandleCommand_Pause(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	cmd := RunnerCommand{
		Type: CommandPause,
	}

	ctx := context.Background()
	tr.handleCommand(ctx, cmd)

	if !tr.IsAllPaused() {
		t.Error("runner should be paused after pause command")
	}
}

func TestTaskRunner_HandleCommand_Resume(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	// First pause, then resume
	tr.PauseAll()
	if !tr.IsAllPaused() {
		t.Fatal("runner should be paused after PauseAll()")
	}

	cmd := RunnerCommand{
		Type: CommandResume,
	}

	ctx := context.Background()
	tr.handleCommand(ctx, cmd)

	if tr.IsAllPaused() {
		t.Error("runner should not be paused after resume command")
	}
}

func TestTaskRunner_HasCommandChannel(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	if tr.commandCh == nil {
		t.Error("TaskRunner should have a command channel")
	}

	// Verify it's buffered
	select {
	case tr.commandCh <- RunnerCommand{Type: CommandDispatch}:
		// Good — can write without blocking
	default:
		t.Error("commandCh should be buffered")
	}
}

func TestTaskRunner_CommandChannelTriggers(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	ctx, cancel := context.WithCancel(context.Background())

	// Start runner in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- tr.Start(ctx)
	}()

	// Wait for runner to start
	time.Sleep(100 * time.Millisecond)

	// Send an affinity update command
	tr.commandCh <- RunnerCommand{
		Type:       CommandAffinityUpdated,
		FeatureIDs: []string{"new-feature"},
	}

	// Wait for the command to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify the config was updated
	tr.mu.RLock()
	featureIDs := tr.config.FeatureIDs
	tr.mu.RUnlock()

	if len(featureIDs) != 1 || featureIDs[0] != "new-feature" {
		t.Errorf("feature IDs = %v, want [new-feature]", featureIDs)
	}

	cancel()
	<-errCh
}

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

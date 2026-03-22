package runner

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
// checkOpencodeStatus Tests
// =============================================================================

func TestCheckOpencodeStatus_Idle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{"type": "idle"})
	}))
	defer server.Close()

	// Extract port from test server URL
	port := serverPort(t, server)

	status := checkOpencodeStatus(port)
	if status != "idle" {
		t.Errorf("checkOpencodeStatus = %q, want %q", status, "idle")
	}
}

func TestCheckOpencodeStatus_Busy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"type": "busy"})
	}))
	defer server.Close()

	port := serverPort(t, server)

	status := checkOpencodeStatus(port)
	if status != "busy" {
		t.Errorf("checkOpencodeStatus = %q, want %q", status, "busy")
	}
}

func TestCheckOpencodeStatus_Unavailable_ConnectionRefused(t *testing.T) {
	// Use a port that nothing is listening on
	status := checkOpencodeStatus(19999)
	if status != "unavailable" {
		t.Errorf("checkOpencodeStatus = %q, want %q", status, "unavailable")
	}
}

func TestCheckOpencodeStatus_Unavailable_BadResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	port := serverPort(t, server)

	status := checkOpencodeStatus(port)
	if status != "unavailable" {
		t.Errorf("checkOpencodeStatus = %q, want %q", status, "unavailable")
	}
}

func TestCheckOpencodeStatus_Unavailable_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	port := serverPort(t, server)

	status := checkOpencodeStatus(port)
	if status != "unavailable" {
		t.Errorf("checkOpencodeStatus = %q, want %q", status, "unavailable")
	}
}

// =============================================================================
// resolveCompleteOnIdle Tests
// =============================================================================

func TestResolveCompleteOnIdle_ExplicitTrue(t *testing.T) {
	trueVal := true
	result := resolveCompleteOnIdle(&trueVal, "")
	if !result {
		t.Error("resolveCompleteOnIdle should return true when explicitly set to true")
	}
}

func TestResolveCompleteOnIdle_ExplicitFalse(t *testing.T) {
	falseVal := false
	result := resolveCompleteOnIdle(&falseVal, "some prompt")
	if result {
		t.Error("resolveCompleteOnIdle should return false when explicitly set to false, even with direct_prompt")
	}
}

func TestResolveCompleteOnIdle_DirectPrompt_DefaultsTrue(t *testing.T) {
	result := resolveCompleteOnIdle(nil, "do something")
	if !result {
		t.Error("resolveCompleteOnIdle should default to true when direct_prompt is set and complete_on_idle is nil")
	}
}

func TestResolveCompleteOnIdle_NoDirectPrompt_DefaultsFalse(t *testing.T) {
	result := resolveCompleteOnIdle(nil, "")
	if result {
		t.Error("resolveCompleteOnIdle should return false when both complete_on_idle and direct_prompt are unset")
	}
}

// =============================================================================
// idleDetectionThreshold Tests
// =============================================================================

func TestIdleDetectionThreshold_Default(t *testing.T) {
	tr := &TaskRunner{
		config: RunnerConfig{
			IdleDetectionThreshold: 0, // not set
		},
	}

	threshold := tr.idleDetectionThreshold()
	if threshold != 30*time.Second {
		t.Errorf("idleDetectionThreshold = %v, want %v", threshold, 30*time.Second)
	}
}

func TestIdleDetectionThreshold_Custom(t *testing.T) {
	tr := &TaskRunner{
		config: RunnerConfig{
			IdleDetectionThreshold: 60000, // 60 seconds in ms
		},
	}

	threshold := tr.idleDetectionThreshold()
	if threshold != 60*time.Second {
		t.Errorf("idleDetectionThreshold = %v, want %v", threshold, 60*time.Second)
	}
}

// =============================================================================
// checkIdleStatus Tests
// =============================================================================

func TestCheckIdleStatus_IdleTask_CompleteOnIdle_MarksCompleted(t *testing.T) {
	// Set up a mock OpenCode server that returns "idle"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"type": "idle"})
	}))
	defer server.Close()
	port := serverPort(t, server)

	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	cfg.IdleDetectionThreshold = 100 // 100ms for fast test

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeBackground,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	// Add a running task with CompleteOnIdle and an idle timestamp in the past
	proc := newMockProcess(100)
	task := RunningTask{
		ID:             "task1",
		Path:           "projects/proj-a/task/task1.md",
		Title:          "Test Task",
		Priority:       "medium",
		ProjectID:      "proj-a",
		PID:            100,
		StartedAt:      time.Now(),
		CompleteOnIdle: true,
		OpencodePort:   port,
		IdleSince:      time.Now().Add(-1 * time.Second).Format(time.RFC3339), // idle for 1 second
	}
	processMgr.Add("task1", task, proc)

	// Track events
	var events []RunnerEvent
	var eventMu sync.Mutex
	tr.OnEvent(func(event RunnerEvent) {
		eventMu.Lock()
		events = append(events, event)
		eventMu.Unlock()
	})

	ctx := context.Background()
	tr.checkIdleStatus(ctx)

	// Verify task was marked completed via API
	updates := client.getUpdateStatusCalls()
	foundCompleted := false
	for _, u := range updates {
		if u.TaskPath == "projects/proj-a/task/task1.md" && u.Status == "completed" {
			foundCompleted = true
		}
	}
	if !foundCompleted {
		t.Errorf("expected task to be marked completed, got updates: %+v", updates)
	}

	// Verify completion event was emitted
	eventMu.Lock()
	defer eventMu.Unlock()
	foundEvent := false
	for _, e := range events {
		if e.Type == EventTaskCompleted {
			foundEvent = true
		}
	}
	if !foundEvent {
		t.Error("expected task_completed event to be emitted")
	}
}

func TestCheckIdleStatus_IdleTask_NotCompleteOnIdle_MarksBlocked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"type": "idle"})
	}))
	defer server.Close()
	port := serverPort(t, server)

	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	cfg.IdleDetectionThreshold = 100

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeBackground,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	proc := newMockProcess(100)
	task := RunningTask{
		ID:             "task1",
		Path:           "projects/proj-a/task/task1.md",
		Title:          "Test Task",
		Priority:       "medium",
		ProjectID:      "proj-a",
		PID:            100,
		StartedAt:      time.Now(),
		CompleteOnIdle: false, // NOT complete on idle
		OpencodePort:   port,
		IdleSince:      time.Now().Add(-1 * time.Second).Format(time.RFC3339),
	}
	processMgr.Add("task1", task, proc)

	ctx := context.Background()
	tr.checkIdleStatus(ctx)

	// Verify task was marked blocked via API
	updates := client.getUpdateStatusCalls()
	foundBlocked := false
	for _, u := range updates {
		if u.TaskPath == "projects/proj-a/task/task1.md" && u.Status == "blocked" {
			foundBlocked = true
		}
	}
	if !foundBlocked {
		t.Errorf("expected task to be marked blocked, got updates: %+v", updates)
	}
}

func TestCheckIdleStatus_IdleTask_FirstDetection_SetsIdleSince(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"type": "idle"})
	}))
	defer server.Close()
	port := serverPort(t, server)

	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	cfg.IdleDetectionThreshold = 60000 // 60 seconds — won't trigger completion

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeBackground,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	proc := newMockProcess(100)
	task := RunningTask{
		ID:             "task1",
		Path:           "projects/proj-a/task/task1.md",
		Title:          "Test Task",
		Priority:       "medium",
		ProjectID:      "proj-a",
		PID:            100,
		StartedAt:      time.Now(),
		CompleteOnIdle: true,
		OpencodePort:   port,
		IdleSince:      "", // Not yet idle
	}
	processMgr.Add("task1", task, proc)

	ctx := context.Background()
	tr.checkIdleStatus(ctx)

	// Verify IdleSince was set (task should still be running, not completed)
	info := processMgr.Get("task1")
	if info == nil {
		t.Fatal("task should still be tracked")
	}
	if info.Task.IdleSince == "" {
		t.Error("IdleSince should be set on first idle detection")
	}

	// Should NOT have updated status (threshold not reached)
	updates := client.getUpdateStatusCalls()
	if len(updates) > 0 {
		t.Errorf("should not update status before threshold, got: %+v", updates)
	}
}

func TestCheckIdleStatus_BusyTask_ClearsIdleSince(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"type": "busy"})
	}))
	defer server.Close()
	port := serverPort(t, server)

	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     testRunnerConfig(),
		Mode:       ExecutionModeBackground,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	proc := newMockProcess(100)
	task := RunningTask{
		ID:             "task1",
		Path:           "projects/proj-a/task/task1.md",
		Title:          "Test Task",
		Priority:       "medium",
		ProjectID:      "proj-a",
		PID:            100,
		StartedAt:      time.Now(),
		CompleteOnIdle: true,
		OpencodePort:   port,
		IdleSince:      time.Now().Add(-10 * time.Second).Format(time.RFC3339), // was idle
	}
	processMgr.Add("task1", task, proc)

	ctx := context.Background()
	tr.checkIdleStatus(ctx)

	// Verify IdleSince was cleared
	info := processMgr.Get("task1")
	if info == nil {
		t.Fatal("task should still be tracked")
	}
	if info.Task.IdleSince != "" {
		t.Errorf("IdleSince should be cleared when busy, got %q", info.Task.IdleSince)
	}
}

func TestCheckIdleStatus_UnavailableStatus_NoAction(t *testing.T) {
	// No server running — port will be unavailable
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     testRunnerConfig(),
		Mode:       ExecutionModeBackground,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	proc := newMockProcess(100)
	task := RunningTask{
		ID:             "task1",
		Path:           "projects/proj-a/task/task1.md",
		Title:          "Test Task",
		Priority:       "medium",
		ProjectID:      "proj-a",
		PID:            100,
		StartedAt:      time.Now(),
		CompleteOnIdle: true,
		OpencodePort:   19998, // nothing listening
		IdleSince:      time.Now().Add(-10 * time.Second).Format(time.RFC3339),
	}
	processMgr.Add("task1", task, proc)

	ctx := context.Background()
	tr.checkIdleStatus(ctx)

	// Should not update status
	updates := client.getUpdateStatusCalls()
	if len(updates) > 0 {
		t.Errorf("should not update status when unavailable, got: %+v", updates)
	}

	// IdleSince should be preserved (not cleared)
	info := processMgr.Get("task1")
	if info == nil {
		t.Fatal("task should still be tracked")
	}
	if info.Task.IdleSince == "" {
		t.Error("IdleSince should be preserved when status is unavailable")
	}
}

func TestCheckIdleStatus_NoPort_SkipsTask(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     testRunnerConfig(),
		Mode:       ExecutionModeBackground,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	proc := newMockProcess(100)
	task := RunningTask{
		ID:             "task1",
		Path:           "projects/proj-a/task/task1.md",
		Title:          "Test Task",
		Priority:       "medium",
		ProjectID:      "proj-a",
		PID:            100,
		StartedAt:      time.Now(),
		CompleteOnIdle: true,
		OpencodePort:   0, // No port discovered yet
	}
	processMgr.Add("task1", task, proc)

	ctx := context.Background()
	tr.checkIdleStatus(ctx)

	// Should not update status
	updates := client.getUpdateStatusCalls()
	if len(updates) > 0 {
		t.Errorf("should not update status when no port, got: %+v", updates)
	}
}

func TestCheckIdleStatus_AppendCompletionNote(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"type": "idle"})
	}))
	defer server.Close()
	port := serverPort(t, server)

	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	cfg.IdleDetectionThreshold = 100

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeBackground,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	proc := newMockProcess(100)
	task := RunningTask{
		ID:             "task1",
		Path:           "projects/proj-a/task/task1.md",
		Title:          "Test Task",
		Priority:       "medium",
		ProjectID:      "proj-a",
		PID:            100,
		StartedAt:      time.Now(),
		CompleteOnIdle: true,
		OpencodePort:   port,
		IdleSince:      time.Now().Add(-1 * time.Second).Format(time.RFC3339),
	}
	processMgr.Add("task1", task, proc)

	ctx := context.Background()
	tr.checkIdleStatus(ctx)

	// Verify a completion note was appended
	client.mu.Lock()
	appendCalls := make([]appendCall, len(client.appendCalls))
	copy(appendCalls, client.appendCalls)
	client.mu.Unlock()

	if len(appendCalls) == 0 {
		t.Error("expected completion note to be appended to task")
	}
}

// =============================================================================
// ProcessManager UpdatePort / UpdateIdleSince Tests
// =============================================================================

func TestProcessManager_UpdatePort(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	task := testRunningTask("task1")
	proc := newMockProcess(100)
	pm.Add("task1", task, proc)

	pm.UpdatePort("task1", 4200)

	info := pm.Get("task1")
	if info == nil {
		t.Fatal("task should exist")
	}
	if info.Task.OpencodePort != 4200 {
		t.Errorf("OpencodePort = %d, want 4200", info.Task.OpencodePort)
	}
}

func TestProcessManager_UpdatePort_NotFound(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	// Should not panic
	pm.UpdatePort("nonexistent", 4200)
}

func TestProcessManager_UpdateIdleSince(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	task := testRunningTask("task1")
	proc := newMockProcess(100)
	pm.Add("task1", task, proc)

	now := time.Now().Format(time.RFC3339)
	pm.UpdateIdleSince("task1", now)

	info := pm.Get("task1")
	if info == nil {
		t.Fatal("task should exist")
	}
	if info.Task.IdleSince != now {
		t.Errorf("IdleSince = %q, want %q", info.Task.IdleSince, now)
	}
}

func TestProcessManager_UpdateIdleSince_Clear(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	task := testRunningTask("task1")
	task.IdleSince = time.Now().Format(time.RFC3339)
	proc := newMockProcess(100)
	pm.Add("task1", task, proc)

	pm.UpdateIdleSince("task1", "")

	info := pm.Get("task1")
	if info == nil {
		t.Fatal("task should exist")
	}
	if info.Task.IdleSince != "" {
		t.Errorf("IdleSince should be empty, got %q", info.Task.IdleSince)
	}
}

// =============================================================================
// CompleteOnIdle set during spawn Tests
// =============================================================================

func TestClaimAndSpawn_SetsCompleteOnIdle_DirectPrompt(t *testing.T) {
	client := newMockClient()
	client.claimResult = ClaimResult{Success: true}

	executor := newMockExecutor()
	proc := newMockProcess(100)
	executor.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/test"}

	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	task := testTask("task1", "proj-a")
	task.DirectPrompt = "do something"
	// CompleteOnIdle not explicitly set — should default to true

	ctx := context.Background()
	err := tr.claimAndSpawn(ctx, task, "proj-a")
	if err != nil {
		t.Fatalf("claimAndSpawn returned error: %v", err)
	}

	info := processMgr.Get("task1")
	if info == nil {
		t.Fatal("task should be tracked")
	}
	if !info.Task.CompleteOnIdle {
		t.Error("CompleteOnIdle should be true when DirectPrompt is set")
	}
}

func TestClaimAndSpawn_SetsCompleteOnIdle_ExplicitTrue(t *testing.T) {
	client := newMockClient()
	client.claimResult = ClaimResult{Success: true}

	executor := newMockExecutor()
	proc := newMockProcess(100)
	executor.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/test"}

	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	task := testTask("task1", "proj-a")
	trueVal := true
	task.CompleteOnIdle = &trueVal

	ctx := context.Background()
	err := tr.claimAndSpawn(ctx, task, "proj-a")
	if err != nil {
		t.Fatalf("claimAndSpawn returned error: %v", err)
	}

	info := processMgr.Get("task1")
	if info == nil {
		t.Fatal("task should be tracked")
	}
	if !info.Task.CompleteOnIdle {
		t.Error("CompleteOnIdle should be true when explicitly set")
	}
}

func TestClaimAndSpawn_SetsCompleteOnIdle_ExplicitFalse(t *testing.T) {
	client := newMockClient()
	client.claimResult = ClaimResult{Success: true}

	executor := newMockExecutor()
	proc := newMockProcess(100)
	executor.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/test"}

	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	task := testTask("task1", "proj-a")
	falseVal := false
	task.CompleteOnIdle = &falseVal
	task.DirectPrompt = "do something" // Even with direct_prompt, explicit false wins

	ctx := context.Background()
	err := tr.claimAndSpawn(ctx, task, "proj-a")
	if err != nil {
		t.Fatalf("claimAndSpawn returned error: %v", err)
	}

	info := processMgr.Get("task1")
	if info == nil {
		t.Fatal("task should be tracked")
	}
	if info.Task.CompleteOnIdle {
		t.Error("CompleteOnIdle should be false when explicitly set to false")
	}
}

func TestClaimAndSpawn_SetsCompleteOnIdle_DefaultFalse(t *testing.T) {
	client := newMockClient()
	client.claimResult = ClaimResult{Success: true}

	executor := newMockExecutor()
	proc := newMockProcess(100)
	executor.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/test"}

	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	task := testTask("task1", "proj-a")
	// No DirectPrompt, no CompleteOnIdle — should default to false

	ctx := context.Background()
	err := tr.claimAndSpawn(ctx, task, "proj-a")
	if err != nil {
		t.Fatalf("claimAndSpawn returned error: %v", err)
	}

	info := processMgr.Get("task1")
	if info == nil {
		t.Fatal("task should be tracked")
	}
	if info.Task.CompleteOnIdle {
		t.Error("CompleteOnIdle should be false when neither DirectPrompt nor CompleteOnIdle is set")
	}
}

// =============================================================================
// Helpers
// =============================================================================

func serverPort(t *testing.T, server *httptest.Server) int {
	t.Helper()
	// server.URL is like "http://127.0.0.1:PORT"
	var port int
	_, err := fmt.Sscanf(server.URL, "http://127.0.0.1:%d", &port)
	if err != nil {
		t.Fatalf("failed to parse server port from %s: %v", server.URL, err)
	}
	return port
}

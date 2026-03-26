package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// Mock Process for testing
// =============================================================================

type mockProcess struct {
	mu       sync.Mutex
	pid      int
	exited   bool
	exitCode int
	killed   bool
	signal   os.Signal
}

func newMockProcess(pid int) *mockProcess {
	return &mockProcess{pid: pid}
}

func (m *mockProcess) Pid() int {
	return m.pid
}

func (m *mockProcess) Exited() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.exited
}

func (m *mockProcess) ExitCode() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.exitCode
}

func (m *mockProcess) Kill(sig os.Signal) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.killed = true
	m.signal = sig
	return nil
}

// simulateExit makes the mock process appear exited.
func (m *mockProcess) simulateExit(code int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.exited = true
	m.exitCode = code
}

// Verify mockProcess implements Process interface
var _ Process = (*mockProcess)(nil)

// =============================================================================
// Process Tracking
// =============================================================================

func TestProcessManager_Add(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	task := testRunningTask("task1")
	proc := newMockProcess(100)

	if err := pm.Add("task1", task, proc); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	if pm.Count() != 1 {
		t.Errorf("Count = %d, want 1", pm.Count())
	}
	if pm.RunningCount() != 1 {
		t.Errorf("RunningCount = %d, want 1", pm.RunningCount())
	}
}

func TestProcessManager_Add_Duplicate(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	task := testRunningTask("task1")
	proc := newMockProcess(100)

	pm.Add("task1", task, proc)

	// Adding duplicate should return error
	err := pm.Add("task1", task, proc)
	if err == nil {
		t.Error("expected error when adding duplicate task")
	}
}

func TestProcessManager_Remove(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	task := testRunningTask("task1")
	proc := newMockProcess(100)
	pm.Add("task1", task, proc)

	info := pm.Remove("task1")
	if info == nil {
		t.Fatal("Remove returned nil, expected ProcessInfo")
	}
	if info.Task.ID != "task1" {
		t.Errorf("removed task ID = %q, want %q", info.Task.ID, "task1")
	}
	if pm.Count() != 0 {
		t.Errorf("Count after remove = %d, want 0", pm.Count())
	}
}

func TestProcessManager_Remove_NotFound(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	info := pm.Remove("nonexistent")
	if info != nil {
		t.Errorf("Remove returned %v, want nil for nonexistent task", info)
	}
}

func TestProcessManager_Get(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	task := testRunningTask("task1")
	proc := newMockProcess(100)
	pm.Add("task1", task, proc)

	info := pm.Get("task1")
	if info == nil {
		t.Fatal("Get returned nil")
	}
	if info.Task.ID != "task1" {
		t.Errorf("task ID = %q, want %q", info.Task.ID, "task1")
	}
}

func TestProcessManager_Get_NotFound(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	info := pm.Get("nonexistent")
	if info != nil {
		t.Errorf("Get returned %v, want nil", info)
	}
}

func TestProcessManager_IsRunning(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	task := testRunningTask("task1")
	proc := newMockProcess(100)
	pm.Add("task1", task, proc)

	if !pm.IsRunning("task1") {
		t.Error("IsRunning = false, want true for running process")
	}

	// Simulate exit
	proc.simulateExit(0)

	if pm.IsRunning("task1") {
		t.Error("IsRunning = true, want false for exited process")
	}
}

func TestProcessManager_IsRunning_NotTracked(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	if pm.IsRunning("nonexistent") {
		t.Error("IsRunning = true for untracked task, want false")
	}
}

func TestProcessManager_GetAll(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	pm.Add("task1", testRunningTask("task1"), newMockProcess(100))
	pm.Add("task2", testRunningTask("task2"), newMockProcess(101))

	all := pm.GetAll()
	if len(all) != 2 {
		t.Errorf("GetAll len = %d, want 2", len(all))
	}
}

func TestProcessManager_GetAllRunning(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	proc1 := newMockProcess(100)
	proc2 := newMockProcess(101)
	pm.Add("task1", testRunningTask("task1"), proc1)
	pm.Add("task2", testRunningTask("task2"), proc2)

	// Exit one process
	proc1.simulateExit(0)

	running := pm.GetAllRunning()
	if len(running) != 1 {
		t.Fatalf("GetAllRunning len = %d, want 1", len(running))
	}
	if running[0].Task.ID != "task2" {
		t.Errorf("running task ID = %q, want %q", running[0].Task.ID, "task2")
	}
}

func TestProcessManager_Count(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	if pm.Count() != 0 {
		t.Errorf("Count = %d, want 0 for empty manager", pm.Count())
	}

	pm.Add("task1", testRunningTask("task1"), newMockProcess(100))
	pm.Add("task2", testRunningTask("task2"), newMockProcess(101))

	if pm.Count() != 2 {
		t.Errorf("Count = %d, want 2", pm.Count())
	}
}

// =============================================================================
// Completion Detection
// =============================================================================

func TestProcessManager_CheckCompletion_Running(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	proc := newMockProcess(100)
	pm.Add("task1", testRunningTask("task1"), proc)

	status := pm.CheckCompletion("task1", false)
	if status != CompletionRunning {
		t.Errorf("CheckCompletion = %q, want %q", status, CompletionRunning)
	}
}

func TestProcessManager_CheckCompletion_NotTracked(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	status := pm.CheckCompletion("nonexistent", false)
	if status != CompletionCrashed {
		t.Errorf("CheckCompletion = %q, want %q for untracked task", status, CompletionCrashed)
	}
}

func TestProcessManager_CheckCompletion_ExitedClean(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	proc := newMockProcess(100)
	pm.Add("task1", testRunningTask("task1"), proc)

	proc.simulateExit(0)

	status := pm.CheckCompletion("task1", false)
	if status != CompletionCompleted {
		t.Errorf("CheckCompletion = %q, want %q for clean exit", status, CompletionCompleted)
	}
}

func TestProcessManager_CheckCompletion_ExitedError(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	proc := newMockProcess(100)
	pm.Add("task1", testRunningTask("task1"), proc)

	proc.simulateExit(1)

	status := pm.CheckCompletion("task1", false)
	if status != CompletionCrashed {
		t.Errorf("CheckCompletion = %q, want %q for error exit", status, CompletionCrashed)
	}
}

func TestProcessManager_CheckCompletion_Timeout(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.TaskTimeout = 100 // 100ms timeout
	pm := NewProcessManager(cfg)

	task := testRunningTask("task1")
	task.StartedAt = time.Now().Add(-1 * time.Second) // started 1s ago
	proc := newMockProcess(100)
	pm.Add("task1", task, proc)

	status := pm.CheckCompletion("task1", false)
	if status != CompletionTimeout {
		t.Errorf("CheckCompletion = %q, want %q for timed out task", status, CompletionTimeout)
	}
}

func TestProcessManager_CheckCompletion_NoTimeout_WhenZero(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.TaskTimeout = 0 // no timeout
	pm := NewProcessManager(cfg)

	task := testRunningTask("task1")
	task.StartedAt = time.Now().Add(-24 * time.Hour) // started 24h ago
	proc := newMockProcess(100)
	pm.Add("task1", task, proc)

	status := pm.CheckCompletion("task1", false)
	if status != CompletionRunning {
		t.Errorf("CheckCompletion = %q, want %q when timeout=0", status, CompletionRunning)
	}
}

func TestProcessManager_CheckCompletion_WithTaskFile_Completed(t *testing.T) {
	// Set up a mock API server that returns completed status
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "completed",
		})
	}))
	defer server.Close()

	cfg := defaultTestConfig()
	cfg.BrainAPIURL = server.URL
	pm := NewProcessManager(cfg)

	task := testRunningTask("task1")
	task.Path = "projects/test/task/abc.md"
	proc := newMockProcess(100)
	pm.Add("task1", task, proc)

	status := pm.CheckCompletion("task1", true)
	if status != CompletionCompleted {
		t.Errorf("CheckCompletion = %q, want %q for completed task file", status, CompletionCompleted)
	}
}

func TestProcessManager_CheckCompletion_WithTaskFile_Blocked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "blocked",
		})
	}))
	defer server.Close()

	cfg := defaultTestConfig()
	cfg.BrainAPIURL = server.URL
	pm := NewProcessManager(cfg)

	task := testRunningTask("task1")
	task.Path = "projects/test/task/abc.md"
	proc := newMockProcess(100)
	pm.Add("task1", task, proc)

	status := pm.CheckCompletion("task1", true)
	if status != CompletionBlocked {
		t.Errorf("CheckCompletion = %q, want %q", status, CompletionBlocked)
	}
}

func TestProcessManager_CheckCompletion_WithTaskFile_Cancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "cancelled",
		})
	}))
	defer server.Close()

	cfg := defaultTestConfig()
	cfg.BrainAPIURL = server.URL
	pm := NewProcessManager(cfg)

	task := testRunningTask("task1")
	task.Path = "projects/test/task/abc.md"
	proc := newMockProcess(100)
	pm.Add("task1", task, proc)

	status := pm.CheckCompletion("task1", true)
	if status != CompletionCancelled {
		t.Errorf("CheckCompletion = %q, want %q", status, CompletionCancelled)
	}
}

func TestProcessManager_CheckCompletion_WithTaskFile_APIError(t *testing.T) {
	// Server returns 500 — should fall through to process state
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := defaultTestConfig()
	cfg.BrainAPIURL = server.URL
	pm := NewProcessManager(cfg)

	proc := newMockProcess(100)
	pm.Add("task1", testRunningTask("task1"), proc)

	// Process still running, API error — should report Running
	status := pm.CheckCompletion("task1", true)
	if status != CompletionRunning {
		t.Errorf("CheckCompletion = %q, want %q on API error with running process", status, CompletionRunning)
	}
}

func TestProcessManager_CheckCompletion_WithRunFinalization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "in_progress",
			"run_finalizations": map[string]interface{}{
				"run-123": map[string]interface{}{
					"status": "completed",
				},
			},
		})
	}))
	defer server.Close()

	cfg := defaultTestConfig()
	cfg.BrainAPIURL = server.URL
	pm := NewProcessManager(cfg)

	task := testRunningTask("task1")
	task.Path = "projects/test/task/abc.md"
	task.RunID = "run-123"
	proc := newMockProcess(100)
	pm.Add("task1", task, proc)

	status := pm.CheckCompletion("task1", true)
	if status != CompletionCompleted {
		t.Errorf("CheckCompletion = %q, want %q for run finalization", status, CompletionCompleted)
	}
}

// =============================================================================
// Process Control
// =============================================================================

func TestProcessManager_Kill(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	proc := newMockProcess(100)
	pm.Add("task1", testRunningTask("task1"), proc)

	// Simulate the process exiting after kill
	go func() {
		time.Sleep(10 * time.Millisecond)
		proc.simulateExit(0)
	}()

	ctx := context.Background()
	killed := pm.Kill(ctx, "task1")
	if !killed {
		t.Error("Kill returned false, want true")
	}
	if !proc.killed {
		t.Error("process was not killed")
	}
}

func TestProcessManager_Kill_NotTracked(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	ctx := context.Background()
	killed := pm.Kill(ctx, "nonexistent")
	if killed {
		t.Error("Kill returned true for untracked task, want false")
	}
}

func TestProcessManager_Kill_AlreadyExited(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	proc := newMockProcess(100)
	pm.Add("task1", testRunningTask("task1"), proc)
	proc.simulateExit(0)

	ctx := context.Background()
	killed := pm.Kill(ctx, "task1")
	if !killed {
		t.Error("Kill returned false for already exited process, want true")
	}
}

func TestProcessManager_KillAll(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	proc1 := newMockProcess(100)
	proc2 := newMockProcess(101)
	pm.Add("task1", testRunningTask("task1"), proc1)
	pm.Add("task2", testRunningTask("task2"), proc2)

	// Simulate both exiting after kill
	go func() {
		time.Sleep(10 * time.Millisecond)
		proc1.simulateExit(0)
		proc2.simulateExit(0)
	}()

	ctx := context.Background()
	pm.KillAll(ctx)

	if !proc1.killed {
		t.Error("proc1 was not killed")
	}
	if !proc2.killed {
		t.Error("proc2 was not killed")
	}
}

// =============================================================================
// State Serialization
// =============================================================================

func TestProcessManager_ToProcessStates(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	proc := newMockProcess(100)
	pm.Add("task1", testRunningTask("task1"), proc)

	states := pm.ToProcessStates()
	if len(states) != 1 {
		t.Fatalf("ToProcessStates len = %d, want 1", len(states))
	}

	s := states[0]
	if s.TaskID != "task1" {
		t.Errorf("TaskID = %q, want %q", s.TaskID, "task1")
	}
	if s.PID != 100 {
		t.Errorf("PID = %d, want 100", s.PID)
	}
	if s.Exited {
		t.Error("Exited = true, want false")
	}
}

func TestProcessManager_ToProcessStates_WithExited(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	proc := newMockProcess(100)
	pm.Add("task1", testRunningTask("task1"), proc)
	proc.simulateExit(42)

	states := pm.ToProcessStates()
	if len(states) != 1 {
		t.Fatalf("ToProcessStates len = %d, want 1", len(states))
	}

	s := states[0]
	if !s.Exited {
		t.Error("Exited = false, want true")
	}
	if s.ExitCode != 42 {
		t.Errorf("ExitCode = %d, want 42", s.ExitCode)
	}
}

// =============================================================================
// Task Result Generation
// =============================================================================

func TestProcessManager_CreateTaskResult(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	proc := newMockProcess(100)
	task := testRunningTask("task1")
	task.StartedAt = time.Now().Add(-5 * time.Second)
	pm.Add("task1", task, proc)
	proc.simulateExit(0)

	result := pm.CreateTaskResult("task1", CompletionCompleted)
	if result == nil {
		t.Fatal("CreateTaskResult returned nil")
	}
	if result.TaskID != "task1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task1")
	}
	if result.Status != TaskResultCompleted {
		t.Errorf("Status = %q, want %q", result.Status, TaskResultCompleted)
	}
	if result.Duration <= 0 {
		t.Errorf("Duration = %d, want > 0", result.Duration)
	}
	if result.ExitCode == nil {
		t.Error("ExitCode is nil, want 0")
	} else if *result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", *result.ExitCode)
	}
}

func TestProcessManager_CreateTaskResult_AllStatuses(t *testing.T) {
	tests := []struct {
		completion CompletionStatus
		want       TaskResultStatus
	}{
		{CompletionCompleted, TaskResultCompleted},
		{CompletionFailed, TaskResultFailed},
		{CompletionBlocked, TaskResultBlocked},
		{CompletionCancelled, TaskResultCancelled},
		{CompletionTimeout, TaskResultTimeout},
		{CompletionCrashed, TaskResultCrashed},
	}

	for _, tt := range tests {
		t.Run(string(tt.completion), func(t *testing.T) {
			pm := NewProcessManager(defaultTestConfig())
			proc := newMockProcess(100)
			pm.Add("task1", testRunningTask("task1"), proc)
			proc.simulateExit(1)

			result := pm.CreateTaskResult("task1", tt.completion)
			if result == nil {
				t.Fatal("CreateTaskResult returned nil")
			}
			if result.Status != tt.want {
				t.Errorf("Status = %q, want %q", result.Status, tt.want)
			}
		})
	}
}

func TestProcessManager_CreateTaskResult_NotTracked(t *testing.T) {
	pm := NewProcessManager(defaultTestConfig())

	result := pm.CreateTaskResult("nonexistent", CompletionCompleted)
	if result != nil {
		t.Errorf("CreateTaskResult returned %v, want nil for untracked task", result)
	}
}

// =============================================================================
// Helpers
// =============================================================================

func defaultTestConfig() RunnerConfig {
	return RunnerConfig{
		BrainAPIURL: "http://localhost:3333",
		APITimeout:  5000,
		TaskTimeout: 0,
		MaxParallel: 2,
	}
}

func testRunningTask(id string) RunningTask {
	return RunningTask{
		ID:        id,
		Path:      fmt.Sprintf("projects/test/task/%s.md", id),
		Title:     fmt.Sprintf("Test Task %s", id),
		Priority:  "medium",
		ProjectID: "test-project",
		PID:       0,
		StartedAt: time.Now(),
	}
}

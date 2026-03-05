package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// StateManager — Save and Load
// ---------------------------------------------------------------------------

func TestStateManager_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir, "test-project")

	tasks := []RunningTask{
		{
			ID:        "task1",
			Path:      "projects/test/task/abc12345.md",
			Title:     "Test Task",
			Priority:  "high",
			ProjectID: "test-project",
			PID:       12345,
			StartedAt: time.Now(),
		},
	}
	stats := RunnerStats{Completed: 5, Failed: 1, TotalRuntime: 60000}
	startedAt := time.Now()

	sm.Save(RunnerStatusProcessing, tasks, stats, startedAt)

	loaded := sm.Load()
	if loaded == nil {
		t.Fatal("Load returned nil, expected state")
	}

	if loaded.ProjectID != "test-project" {
		t.Errorf("ProjectID = %q, want %q", loaded.ProjectID, "test-project")
	}
	if loaded.Status != RunnerStatusProcessing {
		t.Errorf("Status = %q, want %q", loaded.Status, RunnerStatusProcessing)
	}
	if len(loaded.RunningTasks) != 1 {
		t.Fatalf("RunningTasks len = %d, want 1", len(loaded.RunningTasks))
	}
	if loaded.RunningTasks[0].ID != "task1" {
		t.Errorf("RunningTasks[0].ID = %q, want %q", loaded.RunningTasks[0].ID, "task1")
	}
	if loaded.Stats.Completed != 5 {
		t.Errorf("Stats.Completed = %d, want 5", loaded.Stats.Completed)
	}
	if loaded.Stats.Failed != 1 {
		t.Errorf("Stats.Failed = %d, want 1", loaded.Stats.Failed)
	}
}

func TestStateManager_Load_NoFile(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir, "nonexistent")

	loaded := sm.Load()
	if loaded != nil {
		t.Errorf("Load returned %v, want nil for missing file", loaded)
	}
}

func TestStateManager_Load_CorruptedJSON(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir, "corrupt")

	// Write invalid JSON to the state file
	stateFile := filepath.Join(dir, "runner-corrupt.json")
	if err := os.WriteFile(stateFile, []byte("{invalid json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	loaded := sm.Load()
	if loaded != nil {
		t.Errorf("Load returned %v, want nil for corrupted JSON", loaded)
	}
}

// ---------------------------------------------------------------------------
// StateManager — Clear
// ---------------------------------------------------------------------------

func TestStateManager_Clear(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir, "clearme")

	// Create all three files
	sm.Save(RunnerStatusIdle, nil, RunnerStats{}, time.Now())
	sm.SavePid(os.Getpid())
	sm.SaveRunningTasks([]RunningTask{{ID: "t1"}})

	// Verify files exist
	for _, name := range []string{"runner-clearme.json", "runner-clearme.pid", "running-clearme.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected file %s to exist before clear", name)
		}
	}

	sm.Clear()

	// Verify files are gone
	for _, name := range []string{"runner-clearme.json", "runner-clearme.pid", "running-clearme.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("expected file %s to be removed after clear", name)
		}
	}
}

func TestStateManager_Clear_NoFiles(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir, "empty")

	// Should not panic when files don't exist
	sm.Clear()
}

// ---------------------------------------------------------------------------
// StateManager — PID Management
// ---------------------------------------------------------------------------

func TestStateManager_SaveAndLoadPid(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir, "pidtest")

	sm.SavePid(42)

	pid := sm.LoadPid()
	if pid == nil {
		t.Fatal("LoadPid returned nil, expected 42")
	}
	if *pid != 42 {
		t.Errorf("LoadPid = %d, want 42", *pid)
	}
}

func TestStateManager_LoadPid_NoFile(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir, "nopid")

	pid := sm.LoadPid()
	if pid != nil {
		t.Errorf("LoadPid returned %v, want nil for missing file", pid)
	}
}

func TestStateManager_LoadPid_InvalidContent(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir, "badpid")

	pidFile := filepath.Join(dir, "runner-badpid.pid")
	if err := os.WriteFile(pidFile, []byte("not-a-number"), 0o644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	pid := sm.LoadPid()
	if pid != nil {
		t.Errorf("LoadPid returned %v, want nil for invalid content", pid)
	}
}

func TestStateManager_ClearPid(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir, "clearpid")

	sm.SavePid(99)
	sm.ClearPid()

	pid := sm.LoadPid()
	if pid != nil {
		t.Errorf("LoadPid returned %v after ClearPid, want nil", pid)
	}
}

// ---------------------------------------------------------------------------
// StateManager — IsPidRunning
// ---------------------------------------------------------------------------

func TestStateManager_IsPidRunning_CurrentProcess(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir, "running")

	// Save our own PID — it should be running
	sm.SavePid(os.Getpid())

	if !sm.IsPidRunning() {
		t.Error("IsPidRunning = false for current process, want true")
	}
}

func TestStateManager_IsPidRunning_DeadProcess(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir, "dead")

	// PID 99999999 almost certainly doesn't exist
	sm.SavePid(99999999)

	if sm.IsPidRunning() {
		t.Error("IsPidRunning = true for dead PID, want false")
	}
}

func TestStateManager_IsPidRunning_NoPidFile(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir, "nopidfile")

	if sm.IsPidRunning() {
		t.Error("IsPidRunning = true with no PID file, want false")
	}
}

// ---------------------------------------------------------------------------
// StateManager — Running Tasks
// ---------------------------------------------------------------------------

func TestStateManager_SaveAndLoadRunningTasks(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir, "tasks")

	tasks := []RunningTask{
		{ID: "t1", Title: "Task 1", ProjectID: "proj"},
		{ID: "t2", Title: "Task 2", ProjectID: "proj"},
	}

	sm.SaveRunningTasks(tasks)

	loaded := sm.LoadRunningTasks()
	if len(loaded) != 2 {
		t.Fatalf("LoadRunningTasks len = %d, want 2", len(loaded))
	}
	if loaded[0].ID != "t1" {
		t.Errorf("loaded[0].ID = %q, want %q", loaded[0].ID, "t1")
	}
	if loaded[1].ID != "t2" {
		t.Errorf("loaded[1].ID = %q, want %q", loaded[1].ID, "t2")
	}
}

func TestStateManager_LoadRunningTasks_NoFile(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir, "notasks")

	loaded := sm.LoadRunningTasks()
	if len(loaded) != 0 {
		t.Errorf("LoadRunningTasks len = %d, want 0 for missing file", len(loaded))
	}
}

func TestStateManager_LoadRunningTasks_CorruptedJSON(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir, "corrupttasks")

	tasksFile := filepath.Join(dir, "running-corrupttasks.json")
	if err := os.WriteFile(tasksFile, []byte("not json"), 0o644); err != nil {
		t.Fatalf("write tasks file: %v", err)
	}

	loaded := sm.LoadRunningTasks()
	if len(loaded) != 0 {
		t.Errorf("LoadRunningTasks len = %d, want 0 for corrupted JSON", len(loaded))
	}
}

// ---------------------------------------------------------------------------
// StateManager — FindAllRunnerStates (static)
// ---------------------------------------------------------------------------

func TestFindAllRunnerStates(t *testing.T) {
	dir := t.TempDir()

	// Create some runner state files
	for _, name := range []string{"runner-proj1.json", "runner-proj2.json", "other-file.txt", "running-proj1.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o644); err != nil {
			t.Fatalf("write file %s: %v", name, err)
		}
	}

	states := FindAllRunnerStates(dir)
	if len(states) != 2 {
		t.Fatalf("FindAllRunnerStates len = %d, want 2", len(states))
	}

	// Check project IDs (order may vary)
	ids := map[string]bool{}
	for _, s := range states {
		ids[s.ProjectID] = true
	}
	if !ids["proj1"] || !ids["proj2"] {
		t.Errorf("expected proj1 and proj2, got %v", ids)
	}
}

func TestFindAllRunnerStates_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	states := FindAllRunnerStates(dir)
	if len(states) != 0 {
		t.Errorf("FindAllRunnerStates len = %d, want 0 for empty dir", len(states))
	}
}

func TestFindAllRunnerStates_NonexistentDir(t *testing.T) {
	states := FindAllRunnerStates("/nonexistent/path/that/does/not/exist")
	if len(states) != 0 {
		t.Errorf("FindAllRunnerStates len = %d, want 0 for nonexistent dir", len(states))
	}
}

// ---------------------------------------------------------------------------
// StateManager — CleanupStaleStates (static)
// ---------------------------------------------------------------------------

func TestCleanupStaleStates(t *testing.T) {
	dir := t.TempDir()

	// Create a state file with a dead PID
	sm := NewStateManager(dir, "stale")
	sm.Save(RunnerStatusIdle, nil, RunnerStats{}, time.Now())
	sm.SavePid(99999999) // dead PID

	// Create a state file with our own PID (alive)
	sm2 := NewStateManager(dir, "alive")
	sm2.Save(RunnerStatusProcessing, nil, RunnerStats{}, time.Now())
	sm2.SavePid(os.Getpid())

	cleaned := CleanupStaleStates(dir)
	if cleaned != 1 {
		t.Errorf("CleanupStaleStates = %d, want 1", cleaned)
	}

	// Stale state should be gone
	if sm.Load() != nil {
		t.Error("stale state should have been cleaned up")
	}

	// Alive state should remain
	if sm2.Load() == nil {
		t.Error("alive state should not have been cleaned up")
	}
}

// ---------------------------------------------------------------------------
// StateManager — JSON serialization format
// ---------------------------------------------------------------------------

func TestStateManager_SaveFormat(t *testing.T) {
	dir := t.TempDir()
	sm := NewStateManager(dir, "format")

	startedAt := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	sm.Save(RunnerStatusPolling, nil, RunnerStats{Completed: 3}, startedAt)

	// Read raw file and verify it's valid JSON
	data, err := os.ReadFile(filepath.Join(dir, "runner-format.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("state file is not valid JSON: %v", err)
	}

	if raw["projectId"] != "format" {
		t.Errorf("projectId = %v, want %q", raw["projectId"], "format")
	}
	if raw["status"] != "polling" {
		t.Errorf("status = %v, want %q", raw["status"], "polling")
	}
}

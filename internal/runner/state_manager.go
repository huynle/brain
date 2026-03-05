package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// RunnerStateInfo holds a discovered runner state file.
type RunnerStateInfo struct {
	ProjectID string
	StateFile string
}

// StateManager manages state persistence for a brain runner instance.
type StateManager struct {
	stateFile        string
	pidFile          string
	runningTasksFile string
	projectID        string
}

// NewStateManager creates a new StateManager for the given project.
func NewStateManager(stateDir, projectID string) *StateManager {
	return &StateManager{
		stateFile:        filepath.Join(stateDir, fmt.Sprintf("runner-%s.json", projectID)),
		pidFile:          filepath.Join(stateDir, fmt.Sprintf("runner-%s.pid", projectID)),
		runningTasksFile: filepath.Join(stateDir, fmt.Sprintf("running-%s.json", projectID)),
		projectID:        projectID,
	}
}

// =============================================================================
// State Persistence
// =============================================================================

// Save persists the full runner state to disk.
func (sm *StateManager) Save(status RunnerStatus, tasks []RunningTask, stats RunnerStats, startedAt time.Time) {
	state := RunnerState{
		ProjectID:    sm.projectID,
		Status:       status,
		StartedAt:    startedAt,
		UpdatedAt:    time.Now(),
		RunningTasks: tasks,
		Stats:        stats,
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}

	os.WriteFile(sm.stateFile, data, 0o644)
}

// Load reads the runner state from disk. Returns nil if not found or corrupted.
func (sm *StateManager) Load() *RunnerState {
	data, err := os.ReadFile(sm.stateFile)
	if err != nil {
		return nil
	}

	var state RunnerState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil
	}

	return &state
}

// Clear removes all state files for this project.
func (sm *StateManager) Clear() {
	for _, file := range []string{sm.stateFile, sm.pidFile, sm.runningTasksFile} {
		os.Remove(file)
	}
}

// =============================================================================
// PID Management
// =============================================================================

// SavePid writes the runner's process ID to disk.
func (sm *StateManager) SavePid(pid int) {
	os.WriteFile(sm.pidFile, []byte(strconv.Itoa(pid)), 0o644)
}

// LoadPid reads the runner's process ID from disk. Returns nil if not found.
func (sm *StateManager) LoadPid() *int {
	data, err := os.ReadFile(sm.pidFile)
	if err != nil {
		return nil
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return nil
	}

	return &pid
}

// ClearPid removes the PID file.
func (sm *StateManager) ClearPid() {
	os.Remove(sm.pidFile)
}

// IsPidRunning checks if the saved PID is still running.
// Returns false if no PID file exists or the process is dead.
func (sm *StateManager) IsPidRunning() bool {
	pid := sm.LoadPid()
	if pid == nil {
		return false
	}

	return isPidAlive(*pid)
}

// =============================================================================
// Running Tasks
// =============================================================================

// SaveRunningTasks persists running tasks to a separate file for faster recovery.
func (sm *StateManager) SaveRunningTasks(tasks []RunningTask) {
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return
	}

	os.WriteFile(sm.runningTasksFile, data, 0o644)
}

// LoadRunningTasks reads running tasks from disk. Returns empty slice if not found.
func (sm *StateManager) LoadRunningTasks() []RunningTask {
	data, err := os.ReadFile(sm.runningTasksFile)
	if err != nil {
		return []RunningTask{}
	}

	var tasks []RunningTask
	if err := json.Unmarshal(data, &tasks); err != nil {
		return []RunningTask{}
	}

	return tasks
}

// =============================================================================
// Static Utilities
// =============================================================================

// runnerFilePattern matches runner state files: runner-{projectId}.json
var runnerFilePattern = regexp.MustCompile(`^runner-(.+)\.json$`)

// FindAllRunnerStates finds all runner state files in a directory.
// Returns array of {ProjectID, StateFile} objects.
func FindAllRunnerStates(stateDir string) []RunnerStateInfo {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return nil
	}

	var states []RunnerStateInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := runnerFilePattern.FindStringSubmatch(entry.Name())
		if matches != nil {
			states = append(states, RunnerStateInfo{
				ProjectID: matches[1],
				StateFile: filepath.Join(stateDir, entry.Name()),
			})
		}
	}

	return states
}

// CleanupStaleStates removes state files for runners with dead PIDs.
// Returns the number of stale states cleaned up.
func CleanupStaleStates(stateDir string) int {
	states := FindAllRunnerStates(stateDir)
	cleaned := 0

	for _, info := range states {
		sm := NewStateManager(stateDir, info.ProjectID)
		if !sm.IsPidRunning() {
			sm.Clear()
			cleaned++
		}
	}

	return cleaned
}

// =============================================================================
// Helpers
// =============================================================================

// isPidAlive checks if a process with the given PID is still running.
func isPidAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Sending signal 0 checks if process exists without killing it
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

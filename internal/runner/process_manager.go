package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// =============================================================================
// Types
// =============================================================================

// CompletionStatus describes the outcome of a process completion check.
type CompletionStatus string

const (
	CompletionRunning   CompletionStatus = "running"
	CompletionCompleted CompletionStatus = "completed"
	CompletionFailed    CompletionStatus = "failed"
	CompletionBlocked   CompletionStatus = "blocked"
	CompletionCancelled CompletionStatus = "cancelled"
	CompletionTimeout   CompletionStatus = "timeout"
	CompletionCrashed   CompletionStatus = "crashed"
)

// Process is an interface for interacting with a running process.
// This allows mocking in tests.
type Process interface {
	Pid() int
	Exited() bool
	ExitCode() int
	Kill(sig os.Signal) error
}

// OsProcess wraps *exec.Cmd to implement the Process interface.
// It tracks exit status via cmd.Wait() called in a background goroutine.
type OsProcess struct {
	cmd      *exec.Cmd
	pid      int
	exited   bool
	exitCode int
	mu       sync.Mutex
	done     chan struct{}
}

// NewOsProcess creates an OsProcess from a started *exec.Cmd.
// The caller must ensure cmd.Start() has been called.
// A goroutine is started to monitor the process exit.
func NewOsProcess(cmd *exec.Cmd) *OsProcess {
	p := &OsProcess{
		cmd:  cmd,
		pid:  cmd.Process.Pid,
		done: make(chan struct{}),
	}

	// Monitor the process exit in a goroutine
	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		p.exited = true
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				p.exitCode = exitErr.ExitCode()
			} else {
				p.exitCode = -1
			}
		} else {
			p.exitCode = 0
		}
		p.mu.Unlock()
		close(p.done)
	}()

	return p
}

func (p *OsProcess) Pid() int {
	return p.pid
}

func (p *OsProcess) Exited() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exited
}

func (p *OsProcess) ExitCode() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitCode
}

func (p *OsProcess) Kill(sig os.Signal) error {
	if p.cmd.Process == nil {
		return fmt.Errorf("process not started")
	}
	return p.cmd.Process.Signal(sig)
}

// Done returns a channel that is closed when the process exits.
func (p *OsProcess) Done() <-chan struct{} {
	return p.done
}

// PidProcess tracks a process by PID only (for tmux-spawned processes).
// It checks liveness via kill -0 signal probe.
type PidProcess struct {
	pid int
}

// NewPidProcess creates a PidProcess for a given PID.
func NewPidProcess(pid int) *PidProcess {
	return &PidProcess{pid: pid}
}

func (p *PidProcess) Pid() int {
	return p.pid
}

func (p *PidProcess) Exited() bool {
	return !IsPidAlive(p.pid)
}

func (p *PidProcess) ExitCode() int {
	// Cannot determine exit code for PID-only processes
	return -1
}

func (p *PidProcess) Kill(sig os.Signal) error {
	proc, err := os.FindProcess(p.pid)
	if err != nil {
		return err
	}
	return proc.Signal(sig)
}

// ProcessInfo holds a tracked process and its metadata.
type ProcessInfo struct {
	Task     RunningTask
	Proc     Process
	ExitCode int
	IsExited bool
	ExitedAt *time.Time
}

// ProcessState is the serializable form of ProcessInfo for state persistence.
type ProcessState struct {
	TaskID   string      `json:"taskId"`
	Task     RunningTask `json:"task"`
	PID      int         `json:"pid"`
	ExitCode int         `json:"exitCode"`
	Exited   bool        `json:"exited"`
	ExitedAt string      `json:"exitedAt,omitempty"`
}

// =============================================================================
// Process Manager
// =============================================================================

// ProcessManager tracks spawned processes and detects completion.
type ProcessManager struct {
	mu        sync.Mutex
	processes map[string]*ProcessInfo
	config    RunnerConfig
	client    *http.Client
}

// NewProcessManager creates a new ProcessManager.
func NewProcessManager(config RunnerConfig) *ProcessManager {
	return &ProcessManager{
		processes: make(map[string]*ProcessInfo),
		config:    config,
		client: &http.Client{
			Timeout: time.Duration(config.APITimeout) * time.Millisecond,
		},
	}
}

// =============================================================================
// Process Tracking
// =============================================================================

// Add tracks a new process. Returns error if task is already tracked
// by a real (non-reservation) entry.
//
// If a slot reservation exists for this taskID (ReserveSlot was called
// first), Add upgrades the placeholder to a live process without
// consuming an extra slot. This is the "spawn succeeded" path: the
// reservation held capacity during the executor/workdir resolution
// and ack round-trip, and now the running Proc takes its place.
func (pm *ProcessManager) Add(taskID string, task RunningTask, proc Process) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if existing, exists := pm.processes[taskID]; exists {
		if existing.Proc != nil {
			slog.Warn("process already tracked", "task_id", taskID)
			return fmt.Errorf("task %s is already being tracked", taskID)
		}
		// Reservation placeholder — upgrade it in place. This is the
		// happy path: ReserveSlot held the capacity slot during dispatch
		// preflight, and now the real Proc replaces the placeholder.
		existing.Task = task
		existing.Proc = proc
		slog.Info("process tracked (upgraded reservation)", "task_id", taskID, "pid", proc.Pid(), "project", task.ProjectID)
		return nil
	}

	pm.processes[taskID] = &ProcessInfo{
		Task: task,
		Proc: proc,
	}

	slog.Info("process tracked", "task_id", taskID, "pid", proc.Pid(), "project", task.ProjectID)
	return nil
}

// ReserveSlot atomically checks and holds an execution slot for a task.
// Returns true if the slot was granted, false if the runner is at
// capacity. Idempotent for the same task ID (re-reservation returns
// true without consuming an extra slot).
//
// This closes a race in the dispatch consumer: previously the
// capacity check (RunningCount() >= MaxParallel) and the actual
// spawn were separated by several HTTP round-trips (ack, executor
// resolution, workdir resolution). Multiple concurrent dispatch
// workers could all pass the check before any spawned, causing the
// runner to exceed max_parallel (production observed 7-8 concurrent
// processes on a max_parallel=3 runner).
//
// The reservation counts toward RunningCount so subsequent
// ReserveSlot callers see the accurate load. Callers MUST either:
//   - Call Add(taskID, ...) to upgrade the reservation to a live
//     process on successful spawn, or
//   - Call ReleaseReservation(taskID) if the dispatch fails between
//     reserve and spawn (task_lookup_failed, executor_unsupported,
//     workdir_unavailable, ack failure, spawn error).
func (pm *ProcessManager) ReserveSlot(taskID string, maxParallel int) bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Idempotent: if we've already reserved (or the task is already
	// running), let the caller proceed. This handles legitimate
	// dispatch redelivery.
	if _, exists := pm.processes[taskID]; exists {
		return true
	}

	if maxParallel > 0 && len(pm.processes) >= maxParallel {
		return false
	}

	// Insert the placeholder with a nil Proc. RunningCount sees this
	// as one occupied slot, and Add will upgrade it later.
	pm.processes[taskID] = &ProcessInfo{}
	return true
}

// ReleaseReservation removes an unspawned slot reservation. Safe to
// call when no reservation exists (no-op). Never removes a live
// process — those must go through Remove after CheckCompletion.
func (pm *ProcessManager) ReleaseReservation(taskID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	info, exists := pm.processes[taskID]
	if !exists {
		return
	}
	if info.Proc != nil {
		// Not a reservation — a real process. Don't touch it.
		return
	}
	delete(pm.processes, taskID)
}

// Remove removes and returns process info. Returns nil if not found.
func (pm *ProcessManager) Remove(taskID string) *ProcessInfo {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	info, exists := pm.processes[taskID]
	if !exists {
		return nil
	}

	delete(pm.processes, taskID)
	if info.Proc != nil {
		slog.Info("process removed", "task_id", taskID, "pid", info.Proc.Pid())
	} else {
		slog.Info("reservation removed", "task_id", taskID)
	}
	return info
}

// Get returns process info. Returns nil if not found.
func (pm *ProcessManager) Get(taskID string) *ProcessInfo {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	return pm.processes[taskID]
}

// UpdatePort updates the OpencodePort on a tracked task.
func (pm *ProcessManager) UpdatePort(taskID string, port int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	info, exists := pm.processes[taskID]
	if !exists {
		return
	}
	info.Task.OpencodePort = port
}

// UpdateSessionID updates the SessionID on a tracked task.
func (pm *ProcessManager) UpdateSessionID(taskID string, sessionID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	info, exists := pm.processes[taskID]
	if !exists {
		return
	}
	info.Task.SessionID = sessionID
}

// UpdateIdleSince updates the IdleSince timestamp on a tracked task.
func (pm *ProcessManager) UpdateIdleSince(taskID string, idleSince string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	info, exists := pm.processes[taskID]
	if !exists {
		return
	}
	info.Task.IdleSince = idleSince
}

// IsRunning checks if a process is still alive.
func (pm *ProcessManager) IsRunning(taskID string) bool {
	pm.mu.Lock()
	info, exists := pm.processes[taskID]
	pm.mu.Unlock()

	if !exists {
		return false
	}
	if info.Proc == nil {
		// Reservation placeholder — the task is scheduled but not yet
		// spawned. Treat as not-yet-running so callers waiting on real
		// process state don't misfire.
		return false
	}

	return !info.Proc.Exited()
}

// GetAll returns all tracked process info. Excludes unspawned slot
// reservations (nil Proc): a reservation has a zero-value Task, and
// every consumer of this list (completion checks, claim renewal,
// lease release, state persistence) assumes a live process with a
// populated Task — leaking a placeholder produces spurious API calls
// with empty task paths.
func (pm *ProcessManager) GetAll() []ProcessInfo {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	result := make([]ProcessInfo, 0, len(pm.processes))
	for _, info := range pm.processes {
		if info.Proc == nil {
			// Reservation placeholder — no live process yet.
			continue
		}
		result = append(result, *info)
	}
	return result
}

// GetAllRunning returns only running processes. Excludes both exited
// processes and unspawned reservations.
func (pm *ProcessManager) GetAllRunning() []ProcessInfo {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	var result []ProcessInfo
	for _, info := range pm.processes {
		if info.Proc == nil {
			// Reservation placeholder — no live process yet.
			continue
		}
		if !info.Proc.Exited() {
			result = append(result, *info)
		}
	}
	return result
}

// Count returns total tracked processes.
func (pm *ProcessManager) Count() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	return len(pm.processes)
}

// RunningCount returns the count of slots currently in use — both
// live processes AND unspawned reservations. Reservations count so
// concurrent dispatch workers observe the true load and don't
// over-allocate slots (see ReserveSlot).
func (pm *ProcessManager) RunningCount() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	count := 0
	for _, info := range pm.processes {
		if info.Proc == nil {
			// Reservation placeholder — counts as one used slot.
			count++
			continue
		}
		if !info.Proc.Exited() {
			count++
		}
	}
	return count
}

// =============================================================================
// Completion Detection
// =============================================================================

// taskEntrySnapshot holds the relevant fields from a task entry API response.
type taskEntrySnapshot struct {
	Status           string                 `json:"status"`
	RunFinalizations map[string]interface{} `json:"run_finalizations"`
}

// CheckCompletion performs a non-blocking completion check.
// If checkTaskFile is true, it also queries the Brain API for task file status.
func (pm *ProcessManager) CheckCompletion(taskID string, checkTaskFile bool) CompletionStatus {
	pm.mu.Lock()
	info, exists := pm.processes[taskID]
	pm.mu.Unlock()

	if !exists {
		return CompletionCrashed
	}

	if info.Proc == nil {
		// Reservation placeholder — spawn is still in flight, so the
		// task is neither complete nor crashed.
		return CompletionRunning
	}

	// Check for timeout (0 = no timeout)
	if pm.config.TaskTimeout > 0 {
		elapsed := time.Since(info.Task.StartedAt)
		if elapsed > time.Duration(pm.config.TaskTimeout)*time.Millisecond {
			slog.Warn("task timeout detected", "task_id", taskID, "elapsed_ms", elapsed.Milliseconds(), "timeout_ms", pm.config.TaskTimeout)
			return CompletionTimeout
		}
	}

	procExited := info.Proc.Exited()

	// Steered-turn hold: for attachable OpenCode tasks the tracked process
	// is the `opencode run` driver, but an injected prompt (goal steering,
	// control-plane send) runs its own turn on the persistent serve
	// process. If the driver exited cleanly while the session still
	// reports busy, that turn is mid-flight — completing now would tear
	// the serve process down under it. Hold completion until the session
	// idles, bounded by steerHoldMax so a wedged session can't pin the
	// task forever. Checked before every completion path below.
	if procExited && info.Proc.ExitCode() == 0 && info.Task.OpencodePort > 0 {
		if sessionStatusForPort(info.Task.OpencodePort) == "busy" {
			pm.mu.Lock()
			if info.Task.BusyHoldSince.IsZero() {
				info.Task.BusyHoldSince = time.Now()
			}
			held := time.Since(info.Task.BusyHoldSince)
			pm.mu.Unlock()
			if held < steerHoldMax {
				slog.Info("driver exited but session busy; holding completion for in-flight turn",
					"task_id", taskID, "port", info.Task.OpencodePort,
					"held", held.Round(time.Second))
				return CompletionRunning
			}
			slog.Warn("session still busy after hold window; completing anyway",
				"task_id", taskID, "held", held.Round(time.Second))
		}
	}

	// If process has exited and we're not checking task file
	if procExited && !checkTaskFile {
		if info.Proc.ExitCode() == 0 {
			return CompletionCompleted
		}
		return CompletionCrashed
	}

	// Check task file for status via API
	if checkTaskFile {
		entry := pm.getTaskEntry(info.Task.Path)
		if entry != nil {
			slog.Debug("task file status", "task_id", taskID, "status", entry.Status)
			// Check direct status
			switch entry.Status {
			case "completed":
				return CompletionCompleted
			case "blocked":
				return CompletionBlocked
			case "cancelled":
				return CompletionCancelled
			}
			if entry.Status != "" && entry.Status != "in_progress" && entry.Status != "pending" {
				return CompletionFailed
			}

			// Check run finalizations
			if info.Task.RunID != "" {
				finStatus := pm.getRunFinalizedStatus(entry.RunFinalizations, info.Task.RunID)
				switch finStatus {
				case "completed":
					return CompletionCompleted
				case "blocked":
					return CompletionBlocked
				case "cancelled":
					return CompletionCancelled
				}
			}
		}
	}

	// Process still running
	if !procExited {
		return CompletionRunning
	}

	// Process exited but task file didn't update to completion.
	exitCode := info.Proc.ExitCode()
	slog.Info("process exited", "task_id", taskID, "exit_code", exitCode)

	// A KNOWN non-zero exit is never a success signal, whatever the
	// executor: a script that failed its merge, an `opencode run` that died
	// on an auth or model error, a pi process that panicked. The
	// CompleteOnIdle shortcut below exists because PID-tracked tmux
	// processes report -1 (unknowable) — it was never meant to launder
	// genuine failures into completions, which is how a checkout agent
	// dying on startup used to complete its task and let the feature
	// proceed as if reviewed.
	if exitCode > 0 {
		slog.Warn("task process exited non-zero; marking crashed",
			"task_id", taskID, "exit_code", exitCode,
			"executor", info.Task.ExecutorType)
		return CompletionCrashed
	}

	// For complete_on_idle tasks (e.g. direct_prompt), process exit means the
	// agent finished the prompt and the TUI closed — treat as completed.
	// Note: PidProcess always returns ExitCode() == -1 since we can't determine
	// the real exit code for PID-tracked tmux processes, so we accept that
	// unknown exit here.
	if info.Task.CompleteOnIdle {
		return CompletionCompleted
	}

	// Otherwise, process crashed or failed
	return CompletionCrashed
}

// getTaskEntry fetches task status from the Brain API.
func (pm *ProcessManager) getTaskEntry(taskPath string) *taskEntrySnapshot {
	encodedPath := encodePathComponent(taskPath)
	url := fmt.Sprintf("%s/api/v1/entries/%s", pm.config.BrainAPIURL, encodedPath)

	resp, err := pm.client.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var entry taskEntrySnapshot
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return nil
	}

	return &entry
}

// getRunFinalizedStatus extracts the status from run_finalizations for a specific runID.
func (pm *ProcessManager) getRunFinalizedStatus(runFinalizations map[string]interface{}, runID string) string {
	if runFinalizations == nil {
		return ""
	}

	runData, ok := runFinalizations[runID]
	if !ok {
		return ""
	}

	runMap, ok := runData.(map[string]interface{})
	if !ok {
		return ""
	}

	status, ok := runMap["status"].(string)
	if !ok {
		return ""
	}

	return status
}

// =============================================================================
// Process Control
// =============================================================================

// Kill terminates a specific process. Returns true if the process is exited.
func (pm *ProcessManager) Kill(ctx context.Context, taskID string) bool {
	pm.mu.Lock()
	info, exists := pm.processes[taskID]
	pm.mu.Unlock()

	if !exists {
		return false
	}

	if info.Proc == nil {
		// Reservation placeholder — no process to kill.
		return false
	}

	if info.Proc.Exited() {
		return true
	}

	// Send SIGTERM
	slog.Info("process kill SIGTERM", "task_id", taskID, "pid", info.Proc.Pid())
	// Best-effort teardown — the process is being abandoned either way.
	_ = info.Proc.Kill(syscall.SIGTERM)

	// Wait for exit with timeout
	if pm.waitForExit(info.Proc, 5*time.Second) {
		return true
	}

	// Force kill if didn't exit
	slog.Warn("process kill SIGKILL (force)", "task_id", taskID, "pid", info.Proc.Pid())
	// Best-effort teardown — the process is being abandoned either way.
	_ = info.Proc.Kill(syscall.SIGKILL)
	pm.waitForExit(info.Proc, 2*time.Second)

	return info.Proc.Exited()
}

// KillAll terminates all tracked processes.
func (pm *ProcessManager) KillAll(ctx context.Context) {
	pm.mu.Lock()
	taskIDs := make([]string, 0, len(pm.processes))
	for id := range pm.processes {
		taskIDs = append(taskIDs, id)
	}
	pm.mu.Unlock()

	slog.Info("killing all processes", "count", len(taskIDs))

	// Send SIGTERM to all
	for _, id := range taskIDs {
		pm.mu.Lock()
		info, exists := pm.processes[id]
		pm.mu.Unlock()
		if exists && info.Proc != nil && !info.Proc.Exited() {
			// Best-effort teardown — the process is being abandoned either way.
			_ = info.Proc.Kill(syscall.SIGTERM)
		}
	}

	// Wait for graceful exit
	var wg sync.WaitGroup
	for _, id := range taskIDs {
		pm.mu.Lock()
		info, exists := pm.processes[id]
		pm.mu.Unlock()
		if exists && info.Proc != nil {
			wg.Add(1)
			go func(proc Process) {
				defer wg.Done()
				pm.waitForExit(proc, 5*time.Second)
			}(info.Proc)
		}
	}
	wg.Wait()

	// Force kill any remaining
	for _, id := range taskIDs {
		pm.mu.Lock()
		info, exists := pm.processes[id]
		pm.mu.Unlock()
		if exists && info.Proc != nil && !info.Proc.Exited() {
			// Best-effort teardown — the process is being abandoned either way.
			_ = info.Proc.Kill(syscall.SIGKILL)
		}
	}

	slog.Info("all processes killed")
}

// waitForExit polls until the process exits or timeout is reached.
func (pm *ProcessManager) waitForExit(proc Process, timeout time.Duration) bool {
	if proc.Exited() {
		return true
	}

	deadline := time.After(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return proc.Exited()
		case <-ticker.C:
			if proc.Exited() {
				return true
			}
		}
	}
}

// =============================================================================
// State Serialization
// =============================================================================

// ToProcessStates serializes all processes for state persistence.
func (pm *ProcessManager) ToProcessStates() []ProcessState {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	states := make([]ProcessState, 0, len(pm.processes))
	for taskID, info := range pm.processes {
		if info.Proc == nil {
			// Reservation placeholder — nothing to persist yet.
			continue
		}
		s := ProcessState{
			TaskID:   taskID,
			Task:     info.Task,
			PID:      info.Proc.Pid(),
			ExitCode: info.Proc.ExitCode(),
			Exited:   info.Proc.Exited(),
		}
		states = append(states, s)
	}

	return states
}

// =============================================================================
// Task Result Generation
// =============================================================================

// CreateTaskResult generates a TaskResult from a completed process.
func (pm *ProcessManager) CreateTaskResult(taskID string, status CompletionStatus) *TaskResult {
	pm.mu.Lock()
	info, exists := pm.processes[taskID]
	pm.mu.Unlock()

	if !exists || info.Proc == nil {
		return nil
	}

	completedAt := time.Now()
	duration := completedAt.Sub(info.Task.StartedAt).Milliseconds()

	var resultStatus TaskResultStatus
	switch status {
	case CompletionCompleted:
		resultStatus = TaskResultCompleted
	case CompletionFailed:
		resultStatus = TaskResultFailed
	case CompletionBlocked:
		resultStatus = TaskResultBlocked
	case CompletionCancelled:
		resultStatus = TaskResultCancelled
	case CompletionTimeout:
		resultStatus = TaskResultTimeout
	default:
		resultStatus = TaskResultCrashed
	}

	exitCode := info.Proc.ExitCode()

	return &TaskResult{
		TaskID:      taskID,
		Status:      resultStatus,
		StartedAt:   info.Task.StartedAt,
		CompletedAt: completedAt,
		Duration:    duration,
		ExitCode:    &exitCode,
	}
}

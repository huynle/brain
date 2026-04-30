package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Interfaces for dependency injection
// =============================================================================

// Client abstracts the Brain API client for testability.
type Client interface {
	CheckHealth(ctx context.Context) (APIHealth, error)
	ListProjects(ctx context.Context) ([]string, error)
	GetReadyTasks(ctx context.Context, projectID string, featureIDs ...string) ([]types.ResolvedTask, error)
	GetNextTask(ctx context.Context, projectID string, featureIDs ...string) (*types.ResolvedTask, error)
	GetAllTasks(ctx context.Context, projectID string) ([]types.ResolvedTask, error)
	ClaimTask(ctx context.Context, projectID, taskID, runnerID string) (ClaimResult, error)
	ReleaseTask(ctx context.Context, projectID, taskID string) error
	UpdateTaskStatus(ctx context.Context, taskPath, status string) error
	AppendToTask(ctx context.Context, taskPath, content string) error
	UpdateEntry(ctx context.Context, entryPath string, updates map[string]interface{}) (*types.BrainEntry, error)
	UpdateMetadata(ctx context.Context, entryPath string, fields map[string]interface{}) error
	GetEntry(ctx context.Context, entryPath string) (*types.BrainEntry, error)
	EmitEvent(ctx context.Context, eventType string, payload map[string]any, dedupKey string) error
	RegisterRunner(ctx context.Context, req types.RegisterRunnerRequest) (*types.RunnerInfo, error)
	HeartbeatRunner(ctx context.Context, req types.HeartbeatRequest) error
	DeregisterRunner(ctx context.Context, runnerID string) error
}

// TaskExecutor abstracts the Executor for testability.
type TaskExecutor interface {
	BuildPrompt(task *types.ResolvedTask, isResume bool) string
	ResolveWorkdir(task *types.ResolvedTask) (string, error)
	Spawn(ctx context.Context, task *types.ResolvedTask, projectID string, opts SpawnOptions) (*SpawnResult, error)
	Cleanup(taskID, projectID string) error
}

// TaskProcessManager abstracts the ProcessManager for testability.
type TaskProcessManager interface {
	Add(taskID string, task RunningTask, proc Process) error
	Remove(taskID string) *ProcessInfo
	Get(taskID string) *ProcessInfo
	GetAll() []ProcessInfo
	GetAllRunning() []ProcessInfo
	Count() int
	RunningCount() int
	CheckCompletion(taskID string, checkTaskFile bool) CompletionStatus
	CreateTaskResult(taskID string, status CompletionStatus) *TaskResult
	Kill(ctx context.Context, taskID string) bool
	KillAll(ctx context.Context)
	ToProcessStates() []ProcessState
	UpdatePort(taskID string, port int)
	UpdateIdleSince(taskID string, idleSince string)
}

// TaskStateManager abstracts the StateManager for testability.
type TaskStateManager interface {
	Save(status RunnerStatus, tasks []RunningTask, stats RunnerStats, startedAt time.Time)
	Load() *RunnerState
	SavePid(pid int)
	LoadPid() *int
	ClearPid()
	SaveRunningTasks(tasks []RunningTask)
	LoadRunningTasks() []RunningTask
}

// =============================================================================
// TaskRunner Options
// =============================================================================

// TaskRunnerOptions configures a new TaskRunner.
type TaskRunnerOptions struct {
	// ProjectID is the primary project (single-project mode).
	ProjectID string

	// Projects is the list of projects to monitor (multi-project mode).
	Projects []string

	// Config is the runner configuration.
	Config RunnerConfig

	// Mode is the execution mode (headless, tui, dashboard).
	Mode ExecutionMode

	// StartPaused starts the runner with all projects paused.
	StartPaused bool

	// Logger is an optional logger. If nil, a default logger is used.
	Logger *log.Logger

	// Dependencies (injected for testability)
	Client     Client
	Executor   TaskExecutor
	ProcessMgr TaskProcessManager
	StateMgr   TaskStateManager
}

// RunnerStatusInfo is a snapshot of the runner's current state.
type RunnerStatusInfo struct {
	RunnerID    string       `json:"runnerId"`
	Status      RunnerStatus `json:"status"`
	Projects    []string     `json:"projects"`
	Stats       RunnerStats  `json:"stats"`
	Running     int          `json:"running"`
	MaxParallel int          `json:"maxParallel"`
	Paused      []string     `json:"paused"`
	StartedAt   time.Time    `json:"startedAt"`
}

// =============================================================================
// TaskRunner
// =============================================================================

// TaskRunner orchestrates task polling, claiming, spawning, and lifecycle.
type TaskRunner struct {
	runnerID string
	projects []string
	config   RunnerConfig
	mode     ExecutionMode
	logger   *log.Logger

	client     Client
	executor   TaskExecutor
	processMgr TaskProcessManager
	stateMgr   TaskStateManager

	// Mutable state (protected by mu)
	mu              sync.RWMutex
	status          RunnerStatus
	stats           RunnerStats
	startedAt       time.Time
	lastCronCheckAt time.Time
	maxParallel     int    // runtime-adjustable max parallel (0 = use config.MaxParallel)
	lastClaimDate   string // YYYY-MM-DD of last claim, for first_task_today detection

	// Pause state (protected by pauseMu)
	pauseMu         sync.RWMutex
	pauseCache      map[string]bool
	allPaused       bool
	enabledFeatures map[string]bool // features toggled on via TUI "x" key

	// Event handlers (protected by eventMu)
	eventMu  sync.RWMutex
	handlers []EventHandler

	// SSE reactive polling
	wakeCh      chan struct{}
	shutdownCh  chan string
	sseListener *SSEListener

	// Lifecycle
	cancel      context.CancelFunc
	done        chan struct{}
	doneOnce    sync.Once
	cleanupOnce sync.Once
}

// NewTaskRunner creates a new TaskRunner with the given options.
func NewTaskRunner(opts TaskRunnerOptions) *TaskRunner {
	// Generate runner ID
	idBytes := make([]byte, 4)
	rand.Read(idBytes)
	runnerID := "runner_" + hex.EncodeToString(idBytes)

	// Determine projects list
	projects := opts.Projects
	if len(projects) == 0 && opts.ProjectID != "" {
		projects = []string{opts.ProjectID}
	}

	// Default mode
	mode := opts.Mode
	if mode == "" {
		mode = ExecutionModeHeadless
	}

	// Default logger
	logger := opts.Logger
	if logger == nil {
		logger = log.Default()
	}

	tr := &TaskRunner{
		runnerID:        runnerID,
		projects:        projects,
		config:          opts.Config,
		mode:            mode,
		logger:          logger,
		client:          opts.Client,
		executor:        opts.Executor,
		processMgr:      opts.ProcessMgr,
		stateMgr:        opts.StateMgr,
		status:          RunnerStatusIdle,
		pauseCache:      make(map[string]bool),
		enabledFeatures: make(map[string]bool),
		wakeCh:          make(chan struct{}, 1),
		shutdownCh:      make(chan string, 1),
		done:            make(chan struct{}),
	}

	if opts.StartPaused {
		tr.allPaused = true
	}

	return tr
}

// =============================================================================
// Lifecycle
// =============================================================================

// Start begins the polling loop. It blocks until the context is cancelled
// or Stop is called.
func (tr *TaskRunner) Start(ctx context.Context) error {
	ctx, tr.cancel = context.WithCancel(ctx)

	tr.mu.Lock()
	tr.status = RunnerStatusPolling
	tr.startedAt = time.Now()
	tr.mu.Unlock()

	// Save PID
	if tr.stateMgr != nil {
		tr.stateMgr.SavePid(os.Getpid())
	}

	// Save initial state
	tr.saveState()

	// Emit runner started event (local)
	tr.emitEvent(RunnerEvent{
		Type:     EventRunnerStarted,
		Projects: tr.projects,
		Mode:     string(tr.mode),
	})

	// Emit runner.started event to domain event bus via API
	go func() {
		emitCtx, emitCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer emitCancel()
		_ = tr.client.EmitEvent(emitCtx, "runner.started", map[string]any{
			"runner_id": tr.runnerID,
			"projects":  tr.projects,
			"mode":      string(tr.mode),
		}, "runner-started-"+tr.runnerID)
	}()

	// Register runner with the API server for distributed discovery
	go tr.registerRunner(ctx)

	pollInterval := time.Duration(tr.config.PollInterval) * time.Second
	if pollInterval < time.Second {
		pollInterval = time.Second
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Start SSE listener for reactive polling
	if tr.config.BrainAPIURL != "" && len(tr.projects) > 0 {
		tr.sseListener = NewSSEListener(
			tr.config.BrainAPIURL,
			tr.config.APIToken,
			tr.runnerID,
			tr.projects,
			tr.wakeCh,
			tr.shutdownCh,
		)
		go tr.sseListener.Start(ctx)
		slog.Info("SSE listener started for reactive polling", "projects", len(tr.projects))
	}

	// Heartbeat ticker — sends heartbeats at half the stale threshold (every 30s)
	heartbeatTicker := time.NewTicker(30 * time.Second)
	defer heartbeatTicker.Stop()

	// Run initial poll immediately
	tr.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			tr.mu.Lock()
			tr.status = RunnerStatusStopped
			tr.mu.Unlock()
			tr.saveState()
			tr.doneOnce.Do(func() { close(tr.done) })
			return nil
		case <-ticker.C:
			tr.poll(ctx)
		case <-tr.wakeCh:
			tr.poll(ctx)
		case reason := <-tr.shutdownCh:
			slog.Info("remote shutdown requested", "runner_id", tr.runnerID, "reason", reason)
			if tr.cancel != nil {
				tr.cancel()
			}
		case <-heartbeatTicker.C:
			tr.sendHeartbeat(ctx)
		}
	}
}

// Stop gracefully shuts down the runner.
func (tr *TaskRunner) Stop() error {
	if tr.cancel != nil {
		tr.cancel()

		// Wait for the poll loop to exit.
		<-tr.done
	}

	tr.cleanupOnce.Do(func() {
		// Stop SSE listener
		if tr.sseListener != nil {
			tr.sseListener.Stop()
			slog.Info("SSE listener stopped")
		}

		// Clean up tmux windows for all running tasks, then kill processes
		if tr.processMgr != nil {
			for _, info := range tr.processMgr.GetAll() {
				tr.cleanupTaskTmux(info.Task)
			}
			ctx := context.Background()
			tr.processMgr.KillAll(ctx)
		}

		// Clear PID
		if tr.stateMgr != nil {
			tr.stateMgr.ClearPid()
		}

		tr.deregisterRunner()

		// Emit shutdown event
		tr.emitEvent(RunnerEvent{
			Type:   EventShutdown,
			Reason: "graceful shutdown",
		})

		// Save final state
		tr.saveState()
	})

	return nil
}

// =============================================================================
// Poll Loop
// =============================================================================

// poll executes a single poll iteration.
func (tr *TaskRunner) poll(ctx context.Context) {
	// Check context cancellation
	if ctx.Err() != nil {
		return
	}

	// 1. Health check
	health, err := tr.client.CheckHealth(ctx)
	if err != nil || (health.Status != "ok" && health.Status != "healthy") {
		return
	}

	// 2. Check running tasks for completion
	tr.checkRunningTasks(ctx)

	// 2.5. Check idle status for OpenCode agents
	tr.checkIdleStatus(ctx)

	// 2.6. Check scheduled tasks (cron triggers)
	tr.checkScheduledTasks(ctx, time.Now().UTC())

	// 3. Check capacity
	running := tr.processMgr.RunningCount()
	maxParallel := tr.getMaxParallel()
	if running >= maxParallel {
		tr.emitPollComplete()
		return
	}

	// 4. Check if all paused
	tr.pauseMu.RLock()
	allPaused := tr.allPaused
	enabledIDs := tr.getEnabledFeatureIDsLocked()
	tr.pauseMu.RUnlock()
	if allPaused && len(enabledIDs) == 0 {
		tr.emitPollComplete()
		return
	}

	// 5. Fill available slots
	slotsAvailable := maxParallel - running
	filled := 0

	for _, projectID := range tr.projects {
		if ctx.Err() != nil {
			break
		}
		if filled >= slotsAvailable {
			break
		}

		// Skip paused projects (unless features are enabled)
		tr.pauseMu.RLock()
		paused := tr.pauseCache[projectID]
		projEnabledIDs := tr.getEnabledFeatureIDsLocked()
		tr.pauseMu.RUnlock()

		if paused || allPaused {
			if len(projEnabledIDs) == 0 {
				continue // fully paused, no enabled features
			}
			// Paused but features enabled: poll only enabled features
			task, err := tr.client.GetNextTask(ctx, projectID, projEnabledIDs...)
			if err != nil || task == nil {
				continue
			}
			// Filter by capability match before claiming
			if !tr.matchesCapabilities(task) {
				slog.Debug("skipping task (paused/enabled): runner lacks required capabilities",
					"task_id", task.ID,
					"project", projectID,
					"requires", task.RequiresCapability,
					"runner_capabilities", tr.config.Capabilities,
				)
				continue
			}
			if err := tr.claimAndSpawn(ctx, task, projectID); err != nil {
				tr.logger.Printf("claim and spawn (enabled feature) failed for %s/%s: %v", projectID, task.ID, err)
				continue
			}
			filled++
			continue
		}

		// Get next task for this project (filtered by feature IDs if configured)
		task, err := tr.client.GetNextTask(ctx, projectID, tr.config.FeatureIDs...)
		if err != nil || task == nil {
			continue
		}

		// Filter by capability match before claiming
		if !tr.matchesCapabilities(task) {
			slog.Debug("skipping task: runner lacks required capabilities",
				"task_id", task.ID,
				"project", projectID,
				"requires", task.RequiresCapability,
				"runner_capabilities", tr.config.Capabilities,
			)
			continue
		}

		// Claim and spawn
		if err := tr.claimAndSpawn(ctx, task, projectID); err != nil {
			tr.logger.Printf("claim and spawn failed for %s/%s: %v", projectID, task.ID, err)
			continue
		}

		filled++
	}

	// 6. Save state
	tr.saveState()

	// 7. Emit poll complete event
	tr.emitPollComplete()
}

// =============================================================================
// Claim and Spawn
// =============================================================================

// claimAndSpawn claims a task and spawns a process for it.
func (tr *TaskRunner) claimAndSpawn(ctx context.Context, task *types.ResolvedTask, projectID string) error {
	// Claim the task
	result, err := tr.client.ClaimTask(ctx, projectID, task.ID, tr.runnerID)
	if err != nil {
		return fmt.Errorf("claim task: %w", err)
	}
	if !result.Success {
		tr.emitEvent(RunnerEvent{
			Type:      EventTaskClaimRejected,
			TaskID:    task.ID,
			ProjectID: projectID,
			ClaimedBy: result.ClaimedBy,
		})
		return fmt.Errorf("task already claimed by %s", result.ClaimedBy)
	}

	tr.emitEvent(RunnerEvent{
		Type:      EventTaskClaimed,
		TaskID:    task.ID,
		ProjectID: projectID,
		TaskPath:  task.Path,
	})

	// Check if this is the first claim today → emit runner.first_task_today
	today := time.Now().UTC().Format("2006-01-02")
	tr.mu.Lock()
	isFirstToday := tr.lastClaimDate != today
	if isFirstToday {
		tr.lastClaimDate = today
	}
	tr.mu.Unlock()
	if isFirstToday {
		go func() {
			emitCtx, emitCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer emitCancel()
			_ = tr.client.EmitEvent(emitCtx, "runner.first_task_today", map[string]any{
				"runner_id":  tr.runnerID,
				"project_id": projectID,
				"task_id":    task.ID,
				"date":       today,
			}, "first-task-"+today+"-"+tr.runnerID)
		}()
	}

	// Update task status to in_progress
	if err := tr.client.UpdateTaskStatus(ctx, task.Path, "in_progress"); err != nil {
		// Release the claim on failure
		tr.client.ReleaseTask(ctx, projectID, task.ID)
		return fmt.Errorf("update task status: %w", err)
	}

	tr.emitEvent(RunnerEvent{
		Type:       EventTaskStatusChanged,
		TaskID:     task.ID,
		ProjectID:  projectID,
		TaskPath:   task.Path,
		FromStatus: "pending",
		ToStatus:   "in_progress",
	})

	// Resolve workdir (may create git worktree)
	workdir, err := tr.executor.ResolveWorkdir(task)
	if err != nil {
		// Worktree creation failed - mark task as blocked
		tr.emitEvent(RunnerEvent{
			Type:       EventTaskStatusChanged,
			TaskID:     task.ID,
			ProjectID:  projectID,
			TaskPath:   task.Path,
			FromStatus: "in_progress",
			ToStatus:   "blocked",
		})
		tr.emitEvent(RunnerEvent{
			Type:      EventTaskReleased,
			TaskID:    task.ID,
			ProjectID: projectID,
			Reason:    "workdir resolution failed",
		})
		tr.client.ReleaseTask(ctx, projectID, task.ID)
		_ = tr.client.UpdateTaskStatus(ctx, task.Path, "blocked")
		return fmt.Errorf("resolve workdir: %w", err)
	}

	spawnOpts := SpawnOptions{
		Mode:    tr.mode,
		Workdir: workdir,
	}

	spawnResult, err := tr.executor.Spawn(ctx, task, projectID, spawnOpts)
	if err != nil {
		// Release the claim on failure
		tr.emitEvent(RunnerEvent{
			Type:      EventTaskReleased,
			TaskID:    task.ID,
			ProjectID: projectID,
			Reason:    "spawn failed",
		})
		tr.client.ReleaseTask(ctx, projectID, task.ID)
		return fmt.Errorf("spawn task: %w", err)
	}

	// Resolve executor type for tracking (empty defaults to "opencode")
	executorType := task.Executor
	if executorType == "" {
		executorType = "opencode"
	}

	// Build running task record
	runningTask := RunningTask{
		ID:             task.ID,
		Path:           task.Path,
		Title:          task.Title,
		Priority:       task.Priority,
		ProjectID:      projectID,
		PID:            spawnResult.PID,
		PaneID:         spawnResult.PaneID,
		WindowName:     spawnResult.WindowName,
		StartedAt:      time.Now(),
		Workdir:        spawnResult.Workdir,
		Executor:       executorType,
		CompleteOnIdle: resolveCompleteOnIdle(task.CompleteOnIdle, task.DirectPrompt),
		RunID:          latestInProgressRunID(task.Runs),
	}

	// Track in process manager
	if spawnResult.Proc != nil {
		if err := tr.processMgr.Add(task.ID, runningTask, spawnResult.Proc); err != nil {
			return fmt.Errorf("track process: %w", err)
		}
	}

	// Update status
	tr.mu.Lock()
	tr.status = RunnerStatusProcessing
	tr.mu.Unlock()

	// Emit event
	tr.emitEvent(RunnerEvent{
		Type: EventTaskStarted,
		Task: &runningTask,
	})

	// Discover opencode session ID and port in background (only for opencode executor)
	if executorType == "opencode" {
		go tr.discoverAndSaveSession(task.Path, spawnResult.PID)
	}

	return nil
}

// discoverAndSaveSession discovers the opencode port and session ID for a
// spawned process and persists it on the task's entry for later "o"/"O" access.
// The pid is typically a tmux shell PID; the actual opencode runs as a child.
func (tr *TaskRunner) discoverAndSaveSession(taskPath string, pid int) {
	if pid <= 0 {
		return
	}

	// Wait for opencode to start its HTTP server
	time.Sleep(5 * time.Second)

	// Find opencode's port by checking child processes
	var port int
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		port, err = discoverChildPort(pid)
		if err == nil && port > 0 {
			break
		}
		time.Sleep(3 * time.Second)
	}
	if err != nil || port == 0 {
		tr.logger.Printf("session discovery: failed to discover port for PID %d: %v", pid, err)
		return
	}

	// Store the discovered port on the running task for idle detection
	for _, info := range tr.processMgr.GetAll() {
		if info.Task.Path == taskPath {
			tr.processMgr.UpdatePort(info.Task.ID, port)
			break
		}
	}

	// Query opencode's session endpoint
	sessionID, err := discoverSessionID(port)
	if err != nil {
		tr.logger.Printf("session discovery: failed to get session from port %d: %v", port, err)
		return
	}
	if sessionID == "" {
		return
	}

	// Persist session ID to the task's metadata in SQLite via API
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = tr.client.UpdateMetadata(ctx, taskPath, map[string]interface{}{
		"sessions": map[string]interface{}{
			sessionID: map[string]interface{}{
				"timestamp": time.Now().UTC().Format(time.RFC3339),
			},
		},
	})
	if err != nil {
		tr.logger.Printf("session discovery: failed to persist session %s for %s: %v", sessionID, taskPath, err)
	}

	// Also emit event so the TUI updates in-memory immediately
	tr.emitEvent(RunnerEvent{
		Type:      EventSessionDiscovered,
		TaskPath:  taskPath,
		SessionID: sessionID,
	})
	tr.logger.Printf("session discovery: saved session %s for %s (port %d)", sessionID, taskPath, port)
}

// discoverChildPort finds the LISTEN port on any child process of the given PID.
// This handles the tmux case where the tracked PID is the shell, but opencode
// (the child) is the one listening on a port.
func discoverChildPort(parentPID int) (int, error) {
	// First try the parent itself
	if port, err := DiscoverPort(parentPID); err == nil && port > 0 {
		return port, nil
	}

	// Walk children using pgrep
	cmd := exec.Command("pgrep", "-P", fmt.Sprintf("%d", parentPID))
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("pgrep failed: %w", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		childPID := 0
		if _, err := fmt.Sscanf(line, "%d", &childPID); err != nil || childPID <= 0 {
			continue
		}
		if port, err := DiscoverPort(childPID); err == nil && port > 0 {
			return port, nil
		}
		// Recurse one level deeper (opencode may be a grandchild)
		if port, err := discoverChildPort(childPID); err == nil && port > 0 {
			return port, nil
		}
	}

	return 0, fmt.Errorf("no listening port found in process tree of PID %d", parentPID)
}

// discoverSessionID queries an opencode HTTP server for the active session ID.
// The /session endpoint returns an array of sessions; we take the most recent.
func discoverSessionID(port int) (string, error) {
	url := fmt.Sprintf("http://localhost:%d/session", port)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}

	// /session returns an array of sessions
	var sessions []struct {
		ID   string `json:"id"`
		Time struct {
			Updated int64 `json:"updated"`
		} `json:"time"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		// Try single-object response as fallback
		var single struct {
			ID string `json:"id"`
		}
		if err2 := json.Unmarshal([]byte(err.Error()), &single); err2 != nil {
			return "", fmt.Errorf("decode session response: %w", err)
		}
		return single.ID, nil
	}

	if len(sessions) == 0 {
		return "", nil
	}

	// Return the most recently updated session
	latest := sessions[0]
	for _, s := range sessions[1:] {
		if s.Time.Updated > latest.Time.Updated {
			latest = s
		}
	}
	return latest.ID, nil
}

// =============================================================================
// Completion Checking
// =============================================================================

// checkRunningTasks checks all running tasks for completion.
func (tr *TaskRunner) checkRunningTasks(ctx context.Context) {
	allProcesses := tr.processMgr.GetAll()

	for _, info := range allProcesses {
		if ctx.Err() != nil {
			return
		}

		status := tr.processMgr.CheckCompletion(info.Task.ID, true)
		if status == CompletionRunning {
			continue
		}

		tr.handleTaskCompletion(ctx, info.Task.ID, info.Task, status)
	}
}

// handleTaskCompletion processes a completed task.
func (tr *TaskRunner) handleTaskCompletion(ctx context.Context, taskID string, task RunningTask, status CompletionStatus) {
	// Create result before removing from process manager
	result := tr.processMgr.CreateTaskResult(taskID, status)

	// Remove from process manager
	tr.processMgr.Remove(taskID)

	// Map completion status to API status
	var apiStatus string
	var eventType RunnerEventType
	switch status {
	case CompletionCompleted:
		apiStatus = "completed"
		eventType = EventTaskCompleted
	case CompletionBlocked:
		apiStatus = "blocked"
		eventType = EventTaskFailed
	case CompletionCancelled:
		apiStatus = "completed" // cancelled tasks are considered done
		eventType = EventTaskCancelled
	default:
		apiStatus = "pending" // failed/crashed/timeout → back to pending for retry
		eventType = EventTaskFailed
	}

	// Emit status change before the API update
	tr.emitEvent(RunnerEvent{
		Type:       EventTaskStatusChanged,
		TaskID:     taskID,
		ProjectID:  task.ProjectID,
		TaskPath:   task.Path,
		FromStatus: "in_progress",
		ToStatus:   apiStatus,
	})

	// Update API status
	if err := tr.client.UpdateTaskStatus(ctx, task.Path, apiStatus); err != nil {
		tr.logger.Printf("failed to update task status for %s: %v", taskID, err)
	}

	// For script executor tasks: save exit code and captured output to task metadata
	if task.Executor == "script" && result != nil {
		tr.finalizeScriptTask(ctx, task, result)
	}

	// Update run record if this was a scheduled task
	if task.RunID != "" {
		tr.finalizeRun(ctx, task, status)
	}

	// Update stats
	tr.mu.Lock()
	if status == CompletionCompleted {
		tr.stats.Completed++
	} else {
		tr.stats.Failed++
	}
	if result != nil {
		tr.stats.TotalRuntime += result.Duration
	}
	// Update runner status if no more running tasks
	if tr.processMgr.RunningCount() == 0 {
		tr.status = RunnerStatusPolling
	}
	tr.mu.Unlock()

	// Clean up tmux window (graceful: Ctrl+C then kill)
	tr.cleanupTaskTmux(task)

	// Cleanup temp files
	tr.executor.Cleanup(taskID, task.ProjectID)

	// Emit event
	tr.emitEvent(RunnerEvent{
		Type:   eventType,
		Result: result,
		TaskID: taskID,
	})
}

// =============================================================================
// Run Finalization
// =============================================================================

// finalizeRun updates the run record in the entry's runs[] array with completion
// status, timestamp, and duration. Called when a scheduled task finishes execution.
func (tr *TaskRunner) finalizeRun(ctx context.Context, task RunningTask, status CompletionStatus) {
	// 1. Fetch current entry to get latest runs array
	entry, err := tr.client.GetEntry(ctx, task.Path)
	if err != nil {
		tr.logger.Printf("cron: failed to fetch entry for run finalization %s: %v", task.ID, err)
		return
	}

	// 2. Map CompletionStatus to run status string
	var runStatus string
	switch status {
	case CompletionCompleted:
		runStatus = "completed"
	case CompletionBlocked:
		runStatus = "failed"
	case CompletionCancelled:
		runStatus = "completed"
	default:
		runStatus = "failed"
	}

	// 3. Rebuild runs array with the matching run updated
	now := time.Now().UTC()
	runs := make([]interface{}, 0, len(entry.Runs))
	for _, r := range entry.Runs {
		runMap := map[string]interface{}{
			"run_id":    r.RunID,
			"status":    r.Status,
			"started":   r.Started,
			"completed": r.Completed,
		}
		if r.SkipReason != "" {
			runMap["skip_reason"] = r.SkipReason
		}
		if r.Duration != nil {
			runMap["duration"] = *r.Duration
		}
		// Update the matching run
		if r.RunID == task.RunID {
			runMap["status"] = runStatus
			runMap["completed"] = now.Format(time.RFC3339)
			if started, err := time.Parse(time.RFC3339, r.Started); err == nil {
				dur := int(now.Sub(started).Seconds())
				runMap["duration"] = dur
			}
		}
		runs = append(runs, runMap)
	}

	// 4. Persist the updated runs array
	if err := tr.client.UpdateMetadata(ctx, task.Path, map[string]interface{}{
		"runs": runs,
	}); err != nil {
		tr.logger.Printf("cron: failed to update run completion for %s run=%s: %v", task.ID, task.RunID, err)
	} else {
		tr.logger.Printf("cron: finalized run %s for %s: status=%s", task.RunID, task.ID, runStatus)
	}
}

// =============================================================================
// Script Task Finalization
// =============================================================================

// finalizeScriptTask saves exit code and captured output to the task's metadata.
// Output is read from the output log file and truncated to maxScriptOutputBytes (10KB).
func (tr *TaskRunner) finalizeScriptTask(ctx context.Context, task RunningTask, result *TaskResult) {
	// Read output log file
	outputFile := fmt.Sprintf("%s/output_%s_%s.log", tr.config.StateDir, task.ProjectID, task.ID)
	output := ""
	if data, err := os.ReadFile(outputFile); err == nil {
		output = string(data)
		// Truncate to maxScriptOutputBytes, keeping the tail (most recent output)
		if len(output) > maxScriptOutputBytes {
			output = "...(truncated)...\n" + output[len(output)-maxScriptOutputBytes:]
		}
	}

	// Determine exit code
	exitCode := -1
	if result.ExitCode != nil {
		exitCode = *result.ExitCode
	}

	// Save exit_code and output to task metadata
	metaCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	fields := map[string]interface{}{
		"exit_code":     exitCode,
		"script_output": output,
	}
	if err := tr.client.UpdateMetadata(metaCtx, task.Path, fields); err != nil {
		tr.logger.Printf("script: failed to save output metadata for %s: %v", task.ID, err)
	} else {
		tr.logger.Printf("script: saved output (%d bytes, exit=%d) for %s", len(output), exitCode, task.ID)
	}
}

// maxScriptOutputBytes is the maximum size of captured script output (10KB).
const maxScriptOutputBytes = 10 * 1024

// =============================================================================
// Tmux Cleanup
// =============================================================================

// cleanupTaskTmux gracefully closes a task's tmux window.
// It sends Ctrl+C first to let OpenCode shut down, waits briefly, then kills the window.
func (tr *TaskRunner) cleanupTaskTmux(task RunningTask) {
	if task.WindowName == "" && task.PaneID == "" {
		return
	}

	target := task.WindowName
	targetType := "window"
	if target == "" {
		target = task.PaneID
		targetType = "pane"
	}

	// Step 1: Send Ctrl+C for graceful shutdown
	sendKeysCmd := exec.Command("tmux", "send-keys", "-t", target, "C-c", "")
	if err := sendKeysCmd.Run(); err != nil {
		// Window may already be closed
		tr.logger.Printf("tmux send-keys failed for %s %s (may be closed): %v", targetType, target, err)
	}

	// Wait 500ms for graceful shutdown
	time.Sleep(500 * time.Millisecond)

	// Step 2: Kill the window/pane
	var killCmd *exec.Cmd
	if targetType == "window" {
		killCmd = exec.Command("tmux", "kill-window", "-t", target)
	} else {
		killCmd = exec.Command("tmux", "kill-pane", "-t", target)
	}
	if err := killCmd.Run(); err != nil {
		// Already closed
		tr.logger.Printf("tmux kill-%s failed for %s (may be closed): %v", targetType, target, err)
	}
}

// =============================================================================
// Pause / Resume
// =============================================================================

// PauseProject pauses task processing for a specific project.
func (tr *TaskRunner) PauseProject(projectID string) {
	tr.pauseMu.Lock()
	tr.pauseCache[projectID] = true
	tr.pauseMu.Unlock()

	tr.emitEvent(RunnerEvent{
		Type:      EventProjectPaused,
		ProjectID: projectID,
	})
}

// ResumeProject resumes task processing for a specific project.
func (tr *TaskRunner) ResumeProject(projectID string) {
	tr.pauseMu.Lock()
	delete(tr.pauseCache, projectID)
	tr.pauseMu.Unlock()

	tr.emitEvent(RunnerEvent{
		Type:      EventProjectResumed,
		ProjectID: projectID,
	})
}

// PauseAll pauses task processing for all projects.
func (tr *TaskRunner) PauseAll() {
	tr.pauseMu.Lock()
	tr.allPaused = true
	tr.pauseMu.Unlock()

	tr.emitEvent(RunnerEvent{
		Type: EventAllPaused,
	})
}

// ResumeAll resumes task processing for all projects.
func (tr *TaskRunner) ResumeAll() {
	tr.pauseMu.Lock()
	tr.allPaused = false
	tr.pauseMu.Unlock()

	tr.emitEvent(RunnerEvent{
		Type: EventAllResumed,
	})
}

// IsPaused returns whether a project is paused.
func (tr *TaskRunner) IsPaused(projectID string) bool {
	tr.pauseMu.RLock()
	defer tr.pauseMu.RUnlock()
	if tr.allPaused {
		return true
	}
	return tr.pauseCache[projectID]
}

// IsAllPaused returns whether all projects are globally paused.
func (tr *TaskRunner) IsAllPaused() bool {
	tr.pauseMu.RLock()
	defer tr.pauseMu.RUnlock()
	return tr.allPaused
}

// =============================================================================
// Feature Toggle
// =============================================================================

// EnableFeature adds a feature to the enabled whitelist.
// When a project is paused, the poll loop will still pick up tasks
// from enabled features.
func (tr *TaskRunner) EnableFeature(featureID string) {
	tr.pauseMu.Lock()
	tr.enabledFeatures[featureID] = true
	tr.pauseMu.Unlock()

	tr.emitEvent(RunnerEvent{
		Type:      EventFeatureEnabled,
		FeatureID: featureID,
	})
}

// DisableFeature removes a feature from the enabled whitelist.
// Running tasks continue, but no new tasks from this feature
// will be auto-picked when the project is paused.
func (tr *TaskRunner) DisableFeature(featureID string) {
	tr.pauseMu.Lock()
	delete(tr.enabledFeatures, featureID)
	tr.pauseMu.Unlock()

	tr.emitEvent(RunnerEvent{
		Type:      EventFeatureDisabled,
		FeatureID: featureID,
	})
}

// GetEnabledFeatures returns a copy of the enabled features map.
func (tr *TaskRunner) GetEnabledFeatures() map[string]bool {
	tr.pauseMu.RLock()
	defer tr.pauseMu.RUnlock()

	if len(tr.enabledFeatures) == 0 {
		return nil
	}

	cp := make(map[string]bool, len(tr.enabledFeatures))
	for k, v := range tr.enabledFeatures {
		cp[k] = v
	}
	return cp
}

// getEnabledFeatureIDs returns enabled feature IDs as a slice.
// Thread-safe — acquires pauseMu internally.
func (tr *TaskRunner) getEnabledFeatureIDs() []string {
	tr.pauseMu.RLock()
	defer tr.pauseMu.RUnlock()
	return tr.getEnabledFeatureIDsLocked()
}

// getEnabledFeatureIDsLocked returns enabled feature IDs as a slice.
// Caller MUST hold pauseMu (at least RLock).
func (tr *TaskRunner) getEnabledFeatureIDsLocked() []string {
	if len(tr.enabledFeatures) == 0 {
		return nil
	}
	ids := make([]string, 0, len(tr.enabledFeatures))
	for id := range tr.enabledFeatures {
		ids = append(ids, id)
	}
	return ids
}

// =============================================================================
// Max Parallel
// =============================================================================

// SetMaxParallel updates the maximum number of parallel tasks at runtime.
// Values <= 0 are clamped to 1.
func (tr *TaskRunner) SetMaxParallel(n int) {
	if n <= 0 {
		n = 1
	}
	tr.mu.Lock()
	tr.maxParallel = n
	tr.mu.Unlock()
}

// getMaxParallel returns the current effective max parallel limit.
// Uses the runtime-adjusted value if set, otherwise falls back to config.
func (tr *TaskRunner) getMaxParallel() int {
	tr.mu.RLock()
	n := tr.maxParallel
	tr.mu.RUnlock()
	if n > 0 {
		return n
	}
	return tr.config.MaxParallel
}

// =============================================================================
// Status
// =============================================================================

// GetStatus returns a snapshot of the runner's current state.
func (tr *TaskRunner) GetStatus() RunnerStatusInfo {
	tr.mu.RLock()
	status := tr.status
	stats := tr.stats
	startedAt := tr.startedAt
	tr.mu.RUnlock()

	tr.pauseMu.RLock()
	var paused []string
	if tr.allPaused {
		paused = make([]string, len(tr.projects))
		copy(paused, tr.projects)
	} else {
		for p := range tr.pauseCache {
			paused = append(paused, p)
		}
	}
	tr.pauseMu.RUnlock()

	return RunnerStatusInfo{
		RunnerID:    tr.runnerID,
		Status:      status,
		Projects:    tr.projects,
		Stats:       stats,
		Running:     tr.processMgr.RunningCount(),
		MaxParallel: tr.getMaxParallel(),
		Paused:      paused,
		StartedAt:   startedAt,
	}
}

// =============================================================================
// Events
// =============================================================================

// OnEvent registers an event handler.
func (tr *TaskRunner) OnEvent(handler EventHandler) {
	tr.eventMu.Lock()
	tr.handlers = append(tr.handlers, handler)
	tr.eventMu.Unlock()
}

// emitEvent sends an event to all registered handlers.
// It auto-stamps RunnerID on every event so callers don't need to set it.
func (tr *TaskRunner) emitEvent(event RunnerEvent) {
	event.RunnerID = tr.runnerID

	tr.eventMu.RLock()
	handlers := make([]EventHandler, len(tr.handlers))
	copy(handlers, tr.handlers)
	tr.eventMu.RUnlock()

	for _, h := range handlers {
		h(event)
	}
}

// emitPollComplete emits a poll_complete event with current counts.
func (tr *TaskRunner) emitPollComplete() {
	tr.emitEvent(RunnerEvent{
		Type:         EventPollComplete,
		RunningCount: tr.processMgr.RunningCount(),
	})
}

// =============================================================================
// Runner Registration & Heartbeat
// =============================================================================

// registerRunner registers this runner with the brain API server.
// Called once on startup. Failures are logged but not fatal.
func (tr *TaskRunner) registerRunner(ctx context.Context) {
	hostname, _ := os.Hostname()
	req := types.RegisterRunnerRequest{
		RunnerID:     tr.runnerID,
		Hostname:     hostname,
		Projects:     tr.projects,
		Capabilities: tr.config.Capabilities,
		MaxParallel:  tr.getMaxParallel(),
	}

	regCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	info, err := tr.client.RegisterRunner(regCtx, req)
	if err != nil {
		slog.Warn("failed to register runner with API", "runner_id", tr.runnerID, "error", err)
		return
	}
	slog.Info("runner registered with API", "runner_id", info.RunnerID, "hostname", info.Hostname)
}

// sendHeartbeat sends a heartbeat to the brain API server.
// Called periodically. Failures are logged but not fatal.
func (tr *TaskRunner) sendHeartbeat(ctx context.Context) {
	activeTasks := 0
	if tr.processMgr != nil {
		activeTasks = tr.processMgr.RunningCount()
	}

	req := types.HeartbeatRequest{
		RunnerID:    tr.runnerID,
		ActiveTasks: activeTasks,
	}

	hbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := tr.client.HeartbeatRunner(hbCtx, req); err != nil {
		slog.Debug("heartbeat failed", "runner_id", tr.runnerID, "error", err)
	}
}

// deregisterRunner removes this runner from distributed discovery during local cleanup.
func (tr *TaskRunner) deregisterRunner() {
	if tr.client == nil || tr.runnerID == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := tr.client.DeregisterRunner(ctx, tr.runnerID); err != nil {
		slog.Debug("runner deregister failed", "runner_id", tr.runnerID, "error", err)
	}
}

// =============================================================================
// Capability Filtering
// =============================================================================

// matchesCapabilities checks whether this runner has all capabilities required
// by the given task. Tasks without RequiresCapability are claimable by any runner
// (backward compatible). Returns true if the runner can handle the task.
func (tr *TaskRunner) matchesCapabilities(task *types.ResolvedTask) bool {
	if len(task.RequiresCapability) == 0 {
		return true // untagged tasks are claimable by any runner
	}
	if len(tr.config.Capabilities) == 0 {
		return false // runner has no capabilities but task requires some
	}

	// Build a set of runner capabilities for O(1) lookup
	capSet := make(map[string]bool, len(tr.config.Capabilities))
	for _, cap := range tr.config.Capabilities {
		capSet[cap] = true
	}

	// All required capabilities must be present
	for _, req := range task.RequiresCapability {
		if !capSet[req] {
			return false
		}
	}
	return true
}

// =============================================================================
// State Persistence
// =============================================================================

// saveState persists the current runner state.
func (tr *TaskRunner) saveState() {
	if tr.stateMgr == nil {
		return
	}

	tr.mu.RLock()
	status := tr.status
	stats := tr.stats
	startedAt := tr.startedAt
	tr.mu.RUnlock()

	// Collect running tasks from process manager
	var tasks []RunningTask
	for _, info := range tr.processMgr.GetAll() {
		tasks = append(tasks, info.Task)
	}

	tr.stateMgr.Save(status, tasks, stats, startedAt)
	tr.stateMgr.SaveRunningTasks(tasks)
}

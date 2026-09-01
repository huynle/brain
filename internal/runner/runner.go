package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// DefaultRenewInterval is how often the runner renews claims for running tasks.
// Set to half the lease duration (DefaultLeaseDuration = 10min) so claims are
// renewed well before they expire.
const DefaultRenewInterval = 5 * time.Minute

// ErrTaskClaimConflict indicates an expected runner race where another runner
// claimed or was assigned the task before this runner could start it.
var ErrTaskClaimConflict = errors.New("task claim conflict")

func isAutomationGeneratedTask(task *types.ResolvedTask) bool {
	return task != nil && strings.HasPrefix(task.GeneratedBy, "automation:")
}

func automationIDFromGeneratedBy(generatedBy string) string {
	return strings.TrimPrefix(generatedBy, "automation:")
}

// =============================================================================
// Interfaces for dependency injection
// =============================================================================

// TaskFetchOptions holds optional filters for fetching tasks from the Brain API.
// Both fields are optional — nil/empty means no filter (return all).
type TaskFetchOptions struct {
	FeatureIDs        []string // Filter by feature ID
	Executors         []string // Filter by executor type (e.g., "opencode", "pi")
	RunnerID          string   // Runner identity for server-side eligibility checks
	GeneratedByPrefix string   // Filter by generated_by prefix (e.g., "automation:")
}

// Client abstracts the Brain API client for testability.
type Client interface {
	CheckHealth(ctx context.Context) (APIHealth, error)
	ListProjects(ctx context.Context) ([]string, error)
	GetReadyTasks(ctx context.Context, projectID string, opts *TaskFetchOptions) ([]types.ResolvedTask, error)
	GetNextTask(ctx context.Context, projectID string, opts *TaskFetchOptions) (*types.ResolvedTask, error)
	GetAllTasks(ctx context.Context, projectID string) ([]types.ResolvedTask, error)
	GetTasksByFeature(ctx context.Context, projectID, featureID string) ([]types.ResolvedTask, error)
	ClaimTask(ctx context.Context, projectID, taskID, runnerID string) (ClaimResult, error)
	RenewClaim(ctx context.Context, projectID, taskID, runnerID string) error
	ReleaseTask(ctx context.Context, projectID, taskID, runnerID string) error
	AckDispatch(ctx context.Context, runnerID, projectID, taskID, leaseID string) (*types.DispatchAckResponse, error)
	RejectDispatch(ctx context.Context, runnerID, projectID, taskID, leaseID string, reason types.DispatchRejectReason) (*types.DispatchRejectResponse, error)
	ReleaseDispatch(ctx context.Context, runnerID, projectID, taskID string) (*types.DispatchReleaseResponse, error)
	UpdateTaskStatus(ctx context.Context, taskPath, status string) error
	AppendToTask(ctx context.Context, taskPath, content string) error
	UpdateEntry(ctx context.Context, entryPath string, updates map[string]interface{}) (*types.BrainEntry, error)
	UpdateMetadata(ctx context.Context, entryPath string, fields map[string]interface{}) error
	GetEntry(ctx context.Context, entryPath string) (*types.BrainEntry, error)
	ListEntries(ctx context.Context, params map[string]string) (*types.ListEntriesResponse, error)
	RegisterRunner(ctx context.Context, req types.RunnerRegistration) (*types.RunnerInfo, error)
	SendHeartbeat(ctx context.Context, runnerID string, req types.RunnerHeartbeatRequest) error
	DeregisterRunner(ctx context.Context, runnerID string) error
	PostTaskLogs(ctx context.Context, projectID, taskID, runnerID string, lines []types.LogLine) error
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
	UpdateSessionID(taskID string, sessionID string)
	UpdateIdleSince(taskID string, idleSince string)

	// ReserveSlot atomically holds an execution slot for a task if
	// capacity is available. Returns true when the slot is granted or
	// the task is already tracked (idempotent). Callers must call
	// ReleaseReservation (or Add, which upgrades) to free the slot.
	// Closes a race between the RunningCount check and the actual
	// spawn where multiple dispatch workers could overspend capacity.
	ReserveSlot(taskID string, maxParallel int) bool
	// ReleaseReservation frees an unspawned slot reservation. No-op
	// when the task has already been Add'd as a live process or no
	// reservation exists.
	ReleaseReservation(taskID string)
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
	Client Client

	// Executor is a single executor (backward compat). If set and Executors
	// is nil, it is registered as the "opencode" executor.
	Executor TaskExecutor

	// Executors is a named registry of executors. Tasks are dispatched to the
	// executor matching task.Executor. Takes precedence over Executor if both
	// are set.
	Executors map[string]TaskExecutor

	ProcessMgr TaskProcessManager
	StateMgr   TaskStateManager

	// EventPoster enables event forwarding to the Brain API server.
	// If nil (and Client is an *APIClient), the forwarder auto-wires.
	EventPoster EventPoster

	// ExecutorRegistry holds named executors for per-task dispatch.
	// When set, the runner resolves executor per-task using the precedence chain.
	// When nil, falls back to the single Executor field.
	ExecutorRegistry *ExecutorRegistry
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
	runnerID  string
	machineID string
	projects  []string
	config    RunnerConfig
	mode      ExecutionMode
	logger    *log.Logger

	client           Client
	executor         TaskExecutor
	executors        map[string]TaskExecutor
	executorRegistry *ExecutorRegistry
	processMgr       TaskProcessManager
	stateMgr         TaskStateManager

	// Mutable state (protected by mu)
	mu              sync.RWMutex
	status          RunnerStatus
	stats           RunnerStats
	startedAt       time.Time
	lastCronCheckAt time.Time
	maxParallel     int    // runtime-adjustable max parallel (0 = use config.MaxParallel)
	defaultModel    string // runtime-adjustable default model (empty = no override)
	lastClaimDate   string // YYYY-MM-DD of last claim, for first_task_today detection

	// Pause state (protected by pauseMu).
	//
	// Two origins are tracked separately:
	//   - Local origin (pauseCache/allPaused/automationsPaused/
	//     automationPausedProjects): set by direct calls to PauseProject/
	//     PauseAll/etc. — the embedded TUI controller and StartPaused. The
	//     server does not know about these, so they are never overwritten
	//     by reconciliation.
	//   - Server origin (serverTasksPaused/serverAutosPaused): set by SSE
	//     CommandPause/CommandResume events and reconciled wholesale from
	//     GetRunnerStatus on every poll tick in push-dispatch mode. The
	//     sentinel "" key means "globally paused". Reconciliation heals
	//     missed SSE events in both directions — without it, a missed
	//     resume event left the runner rejecting every dispatch as
	//     runner_paused until the next manual pause/resume.
	//
	// Effective pause = local OR server (see IsPaused / the dispatch gate).
	//
	// runnerPaused is a THIRD, orthogonal axis: the runner-scoped dial set
	// by PUT /runners/{runnerId}/pause (SSE command with scope="runner").
	// It deliberately lives outside the project maps above, because those
	// are replaced wholesale by syncServerPauseState from GetRunnerStatus —
	// which only reports *project* pauses. Filing a runner pause in
	// serverTasksPaused[""] (the old behavior) meant it evaporated on the
	// very next poll tick and the runner resumed acking pushed dispatches.
	// It reconciles against its own registry row instead.
	pauseMu                  sync.RWMutex
	pauseCache               map[string]bool
	allPaused                bool
	automationsPaused        bool
	automationPausedProjects map[string]bool
	serverTasksPaused        map[string]bool
	serverAutosPaused        map[string]bool
	runnerPaused             bool

	// Event handlers (protected by eventMu)
	eventMu  sync.RWMutex
	handlers []EventHandler

	// Feature lifecycle tracking
	featureTracker *FeatureTracker

	// Event hook dispatcher (pre/post lifecycle hooks)
	hookDispatcher *HookDispatcher

	// Event forwarding to Brain API server
	eventForwarder *EventForwarder

	// Log streamers (protected by logMu)
	logMu        sync.Mutex
	logStreamers map[string]*LogStreamer // keyed by task ID

	// SSE reactive polling
	wakeCh      chan struct{}
	commandCh   chan RunnerCommand
	sseListener *SSEListener

	// dispatchPool decouples CommandDispatch processing from the SSE
	// consumer goroutine so the consumer can drain commandCh at line
	// rate even while dispatches wait on HTTP calls in
	// handleDispatchCommand. See dispatch_pool.go.
	//
	// nil until startDispatchPool is called (allows tests to invoke
	// handleCommand directly without setting up a full runner).
	dispatchPoolMu sync.RWMutex
	dispatchPool   *dispatchPool

	// sessionClaimMu serializes session discovery across this runner's tasks
	// so two concurrent discoveries cannot claim the same OpenCode session
	// (see claimDiscoveredSession).
	sessionClaimMu sync.Mutex

	// Remote-control bridge (outbound WS tunnel to the Brain API)
	bridgeClient *BridgeClient

	// Lifecycle
	cancel context.CancelFunc
	done   chan struct{}
}

// NewTaskRunner creates a new TaskRunner with the given options.
func NewTaskRunner(opts TaskRunnerOptions) *TaskRunner {
	// Resolve stable identity: a machine-wide id shared by all runners on this
	// host, and a per-runner id persisted in the state dir so a restart
	// re-registers under the same id (remote control relies on this to find
	// the machine that ran a past session).
	runnerID := ResolveRunnerID(opts.Config.StateDir)
	machineID := ResolveMachineID()

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

	// Build executor registry.
	executorRegistry := opts.ExecutorRegistry
	if executorRegistry == nil && (len(opts.Executors) > 0 || opts.Executor != nil) {
		executorRegistry = &ExecutorRegistry{
			executors: make(map[string]TaskExecutor),
			config:    opts.Config,
		}
		for name, exec := range opts.Executors {
			executorRegistry.executors[name] = exec
		}
		if opts.Executor != nil {
			if _, exists := executorRegistry.executors[DefaultExecutorName]; !exists {
				executorRegistry.executors[DefaultExecutorName] = opts.Executor
			}
		}
	}

	defaultExecutor := opts.Executor
	if defaultExecutor == nil && executorRegistry != nil {
		if exec, ok := executorRegistry.Get(DefaultExecutorName); ok {
			defaultExecutor = exec
		}
	}

	tr := &TaskRunner{
		runnerID:                 runnerID,
		machineID:                machineID,
		projects:                 projects,
		config:                   opts.Config,
		mode:                     mode,
		logger:                   logger,
		client:                   opts.Client,
		executor:                 defaultExecutor,
		executors:                nil,
		executorRegistry:         executorRegistry,
		processMgr:               opts.ProcessMgr,
		stateMgr:                 opts.StateMgr,
		status:                   RunnerStatusIdle,
		pauseCache:               make(map[string]bool),
		automationPausedProjects: make(map[string]bool),
		serverTasksPaused:        make(map[string]bool),
		serverAutosPaused:        make(map[string]bool),
		logStreamers:             make(map[string]*LogStreamer),
		wakeCh:                   make(chan struct{}, 1),
		commandCh:                make(chan RunnerCommand, commandChannelCapacity(opts.Config.MaxParallel)),
		done:                     make(chan struct{}),
	}
	if executorRegistry != nil {
		tr.executors = executorRegistry.executors
	}

	if opts.StartPaused {
		tr.allPaused = true
		tr.automationsPaused = true
	}

	// Wire FeatureTracker to emit feature lifecycle events.
	// The tracker registers itself as an OnEvent handler so it
	// receives task events and emits feature-level events back.
	if tr.client != nil {
		tr.featureTracker = NewFeatureTracker(tr.client, tr.emitEvent)
		tr.OnEvent(tr.featureTracker.HandleEvent)
	}

	// Wire HookDispatcher to execute lifecycle hook scripts.
	// The dispatcher loads hooks from both the hooks directory (executable scripts)
	// and inline hook definitions from config. Inline hooks take precedence.
	{
		hookTimeout := time.Duration(opts.Config.HookTimeout) * time.Second
		if hookTimeout <= 0 {
			hookTimeout = 30 * time.Second
		}
		hd, err := NewHookDispatcherWithConfig(opts.Config.Hooks, hookTimeout)
		if err != nil {
			logger.Printf("WARNING: failed to initialize hook dispatcher: %v", err)
		} else {
			tr.hookDispatcher = hd
			// Register an event handler that dispatches hooks for feature lifecycle events.
			// Feature events are emitted by the FeatureTracker and forwarded to hooks here.
			tr.OnEvent(tr.handleFeatureHookEvents)
		}
	}

	// Wire EventForwarder to batch-POST runner events to the API server.
	// Use explicit EventPoster if provided, otherwise try the Client.
	poster := opts.EventPoster
	if poster == nil {
		if apiClient, ok := tr.client.(*APIClient); ok {
			poster = apiClient
		}
	}
	if poster != nil {
		tr.eventForwarder = NewEventForwarder(poster, EventForwarderConfig{})
		tr.OnEvent(tr.eventForwarder.Handle)
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

	// Start event forwarder (must start before emitting events)
	if tr.eventForwarder != nil {
		tr.eventForwarder.Start(ctx)
	}

	// Register with the Brain API (non-fatal on failure for backward compat)
	tr.registerWithAPI(ctx)

	// Reap orphaned in_progress tasks left behind by a previous runner crash.
	// Runs after registerWithAPI so we have a stable runnerID, but before the
	// poll loop so orphans don't distort automation max_concurrent counting.
	// Non-fatal on failure — logs and continues.
	tr.reapOrphanedTasks(ctx)

	tr.emitEvent(RunnerEvent{
		Type:     EventRunnerStarted,
		Projects: tr.getProjects(),
		Mode:     string(tr.mode),
	})

	// Emit runner.started to the domain event bus when the API supports it.
	if emitter, ok := tr.client.(interface {
		EmitEvent(context.Context, string, map[string]any, string) error
	}); ok {
		go func() {
			emitCtx, emitCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer emitCancel()
			_ = emitter.EmitEvent(emitCtx, "runner.started", map[string]any{
				"runner_id": tr.runnerID,
				"projects":  tr.getProjects(),
				"mode":      string(tr.mode),
			}, "runner-started-"+tr.runnerID)
		}()
	}

	pollInterval := time.Duration(tr.config.PollInterval) * time.Second
	if pollInterval < time.Second {
		pollInterval = time.Second
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Start the dispatch worker pool before wiring up the SSE listener.
	// Once the SSE listener is running, dispatch commands can start
	// arriving on commandCh, and the consumer goroutine will submit
	// them to this pool for async processing. Starting the pool first
	// avoids a race where dispatches arrive before workers exist.
	tr.startDispatchPool(ctx)

	// Start SSE listener for reactive polling
	if tr.config.BrainAPIURL != "" {
		initialProjects := tr.getProjects()
		if len(initialProjects) > 0 {
			tr.sseListener = NewSSEListener(
				tr.config.BrainAPIURL,
				tr.config.APIToken,
				initialProjects,
				tr.wakeCh,
			)
			// Wire up runner-scoped command stream
			tr.sseListener.SetRunnerStream(tr.runnerID, tr.commandCh)
			go tr.sseListener.Start(ctx)
			slog.Info("SSE listener started for reactive polling",
				"projects", len(initialProjects),
				"runner_stream", true,
				"runner_id", tr.runnerID,
			)
		}
	}

	// Start the remote-control bridge (outbound WS; reconnects internally)
	if tr.config.BrainAPIURL != "" && !tr.config.Control.Disabled {
		tr.bridgeClient = NewBridgeClient(tr)
		go tr.bridgeClient.Start(ctx)
		slog.Info("bridge client started", "runner_id", tr.runnerID)
	}

	// Start periodic claim renewal goroutine
	renewTicker := time.NewTicker(DefaultRenewInterval)
	go func() {
		defer renewTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-renewTicker.C:
				tr.renewClaims(ctx)
			}
		}
	}()

	// Start heartbeat goroutine
	heartbeatInterval := time.Duration(tr.config.HeartbeatInterval) * time.Second
	if heartbeatInterval < time.Second {
		heartbeatInterval = 30 * time.Second
	}
	heartbeatTicker := time.NewTicker(heartbeatInterval)
	go func() {
		defer heartbeatTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-heartbeatTicker.C:
				tr.sendHeartbeat(ctx)
			}
		}
	}()

	// Start periodic project-list refresh goroutine so projects created
	// after the runner started become visible without a restart
	// (Finding 2 from projects/brain/plan/24urhmtl.md, task wcg6lxfz).
	// Interval <= 0 disables refresh entirely.
	refreshInterval := time.Duration(tr.config.ProjectRefreshInterval) * time.Second
	if refreshInterval > 0 {
		go tr.runProjectRefreshLoop(ctx, refreshInterval)
	}

	// Start command consumer goroutine. Dispatched commands MUST be
	// processed independently of poll() because poll() makes synchronous
	// HTTP calls that can block for tens of seconds when the API is slow
	// (e.g. a SQLite WAL stall or a CPU-bound JSON encode on the server
	// side). Multiplexing poll and commandCh on the same goroutine caused
	// the production wedge documented in brain plan ehwvfq8e and the
	// 2026-06-25 goroutine-dump analysis: a blocked poll() would prevent
	// commandCh from being drained, the SSE listener's 16-slot buffer
	// would fill in seconds, and every subsequent dispatch lease would
	// time out untouched ("dispatch command dropped (channel full)").
	//
	// Keeping this goroutine narrow (only commandCh + ctx.Done) makes
	// dispatch consumption durable against any future slowness in the
	// maintenance path.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case cmd := <-tr.commandCh:
				tr.handleCommand(ctx, cmd)
			}
		}
	}()

	// Run initial poll immediately
	tr.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			tr.mu.Lock()
			tr.status = RunnerStatusStopped
			tr.mu.Unlock()
			// Stop the dispatch pool with a bounded grace period so
			// in-flight dispatches (spawn, ack, HTTP calls) get a
			// chance to finish cleanly. Bounded so shutdown can't
			// hang forever on a stuck handler.
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			tr.stopDispatchPool(stopCtx)
			stopCancel()
			tr.deregisterFromAPI()
			tr.saveState()
			close(tr.done)
			return nil
		case <-ticker.C:
			tr.poll(ctx)
		case <-tr.wakeCh:
			tr.poll(ctx)
		}
	}
}

// commandChannelCapacity sizes the SSE runner-command buffer. The dispatch
// pool runs maxParallel+2 workers, so a burst of dispatches for a large
// runner must not be bottlenecked by a fixed 16-slot channel — commands
// that don't fit are dropped by the SSE listener and only self-heal after
// the lease TTL.
func commandChannelCapacity(maxParallel int) int {
	if cap := 2 * maxParallel; cap > 16 {
		return cap
	}
	return 16
}

// =============================================================================
// Project list management
// =============================================================================
//
// tr.projects is treated as an immutable list at construction time by all the
// read sites in this package, but the runner needs to pick up new projects
// created after startup (Finding 2 from projects/brain/plan/24urhmtl.md).
// getProjects/setProjects/refreshProjects serialize access under tr.mu and
// return defensive copies so callers can iterate freely without worrying
// about concurrent mutation.

// getProjects returns a defensive copy of the current project list. Safe to
// range over concurrently with refreshProjects.
func (tr *TaskRunner) getProjects() []string {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	if len(tr.projects) == 0 {
		return nil
	}
	out := make([]string, len(tr.projects))
	copy(out, tr.projects)
	return out
}

// refreshProjects fetches the current project list from the Brain API, applies
// the runner's include/exclude filters, diffs against the current list, and
// atomically updates tr.projects. Returns the added and removed project IDs
// for logging. On API failure the current list is preserved — the caller must
// keep polling projects it already knows about.
//
// Fixes Finding 2 (wcg6lxfz): projects created after `brain api start --runner`
// starts were previously invisible until restart.
func (tr *TaskRunner) refreshProjects(ctx context.Context) (added, removed []string, err error) {
	if tr.client == nil {
		return nil, nil, nil
	}
	projects, err := tr.client.ListProjects(ctx)
	if err != nil {
		return nil, nil, err
	}
	filtered := FilterProjects(projects, tr.config.IncludeProjects, tr.config.ExcludeProjects)

	// Diff under the lock so concurrent readers see either the old or new
	// list, never a torn intermediate state.
	tr.mu.Lock()
	current := tr.projects
	currentSet := make(map[string]bool, len(current))
	for _, p := range current {
		currentSet[p] = true
	}
	nextSet := make(map[string]bool, len(filtered))
	for _, p := range filtered {
		nextSet[p] = true
		if !currentSet[p] {
			added = append(added, p)
		}
	}
	for _, p := range current {
		if !nextSet[p] {
			removed = append(removed, p)
		}
	}
	if len(added) > 0 || len(removed) > 0 {
		// Store a fresh slice so external callers holding the old
		// snapshot see a stable view.
		next := make([]string, len(filtered))
		copy(next, filtered)
		tr.projects = next
	}
	tr.mu.Unlock()
	return added, removed, nil
}

// runProjectRefreshLoop periodically calls refreshProjects on the configured
// interval and logs added/removed projects. Exits when ctx is cancelled.
// A refresh interval <= 0 disables the loop entirely (call site should skip
// the goroutine, but this guard belts-and-braces the ticker construction).
func (tr *TaskRunner) runProjectRefreshLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			added, removed, err := tr.refreshProjects(ctx)
			if err != nil {
				slog.Warn("project refresh failed",
					"runner_id", tr.runnerID,
					"error", err,
				)
				continue
			}
			if len(added) > 0 {
				slog.Info("project refresh: new projects visible",
					"runner_id", tr.runnerID,
					"added", added,
				)
			}
			if len(removed) > 0 {
				// Warn (not info): mid-flight tasks are not dropped
				// here — the poll loop simply stops considering the
				// project for NEW work. Running claims are renewed
				// and completed by their own path.
				slog.Warn("project refresh: projects no longer listed",
					"runner_id", tr.runnerID,
					"removed", removed,
				)
			}
		}
	}
}

// fetchServerPauseState loads the server's pause switches. The third return
// is false when the state could not be fetched (no client, unsupported
// client, or HTTP error) — callers that reconcile cached state must keep
// the previous snapshot in that case rather than treating "unknown" as
// "nothing paused".
func (tr *TaskRunner) fetchServerPauseState(ctx context.Context) (map[string]bool, map[string]bool, bool) {
	tasksPaused := make(map[string]bool)
	automationsPaused := make(map[string]bool)
	if tr.client == nil {
		return tasksPaused, automationsPaused, false
	}
	statusClient, ok := tr.client.(interface {
		GetRunnerStatus(context.Context) (*types.RunnerStatusResponse, error)
	})
	if !ok {
		return tasksPaused, automationsPaused, false
	}
	status, err := statusClient.GetRunnerStatus(ctx)
	if err != nil || status == nil {
		if err != nil {
			tr.logger.Printf("get server pause state failed: %v", err)
		}
		return tasksPaused, automationsPaused, false
	}
	// Only trust the explicit per-project lists. `status.Paused` and
	// `status.AutomationsPaused` are DERIVED aggregates on the API side
	// (`len(pausedProjects) > 0` — see service/runner.go:GetStatus), not a
	// distinct "global pause" switch. Promoting them into the sentinel ""
	// key caused every project to appear paused whenever any unrelated
	// project was paused: e.g. 43 stale project pauses would make
	// serverPausedFor("personal-productivity") return true and the dispatch
	// gate would reject every lease with runner_paused, including
	// automation-generated tasks whose per-project autos were on.
	//
	// A genuine "pause everything" is expressed by every project being
	// listed in PausedProjects (SetAllProjectTasksPaused writes a row per
	// project); the loops below already convey that.
	for _, projectID := range status.PausedProjects {
		tasksPaused[projectID] = true
	}
	for _, projectID := range status.AutomationPausedProjects {
		automationsPaused[projectID] = true
	}
	return tasksPaused, automationsPaused, true
}

// syncServerPauseState reconciles the server-origin pause snapshot from
// GetRunnerStatus. Called on every poll tick in push-dispatch mode, where
// the poll loop's own pause fetch is skipped by the early return — without
// this the dispatch gate ran entirely on SSE-event state and a single
// missed CommandPause/CommandResume left it permanently stale. On fetch
// failure the previous snapshot is kept: SSE events keep applying on top
// of it, and "unknown" must not be read as "everything resumed".
func (tr *TaskRunner) syncServerPauseState(ctx context.Context) {
	// The runner-scoped dial reconciles independently of the project
	// snapshot: it lives on this runner's registry row, not in
	// project_pause_state, and a failure to read one must not skip the
	// other.
	tr.syncRunnerPauseState(ctx)

	tasksPaused, automationsPaused, ok := tr.fetchServerPauseState(ctx)
	if !ok {
		return
	}
	tr.pauseMu.Lock()
	tr.serverTasksPaused = tasksPaused
	tr.serverAutosPaused = automationsPaused
	tr.pauseMu.Unlock()
}

// syncRunnerPauseState reconciles the runner-scoped pause dial against this
// runner's own registry row, healing SSE pause/resume events lost across a
// stream reconnect and picking up a pause set while the runner was down. On
// fetch failure the current dial is kept — "unknown" is not "resumed".
func (tr *TaskRunner) syncRunnerPauseState(ctx context.Context) {
	paused, ok := tr.fetchRunnerPauseState(ctx)
	if !ok {
		return
	}
	tr.pauseMu.Lock()
	changed := tr.runnerPaused != paused
	tr.runnerPaused = paused
	tr.pauseMu.Unlock()
	if !changed {
		return
	}
	slog.Info("runner pause dial reconciled from server",
		"runner_id", tr.runnerID, "paused", paused)
	if paused {
		tr.emitEvent(RunnerEvent{Type: EventAllPaused})
	} else {
		tr.emitEvent(RunnerEvent{Type: EventAllResumed})
		tr.wake()
	}
}

// fetchRunnerPauseState reads the runner-scoped pause flag from this runner's
// registry record. The second return is false when the state could not be
// read (no client, a client that predates GetRunner, or an HTTP error), in
// which case callers must keep whatever they already had.
func (tr *TaskRunner) fetchRunnerPauseState(ctx context.Context) (bool, bool) {
	if tr.client == nil || tr.runnerID == "" {
		return false, false
	}
	fetcher, ok := tr.client.(interface {
		GetRunner(context.Context, string) (*types.RunnerInfo, error)
	})
	if !ok {
		return false, false
	}
	info, err := fetcher.GetRunner(ctx, tr.runnerID)
	if err != nil {
		tr.logger.Printf("get runner pause state failed: %v", err)
		return false, false
	}
	if info == nil {
		return false, false
	}
	return info.Paused, true
}

// serverPausedFor returns true when the server has either paused all tasks
// or paused this specific project. Pairs with serverPauseState's sentinel
// "" key for global state.
func serverPausedFor(serverPaused map[string]bool, projectID string) bool {
	return serverPaused[""] || serverPaused[projectID]
}

func (tr *TaskRunner) dispatchLeaseExpired(expiresAt string, now time.Time) bool {
	if strings.TrimSpace(expiresAt) == "" {
		return false
	}
	if t, err := time.Parse(time.RFC3339, expiresAt); err == nil {
		return !t.After(now)
	}
	n, err := strconv.ParseInt(expiresAt, 10, 64)
	if err != nil {
		return false
	}
	if n > 1_000_000_000_000 {
		return n <= now.UnixMilli()
	}
	return n <= now.Unix()
}

func (tr *TaskRunner) supportsProject(projectID string) bool {
	for _, p := range tr.getProjects() {
		if p == projectID || p == "all" {
			return true
		}
	}
	return false
}

// handleDispatchCommand executes a CommandDispatch synchronously. This
// function was extracted from the CommandDispatch case in handleCommand
// so the async dispatch worker pool (see dispatch_pool.go) can invoke it
// off the SSE consumer goroutine. Behavior is unchanged from the inline
// version — the only difference is that the caller decides whether to
// run it inline or on a worker.
//
// The break statements from the original switch case become returns.
func (tr *TaskRunner) handleDispatchCommand(ctx context.Context, cmd RunnerCommand) {
	slog.Info("dispatch command received",
		"task_id", cmd.TaskID,
		"project_id", cmd.ProjectID,
		"lease_id", cmd.LeaseID,
		"force", cmd.Force,
		"runner_id", tr.runnerID,
	)
	leaseID := cmd.LeaseID
	if leaseID == "" {
		leaseID = cmd.Lease
	}
	if leaseID == "" || cmd.ProjectID == "" || cmd.TaskID == "" {
		tr.rejectDispatch(ctx, cmd, leaseID, "missing_fields", "dispatch command missing required leaseId, projectId, or taskId")
		return
	}
	if ctx.Err() != nil {
		tr.rejectDispatch(ctx, cmd, leaseID, "runner_shutting_down", ctx.Err().Error())
		return
	}
	if tr.dispatchLeaseExpired(cmd.ExpiresAt, time.Now()) {
		tr.rejectDispatch(ctx, cmd, leaseID, "lease_expired", "dispatch lease has expired")
		return
	}
	if !tr.supportsProject(cmd.ProjectID) {
		tr.rejectDispatch(ctx, cmd, leaseID, "project_unsupported", "runner is not assigned to this project")
		return
	}
	// Pause gating: when the user explicitly forced this run via /run
	// (cmd.Force=true), bypass the pause check to honor the contract
	// documented on SchedulerService.RunTaskNow. Capacity is still
	// enforced — a forced dispatch can't magically create execution
	// slots, and overrunning max_parallel would corrupt accounting.
	//
	// When NOT forced and tasks are paused, we still need to allow
	// automation-generated tasks through if automations aren't paused
	// for this project — mirroring the poll loop's carve-out at
	// runner.go:956–984. The poll path pulls "automation:" tasks even
	// when paused; the dispatch path historically rejected everything
	// as runner_paused, so the PWA's "tasks: paused, autos: on" mode
	// quietly stopped delivering automation work whenever dispatch
	// push was enabled. We can't decide here yet because we don't
	// have the task's GeneratedBy until we've resolved the task.
	//
	// Pause state comes from local caches only — no HTTP round-trip per
	// dispatch, which used to burn ~200ms per dispatch against a slow API
	// and caused task_lookup_failed rejections in production (see
	// fix/inline-dispatch-task). Server-origin state arrives via SSE
	// CommandPause/CommandResume and is reconciled from GetRunnerStatus on
	// every poll tick (syncServerPauseState), so a missed event heals
	// within one poll interval.
	tr.pauseMu.RLock()
	runnerPaused := tr.runnerPaused
	paused := tr.allPaused || tr.pauseCache[cmd.ProjectID] ||
		serverPausedFor(tr.serverTasksPaused, cmd.ProjectID)
	automationsPausedForProject := tr.automationsPaused || tr.automationPausedProjects[cmd.ProjectID] ||
		serverPausedFor(tr.serverAutosPaused, cmd.ProjectID)
	tr.pauseMu.RUnlock()
	// A runner-scoped pause is absolute: no automation carve-out (that
	// exists to keep a project's automations flowing while its manual tasks
	// are paused — meaningless when the whole runner is meant to be idle)
	// and no force override (the scheduler already treats a paused runner as
	// ineligible, so a forced dispatch arriving here means something is out
	// of sync, not that the user picked this runner). Reject before
	// reserving a slot so nothing needs unwinding.
	if runnerPaused {
		tr.rejectDispatch(ctx, cmd, leaseID, "runner_paused", "runner is paused")
		return
	}
	if paused && cmd.Force {
		slog.Info("force-dispatch bypassing pause",
			"task_id", cmd.TaskID,
			"project_id", cmd.ProjectID,
			"all_paused", tr.allPaused,
			"project_paused", tr.pauseCache[cmd.ProjectID],
		)
	}
	if !tr.processMgr.ReserveSlot(cmd.TaskID, tr.getMaxParallel()) {
		tr.rejectDispatch(ctx, cmd, leaseID, "capacity_unavailable", "runner has no available execution slots")
		return
	}
	// From here on, any rejection must release the reservation.
	// rejectDispatch calls ReleaseReservation internally so this
	// happens automatically. The reservation is upgraded to a live
	// process by ProcessManager.Add inside claimAndSpawn on success.

	// Prefer the task inlined in the dispatch payload — the scheduler
	// has the fully-resolved ResolvedTask when it creates the lease, so
	// there's no need to fetch it again. Fall back to GetReadyTasks
	// only when the payload doesn't carry the task (older API server).
	var task *types.ResolvedTask
	if cmd.Task != nil && cmd.Task.ID == cmd.TaskID {
		task = cmd.Task
	} else {
		tasks, err := tr.client.GetReadyTasks(ctx, cmd.ProjectID, tr.buildFetchOptions())
		if err != nil {
			tr.rejectDispatch(ctx, cmd, leaseID, "task_lookup_failed", err.Error())
			return
		}
		for i := range tasks {
			if tasks[i].ID == cmd.TaskID {
				task = &tasks[i]
				break
			}
		}
		if task == nil {
			tr.rejectDispatch(ctx, cmd, leaseID, "task_not_found", "target task is not ready for this runner")
			return
		}
		slog.Debug("dispatch used legacy fetch path (task not inlined in payload)",
			"task_id", cmd.TaskID, "project_id", cmd.ProjectID)
	}
	// Now we know the task's GeneratedBy; finalize the pause decision.
	// Automation-generated tasks may proceed when only tasks (not
	// automations) are paused for this project. Everything else gets
	// the unconditional runner_paused rejection.
	if paused && !cmd.Force {
		if !(isAutomationGeneratedTask(task) && !automationsPausedForProject) {
			tr.rejectDispatch(ctx, cmd, leaseID, "runner_paused", "runner is paused")
			return
		}
		slog.Info("automation dispatch bypassing pause",
			"task_id", cmd.TaskID,
			"project_id", cmd.ProjectID,
			"generated_by", task.GeneratedBy,
		)
	}
	taskExecutor, _, err := tr.resolveExecutor(task)
	if err != nil {
		tr.rejectDispatch(ctx, cmd, leaseID, "executor_unsupported", err.Error())
		return
	}
	workdir, err := taskExecutor.ResolveWorkdir(task)
	if err != nil {
		tr.rejectDispatch(ctx, cmd, leaseID, "workdir_unavailable", err.Error())
		return
	}
	if !tr.ackDispatch(ctx, cmd, leaseID) {
		// The ack failed (network error, or the server refused because the
		// lease expired/was reassigned). The lease self-heals server-side via
		// TTL expiry, but the local slot reservation must be released here or
		// this runner permanently loses an execution slot per failed ack.
		tr.processMgr.ReleaseReservation(cmd.TaskID)
		slog.Warn("dispatch ack failed; aborting spawn",
			"task_id", cmd.TaskID,
			"project_id", cmd.ProjectID,
			"lease_id", leaseID,
		)
		return
	}
	slog.Info("dispatch acked; spawning task",
		"task_id", cmd.TaskID,
		"project_id", cmd.ProjectID,
		"lease_id", leaseID,
	)
	if err := tr.claimAndSpawnWithWorkdir(ctx, task, cmd.ProjectID, workdir); err != nil {
		// Spawn failed after the ack: release both the API-side lease
		// and the local capacity reservation so the runner can accept
		// new work in this slot.
		tr.processMgr.ReleaseReservation(cmd.TaskID)
		tr.releaseDispatchLease(ctx, cmd.ProjectID, cmd.TaskID)
		tr.logger.Printf("claim and spawn dispatched task failed for %s/%s: %v", cmd.ProjectID, cmd.TaskID, err)
		slog.Error("spawn failed",
			"task_id", cmd.TaskID,
			"project_id", cmd.ProjectID,
			"lease_id", leaseID,
			"runner_id", tr.runnerID,
			"path", "dispatch",
			"error", err.Error(),
		)
	}
}

func (tr *TaskRunner) rejectDispatch(ctx context.Context, cmd RunnerCommand, leaseID, code, message string) {
	// If the dispatch had reached the reserve-slot step, free that
	// slot so subsequent dispatches see accurate capacity. Safe no-op
	// when no reservation exists (rejections before ReserveSlot).
	if cmd.TaskID != "" {
		tr.processMgr.ReleaseReservation(cmd.TaskID)
	}
	// Always log the rejection so operators can diagnose why a dispatch
	// fell on the floor — previously the HTTP error was discarded, making
	// "lease stuck in pushed" mysteries impossible to track down.
	slog.Warn("rejecting dispatch",
		"code", code,
		"message", message,
		"task_id", cmd.TaskID,
		"project_id", cmd.ProjectID,
		"lease_id", leaseID,
		"force", cmd.Force,
		"runner_id", tr.runnerID,
	)
	if _, err := tr.client.RejectDispatch(ctx, tr.runnerID, cmd.ProjectID, cmd.TaskID, leaseID, types.DispatchRejectReason{
		Code:    code,
		Message: message,
	}); err != nil {
		// A failed reject leaves the lease in `pushed` until its TTL
		// expires — surface that explicitly so it shows up in runner logs
		// instead of being silently swallowed.
		slog.Error("failed to report dispatch rejection to brain-api",
			"error", err,
			"code", code,
			"task_id", cmd.TaskID,
			"project_id", cmd.ProjectID,
			"lease_id", leaseID,
			"runner_id", tr.runnerID,
		)
	}
}

func (tr *TaskRunner) ackDispatch(ctx context.Context, cmd RunnerCommand, leaseID string) bool {
	resp, err := tr.client.AckDispatch(ctx, tr.runnerID, cmd.ProjectID, cmd.TaskID, leaseID)
	return err == nil && (resp == nil || resp.Success)
}

func (tr *TaskRunner) releaseDispatchLease(ctx context.Context, projectID, taskID string) {
	if tr.client == nil || projectID == "" || taskID == "" {
		return
	}
	if _, err := tr.client.ReleaseDispatch(ctx, tr.runnerID, projectID, taskID); err != nil {
		tr.logger.Printf("release dispatch lease failed for %s/%s: %v", projectID, taskID, err)
	}
}

func (tr *TaskRunner) releaseDispatchLeasesForRunningTasks(ctx context.Context) {
	if tr.processMgr == nil {
		return
	}
	for _, info := range tr.processMgr.GetAll() {
		tr.releaseDispatchLease(ctx, info.Task.ProjectID, info.Task.ID)
	}
}

// handleCommand processes a server-pushed command received via the runner SSE stream.
func (tr *TaskRunner) handleCommand(ctx context.Context, cmd RunnerCommand) {
	slog.Info("handling runner command", "type", cmd.Type, "runner_id", tr.runnerID)

	switch cmd.Type {
	case CommandAffinityUpdated:
		tr.mu.Lock()
		tr.config.FeatureIDs = cmd.FeatureIDs
		tr.mu.Unlock()
		slog.Info("affinity updated", "feature_ids", cmd.FeatureIDs)

	case CommandConfigUpdated:
		if cmd.MaxParallel != nil {
			tr.SetMaxParallel(*cmd.MaxParallel)
			slog.Info("max parallel updated", "max_parallel", *cmd.MaxParallel)
		}
		if cmd.Model != "" {
			tr.mu.Lock()
			tr.config.Opencode.Model = cmd.Model
			tr.mu.Unlock()
			slog.Info("model updated", "model", cmd.Model)
		}
		if cmd.Agent != "" {
			tr.mu.Lock()
			tr.config.Opencode.Agent = cmd.Agent
			tr.mu.Unlock()
			slog.Info("agent updated", "agent", cmd.Agent)
		}

	case CommandPause:
		tr.applyPauseCommand(cmd, true)

	case CommandResume:
		tr.applyPauseCommand(cmd, false)

	case CommandDispatch:
		// Route through the dispatch worker pool when it's running so
		// the SSE consumer goroutine can return to draining commandCh
		// immediately, even under HTTP burst load. When the pool is
		// not running (test scenarios that call handleCommand directly
		// on a freshly-constructed TaskRunner), fall through to inline
		// execution so tests observe ack/spawn side effects
		// synchronously.
		if pool := tr.getDispatchPool(); pool != nil {
			if err := pool.Submit(cmd); err != nil {
				// Bounded pool overflowed. Reject the lease
				// synchronously with a real reason so the scheduler
				// can record it as a placement failure and try
				// elsewhere, instead of the historical silent drop
				// that left leases stuck in `pushed` state.
				leaseID := cmd.LeaseID
				if leaseID == "" {
					leaseID = cmd.Lease
				}
				tr.rejectDispatch(ctx, cmd, leaseID, "dispatch_backlog_full", "runner dispatch queue is full")
			}
			break
		}
		tr.handleDispatchCommand(ctx, cmd)

	case CommandShutdown:
		slog.Info("shutdown command received", "reason", cmd.Reason)
		tr.emitEvent(RunnerEvent{
			Type:   EventShutdown,
			Reason: cmd.Reason,
		})
		if tr.cancel != nil {
			tr.cancel()
		}

	default:
		slog.Warn("unknown runner command type", "type", cmd.Type)
	}
}

// Stop gracefully shuts down the runner.
func (tr *TaskRunner) Stop() error {
	if tr.cancel != nil {
		tr.cancel()
	}

	// Wait for the poll loop to exit
	<-tr.done

	// Deregister from the Brain API (best-effort)
	tr.deregisterFromAPI()

	// Stop the dispatch pool if it wasn't already stopped by the poll
	// loop's ctx.Done path (idempotent — no-op when already stopped).
	tr.stopDispatchPool(context.Background())

	// Stop SSE listener
	if tr.sseListener != nil {
		tr.sseListener.Stop()
		slog.Info("SSE listener stopped")
	}

	// Release/finalize dispatch leases before killing running tasks.
	tr.releaseDispatchLeasesForRunningTasks(context.Background())

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

	// Emit shutdown event
	tr.emitEvent(RunnerEvent{
		Type:   EventShutdown,
		Reason: "graceful shutdown",
	})

	// Stop event forwarder (drains remaining events after shutdown event)
	if tr.eventForwarder != nil {
		tr.eventForwarder.Stop()
	}

	// Save final state
	tr.saveState()

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

	// Passive dispatch-capable runners wait for Brain-assigned dispatch leases
	// instead of actively polling /next for work. Lifecycle maintenance above
	// still runs on every poll tick — including the server pause snapshot,
	// which the dispatch gate consults. Reconciling it here heals any SSE
	// pause/resume events lost across a stream reconnect.
	if tr.dispatchPushEnabled() {
		tr.syncServerPauseState(ctx)
		tr.saveState()
		tr.emitPollComplete()
		return
	}

	// 3. Check capacity
	running := tr.processMgr.RunningCount()
	maxParallel := tr.getMaxParallel()
	if running >= maxParallel {
		tr.emitPollComplete()
		return
	}

	// 4. Check if paused — refresh the server-origin snapshot (kept on fetch
	// failure) and read the cached maps, so pause state delivered via SSE
	// commands survives a transient GetRunnerStatus error.
	tr.syncServerPauseState(ctx)

	// 4a. A runner-scoped pause stops the poll path outright — no per-project
	// carve-outs, no enabled-feature exception. The reconcile above ran first
	// so a resume still lands on the very next tick.
	if tr.IsRunnerPaused() {
		tr.emitPollComplete()
		return
	}

	tr.pauseMu.RLock()
	allPaused := tr.allPaused
	automationsPaused := tr.automationsPaused
	automationPausedProjects := make(map[string]bool, len(tr.automationPausedProjects))
	for projectID, paused := range tr.automationPausedProjects {
		automationPausedProjects[projectID] = paused
	}
	serverTaskPaused := make(map[string]bool, len(tr.serverTasksPaused))
	for projectID, paused := range tr.serverTasksPaused {
		serverTaskPaused[projectID] = paused
	}
	serverAutomationPaused := make(map[string]bool, len(tr.serverAutosPaused))
	for projectID, paused := range tr.serverAutosPaused {
		serverAutomationPaused[projectID] = paused
	}
	tr.pauseMu.RUnlock()

	// 5. Fill available slots
	slotsAvailable := maxParallel - running
	filled := 0

	for _, projectID := range tr.getProjects() {
		if ctx.Err() != nil {
			break
		}
		if filled >= slotsAvailable {
			break
		}

		// Skip paused projects. Automation tasks are the one carve-out.
		tr.pauseMu.RLock()
		paused := tr.pauseCache[projectID]
		tr.pauseMu.RUnlock()

		automationsPausedForProject := automationsPaused || automationPausedProjects[projectID] || serverPausedFor(serverAutomationPaused, projectID)

		if paused || allPaused || serverPausedFor(serverTaskPaused, projectID) {
			if !automationsPausedForProject {
				task, err := tr.client.GetNextTask(ctx, projectID, &TaskFetchOptions{
					GeneratedByPrefix: "automation:",
					Executors:         tr.executorNames(),
					RunnerID:          tr.runnerID,
				})
				if err == nil && task != nil {
					if !tr.matchesCapabilities(task) {
						slog.Debug("skipping automation task: runner lacks required capabilities",
							"task_id", task.ID,
							"project", projectID,
							"requires", task.RequiresCapability,
							"runner_capabilities", tr.config.Capabilities,
						)
					} else if ok, err := tr.canStartAutomationTask(ctx, task); err != nil {
						tr.logger.Printf("automation max_concurrent check failed for %s/%s: %v", projectID, task.ID, err)
					} else if !ok {
						continue
					} else if err := tr.claimAndSpawn(ctx, task, projectID); err != nil {
						if !errors.Is(err, ErrTaskClaimConflict) {
							tr.logger.Printf("claim and spawn automation task failed for %s/%s: %v", projectID, task.ID, err)
							slog.Error("spawn failed",
								"task_id", task.ID,
								"project_id", projectID,
								"runner_id", tr.runnerID,
								"path", "automation_poll",
								"generated_by", task.GeneratedBy,
								"error", err.Error(),
							)
						}
					} else {
						filled++
						continue
					}
				}
			}

			continue // paused: nothing but automations may run
		}

		// Get next task for this project (filtered by feature IDs and executors)
		task, err := tr.client.GetNextTask(ctx, projectID, tr.buildFetchOptions())
		if err != nil || task == nil {
			continue
		}
		if isAutomationGeneratedTask(task) {
			if automationsPausedForProject {
				continue
			}
			if ok, err := tr.canStartAutomationTask(ctx, task); err != nil {
				tr.logger.Printf("automation max_concurrent check failed for %s/%s: %v", projectID, task.ID, err)
				continue
			} else if !ok {
				continue
			}
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
			if errors.Is(err, ErrTaskClaimConflict) {
				continue
			}
			tr.logger.Printf("claim and spawn failed for %s/%s: %v", projectID, task.ID, err)
			slog.Error("spawn failed",
				"task_id", task.ID,
				"project_id", projectID,
				"runner_id", tr.runnerID,
				"path", "poll",
				"error", err.Error(),
			)
			continue
		}

		filled++
	}

	// 6. Save state
	tr.saveState()

	// 7. Emit poll complete event
	tr.emitPollComplete()
}

// buildFetchOptions creates TaskFetchOptions from the runner's current configuration.
func (tr *TaskRunner) buildFetchOptions() *TaskFetchOptions {
	opts := &TaskFetchOptions{
		Executors: tr.executorNames(),
		RunnerID:  tr.runnerID,
	}
	if len(tr.config.FeatureIDs) > 0 {
		opts.FeatureIDs = tr.config.FeatureIDs
	}
	return opts
}

// =============================================================================
// Executor Dispatch
// =============================================================================

// DefaultExecutorName is the executor name used when task.Executor is empty.
const DefaultExecutorName = "opencode"

// executorNames returns a sorted list of registered executor names.
func (tr *TaskRunner) executorNames() []string {
	if tr.executorRegistry != nil {
		return tr.executorRegistry.Names()
	}
	if tr.executor != nil {
		return []string{DefaultExecutorName}
	}
	return nil
}

// =============================================================================
// Executor Resolution
// =============================================================================

// resolveExecutor returns the executor and resolved executor name for a task.
// If an ExecutorRegistry is set, it resolves per-task using the precedence chain.
// Otherwise, it falls back to the single injected executor.
func (tr *TaskRunner) resolveExecutor(task *types.ResolvedTask) (TaskExecutor, string, error) {
	if tr.executorRegistry != nil {
		exec, name, err := tr.executorRegistry.ResolveForTask(task)
		if err != nil {
			return nil, "", fmt.Errorf("resolve executor: %w", err)
		}
		tr.logger.Printf("resolved executor %q for task %s", name, task.ID)
		return exec, name, nil
	}
	if tr.executor == nil {
		return nil, "", fmt.Errorf("no executor configured")
	}
	name := task.Executor
	if name == "" {
		name = DefaultExecutorName
	}
	return tr.executor, name, nil
}

func (tr *TaskRunner) cleanupTaskArtifacts(task RunningTask) {
	if task.ExecutorType != "" && tr.executorRegistry != nil {
		if exec, ok := tr.executorRegistry.Get(task.ExecutorType); ok && exec != nil {
			_ = exec.Cleanup(task.ID, task.ProjectID)
			return
		}
	}
	if tr.executor != nil {
		_ = tr.executor.Cleanup(task.ID, task.ProjectID)
		return
	}
	if tr.executorRegistry != nil {
		if exec, ok := tr.executorRegistry.Get(DefaultExecutorName); ok && exec != nil {
			_ = exec.Cleanup(task.ID, task.ProjectID)
			return
		}
	}
	_ = CommonCleanup(tr.config.StateDir, task.ID, task.ProjectID)
}

// =============================================================================
// Claim and Spawn
// =============================================================================

// claimAndSpawn claims a task and spawns a process for it.
func (tr *TaskRunner) claimAndSpawn(ctx context.Context, task *types.ResolvedTask, projectID string) error {
	return tr.claimAndSpawnWithWorkdir(ctx, task, projectID, "")
}

func (tr *TaskRunner) claimAndSpawnWithWorkdir(ctx context.Context, task *types.ResolvedTask, projectID, resolvedWorkdir string) error {
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
			FeatureID: task.FeatureID,
		})
		if result.Message != "" {
			return fmt.Errorf("%w: %s", ErrTaskClaimConflict, result.Message)
		}
		if result.ClaimedBy != "" {
			return fmt.Errorf("%w: task already claimed by %s", ErrTaskClaimConflict, result.ClaimedBy)
		}
		return ErrTaskClaimConflict
	}

	tr.emitEvent(RunnerEvent{
		Type:      EventTaskClaimed,
		TaskID:    task.ID,
		ProjectID: projectID,
		TaskPath:  task.Path,
		FeatureID: task.FeatureID,
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
		if emitter, ok := tr.client.(interface {
			EmitEvent(context.Context, string, map[string]any, string) error
		}); ok {
			go func() {
				emitCtx, emitCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer emitCancel()
				_ = emitter.EmitEvent(emitCtx, "runner.first_task_today", map[string]any{
					"runner_id":  tr.runnerID,
					"project_id": projectID,
					"task_id":    task.ID,
					"date":       today,
				}, "first-task-"+today+"-"+tr.runnerID)
			}()
		}
	}

	// Update task status to in_progress
	if err := tr.client.UpdateTaskStatus(ctx, task.Path, "in_progress"); err != nil {
		// Release the claim on failure
		// Rollback of a claim we already hold. If the release also fails the
		// lease simply expires on its own, and the caller's error below is
		// the one that matters.
		_ = tr.client.ReleaseTask(ctx, projectID, task.ID, tr.runnerID)
		return fmt.Errorf("update task status: %w", err)
	}

	statusEvt := RunnerEvent{
		Type:       EventTaskStatusChanged,
		TaskID:     task.ID,
		ProjectID:  projectID,
		TaskPath:   task.Path,
		FeatureID:  task.FeatureID,
		FromStatus: "pending",
		ToStatus:   "in_progress",
	}
	statusEvt.RunnerID = tr.runnerID
	tr.emitEvent(statusEvt)

	// Pre-task-start hook: can abort/block task execution.
	// If the hook returns an error, release the claim and reset status to pending.
	if tr.hookDispatcher != nil {
		evt := statusEvt.ToEvent()
		// Use task.started as the event type for pre-task-start hook matching
		evt.Type = types.EventTaskStarted
		evt.TaskTitle = task.Title
		if err := tr.hookDispatcher.DispatchPre(evt); err != nil {
			tr.logger.Printf("pre-task-start hook failed for %s/%s: %v", projectID, task.ID, err)
			// Release claim and reset status
			// Rollback of a claim we already hold. If the release also fails the
			// lease simply expires on its own, and the caller's error below is
			// the one that matters.
			_ = tr.client.ReleaseTask(ctx, projectID, task.ID, tr.runnerID)
			_ = tr.client.UpdateTaskStatus(ctx, task.Path, "pending")
			tr.emitEvent(RunnerEvent{
				Type:       EventTaskStatusChanged,
				TaskID:     task.ID,
				ProjectID:  projectID,
				TaskPath:   task.Path,
				FeatureID:  task.FeatureID,
				FromStatus: "in_progress",
				ToStatus:   "pending",
			})
			tr.emitEvent(RunnerEvent{
				Type:      EventTaskReleased,
				TaskID:    task.ID,
				ProjectID: projectID,
				FeatureID: task.FeatureID,
				Reason:    fmt.Sprintf("pre-task-start hook: %v", err),
			})
			return fmt.Errorf("pre-task-start hook: %w", err)
		}
	}

	// Resolve executor for this task
	taskExecutor, executorType, err := tr.resolveExecutor(task)
	if err != nil {
		// Rollback of a claim we already hold. If the release also fails the
		// lease simply expires on its own, and the caller's error below is
		// the one that matters.
		_ = tr.client.ReleaseTask(ctx, projectID, task.ID, tr.runnerID)
		return fmt.Errorf("resolve executor: %w", err)
	}

	// Resolve workdir (may create git worktree) unless dispatch validation already resolved it.
	workdir := resolvedWorkdir
	if workdir == "" {
		workdir, err = taskExecutor.ResolveWorkdir(task)
	}
	if err != nil {
		// Worktree creation failed - mark task as blocked
		tr.emitEvent(RunnerEvent{
			Type:       EventTaskStatusChanged,
			TaskID:     task.ID,
			ProjectID:  projectID,
			TaskPath:   task.Path,
			FeatureID:  task.FeatureID,
			FromStatus: "in_progress",
			ToStatus:   "blocked",
		})
		tr.emitEvent(RunnerEvent{
			Type:      EventTaskReleased,
			TaskID:    task.ID,
			ProjectID: projectID,
			FeatureID: task.FeatureID,
			Reason:    "workdir resolution failed",
		})
		// Rollback of a claim we already hold. If the release also fails the
		// lease simply expires on its own, and the caller's error below is
		// the one that matters.
		_ = tr.client.ReleaseTask(ctx, projectID, task.ID, tr.runnerID)
		_ = tr.client.UpdateTaskStatus(ctx, task.Path, "blocked")
		return fmt.Errorf("resolve workdir: %w", err)
	}

	spawnOpts := SpawnOptions{
		Mode:                tr.mode,
		Workdir:             workdir,
		RuntimeDefaultModel: tr.getDefaultModel(),
	}

	// Resume-abandoned-tasks flow: if the task carries resume_requested from
	// POST /resume, propagate it into IsResume so the executor's prompt
	// template tells the agent to look for prior progress. NOTE: the flag is
	// cleared AFTER a successful Spawn (see below). Clearing it here would
	// silently drop the user's resume intent when the spawn subsequently
	// fails, leaving the task in blocked with resume_requested=false.
	if task.ResumeRequested {
		spawnOpts.IsResume = true
	}

	// Start log streamer if enabled
	var logStreamer *LogStreamer
	if tr.config.LogStreaming {
		logStreamer = NewLogStreamer(LogStreamerConfig{
			Client:    tr.client,
			RunnerID:  tr.runnerID,
			ProjectID: projectID,
			TaskID:    task.ID,
		})
		spawnOpts.LogWriter = logStreamer
	}

	spawnResult, err := taskExecutor.Spawn(ctx, task, projectID, spawnOpts)
	if err != nil {
		if logStreamer != nil {
			logStreamer.Stop()
		}
		// Release the claim on failure. Include the underlying error text
		// in the release reason so `brain_events_recent` and downstream
		// observers can diagnose *why* the spawn failed (e.g. script
		// command rejected by allowlist) rather than just seeing the
		// opaque "spawn failed" label.
		tr.emitEvent(RunnerEvent{
			Type:      EventTaskReleased,
			TaskID:    task.ID,
			ProjectID: projectID,
			FeatureID: task.FeatureID,
			Reason:    fmt.Sprintf("spawn failed: %v", err),
		})
		// Rollback of a claim we already hold. If the release also fails the
		// lease simply expires on its own, and the caller's error below is
		// the one that matters.
		_ = tr.client.ReleaseTask(ctx, projectID, task.ID, tr.runnerID)
		// Roll the task's status back from "in_progress" (set optimistically
		// at claim time) to "blocked" so it doesn't sit forever waiting on
		// the orphan reaper. Symmetric with the workdir-failure branch
		// above. A human or the Blocked Task Inspector can then address it.
		_ = tr.client.UpdateTaskStatus(ctx, task.Path, "blocked")
		// If we consumed a resume intent, the spawn failure means we never
		// actually resumed. Best-effort re-stamp the flag so a subsequent
		// Trigger or Resume from the user picks up the resume prompt path
		// again. Failure to re-stamp is logged but not fatal.
		if task.ResumeRequested {
			if metaErr := tr.client.UpdateMetadata(ctx, task.Path, map[string]interface{}{
				"resume_requested": true,
			}); metaErr != nil {
				slog.Warn("resume: failed to re-stamp resume_requested after spawn failure",
					"project", projectID, "task_id", task.ID, "error", metaErr)
			}
		}
		return fmt.Errorf("spawn task: %w", err)
	}

	// Spawn succeeded — NOW clear the resume_requested flag so a re-poll after
	// the task completes doesn't route the next dispatch through IsResume=true
	// again. If the clear fails, log and continue: the executor is already
	// running with IsResume=true, and the next dispatch (if any) would just
	// route through IsResume again — a duplicate prompt is preferable to
	// blocking spawn completion.
	if task.ResumeRequested {
		if err := tr.client.UpdateMetadata(ctx, task.Path, map[string]interface{}{
			"resume_requested": false,
		}); err != nil {
			slog.Warn("resume: failed to clear resume_requested flag post-spawn (continuing)",
				"project", projectID, "task_id", task.ID, "error", err)
		} else {
			slog.Info("resume: consumed resume_requested flag, spawned with IsResume=true",
				"project", projectID, "task_id", task.ID)
		}
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
		ExecutorType:   executorType,
		Executor:       executorType,
		Agent:          task.Agent,
		Model:          task.Model,
		CompleteOnIdle: resolveCompleteOnIdle(task.CompleteOnIdle, task.DirectPrompt),
		RunID:          latestInProgressRunID(task.Runs),
		FeatureID:      task.FeatureID,
		GeneratedBy:    task.GeneratedBy,
		AttemptCount:   task.AttemptCount,
		MaxAttempts:    resolveMaxAttempts(task, tr.config),
	}
	// Every executor gets an InstanceID so the Runners tab can surface a
	// "currently running" row for it (issue: script/pi tasks were invisible
	// in the PWA while executing). OpencodePort is only known up front for
	// the OpenCode headless-serve+attach path; other executors report 0.
	runningTask.InstanceID = generateInstanceID()
	if executorType == "opencode" {
		// Headless serve+attach reports its attachable port up front; TUI/
		// dashboard discover it from the tmux child via discoverAndSaveSession.
		runningTask.OpencodePort = spawnResult.OpencodePort
	}

	// Track in process manager
	if spawnResult.Proc != nil {
		if err := tr.processMgr.Add(task.ID, runningTask, spawnResult.Proc); err != nil {
			return fmt.Errorf("track process: %w", err)
		}
	}

	// Track log streamer for cleanup on completion
	if logStreamer != nil {
		tr.trackLogStreamer(task.ID, logStreamer)
	}

	// Update status
	tr.mu.Lock()
	tr.status = RunnerStatusProcessing
	tr.mu.Unlock()

	// Emit event
	startedEvt := RunnerEvent{
		Type: EventTaskStarted,
		Task: &runningTask,
	}
	tr.emitEvent(startedEvt)

	// Post-task-start hook: fire-and-forget after process is tracked.
	if tr.hookDispatcher != nil {
		startedEvt.RunnerID = tr.runnerID
		tr.hookDispatcher.DispatchPost(startedEvt.ToEvent())
	}

	// Discover opencode session ID and port in background (only for opencode executor)
	if executorType == "opencode" {
		// Report the instance immediately (status "starting", port unknown);
		// discovery updates it with the real port and session.
		tr.reportInstance(tr.taskInstance(&ProcessInfo{
			Task: runningTask,
			Proc: NewPidProcess(runningTask.PID),
		}, runnerHostname()))
		go tr.discoverAndSaveSession(task.Path, spawnResult.PID, spawnResult.OpencodePort, spawnResult.ExistingSessionIDs, spawnResult.SessionID)
	}

	return nil
}

// discoverAndSaveSession discovers the opencode port and session ID for a
// spawned process and persists it on the task's entry for later "o"/"O" access.
// The pid is typically a tmux shell PID; the actual opencode runs as a child.
//
// knownPort, when > 0, is the already-resolved server port (headless
// serve+attach reports it at spawn time, and the run --attach process binds
// no port of its own); discovery via the PID is skipped in that case.
//
// pinnedSessionID, when non-empty, is a session this runner created itself and
// handed to `opencode run --session`. There is then nothing to discover: the
// heuristic below is skipped entirely and the known ID is recorded as-is.
func (tr *TaskRunner) discoverAndSaveSession(taskPath string, pid int, knownPort int, excludeSessionIDs map[string]struct{}, pinnedSessionID string) {
	port := knownPort
	if port <= 0 {
		if pid <= 0 {
			return
		}

		// Wait for opencode to start its HTTP server
		time.Sleep(5 * time.Second)

		// Find opencode's port by checking child processes
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
	}

	// Store the discovered port on the running task for idle detection
	for _, info := range tr.processMgr.GetAll() {
		if info.Task.Path == taskPath {
			tr.processMgr.UpdatePort(info.Task.ID, port)
			break
		}
	}
	tr.reportTaskInstanceByPath(taskPath)

	// Query opencode's session endpoint. With serve+attach the session is
	// created by the run process a beat after the server is up, so retry
	// briefly until one appears.
	//
	// Skipped when the session was pinned at spawn: the ID is already known
	// and exact, so there is nothing to guess.
	sessionID := pinnedSessionID
	if sessionID == "" {
		var err error
		for attempt := 0; attempt < 5; attempt++ {
			sessionID, err = tr.claimDiscoveredSession(taskPath, port, excludeSessionIDs)
			if err == nil && sessionID != "" {
				break
			}
			time.Sleep(2 * time.Second)
		}
		if err != nil {
			tr.logger.Printf("session discovery: failed to get session from port %d: %v", port, err)
			return
		}
	}
	if sessionID == "" {
		return
	}
	tr.recordSessionID(taskPath, sessionID)

	// Capture the workdir so the session can be re-served/resumed later from
	// the machine that ran it.
	var workdir string
	for _, info := range tr.processMgr.GetAll() {
		if info.Task.Path == taskPath {
			workdir = info.Task.Workdir
			break
		}
	}

	// Persist session ID to the task's metadata in SQLite via API. Alongside the
	// timestamp we record where the session lives — runner, machine, host and
	// workdir — so remote control can locate and re-open it after the task's
	// live instance is gone (instances are deleted on completion).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := tr.client.UpdateMetadata(ctx, taskPath, map[string]interface{}{
		"sessions": map[string]interface{}{
			sessionID: map[string]interface{}{
				"timestamp":  time.Now().UTC().Format(time.RFC3339),
				"runner_id":  tr.runnerID,
				"machine_id": tr.machineID,
				"hostname":   runnerHostname(),
				"workdir":    workdir,
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
	origin := "discovered"
	if pinnedSessionID != "" {
		origin = "pinned at spawn"
	}
	tr.logger.Printf("session discovery: saved session %s for %s (port %d, %s)", sessionID, taskPath, port, origin)
}

// claimDiscoveredSession picks a session for taskPath and claims it in one
// atomic step, so two tasks discovering at the same moment cannot converge on
// the same ID.
//
// The per-spawn exclude set is a snapshot taken before this task's server
// started; it cannot possibly contain a session another concurrently-spawned
// task is about to claim. Two tasks sharing a workdir therefore both see both
// new sessions, both apply "newest wins", and both record the same ID — after
// which the PWA streams one task's transcript into the other task's pane, with
// no error anywhere. Folding in the sessions already claimed by this runner's
// live instances closes that gap, and holding the lock across the pick and the
// record keeps it closed against the two discovery goroutines racing.
//
// This is only the fallback path: when the session was pinned at spawn (the
// normal attach case) discovery never runs at all.
func (tr *TaskRunner) claimDiscoveredSession(taskPath string, port int, exclude map[string]struct{}) (string, error) {
	tr.sessionClaimMu.Lock()
	defer tr.sessionClaimMu.Unlock()

	skip := make(map[string]struct{}, len(exclude))
	for id := range exclude {
		skip[id] = struct{}{}
	}
	for id := range tr.claimedSessionIDs(taskPath) {
		skip[id] = struct{}{}
	}

	sessionID, err := discoverSessionID(port, skip)
	if err != nil || sessionID == "" {
		return "", err
	}
	// Record the claim before releasing the lock, so a concurrent discovery
	// sees it in claimedSessionIDs and picks a different session.
	tr.recordSessionID(taskPath, sessionID)
	return sessionID, nil
}

// claimedSessionIDs returns the session IDs already spoken for by this
// runner's live instances — tracked tasks plus ad-hoc bridge instances —
// excluding the task at exceptPath (a re-discovery must be able to re-claim
// its own session).
func (tr *TaskRunner) claimedSessionIDs(exceptPath string) map[string]struct{} {
	claimed := make(map[string]struct{})
	for _, info := range tr.processMgr.GetAll() {
		if info.Task.Path == exceptPath {
			continue
		}
		if info.Task.SessionID != "" {
			claimed[info.Task.SessionID] = struct{}{}
		}
	}
	if tr.bridgeClient != nil {
		for _, inst := range tr.bridgeClient.AdhocInstances(runnerHostname()) {
			for _, id := range inst.SessionIDs {
				if id != "" {
					claimed[id] = struct{}{}
				}
			}
		}
	}
	return claimed
}

// recordSessionID stores the session on the tracked task and re-reports the
// instance so the registry reflects it.
func (tr *TaskRunner) recordSessionID(taskPath, sessionID string) {
	for _, info := range tr.processMgr.GetAll() {
		if info.Task.Path == taskPath {
			tr.processMgr.UpdateSessionID(info.Task.ID, sessionID)
			break
		}
	}
	tr.reportTaskInstanceByPath(taskPath)
}

// reportTaskInstanceByPath re-reports the instance record for the tracked
// task with the given path, reflecting newly discovered port/session state.
func (tr *TaskRunner) reportTaskInstanceByPath(taskPath string) {
	for _, info := range tr.processMgr.GetAll() {
		if info.Task.Path == taskPath {
			if info.Task.InstanceID != "" {
				tr.reportInstance(tr.taskInstance(&info, runnerHostname()))
			}
			return
		}
	}
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

type opencodeSession struct {
	ID   string `json:"id"`
	Time struct {
		Created int64 `json:"created"`
		Updated int64 `json:"updated"`
	} `json:"time"`
}

// order returns the value sessions are ranked by when picking one for a task.
// Creation time is the right key: `updated` moves every time the agent writes
// a message, so it says nothing about which session belongs to whom. Older
// OpenCode builds that omit `created` fall back to `updated`.
func (s opencodeSession) order() int64 {
	if s.Time.Created > 0 {
		return s.Time.Created
	}
	return s.Time.Updated
}

func fetchSessions(port int) ([]opencodeSession, error) {
	url := fmt.Sprintf("http://localhost:%d/session", port)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read session response: %w", err)
	}

	var sessions []opencodeSession
	if err := json.Unmarshal(body, &sessions); err == nil {
		return sessions, nil
	}
	var single opencodeSession
	if err := json.Unmarshal(body, &single); err != nil {
		return nil, fmt.Errorf("decode session response: %w", err)
	}
	return []opencodeSession{single}, nil
}

// listSessionIDs snapshots the session IDs an opencode server can currently
// see, for use as a pre-spawn baseline.
func listSessionIDs(port int) (map[string]struct{}, error) {
	sessions, err := fetchSessions(port)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(sessions))
	for _, s := range sessions {
		if s.ID != "" {
			ids[s.ID] = struct{}{}
		}
	}
	return ids, nil
}

// createOpencodeSession creates a session on an OpenCode server and returns
// its ID, so the caller can pin the task's run to a session it already knows.
//
// This exists because session discovery cannot be made reliable by itself:
// `GET /session` is a store-wide listing, not a per-server one. Every
// `opencode serve` started against the same workdir lists the same sessions
// (observed listings reach beyond that workdir too), so "the newest session
// that was not there when I started" cannot tell two concurrently-spawned
// tasks apart — each one's baseline was taken before the other's session
// existed, and both land on whichever session was touched last.
func createOpencodeSession(port int, title string) (string, error) {
	body := map[string]string{}
	if title != "" {
		body["title"] = title
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("encode session request: %w", err)
	}

	url := fmt.Sprintf("http://localhost:%d/session", port)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build session request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("POST %s: status %d", url, resp.StatusCode)
	}

	var created opencodeSession
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", fmt.Errorf("decode session response: %w", err)
	}
	if created.ID == "" {
		return "", fmt.Errorf("POST %s: response had no session id", url)
	}
	return created.ID, nil
}

// discoverSessionID queries an opencode HTTP server for the task session ID.
// When exclude is set, pre-existing sessions from before task spawn are ignored.
//
// Among what remains it takes the OLDEST session, not the newest. Sessions are
// created in spawn order, so when several are in flight the one belonging to
// the task discovering now is the earliest-created that nobody has claimed;
// ranking by recency instead handed every concurrent discovery whichever
// session had been written to last — which is how tasks sharing a workdir all
// ended up on one session ID.
//
// This remains a heuristic. Prefer pinning the session at spawn
// (createOpencodeSession); this is the fallback for when that is unavailable.
func discoverSessionID(port int, exclude map[string]struct{}) (string, error) {
	sessions, err := fetchSessions(port)
	if err != nil {
		return "", err
	}
	var oldest *opencodeSession
	for i := range sessions {
		if sessions[i].ID == "" {
			continue
		}
		if _, ok := exclude[sessions[i].ID]; ok {
			continue
		}
		if oldest == nil || sessions[i].order() < oldest.order() {
			oldest = &sessions[i]
		}
	}
	if oldest == nil {
		return "", nil
	}
	return oldest.ID, nil
}

// =============================================================================
// Runner Registration & Heartbeat
// =============================================================================

// registerWithAPI registers this runner with the Brain API server.
// Registration failure is non-fatal — the runner logs a warning and continues.
// This ensures backward compatibility with older API servers that don't support
// the runner registry endpoints.
func (tr *TaskRunner) registerWithAPI(ctx context.Context) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	dispatchPush := tr.dispatchPushEnabled()
	req := types.RunnerRegistration{
		RunnerID:       tr.runnerID,
		MachineID:      tr.machineID,
		Hostname:       hostname,
		Labels:         tr.config.Labels,
		Executors:      tr.executorNames(),
		Capabilities:   tr.config.Capabilities,
		Projects:       tr.getProjects(),
		MaxParallel:    tr.getMaxParallel(),
		DispatchPush:   dispatchPush,
		WorkspaceRoots: tr.schedulerWorkspaceRoots(),
		Resources:      tr.config.Resources,
		Capacity:       tr.config.Capacity,
		Draining:       tr.config.Draining,
	}

	info, err := tr.client.RegisterRunner(ctx, req)
	if err != nil {
		slog.Warn("runner registration failed (continuing without registration)", "error", err)
		return
	}

	slog.Info("runner registered with API",
		"runner_id", tr.runnerID,
		"hostname", hostname,
		"status", info.Status,
	)
}

// reapOrphanedTasks scans this runner's projects for tasks stuck in
// `in_progress` from a previous runner crash and marks them `blocked` with
// an explanatory note.
//
// Why: when the runner is killed mid-execution (crash, host sleep, SIGKILL),
// child processes die but task frontmatter retains `status: in_progress`.
// Without recovery these zombies live forever, and crucially they continue
// counting toward `countRunnableGeneratedTasks` in automation_service.go,
// silently consuming automation `max_concurrent` slots and (more often)
// just cluttering the TUI with stale work.
//
// Safety model: ownership is gated through the existing claim system.
// We attempt to claim each orphan; if the claim succeeds, no live runner
// owns it (lease expired or never existed) and we may safely mark it
// blocked. If the claim conflicts, another live runner owns the task and
// we leave it alone — the lease cleanup goroutine in
// service/task.go:StartClaimCleanup will handle it when that runner's
// lease expires.
//
// Failures are logged but never fatal; the runner proceeds normally.
func (tr *TaskRunner) reapOrphanedTasks(ctx context.Context) {
	projects := tr.getProjects()
	if tr.client == nil || len(projects) == 0 {
		return
	}

	// Bound this so a slow API can't delay startup forever.
	reapCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	totalReaped := 0
	for _, projectID := range projects {
		if reapCtx.Err() != nil {
			slog.Warn("orphan reaper aborted", "error", reapCtx.Err(), "reaped", totalReaped)
			return
		}
		reaped := tr.reapOrphanedTasksForProject(reapCtx, projectID)
		totalReaped += reaped
	}

	if totalReaped > 0 {
		slog.Info("orphan reaper: marked stale in_progress tasks blocked",
			"runner_id", tr.runnerID,
			"count", totalReaped,
			"projects", len(projects),
		)
	}
}

// reapOrphanedTasksForProject reaps orphans for a single project. Returns
// the number of tasks marked blocked. All errors are logged at debug level
// and skipped — orphan reaping is best-effort.
func (tr *TaskRunner) reapOrphanedTasksForProject(ctx context.Context, projectID string) int {
	resp, err := tr.client.ListEntries(ctx, map[string]string{
		"project": projectID,
		"type":    "task",
		"status":  "in_progress",
		"limit":   "500",
	})
	if err != nil {
		slog.Debug("orphan reaper: list failed", "project", projectID, "error", err)
		return 0
	}
	if resp == nil || len(resp.Entries) == 0 {
		return 0
	}

	reaped := 0
	for i := range resp.Entries {
		entry := &resp.Entries[i]
		if ctx.Err() != nil {
			return reaped
		}
		if entry.Status != "in_progress" || entry.Type != "task" {
			continue // Defensive: respect the filter even if the API ignores it.
		}
		if tr.tryReapOrphan(ctx, projectID, entry) {
			reaped++
		}
	}
	return reaped
}

// tryReapOrphan attempts to reap a single orphan task. Returns true if the
// task was successfully marked blocked.
func (tr *TaskRunner) tryReapOrphan(ctx context.Context, projectID string, entry *types.BrainEntry) bool {
	taskID := entry.ID
	taskPath := entry.Path
	if taskID == "" || taskPath == "" {
		return false
	}

	// Probe ownership via the claim system. If another live runner holds an
	// unexpired claim, ClaimTask returns conflict and we leave the task alone.
	result, err := tr.client.ClaimTask(ctx, projectID, taskID, tr.runnerID)
	if err != nil {
		slog.Debug("orphan reaper: claim probe failed",
			"project", projectID, "task_id", taskID, "error", err)
		return false
	}
	if !result.Success {
		// Live owner — lease cleanup goroutine will handle this if the owner
		// is actually dead. Not our problem.
		slog.Debug("orphan reaper: task still claimed by live runner, skipping",
			"project", projectID, "task_id", taskID, "claimed_by", result.ClaimedBy)
		return false
	}

	// We now hold the claim. Double-check the task is still in_progress —
	// the agent may have completed between list and claim.
	current, err := tr.client.GetEntry(ctx, taskPath)
	if err == nil && current != nil && current.Status != "in_progress" {
		// Race: someone else terminalized it. Release and move on.
		_ = tr.client.ReleaseTask(ctx, projectID, taskID, tr.runnerID)
		return false
	}

	// Resume-vs-reaper race: if a user clicked Resume between our list and
	// our claim, current.Status would still be pending (caught above) — but
	// if they clicked Resume while we already held the claim, they may have
	// set resume_requested=true on a still-in_progress task. Honor the user's
	// intent: release the claim and skip. The runner's normal claim path will
	// pick this task up on its next poll and route it through IsResume=true.
	if current != nil && current.ResumeRequested {
		slog.Info("orphan reaper: task has resume_requested=true, skipping reap",
			"project", projectID, "task_id", taskID)
		_ = tr.client.ReleaseTask(ctx, projectID, taskID, tr.runnerID)
		return false
	}

	// OrphanReaperNoteText is the exact body-append text used to mark a task
	// as reaped. Mirrored by service.OrphanReaperMarker (as a prefix substring)
	// so the API-side enrichAbandonmentState can classify the task without
	// grepping this whole sentence. If you change either constant, change both.
	const orphanReaperNoteText = "*Marked blocked by runner orphan reaper: task was left in `in_progress` after a previous runner exited without finalizing it. The original child process is no longer running.*"
	note := "\n\n---\n" + orphanReaperNoteText + "\n"
	if appendErr := tr.client.AppendToTask(ctx, taskPath, note); appendErr != nil {
		slog.Debug("orphan reaper: append note failed",
			"project", projectID, "task_id", taskID, "error", appendErr)
		// Continue — the status update is the important part.
	}

	// Stamp durable abandonment metadata so the API's enrichAbandonmentState
	// can classify this task as abandoned via a JSON field lookup instead of
	// scanning body text. This is best-effort — a failure here does not block
	// the status transition below. The grep-fallback on the marker note keeps
	// enrichment correct even if this metadata write fails.
	if metaErr := tr.client.UpdateMetadata(ctx, taskPath, map[string]interface{}{
		"abandoned_at":     time.Now().UTC().Format(time.RFC3339),
		"abandoned_reason": "runner_orphan",
	}); metaErr != nil {
		slog.Debug("orphan reaper: metadata stamp failed",
			"project", projectID, "task_id", taskID, "error", metaErr)
	}

	// TOCTOU close: between our earlier resume-guard re-read and the status
	// flip below, a POST /resume request could have committed status=pending
	// + resume_requested=true. Re-read one more time. If the task is no
	// longer in_progress OR resume_requested is now true, the user's intent
	// takes precedence and we bail without touching status. The append+
	// metadata writes above are additive/informational, so leaving them
	// applied does no harm — they document that the reaper considered this
	// task abandoned even though the user beat us to it.
	preFlip, err := tr.client.GetEntry(ctx, taskPath)
	if err == nil && preFlip != nil {
		if preFlip.Status != "in_progress" || preFlip.ResumeRequested {
			slog.Info("orphan reaper: TOCTOU re-read detected concurrent state change, skipping status flip",
				"project", projectID, "task_id", taskID,
				"current_status", preFlip.Status, "resume_requested", preFlip.ResumeRequested)
			_ = tr.client.ReleaseTask(ctx, projectID, taskID, tr.runnerID)
			return false
		}
	}

	if statusErr := tr.client.UpdateTaskStatus(ctx, taskPath, "blocked"); statusErr != nil {
		slog.Warn("orphan reaper: status update failed",
			"project", projectID, "task_id", taskID, "error", statusErr)
		// Best-effort release of the claim we just took so we don't leave a
		// dangling lease behind.
		_ = tr.client.ReleaseTask(ctx, projectID, taskID, tr.runnerID)
		return false
	}

	if releaseErr := tr.client.ReleaseTask(ctx, projectID, taskID, tr.runnerID); releaseErr != nil {
		slog.Debug("orphan reaper: release failed (lease will expire)",
			"project", projectID, "task_id", taskID, "error", releaseErr)
		// Lease will expire on its own — not fatal.
	}

	slog.Info("orphan reaper: marked stale in_progress task blocked",
		"project", projectID, "task_id", taskID, "title", entry.Title)
	return true
}

func (tr *TaskRunner) schedulerWorkspaceRoots() []string {
	if len(tr.config.WorkspaceRoots) > 0 {
		return tr.config.WorkspaceRoots
	}
	return tr.config.Control.AllowedWorkdirRoots
}

// sendHeartbeat sends a heartbeat to the Brain API with current runner stats.
// Heartbeat failure is logged but does not stop the runner.
func (tr *TaskRunner) sendHeartbeat(ctx context.Context) {
	running := tr.processMgr.RunningCount()

	tr.mu.RLock()
	stats := tr.stats
	tr.mu.RUnlock()

	dispatchPush := tr.dispatchPushEnabled()
	draining := tr.config.Draining
	req := types.RunnerHeartbeatRequest{
		RunningTasks:   running,
		DispatchPush:   &dispatchPush,
		Labels:         tr.config.Labels,
		WorkspaceRoots: tr.schedulerWorkspaceRoots(),
		Projects:       tr.getProjects(),
		Resources:      tr.config.Resources,
		Capacity:       tr.config.Capacity,
		Draining:       &draining,
		Stats: map[string]interface{}{
			"completed":    stats.Completed,
			"failed":       stats.Failed,
			"totalRuntime": stats.TotalRuntime,
		},
		Instances: tr.instanceSnapshot(),
	}

	// Include dispatch pool metrics so operators can see backpressure
	// signals in brain_runner_status and the PWA runner card. Silent
	// dispatch drops used to only appear in slog output; now they
	// surface in the heartbeat's Stats map.
	if pool := tr.getDispatchPool(); pool != nil {
		s := pool.Stats()
		req.Stats["dispatchQueueLen"] = s.QueueLen
		req.Stats["dispatchQueueCap"] = s.QueueCapacity
		req.Stats["dispatchWorkers"] = s.Workers
		req.Stats["dispatchBacklogFull"] = s.BacklogFullCount
	}

	if err := tr.client.SendHeartbeat(ctx, tr.runnerID, req); err != nil {
		slog.Warn("heartbeat failed", "runner_id", tr.runnerID, "error", err)
	}
}

// deregisterFromAPI deregisters this runner from the Brain API server.
// This is called during graceful shutdown. Failure is logged but not fatal.
func (tr *TaskRunner) deregisterFromAPI() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := tr.client.DeregisterRunner(ctx, tr.runnerID); err != nil {
		slog.Warn("runner deregistration failed", "runner_id", tr.runnerID, "error", err)
		return
	}
	slog.Info("runner deregistered from API", "runner_id", tr.runnerID)
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
	// Stop log streamer (flushes remaining lines)
	tr.stopLogStreamer(taskID)

	// Create result before removing from process manager
	result := tr.processMgr.CreateTaskResult(taskID, status)

	// Pre-task-end hook: fires before cleanup, can inspect result but not modify status.
	// Hook failures are logged but do not affect the task lifecycle.
	if tr.hookDispatcher != nil {
		// Determine the event type for the pre-hook based on completion status
		var preEndEventType string
		switch status {
		case CompletionCompleted:
			preEndEventType = types.EventTaskCompleted
		case CompletionCancelled:
			preEndEventType = types.EventTaskCancelled
		default:
			preEndEventType = types.EventTaskFailed
		}
		preEvt := RunnerEvent{
			Type:      EventTaskStatusChanged,
			TaskID:    taskID,
			ProjectID: task.ProjectID,
			TaskPath:  task.Path,
			FeatureID: task.FeatureID,
		}
		preEvt.RunnerID = tr.runnerID
		evt := preEvt.ToEvent()
		evt.Type = preEndEventType
		evt.TaskTitle = task.Title
		if err := tr.hookDispatcher.DispatchPre(evt); err != nil {
			tr.logger.Printf("pre-task-end hook failed for %s (non-blocking): %v", taskID, err)
		}
	}

	// Remove from process manager and the instance registry
	tr.processMgr.Remove(taskID)
	tr.removeInstance(task.InstanceID)

	// Map completion status to API status
	var apiStatus string
	var eventType RunnerEventType
	switch status {
	case CompletionCompleted:
		apiStatus = "completed"
		eventType = EventTaskCompleted
		// Clear any attempt counter left by earlier failed runs so a future
		// failure starts from zero.
		tr.clearTaskFailures(ctx, task)
	case CompletionBlocked:
		apiStatus = "blocked"
		eventType = EventTaskFailed
	case CompletionCancelled:
		apiStatus = "completed" // cancelled tasks are considered done
		eventType = EventTaskCancelled
	default:
		// failed/crashed/timeout → retry, but a bounded number of times. This
		// used to reset to "pending" unconditionally, so a task that failed
		// deterministically re-dispatched every poll interval forever.
		apiStatus, _ = tr.recordTaskFailure(ctx, task)
		eventType = EventTaskFailed
	}

	// Emit status change before the API update
	tr.emitEvent(RunnerEvent{
		Type:       EventTaskStatusChanged,
		TaskID:     taskID,
		ProjectID:  task.ProjectID,
		TaskPath:   task.Path,
		FeatureID:  task.FeatureID,
		FromStatus: "in_progress",
		ToStatus:   apiStatus,
	})

	// Release/finalize any dispatch lease now that the task lifecycle is terminal.
	tr.releaseDispatchLease(ctx, task.ProjectID, taskID)

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

	tr.cleanupTaskArtifacts(task)

	// Emit event
	completionEvt := RunnerEvent{
		Type:      eventType,
		Result:    result,
		TaskID:    taskID,
		ProjectID: task.ProjectID,
		TaskPath:  task.Path,
		FeatureID: task.FeatureID,
	}
	tr.emitEvent(completionEvt)

	// Post-task-end hook: fire-and-forget after all cleanup is done.
	// Hook failures don't affect the task lifecycle.
	if tr.hookDispatcher != nil {
		completionEvt.RunnerID = tr.runnerID
		tr.hookDispatcher.DispatchPost(completionEvt.ToEvent())
	}
}

// =============================================================================
// Claim Renewal
// =============================================================================

// renewClaims renews the lease for all running tasks. If renewal fails for a
// task (404 = claim expired/force-released), the runner aborts that task and
// releases it so another runner can pick it up.
func (tr *TaskRunner) renewClaims(ctx context.Context) {
	allProcesses := tr.processMgr.GetAll()
	if len(allProcesses) == 0 {
		return
	}

	for _, info := range allProcesses {
		if ctx.Err() != nil {
			return
		}

		task := info.Task
		err := tr.client.RenewClaim(ctx, task.ProjectID, task.ID, tr.runnerID)
		if err != nil {
			tr.logger.Printf("WARNING: claim renewal failed for %s/%s: %v — aborting task", task.ProjectID, task.ID, err)

			// Kill the running process
			tr.processMgr.Kill(ctx, task.ID)

			// Remove from process manager and the instance registry
			tr.processMgr.Remove(task.ID)
			tr.removeInstance(task.InstanceID)

			// Set task back to pending so another runner can pick it up
			if updateErr := tr.client.UpdateTaskStatus(ctx, task.Path, "pending"); updateErr != nil {
				tr.logger.Printf("failed to reset task status for %s after renewal failure: %v", task.ID, updateErr)
			}

			// Clean up tmux
			tr.cleanupTaskTmux(task)

			// Emit event
			tr.emitEvent(RunnerEvent{
				Type:      EventTaskReleased,
				TaskID:    task.ID,
				ProjectID: task.ProjectID,
				TaskPath:  task.Path,
				Reason:    "claim renewal failed",
			})

			tr.mu.Lock()
			tr.stats.Failed++
			if tr.processMgr.RunningCount() == 0 {
				tr.status = RunnerStatusPolling
			}
			tr.mu.Unlock()

			continue
		}

		slog.Debug("claim renewed", "project", task.ProjectID, "task_id", task.ID)
	}
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

// applyPauseCommand routes an SSE pause/resume command to the
// appropriate runner-local state mutator based on the command's
// ProjectID and Scope. This is the single source of truth for how the
// PWA's per-project + per-scope pause dials translate into runner
// state.
//
// ProjectID:
//
//	""     → global (all projects)
//	"foo"  → project foo only
//
// Scope:
//
//	"" or "all"     → both tasks and automations
//	"tasks"         → task-pause gate only (dispatch gate)
//	"automations"   → automation-pause gate only (carve-out)
//	"runner"        → this runner as a whole; ProjectID is ignored and the
//	                  dial is kept out of the project maps entirely, since
//	                  those are reconciled from GetRunnerStatus (project
//	                  state) and would wipe it on the next poll tick
//
// pause=true applies the pause; pause=false applies the resume.
func (tr *TaskRunner) applyPauseCommand(cmd RunnerCommand, pause bool) {
	projectID := cmd.ProjectID
	scope := cmd.Scope
	tasksScope := scope == "" || scope == "all" || scope == "tasks"
	automationsScope := scope == "" || scope == "all" || scope == "automations"

	verb := "paused"
	if !pause {
		verb = "resumed"
	}

	// scope="runner" is the runner-scoped dial (PUT /runners/{id}/pause).
	// It is NOT project state: routing it into serverTasksPaused would put
	// it in the map syncServerPauseState replaces from GetRunnerStatus on
	// every poll tick, and GetRunnerStatus only ever reports project pauses
	// — so the pause would silently lift within one interval. It gets its
	// own field, reconciled from this runner's registry row.
	if scope == PauseScopeRunner {
		tr.pauseMu.Lock()
		tr.runnerPaused = pause
		tr.pauseMu.Unlock()
		tr.wake()
		if pause {
			tr.emitEvent(RunnerEvent{Type: EventAllPaused})
		} else {
			tr.emitEvent(RunnerEvent{Type: EventAllResumed})
		}
		slog.Info("runner "+verb+" via SSE command", "scope", scope, "runner_id", tr.runnerID)
		return
	}

	// SSE pause/resume commands broadcast server-side state, so pauses land
	// in the server-origin maps (key "" = global) where syncServerPauseState
	// can reconcile them against GetRunnerStatus — TUI-local pauses are a
	// separate concern and stay untouched. Resumes additionally clear the
	// matching local state: an explicit user resume overrides a pause
	// regardless of origin (a StartPaused runner is resumed from the PWA
	// exactly this way, and the pre-server-origin code also cleared local
	// state on resume).
	tr.pauseMu.Lock()
	if tasksScope {
		if pause {
			tr.serverTasksPaused[projectID] = true
		} else {
			delete(tr.serverTasksPaused, projectID)
			if projectID == "" {
				tr.allPaused = false
			} else {
				delete(tr.pauseCache, projectID)
			}
		}
	}
	if automationsScope {
		if pause {
			tr.serverAutosPaused[projectID] = true
		} else {
			delete(tr.serverAutosPaused, projectID)
			if projectID == "" {
				tr.automationsPaused = false
			} else {
				delete(tr.automationPausedProjects, projectID)
			}
		}
	}
	tr.pauseMu.Unlock()
	tr.wake()

	// Emit the same lifecycle events the local pause methods produce so
	// TUI and event-forwarding consumers observe SSE-driven pauses
	// identically to local ones.
	if tasksScope {
		switch {
		case projectID == "" && pause:
			tr.emitEvent(RunnerEvent{Type: EventAllPaused})
		case projectID == "":
			tr.emitEvent(RunnerEvent{Type: EventAllResumed})
		case pause:
			tr.emitEvent(RunnerEvent{Type: EventProjectPaused, ProjectID: projectID})
		default:
			tr.emitEvent(RunnerEvent{Type: EventProjectResumed, ProjectID: projectID})
		}
	}
	slog.Info("runner "+verb+" via SSE command",
		"project_id", projectID, "scope", scope)
}

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

// PauseAutomations pauses automation-generated task processing.
func (tr *TaskRunner) PauseAutomations() {
	tr.pauseMu.Lock()
	tr.automationsPaused = true
	tr.pauseMu.Unlock()
	tr.wake()
}

// ResumeAutomations resumes automation-generated task processing.
func (tr *TaskRunner) ResumeAutomations() {
	tr.pauseMu.Lock()
	tr.automationsPaused = false
	tr.pauseMu.Unlock()
	tr.wake()
}

// IsAutomationsPaused returns whether automation-generated task processing is
// paused, by local request or server-side state.
func (tr *TaskRunner) IsAutomationsPaused() bool {
	tr.pauseMu.RLock()
	defer tr.pauseMu.RUnlock()
	return tr.automationsPaused || tr.serverAutosPaused[""]
}

func (tr *TaskRunner) wake() {
	select {
	case tr.wakeCh <- struct{}{}:
	default:
	}
}

func (tr *TaskRunner) canStartAutomationTask(ctx context.Context, task *types.ResolvedTask) (bool, error) {
	if !isAutomationGeneratedTask(task) {
		return true, nil
	}
	automationID := automationIDFromGeneratedBy(task.GeneratedBy)
	if automationID == "" {
		return true, nil
	}
	automation, err := tr.client.GetEntry(ctx, automationID)
	if err != nil {
		return false, err
	}
	if automation.Trigger == nil || automation.Trigger.MaxConcurrent <= 0 {
		return true, nil
	}
	running := 0
	for _, proc := range tr.processMgr.GetAllRunning() {
		if proc.Task.GeneratedBy == task.GeneratedBy {
			running++
		}
	}
	return running < automation.Trigger.MaxConcurrent, nil
}

// IsPaused returns whether a project is paused, by local request or
// server-side state.
func (tr *TaskRunner) IsPaused(projectID string) bool {
	tr.pauseMu.RLock()
	defer tr.pauseMu.RUnlock()
	if tr.runnerPaused || tr.allPaused || tr.serverTasksPaused[""] {
		return true
	}
	return tr.pauseCache[projectID] || tr.serverTasksPaused[projectID]
}

// IsAllPaused returns whether all projects are globally paused.
func (tr *TaskRunner) IsAllPaused() bool {
	tr.pauseMu.RLock()
	defer tr.pauseMu.RUnlock()
	return tr.runnerPaused || tr.allPaused || tr.serverTasksPaused[""]
}

// IsRunnerPaused reports the runner-scoped pause dial, set through
// PUT /runners/{runnerId}/pause. It is independent of every project dial: a
// paused runner accepts no work at all, automation-generated included.
func (tr *TaskRunner) IsRunnerPaused() bool {
	tr.pauseMu.RLock()
	defer tr.pauseMu.RUnlock()
	return tr.runnerPaused
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
// Default Model
// =============================================================================

// SetDefaultModel updates the runtime default model override.
// An empty string clears the override.
func (tr *TaskRunner) SetDefaultModel(model string) {
	tr.mu.Lock()
	tr.defaultModel = model
	tr.mu.Unlock()
}

// getDefaultModel returns the current runtime default model.
func (tr *TaskRunner) getDefaultModel() string {
	tr.mu.RLock()
	m := tr.defaultModel
	tr.mu.RUnlock()
	return m
}

// =============================================================================
// Status
// =============================================================================

// GetStatus returns a snapshot of the runner's current state.
func (tr *TaskRunner) GetStatus() RunnerStatusInfo {
	projects := tr.getProjects()

	tr.mu.RLock()
	status := tr.status
	stats := tr.stats
	startedAt := tr.startedAt
	tr.mu.RUnlock()

	tr.pauseMu.RLock()
	var paused []string
	if tr.allPaused {
		paused = make([]string, len(projects))
		copy(paused, projects)
	} else {
		for p := range tr.pauseCache {
			paused = append(paused, p)
		}
	}
	tr.pauseMu.RUnlock()

	return RunnerStatusInfo{
		RunnerID:    tr.runnerID,
		Status:      status,
		Projects:    projects,
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

// handleFeatureHookEvents dispatches hooks for feature lifecycle events.
// Registered as an OnEvent handler when a HookDispatcher is available.
// Feature events are emitted by the FeatureTracker; this handler forwards
// them to the hook dispatcher as post-hooks (fire-and-forget).
func (tr *TaskRunner) handleFeatureHookEvents(event RunnerEvent) {
	if tr.hookDispatcher == nil {
		return
	}

	switch event.Type {
	case EventFeatureStarted, EventFeatureCompleted, EventFeatureBlocked, EventFeatureProgress:
		// All feature events are dispatched as post-hooks (fire-and-forget).
		// Feature events are derived from task events and cannot be blocked.
		tr.hookDispatcher.DispatchPost(event.ToEvent())
	}
}

// =============================================================================
// Log Streamer Tracking
// =============================================================================

// trackLogStreamer records a log streamer for a task so it can be stopped on completion.
func (tr *TaskRunner) trackLogStreamer(taskID string, ls *LogStreamer) {
	tr.logMu.Lock()
	tr.logStreamers[taskID] = ls
	tr.logMu.Unlock()
}

// stopLogStreamer stops and removes the log streamer for a task.
func (tr *TaskRunner) stopLogStreamer(taskID string) {
	tr.logMu.Lock()
	ls, ok := tr.logStreamers[taskID]
	if ok {
		delete(tr.logStreamers, taskID)
	}
	tr.logMu.Unlock()

	if ok && ls != nil {
		ls.Stop()
	}
}

// =============================================================================
// Capability Filtering
// =============================================================================

// dispatchPushEnabled reports whether the runner should treat itself as
// push-dispatch capable. In production this is always true because
// LoadConfigFrom rejects dispatch_push: false at config load time. The
// function preserves the original three-way check (config field, legacy
// Passive shim, "dispatch_push" capability advertisement) so existing
// tests can still construct runners that exercise the (otherwise
// unreachable) poll-fetch code path.
//
// Deprecated: production callers should treat push dispatch as always-on.
// The function will be removed once the poll-fetch branch is deleted (see
// brain plan ehwvfq8e Tier 2).
func (tr *TaskRunner) dispatchPushEnabled() bool {
	if tr.config.Passive || tr.config.DispatchPush {
		return true
	}
	for _, capability := range tr.config.Capabilities {
		if capability == "dispatch_push" {
			return true
		}
	}
	return false
}

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

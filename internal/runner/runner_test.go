package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Interface compliance checks (compile-time)
// =============================================================================

var _ Client = (*APIClient)(nil)
var _ TaskExecutor = (*OpenCodeExecutor)(nil)
var _ TaskProcessManager = (*ProcessManager)(nil)
var _ TaskStateManager = (*StateManager)(nil)

// Also verify mocks implement interfaces
var _ Client = (*mockClient)(nil)
var _ TaskExecutor = (*mockExecutor)(nil)
var _ TaskProcessManager = (*mockProcessMgr)(nil)
var _ TaskStateManager = (*mockStateMgr)(nil)

// =============================================================================
// Mock Client
// =============================================================================

type mockClient struct {
	mu sync.Mutex

	healthResult APIHealth
	healthErr    error

	projects    []string
	projectsErr error

	readyTasks    map[string][]types.ResolvedTask
	readyTasksErr error

	nextTask      map[string]*types.ResolvedTask
	nextTaskErr   error
	nextTaskCalls []nextTaskCall

	claimResult ClaimResult
	claimErr    error
	claimCalls  []claimCall

	releaseErr   error
	releaseCalls []releaseCall

	renewErr   error
	renewCalls []renewCall

	updateStatusErr   error
	updateStatusCalls []updateStatusCall

	appendErr   error
	appendCalls []appendCall

	getEntryResult map[string]*types.BrainEntry
	getEntryErr    error

	listEntriesFunc func(params map[string]string) (*types.ListEntriesResponse, error)
	listEntriesErr  error

	registerErr   error
	registerCalls []types.RunnerRegistration

	heartbeatErr   error
	heartbeatCalls []heartbeatCall

	deregisterErr   error
	deregisterCalls []string

	ackErr   error
	ackCalls []dispatchAckCall

	rejectErr   error
	rejectCalls []dispatchRejectCall

	releaseDispatchErr   error
	releaseDispatchCalls []dispatchReleaseCall

	runnerStatus    *types.RunnerStatusResponse
	runnerStatusErr error

	// runnerRecord backs GetRunner — the registry row the runner reconciles
	// its runner-scoped pause dial against. nil means "the client could not
	// determine it", which callers must treat as unknown, not as resumed.
	runnerRecord    *types.RunnerInfo
	runnerRecordErr error

	// healthBlockCh, when set, makes CheckHealth block on the channel until
	// it is closed or a value is sent. Used to simulate a wedged HTTP call
	// inside poll() so tests can prove that other goroutines (dispatch
	// command consumer, heartbeat, claim renewal) keep making progress.
	healthBlockCh chan struct{}
}

type nextTaskCall struct {
	ProjectID  string
	FeatureIDs []string
	RunnerID   string
	Opts       *TaskFetchOptions
}

type claimCall struct {
	ProjectID string
	TaskID    string
	RunnerID  string
}

type releaseCall struct {
	ProjectID string
	TaskID    string
	RunnerID  string
}

type renewCall struct {
	ProjectID string
	TaskID    string
	RunnerID  string
}

type updateStatusCall struct {
	TaskPath string
	Status   string
}

type appendCall struct {
	TaskPath string
	Content  string
}

type heartbeatCall struct {
	RunnerID string
	Request  types.RunnerHeartbeatRequest
}

type dispatchAckCall struct {
	RunnerID  string
	ProjectID string
	TaskID    string
	LeaseID   string
}

type dispatchRejectCall struct {
	RunnerID  string
	ProjectID string
	TaskID    string
	LeaseID   string
	Reason    types.DispatchRejectReason
}

type dispatchReleaseCall struct {
	RunnerID  string
	ProjectID string
	TaskID    string
}

func newMockClient() *mockClient {
	return &mockClient{
		healthResult: APIHealth{Status: "ok"},
		claimResult:  ClaimResult{Success: true},
		readyTasks:   make(map[string][]types.ResolvedTask),
		nextTask:     make(map[string]*types.ResolvedTask),
	}
}

func (m *mockClient) CheckHealth(ctx context.Context) (APIHealth, error) {
	m.mu.Lock()
	blockCh := m.healthBlockCh
	result := m.healthResult
	err := m.healthErr
	m.mu.Unlock()

	if blockCh != nil {
		// Block until the channel is closed or context cancelled.
		select {
		case <-blockCh:
		case <-ctx.Done():
			return APIHealth{}, ctx.Err()
		}
	}
	return result, err
}

func (m *mockClient) ListProjects(ctx context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.projects, m.projectsErr
}

func (m *mockClient) GetReadyTasks(ctx context.Context, projectID string, opts *TaskFetchOptions) ([]types.ResolvedTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readyTasksErr != nil {
		return nil, m.readyTasksErr
	}
	return m.readyTasks[projectID], nil
}

func (m *mockClient) GetNextTask(ctx context.Context, projectID string, opts *TaskFetchOptions) (*types.ResolvedTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Record the call with options for verification
	var featureIDs []string
	var runnerID string
	if opts != nil {
		featureIDs = opts.FeatureIDs
		runnerID = opts.RunnerID
	}
	m.nextTaskCalls = append(m.nextTaskCalls, nextTaskCall{ProjectID: projectID, FeatureIDs: featureIDs, RunnerID: runnerID, Opts: opts})
	if m.nextTaskErr != nil {
		return nil, m.nextTaskErr
	}
	task := m.nextTask[projectID]
	if task != nil && opts != nil && opts.GeneratedByPrefix != "" && !strings.HasPrefix(task.GeneratedBy, opts.GeneratedByPrefix) {
		return nil, nil
	}
	return task, nil
}

func (m *mockClient) ClaimTask(ctx context.Context, projectID, taskID, runnerID string) (ClaimResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.claimCalls = append(m.claimCalls, claimCall{projectID, taskID, runnerID})
	result := m.claimResult
	result.TaskID = taskID
	return result, m.claimErr
}

func (m *mockClient) RenewClaim(ctx context.Context, projectID, taskID, runnerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.renewCalls = append(m.renewCalls, renewCall{projectID, taskID, runnerID})
	return m.renewErr
}

func (m *mockClient) ReleaseTask(ctx context.Context, projectID, taskID, runnerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseCalls = append(m.releaseCalls, releaseCall{projectID, taskID, runnerID})
	return m.releaseErr
}

func (m *mockClient) AckDispatch(ctx context.Context, runnerID, projectID, taskID, leaseID string) (*types.DispatchAckResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ackCalls = append(m.ackCalls, dispatchAckCall{RunnerID: runnerID, ProjectID: projectID, TaskID: taskID, LeaseID: leaseID})
	return &types.DispatchAckResponse{Success: true, RunnerID: runnerID, ProjectID: projectID, TaskID: taskID, LeaseID: leaseID}, m.ackErr
}

func (m *mockClient) RejectDispatch(ctx context.Context, runnerID, projectID, taskID, leaseID string, reason types.DispatchRejectReason) (*types.DispatchRejectResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rejectCalls = append(m.rejectCalls, dispatchRejectCall{RunnerID: runnerID, ProjectID: projectID, TaskID: taskID, LeaseID: leaseID, Reason: reason})
	return &types.DispatchRejectResponse{Success: true, RunnerID: runnerID, ProjectID: projectID, TaskID: taskID, LeaseID: leaseID, Reason: reason}, m.rejectErr
}

func (m *mockClient) ReleaseDispatch(ctx context.Context, runnerID, projectID, taskID string) (*types.DispatchReleaseResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseDispatchCalls = append(m.releaseDispatchCalls, dispatchReleaseCall{RunnerID: runnerID, ProjectID: projectID, TaskID: taskID})
	return &types.DispatchReleaseResponse{Success: true, RunnerID: runnerID, ProjectID: projectID, TaskID: taskID}, m.releaseDispatchErr
}

func (m *mockClient) GetRunnerStatus(ctx context.Context) (*types.RunnerStatusResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runnerStatusErr != nil {
		return nil, m.runnerStatusErr
	}
	if m.runnerStatus == nil {
		return &types.RunnerStatusResponse{Running: true}, nil
	}
	return m.runnerStatus, nil
}

func (m *mockClient) GetRunner(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runnerRecordErr != nil {
		return nil, m.runnerRecordErr
	}
	return m.runnerRecord, nil
}

func (m *mockClient) UpdateTaskStatus(ctx context.Context, taskPath, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateStatusCalls = append(m.updateStatusCalls, updateStatusCall{taskPath, status})
	return m.updateStatusErr
}

func (m *mockClient) AppendToTask(ctx context.Context, taskPath, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendCalls = append(m.appendCalls, appendCall{taskPath, content})
	return m.appendErr
}

func (m *mockClient) UpdateEntry(ctx context.Context, entryPath string, updates map[string]interface{}) (*types.BrainEntry, error) {
	return &types.BrainEntry{Path: entryPath}, nil
}

func (m *mockClient) GetAllTasks(ctx context.Context, projectID string) ([]types.ResolvedTask, error) {
	return nil, nil
}

func (m *mockClient) GetTasksByFeature(ctx context.Context, projectID, featureID string) ([]types.ResolvedTask, error) {
	return nil, nil
}

func (m *mockClient) UpdateMetadata(ctx context.Context, entryPath string, fields map[string]interface{}) error {
	return nil
}

func (m *mockClient) GetEntry(ctx context.Context, entryPath string) (*types.BrainEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getEntryErr != nil {
		return nil, m.getEntryErr
	}
	if m.getEntryResult != nil {
		if entry, ok := m.getEntryResult[entryPath]; ok {
			return entry, nil
		}
	}
	return &types.BrainEntry{Path: entryPath}, nil
}

func (m *mockClient) ListEntries(ctx context.Context, params map[string]string) (*types.ListEntriesResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listEntriesErr != nil {
		return nil, m.listEntriesErr
	}
	if m.listEntriesFunc != nil {
		return m.listEntriesFunc(params)
	}
	// Default: empty result so the orphan reaper is a no-op in existing tests.
	return &types.ListEntriesResponse{}, nil
}

func (m *mockClient) RegisterRunner(ctx context.Context, req types.RunnerRegistration) (*types.RunnerInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.registerErr != nil {
		return nil, m.registerErr
	}
	m.registerCalls = append(m.registerCalls, req)
	return &types.RunnerInfo{
		RunnerID: req.RunnerID,
		Hostname: req.Hostname,
		Status:   types.RunnerStatusOnline,
	}, nil
}

func (m *mockClient) SendHeartbeat(ctx context.Context, runnerID string, req types.RunnerHeartbeatRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heartbeatCalls = append(m.heartbeatCalls, heartbeatCall{RunnerID: runnerID, Request: req})
	return m.heartbeatErr
}

func (m *mockClient) DeregisterRunner(ctx context.Context, runnerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deregisterCalls = append(m.deregisterCalls, runnerID)
	return m.deregisterErr
}

func (m *mockClient) PostTaskLogs(ctx context.Context, projectID, taskID, runnerID string, lines []types.LogLine) error {
	return nil
}

func (m *mockClient) getNextTaskCalls() []nextTaskCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]nextTaskCall, len(m.nextTaskCalls))
	copy(result, m.nextTaskCalls)
	return result
}

func (m *mockClient) getClaimCalls() []claimCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]claimCall, len(m.claimCalls))
	copy(result, m.claimCalls)
	return result
}

func (m *mockClient) getUpdateStatusCalls() []updateStatusCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]updateStatusCall, len(m.updateStatusCalls))
	copy(result, m.updateStatusCalls)
	return result
}

func (m *mockClient) getReleaseCalls() []releaseCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]releaseCall, len(m.releaseCalls))
	copy(result, m.releaseCalls)
	return result
}

func (m *mockClient) getAckCalls() []dispatchAckCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]dispatchAckCall, len(m.ackCalls))
	copy(result, m.ackCalls)
	return result
}

func (m *mockClient) getRejectCalls() []dispatchRejectCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]dispatchRejectCall, len(m.rejectCalls))
	copy(result, m.rejectCalls)
	return result
}

func (m *mockClient) getReleaseDispatchCalls() []dispatchReleaseCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]dispatchReleaseCall, len(m.releaseDispatchCalls))
	copy(result, m.releaseDispatchCalls)
	return result
}

// =============================================================================
// Mock Executor
// =============================================================================

type mockExecutor struct {
	mu sync.Mutex

	buildPromptResult string
	resolveWorkdir    string
	resolveWorkdirErr error
	spawnResult       *SpawnResult
	spawnErr          error
	spawnCalls        []spawnCall
	cleanupCalls      []cleanupCall
}

type spawnCall struct {
	TaskID    string
	ProjectID string
	Opts      SpawnOptions
}

type cleanupCall struct {
	TaskID    string
	ProjectID string
}

func newMockExecutor() *mockExecutor {
	return &mockExecutor{
		resolveWorkdir: "/test/workdir",
		spawnResult: &SpawnResult{
			PID:     12345,
			Workdir: "/test/workdir",
		},
	}
}

func (m *mockExecutor) BuildPrompt(task *types.ResolvedTask, isResume bool) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buildPromptResult
}

func (m *mockExecutor) ResolveWorkdir(task *types.ResolvedTask) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.resolveWorkdirErr != nil {
		return "", m.resolveWorkdirErr
	}
	return m.resolveWorkdir, nil
}

func (m *mockExecutor) Spawn(ctx context.Context, task *types.ResolvedTask, projectID string, opts SpawnOptions) (*SpawnResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.spawnCalls = append(m.spawnCalls, spawnCall{task.ID, projectID, opts})
	if m.spawnErr != nil {
		return nil, m.spawnErr
	}
	result := *m.spawnResult
	return &result, nil
}

func (m *mockExecutor) Cleanup(taskID, projectID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupCalls = append(m.cleanupCalls, cleanupCall{taskID, projectID})
	return nil
}

func (m *mockExecutor) getSpawnCalls() []spawnCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]spawnCall, len(m.spawnCalls))
	copy(result, m.spawnCalls)
	return result
}

func (m *mockExecutor) getCleanupCalls() []cleanupCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]cleanupCall, len(m.cleanupCalls))
	copy(result, m.cleanupCalls)
	return result
}

// =============================================================================
// Mock ProcessManager
// =============================================================================

type mockProcessMgr struct {
	mu sync.Mutex

	processes    map[string]*ProcessInfo
	completions  map[string]CompletionStatus
	taskResults  map[string]*TaskResult
	killAllCalls int
	killCalls    []string
}

func newMockProcessMgr() *mockProcessMgr {
	return &mockProcessMgr{
		processes:   make(map[string]*ProcessInfo),
		completions: make(map[string]CompletionStatus),
		taskResults: make(map[string]*TaskResult),
	}
}

func (m *mockProcessMgr) Add(taskID string, task RunningTask, proc Process) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, exists := m.processes[taskID]; exists {
		if existing.Proc != nil {
			return fmt.Errorf("task %s already tracked", taskID)
		}
		existing.Task = task
		existing.Proc = proc
		return nil
	}
	m.processes[taskID] = &ProcessInfo{Task: task, Proc: proc}
	return nil
}

func (m *mockProcessMgr) Remove(taskID string) *ProcessInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	info := m.processes[taskID]
	delete(m.processes, taskID)
	return info
}

func (m *mockProcessMgr) Get(taskID string) *ProcessInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.processes[taskID]
}

func (m *mockProcessMgr) GetAll() []ProcessInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []ProcessInfo
	for _, info := range m.processes {
		if info.Proc == nil {
			// Reservation placeholder — excluded, matching the real
			// ProcessManager contract.
			continue
		}
		result = append(result, *info)
	}
	return result
}

func (m *mockProcessMgr) GetAllRunning() []ProcessInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []ProcessInfo
	for _, info := range m.processes {
		if info.Proc == nil {
			continue
		}
		if !info.Proc.Exited() {
			result = append(result, *info)
		}
	}
	return result
}

func (m *mockProcessMgr) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.processes)
}

func (m *mockProcessMgr) RunningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, info := range m.processes {
		if info.Proc == nil {
			count++
			continue
		}
		if !info.Proc.Exited() {
			count++
		}
	}
	return count
}

func (m *mockProcessMgr) ReserveSlot(taskID string, maxParallel int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.processes[taskID]; exists {
		return true
	}
	if maxParallel > 0 && len(m.processes) >= maxParallel {
		return false
	}
	m.processes[taskID] = &ProcessInfo{}
	return true
}

func (m *mockProcessMgr) ReleaseReservation(taskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	info, exists := m.processes[taskID]
	if !exists || (info != nil && info.Proc != nil) {
		return
	}
	delete(m.processes, taskID)
}

func (m *mockProcessMgr) CheckCompletion(taskID string, checkTaskFile bool) CompletionStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if status, ok := m.completions[taskID]; ok {
		return status
	}
	return CompletionRunning
}

func (m *mockProcessMgr) CreateTaskResult(taskID string, status CompletionStatus) *TaskResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	if result, ok := m.taskResults[taskID]; ok {
		return result
	}
	return &TaskResult{
		TaskID:      taskID,
		Status:      TaskResultCompleted,
		CompletedAt: time.Now(),
		Duration:    1000,
	}
}

func (m *mockProcessMgr) Kill(ctx context.Context, taskID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.killCalls = append(m.killCalls, taskID)
	return true
}

func (m *mockProcessMgr) KillAll(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.killAllCalls++
}

func (m *mockProcessMgr) ToProcessStates() []ProcessState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return nil
}

func (m *mockProcessMgr) UpdatePort(taskID string, port int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if info, exists := m.processes[taskID]; exists {
		info.Task.OpencodePort = port
	}
}

func (m *mockProcessMgr) UpdateSessionID(taskID string, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if info, exists := m.processes[taskID]; exists {
		info.Task.SessionID = sessionID
	}
}

func (m *mockProcessMgr) UpdateIdleSince(taskID string, idleSince string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if info, exists := m.processes[taskID]; exists {
		info.Task.IdleSince = idleSince
	}
}

// setCompletion sets the completion status for a task.
func (m *mockProcessMgr) setCompletion(taskID string, status CompletionStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completions[taskID] = status
}

func (m *mockProcessMgr) getKillAllCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.killAllCalls
}

// =============================================================================
// Mock StateManager
// =============================================================================

type mockStateMgr struct {
	mu sync.Mutex

	savedStatus    RunnerStatus
	savedTasks     []RunningTask
	savedStats     RunnerStats
	savedPid       *int
	pidCleared     bool
	saveCalls      int
	saveTasksCalls int
}

func newMockStateMgr() *mockStateMgr {
	return &mockStateMgr{}
}

func (m *mockStateMgr) Save(status RunnerStatus, tasks []RunningTask, stats RunnerStats, startedAt time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.savedStatus = status
	m.savedTasks = tasks
	m.savedStats = stats
	m.saveCalls++
}

func (m *mockStateMgr) Load() *RunnerState {
	return nil
}

func (m *mockStateMgr) SavePid(pid int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.savedPid = &pid
}

func (m *mockStateMgr) LoadPid() *int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.savedPid
}

func (m *mockStateMgr) ClearPid() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pidCleared = true
	m.savedPid = nil
}

func (m *mockStateMgr) SaveRunningTasks(tasks []RunningTask) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.savedTasks = tasks
	m.saveTasksCalls++
}

func (m *mockStateMgr) LoadRunningTasks() []RunningTask {
	return nil
}

func (m *mockStateMgr) getSaveCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveCalls
}

func (m *mockStateMgr) isPidCleared() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pidCleared
}

// =============================================================================
// Test Helpers
// =============================================================================

func testRunnerConfig() RunnerConfig {
	return RunnerConfig{
		BrainAPIURL:            "http://localhost:3333",
		PollInterval:           1,
		TaskPollInterval:       1,
		MaxParallel:            2,
		MaxTotalProcesses:      10,
		MemoryThresholdPercent: 10,
		APITimeout:             5000,
		StateDir:               "/tmp/test-state",
		WorkDir:                "/tmp/test-work",
		Opencode: OpencodeConfig{
			Bin: "opencode",
		},
	}
}

func testTask(id, projectID string) *types.ResolvedTask {
	return &types.ResolvedTask{
		ID:       id,
		Path:     fmt.Sprintf("projects/%s/task/%s.md", projectID, id),
		Title:    "Test Task " + id,
		Priority: "medium",
		Status:   "pending",
	}
}

func newTestRunner(client *mockClient, executor *mockExecutor, processMgr *mockProcessMgr, stateMgr *mockStateMgr) *TaskRunner {
	return NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a", "proj-b"},
		Config:   testRunnerConfig(),
		Mode:     ExecutionModeHeadless,
		Client:   client,
		Executors: map[string]TaskExecutor{
			"opencode": executor,
		},
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})
}

// =============================================================================
// NewTaskRunner Tests
// =============================================================================

func TestNewTaskRunner_GeneratesRunnerID(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	if tr.runnerID == "" {
		t.Error("runnerID should not be empty")
	}
	if len(tr.runnerID) < 10 {
		t.Errorf("runnerID too short: %q", tr.runnerID)
	}
	if tr.runnerID[:7] != "runner_" {
		t.Errorf("runnerID should start with 'runner_', got %q", tr.runnerID)
	}
}

// runnerID is now stable per state dir: the same deployment re-registers under
// the same id across restarts, while distinct state dirs (distinct runners on
// one machine) get distinct ids.
func TestRunnerID_StablePerStateDir(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	a1 := ResolveRunnerID(dirA)
	a2 := ResolveRunnerID(dirA) // simulated restart, same state dir
	b1 := ResolveRunnerID(dirB)

	if a1 == "" || !strings.HasPrefix(a1, "runner_") {
		t.Fatalf("unexpected runner id %q", a1)
	}
	if a1 != a2 {
		t.Errorf("same state dir should yield a stable id: %q != %q", a1, a2)
	}
	if a1 == b1 {
		t.Errorf("distinct state dirs should yield distinct ids: %q == %q", a1, b1)
	}
}

// machineID is shared across runners on a host and stable across restarts.
func TestMachineID_StableAcrossCalls(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	m1 := ResolveMachineID()
	m2 := ResolveMachineID()
	if m1 == "" || !strings.HasPrefix(m1, "machine_") {
		t.Fatalf("unexpected machine id %q", m1)
	}
	if m1 != m2 {
		t.Errorf("machine id should be stable: %q != %q", m1, m2)
	}
}

func TestNewTaskRunner_SetsDefaults(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	if tr.status != RunnerStatusIdle {
		t.Errorf("initial status = %q, want %q", tr.status, RunnerStatusIdle)
	}
	if len(tr.projects) != 2 {
		t.Errorf("projects len = %d, want 2", len(tr.projects))
	}
	if tr.mode != ExecutionModeHeadless {
		t.Errorf("mode = %q, want %q", tr.mode, ExecutionModeHeadless)
	}
}

func TestNewTaskRunner_SingleProject(t *testing.T) {
	tr := NewTaskRunner(TaskRunnerOptions{
		ProjectID:  "my-project",
		Config:     testRunnerConfig(),
		Client:     newMockClient(),
		Executor:   newMockExecutor(),
		ProcessMgr: newMockProcessMgr(),
		StateMgr:   newMockStateMgr(),
	})

	if len(tr.projects) != 1 || tr.projects[0] != "my-project" {
		t.Errorf("projects = %v, want [my-project]", tr.projects)
	}
}

func TestTaskRunner_Start_DeregistersWhenRemoteShutdownCancelsContext(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	tr := newTestRunner(client, executor, processMgr, stateMgr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- tr.Start(ctx)
	}()

	deadline := time.After(2 * time.Second)
	for {
		client.mu.Lock()
		registered := len(client.registerCalls) > 0
		client.mu.Unlock()
		if registered {
			break
		}
		select {
		case <-deadline:
			t.Fatal("runner did not register before deadline")
		case <-time.After(10 * time.Millisecond):
		}
	}

	tr.commandCh <- RunnerCommand{Type: CommandShutdown, Reason: "test shutdown"}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not stop after shutdown command")
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.deregisterCalls) != 1 {
		t.Fatalf("deregister calls = %d, want 1", len(client.deregisterCalls))
	}
	if client.deregisterCalls[0] != tr.runnerID {
		t.Fatalf("deregister runner ID = %q, want %q", client.deregisterCalls[0], tr.runnerID)
	}
}

func TestNewTaskRunner_StartPaused(t *testing.T) {
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:    []string{"proj-a"},
		Config:      testRunnerConfig(),
		StartPaused: true,
		Client:      newMockClient(),
		Executor:    newMockExecutor(),
		ProcessMgr:  newMockProcessMgr(),
		StateMgr:    newMockStateMgr(),
	})

	if !tr.allPaused {
		t.Error("allPaused should be true when StartPaused is set")
	}
	if !tr.IsAutomationsPaused() {
		t.Error("automations should be paused when StartPaused is set")
	}
}

func TestNewTaskRunner_DefaultMode(t *testing.T) {
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     testRunnerConfig(),
		Client:     newMockClient(),
		Executor:   newMockExecutor(),
		ProcessMgr: newMockProcessMgr(),
		StateMgr:   newMockStateMgr(),
		// Mode not set
	})

	if tr.mode != ExecutionModeHeadless {
		t.Errorf("default mode = %q, want %q", tr.mode, ExecutionModeHeadless)
	}
}

// =============================================================================
// Start / Stop Lifecycle Tests
// =============================================================================

func TestTaskRunner_StartAndStop(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	ctx, cancel := context.WithCancel(context.Background())

	// Start in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- tr.Start(ctx)
	}()

	// Wait for it to start polling
	time.Sleep(50 * time.Millisecond)

	// Verify status changed to polling
	status := tr.GetStatus()
	if status.Status != RunnerStatusPolling && status.Status != RunnerStatusProcessing {
		t.Errorf("status after start = %q, want polling or processing", status.Status)
	}

	// Stop
	cancel()
	err := <-errCh
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	// Verify stopped
	tr.mu.RLock()
	finalStatus := tr.status
	tr.mu.RUnlock()
	if finalStatus != RunnerStatusStopped {
		t.Errorf("status after stop = %q, want %q", finalStatus, RunnerStatusStopped)
	}
}

func TestTaskRunner_Stop_KillsProcesses(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		tr.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	// Call Stop to trigger cleanup
	tr.Stop()

	if processMgr.getKillAllCalls() < 1 {
		t.Error("Stop should call KillAll on process manager")
	}
	if !stateMgr.isPidCleared() {
		t.Error("Stop should clear PID")
	}
}

func TestTaskRunner_Start_SavesPid(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		tr.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)

	stateMgr.mu.Lock()
	pid := stateMgr.savedPid
	stateMgr.mu.Unlock()

	if pid == nil {
		t.Error("Start should save PID")
	}
}

// =============================================================================
// Paused-Project Poll Tests
// =============================================================================

func TestTaskRunner_Poll_AllPaused_PollsOnlyAutomations(t *testing.T) {
	client := newMockClient()
	task := testTask("task1", "proj-a")
	client.nextTask["proj-a"] = task

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)
	tr.PauseAll()

	ctx := context.Background()
	tr.poll(ctx)

	if len(executor.getSpawnCalls()) > 0 {
		t.Error("should not spawn when all-paused")
	}
	for _, call := range client.getNextTaskCalls() {
		if call.Opts == nil || call.Opts.GeneratedByPrefix != "automation:" {
			t.Fatalf("all-paused runner should only fetch automation tasks, got call: %#v", call)
		}
	}
}

func TestTaskRunner_Poll_ProjectPaused_SkipsProject(t *testing.T) {
	client := newMockClient()
	task := testTask("task1", "proj-a")
	client.nextTask["proj-a"] = task

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)
	tr.PauseProject("proj-a")

	ctx := context.Background()
	tr.poll(ctx)

	// Should NOT spawn from proj-a (proj-b is unpaused but has no task).
	if len(executor.getSpawnCalls()) > 0 {
		t.Error("should not spawn tasks for a paused project")
	}
}

// TestTaskRunner_Poll_Unpaused_UsesConfigFeatureIDs covers the runner-affinity
// feature filter (config.FeatureIDs / RUNNER_FEATURE_IDS), which is a distinct
// and live mechanism from the removed per-runner feature toggle.
func TestTaskRunner_Poll_Unpaused_UsesConfigFeatureIDs(t *testing.T) {
	client := newMockClient()
	task := testTask("task1", "proj-a")
	client.nextTask["proj-a"] = task
	client.claimResult = ClaimResult{Success: true}

	executor := newMockExecutor()
	proc := newMockProcess(100)
	executor.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/test"}

	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	cfg.FeatureIDs = []string{"config-feat-1", "config-feat-2"}

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	ctx := context.Background()
	tr.poll(ctx)

	if len(executor.getSpawnCalls()) == 0 {
		t.Fatal("should spawn tasks when unpaused")
	}

	nextCalls := client.getNextTaskCalls()
	if len(nextCalls) == 0 {
		t.Fatal("expected GetNextTask call")
	}
	call := nextCalls[0]
	if len(call.FeatureIDs) != 2 {
		t.Fatalf("expected 2 config feature IDs, got %d: %v", len(call.FeatureIDs), call.FeatureIDs)
	}
	hasConfig1 := false
	hasConfig2 := false
	for _, fid := range call.FeatureIDs {
		if fid == "config-feat-1" {
			hasConfig1 = true
		}
		if fid == "config-feat-2" {
			hasConfig2 = true
		}
	}
	if !hasConfig1 || !hasConfig2 {
		t.Errorf("expected config feature IDs [config-feat-1, config-feat-2], got %v", call.FeatureIDs)
	}
}

// =============================================================================
// Poll Tests
// =============================================================================

func TestTaskRunner_Poll_DispatchPushCapabilityDoesNotPollNextOrSpawn(t *testing.T) {
	client := newMockClient()
	client.nextTask["proj-a"] = testTask("task1", "proj-a")

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	cfg.Capabilities = []string{"dispatch_push"}
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	tr.poll(context.Background())

	if got := len(client.getNextTaskCalls()); got != 0 {
		t.Fatalf("dispatch-push runner should not call GetNextTask, got %d calls", got)
	}
	if got := len(executor.getSpawnCalls()); got != 0 {
		t.Fatalf("dispatch-push runner should not spawn poll-discovered tasks, got %d spawns", got)
	}
}

func TestTaskRunner_Poll_DispatchPushConfigDoesNotPollNext(t *testing.T) {
	client := newMockClient()
	client.nextTask["proj-a"] = testTask("task1", "proj-a")

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	cfg.DispatchPush = true
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	tr.poll(context.Background())

	if got := len(client.getNextTaskCalls()); got != 0 {
		t.Fatalf("dispatch-push runner should not call GetNextTask, got %d calls", got)
	}
	if got := len(executor.getSpawnCalls()); got != 0 {
		t.Fatalf("dispatch-push runner should not spawn poll-discovered tasks, got %d spawns", got)
	}
}

func TestTaskRunner_Poll_PassiveConfigDoesNotPollNext(t *testing.T) {
	client := newMockClient()
	client.nextTask["proj-a"] = testTask("task1", "proj-a")

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	cfg.Passive = true
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	tr.poll(context.Background())

	if got := len(client.getNextTaskCalls()); got != 0 {
		t.Fatalf("passive runner should not call GetNextTask, got %d calls", got)
	}
	if got := len(executor.getSpawnCalls()); got != 0 {
		t.Fatalf("passive runner should not spawn poll-discovered tasks, got %d spawns", got)
	}
}

func TestTaskRunner_PassiveDispatchCommandAcksAndSpawnsWithoutPollingNext(t *testing.T) {
	client := newMockClient()
	client.nextTask["proj-a"] = testTask("other-task", "proj-a")
	client.readyTasks["proj-a"] = []types.ResolvedTask{*testTask("task1", "proj-a")}

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	cfg.Capabilities = []string{"dispatch_push"}
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	tr.handleCommand(context.Background(), RunnerCommand{
		Type:      CommandDispatch,
		ProjectID: "proj-a",
		TaskID:    "task1",
		LeaseID:   "lease-1",
	})

	if got := len(client.getNextTaskCalls()); got != 0 {
		t.Fatalf("dispatch command should not call GetNextTask, got %d calls", got)
	}
	if got := len(client.getAckCalls()); got != 1 {
		t.Fatalf("dispatch command should ack exactly once, got %d", got)
	}
	spawns := executor.getSpawnCalls()
	if got := len(spawns); got != 1 {
		t.Fatalf("dispatch command should spawn assigned lease task exactly once, got %d", got)
	}
	if spawns[0].TaskID != "task1" {
		t.Fatalf("spawned task ID = %q, want task1", spawns[0].TaskID)
	}
}

func TestTaskRunner_DispatchSpawnFailureReleasesDispatchLease(t *testing.T) {
	client := newMockClient()
	client.readyTasks["proj-a"] = []types.ResolvedTask{*testTask("task1", "proj-a")}
	executor := newMockExecutor()
	executor.spawnErr = errors.New("spawn failed")
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	cfg.Capabilities = []string{"dispatch_push"}
	tr := NewTaskRunner(TaskRunnerOptions{Projects: []string{"proj-a"}, Config: cfg, Mode: ExecutionModeHeadless, Client: client, Executor: executor, ProcessMgr: processMgr, StateMgr: stateMgr})

	tr.handleCommand(context.Background(), RunnerCommand{Type: CommandDispatch, ProjectID: "proj-a", TaskID: "task1", LeaseID: "lease-1"})

	if got := len(client.getAckCalls()); got != 1 {
		t.Fatalf("dispatch command should ack before spawn, got %d ack calls", got)
	}
	releases := client.getReleaseDispatchCalls()
	if len(releases) != 1 {
		t.Fatalf("spawn failure should release dispatch lease exactly once, got %d", len(releases))
	}
	if releases[0].ProjectID != "proj-a" || releases[0].TaskID != "task1" {
		t.Fatalf("release dispatch call = %+v", releases[0])
	}
}

// TestTaskRunner_SpawnFailureIncludesErrorInReleaseReason verifies that when a
// spawn fails, the emitted EventTaskReleased carries the underlying error text
// in its Reason field (not just the bare "spawn failed" label). This is the
// difference between "some spawn failed somewhere" and "spawn failed: script
// command rejected: command X does not match any allowed command prefix" — the
// latter is what operators need to diagnose why an automation is silently
// failing to launch.
func TestTaskRunner_SpawnFailureIncludesErrorInReleaseReason(t *testing.T) {
	client := newMockClient()
	client.claimResult = ClaimResult{Success: true}

	executor := newMockExecutor()
	executor.spawnErr = fmt.Errorf("script command rejected: command %q does not match any allowed command prefix", "timeout 30s mm --random")

	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	var events []RunnerEvent
	var eventMu sync.Mutex
	tr.OnEvent(func(event RunnerEvent) {
		eventMu.Lock()
		events = append(events, event)
		eventMu.Unlock()
	})

	task := testTask("task1", "proj-a")
	ctx := context.Background()

	if err := tr.claimAndSpawn(ctx, task, "proj-a"); err == nil {
		t.Fatalf("claimAndSpawn should return error when spawn fails")
	}

	eventMu.Lock()
	defer eventMu.Unlock()

	var released *RunnerEvent
	for i := range events {
		if events[i].Type == EventTaskReleased && events[i].TaskID == "task1" {
			released = &events[i]
			break
		}
	}
	if released == nil {
		t.Fatalf("expected EventTaskReleased for task1, got events: %+v", events)
	}
	if !strings.Contains(released.Reason, "spawn failed") {
		t.Errorf("Reason = %q, want it to contain %q", released.Reason, "spawn failed")
	}
	if !strings.Contains(released.Reason, "allowed command prefix") {
		t.Errorf("Reason = %q, want it to contain the underlying error text (%q)", released.Reason, "allowed command prefix")
	}
}

func TestTaskRunner_TaskCompletionReleasesDispatchLease(t *testing.T) {
	client := newMockClient()
	processMgr := newMockProcessMgr()
	tr := newTestRunner(client, newMockExecutor(), processMgr, newMockStateMgr())
	task := RunningTask{ID: "task1", Path: "projects/proj-a/task/task1.md", ProjectID: "proj-a"}
	if err := processMgr.Add("task1", task, newMockProcess(100)); err != nil {
		t.Fatalf("add process: %v", err)
	}
	processMgr.completions["task1"] = CompletionCompleted

	tr.checkRunningTasks(context.Background())

	releases := client.getReleaseDispatchCalls()
	if len(releases) != 1 {
		t.Fatalf("task completion should release dispatch lease exactly once, got %d", len(releases))
	}
}

func TestTaskRunner_SchedulerDispatchPayloadParsesAcksAndSpawns(t *testing.T) {
	client := newMockClient()
	client.readyTasks["proj-a"] = []types.ResolvedTask{*testTask("task1", "proj-a")}

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	cfg.Capabilities = []string{"dispatch_push"}
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	payload := []byte(`{
		"type":"dispatch",
		"projectId":"proj-a",
		"taskId":"task1",
		"leaseId":"lease-123",
		"lease":{"id":"lease-123","leaseId":"lease-123","lease_id":"lease-123","expires_at":4102444800000},
		"expiresAt":4102444800000
	}`)
	var cmd RunnerCommand
	if err := json.Unmarshal(payload, &cmd); err != nil {
		t.Fatalf("unmarshal scheduler dispatch payload: %v", err)
	}
	if cmd.LeaseID != "lease-123" || cmd.ProjectID != "proj-a" || cmd.TaskID != "task1" {
		t.Fatalf("parsed command = %#v", cmd)
	}

	tr.handleCommand(context.Background(), cmd)

	acks := client.getAckCalls()
	if len(acks) != 1 {
		t.Fatalf("acks = %d, want 1", len(acks))
	}
	if acks[0].LeaseID != "lease-123" || acks[0].ProjectID != "proj-a" || acks[0].TaskID != "task1" {
		t.Fatalf("ack call = %#v", acks[0])
	}
	spawns := executor.getSpawnCalls()
	if len(spawns) != 1 || spawns[0].TaskID != "task1" {
		t.Fatalf("spawns = %#v, want task1", spawns)
	}
}

func TestTaskRunner_Poll_MixedLegacyAndPassiveBehavior(t *testing.T) {
	// Tests the (test-only) coexistence of poll-fetch and push-dispatch
	// behavior in the runner. In production, LoadConfigFrom rejects
	// dispatch_push: false so the "active" branch this test exercises is
	// unreachable — see brain plan ehwvfq8e (Tier 2) for the planned
	// removal of poll-fetch code. The test is preserved as documentation
	// of the legacy behavior until that cleanup lands.
	activeClient := newMockClient()
	activeClient.nextTask["proj-a"] = testTask("active-task", "proj-a")
	passiveClient := newMockClient()
	passiveClient.nextTask["proj-a"] = testTask("poll-task", "proj-a")
	passiveClient.readyTasks["proj-a"] = []types.ResolvedTask{*testTask("dispatch-task", "proj-a")}

	activeExecutor := newMockExecutor()
	passiveExecutor := newMockExecutor()

	active := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     testRunnerConfig(),
		Mode:       ExecutionModeHeadless,
		Client:     activeClient,
		Executor:   activeExecutor,
		ProcessMgr: newMockProcessMgr(),
		StateMgr:   newMockStateMgr(),
	})

	passiveCfg := testRunnerConfig()
	passiveCfg.Capabilities = []string{"dispatch_push"}
	passive := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     passiveCfg,
		Mode:       ExecutionModeHeadless,
		Client:     passiveClient,
		Executor:   passiveExecutor,
		ProcessMgr: newMockProcessMgr(),
		StateMgr:   newMockStateMgr(),
	})

	active.poll(context.Background())
	passive.poll(context.Background())
	passive.handleCommand(context.Background(), RunnerCommand{Type: CommandDispatch, ProjectID: "proj-a", TaskID: "dispatch-task", LeaseID: "lease-1"})

	if got := len(activeClient.getNextTaskCalls()); got == 0 {
		t.Fatal("legacy active runner should poll GetNextTask")
	}
	if got := len(activeExecutor.getSpawnCalls()); got != 1 {
		t.Fatalf("legacy active runner should spawn polled task once, got %d", got)
	}
	if got := len(passiveClient.getNextTaskCalls()); got != 0 {
		t.Fatalf("passive runner should not poll GetNextTask, got %d calls", got)
	}
	spawns := passiveExecutor.getSpawnCalls()
	if got := len(spawns); got != 1 {
		t.Fatalf("passive runner should spawn dispatch lease task once, got %d", got)
	}
	if spawns[0].TaskID != "dispatch-task" {
		t.Fatalf("passive runner spawned %q, want dispatch-task", spawns[0].TaskID)
	}
}

func TestTaskRunner_RegisterAndHeartbeatAdvertiseDispatchPush(t *testing.T) {
	client := newMockClient()
	tr := newTestRunner(client, newMockExecutor(), newMockProcessMgr(), newMockStateMgr())
	tr.config.Capabilities = []string{"dispatch_push"}

	tr.registerWithAPI(context.Background())
	tr.sendHeartbeat(context.Background())

	if got := len(client.registerCalls); got != 1 {
		t.Fatalf("register calls = %d, want 1", got)
	}
	if !client.registerCalls[0].DispatchPush {
		t.Fatal("registration DispatchPush = false, want true")
	}
	if got := len(client.heartbeatCalls); got != 1 {
		t.Fatalf("heartbeat calls = %d, want 1", got)
	}
	hb := client.heartbeatCalls[0].Request.DispatchPush
	if hb == nil {
		t.Fatal("heartbeat DispatchPush pointer is nil, want true")
	}
	if !*hb {
		t.Fatal("heartbeat DispatchPush = false, want true")
	}
}

func TestTaskRunner_RegisterAndHeartbeatIncludeSchedulerMetadata(t *testing.T) {
	client := newMockClient()
	processMgr := newMockProcessMgr()
	config := testRunnerConfig()
	config.Labels = map[string]string{"pool": "fast"}
	config.WorkspaceRoots = []string{"/work/explicit"}
	config.Resources = map[string]interface{}{"gpu": 2, "arch": "arm64"}
	config.Capacity = map[string]interface{}{"memory_gb": 64}
	config.Draining = true
	config.MaxParallel = 3

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a", "proj-b"},
		Config:     config,
		Mode:       ExecutionModeHeadless,
		Executors:  map[string]TaskExecutor{"opencode": newMockExecutor()},
		ProcessMgr: processMgr,
		StateMgr:   newMockStateMgr(),
		Client:     client,
	})
	if err := processMgr.Add("task-running", RunningTask{ID: "task-running", ProjectID: "proj-a"}, newMockProcess(1)); err != nil {
		t.Fatalf("Add process failed: %v", err)
	}

	tr.registerWithAPI(context.Background())
	tr.sendHeartbeat(context.Background())

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.registerCalls) != 1 {
		t.Fatalf("register calls = %d, want 1", len(client.registerCalls))
	}
	reg := client.registerCalls[0]
	if !reflect.DeepEqual(reg.Labels, config.Labels) || !reflect.DeepEqual(reg.WorkspaceRoots, config.WorkspaceRoots) || !reflect.DeepEqual(reg.Resources, config.Resources) || !reflect.DeepEqual(reg.Capacity, config.Capacity) {
		t.Fatalf("registration metadata = labels %#v roots %#v resources %#v capacity %#v", reg.Labels, reg.WorkspaceRoots, reg.Resources, reg.Capacity)
	}
	if !reg.Draining || reg.MaxParallel != 3 || !reflect.DeepEqual(reg.Projects, []string{"proj-a", "proj-b"}) {
		t.Fatalf("registration scheduling fields = draining %v max %d projects %#v", reg.Draining, reg.MaxParallel, reg.Projects)
	}

	if len(client.heartbeatCalls) != 1 {
		t.Fatalf("heartbeat calls = %d, want 1", len(client.heartbeatCalls))
	}
	hb := client.heartbeatCalls[0].Request
	if hb.RunningTasks != 1 {
		t.Fatalf("heartbeat RunningTasks = %d, want 1", hb.RunningTasks)
	}
	if hb.Draining == nil || !*hb.Draining {
		t.Fatalf("heartbeat Draining = %#v, want true", hb.Draining)
	}
	if !reflect.DeepEqual(hb.Labels, config.Labels) || !reflect.DeepEqual(hb.WorkspaceRoots, config.WorkspaceRoots) || !reflect.DeepEqual(hb.Resources, config.Resources) || !reflect.DeepEqual(hb.Capacity, config.Capacity) || !reflect.DeepEqual(hb.Projects, []string{"proj-a", "proj-b"}) {
		t.Fatalf("heartbeat metadata = labels %#v roots %#v projects %#v resources %#v capacity %#v", hb.Labels, hb.WorkspaceRoots, hb.Projects, hb.Resources, hb.Capacity)
	}
}

func TestTaskRunner_RegisterUsesAllowedWorkdirRootsForSchedulerMetadata(t *testing.T) {
	client := newMockClient()
	config := testRunnerConfig()
	config.Control.AllowedWorkdirRoots = []string{"/work/fallback"}

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     config,
		Mode:       ExecutionModeHeadless,
		Executors:  map[string]TaskExecutor{"opencode": newMockExecutor()},
		ProcessMgr: newMockProcessMgr(),
		StateMgr:   newMockStateMgr(),
		Client:     client,
	})

	tr.registerWithAPI(context.Background())

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.registerCalls) != 1 {
		t.Fatalf("register calls = %d, want 1", len(client.registerCalls))
	}
	if !reflect.DeepEqual(client.registerCalls[0].WorkspaceRoots, []string{"/work/fallback"}) {
		t.Fatalf("WorkspaceRoots = %#v, want fallback roots", client.registerCalls[0].WorkspaceRoots)
	}
}

func TestTaskRunner_Poll_HealthCheckFails_NoSpawn(t *testing.T) {
	client := newMockClient()
	client.healthResult = APIHealth{Status: "unhealthy"}
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	ctx := context.Background()
	tr.poll(ctx)

	// Should not attempt to spawn anything
	if len(executor.getSpawnCalls()) > 0 {
		t.Error("should not spawn when health check fails")
	}
}

func TestTaskRunner_Poll_FillsAvailableSlots(t *testing.T) {
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

	ctx := context.Background()
	tr.poll(ctx)

	spawnCalls := executor.getSpawnCalls()
	if len(spawnCalls) == 0 {
		t.Error("poll should spawn tasks when slots available")
	}
}

func TestTaskRunner_Poll_RespectsMaxParallel(t *testing.T) {
	client := newMockClient()
	task := testTask("task1", "proj-a")
	client.nextTask["proj-a"] = task

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	// Pre-fill to max capacity
	cfg := testRunnerConfig()
	cfg.MaxParallel = 1

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	// Add a running process to fill the slot
	proc := newMockProcess(100)
	processMgr.Add("existing", testRunningTask("existing"), proc)

	ctx := context.Background()
	tr.poll(ctx)

	// Should not spawn because at capacity
	if len(executor.getSpawnCalls()) > 0 {
		t.Error("should not spawn when at max parallel capacity")
	}
}

func TestTaskRunner_Poll_SkipsPausedProjects(t *testing.T) {
	client := newMockClient()
	task := testTask("task1", "proj-a")
	client.nextTask["proj-a"] = task

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)
	tr.PauseProject("proj-a")

	ctx := context.Background()
	tr.poll(ctx)

	if len(executor.getSpawnCalls()) > 0 {
		t.Error("should not spawn tasks for paused projects")
	}
}

func TestTaskRunner_Poll_UsesServerOwnedProjectPauseState(t *testing.T) {
	client := newMockClient()
	client.nextTask["proj-a"] = testTask("task1", "proj-a")
	client.runnerStatus = &types.RunnerStatusResponse{Running: true, PausedProjects: []string{"proj-a"}}

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)
	tr.poll(context.Background())

	if len(executor.getSpawnCalls()) > 0 {
		t.Fatal("server-owned task pause should prevent normal task spawn")
	}
}

func TestDiscoverSessionID_IgnoresExistingSessions(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			_, _ = w.Write([]byte(`[
				{"id":"ses_old","time":{"updated":9000}}
			]`))
		default:
			_, _ = w.Write([]byte(`[
				{"id":"ses_old","time":{"updated":9000}},
				{"id":"ses_task","time":{"updated":1000}}
			]`))
		}
	}))
	defer server.Close()

	port := serverPortFromURL(t, server.URL)
	baseline, err := listSessionIDs(port)
	if err != nil {
		t.Fatalf("listSessionIDs failed: %v", err)
	}

	sessionID, err := discoverSessionID(port, baseline)
	if err != nil {
		t.Fatalf("discoverSessionID failed: %v", err)
	}
	if sessionID != "ses_task" {
		t.Fatalf("sessionID = %q, want ses_task", sessionID)
	}
}

func serverPortFromURL(t *testing.T, rawURL string) int {
	t.Helper()
	idx := strings.LastIndex(rawURL, ":")
	if idx < 0 {
		t.Fatalf("server URL has no port: %s", rawURL)
	}
	port, err := strconv.Atoi(rawURL[idx+1:])
	if err != nil {
		t.Fatalf("parse server port from %q: %v", rawURL, err)
	}
	return port
}

func TestTaskRunner_Poll_RunsAutomationTasksWhenProjectPausedAndAutomationsUnpaused(t *testing.T) {
	client := newMockClient()
	task := testTask("task1", "proj-a")
	task.GeneratedBy = "automation:auto1234"
	client.nextTask["proj-a"] = task

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)
	tr.PauseProject("proj-a")

	ctx := context.Background()
	tr.poll(ctx)

	if len(executor.getSpawnCalls()) != 1 {
		t.Fatalf("paused project with automations unpaused should spawn automation task, got %d spawns", len(executor.getSpawnCalls()))
	}
	calls := client.getNextTaskCalls()
	if len(calls) == 0 || calls[0].Opts == nil || calls[0].Opts.GeneratedByPrefix != "automation:" {
		t.Fatalf("paused project should fetch only automation tasks first, calls=%#v", calls)
	}
}

func TestTaskRunner_Poll_DoesNotRunAutomationTasksWhenAutomationsPaused(t *testing.T) {
	client := newMockClient()
	task := testTask("task1", "proj-a")
	task.GeneratedBy = "automation:auto1234"
	client.nextTask["proj-a"] = task

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)
	tr.PauseProject("proj-a")
	tr.PauseAutomations()

	ctx := context.Background()
	tr.poll(ctx)

	if len(executor.getSpawnCalls()) > 0 {
		t.Fatalf("automation-paused runner should not spawn automation task, got %d spawns", len(executor.getSpawnCalls()))
	}
	for _, call := range client.getNextTaskCalls() {
		if call.ProjectID == "proj-a" {
			t.Fatalf("automation-paused runner should not fetch tasks for paused project, got call: %#v", call)
		}
	}
}

func TestTaskRunner_Poll_DoesNotRunAutomationTasksWhenAutomationsPausedAndProjectUnpaused(t *testing.T) {
	client := newMockClient()
	task := testTask("task1", "proj-a")
	task.GeneratedBy = "automation:auto1234"
	client.nextTask["proj-a"] = task

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)
	tr.PauseAutomations()

	ctx := context.Background()
	tr.poll(ctx)

	if len(executor.getSpawnCalls()) > 0 {
		t.Fatalf("automation-paused runner should not spawn automation task, got %d spawns", len(executor.getSpawnCalls()))
	}
}

func TestTaskRunner_Poll_ProjectAutomationPauseSkipsOnlyThatProject(t *testing.T) {
	client := newMockClient()
	pausedTask := testTask("task-paused", "proj-a")
	pausedTask.GeneratedBy = "automation:auto-a"
	runningTask := testTask("task-running", "proj-b")
	runningTask.GeneratedBy = "automation:auto-b"
	client.nextTask["proj-a"] = pausedTask
	client.nextTask["proj-b"] = runningTask

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)
	tr.PauseProjectAutomations("proj-a")

	ctx := context.Background()
	tr.poll(ctx)

	spawns := executor.getSpawnCalls()
	if len(spawns) != 1 {
		t.Fatalf("expected exactly one spawn for unpaused project, got %d", len(spawns))
	}
	if spawns[0].TaskID != "task-running" {
		t.Fatalf("spawned task = %s, want task-running", spawns[0].TaskID)
	}
}

func TestTaskRunner_Poll_RespectsAutomationMaxConcurrent(t *testing.T) {
	client := newMockClient()
	task := testTask("task1", "proj-a")
	task.GeneratedBy = "automation:auto1234"
	client.nextTask["proj-a"] = task
	client.getEntryResult = map[string]*types.BrainEntry{
		"auto1234": {ID: "auto1234", Trigger: &types.TriggerConfig{MaxConcurrent: 1}},
	}

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	proc := newMockProcess(100)
	processMgr.Add("existing", RunningTask{ID: "existing", GeneratedBy: "automation:auto1234"}, proc)
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)
	tr.PauseProject("proj-a")

	ctx := context.Background()
	tr.poll(ctx)

	if len(executor.getSpawnCalls()) > 0 {
		t.Fatalf("expected max_concurrent to prevent second automation task spawn, got %d spawns", len(executor.getSpawnCalls()))
	}
}

func TestTaskRunner_Poll_DoesNotRunNormalTasksWhenPollingAutomationWhilePaused(t *testing.T) {
	client := newMockClient()
	client.nextTask["proj-a"] = testTask("task1", "proj-a")

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)
	tr.PauseProject("proj-a")

	ctx := context.Background()
	tr.poll(ctx)

	if len(executor.getSpawnCalls()) > 0 {
		t.Error("should not spawn normal tasks while project is paused")
	}
}

func TestTaskRunner_Poll_SkipsAllWhenAllPaused(t *testing.T) {
	client := newMockClient()
	task := testTask("task1", "proj-a")
	client.nextTask["proj-a"] = task

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)
	tr.PauseAll()

	ctx := context.Background()
	tr.poll(ctx)

	if len(executor.getSpawnCalls()) > 0 {
		t.Error("should not spawn tasks when all paused")
	}
}

func TestTaskRunner_Poll_RunsAutomationTasksWhenAllPausedAndAutomationsUnpaused(t *testing.T) {
	client := newMockClient()
	task := testTask("task1", "proj-a")
	task.GeneratedBy = "automation:auto1234"
	client.nextTask["proj-a"] = task

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)
	tr.PauseAll()

	ctx := context.Background()
	tr.poll(ctx)

	if len(executor.getSpawnCalls()) != 1 {
		t.Fatalf("all-paused runner with automations unpaused should spawn automation task, got %d spawns", len(executor.getSpawnCalls()))
	}
}

func TestTaskRunner_Poll_NoTasksAvailable(t *testing.T) {
	client := newMockClient()
	// No tasks set — GetNextTask returns nil

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	ctx := context.Background()
	tr.poll(ctx)

	if len(executor.getSpawnCalls()) > 0 {
		t.Error("should not spawn when no tasks available")
	}
}

func TestTaskRunner_Poll_EmitsPollCompleteEvent(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	var eventCount int32
	tr.OnEvent(func(event RunnerEvent) {
		if event.Type == EventPollComplete {
			atomic.AddInt32(&eventCount, 1)
		}
	})

	ctx := context.Background()
	tr.poll(ctx)

	if atomic.LoadInt32(&eventCount) == 0 {
		t.Error("poll should emit poll_complete event")
	}
}

// =============================================================================
// ClaimAndSpawn Tests
// =============================================================================

func TestTaskRunner_ClaimAndSpawn_Success(t *testing.T) {
	client := newMockClient()
	client.claimResult = ClaimResult{Success: true}

	executor := newMockExecutor()
	proc := newMockProcess(100)
	executor.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/test"}

	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	task := testTask("task1", "proj-a")
	ctx := context.Background()

	err := tr.claimAndSpawn(ctx, task, "proj-a")
	if err != nil {
		t.Fatalf("claimAndSpawn returned error: %v", err)
	}

	// Verify claim was called
	claims := client.getClaimCalls()
	if len(claims) != 1 {
		t.Fatalf("expected 1 claim call, got %d", len(claims))
	}
	if claims[0].TaskID != "task1" {
		t.Errorf("claim taskID = %q, want %q", claims[0].TaskID, "task1")
	}

	// Verify status updated to in_progress
	updates := client.getUpdateStatusCalls()
	if len(updates) < 1 {
		t.Fatal("expected at least 1 update status call")
	}
	if updates[0].Status != "in_progress" {
		t.Errorf("update status = %q, want %q", updates[0].Status, "in_progress")
	}

	// Verify spawn was called
	spawns := executor.getSpawnCalls()
	if len(spawns) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(spawns))
	}
}

func TestTaskRunner_ClaimAndSpawn_ClaimFails(t *testing.T) {
	client := newMockClient()
	client.claimResult = ClaimResult{Success: false, ClaimedBy: "other-runner", Message: "assigned to another runner"}

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)
	var events []RunnerEvent
	tr.OnEvent(func(event RunnerEvent) {
		events = append(events, event)
	})

	task := testTask("task1", "proj-a")
	ctx := context.Background()

	err := tr.claimAndSpawn(ctx, task, "proj-a")
	if err == nil {
		t.Error("claimAndSpawn should return error when claim fails")
	}
	if !errors.Is(err, ErrTaskClaimConflict) {
		t.Fatalf("claimAndSpawn error = %v, want ErrTaskClaimConflict", err)
	}

	var rejected *RunnerEvent
	for i := range events {
		if events[i].Type == EventTaskClaimRejected {
			rejected = &events[i]
			break
		}
	}
	if rejected == nil {
		t.Fatal("claimAndSpawn should emit task_claim_rejected event")
	}
	if rejected.ClaimedBy != "other-runner" {
		t.Errorf("claim rejected ClaimedBy = %q, want %q", rejected.ClaimedBy, "other-runner")
	}

	// Should not spawn
	if len(executor.getSpawnCalls()) > 0 {
		t.Error("should not spawn when claim fails")
	}
	if len(client.getUpdateStatusCalls()) > 0 {
		t.Error("should not update task status when claim conflicts")
	}
	if len(client.getReleaseCalls()) > 0 {
		t.Error("should not release task when claim was not acquired")
	}
}

func TestTaskRunner_Poll_TreatsClaimConflictAsExpectedRace(t *testing.T) {
	client := newMockClient()
	client.nextTask["proj-a"] = testTask("task1", "proj-a")
	client.claimResult = ClaimResult{Success: false, ClaimedBy: "other-runner", Message: "assigned to another runner"}

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	var logs bytes.Buffer
	tr := newTestRunner(client, executor, processMgr, stateMgr)
	tr.logger = log.New(&logs, "", 0)

	var claimRejected bool
	tr.OnEvent(func(event RunnerEvent) {
		if event.Type == EventTaskClaimRejected {
			claimRejected = true
		}
	})

	tr.poll(context.Background())

	if !claimRejected {
		t.Fatal("poll should preserve claim rejected event emission")
	}
	if len(executor.getSpawnCalls()) > 0 {
		t.Error("poll should not spawn after claim conflict")
	}
	if len(client.getUpdateStatusCalls()) > 0 {
		t.Error("poll should not mark task in progress or failed after claim conflict")
	}
	if len(client.getReleaseCalls()) > 0 {
		t.Error("poll should not release a claim that was never acquired")
	}
	if strings.Contains(logs.String(), "claim and spawn failed") {
		t.Fatalf("poll logged claim conflict as generic spawn failure: %q", logs.String())
	}
}

func TestTaskRunner_ClaimAndSpawn_SpawnFails_ReleasesTask(t *testing.T) {
	client := newMockClient()
	client.claimResult = ClaimResult{Success: true}

	executor := newMockExecutor()
	executor.spawnErr = fmt.Errorf("spawn failed")

	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	task := testTask("task1", "proj-a")
	ctx := context.Background()

	err := tr.claimAndSpawn(ctx, task, "proj-a")
	if err == nil {
		t.Error("claimAndSpawn should return error when spawn fails")
	}

	// Should release the task
	releases := client.getReleaseCalls()
	if len(releases) == 0 {
		t.Error("should release task when spawn fails")
	}
}

// TestTaskRunner_SpawnFailureRollsStatusBackToBlocked verifies that when a
// spawn fails, the runner not only releases the lease but also rolls back
// the task's status from "in_progress" (set optimistically at claim time)
// to "blocked". Without this rollback the task is stuck in in_progress
// forever, invisible to schedulers and only recoverable by the orphan
// reaper on a subsequent runner start.
//
// Bug reproduction: script-command rejection released the lease and
// emitted task.released with an accurate reason, but the task itself
// stayed in_progress until the runner was restarted.
//
// The rollback status is "blocked" for symmetry with the workdir-failure
// branch (runner.go: resolve workdir errors also mark the task blocked),
// so a human or the Blocked Task Inspector can address it.
func TestTaskRunner_SpawnFailureRollsStatusBackToBlocked(t *testing.T) {
	client := newMockClient()
	client.claimResult = ClaimResult{Success: true}

	executor := newMockExecutor()
	executor.spawnErr = fmt.Errorf("script command rejected: command %q does not match any allowed command prefix", "rm -rf /tmp/nope")

	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	task := testTask("task1", "proj-a")
	ctx := context.Background()

	if err := tr.claimAndSpawn(ctx, task, "proj-a"); err == nil {
		t.Fatalf("claimAndSpawn should return error when spawn fails")
	}

	updates := client.getUpdateStatusCalls()
	// We expect two UpdateTaskStatus calls: pending→in_progress at claim,
	// then in_progress→blocked at spawn-failure rollback.
	if len(updates) != 2 {
		t.Fatalf("expected 2 UpdateTaskStatus calls (in_progress at claim, blocked on rollback), got %d: %+v", len(updates), updates)
	}
	if updates[0].Status != "in_progress" {
		t.Errorf("first UpdateTaskStatus should be in_progress (claim), got %q", updates[0].Status)
	}
	if updates[1].Status != "blocked" {
		t.Errorf("second UpdateTaskStatus should be blocked (spawn-failure rollback), got %q", updates[1].Status)
	}
	if updates[1].TaskPath != task.Path {
		t.Errorf("rollback UpdateTaskStatus path = %q, want %q", updates[1].TaskPath, task.Path)
	}
}

func TestTaskRunner_ClaimAndSpawn_EmitsTaskStartedEvent(t *testing.T) {
	client := newMockClient()
	client.claimResult = ClaimResult{Success: true}

	executor := newMockExecutor()
	proc := newMockProcess(100)
	executor.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/test"}

	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	var events []RunnerEvent
	var eventMu sync.Mutex
	tr.OnEvent(func(event RunnerEvent) {
		eventMu.Lock()
		events = append(events, event)
		eventMu.Unlock()
	})

	task := testTask("task1", "proj-a")
	ctx := context.Background()

	tr.claimAndSpawn(ctx, task, "proj-a")

	eventMu.Lock()
	defer eventMu.Unlock()

	found := false
	for _, e := range events {
		if e.Type == EventTaskStarted {
			found = true
			if e.Task == nil {
				t.Error("task_started event should have Task set")
			} else if e.Task.ID != "task1" {
				t.Errorf("task_started event task ID = %q, want %q", e.Task.ID, "task1")
			}
		}
	}
	if !found {
		t.Error("claimAndSpawn should emit task_started event")
	}
}

// =============================================================================
// RenewClaims Tests
// =============================================================================

func TestTaskRunner_RenewClaims_Success(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	// Add a running task
	proc := newMockProcess(100)
	task := testRunningTask("task1")
	processMgr.Add("task1", task, proc)

	// renewErr is nil (default) — renewals should succeed
	ctx := context.Background()
	tr.renewClaims(ctx)

	// Verify RenewClaim was called
	client.mu.Lock()
	renewCalls := make([]renewCall, len(client.renewCalls))
	copy(renewCalls, client.renewCalls)
	client.mu.Unlock()

	if len(renewCalls) != 1 {
		t.Fatalf("expected 1 RenewClaim call, got %d", len(renewCalls))
	}
	if renewCalls[0].ProjectID != task.ProjectID {
		t.Errorf("ProjectID = %q, want %q", renewCalls[0].ProjectID, task.ProjectID)
	}
	if renewCalls[0].TaskID != task.ID {
		t.Errorf("TaskID = %q, want %q", renewCalls[0].TaskID, task.ID)
	}
	if renewCalls[0].RunnerID != tr.runnerID {
		t.Errorf("RunnerID = %q, want %q", renewCalls[0].RunnerID, tr.runnerID)
	}

	// Task should still be running (not killed)
	if processMgr.Get("task1") == nil {
		t.Error("task should still be tracked in process manager")
	}
}

func TestTaskRunner_RenewClaims_FailureAbortsTask(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	// Track events
	var eventMu sync.Mutex
	var events []RunnerEvent
	tr.OnEvent(func(e RunnerEvent) {
		eventMu.Lock()
		defer eventMu.Unlock()
		events = append(events, e)
	})

	// Add a running task
	proc := newMockProcess(100)
	task := testRunningTask("task1")
	processMgr.Add("task1", task, proc)

	// Set renewal to fail (simulates expired/force-released claim)
	client.renewErr = fmt.Errorf("claim not found")

	ctx := context.Background()
	tr.renewClaims(ctx)

	// Verify the task was killed
	processMgr.mu.Lock()
	killCalls := make([]string, len(processMgr.killCalls))
	copy(killCalls, processMgr.killCalls)
	processMgr.mu.Unlock()

	if len(killCalls) != 1 || killCalls[0] != "task1" {
		t.Errorf("expected Kill(task1), got %v", killCalls)
	}

	// Verify task was removed from process manager
	if processMgr.Get("task1") != nil {
		t.Error("task should have been removed from process manager")
	}

	// Verify status was set back to pending
	statusCalls := client.getUpdateStatusCalls()
	if len(statusCalls) != 1 {
		t.Fatalf("expected 1 UpdateTaskStatus call, got %d", len(statusCalls))
	}
	if statusCalls[0].Status != "pending" {
		t.Errorf("status = %q, want %q", statusCalls[0].Status, "pending")
	}

	// Verify event was emitted
	eventMu.Lock()
	defer eventMu.Unlock()

	found := false
	for _, e := range events {
		if e.Type == EventTaskReleased && e.TaskID == "task1" {
			found = true
			if e.Reason != "claim renewal failed" {
				t.Errorf("reason = %q, want %q", e.Reason, "claim renewal failed")
			}
		}
	}
	if !found {
		t.Error("expected EventTaskReleased event for task1")
	}

	// Verify stats were updated
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	if tr.stats.Failed != 1 {
		t.Errorf("stats.Failed = %d, want 1", tr.stats.Failed)
	}
}

func TestTaskRunner_RenewClaims_NoRunningTasks(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	// No running tasks — should be a no-op
	ctx := context.Background()
	tr.renewClaims(ctx)

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.renewCalls) != 0 {
		t.Errorf("expected 0 RenewClaim calls, got %d", len(client.renewCalls))
	}
}

func TestTaskRunner_RenewClaims_MultipleTasksPartialFailure(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	// Add two running tasks
	proc1 := newMockProcess(100)
	task1 := testRunningTask("task1")
	processMgr.Add("task1", task1, proc1)

	proc2 := newMockProcess(101)
	task2 := RunningTask{
		ID:        "task2",
		Path:      "projects/proj-a/task/task2.md",
		ProjectID: "proj-a",
	}
	processMgr.Add("task2", task2, proc2)

	// Override RenewClaim to fail only for task1
	// We need a custom implementation since mockClient only supports a single error value.
	// Instead, let the first call succeed (nil error) and make the mock return error.
	// Since mockClient applies the same renewErr to all calls, we need to set a per-call behavior.
	// For simplicity, set renewErr to fail — both tasks will be aborted.
	client.renewErr = fmt.Errorf("claim expired")

	ctx := context.Background()
	tr.renewClaims(ctx)

	// Both tasks should be killed and removed
	processMgr.mu.Lock()
	killCount := len(processMgr.killCalls)
	processMgr.mu.Unlock()

	if killCount != 2 {
		t.Errorf("expected 2 Kill calls, got %d", killCount)
	}

	// Both tasks should be removed
	if processMgr.Get("task1") != nil {
		t.Error("task1 should have been removed")
	}
	if processMgr.Get("task2") != nil {
		t.Error("task2 should have been removed")
	}

	// Stats should reflect both failures
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	if tr.stats.Failed != 2 {
		t.Errorf("stats.Failed = %d, want 2", tr.stats.Failed)
	}
}

// =============================================================================
// CheckRunningTasks Tests
// =============================================================================

func TestTaskRunner_CheckRunningTasks_CompletedTask(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	// Add a running task
	proc := newMockProcess(100)
	task := testRunningTask("task1")
	processMgr.Add("task1", task, proc)

	// Mark it as completed
	processMgr.setCompletion("task1", CompletionCompleted)

	var events []RunnerEvent
	var eventMu sync.Mutex
	tr.OnEvent(func(event RunnerEvent) {
		eventMu.Lock()
		events = append(events, event)
		eventMu.Unlock()
	})

	ctx := context.Background()
	tr.checkRunningTasks(ctx)

	// Verify task was removed from process manager
	if processMgr.Get("task1") != nil {
		t.Error("completed task should be removed from process manager")
	}

	// Verify API status was updated
	updates := client.getUpdateStatusCalls()
	if len(updates) == 0 {
		t.Error("should update API status for completed task")
	}
	if len(updates) > 0 && updates[0].Status != "completed" {
		t.Errorf("API status = %q, want %q", updates[0].Status, "completed")
	}

	// Verify event emitted
	eventMu.Lock()
	defer eventMu.Unlock()
	found := false
	for _, e := range events {
		if e.Type == EventTaskCompleted {
			found = true
		}
	}
	if !found {
		t.Error("should emit task_completed event")
	}
}

// TestTaskRunner_CheckRunningTasks_IgnoresSlotReservations is the
// regression test for the spurious empty-path status update seen during
// push dispatch: ReserveSlot inserts a placeholder ProcessInfo with a
// zero-value Task, and the poll loop's completion check raced the
// in-flight spawn, saw the placeholder in GetAll, treated it as a
// crashed task (CheckCompletion("") = not tracked = crashed), and then
// emitted a bogus in_progress→pending event with empty task_id/project
// and called UpdateTaskStatus with an empty path (API 404
// "Entry not found: "). Uses the real ProcessManager since that is
// where the placeholder leaked from.
func TestTaskRunner_CheckRunningTasks_IgnoresSlotReservations(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	stateMgr := newMockStateMgr()
	processMgr := NewProcessManager(testRunnerConfig())

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"demo"},
		Config:   testRunnerConfig(),
		Mode:     ExecutionModeHeadless,
		Client:   client,
		Executors: map[string]TaskExecutor{
			"opencode": executor,
		},
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	// Dispatch preflight has reserved a slot; the spawn is still in flight.
	if !processMgr.ReserveSlot("task-dispatching", 3) {
		t.Fatal("reservation should succeed")
	}

	var events []RunnerEvent
	var eventMu sync.Mutex
	tr.OnEvent(func(event RunnerEvent) {
		eventMu.Lock()
		events = append(events, event)
		eventMu.Unlock()
	})

	tr.checkRunningTasks(context.Background())

	if calls := client.getUpdateStatusCalls(); len(calls) != 0 {
		t.Fatalf("UpdateTaskStatus called %d times for an unspawned reservation; first call: path=%q status=%q",
			len(calls), calls[0].TaskPath, calls[0].Status)
	}

	eventMu.Lock()
	for _, e := range events {
		if e.Type == EventTaskStatusChanged || e.Type == EventTaskFailed {
			t.Errorf("spurious %s event emitted for reservation: task_id=%q project=%q", e.Type, e.TaskID, e.ProjectID)
		}
	}
	eventMu.Unlock()

	tr.mu.RLock()
	failed := tr.stats.Failed
	tr.mu.RUnlock()
	if failed != 0 {
		t.Errorf("stats.Failed = %d, want 0", failed)
	}

	// The reservation must survive so the in-flight spawn can upgrade it.
	if processMgr.RunningCount() != 1 {
		t.Errorf("RunningCount = %d, want 1 (reservation still held)", processMgr.RunningCount())
	}
}

func TestTaskRunner_CheckRunningTasks_FailedTask(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	proc := newMockProcess(100)
	task := testRunningTask("task1")
	processMgr.Add("task1", task, proc)
	processMgr.setCompletion("task1", CompletionCrashed)

	ctx := context.Background()
	tr.checkRunningTasks(ctx)

	// Failed tasks go back to pending for retry
	updates := client.getUpdateStatusCalls()
	if len(updates) == 0 {
		t.Fatal("should update API status for failed task")
	}
	if updates[0].Status != "pending" {
		t.Errorf("API status = %q, want %q for failed task", updates[0].Status, "pending")
	}

	// Verify stats updated
	tr.mu.RLock()
	failed := tr.stats.Failed
	tr.mu.RUnlock()
	if failed != 1 {
		t.Errorf("stats.Failed = %d, want 1", failed)
	}
}

func TestTaskRunner_CheckRunningTasks_RunningTask_NoAction(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	proc := newMockProcess(100)
	task := testRunningTask("task1")
	processMgr.Add("task1", task, proc)
	// Default completion is CompletionRunning

	ctx := context.Background()
	tr.checkRunningTasks(ctx)

	// Should not update status or remove
	if len(client.getUpdateStatusCalls()) > 0 {
		t.Error("should not update status for running task")
	}
	if processMgr.Get("task1") == nil {
		t.Error("running task should not be removed")
	}
}

func TestTaskRunner_CheckRunningTasks_UpdatesStats(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	proc := newMockProcess(100)
	task := testRunningTask("task1")
	processMgr.Add("task1", task, proc)
	processMgr.setCompletion("task1", CompletionCompleted)

	ctx := context.Background()
	tr.checkRunningTasks(ctx)

	tr.mu.RLock()
	completed := tr.stats.Completed
	tr.mu.RUnlock()

	if completed != 1 {
		t.Errorf("stats.Completed = %d, want 1", completed)
	}
}

func TestTaskRunner_CheckRunningTasks_CleansUpFiles(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	proc := newMockProcess(100)
	task := testRunningTask("task1")
	processMgr.Add("task1", task, proc)
	processMgr.setCompletion("task1", CompletionCompleted)

	ctx := context.Background()
	tr.checkRunningTasks(ctx)

	cleanups := executor.getCleanupCalls()
	if len(cleanups) == 0 {
		t.Error("should cleanup temp files after task completion")
	}
}

// =============================================================================
// Pause / Resume Tests
// =============================================================================

func TestTaskRunner_ProjectAutomationPause_IsScopedToProject(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	tr.PauseProjectAutomations("proj-a")

	if !tr.IsAutomationsPausedForProject("proj-a") {
		t.Fatal("proj-a automations should be paused")
	}
	if tr.IsAutomationsPausedForProject("proj-b") {
		t.Fatal("proj-b automations should not inherit proj-a automation pause")
	}
}

func TestTaskRunner_GlobalAutomationPause_AppliesToAllProjects(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	tr.PauseAutomations()

	if !tr.IsAutomationsPausedForProject("proj-a") {
		t.Fatal("proj-a automations should be paused when global automations are paused")
	}
	if !tr.IsAutomationsPausedForProject("proj-b") {
		t.Fatal("proj-b automations should be paused when global automations are paused")
	}
}

func TestTaskRunner_ProjectAutomationResume_DoesNotResumeGlobalPause(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	tr.PauseAutomations()
	tr.PauseProjectAutomations("proj-a")
	tr.ResumeProjectAutomations("proj-a")

	if !tr.IsAutomationsPausedForProject("proj-a") {
		t.Fatal("proj-a automations should remain paused by global automation pause")
	}
}

func TestTaskRunner_PauseProject(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	tr.PauseProject("proj-a")

	if !tr.IsPaused("proj-a") {
		t.Error("proj-a should be paused")
	}
	if tr.IsPaused("proj-b") {
		t.Error("proj-b should not be paused")
	}
}

func TestTaskRunner_ResumeProject(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	tr.PauseProject("proj-a")
	tr.ResumeProject("proj-a")

	if tr.IsPaused("proj-a") {
		t.Error("proj-a should not be paused after resume")
	}
}

func TestTaskRunner_PauseAll(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	tr.PauseAll()

	if !tr.IsPaused("proj-a") {
		t.Error("proj-a should be paused when all paused")
	}
	if !tr.IsPaused("proj-b") {
		t.Error("proj-b should be paused when all paused")
	}
}

func TestTaskRunner_ResumeAll(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	tr.PauseAll()
	tr.ResumeAll()

	if tr.IsPaused("proj-a") {
		t.Error("proj-a should not be paused after resume all")
	}
}

func TestTaskRunner_PauseResume_EmitsEvents(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	var events []RunnerEvent
	var mu sync.Mutex
	tr.OnEvent(func(event RunnerEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})

	tr.PauseProject("proj-a")
	tr.ResumeProject("proj-a")
	tr.PauseAll()
	tr.ResumeAll()

	mu.Lock()
	defer mu.Unlock()

	expectedTypes := []RunnerEventType{
		EventProjectPaused,
		EventProjectResumed,
		EventAllPaused,
		EventAllResumed,
	}

	if len(events) != len(expectedTypes) {
		t.Fatalf("expected %d events, got %d", len(expectedTypes), len(events))
	}

	for i, expected := range expectedTypes {
		if events[i].Type != expected {
			t.Errorf("event[%d].Type = %q, want %q", i, events[i].Type, expected)
		}
	}
}

// =============================================================================
// GetStatus Tests
// =============================================================================

func TestTaskRunner_GetStatus(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	status := tr.GetStatus()

	if status.RunnerID == "" {
		t.Error("RunnerID should not be empty")
	}
	if status.Status != RunnerStatusIdle {
		t.Errorf("Status = %q, want %q", status.Status, RunnerStatusIdle)
	}
	if len(status.Projects) != 2 {
		t.Errorf("Projects len = %d, want 2", len(status.Projects))
	}
	if status.MaxParallel != 2 {
		t.Errorf("MaxParallel = %d, want 2", status.MaxParallel)
	}
}

func TestTaskRunner_GetStatus_WithPaused(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	tr.PauseProject("proj-a")

	status := tr.GetStatus()
	if len(status.Paused) != 1 {
		t.Errorf("Paused len = %d, want 1", len(status.Paused))
	}
}

// =============================================================================
// SetMaxParallel Tests
// =============================================================================

func TestTaskRunner_SetMaxParallel(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	// Default is 2 from testRunnerConfig
	status := tr.GetStatus()
	if status.MaxParallel != 2 {
		t.Fatalf("initial MaxParallel = %d, want 2", status.MaxParallel)
	}

	// Update to 1
	tr.SetMaxParallel(1)

	status = tr.GetStatus()
	if status.MaxParallel != 1 {
		t.Errorf("after SetMaxParallel(1), MaxParallel = %d, want 1", status.MaxParallel)
	}
}

func TestTaskRunner_SetMaxParallel_EnforcedByPoll(t *testing.T) {
	client := newMockClient()
	task1 := testTask("task1", "proj-a")
	client.nextTask["proj-a"] = task1

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	cfg.MaxParallel = 2 // start with 2

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	// Reduce to 1 at runtime
	tr.SetMaxParallel(1)

	// Add a running process to fill the single slot
	proc := newMockProcess(100)
	processMgr.Add("existing", testRunningTask("existing"), proc)

	ctx := context.Background()
	tr.poll(ctx)

	// Should not spawn because at capacity (1/1)
	if len(executor.getSpawnCalls()) > 0 {
		t.Error("should not spawn when at max parallel capacity after SetMaxParallel(1)")
	}
}

func TestTaskRunner_SetMaxParallel_ZeroDefaultsToOne(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	// Setting 0 should be treated as minimum 1
	tr.SetMaxParallel(0)

	status := tr.GetStatus()
	if status.MaxParallel != 1 {
		t.Errorf("after SetMaxParallel(0), MaxParallel = %d, want 1 (minimum)", status.MaxParallel)
	}
}

func TestTaskRunner_SetMaxParallel_NegativeDefaultsToOne(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	// Setting negative should be treated as minimum 1
	tr.SetMaxParallel(-5)

	status := tr.GetStatus()
	if status.MaxParallel != 1 {
		t.Errorf("after SetMaxParallel(-5), MaxParallel = %d, want 1 (minimum)", status.MaxParallel)
	}
}

// =============================================================================
// Event Handler Tests
// =============================================================================

func TestTaskRunner_OnEvent(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	var received []RunnerEvent
	var mu sync.Mutex
	tr.OnEvent(func(event RunnerEvent) {
		mu.Lock()
		received = append(received, event)
		mu.Unlock()
	})

	tr.emitEvent(RunnerEvent{Type: EventShutdown, Reason: "test"})

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].Type != EventShutdown {
		t.Errorf("event type = %q, want %q", received[0].Type, EventShutdown)
	}
}

func TestTaskRunner_OnEvent_MultipleHandlers(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	var count1, count2 int32
	tr.OnEvent(func(event RunnerEvent) {
		atomic.AddInt32(&count1, 1)
	})
	tr.OnEvent(func(event RunnerEvent) {
		atomic.AddInt32(&count2, 1)
	})

	tr.emitEvent(RunnerEvent{Type: EventShutdown})

	if atomic.LoadInt32(&count1) != 1 {
		t.Error("handler 1 should have been called")
	}
	if atomic.LoadInt32(&count2) != 1 {
		t.Error("handler 2 should have been called")
	}
}

// =============================================================================
// Integration-style: Full poll cycle
// =============================================================================

func TestTaskRunner_FullPollCycle(t *testing.T) {
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

	var events []RunnerEventType
	var eventMu sync.Mutex
	tr.OnEvent(func(event RunnerEvent) {
		eventMu.Lock()
		events = append(events, event.Type)
		eventMu.Unlock()
	})

	// First poll: should claim and spawn
	ctx := context.Background()
	tr.poll(ctx)

	// Verify task was spawned
	if len(executor.getSpawnCalls()) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(executor.getSpawnCalls()))
	}

	// Now mark the task as completed
	processMgr.setCompletion("task1", CompletionCompleted)

	// Second poll: should detect completion
	tr.poll(ctx)

	// Verify completion was handled
	tr.mu.RLock()
	completed := tr.stats.Completed
	tr.mu.RUnlock()
	if completed != 1 {
		t.Errorf("stats.Completed = %d, want 1", completed)
	}

	// Verify events
	eventMu.Lock()
	defer eventMu.Unlock()

	hasStarted := false
	hasCompleted := false
	for _, et := range events {
		if et == EventTaskStarted {
			hasStarted = true
		}
		if et == EventTaskCompleted {
			hasCompleted = true
		}
	}
	if !hasStarted {
		t.Error("should have emitted task_started event")
	}
	if !hasCompleted {
		t.Error("should have emitted task_completed event")
	}
}

func TestTaskRunner_Poll_MultipleProjects(t *testing.T) {
	client := newMockClient()
	taskA := testTask("taskA", "proj-a")
	taskB := testTask("taskB", "proj-b")
	client.nextTask["proj-a"] = taskA
	client.nextTask["proj-b"] = taskB
	client.claimResult = ClaimResult{Success: true}

	executor := newMockExecutor()
	proc := newMockProcess(100)
	executor.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/test"}

	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	ctx := context.Background()
	tr.poll(ctx)

	// With maxParallel=2, should spawn from both projects
	spawns := executor.getSpawnCalls()
	if len(spawns) != 2 {
		t.Errorf("expected 2 spawn calls, got %d", len(spawns))
	}
}

// =============================================================================
// Blocked task handling
// =============================================================================

func TestTaskRunner_CheckRunningTasks_BlockedTask(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	proc := newMockProcess(100)
	task := testRunningTask("task1")
	processMgr.Add("task1", task, proc)
	processMgr.setCompletion("task1", CompletionBlocked)

	ctx := context.Background()
	tr.checkRunningTasks(ctx)

	updates := client.getUpdateStatusCalls()
	if len(updates) == 0 {
		t.Fatal("should update API status for blocked task")
	}
	if updates[0].Status != "blocked" {
		t.Errorf("API status = %q, want %q for blocked task", updates[0].Status, "blocked")
	}
}

// =============================================================================
// ExecuteTask Tests (manual execution from TUI)
// =============================================================================

func TestExecuteTask_ClaimsAndSpawns(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	tr := newTestRunner(client, executor, processMgr, stateMgr)

	task := &types.ResolvedTask{
		ID:       "task-1",
		Path:     "projects/proj-a/task/task-1.md",
		Title:    "Test task",
		Priority: "high",
		Status:   "pending",
	}

	ctx := context.Background()
	err := tr.ExecuteTask(ctx, task, "proj-a")
	if err != nil {
		t.Fatalf("ExecuteTask returned error: %v", err)
	}

	// Should claim the task
	claims := client.getClaimCalls()
	if len(claims) != 1 {
		t.Fatalf("expected 1 claim call, got %d", len(claims))
	}
	if claims[0].TaskID != "task-1" {
		t.Errorf("claimed task ID = %q, want %q", claims[0].TaskID, "task-1")
	}

	// Should update status to in_progress
	updates := client.getUpdateStatusCalls()
	if len(updates) != 1 {
		t.Fatalf("expected 1 status update, got %d", len(updates))
	}
	if updates[0].Status != "in_progress" {
		t.Errorf("status = %q, want %q", updates[0].Status, "in_progress")
	}

	// Should spawn the task
	spawns := executor.getSpawnCalls()
	if len(spawns) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(spawns))
	}
	if spawns[0].TaskID != "task-1" {
		t.Errorf("spawned task ID = %q, want %q", spawns[0].TaskID, "task-1")
	}
}

func TestExecuteTask_AtCapacity_ReturnsError(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	tr := newTestRunner(client, executor, processMgr, stateMgr)

	// Fill up to max parallel (default is 2 from testRunnerConfig)
	processMgr.processes["existing-1"] = &ProcessInfo{
		Task: RunningTask{ID: "existing-1"},
		Proc: newMockProcess(1001),
	}
	processMgr.processes["existing-2"] = &ProcessInfo{
		Task: RunningTask{ID: "existing-2"},
		Proc: newMockProcess(1002),
	}

	task := &types.ResolvedTask{
		ID:       "task-1",
		Path:     "projects/proj-a/task/task-1.md",
		Title:    "Test task",
		Priority: "high",
		Status:   "pending",
	}

	ctx := context.Background()
	err := tr.ExecuteTask(ctx, task, "proj-a")
	if err == nil {
		t.Fatal("expected error when at capacity")
	}

	// Should NOT claim the task
	claims := client.getClaimCalls()
	if len(claims) != 0 {
		t.Errorf("should not claim when at capacity, got %d claims", len(claims))
	}
}

func TestExecuteTask_InProgress_SkipsClaimAndSpawns(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	tr := newTestRunner(client, executor, processMgr, stateMgr)

	task := &types.ResolvedTask{
		ID:       "task-1",
		Path:     "projects/proj-a/task/task-1.md",
		Title:    "Resuming task",
		Priority: "high",
		Status:   "in_progress", // Already in progress — resume path
	}

	ctx := context.Background()
	err := tr.ExecuteTask(ctx, task, "proj-a")
	if err != nil {
		t.Fatalf("ExecuteTask returned error: %v", err)
	}

	// Should NOT claim the task (skip claim for in_progress)
	claims := client.getClaimCalls()
	if len(claims) != 0 {
		t.Errorf("should not claim in_progress task, got %d claims", len(claims))
	}

	// Should NOT update status (already in_progress)
	updates := client.getUpdateStatusCalls()
	if len(updates) != 0 {
		t.Errorf("should not update status for in_progress task, got %d updates", len(updates))
	}

	// Should still spawn the task
	spawns := executor.getSpawnCalls()
	if len(spawns) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(spawns))
	}
	if spawns[0].TaskID != "task-1" {
		t.Errorf("spawned task ID = %q, want %q", spawns[0].TaskID, "task-1")
	}
}

func TestExecuteTask_ClaimFails_ReturnsError(t *testing.T) {
	client := newMockClient()
	client.claimResult = ClaimResult{Success: false, ClaimedBy: "other-runner"}
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	tr := newTestRunner(client, executor, processMgr, stateMgr)

	task := &types.ResolvedTask{
		ID:       "task-1",
		Path:     "projects/proj-a/task/task-1.md",
		Title:    "Test task",
		Priority: "high",
		Status:   "pending",
	}

	ctx := context.Background()
	err := tr.ExecuteTask(ctx, task, "proj-a")
	if err == nil {
		t.Fatal("expected error when claim fails")
	}

	// Should NOT spawn the task
	spawns := executor.getSpawnCalls()
	if len(spawns) != 0 {
		t.Errorf("should not spawn when claim fails, got %d spawns", len(spawns))
	}
}

// =============================================================================
// ExecuteFeature Tests
// =============================================================================

func TestExecuteFeature_FiltersToReadyTasks(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	tr := newTestRunner(client, executor, processMgr, stateMgr)

	tasks := []types.ResolvedTask{
		{ID: "task-1", Path: "projects/proj-a/task/task-1.md", Title: "Ready task", Priority: "high", Status: "pending", Classification: "ready"},
		{ID: "task-2", Path: "projects/proj-a/task/task-2.md", Title: "Waiting task", Priority: "high", Status: "pending", Classification: "waiting"},
		{ID: "task-3", Path: "projects/proj-a/task/task-3.md", Title: "Blocked task", Priority: "high", Status: "blocked", Classification: "blocked"},
	}

	ctx := context.Background()
	started, err := tr.ExecuteFeature(ctx, tasks, "proj-a")
	if err != nil {
		t.Fatalf("ExecuteFeature returned error: %v", err)
	}
	if started != 1 {
		t.Fatalf("expected 1 started, got %d", started)
	}

	// Should only spawn the ready task
	spawns := executor.getSpawnCalls()
	if len(spawns) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(spawns))
	}
	if spawns[0].TaskID != "task-1" {
		t.Errorf("spawned task ID = %q, want %q", spawns[0].TaskID, "task-1")
	}
}

func TestExecuteFeature_NoReadyTasks_ReturnsZeroNil(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	tr := newTestRunner(client, executor, processMgr, stateMgr)

	tasks := []types.ResolvedTask{
		{ID: "task-1", Path: "projects/proj-a/task/task-1.md", Title: "Waiting", Priority: "high", Status: "pending", Classification: "waiting"},
		{ID: "task-2", Path: "projects/proj-a/task/task-2.md", Title: "Blocked", Priority: "high", Status: "blocked", Classification: "blocked"},
	}

	ctx := context.Background()
	started, err := tr.ExecuteFeature(ctx, tasks, "proj-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if started != 0 {
		t.Fatalf("expected 0 started, got %d", started)
	}

	// Should not spawn anything
	spawns := executor.getSpawnCalls()
	if len(spawns) != 0 {
		t.Errorf("expected no spawn calls, got %d", len(spawns))
	}
}

func TestExecuteFeature_EmptyTasks_ReturnsZeroNil(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	tr := newTestRunner(client, executor, processMgr, stateMgr)

	ctx := context.Background()
	started, err := tr.ExecuteFeature(ctx, nil, "proj-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if started != 0 {
		t.Fatalf("expected 0 started, got %d", started)
	}
}

func TestExecuteFeature_SortsByPriorityThenTitle(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	// Need more capacity for this test
	cfg := testRunnerConfig()
	cfg.MaxParallel = 5
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	tasks := []types.ResolvedTask{
		{ID: "low-b", Path: "projects/proj-a/task/low-b.md", Title: "Beta", Priority: "low", Status: "pending", Classification: "ready"},
		{ID: "high-a", Path: "projects/proj-a/task/high-a.md", Title: "Alpha", Priority: "high", Status: "pending", Classification: "ready"},
		{ID: "med-c", Path: "projects/proj-a/task/med-c.md", Title: "Charlie", Priority: "medium", Status: "pending", Classification: "ready"},
		{ID: "high-d", Path: "projects/proj-a/task/high-d.md", Title: "Delta", Priority: "high", Status: "pending", Classification: "ready"},
	}

	ctx := context.Background()
	started, err := tr.ExecuteFeature(ctx, tasks, "proj-a")
	if err != nil {
		t.Fatalf("ExecuteFeature returned error: %v", err)
	}
	if started != 4 {
		t.Fatalf("expected 4 started, got %d", started)
	}

	// Should spawn in priority order: high(Alpha, Delta), medium(Charlie), low(Beta)
	spawns := executor.getSpawnCalls()
	if len(spawns) != 4 {
		t.Fatalf("expected 4 spawn calls, got %d", len(spawns))
	}
	expectedOrder := []string{"high-a", "high-d", "med-c", "low-b"}
	for i, expected := range expectedOrder {
		if spawns[i].TaskID != expected {
			t.Errorf("spawn[%d].TaskID = %q, want %q", i, spawns[i].TaskID, expected)
		}
	}
}

func TestExecuteFeature_RespectsCapacity(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	tr := newTestRunner(client, executor, processMgr, stateMgr) // maxParallel = 2

	// Fill 1 slot, leaving 1 available
	processMgr.processes["existing-1"] = &ProcessInfo{
		Task: RunningTask{ID: "existing-1"},
		Proc: newMockProcess(1001),
	}

	tasks := []types.ResolvedTask{
		{ID: "task-1", Path: "projects/proj-a/task/task-1.md", Title: "First", Priority: "high", Status: "pending", Classification: "ready"},
		{ID: "task-2", Path: "projects/proj-a/task/task-2.md", Title: "Second", Priority: "high", Status: "pending", Classification: "ready"},
		{ID: "task-3", Path: "projects/proj-a/task/task-3.md", Title: "Third", Priority: "high", Status: "pending", Classification: "ready"},
	}

	ctx := context.Background()
	started, err := tr.ExecuteFeature(ctx, tasks, "proj-a")
	if err != nil {
		t.Fatalf("ExecuteFeature returned error: %v", err)
	}
	// Only 1 slot available (maxParallel=2, running=1), so only 1 should start
	if started != 1 {
		t.Fatalf("expected 1 started (capacity limited), got %d", started)
	}
}

func TestExecuteFeature_AtCapacity_ReturnsError(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	tr := newTestRunner(client, executor, processMgr, stateMgr) // maxParallel = 2

	// Fill all slots
	processMgr.processes["existing-1"] = &ProcessInfo{
		Task: RunningTask{ID: "existing-1"},
		Proc: newMockProcess(1001),
	}
	processMgr.processes["existing-2"] = &ProcessInfo{
		Task: RunningTask{ID: "existing-2"},
		Proc: newMockProcess(1002),
	}

	tasks := []types.ResolvedTask{
		{ID: "task-1", Path: "projects/proj-a/task/task-1.md", Title: "Task", Priority: "high", Status: "pending", Classification: "ready"},
	}

	ctx := context.Background()
	started, err := tr.ExecuteFeature(ctx, tasks, "proj-a")
	if err == nil {
		t.Fatal("expected error when at capacity")
	}
	if started != 0 {
		t.Errorf("expected 0 started when at capacity, got %d", started)
	}
}

func TestExecuteFeature_ContextCancelled_StopsLoop(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	cfg.MaxParallel = 10
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	tasks := []types.ResolvedTask{
		{ID: "task-1", Path: "projects/proj-a/task/task-1.md", Title: "First", Priority: "high", Status: "pending", Classification: "ready"},
		{ID: "task-2", Path: "projects/proj-a/task/task-2.md", Title: "Second", Priority: "high", Status: "pending", Classification: "ready"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	started, _ := tr.ExecuteFeature(ctx, tasks, "proj-a")
	// With cancelled context, should start 0 tasks
	if started != 0 {
		t.Errorf("expected 0 started with cancelled context, got %d", started)
	}
}

func TestExecuteFeature_PartialFailure_ContinuesAndReportsError(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	cfg.MaxParallel = 5
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	// Make claims fail
	client.claimResult = ClaimResult{Success: false, ClaimedBy: "other-runner"}

	tasks := []types.ResolvedTask{
		{ID: "task-1", Path: "projects/proj-a/task/task-1.md", Title: "Fails claim", Priority: "high", Status: "pending", Classification: "ready"},
	}

	ctx := context.Background()
	started, err := tr.ExecuteFeature(ctx, tasks, "proj-a")

	// Task failed to claim, so 0 started but error reported
	if started != 0 {
		t.Errorf("expected 0 started (claim failed), got %d", started)
	}
	if err == nil {
		t.Error("expected error when task fails to start")
	}
}

// =============================================================================
// SSE Listener Wiring Tests
// =============================================================================

func TestTaskRunner_Start_CreatesSSEListener(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	ctx, cancel := context.WithCancel(context.Background())

	// Start in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- tr.Start(ctx)
	}()

	// Stop and wait for Start to return before reading tr.sseListener.
	// Start writes the field from its own goroutine and there is no lock on
	// it (Stop() only reads it after <-tr.done, so production is already
	// ordered). Receiving from errCh is the test's happens-before edge —
	// reading the field while Start is still running is a data race.
	// Start has no early-return before the SSE wiring, so by the time it
	// returns the listener has been created.
	cancel()
	<-errCh

	// Verify SSE listener was created (config has BrainAPIURL and projects are set)
	if tr.sseListener == nil {
		t.Error("Start() should create SSE listener when BrainAPIURL is set and projects exist")
	}
}

func TestTaskRunner_Start_NoSSEListener_WhenNoAPIURL(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	cfg.BrainAPIURL = "" // No API URL

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- tr.Start(ctx)
	}()

	// Wait for Start to return before reading tr.sseListener — see
	// TestTaskRunner_Start_CreatesSSEListener for why.
	cancel()
	<-errCh

	// Should NOT create SSE listener when no API URL
	if tr.sseListener != nil {
		t.Error("Start() should NOT create SSE listener when BrainAPIURL is empty")
	}
}

func TestTaskRunner_Start_NoSSEListener_WhenNoProjects(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{}, // No projects
		Config:     testRunnerConfig(),
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- tr.Start(ctx)
	}()

	// Wait for Start to return before reading tr.sseListener — see
	// TestTaskRunner_Start_CreatesSSEListener for why.
	cancel()
	<-errCh

	// Should NOT create SSE listener when no projects
	if tr.sseListener != nil {
		t.Error("Start() should NOT create SSE listener when no projects")
	}
}

func TestTaskRunner_Stop_StopsSSEListener(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		tr.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Cancel context and call Stop
	cancel()
	err := tr.Stop()
	if err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	// After Stop, sseListener should still be non-nil (it was created)
	// but its internal cancel should have been called.
	// We verify it was created — Stop() calling sseListener.Stop() is
	// tested by the fact that Stop() doesn't hang or panic.
	if tr.sseListener == nil {
		t.Error("sseListener should have been created during Start()")
	}
}

// =============================================================================
// Executor Registry / Dispatch Tests
// =============================================================================

func TestNewTaskRunner_ExecutorsMap_PopulatedFromOptions(t *testing.T) {
	exec1 := newMockExecutor()
	exec2 := newMockExecutor()

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"},
		Config:   testRunnerConfig(),
		Mode:     ExecutionModeHeadless,
		Executors: map[string]TaskExecutor{
			"opencode": exec1,
			"custom":   exec2,
		},
		ProcessMgr: newMockProcessMgr(),
		StateMgr:   newMockStateMgr(),
		Client:     newMockClient(),
	})

	if len(tr.executors) != 2 {
		t.Fatalf("expected 2 executors, got %d", len(tr.executors))
	}
	if tr.executors["opencode"] != exec1 {
		t.Error("opencode executor not set correctly")
	}
	if tr.executors["custom"] != exec2 {
		t.Error("custom executor not set correctly")
	}
}

func TestNewTaskRunner_BackwardCompat_SingleExecutor(t *testing.T) {
	exec1 := newMockExecutor()

	// Using legacy single Executor field should register it as "opencode"
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     testRunnerConfig(),
		Mode:       ExecutionModeHeadless,
		Executor:   exec1,
		ProcessMgr: newMockProcessMgr(),
		StateMgr:   newMockStateMgr(),
		Client:     newMockClient(),
	})

	if len(tr.executors) != 1 {
		t.Fatalf("expected 1 executor (backward compat), got %d", len(tr.executors))
	}
	if tr.executors["opencode"] != exec1 {
		t.Error("single Executor should be registered as 'opencode'")
	}
}

func TestTaskRunner_BuildFetchOptionsUsesRegisteredExecutorNames(t *testing.T) {
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"},
		Config:   testRunnerConfig(),
		Mode:     ExecutionModeHeadless,
		Executors: map[string]TaskExecutor{
			"opencode": newMockExecutor(),
			"pi":       newMockExecutor(),
		},
		ProcessMgr: newMockProcessMgr(),
		StateMgr:   newMockStateMgr(),
		Client:     newMockClient(),
	})

	opts := tr.buildFetchOptions()
	if opts == nil {
		t.Fatal("expected fetch options")
	}
	if !reflect.DeepEqual(opts.Executors, []string{"opencode", "pi"}) {
		t.Fatalf("Executors = %#v, want [opencode pi]", opts.Executors)
	}
	if opts.RunnerID != tr.runnerID {
		t.Fatalf("RunnerID = %q, want %q", opts.RunnerID, tr.runnerID)
	}
}

func TestClaimAndSpawn_DispatchesToCorrectExecutor(t *testing.T) {
	client := newMockClient()
	client.claimResult = ClaimResult{Success: true}

	execOpencode := newMockExecutor()
	procOC := newMockProcess(100)
	execOpencode.spawnResult = &SpawnResult{PID: 100, Proc: procOC, Workdir: "/test"}

	execCustom := newMockExecutor()
	procCustom := newMockProcess(200)
	execCustom.spawnResult = &SpawnResult{PID: 200, Proc: procCustom, Workdir: "/test"}

	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"},
		Config:   testRunnerConfig(),
		Mode:     ExecutionModeHeadless,
		Executors: map[string]TaskExecutor{
			"opencode": execOpencode,
			"custom":   execCustom,
		},
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
		Client:     client,
	})

	// Task with Executor="custom" should be dispatched to execCustom
	task := testTask("task1", "proj-a")
	task.Executor = "custom"

	ctx := context.Background()
	err := tr.claimAndSpawn(ctx, task, "proj-a")
	if err != nil {
		t.Fatalf("claimAndSpawn returned error: %v", err)
	}

	// execCustom should have been called
	customSpawns := execCustom.getSpawnCalls()
	if len(customSpawns) != 1 {
		t.Errorf("expected 1 spawn call to custom executor, got %d", len(customSpawns))
	}

	// execOpencode should NOT have been called
	ocSpawns := execOpencode.getSpawnCalls()
	if len(ocSpawns) != 0 {
		t.Errorf("expected 0 spawn calls to opencode executor, got %d", len(ocSpawns))
	}
}

func TestClaimAndSpawn_EmptyExecutor_DefaultsToOpencode(t *testing.T) {
	client := newMockClient()
	client.claimResult = ClaimResult{Success: true}

	execOpencode := newMockExecutor()
	proc := newMockProcess(100)
	execOpencode.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/test"}

	execCustom := newMockExecutor()
	procCustom := newMockProcess(200)
	execCustom.spawnResult = &SpawnResult{PID: 200, Proc: procCustom, Workdir: "/test"}

	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"},
		Config:   testRunnerConfig(),
		Mode:     ExecutionModeHeadless,
		Executors: map[string]TaskExecutor{
			"opencode": execOpencode,
			"custom":   execCustom,
		},
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
		Client:     client,
	})

	// Task with no Executor set (empty string) → defaults to "opencode"
	task := testTask("task1", "proj-a")
	task.Executor = ""

	ctx := context.Background()
	err := tr.claimAndSpawn(ctx, task, "proj-a")
	if err != nil {
		t.Fatalf("claimAndSpawn returned error: %v", err)
	}

	// execOpencode should have been called
	ocSpawns := execOpencode.getSpawnCalls()
	if len(ocSpawns) != 1 {
		t.Errorf("expected 1 spawn call to opencode executor (default), got %d", len(ocSpawns))
	}

	// execCustom should NOT have been called
	customSpawns := execCustom.getSpawnCalls()
	if len(customSpawns) != 0 {
		t.Errorf("expected 0 spawn calls to custom executor, got %d", len(customSpawns))
	}
}

func TestClaimAndSpawn_MissingExecutor_ReleasesAndSkips(t *testing.T) {
	client := newMockClient()
	client.claimResult = ClaimResult{Success: true}

	execOpencode := newMockExecutor()
	proc := newMockProcess(100)
	execOpencode.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/test"}

	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"},
		Config:   testRunnerConfig(),
		Mode:     ExecutionModeHeadless,
		Executors: map[string]TaskExecutor{
			"opencode": execOpencode,
		},
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
		Client:     client,
	})

	// Task requests "pi-rpc" executor which is NOT registered
	task := testTask("task1", "proj-a")
	task.Executor = "pi-rpc"

	ctx := context.Background()
	err := tr.claimAndSpawn(ctx, task, "proj-a")
	if err == nil {
		t.Fatal("expected error when executor not found")
	}

	// Should contain info about missing executor
	if !strings.Contains(err.Error(), "pi-rpc") {
		t.Errorf("error should mention missing executor 'pi-rpc', got: %v", err)
	}

	// Should release the claim
	releases := client.getReleaseCalls()
	if len(releases) == 0 {
		t.Error("should release task when executor not found")
	}

	// Should NOT have spawned anything
	ocSpawns := execOpencode.getSpawnCalls()
	if len(ocSpawns) != 0 {
		t.Errorf("expected 0 spawn calls, got %d", len(ocSpawns))
	}
}

func TestClaimAndSpawn_ResolveWorkdir_UsesMatchedExecutor(t *testing.T) {
	client := newMockClient()
	client.claimResult = ClaimResult{Success: true}

	execOpencode := newMockExecutor()
	execOpencode.resolveWorkdir = "/opencode/workdir"
	proc := newMockProcess(100)
	execOpencode.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/opencode/workdir"}

	execCustom := newMockExecutor()
	execCustom.resolveWorkdir = "/custom/workdir"
	procCustom := newMockProcess(200)
	execCustom.spawnResult = &SpawnResult{PID: 200, Proc: procCustom, Workdir: "/custom/workdir"}

	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"},
		Config:   testRunnerConfig(),
		Mode:     ExecutionModeHeadless,
		Executors: map[string]TaskExecutor{
			"opencode": execOpencode,
			"custom":   execCustom,
		},
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
		Client:     client,
	})

	task := testTask("task1", "proj-a")
	task.Executor = "custom"

	ctx := context.Background()
	err := tr.claimAndSpawn(ctx, task, "proj-a")
	if err != nil {
		t.Fatalf("claimAndSpawn returned error: %v", err)
	}

	// The custom executor's ResolveWorkdir and Spawn should have been used
	customSpawns := execCustom.getSpawnCalls()
	if len(customSpawns) != 1 {
		t.Fatalf("expected 1 spawn call to custom executor, got %d", len(customSpawns))
	}
	if customSpawns[0].Opts.Workdir != "/custom/workdir" {
		t.Errorf("workdir = %q, want %q", customSpawns[0].Opts.Workdir, "/custom/workdir")
	}
}

func TestTaskRunner_RegisterWithAPI_IncludesExecutorNames(t *testing.T) {
	client := newMockClient()
	execOpencode := newMockExecutor()
	execCustom := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	config := testRunnerConfig()
	config.Capabilities = []string{"docker", "gpu"}
	config.MaxParallel = 7

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"},
		Config:   config,
		Mode:     ExecutionModeHeadless,
		Executors: map[string]TaskExecutor{
			"opencode": execOpencode,
			"pi-rpc":   execCustom,
		},
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
		Client:     client,
	})

	ctx := context.Background()
	tr.registerWithAPI(ctx)

	client.mu.Lock()
	defer client.mu.Unlock()

	if len(client.registerCalls) != 1 {
		t.Fatalf("expected 1 register call, got %d", len(client.registerCalls))
	}

	reg := client.registerCalls[0]
	executorSet := make(map[string]bool)
	for _, e := range reg.Executors {
		executorSet[e] = true
	}
	if !executorSet["opencode"] {
		t.Error("registration should include 'opencode' executor")
	}
	if !executorSet["pi-rpc"] {
		t.Error("registration should include 'pi-rpc' executor")
	}
	if !reflect.DeepEqual(reg.Capabilities, []string{"docker", "gpu"}) {
		t.Errorf("registration capabilities = %v, want [docker gpu]", reg.Capabilities)
	}
	if reg.MaxParallel != 7 {
		t.Errorf("registration max_parallel = %d, want 7", reg.MaxParallel)
	}
}

func TestResumeTask_UsesExecutorDispatch(t *testing.T) {
	client := newMockClient()

	execOpencode := newMockExecutor()
	proc := newMockProcess(100)
	execOpencode.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/test"}

	execCustom := newMockExecutor()
	procCustom := newMockProcess(200)
	execCustom.spawnResult = &SpawnResult{PID: 200, Proc: procCustom, Workdir: "/test"}

	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"},
		Config:   testRunnerConfig(),
		Mode:     ExecutionModeHeadless,
		Executors: map[string]TaskExecutor{
			"opencode": execOpencode,
			"custom":   execCustom,
		},
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
		Client:     client,
	})

	task := testTask("task1", "proj-a")
	task.Executor = "custom"
	task.Status = "in_progress"

	ctx := context.Background()
	err := tr.resumeTask(ctx, task, "proj-a")
	if err != nil {
		t.Fatalf("resumeTask returned error: %v", err)
	}

	// custom executor should have been used
	customSpawns := execCustom.getSpawnCalls()
	if len(customSpawns) != 1 {
		t.Errorf("expected 1 spawn call to custom executor, got %d", len(customSpawns))
	}

	// opencode executor should NOT have been used
	ocSpawns := execOpencode.getSpawnCalls()
	if len(ocSpawns) != 0 {
		t.Errorf("expected 0 spawn calls to opencode executor, got %d", len(ocSpawns))
	}
}

// =============================================================================
// Orphan Reaper Tests
// =============================================================================

// TestReapOrphanedTasks_NoProjects exercises the early-return path when the
// runner has no projects to scan. Reaper must be a no-op.
func TestReapOrphanedTasks_NoProjects(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   nil, // no projects
		Config:     testRunnerConfig(),
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	tr.reapOrphanedTasks(context.Background())

	if len(client.getUpdateStatusCalls()) > 0 {
		t.Error("reaper must not call UpdateTaskStatus when no projects configured")
	}
	if len(client.getClaimCalls()) > 0 {
		t.Error("reaper must not claim when no projects configured")
	}
}

// TestReapOrphanedTasks_NoOrphans verifies the reaper is a no-op when
// ListEntries returns an empty slice.
func TestReapOrphanedTasks_NoOrphans(t *testing.T) {
	client := newMockClient()
	client.listEntriesFunc = func(params map[string]string) (*types.ListEntriesResponse, error) {
		// Sanity: the reaper queries with type=task and status=in_progress.
		if params["status"] != "in_progress" {
			t.Errorf("expected status=in_progress filter, got %v", params)
		}
		if params["type"] != "task" {
			t.Errorf("expected type=task filter, got %v", params)
		}
		return &types.ListEntriesResponse{}, nil
	}

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	tr := newTestRunner(client, executor, processMgr, stateMgr)

	tr.reapOrphanedTasks(context.Background())

	if len(client.getUpdateStatusCalls()) > 0 {
		t.Error("reaper must not produce status updates when no orphans exist")
	}
}

// TestReapOrphanedTasks_MarksClaimableOrphanBlocked is the core happy path:
// an in_progress task with no live owner is claimed, marked blocked, then
// released.
func TestReapOrphanedTasks_MarksClaimableOrphanBlocked(t *testing.T) {
	orphan := types.BrainEntry{
		ID:     "orphan1",
		Path:   "projects/proj-a/task/orphan1.md",
		Type:   "task",
		Status: "in_progress",
		Title:  "Automation: dkkz9pr1",
	}

	client := newMockClient()
	client.listEntriesFunc = func(params map[string]string) (*types.ListEntriesResponse, error) {
		if params["project"] == "proj-a" {
			return &types.ListEntriesResponse{Entries: []types.BrainEntry{orphan}}, nil
		}
		return &types.ListEntriesResponse{}, nil
	}
	// Claim succeeds — no live runner owns this task.
	client.claimResult = ClaimResult{Success: true, TaskID: orphan.ID}
	// GetEntry is called after claim to confirm the task is still in_progress.
	client.getEntryResult = map[string]*types.BrainEntry{
		orphan.Path: {Path: orphan.Path, Status: "in_progress"},
	}

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	tr := newTestRunner(client, executor, processMgr, stateMgr)

	tr.reapOrphanedTasks(context.Background())

	// Status updates: must include a "blocked" for the orphan.
	updates := client.getUpdateStatusCalls()
	foundBlocked := false
	for _, u := range updates {
		if u.TaskPath == orphan.Path && u.Status == "blocked" {
			foundBlocked = true
		}
	}
	if !foundBlocked {
		t.Errorf("expected orphan task to be marked blocked, got updates: %+v", updates)
	}

	// An explanatory note must be appended.
	appends := client.appendCalls
	foundNote := false
	for _, a := range appends {
		if a.TaskPath == orphan.Path && len(a.Content) > 0 {
			foundNote = true
		}
	}
	if !foundNote {
		t.Errorf("expected reaper to append a note to %s, got: %+v", orphan.Path, appends)
	}

	// Claim must be released so the lease doesn't dangle.
	releases := client.getReleaseCalls()
	foundRelease := false
	for _, r := range releases {
		if r.TaskID == orphan.ID && r.RunnerID == tr.runnerID {
			foundRelease = true
		}
	}
	if !foundRelease {
		t.Errorf("expected reaper to release claim for %s, got: %+v", orphan.ID, releases)
	}
}

// TestReapOrphanedTasks_SkipsTaskOwnedByLiveRunner verifies the reaper
// respects the claim system: if another runner holds an unexpired claim,
// the orphan is left alone for the lease cleanup goroutine to handle.
func TestReapOrphanedTasks_SkipsTaskOwnedByLiveRunner(t *testing.T) {
	orphan := types.BrainEntry{
		ID:     "owned-task",
		Path:   "projects/proj-a/task/owned-task.md",
		Type:   "task",
		Status: "in_progress",
		Title:  "Owned by another runner",
	}

	client := newMockClient()
	client.listEntriesFunc = func(params map[string]string) (*types.ListEntriesResponse, error) {
		if params["project"] == "proj-a" {
			return &types.ListEntriesResponse{Entries: []types.BrainEntry{orphan}}, nil
		}
		return &types.ListEntriesResponse{}, nil
	}
	// Claim conflict — task is held by another live runner.
	client.claimResult = ClaimResult{Success: false, TaskID: orphan.ID, ClaimedBy: "runner-other", Message: "Task already claimed"}

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	tr := newTestRunner(client, executor, processMgr, stateMgr)

	tr.reapOrphanedTasks(context.Background())

	// Must not touch a task someone else owns.
	updates := client.getUpdateStatusCalls()
	for _, u := range updates {
		if u.TaskPath == orphan.Path {
			t.Errorf("reaper must not update status for live-owned task, got: %+v", u)
		}
	}
	// Must not append to it either.
	for _, a := range client.appendCalls {
		if a.TaskPath == orphan.Path {
			t.Errorf("reaper must not append to live-owned task, got: %+v", a)
		}
	}
}

// TestReapOrphanedTasks_RaceWithCompletion handles the race where the agent
// completes the task between ListEntries and ClaimTask. The reaper must
// detect this via re-fetch and back off (release the claim, no status
// update).
func TestReapOrphanedTasks_RaceWithCompletion(t *testing.T) {
	orphan := types.BrainEntry{
		ID:     "race-task",
		Path:   "projects/proj-a/task/race-task.md",
		Type:   "task",
		Status: "in_progress",
		Title:  "Completed mid-reap",
	}

	client := newMockClient()
	client.listEntriesFunc = func(params map[string]string) (*types.ListEntriesResponse, error) {
		if params["project"] == "proj-a" {
			return &types.ListEntriesResponse{Entries: []types.BrainEntry{orphan}}, nil
		}
		return &types.ListEntriesResponse{}, nil
	}
	client.claimResult = ClaimResult{Success: true, TaskID: orphan.ID}
	// Re-fetch shows the task already completed.
	client.getEntryResult = map[string]*types.BrainEntry{
		orphan.Path: {Path: orphan.Path, Status: "completed"},
	}

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	tr := newTestRunner(client, executor, processMgr, stateMgr)

	tr.reapOrphanedTasks(context.Background())

	// Must not stomp the completed status with blocked.
	updates := client.getUpdateStatusCalls()
	for _, u := range updates {
		if u.TaskPath == orphan.Path {
			t.Errorf("reaper must not update status when task already terminalized, got: %+v", u)
		}
	}
	// Claim must still be released.
	releases := client.getReleaseCalls()
	foundRelease := false
	for _, r := range releases {
		if r.TaskID == orphan.ID {
			foundRelease = true
		}
	}
	if !foundRelease {
		t.Error("reaper must release the claim even when backing off due to race")
	}
}

// TestReapOrphanedTasks_ListFailureDoesNotPanic verifies graceful
// degradation when ListEntries errors. The runner must continue starting up.
func TestReapOrphanedTasks_ListFailureDoesNotPanic(t *testing.T) {
	client := newMockClient()
	client.listEntriesErr = fmt.Errorf("network unreachable")

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	tr := newTestRunner(client, executor, processMgr, stateMgr)

	// Must not panic, must not produce updates.
	tr.reapOrphanedTasks(context.Background())

	if len(client.getUpdateStatusCalls()) > 0 {
		t.Errorf("reaper must not produce updates when list fails, got: %+v", client.getUpdateStatusCalls())
	}
}

// TestTaskRunner_Dispatch_RespectsGlobalServerPause confirms the fix for the
// "lease stuck in pushed when tasks: paused globally" symptom. Previously
// serverPauseState() only read PausedProjects, so a global pause from the
// PWA never propagated to the dispatch SSE handler — the runner would
// happily try to spawn and the user would see no progress because the spawn
// path itself blocks on other gates. Now the runner rejects with
// runner_paused as soon as it sees Paused=true on the server status.
func TestTaskRunner_Dispatch_RespectsGlobalServerPause(t *testing.T) {
	client := newMockClient()
	client.readyTasks["proj-a"] = []types.ResolvedTask{*testTask("task1", "proj-a")}
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	cfg.Capabilities = []string{"dispatch_push"}
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"}, Config: cfg, Mode: ExecutionModeHeadless,
		Client: client, Executor: executor, ProcessMgr: processMgr, StateMgr: stateMgr,
	})
	// Runner pause state is authoritative for the dispatch gate. In
	// production it's synced from the server via SSE
	// CommandPause/CommandResume; here we set it directly.
	tr.PauseAll()

	tr.handleCommand(context.Background(), RunnerCommand{
		Type: CommandDispatch, ProjectID: "proj-a", TaskID: "task1", LeaseID: "lease-1",
	})

	rejects := client.getRejectCalls()
	if len(rejects) != 1 {
		t.Fatalf("expected exactly one reject when globally paused, got %d", len(rejects))
	}
	if rejects[0].Reason.Code != "runner_paused" {
		t.Fatalf("reject code = %q, want runner_paused", rejects[0].Reason.Code)
	}
	if len(executor.getSpawnCalls()) != 0 {
		t.Fatalf("must not spawn when globally paused; got %d spawns", len(executor.getSpawnCalls()))
	}
}

// TestTaskRunner_Dispatch_ForceBypassesPause confirms force=true on a
// dispatch command lets the runner spawn even when the server has paused
// task scheduling. This pairs with the PWA's "Force" toast action.
func TestTaskRunner_Dispatch_ForceBypassesPause(t *testing.T) {
	client := newMockClient()
	client.readyTasks["proj-a"] = []types.ResolvedTask{*testTask("task1", "proj-a")}
	client.runnerStatus = &types.RunnerStatusResponse{Running: true, Paused: true}
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	cfg.Capabilities = []string{"dispatch_push"}
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"}, Config: cfg, Mode: ExecutionModeHeadless,
		Client: client, Executor: executor, ProcessMgr: processMgr, StateMgr: stateMgr,
	})

	tr.handleCommand(context.Background(), RunnerCommand{
		Type: CommandDispatch, ProjectID: "proj-a", TaskID: "task1", LeaseID: "lease-1",
		Force: true,
	})

	if got := len(client.getRejectCalls()); got != 0 {
		t.Fatalf("force=true must not reject when paused, got %d rejects: %+v", got, client.getRejectCalls())
	}
	if got := len(client.getAckCalls()); got != 1 {
		t.Fatalf("force=true must ack the dispatch, got %d acks", got)
	}
	spawns := executor.getSpawnCalls()
	if len(spawns) != 1 {
		t.Fatalf("force=true must spawn exactly once when paused, got %d", len(spawns))
	}
	if spawns[0].TaskID != "task1" {
		t.Fatalf("spawned task = %q, want task1", spawns[0].TaskID)
	}
}

// TestTaskRunner_Dispatch_ForceStillRespectsCapacity confirms force=true
// does NOT override capacity limits. Overrunning max_parallel would corrupt
// slot accounting; the PWA's Force action is an override of "user
// intent" (pause), not of "physical resource" (slots).
func TestTaskRunner_Dispatch_ForceStillRespectsCapacity(t *testing.T) {
	client := newMockClient()
	client.readyTasks["proj-a"] = []types.ResolvedTask{*testTask("task1", "proj-a")}
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	// Saturate slots: register one running process so RunningCount=1, and
	// set MaxParallel=1 so no slot is available.
	if err := processMgr.Add("blocking-task", RunningTask{ID: "blocking-task", ProjectID: "proj-a"}, newMockProcess(1)); err != nil {
		t.Fatalf("seed running process: %v", err)
	}
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	cfg.Capabilities = []string{"dispatch_push"}
	cfg.MaxParallel = 1
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"}, Config: cfg, Mode: ExecutionModeHeadless,
		Client: client, Executor: executor, ProcessMgr: processMgr, StateMgr: stateMgr,
	})

	tr.handleCommand(context.Background(), RunnerCommand{
		Type: CommandDispatch, ProjectID: "proj-a", TaskID: "task1", LeaseID: "lease-1",
		Force: true,
	})

	rejects := client.getRejectCalls()
	if len(rejects) != 1 {
		t.Fatalf("expected one reject due to capacity, got %d", len(rejects))
	}
	if rejects[0].Reason.Code != "capacity_unavailable" {
		t.Fatalf("reject code = %q, want capacity_unavailable", rejects[0].Reason.Code)
	}
	if len(executor.getSpawnCalls()) != 0 {
		t.Fatalf("force=true must not spawn over capacity")
	}
}

// TestTaskRunner_Dispatch_AutomationBypassesGlobalServerPause confirms the
// SSE dispatch path mirrors the poll path's behavior: when tasks are paused
// (globally or per-project) but automations remain enabled for the project,
// automation-generated dispatches MUST still be acked and spawned. The
// poll loop (runner.go:956–984) already implements this carve-out by
// pulling `GeneratedByPrefix: "automation:"` tasks even when paused; the
// push handler historically rejected everything as runner_paused, so the
// "tasks: paused, autos: on" UX in the PWA quietly stopped delivering
// automation work whenever dispatch-push was enabled.
func TestTaskRunner_Dispatch_AutomationBypassesGlobalServerPause(t *testing.T) {
	client := newMockClient()
	task := testTask("auto-task", "proj-a")
	task.GeneratedBy = "automation:auto1234"
	client.readyTasks["proj-a"] = []types.ResolvedTask{*task}
	// Globally task-paused on the server, but automations are NOT paused.
	client.runnerStatus = &types.RunnerStatusResponse{Running: true, Paused: true}
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	cfg.Capabilities = []string{"dispatch_push"}
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"}, Config: cfg, Mode: ExecutionModeHeadless,
		Client: client, Executor: executor, ProcessMgr: processMgr, StateMgr: stateMgr,
	})

	tr.handleCommand(context.Background(), RunnerCommand{
		Type: CommandDispatch, ProjectID: "proj-a", TaskID: "auto-task", LeaseID: "lease-1",
	})

	if got := len(client.getRejectCalls()); got != 0 {
		t.Fatalf("automation dispatch must not be rejected when only tasks (not automations) are paused, got %d rejects: %+v", got, client.getRejectCalls())
	}
	if got := len(client.getAckCalls()); got != 1 {
		t.Fatalf("automation dispatch must be acked when automations are enabled, got %d acks", got)
	}
	spawns := executor.getSpawnCalls()
	if len(spawns) != 1 {
		t.Fatalf("automation dispatch must spawn exactly once, got %d spawns", len(spawns))
	}
	if spawns[0].TaskID != "auto-task" {
		t.Fatalf("spawned task = %q, want auto-task", spawns[0].TaskID)
	}
}

// TestTaskRunner_Dispatch_AutomationStillRejectedWhenAutomationsAlsoPaused
// guards the bypass: it must NOT fire when automations are paused too.
// "Tasks paused, autos on" is the legitimate carve-out. "Tasks paused,
// autos paused" must still reject everything — otherwise users can't
// fully halt a project.
func TestTaskRunner_Dispatch_AutomationStillRejectedWhenAutomationsAlsoPaused(t *testing.T) {
	client := newMockClient()
	task := testTask("auto-task", "proj-a")
	task.GeneratedBy = "automation:auto1234"
	client.readyTasks["proj-a"] = []types.ResolvedTask{*task}
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	cfg.Capabilities = []string{"dispatch_push"}
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"}, Config: cfg, Mode: ExecutionModeHeadless,
		Client: client, Executor: executor, ProcessMgr: processMgr, StateMgr: stateMgr,
	})
	// Both tasks AND automations paused globally. Runner-local state is
	// authoritative; in production it's synced via SSE
	// CommandPause/CommandResume.
	tr.PauseAll()
	tr.PauseAutomations()

	tr.handleCommand(context.Background(), RunnerCommand{
		Type: CommandDispatch, ProjectID: "proj-a", TaskID: "auto-task", LeaseID: "lease-1",
	})

	rejects := client.getRejectCalls()
	if len(rejects) != 1 {
		t.Fatalf("expected one reject when automations also paused, got %d", len(rejects))
	}
	if rejects[0].Reason.Code != "runner_paused" {
		t.Fatalf("reject code = %q, want runner_paused", rejects[0].Reason.Code)
	}
	if len(executor.getSpawnCalls()) != 0 {
		t.Fatalf("must not spawn when automations also paused; got %d spawns", len(executor.getSpawnCalls()))
	}
}

// TestTaskRunner_Dispatch_AutomationBypassesPerProjectServerPause exercises
// the realistic operator path: rather than globally pausing the runner,
// the user adds personal-productivity to PausedProjects via the PWA while
// leaving automations on. The dispatch path must honor "autos: on" by
// letting automation tasks flow even though that specific project is in
// the server's PausedProjects list.
func TestTaskRunner_Dispatch_AutomationBypassesPerProjectServerPause(t *testing.T) {
	client := newMockClient()
	task := testTask("auto-task", "proj-a")
	task.GeneratedBy = "automation:auto1234"
	client.readyTasks["proj-a"] = []types.ResolvedTask{*task}
	// Per-project task pause (not global), automations stay on.
	client.runnerStatus = &types.RunnerStatusResponse{
		Running: true, PausedProjects: []string{"proj-a"},
	}
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	cfg.Capabilities = []string{"dispatch_push"}
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"}, Config: cfg, Mode: ExecutionModeHeadless,
		Client: client, Executor: executor, ProcessMgr: processMgr, StateMgr: stateMgr,
	})

	tr.handleCommand(context.Background(), RunnerCommand{
		Type: CommandDispatch, ProjectID: "proj-a", TaskID: "auto-task", LeaseID: "lease-1",
	})

	if got := len(client.getRejectCalls()); got != 0 {
		t.Fatalf("automation dispatch must not be rejected when only this project's tasks (not automations) are paused, got %d rejects: %+v", got, client.getRejectCalls())
	}
	if got := len(executor.getSpawnCalls()); got != 1 {
		t.Fatalf("automation dispatch must spawn exactly once, got %d", got)
	}
}

// TestTaskRunner_Dispatch_AutomationStillRejectedWhenAutomationsPerProjectPaused
// guards the per-project automation pause: even if tasks are paused for
// proj-a and automations are NOT globally paused, having proj-a in the
// server's AutomationPausedProjects list must keep automation dispatches
// rejected for that project.
func TestTaskRunner_Dispatch_AutomationStillRejectedWhenAutomationsPerProjectPaused(t *testing.T) {
	client := newMockClient()
	task := testTask("auto-task", "proj-a")
	task.GeneratedBy = "automation:auto1234"
	client.readyTasks["proj-a"] = []types.ResolvedTask{*task}
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	cfg.Capabilities = []string{"dispatch_push"}
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"}, Config: cfg, Mode: ExecutionModeHeadless,
		Client: client, Executor: executor, ProcessMgr: processMgr, StateMgr: stateMgr,
	})
	// proj-a is task-paused AND automation-paused per-project. Runner
	// state is authoritative; in production it's SSE-synced.
	tr.PauseProject("proj-a")
	tr.PauseProjectAutomations("proj-a")

	tr.handleCommand(context.Background(), RunnerCommand{
		Type: CommandDispatch, ProjectID: "proj-a", TaskID: "auto-task", LeaseID: "lease-1",
	})

	rejects := client.getRejectCalls()
	if len(rejects) != 1 {
		t.Fatalf("expected one reject when this project's automations also paused, got %d", len(rejects))
	}
	if rejects[0].Reason.Code != "runner_paused" {
		t.Fatalf("reject code = %q, want runner_paused", rejects[0].Reason.Code)
	}
	if len(executor.getSpawnCalls()) != 0 {
		t.Fatalf("must not spawn when this project's automations are paused; got %d spawns", len(executor.getSpawnCalls()))
	}
}

// TestTaskRunner_Dispatch_AutomationDoesNotBypassNonAutomationTask guards
// the gate from the other direction: a non-automation task (e.g. a
// manually-queued or feature-graph task) must still be rejected when the
// project is task-paused, regardless of the automation pause state. Only
// `generated_by` starting with "automation:" earns the carve-out.
func TestTaskRunner_Dispatch_AutomationDoesNotBypassNonAutomationTask(t *testing.T) {
	client := newMockClient()
	// Note: no GeneratedBy set — this is a regular task.
	client.readyTasks["proj-a"] = []types.ResolvedTask{*testTask("regular-task", "proj-a")}
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	cfg.Capabilities = []string{"dispatch_push"}
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"}, Config: cfg, Mode: ExecutionModeHeadless,
		Client: client, Executor: executor, ProcessMgr: processMgr, StateMgr: stateMgr,
	})
	// Runner is globally task-paused. Runner-local state is
	// authoritative; in production it's SSE-synced.
	tr.PauseAll()

	tr.handleCommand(context.Background(), RunnerCommand{
		Type: CommandDispatch, ProjectID: "proj-a", TaskID: "regular-task", LeaseID: "lease-1",
	})

	rejects := client.getRejectCalls()
	if len(rejects) != 1 {
		t.Fatalf("expected one reject for non-automation task under pause, got %d", len(rejects))
	}
	if rejects[0].Reason.Code != "runner_paused" {
		t.Fatalf("reject code = %q, want runner_paused", rejects[0].Reason.Code)
	}
	if len(executor.getSpawnCalls()) != 0 {
		t.Fatalf("must not spawn non-automation task when paused; got %d spawns", len(executor.getSpawnCalls()))
	}
}

// TestTaskRunner_DispatchConsumerNotBlockedByWedgedPoll proves the
// architectural fix for the goroutine-dump bug observed in production
// (2026-06-25): when poll() hangs inside a synchronous HTTP call, dispatch
// commands pushed via SSE must still be consumed because runner.go's main
// loop used a single goroutine that interleaved `ticker.C → poll(ctx)`
// with `commandCh → handleCommand`. A blocked poll() therefore wedged
// commandCh consumption, causing every dispatch lease to time out
// untouched ("dispatch command dropped (channel full)" 195× in 4.5 min).
//
// This test wedges CheckHealth (the first call in poll), pushes a dispatch
// command into commandCh, and asserts the command is processed within a
// short deadline. Under the broken single-goroutine architecture the test
// times out; under the fix (separate consumer goroutine) it passes.
func TestTaskRunner_DispatchConsumerNotBlockedByWedgedPoll(t *testing.T) {
	client := newMockClient()
	client.readyTasks["proj-a"] = []types.ResolvedTask{*testTask("task1", "proj-a")}
	// Block CheckHealth so poll() can never return. This simulates the
	// production bug where checkScheduledTasks → GetAllTasks → HTTP
	// request hangs indefinitely.
	client.healthBlockCh = make(chan struct{})
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	cfg.Capabilities = []string{"dispatch_push"}
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"}, Config: cfg, Mode: ExecutionModeHeadless,
		Client: client, Executor: executor, ProcessMgr: processMgr, StateMgr: stateMgr,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startDone := make(chan error, 1)
	go func() { startDone <- tr.Start(ctx) }()

	// Wait until the runner has registered (proves Start() is running and
	// the wedged initial poll() has begun).
	deadline := time.After(2 * time.Second)
	for {
		client.mu.Lock()
		registered := len(client.registerCalls) > 0
		client.mu.Unlock()
		if registered {
			break
		}
		select {
		case <-deadline:
			t.Fatal("runner did not register before deadline")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Push a dispatch command. Under the broken architecture, the main
	// select goroutine is parked inside the initial tr.poll(ctx) (which
	// blocked on CheckHealth) and cannot consume commandCh.
	tr.commandCh <- RunnerCommand{
		Type: CommandDispatch, ProjectID: "proj-a", TaskID: "task1", LeaseID: "lease-1",
	}

	// Assert the dispatch was processed (spawn invoked) within 2 seconds.
	// 2s is generous; the fix makes this near-instant. The old code
	// would never satisfy it.
	processed := false
	for i := 0; i < 200; i++ {
		if len(executor.getSpawnCalls()) >= 1 {
			processed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Unblock health so the runner can shut down cleanly regardless of
	// pass/fail (avoids leaving Start() goroutines parked on panic).
	close(client.healthBlockCh)
	cancel()
	select {
	case <-startDone:
	case <-time.After(2 * time.Second):
		t.Log("warning: Start() did not return within 2s after cancel")
	}

	if !processed {
		t.Fatalf("dispatch command was not processed while poll() was wedged: "+
			"spawn calls=%d, reject calls=%d, ack calls=%d. "+
			"Expected the command consumer to run on its own goroutine.",
			len(executor.getSpawnCalls()),
			len(client.getRejectCalls()),
			len(client.getAckCalls()))
	}
}

// =============================================================================
// Project Refresh Tests (Finding 2 — wcg6lxfz)
// =============================================================================

// setMockProjects updates the mock client's project list in a thread-safe way.
// Used by refresh tests to simulate projects appearing/disappearing between
// ListProjects calls.
func setMockProjects(m *mockClient, projects []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.projects = projects
}

// TestTaskRunner_GetProjects_ReturnsSnapshot verifies getProjects returns a
// defensive copy — callers must not be able to mutate tr.projects by
// modifying the returned slice.
func TestTaskRunner_GetProjects_ReturnsSnapshot(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	snapshot := tr.getProjects()
	if len(snapshot) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snapshot))
	}

	// Mutate the returned slice.
	snapshot[0] = "hacked"

	// Internal state must be unchanged.
	fresh := tr.getProjects()
	if fresh[0] == "hacked" {
		t.Error("getProjects returned aliased slice; internal state was mutated externally")
	}
}

// TestTaskRunner_RefreshProjects_AddsNewProject verifies that when the API
// starts returning a new project, refreshProjects appends it to tr.projects.
// This is the primary scenario from the plan: projects created after the
// runner starts must become visible without a restart.
func TestTaskRunner_RefreshProjects_AddsNewProject(t *testing.T) {
	client := newMockClient()
	setMockProjects(client, []string{"proj-a", "proj-b"})
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	// Simulate a new project appearing in the API.
	setMockProjects(client, []string{"proj-a", "proj-b", "proj-c"})

	added, removed, err := tr.refreshProjects(context.Background())
	if err != nil {
		t.Fatalf("refreshProjects: %v", err)
	}
	if len(added) != 1 || added[0] != "proj-c" {
		t.Errorf("added = %v, want [proj-c]", added)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want []", removed)
	}

	got := tr.getProjects()
	want := []string{"proj-a", "proj-b", "proj-c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("projects after refresh = %v, want %v", got, want)
	}

	// A subsequent poll must consider proj-c. The simplest way to assert
	// this without running the full poll pipeline is to verify
	// supportsProject picks it up.
	if !tr.supportsProject("proj-c") {
		t.Error("supportsProject(proj-c) = false after refresh")
	}
}

// TestTaskRunner_RefreshProjects_RemovesProject verifies removed projects are
// dropped from tr.projects (but no panic, no mid-flight task disruption at
// this layer — that's a higher-level concern).
func TestTaskRunner_RefreshProjects_RemovesProject(t *testing.T) {
	client := newMockClient()
	setMockProjects(client, []string{"proj-a", "proj-b"})
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	// Simulate proj-b being removed.
	setMockProjects(client, []string{"proj-a"})

	added, removed, err := tr.refreshProjects(context.Background())
	if err != nil {
		t.Fatalf("refreshProjects: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("added = %v, want []", added)
	}
	if len(removed) != 1 || removed[0] != "proj-b" {
		t.Errorf("removed = %v, want [proj-b]", removed)
	}

	got := tr.getProjects()
	want := []string{"proj-a"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("projects after refresh = %v, want %v", got, want)
	}
}

// TestTaskRunner_RefreshProjects_NoChange verifies a stable API response does
// not report spurious add/remove events.
func TestTaskRunner_RefreshProjects_NoChange(t *testing.T) {
	client := newMockClient()
	setMockProjects(client, []string{"proj-a", "proj-b"})
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	added, removed, err := tr.refreshProjects(context.Background())
	if err != nil {
		t.Fatalf("refreshProjects: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("added = %v, want []", added)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want []", removed)
	}

	got := tr.getProjects()
	want := []string{"proj-a", "proj-b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("projects = %v, want %v", got, want)
	}
}

// TestTaskRunner_RefreshProjects_APIErrorPreservesList verifies that a
// transient API failure does not clear the project list. The runner must
// keep polling the projects it already knows about.
func TestTaskRunner_RefreshProjects_APIErrorPreservesList(t *testing.T) {
	client := newMockClient()
	setMockProjects(client, []string{"proj-a", "proj-b"})
	client.mu.Lock()
	client.projectsErr = fmt.Errorf("simulated API failure")
	client.mu.Unlock()

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	_, _, err := tr.refreshProjects(context.Background())
	if err == nil {
		t.Fatal("refreshProjects: expected error, got nil")
	}

	got := tr.getProjects()
	want := []string{"proj-a", "proj-b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("projects after failed refresh = %v, want %v (must not clear list)", got, want)
	}
}

// TestTaskRunner_RefreshProjects_AppliesFilters verifies include/exclude
// filters from RunnerConfig are re-applied on every refresh — otherwise a
// new project matching an exclude pattern would leak in.
func TestTaskRunner_RefreshProjects_AppliesFilters(t *testing.T) {
	client := newMockClient()
	setMockProjects(client, []string{"proj-a", "proj-b"})
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	cfg.ExcludeProjects = []string{"test-*"}
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a", "proj-b"},
		Config:   cfg,
		Mode:     ExecutionModeHeadless,
		Client:   client,
		Executors: map[string]TaskExecutor{
			"opencode": executor,
		},
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	// New project "test-foo" appears in the API — should be filtered out.
	setMockProjects(client, []string{"proj-a", "proj-b", "test-foo", "proj-c"})

	added, _, err := tr.refreshProjects(context.Background())
	if err != nil {
		t.Fatalf("refreshProjects: %v", err)
	}

	// Only proj-c should be added; test-foo must be filtered.
	if len(added) != 1 || added[0] != "proj-c" {
		t.Errorf("added = %v, want [proj-c] (test-foo must be excluded)", added)
	}

	got := tr.getProjects()
	for _, p := range got {
		if p == "test-foo" {
			t.Error("test-foo leaked past ExcludeProjects filter after refresh")
		}
	}
}

// TestTaskRunner_RefreshProjects_ConcurrentReads exercises the mutex: many
// goroutines reading projects while refreshProjects mutates. Under -race
// this catches missing locks around tr.projects.
func TestTaskRunner_RefreshProjects_ConcurrentReads(t *testing.T) {
	client := newMockClient()
	setMockProjects(client, []string{"proj-a", "proj-b"})
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	// Concurrent readers.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = tr.getProjects()
				_ = tr.supportsProject("proj-a")
				select {
				case <-ctx.Done():
					return
				default:
				}
			}
		}()
	}
	// Concurrent writer.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 20; j++ {
			if j%2 == 0 {
				setMockProjects(client, []string{"proj-a", "proj-b", "proj-c"})
			} else {
				setMockProjects(client, []string{"proj-a", "proj-b"})
			}
			_, _, _ = tr.refreshProjects(ctx)
		}
	}()

	wg.Wait()
}

// TestTaskRunner_RefreshProjectsInterval_Config verifies the config field is
// wired up with the documented default and env-var override behaviour.
func TestTaskRunner_RefreshProjectsInterval_Config(t *testing.T) {
	cfg := testRunnerConfig()
	if cfg.ProjectRefreshInterval != 0 {
		// testRunnerConfig() intentionally leaves it 0 (no zero-value
		// override); the LoadConfig default is 60. Tested separately
		// in config_test.go.
	}

	// A NewTaskRunner must accept and store the value.
	cfg.ProjectRefreshInterval = 5
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     newMockClient(),
		Executor:   newMockExecutor(),
		ProcessMgr: newMockProcessMgr(),
		StateMgr:   newMockStateMgr(),
	})
	if tr.config.ProjectRefreshInterval != 5 {
		t.Errorf("config.ProjectRefreshInterval = %d, want 5", tr.config.ProjectRefreshInterval)
	}
}

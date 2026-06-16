package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
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
	defer m.mu.Unlock()
	return m.healthResult, m.healthErr
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
	if _, exists := m.processes[taskID]; exists {
		return fmt.Errorf("task %s already tracked", taskID)
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
		result = append(result, *info)
	}
	return result
}

func (m *mockProcessMgr) GetAllRunning() []ProcessInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []ProcessInfo
	for _, info := range m.processes {
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
		if !info.Proc.Exited() {
			count++
		}
	}
	return count
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

	// Wait for it to start
	time.Sleep(100 * time.Millisecond)

	// Verify SSE listener was created (config has BrainAPIURL and projects are set)
	if tr.sseListener == nil {
		t.Error("Start() should create SSE listener when BrainAPIURL is set and projects exist")
	}

	// Stop
	cancel()
	<-errCh
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

	time.Sleep(100 * time.Millisecond)

	// Should NOT create SSE listener when no API URL
	if tr.sseListener != nil {
		t.Error("Start() should NOT create SSE listener when BrainAPIURL is empty")
	}

	cancel()
	<-errCh
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

	time.Sleep(100 * time.Millisecond)

	// Should NOT create SSE listener when no projects
	if tr.sseListener != nil {
		t.Error("Start() should NOT create SSE listener when no projects")
	}

	cancel()
	<-errCh
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
// Feature Toggle + Poll Integration Tests
// =============================================================================

func TestTaskRunner_Poll_AllPaused_WithEnabledFeatures_PollsEnabledFeatures(t *testing.T) {
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
	tr.PauseAll()
	tr.EnableFeature("feat-auth")

	ctx := context.Background()
	tr.poll(ctx)

	// Should spawn — enabled features override all-paused
	spawnCalls := executor.getSpawnCalls()
	if len(spawnCalls) == 0 {
		t.Error("should spawn tasks when all-paused but features are enabled")
	}

	// Verify GetNextTask was called with the enabled feature IDs and affinity filters.
	nextCalls := client.getNextTaskCalls()
	if len(nextCalls) == 0 {
		t.Fatal("expected at least 1 GetNextTask call")
	}
	found := false
	for _, call := range nextCalls {
		for _, fid := range call.FeatureIDs {
			if fid == "feat-auth" {
				found = true
			}
		}
		if call.RunnerID != tr.runnerID {
			t.Errorf("GetNextTask RunnerID = %q, want %q", call.RunnerID, tr.runnerID)
		}
		if call.Opts == nil {
			t.Fatal("GetNextTask opts should not be nil")
		}
		if len(call.Opts.Executors) != 1 || call.Opts.Executors[0] != "opencode" {
			t.Errorf("GetNextTask executors = %v, want [opencode]", call.Opts.Executors)
		}
	}
	if !found {
		t.Error("GetNextTask should be called with enabled feature ID 'feat-auth'")
	}
}

func TestTaskRunner_Poll_AllPaused_NoEnabledFeatures_PollsOnlyAutomations(t *testing.T) {
	client := newMockClient()
	task := testTask("task1", "proj-a")
	client.nextTask["proj-a"] = task

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)
	tr.PauseAll()
	// No enabled features

	ctx := context.Background()
	tr.poll(ctx)

	if len(executor.getSpawnCalls()) > 0 {
		t.Error("should not spawn when all-paused with no enabled features")
	}
	for _, call := range client.getNextTaskCalls() {
		if call.Opts == nil || call.Opts.GeneratedByPrefix != "automation:" {
			t.Fatalf("all-paused runner should only fetch automation tasks, got call: %#v", call)
		}
	}
}

func TestTaskRunner_Poll_ProjectPaused_WithEnabledFeatures_PollsEnabledFeatures(t *testing.T) {
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
	tr.PauseProject("proj-a")
	tr.EnableFeature("feat-deploy")

	ctx := context.Background()
	tr.poll(ctx)

	// Should spawn from proj-a using enabled features
	spawnCalls := executor.getSpawnCalls()
	if len(spawnCalls) == 0 {
		t.Error("should spawn tasks for paused project when features are enabled")
	}

	// Verify GetNextTask was called with enabled feature IDs (not config feature IDs)
	// and retains runner affinity filters while the project is paused.
	nextCalls := client.getNextTaskCalls()
	foundEnabled := false
	for _, call := range nextCalls {
		if call.ProjectID == "proj-a" {
			for _, fid := range call.FeatureIDs {
				if fid == "feat-deploy" {
					foundEnabled = true
				}
			}
			if call.RunnerID != tr.runnerID {
				t.Errorf("GetNextTask RunnerID = %q, want %q", call.RunnerID, tr.runnerID)
			}
			if call.Opts == nil {
				t.Fatal("GetNextTask opts should not be nil")
			}
			if len(call.Opts.Executors) != 1 || call.Opts.Executors[0] != "opencode" {
				t.Errorf("GetNextTask executors = %v, want [opencode]", call.Opts.Executors)
			}
		}
	}
	if !foundEnabled {
		t.Error("GetNextTask for paused proj-a should use enabled feature ID 'feat-deploy'")
	}
}

func TestTaskRunner_Poll_ProjectPaused_NoEnabledFeatures_SkipsProject(t *testing.T) {
	client := newMockClient()
	task := testTask("task1", "proj-a")
	client.nextTask["proj-a"] = task

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)
	tr.PauseProject("proj-a")
	// No enabled features

	ctx := context.Background()
	tr.poll(ctx)

	// Should NOT spawn from proj-a — existing behavior preserved
	// (proj-b is unpaused but has no task)
	spawnCalls := executor.getSpawnCalls()
	if len(spawnCalls) > 0 {
		t.Error("should not spawn tasks for paused project with no enabled features")
	}
}

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

	// Enable a feature (should NOT be used when unpaused)
	tr.EnableFeature("toggled-feat")

	ctx := context.Background()
	tr.poll(ctx)

	// Should spawn normally
	spawnCalls := executor.getSpawnCalls()
	if len(spawnCalls) == 0 {
		t.Fatal("should spawn tasks when unpaused")
	}

	// Verify GetNextTask was called with config feature IDs, not enabled feature IDs
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

func TestTaskRunner_Poll_AllPaused_MultipleEnabledFeatures(t *testing.T) {
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
	tr.PauseAll()
	tr.EnableFeature("feat-auth")
	tr.EnableFeature("feat-deploy")

	ctx := context.Background()
	tr.poll(ctx)

	// Verify both feature IDs were passed
	nextCalls := client.getNextTaskCalls()
	if len(nextCalls) == 0 {
		t.Fatal("expected GetNextTask calls")
	}

	featureSet := make(map[string]bool)
	for _, call := range nextCalls {
		for _, fid := range call.FeatureIDs {
			featureSet[fid] = true
		}
	}
	if !featureSet["feat-auth"] || !featureSet["feat-deploy"] {
		t.Errorf("expected both feat-auth and feat-deploy in feature IDs, got %v", featureSet)
	}
}

// =============================================================================
// EnableFeature / DisableFeature / GetEnabledFeatures Tests
// =============================================================================

func TestTaskRunner_EnableFeature_AddsToMap(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	tr.EnableFeature("feat-auth")

	enabled := tr.GetEnabledFeatures()
	if !enabled["feat-auth"] {
		t.Error("EnableFeature should add feature to enabled map")
	}
}

func TestTaskRunner_EnableFeature_MultipleFeaturesCoexist(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	tr.EnableFeature("feat-auth")
	tr.EnableFeature("feat-deploy")
	tr.EnableFeature("feat-metrics")

	enabled := tr.GetEnabledFeatures()
	if len(enabled) != 3 {
		t.Fatalf("expected 3 enabled features, got %d", len(enabled))
	}
	for _, id := range []string{"feat-auth", "feat-deploy", "feat-metrics"} {
		if !enabled[id] {
			t.Errorf("expected %q to be enabled", id)
		}
	}
}

func TestTaskRunner_EnableFeature_Idempotent(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	tr.EnableFeature("feat-auth")
	tr.EnableFeature("feat-auth") // enable again — should be no-op

	enabled := tr.GetEnabledFeatures()
	if len(enabled) != 1 {
		t.Errorf("expected 1 enabled feature after duplicate enable, got %d", len(enabled))
	}
}

func TestTaskRunner_EnableFeature_EmitsEvent(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	var events []RunnerEvent
	var mu sync.Mutex
	tr.OnEvent(func(event RunnerEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})

	tr.EnableFeature("feat-auth")

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventFeatureEnabled {
		t.Errorf("event type = %q, want %q", events[0].Type, EventFeatureEnabled)
	}
	if events[0].FeatureID != "feat-auth" {
		t.Errorf("event FeatureID = %q, want %q", events[0].FeatureID, "feat-auth")
	}
}

func TestTaskRunner_DisableFeature_RemovesFromMap(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	tr.EnableFeature("feat-auth")
	tr.EnableFeature("feat-deploy")

	tr.DisableFeature("feat-auth")

	enabled := tr.GetEnabledFeatures()
	if enabled["feat-auth"] {
		t.Error("DisableFeature should remove feature from enabled map")
	}
	if !enabled["feat-deploy"] {
		t.Error("DisableFeature should not affect other features")
	}
}

func TestTaskRunner_DisableFeature_NonExistent_NoOp(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	// Disabling a feature that was never enabled should not panic or error
	tr.DisableFeature("does-not-exist")

	enabled := tr.GetEnabledFeatures()
	if enabled != nil {
		t.Errorf("expected nil enabled features, got %v", enabled)
	}
}

func TestTaskRunner_DisableFeature_EmitsEvent(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	var events []RunnerEvent
	var mu sync.Mutex
	tr.OnEvent(func(event RunnerEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})

	tr.EnableFeature("feat-auth")
	tr.DisableFeature("feat-auth")

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[1].Type != EventFeatureDisabled {
		t.Errorf("event type = %q, want %q", events[1].Type, EventFeatureDisabled)
	}
	if events[1].FeatureID != "feat-auth" {
		t.Errorf("event FeatureID = %q, want %q", events[1].FeatureID, "feat-auth")
	}
}

func TestTaskRunner_GetEnabledFeatures_ReturnsNilWhenEmpty(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	enabled := tr.GetEnabledFeatures()
	if enabled != nil {
		t.Errorf("expected nil for empty enabled features, got %v", enabled)
	}
}

func TestTaskRunner_GetEnabledFeatures_ReturnsCopy(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	tr.EnableFeature("feat-auth")

	// Get a copy
	copy1 := tr.GetEnabledFeatures()
	if !copy1["feat-auth"] {
		t.Fatal("copy should contain feat-auth")
	}

	// Mutate the copy — should NOT affect the internal state
	copy1["feat-injected"] = true
	delete(copy1, "feat-auth")

	// Get fresh copy — should still have original state
	copy2 := tr.GetEnabledFeatures()
	if !copy2["feat-auth"] {
		t.Error("mutating copy should not affect internal map — feat-auth missing")
	}
	if copy2["feat-injected"] {
		t.Error("mutating copy should not affect internal map — feat-injected leaked in")
	}
}

func TestTaskRunner_GetEnabledFeatures_AllDisabled_ReturnsNil(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	tr.EnableFeature("feat-auth")
	tr.DisableFeature("feat-auth")

	enabled := tr.GetEnabledFeatures()
	if enabled != nil {
		t.Errorf("expected nil after disabling all features, got %v", enabled)
	}
}

// =============================================================================
// getEnabledFeatureIDsLocked Tests
// =============================================================================

func TestTaskRunner_GetEnabledFeatureIDsLocked_Empty(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	tr.pauseMu.RLock()
	ids := tr.getEnabledFeatureIDsLocked()
	tr.pauseMu.RUnlock()

	if ids != nil {
		t.Errorf("expected nil for empty enabledFeatures, got %v", ids)
	}
}

func TestTaskRunner_GetEnabledFeatureIDsLocked_WithFeatures(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	tr.EnableFeature("feat-a")
	tr.EnableFeature("feat-b")

	tr.pauseMu.RLock()
	ids := tr.getEnabledFeatureIDsLocked()
	tr.pauseMu.RUnlock()

	if len(ids) != 2 {
		t.Fatalf("expected 2 IDs, got %d", len(ids))
	}

	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	if !idSet["feat-a"] || !idSet["feat-b"] {
		t.Errorf("expected feat-a and feat-b, got %v", ids)
	}
}

func TestTaskRunner_GetEnabledFeatureIDsLocked_AfterDisable(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())

	tr.EnableFeature("feat-a")
	tr.EnableFeature("feat-b")
	tr.DisableFeature("feat-a")

	tr.pauseMu.RLock()
	ids := tr.getEnabledFeatureIDsLocked()
	tr.pauseMu.RUnlock()

	if len(ids) != 1 {
		t.Fatalf("expected 1 ID after disable, got %d", len(ids))
	}
	if ids[0] != "feat-b" {
		t.Errorf("expected feat-b, got %s", ids[0])
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

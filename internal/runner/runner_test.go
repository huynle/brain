package runner

import (
	"context"
	"fmt"
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
var _ TaskExecutor = (*Executor)(nil)
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

	nextTask    map[string]*types.ResolvedTask
	nextTaskErr error

	claimResult ClaimResult
	claimErr    error
	claimCalls  []claimCall

	releaseErr   error
	releaseCalls []releaseCall

	updateStatusErr   error
	updateStatusCalls []updateStatusCall

	appendErr   error
	appendCalls []appendCall
}

type claimCall struct {
	ProjectID string
	TaskID    string
	RunnerID  string
}

type releaseCall struct {
	ProjectID string
	TaskID    string
}

type updateStatusCall struct {
	TaskPath string
	Status   string
}

type appendCall struct {
	TaskPath string
	Content  string
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

func (m *mockClient) GetReadyTasks(ctx context.Context, projectID string) ([]types.ResolvedTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readyTasksErr != nil {
		return nil, m.readyTasksErr
	}
	return m.readyTasks[projectID], nil
}

func (m *mockClient) GetNextTask(ctx context.Context, projectID string) (*types.ResolvedTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.nextTaskErr != nil {
		return nil, m.nextTaskErr
	}
	return m.nextTask[projectID], nil
}

func (m *mockClient) ClaimTask(ctx context.Context, projectID, taskID, runnerID string) (ClaimResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.claimCalls = append(m.claimCalls, claimCall{projectID, taskID, runnerID})
	result := m.claimResult
	result.TaskID = taskID
	return result, m.claimErr
}

func (m *mockClient) ReleaseTask(ctx context.Context, projectID, taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseCalls = append(m.releaseCalls, releaseCall{projectID, taskID})
	return m.releaseErr
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

// =============================================================================
// Mock Executor
// =============================================================================

type mockExecutor struct {
	mu sync.Mutex

	buildPromptResult string
	resolveWorkdir    string
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

func (m *mockExecutor) ResolveWorkdir(task *types.ResolvedTask) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.resolveWorkdir
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
		Projects:   []string{"proj-a", "proj-b"},
		Config:     testRunnerConfig(),
		Mode:       ExecutionModeBackground,
		Client:     client,
		Executor:   executor,
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

func TestNewTaskRunner_UniqueIDs(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr1 := newTestRunner(client, executor, processMgr, stateMgr)
	tr2 := newTestRunner(client, executor, processMgr, stateMgr)

	if tr1.runnerID == tr2.runnerID {
		t.Errorf("two runners should have different IDs: %q == %q", tr1.runnerID, tr2.runnerID)
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
	if tr.mode != ExecutionModeBackground {
		t.Errorf("mode = %q, want %q", tr.mode, ExecutionModeBackground)
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

	if tr.mode != ExecutionModeBackground {
		t.Errorf("default mode = %q, want %q", tr.mode, ExecutionModeBackground)
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
		Mode:       ExecutionModeBackground,
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
	client.claimResult = ClaimResult{Success: false, ClaimedBy: "other-runner"}

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	task := testTask("task1", "proj-a")
	ctx := context.Background()

	err := tr.claimAndSpawn(ctx, task, "proj-a")
	if err == nil {
		t.Error("claimAndSpawn should return error when claim fails")
	}

	// Should not spawn
	if len(executor.getSpawnCalls()) > 0 {
		t.Error("should not spawn when claim fails")
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

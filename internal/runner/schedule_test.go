package runner

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// shouldTrigger Tests
// =============================================================================

func TestShouldTrigger_NextRunPast(t *testing.T) {
	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	nextRun := "2026-03-22T10:15:00Z" // 15 minutes ago

	if !shouldTrigger("*/15 * * * *", nextRun, now) {
		t.Error("should trigger when next_run is in the past")
	}
}

func TestShouldTrigger_NextRunFuture(t *testing.T) {
	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	nextRun := "2026-03-22T10:45:00Z" // 15 minutes from now

	if shouldTrigger("*/15 * * * *", nextRun, now) {
		t.Error("should NOT trigger when next_run is in the future")
	}
}

func TestShouldTrigger_NoNextRun_CronMatches(t *testing.T) {
	// At 10:30, */15 matches (30 is divisible by 15)
	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)

	if !shouldTrigger("*/15 * * * *", "", now) {
		t.Error("should trigger when no next_run and cron matches current time")
	}
}

func TestShouldTrigger_NoNextRun_CronDoesNotMatch(t *testing.T) {
	// At 10:31, */15 does NOT match
	now := time.Date(2026, 3, 22, 10, 31, 0, 0, time.UTC)

	if shouldTrigger("*/15 * * * *", "", now) {
		t.Error("should NOT trigger when no next_run and cron does not match")
	}
}

func TestShouldTrigger_NoSchedule(t *testing.T) {
	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)

	if shouldTrigger("", "", now) {
		t.Error("should NOT trigger when schedule is empty")
	}
}

func TestShouldTrigger_InvalidSchedule(t *testing.T) {
	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)

	if shouldTrigger("invalid cron", "", now) {
		t.Error("should NOT trigger when schedule is invalid")
	}
}

func TestShouldTrigger_InvalidNextRun(t *testing.T) {
	// Invalid next_run should fall back to cron matching
	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)

	if !shouldTrigger("*/15 * * * *", "not-a-date", now) {
		t.Error("should fall back to cron matching when next_run is invalid")
	}
}

// =============================================================================
// getNextRun Tests
// =============================================================================

func TestGetNextRun(t *testing.T) {
	after := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	next, err := getNextRun("*/15 * * * *", after)
	if err != nil {
		t.Fatalf("getNextRun error: %v", err)
	}

	expected := time.Date(2026, 3, 22, 10, 45, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("getNextRun = %v, want %v", next, expected)
	}
}

func TestGetNextRun_InvalidSchedule(t *testing.T) {
	_, err := getNextRun("invalid", time.Now())
	if err == nil {
		t.Error("getNextRun should return error for invalid schedule")
	}
}

// =============================================================================
// checkScheduledTasks Integration Tests
// =============================================================================

// schedTestRunner creates a TaskRunner with a mock client that supports GetAllTasks.
func schedTestRunner() (*TaskRunner, *schedMockClient, *mockProcessMgr) {
	client := newSchedMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a", "proj-b"},
		Config:     testRunnerConfig(),
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	return tr, client, processMgr
}

func TestCheckScheduledTasks_TriggersActiveTask(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:       "sched-1",
			Path:     "projects/proj-a/task/sched-1.md",
			Title:    "Scheduled Task",
			Status:   "active",
			Schedule: "*/15 * * * *",
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	// Check status was reset to pending via UpdateTaskStatus
	statusCalls := client.getUpdateStatusCalls()
	foundStatusReset := false
	for _, c := range statusCalls {
		if c.Path == "projects/proj-a/task/sched-1.md" && c.Status == "pending" {
			foundStatusReset = true
		}
	}
	if !foundStatusReset {
		t.Error("should reset scheduled task status to pending")
	}

	// Check next_run was set via UpdateMetadata
	metaCalls := client.getUpdateMetadataCalls()
	foundNextRun := false
	for _, c := range metaCalls {
		if c.Path == "projects/proj-a/task/sched-1.md" {
			if _, ok := c.Fields["next_run"]; ok {
				foundNextRun = true
			}
		}
	}
	if !foundNextRun {
		t.Error("should advance next_run for triggered task")
	}
}

func TestCheckScheduledTasks_SkipsDisabled(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	disabled := false
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:              "sched-1",
			Path:            "projects/proj-a/task/sched-1.md",
			Title:           "Disabled Scheduled Task",
			Status:          "active",
			Schedule:        "*/15 * * * *",
			ScheduleEnabled: &disabled,
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	calls := client.getUpdateMetadataCalls()
	if len(calls) > 0 {
		t.Error("should NOT trigger disabled scheduled task")
	}
}

func TestCheckScheduledTasks_SkipsPending(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:       "sched-1",
			Path:     "projects/proj-a/task/sched-1.md",
			Title:    "Pending Scheduled Task",
			Status:   "pending",
			Schedule: "*/15 * * * *",
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	calls := client.getUpdateMetadataCalls()
	if len(calls) > 0 {
		t.Error("should NOT trigger pending scheduled task")
	}
}

func TestCheckScheduledTasks_SkipsInProgress(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:       "sched-1",
			Path:     "projects/proj-a/task/sched-1.md",
			Title:    "In-Progress Scheduled Task",
			Status:   "in_progress",
			Schedule: "*/15 * * * *",
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	calls := client.getUpdateMetadataCalls()
	if len(calls) > 0 {
		t.Error("should NOT trigger in_progress scheduled task")
	}
}

func TestCheckScheduledTasks_OverlapGuard(t *testing.T) {
	tr, client, processMgr := schedTestRunner()

	// Add the task to process manager (simulating it's already running)
	proc := newMockProcess(100)
	processMgr.Add("sched-1", testRunningTask("sched-1"), proc)

	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:       "sched-1",
			Path:     "projects/proj-a/task/sched-1.md",
			Title:    "Running Scheduled Task",
			Status:   "active",
			Schedule: "*/15 * * * *",
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	calls := client.getUpdateMetadataCalls()
	if len(calls) > 0 {
		t.Error("should NOT trigger task that is already tracked in process manager (overlap guard)")
	}
}

func TestCheckScheduledTasks_TriggersCompletedTask(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:       "sched-1",
			Path:     "projects/proj-a/task/sched-1.md",
			Title:    "Completed Scheduled Task",
			Status:   "completed",
			Schedule: "*/15 * * * *",
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	statusCalls := client.getUpdateStatusCalls()
	foundStatusReset := false
	for _, c := range statusCalls {
		if c.Status == "pending" {
			foundStatusReset = true
		}
	}
	if !foundStatusReset {
		t.Error("should trigger completed scheduled task")
	}
}

func TestCheckScheduledTasks_TriggersBlockedTask(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:       "sched-1",
			Path:     "projects/proj-a/task/sched-1.md",
			Title:    "Blocked Scheduled Task",
			Status:   "blocked",
			Schedule: "*/15 * * * *",
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	statusCalls := client.getUpdateStatusCalls()
	foundStatusReset := false
	for _, c := range statusCalls {
		if c.Status == "pending" {
			foundStatusReset = true
		}
	}
	if !foundStatusReset {
		t.Error("should trigger blocked scheduled task")
	}
}

func TestCheckScheduledTasks_RateLimiting(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:       "sched-1",
			Path:     "projects/proj-a/task/sched-1.md",
			Title:    "Scheduled Task",
			Status:   "active",
			Schedule: "*/15 * * * *",
		},
	}

	ctx := context.Background()

	// First call should trigger
	tr.checkScheduledTasks(ctx, now)
	calls1 := client.getUpdateMetadataCalls()
	if len(calls1) == 0 {
		t.Fatal("first call should trigger")
	}

	// Second call immediately after should be rate-limited (same time)
	client.clearUpdateMetadataCalls()
	tr.checkScheduledTasks(ctx, now)
	calls2 := client.getUpdateMetadataCalls()
	if len(calls2) > 0 {
		t.Error("second call should be rate-limited")
	}
}

func TestCheckScheduledTasks_NoScheduleField(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:     "task-1",
			Path:   "projects/proj-a/task/task-1.md",
			Title:  "Regular Task",
			Status: "active",
			// No Schedule field
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	calls := client.getUpdateMetadataCalls()
	if len(calls) > 0 {
		t.Error("should NOT trigger task without schedule")
	}
}

// =============================================================================
// schedMockClient — extends mockClient with GetAllTasks + UpdateMetadata tracking
// =============================================================================

type updateMetadataCall struct {
	Path   string
	Fields map[string]interface{}
}

type schedStatusCall struct {
	Path   string
	Status string
}

type schedMockClient struct {
	mu2                 sync.Mutex
	allTasks            map[string][]types.ResolvedTask
	allTasksErr         error
	updateMetadataCalls []updateMetadataCall
	updateStatusCalls   []schedStatusCall

	// Embed base mock for all other Client methods
	healthResult APIHealth
	healthErr    error
	projects     []string
	projectsErr  error
	readyTasks   map[string][]types.ResolvedTask
	nextTask     map[string]*types.ResolvedTask
	claimResult  ClaimResult
}

func newSchedMockClient() *schedMockClient {
	return &schedMockClient{
		healthResult: APIHealth{Status: "ok"},
		claimResult:  ClaimResult{Success: true},
		readyTasks:   make(map[string][]types.ResolvedTask),
		nextTask:     make(map[string]*types.ResolvedTask),
		allTasks:     make(map[string][]types.ResolvedTask),
	}
}

// Client interface implementation
func (m *schedMockClient) CheckHealth(ctx context.Context) (APIHealth, error) {
	return m.healthResult, m.healthErr
}
func (m *schedMockClient) ListProjects(ctx context.Context) ([]string, error) {
	return m.projects, m.projectsErr
}
func (m *schedMockClient) GetReadyTasks(ctx context.Context, projectID string, featureIDs ...string) ([]types.ResolvedTask, error) {
	return m.readyTasks[projectID], nil
}
func (m *schedMockClient) GetNextTask(ctx context.Context, projectID string, featureIDs ...string) (*types.ResolvedTask, error) {
	return m.nextTask[projectID], nil
}
func (m *schedMockClient) ClaimTask(ctx context.Context, projectID, taskID, runnerID string) (ClaimResult, error) {
	return m.claimResult, nil
}
func (m *schedMockClient) ReleaseTask(ctx context.Context, projectID, taskID string) error {
	return nil
}
func (m *schedMockClient) UpdateTaskStatus(ctx context.Context, taskPath, status string) error {
	m.mu2.Lock()
	defer m.mu2.Unlock()
	m.updateStatusCalls = append(m.updateStatusCalls, schedStatusCall{Path: taskPath, Status: status})
	return nil
}

func (m *schedMockClient) getUpdateStatusCalls() []schedStatusCall {
	m.mu2.Lock()
	defer m.mu2.Unlock()
	result := make([]schedStatusCall, len(m.updateStatusCalls))
	copy(result, m.updateStatusCalls)
	return result
}
func (m *schedMockClient) AppendToTask(ctx context.Context, taskPath, content string) error {
	return nil
}
func (m *schedMockClient) UpdateEntry(ctx context.Context, entryPath string, updates map[string]interface{}) (*types.BrainEntry, error) {
	return &types.BrainEntry{Path: entryPath}, nil
}
func (m *schedMockClient) UpdateMetadata(ctx context.Context, entryPath string, fields map[string]interface{}) error {
	m.mu2.Lock()
	defer m.mu2.Unlock()
	m.updateMetadataCalls = append(m.updateMetadataCalls, updateMetadataCall{
		Path:   entryPath,
		Fields: copyMap(fields),
	})
	return nil
}
func (m *schedMockClient) GetAllTasks(ctx context.Context, projectID string) ([]types.ResolvedTask, error) {
	m.mu2.Lock()
	defer m.mu2.Unlock()
	if m.allTasksErr != nil {
		return nil, m.allTasksErr
	}
	return m.allTasks[projectID], nil
}

func (m *schedMockClient) getUpdateMetadataCalls() []updateMetadataCall {
	m.mu2.Lock()
	defer m.mu2.Unlock()
	result := make([]updateMetadataCall, len(m.updateMetadataCalls))
	copy(result, m.updateMetadataCalls)
	return result
}

func (m *schedMockClient) clearUpdateMetadataCalls() {
	m.mu2.Lock()
	defer m.mu2.Unlock()
	m.updateMetadataCalls = nil
}

func copyMap(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// Verify interface compliance
var _ Client = (*schedMockClient)(nil)

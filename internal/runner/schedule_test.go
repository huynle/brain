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

	// GetEntry support for run finalization tests
	getEntryResult map[string]*types.BrainEntry
	getEntryErr    error

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
func (m *schedMockClient) GetEntry(ctx context.Context, entryPath string) (*types.BrainEntry, error) {
	m.mu2.Lock()
	defer m.mu2.Unlock()
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

// =============================================================================
// Feature Schedule Gate Direct-Complete Tests
// =============================================================================

func TestProcessScheduledTask_FeatureScheduleGate_DirectComplete(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:            "gate-1",
			Path:          "projects/proj-a/task/gate-1.md",
			Title:         "Feature Schedule: my-feature",
			Status:        "active",
			Schedule:      "*/15 * * * *",
			GeneratedKind: "feature_schedule",
			FeatureID:     "my-feature",
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	// Should set status to "completed", NOT "pending"
	statusCalls := client.getUpdateStatusCalls()
	foundCompleted := false
	foundPending := false
	for _, c := range statusCalls {
		if c.Path == "projects/proj-a/task/gate-1.md" {
			if c.Status == "completed" {
				foundCompleted = true
			}
			if c.Status == "pending" {
				foundPending = true
			}
		}
	}
	if !foundCompleted {
		t.Error("feature_schedule gate should be directly set to 'completed'")
	}
	if foundPending {
		t.Error("feature_schedule gate should NOT be set to 'pending'")
	}
}

func TestProcessScheduledTask_FeatureScheduleGate_RunRecordedAsCompleted(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:            "gate-1",
			Path:          "projects/proj-a/task/gate-1.md",
			Title:         "Feature Schedule: my-feature",
			Status:        "active",
			Schedule:      "*/15 * * * *",
			GeneratedKind: "feature_schedule",
			FeatureID:     "my-feature",
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	// Check that the run was recorded with status "completed" directly
	metaCalls := client.getUpdateMetadataCalls()
	var runsMeta []interface{}
	for _, c := range metaCalls {
		if c.Path == "projects/proj-a/task/gate-1.md" {
			if runs, ok := c.Fields["runs"]; ok {
				runsMeta = runs.([]interface{})
			}
		}
	}
	if runsMeta == nil {
		t.Fatal("expected runs array in metadata update")
	}

	// The last run should have status "completed"
	lastRun := runsMeta[len(runsMeta)-1].(map[string]interface{})
	if lastRun["status"] != "completed" {
		t.Errorf("run status = %v, want 'completed'", lastRun["status"])
	}
	if lastRun["completed"] == nil || lastRun["completed"] == "" {
		t.Error("run should have a 'completed' timestamp")
	}
}

func TestProcessScheduledTask_FeatureScheduleGate_DisablesSchedule(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:            "gate-1",
			Path:          "projects/proj-a/task/gate-1.md",
			Title:         "Feature Schedule: my-feature",
			Status:        "active",
			Schedule:      "*/15 * * * *",
			GeneratedKind: "feature_schedule",
			FeatureID:     "my-feature",
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	// Check that schedule_enabled was set to false
	metaCalls := client.getUpdateMetadataCalls()
	foundScheduleDisabled := false
	for _, c := range metaCalls {
		if c.Path == "projects/proj-a/task/gate-1.md" {
			if enabled, ok := c.Fields["schedule_enabled"]; ok {
				if enabled == false {
					foundScheduleDisabled = true
				}
			}
		}
	}
	if !foundScheduleDisabled {
		t.Error("feature_schedule gate should disable schedule_enabled after completion")
	}
}

func TestProcessScheduledTask_RegularTask_StillSetsPending(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:       "regular-1",
			Path:     "projects/proj-a/task/regular-1.md",
			Title:    "Regular Scheduled Task",
			Status:   "active",
			Schedule: "*/15 * * * *",
			// No GeneratedKind — regular task
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	// Regular tasks should be set to "pending", NOT "completed"
	statusCalls := client.getUpdateStatusCalls()
	foundPending := false
	foundCompleted := false
	for _, c := range statusCalls {
		if c.Path == "projects/proj-a/task/regular-1.md" {
			if c.Status == "pending" {
				foundPending = true
			}
			if c.Status == "completed" {
				foundCompleted = true
			}
		}
	}
	if !foundPending {
		t.Error("regular task should be set to 'pending'")
	}
	if foundCompleted {
		t.Error("regular task should NOT be set to 'completed'")
	}
}

func TestProcessScheduledTask_FeatureScheduleGate_EmitsCompletedEvent(t *testing.T) {
	tr, client, _ := schedTestRunner()

	// Register event handler to capture events
	var events []RunnerEvent
	tr.OnEvent(func(evt RunnerEvent) {
		events = append(events, evt)
	})

	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:            "gate-1",
			Path:          "projects/proj-a/task/gate-1.md",
			Title:         "Feature Schedule: my-feature",
			Status:        "active",
			Schedule:      "*/15 * * * *",
			GeneratedKind: "feature_schedule",
			FeatureID:     "my-feature",
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	// Check that a completed event was emitted
	foundCompletedEvent := false
	for _, evt := range events {
		if evt.Type == EventTaskCompleted && evt.TaskID == "gate-1" {
			foundCompletedEvent = true
		}
	}
	if !foundCompletedEvent {
		t.Error("feature_schedule gate should emit a completed event")
	}
}

// =============================================================================
// Downstream Unblocking Tests — Feature Schedule Gate Lifecycle
// =============================================================================

func TestProcessFeatureScheduleGate_DownstreamTasksBecomeReady(t *testing.T) {
	// This test simulates the full lifecycle:
	// 1. Feature has 3 tasks + 1 gate, all tasks depend_on the gate
	// 2. Gate triggers at scheduled time → gate set to completed
	// 3. After gate completes, downstream tasks should now be classifiable as "ready"
	//    (because their sole dependency — the gate — is now completed)

	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)

	// Gate task (feature_schedule generated)
	gateTask := types.ResolvedTask{
		ID:            "gate-deploy",
		Path:          "projects/proj-a/task/gate-deploy.md",
		Title:         "Feature Schedule: deploy-v3",
		Status:        "active",
		Schedule:      "*/15 * * * *",
		GeneratedKind: "feature_schedule",
		FeatureID:     "deploy-v3",
	}

	// Three downstream tasks that depend on the gate
	taskAlpha := types.ResolvedTask{
		ID:             "task-alpha",
		Path:           "projects/proj-a/task/task-alpha.md",
		Title:          "Deploy Service",
		Status:         "pending",
		FeatureID:      "deploy-v3",
		DependsOn:      []string{"gate-deploy"},
		WaitingOn:      []string{"gate-deploy"},
		Classification: "waiting",
	}
	taskBeta := types.ResolvedTask{
		ID:             "task-beta",
		Path:           "projects/proj-a/task/task-beta.md",
		Title:          "Run Migrations",
		Status:         "pending",
		FeatureID:      "deploy-v3",
		DependsOn:      []string{"gate-deploy"},
		WaitingOn:      []string{"gate-deploy"},
		Classification: "waiting",
	}
	taskGamma := types.ResolvedTask{
		ID:             "task-gamma",
		Path:           "projects/proj-a/task/task-gamma.md",
		Title:          "Verify Health",
		Status:         "pending",
		FeatureID:      "deploy-v3",
		DependsOn:      []string{"gate-deploy"},
		WaitingOn:      []string{"gate-deploy"},
		Classification: "waiting",
	}

	client.allTasks["proj-a"] = []types.ResolvedTask{gateTask, taskAlpha, taskBeta, taskGamma}

	ctx := context.Background()

	// Trigger the scheduled tasks check — should directly complete the gate
	tr.checkScheduledTasks(ctx, now)

	// Verify gate was set to completed
	statusCalls := client.getUpdateStatusCalls()
	gateCompleted := false
	for _, c := range statusCalls {
		if c.Path == "projects/proj-a/task/gate-deploy.md" && c.Status == "completed" {
			gateCompleted = true
		}
	}
	if !gateCompleted {
		t.Fatal("gate task should have been set to 'completed'")
	}

	// Verify NO status changes were made to the downstream tasks (they remain pending,
	// waiting for the next poll cycle to re-evaluate via dependency resolution)
	for _, c := range statusCalls {
		if c.Path == "projects/proj-a/task/task-alpha.md" ||
			c.Path == "projects/proj-a/task/task-beta.md" ||
			c.Path == "projects/proj-a/task/task-gamma.md" {
			t.Errorf("downstream task %s should NOT have its status changed by gate completion directly", c.Path)
		}
	}

	// Verify schedule was disabled on the gate
	metaCalls := client.getUpdateMetadataCalls()
	gateScheduleDisabled := false
	for _, c := range metaCalls {
		if c.Path == "projects/proj-a/task/gate-deploy.md" {
			if enabled, ok := c.Fields["schedule_enabled"]; ok && enabled == false {
				gateScheduleDisabled = true
			}
		}
	}
	if !gateScheduleDisabled {
		t.Error("gate schedule_enabled should be set to false after completion")
	}
}

func TestProcessFeatureScheduleGate_RunOnceAtGate_ScheduleDisabledAfterCompletion(t *testing.T) {
	// Specifically tests that a gate created from feature_run_once_at
	// (which has a cron schedule set) gets schedule_enabled=false after firing once.
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	schedEnabled := true
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:              "gate-once",
			Path:            "projects/proj-a/task/gate-once.md",
			Title:           "Feature Schedule: one-time-deploy",
			Status:          "active",
			Schedule:        "0 0 1 6 *", // June 1st at midnight
			ScheduleEnabled: &schedEnabled,
			GeneratedKind:   "feature_schedule",
			GeneratedKey:    "feature-schedule:one-time-deploy",
			FeatureID:       "one-time-deploy",
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	// Verify gate was completed
	statusCalls := client.getUpdateStatusCalls()
	gateCompleted := false
	for _, c := range statusCalls {
		if c.Path == "projects/proj-a/task/gate-once.md" && c.Status == "completed" {
			gateCompleted = true
		}
	}
	if !gateCompleted {
		t.Fatal("run_once_at gate should be completed")
	}

	// Verify schedule_enabled was set to false (preventing re-trigger)
	metaCalls := client.getUpdateMetadataCalls()
	foundDisabled := false
	for _, c := range metaCalls {
		if c.Path == "projects/proj-a/task/gate-once.md" {
			if enabled, ok := c.Fields["schedule_enabled"]; ok && enabled == false {
				foundDisabled = true
			}
		}
	}
	if !foundDisabled {
		t.Error("run_once_at gate should disable schedule after completion")
	}
}

// =============================================================================
// latestInProgressRunID Tests
// =============================================================================

func TestLatestInProgressRunID_Found(t *testing.T) {
	runs := []types.CronRun{
		{RunID: "run-001", Status: "completed"},
		{RunID: "run-002", Status: "in_progress"},
		{RunID: "run-003", Status: "completed"},
	}

	got := latestInProgressRunID(runs)
	if got != "run-002" {
		t.Errorf("latestInProgressRunID = %q, want %q", got, "run-002")
	}
}

func TestLatestInProgressRunID_Empty(t *testing.T) {
	got := latestInProgressRunID(nil)
	if got != "" {
		t.Errorf("latestInProgressRunID(nil) = %q, want empty", got)
	}

	got = latestInProgressRunID([]types.CronRun{})
	if got != "" {
		t.Errorf("latestInProgressRunID([]) = %q, want empty", got)
	}
}

func TestLatestInProgressRunID_NoInProgress(t *testing.T) {
	runs := []types.CronRun{
		{RunID: "run-001", Status: "completed"},
		{RunID: "run-002", Status: "failed"},
		{RunID: "run-003", Status: "skipped"},
	}

	got := latestInProgressRunID(runs)
	if got != "" {
		t.Errorf("latestInProgressRunID = %q, want empty", got)
	}
}

func TestLatestInProgressRunID_MultipleInProgress(t *testing.T) {
	runs := []types.CronRun{
		{RunID: "run-001", Status: "in_progress"},
		{RunID: "run-002", Status: "completed"},
		{RunID: "run-003", Status: "in_progress"},
	}

	got := latestInProgressRunID(runs)
	if got != "run-003" {
		t.Errorf("latestInProgressRunID = %q, want %q (last in_progress)", got, "run-003")
	}
}

// =============================================================================
// finalizeRun Tests
// =============================================================================

func TestFinalizeRun_UpdatesRunRecord(t *testing.T) {
	tr, client, _ := schedTestRunner()

	// Set up a mock entry with an in_progress run
	started := time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339)
	client.getEntryResult = map[string]*types.BrainEntry{
		"projects/proj-a/task/sched-1.md": {
			Path: "projects/proj-a/task/sched-1.md",
			Runs: []types.CronRun{
				{RunID: "run-old", Status: "completed", Started: "2026-03-22T10:00:00Z", Completed: "2026-03-22T10:01:00Z"},
				{RunID: "run-123", Status: "in_progress", Started: started},
			},
		},
	}

	task := RunningTask{
		ID:        "sched-1",
		Path:      "projects/proj-a/task/sched-1.md",
		ProjectID: "proj-a",
		RunID:     "run-123",
	}

	ctx := context.Background()
	tr.finalizeRun(ctx, task, CompletionCompleted)

	// Verify UpdateMetadata was called with the runs array
	metaCalls := client.getUpdateMetadataCalls()
	if len(metaCalls) == 0 {
		t.Fatal("expected UpdateMetadata call for run finalization")
	}

	var runCall *updateMetadataCall
	for i, c := range metaCalls {
		if c.Path == "projects/proj-a/task/sched-1.md" {
			if _, ok := c.Fields["runs"]; ok {
				runCall = &metaCalls[i]
				break
			}
		}
	}
	if runCall == nil {
		t.Fatal("expected UpdateMetadata call with 'runs' field")
	}

	runs, ok := runCall.Fields["runs"].([]interface{})
	if !ok {
		t.Fatalf("runs field should be []interface{}, got %T", runCall.Fields["runs"])
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}

	// Check the updated run (run-123)
	updatedRun, ok := runs[1].(map[string]interface{})
	if !ok {
		t.Fatalf("run should be map[string]interface{}, got %T", runs[1])
	}

	if updatedRun["run_id"] != "run-123" {
		t.Errorf("run_id = %v, want run-123", updatedRun["run_id"])
	}
	if updatedRun["status"] != "completed" {
		t.Errorf("status = %v, want completed", updatedRun["status"])
	}
	if updatedRun["completed"] == "" || updatedRun["completed"] == nil {
		t.Error("completed timestamp should be set")
	}
	if dur, ok := updatedRun["duration"].(int); !ok || dur <= 0 {
		t.Errorf("duration should be a positive int, got %v (%T)", updatedRun["duration"], updatedRun["duration"])
	}

	// Check the old run was NOT modified
	oldRun, ok := runs[0].(map[string]interface{})
	if !ok {
		t.Fatalf("run should be map[string]interface{}, got %T", runs[0])
	}
	if oldRun["status"] != "completed" {
		t.Errorf("old run status = %v, want completed (unchanged)", oldRun["status"])
	}
}

func TestFinalizeRun_NoOpWhenRunIDEmpty(t *testing.T) {
	tr, client, _ := schedTestRunner()

	task := RunningTask{
		ID:        "sched-1",
		Path:      "projects/proj-a/task/sched-1.md",
		ProjectID: "proj-a",
		RunID:     "", // empty - should not be called, but test defense
	}

	ctx := context.Background()
	// finalizeRun is only called when RunID != "", but test the guard
	// in handleTaskCompletion by calling handleTaskCompletion directly
	// with an empty RunID task.
	proc := newMockProcess(100)
	pm := tr.processMgr.(*mockProcessMgr)
	pm.Add("sched-1", task, proc)
	pm.setCompletion("sched-1", CompletionCompleted)

	tr.handleTaskCompletion(ctx, "sched-1", task, CompletionCompleted)

	// Should NOT have any UpdateMetadata calls with "runs"
	metaCalls := client.getUpdateMetadataCalls()
	for _, c := range metaCalls {
		if _, ok := c.Fields["runs"]; ok {
			t.Error("should NOT call UpdateMetadata with 'runs' when RunID is empty")
		}
	}
}

func TestFinalizeRun_BlockedStatusMapsFailed(t *testing.T) {
	tr, client, _ := schedTestRunner()

	started := time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339)
	client.getEntryResult = map[string]*types.BrainEntry{
		"projects/proj-a/task/sched-1.md": {
			Path: "projects/proj-a/task/sched-1.md",
			Runs: []types.CronRun{
				{RunID: "run-456", Status: "in_progress", Started: started},
			},
		},
	}

	task := RunningTask{
		ID:        "sched-1",
		Path:      "projects/proj-a/task/sched-1.md",
		ProjectID: "proj-a",
		RunID:     "run-456",
	}

	ctx := context.Background()
	tr.finalizeRun(ctx, task, CompletionBlocked)

	metaCalls := client.getUpdateMetadataCalls()
	var runCall *updateMetadataCall
	for i, c := range metaCalls {
		if _, ok := c.Fields["runs"]; ok {
			runCall = &metaCalls[i]
			break
		}
	}
	if runCall == nil {
		t.Fatal("expected UpdateMetadata call with 'runs' field")
	}

	runs := runCall.Fields["runs"].([]interface{})
	updatedRun := runs[0].(map[string]interface{})

	if updatedRun["status"] != "failed" {
		t.Errorf("blocked completion should map to 'failed', got %v", updatedRun["status"])
	}
}

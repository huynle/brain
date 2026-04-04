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

	if !shouldTrigger("*/15 * * * *", nextRun, now, "") {
		t.Error("should trigger when next_run is in the past")
	}
}

func TestShouldTrigger_NextRunFuture(t *testing.T) {
	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	nextRun := "2026-03-22T10:45:00Z" // 15 minutes from now

	if shouldTrigger("*/15 * * * *", nextRun, now, "") {
		t.Error("should NOT trigger when next_run is in the future")
	}
}

func TestShouldTrigger_NoNextRun_CronMatches(t *testing.T) {
	// At 10:30, */15 matches (30 is divisible by 15)
	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)

	if !shouldTrigger("*/15 * * * *", "", now, "") {
		t.Error("should trigger when no next_run and cron matches current time")
	}
}

func TestShouldTrigger_NoNextRun_CronDoesNotMatch(t *testing.T) {
	// At 10:31, */15 does NOT match
	now := time.Date(2026, 3, 22, 10, 31, 0, 0, time.UTC)

	if shouldTrigger("*/15 * * * *", "", now, "") {
		t.Error("should NOT trigger when no next_run and cron does not match")
	}
}

func TestShouldTrigger_NoSchedule(t *testing.T) {
	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)

	if shouldTrigger("", "", now, "") {
		t.Error("should NOT trigger when schedule is empty")
	}
}

func TestShouldTrigger_InvalidSchedule(t *testing.T) {
	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)

	if shouldTrigger("invalid cron", "", now, "") {
		t.Error("should NOT trigger when schedule is invalid")
	}
}

func TestShouldTrigger_InvalidNextRun(t *testing.T) {
	// Invalid next_run should fall back to cron matching
	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)

	if !shouldTrigger("*/15 * * * *", "not-a-date", now, "") {
		t.Error("should fall back to cron matching when next_run is invalid")
	}
}

// =============================================================================
// Timezone-aware shouldTrigger Tests
// =============================================================================

func TestShouldTrigger_Timezone_ConvertsNowBeforeMatching(t *testing.T) {
	// It's 10:30 UTC, which is 19:30 in Asia/Tokyo (UTC+9).
	// Cron schedule "30 19 * * *" means 19:30 in the task's timezone.
	// Should trigger because now in Tokyo time is 19:30.
	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)

	if !shouldTrigger("30 19 * * *", "", now, "Asia/Tokyo") {
		t.Error("should trigger when now converted to task timezone matches cron")
	}
}

func TestShouldTrigger_Timezone_DoesNotMatchUTC(t *testing.T) {
	// It's 10:30 UTC, cron "30 10 * * *" would match UTC but NOT Asia/Tokyo (19:30).
	// With timezone=Asia/Tokyo, we match against 19:30 not 10:30.
	// "30 10 * * *" should NOT match because in Tokyo it's 19:30.
	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)

	if shouldTrigger("30 10 * * *", "", now, "Asia/Tokyo") {
		t.Error("should NOT trigger: cron 30 10 matches UTC but not Tokyo time (19:30)")
	}
}

func TestShouldTrigger_Timezone_EmptyFallsBackToUTC(t *testing.T) {
	// Empty timezone should behave like UTC (backward compatible)
	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)

	if !shouldTrigger("30 10 * * *", "", now, "") {
		t.Error("empty timezone should fall back to UTC matching")
	}
}

func TestShouldTrigger_Timezone_InvalidFallsBackToUTC(t *testing.T) {
	// Invalid timezone should fall back to UTC
	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)

	if !shouldTrigger("30 10 * * *", "", now, "Invalid/Timezone") {
		t.Error("invalid timezone should fall back to UTC matching")
	}
}

func TestShouldTrigger_Timezone_NextRunStillUsedWhenSet(t *testing.T) {
	// When next_run is set and valid, timezone doesn't affect next_run comparison
	// (next_run is already stored as UTC)
	now := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	nextRun := "2026-03-22T10:15:00Z" // past

	if !shouldTrigger("*/15 * * * *", nextRun, now, "America/New_York") {
		t.Error("should still use next_run comparison regardless of timezone")
	}
}

// =============================================================================
// Timezone-aware getNextRun Tests
// =============================================================================

func TestGetNextRun_Timezone_ComputesInLocalThenReturnsUTC(t *testing.T) {
	// after is 10:30 UTC = 19:30 Tokyo
	// Schedule "0 20 * * *" means 20:00 in Tokyo = 11:00 UTC
	after := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)

	next, err := getNextRun("0 20 * * *", after, "Asia/Tokyo")
	if err != nil {
		t.Fatalf("getNextRun error: %v", err)
	}

	// Next 20:00 Tokyo after 19:30 Tokyo = today 20:00 Tokyo = 11:00 UTC
	expected := time.Date(2026, 3, 22, 11, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("getNextRun = %v, want %v", next, expected)
	}
}

func TestGetNextRun_Timezone_EmptyFallsBackToUTC(t *testing.T) {
	after := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	next, err := getNextRun("*/15 * * * *", after, "")
	if err != nil {
		t.Fatalf("getNextRun error: %v", err)
	}

	expected := time.Date(2026, 3, 22, 10, 45, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("getNextRun = %v, want %v", next, expected)
	}
}

func TestGetNextRun_Timezone_InvalidFallsBackToUTC(t *testing.T) {
	after := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	next, err := getNextRun("*/15 * * * *", after, "Invalid/Zone")
	if err != nil {
		t.Fatalf("getNextRun error: %v", err)
	}

	expected := time.Date(2026, 3, 22, 10, 45, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("getNextRun with invalid timezone = %v, want %v (UTC fallback)", next, expected)
	}
}

// =============================================================================
// parseTimeInZone Tests
// =============================================================================

func TestParseTimeInZone_ValidIANA(t *testing.T) {
	got, err := parseTimeInZone("2026-03-22T10:30:00Z", "America/New_York")
	if err != nil {
		t.Fatalf("parseTimeInZone error: %v", err)
	}
	// Should parse the time and return it in the New York location
	ny, _ := time.LoadLocation("America/New_York")
	expected := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC).In(ny)
	if !got.Equal(expected) {
		t.Errorf("parseTimeInZone = %v, want %v", got, expected)
	}
}

func TestParseTimeInZone_EmptyTimezone(t *testing.T) {
	got, err := parseTimeInZone("2026-03-22T10:30:00Z", "")
	if err != nil {
		t.Fatalf("parseTimeInZone error: %v", err)
	}
	expected := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	if !got.Equal(expected) {
		t.Errorf("parseTimeInZone empty tz = %v, want %v", got, expected)
	}
	if got.Location() != time.UTC {
		t.Errorf("parseTimeInZone empty tz location = %v, want UTC", got.Location())
	}
}

func TestParseTimeInZone_InvalidTimezone(t *testing.T) {
	_, err := parseTimeInZone("2026-03-22T10:30:00Z", "Not/A/Zone")
	if err == nil {
		t.Error("parseTimeInZone should return error for invalid timezone")
	}
}

func TestParseTimeInZone_InvalidTimeString(t *testing.T) {
	_, err := parseTimeInZone("not-a-time", "America/New_York")
	if err == nil {
		t.Error("parseTimeInZone should return error for invalid time string")
	}
}

// =============================================================================
// getNextRun Tests
// =============================================================================

func TestGetNextRun(t *testing.T) {
	after := time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)
	next, err := getNextRun("*/15 * * * *", after, "")
	if err != nil {
		t.Fatalf("getNextRun error: %v", err)
	}

	expected := time.Date(2026, 3, 22, 10, 45, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("getNextRun = %v, want %v", next, expected)
	}
}

func TestGetNextRun_InvalidSchedule(t *testing.T) {
	_, err := getNextRun("invalid", time.Now(), "")
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

// =============================================================================
// checkTimeWindow Tests
// =============================================================================

func TestCheckTimeWindow_NoFields(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	result := checkTimeWindow("", "", now, "")
	if result != windowOpen {
		t.Errorf("no starts_at/expires_at should return windowOpen, got %v", result)
	}
}

func TestCheckTimeWindow_StartsAtFuture(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	startsAt := "2026-07-01T00:00:00Z" // future
	result := checkTimeWindow(startsAt, "", now, "")
	if result != windowNotYet {
		t.Errorf("starts_at in future should return windowNotYet, got %v", result)
	}
}

func TestCheckTimeWindow_StartsAtPast(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	startsAt := "2026-06-01T00:00:00Z" // past
	result := checkTimeWindow(startsAt, "", now, "")
	if result != windowOpen {
		t.Errorf("starts_at in past should return windowOpen, got %v", result)
	}
}

func TestCheckTimeWindow_StartsAtExact(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	startsAt := "2026-06-15T12:00:00Z" // exact match
	result := checkTimeWindow(startsAt, "", now, "")
	if result != windowOpen {
		t.Errorf("starts_at equal to now should return windowOpen, got %v", result)
	}
}

func TestCheckTimeWindow_ExpiresAtPast(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	expiresAt := "2026-06-01T00:00:00Z" // past
	result := checkTimeWindow("", expiresAt, now, "")
	if result != windowExpired {
		t.Errorf("expires_at in past should return windowExpired, got %v", result)
	}
}

func TestCheckTimeWindow_ExpiresAtFuture(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	expiresAt := "2026-07-01T00:00:00Z" // future
	result := checkTimeWindow("", expiresAt, now, "")
	if result != windowOpen {
		t.Errorf("expires_at in future should return windowOpen, got %v", result)
	}
}

func TestCheckTimeWindow_BothFieldsInWindow(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	startsAt := "2026-06-01T00:00:00Z"
	expiresAt := "2026-07-01T00:00:00Z"
	result := checkTimeWindow(startsAt, expiresAt, now, "")
	if result != windowOpen {
		t.Errorf("now within window should return windowOpen, got %v", result)
	}
}

func TestCheckTimeWindow_BothFieldsBeforeWindow(t *testing.T) {
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	startsAt := "2026-06-01T00:00:00Z"
	expiresAt := "2026-07-01T00:00:00Z"
	result := checkTimeWindow(startsAt, expiresAt, now, "")
	if result != windowNotYet {
		t.Errorf("now before window should return windowNotYet, got %v", result)
	}
}

func TestCheckTimeWindow_BothFieldsAfterWindow(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	startsAt := "2026-06-01T00:00:00Z"
	expiresAt := "2026-07-01T00:00:00Z"
	result := checkTimeWindow(startsAt, expiresAt, now, "")
	if result != windowExpired {
		t.Errorf("now after window should return windowExpired, got %v", result)
	}
}

func TestCheckTimeWindow_RespectsTimezone(t *testing.T) {
	// 10:00 UTC = 19:00 Asia/Tokyo
	// starts_at is 2026-06-15T18:00:00Z (= next day 03:00 Tokyo)
	// In UTC: now (10:00) < starts_at (18:00) → not yet
	now := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	startsAt := "2026-06-15T18:00:00Z"
	result := checkTimeWindow(startsAt, "", now, "Asia/Tokyo")
	if result != windowNotYet {
		t.Errorf("should respect timezone, now is before starts_at, got %v", result)
	}
}

func TestCheckTimeWindow_InvalidStartsAtIgnored(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	result := checkTimeWindow("not-a-date", "", now, "")
	if result != windowOpen {
		t.Errorf("invalid starts_at should be ignored (windowOpen), got %v", result)
	}
}

func TestCheckTimeWindow_InvalidExpiresAtIgnored(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	result := checkTimeWindow("", "not-a-date", now, "")
	if result != windowOpen {
		t.Errorf("invalid expires_at should be ignored (windowOpen), got %v", result)
	}
}

// =============================================================================
// Integration: checkProjectScheduledTasks with time windows
// =============================================================================

func TestCheckScheduledTasks_SkipsBeforeStartsAt(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:       "sched-win-1",
			Path:     "projects/proj-a/task/sched-win-1.md",
			Title:    "Future Window Task",
			Status:   "active",
			Schedule: "*/15 * * * *",
			StartsAt: "2026-07-01T00:00:00Z", // future
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	statusCalls := client.getUpdateStatusCalls()
	for _, c := range statusCalls {
		if c.Path == "projects/proj-a/task/sched-win-1.md" && c.Status == "pending" {
			t.Error("should NOT trigger task before starts_at")
		}
	}
}

func TestCheckScheduledTasks_TriggersAfterStartsAt(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:       "sched-win-2",
			Path:     "projects/proj-a/task/sched-win-2.md",
			Title:    "Past Window Task",
			Status:   "active",
			Schedule: "*/15 * * * *",
			StartsAt: "2026-06-01T00:00:00Z", // past
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	statusCalls := client.getUpdateStatusCalls()
	found := false
	for _, c := range statusCalls {
		if c.Path == "projects/proj-a/task/sched-win-2.md" && c.Status == "pending" {
			found = true
		}
	}
	if !found {
		t.Error("should trigger task after starts_at has passed")
	}
}

func TestCheckScheduledTasks_DisablesExpiredTask(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:        "sched-exp-1",
			Path:      "projects/proj-a/task/sched-exp-1.md",
			Title:     "Expired Task",
			Status:    "active",
			Schedule:  "*/15 * * * *",
			ExpiresAt: "2026-06-01T00:00:00Z", // past
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	// Should NOT trigger (no status reset to pending)
	statusCalls := client.getUpdateStatusCalls()
	for _, c := range statusCalls {
		if c.Path == "projects/proj-a/task/sched-exp-1.md" && c.Status == "pending" {
			t.Error("should NOT trigger expired task")
		}
	}

	// Should have disabled the schedule via UpdateMetadata
	metaCalls := client.getUpdateMetadataCalls()
	foundDisable := false
	for _, c := range metaCalls {
		if c.Path == "projects/proj-a/task/sched-exp-1.md" {
			if v, ok := c.Fields["schedule_enabled"]; ok {
				if enabled, ok := v.(bool); ok && !enabled {
					foundDisable = true
				}
			}
		}
	}
	if !foundDisable {
		t.Error("should auto-disable expired task schedule")
	}
}

// =============================================================================
// shouldTrigger — Additional Timezone Tests (America/New_York)
// =============================================================================

func TestShouldTrigger_Timezone_AmericaNewYork(t *testing.T) {
	// It's 15:30 UTC, which is 11:30 EDT (UTC-4) in America/New_York.
	// Cron "30 11 * * *" means 11:30 in NY time.
	now := time.Date(2026, 6, 15, 15, 30, 0, 0, time.UTC)

	if !shouldTrigger("30 11 * * *", "", now, "America/New_York") {
		t.Error("should trigger when now in America/New_York matches cron")
	}
}

func TestShouldTrigger_Timezone_AmericaNewYork_NoMatch(t *testing.T) {
	// It's 15:30 UTC = 11:30 EDT in NY.
	// Cron "30 15 * * *" means 15:30 NY time, which is 19:30 UTC.
	// Should NOT match because in NY it's 11:30, not 15:30.
	now := time.Date(2026, 6, 15, 15, 30, 0, 0, time.UTC)

	if shouldTrigger("30 15 * * *", "", now, "America/New_York") {
		t.Error("should NOT trigger: cron 30 15 matches UTC but not NY time (11:30)")
	}
}

func TestShouldTrigger_Timezone_AmericaNewYork_Winter(t *testing.T) {
	// In January, New York is EST (UTC-5).
	// 16:00 UTC = 11:00 EST
	now := time.Date(2026, 1, 15, 16, 0, 0, 0, time.UTC)

	if !shouldTrigger("0 11 * * *", "", now, "America/New_York") {
		t.Error("should trigger: 16:00 UTC = 11:00 EST in winter")
	}
}

// =============================================================================
// shouldTriggerRunOnce Unit Tests
// =============================================================================

func TestShouldTriggerRunOnce_TriggersAtCorrectTime(t *testing.T) {
	now := time.Date(2026, 6, 15, 14, 0, 0, 0, time.UTC)
	runOnceAt := "2026-06-15T14:00:00Z" // exact match

	if !shouldTriggerRunOnce(runOnceAt, now, "") {
		t.Error("should trigger when now equals run_once_at")
	}
}

func TestShouldTriggerRunOnce_TriggersAfterTime(t *testing.T) {
	now := time.Date(2026, 6, 15, 14, 30, 0, 0, time.UTC)
	runOnceAt := "2026-06-15T14:00:00Z" // 30 minutes ago

	if !shouldTriggerRunOnce(runOnceAt, now, "") {
		t.Error("should trigger when now is after run_once_at")
	}
}

func TestShouldTriggerRunOnce_DoesNotTriggerBeforeTime(t *testing.T) {
	now := time.Date(2026, 6, 15, 13, 30, 0, 0, time.UTC)
	runOnceAt := "2026-06-15T14:00:00Z" // 30 minutes from now

	if shouldTriggerRunOnce(runOnceAt, now, "") {
		t.Error("should NOT trigger when now is before run_once_at")
	}
}

func TestShouldTriggerRunOnce_EmptyString(t *testing.T) {
	now := time.Date(2026, 6, 15, 14, 0, 0, 0, time.UTC)

	if shouldTriggerRunOnce("", now, "") {
		t.Error("should NOT trigger when run_once_at is empty")
	}
}

func TestShouldTriggerRunOnce_InvalidFormat(t *testing.T) {
	now := time.Date(2026, 6, 15, 14, 0, 0, 0, time.UTC)

	if shouldTriggerRunOnce("not-a-date", now, "") {
		t.Error("should NOT trigger when run_once_at is invalid")
	}
}

func TestShouldTriggerRunOnce_PastRunOnceAt(t *testing.T) {
	// A run_once_at that's far in the past should still trigger
	now := time.Date(2026, 6, 15, 14, 0, 0, 0, time.UTC)
	runOnceAt := "2026-01-01T00:00:00Z" // months ago

	if !shouldTriggerRunOnce(runOnceAt, now, "") {
		t.Error("should trigger for past run_once_at (schedule_enabled controls re-trigger)")
	}
}

func TestShouldTriggerRunOnce_WithTimezoneParam(t *testing.T) {
	// run_once_at is RFC3339 (absolute time), timezone param is accepted but
	// doesn't change the comparison since RFC3339 already embeds offset.
	now := time.Date(2026, 6, 15, 14, 0, 0, 0, time.UTC)
	runOnceAt := "2026-06-15T14:00:00Z"

	if !shouldTriggerRunOnce(runOnceAt, now, "America/New_York") {
		t.Error("should trigger regardless of timezone param (RFC3339 is absolute)")
	}
}

// =============================================================================
// run_once_at Integration Tests
// =============================================================================

func TestCheckScheduledTasks_RunOnce_TriggersAndDisables(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 6, 15, 14, 0, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:        "once-1",
			Path:      "projects/proj-a/task/once-1.md",
			Title:     "One-Shot Task",
			Status:    "active",
			RunOnceAt: "2026-06-15T13:00:00Z", // past
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	// Should reset status to pending
	statusCalls := client.getUpdateStatusCalls()
	foundPending := false
	for _, c := range statusCalls {
		if c.Path == "projects/proj-a/task/once-1.md" && c.Status == "pending" {
			foundPending = true
		}
	}
	if !foundPending {
		t.Error("run_once_at task should be reset to pending")
	}

	// Should auto-disable via UpdateMetadata (schedule_enabled=false)
	metaCalls := client.getUpdateMetadataCalls()
	foundDisable := false
	for _, c := range metaCalls {
		if c.Path == "projects/proj-a/task/once-1.md" {
			if v, ok := c.Fields["schedule_enabled"]; ok {
				if enabled, ok := v.(bool); ok && !enabled {
					foundDisable = true
				}
			}
		}
	}
	if !foundDisable {
		t.Error("run_once_at task should auto-disable schedule_enabled after firing")
	}
}

func TestCheckScheduledTasks_RunOnce_DoesNotTriggerBeforeTime(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:        "once-2",
			Path:      "projects/proj-a/task/once-2.md",
			Title:     "Future One-Shot Task",
			Status:    "active",
			RunOnceAt: "2026-06-15T14:00:00Z", // future
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	statusCalls := client.getUpdateStatusCalls()
	for _, c := range statusCalls {
		if c.Path == "projects/proj-a/task/once-2.md" && c.Status == "pending" {
			t.Error("run_once_at task should NOT trigger before its time")
		}
	}
}

func TestCheckScheduledTasks_RunOnce_WithoutCronTreatedAsScheduled(t *testing.T) {
	// A task with run_once_at but NO schedule field should still be treated as scheduled
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 6, 15, 14, 0, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:        "once-3",
			Path:      "projects/proj-a/task/once-3.md",
			Title:     "No-Cron One-Shot",
			Status:    "active",
			Schedule:  "",                     // no cron
			RunOnceAt: "2026-06-15T13:00:00Z", // past
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	statusCalls := client.getUpdateStatusCalls()
	foundPending := false
	for _, c := range statusCalls {
		if c.Path == "projects/proj-a/task/once-3.md" && c.Status == "pending" {
			foundPending = true
		}
	}
	if !foundPending {
		t.Error("task with run_once_at but no cron should still trigger as scheduled")
	}
}

func TestCheckScheduledTasks_RunOnce_RecordsRunEntry(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 6, 15, 14, 0, 0, 0, time.UTC)
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:        "once-4",
			Path:      "projects/proj-a/task/once-4.md",
			Title:     "One-Shot With Runs",
			Status:    "active",
			RunOnceAt: "2026-06-15T13:00:00Z",
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	metaCalls := client.getUpdateMetadataCalls()
	foundRuns := false
	for _, c := range metaCalls {
		if c.Path == "projects/proj-a/task/once-4.md" {
			if runs, ok := c.Fields["runs"]; ok {
				runList, ok := runs.([]interface{})
				if ok && len(runList) > 0 {
					foundRuns = true
				}
			}
		}
	}
	if !foundRuns {
		t.Error("run_once_at task should record a run entry")
	}
}

// =============================================================================
// parseTimeInZone — Additional Tests
// =============================================================================

func TestParseTimeInZone_AsiaTokyo(t *testing.T) {
	got, err := parseTimeInZone("2026-06-15T12:00:00Z", "Asia/Tokyo")
	if err != nil {
		t.Fatalf("parseTimeInZone error: %v", err)
	}
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	expected := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC).In(tokyo)
	if !got.Equal(expected) {
		t.Errorf("parseTimeInZone = %v, want %v", got, expected)
	}
	if got.Location().String() != "Asia/Tokyo" {
		t.Errorf("location = %v, want Asia/Tokyo", got.Location())
	}
}

func TestParseTimeInZone_LocationIsCorrect(t *testing.T) {
	got, err := parseTimeInZone("2026-06-15T12:00:00Z", "America/New_York")
	if err != nil {
		t.Fatalf("parseTimeInZone error: %v", err)
	}
	if got.Location().String() != "America/New_York" {
		t.Errorf("location = %v, want America/New_York", got.Location())
	}
	// In June, NY is EDT (UTC-4), so 12:00 UTC = 08:00 EDT
	if got.Hour() != 8 {
		t.Errorf("hour = %d, want 8 (EDT = UTC-4)", got.Hour())
	}
}

func TestParseTimeInZone_PreservesAbsoluteTime(t *testing.T) {
	// Same absolute moment regardless of timezone
	utc, err := parseTimeInZone("2026-06-15T12:00:00Z", "")
	if err != nil {
		t.Fatalf("parseTimeInZone UTC error: %v", err)
	}
	tokyo, err := parseTimeInZone("2026-06-15T12:00:00Z", "Asia/Tokyo")
	if err != nil {
		t.Fatalf("parseTimeInZone Tokyo error: %v", err)
	}
	if !utc.Equal(tokyo) {
		t.Error("same RFC3339 input should represent same absolute moment in any timezone")
	}
}

// =============================================================================
// getNextRun — Additional Timezone Tests
// =============================================================================

func TestGetNextRun_Timezone_AmericaNewYork(t *testing.T) {
	// after is 15:30 UTC = 11:30 EDT (June, UTC-4)
	// Schedule "0 12 * * *" means 12:00 in New York = 16:00 UTC
	after := time.Date(2026, 6, 15, 15, 30, 0, 0, time.UTC)

	next, err := getNextRun("0 12 * * *", after, "America/New_York")
	if err != nil {
		t.Fatalf("getNextRun error: %v", err)
	}

	// Next 12:00 NY after 11:30 NY = today 12:00 NY = 16:00 UTC
	expected := time.Date(2026, 6, 15, 16, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("getNextRun(America/New_York) = %v, want %v", next, expected)
	}
}

func TestGetNextRun_Timezone_ReturnsUTC(t *testing.T) {
	after := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	next, err := getNextRun("0 20 * * *", after, "Asia/Tokyo")
	if err != nil {
		t.Fatalf("getNextRun error: %v", err)
	}

	if next.Location() != time.UTC {
		t.Errorf("getNextRun should return UTC, got location %v", next.Location())
	}
}

func TestGetNextRun_Timezone_DSTTransition(t *testing.T) {
	// March 8, 2026: US spring forward (EST->EDT at 2:00 AM)
	// After 06:30 UTC = 01:30 EST
	// Schedule "0 3 * * *" = 3:00 AM local
	// After spring forward, 3:00 AM EDT = 07:00 UTC
	after := time.Date(2026, 3, 8, 6, 30, 0, 0, time.UTC)

	next, err := getNextRun("0 3 * * *", after, "America/New_York")
	if err != nil {
		t.Fatalf("getNextRun error: %v", err)
	}

	// 3:00 AM EDT = 07:00 UTC (since clocks spring forward, 2:00 AM -> 3:00 AM)
	expected := time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("getNextRun DST transition = %v, want %v", next, expected)
	}
}

// =============================================================================
// checkTimeWindow — Additional Tests
// =============================================================================

func TestCheckTimeWindow_ExpiresAtExact(t *testing.T) {
	// When now is exactly at expires_at, should still be windowOpen (not expired)
	// because now.After(expires_at) is false when equal
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	expiresAt := "2026-06-15T12:00:00Z"
	result := checkTimeWindow("", expiresAt, now, "")
	if result != windowOpen {
		t.Errorf("expires_at equal to now should return windowOpen, got %v", result)
	}
}

func TestCheckTimeWindow_ExpiresAtBeforeStartsAt(t *testing.T) {
	// Edge case: expires_at is before starts_at (misconfiguration)
	// starts_at is checked first, so if now < starts_at => windowNotYet
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	startsAt := "2026-07-01T00:00:00Z"  // future
	expiresAt := "2026-06-01T00:00:00Z" // past (before starts_at)
	result := checkTimeWindow(startsAt, expiresAt, now, "")
	if result != windowNotYet {
		t.Errorf("when expires_at < starts_at and now < starts_at, should return windowNotYet, got %v", result)
	}
}

func TestCheckTimeWindow_ExpiresAtBeforeStartsAt_NowAfterBoth(t *testing.T) {
	// Edge case: expires_at before starts_at, now is after both
	// starts_at check passes (now > starts_at), expires_at check: now > expires_at => windowExpired
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	startsAt := "2026-06-01T00:00:00Z"  // past
	expiresAt := "2026-05-01T00:00:00Z" // even more past (before starts_at)
	result := checkTimeWindow(startsAt, expiresAt, now, "")
	if result != windowExpired {
		t.Errorf("when now > both and expires_at < starts_at, should return windowExpired, got %v", result)
	}
}

func TestCheckTimeWindow_WithTimezoneOffset(t *testing.T) {
	// starts_at has a non-UTC timezone offset: 2026-06-15T12:00:00+09:00
	// This is 03:00 UTC. If now is 02:00 UTC, window not yet.
	now := time.Date(2026, 6, 15, 2, 0, 0, 0, time.UTC)
	startsAt := "2026-06-15T12:00:00+09:00" // = 03:00 UTC
	result := checkTimeWindow(startsAt, "", now, "")
	if result != windowNotYet {
		t.Errorf("RFC3339 with offset should be correctly parsed, got %v", result)
	}
}

func TestCheckTimeWindow_WithTimezoneOffset_After(t *testing.T) {
	// Same as above but now is after the start
	now := time.Date(2026, 6, 15, 4, 0, 0, 0, time.UTC)
	startsAt := "2026-06-15T12:00:00+09:00" // = 03:00 UTC
	result := checkTimeWindow(startsAt, "", now, "")
	if result != windowOpen {
		t.Errorf("now after starts_at (with offset) should return windowOpen, got %v", result)
	}
}

// =============================================================================
// loadTimezone Tests
// =============================================================================

func TestLoadTimezone_ValidIANA(t *testing.T) {
	loc := loadTimezone("America/New_York")
	if loc.String() != "America/New_York" {
		t.Errorf("loadTimezone = %v, want America/New_York", loc)
	}
}

func TestLoadTimezone_Empty(t *testing.T) {
	loc := loadTimezone("")
	if loc != time.UTC {
		t.Errorf("loadTimezone empty = %v, want UTC", loc)
	}
}

func TestLoadTimezone_Invalid(t *testing.T) {
	loc := loadTimezone("Not/Real/Zone")
	if loc != time.UTC {
		t.Errorf("loadTimezone invalid = %v, want UTC fallback", loc)
	}
}

func TestLoadTimezone_UTC(t *testing.T) {
	loc := loadTimezone("UTC")
	if loc != time.UTC {
		t.Errorf("loadTimezone UTC = %v, want UTC", loc)
	}
}

// =============================================================================
// isScheduledTask Tests
// =============================================================================

func TestIsScheduledTask_WithSchedule(t *testing.T) {
	task := &types.ResolvedTask{Schedule: "*/15 * * * *"}
	if !isScheduledTask(task) {
		t.Error("task with schedule should be scheduled")
	}
}

func TestIsScheduledTask_WithRunOnceAt(t *testing.T) {
	task := &types.ResolvedTask{RunOnceAt: "2026-06-15T14:00:00Z"}
	if !isScheduledTask(task) {
		t.Error("task with run_once_at should be scheduled")
	}
}

func TestIsScheduledTask_WithBoth(t *testing.T) {
	task := &types.ResolvedTask{
		Schedule:  "*/15 * * * *",
		RunOnceAt: "2026-06-15T14:00:00Z",
	}
	if !isScheduledTask(task) {
		t.Error("task with both schedule and run_once_at should be scheduled")
	}
}

func TestIsScheduledTask_Neither(t *testing.T) {
	task := &types.ResolvedTask{}
	if isScheduledTask(task) {
		t.Error("task without schedule or run_once_at should NOT be scheduled")
	}
}

// =============================================================================
// countRuns Tests
// =============================================================================

func TestCountRuns_MixedStatuses(t *testing.T) {
	runs := []types.CronRun{
		{RunID: "r1", Status: "completed"},
		{RunID: "r2", Status: "failed"},
		{RunID: "r3", Status: "skipped"},
		{RunID: "r4", Status: "in_progress"},
	}
	got := countRuns(runs)
	if got != 4 {
		t.Errorf("countRuns = %d, want 4", got)
	}
}

func TestCountRuns_Empty(t *testing.T) {
	got := countRuns(nil)
	if got != 0 {
		t.Errorf("countRuns(nil) = %d, want 0", got)
	}
}

func TestCountRuns_UnknownStatusNotCounted(t *testing.T) {
	runs := []types.CronRun{
		{RunID: "r1", Status: "completed"},
		{RunID: "r2", Status: "unknown"},
		{RunID: "r3", Status: "pending"},
	}
	got := countRuns(runs)
	if got != 1 {
		t.Errorf("countRuns = %d, want 1 (only 'completed' counts)", got)
	}
}

// =============================================================================
// generateRunID Tests
// =============================================================================

func TestGenerateRunID_Format(t *testing.T) {
	ts := time.Date(2026, 6, 15, 14, 30, 0, 0, time.UTC)
	id := generateRunID(ts)

	// Format: YYYYMMDD-HHMM-<6 hex chars>
	if len(id) != 20 { // "20260615-1430-" (14) + 6 hex chars
		t.Errorf("generateRunID length = %d, want 20, got %q", len(id), id)
	}
	if id[:13] != "20260615-1430" {
		t.Errorf("generateRunID prefix = %q, want %q", id[:13], "20260615-1430")
	}
	if id[13] != '-' {
		t.Errorf("generateRunID separator at 13 = %c, want '-'", id[13])
	}
}

func TestGenerateRunID_Unique(t *testing.T) {
	ts := time.Date(2026, 6, 15, 14, 30, 0, 0, time.UTC)
	id1 := generateRunID(ts)
	id2 := generateRunID(ts)
	if id1 == id2 {
		t.Error("generateRunID should produce unique IDs (random suffix)")
	}
}

// =============================================================================
// Integration: max_runs enforcement
// =============================================================================

func TestCheckScheduledTasks_MaxRunsReached(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	maxRuns := 2
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:       "sched-max",
			Path:     "projects/proj-a/task/sched-max.md",
			Title:    "Max Runs Task",
			Status:   "active",
			Schedule: "*/15 * * * *",
			MaxRuns:  &maxRuns,
			Runs: []types.CronRun{
				{RunID: "r1", Status: "completed"},
				{RunID: "r2", Status: "completed"},
			},
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	// Should NOT trigger (max_runs reached)
	statusCalls := client.getUpdateStatusCalls()
	for _, c := range statusCalls {
		if c.Path == "projects/proj-a/task/sched-max.md" && c.Status == "pending" {
			t.Error("should NOT trigger when max_runs is reached")
		}
	}

	// Should auto-disable
	metaCalls := client.getUpdateMetadataCalls()
	foundDisable := false
	for _, c := range metaCalls {
		if c.Path == "projects/proj-a/task/sched-max.md" {
			if v, ok := c.Fields["schedule_enabled"]; ok {
				if enabled, ok := v.(bool); ok && !enabled {
					foundDisable = true
				}
			}
		}
	}
	if !foundDisable {
		t.Error("should auto-disable when max_runs is reached")
	}
}

func TestCheckScheduledTasks_MaxRunsNotReached(t *testing.T) {
	tr, client, _ := schedTestRunner()

	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	maxRuns := 5
	client.allTasks["proj-a"] = []types.ResolvedTask{
		{
			ID:       "sched-max2",
			Path:     "projects/proj-a/task/sched-max2.md",
			Title:    "Under Max Runs",
			Status:   "active",
			Schedule: "*/15 * * * *",
			MaxRuns:  &maxRuns,
			Runs: []types.CronRun{
				{RunID: "r1", Status: "completed"},
			},
		},
	}

	ctx := context.Background()
	tr.checkScheduledTasks(ctx, now)

	statusCalls := client.getUpdateStatusCalls()
	foundPending := false
	for _, c := range statusCalls {
		if c.Path == "projects/proj-a/task/sched-max2.md" && c.Status == "pending" {
			foundPending = true
		}
	}
	if !foundPending {
		t.Error("should trigger when max_runs not yet reached")
	}
}

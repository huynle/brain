package tui

import (
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Test Helpers
// =============================================================================

// makeScheduledTask creates a ResolvedTask with schedule fields set.
func makeScheduledTask(id, title, schedule string, enabled bool) types.ResolvedTask {
	return types.ResolvedTask{
		ID:              id,
		Title:           title,
		Status:          "pending",
		Priority:        "medium",
		Classification:  "ready",
		Schedule:        schedule,
		ScheduleEnabled: &enabled,
	}
}

// =============================================================================
// ScheduleList - Empty State
// =============================================================================

func TestScheduleList_EmptyState_ShowsPlaceholder(t *testing.T) {
	sl := NewScheduleList()
	view := sl.View(80, 20)

	if !strings.Contains(view, "No scheduled tasks found") {
		t.Errorf("expected 'No scheduled tasks found' placeholder, got:\n%s", view)
	}
}

// =============================================================================
// ScheduleList - SetTasks Filtering
// =============================================================================

func TestScheduleList_SetTasks_FiltersToScheduledOnly(t *testing.T) {
	sl := NewScheduleList()

	tasks := []types.ResolvedTask{
		makeScheduledTask("t1", "Scheduled task", "0 */6 * * *", true),
		{ID: "t2", Title: "Regular task", Priority: "medium"}, // No schedule
		makeScheduledTask("t3", "Another scheduled", "0 0 * * *", false),
	}
	sl.SetTasks(tasks)

	// Should only have 2 scheduled tasks
	view := sl.View(80, 20)

	if !strings.Contains(view, "Scheduled task") {
		t.Errorf("expected 'Scheduled task' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Another scheduled") {
		t.Errorf("expected 'Another scheduled' in view, got:\n%s", view)
	}
	if strings.Contains(view, "Regular task") {
		t.Errorf("should NOT contain 'Regular task' (no schedule), got:\n%s", view)
	}
}

func TestScheduleList_SetTasks_IncludesRunOnceAtTasks(t *testing.T) {
	sl := NewScheduleList()

	tasks := []types.ResolvedTask{
		makeScheduledTask("t1", "Cron task", "0 */6 * * *", true),
		{
			ID:              "t2",
			Title:           "One-shot task",
			Status:          "pending",
			Priority:        "medium",
			Classification:  "ready",
			RunOnceAt:       "2099-01-15T10:00:00Z",
			ScheduleEnabled: boolPtr(true),
		},
		{ID: "t3", Title: "Regular task", Priority: "medium"}, // No schedule, no run_once_at
	}
	sl.SetTasks(tasks)

	view := sl.View(80, 20)

	if !strings.Contains(view, "Cron task") {
		t.Errorf("expected 'Cron task' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "One-shot task") {
		t.Errorf("expected 'One-shot task' (has run_once_at) in view, got:\n%s", view)
	}
	if strings.Contains(view, "Regular task") {
		t.Errorf("should NOT contain 'Regular task' (no schedule/run_once_at), got:\n%s", view)
	}
}

func TestScheduleList_View_ShowsOneShotBadge(t *testing.T) {
	sl := NewScheduleList()

	tasks := []types.ResolvedTask{
		{
			ID:              "t1",
			Title:           "One-shot task",
			Status:          "pending",
			Priority:        "medium",
			Classification:  "ready",
			RunOnceAt:       "2099-01-15T10:00:00Z",
			ScheduleEnabled: boolPtr(true),
		},
	}
	sl.SetTasks(tasks)

	view := sl.View(80, 20)

	if !strings.Contains(view, "[one-shot]") {
		t.Errorf("expected '[one-shot]' badge for run_once_at task, got:\n%s", view)
	}
}

func TestScheduleList_View_ShowsCountdownForOneShot(t *testing.T) {
	sl := NewScheduleList()

	tasks := []types.ResolvedTask{
		{
			ID:              "t1",
			Title:           "Future task",
			Status:          "pending",
			Priority:        "medium",
			Classification:  "ready",
			RunOnceAt:       "2099-01-15T10:00:00Z",
			ScheduleEnabled: boolPtr(true),
		},
	}
	sl.SetTasks(tasks)

	view := sl.View(80, 20)

	// Should show a countdown like "in Xd Xh" for a far-future date
	if !strings.Contains(view, "in ") {
		t.Errorf("expected countdown 'in ...' for future run_once_at, got:\n%s", view)
	}
}

func TestScheduleList_View_ShowsPassedForPastOneShot(t *testing.T) {
	sl := NewScheduleList()

	tasks := []types.ResolvedTask{
		{
			ID:              "t1",
			Title:           "Past task",
			Status:          "pending",
			Priority:        "medium",
			Classification:  "ready",
			RunOnceAt:       "2020-01-01T00:00:00Z",
			ScheduleEnabled: boolPtr(true),
		},
	}
	sl.SetTasks(tasks)

	view := sl.View(80, 20)

	if !strings.Contains(view, "passed") {
		t.Errorf("expected 'passed' for past run_once_at, got:\n%s", view)
	}
}

// =============================================================================
// ScheduleList - Navigation
// =============================================================================

func TestScheduleList_Navigation_MoveDown(t *testing.T) {
	sl := NewScheduleList()

	tasks := []types.ResolvedTask{
		makeScheduledTask("t1", "First", "0 * * * *", true),
		makeScheduledTask("t2", "Second", "0 0 * * *", true),
		makeScheduledTask("t3", "Third", "0 0 * * 0", true),
	}
	sl.SetTasks(tasks)

	// Initially should select first task
	if sl.SelectedID != "t1" {
		t.Errorf("expected initial SelectedID 't1', got '%s'", sl.SelectedID)
	}

	sl.MoveDown()
	if sl.SelectedID != "t2" {
		t.Errorf("expected SelectedID 't2' after MoveDown, got '%s'", sl.SelectedID)
	}

	sl.MoveDown()
	if sl.SelectedID != "t3" {
		t.Errorf("expected SelectedID 't3' after second MoveDown, got '%s'", sl.SelectedID)
	}

	// Should not go past the end
	sl.MoveDown()
	if sl.SelectedID != "t3" {
		t.Errorf("expected SelectedID to stay 't3' at end, got '%s'", sl.SelectedID)
	}
}

func TestScheduleList_Navigation_MoveUp(t *testing.T) {
	sl := NewScheduleList()

	tasks := []types.ResolvedTask{
		makeScheduledTask("t1", "First", "0 * * * *", true),
		makeScheduledTask("t2", "Second", "0 0 * * *", true),
		makeScheduledTask("t3", "Third", "0 0 * * 0", true),
	}
	sl.SetTasks(tasks)

	// Move to second task
	sl.MoveDown()
	sl.MoveDown()
	if sl.SelectedID != "t3" {
		t.Fatalf("expected SelectedID 't3', got '%s'", sl.SelectedID)
	}

	sl.MoveUp()
	if sl.SelectedID != "t2" {
		t.Errorf("expected SelectedID 't2' after MoveUp, got '%s'", sl.SelectedID)
	}

	sl.MoveUp()
	if sl.SelectedID != "t1" {
		t.Errorf("expected SelectedID 't1' after second MoveUp, got '%s'", sl.SelectedID)
	}

	// Should not go past the beginning
	sl.MoveUp()
	if sl.SelectedID != "t1" {
		t.Errorf("expected SelectedID to stay 't1' at top, got '%s'", sl.SelectedID)
	}
}

func TestScheduleList_Navigation_MoveToTop(t *testing.T) {
	sl := NewScheduleList()

	tasks := []types.ResolvedTask{
		makeScheduledTask("t1", "First", "0 * * * *", true),
		makeScheduledTask("t2", "Second", "0 0 * * *", true),
		makeScheduledTask("t3", "Third", "0 0 * * 0", true),
	}
	sl.SetTasks(tasks)

	sl.MoveDown()
	sl.MoveDown()
	sl.MoveToTop()

	if sl.SelectedID != "t1" {
		t.Errorf("expected SelectedID 't1' after MoveToTop, got '%s'", sl.SelectedID)
	}
	if sl.Cursor != 0 {
		t.Errorf("expected Cursor 0 after MoveToTop, got %d", sl.Cursor)
	}
}

func TestScheduleList_Navigation_MoveToBottom(t *testing.T) {
	sl := NewScheduleList()

	tasks := []types.ResolvedTask{
		makeScheduledTask("t1", "First", "0 * * * *", true),
		makeScheduledTask("t2", "Second", "0 0 * * *", true),
		makeScheduledTask("t3", "Third", "0 0 * * 0", true),
	}
	sl.SetTasks(tasks)

	sl.MoveToBottom()

	if sl.SelectedID != "t3" {
		t.Errorf("expected SelectedID 't3' after MoveToBottom, got '%s'", sl.SelectedID)
	}
}

// =============================================================================
// ScheduleList - SelectedTask
// =============================================================================

func TestScheduleList_SelectedTask_ReturnsCorrectTask(t *testing.T) {
	sl := NewScheduleList()

	tasks := []types.ResolvedTask{
		makeScheduledTask("t1", "First", "0 * * * *", true),
		makeScheduledTask("t2", "Second", "0 0 * * *", true),
	}
	sl.SetTasks(tasks)

	selected := sl.SelectedTask()
	if selected == nil {
		t.Fatal("expected non-nil SelectedTask")
	}
	if selected.ID != "t1" {
		t.Errorf("expected selected task ID 't1', got '%s'", selected.ID)
	}

	sl.MoveDown()
	selected = sl.SelectedTask()
	if selected == nil {
		t.Fatal("expected non-nil SelectedTask after MoveDown")
	}
	if selected.ID != "t2" {
		t.Errorf("expected selected task ID 't2', got '%s'", selected.ID)
	}
}

func TestScheduleList_SelectedTask_ReturnsNilWhenEmpty(t *testing.T) {
	sl := NewScheduleList()

	selected := sl.SelectedTask()
	if selected != nil {
		t.Errorf("expected nil SelectedTask when empty, got %+v", selected)
	}
}

// =============================================================================
// ScheduleList - View Rendering
// =============================================================================

func TestScheduleList_View_ShowsScheduledBadge(t *testing.T) {
	sl := NewScheduleList()

	tasks := []types.ResolvedTask{
		makeScheduledTask("t1", "Enabled task", "0 * * * *", true),
	}
	sl.SetTasks(tasks)

	view := sl.View(80, 20)

	if !strings.Contains(view, "[scheduled]") {
		t.Errorf("expected '[scheduled]' badge for enabled task, got:\n%s", view)
	}
}

func TestScheduleList_View_ShowsDisabledBadge(t *testing.T) {
	sl := NewScheduleList()

	disabled := false
	tasks := []types.ResolvedTask{
		{
			ID:              "t1",
			Title:           "Disabled task",
			Status:          "pending",
			Priority:        "medium",
			Classification:  "ready",
			Schedule:        "0 * * * *",
			ScheduleEnabled: &disabled,
		},
	}
	sl.SetTasks(tasks)

	view := sl.View(80, 20)

	if !strings.Contains(view, "[disabled]") {
		t.Errorf("expected '[disabled]' badge for disabled task, got:\n%s", view)
	}
}

func TestScheduleList_View_ShowsScheduleExpression(t *testing.T) {
	sl := NewScheduleList()

	tasks := []types.ResolvedTask{
		makeScheduledTask("t1", "Cron task", "0 */6 * * *", true),
	}
	sl.SetTasks(tasks)

	view := sl.View(80, 20)

	if !strings.Contains(view, "0 */6 * * *") {
		t.Errorf("expected schedule expression '0 */6 * * *' in view, got:\n%s", view)
	}
}

func TestScheduleList_View_ShowsSelectionIndicator(t *testing.T) {
	sl := NewScheduleList()

	tasks := []types.ResolvedTask{
		makeScheduledTask("t1", "Selected task", "0 * * * *", true),
		makeScheduledTask("t2", "Other task", "0 0 * * *", true),
	}
	sl.SetTasks(tasks)

	view := sl.View(80, 20)

	// The selected task should have the selection indicator "▸"
	if !strings.Contains(view, "▸") {
		t.Errorf("expected selection indicator '▸' in view, got:\n%s", view)
	}
}

func TestScheduleList_View_ShowsHeader(t *testing.T) {
	sl := NewScheduleList()

	tasks := []types.ResolvedTask{
		makeScheduledTask("t1", "Task", "0 * * * *", true),
	}
	sl.SetTasks(tasks)

	view := sl.View(80, 20)

	if !strings.Contains(view, "Scheduled") {
		t.Errorf("expected 'Scheduled' header in view, got:\n%s", view)
	}
}

func TestScheduleList_View_ShowsPrioritySuffix(t *testing.T) {
	sl := NewScheduleList()

	tasks := []types.ResolvedTask{
		{
			ID:              "t1",
			Title:           "High priority task",
			Status:          "pending",
			Priority:        "high",
			Classification:  "ready",
			Schedule:        "0 * * * *",
			ScheduleEnabled: boolPtr(true),
		},
	}
	sl.SetTasks(tasks)

	view := sl.View(80, 20)

	if !strings.Contains(view, "pri:high") {
		t.Errorf("expected 'pri:high' suffix for high priority task, got:\n%s", view)
	}
}

func TestScheduleList_View_NoPrioritySuffixForMedium(t *testing.T) {
	sl := NewScheduleList()

	tasks := []types.ResolvedTask{
		makeScheduledTask("t1", "Medium priority task", "0 * * * *", true),
	}
	sl.SetTasks(tasks)

	view := sl.View(80, 20)

	if strings.Contains(view, "pri:") {
		t.Errorf("should NOT show priority suffix for medium priority, got:\n%s", view)
	}
}

// =============================================================================
// ScheduleList - SetTasks preserves selection
// =============================================================================

func TestScheduleList_SetTasks_PreservesSelection(t *testing.T) {
	sl := NewScheduleList()

	tasks := []types.ResolvedTask{
		makeScheduledTask("t1", "First", "0 * * * *", true),
		makeScheduledTask("t2", "Second", "0 0 * * *", true),
	}
	sl.SetTasks(tasks)
	sl.MoveDown() // Select t2

	if sl.SelectedID != "t2" {
		t.Fatalf("expected SelectedID 't2', got '%s'", sl.SelectedID)
	}

	// Re-set tasks (simulating SSE update)
	sl.SetTasks(tasks)

	if sl.SelectedID != "t2" {
		t.Errorf("expected SelectedID 't2' preserved after SetTasks, got '%s'", sl.SelectedID)
	}
}

// =============================================================================
// Helpers
// =============================================================================

func boolPtr(b bool) *bool {
	return &b
}

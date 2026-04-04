package tui

import (
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Helper
// =============================================================================

// makeScheduledTaskForDetail creates a ResolvedTask with schedule fields set.
func makeScheduledTaskForDetail(id, title, schedule string, enabled bool) *types.ResolvedTask {
	e := enabled
	return &types.ResolvedTask{
		ID:              id,
		Title:           title,
		Schedule:        schedule,
		ScheduleEnabled: &e,
		Status:          "pending",
		Priority:        "medium",
		Classification:  "ready",
		Path:            "projects/test/task/" + id + ".md",
		ProjectID:       "test-project",
		Created:         "2025-01-15T10:00:00Z",
	}
}

// =============================================================================
// ScheduleDetail - No Task Selected
// =============================================================================

func TestScheduleDetail_NoTask_ShowsPlaceholder(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(60, 20)

	view := sd.View()

	if !strings.Contains(view, "Select a scheduled task to view details") {
		t.Errorf("expected placeholder text, got:\n%s", view)
	}
}

func TestScheduleDetail_ShowsHeader(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(60, 20)

	view := sd.View()

	if !strings.Contains(view, "Schedule Details") {
		t.Errorf("expected 'Schedule Details' header, got:\n%s", view)
	}
}

// =============================================================================
// ScheduleDetail - Task Fields
// =============================================================================

func TestScheduleDetail_ShowsTitle(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(80, 30)

	task := makeScheduledTaskForDetail("abc12def", "Daily backup job", "0 2 * * *", true)
	sd.SetTask(task)

	view := sd.View()

	if !strings.Contains(view, "Daily backup job") {
		t.Errorf("expected task title in view, got:\n%s", view)
	}
}

func TestScheduleDetail_ShowsID(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(80, 30)

	task := makeScheduledTaskForDetail("abc12def", "Test task", "0 2 * * *", true)
	sd.SetTask(task)

	view := sd.View()

	if !strings.Contains(view, "abc12def") {
		t.Errorf("expected ID in view, got:\n%s", view)
	}
}

func TestScheduleDetail_ShowsScheduleExpression(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(80, 30)

	task := makeScheduledTaskForDetail("abc12def", "Test task", "*/15 * * * *", true)
	sd.SetTask(task)

	view := sd.View()

	if !strings.Contains(view, "*/15 * * * *") {
		t.Errorf("expected schedule expression in view, got:\n%s", view)
	}
}

func TestScheduleDetail_ShowsEnabledYes(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(80, 30)

	task := makeScheduledTaskForDetail("abc12def", "Test task", "0 2 * * *", true)
	sd.SetTask(task)

	view := sd.View()

	if !strings.Contains(view, "yes") {
		t.Errorf("expected 'yes' for enabled task, got:\n%s", view)
	}
}

func TestScheduleDetail_ShowsEnabledNo(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(80, 30)

	task := makeScheduledTaskForDetail("abc12def", "Test task", "0 2 * * *", false)
	sd.SetTask(task)

	view := sd.View()

	if !strings.Contains(view, "no") {
		t.Errorf("expected 'no' for disabled task, got:\n%s", view)
	}
}

func TestScheduleDetail_ShowsEnabledNil(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:       "abc12def",
		Title:    "Test task",
		Schedule: "0 2 * * *",
		// ScheduleEnabled is nil — should be treated as not enabled
		Status:         "pending",
		Classification: "ready",
	}
	sd.SetTask(task)

	view := sd.View()

	if !strings.Contains(view, "no") {
		t.Errorf("expected 'no' when ScheduleEnabled is nil, got:\n%s", view)
	}
}

func TestScheduleDetail_ShowsStatus(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(80, 30)

	task := makeScheduledTaskForDetail("abc12def", "Test task", "0 2 * * *", true)
	task.Status = "active"
	task.Classification = "ready"
	sd.SetTask(task)

	view := sd.View()

	if !strings.Contains(view, "active") {
		t.Errorf("expected status 'active' in view, got:\n%s", view)
	}
}

func TestScheduleDetail_ShowsPriority(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(80, 30)

	task := makeScheduledTaskForDetail("abc12def", "Test task", "0 2 * * *", true)
	task.Priority = "high"
	sd.SetTask(task)

	view := sd.View()

	if !strings.Contains(view, "high") {
		t.Errorf("expected priority 'high' in view, got:\n%s", view)
	}
}

func TestScheduleDetail_ShowsProject(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(80, 30)

	task := makeScheduledTaskForDetail("abc12def", "Test task", "0 2 * * *", true)
	task.ProjectID = "my-project"
	sd.SetTask(task)

	view := sd.View()

	if !strings.Contains(view, "my-project") {
		t.Errorf("expected project 'my-project' in view, got:\n%s", view)
	}
}

func TestScheduleDetail_ShowsProjectNone(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(80, 30)

	task := makeScheduledTaskForDetail("abc12def", "Test task", "0 2 * * *", true)
	task.ProjectID = ""
	sd.SetTask(task)

	view := sd.View()

	if !strings.Contains(view, "none") {
		t.Errorf("expected 'none' when no project, got:\n%s", view)
	}
}

func TestScheduleDetail_ShowsPath(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(80, 30)

	task := makeScheduledTaskForDetail("abc12def", "Test task", "0 2 * * *", true)
	sd.SetTask(task)

	view := sd.View()

	if !strings.Contains(view, "projects/test/task/abc12def.md") {
		t.Errorf("expected path in view, got:\n%s", view)
	}
}

func TestScheduleDetail_ShowsCreated(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(80, 30)

	task := makeScheduledTaskForDetail("abc12def", "Test task", "0 2 * * *", true)
	sd.SetTask(task)

	view := sd.View()

	if !strings.Contains(view, "2025-01-15T10:00:00Z") {
		t.Errorf("expected created date in view, got:\n%s", view)
	}
}

// =============================================================================
// ScheduleDetail - Header with task
// =============================================================================

func TestScheduleDetail_WithTask_ShowsHeader(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(80, 30)

	task := makeScheduledTaskForDetail("abc12def", "Test task", "0 2 * * *", true)
	sd.SetTask(task)

	view := sd.View()

	if !strings.Contains(view, "Schedule Details") {
		t.Errorf("expected 'Schedule Details' header with task set, got:\n%s", view)
	}
}

// =============================================================================
// ScheduleDetail - Viewport Scrolling
// =============================================================================

func TestScheduleDetail_ScrollDown(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(80, 5) // Small viewport to force scrolling

	task := makeScheduledTaskForDetail("abc12def", "Test task", "0 2 * * *", true)
	task.Priority = "high"
	task.ProjectID = "my-project"
	sd.SetTask(task)

	sd.View() // Initial render to set totalLines

	sd.ScrollDown()
	view := sd.View()

	if !strings.Contains(view, "▲") {
		t.Errorf("expected up-arrow indicator after scrolling down, got:\n%s", view)
	}
}

func TestScheduleDetail_ScrollUp(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(80, 5)

	task := makeScheduledTaskForDetail("abc12def", "Test task", "0 2 * * *", true)
	task.Priority = "high"
	task.ProjectID = "my-project"
	sd.SetTask(task)

	sd.View()

	// Scroll down first, then up
	sd.ScrollDown()
	sd.ScrollDown()

	if sd.scrollOffset == 0 {
		t.Fatal("expected non-zero scrollOffset after scrolling down")
	}

	sd.ScrollUp()
	previousOffset := sd.scrollOffset

	sd.ScrollUp()
	if sd.scrollOffset >= previousOffset {
		// scrollOffset should decrease or stay at 0
		if sd.scrollOffset != 0 {
			t.Errorf("expected scrollOffset to decrease after ScrollUp, got %d", sd.scrollOffset)
		}
	}

	// Scroll all the way up
	sd.ScrollToTop()
	if sd.scrollOffset != 0 {
		t.Errorf("expected scrollOffset 0 after ScrollToTop, got %d", sd.scrollOffset)
	}
}

func TestScheduleDetail_SetTaskResetsScroll(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(80, 5)

	task := makeScheduledTaskForDetail("abc12def", "Test task", "0 2 * * *", true)
	task.Priority = "high"
	task.ProjectID = "my-project"
	sd.SetTask(task)

	sd.View()
	sd.ScrollDown()
	sd.ScrollDown()

	if sd.scrollOffset == 0 {
		t.Fatal("expected non-zero scrollOffset after scrolling")
	}

	// Setting a new task should reset scroll
	sd.SetTask(task)
	if sd.scrollOffset != 0 {
		t.Errorf("expected scrollOffset 0 after SetTask, got %d", sd.scrollOffset)
	}
}

func TestScheduleDetail_ScrollToBottom(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(80, 5)

	task := makeScheduledTaskForDetail("abc12def", "Test task", "0 2 * * *", true)
	task.Priority = "high"
	task.ProjectID = "my-project"
	sd.SetTask(task)

	sd.View() // Initial render to set totalLines

	sd.ScrollToBottom()
	view := sd.View()

	// At bottom, should not show down arrow
	if strings.Contains(view, "▼") {
		t.Errorf("should not show down-arrow at bottom, got:\n%s", view)
	}
}

func TestScheduleDetail_NoScrollWhenContentFits(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(80, 50) // Large viewport

	task := makeScheduledTaskForDetail("abc12def", "Test task", "0 2 * * *", true)
	sd.SetTask(task)

	view := sd.View()

	if strings.Contains(view, "▲") {
		t.Error("should not show up-arrow when content fits viewport")
	}
	if strings.Contains(view, "▼") {
		t.Error("should not show down-arrow when content fits viewport")
	}
}

// =============================================================================
// ScheduleDetail - New Scheduling Fields
// =============================================================================

func TestScheduleDetail_ShowsRunOnceAt(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(120, 30)

	task := makeScheduledTaskForDetail("abc12def", "One-shot task", "", true)
	task.Schedule = ""
	task.RunOnceAt = "2099-06-15T14:30:00Z"
	sd.SetTask(task)

	view := sd.View()

	if !strings.Contains(view, "Run Once At:") {
		t.Errorf("expected 'Run Once At:' label in view, got:\n%s", view)
	}
	if !strings.Contains(view, "2099-06-15T14:30:00Z") {
		t.Errorf("expected run_once_at timestamp in view, got:\n%s", view)
	}
}

func TestScheduleDetail_RunOnceAtShowsCountdown(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(120, 30)

	task := makeScheduledTaskForDetail("abc12def", "Future task", "", true)
	task.Schedule = ""
	task.RunOnceAt = "2099-01-15T10:00:00Z"
	sd.SetTask(task)

	view := sd.View()

	// Should contain a countdown like "(in Xd Xh)"
	if !strings.Contains(view, "(in ") {
		t.Errorf("expected countdown in parentheses for future run_once_at, got:\n%s", view)
	}
}

func TestScheduleDetail_RunOnceAtShowsPassed(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(120, 30)

	task := makeScheduledTaskForDetail("abc12def", "Past task", "", true)
	task.Schedule = ""
	task.RunOnceAt = "2020-01-01T00:00:00Z"
	sd.SetTask(task)

	view := sd.View()

	if !strings.Contains(view, "(passed)") {
		t.Errorf("expected '(passed)' for past run_once_at, got:\n%s", view)
	}
}

func TestScheduleDetail_ShowsTimezone(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(120, 30)

	task := makeScheduledTaskForDetail("abc12def", "TZ task", "0 2 * * *", true)
	task.Timezone = "America/New_York"
	sd.SetTask(task)

	view := sd.View()

	if !strings.Contains(view, "Timezone:") {
		t.Errorf("expected 'Timezone:' label in view, got:\n%s", view)
	}
	if !strings.Contains(view, "America/New_York") {
		t.Errorf("expected timezone value in view, got:\n%s", view)
	}
}

func TestScheduleDetail_HidesTimezoneWhenEmpty(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(120, 30)

	task := makeScheduledTaskForDetail("abc12def", "No TZ task", "0 2 * * *", true)
	task.Timezone = ""
	sd.SetTask(task)

	view := sd.View()

	if strings.Contains(view, "Timezone:") {
		t.Errorf("should NOT show 'Timezone:' when empty, got:\n%s", view)
	}
}

func TestScheduleDetail_ShowsTimeWindow(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(120, 30)

	task := makeScheduledTaskForDetail("abc12def", "Window task", "0 2 * * *", true)
	task.StartsAt = "2099-01-01T00:00:00Z"
	task.ExpiresAt = "2099-12-31T23:59:59Z"
	sd.SetTask(task)

	view := sd.View()

	if !strings.Contains(view, "Window:") {
		t.Errorf("expected 'Window:' label in view, got:\n%s", view)
	}
	if !strings.Contains(view, "2099-01-01T00:00:00Z") {
		t.Errorf("expected starts_at timestamp in view, got:\n%s", view)
	}
	if !strings.Contains(view, "2099-12-31T23:59:59Z") {
		t.Errorf("expected expires_at timestamp in view, got:\n%s", view)
	}
	if !strings.Contains(view, "→") {
		t.Errorf("expected arrow '→' in time window, got:\n%s", view)
	}
}

func TestScheduleDetail_ShowsTimeWindowStartsOnly(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(120, 30)

	task := makeScheduledTaskForDetail("abc12def", "Start only", "0 2 * * *", true)
	task.StartsAt = "2099-06-01T00:00:00Z"
	task.ExpiresAt = ""
	sd.SetTask(task)

	view := sd.View()

	if !strings.Contains(view, "Window:") {
		t.Errorf("expected 'Window:' label in view, got:\n%s", view)
	}
	if !strings.Contains(view, "2099-06-01T00:00:00Z") {
		t.Errorf("expected starts_at timestamp in view, got:\n%s", view)
	}
	// expires_at side should show "open"
	if !strings.Contains(view, "open") {
		t.Errorf("expected 'open' for missing expires_at, got:\n%s", view)
	}
}

func TestScheduleDetail_HidesTimeWindowWhenBothEmpty(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(120, 30)

	task := makeScheduledTaskForDetail("abc12def", "No window", "0 2 * * *", true)
	task.StartsAt = ""
	task.ExpiresAt = ""
	sd.SetTask(task)

	view := sd.View()

	if strings.Contains(view, "Window:") {
		t.Errorf("should NOT show 'Window:' when both starts_at and expires_at are empty, got:\n%s", view)
	}
}

func TestScheduleDetail_ShowsFeatureID(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(120, 30)

	task := makeScheduledTaskForDetail("abc12def", "Feature task", "0 2 * * *", true)
	task.FeatureID = "auth-v2"
	sd.SetTask(task)

	view := sd.View()

	if !strings.Contains(view, "Feature:") {
		t.Errorf("expected 'Feature:' label in view, got:\n%s", view)
	}
	if !strings.Contains(view, "auth-v2") {
		t.Errorf("expected feature ID 'auth-v2' in view, got:\n%s", view)
	}
}

func TestScheduleDetail_HidesFeatureIDWhenEmpty(t *testing.T) {
	sd := NewScheduleDetail()
	sd.SetSize(120, 30)

	task := makeScheduledTaskForDetail("abc12def", "No feature", "0 2 * * *", true)
	task.FeatureID = ""
	sd.SetTask(task)

	view := sd.View()

	if strings.Contains(view, "Feature:") {
		t.Errorf("should NOT show 'Feature:' when empty, got:\n%s", view)
	}
}

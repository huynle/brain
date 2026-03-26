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

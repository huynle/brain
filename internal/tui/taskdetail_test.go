package tui

import (
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// TaskDetail - No Task Selected
// =============================================================================

func TestTaskDetail_NoTask_ShowsPlaceholder(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(60, 20)

	view := td.View()

	if !strings.Contains(view, "No task selected") {
		t.Errorf("expected 'No task selected' placeholder, got:\n%s", view)
	}
}

func TestTaskDetail_NoTask_ShowsHeader(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(60, 20)

	view := td.View()

	if !strings.Contains(view, "Task Detail") {
		t.Errorf("expected 'Task Detail' header, got:\n%s", view)
	}
}

// =============================================================================
// TaskDetail - Task With All Fields
// =============================================================================

func TestTaskDetail_WithTask_ShowsTitle(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:             "abc12def",
		Title:          "Implement auth module",
		Status:         "pending",
		Priority:       "high",
		Path:           "projects/brain/task/abc12def.md",
		Classification: "ready",
	}
	td.SetTask(task)

	view := td.View()

	if !strings.Contains(view, "Implement auth module") {
		t.Errorf("expected task title in view, got:\n%s", view)
	}
}

func TestTaskDetail_WithTask_ShowsStatus(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:             "abc12def",
		Title:          "Test task",
		Status:         "pending",
		Classification: "ready",
	}
	td.SetTask(task)

	view := td.View()

	if !strings.Contains(view, "pending") {
		t.Errorf("expected status 'pending' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "ready") {
		t.Errorf("expected classification 'ready' in view, got:\n%s", view)
	}
}

func TestTaskDetail_WithTask_ShowsPriority(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:       "abc12def",
		Title:    "Test task",
		Priority: "high",
	}
	td.SetTask(task)

	view := td.View()

	if !strings.Contains(view, "high") {
		t.Errorf("expected priority 'high' in view, got:\n%s", view)
	}
}

func TestTaskDetail_WithTask_ShowsID(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:    "abc12def",
		Title: "Test task",
	}
	td.SetTask(task)

	view := td.View()

	if !strings.Contains(view, "abc12def") {
		t.Errorf("expected ID 'abc12def' in view, got:\n%s", view)
	}
}

func TestTaskDetail_WithTask_ShowsPath(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:    "abc12def",
		Title: "Test task",
		Path:  "projects/brain/task/abc12def.md",
	}
	td.SetTask(task)

	view := td.View()

	if !strings.Contains(view, "projects/brain/task/abc12def.md") {
		t.Errorf("expected path in view, got:\n%s", view)
	}
}

// =============================================================================
// TaskDetail - Dependencies
// =============================================================================

func TestTaskDetail_WithDependencies_ShowsDependsOn(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:        "abc12def",
		Title:     "Test task",
		DependsOn: []string{"dep1", "dep2"},
	}
	td.SetTask(task)

	view := td.View()

	if !strings.Contains(view, "Dependencies") {
		t.Errorf("expected 'Dependencies' section, got:\n%s", view)
	}
	if !strings.Contains(view, "dep1") {
		t.Errorf("expected 'dep1' in dependencies, got:\n%s", view)
	}
	if !strings.Contains(view, "dep2") {
		t.Errorf("expected 'dep2' in dependencies, got:\n%s", view)
	}
}

func TestTaskDetail_NoDependencies_NoDepsSection(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:    "abc12def",
		Title: "Test task",
	}
	td.SetTask(task)

	view := td.View()

	if strings.Contains(view, "Dependencies") {
		t.Errorf("expected no 'Dependencies' section when no deps, got:\n%s", view)
	}
}

// =============================================================================
// TaskDetail - Blocked Task
// =============================================================================

func TestTaskDetail_BlockedTask_ShowsBlockedBy(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:              "abc12def",
		Title:           "Blocked task",
		Classification:  "blocked",
		BlockedBy:       []string{"blocker1", "blocker2"},
		BlockedByReason: "dependency failed",
	}
	td.SetTask(task)

	view := td.View()

	if !strings.Contains(view, "Blocked by") {
		t.Errorf("expected 'Blocked by' section, got:\n%s", view)
	}
	if !strings.Contains(view, "blocker1") {
		t.Errorf("expected 'blocker1' in blocked by, got:\n%s", view)
	}
	if !strings.Contains(view, "dependency failed") {
		t.Errorf("expected blocked reason in view, got:\n%s", view)
	}
}

func TestTaskDetail_WaitingTask_ShowsWaitingOn(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:             "abc12def",
		Title:          "Waiting task",
		Classification: "waiting",
		WaitingOn:      []string{"wait1", "wait2"},
	}
	td.SetTask(task)

	view := td.View()

	if !strings.Contains(view, "Waiting on") {
		t.Errorf("expected 'Waiting on' section, got:\n%s", view)
	}
	if !strings.Contains(view, "wait1") {
		t.Errorf("expected 'wait1' in waiting on, got:\n%s", view)
	}
}

// =============================================================================
// TaskDetail - Cyclic Task
// =============================================================================

func TestTaskDetail_CyclicTask_ShowsCycleWarning(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:      "abc12def",
		Title:   "Cyclic task",
		InCycle: true,
	}
	td.SetTask(task)

	view := td.View()

	if !strings.Contains(view, "↺") {
		t.Errorf("expected cycle indicator '↺' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "cycle") {
		t.Errorf("expected 'cycle' warning text in view, got:\n%s", view)
	}
}

func TestTaskDetail_NoCycle_NoCycleWarning(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:      "abc12def",
		Title:   "Normal task",
		InCycle: false,
	}
	td.SetTask(task)

	view := td.View()

	if strings.Contains(view, "↺") {
		t.Errorf("expected no cycle indicator when InCycle=false, got:\n%s", view)
	}
}

// =============================================================================
// TaskDetail - SetSize
// =============================================================================

func TestTaskDetail_SetSize_UpdatesDimensions(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(100, 50)

	if td.width != 100 {
		t.Errorf("expected width 100, got %d", td.width)
	}
	if td.height != 50 {
		t.Errorf("expected height 50, got %d", td.height)
	}
}

// =============================================================================
// TaskDetail - Git Context
// =============================================================================

func TestTaskDetail_WithGitContext_ShowsBranch(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:        "abc12def",
		Title:     "Git task",
		GitBranch: "feature/auth",
		GitRemote: "origin",
	}
	td.SetTask(task)

	view := td.View()

	if !strings.Contains(view, "feature/auth") {
		t.Errorf("expected git branch in view, got:\n%s", view)
	}
}

// =============================================================================
// TaskDetail - Viewport Scrolling
// =============================================================================

func TestTaskDetail_ViewportScrolling(t *testing.T) {
	td := NewTaskDetail()
	// Very small height to force scrollable content
	td.SetSize(80, 5)

	task := &types.ResolvedTask{
		ID:              "abc12def",
		Title:           "Task with lots of info",
		Status:          "pending",
		Priority:        "high",
		Path:            "projects/brain/task/abc12def.md",
		Classification:  "blocked",
		DependsOn:       []string{"dep1", "dep2", "dep3"},
		BlockedBy:       []string{"blocker1"},
		BlockedByReason: "dependency failed",
		WaitingOn:       []string{"wait1"},
		GitBranch:       "feature/test",
		InCycle:         true,
	}
	td.SetTask(task)

	// Should render without panic
	view := td.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

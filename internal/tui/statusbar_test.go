package tui

import (
	"fmt"
	"strings"
	"testing"
)

func TestStatusBarHeight(t *testing.T) {
	sb := NewStatusBar("test-project")
	sb.Connected = true
	sb.Stats = TaskStats{
		Ready:      5,
		Waiting:    2,
		InProgress: 1,
		Completed:  10,
	}

	rendered := sb.View(80)
	lineCount := strings.Count(rendered, "\n") + 1

	if lineCount != 4 {
		t.Errorf("Status bar must be exactly 4 lines, got %d", lineCount)
	}
}

func TestStatusBarHeightWithBlockedTasks(t *testing.T) {
	sb := NewStatusBar("test-project")
	sb.Connected = true
	sb.Stats = TaskStats{
		Ready:      5,
		Waiting:    2,
		InProgress: 1,
		Completed:  10,
		Blocked:    3,
	}

	rendered := sb.View(80)
	lineCount := strings.Count(rendered, "\n") + 1

	if lineCount != 4 {
		t.Errorf("Status bar must be exactly 4 lines even with blocked tasks, got %d", lineCount)
	}
}

func TestStatusBarHeightWithMetrics(t *testing.T) {
	sb := NewStatusBar("test-project")
	sb.Connected = true
	sb.Stats = TaskStats{Ready: 5, Waiting: 2, InProgress: 1, Completed: 10}
	sb.Metrics = &ResourceMetrics{
		CPUPercent:   45.5,
		MemoryMB:     512,
		ProcessCount: 3,
	}

	rendered := sb.View(80)
	lineCount := strings.Count(rendered, "\n") + 1

	if lineCount != 4 {
		t.Errorf("Status bar must be exactly 4 lines with metrics, got %d", lineCount)
	}
}

func TestStatusBarHeightNarrowWidth(t *testing.T) {
	sb := NewStatusBar("test-project")
	sb.Connected = true
	sb.Stats = TaskStats{Ready: 5, Waiting: 2, InProgress: 1, Completed: 10}

	// Test with minimum width
	rendered := sb.View(30)
	lineCount := strings.Count(rendered, "\n") + 1

	if lineCount != 4 {
		t.Errorf("Status bar must be exactly 4 lines even with narrow width, got %d", lineCount)
	}
}

func TestStatusBarPauseIndicator(t *testing.T) {
	sb := NewStatusBar("test-project")
	sb.Connected = true
	sb.IsPaused = true
	sb.Stats = TaskStats{Ready: 5, Waiting: 2, InProgress: 1, Completed: 10}

	rendered := sb.View(120)

	if !strings.Contains(rendered, "⏸") {
		t.Error("Status bar should show pause indicator when paused")
	}

	// Ensure exactly 4 lines
	lineCount := strings.Count(rendered, "\n") + 1
	if lineCount != 4 {
		t.Errorf("Status bar must be exactly 4 lines with pause indicator, got %d", lineCount)
	}
}

func TestStatusBarActiveFeatureOverridesPause(t *testing.T) {
	sb := NewStatusBar("test-project")
	sb.Connected = true
	sb.IsPaused = true
	sb.ActiveFeatureCount = 2
	sb.Stats = TaskStats{Ready: 5, Waiting: 2, InProgress: 1, Completed: 10}

	rendered := sb.View(120)

	// Active features should take priority: show ▶2 instead of ⏸
	if !strings.Contains(rendered, "▶2") {
		t.Error("Status bar should show active feature count (▶2) when active features > 0")
	}
	if strings.Contains(rendered, "⏸") {
		t.Error("Status bar should NOT show pause indicator when active features > 0")
	}
}

func TestStatusBarPauseWithEnabledFeatures(t *testing.T) {
	sb := NewStatusBar("test-project")
	sb.Connected = true
	sb.IsPaused = true
	sb.EnabledFeatureCount = 3
	sb.Stats = TaskStats{Ready: 5, Waiting: 2, InProgress: 1, Completed: 10}

	rendered := sb.View(120)

	if !strings.Contains(rendered, "⏸") {
		t.Error("Status bar should show pause indicator when paused with no active features")
	}
	if !strings.Contains(rendered, "3 features enabled") {
		t.Error("Status bar should show enabled features count when paused")
	}
}

func TestStatusBarNoIndicatorsWhenNotPaused(t *testing.T) {
	sb := NewStatusBar("test-project")
	sb.Connected = true
	sb.IsPaused = false
	sb.ActiveFeatureCount = 0
	sb.EnabledFeatureCount = 5
	sb.Stats = TaskStats{Ready: 5, Waiting: 2, InProgress: 1, Completed: 10}

	rendered := sb.View(120)

	if strings.Contains(rendered, "⏸") {
		t.Error("Status bar should NOT show pause indicator when not paused")
	}
	// The ▶ character also appears as the "active" task indicator (e.g., "▶ 1 active"),
	// so check specifically for the active feature pattern ▶N (digit immediately after)
	for i := 0; i <= 9; i++ {
		if strings.Contains(rendered, fmt.Sprintf("▶%d", i)) {
			t.Error("Status bar should NOT show active feature indicator (▶N) when count is 0")
			break
		}
	}
	if strings.Contains(rendered, "features enabled") {
		t.Error("Status bar should NOT show enabled features when not paused")
	}
}

func TestStatusBarContentElements(t *testing.T) {
	sb := NewStatusBar("test-project")
	sb.Connected = true
	sb.Stats = TaskStats{Ready: 5, Waiting: 2, InProgress: 1, Completed: 10}

	rendered := sb.View(80)

	// Check that key elements are present
	if !strings.Contains(rendered, "test-project") {
		t.Error("Status bar should contain project name")
	}

	if !strings.Contains(rendered, "ready") {
		t.Error("Status bar should contain 'ready' stat")
	}

	if !strings.Contains(rendered, "waiting") {
		t.Error("Status bar should contain 'waiting' stat")
	}

	if !strings.Contains(rendered, "active") {
		t.Error("Status bar should contain 'active' stat")
	}

	if !strings.Contains(rendered, "done") {
		t.Error("Status bar should contain 'done' stat")
	}
}

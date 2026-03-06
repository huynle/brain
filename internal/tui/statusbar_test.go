package tui

import (
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

	if lineCount != 3 {
		t.Errorf("Status bar must be exactly 3 lines, got %d", lineCount)
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

	if lineCount != 3 {
		t.Errorf("Status bar must be exactly 3 lines even with blocked tasks, got %d", lineCount)
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

	if lineCount != 3 {
		t.Errorf("Status bar must be exactly 3 lines with metrics, got %d", lineCount)
	}
}

func TestStatusBarHeightNarrowWidth(t *testing.T) {
	sb := NewStatusBar("test-project")
	sb.Connected = true
	sb.Stats = TaskStats{Ready: 5, Waiting: 2, InProgress: 1, Completed: 10}

	// Test with minimum width
	rendered := sb.View(30)
	lineCount := strings.Count(rendered, "\n") + 1

	if lineCount != 3 {
		t.Errorf("Status bar must be exactly 3 lines even with narrow width, got %d", lineCount)
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

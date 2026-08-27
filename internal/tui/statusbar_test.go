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

	// Without metrics: border-top + content-row + border-bottom = 3 lines
	if lineCount != 3 {
		t.Errorf("Status bar without metrics must be exactly 3 lines, got %d", lineCount)
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

	// Use wider width to accommodate "inactive" label (longer than "done")
	rendered := sb.View(100)
	lineCount := strings.Count(rendered, "\n") + 1

	// Without metrics: border-top + content-row + border-bottom = 3 lines
	if lineCount != 3 {
		t.Errorf("Status bar without metrics must be exactly 3 lines even with blocked tasks, got %d", lineCount)
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

	// Test with minimum width (no metrics)
	rendered := sb.View(30)
	lineCount := strings.Count(rendered, "\n") + 1

	// At narrow width, content may wrap — minimum is 3 lines (border + content + border)
	if lineCount < 3 {
		t.Errorf("Status bar must be at least 3 lines even with narrow width, got %d", lineCount)
	}

	// Verify it still renders key content
	if !strings.Contains(rendered, "test-project") {
		t.Error("Narrow status bar should still contain project name")
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

	// Ensure exactly 3 lines (no metrics)
	lineCount := strings.Count(rendered, "\n") + 1
	if lineCount != 3 {
		t.Errorf("Status bar without metrics must be exactly 3 lines with pause indicator, got %d", lineCount)
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

func TestStatusBarNoIndicatorsWhenNotPaused(t *testing.T) {
	sb := NewStatusBar("test-project")
	sb.Connected = true
	sb.IsPaused = false
	sb.ActiveFeatureCount = 0
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

func TestStatusBarUsesSquareCorners(t *testing.T) {
	sb := NewStatusBar("test-project")
	sb.Connected = true
	sb.Stats = TaskStats{Ready: 1}

	rendered := sb.View(80)

	// Square corners (NormalBorder) use ┌ and ┐
	if !strings.Contains(rendered, "┌") {
		t.Error("Status bar should use square top-left corner ┌, not rounded ╭")
	}
	if !strings.Contains(rendered, "┐") {
		t.Error("Status bar should use square top-right corner ┐, not rounded ╮")
	}
	if !strings.Contains(rendered, "└") {
		t.Error("Status bar should use square bottom-left corner └, not rounded ╰")
	}
	if !strings.Contains(rendered, "┘") {
		t.Error("Status bar should use square bottom-right corner ┘, not rounded ╯")
	}
	// Ensure rounded corners are NOT present
	if strings.Contains(rendered, "╭") || strings.Contains(rendered, "╮") ||
		strings.Contains(rendered, "╰") || strings.Contains(rendered, "╯") {
		t.Error("Status bar should NOT use rounded corners")
	}
}

func TestStatusBarNoBlankLineWithoutMetrics(t *testing.T) {
	sb := NewStatusBar("test-project")
	sb.Connected = true
	sb.Stats = TaskStats{Ready: 5, Waiting: 2, InProgress: 1, Completed: 10}
	// Metrics is nil — no second row should render

	rendered := sb.View(80)
	lines := strings.Split(rendered, "\n")

	// With border: line 0 = top border, line 1 = content, line 2 = bottom border
	// There should be no blank/space-only line between content and bottom border
	if len(lines) != 3 {
		t.Errorf("Expected 3 lines without metrics, got %d", len(lines))
	}
}

func TestStatusBarSecondRowRendersWithMetrics(t *testing.T) {
	sb := NewStatusBar("test-project")
	sb.Connected = true
	sb.Stats = TaskStats{Ready: 1}
	sb.Metrics = &ResourceMetrics{
		CPUPercent:   25.0,
		MemoryMB:     256,
		ProcessCount: 2,
	}

	rendered := sb.View(80)
	lines := strings.Split(rendered, "\n")

	// With metrics: top border + row1 + row2 + bottom border = 4 lines
	if len(lines) != 4 {
		t.Errorf("Expected 4 lines with metrics, got %d", len(lines))
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

	if !strings.Contains(rendered, "inactive") {
		t.Error("Status bar should contain 'inactive' stat")
	}
}

func TestStatusBarShowsEmbeddingStatusBesideBrainConnection(t *testing.T) {
	sb := NewStatusBar("test-project")
	sb.Connected = true
	sb.EmbeddingReady = true

	ready := sb.View(100)
	if !strings.Contains(ready, "brain") {
		t.Fatalf("expected status bar to label brain connection, got:\n%s", ready)
	}
	if !strings.Contains(ready, "emb") {
		t.Fatalf("expected status bar to label embedding connection, got:\n%s", ready)
	}

	sb.EmbeddingReady = false
	degraded := sb.View(100)
	if !strings.Contains(degraded, "brain") || !strings.Contains(degraded, "emb") {
		t.Fatalf("expected degraded status bar to keep both labels, got:\n%s", degraded)
	}
}

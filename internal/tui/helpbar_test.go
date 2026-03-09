package tui

import (
	"strings"
	"testing"
)

func TestHelpBar_View_ShowsPauseShortcut_Always(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelTasks

	view := h.View(120, false)

	// Pause shortcut should appear regardless of panel (matching TypeScript behavior)
	if !strings.Contains(view, "Pause") {
		t.Errorf("expected help bar to contain 'Pause' shortcut, got:\n%s", view)
	}
	if !strings.Contains(view, "p") {
		t.Errorf("expected help bar to contain 'p' key for pause, got:\n%s", view)
	}
}

func TestHelpBar_View_ShowsPauseShortcut_OnDetailsPanel(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelDetails

	view := h.View(120, false)

	// Pause shortcut should appear even on non-task panels (matching TypeScript behavior)
	if !strings.Contains(view, "Pause") {
		t.Errorf("expected help bar to contain 'Pause' shortcut even on details panel, got:\n%s", view)
	}
}

// Pause indicator tests removed - TypeScript HelpBar doesn't show PAUSED indicators
// (pause state is shown in StatusBar instead)

func TestHelpBar_View_ShowsSessionShortcuts_WhenHasTaskSessions(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelTasks
	h.HasTaskSessions = true

	view := h.View(120, false)

	if !strings.Contains(view, "Session") {
		t.Errorf("expected help bar to contain 'Session' shortcut when HasTaskSessions=true, got:\n%s", view)
	}
	if !strings.Contains(view, "Tmux") {
		t.Errorf("expected help bar to contain 'Tmux' shortcut when HasTaskSessions=true, got:\n%s", view)
	}
}

func TestHelpBar_View_NoSessionShortcuts_WhenNoTaskSessions(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelTasks
	h.HasTaskSessions = false

	view := h.View(120, false)

	if strings.Contains(view, "Session") {
		t.Errorf("expected help bar NOT to contain 'Session' when HasTaskSessions=false, got:\n%s", view)
	}
	if strings.Contains(view, "Tmux") {
		t.Errorf("expected help bar NOT to contain 'Tmux' when HasTaskSessions=false, got:\n%s", view)
	}
}

func TestHelpBar_View_NoSessionShortcuts_WhenNotTaskPanel(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelDetails
	h.HasTaskSessions = true

	view := h.View(120, false)

	// Session shortcuts should only appear on task panel
	if strings.Contains(view, "Session") {
		t.Errorf("expected help bar NOT to contain 'Session' on non-task panel, got:\n%s", view)
	}
	if strings.Contains(view, "Tmux") {
		t.Errorf("expected help bar NOT to contain 'Tmux' on non-task panel, got:\n%s", view)
	}
}

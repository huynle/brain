package tui

import (
	"strings"
	"testing"
)

func TestHelpBar_View_ShowsPauseShortcut_WhenTaskPanel(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelTasks

	view := h.View(120, false)

	if !strings.Contains(view, "Pause") {
		t.Errorf("expected help bar to contain 'Pause' shortcut when on task panel, got:\n%s", view)
	}
	if !strings.Contains(view, "p") {
		t.Errorf("expected help bar to contain 'p' key for pause, got:\n%s", view)
	}
}

func TestHelpBar_View_NoPauseShortcut_WhenNotTaskPanel(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelDetails

	view := h.View(120, false)

	// "Pause" should not appear when not on task panel
	// (but "p" might appear in other text, so only check "Pause")
	if strings.Contains(view, "Pause") {
		t.Errorf("expected help bar NOT to contain 'Pause' when on details panel, got:\n%s", view)
	}
}

func TestHelpBar_View_ShowsPausedIndicator_WhenProjectPaused(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelTasks
	h.IsPaused = true

	view := h.View(120, false)

	if !strings.Contains(view, "PAUSED") {
		t.Errorf("expected help bar to show 'PAUSED' indicator when project is paused, got:\n%s", view)
	}
}

func TestHelpBar_View_ShowsAllPausedIndicator_WhenAllPaused(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelTasks
	h.AllPaused = true

	view := h.View(120, false)

	if !strings.Contains(view, "ALL PAUSED") {
		t.Errorf("expected help bar to show 'ALL PAUSED' indicator when all projects paused, got:\n%s", view)
	}
}

func TestHelpBar_View_AllPausedTakesPrecedence_OverProjectPaused(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelTasks
	h.IsPaused = true
	h.AllPaused = true

	view := h.View(120, false)

	if !strings.Contains(view, "ALL PAUSED") {
		t.Errorf("expected 'ALL PAUSED' to take precedence, got:\n%s", view)
	}
}

func TestHelpBar_View_NoPausedIndicator_WhenNotPaused(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelTasks
	h.IsPaused = false
	h.AllPaused = false

	view := h.View(120, false)

	if strings.Contains(view, "PAUSED") {
		t.Errorf("expected no PAUSED indicator when not paused, got:\n%s", view)
	}
}

func TestHelpBar_PauseIndicator_ShowsOnNonTaskPanels(t *testing.T) {
	// Pause indicator should show regardless of active panel
	h := NewHelpBar()
	h.ActivePanel = PanelDetails
	h.IsPaused = true

	view := h.View(120, false)

	if !strings.Contains(view, "PAUSED") {
		t.Errorf("expected PAUSED indicator even on non-task panel, got:\n%s", view)
	}
}

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

package tui

import (
	"strings"
	"testing"
)

func TestHelpBar_View_ShowsPauseShortcut_Always(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelTasks

	view := h.View(120, false, "test-project")

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

	view := h.View(120, false, "test-project")

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

	view := h.View(120, false, "test-project")

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

	view := h.View(120, false, "test-project")

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

	view := h.View(120, false, "test-project")

	// Session shortcuts should only appear on task panel
	if strings.Contains(view, "Session") {
		t.Errorf("expected help bar NOT to contain 'Session' on non-task panel, got:\n%s", view)
	}
	if strings.Contains(view, "Tmux") {
		t.Errorf("expected help bar NOT to contain 'Tmux' on non-task panel, got:\n%s", view)
	}
}

func TestHelpBar_View_ShowsProjectNameInPause_SingleProject(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelTasks

	view := h.View(120, false, "brain-api")

	// Should show project name in pause command
	if !strings.Contains(view, "Pause (brain-api)") {
		t.Errorf("expected help bar to contain 'Pause (brain-api)' in single-project mode, got:\n%s", view)
	}
}

func TestHelpBar_View_ShowsGenericPause_WhenNoProjectName(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelTasks

	view := h.View(120, false, "")

	// Should show generic pause when no project name
	if !strings.Contains(view, "Pause (project)") {
		t.Errorf("expected help bar to contain 'Pause (project)' when no project name, got:\n%s", view)
	}
}

func TestHelpBar_View_ShowsProjectAll_MultiProject(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelTasks

	view := h.View(120, true, "brain-api")

	// Should show project/all in multi-project mode (project name ignored)
	if !strings.Contains(view, "Pause (project/all)") {
		t.Errorf("expected help bar to contain 'Pause (project/all)' in multi-project mode, got:\n%s", view)
	}
}

func TestHelpBar_View_RunnersTabShowsShutdownShortcut(t *testing.T) {
	h := NewHelpBar()
	h.ActiveContentTab = ContentTabRunners

	view := h.View(120, false, "brain-api")

	if !strings.Contains(view, "Shutdown") {
		t.Fatalf("expected runners tab help to contain Shutdown shortcut, got:\n%s", view)
	}
	if !strings.Contains(view, "s") {
		t.Fatalf("expected runners tab help to contain s key for shutdown, got:\n%s", view)
	}
}

func TestHelpBar_View_BrainTabShowsEntryShortcuts(t *testing.T) {
	h := NewHelpBar()
	h.ActiveContentTab = ContentTabBrain

	view := h.View(120, false, "brain-api")

	for _, want := range []string{"Navigate", "Edit", "Refresh", "Brain"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected Brain help to contain %q, got:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"Execute", "Checkout", "Pause"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("expected Brain help to omit task shortcut %q, got:\n%s", unwanted, view)
		}
	}
}

func TestHelpBar_View_RunnersPanelShowsShutdownShortcut(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelRunners

	view := h.View(120, false, "brain-api")

	if !strings.Contains(view, "Shutdown") {
		t.Fatalf("expected runners panel help to contain Shutdown shortcut, got:\n%s", view)
	}
	if !strings.Contains(view, "s") {
		t.Fatalf("expected runners panel help to contain s key for shutdown, got:\n%s", view)
	}
}

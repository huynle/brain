package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/huynle/brain-api/internal/types"
)

func TestAutomationDetailDisplaysGeneratedTaskSessionAvailability(t *testing.T) {
	m := automationSessionTestModel([]types.BrainEntry{
		{
			ID:              "task1",
			Path:            "projects/demo/task/task1.md",
			Title:           "Review blocked task",
			Type:            "task",
			Status:          "completed",
			GeneratedBy:     "automation:auto1",
			AutomationRunID: "run1",
			Sessions: map[string]types.SessionInfo{
				"ses_older": {Timestamp: "2026-06-10T20:00:00Z"},
				"ses_newer": {Timestamp: "2026-06-10T21:00:00Z"},
			},
		},
	})

	content := m.automationDetailContent("projects/demo/automation/auto1.md", "# Automation")

	for _, want := range []string{
		"task1 [completed] Review blocked task",
		"generated_by=automation:auto1",
		"automation_run_id=run1",
		"path=projects/demo/task/task1.md",
		"session=ses_newer (o: open, O: tmux)",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected automation detail to contain %q, got:\n%s", want, content)
		}
	}
}

func TestAutomationListEnterExpandsRunsAndOpenKeyUsesSelectedRun(t *testing.T) {
	m := automationSessionTestModel([]types.BrainEntry{
		{
			ID:              "older",
			Path:            "projects/demo/task/older.md",
			Title:           "Older generated task",
			Type:            "task",
			Status:          "completed",
			GeneratedBy:     "automation:auto1",
			AutomationRunID: "run-old",
			Modified:        "2026-06-10T20:00:00Z",
			Sessions: map[string]types.SessionInfo{
				"ses_older": {Timestamp: "2026-06-10T20:01:00Z"},
			},
		},
		{
			ID:              "newer",
			Path:            "projects/demo/task/newer.md",
			Title:           "Newer generated task",
			Type:            "task",
			Status:          "completed",
			GeneratedBy:     "automation:auto1",
			AutomationRunID: "run-new",
			Modified:        "2026-06-10T21:00:00Z",
			Sessions: map[string]types.SessionInfo{
				"ses_newest": {Timestamp: "2026-06-10T21:02:00Z"},
				"ses_newer":  {Timestamp: "2026-06-10T21:01:00Z"},
			},
		},
	})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	view := m.automationList.View(140, 20)
	for _, want := range []string{"newer [completed]", "older [completed]", "session=ses_newest"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected expanded automation list to contain %q, got:\n%s", want, view)
		}
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if cmd == nil {
		t.Fatal("expected o on selected automation run line to return a session command")
	}
	msg := cmd()
	fetched, ok := msg.(sessionsFetchedMsg)
	if !ok {
		t.Fatalf("expected sessionsFetchedMsg, got %T", msg)
	}
	if fetched.taskPath != "projects/demo/task/newer.md" {
		t.Fatalf("taskPath = %q, want newest generated task path", fetched.taskPath)
	}
	if fetched.tmuxMode {
		t.Fatal("expected fullscreen mode for o")
	}
	if got, want := strings.Join(fetched.sessionIDs, ","), "ses_newest,ses_newer"; got != want {
		t.Fatalf("sessionIDs = %q, want %q", got, want)
	}
}

func TestAutomationListOpenKeyUsesParentRunFinalizationSession(t *testing.T) {
	m := automationSessionTestModelWithAutomation(types.BrainEntry{
		ID:     "auto1",
		Path:   "projects/demo/automation/auto1.md",
		Title:  "Automation auto1",
		Type:   "automation",
		Status: "active",
		RunFinalizations: map[string]types.RunFinalization{
			"run1": {Status: "completed", FinalizedAt: "2026-06-10T21:00:00Z", SessionID: "ses_finalized"},
		},
	}, []types.BrainEntry{
		{
			ID:              "task1",
			Path:            "projects/demo/task/task1.md",
			Title:           "Generated task",
			Type:            "task",
			Status:          "completed",
			GeneratedBy:     "automation:auto1",
			AutomationRunID: "run1",
		},
	})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	view := m.automationList.View(140, 20)
	if !strings.Contains(view, "session=ses_finalized") {
		t.Fatalf("expected generated task row to show finalized session, got:\n%s", view)
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if cmd == nil {
		t.Fatal("expected o to open finalized session")
	}
	fetched, ok := cmd().(sessionsFetchedMsg)
	if !ok {
		t.Fatalf("expected sessionsFetchedMsg, got %T", cmd())
	}
	if got, want := strings.Join(fetched.sessionIDs, ","), "ses_finalized"; got != want {
		t.Fatalf("sessionIDs = %q, want %q", got, want)
	}
	if fetched.tmuxMode {
		t.Fatal("expected fullscreen mode for o")
	}

	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("O")})
	if cmd == nil {
		t.Fatal("expected O to open finalized session")
	}
	fetched = cmd().(sessionsFetchedMsg)
	if !fetched.tmuxMode {
		t.Fatal("expected tmux mode for O")
	}
}

func TestAutomationListRunSelectionChoosesSpecificRun(t *testing.T) {
	m := automationSessionTestModel([]types.BrainEntry{
		{
			ID:              "older",
			Path:            "projects/demo/task/older.md",
			Title:           "Older generated task",
			Type:            "task",
			Status:          "completed",
			GeneratedBy:     "automation:auto1",
			AutomationRunID: "run-old",
			Modified:        "2026-06-10T20:00:00Z",
			Sessions: map[string]types.SessionInfo{
				"ses_older": {Timestamp: "2026-06-10T20:01:00Z"},
			},
		},
		{
			ID:              "newer",
			Path:            "projects/demo/task/newer.md",
			Title:           "Newer generated task",
			Type:            "task",
			Status:          "completed",
			GeneratedBy:     "automation:auto1",
			AutomationRunID: "run-new",
			Modified:        "2026-06-10T21:00:00Z",
			Sessions: map[string]types.SessionInfo{
				"ses_newer": {Timestamp: "2026-06-10T21:01:00Z"},
			},
		},
	})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	model := updated.(Model)
	view := model.automationList.View(140, 20)
	if !strings.Contains(view, "▸ older [completed] Older generated task") {
		t.Fatalf("expected older generated task run line to be selected after j, got:\n%s", view)
	}

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if cmd == nil {
		t.Fatal("expected o to open selected generated task session")
	}
	fetched, ok := cmd().(sessionsFetchedMsg)
	if !ok {
		t.Fatalf("expected sessionsFetchedMsg, got %T", cmd())
	}
	if fetched.taskPath != "projects/demo/task/older.md" {
		t.Fatalf("taskPath = %q, want selected generated task path", fetched.taskPath)
	}
	if got, want := strings.Join(fetched.sessionIDs, ","), "ses_older"; got != want {
		t.Fatalf("sessionIDs = %q, want %q", got, want)
	}
}

func TestAutomationListSelectedRunExecuteKeyOpensTaskExecuteModal(t *testing.T) {
	m := automationSessionTestModel([]types.BrainEntry{
		{ID: "run-task", Path: "projects/demo/task/run-task.md", Title: "Generated task", Type: "task", Status: "pending", GeneratedBy: "automation:auto1", AutomationRunID: "run1"},
	})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	model := updated.(Model)
	if !model.modalManager.IsOpen() {
		t.Fatal("expected execute modal to be open")
	}
	if got, want := model.modalManager.ActiveModal().Title(), "Execute Task"; got != want {
		t.Fatalf("modal title = %q, want %q", got, want)
	}
	if view := model.modalManager.ActiveModal().View(); !strings.Contains(view, "Generated task") {
		t.Fatalf("expected execute modal to reference selected generated task, got:\n%s", view)
	}
}

func TestAutomationListSelectedRunDeleteKeyOpensTaskDeleteModal(t *testing.T) {
	m := automationSessionTestModel([]types.BrainEntry{
		{ID: "run-task", Path: "projects/demo/task/run-task.md", Title: "Generated task", Type: "task", Status: "pending", GeneratedBy: "automation:auto1", AutomationRunID: "run1"},
	})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	model := updated.(Model)
	if !model.modalManager.IsOpen() {
		t.Fatal("expected delete modal to be open")
	}
	if got, want := model.modalManager.ActiveModal().Title(), "Delete Task"; got != want {
		t.Fatalf("modal title = %q, want %q", got, want)
	}
	if view := model.modalManager.ActiveModal().View(); !strings.Contains(view, "Generated task") {
		t.Fatalf("expected delete modal to reference selected generated task, got:\n%s", view)
	}
}

func TestAutomationDetailSelectionRerendersVisibleDetail(t *testing.T) {
	t.Skip("run selection moved from detail pane into the automation list")

	m := automationSessionTestModel([]types.BrainEntry{
		{ID: "newer", Path: "projects/demo/task/newer.md", Title: "Newer generated task", Type: "task", Status: "completed", GeneratedBy: "automation:auto1", Modified: "2026-06-10T21:00:00Z", Sessions: map[string]types.SessionInfo{"ses_newer": {Timestamp: "2026-06-10T21:01:00Z"}}},
		{ID: "older", Path: "projects/demo/task/older.md", Title: "Older generated task", Type: "task", Status: "completed", GeneratedBy: "automation:auto1", Modified: "2026-06-10T20:00:00Z", Sessions: map[string]types.SessionInfo{"ses_older": {Timestamp: "2026-06-10T20:01:00Z"}}},
	})
	m.taskDetail.SetEntryContent("projects/demo/automation/auto1.md", "Automation auto1", "automation", m.automationDetailContent("projects/demo/automation/auto1.md", "# Automation"), "Automation Detail")
	m.goalDetailRaw = map[string]string{"projects/demo/automation/auto1.md": "# Automation"}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	model := updated.(Model)
	view := model.taskDetail.View()
	if !strings.Contains(view, "▸ older [completed] Older generated task") {
		t.Fatalf("expected visible detail to move cursor to older task after j, got:\n%s", view)
	}
}

func TestAutomationDetailOpenTmuxKeyUsesTmuxMode(t *testing.T) {
	m := automationSessionTestModel([]types.BrainEntry{
		{
			ID:          "task1",
			Path:        "projects/demo/task/task1.md",
			Title:       "Generated task",
			Type:        "task",
			Status:      "completed",
			GeneratedBy: "automation:auto1",
			Sessions: map[string]types.SessionInfo{
				"ses_one": {Timestamp: "2026-06-10T21:00:00Z"},
			},
		},
	})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("O")})
	if cmd == nil {
		t.Fatal("expected O in automation detail to return a session command")
	}
	fetched, ok := cmd().(sessionsFetchedMsg)
	if !ok {
		t.Fatalf("expected sessionsFetchedMsg")
	}
	if !fetched.tmuxMode {
		t.Fatal("expected tmux mode for O")
	}
}

func TestAutomationDetailOpenKeyWithoutLoadedSessionsFetchesPersistedSessions(t *testing.T) {
	m := automationSessionTestModel([]types.BrainEntry{
		{
			ID:          "script1",
			Path:        "projects/demo/task/script1.md",
			Title:       "Script generated task",
			Type:        "task",
			Status:      "completed",
			GeneratedBy: "automation:auto1",
			Executor:    "script",
		},
	})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if cmd == nil {
		t.Fatal("expected command to fetch persisted sessions for generated task")
	}

	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("O")})
	if cmd == nil {
		t.Fatal("expected command to fetch persisted sessions in tmux mode for generated task")
	}
}

func TestAutomationDetailOpenKeyWithoutTaskPathWarnsAndDoesNotOpen(t *testing.T) {
	m := automationSessionTestModel([]types.BrainEntry{
		{
			ID:          "script1",
			Title:       "Script generated task",
			Type:        "task",
			Status:      "completed",
			GeneratedBy: "automation:auto1",
			Executor:    "script",
		},
	})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if cmd != nil {
		t.Fatal("expected no command when generated task has no path")
	}
	updatedModel := updated.(Model)
	if updatedModel.statusMessageType != "warn" {
		t.Fatalf("statusMessageType = %q, want warn", updatedModel.statusMessageType)
	}
	if !strings.Contains(updatedModel.statusMessage, "No sessions available for generated tasks") {
		t.Fatalf("unexpected status message: %q", updatedModel.statusMessage)
	}
}

func automationSessionTestModel(generatedTasks []types.BrainEntry) Model {
	automation := types.BrainEntry{
		ID:     "auto1",
		Path:   "projects/demo/automation/auto1.md",
		Title:  "Automation auto1",
		Type:   "automation",
		Status: "active",
	}
	return automationSessionTestModelWithAutomation(automation, generatedTasks)
}

func automationSessionTestModelWithAutomation(automation types.BrainEntry, generatedTasks []types.BrainEntry) Model {
	m := NewModel(Config{Project: "demo", APIURL: "http://127.0.0.1:1"})
	m.activeContentTab = ContentTabAutomation
	m.activeAutomationSubTab = AutomationSubTabAutomations
	m.activePanel = PanelTasks
	m.detailVisible = true
	m.automationGeneratedTasks = generatedTasks
	m.automationList.SetEntryRows([]types.BrainEntry{automation}, nil, generatedTasks)
	return m
}

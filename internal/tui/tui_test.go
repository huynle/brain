package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Init Tests
// =============================================================================

func TestNewModel_Init_ReturnsSSEConnectCmd(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a non-nil command (SSE connect)")
	}
}

func TestNewModel_DefaultState(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Project:  "test-project",
		Projects: []string{"test-project"},
	}
	m := NewModel(cfg)

	if m.activePanel != PanelTasks {
		t.Errorf("expected default panel to be PanelTasks, got %v", m.activePanel)
	}
	if m.connected {
		t.Error("expected connected to be false initially")
	}
	if m.width != 0 || m.height != 0 {
		t.Errorf("expected initial dimensions 0x0, got %dx%d", m.width, m.height)
	}
	if m.sseClient == nil {
		t.Error("expected sseClient to be initialized")
	}
}

// =============================================================================
// Update Tests - Quit
// =============================================================================

func TestUpdate_QuitOnQ(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Fatal("expected quit command, got nil")
	}

	// Execute the command and check it produces a QuitMsg
	resultMsg := cmd()
	if _, ok := resultMsg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", resultMsg)
	}
}

func TestUpdate_QuitOnCtrlC(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	msg := tea.KeyMsg{Type: tea.KeyCtrlC}
	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Fatal("expected quit command, got nil")
	}

	resultMsg := cmd()
	if _, ok := resultMsg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", resultMsg)
	}
}

// =============================================================================
// Update Tests - Window Resize
// =============================================================================

func TestUpdate_WindowResize(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	msg := tea.WindowSizeMsg{Width: 120, Height: 40}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	if model.width != 120 {
		t.Errorf("expected width 120, got %d", model.width)
	}
	if model.height != 40 {
		t.Errorf("expected height 40, got %d", model.height)
	}
}

// =============================================================================
// Update Tests - Tab Panel Switching
// =============================================================================

func TestUpdate_TabSwitchesPanel(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	if m.activePanel != PanelTasks {
		t.Fatalf("expected initial panel PanelTasks, got %v", m.activePanel)
	}

	// Tab should cycle through panels. Since detail and logs are not visible
	// by default, it should stay on tasks.
	msg := tea.KeyMsg{Type: tea.KeyTab}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	// With no bottom panels visible, Tab cycles back to tasks
	if model.activePanel != PanelTasks {
		t.Errorf("expected panel to stay PanelTasks (no other panels visible), got %v", model.activePanel)
	}
}

// =============================================================================
// Update Tests - SSE Messages
// =============================================================================

func TestUpdate_SSEConnected_SetsConnectedTrue(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	if m.connected {
		t.Fatal("expected connected to be false initially")
	}

	updated, cmd := m.Update(SSEConnectedMsg{})
	model := updated.(Model)

	if !model.connected {
		t.Error("expected connected to be true after SSEConnectedMsg")
	}
	if !model.statusBar.Connected {
		t.Error("expected statusBar.Connected to be true")
	}
	// Should return a continuation command to wait for next message
	if cmd == nil {
		t.Error("expected non-nil command (wait for next SSE message)")
	}
}

func TestUpdate_SSEDisconnected_SetsConnectedFalse(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.connected = true
	m.statusBar.Connected = true

	updated, cmd := m.Update(SSEDisconnectedMsg{})
	model := updated.(Model)

	if model.connected {
		t.Error("expected connected to be false after SSEDisconnectedMsg")
	}
	if model.statusBar.Connected {
		t.Error("expected statusBar.Connected to be false")
	}
	// Should return a reconnect command
	if cmd == nil {
		t.Error("expected non-nil command (reconnect)")
	}
}

func TestUpdate_TasksUpdated_StoresTasksAndStats(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Classification: "ready"},
		{ID: "t2", Title: "Task 2", Classification: "waiting"},
	}
	stats := &types.TaskStats{
		Total:      2,
		Ready:      1,
		Waiting:    1,
		Blocked:    0,
		NotPending: 0,
	}

	updated, cmd := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: stats})
	model := updated.(Model)

	if len(model.tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(model.tasks))
	}
	if model.tasks[0].ID != "t1" {
		t.Errorf("expected first task ID 't1', got '%s'", model.tasks[0].ID)
	}
	if model.stats.Ready != 1 {
		t.Errorf("expected 1 ready, got %d", model.stats.Ready)
	}
	if model.stats.Waiting != 1 {
		t.Errorf("expected 1 waiting, got %d", model.stats.Waiting)
	}
	if model.statusBar.Stats.Ready != 1 {
		t.Errorf("expected statusBar stats ready=1, got %d", model.statusBar.Stats.Ready)
	}
	// Should return a continuation command
	if cmd == nil {
		t.Error("expected non-nil command (wait for next SSE message)")
	}
}

func TestUpdate_SSEError_ReturnsContinuationCmd(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	updated, cmd := m.Update(SSEErrorMsg{Err: nil})
	_ = updated.(Model)

	// Should continue listening despite error
	if cmd == nil {
		t.Error("expected non-nil command (continue listening after error)")
	}
}

func TestUpdate_ReconnectMsg_CreatesNewSSEClient(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	originalClient := m.sseClient

	updated, cmd := m.Update(reconnectMsg{})
	model := updated.(Model)

	// Should have a new SSE client (not the same pointer)
	if model.sseClient == originalClient {
		t.Error("expected new SSE client after reconnect")
	}
	// Should return a connect command
	if cmd == nil {
		t.Error("expected non-nil command (SSE connect)")
	}
}

func TestUpdate_RefreshKey_ReconnectsSSE(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	originalClient := m.sseClient

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}
	updated, cmd := m.Update(msg)
	model := updated.(Model)

	// Should have a new SSE client
	if model.sseClient == originalClient {
		t.Error("expected new SSE client after refresh")
	}
	// Should return a connect command
	if cmd == nil {
		t.Error("expected non-nil command (SSE connect after refresh)")
	}
}

// =============================================================================
// View Tests
// =============================================================================

func TestView_ContainsProjectName(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "my-project",
	}
	m := NewModel(cfg)
	// Set dimensions so View renders properly
	m.width = 80
	m.height = 24

	view := m.View()

	if !strings.Contains(view, "my-project") {
		t.Errorf("expected view to contain project name 'my-project', got:\n%s", view)
	}
}

func TestView_ContainsTaskPanel(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 40

	view := m.View()

	// Empty task tree should show "No tasks" placeholder
	if !strings.Contains(view, "No tasks") {
		t.Errorf("expected view to contain 'No tasks' placeholder, got:\n%s", view)
	}
}

func TestView_ContainsHelpBar(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 40

	view := m.View()

	// Help bar should show quit shortcut
	if !strings.Contains(view, "Quit") {
		t.Errorf("expected view to contain 'Quit' in help bar, got:\n%s", view)
	}
}

func TestView_ContainsConnectionStatus(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.width = 80
	m.height = 24

	// Disconnected state
	view := m.View()
	if !strings.Contains(view, "○") {
		t.Errorf("expected disconnected indicator '○' in view, got:\n%s", view)
	}

	// Connected state
	m.connected = true
	m.statusBar.Connected = true
	view = m.View()
	if !strings.Contains(view, "●") {
		t.Errorf("expected connected indicator '●' in view, got:\n%s", view)
	}
}

func TestView_ShowsTaskStats(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 40

	// Simulate receiving tasks
	m.stats = TaskStats{Ready: 3, Waiting: 2, InProgress: 1, Completed: 5}
	m.statusBar.Stats = m.stats

	view := m.View()

	if !strings.Contains(view, "3 ready") {
		t.Errorf("expected '3 ready' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "2 waiting") {
		t.Errorf("expected '2 waiting' in view, got:\n%s", view)
	}
}

// =============================================================================
// Panel Cycling Tests
// =============================================================================

func TestNextPanel(t *testing.T) {
	tests := []struct {
		name          string
		current       Panel
		detailVisible bool
		logsVisible   bool
		expected      Panel
	}{
		{
			name:          "tasks only - cycles to tasks",
			current:       PanelTasks,
			detailVisible: false,
			logsVisible:   false,
			expected:      PanelTasks,
		},
		{
			name:          "tasks with detail - cycles to detail",
			current:       PanelTasks,
			detailVisible: true,
			logsVisible:   false,
			expected:      PanelDetails,
		},
		{
			name:          "detail with detail visible - cycles to tasks",
			current:       PanelDetails,
			detailVisible: true,
			logsVisible:   false,
			expected:      PanelTasks,
		},
		{
			name:          "tasks with both - cycles to detail",
			current:       PanelTasks,
			detailVisible: true,
			logsVisible:   true,
			expected:      PanelDetails,
		},
		{
			name:          "detail with both - cycles to logs",
			current:       PanelDetails,
			detailVisible: true,
			logsVisible:   true,
			expected:      PanelLogs,
		},
		{
			name:          "logs with both - cycles to tasks",
			current:       PanelLogs,
			detailVisible: true,
			logsVisible:   true,
			expected:      PanelTasks,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextPanel(tt.current, tt.detailVisible, tt.logsVisible)
			if got != tt.expected {
				t.Errorf("NextPanel(%v, detail=%v, logs=%v) = %v, want %v",
					tt.current, tt.detailVisible, tt.logsVisible, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// StatusBar Tests
// =============================================================================

func TestStatusBarView_ContainsStats(t *testing.T) {
	sb := NewStatusBar("test-project")
	sb.Stats = TaskStats{
		Ready:      3,
		Waiting:    2,
		InProgress: 1,
		Completed:  5,
		Blocked:    0,
	}
	sb.Connected = true

	view := sb.View(80)

	if !strings.Contains(view, "3 ready") {
		t.Errorf("expected '3 ready' in status bar, got:\n%s", view)
	}
	if !strings.Contains(view, "2 waiting") {
		t.Errorf("expected '2 waiting' in status bar, got:\n%s", view)
	}
	if !strings.Contains(view, "1 active") {
		t.Errorf("expected '1 active' in status bar, got:\n%s", view)
	}
	if !strings.Contains(view, "5 done") {
		t.Errorf("expected '5 done' in status bar, got:\n%s", view)
	}
}

func TestStatusBarView_ShowsConnectionDot(t *testing.T) {
	sb := NewStatusBar("test-project")

	// Disconnected
	view := sb.View(80)
	if !strings.Contains(view, "○") {
		t.Errorf("expected disconnected dot '○', got:\n%s", view)
	}

	// Connected
	sb.Connected = true
	view = sb.View(80)
	if !strings.Contains(view, "●") {
		t.Errorf("expected connected dot '●', got:\n%s", view)
	}
}

func TestStatusBarView_ShowsProjectName(t *testing.T) {
	sb := NewStatusBar("my-cool-project")
	view := sb.View(80)

	if !strings.Contains(view, "my-cool-project") {
		t.Errorf("expected project name in status bar, got:\n%s", view)
	}
}

// =============================================================================
// HelpBar Tests
// =============================================================================

func TestHelpBarView_ContainsShortcuts(t *testing.T) {
	hb := NewHelpBar()
	view := hb.View(120, false)

	shortcuts := []string{"j/k", "Tab", "Quit"}
	for _, s := range shortcuts {
		if !strings.Contains(view, s) {
			t.Errorf("expected help bar to contain '%s', got:\n%s", s, view)
		}
	}
}

func TestHelpBarView_MultiProjectShowsTabShortcuts(t *testing.T) {
	hb := NewHelpBar()
	view := hb.View(120, true)

	if !strings.Contains(view, "h/l") {
		t.Errorf("expected multi-project help to contain 'h/l' for tab switching, got:\n%s", view)
	}
}

// =============================================================================
// Update Tests - Task Navigation (j/k/g/G)
// =============================================================================

func TestUpdate_JKey_MovesDownInTaskTree(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.width = 80
	m.height = 24

	// Simulate receiving tasks
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Classification: "ready", Priority: "high"},
		{ID: "t2", Title: "Task 2", Classification: "ready", Priority: "medium"},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 2}})
	m = updated.(Model)

	// Should start on group header (new behavior)
	if m.taskTree.SelectedID != "" {
		t.Fatalf("expected initial selection on header (empty), got '%s'", m.taskTree.SelectedID)
	}

	// Press j to enter group and select first task
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.taskTree.SelectedID != "t1" {
		t.Errorf("after first 'j', expected selection 't1', got '%s'", m.taskTree.SelectedID)
	}

	// Press j again to move to second task
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.taskTree.SelectedID != "t2" {
		t.Errorf("after second 'j', expected selection 't2', got '%s'", m.taskTree.SelectedID)
	}
}

func TestUpdate_KKey_MovesUpInTaskTree(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Classification: "ready", Priority: "high"},
		{ID: "t2", Title: "Task 2", Classification: "ready", Priority: "medium"},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 2}})
	m = updated.(Model)

	// Should start on group header
	if m.taskTree.SelectedID != "" {
		t.Fatalf("expected initial selection on header (empty), got '%s'", m.taskTree.SelectedID)
	}

	// Press j to enter group (first task)
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.taskTree.SelectedID != "t1" {
		t.Fatalf("after 'j', expected 't1', got '%s'", m.taskTree.SelectedID)
	}

	// Move down to second task
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.taskTree.SelectedID != "t2" {
		t.Fatalf("after second 'j', expected 't2', got '%s'", m.taskTree.SelectedID)
	}

	// Press k to move up to first task
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.taskTree.SelectedID != "t1" {
		t.Errorf("after 'k', expected selection 't1', got '%s'", m.taskTree.SelectedID)
	}

	// Press k again to return to group header
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.taskTree.SelectedID != "" {
		t.Errorf("after second 'k', expected header (empty), got '%s'", m.taskTree.SelectedID)
	}
}

func TestUpdate_GKey_MovesToTop(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Classification: "ready", Priority: "high"},
		{ID: "t2", Title: "Task 2", Classification: "ready", Priority: "medium"},
		{ID: "t3", Title: "Task 3", Classification: "ready", Priority: "low"},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 3}})
	m = updated.(Model)

	// Should start on group header
	if m.taskTree.SelectedID != "" {
		t.Fatalf("expected initial selection on header (empty), got '%s'", m.taskTree.SelectedID)
	}

	// Move to bottom first (will land on last task if group is expanded)
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.taskTree.SelectedID != "t3" {
		t.Fatalf("after 'G', expected 't3', got '%s'", m.taskTree.SelectedID)
	}

	// Press g to go to top (should return to group header)
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.taskTree.SelectedID != "" {
		t.Errorf("after 'g', expected header (empty), got '%s'", m.taskTree.SelectedID)
	}
}

func TestUpdate_TasksUpdated_UpdatesTaskTree(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.width = 80
	m.height = 24

	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Classification: "ready", Priority: "high"},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 1}})
	m = updated.(Model)

	// Task tree should start on group header (new behavior)
	if m.taskTree.SelectedID != "" {
		t.Errorf("expected task tree to start on header (empty), got '%s'", m.taskTree.SelectedID)
	}

	// View should contain the task title
	view := m.View()
	if !strings.Contains(view, "Task 1") {
		t.Errorf("expected view to contain 'Task 1', got:\n%s", view)
	}
}

// =============================================================================
// Config Tests
// =============================================================================

func TestConfig_IsMultiProject(t *testing.T) {
	tests := []struct {
		name     string
		projects []string
		expected bool
	}{
		{"single project", []string{"proj1"}, false},
		{"multi project", []string{"proj1", "proj2"}, true},
		{"empty", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Projects: tt.projects}
			if got := cfg.IsMultiProject(); got != tt.expected {
				t.Errorf("IsMultiProject() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// =============================================================================
// Panel Toggle Tests - 'L' toggles logs, 'T' toggles detail
// =============================================================================

func TestUpdate_LKey_TogglesLogVisibility(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	if m.logsVisible {
		t.Fatal("expected logsVisible to be false initially")
	}

	// Press 'L' to toggle logs on
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	if !model.logsVisible {
		t.Error("expected logsVisible to be true after 'L' press")
	}

	// Press 'L' again to toggle logs off
	updated, _ = model.Update(msg)
	model = updated.(Model)

	if model.logsVisible {
		t.Error("expected logsVisible to be false after second 'L' press")
	}
}

func TestUpdate_TKey_TogglesDetailVisibility(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	if m.detailVisible {
		t.Fatal("expected detailVisible to be false initially")
	}

	// Press 'T' to toggle detail on
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	if !model.detailVisible {
		t.Error("expected detailVisible to be true after 'T' press")
	}

	// Press 'T' again to toggle detail off
	updated, _ = model.Update(msg)
	model = updated.(Model)

	if model.detailVisible {
		t.Error("expected detailVisible to be false after second 'T' press")
	}
}

// =============================================================================
// Panel Focus Cycling with Visible Panels
// =============================================================================

func TestUpdate_TabCyclesWithDetailVisible(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.detailVisible = true

	if m.activePanel != PanelTasks {
		t.Fatalf("expected initial panel PanelTasks, got %v", m.activePanel)
	}

	// Tab: tasks -> detail
	msg := tea.KeyMsg{Type: tea.KeyTab}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	if model.activePanel != PanelDetails {
		t.Errorf("expected PanelDetails after Tab, got %v", model.activePanel)
	}

	// Tab: detail -> tasks
	updated, _ = model.Update(msg)
	model = updated.(Model)

	if model.activePanel != PanelTasks {
		t.Errorf("expected PanelTasks after second Tab, got %v", model.activePanel)
	}
}

func TestUpdate_TabCyclesWithBothPanelsVisible(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.detailVisible = true
	m.logsVisible = true

	// Tab: tasks -> detail -> logs -> tasks
	msg := tea.KeyMsg{Type: tea.KeyTab}

	updated, _ := m.Update(msg)
	model := updated.(Model)
	if model.activePanel != PanelDetails {
		t.Errorf("expected PanelDetails, got %v", model.activePanel)
	}

	updated, _ = model.Update(msg)
	model = updated.(Model)
	if model.activePanel != PanelLogs {
		t.Errorf("expected PanelLogs, got %v", model.activePanel)
	}

	updated, _ = model.Update(msg)
	model = updated.(Model)
	if model.activePanel != PanelTasks {
		t.Errorf("expected PanelTasks, got %v", model.activePanel)
	}
}

// =============================================================================
// Task Selection Updates Detail Panel
// =============================================================================

func TestUpdate_TaskSelectionUpdatesDetail(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 40
	m.detailVisible = true

	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "First Task", Classification: "ready", Priority: "high"},
		{ID: "t2", Title: "Second Task", Classification: "waiting", Priority: "medium"},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 1, Waiting: 1}})
	m = updated.(Model)

	// Should start on group header (no task selected)
	if m.taskDetail.task != nil {
		t.Fatal("expected taskDetail to have NO task when on group header")
	}

	// Move down to enter group and select first task
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	// Now first task should be selected
	if m.taskDetail.task == nil {
		t.Fatal("expected taskDetail to have a task after entering group")
	}
	if m.taskDetail.task.ID != "t1" {
		t.Errorf("expected taskDetail task ID 't1', got '%s'", m.taskDetail.task.ID)
	}

	// Move down to second task (different group - need to go through header)
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	// Should be on Waiting group header now (t2 is in a different group)
	if m.taskDetail.task != nil {
		t.Errorf("expected no task when on Waiting group header, got '%v'", m.taskDetail.task)
	}

	// Move down once more to enter Waiting group
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.taskDetail.task == nil {
		t.Fatal("expected taskDetail to have a task after navigation")
	}
	if m.taskDetail.task.ID != "t2" {
		t.Errorf("expected taskDetail task ID 't2', got '%s'", m.taskDetail.task.ID)
	}
}

// =============================================================================
// Window Resize Propagates to All Panels
// =============================================================================

func TestUpdate_WindowResize_PropagatesToPanels(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.detailVisible = true
	m.logsVisible = true

	msg := tea.WindowSizeMsg{Width: 160, Height: 50}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	if model.width != 160 {
		t.Errorf("expected width 160, got %d", model.width)
	}
	if model.height != 50 {
		t.Errorf("expected height 50, got %d", model.height)
	}

	// TaskDetail and LogViewer should have non-zero dimensions
	if model.taskDetail.width == 0 {
		t.Error("expected taskDetail width to be set after resize")
	}
	if model.taskDetail.height == 0 {
		t.Error("expected taskDetail height to be set after resize")
	}
	if model.logViewer.width == 0 {
		t.Error("expected logViewer width to be set after resize")
	}
	if model.logViewer.height == 0 {
		t.Error("expected logViewer height to be set after resize")
	}
}

// =============================================================================
// View Contains All Visible Panels
// =============================================================================

func TestView_WithDetailVisible_ContainsDetailPanel(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 40
	m.detailVisible = true

	view := m.View()

	if !strings.Contains(view, "Task Detail") {
		t.Errorf("expected 'Task Detail' in view when detail visible, got:\n%s", view)
	}
}

func TestView_WithLogsVisible_ContainsLogPanel(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 40
	m.logsVisible = true

	view := m.View()

	if !strings.Contains(view, "Logs") {
		t.Errorf("expected 'Logs' in view when logs visible, got:\n%s", view)
	}
}

func TestView_WithBothPanelsVisible_ContainsBoth(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 40
	m.detailVisible = true
	m.logsVisible = true

	view := m.View()

	if !strings.Contains(view, "Task Detail") {
		t.Errorf("expected 'Task Detail' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Logs") {
		t.Errorf("expected 'Logs' in view, got:\n%s", view)
	}
}

func TestView_NoPanelsVisible_NoDetailOrLogs(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 40
	// Both panels hidden by default

	view := m.View()

	if strings.Contains(view, "Task Detail") {
		t.Errorf("expected no 'Task Detail' when detail not visible, got:\n%s", view)
	}
}

// =============================================================================
// Navigation Keys Forward to Active Panel
// =============================================================================

func TestUpdate_JKKeysOnlyWorkInTasksPanel(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.detailVisible = true

	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Classification: "ready", Priority: "high"},
		{ID: "t2", Title: "Task 2", Classification: "ready", Priority: "medium"},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 2}})
	m = updated.(Model)

	// Should start on group header
	if m.taskTree.SelectedID != "" {
		t.Fatalf("expected initial selection on header (empty), got '%s'", m.taskTree.SelectedID)
	}

	// Switch to detail panel
	m.activePanel = PanelDetails

	// Press j - should NOT move task selection since we're in detail panel
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.taskTree.SelectedID != "" {
		t.Errorf("expected selection to stay at header (empty) when not in tasks panel, got '%s'", m.taskTree.SelectedID)
	}
}

// =============================================================================
// Settings Modal Integration Tests
// =============================================================================

func TestUpdate_SKey_OpensSettingsModal(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	if m.modalManager.IsOpen() {
		t.Fatal("expected no modal to be open initially")
	}

	// Press 'S' to open settings
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	if !model.modalManager.IsOpen() {
		t.Error("expected settings modal to be open after 'S' key")
	}
}

func TestUpdate_EscKey_ClosesSettingsModal(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	// Open settings modal
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	if !m.modalManager.IsOpen() {
		t.Fatal("expected modal to be open after 'S'")
	}

	// Press Esc to close
	msg = tea.KeyMsg{Type: tea.KeyEsc}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.modalManager.IsOpen() {
		t.Error("expected modal to be closed after Esc")
	}
}

func TestUpdate_ModalRoutesKeysCorrectly(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	// Open settings modal
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	if !m.modalManager.IsOpen() {
		t.Fatal("expected modal to be open")
	}

	// Send 'j' key - should be handled by modal, not task tree
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	// Modal should still be open (j is navigation in modal)
	if !m.modalManager.IsOpen() {
		t.Error("expected modal to still be open after 'j'")
	}
}

func TestView_WithSettingsModal_ShowsOverlay(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 40

	// Open settings modal
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	view := m.View()

	// View should contain modal title
	if !strings.Contains(view, "Settings") {
		t.Errorf("expected view to contain 'Settings' modal title, got:\n%s", view)
	}

	// View should contain global max parallel
	if !strings.Contains(view, "Global Max Parallel") {
		t.Errorf("expected view to contain 'Global Max Parallel', got:\n%s", view)
	}
}

func TestNewModel_InitializesModalManager(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	// Modal manager should be initialized
	if m.modalManager.IsOpen() {
		t.Error("expected modal manager to have no modal open initially")
	}
}

func TestNewModel_LoadsSettings(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	// Settings should be loaded with defaults
	if m.settings.GlobalMaxParallel == 0 {
		t.Error("expected settings.GlobalMaxParallel to be initialized")
	}
	if m.settings.ProjectLimits == nil {
		t.Error("expected settings.ProjectLimits to be initialized")
	}
}

// =============================================================================
// Pause/Resume State Tests
// =============================================================================

func TestNewModel_InitializesPauseState(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	// pausedProjects should be initialized as empty map
	if m.pausedProjects == nil {
		t.Error("expected pausedProjects to be initialized (non-nil)")
	}
	if len(m.pausedProjects) != 0 {
		t.Errorf("expected pausedProjects to be empty, got %d entries", len(m.pausedProjects))
	}

	// allPaused should be false initially
	if m.allPaused {
		t.Error("expected allPaused to be false initially")
	}
}

func TestPauseToggledMsg_Fields(t *testing.T) {
	// Verify the message type can be constructed and used
	msg := pauseToggledMsg{
		projectID: "brain-api",
		paused:    true,
		err:       nil,
	}
	if msg.projectID != "brain-api" {
		t.Errorf("projectID = %q, want %q", msg.projectID, "brain-api")
	}
	if !msg.paused {
		t.Error("expected paused to be true")
	}
	if msg.err != nil {
		t.Errorf("expected nil error, got %v", msg.err)
	}
}

func TestPauseAllToggledMsg_Fields(t *testing.T) {
	msg := pauseAllToggledMsg{
		paused: true,
		err:    nil,
	}
	if !msg.paused {
		t.Error("expected paused to be true")
	}
	if msg.err != nil {
		t.Errorf("expected nil error, got %v", msg.err)
	}
}

func TestRunnerStatusMsg_Fields(t *testing.T) {
	msg := runnerStatusMsg{
		paused:         true,
		pausedProjects: []string{"brain-api", "my-project"},
		err:            nil,
	}
	if !msg.paused {
		t.Error("expected paused to be true")
	}
	if len(msg.pausedProjects) != 2 {
		t.Fatalf("expected 2 paused projects, got %d", len(msg.pausedProjects))
	}
	if msg.pausedProjects[0] != "brain-api" {
		t.Errorf("pausedProjects[0] = %q, want %q", msg.pausedProjects[0], "brain-api")
	}
	if msg.err != nil {
		t.Errorf("expected nil error, got %v", msg.err)
	}
}

// =============================================================================
// Phase 2: Key Handler Tests for Pause/Resume
// =============================================================================

func TestUpdate_PKey_PausesActiveProject(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	// Press 'p' to pause active project
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}}
	updated, cmd := m.Update(msg)
	model := updated.(Model)

	// Should set optimistic UI update
	if !model.pausedProjects["test-project"] {
		t.Error("expected pausedProjects['test-project'] to be true (optimistic update)")
	}

	// Should return a command (the API call)
	if cmd == nil {
		t.Error("expected non-nil command for pause API call")
	}

	// Should show info status message
	if model.statusMessage == "" {
		t.Error("expected a status message to be set")
	}
	if model.statusMessageType != "info" {
		t.Errorf("expected status message type 'info', got %q", model.statusMessageType)
	}
}

func TestUpdate_PKey_ResumesAlreadyPausedProject(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	// Pre-set project as paused
	m.pausedProjects["test-project"] = true

	// Press 'p' to resume
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}}
	updated, cmd := m.Update(msg)
	model := updated.(Model)

	// Should toggle to not paused (optimistic)
	if model.pausedProjects["test-project"] {
		t.Error("expected pausedProjects['test-project'] to be false after resume toggle")
	}

	// Should return a command
	if cmd == nil {
		t.Error("expected non-nil command for resume API call")
	}

	// Status message should mention resuming
	if !strings.Contains(model.statusMessage, "Resuming") {
		t.Errorf("expected status message to contain 'Resuming', got %q", model.statusMessage)
	}
}

func TestUpdate_PKey_NoProject_DoesNothing(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "", // No project set
	}
	m := NewModel(cfg)

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}}
	updated, cmd := m.Update(msg)
	model := updated.(Model)

	// Should not set any pause state
	if len(model.pausedProjects) != 0 {
		t.Errorf("expected no paused projects, got %d", len(model.pausedProjects))
	}

	// Should return nil command (nothing to do)
	if cmd != nil {
		t.Error("expected nil command when no project is set")
	}
}

func TestUpdate_PKey_UsesActiveProjectID(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Project:  "default-project",
		Projects: []string{"proj-a", "proj-b"},
	}
	m := NewModel(cfg)
	m.activeProjectID = "proj-a"

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}}
	updated, cmd := m.Update(msg)
	model := updated.(Model)

	// Should use activeProjectID, not config.Project
	if !model.pausedProjects["proj-a"] {
		t.Error("expected pausedProjects['proj-a'] to be true")
	}
	if model.pausedProjects["default-project"] {
		t.Error("expected pausedProjects['default-project'] to be false")
	}
	if cmd == nil {
		t.Error("expected non-nil command")
	}
}

func TestUpdate_ShiftPKey_PausesAll(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	// Press 'P' (shift+p) to pause all
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}}
	updated, cmd := m.Update(msg)
	model := updated.(Model)

	// Should set allPaused
	if !model.allPaused {
		t.Error("expected allPaused to be true after 'P'")
	}

	// Should return a command
	if cmd == nil {
		t.Error("expected non-nil command for pause all API call")
	}

	// Should show info status message
	if model.statusMessageType != "info" {
		t.Errorf("expected status message type 'info', got %q", model.statusMessageType)
	}
	if !strings.Contains(model.statusMessage, "Pausing all") {
		t.Errorf("expected status message to contain 'Pausing all', got %q", model.statusMessage)
	}
}

func TestUpdate_ShiftPKey_ResumesAll(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.allPaused = true

	// Press 'P' to resume all
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}}
	updated, cmd := m.Update(msg)
	model := updated.(Model)

	// Should toggle allPaused to false
	if model.allPaused {
		t.Error("expected allPaused to be false after resume toggle")
	}

	if cmd == nil {
		t.Error("expected non-nil command for resume all API call")
	}

	if !strings.Contains(model.statusMessage, "Resuming all") {
		t.Errorf("expected status message to contain 'Resuming all', got %q", model.statusMessage)
	}
}

// =============================================================================
// Phase 2: Message Handler Tests for Pause/Resume
// =============================================================================

func TestUpdate_PauseToggledMsg_Success(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.pausedProjects["test-project"] = true // optimistic update already applied

	// Simulate successful pause response
	updated, cmd := m.Update(pauseToggledMsg{projectID: "test-project", paused: true, err: nil})
	model := updated.(Model)

	// Pause state should remain (confirmed by server)
	if !model.pausedProjects["test-project"] {
		t.Error("expected pausedProjects['test-project'] to remain true on success")
	}

	// Should show success message
	if model.statusMessageType != "success" {
		t.Errorf("expected status message type 'success', got %q", model.statusMessageType)
	}
	if !strings.Contains(model.statusMessage, "paused") {
		t.Errorf("expected status message to contain 'paused', got %q", model.statusMessage)
	}

	// Should return nil (no follow-up command)
	if cmd != nil {
		t.Error("expected nil command after successful pause toggle")
	}
}

func TestUpdate_PauseToggledMsg_Error_RevertsOptimistic(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.pausedProjects["test-project"] = true // optimistic update

	// Simulate error response
	updated, _ := m.Update(pauseToggledMsg{
		projectID: "test-project",
		paused:    true,
		err:       fmt.Errorf("network error"),
	})
	model := updated.(Model)

	// Should revert optimistic update (paused=true means we tried to pause, revert = false)
	if model.pausedProjects["test-project"] {
		t.Error("expected pausedProjects['test-project'] to be reverted to false on error")
	}

	// Should show error message
	if model.statusMessageType != "error" {
		t.Errorf("expected status message type 'error', got %q", model.statusMessageType)
	}
}

func TestUpdate_PauseAllToggledMsg_Success(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.allPaused = true // optimistic update

	updated, cmd := m.Update(pauseAllToggledMsg{paused: true, err: nil})
	model := updated.(Model)

	if !model.allPaused {
		t.Error("expected allPaused to remain true on success")
	}
	if model.statusMessageType != "success" {
		t.Errorf("expected 'success', got %q", model.statusMessageType)
	}
	if !strings.Contains(model.statusMessage, "paused") {
		t.Errorf("expected message to contain 'paused', got %q", model.statusMessage)
	}
	if cmd != nil {
		t.Error("expected nil command")
	}
}

func TestUpdate_PauseAllToggledMsg_Error_RevertsOptimistic(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.allPaused = true // optimistic update

	updated, _ := m.Update(pauseAllToggledMsg{paused: true, err: fmt.Errorf("server error")})
	model := updated.(Model)

	// Should revert
	if model.allPaused {
		t.Error("expected allPaused to be reverted to false on error")
	}
	if model.statusMessageType != "error" {
		t.Errorf("expected 'error', got %q", model.statusMessageType)
	}
}

func TestUpdate_RunnerStatusMsg_Success_UpdatesState(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	updated, cmd := m.Update(runnerStatusMsg{
		paused:         true,
		pausedProjects: []string{"proj-a", "proj-b"},
	})
	model := updated.(Model)

	if !model.allPaused {
		t.Error("expected allPaused to be true")
	}
	if !model.pausedProjects["proj-a"] {
		t.Error("expected pausedProjects['proj-a'] to be true")
	}
	if !model.pausedProjects["proj-b"] {
		t.Error("expected pausedProjects['proj-b'] to be true")
	}
	if len(model.pausedProjects) != 2 {
		t.Errorf("expected 2 paused projects, got %d", len(model.pausedProjects))
	}
	if cmd != nil {
		t.Error("expected nil command")
	}
}

func TestUpdate_RunnerStatusMsg_Error_NoStateChange(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.allPaused = true
	m.pausedProjects["existing"] = true

	updated, _ := m.Update(runnerStatusMsg{err: fmt.Errorf("connection refused")})
	model := updated.(Model)

	// State should not change on error
	if !model.allPaused {
		t.Error("expected allPaused to remain true on error")
	}
	if !model.pausedProjects["existing"] {
		t.Error("expected existing paused project to remain")
	}
}

// =============================================================================
// Phase 2: Tea.Cmd Function Tests
// =============================================================================

func TestPauseProjectCmd_ReturnsPauseToggledMsg(t *testing.T) {
	// Test that the command function returns the right message type
	cmd := pauseProjectCmd("http://localhost:9999", "test-project", false)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	// Execute the command - it will fail to connect but should return the right msg type
	result := cmd()
	msg, ok := result.(pauseToggledMsg)
	if !ok {
		t.Fatalf("expected pauseToggledMsg, got %T", result)
	}
	if msg.projectID != "test-project" {
		t.Errorf("expected projectID 'test-project', got %q", msg.projectID)
	}
	// paused should be true (we're pausing, not resuming)
	if !msg.paused {
		t.Error("expected paused=true when currentlyPaused=false")
	}
	// err should be non-nil since we can't connect
	if msg.err == nil {
		t.Error("expected error since API is not running")
	}
}

func TestPauseProjectCmd_Resume_ReturnsPauseToggledMsg(t *testing.T) {
	cmd := pauseProjectCmd("http://localhost:9999", "test-project", true)
	result := cmd()
	msg, ok := result.(pauseToggledMsg)
	if !ok {
		t.Fatalf("expected pauseToggledMsg, got %T", result)
	}
	// paused should be false (we're resuming)
	if msg.paused {
		t.Error("expected paused=false when currentlyPaused=true (resuming)")
	}
}

func TestPauseAllCmd_ReturnsPauseAllToggledMsg(t *testing.T) {
	cmd := pauseAllCmd("http://localhost:9999", false)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	result := cmd()
	msg, ok := result.(pauseAllToggledMsg)
	if !ok {
		t.Fatalf("expected pauseAllToggledMsg, got %T", result)
	}
	if !msg.paused {
		t.Error("expected paused=true when currentlyPaused=false")
	}
	if msg.err == nil {
		t.Error("expected error since API is not running")
	}
}

func TestFetchRunnerStatusCmd_ReturnsRunnerStatusMsg(t *testing.T) {
	cmd := fetchRunnerStatusCmd("http://localhost:9999")
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	result := cmd()
	msg, ok := result.(runnerStatusMsg)
	if !ok {
		t.Fatalf("expected runnerStatusMsg, got %T", result)
	}
	// Should have error since API is not running
	if msg.err == nil {
		t.Error("expected error since API is not running")
	}
}

// =============================================================================
// Phase 2: Tick Syncs Runner Status
// =============================================================================

func TestUpdate_TickMsg_ReturnsCmd(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	updated, cmd := m.Update(TickMsg{})
	_ = updated.(Model)

	// Should return a command (tick + fetchRunnerStatus batch)
	if cmd == nil {
		t.Error("expected non-nil command from TickMsg handler")
	}
}

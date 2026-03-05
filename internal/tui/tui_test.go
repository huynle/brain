package tui

import (
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

	// First task should be selected
	if m.taskTree.SelectedID != "t1" {
		t.Fatalf("expected initial selection 't1', got '%s'", m.taskTree.SelectedID)
	}

	// Press j to move down
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.taskTree.SelectedID != "t2" {
		t.Errorf("after 'j', expected selection 't2', got '%s'", m.taskTree.SelectedID)
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

	// Move down first
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	// Press k to move up
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.taskTree.SelectedID != "t1" {
		t.Errorf("after 'k', expected selection 't1', got '%s'", m.taskTree.SelectedID)
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

	// Move to bottom first
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.taskTree.SelectedID != "t3" {
		t.Fatalf("after 'G', expected 't3', got '%s'", m.taskTree.SelectedID)
	}

	// Press g to go to top
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.taskTree.SelectedID != "t1" {
		t.Errorf("after 'g', expected 't1', got '%s'", m.taskTree.SelectedID)
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

	// Task tree should have the task
	if m.taskTree.SelectedID != "t1" {
		t.Errorf("expected task tree to select 't1', got '%s'", m.taskTree.SelectedID)
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

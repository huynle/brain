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

func TestView_StatusBarAppearsBeforeWindowSizeMsg(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "my-project",
	}
	m := NewModel(cfg)
	// Deliberately don't set width/height to simulate first render before WindowSizeMsg
	// m.width = 0, m.height = 0 (default values)

	view := m.View()

	// StatusBar should appear even before WindowSizeMsg sets dimensions
	// This verifies fix for: Missing header bar in Go rewrite TUI
	if !strings.Contains(view, "my-project") {
		t.Errorf("expected StatusBar with project name 'my-project' to appear in initial render, got:\n%s", view)
	}
	// Should NOT show "Initializing..." message anymore
	if strings.Contains(view, "Initializing...") {
		t.Errorf("unexpected 'Initializing...' message blocking StatusBar render, got:\n%s", view)
	}
	// Should contain task stats - at least the "ready" indicator
	// (full stats might be truncated with width=0, but at least some stats should show)
	if !strings.Contains(view, "ready") {
		t.Errorf("expected StatusBar to contain task stats, got:\n%s", view)
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
	view := hb.View(120, false, "test-project")

	shortcuts := []string{"j/k", "Tab", "Quit"}
	for _, s := range shortcuts {
		if !strings.Contains(view, s) {
			t.Errorf("expected help bar to contain '%s', got:\n%s", s, view)
		}
	}
}

func TestHelpBarView_MultiProjectShowsTabShortcuts(t *testing.T) {
	hb := NewHelpBar()
	view := hb.View(120, true, "test-project")

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
	cmd := pauseProjectCmd("http://localhost:9999", "", "test-project", false)
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
	cmd := pauseProjectCmd("http://localhost:9999", "", "test-project", true)
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
	cmd := pauseAllCmd("http://localhost:9999", "", false)
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
	cmd := fetchRunnerStatusCmd("http://localhost:9999", "")
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

// =============================================================================
// Phase 3: HelpBar Pause State Sync Tests
// =============================================================================

func TestSyncHelpBarPauseState_ProjectPaused(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.pausedProjects["test-project"] = true

	m.syncHelpBarPauseState()

	if !m.helpBar.IsPaused {
		t.Error("expected helpBar.IsPaused to be true when active project is paused")
	}
	if m.helpBar.AllPaused {
		t.Error("expected helpBar.AllPaused to be false")
	}
}

func TestSyncHelpBarPauseState_AllPaused(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.allPaused = true

	m.syncHelpBarPauseState()

	if !m.helpBar.AllPaused {
		t.Error("expected helpBar.AllPaused to be true")
	}
}

func TestSyncHelpBarPauseState_NotPaused(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	m.syncHelpBarPauseState()

	if m.helpBar.IsPaused {
		t.Error("expected helpBar.IsPaused to be false when no projects paused")
	}
	if m.helpBar.AllPaused {
		t.Error("expected helpBar.AllPaused to be false")
	}
}

func TestSyncHelpBarPauseState_MultiProject_ActiveProjectPaused(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Project:  "default-project",
		Projects: []string{"proj-a", "proj-b"},
	}
	m := NewModel(cfg)
	m.activeProjectID = "proj-a"
	m.pausedProjects["proj-a"] = true

	m.syncHelpBarPauseState()

	if !m.helpBar.IsPaused {
		t.Error("expected helpBar.IsPaused to be true for active project proj-a")
	}
}

func TestSyncHelpBarPauseState_MultiProject_AllTab_FallsBackToConfigProject(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Project:  "default-project",
		Projects: []string{"proj-a", "proj-b"},
	}
	m := NewModel(cfg)
	m.activeProjectID = "all"
	m.pausedProjects["default-project"] = true

	m.syncHelpBarPauseState()

	if !m.helpBar.IsPaused {
		t.Error("expected helpBar.IsPaused to be true when 'all' tab and config project is paused")
	}
}

func TestUpdate_PKey_SyncsHelpBarPauseState(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	// Press 'p' to pause
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	// HelpBar should reflect the optimistic pause state
	if !model.helpBar.IsPaused {
		t.Error("expected helpBar.IsPaused to be true after pressing 'p'")
	}
}

func TestUpdate_ShiftPKey_SyncsHelpBarAllPaused(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	// Press 'P' to pause all
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	if !model.helpBar.AllPaused {
		t.Error("expected helpBar.AllPaused to be true after pressing 'P'")
	}
}

func TestUpdate_PauseToggledMsg_SyncsHelpBar(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.pausedProjects["test-project"] = true // optimistic

	// Successful pause confirmation
	updated, _ := m.Update(pauseToggledMsg{projectID: "test-project", paused: true, err: nil})
	model := updated.(Model)

	if !model.helpBar.IsPaused {
		t.Error("expected helpBar.IsPaused to be true after successful pause toggle")
	}
}

func TestUpdate_PauseToggledMsg_Error_SyncsHelpBarAfterRevert(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.pausedProjects["test-project"] = true // optimistic

	// Error reverts the optimistic update
	updated, _ := m.Update(pauseToggledMsg{
		projectID: "test-project",
		paused:    true,
		err:       fmt.Errorf("server error"),
	})
	model := updated.(Model)

	// After revert, IsPaused should be false
	if model.helpBar.IsPaused {
		t.Error("expected helpBar.IsPaused to be false after error revert")
	}
}

func TestUpdate_RunnerStatusMsg_SyncsHelpBar(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	updated, _ := m.Update(runnerStatusMsg{
		paused:         true,
		pausedProjects: []string{"test-project"},
	})
	model := updated.(Model)

	if !model.helpBar.AllPaused {
		t.Error("expected helpBar.AllPaused to be true after runnerStatusMsg")
	}
	if !model.helpBar.IsPaused {
		t.Error("expected helpBar.IsPaused to be true after runnerStatusMsg with test-project paused")
	}
}

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

// =============================================================================
// Session Integration Tests (Phase 2)
// =============================================================================

func TestExtractTaskID(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"standard path", "projects/test/task/abc12def.md", "abc12def"},
		{"nested path", "projects/my-project/task/xyz98765.md", "xyz98765"},
		{"no extension", "projects/test/task/abc12def", "abc12def"},
		{"just filename", "abc12def.md", "abc12def"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTaskID(tt.path)
			if got != tt.expected {
				t.Errorf("extractTaskID(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

func TestSessionSelectedMsg_Fields(t *testing.T) {
	msg := sessionSelectedMsg{
		sessionID: "ses_abc",
		tmuxMode:  true,
		taskID:    "task123",
	}
	if msg.sessionID != "ses_abc" {
		t.Errorf("sessionID = %q, want %q", msg.sessionID, "ses_abc")
	}
	if !msg.tmuxMode {
		t.Error("expected tmuxMode to be true")
	}
	if msg.taskID != "task123" {
		t.Errorf("taskID = %q, want %q", msg.taskID, "task123")
	}
}

func TestUpdate_OKey_RequiresTaskPanel(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.activePanel = PanelDetails // Not on tasks panel

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}}
	_, cmd := m.Update(msg)

	if cmd != nil {
		t.Error("expected nil command when not on tasks panel")
	}
}

func TestUpdate_OKey_RequiresSelectedTask(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	// No tasks loaded, so no selected task

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}}
	_, cmd := m.Update(msg)

	if cmd != nil {
		t.Error("expected nil command when no task is selected")
	}
}

func TestUpdate_OKey_FetchesSessions(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	// Load tasks and select one
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Path: "projects/test/task/t1.md", Classification: "ready", Priority: "high"},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 1}})
	m = updated.(Model)

	// Move to first task (past group header)
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(jMsg)
	m = updated.(Model)

	if m.taskTree.SelectedTask() == nil {
		t.Fatal("expected a task to be selected after 'j'")
	}

	// Press 'o' to fetch sessions (fullscreen mode)
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}}
	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Error("expected non-nil command (fetchSessionsCmd) after 'o' with selected task")
	}
}

func TestUpdate_ShiftOKey_FetchesSessionsTmux(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	// Load tasks and select one
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Path: "projects/test/task/t1.md", Classification: "ready", Priority: "high"},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 1}})
	m = updated.(Model)

	// Move to first task
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(jMsg)
	m = updated.(Model)

	// Press 'O' to fetch sessions (tmux mode)
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'O'}}
	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Error("expected non-nil command (fetchSessionsCmd) after 'O' with selected task")
	}
}

func TestUpdate_SessionsFetchedMsg_Error(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	updated, cmd := m.Update(sessionsFetchedMsg{
		err:      fmt.Errorf("connection refused"),
		taskPath: "projects/test/task/t1.md",
	})
	model := updated.(Model)

	if model.statusMessageType != "error" {
		t.Errorf("expected status message type 'error', got %q", model.statusMessageType)
	}
	if !strings.Contains(model.statusMessage, "Failed to fetch sessions") {
		t.Errorf("expected error message about fetching sessions, got %q", model.statusMessage)
	}
	if cmd != nil {
		t.Error("expected nil command on error")
	}
}

func TestUpdate_SessionsFetchedMsg_SingleSession_Fullscreen(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	updated, cmd := m.Update(sessionsFetchedMsg{
		sessionIDs: []string{"ses_abc"},
		taskPath:   "projects/test/task/t1.md",
		tmuxMode:   false,
	})
	_ = updated.(Model)

	// Single session should directly open (return a command)
	if cmd == nil {
		t.Error("expected non-nil command for single session fullscreen open")
	}
}

func TestUpdate_SessionsFetchedMsg_SingleSession_Tmux(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	updated, cmd := m.Update(sessionsFetchedMsg{
		sessionIDs: []string{"ses_abc"},
		taskPath:   "projects/test/task/t1.md",
		tmuxMode:   true,
	})
	_ = updated.(Model)

	// Single session in tmux mode should directly open
	if cmd == nil {
		t.Error("expected non-nil command for single session tmux open")
	}
}

func TestUpdate_SessionsFetchedMsg_MultipleSessions_OpensModal(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	updated, _ := m.Update(sessionsFetchedMsg{
		sessionIDs: []string{"ses_a", "ses_b", "ses_c"},
		taskPath:   "projects/test/task/t1.md",
		tmuxMode:   false,
	})
	model := updated.(Model)

	// Should open a modal
	if !model.modalManager.IsOpen() {
		t.Error("expected modal to be open for multiple sessions")
	}
}

func TestUpdate_SessionSelectedMsg_Fullscreen(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	// Pre-open a modal to verify it gets closed
	m.modalManager.Open(NewHelpModal(false))

	updated, cmd := m.Update(sessionSelectedMsg{
		sessionID: "ses_abc",
		tmuxMode:  false,
		taskID:    "t1",
	})
	model := updated.(Model)

	// Modal should be closed
	if model.modalManager.IsOpen() {
		t.Error("expected modal to be closed after session selection")
	}

	// Should return a command to open the session
	if cmd == nil {
		t.Error("expected non-nil command to open session fullscreen")
	}
}

func TestUpdate_SessionSelectedMsg_Tmux(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.modalManager.Open(NewHelpModal(false))

	updated, cmd := m.Update(sessionSelectedMsg{
		sessionID: "ses_abc",
		tmuxMode:  true,
		taskID:    "t1",
	})
	model := updated.(Model)

	if model.modalManager.IsOpen() {
		t.Error("expected modal to be closed after session selection")
	}

	if cmd == nil {
		t.Error("expected non-nil command to open session in tmux")
	}
}

func TestUpdate_SessionOpenedMsg_Success(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	updated, cmd := m.Update(sessionOpenedMsg{
		taskID:    "t1",
		sessionID: "ses_abc",
		err:       nil,
	})
	model := updated.(Model)

	if model.statusMessageType != "success" {
		t.Errorf("expected status message type 'success', got %q", model.statusMessageType)
	}
	if !strings.Contains(model.statusMessage, "Session closed") {
		t.Errorf("expected success message about session, got %q", model.statusMessage)
	}
	if cmd != nil {
		t.Error("expected nil command after session closed")
	}
}

func TestUpdate_SessionOpenedMsg_Error(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	updated, cmd := m.Update(sessionOpenedMsg{
		taskID:    "t1",
		sessionID: "ses_abc",
		err:       fmt.Errorf("opencode not found"),
	})
	model := updated.(Model)

	if model.statusMessageType != "error" {
		t.Errorf("expected status message type 'error', got %q", model.statusMessageType)
	}
	if !strings.Contains(model.statusMessage, "Session error") {
		t.Errorf("expected error message about session, got %q", model.statusMessage)
	}
	if cmd != nil {
		t.Error("expected nil command after session error")
	}
}

// =============================================================================
// ViewMode Type Tests
// =============================================================================

func TestViewMode_Constants(t *testing.T) {
	// ViewModeTasks should be the zero value
	var defaultMode ViewMode
	if defaultMode != ViewModeTasks {
		t.Errorf("expected zero value to be ViewModeTasks, got %v", defaultMode)
	}

	// ViewModeSchedules should be different from ViewModeTasks
	if ViewModeSchedules == ViewModeTasks {
		t.Error("expected ViewModeSchedules to differ from ViewModeTasks")
	}
}

func TestViewMode_String(t *testing.T) {
	tests := []struct {
		mode     ViewMode
		expected string
	}{
		{ViewModeTasks, "tasks"},
		{ViewModeSchedules, "schedules"},
		{ViewMode(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.mode.String(); got != tt.expected {
				t.Errorf("ViewMode(%d).String() = %q, want %q", tt.mode, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// Schedule Sub-Model Wiring Tests
// =============================================================================

func TestNewModel_InitializesScheduleSubModels(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	// viewMode should default to ViewModeTasks (zero value)
	if m.viewMode != ViewModeTasks {
		t.Errorf("expected viewMode to be ViewModeTasks, got %v", m.viewMode)
	}

	// scheduleList should be initialized (verify by checking it doesn't panic)
	view := m.scheduleList.View(80, 20)
	if view == "" {
		t.Error("expected scheduleList.View() to return non-empty string")
	}

	// scheduleDetail should be initialized (verify by checking it doesn't panic)
	detailView := m.scheduleDetail.View()
	if detailView == "" {
		t.Error("expected scheduleDetail.View() to return non-empty string")
	}
}

func TestUpdate_TasksUpdated_PropagatesScheduleList(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	enabled := true
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Regular Task", Classification: "ready", Priority: "high"},
		{ID: "t2", Title: "Scheduled Task", Classification: "ready", Priority: "medium",
			Schedule: "0 */6 * * *", ScheduleEnabled: &enabled},
		{ID: "t3", Title: "Another Scheduled", Classification: "waiting", Priority: "low",
			Schedule: "0 0 * * *", ScheduleEnabled: &enabled},
	}
	stats := &types.TaskStats{Ready: 2, Waiting: 1}

	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: stats})
	model := updated.(Model)

	// scheduleList should have received the tasks and filtered to scheduled only
	selectedTask := model.scheduleList.SelectedTask()
	if selectedTask == nil {
		t.Fatal("expected scheduleList to have a selected task after TasksUpdatedMsg")
	}
	// Should auto-select first scheduled task (t2)
	if selectedTask.ID != "t2" {
		t.Errorf("expected scheduleList to select first scheduled task 't2', got '%s'", selectedTask.ID)
	}
}

func TestSyncScheduleDetail_SyncsWithScheduleList(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	enabled := true
	tasks := []types.ResolvedTask{
		{ID: "s1", Title: "Scheduled One", Classification: "ready", Priority: "high",
			Schedule: "0 */6 * * *", ScheduleEnabled: &enabled},
		{ID: "s2", Title: "Scheduled Two", Classification: "ready", Priority: "medium",
			Schedule: "0 0 * * *", ScheduleEnabled: &enabled},
	}
	m.scheduleList.SetTasks(tasks)

	// syncScheduleDetail should set the detail to the selected task
	(&m).syncScheduleDetail()

	// scheduleDetail should now have the selected task from scheduleList
	detailView := m.scheduleDetail.View()
	if !strings.Contains(detailView, "Scheduled One") {
		t.Errorf("expected scheduleDetail to show 'Scheduled One', got:\n%s", detailView)
	}
}

func TestSyncScheduleDetail_NilWhenNoSelection(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	// No tasks set, so no selection
	(&m).syncScheduleDetail()

	// scheduleDetail should show empty state
	detailView := m.scheduleDetail.View()
	if !strings.Contains(detailView, "Select a scheduled task") {
		t.Errorf("expected empty state placeholder, got:\n%s", detailView)
	}
}

// =============================================================================
// Phase 2: Key Remapping Tests
// =============================================================================

func TestUpdate_CKey_TogglesViewMode(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	// Start in ViewModeTasks
	if m.viewMode != ViewModeTasks {
		t.Fatalf("expected initial viewMode to be ViewModeTasks, got %v", m.viewMode)
	}

	// Press C to toggle to schedules
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	if m.viewMode != ViewModeSchedules {
		t.Errorf("after pressing C, expected ViewModeSchedules, got %v", m.viewMode)
	}

	// Press C again to toggle back to tasks
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.viewMode != ViewModeTasks {
		t.Errorf("after pressing C again, expected ViewModeTasks, got %v", m.viewMode)
	}
}

func TestUpdate_CKey_ClearsSelectionAndFilter(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	// Set up some multi-select state and filter
	m.selectedTasks = map[string]bool{"t1": true, "t2": true}
	m.filterState = FilterLocked
	m.filterQuery = "some filter"

	// Press C to toggle view
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	// Multi-select should be cleared
	if len(m.selectedTasks) != 0 {
		t.Errorf("expected selectedTasks to be cleared after C, got %d items", len(m.selectedTasks))
	}

	// Filter should be deactivated
	if m.filterState != FilterOff {
		t.Errorf("expected filterState to be FilterOff after C, got %v", m.filterState)
	}
	if m.filterQuery != "" {
		t.Errorf("expected filterQuery to be empty after C, got %q", m.filterQuery)
	}
}

func TestUpdate_CKey_SetsDetailVisibleInScheduleMode(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	// Hide detail panel
	m.detailVisible = false

	// Press C to enter schedule mode
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	// Detail should be visible in schedule mode (matching TS behavior)
	if !m.detailVisible {
		t.Error("expected detailVisible to be true when entering schedule mode")
	}
}

func TestUpdate_CKey_SetsFocusToPanelTasks(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	// Set focus to a different panel
	m.activePanel = PanelLogs

	// Press C to toggle view
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	// Focus should reset to tasks panel
	if m.activePanel != PanelTasks {
		t.Errorf("expected activePanel to be PanelTasks after C, got %v", m.activePanel)
	}
}

func TestUpdate_XKey_CancelsTask(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.activePanel = PanelTasks

	// Set up a task in in_progress status
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Running Task", Path: "projects/test/task/t1.md",
			Classification: "ready", Priority: "high", Status: "in_progress"},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 1}})
	m = updated.(Model)

	// Navigate to the task
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(jMsg)
	m = updated.(Model)

	// Press X (uppercase) to cancel
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	// Should open a confirmation modal
	if !m.modalManager.IsOpen() {
		t.Error("expected confirmation modal to open after pressing X on in_progress task")
	}
}

func TestUpdate_XKey_OnlyWorksOnInProgressTasks(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.activePanel = PanelTasks

	// Set up a task NOT in in_progress status
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Ready Task", Path: "projects/test/task/t1.md",
			Classification: "ready", Priority: "high", Status: "pending"},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 1}})
	m = updated.(Model)

	// Navigate to the task
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(jMsg)
	m = updated.(Model)

	// Press X on a non-in_progress task
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	// Should NOT open modal for non-in_progress task
	if m.modalManager.IsOpen() {
		t.Error("expected no modal for non-in_progress task when pressing X")
	}
}

func TestUpdate_XKey_OnlyWorksInViewModeTasks(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.activePanel = PanelTasks

	// Set up a task in in_progress status
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Running Task", Path: "projects/test/task/t1.md",
			Classification: "ready", Priority: "high", Status: "in_progress"},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 1}})
	m = updated.(Model)

	// Navigate to the task
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(jMsg)
	m = updated.(Model)

	// Switch to schedule view
	m.viewMode = ViewModeSchedules

	// Press X in schedule mode
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	// Should NOT open modal in schedule mode
	if m.modalManager.IsOpen() {
		t.Error("expected X key to be guarded by ViewModeTasks")
	}
}

func TestUpdate_TaskActionsGuardedByViewModeTasks(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}

	// Keys that should be guarded by ViewModeTasks
	guardedKeys := []struct {
		key  rune
		name string
	}{
		{'c', "complete"},
		{'x', "execute"},
		{'e', "edit"},
		{'s', "metadata"},
		{'d', "delete"},
	}

	for _, gk := range guardedKeys {
		t.Run(gk.name, func(t *testing.T) {
			m := NewModel(cfg)
			m.activePanel = PanelTasks

			// Set up tasks
			tasks := []types.ResolvedTask{
				{ID: "t1", Title: "Test Task", Path: "projects/test/task/t1.md",
					Classification: "ready", Priority: "high", Status: "in_progress"},
			}
			updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 1}})
			m = updated.(Model)

			// Navigate to the task
			jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
			updated, _ = m.Update(jMsg)
			m = updated.(Model)

			// Switch to schedule view
			m.viewMode = ViewModeSchedules

			// Press the guarded key
			msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{gk.key}}
			updated, cmd := m.Update(msg)
			m = updated.(Model)

			// Should produce no command and no modal
			if cmd != nil {
				t.Errorf("expected nil command for '%c' in schedule mode, got non-nil", gk.key)
			}
			if m.modalManager.IsOpen() {
				t.Errorf("expected no modal for '%c' in schedule mode", gk.key)
			}
		})
	}
}

func TestUpdate_JKKeys_NavigateScheduleListInScheduleMode(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.activePanel = PanelTasks

	// Set up scheduled tasks
	enabled := true
	tasks := []types.ResolvedTask{
		{ID: "s1", Title: "Schedule 1", Classification: "ready", Priority: "high",
			Schedule: "0 */6 * * *", ScheduleEnabled: &enabled},
		{ID: "s2", Title: "Schedule 2", Classification: "ready", Priority: "medium",
			Schedule: "0 0 * * *", ScheduleEnabled: &enabled},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 2}})
	m = updated.(Model)

	// Switch to schedule view
	m.viewMode = ViewModeSchedules

	// Press j to move down in schedule list
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(jMsg)
	m = updated.(Model)

	// Should have navigated in scheduleList (moved to second item)
	selected := m.scheduleList.SelectedTask()
	if selected == nil {
		t.Fatal("expected scheduleList to have a selected task after j in schedule mode")
	}
	if selected.ID != "s2" {
		t.Errorf("expected scheduleList selection to be 's2' after j, got '%s'", selected.ID)
	}
}

func TestUpdate_JKKeys_SyncScheduleDetailInScheduleMode(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.activePanel = PanelTasks

	// Set up scheduled tasks
	enabled := true
	tasks := []types.ResolvedTask{
		{ID: "s1", Title: "Schedule 1", Classification: "ready", Priority: "high",
			Schedule: "0 */6 * * *", ScheduleEnabled: &enabled},
		{ID: "s2", Title: "Schedule 2", Classification: "ready", Priority: "medium",
			Schedule: "0 0 * * *", ScheduleEnabled: &enabled},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 2}})
	m = updated.(Model)

	// Switch to schedule view
	m.viewMode = ViewModeSchedules

	// Press j to move down
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(jMsg)
	m = updated.(Model)

	// scheduleDetail should be synced with the new selection
	detailView := m.scheduleDetail.View()
	if !strings.Contains(detailView, "Schedule 2") {
		t.Errorf("expected scheduleDetail to show 'Schedule 2' after j, got:\n%s", detailView)
	}
}

// =============================================================================
// Phase 3: Rendering and HelpBar Tests
// =============================================================================

func TestRenderBaseView_ShowsScheduleListInScheduleMode(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 30

	// Set up scheduled tasks
	enabled := true
	tasks := []types.ResolvedTask{
		{ID: "s1", Title: "My Scheduled Task", Classification: "ready", Priority: "high",
			Schedule: "0 */6 * * *", ScheduleEnabled: &enabled},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 1}})
	m = updated.(Model)

	// Switch to schedule view
	m.viewMode = ViewModeSchedules

	// Render the view
	view := m.View()

	// Should show schedule list header "Scheduled" (from ScheduleList.View)
	if !strings.Contains(view, "Scheduled") {
		t.Errorf("expected schedule view to show 'Scheduled' header, got:\n%s", view)
	}

	// Should show the cron expression (schedule list shows these)
	if !strings.Contains(view, "0 */6 * * *") {
		t.Errorf("expected schedule view to show cron expression '0 */6 * * *', got:\n%s", view)
	}
}

func TestRenderDetailPanel_ShowsScheduleDetailInScheduleMode(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 30
	m.detailVisible = true

	// Set up scheduled tasks
	enabled := true
	tasks := []types.ResolvedTask{
		{ID: "s1", Title: "My Scheduled Task", Classification: "ready", Priority: "high",
			Schedule: "0 */6 * * *", ScheduleEnabled: &enabled},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 1}})
	m = updated.(Model)

	// Switch to schedule view and sync detail
	m.viewMode = ViewModeSchedules
	(&m).syncScheduleDetail()

	// Render the detail panel
	detailView := m.renderDetailPanel(60, 20)

	// Should show schedule detail content
	if !strings.Contains(detailView, "Schedule Details") {
		t.Errorf("expected detail panel to show 'Schedule Details' in schedule mode, got:\n%s", detailView)
	}
}

func TestHelpBar_ShowsScheduleShortcutsInScheduleMode(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelTasks
	h.ViewMode = ViewModeSchedules

	view := h.View(120, false, "test-project")

	// Should show "View" label (generic label matching TypeScript, may wrap across lines)
	if !strings.Contains(view, "View") {
		t.Errorf("expected helpbar in schedule mode to show 'View' label, got:\n%s", view)
	}

	// Should NOT show task-specific action shortcuts
	if strings.Contains(view, "Execute") {
		t.Errorf("expected helpbar in schedule mode to NOT show 'Execute', got:\n%s", view)
	}
	if strings.Contains(view, "Complete") {
		t.Errorf("expected helpbar in schedule mode to NOT show 'Complete', got:\n%s", view)
	}
}

func TestHelpBar_ShowsTaskShortcutsInTaskMode(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelTasks
	h.ViewMode = ViewModeTasks

	view := h.View(120, false, "test-project")

	// Should show task-specific shortcuts
	if !strings.Contains(view, "Execute") {
		t.Errorf("expected helpbar in task mode to show 'Execute', got:\n%s", view)
	}
	if !strings.Contains(view, "Cancel") {
		t.Errorf("expected helpbar in task mode to show 'Cancel', got:\n%s", view)
	}
}

func TestHelpBar_ShowsCancelOnXInTaskMode(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelTasks
	h.ViewMode = ViewModeTasks

	view := h.View(120, false, "test-project")

	// Cancel should be on X key now (not C)
	if !strings.Contains(view, "X") || !strings.Contains(view, "Cancel") {
		t.Errorf("expected helpbar to show 'X Cancel' in task mode, got:\n%s", view)
	}
}

func TestHelpBar_ShowsScheduleToggleOnC(t *testing.T) {
	h := NewHelpBar()
	h.ActivePanel = PanelTasks
	h.ViewMode = ViewModeTasks

	view := h.View(120, false, "test-project")

	// C should show "View" label (generic label matching TypeScript, may wrap across lines)
	if !strings.Contains(view, "View") {
		t.Errorf("expected helpbar to show 'View' label for C key, got:\n%s", view)
	}
}

func TestHelpModal_ShowsUpdatedKeyBindings(t *testing.T) {
	modal := NewHelpModal(false)
	view := modal.View()

	// Should show C as schedule toggle, not cancel
	if strings.Contains(view, "Cancel task") {
		t.Errorf("expected help modal to NOT show 'Cancel task' for C key, got:\n%s", view)
	}

	// Should show X as cancel
	if !strings.Contains(view, "Cancel") {
		t.Errorf("expected help modal to show 'Cancel' for X key, got:\n%s", view)
	}

	// Should show C as schedule toggle
	if !strings.Contains(view, "Schedule") || !strings.Contains(view, "toggle") {
		t.Errorf("expected help modal to show schedule toggle for C key, got:\n%s", view)
	}
}

// =============================================================================
// Yank (y key) Tests
// =============================================================================

func TestUpdate_YKey_CopiesTaskTitle(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.activePanel = PanelTasks

	// Set up a task
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "My Task Title", Path: "projects/test/task/t1.md",
			Classification: "ready", Priority: "high", Status: "pending"},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 1}})
	m = updated.(Model)

	// Navigate to the task
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(jMsg)
	m = updated.(Model)

	// Press y to yank
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	// Should set a status message (success or error depending on clipboard availability)
	if m.statusMessage == "" {
		t.Error("expected status message after pressing y, got empty")
	}
	// On platforms with clipboard, should show "Copied: My Task Title"
	// On platforms without, should show "Failed to copy"
	if !strings.Contains(m.statusMessage, "Copied:") && !strings.Contains(m.statusMessage, "Failed") {
		t.Errorf("expected status message about clipboard, got: %s", m.statusMessage)
	}
}

func TestUpdate_YKey_NoOpWithoutTask(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.activePanel = PanelTasks

	// No tasks loaded - press y
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	// Should NOT set a status message when no task is selected
	if m.statusMessage != "" {
		t.Errorf("expected no status message when no task selected, got: %s", m.statusMessage)
	}
}

func TestUpdate_YKey_NoOpOutsideTasksPanel(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.activePanel = PanelDetails // Not on tasks panel

	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "My Task", Path: "projects/test/task/t1.md",
			Classification: "ready", Priority: "high", Status: "pending"},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 1}})
	m = updated.(Model)

	// Press y while not on task panel
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	// Should NOT set a status message
	if m.statusMessage != "" {
		t.Errorf("expected no status message when not on tasks panel, got: %s", m.statusMessage)
	}
}

func TestUpdate_YKey_NoOpInScheduleView(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.activePanel = PanelTasks
	m.viewMode = ViewModeSchedules // Schedule view, not tasks view

	// Press y
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	// Should NOT set a status message
	if m.statusMessage != "" {
		t.Errorf("expected no status message in schedule view, got: %s", m.statusMessage)
	}
}

func TestHelpBar_ShowsYankShortcut(t *testing.T) {
	h := HelpBar{ActivePanel: PanelTasks, ViewMode: ViewModeTasks}
	view := h.View(120, false, "test-project")

	if !strings.Contains(view, "Yank") {
		t.Errorf("expected help bar to show 'Yank' shortcut, got:\n%s", view)
	}
}

func TestHelpBar_NoYankInScheduleView(t *testing.T) {
	h := HelpBar{ActivePanel: PanelTasks, ViewMode: ViewModeSchedules}
	view := h.View(120, false, "test-project")

	if strings.Contains(view, "Yank") {
		t.Errorf("expected help bar to NOT show 'Yank' in schedule view, got:\n%s", view)
	}
}

// =============================================================================
// Auto-Monitor Creation Tests (Phase 2)
// =============================================================================

func TestNewModel_InitializesAutoMonitorState(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	// seenFeatureIDs should be initialized as empty map
	if m.seenFeatureIDs == nil {
		t.Error("expected seenFeatureIDs to be initialized (non-nil)")
	}
	if len(m.seenFeatureIDs) != 0 {
		t.Errorf("expected seenFeatureIDs to be empty, got %d entries", len(m.seenFeatureIDs))
	}

	// initialSnapshotDone should be false
	if m.initialSnapshotDone {
		t.Error("expected initialSnapshotDone to be false initially")
	}

	// monitorClient should be initialized
	if m.monitorClient == nil {
		t.Error("expected monitorClient to be initialized (non-nil)")
	}
}

func TestAutoMonitor_FirstLoad_SnapshotsExistingFeatures(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.settings.AutoMonitors = true

	// First load with tasks that have feature_ids
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Classification: "ready", Priority: "high", FeatureID: "feat-1", ProjectID: "test-project"},
		{ID: "t2", Title: "Task 2", Classification: "ready", Priority: "medium", FeatureID: "feat-2", ProjectID: "test-project"},
		{ID: "t3", Title: "Task 3", Classification: "ready", Priority: "low"},
	}
	stats := &types.TaskStats{Ready: 3}

	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: stats})
	model := updated.(Model)

	// initialSnapshotDone should now be true
	if !model.initialSnapshotDone {
		t.Error("expected initialSnapshotDone to be true after first TasksUpdatedMsg")
	}

	// seenFeatureIDs should contain the existing features
	if !model.seenFeatureIDs["feat-1"] {
		t.Error("expected seenFeatureIDs to contain 'feat-1'")
	}
	if !model.seenFeatureIDs["feat-2"] {
		t.Error("expected seenFeatureIDs to contain 'feat-2'")
	}

	// Tasks without feature_id should NOT be in seenFeatureIDs
	if len(model.seenFeatureIDs) != 2 {
		t.Errorf("expected 2 entries in seenFeatureIDs, got %d", len(model.seenFeatureIDs))
	}
}

func TestAutoMonitor_SubsequentLoad_DetectsNewFeatures(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.settings.AutoMonitors = true

	// First load - snapshot existing features
	tasks1 := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Classification: "ready", Priority: "high", FeatureID: "feat-1", ProjectID: "test-project"},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks1, Stats: &types.TaskStats{Ready: 1}})
	m = updated.(Model)

	// Verify initial snapshot done
	if !m.initialSnapshotDone {
		t.Fatal("expected initialSnapshotDone to be true after first load")
	}

	// Second load - add a new feature
	tasks2 := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Classification: "ready", Priority: "high", FeatureID: "feat-1", ProjectID: "test-project"},
		{ID: "t2", Title: "Task 2", Classification: "ready", Priority: "medium", FeatureID: "feat-new", ProjectID: "test-project"},
	}
	updated, cmd := m.Update(TasksUpdatedMsg{Tasks: tasks2, Stats: &types.TaskStats{Ready: 2}})
	m = updated.(Model)

	// seenFeatureIDs should now contain both features
	if !m.seenFeatureIDs["feat-1"] {
		t.Error("expected seenFeatureIDs to still contain 'feat-1'")
	}
	if !m.seenFeatureIDs["feat-new"] {
		t.Error("expected seenFeatureIDs to contain 'feat-new'")
	}

	// Should return a batched command (SSE continuation + auto-monitor creation)
	if cmd == nil {
		t.Error("expected non-nil command (batched SSE + auto-monitor)")
	}
}

func TestAutoMonitor_SeenFeatureIDs_AlwaysUpdated_EvenWhenDisabled(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.settings.AutoMonitors = false // Disabled!

	// First load
	tasks1 := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Classification: "ready", Priority: "high", FeatureID: "feat-1", ProjectID: "test-project"},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks1, Stats: &types.TaskStats{Ready: 1}})
	m = updated.(Model)

	// Second load with new feature - even though auto-monitors disabled
	tasks2 := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Classification: "ready", Priority: "high", FeatureID: "feat-1", ProjectID: "test-project"},
		{ID: "t2", Title: "Task 2", Classification: "ready", Priority: "medium", FeatureID: "feat-2", ProjectID: "test-project"},
	}
	updated, _ = m.Update(TasksUpdatedMsg{Tasks: tasks2, Stats: &types.TaskStats{Ready: 2}})
	m = updated.(Model)

	// seenFeatureIDs should be updated even when AutoMonitors is false
	// This prevents a burst of monitor creation when re-enabling
	if !m.seenFeatureIDs["feat-2"] {
		t.Error("expected seenFeatureIDs to contain 'feat-2' even when AutoMonitors=false")
	}
}

func TestAutoMonitor_KnownFeatures_DoNotTriggerCreation(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)
	m.settings.AutoMonitors = true

	// First load
	tasks := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Classification: "ready", Priority: "high", FeatureID: "feat-1", ProjectID: "test-project"},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 1}})
	m = updated.(Model)

	// Second load with same features (no new ones)
	updated, _ = m.Update(TasksUpdatedMsg{Tasks: tasks, Stats: &types.TaskStats{Ready: 1}})
	m = updated.(Model)

	// seenFeatureIDs should still have exactly 1 entry
	if len(m.seenFeatureIDs) != 1 {
		t.Errorf("expected 1 entry in seenFeatureIDs, got %d", len(m.seenFeatureIDs))
	}
}

func TestAutoMonitor_MultiProject_DetectsNewFeaturesPerProject(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Project:  "proj-a",
		Projects: []string{"proj-a", "proj-b"},
	}
	m := NewModel(cfg)
	m.settings.AutoMonitors = true

	// First load from proj-a
	tasks1 := []types.ResolvedTask{
		{ID: "t1", Title: "Task 1", Classification: "ready", Priority: "high", FeatureID: "feat-1", ProjectID: "proj-a"},
	}
	updated, _ := m.Update(TasksUpdatedMsg{Tasks: tasks1, ProjectID: "proj-a", Stats: &types.TaskStats{Ready: 1}})
	m = updated.(Model)

	// Second load from proj-b with a new feature
	tasks2 := []types.ResolvedTask{
		{ID: "t2", Title: "Task 2", Classification: "ready", Priority: "high", FeatureID: "feat-b1", ProjectID: "proj-b"},
	}
	updated, cmd := m.Update(TasksUpdatedMsg{Tasks: tasks2, ProjectID: "proj-b", Stats: &types.TaskStats{Ready: 1}})
	m = updated.(Model)

	// seenFeatureIDs should contain both features
	if !m.seenFeatureIDs["feat-1"] {
		t.Error("expected seenFeatureIDs to contain 'feat-1'")
	}
	if !m.seenFeatureIDs["feat-b1"] {
		t.Error("expected seenFeatureIDs to contain 'feat-b1'")
	}

	// Should return a command (SSE continuation + auto-monitor for feat-b1)
	if cmd == nil {
		t.Error("expected non-nil command")
	}
}

func TestAutoMonitorCreatedMsg_Fields(t *testing.T) {
	// Verify the message type can be constructed and used
	msg := autoMonitorCreatedMsg{
		featureID:  "feat-1",
		templateID: "blocked-inspector",
		err:        nil,
	}
	if msg.featureID != "feat-1" {
		t.Errorf("featureID = %q, want %q", msg.featureID, "feat-1")
	}
	if msg.templateID != "blocked-inspector" {
		t.Errorf("templateID = %q, want %q", msg.templateID, "blocked-inspector")
	}
	if msg.err != nil {
		t.Errorf("expected nil error, got %v", msg.err)
	}
}

func TestAutoMonitorCreatedMsg_WithError(t *testing.T) {
	msg := autoMonitorCreatedMsg{
		featureID:  "feat-1",
		templateID: "feature-review",
		err:        fmt.Errorf("connection refused"),
	}
	if msg.err == nil {
		t.Error("expected non-nil error")
	}
}

func TestUpdate_AutoMonitorCreatedMsg_Success(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	updated, cmd := m.Update(autoMonitorCreatedMsg{
		featureID:  "feat-1",
		templateID: "blocked-inspector",
		err:        nil,
	})
	model := updated.(Model)

	// Should set a success status message
	if model.statusMessageType != "success" {
		t.Errorf("expected status message type 'success', got %q", model.statusMessageType)
	}
	if !strings.Contains(model.statusMessage, "feat-1") {
		t.Errorf("expected status message to contain feature ID, got %q", model.statusMessage)
	}

	// Should return nil (no follow-up command)
	if cmd != nil {
		t.Error("expected nil command after successful auto-monitor creation")
	}
}

func TestUpdate_AutoMonitorCreatedMsg_Error_SilentlyIgnored(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "test-project",
	}
	m := NewModel(cfg)

	updated, cmd := m.Update(autoMonitorCreatedMsg{
		featureID:  "feat-1",
		templateID: "blocked-inspector",
		err:        fmt.Errorf("server error"),
	})
	_ = updated.(Model)

	// Errors should not crash or block
	if cmd != nil {
		t.Error("expected nil command after auto-monitor creation error")
	}
}

func TestAutoCreateMonitorsCmd_ReturnsAutoMonitorCreatedMsg(t *testing.T) {
	// Create a monitor client pointing to a non-existent server
	client := NewMonitorClient("http://localhost:9999", "")

	cmd := autoCreateMonitorsCmd(client, "feat-1", "test-project")
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	// Execute the command - it will fail to connect but should return the right msg type
	result := cmd()
	msg, ok := result.(autoMonitorCreatedMsg)
	if !ok {
		t.Fatalf("expected autoMonitorCreatedMsg, got %T", result)
	}
	if msg.featureID != "feat-1" {
		t.Errorf("expected featureID 'feat-1', got %q", msg.featureID)
	}
	// err should be non-nil since we can't connect
	if msg.err == nil {
		t.Error("expected error since API is not running")
	}
}

func TestBlockedInspectorPrompt_ContainsFeatureAndProject(t *testing.T) {
	prompt := blockedInspectorPrompt("my-feature", "my-project")

	if !strings.Contains(prompt, "my-feature") {
		t.Errorf("expected prompt to contain feature ID 'my-feature', got: %s", prompt)
	}
	if !strings.Contains(prompt, "my-project") {
		t.Errorf("expected prompt to contain project 'my-project', got: %s", prompt)
	}
	if !strings.Contains(prompt, "blocked") {
		t.Errorf("expected prompt to contain 'blocked', got: %s", prompt)
	}
}

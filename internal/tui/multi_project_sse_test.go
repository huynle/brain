package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/huynle/brain-api/internal/types"
)

// TestModelHasTasksByProjectField verifies Model has tasksByProject field.
func TestModelHasTasksByProjectField(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)

	// Test that field exists and is initialized
	if m.tasksByProject == nil {
		t.Fatal("tasksByProject should be initialized")
	}

	// Test that it's empty initially
	if len(m.tasksByProject) != 0 {
		t.Errorf("tasksByProject should be empty initially, got %d entries", len(m.tasksByProject))
	}
}

// TestModelHasSSEClientsField verifies Model has sseClients field.
func TestModelHasSSEClientsField(t *testing.T) {
	// Single-project mode: sseClients should be empty
	cfg1 := Config{
		APIURL:  "http://localhost:3333",
		Project: "single-proj",
	}
	m1 := NewModel(cfg1)

	if m1.sseClients == nil {
		t.Fatal("sseClients should be initialized")
	}

	if len(m1.sseClients) != 0 {
		t.Errorf("sseClients should be empty in single-project mode, got %d entries", len(m1.sseClients))
	}

	// Multi-project mode: sseClients should be populated
	cfg2 := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m2 := NewModel(cfg2)

	if m2.sseClients == nil {
		t.Fatal("sseClients should be initialized")
	}

	if len(m2.sseClients) != 2 {
		t.Errorf("sseClients should have 2 entries in multi-project mode, got %d", len(m2.sseClients))
	}
}

// TestTasksUpdatedMsgHasProjectID verifies TasksUpdatedMsg has ProjectID field.
func TestTasksUpdatedMsgHasProjectID(t *testing.T) {
	msg := TasksUpdatedMsg{
		ProjectID: "test-project",
		Tasks:     []types.ResolvedTask{},
		Stats:     nil,
	}

	if msg.ProjectID != "test-project" {
		t.Errorf("ProjectID field not accessible, expected 'test-project', got '%s'", msg.ProjectID)
	}
}

// TestResolvedTaskHasProjectIDField verifies ResolvedTask type has ProjectID field.
func TestResolvedTaskHasProjectIDField(t *testing.T) {
	task := types.ResolvedTask{
		ID:        "test-id",
		ProjectID: "test-project",
	}

	if task.ProjectID != "test-project" {
		t.Errorf("ProjectID field not accessible, expected 'test-project', got '%s'", task.ProjectID)
	}
}

// TestInitCreatesMultipleSSEClientsForMultiProject verifies Init() creates SSE clients for each project.
func TestInitCreatesMultipleSSEClientsForMultiProject(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2", "proj3"},
	}
	m := NewModel(cfg)

	// After NewModel(), sseClients should have one entry per project
	if len(m.sseClients) != 3 {
		t.Errorf("Expected 3 SSE clients for 3 projects, got %d", len(m.sseClients))
	}

	// Verify each project has a client
	for _, proj := range cfg.Projects {
		if m.sseClients[proj] == nil {
			t.Errorf("Missing SSE client for project '%s'", proj)
		}
	}
}

// TestInitCreatesSingleSSEClientForSingleProject verifies Init() uses legacy single-client mode for single project.
func TestInitCreatesSingleSSEClientForSingleProject(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3333",
		Project: "single-proj",
	}
	m := NewModel(cfg)

	// For single-project mode, should use legacy sseClient field
	if m.sseClient == nil {
		t.Error("Expected legacy sseClient to be set for single-project mode")
	}

	// sseClients should be empty in single-project mode
	if len(m.sseClients) != 0 {
		t.Errorf("Expected sseClients to be empty in single-project mode, got %d", len(m.sseClients))
	}
}

// TestTasksUpdatedMsgStoresTasksPerProject verifies Update() stores tasks by project.
func TestTasksUpdatedMsgStoresTasksPerProject(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)

	// Simulate receiving tasks for proj1
	tasks1 := []types.ResolvedTask{
		{ID: "task1", Title: "Task 1", ProjectID: "proj1"},
		{ID: "task2", Title: "Task 2", ProjectID: "proj1"},
	}
	msg1 := TasksUpdatedMsg{
		ProjectID: "proj1",
		Tasks:     tasks1,
		Stats:     nil,
	}

	updatedModel, _ := m.Update(msg1)
	m = updatedModel.(Model)

	// Verify tasks are stored under proj1
	if len(m.tasksByProject["proj1"]) != 2 {
		t.Errorf("Expected 2 tasks for proj1, got %d", len(m.tasksByProject["proj1"]))
	}

	// Simulate receiving tasks for proj2
	tasks2 := []types.ResolvedTask{
		{ID: "task3", Title: "Task 3", ProjectID: "proj2"},
	}
	msg2 := TasksUpdatedMsg{
		ProjectID: "proj2",
		Tasks:     tasks2,
		Stats:     nil,
	}

	updatedModel2, _ := m.Update(msg2)
	m = updatedModel2.(Model)

	// Verify tasks are stored under proj2
	if len(m.tasksByProject["proj2"]) != 1 {
		t.Errorf("Expected 1 task for proj2, got %d", len(m.tasksByProject["proj2"]))
	}

	// Verify proj1 tasks are still there
	if len(m.tasksByProject["proj1"]) != 2 {
		t.Errorf("Expected proj1 tasks to persist, got %d", len(m.tasksByProject["proj1"]))
	}
}

// =============================================================================
// Gap 4: SSE Messages carry ProjectID
// =============================================================================

// TestSSEConnectedMsgHasProjectID verifies SSEConnectedMsg carries ProjectID.
func TestSSEConnectedMsgHasProjectID(t *testing.T) {
	msg := SSEConnectedMsg{ProjectID: "proj1"}
	if msg.ProjectID != "proj1" {
		t.Errorf("Expected ProjectID 'proj1', got '%s'", msg.ProjectID)
	}
}

// TestSSEDisconnectedMsgHasProjectID verifies SSEDisconnectedMsg carries ProjectID.
func TestSSEDisconnectedMsgHasProjectID(t *testing.T) {
	msg := SSEDisconnectedMsg{ProjectID: "proj2"}
	if msg.ProjectID != "proj2" {
		t.Errorf("Expected ProjectID 'proj2', got '%s'", msg.ProjectID)
	}
}

// TestSSEErrorMsgHasProjectID verifies SSEErrorMsg carries ProjectID.
func TestSSEErrorMsgHasProjectID(t *testing.T) {
	msg := SSEErrorMsg{ProjectID: "proj3", Err: nil}
	if msg.ProjectID != "proj3" {
		t.Errorf("Expected ProjectID 'proj3', got '%s'", msg.ProjectID)
	}
}

// TestParseSSEEvent_TasksSnapshot_SetsProjectID verifies parseSSEEvent extracts ProjectID from payload.
func TestParseSSEEvent_TasksSnapshot_SetsProjectID(t *testing.T) {
	lines := []string{
		`event: tasks_snapshot`,
		`data: {"type":"tasks_snapshot","projectId":"my-project","tasks":[],"count":0,"stats":{"total":5,"ready":2,"waiting":1,"blocked":1,"not_pending":1}}`,
	}

	msg, err := parseSSEEvent(lines)
	if err != nil {
		t.Fatalf("parseSSEEvent returned error: %v", err)
	}

	tasksMsg, ok := msg.(TasksUpdatedMsg)
	if !ok {
		t.Fatalf("Expected TasksUpdatedMsg, got %T", msg)
	}

	if tasksMsg.ProjectID != "my-project" {
		t.Errorf("Expected ProjectID 'my-project', got '%s'", tasksMsg.ProjectID)
	}
}

// TestParseSSEEvent_Connected_SetsProjectID verifies parseSSEEvent extracts ProjectID from connected event.
func TestParseSSEEvent_Connected_SetsProjectID(t *testing.T) {
	lines := []string{
		`event: connected`,
		`data: {"type":"connected","projectId":"my-project","transport":"sse","timestamp":"2024-01-01T00:00:00Z"}`,
	}

	msg, err := parseSSEEvent(lines)
	if err != nil {
		t.Fatalf("parseSSEEvent returned error: %v", err)
	}

	connMsg, ok := msg.(SSEConnectedMsg)
	if !ok {
		t.Fatalf("Expected SSEConnectedMsg, got %T", msg)
	}

	if connMsg.ProjectID != "my-project" {
		t.Errorf("Expected ProjectID 'my-project', got '%s'", connMsg.ProjectID)
	}
}

// TestParseSSEEvent_Error_SetsProjectID verifies parseSSEEvent extracts ProjectID from error event.
func TestParseSSEEvent_Error_SetsProjectID(t *testing.T) {
	lines := []string{
		`event: error`,
		`data: {"type":"error","projectId":"my-project","message":"something went wrong"}`,
	}

	msg, err := parseSSEEvent(lines)
	if err != nil {
		t.Fatalf("parseSSEEvent returned error: %v", err)
	}

	errMsg, ok := msg.(SSEErrorMsg)
	if !ok {
		t.Fatalf("Expected SSEErrorMsg, got %T", msg)
	}

	if errMsg.ProjectID != "my-project" {
		t.Errorf("Expected ProjectID 'my-project', got '%s'", errMsg.ProjectID)
	}
}

// =============================================================================
// Gap 4c: SSE Connection/Disconnection routing to per-project clients
// =============================================================================

// TestSSEConnectedMsg_RoutesToProjectClient verifies SSEConnectedMsg routes to correct per-project client.
func TestSSEConnectedMsg_RoutesToProjectClient(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)

	// Send SSEConnectedMsg with ProjectID
	msg := SSEConnectedMsg{ProjectID: "proj1"}
	updatedModel, cmd := m.Update(msg)
	m = updatedModel.(Model)

	// Model should be connected
	if !m.connected {
		t.Error("Expected connected=true after SSEConnectedMsg")
	}

	// Should return a cmd (WaitForNextMsg from the project-specific client)
	if cmd == nil {
		t.Error("Expected non-nil cmd after SSEConnectedMsg with ProjectID")
	}
}

// TestSSEDisconnectedMsg_RoutesToProjectClient verifies SSEDisconnectedMsg routes to correct per-project client.
func TestSSEDisconnectedMsg_RoutesToProjectClient(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)

	// Send SSEDisconnectedMsg with ProjectID
	msg := SSEDisconnectedMsg{ProjectID: "proj1"}
	updatedModel, cmd := m.Update(msg)
	_ = updatedModel.(Model)

	// Should return a cmd (Reconnect from the project-specific client)
	if cmd == nil {
		t.Error("Expected non-nil cmd after SSEDisconnectedMsg with ProjectID")
	}
}

// TestSSEErrorMsg_RoutesToProjectClient verifies SSEErrorMsg routes to correct per-project client.
func TestSSEErrorMsg_RoutesToProjectClient(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)

	// Send SSEErrorMsg with ProjectID
	msg := SSEErrorMsg{ProjectID: "proj1", Err: nil}
	updatedModel, cmd := m.Update(msg)
	_ = updatedModel.(Model)

	// Should return a cmd (WaitForNextMsg from the project-specific client)
	if cmd == nil {
		t.Error("Expected non-nil cmd after SSEErrorMsg with ProjectID")
	}
}

// =============================================================================
// Gap 2: TasksUpdatedMsg calls ProjectTabs.UpdateStats
// =============================================================================

// TestTasksUpdatedMsg_UpdatesProjectTabStats verifies that receiving TasksUpdatedMsg
// with ProjectID and Stats updates ProjectTabs stats in multi-project mode.
func TestTasksUpdatedMsg_UpdatesProjectTabStats(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)

	// Send TasksUpdatedMsg with stats for proj1
	stats := &types.TaskStats{Ready: 3, Waiting: 1, Blocked: 0, NotPending: 2}
	msg := TasksUpdatedMsg{
		ProjectID: "proj1",
		Tasks:     []types.ResolvedTask{{ID: "t1", ProjectID: "proj1"}},
		Stats:     stats,
	}

	updatedModel, _ := m.Update(msg)
	m = updatedModel.(Model)

	// ProjectTabs should have stats for proj1
	projStats, ok := m.projectTabs.StatsByProject["proj1"]
	if !ok {
		t.Fatal("Expected ProjectTabs to have stats for proj1")
	}
	if projStats.Ready != 3 {
		t.Errorf("Expected proj1 Ready=3, got %d", projStats.Ready)
	}
	if projStats.Waiting != 1 {
		t.Errorf("Expected proj1 Waiting=1, got %d", projStats.Waiting)
	}

	// AggregateStats should reflect proj1 stats (only project with stats so far)
	if m.projectTabs.AggregateStats.Ready != 3 {
		t.Errorf("Expected AggregateStats.Ready=3, got %d", m.projectTabs.AggregateStats.Ready)
	}
}

// TestTasksUpdatedMsg_AggregatesMultipleProjects verifies aggregate stats across projects.
func TestTasksUpdatedMsg_AggregatesMultipleProjects(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)

	// Send stats for proj1
	msg1 := TasksUpdatedMsg{
		ProjectID: "proj1",
		Tasks:     []types.ResolvedTask{{ID: "t1", ProjectID: "proj1"}},
		Stats:     &types.TaskStats{Ready: 3, Waiting: 1, Blocked: 0, NotPending: 2},
	}
	updatedModel, _ := m.Update(msg1)
	m = updatedModel.(Model)

	// Send stats for proj2
	msg2 := TasksUpdatedMsg{
		ProjectID: "proj2",
		Tasks:     []types.ResolvedTask{{ID: "t2", ProjectID: "proj2"}},
		Stats:     &types.TaskStats{Ready: 5, Waiting: 2, Blocked: 1, NotPending: 0},
	}
	updatedModel, _ = m.Update(msg2)
	m = updatedModel.(Model)

	// AggregateStats should sum both projects
	agg := m.projectTabs.AggregateStats
	if agg.Ready != 8 {
		t.Errorf("Expected AggregateStats.Ready=8 (3+5), got %d", agg.Ready)
	}
	if agg.Waiting != 3 {
		t.Errorf("Expected AggregateStats.Waiting=3 (1+2), got %d", agg.Waiting)
	}
	if agg.Blocked != 1 {
		t.Errorf("Expected AggregateStats.Blocked=1 (0+1), got %d", agg.Blocked)
	}
}

// TestTasksUpdatedMsg_SetsCurrentStatsFromProjectTabs verifies m.stats is set from ProjectTabs.CurrentStats().
func TestTasksUpdatedMsg_SetsCurrentStatsFromProjectTabs(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)
	// Active tab is "all" by default

	// Send stats for proj1
	msg1 := TasksUpdatedMsg{
		ProjectID: "proj1",
		Tasks:     []types.ResolvedTask{{ID: "t1", ProjectID: "proj1"}},
		Stats:     &types.TaskStats{Ready: 3, Waiting: 1, Blocked: 0, NotPending: 2},
	}
	updatedModel, _ := m.Update(msg1)
	m = updatedModel.(Model)

	// On "all" tab, m.stats should be aggregate stats
	if m.stats.Ready != 3 {
		t.Errorf("Expected m.stats.Ready=3 (aggregate from proj1 only), got %d", m.stats.Ready)
	}

	// StatusBar should also have the same stats
	if m.statusBar.Stats.Ready != 3 {
		t.Errorf("Expected statusBar.Stats.Ready=3, got %d", m.statusBar.Stats.Ready)
	}
}

// =============================================================================
// Gap 3: Tab Switch Updates Stats
// =============================================================================

// TestTabSwitch_AllTab_SetsAggregateStats verifies switching to "all" tab sets aggregate stats.
func TestTabSwitch_AllTab_SetsAggregateStats(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 40

	// Pre-populate stats in ProjectTabs
	m.projectTabs.UpdateStats("proj1", TaskStats{Ready: 3, Waiting: 1})
	m.projectTabs.UpdateStats("proj2", TaskStats{Ready: 5, Waiting: 2})

	// Pre-populate tasks
	m.tasksByProject["proj1"] = []types.ResolvedTask{{ID: "t1", ProjectID: "proj1"}}
	m.tasksByProject["proj2"] = []types.ResolvedTask{{ID: "t2", ProjectID: "proj2"}}

	// Start on proj1 tab
	m.projectTabs.ActiveIndex = 1
	m.activeProjectID = "proj1"

	// Switch to "all" tab (press 'h' to go back)
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	// Stats should be aggregate
	if model.stats.Ready != 8 {
		t.Errorf("Expected aggregate Ready=8 (3+5), got %d", model.stats.Ready)
	}
	if model.stats.Waiting != 3 {
		t.Errorf("Expected aggregate Waiting=3 (1+2), got %d", model.stats.Waiting)
	}

	// StatusBar should also have aggregate stats
	if model.statusBar.Stats.Ready != 8 {
		t.Errorf("Expected statusBar.Stats.Ready=8, got %d", model.statusBar.Stats.Ready)
	}
}

// TestTabSwitch_ProjectTab_SetsProjectStats verifies switching to a project tab sets that project's stats.
func TestTabSwitch_ProjectTab_SetsProjectStats(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 40

	// Pre-populate stats
	m.projectTabs.UpdateStats("proj1", TaskStats{Ready: 3, Waiting: 1})
	m.projectTabs.UpdateStats("proj2", TaskStats{Ready: 5, Waiting: 2})

	// Pre-populate tasks
	m.tasksByProject["proj1"] = []types.ResolvedTask{{ID: "t1", ProjectID: "proj1"}}
	m.tasksByProject["proj2"] = []types.ResolvedTask{{ID: "t2", ProjectID: "proj2"}}

	// Start on "all" tab
	m.projectTabs.ActiveIndex = 0
	m.activeProjectID = "all"

	// Switch to proj1 tab (press 'l')
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	// Stats should be proj1-specific
	if model.stats.Ready != 3 {
		t.Errorf("Expected proj1 Ready=3, got %d", model.stats.Ready)
	}
	if model.stats.Waiting != 1 {
		t.Errorf("Expected proj1 Waiting=1, got %d", model.stats.Waiting)
	}

	// StatusBar should also have proj1 stats
	if model.statusBar.Stats.Ready != 3 {
		t.Errorf("Expected statusBar.Stats.Ready=3, got %d", model.statusBar.Stats.Ready)
	}
}

// TestTabSwitch_NumberKey_SetsCorrectStats verifies number key tab jump sets correct stats.
func TestTabSwitch_NumberKey_SetsCorrectStats(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)
	m.width = 120
	m.height = 40

	// Pre-populate stats
	m.projectTabs.UpdateStats("proj1", TaskStats{Ready: 3})
	m.projectTabs.UpdateStats("proj2", TaskStats{Ready: 7})

	// Pre-populate tasks
	m.tasksByProject["proj1"] = []types.ResolvedTask{{ID: "t1", ProjectID: "proj1"}}
	m.tasksByProject["proj2"] = []types.ResolvedTask{{ID: "t2", ProjectID: "proj2"}}

	// Jump to tab 3 (proj2)
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}}
	updated, _ := m.Update(msg)
	model := updated.(Model)

	// Stats should be proj2-specific
	if model.stats.Ready != 7 {
		t.Errorf("Expected proj2 Ready=7, got %d", model.stats.Ready)
	}
}

// =============================================================================
// Gap 1: Init() connects per-project SSE clients
// =============================================================================

// TestInit_MultiProject_ReturnsMultipleConnectCmds verifies Init() returns cmds for all project SSE clients.
func TestInit_MultiProject_ReturnsMultipleConnectCmds(t *testing.T) {
	cfg := Config{
		APIURL:   "http://localhost:3333",
		Projects: []string{"proj1", "proj2"},
	}
	m := NewModel(cfg)

	// Init() should return a batch cmd that includes connections for all projects
	cmd := m.Init()

	// The cmd should not be nil
	if cmd == nil {
		t.Fatal("Expected Init() to return non-nil cmd")
	}

	// We can't easily inspect the batch cmd contents, but we can verify
	// that the model has the right number of SSE clients
	if len(m.sseClients) != 2 {
		t.Errorf("Expected 2 SSE clients, got %d", len(m.sseClients))
	}
}

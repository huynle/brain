package tui

import (
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestRunnerPanel_NewRunnerPanel(t *testing.T) {
	rp := NewRunnerPanel()
	if len(rp.runners) != 0 {
		t.Errorf("expected empty runners, got %d", len(rp.runners))
	}
	if rp.cursor != 0 {
		t.Errorf("expected cursor 0, got %d", rp.cursor)
	}
}

func TestRunnerPanel_SetRunners(t *testing.T) {
	rp := NewRunnerPanel()
	runners := []types.RunnerInfo{
		{RunnerID: "runner-1", Hostname: "host1", Status: types.RunnerStatusOnline, MaxParallel: 4},
		{RunnerID: "runner-2", Hostname: "host2", Status: types.RunnerStatusStale, MaxParallel: 2},
		{RunnerID: "runner-3", Hostname: "host3", Status: types.RunnerStatusOffline, MaxParallel: 1},
	}
	rp.SetRunners(runners)

	if len(rp.runners) != 3 {
		t.Fatalf("expected 3 runners, got %d", len(rp.runners))
	}
	if rp.runners[0].RunnerID != "runner-1" {
		t.Errorf("expected runner-1, got %s", rp.runners[0].RunnerID)
	}
}

func TestRunnerPanel_Navigation(t *testing.T) {
	rp := NewRunnerPanel()
	runners := []types.RunnerInfo{
		{RunnerID: "runner-1", Hostname: "host1", Status: types.RunnerStatusOnline},
		{RunnerID: "runner-2", Hostname: "host2", Status: types.RunnerStatusStale},
		{RunnerID: "runner-3", Hostname: "host3", Status: types.RunnerStatusOffline},
	}
	rp.SetRunners(runners)

	// Start at 0
	if rp.cursor != 0 {
		t.Errorf("expected cursor 0, got %d", rp.cursor)
	}

	// Move down
	rp.MoveDown()
	if rp.cursor != 1 {
		t.Errorf("expected cursor 1 after MoveDown, got %d", rp.cursor)
	}

	rp.MoveDown()
	if rp.cursor != 2 {
		t.Errorf("expected cursor 2 after second MoveDown, got %d", rp.cursor)
	}

	// Can't go past end
	rp.MoveDown()
	if rp.cursor != 2 {
		t.Errorf("expected cursor 2 (clamped), got %d", rp.cursor)
	}

	// Move up
	rp.MoveUp()
	if rp.cursor != 1 {
		t.Errorf("expected cursor 1 after MoveUp, got %d", rp.cursor)
	}

	rp.MoveUp()
	if rp.cursor != 0 {
		t.Errorf("expected cursor 0 after second MoveUp, got %d", rp.cursor)
	}

	// Can't go past start
	rp.MoveUp()
	if rp.cursor != 0 {
		t.Errorf("expected cursor 0 (clamped), got %d", rp.cursor)
	}
}

func TestRunnerPanel_NavigationUsesRenderedViewportHeight(t *testing.T) {
	rp := NewRunnerPanel()
	runners := []types.RunnerInfo{
		{RunnerID: "runner-1", Hostname: "host1", Status: types.RunnerStatusOnline},
		{RunnerID: "runner-2", Hostname: "host2", Status: types.RunnerStatusOnline},
		{RunnerID: "runner-3", Hostname: "host3", Status: types.RunnerStatusOnline},
		{RunnerID: "runner-4", Hostname: "host4", Status: types.RunnerStatusOnline},
	}
	rp.SetRunners(runners)

	// Match live TUI usage: the panel is rendered with a viewport height, but
	// SetSize is not called separately before j/k navigation.
	_ = rp.View(80, 5)
	rp.MoveDown()

	if rp.cursor != 1 {
		t.Fatalf("expected cursor 1 after MoveDown, got %d", rp.cursor)
	}
	if rp.scrollTop != 0 {
		t.Fatalf("expected scrollTop to stay 0 while cursor remains visible, got %d", rp.scrollTop)
	}
}

func TestRunnerPanel_SelectedRunner(t *testing.T) {
	rp := NewRunnerPanel()

	// No runners: returns nil
	if rp.SelectedRunner() != nil {
		t.Error("expected nil when no runners")
	}

	runners := []types.RunnerInfo{
		{RunnerID: "runner-1", Hostname: "host1"},
		{RunnerID: "runner-2", Hostname: "host2"},
	}
	rp.SetRunners(runners)

	selected := rp.SelectedRunner()
	if selected == nil {
		t.Fatal("expected non-nil runner")
	}
	if selected.RunnerID != "runner-1" {
		t.Errorf("expected runner-1, got %s", selected.RunnerID)
	}

	rp.MoveDown()
	selected = rp.SelectedRunner()
	if selected.RunnerID != "runner-2" {
		t.Errorf("expected runner-2, got %s", selected.RunnerID)
	}
}

func TestRunnerPanel_CursorClampOnSetRunners(t *testing.T) {
	rp := NewRunnerPanel()
	runners := []types.RunnerInfo{
		{RunnerID: "runner-1"},
		{RunnerID: "runner-2"},
		{RunnerID: "runner-3"},
	}
	rp.SetRunners(runners)
	rp.MoveDown()
	rp.MoveDown()
	if rp.cursor != 2 {
		t.Fatalf("expected cursor 2, got %d", rp.cursor)
	}

	// Reduce runner list: cursor should clamp
	rp.SetRunners([]types.RunnerInfo{{RunnerID: "runner-1"}})
	if rp.cursor != 0 {
		t.Errorf("expected cursor 0 after clamp, got %d", rp.cursor)
	}
}

func TestRunnerPanel_View(t *testing.T) {
	rp := NewRunnerPanel()
	runners := []types.RunnerInfo{
		{RunnerID: "runner-1", Hostname: "host1", Status: types.RunnerStatusOnline, MaxParallel: 4},
		{RunnerID: "runner-2", Hostname: "host2", Status: types.RunnerStatusStale, MaxParallel: 2},
	}
	rp.SetRunners(runners)

	view := rp.View(80, 20)

	// Should contain header
	if !strings.Contains(view, "Runners (2)") {
		t.Error("expected 'Runners (2)' in view")
	}

	// Should contain runner IDs
	if !strings.Contains(view, "runner-1") {
		t.Error("expected 'runner-1' in view")
	}
	if !strings.Contains(view, "runner-2") {
		t.Error("expected 'runner-2' in view")
	}
}

func TestRunnerPanel_ViewEmpty(t *testing.T) {
	rp := NewRunnerPanel()
	view := rp.View(80, 20)

	if !strings.Contains(view, "Runners (0)") {
		t.Error("expected 'Runners (0)' in view")
	}
	if !strings.Contains(view, "No runners registered") {
		t.Error("expected 'No runners registered' in view")
	}
}

func TestRunnerPanel_ViewDetail(t *testing.T) {
	rp := NewRunnerPanel()
	runners := []types.RunnerInfo{
		{
			RunnerID:    "runner-1",
			Hostname:    "my-laptop",
			Status:      types.RunnerStatusOnline,
			MaxParallel: 4,
			Executors:   []string{"opencode", "shell"},
			FeatureIDs:  "feature-auth,feature-api",
			FeatureAssignments: []types.FeatureAssignmentResponse{
				{ProjectID: "brain-api", FeatureID: "feature-auth", RunnerID: "runner-1", Source: "manual", Status: "active"},
				{ProjectID: "brain-api", FeatureID: "feature-api", RunnerID: "runner-1", Source: "auto", Status: "active"},
			},
			RegisteredAt:  "2025-01-01T00:00:00Z",
			LastHeartbeat: "2025-01-01T00:01:00Z",
			Labels:        map[string]string{"env": "dev"},
		},
	}
	rp.SetRunners(runners)

	detail := rp.ViewDetail(80, 20)

	if !strings.Contains(detail, "runner-1") {
		t.Error("expected runner ID in detail")
	}
	if !strings.Contains(detail, "my-laptop") {
		t.Error("expected hostname in detail")
	}
	if !strings.Contains(detail, "online") {
		t.Error("expected status in detail")
	}
	if !strings.Contains(detail, "opencode, shell") {
		t.Error("expected executors in detail")
	}
	if !strings.Contains(detail, "feature-auth,feature-api") {
		t.Error("expected feature IDs in detail")
	}
	if !strings.Contains(detail, "feature-auth (brain-api, manual)") {
		t.Error("expected manual assignment details")
	}
	if !strings.Contains(detail, "feature-api (brain-api, auto)") {
		t.Error("expected auto assignment details")
	}
	if !strings.Contains(detail, "env=dev") {
		t.Error("expected labels in detail")
	}
}

func TestRunnerPanel_ViewDetailNoSelection(t *testing.T) {
	rp := NewRunnerPanel()
	detail := rp.ViewDetail(80, 20)

	if !strings.Contains(detail, "No runner selected") {
		t.Error("expected 'No runner selected' when no runners")
	}
}

func TestRunnerPanel_StatusIndicators(t *testing.T) {
	rp := NewRunnerPanel()

	tests := []struct {
		status types.RunnerStatus
	}{
		{types.RunnerStatusOnline},
		{types.RunnerStatusStale},
		{types.RunnerStatusOffline},
	}

	for _, tt := range tests {
		indicator := rp.statusIndicator(tt.status)
		if indicator == "" {
			t.Errorf("expected non-empty indicator for status %s", tt.status)
		}
	}
}

func TestNextPanelWithRunners(t *testing.T) {
	tests := []struct {
		name          string
		current       Panel
		detailVisible bool
		logsVisible   bool
		runnerVisible bool
		expected      Panel
	}{
		{
			name:          "tasks -> runners when only runners visible",
			current:       PanelTasks,
			runnerVisible: true,
			expected:      PanelRunners,
		},
		{
			name:          "runners -> tasks (cycle)",
			current:       PanelRunners,
			runnerVisible: true,
			expected:      PanelTasks,
		},
		{
			name:          "tasks -> details -> runners cycle",
			current:       PanelDetails,
			detailVisible: true,
			runnerVisible: true,
			expected:      PanelRunners,
		},
		{
			name:          "no runners visible: tasks -> tasks",
			current:       PanelTasks,
			runnerVisible: false,
			expected:      PanelTasks,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NextPanelWithRunners(tt.current, tt.detailVisible, tt.logsVisible, tt.runnerVisible)
			if result != tt.expected {
				t.Errorf("expected panel %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestRunnerPanel_ViewShowsDispatchAndCapacity(t *testing.T) {
	rp := NewRunnerPanel()
	rp.SetRunners([]types.RunnerInfo{
		{
			RunnerID:     "runner-push",
			Hostname:     "host1",
			Status:       types.RunnerStatusOnline,
			DispatchPush: true,
			ActiveTasks:  2,
			MaxParallel:  4,
			Executors:    []string{"opencode"},
		},
		{
			RunnerID:    "runner-drain",
			Hostname:    "host2",
			Status:      types.RunnerStatusOnline,
			Draining:    true,
			ActiveTasks: 1,
			MaxParallel: 1,
			Executors:   []string{"pi"},
		},
	})

	view := rp.View(120, 20)

	for _, want := range []string{"Tasks", "Dispatch", "2/4", "push", "1/1", "poll,drain"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in view:\n%s", want, view)
		}
	}
}

func TestRunnerPanel_ViewDetailShowsDispatchPlacementFields(t *testing.T) {
	rp := NewRunnerPanel()
	rp.SetRunners([]types.RunnerInfo{
		{
			RunnerID:       "runner-1",
			MachineID:      "machine-a",
			Hostname:       "host1",
			Status:         types.RunnerStatusOnline,
			DispatchPush:   true,
			Draining:       true,
			ActiveTasks:    2,
			MaxParallel:    4,
			Projects:       []string{"brain-api"},
			Capabilities:   []string{"gpu", "worktree"},
			WorkspaceRoots: []string{"/work/brain"},
			Resources:      map[string]interface{}{"cpu": "4"},
			Capacity:       map[string]interface{}{"slots": 4},
			Executors:      []string{"opencode"},
		},
	})

	detail := rp.ViewDetail(120, 40)

	for _, want := range []string{
		"Machine:       machine-a",
		"Tasks:         2/4",
		"Dispatch:      push",
		"Draining:      yes",
		"Projects:      brain-api",
		"Capabilities:  gpu, worktree",
		"Workspaces:    /work/brain",
		"Resources:     cpu=4",
		"Capacity:      slots=4",
	} {
		if !strings.Contains(detail, want) {
			t.Fatalf("expected %q in detail:\n%s", want, detail)
		}
	}
}

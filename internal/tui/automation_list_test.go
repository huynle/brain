package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/huynle/brain-api/internal/types"
)

func TestAutomationList_SetEntriesAndTasks_MergesAutomationAndScheduledRows(t *testing.T) {
	al := NewAutomationList()
	al.SetEntriesAndTasks(
		[]types.BrainEntry{
			{
				ID:     "auto1",
				Title:  "Review feature",
				Type:   "automation",
				Status: "active",
				Trigger: &types.TriggerConfig{
					Type:  "event",
					Event: "feature.completed",
				},
			},
		},
		[]types.ResolvedTask{
			{ID: "task1", Title: "Cron task", Status: "pending", Schedule: "0 * * * *", ScheduleEnabled: boolPtr(true)},
			{ID: "task2", Title: "Run once", Status: "pending", RunOnceAt: "2099-01-15T10:00:00Z"},
			{ID: "task3", Title: "Regular task", Status: "pending"},
		},
	)

	view := al.View(100, 20)

	for _, want := range []string{"Review feature", "Cron task", "Run once", "event", "cron", "run_once"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q, got:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Regular task") {
		t.Fatalf("expected unscheduled task to be excluded, got:\n%s", view)
	}
}

func TestAutomationList_View_ShowsAutomationScope(t *testing.T) {
	al := NewAutomationList()
	al.SetEntryRows(
		[]types.BrainEntry{
			{ID: "global", Path: "global/automation/global.md", Title: "Global auto", Type: "automation", Status: "active"},
			{ID: "project", Path: "projects/brain-api/automation/project.md", Title: "Project auto", Type: "automation", Status: "active", ProjectID: "brain-api"},
		},
		nil,
		nil,
	)

	view := al.View(140, 20)
	for _, want := range []string{"global", "project"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected scope %q in view, got:\n%s", want, view)
		}
	}
}

func TestAutomationList_AutomationRowFromEntry_RendersTriggerDetails(t *testing.T) {
	tests := []struct {
		name    string
		trigger types.TriggerConfig
		want    string
	}{
		{
			name: "event trigger preserves event detail",
			trigger: types.TriggerConfig{
				Type:  "event",
				Event: "feature.completed",
			},
			want: "event:feature.completed",
		},
		{
			name: "cron trigger preserves schedule detail",
			trigger: types.TriggerConfig{
				Type:     "cron",
				Schedule: "0 * * * *",
			},
			want: "cron:0 * * * *",
		},
		{
			name: "webhook trigger preserves webhook detail",
			trigger: types.TriggerConfig{
				Type:    "webhook",
				Webhook: "/hooks/feature-review",
			},
			want: "webhook:/hooks/feature-review",
		},
		{
			name: "session trigger preserves explicit event detail",
			trigger: types.TriggerConfig{
				Type:  "session",
				Event: types.EventRunnerSessionDiscovered,
			},
			want: "session:" + types.EventRunnerSessionDiscovered,
		},
		{
			name: "session trigger renders default detail when event omitted",
			trigger: types.TriggerConfig{
				Type: "session",
			},
			want: "session:" + types.EventRunnerSessionDiscovered,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			al := NewAutomationList()
			al.SetEntriesAndTasks(
				[]types.BrainEntry{
					{
						ID:      "auto1",
						Title:   "Automation",
						Type:    "automation",
						Status:  "active",
						Trigger: &tt.trigger,
					},
				},
				nil,
			)

			view := al.View(120, 20)
			if !strings.Contains(view, tt.want) {
				t.Fatalf("expected view to contain %q, got:\n%s", tt.want, view)
			}
		})
	}
}

func TestAutomationList_View_ShowsEnabledAndDisabledStates(t *testing.T) {
	al := NewAutomationList()
	al.SetEntriesAndTasks(
		[]types.BrainEntry{
			{ID: "auto1", Title: "Active automation", Type: "automation", Status: "active"},
			{ID: "auto2", Title: "Archived automation", Type: "automation", Status: "archived"},
		},
		[]types.ResolvedTask{
			{ID: "task1", Title: "Disabled task", Status: "pending", Schedule: "0 * * * *", ScheduleEnabled: boolPtr(false)},
		},
	)

	view := al.View(100, 20)

	if !strings.Contains(view, "[enabled]") {
		t.Fatalf("expected enabled state in view, got:\n%s", view)
	}
	if count := strings.Count(view, "[disabled]"); count != 2 {
		t.Fatalf("expected two disabled states, got %d in:\n%s", count, view)
	}
}

func TestAutomationList_View_ShowsGeneratedTaskLifecycle(t *testing.T) {
	al := NewAutomationList()
	al.SetEntryRows(
		[]types.BrainEntry{{ID: "auto1", Title: "Check site", Type: "automation", Status: "active"}},
		nil,
		[]types.BrainEntry{
			{ID: "task1", Type: "task", Status: "pending", GeneratedBy: "automation:auto1"},
			{ID: "task2", Type: "task", Status: "in_progress", GeneratedBy: "automation:auto1"},
			{ID: "task3", Type: "task", Status: "completed", GeneratedBy: "automation:auto1"},
		},
	)

	view := al.View(140, 20)
	for _, want := range []string{"run:", "1 queued", "1 running", "1 done"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q, got:\n%s", want, view)
		}
	}
}

func TestAutomationList_View_GoalAutomationShowsBadgeAndProgress(t *testing.T) {
	al := NewAutomationList()
	al.SetEntryRows(
		[]types.BrainEntry{
			{
				ID:          "goal1",
				Title:       "Ship feature",
				Type:        "automation",
				Status:      "active",
				GeneratedBy: types.GoalGeneratedBy,
				FeatureID:   "ship-it",
			},
			{ID: "auto1", Title: "Regular automation", Type: "automation", Status: "active"},
		},
		nil,
		[]types.BrainEntry{
			{ID: "t1", Type: "task", Status: "completed", FeatureID: "ship-it"},
			{ID: "t2", Type: "task", Status: "validated", FeatureID: "ship-it"},
			{ID: "t3", Type: "task", Status: "pending", FeatureID: "ship-it"},
			{ID: "t4", Type: "task", Status: "in_progress", FeatureID: "ship-it"},
		},
	)

	view := al.View(160, 20)
	if !strings.Contains(view, "[goal]") {
		t.Fatalf("expected goal badge in view, got:\n%s", view)
	}
	if !strings.Contains(view, "2/4") {
		t.Fatalf("expected progress counter 2/4 in view, got:\n%s", view)
	}
	// Regular (non-goal) automation rows must not gain a goal badge.
	if strings.Count(view, "[goal]") != 1 {
		t.Fatalf("expected exactly one goal badge, got %d in:\n%s", strings.Count(view, "[goal]"), view)
	}
}

func TestAutomationList_GoalRowWithoutLinkedTasksShowsZeroProgress(t *testing.T) {
	al := NewAutomationList()
	al.SetEntryRows(
		[]types.BrainEntry{
			{
				ID:          "goal1",
				Title:       "Empty goal",
				Type:        "automation",
				Status:      "active",
				GeneratedBy: types.GoalGeneratedBy,
				FeatureID:   "empty",
			},
		},
		nil,
		nil,
	)

	view := al.View(160, 20)
	if !strings.Contains(view, "[goal]") {
		t.Fatalf("expected goal badge in view, got:\n%s", view)
	}
	if !strings.Contains(view, "0/0") {
		t.Fatalf("expected 0/0 progress for goal without linked tasks, got:\n%s", view)
	}
}

func TestAutomationRowFromEntry_SetsIsGoalForGoalAutomations(t *testing.T) {
	goalRow := AutomationRowFromEntry(types.BrainEntry{
		ID:          "goal1",
		Type:        "automation",
		Status:      "active",
		GeneratedBy: types.GoalGeneratedBy,
	})
	if !goalRow.IsGoal {
		t.Fatal("expected IsGoal=true for generated_by=brain-goal entry")
	}

	plainRow := AutomationRowFromEntry(types.BrainEntry{
		ID:     "auto1",
		Type:   "automation",
		Status: "active",
	})
	if plainRow.IsGoal {
		t.Fatal("expected IsGoal=false for non-goal automation entry")
	}
}

func TestRenderGoalProgress_FillsBarProportionally(t *testing.T) {
	tests := []struct {
		name        string
		done, total int
		wantCounter string
	}{
		{name: "empty", done: 0, total: 0, wantCounter: "0/0"},
		{name: "partial", done: 1, total: 4, wantCounter: "1/4"},
		{name: "complete", done: 3, total: 3, wantCounter: "3/3"},
		{name: "clamps overflow", done: 5, total: 3, wantCounter: "3/3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderGoalProgress(tt.done, tt.total)
			if !strings.Contains(out, tt.wantCounter) {
				t.Fatalf("expected counter %q in %q", tt.wantCounter, out)
			}
		})
	}

	// A complete goal should render a fully-filled bar (no empty cells).
	complete := renderGoalProgress(4, 4)
	if strings.Contains(complete, "░") {
		t.Fatalf("expected no empty cells for complete goal, got %q", complete)
	}
	// A partial goal should contain at least one empty cell.
	partial := renderGoalProgress(1, 4)
	if !strings.Contains(partial, "░") {
		t.Fatalf("expected empty cells for partial goal, got %q", partial)
	}
}

func TestAutomationList_SetEntryRowsSortsRowsDeterministically(t *testing.T) {
	al := NewAutomationList()
	al.SetEntryRows(
		[]types.BrainEntry{
			{ID: "auto-z", Title: "Zulu", Type: "automation", Status: "active", Trigger: &types.TriggerConfig{Type: "event", Event: "task.completed"}},
			{ID: "auto-a2", Title: "Alpha", Type: "automation", Status: "active", Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 1 * * *"}},
			{ID: "auto-a1", Title: "Alpha", Type: "automation", Status: "active", Trigger: &types.TriggerConfig{Type: "event", Event: "task.status_changed"}},
		},
		[]types.BrainEntry{
			{ID: "task-z", Title: "Zulu scheduled", Type: "task", Status: "pending", Schedule: "0 2 * * *"},
			{ID: "task-a", Title: "Alpha scheduled", Type: "task", Status: "pending", Schedule: "0 3 * * *"},
		},
		nil,
	)

	var got []string
	for _, row := range al.rows {
		got = append(got, row.ID)
	}
	want := []string{"auto-a2", "auto-a1", "auto-z", "task-a", "task-z"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("row order = %v, want %v", got, want)
	}
}

func TestAutomationList_EmptyEntriesShowsEmptyState(t *testing.T) {
	list := NewAutomationList()
	list.SetEntryRows(nil, nil, nil)

	if row := list.SelectedRow(); row != nil {
		t.Fatalf("expected no selected row, got %#v", row)
	}
	view := list.View(100, 10)
	if !strings.Contains(view, "No automations found") || strings.Contains(view, "Dream") {
		t.Fatalf("expected empty automation state without synthetic Dream row, got:\n%s", view)
	}
}

func TestAutomationList_NavigationAndSelectedRow(t *testing.T) {
	al := NewAutomationList()
	al.SetRows([]AutomationListRow{
		{ID: "r1", Title: "First", Enabled: true, TriggerKind: "event"},
		{ID: "r2", Title: "Second", Enabled: true, TriggerKind: "cron"},
		{ID: "r3", Title: "Third", Enabled: true, TriggerKind: "webhook"},
	})

	if selected := al.SelectedRow(); selected == nil || selected.ID != "r1" {
		t.Fatalf("expected first row selected initially, got %+v", selected)
	}

	al.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if selected := al.SelectedRow(); selected == nil || selected.ID != "r2" {
		t.Fatalf("expected second row after j, got %+v", selected)
	}

	al.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if selected := al.SelectedRow(); selected == nil || selected.ID != "r3" {
		t.Fatalf("expected last row after G, got %+v", selected)
	}

	al.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if selected := al.SelectedRow(); selected == nil || selected.ID != "r1" {
		t.Fatalf("expected first row after g, got %+v", selected)
	}

	al.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if selected := al.SelectedRow(); selected == nil || selected.ID != "r1" {
		t.Fatalf("expected selection to stay at first row after k, got %+v", selected)
	}
}

func TestAutomationList_SelectVisibleRowUsesScrollOffset(t *testing.T) {
	al := NewAutomationList()
	al.SetRows([]AutomationListRow{
		{ID: "r1", Title: "First"},
		{ID: "r2", Title: "Second"},
		{ID: "r3", Title: "Third"},
		{ID: "r4", Title: "Fourth"},
	})
	al.ScrollDown(2)
	al.View(80, 3)

	if ok := al.SelectVisibleRow(1); !ok {
		t.Fatal("expected visible row to be selectable")
	}
	if selected := al.SelectedRow(); selected == nil || selected.ID != "r3" {
		t.Fatalf("expected visible row selection to account for scroll offset, got %+v", selected)
	}
}

func TestAutomationList_ScrollMovesMultipleRows(t *testing.T) {
	al := NewAutomationList()
	al.SetRows([]AutomationListRow{
		{ID: "r1", Title: "First"},
		{ID: "r2", Title: "Second"},
		{ID: "r3", Title: "Third"},
		{ID: "r4", Title: "Fourth"},
	})

	al.ScrollDown(3)
	if selected := al.SelectedRow(); selected == nil || selected.ID != "r4" {
		t.Fatalf("expected scroll down to move multiple rows, got %+v", selected)
	}
	al.ScrollUp(2)
	if selected := al.SelectedRow(); selected == nil || selected.ID != "r2" {
		t.Fatalf("expected scroll up to move multiple rows, got %+v", selected)
	}
}

func TestAutomationList_States(t *testing.T) {
	al := NewAutomationList()

	if view := al.View(80, 20); !strings.Contains(view, "No automations found") {
		t.Fatalf("expected empty state, got:\n%s", view)
	}

	al.SetLoading(true)
	if view := al.View(80, 20); !strings.Contains(view, "Loading automations") {
		t.Fatalf("expected loading state, got:\n%s", view)
	}

	al.SetError("fetch failed")
	if view := al.View(80, 20); !strings.Contains(view, "fetch failed") {
		t.Fatalf("expected error state, got:\n%s", view)
	}
}

func TestAutomationList_SetRows_PreservesSelection(t *testing.T) {
	al := NewAutomationList()
	al.SetRows([]AutomationListRow{
		{ID: "r1", Title: "First", Enabled: true, TriggerKind: "event"},
		{ID: "r2", Title: "Second", Enabled: true, TriggerKind: "cron"},
	})
	al.MoveDown()

	al.SetRows([]AutomationListRow{
		{ID: "r2", Title: "Second updated", Enabled: true, TriggerKind: "cron"},
		{ID: "r3", Title: "Third", Enabled: true, TriggerKind: "webhook"},
	})

	if selected := al.SelectedRow(); selected == nil || selected.ID != "r2" || selected.Title != "Second updated" {
		t.Fatalf("expected selection to be preserved on r2, got %+v", selected)
	}
}

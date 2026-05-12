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
				Trigger: &types.AutomationTrigger{
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

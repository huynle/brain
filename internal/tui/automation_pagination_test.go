package tui

import (
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestAutomationList_PaginationInitiallyShows10Tasks(t *testing.T) {
	al := NewAutomationList()
	
	// Create automation with 25 generated tasks
	// Sorted newest-first by Modified, so task24 will be first
	tasks := make([]types.BrainEntry, 25)
	for i := 0; i < 25; i++ {
		tasks[i] = types.BrainEntry{
			ID:          taskID(i),
			Type:        "task",
			Status:      "completed",
			GeneratedBy: "automation:auto1",
			Title:       taskTitle(i),
			Modified:    modifiedTime(i), // Earlier times for lower numbers
		}
	}
	
	al.SetEntryRows(
		[]types.BrainEntry{
			{ID: "auto1", Title: "Test automation", Type: "automation", Status: "active"},
		},
		nil,
		tasks,
	)
	
	// Expand the automation
	al.ToggleExpandedSelected()
	view := stripANSI(al.View(140, 50))
	
	// Should show first 10 tasks (newest first: task24 down to task15)
	for i := 24; i >= 15; i-- {
		if !strings.Contains(view, taskID(i)) {
			t.Errorf("expected to see task %s in view", taskID(i))
		}
	}
	
	// Should NOT show task14 and below yet
	if strings.Contains(view, taskID(14)) {
		t.Errorf("expected task %s to be hidden initially", taskID(14))
	}
	
	// Should show "show more" pseudo-row
	if !strings.Contains(view, "Show 10 more") {
		t.Errorf("expected 'Show 10 more' row, got:\n%s", view)
	}
	if !strings.Contains(view, "(10/25 shown)") {
		t.Errorf("expected '(10/25 shown)' counter, got:\n%s", view)
	}
}

func TestAutomationList_PaginationExpandsOnEnter(t *testing.T) {
	al := NewAutomationList()
	
	// Create automation with 25 generated tasks (newest first after sort)
	tasks := make([]types.BrainEntry, 25)
	for i := 0; i < 25; i++ {
		tasks[i] = types.BrainEntry{
			ID:          taskID(i),
			Type:        "task",
			Status:      "completed",
			GeneratedBy: "automation:auto1",
			Title:       taskTitle(i),
			Modified:    modifiedTime(i),
		}
	}
	
	al.SetEntryRows(
		[]types.BrainEntry{
			{ID: "auto1", Title: "Test automation", Type: "automation", Status: "active"},
		},
		nil,
		tasks,
	)
	
	// Expand the automation
	al.ToggleExpandedSelected()
	
	// Navigate to "show more" pseudo-row
	for i := 0; i < 11; i++ { // Move through 10 tasks + 1 to reach show-more
		al.MoveDown()
	}
	
	// Verify we're on the "show more" row
	if al.selectedRunTaskID != "__show_more__:auto1" {
		t.Fatalf("expected to be on show-more row, got selectedRunTaskID=%q", al.selectedRunTaskID)
	}
	
	// Press Enter to expand next page
	al.ToggleExpandedSelected()
	
	view := stripANSI(al.View(140, 50))
	
	// Should now show first 20 tasks (task24 down to task05)
	for i := 24; i >= 5; i-- {
		if !strings.Contains(view, taskID(i)) {
			t.Errorf("expected to see task %s after expansion", taskID(i))
		}
	}
	
	// Should still show "show more" with updated counter
	if !strings.Contains(view, "Show 5 more") {
		t.Errorf("expected 'Show 5 more' row, got:\n%s", view)
	}
	if !strings.Contains(view, "(20/25 shown)") {
		t.Errorf("expected '(20/25 shown)' counter, got:\n%s", view)
	}
}

func TestAutomationList_PaginationExpandsToShowAll(t *testing.T) {
	al := NewAutomationList()
	
	// Create automation with 25 generated tasks (newest first)
	tasks := make([]types.BrainEntry, 25)
	for i := 0; i < 25; i++ {
		tasks[i] = types.BrainEntry{
			ID:          taskID(i),
			Type:        "task",
			Status:      "completed",
			GeneratedBy: "automation:auto1",
			Title:       taskTitle(i),
			Modified:    modifiedTime(i),
		}
	}
	
	al.SetEntryRows(
		[]types.BrainEntry{
			{ID: "auto1", Title: "Test automation", Type: "automation", Status: "active"},
		},
		nil,
		tasks,
	)
	
	// Expand the automation
	al.ToggleExpandedSelected()
	
	// Navigate to "show more" and expand twice
	for i := 0; i < 11; i++ {
		al.MoveDown()
	}
	al.ToggleExpandedSelected() // First expansion: 10 -> 20
	
	// Navigate to "show more" again
	for i := 0; i < 11; i++ {
		al.MoveDown()
	}
	al.ToggleExpandedSelected() // Second expansion: 20 -> 25
	
	view := stripANSI(al.View(140, 50))
	
	// Should now show all 25 tasks
	for i := 0; i < 25; i++ {
		if !strings.Contains(view, taskID(i)) {
			t.Errorf("expected to see task %s after full expansion", taskID(i))
		}
	}
	
	// Should NOT show "show more" anymore
	if strings.Contains(view, "Show") && strings.Contains(view, "more") {
		t.Errorf("expected no 'Show more' row when all tasks visible, got:\n%s", view)
	}
}

func TestAutomationList_PaginationSelectedRunTaskReturnsFalseForShowMore(t *testing.T) {
	al := NewAutomationList()
	
	tasks := make([]types.BrainEntry, 15)
	for i := 0; i < 15; i++ {
		tasks[i] = types.BrainEntry{
			ID:          taskID(i),
			Type:        "task",
			Status:      "completed",
			GeneratedBy: "automation:auto1",
			Modified:    modifiedTime(i),
		}
	}
	
	al.SetEntryRows(
		[]types.BrainEntry{
			{ID: "auto1", Title: "Test automation", Type: "automation", Status: "active"},
		},
		nil,
		tasks,
	)
	
	// Expand and navigate to "show more"
	al.ToggleExpandedSelected()
	for i := 0; i < 11; i++ {
		al.MoveDown()
	}
	
	// Verify we're on show-more
	if al.selectedRunTaskID != "__show_more__:auto1" {
		t.Fatalf("expected to be on show-more row")
	}
	
	// SelectedRunTask should return false for the pseudo-row
	if _, ok := al.SelectedRunTask(); ok {
		t.Errorf("expected SelectedRunTask to return false for show-more pseudo-row")
	}
}

func TestAutomationList_PaginationNoShowMoreForFewerThan10Tasks(t *testing.T) {
	al := NewAutomationList()
	
	// Create automation with only 5 tasks
	tasks := make([]types.BrainEntry, 5)
	for i := 0; i < 5; i++ {
		tasks[i] = types.BrainEntry{
			ID:          taskID(i),
			Type:        "task",
			Status:      "completed",
			GeneratedBy: "automation:auto1",
			Modified:    modifiedTime(i),
		}
	}
	
	al.SetEntryRows(
		[]types.BrainEntry{
			{ID: "auto1", Title: "Test automation", Type: "automation", Status: "active"},
		},
		nil,
		tasks,
	)
	
	// Expand the automation
	al.ToggleExpandedSelected()
	view := stripANSI(al.View(140, 50))
	
	// Should show all 5 tasks
	for i := 0; i < 5; i++ {
		if !strings.Contains(view, taskID(i)) {
			t.Errorf("expected to see task %s", taskID(i))
		}
	}
	
	// Should NOT show "show more" row
	if strings.Contains(view, "Show") && strings.Contains(view, "more") {
		t.Errorf("expected no 'Show more' row for fewer than page size tasks, got:\n%s", view)
	}
}

func TestAutomationList_PaginationNavigationThroughShowMore(t *testing.T) {
	al := NewAutomationList()
	
	tasks := make([]types.BrainEntry, 15)
	for i := 0; i < 15; i++ {
		tasks[i] = types.BrainEntry{
			ID:          taskID(i),
			Type:        "task",
			Status:      "completed",
			GeneratedBy: "automation:auto1",
			Modified:    modifiedTime(i),
		}
	}
	
	al.SetEntryRows(
		[]types.BrainEntry{
			{ID: "auto1", Title: "Test automation", Type: "automation", Status: "active"},
		},
		nil,
		tasks,
	)
	
	// Expand - this automatically selects the first child task (task14)
	al.ToggleExpandedSelected()
	if al.selectedRunTaskID != "task14" {
		t.Fatalf("expected first child task14 to be selected after expand, got %s", al.selectedRunTaskID)
	}
	
	// Move down through remaining visible tasks (task14 is already selected, so 9 more moves)
	expectedTasks := []string{"task13", "task12", "task11", "task10", "task09", "task08", "task07", "task06", "task05"}
	for i := 0; i < 9; i++ {
		al.MoveDown()
		if al.selectedRunTaskID != expectedTasks[i] {
			t.Errorf("after %d MoveDown from task14, expected %s, got %s", i+1, expectedTasks[i], al.selectedRunTaskID)
		}
	}
	
	// One more down should land on "show more"
	al.MoveDown()
	if al.selectedRunTaskID != "__show_more__:auto1" {
		t.Errorf("expected to be on show-more, got %s", al.selectedRunTaskID)
	}
	
	// Move up should go back to last visible task
	al.MoveUp()
	if al.selectedRunTaskID != "task05" {
		t.Errorf("expected to move back to task05, got %s", al.selectedRunTaskID)
	}
}

func TestAutomationList_PaginationCollapseResetsLimit(t *testing.T) {
	al := NewAutomationList()
	
	tasks := make([]types.BrainEntry, 25)
	for i := 0; i < 25; i++ {
		tasks[i] = types.BrainEntry{
			ID:          taskID(i),
			Type:        "task",
			Status:      "completed",
			GeneratedBy: "automation:auto1",
			Modified:    modifiedTime(i),
		}
	}
	
	al.SetEntryRows(
		[]types.BrainEntry{
			{ID: "auto1", Title: "Test automation", Type: "automation", Status: "active"},
		},
		nil,
		tasks,
	)
	
	// Expand and increase limit to 20
	al.ToggleExpandedSelected()
	for i := 0; i < 11; i++ {
		al.MoveDown()
	}
	al.ToggleExpandedSelected() // Expand to 20
	
	// Collapse
	al.MoveUp()
	for i := 0; i < 11; i++ {
		al.MoveUp()
	}
	al.ToggleExpandedSelected()
	
	// Re-expand should reset to initial page size (10)
	al.ToggleExpandedSelected()
	view := stripANSI(al.View(140, 50))
	
	if !strings.Contains(view, "(10/25 shown)") {
		t.Errorf("expected limit reset to 10 after collapse/re-expand, got:\n%s", view)
	}
}

// Helper functions for test data generation
func taskID(i int) string {
	return "task" + pad(i, 2)
}

func taskTitle(i int) string {
	return "Generated task " + pad(i, 2)
}

func modifiedTime(i int) string {
	// Return ISO8601 timestamps where lower index = earlier time
	// This ensures task24 (i=24) is newest, task00 (i=0) is oldest
	return "2026-06-" + pad(i+1, 2) + "T10:00:00Z"
}

func pad(n, width int) string {
	s := ""
	for n > 0 {
		s = string(rune('0'+(n%10))) + s
		n /= 10
	}
	for len(s) < width {
		s = "0" + s
	}
	return s
}

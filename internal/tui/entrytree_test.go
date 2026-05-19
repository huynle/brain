package tui

import (
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestEntryTree_RendersHierarchyAndEmbeddingIndicators(t *testing.T) {
	tree := NewEntryTree()
	tree.SetEntries([]types.BrainEntry{
		{ID: "task1", Path: "projects/brain-api/task/task1.md", Title: "Task One", Type: "task", EmbeddingStatus: "current"},
		{ID: "plan1", Path: "projects/brain-api/plan/plan1.md", Title: "Plan One", Type: "plan", EmbeddingStatus: "missing"},
		{ID: "note1", Path: "projects/brain-api/note/note1.md", Title: "Note One", Type: "note", EmbeddingStatus: "stale"},
	})

	view := tree.View(100, 20)
	for _, want := range []string{"Brain Entries (3)", "task/", "Task One", "plan/", "Plan One", "note/", "Note One"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q, got:\n%s", want, view)
		}
	}
	for _, want := range []string{"embed:missing", "embed:stale"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected embedding indicator %q, got:\n%s", want, view)
		}
	}
	if strings.Contains(view, "embed:current") {
		t.Fatalf("current embeddings should not add warning noise, got:\n%s", view)
	}
}

func TestEntryTree_NavigationAndMouseSelection(t *testing.T) {
	tree := NewEntryTree()
	tree.SetEntries([]types.BrainEntry{
		{ID: "a", Path: "projects/brain-api/task/a.md", Title: "A", Type: "task"},
		{ID: "b", Path: "projects/brain-api/task/b.md", Title: "B", Type: "task"},
	})

	tree.MoveDown()
	if got := tree.SelectedEntry(); got == nil || got.ID != "b" {
		t.Fatalf("expected selected entry b after MoveDown, got %#v", got)
	}
	tree.MoveUp()
	if got := tree.SelectedEntry(); got == nil || got.ID != "a" {
		t.Fatalf("expected selected entry a after MoveUp, got %#v", got)
	}
	if !tree.SelectVisibleLine(3) {
		t.Fatal("expected mouse selection on second entry line to succeed")
	}
	if got := tree.SelectedEntry(); got == nil || got.ID != "b" {
		t.Fatalf("expected selected entry b after mouse select, got %#v", got)
	}
}

func TestEntryTree_KeepsSelectionVisibleWhenScrolling(t *testing.T) {
	tree := NewEntryTree()
	entries := make([]types.BrainEntry, 0, 8)
	for i := 0; i < 8; i++ {
		id := string(rune('a' + i))
		entries = append(entries, types.BrainEntry{ID: id, Path: "projects/brain-api/task/" + id + ".md", Title: strings.ToUpper(id), Type: "task"})
	}
	tree.SetEntries(entries)

	for i := 0; i < 7; i++ {
		tree.MoveDown()
	}

	view := tree.View(80, 5)
	if !strings.Contains(view, "H [task]") {
		t.Fatalf("expected selected final entry to be visible after scrolling, got:\n%s", view)
	}
	if strings.Contains(view, "A [task]") {
		t.Fatalf("expected early entries to scroll out of view, got:\n%s", view)
	}
}

func TestEntryTree_SetEntriesInOrderPreservesSearchRelevance(t *testing.T) {
	tree := NewEntryTree()
	tree.SetSearchResults([]types.BrainEntry{
		{ID: "semantic", Path: "projects/test/idea/semantic.md", Title: "Semantic Top", Type: "idea"},
		{ID: "automation", Path: "projects/test/automation/automation.md", Title: "Automation Later", Type: "automation"},
	})

	selected := tree.SelectedEntry()
	if selected == nil || selected.ID != "semantic" {
		t.Fatalf("expected first semantic result to remain selected, got %#v", selected)
	}
	view := tree.View(80, 8)
	semanticIndex := strings.Index(view, "Semantic Top")
	automationIndex := strings.Index(view, "Automation Later")
	if semanticIndex < 0 || automationIndex < 0 || semanticIndex > automationIndex {
		t.Fatalf("expected semantic result before automation result, got:\n%s", view)
	}
	if !strings.Contains(view, "Brain Search Results (2)") {
		t.Fatalf("expected search-specific title, got:\n%s", view)
	}
}

func TestEntryTree_ToggleCollapseHidesAndShowsGroupEntries(t *testing.T) {
	tree := NewEntryTree()
	tree.SetEntries([]types.BrainEntry{
		{ID: "report1", Path: "projects/brain-api/report/report1.md", Title: "Report One", Type: "report"},
		{ID: "task1", Path: "projects/brain-api/task/task1.md", Title: "Task One", Type: "task"},
	})

	if !tree.SelectVisibleLine(1) {
		t.Fatal("expected selecting report group header to succeed")
	}
	if !tree.IsOnGroupHeader() {
		t.Fatal("expected top row to be a group header")
	}
	if !tree.ToggleCollapse() {
		t.Fatal("expected ToggleCollapse on group header to succeed")
	}
	view := tree.View(100, 20)
	if !strings.Contains(view, "▸ report/") {
		t.Fatalf("expected collapsed report group marker, got:\n%s", view)
	}
	if strings.Contains(view, "Report One") {
		t.Fatalf("expected collapsed report entry to be hidden, got:\n%s", view)
	}
	if !strings.Contains(view, "Task One") {
		t.Fatalf("expected other groups to remain visible, got:\n%s", view)
	}

	if !tree.ToggleCollapse() {
		t.Fatal("expected ToggleCollapse to expand group")
	}
	view = tree.View(100, 20)
	if !strings.Contains(view, "▾ report/") || !strings.Contains(view, "Report One") {
		t.Fatalf("expected expanded report group and entry, got:\n%s", view)
	}
}

func TestEntryTree_SelectVisibleGroupHeader(t *testing.T) {
	tree := NewEntryTree()
	tree.SetEntries([]types.BrainEntry{{ID: "task1", Path: "projects/brain-api/task/task1.md", Title: "Task One", Type: "task"}})

	if !tree.SelectVisibleLine(1) {
		t.Fatal("expected selecting visible group header to succeed")
	}
	if !tree.IsOnGroupHeader() {
		t.Fatal("expected selected row to be group header")
	}
}

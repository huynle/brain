package service

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"

	_ "github.com/glebarez/go-sqlite"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestBrainService creates a BrainServiceImpl with in-memory DB and temp brainDir.
func newTestBrainService(t *testing.T) (*BrainServiceImpl, *storage.StorageLayer, string) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}

	store, err := storage.NewWithDB(db)
	if err != nil {
		t.Fatalf("NewWithDB failed: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	brainDir := t.TempDir()
	cfg := &config.Config{BrainDir: brainDir}
	idx := indexer.NewIndexer(brainDir, store)

	svc := NewBrainService(cfg, store, idx)
	return svc, store, brainDir
}

// strPtr returns a pointer to a string.
func strPtr(s string) *string {
	return &s
}

// boolPtr returns a pointer to a bool.
func boolPtr(b bool) *bool {
	return &b
}

// intPtr returns a pointer to an int.
func intPtr(i int) *int {
	return &i
}

// freezeTime sets TimeNowUTC to return a fixed time and restores it on cleanup.
func freezeTime(t *testing.T, fixed time.Time) {
	t.Helper()
	original := types.TimeNowUTC
	types.TimeNowUTC = func() time.Time { return fixed }
	t.Cleanup(func() { types.TimeNowUTC = original })
}

// =============================================================================
// Save tests
// =============================================================================

func TestSave_BasicEntry(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "My Test Plan",
		Content: "This is the plan content.",
		Tags:    []string{"go", "test"},
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify response fields
	if resp.ID == "" {
		t.Error("expected non-empty ID")
	}
	if len(resp.ID) != 8 {
		t.Errorf("expected 8-char ID, got %d chars: %q", len(resp.ID), resp.ID)
	}
	if resp.Type != "plan" {
		t.Errorf("expected type 'plan', got %q", resp.Type)
	}
	if resp.Title != "My Test Plan" {
		t.Errorf("expected title 'My Test Plan', got %q", resp.Title)
	}
	if resp.Status != "active" {
		t.Errorf("expected status 'active', got %q", resp.Status)
	}
	if !strings.Contains(resp.Path, "projects/default/plan/") {
		t.Errorf("expected path to contain 'projects/default/plan/', got %q", resp.Path)
	}
	if !strings.HasSuffix(resp.Path, ".md") {
		t.Errorf("expected path to end with .md, got %q", resp.Path)
	}
	if resp.Link == "" {
		t.Error("expected non-empty link")
	}

	// Verify file exists on disk
	absPath := filepath.Join(brainDir, filepath.FromSlash(resp.Path))
	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("file not found on disk: %v", err)
	}
	fileStr := string(content)
	if !strings.Contains(fileStr, "title: My Test Plan") {
		t.Error("file should contain title in frontmatter")
	}
	if !strings.Contains(fileStr, "type: plan") {
		t.Error("file should contain type in frontmatter")
	}
	if !strings.Contains(fileStr, "This is the plan content.") {
		t.Error("file should contain body content")
	}
}

func TestSave_GlobalEntry(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "pattern",
		Title:   "Global Pattern",
		Content: "A reusable pattern.",
		Global:  boolPtr(true),
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if !strings.HasPrefix(resp.Path, "global/pattern/") {
		t.Errorf("expected global path, got %q", resp.Path)
	}
}

func TestSave_CustomProject(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Project Task",
		Project: "my-project",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if !strings.Contains(resp.Path, "projects/my-project/task/") {
		t.Errorf("expected path with project 'my-project', got %q", resp.Path)
	}
}

func TestSave_CustomStatus(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:   "task",
		Title:  "Pending Task",
		Status: "pending",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if resp.Status != "pending" {
		t.Errorf("expected status 'pending', got %q", resp.Status)
	}
}

func TestSave_MissingType(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := svc.Save(ctx, types.CreateEntryRequest{
		Title: "No Type",
	})
	if err == nil {
		t.Fatal("expected error for missing type")
	}
}

func TestSave_MissingTitle(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := svc.Save(ctx, types.CreateEntryRequest{
		Type: "plan",
	})
	if err == nil {
		t.Fatal("expected error for missing title")
	}
}

func TestSave_WithDependsOn(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:      "task",
		Title:     "Dependent Task",
		DependsOn: []string{"abc12def", "xyz98765"},
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify depends_on in file
	absPath := filepath.Join(brainDir, filepath.FromSlash(resp.Path))
	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	fileStr := string(content)
	if !strings.Contains(fileStr, "depends_on:") {
		t.Error("file should contain depends_on")
	}
	if !strings.Contains(fileStr, "abc12def") {
		t.Error("file should contain first dependency")
	}
}

func TestSave_IndexedInDB(t *testing.T) {
	svc, store, _ := newTestBrainService(t)
	ctx := context.Background()

	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Indexed Plan",
		Content: "Content for indexing.",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify it's in the database
	row, err := store.GetNoteByShortID(ctx, resp.ID)
	if err != nil {
		t.Fatalf("GetNoteByShortID failed: %v", err)
	}
	if row == nil {
		t.Fatal("expected note in DB after save")
	}
	if row.Title != "Indexed Plan" {
		t.Errorf("expected title 'Indexed Plan', got %q", row.Title)
	}
}

func TestSave_WithScheduleFields(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:            "task",
		Title:           "Scheduled Task",
		Schedule:        "0 */6 * * *",
		ScheduleEnabled: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	absPath := filepath.Join(brainDir, filepath.FromSlash(resp.Path))
	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	fileStr := string(content)
	if !strings.Contains(fileStr, "schedule:") {
		t.Error("file should contain schedule")
	}
	if !strings.Contains(fileStr, "schedule_enabled: true") {
		t.Error("file should contain schedule_enabled: true")
	}
}

// =============================================================================
// Recall tests
// =============================================================================

func TestRecall_ByShortID(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Recall Test",
		Content: "Recall content.",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	entry, err := svc.Recall(ctx, saved.ID)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}

	if entry.ID != saved.ID {
		t.Errorf("expected ID %q, got %q", saved.ID, entry.ID)
	}
	if entry.Title != "Recall Test" {
		t.Errorf("expected title 'Recall Test', got %q", entry.Title)
	}
	if entry.Type != "plan" {
		t.Errorf("expected type 'plan', got %q", entry.Type)
	}
}

func TestRecall_ByPath(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Path Recall",
		Content: "Path content.",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	entry, err := svc.Recall(ctx, saved.Path)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}

	if entry.Path != saved.Path {
		t.Errorf("expected path %q, got %q", saved.Path, entry.Path)
	}
}

func TestRecall_ByTitle(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Unique Title For Recall",
		Content: "Title content.",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	entry, err := svc.Recall(ctx, "Unique Title For Recall")
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}

	if entry.Title != "Unique Title For Recall" {
		t.Errorf("expected title 'Unique Title For Recall', got %q", entry.Title)
	}
}

func TestRecall_NotFound(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := svc.Recall(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent entry")
	}
}

func TestRecall_RecordsAccess(t *testing.T) {
	svc, store, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:  "plan",
		Title: "Access Test",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Recall twice
	_, _ = svc.Recall(ctx, saved.ID)
	_, _ = svc.Recall(ctx, saved.ID)

	meta, err := store.GetAccessStats(ctx, saved.Path)
	if err != nil {
		t.Fatalf("GetAccessStats failed: %v", err)
	}
	if meta == nil {
		t.Fatal("expected access stats")
	}
	// At least 2 accesses (Recall calls RecordAccess)
	if meta.AccessCount < 2 {
		t.Errorf("expected at least 2 accesses, got %d", meta.AccessCount)
	}
}

func TestRecall_EmptyInput(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := svc.Recall(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

// =============================================================================
// Update tests
// =============================================================================

func TestUpdate_Title(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:  "plan",
		Title: "Original Title",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	updated, err := svc.Update(ctx, saved.ID, types.UpdateEntryRequest{
		Title: strPtr("Updated Title"),
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.Title != "Updated Title" {
		t.Errorf("expected title 'Updated Title', got %q", updated.Title)
	}
}

func TestUpdate_Status(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:  "task",
		Title: "Status Task",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	updated, err := svc.Update(ctx, saved.ID, types.UpdateEntryRequest{
		Status: strPtr("completed"),
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", updated.Status)
	}
}

func TestUpdate_Tags(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:  "plan",
		Title: "Tag Test",
		Tags:  []string{"old-tag"},
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	updated, err := svc.Update(ctx, saved.ID, types.UpdateEntryRequest{
		Tags: []string{"new-tag-1", "new-tag-2"},
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Tags should be replaced
	found := false
	for _, tag := range updated.Tags {
		if tag == "new-tag-1" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'new-tag-1' in tags, got %v", updated.Tags)
	}
}

func TestUpdate_Append(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Append Test",
		Content: "Original content.",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	_, err = svc.Update(ctx, saved.ID, types.UpdateEntryRequest{
		Append: strPtr("Appended text."),
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Read file to verify
	absPath := filepath.Join(brainDir, filepath.FromSlash(saved.Path))
	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	fileStr := string(content)
	if !strings.Contains(fileStr, "Original content.") {
		t.Error("file should still contain original content")
	}
	if !strings.Contains(fileStr, "Appended text.") {
		t.Error("file should contain appended text")
	}
}

func TestUpdate_Note(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	freezeTime(t, time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC))

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:  "task",
		Title: "Note Test",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	_, err = svc.Update(ctx, saved.ID, types.UpdateEntryRequest{
		Status: strPtr("completed"),
		Note:   strPtr("Task is done."),
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	absPath := filepath.Join(brainDir, filepath.FromSlash(saved.Path))
	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	fileStr := string(content)
	if !strings.Contains(fileStr, "Status changed to **completed**") {
		t.Error("file should contain status change note")
	}
	if !strings.Contains(fileStr, "Task is done.") {
		t.Error("file should contain note text")
	}
}

func TestUpdate_Content(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Content Replace",
		Content: "Old content.",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	_, err = svc.Update(ctx, saved.ID, types.UpdateEntryRequest{
		Content: strPtr("New content entirely."),
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	absPath := filepath.Join(brainDir, filepath.FromSlash(saved.Path))
	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	fileStr := string(content)
	if strings.Contains(fileStr, "Old content.") {
		t.Error("file should NOT contain old content")
	}
	if !strings.Contains(fileStr, "New content entirely.") {
		t.Error("file should contain new content")
	}
}

func TestUpdate_NotFound(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := svc.Update(ctx, "nonexistent", types.UpdateEntryRequest{
		Title: strPtr("Nope"),
	})
	if err == nil {
		t.Fatal("expected error for nonexistent entry")
	}
}

func TestUpdate_DependsOn(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:      "task",
		Title:     "Deps Update",
		DependsOn: []string{"old-dep"},
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	newDeps := []string{"new-dep-1", "new-dep-2"}
	_, err = svc.Update(ctx, saved.ID, types.UpdateEntryRequest{
		DependsOn: &newDeps,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	absPath := filepath.Join(brainDir, filepath.FromSlash(saved.Path))
	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	fileStr := string(content)
	if !strings.Contains(fileStr, "new-dep-1") {
		t.Error("file should contain new-dep-1")
	}
	if !strings.Contains(fileStr, "new-dep-2") {
		t.Error("file should contain new-dep-2")
	}
}

func TestUpdate_Priority(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:     "task",
		Title:    "Priority Update",
		Priority: "low",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	updated, err := svc.Update(ctx, saved.ID, types.UpdateEntryRequest{
		Priority: strPtr("high"),
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.Priority != "high" {
		t.Errorf("expected priority 'high', got %q", updated.Priority)
	}
}

// =============================================================================
// Delete tests
// =============================================================================

func TestDelete_ByShortID(t *testing.T) {
	svc, store, brainDir := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:  "plan",
		Title: "Delete Me",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists
	absPath := filepath.Join(brainDir, filepath.FromSlash(saved.Path))
	if _, err := os.Stat(absPath); err != nil {
		t.Fatalf("file should exist before delete: %v", err)
	}

	// Delete
	if err := svc.Delete(ctx, saved.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(absPath); !os.IsNotExist(err) {
		t.Error("file should not exist after delete")
	}

	// Verify removed from DB
	row, err := store.GetNoteByShortID(ctx, saved.ID)
	if err != nil {
		t.Fatalf("GetNoteByShortID failed: %v", err)
	}
	if row != nil {
		t.Error("note should not exist in DB after delete")
	}
}

func TestDelete_NotFound(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	err := svc.Delete(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent entry")
	}
}

// =============================================================================
// List tests
// =============================================================================

func TestList_AllEntries(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Create a few entries
	for i := 0; i < 3; i++ {
		_, err := svc.Save(ctx, types.CreateEntryRequest{
			Type:  "plan",
			Title: "Plan " + string(rune('A'+i)),
		})
		if err != nil {
			t.Fatalf("Save failed: %v", err)
		}
	}

	resp, err := svc.List(ctx, types.ListEntriesRequest{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if resp.Total != 3 {
		t.Errorf("expected 3 entries, got %d", resp.Total)
	}
	if len(resp.Entries) != 3 {
		t.Errorf("expected 3 entries in slice, got %d", len(resp.Entries))
	}
}

func TestList_FilterByType(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Plan"})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "task", Title: "Task"})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Plan 2"})

	resp, err := svc.List(ctx, types.ListEntriesRequest{Type: "plan"})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if resp.Total != 2 {
		t.Errorf("expected 2 plans, got %d", resp.Total)
	}
}

func TestList_FilterByStatus(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "task", Title: "Active", Status: "active"})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "task", Title: "Pending", Status: "pending"})

	resp, err := svc.List(ctx, types.ListEntriesRequest{Status: "pending"})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if resp.Total != 1 {
		t.Errorf("expected 1 pending, got %d", resp.Total)
	}
	if resp.Entries[0].Title != "Pending" {
		t.Errorf("expected 'Pending', got %q", resp.Entries[0].Title)
	}
}

func TestList_GlobalFilter(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "pattern", Title: "Global", Global: boolPtr(true)})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Project"})

	resp, err := svc.List(ctx, types.ListEntriesRequest{Global: boolPtr(true)})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if resp.Total != 1 {
		t.Errorf("expected 1 global entry, got %d", resp.Total)
	}
}

func TestList_WithLimit(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Plan " + string(rune('A'+i))})
	}

	resp, err := svc.List(ctx, types.ListEntriesRequest{Limit: 2})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(resp.Entries) != 2 {
		t.Errorf("expected 2 entries with limit, got %d", len(resp.Entries))
	}
	if resp.Limit != 2 {
		t.Errorf("expected limit 2 in response, got %d", resp.Limit)
	}
}

func TestList_EmptyResult(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	resp, err := svc.List(ctx, types.ListEntriesRequest{Type: "nonexistent"})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if resp.Total != 0 {
		t.Errorf("expected 0 entries, got %d", resp.Total)
	}
	if resp.Entries == nil {
		t.Error("entries should be non-nil empty slice")
	}
}

// =============================================================================
// Search tests
// =============================================================================

func TestSearch_BasicQuery(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Authentication Design", Content: "JWT tokens and OAuth flow."})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Database Schema", Content: "PostgreSQL tables."})

	resp, err := svc.Search(ctx, types.SearchRequest{Query: "authentication"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if resp.Total == 0 {
		t.Error("expected at least 1 search result")
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	resp, err := svc.Search(ctx, types.SearchRequest{Query: ""})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if resp.Total != 0 {
		t.Errorf("expected 0 results for empty query, got %d", resp.Total)
	}
}

func TestSearch_WithTypeFilter(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Plan About Go", Content: "Go programming."})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "task", Title: "Task About Go", Content: "Go task."})

	resp, err := svc.Search(ctx, types.SearchRequest{Query: "Go", Type: "plan"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	for _, r := range resp.Results {
		if r.Type != "plan" {
			t.Errorf("expected type 'plan', got %q", r.Type)
		}
	}
}

// =============================================================================
// Inject tests
// =============================================================================

func TestInject_BasicQuery(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Auth Plan", Content: "Authentication details."})

	resp, err := svc.Inject(ctx, types.InjectRequest{Query: "authentication"})
	if err != nil {
		t.Fatalf("Inject failed: %v", err)
	}

	if resp.Context == "" {
		t.Error("expected non-empty context")
	}
	if !strings.Contains(resp.Context, "## Auth Plan") {
		t.Error("context should contain entry title as heading")
	}
	if len(resp.Entries) == 0 {
		t.Error("expected at least 1 entry")
	}
}

func TestInject_EmptyQuery(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	resp, err := svc.Inject(ctx, types.InjectRequest{Query: ""})
	if err != nil {
		t.Fatalf("Inject failed: %v", err)
	}

	if resp.Context != "" {
		t.Errorf("expected empty context for empty query, got %q", resp.Context)
	}
}

func TestInject_WithMaxEntries(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Plan " + string(rune('A'+i)), Content: "Content about testing."})
	}

	resp, err := svc.Inject(ctx, types.InjectRequest{Query: "testing", MaxEntries: intPtr(2)})
	if err != nil {
		t.Fatalf("Inject failed: %v", err)
	}

	if len(resp.Entries) > 2 {
		t.Errorf("expected at most 2 entries, got %d", len(resp.Entries))
	}
}

// =============================================================================
// Move tests
// =============================================================================

func TestMove_BetweenProjects(t *testing.T) {
	svc, store, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Movable Plan",
		Project: "project-a",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	result, err := svc.Move(ctx, saved.ID, "project-b")
	if err != nil {
		t.Fatalf("Move failed: %v", err)
	}

	if !result.Success {
		t.Error("expected success=true")
	}
	if result.From != saved.Path {
		t.Errorf("expected from=%q, got %q", saved.Path, result.From)
	}
	if !strings.Contains(result.To, "project-b") {
		t.Errorf("expected new path to contain 'project-b', got %q", result.To)
	}

	// Verify old path is gone from DB
	oldRow, err := store.GetNoteByPath(ctx, saved.Path)
	if err != nil {
		t.Fatalf("GetNoteByPath failed: %v", err)
	}
	if oldRow != nil {
		t.Error("old path should not exist in DB")
	}

	// Verify new path exists in DB
	newRow, err := store.GetNoteByPath(ctx, result.To)
	if err != nil {
		t.Fatalf("GetNoteByPath failed: %v", err)
	}
	if newRow == nil {
		t.Error("new path should exist in DB")
	}
}

func TestMove_PreventInProgress(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:   "task",
		Title:  "In Progress Task",
		Status: "in_progress",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	_, err = svc.Move(ctx, saved.ID, "other-project")
	if err == nil {
		t.Fatal("expected error when moving in_progress task")
	}
	if !strings.Contains(err.Error(), "in_progress") {
		t.Errorf("error should mention in_progress, got: %v", err)
	}
}

func TestMove_EmptyTargetProject(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:  "plan",
		Title: "Move Target Empty",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	_, err = svc.Move(ctx, saved.ID, "")
	if err == nil {
		t.Fatal("expected error for empty target project")
	}
}

func TestMove_GlobalToProject(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:   "pattern",
		Title:  "Global Pattern",
		Global: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	result, err := svc.Move(ctx, saved.ID, "target-project")
	if err != nil {
		t.Fatalf("Move failed: %v", err)
	}

	if !strings.Contains(result.To, "projects/target-project/") {
		t.Errorf("expected new path in target-project, got %q", result.To)
	}
}

// =============================================================================
// computeMovedPath tests
// =============================================================================

func TestComputeMovedPath_ProjectToProject(t *testing.T) {
	result, err := computeMovedPath("projects/old/task/abc12def.md", "new-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "projects/new-project/task/abc12def.md" {
		t.Errorf("expected 'projects/new-project/task/abc12def.md', got %q", result)
	}
}

func TestComputeMovedPath_GlobalToProject(t *testing.T) {
	result, err := computeMovedPath("global/pattern/abc12def.md", "my-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "projects/my-project/pattern/abc12def.md" {
		t.Errorf("expected 'projects/my-project/pattern/abc12def.md', got %q", result)
	}
}

func TestComputeMovedPath_InvalidPath(t *testing.T) {
	_, err := computeMovedPath("invalid.md", "project")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

// =============================================================================
// Integration: Save + Recall + Update + Delete lifecycle
// =============================================================================

func TestLifecycle_SaveRecallUpdateDelete(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Save
	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Lifecycle Test",
		Content: "Initial content.",
		Tags:    []string{"lifecycle"},
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Recall
	entry, err := svc.Recall(ctx, saved.ID)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}
	if entry.Title != "Lifecycle Test" {
		t.Errorf("expected title 'Lifecycle Test', got %q", entry.Title)
	}

	// Update
	updated, err := svc.Update(ctx, saved.ID, types.UpdateEntryRequest{
		Title:  strPtr("Updated Lifecycle"),
		Status: strPtr("completed"),
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.Title != "Updated Lifecycle" {
		t.Errorf("expected updated title, got %q", updated.Title)
	}
	if updated.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", updated.Status)
	}

	// Delete
	if err := svc.Delete(ctx, saved.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify gone
	_, err = svc.Recall(ctx, saved.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

// Stub tests removed — methods now implemented in brain.go (Phase 4).

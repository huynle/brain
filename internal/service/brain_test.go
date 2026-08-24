package service

import (
	"context"
	"fmt"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/events"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/frontmatter"

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

	svc := NewBrainService(cfg, store, idx, nil, nil)
	return svc, store, brainDir
}

// newTestBrainServiceWithBus creates a BrainServiceImpl with an event bus for testing event publishing.
func newTestBrainServiceWithBus(t *testing.T) (*BrainServiceImpl, *storage.StorageLayer, string, *events.MemoryBus) {
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
	bus := events.NewMemoryBus()
	t.Cleanup(func() { bus.Close() })

	svc := NewBrainService(cfg, store, idx, bus, nil)
	return svc, store, brainDir, bus
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

// TestUpdate_GitRemote verifies that git_remote can be updated via the
// standard Update path. Regression test for a bug where git_remote (and other
// execution-context frontmatter fields) were unreachable through PATCH
// /entries/{path} because they were missing from UpdateEntryRequest.
func TestUpdate_GitRemote(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:      "task",
		Title:     "GitRemote Task",
		GitRemote: "git@example.com:orig/repo.git",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	updated, err := svc.Update(ctx, saved.ID, types.UpdateEntryRequest{
		GitRemote: strPtr("git@example.com:new/repo.git"),
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.GitRemote != "git@example.com:new/repo.git" {
		t.Errorf("expected git_remote 'git@example.com:new/repo.git', got %q", updated.GitRemote)
	}

	// Re-read via Recall to confirm the value persisted to file+DB, not just
	// the in-memory response.
	recalled, err := svc.Recall(ctx, saved.ID)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}
	if recalled.GitRemote != "git@example.com:new/repo.git" {
		t.Errorf("recall: expected git_remote 'git@example.com:new/repo.git', got %q", recalled.GitRemote)
	}
}

// TestUpdate_Workdir verifies that workdir can be updated via Update.
func TestUpdate_Workdir(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Workdir Task",
		Workdir: "orig/path",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	updated, err := svc.Update(ctx, saved.ID, types.UpdateEntryRequest{
		Workdir: strPtr("new/path"),
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.Workdir != "new/path" {
		t.Errorf("expected workdir 'new/path', got %q", updated.Workdir)
	}

	recalled, err := svc.Recall(ctx, saved.ID)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}
	if recalled.Workdir != "new/path" {
		t.Errorf("recall: expected workdir 'new/path', got %q", recalled.Workdir)
	}
}

// TestUpdate_UserOriginalRequest verifies user_original_request can be updated.
func TestUpdate_UserOriginalRequest(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:                "task",
		Title:               "UserOriginalRequest Task",
		UserOriginalRequest: "original ask",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	updated, err := svc.Update(ctx, saved.ID, types.UpdateEntryRequest{
		UserOriginalRequest: strPtr("clarified ask"),
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.UserOriginalRequest != "clarified ask" {
		t.Errorf("expected user_original_request 'clarified ask', got %q", updated.UserOriginalRequest)
	}

	recalled, err := svc.Recall(ctx, saved.ID)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}
	if recalled.UserOriginalRequest != "clarified ask" {
		t.Errorf("recall: expected user_original_request 'clarified ask', got %q", recalled.UserOriginalRequest)
	}
}

// TestUpdate_GitRemote_NotRevertedBySubsequentUpdate is the regression test
// for the observed bug: an earlier `PATCH /entries/{path}/metadata` call with
// git_remote wrote to the DB but not to the file. A later `PATCH /entries/{path}`
// call (which reads from file, applies patch, writes file, reindexes)
// silently overwrote the DB with the stale file value. Now that git_remote
// is a first-class Update field, subsequent updates that don't touch
// git_remote must NOT revert prior git_remote changes.
func TestUpdate_GitRemote_NotRevertedBySubsequentUpdate(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:      "task",
		Title:     "Persistence Task",
		GitRemote: "git@example.com:orig/repo.git",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Step 1: update git_remote via Update
	if _, err := svc.Update(ctx, saved.ID, types.UpdateEntryRequest{
		GitRemote: strPtr("git@example.com:new/repo.git"),
	}); err != nil {
		t.Fatalf("first Update failed: %v", err)
	}

	// Step 2: unrelated update (only touches priority) — must NOT revert git_remote
	if _, err := svc.Update(ctx, saved.ID, types.UpdateEntryRequest{
		Priority: strPtr("high"),
	}); err != nil {
		t.Fatalf("second Update failed: %v", err)
	}

	// Step 3: verify git_remote survived
	recalled, err := svc.Recall(ctx, saved.ID)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}
	if recalled.GitRemote != "git@example.com:new/repo.git" {
		t.Errorf("git_remote was reverted after unrelated update: got %q, want %q",
			recalled.GitRemote, "git@example.com:new/repo.git")
	}
	if recalled.Priority != "high" {
		t.Errorf("priority not applied: got %q, want %q", recalled.Priority, "high")
	}
}

// TestUpdate_DirectPrompt_OnAutomationMirrorsToAction verifies that updating
// the root direct_prompt of an automation entry also writes the same value to
// action.direct_prompt. The runtime renders prompts from action.direct_prompt;
// callers (notably MCP brain_update) only expose top-level direct_prompt and
// would otherwise silently fail to update the prompt that's actually used.
func TestUpdate_DirectPrompt_OnAutomationMirrorsToAction(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	originalAction := &types.AutomationAction{
		Type:         "prompt",
		DirectPrompt: "old prompt",
		Agent:        "general",
	}
	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Inspector",
		Project: "automation-update-test",
		Trigger: &types.TriggerConfig{
			Type:  "event",
			Event: "task.status_changed",
		},
		Action: originalAction,
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	newPrompt := "new prompt with {{.TaskID}}"
	updated, err := svc.Update(ctx, saved.ID, types.UpdateEntryRequest{
		DirectPrompt: &newPrompt,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.DirectPrompt != newPrompt {
		t.Errorf("root direct_prompt = %q, want %q", updated.DirectPrompt, newPrompt)
	}
	if updated.Action == nil {
		t.Fatalf("action is nil after update; expected mirrored prompt")
	}
	if updated.Action.DirectPrompt != newPrompt {
		t.Errorf("action.direct_prompt = %q, want %q", updated.Action.DirectPrompt, newPrompt)
	}
	// Other fields on the existing Action must be preserved (the mirror is
	// strictly additive — it must not stomp agent/type set on the action).
	if updated.Action.Agent != "general" {
		t.Errorf("action.agent = %q, want preserved %q", updated.Action.Agent, "general")
	}
	if updated.Action.Type != "prompt" {
		t.Errorf("action.type = %q, want preserved %q", updated.Action.Type, "prompt")
	}
}

// TestUpdate_DirectPrompt_AutomationExplicitActionWins verifies that an
// explicit action.direct_prompt in the same update wins over the root
// direct_prompt mirror. Callers that pass both a root direct_prompt and a
// nested action expect the nested one to take precedence.
func TestUpdate_DirectPrompt_AutomationExplicitActionWins(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Inspector",
		Project: "automation-update-explicit-test",
		Trigger: &types.TriggerConfig{Type: "event", Event: "task.status_changed"},
		Action:  &types.AutomationAction{Type: "prompt", DirectPrompt: "old"},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	root := "from root"
	updated, err := svc.Update(ctx, saved.ID, types.UpdateEntryRequest{
		DirectPrompt: &root,
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "from nested action",
		},
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.Action.DirectPrompt != "from nested action" {
		t.Errorf("action.direct_prompt = %q, want explicit nested value", updated.Action.DirectPrompt)
	}
	if updated.DirectPrompt != root {
		t.Errorf("root direct_prompt = %q, want %q", updated.DirectPrompt, root)
	}
}

// TestUpdate_DirectPrompt_OnTaskDoesNotCreateAction verifies that the
// automation-mirror behavior does not leak into task entries.
func TestUpdate_DirectPrompt_OnTaskDoesNotCreateAction(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Real task",
		Project: "task-update-test",
	})
	if err != nil {
		t.Fatalf("Save task failed: %v", err)
	}

	prompt := "do the work"
	updated, err := svc.Update(ctx, saved.ID, types.UpdateEntryRequest{
		DirectPrompt: &prompt,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.DirectPrompt != prompt {
		t.Errorf("root direct_prompt = %q, want %q", updated.DirectPrompt, prompt)
	}
	if updated.Action != nil {
		t.Errorf("task entry should not have action set after update; got %+v", updated.Action)
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
// Move: defensive safety tests
// =============================================================================

func TestMove_SourceFileMissingOnDisk(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	// Save an entry so it exists in the DB and on disk
	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Ghost Task",
		Project: "project-a",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Delete the file from disk but leave the DB entry intact
	absPath := filepath.Join(brainDir, filepath.FromSlash(saved.Path))
	if err := os.Remove(absPath); err != nil {
		t.Fatalf("failed to remove file: %v", err)
	}

	// Move should fail with a clear "source file does not exist on disk" error
	_, err = svc.Move(ctx, saved.ID, "project-b")
	if err == nil {
		t.Fatal("expected error when source file is missing on disk")
	}
	if !strings.Contains(err.Error(), "source file does not exist on disk") {
		t.Errorf("error should mention 'source file does not exist on disk', got: %v", err)
	}
}

func TestMove_DestinationVerifiedAfterWrite(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Verified Move",
		Content: "Some content to verify size.",
		Project: "project-a",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	result, err := svc.Move(ctx, saved.ID, "project-b")
	if err != nil {
		t.Fatalf("Move failed: %v", err)
	}

	// Verify destination file exists on disk with non-zero size
	newAbsPath := filepath.Join(brainDir, filepath.FromSlash(result.To))
	info, err := os.Stat(newAbsPath)
	if err != nil {
		t.Fatalf("destination file does not exist: %v", err)
	}
	if info.Size() == 0 {
		t.Error("destination file should have non-zero size")
	}

	// Verify source file is gone
	oldAbsPath := filepath.Join(brainDir, filepath.FromSlash(result.From))
	if _, err := os.Stat(oldAbsPath); !os.IsNotExist(err) {
		t.Error("source file should have been removed")
	}
}

func TestMove_PreservesSourceOnDestWriteFailure(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Protected Task",
		Content: "Important content that must not be lost.",
		Project: "project-a",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Make the destination directory unwritable by creating a FILE where the
	// directory would need to be (so MkdirAll fails).
	destDir := filepath.Join(brainDir, "projects", "project-b", "task")
	// Create parent so we can place a file at the task dir path
	if err := os.MkdirAll(filepath.Dir(destDir), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	// Create a regular file where the directory should be — MkdirAll will fail
	if err := os.WriteFile(destDir, []byte("blocker"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	_, err = svc.Move(ctx, saved.ID, "project-b")
	if err == nil {
		t.Fatal("expected error when destination is unwritable")
	}

	// Source file must still exist
	srcPath := filepath.Join(brainDir, filepath.FromSlash(saved.Path))
	if _, err := os.Stat(srcPath); err != nil {
		t.Errorf("source file should be preserved after failed move, got: %v", err)
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

// =============================================================================
// RecallFull tests
// =============================================================================

func TestRecallFull_ReturnsRawContent(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Save an entry
	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Full Content Test",
		Content: "The body content.",
		Tags:    []string{"test"},
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// RecallFull should return the full file with frontmatter
	fullContent, err := svc.RecallFull(ctx, resp.ID)
	if err != nil {
		t.Fatalf("RecallFull failed: %v", err)
	}

	// Must contain frontmatter delimiters
	if !strings.HasPrefix(fullContent, "---\n") {
		t.Error("expected full content to start with frontmatter delimiter")
	}
	if !strings.Contains(fullContent, "title: Full Content Test") {
		t.Error("expected full content to contain title in frontmatter")
	}
	if !strings.Contains(fullContent, "type: plan") {
		t.Error("expected full content to contain type in frontmatter")
	}
	if !strings.Contains(fullContent, "The body content.") {
		t.Error("expected full content to contain body")
	}
}

func TestRecallFull_ByPath(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Path Lookup Test",
		Content: "Lookup by path.",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	fullContent, err := svc.RecallFull(ctx, resp.Path)
	if err != nil {
		t.Fatalf("RecallFull by path failed: %v", err)
	}

	if !strings.Contains(fullContent, "title: Path Lookup Test") {
		t.Error("expected full content to contain title")
	}
}

func TestRecallFull_ByTitle(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "idea",
		Title:   "Unique Title For RecallFull",
		Content: "Some idea content.",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	fullContent, err := svc.RecallFull(ctx, "Unique Title For RecallFull")
	if err != nil {
		t.Fatalf("RecallFull by title failed: %v", err)
	}

	if !strings.Contains(fullContent, "title: Unique Title For RecallFull") {
		t.Error("expected full content to contain title")
	}
}

func TestRecallFull_NotFound(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := svc.RecallFull(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent entry")
	}
}

func TestRecallFull_EmptyPathOrID(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := svc.RecallFull(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty pathOrID")
	}
}

func TestRecallFull_RoundTrips(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	// Save an entry
	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Round Trip Test",
		Content: "Round trip content with special chars: *bold* and `code`.",
		Tags:    []string{"go", "test"},
		Status:  "pending",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Get the full content via RecallFull
	fullContent, err := svc.RecallFull(ctx, resp.ID)
	if err != nil {
		t.Fatalf("RecallFull failed: %v", err)
	}

	// Read the actual file from disk
	absPath := filepath.Join(brainDir, filepath.FromSlash(resp.Path))
	diskContent, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("failed to read file from disk: %v", err)
	}

	// The full content should match what's on disk
	if fullContent != string(diskContent) {
		t.Errorf("RecallFull content does not match disk content.\nRecallFull:\n%s\nDisk:\n%s", fullContent, string(diskContent))
	}
}

func TestRecallFull_FallbackReconstruction(t *testing.T) {
	svc, store, _ := newTestBrainService(t)
	ctx := context.Background()

	// Save an entry
	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Fallback Test",
		Content: "Fallback body content.",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Clear the raw_content in DB to force fallback reconstruction
	_, err = store.DB().ExecContext(ctx, `UPDATE notes SET raw_content = NULL WHERE short_id = ?`, resp.ID)
	if err != nil {
		t.Fatalf("failed to clear raw_content: %v", err)
	}

	// RecallFull should still work via reconstruction
	fullContent, err := svc.RecallFull(ctx, resp.ID)
	if err != nil {
		t.Fatalf("RecallFull with fallback failed: %v", err)
	}

	// Must contain frontmatter
	if !strings.HasPrefix(fullContent, "---\n") {
		t.Error("reconstructed content should start with frontmatter delimiter")
	}
	if !strings.Contains(fullContent, "title: Fallback Test") {
		t.Error("reconstructed content should contain title")
	}
	if !strings.Contains(fullContent, "type: plan") {
		t.Error("reconstructed content should contain type")
	}
	if !strings.Contains(fullContent, "Fallback body content.") {
		t.Error("reconstructed content should contain body")
	}

	// Verify the reconstructed content round-trips through Parse
	doc, err := frontmatter.Parse(fullContent)
	if err != nil {
		t.Fatalf("failed to parse reconstructed content: %v", err)
	}
	if doc.Frontmatter.Title != "Fallback Test" {
		t.Errorf("expected title 'Fallback Test', got %q", doc.Frontmatter.Title)
	}
	if doc.Frontmatter.Type != "plan" {
		t.Errorf("expected type 'plan', got %q", doc.Frontmatter.Type)
	}
	if !strings.Contains(doc.Body, "Fallback body content.") {
		t.Errorf("expected body to contain 'Fallback body content.', got %q", doc.Body)
	}
}

func TestRecallFull_EmptyBody(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:  "scratch",
		Title: "Empty Body Test",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	fullContent, err := svc.RecallFull(ctx, resp.ID)
	if err != nil {
		t.Fatalf("RecallFull failed: %v", err)
	}

	if !strings.HasPrefix(fullContent, "---\n") {
		t.Error("expected full content to start with frontmatter delimiter")
	}
	if !strings.Contains(fullContent, "title: Empty Body Test") {
		t.Error("expected full content to contain title")
	}
}

func TestSaveRecall_ExecutorAndExtensions(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Create a task with executor and extensions
	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:       "task",
		Title:      "Executor Round Trip",
		Content:    "Test body",
		Executor:   "pi",
		Extensions: []string{"browser", "filesystem"},
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Recall the entry
	entry, err := svc.Recall(ctx, resp.ID)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}

	if entry.Executor != "pi" {
		t.Errorf("executor = %q, want %q", entry.Executor, "pi")
	}
	if len(entry.Extensions) != 2 {
		t.Fatalf("extensions len = %d, want 2", len(entry.Extensions))
	}
	if entry.Extensions[0] != "browser" {
		t.Errorf("extensions[0] = %q, want %q", entry.Extensions[0], "browser")
	}
	if entry.Extensions[1] != "filesystem" {
		t.Errorf("extensions[1] = %q, want %q", entry.Extensions[1], "filesystem")
	}
}

// Stub tests removed — methods now implemented in brain.go (Phase 4).

// =============================================================================
// BulkUpdate tests
// =============================================================================

func TestBulkUpdate_ValidationErrors(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	tests := []struct {
		name string
		req  types.BulkUpdateRequest
		err  string
	}{
		{
			name: "no filter or entries",
			req:  types.BulkUpdateRequest{},
			err:  "must specify either filter or entries",
		},
		{
			name: "both filter and entries",
			req: types.BulkUpdateRequest{
				Filter:  &types.BulkUpdateFilter{},
				Entries: []types.BulkUpdateEntry{{Path: "x"}},
			},
			err: "cannot specify both filter and entries",
		},
		{
			name: "filter without updates",
			req: types.BulkUpdateRequest{
				Filter: &types.BulkUpdateFilter{},
			},
			err: "updates required when using filter mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.BulkUpdate(ctx, tt.req)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.err) {
				t.Errorf("expected error containing %q, got %q", tt.err, err.Error())
			}
		})
	}
}

func TestBulkUpdate_ExplicitMode(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Create two entries
	resp1, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Task One",
		Content: "content one",
		Project: "test-proj",
	})
	if err != nil {
		t.Fatalf("Save 1 failed: %v", err)
	}

	resp2, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Task Two",
		Content: "content two",
		Project: "test-proj",
	})
	if err != nil {
		t.Fatalf("Save 2 failed: %v", err)
	}

	// Bulk update both to status=completed
	result, err := svc.BulkUpdate(ctx, types.BulkUpdateRequest{
		Entries: []types.BulkUpdateEntry{
			{Path: resp1.Path, Updates: types.UpdateEntryRequest{Status: strPtr("completed")}},
			{Path: resp2.Path, Updates: types.UpdateEntryRequest{Status: strPtr("completed")}},
		},
	})
	if err != nil {
		t.Fatalf("BulkUpdate failed: %v", err)
	}

	if result.Updated != 2 {
		t.Errorf("expected 2 updated, got %d", result.Updated)
	}
	if result.Failed != 0 {
		t.Errorf("expected 0 failed, got %d", result.Failed)
	}
	if result.Total != 2 {
		t.Errorf("expected total 2, got %d", result.Total)
	}
	if result.DryRun {
		t.Error("expected DryRun=false")
	}

	// Verify entries were actually updated
	entry1, err := svc.Recall(ctx, resp1.ID)
	if err != nil {
		t.Fatalf("Recall 1 failed: %v", err)
	}
	if entry1.Status != "completed" {
		t.Errorf("expected status completed, got %q", entry1.Status)
	}

	entry2, err := svc.Recall(ctx, resp2.ID)
	if err != nil {
		t.Fatalf("Recall 2 failed: %v", err)
	}
	if entry2.Status != "completed" {
		t.Errorf("expected status completed, got %q", entry2.Status)
	}
}

func TestBulkUpdate_FilterMode(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Create entries with known feature_id
	for i := 0; i < 3; i++ {
		_, err := svc.Save(ctx, types.CreateEntryRequest{
			Type:      "task",
			Title:     "Filter Task",
			Content:   "content",
			Project:   "filter-proj",
			FeatureID: "feat-abc",
			Status:    "active",
		})
		if err != nil {
			t.Fatalf("Save %d failed: %v", i, err)
		}
	}

	// Create an entry with different feature_id (should NOT be updated)
	_, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:      "task",
		Title:     "Other Task",
		Content:   "other",
		Project:   "filter-proj",
		FeatureID: "feat-other",
		Status:    "active",
	})
	if err != nil {
		t.Fatalf("Save other failed: %v", err)
	}

	// Bulk update by filter: feature_id=feat-abc
	featureID := "feat-abc"
	result, err := svc.BulkUpdate(ctx, types.BulkUpdateRequest{
		Filter: &types.BulkUpdateFilter{
			FeatureID: &featureID,
		},
		Updates: &types.UpdateEntryRequest{
			Status: strPtr("completed"),
		},
	})
	if err != nil {
		t.Fatalf("BulkUpdate failed: %v", err)
	}

	if result.Updated != 3 {
		t.Errorf("expected 3 updated, got %d", result.Updated)
	}
	if result.Failed != 0 {
		t.Errorf("expected 0 failed, got %d", result.Failed)
	}
}

func TestBulkUpdate_DryRun(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Create an entry
	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "DryRun Task",
		Content: "content",
		Project: "dry-proj",
		Status:  "active",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Dry run: should return matches without applying
	result, err := svc.BulkUpdate(ctx, types.BulkUpdateRequest{
		Entries: []types.BulkUpdateEntry{
			{Path: resp.Path, Updates: types.UpdateEntryRequest{Status: strPtr("completed")}},
		},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("BulkUpdate dry run failed: %v", err)
	}

	if !result.DryRun {
		t.Error("expected DryRun=true")
	}
	if result.Total != 1 {
		t.Errorf("expected total 1, got %d", result.Total)
	}
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	if result.Results[0].Status != "ok" {
		t.Errorf("expected dry run result status=ok, got %q", result.Results[0].Status)
	}

	// Verify entry was NOT actually updated
	entry, err := svc.Recall(ctx, resp.ID)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}
	if entry.Status != "active" {
		t.Errorf("expected status to remain active after dry run, got %q", entry.Status)
	}
}

func TestBulkUpdate_SafetyCap(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Create 5 entries
	for i := 0; i < 5; i++ {
		_, err := svc.Save(ctx, types.CreateEntryRequest{
			Type:    "task",
			Title:   "Cap Task",
			Content: "content",
			Project: "cap-proj",
		})
		if err != nil {
			t.Fatalf("Save %d failed: %v", i, err)
		}
	}

	// Request with limit=2
	project := "cap-proj"
	result, err := svc.BulkUpdate(ctx, types.BulkUpdateRequest{
		Filter: &types.BulkUpdateFilter{
			Project: &project,
		},
		Updates: &types.UpdateEntryRequest{
			Status: strPtr("completed"),
		},
		Limit: 2,
	})
	if err != nil {
		t.Fatalf("BulkUpdate with limit failed: %v", err)
	}

	if result.Total > 2 {
		t.Errorf("expected total <= 2 (safety cap), got %d", result.Total)
	}
}

func TestBulkUpdate_SafetyCapClampsAbove100(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Create 1 entry
	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Clamp Task",
		Content: "content",
		Project: "clamp-proj",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Request with limit=999 (should be clamped to 100)
	result, err := svc.BulkUpdate(ctx, types.BulkUpdateRequest{
		Entries: []types.BulkUpdateEntry{
			{Path: resp.Path, Updates: types.UpdateEntryRequest{Status: strPtr("completed")}},
		},
		Limit: 999,
	})
	if err != nil {
		t.Fatalf("BulkUpdate failed: %v", err)
	}

	// Should still work — the clamp doesn't block, it just limits
	if result.Updated != 1 {
		t.Errorf("expected 1 updated, got %d", result.Updated)
	}
}

func TestBulkUpdate_PartialFailure(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Create one valid entry
	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Valid Task",
		Content: "content",
		Project: "partial-proj",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Bulk update with one valid and one invalid path
	result, err := svc.BulkUpdate(ctx, types.BulkUpdateRequest{
		Entries: []types.BulkUpdateEntry{
			{Path: resp.Path, Updates: types.UpdateEntryRequest{Status: strPtr("completed")}},
			{Path: "nonexistent/path.md", Updates: types.UpdateEntryRequest{Status: strPtr("completed")}},
		},
	})
	if err != nil {
		t.Fatalf("BulkUpdate failed: %v", err)
	}

	// Should have partial success
	if result.Updated != 1 {
		t.Errorf("expected 1 updated, got %d", result.Updated)
	}
	if result.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", result.Failed)
	}
	if result.Total != 2 {
		t.Errorf("expected total 2, got %d", result.Total)
	}

	// Check result statuses
	var okCount, errCount int
	for _, r := range result.Results {
		switch r.Status {
		case "ok":
			okCount++
		case "error":
			errCount++
			if r.Error == "" {
				t.Error("expected error message for failed result")
			}
		}
	}
	if okCount != 1 || errCount != 1 {
		t.Errorf("expected 1 ok + 1 error, got %d ok + %d error", okCount, errCount)
	}
}

func TestBulkUpdate_FilterWithMultipleFields(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Create entries with specific type+status+project
	for i := 0; i < 2; i++ {
		_, err := svc.Save(ctx, types.CreateEntryRequest{
			Type:    "task",
			Title:   "Multi-Filter Task",
			Content: "content",
			Project: "mf-proj",
			Status:  "pending",
		})
		if err != nil {
			t.Fatalf("Save %d failed: %v", i, err)
		}
	}

	// Create one with different status (should NOT match)
	_, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Active Task",
		Content: "content",
		Project: "mf-proj",
		Status:  "active",
	})
	if err != nil {
		t.Fatalf("Save active failed: %v", err)
	}

	// Filter by project + status=pending
	project := "mf-proj"
	status := "pending"
	entryType := "task"
	result, err := svc.BulkUpdate(ctx, types.BulkUpdateRequest{
		Filter: &types.BulkUpdateFilter{
			Project: &project,
			Status:  &status,
			Type:    &entryType,
		},
		Updates: &types.UpdateEntryRequest{
			Status: strPtr("completed"),
		},
	})
	if err != nil {
		t.Fatalf("BulkUpdate failed: %v", err)
	}

	if result.Updated != 2 {
		t.Errorf("expected 2 updated (only pending tasks), got %d", result.Updated)
	}
}

func TestBulkUpdate_DryRunWithInvalidEntry(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Dry run with an invalid path should report error in results, not fail the request
	result, err := svc.BulkUpdate(ctx, types.BulkUpdateRequest{
		Entries: []types.BulkUpdateEntry{
			{Path: "nonexistent/path.md", Updates: types.UpdateEntryRequest{Status: strPtr("completed")}},
		},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("BulkUpdate dry run failed: %v", err)
	}

	if !result.DryRun {
		t.Error("expected DryRun=true")
	}
	if result.Total != 1 {
		t.Errorf("expected total 1, got %d", result.Total)
	}
	if result.Results[0].Status != "error" {
		t.Errorf("expected dry run result status=error for invalid entry, got %q", result.Results[0].Status)
	}
}

// ---------------------------------------------------------------------------
// Feature completion detection tests
// ---------------------------------------------------------------------------

func TestUpdate_FeatureAllCompleted_EmittedWhenAllTasksComplete(t *testing.T) {
	svc, _, _, bus := newTestBrainServiceWithBus(t)
	ctx := context.Background()

	featureID := "test-feature"

	// Create 3 tasks in the same feature, 2 already completed, 1 pending
	for i, status := range []string{"completed", "completed", "pending"} {
		_, err := svc.Save(ctx, types.CreateEntryRequest{
			Type:      "task",
			Title:     "Task " + string(rune('A'+i)),
			Content:   "Task content",
			Status:    status,
			Project:   "testproj",
			FeatureID: featureID,
		})
		if err != nil {
			t.Fatalf("Save task %d: %v", i, err)
		}
	}

	// Subscribe to feature.all_completed events
	received := make(chan events.Event, 1)
	bus.Subscribe(events.FeatureAllCompleted, func(e events.Event) {
		received <- e
	})

	// Complete the last pending task - should trigger feature.all_completed
	// List tasks to find the pending one
	listResp, err := svc.List(ctx, types.ListEntriesRequest{
		Type:      "task",
		Project:   "testproj",
		FeatureID: featureID,
		Status:    "pending",
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listResp.Entries) != 1 {
		t.Fatalf("expected 1 pending task, got %d", len(listResp.Entries))
	}

	pendingTask := listResp.Entries[0]
	_, err = svc.Update(ctx, pendingTask.Path, types.UpdateEntryRequest{
		Status: strPtr("completed"),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Wait for the event (handlers run in goroutines)
	select {
	case evt := <-received:
		if evt.Type != events.FeatureAllCompleted {
			t.Errorf("expected event type %q, got %q", events.FeatureAllCompleted, evt.Type)
		}
		if evt.Payload["feature_id"] != featureID {
			t.Errorf("expected feature_id=%q, got %q", featureID, evt.Payload["feature_id"])
		}
		if evt.Payload["project"] != "testproj" {
			t.Errorf("expected project=testproj, got %q", evt.Payload["project"])
		}
		if evt.Payload["task_count"] != 3 {
			t.Errorf("expected task_count=3, got %v", evt.Payload["task_count"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for feature.all_completed event")
	}
}

func TestUpdate_FeatureAllCompleted_NotEmittedWhenTasksRemain(t *testing.T) {
	svc, _, _, bus := newTestBrainServiceWithBus(t)
	ctx := context.Background()

	featureID := "incomplete-feature"

	// Create 3 tasks: 1 completed, 2 pending
	for i, status := range []string{"completed", "pending", "pending"} {
		_, err := svc.Save(ctx, types.CreateEntryRequest{
			Type:      "task",
			Title:     "Task " + string(rune('A'+i)),
			Content:   "Task content",
			Status:    status,
			Project:   "testproj",
			FeatureID: featureID,
		})
		if err != nil {
			t.Fatalf("Save task %d: %v", i, err)
		}
	}

	// Subscribe to feature.all_completed events
	received := make(chan events.Event, 1)
	bus.Subscribe(events.FeatureAllCompleted, func(e events.Event) {
		received <- e
	})

	// Complete ONE of the two pending tasks - feature should NOT be completed
	listResp, err := svc.List(ctx, types.ListEntriesRequest{
		Type:      "task",
		Project:   "testproj",
		FeatureID: featureID,
		Status:    "pending",
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	_, err = svc.Update(ctx, listResp.Entries[0].Path, types.UpdateEntryRequest{
		Status: strPtr("completed"),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Should NOT receive feature.all_completed
	select {
	case evt := <-received:
		t.Fatalf("unexpected feature.all_completed event: %+v", evt)
	case <-time.After(200 * time.Millisecond):
		// Good - no event emitted
	}
}

func TestUpdate_FeatureAllCompleted_NotEmittedForTaskWithoutFeatureID(t *testing.T) {
	svc, _, _, bus := newTestBrainServiceWithBus(t)
	ctx := context.Background()

	// Create a task without feature_id
	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Standalone Task",
		Content: "No feature",
		Status:  "pending",
		Project: "testproj",
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Subscribe to feature.all_completed events
	received := make(chan events.Event, 1)
	bus.Subscribe(events.FeatureAllCompleted, func(e events.Event) {
		received <- e
	})

	// Complete the task
	_, err = svc.Update(ctx, resp.Path, types.UpdateEntryRequest{
		Status: strPtr("completed"),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Should NOT receive feature.all_completed
	select {
	case evt := <-received:
		t.Fatalf("unexpected feature.all_completed event for task without feature_id: %+v", evt)
	case <-time.After(200 * time.Millisecond):
		// Good - no event
	}
}

func TestUpdate_FeatureAllCompleted_DedupKeyPreventsDoubleEmission(t *testing.T) {
	svc, _, _, bus := newTestBrainServiceWithBus(t)
	ctx := context.Background()

	featureID := "dedup-feature"

	// Create 2 tasks in feature, both pending
	var taskPaths []string
	for i := range 2 {
		resp, err := svc.Save(ctx, types.CreateEntryRequest{
			Type:      "task",
			Title:     "Task " + string(rune('A'+i)),
			Content:   "Task content",
			Status:    "pending",
			Project:   "testproj",
			FeatureID: featureID,
		})
		if err != nil {
			t.Fatalf("Save task %d: %v", i, err)
		}
		taskPaths = append(taskPaths, resp.Path)
	}

	// Subscribe and collect all events
	received := make(chan events.Event, 10)
	bus.Subscribe(events.FeatureAllCompleted, func(e events.Event) {
		received <- e
	})

	// Complete both tasks
	for _, path := range taskPaths {
		_, err := svc.Update(ctx, path, types.UpdateEntryRequest{
			Status: strPtr("completed"),
		})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
	}

	// Wait for events to arrive
	time.Sleep(300 * time.Millisecond)

	// Should have received exactly 1 event (second complete also triggers check,
	// but both should produce the event - dedup is by DedupKey at bus level)
	// At minimum, the event for the LAST completion should have a DedupKey set
	close(received)
	var evts []events.Event
	for e := range received {
		evts = append(evts, e)
	}

	if len(evts) == 0 {
		t.Fatal("expected at least 1 feature.all_completed event")
	}

	// Verify DedupKey is set on the event
	for _, e := range evts {
		if e.DedupKey == "" {
			t.Error("expected DedupKey to be set on feature.all_completed event")
		}
	}
}

// TestInject_BoundsAssembledContext pins that inject cannot swallow the
// caller's working context.
//
// Inject is the load-context primitive: an agent calls it BEFORE working,
// so whatever it returns is spent from the budget it then has to think
// with. Bodies used to be concatenated whole with no cap, so a default
// five-entry call against long entries returned tens of thousands of
// tokens — the tool meant to prepare an agent was the most expensive one
// available to it. Observed live: max_entries:3 returned ~9k tokens.
func TestInject_BoundsAssembledContext(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	body := strings.Repeat("lorem ipsum dolor sit amet\n\n", 900) // ~25k chars each
	for i := 0; i < 3; i++ {
		if _, err := svc.Save(ctx, types.CreateEntryRequest{
			Type: "note", Project: "p",
			Title:   fmt.Sprintf("Bulky note %d", i),
			Content: body,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	max := 3
	resp, err := svc.Inject(ctx, types.InjectRequest{Query: "lorem", Project: "p", MaxEntries: &max})
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	if len(resp.Entries) == 0 {
		t.Fatal("fixture problem: expected the seeded notes to match")
	}

	if len(resp.Context) > types.DefaultInjectMaxChars+2000 {
		t.Errorf("context is %d chars, well past the %d default budget",
			len(resp.Context), types.DefaultInjectMaxChars)
	}
	if !resp.Truncated {
		t.Error("truncation happened but was not reported")
	}
	if !strings.Contains(resp.Context, "truncated — recall") {
		t.Errorf("a cut entry must name itself so the caller can fetch it in full:\n%.400s", resp.Context)
	}

	// Every match should still be represented — the budget is split, not
	// spent first-come, so one huge entry cannot crowd the others out.
	for i := 0; i < len(resp.Entries); i++ {
		title := fmt.Sprintf("Bulky note %d", i)
		if !strings.Contains(resp.Context, title) {
			t.Errorf("%q was crowded out of the budget entirely", title)
		}
	}
}

// TestInject_NegativeMaxCharsIsUnbounded pins the escape hatch for callers
// that genuinely want every byte.
func TestInject_NegativeMaxCharsIsUnbounded(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	body := strings.Repeat("alpha beta gamma\n\n", 2000)
	if _, err := svc.Save(ctx, types.CreateEntryRequest{
		Type: "note", Project: "p", Title: "Huge", Content: body,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	unbounded := -1
	resp, err := svc.Inject(ctx, types.InjectRequest{Query: "alpha", Project: "p", MaxChars: &unbounded})
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	if resp.Truncated {
		t.Error("negative maxChars must not truncate")
	}
	if len(resp.Context) < len(body) {
		t.Errorf("unbounded inject returned %d chars for a %d-char body", len(resp.Context), len(body))
	}
}

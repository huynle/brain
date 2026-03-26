package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// UpdateMetadata file-sync tests
// =============================================================================

// TestUpdateMetadata_StatusChangeWritesToFile verifies that updating status via
// UpdateMetadata writes the change back to the markdown file on disk.
func TestUpdateMetadata_StatusChangeWritesToFile(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	// Create an entry with status "pending"
	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Metadata Status Test",
		Content: "Some task content.",
		Status:  "pending",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Update status via UpdateMetadata
	_, err = svc.UpdateMetadata(ctx, saved.ID, map[string]interface{}{
		"status": "completed",
	})
	if err != nil {
		t.Fatalf("UpdateMetadata failed: %v", err)
	}

	// Read the file from disk and verify frontmatter has status: completed
	absPath := filepath.Join(brainDir, filepath.FromSlash(saved.Path))
	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	fileStr := string(content)

	if !strings.Contains(fileStr, "status: completed") {
		t.Errorf("file should contain 'status: completed', got:\n%s", fileStr)
	}
	if strings.Contains(fileStr, "status: pending") {
		t.Errorf("file should NOT contain 'status: pending', got:\n%s", fileStr)
	}
}

// TestUpdateMetadata_TransientOnlyDoesNotWriteFile verifies that updating only
// transient fields (like sessions) does NOT modify the file on disk.
func TestUpdateMetadata_TransientOnlyDoesNotWriteFile(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Transient Only Test",
		Content: "Original content.",
		Status:  "pending",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Record the file's modification time
	absPath := filepath.Join(brainDir, filepath.FromSlash(saved.Path))
	info1, err := os.Stat(absPath)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	mtime1 := info1.ModTime()

	// Sleep briefly to ensure mtime would differ if file is rewritten
	time.Sleep(50 * time.Millisecond)

	// Update only transient fields (sessions, claimed_by, claimed_at)
	_, err = svc.UpdateMetadata(ctx, saved.ID, map[string]interface{}{
		"sessions": map[string]interface{}{
			"ses_abc123": map[string]interface{}{
				"timestamp": "2025-01-01T00:00:00Z",
			},
		},
		"claimed_by": "runner-1",
		"claimed_at": 1234567890,
	})
	if err != nil {
		t.Fatalf("UpdateMetadata failed: %v", err)
	}

	// Verify file mtime has NOT changed
	info2, err := os.Stat(absPath)
	if err != nil {
		t.Fatalf("stat file after update: %v", err)
	}
	mtime2 := info2.ModTime()

	if !mtime1.Equal(mtime2) {
		t.Errorf("file mtime should NOT have changed for transient-only update; before=%v after=%v", mtime1, mtime2)
	}
}

// TestUpdateMetadata_PreservesRuntimeFieldsAfterFileWrite verifies that when
// UpdateMetadata writes durable fields to disk, runtime-only fields (like
// sessions) are preserved in the DB after re-indexing.
func TestUpdateMetadata_PreservesRuntimeFieldsAfterFileWrite(t *testing.T) {
	svc, store, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:   "task",
		Title:  "Preserve Runtime Test",
		Status: "pending",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// First, set some runtime metadata (sessions) via MergeMetadata directly
	_, err = store.MergeMetadata(ctx, saved.Path, map[string]interface{}{
		"sessions": map[string]interface{}{
			"ses_runtime": map[string]interface{}{
				"timestamp": "2025-06-15T00:00:00Z",
			},
		},
		"direct_prompt": "do the thing",
	})
	if err != nil {
		t.Fatalf("MergeMetadata (setup) failed: %v", err)
	}

	// Now update a durable field (status) via UpdateMetadata — this should
	// write to file AND preserve the runtime fields in the DB
	entry, err := svc.UpdateMetadata(ctx, saved.ID, map[string]interface{}{
		"status": "completed",
	})
	if err != nil {
		t.Fatalf("UpdateMetadata failed: %v", err)
	}

	// Verify the returned entry has status completed
	if entry.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", entry.Status)
	}

	// Verify runtime fields are still in the DB by recalling
	recalled, err := svc.Recall(ctx, saved.ID)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}

	// The sessions should still be present (preserved across re-index)
	if recalled.Sessions == nil || len(recalled.Sessions) == 0 {
		t.Error("expected sessions to be preserved after file write + re-index")
	}
}

// TestUpdateMetadata_AppendAddsContentToFileBody verifies that the "append"
// field in UpdateMetadata appends text to the markdown file body.
func TestUpdateMetadata_AppendAddsContentToFileBody(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Append Test",
		Content: "Original content here.",
		Status:  "pending",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Append content via UpdateMetadata
	_, err = svc.UpdateMetadata(ctx, saved.ID, map[string]interface{}{
		"append": "## Progress\n- Done step 1",
	})
	if err != nil {
		t.Fatalf("UpdateMetadata failed: %v", err)
	}

	// Read the file and verify both original and appended content
	absPath := filepath.Join(brainDir, filepath.FromSlash(saved.Path))
	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	fileStr := string(content)

	if !strings.Contains(fileStr, "Original content here.") {
		t.Errorf("file should still contain original content, got:\n%s", fileStr)
	}
	if !strings.Contains(fileStr, "## Progress") {
		t.Errorf("file should contain appended heading, got:\n%s", fileStr)
	}
	if !strings.Contains(fileStr, "- Done step 1") {
		t.Errorf("file should contain appended list item, got:\n%s", fileStr)
	}
}

// TestUpdateMetadata_NoteAppendsTimestampedContent verifies that the "note"
// field in UpdateMetadata appends a timestamped status note to the file body.
func TestUpdateMetadata_NoteAppendsTimestampedContent(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	freezeTime(t, time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC))

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Note Test",
		Content: "Task body.",
		Status:  "pending",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Update status with a note via UpdateMetadata
	_, err = svc.UpdateMetadata(ctx, saved.ID, map[string]interface{}{
		"status": "completed",
		"note":   "Task is done.",
	})
	if err != nil {
		t.Fatalf("UpdateMetadata failed: %v", err)
	}

	absPath := filepath.Join(brainDir, filepath.FromSlash(saved.Path))
	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	fileStr := string(content)

	if !strings.Contains(fileStr, "Status changed to **completed**") {
		t.Errorf("file should contain status change note, got:\n%s", fileStr)
	}
	if !strings.Contains(fileStr, "Task is done.") {
		t.Errorf("file should contain note text, got:\n%s", fileStr)
	}
}

// TestUpdateMetadata_MultipleDurableFields verifies that updating multiple
// durable fields at once (status + priority + tags) all get written to file.
func TestUpdateMetadata_MultipleDurableFields(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:     "task",
		Title:    "Multi Field Test",
		Status:   "pending",
		Priority: "low",
		Tags:     []string{"old-tag"},
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Update multiple durable fields at once
	_, err = svc.UpdateMetadata(ctx, saved.ID, map[string]interface{}{
		"status":   "in_progress",
		"priority": "high",
		"tags":     []interface{}{"new-tag-1", "new-tag-2"},
	})
	if err != nil {
		t.Fatalf("UpdateMetadata failed: %v", err)
	}

	absPath := filepath.Join(brainDir, filepath.FromSlash(saved.Path))
	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	fileStr := string(content)

	if !strings.Contains(fileStr, "status: in_progress") {
		t.Errorf("file should contain 'status: in_progress', got:\n%s", fileStr)
	}
	if !strings.Contains(fileStr, "priority: high") {
		t.Errorf("file should contain 'priority: high', got:\n%s", fileStr)
	}
	if !strings.Contains(fileStr, "new-tag-1") {
		t.Errorf("file should contain 'new-tag-1', got:\n%s", fileStr)
	}
}

// TestUpdateMetadata_DependsOnWritesToFile verifies that updating depends_on
// via UpdateMetadata writes the change to the file.
func TestUpdateMetadata_DependsOnWritesToFile(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:  "task",
		Title: "Deps Metadata Test",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	_, err = svc.UpdateMetadata(ctx, saved.ID, map[string]interface{}{
		"depends_on": []interface{}{"abc12def", "xyz98765"},
	})
	if err != nil {
		t.Fatalf("UpdateMetadata failed: %v", err)
	}

	absPath := filepath.Join(brainDir, filepath.FromSlash(saved.Path))
	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	fileStr := string(content)

	if !strings.Contains(fileStr, "depends_on:") {
		t.Errorf("file should contain 'depends_on:', got:\n%s", fileStr)
	}
	if !strings.Contains(fileStr, "abc12def") {
		t.Errorf("file should contain 'abc12def', got:\n%s", fileStr)
	}
}

// TestUpdateMetadata_FeatureFieldsWriteToFile verifies that feature_id and
// feature_priority are written to the file.
func TestUpdateMetadata_FeatureFieldsWriteToFile(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:  "task",
		Title: "Feature Fields Test",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	_, err = svc.UpdateMetadata(ctx, saved.ID, map[string]interface{}{
		"feature_id":       "feat-auth",
		"feature_priority": "high",
	})
	if err != nil {
		t.Fatalf("UpdateMetadata failed: %v", err)
	}

	absPath := filepath.Join(brainDir, filepath.FromSlash(saved.Path))
	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	fileStr := string(content)

	if !strings.Contains(fileStr, "feature_id: feat-auth") {
		t.Errorf("file should contain 'feature_id: feat-auth', got:\n%s", fileStr)
	}
	if !strings.Contains(fileStr, "feature_priority: high") {
		t.Errorf("file should contain 'feature_priority: high', got:\n%s", fileStr)
	}
}

// TestUpdateMetadata_TitleChangeWritesToFile verifies that updating title via
// UpdateMetadata writes the change to the file.
func TestUpdateMetadata_TitleChangeWritesToFile(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:  "task",
		Title: "Original Title",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	_, err = svc.UpdateMetadata(ctx, saved.ID, map[string]interface{}{
		"title": "Updated Title Via Metadata",
	})
	if err != nil {
		t.Fatalf("UpdateMetadata failed: %v", err)
	}

	absPath := filepath.Join(brainDir, filepath.FromSlash(saved.Path))
	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	fileStr := string(content)

	if !strings.Contains(fileStr, "Updated Title Via Metadata") {
		t.Errorf("file should contain updated title, got:\n%s", fileStr)
	}
}

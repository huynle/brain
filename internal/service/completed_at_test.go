package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// completed_at stamping
// =============================================================================

func TestCompletionStamp_Transitions(t *testing.T) {
	tests := []struct {
		name        string
		oldStatus   string
		newStatus   string
		wantChanged bool
		wantStamp   bool // true = RFC3339 stamp, false = cleared ("")
	}{
		{"pending to completed stamps", "pending", "completed", true, true},
		{"in_progress to completed stamps", "in_progress", "completed", true, true},
		{"pending to validated stamps", "pending", "validated", true, true},
		{"completed to validated preserves", "completed", "validated", false, false},
		{"validated to completed preserves", "validated", "completed", false, false},
		{"completed to pending clears", "completed", "pending", true, false},
		{"validated to pending clears", "validated", "pending", true, false},
		{"pending to blocked no-op", "pending", "blocked", false, false},
		{"pending to cancelled no-op (not a completion)", "pending", "cancelled", false, false},
		{"completed to cancelled clears", "completed", "cancelled", true, false},
		{"same status no-op", "completed", "completed", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stamp, changed := completionStamp(tt.oldStatus, tt.newStatus)
			if changed != tt.wantChanged {
				t.Fatalf("changed = %v, want %v", changed, tt.wantChanged)
			}
			if !changed {
				return
			}
			if tt.wantStamp {
				if _, err := time.Parse(time.RFC3339, stamp); err != nil {
					t.Errorf("stamp %q is not RFC3339: %v", stamp, err)
				}
			} else if stamp != "" {
				t.Errorf("stamp = %q, want cleared", stamp)
			}
		})
	}
}

// TestUpdateMetadata_StampsCompletedAt covers the runner's completion path:
// UpdateTaskStatus goes through PATCH /entries/*/metadata → UpdateMetadata.
// The stamp must land in both the DB metadata JSON and the file frontmatter.
func TestUpdateMetadata_StampsCompletedAt(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type: "task", Title: "Stamp Via Metadata", Content: "x", Status: "pending",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	updated, err := svc.UpdateMetadata(ctx, saved.ID, map[string]interface{}{"status": "completed"})
	if err != nil {
		t.Fatalf("UpdateMetadata failed: %v", err)
	}
	if updated.CompletedAt == "" {
		t.Fatal("entry.CompletedAt empty after completion via UpdateMetadata")
	}
	if _, err := time.Parse(time.RFC3339, updated.CompletedAt); err != nil {
		t.Fatalf("CompletedAt %q not RFC3339: %v", updated.CompletedAt, err)
	}

	// Durable: the stamp must reach the markdown file.
	content, err := os.ReadFile(filepath.Join(brainDir, filepath.FromSlash(saved.Path)))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(content), "completed_at: ") {
		t.Errorf("file should contain completed_at, got:\n%s", content)
	}

	// Reopen clears the stamp in DB and file.
	reopened, err := svc.UpdateMetadata(ctx, saved.ID, map[string]interface{}{"status": "pending"})
	if err != nil {
		t.Fatalf("UpdateMetadata reopen failed: %v", err)
	}
	if reopened.CompletedAt != "" {
		t.Errorf("CompletedAt = %q after reopen, want cleared", reopened.CompletedAt)
	}
	content, _ = os.ReadFile(filepath.Join(brainDir, filepath.FromSlash(saved.Path)))
	if strings.Contains(string(content), "completed_at: ") {
		t.Errorf("file should not contain completed_at after reopen, got:\n%s", content)
	}
}

// TestUpdate_StampsCompletedAt covers the PATCH-with-body path.
func TestUpdate_StampsCompletedAt(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type: "task", Title: "Stamp Via Update", Content: "x", Status: "pending",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	done := "completed"
	updated, err := svc.Update(ctx, saved.ID, types.UpdateEntryRequest{Status: &done})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.CompletedAt == "" {
		t.Fatal("entry.CompletedAt empty after completion via Update")
	}

	// completed → validated must keep the original stamp.
	first := updated.CompletedAt
	validated := "validated"
	updated, err = svc.Update(ctx, saved.ID, types.UpdateEntryRequest{Status: &validated})
	if err != nil {
		t.Fatalf("Update to validated failed: %v", err)
	}
	if updated.CompletedAt != first {
		t.Errorf("CompletedAt changed on completed→validated: %q → %q", first, updated.CompletedAt)
	}
}

// TestListNotes_SortByCompleted verifies ordering by the completed_at stamp
// with a modified-date fallback for entries that predate the field.
func TestListNotes_SortByCompleted(t *testing.T) {
	svc, store, brainDir := newTestBrainService(t)
	ctx := context.Background()

	mk := func(title string) string {
		saved, err := svc.Save(ctx, types.CreateEntryRequest{
			Type: "task", Title: title, Content: "x", Status: "pending",
		})
		if err != nil {
			t.Fatalf("Save %s: %v", title, err)
		}
		return saved.ID
	}
	first := mk("first-done")
	second := mk("second-done")
	legacy := mk("legacy-done")

	// Complete "first" with an old stamp and "second" with a newer one by
	// completing them in order (stamps are wall-clock; enforce ordering by
	// patching completed_at explicitly to controlled values).
	for id, stamp := range map[string]string{
		first:  "2026-01-01T00:00:00Z",
		second: "2026-06-01T00:00:00Z",
	} {
		if _, err := svc.UpdateMetadata(ctx, id, map[string]interface{}{"status": "completed"}); err != nil {
			t.Fatalf("complete %s: %v", id, err)
		}
		if _, err := svc.UpdateMetadata(ctx, id, map[string]interface{}{"completed_at": stamp}); err != nil {
			t.Fatalf("backdate %s: %v", id, err)
		}
	}
	// "legacy" simulates a pre-completed_at entry: completed status but the
	// stamp field wiped (as if written by an old binary). Its modified date
	// (now, the newest write) is the fallback → it must sort first.
	if _, err := svc.UpdateMetadata(ctx, legacy, map[string]interface{}{"status": "completed"}); err != nil {
		t.Fatalf("complete legacy: %v", err)
	}
	if _, err := svc.UpdateMetadata(ctx, legacy, map[string]interface{}{"completed_at": ""}); err != nil {
		t.Fatalf("wipe legacy stamp: %v", err)
	}
	_ = brainDir

	rows, err := store.ListNotes(ctx, &storage.ListOptions{
		Type:      "task",
		Status:    "completed",
		SortBy:    "completed",
		SortOrder: "desc",
	})
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	var titles []string
	for _, r := range rows {
		titles = append(titles, r.Title)
	}
	want := []string{"legacy-done", "second-done", "first-done"}
	if len(titles) != 3 || titles[0] != want[0] || titles[1] != want[1] || titles[2] != want[2] {
		t.Fatalf("order = %v, want %v", titles, want)
	}
}

package service

import (
	"context"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Archived-as-settled semantics: archive counts as done for logic,
// is excluded from view/progress math for UI.
// =============================================================================

// ─── completionStamp ───────────────────────────────────────────────

func TestCompletionStamp_ArchivedTransitions(t *testing.T) {
	tests := []struct {
		name        string
		oldStatus   string
		newStatus   string
		wantChanged bool
		wantStamp   bool // true = RFC3339 stamp, false = cleared ("")
	}{
		{"completed to archived preserves stamp", "completed", "archived", false, false},
		{"validated to archived preserves stamp", "validated", "archived", false, false},
		{"pending to archived never mints a stamp", "pending", "archived", false, false},
		{"in_progress to archived no-op", "in_progress", "archived", false, false},
		{"completed to pending still clears", "completed", "pending", true, false},
		{"pending to completed still stamps", "pending", "completed", true, true},
		{"archived to completed stamps (unarchive to done)", "archived", "completed", true, true},
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

// End-to-end through UpdateMetadata (the runner/PWA status path): archiving a
// completed task must not clear completed_at; archiving a pending task must
// not mint one.
func TestUpdateMetadata_ArchivePreservesCompletedAt(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type: "task", Title: "Archive Keeps Stamp", Content: "x", Status: "pending",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	completed, err := svc.UpdateMetadata(ctx, saved.ID, map[string]interface{}{"status": "completed"})
	if err != nil {
		t.Fatalf("UpdateMetadata complete failed: %v", err)
	}
	if completed.CompletedAt == "" {
		t.Fatal("CompletedAt empty after completion")
	}

	archived, err := svc.UpdateMetadata(ctx, saved.ID, map[string]interface{}{"status": "archived"})
	if err != nil {
		t.Fatalf("UpdateMetadata archive failed: %v", err)
	}
	if archived.CompletedAt != completed.CompletedAt {
		t.Errorf("CompletedAt = %q after archive, want preserved %q", archived.CompletedAt, completed.CompletedAt)
	}
}

func TestUpdateMetadata_PendingToArchivedStaysUnstamped(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type: "task", Title: "Archive Pending", Content: "x", Status: "pending",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	archived, err := svc.UpdateMetadata(ctx, saved.ID, map[string]interface{}{"status": "archived"})
	if err != nil {
		t.Fatalf("UpdateMetadata archive failed: %v", err)
	}
	if archived.CompletedAt != "" {
		t.Errorf("CompletedAt = %q after pending→archived, want empty", archived.CompletedAt)
	}
}

// ─── CheckFeatureCompletion ────────────────────────────────────────

func TestCheckFeatureCompletion_ArchivedCountsAsDone(t *testing.T) {
	tests := []struct {
		name          string
		tasks         []types.ResolvedTask
		wantType      string
		wantCompleted string
		wantTotal     string
	}{
		{
			name: "completed+archived emits feature.completed",
			tasks: []types.ResolvedTask{
				{ID: "t1", FeatureID: "feat-1", Status: "completed"},
				{ID: "t2", FeatureID: "feat-1", Status: "archived"},
			},
			wantType:      types.EventFeatureCompleted,
			wantCompleted: "2",
			wantTotal:     "2",
		},
		{
			name: "all archived emits feature.completed",
			tasks: []types.ResolvedTask{
				{ID: "t1", FeatureID: "feat-1", Status: "archived"},
				{ID: "t2", FeatureID: "feat-1", Status: "archived"},
			},
			wantType:      types.EventFeatureCompleted,
			wantCompleted: "2",
			wantTotal:     "2",
		},
		{
			name: "archived counts in progress math",
			tasks: []types.ResolvedTask{
				{ID: "t1", FeatureID: "feat-1", Status: "archived"},
				{ID: "t2", FeatureID: "feat-1", Status: "pending"},
			},
			wantType:      types.EventFeatureProgress,
			wantCompleted: "1",
			wantTotal:     "2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, hub := newTestEventService()
			svc.SetFeatureTaskLister(&mockFeatureTaskLister{tasks: tt.tasks})

			svc.CheckFeatureCompletion(context.Background(), "proj-1", "feat-1", "t1")

			events := hub.Replay("")
			if len(events) != 1 {
				t.Fatalf("expected 1 event, got %d", len(events))
			}
			if events[0].Type != tt.wantType {
				t.Errorf("event type = %q, want %q", events[0].Type, tt.wantType)
			}
			if got := events[0].Metadata["completed"]; got != tt.wantCompleted {
				t.Errorf("completed = %q, want %q", got, tt.wantCompleted)
			}
			if got := events[0].Metadata["total"]; got != tt.wantTotal {
				t.Errorf("total = %q, want %q", got, tt.wantTotal)
			}
		})
	}
}

// ─── Goal reconcile ────────────────────────────────────────────────

// A goal without custom complete_statuses must see archived tasks as complete;
// otherwise archiving a checked-out feature would trigger need_work and the
// goal would regenerate the work it just shelved.
func TestDecideReconcile_ArchivedCountsCompleteByDefault(t *testing.T) {
	tests := []struct {
		name  string
		cfg   types.GoalConfig
		tasks []types.ResolvedTask
		want  ReconcileDecision
	}{
		{
			name: "completed+archived -> complete",
			cfg:  types.GoalConfig{ID: "g1"},
			tasks: []types.ResolvedTask{
				task("t1", "A", "completed"),
				task("t2", "B", "archived"),
			},
			want: ReconcileComplete,
		},
		{
			name: "all archived -> complete",
			cfg:  types.GoalConfig{ID: "g1"},
			tasks: []types.ResolvedTask{
				task("t1", "A", "archived"),
			},
			want: ReconcileComplete,
		},
		{
			name: "explicit complete_statuses override still wins",
			cfg:  types.GoalConfig{ID: "g1", CompleteStatuses: []string{"validated"}},
			tasks: []types.ResolvedTask{
				task("t1", "A", "archived"),
			},
			want: ReconcileNeedWork,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := decideReconcile(tt.cfg, tt.tasks)
			if got != tt.want {
				t.Errorf("decideReconcile() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ─── Feature stats & status ────────────────────────────────────────

func TestComputeTaskStats_ExcludesArchived(t *testing.T) {
	stats := computeTaskStats([]types.ResolvedTask{
		{Status: "completed"},
		{Status: "archived"},
		{Status: "archived"},
		{Status: "pending"},
	})
	if stats.Total != 2 {
		t.Errorf("Total = %d, want 2 (archived excluded)", stats.Total)
	}
	if stats.Completed != 1 {
		t.Errorf("Completed = %d, want 1", stats.Completed)
	}
	if stats.Pending != 1 {
		t.Errorf("Pending = %d, want 1", stats.Pending)
	}
	if stats.InProgress != 0 || stats.Blocked != 0 {
		t.Errorf("InProgress/Blocked = %d/%d, want 0/0", stats.InProgress, stats.Blocked)
	}
}

func TestComputeFeatureStatus_Archived(t *testing.T) {
	tests := []struct {
		name  string
		tasks []types.ResolvedTask
		want  string
	}{
		{
			name: "mixed archived+completed stays completed",
			tasks: []types.ResolvedTask{
				{Status: "completed"},
				{Status: "archived"},
			},
			want: "completed",
		},
		{
			name: "all archived -> archived",
			tasks: []types.ResolvedTask{
				{Status: "archived"},
				{Status: "archived"},
			},
			want: "archived",
		},
		{
			name: "archived does not mask active work",
			tasks: []types.ResolvedTask{
				{Status: "archived"},
				{Status: "in_progress"},
			},
			want: "in_progress",
		},
		{
			name: "archived+pending -> pending",
			tasks: []types.ResolvedTask{
				{Status: "archived"},
				{Status: "pending"},
			},
			want: "pending",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeFeatureStatus(tt.tasks)
			if got != tt.want {
				t.Errorf("ComputeFeatureStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetReadyFeatures_ExcludesArchived(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", Path: "projects/p/task/t1.md", FeatureID: "feat-archived", Status: "archived"},
		{ID: "t2", Path: "projects/p/task/t2.md", FeatureID: "feat-open", Status: "pending"},
	}
	result := ComputeAndResolveFeatures(tasks)
	ready := GetReadyFeatures(result.Features)
	for _, f := range ready {
		if f.ID == "feat-archived" {
			t.Errorf("all-archived feature %q appeared in ready features", f.ID)
		}
	}
	if len(ready) != 1 || ready[0].ID != "feat-open" {
		t.Errorf("ready = %v, want only feat-open", ready)
	}
}

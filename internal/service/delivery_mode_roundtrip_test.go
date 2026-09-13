package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// TestDeliveryMode_SurvivesStorageRoundTrip is the regression test for the
// dropped delivery_mode field (task cvm87vaw).
//
// delivery_mode was emitted into frontmatter and read back into the
// frontmatter struct correctly, but the API response mapping
// (frontmatter → ResolvedTask in internal/api/entries.go) and the typed
// UpdateEntry path silently dropped it, so every consumer downstream of a
// read saw "". EffectiveDeliveryMode("", mergePolicy) then fell through to the
// merge_policy bridge, so an mr feature folded to local_merge.
//
// This asserts the value survives BOTH the entry read-back (BrainEntry +
// ResolvedTask) AND lands on disk in frontmatter — the on-disk assertion is
// the one the task says would have caught it.
func TestDeliveryMode_SurvivesStorageRoundTrip(t *testing.T) {
	tests := []struct {
		name         string
		deliveryMode string
		want         string
	}{
		{name: "mr persists", deliveryMode: "mr", want: "mr"},
		{name: "local_merge persists", deliveryMode: "local_merge", want: "local_merge"},
		{name: "none persists", deliveryMode: "none", want: "none"},
		{name: "empty stays empty", deliveryMode: "", want: ""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			brain, taskSvc := newTestBrainAndTaskService(t)
			ctx := context.Background()

			saved, err := brain.Save(ctx, types.CreateEntryRequest{
				Type:         "task",
				Title:        "Delivery round trip task",
				Content:      "body",
				Status:       "pending",
				Project:      "brain",
				FeatureID:    "feat-delivery-roundtrip",
				DeliveryMode: tc.deliveryMode,
			})
			if err != nil {
				t.Fatalf("Save: %v", err)
			}

			// Hop 1: metadata JSON → BrainEntry.DeliveryMode.
			entry, err := brain.Recall(ctx, saved.Path)
			if err != nil {
				t.Fatalf("Recall: %v", err)
			}
			if entry.DeliveryMode != tc.want {
				t.Errorf("BrainEntry.DeliveryMode = %q, want %q", entry.DeliveryMode, tc.want)
			}

			// Hop 2: BrainEntry → ResolvedTask.DeliveryMode.
			resolved, err := taskSvc.GetTasks(ctx, "brain")
			if err != nil {
				t.Fatalf("GetTasks: %v", err)
			}
			if len(resolved.Tasks) != 1 {
				t.Fatalf("expected 1 resolved task, got %d", len(resolved.Tasks))
			}
			if got := resolved.Tasks[0].DeliveryMode; got != tc.want {
				t.Errorf("ResolvedTask.DeliveryMode = %q, want %q", got, tc.want)
			}

			// Hop 0 (the assertion that would have caught it): the value is
			// actually on disk in frontmatter, not just round-tripping through
			// an in-memory index.
			raw := readDeliveryTaskFile(t, brain.config.BrainDir, "brain")
			if tc.want == "" {
				if strings.Contains(raw, "delivery_mode:") {
					t.Errorf("expected no delivery_mode line in frontmatter, got:\n%s", raw)
				}
				return
			}
			if !strings.Contains(raw, "delivery_mode: "+tc.want) {
				t.Errorf("expected %q in frontmatter, got:\n%s", "delivery_mode: "+tc.want, raw)
			}
		})
	}
}

// TestFoldDeliveryMode_ReadsPersistedTasks pins the fold against tasks that
// actually went through storage, rather than struct literals.
//
// The existing fold unit tests build ResolvedTask values by hand and so cannot
// detect a broken read path feeding the fold. This test can: if any hop drops
// delivery_mode, an mr feature collapses to the merge_policy bridge default.
func TestFoldDeliveryMode_ReadsPersistedTasks(t *testing.T) {
	tests := []struct {
		name  string
		modes []string
		want  string
	}{
		{name: "all mr folds to mr", modes: []string{"mr", "mr"}, want: "mr"},
		{name: "all local_merge folds to local_merge", modes: []string{"local_merge", "local_merge"}, want: "local_merge"},
		{name: "all none folds to none", modes: []string{"none", "none"}, want: "none"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			brain, taskSvc := newTestBrainAndTaskService(t)
			ctx := context.Background()

			for i, mode := range tc.modes {
				if _, err := brain.Save(ctx, types.CreateEntryRequest{
					Type:         "task",
					Title:        "Feature task",
					Content:      "body",
					Status:       "completed",
					Project:      "brain",
					FeatureID:    "feat-delivery-fold",
					DeliveryMode: mode,
				}); err != nil {
					t.Fatalf("seed task %d (mode %q): %v", i, mode, err)
				}
			}

			tasks, err := taskSvc.GetTasksByFeature(ctx, "brain", "feat-delivery-fold")
			if err != nil {
				t.Fatalf("GetTasksByFeature: %v", err)
			}
			if len(tasks) != len(tc.modes) {
				t.Fatalf("expected %d feature tasks, got %d", len(tc.modes), len(tasks))
			}

			if got := foldDeliveryMode(tasks); got != tc.want {
				t.Errorf("foldDeliveryMode(persisted tasks) = %q, want %q", got, tc.want)
			}
		})
	}
}

// readDeliveryTaskFile returns the contents of a single task markdown file for
// the project (mirrors readCheckoutTaskFile but tolerant of one file).
func readDeliveryTaskFile(t *testing.T, brainDir, project string) string {
	t.Helper()

	dir := filepath.Join(brainDir, "projects", project, "task")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read task dir %s: %v", dir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 task file in %s, got %d", dir, len(entries))
	}

	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read task file: %v", err)
	}
	return string(data)
}

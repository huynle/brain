package service

import (
	"context"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// seedTasks creates n tasks in a project/feature and returns their paths.
func seedTasks(t *testing.T, svc *BrainServiceImpl, project, featureID string, n int) []string {
	t.Helper()
	ctx := context.Background()
	paths := make([]string, 0, n)
	for i := 0; i < n; i++ {
		resp, err := svc.Save(ctx, types.CreateEntryRequest{
			Type:      "task",
			Title:     "Bulk delete task",
			Content:   "content",
			Project:   project,
			FeatureID: featureID,
			Status:    "active",
		})
		if err != nil {
			t.Fatalf("seed task %d: %v", i, err)
		}
		paths = append(paths, resp.Path)
	}
	return paths
}

func TestBulkDelete_RequiresFilterOrPaths(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if _, err := svc.BulkDelete(ctx, types.BulkDeleteRequest{}); err == nil {
		t.Error("expected error when neither filter nor paths given")
	}

	feature := "f"
	_, err := svc.BulkDelete(ctx, types.BulkDeleteRequest{
		Filter: &types.BulkUpdateFilter{FeatureID: &feature},
		Paths:  []string{"projects/p/task/x.md"},
	})
	if err == nil {
		t.Error("expected error when both filter and paths given")
	}
}

func TestBulkDelete_FilterModeDeletesOnlyMatches(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	seedTasks(t, svc, "bd-proj", "feat-target", 3)
	keep := seedTasks(t, svc, "bd-proj", "feat-other", 2)

	featureID := "feat-target"
	resp, err := svc.BulkDelete(ctx, types.BulkDeleteRequest{
		Filter: &types.BulkUpdateFilter{FeatureID: &featureID},
	})
	if err != nil {
		t.Fatalf("BulkDelete: %v", err)
	}

	if resp.Deleted != 3 {
		t.Errorf("Deleted = %d, want 3", resp.Deleted)
	}
	if resp.Failed != 0 {
		t.Errorf("Failed = %d, want 0", resp.Failed)
	}

	// The non-matching feature must be untouched — a filter typo widening
	// the match is the failure mode that matters most on a delete.
	for _, p := range keep {
		if entry, err := svc.Recall(ctx, p); err != nil || entry == nil {
			t.Errorf("entry %s in feat-other was deleted; filter matched too broadly", p)
		}
	}
}

func TestBulkDelete_ExplicitPathsMode(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	paths := seedTasks(t, svc, "bd-explicit", "feat-x", 3)

	resp, err := svc.BulkDelete(ctx, types.BulkDeleteRequest{
		Paths: paths[:2],
	})
	if err != nil {
		t.Fatalf("BulkDelete: %v", err)
	}
	if resp.Deleted != 2 {
		t.Errorf("Deleted = %d, want 2", resp.Deleted)
	}

	if entry, err := svc.Recall(ctx, paths[2]); err != nil || entry == nil {
		t.Error("third entry should survive — it was not in the paths list")
	}
}

func TestBulkDelete_DryRunDeletesNothing(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	paths := seedTasks(t, svc, "bd-dry", "feat-dry", 3)

	featureID := "feat-dry"
	resp, err := svc.BulkDelete(ctx, types.BulkDeleteRequest{
		Filter: &types.BulkUpdateFilter{FeatureID: &featureID},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("BulkDelete: %v", err)
	}

	if !resp.DryRun {
		t.Error("DryRun flag not set on response")
	}
	if resp.Total != 3 {
		t.Errorf("Total = %d, want 3", resp.Total)
	}
	if resp.Deleted != 0 {
		t.Errorf("Deleted = %d, want 0 on a dry run", resp.Deleted)
	}

	for _, p := range paths {
		if entry, err := svc.Recall(ctx, p); err != nil || entry == nil {
			t.Errorf("entry %s was deleted during a dry run", p)
		}
	}
}

// A dry run must carry enough detail to render a confirmation list —
// a bare path is poor material for "you are about to delete these".
func TestBulkDelete_DryRunCarriesIDAndTitle(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if _, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:      "task",
		Title:     "Distinctive title",
		Content:   "c",
		Project:   "bd-detail",
		FeatureID: "feat-detail",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	featureID := "feat-detail"
	resp, err := svc.BulkDelete(ctx, types.BulkDeleteRequest{
		Filter: &types.BulkUpdateFilter{FeatureID: &featureID},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("BulkDelete: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("Results len = %d, want 1", len(resp.Results))
	}
	if resp.Results[0].Title != "Distinctive title" {
		t.Errorf("Title = %q, want %q", resp.Results[0].Title, "Distinctive title")
	}
	if resp.Results[0].ID == "" {
		t.Error("ID is empty; a confirmation list cannot identify the entry")
	}
}

// Deletion is not transactional. A missing path must be reported as a
// failed result while its siblings still get deleted — callers rely on the
// per-entry list to report partial failure honestly.
func TestBulkDelete_PartialFailureReportsPerEntry(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	paths := seedTasks(t, svc, "bd-partial", "feat-p", 2)
	targets := append([]string{"projects/bd-partial/task/does-not-exist.md"}, paths...)

	resp, err := svc.BulkDelete(ctx, types.BulkDeleteRequest{Paths: targets})
	if err != nil {
		t.Fatalf("BulkDelete returned a hard error; it should report per-entry: %v", err)
	}

	if resp.Deleted != 2 {
		t.Errorf("Deleted = %d, want 2", resp.Deleted)
	}
	if resp.Failed != 1 {
		t.Errorf("Failed = %d, want 1", resp.Failed)
	}

	var sawError bool
	for _, r := range resp.Results {
		if r.Status == "error" {
			sawError = true
			if r.Error == "" {
				t.Error("failed result carries no error message")
			}
		}
	}
	if !sawError {
		t.Error("no error result present despite Failed=1")
	}
}

// The 100-entry cap used to truncate silently on bulk update. Both bulk
// operations must now say so, or a caller "successfully" deleting a
// 120-task feature is left with 20 orphans and no indication why.
func TestBulkDelete_TruncationIsReported(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	seedTasks(t, svc, "bd-cap", "feat-cap", 12)

	featureID := "feat-cap"
	resp, err := svc.BulkDelete(ctx, types.BulkDeleteRequest{
		Filter: &types.BulkUpdateFilter{FeatureID: &featureID},
		DryRun: true,
		Limit:  5,
	})
	if err != nil {
		t.Fatalf("BulkDelete: %v", err)
	}

	if !resp.Truncated {
		t.Error("Truncated = false, want true when matches exceed the limit")
	}
	if resp.MatchedTotal != 12 {
		t.Errorf("MatchedTotal = %d, want 12", resp.MatchedTotal)
	}
	if resp.Total != 5 {
		t.Errorf("Total = %d, want 5 (the capped set)", resp.Total)
	}
}

func TestBulkDelete_NoTruncationWhenUnderLimit(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	seedTasks(t, svc, "bd-under", "feat-under", 3)

	featureID := "feat-under"
	resp, err := svc.BulkDelete(ctx, types.BulkDeleteRequest{
		Filter: &types.BulkUpdateFilter{FeatureID: &featureID},
		DryRun: true,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("BulkDelete: %v", err)
	}
	if resp.Truncated {
		t.Error("Truncated = true, want false when matches fit under the limit")
	}
}

// Explicit-paths mode has no matched population to under-report, so it
// must not claim truncation.
func TestBulkDelete_ExplicitModeNeverReportsTruncation(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	paths := seedTasks(t, svc, "bd-expl-trunc", "feat-e", 3)

	resp, err := svc.BulkDelete(ctx, types.BulkDeleteRequest{
		Paths:  paths,
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("BulkDelete: %v", err)
	}
	if resp.Truncated {
		t.Error("Truncated = true in explicit mode; there is no filter to truncate")
	}
	if resp.MatchedTotal != 0 {
		t.Errorf("MatchedTotal = %d, want 0 in explicit mode", resp.MatchedTotal)
	}
}

func TestBulkDelete_LimitClampedToHundred(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	seedTasks(t, svc, "bd-clamp", "feat-clamp", 3)

	featureID := "feat-clamp"
	resp, err := svc.BulkDelete(ctx, types.BulkDeleteRequest{
		Filter: &types.BulkUpdateFilter{FeatureID: &featureID},
		DryRun: true,
		Limit:  5000,
	})
	if err != nil {
		t.Fatalf("BulkDelete: %v", err)
	}
	// Nothing to assert beyond "it did not blow past the cap": with only 3
	// entries the clamp is invisible in the count, but an unclamped limit
	// would have been passed straight to storage.
	if resp.Total != 3 {
		t.Errorf("Total = %d, want 3", resp.Total)
	}
}

// The same truncation signal must reach bulk UPDATE, which is what the
// feature-wide status change uses.
func TestBulkUpdate_TruncationIsReported(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	seedTasks(t, svc, "bu-cap", "feat-bucap", 12)

	featureID := "feat-bucap"
	resp, err := svc.BulkUpdate(ctx, types.BulkUpdateRequest{
		Filter:  &types.BulkUpdateFilter{FeatureID: &featureID},
		Updates: &types.UpdateEntryRequest{Status: strPtr("cancelled")},
		DryRun:  true,
		Limit:   5,
	})
	if err != nil {
		t.Fatalf("BulkUpdate: %v", err)
	}
	if !resp.Truncated {
		t.Error("Truncated = false, want true")
	}
	if resp.MatchedTotal != 12 {
		t.Errorf("MatchedTotal = %d, want 12", resp.MatchedTotal)
	}
}

func TestBulkUpdate_ExplicitModeNeverReportsTruncation(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	paths := seedTasks(t, svc, "bu-expl", "feat-buexpl", 2)
	entries := make([]types.BulkUpdateEntry, 0, len(paths))
	for _, p := range paths {
		entries = append(entries, types.BulkUpdateEntry{
			Path:    p,
			Updates: types.UpdateEntryRequest{Status: strPtr("cancelled")},
		})
	}

	resp, err := svc.BulkUpdate(ctx, types.BulkUpdateRequest{Entries: entries})
	if err != nil {
		t.Fatalf("BulkUpdate: %v", err)
	}
	if resp.Truncated {
		t.Error("Truncated = true in explicit mode")
	}
}

package service

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// newTestBrainAndTaskService builds a BrainServiceImpl and a TaskServiceImpl
// over one shared store and brain dir, so an entry written through Save is
// readable through GetTasks.
//
// Sharing the backing store is the whole point: tests that construct
// types.ResolvedTask literals cannot observe the frontmatter → index →
// metadata JSON → BrainEntry → ResolvedTask hops, which is where checkout_mode
// used to be dropped.
func newTestBrainAndTaskService(t *testing.T) (*BrainServiceImpl, *TaskServiceImpl) {
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

	return NewBrainService(cfg, store, idx, nil, nil), NewTaskService(cfg, store, idx)
}

// TestCheckoutMode_SurvivesStorageRoundTrip is the regression test for the
// write-only checkout_mode field.
//
// The value was emitted into frontmatter and indexed into the notes metadata
// JSON correctly, but neither parseMetadataIntoEntry (BrainEntry) nor
// brainEntryToResolvedTask (ResolvedTask) read it back, so every consumer
// downstream of storage saw "". foldCheckoutMode therefore always returned
// "ai" and the simple squash-merge automation was unreachable.
func TestCheckoutMode_SurvivesStorageRoundTrip(t *testing.T) {
	tests := []struct {
		name         string
		checkoutMode string
		want         string
	}{
		{name: "simple persists", checkoutMode: "simple", want: "simple"},
		{name: "ai persists", checkoutMode: "ai", want: "ai"},
		{name: "empty stays empty", checkoutMode: "", want: ""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			brain, taskSvc := newTestBrainAndTaskService(t)
			ctx := context.Background()

			saved, err := brain.Save(ctx, types.CreateEntryRequest{
				Type:         "task",
				Title:        "Round trip task",
				Content:      "body",
				Status:       "pending",
				Project:      "brain",
				FeatureID:    "feat-roundtrip",
				CheckoutMode: tc.checkoutMode,
			})
			if err != nil {
				t.Fatalf("Save: %v", err)
			}

			// Hop 1: metadata JSON → BrainEntry.CheckoutMode.
			entry, err := brain.Recall(ctx, saved.Path)
			if err != nil {
				t.Fatalf("Recall: %v", err)
			}
			if entry.CheckoutMode != tc.want {
				t.Errorf("BrainEntry.CheckoutMode = %q, want %q", entry.CheckoutMode, tc.want)
			}

			// Hop 2: BrainEntry → ResolvedTask.CheckoutMode.
			resolved, err := taskSvc.GetTasks(ctx, "brain")
			if err != nil {
				t.Fatalf("GetTasks: %v", err)
			}
			if len(resolved.Tasks) != 1 {
				t.Fatalf("expected 1 resolved task, got %d", len(resolved.Tasks))
			}
			if got := resolved.Tasks[0].CheckoutMode; got != tc.want {
				t.Errorf("ResolvedTask.CheckoutMode = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestFoldCheckoutMode_ReadsPersistedTasks pins the fold against tasks that
// actually went through storage, rather than struct literals.
//
// The nine TestFoldCheckoutMode_* unit tests cover the fold's own logic, but
// they build ResolvedTask values by hand and so cannot detect a broken read
// path feeding it. This test can: if any hop drops checkout_mode, every case
// below collapses to "ai".
func TestFoldCheckoutMode_ReadsPersistedTasks(t *testing.T) {
	tests := []struct {
		name  string
		modes []string
		want  string
	}{
		{name: "all simple folds to simple", modes: []string{"simple", "simple"}, want: "simple"},
		{name: "one simple wins over ai", modes: []string{"ai", "simple"}, want: "simple"},
		{name: "one simple wins over empty", modes: []string{"", "simple"}, want: "simple"},
		{name: "all ai folds to ai", modes: []string{"ai", "ai"}, want: "ai"},
		{name: "all empty folds to ai", modes: []string{"", ""}, want: "ai"},
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
					FeatureID:    "feat-fold",
					CheckoutMode: mode,
				}); err != nil {
					t.Fatalf("seed task %d (mode %q): %v", i, mode, err)
				}
			}

			tasks, err := taskSvc.GetTasksByFeature(ctx, "brain", "feat-fold")
			if err != nil {
				t.Fatalf("GetTasksByFeature: %v", err)
			}
			if len(tasks) != len(tc.modes) {
				t.Fatalf("expected %d feature tasks, got %d", len(tc.modes), len(tasks))
			}

			if got := foldCheckoutMode(tasks); got != tc.want {
				t.Errorf("foldCheckoutMode(persisted tasks) = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCheckoutFeature_PersistsCheckoutMode covers the second write path.
//
// FeatureCheckoutOptions.CheckoutMode is accepted by the MCP feature_checkout
// tool and by POST /tasks/{project}/features/{id}/checkout, but
// normalizeFeatureCheckoutOptions used to drop the field and CheckoutFeature
// never wrote it to frontmatter — so the caller's choice was silently
// discarded and the resulting checkout task always folded to "ai".
func TestCheckoutFeature_PersistsCheckoutMode(t *testing.T) {
	tests := []struct {
		name         string
		checkoutMode string
		want         string
	}{
		{name: "simple is persisted", checkoutMode: "simple", want: "simple"},
		{name: "ai is persisted", checkoutMode: "ai", want: "ai"},
		{name: "empty is omitted", checkoutMode: "", want: ""},
		{name: "surrounding whitespace is trimmed", checkoutMode: "  simple  ", want: "simple"},
		{name: "unrecognized mode is dropped, not persisted", checkoutMode: "bogus", want: ""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			svc, _, brainDir := newTestTaskService(t)
			ctx := context.Background()

			result, err := svc.CheckoutFeature(ctx, "brain", "feat-checkout", &types.FeatureCheckoutOptions{
				CheckoutMode:      tc.checkoutMode,
				MergeTargetBranch: "main",
			})
			if err != nil {
				t.Fatalf("CheckoutFeature: %v", err)
			}
			if !result.Created {
				t.Fatalf("expected checkout task to be created")
			}

			raw := readCheckoutTaskFile(t, brainDir, "brain")
			if tc.want == "" {
				if strings.Contains(raw, "checkout_mode:") {
					t.Errorf("expected no checkout_mode line in frontmatter, got:\n%s", raw)
				}
				return
			}
			if !strings.Contains(raw, "checkout_mode: "+tc.want) {
				t.Errorf("expected %q in frontmatter, got:\n%s", "checkout_mode: "+tc.want, raw)
			}
		})
	}
}

// readCheckoutTaskFile returns the contents of the single task markdown file
// CheckoutFeature wrote for the project.
func readCheckoutTaskFile(t *testing.T, brainDir, project string) string {
	t.Helper()

	dir := filepath.Join(brainDir, "projects", project, "task")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read task dir %s: %v", dir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 checkout task file in %s, got %d", dir, len(entries))
	}

	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read task file: %v", err)
	}
	return string(data)
}

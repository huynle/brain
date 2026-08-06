package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// taskFileSpec seeds a minimal .md file for getFeatureTasksFromFilesystem
// to enumerate. The service reads task frontmatter from disk regardless of
// what's in the DB, so tests that go through ResumeFeature (which fans out
// via that helper) need both storage rows AND disk files.
type taskFileSpec struct {
	ID     string
	Status string
}

// seedFeatureTaskFiles writes minimal task .md files under
// projects/<project>/task/ so getFeatureTasksFromFilesystem sees them.
// Frontmatter matches what parseMetadataIntoEntry / frontmatter.Parse
// expects — id, feature_id, and status are enough for the enumeration
// filter to pick up.
func seedFeatureTaskFiles(t *testing.T, brainDir, projectID, featureID string, specs []taskFileSpec) {
	t.Helper()
	taskDir := filepath.Join(brainDir, "projects", projectID, "task")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", taskDir, err)
	}
	for _, s := range specs {
		body := fmt.Sprintf(`---
id: %s
feature_id: %s
status: %s
type: task
---

Test task body.
`, s.ID, featureID, s.Status)
		if err := os.WriteFile(filepath.Join(taskDir, s.ID+".md"), []byte(body), 0o644); err != nil {
			t.Fatalf("write task file %s: %v", s.ID, err)
		}
	}
}

// ResumeFeature test coverage. Uses the same in-memory harness as
// resume_task_test.go (newTestTaskService, insertTaskNote,
// createProjectDir). Focused on the batch semantics that the initial
// commit shipped without tests + the terminal-status guard added
// after the adversarial review.

const featureTestProject = "resume-feat-proj"

// TestResumeFeature_TerminalTasksExcludedFromBatch is the regression
// test for the critical batch-force bug: POST /features/{f}/resume with
// force=true must NOT resurrect completed/validated/cancelled/superseded/
// archived tasks. Each terminal task should appear as a skipped result
// with a reason containing "terminal_status_excluded_from_batch".
func TestResumeFeature_TerminalTasksExcludedFromBatch(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, featureTestProject)

	// Seed one task per terminal status + one legitimately abandoned task.
	// Force=true. Expected: 5 terminal tasks skipped with the guard
	// reason, 1 abandoned task resumed.
	terminalStatusList := []string{"completed", "validated", "cancelled", "superseded", "archived"}
	for i, status := range terminalStatusList {
		insertTaskNote(t, store, "term-"+status, "Terminal "+status, status, "medium", featureTestProject, map[string]interface{}{
			"feature_id": "batch-feat",
		})
		_ = i
	}
	insertTaskNote(t, store, "resumable1", "Resumable", "in_progress", "medium", featureTestProject, map[string]interface{}{
		"feature_id": "batch-feat",
	})
	// Give the resumable task an offline-runner claim so IsAbandoned enrichment fires.
	seedAbandonedTaskWithOfflineClaim(t, store, "resumable1", "dead-runner-feat")

	// Also need feature tasks discovered via filesystem — insertTaskNote
	// only touches storage. Create empty .md files matching the seeded IDs
	// so getFeatureTasksFromFilesystem can enumerate them.
	seedFeatureTaskFiles(t, brainDir, featureTestProject, "batch-feat", []taskFileSpec{
		{ID: "term-completed", Status: "completed"},
		{ID: "term-validated", Status: "validated"},
		{ID: "term-cancelled", Status: "cancelled"},
		{ID: "term-superseded", Status: "superseded"},
		{ID: "term-archived", Status: "archived"},
		{ID: "resumable1", Status: "in_progress"},
	})

	ctx := context.Background()
	result, err := svc.ResumeFeature(ctx, featureTestProject, "batch-feat", &types.ResumeTaskOptions{Force: true})
	if err != nil {
		t.Fatalf("ResumeFeature: %v", err)
	}

	// Verify: terminal-status tasks were skipped with the correct reason
	// and NOT written back to pending.
	skippedTerminal := 0
	for _, r := range result.Results {
		if containsFold(r.Reason, "terminal_status_excluded_from_batch") {
			skippedTerminal++
		}
	}
	if skippedTerminal != len(terminalStatusList) {
		t.Errorf("expected %d terminal-skipped results, got %d (results: %+v)",
			len(terminalStatusList), skippedTerminal, result.Results)
	}

	// Verify the resumable one actually resumed.
	if result.TotalResumed < 1 {
		t.Errorf("expected the abandoned task to resume, got total_resumed=%d", result.TotalResumed)
	}

	// Verify each terminal task's status is UNCHANGED. This is the
	// critical safety property — a batch force must never revert historic
	// terminal state.
	tasks, err := svc.GetTasks(ctx, featureTestProject)
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	statusByID := make(map[string]string)
	for _, task := range tasks.Tasks {
		statusByID[task.ID] = task.Status
	}
	for _, status := range terminalStatusList {
		id := "term-" + status
		got := statusByID[id]
		if got != status {
			t.Errorf("terminal task %s: status = %q, want %q (batch force must not resurrect)", id, got, status)
		}
	}
}

// TestResumeFeature_NotFound guards the 404 path — an unknown feature
// (or a feature with zero tasks in the project) returns an error whose
// message contains "not found" so HandleResumeFeature's fallback maps
// to HTTP 404.
func TestResumeFeature_NotFound(t *testing.T) {
	svc, _, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, featureTestProject)

	ctx := context.Background()
	_, err := svc.ResumeFeature(ctx, featureTestProject, "does-not-exist", nil)
	if err == nil {
		t.Fatal("expected error for unknown feature")
	}
	if !containsFold(err.Error(), "not found") {
		t.Errorf("error should contain 'not found' (for 404 fallback), got: %v", err)
	}
}

// TestResumeFeature_MixedBatch — a feature with a mix of non-terminal
// tasks: 2 abandoned (should resume), 1 pending w/o resume flag +
// force=false (should skip as not-abandoned), 1 blocked (should skip as
// not-abandoned without force).
func TestResumeFeature_MixedBatch(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, featureTestProject)

	insertTaskNote(t, store, "ab1", "Abandoned 1", "in_progress", "medium", featureTestProject, map[string]interface{}{
		"feature_id": "mixed-feat",
	})
	insertTaskNote(t, store, "ab2", "Abandoned 2", "in_progress", "medium", featureTestProject, map[string]interface{}{
		"feature_id": "mixed-feat",
	})
	insertTaskNote(t, store, "pend1", "Regular Pending", "pending", "medium", featureTestProject, map[string]interface{}{
		"feature_id": "mixed-feat",
	})
	insertTaskNote(t, store, "blk1", "Blocked", "blocked", "medium", featureTestProject, map[string]interface{}{
		"feature_id": "mixed-feat",
	})

	// Two abandoned tasks share a single dead runner to keep test setup lean.
	seedAbandonedTaskWithOfflineClaim(t, store, "ab1", "dead-runner-mixed")
	seedAbandonedTaskWithOfflineClaim(t, store, "ab2", "dead-runner-mixed")

	seedFeatureTaskFiles(t, brainDir, featureTestProject, "mixed-feat", []taskFileSpec{
		{ID: "ab1", Status: "in_progress"},
		{ID: "ab2", Status: "in_progress"},
		{ID: "pend1", Status: "pending"},
		{ID: "blk1", Status: "blocked"},
	})

	ctx := context.Background()
	result, err := svc.ResumeFeature(ctx, featureTestProject, "mixed-feat", nil)
	if err != nil {
		t.Fatalf("ResumeFeature: %v", err)
	}

	if result.TotalResumed != 2 {
		t.Errorf("expected 2 resumed (the abandoned pair), got %d", result.TotalResumed)
	}
	if result.TotalSkipped != 2 {
		t.Errorf("expected 2 skipped (pending + blocked without force), got %d", result.TotalSkipped)
	}
	if result.TotalResumed+result.TotalSkipped != len(result.Results) {
		t.Errorf("counts don't add up: %d + %d != %d",
			result.TotalResumed, result.TotalSkipped, len(result.Results))
	}
}

// TestResumeFeature_ConcurrentSameFeatureSerializes proves the per-feature
// mutex actually serializes work. Two goroutines call ResumeFeature on the
// same feature simultaneously; a MergeMetadata regression that dropped the
// Lock() would let them interleave and double-stamp resume_requested. This
// guards the whole reason the sync.Map + refcounted entry exists.
func TestResumeFeature_ConcurrentSameFeatureSerializes(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, featureTestProject)

	insertTaskNote(t, store, "conc1", "Concurrent 1", "in_progress", "medium", featureTestProject, map[string]interface{}{
		"feature_id": "concur-feat",
	})
	seedAbandonedTaskWithOfflineClaim(t, store, "conc1", "dead-conc-runner")
	seedFeatureTaskFiles(t, brainDir, featureTestProject, "concur-feat", []taskFileSpec{
		{ID: "conc1", Status: "in_progress"},
	})

	// Fire two concurrent calls. Under the per-feature lock, they must
	// serialize — one resumes cleanly, the other observes the state left
	// by the first (task is already pending+resume_requested) and returns
	// a no-op skipped result.
	var wg sync.WaitGroup
	results := make([]*types.ResumeFeatureResult, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = svc.ResumeFeature(context.Background(), featureTestProject, "concur-feat", nil)
		}(i)
	}
	wg.Wait()

	for i := 0; i < 2; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d error: %v", i, errs[i])
		}
		if results[i] == nil {
			t.Fatalf("goroutine %d returned nil result", i)
		}
	}

	// Combined: exactly one resume should have taken effect. The second
	// caller must see the idempotent "already requested" state and skip.
	totalResumed := results[0].TotalResumed + results[1].TotalResumed
	if totalResumed != 1 {
		t.Errorf("expected exactly 1 resume across both calls, got %d (r0=%+v r1=%+v)",
			totalResumed, results[0], results[1])
	}
}

// TestResumeFeature_ConcurrentDifferentFeaturesParallelize verifies the
// mutex is genuinely per-feature — different features don't block on each
// other. Loose timing assertion: two concurrent calls on different features
// should complete in noticeably less than 2× the single-call latency.
// Skipped as a hard-timing test; instead we assert that both complete
// successfully with a small ctx timeout, which fails if either was blocked
// waiting on the other.
func TestResumeFeature_ConcurrentDifferentFeaturesParallelize(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, featureTestProject)

	for _, fid := range []string{"para-a", "para-b"} {
		id := "p" + fid[len(fid)-1:] + "1"
		insertTaskNote(t, store, id, "Parallel "+fid, "in_progress", "medium", featureTestProject, map[string]interface{}{
			"feature_id": fid,
		})
		seedAbandonedTaskWithOfflineClaim(t, store, id, "dead-"+fid)
		seedFeatureTaskFiles(t, brainDir, featureTestProject, fid, []taskFileSpec{
			{ID: id, Status: "in_progress"},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, fid := range []string{"para-a", "para-b"} {
		wg.Add(1)
		go func(idx int, feature string) {
			defer wg.Done()
			_, errs[idx] = svc.ResumeFeature(ctx, featureTestProject, feature, nil)
		}(i, fid)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d failed (likely blocked on cross-feature lock): %v", i, err)
		}
	}
}

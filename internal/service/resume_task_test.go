package service

import (
	"context"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// ResumeTask branch coverage. Each test seeds a minimal task + claim + runner
// state, calls ResumeTask, and asserts the response shape plus the observable
// side effects. Uses the same in-memory storage harness as the Trigger tests
// (see newTestTaskService in task_test.go) so no filesystem or SQLite-on-disk
// dependencies leak in.

const resumeTestProject = "resume-proj"

// seedAbandonedTaskWithOfflineClaim sets up a task that enrichAbandonmentState
// will classify as abandoned via the runner_offline signal:
//   - task status=in_progress
//   - task_claims row exists, not yet expired
//   - the runner holding that claim has runners.status=offline
func seedAbandonedTaskWithOfflineClaim(t *testing.T, store *storage.StorageLayer, taskID, runnerID string) {
	t.Helper()
	insertTaskNote(t, store, taskID, "Abandoned Task", "in_progress", "medium", resumeTestProject, map[string]interface{}{})

	// Register the runner as offline. Use a heartbeat well past the stale
	// threshold so the lifecycle sweep would have marked it offline anyway.
	now := time.Now().UnixMilli()
	if err := store.UpsertRunner(context.Background(), &storage.RunnerRow{
		RunnerID:      runnerID,
		Hostname:      runnerID + "-host",
		Labels:        map[string]string{},
		Executors:     []string{"opencode"},
		Capabilities:  []string{},
		MaxParallel:   1,
		RegisteredAt:  now - int64(30*time.Minute/time.Millisecond),
		LastHeartbeat: now - int64(30*time.Minute/time.Millisecond),
		Status:        "offline",
	}); err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	// Fresh, unexpired claim held by the offline runner. This is the "runner
	// died mid-task, lease not yet naturally expired" case.
	ok, _, err := store.ClaimTask(context.Background(), resumeTestProject, taskID, runnerID, 10*time.Minute)
	if err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}
	if !ok {
		t.Fatal("ClaimTask should have succeeded on a fresh task")
	}
}

// TestResumeTask_HappyPath — abandoned task (offline-runner claim), no force,
// expected: Resumed=true, status flipped to pending, resume_requested stamped.
func TestResumeTask_HappyPath(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, resumeTestProject)
	seedAbandonedTaskWithOfflineClaim(t, store, "abnd0001", "dead-runner")

	ctx := context.Background()
	result, err := svc.ResumeTask(ctx, resumeTestProject, "abnd0001", nil)
	if err != nil {
		t.Fatalf("ResumeTask: %v", err)
	}
	if !result.Resumed {
		t.Fatalf("expected Resumed=true, got %+v", result)
	}
	if result.PriorStatus != "in_progress" {
		t.Errorf("PriorStatus = %q, want in_progress", result.PriorStatus)
	}
	if result.AbandonReason != AbandonReasonRunnerOffline {
		t.Errorf("AbandonReason = %q, want %q", result.AbandonReason, AbandonReasonRunnerOffline)
	}

	// Verify side effects: status now pending, resume_requested stamped.
	tasks, err := svc.GetTasks(ctx, resumeTestProject)
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	var found *types.ResolvedTask
	for i := range tasks.Tasks {
		if tasks.Tasks[i].ID == "abnd0001" {
			found = &tasks.Tasks[i]
			break
		}
	}
	if found == nil {
		t.Fatal("task not found after resume")
	}
	if found.Status != "pending" {
		t.Errorf("post-resume status = %q, want pending", found.Status)
	}
	if !found.ResumeRequested {
		t.Error("post-resume resume_requested = false, want true")
	}
	// After resume, IsAbandoned should be false (status is now pending, not
	// in_progress or blocked).
	if found.IsAbandoned {
		t.Error("post-resume task still shows IsAbandoned=true")
	}
}

// TestResumeTask_IdempotentReplay — resume a task already in pending+
// resume_requested=true state. Should be a no-op with Resumed=false.
func TestResumeTask_IdempotentReplay(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, resumeTestProject)
	insertTaskNote(t, store, "already1", "Already Resumed", "pending", "medium", resumeTestProject, map[string]interface{}{
		"resume_requested": true,
	})

	ctx := context.Background()
	result, err := svc.ResumeTask(ctx, resumeTestProject, "already1", nil)
	if err != nil {
		t.Fatalf("ResumeTask: %v", err)
	}
	if result.Resumed {
		t.Errorf("expected Resumed=false for idempotent replay, got %+v", result)
	}
	if result.Reason == "" {
		t.Error("expected non-empty Reason on idempotent no-op")
	}
}

// TestResumeTask_StuckPendingUnstuck — pending task WITHOUT resume_requested
// (e.g. auto-reset via claim-renewal-fail path). With force=true, we should
// fall through the pending-split and successfully stamp resume_requested.
func TestResumeTask_StuckPendingUnstuck(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, resumeTestProject)
	insertTaskNote(t, store, "stuck001", "Stuck Pending", "pending", "medium", resumeTestProject, map[string]interface{}{})

	ctx := context.Background()
	result, err := svc.ResumeTask(ctx, resumeTestProject, "stuck001", &types.ResumeTaskOptions{Force: true})
	if err != nil {
		t.Fatalf("ResumeTask: %v", err)
	}
	if !result.Resumed {
		t.Fatalf("expected Resumed=true with force on stuck-pending, got %+v (reason=%s)", result, result.Reason)
	}
}

// TestResumeTask_NotAbandonedNoForce — pending task without the flag AND no
// force → refuse cleanly (Resumed=false with a reason).
func TestResumeTask_NotAbandonedNoForce(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, resumeTestProject)
	insertTaskNote(t, store, "notabnd1", "Regular Pending", "active", "medium", resumeTestProject, map[string]interface{}{})

	ctx := context.Background()
	result, err := svc.ResumeTask(ctx, resumeTestProject, "notabnd1", nil)
	if err != nil {
		t.Fatalf("ResumeTask: %v", err)
	}
	if result.Resumed {
		t.Errorf("expected Resumed=false without force, got %+v", result)
	}
	if result.Reason == "" {
		t.Error("expected non-empty Reason on refused resume")
	}
}

// TestResumeTask_TerminalStatusGate — completed task without force → refuse.
// The reason should mention "terminal" so users know to use Trigger instead.
func TestResumeTask_TerminalStatusGate(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, resumeTestProject)
	insertTaskNote(t, store, "term0001", "Completed Task", "completed", "medium", resumeTestProject, map[string]interface{}{})

	ctx := context.Background()
	result, err := svc.ResumeTask(ctx, resumeTestProject, "term0001", nil)
	if err != nil {
		t.Fatalf("ResumeTask: %v", err)
	}
	if result.Resumed {
		t.Errorf("expected Resumed=false on terminal status, got %+v", result)
	}
}

// TestResumeTask_LiveClaimSafety — force=true against a task whose current
// claim is held by an ONLINE runner. Should refuse (protecting against
// double-execution) even with force.
func TestResumeTask_LiveClaimSafety(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, resumeTestProject)
	insertTaskNote(t, store, "live0001", "Live Task", "in_progress", "medium", resumeTestProject, map[string]interface{}{})

	// Register a LIVE runner and give it the claim.
	insertRunnerForTaskSelectionTest(t, store, "live-runner", []string{"opencode"}, nil)
	ok, _, err := store.ClaimTask(context.Background(), resumeTestProject, "live0001", "live-runner", 10*time.Minute)
	if err != nil || !ok {
		t.Fatalf("ClaimTask setup failed: err=%v ok=%v", err, ok)
	}

	ctx := context.Background()
	result, err := svc.ResumeTask(ctx, resumeTestProject, "live0001", &types.ResumeTaskOptions{Force: true})
	if err != nil {
		t.Fatalf("ResumeTask: %v", err)
	}
	if result.Resumed {
		t.Errorf("expected Resumed=false when live runner holds claim (even with force), got %+v", result)
	}
	if result.Reason == "" {
		t.Error("expected explanatory Reason mentioning live claim")
	}

	// The live runner's claim should still be intact — no nuke happened.
	claim, err := store.GetClaim(context.Background(), resumeTestProject, "live0001")
	if err != nil {
		t.Fatalf("GetClaim: %v", err)
	}
	if claim == nil {
		t.Fatal("live runner's claim was deleted (nuke race regression)")
	}
	if claim.RunnerID != "live-runner" {
		t.Errorf("claim runner_id = %q, want live-runner", claim.RunnerID)
	}
}

// TestResumeTask_NotFound — unknown taskId returns an error whose message
// contains "not found" so HandleResumeTask's fallback maps to HTTP 404.
func TestResumeTask_NotFound(t *testing.T) {
	svc, _, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, resumeTestProject)

	ctx := context.Background()
	_, err := svc.ResumeTask(ctx, resumeTestProject, "does-not-exist", nil)
	if err == nil {
		t.Fatal("expected error for unknown task ID")
	}
	// The handler's 404 fallback uses substring "not found" from either the
	// canonical ErrNotFound or the plain error text — this test guards the
	// text so a rename doesn't silently break the handler mapping.
	if !containsFold(err.Error(), "not found") {
		t.Errorf("error should contain 'not found' (for 404 fallback), got: %v", err)
	}
}

// containsFold is a tiny case-insensitive substring check so this file
// doesn't need to import strings. Keeps the test self-contained.
func containsFold(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(sub) > len(s) {
		return false
	}
	// Lowercase compare — sufficient for ASCII test-fixture inputs.
	lower := func(b byte) byte {
		if 'A' <= b && b <= 'Z' {
			return b + ('a' - 'A')
		}
		return b
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		ok := true
		for j := 0; j < len(sub); j++ {
			if lower(s[i+j]) != lower(sub[j]) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

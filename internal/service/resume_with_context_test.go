package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// ResumeTaskWithContext branch coverage. Mirrors resume_task_test.go's harness
// (newTestTaskService, insertTaskNote, seedAbandonedTaskWithOfflineClaim) but
// exercises the context-injection decision tree: live-inject vs relaunch,
// same_session vs rehydrate mode, and the extended metadata stamp.

// fakeLiveInjector is a test double for the service LiveInjector dependency.
type fakeLiveInjector struct {
	injected   bool
	sessionID  string
	err        error
	calledWith string // captured injectedContext
	calledTask string
	callCount  int
}

func (f *fakeLiveInjector) InjectContext(_ context.Context, _ /*projectID*/, taskID, injectedContext string) (bool, string, error) {
	f.callCount++
	f.calledWith = injectedContext
	f.calledTask = taskID
	return f.injected, f.sessionID, f.err
}

// readTaskMeta reads back the raw metadata JSON for a task note so tests can
// assert the extended resume-with-context stamps landed.
func readTaskMeta(t *testing.T, store *storage.TenantStore, projectID, taskID string) map[string]interface{} {
	t.Helper()
	path := "projects/" + projectID + "/task/" + taskID + ".md"
	row, err := store.GetNoteByPath(context.Background(), path)
	if err != nil {
		t.Fatalf("GetNote(%s): %v", path, err)
	}
	if row == nil {
		t.Fatalf("note %s not found", path)
	}
	meta := map[string]interface{}{}
	if row.Metadata != "" {
		if err := json.Unmarshal([]byte(row.Metadata), &meta); err != nil {
			t.Fatalf("unmarshal metadata: %v", err)
		}
	}
	return meta
}

// seedAbandonedOpencodeTaskWithSession mirrors seedAbandonedTaskWithOfflineClaim
// but stamps an executor + a stored prior session id, so the same_session
// intent can fire.
func seedAbandonedOpencodeTaskWithSession(t *testing.T, store *storage.TenantStore, taskID, runnerID, sessionID string) {
	t.Helper()
	insertTaskNote(t, store, taskID, "Abandoned OC Task", "in_progress", "medium", resumeTestProject, map[string]interface{}{
		"executor": "opencode",
		"sessions": map[string]interface{}{
			sessionID: map[string]interface{}{
				"timestamp": time.Now().UTC().Format(time.RFC3339),
				"runner_id": runnerID,
			},
		},
	})

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
	ok, _, err := store.ClaimTask(context.Background(), resumeTestProject, taskID, runnerID, 10*time.Minute)
	if err != nil || !ok {
		t.Fatalf("ClaimTask setup failed: err=%v ok=%v", err, ok)
	}
}

// TestResumeTaskWithContext_EmptyContext — empty injected_context must be
// rejected with an error (handler maps to 400).
func TestResumeTaskWithContext_EmptyContext(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, resumeTestProject)
	seedAbandonedTaskWithOfflineClaim(t, store, "empty001", "dead-runner")

	_, err := svc.ResumeTaskWithContext(context.Background(), resumeTestProject, "empty001",
		&types.ResumeWithContextOptions{InjectedContext: ""})
	if err == nil {
		t.Fatal("expected error for empty injected_context")
	}
}

// TestResumeTaskWithContext_NotAbandonedNoForce — not-abandoned + no force →
// Resumed=false with a Reason (gate reused from ResumeTask).
func TestResumeTaskWithContext_NotAbandonedNoForce(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, resumeTestProject)
	insertTaskNote(t, store, "notabnd2", "Regular Active", "active", "medium", resumeTestProject, map[string]interface{}{})

	result, err := svc.ResumeTaskWithContext(context.Background(), resumeTestProject, "notabnd2",
		&types.ResumeWithContextOptions{InjectedContext: "ctx"})
	if err != nil {
		t.Fatalf("ResumeTaskWithContext: %v", err)
	}
	if result.Resumed {
		t.Errorf("expected Resumed=false without force, got %+v", result)
	}
	if result.Reason == "" {
		t.Error("expected non-empty Reason on refused resume")
	}
}

// TestResumeTaskWithContext_RelaunchSameSession — abandoned opencode task with
// a stored session + prefer_same_session → Resumed=true, ResumeMode=same_session,
// TargetSessionID set, and the extended metadata is stamped.
func TestResumeTaskWithContext_RelaunchSameSession(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, resumeTestProject)
	seedAbandonedOpencodeTaskWithSession(t, store, "sess0001", "dead-runner", "ses-abc")

	result, err := svc.ResumeTaskWithContext(context.Background(), resumeTestProject, "sess0001",
		&types.ResumeWithContextOptions{InjectedContext: "supervisor context here", PreferSameSession: true})
	if err != nil {
		t.Fatalf("ResumeTaskWithContext: %v", err)
	}
	if !result.Resumed {
		t.Fatalf("expected Resumed=true, got %+v", result)
	}
	if result.ResumeMode != types.ResumeModeSameSession {
		t.Errorf("ResumeMode = %q, want %q", result.ResumeMode, types.ResumeModeSameSession)
	}
	if result.TargetSessionID != "ses-abc" {
		t.Errorf("TargetSessionID = %q, want ses-abc", result.TargetSessionID)
	}
	if result.InjectedLive {
		t.Error("InjectedLive should be false on relaunch path")
	}

	meta := readTaskMeta(t, store, resumeTestProject, "sess0001")
	if s, _ := metaString(meta, "status"); s != "pending" {
		t.Errorf("status metadata = %q, want pending", s)
	}
	if b, _ := metaBool(meta, "resume_requested"); !b {
		t.Error("resume_requested metadata not stamped")
	}
	if s, _ := metaString(meta, "resume_injected_context"); s != "supervisor context here" {
		t.Errorf("resume_injected_context = %q, want it stamped", s)
	}
	if b, _ := metaBool(meta, "resume_prefer_same_session"); !b {
		t.Error("resume_prefer_same_session metadata not stamped true")
	}
	if s, _ := metaString(meta, "resume_mode"); s != types.ResumeModeSameSession {
		t.Errorf("resume_mode metadata = %q, want same_session", s)
	}
}

// TestResumeTaskWithContext_RehydrateWhenNoSession — abandoned opencode task
// with NO stored session + prefer_same_session=true → mode falls to rehydrate.
func TestResumeTaskWithContext_RehydrateWhenNoSession(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, resumeTestProject)
	// No sessions stored → same_session intent cannot be honored.
	insertTaskNote(t, store, "rehy0001", "No Session Task", "in_progress", "medium", resumeTestProject, map[string]interface{}{
		"executor": "opencode",
	})
	seedOfflineClaim(t, store, "rehy0001", "dead-runner")

	result, err := svc.ResumeTaskWithContext(context.Background(), resumeTestProject, "rehy0001",
		&types.ResumeWithContextOptions{InjectedContext: "ctx", PreferSameSession: true})
	if err != nil {
		t.Fatalf("ResumeTaskWithContext: %v", err)
	}
	if !result.Resumed {
		t.Fatalf("expected Resumed=true, got %+v", result)
	}
	if result.ResumeMode != types.ResumeModeRehydrate {
		t.Errorf("ResumeMode = %q, want rehydrate (no stored session)", result.ResumeMode)
	}
}

// TestResumeTaskWithContext_RehydrateWhenPreferFalse — prefer_same_session=false
// forces rehydrate even with a stored session.
func TestResumeTaskWithContext_RehydrateWhenPreferFalse(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, resumeTestProject)
	seedAbandonedOpencodeTaskWithSession(t, store, "rehy0002", "dead-runner", "ses-xyz")

	result, err := svc.ResumeTaskWithContext(context.Background(), resumeTestProject, "rehy0002",
		&types.ResumeWithContextOptions{InjectedContext: "ctx", PreferSameSession: false})
	if err != nil {
		t.Fatalf("ResumeTaskWithContext: %v", err)
	}
	if result.ResumeMode != types.ResumeModeRehydrate {
		t.Errorf("ResumeMode = %q, want rehydrate (prefer_same_session=false)", result.ResumeMode)
	}
}

// TestResumeTaskWithContext_RehydrateForPiExecutor — a Pi task cannot reuse an
// opencode session → rehydrate regardless of prefer_same_session.
func TestResumeTaskWithContext_RehydrateForPiExecutor(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, resumeTestProject)
	insertTaskNote(t, store, "pi000001", "Pi Task", "in_progress", "medium", resumeTestProject, map[string]interface{}{
		"executor": "pi",
		"sessions": map[string]interface{}{
			"ses-pi": map[string]interface{}{"timestamp": time.Now().UTC().Format(time.RFC3339)},
		},
	})
	seedOfflineClaim(t, store, "pi000001", "dead-runner")

	result, err := svc.ResumeTaskWithContext(context.Background(), resumeTestProject, "pi000001",
		&types.ResumeWithContextOptions{InjectedContext: "ctx", PreferSameSession: true})
	if err != nil {
		t.Fatalf("ResumeTaskWithContext: %v", err)
	}
	if result.ResumeMode != types.ResumeModeRehydrate {
		t.Errorf("ResumeMode = %q, want rehydrate for pi executor", result.ResumeMode)
	}
}

// TestResumeTaskWithContext_LiveInject — when the LiveInjector reports the
// session is live, inject into it and do NOT flip status to pending.
func TestResumeTaskWithContext_LiveInject(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, resumeTestProject)
	seedAbandonedOpencodeTaskWithSession(t, store, "live0002", "dead-runner", "ses-live")

	injector := &fakeLiveInjector{injected: true, sessionID: "ses-live"}
	svc.SetLiveInjector(injector)

	result, err := svc.ResumeTaskWithContext(context.Background(), resumeTestProject, "live0002",
		&types.ResumeWithContextOptions{InjectedContext: "live ctx blob", PreferSameSession: true})
	if err != nil {
		t.Fatalf("ResumeTaskWithContext: %v", err)
	}
	if !result.Resumed {
		t.Fatalf("expected Resumed=true for live inject, got %+v", result)
	}
	if result.ResumeMode != types.ResumeModeLiveInjected {
		t.Errorf("ResumeMode = %q, want live_injected", result.ResumeMode)
	}
	if !result.InjectedLive {
		t.Error("expected InjectedLive=true")
	}
	if result.TargetSessionID != "ses-live" {
		t.Errorf("TargetSessionID = %q, want ses-live", result.TargetSessionID)
	}
	if injector.calledWith != "live ctx blob" {
		t.Errorf("injector got context %q, want the passed blob", injector.calledWith)
	}

	// Status must NOT be flipped to pending on the live-inject path.
	meta := readTaskMeta(t, store, resumeTestProject, "live0002")
	if s, _ := metaString(meta, "status"); s == "pending" {
		t.Error("status was flipped to pending on live-inject path (should stay in_progress)")
	}
	if b, _ := metaBool(meta, "resume_requested"); b {
		t.Error("resume_requested was stamped on live-inject path (should not be)")
	}
}

// TestResumeTaskWithContext_LiveInjectFallthroughOnFalse — injector returns
// injected=false (dead/no instance) → fall through to relaunch (status flips).
func TestResumeTaskWithContext_LiveInjectFallthroughOnFalse(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, resumeTestProject)
	seedAbandonedOpencodeTaskWithSession(t, store, "fall0001", "dead-runner", "ses-dead")

	injector := &fakeLiveInjector{injected: false}
	svc.SetLiveInjector(injector)

	result, err := svc.ResumeTaskWithContext(context.Background(), resumeTestProject, "fall0001",
		&types.ResumeWithContextOptions{InjectedContext: "ctx", PreferSameSession: true})
	if err != nil {
		t.Fatalf("ResumeTaskWithContext: %v", err)
	}
	if !result.Resumed {
		t.Fatalf("expected Resumed=true (relaunch), got %+v", result)
	}
	if result.InjectedLive {
		t.Error("InjectedLive should be false when injector reported not-live")
	}
	if result.ResumeMode == types.ResumeModeLiveInjected {
		t.Error("ResumeMode should not be live_injected after fallthrough")
	}
	meta := readTaskMeta(t, store, resumeTestProject, "fall0001")
	if s, _ := metaString(meta, "status"); s != "pending" {
		t.Errorf("status = %q, want pending after relaunch fallthrough", s)
	}
}

// TestResumeTaskWithContext_LiveClaimSafety — force=true against a task claimed
// by an ONLINE runner must refuse even with force (mirrors ResumeTask).
func TestResumeTaskWithContext_LiveClaimSafety(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, resumeTestProject)
	insertTaskNote(t, store, "liveclm1", "Live Task", "in_progress", "medium", resumeTestProject, map[string]interface{}{
		"executor": "opencode",
	})
	insertRunnerForTaskSelectionTest(t, store, "live-runner", []string{"opencode"}, nil)
	ok, _, err := store.ClaimTask(context.Background(), resumeTestProject, "liveclm1", "live-runner", 10*time.Minute)
	if err != nil || !ok {
		t.Fatalf("ClaimTask setup failed: err=%v ok=%v", err, ok)
	}

	result, err := svc.ResumeTaskWithContext(context.Background(), resumeTestProject, "liveclm1",
		&types.ResumeWithContextOptions{InjectedContext: "ctx", Force: true})
	if err != nil {
		t.Fatalf("ResumeTaskWithContext: %v", err)
	}
	if result.Resumed {
		t.Errorf("expected Resumed=false when live runner holds claim (even with force), got %+v", result)
	}
	if result.Reason == "" {
		t.Error("expected explanatory Reason mentioning live claim")
	}
	// Claim must still be intact.
	claim, err := store.GetClaim(context.Background(), resumeTestProject, "liveclm1")
	if err != nil {
		t.Fatalf("GetClaim: %v", err)
	}
	if claim == nil || claim.RunnerID != "live-runner" {
		t.Fatal("live runner's claim was disturbed (nuke race regression)")
	}
}

// TestResumeTaskWithContext_Idempotent — already pending+resume_requested →
// Resumed=false with a Reason.
func TestResumeTaskWithContext_Idempotent(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, resumeTestProject)
	insertTaskNote(t, store, "idem0001", "Already Resumed", "pending", "medium", resumeTestProject, map[string]interface{}{
		"resume_requested": true,
	})

	result, err := svc.ResumeTaskWithContext(context.Background(), resumeTestProject, "idem0001",
		&types.ResumeWithContextOptions{InjectedContext: "ctx"})
	if err != nil {
		t.Fatalf("ResumeTaskWithContext: %v", err)
	}
	if result.Resumed {
		t.Errorf("expected Resumed=false for idempotent replay, got %+v", result)
	}
	if result.Reason == "" {
		t.Error("expected non-empty Reason on idempotent no-op")
	}
}

// TestResumeTaskWithContext_NotFound — unknown task returns an error containing
// "not found" so the handler maps to 404.
func TestResumeTaskWithContext_NotFound(t *testing.T) {
	svc, _, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, resumeTestProject)

	_, err := svc.ResumeTaskWithContext(context.Background(), resumeTestProject, "does-not-exist",
		&types.ResumeWithContextOptions{InjectedContext: "ctx"})
	if err == nil {
		t.Fatal("expected error for unknown task ID")
	}
	if !containsFold(err.Error(), "not found") {
		t.Errorf("error should contain 'not found' (for 404 fallback), got: %v", err)
	}
}

// TestResumeTaskWithContext_ParseBack — verify the extended metadata is parsed
// back onto the enriched ResolvedTask (the Phase 4 runner reads these).
func TestResumeTaskWithContext_ParseBack(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, resumeTestProject)
	seedAbandonedOpencodeTaskWithSession(t, store, "parse001", "dead-runner", "ses-pb")

	_, err := svc.ResumeTaskWithContext(context.Background(), resumeTestProject, "parse001",
		&types.ResumeWithContextOptions{InjectedContext: "the injected blob", PreferSameSession: true, ExecutorOverride: "opencode"})
	if err != nil {
		t.Fatalf("ResumeTaskWithContext: %v", err)
	}

	task, err := svc.GetTask(context.Background(), resumeTestProject, "parse001")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.ResumeInjectedContext != "the injected blob" {
		t.Errorf("ResumeInjectedContext = %q, want the injected blob", task.ResumeInjectedContext)
	}
	if !task.ResumePreferSameSession {
		t.Error("ResumePreferSameSession = false, want true")
	}
	if task.ResumeMode != types.ResumeModeSameSession {
		t.Errorf("ResumeMode = %q, want same_session", task.ResumeMode)
	}
	if task.ResumeExecutorOverride != "opencode" {
		t.Errorf("ResumeExecutorOverride = %q, want opencode", task.ResumeExecutorOverride)
	}
}

// TestResumeFeatureWithContext_MixedBatch — fan-out over a feature with a mix
// of abandoned + terminal tasks. Abandoned ones resume; terminal ones skip.
func TestResumeFeatureWithContext_MixedBatch(t *testing.T) {
	svc, store, brainDir := newTestTaskServiceWithDefaults(t, config.TaskDefaultsConfig{})
	createProjectDir(t, brainDir, resumeTestProject)

	// Two abandoned opencode tasks + one completed (terminal) — all same feature.
	seedFeatureTaskFiles(t, brainDir, resumeTestProject, "featbatch", []taskFileSpec{
		{ID: "featx001", Status: "in_progress"},
		{ID: "featx002", Status: "in_progress"},
		{ID: "featx003", Status: "completed"},
	})

	insertTaskNote(t, store, "featx001", "Abnd A", "in_progress", "medium", resumeTestProject, map[string]interface{}{
		"executor": "opencode", "feature_id": "featbatch",
	})
	insertTaskNote(t, store, "featx002", "Abnd B", "in_progress", "medium", resumeTestProject, map[string]interface{}{
		"executor": "opencode", "feature_id": "featbatch",
	})
	insertTaskNote(t, store, "featx003", "Done C", "completed", "medium", resumeTestProject, map[string]interface{}{
		"executor": "opencode", "feature_id": "featbatch",
	})
	seedOfflineClaim(t, store, "featx001", "dead-runner")
	seedOfflineClaim(t, store, "featx002", "dead-runner")

	result, err := svc.ResumeFeatureWithContext(context.Background(), resumeTestProject, "featbatch",
		&types.ResumeWithContextOptions{InjectedContext: "batch ctx"})
	if err != nil {
		t.Fatalf("ResumeFeatureWithContext: %v", err)
	}
	if result.TotalResumed != 2 {
		t.Errorf("TotalResumed = %d, want 2", result.TotalResumed)
	}
	if result.TotalSkipped != 1 {
		t.Errorf("TotalSkipped = %d, want 1", result.TotalSkipped)
	}
	if len(result.Results) != 3 {
		t.Fatalf("len(Results) = %d, want 3", len(result.Results))
	}
}

// seedOfflineClaim registers an offline runner and gives it an unexpired claim
// on the task, matching the runner_offline abandonment signal. Split out of
// seedAbandonedTaskWithOfflineClaim so a caller can seed a custom task note.
func seedOfflineClaim(t *testing.T, store *storage.TenantStore, taskID, runnerID string) {
	t.Helper()
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
	ok, _, err := store.ClaimTask(context.Background(), resumeTestProject, taskID, runnerID, 10*time.Minute)
	if err != nil || !ok {
		t.Fatalf("ClaimTask setup failed: err=%v ok=%v", err, ok)
	}
}

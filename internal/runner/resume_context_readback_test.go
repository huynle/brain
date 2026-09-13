package runner

import (
	"context"
	"fmt"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Phase 4 runner read-back: resume-with-context metadata -> SpawnOptions
//
// A resume-with-context relaunch arrives at the runner as a PENDING task
// carrying the extended metadata (resume_requested + resume_mode + injected
// context + prefer_same_session + executor_override). claimAndSpawnWithWorkdir
// reads those fields onto SpawnOptions and finalizes the same_session-vs-
// rehydrate decision via the executor's CanResumeSession probe.
// =============================================================================

// resumeCtxTask builds a pending task shaped like one stamped by
// ResumeTaskWithContext, with a single stored opencode session.
func resumeCtxTask(id, projectID string) *types.ResolvedTask {
	task := testTask(id, projectID)
	task.ResumeRequested = true
	task.ResumeMode = "same_session"
	task.ResumeInjectedContext = "supervisor says: the DB migration already ran, do not re-run it"
	task.ResumePreferSameSession = true
	task.Sessions = map[string]types.SessionInfo{
		"ses_stored_1": {Timestamp: "2026-01-01T00:00:00Z"},
	}
	return task
}

func TestClaimAndSpawn_ResumeWithContext_SameSessionWhenCapable(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	executor.resumeCapability = SessionResumeCapability{SameSession: true}
	proc := newMockProcess(100)
	executor.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/test"}
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	task := resumeCtxTask("task1", "proj-a")

	if err := tr.claimAndSpawn(context.Background(), task, "proj-a"); err != nil {
		t.Fatalf("claimAndSpawn returned error: %v", err)
	}

	spawns := executor.getSpawnCalls()
	if len(spawns) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(spawns))
	}
	opts := spawns[0].Opts
	if !opts.IsResume {
		t.Error("IsResume should still be true (legacy toggle preserved)")
	}
	if opts.ResumeMode != ResumeModeSameSession {
		t.Errorf("ResumeMode = %q, want %q", opts.ResumeMode, ResumeModeSameSession)
	}
	if opts.ResumeSessionID != "ses_stored_1" {
		t.Errorf("ResumeSessionID = %q, want %q", opts.ResumeSessionID, "ses_stored_1")
	}
	if opts.InjectedContext != task.ResumeInjectedContext {
		t.Errorf("InjectedContext = %q, want %q", opts.InjectedContext, task.ResumeInjectedContext)
	}
}

func TestClaimAndSpawn_ResumeWithContext_RehydrateWhenNotCapable(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	// Executor cannot reload the session's history -> fall back to rehydrate.
	executor.resumeCapability = SessionResumeCapability{SameSession: false, Reason: "not on disk"}
	proc := newMockProcess(100)
	executor.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/test"}
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	task := resumeCtxTask("task1", "proj-a")

	if err := tr.claimAndSpawn(context.Background(), task, "proj-a"); err != nil {
		t.Fatalf("claimAndSpawn returned error: %v", err)
	}

	spawns := executor.getSpawnCalls()
	if len(spawns) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(spawns))
	}
	opts := spawns[0].Opts
	if opts.ResumeMode != ResumeModeRehydrate {
		t.Errorf("ResumeMode = %q, want %q", opts.ResumeMode, ResumeModeRehydrate)
	}
	if opts.ResumeSessionID != "" {
		t.Errorf("ResumeSessionID = %q, want empty for rehydrate", opts.ResumeSessionID)
	}
	if opts.InjectedContext != task.ResumeInjectedContext {
		t.Errorf("InjectedContext = %q, want %q", opts.InjectedContext, task.ResumeInjectedContext)
	}
}

func TestClaimAndSpawn_ResumeWithContext_RehydrateWhenPreferSameSessionFalse(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	// Even though the executor COULD same-session resume, the request opted out.
	executor.resumeCapability = SessionResumeCapability{SameSession: true}
	proc := newMockProcess(100)
	executor.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/test"}
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	task := resumeCtxTask("task1", "proj-a")
	task.ResumePreferSameSession = false

	if err := tr.claimAndSpawn(context.Background(), task, "proj-a"); err != nil {
		t.Fatalf("claimAndSpawn returned error: %v", err)
	}

	spawns := executor.getSpawnCalls()
	opts := spawns[0].Opts
	if opts.ResumeMode != ResumeModeRehydrate {
		t.Errorf("ResumeMode = %q, want %q", opts.ResumeMode, ResumeModeRehydrate)
	}
	if opts.ResumeSessionID != "" {
		t.Errorf("ResumeSessionID = %q, want empty when prefer_same_session=false", opts.ResumeSessionID)
	}
}

func TestClaimAndSpawn_ResumeWithContext_RehydrateWhenNoStoredSession(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	executor.resumeCapability = SessionResumeCapability{SameSession: true}
	proc := newMockProcess(100)
	executor.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/test"}
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	task := resumeCtxTask("task1", "proj-a")
	task.Sessions = nil // no stored session id to reuse

	if err := tr.claimAndSpawn(context.Background(), task, "proj-a"); err != nil {
		t.Fatalf("claimAndSpawn returned error: %v", err)
	}

	spawns := executor.getSpawnCalls()
	opts := spawns[0].Opts
	if opts.ResumeMode != ResumeModeRehydrate {
		t.Errorf("ResumeMode = %q, want %q", opts.ResumeMode, ResumeModeRehydrate)
	}
	if opts.ResumeSessionID != "" {
		t.Errorf("ResumeSessionID = %q, want empty with no stored session", opts.ResumeSessionID)
	}
	if opts.InjectedContext != task.ResumeInjectedContext {
		t.Errorf("InjectedContext = %q, want %q", opts.InjectedContext, task.ResumeInjectedContext)
	}
}

// picks the most recent SessionInfo by timestamp deterministically.
func TestClaimAndSpawn_ResumeWithContext_PicksMostRecentStoredSession(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	executor.resumeCapability = SessionResumeCapability{SameSession: true}
	proc := newMockProcess(100)
	executor.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/test"}
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	task := resumeCtxTask("task1", "proj-a")
	task.Sessions = map[string]types.SessionInfo{
		"ses_old":    {Timestamp: "2026-01-01T00:00:00Z"},
		"ses_newest": {Timestamp: "2026-06-01T00:00:00Z"},
		"ses_mid":    {Timestamp: "2026-03-01T00:00:00Z"},
	}

	if err := tr.claimAndSpawn(context.Background(), task, "proj-a"); err != nil {
		t.Fatalf("claimAndSpawn returned error: %v", err)
	}

	spawns := executor.getSpawnCalls()
	opts := spawns[0].Opts
	if opts.ResumeMode != ResumeModeSameSession {
		t.Fatalf("ResumeMode = %q, want %q", opts.ResumeMode, ResumeModeSameSession)
	}
	if opts.ResumeSessionID != "ses_newest" {
		t.Errorf("ResumeSessionID = %q, want %q (most-recent by timestamp)", opts.ResumeSessionID, "ses_newest")
	}
}

// Legacy /resume (ResumeRequested set, ResumeMode empty) must behave EXACTLY
// as before: IsResume=true only, no ResumeMode / session / injected context.
func TestClaimAndSpawn_LegacyResume_Unchanged(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	executor.resumeCapability = SessionResumeCapability{SameSession: true}
	proc := newMockProcess(100)
	executor.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/test"}
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	task := testTask("task1", "proj-a")
	task.ResumeRequested = true
	// ResumeMode intentionally empty; a stored session exists but must be ignored.
	task.Sessions = map[string]types.SessionInfo{"ses_stored_1": {Timestamp: "2026-01-01T00:00:00Z"}}

	if err := tr.claimAndSpawn(context.Background(), task, "proj-a"); err != nil {
		t.Fatalf("claimAndSpawn returned error: %v", err)
	}

	spawns := executor.getSpawnCalls()
	opts := spawns[0].Opts
	if !opts.IsResume {
		t.Error("IsResume should be true for legacy resume")
	}
	if opts.ResumeMode != "" {
		t.Errorf("ResumeMode = %q, want empty for legacy resume", opts.ResumeMode)
	}
	if opts.ResumeSessionID != "" {
		t.Errorf("ResumeSessionID = %q, want empty for legacy resume", opts.ResumeSessionID)
	}
	if opts.InjectedContext != "" {
		t.Errorf("InjectedContext = %q, want empty for legacy resume", opts.InjectedContext)
	}
}

// Clear-after-success wipes ALL the resume-with-context runtime keys, not just
// resume_requested.
func TestClaimAndSpawn_ResumeWithContext_ClearsExtendedMetadataOnSuccess(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	executor.resumeCapability = SessionResumeCapability{SameSession: true}
	proc := newMockProcess(100)
	executor.spawnResult = &SpawnResult{PID: 100, Proc: proc, Workdir: "/test"}
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	task := resumeCtxTask("task1", "proj-a")

	if err := tr.claimAndSpawn(context.Background(), task, "proj-a"); err != nil {
		t.Fatalf("claimAndSpawn returned error: %v", err)
	}

	writes := client.metadataWrites()
	clear := findMetadataWrite(writes, task.Path, "resume_mode")
	if clear == nil {
		t.Fatalf("expected a metadata write clearing resume keys, got %#v", writes)
	}
	assertMetaValue(t, clear.Fields, "resume_requested", false)
	assertMetaValue(t, clear.Fields, "resume_mode", "")
	assertMetaValue(t, clear.Fields, "resume_injected_context", "")
	assertMetaValue(t, clear.Fields, "resume_prefer_same_session", false)
	assertMetaValue(t, clear.Fields, "resume_executor_override", "")
}

// Spawn-failure rollback re-stamps ALL the resume-with-context keys so a retry
// still carries the injected context + mode.
func TestClaimAndSpawn_ResumeWithContext_RestampsExtendedMetadataOnSpawnFailure(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	executor.resumeCapability = SessionResumeCapability{SameSession: true}
	executor.spawnErr = fmt.Errorf("spawn failed")
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)

	task := resumeCtxTask("task1", "proj-a")

	if err := tr.claimAndSpawn(context.Background(), task, "proj-a"); err == nil {
		t.Fatal("expected spawn error to propagate")
	}

	writes := client.metadataWrites()
	restamp := findMetadataWrite(writes, task.Path, "resume_mode")
	if restamp == nil {
		t.Fatalf("expected a re-stamp metadata write, got %#v", writes)
	}
	assertMetaValue(t, restamp.Fields, "resume_requested", true)
	assertMetaValue(t, restamp.Fields, "resume_mode", "same_session")
	assertMetaValue(t, restamp.Fields, "resume_injected_context", task.ResumeInjectedContext)
	assertMetaValue(t, restamp.Fields, "resume_prefer_same_session", true)
}

// findMetadataWrite returns the first recorded UpdateMetadata call for the
// given path that contains the given key, or nil.
func findMetadataWrite(writes []metadataCall, path, key string) *metadataCall {
	for i := range writes {
		if writes[i].Path != path {
			continue
		}
		if _, ok := writes[i].Fields[key]; ok {
			return &writes[i]
		}
	}
	return nil
}

func assertMetaValue(t *testing.T, fields map[string]interface{}, key string, want interface{}) {
	t.Helper()
	got, ok := fields[key]
	if !ok {
		t.Errorf("metadata missing key %q (fields=%#v)", key, fields)
		return
	}
	if got != want {
		t.Errorf("metadata[%q] = %#v, want %#v", key, got, want)
	}
}

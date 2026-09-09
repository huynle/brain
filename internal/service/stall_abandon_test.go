package service

import (
	"context"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/storage"
)

const stallTestProject = "stall-proj"

// seedInProgressTaskWithBody inserts an in_progress task whose body is `body`,
// held by a fresh (unexpired) claim from an ONLINE runner. This is the
// "runner is up, session is wedged" shape: without the stall marker such a
// task would fall through to the existing signals and be classified NOT
// abandoned (online runner + live claim).
func seedInProgressTaskWithBody(t *testing.T, store *storage.TenantStore, taskID, runnerID, body string) {
	t.Helper()
	ctx := context.Background()

	typ := "task"
	status := "in_progress"
	priority := "medium"
	created := "2025-01-01T00:00:00Z"
	modified := "2025-01-02T00:00:00Z"
	proj := stallTestProject
	path := "projects/" + stallTestProject + "/task/" + taskID + ".md"

	note := &storage.NoteRow{
		Path:      path,
		ShortID:   taskID,
		Title:     "Stall Task",
		Body:      &body,
		Metadata:  "{}",
		Type:      &typ,
		Status:    &status,
		Priority:  &priority,
		ProjectID: &proj,
		Created:   &created,
		Modified:  &modified,
	}
	if _, err := store.InsertNote(ctx, note); err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	// Register the runner as ONLINE with a fresh heartbeat.
	now := time.Now().UnixMilli()
	if err := store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      runnerID,
		Hostname:      runnerID + "-host",
		Labels:        map[string]string{},
		Executors:     []string{"opencode"},
		Capabilities:  []string{},
		MaxParallel:   1,
		RegisteredAt:  now,
		LastHeartbeat: now,
		Status:        "online",
	}); err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}

	// Fresh, unexpired claim held by the online runner.
	ok, _, err := store.ClaimTask(ctx, stallTestProject, taskID, runnerID, 10*time.Minute)
	if err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}
	if !ok {
		t.Fatal("ClaimTask should have succeeded on a fresh task")
	}
}

// A task whose body contains StalledMarker, in_progress, held by a LIVE claim
// from an ONLINE runner ⇒ enrichAbandonmentState surfaces it as abandoned with
// reason "stalled". The marker is the authority: it wins over the live-claim /
// online-runner signals that would otherwise mark it NOT abandoned.
func TestEnrichAbandonment_StalledMarker_SurfacesStalled(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, stallTestProject)

	body := "Some task work.\n\n---\n" + StalledMarker + " (no output for 12m0s).*\n"
	seedInProgressTaskWithBody(t, store, "stld0001", "live-runner", body)

	task, err := svc.GetTask(context.Background(), stallTestProject, "stld0001")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !task.IsAbandoned {
		t.Fatalf("expected IsAbandoned=true for a stalled task, got %+v", task)
	}
	if task.AbandonReason != AbandonReasonStalled {
		t.Errorf("AbandonReason = %q, want %q", task.AbandonReason, AbandonReasonStalled)
	}
}

// A control task WITHOUT the stall marker, under the same online-runner +
// live-claim shape, must fall through to the existing signals and be
// classified NOT abandoned.
func TestEnrichAbandonment_NoStalledMarker_NotAbandoned(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, stallTestProject)

	body := "Some task work, no stall marker here.\n"
	seedInProgressTaskWithBody(t, store, "ctrl0001", "live-runner", body)

	task, err := svc.GetTask(context.Background(), stallTestProject, "ctrl0001")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.IsAbandoned {
		t.Fatalf("expected IsAbandoned=false for a non-stalled online+live task, got reason=%q", task.AbandonReason)
	}
	if task.AbandonReason != "" {
		t.Errorf("AbandonReason = %q, want empty", task.AbandonReason)
	}
}

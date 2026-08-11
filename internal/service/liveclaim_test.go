package service

import (
	"context"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// upsertRunnerWithStatus registers a runner in a specific lifecycle state.
func upsertRunnerWithStatus(t *testing.T, store *storage.StorageLayer, runnerID, status string) {
	t.Helper()
	now := time.Now().UnixMilli()
	if err := store.UpsertRunner(context.Background(), &storage.RunnerRow{
		RunnerID:      runnerID,
		Hostname:      runnerID + "-host",
		Labels:        map[string]string{},
		Executors:     []string{"opencode"},
		Capabilities:  []string{},
		MaxParallel:   1,
		RegisteredAt:  now,
		LastHeartbeat: now,
		Status:        status,
	}); err != nil {
		t.Fatalf("UpsertRunner: %v", err)
	}
}

func TestGetLiveClaim_NoClaimIsNotLive(t *testing.T) {
	svc, _, _ := newTestTaskService(t)

	claim, err := svc.GetLiveClaim(context.Background(), "proj", "task1")
	if err != nil {
		t.Fatalf("GetLiveClaim: %v", err)
	}
	if claim.Live {
		t.Error("Live = true with no claim present")
	}
	if claim.RunnerID != "" {
		t.Errorf("RunnerID = %q, want empty", claim.RunnerID)
	}
}

func TestGetLiveClaim_ClaimByOnlineRunnerIsLive(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	upsertRunnerWithStatus(t, store, "runner-1", string(types.RunnerStatusOnline))
	if _, err := svc.ClaimTask(ctx, "proj", "task1", "runner-1"); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}

	claim, err := svc.GetLiveClaim(ctx, "proj", "task1")
	if err != nil {
		t.Fatalf("GetLiveClaim: %v", err)
	}
	if !claim.Live {
		t.Error("Live = false for a fresh claim held by an online runner")
	}
	if claim.RunnerID != "runner-1" {
		t.Errorf("RunnerID = %q, want runner-1", claim.RunnerID)
	}
}

// A claim held by a crashed runner is exactly the abandoned case the resume
// flow recovers. It must NOT count as live, or users could never clean up
// after a crash.
func TestGetLiveClaim_ClaimByOfflineRunnerIsNotLive(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	upsertRunnerWithStatus(t, store, "runner-1", string(types.RunnerStatusOnline))
	if _, err := svc.ClaimTask(ctx, "proj", "task1", "runner-1"); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	upsertRunnerWithStatus(t, store, "runner-1", string(types.RunnerStatusOffline))

	claim, err := svc.GetLiveClaim(ctx, "proj", "task1")
	if err != nil {
		t.Fatalf("GetLiveClaim: %v", err)
	}
	if claim.Live {
		t.Error("Live = true for a claim held by an offline runner")
	}
}

// A claim whose owning runner was never registered cannot be attributed.
// Fail open: treat it as not live rather than blocking every delete.
func TestGetLiveClaim_UnknownRunnerIsNotLive(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	if _, err := svc.ClaimTask(ctx, "proj", "task1", "ghost-runner"); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}

	claim, err := svc.GetLiveClaim(ctx, "proj", "task1")
	if err != nil {
		t.Fatalf("GetLiveClaim: %v", err)
	}
	if claim.Live {
		t.Error("Live = true for a claim owned by an unregistered runner")
	}
}

// Releasing a claim must immediately clear liveness — otherwise a delete
// stays blocked after the runner has finished.
func TestGetLiveClaim_ReleasedClaimIsNotLive(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	upsertRunnerWithStatus(t, store, "runner-1", string(types.RunnerStatusOnline))
	if _, err := svc.ClaimTask(ctx, "proj", "task1", "runner-1"); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	if err := svc.ReleaseTask(ctx, "proj", "task1", "runner-1"); err != nil {
		t.Fatalf("ReleaseTask: %v", err)
	}

	claim, err := svc.GetLiveClaim(ctx, "proj", "task1")
	if err != nil {
		t.Fatalf("GetLiveClaim: %v", err)
	}
	if claim.Live {
		t.Error("Live = true after the claim was released")
	}
}

// Liveness is per task — a claim on one task must not block a sibling.
func TestGetLiveClaim_IsScopedToTheTask(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	upsertRunnerWithStatus(t, store, "runner-1", string(types.RunnerStatusOnline))
	if _, err := svc.ClaimTask(ctx, "proj", "task1", "runner-1"); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}

	claim, err := svc.GetLiveClaim(ctx, "proj", "task2")
	if err != nil {
		t.Fatalf("GetLiveClaim: %v", err)
	}
	if claim.Live {
		t.Error("Live = true for a task that was never claimed")
	}
}

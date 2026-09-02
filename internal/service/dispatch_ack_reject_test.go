package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

func TestTaskServiceAckDispatchLeaseMarksAcked(t *testing.T) {
	ctx := context.Background()
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, "brain-api")

	createdLease, ok, err := store.CreateDispatchLease(ctx, storage.DispatchLeaseCreate{ProjectID: "brain-api", TaskID: "task-001", AssignedRunnerID: "runner-123", PushedAt: time.Now().UnixMilli(), ExpiresAt: time.Now().Add(time.Minute).UnixMilli()})
	if err != nil || !ok {
		t.Fatalf("CreateDispatchLease ok=%v err=%v", ok, err)
	}

	resp, err := svc.AckDispatch(ctx, "brain-api", "task-001", "runner-123", createdLease.LeaseID)
	if err != nil {
		t.Fatalf("AckDispatch() error = %v", err)
	}
	if !resp.Success || resp.LeaseID != createdLease.LeaseID || resp.ProjectID != "brain-api" || resp.TaskID != "task-001" || resp.RunnerID != "runner-123" {
		t.Fatalf("response = %+v", resp)
	}
	lease, err := store.GetDispatchLeaseRow(ctx, "brain-api", "task-001")
	if err != nil {
		t.Fatal(err)
	}
	if lease.State != storage.DispatchLeaseStateAcked || lease.AckedAt == 0 || lease.LastError != "" {
		t.Fatalf("lease after ack = %+v", lease)
	}
}

func TestTaskServiceRejectDispatchLeaseRecordsStructuredReason(t *testing.T) {
	ctx := context.Background()
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, "brain-api")

	createdLease, ok, err := store.CreateDispatchLease(ctx, storage.DispatchLeaseCreate{ProjectID: "brain-api", TaskID: "task-001", AssignedRunnerID: "runner-123", PushedAt: time.Now().UnixMilli(), ExpiresAt: time.Now().Add(time.Minute).UnixMilli()})
	if err != nil || !ok {
		t.Fatalf("CreateDispatchLease ok=%v err=%v", ok, err)
	}

	reason := types.DispatchRejectReason{Code: "executor_unavailable", Message: "no pi executor", Details: map[string]string{"executor": "pi"}}
	resp, err := svc.RejectDispatch(ctx, "brain-api", "task-001", "runner-123", createdLease.LeaseID, reason)
	if err != nil {
		t.Fatalf("RejectDispatch() error = %v", err)
	}
	if !resp.Success || resp.Reason.Code != reason.Code || resp.Reason.Message != reason.Message {
		t.Fatalf("response = %+v", resp)
	}
	lease, err := store.GetDispatchLeaseRow(ctx, "brain-api", "task-001")
	if err != nil {
		t.Fatal(err)
	}
	if lease.State != storage.DispatchLeaseStateRejected || lease.RejectedAt == 0 {
		t.Fatalf("lease after reject = %+v", lease)
	}
	var stored types.DispatchRejectReason
	if err := json.Unmarshal([]byte(lease.LastError), &stored); err != nil {
		t.Fatalf("last_error %q is not structured JSON: %v", lease.LastError, err)
	}
	if stored.Code != reason.Code || stored.Message != reason.Message || stored.Details["executor"] != "pi" {
		t.Fatalf("stored reason = %+v", stored)
	}
}

// TestTaskServiceRejectDispatchRecordsPlacementHistory is the regression test
// for dispatch rejections being invisible in the UI.
//
// task_dispatch_leases holds only the latest attempt (PK is project+task), so a
// task rejected repeatedly (e.g. an AI-mode checkout with no git context
// looping on "workdir_unavailable") left no history of how many times it tried
// or why. RejectDispatch now also appends a "dispatch_rejected" row to the
// task_placement_reasons history the PWA already renders, so the operator sees
// the attempt count and reason instead of a task silently stuck pending.
func TestTaskServiceRejectDispatchRecordsPlacementHistory(t *testing.T) {
	ctx := context.Background()
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, "brain-api")

	reason := types.DispatchRejectReason{
		Code:    "workdir_unavailable",
		Message: "worktree mode requires a valid git repo context",
		Details: map[string]string{"execution_mode": "worktree"},
	}

	// Two rejection rounds: create a fresh lease each time (the scheduler
	// pushes a new lease per attempt), reject it, and expect one history row
	// per round.
	for round := 0; round < 2; round++ {
		createdLease, ok, err := store.CreateDispatchLease(ctx, storage.DispatchLeaseCreate{
			ProjectID:        "brain-api",
			TaskID:           "task-hist",
			AssignedRunnerID: "runner-123",
			PushedAt:         time.Now().UnixMilli(),
			ExpiresAt:        time.Now().Add(time.Minute).UnixMilli(),
		})
		if err != nil || !ok {
			t.Fatalf("round %d CreateDispatchLease ok=%v err=%v", round, ok, err)
		}
		if _, err := svc.RejectDispatch(ctx, "brain-api", "task-hist", "runner-123", createdLease.LeaseID, reason); err != nil {
			t.Fatalf("round %d RejectDispatch() error = %v", round, err)
		}
	}

	reasons, err := store.ListPlacementReasons(ctx, "brain-api", "task-hist")
	if err != nil {
		t.Fatalf("ListPlacementReasons error = %v", err)
	}
	if len(reasons) != 2 {
		t.Fatalf("expected 2 placement-reason history rows, got %d: %+v", len(reasons), reasons)
	}
	for i, r := range reasons {
		if r.Decision != "dispatch_rejected" {
			t.Errorf("row %d decision = %q, want dispatch_rejected", i, r.Decision)
		}
		if !strings.Contains(r.Reason, "workdir_unavailable") {
			t.Errorf("row %d reason = %q, want it to contain the rejection code", i, r.Reason)
		}
		if r.RunnerID != "runner-123" {
			t.Errorf("row %d runner_id = %q, want runner-123", i, r.RunnerID)
		}
	}
}

func TestTaskServiceAckRejectMissingOrMismatchedLease(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestTaskService(t)

	if _, err := svc.AckDispatch(ctx, "brain-api", "missing", "runner-123", "lease-abc"); err != api.ErrNotFound {
		t.Fatalf("AckDispatch missing err = %v, want ErrNotFound", err)
	}
	createdLease, ok, err := store.CreateDispatchLease(ctx, storage.DispatchLeaseCreate{ProjectID: "brain-api", TaskID: "task-001", AssignedRunnerID: "runner-other", PushedAt: time.Now().UnixMilli(), ExpiresAt: time.Now().Add(time.Minute).UnixMilli()})
	if err != nil || !ok {
		t.Fatalf("CreateDispatchLease ok=%v err=%v", ok, err)
	}
	_, err = svc.RejectDispatch(ctx, "brain-api", "task-001", "runner-other", "wrong-lease", types.DispatchRejectReason{Code: "busy"})
	if err != api.ErrNotFound && !strings.Contains(err.Error(), "not found") {
		t.Fatalf("RejectDispatch mismatched lease err = %v, want not found", err)
	}
	_, err = svc.RejectDispatch(ctx, "brain-api", "task-001", "runner-123", createdLease.LeaseID, types.DispatchRejectReason{Code: "busy"})
	if err != api.ErrNotFound && !strings.Contains(err.Error(), "not found") {
		t.Fatalf("RejectDispatch mismatched runner err = %v, want not found", err)
	}
}

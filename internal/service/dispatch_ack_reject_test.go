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

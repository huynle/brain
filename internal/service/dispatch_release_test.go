package service

import (
	"context"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
)

func TestTaskServiceReleaseDispatchDeletesLease(t *testing.T) {
	ctx := context.Background()
	svc, store, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, "brain-api")
	_, ok, err := store.CreateDispatchLease(ctx, storage.DispatchLeaseCreate{ProjectID: "brain-api", TaskID: "task-001", AssignedRunnerID: "runner-123", PushedAt: time.Now().UnixMilli(), ExpiresAt: time.Now().Add(time.Minute).UnixMilli()})
	if err != nil || !ok {
		t.Fatalf("CreateDispatchLease ok=%v err=%v", ok, err)
	}
	resp, err := svc.ReleaseDispatch(ctx, "brain-api", "task-001", "runner-123")
	if err != nil {
		t.Fatalf("ReleaseDispatch() error = %v", err)
	}
	if !resp.Success || resp.ProjectID != "brain-api" || resp.TaskID != "task-001" || resp.RunnerID != "runner-123" {
		t.Fatalf("response = %+v", resp)
	}
	lease, err := store.GetDispatchLeaseRow(ctx, "brain-api", "task-001")
	if err != nil {
		t.Fatal(err)
	}
	if lease != nil {
		t.Fatalf("expected dispatch lease to be released, got %+v", lease)
	}
}

func TestTaskServiceReleaseDispatchMissingLease(t *testing.T) {
	ctx := context.Background()
	svc, _, brainDir := newTestTaskService(t)
	createProjectDir(t, brainDir, "brain-api")
	resp, err := svc.ReleaseDispatch(ctx, "brain-api", "missing", "runner-123")
	if err != api.ErrNotFound {
		t.Fatalf("ReleaseDispatch missing err = %v, want ErrNotFound", err)
	}
	if resp == nil || resp.Success || resp.Error == "" {
		t.Fatalf("response = %+v", resp)
	}
}

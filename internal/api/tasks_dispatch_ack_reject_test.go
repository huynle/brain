package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestHandleAckDispatchRequiresLeaseProjectTaskAndUsesRouteRunner(t *testing.T) {
	var gotProjectID, gotTaskID, gotRunnerID, gotLeaseID string
	taskMock := &mockTaskService{
		ackDispatchFunc: func(ctx context.Context, projectID, taskID, runnerID, leaseID string) (*types.DispatchAckResponse, error) {
			gotProjectID = projectID
			gotTaskID = taskID
			gotRunnerID = runnerID
			gotLeaseID = leaseID
			return &types.DispatchAckResponse{Success: true, ProjectID: projectID, TaskID: taskID, RunnerID: runnerID, LeaseID: leaseID}, nil
		},
	}
	router := newTaskTestRouter(taskMock, &mockRunnerService{})
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/tasks/runners/runner-123/dispatch/ack", "application/json", jsonBody(t, map[string]string{
		"leaseId":   "lease-abc",
		"projectId": "brain-api",
		"taskId":    "task-001",
		"runnerId":  "spoofed-runner",
	}))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if gotProjectID != "brain-api" || gotTaskID != "task-001" || gotRunnerID != "runner-123" || gotLeaseID != "lease-abc" {
		t.Fatalf("service args project=%q task=%q runner=%q lease=%q", gotProjectID, gotTaskID, gotRunnerID, gotLeaseID)
	}
	body := decodeJSON[types.DispatchAckResponse](t, resp)
	if !body.Success || body.LeaseID != "lease-abc" || body.ProjectID != "brain-api" || body.TaskID != "task-001" {
		t.Fatalf("response = %+v", body)
	}
}

func TestHandleRejectDispatchPassesStructuredReason(t *testing.T) {
	var gotReason types.DispatchRejectReason
	taskMock := &mockTaskService{
		rejectDispatchFunc: func(ctx context.Context, projectID, taskID, runnerID, leaseID string, reason types.DispatchRejectReason) (*types.DispatchRejectResponse, error) {
			if projectID != "brain-api" || taskID != "task-001" || runnerID != "runner-123" || leaseID != "lease-abc" {
				return nil, fmt.Errorf("unexpected args project=%q task=%q runner=%q lease=%q", projectID, taskID, runnerID, leaseID)
			}
			gotReason = reason
			return &types.DispatchRejectResponse{Success: true, ProjectID: projectID, TaskID: taskID, RunnerID: runnerID, LeaseID: leaseID, Reason: reason}, nil
		},
	}
	router := newTaskTestRouter(taskMock, &mockRunnerService{})
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/tasks/runners/runner-123/dispatch/reject", "application/json", jsonBody(t, map[string]any{
		"leaseId":   "lease-abc",
		"projectId": "brain-api",
		"taskId":    "task-001",
		"reason": map[string]string{
			"code":    "executor_unavailable",
			"message": "no pi executor on this runner",
		},
	}))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if gotReason.Code != "executor_unavailable" || gotReason.Message != "no pi executor on this runner" {
		t.Fatalf("reason = %+v", gotReason)
	}
	body := decodeJSON[types.DispatchRejectResponse](t, resp)
	if body.Reason.Code != "executor_unavailable" || body.Reason.Message != "no pi executor on this runner" {
		t.Fatalf("response reason = %+v", body.Reason)
	}
}

func TestHandleDispatchAckRejectBadRequests(t *testing.T) {
	router := newTaskTestRouter(&mockTaskService{}, &mockRunnerService{})
	srv := httptest.NewServer(router)
	defer srv.Close()

	tests := []struct {
		name string
		path string
		body any
	}{
		{name: "ack missing lease", path: "/tasks/runners/runner-123/dispatch/ack", body: map[string]string{"projectId": "brain-api", "taskId": "task-001"}},
		{name: "ack missing project", path: "/tasks/runners/runner-123/dispatch/ack", body: map[string]string{"leaseId": "lease-abc", "taskId": "task-001"}},
		{name: "ack missing task", path: "/tasks/runners/runner-123/dispatch/ack", body: map[string]string{"leaseId": "lease-abc", "projectId": "brain-api"}},
		{name: "reject missing reason code", path: "/tasks/runners/runner-123/dispatch/reject", body: map[string]any{"leaseId": "lease-abc", "projectId": "brain-api", "taskId": "task-001", "reason": map[string]string{"message": "nope"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Post(srv.URL+tt.path, "application/json", jsonBody(t, tt.body))
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
			}
		})
	}
}

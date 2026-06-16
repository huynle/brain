package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestAPIClient_AckDispatchUsesRunnerScopedRouteAndPayload(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(types.DispatchAckResponse{Success: true, LeaseID: gotBody["leaseId"], ProjectID: gotBody["projectId"], TaskID: gotBody["taskId"], RunnerID: "runner-123"})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	resp, err := client.AckDispatch(context.Background(), "runner-123", "brain-api", "task-001", "lease-abc")
	if err != nil {
		t.Fatalf("AckDispatch() error = %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/tasks/runners/runner-123/dispatch/ack" {
		t.Fatalf("request %s %s", gotMethod, gotPath)
	}
	if gotBody["leaseId"] != "lease-abc" || gotBody["projectId"] != "brain-api" || gotBody["taskId"] != "task-001" {
		t.Fatalf("body = %#v", gotBody)
	}
	if !resp.Success || resp.RunnerID != "runner-123" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestAPIClient_RejectDispatchUsesStructuredReason(t *testing.T) {
	var gotPath string
	var gotBody struct {
		LeaseID   string                     `json:"leaseId"`
		ProjectID string                     `json:"projectId"`
		TaskID    string                     `json:"taskId"`
		Reason    types.DispatchRejectReason `json:"reason"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(types.DispatchRejectResponse{Success: true, LeaseID: gotBody.LeaseID, ProjectID: gotBody.ProjectID, TaskID: gotBody.TaskID, RunnerID: "runner-123", Reason: gotBody.Reason})
	}))
	defer srv.Close()

	client := NewAPIClient(testConfig(srv.URL))
	reason := types.DispatchRejectReason{Code: "busy", Message: "runner at capacity", Details: map[string]string{"capacity": "0"}}
	resp, err := client.RejectDispatch(context.Background(), "runner-123", "brain-api", "task-001", "lease-abc", reason)
	if err != nil {
		t.Fatalf("RejectDispatch() error = %v", err)
	}
	if gotPath != "/api/v1/tasks/runners/runner-123/dispatch/reject" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody.Reason.Code != "busy" || gotBody.Reason.Message != "runner at capacity" || gotBody.Reason.Details["capacity"] != "0" {
		t.Fatalf("body = %+v", gotBody)
	}
	if !resp.Success || resp.Reason.Code != "busy" {
		t.Fatalf("response = %+v", resp)
	}
}

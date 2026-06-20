package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestHandleUpsertInstance(t *testing.T) {
	var got types.OpencodeInstance
	var gotRunnerID string
	mock := &mockRunnerRegistryService{
		upsertInstanceFunc: func(ctx context.Context, runnerID string, inst types.OpencodeInstance) error {
			gotRunnerID = runnerID
			got = inst
			return nil
		},
	}
	router := newRunnerTestRouter(mock)

	body := `{"kind":"task","project_id":"proj","task_id":"t1","port":4096,"status":"busy"}`
	req := httptest.NewRequest(http.MethodPut, "/runners/runner-1/instances/inst_ab12", strings.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotRunnerID != "runner-1" {
		t.Errorf("expected runner-1, got %q", gotRunnerID)
	}
	// Path params must win over body values.
	if got.InstanceID != "inst_ab12" || got.RunnerID != "runner-1" {
		t.Errorf("instance identity not taken from path: %+v", got)
	}
	if got.Port != 4096 || got.Status != "busy" {
		t.Errorf("body fields not decoded: %+v", got)
	}
}

func TestHandleDeleteInstance_NotFound(t *testing.T) {
	mock := &mockRunnerRegistryService{
		deleteInstanceFunc: func(ctx context.Context, runnerID, instanceID string) error {
			return ErrNotFound
		},
	}
	router := newRunnerTestRouter(mock)

	req := httptest.NewRequest(http.MethodDelete, "/runners/runner-1/instances/inst_missing", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleListInstances(t *testing.T) {
	instances := []types.OpencodeInstance{
		{InstanceID: "inst_01", RunnerID: "runner-1", Kind: "task", Status: "busy"},
		{InstanceID: "inst_02", RunnerID: "runner-1", Kind: "adhoc", Status: "idle"},
	}
	mock := &mockRunnerRegistryService{
		listInstancesFunc: func(ctx context.Context, runnerID string) (*types.InstanceListResponse, error) {
			if runnerID != "runner-1" {
				t.Errorf("unexpected runner id %q", runnerID)
			}
			return &types.InstanceListResponse{Instances: instances, Total: len(instances)}, nil
		},
		listAllInstancesFunc: func(ctx context.Context) (*types.InstanceListResponse, error) {
			return &types.InstanceListResponse{Instances: instances, Total: len(instances)}, nil
		},
	}
	router := newRunnerTestRouter(mock)

	for _, path := range []string{"/runners/runner-1/instances", "/instances"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: expected 200, got %d", path, rec.Code)
		}
		var resp types.InstanceListResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("GET %s: decode failed: %v", path, err)
		}
		if resp.Total != 2 || len(resp.Instances) != 2 {
			t.Errorf("GET %s: unexpected response: %+v", path, resp)
		}
	}
}

func TestHandleHeartbeat_PassesInstances(t *testing.T) {
	var gotReq types.RunnerHeartbeatRequest
	mock := &mockRunnerRegistryService{
		heartbeatFunc: func(ctx context.Context, runnerID string, req types.RunnerHeartbeatRequest) error {
			gotReq = req
			return nil
		},
	}
	router := newRunnerTestRouter(mock)

	body := `{"running_tasks":1,"instances":[{"instance_id":"inst_01","kind":"task","status":"busy"}]}`
	req := httptest.NewRequest(http.MethodPost, "/runners/runner-1/heartbeat", strings.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(gotReq.Instances) != 1 || gotReq.Instances[0].InstanceID != "inst_01" {
		t.Errorf("instances not passed through heartbeat: %+v", gotReq.Instances)
	}
}

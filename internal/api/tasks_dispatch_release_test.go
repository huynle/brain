package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestHandleReleaseDispatch(t *testing.T) {
	var gotProject, gotTask, gotRunner string
	router := newTaskTestRouter(&mockTaskService{releaseDispatchFunc: func(ctx context.Context, projectId, taskId, runnerId string) (*types.DispatchReleaseResponse, error) {
		gotProject, gotTask, gotRunner = projectId, taskId, runnerId
		return &types.DispatchReleaseResponse{Success: true, ProjectID: projectId, TaskID: taskId, RunnerID: runnerId}, nil
	}}, nil)
	req := httptest.NewRequest(http.MethodPost, "/tasks/runners/runner-123/dispatch/release", bytes.NewBufferString(`{"projectId":"brain-api","taskId":"task-001"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if gotProject != "brain-api" || gotTask != "task-001" || gotRunner != "runner-123" {
		t.Fatalf("release args = %q %q %q", gotProject, gotTask, gotRunner)
	}
	var resp types.DispatchReleaseResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("response = %+v", resp)
	}
}

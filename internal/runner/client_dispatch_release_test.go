package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestAPIClientReleaseDispatch(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["projectId"] != "brain-api" || body["taskId"] != "task-001" {
			t.Fatalf("body = %#v", body)
		}
		_ = json.NewEncoder(w).Encode(types.DispatchReleaseResponse{Success: true, ProjectID: "brain-api", TaskID: "task-001", RunnerID: "runner-123"})
	}))
	defer server.Close()
	client := NewAPIClient(RunnerConfig{BrainAPIURL: server.URL})
	resp, err := client.ReleaseDispatch(context.Background(), "runner-123", "brain-api", "task-001")
	if err != nil {
		t.Fatalf("ReleaseDispatch() error = %v", err)
	}
	if gotPath != "/api/v1/tasks/runners/runner-123/dispatch/release" {
		t.Fatalf("path = %q", gotPath)
	}
	if !resp.Success {
		t.Fatalf("response = %+v", resp)
	}
}

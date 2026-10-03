package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestTaskAssignmentTools_RequestAndFormatting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks/brain/task-one/runner-candidates":
			json.NewEncoder(w).Encode(types.RunnerCandidatesResponse{ProjectID: "brain", TaskID: "task-one", Candidates: []types.RunnerCandidate{{Runner: types.RunnerInfo{RunnerID: "runner-1"}, Compatible: true, Available: true}}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tasks/brain/runner-candidates":
			json.NewEncoder(w).Encode(types.RunnerCandidatesResponse{ProjectID: "brain", Candidates: []types.RunnerCandidate{{Runner: types.RunnerInfo{RunnerID: "runner-before-create"}, Compatible: true, Available: true}}})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/tasks/brain/task-one/assignment":
			json.NewEncoder(w).Encode(types.TaskAssignmentResponse{ProjectID: "brain", TaskID: "task-one", RunnerID: "runner-1", Scope: "task", Status: "active"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tasks/brain/task-one/assignment/clear":
			json.NewEncoder(w).Encode(types.TaskAssignmentResponse{ProjectID: "brain", TaskID: "task-one", Scope: "task", Status: "cleared"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	s := NewServer()
	RegisterTaskTools(s, NewAPIClient(server.URL))
	for _, tc := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"runner_candidates", map[string]any{"project": "brain", "task_id": "task-one"}, "runner-1"},
		{"runner_candidates", map[string]any{"project": "brain", "executor": "opencode", "requires_capability": []any{"gpu"}}, "runner-before-create"},
		{"task_assign", map[string]any{"project": "brain", "task_id": "task-one", "runner_id": "runner-1"}, "active"},
		{"task_clear_assignment", map[string]any{"project": "brain", "task_id": "task-one"}, "cleared"},
	} {
		out, err := s.tools[tc.tool].handler(context.Background(), tc.args)
		if err != nil {
			t.Fatalf("%s: %v", tc.tool, err)
		}
		if !strings.Contains(out, tc.want) {
			t.Fatalf("%s output = %q, want %q", tc.tool, out, tc.want)
		}
	}
}

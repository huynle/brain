package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterRunnerTools_CountNamesHandlersDescriptions(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterRunnerTools(s, client)

	expected := []string{
		"brain_runner_status",
		"brain_runners",
		"brain_runner_get",
		"brain_runner_instances",
		"brain_runner_instances_all",
	}
	if len(s.tools) != len(expected) {
		t.Fatalf("expected %d runner tools registered, got %d", len(expected), len(s.tools))
	}
	for _, name := range expected {
		rt, ok := s.tools[name]
		if !ok {
			t.Fatalf("tool %q not registered", name)
		}
		if rt.handler == nil {
			t.Errorf("tool %q has nil handler", name)
		}
		if strings.TrimSpace(rt.tool.Description) == "" {
			t.Errorf("tool %q has empty description", name)
		}
		if rt.tool.InputSchema.Type != "object" {
			t.Errorf("tool %q inputSchema.type = %q, want object", name, rt.tool.InputSchema.Type)
		}
	}
}

func TestRunnerToolSchemas(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterRunnerTools(s, client)

	tests := []struct {
		tool     string
		required []string
		props    []string
	}{
		{"brain_runner_status", nil, []string{}},
		{"brain_runners", nil, []string{"status", "executor", "project", "limit"}},
		{"brain_runner_get", []string{"runnerId"}, []string{"runnerId"}},
		{"brain_runner_instances", []string{"runnerId"}, []string{"runnerId", "status", "kind", "project"}},
		{"brain_runner_instances_all", nil, []string{"runnerId", "status", "kind", "project"}},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			tool := s.tools[tt.tool].tool
			if len(tool.InputSchema.Required) != len(tt.required) {
				t.Fatalf("required = %v, want %v", tool.InputSchema.Required, tt.required)
			}
			for _, req := range tt.required {
				found := false
				for _, got := range tool.InputSchema.Required {
					if got == req {
						found = true
					}
				}
				if !found {
					t.Errorf("required missing %q in %v", req, tool.InputSchema.Required)
				}
			}
			for _, prop := range tt.props {
				if _, ok := tool.InputSchema.Properties[prop]; !ok {
					t.Errorf("schema missing property %q", prop)
				}
			}
		})
	}
}

func TestRunnerTools_RequestPathsNoBodiesAndFormatting(t *testing.T) {
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if len(body) != 0 {
			t.Fatalf("unexpected request body for %s %s: %s", r.Method, r.URL.RequestURI(), body)
		}
		if r.URL.RawQuery != "" {
			t.Fatalf("unexpected query for %s %s: %s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v1/tasks/runner/status":
			json.NewEncoder(w).Encode(map[string]any{"running": true, "paused": false, "pausedProjects": []string{"sandbox"}, "automationsPaused": true, "automationPausedProjects": []string{"brain"}})
		case "/api/v1/runners":
			json.NewEncoder(w).Encode(map[string]any{"total": 2, "runners": []map[string]any{{"runner_id": "runner-1", "hostname": "host-a", "status": "online", "projects": []string{"brain"}, "capabilities": []string{"go"}, "executors": []string{"opencode"}, "max_parallel": 3, "active_tasks": 1, "registered_at": "2026-06-17T00:00:00Z", "last_heartbeat": "2026-06-17T00:01:00Z"}, {"runner_id": "runner-2", "hostname": "host-b", "status": "offline", "projects": []string{"other"}, "capabilities": []string{"pi"}, "executors": []string{"pi"}, "max_parallel": 1}}})
		case "/api/v1/runners/runner-1":
			json.NewEncoder(w).Encode(map[string]any{"runner_id": "runner-1", "hostname": "host-a", "status": "online", "projects": []string{"brain"}, "capabilities": []string{"go"}, "executors": []string{"opencode"}, "max_parallel": 3, "active_tasks": 1, "last_heartbeat": "2026-06-17T00:01:00Z"})
		case "/api/v1/runners/runner-1/instances":
			json.NewEncoder(w).Encode(map[string]any{"total": 1, "instances": []map[string]any{{"instance_id": "inst-1", "runner_id": "runner-1", "kind": "task", "project_id": "brain", "task_id": "task-1", "title": "Do work", "status": "busy", "executor": "opencode", "agent": "tdd-dev", "started_at": int64(1781654400000), "last_seen": int64(1781654460000)}}})
		case "/api/v1/instances":
			json.NewEncoder(w).Encode(map[string]any{"total": 1, "instances": []map[string]any{{"instance_id": "inst-2", "runner_id": "runner-2", "kind": "adhoc", "project_id": "brain", "title": "Scratch", "status": "idle", "executor": "pi"}}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterRunnerTools(s, client)

	status, err := s.tools["brain_runner_status"].handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("status handler error: %v", err)
	}
	for _, want := range []string{"Runner Status", "Running: true", "Paused: false", "Paused projects: sandbox", "Automations paused: true", "Automation paused projects: brain"} {
		if !strings.Contains(status, want) {
			t.Errorf("status missing %q:\n%s", want, status)
		}
	}

	runners, err := s.tools["brain_runners"].handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("runners handler error: %v", err)
	}
	for _, want := range []string{"Runners", "Total: 2", "runner-1", "host-a", "online", "Projects: brain", "Capabilities: go", "Instances: use brain_runner_instances"} {
		if !strings.Contains(runners, want) {
			t.Errorf("runners missing %q:\n%s", want, runners)
		}
	}

	runner, err := s.tools["brain_runner_get"].handler(context.Background(), map[string]any{"runnerId": "runner-1"})
	if err != nil {
		t.Fatalf("runner get handler error: %v", err)
	}
	for _, want := range []string{"Runner runner-1", "Hostname: host-a", "State: online", "Projects: brain", "Capabilities: go", "Active tasks: 1", "Last heartbeat: 2026-06-17T00:01:00Z"} {
		if !strings.Contains(runner, want) {
			t.Errorf("runner get missing %q:\n%s", want, runner)
		}
	}

	runnerInstances, err := s.tools["brain_runner_instances"].handler(context.Background(), map[string]any{"runnerId": "runner-1"})
	if err != nil {
		t.Fatalf("runner instances handler error: %v", err)
	}
	for _, want := range []string{"Runner Instances", "Runner: runner-1", "Total: 1", "inst-1", "busy", "Project: brain", "Task: task-1", "Do work", "Started: 1781654400000", "Last seen: 1781654460000"} {
		if !strings.Contains(runnerInstances, want) {
			t.Errorf("runner instances missing %q:\n%s", want, runnerInstances)
		}
	}

	allInstances, err := s.tools["brain_runner_instances_all"].handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("all instances handler error: %v", err)
	}
	for _, want := range []string{"Runner Instances", "All runners", "Total: 1", "inst-2", "runner-2", "adhoc", "idle", "Scratch"} {
		if !strings.Contains(allInstances, want) {
			t.Errorf("all instances missing %q:\n%s", want, allInstances)
		}
	}

	wantRequests := []string{
		"GET /api/v1/tasks/runner/status",
		"GET /api/v1/runners",
		"GET /api/v1/runners/runner-1",
		"GET /api/v1/runners/runner-1/instances",
		"GET /api/v1/instances",
	}
	if len(requests) != len(wantRequests) {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
	for i := range wantRequests {
		if requests[i] != wantRequests[i] {
			t.Errorf("request[%d] = %q, want %q", i, requests[i], wantRequests[i])
		}
	}
}

func TestRunnerTools_ValidationErrors(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:1")
	RegisterRunnerTools(s, client)

	tests := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"brain_runner_get", map[string]any{}, "runnerId"},
		{"brain_runner_instances", map[string]any{}, "runnerId"},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			result, err := s.tools[tt.tool].handler(context.Background(), tt.args)
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			if !strings.Contains(result, tt.want) {
				t.Fatalf("result = %q, want substring %q", result, tt.want)
			}
		})
	}
}

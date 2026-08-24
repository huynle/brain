package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestRegisterRunnerTools_CountNamesHandlersDescriptions(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterRunnerTools(s, client)

	expected := []string{
		"runner_status",
		"runners",
		"runner_get",
		"runner_instances",
		"runner_instances_all",
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
		{"runner_status", nil, []string{}},
		{"runners", nil, []string{"status", "executor", "project", "limit"}},
		{"runner_get", []string{"runner_id"}, []string{"runner_id"}},
		{"runner_instances", []string{"runner_id"}, []string{"runner_id", "status", "kind", "project"}},
		{"runner_instances_all", nil, []string{"runner_id", "status", "kind", "project"}},
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

	status, err := s.tools["runner_status"].handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("status handler error: %v", err)
	}
	// The old assertions pinned "Paused: false" alongside "Paused projects:
	// sandbox" — this very fixture shows the API's paused flag saying "false"
	// while a project IS paused, because the flag is any(pausedProjects) built
	// elsewhere and carries no independent meaning. The renderer now derives
	// everything from the lists and surfaces the cross-axis asymmetry, which is
	// the only part of two long lists that informs anything.
	for _, want := range []string{
		"Runner Status",
		"Server running: true",
		"Projects with task execution paused: 1",
		"Projects with automation execution paused: 1",
		"Tasks paused, automations still running: sandbox",
		"Automations paused, tasks still running: brain",
	} {
		if !strings.Contains(status, want) {
			t.Errorf("status missing %q:\n%s", want, status)
		}
	}
	if strings.Contains(status, "Paused: false") {
		t.Errorf("status still prints the meaningless global flag:\n%s", status)
	}

	runners, err := s.tools["runners"].handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("runners handler error: %v", err)
	}
	for _, want := range []string{"Runners", "Total: 2", "runner-1", "host-a", "online", "Projects: brain", "Capabilities: go", "Instances: use runner_instances with this runner_id"} {
		if !strings.Contains(runners, want) {
			t.Errorf("runners missing %q:\n%s", want, runners)
		}
	}

	runner, err := s.tools["runner_get"].handler(context.Background(), map[string]any{"runnerId": "runner-1"})
	if err != nil {
		t.Fatalf("runner get handler error: %v", err)
	}
	for _, want := range []string{"Runner runner-1", "Hostname: host-a", "State: online", "Projects: brain", "Capabilities: go", "Active tasks: 1", "Last heartbeat: 2026-06-17T00:01:00Z"} {
		if !strings.Contains(runner, want) {
			t.Errorf("runner get missing %q:\n%s", want, runner)
		}
	}

	runnerInstances, err := s.tools["runner_instances"].handler(context.Background(), map[string]any{"runnerId": "runner-1"})
	if err != nil {
		t.Fatalf("runner instances handler error: %v", err)
	}
	for _, want := range []string{"Runner Instances", "Runner: runner-1", "Total: 1", "inst-1", "busy", "Project: brain", "Task: task-1", "Do work", "Started: 1781654400000", "Last seen: 1781654460000"} {
		if !strings.Contains(runnerInstances, want) {
			t.Errorf("runner instances missing %q:\n%s", want, runnerInstances)
		}
	}

	allInstances, err := s.tools["runner_instances_all"].handler(context.Background(), map[string]any{})
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
		{"runner_get", map[string]any{}, "runner_id is required"},
		{"runner_instances", map[string]any{}, "runner_id is required"},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			result, err := s.tools[tt.tool].handler(context.Background(), tt.args)
			if err == nil {
				t.Fatalf("expected validation error, got result: %q", result)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

// TestFormatRunnerStatus_ProjectScopedAnswer covers the question an agent
// actually asks — "is this project paused, and on which dial" — which previously
// required eyeballing two comma-joined lists of dozens of project IDs.
func TestFormatRunnerStatus_ProjectScopedAnswer(t *testing.T) {
	status := types.RunnerStatusResponse{
		Running:                  true,
		Paused:                   true,
		PausedProjects:           []string{"brain-api", "tasks-only"},
		AutomationsPaused:        true,
		AutomationPausedProjects: []string{"brain-api", "autos-only"},
	}

	tests := []struct {
		project string
		want    []string
		notWant []string
	}{
		{
			project: "brain-api", // paused on both
			want:    []string{"Manual/user tasks: PAUSED", "Automation-generated tasks: PAUSED", "nothing dispatches"},
		},
		{
			project: "tasks-only",
			want:    []string{"Manual/user tasks: PAUSED", "Automation-generated tasks: running", "automation-generated tasks still dispatch"},
		},
		{
			project: "autos-only",
			want:    []string{"Manual/user tasks: running", "Automation-generated tasks: PAUSED", "manual tasks still dispatch"},
		},
		{
			project: "never-paused",
			// The important case: not paused, so if work still is not running the
			// agent must be pointed somewhere else rather than left concluding
			// "not paused" means "should be running".
			want:    []string{"not paused on either axis", "scheduler_status"},
			notWant: []string{"PAUSED"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.project, func(t *testing.T) {
			out := formatRunnerStatus(status, tt.project)
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q:\n%s", w, out)
				}
			}
			for _, nw := range tt.notWant {
				if strings.Contains(out, nw) {
					t.Errorf("unexpectedly contains %q:\n%s", nw, out)
				}
			}
		})
	}
}

// TestFormatRunnerStatus_DoesNotClaimAGlobalPause is the regression guard for the
// finding that motivated this change. RunnerStatusResponse.Paused is built as
// len(pausedProjects) > 0, so one stale test fixture makes it true while every
// real project dispatches normally. Rendering it as "- Paused: true" invited the
// reading "the runner is paused", which was verified false against production.
func TestFormatRunnerStatus_DoesNotClaimAGlobalPause(t *testing.T) {
	out := formatRunnerStatus(types.RunnerStatusResponse{
		Running:           true,
		Paused:            true, // any() over one dead fixture
		PausedProjects:    []string{"_test-1772027096679"},
		AutomationsPaused: false,
	}, "")

	if strings.Contains(out, "- Paused: true") {
		t.Errorf("renders the any() flag as a global pause:\n%s", out)
	}
	if !strings.Contains(out, "Projects with task execution paused: 1") {
		t.Errorf("should report the count instead:\n%s", out)
	}
	// A project nobody paused must read as running.
	if got := formatRunnerStatus(types.RunnerStatusResponse{
		Paused:         true,
		PausedProjects: []string{"_test-1772027096679"},
	}, "supernote"); !strings.Contains(got, "not paused on either axis") {
		t.Errorf("an unpaused project must not inherit the any() flag:\n%s", got)
	}
}

// TestCapList_DisclosesWhatItWithheld — a truncated list that looks complete is
// the same class of lie this whole effort is about.
func TestCapList_DisclosesWhatItWithheld(t *testing.T) {
	if got := capList(nil); got != "(none)" {
		t.Errorf("capList(nil) = %q, want (none)", got)
	}

	many := make([]string, runnerStatusListCap+5)
	for i := range many {
		many[i] = fmt.Sprintf("p%02d", i)
	}
	got := capList(many)
	if !strings.Contains(got, "(+5 more)") {
		t.Errorf("capList must disclose the withheld count, got: %s", got)
	}

	exact := many[:runnerStatusListCap]
	if strings.Contains(capList(exact), "more") {
		t.Errorf("a list at exactly the cap is complete and must not claim more: %s", capList(exact))
	}
}

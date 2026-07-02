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

func TestRegisterControlTools_CountNamesHandlersDescriptions(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterControlTools(s, client)

	expected := []string{
		"runner_pause_project",
		"runner_resume_project",
		"runner_pause_all",
		"runner_resume_all",
		"control_send_prompt",
		"control_abort_session",
		"control_permission",
		"control_spawn_instance",
		"control_kill_instance",
	}
	if len(s.tools) != len(expected) {
		t.Fatalf("expected %d control tools registered, got %d", len(expected), len(s.tools))
	}
	for _, name := range expected {
		rt, ok := s.tools[name]
		if !ok {
			t.Fatalf("tool %q not registered", name)
		}
		if rt.handler == nil {
			t.Errorf("tool %q has nil handler", name)
		}
		desc := strings.ToLower(rt.tool.Description)
		if strings.TrimSpace(desc) == "" {
			t.Errorf("tool %q has empty description", name)
		}
		for _, want := range []string{"side effect", "requires"} {
			if !strings.Contains(desc, want) {
				t.Errorf("tool %q description %q missing explicit warning %q", name, rt.tool.Description, want)
			}
		}
		if rt.tool.InputSchema.Type != "object" {
			t.Errorf("tool %q inputSchema.type = %q, want object", name, rt.tool.InputSchema.Type)
		}
	}
}

func TestControlToolSchemas(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterControlTools(s, client)

	tests := []struct {
		tool     string
		required []string
		props    []string
	}{
		{"runner_pause_project", []string{"projectId"}, []string{"projectId"}},
		{"runner_resume_project", []string{"projectId"}, []string{"projectId"}},
		{"runner_pause_all", []string{"confirm"}, []string{"confirm"}},
		{"runner_resume_all", []string{"confirm"}, []string{"confirm"}},
		{"control_send_prompt", []string{"runnerId", "instanceId", "sessionId", "text"}, []string{"runnerId", "instanceId", "sessionId", "text", "agent", "providerID", "modelID"}},
		{"control_abort_session", []string{"runnerId", "instanceId", "sessionId"}, []string{"runnerId", "instanceId", "sessionId"}},
		{"control_permission", []string{"runnerId", "instanceId", "sessionId", "permissionId", "response"}, []string{"runnerId", "instanceId", "sessionId", "permissionId", "response", "remember"}},
		{"control_spawn_instance", []string{"runnerId", "workdir"}, []string{"runnerId", "workdir", "agent", "model", "title"}},
		{"control_kill_instance", []string{"runnerId", "instanceId", "confirm"}, []string{"runnerId", "instanceId", "confirm"}},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			tool := s.tools[tt.tool].tool
			for _, req := range tt.required {
				if !containsString(tool.InputSchema.Required, req) {
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

func TestControlTools_RequestMethodsPathsBodiesAndFormatting(t *testing.T) {
	type recordedRequest struct {
		Method string
		Path   string
		Body   string
	}
	var requests []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if r.URL.RawQuery != "" {
			t.Fatalf("unexpected query for %s %s: %s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		requests = append(requests, recordedRequest{Method: r.Method, Path: r.URL.Path, Body: string(body)})
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v1/tasks/runner/pause/brain":
			json.NewEncoder(w).Encode(map[string]any{"success": true})
		case "/api/v1/tasks/runner/resume/brain":
			json.NewEncoder(w).Encode(map[string]any{"success": true})
		case "/api/v1/tasks/runner/pause":
			json.NewEncoder(w).Encode(map[string]any{"success": true})
		case "/api/v1/tasks/runner/resume":
			json.NewEncoder(w).Encode(map[string]any{"success": true})
		case "/api/v1/control/runners/runner-1/instances/inst-1/sessions/ses-1/prompt":
			json.NewEncoder(w).Encode(map[string]any{"success": true, "queued": true})
		case "/api/v1/control/runners/runner-1/instances/inst-1/sessions/ses-1/abort":
			json.NewEncoder(w).Encode(map[string]any{"success": true})
		case "/api/v1/control/runners/runner-1/instances/inst-1/sessions/ses-1/permissions/perm-1":
			json.NewEncoder(w).Encode(map[string]any{"success": true})
		case "/api/v1/control/runners/runner-1/instances":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"success": true, "instance": map[string]any{"instance_id": "inst-new", "runner_id": "runner-1", "status": "starting"}})
		case "/api/v1/control/runners/runner-1/instances/inst-new":
			json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterControlTools(s, client)

	calls := []struct {
		tool string
		args map[string]any
		want []string
	}{
		{"runner_pause_project", map[string]any{"projectId": "brain"}, []string{"Paused runner execution for project brain"}},
		{"runner_resume_project", map[string]any{"projectId": "brain"}, []string{"Resumed runner execution for project brain"}},
		{"runner_pause_all", map[string]any{"confirm": true}, []string{"Paused runner execution for all projects"}},
		{"runner_resume_all", map[string]any{"confirm": true}, []string{"Resumed runner execution for all projects"}},
		{"control_send_prompt", map[string]any{"runnerId": "runner-1", "instanceId": "inst-1", "sessionId": "ses-1", "text": "continue", "agent": "dev", "providerID": "anthropic", "modelID": "claude"}, []string{"Sent prompt to session ses-1", "runner-1", "inst-1"}},
		{"control_abort_session", map[string]any{"runnerId": "runner-1", "instanceId": "inst-1", "sessionId": "ses-1"}, []string{"Abort requested for session ses-1", "runner-1", "inst-1"}},
		{"control_permission", map[string]any{"runnerId": "runner-1", "instanceId": "inst-1", "sessionId": "ses-1", "permissionId": "perm-1", "response": "allow", "remember": "once"}, []string{"Responded allow to permission perm-1", "session ses-1"}},
		{"control_spawn_instance", map[string]any{"runnerId": "runner-1", "workdir": "/tmp/brain", "agent": "dev", "model": "anthropic/claude", "title": "Scratch"}, []string{"Spawned control instance on runner runner-1", "inst-new", "/tmp/brain"}},
		{"control_kill_instance", map[string]any{"runnerId": "runner-1", "instanceId": "inst-new", "confirm": true}, []string{"Killed control instance inst-new", "runner-1"}},
	}
	for _, call := range calls {
		t.Run(call.tool, func(t *testing.T) {
			got, err := s.tools[call.tool].handler(context.Background(), call.args)
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			for _, want := range call.want {
				if !strings.Contains(got, want) {
					t.Fatalf("result = %q, want substring %q", got, want)
				}
			}
		})
	}

	want := []recordedRequest{
		{Method: "POST", Path: "/api/v1/tasks/runner/pause/brain", Body: "{}"},
		{Method: "POST", Path: "/api/v1/tasks/runner/resume/brain", Body: "{}"},
		{Method: "POST", Path: "/api/v1/tasks/runner/pause", Body: "{}"},
		{Method: "POST", Path: "/api/v1/tasks/runner/resume", Body: "{}"},
		{Method: "POST", Path: "/api/v1/control/runners/runner-1/instances/inst-1/sessions/ses-1/prompt", Body: `{"agent":"dev","model":{"modelID":"claude","providerID":"anthropic"},"text":"continue"}`},
		{Method: "POST", Path: "/api/v1/control/runners/runner-1/instances/inst-1/sessions/ses-1/abort", Body: "{}"},
		{Method: "POST", Path: "/api/v1/control/runners/runner-1/instances/inst-1/sessions/ses-1/permissions/perm-1", Body: `{"remember":"once","response":"allow"}`},
		{Method: "POST", Path: "/api/v1/control/runners/runner-1/instances", Body: `{"agent":"dev","model":"anthropic/claude","title":"Scratch","workdir":"/tmp/brain"}`},
		{Method: "DELETE", Path: "/api/v1/control/runners/runner-1/instances/inst-new", Body: ""},
	}
	if len(requests) != len(want) {
		t.Fatalf("requests = %#v, want %#v", requests, want)
	}
	for i := range want {
		if requests[i] != want[i] {
			t.Errorf("request[%d] = %#v, want %#v", i, requests[i], want[i])
		}
	}
}

func TestControlTools_ValidationAndConfirmation(t *testing.T) {
	hitServer := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitServer = true
		t.Fatalf("unexpected network call for invalid args: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterControlTools(s, client)

	tests := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"runner_pause_project", map[string]any{}, "projectId is required"},
		{"runner_resume_project", map[string]any{}, "projectId is required"},
		{"runner_pause_all", map[string]any{}, "confirm=true is required"},
		{"runner_resume_all", map[string]any{"confirm": false}, "confirm=true is required"},
		{"control_send_prompt", map[string]any{"runnerId": "runner-1", "instanceId": "inst-1", "sessionId": "ses-1"}, "text is required"},
		{"control_abort_session", map[string]any{"runnerId": "runner-1", "instanceId": "inst-1"}, "sessionId is required"},
		{"control_permission", map[string]any{"runnerId": "runner-1", "instanceId": "inst-1", "sessionId": "ses-1", "permissionId": "perm-1", "response": "maybe"}, "response must be allow or deny"},
		{"control_permission", map[string]any{"runnerId": "runner-1", "instanceId": "inst-1", "sessionId": "ses-1", "permissionId": "perm-1", "response": "allow", "remember": "forever"}, "remember must be once or always"},
		{"control_spawn_instance", map[string]any{"runnerId": "runner-1", "workdir": "relative/path"}, "workdir must be an absolute path"},
		{"control_kill_instance", map[string]any{"runnerId": "runner-1", "instanceId": "inst-1"}, "confirm=true is required"},
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
	if hitServer {
		t.Fatal("invalid validation cases made a network call")
	}
}

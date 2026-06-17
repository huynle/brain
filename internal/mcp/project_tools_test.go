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

func TestRegisterProjectTools_CountNamesHandlersDescriptions(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterProjectTools(s, client)

	expected := []string{
		"brain_context_resolve",
		"brain_project_placement_get",
		"brain_project_placement_put",
	}
	if len(s.tools) != len(expected) {
		t.Fatalf("expected %d project tools registered, got %d", len(expected), len(s.tools))
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

func TestProjectToolSchemas(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterProjectTools(s, client)

	tests := []struct {
		tool     string
		required []string
		props    []string
	}{
		{"brain_context_resolve", []string{"client_id", "host_id"}, []string{"client_id", "host_id", "kind", "hostname", "os", "arch", "username", "home_dir", "labels", "capabilities", "path", "git_root", "git_common_dir", "git_worktree_main", "git_branch", "git_remote", "folder_name"}},
		{"brain_project_placement_get", []string{"project"}, []string{"project"}},
		{"brain_project_placement_put", []string{"project"}, []string{"project", "affinity", "preferred_machines", "allowed_machines", "workspace_policy", "required_labels", "required_capabilities", "resources"}},
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

func TestProjectTools_RequestBodiesPathsQueriesAndFormatting(t *testing.T) {
	type seenRequest struct {
		Method     string
		RequestURI string
		Body       map[string]any
	}
	var requests []seenRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		seen := seenRequest{Method: r.Method, RequestURI: r.URL.RequestURI()}
		if len(bodyBytes) > 0 {
			if err := json.Unmarshal(bodyBytes, &seen.Body); err != nil {
				t.Fatalf("decode body for %s %s: %v", r.Method, r.URL.RequestURI(), err)
			}
		}
		requests = append(requests, seen)
		w.Header().Set("Content-Type", "application/json")

		switch r.Method + " " + r.URL.RequestURI() {
		case "POST /api/v1/context/resolve":
			client := seen.Body["client"].(map[string]any)
			workspace := seen.Body["workspace"].(map[string]any)
			if client["client_id"] != "client-1" || client["host_id"] != "host-1" || client["kind"] != "opencode" {
				t.Fatalf("unexpected client body: %#v", client)
			}
			labels := client["labels"].(map[string]any)
			if labels["tier"] != "dev" {
				t.Fatalf("unexpected labels: %#v", labels)
			}
			if workspace["path"] != "/Users/me/code/brain" || workspace["git_branch"] != "feature/context" || workspace["folder_name"] != "brain" {
				t.Fatalf("unexpected workspace body: %#v", workspace)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"project_id": "brain-api",
				"confidence": "high",
				"source":     "workspace_git_remote",
				"dream": map[string]any{
					"id": "dream-1", "title": "Latest brain dream", "path": "projects/brain-api/dream/latest.md",
				},
			})
		case "GET /api/v1/projects/brain%20api/placement":
			if len(bodyBytes) != 0 {
				t.Fatalf("unexpected GET body: %s", bodyBytes)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"project_id":            "brain api",
				"affinity":              "soft",
				"preferred_machines":    []string{"mac-a"},
				"allowed_machines":      []string{"mac-a", "mac-b"},
				"workspace_policy":      "worktree",
				"required_labels":       map[string]string{"tier": "dev"},
				"required_capabilities": []string{"go", "mcp"},
				"resources":             map[string]any{"cpu": float64(2)},
			})
		case "PUT /api/v1/projects/brain%20api/placement":
			if seen.Body["affinity"] != "strict" || seen.Body["workspace_policy"] != "current_branch" {
				t.Fatalf("unexpected placement body: %#v", seen.Body)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"project_id":            "brain api",
				"affinity":              "strict",
				"preferred_machines":    []string{"mac-c"},
				"allowed_machines":      []string{"mac-c"},
				"workspace_policy":      "current_branch",
				"required_labels":       map[string]string{"gpu": "false"},
				"required_capabilities": []string{"pi"},
				"resources":             map[string]any{"memory_gb": float64(16)},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterProjectTools(s, client)

	contextResult, err := s.tools["brain_context_resolve"].handler(context.Background(), map[string]any{
		"client_id": "client-1", "host_id": "host-1", "kind": "opencode", "hostname": "devbox",
		"os": "darwin", "arch": "arm64", "username": "me", "home_dir": "/Users/me",
		"labels": map[string]any{"tier": "dev"}, "capabilities": []any{"go", "mcp"},
		"path": "/Users/me/code/brain", "git_root": "/Users/me/code/brain", "git_common_dir": "/Users/me/code/brain/.git",
		"git_worktree_main": "/Users/me/code/brain", "git_branch": "feature/context", "git_remote": "git@example.com:brain.git", "folder_name": "brain",
	})
	if err != nil {
		t.Fatalf("context handler error: %v", err)
	}
	for _, want := range []string{"Context resolution", "Project: brain-api", "Confidence: high", "Source: workspace_git_remote", "Client: client-1", "Host: host-1", "Kind: opencode", "Workspace: /Users/me/code/brain", "Git branch: feature/context", "Dream: Latest brain dream", "projects/brain-api/dream/latest.md"} {
		if !strings.Contains(contextResult, want) {
			t.Errorf("context result missing %q:\n%s", want, contextResult)
		}
	}

	placementResult, err := s.tools["brain_project_placement_get"].handler(context.Background(), map[string]any{"project": "brain api"})
	if err != nil {
		t.Fatalf("placement get handler error: %v", err)
	}
	for _, want := range []string{"Project placement", "Project: brain api", "Affinity: soft", "Preferred machines: mac-a", "Allowed machines: mac-a, mac-b", "Workspace policy: worktree", "Required labels: tier=dev", "Required capabilities: go, mcp", "Resources", "cpu: 2"} {
		if !strings.Contains(placementResult, want) {
			t.Errorf("placement get result missing %q:\n%s", want, placementResult)
		}
	}

	putResult, err := s.tools["brain_project_placement_put"].handler(context.Background(), map[string]any{
		"project": "brain api", "affinity": "strict", "preferred_machines": []any{"mac-c"}, "allowed_machines": []any{"mac-c"},
		"workspace_policy": "current_branch", "required_labels": map[string]any{"gpu": "false"}, "required_capabilities": []any{"pi"}, "resources": map[string]any{"memory_gb": float64(16)},
	})
	if err != nil {
		t.Fatalf("placement put handler error: %v", err)
	}
	for _, want := range []string{"Project placement updated", "Project: brain api", "Affinity: strict", "Preferred machines: mac-c", "Workspace policy: current_branch", "Required labels: gpu=false", "Required capabilities: pi", "memory_gb: 16"} {
		if !strings.Contains(putResult, want) {
			t.Errorf("placement put result missing %q:\n%s", want, putResult)
		}
	}

	wantRequests := []string{
		"POST /api/v1/context/resolve",
		"GET /api/v1/projects/brain%20api/placement",
		"PUT /api/v1/projects/brain%20api/placement",
	}
	if len(requests) != len(wantRequests) {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
	for i := range wantRequests {
		got := requests[i].Method + " " + requests[i].RequestURI
		if got != wantRequests[i] {
			t.Errorf("request[%d] = %q, want %q", i, got, wantRequests[i])
		}
	}
}

func TestProjectTools_ValidationErrors(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:1")
	RegisterProjectTools(s, client)

	tests := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"brain_context_resolve", map[string]any{"host_id": "host-1"}, "client_id"},
		{"brain_context_resolve", map[string]any{"client_id": "client-1"}, "host_id"},
		{"brain_project_placement_get", map[string]any{}, "project"},
		{"brain_project_placement_put", map[string]any{}, "project"},
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

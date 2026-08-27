package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

func TestResolveProjectList_SingleProject(t *testing.T) {
	// Single project should return immediately without API call
	cfg := runner.RunnerConfig{}
	projects, err := resolveProjectList("my-project", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 1 || projects[0] != "my-project" {
		t.Fatalf("expected [my-project], got %v", projects)
	}
}

func TestResolveProjectList_AllProjects(t *testing.T) {
	// Mock API server returning project list
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := types.ProjectListResponse{
			Projects: []string{"prod-api", "prod-web", "test-staging"},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := runner.RunnerConfig{
		BrainAPIURL: srv.URL,
	}

	projects, err := resolveProjectList("all", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 3 {
		t.Fatalf("expected 3 projects, got %d: %v", len(projects), projects)
	}
}

func TestResolveProjectList_WithIncludeFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := types.ProjectListResponse{
			Projects: []string{"prod-api", "prod-web", "test-staging"},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := runner.RunnerConfig{
		BrainAPIURL:     srv.URL,
		IncludeProjects: []string{"prod-*"},
	}

	projects, err := resolveProjectList("all", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d: %v", len(projects), projects)
	}
	for _, p := range projects {
		if p != "prod-api" && p != "prod-web" {
			t.Errorf("unexpected project: %s", p)
		}
	}
}

func TestResolveProjectList_WithExcludeFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := types.ProjectListResponse{
			Projects: []string{"prod-api", "prod-web", "test-staging"},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := runner.RunnerConfig{
		BrainAPIURL:     srv.URL,
		ExcludeProjects: []string{"test-*"},
	}

	projects, err := resolveProjectList("all", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d: %v", len(projects), projects)
	}
}

func TestResolveProjectList_AllFiltered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := types.ProjectListResponse{
			Projects: []string{"test-a", "test-b"},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := runner.RunnerConfig{
		BrainAPIURL:     srv.URL,
		ExcludeProjects: []string{"test-*"},
	}

	_, err := resolveProjectList("all", cfg)
	if err == nil {
		t.Fatal("expected error when all projects filtered out")
	}
	if err.Error() != "all projects filtered out by include/exclude patterns" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestResolveProjectList_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := types.ProjectListResponse{
			Projects: []string{},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := runner.RunnerConfig{
		BrainAPIURL: srv.URL,
	}

	_, err := resolveProjectList("all", cfg)
	if err == nil {
		t.Fatal("expected error when no projects found")
	}
	if err.Error() != "no projects found" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestResolveProjectList_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	cfg := runner.RunnerConfig{
		BrainAPIURL: srv.URL,
	}

	_, err := resolveProjectList("all", cfg)
	if err == nil {
		t.Fatal("expected error when API returns error")
	}
}

// =============================================================================
// Helper to build a RunCommand with a mock server
// =============================================================================

// =============================================================================
// makeAPIClient tests
// =============================================================================

func TestRunCommand_MakeAPIClient_NoURL(t *testing.T) {
	cmd := &RunCommand{
		Config: &UnifiedConfig{},
		Flags:  &RunnerFlags{},
	}
	_, err := cmd.makeAPIClient()
	if err == nil {
		t.Fatal("expected error when BRAIN_API_URL not configured")
	}
	if !strings.Contains(err.Error(), "BRAIN_API_URL not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCommand_MakeAPIClient_FallbackToMCP(t *testing.T) {
	cmd := &RunCommand{
		Config: &UnifiedConfig{},
		Flags:  &RunnerFlags{},
	}
	cmd.Config.MCP.APIURL = "http://localhost:9999"
	client, err := cmd.makeAPIClient()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

// =============================================================================
// runList tests
// =============================================================================

func TestRunList_AllProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/tasks":
			json.NewEncoder(w).Encode(types.ProjectListResponse{
				Projects: []string{"proj-a", "proj-b"},
			})
		case "/api/v1/tasks/proj-a":
			json.NewEncoder(w).Encode(struct {
				Tasks []types.ResolvedTask `json:"tasks"`
			}{
				Tasks: []types.ResolvedTask{
					{ID: "t1", Title: "Task 1", Status: "pending", Classification: "ready"},
					{ID: "t2", Title: "Task 2", Status: "pending", Classification: "blocked"},
				},
			})
		case "/api/v1/tasks/proj-b":
			json.NewEncoder(w).Encode(struct {
				Tasks []types.ResolvedTask `json:"tasks"`
			}{
				Tasks: []types.ResolvedTask{
					{ID: "t3", Title: "Task 3", Status: "completed", Classification: "ready"},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cmd := &RunCommand{
		Subcommand: "list",
		Project:    "all",
		Config: &UnifiedConfig{
			Runner: runner.RunnerConfig{BrainAPIURL: srv.URL},
		},
		Flags: &RunnerFlags{},
	}

	err := cmd.runList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunList_SingleProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/my-proj" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(struct {
			Tasks []types.ResolvedTask `json:"tasks"`
		}{
			Tasks: []types.ResolvedTask{
				{ID: "abc123", Title: "Do something", Status: "pending", Priority: "high", FeatureID: "feat-1"},
			},
		})
	}))
	defer srv.Close()

	cmd := &RunCommand{
		Subcommand: "list",
		Project:    "my-proj",
		Config: &UnifiedConfig{
			Runner: runner.RunnerConfig{BrainAPIURL: srv.URL},
		},
		Flags: &RunnerFlags{},
	}

	err := cmd.runList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunList_EmptyProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(struct {
			Tasks []types.ResolvedTask `json:"tasks"`
		}{
			Tasks: []types.ResolvedTask{},
		})
	}))
	defer srv.Close()

	cmd := &RunCommand{
		Subcommand: "list",
		Project:    "empty-proj",
		Config: &UnifiedConfig{
			Runner: runner.RunnerConfig{BrainAPIURL: srv.URL},
		},
		Flags: &RunnerFlags{},
	}

	err := cmd.runList()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// =============================================================================
// runStop tests
// =============================================================================

func TestRunStop_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/runner/pause" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cmd := &RunCommand{
		Subcommand: "stop",
		Config: &UnifiedConfig{
			Runner: runner.RunnerConfig{BrainAPIURL: srv.URL},
		},
		Flags: &RunnerFlags{},
	}

	err := cmd.runStop()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunStop_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	cmd := &RunCommand{
		Subcommand: "stop",
		Config: &UnifiedConfig{
			Runner: runner.RunnerConfig{BrainAPIURL: srv.URL},
		},
		Flags: &RunnerFlags{},
	}

	err := cmd.runStop()
	if err == nil {
		t.Fatal("expected error on API failure")
	}
}

// =============================================================================
// runStatus tests
// =============================================================================

func TestRunStatus_Running(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/tasks/runner/status":
			json.NewEncoder(w).Encode(types.RunnerStatusResponse{
				Running: true,
				Paused:  false,
			})
		case "/api/v1/runners":
			json.NewEncoder(w).Encode(types.RunnerListResponse{
				Runners: []types.RunnerInfo{
					{RunnerID: "runner-1", Hostname: "host-a", Status: "online", MaxParallel: 3},
				},
				Total: 1,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cmd := &RunCommand{
		Subcommand: "status",
		Config: &UnifiedConfig{
			Runner: runner.RunnerConfig{BrainAPIURL: srv.URL},
		},
		Flags: &RunnerFlags{},
	}

	err := cmd.runStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunStatus_Paused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/tasks/runner/status":
			json.NewEncoder(w).Encode(types.RunnerStatusResponse{
				Running:        true,
				Paused:         true,
				PausedProjects: []string{"proj-a"},
			})
		case "/api/v1/runners":
			json.NewEncoder(w).Encode(types.RunnerListResponse{
				Runners: []types.RunnerInfo{},
				Total:   0,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cmd := &RunCommand{
		Subcommand: "status",
		Config: &UnifiedConfig{
			Runner: runner.RunnerConfig{BrainAPIURL: srv.URL},
		},
		Flags: &RunnerFlags{},
	}

	err := cmd.runStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// =============================================================================
// runReady tests
// =============================================================================

func TestRunReady_WithTasks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/my-proj/ready" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(types.TaskListResponse{
			Tasks: []types.ResolvedTask{
				{ID: "t1", Title: "Ready Task", Priority: "high"},
				{ID: "t2", Title: "Another Ready Task", Priority: "medium", FeatureID: "feat-x"},
			},
		})
	}))
	defer srv.Close()

	cmd := &RunCommand{
		Subcommand: "ready",
		Project:    "my-proj",
		Config: &UnifiedConfig{
			Runner: runner.RunnerConfig{BrainAPIURL: srv.URL},
		},
		Flags: &RunnerFlags{},
	}

	err := cmd.runReady()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunReady_NoProject(t *testing.T) {
	cmd := &RunCommand{
		Subcommand: "ready",
		Project:    "",
		Config: &UnifiedConfig{
			Runner: runner.RunnerConfig{BrainAPIURL: "http://localhost:9999"},
		},
		Flags: &RunnerFlags{},
	}

	err := cmd.runReady()
	if err == nil {
		t.Fatal("expected error when no project specified")
	}
	if !strings.Contains(err.Error(), "project required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunReady_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(types.TaskListResponse{
			Tasks: []types.ResolvedTask{},
		})
	}))
	defer srv.Close()

	cmd := &RunCommand{
		Subcommand: "ready",
		Project:    "my-proj",
		Config: &UnifiedConfig{
			Runner: runner.RunnerConfig{BrainAPIURL: srv.URL},
		},
		Flags: &RunnerFlags{},
	}

	err := cmd.runReady()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// =============================================================================
// Stub message tests (features/logs/config)
// =============================================================================

func TestRunFeatures_StubMessage(t *testing.T) {
	cmd := &RunCommand{Subcommand: "features"}
	err := cmd.runFeatures()
	if err == nil {
		t.Fatal("expected error from stub")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "/api/v1/tasks") {
		t.Fatalf("stub should mention API endpoint, got: %v", err)
	}
}

func TestRunLogs_StubMessage(t *testing.T) {
	cmd := &RunCommand{Subcommand: "logs"}
	err := cmd.runLogs()
	if err == nil {
		t.Fatal("expected error from stub")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunConfig_StubMessage(t *testing.T) {
	cmd := &RunCommand{Subcommand: "config"}
	err := cmd.runConfig()
	if err == nil {
		t.Fatal("expected error from stub")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Fatalf("unexpected error: %v", err)
	}
}

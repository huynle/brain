package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

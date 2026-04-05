package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huynle/brain-api/internal/config"
)

func TestTaskDefaultsEndpoint(t *testing.T) {
	boolPtr := func(v bool) *bool { return &v }

	cfg := testConfig()
	cfg.TaskDefaults = config.TaskDefaultsConfig{
		Agent:              "tdd-dev",
		Model:              "claude-sonnet-4-20250514",
		ExecutionMode:      "worktree",
		CompleteOnIdle:     boolPtr(true),
		MergePolicy:        "auto_merge",
		MergeStrategy:      "squash",
		MergeTargetBranch:  "main",
		RemoteBranchPolicy: "delete",
		OpenPRBeforeMerge:  boolPtr(false),
		TargetWorkdir:      "/tmp/work",
	}

	router := NewRouter(cfg)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/config/task-defaults")
	if err != nil {
		t.Fatalf("GET /api/v1/config/task-defaults failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var got taskDefaultsResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify all fields match configured defaults
	if got.Agent != "tdd-dev" {
		t.Errorf("agent = %q, want %q", got.Agent, "tdd-dev")
	}
	if got.Model != "claude-sonnet-4-20250514" {
		t.Errorf("model = %q, want %q", got.Model, "claude-sonnet-4-20250514")
	}
	if got.ExecutionMode != "worktree" {
		t.Errorf("execution_mode = %q, want %q", got.ExecutionMode, "worktree")
	}
	if got.CompleteOnIdle == nil || *got.CompleteOnIdle != true {
		t.Errorf("complete_on_idle = %v, want true", got.CompleteOnIdle)
	}
	if got.MergePolicy != "auto_merge" {
		t.Errorf("merge_policy = %q, want %q", got.MergePolicy, "auto_merge")
	}
	if got.MergeStrategy != "squash" {
		t.Errorf("merge_strategy = %q, want %q", got.MergeStrategy, "squash")
	}
	if got.MergeTargetBranch != "main" {
		t.Errorf("merge_target_branch = %q, want %q", got.MergeTargetBranch, "main")
	}
	if got.RemoteBranchPolicy != "delete" {
		t.Errorf("remote_branch_policy = %q, want %q", got.RemoteBranchPolicy, "delete")
	}
	if got.OpenPRBeforeMerge == nil || *got.OpenPRBeforeMerge != false {
		t.Errorf("open_pr_before_merge = %v, want false", got.OpenPRBeforeMerge)
	}
	if got.TargetWorkdir != "/tmp/work" {
		t.Errorf("target_workdir = %q, want %q", got.TargetWorkdir, "/tmp/work")
	}
}

func TestTaskDefaultsEndpoint_EmptyDefaults(t *testing.T) {
	cfg := testConfig()
	// Leave TaskDefaults as zero value

	router := NewRouter(cfg)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/config/task-defaults")
	if err != nil {
		t.Fatalf("GET /api/v1/config/task-defaults failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var got taskDefaultsResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Empty/nil fields should be returned as empty strings or null
	if got.Agent != "" {
		t.Errorf("agent = %q, want empty string", got.Agent)
	}
	if got.Model != "" {
		t.Errorf("model = %q, want empty string", got.Model)
	}
	if got.CompleteOnIdle != nil {
		t.Errorf("complete_on_idle = %v, want nil", got.CompleteOnIdle)
	}
	if got.OpenPRBeforeMerge != nil {
		t.Errorf("open_pr_before_merge = %v, want nil", got.OpenPRBeforeMerge)
	}
}

func TestTaskDefaultsEndpoint_AuthRequired(t *testing.T) {
	cfg := testConfig()
	cfg.EnableAuth = true

	validator := &mockTokenValidator{validToken: "test-secret-key"}
	router := NewRouter(cfg, WithTokenValidator(validator))
	srv := httptest.NewServer(router)
	defer srv.Close()

	// No auth token → 401
	t.Run("no auth returns 401", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/v1/config/task-defaults")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}
	})

	// Valid auth token → 200
	t.Run("valid auth returns 200", func(t *testing.T) {
		req, _ := http.NewRequest("GET", srv.URL+"/api/v1/config/task-defaults", nil)
		req.Header.Set("Authorization", "Bearer test-secret-key")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})
}

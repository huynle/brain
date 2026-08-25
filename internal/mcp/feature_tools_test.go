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

func TestRegisterFeatureTools_CountNamesHandlersDescriptions(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterFeatureTools(s, client)

	expected := []string{
		"features",
		"feature_ready",
		"feature_get",
		"feature_checkout",
		"feature_assign",
		"feature_clear_assignment",
	}
	if len(s.tools) != len(expected) {
		t.Fatalf("expected %d feature tools registered, got %d", len(expected), len(s.tools))
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

func TestFeatureToolSchemas(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterFeatureTools(s, client)

	tests := []struct {
		tool     string
		required []string
		props    []string
	}{
		{"features", nil, []string{"project", "ready_only", "limit"}},
		{"feature_ready", nil, []string{"project", "limit"}},
		{"feature_get", []string{"feature_id"}, []string{"project", "feature_id"}},
		{"feature_checkout", []string{"feature_id"}, []string{"project", "feature_id", "execution_branch", "merge_target_branch", "merge_policy", "merge_strategy", "remote_branch_policy", "open_pr_before_merge", "execution_mode", "checkout_mode"}},
		{"feature_assign", []string{"feature_id", "runner_id"}, []string{"project", "feature_id", "runner_id", "intent", "force"}},
		{"feature_clear_assignment", []string{"feature_id"}, []string{"project", "feature_id", "intent"}},
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

func TestBrainFeatures_RequestAndFormatting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/tasks/test-project/features" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"features": []map[string]any{{
			"featureId": "auth-system",
			"ready":     true,
			"stats":     map[string]any{"total": 2, "ready": 1, "waiting": 0, "blocked": 0, "not_pending": 1},
			"tasks": []map[string]any{{
				"id": "task-1", "title": "Add auth", "status": "pending", "classification": "ready",
			}},
		}}})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterFeatureTools(s, client)

	result, err := s.tools["features"].handler(context.Background(), map[string]any{"project": "test-project"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	for _, want := range []string{"Features for project: test-project", "auth-system", "ready", "Tasks: 2", "Ready: 1", "Add auth", "task-1"} {
		if !strings.Contains(result, want) {
			t.Errorf("result missing %q:\n%s", want, result)
		}
	}
}

func TestBrainFeatures_ReadyOnlyUsesReadyEndpointAndLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/tasks/test-project/features/ready" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"features": []map[string]any{{"featureId": "one", "ready": true}, {"featureId": "two", "ready": true}}})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterFeatureTools(s, client)

	result, err := s.tools["features"].handler(context.Background(), map[string]any{"project": "test-project", "ready_only": true, "limit": float64(1)})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(result, "one") || strings.Contains(result, "two") {
		t.Fatalf("limit not applied to result:\n%s", result)
	}
}

func TestBrainFeatureReady_RequestAndEmptyFormatting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/tasks/test-project/features/ready" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"features": []map[string]any{}})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterFeatureTools(s, client)

	result, err := s.tools["feature_ready"].handler(context.Background(), map[string]any{"project": "test-project"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(result, "No ready features found") {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestBrainFeatureGet_RequestAndFormatting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/tasks/test-project/features/auth-system" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"feature": map[string]any{
			"featureId": "auth-system",
			"ready":     false,
			"stats":     map[string]any{"total": 2, "ready": 1, "waiting": 1, "blocked": 0, "not_pending": 0},
			"tasks": []map[string]any{
				{"id": "task-1", "title": "Ready task", "status": "pending", "classification": "ready"},
				{"id": "task-2", "title": "Waiting task", "status": "pending", "classification": "waiting", "waiting_on": []string{"task-1"}},
			},
		}})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterFeatureTools(s, client)

	result, err := s.tools["feature_get"].handler(context.Background(), map[string]any{"project": "test-project", "feature_id": "auth-system"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	// "Status: waiting" became "Dependencies: waiting on other features" — the
	// single Status line conflated feature-dependency state with whether any task
	// can actually run, and rendered the word "ready" in a different sense from
	// the task-level "ready" printed beside it.
	for _, want := range []string{"Feature auth-system", "Project: test-project", "Dependencies: waiting on other features", "Work: 1 task(s) ready to run", "Ready task", "Waiting task", "waiting_on: task-1"} {
		if !strings.Contains(result, want) {
			t.Errorf("result missing %q:\n%s", want, result)
		}
	}
}

func TestBrainFeatureCheckout_RequestBodyAndFormatting(t *testing.T) {
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/tasks/test-project/features/auth-system/checkout" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"created":      true,
			"generatedKey": "feature-checkout:auth-system",
			"task": map[string]any{
				"id": "checkout-1", "path": "projects/test/task/checkout-1.md", "title": "Review auth-system", "status": "pending",
			},
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterFeatureTools(s, client)

	result, err := s.tools["feature_checkout"].handler(context.Background(), map[string]any{
		"project": "test-project", "feature_id": "auth-system", "execution_branch": "feat/auth", "merge_target_branch": "main",
		"merge_policy": "auto_pr", "merge_strategy": "squash", "remote_branch_policy": "delete", "open_pr_before_merge": true, "execution_mode": "worktree",
		"checkout_mode": "simple",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	for _, key := range []string{"execution_branch", "merge_target_branch", "merge_policy", "merge_strategy", "remote_branch_policy", "open_pr_before_merge", "execution_mode", "checkout_mode"} {
		if _, ok := capturedBody[key]; !ok {
			t.Fatalf("body missing %q: %#v", key, capturedBody)
		}
	}
	if got := capturedBody["checkout_mode"]; got != "simple" {
		t.Fatalf("checkout_mode = %#v, want %q", got, "simple")
	}
	for _, want := range []string{"Feature checkout", "auth-system", "created: true", "feature-checkout:auth-system", "checkout-1", "Review auth-system"} {
		if !strings.Contains(result, want) {
			t.Errorf("result missing %q:\n%s", want, result)
		}
	}
}

func TestBrainFeatureAssignAndClear_RequestBodiesAndFormatting(t *testing.T) {
	requests := []string{}
	var assignBody map[string]any
	var clearBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/tasks/test-project/features/auth-system/assignment":
			if r.Method != http.MethodPut {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&assignBody); err != nil {
				t.Fatalf("decode assign body: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]any{"project_id": "test-project", "feature_id": "auth-system", "runner_id": "runner-1", "previous_runner": "runner-0", "source": "manual", "status": "assigned", "assigned_at": "2026-06-17T00:00:00Z"})
		case "/api/v1/tasks/test-project/features/auth-system/assignment/clear":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&clearBody); err != nil {
				t.Fatalf("decode clear body: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]any{"project_id": "test-project", "feature_id": "auth-system", "previous_runner": "runner-1", "source": "manual", "status": "cleared", "updated_at": "2026-06-17T00:01:00Z"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterFeatureTools(s, client)

	assigned, err := s.tools["feature_assign"].handler(context.Background(), map[string]any{"project": "test-project", "feature_id": "auth-system", "runner_id": "runner-1", "intent": "claim feature", "force": true})
	if err != nil {
		t.Fatalf("assign handler error: %v", err)
	}
	cleared, err := s.tools["feature_clear_assignment"].handler(context.Background(), map[string]any{"project": "test-project", "feature_id": "auth-system", "intent": "release feature"})
	if err != nil {
		t.Fatalf("clear handler error: %v", err)
	}

	if assignBody["runner_id"] != "runner-1" || assignBody["intent"] != "claim feature" || assignBody["force"] != true {
		t.Fatalf("unexpected assign body: %#v", assignBody)
	}
	if clearBody["intent"] != "release feature" {
		t.Fatalf("unexpected clear body: %#v", clearBody)
	}
	for _, want := range []string{"Feature assignment", "assigned", "runner-1", "runner-0", "manual"} {
		if !strings.Contains(assigned, want) {
			t.Errorf("assign result missing %q:\n%s", want, assigned)
		}
	}
	for _, want := range []string{"Feature assignment", "cleared", "runner-1", "manual"} {
		if !strings.Contains(cleared, want) {
			t.Errorf("clear result missing %q:\n%s", want, cleared)
		}
	}
	wantRequests := []string{"PUT /api/v1/tasks/test-project/features/auth-system/assignment", "POST /api/v1/tasks/test-project/features/auth-system/assignment/clear"}
	if len(requests) != len(wantRequests) {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
	for i := range wantRequests {
		if requests[i] != wantRequests[i] {
			t.Errorf("request[%d] = %q, want %q", i, requests[i], wantRequests[i])
		}
	}
}

func TestFeatureTools_ValidationErrors(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:1")
	RegisterFeatureTools(s, client)

	tests := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"feature_get", map[string]any{}, "feature_id"},
		{"feature_checkout", map[string]any{}, "feature_id"},
		{"feature_assign", map[string]any{"feature_id": "auth"}, "runner_id"},
		{"feature_clear_assignment", map[string]any{}, "feature_id"},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			result, err := s.tools[tt.tool].handler(context.Background(), tt.args)
			if err == nil {
				t.Fatalf("expected validation error, got result %q", result)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

// TestFormatFeatureGitLines_ShowsWhereWorkLands pins that a feature reports
// its branch and merge configuration.
//
// ResolvedTask carries nine git/merge fields and feature_get rendered none
// of them, so "what branch is this feature on and what does it merge into?"
// — a question anyone orchestrating work has to answer — could not be
// answered through MCP at all, despite the data being decoded and in hand.
func TestFormatFeatureGitLines_ShowsWhereWorkLands(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "a", GitBranch: "feat-x", MergeTargetBranch: "main", MergePolicy: "auto_pr", MergeStrategy: "squash", ExecutionMode: "worktree", TargetWorkdir: "/repo"},
		{ID: "b", GitBranch: "feat-x", MergeTargetBranch: "main", MergePolicy: "auto_pr", MergeStrategy: "squash", ExecutionMode: "worktree", TargetWorkdir: "/repo"},
	}
	out := strings.Join(formatFeatureGitLines(tasks), "\n")

	for _, want := range []string{
		"### Git & merge",
		"- branch: feat-x",
		"- merges into: main",
		"- merge policy: auto_pr",
		"- merge strategy: squash",
		"- execution mode: worktree",
		"- workdir: /repo",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "disagree") {
		t.Errorf("uniform tasks must not be reported as disagreeing:\n%s", out)
	}
}

// TestFormatFeatureGitLines_FlagsDivergence pins the failure this section
// exists to surface. The model is one feature, one branch, one merge — but
// nothing enforces it, so tasks can quietly carry different merge targets
// and the checkout then misbehaves with no visible cause.
func TestFormatFeatureGitLines_FlagsDivergence(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "a", GitBranch: "feat-x", MergeTargetBranch: "main"},
		{ID: "b", GitBranch: "feat-x", MergeTargetBranch: "develop"},
		{ID: "c", GitBranch: "feat-y", MergeTargetBranch: "main"},
	}
	out := strings.Join(formatFeatureGitLines(tasks), "\n")

	if !strings.Contains(out, "- branch: ⚠ tasks disagree — feat-x, feat-y") {
		t.Errorf("branch divergence not flagged:\n%s", out)
	}
	if !strings.Contains(out, "- merges into: ⚠ tasks disagree — develop, main") {
		t.Errorf("merge-target divergence not flagged:\n%s", out)
	}
}

// TestFormatFeatureGitLines_ReportsUnsetLandingFields pins that the two
// fields whose absence changes what happens at checkout are called out,
// rather than silently omitted like the optional ones.
func TestFormatFeatureGitLines_ReportsUnsetLandingFields(t *testing.T) {
	out := strings.Join(formatFeatureGitLines([]types.ResolvedTask{{ID: "a"}}), "\n")

	if !strings.Contains(out, "- branch: (unset)") {
		t.Errorf("unset branch should be reported:\n%s", out)
	}
	if !strings.Contains(out, "- merges into: (unset)") {
		t.Errorf("unset merge target should be reported:\n%s", out)
	}
	if strings.Contains(out, "git remote") {
		t.Errorf("optional unset fields should stay quiet:\n%s", out)
	}
}

// TestFormatFeatureGitLines_EmptyFeature pins that a feature with no tasks
// renders no git section at all rather than a header over nothing.
func TestFormatFeatureGitLines_EmptyFeature(t *testing.T) {
	if got := formatFeatureGitLines(nil); got != nil {
		t.Errorf("expected no lines for an empty feature, got: %v", got)
	}
}

// TestFeatureWorkState_DistinguishesFinishedFromUnstarted is the core of the
// fix. Observed live on brain-api: a feature whose 13 tasks were all completed,
// one whose 15 were all still draft, and one with genuinely runnable work ALL
// rendered "Status: ready", because that word reported feature-dependency state
// while sitting one line above a stats line reading "Ready: 0".
func TestFeatureWorkState_DistinguishesFinishedFromUnstarted(t *testing.T) {
	tests := []struct {
		name  string
		stats *types.TaskStats
		want  string
	}{
		{"runnable work", &types.TaskStats{Total: 6, Ready: 4, Waiting: 2}, "4 task(s) ready to run"},
		{"all waiting on deps", &types.TaskStats{Total: 3, Waiting: 3}, "3 waiting on dependencies"},
		{"blocked", &types.TaskStats{Total: 2, Blocked: 2}, "tasks are blocked"},
		{"all outside pending", &types.TaskStats{Total: 15, NotPending: 15}, "all 15 task(s) are outside the pending lifecycle"},
		{"empty feature", &types.TaskStats{}, "no tasks"},
		{"nil stats", nil, "no tasks"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := featureWorkState(tt.stats); !strings.Contains(got, tt.want) {
				t.Errorf("featureWorkState = %q, want it to contain %q", got, tt.want)
			}
		})
	}
}

// TestFeatureStateLines_DependencyStateIsNotCalledReady guards the word
// collision specifically: "ready" at the feature level meant "dependencies
// satisfied", the same word the task classification uses for "runnable".
func TestFeatureStateLines_DependencyStateIsNotCalledReady(t *testing.T) {
	// Dependencies satisfied, but every task is finished — the case that used to
	// print a bare "Status: ready" for a feature with nothing left to do.
	lines := featureStateLines(types.Feature{
		FeatureID: "done-feature",
		Ready:     true,
		Stats:     &types.TaskStats{Total: 13, NotPending: 13},
	})
	out := strings.Join(lines, "\n")

	if strings.Contains(out, "Status: ready") {
		t.Errorf("still labels a finished feature 'ready':\n%s", out)
	}
	if !strings.Contains(out, "Dependencies: satisfied") {
		t.Errorf("dependency state should be named as such:\n%s", out)
	}
	if !strings.Contains(out, "outside the pending lifecycle") {
		t.Errorf("should say there is nothing runnable:\n%s", out)
	}
}

// TestFeatureWorkState_AgreesWithTheStatsLine pins the invariant that keeps the
// summary honest: it is derived only from counts already displayed, so the
// prose and the numbers beside it cannot contradict each other.
func TestFeatureWorkState_AgreesWithTheStatsLine(t *testing.T) {
	stats := &types.TaskStats{Total: 6, Ready: 4, Waiting: 2}
	feature := types.Feature{FeatureID: "f", Ready: true, Stats: stats}

	out := strings.Join(append(featureStateLines(feature), formatStatsLine(stats)), "\n")

	if !strings.Contains(out, "Work: 4 task(s) ready to run") || !strings.Contains(out, "Ready: 4") {
		t.Errorf("summary and stats line must report the same number:\n%s", out)
	}
}

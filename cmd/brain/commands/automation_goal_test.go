package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// AutomationGoalCommand Unit Tests
// =============================================================================

func TestAutomationGoalCommand_Type(t *testing.T) {
	cmd := &AutomationGoalCommand{}
	if cmd.Type() != "automation goal" {
		t.Errorf("Type() = %q, want %q", cmd.Type(), "automation goal")
	}
}

func TestAutomationGoalCommand_UnknownSubcommand(t *testing.T) {
	cmd := &AutomationGoalCommand{
		Subcommand: "foobar",
		Config:     testGoalConfig("http://localhost:9999"),
		Flags:      &GoalFlags{},
	}
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "unknown automation goal subcommand") {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// set Subcommand Tests
// =============================================================================

func TestAutomationGoalCommand_Set(t *testing.T) {
	var receivedReq types.CreateGoalRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/goals" {
			json.NewDecoder(r.Body).Decode(&receivedReq)
			w.WriteHeader(http.StatusCreated)
			resp := types.GoalSummary{
				EntryID: "entry123",
				GoalID:  receivedReq.Config.ID,
				Title:   receivedReq.Title,
				Project: receivedReq.Project,
				Status:  "active",
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	flags := &GoalFlags{
		Criteria: []string{"all tests pass"},
		Validate: []string{"go test ./..."},
		Agent:    "tdd-dev",
	}
	cmd := newTestGoalCommand(server.URL, "set", "my-project", "Ship the dark mode feature", flags, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if receivedReq.Project != "my-project" {
		t.Errorf("expected project 'my-project', got %q", receivedReq.Project)
	}
	if receivedReq.Title != "Ship the dark mode feature" {
		t.Errorf("expected title from objective, got %q", receivedReq.Title)
	}
	if receivedReq.Config.ID == "" {
		t.Error("expected generated goal ID, got empty")
	}
	if !strings.HasPrefix(receivedReq.Config.ID, "ship-the-dark-mode-feature-") {
		t.Errorf("expected slug-prefixed goal ID, got %q", receivedReq.Config.ID)
	}
	if receivedReq.Config.Criteria != "all tests pass" {
		t.Errorf("expected joined criteria, got %q", receivedReq.Config.Criteria)
	}
	if receivedReq.Config.Validation != "go test ./..." {
		t.Errorf("expected joined validation, got %q", receivedReq.Config.Validation)
	}
	if receivedReq.Action.Type != "prompt" {
		t.Errorf("expected action type 'prompt', got %q", receivedReq.Action.Type)
	}
	if receivedReq.Action.Agent != "tdd-dev" {
		t.Errorf("expected action agent 'tdd-dev', got %q", receivedReq.Action.Agent)
	}

	output := out.String()
	if !strings.Contains(output, receivedReq.Config.ID) {
		t.Errorf("output should contain created goal ID: %q", output)
	}
	if !strings.Contains(output, "entry123") {
		t.Errorf("output should contain entry ID: %q", output)
	}
}

func TestAutomationGoalCommand_Set_TitleFlagOverridesObjective(t *testing.T) {
	var receivedReq types.CreateGoalRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/goals" {
			json.NewDecoder(r.Body).Decode(&receivedReq)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(types.GoalSummary{EntryID: "e1", GoalID: receivedReq.Config.ID})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	flags := &GoalFlags{Title: "Custom Title"}
	cmd := newTestGoalCommand(server.URL, "set", "proj", "the objective text", flags, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if receivedReq.Title != "Custom Title" {
		t.Errorf("expected --title to override objective, got %q", receivedReq.Title)
	}
	// Goal ID slug still derives from the objective.
	if !strings.HasPrefix(receivedReq.Config.ID, "the-objective-text-") {
		t.Errorf("expected goal ID slug from objective, got %q", receivedReq.Config.ID)
	}
}

func TestAutomationGoalCommand_Set_MissingObjective(t *testing.T) {
	var out bytes.Buffer
	cmd := newTestGoalCommand("http://localhost:9999", "set", "proj", "", &GoalFlags{}, &out)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing objective")
	}
}

// =============================================================================
// list Subcommand Tests
// =============================================================================

func TestAutomationGoalCommand_List(t *testing.T) {
	goals := []types.GoalSummary{
		{
			EntryID:   "e1",
			GoalID:    "goal-aaa",
			Title:     "Dark Mode",
			Project:   "my-project",
			FeatureID: "dark-mode",
			Status:    "active",
			Config:    &types.GoalConfig{TriggerSource: "both"},
		},
		{
			EntryID: "e2",
			GoalID:  "goal-bbb",
			Title:   "Auth Flow",
			Project: "my-project",
			Status:  "active",
			Config:  &types.GoalConfig{TriggerSource: "task"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/goals" {
			resp := struct {
				Goals []types.GoalSummary `json:"goals"`
				Count int                 `json:"count"`
			}{Goals: goals, Count: len(goals)}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestGoalCommand(server.URL, "list", "my-project", "", &GoalFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "goal-aaa") {
		t.Errorf("output should contain goal ID 'goal-aaa': %q", output)
	}
	if !strings.Contains(output, "Dark Mode") {
		t.Errorf("output should contain title 'Dark Mode': %q", output)
	}
	if !strings.Contains(output, "goal-bbb") {
		t.Errorf("output should contain goal ID 'goal-bbb': %q", output)
	}
}

func TestAutomationGoalCommand_List_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/goals" {
			resp := struct {
				Goals []types.GoalSummary `json:"goals"`
				Count int                 `json:"count"`
			}{Goals: []types.GoalSummary{}, Count: 0}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestGoalCommand(server.URL, "list", "", "", &GoalFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if !strings.Contains(out.String(), "No goals found") {
		t.Errorf("output should contain 'No goals found': %q", out.String())
	}
}

func TestAutomationGoalCommand_List_JSON(t *testing.T) {
	goals := []types.GoalSummary{
		{EntryID: "e1", GoalID: "goal-aaa", Title: "Dark Mode", Status: "active"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Goals []types.GoalSummary `json:"goals"`
			Count int                 `json:"count"`
		}{Goals: goals, Count: 1}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestGoalCommand(server.URL, "list", "", "", &GoalFlags{Format: "json"}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	var parsed []types.GoalSummary
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Errorf("output is not valid JSON: %v\n%s", err, out.String())
	}
	if len(parsed) != 1 || parsed[0].GoalID != "goal-aaa" {
		t.Errorf("unexpected parsed JSON: %+v", parsed)
	}
}

// =============================================================================
// show Subcommand Tests
// =============================================================================

func TestAutomationGoalCommand_Show(t *testing.T) {
	goals := []types.GoalSummary{
		{
			EntryID:   "entry-xyz",
			GoalID:    "goal-show",
			Title:     "Show Me",
			Project:   "my-project",
			FeatureID: "feat-1",
			Status:    "active",
			Config:    &types.GoalConfig{ID: "goal-show", Criteria: "done when green"},
		},
		{EntryID: "other", GoalID: "goal-other", Title: "Other"},
	}
	progress := types.GoalProgressResponse{
		GoalID:     "goal-show",
		EntryID:    "entry-xyz",
		Total:      3,
		Pending:    1,
		InProgress: 1,
		Completed:  1,
		Blocked:    0,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/goals":
			resp := struct {
				Goals []types.GoalSummary `json:"goals"`
				Count int                 `json:"count"`
			}{Goals: goals, Count: len(goals)}
			json.NewEncoder(w).Encode(resp)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/goals/goal-show/progress":
			json.NewEncoder(w).Encode(progress)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestGoalCommand(server.URL, "show", "my-project", "goal-show", &GoalFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "goal-show") {
		t.Errorf("output should contain goal ID: %q", output)
	}
	if !strings.Contains(output, "Show Me") {
		t.Errorf("output should contain title: %q", output)
	}
	if !strings.Contains(output, "done when green") {
		t.Errorf("output should contain criteria: %q", output)
	}
	// Progress numbers should appear.
	if !strings.Contains(output, "3") || !strings.Contains(output, "Completed") {
		t.Errorf("output should contain progress details: %q", output)
	}
}

func TestAutomationGoalCommand_Show_NotFound(t *testing.T) {
	goals := []types.GoalSummary{
		{EntryID: "e", GoalID: "goal-aaa", Title: "A"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/goals" {
			resp := struct {
				Goals []types.GoalSummary `json:"goals"`
			}{Goals: goals}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestGoalCommand(server.URL, "show", "my-project", "goal-missing", &GoalFlags{}, &out)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing goal")
	}
	if !strings.Contains(err.Error(), "goal not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// edit Subcommand Tests
// =============================================================================

func TestAutomationGoalCommand_Edit(t *testing.T) {
	var receivedReq types.UpdateGoalRequest
	var receivedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/goals/") {
			receivedPath = r.URL.Path
			json.NewDecoder(r.Body).Decode(&receivedReq)
			resp := types.GoalSummary{EntryID: "e1", GoalID: "goal-edit", Title: "New Title", Status: "active"}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	flags := &GoalFlags{
		Title:    "New Title",
		Criteria: []string{"crit a", "crit b"},
		Validate: []string{"validate cmd"},
	}
	cmd := newTestGoalCommand(server.URL, "edit", "my-project", "goal-edit", flags, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if receivedPath != "/api/v1/goals/goal-edit" {
		t.Errorf("expected PATCH to /api/v1/goals/goal-edit, got %q", receivedPath)
	}
	if receivedReq.Title == nil || *receivedReq.Title != "New Title" {
		t.Errorf("expected title pointer 'New Title', got %v", receivedReq.Title)
	}
	if receivedReq.Criteria == nil || *receivedReq.Criteria != "crit a\ncrit b" {
		t.Errorf("expected joined criteria, got %v", receivedReq.Criteria)
	}
	if receivedReq.Validation == nil || *receivedReq.Validation != "validate cmd" {
		t.Errorf("expected validation pointer, got %v", receivedReq.Validation)
	}
	// Status not passed → should remain nil.
	if receivedReq.Status != nil {
		t.Errorf("expected nil status, got %v", *receivedReq.Status)
	}

	if !strings.Contains(out.String(), "goal-edit") {
		t.Errorf("output should contain goal ID: %q", out.String())
	}
}

// =============================================================================
// run / reconcile Subcommand Tests
// =============================================================================

func TestAutomationGoalCommand_Run(t *testing.T) {
	audit := types.GoalReconcileAudit{
		Timestamp:       "2025-01-01T00:00:00Z",
		GoalID:          "goal-run",
		Decision:        types.ReconcileNeedWork,
		Reason:          "no active work, generating next task",
		TriggeringEvent: "manual",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/goals/goal-run/run" {
			json.NewEncoder(w).Encode(audit)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestGoalCommand(server.URL, "run", "my-project", "goal-run", &GoalFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "need_work") {
		t.Errorf("output should contain decision 'need_work': %q", output)
	}
	if !strings.Contains(output, "no active work") {
		t.Errorf("output should contain reason: %q", output)
	}
}

func TestAutomationGoalCommand_Reconcile_Alias(t *testing.T) {
	audit := types.GoalReconcileAudit{
		GoalID:   "goal-rec",
		Decision: types.ReconcileComplete,
		Reason:   "all linked tasks complete",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/goals/goal-rec/run" {
			json.NewEncoder(w).Encode(audit)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestGoalCommand(server.URL, "reconcile", "my-project", "goal-rec", &GoalFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if !strings.Contains(out.String(), "complete") {
		t.Errorf("output should contain decision 'complete': %q", out.String())
	}
}

func TestAutomationGoalCommand_Run_JSON(t *testing.T) {
	audit := types.GoalReconcileAudit{GoalID: "goal-run", Decision: types.ReconcileNoop, Reason: "work in progress"}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(audit)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestGoalCommand(server.URL, "run", "my-project", "goal-run", &GoalFlags{Format: "json"}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	var parsed types.GoalReconcileAudit
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Errorf("output is not valid JSON: %v\n%s", err, out.String())
	}
	if parsed.Decision != types.ReconcileNoop {
		t.Errorf("unexpected decision: %v", parsed.Decision)
	}
}

// =============================================================================
// pause / resume / archive / clear Subcommand Tests
// =============================================================================

func goalStatusServer(t *testing.T, goalID string, gotStatus *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && r.URL.Path == "/api/v1/goals/"+goalID {
			var body types.UpdateGoalRequest
			json.NewDecoder(r.Body).Decode(&body)
			if body.Status != nil {
				*gotStatus = *body.Status
			}
			json.NewEncoder(w).Encode(types.GoalSummary{EntryID: "e1", GoalID: goalID, Status: *gotStatus})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestAutomationGoalCommand_Pause(t *testing.T) {
	var got string
	server := goalStatusServer(t, "goal-p", &got)
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestGoalCommand(server.URL, "pause", "proj", "goal-p", &GoalFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if got != "blocked" {
		t.Errorf("expected status 'blocked', got %q", got)
	}
	if !strings.Contains(out.String(), "paused") {
		t.Errorf("output should mention paused: %q", out.String())
	}
}

func TestAutomationGoalCommand_Resume(t *testing.T) {
	var got string
	server := goalStatusServer(t, "goal-r", &got)
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestGoalCommand(server.URL, "resume", "proj", "goal-r", &GoalFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if got != "active" {
		t.Errorf("expected status 'active', got %q", got)
	}
	if !strings.Contains(out.String(), "resumed") {
		t.Errorf("output should mention resumed: %q", out.String())
	}
}

func TestAutomationGoalCommand_Archive(t *testing.T) {
	var got string
	server := goalStatusServer(t, "goal-a", &got)
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestGoalCommand(server.URL, "archive", "proj", "goal-a", &GoalFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if got != "archived" {
		t.Errorf("expected status 'archived', got %q", got)
	}
	if !strings.Contains(out.String(), "archived") {
		t.Errorf("output should mention archived: %q", out.String())
	}
}

func TestAutomationGoalCommand_Clear(t *testing.T) {
	var got string
	server := goalStatusServer(t, "goal-c", &got)
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestGoalCommand(server.URL, "clear", "proj", "goal-c", &GoalFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if got != "archived" {
		t.Errorf("expected status 'archived', got %q", got)
	}
	if !strings.Contains(out.String(), "cleared") {
		t.Errorf("output should mention cleared: %q", out.String())
	}
}

// =============================================================================
// validate Subcommand Tests
// =============================================================================

func TestAutomationGoalCommand_Validate(t *testing.T) {
	progress := types.GoalProgressResponse{
		GoalID:    "goal-v",
		EntryID:   "e1",
		Total:     2,
		Completed: 2,
		Pending:   0,
		Blocked:   0,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/goals/goal-v/progress" {
			json.NewEncoder(w).Encode(progress)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestGoalCommand(server.URL, "validate", "proj", "goal-v", &GoalFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "goal-v") {
		t.Errorf("output should contain goal ID: %q", output)
	}
	// 2/2 complete → criteria appear met.
	if !strings.Contains(strings.ToLower(output), "met") {
		t.Errorf("output should report validation summary (met): %q", output)
	}
}

func TestAutomationGoalCommand_Validate_JSON(t *testing.T) {
	progress := types.GoalProgressResponse{GoalID: "goal-v", Total: 3, Completed: 1}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(progress)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestGoalCommand(server.URL, "validate", "proj", "goal-v", &GoalFlags{Format: "json"}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	var parsed types.GoalProgressResponse
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Errorf("output is not valid JSON: %v\n%s", err, out.String())
	}
	if parsed.Total != 3 {
		t.Errorf("unexpected total: %d", parsed.Total)
	}
}

// =============================================================================
// Test Helpers
// =============================================================================

func testGoalConfig(apiURL string) *UnifiedConfig {
	cfg := &UnifiedConfig{}
	cfg.Runner.BrainAPIURL = apiURL
	cfg.Runner.APITimeout = 5000
	return cfg
}

func newTestGoalCommand(apiURL, subcommand, project, goalID string, flags *GoalFlags, out *bytes.Buffer) *AutomationGoalCommand {
	cfg := testGoalConfig(apiURL)
	return &AutomationGoalCommand{
		Subcommand: subcommand,
		Project:    project,
		GoalID:     goalID,
		Config:     cfg,
		Flags:      flags,
		Out:        out,
		apiClient:  runner.NewAPIClient(cfg.Runner),
	}
}

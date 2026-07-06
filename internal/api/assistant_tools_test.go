package api

// Tool registry tests. One representative test per tier plus schema
// invariants. Each tool has minimal happy-path + one error-path coverage; the
// deeper argument-shape testing lives inside each service's own test suite.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestListToolDefinitions_SchemaInvariants(t *testing.T) {
	defs := ListToolDefinitions()
	if len(defs) == 0 {
		t.Fatal("expected at least one tool definition")
	}
	names := map[string]bool{}
	for _, d := range defs {
		if d.Name == "" {
			t.Errorf("tool with empty name: %+v", d)
		}
		if names[d.Name] {
			t.Errorf("duplicate tool name %q", d.Name)
		}
		names[d.Name] = true
		if d.Handler == nil {
			t.Errorf("tool %q has nil handler", d.Name)
		}
		if d.Schema == nil {
			t.Errorf("tool %q has nil schema", d.Name)
		} else if d.Schema["type"] != "object" {
			t.Errorf("tool %q schema type = %v, want object", d.Name, d.Schema["type"])
		}
	}
	// Sanity: key tools by tier exist.
	must := []string{
		"list_entries", "get_entry", "list_tasks", "get_task", "list_automations",
		"create_task", "update_entry", "trigger_task",
		"delete_entry", "bulk_update", "pause_project",
	}
	for _, n := range must {
		if !names[n] {
			t.Errorf("expected tool %q to be registered", n)
		}
	}
}

func findTool(t *testing.T, name string) ToolDefinition {
	t.Helper()
	for _, d := range ListToolDefinitions() {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("tool %q not found", name)
	return ToolDefinition{}
}

// ─── Reads ────────────────────────────────────────────────────────────

func TestListAutomationsTool_ReturnsAutomationEntries(t *testing.T) {
	var gotReq types.ListEntriesRequest
	mock := &mockBrainService{listFunc: func(_ context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
		gotReq = req
		return &types.ListEntriesResponse{
			Total: 2,
			Entries: []types.BrainEntry{
				{ID: "a1", Path: "projects/prod/automation/a1.md", Title: "Nightly cron", Type: "automation", Status: "active", Schedule: "0 2 * * *"},
				{ID: "a2", Path: "projects/prod/automation/a2.md", Title: "Weekly review", Type: "automation", Status: "active"},
			},
		}, nil
	}}
	svc := NewAssistantService(AssistantServiceOptions{Enabled: true, Planner: stubAssistantPlanner{}, Brain: mock})
	tool := findTool(t, "list_automations")
	res, err := tool.Handler(context.Background(), svc, "prod", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handler err = %v", err)
	}
	if gotReq.Type != "automation" || gotReq.Project != "prod" {
		t.Fatalf("list req = %+v, want type=automation project=prod", gotReq)
	}
	m := res.(map[string]any)
	if m["total"].(int) != 2 {
		t.Fatalf("total = %v, want 2", m["total"])
	}
	rows := m["automations"].([]map[string]any)
	if len(rows) != 2 || rows[0]["title"] != "Nightly cron" {
		t.Fatalf("automations = %+v", rows)
	}
}

func TestGetTaskTool_MissingProjectErrors(t *testing.T) {
	svc := NewAssistantService(AssistantServiceOptions{Enabled: true, Planner: stubAssistantPlanner{}, Tasks: &mockTaskService{}})
	tool := findTool(t, "get_task")
	_, err := tool.Handler(context.Background(), svc, "", json.RawMessage(`{"task_id":"abc"}`))
	if err == nil || !strings.Contains(err.Error(), "project is required") {
		t.Fatalf("err = %v, want project-required error", err)
	}
}

func TestGetTaskTool_ReturnsResolvedTask(t *testing.T) {
	mock := &mockTaskService{getTaskFunc: func(_ context.Context, project, id string) (*types.ResolvedTask, error) {
		if project != "prod" || id != "abc12def" {
			t.Fatalf("getTask args = (%q,%q)", project, id)
		}
		return &types.ResolvedTask{
			ID: "abc12def", Title: "Fix auth", Status: "pending",
			Classification: "waiting", BlockedBy: []string{"upstream:xy98"},
			BlockedByReason: "dependency xy98 not completed",
		}, nil
	}}
	svc := NewAssistantService(AssistantServiceOptions{Enabled: true, Planner: stubAssistantPlanner{}, Tasks: mock})
	tool := findTool(t, "get_task")
	res, err := tool.Handler(context.Background(), svc, "prod", json.RawMessage(`{"task_id":"abc12def"}`))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	task, ok := res.(*types.ResolvedTask)
	if !ok {
		t.Fatalf("result type = %T, want *types.ResolvedTask", res)
	}
	if task.BlockedByReason == "" {
		t.Fatalf("expected blocked_by_reason in result, got %+v", task)
	}
}

// ─── Writes ───────────────────────────────────────────────────────────

func TestCreateTaskTool_UsesDefaultProject(t *testing.T) {
	var gotReq types.CreateEntryRequest
	mock := &mockBrainService{saveFunc: func(_ context.Context, req types.CreateEntryRequest) (*types.CreateEntryResponse, error) {
		gotReq = req
		return &types.CreateEntryResponse{ID: "xyz", Title: req.Title, Type: req.Type, Status: "pending"}, nil
	}}
	svc := NewAssistantService(AssistantServiceOptions{Enabled: true, Planner: stubAssistantPlanner{}, Brain: mock})
	tool := findTool(t, "create_task")
	args := []byte(`{"title":"New task","content":"details"}`)
	_, err := tool.Handler(context.Background(), svc, "brain-api", args)
	if err != nil {
		t.Fatalf("handler err = %v", err)
	}
	if gotReq.Type != "task" || gotReq.Project != "brain-api" || gotReq.Title != "New task" {
		t.Fatalf("save req = %+v", gotReq)
	}
}

func TestUpdateEntryTool_OnlySetsProvidedFields(t *testing.T) {
	var gotReq types.UpdateEntryRequest
	mock := &mockBrainService{updateFunc: func(_ context.Context, _ string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
		gotReq = req
		return &types.BrainEntry{ID: "abc"}, nil
	}}
	svc := NewAssistantService(AssistantServiceOptions{Enabled: true, Planner: stubAssistantPlanner{}, Brain: mock})
	tool := findTool(t, "update_entry")
	// Only status is set — title/content/append should stay nil.
	_, err := tool.Handler(context.Background(), svc, "", json.RawMessage(`{"path_or_id":"abc","status":"in_progress"}`))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if gotReq.Status == nil || *gotReq.Status != "in_progress" {
		t.Fatalf("Status = %v, want in_progress", gotReq.Status)
	}
	if gotReq.Title != nil || gotReq.Content != nil || gotReq.Append != nil {
		t.Fatalf("unexpected non-nil field: title=%v content=%v append=%v", gotReq.Title, gotReq.Content, gotReq.Append)
	}
}

// ─── Destructive: explicit gate ───────────────────────────────────────

func TestDestructiveTool_NotExplicit_ReturnsProposedAction(t *testing.T) {
	// Wire delete_entry via executeToolCall — that's where the gate lives.
	// The mock brain would panic if Delete was actually called, since
	// deleteFunc is not set.
	mock := &mockBrainService{deleteFunc: func(_ context.Context, _ string) error {
		return fmt.Errorf("should not be called")
	}}
	svc := NewAssistantService(AssistantServiceOptions{Enabled: true, Planner: stubAssistantPlanner{}, Brain: mock})
	def := findTool(t, "delete_entry")
	if def.Tier != TierDestructive {
		t.Fatalf("delete_entry tier = %v, want destructive", def.Tier)
	}
	call := toolCall{
		ID:   "call_1",
		Type: "function",
		Function: toolCallFunction{
			Name:      "delete_entry",
			Arguments: `{"path_or_id":"projects/x/task/abc.md"}`,
		},
	}
	result, execRec, proposed, err := svc.executeToolCall(context.Background(), "x", def, call)
	if err != nil {
		t.Fatalf("executeToolCall err = %v", err)
	}
	if result != nil {
		t.Fatalf("result should be nil for proposed action, got %+v", result)
	}
	if execRec != nil {
		t.Fatalf("no execution record expected, got %+v", execRec)
	}
	if proposed == nil {
		t.Fatal("expected a proposed action, got nil")
	}
	if proposed.Type != "delete_entry" || proposed.Explicit {
		t.Fatalf("proposed = %+v, want delete_entry explicit=false", proposed)
	}
	if _, has := proposed.Payload["_explicit"]; has {
		t.Fatalf("_explicit should be stripped from proposed payload, got %+v", proposed.Payload)
	}
}

func TestDestructiveTool_ExplicitTrue_Executes(t *testing.T) {
	called := 0
	mock := &mockBrainService{deleteFunc: func(_ context.Context, id string) error {
		called++
		if id != "projects/x/task/abc.md" {
			t.Fatalf("delete id = %q", id)
		}
		return nil
	}}
	svc := NewAssistantService(AssistantServiceOptions{Enabled: true, Planner: stubAssistantPlanner{}, Brain: mock})
	def := findTool(t, "delete_entry")
	call := toolCall{
		ID:   "call_1",
		Type: "function",
		Function: toolCallFunction{
			Name:      "delete_entry",
			Arguments: `{"path_or_id":"projects/x/task/abc.md","_explicit":true}`,
		},
	}
	_, execRec, proposed, err := svc.executeToolCall(context.Background(), "x", def, call)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if called != 1 {
		t.Fatalf("delete called %d times, want 1", called)
	}
	if execRec == nil || execRec.Status != "completed" {
		t.Fatalf("execRec = %+v, want completed", execRec)
	}
	if proposed != nil {
		t.Fatalf("no proposed action expected, got %+v", proposed)
	}
}

func TestPauseProjectTool_UsesDefaultProject(t *testing.T) {
	var got string
	mock := &mockRunnerService{pauseFunc: func(_ context.Context, project string) error {
		got = project
		return nil
	}}
	svc := NewAssistantService(AssistantServiceOptions{Enabled: true, Planner: stubAssistantPlanner{}, Runner: mock})
	def := findTool(t, "pause_project")
	res, err := def.Handler(context.Background(), svc, "prod", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != "prod" {
		t.Fatalf("pause called with %q, want prod", got)
	}
	m, ok := res.(map[string]any)
	if !ok || m["paused"] != "prod" {
		t.Fatalf("result = %+v, want paused=prod", res)
	}
}

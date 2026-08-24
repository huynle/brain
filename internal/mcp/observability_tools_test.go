package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Tool Registration Tests
// =============================================================================

func TestRegisterObservabilityTools_Count(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterObservabilityTools(s, client)

	count := len(s.tools)
	if count != 7 {
		t.Errorf("expected 7 observability tools registered, got %d", count)
	}
}

func TestRegisterObservabilityTools_Names(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterObservabilityTools(s, client)

	expectedTools := []string{
		"task_logs",
		"task_dispatch_lease",
		"task_placement_reasons",
		"events_recent",
		"automation_runs",
		"automation_run_get",
		"scheduler_status",
	}

	for _, name := range expectedTools {
		if _, ok := s.tools[name]; !ok {
			t.Errorf("tool %q not registered", name)
		}
	}
}

func TestRegisterObservabilityTools_AllHandlersSet(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterObservabilityTools(s, client)

	for name, rt := range s.tools {
		if rt.handler == nil {
			t.Errorf("tool %q has nil handler", name)
		}
	}
}

func TestRegisterObservabilityTools_Descriptions(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterObservabilityTools(s, client)

	for name, rt := range s.tools {
		if rt.tool.Description == "" {
			t.Errorf("tool %q has empty description", name)
		}
		if rt.tool.InputSchema.Type != "object" {
			t.Errorf("tool %q inputSchema.type = %q, want %q", name, rt.tool.InputSchema.Type, "object")
		}
	}
}

// =============================================================================
// Schema Tests
// =============================================================================

func TestBrainTaskPlacementReasons_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterObservabilityTools(s, client)

	tool := s.tools["task_placement_reasons"].tool

	// Required fields
	expectedRequired := []string{"task_id"}
	if len(tool.InputSchema.Required) != len(expectedRequired) {
		t.Errorf("brain_task_placement_reasons required = %v, want %v", tool.InputSchema.Required, expectedRequired)
	}
	for _, req := range expectedRequired {
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

	// Check properties exist
	expectedProps := []string{"project", "task_id"}
	for _, prop := range expectedProps {
		if _, ok := tool.InputSchema.Properties[prop]; !ok {
			t.Errorf("schema missing property %q", prop)
		}
	}

	// Verify property types
	if tool.InputSchema.Properties["project"].Type != "string" {
		t.Errorf("project type = %q, want string", tool.InputSchema.Properties["project"].Type)
	}
	if tool.InputSchema.Properties["task_id"].Type != "string" {
		t.Errorf("task_id type = %q, want string", tool.InputSchema.Properties["task_id"].Type)
	}
}

func TestBrainAutomationRuns_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterObservabilityTools(s, client)

	tool := s.tools["automation_runs"].tool

	// No required fields
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("brain_automation_runs required = %v, want []", tool.InputSchema.Required)
	}

	// Check properties exist
	expectedProps := []string{"project", "automation_id", "status", "limit"}
	for _, prop := range expectedProps {
		if _, ok := tool.InputSchema.Properties[prop]; !ok {
			t.Errorf("schema missing property %q", prop)
		}
	}

	// Verify property types
	if tool.InputSchema.Properties["project"].Type != "string" {
		t.Errorf("project type = %q, want string", tool.InputSchema.Properties["project"].Type)
	}
	if tool.InputSchema.Properties["automation_id"].Type != "string" {
		t.Errorf("automation_id type = %q, want string", tool.InputSchema.Properties["automation_id"].Type)
	}
	if tool.InputSchema.Properties["status"].Type != "string" {
		t.Errorf("status type = %q, want string", tool.InputSchema.Properties["status"].Type)
	}
	if tool.InputSchema.Properties["limit"].Type != "number" {
		t.Errorf("limit type = %q, want number", tool.InputSchema.Properties["limit"].Type)
	}
}

func TestBrainAutomationRunGet_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterObservabilityTools(s, client)

	tool := s.tools["automation_run_get"].tool

	// Required fields
	expectedRequired := []string{"run_id"}
	if len(tool.InputSchema.Required) != len(expectedRequired) {
		t.Errorf("brain_automation_run_get required = %v, want %v", tool.InputSchema.Required, expectedRequired)
	}
	for _, req := range expectedRequired {
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

	// Check properties exist
	expectedProps := []string{"run_id"}
	for _, prop := range expectedProps {
		if _, ok := tool.InputSchema.Properties[prop]; !ok {
			t.Errorf("schema missing property %q", prop)
		}
	}

	// Verify property types
	if tool.InputSchema.Properties["run_id"].Type != "string" {
		t.Errorf("run_id type = %q, want string", tool.InputSchema.Properties["run_id"].Type)
	}
}

// =============================================================================
// Integration Tests
// =============================================================================

func TestBrainTaskPlacementReasons(t *testing.T) {
	// Mock API server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/test-proj/task123/placement-reasons" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}

		resp := types.PlacementReasonListResponse{
			Reasons: []types.PlacementReason{
				{
					RunnerID:  "runner1",
					Decision:  "accept",
					Reason:    "has capacity",
					CreatedAt: 1234567890,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewAPIClient(srv.URL)
	server := NewServer()
	RegisterObservabilityTools(server, client)

	tool := server.tools["task_placement_reasons"]
	result, err := tool.handler(context.Background(), map[string]any{
		"project": "test-proj",
		"taskId":  "task123",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "Placement Reasons") {
		t.Errorf("expected 'Placement Reasons' in result")
	}
	if !contains(result, "runner1") {
		t.Errorf("expected runner ID in result")
	}
	if !contains(result, "accept") {
		t.Errorf("expected decision in result")
	}
}

func TestBrainAutomationRuns(t *testing.T) {
	// Mock API server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/automation-runs" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		// Check query params
		if project := r.URL.Query().Get("project"); project != "test-proj" {
			t.Errorf("expected project=test-proj, got %s", project)
		}

		resp := types.ListEntriesResponse{
			Entries: []types.BrainEntry{
				{
					ID:    "run1",
					Title: "Auto Run 1",
					Type:  "automation",
				},
			},
			Total: 1,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewAPIClient(srv.URL)
	server := NewServer()
	RegisterObservabilityTools(server, client)

	tool := server.tools["automation_runs"]
	result, err := tool.handler(context.Background(), map[string]any{
		"project": "test-proj",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "Automation Runs") {
		t.Errorf("expected 'Automation Runs' in result")
	}
	if !contains(result, "Auto Run 1") {
		t.Errorf("expected automation title in result")
	}
}

func TestBrainAutomationRunGet(t *testing.T) {
	// Mock API server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/automation-runs/run123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		resp := types.BrainEntry{
			ID:      "run123",
			Title:   "Test Automation Run",
			Type:    "automation",
			Content: "Run details here",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewAPIClient(srv.URL)
	server := NewServer()
	RegisterObservabilityTools(server, client)

	tool := server.tools["automation_run_get"]
	result, err := tool.handler(context.Background(), map[string]any{
		"runId": "run123",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "Automation Run") {
		t.Errorf("expected 'Automation Run' in result")
	}
	if !contains(result, "Test Automation Run") {
		t.Errorf("expected run title in result")
	}
}

func TestBrainTaskLogs(t *testing.T) {
	// Mock API server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/test-proj/task123/logs" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}

		resp := types.LogQueryResponse{
			Lines: []types.LogLine{
				{Timestamp: "2024-01-01T12:00:00Z", Level: "info", Content: "test log"},
			},
			Total: 1,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewAPIClient(srv.URL)
	server := NewServer()
	RegisterObservabilityTools(server, client)

	// Get registered tool
	tool, ok := server.tools["task_logs"]
	if !ok {
		t.Fatal("brain_task_logs tool not registered")
	}

	// Call handler directly
	result, err := tool.handler(context.Background(), map[string]any{
		"project": "test-proj",
		"taskId":  "task123",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == "" {
		t.Fatal("expected non-empty result")
	}

	// Check formatted output contains expected content
	if !contains(result, "Task Logs") {
		t.Errorf("expected 'Task Logs' in result")
	}
	if !contains(result, "test-proj") {
		t.Errorf("expected project ID in result")
	}
	if !contains(result, "task123") {
		t.Errorf("expected task ID in result")
	}
}

func TestBrainTaskDispatchLease(t *testing.T) {
	// Mock API server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/test-proj/task123/dispatch-lease" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		resp := types.DispatchLease{
			LeaseID:          "lease1",
			ProjectID:        "test-proj",
			TaskID:           "task123",
			State:            "active",
			AssignedRunnerID: "runner1",
			PushedAt:         1234567890,
			ExpiresAt:        1234567999,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewAPIClient(srv.URL)
	server := NewServer()
	RegisterObservabilityTools(server, client)

	tool := server.tools["task_dispatch_lease"]
	result, err := tool.handler(context.Background(), map[string]any{
		"project": "test-proj",
		"taskId":  "task123",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "Dispatch Lease") {
		t.Errorf("expected 'Dispatch Lease' in result")
	}
	if !contains(result, "lease1") {
		t.Errorf("expected lease ID in result")
	}
}

func TestBrainEventsRecent(t *testing.T) {
	// Mock API server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/events/recent" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		// Check query params
		if limit := r.URL.Query().Get("limit"); limit != "50" {
			t.Errorf("expected limit=50, got %s", limit)
		}

		resp := map[string]any{
			"events": []types.Event{
				{
					ID:     "evt1",
					Type:   "task.started",
					Source: "runner",
				},
			},
			"count": 1,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewAPIClient(srv.URL)
	server := NewServer()
	RegisterObservabilityTools(server, client)

	tool := server.tools["events_recent"]
	result, err := tool.handler(context.Background(), map[string]any{
		"limit": float64(50), // JSON numbers are float64
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "Recent Events") {
		t.Errorf("expected 'Recent Events' in result")
	}
}

func TestBrainSchedulerStatus(t *testing.T) {
	// Mock API server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/scheduler/status" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		resp := types.SchedulerStatus{
			Started:    true,
			Running:    true,
			Interval:   "10s",
			TotalTicks: 42,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewAPIClient(srv.URL)
	server := NewServer()
	RegisterObservabilityTools(server, client)

	tool := server.tools["scheduler_status"]
	result, err := tool.handler(context.Background(), map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !contains(result, "Scheduler Status") {
		t.Errorf("expected 'Scheduler Status' in result")
	}
	if !contains(result, "42") {
		t.Errorf("expected total ticks in result")
	}
}

// TestHTTPHandlerRegistersObservabilityTools verifies that observability tools
// are registered and accessible via the HTTP handler.
func TestHTTPHandlerRegistersObservabilityTools(t *testing.T) {
	// Mock Brain API server
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Just return valid response for any endpoint
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer apiSrv.Close()

	// Create HTTP handler
	client := NewAPIClient(apiSrv.URL)
	handler := NewHTTPHandler(client)

	// Send a tools/list request to verify observability tools are registered
	reqBody := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/list",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp JSONRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	// Parse result as tools list
	resultBytes, _ := json.Marshal(resp.Result)
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("failed to parse tools list: %v", err)
	}

	// Verify all 7 observability tools are present
	expectedTools := []string{
		"task_logs",
		"task_dispatch_lease",
		"task_placement_reasons",
		"events_recent",
		"automation_runs",
		"automation_run_get",
		"scheduler_status",
	}

	toolNames := make(map[string]bool)
	for _, tool := range result.Tools {
		toolNames[tool.Name] = true
	}

	for _, expected := range expectedTools {
		if !toolNames[expected] {
			t.Errorf("expected tool %s not found in HTTP handler", expected)
		}
	}
}

// Helper functions
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && hasSubstring(s, substr))
}

func hasSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestFormatSchedulerStatus_SkipBreakdownNamesTheCause locks in the fix for a
// scheduler_status that reported "Skipped: N" and nothing else. The three skip
// causes call for three different operator responses — flip a pause dial, fix
// runner eligibility, or do nothing because the work is already in flight — and
// collapsing them into one number made the most common question ("why is
// nothing running?") unanswerable from this tool alone.
func TestFormatSchedulerStatus_SkipBreakdownNamesTheCause(t *testing.T) {
	status := types.SchedulerStatus{
		Started:    true,
		Running:    true,
		TotalTicks: 10,
		LastProjectResults: map[string]types.SchedulerResult{
			"held": {
				ProjectID: "held", Considered: 4, Dispatched: 0, Skipped: 4,
				SkippedTasksPaused: 3, SkippedAutomationsPaused: 1,
			},
			"unplaceable": {
				ProjectID: "unplaceable", Considered: 2, Dispatched: 0, Skipped: 2,
				SkippedNoCandidate: 2,
			},
			"inflight": {
				ProjectID: "inflight", Considered: 1, Dispatched: 0, Skipped: 1,
				SkippedAlreadyLeased: 1,
			},
		},
	}

	out := formatSchedulerStatus(status)

	for _, want := range []string{
		"3 held by tasks-paused",
		"1 held by automations-paused",
		"2 no eligible runner",
		"1 already dispatched by an earlier pass",
	} {
		if !contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}

// TestFormatSchedulerStatus_UnaccountedSkipsAreDisclosed guards the invariant
// that the rendered parts visibly sum to the total. If a future skip site
// increments Skipped without a matching cause counter, the shortfall must show
// up as "other" rather than vanishing into a breakdown that silently under-adds.
func TestFormatSchedulerStatus_UnaccountedSkipsAreDisclosed(t *testing.T) {
	out := formatSchedulerStatus(types.SchedulerStatus{
		LastProjectResults: map[string]types.SchedulerResult{
			"p": {ProjectID: "p", Considered: 5, Skipped: 5, SkippedTasksPaused: 2},
		},
	})

	if !contains(out, "2 held by tasks-paused") || !contains(out, "3 other") {
		t.Errorf("expected the unaccounted remainder to be disclosed, got:\n%s", out)
	}
}

// TestFormatSchedulerStatus_OmitsIdleProjectsButSaysSo verifies the noise fix.
// A live store carries dozens of projects, most of them dead test fixtures that
// report three zeros forever. They are omitted, but the count is disclosed —
// silently dropping them would make "project filtered out of this view" look
// identical to "project no longer exists".
func TestFormatSchedulerStatus_OmitsIdleProjectsButSaysSo(t *testing.T) {
	results := map[string]types.SchedulerResult{
		"busy": {ProjectID: "busy", Considered: 2, Dispatched: 2},
	}
	for _, idle := range []string{"dead-fixture-1", "dead-fixture-2", "dead-fixture-3"} {
		results[idle] = types.SchedulerResult{ProjectID: idle}
	}

	out := formatSchedulerStatus(types.SchedulerStatus{LastProjectResults: results})

	if !contains(out, "**busy:**") {
		t.Errorf("expected the active project to be rendered:\n%s", out)
	}
	if contains(out, "dead-fixture") {
		t.Errorf("expected idle projects to be omitted:\n%s", out)
	}
	if !contains(out, "3 project(s) omitted") {
		t.Errorf("expected the omitted count to be disclosed:\n%s", out)
	}
}

// TestFormatSchedulerStatus_ProjectOrderIsDeterministic pins the ordering. The
// renderer used to range directly over the map, so Go's randomized iteration
// order reshuffled the output on every call — which makes diffing two
// consecutive reads to see what changed useless.
func TestFormatSchedulerStatus_ProjectOrderIsDeterministic(t *testing.T) {
	results := map[string]types.SchedulerResult{}
	for _, name := range []string{"zeta", "alpha", "mike", "bravo", "yankee", "delta", "kilo", "echo"} {
		results[name] = types.SchedulerResult{ProjectID: name, Considered: 1, Skipped: 1, SkippedNoCandidate: 1}
	}
	status := types.SchedulerStatus{LastProjectResults: results}

	first := formatSchedulerStatus(status)
	for i := 0; i < 20; i++ {
		if got := formatSchedulerStatus(status); got != first {
			t.Fatalf("output changed between calls (iteration %d):\n--- first ---\n%s\n--- got ---\n%s", i, first, got)
		}
	}
	if !contains(first, "**alpha:**") {
		t.Fatalf("expected sorted output to contain alpha:\n%s", first)
	}
}

// TestValidateEventTypeFilter_RejectsTypesTheStreamDoesNotCarry — an
// unrecognised filter previously produced "Found 0 events: No events found.",
// byte-identical to a valid filter that genuinely matched nothing. Probed live
// against production: events_recent(type: "automation.run") returned a clean
// zero, and there is no automation.* family at all.
//
// Named for what the check actually establishes. An earlier name said
// "NothingEmits", which is the overclaim goal.reconcile disproved — the
// allowlist knows what the realtime stream carries, not what exists.
func TestValidateEventTypeFilter_RejectsTypesTheStreamDoesNotCarry(t *testing.T) {
	for _, bad := range []string{"automation.run", "task.complete", "nonsense", "runner.*"[:7] + "bogus"} {
		t.Run(bad, func(t *testing.T) {
			err := validateEventTypeFilter(bad)
			if err == nil {
				t.Fatalf("validateEventTypeFilter(%q) = nil, want an error", bad)
			}
			// The error has to point somewhere, not just say no.
			if !strings.Contains(err.Error(), "task.*") {
				t.Errorf("error should name the known families, got: %v", err)
			}
		})
	}
}

// TestValidateEventTypeFilter_AcceptsRealTypesAndFamilies guards against the fix
// turning into a different lie: rejecting filters that DO work.
func TestValidateEventTypeFilter_AcceptsRealTypesAndFamilies(t *testing.T) {
	// "*" is the global wildcard: MatchEventPattern returns true for every event
	// type, and it is the same pattern language webhook Events and automation
	// Trigger.Event use. The first version of this validator rejected it — the
	// exact failure this test's name promises to prevent, missed because the
	// list below enumerated families and exact types and never the wildcard.
	good := []string{"*", "task.*", "runner.*", "feature.*", "entry.*", "control.*"}
	// Every exact type must pass too — the allowlist is the source of truth.
	good = append(good, types.AllEventTypes...)

	for _, f := range good {
		if err := validateEventTypeFilter(f); err != nil {
			t.Errorf("validateEventTypeFilter(%q) = %v, want nil", f, err)
		}
	}
}

// TestFormatRecentEvents_EmptyResultStatesItsWindow — a zero count from a
// volatile in-memory ring is not evidence the event never happened. The ring is
// empty after every restart and is filled continuously by runner.poll_complete.
func TestFormatRecentEvents_EmptyResultStatesItsWindow(t *testing.T) {
	out := formatRecentEvents(nil, 0, types.EventCoverage{
		Buffered: 3, Capacity: 1000, Oldest: "2026-08-24T15:48:00Z",
	})

	for _, want := range []string{
		"No events matched",
		"Searched 3 buffered event(s)",
		"ring capacity 1000",
		"oldest retained: 2026-08-24T15:48:00Z",
		"starts empty after a restart",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

// TestFormatRecentEvents_FullRingSaysHistoryIsGone — when the ring is saturated,
// "oldest retained" is not just informational: everything before it is destroyed.
func TestFormatRecentEvents_FullRingSaysHistoryIsGone(t *testing.T) {
	out := formatRecentEvents(nil, 0, types.EventCoverage{
		Buffered: 1000, Capacity: 1000, Oldest: "2026-08-24T15:10:00Z",
	})
	if !strings.Contains(out, "ring is FULL") || !strings.Contains(out, "overwritten") {
		t.Errorf("a saturated ring must say older history is gone:\n%s", out)
	}
}

// TestFormatPlacementReasons_EmptyIsGoodNews — this table records REJECTIONS
// only. recordNoCandidate (internal/service/scheduler.go) is its sole writer and
// its only Decision value is "no_candidate", so a successful dispatch leaves no
// row. "No placement decisions recorded" therefore read as "nothing is known"
// when it actually means "never failed to place" — and sent readers hunting for
// a fault this table would never have shown them.
func TestFormatPlacementReasons_EmptyIsGoodNews(t *testing.T) {
	out := formatPlacementReasons("brain-api", "oanq4y2n", types.PlacementReasonListResponse{})

	if !strings.Contains(out, "never failed to find an eligible runner") {
		t.Errorf("empty result should read as good news:\n%s", out)
	}
	if !strings.Contains(out, "records rejections ONLY") {
		t.Errorf("must state what the table does and does not hold:\n%s", out)
	}
	// It must redirect, not dead-end.
	for _, want := range []string{"runner_status", "scheduler_status"} {
		if !strings.Contains(out, want) {
			t.Errorf("should point at where the real cause lives (%s):\n%s", want, out)
		}
	}
	if strings.Contains(out, "No placement decisions recorded") {
		t.Errorf("stale wording implying nothing was decided:\n%s", out)
	}
}

// TestFormatFeatureListTruncation_IsDisclosed — the server returns the complete
// unpaginated list, so a silent features[:limit] made a truncated view look like
// the whole project. The per-feature task list has always said "...and N more
// tasks"; this is the same courtesy one level up.
func TestFormatFeatureListTruncation_IsDisclosed(t *testing.T) {
	features := make([]types.Feature, 63)
	for i := range features {
		features[i] = types.Feature{
			FeatureID: fmt.Sprintf("feat-%02d", i),
			Stats:     &types.TaskStats{Total: 1, Ready: 1},
		}
	}

	out := formatFeatureList("Features", "No features found", features, 50)

	for _, want := range []string{"Showing 50 of 63 features", "13 omitted", "Raise 'limit'"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out[:400])
		}
	}
	// And a list that fits must not claim anything was dropped.
	short := formatFeatureList("Features", "none", features[:5], 50)
	if strings.Contains(short, "omitted") {
		t.Errorf("a complete list must not claim truncation:\n%s", short)
	}
}

// TestValidateEventTypeFilter_GoalEventsRedirectRatherThanDeny — goal.reconcile
// is a REAL event type, written on every goal reconcile via store.InsertEvent,
// but to the durable event_log rather than the realtime ring this tool reads
// (Ingest rejects it outright, so it can never appear here).
//
// The first version told callers "nothing emits it" and listed families without
// goal.*, so an agent asking "have any goal reconciles happened?" was told the
// goal family does not exist and never found goal_audit. The answer has to be
// accurate about WHY, and point somewhere useful.
func TestValidateEventTypeFilter_GoalEventsRedirectRatherThanDeny(t *testing.T) {
	for _, f := range []string{"goal.reconcile", "goal.*"} {
		t.Run(f, func(t *testing.T) {
			err := validateEventTypeFilter(f)
			if err == nil {
				t.Fatalf("validateEventTypeFilter(%q) = nil; it cannot match here and should say so", f)
			}
			if strings.Contains(err.Error(), "nothing emits it") {
				t.Errorf("must not claim nothing emits a continuously-emitted type: %v", err)
			}
			for _, want := range []string{"durable event log", "goal_audit"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error should mention %q, got: %v", want, err)
				}
			}
		})
	}
}

// TestValidateEventTypeFilter_RejectionDoesNotOverclaim — the allowlist is
// AllEventTypes, which means "carried by the realtime stream", NOT "every real
// event type". goal.reconcile proved those differ, so the rejection must claim
// only the former.
func TestValidateEventTypeFilter_RejectionDoesNotOverclaim(t *testing.T) {
	err := validateEventTypeFilter("totally.bogus")
	if err == nil {
		t.Fatal("expected an error for an unknown type")
	}
	if strings.Contains(err.Error(), "nothing emits it") {
		t.Errorf("rejection overclaims what the allowlist knows: %v", err)
	}
	if !strings.Contains(err.Error(), "realtime event stream") {
		t.Errorf("rejection should scope its claim to the realtime stream: %v", err)
	}
	// It must still point somewhere, including the wildcard.
	if !strings.Contains(err.Error(), `"*"`) {
		t.Errorf("rejection should mention the global wildcard: %v", err)
	}
}

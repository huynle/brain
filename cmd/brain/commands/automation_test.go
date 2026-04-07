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
// AutomationCommand Unit Tests
// =============================================================================

func TestAutomationCommand_Type(t *testing.T) {
	cmd := &AutomationCommand{}
	if cmd.Type() != "automation" {
		t.Errorf("Type() = %q, want %q", cmd.Type(), "automation")
	}
}

func TestAutomationCommand_UnknownSubcommand(t *testing.T) {
	cmd := &AutomationCommand{
		Subcommand: "foobar",
		Config:     testAutomationConfig("http://localhost:9999"),
		Flags:      &AutomationFlags{},
	}
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "unknown automation subcommand") {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// List Subcommand Tests
// =============================================================================

func TestAutomationCommand_List_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/api/v1/entries") {
			// Verify type=automation filter
			if r.URL.Query().Get("type") != "automation" {
				t.Errorf("expected type=automation query param, got %q", r.URL.Query().Get("type"))
			}
			resp := types.ListEntriesResponse{Entries: []types.BrainEntry{}, Total: 0}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestAutomationCommand(server.URL, "list", "", &AutomationFlags{}, &out)
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "No automations found") {
		t.Errorf("output = %q, expected 'No automations found'", output)
	}
}

func TestAutomationCommand_List_WithEntries(t *testing.T) {
	entries := []types.BrainEntry{
		{
			ID:        "auto1234",
			Title:     "On Task Complete",
			Type:      "automation",
			Status:    "active",
			ProjectID: "my-project",
			Trigger:   &types.AutomationTrigger{Type: "event", Event: "task.completed"},
			Action:    &types.AutomationAction{Type: "prompt", DirectPrompt: "Review task"},
		},
		{
			ID:        "auto5678",
			Title:     "Nightly Build",
			Type:      "automation",
			Status:    "active",
			ProjectID: "my-project",
			Trigger:   &types.AutomationTrigger{Type: "cron", Schedule: "0 0 * * *"},
			Action:    &types.AutomationAction{Type: "script", Command: "make build"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/api/v1/entries") {
			resp := types.ListEntriesResponse{Entries: entries, Total: len(entries)}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestAutomationCommand(server.URL, "list", "", &AutomationFlags{}, &out)
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "auto1234") {
		t.Errorf("output should contain automation ID 'auto1234': %q", output)
	}
	if !strings.Contains(output, "On Task Complete") {
		t.Errorf("output should contain title 'On Task Complete': %q", output)
	}
	if !strings.Contains(output, "event") {
		t.Errorf("output should contain trigger type 'event': %q", output)
	}
	if !strings.Contains(output, "Total: 2") {
		t.Errorf("output should contain 'Total: 2': %q", output)
	}
}

func TestAutomationCommand_List_JSON(t *testing.T) {
	entries := []types.BrainEntry{
		{ID: "auto1234", Title: "Test", Type: "automation", Status: "active"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := types.ListEntriesResponse{Entries: entries, Total: 1}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestAutomationCommand(server.URL, "list", "", &AutomationFlags{Format: "json"}, &out)
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// Verify output is valid JSON
	var parsed []types.BrainEntry
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Errorf("output is not valid JSON: %v", err)
	}
}

// =============================================================================
// Test Subcommand Tests (dry-run event simulation)
// =============================================================================

func TestAutomationCommand_Test_NoEvent(t *testing.T) {
	cmd := &AutomationCommand{
		Subcommand: "test",
		IDOrName:   "",
		Config:     testAutomationConfig("http://localhost:9999"),
		Flags:      &AutomationFlags{},
	}
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing event name")
	}
}

func TestAutomationCommand_Test_MatchesEvent(t *testing.T) {
	entries := []types.BrainEntry{
		{
			ID:      "auto1234",
			Title:   "On Task Complete",
			Type:    "automation",
			Status:  "active",
			Trigger: &types.AutomationTrigger{Type: "event", Event: "task.completed"},
			Action:  &types.AutomationAction{Type: "prompt", DirectPrompt: "Review it"},
		},
		{
			ID:      "auto5678",
			Title:   "On Entry Created",
			Type:    "automation",
			Status:  "active",
			Trigger: &types.AutomationTrigger{Type: "event", Event: "entry.created"},
			Action:  &types.AutomationAction{Type: "script", Command: "echo done"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := types.ListEntriesResponse{Entries: entries, Total: len(entries)}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestAutomationCommand(server.URL, "test", "task.completed", &AutomationFlags{}, &out)
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "MATCH") {
		t.Errorf("output should contain 'MATCH': %q", output)
	}
	if !strings.Contains(output, "On Task Complete") {
		t.Errorf("output should contain matched automation name: %q", output)
	}
	if !strings.Contains(output, "1 automation(s) would match") {
		t.Errorf("output should show 1 match: %q", output)
	}
}

func TestAutomationCommand_Test_NoMatches(t *testing.T) {
	entries := []types.BrainEntry{
		{
			ID:      "auto1234",
			Title:   "On Task Complete",
			Type:    "automation",
			Status:  "active",
			Trigger: &types.AutomationTrigger{Type: "event", Event: "task.completed"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := types.ListEntriesResponse{Entries: entries, Total: len(entries)}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestAutomationCommand(server.URL, "test", "entry.deleted", &AutomationFlags{}, &out)
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "No automations matched") {
		t.Errorf("output should contain 'No automations matched': %q", output)
	}
}

// =============================================================================
// Enable / Disable Subcommand Tests
// =============================================================================

func TestAutomationCommand_Enable_NoID(t *testing.T) {
	cmd := &AutomationCommand{
		Subcommand: "enable",
		IDOrName:   "",
		Config:     testAutomationConfig("http://localhost:9999"),
		Flags:      &AutomationFlags{},
	}
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing ID")
	}
}

func TestAutomationCommand_Enable(t *testing.T) {
	var receivedStatus string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" && strings.Contains(r.URL.Path, "/api/v1/entries/auto1234") {
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			receivedStatus, _ = body["status"].(string)
			resp := types.BrainEntry{ID: "auto1234", Status: "active"}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestAutomationCommand(server.URL, "enable", "auto1234", &AutomationFlags{}, &out)
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if receivedStatus != "active" {
		t.Errorf("expected status 'active', got %q", receivedStatus)
	}
	if !strings.Contains(out.String(), "enabled") {
		t.Errorf("output should contain 'enabled': %q", out.String())
	}
}

func TestAutomationCommand_Disable(t *testing.T) {
	var receivedStatus string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" && strings.Contains(r.URL.Path, "/api/v1/entries/auto1234") {
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			receivedStatus, _ = body["status"].(string)
			resp := types.BrainEntry{ID: "auto1234", Status: "archived"}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestAutomationCommand(server.URL, "disable", "auto1234", &AutomationFlags{}, &out)
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if receivedStatus != "archived" {
		t.Errorf("expected status 'archived', got %q", receivedStatus)
	}
	if !strings.Contains(out.String(), "disabled") {
		t.Errorf("output should contain 'disabled': %q", out.String())
	}
}

// =============================================================================
// History Subcommand Tests
// =============================================================================

func TestAutomationCommand_History_NoID(t *testing.T) {
	cmd := &AutomationCommand{
		Subcommand: "history",
		IDOrName:   "",
		Config:     testAutomationConfig("http://localhost:9999"),
		Flags:      &AutomationFlags{},
	}
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing ID")
	}
}

func TestAutomationCommand_History_WithTasks(t *testing.T) {
	automationEntry := types.BrainEntry{
		ID:        "auto1234",
		Path:      "projects/test/automation/auto1234.md",
		Title:     "On Deploy",
		Type:      "automation",
		Status:    "active",
		ProjectID: "test",
	}
	tasks := []types.BrainEntry{
		{ID: "task0001", Title: "Deploy task 1", Type: "task", Status: "completed", GeneratedBy: "auto1234", Created: "2025-01-01T00:00:00Z"},
		{ID: "task0002", Title: "Deploy task 2", Type: "task", Status: "pending", GeneratedBy: "auto1234", Created: "2025-01-02T00:00:00Z"},
		{ID: "task0003", Title: "Unrelated task", Type: "task", Status: "active", GeneratedBy: "other", Created: "2025-01-03T00:00:00Z"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/api/v1/entries/auto1234") {
			json.NewEncoder(w).Encode(automationEntry)
			return
		}
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/api/v1/entries") && r.URL.Query().Get("type") == "task" {
			resp := types.ListEntriesResponse{Entries: tasks, Total: len(tasks)}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestAutomationCommand(server.URL, "history", "auto1234", &AutomationFlags{Limit: 20}, &out)
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "task0001") {
		t.Errorf("output should contain task generated by automation: %q", output)
	}
	if !strings.Contains(output, "task0002") {
		t.Errorf("output should contain second generated task: %q", output)
	}
	// task0003 has GeneratedBy "other", should be excluded
	if strings.Contains(output, "task0003") {
		t.Errorf("output should NOT contain unrelated task: %q", output)
	}
	if !strings.Contains(output, "Total: 2") {
		t.Errorf("output should show 'Total: 2': %q", output)
	}
}

func TestAutomationCommand_History_NotAutomation(t *testing.T) {
	entry := types.BrainEntry{ID: "abc12def", Type: "task", Title: "Some task"}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/api/v1/entries/abc12def") {
			json.NewEncoder(w).Encode(entry)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestAutomationCommand(server.URL, "history", "abc12def", &AutomationFlags{}, &out)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for non-automation entry")
	}
	if !strings.Contains(err.Error(), "not an automation") {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// Create Subcommand Tests
// =============================================================================

func TestAutomationCommand_Create(t *testing.T) {
	var receivedReq types.CreateEntryRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.Contains(r.URL.Path, "/api/v1/entries") {
			json.NewDecoder(r.Body).Decode(&receivedReq)
			w.WriteHeader(http.StatusCreated)
			resp := types.CreateEntryResponse{ID: "newauto1", Path: "projects/test/automation/newauto1.md"}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Simulate interactive input: name, trigger=event, event name, action=prompt, prompt text, agent (skip), project (skip)
	input := "My Test Automation\n1\ntask.completed\n1\nReview the task\n\n\n"

	var out bytes.Buffer
	cmd := newTestAutomationCommand(server.URL, "create", "", &AutomationFlags{}, &out)
	cmd.In = strings.NewReader(input)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if receivedReq.Type != "automation" {
		t.Errorf("expected type 'automation', got %q", receivedReq.Type)
	}
	if receivedReq.Title != "My Test Automation" {
		t.Errorf("expected title 'My Test Automation', got %q", receivedReq.Title)
	}
	if receivedReq.Trigger == nil || receivedReq.Trigger.Type != "event" {
		t.Errorf("expected event trigger, got %+v", receivedReq.Trigger)
	}
	if receivedReq.Trigger != nil && receivedReq.Trigger.Event != "task.completed" {
		t.Errorf("expected event 'task.completed', got %q", receivedReq.Trigger.Event)
	}
	if receivedReq.Action == nil || receivedReq.Action.Type != "prompt" {
		t.Errorf("expected prompt action, got %+v", receivedReq.Action)
	}

	output := out.String()
	if !strings.Contains(output, "Automation created") {
		t.Errorf("output should contain 'Automation created': %q", output)
	}
}

// =============================================================================
// matchesEvent Tests
// =============================================================================

func TestMatchesEvent(t *testing.T) {
	tests := []struct {
		pattern   string
		eventName string
		want      bool
	}{
		{"task.completed", "task.completed", true},
		{"task.completed", "task.created", false},
		{"task.*", "task.completed", true},
		{"task.*", "task.created", true},
		{"task.*", "entry.created", false},
		{"*", "anything", true},
		{"entry.created", "entry.created", true},
		{"entry.created", "entry.deleted", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_vs_"+tt.eventName, func(t *testing.T) {
			got := matchesEvent(tt.pattern, tt.eventName)
			if got != tt.want {
				t.Errorf("matchesEvent(%q, %q) = %v, want %v", tt.pattern, tt.eventName, got, tt.want)
			}
		})
	}
}

// =============================================================================
// truncate Tests
// =============================================================================

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a long string", 10, "this is..."},
		{"ab", 2, "ab"},
		{"abc", 2, "ab"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncate(tt.input, tt.max)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
			}
		})
	}
}

// =============================================================================
// Test Helpers
// =============================================================================

func testAutomationConfig(apiURL string) *UnifiedConfig {
	cfg := &UnifiedConfig{}
	cfg.Runner.BrainAPIURL = apiURL
	cfg.Runner.APITimeout = 5000
	return cfg
}

func newTestAutomationCommand(apiURL, subcommand, idOrName string, flags *AutomationFlags, out *bytes.Buffer) *AutomationCommand {
	cfg := testAutomationConfig(apiURL)
	return &AutomationCommand{
		Subcommand: subcommand,
		IDOrName:   idOrName,
		Config:     cfg,
		Flags:      flags,
		Out:        out,
		apiClient:  runner.NewAPIClient(cfg.Runner),
	}
}

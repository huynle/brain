package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/runner"
)

// =============================================================================
// Test Helpers
// =============================================================================

// setupTestMonitorTemplates populates the modal with default monitor templates
// for testing. Since templates are now fetched from the API in production,
// tests need to pre-populate them.
func setupTestMonitorTemplates(modal *MetadataModal) {
	modal.monitorTemplates = []MonitorTemplateState{
		{TemplateID: "blocked-inspector", Label: "Blocked Inspector", Status: "loading", Schedule: "*/30 * * * *"},
		{TemplateID: "feature-review", Label: "Feature Review", Status: "loading", Schedule: "one-shot"},
	}
	modal.monitorLoading = false
}

// =============================================================================
// Monitor Template Initialization Tests
// =============================================================================

func TestMetadataModalFeature_InitializesEmpty(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient("http://localhost:3333", "")

	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)

	// Templates are now fetched from API in Init(), so they start empty
	if modal.monitorTemplates != nil {
		t.Errorf("monitorTemplates should be nil initially, got %d items", len(modal.monitorTemplates))
	}

	// monitorLoading should be true (will fetch in Init)
	if !modal.monitorLoading {
		t.Error("monitorLoading should be true initially")
	}
	// focusedMonitorIndex should be -1 (not focused)
	if modal.focusedMonitorIndex != -1 {
		t.Errorf("focusedMonitorIndex = %d, want -1", modal.focusedMonitorIndex)
	}

	// monitorClient should be set
	if modal.monitorClient == nil {
		t.Error("monitorClient should not be nil")
	}
}

func TestMetadataModalFeature_NonFeatureMode_NoMonitorTemplates(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	// Single mode should not have monitor templates
	modal := NewMetadataModal("task123", apiClient)
	if len(modal.monitorTemplates) != 0 {
		t.Errorf("single mode should have 0 monitor templates, got %d", len(modal.monitorTemplates))
	}

	// Batch mode should not have monitor templates
	modalBatch := NewMetadataModalBatch([]string{"t1", "t2"}, apiClient)
	if len(modalBatch.monitorTemplates) != 0 {
		t.Errorf("batch mode should have 0 monitor templates, got %d", len(modalBatch.monitorTemplates))
	}
}

// =============================================================================
// Monitor Status Fetch Tests
// =============================================================================

func TestMetadataModalFeature_HandleMonitorFetchedMsg(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient("http://localhost:3333", "")
	modal := NewMetadataModalFeature("dark-mode", "brain-api", apiClient, monitorClient)

	// Step 1: Simulate template list fetch (sets template structure)
	listMsg := monitorTemplatesListedMsg{
		templates: []MonitorTemplateState{
			{TemplateID: "blocked-inspector", Label: "Blocked Inspector", Status: "loading", Schedule: "*/30 * * * *"},
			{TemplateID: "feature-review", Label: "Feature Review", Status: "loading", Schedule: "one-shot"},
		},
		err: nil,
	}
	modal.Update(listMsg)

	if len(modal.monitorTemplates) != 2 {
		t.Fatalf("monitorTemplates length = %d, want 2 after list fetch", len(modal.monitorTemplates))
	}

	// Step 2: Simulate status fetch (updates each template's status)
	statusMsg := monitorTemplatesFetchedMsg{
		templates: []MonitorTemplateState{
			{TemplateID: "blocked-inspector", Label: "Blocked Inspector", Status: "enabled", Schedule: "*/30 * * * *", TaskPath: "projects/brain-api/task/monitor123.md"},
			{TemplateID: "feature-review", Label: "Feature Review", Status: "create", Schedule: "one-shot"},
		},
		err: nil,
	}
	modal.Update(statusMsg)

	// Check that templates were updated with statuses
	if modal.monitorLoading {
		t.Error("monitorLoading should be false after status fetch")
	}
	if modal.monitorTemplates[0].Status != "enabled" {
		t.Errorf("blocked-inspector status = %q, want %q", modal.monitorTemplates[0].Status, "enabled")
	}
	if modal.monitorTemplates[0].TaskPath != "projects/brain-api/task/monitor123.md" {
		t.Errorf("blocked-inspector taskPath = %q, want %q", modal.monitorTemplates[0].TaskPath, "projects/brain-api/task/monitor123.md")
	}
	if modal.monitorTemplates[1].Status != "create" {
		t.Errorf("feature-review status = %q, want %q", modal.monitorTemplates[1].Status, "create")
	}
}

// =============================================================================
// Navigation Tests
// =============================================================================

func TestMetadataModalFeature_NavigateFromFieldsToMonitors(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient("http://localhost:3333", "")
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)
	setupTestMonitorTemplates(modal)

	// Set monitor templates to non-loading state
	modal.monitorTemplates[0].Status = "enabled"
	modal.monitorTemplates[1].Status = "create"
	modal.monitorLoading = false

	// Navigate to last field
	lastFieldIndex := len(modal.fieldList) - 1
	modal.focusedIndex = lastFieldIndex
	modal.focusedField = modal.fieldList[lastFieldIndex]

	// Press down - should move to first monitor row
	handled, _ := modal.HandleKey("j")
	if !handled {
		t.Error("j key should be handled")
	}
	if modal.focusedMonitorIndex != 0 {
		t.Errorf("focusedMonitorIndex = %d, want 0", modal.focusedMonitorIndex)
	}
	// focusedIndex should be beyond field list to indicate monitor zone
	if modal.focusedIndex != len(modal.fieldList) {
		t.Errorf("focusedIndex = %d, want %d (beyond field list)", modal.focusedIndex, len(modal.fieldList))
	}
}

func TestMetadataModalFeature_NavigateFromMonitorsToFields(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient("http://localhost:3333", "")
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)
	setupTestMonitorTemplates(modal)

	// Set monitor templates to non-loading state
	modal.monitorTemplates[0].Status = "enabled"
	modal.monitorTemplates[1].Status = "create"
	modal.monitorLoading = false

	// Start at first monitor row
	modal.focusedIndex = len(modal.fieldList)
	modal.focusedMonitorIndex = 0

	// Press up - should move back to last field
	handled, _ := modal.HandleKey("k")
	if !handled {
		t.Error("k key should be handled")
	}
	if modal.focusedMonitorIndex != -1 {
		t.Errorf("focusedMonitorIndex = %d, want -1", modal.focusedMonitorIndex)
	}
	lastFieldIndex := len(modal.fieldList) - 1
	if modal.focusedIndex != lastFieldIndex {
		t.Errorf("focusedIndex = %d, want %d (last field)", modal.focusedIndex, lastFieldIndex)
	}
	if modal.focusedField != modal.fieldList[lastFieldIndex] {
		t.Errorf("focusedField = %q, want %q", modal.focusedField, modal.fieldList[lastFieldIndex])
	}
}

func TestMetadataModalFeature_NavigateBetweenMonitorRows(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient("http://localhost:3333", "")
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)
	setupTestMonitorTemplates(modal)

	// Set monitor templates to non-loading state
	modal.monitorTemplates[0].Status = "enabled"
	modal.monitorTemplates[1].Status = "create"
	modal.monitorLoading = false

	// Start at first monitor row
	modal.focusedIndex = len(modal.fieldList)
	modal.focusedMonitorIndex = 0

	// Press down - should move to second monitor row
	handled, _ := modal.HandleKey("j")
	if !handled {
		t.Error("j key should be handled")
	}
	if modal.focusedMonitorIndex != 1 {
		t.Errorf("focusedMonitorIndex = %d, want 1", modal.focusedMonitorIndex)
	}

	// Press down again - should wrap to first field (top of list)
	handled, _ = modal.HandleKey("j")
	if !handled {
		t.Error("j key should be handled")
	}
	if modal.focusedMonitorIndex != -1 {
		t.Errorf("focusedMonitorIndex = %d, want -1 (back to fields)", modal.focusedMonitorIndex)
	}
	if modal.focusedIndex != 0 {
		t.Errorf("focusedIndex = %d, want 0 (first field)", modal.focusedIndex)
	}
}

func TestMetadataModalFeature_NavigateUp_WrapsFromFirstFieldToLastMonitor(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient("http://localhost:3333", "")
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)
	setupTestMonitorTemplates(modal)

	// Set monitor templates to non-loading state
	modal.monitorTemplates[0].Status = "enabled"
	modal.monitorTemplates[1].Status = "create"
	modal.monitorLoading = false

	// Start at first field
	modal.focusedIndex = 0
	modal.focusedField = modal.fieldList[0]
	modal.focusedMonitorIndex = -1

	// Press up - should wrap to last monitor row
	handled, _ := modal.HandleKey("k")
	if !handled {
		t.Error("k key should be handled")
	}
	if modal.focusedMonitorIndex != len(modal.monitorTemplates)-1 {
		t.Errorf("focusedMonitorIndex = %d, want %d (last monitor)", modal.focusedMonitorIndex, len(modal.monitorTemplates)-1)
	}
}

// =============================================================================
// Toggle Tests
// =============================================================================

func TestMetadataModalFeature_ToggleMonitor_Create(t *testing.T) {
	createCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Monitor create endpoint
		if r.URL.Path == "/api/v1/monitors" && r.Method == http.MethodPost {
			createCalled = true
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"monitor": map[string]interface{}{
					"taskId": "new-task-id",
					"path":   "projects/brain-api/task/new-monitor.md",
				},
			})
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := runner.RunnerConfig{BrainAPIURL: srv.URL, APITimeout: 5000}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient(srv.URL, "")
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)
	setupTestMonitorTemplates(modal)

	// Set feature-review to "create" status
	modal.monitorTemplates[1].Status = "create"
	modal.monitorLoading = false

	// Focus on feature-review monitor row
	modal.focusedIndex = len(modal.fieldList) + 1
	modal.focusedMonitorIndex = 1

	// Press enter to toggle
	handled, cmd := modal.HandleKey("enter")
	if !handled {
		t.Error("enter key should be handled on monitor row")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd for monitor toggle")
	}

	// Template should be set to loading
	if modal.monitorTemplates[1].Status != "loading" {
		t.Errorf("status should be 'loading' during toggle, got %q", modal.monitorTemplates[1].Status)
	}

	// Execute the command
	msg := cmd()
	toggleMsg, ok := msg.(monitorToggleResultMsg)
	if !ok {
		t.Fatalf("expected monitorToggleResultMsg, got %T", msg)
	}

	if toggleMsg.err != nil {
		t.Fatalf("unexpected error: %v", toggleMsg.err)
	}
	if toggleMsg.newStatus != "enabled" {
		t.Errorf("newStatus = %q, want %q", toggleMsg.newStatus, "enabled")
	}

	if !createCalled {
		t.Error("expected create API to be called")
	}
}

func TestMetadataModalFeature_ToggleMonitor_Delete(t *testing.T) {
	deleteCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Monitor delete endpoint
		if strings.HasPrefix(r.URL.Path, "/api/v1/monitors/") && r.Method == http.MethodDelete {
			deleteCalled = true
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := runner.RunnerConfig{BrainAPIURL: srv.URL, APITimeout: 5000}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient(srv.URL, "")
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)
	setupTestMonitorTemplates(modal)

	// Set feature-review to "enabled" status with a task path
	modal.monitorTemplates[1].Status = "enabled"
	modal.monitorTemplates[1].TaskPath = "existing-task-id"
	modal.monitorLoading = false

	// Focus on feature-review monitor row
	modal.focusedIndex = len(modal.fieldList) + 1
	modal.focusedMonitorIndex = 1

	// Press enter to toggle
	handled, cmd := modal.HandleKey("enter")
	if !handled {
		t.Error("enter key should be handled on monitor row")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd for monitor toggle")
	}

	// Execute the command
	msg := cmd()
	toggleMsg, ok := msg.(monitorToggleResultMsg)
	if !ok {
		t.Fatalf("expected monitorToggleResultMsg, got %T", msg)
	}

	if toggleMsg.err != nil {
		t.Fatalf("unexpected error: %v", toggleMsg.err)
	}
	if toggleMsg.newStatus != "create" {
		t.Errorf("newStatus = %q, want %q", toggleMsg.newStatus, "create")
	}

	if !deleteCalled {
		t.Error("expected delete API to be called")
	}
}

func TestMetadataModalFeature_ToggleScheduledTask_Create(t *testing.T) {
	createCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Monitors create endpoint (unified path for all templates)
		if r.URL.Path == "/api/v1/monitors" && r.Method == http.MethodPost {
			createCalled = true
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":    "scheduled123",
				"path":  "projects/brain-api/task/scheduled123.md",
				"title": "Blocked Inspector: feat-auth",
			})
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := runner.RunnerConfig{BrainAPIURL: srv.URL, APITimeout: 5000}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient(srv.URL, "")
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)
	setupTestMonitorTemplates(modal)

	// Set blocked-inspector to "create" status
	modal.monitorTemplates[0].Status = "create"
	modal.monitorLoading = false

	// Focus on blocked-inspector monitor row
	modal.focusedIndex = len(modal.fieldList)
	modal.focusedMonitorIndex = 0

	// Press enter to toggle
	handled, cmd := modal.HandleKey("enter")
	if !handled {
		t.Error("enter key should be handled on monitor row")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd for monitor toggle")
	}

	// Execute the command
	msg := cmd()
	toggleMsg, ok := msg.(monitorToggleResultMsg)
	if !ok {
		t.Fatalf("expected monitorToggleResultMsg, got %T", msg)
	}

	if toggleMsg.err != nil {
		t.Fatalf("unexpected error: %v", toggleMsg.err)
	}
	if toggleMsg.newStatus != "enabled" {
		t.Errorf("newStatus = %q, want %q", toggleMsg.newStatus, "enabled")
	}

	if !createCalled {
		t.Error("expected create API to be called")
	}
}

func TestMetadataModalFeature_ToggleScheduledTask_Delete(t *testing.T) {
	deleteCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Entries delete endpoint
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/entries/") {
			deleteCalled = true
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := runner.RunnerConfig{BrainAPIURL: srv.URL, APITimeout: 5000}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient(srv.URL, "")
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)
	setupTestMonitorTemplates(modal)

	// Set blocked-inspector to "enabled" status with a task path
	modal.monitorTemplates[0].Status = "enabled"
	modal.monitorTemplates[0].TaskPath = "projects/brain-api/task/monitor123.md"
	modal.monitorLoading = false

	// Focus on blocked-inspector monitor row
	modal.focusedIndex = len(modal.fieldList)
	modal.focusedMonitorIndex = 0

	// Press enter to toggle
	handled, cmd := modal.HandleKey("enter")
	if !handled {
		t.Error("enter key should be handled on monitor row")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd for monitor toggle")
	}

	// Execute the command
	msg := cmd()
	toggleMsg, ok := msg.(monitorToggleResultMsg)
	if !ok {
		t.Fatalf("expected monitorToggleResultMsg, got %T", msg)
	}

	if toggleMsg.err != nil {
		t.Fatalf("unexpected error: %v", toggleMsg.err)
	}
	if toggleMsg.newStatus != "create" {
		t.Errorf("newStatus = %q, want %q", toggleMsg.newStatus, "create")
	}

	if !deleteCalled {
		t.Error("expected delete API to be called")
	}
}

// =============================================================================
// Toggle Result Message Handling Tests
// =============================================================================

func TestMetadataModalFeature_HandleToggleResult_Success(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient("http://localhost:3333", "")
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)
	setupTestMonitorTemplates(modal)

	// Set initial state
	modal.monitorTemplates[1].Status = "loading"

	// Send toggle result
	resultMsg := monitorToggleResultMsg{
		index:     1,
		newStatus: "enabled",
		taskPath:  "projects/brain-api/task/new-monitor.md",
		err:       nil,
	}

	modal.Update(resultMsg)

	if modal.monitorTemplates[1].Status != "enabled" {
		t.Errorf("status = %q, want %q", modal.monitorTemplates[1].Status, "enabled")
	}
	if modal.monitorTemplates[1].TaskPath != "projects/brain-api/task/new-monitor.md" {
		t.Errorf("taskPath = %q, want %q", modal.monitorTemplates[1].TaskPath, "projects/brain-api/task/new-monitor.md")
	}
}

func TestMetadataModalFeature_HandleToggleResult_Revert(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient("http://localhost:3333", "")
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)
	setupTestMonitorTemplates(modal)

	// Set initial state - was "create", toggling to create it
	modal.monitorTemplates[1].Status = "loading"

	// Send toggle result with revert status
	resultMsg := monitorToggleResultMsg{
		index:     1,
		newStatus: "create",
		taskPath:  "",
		err:       nil,
	}

	modal.Update(resultMsg)

	if modal.monitorTemplates[1].Status != "create" {
		t.Errorf("status = %q, want %q", modal.monitorTemplates[1].Status, "create")
	}
}

// =============================================================================
// Rendering Tests
// =============================================================================

func TestMetadataModalFeature_View_ShowsMonitorSection(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient("http://localhost:3333", "")
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)
	setupTestMonitorTemplates(modal)

	// Set up loaded state
	modal.loading = false
	modal.monitorLoading = false
	modal.monitorTemplates[0].Status = "enabled"
	modal.monitorTemplates[0].Schedule = "*/30 * * * *"
	modal.monitorTemplates[1].Status = "create"

	view := modal.View()

	// Should contain the separator
	if !strings.Contains(view, "Automated Tasks") {
		t.Error("View should contain 'Automated Tasks' separator")
	}

	// Should contain template labels
	if !strings.Contains(view, "Blocked Inspector") {
		t.Error("View should contain 'Blocked Inspector' label")
	}
	if !strings.Contains(view, "Feature Review") {
		t.Error("View should contain 'Feature Review' label")
	}
}

func TestMetadataModalFeature_View_ShowsStatusIcons(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient("http://localhost:3333", "")
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)
	setupTestMonitorTemplates(modal)

	// Set up loaded state
	modal.loading = false
	modal.monitorLoading = false
	modal.monitorTemplates[0].Status = "enabled"
	modal.monitorTemplates[1].Status = "create"

	view := modal.View()

	// Should contain filled circle for enabled
	if !strings.Contains(view, "●") {
		t.Error("View should contain filled circle (●) for enabled monitor")
	}
	// Should contain empty circle for create
	if !strings.Contains(view, "○") {
		t.Error("View should contain empty circle (○) for create monitor")
	}
}

func TestMetadataModalFeature_View_ShowsLoadingState(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient("http://localhost:3333", "")
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)
	setupTestMonitorTemplates(modal)

	// Set up: main data loaded but monitors still loading
	modal.loading = false
	modal.monitorLoading = true

	view := modal.View()

	// Should contain loading indicator for monitors
	if !strings.Contains(view, "Loading") {
		t.Error("View should show loading state for monitors")
	}
}

func TestMetadataModalFeature_View_FocusedMonitorRow(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient("http://localhost:3333", "")
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)
	setupTestMonitorTemplates(modal)

	// Set up loaded state
	modal.loading = false
	modal.monitorLoading = false
	modal.monitorTemplates[0].Status = "enabled"
	modal.monitorTemplates[1].Status = "create"

	// Focus on first monitor row
	modal.focusedIndex = len(modal.fieldList)
	modal.focusedMonitorIndex = 0

	view := modal.View()

	// Should contain arrow indicator for focused row
	if !strings.Contains(view, "→") {
		t.Error("View should contain arrow (→) for focused monitor row")
	}
}

func TestMetadataModalFeature_View_ShowsStatusTags(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient("http://localhost:3333", "")
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)
	setupTestMonitorTemplates(modal)

	// Set up loaded state
	modal.loading = false
	modal.monitorLoading = false
	modal.monitorTemplates[0].Status = "enabled"
	modal.monitorTemplates[1].Status = "create"

	view := modal.View()

	// Should contain status tags
	if !strings.Contains(view, "[enabled]") {
		t.Error("View should contain '[enabled]' tag")
	}
	if !strings.Contains(view, "[create]") {
		t.Error("View should contain '[create]' tag")
	}
}

// =============================================================================
// Height Calculation Tests
// =============================================================================

func TestMetadataModalFeature_Height_IncludesMonitorRows(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient("http://localhost:3333", "")
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)

	// Feature mode height should account for monitor rows
	// Base height (20) + separator (1) + template count (2) + spacing (1) = 24
	expectedMinHeight := 24
	if modal.Height() < expectedMinHeight {
		t.Errorf("Height() = %d, want >= %d (should include monitor rows)", modal.Height(), expectedMinHeight)
	}
}

// =============================================================================
// Enter on Monitor Row Does Not Enter Edit Mode
// =============================================================================

func TestMetadataModalFeature_EnterOnMonitorRow_DoesNotEnterEditMode(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient("http://localhost:3333", "")
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)
	setupTestMonitorTemplates(modal)

	// Set up loaded state
	modal.monitorTemplates[0].Status = "create"
	modal.monitorLoading = false

	// Focus on monitor row
	modal.focusedIndex = len(modal.fieldList)
	modal.focusedMonitorIndex = 0

	// Press enter
	modal.HandleKey("enter")

	// Should NOT enter edit mode (text or dropdown)
	if modal.interactionMode == ModeEditText || modal.interactionMode == ModeEditDropdown {
		t.Error("enter on monitor row should toggle, not enter edit mode")
	}
}

// =============================================================================
// Space Key Toggle Tests
// =============================================================================

func TestMetadataModalFeature_SpaceToggle(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient("http://localhost:3333", "")
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)
	setupTestMonitorTemplates(modal)

	// Set up loaded state
	modal.monitorTemplates[0].Status = "create"
	modal.monitorLoading = false

	// Focus on monitor row
	modal.focusedIndex = len(modal.fieldList)
	modal.focusedMonitorIndex = 0

	// Press space - should also toggle
	handled, cmd := modal.HandleKey(" ")
	if !handled {
		t.Error("space key should be handled on monitor row")
	}
	if cmd == nil {
		t.Error("space key should return a cmd for toggle")
	}
}

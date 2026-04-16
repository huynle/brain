package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/runner"
)

// TestNewMetadataModalFeature tests the feature mode constructor.
func TestNewMetadataModalFeature(t *testing.T) {
	// Create mock API client
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	// Create modal with feature ID and project ID
	featureID := "feat-auth-123"
	projectID := "brain-api"
	modal := NewMetadataModalFeature(featureID, projectID, apiClient)

	if modal == nil {
		t.Fatal("NewMetadataModalFeature returned nil")
	}

	// Check that featureID is set
	if modal.featureID != featureID {
		t.Errorf("featureID = %q, want %q", modal.featureID, featureID)
	}

	// Check that projectID is set
	if modal.projectID != projectID {
		t.Errorf("projectID = %q, want %q", modal.projectID, projectID)
	}

	// Check mode is feature
	if modal.mode != ModeFeature {
		t.Errorf("mode = %v, want ModeFeature", modal.mode)
	}

	// Check that apiClient is set
	if modal.apiClient == nil {
		t.Error("apiClient is nil")
	}

	// Check that taskPaths starts empty (will be populated in Init)
	if len(modal.taskPaths) != 0 {
		t.Errorf("taskPaths length = %d, want 0 (populated in Init)", len(modal.taskPaths))
	}

	// Check initial interaction mode
	if modal.interactionMode != ModeNavigate {
		t.Errorf("interactionMode = %v, want ModeNavigate", modal.interactionMode)
	}

	// Check dimensions
	if modal.width != 60 {
		t.Errorf("width = %d, want 60", modal.width)
	}
	if modal.height != 24 {
		t.Errorf("height = %d, want 24", modal.height)
	}
}

// TestMetadataModalFeature_Title tests the title includes feature ID and task count.
func TestMetadataModalFeature_Title(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	featureID := "feat-auth-123"
	modal := NewMetadataModalFeature(featureID, "brain-api", apiClient)

	// Simulate Init populating taskPaths
	modal.taskPaths = []string{"task1", "task2", "task3"}

	expectedTitle := "Update Feature Metadata - feat-auth-123 (3 tasks)"
	actualTitle := modal.Title()

	if actualTitle != expectedTitle {
		t.Errorf("Title() = %q, want %q", actualTitle, expectedTitle)
	}
}

// TestMetadataModalFeature_Title_NoTasks tests title when no tasks loaded yet.
func TestMetadataModalFeature_Title_NoTasks(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	featureID := "feat-auth-123"
	modal := NewMetadataModalFeature(featureID, "brain-api", apiClient)

	// taskIDs should be empty initially
	expectedTitle := "Update Feature Metadata - feat-auth-123 (0 tasks)"
	actualTitle := modal.Title()

	if actualTitle != expectedTitle {
		t.Errorf("Title() = %q, want %q", actualTitle, expectedTitle)
	}
}

// TestMetadataModalFeature_Init_FetchesFeatureTasks tests that Init fetches feature tasks and populates taskPaths.
func TestMetadataModalFeature_Init_FetchesFeatureTasks(t *testing.T) {
	// Create test server that returns feature task data
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Feature endpoint (GET /tasks/:project/features/:featureId)
		if r.URL.Path == "/api/v1/tasks/brain-api/features/dark-mode" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"feature": map[string]interface{}{
					"featureId": "dark-mode",
					"tasks": []map[string]interface{}{
						{"id": "task1", "path": "projects/brain-api/task/task1.md", "title": "Task 1", "status": "active"},
						{"id": "task2", "path": "projects/brain-api/task/task2.md", "title": "Task 2", "status": "pending"},
					},
				},
			})
			return
		}

		// Entry endpoints for task paths (r.URL.Path decodes percent-encoding)
		if strings.HasPrefix(r.URL.Path, "/api/v1/entries/") && (strings.Contains(r.URL.Path, "task1") || strings.Contains(r.URL.Path, "task2")) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":               "active",
				"priority":             "high",
				"feature_id":           "dark-mode",
				"feature_priority":     "high",
				"feature_depends_on":   []string{"feature1"},
				"complete_on_idle":     false,
				"open_pr_before_merge": false,
			})
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := runner.RunnerConfig{BrainAPIURL: srv.URL, APITimeout: 5000}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModalFeature("dark-mode", "brain-api", apiClient)

	// Call Init and execute the returned command
	cmd := modal.Init()
	if cmd == nil {
		t.Fatal("Init() returned nil cmd")
	}

	// Execute the command to fetch data
	msg := cmd()

	// Check that we got a metadataFetchedMsg
	fetchedMsg, ok := msg.(metadataFetchedMsg)
	if !ok {
		t.Fatalf("expected metadataFetchedMsg, got %T", msg)
	}

	// Check no error
	if fetchedMsg.err != nil {
		t.Fatalf("unexpected error: %v", fetchedMsg.err)
	}

	// Check that we got 2 entries (one for each task)
	if len(fetchedMsg.entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(fetchedMsg.entries))
	}
}

// TestMetadataModalFeature_Has5Tabs tests that feature mode has 5 tabs.
func TestMetadataModalFeature_Has5Tabs(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModalFeature("feat-auth-123", "brain-api", apiClient)

	if len(modal.tabs) != 5 {
		t.Errorf("Feature mode tab count = %d, want 5", len(modal.tabs))
	}

	// Verify tab order
	expectedTabs := []MetadataTab{MetaTabFeature, MetaTabTask, MetaTabExecution, MetaTabGitMerge, MetaTabAutomations}
	for i, tab := range expectedTabs {
		if modal.tabs[i] != tab {
			t.Errorf("tabs[%d] = %v, want %v", i, modal.tabs[i], tab)
		}
	}
}

// TestMetadataModalFeature_StartsOnFeatureTab tests that feature mode starts on the Feature tab.
func TestMetadataModalFeature_StartsOnFeatureTab(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModalFeature("feat-auth-123", "brain-api", apiClient)

	if modal.currentTab != MetaTabFeature {
		t.Errorf("initial tab = %v, want MetaTabFeature", modal.currentTab)
	}
}

// TestMetadataModalFeature_MonitorsOnlyInMonitorsTab tests that monitors only render on the Monitors tab.
func TestMetadataModalFeature_MonitorsOnlyInMonitorsTab(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	monitorClient := NewMonitorClient("http://localhost:3333", "")
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient, monitorClient)

	// Set up monitor templates
	modal.monitorTemplates = []MonitorTemplateState{
		{TemplateID: "blocked-inspector", Label: "Blocked Inspector", Status: "enabled", Schedule: "*/30 * * * *"},
	}
	modal.monitorLoading = false
	modal.loading = false

	// On Feature tab (default), monitors should NOT appear
	view := modal.View()
	if strings.Contains(view, "Automated Tasks") {
		t.Error("Feature tab should NOT show 'Automated Tasks' section")
	}

	// Switch to Monitors tab — monitors should appear
	modal.switchToTab(MetaTabMonitors)
	view = modal.View()
	if !strings.Contains(view, "Automated Tasks") {
		t.Error("Monitors tab should show 'Automated Tasks' section")
	}
}

// TestMetadataModalFeature_TabHeaderRendered tests that the tab header is rendered.
func TestMetadataModalFeature_TabHeaderRendered(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModalFeature("feat-auth", "brain-api", apiClient)
	modal.loading = false
	modal.width = 80 // Ensure all tabs fit within the visible tab range

	view := modal.View()

	// Should contain tab labels
	if !strings.Contains(view, "Feature") {
		t.Error("View should contain 'Feature' tab label")
	}
	if !strings.Contains(view, "Task") {
		t.Error("View should contain 'Task' tab label")
	}
	if !strings.Contains(view, "Execution") {
		t.Error("View should contain 'Execution' tab label")
	}
	if !strings.Contains(view, "Git & Merge") {
		t.Error("View should contain 'Git & Merge' tab label")
	}
	if !strings.Contains(view, "Automations") {
		t.Error("View should contain 'Automations' tab label")
	}
}

// TestMetadataModalFeature_Init_ErrorHandling tests error scenarios in Init.
func TestMetadataModalFeature_Init_ErrorHandling(t *testing.T) {
	t.Run("feature not found", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "feature not found", http.StatusNotFound)
		}))
		defer srv.Close()

		cfg := runner.RunnerConfig{BrainAPIURL: srv.URL, APITimeout: 5000}
		apiClient := runner.NewAPIClient(cfg)
		modal := NewMetadataModalFeature("nonexistent", "brain-api", apiClient)

		cmd := modal.Init()
		msg := cmd()

		fetchedMsg, ok := msg.(metadataFetchedMsg)
		if !ok {
			t.Fatalf("expected metadataFetchedMsg, got %T", msg)
		}

		if fetchedMsg.err == nil {
			t.Error("expected error for 404 response")
		}
	})

	t.Run("task entry fetch fails", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			// Feature endpoint succeeds
			if r.URL.Path == "/api/v1/tasks/brain-api/features/dark-mode" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"feature": map[string]interface{}{
						"featureId": "dark-mode",
						"tasks": []map[string]interface{}{
							{"id": "task1", "path": "projects/brain-api/task/task1.md", "title": "Task 1"},
						},
					},
				})
				return
			}

			// Entry endpoint fails
			http.Error(w, "entry not found", http.StatusNotFound)
		}))
		defer srv.Close()

		cfg := runner.RunnerConfig{BrainAPIURL: srv.URL, APITimeout: 5000}
		apiClient := runner.NewAPIClient(cfg)
		modal := NewMetadataModalFeature("dark-mode", "brain-api", apiClient)

		cmd := modal.Init()
		msg := cmd()

		fetchedMsg, ok := msg.(metadataFetchedMsg)
		if !ok {
			t.Fatalf("expected metadataFetchedMsg, got %T", msg)
		}

		// Should have error from entry fetch failure
		if fetchedMsg.err == nil {
			t.Error("expected error from entry fetch failure")
		}
	})
}

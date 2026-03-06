package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	// Check that taskIDs starts empty (will be populated in Init)
	if len(modal.taskIDs) != 0 {
		t.Errorf("taskIDs length = %d, want 0 (populated in Init)", len(modal.taskIDs))
	}

	// Check initial interaction mode
	if modal.interactionMode != ModeNavigate {
		t.Errorf("interactionMode = %v, want ModeNavigate", modal.interactionMode)
	}

	// Check dimensions
	if modal.width != 60 {
		t.Errorf("width = %d, want 60", modal.width)
	}
	if modal.height != 20 {
		t.Errorf("height = %d, want 20", modal.height)
	}
}

// TestMetadataModalFeature_Title tests the title includes feature ID and task count.
func TestMetadataModalFeature_Title(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	featureID := "feat-auth-123"
	modal := NewMetadataModalFeature(featureID, "brain-api", apiClient)

	// Simulate Init populating taskIDs
	modal.taskIDs = []string{"task1", "task2", "task3"}

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

// TestMetadataModalFeature_Init_FetchesFeatureTasks tests that Init fetches feature and populates taskIDs.
func TestMetadataModalFeature_Init_FetchesFeatureTasks(t *testing.T) {
	// Create test server that returns feature data
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Feature endpoint
		if r.URL.Path == "/api/v1/tasks/brain-api/features/dark-mode" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"feature": map[string]interface{}{
					"featureId": "dark-mode",
					"tasks": []map[string]interface{}{
						{"id": "task1", "title": "Task 1", "status": "active"},
						{"id": "task2", "title": "Task 2", "status": "pending"},
					},
					"ready": true,
				},
			})
			return
		}

		// Entry endpoints for task1 and task2
		if r.URL.Path == "/api/v1/entries/task1" || r.URL.Path == "/api/v1/entries/task2" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entry": map[string]interface{}{
					"status":            "active",
					"priority":          "high",
					"featureId":         "dark-mode",
					"featurePriority":   "high",
					"featureDependsOn":  []string{"feature1"},
					"completeOnIdle":    false,
					"openPRBeforeMerge": false,
				},
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
							{"id": "task1", "title": "Task 1"},
						},
						"ready": true,
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

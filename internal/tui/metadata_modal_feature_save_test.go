package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/runner"
)

// TestMetadataModalFeature_SaveField_UpdatesAllTasks tests that saving a field updates all tasks in the feature.
func TestMetadataModalFeature_SaveField_UpdatesAllTasks(t *testing.T) {
	updateCount := 0
	updatedTasks := make(map[string]bool)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Feature endpoint
		if r.URL.Path == "/api/v1/tasks/brain-api/features/dark-mode" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"feature": map[string]interface{}{
					"featureId": "dark-mode",
					"tasks": []map[string]interface{}{
						{"id": "task1", "title": "Task 1", "status": "active"},
						{"id": "task2", "title": "Task 2", "status": "active"},
						{"id": "task3", "title": "Task 3", "status": "pending"},
					},
					"ready": true,
				},
			})
			return
		}

		// Entry GET endpoints
		if r.Method == http.MethodGet && (r.URL.Path == "/api/v1/entries/task1" || r.URL.Path == "/api/v1/entries/task2" || r.URL.Path == "/api/v1/entries/task3") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entry": map[string]interface{}{
					"status":            "active",
					"priority":          "medium",
					"featureId":         "dark-mode",
					"featurePriority":   "high",
					"completeOnIdle":    false,
					"openPRBeforeMerge": false,
				},
			})
			return
		}

		// Entry PATCH endpoints - track updates
		if r.Method == http.MethodPatch {
			updateCount++
			// Extract task ID from path
			if r.URL.Path == "/api/v1/entries/task1" {
				updatedTasks["task1"] = true
			} else if r.URL.Path == "/api/v1/entries/task2" {
				updatedTasks["task2"] = true
			} else if r.URL.Path == "/api/v1/entries/task3" {
				updatedTasks["task3"] = true
			}

			// Return success
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entry": map[string]interface{}{
					"status": "completed",
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

	// Initialize to populate taskIDs
	cmd := modal.Init()
	msg := cmd()
	modalInterface, _ := modal.Update(msg)
	modal = modalInterface.(*MetadataModal)

	// Simulate user editing status field
	modal.focusedField = FieldStatus
	modal.interactionMode = ModeEditDropdown
	modal.dropdownOptions = []string{"pending", "active", "in_progress", "blocked", "completed"}
	modal.dropdownIndex = 4 // "completed"

	// Call saveField
	saveCmd := modal.saveField()
	if saveCmd == nil {
		t.Fatal("saveField returned nil cmd")
	}

	// Execute save command
	saveMsg := saveCmd()
	updatedMsg, ok := saveMsg.(metadataUpdatedMsg)
	if !ok {
		t.Fatalf("expected metadataUpdatedMsg, got %T", saveMsg)
	}

	if updatedMsg.err != nil {
		t.Fatalf("unexpected error: %v", updatedMsg.err)
	}

	// Verify all 3 tasks were updated
	if updateCount != 3 {
		t.Errorf("updateCount = %d, want 3", updateCount)
	}

	if len(updatedTasks) != 3 {
		t.Errorf("updated %d unique tasks, want 3", len(updatedTasks))
	}

	for _, taskID := range []string{"task1", "task2", "task3"} {
		if !updatedTasks[taskID] {
			t.Errorf("task %s was not updated", taskID)
		}
	}
}

// TestMetadataModalFeature_SaveField_ParallelUpdates tests that updates happen in parallel.
func TestMetadataModalFeature_SaveField_ParallelUpdates(t *testing.T) {
	requestTimes := make([]time.Time, 0)
	completionTimes := make([]time.Time, 0)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Feature endpoint
		if r.URL.Path == "/api/v1/tasks/brain-api/features/perf-test" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"feature": map[string]interface{}{
					"featureId": "perf-test",
					"tasks": []map[string]interface{}{
						{"id": "task1", "title": "Task 1"},
						{"id": "task2", "title": "Task 2"},
						{"id": "task3", "title": "Task 3"},
						{"id": "task4", "title": "Task 4"},
						{"id": "task5", "title": "Task 5"},
					},
					"ready": true,
				},
			})
			return
		}

		// Entry GET endpoints
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entry": map[string]interface{}{
					"status":            "active",
					"priority":          "medium",
					"completeOnIdle":    false,
					"openPRBeforeMerge": false,
				},
			})
			return
		}

		// Entry PATCH endpoints - simulate slow update
		if r.Method == http.MethodPatch {
			requestTimes = append(requestTimes, time.Now())
			time.Sleep(50 * time.Millisecond) // Simulate work
			completionTimes = append(completionTimes, time.Now())

			json.NewEncoder(w).Encode(map[string]interface{}{
				"entry": map[string]interface{}{
					"status": "completed",
				},
			})
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := runner.RunnerConfig{BrainAPIURL: srv.URL, APITimeout: 5000}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModalFeature("perf-test", "brain-api", apiClient)

	// Initialize
	cmd := modal.Init()
	msg := cmd()
	modalInterface, _ := modal.Update(msg)
	modal = modalInterface.(*MetadataModal)

	// Simulate editing
	modal.focusedField = FieldStatus
	modal.interactionMode = ModeEditDropdown
	modal.dropdownOptions = []string{"completed"}
	modal.dropdownIndex = 0

	// Measure time
	start := time.Now()
	saveCmd := modal.saveField()
	saveMsg := saveCmd()
	elapsed := time.Since(start)

	updatedMsg, ok := saveMsg.(metadataUpdatedMsg)
	if !ok {
		t.Fatalf("expected metadataUpdatedMsg, got %T", saveMsg)
	}

	if updatedMsg.err != nil {
		t.Fatalf("unexpected error: %v", updatedMsg.err)
	}

	// With 5 tasks × 50ms each:
	// - Sequential: 250ms+
	// - Parallel: 50-100ms
	// Allow some overhead, but it should be clearly parallel
	if elapsed > 150*time.Millisecond {
		t.Errorf("updates took %v, expected parallel execution < 150ms (sequential would be 250ms+)", elapsed)
	}

	// Check that requests started close together (within 50ms window)
	if len(requestTimes) >= 2 {
		firstRequest := requestTimes[0]
		lastRequest := requestTimes[len(requestTimes)-1]
		requestSpan := lastRequest.Sub(firstRequest)
		if requestSpan > 50*time.Millisecond {
			t.Errorf("request span = %v, want < 50ms for parallel execution", requestSpan)
		}
	}
}

// TestMetadataModalFeature_SaveField_ErrorHandling tests that any error aborts the operation.
func TestMetadataModalFeature_SaveField_ErrorHandling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Feature endpoint
		if r.URL.Path == "/api/v1/tasks/brain-api/features/error-test" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"feature": map[string]interface{}{
					"featureId": "error-test",
					"tasks": []map[string]interface{}{
						{"id": "task1", "title": "Task 1"},
						{"id": "task2", "title": "Task 2"},
						{"id": "task3", "title": "Task 3"},
					},
					"ready": true,
				},
			})
			return
		}

		// Entry GET endpoints
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entry": map[string]interface{}{
					"status":            "active",
					"completeOnIdle":    false,
					"openPRBeforeMerge": false,
				},
			})
			return
		}

		// Entry PATCH endpoints - task2 fails
		if r.Method == http.MethodPatch {
			if r.URL.Path == "/api/v1/entries/task2" {
				http.Error(w, "update failed", http.StatusInternalServerError)
				return
			}

			json.NewEncoder(w).Encode(map[string]interface{}{
				"entry": map[string]interface{}{
					"status": "completed",
				},
			})
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := runner.RunnerConfig{BrainAPIURL: srv.URL, APITimeout: 5000}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModalFeature("error-test", "brain-api", apiClient)

	// Initialize
	cmd := modal.Init()
	msg := cmd()
	modalInterface, _ := modal.Update(msg)
	modal = modalInterface.(*MetadataModal)

	// Simulate editing
	modal.focusedField = FieldStatus
	modal.interactionMode = ModeEditDropdown
	modal.dropdownOptions = []string{"completed"}
	modal.dropdownIndex = 0

	// Call saveField
	saveCmd := modal.saveField()
	saveMsg := saveCmd()

	updatedMsg, ok := saveMsg.(metadataUpdatedMsg)
	if !ok {
		t.Fatalf("expected metadataUpdatedMsg, got %T", saveMsg)
	}

	// Should have error
	if updatedMsg.err == nil {
		t.Error("expected error from failed task2 update")
	}
}

// TestMetadataModalFeature_SaveField_FeaturePriority tests feature-specific field updates.
func TestMetadataModalFeature_SaveField_FeaturePriority(t *testing.T) {
	receivedUpdates := make(map[string]map[string]interface{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Feature endpoint
		if r.URL.Path == "/api/v1/tasks/brain-api/features/priority-test" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"feature": map[string]interface{}{
					"featureId": "priority-test",
					"tasks": []map[string]interface{}{
						{"id": "task1", "title": "Task 1"},
						{"id": "task2", "title": "Task 2"},
					},
					"ready": true,
				},
			})
			return
		}

		// Entry GET endpoints
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entry": map[string]interface{}{
					"status":            "active",
					"featurePriority":   "medium",
					"completeOnIdle":    false,
					"openPRBeforeMerge": false,
				},
			})
			return
		}

		// Entry PATCH endpoints - capture update payload
		if r.Method == http.MethodPatch {
			var updates map[string]interface{}
			json.NewDecoder(r.Body).Decode(&updates)
			receivedUpdates[r.URL.Path] = updates

			json.NewEncoder(w).Encode(map[string]interface{}{
				"entry": map[string]interface{}{
					"featurePriority": updates["featurePriority"],
				},
			})
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := runner.RunnerConfig{BrainAPIURL: srv.URL, APITimeout: 5000}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModalFeature("priority-test", "brain-api", apiClient)

	// Initialize
	cmd := modal.Init()
	msg := cmd()
	modalInterface, _ := modal.Update(msg)
	modal = modalInterface.(*MetadataModal)

	// Simulate editing FieldFeaturePriority
	modal.focusedField = FieldFeaturePriority
	modal.interactionMode = ModeEditDropdown
	modal.dropdownOptions = []string{"low", "medium", "high"}
	modal.dropdownIndex = 2 // "high"

	// Call saveField
	saveCmd := modal.saveField()
	saveMsg := saveCmd()

	updatedMsg, ok := saveMsg.(metadataUpdatedMsg)
	if !ok {
		t.Fatalf("expected metadataUpdatedMsg, got %T", saveMsg)
	}

	if updatedMsg.err != nil {
		t.Fatalf("unexpected error: %v", updatedMsg.err)
	}

	// Verify both tasks received the feature_priority update
	for _, path := range []string{"/api/v1/entries/task1", "/api/v1/entries/task2"} {
		updates, ok := receivedUpdates[path]
		if !ok {
			t.Errorf("no update received for %s", path)
			continue
		}

		priority, ok := updates["feature_priority"]
		if !ok {
			t.Errorf("feature_priority not in update payload for %s", path)
			continue
		}

		if priority != "high" {
			t.Errorf("feature_priority = %v, want 'high' for %s", priority, path)
		}
	}
}

// TestMetadataModalFeature_SuccessMessage tests that success message shows task count and feature ID.
func TestMetadataModalFeature_SuccessMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Feature endpoint
		if r.URL.Path == "/api/v1/tasks/brain-api/features/message-test" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"feature": map[string]interface{}{
					"featureId": "message-test",
					"tasks": []map[string]interface{}{
						{"id": "task1", "title": "Task 1"},
						{"id": "task2", "title": "Task 2"},
						{"id": "task3", "title": "Task 3"},
					},
					"ready": true,
				},
			})
			return
		}

		// Entry GET endpoints
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entry": map[string]interface{}{
					"status":            "active",
					"completeOnIdle":    false,
					"openPRBeforeMerge": false,
				},
			})
			return
		}

		// Entry PATCH endpoints
		if r.Method == http.MethodPatch {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"entry": map[string]interface{}{
					"status": "completed",
				},
			})
			return
		}

		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := runner.RunnerConfig{BrainAPIURL: srv.URL, APITimeout: 5000}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModalFeature("message-test", "brain-api", apiClient)

	// Initialize
	cmd := modal.Init()
	msg := cmd()
	modalInterface, _ := modal.Update(msg)
	modal = modalInterface.(*MetadataModal)

	// Simulate editing and saving
	modal.focusedField = FieldStatus
	modal.interactionMode = ModeEditDropdown
	modal.dropdownOptions = []string{"completed"}
	modal.dropdownIndex = 0

	saveCmd := modal.saveField()
	saveMsg := saveCmd()

	// Process the update message
	modalInterface, _ = modal.Update(saveMsg)
	modal = modalInterface.(*MetadataModal)

	// Render view and check success message
	view := modal.View()

	// Should contain: "Updated 3 tasks in feature message-test"
	expectedMessage := "3 tasks"
	if !strings.Contains(view, expectedMessage) {
		t.Errorf("success message should contain %q, got:\n%s", expectedMessage, view)
	}

	expectedFeature := "message-test"
	if !strings.Contains(view, expectedFeature) {
		t.Errorf("success message should contain feature ID %q, got:\n%s", expectedFeature, view)
	}
}

package tui

import (
	"testing"

	"github.com/huynle/brain-api/internal/runner"
)

// TestNewMetadataModalFeature tests the feature mode constructor.
func TestNewMetadataModalFeature(t *testing.T) {
	// Create mock API client
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	// Create modal with feature ID
	featureID := "feat-auth-123"
	modal := NewMetadataModalFeature(featureID, apiClient)

	if modal == nil {
		t.Fatal("NewMetadataModalFeature returned nil")
	}

	// Check that featureID is set
	if modal.featureID != featureID {
		t.Errorf("featureID = %q, want %q", modal.featureID, featureID)
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
	modal := NewMetadataModalFeature(featureID, apiClient)

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
	modal := NewMetadataModalFeature(featureID, apiClient)

	// taskIDs should be empty initially
	expectedTitle := "Update Feature Metadata - feat-auth-123 (0 tasks)"
	actualTitle := modal.Title()

	if actualTitle != expectedTitle {
		t.Errorf("Title() = %q, want %q", actualTitle, expectedTitle)
	}
}

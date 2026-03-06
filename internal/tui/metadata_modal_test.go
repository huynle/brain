package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/huynle/brain-api/internal/runner"
)

// ============================================================================
// MetadataModal Construction Tests
// ============================================================================

func TestNewMetadataModal(t *testing.T) {
	// Create mock API client
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	// Create modal
	modal := NewMetadataModal("task123", apiClient)

	if modal == nil {
		t.Fatal("NewMetadataModal returned nil")
	}

	// Check that taskID is set
	if modal.taskID != "task123" {
		t.Errorf("taskID = %q, want %q", modal.taskID, "task123")
	}

	// Check that apiClient is set
	if modal.apiClient == nil {
		t.Error("apiClient is nil")
	}

	// Check that fieldList is initialized
	if len(modal.fieldList) == 0 {
		t.Error("fieldList is empty, expected fields to be initialized")
	}

	// Check initial mode
	if modal.interactionMode != ModeNavigate {
		t.Errorf("interactionMode = %v, want ModeNavigate", modal.interactionMode)
	}
}

// ============================================================================
// Modal Interface Tests
// ============================================================================

func TestMetadataModal_Interface(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	// Test that modal implements Modal interface
	var _ Modal = modal

	// Test Title
	if modal.Title() != "Update Metadata" {
		t.Errorf("Title() = %q, want %q", modal.Title(), "Update Metadata")
	}

	// Test Width
	if modal.Width() != 60 {
		t.Errorf("Width() = %d, want 60", modal.Width())
	}

	// Test Height
	if modal.Height() != 25 {
		t.Errorf("Height() = %d, want 25", modal.Height())
	}

	// Test Init
	cmd := modal.Init()
	if cmd != nil {
		t.Error("Init() returned non-nil cmd for stub")
	}

	// Test Update (stub should return modal and nil cmd)
	updatedModal, cmd := modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if updatedModal == nil {
		t.Error("Update() returned nil modal")
	}
	if cmd != nil {
		t.Error("Update() returned non-nil cmd for stub")
	}

	// Test View (stub should return placeholder)
	view := modal.View()
	if view == "" {
		t.Error("View() returned empty string")
	}

	// Test HandleKey - now implemented, should handle navigation keys
	handled, cmd := modal.HandleKey("j")
	if !handled {
		t.Error("HandleKey('j') should return true (handled)")
	}
	if cmd != nil {
		t.Error("HandleKey() should return nil cmd")
	}

	// Test unhandled key returns false
	handled, cmd = modal.HandleKey("x")
	if handled {
		t.Error("HandleKey('x') should return false (not handled)")
	}
	if cmd != nil {
		t.Error("HandleKey('x') should return nil cmd")
	}
}

// ============================================================================
// Field List Tests
// ============================================================================

func TestMetadataModal_FieldList(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	// Check that all 15 fields are in the list
	expectedFields := []MetadataField{
		FieldStatus,
		FieldPriority,
		FieldFeatureID,
		FieldGitBranch,
		FieldMergeTargetBranch,
		FieldMergePolicy,
		FieldMergeStrategy,
		FieldExecutionMode,
		FieldDirectPrompt,
		FieldAgent,
		FieldModel,
		FieldTargetWorkdir,
		FieldCompleteOnIdle,
		FieldOpenPRBeforeMerge,
		FieldSchedule,
	}

	if len(modal.fieldList) != len(expectedFields) {
		t.Errorf("fieldList length = %d, want %d", len(modal.fieldList), len(expectedFields))
	}

	// Verify all expected fields are present (order may vary)
	fieldSet := make(map[MetadataField]bool)
	for _, field := range modal.fieldList {
		fieldSet[field] = true
	}

	for _, expected := range expectedFields {
		if !fieldSet[expected] {
			t.Errorf("fieldList missing field: %s", expected)
		}
	}
}

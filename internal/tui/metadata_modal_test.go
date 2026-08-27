package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

// ============================================================================
// Test Helpers
// ============================================================================

// executeBatchCmd executes a tea.Cmd (which may be a Batch) and returns all resulting messages.
// This handles both single commands and tea.Batch commands transparently.
func executeBatchCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var msgs []tea.Msg
		for _, c := range batch {
			if c != nil {
				msgs = append(msgs, c())
			}
		}
		return msgs
	}
	return []tea.Msg{msg}
}

// findMsg finds a message of the target type from a slice of messages.
// Returns the message and true if found, or zero value and false otherwise.
func findMsg[T any](msgs []tea.Msg) (T, bool) {
	for _, msg := range msgs {
		if typed, ok := msg.(T); ok {
			return typed, true
		}
	}
	var zero T
	return zero, false
}

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

	// Check that taskPaths is set with single task
	if len(modal.taskPaths) != 1 {
		t.Errorf("taskPaths length = %d, want 1", len(modal.taskPaths))
	}
	if modal.taskPaths[0] != "task123" {
		t.Errorf("taskPaths[0] = %q, want %q", modal.taskPaths[0], "task123")
	}

	// Check mode is single
	if modal.mode != ModeSingle {
		t.Errorf("mode = %v, want ModeSingle", modal.mode)
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
	if cmd == nil {
		t.Error("Init() should return non-nil cmd to fetch entry")
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

	// Default tab is Task — should have 4 fields (Status, Priority, FeatureID, MoveToProject)
	expectedTaskFields := []MetadataField{
		FieldStatus,
		FieldPriority,
		FieldFeatureID,
		FieldMoveToProject,
	}

	if len(modal.fieldList) != len(expectedTaskFields) {
		t.Errorf("Task tab fieldList length = %d, want %d", len(modal.fieldList), len(expectedTaskFields))
	}

	fieldSet := make(map[MetadataField]bool)
	for _, field := range modal.fieldList {
		fieldSet[field] = true
	}
	for _, expected := range expectedTaskFields {
		if !fieldSet[expected] {
			t.Errorf("Task tab fieldList missing field: %s", expected)
		}
	}

	// Verify all 15 fields are accessible across all tabs
	allFields := make(map[MetadataField]bool)
	for _, tab := range modal.tabs {
		for _, field := range fieldsForTab(tab, modal.mode) {
			allFields[field] = true
		}
	}

	expectedAllFields := []MetadataField{
		FieldStatus, FieldPriority, FieldFeatureID, FieldMoveToProject,
		FieldGitBranch, FieldMergeTargetBranch, FieldMergePolicy,
		FieldMergeStrategy, FieldExecutionMode, FieldDirectPrompt,
		FieldAgent, FieldModel, FieldTargetWorkdir,
		FieldCompleteOnIdle, FieldOpenPRBeforeMerge, FieldSchedule,
	}

	for _, expected := range expectedAllFields {
		if !allFields[expected] {
			t.Errorf("field %s not found in any tab", expected)
		}
	}
}

// ============================================================================
// Phase 3: Text Editing Mode Tests
// ============================================================================

func TestMetadataModal_TextEditing_AppendChar(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	modal.editBuffer = "hello"
	modal.appendChar('x')

	if modal.editBuffer != "hellox" {
		t.Errorf("editBuffer = %q, want %q", modal.editBuffer, "hellox")
	}
}

func TestMetadataModal_TextEditing_DeleteChar(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	modal.editBuffer = "hello"
	modal.deleteChar()

	if modal.editBuffer != "hell" {
		t.Errorf("editBuffer = %q, want %q", modal.editBuffer, "hell")
	}

	// Delete from empty buffer should be safe
	modal.editBuffer = ""
	modal.deleteChar()
	if modal.editBuffer != "" {
		t.Errorf("deleteChar on empty buffer changed it to %q", modal.editBuffer)
	}
}

func TestMetadataModal_TextEditing_ClearBuffer(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	modal.editBuffer = "hello world"
	modal.clearBuffer()

	if modal.editBuffer != "" {
		t.Errorf("editBuffer = %q, want empty string", modal.editBuffer)
	}
}

func TestMetadataModal_handleEditTextMode(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	// Setup: focus on FeatureID field and enter edit mode
	modal.focusedField = FieldFeatureID
	modal.focusedIndex = 2
	modal.enterEditMode()
	modal.editBuffer = ""

	tests := []struct {
		name           string
		key            string
		initialBuffer  string
		expectedBuffer string
		expectHandled  bool
		expectMode     MetadataInteractionMode
	}{
		{
			name:           "Type 'a' appends character",
			key:            "a",
			initialBuffer:  "",
			expectedBuffer: "a",
			expectHandled:  true,
			expectMode:     ModeEditText,
		},
		{
			name:           "Type 'z' appends character",
			key:            "z",
			initialBuffer:  "abc",
			expectedBuffer: "abcz",
			expectHandled:  true,
			expectMode:     ModeEditText,
		},
		{
			name:           "Backspace deletes character",
			key:            "backspace",
			initialBuffer:  "hello",
			expectedBuffer: "hell",
			expectHandled:  true,
			expectMode:     ModeEditText,
		},
		{
			name:           "Ctrl+U clears buffer",
			key:            "ctrl+u",
			initialBuffer:  "hello world",
			expectedBuffer: "",
			expectHandled:  true,
			expectMode:     ModeEditText,
		},
		{
			name:           "Enter saves and exits to Navigate",
			key:            "enter",
			initialBuffer:  "new-feature",
			expectedBuffer: "", // Buffer cleared after save
			expectHandled:  true,
			expectMode:     ModeNavigate,
		},
		{
			name:           "Esc cancels and exits to Navigate",
			key:            "esc",
			initialBuffer:  "abandoned",
			expectedBuffer: "", // Buffer cleared on cancel
			expectHandled:  true,
			expectMode:     ModeNavigate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset state
			modal.interactionMode = ModeEditText
			modal.editBuffer = tt.initialBuffer
			modal.focusedField = FieldFeatureID
			delete(modal.values, FieldFeatureID) // Clear any previous value

			// Call handleEditTextMode
			handled, _ := modal.handleEditTextMode(tt.key)

			if handled != tt.expectHandled {
				t.Errorf("handleEditTextMode(%q) handled = %v, want %v", tt.key, handled, tt.expectHandled)
			}

			if modal.editBuffer != tt.expectedBuffer {
				t.Errorf("editBuffer = %q, want %q", modal.editBuffer, tt.expectedBuffer)
			}

			if modal.interactionMode != tt.expectMode {
				t.Errorf("interactionMode = %v, want %v", modal.interactionMode, tt.expectMode)
			}

			// Special check for "enter" key - verify value was saved
			if tt.key == "enter" {
				if modal.values[FieldFeatureID] != tt.initialBuffer {
					t.Errorf("values[FieldFeatureID] = %q, want %q", modal.values[FieldFeatureID], tt.initialBuffer)
				}
			}
		})
	}
}

// ============================================================================
// Phase 3: Dropdown Navigation Tests
// ============================================================================

func TestMetadataModal_moveDropdownDown(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	tests := []struct {
		name          string
		options       []string
		initialIndex  int
		expectedIndex int
	}{
		{
			name:          "Move from 0 to 1",
			options:       []string{"a", "b", "c"},
			initialIndex:  0,
			expectedIndex: 1,
		},
		{
			name:          "Move from 1 to 2",
			options:       []string{"a", "b", "c"},
			initialIndex:  1,
			expectedIndex: 2,
		},
		{
			name:          "Wrap from last to first",
			options:       []string{"a", "b", "c"},
			initialIndex:  2,
			expectedIndex: 0,
		},
		{
			name:          "Single option wraps to itself",
			options:       []string{"a"},
			initialIndex:  0,
			expectedIndex: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modal.dropdownOptions = tt.options
			modal.dropdownIndex = tt.initialIndex

			modal.moveDropdownDown()

			if modal.dropdownIndex != tt.expectedIndex {
				t.Errorf("dropdownIndex = %d, want %d", modal.dropdownIndex, tt.expectedIndex)
			}
		})
	}
}

func TestMetadataModal_moveDropdownUp(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	tests := []struct {
		name          string
		options       []string
		initialIndex  int
		expectedIndex int
	}{
		{
			name:          "Move from 2 to 1",
			options:       []string{"a", "b", "c"},
			initialIndex:  2,
			expectedIndex: 1,
		},
		{
			name:          "Move from 1 to 0",
			options:       []string{"a", "b", "c"},
			initialIndex:  1,
			expectedIndex: 0,
		},
		{
			name:          "Wrap from first to last",
			options:       []string{"a", "b", "c"},
			initialIndex:  0,
			expectedIndex: 2,
		},
		{
			name:          "Single option wraps to itself",
			options:       []string{"a"},
			initialIndex:  0,
			expectedIndex: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modal.dropdownOptions = tt.options
			modal.dropdownIndex = tt.initialIndex

			modal.moveDropdownUp()

			if modal.dropdownIndex != tt.expectedIndex {
				t.Errorf("dropdownIndex = %d, want %d", modal.dropdownIndex, tt.expectedIndex)
			}
		})
	}
}

func TestMetadataModal_handleEditDropdownMode(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	// Setup: focus on Status field and enter dropdown edit mode
	modal.focusedField = FieldStatus
	modal.focusedIndex = 0
	modal.enterEditMode()

	tests := []struct {
		name          string
		key           string
		initialIndex  int
		expectedIndex int
		expectHandled bool
		expectMode    MetadataInteractionMode
		expectSaved   bool
	}{
		{
			name:          "j moves down",
			key:           "j",
			initialIndex:  0,
			expectedIndex: 1,
			expectHandled: true,
			expectMode:    ModeEditDropdown,
			expectSaved:   false,
		},
		{
			name:          "down moves down",
			key:           "down",
			initialIndex:  0,
			expectedIndex: 1,
			expectHandled: true,
			expectMode:    ModeEditDropdown,
			expectSaved:   false,
		},
		{
			name:          "k moves up",
			key:           "k",
			initialIndex:  2,
			expectedIndex: 1,
			expectHandled: true,
			expectMode:    ModeEditDropdown,
			expectSaved:   false,
		},
		{
			name:          "up moves up",
			key:           "up",
			initialIndex:  2,
			expectedIndex: 1,
			expectHandled: true,
			expectMode:    ModeEditDropdown,
			expectSaved:   false,
		},
		{
			name:          "enter saves and exits to Navigate",
			key:           "enter",
			initialIndex:  1,
			expectedIndex: 1,
			expectHandled: true,
			expectMode:    ModeNavigate,
			expectSaved:   true,
		},
		{
			name:          "esc cancels and exits to Navigate",
			key:           "esc",
			initialIndex:  2,
			expectedIndex: 2,
			expectHandled: true,
			expectMode:    ModeNavigate,
			expectSaved:   false,
		},
		{
			name:          "other keys are consumed but ignored",
			key:           "x",
			initialIndex:  1,
			expectedIndex: 1,
			expectHandled: true,
			expectMode:    ModeEditDropdown,
			expectSaved:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset state
			modal.interactionMode = ModeEditDropdown
			modal.focusedField = FieldStatus
			modal.dropdownOptions = []string{"draft", "pending", "active", "in_progress"}
			modal.dropdownIndex = tt.initialIndex
			delete(modal.values, FieldStatus) // Clear any previous value

			// Call handleEditDropdownMode
			handled, _ := modal.handleEditDropdownMode(tt.key)

			if handled != tt.expectHandled {
				t.Errorf("handleEditDropdownMode(%q) handled = %v, want %v", tt.key, handled, tt.expectHandled)
			}

			if modal.dropdownIndex != tt.expectedIndex {
				t.Errorf("dropdownIndex = %d, want %d", modal.dropdownIndex, tt.expectedIndex)
			}

			if modal.interactionMode != tt.expectMode {
				t.Errorf("interactionMode = %v, want %v", modal.interactionMode, tt.expectMode)
			}

			// Check if value was saved when expected
			if tt.expectSaved {
				expectedValue := modal.dropdownOptions[tt.initialIndex]
				if modal.values[FieldStatus] != expectedValue {
					t.Errorf("values[FieldStatus] = %q, want %q", modal.values[FieldStatus], expectedValue)
				}
			} else if tt.key != "x" { // Don't check for "other keys" test
				if _, ok := modal.values[FieldStatus]; ok && tt.key == "esc" {
					t.Error("esc should not save value")
				}
			}
		})
	}
}

func TestMetadataModal_HandleKey_RoutesByMode(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	// Test Navigate mode
	t.Run("Navigate mode handles j key", func(t *testing.T) {
		modal.interactionMode = ModeNavigate
		modal.focusedIndex = 0

		handled, _ := modal.HandleKey("j")

		if !handled {
			t.Error("HandleKey should handle 'j' in Navigate mode")
		}
		if modal.focusedIndex != 1 {
			t.Errorf("focusedIndex = %d, want 1", modal.focusedIndex)
		}
	})

	// Test EditText mode
	t.Run("EditText mode handles character input", func(t *testing.T) {
		modal.interactionMode = ModeEditText
		modal.editBuffer = ""
		modal.focusedField = FieldFeatureID

		handled, _ := modal.HandleKey("a")

		if !handled {
			t.Error("HandleKey should handle 'a' in EditText mode")
		}
		if modal.editBuffer != "a" {
			t.Errorf("editBuffer = %q, want 'a'", modal.editBuffer)
		}
	})

	// Test EditDropdown mode
	t.Run("EditDropdown mode handles j key", func(t *testing.T) {
		modal.interactionMode = ModeEditDropdown
		modal.dropdownOptions = []string{"opt1", "opt2", "opt3"}
		modal.dropdownIndex = 0

		handled, _ := modal.HandleKey("j")

		if !handled {
			t.Error("HandleKey should handle 'j' in EditDropdown mode")
		}
		if modal.dropdownIndex != 1 {
			t.Errorf("dropdownIndex = %d, want 1", modal.dropdownIndex)
		}
	})

	// Test that j in Navigate mode doesn't affect EditText buffer
	t.Run("j in Navigate mode doesn't affect edit buffer", func(t *testing.T) {
		modal.interactionMode = ModeNavigate
		modal.editBuffer = "test"

		modal.HandleKey("j")

		if modal.editBuffer != "test" {
			t.Error("Navigate mode j should not affect editBuffer")
		}
	})
}

func TestMetadataModal_enterEditMode_InitializesDropdownIndex(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	tests := []struct {
		name          string
		field         MetadataField
		currentValue  string
		expectedIndex int
		expectedMode  MetadataInteractionMode
	}{
		{
			name:          "Status field with existing value 'active'",
			field:         FieldStatus,
			currentValue:  "active",
			expectedIndex: 2, // "active" is at index 2 in status options
			expectedMode:  ModeEditDropdown,
		},
		{
			name:          "Priority field with value 'high'",
			field:         FieldPriority,
			currentValue:  "high",
			expectedIndex: 0, // "high" is at index 0 in priority options
			expectedMode:  ModeEditDropdown,
		},
		{
			name:          "Status field with no current value defaults to 0",
			field:         FieldStatus,
			currentValue:  "",
			expectedIndex: 0,
			expectedMode:  ModeEditDropdown,
		},
		{
			name:          "Status field with unknown value defaults to 0",
			field:         FieldStatus,
			currentValue:  "nonexistent",
			expectedIndex: 0,
			expectedMode:  ModeEditDropdown,
		},
		{
			name:          "Boolean field with true value",
			field:         FieldCompleteOnIdle,
			currentValue:  "true",
			expectedIndex: 0, // "true" is at index 0 in boolean options
			expectedMode:  ModeEditDropdown,
		},
		{
			name:          "Boolean field with false value",
			field:         FieldOpenPRBeforeMerge,
			currentValue:  "false",
			expectedIndex: 1, // "false" is at index 1 in boolean options
			expectedMode:  ModeEditDropdown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			modal.focusedField = tt.field
			if tt.currentValue != "" {
				if getFieldType(tt.field) == FieldTypeBoolean {
					modal.boolValues[tt.field] = tt.currentValue == "true"
				} else {
					modal.values[tt.field] = tt.currentValue
				}
			} else {
				// Clear any existing value
				delete(modal.values, tt.field)
				delete(modal.boolValues, tt.field)
			}

			// Call enterEditMode
			modal.enterEditMode()

			// Verify mode
			if modal.interactionMode != tt.expectedMode {
				t.Errorf("interactionMode = %v, want %v", modal.interactionMode, tt.expectedMode)
			}

			// Verify dropdown index
			if modal.dropdownIndex != tt.expectedIndex {
				t.Errorf("dropdownIndex = %d, want %d (options: %v, value: %q)",
					modal.dropdownIndex, tt.expectedIndex, modal.dropdownOptions, tt.currentValue)
			}

			// Verify dropdown options were set
			if len(modal.dropdownOptions) == 0 {
				t.Error("dropdownOptions should be populated")
			}
		})
	}
}

// ============================================================================
// API Integration Tests
// ============================================================================

func TestMetadataModal_Init_FetchesEntry(t *testing.T) {
	// Create test server
	srv := createTestServer(t, map[string]interface{}{
		"id":       "abc123",
		"status":   "pending",
		"priority": "high",
		"agent":    "dev",
	})
	defer srv.Close()

	cfg := runner.RunnerConfig{BrainAPIURL: srv.URL}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/test/task/abc123.md", apiClient)

	// Init should return a command (now a batch)
	cmd := modal.Init()
	if cmd == nil {
		t.Fatal("Init() should return non-nil command to fetch entry")
	}

	// Execute the batch and find the metadataFetchedMsg
	msgs := executeBatchCmd(cmd)
	fetchedMsg, ok := findMsg[metadataFetchedMsg](msgs)
	if !ok {
		t.Fatalf("Init() batch should contain metadataFetchedMsg, got messages: %v", msgs)
	}

	// Check that entry was fetched
	if fetchedMsg.err != nil {
		t.Fatalf("fetch error: %v", fetchedMsg.err)
	}
	if len(fetchedMsg.entries) == 0 {
		t.Fatal("expected non-empty entries")
	}
	if fetchedMsg.entries[0].ID != "abc123" {
		t.Errorf("entry ID = %q, want %q", fetchedMsg.entries[0].ID, "abc123")
	}
}

func TestMetadataModal_Update_HandlesFetchSuccess(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	// Create fetched message
	fetchedMsg := metadataFetchedMsg{
		entries: []*types.BrainEntry{
			{
				ID:       "task123",
				Status:   "pending",
				Priority: "high",
				Agent:    "dev",
			},
		},
		err: nil,
	}

	// Update with fetched message
	updatedModal, cmd := modal.Update(fetchedMsg)
	if cmd != nil {
		t.Error("Update with fetched message should return nil cmd")
	}

	m, ok := updatedModal.(*MetadataModal)
	if !ok {
		t.Fatalf("Update should return *MetadataModal, got %T", updatedModal)
	}

	// Check that values were populated
	if m.values[FieldStatus] != "pending" {
		t.Errorf("status = %q, want 'pending'", m.values[FieldStatus])
	}
	if m.values[FieldPriority] != "high" {
		t.Errorf("priority = %q, want 'high'", m.values[FieldPriority])
	}
	if m.values[FieldAgent] != "dev" {
		t.Errorf("agent = %q, want 'dev'", m.values[FieldAgent])
	}
}

// ============================================================================
// metadataFetchedMsg Feature Field Population Tests
// ============================================================================

func TestMetadataModal_Update_PopulatesFeaturePriority(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	fetchedMsg := metadataFetchedMsg{
		entries: []*types.BrainEntry{
			{
				ID:              "task123",
				Status:          "pending",
				FeaturePriority: "high",
			},
		},
	}

	updatedModal, _ := modal.Update(fetchedMsg)
	m := updatedModal.(*MetadataModal)

	if m.values[FieldFeaturePriority] != "high" {
		t.Errorf("FieldFeaturePriority = %q, want 'high'", m.values[FieldFeaturePriority])
	}
}

func TestMetadataModal_Update_PopulatesFeaturePriority_Empty(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	fetchedMsg := metadataFetchedMsg{
		entries: []*types.BrainEntry{
			{
				ID:              "task123",
				Status:          "pending",
				FeaturePriority: "",
			},
		},
	}

	updatedModal, _ := modal.Update(fetchedMsg)
	m := updatedModal.(*MetadataModal)

	if m.values[FieldFeaturePriority] != "" {
		t.Errorf("FieldFeaturePriority = %q, want empty string", m.values[FieldFeaturePriority])
	}
}

func TestMetadataModal_Update_PopulatesFeatureDependsOn_CommaJoined(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	fetchedMsg := metadataFetchedMsg{
		entries: []*types.BrainEntry{
			{
				ID:               "task123",
				Status:           "pending",
				FeatureDependsOn: []string{"feat-auth", "feat-db", "feat-ui"},
			},
		},
	}

	updatedModal, _ := modal.Update(fetchedMsg)
	m := updatedModal.(*MetadataModal)

	expected := "feat-auth, feat-db, feat-ui"
	if m.values[FieldFeatureDependsOn] != expected {
		t.Errorf("FieldFeatureDependsOn = %q, want %q", m.values[FieldFeatureDependsOn], expected)
	}
}

func TestMetadataModal_Update_PopulatesFeatureDependsOn_SingleDep(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	fetchedMsg := metadataFetchedMsg{
		entries: []*types.BrainEntry{
			{
				ID:               "task123",
				Status:           "pending",
				FeatureDependsOn: []string{"feat-auth"},
			},
		},
	}

	updatedModal, _ := modal.Update(fetchedMsg)
	m := updatedModal.(*MetadataModal)

	if m.values[FieldFeatureDependsOn] != "feat-auth" {
		t.Errorf("FieldFeatureDependsOn = %q, want 'feat-auth'", m.values[FieldFeatureDependsOn])
	}
}

func TestMetadataModal_Update_PopulatesFeatureDependsOn_Empty(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	fetchedMsg := metadataFetchedMsg{
		entries: []*types.BrainEntry{
			{
				ID:               "task123",
				Status:           "pending",
				FeatureDependsOn: nil,
			},
		},
	}

	updatedModal, _ := modal.Update(fetchedMsg)
	m := updatedModal.(*MetadataModal)

	if m.values[FieldFeatureDependsOn] != "" {
		t.Errorf("FieldFeatureDependsOn = %q, want empty string for nil deps", m.values[FieldFeatureDependsOn])
	}
}

func TestMetadataModal_Update_PopulatesFeatureDependsOn_EmptySlice(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	fetchedMsg := metadataFetchedMsg{
		entries: []*types.BrainEntry{
			{
				ID:               "task123",
				Status:           "pending",
				FeatureDependsOn: []string{},
			},
		},
	}

	updatedModal, _ := modal.Update(fetchedMsg)
	m := updatedModal.(*MetadataModal)

	if m.values[FieldFeatureDependsOn] != "" {
		t.Errorf("FieldFeatureDependsOn = %q, want empty string for empty slice", m.values[FieldFeatureDependsOn])
	}
}

func TestMetadataModal_Update_PopulatesBothFeatureFields(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	fetchedMsg := metadataFetchedMsg{
		entries: []*types.BrainEntry{
			{
				ID:               "task123",
				Status:           "active",
				Priority:         "medium",
				FeaturePriority:  "high",
				FeatureDependsOn: []string{"feat-auth", "feat-db"},
				Agent:            "tdd-dev",
			},
		},
	}

	updatedModal, _ := modal.Update(fetchedMsg)
	m := updatedModal.(*MetadataModal)

	// Verify all fields populated together
	if m.values[FieldStatus] != "active" {
		t.Errorf("FieldStatus = %q, want 'active'", m.values[FieldStatus])
	}
	if m.values[FieldPriority] != "medium" {
		t.Errorf("FieldPriority = %q, want 'medium'", m.values[FieldPriority])
	}
	if m.values[FieldFeaturePriority] != "high" {
		t.Errorf("FieldFeaturePriority = %q, want 'high'", m.values[FieldFeaturePriority])
	}
	if m.values[FieldFeatureDependsOn] != "feat-auth, feat-db" {
		t.Errorf("FieldFeatureDependsOn = %q, want 'feat-auth, feat-db'", m.values[FieldFeatureDependsOn])
	}
	if m.values[FieldAgent] != "tdd-dev" {
		t.Errorf("FieldAgent = %q, want 'tdd-dev'", m.values[FieldAgent])
	}
}

func TestMetadataModal_Update_HandlesFetchError(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	// Create error message
	fetchedMsg := metadataFetchedMsg{
		entries: nil,
		err:     fmt.Errorf("network error"),
	}

	// Update with error message
	updatedModal, cmd := modal.Update(fetchedMsg)
	if cmd != nil {
		t.Error("Update with error message should return nil cmd")
	}

	m, ok := updatedModal.(*MetadataModal)
	if !ok {
		t.Fatalf("Update should return *MetadataModal, got %T", updatedModal)
	}

	// Check that error was set
	if m.fetchError == nil {
		t.Error("fetchError should be set")
	}
}

// ============================================================================
// Batch Mode Tests
// ============================================================================

func TestDetectMixedFields(t *testing.T) {
	t.Run("single entry returns no mixed fields", func(t *testing.T) {
		entries := []*types.BrainEntry{
			{Status: "pending", Priority: "high", FeatureID: "test"},
		}
		mixed := detectMixedFields(entries)
		if len(mixed) != 0 {
			t.Errorf("expected no mixed fields, got %d", len(mixed))
		}
	})

	t.Run("two entries with same values returns no mixed fields", func(t *testing.T) {
		entries := []*types.BrainEntry{
			{Status: "pending", Priority: "high", FeatureID: "test"},
			{Status: "pending", Priority: "high", FeatureID: "test"},
		}
		mixed := detectMixedFields(entries)
		if len(mixed) != 0 {
			t.Errorf("expected no mixed fields, got %d", len(mixed))
		}
	})

	t.Run("two entries with different status returns status as mixed", func(t *testing.T) {
		entries := []*types.BrainEntry{
			{Status: "pending", Priority: "high", FeatureID: "test"},
			{Status: "active", Priority: "high", FeatureID: "test"},
		}
		mixed := detectMixedFields(entries)
		if !mixed[FieldStatus] {
			t.Error("expected status to be mixed")
		}
		if mixed[FieldPriority] {
			t.Error("expected priority to not be mixed")
		}
		if mixed[FieldFeatureID] {
			t.Error("expected feature_id to not be mixed")
		}
	})

	t.Run("multiple entries with different feature IDs", func(t *testing.T) {
		entries := []*types.BrainEntry{
			{Status: "pending", FeatureID: "feature-1"},
			{Status: "pending", FeatureID: "feature-2"},
			{Status: "pending", FeatureID: "feature-3"},
		}
		mixed := detectMixedFields(entries)
		if mixed[FieldStatus] {
			t.Error("expected status to not be mixed")
		}
		if !mixed[FieldFeatureID] {
			t.Error("expected feature_id to be mixed")
		}
	})

	t.Run("mixed boolean fields", func(t *testing.T) {
		trueVal := true
		falseVal := false
		entries := []*types.BrainEntry{
			{CompleteOnIdle: &trueVal, OpenPRBeforeMerge: &falseVal},
			{CompleteOnIdle: &falseVal, OpenPRBeforeMerge: &falseVal},
		}
		mixed := detectMixedFields(entries)
		if !mixed[FieldCompleteOnIdle] {
			t.Error("expected complete_on_idle to be mixed")
		}
		if mixed[FieldOpenPRBeforeMerge] {
			t.Error("expected open_pr_before_merge to not be mixed")
		}
	})

	t.Run("mixed feature_priority field", func(t *testing.T) {
		entries := []*types.BrainEntry{
			{Status: "pending", FeaturePriority: "high"},
			{Status: "pending", FeaturePriority: "low"},
		}
		mixed := detectMixedFields(entries)
		if !mixed[FieldFeaturePriority] {
			t.Error("expected feature_priority to be mixed when values differ")
		}
	})

	t.Run("same feature_priority field not mixed", func(t *testing.T) {
		entries := []*types.BrainEntry{
			{Status: "pending", FeaturePriority: "high"},
			{Status: "pending", FeaturePriority: "high"},
		}
		mixed := detectMixedFields(entries)
		if mixed[FieldFeaturePriority] {
			t.Error("expected feature_priority to NOT be mixed when values are same")
		}
	})

	t.Run("mixed feature_depends_on field", func(t *testing.T) {
		entries := []*types.BrainEntry{
			{Status: "pending", FeatureDependsOn: []string{"feat-auth"}},
			{Status: "pending", FeatureDependsOn: []string{"feat-db"}},
		}
		mixed := detectMixedFields(entries)
		if !mixed[FieldFeatureDependsOn] {
			t.Error("expected feature_depends_on to be mixed when values differ")
		}
	})

	t.Run("same feature_depends_on field not mixed", func(t *testing.T) {
		entries := []*types.BrainEntry{
			{Status: "pending", FeatureDependsOn: []string{"feat-auth", "feat-db"}},
			{Status: "pending", FeatureDependsOn: []string{"feat-auth", "feat-db"}},
		}
		mixed := detectMixedFields(entries)
		if mixed[FieldFeatureDependsOn] {
			t.Error("expected feature_depends_on to NOT be mixed when values are same")
		}
	})
}

func TestMetadataMode(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	t.Run("single mode title", func(t *testing.T) {
		modal := NewMetadataModal("task1", apiClient)
		if modal.Title() != "Update Metadata" {
			t.Errorf("Title() = %q, want 'Update Metadata'", modal.Title())
		}
	})

	t.Run("batch mode title with count", func(t *testing.T) {
		modal := NewMetadataModalBatch([]string{"task1", "task2", "task3"}, apiClient)
		expected := "Update Metadata - 3 tasks selected"
		if modal.Title() != expected {
			t.Errorf("Title() = %q, want %q", modal.Title(), expected)
		}
	})
}

func TestAllEqual(t *testing.T) {
	t.Run("empty slice", func(t *testing.T) {
		if !allEqual([]string{}) {
			t.Error("expected empty slice to be equal")
		}
	})

	t.Run("single element", func(t *testing.T) {
		if !allEqual([]string{"a"}) {
			t.Error("expected single element to be equal")
		}
	})

	t.Run("all same strings", func(t *testing.T) {
		if !allEqual([]string{"a", "a", "a"}) {
			t.Error("expected all same strings to be equal")
		}
	})

	t.Run("different strings", func(t *testing.T) {
		if allEqual([]string{"a", "b", "a"}) {
			t.Error("expected different strings to not be equal")
		}
	})

	t.Run("all same bools", func(t *testing.T) {
		if !allEqual([]bool{true, true, true}) {
			t.Error("expected all same bools to be equal")
		}
	})

	t.Run("different bools", func(t *testing.T) {
		if allEqual([]bool{true, false, true}) {
			t.Error("expected different bools to not be equal")
		}
	})
}

// ===========================================================================
// Tab Switching Tests
// ===========================================================================

func TestMetadataModal_TabSwitching_NextTab(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	// Single mode has 3 tabs: Task, Execution, Git & Merge
	if modal.currentTab != MetaTabTask {
		t.Fatalf("initial tab = %v, want MetaTabTask", modal.currentTab)
	}

	// Tab key cycles to next tab
	modal.HandleKey("tab")
	if modal.currentTab != MetaTabExecution {
		t.Errorf("after tab: currentTab = %v, want MetaTabExecution", modal.currentTab)
	}

	modal.HandleKey("tab")
	if modal.currentTab != MetaTabGitMerge {
		t.Errorf("after 2nd tab: currentTab = %v, want MetaTabGitMerge", modal.currentTab)
	}

	// Wraps around
	modal.HandleKey("tab")
	if modal.currentTab != MetaTabTask {
		t.Errorf("after 3rd tab: currentTab = %v, want MetaTabTask (wrapped)", modal.currentTab)
	}
}

func TestMetadataModal_TabSwitching_PrevTab(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	// shift+tab cycles to previous tab (wraps)
	modal.HandleKey("shift+tab")
	if modal.currentTab != MetaTabGitMerge {
		t.Errorf("after shift+tab: currentTab = %v, want MetaTabGitMerge (wrapped)", modal.currentTab)
	}
}

func TestMetadataModal_TabSwitching_HL(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	// L (Shift+L) key goes to next tab
	modal.HandleKey("L")
	if modal.currentTab != MetaTabExecution {
		t.Errorf("after L: currentTab = %v, want MetaTabExecution", modal.currentTab)
	}

	// H (Shift+H) key goes to previous tab
	modal.HandleKey("H")
	if modal.currentTab != MetaTabTask {
		t.Errorf("after H: currentTab = %v, want MetaTabTask", modal.currentTab)
	}
}

func TestMetadataModal_TabSwitching_NumberKeys(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	// Number keys jump to specific tabs (1-indexed)
	modal.HandleKey("3")
	if modal.currentTab != MetaTabGitMerge {
		t.Errorf("after '3': currentTab = %v, want MetaTabGitMerge", modal.currentTab)
	}

	modal.HandleKey("1")
	if modal.currentTab != MetaTabTask {
		t.Errorf("after '1': currentTab = %v, want MetaTabTask", modal.currentTab)
	}

	modal.HandleKey("2")
	if modal.currentTab != MetaTabExecution {
		t.Errorf("after '2': currentTab = %v, want MetaTabExecution", modal.currentTab)
	}
}

func TestMetadataModal_TabSwitching_ResetsIndex(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	// Navigate down a few fields
	modal.HandleKey("j")
	modal.HandleKey("j")
	if modal.focusedIndex == 0 {
		t.Fatal("focusedIndex should not be 0 after navigating down")
	}

	// Switch tab — focusedIndex should reset to 0
	modal.HandleKey("tab")
	if modal.focusedIndex != 0 {
		t.Errorf("focusedIndex = %d, want 0 after tab switch", modal.focusedIndex)
	}
}

func TestMetadataModal_TabSwitching_RebuildFieldList(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	// Task tab fields
	taskFields := len(modal.fieldList)

	// Switch to Execution tab
	modal.HandleKey("tab")
	execFields := len(modal.fieldList)

	// Field counts should differ
	if taskFields == execFields {
		t.Errorf("Task tab (%d fields) and Execution tab (%d fields) should have different field counts", taskFields, execFields)
	}
}

func TestMetadataModal_HL_OnlyInNavigateMode(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	// Enter text edit mode
	modal.interactionMode = ModeEditText
	modal.editBuffer = ""

	// H/L should NOT switch tabs in edit mode — they should be treated as text input
	initialTab := modal.currentTab
	modal.HandleKey("H")
	if modal.editBuffer != "H" {
		t.Errorf("editBuffer = %q, want 'H' (should be text input in edit mode)", modal.editBuffer)
	}
	if modal.currentTab != initialTab {
		t.Error("H should not switch tabs in edit mode")
	}
}

// ===========================================================================
// Bug Fix: focusedField must be initialized to first field
// ===========================================================================

func TestMetadataModal_FocusedFieldInitialized(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModal("task123", apiClient)

	// focusedField should be initialized to the first field (Status)
	if modal.focusedField != FieldStatus {
		t.Errorf("focusedField = %q, want %q (first field in list)", modal.focusedField, FieldStatus)
	}
}

func TestMetadataModal_EnterOnStatusOpensDropdown(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModal("task123", apiClient)

	// Without navigating, press Enter on the first field (Status)
	// Status is a dropdown field, so it should enter ModeEditDropdown
	modal.enterEditMode()

	if modal.interactionMode != ModeEditDropdown {
		t.Errorf("interactionMode = %d, want %d (ModeEditDropdown). Status field should open dropdown, not text input",
			modal.interactionMode, ModeEditDropdown)
	}
}

func TestMetadataModalBatch_FocusedFieldInitialized(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModalBatch([]string{"task1", "task2"}, apiClient)

	if modal.focusedField != FieldStatus {
		t.Errorf("focusedField = %q, want %q (first field in list)", modal.focusedField, FieldStatus)
	}
}

// ===========================================================================
// Bug Fix: Esc in edit mode should return to navigate, not close modal
// ===========================================================================

func TestModalManager_EscInEditMode_ReturnsToNavigate(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModal("task123", apiClient)
	// Simulate being in dropdown edit mode (e.g., editing Status)
	modal.interactionMode = ModeEditDropdown
	modal.focusedField = FieldStatus

	mgr := NewModalManager()
	mgr.Open(modal)

	// Press Esc while in dropdown edit mode
	handled, _ := mgr.HandleKey("esc")

	// Key should be handled
	if !handled {
		t.Error("expected Esc to be handled")
	}

	// Modal should still be open (Esc should return to navigate mode, not close)
	if !mgr.IsOpen() {
		t.Error("expected modal to remain open after Esc in edit mode - should return to navigate mode, not close")
	}

	// Modal should be back in navigate mode
	if modal.interactionMode != ModeNavigate {
		t.Errorf("interactionMode = %d, want %d (ModeNavigate)", modal.interactionMode, ModeNavigate)
	}
}

func TestModalManager_EscInTextEditMode_ReturnsToNavigate(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModal("task123", apiClient)
	// Simulate being in text edit mode (e.g., editing Git Branch)
	modal.interactionMode = ModeEditText
	modal.focusedField = FieldGitBranch
	modal.editBuffer = "some-branch"

	mgr := NewModalManager()
	mgr.Open(modal)

	// Press Esc while in text edit mode
	handled, _ := mgr.HandleKey("esc")

	if !handled {
		t.Error("expected Esc to be handled")
	}

	// Modal should still be open
	if !mgr.IsOpen() {
		t.Error("expected modal to remain open after Esc in text edit mode")
	}

	// Should be back in navigate mode
	if modal.interactionMode != ModeNavigate {
		t.Errorf("interactionMode = %d, want %d (ModeNavigate)", modal.interactionMode, ModeNavigate)
	}
}

func TestModalManager_EscInNavigateMode_ClosesModal(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)

	modal := NewMetadataModal("task123", apiClient)
	// In navigate mode (default)
	modal.interactionMode = ModeNavigate

	mgr := NewModalManager()
	mgr.Open(modal)

	// Press Esc in navigate mode
	handled, _ := mgr.HandleKey("esc")

	if !handled {
		t.Error("expected Esc to be handled")
	}

	// Modal should be closed
	if mgr.IsOpen() {
		t.Error("expected modal to close after Esc in navigate mode")
	}
}

// ===========================================================================
// Mouse Support Tests
// ===========================================================================

func TestMetadataModal_HandleMouse_TabClick(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)
	modal.loading = false

	// Single mode tabs: " Task " (pos 1-7), "  " gap, " Execution " (pos 9-20), "  " gap, " Git & Merge " (pos 22-35)
	// Start on Task tab
	if modal.currentTab != MetaTabTask {
		t.Fatalf("expected initial tab MetaTabTask, got %v", modal.currentTab)
	}

	// Click on " Execution " (x=10, y=0)
	handled, _ := modal.HandleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}, 10, 0)
	if !handled {
		t.Error("expected tab click to be handled")
	}
	if modal.currentTab != MetaTabExecution {
		t.Errorf("after clicking Execution tab: currentTab = %v, want MetaTabExecution", modal.currentTab)
	}

	// Click on " Task " (x=3, y=0)
	handled, _ = modal.HandleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}, 3, 0)
	if !handled {
		t.Error("expected tab click to be handled")
	}
	if modal.currentTab != MetaTabTask {
		t.Errorf("after clicking Task tab: currentTab = %v, want MetaTabTask", modal.currentTab)
	}
}

func TestMetadataModal_HandleMouse_FieldClick(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)
	modal.loading = false

	// Task tab fields: Status (index 0), Priority (index 1), Feature ID (index 2)
	// Field rows start at y=2 (tab header line 0, blank line 1)

	// Click on field row 1 (Priority, y=3)
	handled, _ := modal.HandleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}, 5, 3)
	if !handled {
		t.Error("expected field click to be handled")
	}
	if modal.focusedIndex != 1 {
		t.Errorf("focusedIndex = %d, want 1 (Priority)", modal.focusedIndex)
	}
	if modal.focusedField != FieldPriority {
		t.Errorf("focusedField = %v, want FieldPriority", modal.focusedField)
	}

	// Click on field row 2 (Feature ID, y=4)
	handled, _ = modal.HandleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}, 5, 4)
	if !handled {
		t.Error("expected field click to be handled")
	}
	if modal.focusedIndex != 2 {
		t.Errorf("focusedIndex = %d, want 2 (Feature ID)", modal.focusedIndex)
	}
}

func TestMetadataModal_HandleMouse_FocusedFieldClickEntersEditMode(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)
	modal.loading = false

	// Click the already-focused Status field row.
	handled, cmd := modal.HandleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}, 5, 2)
	if !handled {
		t.Fatal("expected focused field click to be handled")
	}
	if cmd != nil {
		t.Fatalf("expected status dropdown to open synchronously, got command")
	}
	if modal.interactionMode != ModeEditDropdown {
		t.Fatalf("expected click on focused dropdown field to enter edit mode, got %v", modal.interactionMode)
	}
}

func TestMetadataModal_HandleMouse_DropdownOptionClickSelectsOption(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)
	modal.loading = false
	modal.focusedField = FieldStatus
	modal.focusedIndex = 0
	modal.enterEditMode()

	// Dropdown rows start after the focused field line and blank separator.
	handled, _ := modal.HandleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}, 5, 5)
	if !handled {
		t.Fatal("expected dropdown option click to be handled")
	}
	if modal.dropdownIndex != 1 {
		t.Fatalf("expected dropdownIndex 1 after clicking second option, got %d", modal.dropdownIndex)
	}
}

func TestMetadataModal_HandleMouse_NotInEditMode(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)
	modal.loading = false
	modal.interactionMode = ModeEditText

	// Mouse clicks should not be handled during edit mode
	handled, _ := modal.HandleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}, 3, 0)
	if handled {
		t.Error("mouse clicks should not be handled during edit mode")
	}
}

func TestMetadataModal_HandleMouse_OutOfBounds(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)
	modal.loading = false

	// Click way below all fields (y=100)
	handled, _ := modal.HandleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}, 5, 100)
	if handled {
		t.Error("out-of-bounds click should not be handled")
	}
}

func TestMetadataModalFeature_HandleMouse_TabClick(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModalFeature("feat-1", "proj", apiClient)
	modal.loading = false

	// Feature mode tabs: " Feature " (pos 1-10), "  " gap, " Task " (pos 12-18), ...
	// Start on Feature tab
	if modal.currentTab != MetaTabFeature {
		t.Fatalf("expected initial tab MetaTabFeature, got %v", modal.currentTab)
	}

	// Click on " Task " area (x=14, y=0)
	handled, _ := modal.HandleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}, 14, 0)
	if !handled {
		t.Error("expected tab click to be handled")
	}
	if modal.currentTab != MetaTabTask {
		t.Errorf("after clicking Task tab: currentTab = %v, want MetaTabTask", modal.currentTab)
	}
}

// ===========================================================================
// Tab Truncation Tests
// ===========================================================================

func TestMetadataModal_VisibleTabRange_AllFit(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)
	// Single mode has 3 tabs: Task (6) + Execution (11) + Git & Merge (13)
	// Total: 1 + 8 + 2 + 13 + 2 + 15 = 41, well within 60
	lo, hi := modal.visibleTabRange()
	if lo != 0 || hi != 2 {
		t.Errorf("expected all tabs visible (0,2), got (%d,%d)", lo, hi)
	}
}

func TestMetadataModal_VisibleTabRange_Truncated(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModalFeature("feat-1", "proj", apiClient)
	modal.loading = false

	// Force a narrow width to trigger truncation
	modal.width = 30

	// Active tab is Feature (index 0) — it should be visible
	lo, hi := modal.visibleTabRange()
	if lo != 0 {
		t.Errorf("expected lo=0 (active tab visible), got %d", lo)
	}
	if hi >= len(modal.tabs)-1 {
		t.Errorf("expected truncation (hi < %d), got hi=%d", len(modal.tabs)-1, hi)
	}
}

func TestMetadataModal_VisibleTabRange_LastTab(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModalFeature("feat-1", "proj", apiClient)
	modal.loading = false
	modal.width = 30

	// Switch to Automations (last tab)
	modal.switchToTab(MetaTabAutomations)
	lo, hi := modal.visibleTabRange()
	if hi != len(modal.tabs)-1 {
		t.Errorf("expected hi=%d (Automations visible), got %d", len(modal.tabs)-1, hi)
	}
	if lo <= 0 {
		// With narrow width, first tabs should be hidden
		// lo > 0 means left overflow
		t.Logf("lo=%d (some left tabs hidden)", lo)
	}
}

func TestMetadataModal_RenderTabHeader_NoWrap(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModalFeature("feat-1", "proj", apiClient)
	modal.loading = false
	modal.width = 30

	header := modal.renderTabHeader()
	// The header should not contain a newline — truncation prevents wrapping
	if strings.Contains(header, "\n") {
		t.Error("tab header should not contain newlines (should truncate instead of wrap)")
	}
}

// ===========================================================================
// Test Helper
// ===========================================================================

func createTestServer(t *testing.T, entryData map[string]interface{}) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Brain API returns entries as flat top-level JSON objects (not wrapped)
		json.NewEncoder(w).Encode(entryData)
	}))
}

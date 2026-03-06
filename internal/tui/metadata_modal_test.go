package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
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

	// Init should return a command
	cmd := modal.Init()
	if cmd == nil {
		t.Fatal("Init() should return non-nil command to fetch entry")
	}

	// Execute the command
	msg := cmd()
	fetchedMsg, ok := msg.(metadataFetchedMsg)
	if !ok {
		t.Fatalf("Init() command should return metadataFetchedMsg, got %T", msg)
	}

	// Check that entry was fetched
	if fetchedMsg.err != nil {
		t.Fatalf("fetch error: %v", fetchedMsg.err)
	}
	if fetchedMsg.entry == nil {
		t.Fatal("expected non-nil entry")
	}
	if fetchedMsg.entry.ID != "abc123" {
		t.Errorf("entry ID = %q, want %q", fetchedMsg.entry.ID, "abc123")
	}
}

func TestMetadataModal_Update_HandlesFetchSuccess(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	// Create fetched message
	fetchedMsg := metadataFetchedMsg{
		entry: &types.BrainEntry{
			ID:       "task123",
			Status:   "pending",
			Priority: "high",
			Agent:    "dev",
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

func TestMetadataModal_Update_HandlesFetchError(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("task123", apiClient)

	// Create error message
	fetchedMsg := metadataFetchedMsg{
		entry: nil,
		err:   fmt.Errorf("network error"),
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

// ===========================================================================
// Test Helper
// ===========================================================================

func createTestServer(t *testing.T, entryData map[string]interface{}) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{
			"entry": entryData,
		}
		json.NewEncoder(w).Encode(response)
	}))
}

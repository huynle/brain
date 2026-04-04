package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/runner"
)

// ============================================================================
// Multi-Select Filter Dropdown — Interaction Tests
// ============================================================================

// newMultiFilterTestModal creates a MetadataModal pre-configured for multi-select
// filter dropdown testing with the given options and pre-selected items.
func newMultiFilterTestModal(options []string, preSelected map[string]bool) *MetadataModal {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/test1.md", apiClient)

	modal.interactionMode = ModeEditMultiFilterDropdown
	modal.focusedField = FieldFeatureDependsOn
	modal.dropdownOptions = options
	modal.filteredOptions = make([]string, len(options))
	copy(modal.filteredOptions, options)
	modal.dropdownIndex = 0
	modal.editBuffer = ""

	if preSelected != nil {
		modal.selectedItems = preSelected
	} else {
		modal.selectedItems = make(map[string]bool)
	}

	return modal
}

// ============================================================================
// Navigation Tests (j/k/up/down)
// ============================================================================

func TestMultiFilterDropdown_NavigateDown(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth", "feat-db", "feat-ui"}, nil)

	// Start at index 0
	if modal.dropdownIndex != 0 {
		t.Fatalf("initial dropdownIndex = %d, want 0", modal.dropdownIndex)
	}

	// Press j to move down
	handled, _ := modal.handleEditMultiFilterDropdownMode("j")
	if !handled {
		t.Error("j key should be handled")
	}
	if modal.dropdownIndex != 1 {
		t.Errorf("after j, dropdownIndex = %d, want 1", modal.dropdownIndex)
	}

	// Press down to move down again
	handled, _ = modal.handleEditMultiFilterDropdownMode("down")
	if !handled {
		t.Error("down key should be handled")
	}
	if modal.dropdownIndex != 2 {
		t.Errorf("after down, dropdownIndex = %d, want 2", modal.dropdownIndex)
	}
}

func TestMultiFilterDropdown_NavigateUp(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth", "feat-db", "feat-ui"}, nil)
	modal.dropdownIndex = 2 // Start at last item

	// Press k to move up
	handled, _ := modal.handleEditMultiFilterDropdownMode("k")
	if !handled {
		t.Error("k key should be handled")
	}
	if modal.dropdownIndex != 1 {
		t.Errorf("after k, dropdownIndex = %d, want 1", modal.dropdownIndex)
	}

	// Press up to move up again
	handled, _ = modal.handleEditMultiFilterDropdownMode("up")
	if !handled {
		t.Error("up key should be handled")
	}
	if modal.dropdownIndex != 0 {
		t.Errorf("after up, dropdownIndex = %d, want 0", modal.dropdownIndex)
	}
}

func TestMultiFilterDropdown_NavigateWrapsDown(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth", "feat-db", "feat-ui"}, nil)
	modal.dropdownIndex = 2 // Last item

	// Press j to wrap around to top
	modal.handleEditMultiFilterDropdownMode("j")
	if modal.dropdownIndex != 0 {
		t.Errorf("expected wrap to 0, got %d", modal.dropdownIndex)
	}
}

func TestMultiFilterDropdown_NavigateWrapsUp(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth", "feat-db", "feat-ui"}, nil)
	modal.dropdownIndex = 0 // First item

	// Press k to wrap around to bottom
	modal.handleEditMultiFilterDropdownMode("k")
	if modal.dropdownIndex != 2 {
		t.Errorf("expected wrap to 2, got %d", modal.dropdownIndex)
	}
}

func TestMultiFilterDropdown_NavigateEmptyList(t *testing.T) {
	modal := newMultiFilterTestModal([]string{}, nil)

	// Navigate on empty list should not panic
	handled, _ := modal.handleEditMultiFilterDropdownMode("j")
	if !handled {
		t.Error("j key should be handled even with empty list")
	}

	handled, _ = modal.handleEditMultiFilterDropdownMode("k")
	if !handled {
		t.Error("k key should be handled even with empty list")
	}
}

// ============================================================================
// Toggle Selection Tests (Space)
// ============================================================================

func TestMultiFilterDropdown_SpaceTogglesSelection(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth", "feat-db", "feat-ui"}, nil)
	modal.dropdownIndex = 0

	// Toggle on
	handled, _ := modal.handleEditMultiFilterDropdownMode(" ")
	if !handled {
		t.Error("space key should be handled")
	}
	if !modal.selectedItems["feat-auth"] {
		t.Error("feat-auth should be selected after space toggle")
	}

	// Toggle off
	modal.handleEditMultiFilterDropdownMode(" ")
	if modal.selectedItems["feat-auth"] {
		t.Error("feat-auth should be deselected after second space toggle")
	}
}

func TestMultiFilterDropdown_SpaceTogglesMultipleItems(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth", "feat-db", "feat-ui"}, nil)

	// Select first item
	modal.dropdownIndex = 0
	modal.handleEditMultiFilterDropdownMode(" ")

	// Navigate and select third item
	modal.dropdownIndex = 2
	modal.handleEditMultiFilterDropdownMode(" ")

	if !modal.selectedItems["feat-auth"] {
		t.Error("feat-auth should be selected")
	}
	if modal.selectedItems["feat-db"] {
		t.Error("feat-db should NOT be selected")
	}
	if !modal.selectedItems["feat-ui"] {
		t.Error("feat-ui should be selected")
	}
}

func TestMultiFilterDropdown_SpaceOnEmptyList(t *testing.T) {
	modal := newMultiFilterTestModal([]string{}, nil)

	// Space on empty list should not panic
	handled, _ := modal.handleEditMultiFilterDropdownMode(" ")
	if !handled {
		t.Error("space key should be handled even with empty list")
	}
}

// ============================================================================
// Enter (Confirm) Tests
// ============================================================================

func TestMultiFilterDropdown_EnterConfirmsSelections(t *testing.T) {
	modal := newMultiFilterTestModal(
		[]string{"feat-auth", "feat-db", "feat-ui"},
		map[string]bool{"feat-auth": true, "feat-ui": true},
	)
	// Need task paths for saveField to work
	modal.taskPaths = []string{"projects/brain-api/task/test1.md"}

	handled, _ := modal.handleEditMultiFilterDropdownMode("enter")
	if !handled {
		t.Error("enter key should be handled")
	}

	// After enter, should return to navigate mode
	if modal.interactionMode != ModeNavigate {
		t.Errorf("after enter, interactionMode = %v, want ModeNavigate", modal.interactionMode)
	}

	// Values should be joined into comma-separated string
	value := modal.values[FieldFeatureDependsOn]
	if !strings.Contains(value, "feat-auth") {
		t.Errorf("expected value to contain feat-auth, got %q", value)
	}
	if !strings.Contains(value, "feat-ui") {
		t.Errorf("expected value to contain feat-ui, got %q", value)
	}
	// feat-db should NOT be in the value
	if strings.Contains(value, "feat-db") {
		t.Errorf("expected value to NOT contain feat-db, got %q", value)
	}
}

func TestMultiFilterDropdown_EnterWithNoSelections(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth", "feat-db"}, nil)
	modal.taskPaths = []string{"projects/brain-api/task/test1.md"}

	modal.handleEditMultiFilterDropdownMode("enter")

	// Should produce empty value
	value := modal.values[FieldFeatureDependsOn]
	if value != "" {
		t.Errorf("expected empty value with no selections, got %q", value)
	}

	if modal.interactionMode != ModeNavigate {
		t.Errorf("after enter, interactionMode = %v, want ModeNavigate", modal.interactionMode)
	}
}

func TestMultiFilterDropdown_EnterPreservesOriginalOrder(t *testing.T) {
	options := []string{"feat-auth", "feat-db", "feat-ui", "feat-payments"}
	modal := newMultiFilterTestModal(
		options,
		map[string]bool{"feat-ui": true, "feat-auth": true, "feat-payments": true},
	)
	modal.taskPaths = []string{"projects/brain-api/task/test1.md"}

	modal.handleEditMultiFilterDropdownMode("enter")

	value := modal.values[FieldFeatureDependsOn]
	// Order should follow dropdownOptions order: feat-auth, feat-ui, feat-payments
	parts := strings.Split(value, ", ")
	if len(parts) != 3 {
		t.Fatalf("expected 3 items, got %d: %q", len(parts), value)
	}
	if parts[0] != "feat-auth" {
		t.Errorf("first item = %q, want feat-auth", parts[0])
	}
	if parts[1] != "feat-ui" {
		t.Errorf("second item = %q, want feat-ui", parts[1])
	}
	if parts[2] != "feat-payments" {
		t.Errorf("third item = %q, want feat-payments", parts[2])
	}
}

func TestMultiFilterDropdown_EnterClearsEditBuffer(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth"}, nil)
	modal.taskPaths = []string{"projects/brain-api/task/test1.md"}
	modal.editBuffer = "auth"

	modal.handleEditMultiFilterDropdownMode("enter")

	if modal.editBuffer != "" {
		t.Errorf("editBuffer should be empty after enter, got %q", modal.editBuffer)
	}
}

// ============================================================================
// Escape (Cancel) Tests
// ============================================================================

func TestMultiFilterDropdown_EscCancelsWithoutSaving(t *testing.T) {
	modal := newMultiFilterTestModal(
		[]string{"feat-auth", "feat-db"},
		map[string]bool{"feat-auth": true},
	)
	// Set an original value
	modal.values[FieldFeatureDependsOn] = "original-value"

	handled, cmd := modal.handleEditMultiFilterDropdownMode("esc")
	if !handled {
		t.Error("esc key should be handled")
	}
	if cmd != nil {
		t.Error("esc should not return a command (no save)")
	}
	if modal.interactionMode != ModeNavigate {
		t.Errorf("after esc, interactionMode = %v, want ModeNavigate", modal.interactionMode)
	}
	// Original value should be preserved
	if modal.values[FieldFeatureDependsOn] != "original-value" {
		t.Errorf("value should be unchanged after esc, got %q", modal.values[FieldFeatureDependsOn])
	}
}

func TestMultiFilterDropdown_EscClearsEditBuffer(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth"}, nil)
	modal.editBuffer = "some filter text"

	modal.handleEditMultiFilterDropdownMode("esc")

	if modal.editBuffer != "" {
		t.Errorf("editBuffer should be empty after esc, got %q", modal.editBuffer)
	}
}

// ============================================================================
// Type-to-Filter Tests
// ============================================================================

func TestMultiFilterDropdown_TypeToFilter(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth", "feat-db", "payment-flow"}, nil)

	// Type 'f' to filter
	handled, _ := modal.handleEditMultiFilterDropdownMode("f")
	if !handled {
		t.Error("typing should be handled")
	}
	if modal.editBuffer != "f" {
		t.Errorf("editBuffer = %q, want 'f'", modal.editBuffer)
	}

	// Type 'e' to narrow
	modal.handleEditMultiFilterDropdownMode("e")
	if modal.editBuffer != "fe" {
		t.Errorf("editBuffer = %q, want 'fe'", modal.editBuffer)
	}

	// Filtered options should only include items matching "fe"
	if len(modal.filteredOptions) != 2 {
		t.Errorf("expected 2 filtered options for 'fe', got %d: %v", len(modal.filteredOptions), modal.filteredOptions)
	}
}

func TestMultiFilterDropdown_BackspaceRemovesFilterChar(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth", "feat-db", "payment-flow"}, nil)
	modal.editBuffer = "fea"
	modal.filterFeatures()

	modal.handleEditMultiFilterDropdownMode("backspace")
	if modal.editBuffer != "fe" {
		t.Errorf("editBuffer = %q, want 'fe'", modal.editBuffer)
	}

	// Dropdown index should reset to 0 on backspace
	if modal.dropdownIndex != 0 {
		t.Errorf("dropdownIndex should reset to 0, got %d", modal.dropdownIndex)
	}
}

func TestMultiFilterDropdown_CtrlUClearsFilter(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth", "feat-db", "payment-flow"}, nil)
	modal.editBuffer = "payment"
	modal.filterFeatures()

	modal.handleEditMultiFilterDropdownMode("ctrl+u")
	if modal.editBuffer != "" {
		t.Errorf("editBuffer should be empty after ctrl+u, got %q", modal.editBuffer)
	}

	// All options should be visible again
	if len(modal.filteredOptions) != 3 {
		t.Errorf("expected 3 options after clearing filter, got %d", len(modal.filteredOptions))
	}
}

func TestMultiFilterDropdown_FilterResetsDropdownIndex(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth", "feat-db", "payment-flow"}, nil)
	modal.dropdownIndex = 2

	// Typing should reset index to 0
	modal.handleEditMultiFilterDropdownMode("p")
	if modal.dropdownIndex != 0 {
		t.Errorf("dropdownIndex should reset to 0 when typing, got %d", modal.dropdownIndex)
	}
}

// ============================================================================
// filterFeatures Function Tests
// ============================================================================

func TestFilterFeatures_EmptyBuffer(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth", "feat-db", "payment-flow"}, nil)
	modal.editBuffer = ""
	modal.filterFeatures()

	if len(modal.filteredOptions) != 3 {
		t.Errorf("empty filter should show all options, got %d", len(modal.filteredOptions))
	}
}

func TestFilterFeatures_CaseInsensitive(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"Feat-Auth", "feat-db", "Payment-Flow"}, nil)
	modal.editBuffer = "feat"
	modal.filterFeatures()

	if len(modal.filteredOptions) != 2 {
		t.Errorf("expected 2 matching options for 'feat', got %d: %v", len(modal.filteredOptions), modal.filteredOptions)
	}
}

func TestFilterFeatures_NoMatch(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth", "feat-db", "payment-flow"}, nil)
	modal.editBuffer = "xyz"
	modal.filterFeatures()

	if len(modal.filteredOptions) != 0 {
		t.Errorf("expected 0 matching options for 'xyz', got %d", len(modal.filteredOptions))
	}
}

func TestFilterFeatures_SubstringMatch(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth", "feat-db", "payment-flow"}, nil)
	modal.editBuffer = "auth"
	modal.filterFeatures()

	if len(modal.filteredOptions) != 1 {
		t.Errorf("expected 1 matching option for 'auth', got %d", len(modal.filteredOptions))
	}
	if modal.filteredOptions[0] != "feat-auth" {
		t.Errorf("expected feat-auth, got %q", modal.filteredOptions[0])
	}
}

// ============================================================================
// Pre-Selected Items Tests
// ============================================================================

func TestMultiFilterDropdown_PreSelectedItemsShown(t *testing.T) {
	preSelected := map[string]bool{"feat-auth": true, "feat-ui": true}
	modal := newMultiFilterTestModal(
		[]string{"feat-auth", "feat-db", "feat-ui"},
		preSelected,
	)

	if !modal.selectedItems["feat-auth"] {
		t.Error("feat-auth should be pre-selected")
	}
	if modal.selectedItems["feat-db"] {
		t.Error("feat-db should NOT be pre-selected")
	}
	if !modal.selectedItems["feat-ui"] {
		t.Error("feat-ui should be pre-selected")
	}
}

func TestMultiFilterDropdown_PreSelectedCanBeToggled(t *testing.T) {
	preSelected := map[string]bool{"feat-auth": true}
	modal := newMultiFilterTestModal([]string{"feat-auth", "feat-db"}, preSelected)
	modal.dropdownIndex = 0

	// Toggle off pre-selected item
	modal.handleEditMultiFilterDropdownMode(" ")
	if modal.selectedItems["feat-auth"] {
		t.Error("feat-auth should be deselected after toggle")
	}

	// Toggle it back on
	modal.handleEditMultiFilterDropdownMode(" ")
	if !modal.selectedItems["feat-auth"] {
		t.Error("feat-auth should be selected again after second toggle")
	}
}

// ============================================================================
// Rendering Tests
// ============================================================================

func TestRenderMultiFilterDropdown_ShowsCheckmarks(t *testing.T) {
	modal := newMultiFilterTestModal(
		[]string{"feat-auth", "feat-db", "feat-ui"},
		map[string]bool{"feat-auth": true, "feat-ui": true},
	)

	rendered := modal.renderMultiFilterDropdown()

	// Should contain [x] for selected items
	if !strings.Contains(rendered, "[x] feat-auth") {
		t.Errorf("expected [x] feat-auth in rendered output, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "[x] feat-ui") {
		t.Errorf("expected [x] feat-ui in rendered output, got:\n%s", rendered)
	}

	// Should contain [ ] for unselected items
	if !strings.Contains(rendered, "[ ] feat-db") {
		t.Errorf("expected [ ] feat-db in rendered output, got:\n%s", rendered)
	}
}

func TestRenderMultiFilterDropdown_ShowsFocusIndicator(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth", "feat-db"}, nil)
	modal.dropdownIndex = 1

	rendered := modal.renderMultiFilterDropdown()

	// The focused item (index 1) should have > cursor
	if !strings.Contains(rendered, "> [ ] feat-db") {
		t.Errorf("expected > cursor on feat-db, got:\n%s", rendered)
	}
}

func TestRenderMultiFilterDropdown_ShowsSelectedCount(t *testing.T) {
	modal := newMultiFilterTestModal(
		[]string{"feat-auth", "feat-db", "feat-ui"},
		map[string]bool{"feat-auth": true, "feat-ui": true},
	)

	rendered := modal.renderMultiFilterDropdown()

	if !strings.Contains(rendered, "2 selected") {
		t.Errorf("expected '2 selected' in rendered output, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "3 of 3 features") {
		t.Errorf("expected '3 of 3 features' count, got:\n%s", rendered)
	}
}

func TestRenderMultiFilterDropdown_ShowsFilteredCount(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth", "feat-db", "payment-flow"}, nil)
	modal.editBuffer = "feat"
	modal.filterFeatures()

	rendered := modal.renderMultiFilterDropdown()

	if !strings.Contains(rendered, "2 of 3 features") {
		t.Errorf("expected '2 of 3 features' in rendered output, got:\n%s", rendered)
	}
}

func TestRenderMultiFilterDropdown_EmptyShowsMessage(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth", "feat-db"}, nil)
	modal.editBuffer = "xyz"
	modal.filterFeatures()

	rendered := modal.renderMultiFilterDropdown()

	if !strings.Contains(rendered, "no matching features") {
		t.Errorf("expected 'no matching features' message, got:\n%s", rendered)
	}
}

// ============================================================================
// View Integration Tests
// ============================================================================

func TestMultiFilterDropdown_ViewShowsHelpText(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth"}, nil)
	// Need to set up minimal state for View()
	modal.fieldList = []MetadataField{FieldFeatureDependsOn}
	modal.focusedField = FieldFeatureDependsOn
	modal.focusedIndex = 0
	modal.tabs = []MetadataTab{MetaTabFeature}
	modal.currentTab = MetaTabFeature

	view := modal.View()

	if !strings.Contains(view, "type to filter") {
		t.Errorf("expected 'type to filter' in help text, got:\n%s", view)
	}
	if !strings.Contains(view, "space: toggle") {
		t.Errorf("expected 'space: toggle' in help text, got:\n%s", view)
	}
}

func TestMultiFilterDropdown_ViewShowsFilterInput(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth", "feat-db"}, nil)
	modal.fieldList = []MetadataField{FieldFeatureDependsOn}
	modal.focusedField = FieldFeatureDependsOn
	modal.focusedIndex = 0
	modal.editBuffer = "auth"
	modal.tabs = []MetadataTab{MetaTabFeature}
	modal.currentTab = MetaTabFeature

	view := modal.View()

	// Should show the filter input with the current buffer
	if !strings.Contains(view, "auth") {
		t.Errorf("expected filter buffer 'auth' in view, got:\n%s", view)
	}
}

// ============================================================================
// FieldType Tests for MultiFilterDropdown
// ============================================================================

func TestFieldFeatureDependsOn_Type(t *testing.T) {
	fieldType := getFieldType(FieldFeatureDependsOn)
	if fieldType != FieldTypeMultiFilterDropdown {
		t.Errorf("getFieldType(FieldFeatureDependsOn) = %v, want FieldTypeMultiFilterDropdown", fieldType)
	}
}

func TestFieldFeatureDependsOn_Label(t *testing.T) {
	label := getFieldLabel(FieldFeatureDependsOn)
	if label != "Feature Dependencies" {
		t.Errorf("getFieldLabel(FieldFeatureDependsOn) = %q, want 'Feature Dependencies'", label)
	}
}

// ============================================================================
// HandleKey Dispatch Test
// ============================================================================

func TestHandleKey_DispatchesToMultiFilterDropdown(t *testing.T) {
	modal := newMultiFilterTestModal([]string{"feat-auth"}, nil)

	// HandleKey should dispatch to multi-filter dropdown handler
	handled, _ := modal.HandleKey("j")
	if !handled {
		t.Error("HandleKey should dispatch to multi-filter dropdown handler for j")
	}

	handled, _ = modal.HandleKey(" ")
	if !handled {
		t.Error("HandleKey should dispatch to multi-filter dropdown handler for space")
	}

	handled, _ = modal.HandleKey("esc")
	if !handled {
		t.Error("HandleKey should dispatch to multi-filter dropdown handler for esc")
	}
}

// ============================================================================
// featureListFetchedMsg Pre-Selection Tests
// ============================================================================

func TestFeatureListFetched_PreSelectsCurrentDeps(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/test1.md", apiClient)

	// Set current feature_depends_on value
	modal.values[FieldFeatureDependsOn] = "feat-auth, feat-db"
	modal.focusedField = FieldFeatureDependsOn

	// Simulate receiving the feature list
	msg := featureListFetchedMsg{
		featureIDs: []string{"feat-auth", "feat-db", "feat-ui", "feat-payments"},
	}

	modal.Update(msg)

	// Should be in multi-filter dropdown mode
	if modal.interactionMode != ModeEditMultiFilterDropdown {
		t.Errorf("interactionMode = %v, want ModeEditMultiFilterDropdown", modal.interactionMode)
	}

	// Pre-selected items should be set from current value
	if !modal.selectedItems["feat-auth"] {
		t.Error("feat-auth should be pre-selected")
	}
	if !modal.selectedItems["feat-db"] {
		t.Error("feat-db should be pre-selected")
	}
	if modal.selectedItems["feat-ui"] {
		t.Error("feat-ui should NOT be pre-selected")
	}
	if modal.selectedItems["feat-payments"] {
		t.Error("feat-payments should NOT be pre-selected")
	}
}

func TestFeatureListFetched_EmptyCurrentDeps(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/test1.md", apiClient)

	modal.values[FieldFeatureDependsOn] = ""
	modal.focusedField = FieldFeatureDependsOn

	msg := featureListFetchedMsg{
		featureIDs: []string{"feat-auth", "feat-db"},
	}

	modal.Update(msg)

	// No items should be pre-selected
	if len(modal.selectedItems) != 0 {
		t.Errorf("expected 0 pre-selected items, got %d", len(modal.selectedItems))
	}
}

func TestFeatureListFetched_SetsDropdownOptions(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/test1.md", apiClient)

	modal.focusedField = FieldFeatureDependsOn
	msg := featureListFetchedMsg{
		featureIDs: []string{"feat-auth", "feat-db", "feat-ui"},
	}

	modal.Update(msg)

	if len(modal.dropdownOptions) != 3 {
		t.Errorf("expected 3 dropdown options, got %d", len(modal.dropdownOptions))
	}
	if len(modal.filteredOptions) != 3 {
		t.Errorf("expected 3 filtered options, got %d", len(modal.filteredOptions))
	}
}

func TestFeatureListFetched_Error(t *testing.T) {
	cfg := runner.RunnerConfig{BrainAPIURL: "http://localhost:3333"}
	apiClient := runner.NewAPIClient(cfg)
	modal := NewMetadataModal("projects/brain-api/task/test1.md", apiClient)

	errMsg := featureListFetchedMsg{
		err: fmt.Errorf("API error"),
	}

	modal.Update(errMsg)

	// Should NOT enter multi-filter mode on error
	if modal.interactionMode == ModeEditMultiFilterDropdown {
		t.Error("should not enter multi-filter mode on error")
	}
	if modal.saveError == nil {
		t.Error("expected saveError to be set")
	}
}

// ============================================================================
// getFieldDisplayValue Tests for MultiFilterDropdown
// ============================================================================

func TestGetFieldDisplayValue_MultiFilterDropdown_WithValue(t *testing.T) {
	modal := newMultiFilterTestModal(nil, nil)
	modal.values[FieldFeatureDependsOn] = "feat-auth, feat-db"

	value := modal.getFieldDisplayValue(FieldFeatureDependsOn)
	if value != "feat-auth, feat-db" {
		t.Errorf("expected 'feat-auth, feat-db', got %q", value)
	}
}

func TestGetFieldDisplayValue_MultiFilterDropdown_Empty(t *testing.T) {
	modal := newMultiFilterTestModal(nil, nil)
	modal.values[FieldFeatureDependsOn] = ""

	value := modal.getFieldDisplayValue(FieldFeatureDependsOn)
	// Should show "(none)" styled
	if !strings.Contains(value, "none") {
		t.Errorf("expected '(none)' for empty value, got %q", value)
	}
}

package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/huynle/brain-api/internal/runner"
)

// ============================================================================
// Interaction Mode Enum
// ============================================================================

// MetadataInteractionMode represents the current interaction mode.
type MetadataInteractionMode int

const (
	ModeNavigate MetadataInteractionMode = iota
	ModeEditText
	ModeEditDropdown
)

// ============================================================================
// MetadataModal
// ============================================================================

// MetadataModal is a modal for editing task metadata fields.
type MetadataModal struct {
	taskID          string
	apiClient       *runner.APIClient
	interactionMode MetadataInteractionMode
	focusedField    MetadataField
	focusedIndex    int
	values          map[MetadataField]string
	boolValues      map[MetadataField]bool
	editBuffer      string
	dropdownIndex   int
	dropdownOptions []string
	fieldList       []MetadataField
	width           int
	height          int
}

// NewMetadataModal creates a new metadata editing modal.
func NewMetadataModal(taskID string, apiClient *runner.APIClient) *MetadataModal {
	// Initialize field list in proper order
	fieldList := []MetadataField{
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

	return &MetadataModal{
		taskID:          taskID,
		apiClient:       apiClient,
		interactionMode: ModeNavigate,
		focusedIndex:    0,
		values:          make(map[MetadataField]string),
		boolValues:      make(map[MetadataField]bool),
		fieldList:       fieldList,
		width:           60,
		height:          25,
	}
}

// ============================================================================
// Modal Interface Implementation (Stubs)
// ============================================================================

// Init initializes the modal (stub).
func (m *MetadataModal) Init() tea.Cmd {
	return nil
}

// Update handles messages (stub).
func (m *MetadataModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	return m, nil
}

// View renders the modal content (stub).
func (m *MetadataModal) View() string {
	return "Metadata Modal (stub)"
}

// HandleKey handles a key press (stub).
func (m *MetadataModal) HandleKey(key string) (bool, tea.Cmd) {
	return false, nil
}

// Title returns the modal title.
func (m *MetadataModal) Title() string {
	return "Update Metadata"
}

// Width returns the desired width.
func (m *MetadataModal) Width() int {
	return m.width
}

// Height returns the desired height.
func (m *MetadataModal) Height() int {
	return m.height
}

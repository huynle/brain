package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
// Navigation Methods
// ============================================================================

// moveDown moves focus to the next field (wraps to top).
func (m *MetadataModal) moveDown() {
	m.focusedIndex++
	if m.focusedIndex >= len(m.fieldList) {
		m.focusedIndex = 0
	}
	m.focusedField = m.fieldList[m.focusedIndex]
}

// moveUp moves focus to the previous field (wraps to bottom).
func (m *MetadataModal) moveUp() {
	m.focusedIndex--
	if m.focusedIndex < 0 {
		m.focusedIndex = len(m.fieldList) - 1
	}
	m.focusedField = m.fieldList[m.focusedIndex]
}

// moveToTop moves focus to the first field.
func (m *MetadataModal) moveToTop() {
	m.focusedIndex = 0
	m.focusedField = m.fieldList[m.focusedIndex]
}

// moveToBottom moves focus to the last field.
func (m *MetadataModal) moveToBottom() {
	m.focusedIndex = len(m.fieldList) - 1
	m.focusedField = m.fieldList[m.focusedIndex]
}

// moveDropdownDown moves dropdown selection down (wraps to top).
func (m *MetadataModal) moveDropdownDown() {
	m.dropdownIndex++
	if m.dropdownIndex >= len(m.dropdownOptions) {
		m.dropdownIndex = 0
	}
}

// moveDropdownUp moves dropdown selection up (wraps to bottom).
func (m *MetadataModal) moveDropdownUp() {
	m.dropdownIndex--
	if m.dropdownIndex < 0 {
		m.dropdownIndex = len(m.dropdownOptions) - 1
	}
}

// enterEditMode transitions to edit mode based on field type.
func (m *MetadataModal) enterEditMode() {
	fieldType := getFieldType(m.focusedField)

	switch fieldType {
	case FieldTypeText:
		m.interactionMode = ModeEditText
		// Initialize editBuffer with current value
		if val, ok := m.values[m.focusedField]; ok {
			m.editBuffer = val
		} else {
			m.editBuffer = ""
		}
	case FieldTypeDropdown, FieldTypeBoolean:
		m.interactionMode = ModeEditDropdown
		m.dropdownOptions = getEnumOptions(m.focusedField)

		// For booleans, create options if needed
		if fieldType == FieldTypeBoolean {
			m.dropdownOptions = []string{"true", "false"}
		}

		// Find current value in dropdown options and set index
		m.dropdownIndex = 0 // Default to 0
		var currentValue string

		if fieldType == FieldTypeBoolean {
			// Get boolean value and convert to string
			if val, ok := m.boolValues[m.focusedField]; ok {
				if val {
					currentValue = "true"
				} else {
					currentValue = "false"
				}
			}
		} else {
			// Get string value
			if val, ok := m.values[m.focusedField]; ok {
				currentValue = val
			}
		}

		// Find index of current value in options
		if currentValue != "" {
			for i, option := range m.dropdownOptions {
				if option == currentValue {
					m.dropdownIndex = i
					break
				}
			}
		}
	}
}

// ============================================================================
// Text Editing Methods
// ============================================================================

// appendChar appends a rune to the edit buffer.
func (m *MetadataModal) appendChar(r rune) {
	m.editBuffer += string(r)
}

// deleteChar removes the last rune from the edit buffer.
func (m *MetadataModal) deleteChar() {
	if len(m.editBuffer) == 0 {
		return
	}
	// Convert to runes to handle multi-byte characters correctly
	runes := []rune(m.editBuffer)
	if len(runes) > 0 {
		m.editBuffer = string(runes[:len(runes)-1])
	}
}

// clearBuffer clears the edit buffer.
func (m *MetadataModal) clearBuffer() {
	m.editBuffer = ""
}

// handleEditTextMode handles key presses in text editing mode.
func (m *MetadataModal) handleEditTextMode(key string) (bool, tea.Cmd) {
	switch key {
	case "backspace":
		m.deleteChar()
		return true, nil
	case "ctrl+u":
		m.clearBuffer()
		return true, nil
	case "enter":
		cmd := m.saveField()
		m.interactionMode = ModeNavigate
		return true, cmd
	case "esc":
		// Discard changes
		m.editBuffer = ""
		m.interactionMode = ModeNavigate
		return true, nil
	default:
		// Check if it's a single printable character
		if len(key) == 1 {
			m.appendChar(rune(key[0]))
			return true, nil
		}
		return false, nil
	}
}

// handleEditDropdownMode handles key presses in dropdown editing mode.
func (m *MetadataModal) handleEditDropdownMode(key string) (bool, tea.Cmd) {
	switch key {
	case "j", "down":
		m.moveDropdownDown()
		return true, nil
	case "k", "up":
		m.moveDropdownUp()
		return true, nil
	case "enter":
		cmd := m.saveField()
		m.interactionMode = ModeNavigate
		return true, cmd
	case "esc":
		// Discard changes
		m.interactionMode = ModeNavigate
		return true, nil
	default:
		// Consume but ignore all other keys
		return true, nil
	}
}

// saveField saves the current edit to the values map.
func (m *MetadataModal) saveField() tea.Cmd {
	fieldType := getFieldType(m.focusedField)

	switch fieldType {
	case FieldTypeText:
		m.values[m.focusedField] = m.editBuffer
	case FieldTypeDropdown:
		if m.dropdownIndex >= 0 && m.dropdownIndex < len(m.dropdownOptions) {
			m.values[m.focusedField] = m.dropdownOptions[m.dropdownIndex]
		}
	case FieldTypeBoolean:
		// For booleans in dropdown mode
		if m.dropdownIndex >= 0 && m.dropdownIndex < len(m.dropdownOptions) {
			m.boolValues[m.focusedField] = m.dropdownOptions[m.dropdownIndex] == "true"
		}
	}

	// Clear edit buffer
	m.editBuffer = ""

	// Phase 4 will add API call here
	return nil
}

// ============================================================================
// Modal Interface Implementation
// ============================================================================

// Init initializes the modal.
func (m *MetadataModal) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (m *MetadataModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	return m, nil
}

// View renders the modal content.
func (m *MetadataModal) View() string {
	var b strings.Builder

	// Render field list
	for i, field := range m.fieldList {
		// Determine indicator and styling based on focus
		var indicator, line string
		isFocused := i == m.focusedIndex

		if isFocused {
			indicator = "→"
		} else {
			indicator = " "
		}

		// Get field label and value
		label := getFieldLabel(field)
		value := m.getFieldDisplayValue(field)

		// Format line
		line = fmt.Sprintf("%s %s: %s", indicator, label, value)

		// Apply styling
		if isFocused {
			style := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
			line = style.Render(line)
		} else {
			style := lipgloss.NewStyle().Foreground(ColorDim)
			line = style.Render(line)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	// Add footer help text
	b.WriteString("\n")
	helpStyle := lipgloss.NewStyle().Foreground(ColorDim).Italic(true)
	helpText := "j/k: navigate  Enter: edit  Esc: close"
	b.WriteString(helpStyle.Render(helpText))

	return b.String()
}

// getFieldDisplayValue returns the display value for a field.
func (m *MetadataModal) getFieldDisplayValue(field MetadataField) string {
	fieldType := getFieldType(field)

	switch fieldType {
	case FieldTypeBoolean:
		if val, ok := m.boolValues[field]; ok {
			if val {
				return "true"
			}
			return "false"
		}
		return lipgloss.NewStyle().Foreground(ColorDim).Render("(none)")

	case FieldTypeText, FieldTypeDropdown:
		if val, ok := m.values[field]; ok && val != "" {
			return val
		}
		return lipgloss.NewStyle().Foreground(ColorDim).Render("(none)")

	default:
		return lipgloss.NewStyle().Foreground(ColorDim).Render("(none)")
	}
}

// HandleKey handles a key press.
func (m *MetadataModal) HandleKey(key string) (bool, tea.Cmd) {
	switch m.interactionMode {
	case ModeNavigate:
		return m.handleNavigateMode(key)
	case ModeEditText:
		return m.handleEditTextMode(key)
	case ModeEditDropdown:
		return m.handleEditDropdownMode(key)
	default:
		return false, nil
	}
}

// handleNavigateMode handles key presses in navigation mode.
func (m *MetadataModal) handleNavigateMode(key string) (bool, tea.Cmd) {
	switch key {
	case "j", "down":
		m.moveDown()
		return true, nil
	case "k", "up":
		m.moveUp()
		return true, nil
	case "g":
		m.moveToTop()
		return true, nil
	case "G":
		m.moveToBottom()
		return true, nil
	case "enter":
		m.enterEditMode()
		return true, nil
	default:
		return false, nil
	}
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

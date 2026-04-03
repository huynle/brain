package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

// ============================================================================
// Messages
// ============================================================================

// metadataFetchedMsg is sent when entry metadata has been fetched.
type metadataFetchedMsg struct {
	entries []*types.BrainEntry
	err     error
}

// metadataUpdatedMsg is sent when a field has been updated.
type metadataUpdatedMsg struct {
	field MetadataField
	value string
	err   error
}

// monitorTemplatesFetchedMsg is sent when monitor template statuses have been fetched.
type monitorTemplatesFetchedMsg struct {
	templates []MonitorTemplateState
	err       error
}

// monitorTemplatesListedMsg is sent when the template list has been fetched from the API.
type monitorTemplatesListedMsg struct {
	templates []MonitorTemplateState
	err       error
}

// monitorToggleResultMsg is sent when a monitor template toggle completes.
type monitorToggleResultMsg struct {
	index     int
	newStatus string
	taskPath  string
	err       error
}

// ============================================================================
// Monitor Template Types
// ============================================================================

// MonitorTemplateState represents the state of a monitor template in the metadata modal.
type MonitorTemplateState struct {
	TemplateID string
	Label      string
	Status     string // "loading", "enabled", "create"
	Schedule   string
	TaskPath   string // path for deletion when enabled
}

// ============================================================================
// Mode Enums
// ============================================================================

// MetadataMode represents the editing mode (single, batch, or feature).
type MetadataMode int

const (
	ModeSingle MetadataMode = iota
	ModeBatch
	ModeFeature
)

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
// taskPaths stores entry paths (e.g. "projects/brain-api/task/abc123.md") for API calls.
type MetadataModal struct {
	taskPaths       []string
	featureID       string // Feature ID for ModeFeature
	projectID       string // Project ID for ModeFeature (needed for API calls)
	mode            MetadataMode
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

	// Tab state
	currentTab MetadataTab   // active tab
	tabs       []MetadataTab // ordered tabs for current mode

	// Mixed field tracking for batch mode
	mixedFields map[MetadataField]bool

	// API state
	loading        bool
	fetchError     error
	saveError      error
	saveSuccess    bool
	lastSavedField MetadataField

	// Monitor template state (feature mode only)
	monitorTemplates    []MonitorTemplateState
	monitorLoading      bool
	focusedMonitorIndex int // -1 when not in monitor zone
	monitorClient       *MonitorClient
}

// NewMetadataModal creates a new metadata editing modal for a single task.
// taskPath should be the entry path (e.g. "projects/brain-api/task/abc123.md").
func NewMetadataModal(taskPath string, apiClient *runner.APIClient) *MetadataModal {
	return newMetadataModal([]string{taskPath}, ModeSingle, apiClient)
}

// NewMetadataModalBatch creates a new metadata editing modal for multiple tasks.
// taskPaths should be entry paths (e.g. "projects/brain-api/task/abc123.md").
func NewMetadataModalBatch(taskPaths []string, apiClient *runner.APIClient) *MetadataModal {
	return newMetadataModal(taskPaths, ModeBatch, apiClient)
}

// NewMetadataModalFeature creates a new metadata editing modal for a feature.
// The taskPaths will be populated in Init() when fetching tasks by feature_id.
// An optional MonitorClient can be passed to enable monitor template rows.
func NewMetadataModalFeature(featureID, projectID string, apiClient *runner.APIClient, monitorClients ...*MonitorClient) *MetadataModal {
	var mc *MonitorClient
	if len(monitorClients) > 0 {
		mc = monitorClients[0]
	}

	// Templates are fetched from API in Init() — start empty
	tabs := tabsForMode(ModeFeature)
	m := &MetadataModal{
		featureID:           featureID,
		projectID:           projectID,
		mode:                ModeFeature,
		apiClient:           apiClient,
		taskPaths:           []string{}, // Will be populated in Init
		values:              make(map[MetadataField]string),
		boolValues:          make(map[MetadataField]bool),
		mixedFields:         make(map[MetadataField]bool),
		interactionMode:     ModeNavigate,
		focusedIndex:        0,
		width:               60,
		height:              24,
		monitorTemplates:    nil, // fetched from API in Init
		monitorLoading:      true,
		focusedMonitorIndex: -1,
		monitorClient:       mc,
		tabs:                tabs,
		currentTab:          tabs[0],
	}
	// Set field list based on mode and current tab
	m.fieldList = m.buildFieldList()
	return m
}

// newMetadataModal is the internal constructor.
func newMetadataModal(taskPaths []string, mode MetadataMode, apiClient *runner.APIClient) *MetadataModal {
	tabs := tabsForMode(mode)
	m := &MetadataModal{
		taskPaths:           taskPaths,
		mode:                mode,
		apiClient:           apiClient,
		interactionMode:     ModeNavigate,
		focusedIndex:        0,
		values:              make(map[MetadataField]string),
		boolValues:          make(map[MetadataField]bool),
		mixedFields:         make(map[MetadataField]bool),
		width:               60,
		height:              25,
		focusedMonitorIndex: -1,
		tabs:                tabs,
		currentTab:          tabs[0],
	}
	// Set field list based on mode/tab and initialize focused field
	m.fieldList = m.buildFieldList()
	if len(m.fieldList) > 0 {
		m.focusedField = m.fieldList[0]
	}
	return m
}

// ============================================================================
// Field Management Methods
// ============================================================================

// buildFieldList returns the list of fields to display based on mode and current tab.
func (m *MetadataModal) buildFieldList() []MetadataField {
	return fieldsForTab(m.currentTab, m.mode)
}

// ============================================================================
// Navigation Methods
// ============================================================================

// hasMonitorRows returns true if this modal has monitor template rows to display.
func (m *MetadataModal) hasMonitorRows() bool {
	return m.mode == ModeFeature && m.currentTab == MetaTabMonitors && len(m.monitorTemplates) > 0 && !m.monitorLoading
}

// inMonitorZone returns true if focus is currently in the monitor rows zone.
func (m *MetadataModal) inMonitorZone() bool {
	return m.focusedMonitorIndex >= 0
}

// moveDown moves focus to the next field or monitor row (wraps to top).
func (m *MetadataModal) moveDown() {
	if m.inMonitorZone() {
		// In monitor zone: move to next monitor row or wrap to first monitor
		m.focusedMonitorIndex++
		if m.focusedMonitorIndex >= len(m.monitorTemplates) {
			// Wrap to first monitor row (Monitors tab has no fields)
			m.focusedMonitorIndex = 0
		}
		return
	}

	// In field zone
	if len(m.fieldList) == 0 {
		// No fields (e.g., Monitors tab) — stay in monitor zone if available
		if m.hasMonitorRows() {
			m.focusedMonitorIndex = 0
		}
		return
	}

	m.focusedIndex++
	if m.focusedIndex >= len(m.fieldList) {
		// Wrap to first field
		m.focusedIndex = 0
	}
	if m.focusedIndex < len(m.fieldList) {
		m.focusedField = m.fieldList[m.focusedIndex]
	}
}

// moveUp moves focus to the previous field or monitor row (wraps to bottom).
func (m *MetadataModal) moveUp() {
	if m.inMonitorZone() {
		// In monitor zone: move to previous monitor row or wrap to last monitor
		m.focusedMonitorIndex--
		if m.focusedMonitorIndex < 0 {
			// Wrap to last monitor row (Monitors tab has no fields)
			m.focusedMonitorIndex = len(m.monitorTemplates) - 1
		}
		return
	}

	// In field zone
	if len(m.fieldList) == 0 {
		// No fields (e.g., Monitors tab) — stay in monitor zone if available
		if m.hasMonitorRows() {
			m.focusedMonitorIndex = len(m.monitorTemplates) - 1
		}
		return
	}

	m.focusedIndex--
	if m.focusedIndex < 0 {
		// Wrap to last field
		m.focusedIndex = len(m.fieldList) - 1
	}
	if m.focusedIndex < len(m.fieldList) {
		m.focusedField = m.fieldList[m.focusedIndex]
	}
}

// moveToTop moves focus to the first field or first monitor row.
func (m *MetadataModal) moveToTop() {
	m.focusedMonitorIndex = -1
	if len(m.fieldList) > 0 {
		m.focusedIndex = 0
		m.focusedField = m.fieldList[0]
	} else if m.hasMonitorRows() {
		m.focusedMonitorIndex = 0
	}
}

// moveToBottom moves focus to the last field or last monitor row.
func (m *MetadataModal) moveToBottom() {
	if m.hasMonitorRows() {
		m.focusedMonitorIndex = len(m.monitorTemplates) - 1
		m.focusedIndex = len(m.fieldList)
	} else if len(m.fieldList) > 0 {
		m.focusedIndex = len(m.fieldList) - 1
		m.focusedField = m.fieldList[m.focusedIndex]
	}
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

// saveField saves the current edit to the values map and sends API update.
func (m *MetadataModal) saveField() tea.Cmd {
	fieldType := getFieldType(m.focusedField)

	// Build updates map
	updates := make(map[string]interface{})

	if fieldType == FieldTypeText || fieldType == FieldTypeDropdown {
		var value string
		if m.interactionMode == ModeEditText {
			value = m.editBuffer
		} else {
			value = m.dropdownOptions[m.dropdownIndex]
		}

		m.values[m.focusedField] = value
		updates[string(m.focusedField)] = value
	} else if fieldType == FieldTypeBoolean {
		value := m.boolValues[m.focusedField]
		updates[string(m.focusedField)] = value
	}

	// Clear mixed indicator for this field (user made explicit choice)
	m.mixedFields[m.focusedField] = false

	// Clear edit buffer
	m.editBuffer = ""

	// Save field and value for response handling
	field := m.focusedField
	fieldValue := m.values[field]

	// Return command that updates via API (all tasks in batch mode)
	return func() tea.Msg {
		ctx := context.Background()

		var wg sync.WaitGroup
		errors := make([]error, len(m.taskPaths))

		for i, taskPath := range m.taskPaths {
			wg.Add(1)
			go func(idx int, path string) {
				defer wg.Done()
				_, err := m.apiClient.UpdateEntry(ctx, path, updates)
				errors[idx] = err
			}(i, taskPath)
		}
		wg.Wait()

		// Check for errors
		for _, err := range errors {
			if err != nil {
				return metadataUpdatedMsg{
					field: field,
					value: fieldValue,
					err:   err,
				}
			}
		}

		return metadataUpdatedMsg{
			field: field,
			value: fieldValue,
			err:   nil,
		}
	}
}

// ============================================================================
// Modal Interface Implementation
// ============================================================================

// Init initializes the modal by fetching entry data.
func (m *MetadataModal) Init() tea.Cmd {
	m.loading = true
	return func() tea.Msg {
		ctx := context.Background()

		// Step 1: If ModeFeature and task paths not already populated, fetch from API
		taskPaths := m.taskPaths
		if m.mode == ModeFeature && m.featureID != "" && len(taskPaths) == 0 {
			tasks, err := m.apiClient.GetTasksByFeature(ctx, m.projectID, m.featureID)
			if err != nil {
				return metadataFetchedMsg{entries: nil, err: fmt.Errorf("fetch tasks for feature: %w", err)}
			}

			// Extract task paths from feature tasks
			taskPaths = make([]string, len(tasks))
			for i, task := range tasks {
				taskPaths[i] = task.Path
			}

			// Store for later use
			m.taskPaths = taskPaths
		}

		// Step 2: Fetch all task entries in parallel
		entries := make([]*types.BrainEntry, len(taskPaths))
		errors := make([]error, len(taskPaths))

		var wg sync.WaitGroup
		for i, taskPath := range taskPaths {
			wg.Add(1)
			go func(idx int, path string) {
				defer wg.Done()
				entry, err := m.apiClient.GetEntry(ctx, path)
				entries[idx] = entry
				errors[idx] = err
			}(i, taskPath)
		}
		wg.Wait()

		// Check for errors
		for _, err := range errors {
			if err != nil {
				return metadataFetchedMsg{entries: nil, err: err}
			}
		}

		return metadataFetchedMsg{
			entries: entries,
			err:     nil,
		}
	}
}

// fetchMonitorStatusesCmd returns a tea.Cmd that fetches monitor template statuses.
// fetchMonitorTemplatesCmd fetches available templates from the API.
func (m *MetadataModal) fetchMonitorTemplatesCmd() tea.Cmd {
	client := m.monitorClient
	return func() tea.Msg {
		ctx := context.Background()
		apiTemplates, err := client.FetchTemplates(ctx)
		if err != nil {
			return monitorTemplatesListedMsg{err: err}
		}

		templates := make([]MonitorTemplateState, len(apiTemplates))
		for i, t := range apiTemplates {
			schedule := t.DefaultSchedule
			if schedule == "" {
				schedule = "one-shot"
			}
			templates[i] = MonitorTemplateState{
				TemplateID: t.ID,
				Label:      t.Label,
				Status:     "loading",
				Schedule:   schedule,
			}
		}

		return monitorTemplatesListedMsg{templates: templates, err: nil}
	}
}

func (m *MetadataModal) fetchMonitorStatusesCmd() tea.Cmd {
	client := m.monitorClient
	featureID := m.featureID
	projectID := m.projectID
	templates := make([]MonitorTemplateState, len(m.monitorTemplates))
	copy(templates, m.monitorTemplates)

	return func() tea.Msg {
		ctx := context.Background()

		for i, tmpl := range templates {
			// Unified find: try monitors API first, fall back to entries API (for legacy)
			result, err := client.FindMonitorTask(ctx, tmpl.TemplateID, featureID, projectID)
			if err != nil {
				// Fall back to entries API for backward compat with pre-registry monitors
				legacyResult, legacyErr := client.FindScheduledTask(ctx, tmpl.TemplateID, featureID)
				if legacyErr != nil {
					return monitorTemplatesFetchedMsg{err: err}
				}
				if legacyResult != nil {
					templates[i].Status = "enabled"
					templates[i].TaskPath = legacyResult.Path
					continue
				}
			}
			if result != nil {
				templates[i].Status = "enabled"
				templates[i].TaskPath = result.TaskID
			} else {
				templates[i].Status = "create"
			}
		}

		return monitorTemplatesFetchedMsg{templates: templates, err: nil}
	}
}

// Update handles messages.
func (m *MetadataModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case metadataFetchedMsg:
		m.loading = false
		if msg.err != nil {
			m.fetchError = msg.err
			return m, nil
		}

		if len(msg.entries) == 0 {
			m.fetchError = fmt.Errorf("no entries loaded")
			return m, nil
		}

		// Detect mixed fields for batch mode
		m.mixedFields = detectMixedFields(msg.entries)

		// Populate values from first entry (or shared values)
		entry := msg.entries[0]
		m.values[FieldStatus] = entry.Status
		m.values[FieldPriority] = entry.Priority
		m.values[FieldFeatureID] = entry.FeatureID
		m.values[FieldGitBranch] = entry.GitBranch
		m.values[FieldMergeTargetBranch] = entry.MergeTargetBranch
		m.values[FieldMergePolicy] = entry.MergePolicy
		m.values[FieldMergeStrategy] = entry.MergeStrategy
		m.values[FieldExecutionMode] = entry.ExecutionMode
		m.values[FieldDirectPrompt] = entry.DirectPrompt
		m.values[FieldAgent] = entry.Agent
		m.values[FieldModel] = entry.Model
		m.values[FieldTargetWorkdir] = entry.TargetWorkdir
		m.values[FieldSchedule] = entry.Schedule

		// Boolean values
		if entry.CompleteOnIdle != nil {
			m.boolValues[FieldCompleteOnIdle] = *entry.CompleteOnIdle
		}
		if entry.OpenPRBeforeMerge != nil {
			m.boolValues[FieldOpenPRBeforeMerge] = *entry.OpenPRBeforeMerge
		}

		// In feature mode, kick off template list fetch from API
		if m.mode == ModeFeature && m.monitorClient != nil {
			return m, m.fetchMonitorTemplatesCmd()
		}

		return m, nil

	case metadataUpdatedMsg:
		if msg.err != nil {
			m.saveError = msg.err
			m.saveSuccess = false
		} else {
			m.saveSuccess = true
			m.lastSavedField = msg.field
			m.saveError = nil
			// Clear mixed indicator for this field after successful save
			m.mixedFields[msg.field] = false
		}
		return m, nil

	case monitorTemplatesListedMsg:
		if msg.err != nil {
			// Template fetch failed — proceed without monitor rows
			m.monitorLoading = false
			return m, nil
		}
		m.monitorTemplates = msg.templates
		// Now fetch statuses for each template
		return m, m.fetchMonitorStatusesCmd()

	case monitorTemplatesFetchedMsg:
		m.monitorLoading = false
		if msg.err != nil {
			return m, nil
		}
		if len(msg.templates) == len(m.monitorTemplates) {
			m.monitorTemplates = msg.templates
		}
		return m, nil

	case monitorToggleResultMsg:
		if msg.index >= 0 && msg.index < len(m.monitorTemplates) {
			m.monitorTemplates[msg.index].Status = msg.newStatus
			m.monitorTemplates[msg.index].TaskPath = msg.taskPath
		}
		return m, nil
	}

	return m, nil
}

// renderTabHeader renders the tab selection header matching the main TUI's
// Tasks/Dream tab bar style: space-padded labels, bold+cyan active, dim inactive.
// If all tabs don't fit in the modal width, a sliding window is shown with
// ◀/▶ overflow indicators, always keeping the active tab visible.
func (m *MetadataModal) renderTabHeader() string {
	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorCyan)
	inactiveStyle := lipgloss.NewStyle().Foreground(ColorDim)
	overflowStyle := lipgloss.NewStyle().Foreground(ColorDim)

	lo, hi := m.visibleTabRange()
	allVisible := lo == 0 && hi == len(m.tabs)-1

	// Build rendered tab strings for the visible range
	var parts []string
	if !allVisible && lo > 0 {
		parts = append(parts, overflowStyle.Render("◀"))
	}
	for i := lo; i <= hi; i++ {
		name := " " + tabLabel(m.tabs[i]) + " "
		if m.tabs[i] == m.currentTab {
			parts = append(parts, activeStyle.Render(name))
		} else {
			parts = append(parts, inactiveStyle.Render(name))
		}
	}
	if !allVisible && hi < len(m.tabs)-1 {
		parts = append(parts, overflowStyle.Render("▶"))
	}

	// Use 2-space gap when all fit, 1-space when truncated (tighter layout)
	sep := "  "
	if !allVisible {
		sep = " "
	}
	return " " + strings.Join(parts, sep)
}

// View renders the modal content.
func (m *MetadataModal) View() string {
	var b strings.Builder

	// Show loading state
	if m.loading {
		loadingStyle := lipgloss.NewStyle().Foreground(ColorCyan).Italic(true)
		b.WriteString(loadingStyle.Render("Loading metadata..."))
		b.WriteString("\n")
		return b.String()
	}

	// Show fetch error
	if m.fetchError != nil {
		errorStyle := lipgloss.NewStyle().Foreground(ColorBlocked).Bold(true)
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.fetchError)))
		b.WriteString("\n")
		helpStyle := lipgloss.NewStyle().Foreground(ColorDim).Italic(true)
		b.WriteString(helpStyle.Render("Press Esc to close"))
		b.WriteString("\n")
		return b.String()
	}

	// Show save success message
	if m.saveSuccess {
		successStyle := lipgloss.NewStyle().Foreground(ColorReady).Bold(true)
		var message string
		if m.mode == ModeFeature {
			message = fmt.Sprintf("✓ Updated %d tasks in feature %s", len(m.taskPaths), m.featureID)
		} else {
			message = fmt.Sprintf("✓ Saved %s", getFieldLabel(m.lastSavedField))
		}
		b.WriteString(successStyle.Render(message))
		b.WriteString("\n\n")
		m.saveSuccess = false // Clear after displaying
	}

	// Show save error
	if m.saveError != nil {
		errorStyle := lipgloss.NewStyle().Foreground(ColorBlocked).Bold(true)
		b.WriteString(errorStyle.Render(fmt.Sprintf("✗ Error: %v", m.saveError)))
		b.WriteString("\n\n")
	}

	// Render tab header
	b.WriteString(m.renderTabHeader())
	b.WriteString("\n\n")

	// Render field list
	for i, field := range m.fieldList {
		// Determine indicator and styling based on focus
		var indicator, line string
		isFocused := i == m.focusedIndex && !m.inMonitorZone()

		if isFocused {
			indicator = "→"
		} else {
			indicator = " "
		}

		// Get field label and value
		label := getFieldLabel(field)
		value := m.getFieldDisplayValue(field)

		// If in edit mode for this field, show edit UI
		if isFocused && m.interactionMode == ModeEditText {
			// Show edit buffer with cursor
			line = fmt.Sprintf("%s %s: %s_", indicator, label, m.editBuffer)
			style := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Background(lipgloss.Color("235"))
			line = style.Render(line)
		} else if isFocused && m.interactionMode == ModeEditDropdown {
			// Show dropdown popup
			line = fmt.Sprintf("%s %s: %s", indicator, label, value)
			style := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
			b.WriteString(style.Render(line))
			b.WriteString("\n\n")
			// Render dropdown options
			b.WriteString(m.renderDropdown())
			b.WriteString("\n")
			continue
		} else {
			// Format line normally
			line = fmt.Sprintf("%s %s: %s", indicator, label, value)

			// Apply styling
			if isFocused {
				style := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
				line = style.Render(line)
			} else {
				style := lipgloss.NewStyle().Foreground(ColorDim)
				line = style.Render(line)
			}
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	// Render monitor template section (Monitors tab only)
	if m.mode == ModeFeature && m.currentTab == MetaTabMonitors && len(m.monitorTemplates) > 0 {
		b.WriteString("\n")
		// Separator
		separatorStyle := lipgloss.NewStyle().Foreground(ColorDim).Bold(true)
		b.WriteString(separatorStyle.Render("── Automated Tasks ──"))
		b.WriteString("\n")

		if m.monitorLoading {
			loadingStyle := lipgloss.NewStyle().Foreground(ColorDim).Italic(true)
			b.WriteString(loadingStyle.Render("  Loading..."))
			b.WriteString("\n")
		} else {
			for i, tmpl := range m.monitorTemplates {
				isFocused := m.focusedMonitorIndex == i

				// Arrow prefix
				var indicator string
				if isFocused {
					indicator = "→"
				} else {
					indicator = " "
				}

				// Status icon
				var icon, statusTag string
				switch tmpl.Status {
				case "enabled":
					icon = lipgloss.NewStyle().Foreground(ColorReady).Render("●")
					statusTag = lipgloss.NewStyle().Foreground(ColorReady).Render("[enabled]")
				case "loading":
					icon = lipgloss.NewStyle().Foreground(ColorWaiting).Render("◌")
					statusTag = lipgloss.NewStyle().Foreground(ColorWaiting).Render("[loading]")
				default: // "create"
					icon = lipgloss.NewStyle().Foreground(ColorDim).Render("○")
					statusTag = lipgloss.NewStyle().Foreground(ColorDim).Render("[create]")
				}

				// Label
				var label string
				if isFocused {
					label = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Render(tmpl.Label)
				} else {
					label = tmpl.Label
				}

				// Schedule (show for enabled)
				var schedule string
				if tmpl.Status == "enabled" && tmpl.Schedule != "" {
					schedule = " " + lipgloss.NewStyle().Foreground(ColorDim).Render(tmpl.Schedule)
				}

				line := fmt.Sprintf("%s %s %s %s%s", indicator, icon, label, statusTag, schedule)
				b.WriteString(line)
				b.WriteString("\n")
			}
		}
	}

	// Add footer help text
	b.WriteString("\n")
	helpStyle := lipgloss.NewStyle().Foreground(ColorDim).Italic(true)
	var helpText string
	switch m.interactionMode {
	case ModeEditText:
		helpText = "Enter: save  Ctrl-U: clear  Esc: cancel"
	case ModeEditDropdown:
		helpText = "j/k: select  Enter: save  Esc: cancel"
	default:
		helpText = "j/k: navigate  Enter: edit  H/L: sections  Esc: close"
	}
	b.WriteString(helpStyle.Render(helpText))

	return b.String()
}

// renderDropdown renders dropdown options with selection indicator.
func (m *MetadataModal) renderDropdown() string {
	var lines []string
	for i, option := range m.dropdownOptions {
		indicator := " "
		if i == m.dropdownIndex {
			indicator = "→"
		}
		line := fmt.Sprintf("  %s %s", indicator, option)
		if i == m.dropdownIndex {
			style := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true).Background(lipgloss.Color("235"))
			lines = append(lines, style.Render(line))
		} else {
			style := lipgloss.NewStyle().Foreground(ColorDim)
			lines = append(lines, style.Render(line))
		}
	}

	return strings.Join(lines, "\n")
}

// getFieldDisplayValue returns the display value for a field.
func (m *MetadataModal) getFieldDisplayValue(field MetadataField) string {
	// In batch/feature mode, show "(mixed)" for fields that differ across tasks
	if m.mixedFields[field] {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#ff9900")).Italic(true).Render("(mixed)")
	}

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

// HandleMouse handles mouse events within the modal content area.
// x, y are relative to the content origin (0,0 = top-left of scrollable content).
// Implements the MouseModal interface.
func (m *MetadataModal) HandleMouse(msg tea.MouseMsg, x, y int) (bool, tea.Cmd) {
	// Don't handle mouse during edit modes
	if m.interactionMode != ModeNavigate {
		return false, nil
	}

	// Line 0: tab header
	if y == 0 {
		return m.handleTabClick(x), nil
	}

	// Line 1: blank line after tab header
	// Lines 2+: field rows (or status messages may shift things, but save/error messages
	// are transient and we'll use a simple offset)

	// Success/error messages appear before the tab header in the View output,
	// but since those are transient and cleared after display, the normal layout is:
	//   Line 0: tab header
	//   Line 1: blank
	//   Line 2..N: field rows (one per field)
	//   After fields: monitor rows (on Monitors tab)

	fieldStartY := 2 // tab header (1) + blank line (1)
	fieldRow := y - fieldStartY

	// Click on a field row
	if fieldRow >= 0 && fieldRow < len(m.fieldList) {
		m.focusedMonitorIndex = -1
		m.focusedIndex = fieldRow
		m.focusedField = m.fieldList[fieldRow]
		return true, nil
	}

	// Click on a monitor row (Monitors tab)
	if m.currentTab == MetaTabMonitors && m.hasMonitorRows() {
		// Monitor rows start after fields + blank line + separator line
		monitorStartY := fieldStartY + len(m.fieldList)
		if len(m.fieldList) == 0 {
			// Monitors tab has no fields, rows start after: blank + separator
			monitorStartY = fieldStartY + 2 // blank line + separator "── Automated Tasks ──"
		} else {
			monitorStartY = fieldStartY + len(m.fieldList) + 2 // fields + blank + separator
		}

		monitorRow := y - monitorStartY
		if monitorRow >= 0 && monitorRow < len(m.monitorTemplates) {
			m.focusedMonitorIndex = monitorRow
			m.focusedIndex = len(m.fieldList) // move past fields
			return true, nil
		}
	}

	return false, nil
}

// handleTabClick processes a mouse click on the tab header line.
// x is the column position relative to the content area.
// Returns true if a tab was clicked.
func (m *MetadataModal) handleTabClick(x int) bool {
	lo, hi := m.visibleTabRange()

	// Reconstruct the layout from renderTabHeader:
	//   " " + [optional "◀ "] + " Label " + " " + " Label " + [optional " ▶"]
	pos := 1 // leading " "

	if lo > 0 {
		// "◀" (1 char) + " " (1 char gap) in the joined output
		pos += 2 // skip past "◀ "
	}

	for i := lo; i <= hi; i++ {
		label := " " + tabLabel(m.tabs[i]) + " "
		tabEnd := pos + len(label)
		if x >= pos && x < tabEnd {
			if m.tabs[i] != m.currentTab {
				m.switchToTab(m.tabs[i])
			}
			return true
		}
		pos = tabEnd + 1 // " " gap (truncated mode uses single space joins)
	}

	// Click on overflow arrows: navigate in that direction
	if lo > 0 && x >= 1 && x < 3 {
		m.prevTab()
		return true
	}
	// Right arrow position is after the last visible tab
	if hi < len(m.tabs)-1 && x >= pos {
		m.nextTab()
		return true
	}

	return false
}

// visibleTabRange returns the lo..hi indices (inclusive) of tabs visible in the
// tab header, matching the sliding window logic in renderTabHeader.
func (m *MetadataModal) visibleTabRange() (lo, hi int) {
	gap := 2   // "  " between tabs in non-truncated mode
	lead := 1  // leading " "
	arrow := 2 // "◀ " or " ▶"

	// Check if everything fits
	totalW := lead
	for i, tab := range m.tabs {
		if i > 0 {
			totalW += gap
		}
		totalW += len(" " + tabLabel(tab) + " ")
	}
	if totalW <= m.width {
		return 0, len(m.tabs) - 1
	}

	// Find active tab index
	activeIdx := 0
	for i, tab := range m.tabs {
		if tab == m.currentTab {
			activeIdx = i
			break
		}
	}

	lo, hi = activeIdx, activeIdx
	usedW := lead + len(" "+tabLabel(m.tabs[activeIdx])+" ")

	for {
		expanded := false
		if hi+1 < len(m.tabs) {
			nextW := gap + len(" "+tabLabel(m.tabs[hi+1])+" ")
			leftReserve := 0
			if lo > 0 {
				leftReserve = arrow
			}
			rightReserve := 0
			if hi+2 < len(m.tabs) {
				rightReserve = arrow
			}
			if usedW+nextW+leftReserve+rightReserve <= m.width {
				hi++
				usedW += nextW
				expanded = true
			}
		}
		if lo-1 >= 0 {
			nextW := gap + len(" "+tabLabel(m.tabs[lo-1])+" ")
			leftReserve := 0
			if lo-2 >= 0 {
				leftReserve = arrow
			}
			rightReserve := 0
			if hi < len(m.tabs)-1 {
				rightReserve = arrow
			}
			if usedW+nextW+leftReserve+rightReserve <= m.width {
				lo--
				usedW += nextW
				expanded = true
			}
		}
		if !expanded {
			break
		}
	}

	return lo, hi
}

// switchToTab switches to the given tab, resetting focus state.
func (m *MetadataModal) switchToTab(tab MetadataTab) {
	m.currentTab = tab
	m.focusedIndex = 0
	m.focusedMonitorIndex = -1
	m.fieldList = m.buildFieldList()
	if len(m.fieldList) > 0 {
		m.focusedField = m.fieldList[0]
	} else if m.hasMonitorRows() {
		// Monitors tab: jump directly to monitor rows
		m.focusedMonitorIndex = 0
	}
}

// nextTab cycles to the next tab (wraps around).
func (m *MetadataModal) nextTab() {
	for i, tab := range m.tabs {
		if tab == m.currentTab {
			nextIdx := (i + 1) % len(m.tabs)
			m.switchToTab(m.tabs[nextIdx])
			return
		}
	}
}

// prevTab cycles to the previous tab (wraps around).
func (m *MetadataModal) prevTab() {
	for i, tab := range m.tabs {
		if tab == m.currentTab {
			prevIdx := (i - 1 + len(m.tabs)) % len(m.tabs)
			m.switchToTab(m.tabs[prevIdx])
			return
		}
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
	case "H":
		m.prevTab()
		return true, nil
	case "L":
		m.nextTab()
		return true, nil
	case "tab":
		m.nextTab()
		return true, nil
	case "shift+tab":
		m.prevTab()
		return true, nil
	case "g":
		m.moveToTop()
		return true, nil
	case "G":
		m.moveToBottom()
		return true, nil
	case "enter", " ":
		if m.inMonitorZone() {
			return m.toggleMonitorTemplate()
		}
		if key == "enter" && len(m.fieldList) > 0 {
			m.enterEditMode()
			return true, nil
		}
		return false, nil
	case "1", "2", "3", "4", "5":
		idx := int(key[0]-'0') - 1
		if idx >= 0 && idx < len(m.tabs) {
			m.switchToTab(m.tabs[idx])
			return true, nil
		}
		return false, nil
	default:
		return false, nil
	}
}

// toggleMonitorTemplate toggles the focused monitor template between create/enabled.
func (m *MetadataModal) toggleMonitorTemplate() (bool, tea.Cmd) {
	if m.focusedMonitorIndex < 0 || m.focusedMonitorIndex >= len(m.monitorTemplates) {
		return false, nil
	}

	tmpl := &m.monitorTemplates[m.focusedMonitorIndex]
	if tmpl.Status == "loading" {
		return true, nil // Already loading, ignore
	}

	prevStatus := tmpl.Status
	tmpl.Status = "loading"

	idx := m.focusedMonitorIndex
	return true, toggleMonitorTemplateCmd(m.monitorClient, idx, m.monitorTemplates[idx], m.featureID, m.projectID, prevStatus)
}

// toggleMonitorTemplateCmd creates a tea.Cmd that toggles a monitor template.
// All templates are created/deleted via the unified monitors API.
func toggleMonitorTemplateCmd(client *MonitorClient, index int, tmpl MonitorTemplateState, featureID, project, prevStatus string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		if prevStatus == "create" {
			// Create via monitors API (handles both scheduled and dependency-gated)
			err := client.CreateMonitorTask(ctx, tmpl.TemplateID, featureID, project)
			if err != nil {
				return monitorToggleResultMsg{index: index, newStatus: prevStatus, err: err}
			}

			// Find the created task to get its path
			var taskPath string
			result, findErr := client.FindMonitorTask(ctx, tmpl.TemplateID, featureID, project)
			if findErr == nil && result != nil {
				taskPath = result.TaskID
			}

			return monitorToggleResultMsg{index: index, newStatus: "enabled", taskPath: taskPath, err: nil}
		}

		// Delete via monitors API
		err := client.DeleteMonitorTask(ctx, tmpl.TaskPath)
		if err != nil {
			// Fall back to entries API for legacy monitors
			err = client.DeleteScheduledTask(ctx, tmpl.TaskPath)
		}
		if err != nil {
			return monitorToggleResultMsg{index: index, newStatus: prevStatus, err: err}
		}

		return monitorToggleResultMsg{index: index, newStatus: "create", taskPath: "", err: nil}
	}
}

// Title returns the modal title.
func (m *MetadataModal) Title() string {
	switch m.mode {
	case ModeSingle:
		return "Update Metadata"
	case ModeBatch:
		return fmt.Sprintf("Update Metadata - %d tasks selected", len(m.taskPaths))
	case ModeFeature:
		return fmt.Sprintf("Update Feature Metadata - %s (%d tasks)", m.featureID, len(m.taskPaths))
	default:
		return "Update Metadata"
	}
}

// Width returns the desired width.
func (m *MetadataModal) Width() int {
	return m.width
}

// Height returns the desired height.
func (m *MetadataModal) Height() int {
	return m.height
}

// ============================================================================
// Batch Mode Helper Functions
// ============================================================================

// detectMixedFields compares field values across tasks and returns fields with differing values.
func detectMixedFields(entries []*types.BrainEntry) map[MetadataField]bool {
	mixed := make(map[MetadataField]bool)

	if len(entries) <= 1 {
		return mixed
	}

	// String fields
	stringFields := []MetadataField{
		FieldFeatureID, FieldGitBranch, FieldMergeTargetBranch,
		FieldMergePolicy, FieldMergeStrategy, FieldExecutionMode,
		FieldDirectPrompt, FieldAgent, FieldModel, FieldTargetWorkdir, FieldSchedule,
	}

	for _, field := range stringFields {
		values := make([]string, len(entries))
		for i, entry := range entries {
			switch field {
			case FieldFeatureID:
				values[i] = entry.FeatureID
			case FieldGitBranch:
				values[i] = entry.GitBranch
			case FieldMergeTargetBranch:
				values[i] = entry.MergeTargetBranch
			case FieldMergePolicy:
				values[i] = entry.MergePolicy
			case FieldMergeStrategy:
				values[i] = entry.MergeStrategy
			case FieldExecutionMode:
				values[i] = entry.ExecutionMode
			case FieldDirectPrompt:
				values[i] = entry.DirectPrompt
			case FieldAgent:
				values[i] = entry.Agent
			case FieldModel:
				values[i] = entry.Model
			case FieldTargetWorkdir:
				values[i] = entry.TargetWorkdir
			case FieldSchedule:
				values[i] = entry.Schedule
			}
		}
		if !allEqual(values) {
			mixed[field] = true
		}
	}

	// Status and Priority fields
	statusValues := make([]string, len(entries))
	for i, entry := range entries {
		statusValues[i] = entry.Status
	}
	if !allEqual(statusValues) {
		mixed[FieldStatus] = true
	}

	priorityValues := make([]string, len(entries))
	for i, entry := range entries {
		priorityValues[i] = entry.Priority
	}
	if !allEqual(priorityValues) {
		mixed[FieldPriority] = true
	}

	// Boolean fields
	completeOnIdleValues := make([]bool, len(entries))
	for i, entry := range entries {
		if entry.CompleteOnIdle != nil {
			completeOnIdleValues[i] = *entry.CompleteOnIdle
		}
	}
	if !allEqual(completeOnIdleValues) {
		mixed[FieldCompleteOnIdle] = true
	}

	openPRValues := make([]bool, len(entries))
	for i, entry := range entries {
		if entry.OpenPRBeforeMerge != nil {
			openPRValues[i] = *entry.OpenPRBeforeMerge
		}
	}
	if !allEqual(openPRValues) {
		mixed[FieldOpenPRBeforeMerge] = true
	}

	return mixed
}

// allEqual checks if all values in a slice are equal.
func allEqual[T comparable](values []T) bool {
	if len(values) == 0 {
		return true
	}
	first := values[0]
	for _, v := range values {
		if v != first {
			return false
		}
	}
	return true
}

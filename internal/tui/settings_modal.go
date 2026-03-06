package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SettingsTab represents the active tab in the settings modal
type SettingsTab int

const (
	TabLimits SettingsTab = iota
	TabGroups
	TabRuntime
)

// StatusGroups represents the status groups available in the TUI
var StatusGroups = []string{"Ready", "Waiting", "Active", "Blocked", "Draft", "Cancelled", "Completed", "Validated", "Superseded", "Archived"}

// SettingsModal allows editing project limits, global max parallel, group visibility, and runtime settings.
// Navigation: j/k to move up/down, tab to switch sections
// Adjustment: +/- to increase/decrease limits (Limits tab)
// Toggle: Space to toggle group visibility - controls whether groups are shown in the task list (Groups tab)
// Toggle: Space to toggle text wrap (Runtime tab)
// Direct navigation: 1 for Limits, 2 for Groups, 3 for Runtime
//
// Note: Group visibility (GroupVisible) is separate from collapse state (GroupCollapsed).
// Visibility controls filtering (whether the group appears at all).
// Collapse controls UI folding (whether an visible group is expanded or collapsed).
type SettingsModal struct {
	settings      Settings
	selectedIndex int         // 0 = global, 1..N = projects (Limits tab) or 0..N = groups (Groups tab) or 0..2 = runtime settings (Runtime tab)
	projects      []string    // sorted project list
	currentTab    SettingsTab // active tab
	editMode      bool        // true when editing the default model field
	editBuffer    string      // buffer for editing the default model
	saveError     error       // error from last save attempt
	saveSuccess   bool        // true if last save was successful
}

// settingsSavedMsg is sent when settings have been saved (successfully or with error)
type settingsSavedMsg struct {
	err error
}

// NewSettingsModal creates a new settings modal with the given settings.
// Projects are sorted alphabetically for consistent display.
func NewSettingsModal(settings Settings) *SettingsModal {
	// Extract and sort project names
	projects := make([]string, 0, len(settings.ProjectLimits))
	for proj := range settings.ProjectLimits {
		projects = append(projects, proj)
	}
	sort.Strings(projects)

	return &SettingsModal{
		settings:      settings,
		selectedIndex: 0,
		projects:      projects,
		currentTab:    TabLimits,
	}
}

// Init implements Modal
func (m *SettingsModal) Init() tea.Cmd {
	return nil
}

// Update implements Modal
func (m *SettingsModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Route keyboard input to HandleKey
		handled, cmd := m.HandleKey(msg.String())
		if handled {
			return m, cmd
		}

	case settingsSavedMsg:
		if msg.err != nil {
			m.saveError = msg.err
			m.saveSuccess = false
		} else {
			m.saveSuccess = true
			m.saveError = nil
		}
		return m, nil
	}

	return m, nil
}

// saveSettingsCmd returns a tea.Cmd that saves settings asynchronously
func (m *SettingsModal) saveSettingsCmd() tea.Cmd {
	settings := m.settings // Capture current state
	return func() tea.Msg {
		err := SaveSettings(settings)
		return settingsSavedMsg{err: err}
	}
}

// getMaxIndex returns the maximum valid index for the current tab
func (m *SettingsModal) getMaxIndex() int {
	switch m.currentTab {
	case TabLimits:
		return len(m.projects) // 0=global, 1..N=projects
	case TabGroups:
		return len(StatusGroups) - 1
	case TabRuntime:
		return 2 // 0=model, 1=wrap, 2=log
	}
	return 0
}

// View implements Modal
func (m *SettingsModal) View() string {
	var s strings.Builder

	// Show success/error at top (like MetadataModal)
	if m.saveSuccess {
		successStyle := lipgloss.NewStyle().Foreground(ColorReady).Bold(true)
		s.WriteString(successStyle.Render("✓ Settings saved"))
		s.WriteString("\n\n")
		m.saveSuccess = false // Clear after displaying
	}
	if m.saveError != nil {
		errorStyle := lipgloss.NewStyle().Foreground(ColorBlocked).Bold(true)
		s.WriteString(errorStyle.Render(fmt.Sprintf("✗ Error: %v", m.saveError)))
		s.WriteString("\n\n")
	}

	// Render tab header
	s.WriteString(m.renderTabHeader())
	s.WriteString("\n\n")

	// Render active tab content
	switch m.currentTab {
	case TabLimits:
		s.WriteString(m.renderLimitsTab())
	case TabGroups:
		s.WriteString(m.renderGroupsTab())
	case TabRuntime:
		s.WriteString(m.renderRuntimeTab())
	}

	return s.String()
}

// renderTabHeader renders the tab selection header
func (m *SettingsModal) renderTabHeader() string {
	var s strings.Builder

	// Limits tab
	if m.currentTab == TabLimits {
		s.WriteString("[Limits]")
	} else {
		s.WriteString(" Limits ")
	}
	s.WriteString("  ")

	// Groups tab
	if m.currentTab == TabGroups {
		s.WriteString("[Groups]")
	} else {
		s.WriteString(" Groups ")
	}
	s.WriteString("  ")

	// Runtime tab
	if m.currentTab == TabRuntime {
		s.WriteString("[Runtime]")
	} else {
		s.WriteString(" Runtime ")
	}

	return s.String()
}

// renderLimitsTab renders the Limits tab content
func (m *SettingsModal) renderLimitsTab() string {
	var s strings.Builder

	// Global max parallel section
	s.WriteString(m.renderGlobalLimit())
	s.WriteString("\n\n")

	// Project limits section
	s.WriteString("Project Limits:\n")
	s.WriteString(m.renderProjectLimits())

	return s.String()
}

// renderGroupsTab renders the Groups tab content
func (m *SettingsModal) renderGroupsTab() string {
	var s strings.Builder

	s.WriteString("Status Groups (☑ = visible, ☐ = hidden):\n")

	for i, group := range StatusGroups {
		cursor := m.getCursor(i)

		// Check if group is visible using GroupVisible map
		// Default to true if not explicitly set to false
		visible := m.settings.GroupVisible[group]
		checkbox := "☑"
		if !visible {
			checkbox = "☐"
		}

		s.WriteString(fmt.Sprintf("%s %s %s\n", cursor, checkbox, group))
	}

	s.WriteString("\nPress Space to toggle visibility, j/k to navigate")

	return s.String()
}

// renderRuntimeTab renders the Runtime tab content
func (m *SettingsModal) renderRuntimeTab() string {
	var s strings.Builder

	// Default Model setting (index 0)
	cursor0 := m.getCursorForRuntimeTab(0)
	modelDisplay := m.settings.DefaultModel
	if modelDisplay == "" {
		modelDisplay = "(none - uses task default)"
	}
	if m.editMode && m.selectedIndex == 0 {
		modelDisplay = m.editBuffer + "█" // Show cursor when editing
	}
	s.WriteString(fmt.Sprintf("%s Default Model: %s\n", cursor0, modelDisplay))

	// Text Wrapping setting (index 1)
	cursor1 := m.getCursorForRuntimeTab(1)
	wrapCheckbox := "☑"
	if !m.settings.TextWrap {
		wrapCheckbox = "☐"
	}
	s.WriteString(fmt.Sprintf("%s %s Text Wrapping\n", cursor1, wrapCheckbox))

	// Log Level setting (index 2)
	cursor2 := m.getCursorForRuntimeTab(2)
	s.WriteString(fmt.Sprintf("%s Log Level:\n", cursor2))

	// Log level radio buttons
	logLevels := []string{"error", "info", "debug"}
	for _, level := range logLevels {
		prefix := "   "
		if m.selectedIndex == 2 {
			prefix = "   " // Indent for radio buttons
		}
		radio := "○"
		if m.settings.LogLevel == level {
			radio = "●"
		}
		s.WriteString(fmt.Sprintf("%s %s %s\n", prefix, radio, level))
	}

	s.WriteString("\n")
	if m.editMode {
		s.WriteString("Type model name, Enter to save, Esc to cancel")
	} else {
		s.WriteString("Enter: Edit model  Space: Toggle wrap  j/k: Navigate")
	}

	return s.String()
}

// renderGlobalLimit renders the global max parallel setting line
func (m *SettingsModal) renderGlobalLimit() string {
	cursor := m.getCursorForLimitsTab(0)
	return fmt.Sprintf("%s Global Max Parallel: %d", cursor, m.settings.GlobalMaxParallel)
}

// renderProjectLimits renders the project limits list
func (m *SettingsModal) renderProjectLimits() string {
	var s strings.Builder
	for i, proj := range m.projects {
		cursor := m.getCursorForLimitsTab(i + 1)
		limitStr := m.formatLimit(m.settings.ProjectLimits[proj])
		s.WriteString(fmt.Sprintf("%s  %s: %s\n", cursor, proj, limitStr))
	}
	return s.String()
}

// getCursor returns ">" if index is selected, " " otherwise
func (m *SettingsModal) getCursor(index int) string {
	if m.selectedIndex == index {
		return ">"
	}
	return " "
}

// getCursorForLimitsTab returns cursor for Limits tab (only shows when on Limits tab)
func (m *SettingsModal) getCursorForLimitsTab(index int) string {
	if m.currentTab == TabLimits && m.selectedIndex == index {
		return ">"
	}
	return " "
}

// getCursorForRuntimeTab returns cursor for Runtime tab (only shows when on Runtime tab)
func (m *SettingsModal) getCursorForRuntimeTab(index int) string {
	if m.currentTab == TabRuntime && m.selectedIndex == index {
		return ">"
	}
	return " "
}

// formatLimit formats a limit value (0 = ∞, otherwise number)
func (m *SettingsModal) formatLimit(limit int) string {
	if limit == 0 {
		return "∞"
	}
	return fmt.Sprintf("%d", limit)
}

// HandleKey implements Modal
func (m *SettingsModal) HandleKey(key string) (bool, tea.Cmd) {
	// If in edit mode, handle text input differently
	if m.editMode {
		switch key {
		case "enter":
			// Save the edited model
			m.settings.DefaultModel = m.editBuffer
			m.editMode = false
			return true, m.saveSettingsCmd()
		case "esc":
			// Cancel editing
			m.editMode = false
			return true, nil
		case "backspace":
			if len(m.editBuffer) > 0 {
				m.editBuffer = m.editBuffer[:len(m.editBuffer)-1]
			}
			return true, nil
		default:
			// Append character to buffer (only printable characters)
			if len(key) == 1 && key[0] >= 32 && key[0] <= 126 {
				m.editBuffer += key
			}
			return true, nil
		}
	}

	switch key {
	case "tab":
		m.switchTab()
		return true, nil

	case "1":
		m.currentTab = TabLimits
		m.selectedIndex = 0
		return true, nil

	case "2":
		m.currentTab = TabGroups
		m.selectedIndex = 0
		return true, nil

	case "3":
		m.currentTab = TabRuntime
		m.selectedIndex = 0
		return true, nil

	case "j":
		m.moveDown()
		return true, nil

	case "k":
		m.moveUp()
		return true, nil

	case "enter":
		if m.currentTab == TabRuntime && m.selectedIndex == 0 {
			// Start editing the model field
			m.editMode = true
			m.editBuffer = m.settings.DefaultModel
			return true, nil
		}
		if m.currentTab == TabRuntime && m.selectedIndex == 2 {
			// Cycle log level when on log level field
			return true, m.cycleLogLevel()
		}
		return false, nil

	case " ":
		if m.currentTab == TabGroups {
			return true, m.toggleGroupVisibility()
		}
		if m.currentTab == TabRuntime && m.selectedIndex == 1 {
			return true, m.toggleTextWrap()
		}
		return false, nil

	case "+":
		if m.currentTab == TabLimits {
			m.increaseLimit()
			return true, nil
		}
		return false, nil

	case "-":
		if m.currentTab == TabLimits {
			m.decreaseLimit()
			return true, nil
		}
		return false, nil

	case "0":
		if m.currentTab == TabLimits {
			m.setUnlimited()
			return true, nil
		}
		return false, nil
	}

	return false, nil
}

// switchTab cycles to the next tab
func (m *SettingsModal) switchTab() {
	switch m.currentTab {
	case TabLimits:
		m.currentTab = TabGroups
	case TabGroups:
		m.currentTab = TabRuntime
	case TabRuntime:
		m.currentTab = TabLimits
	}
	m.selectedIndex = 0 // Reset selection when switching tabs
	m.editMode = false  // Exit edit mode when switching tabs

	// Ensure selectedIndex is valid for the new tab
	maxIndex := m.getMaxIndex()
	if m.selectedIndex > maxIndex {
		m.selectedIndex = 0
	}
}

// toggleTextWrap toggles the text wrapping setting and returns a save command
func (m *SettingsModal) toggleTextWrap() tea.Cmd {
	m.settings.TextWrap = !m.settings.TextWrap
	return m.saveSettingsCmd()
}

// cycleLogLevel cycles through log levels: error -> info -> debug -> error
func (m *SettingsModal) cycleLogLevel() tea.Cmd {
	switch m.settings.LogLevel {
	case "error":
		m.settings.LogLevel = "info"
	case "info":
		m.settings.LogLevel = "debug"
	case "debug":
		m.settings.LogLevel = "error"
	default:
		m.settings.LogLevel = "info"
	}
	return m.saveSettingsCmd()
}

// toggleGroupVisibility toggles the visibility of the selected group and returns a save command
func (m *SettingsModal) toggleGroupVisibility() tea.Cmd {
	if m.selectedIndex < 0 || m.selectedIndex >= len(StatusGroups) {
		return nil
	}

	group := StatusGroups[m.selectedIndex]
	// Toggle visibility: flip the GroupVisible value
	m.settings.GroupVisible[group] = !m.settings.GroupVisible[group]

	return m.saveSettingsCmd()
}

// moveDown moves selection down one item
func (m *SettingsModal) moveDown() {
	maxIndex := m.getMaxIndex()
	if m.selectedIndex < maxIndex {
		m.selectedIndex++
	}
}

// moveUp moves selection up one item
func (m *SettingsModal) moveUp() {
	if m.selectedIndex > 0 {
		m.selectedIndex--
	}
}

// increaseLimit increases the selected limit by 1 (Limits tab only)
func (m *SettingsModal) increaseLimit() {
	if m.currentTab != TabLimits {
		return
	}

	if m.selectedIndex == 0 {
		// Global max parallel
		m.settings.GlobalMaxParallel++
	} else {
		// Project limit
		proj := m.projects[m.selectedIndex-1]
		m.settings.ProjectLimits[proj]++
	}
}

// decreaseLimit decreases the selected limit by 1 (min 1 for global, min 0 for projects)
func (m *SettingsModal) decreaseLimit() {
	if m.currentTab != TabLimits {
		return
	}

	if m.selectedIndex == 0 {
		// Global max parallel - minimum 1
		if m.settings.GlobalMaxParallel > 1 {
			m.settings.GlobalMaxParallel--
		}
	} else {
		// Project limit - minimum 0
		proj := m.projects[m.selectedIndex-1]
		if m.settings.ProjectLimits[proj] > 0 {
			m.settings.ProjectLimits[proj]--
		}
	}
}

// setUnlimited sets the selected project limit to unlimited (0)
// Only applies to projects, not global setting
func (m *SettingsModal) setUnlimited() {
	if m.currentTab != TabLimits {
		return
	}

	if m.selectedIndex > 0 {
		proj := m.projects[m.selectedIndex-1]
		m.settings.ProjectLimits[proj] = 0
	}
}

// Title implements Modal
func (m *SettingsModal) Title() string {
	return "Settings"
}

// Width implements Modal
func (m *SettingsModal) Width() int {
	return 50
}

// Height implements Modal
func (m *SettingsModal) Height() int {
	switch m.currentTab {
	case TabLimits:
		// Global (1) + blank line (1) + header (1) + projects
		return 3 + len(m.projects)
	case TabGroups:
		// Header (1) + groups + blank line (1) + help text (1)
		return 3 + len(StatusGroups)
	case TabRuntime:
		// Model (1) + Wrap (1) + Log Level header (1) + 3 radio buttons + blank line (1) + help text (1)
		return 8
	default:
		return 3
	}
}

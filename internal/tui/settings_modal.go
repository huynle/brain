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
	TabMonitors
)

// StatusGroups represents the status groups available in the TUI.
// Terminal statuses (completed, validated, cancelled, superseded, archived) are
// consolidated under a single "Inactive" group.
var StatusGroups = []string{"Draft", "Pending", "Active", "In Progress", "Blocked", "Inactive"}

// statusGroupToAPIStatuses maps display group names to one or more API status values.
// Used for counting tasks per status group. "Inactive" maps to all terminal statuses.
var statusGroupToAPIStatuses = map[string][]string{
	"Draft":       {"draft"},
	"Pending":     {"pending"},
	"Active":      {"active"},
	"In Progress": {"in_progress"},
	"Blocked":     {"blocked"},
	"Inactive":    {"completed", "validated", "cancelled", "superseded", "archived"},
}

// SettingsModal allows editing project limits, global max parallel, group visibility, runtime settings, and monitors.
// Navigation: j/k to move up/down, tab to switch sections
// Adjustment: +/- to increase/decrease limits (Limits tab)
// Toggle: Space to toggle group visibility - controls whether groups are shown in the task list (Groups tab)
// Toggle: c to toggle collapse state - controls whether a visible group is expanded or collapsed (Groups tab)
// Toggle: Space to toggle text wrap (Runtime tab)
// Toggle: Space to toggle auto-create monitors (Monitors tab)
// Direct navigation: 1 for Limits, 2 for Groups, 3 for Runtime, 4 for Monitors
//
// Note: Group visibility (GroupVisible) is separate from collapse state (GroupCollapsed).
// Visibility controls filtering (whether the group appears at all).
// Collapse controls UI folding (whether a visible group is expanded or collapsed).
type SettingsModal struct {
	settings          Settings
	selectedIndex     int            // 0 = global, 1..N = projects (Limits tab) or 0..N = groups (Groups tab) or 0..2 = runtime settings (Runtime tab) or 0 = autoMonitors (Monitors tab)
	projects          []string       // sorted project list
	currentTab        SettingsTab    // active tab
	editMode          bool           // true when editing the default model field
	editBuffer        string         // buffer for editing the default model
	saveError         error          // error from last save attempt
	taskCounts        map[string]int // status group name -> task count (e.g., "Draft" -> 3)
	runningPerProject map[string]int // project ID -> number of in_progress tasks
}

// settingsSavedMsg is sent when settings have been saved (successfully or with error)
type settingsSavedMsg struct {
	err error
}

// SettingsModalOption is a functional option for configuring SettingsModal.
type SettingsModalOption func(*SettingsModal)

// WithTaskCounts sets the status group task counts (e.g., "Draft" -> 3).
func WithTaskCounts(counts map[string]int) SettingsModalOption {
	return func(m *SettingsModal) {
		m.taskCounts = counts
	}
}

// WithRunningPerProject sets the per-project running task counts.
func WithRunningPerProject(running map[string]int) SettingsModalOption {
	return func(m *SettingsModal) {
		m.runningPerProject = running
	}
}

// NewSettingsModal creates a new settings modal with the given settings and optional configuration.
// Projects are sorted alphabetically for consistent display.
// Accepts variadic options: use WithTaskCounts() and WithRunningPerProject() to provide data.
// For backward compatibility, also accepts a plain map[string]int as the first variadic arg (treated as taskCounts).
func NewSettingsModal(settings Settings, opts ...interface{}) *SettingsModal {
	// Extract and sort project names
	projects := make([]string, 0, len(settings.ProjectLimits))
	for proj := range settings.ProjectLimits {
		projects = append(projects, proj)
	}
	sort.Strings(projects)

	modal := &SettingsModal{
		settings:      settings,
		selectedIndex: 0,
		projects:      projects,
		currentTab:    TabLimits,
	}

	// Process variadic options
	for _, opt := range opts {
		switch v := opt.(type) {
		case SettingsModalOption:
			v(modal)
		case map[string]int:
			// Backward compatibility: plain map treated as taskCounts
			modal.taskCounts = v
		}
	}

	return modal
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
		} else {
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
	case TabMonitors:
		return 0 // single toggle: autoMonitors
	}
	return 0
}

// View implements Modal
func (m *SettingsModal) View() string {
	var s strings.Builder

	// Show save errors at top
	if m.saveError != nil {
		errorStyle := lipgloss.NewStyle().Foreground(ColorBlocked).Bold(true)
		s.WriteString(errorStyle.Render(fmt.Sprintf("✗ Error: %v", m.saveError)))
		s.WriteString("\n\n")
	}

	// Render tab header with indicators
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
	case TabMonitors:
		s.WriteString(m.renderMonitorsTab())
	}

	return s.String()
}

// renderTabHeader renders the tab selection header with visual styling
func (m *SettingsModal) renderTabHeader() string {
	var tabs []string
	activeStyle := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	inactiveStyle := lipgloss.NewStyle().Foreground(ColorDim)

	tabNames := []string{"Limits", "Groups", "Runtime", "Monitors"}
	for i, name := range tabNames {
		if SettingsTab(i) == m.currentTab {
			tabs = append(tabs, activeStyle.Render(fmt.Sprintf("[%s]", name)))
		} else {
			tabs = append(tabs, inactiveStyle.Render(name))
		}
	}

	return strings.Join(tabs, "  ")
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

	// Separator and help text at bottom
	s.WriteString("\n")
	dimStyle := lipgloss.NewStyle().Foreground(ColorDim)
	s.WriteString(dimStyle.Render("  ────────────────────────────────────────────"))
	s.WriteString("\n")
	helpStyle := lipgloss.NewStyle().Foreground(ColorDim).Italic(true)
	s.WriteString(helpStyle.Render("  j/k: navigate  +/=: increase  -: decrease  0: unlimited  tab/1-4: switch tabs"))

	return s.String()
}

// renderGroupsTab renders the Groups tab content with visibility checkboxes,
// collapse indicators (▸/▾), and task counts per status group.
func (m *SettingsModal) renderGroupsTab() string {
	var s strings.Builder

	s.WriteString("Status Groups (☑ = visible, ☐ = hidden):\n")

	dimStyle := lipgloss.NewStyle().Foreground(ColorDim)

	for i, group := range StatusGroups {
		cursor := m.getCursor(i)

		// Check if group is visible using GroupVisible map
		// Default to true if not explicitly set to false
		visible := m.settings.GroupVisible[group]
		checkbox := "☑"
		if !visible {
			checkbox = "☐"
		}

		// Collapse indicator: only shown when group is visible
		collapseIcon := " "
		if visible {
			collapsed := m.settings.GroupCollapsed[group]
			if collapsed {
				collapseIcon = "▸"
			} else {
				collapseIcon = "▾"
			}
		}

		// Task count
		count := 0
		if m.taskCounts != nil {
			count = m.taskCounts[group]
		}
		countStr := fmt.Sprintf("(%d)", count)

		// Dim the group name and count if not visible
		if visible {
			fmt.Fprintf(&s, "%s %s %s %-14s %s\n", cursor, checkbox, collapseIcon, group, countStr)
		} else {
			fmt.Fprintf(&s, "%s %s %s %s\n", cursor, checkbox, collapseIcon, dimStyle.Render(fmt.Sprintf("%-14s %s", group, countStr)))
		}
	}

	s.WriteString("\n")
	dimSepStyle := lipgloss.NewStyle().Foreground(ColorDim)
	s.WriteString(dimSepStyle.Render("  ────────────────────────────────────────────"))
	s.WriteString("\n")
	helpStyle := lipgloss.NewStyle().Foreground(ColorDim).Italic(true)
	s.WriteString(helpStyle.Render("  j/k: navigate  space: toggle visibility  c: collapse  tab/1-4: switch tabs"))

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
	fmt.Fprintf(&s, "%s Default Model: %s\n", cursor0, modelDisplay)

	// Text Wrapping setting (index 1)
	cursor1 := m.getCursorForRuntimeTab(1)
	wrapCheckbox := "☑"
	if !m.settings.TextWrap {
		wrapCheckbox = "☐"
	}
	fmt.Fprintf(&s, "%s %s Text Wrapping\n", cursor1, wrapCheckbox)

	// Log Level setting (index 2)
	cursor2 := m.getCursorForRuntimeTab(2)
	fmt.Fprintf(&s, "%s Log Level:\n", cursor2)

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
		fmt.Fprintf(&s, "%s %s %s\n", prefix, radio, level)
	}

	// Separator and mode-aware help text
	s.WriteString("\n")
	dimSepStyle := lipgloss.NewStyle().Foreground(ColorDim)
	s.WriteString(dimSepStyle.Render("  ────────────────────────────────────────────"))
	s.WriteString("\n")
	helpStyle := lipgloss.NewStyle().Foreground(ColorDim).Italic(true)
	var helpText string
	if m.editMode {
		helpText = "  enter: save  esc: cancel  backspace: delete  ctrl+u: clear"
	} else {
		helpText = "  j/k: navigate  enter: edit model  space: toggle  tab/1-4: switch tabs"
	}
	s.WriteString(helpStyle.Render(helpText))

	return s.String()
}

// renderMonitorsTab renders the Monitors tab content
func (m *SettingsModal) renderMonitorsTab() string {
	var s strings.Builder

	// Description
	descStyle := lipgloss.NewStyle().Foreground(ColorDim)
	s.WriteString(descStyle.Render("Auto-create monitors for new features"))
	s.WriteString("\n\n")

	// Auto-create monitors toggle (index 0)
	cursor := m.getCursorForMonitorsTab(0)
	if m.settings.AutoMonitors {
		onStyle := lipgloss.NewStyle().Foreground(ColorReady).Bold(true)
		fmt.Fprintf(&s, "%s Auto-create monitors: %s\n", cursor, onStyle.Render("[ON]"))
	} else {
		offStyle := lipgloss.NewStyle().Foreground(ColorDim).Bold(true)
		fmt.Fprintf(&s, "%s Auto-create monitors: %s\n", cursor, offStyle.Render("[OFF]"))
	}

	// Sub-description
	s.WriteString(descStyle.Render("  Creates Blocked Task Inspector and Feature Code Review"))
	s.WriteString("\n")
	s.WriteString(descStyle.Render("  for every new feature_id detected at runtime."))
	s.WriteString("\n")

	// Separator and help text
	s.WriteString("\n")
	dimSepStyle := lipgloss.NewStyle().Foreground(ColorDim)
	s.WriteString(dimSepStyle.Render("  ────────────────────────────────────────────"))
	s.WriteString("\n")
	helpStyle := lipgloss.NewStyle().Foreground(ColorDim).Italic(true)
	s.WriteString(helpStyle.Render("  j/k: navigate  space: toggle  tab/1-4: switch tabs"))

	return s.String()
}

// getCursorForMonitorsTab returns styled cursor for Monitors tab (only shows when on Monitors tab)
func (m *SettingsModal) getCursorForMonitorsTab(index int) string {
	if m.currentTab == TabMonitors && m.selectedIndex == index {
		cursorStyle := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
		return cursorStyle.Render(">")
	}
	return " "
}

// renderGlobalLimit renders the global max parallel setting line
func (m *SettingsModal) renderGlobalLimit() string {
	cursor := m.getCursorForLimitsTab(0)
	return fmt.Sprintf("%s Global Max Parallel: %d", cursor, m.settings.GlobalMaxParallel)
}

// renderProjectLimits renders the project limits list with running task counts.
// Format: "project-name [no limit] (0 running)" or "project-name [2] (1 running)"
func (m *SettingsModal) renderProjectLimits() string {
	var s strings.Builder
	for i, proj := range m.projects {
		cursor := m.getCursorForLimitsTab(i + 1)
		limitStr := m.formatLimit(m.settings.ProjectLimits[proj])
		running := 0
		if m.runningPerProject != nil {
			running = m.runningPerProject[proj]
		}
		dimStyle := lipgloss.NewStyle().Foreground(ColorDim)
		runningStr := dimStyle.Render(fmt.Sprintf("(%d running)", running))
		fmt.Fprintf(&s, "%s  %s %s %s\n", cursor, proj, limitStr, runningStr)
	}
	return s.String()
}

// getCursor returns styled ">" if index is selected, " " otherwise
func (m *SettingsModal) getCursor(index int) string {
	if m.selectedIndex == index {
		cursorStyle := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
		return cursorStyle.Render(">")
	}
	return " "
}

// getCursorForLimitsTab returns styled cursor for Limits tab (only shows when on Limits tab)
func (m *SettingsModal) getCursorForLimitsTab(index int) string {
	if m.currentTab == TabLimits && m.selectedIndex == index {
		cursorStyle := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
		return cursorStyle.Render(">")
	}
	return " "
}

// getCursorForRuntimeTab returns styled cursor for Runtime tab (only shows when on Runtime tab)
func (m *SettingsModal) getCursorForRuntimeTab(index int) string {
	if m.currentTab == TabRuntime && m.selectedIndex == index {
		cursorStyle := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
		return cursorStyle.Render(">")
	}
	return " "
}

// formatLimit formats a limit value for display.
// 0 = "[no limit]", otherwise "[N]" where N is the limit number.
func (m *SettingsModal) formatLimit(limit int) string {
	if limit == 0 {
		return "[no limit]"
	}
	return fmt.Sprintf("[%d]", limit)
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
		case "ctrl+u":
			// Clear entire line
			m.editBuffer = ""
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

	case "4":
		m.currentTab = TabMonitors
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
		if m.currentTab == TabMonitors && m.selectedIndex == 0 {
			return true, m.toggleAutoMonitors()
		}
		return false, nil

	case "c":
		if m.currentTab == TabGroups {
			return true, m.toggleGroupCollapse()
		}
		return false, nil

	case "+":
		if m.currentTab == TabLimits {
			m.increaseLimit()
			return true, m.saveSettingsCmd()
		}
		return false, nil

	case "-":
		if m.currentTab == TabLimits {
			m.decreaseLimit()
			return true, m.saveSettingsCmd()
		}
		return false, nil

	case "0":
		if m.currentTab == TabLimits {
			m.setUnlimited()
			return true, m.saveSettingsCmd()
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
		m.currentTab = TabMonitors
	case TabMonitors:
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

// toggleAutoMonitors toggles the auto monitors setting and returns a save command
func (m *SettingsModal) toggleAutoMonitors() tea.Cmd {
	m.settings.AutoMonitors = !m.settings.AutoMonitors
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

// toggleGroupCollapse toggles the collapse state of the selected group and returns a save command.
// Only works when the selected group is visible.
func (m *SettingsModal) toggleGroupCollapse() tea.Cmd {
	if m.selectedIndex < 0 || m.selectedIndex >= len(StatusGroups) {
		return nil
	}

	group := StatusGroups[m.selectedIndex]

	// Only toggle collapse for visible groups
	if !m.settings.GroupVisible[group] {
		return nil
	}

	// Toggle collapse state
	if m.settings.GroupCollapsed == nil {
		m.settings.GroupCollapsed = make(map[string]bool)
	}
	m.settings.GroupCollapsed[group] = !m.settings.GroupCollapsed[group]

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
	case TabMonitors:
		// Description (1) + blank line (1) + toggle (1) + sub-desc (2) + blank line (1) + help text (1)
		return 7
	default:
		return 3
	}
}

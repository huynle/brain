package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// SettingsTab represents the active tab in the settings modal
type SettingsTab int

const (
	TabLimits SettingsTab = iota
	TabGroups
)

// StatusGroups represents the status groups available in the TUI
var StatusGroups = []string{"Ready", "Waiting", "Active", "Blocked", "Completed"}

// SettingsModal allows editing project limits, global max parallel, and group visibility.
// Navigation: j/k to move up/down, tab to switch sections
// Adjustment: +/- to increase/decrease limits (Limits tab)
// Toggle: Space to toggle group visibility (Groups tab)
// Direct navigation: 1 for Limits, 2 for Groups
type SettingsModal struct {
	settings      Settings
	selectedIndex int         // 0 = global, 1..N = projects (Limits tab) or 0..N = groups (Groups tab)
	projects      []string    // sorted project list
	currentTab    SettingsTab // active tab
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
	return m, nil
}

// View implements Modal
func (m *SettingsModal) View() string {
	var s strings.Builder

	// Render tab header
	s.WriteString(m.renderTabHeader())
	s.WriteString("\n\n")

	// Render active tab content
	switch m.currentTab {
	case TabLimits:
		s.WriteString(m.renderLimitsTab())
	case TabGroups:
		s.WriteString(m.renderGroupsTab())
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

	s.WriteString("Status Groups:\n")
	
	for i, group := range StatusGroups {
		cursor := m.getCursor(i)
		
		// Check if group is visible (not in GroupCollapsed or explicitly false)
		visible := !m.settings.GroupCollapsed[group]
		checkbox := "☑"
		if !visible {
			checkbox = "☐"
		}
		
		s.WriteString(fmt.Sprintf("%s %s %s\n", cursor, checkbox, group))
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

// formatLimit formats a limit value (0 = ∞, otherwise number)
func (m *SettingsModal) formatLimit(limit int) string {
	if limit == 0 {
		return "∞"
	}
	return fmt.Sprintf("%d", limit)
}

// HandleKey implements Modal
func (m *SettingsModal) HandleKey(key string) (bool, tea.Cmd) {
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

	case "j":
		m.moveDown()
		return true, nil

	case "k":
		m.moveUp()
		return true, nil

	case " ":
		if m.currentTab == TabGroups {
			m.toggleGroupVisibility()
			return true, nil
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
	if m.currentTab == TabLimits {
		m.currentTab = TabGroups
	} else {
		m.currentTab = TabLimits
	}
	m.selectedIndex = 0 // Reset selection when switching tabs
}

// toggleGroupVisibility toggles the visibility of the selected group
func (m *SettingsModal) toggleGroupVisibility() {
	if m.selectedIndex < 0 || m.selectedIndex >= len(StatusGroups) {
		return
	}
	
	group := StatusGroups[m.selectedIndex]
	// Toggle: if currently visible (not in map or false), hide it (set to true)
	// if currently hidden (true), show it (set to false or remove)
	if m.settings.GroupCollapsed[group] {
		// Currently hidden, show it
		m.settings.GroupCollapsed[group] = false
	} else {
		// Currently visible, hide it
		m.settings.GroupCollapsed[group] = true
	}
	
	// Persist settings immediately
	_ = SaveSettings(m.settings) // Ignore errors (non-critical)
}

// moveDown moves selection down one item
func (m *SettingsModal) moveDown() {
	var maxIndex int
	
	switch m.currentTab {
	case TabLimits:
		maxIndex = len(m.projects) // 0 for global, 1..N for projects
	case TabGroups:
		maxIndex = len(StatusGroups) - 1
	}
	
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
		// Header (1) + groups
		return 1 + len(StatusGroups)
	default:
		return 3
	}
}

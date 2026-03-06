package tui

import (
	"fmt"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
)

// SettingsModal allows editing project limits and global max parallel.
// Navigation: j/k to move up/down
// Adjustment: +/- to increase/decrease limits
// Unlimited: 0 to set project limit to unlimited (displays as ∞)
type SettingsModal struct {
	settings      Settings
	selectedIndex int      // 0 = global, 1..N = projects
	projects      []string // sorted project list
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
	var s string

	// Global max parallel section
	s += m.renderGlobalLimit()
	s += "\n\n"

	// Project limits section
	s += "Project Limits:\n"
	s += m.renderProjectLimits()

	return s
}

// renderGlobalLimit renders the global max parallel setting line
func (m *SettingsModal) renderGlobalLimit() string {
	cursor := m.getCursor(0)
	return fmt.Sprintf("%s Global Max Parallel: %d", cursor, m.settings.GlobalMaxParallel)
}

// renderProjectLimits renders the project limits list
func (m *SettingsModal) renderProjectLimits() string {
	var s string
	for i, proj := range m.projects {
		cursor := m.getCursor(i + 1)
		limitStr := m.formatLimit(m.settings.ProjectLimits[proj])
		s += fmt.Sprintf("%s  %s: %s\n", cursor, proj, limitStr)
	}
	return s
}

// getCursor returns ">" if index is selected, " " otherwise
func (m *SettingsModal) getCursor(index int) string {
	if m.selectedIndex == index {
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
	case "j":
		m.moveDown()
		return true, nil

	case "k":
		m.moveUp()
		return true, nil

	case "+":
		m.increaseLimit()
		return true, nil

	case "-":
		m.decreaseLimit()
		return true, nil

	case "0":
		m.setUnlimited()
		return true, nil
	}

	return false, nil
}

// moveDown moves selection down one item
func (m *SettingsModal) moveDown() {
	maxIndex := len(m.projects) // 0 for global, 1..N for projects
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

// increaseLimit increases the selected limit by 1
func (m *SettingsModal) increaseLimit() {
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
	// Global (1) + blank line (1) + header (1) + projects
	return 3 + len(m.projects)
}

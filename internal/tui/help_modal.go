package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HelpModal displays keyboard shortcuts reference.
type HelpModal struct {
	isMultiProject bool
}

// NewHelpModal creates a new help modal.
func NewHelpModal(isMultiProject bool) *HelpModal {
	return &HelpModal{
		isMultiProject: isMultiProject,
	}
}

// Init implements Modal.
func (m *HelpModal) Init() tea.Cmd {
	return nil
}

// Update implements Modal.
func (m *HelpModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	return m, nil
}

// View implements Modal.
func (m *HelpModal) View() string {
	var b strings.Builder

	// Define styles for help content
	categoryStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorCyan).
		MarginTop(1)

	keyStyle := lipgloss.NewStyle().
		Foreground(ColorCyan).
		Bold(true).
		Width(12)

	descStyle := lipgloss.NewStyle().
		Foreground(ColorWhite)

	// Helper to format shortcut line
	formatShortcut := func(key, description string) string {
		return keyStyle.Render(key) + "  " + descStyle.Render(description)
	}

	// Navigation shortcuts
	b.WriteString(categoryStyle.Render("Navigation:"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("j/k", "Move selection up/down"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("g/G", "Jump to top/bottom"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("Enter", "Collapse/expand group"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("Tab", "Switch panel focus"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("/", "Filter tasks"))
	b.WriteString("\n")

	// Actions shortcuts
	b.WriteString(categoryStyle.Render("Actions:"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("s", "Edit task settings"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("S", "Global settings"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("x", "Execute task / toggle feature"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("c", "Complete task"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("X", "Cancel running task"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("C", "Schedule/task view toggle"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("e", "Edit in $EDITOR"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("d", "Delete task"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("p", "Pause/resume project"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("P", "Pause/resume all projects"))
	b.WriteString("\n")

	// Multi-select shortcuts
	b.WriteString(categoryStyle.Render("Multi-Select:"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("Space", "Toggle selection / collapse group"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("A", "Select all"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("D", "Deselect all"))
	b.WriteString("\n")

	// Views shortcuts
	b.WriteString(categoryStyle.Render("Views:"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("T", "Toggle task detail"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("h/l  [/]", "Switch content tab (Tasks/Brain/Automation/…)"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("z", "Toggle logs panel"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("w", "Toggle text wrap/truncate"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("R", "Show runners panel"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("r", "Refresh"))
	b.WriteString("\n")

	// Project navigation (only in multi-project mode)
	if m.isMultiProject {
		b.WriteString(categoryStyle.Render("Projects (Multi-Project Mode):"))
		b.WriteString("\n")
		b.WriteString(formatShortcut("H/L", "Previous/next project (shift)"))
		b.WriteString("\n")
		b.WriteString(formatShortcut("1-9", "Jump to project tab"))
		b.WriteString("\n")
	}

	// Automations tab — automation and goal row actions
	b.WriteString(categoryStyle.Render("Automations:"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("n", "New goal automation"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("s", "Edit automation metadata"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("e", "Edit automation / goal config"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("x", "Run automation / goal reconcile"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("Space", "Enable/disable automation"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("d", "Delete project automation only"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("ctrl+s", "Save config (in modal)"))
	b.WriteString("\n")

	// Help and quit
	b.WriteString(categoryStyle.Render("Other:"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("?", "Show this help"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("q", "Quit"))
	b.WriteString("\n")

	// Footer
	b.WriteString("\n")
	footerStyle := lipgloss.NewStyle().
		Foreground(ColorDim).
		Italic(true)
	b.WriteString(footerStyle.Render("Press ? or Esc to close"))

	return b.String()
}

// HandleKey implements Modal.
func (m *HelpModal) HandleKey(key string) (bool, tea.Cmd) {
	switch key {
	case "esc":
		// Let ModalManager handle close
		return false, nil
	default:
		// Consume all other keys to prevent passthrough
		return true, nil
	}
}

// Title implements Modal.
func (m *HelpModal) Title() string {
	return "Keyboard Shortcuts"
}

// Width implements Modal.
func (m *HelpModal) Width() int {
	// Fixed width for help content
	return 60
}

// Height implements Modal.
func (m *HelpModal) Height() int {
	// Calculate based on content:
	// Categories: Navigation (5), Actions (10), Multi-Select (3), Views (5-6),
	// Automations/Goal Rows (5), Other (2)
	// Plus category headers (6 or 7) and footer (2)
	viewLines := 6
	automationLines := 5
	// content lines + category headers (6) + footer (2)
	baseLines := 5 + 10 + 3 + viewLines + automationLines + 2 + 6 + 2

	// Add 2 more lines if multi-project mode (Projects section: H/L + 1-9)
	if m.isMultiProject {
		return baseLines + 2 + 1 // +1 for category header
	}

	return baseLines
}

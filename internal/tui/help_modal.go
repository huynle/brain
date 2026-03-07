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
	b.WriteString(formatShortcut("Tab", "Switch panel focus"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("/", "Filter tasks"))
	b.WriteString("\n")

	// Actions shortcuts
	b.WriteString(categoryStyle.Render("Actions:"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("s", "Set metadata (single)"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("S", "Global settings"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("x", "Execute task"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("c", "Complete task"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("C", "Cancel task"))
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
	b.WriteString(formatShortcut("Space", "Toggle selection"))
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
	b.WriteString(formatShortcut("L", "Toggle logs"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("w", "Toggle text wrap/truncate"))
	b.WriteString("\n")
	b.WriteString(formatShortcut("r", "Refresh"))
	b.WriteString("\n")

	// Project navigation (only in multi-project mode)
	if m.isMultiProject {
		b.WriteString(categoryStyle.Render("Projects (Multi-Project Mode):"))
		b.WriteString("\n")
		b.WriteString(formatShortcut("h/l", "Previous/next project"))
		b.WriteString("\n")
		b.WriteString(formatShortcut("[/]", "Previous/next project"))
		b.WriteString("\n")
		b.WriteString(formatShortcut("1-9", "Jump to project tab"))
		b.WriteString("\n")
	}

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
	case "?", "q":
		// Close modal on '?' or 'q'
		return true, nil
	default:
		// Consume other keys to prevent passthrough
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
	// Categories: Navigation (5), Actions (9), Multi-Select (3), Views (4), Other (2)
	// Plus category headers (5 or 6) and footer (2)
	baseLines := 5 + 9 + 3 + 4 + 2 + 5 + 2

	// Add 3 more lines if multi-project mode (Projects section)
	if m.isMultiProject {
		return baseLines + 3 + 1 // +1 for category header
	}

	return baseLines
}

package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// HelpBar displays keyboard shortcuts at the bottom of the TUI.
type HelpBar struct {
	ActivePanel Panel
}

// NewHelpBar creates a new HelpBar.
func NewHelpBar() HelpBar {
	return HelpBar{}
}

// View renders the help bar showing context-aware keyboard shortcuts.
// isMultiProject controls whether tab-switching shortcuts are shown.
func (h HelpBar) View(width int, isMultiProject bool) string {
	bold := BoldStyle.Render
	dim := DimStyle.Render

	var shortcuts string

	// Multi-project tab shortcuts
	if isMultiProject {
		shortcuts += fmt.Sprintf("%s Tabs  ", bold("h/l"))
	}

	// Common shortcuts
	shortcuts += fmt.Sprintf("%s Navigate  ", bold("j/k"))
	shortcuts += fmt.Sprintf("%s Top/Bottom  ", bold("g/G"))
	shortcuts += fmt.Sprintf("%s Panel  ", bold("Tab"))
	shortcuts += fmt.Sprintf("%s Detail  ", bold("T"))
	shortcuts += fmt.Sprintf("%s Logs  ", bold("L"))
	shortcuts += fmt.Sprintf("%s Refresh  ", bold("r"))

	// Task panel specific shortcuts
	if h.ActivePanel == PanelTasks {
		shortcuts += fmt.Sprintf("%s Execute  ", bold("x"))
		shortcuts += fmt.Sprintf("%s Edit  ", bold("e"))
		shortcuts += fmt.Sprintf("%s Complete  ", bold("c"))
		shortcuts += fmt.Sprintf("%s Cancel  ", bold("C"))
		shortcuts += fmt.Sprintf("%s Delete  ", bold("d"))
		shortcuts += fmt.Sprintf("%s Metadata  ", bold("s"))
		shortcuts += fmt.Sprintf("%s Filter  ", bold("/"))
		shortcuts += fmt.Sprintf("%s Settings  ", bold("S"))
	}

	shortcuts += fmt.Sprintf("%s Quit", bold("Ctrl-C"))

	// Focus indicator on the right
	focusLabel := ""
	if h.ActivePanel.String() != "unknown" {
		focusLabel = dim(fmt.Sprintf("Focus: ")) +
			lipgloss.NewStyle().Foreground(ColorCyan).Render(h.ActivePanel.String())
	}

	// Layout: shortcuts on left, focus on right
	leftStyle := lipgloss.NewStyle().
		PaddingLeft(1).
		Width(width - 20)

	rightStyle := lipgloss.NewStyle().
		Align(lipgloss.Right).
		Width(18)

	return lipgloss.JoinHorizontal(lipgloss.Top,
		leftStyle.Render(dim(shortcuts)),
		rightStyle.Render(focusLabel),
	)
}

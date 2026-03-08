package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// HelpBar displays keyboard shortcuts at the bottom of the TUI.
type HelpBar struct {
	ActivePanel     Panel
	ViewMode        ViewMode
	TextWrap        bool
	IsPaused        bool
	AllPaused       bool
	HasTaskSessions bool // whether selected task has sessions (shows o/O shortcuts)
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
	shortcuts += fmt.Sprintf("%s Collapse  ", bold("Enter"))
	shortcuts += fmt.Sprintf("%s Panel  ", bold("Tab"))
	shortcuts += fmt.Sprintf("%s Detail  ", bold("T"))
	shortcuts += fmt.Sprintf("%s Logs  ", bold("L"))
	shortcuts += fmt.Sprintf("%s Refresh  ", bold("r"))

	// Panel-specific shortcuts (view mode aware)
	if h.ActivePanel == PanelLogs {
		shortcuts += fmt.Sprintf("%s Filter  ", bold("f"))
	}
	if h.ActivePanel == PanelTasks {
		if h.ViewMode == ViewModeSchedules {
			// Schedule view shortcuts
			shortcuts += fmt.Sprintf("%s Tasks  ", bold("C"))
		} else {
			// Task view shortcuts
			shortcuts += fmt.Sprintf("%s Execute  ", bold("x"))
			shortcuts += fmt.Sprintf("%s Edit  ", bold("e"))
			shortcuts += fmt.Sprintf("%s Complete  ", bold("c"))
			shortcuts += fmt.Sprintf("%s Cancel  ", bold("X"))
			shortcuts += fmt.Sprintf("%s Delete  ", bold("d"))
			shortcuts += fmt.Sprintf("%s Metadata  ", bold("s"))
			shortcuts += fmt.Sprintf("%s Yank  ", bold("y"))
			shortcuts += fmt.Sprintf("%s Schedules  ", bold("C"))
			shortcuts += fmt.Sprintf("%s Filter  ", bold("/"))
			shortcuts += fmt.Sprintf("%s Settings  ", bold("S"))
			shortcuts += fmt.Sprintf("%s Pause  ", bold("p"))
			if h.HasTaskSessions {
				shortcuts += fmt.Sprintf("%s Session  ", bold("o"))
				shortcuts += fmt.Sprintf("%s Tmux  ", bold("O"))
			}
		}
	}

	// Pause indicator (shown regardless of active panel)
	if h.AllPaused {
		shortcuts += BoldStyle.Foreground(ColorWaiting).Render("⏸ ALL PAUSED") + "  "
	} else if h.IsPaused {
		shortcuts += BoldStyle.Foreground(ColorWaiting).Render("⏸ PAUSED") + "  "
	}

	if h.TextWrap {
		shortcuts += fmt.Sprintf("%s Wrap  ", bold("w"))
	} else {
		shortcuts += fmt.Sprintf("%s Trunc  ", bold("w"))
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

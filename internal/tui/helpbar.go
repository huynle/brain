package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// HelpBar displays keyboard shortcuts at the bottom of the TUI.
type HelpBar struct {
	ActivePanel        Panel
	ViewMode           ViewMode
	TextWrap           bool
	IsPaused           bool
	AllPaused          bool
	HasTaskSessions    bool       // whether selected task has sessions (shows o/O shortcuts)
	HasSelectedTasks   bool       // whether tasks are currently selected (shows delete shortcut)
	ActiveContentTab   ContentTab // which content tab is active (Tasks/Dream)
	RunnerPanelVisible bool       // whether the runner panel is shown
}

// NewHelpBar creates a new HelpBar.
func NewHelpBar() HelpBar {
	return HelpBar{}
}

// View renders the help bar showing context-aware keyboard shortcuts.
// isMultiProject controls whether tab-switching shortcuts are shown.
// projectName is the current project name for contextual display (single-project mode).
func (h HelpBar) View(width int, isMultiProject bool, projectName string) string {
	bold := BoldStyle.Render
	dim := DimStyle.Render

	var shortcuts string

	// Content tab indicator
	shortcuts += fmt.Sprintf("%s Tab  ", bold("H/L"))

	// Dream tab has vim-style navigation help
	if h.ActiveContentTab == ContentTabDream {
		shortcuts += fmt.Sprintf("%s Scroll  ", bold("j/k"))
		shortcuts += fmt.Sprintf("%s Page  ", bold("ctrl+d/u"))
		shortcuts += fmt.Sprintf("%s Top/Bot  ", bold("g/G"))
		shortcuts += fmt.Sprintf("%s Search  ", bold("/"))
		shortcuts += fmt.Sprintf("%s Tabs  ", bold("H/L"))
		shortcuts += fmt.Sprintf("%s Refresh  ", bold("r"))
		shortcuts += fmt.Sprintf("%s Quit", bold("q"))

		// Focus indicator on the right
		focusLabel := dim("Tab: ") +
			lipgloss.NewStyle().Foreground(ColorCyan).Render("Dream")

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

	// Runners tab has navigation help
	if h.ActiveContentTab == ContentTabRunners {
		shortcuts += fmt.Sprintf("%s Navigate  ", bold("j/k"))
		shortcuts += fmt.Sprintf("%s Top/Bot  ", bold("g/G"))
		shortcuts += fmt.Sprintf("%s Assign  ", bold("a"))
		shortcuts += fmt.Sprintf("%s Shutdown  ", bold("s"))
		shortcuts += fmt.Sprintf("%s Tabs  ", bold("H/L"))
		shortcuts += fmt.Sprintf("%s Refresh  ", bold("r"))
		shortcuts += fmt.Sprintf("%s Quit", bold("q"))

		// Focus indicator on the right
		focusLabel := dim("Tab: ") +
			lipgloss.NewStyle().Foreground(ColorCyan).Render("Runners")

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

	// Logs tab has log navigation help
	if h.ActiveContentTab == ContentTabLogs {
		shortcuts += fmt.Sprintf("%s Scroll  ", bold("j/k"))
		shortcuts += fmt.Sprintf("%s Top/Bot  ", bold("g/G"))
		shortcuts += fmt.Sprintf("%s Filter  ", bold("f"))
		shortcuts += fmt.Sprintf("%s Tabs  ", bold("H/L"))
		shortcuts += fmt.Sprintf("%s Refresh  ", bold("r"))
		shortcuts += fmt.Sprintf("%s Quit", bold("q"))

		focusLabel := dim("Tab: ") +
			lipgloss.NewStyle().Foreground(ColorCyan).Render("Logs")

		leftStyle := lipgloss.NewStyle().PaddingLeft(1).Width(width - 20)
		rightStyle := lipgloss.NewStyle().Align(lipgloss.Right).Width(18)

		return lipgloss.JoinHorizontal(lipgloss.Top,
			leftStyle.Render(dim(shortcuts)),
			rightStyle.Render(focusLabel),
		)
	}

	// Multi-project tab shortcuts (matches TypeScript: h/l/[/]/1-9)
	if isMultiProject {
		shortcuts += fmt.Sprintf("%s Tabs  ", bold("h/l/[/]/1-9"))
	}

	// j/k Navigate/Scroll
	navLabel := "Navigate"
	if h.ActivePanel == PanelLogs || h.ActivePanel == PanelDetails {
		navLabel = "Scroll"
	}
	shortcuts += fmt.Sprintf("%s %s  ", bold("j/k"), navLabel)

	// g/G Top/Bottom
	shortcuts += fmt.Sprintf("%s Top/Bottom  ", bold("g/G"))

	// Panel-specific shortcuts (view mode aware)
	if h.ActivePanel == PanelRunners {
		shortcuts += fmt.Sprintf("%s Navigate  ", bold("j/k"))
		shortcuts += fmt.Sprintf("%s Info  ", bold("i"))
		shortcuts += fmt.Sprintf("%s Assign  ", bold("a"))
		shortcuts += fmt.Sprintf("%s Shutdown  ", bold("s"))
	} else if h.ActivePanel == PanelLogs {
		shortcuts += fmt.Sprintf("%s Filter  ", bold("f"))
	} else if h.ActivePanel == PanelDetails {
		if h.ViewMode == ViewModeSchedules {
			shortcuts += fmt.Sprintf("%s Scroll  ", bold("j/k"))
		} else {
			shortcuts += fmt.Sprintf("%s Dependencies  ", bold("d"))
		}
	} else if h.ActivePanel == PanelTasks {
		if h.ViewMode == ViewModeSchedules {
			// Schedule view shortcuts
			shortcuts += fmt.Sprintf("%s Toggle  ", bold("d"))
			shortcuts += fmt.Sprintf("%s Details  ", bold("Enter"))
		} else {
			// Task view shortcuts
			shortcuts += fmt.Sprintf("%s Filter  ", bold("/"))
			shortcuts += fmt.Sprintf("%s Select  ", bold("Space"))
			if h.HasSelectedTasks {
				shortcuts += fmt.Sprintf("%s Delete  ", bold("⌫"))
			}
			shortcuts += fmt.Sprintf("%s Settings  ", bold("s"))
			shortcuts += fmt.Sprintf("%s Edit  ", bold("e"))
			if h.HasTaskSessions {
				shortcuts += fmt.Sprintf("%s Session  ", bold("o"))
				shortcuts += fmt.Sprintf("%s Tmux  ", bold("O"))
			}
			shortcuts += fmt.Sprintf("%s Yank  ", bold("y"))
			shortcuts += fmt.Sprintf("%s Execute  ", bold("x"))
			shortcuts += fmt.Sprintf("%s Checkout  ", bold("f"))
			shortcuts += fmt.Sprintf("%s Cancel  ", bold("X"))
		}
	}

	// Pause (multi-project shows p/P, single-project shows p with project name)
	if isMultiProject {
		shortcuts += fmt.Sprintf("%s Pause (project/all)  ", bold("p/P"))
	} else {
		if projectName != "" {
			shortcuts += fmt.Sprintf("%s Pause (%s)  ", bold("p"), projectName)
		} else {
			shortcuts += fmt.Sprintf("%s Pause (project)  ", bold("p"))
		}
	}

	// w Wrap/Trunc
	if h.TextWrap {
		shortcuts += fmt.Sprintf("%s Wrap  ", bold("w"))
	} else {
		shortcuts += fmt.Sprintf("%s Trunc  ", bold("w"))
	}

	// S Settings
	shortcuts += fmt.Sprintf("%s Settings  ", bold("S"))

	// C View
	shortcuts += fmt.Sprintf("%s View  ", bold("C"))

	// Tab Panel
	shortcuts += fmt.Sprintf("%s Panel  ", bold("Tab"))

	// l Logs
	shortcuts += fmt.Sprintf("%s Logs  ", bold("l"))

	// T Detail
	shortcuts += fmt.Sprintf("%s Detail  ", bold("T"))

	// R Runners
	shortcuts += fmt.Sprintf("%s Runners  ", bold("R"))

	// r Refresh
	shortcuts += fmt.Sprintf("%s Refresh  ", bold("r"))

	// Ctrl-C Quit
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

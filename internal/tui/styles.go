package tui

import "github.com/charmbracelet/lipgloss"

// =============================================================================
// Colors
// =============================================================================

// Status colors match the React TUI's color scheme.
var (
	ColorReady     = lipgloss.Color("2")  // green
	ColorWaiting   = lipgloss.Color("3")  // yellow
	ColorActive    = lipgloss.Color("4")  // blue
	ColorBlocked   = lipgloss.Color("1")  // red
	ColorCompleted = lipgloss.Color("8")  // gray/dim
	ColorCyan      = lipgloss.Color("6")  // cyan
	ColorMagenta   = lipgloss.Color("5")  // magenta
	ColorWhite     = lipgloss.Color("15") // white
	ColorDim       = lipgloss.Color("8")  // dim gray
)

// Priority colors.
var (
	ColorPriorityHigh   = lipgloss.Color("1") // red
	ColorPriorityMedium = lipgloss.Color("3") // yellow
	ColorPriorityLow    = lipgloss.Color("8") // gray
)

// =============================================================================
// Status Indicators (matching React TUI)
// =============================================================================

const (
	IndicatorReady     = "●" // green filled circle
	IndicatorWaiting   = "○" // yellow empty circle
	IndicatorActive    = "▶" // blue play
	IndicatorCompleted = "✓" // green dim check
	IndicatorBlocked   = "✗" // red x
	IndicatorConnected = "●" // green dot
	IndicatorDisconn   = "○" // red dot
)

// =============================================================================
// Border Styles
// =============================================================================

// ActiveBorder is used for the currently focused panel.
var ActiveBorder = lipgloss.NewStyle().
	BorderStyle(lipgloss.RoundedBorder()).
	BorderForeground(ColorCyan)

// InactiveBorder is used for unfocused panels.
var InactiveBorder = lipgloss.NewStyle().
	BorderStyle(lipgloss.RoundedBorder()).
	BorderForeground(ColorDim)

// =============================================================================
// Text Styles
// =============================================================================

// TitleStyle is used for panel titles.
var TitleStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(ColorCyan)

// DimStyle is used for secondary/help text.
var DimStyle = lipgloss.NewStyle().
	Foreground(ColorDim)

// BoldStyle is used for keyboard shortcut keys in help bar.
var BoldStyle = lipgloss.NewStyle().Bold(true)

// GroupHeaderStyle is used for collapsible group headers.
var GroupHeaderStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(ColorCyan)

// SelectedTaskStyle is used for tasks that are selected (but not focused).
var SelectedTaskStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("39")). // Blue highlight
	Bold(true)

// SelectedCountStyle is used for the selection count in status bar.
var SelectedCountStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("39")).
	Bold(true)

// =============================================================================
// Status Styles
// =============================================================================

// StatusStyle returns a styled string for a task classification.
func StatusStyle(classification string) lipgloss.Style {
	switch classification {
	case "ready":
		return lipgloss.NewStyle().Foreground(ColorReady)
	case "waiting":
		return lipgloss.NewStyle().Foreground(ColorWaiting)
	case "blocked":
		return lipgloss.NewStyle().Foreground(ColorBlocked)
	default:
		return lipgloss.NewStyle().Foreground(ColorCompleted)
	}
}

// StatusStyleWithState returns a styled string for a task, prioritizing status over classification.
// Status (execution state) takes precedence: in_progress (blue), completed (gray), cancelled (red)
// Classification (dependency state) is used for pending tasks: ready (green), waiting (yellow), blocked (red)
func StatusStyleWithState(status, classification string) lipgloss.Style {
	// Prioritize status (execution state)
	switch status {
	case "in_progress":
		return lipgloss.NewStyle().Foreground(ColorActive)
	case "completed":
		return lipgloss.NewStyle().Foreground(ColorCompleted)
	case "cancelled":
		return lipgloss.NewStyle().Foreground(ColorBlocked)
	}

	// Fall back to classification (dependency state)
	return StatusStyle(classification)
}

// PriorityStyle returns a styled string for a priority level.
func PriorityStyle(priority string) lipgloss.Style {
	switch priority {
	case "high":
		return lipgloss.NewStyle().Foreground(ColorPriorityHigh)
	case "medium":
		return lipgloss.NewStyle().Foreground(ColorPriorityMedium)
	default:
		return lipgloss.NewStyle().Foreground(ColorPriorityLow)
	}
}

// =============================================================================
// Filter Styles
// =============================================================================

// FilterTypingStyle is used for the filter input bar when actively typing (yellow bg).
var FilterTypingStyle = lipgloss.NewStyle().
	Background(lipgloss.Color("3")). // yellow
	Foreground(lipgloss.Color("0")). // black text on yellow
	Padding(0, 0)

// FilterLockedStyle is used for the locked filter badge (cyan bg).
var FilterLockedStyle = lipgloss.NewStyle().
	Background(lipgloss.Color("6")). // cyan
	Foreground(lipgloss.Color("0")). // black text on cyan
	Padding(0, 0)

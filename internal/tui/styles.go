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

// FilterBarStyle is used for the filter input bar at the bottom.
var FilterBarStyle = lipgloss.NewStyle().
	Background(lipgloss.Color("240")).
	Foreground(lipgloss.Color("255")).
	Padding(0, 1)

// FilterStatusStyle is used for the filter status line (e.g., "Filtered: 5/24 tasks").
var FilterStatusStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("243")).
	Italic(true)

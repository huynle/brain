package tui

import "github.com/charmbracelet/lipgloss"

// =============================================================================
// Colors
// =============================================================================

// Status colors match the React TUI's color scheme.
var (
	ColorReady     = lipgloss.Color("2")  // green
	ColorWaiting   = lipgloss.Color("3")  // yellow
	ColorActive    = lipgloss.Color("6")  // cyan
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
	IndicatorCancelled = "⊘" // magenta slash
	IndicatorConnected = "●" // green dot
	IndicatorDisconn   = "○" // red dot
)

// =============================================================================
// Border Styles
// =============================================================================

// ActiveBorder is used for the currently focused panel.
var ActiveBorder = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(ColorCyan)

// InactiveBorder is used for unfocused panels.
var InactiveBorder = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
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
	Foreground(ColorCyan).
	Background(lipgloss.Color("#1a2a3a")). // Dark blue background for feature headers
	Padding(0, 1)                          // Padding for background visibility

// DraftHeaderStyle is used for Draft status group headers (gray color).
var DraftHeaderStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("#aaaaaa")). // Gray (matches TypeScript)
	Padding(0, 1)

// CompletedHeaderStyle is used for Completed status group headers (green color).
var CompletedHeaderStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("#66cc66")). // Green (matches TypeScript)
	Padding(0, 1)

// SelectedTaskStyle is used for tasks that are selected (but not focused).
var SelectedTaskStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("39")). // Blue highlight
	Bold(true)

// SelectedCountStyle is used for the selection count in status bar.
var SelectedCountStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("39")).
	Bold(true)

// SelectedRowStyle is used for the currently selected task/feature (blue background).
var SelectedRowStyle = lipgloss.NewStyle().
	Background(lipgloss.Color("4")). // blue background
	Foreground(ColorWhite).          // white text
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

// StatusStyleWithState returns a styled string for a task.
// For pending tasks, classification (dependency state) takes precedence to show readiness.
// For other statuses, status (execution state) takes precedence.
func StatusStyleWithState(status, classification string) lipgloss.Style {
	// Priority 1: For pending tasks, check classification first to show readiness
	if status == "pending" {
		switch classification {
		case "ready":
			return lipgloss.NewStyle().Foreground(ColorReady) // green - ready to execute
		case "waiting":
			return lipgloss.NewStyle().Foreground(ColorWaiting) // yellow - waiting on dependencies
		case "blocked":
			return lipgloss.NewStyle().Foreground(ColorBlocked) // red - blocked by deps
		default:
			// pending with unknown classification defaults to waiting
			return lipgloss.NewStyle().Foreground(ColorWaiting) // yellow
		}
	}

	// Priority 2: Handle all other explicit status values
	switch status {
	case "draft":
		return lipgloss.NewStyle().Foreground(ColorDim) // gray
	case "active":
		return lipgloss.NewStyle().Foreground(ColorActive) // blue
	case "in_progress":
		return lipgloss.NewStyle().Foreground(ColorActive) // cyan
	case "blocked":
		return lipgloss.NewStyle().Foreground(ColorBlocked) // red
	case "cancelled":
		return lipgloss.NewStyle().Foreground(ColorMagenta) // magenta
	case "completed":
		return lipgloss.NewStyle().Foreground(ColorCompleted) // green dim
	case "validated":
		return lipgloss.NewStyle().Foreground(ColorReady) // green bright
	case "superseded":
		return lipgloss.NewStyle().Foreground(ColorDim) // gray
	case "archived":
		return lipgloss.NewStyle().Foreground(ColorDim) // gray
	}

	// Priority 3: Fall back to classification (dependency state)
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

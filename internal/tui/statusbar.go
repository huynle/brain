package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// StatusBar displays project name, task stats, and connection status.
type StatusBar struct {
	Project             string
	Connected           bool
	Stats               TaskStats
	SelectedCount       int
	Metrics             *ResourceMetrics
	IsPaused            bool
	EnabledFeatureCount int
	ActiveFeatureCount  int
}

// NewStatusBar creates a new StatusBar for the given project.
func NewStatusBar(project string) StatusBar {
	return StatusBar{Project: project}
}

// View renders the status bar as a styled multi-row string.
// Row 1: Project name, indicators, task stats, connection indicator
// Row 2: System metrics (CPU/Mem/Proc)
func (s StatusBar) View(width int) string {
	if width < 20 {
		width = 20
	}

	firstRow := s.renderFirstRow(width)

	// Use a border style for the status bar
	barStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(ColorCyan).
		Width(width-2).
		Padding(0, 1) // 0 vertical padding, 1 horizontal padding

	// Always include second row for metrics display (matches origin/main TS TUI)
	// Shows "CPU:0.0% Mem:0.0MB 0 procs" when no processes are tracked
	content := firstRow
	if s.Metrics != nil {
		secondRow := s.renderSecondRow(width)
		content = firstRow + "\n" + secondRow
	}
	rendered := barStyle.Render(content)

	return rendered
}

// renderFirstRow renders the first row: project name, indicators, task stats, connection indicator
func (s StatusBar) renderFirstRow(width int) string {
	// Left side: project name
	projectName := TitleStyle.Render(s.Project)

	// Pause and feature indicators (before stats, matching TS StatusBar)
	indicators := ""
	if s.ActiveFeatureCount > 0 {
		// Active features take priority over pause indicator
		indicators += lipgloss.NewStyle().Foreground(ColorMagenta).Bold(true).
			Render(fmt.Sprintf("▶%d", s.ActiveFeatureCount)) + " "
	} else if s.IsPaused {
		// Show pause symbol when no active features (gray, non-bold to match TS)
		indicators += lipgloss.NewStyle().Foreground(lipgloss.Color("8")).
			Render("⏸")
		if s.EnabledFeatureCount > 0 {
			suffix := "s"
			if s.EnabledFeatureCount == 1 {
				suffix = ""
			}
			indicators += lipgloss.NewStyle().Foreground(ColorReady).
				Render(fmt.Sprintf(" [%d feature%s enabled]", s.EnabledFeatureCount, suffix))
		}
		indicators += " "
	}

	// Middle: task stats
	stats := fmt.Sprintf(
		"%s %d ready  %s %d waiting  %s %d active  %s %d inactive",
		lipgloss.NewStyle().Foreground(ColorReady).Render(IndicatorReady),
		s.Stats.Ready,
		lipgloss.NewStyle().Foreground(ColorWaiting).Render(IndicatorWaiting),
		s.Stats.Waiting,
		lipgloss.NewStyle().Foreground(ColorActive).Render(IndicatorActive),
		s.Stats.InProgress,
		lipgloss.NewStyle().Foreground(ColorDim).Render(IndicatorCompleted),
		s.Stats.Completed,
	)

	// Add blocked count if > 0
	if s.Stats.Blocked > 0 {
		stats += fmt.Sprintf("  %s %d blocked",
			lipgloss.NewStyle().Foreground(ColorBlocked).Render(IndicatorBlocked),
			s.Stats.Blocked,
		)
	}

	// Add selected count if > 0
	if s.SelectedCount > 0 {
		stats += SelectedCountStyle.Render(fmt.Sprintf("  • %d selected", s.SelectedCount))
	}

	// Right side: connection indicator
	connDot := lipgloss.NewStyle().Foreground(ColorBlocked).Render(IndicatorDisconn)
	if s.Connected {
		connDot = lipgloss.NewStyle().Foreground(ColorReady).Render(IndicatorConnected)
	}

	// Compose first row
	leftContent := projectName + "  " + indicators + stats

	// Place left and right content with space between
	innerWidth := width - 6 // account for border + padding
	if innerWidth < 10 {
		innerWidth = 10
	}

	leftStyle := lipgloss.NewStyle().Width(innerWidth - 2)
	rightStyle := lipgloss.NewStyle().Align(lipgloss.Right).Width(2)

	row := lipgloss.JoinHorizontal(lipgloss.Top,
		leftStyle.Render(leftContent),
		rightStyle.Render(connDot),
	)

	return row
}

// renderSecondRow renders the second row: system metrics (CPU/Mem/Proc).
// Only called when Metrics is non-nil and has active processes.
func (s StatusBar) renderSecondRow(width int) string {
	return lipgloss.NewStyle().
		Foreground(ColorReady).
		Render(s.Metrics.Format())
}

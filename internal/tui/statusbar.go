package tui

import (
	"fmt"
	"strings"

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
	secondRow := s.renderSecondRow(width)

	// Use a border style for the status bar
	barStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorCyan).
		Width(width - 2).
		PaddingLeft(1).
		PaddingRight(1)

	// Combine both rows with border
	content := firstRow + "\n" + secondRow
	rendered := barStyle.Render(content)

	// CRITICAL: Ensure exactly 4 lines (2 content + 2 border lines)
	// This matches the TypeScript TUI multi-row behavior
	lineCount := strings.Count(rendered, "\n") + 1
	if lineCount != 4 {
		// Pad or truncate to exactly 4 lines
		lines := strings.Split(rendered, "\n")
		for len(lines) < 4 {
			lines = append(lines, "") // Pad with blank lines
		}
		if len(lines) > 4 {
			lines = lines[:4] // Truncate
		}
		rendered = strings.Join(lines, "\n")
	}

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
		"%s %d ready  %s %d waiting  %s %d active  %s %d done",
		lipgloss.NewStyle().Foreground(ColorReady).Render(IndicatorReady),
		s.Stats.Ready,
		lipgloss.NewStyle().Foreground(ColorWaiting).Render(IndicatorWaiting),
		s.Stats.Waiting,
		lipgloss.NewStyle().Foreground(ColorActive).Render(IndicatorActive),
		s.Stats.InProgress,
		lipgloss.NewStyle().Foreground(ColorCompleted).Render(IndicatorCompleted),
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

// renderSecondRow renders the second row: system metrics (CPU/Mem/Proc)
func (s StatusBar) renderSecondRow(width int) string {
	// System metrics
	metricsContent := ""
	if s.Metrics != nil && s.Metrics.ProcessCount > 0 {
		metricsContent = lipgloss.NewStyle().
			Foreground(ColorReady).
			Render(s.Metrics.Format())
	}

	// If no metrics, render empty space
	if metricsContent == "" {
		metricsContent = " "
	}

	return metricsContent
}

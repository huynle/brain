package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// StatusBar displays project name, task stats, and connection status.
type StatusBar struct {
	Project       string
	Connected     bool
	Stats         TaskStats
	SelectedCount int
	Metrics       *ResourceMetrics
}

// NewStatusBar creates a new StatusBar for the given project.
func NewStatusBar(project string) StatusBar {
	return StatusBar{Project: project}
}

// View renders the status bar as a styled single-line string.
func (s StatusBar) View(width int) string {
	if width < 20 {
		width = 20
	}

	// Left side: project name
	projectName := TitleStyle.Render(s.Project)

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

	// Right side: metrics + connection indicator
	rightContent := ""
	if s.Metrics != nil && s.Metrics.ProcessCount > 0 {
		rightContent = lipgloss.NewStyle().
			Foreground(ColorReady).
			Render(s.Metrics.Format()) + "  "
	}
	rightContent += connDot

	// Compose the status bar
	leftContent := projectName + "  " + stats

	// Use a border style for the status bar
	barStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(ColorCyan).
		Width(width - 2).
		PaddingLeft(1).
		PaddingRight(1)

	// Place left and right content with space between
	innerWidth := width - 6 // account for border + padding
	if innerWidth < 10 {
		innerWidth = 10
	}

	leftStyle := lipgloss.NewStyle().Width(innerWidth - 2)
	rightStyle := lipgloss.NewStyle().Align(lipgloss.Right).Width(2)

	row := lipgloss.JoinHorizontal(lipgloss.Top,
		leftStyle.Render(leftContent),
		rightStyle.Render(rightContent),
	)

	rendered := barStyle.Render(row)

	// CRITICAL: Ensure exactly 3 lines (1 content + 2 border lines)
	// This matches the TypeScript TUI behavior
	lineCount := strings.Count(rendered, "\n") + 1
	if lineCount != 3 {
		// Pad or truncate to exactly 3 lines
		lines := strings.Split(rendered, "\n")
		for len(lines) < 3 {
			lines = append(lines, "") // Pad with blank lines
		}
		if len(lines) > 3 {
			lines = lines[:3] // Truncate
		}
		rendered = strings.Join(lines, "\n")
	}

	return rendered
}

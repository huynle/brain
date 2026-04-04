package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/huynle/brain-api/internal/types"
)

// ScheduleDetail displays detailed information about the currently selected scheduled task.
type ScheduleDetail struct {
	task          *types.ResolvedTask
	width, height int

	// Viewport scrolling state
	scrollOffset int // First visible line index (0-based)
	totalLines   int // Total content lines (set after rendering)
}

// NewScheduleDetail creates a new empty ScheduleDetail component.
func NewScheduleDetail() ScheduleDetail {
	return ScheduleDetail{}
}

// SetTask updates the displayed task and resets scroll position.
func (sd *ScheduleDetail) SetTask(task *types.ResolvedTask) {
	sd.task = task
	sd.scrollOffset = 0
	sd.totalLines = 0
}

// ScrollDown scrolls the viewport down by one line.
func (sd *ScheduleDetail) ScrollDown() {
	viewportHeight := sd.height - 1 // Account for header line
	if viewportHeight <= 0 || sd.totalLines <= viewportHeight {
		return
	}
	maxOffset := sd.totalLines - viewportHeight
	if sd.scrollOffset < maxOffset {
		sd.scrollOffset++
	}
}

// ScrollUp scrolls the viewport up by one line.
func (sd *ScheduleDetail) ScrollUp() {
	if sd.scrollOffset > 0 {
		sd.scrollOffset--
	}
}

// ScrollToTop scrolls to the top of the content.
func (sd *ScheduleDetail) ScrollToTop() {
	sd.scrollOffset = 0
}

// ScrollToBottom scrolls to the bottom of the content.
func (sd *ScheduleDetail) ScrollToBottom() {
	viewportHeight := sd.height - 1 // Account for header line
	if viewportHeight <= 0 || sd.totalLines <= viewportHeight {
		sd.scrollOffset = 0
		return
	}
	sd.scrollOffset = sd.totalLines - viewportHeight
}

// SetSize updates the component dimensions.
func (sd *ScheduleDetail) SetSize(width, height int) {
	sd.width = width
	sd.height = height
}

// View renders the schedule detail panel.
func (sd *ScheduleDetail) View() string {
	if sd.task == nil {
		return sd.renderEmpty()
	}
	return sd.renderSchedule()
}

// renderEmpty renders the placeholder when no task is selected.
func (sd *ScheduleDetail) renderEmpty() string {
	header := TitleStyle.Render("Schedule Details")
	placeholder := DimStyle.Render("Select a scheduled task to view details")
	sd.totalLines = 0
	sd.scrollOffset = 0
	return header + "\n" + placeholder
}

// renderSchedule renders the full schedule detail view.
func (sd *ScheduleDetail) renderSchedule() string {
	task := sd.task
	var lines []string

	// Title
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render(task.Title))

	// ID
	lines = append(lines, fmt.Sprintf("ID: %s", DimStyle.Render(task.ID)))

	// Schedule expression
	if task.Schedule != "" {
		lines = append(lines, fmt.Sprintf("Schedule: %s", task.Schedule))
	}

	// Run-once-at with countdown
	if task.RunOnceAt != "" {
		countdown := formatRunOnceAt(task.RunOnceAt)
		runOnceLabel := fmt.Sprintf("Run Once At: %s", task.RunOnceAt)
		runOnceLabel += DimStyle.Render(fmt.Sprintf("  (%s)", countdown))
		lines = append(lines, runOnceLabel)
	}

	// Timezone
	if task.Timezone != "" {
		lines = append(lines, fmt.Sprintf("Timezone: %s", task.Timezone))
	}

	// Time window: starts_at / expires_at
	if task.StartsAt != "" || task.ExpiresAt != "" {
		lines = append(lines, sd.renderTimeWindow(task.StartsAt, task.ExpiresAt))
	}

	// Enabled
	if task.ScheduleEnabled != nil {
		if *task.ScheduleEnabled {
			enabledText := lipgloss.NewStyle().Foreground(ColorReady).Render("yes")
			lines = append(lines, fmt.Sprintf("Enabled: %s", enabledText))
		} else {
			disabledText := lipgloss.NewStyle().Foreground(ColorWaiting).Render("no")
			lines = append(lines, fmt.Sprintf("Enabled: %s", disabledText))
		}
	} else {
		disabledText := lipgloss.NewStyle().Foreground(ColorWaiting).Render("no")
		lines = append(lines, fmt.Sprintf("Enabled: %s", disabledText))
	}

	// Status + Classification
	statusLine := fmt.Sprintf("Status: %s", StatusStyle(task.Classification).Render(task.Status))
	if task.Classification != "" {
		statusLine += DimStyle.Render(" (") +
			StatusStyle(task.Classification).Render(task.Classification) +
			DimStyle.Render(")")
	}
	lines = append(lines, statusLine)

	// Priority
	if task.Priority != "" {
		priorityLine := fmt.Sprintf("Priority: %s", PriorityStyle(task.Priority).Render(task.Priority))
		lines = append(lines, priorityLine)
	}

	// Feature info
	if task.FeatureID != "" {
		lines = append(lines, fmt.Sprintf("Feature: %s", lipgloss.NewStyle().Foreground(ColorMagenta).Render(task.FeatureID)))
	}

	// Project
	if task.ProjectID != "" {
		lines = append(lines, fmt.Sprintf("Project: %s", task.ProjectID))
	} else {
		lines = append(lines, fmt.Sprintf("Project: %s", DimStyle.Render("none")))
	}

	// Path
	if task.Path != "" {
		lines = append(lines, fmt.Sprintf("Path: %s", DimStyle.Render(task.Path)))
	}

	// Created
	if task.Created != "" {
		lines = append(lines, fmt.Sprintf("Created: %s", DimStyle.Render(task.Created)))
	}

	// Store total content lines
	sd.totalLines = len(lines)

	// Build header with position indicator
	var headerLine string

	if sd.height > 0 && sd.totalLines > sd.height-1 {
		// Content is scrollable (more lines than viewport minus header)
		viewportHeight := sd.height - 1 // Reserve 1 line for header
		if viewportHeight < 1 {
			viewportHeight = 1
		}

		// Clamp scroll offset
		maxOffset := sd.totalLines - viewportHeight
		if maxOffset < 0 {
			maxOffset = 0
		}
		if sd.scrollOffset > maxOffset {
			sd.scrollOffset = maxOffset
		}

		startLine := sd.scrollOffset + 1
		endLine := sd.scrollOffset + viewportHeight
		if endLine > sd.totalLines {
			endLine = sd.totalLines
		}

		headerLine = TitleStyle.Render("Schedule Details") +
			DimStyle.Render(fmt.Sprintf(" (%d-%d/%d)", startLine, endLine, sd.totalLines))

		// Viewport slice
		end := sd.scrollOffset + viewportHeight
		if end > sd.totalLines {
			end = sd.totalLines
		}
		visibleLines := make([]string, end-sd.scrollOffset)
		copy(visibleLines, lines[sd.scrollOffset:end])

		// Replace first/last visible lines with scroll indicators
		hasMore := sd.scrollOffset > 0
		hasBelow := end < sd.totalLines

		if hasMore && len(visibleLines) > 0 {
			visibleLines[0] = DimStyle.Render("▲ more above")
		}
		if hasBelow && len(visibleLines) > 0 {
			visibleLines[len(visibleLines)-1] = DimStyle.Render("▼ more below")
		}

		var result []string
		result = append(result, headerLine)
		result = append(result, visibleLines...)
		return strings.Join(result, "\n")
	}

	// Content fits in viewport - no scrolling needed
	sd.scrollOffset = 0
	headerLine = TitleStyle.Render("Schedule Details")
	var result []string
	result = append(result, headerLine)
	result = append(result, lines...)
	return strings.Join(result, "\n")
}

// renderTimeWindow formats the starts_at/expires_at time window display.
func (sd *ScheduleDetail) renderTimeWindow(startsAt, expiresAt string) string {
	parts := []string{"Window:"}

	if startsAt != "" {
		startLabel := startsAt
		if t, err := time.Parse(time.RFC3339, startsAt); err == nil {
			startLabel += DimStyle.Render(fmt.Sprintf(" (%s)", formatCountdown(t)))
		}
		parts = append(parts, startLabel)
	} else {
		parts = append(parts, DimStyle.Render("open"))
	}

	parts = append(parts, "→")

	if expiresAt != "" {
		expiresLabel := expiresAt
		if t, err := time.Parse(time.RFC3339, expiresAt); err == nil {
			expiresLabel += DimStyle.Render(fmt.Sprintf(" (%s)", formatCountdown(t)))
		}
		parts = append(parts, expiresLabel)
	} else {
		parts = append(parts, DimStyle.Render("open"))
	}

	return strings.Join(parts, " ")
}

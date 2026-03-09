package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/huynle/brain-api/internal/types"
)

// ProjectLabelStyle is used for project name labels in aggregate view.
var ProjectLabelStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("cyan")).
	Bold(true)

// renderGroupedTaskLineWithProject renders a single task line with optional project label.
// When activeProjectID == "all", shows [project-name] prefix for each task.
// When activeProjectID is a specific project, no project label is shown.
// The width parameter is used for truncation when TextWrap is false.
func (tt *TaskTree) renderGroupedTaskLineWithProject(task types.ResolvedTask, isSelected bool, selectedTasks map[string]bool, showCheckboxes bool, activeProjectID string, width int) string {
	// Selection marker
	selMarker := "  "
	if isSelected {
		selMarker = lipgloss.NewStyle().Foreground(ColorCyan).Render("▸ ")
	}

	// Checkbox indicator (ONLY when multi-select active)
	checkboxPart := ""
	if showCheckboxes {
		checkbox := "[ ]"
		if selectedTasks[task.ID] {
			checkbox = "[x]"
		}
		checkboxPart = checkbox + " "
	}

	// Status indicator with color
	indicator := statusIndicator(task.Classification)
	indicatorStyled := StatusStyle(task.Classification).Render(indicator)

	// Project label (ONLY in aggregate view and if ProjectID is not empty)
	projectLabel := ""
	projectLabelPlain := ""
	if activeProjectID == "all" && task.ProjectID != "" {
		projectLabelPlain = fmt.Sprintf("[%s] ", task.ProjectID)
		projectLabel = ProjectLabelStyle.Render(projectLabelPlain)
	}

	// Title — truncate BEFORE styling to avoid cutting ANSI sequences
	title := task.Title
	if !tt.TextWrap && width > 0 {
		// Overhead: selMarker(2) + checkbox + indicator(2) + space(1) + projectLabel + suffix
		overhead := 2 + len(checkboxPart) + 2 + 1 + len(projectLabelPlain)
		if task.Priority == "high" {
			overhead++
		}
		availableWidth := width - overhead
		title = truncateTitle(title, availableWidth)
	}

	if isSelected {
		title = lipgloss.NewStyle().Bold(true).Foreground(ColorWhite).Render(title)
	} else if selectedTasks[task.ID] {
		// Apply selection style to selected tasks even when not focused
		title = SelectedTaskStyle.Render(title)
	}

	// Priority suffix
	prioritySuffix := ""
	if task.Priority == "high" {
		prioritySuffix = lipgloss.NewStyle().Foreground(ColorPriorityHigh).Bold(true).Render("!")
	}

	return fmt.Sprintf("%s%s%s %s%s%s", selMarker, checkboxPart, indicatorStyled, projectLabel, title, prioritySuffix)
}

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
func (tt *TaskTree) renderGroupedTaskLineWithProject(task types.ResolvedTask, isSelected bool, selectedTasks map[string]bool, showCheckboxes bool, activeProjectID string) string {
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
	if activeProjectID == "all" && task.ProjectID != "" {
		projectLabel = ProjectLabelStyle.Render(fmt.Sprintf("[%s] ", task.ProjectID))
	}

	// Title
	title := task.Title
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

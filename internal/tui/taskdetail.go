package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/huynle/brain-api/internal/types"
)

// TaskDetail displays detailed information about the currently selected task.
type TaskDetail struct {
	task          *types.ResolvedTask
	width, height int
}

// NewTaskDetail creates a new empty TaskDetail component.
func NewTaskDetail() TaskDetail {
	return TaskDetail{}
}

// SetTask updates the displayed task.
func (td *TaskDetail) SetTask(task *types.ResolvedTask) {
	td.task = task
}

// SetSize updates the component dimensions.
func (td *TaskDetail) SetSize(width, height int) {
	td.width = width
	td.height = height
}

// View renders the task detail panel.
func (td *TaskDetail) View() string {
	if td.task == nil {
		return td.renderEmpty()
	}
	return td.renderTask()
}

// renderEmpty renders the placeholder when no task is selected.
func (td *TaskDetail) renderEmpty() string {
	header := TitleStyle.Render("Task Detail")
	placeholder := DimStyle.Render("No task selected")
	return header + "\n" + placeholder
}

// renderTask renders the full task detail view.
func (td *TaskDetail) renderTask() string {
	task := td.task
	var lines []string

	// Header
	lines = append(lines, TitleStyle.Render("Task Detail"))
	lines = append(lines, "")

	// Title
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render(task.Title))

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

	// ID
	lines = append(lines, fmt.Sprintf("ID: %s", DimStyle.Render(task.ID)))

	// Path
	if task.Path != "" {
		lines = append(lines, fmt.Sprintf("Path: %s", DimStyle.Render(task.Path)))
	}

	// Git context
	if task.GitBranch != "" || task.GitRemote != "" {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Underline(true).Render("Git Context:"))
		if task.GitBranch != "" {
			lines = append(lines, fmt.Sprintf("  Branch: %s",
				lipgloss.NewStyle().Foreground(ColorCyan).Render(task.GitBranch)))
		}
		if task.GitRemote != "" {
			lines = append(lines, fmt.Sprintf("  Remote: %s", task.GitRemote))
		}
	}

	// Working directory
	if task.Workdir != "" || task.ResolvedWorkdir != "" {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Underline(true).Render("Working Directory:"))
		if task.Workdir != "" {
			lines = append(lines, fmt.Sprintf("  workdir: %s", task.Workdir))
		}
		if task.ResolvedWorkdir != "" {
			lines = append(lines, fmt.Sprintf("  resolved: %s",
				lipgloss.NewStyle().Foreground(ColorReady).Render(task.ResolvedWorkdir)))
		}
	}

	// Dependencies
	hasDeps := len(task.DependsOn) > 0
	hasWaiting := len(task.WaitingOn) > 0
	hasBlocked := len(task.BlockedBy) > 0

	if hasDeps || hasWaiting || hasBlocked {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Underline(true).Render("Dependencies:"))

		for _, dep := range task.DependsOn {
			lines = append(lines, DimStyle.Render(fmt.Sprintf("  - %s", dep)))
		}

		if hasWaiting {
			lines = append(lines, fmt.Sprintf("  %s %s",
				lipgloss.NewStyle().Foreground(ColorWaiting).Render("Waiting on:"),
				DimStyle.Render(strings.Join(task.WaitingOn, ", "))))
		}

		if hasBlocked {
			lines = append(lines, fmt.Sprintf("  %s %s",
				lipgloss.NewStyle().Foreground(ColorBlocked).Render("Blocked by:"),
				DimStyle.Render(strings.Join(task.BlockedBy, ", "))))
		}

		if task.BlockedByReason != "" {
			lines = append(lines, fmt.Sprintf("  %s %s",
				lipgloss.NewStyle().Foreground(ColorBlocked).Render("Reason:"),
				task.BlockedByReason))
		}
	}

	// Cycle warning
	if task.InCycle {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(ColorBlocked).Bold(true).
			Render("↺ Task is part of a dependency cycle"))
	}

	// Truncate to height if needed
	content := strings.Join(lines, "\n")
	if td.height > 0 {
		visibleLines := strings.Split(content, "\n")
		if len(visibleLines) > td.height {
			visibleLines = visibleLines[:td.height]
			content = strings.Join(visibleLines, "\n")
		}
	}

	return content
}

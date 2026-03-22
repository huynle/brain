package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/huynle/brain-api/internal/types"
)

// TaskDetail displays detailed information about the currently selected task.
type TaskDetail struct {
	task          *types.ResolvedTask
	width, height int

	// Viewport scrolling state
	scrollOffset int // First visible line index (0-based)
	totalLines   int // Total content lines (set after rendering)
}

// NewTaskDetail creates a new empty TaskDetail component.
func NewTaskDetail() TaskDetail {
	return TaskDetail{}
}

// SetTask updates the displayed task. Only resets scroll position if the task changes.
func (td *TaskDetail) SetTask(task *types.ResolvedTask) {
	// Only reset scroll position when switching to a different task
	oldID := ""
	if td.task != nil {
		oldID = td.task.ID
	}
	newID := ""
	if task != nil {
		newID = task.ID
	}

	td.task = task
	if oldID != newID {
		td.scrollOffset = 0
		td.totalLines = 0
	}
}

// ScrollDown scrolls the viewport down by one line.
func (td *TaskDetail) ScrollDown() {
	viewportHeight := td.height - 1 // Account for header line
	if viewportHeight <= 0 || td.totalLines <= viewportHeight {
		return
	}
	maxOffset := td.totalLines - viewportHeight
	if td.scrollOffset < maxOffset {
		td.scrollOffset++
	}
}

// ScrollUp scrolls the viewport up by one line.
func (td *TaskDetail) ScrollUp() {
	if td.scrollOffset > 0 {
		td.scrollOffset--
	}
}

// ScrollToTop scrolls to the top of the content.
func (td *TaskDetail) ScrollToTop() {
	td.scrollOffset = 0
}

// ScrollToBottom scrolls to the bottom of the content.
func (td *TaskDetail) ScrollToBottom() {
	viewportHeight := td.height - 1 // Account for header line
	if viewportHeight <= 0 || td.totalLines <= viewportHeight {
		td.scrollOffset = 0
		return
	}
	td.scrollOffset = td.totalLines - viewportHeight
}

// SetSize updates the component dimensions and recomputes total lines for scrolling.
func (td *TaskDetail) SetSize(width, height int) {
	td.width = width
	td.height = height
	// Recompute totalLines so ScrollDown/ScrollUp have accurate bounds
	if td.task != nil {
		td.totalLines = td.countContentLines()
	}
}

// countContentLines counts how many content lines the task produces (excluding header).
// Used by SetSize to precompute totalLines for scroll bounds.
func (td *TaskDetail) countContentLines() int {
	if td.task == nil {
		return 0
	}
	task := td.task
	count := 0

	// Title
	count++
	// Status
	count++
	// Priority
	if task.Priority != "" {
		count++
	}
	// ID
	count++
	// Path
	if task.Path != "" {
		count++
	}
	// Created
	if task.Created != "" {
		count++
	}
	// Git context
	if task.GitBranch != "" || task.GitRemote != "" {
		count++ // blank line
		count++ // header
		if task.GitBranch != "" {
			count++
		}
		if task.GitRemote != "" {
			count++
		}
	}
	// Working directory
	if task.ResolvedWorkdir != "" || task.Workdir != "" {
		count++ // blank line
		count++ // header
		if task.ResolvedWorkdir != "" {
			count++
		}
		if task.Workdir != "" && task.Workdir != task.ResolvedWorkdir {
			count++
		}
	}
	// Dependencies
	hasDeps := len(task.DependsOn) > 0
	hasWaiting := len(task.WaitingOn) > 0
	hasBlocked := len(task.BlockedBy) > 0
	if hasDeps || hasWaiting || hasBlocked {
		count++ // blank line
		count++ // header
		count += len(task.DependsOn)
		if hasWaiting {
			count++
		}
		if hasBlocked {
			count++
		}
		if task.BlockedByReason != "" {
			count++
		}
	}
	// Sessions
	if len(task.Sessions) > 0 {
		count++ // blank line
		count++ // header
		count += len(task.Sessions)
	}
	// Cycle warning
	if task.InCycle {
		count++ // blank line
		count++ // warning
	}
	return count
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
	td.totalLines = 0
	td.scrollOffset = 0
	return header + "\n" + placeholder
}

// renderTask renders the full task detail view.
func (td *TaskDetail) renderTask() string {
	task := td.task
	var lines []string

	// Header with position indicator (rendered separately, not scrolled)
	// Will be prepended after viewport slicing
	var headerLine string

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

	// Created timestamp
	if task.Created != "" {
		lines = append(lines, fmt.Sprintf("Created: %s", DimStyle.Render(task.Created)))
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
	if task.ResolvedWorkdir != "" || task.Workdir != "" {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Underline(true).Render("Working Directory:"))

		// Show resolved path first (more important, fully qualified)
		if task.ResolvedWorkdir != "" {
			lines = append(lines, fmt.Sprintf("  %s",
				lipgloss.NewStyle().Foreground(ColorReady).Render(task.ResolvedWorkdir)))
		}

		// Show original workdir only if different from resolved
		if task.Workdir != "" && task.Workdir != task.ResolvedWorkdir {
			lines = append(lines, fmt.Sprintf("  (from: %s)", DimStyle.Render(task.Workdir)))
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

	// Sessions
	if len(task.Sessions) > 0 {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Underline(true).Render(
			fmt.Sprintf("Sessions (%d):", len(task.Sessions))))

		// Sort by timestamp descending (most recent first)
		type sessionEntry struct {
			id        string
			timestamp string
		}
		sortedSessions := make([]sessionEntry, 0, len(task.Sessions))
		for id, info := range task.Sessions {
			sortedSessions = append(sortedSessions, sessionEntry{id: id, timestamp: info.Timestamp})
		}
		sort.Slice(sortedSessions, func(i, j int) bool {
			return sortedSessions[i].timestamp > sortedSessions[j].timestamp
		})

		for _, s := range sortedSessions {
			sessionStyle := lipgloss.NewStyle().Foreground(ColorCyan)
			line := fmt.Sprintf("  %s", sessionStyle.Render(s.id))
			if s.timestamp != "" {
				line += DimStyle.Render(fmt.Sprintf(" (%s)", s.timestamp))
			}
			lines = append(lines, line)
		}
	}

	// Cycle warning
	if task.InCycle {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(ColorBlocked).Bold(true).
			Render("↺ Task is part of a dependency cycle"))
	}

	// Store total content lines
	td.totalLines = len(lines)

	// Build header with position indicator
	if td.height > 0 && td.totalLines > td.height-1 {
		// Content is scrollable (more lines than viewport minus header)
		viewportHeight := td.height - 1 // Reserve 1 line for header
		if viewportHeight < 1 {
			viewportHeight = 1
		}

		// Clamp scroll offset
		maxOffset := td.totalLines - viewportHeight
		if maxOffset < 0 {
			maxOffset = 0
		}
		if td.scrollOffset > maxOffset {
			td.scrollOffset = maxOffset
		}

		startLine := td.scrollOffset + 1
		endLine := td.scrollOffset + viewportHeight
		if endLine > td.totalLines {
			endLine = td.totalLines
		}

		headerLine = TitleStyle.Render("Task Detail") +
			DimStyle.Render(fmt.Sprintf(" (%d-%d/%d)", startLine, endLine, td.totalLines))

		// Viewport slice
		end := td.scrollOffset + viewportHeight
		if end > td.totalLines {
			end = td.totalLines
		}
		visibleLines := make([]string, end-td.scrollOffset)
		copy(visibleLines, lines[td.scrollOffset:end])

		// Replace first/last visible lines with scroll indicators
		hasMore := td.scrollOffset > 0
		hasBelow := end < td.totalLines

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
	td.scrollOffset = 0
	headerLine = TitleStyle.Render("Task Detail")
	var result []string
	result = append(result, headerLine)
	result = append(result, lines...)
	return strings.Join(result, "\n")
}

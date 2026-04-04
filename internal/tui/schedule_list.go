package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/huynle/brain-api/internal/types"
)

// ScheduleList displays scheduled tasks in a flat list with schedule metadata.
// It filters to only show tasks that have a non-empty Schedule field.
type ScheduleList struct {
	SelectedID string
	Cursor     int

	tasks          []types.ResolvedTask
	scrollOffset   int
	viewportHeight int
}

// NewScheduleList creates a new empty ScheduleList component.
func NewScheduleList() ScheduleList {
	return ScheduleList{}
}

// SetTasks updates the task list, filtering to only scheduled tasks.
// Includes tasks with a cron Schedule or a RunOnceAt timestamp.
// Preserves the current selection if the selected task still exists.
func (sl *ScheduleList) SetTasks(tasks []types.ResolvedTask) {
	// Filter to tasks with a non-empty Schedule OR RunOnceAt
	var scheduled []types.ResolvedTask
	for _, t := range tasks {
		if t.Schedule != "" || t.RunOnceAt != "" {
			scheduled = append(scheduled, t)
		}
	}
	sl.tasks = scheduled

	// Preserve selection if possible
	if sl.SelectedID != "" {
		for i, t := range sl.tasks {
			if t.ID == sl.SelectedID {
				sl.Cursor = i
				return
			}
		}
	}

	// Selection lost or no previous selection — auto-select first
	if len(sl.tasks) > 0 {
		sl.SelectedID = sl.tasks[0].ID
		sl.Cursor = 0
	} else {
		sl.SelectedID = ""
		sl.Cursor = 0
	}
}

// MoveDown moves the cursor down one position.
func (sl *ScheduleList) MoveDown() {
	if len(sl.tasks) == 0 {
		return
	}
	if sl.Cursor < len(sl.tasks)-1 {
		sl.Cursor++
		sl.SelectedID = sl.tasks[sl.Cursor].ID
	}
}

// MoveUp moves the cursor up one position.
func (sl *ScheduleList) MoveUp() {
	if len(sl.tasks) == 0 {
		return
	}
	if sl.Cursor > 0 {
		sl.Cursor--
		sl.SelectedID = sl.tasks[sl.Cursor].ID
	}
}

// MoveToTop moves the cursor to the first task.
func (sl *ScheduleList) MoveToTop() {
	if len(sl.tasks) == 0 {
		return
	}
	sl.Cursor = 0
	sl.SelectedID = sl.tasks[0].ID
}

// MoveToBottom moves the cursor to the last task.
func (sl *ScheduleList) MoveToBottom() {
	if len(sl.tasks) == 0 {
		return
	}
	sl.Cursor = len(sl.tasks) - 1
	sl.SelectedID = sl.tasks[sl.Cursor].ID
}

// SelectedTask returns the currently selected task, or nil if none.
func (sl *ScheduleList) SelectedTask() *types.ResolvedTask {
	if sl.SelectedID == "" || len(sl.tasks) == 0 {
		return nil
	}
	for i := range sl.tasks {
		if sl.tasks[i].ID == sl.SelectedID {
			return &sl.tasks[i]
		}
	}
	return nil
}

// View renders the schedule list as a string within the given dimensions.
func (sl *ScheduleList) View(width, height int) string {
	// Header
	header := TitleStyle.Render("Scheduled")

	if len(sl.tasks) == 0 {
		placeholder := DimStyle.Render("No scheduled tasks found")
		return header + "\n" + placeholder
	}

	var lines []string
	lines = append(lines, header)

	// Calculate viewport (height minus header line)
	viewportHeight := height - 1
	if viewportHeight < 1 {
		viewportHeight = len(sl.tasks)
	}

	// Ensure selected task is visible by adjusting scroll offset
	if sl.Cursor < sl.scrollOffset {
		sl.scrollOffset = sl.Cursor
	}
	if sl.Cursor >= sl.scrollOffset+viewportHeight {
		sl.scrollOffset = sl.Cursor - viewportHeight + 1
	}

	// Clamp scroll offset
	maxOffset := len(sl.tasks) - viewportHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if sl.scrollOffset > maxOffset {
		sl.scrollOffset = maxOffset
	}
	if sl.scrollOffset < 0 {
		sl.scrollOffset = 0
	}

	// Determine visible range
	start := sl.scrollOffset
	end := start + viewportHeight
	if end > len(sl.tasks) {
		end = len(sl.tasks)
	}

	// Check if scroll indicators are needed
	hasAbove := start > 0
	hasBelow := end < len(sl.tasks)

	for i := start; i < end; i++ {
		task := sl.tasks[i]
		isSelected := task.ID == sl.SelectedID

		// Check if this line should be a scroll indicator
		isFirstVisible := i == start
		isLastVisible := i == end-1

		if hasAbove && isFirstVisible && end-start > 1 {
			lines = append(lines, DimStyle.Render("▲ more above"))
			continue
		}
		if hasBelow && isLastVisible && end-start > 1 {
			lines = append(lines, DimStyle.Render("▼ more below"))
			continue
		}

		line := sl.renderTaskLine(task, isSelected)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// renderTaskLine renders a single scheduled task line.
func (sl *ScheduleList) renderTaskLine(task types.ResolvedTask, isSelected bool) string {
	// Selection marker
	selMarker := "  "
	if isSelected {
		selMarker = lipgloss.NewStyle().Foreground(ColorCyan).Render("▸ ")
	}

	// Status indicator with color
	indicator := statusIndicator(task.Status, task.Classification)
	indicatorStyled := StatusStyleWithState(task.Status, task.Classification).Render(indicator)

	// Title (dimmed if schedule disabled)
	title := task.Title
	isDisabled := task.ScheduleEnabled != nil && !*task.ScheduleEnabled
	if isDisabled {
		title = DimStyle.Render(title)
	} else if isSelected {
		title = lipgloss.NewStyle().Bold(true).Foreground(ColorWhite).Render(title)
	}

	// Badge: [disabled] (yellow), [one-shot] (cyan), or [scheduled] (magenta)
	badge := ""
	isOneShot := task.RunOnceAt != "" && task.Schedule == ""
	if isDisabled {
		badge = lipgloss.NewStyle().Foreground(ColorWaiting).Render("  [disabled]")
	} else if isOneShot {
		badge = lipgloss.NewStyle().Foreground(ColorCyan).Render("  [one-shot]")
	} else {
		badge = lipgloss.NewStyle().Foreground(ColorMagenta).Render("  [scheduled]")
	}

	// Schedule expression or run_once_at countdown (dim)
	scheduleExpr := ""
	if isOneShot {
		scheduleExpr = DimStyle.Render("  " + formatRunOnceAt(task.RunOnceAt))
	} else if task.Schedule != "" {
		scheduleExpr = DimStyle.Render("  " + task.Schedule)
	}

	// Priority suffix (only if not medium)
	prioritySuffix := ""
	if task.Priority != "" && task.Priority != "medium" {
		prioritySuffix = DimStyle.Render("  pri:" + task.Priority)
	}

	return fmt.Sprintf("%s%s %s%s%s%s", selMarker, indicatorStyled, title, badge, scheduleExpr, prioritySuffix)
}

// formatRunOnceAt formats a run_once_at timestamp as a countdown or timestamp.
// Returns "in Xh Ym" for future times, "passed" for past times, or the raw value on parse error.
func formatRunOnceAt(runOnceAt string) string {
	t, err := time.Parse(time.RFC3339, runOnceAt)
	if err != nil {
		return runOnceAt
	}
	return formatCountdown(t)
}

// formatCountdown returns a human-readable countdown string for a target time.
// Future: "in 2h 30m", past: "passed", imminent (<1m): "in <1m".
func formatCountdown(target time.Time) string {
	now := time.Now()
	diff := target.Sub(now)

	if diff <= 0 {
		return "passed"
	}

	days := int(diff.Hours()) / 24
	hours := int(diff.Hours()) % 24
	minutes := int(diff.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("in %dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("in %dh %dm", hours, minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("in %dm", minutes)
	}
	return "in <1m"
}

// ContentHeight returns the number of content lines in the schedule list.
func (sl *ScheduleList) ContentHeight() int {
	return len(sl.tasks)
}

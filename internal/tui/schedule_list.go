package tui

import (
	"fmt"
	"strings"

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
// Preserves the current selection if the selected task still exists.
func (sl *ScheduleList) SetTasks(tasks []types.ResolvedTask) {
	// Filter to only tasks with a non-empty Schedule
	var scheduled []types.ResolvedTask
	for _, t := range tasks {
		if t.Schedule != "" {
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

	// Badge: [disabled] (yellow) or [scheduled] (magenta)
	badge := ""
	if isDisabled {
		badge = lipgloss.NewStyle().Foreground(ColorWaiting).Render("  [disabled]")
	} else {
		badge = lipgloss.NewStyle().Foreground(ColorMagenta).Render("  [scheduled]")
	}

	// Schedule expression (dim)
	scheduleExpr := DimStyle.Render("  " + task.Schedule)

	// Priority suffix (only if not medium)
	prioritySuffix := ""
	if task.Priority != "" && task.Priority != "medium" {
		prioritySuffix = DimStyle.Render("  pri:" + task.Priority)
	}

	return fmt.Sprintf("%s%s %s%s%s%s", selMarker, indicatorStyled, title, badge, scheduleExpr, prioritySuffix)
}

// ContentHeight returns the number of content lines in the schedule list.
func (sl *ScheduleList) ContentHeight() int {
	return len(sl.tasks)
}

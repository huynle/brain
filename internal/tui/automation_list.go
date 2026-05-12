package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/huynle/brain-api/internal/types"
)

// AutomationListRow is the normalized row model for automation entries and
// scheduled task entries shown together in the automation tab.
type AutomationListRow struct {
	ID            string
	Path          string
	Title         string
	Source        string
	Status        string
	Enabled       bool
	TriggerKind   string
	TriggerDetail string
	Priority      string
}

// AutomationList displays automation entries and cron/run-once task entries.
type AutomationList struct {
	SelectedID string
	Cursor     int

	rows         []AutomationListRow
	loading      bool
	errMsg       string
	scrollOffset int
}

// NewAutomationList creates an empty AutomationList component.
func NewAutomationList() AutomationList {
	return AutomationList{}
}

// SetLoading toggles the loading state and clears stale errors when loading.
func (al *AutomationList) SetLoading(loading bool) {
	al.loading = loading
	if loading {
		al.errMsg = ""
	}
}

// SetError stores an error message and leaves loading state.
func (al *AutomationList) SetError(msg string) {
	al.loading = false
	al.errMsg = msg
}

// SetEntriesAndTasks normalizes automation entries and scheduled tasks into one list.
func (al *AutomationList) SetEntriesAndTasks(entries []types.BrainEntry, tasks []types.ResolvedTask) {
	rows := make([]AutomationListRow, 0, len(entries)+len(tasks))
	for _, entry := range entries {
		if entry.Type != "automation" {
			continue
		}
		rows = append(rows, AutomationRowFromEntry(entry))
	}
	for _, task := range tasks {
		if task.Schedule == "" && task.RunOnceAt == "" {
			continue
		}
		rows = append(rows, AutomationRowFromTask(task))
	}
	al.SetRows(rows)
}

// SetEntryRows normalizes automation entries and scheduled task entries into one list.
func (al *AutomationList) SetEntryRows(entries []types.BrainEntry, tasks []types.BrainEntry) {
	rows := make([]AutomationListRow, 0, len(entries)+len(tasks))
	for _, entry := range entries {
		if entry.Type != "automation" {
			continue
		}
		rows = append(rows, AutomationRowFromEntry(entry))
	}
	for _, task := range tasks {
		if task.Type != "task" || (task.Schedule == "" && task.RunOnceAt == "") {
			continue
		}
		rows = append(rows, AutomationRowFromTaskEntry(task))
	}
	al.SetRows(rows)
}

// SetRows replaces rows while preserving the selected row when possible.
func (al *AutomationList) SetRows(rows []AutomationListRow) {
	al.rows = rows
	al.loading = false
	al.errMsg = ""

	if al.SelectedID != "" {
		for i, row := range al.rows {
			if row.ID == al.SelectedID {
				al.Cursor = i
				return
			}
		}
	}

	if len(al.rows) > 0 {
		al.Cursor = 0
		al.SelectedID = al.rows[0].ID
	} else {
		al.Cursor = 0
		al.SelectedID = ""
	}
}

// Update handles keyboard navigation for the list.
func (al *AutomationList) Update(msg tea.Msg) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return
	}

	switch key.String() {
	case "j", "down":
		al.MoveDown()
	case "k", "up":
		al.MoveUp()
	case "g", "home":
		al.MoveToTop()
	case "G", "end":
		al.MoveToBottom()
	}
}

// MoveDown moves the cursor down one row.
func (al *AutomationList) MoveDown() {
	if al.Cursor < len(al.rows)-1 {
		al.Cursor++
		al.SelectedID = al.rows[al.Cursor].ID
	}
}

// MoveUp moves the cursor up one row.
func (al *AutomationList) MoveUp() {
	if al.Cursor > 0 {
		al.Cursor--
		al.SelectedID = al.rows[al.Cursor].ID
	}
}

// MoveToTop moves selection to the first row.
func (al *AutomationList) MoveToTop() {
	if len(al.rows) == 0 {
		return
	}
	al.Cursor = 0
	al.SelectedID = al.rows[0].ID
}

// MoveToBottom moves selection to the last row.
func (al *AutomationList) MoveToBottom() {
	if len(al.rows) == 0 {
		return
	}
	al.Cursor = len(al.rows) - 1
	al.SelectedID = al.rows[al.Cursor].ID
}

// SelectVisibleRow selects a rendered row by its zero-based visible index.
func (al *AutomationList) SelectVisibleRow(visibleIndex int) bool {
	idx := al.scrollOffset + visibleIndex
	if idx < 0 || idx >= len(al.rows) {
		return false
	}
	al.Cursor = idx
	al.SelectedID = al.rows[idx].ID
	return true
}

// ScrollUp moves the selection up by n rows.
func (al *AutomationList) ScrollUp(n int) {
	if n < 1 {
		n = 1
	}
	for i := 0; i < n; i++ {
		al.MoveUp()
	}
}

// ScrollDown moves the selection down by n rows.
func (al *AutomationList) ScrollDown(n int) {
	if n < 1 {
		n = 1
	}
	for i := 0; i < n; i++ {
		al.MoveDown()
	}
}

// SelectedRow returns the selected normalized row, or nil when the list is empty.
func (al *AutomationList) SelectedRow() *AutomationListRow {
	if al.SelectedID == "" || len(al.rows) == 0 {
		return nil
	}
	for i := range al.rows {
		if al.rows[i].ID == al.SelectedID {
			return &al.rows[i]
		}
	}
	return nil
}

// View renders the automation list within the given dimensions.
func (al *AutomationList) View(width, height int) string {
	header := TitleStyle.Render("Automations")

	if al.loading {
		return header + "\n" + DimStyle.Render("Loading automations...")
	}
	if al.errMsg != "" {
		return header + "\n" + lipgloss.NewStyle().Foreground(ColorBlocked).Render("Error: "+al.errMsg)
	}
	if len(al.rows) == 0 {
		return header + "\n" + DimStyle.Render("No automations found")
	}

	if height < 2 {
		height = len(al.rows) + 1
	}
	viewportHeight := height - 1
	al.ensureCursorVisible(viewportHeight)

	start := al.scrollOffset
	end := start + viewportHeight
	if end > len(al.rows) {
		end = len(al.rows)
	}

	lines := []string{header}
	for i := start; i < end; i++ {
		lines = append(lines, al.renderRow(al.rows[i], i == al.Cursor, width))
	}

	return strings.Join(lines, "\n")
}

func (al *AutomationList) ensureCursorVisible(viewportHeight int) {
	if viewportHeight < 1 {
		viewportHeight = len(al.rows)
	}
	if al.Cursor < al.scrollOffset {
		al.scrollOffset = al.Cursor
	}
	if al.Cursor >= al.scrollOffset+viewportHeight {
		al.scrollOffset = al.Cursor - viewportHeight + 1
	}
	maxOffset := len(al.rows) - viewportHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if al.scrollOffset > maxOffset {
		al.scrollOffset = maxOffset
	}
	if al.scrollOffset < 0 {
		al.scrollOffset = 0
	}
}

func (al *AutomationList) renderRow(row AutomationListRow, selected bool, width int) string {
	marker := "  "
	if selected {
		marker = lipgloss.NewStyle().Foreground(ColorCyan).Render("▸ ")
	}

	state := "enabled"
	stateStyle := lipgloss.NewStyle().Foreground(ColorReady)
	if !row.Enabled {
		state = "disabled"
		stateStyle = lipgloss.NewStyle().Foreground(ColorDim)
	}

	title := row.Title
	if selected {
		title = lipgloss.NewStyle().Bold(true).Foreground(ColorWhite).Render(title)
	} else if !row.Enabled {
		title = DimStyle.Render(title)
	}

	trigger := row.TriggerKind
	if row.TriggerDetail != "" {
		trigger += ":" + row.TriggerDetail
	}
	if trigger == "" {
		trigger = "manual"
	}

	priority := ""
	if row.Priority != "" && row.Priority != "medium" {
		priority = DimStyle.Render("  pri:" + row.Priority)
	}

	line := fmt.Sprintf("%s%s  %s  %s  %s%s",
		marker,
		DimStyle.Render(fmt.Sprintf("%-10s", row.Source)),
		title,
		stateStyle.Render("["+state+"]"),
		DimStyle.Render(trigger),
		priority,
	)
	if width > 0 && lipgloss.Width(line) > width {
		return lipgloss.NewStyle().MaxWidth(width).Render(line)
	}
	return line
}

// AutomationRowFromEntry converts a type=automation brain entry into a row.
func AutomationRowFromEntry(entry types.BrainEntry) AutomationListRow {
	row := AutomationListRow{
		ID:      entry.ID,
		Path:    entry.Path,
		Title:   entry.Title,
		Source:  "automation",
		Status:  entry.Status,
		Enabled: entry.Status == "active",
	}
	if entry.Trigger != nil {
		row.TriggerKind = entry.Trigger.Type
		switch entry.Trigger.Type {
		case "event":
			row.TriggerDetail = entry.Trigger.Event
		case "cron":
			row.TriggerDetail = entry.Trigger.Schedule
		case "webhook":
			row.TriggerDetail = entry.Trigger.Webhook
		}
	}
	return row
}

// AutomationRowFromTask converts a cron or run-once task into a row.
func AutomationRowFromTask(task types.ResolvedTask) AutomationListRow {
	enabled := true
	if task.ScheduleEnabled != nil {
		enabled = *task.ScheduleEnabled
	}

	row := AutomationListRow{
		ID:       task.ID,
		Path:     task.Path,
		Title:    task.Title,
		Source:   "task",
		Status:   task.Status,
		Enabled:  enabled,
		Priority: task.Priority,
	}

	if task.RunOnceAt != "" && task.Schedule == "" {
		row.TriggerKind = "run_once"
		row.TriggerDetail = task.RunOnceAt
	} else {
		row.TriggerKind = "cron"
		row.TriggerDetail = task.Schedule
	}

	return row
}

// AutomationRowFromTaskEntry converts a scheduled/run-once task entry into a row.
func AutomationRowFromTaskEntry(task types.BrainEntry) AutomationListRow {
	enabled := true
	if task.ScheduleEnabled != nil {
		enabled = *task.ScheduleEnabled
	}

	row := AutomationListRow{
		ID:       task.ID,
		Path:     task.Path,
		Title:    task.Title,
		Source:   "task",
		Status:   task.Status,
		Enabled:  enabled,
		Priority: task.Priority,
	}

	if task.RunOnceAt != "" && task.Schedule == "" {
		row.TriggerKind = "run_once"
		row.TriggerDetail = task.RunOnceAt
	} else {
		row.TriggerKind = "cron"
		row.TriggerDetail = task.Schedule
	}

	return row
}

package tui

import (
	"fmt"
	"sort"
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
	Scope         string
	Status        string
	Enabled       bool
	TriggerKind   string
	TriggerDetail string
	Priority      string
	RunSummary    string
	RunTaskID     string
	RunStatus     string

	// IsGoal marks rows that represent goal automations
	// (entry.generated_by == brain-goal). Goal rows render a [goal] badge and
	// a linked-task progress segment.
	IsGoal bool
	// GoalDone and GoalTotal capture goal progress derived from linked tasks
	// (tasks sharing the goal's feature_id). Used to render the progress bar.
	GoalDone  int
	GoalTotal int
}

// AutomationList displays automation entries and cron/run-once task entries.
type AutomationList struct {
	SelectedID string
	Cursor     int

	rows              []AutomationListRow
	generatedTasks    []types.BrainEntry
	expandedID        string
	selectedRunTaskID string
	loading           bool
	errMsg            string
	scrollOffset      int
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
	sortAutomationRows(rows)
	al.SetRows(rows)
}

// SetEntryRows normalizes automation entries and scheduled task entries into one list.
func (al *AutomationList) SetEntryRows(entries []types.BrainEntry, tasks []types.BrainEntry, generatedTasks []types.BrainEntry) {
	al.generatedTasks = append([]types.BrainEntry(nil), generatedTasks...)
	rows := make([]AutomationListRow, 0, len(entries)+len(tasks))
	runsByAutomation := automationRunSummaries(generatedTasks)
	goalProgress := goalProgressByFeature(generatedTasks)
	for _, entry := range entries {
		if entry.Type != "automation" {
			continue
		}
		row := AutomationRowFromEntry(entry)
		if summary, ok := runsByAutomation[entry.ID]; ok {
			row.RunSummary = summary.summary()
			row.RunTaskID = summary.latestID
			row.RunStatus = summary.latestStatus
		}
		if row.IsGoal && entry.FeatureID != "" {
			if progress, ok := goalProgress[entry.FeatureID]; ok {
				row.GoalDone = progress.done
				row.GoalTotal = progress.total
			}
		}
		rows = append(rows, row)
	}
	for _, task := range tasks {
		if task.Type != "task" || (task.Schedule == "" && task.RunOnceAt == "") {
			continue
		}
		rows = append(rows, AutomationRowFromTaskEntry(task))
	}
	sortAutomationRows(rows)
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
	if al.expandedID == al.SelectedID {
		runs := al.runTasksForSelectedRow()
		if len(runs) > 0 {
			if al.selectedRunTaskID == "" {
				al.selectedRunTaskID = runs[0].ID
				return
			}
			for i, task := range runs {
				if task.ID == al.selectedRunTaskID && i < len(runs)-1 {
					al.selectedRunTaskID = runs[i+1].ID
					return
				}
			}
		}
	}
	if al.Cursor < len(al.rows)-1 {
		al.Cursor++
		al.SelectedID = al.rows[al.Cursor].ID
		al.selectedRunTaskID = ""
	}
}

// MoveUp moves the cursor up one row.
func (al *AutomationList) MoveUp() {
	if al.expandedID == al.SelectedID && al.selectedRunTaskID != "" {
		runs := al.runTasksForSelectedRow()
		for i, task := range runs {
			if task.ID == al.selectedRunTaskID {
				if i > 0 {
					al.selectedRunTaskID = runs[i-1].ID
				} else {
					al.selectedRunTaskID = ""
				}
				return
			}
		}
		al.selectedRunTaskID = ""
		return
	}
	if al.Cursor > 0 {
		al.Cursor--
		al.SelectedID = al.rows[al.Cursor].ID
		al.selectedRunTaskID = ""
	}
}

// MoveToTop moves selection to the first row.
func (al *AutomationList) MoveToTop() {
	if len(al.rows) == 0 {
		return
	}
	al.Cursor = 0
	al.SelectedID = al.rows[0].ID
	al.selectedRunTaskID = ""
}

// MoveToBottom moves selection to the last row.
func (al *AutomationList) MoveToBottom() {
	if len(al.rows) == 0 {
		return
	}
	al.Cursor = len(al.rows) - 1
	al.SelectedID = al.rows[al.Cursor].ID
	al.selectedRunTaskID = ""
}

// SelectVisibleRow selects a rendered row by its zero-based visible index.
func (al *AutomationList) SelectVisibleRow(visibleIndex int) bool {
	idx := al.scrollOffset + visibleIndex
	if idx < 0 || idx >= len(al.rows) {
		return false
	}
	al.Cursor = idx
	al.SelectedID = al.rows[idx].ID
	al.selectedRunTaskID = ""
	return true
}

func (al *AutomationList) ToggleExpandedSelected() {
	if al.SelectedID == "" {
		return
	}
	if al.expandedID == al.SelectedID {
		al.expandedID = ""
		al.selectedRunTaskID = ""
		return
	}
	al.expandedID = al.SelectedID
	runs := al.runTasksForSelectedRow()
	if len(runs) > 0 {
		al.selectedRunTaskID = runs[0].ID
	} else {
		al.selectedRunTaskID = ""
	}
}

func (al *AutomationList) SelectedRunTask() (types.BrainEntry, bool) {
	if al.expandedID != al.SelectedID || al.selectedRunTaskID == "" {
		return types.BrainEntry{}, false
	}
	for _, task := range al.runTasksForSelectedRow() {
		if task.ID == al.selectedRunTaskID {
			return task, true
		}
	}
	return types.BrainEntry{}, false
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
		if al.expandedID == al.rows[i].ID {
			for _, task := range al.runTasksForRow(al.rows[i]) {
				lines = append(lines, al.renderRunTaskRow(task, width))
			}
		}
	}

	return strings.Join(lines, "\n")
}

func (al *AutomationList) runTasksForSelectedRow() []types.BrainEntry {
	row := al.SelectedRow()
	if row == nil {
		return nil
	}
	return al.runTasksForRow(*row)
}

func (al *AutomationList) runTasksForRow(row AutomationListRow) []types.BrainEntry {
	if row.ID == "" {
		return nil
	}
	generatedBy := "automation:" + row.ID
	tasks := make([]types.BrainEntry, 0)
	for _, task := range al.generatedTasks {
		if task.Type != "task" || task.GeneratedBy != generatedBy {
			continue
		}
		tasks = append(tasks, task)
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].Modified != tasks[j].Modified {
			return tasks[i].Modified > tasks[j].Modified
		}
		return tasks[i].ID > tasks[j].ID
	})
	return tasks
}

func (al *AutomationList) renderRunTaskRow(task types.BrainEntry, width int) string {
	marker := "  "
	if task.ID == al.selectedRunTaskID {
		marker = "▸ "
	}
	status := task.Status
	if status == "" {
		status = "unknown"
	}
	line := fmt.Sprintf("  %s%s [%s] %s", marker, task.ID, status, task.Title)
	if task.AutomationRunID != "" {
		line += " run=" + task.AutomationRunID
	}
	if sessionID := newestSessionID(task.Sessions); sessionID != "" {
		line += " session=" + sessionID + " (o: open, O: tmux)"
	} else {
		line += " session=none"
	}
	if width > 0 && lipgloss.Width(line) > width {
		line = truncateTitle(line, width)
	}
	return line
}

type automationRunSummary struct {
	pending      int
	active       int
	inactive     int
	latestID     string
	latestStatus string
	realRunCount int
}

func (s automationRunSummary) summary() string {
	if s.realRunCount > 0 {
		status := s.latestStatus
		if status == "" {
			status = "unknown"
		}
		return fmt.Sprintf("last %s (%d runs)", status, s.realRunCount)
	}
	parts := make([]string, 0, 3)
	if s.pending > 0 {
		parts = append(parts, fmt.Sprintf("%d queued", s.pending))
	}
	if s.active > 0 {
		parts = append(parts, fmt.Sprintf("%d running", s.active))
	}
	if s.inactive > 0 {
		parts = append(parts, fmt.Sprintf("%d done", s.inactive))
	}
	return strings.Join(parts, ", ")
}

func automationRunSummaries(tasks []types.BrainEntry) map[string]automationRunSummary {
	summaries := make(map[string]automationRunSummary)
	for _, task := range tasks {
		if task.Type == "automation_run" {
			automationID := automationRunContentField(task.Content, "automation_id")
			if automationID == "" {
				continue
			}
			summary := summaries[automationID]
			summary.realRunCount++
			if summary.latestID == "" || task.Modified > "" {
				summary.latestID = task.ID
				summary.latestStatus = task.Status
			}
			summaries[automationID] = summary
			continue
		}
		if !strings.HasPrefix(task.GeneratedBy, "automation:") {
			continue
		}
		automationID := strings.TrimPrefix(task.GeneratedBy, "automation:")
		if automationID == "" {
			continue
		}
		summary := summaries[automationID]
		switch task.Status {
		case "pending":
			summary.pending++
		case "active", "in_progress":
			summary.active++
		default:
			summary.inactive++
		}
		if summary.latestID == "" || task.Modified > "" {
			summary.latestID = task.ID
			summary.latestStatus = task.Status
		}
		summaries[automationID] = summary
	}
	return summaries
}

func automationRunContentField(content, field string) string {
	prefix := field + ":"
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

// goalTaskProgress accumulates linked-task counts for a single goal feature.
type goalTaskProgress struct {
	done  int
	total int
}

// goalProgressByFeature derives goal progress (done/total) keyed by feature_id
// from the supplied generated tasks. Goal automations link their work via the
// shared feature_id, mirroring the server-side GetTasksByFeature used by
// GoalProgress. A task counts as "done" when its status is completed or
// validated.
func goalProgressByFeature(tasks []types.BrainEntry) map[string]goalTaskProgress {
	progress := make(map[string]goalTaskProgress)
	for _, task := range tasks {
		if task.FeatureID == "" {
			continue
		}
		p := progress[task.FeatureID]
		p.total++
		switch task.Status {
		case "completed", "validated":
			p.done++
		}
		progress[task.FeatureID] = p
	}
	return progress
}

func sortAutomationRows(rows []AutomationListRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		left := automationRowSortKey(rows[i])
		right := automationRowSortKey(rows[j])
		for idx := range left {
			if left[idx] != right[idx] {
				return left[idx] < right[idx]
			}
		}
		return false
	})
}

func automationRowSortKey(row AutomationListRow) [6]string {
	return [6]string{
		automationSourceSortRank(row.Source),
		strings.ToLower(row.Title),
		strings.ToLower(row.TriggerKind),
		strings.ToLower(row.TriggerDetail),
		strings.ToLower(row.Status),
		row.ID,
	}
}

func automationSourceSortRank(source string) string {
	switch source {
	case "automation":
		return "1"
	case "task":
		return "2"
	default:
		return "9"
	}
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

	runs := ""
	if row.RunSummary != "" {
		runs = "  " + lipgloss.NewStyle().Foreground(ColorWaiting).Render("run: "+row.RunSummary)
		if row.RunTaskID != "" {
			runs += DimStyle.Render(" #" + row.RunTaskID)
		}
	}

	badge := ""
	goalProgress := ""
	if row.IsGoal {
		badge = "  " + lipgloss.NewStyle().Foreground(ColorMagenta).Render("[goal]")
		goalProgress = "  " + renderGoalProgress(row.GoalDone, row.GoalTotal)
	}

	line := fmt.Sprintf("%s%s  %s  %s  %s%s  %s%s%s%s",
		marker,
		DimStyle.Render(fmt.Sprintf("%-10s", row.Source)),
		DimStyle.Render(fmt.Sprintf("%-7s", row.Scope)),
		title,
		stateStyle.Render("["+state+"]"),
		badge,
		DimStyle.Render(trigger),
		priority,
		goalProgress,
		runs,
	)
	if width > 0 && lipgloss.Width(line) > width {
		return lipgloss.NewStyle().MaxWidth(width).Render(line)
	}
	return line
}

// renderGoalProgress renders a compact block-character progress bar plus a
// done/total counter for a goal automation row. With no linked tasks it shows
// an empty bar and "0/0".
func renderGoalProgress(done, total int) string {
	const barWidth = 8
	if done < 0 {
		done = 0
	}
	if total < 0 {
		total = 0
	}
	if done > total {
		done = total
	}

	filled := 0
	if total > 0 {
		filled = done * barWidth / total
		if filled == 0 && done > 0 {
			filled = 1
		}
		if filled > barWidth {
			filled = barWidth
		}
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	barColor := ColorWaiting
	if total > 0 && done == total {
		barColor = ColorReady
	}

	styledBar := lipgloss.NewStyle().Foreground(barColor).Render(bar)
	counter := DimStyle.Render(fmt.Sprintf(" %d/%d", done, total))
	return styledBar + counter
}

// AutomationRowFromEntry converts a type=automation brain entry into a row.
func AutomationRowFromEntry(entry types.BrainEntry) AutomationListRow {
	row := AutomationListRow{
		ID:      entry.ID,
		Path:    entry.Path,
		Title:   entry.Title,
		Source:  "automation",
		Scope:   automationEntryScope(entry),
		Status:  entry.Status,
		Enabled: entry.Status == "active",
		IsGoal:  entry.GeneratedBy == types.GoalGeneratedBy,
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
		case "session":
			row.TriggerDetail = entry.Trigger.Event
			if row.TriggerDetail == "" {
				row.TriggerDetail = types.EventRunnerSessionDiscovered
			}
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
		Scope:    "project",
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
		Scope:    "project",
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

func automationEntryScope(entry types.BrainEntry) string {
	if strings.HasPrefix(entry.Path, "global/") {
		return "global"
	}
	if entry.ProjectID != "" || strings.HasPrefix(entry.Path, "projects/") {
		return "project"
	}
	return "unknown"
}

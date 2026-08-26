package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/types"
)

// TaskDetail displays detailed information about the currently selected task.
type TaskDetail struct {
	task          *types.ResolvedTask
	width, height int

	// Entry mode: reuses the same detail viewport for full brain entry content.
	entryMode          bool
	entryPath          string
	entryTitle         string
	entryType          string
	entryContent       string
	entryAttachments   []types.AttachmentReference
	selectedAttachment int
	entryLoading       bool
	entryErr           string
	entryHeader        string

	// Feature mode: when a feature header is selected instead of a task
	featureMode bool
	featureID   string
	feature     *service.ComputedFeature

	// Viewport scrolling state
	scrollOffset int // First visible line index (0-based)
	totalLines   int // Total content lines (set after rendering)
}

// NewTaskDetail creates a new empty TaskDetail component.
func NewTaskDetail() TaskDetail {
	return TaskDetail{}
}

// SetTask updates the displayed task. Only resets scroll position if the task changes.
// Clears feature mode (task and feature display are mutually exclusive).
func (td *TaskDetail) SetTask(task *types.ResolvedTask) {
	// Clear feature mode
	td.featureMode = false
	td.featureID = ""
	td.feature = nil
	td.entryMode = false
	td.entryPath = ""
	td.entryTitle = ""
	td.entryType = ""
	td.entryContent = ""
	td.entryAttachments = nil
	td.selectedAttachment = 0
	td.entryLoading = false
	td.entryErr = ""
	td.entryHeader = ""

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

// SetFeature switches to feature detail mode, showing aggregate feature info.
// Clears task mode (task and feature display are mutually exclusive).
func (td *TaskDetail) SetFeature(featureID string, feature *service.ComputedFeature) {
	// Clear task mode
	td.task = nil
	td.entryMode = false
	td.entryPath = ""
	td.entryTitle = ""
	td.entryType = ""
	td.entryContent = ""
	td.entryAttachments = nil
	td.selectedAttachment = 0
	td.entryLoading = false
	td.entryErr = ""
	td.entryHeader = ""

	// Only reset scroll when switching to a different feature
	oldID := td.featureID
	td.featureMode = true
	td.featureID = featureID
	td.feature = feature

	if oldID != featureID {
		td.scrollOffset = 0
		td.totalLines = 0
	}
}

// SetEntryLoading switches to brain entry detail mode while content is fetched.
func (td *TaskDetail) SetEntryLoading(entry types.BrainEntry, header ...string) {
	td.task = nil
	td.featureMode = false
	td.featureID = ""
	td.feature = nil

	oldPath := td.entryPath
	td.entryMode = true
	td.entryPath = entry.Path
	td.entryTitle = entry.Title
	td.entryType = entry.Type
	td.entryContent = ""
	td.entryAttachments = append([]types.AttachmentReference(nil), entry.Attachments...)
	td.clampSelectedAttachment()
	td.entryLoading = true
	td.entryErr = ""
	td.entryHeader = entryDetailHeader(header)
	if oldPath != entry.Path {
		td.scrollOffset = 0
		td.totalLines = 0
	}
}

// SetEntryContent displays full brain entry content in the detail viewport.
func (td *TaskDetail) SetEntryContent(path, title, entryType, content string, header ...string) {
	td.SetEntryContentWithAttachments(path, title, entryType, content, nil, header...)
}

// SetEntryContentWithAttachments displays full brain entry content with metadata-only attachment references.
func (td *TaskDetail) SetEntryContentWithAttachments(path, title, entryType, content string, attachments []types.AttachmentReference, header ...string) {
	oldPath := td.entryPath
	td.task = nil
	td.featureMode = false
	td.featureID = ""
	td.feature = nil
	td.entryMode = true
	td.entryPath = path
	td.entryTitle = title
	td.entryType = entryType
	td.entryContent = content
	td.entryAttachments = append([]types.AttachmentReference(nil), attachments...)
	td.clampSelectedAttachment()
	td.entryLoading = false
	td.entryErr = ""
	td.entryHeader = entryDetailHeader(header)
	td.totalLines = td.countEntryContentLines()
	if oldPath != path {
		td.scrollOffset = 0
	}
}

func (td *TaskDetail) HasEntryAttachments() bool {
	return td.entryMode && len(td.entryAttachments) > 0
}

func (td *TaskDetail) SelectedAttachment() (types.AttachmentReference, bool) {
	if !td.HasEntryAttachments() {
		return types.AttachmentReference{}, false
	}
	td.clampSelectedAttachment()
	return td.entryAttachments[td.selectedAttachment], true
}

func (td *TaskDetail) SelectNextAttachment() bool {
	if !td.HasEntryAttachments() {
		return false
	}
	td.selectedAttachment = (td.selectedAttachment + 1) % len(td.entryAttachments)
	return true
}

func (td *TaskDetail) SelectPrevAttachment() bool {
	if !td.HasEntryAttachments() {
		return false
	}
	td.selectedAttachment = (td.selectedAttachment + len(td.entryAttachments) - 1) % len(td.entryAttachments)
	return true
}

func (td *TaskDetail) SelectAttachmentAtEntryLine(line int) bool {
	idx := td.attachmentIndexAtEntryLine(line)
	if idx < 0 {
		return false
	}
	td.selectedAttachment = idx
	return true
}

func (td *TaskDetail) attachmentIndexAtEntryLine(line int) int {
	if !td.HasEntryAttachments() || line < 0 || td.entryLoading || td.entryErr != "" {
		return -1
	}
	contentLines := 1
	if td.entryContent != "" {
		contentLines = len(strings.Split(td.entryContent, "\n"))
	}
	attachmentLine := line - contentLines
	if attachmentLine < 2 {
		return -1
	}
	attachmentLine -= 2 // blank line + Attachments header
	for i, att := range td.entryAttachments {
		rowLines := 5 + len(attachmentExtractionLines(att)) + len(att.Derived)
		if len(att.Derived) > 0 {
			rowLines++
		}
		if attachmentLine < rowLines {
			return i
		}
		attachmentLine -= rowLines
	}
	return -1
}

func (td *TaskDetail) clampSelectedAttachment() {
	if len(td.entryAttachments) == 0 {
		td.selectedAttachment = 0
		return
	}
	if td.selectedAttachment < 0 {
		td.selectedAttachment = 0
	}
	if td.selectedAttachment >= len(td.entryAttachments) {
		td.selectedAttachment = len(td.entryAttachments) - 1
	}
}

// SetEntryError displays an entry fetch error in the detail viewport.
func (td *TaskDetail) SetEntryError(path, title, entryType string, err error, header ...string) {
	td.SetEntryContent(path, title, entryType, "", entryDetailHeader(header))
	if err != nil {
		td.entryErr = err.Error()
	}
}

func entryDetailHeader(header []string) string {
	if len(header) > 0 && header[0] != "" {
		return header[0]
	}
	return "Entry Detail"
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
	if td.featureMode && td.feature != nil {
		td.totalLines = td.countFeatureContentLines()
	} else if td.entryMode {
		td.totalLines = td.countEntryContentLines()
	} else if td.task != nil {
		td.totalLines = td.countContentLines()
	}
}

func (td *TaskDetail) countEntryContentLines() int {
	if !td.entryMode || td.entryLoading || td.entryErr != "" {
		return 1
	}
	if td.entryContent == "" {
		return 1 + len(td.renderEntryAttachmentLines())
	}
	return len(strings.Split(td.entryContent, "\n")) + len(td.renderEntryAttachmentLines())
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
	// Dispatch diagnostics
	dispatchLines := td.renderDispatchLines(task)
	if len(dispatchLines) > 0 {
		count++ // blank line
		count++ // header
		count += len(dispatchLines)
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
	// Content/body
	if strings.TrimSpace(task.Content) != "" {
		count++ // blank line
		count++ // header
		count += len(strings.Split(task.Content, "\n"))
	}
	// Frontmatter — compute actual count
	fmLines := td.renderFrontmatter(task)
	if len(fmLines) > 0 {
		count++ // blank line
		count++ // header
		count += len(fmLines)
	}
	// Direct prompt
	if task.DirectPrompt != "" {
		count += 2 // blank line + header
		prompt := task.DirectPrompt
		if len(prompt) > 200 {
			prompt = prompt[:200] + "..."
		}
		count += len(strings.Split(prompt, "\n"))
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
	if td.featureMode && td.feature != nil {
		return td.renderFeature()
	}
	if td.entryMode {
		return td.renderEntry()
	}
	if td.task == nil {
		return td.renderEmpty()
	}
	return td.renderTask()
}

func (td *TaskDetail) renderEntry() string {
	label := td.entryTitle
	if label == "" {
		label = td.entryPath
	}
	if td.entryType != "" {
		label = fmt.Sprintf("%s [%s]", label, td.entryType)
	}

	lines := []string{}
	if td.entryLoading {
		lines = append(lines, DimStyle.Render("Loading entry content..."))
	} else if td.entryErr != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(ColorBlocked).Render("Error: "+td.entryErr))
	} else if td.entryContent == "" {
		lines = append(lines, DimStyle.Render("Entry is empty"))
	} else {
		lines = strings.Split(td.entryContent, "\n")
	}
	lines = append(lines, td.renderEntryAttachmentLines()...)
	td.totalLines = len(lines)

	headerTitle := td.entryHeader
	if headerTitle == "" {
		headerTitle = "Entry Detail"
	}
	header := TitleStyle.Render(headerTitle)
	if label != "" {
		header = TitleStyle.Render(headerTitle + ": " + label)
	}

	if td.height > 0 && td.totalLines > td.height-1 {
		viewportHeight := td.height - 1
		if viewportHeight < 1 {
			viewportHeight = 1
		}
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
		header += DimStyle.Render(fmt.Sprintf(" (%d-%d/%d)", startLine, endLine, td.totalLines))

		end := td.scrollOffset + viewportHeight
		if end > td.totalLines {
			end = td.totalLines
		}
		visibleLines := make([]string, end-td.scrollOffset)
		copy(visibleLines, lines[td.scrollOffset:end])
		if td.scrollOffset > 0 && len(visibleLines) > 0 {
			visibleLines[0] = DimStyle.Render("▲ more above")
		}
		if end < td.totalLines && len(visibleLines) > 0 {
			visibleLines[len(visibleLines)-1] = DimStyle.Render("▼ more below")
		}
		return strings.Join(append([]string{header}, visibleLines...), "\n")
	}

	td.scrollOffset = 0
	return strings.Join(append([]string{header}, lines...), "\n")
}

func (td *TaskDetail) renderEntryAttachmentLines() []string {
	if len(td.entryAttachments) == 0 {
		return nil
	}

	lines := []string{"", fmt.Sprintf("Attachments (%d):", len(td.entryAttachments))}
	td.clampSelectedAttachment()
	for i, att := range td.entryAttachments {
		name := att.Filename
		if name == "" {
			name = "(unnamed)"
		}
		role := att.Role
		if role == "" {
			role = "attachment"
		}
		contentType := att.ContentType
		if contentType == "" {
			contentType = "unknown MIME"
		}
		size := formatAttachmentSize(att.Size)

		marker := " "
		if i == td.selectedAttachment {
			marker = ">"
		}
		lines = append(lines, fmt.Sprintf("  %s %s", marker, name))
		lines = append(lines, fmt.Sprintf("    Role: %s", role))
		lines = append(lines, fmt.Sprintf("    MIME: %s", contentType))
		lines = append(lines, fmt.Sprintf("    Size: %s", size))
		lines = append(lines, fmt.Sprintf("    ID: %s", att.ID))
		for _, extractionLine := range attachmentExtractionLines(att) {
			lines = append(lines, "    "+extractionLine)
		}

		for _, derived := range att.Derived {
			derivedType := derived.ContentType
			if derivedType == "" {
				derivedType = "unknown MIME"
			}
			lines = append(lines, fmt.Sprintf("    extracted: %s (%s, %s)", derived.Kind, derivedType, formatAttachmentSize(derived.Size)))
		}
		if len(att.Derived) > 0 {
			lines = append(lines, "    search: available")
		}
	}
	return lines
}

func formatAttachmentSize(size int64) string {
	if size <= 0 {
		return "unknown"
	}
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size) / unit
	if value < unit {
		return fmt.Sprintf("%.1f KB", value)
	}
	value /= unit
	if value < unit {
		return fmt.Sprintf("%.1f MB", value)
	}
	value /= unit
	return fmt.Sprintf("%.1f GB", value)
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

	// Dispatch diagnostics
	dispatchLines := td.renderDispatchLines(task)
	if len(dispatchLines) > 0 {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Underline(true).Render("Dispatch:"))
		lines = append(lines, dispatchLines...)
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

	// Frontmatter — all metadata fields for deep understanding of the task
	fmLines := td.renderFrontmatter(task)
	if len(fmLines) > 0 {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Underline(true).Render("Frontmatter:"))
		lines = append(lines, fmLines...)
	}

	// Cycle warning
	if task.InCycle {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(ColorBlocked).Bold(true).
			Render("↺ Task is part of a dependency cycle"))
	}

	// Direct prompt (truncated preview)
	if task.DirectPrompt != "" {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Underline(true).Render("Direct Prompt:"))
		prompt := task.DirectPrompt
		maxLen := 200
		if len(prompt) > maxLen {
			prompt = prompt[:maxLen] + "..."
		}
		// Split by newlines and indent
		for _, pLine := range strings.Split(prompt, "\n") {
			lines = append(lines, DimStyle.Render("  "+pLine))
		}
	}

	if strings.TrimSpace(task.Content) != "" {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Underline(true).Render("Content:"))
		lines = append(lines, strings.Split(task.Content, "\n")...)
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

func (td *TaskDetail) renderDispatchLines(task *types.ResolvedTask) []string {
	var lines []string
	if task.DispatchLease != nil {
		lease := task.DispatchLease
		lines = append(lines, fmt.Sprintf("  Lease: %s", lipgloss.NewStyle().Foreground(ColorCyan).Render(lease.State)))
		if lease.AssignedRunnerID != "" {
			lines = append(lines, fmt.Sprintf("  Runner: %s", lease.AssignedRunnerID))
		}
		if lease.AssignedMachineID != "" {
			lines = append(lines, fmt.Sprintf("  Machine: %s", lease.AssignedMachineID))
		}
		if lease.ExpiresAt != 0 {
			lines = append(lines, fmt.Sprintf("  Expires: %d", lease.ExpiresAt))
		}
		if lease.LastError != "" {
			lines = append(lines, fmt.Sprintf("  Last error: %s", lease.LastError))
		}
	}
	if task.LastPlacementReason != nil {
		reason := task.LastPlacementReason
		lines = append(lines, fmt.Sprintf("  Placement: %s", reason.Decision))
		if reason.Reason != "" {
			lines = append(lines, fmt.Sprintf("  Reason: %s", reason.Reason))
		}
		if reason.RunnerID != "" {
			lines = append(lines, fmt.Sprintf("  Placement runner: %s", reason.RunnerID))
		}
		if reason.MachineID != "" {
			lines = append(lines, fmt.Sprintf("  Placement machine: %s", reason.MachineID))
		}
	}
	return lines
}

// renderFrontmatter renders all metadata fields as key: value pairs.
// Only non-empty fields are shown. This matches origin/main's "Frontmatter:" section.
func (td *TaskDetail) renderFrontmatter(task *types.ResolvedTask) []string {
	var lines []string

	keyStyle := DimStyle
	valStyle := lipgloss.NewStyle()
	boolStyle := lipgloss.NewStyle().Foreground(ColorCyan)

	addField := func(key, val string) {
		if val != "" {
			lines = append(lines, fmt.Sprintf("  %s %s", keyStyle.Render(key+":"), valStyle.Render(val)))
		}
	}

	addBool := func(key string, val *bool) {
		if val != nil {
			lines = append(lines, fmt.Sprintf("  %s %s", keyStyle.Render(key+":"), boolStyle.Render(fmt.Sprintf("%v", *val))))
		}
	}

	// Core fields (not already shown above)
	addField("created", task.Created)
	addField("execution_mode", task.ExecutionMode)
	addField("user_original_request", task.UserOriginalRequest)
	addField("feature_id", task.FeatureID)
	addField("feature_priority", task.FeaturePriority)
	addField("git_branch", task.GitBranch)
	addField("git_remote", task.GitRemote)
	addField("merge_target_branch", task.MergeTargetBranch)
	addField("merge_policy", task.MergePolicy)
	addField("merge_strategy", task.MergeStrategy)
	addField("remote_branch_policy", task.RemoteBranchPolicy)
	addBool("open_pr_before_merge", task.OpenPRBeforeMerge)
	addField("target_workdir", task.TargetWorkdir)
	addField("workdir", task.Workdir)
	addField("resolved_workdir", task.ResolvedWorkdir)

	// Agent/model
	addField("agent", task.Agent)
	addField("model", task.Model)
	addBool("complete_on_idle", task.CompleteOnIdle)

	// Schedule
	addField("schedule", task.Schedule)
	addBool("schedule_enabled", task.ScheduleEnabled)
	addField("next_run", task.NextRun)
	if task.MaxRuns != nil {
		lines = append(lines, fmt.Sprintf("  %s %s", keyStyle.Render("max_runs:"), valStyle.Render(fmt.Sprintf("%d", *task.MaxRuns))))
	}
	if len(task.Runs) > 0 {
		lines = append(lines, fmt.Sprintf("  %s %s", keyStyle.Render("runs:"), valStyle.Render(fmt.Sprintf("%d total", len(task.Runs)))))
	}

	// Generated
	addBool("generated", task.Generated)
	addField("generated_kind", task.GeneratedKind)
	addField("generated_key", task.GeneratedKey)
	addField("generated_by", task.GeneratedBy)

	// Feature dependencies
	if len(task.FeatureDependsOn) > 0 {
		lines = append(lines, fmt.Sprintf("  %s %s", keyStyle.Render("feature_depends_on:"),
			valStyle.Render(strings.Join(task.FeatureDependsOn, ", "))))
	}

	// Env vars
	if len(task.Env) > 0 {
		lines = append(lines, fmt.Sprintf("  %s", keyStyle.Render("env:")))
		for k, v := range task.Env {
			lines = append(lines, fmt.Sprintf("    %s=%s", k, DimStyle.Render(v)))
		}
	}

	return lines
}

// featureTaskStatusIcon returns the icon for a task status within a feature view.
func featureTaskStatusIcon(status string) string {
	switch status {
	case "completed", "validated":
		return "✓"
	case "in_progress":
		return "⚡"
	case "blocked", "cancelled":
		return "✗"
	case "pending":
		return "◌"
	default:
		return "◌"
	}
}

// featureDepStatusIconSimple returns a simple icon for a pre-resolved feature dependency status.
// Used in taskdetail rendering where we don't have the full feature list.
func featureDepStatusIconSimple(status string) string {
	switch status {
	case "completed":
		return "✓"
	case "in_progress":
		return "◷"
	case "blocked":
		return "✗"
	case "pending":
		return "◌"
	default:
		return "◌"
	}
}

// countFeatureContentLines counts content lines for feature mode (excluding header).
func (td *TaskDetail) countFeatureContentLines() int {
	if td.feature == nil {
		return 0
	}
	f := td.feature
	count := 0

	// Status
	count++
	// Priority
	if f.Priority != "" {
		count++
	}
	// Task stats
	count++

	// Dependencies section
	if len(f.DependsOnFeatures) > 0 {
		count++ // blank line
		count++ // header
		count += len(f.DependsOnFeatures)
	}

	// Dependents section (blocked-by + waiting-on combined)
	hasDependents := len(f.BlockedByFeatures) > 0 || len(f.WaitingOnFeatures) > 0
	if hasDependents {
		count++ // blank line
		count++ // header
		count += len(f.BlockedByFeatures)
		count += len(f.WaitingOnFeatures)
	}

	// Tasks section
	if len(f.Tasks) > 0 {
		count++ // blank line
		count++ // header
		count += len(f.Tasks)
	}

	// Cycle warning
	if f.InCycle {
		count++ // blank line
		count++ // warning
	}

	return count
}

// renderFeature renders the feature detail view.
func (td *TaskDetail) renderFeature() string {
	f := td.feature
	var lines []string

	// Status
	statusLine := fmt.Sprintf("Status:      %s", StatusStyle(f.Classification).Render(f.Status))
	lines = append(lines, statusLine)

	// Priority
	if f.Priority != "" {
		priorityLine := fmt.Sprintf("Priority:    %s", PriorityStyle(f.Priority).Render(f.Priority))
		lines = append(lines, priorityLine)
	}

	// Task stats
	stats := f.TaskStats
	activeCount := stats.InProgress
	statsLine := fmt.Sprintf("Tasks:       %d/%d complete", stats.Completed, stats.Total)
	if activeCount > 0 {
		statsLine += fmt.Sprintf(" (%d active)", activeCount)
	}
	lines = append(lines, statsLine)

	// Dependencies (features this feature depends on)
	if len(f.DependsOnFeatures) > 0 {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Underline(true).Render("Dependencies:"))
		for _, depID := range f.DependsOnFeatures {
			// We don't have the dep feature objects directly, but we know if it's in
			// BlockedByFeatures or WaitingOnFeatures
			var icon string
			var style lipgloss.Style
			if sliceContains(f.BlockedByFeatures, depID) {
				icon = featureDepStatusIconSimple("blocked")
				style = lipgloss.NewStyle().Foreground(ColorBlocked)
			} else if sliceContains(f.WaitingOnFeatures, depID) {
				icon = featureDepStatusIconSimple("in_progress")
				style = lipgloss.NewStyle().Foreground(ColorWaiting)
			} else {
				// If not blocked/waiting, it must be completed
				icon = featureDepStatusIconSimple("completed")
				style = lipgloss.NewStyle().Foreground(ColorReady)
			}
			lines = append(lines, fmt.Sprintf("  %s %s", style.Render(icon), depID))
		}
	}

	// Dependents (features blocked by or waiting on this feature)
	hasDependents := len(f.BlockedByFeatures) > 0 || len(f.WaitingOnFeatures) > 0
	if hasDependents {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Underline(true).Render("Dependents:"))
		for _, depID := range f.BlockedByFeatures {
			icon := "✗"
			style := lipgloss.NewStyle().Foreground(ColorBlocked)
			lines = append(lines, fmt.Sprintf("  %s %s (blocked)", style.Render(icon), depID))
		}
		for _, depID := range f.WaitingOnFeatures {
			icon := "◌"
			style := lipgloss.NewStyle().Foreground(ColorWaiting)
			lines = append(lines, fmt.Sprintf("  %s %s (waiting on this)", style.Render(icon), depID))
		}
	}

	// Tasks within the feature
	if len(f.Tasks) > 0 {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Underline(true).Render("Tasks:"))
		for _, task := range f.Tasks {
			icon := featureTaskStatusIcon(task.Status)
			var style lipgloss.Style
			switch task.Status {
			case "completed", "validated":
				style = lipgloss.NewStyle().Foreground(ColorCompleted)
			case "in_progress":
				style = lipgloss.NewStyle().Foreground(ColorActive)
			case "blocked", "cancelled":
				style = lipgloss.NewStyle().Foreground(ColorBlocked)
			default:
				style = DimStyle
			}

			taskLine := fmt.Sprintf("  %s %s", style.Render(icon), task.Title)
			if task.Status == "in_progress" {
				taskLine += DimStyle.Render(" (in_progress)")
			}
			lines = append(lines, taskLine)
		}
	}

	// Cycle warning
	if f.InCycle {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(ColorBlocked).Bold(true).
			Render("↺ Feature is part of a dependency cycle"))
	}

	// Store total content lines
	td.totalLines = len(lines)

	// Build header: "── Feature: <name> ──────"
	featureTitle := fmt.Sprintf("── Feature: %s ", f.ID)
	remainingWidth := td.width - len(featureTitle) - 2
	if remainingWidth < 0 {
		remainingWidth = 0
	}
	headerText := featureTitle + strings.Repeat("─", remainingWidth)
	var headerLine string

	// Apply viewport scrolling (same logic as renderTask)
	if td.height > 0 && td.totalLines > td.height-1 {
		viewportHeight := td.height - 1
		if viewportHeight < 1 {
			viewportHeight = 1
		}

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

		headerLine = FeatureHeaderStyle.Render(headerText) +
			DimStyle.Render(fmt.Sprintf(" (%d-%d/%d)", startLine, endLine, td.totalLines))

		end := td.scrollOffset + viewportHeight
		if end > td.totalLines {
			end = td.totalLines
		}
		visibleLines := make([]string, end-td.scrollOffset)
		copy(visibleLines, lines[td.scrollOffset:end])

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

	// Content fits in viewport
	td.scrollOffset = 0
	headerLine = FeatureHeaderStyle.Render(headerText)
	var result []string
	result = append(result, headerLine)
	result = append(result, lines...)
	return strings.Join(result, "\n")
}

// sliceContains checks if a string slice contains a value.
func sliceContains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

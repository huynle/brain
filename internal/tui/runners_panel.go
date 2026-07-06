package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/huynle/brain-api/internal/types"
)

// RunnersPanel displays registered runners and their status.
type RunnersPanel struct {
	runners []types.RunnerInfo
	cursor  int
	width   int
	height  int
	scrollY int
}

// NewRunnersPanel creates a new RunnersPanel.
func NewRunnersPanel() RunnersPanel {
	return RunnersPanel{}
}

// SetRunners updates the runner list.
func (p *RunnersPanel) SetRunners(runners []types.RunnerInfo) {
	p.runners = runners
	// Keep cursor in bounds
	if p.cursor >= len(runners) {
		p.cursor = len(runners) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

// SetSize updates the panel dimensions.
func (p *RunnersPanel) SetSize(w, h int) {
	p.width = w
	p.height = h
}

// MoveUp moves the cursor up.
func (p *RunnersPanel) MoveUp() {
	if p.cursor > 0 {
		p.cursor--
	}
}

// MoveDown moves the cursor down.
func (p *RunnersPanel) MoveDown() {
	if p.cursor < len(p.runners)-1 {
		p.cursor++
	}
}

// GotoTop moves the cursor to the first runner.
func (p *RunnersPanel) GotoTop() {
	p.cursor = 0
	p.scrollY = 0
}

// GotoBottom moves the cursor to the last runner.
func (p *RunnersPanel) GotoBottom() {
	if len(p.runners) > 0 {
		p.cursor = len(p.runners) - 1
	}
}

// SelectedRunner returns the currently selected runner, or nil.
func (p *RunnersPanel) SelectedRunner() *types.RunnerInfo {
	if p.cursor >= 0 && p.cursor < len(p.runners) {
		return &p.runners[p.cursor]
	}
	return nil
}

// ContentHeight returns the total content lines (for layout calculations).
func (p *RunnersPanel) ContentHeight() int {
	if len(p.runners) == 0 {
		return 3 // "No runners registered" message
	}
	// Header (2 lines) + one line per runner + spacing
	return len(p.runners) + 3
}

// View renders the runners panel.
func (p *RunnersPanel) View(width, height int) string {
	if width < 10 {
		width = 10
	}
	if height < 1 {
		height = 1
	}

	if len(p.runners) == 0 {
		msg := DimStyle.Render("No runners registered")
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, msg)
	}

	var lines []string

	// Header line
	header := p.renderHeader(width)
	lines = append(lines, header)
	lines = append(lines, DimStyle.Render(strings.Repeat("─", width)))

	// Runner rows
	for i, r := range p.runners {
		row := p.renderRunnerRow(r, i == p.cursor, width)
		lines = append(lines, row)
	}

	// Summary line
	lines = append(lines, "")
	online, lost := p.countByStatus()
	summary := DimStyle.Render(fmt.Sprintf(" Total: %d  Online: %d  Lost: %d", len(p.runners), online, lost))
	lines = append(lines, summary)

	content := strings.Join(lines, "\n")

	// Truncate to fit
	contentLines := strings.Split(content, "\n")
	if len(contentLines) > height {
		// Ensure cursor is visible
		if p.cursor+2 >= p.scrollY+height {
			p.scrollY = p.cursor + 3 - height
		}
		if p.cursor+2 < p.scrollY {
			p.scrollY = p.cursor
		}
		if p.scrollY < 0 {
			p.scrollY = 0
		}
		end := p.scrollY + height
		if end > len(contentLines) {
			end = len(contentLines)
		}
		contentLines = contentLines[p.scrollY:end]
		content = strings.Join(contentLines, "\n")
	}

	return content
}

// renderHeader renders the column headers.
func (p *RunnersPanel) renderHeader(width int) string {
	bold := BoldStyle

	// Column layout: Status | Runner ID | Hostname | Tasks | Assigned | Capabilities | Heartbeat
	cols := []struct {
		label string
		width int
	}{
		{"Status", 8},
		{"Runner ID", 20},
		{"Hostname", 16},
		{"Tasks", 10},
		{"Assigned", 18},
		{"Capabilities", 20},
		{"Heartbeat", 14},
	}

	var parts []string
	for _, col := range cols {
		label := col.label
		if len(label) > col.width {
			label = label[:col.width]
		}
		parts = append(parts, bold.Render(fmt.Sprintf("%-*s", col.width, label)))
	}

	return " " + strings.Join(parts, " ")
}

// renderRunnerRow renders a single runner row.
func (p *RunnersPanel) renderRunnerRow(r types.RunnerInfo, selected bool, width int) string {
	// Status indicator
	var statusIndicator string
	var statusStyle lipgloss.Style
	switch r.Status {
	case "online":
		statusIndicator = IndicatorConnected
		statusStyle = lipgloss.NewStyle().Foreground(ColorReady)
	case "lost":
		statusIndicator = "✗"
		statusStyle = lipgloss.NewStyle().Foreground(ColorBlocked)
	default:
		statusIndicator = IndicatorWaiting
		statusStyle = lipgloss.NewStyle().Foreground(ColorWaiting)
	}

	statusCol := statusStyle.Render(fmt.Sprintf("%-8s", statusIndicator+" "+string(r.Status)))

	// Runner ID (truncate if needed)
	runnerID := r.RunnerID
	if len(runnerID) > 20 {
		runnerID = runnerID[:17] + "..."
	}
	runnerIDCol := fmt.Sprintf("%-20s", runnerID)

	// Hostname
	hostname := r.Hostname
	if len(hostname) > 16 {
		hostname = hostname[:13] + "..."
	}
	hostnameCol := fmt.Sprintf("%-16s", hostname)

	// Tasks utilization
	utilPct := 0
	if r.MaxParallel > 0 {
		utilPct = r.ActiveTasks * 100 / r.MaxParallel
	}
	var tasksStyle lipgloss.Style
	switch {
	case utilPct >= 80:
		tasksStyle = lipgloss.NewStyle().Foreground(ColorBlocked)
	case utilPct >= 50:
		tasksStyle = lipgloss.NewStyle().Foreground(ColorWaiting)
	default:
		tasksStyle = lipgloss.NewStyle().Foreground(ColorReady)
	}
	tasksCol := tasksStyle.Render(fmt.Sprintf("%-10s", fmt.Sprintf("%d/%d %d%%", r.ActiveTasks, r.MaxParallel, utilPct)))

	assignments := formatRunnerAssignments(r.FeatureAssignments, false)
	if assignments == "" {
		assignments = "-"
	}
	if len(assignments) > 18 {
		assignments = assignments[:15] + "..."
	}
	assignmentCol := DimStyle.Render(fmt.Sprintf("%-18s", assignments))

	// Capabilities
	caps := strings.Join(r.Capabilities, ",")
	if len(caps) > 20 {
		caps = caps[:17] + "..."
	}
	if caps == "" {
		caps = "-"
	}
	capsCol := DimStyle.Render(fmt.Sprintf("%-20s", caps))

	// Heartbeat age
	heartbeatCol := p.renderHeartbeatAge(r.LastHeartbeat)

	row := fmt.Sprintf(" %s %s %s %s %s %s %s", statusCol, runnerIDCol, hostnameCol, tasksCol, assignmentCol, capsCol, heartbeatCol)

	if selected {
		row = GetSelectedRowStyle().Width(width).Render(row)
	}

	return row
}

// renderHeartbeatAge renders the time since last heartbeat.
func (p *RunnersPanel) renderHeartbeatAge(lastHeartbeat string) string {
	if lastHeartbeat == "" {
		return DimStyle.Render("never")
	}

	t, err := time.Parse(time.RFC3339, lastHeartbeat)
	if err != nil {
		// Try alternate format
		t, err = time.Parse("2006-01-02T15:04:05Z", lastHeartbeat)
		if err != nil {
			return DimStyle.Render("unknown")
		}
	}

	age := time.Since(t)
	var ageStr string
	switch {
	case age < time.Minute:
		ageStr = fmt.Sprintf("%ds ago", int(age.Seconds()))
	case age < time.Hour:
		ageStr = fmt.Sprintf("%dm ago", int(age.Minutes()))
	default:
		ageStr = fmt.Sprintf("%dh ago", int(age.Hours()))
	}

	// Color based on age — stale runners get red
	if age > 2*time.Minute {
		return lipgloss.NewStyle().Foreground(ColorBlocked).Render(ageStr)
	}
	if age > time.Minute {
		return lipgloss.NewStyle().Foreground(ColorWaiting).Render(ageStr)
	}
	return DimStyle.Render(ageStr)
}

// countByStatus returns the count of online and lost runners.
func (p *RunnersPanel) countByStatus() (online, lost int) {
	for _, r := range p.runners {
		switch r.Status {
		case "online":
			online++
		case "lost":
			lost++
		}
	}
	return
}

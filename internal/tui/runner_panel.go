package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/huynle/brain-api/internal/types"
)

// RunnerPanel displays registered runners with status, executors, and workload.
type RunnerPanel struct {
	runners    []types.RunnerInfo
	cursor     int
	width      int
	height     int
	scrollTop  int
	lastUpdate time.Time
}

// NewRunnerPanel creates a new RunnerPanel.
func NewRunnerPanel() RunnerPanel {
	return RunnerPanel{}
}

// SetRunners updates the runner list.
func (rp *RunnerPanel) SetRunners(runners []types.RunnerInfo) {
	rp.runners = runners
	rp.lastUpdate = time.Now()
	// Clamp cursor
	if rp.cursor >= len(runners) {
		rp.cursor = len(runners) - 1
	}
	if rp.cursor < 0 {
		rp.cursor = 0
	}
}

// SetSize sets the panel dimensions.
func (rp *RunnerPanel) SetSize(width, height int) {
	rp.width = width
	rp.height = height
}

// MoveDown moves the cursor down.
func (rp *RunnerPanel) MoveDown() {
	if rp.cursor < len(rp.runners)-1 {
		rp.cursor++
		rp.ensureVisible()
	}
}

// MoveUp moves the cursor up.
func (rp *RunnerPanel) MoveUp() {
	if rp.cursor > 0 {
		rp.cursor--
		rp.ensureVisible()
	}
}

// ensureVisible scrolls viewport to keep cursor visible.
func (rp *RunnerPanel) ensureVisible() {
	// Each runner takes 1 line in list view
	visibleLines := rp.height - 3 // header + blank + footer buffer
	if visibleLines < 1 {
		visibleLines = 1
	}
	if rp.cursor < rp.scrollTop {
		rp.scrollTop = rp.cursor
	}
	if rp.cursor >= rp.scrollTop+visibleLines {
		rp.scrollTop = rp.cursor - visibleLines + 1
	}
}

// SelectedRunner returns the currently selected runner, or nil if none.
func (rp *RunnerPanel) SelectedRunner() *types.RunnerInfo {
	if len(rp.runners) == 0 || rp.cursor < 0 || rp.cursor >= len(rp.runners) {
		return nil
	}
	r := rp.runners[rp.cursor]
	return &r
}

// View renders the runner panel.
func (rp RunnerPanel) View(width, height int) string {
	if width < 10 {
		width = 10
	}
	if height < 3 {
		height = 3
	}

	var b strings.Builder

	// Header
	title := TitleStyle.Render(fmt.Sprintf("Runners (%d)", len(rp.runners)))
	b.WriteString(title)
	b.WriteString("\n")

	if len(rp.runners) == 0 {
		b.WriteString(DimStyle.Render("  No runners registered"))
		return b.String()
	}

	// Column header
	headerLine := fmt.Sprintf("  %-14s %-10s %-8s %-6s %-6s %s",
		"ID", "Host", "Status", "Tasks", "Max", "Executors")
	b.WriteString(DimStyle.Render(headerLine))
	b.WriteString("\n")

	// Runner list
	listHeight := height - 2 // minus header + column header
	if listHeight < 1 {
		listHeight = 1
	}

	endIdx := rp.scrollTop + listHeight
	if endIdx > len(rp.runners) {
		endIdx = len(rp.runners)
	}

	for i := rp.scrollTop; i < endIdx; i++ {
		runner := rp.runners[i]
		line := rp.renderRunnerLine(runner, width)

		if i == rp.cursor {
			line = SelectedRowStyle.Width(width).Render(line)
		}

		b.WriteString(line)
		if i < endIdx-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

// ViewDetail renders detail for the selected runner.
func (rp RunnerPanel) ViewDetail(width, height int) string {
	runner := rp.SelectedRunner()
	if runner == nil {
		return DimStyle.Render("No runner selected")
	}

	var b strings.Builder

	// Title
	b.WriteString(TitleStyle.Render("Runner: "+runner.RunnerID) + "\n\n")

	// Status with color
	statusStyle := rp.statusStyle(runner.Status)
	b.WriteString(fmt.Sprintf("  Status:        %s\n", statusStyle.Render(string(runner.Status))))
	b.WriteString(fmt.Sprintf("  Hostname:      %s\n", runner.Hostname))
	b.WriteString(fmt.Sprintf("  Max Parallel:  %d\n", runner.MaxParallel))

	// Executors
	if len(runner.Executors) > 0 {
		b.WriteString(fmt.Sprintf("  Executors:     %s\n", strings.Join(runner.Executors, ", ")))
	} else {
		b.WriteString(fmt.Sprintf("  Executors:     %s\n", DimStyle.Render("none")))
	}

	// Feature affinity
	if runner.FeatureIDs != "" {
		b.WriteString(fmt.Sprintf("  Features:      %s\n", runner.FeatureIDs))
	}

	// Labels
	if len(runner.Labels) > 0 {
		labelParts := make([]string, 0, len(runner.Labels))
		for k, v := range runner.Labels {
			labelParts = append(labelParts, fmt.Sprintf("%s=%s", k, v))
		}
		b.WriteString(fmt.Sprintf("  Labels:        %s\n", strings.Join(labelParts, ", ")))
	}

	// Timestamps
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  Registered:    %s\n", rp.formatTimestamp(runner.RegisteredAt)))
	b.WriteString(fmt.Sprintf("  Last Beat:     %s\n", rp.formatTimestamp(runner.LastHeartbeat)))

	// Uptime
	if runner.RegisteredAt != "" {
		if registered, err := time.Parse(time.RFC3339, runner.RegisteredAt); err == nil {
			uptime := time.Since(registered).Truncate(time.Second)
			b.WriteString(fmt.Sprintf("  Uptime:        %s\n", uptime.String()))
		}
	}

	return b.String()
}

// renderRunnerLine renders a single runner as a compact line.
func (rp RunnerPanel) renderRunnerLine(runner types.RunnerInfo, width int) string {
	statusIndicator := rp.statusIndicator(runner.Status)

	// Truncate runner ID if needed
	id := runner.RunnerID
	if len(id) > 14 {
		id = id[:11] + "..."
	}

	// Truncate hostname
	host := runner.Hostname
	if len(host) > 10 {
		host = host[:7] + "..."
	}

	// Executors summary
	executors := DimStyle.Render("none")
	if len(runner.Executors) > 0 {
		executors = strings.Join(runner.Executors, ",")
		if len(executors) > 20 {
			executors = executors[:17] + "..."
		}
	}

	// Running tasks (from heartbeat stats, if available)
	runningTasks := "-"

	return fmt.Sprintf("  %s %-14s %-10s %-8s %-6s %-6d %s",
		statusIndicator,
		id,
		host,
		string(runner.Status),
		runningTasks,
		runner.MaxParallel,
		executors,
	)
}

// statusIndicator returns a colored status dot for a runner.
func (rp RunnerPanel) statusIndicator(status types.RunnerStatus) string {
	switch status {
	case types.RunnerStatusOnline:
		return lipgloss.NewStyle().Foreground(ColorReady).Render(IndicatorConnected) // green dot
	case types.RunnerStatusStale:
		return lipgloss.NewStyle().Foreground(ColorWaiting).Render(IndicatorConnected) // yellow dot
	case types.RunnerStatusOffline:
		return lipgloss.NewStyle().Foreground(ColorBlocked).Render(IndicatorDisconn) // red circle
	default:
		return lipgloss.NewStyle().Foreground(ColorDim).Render(IndicatorDisconn) // gray circle
	}
}

// statusStyle returns a style for a runner status.
func (rp RunnerPanel) statusStyle(status types.RunnerStatus) lipgloss.Style {
	switch status {
	case types.RunnerStatusOnline:
		return lipgloss.NewStyle().Foreground(ColorReady).Bold(true)
	case types.RunnerStatusStale:
		return lipgloss.NewStyle().Foreground(ColorWaiting).Bold(true)
	case types.RunnerStatusOffline:
		return lipgloss.NewStyle().Foreground(ColorBlocked).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(ColorDim)
	}
}

// formatTimestamp formats an RFC3339 timestamp to a relative "time ago" or short form.
func (rp RunnerPanel) formatTimestamp(ts string) string {
	if ts == "" {
		return DimStyle.Render("never")
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	ago := time.Since(t)
	switch {
	case ago < time.Minute:
		return fmt.Sprintf("%ds ago", int(ago.Seconds()))
	case ago < time.Hour:
		return fmt.Sprintf("%dm ago", int(ago.Minutes()))
	case ago < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(ago.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(ago.Hours()/24))
	}
}

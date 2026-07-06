package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// MaxProjectNameLength is the maximum characters for a project name before truncation.
const MaxProjectNameLength = 15

// ProjectTabs manages project tab state and rendering for multi-project mode.
type ProjectTabs struct {
	Projects       []string // All project IDs
	ActiveIndex    int      // 0 = "All", 1+ = specific project
	StatsByProject map[string]TaskStats
	AggregateStats TaskStats
}

// NewProjectTabs creates a new ProjectTabs with the given projects.
func NewProjectTabs(projects []string) ProjectTabs {
	return ProjectTabs{
		Projects:       projects,
		ActiveIndex:    0, // Start with "All" tab
		StatsByProject: make(map[string]TaskStats),
	}
}

// ActiveProject returns the currently active project ID, or "all" if on the All tab.
func (p ProjectTabs) ActiveProject() string {
	if p.ActiveIndex == 0 {
		return "all"
	}
	if p.ActiveIndex > 0 && p.ActiveIndex <= len(p.Projects) {
		return p.Projects[p.ActiveIndex-1]
	}
	return "all"
}

// SetActiveProject sets the active tab by project ID ("all" or specific project).
func (p *ProjectTabs) SetActiveProject(projectID string) {
	if projectID == "all" {
		p.ActiveIndex = 0
		return
	}
	for i, proj := range p.Projects {
		if proj == projectID {
			p.ActiveIndex = i + 1
			return
		}
	}
}

// NextTab cycles to the next tab (wraps around).
func (p *ProjectTabs) NextTab() {
	p.ActiveIndex = (p.ActiveIndex + 1) % (len(p.Projects) + 1)
}

// PrevTab cycles to the previous tab (wraps around).
func (p *ProjectTabs) PrevTab() {
	p.ActiveIndex--
	if p.ActiveIndex < 0 {
		p.ActiveIndex = len(p.Projects)
	}
}

// JumpToTab jumps to tab by number (1 = "All", 2 = first project, etc.).
// Returns false if the tab number is out of range.
func (p *ProjectTabs) JumpToTab(tabNum int) bool {
	if tabNum < 1 || tabNum > len(p.Projects)+1 {
		return false
	}
	p.ActiveIndex = tabNum - 1
	return true
}

// UpdateStats updates the stats for a specific project and recalculates aggregate.
func (p *ProjectTabs) UpdateStats(projectID string, stats TaskStats) {
	p.StatsByProject[projectID] = stats
	p.recalculateAggregate()
}

// recalculateAggregate computes aggregate stats from all projects.
func (p *ProjectTabs) recalculateAggregate() {
	agg := TaskStats{}
	for _, stats := range p.StatsByProject {
		agg.Ready += stats.Ready
		agg.Waiting += stats.Waiting
		agg.Blocked += stats.Blocked
		agg.InProgress += stats.InProgress
		agg.Completed += stats.Completed
	}
	p.AggregateStats = agg
}

// CurrentStats returns the stats for the currently active project or aggregate.
func (p ProjectTabs) CurrentStats() TaskStats {
	if p.ActiveIndex == 0 {
		return p.AggregateStats
	}
	projectID := p.ActiveProject()
	if stats, ok := p.StatsByProject[projectID]; ok {
		return stats
	}
	return TaskStats{}
}

// truncateName truncates a project name if too long.
func truncateName(name string, maxLength int) string {
	if len(name) <= maxLength {
		return name
	}
	return name[:maxLength-2] + ".."
}

// getProjectIndicator returns an activity indicator and color for a project.
// Priority: in_progress (▶ blue) > blocked (✗ red) > ready (● green)
func getProjectIndicator(stats TaskStats) (string, lipgloss.Color) {
	if stats.InProgress > 0 {
		return IndicatorActive, ColorActive // ▶ blue
	}
	if stats.Blocked > 0 {
		return IndicatorBlocked, ColorBlocked // ✗ red
	}
	if stats.Ready > 0 {
		return IndicatorReady, ColorReady // ● green
	}
	return "", ColorDim
}

// ActiveTabStyle is used for the currently selected tab.
var ActiveTabStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("0")). // Black text
	Background(ColorCyan)            // Cyan background

// InactiveTabStyle is used for non-selected tabs.
var InactiveTabStyle = lipgloss.NewStyle().
	Foreground(ColorDim)

// View renders the tab bar as a single-line string.
func (p ProjectTabs) View(width int) string {
	// The project-tab row is intentionally not rendered: projects are switched
	// with H/L (shift) and the active project is shown in the status bar. This
	// keeps every content tab visually clean (matching the Automations view).
	// ProjectTabs still tracks active index + per-project stats for H/L nav.
	return ""
}

// TabIndexAt returns the tab index at a rendered x coordinate, or -1 if none.
func (p ProjectTabs) TabIndexAt(x, width int) int {
	if len(p.Projects) <= 1 {
		return -1
	}
	tabs := p.tabLabels(false)
	pos := 0
	for i, tab := range tabs {
		tabWidth := lipgloss.Width(tab)
		if pos+tabWidth > width {
			return -1
		}
		if x >= pos && x < pos+tabWidth {
			return i
		}
		pos += tabWidth + 2 // strings.Join separator
	}
	return -1
}

func (p ProjectTabs) render(width int, styled bool) string {
	tabs := p.tabLabels(styled)
	tabLine := strings.Join(tabs, "  ")

	// Truncate if too wide
	if lipgloss.Width(tabLine) > width {
		tabLine = tabLine[:width-3] + "..."
	}

	return tabLine
}

func (p ProjectTabs) tabLabels(styled bool) []string {

	var tabs []string

	// "All" tab
	allIndicator, allColor := getProjectIndicator(p.AggregateStats)
	allLabel := fmt.Sprintf("All (%d)", p.AggregateStats.Ready+p.AggregateStats.Waiting+p.AggregateStats.InProgress)
	allText := allLabel
	if allIndicator != "" {
		allText = lipgloss.NewStyle().Foreground(allColor).Render(allIndicator) + " " + allLabel
	}
	if !styled {
		tabs = append(tabs, "["+allText+"]")
	} else if p.ActiveIndex == 0 {
		tabs = append(tabs, ActiveTabStyle.Render("["+allText+"]"))
	} else {
		tabs = append(tabs, InactiveTabStyle.Render("["+allText+"]"))
	}

	// Project tabs
	for i, projectID := range p.Projects {
		stats := p.StatsByProject[projectID]
		indicator, indicatorColor := getProjectIndicator(stats)
		truncated := truncateName(projectID, MaxProjectNameLength)
		taskCount := stats.Ready + stats.Waiting + stats.InProgress
		label := fmt.Sprintf("%s (%d)", truncated, taskCount)
		tabText := label
		if indicator != "" {
			tabText = lipgloss.NewStyle().Foreground(indicatorColor).Render(indicator) + " " + label
		}
		if !styled {
			tabs = append(tabs, "["+tabText+"]")
		} else if p.ActiveIndex == i+1 {
			tabs = append(tabs, ActiveTabStyle.Render("["+tabText+"]"))
		} else {
			tabs = append(tabs, InactiveTabStyle.Render("["+tabText+"]"))
		}
	}

	return tabs
}

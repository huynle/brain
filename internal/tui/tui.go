package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Model is the root Bubble Tea model for the TUI dashboard.
type Model struct {
	config Config
	keymap KeyMap

	// Sub-models
	statusBar StatusBar
	helpBar   HelpBar

	// State
	activePanel   Panel
	connected     bool
	width, height int

	// Visibility toggles for bottom panels
	detailVisible bool
	logsVisible   bool

	// Task data (placeholder for Phase 2)
	stats TaskStats
}

// NewModel creates a new TUI model with the given configuration.
func NewModel(cfg Config) Model {
	return Model{
		config:      cfg,
		keymap:      DefaultKeyMap(),
		statusBar:   NewStatusBar(cfg.Project),
		helpBar:     NewHelpBar(),
		activePanel: PanelTasks,
	}
}

// Init implements tea.Model. Returns nil in Phase 1 (no initial commands).
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model. Handles messages and returns updated model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case TasksUpdatedMsg:
		if msg.Stats != nil {
			m.stats = TaskStatsFromAPI(msg.Stats)
			m.statusBar.Stats = m.stats
		}
		return m, nil

	case SSEConnectedMsg:
		m.connected = true
		m.statusBar.Connected = true
		return m, nil

	case SSEDisconnectedMsg:
		m.connected = false
		m.statusBar.Connected = false
		return m, nil
	}

	return m, nil
}

// handleKeyMsg processes keyboard input.
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit

	case tea.KeyTab:
		m.activePanel = NextPanel(m.activePanel, m.detailVisible, m.logsVisible)
		m.helpBar.ActivePanel = m.activePanel
		return m, nil

	case tea.KeyRunes:
		switch string(msg.Runes) {
		case "q":
			return m, tea.Quit
		case "r":
			// Refresh - will be implemented in Phase 2
			return m, nil
		}
	}

	return m, nil
}

// View implements tea.Model. Renders the TUI layout.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing..."
	}

	// Render status bar at top
	statusBarView := m.statusBar.View(m.width)

	// Calculate available height for main content
	// StatusBar: ~3 lines, HelpBar: 1 line
	mainHeight := m.height - 4
	if mainHeight < 3 {
		mainHeight = 3
	}

	// Main content area: task tree placeholder (full width)
	taskPanelStyle := InactiveBorder
	if m.activePanel == PanelTasks {
		taskPanelStyle = ActiveBorder
	}

	taskContent := fmt.Sprintf(" Tasks (%d ready, %d total)",
		m.stats.Ready,
		m.stats.Ready+m.stats.Waiting+m.stats.InProgress+m.stats.Completed+m.stats.Blocked)

	taskPanel := taskPanelStyle.
		Width(m.width - 2).
		Height(mainHeight).
		Render(taskContent)

	// Help bar at bottom
	helpBarView := m.helpBar.View(m.width, m.config.IsMultiProject())

	// Compose layout vertically
	return lipgloss.JoinVertical(lipgloss.Left,
		statusBarView,
		taskPanel,
		helpBarView,
	)
}

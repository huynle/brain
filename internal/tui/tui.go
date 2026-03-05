package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/huynle/brain-api/internal/types"
)

// DefaultReconnectDelay is the default delay before reconnecting after disconnect.
const DefaultReconnectDelay = 3 * time.Second

// Model is the root Bubble Tea model for the TUI dashboard.
type Model struct {
	config Config
	keymap KeyMap

	// Sub-models
	statusBar StatusBar
	helpBar   HelpBar
	taskTree  TaskTree

	// SSE client
	sseClient *SSEClient
	ctx       context.Context

	// State
	activePanel   Panel
	connected     bool
	width, height int

	// Visibility toggles for bottom panels
	detailVisible bool
	logsVisible   bool

	// Task data
	tasks []types.ResolvedTask
	stats TaskStats
}

// NewModel creates a new TUI model with the given configuration.
func NewModel(cfg Config) Model {
	return Model{
		config:      cfg,
		keymap:      DefaultKeyMap(),
		statusBar:   NewStatusBar(cfg.Project),
		helpBar:     NewHelpBar(),
		taskTree:    NewTaskTree(),
		activePanel: PanelTasks,
		sseClient:   NewSSEClient(cfg.APIURL, cfg.Project),
		ctx:         context.Background(),
	}
}

// NewModelWithContext creates a new TUI model with a custom context.
// Use this when you need to control the SSE connection lifecycle.
func NewModelWithContext(cfg Config, ctx context.Context) Model {
	m := NewModel(cfg)
	m.ctx = ctx
	return m
}

// Init implements tea.Model. Starts the SSE connection on startup.
func (m Model) Init() tea.Cmd {
	return m.sseClient.Connect(m.ctx)
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
		m.tasks = msg.Tasks
		if msg.Stats != nil {
			m.stats = TaskStatsFromAPI(msg.Stats)
			m.statusBar.Stats = m.stats
		}
		// Update task tree with new data
		m.taskTree.SetTasks(msg.Tasks)
		// Continue listening for next SSE message
		return m, m.sseClient.WaitForNextMsg()

	case SSEConnectedMsg:
		m.connected = true
		m.statusBar.Connected = true
		// Continue listening for next SSE message
		return m, m.sseClient.WaitForNextMsg()

	case SSEDisconnectedMsg:
		m.connected = false
		m.statusBar.Connected = false
		// Schedule reconnect
		return m, m.sseClient.Reconnect(DefaultReconnectDelay)

	case SSEErrorMsg:
		// Log error, stay connected, continue listening
		return m, m.sseClient.WaitForNextMsg()

	case reconnectMsg:
		// Stop old client and create a new one for reconnection
		m.sseClient.Stop()
		m.sseClient = NewSSEClient(m.config.APIURL, m.config.Project)
		return m, m.sseClient.Connect(m.ctx)
	}

	return m, nil
}

// handleKeyMsg processes keyboard input.
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		m.sseClient.Stop()
		return m, tea.Quit

	case tea.KeyTab:
		m.activePanel = NextPanel(m.activePanel, m.detailVisible, m.logsVisible)
		m.helpBar.ActivePanel = m.activePanel
		return m, nil

	case tea.KeyRunes:
		switch string(msg.Runes) {
		case "q":
			m.sseClient.Stop()
			return m, tea.Quit
		case "r":
			// Refresh: reconnect SSE to get fresh snapshot
			m.sseClient.Stop()
			m.sseClient = NewSSEClient(m.config.APIURL, m.config.Project)
			return m, m.sseClient.Connect(m.ctx)
		case "j":
			if m.activePanel == PanelTasks {
				m.taskTree.MoveDown()
			}
			return m, nil
		case "k":
			if m.activePanel == PanelTasks {
				m.taskTree.MoveUp()
			}
			return m, nil
		case "g":
			if m.activePanel == PanelTasks {
				m.taskTree.MoveToTop()
			}
			return m, nil
		case "G":
			if m.activePanel == PanelTasks {
				m.taskTree.MoveToBottom()
			}
			return m, nil
		}

	case tea.KeyUp:
		if m.activePanel == PanelTasks {
			m.taskTree.MoveUp()
		}
		return m, nil

	case tea.KeyDown:
		if m.activePanel == PanelTasks {
			m.taskTree.MoveDown()
		}
		return m, nil
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

	// Main content area: task tree (full width)
	taskPanelStyle := InactiveBorder
	if m.activePanel == PanelTasks {
		taskPanelStyle = ActiveBorder
	}

	// Render task tree content
	innerWidth := m.width - 4     // account for border + padding
	innerHeight := mainHeight - 2 // account for border
	if innerWidth < 10 {
		innerWidth = 10
	}
	if innerHeight < 1 {
		innerHeight = 1
	}

	taskContent := m.taskTree.View(innerWidth, innerHeight)

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

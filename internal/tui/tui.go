package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/huynle/brain-api/internal/types"
)

// DefaultReconnectDelay is the default delay before reconnecting after disconnect.
const DefaultReconnectDelay = 3 * time.Second

// DefaultMaxLogEntries is the default maximum number of log entries to keep.
const DefaultMaxLogEntries = 500

// Model is the root Bubble Tea model for the TUI dashboard.
type Model struct {
	config Config
	keymap KeyMap

	// Sub-models
	statusBar  StatusBar
	helpBar    HelpBar
	taskTree   TaskTree
	taskDetail TaskDetail
	logViewer  LogViewer

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

	// Filter state
	filterMode   bool   // Is filter input active?
	filterQuery  string // Current filter text
	filterActive bool   // Is a filter currently applied?

	// Task data
	tasks []types.ResolvedTask
	stats TaskStats

	// Multi-select state
	selectedTasks map[string]bool

	// Resource metrics
	metricsCollector *MetricsCollector
	resourceMetrics  ResourceMetrics
}

// NewModel creates a new TUI model with the given configuration.
func NewModel(cfg Config) Model {
	return Model{
		config:           cfg,
		keymap:           DefaultKeyMap(),
		statusBar:        NewStatusBar(cfg.Project),
		helpBar:          NewHelpBar(),
		taskTree:         NewTaskTree(),
		taskDetail:       NewTaskDetail(),
		logViewer:        NewLogViewer(DefaultMaxLogEntries),
		activePanel:      PanelTasks,
		sseClient:        NewSSEClient(cfg.APIURL, cfg.Project),
		ctx:              context.Background(),
		selectedTasks:    make(map[string]bool),
		metricsCollector: NewMetricsCollector(),
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
	return tea.Batch(
		m.sseClient.Connect(m.ctx),
		tickCmd(),
	)
}

// tickCmd returns a command that sends a TickMsg every 2 seconds.
func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return TickMsg{}
	})
}

// Update implements tea.Model. Handles messages and returns updated model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalcPanelSizes()
		return m, nil

	case TasksUpdatedMsg:
		m.tasks = msg.Tasks
		if msg.Stats != nil {
			m.stats = TaskStatsFromAPI(msg.Stats)
			m.statusBar.Stats = m.stats
		}
		// Update task tree with new data
		m.taskTree.SetTasks(msg.Tasks)
		// Sync task detail with current selection
		m.syncTaskDetail()
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

	case TickMsg:
		// Collect resource metrics
		m.resourceMetrics = m.metricsCollector.Collect()
		// Schedule next tick
		return m, tickCmd()
	}

	return m, nil
}

// handleKeyMsg processes keyboard input.
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If in filter mode, handle filter input first
	if m.filterMode {
		return m.handleFilterInput(msg)
	}

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
		case "/":
			// Activate filter mode
			if m.activePanel == PanelTasks {
				m.filterMode = true
				m.filterQuery = ""
			}
			return m, nil
		case "c":
			// Clear filter if active
			if m.filterActive && !m.filterMode {
				m.clearFilter()
			}
			return m, nil
		case "q":
			m.sseClient.Stop()
			return m, tea.Quit
		case "r":
			// Refresh: reconnect SSE to get fresh snapshot
			m.sseClient.Stop()
			m.sseClient = NewSSEClient(m.config.APIURL, m.config.Project)
			return m, m.sseClient.Connect(m.ctx)
		case "L":
			m.logsVisible = !m.logsVisible
			// If hiding the active panel, switch back to tasks
			if !m.logsVisible && m.activePanel == PanelLogs {
				m.activePanel = PanelTasks
			}
			m.recalcPanelSizes()
			return m, nil
		case "T":
			m.detailVisible = !m.detailVisible
			// If hiding the active panel, switch back to tasks
			if !m.detailVisible && m.activePanel == PanelDetails {
				m.activePanel = PanelTasks
			}
			m.recalcPanelSizes()
			return m, nil
		case "j":
			if m.activePanel == PanelTasks {
				m.taskTree.MoveDown()
				m.syncTaskDetail()
			}
			return m, nil
		case "k":
			if m.activePanel == PanelTasks {
				m.taskTree.MoveUp()
				m.syncTaskDetail()
			}
			return m, nil
		case "g":
			if m.activePanel == PanelTasks {
				m.taskTree.MoveToTop()
				m.syncTaskDetail()
			}
			return m, nil
		case "G":
			if m.activePanel == PanelTasks {
				m.taskTree.MoveToBottom()
				m.syncTaskDetail()
			}
			return m, nil
		case " ":
			// Space toggles group collapse when on group header, selection when on task
			if m.activePanel == PanelTasks {
				if m.taskTree.IsOnGroupHeader() {
					// On group header: toggle collapse
					m.taskTree.ToggleCollapse()
				} else {
					// On task: toggle selection
					m.toggleTaskSelection()
				}
			}
			return m, nil
		case "A":
			// Select all visible tasks
			if m.activePanel == PanelTasks {
				m.selectAllTasks()
			}
			return m, nil
		case "D":
			// Clear all selections
			if m.activePanel == PanelTasks {
				m.clearSelection()
			}
			return m, nil
		}

	case tea.KeyUp:
		if m.activePanel == PanelTasks {
			m.taskTree.MoveUp()
			m.syncTaskDetail()
		}
		return m, nil

	case tea.KeyDown:
		if m.activePanel == PanelTasks {
			m.taskTree.MoveDown()
			m.syncTaskDetail()
		}
		return m, nil
	}

	return m, nil
}

// syncTaskDetail updates the task detail panel with the currently selected task.
func (m *Model) syncTaskDetail() {
	m.taskDetail.SetTask(m.taskTree.SelectedTask())
}

// toggleTaskSelection toggles selection for the currently focused task.
func (m *Model) toggleTaskSelection() {
	task := m.taskTree.SelectedTask()
	if task == nil {
		return
	}

	if m.selectedTasks[task.ID] {
		delete(m.selectedTasks, task.ID)
	} else {
		m.selectedTasks[task.ID] = true
	}
}

// clearSelection clears all selected tasks.
func (m *Model) clearSelection() {
	m.selectedTasks = make(map[string]bool)
}

// selectAllTasks selects all visible tasks.
func (m *Model) selectAllTasks() {
	for _, task := range m.filteredTasks() {
		m.selectedTasks[task.ID] = true
	}
}

// getSelectedTasks returns all selected tasks.
func (m *Model) getSelectedTasks() []types.ResolvedTask {
	selected := []types.ResolvedTask{}
	for _, task := range m.tasks {
		if m.selectedTasks[task.ID] {
			selected = append(selected, task)
		}
	}
	return selected
}

// recalcPanelSizes recalculates panel dimensions based on current window size.
func (m *Model) recalcPanelSizes() {
	if m.width == 0 || m.height == 0 {
		return
	}

	// Main content height: total - statusbar (3 lines) - helpbar (1 line)
	mainHeight := m.height - 4
	if mainHeight < 3 {
		mainHeight = 3
	}

	// If right panels are visible, split width 60/40
	hasRightPanel := m.detailVisible || m.logsVisible
	if !hasRightPanel {
		return
	}

	rightWidth := m.width * 40 / 100
	if rightWidth < 20 {
		rightWidth = 20
	}

	// Split right panel height between detail and logs
	if m.detailVisible && m.logsVisible {
		halfHeight := mainHeight / 2
		m.taskDetail.SetSize(rightWidth, halfHeight)
		m.logViewer.SetSize(rightWidth, mainHeight-halfHeight)
	} else if m.detailVisible {
		m.taskDetail.SetSize(rightWidth, mainHeight)
	} else if m.logsVisible {
		m.logViewer.SetSize(rightWidth, mainHeight)
	}
}

// View implements tea.Model. Renders the TUI layout.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing..."
	}

	// Update status bar with selection count and metrics
	m.statusBar.SelectedCount = len(m.selectedTasks)
	m.statusBar.Metrics = &m.resourceMetrics
	// Render status bar at top
	statusBarView := m.statusBar.View(m.width)

	// Calculate available height for main content
	// StatusBar: ~3 lines, HelpBar: 1 line
	mainHeight := m.height - 4
	if mainHeight < 3 {
		mainHeight = 3
	}

	// Determine if right panels are visible
	hasRightPanel := m.detailVisible || m.logsVisible

	// Calculate widths
	var leftWidth, rightWidth int
	if hasRightPanel {
		rightWidth = m.width * 40 / 100
		if rightWidth < 20 {
			rightWidth = 20
		}
		leftWidth = m.width - rightWidth
	} else {
		leftWidth = m.width
	}

	// Left panel: task tree
	taskPanelStyle := InactiveBorder
	if m.activePanel == PanelTasks {
		taskPanelStyle = ActiveBorder
	}

	innerWidth := leftWidth - 4 // account for border + padding
	innerHeight := mainHeight - 2
	if innerWidth < 10 {
		innerWidth = 10
	}
	if innerHeight < 1 {
		innerHeight = 1
	}

	taskContent := m.taskTree.View(innerWidth, innerHeight)
	taskPanel := taskPanelStyle.
		Width(leftWidth - 2).
		Height(mainHeight).
		Render(taskContent)

	// Build main content
	var mainContent string
	if hasRightPanel {
		rightPanel := m.renderRightPanel(rightWidth, mainHeight)
		mainContent = lipgloss.JoinHorizontal(lipgloss.Top, taskPanel, rightPanel)
	} else {
		mainContent = taskPanel
	}

	// Help bar at bottom
	helpBarView := m.helpBar.View(m.width, m.config.IsMultiProject())

	// Filter bar (if active or in input mode)
	var filterBarView string
	if m.filterMode {
		// Show filter input bar
		filterBarView = FilterBarStyle.Render("Filter: " + m.filterQuery + "_")
	} else if m.filterActive {
		// Show filter status
		totalCount := len(m.tasks)
		matchCount := len(m.filteredTasks())
		filterBarView = FilterStatusStyle.Render(fmt.Sprintf("Filtered: %d/%d tasks (press 'c' to clear)", matchCount, totalCount))
	}

	// Compose layout vertically
	if filterBarView != "" {
		return lipgloss.JoinVertical(lipgloss.Left,
			statusBarView,
			mainContent,
			helpBarView,
			filterBarView,
		)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		statusBarView,
		mainContent,
		helpBarView,
	)
}

// renderRightPanel renders the right side panel(s) - detail and/or logs.
func (m Model) renderRightPanel(width, height int) string {
	if m.detailVisible && m.logsVisible {
		// Split vertically: detail on top, logs on bottom
		halfHeight := height / 2
		detailPanel := m.renderDetailPanel(width, halfHeight)
		logPanel := m.renderLogPanel(width, height-halfHeight)
		return lipgloss.JoinVertical(lipgloss.Left, detailPanel, logPanel)
	}

	if m.detailVisible {
		return m.renderDetailPanel(width, height)
	}

	return m.renderLogPanel(width, height)
}

// renderDetailPanel renders the task detail panel with border.
func (m Model) renderDetailPanel(width, height int) string {
	style := InactiveBorder
	if m.activePanel == PanelDetails {
		style = ActiveBorder
	}

	innerWidth := width - 4
	innerHeight := height - 2
	if innerWidth < 10 {
		innerWidth = 10
	}
	if innerHeight < 1 {
		innerHeight = 1
	}

	// Temporarily set size for rendering
	detail := m.taskDetail
	detail.SetSize(innerWidth, innerHeight)
	content := detail.View()

	return style.
		Width(width - 2).
		Height(height).
		Render(content)
}

// renderLogPanel renders the log viewer panel with border.
func (m Model) renderLogPanel(width, height int) string {
	style := InactiveBorder
	if m.activePanel == PanelLogs {
		style = ActiveBorder
	}

	innerWidth := width - 4
	innerHeight := height - 2
	if innerWidth < 10 {
		innerWidth = 10
	}
	if innerHeight < 1 {
		innerHeight = 1
	}

	// Temporarily set size for rendering
	lv := m.logViewer
	lv.SetSize(innerWidth, innerHeight)
	content := lv.View()

	return style.
		Width(width - 2).
		Height(height).
		Render(content)
}

// =============================================================================
// Filter Methods
// =============================================================================

// handleFilterInput processes keyboard input in filter mode.
func (m Model) handleFilterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		// Confirm filter and exit filter mode
		m.filterMode = false
		m.filterActive = (m.filterQuery != "")
		// Apply filter to task tree
		m.applyFilter()
		return m, nil

	case tea.KeyEsc:
		// Cancel filter
		m.filterMode = false
		m.filterQuery = ""
		// If no filter was active, no need to update
		if !m.filterActive {
			return m, nil
		}
		// Clear the active filter
		m.clearFilter()
		return m, nil

	case tea.KeyBackspace, tea.KeyDelete:
		// Delete last character
		if len(m.filterQuery) > 0 {
			m.filterQuery = m.filterQuery[:len(m.filterQuery)-1]
		}
		// Real-time filtering
		m.applyFilter()
		return m, nil

	case tea.KeyCtrlU:
		// Clear entire filter input
		m.filterQuery = ""
		m.applyFilter()
		return m, nil

	case tea.KeyRunes:
		// Append character to filter
		m.filterQuery += string(msg.Runes)
		// Real-time filtering
		m.applyFilter()
		return m, nil
	}

	return m, nil
}

// applyFilter applies the current filter query to the task list.
func (m *Model) applyFilter() {
	if m.filterQuery == "" {
		// No filter, show all tasks
		m.taskTree.SetTasks(m.tasks)
		m.filterActive = false
	} else {
		// Filter tasks
		filtered := FilterTasks(m.tasks, m.filterQuery)
		m.taskTree.SetTasks(filtered)
		m.filterActive = true
	}
	// Sync task detail after filter changes
	m.syncTaskDetail()
}

// clearFilter removes the active filter and restores all tasks.
func (m *Model) clearFilter() {
	m.filterQuery = ""
	m.filterActive = false
	m.taskTree.SetTasks(m.tasks)
	m.syncTaskDetail()
}

// filteredTasks returns the current list of tasks (filtered or not).
func (m *Model) filteredTasks() []types.ResolvedTask {
	if m.filterQuery == "" {
		return m.tasks
	}
	return FilterTasks(m.tasks, m.filterQuery)
}

package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/huynle/brain-api/internal/runner"
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

	// Modal management
	modalManager ModalManager
	settings     Settings

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

	// Multi-project state
	projectTabs     ProjectTabs
	activeProjectID string
	tasksByProject  map[string][]types.ResolvedTask
	sseClients      map[string]*SSEClient

	// Multi-select state
	selectedTasks map[string]bool

	// Pause/resume state
	pausedProjects map[string]bool
	allPaused      bool

	// Resource metrics
	metricsCollector *MetricsCollector
	resourceMetrics  ResourceMetrics

	// Status message for user feedback
	statusMessage     string
	statusMessageType string // "success", "error", "info"
	statusMessageTime time.Time
}

// NewModel creates a new TUI model with the given configuration.
func NewModel(cfg Config) Model {
	// Load settings from disk
	settings, err := LoadSettings()
	if err != nil {
		// Fallback to defaults on error (file might not exist yet)
		settings = Settings{
			GroupCollapsed:    make(map[string]bool),
			FeatureCollapsed:  make(map[string]bool),
			ProjectLimits:     make(map[string]int),
			GlobalMaxParallel: 4,
		}
	}

	m := Model{
		config:           cfg,
		keymap:           DefaultKeyMap(),
		statusBar:        NewStatusBar(cfg.Project),
		helpBar:          NewHelpBar(),
		taskTree:         NewTaskTree(),
		taskDetail:       NewTaskDetail(),
		logViewer:        NewLogViewer(DefaultMaxLogEntries),
		modalManager:     NewModalManager(),
		settings:         settings,
		activePanel:      PanelTasks,
		sseClient:        NewSSEClient(cfg.APIURL, cfg.Project),
		ctx:              context.Background(),
		selectedTasks:    make(map[string]bool),
		pausedProjects:   make(map[string]bool),
		tasksByProject:   make(map[string][]types.ResolvedTask),
		sseClients:       make(map[string]*SSEClient),
		metricsCollector: NewMetricsCollector(),
	}

	// Wire TextWrap setting to sub-models
	m.taskTree.TextWrap = settings.TextWrap
	m.helpBar.TextWrap = settings.TextWrap

	// Create SSE clients for multi-project mode
	// Initialize ProjectTabs for multi-project mode
	if cfg.IsMultiProject() {
		m.projectTabs = NewProjectTabs(cfg.Projects)
	}

	// Initialize activeProjectID for multi-project mode
	if cfg.IsMultiProject() {
		m.activeProjectID = "all"
	}
	if cfg.IsMultiProject() {
		for _, projectID := range cfg.Projects {
			m.sseClients[projectID] = NewSSEClient(cfg.APIURL, projectID)
		}
	}

	return m
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
	if m.config.IsMultiProject() {
		// Multi-project mode: connect each per-project SSE client
		cmds := []tea.Cmd{
			tea.EnableMouseAllMotion,
			tickCmd(),
		}
		for _, client := range m.sseClients {
			cmds = append(cmds, client.Connect(m.ctx))
		}
		return tea.Batch(cmds...)
	}

	// Single-project mode: connect legacy single client
	return tea.Batch(
		tea.EnableMouseAllMotion,
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

	case tea.MouseMsg:
		return m.handleMouseMsg(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalcPanelSizes()
		return m, nil

	case TasksUpdatedMsg:
		// Store tasks by project if ProjectID is set (multi-project mode)
		if msg.ProjectID != "" {
			m.tasksByProject[msg.ProjectID] = msg.Tasks
		} else {
			// Single-project mode: update legacy tasks field directly
			m.tasks = msg.Tasks
		}

		// Update stats if provided
		if msg.Stats != nil {
			tuiStats := TaskStatsFromAPI(msg.Stats)

			// Gap 2: In multi-project mode, update ProjectTabs stats
			if msg.ProjectID != "" && m.config.IsMultiProject() {
				m.projectTabs.UpdateStats(msg.ProjectID, tuiStats)
				// Set m.stats from ProjectTabs (respects active tab)
				m.stats = m.projectTabs.CurrentStats()
				m.statusBar.Stats = m.stats
			} else {
				// Single-project mode: set stats directly
				m.stats = tuiStats
				m.statusBar.Stats = m.stats
			}
		}

		// Sync active project view (handles aggregate vs project-specific view)
		// In single-project mode, this is a no-op
		m.syncActiveProjectView()

		// Update taskTree
		m.taskTree.SetTasks(m.tasks)

		// Sync task detail with current selection
		m.syncTaskDetail()

		// Continue listening for next SSE message
		// In multi-project mode, use the project-specific client
		var nextCmd tea.Cmd
		if msg.ProjectID != "" && m.sseClients[msg.ProjectID] != nil {
			nextCmd = m.sseClients[msg.ProjectID].WaitForNextMsg()
		} else {
			nextCmd = m.sseClient.WaitForNextMsg()
		}
		return m, nextCmd

	case SSEConnectedMsg:
		m.connected = true
		m.statusBar.Connected = true
		// Continue listening for next SSE message
		// Gap 4c: Route to per-project client if ProjectID is set
		if msg.ProjectID != "" && m.sseClients[msg.ProjectID] != nil {
			return m, m.sseClients[msg.ProjectID].WaitForNextMsg()
		}
		return m, m.sseClient.WaitForNextMsg()

	case SSEDisconnectedMsg:
		m.connected = false
		m.statusBar.Connected = false
		// Gap 4c: Route reconnect to per-project client if ProjectID is set
		if msg.ProjectID != "" && m.sseClients[msg.ProjectID] != nil {
			return m, m.sseClients[msg.ProjectID].Reconnect(DefaultReconnectDelay)
		}
		// Schedule reconnect for legacy single client
		return m, m.sseClient.Reconnect(DefaultReconnectDelay)

	case SSEErrorMsg:
		// Log error, stay connected, continue listening
		// Gap 4c: Route to per-project client if ProjectID is set
		if msg.ProjectID != "" && m.sseClients[msg.ProjectID] != nil {
			return m, m.sseClients[msg.ProjectID].WaitForNextMsg()
		}
		return m, m.sseClient.WaitForNextMsg()

	case reconnectMsg:
		// Stop old client and create a new one for reconnection
		m.sseClient.Stop()
		m.sseClient = NewSSEClient(m.config.APIURL, m.config.Project)
		return m, m.sseClient.Connect(m.ctx)

	case reconnectProjectMsg:
		// Stop old per-project client and create a new one for reconnection
		if client, ok := m.sseClients[msg.ProjectID]; ok {
			client.Stop()
		}
		m.sseClients[msg.ProjectID] = NewSSEClient(m.config.APIURL, msg.ProjectID)
		return m, m.sseClients[msg.ProjectID].Connect(m.ctx)

	case TickMsg:
		// Collect resource metrics
		m.resourceMetrics = m.metricsCollector.Collect()
		// Schedule next tick and sync runner pause state
		return m, tea.Batch(tickCmd(), fetchRunnerStatusCmd(m.config.APIURL))

	case taskCompletedMsg:
		if msg.err != nil {
			m.setStatusMessage("error", fmt.Sprintf("Failed to complete task: %v", msg.err))
		} else {
			m.setStatusMessage("success", "Task completed successfully")
		}
		return m, nil

	case taskCancelledMsg:
		if msg.err != nil {
			m.setStatusMessage("error", fmt.Sprintf("Failed to cancel task: %v", msg.err))
		} else {
			m.setStatusMessage("success", "Task cancelled successfully")
		}
		// Close modal after cancel completes
		m.modalManager.Close()
		return m, nil

	case batchTasksCompletedMsg:
		if len(msg.errors) > 0 {
			m.setStatusMessage("error", fmt.Sprintf("Completed %d tasks, %d failed", msg.successCount, msg.failedCount))
		} else {
			m.setStatusMessage("success", fmt.Sprintf("Completed %d tasks successfully", msg.successCount))
			// Clear selection on success
			m.clearSelection()
		}
		return m, nil

	case batchTasksCancelledMsg:
		if len(msg.errors) > 0 {
			m.setStatusMessage("error", fmt.Sprintf("Cancelled %d tasks, %d failed", msg.successCount, msg.failedCount))
		} else {
			m.setStatusMessage("success", fmt.Sprintf("Cancelled %d tasks successfully", msg.successCount))
			// Clear selection on success
			m.clearSelection()
		}
		// Close modal after batch cancel completes
		m.modalManager.Close()
		return m, nil

	case taskExecutedMsg:
		if msg.err != nil {
			if msg.claimedBy != "" {
				m.setStatusMessage("error", fmt.Sprintf("✗ Already claimed by %s", msg.claimedBy))
			} else {
				m.setStatusMessage("error", fmt.Sprintf("✗ Execute failed: %v", msg.err))
			}
		} else {
			m.setStatusMessage("success", "✓ Task claimed for execution")
		}
		m.modalManager.Close()
		return m, nil

	case taskDeletedMsg:
		if msg.err != nil {
			m.setStatusMessage("error", fmt.Sprintf("✗ Delete failed: %v", msg.err))
		} else {
			m.setStatusMessage("success", "✓ Task deleted")
		}
		m.modalManager.Close()
		return m, nil

	case batchTasksDeletedMsg:
		if msg.failedCount > 0 {
			m.setStatusMessage("info", fmt.Sprintf("✓ %d deleted, ✗ %d failed", msg.successCount, msg.failedCount))
		} else {
			m.setStatusMessage("success", fmt.Sprintf("✓ %d tasks deleted", msg.successCount))
			// Clear selection on success
			m.clearSelection()
		}
		m.modalManager.Close()
		return m, nil

	case editorClosedMsg:
		if msg.err != nil {
			m.setStatusMessage("error", fmt.Sprintf("✗ Editor error: %v", msg.err))
		} else {
			m.setStatusMessage("success", "✓ File saved - refreshing...")
		}
		return m, nil

	case pauseToggledMsg:
		if msg.err != nil {
			// Revert optimistic update
			m.pausedProjects[msg.projectID] = !msg.paused
			m.setStatusMessage("error", fmt.Sprintf("Failed to toggle pause: %v", msg.err))
		} else {
			if msg.paused {
				m.setStatusMessage("success", fmt.Sprintf("Project %s paused", msg.projectID))
			} else {
				m.setStatusMessage("success", fmt.Sprintf("Project %s resumed", msg.projectID))
			}
		}
		m.syncHelpBarPauseState()
		return m, nil

	case pauseAllToggledMsg:
		if msg.err != nil {
			m.allPaused = !msg.paused
			m.setStatusMessage("error", fmt.Sprintf("Failed to toggle pause all: %v", msg.err))
		} else {
			if msg.paused {
				m.setStatusMessage("success", "All projects paused")
			} else {
				m.setStatusMessage("success", "All projects resumed")
			}
		}
		m.syncHelpBarPauseState()
		return m, nil

	case runnerStatusMsg:
		if msg.err == nil {
			m.allPaused = msg.paused
			m.pausedProjects = make(map[string]bool)
			for _, id := range msg.pausedProjects {
				m.pausedProjects[id] = true
			}
		}
		m.syncHelpBarPauseState()
		return m, nil
	}

	return m, nil
}

// handleKeyMsg processes keyboard input.
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If modal is open, route keys to modal first
	if m.modalManager.IsOpen() {
		handled, cmd := m.modalManager.HandleKey(string(msg.Runes))
		if msg.Type == tea.KeyEsc {
			// Esc always closes modal
			handled, cmd = m.modalManager.HandleKey("esc")
		}
		if handled {
			return m, cmd
		}
	}

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
		// Multi-project tab navigation
		if m.config.IsMultiProject() {
			switch string(msg.Runes) {
			case "h", "[":
				m.projectTabs.PrevTab()
				m.activeProjectID = m.projectTabs.ActiveProject()
				m.syncActiveProjectView()
				return m, nil
			case "l", "]":
				m.projectTabs.NextTab()
				m.activeProjectID = m.projectTabs.ActiveProject()
				m.syncActiveProjectView()
				return m, nil
			case "1", "2", "3", "4", "5", "6", "7", "8", "9":
				tabNum := int(msg.Runes[0] - '0')
				if m.projectTabs.JumpToTab(tabNum) {
					m.activeProjectID = m.projectTabs.ActiveProject()
					m.syncActiveProjectView()
					return m, nil
				}
			}
		}

		switch string(msg.Runes) {
		case "?":
			// Open help modal
			modal := NewHelpModal(m.config.IsMultiProject())
			cmd := m.modalManager.Open(modal)
			return m, cmd
		case "S":
			// Open settings modal
			modal := NewSettingsModal(m.settings)
			cmd := m.modalManager.Open(modal)
			return m, cmd
		case "s":
			// Open metadata modal for selected task(s)
			if m.activePanel == PanelTasks {
				// Create API client for modal
				apiClient := runner.NewAPIClient(runner.RunnerConfig{
					BrainAPIURL: m.config.APIURL,
					APITimeout:  5000, // 5 second timeout
				})

				var modal Modal

				// Case 0: Feature header selected (in feature view mode)
				if m.taskTree.useFeatureView {
					featureID := m.taskTree.GetSelectedFeatureID()
					if featureID != "" {
						modal = NewMetadataModalFeature(featureID, m.config.Project, apiClient)
						if modal != nil {
							cmd := m.modalManager.Open(modal)
							return m, cmd
						}
					}
				}

				// Case 1: Multi-select active - batch mode
				if len(m.selectedTasks) > 0 {
					taskIDs := make([]string, 0, len(m.selectedTasks))
					for id := range m.selectedTasks {
						taskIDs = append(taskIDs, id)
					}
					modal = NewMetadataModalBatch(taskIDs, apiClient)
				} else {
					// Case 2: Single task selected
					selectedTask := m.taskTree.SelectedTask()
					if selectedTask != nil {
						modal = NewMetadataModal(selectedTask.ID, apiClient)
					}
				}

				if modal != nil {
					cmd := m.modalManager.Open(modal)
					return m, cmd
				}
			}
			return m, nil
		case "c":
			// Complete task(s) - no confirmation required
			if m.activePanel == PanelTasks {
				// Case 1: Multi-select active - batch complete
				if len(m.selectedTasks) > 0 {
					taskPaths := []string{}
					taskIDs := []string{}
					for id := range m.selectedTasks {
						// Find task to get its path
						for _, t := range m.tasks {
							if t.ID == id {
								taskPaths = append(taskPaths, t.Path)
								taskIDs = append(taskIDs, id)
								break
							}
						}
					}
					return m, batchCompleteTasksCmd(m.config.APIURL, taskPaths, taskIDs)
				}

				// Case 2: Single task selected
				selectedTask := m.taskTree.SelectedTask()
				if selectedTask != nil {
					return m, completeTaskCmd(m.config.APIURL, selectedTask.Path)
				}
			}
			return m, nil
		case "C":
			// Cancel task(s) - with confirmation modal
			if m.activePanel == PanelTasks {
				// Gather task information for cancel
				var taskPaths []string
				var taskIDs []string
				var confirmMsg string

				// Case 1: Multi-select active - batch cancel
				if len(m.selectedTasks) > 0 {
					for id := range m.selectedTasks {
						// Find task to get its path
						for _, t := range m.tasks {
							if t.ID == id {
								taskPaths = append(taskPaths, t.Path)
								taskIDs = append(taskIDs, id)
								break
							}
						}
					}
					confirmMsg = fmt.Sprintf("Cancel %d selected tasks?", len(taskIDs))
				} else {
					// Case 2: Single task selected
					selectedTask := m.taskTree.SelectedTask()
					if selectedTask != nil {
						taskPaths = []string{selectedTask.Path}
						taskIDs = []string{selectedTask.ID}
						confirmMsg = "Cancel this task?"
					}
				}

				if len(taskPaths) > 0 {
					// Create modal with callback
					modal := NewConfirmModal("Confirm Cancel", confirmMsg).
						WithOnConfirm(func() tea.Msg {
							if len(taskPaths) == 1 {
								return cancelTaskCmd(m.config.APIURL, taskPaths[0])()
							}
							return batchCancelTasksCmd(m.config.APIURL, taskPaths, taskIDs)()
						})
					cmd := m.modalManager.Open(modal)
					return m, cmd
				}
			}
			return m, nil
		case "x":
			// Execute task - claim for immediate execution
			if m.activePanel != PanelTasks {
				return m, nil
			}

			selectedTask := m.taskTree.SelectedTask()
			if selectedTask == nil {
				return m, nil
			}

			// Get runner ID from config or generate
			runnerID := "manual-tui"
			if m.config.RunnerID != "" {
				runnerID = m.config.RunnerID
			}

			apiClient := runner.NewAPIClient(runner.RunnerConfig{
				BrainAPIURL: m.config.APIURL,
				APITimeout:  5000,
			})

			message := fmt.Sprintf("Execute task '%s' now?\nThis will claim it for immediate execution.", selectedTask.Title)
			modal := NewConfirmModal("Execute Task", message).
				WithOnConfirm(func() tea.Msg {
					return executeTaskCmd(apiClient, m.config.Project, selectedTask.ID, runnerID)()
				})
			return m, m.modalManager.Open(modal)
		case "d":
			// Delete task(s) - with confirmation modal
			if m.activePanel != PanelTasks {
				return m, nil
			}

			apiClient := runner.NewAPIClient(runner.RunnerConfig{
				BrainAPIURL: m.config.APIURL,
				APITimeout:  5000,
			})

			// Case 1: Multi-select mode - batch delete
			if len(m.selectedTasks) > 0 {
				count := len(m.selectedTasks)
				taskIDs := make([]string, 0, count)
				taskPaths := make([]string, 0, count)
				for id := range m.selectedTasks {
					taskIDs = append(taskIDs, id)
					// Find task to get path
					for _, t := range m.tasks {
						if t.ID == id {
							taskPaths = append(taskPaths, t.Path)
							break
						}
					}
				}

				message := fmt.Sprintf("Delete %d tasks? This cannot be undone.", count)
				modal := NewConfirmModal("Delete Tasks", message).
					WithOnConfirm(func() tea.Msg {
						return batchDeleteTasksCmd(apiClient, taskPaths, taskIDs)()
					})
				return m, m.modalManager.Open(modal)
			}

			// Case 2: Single task mode
			selectedTask := m.taskTree.SelectedTask()
			if selectedTask == nil {
				return m, nil
			}

			message := fmt.Sprintf("Delete task '%s'?\nThis cannot be undone.", selectedTask.Title)
			modal := NewConfirmModal("Delete Task", message).
				WithOnConfirm(func() tea.Msg {
					return deleteTaskCmd(apiClient, selectedTask.Path)()
				})
			return m, m.modalManager.Open(modal)
		case "e":
			// Edit task in $EDITOR
			if m.activePanel != PanelTasks {
				return m, nil
			}

			selectedTask := m.taskTree.SelectedTask()
			if selectedTask == nil {
				return m, nil
			}

			// Construct full file path
			// Task path format: projects/{project}/task/{id}.md
			fullPath := filepath.Join(m.config.BrainDir, selectedTask.Path)

			return m, tea.ExecProcess(getEditorCmd(fullPath), func(err error) tea.Msg {
				return editorClosedMsg{taskID: selectedTask.ID, err: err}
			})
		case "/":
			// Activate filter mode
			if m.activePanel == PanelTasks {
				m.filterMode = true
				m.filterQuery = ""
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
		case "w":
			m.settings.TextWrap = !m.settings.TextWrap
			m.taskTree.TextWrap = m.settings.TextWrap
			m.helpBar.TextWrap = m.settings.TextWrap
			_ = SaveSettings(m.settings)
			return m, nil
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
			} else if m.activePanel == PanelDetails {
				m.taskDetail.ScrollDown()
			}
			return m, nil
		case "k":
			if m.activePanel == PanelTasks {
				m.taskTree.MoveUp()
				m.syncTaskDetail()
			} else if m.activePanel == PanelDetails {
				m.taskDetail.ScrollUp()
			}
			return m, nil
		case "g":
			if m.activePanel == PanelTasks {
				m.taskTree.MoveToTop()
				m.syncTaskDetail()
			} else if m.activePanel == PanelDetails {
				m.taskDetail.ScrollToTop()
			}
			return m, nil
		case "G":
			if m.activePanel == PanelTasks {
				m.taskTree.MoveToBottom()
				m.syncTaskDetail()
			} else if m.activePanel == PanelDetails {
				m.taskDetail.ScrollToBottom()
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
		case "p":
			// Pause/resume active project
			projectID := m.activeProjectID
			if projectID == "" || projectID == "all" {
				projectID = m.config.Project
			}
			if projectID != "" {
				currentlyPaused := m.pausedProjects[projectID]
				// Optimistic UI update
				m.pausedProjects[projectID] = !currentlyPaused
				if currentlyPaused {
					m.setStatusMessage("info", fmt.Sprintf("Resuming project %s...", projectID))
				} else {
					m.setStatusMessage("info", fmt.Sprintf("Pausing project %s...", projectID))
				}
				m.syncHelpBarPauseState()
				return m, pauseProjectCmd(m.config.APIURL, projectID, currentlyPaused)
			}
			return m, nil
		case "P":
			// Pause/resume all projects
			m.allPaused = !m.allPaused
			if m.allPaused {
				m.setStatusMessage("info", "Pausing all projects...")
			} else {
				m.setStatusMessage("info", "Resuming all projects...")
			}
			m.syncHelpBarPauseState()
			return m, pauseAllCmd(m.config.APIURL, !m.allPaused)
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

// handleMouseMsg processes mouse input.
func (m Model) handleMouseMsg(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// If modal is open, don't handle mouse events in the main UI
	if m.modalManager.IsOpen() {
		return m, nil
	}

	switch msg.Type {
	case tea.MouseLeft:
		return m.handleMouseClick(msg)
	case tea.MouseWheelUp:
		return m.handleMouseWheelUp(msg)
	case tea.MouseWheelDown:
		return m.handleMouseWheelDown(msg)
	case tea.MouseRight:
		return m.handleRightClick(msg)
	}

	return m, nil
}

// handleMouseClick handles left mouse button clicks.
func (m Model) handleMouseClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	x, y := msg.X, msg.Y

	// Detect which panel was clicked
	// Status bar is at top (lines 0-2)
	// Project tabs (if multi-project) is next line
	// Main content starts after that
	// Help bar is at bottom

	statusBarHeight := 3
	projectTabsHeight := 0
	if m.config.IsMultiProject() {
		projectTabsHeight = 1
	}
	mainContentStartY := statusBarHeight + projectTabsHeight

	// Click in main content area
	if y >= mainContentStartY && y < m.height-1 {
		// Determine if click is in task panel or right panels
		hasRightPanel := m.detailVisible || m.logsVisible
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

		if x < leftWidth {
			// Click in task panel
			m.activePanel = PanelTasks
			m.helpBar.ActivePanel = m.activePanel

			// Detect task/group clicked
			lineInPanel := y - mainContentStartY
			return m.handleTaskPanelClick(lineInPanel, x)
		} else {
			// Click in right panel
			mainHeight := m.height - 4
			if m.detailVisible && m.logsVisible {
				halfHeight := mainHeight / 2
				if y < mainContentStartY+halfHeight {
					// Click in detail panel
					m.activePanel = PanelDetails
				} else {
					// Click in logs panel
					m.activePanel = PanelLogs
				}
			} else if m.detailVisible {
				m.activePanel = PanelDetails
			} else if m.logsVisible {
				m.activePanel = PanelLogs
			}
			m.helpBar.ActivePanel = m.activePanel
		}
	}

	return m, nil
}

// handleTaskPanelClick handles clicks within the task panel.
func (m Model) handleTaskPanelClick(lineInPanel, x int) (tea.Model, tea.Cmd) {
	if m.taskTree.useGroupedView {
		return m.handleGroupedViewClick(lineInPanel, x)
	}
	// Legacy tree view - simple task selection by line
	if lineInPanel >= 0 && lineInPanel < len(m.taskTree.order) {
		m.taskTree.Cursor = lineInPanel
		m.taskTree.SelectedID = m.taskTree.order[lineInPanel]
		m.syncTaskDetail()
	}
	return m, nil
}

// handleGroupedViewClick handles clicks in grouped view mode.
func (m Model) handleGroupedViewClick(lineInPanel, x int) (tea.Model, tea.Cmd) {
	if m.taskTree.useFeatureView {
		return m.handleFeatureViewClick(lineInPanel, x)
	}

	// Classification-based grouping
	currentLine := 0
	showCheckboxes := len(m.selectedTasks) > 0

	for gIdx, group := range m.taskTree.groups {
		// Group header line
		if currentLine == lineInPanel {
			// Click on group header
			if x >= 0 && x <= 2 {
				// Click on collapse indicator (▸/▾)
				m.taskTree.selectedGroupIdx = gIdx
				m.taskTree.selectedTaskIdx = -1
				m.taskTree.SelectedID = ""
				m.taskTree.ToggleCollapse()
			} else {
				// Click anywhere else on header - select header
				m.taskTree.selectedGroupIdx = gIdx
				m.taskTree.selectedTaskIdx = -1
				m.taskTree.SelectedID = ""
				m.syncTaskDetail()
			}
			return m, nil
		}
		currentLine++

		// Task lines (if not collapsed)
		if !group.Collapsed {
			for tIdx, task := range group.Tasks {
				if currentLine == lineInPanel {
					// Click on task
					if showCheckboxes && x >= 2 && x <= 4 {
						// Click on checkbox
						m.taskTree.selectedGroupIdx = gIdx
						m.taskTree.selectedTaskIdx = tIdx
						m.taskTree.SelectedID = task.ID
						m.toggleTaskSelection()
					} else {
						// Click on task (select it)
						m.taskTree.selectedGroupIdx = gIdx
						m.taskTree.selectedTaskIdx = tIdx
						m.taskTree.SelectedID = task.ID
						m.syncTaskDetail()
					}
					return m, nil
				}
				currentLine++
			}
		}
	}

	return m, nil
}

// handleFeatureViewClick handles clicks in feature view mode.
func (m Model) handleFeatureViewClick(lineInPanel, x int) (tea.Model, tea.Cmd) {
	currentLine := 0
	showCheckboxes := len(m.selectedTasks) > 0

	// Check features
	for fIdx, feature := range m.taskTree.featureGroups.Features {
		// Feature header line
		if currentLine == lineInPanel {
			if x >= 0 && x <= 2 {
				// Click on collapse indicator
				m.taskTree.selectedFeatureIdx = fIdx
				m.taskTree.selectedFeatureTaskIdx = -1
				m.taskTree.isOnUngrouped = false
				m.taskTree.SelectedID = ""
				m.taskTree.ToggleCollapse()
			} else {
				// Select feature header
				m.taskTree.selectedFeatureIdx = fIdx
				m.taskTree.selectedFeatureTaskIdx = -1
				m.taskTree.isOnUngrouped = false
				m.taskTree.SelectedID = ""
				m.syncTaskDetail()
			}
			return m, nil
		}
		currentLine++

		// Task lines (if not collapsed)
		if !feature.Collapsed {
			for tIdx, task := range feature.Tasks {
				if currentLine == lineInPanel {
					if showCheckboxes && x >= 2 && x <= 4 {
						// Click on checkbox
						m.taskTree.selectedFeatureIdx = fIdx
						m.taskTree.selectedFeatureTaskIdx = tIdx
						m.taskTree.isOnUngrouped = false
						m.taskTree.SelectedID = task.ID
						m.toggleTaskSelection()
					} else {
						// Select task
						m.taskTree.selectedFeatureIdx = fIdx
						m.taskTree.selectedFeatureTaskIdx = tIdx
						m.taskTree.isOnUngrouped = false
						m.taskTree.SelectedID = task.ID
						m.syncTaskDetail()
					}
					return m, nil
				}
				currentLine++
			}
		}
	}

	// Check ungrouped
	if m.taskTree.featureGroups.Ungrouped != nil {
		ungrouped := m.taskTree.featureGroups.Ungrouped

		// Ungrouped header line
		if currentLine == lineInPanel {
			if x >= 0 && x <= 2 {
				// Click on collapse indicator
				m.taskTree.selectedFeatureIdx = -1
				m.taskTree.selectedFeatureTaskIdx = -1
				m.taskTree.isOnUngrouped = true
				m.taskTree.SelectedID = ""
				m.taskTree.ToggleCollapse()
			} else {
				// Select ungrouped header
				m.taskTree.selectedFeatureIdx = -1
				m.taskTree.selectedFeatureTaskIdx = -1
				m.taskTree.isOnUngrouped = true
				m.taskTree.SelectedID = ""
				m.syncTaskDetail()
			}
			return m, nil
		}
		currentLine++

		// Ungrouped task lines (if not collapsed)
		if !ungrouped.Collapsed {
			for tIdx, task := range ungrouped.Tasks {
				if currentLine == lineInPanel {
					if showCheckboxes && x >= 2 && x <= 4 {
						// Click on checkbox
						m.taskTree.selectedFeatureIdx = -1
						m.taskTree.selectedFeatureTaskIdx = tIdx
						m.taskTree.isOnUngrouped = true
						m.taskTree.SelectedID = task.ID
						m.toggleTaskSelection()
					} else {
						// Select task
						m.taskTree.selectedFeatureIdx = -1
						m.taskTree.selectedFeatureTaskIdx = tIdx
						m.taskTree.isOnUngrouped = true
						m.taskTree.SelectedID = task.ID
						m.syncTaskDetail()
					}
					return m, nil
				}
				currentLine++
			}
		}
	}

	return m, nil
}

// handleMouseWheelUp handles scroll wheel up (scroll up / move selection up).
func (m Model) handleMouseWheelUp(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.activePanel == PanelTasks {
		m.taskTree.MoveUp()
		m.syncTaskDetail()
	} else if m.activePanel == PanelDetails {
		m.taskDetail.ScrollUp()
	}
	return m, nil
}

// handleMouseWheelDown handles scroll wheel down (scroll down / move selection down).
func (m Model) handleMouseWheelDown(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.activePanel == PanelTasks {
		m.taskTree.MoveDown()
		m.syncTaskDetail()
	} else if m.activePanel == PanelDetails {
		m.taskDetail.ScrollDown()
	}
	return m, nil
}

// handleRightClick handles right mouse button clicks (context menu).
func (m Model) handleRightClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// TODO: Implement context menu
	// For now, treat right-click same as left-click (select task)
	return m.handleMouseClick(msg)
}

// syncTaskDetail updates the task detail panel with the currently selected task.
func (m *Model) syncTaskDetail() {
	m.taskDetail.SetTask(m.taskTree.SelectedTask())
}

// syncHelpBarPauseState updates the help bar's pause indicators based on current state.
func (m *Model) syncHelpBarPauseState() {
	m.helpBar.AllPaused = m.allPaused
	// Determine active project ID for pause check
	projectID := m.activeProjectID
	if projectID == "" || projectID == "all" {
		projectID = m.config.Project
	}
	m.helpBar.IsPaused = m.pausedProjects[projectID]
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

	// Render base UI
	baseView := m.renderBaseView()

	// Overlay modal if open
	if m.modalManager.IsOpen() {
		modalOverlay := m.modalManager.View(m.width, m.height)
		// Combine base view with modal overlay (modal handles centering)
		return baseView + "\n" + modalOverlay
	}

	return baseView
}

// renderBaseView renders the main TUI layout (without modal)
func (m Model) renderBaseView() string {
	// Update status bar with selection count and metrics
	m.statusBar.SelectedCount = len(m.selectedTasks)
	m.statusBar.Metrics = &m.resourceMetrics
	// Render ProjectTabs if multi-project mode
	var projectTabsView string
	if m.config.IsMultiProject() {
		projectTabsView = m.projectTabs.View(m.width)
	}

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

	taskContent := m.taskTree.ViewWithSelection(innerWidth, innerHeight, m.selectedTasks, m.activeProjectID)
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

	// Status message (if active and not expired)
	var statusMessageView string
	if m.statusMessage != "" && time.Since(m.statusMessageTime) < 3*time.Second {
		style := lipgloss.NewStyle().Padding(0, 1)
		switch m.statusMessageType {
		case "success":
			style = style.Foreground(lipgloss.Color("10")) // green
		case "error":
			style = style.Foreground(lipgloss.Color("9")) // red
		case "info":
			style = style.Foreground(lipgloss.Color("12")) // blue
		}
		statusMessageView = style.Render(m.statusMessage)
	}

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
	var bottomPanels []string
	if statusMessageView != "" {
		bottomPanels = append(bottomPanels, statusMessageView)
	}
	bottomPanels = append(bottomPanels, helpBarView)
	if filterBarView != "" {
		bottomPanels = append(bottomPanels, filterBarView)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		statusBarView,
		projectTabsView,
		mainContent,
		lipgloss.JoinVertical(lipgloss.Left, bottomPanels...),
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
		// Multi-project tab navigation
		if m.config.IsMultiProject() {
			switch string(msg.Runes) {
			case "h", "[":
				m.projectTabs.PrevTab()
				m.activeProjectID = m.projectTabs.ActiveProject()
				m.syncActiveProjectView()
				return m, nil
			case "l", "]":
				m.projectTabs.NextTab()
				m.activeProjectID = m.projectTabs.ActiveProject()
				m.syncActiveProjectView()
				return m, nil
			case "1", "2", "3", "4", "5", "6", "7", "8", "9":
				tabNum := int(msg.Runes[0] - '0')
				if m.projectTabs.JumpToTab(tabNum) {
					m.activeProjectID = m.projectTabs.ActiveProject()
					m.syncActiveProjectView()
					return m, nil
				}
			}
		}

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

// syncActiveProjectView switches between aggregate view (all projects) and project-specific view.
// In aggregate view (activeProjectID="all"), merges tasks from all projects.
// In project-specific view, shows only that project's tasks.
func (m *Model) syncActiveProjectView() {
	// Single-project mode: no-op
	if !m.config.IsMultiProject() {
		return
	}

	// Note: activeProjectID may be set either:
	// 1. From projectTabs (via tab navigation)
	// 2. Manually (in tests or other code paths)
	// We don't override it here - it's set by the caller.

	// Update statusBar.Project to reflect current activeProjectID
	if m.activeProjectID != "" {
		m.statusBar.Project = m.activeProjectID
	}

	// Determine which tasks to show
	if m.activeProjectID == "all" {
		// Aggregate view: merge all tasks
		m.tasks = m.getAllTasks()
		// Gap 3: Set aggregate stats from ProjectTabs
		m.stats = m.projectTabs.AggregateStats
		m.statusBar.Stats = m.stats
	} else {
		// Project-specific view: show only that project's tasks
		if tasks, ok := m.tasksByProject[m.activeProjectID]; ok {
			m.tasks = tasks
		} else {
			m.tasks = []types.ResolvedTask{}
		}
		// Gap 3: Set project-specific stats from ProjectTabs
		if stats, ok := m.projectTabs.StatsByProject[m.activeProjectID]; ok {
			m.stats = stats
		} else {
			m.stats = TaskStats{}
		}
		m.statusBar.Stats = m.stats
	}

	// Update taskTree with the selected tasks
	if m.filterActive {
		// If filter is active, apply it
		m.applyFilter()
	} else {
		// No filter, just set tasks directly
		m.taskTree.SetTasks(m.tasks)
	}
}

// getAllTasks merges all tasks from all projects into a single slice.
// Returns an empty slice if tasksByProject is empty.
func (m *Model) getAllTasks() []types.ResolvedTask {
	if len(m.tasksByProject) == 0 {
		return []types.ResolvedTask{}
	}

	var allTasks []types.ResolvedTask
	for _, tasks := range m.tasksByProject {
		allTasks = append(allTasks, tasks...)
	}

	return allTasks
}

// setStatusMessage sets a status message to be displayed to the user.
func (m *Model) setStatusMessage(msgType, message string) {
	m.statusMessage = message
	m.statusMessageType = msgType
	m.statusMessageTime = time.Now()
}

// =============================================================================
// Message Types
// =============================================================================

type taskCompletedMsg struct {
	taskID string
	err    error
}

type taskCancelledMsg struct {
	taskID string
	err    error
}

type batchTasksCompletedMsg struct {
	successCount int
	failedCount  int
	errors       []error
}

type batchTasksCancelledMsg struct {
	successCount int
	failedCount  int
	errors       []error
}

type taskExecutedMsg struct {
	taskID    string
	err       error
	claimedBy string
}

type taskDeletedMsg struct {
	taskID string
	err    error
}

type batchTasksDeletedMsg struct {
	successCount int
	failedCount  int
	errors       []error
}

type editorClosedMsg struct {
	taskID string
	err    error
}

type pauseToggledMsg struct {
	projectID string
	paused    bool
	err       error
}

type pauseAllToggledMsg struct {
	paused bool
	err    error
}

type runnerStatusMsg struct {
	paused         bool
	pausedProjects []string
	err            error
}

// =============================================================================
// Command Functions
// =============================================================================

// completeTaskCmd completes a single task.
func completeTaskCmd(apiURL, taskPath string) tea.Cmd {
	return func() tea.Msg {
		apiClient := runner.NewAPIClient(runner.RunnerConfig{
			BrainAPIURL: apiURL,
			APITimeout:  5000,
		})

		ctx := context.Background()
		err := apiClient.UpdateTaskStatus(ctx, taskPath, "completed")
		return taskCompletedMsg{taskID: taskPath, err: err}
	}
}

// cancelTaskCmd cancels a single task.
func cancelTaskCmd(apiURL, taskPath string) tea.Cmd {
	return func() tea.Msg {
		apiClient := runner.NewAPIClient(runner.RunnerConfig{
			BrainAPIURL: apiURL,
			APITimeout:  5000,
		})

		ctx := context.Background()
		err := apiClient.UpdateTaskStatus(ctx, taskPath, "cancelled")
		return taskCancelledMsg{taskID: taskPath, err: err}
	}
}

// batchCompleteTasksCmd completes multiple tasks in parallel.
func batchCompleteTasksCmd(apiURL string, taskPaths, taskIDs []string) tea.Cmd {
	return func() tea.Msg {
		apiClient := runner.NewAPIClient(runner.RunnerConfig{
			BrainAPIURL: apiURL,
			APITimeout:  5000,
		})

		type result struct {
			taskID string
			err    error
		}

		results := make(chan result, len(taskPaths))
		ctx := context.Background()

		// Execute all completions in parallel
		for i, taskPath := range taskPaths {
			go func(path, id string) {
				err := apiClient.UpdateTaskStatus(ctx, path, "completed")
				results <- result{taskID: id, err: err}
			}(taskPath, taskIDs[i])
		}

		// Collect results
		var errors []error
		successCount := 0
		failedCount := 0

		for range taskPaths {
			res := <-results
			if res.err != nil {
				errors = append(errors, fmt.Errorf("%s: %w", res.taskID, res.err))
				failedCount++
			} else {
				successCount++
			}
		}

		return batchTasksCompletedMsg{
			successCount: successCount,
			failedCount:  failedCount,
			errors:       errors,
		}
	}
}

// batchCancelTasksCmd cancels multiple tasks in parallel.
func batchCancelTasksCmd(apiURL string, taskPaths, taskIDs []string) tea.Cmd {
	return func() tea.Msg {
		apiClient := runner.NewAPIClient(runner.RunnerConfig{
			BrainAPIURL: apiURL,
			APITimeout:  5000,
		})

		type result struct {
			taskID string
			err    error
		}

		results := make(chan result, len(taskPaths))
		ctx := context.Background()

		// Execute all cancellations in parallel
		for i, taskPath := range taskPaths {
			go func(path, id string) {
				err := apiClient.UpdateTaskStatus(ctx, path, "cancelled")
				results <- result{taskID: id, err: err}
			}(taskPath, taskIDs[i])
		}

		// Collect results
		var errors []error
		successCount := 0
		failedCount := 0

		for range taskPaths {
			res := <-results
			if res.err != nil {
				errors = append(errors, fmt.Errorf("%s: %w", res.taskID, res.err))
				failedCount++
			} else {
				successCount++
			}
		}

		return batchTasksCancelledMsg{
			successCount: successCount,
			failedCount:  failedCount,
			errors:       errors,
		}
	}
}

// executeTaskCmd claims a task for immediate execution.
func executeTaskCmd(client *runner.APIClient, project, taskID, runnerID string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		result, err := client.ClaimTask(ctx, project, taskID, runnerID)
		if err != nil {
			return taskExecutedMsg{taskID: taskID, err: err}
		}
		if !result.Success {
			return taskExecutedMsg{
				taskID:    taskID,
				err:       fmt.Errorf("already claimed by %s", result.ClaimedBy),
				claimedBy: result.ClaimedBy,
			}
		}
		return taskExecutedMsg{taskID: taskID, err: nil}
	}
}

// deleteTaskCmd deletes a single task.
func deleteTaskCmd(client *runner.APIClient, taskPath string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := client.DeleteEntry(ctx, taskPath)
		return taskDeletedMsg{taskID: taskPath, err: err}
	}
}

// batchDeleteTasksCmd deletes multiple tasks in parallel.
func batchDeleteTasksCmd(client *runner.APIClient, taskPaths, taskIDs []string) tea.Cmd {
	return func() tea.Msg {
		type result struct {
			taskID string
			err    error
		}

		results := make(chan result, len(taskPaths))
		ctx := context.Background()

		// Execute all deletions in parallel
		for i, taskPath := range taskPaths {
			go func(path, id string) {
				err := client.DeleteEntry(ctx, path)
				results <- result{taskID: id, err: err}
			}(taskPath, taskIDs[i])
		}

		// Collect results
		var errors []error
		successCount := 0
		failedCount := 0

		for range taskPaths {
			res := <-results
			if res.err != nil {
				errors = append(errors, fmt.Errorf("%s: %w", res.taskID, res.err))
				failedCount++
			} else {
				successCount++
			}
		}

		return batchTasksDeletedMsg{
			successCount: successCount,
			failedCount:  failedCount,
			errors:       errors,
		}
	}
}

// pauseProjectCmd toggles pause/resume for a specific project.
func pauseProjectCmd(apiURL, projectID string, currentlyPaused bool) tea.Cmd {
	return func() tea.Msg {
		apiClient := runner.NewAPIClient(runner.RunnerConfig{
			BrainAPIURL: apiURL,
			APITimeout:  5000,
		})

		ctx := context.Background()
		var err error
		if currentlyPaused {
			err = apiClient.ResumeProject(ctx, projectID)
		} else {
			err = apiClient.PauseProject(ctx, projectID)
		}
		return pauseToggledMsg{projectID: projectID, paused: !currentlyPaused, err: err}
	}
}

// pauseAllCmd toggles pause/resume for all projects.
func pauseAllCmd(apiURL string, currentlyPaused bool) tea.Cmd {
	return func() tea.Msg {
		apiClient := runner.NewAPIClient(runner.RunnerConfig{
			BrainAPIURL: apiURL,
			APITimeout:  5000,
		})

		ctx := context.Background()
		var err error
		if currentlyPaused {
			err = apiClient.ResumeAll(ctx)
		} else {
			err = apiClient.PauseAll(ctx)
		}
		return pauseAllToggledMsg{paused: !currentlyPaused, err: err}
	}
}

// fetchRunnerStatusCmd fetches the current runner status (pause state).
func fetchRunnerStatusCmd(apiURL string) tea.Cmd {
	return func() tea.Msg {
		apiClient := runner.NewAPIClient(runner.RunnerConfig{
			BrainAPIURL: apiURL,
			APITimeout:  5000,
		})

		ctx := context.Background()
		status, err := apiClient.GetRunnerStatus(ctx)
		if err != nil {
			return runnerStatusMsg{err: err}
		}
		return runnerStatusMsg{paused: status.Paused, pausedProjects: status.PausedProjects}
	}
}

// getEditorCmd returns an exec.Cmd configured to open a file in $EDITOR.
func getEditorCmd(filePath string) *exec.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim" // fallback
	}

	cmd := exec.Command(editor, filePath)
	return cmd
}

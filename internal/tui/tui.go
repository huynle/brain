package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/types"
	"github.com/muesli/termenv"
)

// truncateToHeight truncates content to fit within the specified number of lines.
// If content has fewer lines, it's returned as-is (padding will be added by lipgloss.Height).
// If content has more lines, it's truncated to fit.
func truncateToHeight(content string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}

	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return content
	}

	// Truncate to maxLines
	return strings.Join(lines[:maxLines], "\n")
}

// DefaultReconnectDelay is the default delay before reconnecting after disconnect.
const DefaultReconnectDelay = 3 * time.Second

// DefaultMaxLogEntries is the default maximum number of log entries to keep.
const DefaultMaxLogEntries = 500

const (
	minTaskPanelHeight   = 8
	minBottomPanelHeight = 6
	minSubPanelHeight    = 4
)

// Model is the root Bubble Tea model for the TUI dashboard.
type Model struct {
	config Config
	keymap KeyMap

	// Sub-models
	statusBar  StatusBar
	helpBar    HelpBar
	taskTree   *TaskTree
	taskDetail TaskDetail
	logViewer  LogViewer

	// Schedule view
	viewMode       ViewMode
	scheduleList   ScheduleList
	scheduleDetail ScheduleDetail

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

	// Filter state (3-mode state machine: Off → Typing → Locked)
	filterState FilterMode // Current filter mode
	filterQuery string     // Current filter text

	// Task data
	tasks []types.ResolvedTask
	stats TaskStats

	// Multi-project state
	projectTabs     ProjectTabs
	activeProjectID string
	tasksByProject  map[string][]types.ResolvedTask
	sseClients      map[string]*SSEClient

	// Multi-select state
	selectedTasks   map[string]bool
	selectedRunners map[string]bool

	// Pause/resume state
	pausedProjects   map[string]bool
	allPaused        bool
	runnerController RunnerController // direct reference to embedded runner (nil if standalone)

	// Resource metrics
	metricsCollector *MetricsCollector
	resourceMetrics  ResourceMetrics

	// Log filtering: show only logs for the selected task
	filterLogsByTask bool

	// Log file truncation counter (triggers every 150 ticks ≈ 5 minutes)
	truncateCounter int

	// Status message for user feedback
	statusMessage     string
	statusMessageType string // "success", "error", "info"
	statusMessageTime time.Time

	// Auto-monitor state
	seenFeatureIDs      map[string]bool // tracks all feature_ids ever seen across task updates
	initialSnapshotDone bool            // prevents creating monitors for pre-existing features on first load
	monitorClient       *MonitorClient  // reusable client for monitor API calls

	// Feature toggle execution state
	enabledFeatures map[string]bool // features toggled on via x key

	// Content tab state (Project: Tasks/Brain/Automation, Global: Runners/Logs)
	activeContentTab       ContentTab
	activeAutomationSubTab AutomationSubTab
	dreamViewer            DreamViewer
	runnersPanel           RunnersPanel

	// User-resizable vertical split between the task pane and visible bottom panes.
	taskPanelHeight        int
	bottomTopPanelHeight   int
	splitDragActive        bool
	bottomSplitDragActive  bool
	splitDragOffsetY       int
	bottomSplitDragOffsetY int
}

// NewModel creates a new TUI model with the given configuration.
func NewModel(cfg Config) Model {
	// Force TrueColor support for proper selection highlighting
	// This ensures the blue background renders correctly in tmux
	lipgloss.SetColorProfile(termenv.TrueColor)

	// Recreate SelectedRowStyle AFTER setting color profile
	// Try using ANSI color code directly for maximum compatibility
	SelectedRowStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("4")).  // blue background (ANSI color 4)
		Foreground(lipgloss.Color("15")). // white text (ANSI color 15)
		Bold(true)

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
		config:                 cfg,
		keymap:                 KeyMapFromConfig(DefaultKeyMap(), cfg.KeyBindings),
		statusBar:              NewStatusBar(cfg.Project),
		helpBar:                NewHelpBar(),
		taskTree:               NewTaskTree(),
		taskDetail:             NewTaskDetail(),
		logViewer:              NewLogViewer(DefaultMaxLogEntries),
		scheduleList:           NewScheduleList(),
		scheduleDetail:         NewScheduleDetail(),
		modalManager:           NewModalManager(),
		settings:               settings,
		activePanel:            PanelTasks,
		sseClient:              NewSSEClient(cfg.APIURL, cfg.APIToken, cfg.Project),
		ctx:                    context.Background(),
		selectedTasks:          make(map[string]bool),
		selectedRunners:        make(map[string]bool),
		pausedProjects:         make(map[string]bool),
		runnerController:       cfg.Runner,
		tasksByProject:         make(map[string][]types.ResolvedTask),
		sseClients:             make(map[string]*SSEClient),
		metricsCollector:       NewMetricsCollector(),
		seenFeatureIDs:         make(map[string]bool),
		monitorClient:          NewMonitorClient(cfg.APIURL, cfg.APIToken),
		enabledFeatures:        make(map[string]bool),
		activeAutomationSubTab: AutomationSubTabAutomations,
		dreamViewer:            NewDreamViewer(),
		runnersPanel:           NewRunnersPanel(),
		taskPanelHeight:        settings.TaskPanelHeight,
		bottomTopPanelHeight:   settings.BottomTopPanelHeight,
	}

	// Wire TextWrap setting to sub-models
	m.taskTree.TextWrap = settings.TextWrap
	m.helpBar.TextWrap = settings.TextWrap

	// Propagate max parallel setting to the runner on startup
	if cfg.Runner != nil && settings.GlobalMaxParallel > 0 {
		cfg.Runner.SetMaxParallel(settings.GlobalMaxParallel)
	}

	// Create SSE clients for multi-project mode
	// Initialize ProjectTabs for multi-project mode
	if cfg.IsMultiProject() {
		m.projectTabs = NewProjectTabs(cfg.Projects)
		m.logViewer.SetMultiProject(true)
	}

	// Initialize activeProjectID for multi-project mode
	if cfg.IsMultiProject() {
		m.activeProjectID = "all"
	}
	if cfg.IsMultiProject() {
		for _, projectID := range cfg.Projects {
			m.sseClients[projectID] = NewSSEClient(cfg.APIURL, cfg.APIToken, projectID)
		}
	}

	// Wire log file persistence
	if cfg.Project != "" {
		var logPath string
		if cfg.LogDir != "" {
			logPath = filepath.Join(cfg.LogDir, cfg.Project, "tui-logs.jsonl")
		} else {
			homeDir, err := os.UserHomeDir()
			if err == nil {
				logPath = filepath.Join(homeDir, ".local", "log", "brain-runner", cfg.Project, "tui-logs.jsonl")
			}
		}
		if logPath != "" {
			m.logViewer.SetLogFile(logPath)
			// Load existing log entries (ignore errors — don't fail startup)
			_ = m.logViewer.LoadFromFile()
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

// apiRunnerConfig returns a runner.RunnerConfig populated from the TUI config.
// Use this instead of building RunnerConfig inline to ensure APIToken and
// a reasonable timeout are always included.
// The timeout is clamped to at least DefaultAPITimeout (15s) because the runner's
// default (5s) is too short for TUI operations like fetching entry metadata from
// a remote server (where DNS + TLS handshake can consume significant time).
func (m Model) apiRunnerConfig() runner.RunnerConfig {
	timeout := m.config.APITimeout
	if timeout < DefaultAPITimeout {
		timeout = DefaultAPITimeout
	}
	return runner.RunnerConfig{
		BrainAPIURL: m.config.APIURL,
		APIToken:    m.config.APIToken,
		APITimeout:  timeout,
	}
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
		m.dreamViewer.SetSize(msg.Width-4, msg.Height-10) // approximate inner size
		m.syncPanelSizes()
		return m, nil

	case TasksUpdatedMsg:
		// Always store tasks by project if ProjectID is set
		if msg.ProjectID != "" {
			m.tasksByProject[msg.ProjectID] = msg.Tasks
		}
		// In single-project mode, always update m.tasks directly
		// (SSE events include ProjectID even for single-project connections,
		// but syncActiveProjectView is a no-op in single-project mode)
		if !m.config.IsMultiProject() {
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

		// Update taskTree and scheduleList
		m.taskTree.SetTasks(m.tasks)
		m.scheduleList.SetTasks(m.tasks)

		// Sync task detail with current selection
		m.syncTaskDetail()

		// Auto-monitor detection: track feature_ids and create monitors for new ones
		autoMonitorCmds := m.processAutoMonitors(msg)

		// Continue listening for next SSE message
		// In multi-project mode, use the project-specific client
		var nextCmd tea.Cmd
		if msg.ProjectID != "" && m.sseClients[msg.ProjectID] != nil {
			nextCmd = m.sseClients[msg.ProjectID].WaitForNextMsg()
		} else {
			nextCmd = m.sseClient.WaitForNextMsg()
		}

		// Batch SSE continuation with any auto-monitor commands
		if len(autoMonitorCmds) > 0 {
			allCmds := append([]tea.Cmd{nextCmd}, autoMonitorCmds...)
			return m, tea.Batch(allCmds...)
		}
		return m, nextCmd

	case SSEConnectedMsg:
		m.connected = true
		m.statusBar.Connected = true
		m.addLog("info", "Connected to server")
		// Continue listening for next SSE message
		// Gap 4c: Route to per-project client if ProjectID is set
		if msg.ProjectID != "" && m.sseClients[msg.ProjectID] != nil {
			return m, m.sseClients[msg.ProjectID].WaitForNextMsg()
		}
		return m, m.sseClient.WaitForNextMsg()

	case SSEDisconnectedMsg:
		m.connected = false
		m.statusBar.Connected = false
		m.addLog("warn", "Disconnected from server")
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
		m.sseClient = NewSSEClient(m.config.APIURL, m.config.APIToken, m.config.Project)
		return m, m.sseClient.Connect(m.ctx)

	case reconnectProjectMsg:
		// Stop old per-project client and create a new one for reconnection
		if client, ok := m.sseClients[msg.ProjectID]; ok {
			client.Stop()
		}
		m.sseClients[msg.ProjectID] = NewSSEClient(m.config.APIURL, m.config.APIToken, msg.ProjectID)
		return m, m.sseClients[msg.ProjectID].Connect(m.ctx)

	case TickMsg:
		// Collect resource metrics
		m.resourceMetrics = m.metricsCollector.Collect()
		// Periodic log file truncation (~5 minutes at 2s ticks = 150 ticks)
		m.truncateCounter++
		if m.truncateCounter >= 150 {
			_ = m.logViewer.TruncateFile()
			m.truncateCounter = 0
		}
		// Schedule next tick and sync runner pause state
		cmds := []tea.Cmd{tickCmd(), fetchRunnerStatusCmd(m.apiRunnerConfig())}
		// Poll runner list when Runners tab is active (every tick = ~2s)
		if m.activeContentTab == ContentTabRunners {
			cmds = append(cmds, fetchRunnersListCmd(m.apiRunnerConfig()))
		}
		return m, tea.Batch(cmds...)

	case taskCompletedMsg:
		if msg.err != nil {
			m.setStatusMessage("error", fmt.Sprintf("Failed to complete task: %v", msg.err))
			m.addLog("error", fmt.Sprintf("Complete failed: %v", msg.err))
		} else {
			m.setStatusMessage("success", "Task completed successfully")
			m.addLog("info", "Task marked completed")
		}
		return m, nil

	case taskCancelledMsg:
		if msg.err != nil {
			m.setStatusMessage("error", fmt.Sprintf("Failed to cancel task: %v", msg.err))
			m.addLog("error", fmt.Sprintf("Cancel failed: %v", msg.err))
		} else {
			m.setStatusMessage("success", "Task cancelled successfully")
			m.addLog("info", "Task cancelled")
		}
		// Close modal after cancel completes
		m.modalManager.Close()
		return m, nil

	case batchTasksCompletedMsg:
		if len(msg.errors) > 0 {
			m.setStatusMessage("error", fmt.Sprintf("Completed %d tasks, %d failed", msg.successCount, msg.failedCount))
			m.addLog("warn", fmt.Sprintf("Batch complete: %d succeeded, %d failed", msg.successCount, msg.failedCount))
		} else {
			m.setStatusMessage("success", fmt.Sprintf("Completed %d tasks successfully", msg.successCount))
			m.addLog("info", fmt.Sprintf("Batch completed %d tasks", msg.successCount))
			// Clear selection on success
			m.clearSelection()
		}
		return m, nil

	case batchTasksCancelledMsg:
		if len(msg.errors) > 0 {
			m.setStatusMessage("error", fmt.Sprintf("Cancelled %d tasks, %d failed", msg.successCount, msg.failedCount))
			m.addLog("warn", fmt.Sprintf("Batch cancel: %d succeeded, %d failed", msg.successCount, msg.failedCount))
		} else {
			m.setStatusMessage("success", fmt.Sprintf("Cancelled %d tasks successfully", msg.successCount))
			m.addLog("info", fmt.Sprintf("Batch cancelled %d tasks", msg.successCount))
			// Clear selection on success
			m.clearSelection()
		}
		// Close modal after batch cancel completes
		m.modalManager.Close()
		return m, nil

	case statusPickerResultMsg:
		m.modalManager.Close()
		if msg.err != nil {
			if msg.failedCount > 0 && msg.successCount > 0 {
				m.setStatusMessage("error", fmt.Sprintf("Status → %s: %d succeeded, %d failed", msg.newStatus, msg.successCount, msg.failedCount))
				m.addLog("warn", fmt.Sprintf("Status change partial: %d/%d failed", msg.failedCount, msg.successCount+msg.failedCount))
			} else {
				m.setStatusMessage("error", fmt.Sprintf("Failed to change status: %v", msg.err))
				m.addLog("error", fmt.Sprintf("Status change failed: %v", msg.err))
			}
		} else {
			count := msg.successCount
			if count == 1 {
				m.setStatusMessage("success", fmt.Sprintf("Status changed to %s", msg.newStatus))
				m.addLog("info", fmt.Sprintf("Status → %s", msg.newStatus))
			} else {
				m.setStatusMessage("success", fmt.Sprintf("Changed %d tasks to %s", count, msg.newStatus))
				m.addLog("info", fmt.Sprintf("Batch status → %s (%d tasks)", msg.newStatus, count))
				m.clearSelection()
			}
		}
		return m, nil

	case LogEntryMsg:
		m.logViewer.AddEntry(msg.Entry)
		return m, nil

	case SessionDiscoveredMsg:
		// Store session ID on the matching task in-memory
		for i := range m.tasks {
			if m.tasks[i].Path == msg.TaskPath {
				if m.tasks[i].Sessions == nil {
					m.tasks[i].Sessions = make(map[string]types.SessionInfo)
				}
				m.tasks[i].Sessions[msg.SessionID] = types.SessionInfo{
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				}
				m.addLog("info", fmt.Sprintf("Session discovered: %s", msg.SessionID))
				break
			}
		}
		return m, nil

	case DreamContentMsg:
		if msg.Error != nil {
			m.dreamViewer.SetError(msg.Error.Error())
		} else {
			m.dreamViewer.SetContent(msg.Content)
		}
		return m, nil

	case DreamConfigMsg:
		if msg.Error != nil {
			m.dreamViewer.SetDreamConfigError(msg.Error.Error())
		} else if msg.Config != nil {
			m.dreamViewer.SetDreamConfig(*msg.Config)
		}
		return m, nil

	case RunnersUpdatedMsg:
		m.runnersPanel.SetRunners(msg.Runners)
		m.pruneRunnerSelection()
		return m, nil

	case batchRunnersShutdownMsg:
		m.modalManager.Close()
		if msg.failedCount > 0 {
			m.setStatusMessage("info", fmt.Sprintf("✓ %d shutdown, ✗ %d failed", msg.successCount, msg.failedCount))
			return m, nil
		}
		m.setStatusMessage("success", fmt.Sprintf("✓ %d runner(s) shutdown", msg.successCount))
		m.clearRunnerSelection()
		return m, fetchRunnersListCmd(m.apiRunnerConfig())

	case featureExecutedMsg:
		if msg.err != nil {
			m.setStatusMessage("error", fmt.Sprintf("Feature execute error: %v", msg.err))
		} else if msg.started > 0 {
			m.setStatusMessage("success", fmt.Sprintf("Feature '%s' enabled — %d task(s) started", msg.featureID, msg.started))
		} else {
			m.setStatusMessage("info", fmt.Sprintf("Feature '%s' enabled — no ready tasks to start", msg.featureID))
		}
		return m, nil

	case taskExecutedMsg:
		if msg.err != nil {
			if msg.claimedBy != "" {
				m.setStatusMessage("error", fmt.Sprintf("✗ Already claimed by %s", msg.claimedBy))
				m.addTaskLog("error", fmt.Sprintf("Execute failed: already claimed by %s", msg.claimedBy), msg.taskID)
			} else {
				m.setStatusMessage("error", fmt.Sprintf("✗ Execute failed: %v", msg.err))
				m.addTaskLog("error", fmt.Sprintf("Execute failed: %v", msg.err), msg.taskID)
			}
		} else {
			m.setStatusMessage("success", "✓ Task executing")
			m.addTaskLog("info", "Task claimed for execution", msg.taskID)
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

	case sessionsFetchedMsg:
		if msg.err != nil {
			m.setStatusMessage("error", fmt.Sprintf("✗ Failed to fetch sessions: %v", msg.err))
			m.addLog("error", fmt.Sprintf("Session fetch failed: %v", msg.err))
			return m, nil
		}
		if len(msg.sessionIDs) == 0 {
			m.setStatusMessage("warn", "No sessions available for this task")
			m.addLog("warn", "No sessions available for this task")
			return m, nil
		}
		taskID := extractTaskID(msg.taskPath)
		if len(msg.sessionIDs) == 1 {
			if msg.tmuxMode {
				return m, openSessionTmux(msg.sessionIDs[0], taskID)
			}
			return m, openSessionFullscreen(msg.sessionIDs[0], taskID)
		}
		// Multiple sessions: open selection modal
		onSelect := func(sessionID string) tea.Msg {
			return sessionSelectedMsg{
				sessionID: sessionID,
				tmuxMode:  msg.tmuxMode,
				taskID:    taskID,
			}
		}
		modal := NewSessionSelectModal(msg.sessionIDs, msg.tmuxMode, onSelect)
		return m, m.modalManager.Open(modal)

	case sessionSelectedMsg:
		m.modalManager.Close()
		if msg.tmuxMode {
			return m, openSessionTmux(msg.sessionID, msg.taskID)
		}
		return m, openSessionFullscreen(msg.sessionID, msg.taskID)

	case sessionOpenedMsg:
		if msg.err != nil {
			m.setStatusMessage("error", fmt.Sprintf("✗ Session error: %v", msg.err))
		} else {
			m.setStatusMessage("success", "✓ Session closed")
		}
		return m, nil

	case editorClosedMsg:
		if msg.err != nil {
			m.setStatusMessage("error", fmt.Sprintf("✗ Editor error: %v", msg.err))
			// Clean up temp file
			if msg.tempFile != "" {
				os.RemoveAll(filepath.Dir(msg.tempFile))
			}
			return m, nil
		}

		// Read back the temp file and check for changes
		if msg.tempFile != "" {
			newContent, err := os.ReadFile(msg.tempFile)
			// Clean up temp file
			os.RemoveAll(filepath.Dir(msg.tempFile))

			if err != nil {
				m.setStatusMessage("error", fmt.Sprintf("✗ Failed to read edited file: %v", err))
				return m, nil
			}

			if string(newContent) == msg.originalContent {
				m.setStatusMessage("info", "No changes made")
				return m, nil
			}

			// Sync changes back to API
			apiClient := runner.NewAPIClient(m.apiRunnerConfig())
			_, err = apiClient.UpdateEntry(context.Background(), msg.taskPath, map[string]interface{}{
				"content": string(newContent),
			})
			if err != nil {
				m.setStatusMessage("error", fmt.Sprintf("✗ Failed to sync changes: %v", err))
				return m, nil
			}

			m.setStatusMessage("success", "✓ Task updated from editor")
		} else {
			m.setStatusMessage("success", "✓ File saved - refreshing...")
		}
		return m, nil

	case pauseToggledMsg:
		if msg.err != nil {
			// Revert optimistic update
			m.pausedProjects[msg.projectID] = !msg.paused
			m.setStatusMessage("error", fmt.Sprintf("Failed to toggle pause: %v", msg.err))
			m.addLog("error", fmt.Sprintf("Pause toggle failed: %v", msg.err))
		} else {
			if msg.paused {
				m.setStatusMessage("success", fmt.Sprintf("Project %s paused", msg.projectID))
				m.addLog("info", fmt.Sprintf("Project paused: %s", msg.projectID))
			} else {
				m.setStatusMessage("success", fmt.Sprintf("Project %s resumed", msg.projectID))
				m.addLog("info", fmt.Sprintf("Project resumed: %s", msg.projectID))
			}
		}
		m.syncHelpBarPauseState()
		return m, nil

	case pauseAllToggledMsg:
		if msg.err != nil {
			m.allPaused = !msg.paused
			m.setStatusMessage("error", fmt.Sprintf("Failed to toggle pause all: %v", msg.err))
			m.addLog("error", fmt.Sprintf("Pause all toggle failed: %v", msg.err))
		} else {
			if msg.paused {
				m.setStatusMessage("success", "All projects paused")
				m.addLog("info", "All projects paused")
			} else {
				m.setStatusMessage("success", "All projects resumed")
				m.addLog("info", "All projects resumed")
			}
		}
		m.syncHelpBarPauseState()
		return m, nil

	case runnerStatusMsg:
		if msg.err == nil {
			// If we have a direct runner controller, get pause state from it
			// instead of from the API server's separate RunnerService
			if m.runnerController != nil {
				m.allPaused = m.runnerController.IsAllPaused()
				// Per-project pause state from the embedded runner
				m.pausedProjects = make(map[string]bool)
				for _, proj := range m.config.Projects {
					if m.runnerController.IsPaused(proj) && !m.runnerController.IsAllPaused() {
						m.pausedProjects[proj] = true
					}
				}
			} else {
				m.allPaused = msg.paused
				m.pausedProjects = make(map[string]bool)
				for _, id := range msg.pausedProjects {
					m.pausedProjects[id] = true
				}
			}
		}
		m.syncHelpBarPauseState()
		return m, nil

	case autoMonitorCreatedMsg:
		if msg.err != nil {
			// Silently ignore errors (matching TS behavior - 409 expected)
			return m, nil
		}
		m.setStatusMessage("success", fmt.Sprintf("Auto-monitor created: %s for %s", msg.templateID, msg.featureID))
		return m, nil

	case ProcessStartedMsg:
		// Track the runner child process PID for resource metrics
		if m.metricsCollector != nil && msg.PID > 0 {
			_ = m.metricsCollector.TrackProcess(int32(msg.PID))
		}
		return m, nil

	case ProcessStoppedMsg:
		// Untrack the runner child process PID from resource metrics
		if m.metricsCollector != nil && msg.PID > 0 {
			m.metricsCollector.UntrackProcess(int32(msg.PID))
		}
		return m, nil

	case SettingsChangedMsg:
		// Reload settings and re-apply task grouping
		settings, err := LoadSettings()
		if err == nil {
			m.settings = settings
			m.taskTree.SetTasks(m.tasks)

			// Propagate max parallel setting to the runner
			if m.runnerController != nil && settings.GlobalMaxParallel > 0 {
				m.runnerController.SetMaxParallel(settings.GlobalMaxParallel)
			}
		}
		return m, nil
	}

	// Route unhandled messages to the active modal (e.g., metadataFetchedMsg,
	// metadataUpdatedMsg, monitorTemplatesFetchedMsg, monitorToggleResultMsg).
	// Key events are already routed via handleKeyMsg -> modalManager.HandleKey(),
	// but async command results (tea.Cmd responses) need to be forwarded here.
	//
	// IMPORTANT: Don't consume messages that the main Update needs to handle
	// (like sessionSelectedMsg, taskExecutedMsg, etc.). Only forward if the
	// modal's Update actually produces a command.
	if m.modalManager.IsOpen() {
		// Don't intercept messages that are results from modal actions —
		// these need to flow through the main switch to close the modal
		// and trigger the next action.
		switch msg.(type) {
		case sessionSelectedMsg, taskExecutedMsg, featureExecutedMsg, taskCompletedMsg, taskCancelledMsg,
			batchTasksCompletedMsg, batchTasksCancelledMsg, taskDeletedMsg, batchTasksDeletedMsg,
			sessionOpenedMsg, statusPickerResultMsg:
			// Let these fall through to the main switch above (they won't match
			// because we're past it, so we need to handle them here)
		default:
			var cmd tea.Cmd
			m.modalManager, cmd = m.modalManager.Update(msg)
			if cmd != nil {
				return m, cmd
			}
		}
	}

	return m, nil
}

// handleKeyMsg processes keyboard input.
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If modal is open, route keys to modal first
	if m.modalManager.IsOpen() {
		// Convert tea.KeyMsg to a string key name for the modal
		keyStr := tea.Key(msg).String()

		handled, cmd := m.modalManager.HandleKey(keyStr)
		if handled {
			return m, cmd
		}
	}

	// If on Automation tab with search typing mode, handle search input first
	if m.activeContentTab == ContentTabAutomation && m.dreamViewer.SearchMode() == DreamSearchTyping {
		return m.handleDreamSearchInput(msg)
	}

	// If in filter typing mode, handle filter input first
	if m.filterState == FilterTyping {
		return m.handleFilterInput(msg)
	}

	// If in filter locked mode, handle Esc to clear and / to re-enter typing
	if m.filterState == FilterLocked {
		switch msg.Type {
		case tea.KeyEsc:
			m.filterState = FilterOff
			m.filterQuery = ""
			m.clearFilter()
			return m, nil
		case tea.KeyRunes:
			if string(msg.Runes) == "/" {
				// Re-enter typing mode, keep current query
				m.filterState = FilterTyping
				return m, nil
			}
		}
		// All other keys fall through to normal handling
	}

	// Esc clears multi-select mode when tasks are selected
	if msg.Type == tea.KeyEsc && len(m.selectedTasks) > 0 {
		m.selectedTasks = make(map[string]bool)
		return m, nil
	}

	// Configurable keybindings (checked via key.Matches before hardcoded switch).
	// These must be checked before the msg.Type switch because some bindings
	// (e.g., ctrl+l) use special key types, not KeyRunes.
	// Skip toggle-logs in multi-project mode where 'l' is used for project tab navigation
	if key.Matches(msg, m.keymap.ToggleLogs) && !m.config.IsMultiProject() {
		m.logsVisible = !m.logsVisible
		if !m.logsVisible && m.activePanel == PanelLogs {
			m.activePanel = PanelTasks
		}
		m.syncPanelSizes()
		return m, nil
	}
	if key.Matches(msg, m.keymap.ToggleDetail) {
		m.detailVisible = !m.detailVisible
		if !m.detailVisible && m.activePanel == PanelDetails {
			m.activePanel = PanelTasks
		}
		if m.detailVisible {
			m.syncTaskDetail()
		} else {
			m.syncPanelSizes()
		}
		return m, nil
	}
	if key.Matches(msg, m.keymap.NextContentTab) {
		m.activeContentTab = nextContentTab(m.activeContentTab)
		m.helpBar.ActiveContentTab = m.activeContentTab
		// Fetch dream content/config lazily when switching to Automation tab.
		if m.activeContentTab == ContentTabAutomation && (!m.dreamViewer.HasContent() || !m.dreamViewer.HasConfig()) {
			m.prepareDreamFetch()
			return m, m.fetchDreamTabCmd()
		}
		// Fetch runners lazily when switching to Runners tab
		if m.activeContentTab == ContentTabRunners {
			return m, fetchRunnersListCmd(m.apiRunnerConfig())
		}
		return m, nil
	}
	if key.Matches(msg, m.keymap.PrevContentTab) {
		m.activeContentTab = prevContentTab(m.activeContentTab)
		m.helpBar.ActiveContentTab = m.activeContentTab
		// Fetch dream content/config lazily when switching to Automation tab.
		if m.activeContentTab == ContentTabAutomation && (!m.dreamViewer.HasContent() || !m.dreamViewer.HasConfig()) {
			m.prepareDreamFetch()
			return m, m.fetchDreamTabCmd()
		}
		// Fetch runners lazily when switching to Runners tab
		if m.activeContentTab == ContentTabRunners {
			return m, fetchRunnersListCmd(m.apiRunnerConfig())
		}
		return m, nil
	}

	// When on Runners tab, handle runner-specific keys
	if m.activeContentTab == ContentTabRunners {
		switch msg.Type {
		case tea.KeyCtrlC:
			m.sseClient.Stop()
			return m, tea.Quit
		case tea.KeyEsc:
			if len(m.selectedRunners) > 0 {
				m.clearRunnerSelection()
				return m, nil
			}
			m.activeContentTab = ContentTabTasks
			m.helpBar.ActiveContentTab = m.activeContentTab
			return m, nil
		case tea.KeySpace:
			m.toggleRunnerSelection()
			return m, nil
		}
		switch string(msg.Runes) {
		case "j":
			m.runnersPanel.MoveDown()
			return m, nil
		case "k":
			m.runnersPanel.MoveUp()
			return m, nil
		case "g":
			m.runnersPanel.GotoTop()
			return m, nil
		case "G":
			m.runnersPanel.GotoBottom()
			return m, nil
		case "r":
			// Refresh runners list
			return m, fetchRunnersListCmd(m.apiRunnerConfig())
		case " ":
			m.toggleRunnerSelection()
			return m, nil
		case "A":
			m.selectAllOnlineRunners()
			return m, nil
		case "D":
			m.clearRunnerSelection()
			return m, nil
		case "X":
			runnerIDs, runnerTitles := m.runnerShutdownTargets()
			if len(runnerIDs) == 0 {
				return m, nil
			}
			cfg := m.apiRunnerConfig()
			message := fmt.Sprintf("Shutdown %d runner(s)?", len(runnerIDs))
			modal := NewConfirmModal("Shutdown Runners", message).
				WithTaskTitles(runnerTitles).
				WithDestructive(true).
				WithOnConfirm(func() tea.Msg {
					return batchShutdownRunnersCmd(cfg, runnerIDs)()
				})
			return m, m.modalManager.Open(modal)
		case "q":
			m.sseClient.Stop()
			return m, tea.Quit
		case "?":
			m.modalManager.Open(NewHelpModal(m.config.IsMultiProject()))
			return m, nil
		}
		return m, nil
	}

	// Logs is a global tab, so keep its keys scoped to log-view actions.
	if m.activeContentTab == ContentTabLogs {
		switch msg.Type {
		case tea.KeyCtrlC:
			m.sseClient.Stop()
			return m, tea.Quit
		case tea.KeyEsc:
			m.activeContentTab = ContentTabTasks
			m.helpBar.ActiveContentTab = m.activeContentTab
			return m, nil
		}
		switch string(msg.Runes) {
		case "f":
			m.filterLogsByTask = !m.filterLogsByTask
			return m, nil
		case "q":
			m.sseClient.Stop()
			return m, tea.Quit
		case "?":
			m.modalManager.Open(NewHelpModal(m.config.IsMultiProject()))
			return m, nil
		}
		return m, nil
	}

	// When on Automation tab, forward non-rune keys (ctrl+d/u/f/b, arrows, pgup/pgdn)
	// to the viewport for vim-style scrolling. Only intercept quit keys.
	if m.activeContentTab == ContentTabAutomation {
		switch msg.Type {
		case tea.KeyCtrlC:
			m.sseClient.Stop()
			return m, tea.Quit
		case tea.KeyEsc:
			// If search is locked, Esc cancels search
			if m.dreamViewer.SearchMode() == DreamSearchLocked {
				m.dreamViewer.CancelSearch()
				return m, nil
			}
			// Otherwise, allow Escape to work normally (close modals, etc.)
		case tea.KeyRunes:
			// Fall through to rune handling below
		default:
			// Forward ctrl+d, ctrl+u, ctrl+f, ctrl+b, arrows, pgup, pgdn, etc.
			cmd := m.dreamViewer.Update(msg)
			return m, cmd
		}
	}

	switch msg.Type {
	case tea.KeyBackspace:
		// Delete task(s) - with confirmation modal (tasks view only)
		// Only handle when NOT in filter mode (filter mode consumes backspace for editing)
		if m.filterState == FilterOff && m.viewMode == ViewModeTasks && m.activePanel == PanelTasks {
			apiClient := runner.NewAPIClient(m.apiRunnerConfig())

			// Case 1: Multi-select mode - batch delete
			if len(m.selectedTasks) > 0 {
				count := len(m.selectedTasks)
				taskIDs := make([]string, 0, count)
				taskPaths := make([]string, 0, count)
				taskTitles := make([]string, 0, count)
				for id := range m.selectedTasks {
					taskIDs = append(taskIDs, id)
					// Find task to get path and title
					for _, t := range m.tasks {
						if t.ID == id {
							taskPaths = append(taskPaths, t.Path)
							taskTitles = append(taskTitles, t.Title)
							break
						}
					}
				}

				message := fmt.Sprintf("Delete %d task(s)?", count)
				modal := NewConfirmModal("Delete Tasks", message).
					WithTaskTitles(taskTitles).
					WithDestructive(true).
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

			message := fmt.Sprintf("Delete %d task(s)?", 1)
			modal := NewConfirmModal("Delete Task", message).
				WithTaskTitles([]string{selectedTask.Title}).
				WithDestructive(true).
				WithOnConfirm(func() tea.Msg {
					return deleteTaskCmd(apiClient, selectedTask.Path)()
				})
			return m, m.modalManager.Open(modal)
		}
		return m, nil

	case tea.KeyCtrlC:
		m.sseClient.Stop()
		return m, tea.Quit

	case tea.KeyTab:
		m.activePanel = NextPanel(m.activePanel, m.detailVisible, m.logsVisible)
		m.helpBar.ActivePanel = m.activePanel
		return m, nil

	case tea.KeyEnter:
		// Enter toggles group collapse when on a group header
		if m.activePanel == PanelTasks && m.taskTree.IsOnGroupHeader() {
			m.taskTree.ToggleCollapse()
		}
		return m, nil

	case tea.KeySpace:
		// Space toggles group collapse when on group header, selection when on task.
		// NOTE: Bubbletea sends Space as KeySpace (not KeyRunes with ' '), so this
		// must be handled separately from the KeyRunes switch below.
		if m.activePanel == PanelTasks {
			if m.taskTree.IsOnGroupHeader() {
				m.taskTree.ToggleCollapse()
			} else {
				m.toggleTaskSelection()
			}
		}
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

		// When on Automation tab, handle dream-specific keys then forward the rest to the viewport
		if m.activeContentTab == ContentTabAutomation {
			switch string(msg.Runes) {
			case "/":
				m.dreamViewer.StartSearch()
				return m, nil
			case "n":
				if m.dreamViewer.SearchMode() == DreamSearchLocked {
					m.dreamViewer.NextMatch()
				}
				return m, nil
			case "N":
				if m.dreamViewer.SearchMode() == DreamSearchLocked {
					m.dreamViewer.PrevMatch()
				}
				return m, nil
			case "g":
				m.dreamViewer.GotoTop()
				return m, nil
			case "G":
				m.dreamViewer.GotoBottom()
				return m, nil
			case "r":
				// Re-fetch dream content and monitor configuration.
				m.prepareDreamFetch()
				return m, m.fetchDreamTabCmd()
			case "q":
				m.sseClient.Stop()
				return m, tea.Quit
			case "?":
				modal := NewHelpModal(m.config.IsMultiProject())
				cmd := m.modalManager.Open(modal)
				return m, cmd
			}
			// Forward all other keys to the viewport for vim-style navigation
			// (j/k, g/G, ctrl+d/u, ctrl+f/b, d/u, f/b, pgup/pgdn, space, arrow keys)
			cmd := m.dreamViewer.Update(msg)
			return m, cmd
		}

		switch string(msg.Runes) {
		case "?":
			// Open help modal
			modal := NewHelpModal(m.config.IsMultiProject())
			cmd := m.modalManager.Open(modal)
			return m, cmd
		case "S":
			// Open settings modal with task counts per status group and per-project running counts
			taskCounts := m.computeTaskCountsByStatus()
			runningPerProject := m.computeRunningPerProject()
			modal := NewSettingsModal(m.settings, WithTaskCounts(taskCounts), WithRunningPerProject(runningPerProject))
			cmd := m.modalManager.Open(modal)
			return m, cmd
		case "s":
			// Open metadata modal for selected task(s) (tasks view only)
			if m.viewMode != ViewModeTasks {
				return m, nil
			}
			if m.activePanel == PanelTasks {
				apiClient := runner.NewAPIClient(m.apiRunnerConfig())

				// Case 0: Feature header selected → open MetadataModalFeature
				featureID := m.taskTree.GetSelectedFeatureID()
				if featureID != "" && featureID != "[Ungrouped]" {
					featureModal := NewMetadataModalFeature(featureID, m.config.Project, apiClient, m.monitorClient)
					if featureModal != nil {
						if featureTasks := m.taskTree.GetSelectedFeatureTasks(); len(featureTasks) > 0 {
							taskPaths := make([]string, len(featureTasks))
							for i, t := range featureTasks {
								taskPaths[i] = t.Path
							}
							featureModal.taskPaths = taskPaths
						}
						cmd := m.modalManager.Open(featureModal)
						return m, cmd
					}
				}

				// Case 0b: Non-feature group header (Ungrouped, Draft, Inactive, terminal sub-features)
				// → open batch metadata editor for all tasks in the group
				if m.taskTree.IsOnGroupHeader() {
					if groupTasks := m.taskTree.GetSelectedGroupTasks(); len(groupTasks) > 0 {
						taskPaths := make([]string, 0, len(groupTasks))
						for _, t := range groupTasks {
							taskPaths = append(taskPaths, t.Path)
						}
						modal := NewMetadataModalBatch(taskPaths, apiClient)
						cmd := m.modalManager.Open(modal)
						return m, cmd
					}
				}

				var modal Modal

				// Case 1: Multi-select active → batch metadata editor
				if len(m.selectedTasks) > 0 {
					taskPaths := make([]string, 0, len(m.selectedTasks))
					for id := range m.selectedTasks {
						for _, t := range m.tasks {
							if t.ID == id {
								taskPaths = append(taskPaths, t.Path)
								break
							}
						}
					}
					modal = NewMetadataModalBatch(taskPaths, apiClient)
				} else {
					// Case 2: Single task selected → full metadata editor
					selectedTask := m.taskTree.SelectedTask()
					if selectedTask != nil {
						modal = NewMetadataModal(selectedTask.Path, apiClient)
					}
				}

				if modal != nil {
					cmd := m.modalManager.Open(modal)
					return m, cmd
				}
			}
			return m, nil
		case "c":
			// Complete task(s) - no confirmation required (tasks view only)
			if m.viewMode != ViewModeTasks {
				return m, nil
			}
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
					return m, batchCompleteTasksCmd(m.apiRunnerConfig(), taskPaths, taskIDs)
				}

				// Case 2: Single task selected
				selectedTask := m.taskTree.SelectedTask()
				if selectedTask != nil {
					return m, completeTaskCmd(m.apiRunnerConfig(), selectedTask.Path)
				}
			}
			return m, nil
		case "C":
			// Toggle view mode between tasks and schedules
			if m.viewMode == ViewModeTasks {
				m.viewMode = ViewModeSchedules
				m.detailVisible = true
			} else {
				m.viewMode = ViewModeTasks
			}
			m.activePanel = PanelTasks
			m.helpBar.ViewMode = m.viewMode
			m.selectedTasks = make(map[string]bool)
			m.syncHelpBarSelectionState()
			m.filterState = FilterOff
			m.filterQuery = ""
			m.clearFilter()
			return m, nil
		case "X":
			// Cancel task - only in_progress tasks, with confirmation modal (matching TS)
			if m.viewMode != ViewModeTasks || m.activePanel != PanelTasks {
				return m, nil
			}
			selectedTask := m.taskTree.SelectedTask()
			if selectedTask == nil || selectedTask.Status != "in_progress" {
				return m, nil
			}
			confirmMsg := fmt.Sprintf("Cancel task '%s'?", selectedTask.Title)
			taskPath := selectedTask.Path
			cfg := m.apiRunnerConfig()
			modal := NewConfirmModal("Confirm Cancel", confirmMsg).
				WithOnConfirm(func() tea.Msg {
					return cancelTaskCmd(cfg, taskPath)()
				})
			cmd := m.modalManager.Open(modal)
			return m, cmd
		case "x":
			// Execute task - claim and spawn for immediate execution (tasks view only)
			if m.viewMode != ViewModeTasks || m.activePanel != PanelTasks {
				return m, nil
			}

			selectedTask := m.taskTree.SelectedTask()
			if selectedTask == nil {
				// Check if cursor is on a feature header → toggle feature execution
				featureID := m.taskTree.GetSelectedFeatureID()
				featureTasks := m.taskTree.GetSelectedFeatureTasks()
				if featureID != "" && len(featureTasks) > 0 && m.runnerController != nil {
					projectID := m.config.Project
					if m.activeProjectID != "" && m.activeProjectID != "all" {
						projectID = m.activeProjectID
					}

					if m.enabledFeatures[featureID] {
						// DISABLE: remove from enabled set
						delete(m.enabledFeatures, featureID)
						m.runnerController.DisableFeature(featureID)
						m.taskTree.SetEnabledFeatures(m.enabledFeatures)
						m.setStatusMessage("info", fmt.Sprintf("Feature '%s' disabled", featureID))
					} else {
						// ENABLE: add to enabled set + batch-execute ready tasks
						m.enabledFeatures[featureID] = true
						m.runnerController.EnableFeature(featureID)
						m.taskTree.SetEnabledFeatures(m.enabledFeatures)

						// Fire-and-forget batch execution
						rc := m.runnerController
						tasksCopy := make([]types.ResolvedTask, len(featureTasks))
						copy(tasksCopy, featureTasks)
						pid := projectID
						fid := featureID
						return m, func() tea.Msg {
							ctx := context.Background()
							started, err := rc.ExecuteFeature(ctx, tasksCopy, pid)
							return featureExecutedMsg{featureID: fid, started: started, err: err}
						}
					}
				}
				return m, nil
			}

			// Determine action label based on task status
			actionLabel := "Execute"
			if selectedTask.Status == "in_progress" {
				actionLabel = "Resume"
			}

			// Determine project ID for execution
			projectID := m.config.Project
			if m.activeProjectID != "" && m.activeProjectID != "all" {
				projectID = m.activeProjectID
			}

			message := fmt.Sprintf("%s task '%s' now?", actionLabel, selectedTask.Title)
			rc := m.runnerController
			taskCopy := *selectedTask // copy for closure

			modal := NewConfirmModal(actionLabel+" Task", message).
				WithOnConfirm(func() tea.Msg {
					if rc != nil {
						// Use embedded runner for full pipeline (claim + spawn)
						return executeTaskViaRunnerCmd(rc, &taskCopy, projectID)()
					}
					// Fallback: API-only claim (no spawn)
					runnerID := "manual-tui"
					if m.config.RunnerID != "" {
						runnerID = m.config.RunnerID
					}
					apiClient := runner.NewAPIClient(m.apiRunnerConfig())
					return executeTaskCmd(apiClient, projectID, taskCopy.ID, runnerID)()
				})
			return m, m.modalManager.Open(modal)
		case "d":
			// Delete task(s) - with confirmation modal (tasks view only)
			if m.viewMode != ViewModeTasks || m.activePanel != PanelTasks {
				return m, nil
			}

			apiClient := runner.NewAPIClient(m.apiRunnerConfig())

			// Case 1: Multi-select mode - batch delete
			if len(m.selectedTasks) > 0 {
				count := len(m.selectedTasks)
				taskIDs := make([]string, 0, count)
				taskPaths := make([]string, 0, count)
				taskTitles := make([]string, 0, count)
				for id := range m.selectedTasks {
					taskIDs = append(taskIDs, id)
					// Find task to get path and title
					for _, t := range m.tasks {
						if t.ID == id {
							taskPaths = append(taskPaths, t.Path)
							taskTitles = append(taskTitles, t.Title)
							break
						}
					}
				}

				message := fmt.Sprintf("Delete %d task(s)?", count)
				modal := NewConfirmModal("Delete Tasks", message).
					WithTaskTitles(taskTitles).
					WithDestructive(true).
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

			message := fmt.Sprintf("Delete %d task(s)?", 1)
			modal := NewConfirmModal("Delete Task", message).
				WithTaskTitles([]string{selectedTask.Title}).
				WithDestructive(true).
				WithOnConfirm(func() tea.Msg {
					return deleteTaskCmd(apiClient, selectedTask.Path)()
				})
			return m, m.modalManager.Open(modal)
		case "e":
			// Edit task in $EDITOR (tasks view only)
			// Fetches content from API, writes to temp file, opens editor,
			// then syncs changes back on close.
			if m.viewMode != ViewModeTasks || m.activePanel != PanelTasks {
				return m, nil
			}

			selectedTask := m.taskTree.SelectedTask()
			if selectedTask == nil {
				return m, nil
			}

			// Fetch entry content from API and write to temp file
			apiClient := runner.NewAPIClient(m.apiRunnerConfig())
			entry, err := apiClient.GetEntry(context.Background(), selectedTask.Path)
			if err != nil {
				m.setStatusMessage("error", fmt.Sprintf("Failed to fetch task: %v", err))
				return m, nil
			}

			// Write content to temp file
			tempDir, err := os.MkdirTemp("", "brain-task-")
			if err != nil {
				m.setStatusMessage("error", fmt.Sprintf("Failed to create temp dir: %v", err))
				return m, nil
			}
			tempFile := filepath.Join(tempDir, selectedTask.ID+".md")
			if err := os.WriteFile(tempFile, []byte(entry.Content), 0o644); err != nil {
				m.setStatusMessage("error", fmt.Sprintf("Failed to write temp file: %v", err))
				return m, nil
			}

			originalContent := entry.Content
			taskID := selectedTask.ID
			taskPath := selectedTask.Path

			return m, tea.ExecProcess(getEditorCmd(tempFile), func(err error) tea.Msg {
				return editorClosedMsg{
					taskID:          taskID,
					taskPath:        taskPath,
					tempFile:        tempFile,
					originalContent: originalContent,
					err:             err,
				}
			})
		case "o":
			// Open session fullscreen (tasks view only)
			if m.viewMode != ViewModeTasks || m.activePanel != PanelTasks {
				return m, nil
			}
			selectedTask := m.taskTree.SelectedTask()
			if selectedTask == nil {
				return m, nil
			}
			return m, m.openSessionForTask(selectedTask, false)
		case "O":
			// Open session in tmux (tasks view only)
			if m.viewMode != ViewModeTasks || m.activePanel != PanelTasks {
				return m, nil
			}
			selectedTask := m.taskTree.SelectedTask()
			if selectedTask == nil {
				return m, nil
			}
			return m, m.openSessionForTask(selectedTask, true)
		case "/":
			// Activate filter typing mode
			if m.activePanel == PanelTasks {
				m.filterState = FilterTyping
				m.filterQuery = ""
			}
			return m, nil
		case "q":
			m.sseClient.Stop()
			return m, tea.Quit
		case "r":
			// Refresh: reconnect SSE to get fresh snapshot
			m.sseClient.Stop()
			m.sseClient = NewSSEClient(m.config.APIURL, m.config.APIToken, m.config.Project)
			return m, m.sseClient.Connect(m.ctx)
		case "w":
			m.settings.TextWrap = !m.settings.TextWrap
			m.taskTree.TextWrap = m.settings.TextWrap
			m.helpBar.TextWrap = m.settings.TextWrap
			_ = SaveSettings(m.settings)
			return m, nil
		case "f":
			// Toggle log filtering by selected task (when logs panel is focused)
			if m.activePanel == PanelLogs {
				m.filterLogsByTask = !m.filterLogsByTask
				return m, nil
			}
			return m, nil
		case "j":
			if m.activePanel == PanelTasks {
				if m.viewMode == ViewModeSchedules {
					m.scheduleList.MoveDown()
					m.syncScheduleDetail()
				} else {
					m.taskTree.MoveDown()
					m.syncTaskDetail()
				}
			} else if m.activePanel == PanelDetails {
				if m.viewMode == ViewModeSchedules {
					m.scheduleDetail.ScrollDown()
				} else {
					m.taskDetail.ScrollDown()
				}
			}
			return m, nil
		case "k":
			if m.activePanel == PanelTasks {
				if m.viewMode == ViewModeSchedules {
					m.scheduleList.MoveUp()
					m.syncScheduleDetail()
				} else {
					m.taskTree.MoveUp()
					m.syncTaskDetail()
				}
			} else if m.activePanel == PanelDetails {
				if m.viewMode == ViewModeSchedules {
					m.scheduleDetail.ScrollUp()
				} else {
					m.taskDetail.ScrollUp()
				}
			}
			return m, nil
		case "g":
			if m.activePanel == PanelTasks {
				if m.viewMode == ViewModeSchedules {
					m.scheduleList.MoveToTop()
					m.syncScheduleDetail()
				} else {
					m.taskTree.MoveToTop()
					m.syncTaskDetail()
				}
			} else if m.activePanel == PanelDetails {
				if m.viewMode == ViewModeSchedules {
					m.scheduleDetail.ScrollToTop()
				} else {
					m.taskDetail.ScrollToTop()
				}
			}
			return m, nil
		case "G":
			if m.activePanel == PanelTasks {
				if m.viewMode == ViewModeSchedules {
					m.scheduleList.MoveToBottom()
					m.syncScheduleDetail()
				} else {
					m.taskTree.MoveToBottom()
					m.syncTaskDetail()
				}
			} else if m.activePanel == PanelDetails {
				if m.viewMode == ViewModeSchedules {
					m.scheduleDetail.ScrollToBottom()
				} else {
					m.taskDetail.ScrollToBottom()
				}
			}
			return m, nil
		case " ":
			// NOTE: In bubbletea v0.22+, Space is sent as tea.KeySpace (handled above),
			// NOT as tea.KeyRunes with ' '. This case is kept for backwards compatibility
			// with older bubbletea versions or custom key handling.
			if m.activePanel == PanelTasks {
				if m.taskTree.IsOnGroupHeader() {
					m.taskTree.ToggleCollapse()
				} else {
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
			// Pause/resume active project.
			// In single-project mode, if allPaused is true (startup default),
			// pressing 'p' should toggle allPaused to start execution.
			projectID := m.activeProjectID
			if projectID == "" || projectID == "all" {
				projectID = m.config.Project
			}
			if projectID != "" {
				if m.allPaused {
					// Global pause is active; toggle it off to start running
					m.allPaused = false
					m.setStatusMessage("info", fmt.Sprintf("Resuming project %s...", projectID))
					m.syncHelpBarPauseState()
					return m, pauseAllCmd(m.apiRunnerConfig(), true, m.runnerController) // currentlyPaused=true -> will resume
				}
				// Single-project mode: check if we should re-pause globally
				if !m.config.IsMultiProject() && !m.pausedProjects[projectID] {
					// For single-project: 'p' toggles allPaused for consistent behavior
					m.allPaused = true
					m.setStatusMessage("info", fmt.Sprintf("Pausing project %s...", projectID))
					m.syncHelpBarPauseState()
					return m, pauseAllCmd(m.apiRunnerConfig(), false, m.runnerController) // currentlyPaused=false -> will pause
				}
				// Multi-project mode: toggle per-project pause
				currentlyPaused := m.pausedProjects[projectID]
				m.pausedProjects[projectID] = !currentlyPaused
				if currentlyPaused {
					m.setStatusMessage("info", fmt.Sprintf("Resuming project %s...", projectID))
				} else {
					m.setStatusMessage("info", fmt.Sprintf("Pausing project %s...", projectID))
				}
				m.syncHelpBarPauseState()
				return m, pauseProjectCmd(m.apiRunnerConfig(), projectID, currentlyPaused, m.runnerController)
			}
			return m, nil
		case "y":
			// Yank (copy) selected task title to clipboard (tasks view only)
			if m.viewMode != ViewModeTasks || m.activePanel != PanelTasks {
				return m, nil
			}
			selectedTask := m.taskTree.SelectedTask()
			if selectedTask == nil {
				return m, nil
			}
			if CopyToClipboard(selectedTask.Title) {
				m.setStatusMessage("success", fmt.Sprintf("Copied: %s", selectedTask.Title))
			} else {
				m.setStatusMessage("error", "Failed to copy to clipboard")
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
			return m, pauseAllCmd(m.apiRunnerConfig(), !m.allPaused, m.runnerController)
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
	// If modal is open, forward mouse events to the modal
	if m.modalManager.IsOpen() {
		handled, cmd := m.modalManager.HandleMouse(msg, m.width, m.height)
		if handled {
			return m, cmd
		}
		return m, nil
	}

	if m.splitDragActive {
		if msg.Action == tea.MouseActionRelease || msg.Type == tea.MouseRelease {
			m.splitDragActive = false
			m.splitDragOffsetY = 0
			m.persistPanelHeights()
			return m, nil
		}
		if msg.Action == tea.MouseActionMotion || msg.Type == tea.MouseMotion || msg.Type == tea.MouseLeft {
			m.resizeMainSplitToY(msg.Y - m.splitDragOffsetY)
			return m, nil
		}
	}
	if m.bottomSplitDragActive {
		if msg.Action == tea.MouseActionRelease || msg.Type == tea.MouseRelease {
			m.bottomSplitDragActive = false
			m.bottomSplitDragOffsetY = 0
			m.persistPanelHeights()
			return m, nil
		}
		if msg.Action == tea.MouseActionMotion || msg.Type == tea.MouseMotion || msg.Type == tea.MouseLeft {
			m.resizeBottomSplitToY(msg.Y - m.bottomSplitDragOffsetY)
			return m, nil
		}
	}

	switch msg.Type {
	case tea.MouseLeft:
		if m.isBottomSplitterY(msg.Y) {
			bottomStart, bottomHeight := m.bottomPanelBounds()
			detailHeight := m.computeBottomTopPanelHeight(bottomHeight)
			m.bottomSplitDragOffsetY = msg.Y - (bottomStart + detailHeight - 1)
			m.bottomSplitDragActive = true
			return m, nil
		}
		if m.isMainSplitterY(msg.Y) {
			mainContentStartY, taskPanelOuterHeight, _ := m.computeTaskPanelMetrics()
			m.splitDragOffsetY = msg.Y - (mainContentStartY + taskPanelOuterHeight - 1)
			m.splitDragActive = true
			return m, nil
		}
		return m.handleMouseClick(msg)
	case tea.MouseRelease:
		wasDragging := m.splitDragActive || m.bottomSplitDragActive
		m.splitDragActive = false
		m.bottomSplitDragActive = false
		m.splitDragOffsetY = 0
		m.bottomSplitDragOffsetY = 0
		if wasDragging {
			m.persistPanelHeights()
		}
		return m, nil
	case tea.MouseWheelUp:
		return m.handleMouseWheelUp(msg)
	case tea.MouseWheelDown:
		return m.handleMouseWheelDown(msg)
	case tea.MouseRight:
		return m.handleRightClick(msg)
	}

	return m, nil
}

func (m *Model) persistPanelHeights() {
	m.settings.TaskPanelHeight = m.taskPanelHeight
	m.settings.BottomTopPanelHeight = m.bottomTopPanelHeight
	_ = SaveSettings(m.settings)
}

func (m Model) isMainSplitterY(y int) bool {
	if m.activeContentTab != ContentTabTasks || !m.hasBottomPanel() {
		return false
	}
	mainContentStartY, taskPanelOuterHeight, _ := m.computeTaskPanelMetrics()
	return absInt(y-(mainContentStartY+taskPanelOuterHeight-1)) <= 1
}

func (m Model) isBottomSplitterY(y int) bool {
	if m.activeContentTab != ContentTabTasks || !(m.detailVisible && m.logsVisible) {
		return false
	}
	bottomStart, bottomHeight := m.bottomPanelBounds()
	if bottomHeight <= 0 {
		return false
	}
	detailHeight := m.computeBottomTopPanelHeight(bottomHeight)
	return absInt(y-(bottomStart+detailHeight-1)) <= 1
}

func (m *Model) resizeMainSplitToY(y int) {
	if !m.hasBottomPanel() {
		return
	}
	mainContentStartY := m.computeMainContentStartY()
	mainHeight := m.mainContentHeight()
	m.taskPanelHeight = clampTaskPanelHeight(y-mainContentStartY+1, mainHeight)
	m.syncPanelSizes()
}

func (m *Model) resizeBottomSplitToY(y int) {
	bottomStart, bottomHeight := m.bottomPanelBounds()
	if bottomHeight <= 0 {
		return
	}
	m.bottomTopPanelHeight = clampBottomTopPanelHeight(y-bottomStart+1, bottomHeight)
	m.syncPanelSizes()
}

func (m Model) bottomPanelBounds() (start, height int) {
	mainContentStartY, taskPanelOuterHeight, _ := m.computeTaskPanelMetrics()
	start = mainContentStartY + taskPanelOuterHeight
	height = m.mainContentHeight() - taskPanelOuterHeight
	if height < 0 {
		height = 0
	}
	return start, height
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// handleMouseClick handles left mouse button clicks.
func (m Model) handleMouseClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	x, y := msg.X, msg.Y
	mainContentStartY, taskPanelOuterHeight, _ := m.computeTaskPanelMetrics()

	// Click on content tab bar (the row just above mainContentStartY)
	contentTabBarY := mainContentStartY - 1
	if y == contentTabBarY || y == contentTabBarY+1 {
		if newTab, ok := m.contentTabAtX(x); ok && newTab != m.activeContentTab {
			m.activeContentTab = newTab
			m.helpBar.ActiveContentTab = m.activeContentTab
			if m.activeContentTab == ContentTabAutomation && (!m.dreamViewer.HasContent() || !m.dreamViewer.HasConfig()) {
				m.prepareDreamFetch()
				return m, m.fetchDreamTabCmd()
			}
			if m.activeContentTab == ContentTabRunners {
				return m, fetchRunnersListCmd(m.apiRunnerConfig())
			}
		}
		return m, nil
	}

	if y >= mainContentStartY && y < mainContentStartY+taskPanelOuterHeight {
		// Click in top task panel (full-width in current layout)
		m.activePanel = PanelTasks
		m.helpBar.ActivePanel = m.activePanel
		lineInPanel := y - mainContentStartY
		return m.handleTaskPanelClick(lineInPanel, x)
	}

	// Click in bottom panel area (detail/logs) if visible
	if y >= mainContentStartY+taskPanelOuterHeight && y < m.height-1 {
		if m.detailVisible && m.logsVisible {
			bottomStart := mainContentStartY + taskPanelOuterHeight
			bottomOuterHeight := m.height - bottomStart - 1 // reserve footer line
			detailHeight := m.computeBottomTopPanelHeight(bottomOuterHeight)
			if y < bottomStart+detailHeight {
				m.activePanel = PanelDetails
			} else {
				m.activePanel = PanelLogs
			}
		} else if m.detailVisible {
			m.activePanel = PanelDetails
		} else if m.logsVisible {
			m.activePanel = PanelLogs
		}
		m.helpBar.ActivePanel = m.activePanel
	}

	return m, nil
}

// computeTaskPanelMetrics mirrors renderBaseView layout math for click hit-testing.
func (m Model) computeTaskPanelMetrics() (mainContentStartY, taskPanelOuterHeight, taskInnerHeight int) {
	// Mirror renderBaseView calculations so hit-testing stays aligned with what is drawn.
	var projectTabsView string
	if m.config.IsMultiProject() {
		projectTabsView = m.projectTabs.View(m.width)
	}
	statusBarView := m.statusBar.View(m.width)

	focusLabel := fmt.Sprintf("%d", m.activePanel)
	focusStyle := lipgloss.NewStyle().Foreground(ColorCyan)
	helpHint := DimStyle.Render("? Help")
	rightSide := fmt.Sprintf("Focus: %s", focusStyle.Render(focusLabel))
	leftPad := m.width - lipgloss.Width(helpHint) - lipgloss.Width(rightSide) - 2
	if leftPad < 1 {
		leftPad = 1
	}
	helpBarView := " " + helpHint + strings.Repeat(" ", leftPad) + rightSide

	var statusMessageView string
	if m.statusMessage != "" && time.Since(m.statusMessageTime) < 3*time.Second {
		style := lipgloss.NewStyle().Padding(0, 1)
		switch m.statusMessageType {
		case "success":
			style = style.Foreground(lipgloss.Color("10"))
		case "error":
			style = style.Foreground(lipgloss.Color("9"))
		case "info":
			style = style.Foreground(lipgloss.Color("12"))
		}
		statusMessageView = style.Render(m.statusMessage)
	}

	var filterBarView string
	switch m.filterState {
	case FilterTyping:
		matchCount := len(m.filteredTasks())
		matchWord := "matches"
		if matchCount == 1 {
			matchWord = "match"
		}
		filterBarView = FilterTypingStyle.Render(fmt.Sprintf(" / %s_ (%d %s) ", m.filterQuery, matchCount, matchWord))
	case FilterLocked:
		totalCount := len(m.tasks)
		matchCount := len(m.filteredTasks())
		filterBarView = FilterLockedStyle.Render(fmt.Sprintf(" Filter: %s (%d/%d) ", m.filterQuery, matchCount, totalCount)) + DimStyle.Render("  Esc: clear")
	}

	statusBarHeight := lipgloss.Height(statusBarView)
	helpBarHeight := lipgloss.Height(helpBarView)
	projectTabsHeight := 0
	if projectTabsView != "" {
		projectTabsHeight = lipgloss.Height(projectTabsView)
	}
	statusMessageHeight := 0
	if statusMessageView != "" {
		statusMessageHeight = lipgloss.Height(statusMessageView)
	}
	filterBarHeight := 0
	if filterBarView != "" {
		filterBarHeight = lipgloss.Height(filterBarView)
	}

	contentTabBarHeight := 1 // content tab bar always present
	fixedUIHeight := statusBarHeight + projectTabsHeight + contentTabBarHeight + helpBarHeight + statusMessageHeight + filterBarHeight
	mainHeight := m.height - fixedUIHeight
	if mainHeight < 3 {
		mainHeight = 3
	}

	topHeight := m.computeTaskPanelOuterHeight(mainHeight)

	mainContentStartY = statusBarHeight + projectTabsHeight + contentTabBarHeight
	taskPanelOuterHeight = topHeight
	taskInnerHeight = topHeight - 2
	if taskInnerHeight < 1 {
		taskInnerHeight = 1
	}
	return mainContentStartY, taskPanelOuterHeight, taskInnerHeight
}

func (m Model) mainContentHeight() int {
	_, taskPanelOuterHeight, _ := m.computeTaskPanelMetrics()
	if !m.hasBottomPanel() {
		return taskPanelOuterHeight
	}
	return taskPanelOuterHeight + (m.height - 1 - (m.computeMainContentStartY() + taskPanelOuterHeight))
}

func (m Model) computeMainContentStartY() int {
	projectTabsHeight := 0
	if m.config.IsMultiProject() {
		if projectTabsView := m.projectTabs.View(m.width); projectTabsView != "" {
			projectTabsHeight = lipgloss.Height(projectTabsView)
		}
	}
	return lipgloss.Height(m.statusBar.View(m.width)) + projectTabsHeight + 1
}

func (m Model) hasBottomPanel() bool {
	return m.activeContentTab == ContentTabTasks && (m.detailVisible || m.logsVisible)
}

func (m Model) isLogPaneY(y int) bool {
	if m.activeContentTab == ContentTabLogs {
		mainStart := m.computeMainContentStartY()
		return y >= mainStart && y < mainStart+m.mainContentHeight()
	}
	if m.activeContentTab != ContentTabTasks || !m.logsVisible {
		return false
	}
	bottomStart, bottomHeight := m.bottomPanelBounds()
	if bottomHeight <= 0 || y < bottomStart || y >= m.height-1 {
		return false
	}
	if !m.detailVisible {
		return true
	}
	detailHeight := m.computeBottomTopPanelHeight(bottomHeight)
	return y >= bottomStart+detailHeight
}

func (m Model) isDetailPaneY(y int) bool {
	if m.activeContentTab != ContentTabTasks || !m.detailVisible {
		return false
	}
	bottomStart, bottomHeight := m.bottomPanelBounds()
	if bottomHeight <= 0 || y < bottomStart || y >= m.height-1 {
		return false
	}
	if !m.logsVisible {
		return true
	}
	detailHeight := m.computeBottomTopPanelHeight(bottomHeight)
	return y < bottomStart+detailHeight
}

func (m Model) isTaskPaneY(y int) bool {
	if m.activeContentTab != ContentTabTasks {
		return false
	}
	mainContentStartY, taskPanelOuterHeight, _ := m.computeTaskPanelMetrics()
	return y >= mainContentStartY && y < mainContentStartY+taskPanelOuterHeight
}

func (m *Model) syncLogViewerSizeForActiveTab() {
	innerWidth := m.width - 4
	if innerWidth < 10 {
		innerWidth = 10
	}
	if m.activeContentTab == ContentTabLogs {
		innerHeight := m.mainContentHeight() - 2
		if innerHeight < 1 {
			innerHeight = 1
		}
		m.logViewer.SetSize(innerWidth, innerHeight)
		return
	}
	m.syncPanelSizes()
}

func (m Model) computeTaskPanelOuterHeight(mainHeight int) int {
	if !m.hasBottomPanel() {
		return mainHeight
	}
	if m.taskPanelHeight > 0 {
		return clampTaskPanelHeight(m.taskPanelHeight, mainHeight)
	}

	taskContentLines := 0
	if m.viewMode == ViewModeSchedules {
		taskContentLines = m.scheduleList.ContentHeight()
	} else {
		taskContentLines = m.taskTree.ContentHeight()
	}
	desiredTaskHeight := taskContentLines + 3
	maxTaskRatio := mainHeight * 60 / 100
	topHeight := desiredTaskHeight
	if topHeight < minTaskPanelHeight {
		topHeight = minTaskPanelHeight
	}
	if topHeight > maxTaskRatio {
		topHeight = maxTaskRatio
	}
	return clampTaskPanelHeight(topHeight, mainHeight)
}

func clampTaskPanelHeight(height, mainHeight int) int {
	if mainHeight <= minTaskPanelHeight+minBottomPanelHeight {
		if mainHeight-minBottomPanelHeight > 1 {
			return mainHeight - minBottomPanelHeight
		}
		return 1
	}
	minHeight := minTaskPanelHeight
	maxHeight := mainHeight - minBottomPanelHeight
	if height < minHeight {
		return minHeight
	}
	if height > maxHeight {
		return maxHeight
	}
	return height
}

func (m Model) computeBottomTopPanelHeight(bottomHeight int) int {
	if m.bottomTopPanelHeight > 0 {
		return clampBottomTopPanelHeight(m.bottomTopPanelHeight, bottomHeight)
	}
	return clampBottomTopPanelHeight(bottomHeight*60/100, bottomHeight)
}

func clampBottomTopPanelHeight(height, bottomHeight int) int {
	if bottomHeight <= minSubPanelHeight*2 {
		if bottomHeight-minSubPanelHeight > 1 {
			return bottomHeight - minSubPanelHeight
		}
		return 1
	}
	minHeight := minSubPanelHeight
	maxHeight := bottomHeight - minSubPanelHeight
	if height < minHeight {
		return minHeight
	}
	if height > maxHeight {
		return maxHeight
	}
	return height
}

// handleTaskPanelClick handles clicks within the task panel.
func (m Model) handleTaskPanelClick(lineInPanel, x int) (tea.Model, tea.Cmd) {
	// Convert from panel-relative line (includes top border) to task-list line
	// within taskTree view content (excludes "Tasks (N)" + blank line).
	contentLine := lineInPanel - 1
	if contentLine < 0 {
		return m, nil
	}
	lineInList := contentLine - 2
	if lineInList < 0 {
		return m, nil
	}

	if m.taskTree.useGroupedView {
		return m.handleGroupedViewClick(lineInList, x)
	}
	// Legacy tree view - simple task selection by line
	if lineInList >= 0 && lineInList < len(m.taskTree.order) {
		m.taskTree.Cursor = lineInList
		m.taskTree.SelectedID = m.taskTree.order[lineInList]
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

	// Build the same filtered sections as viewFeatureGrouped so click hit-testing
	// matches exactly what is rendered.
	var draftTasks, inactiveTasks []types.ResolvedTask
	activeFeatureGroups := make([]FeatureGroup, 0)
	var activeUngrouped *FeatureGroup

	for _, feature := range m.taskTree.featureGroups.Features {
		var activeTasks []types.ResolvedTask
		for _, task := range feature.Tasks {
			switch task.Status {
			case "draft":
				draftTasks = append(draftTasks, task)
			case "cancelled", "superseded", "archived", "completed", "validated":
				inactiveTasks = append(inactiveTasks, task)
			default:
				activeTasks = append(activeTasks, task)
			}
		}
		if len(activeTasks) > 0 {
			activeFeature := feature
			activeFeature.Tasks = activeTasks
			activeFeatureGroups = append(activeFeatureGroups, activeFeature)
		}
	}

	if m.taskTree.featureGroups.Ungrouped != nil {
		var activeUngroupedTasks []types.ResolvedTask
		for _, task := range m.taskTree.featureGroups.Ungrouped.Tasks {
			switch task.Status {
			case "draft":
				draftTasks = append(draftTasks, task)
			case "cancelled", "superseded", "archived", "completed", "validated":
				inactiveTasks = append(inactiveTasks, task)
			default:
				activeUngroupedTasks = append(activeUngroupedTasks, task)
			}
		}
		if len(activeUngroupedTasks) > 0 {
			activeUngrouped = &FeatureGroup{ID: "", Name: "[Ungrouped]", Tasks: activeUngroupedTasks}
		}
	}

	termSections := []terminalSectionLineInfo{
		{tasks: draftTasks, isOn: m.taskTree.isOnDraftSection, collapsed: m.taskTree.draftCollapsed, featureIdx: m.taskTree.draftFeatureIdx, taskIdx: m.taskTree.draftTaskIdx, featureIDs: m.taskTree.draftFeatureIDs, sectionName: "draft", featureCollapsed: m.taskTree.featureCollapsed},
		{tasks: inactiveTasks, isOn: m.taskTree.isOnCompletedSection, collapsed: m.taskTree.completedCollapsed, featureIdx: m.taskTree.completedFeatureIdx, taskIdx: m.taskTree.completedTaskIdx, featureIDs: m.taskTree.completedFeatureIDs, sectionName: "completed", featureCollapsed: m.taskTree.featureCollapsed},
	}

	// Convert viewport-visible line to absolute rendered line (before viewport slicing).
	_, _, taskInnerHeight := m.computeTaskPanelMetrics()
	listHeight := taskInnerHeight - 2 // task list viewport excludes "Tasks (N)" + blank
	if listHeight < 1 {
		listHeight = 1
	}

	clickedLine := lineInPanel
	totalLines := featureViewTotalLineCount(activeFeatureGroups, activeUngrouped, m.taskTree.tasks, termSections)
	if totalLines > listHeight {
		start := m.taskTree.viewportStart
		if start < 0 {
			start = 0
		}
		maxStart := totalLines - listHeight
		if maxStart < 0 {
			maxStart = 0
		}
		if start > maxStart {
			start = maxStart
		}
		end := start + listHeight
		if end > totalLines {
			end = totalLines
		}

		if start > 0 {
			// Top overflow indicator replaces the first visible line.
			if clickedLine == 0 {
				return m, nil
			}
		}

		windowLines := end - start
		if clickedLine < 0 || clickedLine >= windowLines {
			// Outside visible window
			return m, nil
		}
		if end < totalLines && clickedLine == windowLines-1 {
			// Bottom overflow indicator replaces the last visible line.
			return m, nil
		}

		clickedLine = start + clickedLine
	}

	// Check features
	for _, feature := range activeFeatureGroups {
		// Feature header line
		if currentLine == clickedLine {
			if x >= 0 && x <= 2 {
				// Click on collapse indicator
				// Map back to unfiltered feature index
				for i, original := range m.taskTree.featureGroups.Features {
					if original.ID == feature.ID {
						m.taskTree.selectedFeatureIdx = i
						break
					}
				}
				m.taskTree.selectedFeatureTaskIdx = -1
				m.taskTree.isOnUngrouped = false
				m.taskTree.clearTerminalSectionNav()
				m.taskTree.SelectedID = ""
				m.taskTree.ToggleCollapse()
			} else {
				// Select feature header
				for i, original := range m.taskTree.featureGroups.Features {
					if original.ID == feature.ID {
						m.taskTree.selectedFeatureIdx = i
						break
					}
				}
				m.taskTree.selectedFeatureTaskIdx = -1
				m.taskTree.isOnUngrouped = false
				m.taskTree.clearTerminalSectionNav()
				m.taskTree.SelectedID = ""
				m.syncTaskDetail()
			}
			return m, nil
		}
		currentLine++

		// Task lines (if not collapsed)
		if !feature.Collapsed {
			treeOrder := featureActiveTreeOrder(feature.Tasks)
			for tIdx, taskID := range treeOrder {
				if currentLine == clickedLine {
					if showCheckboxes && x >= 2 && x <= 4 {
						// Click on checkbox
						for i, original := range m.taskTree.featureGroups.Features {
							if original.ID == feature.ID {
								m.taskTree.selectedFeatureIdx = i
								break
							}
						}
						m.taskTree.selectedFeatureTaskIdx = tIdx
						m.taskTree.isOnUngrouped = false
						m.taskTree.clearTerminalSectionNav()
						m.taskTree.SelectedID = taskID
						m.toggleTaskSelection()
					} else {
						// Select task
						for i, original := range m.taskTree.featureGroups.Features {
							if original.ID == feature.ID {
								m.taskTree.selectedFeatureIdx = i
								break
							}
						}
						m.taskTree.selectedFeatureTaskIdx = tIdx
						m.taskTree.isOnUngrouped = false
						m.taskTree.clearTerminalSectionNav()
						m.taskTree.SelectedID = taskID
						m.syncTaskDetail()
					}
					return m, nil
				}
				currentLine++
			}
		}
	}

	// Check ungrouped
	if activeUngrouped != nil {
		ungrouped := activeUngrouped

		// Ungrouped header line
		if currentLine == clickedLine {
			if x >= 0 && x <= 2 {
				// Click on collapse indicator
				m.taskTree.selectedFeatureIdx = -1
				m.taskTree.selectedFeatureTaskIdx = -1
				m.taskTree.isOnUngrouped = true
				m.taskTree.clearTerminalSectionNav()
				m.taskTree.SelectedID = ""
				m.taskTree.ToggleCollapse()
			} else {
				// Select ungrouped header
				m.taskTree.selectedFeatureIdx = -1
				m.taskTree.selectedFeatureTaskIdx = -1
				m.taskTree.isOnUngrouped = true
				m.taskTree.clearTerminalSectionNav()
				m.taskTree.SelectedID = ""
				m.syncTaskDetail()
			}
			return m, nil
		}
		currentLine++

		// Ungrouped task lines (if not collapsed)
		if !ungrouped.Collapsed {
			treeOrder := featureActiveTreeOrder(ungrouped.Tasks)
			for tIdx, taskID := range treeOrder {
				if currentLine == clickedLine {
					if showCheckboxes && x >= 2 && x <= 4 {
						// Click on checkbox
						m.taskTree.selectedFeatureIdx = -1
						m.taskTree.selectedFeatureTaskIdx = tIdx
						m.taskTree.isOnUngrouped = true
						m.taskTree.clearTerminalSectionNav()
						m.taskTree.SelectedID = taskID
						m.toggleTaskSelection()
					} else {
						// Select task
						m.taskTree.selectedFeatureIdx = -1
						m.taskTree.selectedFeatureTaskIdx = tIdx
						m.taskTree.isOnUngrouped = true
						m.taskTree.clearTerminalSectionNav()
						m.taskTree.SelectedID = taskID
						m.syncTaskDetail()
					}
					return m, nil
				}
				currentLine++
			}
		}
	}

	// Check terminal sections (Draft, Inactive) with the same rendered order.
	for _, sec := range termSections {
		if len(sec.tasks) == 0 {
			continue
		}

		// blank line before section
		if currentLine == clickedLine {
			return m, nil
		}
		currentLine++

		// section header
		if currentLine == clickedLine {
			switch sec.sectionName {
			case "draft":
				m.taskTree.moveToDraftSection()
			case "completed":
				m.taskTree.moveToCompletedSection()
			}
			m.syncTaskDetail()
			return m, nil
		}
		currentLine++

		if sec.collapsed {
			continue
		}

		byFeature := make(map[string][]types.ResolvedTask)
		for _, task := range sec.tasks {
			fid := task.FeatureID
			if fid == "" {
				fid = "[Ungrouped]"
			}
			byFeature[fid] = append(byFeature[fid], task)
		}

		for fIdx, featureID := range sec.featureIDs {
			isSubCollapsed := false
			if sec.featureCollapsed != nil {
				isSubCollapsed = sec.featureCollapsed[sec.sectionName+":"+featureID]
			}

			if currentLine == clickedLine {
				switch sec.sectionName {
				case "draft":
					m.taskTree.moveToDraftSection()
					m.taskTree.draftFeatureIdx = fIdx
					m.taskTree.draftTaskIdx = -1
				case "completed":
					m.taskTree.moveToCompletedSection()
					m.taskTree.completedFeatureIdx = fIdx
					m.taskTree.completedTaskIdx = -1
				}
				m.syncTaskDetail()
				return m, nil
			}
			currentLine++

			if !isSubCollapsed {
				treeOrder := terminalSubFeatureTreeOrder(byFeature[featureID])
				for tIdx, taskID := range treeOrder {
					if currentLine == clickedLine {
						switch sec.sectionName {
						case "draft":
							m.taskTree.moveToDraftSection()
							m.taskTree.draftFeatureIdx = fIdx
							m.taskTree.draftTaskIdx = tIdx
						case "completed":
							m.taskTree.moveToCompletedSection()
							m.taskTree.completedFeatureIdx = fIdx
							m.taskTree.completedTaskIdx = tIdx
						}
						m.taskTree.SelectedID = taskID
						m.syncTaskDetail()
						return m, nil
					}
					currentLine++
				}
			}
		}
	}

	return m, nil
}

// handleMouseWheelUp handles scroll wheel up (scroll up / move selection up).
func (m Model) handleMouseWheelUp(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.activeContentTab == ContentTabAutomation {
		m.dreamViewer.ScrollUp(3)
		return m, nil
	}
	if m.isLogPaneY(msg.Y) {
		m.syncLogViewerSizeForActiveTab()
		m.logViewer.ScrollUp()
		return m, nil
	}
	if m.isDetailPaneY(msg.Y) {
		m.taskDetail.ScrollUp()
		return m, nil
	}
	if m.isTaskPaneY(msg.Y) || m.activePanel == PanelTasks {
		m.taskTree.MoveUp()
		m.syncTaskDetail()
	} else if m.activePanel == PanelDetails {
		m.taskDetail.ScrollUp()
	} else if m.activePanel == PanelLogs {
		m.syncLogViewerSizeForActiveTab()
		m.logViewer.ScrollUp()
	}
	return m, nil
}

// handleMouseWheelDown handles scroll wheel down (scroll down / move selection down).
func (m Model) handleMouseWheelDown(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.activeContentTab == ContentTabAutomation {
		m.dreamViewer.ScrollDown(3)
		return m, nil
	}
	if m.isLogPaneY(msg.Y) {
		m.syncLogViewerSizeForActiveTab()
		m.logViewer.ScrollDown()
		return m, nil
	}
	if m.isDetailPaneY(msg.Y) {
		m.taskDetail.ScrollDown()
		return m, nil
	}
	if m.isTaskPaneY(msg.Y) || m.activePanel == PanelTasks {
		m.taskTree.MoveDown()
		m.syncTaskDetail()
	} else if m.activePanel == PanelDetails {
		m.taskDetail.ScrollDown()
	} else if m.activePanel == PanelLogs {
		m.syncLogViewerSizeForActiveTab()
		m.logViewer.ScrollDown()
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
	// When cursor is on a feature header, show feature detail instead of "No task selected"
	if m.taskTree.IsOnGroupHeader() {
		if featureID := m.taskTree.GetSelectedFeatureID(); featureID != "" {
			// Compute features from the current task list and find the matching one
			features := service.ComputeFeatures(m.taskTree.tasks)
			var matched *service.ComputedFeature
			for _, f := range features {
				if f.ID == featureID {
					matched = f
					break
				}
			}
			m.taskDetail.SetFeature(featureID, matched)
			m.syncPanelSizes()
			m.syncHelpBarSessionState()
			return
		}
	}

	// Default: show selected task (or nil/"No task selected" for ungrouped/section headers)
	m.taskDetail.SetTask(m.taskTree.SelectedTask())
	m.syncPanelSizes()
	m.syncHelpBarSessionState()
}

// syncPanelSizes computes and sets the inner dimensions for detail/log panels.
// This must be called whenever panel visibility, window size, or selected task changes,
// so that ScrollDown/ScrollUp have accurate height and totalLines for scroll bounds.
func (m *Model) syncPanelSizes() {
	if m.width == 0 || m.height == 0 {
		return
	}

	mainHeight := m.mainContentHeight()
	topHeight := m.computeTaskPanelOuterHeight(mainHeight)
	bottomHeight := mainHeight - topHeight

	hasBottomPanel := m.hasBottomPanel()
	if !hasBottomPanel {
		return
	}

	innerWidth := m.width - 4
	if innerWidth < 10 {
		innerWidth = 10
	}

	if m.detailVisible && m.logsVisible {
		detailHeight := m.computeBottomTopPanelHeight(bottomHeight)
		logHeight := bottomHeight - detailHeight
		detailInner := detailHeight - 2
		logInner := logHeight - 2
		if detailInner < 1 {
			detailInner = 1
		}
		if logInner < 1 {
			logInner = 1
		}
		m.taskDetail.SetSize(innerWidth, detailInner)
		m.logViewer.SetSize(innerWidth, logInner)
	} else if m.detailVisible {
		detailInner := bottomHeight - 2
		if detailInner < 1 {
			detailInner = 1
		}
		m.taskDetail.SetSize(innerWidth, detailInner)
	} else if m.logsVisible {
		logInner := bottomHeight - 2
		if logInner < 1 {
			logInner = 1
		}
		m.logViewer.SetSize(innerWidth, logInner)
	}
}

// syncScheduleDetail updates the schedule detail panel with the currently selected scheduled task.
func (m *Model) syncScheduleDetail() {
	m.scheduleDetail.SetTask(m.scheduleList.SelectedTask())
}

// syncHelpBarSessionState updates the help bar's HasTaskSessions field based on current selection.
func (m *Model) syncHelpBarSessionState() {
	selectedTask := m.taskTree.SelectedTask()
	m.helpBar.HasTaskSessions = selectedTask != nil
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

// syncHelpBarSelectionState updates the help bar's HasSelectedTasks field based on multi-select state.
func (m *Model) syncHelpBarSelectionState() {
	m.helpBar.HasSelectedTasks = len(m.selectedTasks) > 0
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
	m.syncHelpBarSelectionState()
}

// clearSelection clears all selected tasks.
func (m *Model) clearSelection() {
	m.selectedTasks = make(map[string]bool)
	m.syncHelpBarSelectionState()
}

// selectAllTasks selects all visible tasks.
func (m *Model) selectAllTasks() {
	for _, task := range m.filteredTasks() {
		m.selectedTasks[task.ID] = true
	}
	m.syncHelpBarSelectionState()
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

// toggleRunnerSelection toggles selection for the focused online runner.
func (m *Model) toggleRunnerSelection() {
	runner := m.runnersPanel.SelectedRunner()
	if runner == nil || runner.Status != "online" {
		return
	}
	if m.selectedRunners == nil {
		m.selectedRunners = make(map[string]bool)
	}
	if m.selectedRunners[runner.RunnerID] {
		delete(m.selectedRunners, runner.RunnerID)
	} else {
		m.selectedRunners[runner.RunnerID] = true
	}
}

// selectAllOnlineRunners selects all currently listed online runners.
func (m *Model) selectAllOnlineRunners() {
	if m.selectedRunners == nil {
		m.selectedRunners = make(map[string]bool)
	}
	for _, runner := range m.runnersPanel.runners {
		if runner.Status == "online" {
			m.selectedRunners[runner.RunnerID] = true
		}
	}
}

// clearRunnerSelection clears all selected runners.
func (m *Model) clearRunnerSelection() {
	m.selectedRunners = make(map[string]bool)
}

// pruneRunnerSelection removes selections for runners that disappeared or are no longer online.
func (m *Model) pruneRunnerSelection() {
	if len(m.selectedRunners) == 0 {
		return
	}
	online := make(map[string]bool, len(m.runnersPanel.runners))
	for _, runner := range m.runnersPanel.runners {
		if runner.Status == "online" {
			online[runner.RunnerID] = true
		}
	}
	for runnerID := range m.selectedRunners {
		if !online[runnerID] {
			delete(m.selectedRunners, runnerID)
		}
	}
}

// runnerShutdownTargets returns selected online runners, or the focused runner when none are selected.
func (m *Model) runnerShutdownTargets() ([]string, []string) {
	ids := []string{}
	titles := []string{}
	if len(m.selectedRunners) > 0 {
		for _, runner := range m.runnersPanel.runners {
			if runner.Status == "online" && m.selectedRunners[runner.RunnerID] {
				ids = append(ids, runner.RunnerID)
				titles = append(titles, runnerDisplayTitle(runner))
			}
		}
		return ids, titles
	}

	runner := m.runnersPanel.SelectedRunner()
	if runner == nil || runner.Status != "online" {
		return nil, nil
	}
	return []string{runner.RunnerID}, []string{runnerDisplayTitle(*runner)}
}

func runnerDisplayTitle(runner types.RunnerInfo) string {
	if runner.Hostname == "" {
		return runner.RunnerID
	}
	return fmt.Sprintf("%s (%s)", runner.RunnerID, runner.Hostname)
}

// View implements tea.Model. Renders the TUI layout.
func (m Model) View() string {
	// Don't block rendering when dimensions are unset - components handle zero dimensions gracefully
	// This ensures StatusBar and other UI elements appear on first render before WindowSizeMsg arrives
	// The early "Initializing..." message was hiding the StatusBar unnecessarily

	// Render base UI
	baseView := m.renderBaseView()

	// Overlay modal if open (replaces base view since modal is full-screen overlay)
	if m.modalManager.IsOpen() {
		return m.modalManager.View(m.width, m.height)
	}

	// Safety: ensure final output never exceeds terminal height.
	// lipgloss.PlaceVertical is a noop when content is taller than the given height,
	// which causes the terminal to scroll and produces visual glitches (status bar
	// content appearing in the middle of the task tree, missing borders, etc.).
	if m.height > 0 {
		baseView = truncateToHeight(baseView, m.height)
	}

	return baseView
}

func (m Model) renderContentTabBar() string {
	activeStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("0")).
		Background(ColorCyan).
		Padding(0, 1)
	inactiveStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Padding(0, 1)
	globalInactiveStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Padding(0, 1)
	globalActiveStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("0")).
		Background(lipgloss.Color("6")).
		Padding(0, 1)
	dividerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	tab := func(ct ContentTab) string {
		isGlobal := ct == ContentTabRunners || ct == ContentTabLogs
		if m.activeContentTab == ct {
			if isGlobal {
				return globalActiveStyle.Render(ct.String())
			}
			return activeStyle.Render(ct.String())
		}
		if isGlobal {
			return globalInactiveStyle.Render(ct.String())
		}
		return inactiveStyle.Render(ct.String())
	}

	return lipgloss.JoinHorizontal(lipgloss.Center,
		" ",
		tab(ContentTabRunners),
		" ",
		tab(ContentTabLogs),
		"   ",
		dividerStyle.Render("│"),
		" ",
		tab(ContentTabTasks),
		" ",
		tab(ContentTabBrain),
		" ",
		tab(ContentTabAutomation),
	)
}

func contentTabOrder() []ContentTab {
	return []ContentTab{
		ContentTabRunners,
		ContentTabLogs,
		ContentTabTasks,
		ContentTabBrain,
		ContentTabAutomation,
	}
}

func nextContentTab(current ContentTab) ContentTab {
	order := contentTabOrder()
	for i, tab := range order {
		if tab == current {
			return order[(i+1)%len(order)]
		}
	}
	return order[0]
}

func prevContentTab(current ContentTab) ContentTab {
	order := contentTabOrder()
	for i, tab := range order {
		if tab == current {
			return order[(i+len(order)-1)%len(order)]
		}
	}
	return order[len(order)-1]
}

func (m Model) contentTabAtX(x int) (ContentTab, bool) {
	plain := " "
	for _, zone := range []struct {
		tab   ContentTab
		label string
	}{
		{ContentTabRunners, "Runners"},
		{ContentTabLogs, "Logs"},
	} {
		start := len(plain)
		plain += " " + zone.label + " "
		end := len(plain)
		if x >= start && x < end {
			return zone.tab, true
		}
		plain += " "
	}

	plain += "  │ "
	for _, zone := range []struct {
		tab   ContentTab
		label string
	}{
		{ContentTabTasks, "Tasks"},
		{ContentTabBrain, "Brain"},
		{ContentTabAutomation, "Automation"},
	} {
		start := len(plain)
		plain += " " + zone.label + " "
		end := len(plain)
		if x >= start && x < end {
			return zone.tab, true
		}
		plain += " "
	}

	return m.activeContentTab, false
}

// renderBaseView renders the main TUI layout (without modal)
func (m Model) renderBaseView() string {
	// Safety check: ensure we have valid dimensions before rendering
	if m.width < 10 || m.height < 10 {
		return "Initializing..."
	}

	// Update status bar with selection count, metrics, and pause/feature indicators
	m.statusBar.SelectedCount = len(m.selectedTasks)
	m.statusBar.Metrics = &m.resourceMetrics

	// Wire pause state to status bar
	projectID := m.activeProjectID
	if projectID == "" || projectID == "all" {
		projectID = m.config.Project
	}
	if m.activeProjectID == "all" && m.config.IsMultiProject() {
		// In "all" mode, paused only if every project is paused
		m.statusBar.IsPaused = m.allPaused
	} else {
		m.statusBar.IsPaused = m.pausedProjects[projectID]
	}

	// EnabledFeatureCount reflects user-toggled features (via x key)
	m.statusBar.EnabledFeatureCount = len(m.enabledFeatures)
	// ActiveFeatureCount reflects features with currently running tasks
	activeFeatures := make(map[string]bool)
	for _, task := range m.tasks {
		if task.FeatureID != "" && task.Status == "in_progress" {
			activeFeatures[task.FeatureID] = true
		}
	}
	m.statusBar.ActiveFeatureCount = len(activeFeatures)
	// Render ProjectTabs if multi-project mode
	var projectTabsView string
	if m.config.IsMultiProject() {
		projectTabsView = m.projectTabs.View(m.width)
	}

	// Render content tab bar grouped by scope: project-local views first, global views second.
	contentTabBarView := m.renderContentTabBar()

	// Render status bar at top
	statusBarView := m.statusBar.View(m.width)

	// Minimal bottom bar: just focus indicator + help hint
	focusLabel := fmt.Sprintf("%d", m.activePanel)
	focusStyle := lipgloss.NewStyle().Foreground(ColorCyan)
	helpHint := DimStyle.Render("? Help")
	rightSide := fmt.Sprintf("Focus: %s", focusStyle.Render(focusLabel))
	leftPad := m.width - lipgloss.Width(helpHint) - lipgloss.Width(rightSide) - 2
	if leftPad < 1 {
		leftPad = 1
	}
	helpBarView := " " + helpHint + strings.Repeat(" ", leftPad) + rightSide

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

	// Filter/Search bar (based on context)
	var filterBarView string
	if m.activeContentTab == ContentTabAutomation {
		// Automation tab currently reuses the dream search bar until subtab rendering lands.
		switch m.dreamViewer.SearchMode() {
		case DreamSearchTyping:
			matchCount := m.dreamViewer.MatchCount()
			matchWord := "matches"
			if matchCount == 1 {
				matchWord = "match"
			}
			filterBarView = FilterTypingStyle.Render(fmt.Sprintf(" / %s_ (%d %s) ", m.dreamViewer.SearchQuery(), matchCount, matchWord))
		case DreamSearchLocked:
			matchCount := m.dreamViewer.MatchCount()
			currentIdx := m.dreamViewer.CurrentMatchIndex()
			if matchCount > 0 {
				filterBarView = FilterLockedStyle.Render(fmt.Sprintf(" Search: %s (%d/%d) ", m.dreamViewer.SearchQuery(), currentIdx, matchCount)) +
					DimStyle.Render("  n/N: next/prev  Esc: clear")
			} else {
				filterBarView = FilterLockedStyle.Render(fmt.Sprintf(" Search: %s (no matches) ", m.dreamViewer.SearchQuery())) +
					DimStyle.Render("  Esc: clear")
			}
		}
	} else {
		// Tasks tab: show task filter bar
		switch m.filterState {
		case FilterTyping:
			matchCount := len(m.filteredTasks())
			matchWord := "matches"
			if matchCount == 1 {
				matchWord = "match"
			}
			filterBarView = FilterTypingStyle.Render(fmt.Sprintf(" / %s_ (%d %s) ", m.filterQuery, matchCount, matchWord))
		case FilterLocked:
			totalCount := len(m.tasks)
			matchCount := len(m.filteredTasks())
			filterBarView = FilterLockedStyle.Render(fmt.Sprintf(" Filter: %s (%d/%d) ", m.filterQuery, matchCount, totalCount)) +
				DimStyle.Render("  Esc: clear")
		}
	}

	// Calculate available height for main content by measuring actual rendered heights
	// This avoids hardcoded estimates that can drift from reality
	statusBarHeight := lipgloss.Height(statusBarView)
	helpBarHeight := lipgloss.Height(helpBarView)

	// lipgloss.Height("") returns 1, not 0, so use 0 for empty strings
	projectTabsHeight := 0
	if projectTabsView != "" {
		projectTabsHeight = lipgloss.Height(projectTabsView)
	}
	statusMessageHeight := 0
	if statusMessageView != "" {
		statusMessageHeight = lipgloss.Height(statusMessageView)
	}
	filterBarHeight := 0
	if filterBarView != "" {
		filterBarHeight = lipgloss.Height(filterBarView)
	}

	contentTabBarHeight := 0
	if contentTabBarView != "" {
		contentTabBarHeight = lipgloss.Height(contentTabBarView)
	}

	// Total height consumed by fixed UI elements (header at top, footer at bottom)
	fixedUIHeight := statusBarHeight + projectTabsHeight + contentTabBarHeight + helpBarHeight + statusMessageHeight + filterBarHeight

	// Available height for main content area (tasks + detail/logs panels)
	mainHeight := m.height - fixedUIHeight
	if mainHeight < 3 {
		mainHeight = 3
	}

	// Determine if bottom panels are visible.
	hasBottomPanel := m.hasBottomPanel()

	// Calculate heights - ensure total equals mainHeight exactly
	topHeight := m.computeTaskPanelOuterHeight(mainHeight)
	bottomHeight := 0
	if hasBottomPanel {
		bottomHeight = mainHeight - topHeight
	}

	// Top panel: task tree
	taskPanelStyle := InactiveBorder
	if m.activePanel == PanelTasks {
		taskPanelStyle = ActiveBorder
	}

	// Note: lipgloss Height() sets the INNER content height; border adds 2 more lines.
	// So we subtract 2 from the allocated height to get the correct inner height,
	// ensuring the total rendered height matches our allocation.
	taskInnerHeight := topHeight - 2 // subtract border (top + bottom)
	innerWidth := m.width - 4        // account for border + padding, use full width
	if innerWidth < 10 {
		innerWidth = 10
	}
	if taskInnerHeight < 1 {
		taskInnerHeight = 1
	}

	var taskContent string
	if m.viewMode == ViewModeSchedules {
		taskContent = m.scheduleList.View(innerWidth, taskInnerHeight)
	} else {
		taskContent = m.taskTree.ViewWithSelection(innerWidth, taskInnerHeight, m.selectedTasks, m.activeProjectID)
	}
	// Truncate task content to fit within allocated height (minus border)
	taskContent = truncateToHeight(taskContent, taskInnerHeight)

	taskPanel := taskPanelStyle.
		Width(m.width - 2).
		Height(taskInnerHeight).
		MaxHeight(topHeight).
		Render(taskContent)

	// Build main content based on active content tab
	var mainContent string
	if m.activeContentTab == ContentTabRunners {
		// Runners tab: full-width runners panel, no task/detail/log panels
		runnersView := m.runnersPanel.ViewWithSelection(m.width-4, mainHeight-2, m.selectedRunners)
		runnersPanel := InactiveBorder.
			Width(m.width - 2).
			Height(mainHeight - 2).
			Render(runnersView)
		mainContent = runnersPanel
	} else if m.activeContentTab == ContentTabLogs {
		// Logs tab: global full-height log stream.
		mainContent = m.renderLogPanel(m.width, mainHeight)
	} else if m.activeContentTab == ContentTabAutomation {
		// Automation tab currently reuses the dream viewer until subtab rendering lands.
		dreamView := m.dreamViewer.View(m.width-4, mainHeight-2)
		dreamPanel := InactiveBorder.
			Width(m.width - 2).
			Height(mainHeight - 2).
			Render(dreamView)
		mainContent = dreamPanel
	} else if hasBottomPanel {
		bottomPanel := m.renderBottomPanel(m.width, bottomHeight)
		mainContent = lipgloss.JoinVertical(lipgloss.Left, taskPanel, bottomPanel)
	} else {
		mainContent = taskPanel
	}

	// Build the main content (everything above the footer)
	var sections []string
	sections = append(sections, statusBarView)
	if projectTabsView != "" {
		sections = append(sections, projectTabsView)
	}
	sections = append(sections, contentTabBarView)
	sections = append(sections, mainContent)
	if statusMessageView != "" {
		sections = append(sections, statusMessageView)
	}
	if filterBarView != "" {
		sections = append(sections, filterBarView)
	}

	// Join all content sections + footer
	sections = append(sections, helpBarView)
	output := lipgloss.JoinVertical(lipgloss.Left, sections...)

	// Use lipgloss.Place to ensure output fills the full terminal.
	// PlaceVertical positions content at top, fills remaining with spaces.
	return lipgloss.PlaceVertical(m.height, lipgloss.Top, output)
}

// renderBottomPanel renders the bottom panel(s) - detail and/or logs.
// height is the total outer height for the bottom section.
func (m Model) renderBottomPanel(width, height int) string {
	if m.detailVisible && m.logsVisible {
		// Stack vertically: detail on top, logs on bottom.
		detailHeight := m.computeBottomTopPanelHeight(height)
		logHeight := height - detailHeight
		detailPanel := m.renderDetailPanel(width, detailHeight)
		logPanel := m.renderLogPanel(width, logHeight)
		return lipgloss.JoinVertical(lipgloss.Left, detailPanel, logPanel)
	}

	if m.detailVisible {
		return m.renderDetailPanel(width, height)
	}

	return m.renderLogPanel(width, height)
}

// renderDetailPanel renders the task detail panel with border.
// height is the total outer height including borders.
func (m Model) renderDetailPanel(width, height int) string {
	style := InactiveBorder
	if m.activePanel == PanelDetails {
		style = ActiveBorder
	}

	// lipgloss Height() sets inner content height; border adds 2 lines
	innerWidth := width - 4
	innerHeight := height - 2
	if innerWidth < 10 {
		innerWidth = 10
	}
	if innerHeight < 1 {
		innerHeight = 1
	}

	// Temporarily set size for rendering
	var content string
	if m.viewMode == ViewModeSchedules {
		schedDetail := m.scheduleDetail
		schedDetail.SetSize(innerWidth, innerHeight)
		content = schedDetail.View()
	} else {
		detail := m.taskDetail
		detail.SetSize(innerWidth, innerHeight)
		content = detail.View()
	}

	// Truncate content to fit within allocated height
	content = truncateToHeight(content, innerHeight)

	return style.
		Width(width - 2).
		Height(innerHeight).
		MaxHeight(height).
		Render(content)
}

// renderLogPanel renders the log viewer panel with border.
// height is the total outer height including borders.
func (m Model) renderLogPanel(width, height int) string {
	style := InactiveBorder
	if m.activePanel == PanelLogs {
		style = ActiveBorder
	}

	// lipgloss Height() sets inner content height; border adds 2 lines
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

	// Wire log filtering by selected task
	if m.filterLogsByTask {
		selectedTask := m.taskTree.SelectedTask()
		if selectedTask != nil {
			lv.IsFiltering = true
			lv.FilterTaskID = selectedTask.ID
		} else {
			lv.IsFiltering = false
			lv.FilterTaskID = ""
		}
	} else {
		lv.IsFiltering = false
		lv.FilterTaskID = ""
	}

	content := lv.View()

	// Truncate content to fit within allocated height
	content = truncateToHeight(content, innerHeight)

	return style.
		Width(width - 2).
		Height(innerHeight).
		MaxHeight(height).
		Render(content)
}

// =============================================================================
// Filter Methods
// =============================================================================

// handleDreamSearchInput processes keyboard input when searching in the Automation tab.
func (m Model) handleDreamSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		// Lock search for n/N navigation
		m.dreamViewer.LockSearch()
		return m, nil

	case tea.KeyEsc:
		// Cancel search
		m.dreamViewer.CancelSearch()
		return m, nil

	case tea.KeyBackspace, tea.KeyDelete:
		q := m.dreamViewer.SearchQuery()
		if len(q) > 0 {
			m.dreamViewer.SetSearchQuery(q[:len(q)-1])
		}
		return m, nil

	case tea.KeyCtrlU:
		// Clear search input
		m.dreamViewer.SetSearchQuery("")
		return m, nil

	case tea.KeyRunes:
		q := m.dreamViewer.SearchQuery() + string(msg.Runes)
		m.dreamViewer.SetSearchQuery(q)
		return m, nil
	}

	return m, nil
}

// handleFilterInput processes keyboard input in FilterTyping mode.
// All runes are captured as filter text — no key leaking to other handlers.
func (m Model) handleFilterInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		// Lock filter if query is non-empty, otherwise go back to off
		if m.filterQuery != "" {
			m.filterState = FilterLocked
		} else {
			m.filterState = FilterOff
		}
		m.applyFilter()
		return m, nil

	case tea.KeyEsc:
		// Always cancel: clear query and go to off
		m.filterState = FilterOff
		m.filterQuery = ""
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
		// ALL runes go to filter query — no multi-project tab navigation leak
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
	} else {
		// Filter tasks
		filtered := FilterTasks(m.tasks, m.filterQuery)
		m.taskTree.SetTasks(filtered)
	}
	// Sync task detail after filter changes
	m.syncTaskDetail()
}

// clearFilter removes the active filter and restores all tasks.
func (m *Model) clearFilter() {
	m.filterQuery = ""
	m.filterState = FilterOff
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
	if m.filterState == FilterLocked || m.filterState == FilterTyping {
		// If filter is active (locked or typing), apply it
		m.applyFilter()
	} else {
		// No filter, just set tasks directly
		m.taskTree.SetTasks(m.tasks)
	}
}

func (m *Model) activeDreamProject() string {
	project := m.config.Project
	if m.activeProjectID != "" && m.activeProjectID != "all" {
		project = m.activeProjectID
	}
	return project
}

func (m *Model) prepareDreamFetch() {
	m.dreamViewer.SetLoading(true)
	m.dreamViewer.SetDreamConfigLoading(true)
}

func (m *Model) fetchDreamTabCmd() tea.Cmd {
	project := m.activeDreamProject()
	return tea.Batch(
		fetchDreamContentCmd(m.apiRunnerConfig(), project),
		fetchDreamConfigCmd(m.monitorClient, project),
	)
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

// openSessionForTask opens a session for the given task, using the task's
// sessions data directly (no API fetch needed). If the task has no sessions,
// falls back to fetching from the API.
func (m *Model) openSessionForTask(task *types.ResolvedTask, tmuxMode bool) tea.Cmd {
	// Use sessions from the already-loaded task data
	sessionIDs := sortedSessionIDs(task.Sessions)

	if len(sessionIDs) == 0 {
		// Fallback: try fetching from API in case SSE data is stale
		apiClient := runner.NewAPIClient(m.apiRunnerConfig())
		return fetchSessionsCmd(apiClient, task.Path, tmuxMode)
	}

	// Return a sessionsFetchedMsg directly (reuse existing handler)
	return func() tea.Msg {
		return sessionsFetchedMsg{
			sessionIDs: sessionIDs,
			taskPath:   task.Path,
			tmuxMode:   tmuxMode,
		}
	}
}

// addLog adds a log entry to the log viewer for the Logs panel.
func (m *Model) addLog(level, message string) {
	m.logViewer.AddEntry(LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
	})
}

// addTaskLog adds a log entry with task context.
func (m *Model) addTaskLog(level, message, taskID string) {
	m.logViewer.AddEntry(LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
		TaskID:    taskID,
	})
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

type featureExecutedMsg struct {
	featureID string
	started   int
	err       error
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

type batchRunnersShutdownMsg struct {
	successCount int
	failedCount  int
	errors       []error
}

type editorClosedMsg struct {
	taskID          string
	taskPath        string
	tempFile        string
	originalContent string
	err             error
}

type autoMonitorCreatedMsg struct {
	featureID  string
	templateID string
	err        error
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

// fetchDreamContentCmd fetches dream content for a project from the API.
func fetchDreamContentCmd(cfg runner.RunnerConfig, project string) tea.Cmd {
	return func() tea.Msg {
		apiClient := runner.NewAPIClient(cfg)
		ctx := context.Background()

		// Search for dream entries in this project
		limit := 1
		searchResp, err := apiClient.SearchEntries(ctx, types.SearchRequest{
			Query:   "Project Dream",
			Type:    "dream",
			Project: project,
			Limit:   &limit,
		})
		if err != nil {
			return DreamContentMsg{Error: fmt.Errorf("search failed: %w", err)}
		}

		if len(searchResp.Results) == 0 {
			return DreamContentMsg{Content: ""}
		}

		// Fetch the full entry content
		entry, err := apiClient.GetEntry(ctx, searchResp.Results[0].Path)
		if err != nil {
			return DreamContentMsg{Error: fmt.Errorf("fetch failed: %w", err)}
		}

		return DreamContentMsg{Content: entry.Content}
	}
}

// fetchDreamConfigCmd fetches Dream monitor configuration for a project from the API.
func fetchDreamConfigCmd(client *MonitorClient, project string) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return DreamConfigMsg{Error: fmt.Errorf("monitor client unavailable")}
		}
		config, err := client.FetchDreamConfig(context.Background(), project)
		if err != nil {
			return DreamConfigMsg{Error: err}
		}
		return DreamConfigMsg{Config: config}
	}
}

// completeTaskCmd completes a single task.
func completeTaskCmd(cfg runner.RunnerConfig, taskPath string) tea.Cmd {
	return func() tea.Msg {
		apiClient := runner.NewAPIClient(cfg)

		ctx := context.Background()
		err := apiClient.UpdateTaskStatus(ctx, taskPath, "completed")
		return taskCompletedMsg{taskID: taskPath, err: err}
	}
}

// cancelTaskCmd cancels a single task.
func cancelTaskCmd(cfg runner.RunnerConfig, taskPath string) tea.Cmd {
	return func() tea.Msg {
		apiClient := runner.NewAPIClient(cfg)

		ctx := context.Background()
		err := apiClient.UpdateTaskStatus(ctx, taskPath, "cancelled")
		return taskCancelledMsg{taskID: taskPath, err: err}
	}
}

// batchCompleteTasksCmd completes multiple tasks in parallel.
func batchCompleteTasksCmd(cfg runner.RunnerConfig, taskPaths, taskIDs []string) tea.Cmd {
	return func() tea.Msg {
		apiClient := runner.NewAPIClient(cfg)

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
func batchCancelTasksCmd(cfg runner.RunnerConfig, taskPaths, taskIDs []string) tea.Cmd {
	return func() tea.Msg {
		apiClient := runner.NewAPIClient(cfg)

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

// executeTaskViaRunnerCmd executes a task through the embedded runner controller.
// This performs the full pipeline: claim → status update → workdir resolve → spawn.
func executeTaskViaRunnerCmd(rc RunnerController, task *types.ResolvedTask, projectID string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		err := rc.ExecuteTask(ctx, task, projectID)
		return taskExecutedMsg{taskID: task.ID, err: err}
	}
}

// executeTaskCmd claims a task for immediate execution (API-only fallback).
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

// batchShutdownRunnersCmd requests graceful shutdown for multiple runners in parallel.
func batchShutdownRunnersCmd(cfg runner.RunnerConfig, runnerIDs []string) tea.Cmd {
	return func() tea.Msg {
		apiClient := runner.NewAPIClient(cfg)

		type result struct {
			runnerID string
			err      error
		}

		results := make(chan result, len(runnerIDs))
		ctx := context.Background()

		for _, runnerID := range runnerIDs {
			go func(id string) {
				err := apiClient.ShutdownRunner(ctx, id, "operator requested shutdown from TUI")
				results <- result{runnerID: id, err: err}
			}(runnerID)
		}

		var errors []error
		successCount := 0
		failedCount := 0
		for range runnerIDs {
			res := <-results
			if res.err != nil {
				errors = append(errors, fmt.Errorf("%s: %w", res.runnerID, res.err))
				failedCount++
			} else {
				successCount++
			}
		}

		return batchRunnersShutdownMsg{
			successCount: successCount,
			failedCount:  failedCount,
			errors:       errors,
		}
	}
}

// pauseProjectCmd toggles pause/resume for a specific project.
func pauseProjectCmd(cfg runner.RunnerConfig, projectID string, currentlyPaused bool, rc RunnerController) tea.Cmd {
	return func() tea.Msg {
		// If we have a direct runner reference, use it (in-process, no HTTP)
		if rc != nil {
			if currentlyPaused {
				rc.ResumeProject(projectID)
			} else {
				rc.PauseProject(projectID)
			}
			return pauseToggledMsg{projectID: projectID, paused: !currentlyPaused, err: nil}
		}

		// Fallback to HTTP API
		apiClient := runner.NewAPIClient(cfg)
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
func pauseAllCmd(cfg runner.RunnerConfig, currentlyPaused bool, rc RunnerController) tea.Cmd {
	return func() tea.Msg {
		// If we have a direct runner reference, use it (in-process, no HTTP)
		if rc != nil {
			if currentlyPaused {
				rc.ResumeAll()
			} else {
				rc.PauseAll()
			}
			return pauseAllToggledMsg{paused: !currentlyPaused, err: nil}
		}

		// Fallback to HTTP API
		apiClient := runner.NewAPIClient(cfg)
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
func fetchRunnerStatusCmd(cfg runner.RunnerConfig) tea.Cmd {
	return func() tea.Msg {
		apiClient := runner.NewAPIClient(cfg)

		ctx := context.Background()
		status, err := apiClient.GetRunnerStatus(ctx)
		if err != nil {
			return runnerStatusMsg{err: err}
		}
		return runnerStatusMsg{paused: status.Paused, pausedProjects: status.PausedProjects}
	}
}

// processAutoMonitors detects new feature_ids in task updates and returns
// tea.Cmds to create monitors for each new feature. On the first load
// (initialSnapshotDone == false), it only snapshots existing feature_ids
// without creating monitors. seenFeatureIDs is always updated, even when
// AutoMonitors is disabled, to prevent a burst of creation on re-enable.
func (m *Model) processAutoMonitors(msg TasksUpdatedMsg) []tea.Cmd {
	var newFeatures []struct {
		featureID string
		projectID string
	}

	for _, task := range msg.Tasks {
		if task.FeatureID == "" {
			continue
		}
		if !m.seenFeatureIDs[task.FeatureID] {
			m.seenFeatureIDs[task.FeatureID] = true
			if m.initialSnapshotDone {
				newFeatures = append(newFeatures, struct {
					featureID string
					projectID string
				}{featureID: task.FeatureID, projectID: task.ProjectID})
			}
		}
	}

	// Mark initial snapshot as done after first load
	if !m.initialSnapshotDone {
		m.initialSnapshotDone = true
	}

	// Only create monitors if auto-monitors is enabled and there are new features
	if !m.settings.AutoMonitors || len(newFeatures) == 0 {
		return nil
	}

	var cmds []tea.Cmd
	for _, f := range newFeatures {
		cmds = append(cmds, autoCreateMonitorsCmd(m.monitorClient, f.featureID, f.projectID))
	}
	return cmds
}

// autoCreateMonitorsCmd returns a tea.Cmd that creates all monitor templates
// for a feature. Templates are fetched from the API, so adding a new template
// to the server registry automatically includes it in auto-creation.
// Errors are returned in the message (not panicked) for silent handling.
func autoCreateMonitorsCmd(client *MonitorClient, featureID, projectID string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		// Fetch available templates from API
		templates, err := client.FetchTemplates(ctx)
		if err != nil {
			return autoMonitorCreatedMsg{
				featureID:  featureID,
				templateID: "all",
				err:        fmt.Errorf("fetch templates: %w", err),
			}
		}

		// Create each template via the monitors API
		var created []string
		for _, tmpl := range templates {
			err := client.CreateMonitorTask(ctx, tmpl.ID, featureID, projectID)
			if err != nil {
				// Log but continue — don't fail all because one failed
				continue
			}
			created = append(created, tmpl.ID)
		}

		templateIDs := strings.Join(created, "+")
		return autoMonitorCreatedMsg{
			featureID:  featureID,
			templateID: templateIDs,
			err:        nil,
		}
	}
}

// computeTaskCountsByStatus computes task counts per display status group name.
// Maps the current tasks' statuses to display group names used by StatusGroups.
func (m Model) computeTaskCountsByStatus() map[string]int {
	counts := make(map[string]int)
	// Build reverse mapping: API status -> display group name
	apiToDisplay := make(map[string]string)
	for display, apiStatuses := range statusGroupToAPIStatuses {
		for _, api := range apiStatuses {
			apiToDisplay[api] = display
		}
	}

	for _, task := range m.tasks {
		if displayName, ok := apiToDisplay[task.Status]; ok {
			counts[displayName]++
		}
	}
	return counts
}

// computeRunningPerProject computes the number of in_progress tasks per project.
// Uses tasksByProject in multi-project mode, or tasks in single-project mode.
func (m Model) computeRunningPerProject() map[string]int {
	running := make(map[string]int)
	if len(m.tasksByProject) > 0 {
		// Multi-project mode: iterate per-project tasks
		for projectID, tasks := range m.tasksByProject {
			for _, task := range tasks {
				if task.Status == "in_progress" {
					running[projectID]++
				}
			}
		}
	} else {
		// Single-project mode: use projectTabs stats if available
		for _, proj := range m.projectTabs.Projects {
			if stats, ok := m.projectTabs.StatsByProject[proj]; ok {
				running[proj] = stats.InProgress
			}
		}
		// Fallback: count from tasks directly
		if len(running) == 0 {
			for _, task := range m.tasks {
				if task.Status == "in_progress" {
					running[task.ProjectID]++
				}
			}
		}
	}
	return running
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

// fetchRunnersListCmd returns a tea.Cmd that fetches the runner list from the API.
func fetchRunnersListCmd(cfg runner.RunnerConfig) tea.Cmd {
	return func() tea.Msg {
		apiClient := runner.NewAPIClient(cfg)
		ctx := context.Background()
		resp, err := apiClient.ListRunners(ctx)
		if err != nil {
			// Silently ignore errors — runners panel will just show stale data
			return nil
		}
		return RunnersUpdatedMsg{Runners: resp.Runners}
	}
}

package tui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"text/template"
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
	selectedTasks map[string]bool

	// Pause/resume state
	pausedProjects    map[string]bool
	allPaused         bool
	automationsPaused bool
	runnerController  RunnerController // direct reference to embedded runner (nil if standalone)

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

	// Content tab state (global Runners/Logs, project Tasks/Brain/Automation)
	activeContentTab         ContentTab
	activeAutomationSubTab   AutomationSubTab
	automationList           AutomationList
	automationGeneratedTasks []types.BrainEntry
	// goalAuditByEntry caches the reconcile audit history for goal automation
	// rows, keyed by the automation entry ID (AutomationListRow.ID). Populated
	// asynchronously when a goal detail pane syncs (goalDetailAuditMsg).
	goalAuditByEntry map[string][]types.GoalReconcileAudit
	// goalDetailRaw caches the raw (un-decorated) entry content for goal detail
	// rows keyed by entry path, so the detail pane can be re-rendered with the
	// reconcile section once the audit history loads asynchronously.
	goalDetailRaw            map[string]string
	dreamViewer              DreamViewer
	entryTree                EntryTree
	brainEntries             []types.BrainEntry
	brainSearchState         FilterMode
	brainSearchQuery         string
	brainSearchLabel         string
	lastAttachmentClickIndex int
	lastAttachmentClickTime  time.Time

	// Runner panel state
	runnerPanel        RunnerPanel
	runnerPanelVisible bool

	// User-resizable vertical split between the task pane and visible bottom pane.
	taskPanelHeight        int
	bottomTopPanelHeight   int
	splitDragActive        bool
	bottomSplitDragActive  bool
	splitDragOffsetY       int
	bottomSplitDragOffsetY int
}

func visibleContentTabs() []ContentTab {
	return []ContentTab{ContentTabRunners, ContentTabLogs, ContentTabTasks, ContentTabBrain, ContentTabAutomation}
}

func nextContentTab(current ContentTab) ContentTab {
	tabs := visibleContentTabs()
	for i, tab := range tabs {
		if tab == current {
			return tabs[(i+1)%len(tabs)]
		}
	}
	return ContentTabTasks
}

func prevContentTab(current ContentTab) ContentTab {
	tabs := visibleContentTabs()
	for i, tab := range tabs {
		if tab == current {
			return tabs[(i+len(tabs)-1)%len(tabs)]
		}
	}
	return ContentTabTasks
}

func (m Model) renderContentTabBar() string {
	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(ColorCyan).Padding(0, 1)
	inactiveStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Padding(0, 1)
	globalActiveStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("6")).Padding(0, 1)
	globalInactiveStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Padding(0, 1)
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
		{ContentTabAutomation, "Automations"},
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

func (m Model) automationSubTabAtX(x int) (AutomationSubTab, bool) {
	plain := " "
	for _, zone := range []struct {
		tab   AutomationSubTab
		label string
	}{
		{AutomationSubTabAutomations, "Automations"},
		{AutomationSubTabDream, "Dream"},
	} {
		start := len(plain)
		plain += " " + zone.label + " "
		end := len(plain)
		if x >= start && x < end {
			return zone.tab, true
		}
		plain += " "
	}
	return m.activeAutomationSubTab, false
}

func contentTabCenterX(tab ContentTab) (int, bool) {
	m := Model{}
	for x := 0; x < 80; x++ {
		if got, ok := m.contentTabAtX(x); ok && got == tab {
			return x, true
		}
	}
	return 0, false
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
		pausedProjects:         make(map[string]bool),
		runnerController:       cfg.Runner,
		tasksByProject:         make(map[string][]types.ResolvedTask),
		sseClients:             make(map[string]*SSEClient),
		metricsCollector:       NewMetricsCollector(),
		seenFeatureIDs:         make(map[string]bool),
		monitorClient:          NewMonitorClient(cfg.APIURL, cfg.APIToken),
		enabledFeatures:        make(map[string]bool),
		activeAutomationSubTab: AutomationSubTabAutomations,
		automationList:         NewAutomationList(),
		goalAuditByEntry:       make(map[string][]types.GoalReconcileAudit),
		goalDetailRaw:          make(map[string]string),
		dreamViewer:            NewDreamViewer(),
		entryTree:              NewEntryTree(),
		runnerPanel:            NewRunnerPanel(),
		taskPanelHeight:        settings.TaskPanelHeight,
		bottomTopPanelHeight:   settings.BottomTopPanelHeight,
	}

	// Wire TextWrap setting to sub-models
	m.taskTree.TextWrap = settings.TextWrap
	m.helpBar.TextWrap = settings.TextWrap
	m.helpBar.ActiveAutomationSubTab = m.activeAutomationSubTab

	// Propagate max parallel setting to the runner on startup
	if cfg.Runner != nil && settings.GlobalMaxParallel > 0 {
		cfg.Runner.SetMaxParallel(settings.GlobalMaxParallel)
	}

	// Propagate default model setting to the runner on startup
	if cfg.Runner != nil && settings.DefaultModel != "" {
		cfg.Runner.SetDefaultModel(settings.DefaultModel)
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
			// Respect XDG_STATE_HOME for log directory fallback
			stateHome := os.Getenv("XDG_STATE_HOME")
			if stateHome == "" {
				homeDir, err := os.UserHomeDir()
				if err == nil {
					stateHome = filepath.Join(homeDir, ".local", "state")
				}
			}
			if stateHome != "" {
				logPath = filepath.Join(stateHome, "brain-runner", cfg.Project, "tui-logs.jsonl")
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
			fetchAPIHealthCmd(m.apiRunnerConfig()),
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
		fetchAPIHealthCmd(m.apiRunnerConfig()),
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
			m.tasksByProject[msg.ProjectID] = visibleTaskRows(msg.Tasks)
		}
		// In single-project mode, always update m.tasks directly
		// (SSE events include ProjectID even for single-project connections,
		// but syncActiveProjectView is a no-op in single-project mode)
		if !m.config.IsMultiProject() {
			m.tasks = visibleTaskRows(msg.Tasks)
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
				m.stats = displayStatsForTasks(msg.Tasks, tuiStats)
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

		// Batch SSE continuation with any auto-monitor commands and automation refreshes.
		additionalCmds := autoMonitorCmds
		if m.activeContentTab == ContentTabAutomation && m.activeAutomationSubTab == AutomationSubTabAutomations {
			additionalCmds = append(additionalCmds, m.fetchAutomationListCmd())
		}
		if len(additionalCmds) > 0 {
			allCmds := append([]tea.Cmd{nextCmd}, additionalCmds...)
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
		cmds := []tea.Cmd{tickCmd(), fetchRunnerStatusCmd(m.apiRunnerConfig()), fetchAPIHealthCmd(m.apiRunnerConfig())}
		// Always fetch runners for status bar metrics (data goes to both panel and status bar)
		cmds = append(cmds, fetchRunnerListCmd(m.apiRunnerConfig()))
		return m, tea.Batch(cmds...)

	case RunnerListMsg:
		if msg.Err == nil {
			m.runnerPanel.SetRunners(msg.Runners)
			// Compute runner metrics for status bar
			m.statusBar.RunnerMetrics = computeRunnerMetrics(msg.Runners)
		}
		return m, nil

	case apiHealthMsg:
		if msg.err != nil || msg.health.Status == "unhealthy" {
			m.statusBar.EmbeddingReady = false
			return m, nil
		}
		m.statusBar.EmbeddingReady = msg.health.Embedding.Enabled && msg.health.Embedding.Status == "ready"
		return m, nil

	case BrainEntriesMsg:
		if msg.Err != nil {
			m.setStatusMessage("error", fmt.Sprintf("Failed to fetch brain entries: %v", msg.Err))
			return m, nil
		}
		m.brainEntries = append([]types.BrainEntry(nil), msg.Entries...)
		m.entryTree.SetEntries(msg.Entries)
		m.clearBrainSearch()
		if m.activeContentTab == ContentTabBrain && m.detailVisible {
			return m, m.syncBrainEntryDetail()
		}
		return m, nil

	case BrainSearchMsg:
		if msg.Err != nil {
			m.setStatusMessage("error", fmt.Sprintf("Brain search failed: %v", msg.Err))
			return m, nil
		}
		m.brainSearchState = FilterLocked
		m.brainSearchQuery = msg.Query
		m.brainSearchLabel = msg.Strategy
		m.entryTree.SetSearchResults(msg.Entries)
		if m.activeContentTab == ContentTabBrain && m.detailVisible {
			return m, m.syncBrainEntryDetail()
		}
		return m, nil

	case BrainEmbeddingBackfillMsg:
		if msg.Err != nil {
			m.setStatusMessage("error", fmt.Sprintf("Embedding backfill failed: %v", msg.Err))
			return m, nil
		}
		scope := msg.Project
		if msg.All || scope == "" {
			scope = "all entries"
		}
		m.setStatusMessage("success", fmt.Sprintf("Embedded %s: %d processed, %d skipped, %d failed", scope, msg.Result.Processed, msg.Result.Skipped, msg.Result.Failed))
		return m, fetchBrainEntriesCmd(m.apiRunnerConfig(), m.currentProjectID())

	case runnerShutdownRequestedMsg:
		if msg.err != nil {
			m.setStatusMessage("error", fmt.Sprintf("Failed to shutdown runner %s: %v", msg.runnerID, msg.err))
			m.addLog("error", fmt.Sprintf("Runner shutdown failed: %s: %v", msg.runnerID, msg.err))
			return m, nil
		}
		m.setStatusMessage("success", fmt.Sprintf("Shutdown requested for runner %s", msg.runnerID))
		m.addLog("info", fmt.Sprintf("Runner shutdown requested: %s", msg.runnerID))
		return m, fetchRunnerListCmd(m.apiRunnerConfig())

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

	case featureAssignmentResultMsg:
		m.modalManager.Close()
		if msg.err != nil {
			m.setStatusMessage("error", fmt.Sprintf("Failed to %s feature %s: %v", msg.action, msg.featureID, msg.err))
			m.addLog("error", fmt.Sprintf("Feature assignment %s failed for runner %s feature %s: %v", msg.action, msg.runnerID, msg.featureID, msg.err))
		} else {
			if msg.action == "clear" {
				m.setStatusMessage("success", fmt.Sprintf("Cleared assignment for %s", msg.featureID))
				m.addLog("info", fmt.Sprintf("Feature %s assignment cleared", msg.featureID))
			} else {
				m.setStatusMessage("success", fmt.Sprintf("Assigned %s to runner %s", msg.featureID, msg.runnerID))
				m.addLog("info", fmt.Sprintf("Feature %s assigned to runner %s via %s", msg.featureID, msg.runnerID, msg.action))
			}
			// Refresh runner list to show updated assignments.
			return m, fetchRunnerListCmd(m.apiRunnerConfig())
		}
		return m, nil

	case LogEntryMsg:
		m.logViewer.AddEntry(msg.Entry)
		return m, nil

	case RunnerLogMsg:
		// Convert runner_log SSE event lines to LogEntry and add to viewer.
		// This enables monitor-only mode to display logs from remote runners.
		// In hybrid mode, these remote logs are merged chronologically with local logs.
		for _, line := range msg.Lines {
			ts, err := time.Parse(time.RFC3339, line.Timestamp)
			if err != nil {
				// Fallback to current time if parse fails
				ts = time.Now()
			}
			entry := LogEntry{
				Timestamp: ts,
				Level:     line.Level,
				Message:   line.Content,
				TaskID:    msg.TaskID,
				ProjectID: msg.ProjectID,
				RunnerID:  msg.RunnerID,
			}
			m.logViewer.AddEntry(entry)
		}
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

	case AutomationDataMsg:
		if msg.Error != nil {
			m.automationList.SetError(msg.Error.Error())
			return m, nil
		}
		m.automationGeneratedTasks = msg.GeneratedTasks
		m.automationList.SetEntryRows(msg.Automations, msg.ScheduledTasks, msg.GeneratedTasks)
		if m.activeContentTab == ContentTabAutomation && m.activeAutomationSubTab == AutomationSubTabAutomations && m.detailVisible {
			return m, m.syncAutomationEntryDetail()
		}
		return m, nil

	case BrainEntryContentMsg:
		if !m.detailVisible || msg.Path == "" || msg.Path != m.taskDetail.entryPath {
			return m, nil
		}
		header := "Entry Detail"
		if m.activeContentTab == ContentTabAutomation {
			header = "Automation Detail"
		}
		if msg.Err != nil {
			m.taskDetail.SetEntryError(msg.Path, msg.Title, msg.Type, msg.Err, header)
			return m, nil
		}
		content := msg.Content
		if m.activeContentTab == ContentTabAutomation && msg.Type == "automation" {
			// Cache raw automation content so the detail pane can be re-decorated
			// when async data loads or the generated-task cursor moves.
			if row := m.automationList.SelectedRow(); row != nil && row.Path == msg.Path {
				if m.goalDetailRaw == nil {
					m.goalDetailRaw = make(map[string]string)
				}
				m.goalDetailRaw[msg.Path] = msg.Content
			}
			content = m.automationDetailContent(msg.Path, content)
		}
		attachments := m.taskDetail.entryAttachments
		if msg.Attachments != nil {
			attachments = msg.Attachments
		}
		m.taskDetail.SetEntryContentWithAttachments(msg.Path, msg.Title, msg.Type, content, attachments, header)
		return m, nil

	case AttachmentActionMsg:
		if msg.Err != nil {
			m.setStatusMessage("error", fmt.Sprintf("Attachment %s failed: %v", msg.Action, msg.Err))
			m.addLog("error", fmt.Sprintf("Attachment %s failed for %s: %v", msg.Action, msg.AttachmentID, msg.Err))
			return m, nil
		}
		if msg.Action == "extract" {
			status := attachmentDisplayValue(msg.Status, "complete")
			detail := status
			if msg.Provider != "" || msg.Model != "" {
				detail = fmt.Sprintf("%s via %s / %s", status, attachmentDisplayValue(msg.Provider, "unknown provider"), attachmentDisplayValue(msg.Model, "unknown model"))
			}
			m.setStatusMessage("success", fmt.Sprintf("Attachment extract %s: %s", detail, msg.AttachmentID))
			m.addLog("info", fmt.Sprintf("Attachment extract %s for %s", detail, msg.AttachmentID))
			return m, nil
		}
		m.setStatusMessage("success", fmt.Sprintf("Attachment %s complete: %s", msg.Action, msg.Path))
		m.addLog("info", fmt.Sprintf("Attachment %s complete: %s", msg.Action, msg.Path))
		return m, nil

	case AutomationToggleMsg:
		if msg.Error != nil {
			m.setStatusMessage("error", fmt.Sprintf("Failed to toggle automation: %v", msg.Error))
			return m, nil
		}
		m.setStatusMessage("success", "Automation updated")
		return m, m.refreshActiveAutomationSubTab()

	case AutomationRunMsg:
		if msg.Error != nil {
			m.setStatusMessage("error", fmt.Sprintf("Failed to run automation: %v", msg.Error))
			return m, nil
		}
		m.setStatusMessage("success", fmt.Sprintf("Automation run queued: %s", msg.TaskID))
		return m, m.refreshActiveAutomationSubTab()

	case AutomationDeletedMsg:
		if m.modalManager.IsOpen() {
			m.modalManager.Close()
		}
		if msg.Error != nil {
			m.setStatusMessage("error", fmt.Sprintf("Failed to delete automation: %v", msg.Error))
			return m, nil
		}
		m.setStatusMessage("success", "Automation deleted")
		return m, m.refreshActiveAutomationSubTab()

	case goalConfigOpenMsg:
		// Resolved goal summary -> open the GoalConfigModal. Opening a modal
		// mutates the manager, so it must happen here on the Model.
		if msg.err != nil {
			m.setStatusMessage("error", fmt.Sprintf("Failed to load goal: %v", msg.err))
			return m, nil
		}
		if msg.summary == nil {
			m.setStatusMessage("error", "Goal summary unavailable")
			return m, nil
		}
		modal := NewGoalConfigModal(*msg.summary, runner.NewAPIClient(m.apiRunnerConfig()))
		return m, m.modalManager.Open(modal)

	case goalConfigSavedMsg:
		if msg.err != nil {
			verb := "save"
			if msg.created {
				verb = "create"
			}
			m.setStatusMessage("error", fmt.Sprintf("Failed to %s goal: %v", verb, msg.err))
			return m, nil
		}
		if msg.created {
			m.setStatusMessage("success", "Goal created")
		} else {
			m.setStatusMessage("success", "Goal updated")
		}
		m.modalManager.Close()
		return m, m.refreshActiveAutomationSubTab()

	case goalReconcileResultMsg:
		if msg.err != nil {
			m.setStatusMessage("error", fmt.Sprintf("Reconcile failed: %v", msg.err))
			return m, nil
		}
		if msg.audit != nil {
			m.setStatusMessage("success", fmt.Sprintf("Reconcile: %s — %s", string(msg.audit.Decision), msg.audit.Reason))
		} else {
			m.setStatusMessage("success", "Reconcile complete")
		}
		return m, m.refreshActiveAutomationSubTab()

	case goalDetailAuditMsg:
		if msg.err == nil {
			if m.goalAuditByEntry == nil {
				m.goalAuditByEntry = make(map[string][]types.GoalReconcileAudit)
			}
			m.goalAuditByEntry[msg.entryID] = msg.audit
		}
		// Re-render the goal detail pane with the now-available reconcile
		// history, using the cached raw content for the matching path.
		if row := m.automationList.SelectedRow(); row != nil && row.IsGoal && row.ID == msg.entryID && m.detailVisible {
			if raw, ok := m.goalDetailRaw[row.Path]; ok {
				content := m.automationDetailContent(row.Path, raw)
				m.taskDetail.SetEntryContent(row.Path, row.Title, "automation", content, "Automation Detail")
			}
		}
		return m, nil

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
			if msg.fullContent {
				err = apiClient.UpdateEntryFull(context.Background(), msg.taskPath, string(newContent))
			} else {
				_, err = apiClient.UpdateEntry(context.Background(), msg.taskPath, map[string]interface{}{
					"content": string(newContent),
				})
			}
			if err != nil {
				m.setStatusMessage("error", fmt.Sprintf("✗ Failed to sync changes: %v", err))
				return m, nil
			}

			if msg.fullContent {
				m.setStatusMessage("success", "✓ Entry updated from editor")
				return m, fetchBrainEntriesCmd(m.apiRunnerConfig(), m.currentProjectID())
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

	case automationsPauseToggledMsg:
		if msg.err != nil {
			m.automationsPaused = !msg.paused
			m.setStatusMessage("error", fmt.Sprintf("Failed to toggle automation pause: %v", msg.err))
		} else if msg.paused {
			m.setStatusMessage("success", "Automations paused")
			m.addLog("info", "Automations paused")
		} else {
			m.setStatusMessage("success", "Automations resumed")
			m.addLog("info", "Automations resumed")
		}
		m.syncHelpBarPauseState()
		return m, nil

	case runnerStatusMsg:
		if msg.err == nil {
			// If we have a direct runner controller, get pause state from it
			// instead of from the API server's separate RunnerService
			if m.runnerController != nil {
				m.allPaused = m.runnerController.IsAllPaused()
				m.automationsPaused = m.runnerController.IsAutomationsPaused()
				// Per-project pause state from the embedded runner
				m.pausedProjects = make(map[string]bool)
				for _, proj := range m.config.Projects {
					if m.runnerController.IsPaused(proj) && !m.runnerController.IsAllPaused() {
						m.pausedProjects[proj] = true
					}
				}
			} else {
				m.allPaused = msg.paused
				m.automationsPaused = msg.automationsPaused
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

			// Propagate default model setting to the runner
			if m.runnerController != nil {
				m.runnerController.SetDefaultModel(settings.DefaultModel)
			}
		}
		return m, nil

	case projectSelectedMsg:
		m.modalManager.Close()
		return m.selectProject(msg.projectID)

	case attachmentModalActionMsg:
		m.modalManager.Close()
		return m, brainAttachmentActionCmd(m.apiRunnerConfig(), m.currentProjectID(), msg.Attachment, msg.Action)
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
			sessionOpenedMsg, statusPickerResultMsg, AutomationDeletedMsg,
			goalConfigOpenMsg, goalConfigSavedMsg, goalReconcileResultMsg:
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

	// If on Automation > Dream with search typing mode, handle search input first
	if m.isAutomationDreamActive() && m.dreamViewer.SearchMode() == DreamSearchTyping {
		return m.handleDreamSearchInput(msg)
	}
	if m.activeContentTab == ContentTabBrain && m.brainSearchState == FilterTyping {
		return m.handleBrainSearchInput(msg)
	}
	if m.activeContentTab == ContentTabBrain && m.brainSearchState == FilterLocked {
		switch msg.Type {
		case tea.KeyEsc:
			m.clearBrainSearch()
			return m, nil
		case tea.KeyRunes:
			if string(msg.Runes) == "/" {
				m.brainSearchState = FilterTyping
				return m, nil
			}
		}
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
	// In multi-project mode, plain 'l' still means next project tab; 'z' toggles logs.
	if key.Matches(msg, m.keymap.ToggleLogs) && !(m.config.IsMultiProject() && key.Matches(msg, m.keymap.NextTab)) {
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
			if m.activeContentTab == ContentTabBrain {
				return m, m.syncBrainEntryDetail()
			}
			if m.activeContentTab == ContentTabAutomation && m.activeAutomationSubTab == AutomationSubTabAutomations {
				return m, m.syncAutomationEntryDetail()
			}
			m.syncTaskDetail()
		} else {
			m.syncPanelSizes()
		}
		return m, nil
	}
	if key.Matches(msg, m.keymap.NextContentTab) {
		m.activeContentTab = nextContentTab(m.activeContentTab)
		m.helpBar.ActiveContentTab = m.activeContentTab
		m.helpBar.ActiveAutomationSubTab = m.activeAutomationSubTab
		if m.activeContentTab == ContentTabRunners {
			m.activePanel = PanelRunners
			m.helpBar.ActivePanel = m.activePanel
			return m, fetchRunnerListCmd(m.apiRunnerConfig())
		}
		if m.activeContentTab == ContentTabLogs {
			m.activePanel = PanelLogs
			m.helpBar.ActivePanel = m.activePanel
			m.syncPanelSizes()
			return m, nil
		}
		if m.activeContentTab == ContentTabBrain {
			m.activePanel = PanelTasks
			m.helpBar.ActivePanel = m.activePanel
			return m, fetchBrainEntriesCmd(m.apiRunnerConfig(), m.currentProjectID())
		}
		if m.activeContentTab == ContentTabTasks {
			m.activePanel = PanelTasks
			m.helpBar.ActivePanel = m.activePanel
		}
		if m.activeContentTab == ContentTabAutomation {
			m.activePanel = PanelTasks
			m.helpBar.ActivePanel = m.activePanel
			return m, m.prepareActiveAutomationFetch()
		}
		return m, nil
	}
	if key.Matches(msg, m.keymap.PrevContentTab) {
		m.activeContentTab = prevContentTab(m.activeContentTab)
		m.helpBar.ActiveContentTab = m.activeContentTab
		m.helpBar.ActiveAutomationSubTab = m.activeAutomationSubTab
		if m.activeContentTab == ContentTabRunners {
			m.activePanel = PanelRunners
			m.helpBar.ActivePanel = m.activePanel
			return m, fetchRunnerListCmd(m.apiRunnerConfig())
		}
		if m.activeContentTab == ContentTabLogs {
			m.activePanel = PanelLogs
			m.helpBar.ActivePanel = m.activePanel
			m.syncPanelSizes()
			return m, nil
		}
		if m.activeContentTab == ContentTabBrain {
			m.activePanel = PanelTasks
			m.helpBar.ActivePanel = m.activePanel
			return m, fetchBrainEntriesCmd(m.apiRunnerConfig(), m.currentProjectID())
		}
		if m.activeContentTab == ContentTabTasks {
			m.activePanel = PanelTasks
			m.helpBar.ActivePanel = m.activePanel
		}
		if m.activeContentTab == ContentTabAutomation {
			m.activePanel = PanelTasks
			m.helpBar.ActivePanel = m.activePanel
			return m, m.prepareActiveAutomationFetch()
		}
		return m, nil
	}

	// When on Automation > Dream, forward non-rune keys (ctrl+d/u/f/b, arrows, pgup/pgdn)
	// to the viewport for vim-style scrolling. Only intercept quit keys.
	if m.isAutomationDreamActive() {
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
		if m.activeContentTab == ContentTabAutomation && m.activeAutomationSubTab == AutomationSubTabAutomations {
			return m, m.deleteSelectedAutomationRunTask()
		}
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
		if m.runnerPanelVisible {
			m.activePanel = NextPanelWithRunners(m.activePanel, m.detailVisible, m.logsVisible, true)
		} else {
			m.activePanel = NextPanel(m.activePanel, m.detailVisible, m.logsVisible)
		}
		m.helpBar.ActivePanel = m.activePanel
		return m, nil

	case tea.KeyEnter:
		if m.activeContentTab == ContentTabAutomation && m.activeAutomationSubTab == AutomationSubTabAutomations {
			m.automationList.ToggleExpandedSelected()
			return m, nil
		}
		if m.activeContentTab == ContentTabBrain && m.entryTree.ToggleCollapse() {
			return m, nil
		}
		// Enter toggles group collapse when on a group header
		if m.activePanel == PanelTasks && m.taskTree.IsOnGroupHeader() {
			m.taskTree.ToggleCollapse()
		}
		return m, nil

	case tea.KeySpace:
		if m.activeContentTab == ContentTabAutomation && m.activeAutomationSubTab == AutomationSubTabAutomations {
			return m, m.toggleSelectedAutomationRow()
		}
		if m.activeContentTab == ContentTabBrain && m.entryTree.ToggleCollapse() {
			return m, nil
		}
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
				return m.selectProject(m.projectTabs.ActiveProject())
			case "l", "]":
				m.projectTabs.NextTab()
				return m.selectProject(m.projectTabs.ActiveProject())
			case "1", "2", "3", "4", "5", "6", "7", "8", "9":
				tabNum := int(msg.Runes[0] - '0')
				if m.projectTabs.JumpToTab(tabNum) {
					return m.selectProject(m.projectTabs.ActiveProject())
				}
			}
		}

		// When on Automation tab, handle active-subtab keys.
		if m.activeContentTab == ContentTabAutomation {
			if m.activeAutomationSubTab == AutomationSubTabAutomations && msg.String() == "enter" {
				m.automationList.ToggleExpandedSelected()
				return m, nil
			}
			if m.activePanel == PanelDetails {
				switch string(msg.Runes) {
				case "j":
					m.taskDetail.ScrollDown()
					return m, nil
				case "k":
					m.taskDetail.ScrollUp()
					return m, nil
				case "g":
					m.taskDetail.ScrollToTop()
					return m, nil
				case "G":
					m.taskDetail.ScrollToBottom()
					return m, nil
				}
			}
			if m.activeAutomationSubTab == AutomationSubTabAutomations {
				switch string(msg.Runes) {
				case "o":
					return m, m.openSessionForAutomationGeneratedTask(false)
				case "O":
					return m, m.openSessionForAutomationGeneratedTask(true)
				case "d":
					return m, m.deleteSelectedAutomationItem()
				case "s":
					return m, m.openSelectedAutomationMetadata()
				case "j", "k", "g", "G":
					m.automationList.Update(msg)
					if m.detailVisible {
						return m, m.syncAutomationEntryDetail()
					}
					return m, nil
				case "n":
					// Create a brand-new goal automation.
					modal := NewGoalCreateModal(m.activeAutomationProject(), runner.NewAPIClient(m.apiRunnerConfig()))
					return m, m.modalManager.Open(modal)
				case "e":
					// Goal rows open the GoalConfigModal; non-goal rows open $EDITOR.
					if row := m.automationList.SelectedRow(); row != nil && row.IsGoal {
						return m, m.editSelectedGoalRow()
					}
					return m, m.editSelectedAutomationRow()
				case "r":
					return m, m.refreshActiveAutomationSubTab()
				case " ":
					// Toggle enable/disable. For goals this flips the underlying
					// automation entry status (active <-> archived), same as any
					// automation entry — toggleSelectedAutomationRow handles both.
					return m, m.toggleSelectedAutomationRow()
				case "x":
					if _, ok := m.automationList.SelectedRunTask(); ok {
						return m, m.executeSelectedAutomationRunTask()
					}
					// Goal rows run an inline reconcile via RunGoal; non-goal rows
					// use the generic manual-run that creates a task.
					if row := m.automationList.SelectedRow(); row != nil && row.IsGoal {
						return m, m.runSelectedGoalRow()
					}
					return m, m.runSelectedAutomationRow()
				case "p":
					currentlyPaused := m.automationsPaused
					m.automationsPaused = !currentlyPaused
					if currentlyPaused {
						m.setStatusMessage("info", "Resuming automations...")
					} else {
						m.setStatusMessage("info", "Pausing automations...")
					}
					m.syncHelpBarPauseState()
					return m, pauseAutomationsCmd(m.apiRunnerConfig(), currentlyPaused, m.runnerController)
				case "q":
					m.sseClient.Stop()
					return m, tea.Quit
				case "?":
					modal := NewHelpModal(m.config.IsMultiProject())
					cmd := m.modalManager.Open(modal)
					return m, cmd
				}
				return m, nil
			}

			// Automation > Dream: handle dream-specific keys then forward the rest to the viewport.
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
				return m, m.refreshActiveAutomationSubTab()
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

		if m.activeContentTab == ContentTabBrain {
			if m.activePanel == PanelDetails {
				switch string(msg.Runes) {
				case "a":
					if !m.taskDetail.HasEntryAttachments() {
						m.setStatusMessage("info", "No attachments on selected entry")
						return m, nil
					}
					modal := NewAttachmentModal(m.taskDetail.entryAttachments, m.taskDetail.selectedAttachment)
					return m, m.modalManager.Open(modal)
				case "A":
					if !m.taskDetail.SelectPrevAttachment() {
						m.setStatusMessage("info", "No attachments on selected entry")
					}
					return m, nil
				case "d":
					return m, m.selectedBrainAttachmentCmd("download")
				case "o":
					return m, m.selectedBrainAttachmentCmd("open")
				case "j":
					m.taskDetail.ScrollDown()
					return m, nil
				case "k":
					m.taskDetail.ScrollUp()
					return m, nil
				case "g":
					m.taskDetail.ScrollToTop()
					return m, nil
				case "G":
					m.taskDetail.ScrollToBottom()
					return m, nil
				}
			}
			switch string(msg.Runes) {
			case "/":
				m.brainSearchState = FilterTyping
				m.brainSearchQuery = ""
				m.brainSearchLabel = ""
				return m, nil
			case "j":
				m.entryTree.MoveDown()
				if m.detailVisible {
					return m, m.syncBrainEntryDetail()
				}
				return m, nil
			case "k":
				m.entryTree.MoveUp()
				if m.detailVisible {
					return m, m.syncBrainEntryDetail()
				}
				return m, nil
			case "g":
				m.entryTree.GotoTop()
				if m.detailVisible {
					return m, m.syncBrainEntryDetail()
				}
				return m, nil
			case "G":
				m.entryTree.GotoBottom()
				if m.detailVisible {
					return m, m.syncBrainEntryDetail()
				}
				return m, nil
			case "r":
				return m, fetchBrainEntriesCmd(m.apiRunnerConfig(), m.currentProjectID())
			case "b":
				return m, fetchBrainEmbeddingBackfillCmd(m.apiRunnerConfig(), m.currentProjectID(), false, false)
			case "B":
				return m, fetchBrainEmbeddingBackfillCmd(m.apiRunnerConfig(), "", true, false)
			case "F":
				return m, fetchBrainEmbeddingBackfillCmd(m.apiRunnerConfig(), m.currentProjectID(), false, true)
			case "A":
				return m, fetchBrainEmbeddingBackfillCmd(m.apiRunnerConfig(), "", true, true)
			case "e":
				return m.editSelectedBrainEntry()
			case "q":
				m.sseClient.Stop()
				return m, tea.Quit
			}
		}

		switch string(msg.Runes) {
		case "?":
			// Open help modal
			modal := NewHelpModal(m.config.IsMultiProject())
			cmd := m.modalManager.Open(modal)
			return m, cmd
		case "R":
			m.activeContentTab = ContentTabRunners
			m.activePanel = PanelRunners
			m.helpBar.ActiveContentTab = m.activeContentTab
			m.helpBar.ActivePanel = m.activePanel
			return m, fetchRunnerListCmd(m.apiRunnerConfig())
		case "S":
			// Open settings modal with task counts per status group and per-project running counts
			taskCounts := m.computeTaskCountsByStatus()
			runningPerProject := m.computeRunningPerProject()
			modal := NewSettingsModal(m.settings, WithTaskCounts(taskCounts), WithRunningPerProject(runningPerProject))
			cmd := m.modalManager.Open(modal)
			return m, cmd
		case "s":
			if m.activePanel == PanelRunners {
				selectedRunner := m.runnerPanel.SelectedRunner()
				if selectedRunner == nil {
					return m, nil
				}
				m.setStatusMessage("info", fmt.Sprintf("Requesting shutdown for runner %s...", selectedRunner.RunnerID))
				return m, shutdownRunnerCmd(m.apiRunnerConfig(), selectedRunner.RunnerID, "requested from TUI")
			}

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
		case "a":
			// Assign a project feature to the selected runner (runner panel only).
			if m.activePanel == PanelRunners {
				selectedRunner := m.runnerPanel.SelectedRunner()
				if selectedRunner == nil {
					return m, nil
				}
				projectID, ok := m.assignmentProjectID()
				if !ok {
					m.setStatusMessage("error", "Select a project tab before assigning a runner feature")
					return m, nil
				}

				allFeatures := m.assignmentFeaturesForProject(projectID)
				assignments := m.runnerAssignmentsForProject(projectID)
				apiClient := runner.NewAPIClient(m.apiRunnerConfig())
				modal := NewFeaturePickerModal(selectedRunner.RunnerID, projectID, allFeatures, assignments, apiClient)
				cmd := m.modalManager.Open(modal)
				return m, cmd
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
			if m.activePanel == PanelRunners {
				m.runnerPanel.MoveDown()
			} else if m.activePanel == PanelTasks {
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
			} else if m.activePanel == PanelLogs {
				m.logViewer.ScrollDown()
			}
			return m, nil
		case "k":
			if m.activePanel == PanelRunners {
				m.runnerPanel.MoveUp()
			} else if m.activePanel == PanelTasks {
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
			} else if m.activePanel == PanelLogs {
				m.logViewer.ScrollUp()
			}
			return m, nil
		case "g":
			if m.activePanel == PanelRunners {
				m.runnerPanel.GotoTop()
			} else if m.activePanel == PanelTasks {
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
			} else if m.activePanel == PanelLogs {
				for !m.logViewer.autoFollow && m.logViewer.scrollTop > 0 {
					m.logViewer.ScrollUp()
				}
			}
			return m, nil
		case "G":
			if m.activePanel == PanelRunners {
				m.runnerPanel.GotoBottom()
			} else if m.activePanel == PanelTasks {
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
			} else if m.activePanel == PanelLogs {
				m.logViewer.autoFollow = true
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
			if m.activeContentTab == ContentTabAutomation && m.activeAutomationSubTab == AutomationSubTabAutomations {
				currentlyPaused := m.automationsPaused
				m.automationsPaused = !currentlyPaused
				if currentlyPaused {
					m.setStatusMessage("info", "Resuming automations...")
				} else {
					m.setStatusMessage("info", "Pausing automations...")
				}
				m.syncHelpBarPauseState()
				return m, pauseAutomationsCmd(m.apiRunnerConfig(), currentlyPaused, m.runnerController)
			}
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
	case tea.MouseMotion:
		return m.handleMouseHover(msg)
	}

	return m, nil
}

func (m *Model) persistPanelHeights() {
	m.settings.TaskPanelHeight = m.taskPanelHeight
	m.settings.BottomTopPanelHeight = m.bottomTopPanelHeight
	_ = SaveSettings(m.settings)
}

func (m Model) isMainSplitterY(y int) bool {
	if !m.hasBottomPanel() {
		return false
	}
	mainContentStartY, taskPanelOuterHeight, _ := m.computeTaskPanelMetrics()
	return absInt(y-(mainContentStartY+taskPanelOuterHeight-1)) <= 1
}

func (m Model) isBottomSplitterY(y int) bool {
	if m.activeContentTab == ContentTabAutomation || m.runnerPanelVisible || !(m.detailVisible && m.logsVisible) {
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

func (m Model) handleMouseHover(_ tea.MouseMsg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m Model) brainMouseLine(mouseY, mainContentStartY int) int {
	// Terminal mouse coordinates track the cursor cell under the body of the
	// pointer; subtract one extra row so the arrow tip selects the intended row.
	return mouseY - mainContentStartY - 2
}

// handleMouseClick handles left mouse button clicks.
func (m Model) handleMouseClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	x, y := msg.X, msg.Y
	mainContentStartY, taskPanelOuterHeight, _ := m.computeTaskPanelMetrics()

	if m.config.IsMultiProject() {
		statusBarHeight := lipgloss.Height(m.statusBar.View(m.width))
		projectTabsView := m.projectTabs.View(m.width)
		projectTabsHeight := lipgloss.Height(projectTabsView)
		isProjectTabRow := projectTabsView != "" && y >= statusBarHeight && y < statusBarHeight+projectTabsHeight
		isAdjacentProjectRow := projectTabsView != "" && y == statusBarHeight+projectTabsHeight
		if isProjectTabRow || isAdjacentProjectRow {
			if tabIndex := m.projectTabs.TabIndexAt(x, m.width); tabIndex >= 0 {
				if tabIndex == 0 {
					m.modalManager.Open(NewProjectPickerModal(m.projectTabs.Projects, m.activeProjectID))
					return m, nil
				}
				return m.selectProject(m.projectTabs.Projects[tabIndex-1])
			}
			if isAdjacentProjectRow {
				if _, ok := m.contentTabAtX(x); ok {
					return m.handleContentTabClick(x)
				}
			}
			return m, nil
		}
	}

	// Click on content tab bar (the row just above mainContentStartY)
	contentTabBarY := mainContentStartY - 1
	if y == contentTabBarY || y == contentTabBarY+1 {
		return m.handleContentTabClick(x)
	}

	if m.activeContentTab == ContentTabRunners && y >= mainContentStartY && y < m.height-1 {
		m.activePanel = PanelRunners
		m.helpBar.ActivePanel = m.activePanel
		lineInPanel := y - mainContentStartY
		return m.handleRunnerPanelClick(lineInPanel, x)
	}
	if m.activeContentTab == ContentTabLogs && y >= mainContentStartY && y < m.height-1 {
		m.activePanel = PanelLogs
		m.helpBar.ActivePanel = m.activePanel
		return m, nil
	}
	if m.activeContentTab == ContentTabBrain && y >= mainContentStartY && y < m.height-1 {
		if m.detailVisible && y >= mainContentStartY+taskPanelOuterHeight {
			m.activePanel = PanelDetails
			m.helpBar.ActivePanel = m.activePanel
			line := y - (mainContentStartY + taskPanelOuterHeight) - 2 + m.taskDetail.scrollOffset
			if m.taskDetail.SelectAttachmentAtEntryLine(line) {
				idx := m.taskDetail.selectedAttachment
				now := time.Now()
				isDoubleClick := idx == m.lastAttachmentClickIndex && now.Sub(m.lastAttachmentClickTime) <= 500*time.Millisecond
				m.lastAttachmentClickIndex = idx
				m.lastAttachmentClickTime = now
				if isDoubleClick {
					return m, m.selectedBrainAttachmentCmd("open")
				}
			}
			return m, nil
		}
		m.activePanel = PanelTasks
		m.helpBar.ActivePanel = m.activePanel
		if m.entryTree.SelectVisibleLine(m.brainMouseLine(y, mainContentStartY)) && m.entryTree.IsOnGroupHeader() {
			m.entryTree.ToggleCollapse()
		}
		if m.detailVisible {
			return m, m.syncBrainEntryDetail()
		}
		return m, nil
	}
	if m.activeContentTab == ContentTabAutomation && y >= mainContentStartY && y < m.height-1 {
		if m.detailVisible && m.activeAutomationSubTab == AutomationSubTabAutomations && y >= mainContentStartY+taskPanelOuterHeight {
			m.activePanel = PanelDetails
			m.helpBar.ActivePanel = m.activePanel
			return m, nil
		}
		m.activePanel = PanelTasks
		m.helpBar.ActivePanel = m.activePanel
		return m.handleAutomationPanelClick(y-mainContentStartY, x)
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
		bottomStart := mainContentStartY + taskPanelOuterHeight
		if m.runnerPanelVisible {
			m.activePanel = PanelRunners
			m.helpBar.ActivePanel = m.activePanel
			lineInPanel := y - bottomStart
			return m.handleRunnerPanelClick(lineInPanel, x)
		}

		if m.detailVisible && m.logsVisible {
			bottomOuterHeight := m.height - bottomStart - 1 // reserve footer line
			detailHeight := bottomOuterHeight * 60 / 100
			if detailHeight < 4 {
				detailHeight = 4
			}
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

func (m Model) selectedBrainAttachmentCmd(action string) tea.Cmd {
	att, ok := m.taskDetail.SelectedAttachment()
	if !ok {
		return func() tea.Msg {
			return AttachmentActionMsg{Action: action, Err: fmt.Errorf("selected entry has no attachments")}
		}
	}
	return brainAttachmentActionCmd(m.apiRunnerConfig(), m.currentProjectID(), att, action)
}

func brainAttachmentActionCmd(cfg runner.RunnerConfig, projectID string, att types.AttachmentReference, action string) tea.Cmd {
	return func() tea.Msg {
		if action != "download" && action != "open" && action != "extract" {
			return AttachmentActionMsg{Action: action, AttachmentID: att.ID, Err: fmt.Errorf("unsupported attachment action %q", action)}
		}
		if strings.TrimSpace(att.ID) == "" {
			return AttachmentActionMsg{Action: action, Err: fmt.Errorf("attachment ID is required")}
		}
		client := runner.NewAPIClient(cfg)
		if action == "extract" {
			result, err := client.ExtractAttachmentText(context.Background(), projectID, att.ID)
			if err != nil {
				return AttachmentActionMsg{Action: action, AttachmentID: att.ID, Err: err}
			}
			msg := AttachmentActionMsg{Action: action, AttachmentID: att.ID}
			if result != nil {
				msg.Status = result.DerivedText.Status
				msg.Provider = result.DerivedText.Metadata["provider"]
				msg.Model = result.DerivedText.Metadata["model"]
			}
			return msg
		}
		attachment, data, err := client.DownloadAttachment(context.Background(), projectID, att.ID)
		if err != nil {
			return AttachmentActionMsg{Action: action, AttachmentID: att.ID, Err: err}
		}
		filename := att.Filename
		if filename == "" && attachment != nil {
			filename = attachment.Filename
		}
		if filename == "" {
			filename = att.ID
		}

		dir := attachmentDownloadDir(action)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return AttachmentActionMsg{Action: action, AttachmentID: att.ID, Err: fmt.Errorf("create attachment directory: %w", err)}
		}
		path, err := writeUniqueAttachmentFile(dir, filename, data)
		if err != nil {
			return AttachmentActionMsg{Action: action, AttachmentID: att.ID, Err: err}
		}
		if action == "open" {
			if err := openFileWithDefaultApp(path); err != nil {
				return AttachmentActionMsg{Action: action, AttachmentID: att.ID, Path: path, Err: err}
			}
		}
		return AttachmentActionMsg{Action: action, AttachmentID: att.ID, Path: path}
	}
}

func attachmentDownloadDir(action string) string {
	if action == "open" {
		return filepath.Join(os.TempDir(), "brain-attachments")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "."
	}
	return filepath.Join(home, "Downloads")
}

func writeUniqueAttachmentFile(dir, filename string, data []byte) (string, error) {
	name := sanitizeAttachmentFilename(filename)
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 0; i < 1000; i++ {
		candidate := filepath.Join(dir, name)
		if i > 0 {
			candidate = filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, i, ext))
		}
		file, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("create attachment file: %w", err)
		}
		if _, err := file.Write(data); err != nil {
			_ = file.Close()
			return "", fmt.Errorf("write attachment file: %w", err)
		}
		if err := file.Close(); err != nil {
			return "", fmt.Errorf("close attachment file: %w", err)
		}
		return candidate, nil
	}
	return "", fmt.Errorf("could not choose unique filename for %q", filename)
}

func sanitizeAttachmentFilename(filename string) string {
	name := filepath.Base(strings.TrimSpace(filename))
	if name == "." || name == string(filepath.Separator) || name == "" {
		return "attachment"
	}
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == 0 {
			return '-'
		}
		return r
	}, name)
	if strings.Trim(name, ". ") == "" {
		return "attachment"
	}
	return name
}

func openFileWithDefaultApp(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

func (m Model) handleContentTabClick(x int) (tea.Model, tea.Cmd) {
	newTab, ok := m.contentTabAtX(x)
	if !ok {
		return m, nil
	}
	if newTab != m.activeContentTab {
		m.activeContentTab = newTab
		m.helpBar.ActiveContentTab = m.activeContentTab
		if m.activeContentTab == ContentTabRunners {
			m.activePanel = PanelRunners
			m.helpBar.ActivePanel = m.activePanel
			return m, fetchRunnerListCmd(m.apiRunnerConfig())
		}
		if m.activeContentTab == ContentTabLogs {
			m.activePanel = PanelLogs
			m.helpBar.ActivePanel = m.activePanel
			m.syncPanelSizes()
			return m, nil
		}
		if m.activeContentTab == ContentTabBrain {
			m.activePanel = PanelTasks
			m.helpBar.ActivePanel = m.activePanel
			return m, fetchBrainEntriesCmd(m.apiRunnerConfig(), m.currentProjectID())
		}
		if m.activeContentTab == ContentTabTasks {
			m.activePanel = PanelTasks
			m.helpBar.ActivePanel = m.activePanel
		}
		if m.activeContentTab == ContentTabAutomation {
			m.activePanel = PanelTasks
			m.helpBar.ActivePanel = m.activePanel
			return m, m.prepareActiveAutomationFetch()
		}
	}
	return m, nil
}

// handleRunnerPanelClick handles clicks within the runner panel.
func (m Model) handleRunnerPanelClick(lineInPanel, x int) (tea.Model, tea.Cmd) {
	_ = x
	contentLine := lineInPanel - 1 // exclude top border
	if contentLine < 0 {
		return m, nil
	}

	// RunnerPanel.View renders title at content line 0, column header at 1,
	// and runner rows starting at 2.
	runnerRow := contentLine - 2 + m.runnerPanel.scrollTop
	if runnerRow >= 0 && runnerRow < len(m.runnerPanel.runners) {
		m.runnerPanel.SelectIndex(runnerRow)
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
	if m.activeContentTab == ContentTabTasks {
		return m.detailVisible || m.logsVisible || m.runnerPanelVisible
	}
	if m.activeContentTab == ContentTabBrain {
		return m.detailVisible
	}
	if m.activeContentTab == ContentTabAutomation && m.activeAutomationSubTab == AutomationSubTabAutomations {
		return m.detailVisible
	}
	return false
}

func (m Model) computeTaskPanelOuterHeight(mainHeight int) int {
	if !m.hasBottomPanel() {
		return mainHeight
	}
	if m.taskPanelHeight > 0 {
		return clampTaskPanelHeight(m.taskPanelHeight, mainHeight)
	}

	taskContentLines := 0
	if m.activeContentTab == ContentTabBrain {
		taskContentLines = len(m.entryTree.visible) + 1
	} else if m.activeContentTab == ContentTabAutomation && m.activeAutomationSubTab == AutomationSubTabAutomations {
		taskContentLines = len(m.automationList.rows) + 2
	} else if m.viewMode == ViewModeSchedules {
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

func (m Model) handleAutomationPanelClick(lineInPanel, x int) (tea.Model, tea.Cmd) {
	contentLine := lineInPanel - 1 // account for top border
	if contentLine < 0 {
		return m, nil
	}

	if m.activeAutomationSubTab != AutomationSubTabAutomations {
		return m, nil
	}

	rowLine := contentLine - 1 // terminal reports the cursor body row; use the pointer tip row
	if rowLine < 0 {
		return m, nil
	}
	if m.automationList.SelectVisibleRow(rowLine) && m.detailVisible {
		return m, m.syncAutomationEntryDetail()
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
	return m.handleMouseWheel(msg, -1)
}

// handleMouseWheelDown handles scroll wheel down (scroll down / move selection down).
func (m Model) handleMouseWheelDown(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	return m.handleMouseWheel(msg, 1)
}

func (m Model) handleMouseWheel(msg tea.MouseMsg, direction int) (tea.Model, tea.Cmd) {
	if m.activeContentTab == ContentTabAutomation {
		mainContentStartY, taskPanelOuterHeight, _ := m.computeTaskPanelMetrics()
		if msg.Y >= mainContentStartY && msg.Y < m.height-1 {
			if m.detailVisible && m.activeAutomationSubTab == AutomationSubTabAutomations && msg.Y >= mainContentStartY+taskPanelOuterHeight {
				if direction < 0 {
					m.taskDetail.ScrollUp()
				} else {
					m.taskDetail.ScrollDown()
				}
				return m, nil
			}
			if m.activeAutomationSubTab == AutomationSubTabAutomations {
				if direction < 0 {
					m.automationList.ScrollUp(3)
				} else {
					m.automationList.ScrollDown(3)
				}
				if m.detailVisible {
					return m, m.syncAutomationEntryDetail()
				}
				return m, nil
			}
			key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}
			if direction < 0 {
				key = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")}
			}
			cmd := m.dreamViewer.Update(key)
			return m, cmd
		}
		return m, nil
	}
	if m.activeContentTab == ContentTabBrain {
		mainContentStartY, taskPanelOuterHeight, _ := m.computeTaskPanelMetrics()
		if msg.Y >= mainContentStartY && msg.Y < m.height-1 {
			if m.detailVisible && msg.Y >= mainContentStartY+taskPanelOuterHeight {
				if direction < 0 {
					m.taskDetail.ScrollUp()
				} else {
					m.taskDetail.ScrollDown()
				}
				return m, nil
			}
			m.entryTree.SetSize(m.width-4, m.height-mainContentStartY-1)
			if direction < 0 {
				m.entryTree.MoveUp()
			} else {
				m.entryTree.MoveDown()
			}
			if m.detailVisible {
				return m, m.syncBrainEntryDetail()
			}
		}
		return m, nil
	}

	targetPanel, ok := m.panelAtMouseY(msg.Y)
	if !ok {
		return m, nil
	}

	m.syncPanelSizes()
	switch targetPanel {
	case PanelRunners:
		if direction < 0 {
			m.runnerPanel.MoveUp()
		} else {
			m.runnerPanel.MoveDown()
		}
	case PanelTasks:
		if m.viewMode == ViewModeSchedules {
			if direction < 0 {
				m.scheduleList.MoveUp()
			} else {
				m.scheduleList.MoveDown()
			}
			m.syncScheduleDetail()
		} else {
			if direction < 0 {
				m.taskTree.MoveUp()
			} else {
				m.taskTree.MoveDown()
			}
			m.syncTaskDetail()
		}
	case PanelDetails:
		if m.viewMode == ViewModeSchedules {
			if direction < 0 {
				m.scheduleDetail.ScrollUp()
			} else {
				m.scheduleDetail.ScrollDown()
			}
		} else {
			if direction < 0 {
				m.taskDetail.ScrollUp()
			} else {
				m.taskDetail.ScrollDown()
			}
		}
	case PanelLogs:
		if direction < 0 {
			m.logViewer.ScrollUp()
		} else {
			m.logViewer.ScrollDown()
		}
	}
	return m, nil
}

func (m Model) panelAtMouseY(y int) (Panel, bool) {
	mainContentStartY, taskPanelOuterHeight, _ := m.computeTaskPanelMetrics()
	if m.activeContentTab == ContentTabRunners {
		return PanelRunners, y >= mainContentStartY && y < m.height-1
	}
	if m.activeContentTab == ContentTabLogs {
		return PanelLogs, y >= mainContentStartY && y < m.height-1
	}
	if m.activeContentTab == ContentTabBrain {
		if y >= mainContentStartY && y < mainContentStartY+taskPanelOuterHeight {
			return PanelTasks, true
		}
		if m.detailVisible && y >= mainContentStartY+taskPanelOuterHeight && y < m.height-1 {
			return PanelDetails, true
		}
		return PanelTasks, false
	}
	if m.activeContentTab == ContentTabAutomation && m.activeAutomationSubTab == AutomationSubTabAutomations {
		if y >= mainContentStartY && y < mainContentStartY+taskPanelOuterHeight {
			return PanelTasks, true
		}
		if m.detailVisible && y >= mainContentStartY+taskPanelOuterHeight && y < m.height-1 {
			return PanelDetails, true
		}
		return PanelTasks, false
	}

	if y >= mainContentStartY && y < mainContentStartY+taskPanelOuterHeight {
		return PanelTasks, true
	}

	bottomStart := mainContentStartY + taskPanelOuterHeight
	if y < bottomStart || y >= m.height-1 {
		return PanelTasks, false
	}

	if m.runnerPanelVisible {
		return PanelRunners, true
	}
	if m.detailVisible && m.logsVisible {
		_, bottomOuterHeight := m.bottomPanelBounds()
		detailHeight := m.computeBottomTopPanelHeight(bottomOuterHeight)
		if y < bottomStart+detailHeight {
			return PanelDetails, true
		}
		return PanelLogs, true
	}
	if m.detailVisible {
		return PanelDetails, true
	}
	if m.logsVisible {
		return PanelLogs, true
	}
	return PanelTasks, false
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

func (m *Model) syncBrainEntryDetail() tea.Cmd {
	entry := m.entryTree.SelectedEntry()
	if entry == nil {
		m.taskDetail.SetTask(nil)
		m.syncPanelSizes()
		return nil
	}
	m.taskDetail.SetEntryLoading(*entry)
	m.syncPanelSizes()
	return fetchBrainEntryContentCmd(m.apiRunnerConfig(), *entry)
}

func (m *Model) syncAutomationEntryDetail() tea.Cmd {
	row := m.automationList.SelectedRow()
	if row == nil || row.Path == "" {
		m.taskDetail.SetTask(nil)
		m.syncPanelSizes()
		return nil
	}
	entry := types.BrainEntry{ID: row.ID, Path: row.Path, Title: row.Title, Type: row.Source}
	m.taskDetail.SetEntryLoading(entry, "Automation Detail")
	m.syncPanelSizes()
	contentCmd := fetchBrainEntryContentCmd(m.apiRunnerConfig(), entry)
	// Goal rows additionally load reconcile audit history for the detail pane.
	if row.IsGoal {
		auditCmd := fetchGoalDetailAuditCmd(m.apiRunnerConfig(), *row, m.activeAutomationProject())
		return tea.Batch(contentCmd, auditCmd)
	}
	return contentCmd
}

func (m *Model) automationDetailContent(path, content string) string {
	row := m.automationList.SelectedRow()
	if row == nil || row.Path != path {
		return content
	}

	var b strings.Builder
	b.WriteString(content)

	// Runs section: first-class automation_run entries when available, with
	// generated-task inference retained as compatibility fallback.
	generatedBy := "automation:" + row.ID
	realRuns := make([]types.BrainEntry, 0)
	runs := make([]types.BrainEntry, 0)
	for _, task := range m.automationGeneratedTasks {
		if task.Type == "automation_run" && automationRunContentField(task.Content, "automation_id") == row.ID {
			realRuns = append(realRuns, task)
		} else if task.GeneratedBy == generatedBy {
			runs = append(runs, task)
		}
	}
	if len(realRuns) > 0 {
		tasksByRun := automationTasksByRun(m.automationGeneratedTasks)
		sort.SliceStable(realRuns, func(i, j int) bool {
			return realRuns[i].Modified > realRuns[j].Modified
		})
		b.WriteString("\n\n## Runs\n")
		for _, run := range realRuns {
			status := run.Status
			if status == "" {
				status = "unknown"
			}
			b.WriteString(fmt.Sprintf("- %s [%s] %s", run.ID, status, run.Title))
			if run.Modified != "" {
				b.WriteString(" modified=" + run.Modified)
			}
			if errText := automationRunContentField(run.Content, "error"); errText != "" {
				b.WriteString(" error=" + errText)
			}
			if skipReason := automationRunContentField(run.Content, "skip_reason"); skipReason != "" {
				b.WriteString(" skip=" + skipReason)
			}
			b.WriteString("\n")
			seenTasks := make(map[string]bool)
			for _, taskID := range automationRunTaskIDs(run.Content) {
				seenTasks[taskID] = true
				if task, ok := tasksByRun[run.ID][taskID]; ok {
					b.WriteString("  - " + automationRunTaskDetailLine(task) + "\n")
					continue
				}
				b.WriteString("  - " + taskID + "\n")
			}
			extraTasks := make([]types.BrainEntry, 0, len(tasksByRun[run.ID]))
			for _, task := range tasksByRun[run.ID] {
				if seenTasks[task.ID] {
					continue
				}
				extraTasks = append(extraTasks, task)
			}
			sort.SliceStable(extraTasks, func(i, j int) bool {
				return extraTasks[i].ID < extraTasks[j].ID
			})
			for _, task := range extraTasks {
				b.WriteString("  - " + automationRunTaskDetailLine(task) + "\n")
			}
		}
	} else if len(runs) == 0 {
		b.WriteString("\n\n## Runs\nNo generated runs.")
	} else {
		sort.SliceStable(runs, func(i, j int) bool {
			return runs[i].Modified > runs[j].Modified
		})
		b.WriteString("\n\n## Runs\n")
		for _, task := range runs {
			line := automationRunTaskDetailLine(task)
			if task.Modified != "" {
				line += " modified=" + task.Modified
			}
			b.WriteString("- " + line + "\n")
		}
	}

	// Goal rows additionally render the last reconcile decision and audit
	// history from the asynchronously-cached audit (m.goalAuditByEntry).
	if row.IsGoal {
		b.WriteString(m.goalReconcileDetailSection(row.ID))
	}

	return b.String()
}

func automationRunTaskIDs(content string) []string {
	lines := strings.Split(content, "\n")
	result := make([]string, 0)
	inTasks := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### ") || strings.HasPrefix(trimmed, "## ") {
			inTasks = strings.EqualFold(strings.TrimSpace(strings.TrimLeft(trimmed, "#")), "Generated Tasks")
			continue
		}
		if !inTasks || !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		item := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		if item != "" && item != "none" {
			result = append(result, strings.Fields(item)[0])
		}
	}
	return result
}

func automationTasksByRun(entries []types.BrainEntry) map[string]map[string]types.BrainEntry {
	result := make(map[string]map[string]types.BrainEntry)
	for _, entry := range entries {
		if entry.Type != "task" || entry.AutomationRunID == "" {
			continue
		}
		if result[entry.AutomationRunID] == nil {
			result[entry.AutomationRunID] = make(map[string]types.BrainEntry)
		}
		result[entry.AutomationRunID][entry.ID] = entry
	}
	return result
}

func automationRunTaskDetailLine(task types.BrainEntry) string {
	status := task.Status
	if status == "" {
		status = "unknown"
	}
	line := fmt.Sprintf("%s [%s] %s", task.ID, status, task.Title)
	if task.GeneratedBy != "" {
		line += " generated_by=" + task.GeneratedBy
	}
	if task.AutomationRunID != "" {
		line += " automation_run_id=" + task.AutomationRunID
	}
	if task.Path != "" {
		line += " path=" + task.Path
	}
	if sessionID := newestSessionID(task.Sessions); sessionID != "" {
		line += " session=" + sessionID + " (o: open, O: tmux)"
	} else {
		line += " session=none"
	}
	return line
}

func newestSessionID(sessions map[string]types.SessionInfo) string {
	sessionIDs := sortedSessionIDs(sessions)
	if len(sessionIDs) == 0 {
		return ""
	}
	return sessionIDs[0]
}

func (m *Model) openSessionForAutomationGeneratedTask(tmuxMode bool) tea.Cmd {
	task, ok := m.automationList.SelectedRunTask()
	if !ok || task.Path == "" {
		m.setStatusMessage("warn", "No sessions available for generated tasks")
		m.addLog("warn", "No sessions available for generated tasks")
		return nil
	}
	if len(task.Sessions) == 0 {
		if sessionID := m.automationRunFinalizationSessionID(task); sessionID != "" {
			return func() tea.Msg {
				return sessionsFetchedMsg{
					sessionIDs: []string{sessionID},
					taskPath:   task.Path,
					tmuxMode:   tmuxMode,
				}
			}
		}
		if sessionID := localOpenCodeSessionIDForTask(task); sessionID != "" {
			return func() tea.Msg {
				return sessionsFetchedMsg{
					sessionIDs: []string{sessionID},
					taskPath:   task.Path,
					tmuxMode:   tmuxMode,
				}
			}
		}
	}
	resolved := types.ResolvedTask{
		ID:       task.ID,
		Path:     task.Path,
		Title:    task.Title,
		Status:   task.Status,
		Sessions: task.Sessions,
	}
	return m.openSessionForTask(&resolved, tmuxMode)
}

func (m *Model) automationRunFinalizationSessionID(task types.BrainEntry) string {
	automationID := strings.TrimPrefix(task.GeneratedBy, "automation:")
	if automationID == task.GeneratedBy || automationID == "" || task.AutomationRunID == "" {
		return ""
	}
	entry, ok := m.automationList.automationsByID[automationID]
	if !ok {
		return ""
	}
	finalization, ok := entry.RunFinalizations[task.AutomationRunID]
	if !ok {
		return ""
	}
	return finalization.SessionID
}

// goalReconcileDetailSection renders the "Last Reconcile" + "Audit" detail
// markdown for a goal automation row, using the audit cached under entryID. It
// returns a placeholder when no audit has been cached yet (the audit loads
// asynchronously, mirroring how Runs render off cached generated tasks).
func (m *Model) goalReconcileDetailSection(entryID string) string {
	audit := m.goalAuditByEntry[entryID]
	if len(audit) == 0 {
		return "\n\n## Last Reconcile\nNo reconcile history (or loading…)."
	}

	last := audit[0]
	var b strings.Builder
	b.WriteString("\n\n## Last Reconcile\n")
	b.WriteString(fmt.Sprintf("%s — %s", string(last.Decision), last.Reason))
	if last.Timestamp != "" {
		b.WriteString("\n" + last.Timestamp)
	}

	b.WriteString("\n\n## Audit\n")
	for _, a := range audit {
		b.WriteString(fmt.Sprintf("- %s [%s] %s", a.Timestamp, string(a.Decision), a.Reason))
		if a.GeneratedTaskID != "" {
			b.WriteString(" task=" + a.GeneratedTaskID)
		}
		b.WriteString("\n")
	}
	return b.String()
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

	innerWidth := m.width - 4
	if innerWidth < 10 {
		innerWidth = 10
	}
	if m.activeContentTab == ContentTabRunners {
		runnerInner := mainHeight - 2
		if runnerInner < 1 {
			runnerInner = 1
		}
		m.runnerPanel.SetSize(innerWidth, runnerInner)
		return
	}
	if m.activeContentTab == ContentTabLogs {
		logInner := mainHeight - 2
		if logInner < 1 {
			logInner = 1
		}
		m.logViewer.SetSize(innerWidth, logInner)
		return
	}
	if m.activeContentTab == ContentTabBrain {
		entryOuter := mainHeight
		if m.detailVisible {
			entryOuter = topHeight
		}
		entryInner := entryOuter - 2
		if entryInner < 1 {
			entryInner = 1
		}
		m.entryTree.SetSize(innerWidth, entryInner)
		if m.detailVisible {
			detailInner := bottomHeight - 2
			if detailInner < 1 {
				detailInner = 1
			}
			m.taskDetail.SetSize(innerWidth, detailInner)
		}
		return
	}
	if m.activeContentTab == ContentTabAutomation && m.activeAutomationSubTab == AutomationSubTabAutomations && m.detailVisible {
		detailInner := bottomHeight - 2
		if detailInner < 1 {
			detailInner = 1
		}
		m.taskDetail.SetSize(innerWidth, detailInner)
		return
	}

	hasBottomPanel := m.detailVisible || m.logsVisible || m.runnerPanelVisible
	if !hasBottomPanel {
		return
	}

	if m.runnerPanelVisible {
		runnerInner := bottomHeight - 2
		if runnerInner < 1 {
			runnerInner = 1
		}
		m.runnerPanel.SetSize(innerWidth, runnerInner)
	} else if m.detailVisible && m.logsVisible {
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

func (m Model) currentProjectID() string {
	if m.activeProjectID != "" && m.activeProjectID != "all" {
		return m.activeProjectID
	}
	return m.config.Project
}

func (m Model) editSelectedBrainEntry() (tea.Model, tea.Cmd) {
	entry := m.entryTree.SelectedEntry()
	if entry == nil {
		return m, nil
	}
	apiClient := runner.NewAPIClient(m.apiRunnerConfig())
	content, err := apiClient.GetEntryFull(context.Background(), entry.Path)
	if err != nil {
		m.setStatusMessage("error", fmt.Sprintf("Failed to fetch entry: %v", err))
		return m, nil
	}
	tempDir, err := os.MkdirTemp("", "brain-entry-")
	if err != nil {
		m.setStatusMessage("error", fmt.Sprintf("Failed to create temp dir: %v", err))
		return m, nil
	}
	tempFile := filepath.Join(tempDir, entry.ID+".md")
	if err := os.WriteFile(tempFile, []byte(content), 0o644); err != nil {
		m.setStatusMessage("error", fmt.Sprintf("Failed to write temp file: %v", err))
		return m, nil
	}

	return m, tea.ExecProcess(getEditorCmd(tempFile), func(err error) tea.Msg {
		return editorClosedMsg{
			taskID:          entry.ID,
			taskPath:        entry.Path,
			tempFile:        tempFile,
			originalContent: content,
			fullContent:     true,
			err:             err,
		}
	})
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
	if m.activeContentTab == ContentTabAutomation && m.activeAutomationSubTab == AutomationSubTabAutomations {
		m.statusBar.IsPaused = m.automationsPaused
	} else {
		projectID := m.activeProjectID
		if projectID == "" || projectID == "all" {
			projectID = m.config.Project
		}
		if m.activeProjectID == "all" && m.config.IsMultiProject() {
			// In "all" mode, paused only if every project is paused
			m.statusBar.IsPaused = m.allPaused
		} else {
			m.statusBar.IsPaused = m.allPaused || m.pausedProjects[projectID]
		}
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

	// Render content tab bar.
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
	if m.isAutomationDreamActive() {
		// Automation > Dream: show search bar
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
	} else if m.activeContentTab == ContentTabTasks {
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

	// Determine if bottom panels are visible
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
		// Runners tab: full-width runners panel, no task/detail/log panels.
		runnerView := m.runnerPanel.View(m.width-4, mainHeight-2)
		runnerPanel := InactiveBorder.
			Width(m.width - 2).
			Height(mainHeight - 2).
			Render(runnerView)
		mainContent = runnerPanel
	} else if m.activeContentTab == ContentTabLogs {
		// Logs tab: global full-height log stream.
		mainContent = m.renderLogPanel(m.width, mainHeight)
	} else if m.activeContentTab == ContentTabBrain {
		// Brain tab: project entry tree for understanding stored memory.
		entryOuterHeight := mainHeight
		if m.detailVisible {
			entryOuterHeight = topHeight
		}
		entryHeight := entryOuterHeight - 2
		entryView := m.entryTree.View(m.width-4, entryHeight)
		if searchBar := m.renderBrainSearchBar(m.width - 4); searchBar != "" {
			entryHeight--
			if entryHeight < 1 {
				entryHeight = 1
			}
			entryView = searchBar + "\n" + m.entryTree.View(m.width-4, entryHeight)
		}
		entryPanelStyle := InactiveBorder
		if m.activePanel == PanelTasks {
			entryPanelStyle = ActiveBorder
		}
		entryPanel := entryPanelStyle.
			Width(m.width - 2).
			Height(entryHeight).
			MaxHeight(entryOuterHeight).
			Render(entryView)
		if m.detailVisible {
			bottomPanel := m.renderBottomPanel(m.width, bottomHeight)
			mainContent = lipgloss.JoinVertical(lipgloss.Left, entryPanel, bottomPanel)
		} else {
			mainContent = entryPanel
		}
	} else if m.activeContentTab == ContentTabAutomation {
		// Automation tab: full-width automation list or Dream subtab.
		contentOuterHeight := mainHeight
		if m.detailVisible && m.activeAutomationSubTab == AutomationSubTabAutomations {
			contentOuterHeight = topHeight
		}
		contentHeight := contentOuterHeight - 2
		if contentHeight < 1 {
			contentHeight = 1
		}
		var content string
		if m.activeAutomationSubTab == AutomationSubTabDream {
			content = m.dreamViewer.View(m.width-4, contentHeight-1)
		} else {
			content = m.automationList.View(m.width-4, contentHeight)
		}
		automationPanelStyle := InactiveBorder
		if m.activePanel == PanelTasks {
			automationPanelStyle = ActiveBorder
		}
		automationPanel := automationPanelStyle.
			Width(m.width - 2).
			Height(contentHeight).
			MaxHeight(contentOuterHeight).
			Render(content)
		if m.detailVisible && m.activeAutomationSubTab == AutomationSubTabAutomations {
			bottomPanel := m.renderBottomPanel(m.width, bottomHeight)
			mainContent = lipgloss.JoinVertical(lipgloss.Left, automationPanel, bottomPanel)
		} else {
			mainContent = automationPanel
		}
	} else if m.runnerPanelVisible {
		// Runner panel visible: split into task panel (top) + runner panel (bottom)
		// Use the same bottom panel area for the runner panel
		runnerPanelStyle := InactiveBorder
		if m.activePanel == PanelRunners {
			runnerPanelStyle = ActiveBorder
		}

		runnerInnerHeight := bottomHeight - 2
		if runnerInnerHeight < 1 {
			runnerInnerHeight = 1
		}
		runnerView := m.runnerPanel.View(innerWidth, runnerInnerHeight)
		runnerView = truncateToHeight(runnerView, runnerInnerHeight)
		runnerPanelView := runnerPanelStyle.
			Width(m.width - 2).
			Height(runnerInnerHeight).
			MaxHeight(bottomHeight).
			Render(runnerView)

		mainContent = lipgloss.JoinVertical(lipgloss.Left, taskPanel, runnerPanelView)
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

// handleDreamSearchInput processes keyboard input when searching in the Dream tab.
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

// handleBrainSearchInput processes Brain tab search input.
func (m Model) handleBrainSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		if m.brainSearchQuery == "" {
			m.clearBrainSearch()
			return m, nil
		}
		m.brainSearchState = FilterLocked
		return m, fetchBrainSearchCmd(m.apiRunnerConfig(), m.currentProjectID(), m.brainSearchQuery)

	case tea.KeyEsc:
		m.clearBrainSearch()
		return m, nil

	case tea.KeyBackspace, tea.KeyDelete:
		if len(m.brainSearchQuery) > 0 {
			m.brainSearchQuery = m.brainSearchQuery[:len(m.brainSearchQuery)-1]
		}
		return m, nil

	case tea.KeyCtrlU:
		m.brainSearchQuery = ""
		return m, nil

	case tea.KeySpace:
		m.brainSearchQuery += " "
		return m, nil

	case tea.KeyRunes:
		m.brainSearchQuery += string(msg.Runes)
		return m, nil
	}
	return m, nil
}

func (m *Model) clearBrainSearch() {
	m.brainSearchState = FilterOff
	m.brainSearchQuery = ""
	m.brainSearchLabel = ""
	if m.brainEntries != nil {
		m.entryTree.SetEntries(m.brainEntries)
	}
}

func (m Model) renderBrainSearchBar(width int) string {
	switch m.brainSearchState {
	case FilterTyping:
		return FilterTypingStyle.Width(width).Render(fmt.Sprintf(" / %s_ ", m.brainSearchQuery))
	case FilterLocked:
		strategy := m.brainSearchLabel
		if strategy == "" {
			strategy = "search"
		}
		return FilterLockedStyle.Render(fmt.Sprintf(" Brain %s: %s (%d) ", strategy, m.brainSearchQuery, len(m.entryTree.entries))) + DimStyle.Render("  Esc: clear")
	default:
		return ""
	}
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

func visibleTaskRows(tasks []types.ResolvedTask) []types.ResolvedTask {
	visible := make([]types.ResolvedTask, 0, len(tasks))
	for _, task := range tasks {
		if strings.HasPrefix(task.GeneratedBy, "automation:") {
			continue
		}
		visible = append(visible, task)
	}
	return visible
}

func displayStatsForTasks(tasks []types.ResolvedTask, fallback TaskStats) TaskStats {
	stats := fallback
	for _, task := range tasks {
		if !strings.HasPrefix(task.GeneratedBy, "automation:") {
			continue
		}
		switch task.Status {
		case "pending":
			if stats.Ready > 0 {
				stats.Ready--
			}
		case "blocked":
			if stats.Blocked > 0 {
				stats.Blocked--
			}
		case "active", "in_progress":
			if stats.InProgress > 0 {
				stats.InProgress--
			}
		default:
			if stats.Completed > 0 {
				stats.Completed--
			}
		}
	}
	return stats
}

func (m Model) assignmentProjectID() (string, bool) {
	if m.config.IsMultiProject() {
		if m.activeProjectID == "" || m.activeProjectID == "all" {
			return "", false
		}
		return m.activeProjectID, true
	}
	if m.config.Project == "" || m.config.Project == "all" {
		return "", false
	}
	return m.config.Project, true
}

func (m Model) assignmentFeaturesForProject(projectID string) []string {
	tasks := m.tasks
	if projectTasks, ok := m.tasksByProject[projectID]; ok {
		tasks = projectTasks
	}

	featureIDSet := make(map[string]bool)
	for _, task := range tasks {
		if task.ProjectID != "" && task.ProjectID != projectID {
			continue
		}
		if task.FeatureID != "" {
			featureIDSet[task.FeatureID] = true
		}
	}

	features := make([]string, 0, len(featureIDSet))
	for featureID := range featureIDSet {
		features = append(features, featureID)
	}
	sort.Strings(features)
	return features
}

func (m Model) runnerAssignmentsForProject(projectID string) []types.FeatureAssignmentResponse {
	assignmentsByFeature := make(map[string]types.FeatureAssignmentResponse)
	for _, runnerInfo := range m.runnerPanel.runners {
		for _, assignment := range runnerInfo.FeatureAssignments {
			if assignment.ProjectID == projectID && assignment.FeatureID != "" {
				assignmentsByFeature[assignment.FeatureID] = assignment
			}
		}
	}

	featureIDs := make([]string, 0, len(assignmentsByFeature))
	for featureID := range assignmentsByFeature {
		featureIDs = append(featureIDs, featureID)
	}
	sort.Strings(featureIDs)

	assignments := make([]types.FeatureAssignmentResponse, 0, len(featureIDs))
	for _, featureID := range featureIDs {
		assignments = append(assignments, assignmentsByFeature[featureID])
	}
	return assignments
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
		m.stats = displayStatsForTasks(m.tasks, m.stats)
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
		m.stats = displayStatsForTasks(m.tasks, m.stats)
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

func (m Model) selectProject(projectID string) (tea.Model, tea.Cmd) {
	if projectID == "" {
		projectID = "all"
	}
	m.projectTabs.SetActiveProject(projectID)
	m.activeProjectID = m.projectTabs.ActiveProject()
	m.syncActiveProjectView()
	if m.activeContentTab == ContentTabAutomation {
		return m, m.refreshActiveAutomationSubTab()
	}
	return m, m.refreshBrainOnProjectSwitch()
}

func (m *Model) refreshBrainOnProjectSwitch() tea.Cmd {
	if m.activeContentTab != ContentTabBrain {
		return nil
	}
	m.clearBrainSearch()
	m.brainEntries = nil
	m.entryTree.SetEntries(nil)
	return fetchBrainEntriesCmd(m.apiRunnerConfig(), m.currentProjectID())
}

func (m *Model) activeDreamProject() string {
	project := m.config.Project
	if m.activeProjectID != "" && m.activeProjectID != "all" {
		project = m.activeProjectID
	}
	return project
}

func (m *Model) activeAutomationProject() string {
	return m.activeDreamProject()
}

func (m Model) isAutomationDreamActive() bool {
	return m.activeContentTab == ContentTabAutomation && m.activeAutomationSubTab == AutomationSubTabDream
}

func (m *Model) cycleAutomationSubTab() tea.Cmd {
	if m.activeAutomationSubTab == AutomationSubTabAutomations {
		return m.setAutomationSubTab(AutomationSubTabDream)
	}
	return m.setAutomationSubTab(AutomationSubTabAutomations)
}

func (m *Model) setAutomationSubTab(tab AutomationSubTab) tea.Cmd {
	m.activeAutomationSubTab = tab
	m.helpBar.ActiveAutomationSubTab = tab
	return m.prepareActiveAutomationFetch()
}

func (m *Model) prepareActiveAutomationFetch() tea.Cmd {
	if m.activeAutomationSubTab == AutomationSubTabDream {
		if !m.dreamViewer.HasContent() || !m.dreamViewer.HasConfig() {
			m.prepareDreamFetch()
			return m.fetchDreamTabCmd()
		}
		return nil
	}
	if len(m.automationList.rows) == 0 {
		m.prepareAutomationFetch()
		return m.fetchAutomationListCmd()
	}
	return nil
}

func (m *Model) refreshActiveAutomationSubTab() tea.Cmd {
	if m.activeAutomationSubTab == AutomationSubTabDream {
		m.prepareDreamFetch()
		return m.fetchDreamTabCmd()
	}
	m.prepareAutomationFetch()
	return m.fetchAutomationListCmd()
}

func (m *Model) prepareAutomationFetch() {
	m.automationList.SetLoading(true)
}

func (m *Model) fetchAutomationListCmd() tea.Cmd {
	return fetchAutomationDataCmd(m.apiRunnerConfig(), m.activeAutomationProject())
}

func (m *Model) toggleSelectedAutomationRow() tea.Cmd {
	row := m.automationList.SelectedRow()
	if row == nil {
		return nil
	}
	return toggleAutomationRowCmd(m.apiRunnerConfig(), *row)
}

func (m *Model) runSelectedAutomationRow() tea.Cmd {
	row := m.automationList.SelectedRow()
	if row == nil {
		return nil
	}
	return runAutomationRowCmd(m.apiRunnerConfig(), *row, m.activeAutomationProject())
}

func (m *Model) executeSelectedAutomationRunTask() tea.Cmd {
	entry, ok := m.automationList.SelectedRunTask()
	if !ok {
		return nil
	}
	task := resolvedTaskFromAutomationRunEntry(entry)
	actionLabel := "Execute"
	if task.Status == "in_progress" {
		actionLabel = "Resume"
	}
	projectID := m.activeAutomationProject()
	rc := m.runnerController
	taskCopy := task
	modal := NewConfirmModal(actionLabel+" Task", fmt.Sprintf("%s task '%s' now?", actionLabel, task.Title)).
		WithOnConfirm(func() tea.Msg {
			if rc != nil {
				return executeTaskViaRunnerCmd(rc, taskCopy, projectID)()
			}
			runnerID := "manual-tui"
			if m.config.RunnerID != "" {
				runnerID = m.config.RunnerID
			}
			apiClient := runner.NewAPIClient(m.apiRunnerConfig())
			return executeTaskCmd(apiClient, projectID, taskCopy.ID, runnerID)()
		})
	return m.modalManager.Open(modal)
}

func (m *Model) deleteSelectedAutomationRunTask() tea.Cmd {
	entry, ok := m.automationList.SelectedRunTask()
	if !ok {
		return nil
	}
	apiClient := runner.NewAPIClient(m.apiRunnerConfig())
	modal := NewConfirmModal("Delete Task", "Delete 1 task(s)?").
		WithTaskTitles([]string{entry.Title}).
		WithDestructive(true).
		WithOnConfirm(func() tea.Msg {
			return deleteTaskCmd(apiClient, entry.Path)()
		})
	return m.modalManager.Open(modal)
}

func (m *Model) deleteSelectedAutomationItem() tea.Cmd {
	if _, ok := m.automationList.SelectedRunTask(); ok {
		return m.deleteSelectedAutomationRunTask()
	}
	row := m.automationList.SelectedRow()
	if row == nil {
		return nil
	}
	if row.Source != "automation" {
		m.setStatusMessage("warn", "Only automation entries can be deleted here")
		return nil
	}
	if row.Scope == "global" || row.Scope == "built-in" || strings.HasPrefix(row.Path, "global/") {
		m.setStatusMessage("warn", "Built-in automations cannot be deleted")
		return nil
	}
	if !strings.HasPrefix(row.Path, "projects/") {
		m.setStatusMessage("warn", "Only project-level automations can be deleted")
		return nil
	}
	rowCopy := *row
	modal := NewConfirmModal("Delete Automation", "Delete project automation?").
		WithTaskTitles([]string{row.Title}).
		WithDestructive(true).
		WithOnConfirm(func() tea.Msg {
			return deleteAutomationRowCmd(m.apiRunnerConfig(), rowCopy)()
		})
	return m.modalManager.Open(modal)
}

func (m *Model) openSelectedAutomationMetadata() tea.Cmd {
	apiClient := runner.NewAPIClient(m.apiRunnerConfig())
	if entry, ok := m.automationList.SelectedRunTask(); ok {
		if entry.Path == "" {
			m.setStatusMessage("error", "Selected automation run task has no path")
			return nil
		}
		return m.modalManager.Open(NewMetadataModal(entry.Path, apiClient))
	}
	row := m.automationList.SelectedRow()
	if row == nil {
		return nil
	}
	path := row.Path
	if path == "" {
		path = row.ID
	}
	if path == "" {
		m.setStatusMessage("error", "Selected automation has no path")
		return nil
	}
	return m.modalManager.Open(NewMetadataModal(path, apiClient))
}

func resolvedTaskFromAutomationRunEntry(entry types.BrainEntry) *types.ResolvedTask {
	return &types.ResolvedTask{
		ID:            entry.ID,
		Path:          entry.Path,
		Title:         entry.Title,
		Content:       entry.Content,
		Priority:      entry.Priority,
		Status:        entry.Status,
		Created:       entry.Created,
		Workdir:       entry.Workdir,
		GitRemote:     entry.GitRemote,
		GitBranch:     entry.GitBranch,
		TargetWorkdir: entry.TargetWorkdir,
		DirectPrompt:  entry.DirectPrompt,
		Agent:         entry.Agent,
		Model:         entry.Model,
		Executor:      entry.Executor,
		Sessions:      entry.Sessions,
		Generated:     entry.Generated,
		GeneratedBy:   entry.GeneratedBy,
	}
}

func (m *Model) editSelectedAutomationRow() tea.Cmd {
	row := m.automationList.SelectedRow()
	if row == nil {
		return nil
	}
	path := row.Path
	if path == "" {
		path = row.ID
	}
	if path == "" {
		m.setStatusMessage("error", "Selected automation has no path")
		return nil
	}

	apiClient := runner.NewAPIClient(m.apiRunnerConfig())
	entry, err := apiClient.GetEntry(context.Background(), path)
	if err != nil {
		m.setStatusMessage("error", fmt.Sprintf("Failed to fetch automation: %v", err))
		return nil
	}

	tempDir, err := os.MkdirTemp("", "brain-automation-")
	if err != nil {
		m.setStatusMessage("error", fmt.Sprintf("Failed to create temp dir: %v", err))
		return nil
	}
	tempFile := filepath.Join(tempDir, row.ID+".md")
	if err := os.WriteFile(tempFile, []byte(entry.Content), 0o644); err != nil {
		os.RemoveAll(tempDir)
		m.setStatusMessage("error", fmt.Sprintf("Failed to write temp file: %v", err))
		return nil
	}

	originalContent := entry.Content
	return tea.ExecProcess(getEditorCmd(tempFile), func(err error) tea.Msg {
		return editorClosedMsg{
			taskID:          row.ID,
			taskPath:        path,
			tempFile:        tempFile,
			originalContent: originalContent,
			err:             err,
		}
	})
}

// ============================================================================
// Goal automation row actions (Phase 3)
//
// When the selected Automations row is a goal (IsGoal == true), the `e` and `x`
// keys route to goal-specific behavior instead of the generic automation
// editor / manual-run:
//   - `e` -> editSelectedGoalRow(): resolves the goal's GoalSummary and opens
//            the GoalConfigModal (NOT $EDITOR).
//   - `x` -> runSelectedGoalRow(): runs an inline goal reconcile via
//            apiClient.RunGoal (NOT the generic automation manual-run that
//            creates a task).
//
// Goal id resolution: AutomationListRow carries the automation entry id (ID)
// and path, but the goal API methods key off the goal id. We resolve the goal
// id by calling apiClient.ListGoals(project, "") and matching on
// GoalSummary.EntryID == row.ID. This is preferred over GetEntry because it
// returns a fully-populated types.GoalSummary that can be passed straight into
// NewGoalConfigModal, AND yields GoalID for RunGoal/GoalAudit — one fetch
// serves both needs.
// ============================================================================

// goalConfigOpenMsg is emitted after editSelectedGoalRow resolves the goal's
// summary. The main Update opens the GoalConfigModal (opening a modal mutates
// the ModalManager, which must happen on the Model in Update, not in a cmd).
type goalConfigOpenMsg struct {
	summary *types.GoalSummary
	err     error
}

// goalReconcileResultMsg is emitted after runSelectedGoalRow triggers an inline
// goal reconcile via apiClient.RunGoal.
type goalReconcileResultMsg struct {
	goalID string
	audit  *types.GoalReconcileAudit
	err    error
}

// goalDetailAuditMsg carries reconcile audit history fetched for the goal detail
// pane. It is DISTINCT from the modal's goalAuditLoadedMsg so the two consumers
// (detail pane vs config modal) never clash. entryID is the automation entry id
// (AutomationListRow.ID) used as the cache key in m.goalAuditByEntry.
type goalDetailAuditMsg struct {
	entryID string
	audit   []types.GoalReconcileAudit
	err     error
}

// resolveGoalSummaryForRow fetches the fully-populated GoalSummary for an
// automation row by listing goals for the project and matching on EntryID.
func resolveGoalSummaryForRow(cfg runner.RunnerConfig, row AutomationListRow, project string) (*types.GoalSummary, error) {
	apiClient := runner.NewAPIClient(cfg)
	goals, err := apiClient.ListGoals(context.Background(), project, "")
	if err != nil {
		return nil, err
	}
	for i := range goals {
		if goals[i].EntryID == row.ID {
			return &goals[i], nil
		}
	}
	return nil, fmt.Errorf("goal not found for automation entry %q", row.ID)
}

// editSelectedGoalRow returns a command that resolves the selected goal row's
// GoalSummary and emits goalConfigOpenMsg. Returns nil when no row is selected
// or the selected row is not a goal (callers fall back to generic behavior).
func (m *Model) editSelectedGoalRow() tea.Cmd {
	row := m.automationList.SelectedRow()
	if row == nil || !row.IsGoal {
		return nil
	}
	cfg := m.apiRunnerConfig()
	project := m.activeAutomationProject()
	selected := *row
	return func() tea.Msg {
		summary, err := resolveGoalSummaryForRow(cfg, selected, project)
		return goalConfigOpenMsg{summary: summary, err: err}
	}
}

// runSelectedGoalRow returns a command that resolves the selected goal row's
// goal id and triggers an inline reconcile via apiClient.RunGoal, emitting
// goalReconcileResultMsg. Returns nil when no row is selected or the selected
// row is not a goal.
func (m *Model) runSelectedGoalRow() tea.Cmd {
	row := m.automationList.SelectedRow()
	if row == nil || !row.IsGoal {
		return nil
	}
	cfg := m.apiRunnerConfig()
	project := m.activeAutomationProject()
	selected := *row
	return func() tea.Msg {
		summary, err := resolveGoalSummaryForRow(cfg, selected, project)
		if err != nil {
			return goalReconcileResultMsg{err: err}
		}
		apiClient := runner.NewAPIClient(cfg)
		audit, err := apiClient.RunGoal(context.Background(), summary.GoalID)
		return goalReconcileResultMsg{goalID: summary.GoalID, audit: audit, err: err}
	}
}

// fetchGoalDetailAuditCmd returns a command that loads the reconcile audit
// history for a goal row's detail pane and emits goalDetailAuditMsg.
func fetchGoalDetailAuditCmd(cfg runner.RunnerConfig, row AutomationListRow, project string) tea.Cmd {
	entryID := row.ID
	return func() tea.Msg {
		summary, err := resolveGoalSummaryForRow(cfg, row, project)
		if err != nil {
			return goalDetailAuditMsg{entryID: entryID, err: err}
		}
		apiClient := runner.NewAPIClient(cfg)
		audit, err := apiClient.GoalAudit(context.Background(), summary.GoalID, 10)
		return goalDetailAuditMsg{entryID: entryID, audit: audit, err: err}
	}
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

func fetchBrainEntriesCmd(cfg runner.RunnerConfig, project string) tea.Cmd {
	return func() tea.Msg {
		client := runner.NewAPIClient(cfg)
		params := map[string]string{"sortBy": "modified", "sortOrder": "desc", "limit": "500", "include": "attachments"}
		if project != "" && project != "all" {
			params["project"] = project
		}
		resp, err := client.ListEntries(context.Background(), params)
		if err != nil {
			return BrainEntriesMsg{Err: err}
		}
		return BrainEntriesMsg{Entries: resp.Entries}
	}
}

func fetchBrainEntryContentCmd(cfg runner.RunnerConfig, entry types.BrainEntry) tea.Cmd {
	return func() tea.Msg {
		if entry.Path == "" {
			return BrainEntryContentMsg{Path: entry.Path, Title: entry.Title, Type: entry.Type, Err: fmt.Errorf("entry has no path")}
		}
		client := runner.NewAPIClient(cfg)
		content, err := client.GetEntryFull(context.Background(), entry.Path)
		return BrainEntryContentMsg{Path: entry.Path, Title: entry.Title, Type: entry.Type, Content: content, Attachments: append([]types.AttachmentReference(nil), entry.Attachments...), Err: err}
	}
}

func fetchBrainSearchCmd(cfg runner.RunnerConfig, project, query string) tea.Cmd {
	return func() tea.Msg {
		strategy := "semantic"
		limit := 10
		client := runner.NewAPIClient(cfg)
		resp, err := client.SearchEntries(context.Background(), types.SearchRequest{
			Query:    query,
			Project:  project,
			Strategy: strategy,
			Limit:    &limit,
			Include:  []string{"attachments"},
		})
		if err != nil {
			return BrainSearchMsg{Query: query, Strategy: strategy, Err: err}
		}
		entries := make([]types.BrainEntry, 0, len(resp.Results))
		for _, result := range resp.Results {
			entries = append(entries, types.BrainEntry{
				ID:          result.ID,
				Path:        result.Path,
				Title:       result.Title,
				Type:        result.Type,
				Status:      result.Status,
				ProjectID:   project,
				Attachments: append([]types.AttachmentReference(nil), result.Attachments...),
			})
		}
		return BrainSearchMsg{Entries: entries, Query: query, Strategy: strategy}
	}
}

func fetchBrainEmbeddingBackfillCmd(cfg runner.RunnerConfig, project string, all, force bool) tea.Cmd {
	return func() tea.Msg {
		client := runner.NewAPIClient(cfg)
		req := types.EmbeddingBackfillRequest{Force: force}
		if !all {
			req.Project = project
		}
		resp, err := client.BackfillEmbeddings(context.Background(), req)
		return BrainEmbeddingBackfillMsg{Project: req.Project, All: all, Force: force, Result: resp, Err: err}
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

type editorClosedMsg struct {
	taskID          string
	taskPath        string
	tempFile        string
	originalContent string
	fullContent     bool
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

type automationsPauseToggledMsg struct {
	paused bool
	err    error
}

type runnerStatusMsg struct {
	paused            bool
	pausedProjects    []string
	automationsPaused bool
	err               error
}

type apiHealthMsg struct {
	health runner.APIHealth
	err    error
}

type runnerShutdownRequestedMsg struct {
	runnerID string
	err      error
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

func fetchAutomationDataCmd(cfg runner.RunnerConfig, project string) tea.Cmd {
	return func() tea.Msg {
		apiClient := runner.NewAPIClient(cfg)
		ctx := context.Background()

		automationParams := map[string]string{"type": "automation"}
		taskParams := map[string]string{"type": "task"}
		if project != "" {
			automationParams["project"] = project
			taskParams["project"] = project
		}

		automations, err := apiClient.ListEntries(ctx, automationParams)
		if err != nil {
			return AutomationDataMsg{Error: fmt.Errorf("fetch automation entries: %w", err)}
		}
		if project != "" {
			globalAutomations, err := apiClient.ListEntries(ctx, map[string]string{"type": "automation", "global": "true"})
			if err != nil {
				return AutomationDataMsg{Error: fmt.Errorf("fetch global automation entries: %w", err)}
			}
			automations.Entries = appendUniqueAutomationEntries(globalAutomations.Entries, automations.Entries)
		}
		tasks, err := apiClient.ListEntries(ctx, taskParams)
		if err != nil {
			return AutomationDataMsg{Error: fmt.Errorf("fetch scheduled task entries: %w", err)}
		}

		// Collect feature IDs of goal automations so their linked tasks can be
		// included in generatedTasks for progress computation. Goal automations
		// link work via the shared feature_id (mirroring server GetTasksByFeature).
		goalFeatureIDs := make(map[string]bool)
		for _, entry := range automations.Entries {
			if entry.GeneratedBy == types.GoalGeneratedBy && entry.FeatureID != "" {
				goalFeatureIDs[entry.FeatureID] = true
			}
		}

		scheduledTasks := make([]types.BrainEntry, 0, len(tasks.Entries))
		generatedTasks := make([]types.BrainEntry, 0)
		for _, task := range tasks.Entries {
			if task.Schedule != "" || task.RunOnceAt != "" {
				scheduledTasks = append(scheduledTasks, task)
			}
			if strings.HasPrefix(task.GeneratedBy, "automation:") {
				generatedTasks = append(generatedTasks, task)
			} else if task.FeatureID != "" && goalFeatureIDs[task.FeatureID] {
				generatedTasks = append(generatedTasks, task)
			}
		}
		runParams := map[string]string{"type": "automation_run"}
		if project != "" {
			runParams["project"] = project
		}
		if automationRuns, err := apiClient.ListEntries(ctx, runParams); err == nil {
			generatedTasks = append(generatedTasks, automationRuns.Entries...)
		}

		return AutomationDataMsg{Automations: automations.Entries, ScheduledTasks: scheduledTasks, GeneratedTasks: generatedTasks}
	}
}

func appendUniqueAutomationEntries(first, second []types.BrainEntry) []types.BrainEntry {
	entries := make([]types.BrainEntry, 0, len(first)+len(second))
	seen := make(map[string]struct{}, len(first)+len(second))
	for _, group := range [][]types.BrainEntry{first, second} {
		for _, entry := range group {
			key := entry.Path
			if key == "" {
				key = entry.ID
			}
			if key != "" {
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
			}
			entries = append(entries, entry)
		}
	}
	return entries
}

func toggleAutomationRowCmd(cfg runner.RunnerConfig, row AutomationListRow) tea.Cmd {
	return func() tea.Msg {
		path := row.Path
		if path == "" {
			path = row.ID
		}
		if path == "" {
			return AutomationToggleMsg{RowID: row.ID, Error: fmt.Errorf("automation row has no path or id")}
		}

		updates := map[string]interface{}{}
		switch row.Source {
		case "automation":
			newStatus := "active"
			if row.Enabled || row.Status == "active" {
				newStatus = "archived"
			}
			updates["status"] = newStatus
		case "task":
			updates["schedule_enabled"] = !row.Enabled
		default:
			return AutomationToggleMsg{RowID: row.ID, Error: fmt.Errorf("unknown automation row source %q", row.Source)}
		}

		apiClient := runner.NewAPIClient(cfg)
		_, err := apiClient.UpdateEntry(context.Background(), path, updates)
		return AutomationToggleMsg{RowID: row.ID, Error: err}
	}
}

func deleteAutomationRowCmd(cfg runner.RunnerConfig, row AutomationListRow) tea.Cmd {
	return func() tea.Msg {
		path := row.Path
		if path == "" {
			path = row.ID
		}
		if path == "" {
			return AutomationDeletedMsg{RowID: row.ID, Error: fmt.Errorf("automation row has no path or id")}
		}
		if row.Source != "automation" {
			return AutomationDeletedMsg{RowID: row.ID, Error: fmt.Errorf("delete only supports automation entries")}
		}
		if row.Scope == "global" || row.Scope == "built-in" || strings.HasPrefix(path, "global/") {
			return AutomationDeletedMsg{RowID: row.ID, Error: fmt.Errorf("built-in automations cannot be deleted")}
		}
		if !strings.HasPrefix(path, "projects/") {
			return AutomationDeletedMsg{RowID: row.ID, Error: fmt.Errorf("only project-level automations can be deleted")}
		}

		apiClient := runner.NewAPIClient(cfg)
		err := apiClient.DeleteEntry(context.Background(), path)
		return AutomationDeletedMsg{RowID: row.ID, Error: err}
	}
}

func runAutomationRowCmd(cfg runner.RunnerConfig, row AutomationListRow, activeProject string) tea.Cmd {
	return func() tea.Msg {
		path := row.Path
		if path == "" {
			path = row.ID
		}
		if path == "" {
			return AutomationRunMsg{RowID: row.ID, Error: fmt.Errorf("automation row has no path or id")}
		}
		if row.Source != "automation" {
			return AutomationRunMsg{RowID: row.ID, Error: fmt.Errorf("manual run only supports automation entries")}
		}

		apiClient := runner.NewAPIClient(cfg)
		entry, err := apiClient.GetEntry(context.Background(), path)
		if err != nil {
			return AutomationRunMsg{RowID: row.ID, Error: err}
		}
		if entry.Action == nil {
			return AutomationRunMsg{RowID: row.ID, Error: fmt.Errorf("automation has no action")}
		}

		project := entry.ProjectID
		if project == "" {
			project = activeProject
		}
		if project == "" || project == "all" {
			return AutomationRunMsg{RowID: row.ID, Error: fmt.Errorf("automation has no project")}
		}

		generated := true
		prompt := interpolateAutomationManualPrompt(entry.Action.DirectPrompt, project)
		if entry.Action.Type == "script" {
			prompt = interpolateAutomationManualPrompt(entry.Action.Command, project)
		}
		if prompt == "" {
			return AutomationRunMsg{RowID: row.ID, Error: fmt.Errorf("automation action has no prompt or command")}
		}

		completeOnIdle := automationManualCompleteOnIdle(entry.Action.CompleteOnIdle)
		agent := firstNonEmpty(entry.Agent, entry.Action.Agent)
		model := firstNonEmpty(entry.Model, entry.Action.Model)
		executor := firstNonEmpty(entry.Executor, entry.Action.Executor)
		executionMode := firstNonEmpty(entry.ExecutionMode, entry.Action.ExecutionMode)
		targetWorkdir := firstNonEmpty(entry.TargetWorkdir, entry.Action.TargetWorkdir)
		req := types.CreateEntryRequest{
			Type:           "task",
			Title:          fmt.Sprintf("Automation: %s", entry.ID),
			Content:        prompt,
			Status:         "pending",
			Project:        project,
			Generated:      &generated,
			GeneratedBy:    fmt.Sprintf("automation:%s", entry.ID),
			GeneratedKey:   fmt.Sprintf("automation:manual:%s:%d", entry.ID, time.Now().UTC().UnixNano()),
			DirectPrompt:   prompt,
			Agent:          agent,
			Model:          model,
			Executor:       executor,
			ExecutionMode:  executionMode,
			CompleteOnIdle: completeOnIdle,
			TargetWorkdir:  targetWorkdir,
		}
		if entry.Action.Type == "script" {
			req.Executor = "script"
		}

		created, err := apiClient.CreateEntry(context.Background(), req)
		if err != nil {
			return AutomationRunMsg{RowID: row.ID, Error: err}
		}
		return AutomationRunMsg{RowID: row.ID, TaskID: created.ID}
	}
}

func automationManualCompleteOnIdle(value *bool) *bool {
	if value != nil {
		return value
	}
	defaultValue := true
	return &defaultValue
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func interpolateAutomationManualPrompt(prompt, project string) string {
	if prompt == "" {
		return ""
	}
	tmpl, err := template.New("automation-manual-run").Option("missingkey=error").Parse(prompt)
	if err != nil {
		return prompt
	}
	data := struct {
		Project   string
		ProjectID string
	}{
		Project:   project,
		ProjectID: project,
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return prompt
	}
	return buf.String()
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

// fetchRunnerListCmd fetches the list of registered runners from the API.
func fetchRunnerListCmd(cfg runner.RunnerConfig) tea.Cmd {
	return func() tea.Msg {
		apiClient := runner.NewAPIClient(cfg)
		ctx := context.Background()
		resp, err := apiClient.ListRunners(ctx)
		if err != nil {
			return RunnerListMsg{Err: err}
		}
		return RunnerListMsg{Runners: resp.Runners}
	}
}

// shutdownRunnerCmd requests graceful shutdown for a registered runner.
func shutdownRunnerCmd(cfg runner.RunnerConfig, runnerID, reason string) tea.Cmd {
	return func() tea.Msg {
		apiClient := runner.NewAPIClient(cfg)
		ctx := context.Background()
		err := apiClient.ShutdownRunner(ctx, runnerID, reason)
		return runnerShutdownRequestedMsg{runnerID: runnerID, err: err}
	}
}

// pauseAutomationsCmd toggles pause/resume for automation-generated tasks.
func pauseAutomationsCmd(cfg runner.RunnerConfig, currentlyPaused bool, rc RunnerController) tea.Cmd {
	return func() tea.Msg {
		if rc != nil {
			if currentlyPaused {
				rc.ResumeAutomations()
			} else {
				rc.PauseAutomations()
			}
			return automationsPauseToggledMsg{paused: !currentlyPaused, err: nil}
		}
		apiClient := runner.NewAPIClient(cfg)
		ctx := context.Background()
		var err error
		if currentlyPaused {
			err = apiClient.ResumeAutomations(ctx)
		} else {
			err = apiClient.PauseAutomations(ctx)
		}
		return automationsPauseToggledMsg{paused: !currentlyPaused, err: err}
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
		return runnerStatusMsg{paused: status.Paused, pausedProjects: status.PausedProjects, automationsPaused: status.AutomationsPaused}
	}
}

// fetchAPIHealthCmd fetches server and embedding readiness for the status bar.
func fetchAPIHealthCmd(cfg runner.RunnerConfig) tea.Cmd {
	return func() tea.Msg {
		apiClient := runner.NewAPIClient(cfg)
		health, err := apiClient.CheckHealth(context.Background())
		return apiHealthMsg{health: health, err: err}
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

// computeRunnerMetrics calculates aggregate runner statistics from a list of runners.
func computeRunnerMetrics(runners []types.RunnerInfo) *RunnerMetrics {
	if len(runners) == 0 {
		return nil
	}

	metrics := &RunnerMetrics{
		TotalRunners: len(runners),
	}

	for _, r := range runners {
		switch r.Status {
		case types.RunnerStatusOnline:
			metrics.OnlineRunners++
		case types.RunnerStatusStale:
			metrics.StaleRunners++
		}
	}

	return metrics
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

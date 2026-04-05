// Package runner implements the brain task runner that processes tasks
// from the Brain API using OpenCode.
package runner

import "time"

// =============================================================================
// Configuration Types
// =============================================================================

// RunnerConfig holds all configuration for the brain task runner.
type RunnerConfig struct {
	BrainAPIURL            string         `yaml:"brain_api_url" json:"brain_api_url"`
	APIToken               string         `yaml:"api_token" json:"api_token"`
	PollInterval           int            `yaml:"poll_interval" json:"poll_interval"`           // seconds
	TaskPollInterval       int            `yaml:"task_poll_interval" json:"task_poll_interval"` // seconds
	MaxParallel            int            `yaml:"max_parallel" json:"max_parallel"`
	StateDir               string         `yaml:"state_dir" json:"state_dir"`
	LogDir                 string         `yaml:"log_dir" json:"log_dir"`
	WorkDir                string         `yaml:"work_dir" json:"work_dir"`
	APITimeout             int            `yaml:"api_timeout" json:"api_timeout"`                           // ms
	TaskTimeout            int            `yaml:"task_timeout" json:"task_timeout"`                         // ms
	IdleDetectionThreshold int            `yaml:"idle_detection_threshold" json:"idle_detection_threshold"` // ms
	MaxTotalProcesses      int            `yaml:"max_total_processes" json:"max_total_processes"`
	MemoryThresholdPercent int            `yaml:"memory_threshold_percent" json:"memory_threshold_percent"`
	Opencode               OpencodeConfig `yaml:"opencode" json:"opencode"`
	Pi                     PiConfig       `yaml:"pi" json:"pi"`
	Executors              []string       `yaml:"executors" json:"executors"`
	ExcludeProjects        []string       `yaml:"exclude_projects" json:"exclude_projects"`
	AutoMonitors           bool           `yaml:"auto_monitors" json:"auto_monitors"`

	// EnvPassthrough is a list of environment variable names to forward
	// from the runner process to spawned OpenCode agents.
	// Defaults: ["BRAIN_API_URL", "BRAIN_API_TOKEN"]
	// Values are read from the runner's own environment at spawn time.
	EnvPassthrough []string `yaml:"env_passthrough" json:"env_passthrough"`

	// FeatureIDs constrains the runner to only execute tasks from specific features.
	// When empty, all features are eligible. Supports multiple feature IDs.
	// Set via --feature-id CLI flag or RUNNER_FEATURE_IDS env var (comma-separated).
	FeatureIDs []string `yaml:"feature_ids" json:"feature_ids"`

	// HeartbeatInterval is how often (in seconds) the runner sends heartbeats
	// to the Brain API. Default: 30s. Set via RUNNER_HEARTBEAT_INTERVAL env var.
	HeartbeatInterval int `yaml:"heartbeat_interval" json:"heartbeat_interval"`

	// LogStreaming enables runner-side log streaming. When true, the runner
	// captures executor stdout/stderr and POSTs batches to the Brain API.
	// Default: true. Set via RUNNER_LOG_STREAMING env var.
	LogStreaming bool `yaml:"log_streaming" json:"log_streaming"`
}

// OpencodeConfig holds configuration for the OpenCode executor.
type OpencodeConfig struct {
	Bin   string `yaml:"bin" json:"bin"`
	Agent string `yaml:"agent" json:"agent"`
	Model string `yaml:"model" json:"model"`
}

// PiConfig holds configuration for the Pi executor.
type PiConfig struct {
	Bin   string `yaml:"bin" json:"bin"`
	Model string `yaml:"model" json:"model"`
}

// =============================================================================
// Execution Types
// =============================================================================

// ExecutionMode describes how the runner spawns tasks.
type ExecutionMode string

const (
	ExecutionModeTUI       ExecutionMode = "tui"
	ExecutionModeDashboard ExecutionMode = "dashboard"
	ExecutionModeHeadless  ExecutionMode = "headless"
)

// RunningTask represents a task currently being executed by the runner.
type RunningTask struct {
	ID              string    `json:"id"`
	Path            string    `json:"path"`
	Title           string    `json:"title"`
	Priority        string    `json:"priority"`
	ProjectID       string    `json:"projectId"`
	PID             int       `json:"pid"`
	PaneID          string    `json:"paneId,omitempty"`
	WindowName      string    `json:"windowName,omitempty"`
	StartedAt       time.Time `json:"startedAt"`
	IsResume        bool      `json:"isResume"`
	Workdir         string    `json:"workdir"`
	OpencodePort    int       `json:"opencodePort,omitempty"`
	SessionID       string    `json:"sessionId,omitempty"`
	IdleSince       string    `json:"idleSince,omitempty"` // ISO timestamp
	CompleteOnIdle  bool      `json:"completeOnIdle,omitempty"`
	ScheduledTaskID string    `json:"scheduledTaskId,omitempty"`
	RunID           string    `json:"runId,omitempty"`
}

// TaskResultStatus enumerates possible outcomes of a task execution.
type TaskResultStatus string

const (
	TaskResultCompleted TaskResultStatus = "completed"
	TaskResultFailed    TaskResultStatus = "failed"
	TaskResultBlocked   TaskResultStatus = "blocked"
	TaskResultCancelled TaskResultStatus = "cancelled"
	TaskResultTimeout   TaskResultStatus = "timeout"
	TaskResultCrashed   TaskResultStatus = "crashed"
)

// TaskResult records the outcome of a completed task execution.
type TaskResult struct {
	TaskID          string           `json:"taskId"`
	Status          TaskResultStatus `json:"status"`
	StartedAt       time.Time        `json:"startedAt"`
	CompletedAt     time.Time        `json:"completedAt"`
	Duration        int64            `json:"duration"` // ms
	ExitCode        *int             `json:"exitCode,omitempty"`
	ScheduledTaskID string           `json:"scheduledTaskId,omitempty"`
}

// =============================================================================
// State Types
// =============================================================================

// RunnerStatus describes the current state of the runner.
type RunnerStatus string

const (
	RunnerStatusIdle       RunnerStatus = "idle"
	RunnerStatusPolling    RunnerStatus = "polling"
	RunnerStatusProcessing RunnerStatus = "processing"
	RunnerStatusStopped    RunnerStatus = "stopped"
)

// RunnerStats tracks aggregate execution statistics.
type RunnerStats struct {
	Completed    int   `json:"completed"`
	Failed       int   `json:"failed"`
	TotalRuntime int64 `json:"totalRuntime"` // ms
}

// RunnerState represents the persisted state of the runner.
type RunnerState struct {
	ProjectID    string        `json:"projectId"`
	Status       RunnerStatus  `json:"status"`
	StartedAt    time.Time     `json:"startedAt"`
	UpdatedAt    time.Time     `json:"updatedAt"`
	RunningTasks []RunningTask `json:"runningTasks"`
	Stats        RunnerStats   `json:"stats"`
	Config       RunnerConfig  `json:"config"`
}

// =============================================================================
// Event Types
// =============================================================================

// RunnerEventType enumerates the kinds of events the runner can emit.
type RunnerEventType string

const (
	EventTaskStarted       RunnerEventType = "task_started"
	EventTaskCompleted     RunnerEventType = "task_completed"
	EventTaskFailed        RunnerEventType = "task_failed"
	EventTaskCancelled     RunnerEventType = "task_cancelled"
	EventPollComplete      RunnerEventType = "poll_complete"
	EventStateSaved        RunnerEventType = "state_saved"
	EventShutdown          RunnerEventType = "shutdown"
	EventProjectPaused     RunnerEventType = "project_paused"
	EventProjectResumed    RunnerEventType = "project_resumed"
	EventAllPaused         RunnerEventType = "all_paused"
	EventAllResumed        RunnerEventType = "all_resumed"
	EventFeatureEnabled    RunnerEventType = "feature_enabled"
	EventFeatureDisabled   RunnerEventType = "feature_disabled"
	EventSessionDiscovered RunnerEventType = "session_discovered"
	EventTaskClaimed       RunnerEventType = "task_claimed"
	EventTaskClaimRejected RunnerEventType = "task_claim_rejected"
	EventTaskStatusChanged RunnerEventType = "task_status_changed"
	EventTaskReleased      RunnerEventType = "task_released"
	EventRunnerStarted     RunnerEventType = "runner_started"
)

// RunnerEvent is a discriminated event emitted by the runner.
type RunnerEvent struct {
	Type RunnerEventType `json:"type"`

	// Populated on ALL events by emitEvent().
	RunnerID string `json:"runnerId,omitempty"`

	// Populated for task_started events.
	Task *RunningTask `json:"task,omitempty"`

	// Populated for task_completed and task_failed events.
	Result *TaskResult `json:"result,omitempty"`

	// Populated for task_cancelled events.
	TaskID   string `json:"taskId,omitempty"`
	TaskPath string `json:"taskPath,omitempty"`

	// Populated for poll_complete events.
	ReadyCount   int `json:"readyCount,omitempty"`
	RunningCount int `json:"runningCount,omitempty"`

	// Populated for state_saved events.
	Path string `json:"path,omitempty"`

	// Populated for shutdown events.
	Reason string `json:"reason,omitempty"`

	// Populated for project_paused/resumed events.
	ProjectID string `json:"projectId,omitempty"`

	// Populated for feature_enabled/disabled events.
	FeatureID string `json:"featureId,omitempty"`

	// Populated for session_discovered events.
	SessionID string `json:"sessionId,omitempty"`

	// Populated for task_status_changed events.
	FromStatus string `json:"fromStatus,omitempty"`
	ToStatus   string `json:"toStatus,omitempty"`

	// Populated for task_claim_rejected events.
	ClaimedBy string `json:"claimedBy,omitempty"`

	// Populated for runner_started events.
	Projects []string `json:"projects,omitempty"`
	Mode     string   `json:"mode,omitempty"`
}

// EventHandler is a callback for runner events.
type EventHandler func(event RunnerEvent)

// =============================================================================
// API Response Types (used by client)
// =============================================================================

// APIHealth represents the health status of the Brain API.
type APIHealth struct {
	Status      string `json:"status"`
	ZKAvailable bool   `json:"zkAvailable"`
	DBAvailable bool   `json:"dbAvailable"`
}

// ClaimResult represents the outcome of a task claim attempt.
type ClaimResult struct {
	Success   bool   `json:"success"`
	TaskID    string `json:"taskId"`
	ClaimedBy string `json:"claimedBy,omitempty"`
	Message   string `json:"message,omitempty"`
}

// =============================================================================
// Runner Command Types (SSE command channel)
// =============================================================================

// RunnerCommandType enumerates the kinds of commands the server can push to a runner.
type RunnerCommandType string

const (
	// CommandAffinityUpdated signals the runner to update its FeatureIDs.
	CommandAffinityUpdated RunnerCommandType = "affinity_updated"

	// CommandConfigUpdated signals the runner to update maxParallel, model, agent.
	CommandConfigUpdated RunnerCommandType = "config_updated"

	// CommandDispatch signals the runner to immediately wake for targeted task pickup.
	CommandDispatch RunnerCommandType = "dispatch"

	// CommandShutdown signals the runner to initiate graceful shutdown.
	CommandShutdown RunnerCommandType = "shutdown"
)

// RunnerCommand represents a server-pushed command received via the runner SSE stream.
type RunnerCommand struct {
	Type RunnerCommandType `json:"type"`

	// Populated for affinity_updated commands.
	FeatureIDs []string `json:"featureIds,omitempty"`

	// Populated for config_updated commands.
	MaxParallel *int   `json:"maxParallel,omitempty"`
	Model       string `json:"model,omitempty"`
	Agent       string `json:"agent,omitempty"`

	// Populated for dispatch commands.
	TaskID    string `json:"taskId,omitempty"`
	ProjectID string `json:"projectId,omitempty"`

	// Populated for shutdown commands.
	Reason string `json:"reason,omitempty"`
}

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
	ExcludeProjects        []string       `yaml:"exclude_projects" json:"exclude_projects"`
	AutoMonitors           bool           `yaml:"auto_monitors" json:"auto_monitors"`
}

// OpencodeConfig holds configuration for the OpenCode executor.
type OpencodeConfig struct {
	Bin   string `yaml:"bin" json:"bin"`
	Agent string `yaml:"agent" json:"agent"`
	Model string `yaml:"model" json:"model"`
}

// =============================================================================
// Execution Types
// =============================================================================

// ExecutionMode describes how the runner spawns tasks.
type ExecutionMode string

const (
	ExecutionModeTUI        ExecutionMode = "tui"
	ExecutionModeDashboard  ExecutionMode = "dashboard"
	ExecutionModeBackground ExecutionMode = "background"
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
	EventTaskStarted     RunnerEventType = "task_started"
	EventTaskCompleted   RunnerEventType = "task_completed"
	EventTaskFailed      RunnerEventType = "task_failed"
	EventTaskCancelled   RunnerEventType = "task_cancelled"
	EventPollComplete    RunnerEventType = "poll_complete"
	EventStateSaved      RunnerEventType = "state_saved"
	EventShutdown        RunnerEventType = "shutdown"
	EventProjectPaused   RunnerEventType = "project_paused"
	EventProjectResumed  RunnerEventType = "project_resumed"
	EventAllPaused       RunnerEventType = "all_paused"
	EventAllResumed      RunnerEventType = "all_resumed"
	EventFeatureEnabled  RunnerEventType = "feature_enabled"
	EventFeatureDisabled RunnerEventType = "feature_disabled"
)

// RunnerEvent is a discriminated event emitted by the runner.
type RunnerEvent struct {
	Type RunnerEventType `json:"type"`

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

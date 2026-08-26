// Package runner implements the brain task runner that processes tasks
// from the Brain API using OpenCode.
package runner

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Configuration Types
// =============================================================================

// RunnerConfig holds all configuration for the brain task runner.
type RunnerConfig struct {
	BrainAPIURL               string             `yaml:"brain_api_url" json:"brain_api_url"`
	APIToken                  string             `yaml:"api_token" json:"api_token"`
	APITokenEnv               string             `yaml:"api_token_env" json:"api_token_env"`
	PollInterval              int                `yaml:"poll_interval" json:"poll_interval"`           // seconds
	TaskPollInterval          int                `yaml:"task_poll_interval" json:"task_poll_interval"` // seconds
	MaxParallel               int                `yaml:"max_parallel" json:"max_parallel"`
	StateDir                  string             `yaml:"state_dir" json:"state_dir"`
	LogDir                    string             `yaml:"log_dir" json:"log_dir"`
	WorkDir                   string             `yaml:"work_dir" json:"work_dir"`
	RepoCacheDir              string             `yaml:"repo_cache_dir" json:"repo_cache_dir"`
	GitToken                  string             `yaml:"git_token" json:"git_token"`
	GitTokenEnv               string             `yaml:"git_token_env" json:"git_token_env"`
	RequireHTTPS              bool               `yaml:"require_https" json:"require_https"`
	AllowUnauthenticatedHTTPS bool               `yaml:"allow_unauthenticated_https" json:"allow_unauthenticated_https"`
	APITimeout                int                `yaml:"api_timeout" json:"api_timeout"`                           // ms
	TaskTimeout               int                `yaml:"task_timeout" json:"task_timeout"`                         // ms
	IdleDetectionThreshold    int                `yaml:"idle_detection_threshold" json:"idle_detection_threshold"` // ms
	MaxTotalProcesses         int                `yaml:"max_total_processes" json:"max_total_processes"`
	MemoryThresholdPercent    int                `yaml:"memory_threshold_percent" json:"memory_threshold_percent"`
	Opencode                  OpencodeConfig     `yaml:"opencode" json:"opencode"`
	Script                    ScriptConfig       `yaml:"script" json:"script"`
	Pi                        PiConfig           `yaml:"pi" json:"pi"`
	Executors                 []string           `yaml:"executors" json:"executors"`
	DefaultExecutor           string             `yaml:"default_executor" json:"default_executor"`
	TaskDefaults              TaskDefaultsConfig `yaml:"task_defaults" json:"task_defaults"`
	ExcludeProjects           []string           `yaml:"exclude_projects" json:"exclude_projects"`
	IncludeProjects           []string           `yaml:"include_projects" json:"include_projects"`
	AutoMonitors              bool               `yaml:"auto_monitors" json:"auto_monitors"`

	// EnvPassthrough is a list of environment variable names to forward
	// from the runner process to spawned OpenCode agents.
	// Defaults: ["BRAIN_API_URL", "BRAIN_API_TOKEN"]
	// Values are read from the runner's own environment at spawn time.
	EnvPassthrough []string `yaml:"env_passthrough" json:"env_passthrough"`

	// FeatureIDs constrains the runner to only execute tasks from specific features.
	// When empty, all features are eligible. Supports multiple feature IDs.
	// Set via --feature-id CLI flag or RUNNER_FEATURE_IDS env var (comma-separated).
	FeatureIDs []string `yaml:"feature_ids" json:"feature_ids"`

	// Hooks holds the event hook system configuration, including hooks directory
	// and inline hook definitions.
	Hooks HooksConfig `yaml:"hooks" json:"hooks"`

	// HooksDir is the directory containing hook scripts (pre-*/post-* executables).
	// Default: ~/.config/brain/hooks
	// Deprecated: Use Hooks.HooksDir instead. Kept for backward compatibility.
	HooksDir string `yaml:"hooks_dir" json:"hooks_dir"`

	// HookTimeout is the maximum duration in seconds for pre-hook execution.
	// Post-hooks are fire-and-forget and not subject to this timeout.
	// Default: 30
	// Deprecated: Use per-hook timeout in Hooks.Hooks[name].Timeout instead.
	HookTimeout int `yaml:"hook_timeout" json:"hook_timeout"`

	// HeartbeatInterval is how often (in seconds) the runner sends heartbeats
	// to the Brain API. Default: 30s. Set via RUNNER_HEARTBEAT_INTERVAL env var.
	HeartbeatInterval int `yaml:"heartbeat_interval" json:"heartbeat_interval"`

	// ProjectRefreshInterval is how often (in seconds) the runner refreshes its
	// project list from the Brain API by calling ListProjects and applying the
	// include/exclude filters. Projects created after the runner started become
	// visible without a restart. Default: 60s. Set via
	// RUNNER_PROJECT_REFRESH_INTERVAL env var. Set to 0 to disable refresh
	// (list is frozen at startup — legacy behaviour).
	ProjectRefreshInterval int `yaml:"project_refresh_interval" json:"project_refresh_interval"`

	// LogStreaming enables runner-side log streaming. When true, the runner
	// captures executor stdout/stderr and POSTs batches to the Brain API.
	// Default: true. Set via RUNNER_LOG_STREAMING env var.
	LogStreaming bool `yaml:"log_streaming" json:"log_streaming"`

	// Capabilities declares what this runner can do. Tasks with requires_capability
	// tags are only claimable by runners whose capabilities include all required values.
	// Untagged tasks are claimable by any runner (backward compatible).
	// Set via config file or RUNNER_CAPABILITIES env var (comma-separated).
	Capabilities []string `yaml:"capabilities" json:"capabilities"`

	// DispatchPush advertises that this runner accepts Brain-assigned dispatch
	// leases instead of relying on active /next polling. Set via
	// RUNNER_DISPATCH_PUSH env var.
	DispatchPush bool `yaml:"dispatch_push" json:"dispatch_push"`

	// Labels and resource metadata are advertised to the scheduler during
	// registration and heartbeat so placement can target suitable runners.
	Labels         map[string]string      `yaml:"labels" json:"labels"`
	WorkspaceRoots []string               `yaml:"workspace_roots" json:"workspace_roots"`
	Resources      map[string]interface{} `yaml:"resources" json:"resources"`
	Capacity       map[string]interface{} `yaml:"capacity" json:"capacity"`
	Draining       bool                   `yaml:"draining" json:"draining"`

	// Passive prevents active /next polling. Passive runners only execute
	// Brain-assigned dispatch leases. Set via RUNNER_PASSIVE env var.
	Passive bool `yaml:"passive" json:"passive"`

	// Control holds remote-control bridge settings (WebSocket tunnel to the
	// Brain API for proxied OpenCode access and ad-hoc spawning).
	Control ControlConfig `yaml:"control" json:"control"`
}

// ControlConfig holds remote-control bridge settings.
type ControlConfig struct {
	// Disabled turns off the bridge connection. The bridge is enabled by
	// default whenever a Brain API URL is configured.
	Disabled bool `yaml:"disabled" json:"disabled"`

	// AllowedWorkdirRoots restricts where ad-hoc OpenCode instances may be
	// spawned. Defaults to the user's home directory when empty.
	AllowedWorkdirRoots []string `yaml:"allowed_workdir_roots" json:"allowed_workdir_roots"`
}

// OpencodeConfig holds configuration for the OpenCode executor.
type OpencodeConfig struct {
	Bin   string `yaml:"bin" json:"bin"`
	Agent string `yaml:"agent" json:"agent"`
	Model string `yaml:"model" json:"model"`
}

// HooksConfig holds configuration for the event hook system.
// It supports both directory-based hook discovery and inline hook definitions.
type HooksConfig struct {
	// HooksDir is the directory containing hook scripts (pre-*/post-* executables).
	// Default: ~/.config/brain/hooks
	HooksDir string `yaml:"hooks_dir" json:"hooks_dir"`

	// Inline hook definitions keyed by hook name (e.g., "post-task-blocked").
	// YAML config inline hooks take precedence over directory scripts.
	Hooks map[string]InlineHookConfig `yaml:"hooks" json:"hooks"`
}

// InlineHookConfig defines an inline hook from YAML configuration.
// Either Command or Script must be set, but not both.
type InlineHookConfig struct {
	// Command is a shell command string to execute (run via "sh -c").
	Command string `yaml:"command" json:"command"`
	// Script is the path to an executable script file.
	Script string `yaml:"script" json:"script"`
	// Timeout is the maximum duration for hook execution.
	// Defaults to 30s for pre-hooks, 10s for post-hooks.
	Timeout Duration `yaml:"timeout" json:"timeout"`
	// Blocking controls whether a pre-hook blocks the action on failure.
	// Only meaningful for pre-hooks. Default: true for pre-hooks.
	Blocking *bool `yaml:"blocking" json:"blocking"`
	// Enabled controls whether this hook is active. Default: true.
	Enabled *bool `yaml:"enabled" json:"enabled"`
}

// Duration is a time.Duration that supports YAML unmarshaling from strings like "10s", "5m".
type Duration struct {
	time.Duration
}

// UnmarshalYAML implements yaml.Unmarshaler for Duration.
func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		// Try as integer (seconds).
		var secs int
		if err2 := unmarshal(&secs); err2 != nil {
			return err
		}
		d.Duration = time.Duration(secs) * time.Second
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

// MarshalYAML implements yaml.Marshaler for Duration.
func (d Duration) MarshalYAML() (interface{}, error) {
	return d.Duration.String(), nil
}

// IsEnabled returns whether the inline hook is enabled (defaults to true).
func (h InlineHookConfig) IsEnabled() bool {
	if h.Enabled == nil {
		return true
	}
	return *h.Enabled
}

// IsBlocking returns whether the inline hook is blocking (defaults to true for pre-hooks).
func (h InlineHookConfig) IsBlocking() bool {
	if h.Blocking == nil {
		return true
	}
	return *h.Blocking
}

// GetTimeout returns the configured timeout, or the given default if not set.
func (h InlineHookConfig) GetTimeout(defaultTimeout time.Duration) time.Duration {
	if h.Timeout.Duration > 0 {
		return h.Timeout.Duration
	}
	return defaultTimeout
}

// PiConfig holds configuration for the Pi executor.
type PiConfig struct {
	Bin           string   `yaml:"bin" json:"bin"`
	Model         string   `yaml:"model" json:"model"`
	Thinking      string   `yaml:"thinking" json:"thinking"`
	AgentsDir     string   `yaml:"agents_dir" json:"agents_dir"`
	ExtensionsDir string   `yaml:"extensions_dir" json:"extensions_dir"`
	Extensions    []string `yaml:"extensions" json:"extensions"`
	NoSession     bool     `yaml:"no_session" json:"no_session"`
}

// TaskDefaultsConfig holds default values applied to tasks when not overridden
// by per-task metadata.
type TaskDefaultsConfig struct {
	Agent              string   `yaml:"agent" json:"agent"`
	Model              string   `yaml:"model" json:"model"`
	Executor           string   `yaml:"executor" json:"executor"`
	Extensions         []string `yaml:"extensions" json:"extensions"`
	ExecutionMode      string   `yaml:"execution_mode" json:"execution_mode"`
	CompleteOnIdle     *bool    `yaml:"complete_on_idle" json:"complete_on_idle"`
	MergePolicy        string   `yaml:"merge_policy" json:"merge_policy"`
	MergeStrategy      string   `yaml:"merge_strategy" json:"merge_strategy"`
	MergeTargetBranch  string   `yaml:"merge_target_branch" json:"merge_target_branch"`
	RemoteBranchPolicy string   `yaml:"remote_branch_policy" json:"remote_branch_policy"`
	OpenPRBeforeMerge  *bool    `yaml:"open_pr_before_merge" json:"open_pr_before_merge"`
	TargetWorkdir      string   `yaml:"target_workdir" json:"target_workdir"`
}

// ScriptConfig holds configuration for the script executor.
// Scripts are disabled by default and must be explicitly enabled.
type ScriptConfig struct {
	Enabled         bool     `yaml:"enabled" json:"enabled"`
	AllowedCommands []string `yaml:"allowed_commands" json:"allowed_commands"`
	BlockedCommands []string `yaml:"blocked_commands" json:"blocked_commands"`
	MaxTimeout      int      `yaml:"max_timeout" json:"max_timeout"`
	WorkdirRestrict []string `yaml:"workdir_restrict" json:"workdir_restrict"`
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
	ExecutorType    string    `json:"executorType,omitempty"` // "opencode", "pi", or "script"
	Executor        string    `json:"executor,omitempty"`
	Agent           string    `json:"agent,omitempty"`
	Model           string    `json:"model,omitempty"`
	InstanceID      string    `json:"instanceId,omitempty"`
	OpencodePort    int       `json:"opencodePort,omitempty"`
	SessionID       string    `json:"sessionId,omitempty"`
	IdleSince       string    `json:"idleSince,omitempty"` // ISO timestamp
	CompleteOnIdle  bool      `json:"completeOnIdle,omitempty"`
	ScheduledTaskID string    `json:"scheduledTaskId,omitempty"`
	RunID           string    `json:"runId,omitempty"`
	FeatureID       string    `json:"featureId,omitempty"`
	GeneratedBy     string    `json:"generatedBy,omitempty"`
	// BusyHoldSince marks when the run process was first observed exited
	// while the attached OpenCode session still reported busy — a steered
	// (injected) turn finishing its work on the serve process. Completion
	// is held until the session idles or the hold window lapses.
	BusyHoldSince time.Time `json:"busyHoldSince,omitempty"`
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
	EventSessionDiscovered RunnerEventType = "session_discovered"
	EventTaskClaimed       RunnerEventType = "task_claimed"
	EventTaskClaimRejected RunnerEventType = "task_claim_rejected"
	EventTaskStatusChanged RunnerEventType = "task_status_changed"
	EventTaskReleased      RunnerEventType = "task_released"
	EventRunnerStarted     RunnerEventType = "runner_started"

	// Feature lifecycle events (emitted by FeatureTracker).
	EventFeatureStarted   RunnerEventType = "feature_started"
	EventFeatureCompleted RunnerEventType = "feature_completed"
	EventFeatureBlocked   RunnerEventType = "feature_blocked"
	EventFeatureProgress  RunnerEventType = "feature_progress"
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

	// Populated for task and feature lifecycle events.
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
	Status      string             `json:"status"`
	ZKAvailable bool               `json:"zkAvailable"`
	DBAvailable bool               `json:"dbAvailable"`
	Embedding   APIEmbeddingHealth `json:"embedding"`
}

// APIEmbeddingHealth represents embedding model availability from /health.
type APIEmbeddingHealth struct {
	Enabled  bool   `json:"enabled"`
	Status   string `json:"status"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
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

	// CommandPause signals the runner to pause all task claims.
	CommandPause RunnerCommandType = "pause"

	// CommandResume signals the runner to resume task claims.
	CommandResume RunnerCommandType = "resume"

	// CommandShutdown signals the runner to initiate graceful shutdown.
	CommandShutdown RunnerCommandType = "shutdown"
)

// Pause command scopes carried on CommandPause/CommandResume payloads.
//
// The project scopes below are broadcast by the per-project pause dials and
// are reconciled against GetRunnerStatus. PauseScopeRunner is the
// runner-scoped dial (PUT /runners/{runnerId}/pause) and is reconciled
// against the runner's own registry row instead — see TaskRunner.runnerPaused
// for why conflating the two silently un-paused runners.
const (
	// PauseScopeTasks affects a project's non-automation tasks.
	PauseScopeTasks = "tasks"

	// PauseScopeAutomations affects a project's automation-generated tasks.
	PauseScopeAutomations = "automations"

	// PauseScopeAll affects both project dials.
	PauseScopeAll = "all"

	// PauseScopeRunner affects this runner as a whole, regardless of project.
	PauseScopeRunner = "runner"
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
	LeaseID   string `json:"leaseId,omitempty"`
	Lease     string `json:"-"`
	ExpiresAt string `json:"expiresAt,omitempty"`

	// Task is the fully-resolved task inlined into the dispatch payload
	// by the scheduler. When present, the runner can skip its historical
	// GetReadyTasks HTTP round-trip in handleDispatchCommand, which was
	// a common source of task_lookup_failed rejections under API load.
	// May be nil for backwards compatibility with older API servers —
	// the runner falls back to fetching in that case. See
	// SchedulerService.ScheduleProject.
	Task *types.ResolvedTask `json:"task,omitempty"`
	// Force=true on a dispatch command tells the runner to bypass its local
	// pause/capacity gates. Set by the scheduler when the user explicitly
	// requested a run via /run?force=true (e.g. the PWA's "Force" toast
	// action). Capacity is still respected when there is genuinely no slot —
	// but pause is treated as an override hint, matching the documented
	// contract in SchedulerService.RunTaskNow.
	Force bool `json:"force,omitempty"`

	// Populated for pause/resume commands. Empty ProjectID means the
	// command applies globally (matches legacy runner-scoped pause);
	// non-empty ProjectID targets that specific project. Scope
	// distinguishes tasks vs automations for the pause/resume gate:
	//   "" or "all" → both tasks and automations
	//   "tasks"     → only the task-pause dial (dispatch gate)
	//   "automations" → only the automation-pause dial (carve-out)
	// See runner.go pause/resume handler.
	Scope string `json:"scope,omitempty"`

	// Populated for shutdown commands.
	Reason string `json:"reason,omitempty"`
}

// UnmarshalJSON preserves compatibility with scheduler dispatch payloads that
// may provide either leaseId directly or a lease object with an id field.
func (c *RunnerCommand) UnmarshalJSON(data []byte) error {
	type alias RunnerCommand
	var raw struct {
		*alias
		Lease     json.RawMessage `json:"lease"`
		ExpiresAt json.RawMessage `json:"expiresAt"`
	}
	raw.alias = (*alias)(c)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw.Lease) > 0 && string(raw.Lease) != "null" {
		var leaseID string
		if err := json.Unmarshal(raw.Lease, &leaseID); err == nil {
			c.Lease = leaseID
			if c.LeaseID == "" {
				c.LeaseID = leaseID
			}
		} else {
			var lease struct {
				ID        string `json:"id"`
				LeaseID   string `json:"leaseId"`
				LeaseIDUS string `json:"lease_id"`
				ExpiresAt int64  `json:"expires_at"`
			}
			if err := json.Unmarshal(raw.Lease, &lease); err != nil {
				return err
			}
			if lease.ID != "" {
				c.Lease = lease.ID
				if c.LeaseID == "" {
					c.LeaseID = lease.ID
				}
			} else if lease.LeaseID != "" {
				c.Lease = lease.LeaseID
				if c.LeaseID == "" {
					c.LeaseID = lease.LeaseID
				}
			} else if lease.LeaseIDUS != "" {
				c.Lease = lease.LeaseIDUS
				if c.LeaseID == "" {
					c.LeaseID = lease.LeaseIDUS
				}
			}
			if c.ExpiresAt == "" && lease.ExpiresAt > 0 {
				c.ExpiresAt = strconv.FormatInt(lease.ExpiresAt, 10)
			}
		}
	}
	if len(raw.ExpiresAt) > 0 && string(raw.ExpiresAt) != "null" {
		var expires string
		if err := json.Unmarshal(raw.ExpiresAt, &expires); err == nil {
			c.ExpiresAt = expires
		} else {
			var expiresNum int64
			if err := json.Unmarshal(raw.ExpiresAt, &expiresNum); err != nil {
				return err
			}
			c.ExpiresAt = strconv.FormatInt(expiresNum, 10)
		}
	}
	return nil
}

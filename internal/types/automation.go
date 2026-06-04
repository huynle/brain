package types

// =============================================================================
// Automation Entry Types
// =============================================================================

// AutomationTrigger defines when an automation fires.
type AutomationTrigger struct {
	Type                   string            `json:"type" yaml:"type"`                                                             // cron, event, webhook, session
	Event                  string            `json:"event,omitempty" yaml:"event,omitempty"`                                       // event name for type=event
	Schedule               string            `json:"schedule,omitempty" yaml:"schedule,omitempty"`                                 // cron expression for type=cron
	Filter                 map[string]string `json:"filter,omitempty" yaml:"filter,omitempty"`                                     // key-value filter conditions
	OncePer                string            `json:"once_per,omitempty" yaml:"once_per,omitempty"`                                 // dedup key (e.g. "session", "day")
	Webhook                string            `json:"webhook,omitempty" yaml:"webhook,omitempty"`                                   // webhook path for type=webhook
	IgnoreAutomationEvents *bool             `json:"ignore_automation_events,omitempty" yaml:"ignore_automation_events,omitempty"` // default true; set false to process automation-generated events
}

// AutomationAction defines what an automation does when triggered.
type AutomationAction struct {
	Type               string `json:"type" yaml:"type"`                                                   // prompt, script, update, http
	DirectPrompt       string `json:"direct_prompt,omitempty" yaml:"direct_prompt,omitempty"`             // prompt text for type=prompt
	Command            string `json:"command,omitempty" yaml:"command,omitempty"`                         // shell command for type=script
	Agent              string `json:"agent,omitempty" yaml:"agent,omitempty"`                             // agent to use for type=prompt
	Model              string `json:"model,omitempty" yaml:"model,omitempty"`                             // model override
	ExecutionMode      string `json:"execution_mode,omitempty" yaml:"execution_mode,omitempty"`           // worktree, current_branch
	SessionMode        string `json:"session_mode,omitempty" yaml:"session_mode,omitempty"`               // continue, fresh
	CompleteOnIdle     *bool  `json:"complete_on_idle,omitempty" yaml:"complete_on_idle,omitempty"`       // auto-complete on idle
	Timeout            string `json:"timeout,omitempty" yaml:"timeout,omitempty"`                         // execution timeout (e.g. "5m", "1h")
	RequiresCapability string `json:"requires_capability,omitempty" yaml:"requires_capability,omitempty"` // required runner capability
}

// AutomationRetry defines retry behavior for failed automation actions.
type AutomationRetry struct {
	MaxAttempts int    `json:"max_attempts,omitempty" yaml:"max_attempts,omitempty"` // max retry attempts (default: 0 = no retry)
	Backoff     string `json:"backoff,omitempty" yaml:"backoff,omitempty"`           // fixed, exponential
	Delay       string `json:"delay,omitempty" yaml:"delay,omitempty"`               // delay between retries (e.g. "30s", "5m")
}

// =============================================================================
// Goal Automation Types
// =============================================================================

// Goal trigger sources control which lifecycle events a goal automation
// reacts to. "both" subscribes to task status changes AND feature completion.
const (
	// GoalTriggerSourceTask reacts to task.status_changed events only.
	GoalTriggerSourceTask = "task"
	// GoalTriggerSourceFeature reacts to feature.completed events only.
	GoalTriggerSourceFeature = "feature"
	// GoalTriggerSourceBoth reacts to both task and feature lifecycle events.
	GoalTriggerSourceBoth = "both"
)

// GoalConfig holds the goal-specific configuration carried by a goal
// automation entry. A goal is an automation entry (type=automation,
// generated_by=brain-goal) whose trigger/action drive a deterministic
// in-process reconcile loop. GoalConfig captures the inputs that the
// reconcile core consumes: success criteria, validation rules, the working
// directory for generated work, the trigger source, and the set of task
// statuses that count as "complete" vs. "blocked"/other for state tracking.
type GoalConfig struct {
	// ID is the stable goal identifier (used for tag goal:<id> and dedup).
	ID string `json:"id" yaml:"id"`
	// Criteria describes the success criteria the goal must satisfy.
	Criteria string `json:"criteria,omitempty" yaml:"criteria,omitempty"`
	// Validation describes how completion is validated.
	Validation string `json:"validation,omitempty" yaml:"validation,omitempty"`
	// Workdir is the working directory for goal-generated work.
	Workdir string `json:"workdir,omitempty" yaml:"workdir,omitempty"`
	// TriggerSource controls which events the goal reacts to
	// (task | feature | both). Defaults to both.
	TriggerSource string `json:"trigger_source,omitempty" yaml:"trigger_source,omitempty"`
	// CompleteStatuses are task statuses that count toward goal completion.
	CompleteStatuses []string `json:"complete_statuses,omitempty" yaml:"complete_statuses,omitempty"`
	// BlockedStatuses are task statuses that mark the goal as blocked
	// (tracked separately so the reconcile loop can surface blocked work).
	BlockedStatuses []string `json:"blocked_statuses,omitempty" yaml:"blocked_statuses,omitempty"`
}

// NormalizedTriggerSource returns the effective trigger source, defaulting
// empty/unknown values to GoalTriggerSourceBoth.
func (g *GoalConfig) NormalizedTriggerSource() string {
	if g == nil {
		return GoalTriggerSourceBoth
	}
	switch g.TriggerSource {
	case GoalTriggerSourceTask, GoalTriggerSourceFeature, GoalTriggerSourceBoth:
		return g.TriggerSource
	default:
		return GoalTriggerSourceBoth
	}
}

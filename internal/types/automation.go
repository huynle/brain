package types

import (
	"errors"
	"strings"
)

// Canonical automation action types. Aliases are normalized via
// NormalizeAutomationActionType.
const (
	AutomationActionPrompt = "prompt"
	AutomationActionScript = "script"
	AutomationActionUpdate = "update"
	AutomationActionHTTP   = "http"
)

// NormalizeAutomationActionType returns the canonical action type for the
// given raw value, folding known aliases into their canonical form.
//
// Currently recognized aliases:
//   - "shell" → "script" (undocumented alias used by early automations that
//     wrapped shell commands; kept working for backward compatibility so
//     manual runs and event dispatch treat them the same as scripts).
//
// Unknown values are returned lower-cased and trimmed; empty stays empty
// (callers should apply their own default, typically "prompt").
func NormalizeAutomationActionType(raw string) string {
	t := strings.ToLower(strings.TrimSpace(raw))
	switch t {
	case "shell":
		return AutomationActionScript
	default:
		return t
	}
}

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
	Executor           string `json:"executor,omitempty" yaml:"executor,omitempty"`                       // executor override
	TargetWorkdir      string `json:"target_workdir,omitempty" yaml:"target_workdir,omitempty"`           // workdir override
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
	// TaskID scopes the goal to a single task. When set it takes precedence
	// over the entry's feature scope: linked-task resolution and the trigger
	// filter both narrow to this one task (in any feature).
	TaskID string `json:"task_id,omitempty" yaml:"task_id,omitempty"`
	// CompleteStatuses are task statuses that count toward goal completion.
	CompleteStatuses []string `json:"complete_statuses,omitempty" yaml:"complete_statuses,omitempty"`
	// BlockedStatuses are task statuses that mark the goal as blocked
	// (tracked separately so the reconcile loop can surface blocked work).
	BlockedStatuses []string `json:"blocked_statuses,omitempty" yaml:"blocked_statuses,omitempty"`
	// Steering configures live-session steering: when linked work is
	// in_progress, the reconcile loop nudges the running agent sessions
	// toward the goal instead of idling. Nil means steering enabled with
	// defaults.
	Steering *GoalSteering `json:"steering,omitempty" yaml:"steering,omitempty"`
}

// DefaultGoalSteeringCooldownMinutes is the minimum interval between steering
// prompt injections for a goal when no explicit cooldown is configured.
const DefaultGoalSteeringCooldownMinutes = 15

// GoalSteering configures live-session steering for a goal.
type GoalSteering struct {
	// Enabled toggles steering. Nil defaults to true so goals steer live
	// sessions out of the box; set false to opt out.
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	// CooldownMinutes is the minimum interval between steering injections
	// per goal. 0 defaults to DefaultGoalSteeringCooldownMinutes.
	CooldownMinutes int `json:"cooldown_minutes,omitempty" yaml:"cooldown_minutes,omitempty"`
}

// SteeringEnabled reports whether steering is on for this goal config.
// A nil GoalSteering (or nil Enabled) means enabled.
func (g *GoalConfig) SteeringEnabled() bool {
	if g == nil {
		return false
	}
	if g.Steering == nil || g.Steering.Enabled == nil {
		return true
	}
	return *g.Steering.Enabled
}

// SteeringCooldownMinutes returns the effective steering cooldown in minutes,
// applying the default when unset or non-positive.
func (g *GoalConfig) SteeringCooldownMinutes() int {
	if g == nil || g.Steering == nil || g.Steering.CooldownMinutes <= 0 {
		return DefaultGoalSteeringCooldownMinutes
	}
	return g.Steering.CooldownMinutes
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

// =============================================================================
// Goal API DTOs
//
// These request/response types form the goal API contract. They live in the
// shared types package so both the api and service packages can reference them
// without creating an api<->service import cycle.
// =============================================================================

// ErrGoalNotFound is returned by the goal API when no active goal automation
// matches the requested goal ID.
var ErrGoalNotFound = errors.New("goal not found")

// ReconcileDecision is the outcome of the deterministic reconcile decision.
type ReconcileDecision string

const (
	// ReconcileComplete means every linked task counts as complete.
	ReconcileComplete ReconcileDecision = "complete"
	// ReconcileBlock means linked work is blocked with nothing active.
	ReconcileBlock ReconcileDecision = "block"
	// ReconcileNeedWork means more work must be generated.
	ReconcileNeedWork ReconcileDecision = "need_work"
	// ReconcileNoop means work is already in progress; nothing to do.
	ReconcileNoop ReconcileDecision = "noop"
	// ReconcileSteer means work was in progress and the reconcile loop
	// steered the live agent session(s) toward the goal (a noop that
	// additionally injected steering prompts).
	ReconcileSteer ReconcileDecision = "steer"
)

// LinkedTaskSnapshot is a serializable snapshot of a goal's linked task,
// captured for the reconcile audit record and progress reporting.
type LinkedTaskSnapshot struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// GoalReconcileAudit is the auditable record of a single reconcile decision.
type GoalReconcileAudit struct {
	Timestamp       string               `json:"timestamp"` // RFC3339 UTC
	GoalID          string               `json:"goal_id"`
	Project         string               `json:"project,omitempty"`
	FeatureID       string               `json:"feature_id,omitempty"`
	TriggeringEvent string               `json:"triggering_event"` // evt.Type (or "manual")
	EventID         string               `json:"event_id,omitempty"`
	Decision        ReconcileDecision    `json:"decision"`
	Reason          string               `json:"reason"`
	LinkedTasks     []LinkedTaskSnapshot `json:"linked_tasks"`
	GeneratedTaskID string               `json:"generated_task_id,omitempty"`
	// SessionsSteered / SessionsSkipped report steering outcomes when
	// Decision is ReconcileSteer: how many in-progress tasks' live sessions
	// received a steering prompt vs. were skipped (no live session,
	// unsupported executor, or send failure).
	SessionsSteered int `json:"sessions_steered,omitempty"`
	SessionsSkipped int `json:"sessions_skipped,omitempty"`
}

// CreateGoalRequest is the input for creating a goal automation over the API.
type CreateGoalRequest struct {
	Project   string           `json:"project"`
	FeatureID string           `json:"feature_id,omitempty"`
	Title     string           `json:"title"`
	Content   string           `json:"content,omitempty"`
	Config    GoalConfig       `json:"config"`
	Action    AutomationAction `json:"action"`
}

// UpdateGoalRequest is the input for updating an existing goal automation.
// Provided fields are merged onto the existing goal; nil fields are unchanged.
type UpdateGoalRequest struct {
	Title            *string           `json:"title,omitempty"`
	Content          *string           `json:"content,omitempty"`
	Status           *string           `json:"status,omitempty"`
	Criteria         *string           `json:"criteria,omitempty"`
	Validation       *string           `json:"validation,omitempty"`
	Workdir          *string           `json:"workdir,omitempty"`
	TriggerSource    *string           `json:"trigger_source,omitempty"`
	TaskID           *string           `json:"task_id,omitempty"`
	CompleteStatuses *[]string         `json:"complete_statuses,omitempty"`
	BlockedStatuses  *[]string         `json:"blocked_statuses,omitempty"`
	Steering         *GoalSteering     `json:"steering,omitempty"`
	Action           *AutomationAction `json:"action,omitempty"`
}

// GoalSummary is a serializable view of a goal automation entry.
type GoalSummary struct {
	EntryID   string            `json:"entry_id"`
	GoalID    string            `json:"goal_id"`
	Title     string            `json:"title"`
	Project   string            `json:"project,omitempty"`
	FeatureID string            `json:"feature_id,omitempty"`
	Status    string            `json:"status"`
	Config    *GoalConfig       `json:"config,omitempty"`
	Action    *AutomationAction `json:"action,omitempty"`
	Trigger   *TriggerConfig    `json:"trigger,omitempty"`
}

// GoalProgressResponse reports goal-scoped linked-task progress.
type GoalProgressResponse struct {
	GoalID        string               `json:"goal_id"`
	EntryID       string               `json:"entry_id"`
	Project       string               `json:"project,omitempty"`
	FeatureID     string               `json:"feature_id,omitempty"`
	TaskID        string               `json:"task_id,omitempty"`
	FeatureStatus string               `json:"feature_status"`
	Total         int                  `json:"total"`
	Pending       int                  `json:"pending"`
	InProgress    int                  `json:"in_progress"`
	Completed     int                  `json:"completed"`
	Blocked       int                  `json:"blocked"`
	Tasks         []LinkedTaskSnapshot `json:"tasks"`
}

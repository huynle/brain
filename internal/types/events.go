// Package types defines all domain types for the Brain API.
//
// This file defines the unified Event type and namespaced event taxonomy
// used across the runner, API server, hooks, and webhooks.
package types

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"
)

// =============================================================================
// Event Sources
// =============================================================================

const (
	// EventSourceRunner indicates the event originated from the task runner.
	EventSourceRunner = "runner"
	// EventSourceAPI indicates the event originated from the API server.
	EventSourceAPI = "api"
)

// =============================================================================
// Event Type Constants (namespaced strings)
// =============================================================================

const (
	// Runner lifecycle events.
	EventRunnerStarted           = "runner.started"
	EventRunnerStopped           = "runner.stopped"
	EventRunnerPollComplete      = "runner.poll_complete"
	EventRunnerStateSaved        = "runner.state_saved"
	EventRunnerAllPaused         = "runner.all_paused"
	EventRunnerAllResumed        = "runner.all_resumed"
	EventRunnerSessionDiscovered = "runner.session_discovered"

	// Project lifecycle events.
	EventProjectStarted = "project.started"
	EventProjectPaused  = "project.paused"
	EventProjectResumed = "project.resumed"

	// Task lifecycle events.
	EventTaskClaimed       = "task.claimed"
	EventTaskClaimRejected = "task.claim_rejected"
	EventTaskStarted       = "task.started"
	EventTaskCompleted     = "task.completed"
	EventTaskFailed        = "task.failed"
	EventTaskBlocked       = "task.blocked"
	EventTaskCancelled     = "task.cancelled"
	EventTaskReleased      = "task.released"
	EventTaskStatusChanged = "task.status_changed"
	EventTaskTriggered     = "task.triggered"
	EventTaskIdleDetected  = "task.idle_detected"

	// Feature lifecycle events.
	EventFeatureStarted   = "feature.started"
	EventFeatureCompleted = "feature.completed"
	EventFeatureBlocked   = "feature.blocked"
	EventFeatureProgress  = "feature.progress"
	EventFeatureEnabled   = "feature.enabled"
	EventFeatureDisabled  = "feature.disabled"

	// Entry CRUD events.
	EventEntryCreated = "entry.created"
	EventEntryUpdated = "entry.updated"
	EventEntryDeleted = "entry.deleted"
)

// AllEventTypes enumerates all valid event type strings.
var AllEventTypes = []string{
	EventRunnerStarted, EventRunnerStopped,
	EventRunnerPollComplete, EventRunnerStateSaved,
	EventRunnerAllPaused, EventRunnerAllResumed, EventRunnerSessionDiscovered,
	EventProjectStarted, EventProjectPaused, EventProjectResumed,
	EventTaskClaimed, EventTaskClaimRejected, EventTaskStarted, EventTaskCompleted,
	EventTaskFailed, EventTaskBlocked, EventTaskCancelled, EventTaskReleased,
	EventTaskStatusChanged, EventTaskTriggered, EventTaskIdleDetected,
	EventFeatureStarted, EventFeatureCompleted, EventFeatureBlocked, EventFeatureProgress,
	EventFeatureEnabled, EventFeatureDisabled,
	EventEntryCreated, EventEntryUpdated, EventEntryDeleted,
}

// eventTypeSet is a lookup set for O(1) event type validation.
var eventTypeSet = makeSet(AllEventTypes)

// IsValidEventType returns true if s is a valid event type.
func IsValidEventType(s string) bool {
	return eventTypeSet[s]
}

// =============================================================================
// Event Struct
// =============================================================================

// Event represents a unified event emitted by the runner, API server,
// hooks, or webhooks. All consumers share this single event type.
type Event struct {
	// ID is a unique identifier for the event (format: "evt_<random>").
	ID string `json:"id" yaml:"id"`
	// Type is the namespaced event type (e.g., "task.started").
	Type string `json:"type" yaml:"type"`
	// Source indicates where the event originated ("runner" or "api").
	Source string `json:"source" yaml:"source"`
	// Timestamp is when the event was created.
	Timestamp time.Time `json:"timestamp" yaml:"timestamp"`

	// RunnerID identifies which runner instance emitted the event.
	RunnerID string `json:"runner_id,omitempty" yaml:"runner_id,omitempty"`
	// ProjectID is the project this event relates to.
	ProjectID string `json:"project_id,omitempty" yaml:"project_id,omitempty"`
	// TaskID is the task this event relates to.
	TaskID string `json:"task_id,omitempty" yaml:"task_id,omitempty"`
	// TaskPath is the brain path of the task.
	TaskPath string `json:"task_path,omitempty" yaml:"task_path,omitempty"`
	// TaskTitle is the human-readable title of the task.
	TaskTitle string `json:"task_title,omitempty" yaml:"task_title,omitempty"`
	// FeatureID is the feature this event relates to.
	FeatureID string `json:"feature_id,omitempty" yaml:"feature_id,omitempty"`

	// FromStatus is the previous status (for status change events).
	FromStatus string `json:"from_status,omitempty" yaml:"from_status,omitempty"`
	// ToStatus is the new status (for status change events).
	ToStatus string `json:"to_status,omitempty" yaml:"to_status,omitempty"`

	// Metadata holds arbitrary key-value pairs for extensibility.
	Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// =============================================================================
// TriggerConfig
// =============================================================================

// TriggerConfig defines when a hook should fire based on an event.
//
// A single TriggerConfig can express multiple events and OR-able filter
// values, allowing one automation entry to match a set of related events
// (e.g., task.completed OR feature.completed) and a set of statuses
// (e.g., to_status in {completed, blocked}).
//
// Backward compatibility: the legacy single-event field Event and plain
// exact-match filter values continue to work unchanged. New shapes are
// opt-in via the Events slice and the "in:" filter value prefix.
type TriggerConfig struct {
	// Type is optional and used by automation entries (event, cron, webhook, session).
	// For legacy trigger-based tasks, this is typically empty and Event carries the match rule.
	Type string `json:"type,omitempty" yaml:"type,omitempty"`
	// Event is the event pattern to match (e.g., "task.completed", "task.*").
	// For multi-event triggers, use Events. Event and Events are combined
	// (OR semantics) by EventPatterns().
	Event string `json:"event,omitempty" yaml:"event,omitempty"`
	// Events is an optional list of event patterns to match (OR semantics).
	// An event matches the trigger if it matches Event OR any entry in Events.
	Events []string `json:"events,omitempty" yaml:"events,omitempty"`
	// Schedule is used by cron-style automations.
	Schedule string `json:"schedule,omitempty" yaml:"schedule,omitempty"`
	// Filter is optional key-value filters applied to event fields.
	//
	// Filter values support two forms:
	//   - Exact match (default): "to_status": "completed" matches only "completed".
	//   - OR-able set via "in:" prefix: "to_status": "in:completed,blocked"
	//     matches if the event field is any of the comma-separated values.
	//   - Wildcard "*" matches any non-empty value.
	Filter map[string]string `json:"filter,omitempty" yaml:"filter,omitempty"`
	// OncePer is an automation dedup key (e.g. feature_id, session, day).
	OncePer string `json:"once_per,omitempty" yaml:"once_per,omitempty"`
	// Webhook is the webhook path for webhook-triggered automations.
	Webhook string `json:"webhook,omitempty" yaml:"webhook,omitempty"`
	// IgnoreAutomationEvents defaults automation matching away from self-generated events.
	IgnoreAutomationEvents *bool `json:"ignore_automation_events,omitempty" yaml:"ignore_automation_events,omitempty"`
	// Cooldown is the minimum interval between firings (e.g., "5m", "1h").
	Cooldown string `json:"cooldown,omitempty" yaml:"cooldown,omitempty"`
	// MaxConcurrent limits the number of concurrent executions.
	MaxConcurrent int `json:"max_concurrent,omitempty" yaml:"max_concurrent,omitempty"`
}

// EventPatterns returns the union of Event and Events as the full set of
// event patterns this trigger matches (OR semantics). Empty patterns are
// skipped and duplicates are removed while preserving first-seen order.
func (tc *TriggerConfig) EventPatterns() []string {
	if tc == nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	add(tc.Event)
	for _, e := range tc.Events {
		add(e)
	}
	return out
}

// MatchesEvent reports whether the given event type matches any of this
// trigger's event patterns (OR semantics). It uses MatchEventPattern for
// each pattern, so exact matches, namespace wildcards ("task.*"), and the
// global wildcard ("*") are all supported.
//
// A trigger with no event patterns matches nothing.
func (tc *TriggerConfig) MatchesEvent(eventType string) bool {
	if tc == nil {
		return false
	}
	for _, pattern := range tc.EventPatterns() {
		if MatchEventPattern(pattern, eventType) {
			return true
		}
	}
	return false
}

// MatchFilterValue reports whether an actual event field value satisfies a
// filter expression. Supported expression forms:
//
//   - "*"                  → matches any non-empty actual value.
//   - "in:a,b,c"           → matches if actual is any of a, b, or c (OR-able set).
//   - "<value>" (default)  → exact match against actual.
//
// Whitespace around "in:" members is trimmed and empty members are ignored.
func MatchFilterValue(actual, filterExpr string) bool {
	if filterExpr == "*" {
		return actual != ""
	}
	if rest, ok := parseInFilter(filterExpr); ok {
		for _, want := range rest {
			if actual == want {
				return true
			}
		}
		return false
	}
	return actual == filterExpr
}

// parseInFilter parses an "in:a,b,c" filter expression into its members.
// The second return value is false if the expression is not an "in:" form.
func parseInFilter(filterExpr string) ([]string, bool) {
	const prefix = "in:"
	if !strings.HasPrefix(filterExpr, prefix) {
		return nil, false
	}
	raw := strings.TrimPrefix(filterExpr, prefix)
	parts := strings.Split(raw, ",")
	members := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			members = append(members, p)
		}
	}
	return members, true
}

// MatchesFilters reports whether all of this trigger's filters are satisfied
// for an event, using getField to resolve each filter key to the event's
// field value. Each filter value is evaluated with MatchFilterValue, so
// exact, "in:" (OR-able), and "*" (wildcard) semantics are all honored.
//
// A trigger with no filters always matches (returns true). getField may be
// nil only when there are no filters.
func (tc *TriggerConfig) MatchesFilters(getField func(key string) string) bool {
	if tc == nil || len(tc.Filter) == 0 {
		return true
	}
	for key, expr := range tc.Filter {
		actual := ""
		if getField != nil {
			actual = getField(key)
		}
		if !MatchFilterValue(actual, expr) {
			return false
		}
	}
	return true
}

// =============================================================================
// WebhookConfig
// =============================================================================

// WebhookConfig defines an outbound webhook endpoint.
type WebhookConfig struct {
	// ID is the unique identifier for this webhook.
	ID string `json:"id" yaml:"id"`
	// Name is a human-readable label.
	Name string `json:"name" yaml:"name"`
	// URL is the HTTP endpoint to deliver events to.
	URL string `json:"url" yaml:"url"`
	// Events is the list of event patterns this webhook subscribes to.
	Events []string `json:"events" yaml:"events"`
	// Filter is optional key-value filters applied to event fields.
	Filter map[string]string `json:"filter,omitempty" yaml:"filter,omitempty"`
	// Secret is used for HMAC signature verification.
	Secret string `json:"secret,omitempty" yaml:"secret,omitempty"`
	// Enabled controls whether this webhook is active.
	Enabled bool `json:"enabled" yaml:"enabled"`
}

// =============================================================================
// WebhookDelivery
// =============================================================================

// WebhookDelivery records a single attempt to deliver an event to a webhook.
type WebhookDelivery struct {
	// ID is the unique identifier for this delivery attempt.
	ID string `json:"id" yaml:"id"`
	// WebhookID is the webhook this delivery is for.
	WebhookID string `json:"webhook_id" yaml:"webhook_id"`
	// EventID is the event being delivered.
	EventID string `json:"event_id" yaml:"event_id"`
	// EventType is the type of event delivered.
	EventType string `json:"event_type" yaml:"event_type"`
	// Timestamp is when the delivery was attempted.
	Timestamp time.Time `json:"timestamp" yaml:"timestamp"`
	// URL is the endpoint the event was delivered to.
	URL string `json:"url" yaml:"url"`
	// StatusCode is the HTTP status code returned.
	StatusCode int `json:"status_code" yaml:"status_code"`
	// Success indicates whether the delivery was successful.
	Success bool `json:"success" yaml:"success"`
	// ResponseBody is the response body (truncated for storage).
	ResponseBody string `json:"response_body,omitempty" yaml:"response_body,omitempty"`
	// Error is set if the delivery failed.
	Error string `json:"error,omitempty" yaml:"error,omitempty"`
	// Duration is the request duration in milliseconds.
	Duration int64 `json:"duration_ms" yaml:"duration_ms"`
}

// =============================================================================
// Helper Functions
// =============================================================================

// NewEvent creates a new Event with an auto-generated unique ID and
// the current UTC timestamp.
func NewEvent(eventType, source string) Event {
	return Event{
		ID:        generateEventID(),
		Type:      eventType,
		Source:    source,
		Timestamp: TimeNowUTC(),
	}
}

// MatchEventPattern checks whether an event type matches a pattern.
// Supported patterns:
//   - Exact match: "task.started" matches "task.started"
//   - Namespace wildcard: "task.*" matches any event starting with "task."
//   - Global wildcard: "*" matches any event type
//
// Empty patterns or event types never match.
func MatchEventPattern(pattern, eventType string) bool {
	if pattern == "" || eventType == "" {
		return false
	}

	// Global wildcard matches everything.
	if pattern == "*" {
		return true
	}

	// Namespace wildcard: "task.*" matches "task.started", "task.completed", etc.
	if strings.HasSuffix(pattern, ".*") {
		namespace := strings.TrimSuffix(pattern, ".*")
		return strings.HasPrefix(eventType, namespace+".")
	}

	// Exact match.
	return pattern == eventType
}

// =============================================================================
// Webhook Request/Response Types
// =============================================================================

// CreateWebhookRequest is the payload for creating a webhook.
type CreateWebhookRequest struct {
	Name    string            `json:"name"`
	URL     string            `json:"url"`
	Events  []string          `json:"events"`
	Filter  map[string]string `json:"filter,omitempty"`
	Secret  string            `json:"secret,omitempty"`
	Enabled *bool             `json:"enabled,omitempty"` // defaults to true
}

// UpdateWebhookRequest is the payload for updating a webhook.
type UpdateWebhookRequest struct {
	Name    *string           `json:"name,omitempty"`
	URL     *string           `json:"url,omitempty"`
	Events  []string          `json:"events,omitempty"`
	Filter  map[string]string `json:"filter,omitempty"`
	Secret  *string           `json:"secret,omitempty"`
	Enabled *bool             `json:"enabled,omitempty"`
}

// WebhookResponse is the API response for a webhook.
type WebhookResponse struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	URL       string            `json:"url"`
	Events    []string          `json:"events"`
	Filter    map[string]string `json:"filter,omitempty"`
	Enabled   bool              `json:"enabled"`
	CreatedAt string            `json:"created_at"`
	UpdatedAt string            `json:"updated_at"`
}

// WebhookDeliveryResponse is the API response for a delivery attempt.
type WebhookDeliveryResponse struct {
	ID         string `json:"id"`
	WebhookID  string `json:"webhook_id"`
	EventType  string `json:"event_type"`
	StatusCode *int   `json:"status_code,omitempty"`
	Success    bool   `json:"success"`
	LatencyMs  *int   `json:"latency_ms,omitempty"`
	Error      string `json:"error,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// =============================================================================
// Helper Functions
// =============================================================================

// generateEventID creates a unique event ID with the "evt_" prefix.
func generateEventID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("evt_%x", b)
}

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
	EventTaskStatusChanged, EventTaskIdleDetected,
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
type TriggerConfig struct {
	// Event is the event pattern to match (e.g., "task.completed", "task.*").
	Event string `json:"event" yaml:"event"`
	// Filter is optional key-value filters applied to event fields.
	Filter map[string]string `json:"filter,omitempty" yaml:"filter,omitempty"`
	// Cooldown is the minimum interval between firings (e.g., "5m", "1h").
	Cooldown string `json:"cooldown,omitempty" yaml:"cooldown,omitempty"`
	// MaxConcurrent limits the number of concurrent executions.
	MaxConcurrent int `json:"max_concurrent,omitempty" yaml:"max_concurrent,omitempty"`
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

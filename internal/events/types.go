// Package events provides a typed event bus for domain events.
//
// The event bus decouples event producers from consumers, supporting
// typed events with pattern-based subscriptions. It replaces direct
// coupling to the SSE-specific realtime.Hub for internal event routing.
package events

import "time"

// EventType represents the type of a domain event.
// Events follow a "resource.action" naming convention.
type EventType string

// Event type constants organized by domain resource.
const (
	// Entry events
	EntryCreated EventType = "entry.created"
	EntryUpdated EventType = "entry.updated"
	EntryDeleted EventType = "entry.deleted"

	// Task events
	TaskCompleted EventType = "task.completed"
	TaskFailed    EventType = "task.failed"
	TaskClaimed   EventType = "task.claimed"
	TaskReleased  EventType = "task.released"

	// Feature events
	FeatureAllCompleted EventType = "feature.all_completed"

	// Schedule events
	ScheduleFired EventType = "schedule.fired"

	// Runner events
	RunnerStarted        EventType = "runner.started"
	RunnerFirstTaskToday EventType = "runner.first_task_today"

	// Webhook events
	WebhookReceived EventType = "webhook.received"

	// Project events (compatibility with existing Hub behavior)
	ProjectDirty  EventType = "project.dirty"
	ProjectError  EventType = "project.error"
	TasksSnapshot EventType = "tasks.snapshot"
)

// Event represents a typed domain event flowing through the bus.
type Event struct {
	// Type identifies the event kind (e.g. "entry.created", "task.completed").
	Type EventType

	// Payload carries event-specific data as a flexible map.
	Payload map[string]any

	// DedupKey is an optional key for deduplication. Events with the
	// same DedupKey within a dedup window may be collapsed.
	DedupKey string

	// Source identifies the component that produced the event.
	Source string

	// Timestamp is when the event was created.
	Timestamp time.Time

	// ProjectID scopes the event to a specific project.
	ProjectID string
}

// Handler is a function that processes events from the bus.
type Handler func(Event)

// Subscription represents an active subscription that can be cancelled.
type Subscription interface {
	// Unsubscribe removes this subscription from the bus.
	// Safe to call multiple times.
	Unsubscribe()
}

// Bus is the interface for publishing and subscribing to typed events.
type Bus interface {
	// Publish sends an event to all matching subscribers.
	// Non-blocking: slow subscribers may miss events.
	Publish(event Event)

	// Subscribe registers a handler for a specific event type.
	// Returns a Subscription that can be used to unsubscribe.
	Subscribe(eventType EventType, handler Handler) Subscription

	// SubscribePattern registers a handler for events matching a pattern.
	// Patterns support wildcards: "task.*" matches task.completed, task.failed, etc.
	// A bare "*" matches all events.
	SubscribePattern(pattern string, handler Handler) Subscription

	// Close shuts down the bus and releases resources.
	// After Close, Publish is a no-op and Subscribe returns a no-op Subscription.
	Close()
}

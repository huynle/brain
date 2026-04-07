// Package events — AutomationMatcher: event-driven automation matching engine.
//
// AutomationMatcher subscribes to the event bus, matches incoming events
// against active automation entries, and creates tasks on match. It handles
// event-type automations (cron automations are handled by CronEmitter).
package events

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/huynle/brain-api/internal/types"
)

// AutomationEntry represents an automation brain entry with its trigger and action config.
type AutomationEntry struct {
	ID        string                  // Short 8-char ID
	Path      string                  // Full path (e.g., "projects/test/automation/auto1.md")
	ProjectID string                  // Project scope (empty = global)
	Trigger   types.AutomationTrigger // When to fire
	Action    types.AutomationAction  // What to do
	Status    string                  // Entry status (should be "active")
}

// AutomationSource provides the matcher with active automation entries.
type AutomationSource interface {
	// ListActiveAutomations returns all automation entries with status=active.
	ListActiveAutomations(ctx context.Context) ([]AutomationEntry, error)
}

// TaskCreator abstracts task creation and update operations for the matcher.
// This avoids importing the full BrainService into the events package.
type TaskCreator interface {
	// CreateTask creates a new task entry triggered by an automation.
	CreateTask(ctx context.Context, automationID string, req types.CreateEntryRequest) error
	// UpdateEntry updates an existing entry (for action.type=update).
	UpdateEntry(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) error
	// HasGeneratedKey checks if a task with the given generated_key already exists.
	HasGeneratedKey(ctx context.Context, project, generatedKey string) (bool, error)
}

// EventMarker abstracts marking events as processed.
// Optional: if nil, events are not marked.
type EventMarker interface {
	MarkProcessed(ctx context.Context, id int64) error
}

// AutomationMatcherConfig holds configuration for creating an AutomationMatcher.
type AutomationMatcherConfig struct {
	Bus     Bus              // Event bus to subscribe to
	Source  AutomationSource // Provides active automations
	Creator TaskCreator      // Creates tasks / updates entries
	Marker  EventMarker      // Optional: marks events as processed in event_log
}

// AutomationMatcher subscribes to the event bus, matches incoming events
// against cached automation entries, and dispatches actions on match.
type AutomationMatcher struct {
	bus     Bus
	source  AutomationSource
	creator TaskCreator
	marker  EventMarker

	mu    sync.RWMutex
	cache []AutomationEntry
}

// NewAutomationMatcher creates a new AutomationMatcher with the given configuration.
func NewAutomationMatcher(cfg AutomationMatcherConfig) *AutomationMatcher {
	return &AutomationMatcher{
		bus:     cfg.Bus,
		source:  cfg.Source,
		creator: cfg.Creator,
		marker:  cfg.Marker,
	}
}

// Start begins the automation matching loop. It blocks until ctx is cancelled.
// Safe to call from a goroutine.
func (am *AutomationMatcher) Start(ctx context.Context) {
	slog.Info("automation_matcher: starting")

	// Load initial cache
	am.refreshCache(ctx)

	// Subscribe to all events using wildcard
	sub := am.bus.SubscribePattern("*", func(e Event) {
		am.handleEvent(ctx, e)
	})

	<-ctx.Done()
	sub.Unsubscribe()
	slog.Info("automation_matcher: shutting down")
}

// refreshCache reloads the automation entries from the source.
func (am *AutomationMatcher) refreshCache(ctx context.Context) {
	entries, err := am.source.ListActiveAutomations(ctx)
	if err != nil {
		slog.Warn("automation_matcher: failed to load automations", "error", err)
		return
	}

	am.mu.Lock()
	am.cache = entries
	am.mu.Unlock()

	slog.Debug("automation_matcher: cache refreshed", "count", len(entries))
}

// handleEvent processes a single event from the bus.
func (am *AutomationMatcher) handleEvent(ctx context.Context, event Event) {
	// Check if this is an automation entry change — invalidate cache
	if am.isAutomationEntryChange(event) {
		am.refreshCache(ctx)
		am.markEventProcessed(ctx, event)
		return // Don't match automation entry changes against automations
	}

	// Get cached automations
	am.mu.RLock()
	automations := make([]AutomationEntry, len(am.cache))
	copy(automations, am.cache)
	am.mu.RUnlock()

	// Match event against each automation
	for _, auto := range automations {
		if am.matches(event, auto) {
			am.dispatch(ctx, event, auto)
		}
	}

	// Mark event as processed after all matching is done
	am.markEventProcessed(ctx, event)
}

// isAutomationEntryChange returns true if the event indicates an automation entry
// was created, updated, or deleted. Used to invalidate the cache.
func (am *AutomationMatcher) isAutomationEntryChange(event Event) bool {
	switch event.Type {
	case EntryCreated, EntryUpdated, EntryDeleted:
		if entryType, ok := event.Payload["type"].(string); ok {
			return entryType == "automation"
		}
	}
	return false
}

// matches checks whether an event matches an automation's trigger criteria.
func (am *AutomationMatcher) matches(event Event, auto AutomationEntry) bool {
	// Only match event-type triggers (cron handled by CronEmitter)
	if auto.Trigger.Type != "event" {
		return false
	}

	// Loop prevention: by default, ignore events from automation-generated sources.
	// Per-automation override: set ignore_automation_events: false to opt-in to
	// processing automation-generated events.
	if isAutomationSource(event.Source) {
		if auto.Trigger.IgnoreAutomationEvents == nil || *auto.Trigger.IgnoreAutomationEvents {
			// Default behavior (nil or true): ignore automation events
			return false
		}
		// Explicit opt-in: ignore_automation_events: false — allow this event
	}

	// Match event type
	if string(event.Type) != auto.Trigger.Event {
		return false
	}

	// Project scope matching:
	// - Global automations (ProjectID == "") match all projects
	// - If filter has "project": "*", match all projects (explicit wildcard)
	// - Otherwise, project-scoped automations only match their project
	if auto.ProjectID != "" && auto.ProjectID != event.ProjectID {
		// Allow if filter explicitly uses wildcard for project
		if auto.Trigger.Filter["project"] != "*" {
			return false
		}
	}

	// Filter matching: each filter key-value must match the event payload
	if !am.matchFilters(event, auto.Trigger.Filter) {
		return false
	}

	return true
}

// matchFilters checks that all filter conditions match the event.
// Filter values of "*" match any value. Other values must match exactly.
// Filters also check against the event's ProjectID for "project" key.
func (am *AutomationMatcher) matchFilters(event Event, filters map[string]string) bool {
	for key, expected := range filters {
		// Wildcard matches everything
		if expected == "*" {
			continue
		}

		// Check payload first
		var actual string
		if val, ok := event.Payload[key]; ok {
			actual = fmt.Sprintf("%v", val)
		} else if key == "project" {
			// Fall back to event.ProjectID for "project" filter
			actual = event.ProjectID
		}

		if actual != expected {
			return false
		}
	}
	return true
}

// dispatch executes the action for a matched automation.
func (am *AutomationMatcher) dispatch(ctx context.Context, event Event, auto AutomationEntry) {
	// Check once_per dedup constraint
	if auto.Trigger.OncePer != "" {
		generatedKey := am.buildGeneratedKey(auto, event)
		project := auto.ProjectID
		if project == "" {
			project = event.ProjectID
		}

		exists, err := am.creator.HasGeneratedKey(ctx, project, generatedKey)
		if err != nil {
			slog.Warn("automation_matcher: dedup check failed",
				"automation_id", auto.ID,
				"error", err,
			)
			return
		}
		if exists {
			slog.Debug("automation_matcher: skipping (once_per dedup)",
				"automation_id", auto.ID,
				"generated_key", generatedKey,
			)
			return
		}
	}

	switch auto.Action.Type {
	case "update":
		am.dispatchUpdate(ctx, event, auto)
	default:
		// prompt, script, http — all create tasks
		am.dispatchCreateTask(ctx, event, auto)
	}
}

// dispatchCreateTask creates a task entry for the matched automation.
func (am *AutomationMatcher) dispatchCreateTask(ctx context.Context, event Event, auto AutomationEntry) {
	project := auto.ProjectID
	if project == "" {
		project = event.ProjectID
	}

	generatedBy := fmt.Sprintf("automation:%s", auto.ID)
	trueVal := true

	req := types.CreateEntryRequest{
		Type:           "task",
		Title:          fmt.Sprintf("Automation: %s", auto.ID),
		Content:        auto.Action.DirectPrompt,
		Status:         "pending",
		Project:        project,
		Generated:      &trueVal,
		GeneratedBy:    generatedBy,
		DirectPrompt:   auto.Action.DirectPrompt,
		Agent:          auto.Action.Agent,
		Model:          auto.Action.Model,
		ExecutionMode:  auto.Action.ExecutionMode,
		CompleteOnIdle: auto.Action.CompleteOnIdle,
	}

	// Set generated_key for dedup if once_per is set
	if auto.Trigger.OncePer != "" {
		req.GeneratedKey = am.buildGeneratedKey(auto, event)
	}

	if err := am.creator.CreateTask(ctx, auto.ID, req); err != nil {
		slog.Error("automation_matcher: failed to create task",
			"automation_id", auto.ID,
			"error", err,
		)
		return
	}

	slog.Info("automation_matcher: task created",
		"automation_id", auto.ID,
		"project", project,
		"event_type", event.Type,
	)
}

// dispatchUpdate handles action.type=update by calling UpdateEntry directly.
func (am *AutomationMatcher) dispatchUpdate(ctx context.Context, event Event, auto AutomationEntry) {
	// For update actions, the target is derived from the event payload
	pathOrID := ""
	if path, ok := event.Payload["path"].(string); ok {
		pathOrID = path
	} else if id, ok := event.Payload["id"].(string); ok {
		pathOrID = id
	}

	if pathOrID == "" {
		slog.Warn("automation_matcher: update action has no target",
			"automation_id", auto.ID,
		)
		return
	}

	req := types.UpdateEntryRequest{}

	if err := am.creator.UpdateEntry(ctx, pathOrID, req); err != nil {
		slog.Error("automation_matcher: failed to update entry",
			"automation_id", auto.ID,
			"target", pathOrID,
			"error", err,
		)
		return
	}

	slog.Info("automation_matcher: entry updated",
		"automation_id", auto.ID,
		"target", pathOrID,
	)
}

// buildGeneratedKey constructs the dedup key from automation ID and the once_per field value.
// Format: "automation:{automation_id}:{once_per_value}"
func (am *AutomationMatcher) buildGeneratedKey(auto AutomationEntry, event Event) string {
	oncePerValue := ""
	if val, ok := event.Payload[auto.Trigger.OncePer]; ok {
		oncePerValue = fmt.Sprintf("%v", val)
	}
	return fmt.Sprintf("automation:%s:%s", auto.ID, oncePerValue)
}

// isAutomationSource returns true if the event source indicates it was
// generated by the automation system (matcher or service responding to automation).
func isAutomationSource(source string) bool {
	return source == "automation_matcher" || source == "automation"
}

// markEventProcessed marks an event as processed in the event_log via the EventMarker.
// The event_log ID is expected in Payload["_event_log_id"] (injected by DedupBus).
func (am *AutomationMatcher) markEventProcessed(ctx context.Context, event Event) {
	if am.marker == nil {
		return
	}

	idRaw, ok := event.Payload["_event_log_id"]
	if !ok {
		return
	}

	var id int64
	switch v := idRaw.(type) {
	case int64:
		id = v
	case float64:
		id = int64(v)
	case int:
		id = int64(v)
	default:
		return
	}

	if err := am.marker.MarkProcessed(ctx, id); err != nil {
		slog.Warn("automation_matcher: failed to mark event processed",
			"event_log_id", id,
			"error", err,
		)
	}
}

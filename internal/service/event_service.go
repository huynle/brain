package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// Compile-time check that EventServiceImpl implements api.EventService.
var _ api.EventService = (*EventServiceImpl)(nil)

// defaultRecentLimit is the default limit when Recent is called with limit <= 0.
const defaultRecentLimit = 100

// EventServiceImpl implements api.EventService using the EventHub.
// It handles event ingestion with validation and deduplication,
// querying recent events, and subscribing to filtered event streams.
type EventServiceImpl struct {
	hub *realtime.EventHub

	// seenIDs tracks event IDs for deduplication.
	mu      sync.RWMutex
	seenIDs map[string]struct{}
}

// NewEventService creates a new EventServiceImpl.
func NewEventService(hub *realtime.EventHub) *EventServiceImpl {
	return &EventServiceImpl{
		hub:     hub,
		seenIDs: make(map[string]struct{}),
	}
}

// =============================================================================
// Ingest
// =============================================================================

// Ingest accepts a batch of events, validates them, assigns IDs and
// timestamps if missing, deduplicates by ID, and publishes to the EventHub.
func (s *EventServiceImpl) Ingest(ctx context.Context, events []types.Event) error {
	if len(events) == 0 {
		return nil
	}

	for i := range events {
		evt := &events[i]

		// Validate event type.
		if !types.IsValidEventType(evt.Type) {
			return fmt.Errorf("invalid event type %q at index %d", evt.Type, i)
		}

		// Validate source.
		if evt.Source != types.EventSourceRunner && evt.Source != types.EventSourceAPI {
			return fmt.Errorf("invalid event source %q at index %d: must be %q or %q",
				evt.Source, i, types.EventSourceRunner, types.EventSourceAPI)
		}

		// Auto-assign ID if missing.
		if evt.ID == "" {
			evt.ID = generateEventID()
		}

		// Auto-assign timestamp if missing.
		if evt.Timestamp.IsZero() {
			evt.Timestamp = types.TimeNowUTC()
		}

		// Deduplication check.
		s.mu.RLock()
		_, seen := s.seenIDs[evt.ID]
		s.mu.RUnlock()
		if seen {
			continue
		}

		// Mark as seen and publish.
		s.mu.Lock()
		// Double-check under write lock.
		if _, seen := s.seenIDs[evt.ID]; seen {
			s.mu.Unlock()
			continue
		}
		s.seenIDs[evt.ID] = struct{}{}
		s.mu.Unlock()

		s.hub.Publish(*evt)
	}

	return nil
}

// generateEventID creates a unique event ID. Delegates to types.NewEvent
// to reuse the same ID format.
func generateEventID() string {
	evt := types.NewEvent("", "")
	return evt.ID
}

// =============================================================================
// Recent
// =============================================================================

// Recent returns recent events from the ring buffer with optional filters.
// Supported filter keys: "project_id", "type", "source", "feature_id".
func (s *EventServiceImpl) Recent(_ context.Context, limit int, filters map[string]string) ([]types.Event, error) {
	if limit <= 0 {
		limit = defaultRecentLimit
	}

	// Get all events from the hub's ring buffer.
	all := s.hub.Replay("")

	// Apply filters.
	var filtered []types.Event
	for _, evt := range all {
		if matchesFilters(evt, filters) {
			filtered = append(filtered, evt)
		}
	}

	// Return the most recent `limit` events.
	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}

	return filtered, nil
}

// matchesFilters checks whether an event matches all filter key-value pairs.
func matchesFilters(evt types.Event, filters map[string]string) bool {
	for key, val := range filters {
		switch key {
		case "project_id":
			if evt.ProjectID != val {
				return false
			}
		case "type":
			if !types.MatchEventPattern(val, evt.Type) {
				return false
			}
		case "source":
			if evt.Source != val {
				return false
			}
		case "feature_id":
			if evt.FeatureID != val {
				return false
			}
		case "task_id":
			if evt.TaskID != val {
				return false
			}
		}
	}
	return true
}

// =============================================================================
// Subscribe
// =============================================================================

// Subscribe returns a channel of events matching the given filters and
// an unsubscribe function. Delegates to EventHub.Subscribe with a
// translated EventFilter.
func (s *EventServiceImpl) Subscribe(_ context.Context, filters map[string]string) (<-chan types.Event, func()) {
	filter := realtime.EventFilter{}

	if v, ok := filters["type"]; ok {
		filter.TypePatterns = []string{v}
	}
	if v, ok := filters["project_id"]; ok {
		filter.ProjectID = v
	}
	if v, ok := filters["feature_id"]; ok {
		filter.FeatureID = v
	}

	return s.hub.Subscribe(filter)
}

package sse

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Event represents a parsed SSE event.
type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Known event types that carry meaningful data.
var knownEventTypes = map[string]bool{
	"connected":      true,
	"tasks_snapshot": true,
	"error":          true,
}

// Ignored event types (returned as nil, nil).
var ignoredEventTypes = map[string]bool{
	"heartbeat":     true,
	"project_dirty": true,
}

// ParseEvent parses raw SSE lines into an Event.
// Lines are in format: "event: <type>\ndata: <json>\n"
// Returns (nil, nil) for events that should be ignored (heartbeat, project_dirty, unknown).
// Returns (nil, error) for parse errors on known event types.
func ParseEvent(lines []string) (*Event, error) {
	var eventType string
	var dataStr string

	for _, line := range lines {
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			dataStr = strings.TrimPrefix(line, "data: ")
		}
	}

	// Missing event or data line — ignore
	if eventType == "" || dataStr == "" {
		return nil, nil
	}

	// Ignored event types
	if ignoredEventTypes[eventType] {
		return nil, nil
	}

	// Unknown event types
	if !knownEventTypes[eventType] {
		return nil, nil
	}

	// Validate JSON for known event types
	if !json.Valid([]byte(dataStr)) {
		return nil, fmt.Errorf("parse %s event: invalid JSON", eventType)
	}

	return &Event{
		Type: eventType,
		Data: json.RawMessage(dataStr),
	}, nil
}

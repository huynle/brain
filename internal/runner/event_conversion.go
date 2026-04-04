package runner

import (
	"fmt"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// runnerEventTypeMap maps RunnerEventType values to unified namespaced event type strings.
var runnerEventTypeMap = map[RunnerEventType]string{
	EventTaskStarted:       types.EventTaskStarted,
	EventTaskCompleted:     types.EventTaskCompleted,
	EventTaskFailed:        types.EventTaskFailed,
	EventTaskCancelled:     types.EventTaskCancelled,
	EventTaskClaimed:       types.EventTaskClaimed,
	EventTaskClaimRejected: types.EventTaskClaimRejected,
	EventTaskStatusChanged: types.EventTaskStatusChanged,
	EventTaskReleased:      types.EventTaskReleased,
	EventRunnerStarted:     types.EventRunnerStarted,
	EventShutdown:          types.EventRunnerStopped,
	EventProjectPaused:     types.EventProjectPaused,
	EventProjectResumed:    types.EventProjectResumed,
	EventFeatureEnabled:    types.EventFeatureEnabled,
	EventFeatureDisabled:   types.EventFeatureDisabled,
	EventPollComplete:      types.EventRunnerPollComplete,
	EventStateSaved:        types.EventRunnerStateSaved,
	EventAllPaused:         types.EventRunnerAllPaused,
	EventAllResumed:        types.EventRunnerAllResumed,
	EventSessionDiscovered: types.EventRunnerSessionDiscovered,
}

// ToEvent converts a RunnerEvent to the unified types.Event type.
// The conversion is lossless: all RunnerEvent fields are mapped to Event fields
// or placed in the Metadata map. Source is always set to "runner".
func (re RunnerEvent) ToEvent() types.Event {
	// Create base event with auto-generated ID and timestamp.
	evt := types.NewEvent(re.mappedType(), types.EventSourceRunner)

	// Always set RunnerID.
	evt.RunnerID = re.RunnerID

	// Copy top-level fields that exist directly on both structs.
	evt.ProjectID = re.ProjectID
	evt.TaskID = re.TaskID
	evt.TaskPath = re.TaskPath
	evt.FeatureID = re.FeatureID
	evt.FromStatus = re.FromStatus
	evt.ToStatus = re.ToStatus

	// Extract fields from embedded Task (for task_started events).
	if re.Task != nil {
		evt.TaskID = re.Task.ID
		evt.TaskPath = re.Task.Path
		evt.TaskTitle = re.Task.Title
		evt.ProjectID = re.Task.ProjectID
		if re.Task.FeatureID != "" {
			evt.FeatureID = re.Task.FeatureID
		}
	}

	// Extract TaskID from Result if set (for task_completed/task_failed).
	if re.Result != nil && evt.TaskID == "" {
		evt.TaskID = re.Result.TaskID
	}

	// Populate Metadata for fields that don't have direct Event counterparts.
	meta := make(map[string]string)

	if re.Reason != "" {
		meta["reason"] = re.Reason
	}
	if re.ClaimedBy != "" {
		meta["claimed_by"] = re.ClaimedBy
	}
	if re.Path != "" {
		meta["path"] = re.Path
	}
	if re.SessionID != "" {
		meta["session_id"] = re.SessionID
	}
	if re.Mode != "" {
		meta["mode"] = re.Mode
	}
	if len(re.Projects) > 0 {
		meta["projects"] = strings.Join(re.Projects, ",")
	}
	if re.ReadyCount > 0 {
		meta["ready_count"] = fmt.Sprintf("%d", re.ReadyCount)
	}
	if re.RunningCount > 0 {
		meta["running_count"] = fmt.Sprintf("%d", re.RunningCount)
	}

	if len(meta) > 0 {
		evt.Metadata = meta
	}

	return evt
}

// mappedType returns the unified event type string for this RunnerEvent's type.
// Falls back to "runner.<original_type>" for any unmapped values.
func (re RunnerEvent) mappedType() string {
	if mapped, ok := runnerEventTypeMap[re.Type]; ok {
		return mapped
	}
	// Fallback: prefix with "runner." namespace for unknown types.
	return "runner." + string(re.Type)
}

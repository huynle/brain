package runner

import (
	"context"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// FeatureClient is the subset of Client that FeatureTracker needs
// to query feature task counts from the API.
type FeatureClient interface {
	GetTasksByFeature(ctx context.Context, projectID, featureID string) ([]types.ResolvedTask, error)
}

// FeatureState tracks the lifecycle state of a single feature group.
type FeatureState struct {
	FeatureID   string
	ProjectID   string
	FirstTaskAt time.Time
	TaskCount   int    // Total tasks in feature (queried from API)
	Running     int    // Currently running tasks
	Completed   int    // Completed tasks
	Status      string // "idle" | "active" | "completed"
}

// FeatureTracker monitors task events and emits feature-level lifecycle events.
// It tracks active features in runner memory and emits:
//   - feature_started: when the first task in a feature begins
//   - feature_progress: when a task completes but others remain
//   - feature_completed: when all tasks in a feature are done
//   - feature_blocked: when a task in a feature becomes blocked or fails
//
// Thread-safe: multiple goroutines can call HandleEvent concurrently.
type FeatureTracker struct {
	client FeatureClient
	emit   EventHandler
	mu     sync.Mutex
	// activeFeatures maps "projectId:featureId" -> *FeatureState
	activeFeatures map[string]*FeatureState
}

// NewFeatureTracker creates a FeatureTracker that queries the given client
// for task counts and emits events via the given handler.
func NewFeatureTracker(client FeatureClient, emit EventHandler) *FeatureTracker {
	return &FeatureTracker{
		client:         client,
		emit:           emit,
		activeFeatures: make(map[string]*FeatureState),
	}
}

// HandleEvent processes a RunnerEvent and emits feature lifecycle events
// as appropriate. This method is intended to be registered as an OnEvent handler.
func (ft *FeatureTracker) HandleEvent(event RunnerEvent) {
	projectID, featureID := ft.extractIDs(event)
	if featureID == "" {
		return // Only process events with a feature ID
	}

	switch event.Type {
	case EventTaskStarted:
		ft.handleTaskStarted(projectID, featureID)
	case EventTaskCompleted:
		ft.handleTaskCompleted(projectID, featureID)
	case EventTaskFailed:
		ft.handleTaskBlocked(projectID, featureID)
	case EventTaskStatusChanged:
		if event.ToStatus == "blocked" {
			ft.handleTaskBlocked(projectID, featureID)
		}
	}
}

// GetState returns a copy of the current state for a tracked feature,
// or nil if the feature is not being tracked.
func (ft *FeatureTracker) GetState(projectID, featureID string) *FeatureState {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	key := projectID + ":" + featureID
	state, ok := ft.activeFeatures[key]
	if !ok {
		return nil
	}

	// Return a copy to avoid race conditions
	copy := *state
	return &copy
}

// handleTaskStarted processes a task_started event for a feature.
// If the feature is not tracked or has completed, it queries the API
// for the total task count and emits feature_started.
func (ft *FeatureTracker) handleTaskStarted(projectID, featureID string) {
	ft.mu.Lock()
	key := projectID + ":" + featureID
	state, tracked := ft.activeFeatures[key]

	// If already active, just increment running count
	if tracked && state.Status == "active" {
		state.Running++
		ft.mu.Unlock()
		return
	}
	ft.mu.Unlock()

	// Query API for task count (outside lock to avoid blocking)
	tasks, err := ft.client.GetTasksByFeature(context.Background(), projectID, featureID)
	if err != nil {
		// Can't determine task count - skip feature tracking
		return
	}

	taskCount := len(tasks)
	if taskCount == 0 {
		return
	}

	// Count currently completed tasks
	completed := 0
	running := 0
	for _, t := range tasks {
		switch t.Status {
		case "completed", "validated":
			completed++
		case "in_progress":
			running++
		}
	}

	ft.mu.Lock()
	// Re-check: another goroutine may have created the state while we held no lock
	state, tracked = ft.activeFeatures[key]
	if tracked && state.Status == "active" {
		state.Running = running
		state.Completed = completed
		ft.mu.Unlock()
		return
	}

	// Create new feature state
	ft.activeFeatures[key] = &FeatureState{
		FeatureID:   featureID,
		ProjectID:   projectID,
		FirstTaskAt: time.Now(),
		TaskCount:   taskCount,
		Running:     running,
		Completed:   completed,
		Status:      "active",
	}
	ft.mu.Unlock()

	// Emit feature.started
	ft.emit(RunnerEvent{
		Type:       EventFeatureStarted,
		ProjectID:  projectID,
		FeatureID:  featureID,
		ReadyCount: taskCount, // Total tasks in feature
	})
}

// handleTaskCompleted processes a task_completed event for a feature.
// It refreshes the task count from the API, then emits either
// feature_completed (all done) or feature_progress (partial).
func (ft *FeatureTracker) handleTaskCompleted(projectID, featureID string) {
	ft.mu.Lock()
	key := projectID + ":" + featureID
	state, tracked := ft.activeFeatures[key]
	if !tracked {
		ft.mu.Unlock()
		return
	}
	ft.mu.Unlock()

	// Query API for current task statuses
	tasks, err := ft.client.GetTasksByFeature(context.Background(), projectID, featureID)
	if err != nil {
		return
	}

	completed := 0
	running := 0
	for _, t := range tasks {
		switch t.Status {
		case "completed", "validated":
			completed++
		case "in_progress":
			running++
		}
	}

	taskCount := len(tasks)
	allDone := completed >= taskCount && taskCount > 0

	ft.mu.Lock()
	state.Completed = completed
	state.Running = running
	state.TaskCount = taskCount

	if allDone {
		state.Status = "completed"
	}
	ft.mu.Unlock()

	if allDone {
		ft.emit(RunnerEvent{
			Type:       EventFeatureCompleted,
			ProjectID:  projectID,
			FeatureID:  featureID,
			ReadyCount: taskCount,
		})
	} else {
		ft.emit(RunnerEvent{
			Type:         EventFeatureProgress,
			ProjectID:    projectID,
			FeatureID:    featureID,
			ReadyCount:   taskCount,
			RunningCount: completed,
		})
	}
}

// handleTaskBlocked emits feature_blocked when a task in the feature
// becomes blocked or fails.
func (ft *FeatureTracker) handleTaskBlocked(projectID, featureID string) {
	ft.mu.Lock()
	key := projectID + ":" + featureID
	_, tracked := ft.activeFeatures[key]
	ft.mu.Unlock()

	if !tracked {
		return
	}

	ft.emit(RunnerEvent{
		Type:      EventFeatureBlocked,
		ProjectID: projectID,
		FeatureID: featureID,
	})
}

// extractIDs pulls projectID and featureID from a RunnerEvent.
// It checks the embedded Task first (for task_started events),
// then falls back to top-level fields.
func (ft *FeatureTracker) extractIDs(event RunnerEvent) (projectID, featureID string) {
	if event.Task != nil {
		projectID = event.Task.ProjectID
		featureID = event.Task.FeatureID
	}

	// Top-level fields override if the Task didn't have them
	if event.ProjectID != "" {
		projectID = event.ProjectID
	}
	if event.FeatureID != "" {
		featureID = event.FeatureID
	}

	return projectID, featureID
}

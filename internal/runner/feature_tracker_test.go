package runner

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// mockFeatureClient implements FeatureClient for testing.
type mockFeatureClient struct {
	mu             sync.Mutex
	tasksByFeature map[string][]types.ResolvedTask // key: "projectId:featureId"
	err            error
	calls          []string
}

func newMockFeatureClient() *mockFeatureClient {
	return &mockFeatureClient{
		tasksByFeature: make(map[string][]types.ResolvedTask),
	}
}

func (m *mockFeatureClient) GetTasksByFeature(ctx context.Context, projectID, featureID string) ([]types.ResolvedTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, fmt.Sprintf("GetTasksByFeature(%s,%s)", projectID, featureID))
	if m.err != nil {
		return nil, m.err
	}
	key := projectID + ":" + featureID
	return m.tasksByFeature[key], nil
}

func (m *mockFeatureClient) setTasks(projectID, featureID string, tasks []types.ResolvedTask) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasksByFeature[projectID+":"+featureID] = tasks
}

func (m *mockFeatureClient) getCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.calls))
	copy(result, m.calls)
	return result
}

// eventCollector collects emitted RunnerEvents for assertions.
type eventCollector struct {
	mu     sync.Mutex
	events []RunnerEvent
}

func (ec *eventCollector) handler(event RunnerEvent) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.events = append(ec.events, event)
}

func (ec *eventCollector) getEvents() []RunnerEvent {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	result := make([]RunnerEvent, len(ec.events))
	copy(result, ec.events)
	return result
}

func (ec *eventCollector) getEventTypes() []RunnerEventType {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	result := make([]RunnerEventType, len(ec.events))
	for i, e := range ec.events {
		result[i] = e.Type
	}
	return result
}

// makeTasks is a helper to create N tasks with the given feature ID, with specified statuses.
func makeTasks(featureID string, statuses ...string) []types.ResolvedTask {
	tasks := make([]types.ResolvedTask, len(statuses))
	for i, status := range statuses {
		tasks[i] = types.ResolvedTask{
			ID:        fmt.Sprintf("task-%d", i+1),
			Path:      fmt.Sprintf("projects/test/task/task-%d.md", i+1),
			Title:     fmt.Sprintf("Task %d", i+1),
			Status:    status,
			FeatureID: featureID,
		}
	}
	return tasks
}

// =============================================================================
// Tests: Feature Started
// =============================================================================

func TestFeatureTracker_EmitsFeatureStarted_OnFirstTaskStart(t *testing.T) {
	client := newMockFeatureClient()
	collector := &eventCollector{}
	tracker := NewFeatureTracker(client, collector.handler)

	// Set up: feature has 3 tasks total
	client.setTasks("proj1", "feat1", makeTasks("feat1", "in_progress", "pending", "pending"))

	// Simulate task_started event with feature_id
	tracker.HandleEvent(RunnerEvent{
		Type: EventTaskStarted,
		Task: &RunningTask{
			ID:        "task-1",
			ProjectID: "proj1",
			FeatureID: "feat1",
		},
	})

	events := collector.getEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}
	if events[0].Type != EventFeatureStarted {
		t.Errorf("expected EventFeatureStarted, got %s", events[0].Type)
	}
	if events[0].FeatureID != "feat1" {
		t.Errorf("expected FeatureID=feat1, got %s", events[0].FeatureID)
	}
	if events[0].ProjectID != "proj1" {
		t.Errorf("expected ProjectID=proj1, got %s", events[0].ProjectID)
	}
}

func TestFeatureTracker_DoesNotEmitStarted_OnSubsequentTaskStart(t *testing.T) {
	client := newMockFeatureClient()
	collector := &eventCollector{}
	tracker := NewFeatureTracker(client, collector.handler)

	client.setTasks("proj1", "feat1", makeTasks("feat1", "in_progress", "in_progress", "pending"))

	// First task start -> should emit feature.started
	tracker.HandleEvent(RunnerEvent{
		Type: EventTaskStarted,
		Task: &RunningTask{ID: "task-1", ProjectID: "proj1", FeatureID: "feat1"},
	})
	// Second task start -> should NOT emit feature.started again
	tracker.HandleEvent(RunnerEvent{
		Type: EventTaskStarted,
		Task: &RunningTask{ID: "task-2", ProjectID: "proj1", FeatureID: "feat1"},
	})

	types := collector.getEventTypes()
	startedCount := 0
	for _, t2 := range types {
		if t2 == EventFeatureStarted {
			startedCount++
		}
	}
	if startedCount != 1 {
		t.Errorf("expected 1 EventFeatureStarted, got %d (all events: %v)", startedCount, types)
	}
}

func TestFeatureTracker_IgnoresEventsWithoutFeatureID(t *testing.T) {
	client := newMockFeatureClient()
	collector := &eventCollector{}
	tracker := NewFeatureTracker(client, collector.handler)

	// Task without feature_id should be ignored
	tracker.HandleEvent(RunnerEvent{
		Type: EventTaskStarted,
		Task: &RunningTask{ID: "task-1", ProjectID: "proj1"},
	})

	events := collector.getEvents()
	if len(events) != 0 {
		t.Errorf("expected 0 events for task without feature_id, got %d", len(events))
	}
}

// =============================================================================
// Tests: Feature Progress
// =============================================================================

func TestFeatureTracker_EmitsFeatureProgress_OnPartialCompletion(t *testing.T) {
	client := newMockFeatureClient()
	collector := &eventCollector{}
	tracker := NewFeatureTracker(client, collector.handler)

	// 3 tasks, 1 will complete
	client.setTasks("proj1", "feat1", makeTasks("feat1", "in_progress", "pending", "pending"))

	// Start feature
	tracker.HandleEvent(RunnerEvent{
		Type: EventTaskStarted,
		Task: &RunningTask{ID: "task-1", ProjectID: "proj1", FeatureID: "feat1"},
	})

	// Now update API to reflect 1 completed
	client.setTasks("proj1", "feat1", makeTasks("feat1", "completed", "in_progress", "pending"))

	// Task completes
	tracker.HandleEvent(RunnerEvent{
		Type:   EventTaskCompleted,
		Result: &TaskResult{TaskID: "task-1", Status: TaskResultCompleted},
		// FeatureID and ProjectID need to come from tracker state since
		// the runner event for completed may not have them directly on the event.
		// Let's also set them on the event for clarity.
		ProjectID: "proj1",
		FeatureID: "feat1",
	})

	events := collector.getEvents()
	// Should have: feature.started + feature.progress
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d: %+v", len(events), events)
	}
	if events[1].Type != EventFeatureProgress {
		t.Errorf("expected EventFeatureProgress, got %s", events[1].Type)
	}
}

// =============================================================================
// Tests: Feature Completed
// =============================================================================

func TestFeatureTracker_EmitsFeatureCompleted_WhenAllTasksDone(t *testing.T) {
	client := newMockFeatureClient()
	collector := &eventCollector{}
	tracker := NewFeatureTracker(client, collector.handler)

	// Feature with 2 tasks
	client.setTasks("proj1", "feat1", makeTasks("feat1", "in_progress", "pending"))

	// Start feature
	tracker.HandleEvent(RunnerEvent{
		Type: EventTaskStarted,
		Task: &RunningTask{ID: "task-1", ProjectID: "proj1", FeatureID: "feat1"},
	})

	// Complete first task (partial)
	client.setTasks("proj1", "feat1", makeTasks("feat1", "completed", "in_progress"))
	tracker.HandleEvent(RunnerEvent{
		Type:      EventTaskCompleted,
		Result:    &TaskResult{TaskID: "task-1", Status: TaskResultCompleted},
		ProjectID: "proj1",
		FeatureID: "feat1",
	})

	// Complete second task (all done)
	client.setTasks("proj1", "feat1", makeTasks("feat1", "completed", "completed"))
	tracker.HandleEvent(RunnerEvent{
		Type:      EventTaskCompleted,
		Result:    &TaskResult{TaskID: "task-2", Status: TaskResultCompleted},
		ProjectID: "proj1",
		FeatureID: "feat1",
	})

	eventTypes := collector.getEventTypes()
	// Expect: feature.started, feature.progress, feature.completed
	if len(eventTypes) != 3 {
		t.Fatalf("expected 3 events, got %d: %v", len(eventTypes), eventTypes)
	}
	if eventTypes[2] != EventFeatureCompleted {
		t.Errorf("expected last event=EventFeatureCompleted, got %s", eventTypes[2])
	}
}

// =============================================================================
// Tests: Feature Blocked
// =============================================================================

func TestFeatureTracker_EmitsFeatureBlocked_OnTaskBlocked(t *testing.T) {
	client := newMockFeatureClient()
	collector := &eventCollector{}
	tracker := NewFeatureTracker(client, collector.handler)

	client.setTasks("proj1", "feat1", makeTasks("feat1", "in_progress", "pending"))

	// Start feature
	tracker.HandleEvent(RunnerEvent{
		Type: EventTaskStarted,
		Task: &RunningTask{ID: "task-1", ProjectID: "proj1", FeatureID: "feat1"},
	})

	// Task becomes blocked (via status change)
	tracker.HandleEvent(RunnerEvent{
		Type:       EventTaskStatusChanged,
		TaskID:     "task-1",
		ProjectID:  "proj1",
		FeatureID:  "feat1",
		FromStatus: "in_progress",
		ToStatus:   "blocked",
	})

	eventTypes := collector.getEventTypes()
	// Expect: feature.started, feature.blocked
	if len(eventTypes) != 2 {
		t.Fatalf("expected 2 events, got %d: %v", len(eventTypes), eventTypes)
	}
	if eventTypes[1] != EventFeatureBlocked {
		t.Errorf("expected EventFeatureBlocked, got %s", eventTypes[1])
	}
}

// =============================================================================
// Tests: Feature Failed
// =============================================================================

func TestFeatureTracker_EmitsFeatureBlocked_OnTaskFailed(t *testing.T) {
	client := newMockFeatureClient()
	collector := &eventCollector{}
	tracker := NewFeatureTracker(client, collector.handler)

	client.setTasks("proj1", "feat1", makeTasks("feat1", "in_progress", "pending"))

	// Start feature
	tracker.HandleEvent(RunnerEvent{
		Type: EventTaskStarted,
		Task: &RunningTask{ID: "task-1", ProjectID: "proj1", FeatureID: "feat1"},
	})

	// Task fails
	tracker.HandleEvent(RunnerEvent{
		Type:      EventTaskFailed,
		Result:    &TaskResult{TaskID: "task-1", Status: TaskResultFailed},
		ProjectID: "proj1",
		FeatureID: "feat1",
	})

	eventTypes := collector.getEventTypes()
	// Expect: feature.started, feature.blocked (a failed task blocks the feature)
	if len(eventTypes) != 2 {
		t.Fatalf("expected 2 events, got %d: %v", len(eventTypes), eventTypes)
	}
	if eventTypes[1] != EventFeatureBlocked {
		t.Errorf("expected EventFeatureBlocked on task failure, got %s", eventTypes[1])
	}
}

// =============================================================================
// Tests: Thread Safety
// =============================================================================

func TestFeatureTracker_ConcurrentEvents(t *testing.T) {
	client := newMockFeatureClient()
	collector := &eventCollector{}
	tracker := NewFeatureTracker(client, collector.handler)

	// Set up 10 features with 2 tasks each
	for i := 0; i < 10; i++ {
		fid := fmt.Sprintf("feat-%d", i)
		client.setTasks("proj1", fid, makeTasks(fid, "in_progress", "pending"))
	}

	// Concurrently start all features
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			fid := fmt.Sprintf("feat-%d", idx)
			tracker.HandleEvent(RunnerEvent{
				Type: EventTaskStarted,
				Task: &RunningTask{
					ID:        fmt.Sprintf("task-%d-1", idx),
					ProjectID: "proj1",
					FeatureID: fid,
				},
			})
		}(i)
	}
	wg.Wait()

	events := collector.getEvents()
	// Each feature should emit exactly 1 feature.started
	startedCount := 0
	for _, e := range events {
		if e.Type == EventFeatureStarted {
			startedCount++
		}
	}
	if startedCount != 10 {
		t.Errorf("expected 10 EventFeatureStarted events, got %d", startedCount)
	}
}

// =============================================================================
// Tests: API Query
// =============================================================================

func TestFeatureTracker_QueriesAPI_OnFeatureStart(t *testing.T) {
	client := newMockFeatureClient()
	collector := &eventCollector{}
	tracker := NewFeatureTracker(client, collector.handler)

	client.setTasks("proj1", "feat1", makeTasks("feat1", "in_progress", "pending", "pending"))

	tracker.HandleEvent(RunnerEvent{
		Type: EventTaskStarted,
		Task: &RunningTask{ID: "task-1", ProjectID: "proj1", FeatureID: "feat1"},
	})

	calls := client.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 API call, got %d: %v", len(calls), calls)
	}
	if calls[0] != "GetTasksByFeature(proj1,feat1)" {
		t.Errorf("unexpected API call: %s", calls[0])
	}
}

func TestFeatureTracker_HandlesAPIError_Gracefully(t *testing.T) {
	client := newMockFeatureClient()
	client.err = fmt.Errorf("connection refused")
	collector := &eventCollector{}
	tracker := NewFeatureTracker(client, collector.handler)

	// Should not panic on API error
	tracker.HandleEvent(RunnerEvent{
		Type: EventTaskStarted,
		Task: &RunningTask{ID: "task-1", ProjectID: "proj1", FeatureID: "feat1"},
	})

	// No events should be emitted (API call failed, can't determine task count)
	events := collector.getEvents()
	if len(events) != 0 {
		t.Errorf("expected 0 events on API error, got %d", len(events))
	}
}

// =============================================================================
// Tests: Feature State Queries
// =============================================================================

func TestFeatureTracker_GetState_ReturnsTrackedFeature(t *testing.T) {
	client := newMockFeatureClient()
	collector := &eventCollector{}
	tracker := NewFeatureTracker(client, collector.handler)

	client.setTasks("proj1", "feat1", makeTasks("feat1", "in_progress", "pending"))

	tracker.HandleEvent(RunnerEvent{
		Type: EventTaskStarted,
		Task: &RunningTask{ID: "task-1", ProjectID: "proj1", FeatureID: "feat1"},
	})

	state := tracker.GetState("proj1", "feat1")
	if state == nil {
		t.Fatal("expected feature state, got nil")
	}
	if state.FeatureID != "feat1" {
		t.Errorf("expected FeatureID=feat1, got %s", state.FeatureID)
	}
	if state.ProjectID != "proj1" {
		t.Errorf("expected ProjectID=proj1, got %s", state.ProjectID)
	}
	if state.Status != "active" {
		t.Errorf("expected Status=active, got %s", state.Status)
	}
	if state.TaskCount != 2 {
		t.Errorf("expected TaskCount=2, got %d", state.TaskCount)
	}
	if state.Running != 1 {
		t.Errorf("expected Running=1, got %d", state.Running)
	}
}

func TestFeatureTracker_GetState_ReturnsNilForUntracked(t *testing.T) {
	client := newMockFeatureClient()
	collector := &eventCollector{}
	tracker := NewFeatureTracker(client, collector.handler)

	state := tracker.GetState("proj1", "feat-unknown")
	if state != nil {
		t.Errorf("expected nil for untracked feature, got %+v", state)
	}
}

// =============================================================================
// Tests: Restart Recovery
// =============================================================================

func TestFeatureTracker_RestartsFeature_AfterCompletion(t *testing.T) {
	client := newMockFeatureClient()
	collector := &eventCollector{}
	tracker := NewFeatureTracker(client, collector.handler)

	// Feature with 1 task
	client.setTasks("proj1", "feat1", makeTasks("feat1", "in_progress"))

	// Start and complete feature
	tracker.HandleEvent(RunnerEvent{
		Type: EventTaskStarted,
		Task: &RunningTask{ID: "task-1", ProjectID: "proj1", FeatureID: "feat1"},
	})
	client.setTasks("proj1", "feat1", makeTasks("feat1", "completed"))
	tracker.HandleEvent(RunnerEvent{
		Type:      EventTaskCompleted,
		Result:    &TaskResult{TaskID: "task-1", Status: TaskResultCompleted},
		ProjectID: "proj1",
		FeatureID: "feat1",
	})

	// Feature completed. Now a new task starts for same feature (new run).
	client.setTasks("proj1", "feat1", makeTasks("feat1", "in_progress", "pending"))
	tracker.HandleEvent(RunnerEvent{
		Type: EventTaskStarted,
		Task: &RunningTask{ID: "task-3", ProjectID: "proj1", FeatureID: "feat1"},
	})

	eventTypes := collector.getEventTypes()
	// Expect: started, completed, started (re-started after completion)
	startedCount := 0
	for _, et := range eventTypes {
		if et == EventFeatureStarted {
			startedCount++
		}
	}
	if startedCount != 2 {
		t.Errorf("expected 2 EventFeatureStarted (original + restart), got %d. Events: %v", startedCount, eventTypes)
	}
}

// =============================================================================
// Tests: Event Metadata
// =============================================================================

func TestFeatureTracker_FeatureStarted_IncludesTaskCount(t *testing.T) {
	client := newMockFeatureClient()
	collector := &eventCollector{}
	tracker := NewFeatureTracker(client, collector.handler)

	client.setTasks("proj1", "feat1", makeTasks("feat1", "in_progress", "pending", "pending", "pending"))

	tracker.HandleEvent(RunnerEvent{
		Type: EventTaskStarted,
		Task: &RunningTask{ID: "task-1", ProjectID: "proj1", FeatureID: "feat1"},
	})

	events := collector.getEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	// ReadyCount is repurposed for total task count in feature events
	if events[0].ReadyCount != 4 {
		t.Errorf("expected ReadyCount=4 (total tasks), got %d", events[0].ReadyCount)
	}
}

// =============================================================================
// Tests: Edge Cases
// =============================================================================

func TestFeatureTracker_IgnoresNonTaskEvents(t *testing.T) {
	client := newMockFeatureClient()
	collector := &eventCollector{}
	tracker := NewFeatureTracker(client, collector.handler)

	// Poll complete, runner started, etc. should be ignored
	tracker.HandleEvent(RunnerEvent{Type: EventPollComplete, RunningCount: 5})
	tracker.HandleEvent(RunnerEvent{Type: EventRunnerStarted, Mode: "headless"})
	tracker.HandleEvent(RunnerEvent{Type: EventStateSaved, Path: "/tmp/state"})

	events := collector.getEvents()
	if len(events) != 0 {
		t.Errorf("expected 0 events for non-task events, got %d", len(events))
	}
}

func TestFeatureTracker_MultipleFeatures_TrackedIndependently(t *testing.T) {
	client := newMockFeatureClient()
	collector := &eventCollector{}
	tracker := NewFeatureTracker(client, collector.handler)

	client.setTasks("proj1", "feat-a", makeTasks("feat-a", "in_progress", "pending"))
	client.setTasks("proj1", "feat-b", makeTasks("feat-b", "in_progress", "pending"))

	tracker.HandleEvent(RunnerEvent{
		Type: EventTaskStarted,
		Task: &RunningTask{ID: "ta-1", ProjectID: "proj1", FeatureID: "feat-a"},
	})
	tracker.HandleEvent(RunnerEvent{
		Type: EventTaskStarted,
		Task: &RunningTask{ID: "tb-1", ProjectID: "proj1", FeatureID: "feat-b"},
	})

	// Complete feat-a
	client.setTasks("proj1", "feat-a", makeTasks("feat-a", "completed", "completed"))
	tracker.HandleEvent(RunnerEvent{
		Type: EventTaskCompleted, Result: &TaskResult{TaskID: "ta-1", Status: TaskResultCompleted},
		ProjectID: "proj1", FeatureID: "feat-a",
	})
	tracker.HandleEvent(RunnerEvent{
		Type: EventTaskCompleted, Result: &TaskResult{TaskID: "ta-2", Status: TaskResultCompleted},
		ProjectID: "proj1", FeatureID: "feat-a",
	})

	// feat-b should still be active
	stateB := tracker.GetState("proj1", "feat-b")
	if stateB == nil {
		t.Fatal("expected feat-b to still be tracked")
	}
	if stateB.Status != "active" {
		t.Errorf("expected feat-b Status=active, got %s", stateB.Status)
	}

	// feat-a should be completed
	stateA := tracker.GetState("proj1", "feat-a")
	if stateA == nil {
		t.Fatal("expected feat-a to still be in state map")
	}
	if stateA.Status != "completed" {
		t.Errorf("expected feat-a Status=completed, got %s", stateA.Status)
	}
}

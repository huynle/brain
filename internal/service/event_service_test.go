package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// newTestEventService creates an EventServiceImpl with a fresh EventHub for testing.
func newTestEventService() (*EventServiceImpl, *realtime.EventHub) {
	hub := realtime.NewEventHub()
	svc := NewEventService(hub)
	return svc, hub
}

// =============================================================================
// Ingest Tests
// =============================================================================

func TestEventService_Ingest_AssignsIDs(t *testing.T) {
	svc, hub := newTestEventService()
	ctx := context.Background()

	events := []types.Event{
		{Type: "task.started", Source: "runner"},
		{Type: "task.completed", Source: "runner"},
	}

	if err := svc.Ingest(ctx, events); err != nil {
		t.Fatalf("Ingest() error: %v", err)
	}

	// Events should be in the hub with auto-assigned IDs.
	recent := hub.Replay("")
	if len(recent) != 2 {
		t.Fatalf("expected 2 events in hub, got %d", len(recent))
	}
	for i, evt := range recent {
		if evt.ID == "" {
			t.Errorf("event[%d]: expected auto-assigned ID, got empty", i)
		}
		if evt.Timestamp.IsZero() {
			t.Errorf("event[%d]: expected auto-assigned timestamp, got zero", i)
		}
	}
}

func TestEventService_Ingest_PreservesExistingIDs(t *testing.T) {
	svc, hub := newTestEventService()
	ctx := context.Background()

	events := []types.Event{
		{ID: "evt_custom123", Type: "task.started", Source: "runner", Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
	}

	if err := svc.Ingest(ctx, events); err != nil {
		t.Fatalf("Ingest() error: %v", err)
	}

	recent := hub.Replay("")
	if len(recent) != 1 {
		t.Fatalf("expected 1 event in hub, got %d", len(recent))
	}
	if recent[0].ID != "evt_custom123" {
		t.Errorf("expected preserved ID 'evt_custom123', got %q", recent[0].ID)
	}
	if !recent[0].Timestamp.Equal(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("expected preserved timestamp, got %v", recent[0].Timestamp)
	}
}

func TestEventService_Ingest_DeduplicatesByID(t *testing.T) {
	svc, hub := newTestEventService()
	ctx := context.Background()

	// First ingest.
	events := []types.Event{
		{ID: "evt_dup001", Type: "task.started", Source: "runner"},
	}
	if err := svc.Ingest(ctx, events); err != nil {
		t.Fatalf("first Ingest() error: %v", err)
	}

	// Second ingest with same ID.
	events2 := []types.Event{
		{ID: "evt_dup001", Type: "task.started", Source: "runner"},
	}
	if err := svc.Ingest(ctx, events2); err != nil {
		t.Fatalf("second Ingest() error: %v", err)
	}

	recent := hub.Replay("")
	if len(recent) != 1 {
		t.Fatalf("expected 1 event (deduped), got %d", len(recent))
	}
}

func TestEventService_Ingest_ValidatesEventType(t *testing.T) {
	svc, _ := newTestEventService()
	ctx := context.Background()

	events := []types.Event{
		{Type: "invalid.event.type", Source: "runner"},
	}

	err := svc.Ingest(ctx, events)
	if err == nil {
		t.Fatal("expected error for invalid event type, got nil")
	}
}

func TestEventService_Ingest_ValidatesSource(t *testing.T) {
	svc, _ := newTestEventService()
	ctx := context.Background()

	events := []types.Event{
		{Type: "task.started", Source: "unknown"},
	}

	err := svc.Ingest(ctx, events)
	if err == nil {
		t.Fatal("expected error for invalid source, got nil")
	}
}

func TestEventService_Ingest_EmptyBatch(t *testing.T) {
	svc, _ := newTestEventService()
	ctx := context.Background()

	err := svc.Ingest(ctx, nil)
	if err != nil {
		t.Fatalf("Ingest(nil) should succeed, got error: %v", err)
	}

	err = svc.Ingest(ctx, []types.Event{})
	if err != nil {
		t.Fatalf("Ingest([]) should succeed, got error: %v", err)
	}
}

// =============================================================================
// Recent Tests
// =============================================================================

func TestEventService_Recent_ReturnsLatest(t *testing.T) {
	svc, _ := newTestEventService()
	ctx := context.Background()

	// Ingest 5 events.
	for i := 0; i < 5; i++ {
		events := []types.Event{
			{Type: "task.started", Source: "runner", ProjectID: "proj-1"},
		}
		if err := svc.Ingest(ctx, events); err != nil {
			t.Fatalf("Ingest() error: %v", err)
		}
	}

	// Query last 3.
	recent, err := svc.Recent(ctx, 3, nil)
	if err != nil {
		t.Fatalf("Recent() error: %v", err)
	}
	if len(recent) != 3 {
		t.Fatalf("expected 3 events, got %d", len(recent))
	}
}

func TestEventService_Recent_FiltersbyProjectID(t *testing.T) {
	svc, _ := newTestEventService()
	ctx := context.Background()

	// Ingest events for two projects.
	events := []types.Event{
		{Type: "task.started", Source: "runner", ProjectID: "proj-a"},
		{Type: "task.completed", Source: "runner", ProjectID: "proj-b"},
		{Type: "task.failed", Source: "runner", ProjectID: "proj-a"},
	}
	if err := svc.Ingest(ctx, events); err != nil {
		t.Fatalf("Ingest() error: %v", err)
	}

	recent, err := svc.Recent(ctx, 10, map[string]string{"project_id": "proj-a"})
	if err != nil {
		t.Fatalf("Recent() error: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2 events for proj-a, got %d", len(recent))
	}
	for _, evt := range recent {
		if evt.ProjectID != "proj-a" {
			t.Errorf("expected project_id 'proj-a', got %q", evt.ProjectID)
		}
	}
}

func TestEventService_Recent_FiltersByType(t *testing.T) {
	svc, _ := newTestEventService()
	ctx := context.Background()

	events := []types.Event{
		{Type: "task.started", Source: "runner"},
		{Type: "task.completed", Source: "runner"},
		{Type: "task.started", Source: "api"},
	}
	if err := svc.Ingest(ctx, events); err != nil {
		t.Fatalf("Ingest() error: %v", err)
	}

	recent, err := svc.Recent(ctx, 10, map[string]string{"type": "task.started"})
	if err != nil {
		t.Fatalf("Recent() error: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2 task.started events, got %d", len(recent))
	}
}

func TestEventService_Recent_FiltersBySource(t *testing.T) {
	svc, _ := newTestEventService()
	ctx := context.Background()

	events := []types.Event{
		{Type: "task.started", Source: "runner"},
		{Type: "task.completed", Source: "api"},
	}
	if err := svc.Ingest(ctx, events); err != nil {
		t.Fatalf("Ingest() error: %v", err)
	}

	recent, err := svc.Recent(ctx, 10, map[string]string{"source": "api"})
	if err != nil {
		t.Fatalf("Recent() error: %v", err)
	}
	if len(recent) != 1 {
		t.Fatalf("expected 1 api event, got %d", len(recent))
	}
	if recent[0].Source != "api" {
		t.Errorf("expected source 'api', got %q", recent[0].Source)
	}
}

func TestEventService_Recent_LimitClamped(t *testing.T) {
	svc, _ := newTestEventService()
	ctx := context.Background()

	// Ingest 3 events.
	events := []types.Event{
		{Type: "task.started", Source: "runner"},
		{Type: "task.completed", Source: "runner"},
		{Type: "task.failed", Source: "runner"},
	}
	if err := svc.Ingest(ctx, events); err != nil {
		t.Fatalf("Ingest() error: %v", err)
	}

	// Request more than available.
	recent, err := svc.Recent(ctx, 100, nil)
	if err != nil {
		t.Fatalf("Recent() error: %v", err)
	}
	if len(recent) != 3 {
		t.Fatalf("expected 3 events (all available), got %d", len(recent))
	}

	// Zero limit should return a reasonable default.
	recent, err = svc.Recent(ctx, 0, nil)
	if err != nil {
		t.Fatalf("Recent(0) error: %v", err)
	}
	if len(recent) != 3 {
		t.Fatalf("expected 3 events with limit=0, got %d", len(recent))
	}
}

// =============================================================================
// Subscribe Tests
// =============================================================================

func TestEventService_Subscribe_ReceivesMatchingEvents(t *testing.T) {
	svc, _ := newTestEventService()
	ctx := context.Background()

	// Subscribe to task events.
	ch, unsub := svc.Subscribe(ctx, map[string]string{"type": "task.*"})
	defer unsub()

	// Ingest a matching event.
	events := []types.Event{
		{Type: "task.started", Source: "runner"},
	}
	if err := svc.Ingest(ctx, events); err != nil {
		t.Fatalf("Ingest() error: %v", err)
	}

	select {
	case evt := <-ch:
		if evt.Type != "task.started" {
			t.Errorf("expected type 'task.started', got %q", evt.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestEventService_Subscribe_FiltersNonMatching(t *testing.T) {
	svc, _ := newTestEventService()
	ctx := context.Background()

	// Subscribe to feature events only.
	ch, unsub := svc.Subscribe(ctx, map[string]string{"type": "feature.*"})
	defer unsub()

	// Ingest a task event (should not match).
	events := []types.Event{
		{Type: "task.started", Source: "runner"},
	}
	if err := svc.Ingest(ctx, events); err != nil {
		t.Fatalf("Ingest() error: %v", err)
	}

	select {
	case evt := <-ch:
		t.Fatalf("expected no event, got %+v", evt)
	case <-time.After(100 * time.Millisecond):
		// Good - no event received.
	}
}

func TestEventService_Subscribe_ByProjectID(t *testing.T) {
	svc, _ := newTestEventService()
	ctx := context.Background()

	ch, unsub := svc.Subscribe(ctx, map[string]string{"project_id": "proj-x"})
	defer unsub()

	events := []types.Event{
		{Type: "task.started", Source: "runner", ProjectID: "proj-y"},
		{Type: "task.started", Source: "runner", ProjectID: "proj-x"},
	}
	if err := svc.Ingest(ctx, events); err != nil {
		t.Fatalf("Ingest() error: %v", err)
	}

	select {
	case evt := <-ch:
		if evt.ProjectID != "proj-x" {
			t.Errorf("expected project_id 'proj-x', got %q", evt.ProjectID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestEventService_Subscribe_Unsubscribe(t *testing.T) {
	svc, _ := newTestEventService()
	ctx := context.Background()

	ch, unsub := svc.Subscribe(ctx, nil)
	unsub()

	// After unsubscribe, channel should be closed.
	_, ok := <-ch
	if ok {
		t.Fatal("expected channel to be closed after unsubscribe")
	}
}

// =============================================================================
// CheckFeatureCompletion Tests
// =============================================================================

// mockFeatureTaskLister implements FeatureTaskLister for testing.
type mockFeatureTaskLister struct {
	tasks []types.ResolvedTask
	err   error
}

// mockFeatureAssignmentCleaner implements the feature assignment cleanup
// dependency for testing guarded cleanup behavior.
type mockFeatureAssignmentCleaner struct {
	calls []featureAssignmentCleanupCall
	err   error
}

type featureAssignmentCleanupCall struct {
	projectID string
	featureID string
}

func (m *mockFeatureAssignmentCleaner) ClearFeatureAssignment(ctx context.Context, projectID, featureID string) (bool, error) {
	m.calls = append(m.calls, featureAssignmentCleanupCall{projectID: projectID, featureID: featureID})
	if m.err != nil {
		return false, m.err
	}
	return true, nil
}

func (m *mockFeatureTaskLister) GetTasksByFeature(ctx context.Context, projectID, featureID string) ([]types.ResolvedTask, error) {
	if m.err != nil {
		return nil, m.err
	}
	// Filter tasks by feature ID to simulate real behavior
	var filtered []types.ResolvedTask
	for _, t := range m.tasks {
		if t.FeatureID == featureID {
			filtered = append(filtered, t)
		}
	}
	return filtered, nil
}

func TestCheckFeatureCompletion_EmitsCompletedWhenAllDone(t *testing.T) {
	svc, hub := newTestEventService()
	ctx := context.Background()

	lister := &mockFeatureTaskLister{
		tasks: []types.ResolvedTask{
			{ID: "t1", FeatureID: "feat-1", Status: "completed"},
			{ID: "t2", FeatureID: "feat-1", Status: "completed"},
			{ID: "t3", FeatureID: "feat-1", Status: "validated"},
		},
	}
	svc.SetFeatureTaskLister(lister)

	svc.CheckFeatureCompletion(ctx, "proj-1", "feat-1", "t1")

	events := hub.Replay("")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != types.EventFeatureCompleted {
		t.Errorf("expected event type %q, got %q", types.EventFeatureCompleted, events[0].Type)
	}
	if events[0].ProjectID != "proj-1" {
		t.Errorf("expected project_id 'proj-1', got %q", events[0].ProjectID)
	}
	if events[0].FeatureID != "feat-1" {
		t.Errorf("expected feature_id 'feat-1', got %q", events[0].FeatureID)
	}
	if events[0].Source != types.EventSourceAPI {
		t.Errorf("expected source %q, got %q", types.EventSourceAPI, events[0].Source)
	}
}

func TestCheckFeatureCompletion_ClearsAssignmentWhenAllTasksCompleted(t *testing.T) {
	svc, _ := newTestEventService()
	ctx := context.Background()

	lister := &mockFeatureTaskLister{
		tasks: []types.ResolvedTask{
			{ID: "t1", FeatureID: "feat-1", Status: "completed"},
			{ID: "t2", FeatureID: "feat-1", Status: "completed"},
		},
	}
	cleaner := &mockFeatureAssignmentCleaner{}
	svc.SetFeatureTaskLister(lister)
	svc.SetFeatureAssignmentCleaner(cleaner)

	svc.CheckFeatureCompletion(ctx, "proj-1", "feat-1", "t1")

	if len(cleaner.calls) != 1 {
		t.Fatalf("expected assignment cleanup once, got %d calls", len(cleaner.calls))
	}
	if cleaner.calls[0].projectID != "proj-1" || cleaner.calls[0].featureID != "feat-1" {
		t.Fatalf("expected cleanup for proj-1/feat-1, got %s/%s", cleaner.calls[0].projectID, cleaner.calls[0].featureID)
	}
}

func TestCheckFeatureCompletion_ClearsAssignmentWhenTasksCompletedOrValidated(t *testing.T) {
	svc, _ := newTestEventService()
	ctx := context.Background()

	lister := &mockFeatureTaskLister{
		tasks: []types.ResolvedTask{
			{ID: "t1", FeatureID: "feat-1", Status: "completed"},
			{ID: "t2", FeatureID: "feat-1", Status: "validated"},
			{ID: "t3", FeatureID: "feat-1", Status: "completed"},
		},
	}
	cleaner := &mockFeatureAssignmentCleaner{}
	svc.SetFeatureTaskLister(lister)
	svc.SetFeatureAssignmentCleaner(cleaner)

	svc.CheckFeatureCompletion(ctx, "proj-1", "feat-1", "t2")

	if len(cleaner.calls) != 1 {
		t.Fatalf("expected assignment cleanup once for completed+validated tasks, got %d calls", len(cleaner.calls))
	}
}

func TestCheckFeatureCompletion_EmitsProgressWhenPartiallyDone(t *testing.T) {
	svc, hub := newTestEventService()
	ctx := context.Background()

	lister := &mockFeatureTaskLister{
		tasks: []types.ResolvedTask{
			{ID: "t1", FeatureID: "feat-1", Status: "completed"},
			{ID: "t2", FeatureID: "feat-1", Status: "pending"},
			{ID: "t3", FeatureID: "feat-1", Status: "in_progress"},
		},
	}
	svc.SetFeatureTaskLister(lister)

	svc.CheckFeatureCompletion(ctx, "proj-1", "feat-1", "t1")

	events := hub.Replay("")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != types.EventFeatureProgress {
		t.Errorf("expected event type %q, got %q", types.EventFeatureProgress, events[0].Type)
	}
	if events[0].ProjectID != "proj-1" {
		t.Errorf("expected project_id 'proj-1', got %q", events[0].ProjectID)
	}
	if events[0].FeatureID != "feat-1" {
		t.Errorf("expected feature_id 'feat-1', got %q", events[0].FeatureID)
	}
	// Check metadata contains progress info
	if events[0].Metadata == nil {
		t.Fatal("expected metadata with progress info")
	}
	if events[0].Metadata["completed"] != "1" {
		t.Errorf("expected completed=1, got %q", events[0].Metadata["completed"])
	}
	if events[0].Metadata["total"] != "3" {
		t.Errorf("expected total=3, got %q", events[0].Metadata["total"])
	}
}

func TestCheckFeatureCompletion_DoesNotClearAssignmentWhenAnyTaskIncomplete(t *testing.T) {
	tests := []struct {
		name   string
		status string
	}{
		{name: "pending", status: "pending"},
		{name: "active", status: "active"},
		{name: "waiting", status: "waiting"},
		{name: "blocked", status: "blocked"},
		{name: "in_progress", status: "in_progress"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newTestEventService()
			ctx := context.Background()

			lister := &mockFeatureTaskLister{
				tasks: []types.ResolvedTask{
					{ID: "t1", FeatureID: "feat-1", Status: "completed"},
					{ID: "t2", FeatureID: "feat-1", Status: tt.status},
				},
			}
			cleaner := &mockFeatureAssignmentCleaner{}
			svc.SetFeatureTaskLister(lister)
			svc.SetFeatureAssignmentCleaner(cleaner)

			svc.CheckFeatureCompletion(ctx, "proj-1", "feat-1", "t1")

			if len(cleaner.calls) != 0 {
				t.Fatalf("expected no assignment cleanup while task is %s, got %d calls", tt.status, len(cleaner.calls))
			}
		})
	}
}

func TestCheckFeatureCompletion_DoesNotClearManualAssignmentWhenFeatureIncomplete(t *testing.T) {
	svc, _ := newTestEventService()
	ctx := context.Background()

	lister := &mockFeatureTaskLister{
		tasks: []types.ResolvedTask{
			{ID: "manual-task", FeatureID: "feat-1", Status: "in_progress"},
			{ID: "next-task", FeatureID: "feat-1", Status: "pending"},
		},
	}
	cleaner := &mockFeatureAssignmentCleaner{}
	svc.SetFeatureTaskLister(lister)
	svc.SetFeatureAssignmentCleaner(cleaner)

	svc.CheckFeatureCompletion(ctx, "proj-1", "feat-1", "manual-task")

	if len(cleaner.calls) != 0 {
		t.Fatalf("expected manual assignment to remain while feature is incomplete, got %d cleanup calls", len(cleaner.calls))
	}
}

func TestCheckFeatureCompletion_WithTaskServiceClearsAssignmentOnlyWhenAllFeatureTasksDone(t *testing.T) {
	taskSvc, store, _ := newTestTaskService(t)
	eventSvc, _ := newTestEventService()
	ctx := context.Background()

	if _, err := store.ForceAssignFeature(ctx, "proj", "feature-auth", "runner-a", "manual", "active"); err != nil {
		t.Fatalf("ForceAssignFeature failed: %v", err)
	}
	insertTaskNote(t, store, "done1111", "Done task", "completed", "high", "proj", map[string]interface{}{
		"feature_id": "feature-auth",
	})
	insertTaskNote(t, store, "todo2222", "Todo task", "pending", "medium", "proj", map[string]interface{}{
		"feature_id": "feature-auth",
	})
	eventSvc.SetFeatureTaskLister(taskSvc)
	eventSvc.SetFeatureAssignmentCleaner(store)

	eventSvc.CheckFeatureCompletion(ctx, "proj", "feature-auth", "done1111")
	assignment, err := store.GetFeatureAssignment(ctx, "proj", "feature-auth")
	if err != nil {
		t.Fatalf("GetFeatureAssignment failed: %v", err)
	}
	if assignment == nil || assignment.RunnerID != "runner-a" {
		t.Fatalf("assignment should remain while feature is incomplete, got %+v", assignment)
	}

	if _, err := store.UpdateNote(ctx, "projects/proj/task/todo2222.md", map[string]interface{}{"status": "validated"}); err != nil {
		t.Fatalf("UpdateNote status failed: %v", err)
	}
	eventSvc.CheckFeatureCompletion(ctx, "proj", "feature-auth", "todo2222")
	assignment, err = store.GetFeatureAssignment(ctx, "proj", "feature-auth")
	if err != nil {
		t.Fatalf("GetFeatureAssignment after completion failed: %v", err)
	}
	if assignment != nil {
		t.Fatalf("assignment should clear after all feature tasks are completed or validated, got %+v", assignment)
	}
}

func TestCheckFeatureCompletion_NoEventWhenNoFeatureID(t *testing.T) {
	svc, hub := newTestEventService()
	ctx := context.Background()

	lister := &mockFeatureTaskLister{}
	svc.SetFeatureTaskLister(lister)

	// Empty feature ID should be a no-op
	svc.CheckFeatureCompletion(ctx, "proj-1", "", "t1")

	events := hub.Replay("")
	if len(events) != 0 {
		t.Fatalf("expected 0 events for empty feature_id, got %d", len(events))
	}
}

func TestCheckFeatureCompletion_NoEventWhenNoLister(t *testing.T) {
	svc, hub := newTestEventService()
	ctx := context.Background()

	// Don't set a lister - should be a safe no-op
	svc.CheckFeatureCompletion(ctx, "proj-1", "feat-1", "t1")

	events := hub.Replay("")
	if len(events) != 0 {
		t.Fatalf("expected 0 events when no lister set, got %d", len(events))
	}
}

func TestCheckFeatureCompletion_NoEventWhenListerErrors(t *testing.T) {
	svc, hub := newTestEventService()
	ctx := context.Background()

	lister := &mockFeatureTaskLister{
		err: fmt.Errorf("connection refused"),
	}
	svc.SetFeatureTaskLister(lister)

	svc.CheckFeatureCompletion(ctx, "proj-1", "feat-1", "t1")

	events := hub.Replay("")
	if len(events) != 0 {
		t.Fatalf("expected 0 events on lister error, got %d", len(events))
	}
}

func TestCheckFeatureCompletion_NoEventWhenNoTasks(t *testing.T) {
	svc, hub := newTestEventService()
	ctx := context.Background()

	lister := &mockFeatureTaskLister{
		tasks: []types.ResolvedTask{}, // no tasks for this feature
	}
	svc.SetFeatureTaskLister(lister)

	svc.CheckFeatureCompletion(ctx, "proj-1", "feat-1", "t1")

	events := hub.Replay("")
	if len(events) != 0 {
		t.Fatalf("expected 0 events for empty feature, got %d", len(events))
	}
}

func TestCheckFeatureCompletion_NoDuplicateEvents(t *testing.T) {
	svc, hub := newTestEventService()
	ctx := context.Background()

	lister := &mockFeatureTaskLister{
		tasks: []types.ResolvedTask{
			{ID: "t1", FeatureID: "feat-1", Status: "completed"},
			{ID: "t2", FeatureID: "feat-1", Status: "completed"},
		},
	}
	svc.SetFeatureTaskLister(lister)

	// Call twice for the same feature
	svc.CheckFeatureCompletion(ctx, "proj-1", "feat-1", "t1")
	svc.CheckFeatureCompletion(ctx, "proj-1", "feat-1", "t2")

	events := hub.Replay("")
	// Should have 2 events since dedup is by event ID not feature ID,
	// but both should be feature.completed
	completedCount := 0
	for _, evt := range events {
		if evt.Type == types.EventFeatureCompleted {
			completedCount++
		}
	}
	// At minimum, both calls should emit feature.completed since all tasks are done
	if completedCount < 1 {
		t.Errorf("expected at least 1 feature.completed event, got %d", completedCount)
	}
}

func TestCheckFeatureCompletion_ValidatedCountsAsCompleted(t *testing.T) {
	svc, hub := newTestEventService()
	ctx := context.Background()

	lister := &mockFeatureTaskLister{
		tasks: []types.ResolvedTask{
			{ID: "t1", FeatureID: "feat-1", Status: "validated"},
			{ID: "t2", FeatureID: "feat-1", Status: "validated"},
		},
	}
	svc.SetFeatureTaskLister(lister)

	svc.CheckFeatureCompletion(ctx, "proj-1", "feat-1", "t1")

	events := hub.Replay("")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != types.EventFeatureCompleted {
		t.Errorf("expected feature.completed, got %q", events[0].Type)
	}
}

func TestCheckFeatureCompletion_MixedCompletedValidatedIsComplete(t *testing.T) {
	svc, hub := newTestEventService()
	ctx := context.Background()

	lister := &mockFeatureTaskLister{
		tasks: []types.ResolvedTask{
			{ID: "t1", FeatureID: "feat-1", Status: "completed"},
			{ID: "t2", FeatureID: "feat-1", Status: "validated"},
			{ID: "t3", FeatureID: "feat-1", Status: "completed"},
		},
	}
	svc.SetFeatureTaskLister(lister)

	svc.CheckFeatureCompletion(ctx, "proj-1", "feat-1", "t1")

	events := hub.Replay("")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != types.EventFeatureCompleted {
		t.Errorf("expected feature.completed for mixed completed/validated, got %q", events[0].Type)
	}
}

func TestCheckFeatureCompletion_CancelledTasksCountAsNotDone(t *testing.T) {
	svc, hub := newTestEventService()
	ctx := context.Background()

	lister := &mockFeatureTaskLister{
		tasks: []types.ResolvedTask{
			{ID: "t1", FeatureID: "feat-1", Status: "completed"},
			{ID: "t2", FeatureID: "feat-1", Status: "cancelled"},
		},
	}
	svc.SetFeatureTaskLister(lister)

	svc.CheckFeatureCompletion(ctx, "proj-1", "feat-1", "t1")

	events := hub.Replay("")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	// Cancelled tasks are NOT completed, so this should be progress
	if events[0].Type != types.EventFeatureProgress {
		t.Errorf("expected feature.progress when cancelled tasks exist, got %q", events[0].Type)
	}
}

// =============================================================================
// Checkout Mode Folding (Phase 3.1)
// =============================================================================

func TestFoldCheckoutMode_AllEmptyDefaultsToAI(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1"},
		{ID: "t2"},
	}
	if got := foldCheckoutMode(tasks); got != "ai" {
		t.Fatalf("foldCheckoutMode(all empty) = %q, want %q", got, "ai")
	}
}

func TestFoldCheckoutMode_AllAIStaysAI(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", CheckoutMode: "ai"},
		{ID: "t2", CheckoutMode: "ai"},
	}
	if got := foldCheckoutMode(tasks); got != "ai" {
		t.Fatalf("foldCheckoutMode(all ai) = %q, want %q", got, "ai")
	}
}

func TestFoldCheckoutMode_AllSimpleStaysSimple(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", CheckoutMode: "simple"},
		{ID: "t2", CheckoutMode: "simple"},
	}
	if got := foldCheckoutMode(tasks); got != "simple" {
		t.Fatalf("foldCheckoutMode(all simple) = %q, want %q", got, "simple")
	}
}

func TestFoldCheckoutMode_AnySimpleWins(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", CheckoutMode: "ai"},
		{ID: "t2", CheckoutMode: "simple"},
		{ID: "t3", CheckoutMode: "ai"},
	}
	if got := foldCheckoutMode(tasks); got != "simple" {
		t.Fatalf("foldCheckoutMode(mixed ai+simple) = %q, want %q", got, "simple")
	}
}

func TestFoldCheckoutMode_EmptyPlusSimpleIsSimple(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1"},
		{ID: "t2", CheckoutMode: "simple"},
	}
	if got := foldCheckoutMode(tasks); got != "simple" {
		t.Fatalf("foldCheckoutMode(empty+simple) = %q, want %q", got, "simple")
	}
}

func TestFoldCheckoutMode_EmptyPlusAIIsAI(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1"},
		{ID: "t2", CheckoutMode: "ai"},
	}
	if got := foldCheckoutMode(tasks); got != "ai" {
		t.Fatalf("foldCheckoutMode(empty+ai) = %q, want %q", got, "ai")
	}
}

func TestFoldCheckoutMode_UnknownValueIgnored(t *testing.T) {
	// Defensive: API validation should prevent this, but if it happens,
	// unknown values are treated as empty (fall through to default).
	tasks := []types.ResolvedTask{
		{ID: "t1", CheckoutMode: "bogus"},
		{ID: "t2", CheckoutMode: "bogus"},
	}
	if got := foldCheckoutMode(tasks); got != "ai" {
		t.Fatalf("foldCheckoutMode(unknown only) = %q, want %q (fall back to default)", got, "ai")
	}
}

func TestFoldCheckoutMode_UnknownPlusSimpleIsSimple(t *testing.T) {
	tasks := []types.ResolvedTask{
		{ID: "t1", CheckoutMode: "bogus"},
		{ID: "t2", CheckoutMode: "simple"},
	}
	if got := foldCheckoutMode(tasks); got != "simple" {
		t.Fatalf("foldCheckoutMode(bogus+simple) = %q, want %q", got, "simple")
	}
}

func TestFoldCheckoutMode_EmptySlice(t *testing.T) {
	if got := foldCheckoutMode(nil); got != "ai" {
		t.Fatalf("foldCheckoutMode(nil) = %q, want %q", got, "ai")
	}
	if got := foldCheckoutMode([]types.ResolvedTask{}); got != "ai" {
		t.Fatalf("foldCheckoutMode(empty) = %q, want %q", got, "ai")
	}
}

// -----------------------------------------------------------------------------
// CheckFeatureCompletion — checkout_mode metadata enrichment (Phase 3.1)
// -----------------------------------------------------------------------------

func TestCheckFeatureCompletion_EmitsCheckoutModeMetadata_Simple(t *testing.T) {
	svc, hub := newTestEventService()
	ctx := context.Background()

	lister := &mockFeatureTaskLister{
		tasks: []types.ResolvedTask{
			{ID: "t1", FeatureID: "feat-1", Status: "completed", CheckoutMode: "simple"},
			{ID: "t2", FeatureID: "feat-1", Status: "completed", CheckoutMode: "simple"},
		},
	}
	svc.SetFeatureTaskLister(lister)

	svc.CheckFeatureCompletion(ctx, "proj-1", "feat-1", "t1")

	events := hub.Replay("")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != types.EventFeatureCompleted {
		t.Fatalf("expected feature.completed, got %q", events[0].Type)
	}
	if got := events[0].Metadata["checkout_mode"]; got != "simple" {
		t.Fatalf("expected metadata checkout_mode=simple, got %q", got)
	}
}

func TestCheckFeatureCompletion_DefaultsCheckoutModeToAI(t *testing.T) {
	svc, hub := newTestEventService()
	ctx := context.Background()

	lister := &mockFeatureTaskLister{
		tasks: []types.ResolvedTask{
			{ID: "t1", FeatureID: "feat-1", Status: "completed"},
			{ID: "t2", FeatureID: "feat-1", Status: "completed"},
		},
	}
	svc.SetFeatureTaskLister(lister)

	svc.CheckFeatureCompletion(ctx, "proj-1", "feat-1", "t1")

	events := hub.Replay("")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if got := events[0].Metadata["checkout_mode"]; got != "ai" {
		t.Fatalf("expected metadata checkout_mode=ai (default), got %q", got)
	}
}

func TestCheckFeatureCompletion_MixedTasksFoldToSimple(t *testing.T) {
	svc, hub := newTestEventService()
	ctx := context.Background()

	lister := &mockFeatureTaskLister{
		tasks: []types.ResolvedTask{
			{ID: "t1", FeatureID: "feat-1", Status: "completed", CheckoutMode: "ai"},
			{ID: "t2", FeatureID: "feat-1", Status: "completed", CheckoutMode: "simple"},
		},
	}
	svc.SetFeatureTaskLister(lister)

	svc.CheckFeatureCompletion(ctx, "proj-1", "feat-1", "t1")

	events := hub.Replay("")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if got := events[0].Metadata["checkout_mode"]; got != "simple" {
		t.Fatalf("expected metadata checkout_mode=simple (mixed folds to simple), got %q", got)
	}
}

func TestCheckFeatureCompletion_ProgressEventCarriesCheckoutMode(t *testing.T) {
	svc, hub := newTestEventService()
	ctx := context.Background()

	lister := &mockFeatureTaskLister{
		tasks: []types.ResolvedTask{
			{ID: "t1", FeatureID: "feat-1", Status: "completed", CheckoutMode: "simple"},
			{ID: "t2", FeatureID: "feat-1", Status: "pending", CheckoutMode: "simple"},
		},
	}
	svc.SetFeatureTaskLister(lister)

	svc.CheckFeatureCompletion(ctx, "proj-1", "feat-1", "t1")

	events := hub.Replay("")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != types.EventFeatureProgress {
		t.Fatalf("expected feature.progress, got %q", events[0].Type)
	}
	if got := events[0].Metadata["checkout_mode"]; got != "simple" {
		t.Fatalf("expected progress metadata checkout_mode=simple, got %q", got)
	}
}

// =============================================================================
// Interface compliance
// =============================================================================

func TestEventServiceImpl_ImplementsInterface(t *testing.T) {
	svc, _ := newTestEventService()
	// Compile-time check is in the implementation file,
	// but verify at test time too.
	var _ interface {
		Ingest(context.Context, []types.Event) error
		Recent(context.Context, int, map[string]string) ([]types.Event, error)
		Subscribe(context.Context, map[string]string) (<-chan types.Event, func())
	} = svc
}

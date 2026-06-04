package service

import (
	"context"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Test Helpers
// =============================================================================

// mockTriggerTaskStore implements TriggerTaskStore for testing.
type mockTriggerTaskStore struct {
	entries      []types.BrainEntry
	updatedPaths []string
	mergedFields map[string]map[string]interface{}
	listErr      error
	mergeErr     error
}

func newMockTriggerTaskStore() *mockTriggerTaskStore {
	return &mockTriggerTaskStore{
		mergedFields: make(map[string]map[string]interface{}),
	}
}

func (m *mockTriggerTaskStore) ListTriggeredTasks(ctx context.Context) ([]types.BrainEntry, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []types.BrainEntry
	for _, e := range m.entries {
		if e.Trigger != nil && len(e.Trigger.EventPatterns()) > 0 {
			result = append(result, e)
		}
	}
	return result, nil
}

func (m *mockTriggerTaskStore) ActivateTask(ctx context.Context, path string, fields map[string]interface{}) error {
	if m.mergeErr != nil {
		return m.mergeErr
	}
	m.updatedPaths = append(m.updatedPaths, path)
	m.mergedFields[path] = fields
	return nil
}

func (m *mockTriggerTaskStore) CountInProgressByTrigger(ctx context.Context, triggerEvent, projectID string) (int, error) {
	count := 0
	for _, e := range m.entries {
		if e.Status == "in_progress" && e.Trigger != nil {
			if types.MatchEventPattern(e.Trigger.Event, triggerEvent) || types.MatchEventPattern(triggerEvent, e.Trigger.Event) {
				if projectID == "" || e.ProjectID == projectID {
					count++
				}
			}
		}
	}
	return count, nil
}

// =============================================================================
// Event Matching Tests
// =============================================================================

func TestTriggerService_MatchesExactEventType(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:      "projects/myproj/task/abc.md",
			ID:        "abc12345",
			Status:    "active",
			ProjectID: "myproj",
			Trigger:   &types.TriggerConfig{Event: "task.completed"},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:        "evt_test1",
		Type:      "task.completed",
		Source:    "runner",
		ProjectID: "myproj",
		Timestamp: time.Now().UTC(),
	}

	results, err := svc.Evaluate(context.Background(), evt)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].TaskPath != "projects/myproj/task/abc.md" {
		t.Errorf("expected task path 'projects/myproj/task/abc.md', got %q", results[0].TaskPath)
	}
	if !results[0].Matched {
		t.Error("expected Matched=true")
	}
}

func TestTriggerService_MatchesWildcardEventType(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:      "projects/myproj/task/abc.md",
			ID:        "abc12345",
			Status:    "active",
			ProjectID: "myproj",
			Trigger:   &types.TriggerConfig{Event: "task.*"},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:        "evt_test2",
		Type:      "task.started",
		Source:    "runner",
		ProjectID: "myproj",
		Timestamp: time.Now().UTC(),
	}

	results, err := svc.Evaluate(context.Background(), evt)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestTriggerService_NoMatchOnDifferentEventType(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:      "projects/myproj/task/abc.md",
			ID:        "abc12345",
			Status:    "active",
			ProjectID: "myproj",
			Trigger:   &types.TriggerConfig{Event: "task.completed"},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:     "evt_test3",
		Type:   "feature.completed",
		Source: "api",
	}

	results, err := svc.Evaluate(context.Background(), evt)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestTriggerService_MatchesAnyOfMultipleEvents(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		want      int
	}{
		{name: "first event in set", eventType: "task.completed", want: 1},
		{name: "second event in set", eventType: "feature.completed", want: 1},
		{name: "event not in set", eventType: "task.started", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockTriggerTaskStore()
			store.entries = []types.BrainEntry{
				{
					Path:      "projects/myproj/task/abc.md",
					ID:        "abc12345",
					Status:    "active",
					ProjectID: "myproj",
					Trigger: &types.TriggerConfig{
						Event:  "task.completed",
						Events: []string{"feature.completed"},
					},
				},
			}
			svc := NewTriggerService(store)

			evt := types.Event{
				ID:        "evt_multi_" + tt.eventType,
				Type:      tt.eventType,
				Source:    "runner",
				ProjectID: "myproj",
			}

			results, err := svc.Evaluate(context.Background(), evt)
			if err != nil {
				t.Fatalf("Evaluate() error: %v", err)
			}
			if len(results) != tt.want {
				t.Fatalf("event %q: expected %d results, got %d", tt.eventType, tt.want, len(results))
			}
		})
	}
}

func TestTriggerService_MatchesEventsOnlyTrigger(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:      "projects/myproj/task/abc.md",
			ID:        "abc12345",
			Status:    "active",
			ProjectID: "myproj",
			Trigger: &types.TriggerConfig{
				Events: []string{"task.completed", "feature.completed"},
			},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:        "evt_events_only",
		Type:      "feature.completed",
		Source:    "runner",
		ProjectID: "myproj",
	}

	results, err := svc.Evaluate(context.Background(), evt)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for events-only trigger, got %d", len(results))
	}
}

func TestTriggerService_MatchesOrableStatusFilter(t *testing.T) {
	tests := []struct {
		name     string
		toStatus string
		want     int
	}{
		{name: "completed in set", toStatus: "completed", want: 1},
		{name: "blocked in set", toStatus: "blocked", want: 1},
		{name: "active not in set", toStatus: "active", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockTriggerTaskStore()
			store.entries = []types.BrainEntry{
				{
					Path:      "projects/myproj/task/abc.md",
					ID:        "abc12345",
					Status:    "active",
					ProjectID: "myproj",
					Trigger: &types.TriggerConfig{
						Event:  "task.status_changed",
						Filter: map[string]string{"to_status": "in:completed,blocked"},
					},
				},
			}
			svc := NewTriggerService(store)

			evt := types.Event{
				ID:        "evt_or_status_" + tt.toStatus,
				Type:      "task.status_changed",
				Source:    "runner",
				ProjectID: "myproj",
				ToStatus:  tt.toStatus,
			}

			results, err := svc.Evaluate(context.Background(), evt)
			if err != nil {
				t.Fatalf("Evaluate() error: %v", err)
			}
			if len(results) != tt.want {
				t.Fatalf("to_status=%q: expected %d results, got %d", tt.toStatus, tt.want, len(results))
			}
		})
	}
}

func TestTriggerService_CombinesMultiEventAndOrableStatus(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		toStatus  string
		want      int
	}{
		{name: "task event + completed", eventType: "task.status_changed", toStatus: "completed", want: 1},
		{name: "feature event + blocked", eventType: "feature.status_changed", toStatus: "blocked", want: 1},
		{name: "matching event but status not in set", eventType: "task.status_changed", toStatus: "active", want: 0},
		{name: "status in set but event not in set", eventType: "task.started", toStatus: "completed", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockTriggerTaskStore()
			store.entries = []types.BrainEntry{
				{
					Path:      "projects/myproj/task/abc.md",
					ID:        "abc12345",
					Status:    "active",
					ProjectID: "myproj",
					Trigger: &types.TriggerConfig{
						Event:  "task.status_changed",
						Events: []string{"feature.status_changed"},
						Filter: map[string]string{"to_status": "in:completed,blocked"},
					},
				},
			}
			svc := NewTriggerService(store)

			evt := types.Event{
				ID:        "evt_combo_" + tt.name,
				Type:      tt.eventType,
				Source:    "runner",
				ProjectID: "myproj",
				ToStatus:  tt.toStatus,
			}

			results, err := svc.Evaluate(context.Background(), evt)
			if err != nil {
				t.Fatalf("Evaluate() error: %v", err)
			}
			if len(results) != tt.want {
				t.Fatalf("%s: expected %d results, got %d", tt.name, tt.want, len(results))
			}
		})
	}
}

// =============================================================================
// Filter Matching Tests
// =============================================================================

func TestTriggerService_FilterMatchesProjectID(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:      "projects/myproj/task/abc.md",
			ID:        "abc12345",
			Status:    "active",
			ProjectID: "myproj",
			Trigger: &types.TriggerConfig{
				Event:  "task.completed",
				Filter: map[string]string{"project_id": "target-proj"},
			},
		},
	}
	svc := NewTriggerService(store)

	// Matching project_id
	evt := types.Event{
		ID:        "evt_test4",
		Type:      "task.completed",
		Source:    "runner",
		ProjectID: "target-proj",
	}

	results, err := svc.Evaluate(context.Background(), evt)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result with matching project_id, got %d", len(results))
	}

	// Non-matching project_id
	evt2 := types.Event{
		ID:        "evt_test5",
		Type:      "task.completed",
		Source:    "runner",
		ProjectID: "other-proj",
	}

	results2, err := svc.Evaluate(context.Background(), evt2)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if len(results2) != 0 {
		t.Fatalf("expected 0 results with non-matching project_id, got %d", len(results2))
	}
}

func TestTriggerService_FilterMatchesFeatureID(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:      "projects/myproj/task/abc.md",
			ID:        "abc12345",
			Status:    "active",
			ProjectID: "myproj",
			Trigger: &types.TriggerConfig{
				Event:  "task.completed",
				Filter: map[string]string{"feature_id": "auth-system"},
			},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:        "evt_test6",
		Type:      "task.completed",
		Source:    "runner",
		FeatureID: "auth-system",
	}

	results, err := svc.Evaluate(context.Background(), evt)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result with matching feature_id, got %d", len(results))
	}
}

func TestTriggerService_FilterMatchesMetadata(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:      "projects/myproj/task/abc.md",
			ID:        "abc12345",
			Status:    "active",
			ProjectID: "myproj",
			Trigger: &types.TriggerConfig{
				Event:  "task.completed",
				Filter: map[string]string{"environment": "production"},
			},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:       "evt_test7",
		Type:     "task.completed",
		Source:   "runner",
		Metadata: map[string]string{"environment": "production"},
	}

	results, err := svc.Evaluate(context.Background(), evt)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result with matching metadata, got %d", len(results))
	}
}

func TestTriggerService_FilterMultipleFieldsMustAllMatch(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:      "projects/myproj/task/abc.md",
			ID:        "abc12345",
			Status:    "active",
			ProjectID: "myproj",
			Trigger: &types.TriggerConfig{
				Event: "task.completed",
				Filter: map[string]string{
					"project_id": "target-proj",
					"feature_id": "auth",
				},
			},
		},
	}
	svc := NewTriggerService(store)

	// Both match
	evt1 := types.Event{
		ID:        "evt_test8",
		Type:      "task.completed",
		Source:    "runner",
		ProjectID: "target-proj",
		FeatureID: "auth",
	}
	results1, _ := svc.Evaluate(context.Background(), evt1)
	if len(results1) != 1 {
		t.Fatalf("expected 1 result when all filters match, got %d", len(results1))
	}

	// Only one matches
	evt2 := types.Event{
		ID:        "evt_test9",
		Type:      "task.completed",
		Source:    "runner",
		ProjectID: "target-proj",
		FeatureID: "billing",
	}
	results2, _ := svc.Evaluate(context.Background(), evt2)
	if len(results2) != 0 {
		t.Fatalf("expected 0 results when only partial filter match, got %d", len(results2))
	}
}

// =============================================================================
// Cooldown Tests
// =============================================================================

func TestTriggerService_CooldownPreventsReTriggering(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:      "projects/myproj/task/abc.md",
			ID:        "abc12345",
			Status:    "active",
			ProjectID: "myproj",
			Trigger: &types.TriggerConfig{
				Event:    "task.completed",
				Cooldown: "5m",
			},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:        "evt_cool1",
		Type:      "task.completed",
		Source:    "runner",
		Timestamp: time.Now().UTC(),
	}

	// First evaluation should match
	results, err := svc.Evaluate(context.Background(), evt)
	if err != nil {
		t.Fatalf("first Evaluate() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result on first eval, got %d", len(results))
	}

	// Activate to record the trigger time
	svc.Activate(context.Background(), results)

	// Second evaluation within cooldown should not match
	evt2 := types.Event{
		ID:        "evt_cool2",
		Type:      "task.completed",
		Source:    "runner",
		Timestamp: time.Now().UTC(),
	}
	results2, err := svc.Evaluate(context.Background(), evt2)
	if err != nil {
		t.Fatalf("second Evaluate() error: %v", err)
	}
	if len(results2) != 0 {
		t.Fatalf("expected 0 results within cooldown, got %d", len(results2))
	}
}

func TestTriggerService_CooldownExpiresAllowsReTriggering(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:      "projects/myproj/task/abc.md",
			ID:        "abc12345",
			Status:    "active",
			ProjectID: "myproj",
			Trigger: &types.TriggerConfig{
				Event:    "task.completed",
				Cooldown: "1s",
			},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:        "evt_cool3",
		Type:      "task.completed",
		Source:    "runner",
		Timestamp: time.Now().UTC(),
	}

	results, _ := svc.Evaluate(context.Background(), evt)
	svc.Activate(context.Background(), results)

	// Wait for cooldown to expire
	time.Sleep(1100 * time.Millisecond)

	evt2 := types.Event{
		ID:        "evt_cool4",
		Type:      "task.completed",
		Source:    "runner",
		Timestamp: time.Now().UTC(),
	}
	results2, err := svc.Evaluate(context.Background(), evt2)
	if err != nil {
		t.Fatalf("Evaluate() error after cooldown: %v", err)
	}
	if len(results2) != 1 {
		t.Fatalf("expected 1 result after cooldown expiry, got %d", len(results2))
	}
}

func TestTriggerService_InvalidCooldownIgnored(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:      "projects/myproj/task/abc.md",
			ID:        "abc12345",
			Status:    "active",
			ProjectID: "myproj",
			Trigger: &types.TriggerConfig{
				Event:    "task.completed",
				Cooldown: "invalid-duration",
			},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:     "evt_cool5",
		Type:   "task.completed",
		Source: "runner",
	}

	// Invalid cooldown should be treated as zero (no cooldown)
	results, err := svc.Evaluate(context.Background(), evt)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result with invalid cooldown, got %d", len(results))
	}
}

// =============================================================================
// MaxConcurrent Tests
// =============================================================================

func TestTriggerService_MaxConcurrentPreventsActivation(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:      "projects/myproj/task/abc.md",
			ID:        "abc12345",
			Status:    "active",
			ProjectID: "myproj",
			Trigger: &types.TriggerConfig{
				Event:         "task.completed",
				MaxConcurrent: 1,
			},
		},
		// An in_progress instance with the same trigger
		{
			Path:      "projects/myproj/task/running.md",
			ID:        "run12345",
			Status:    "in_progress",
			ProjectID: "myproj",
			Trigger: &types.TriggerConfig{
				Event: "task.completed",
			},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:     "evt_conc1",
		Type:   "task.completed",
		Source: "runner",
	}

	results, err := svc.Evaluate(context.Background(), evt)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}

	// abc12345 should be skipped due to max_concurrent, running.md matched but is already in_progress
	matchedActive := 0
	for _, r := range results {
		if r.Matched && r.TaskPath == "projects/myproj/task/abc.md" {
			matchedActive++
		}
	}
	if matchedActive != 0 {
		t.Fatalf("expected abc.md to be skipped due to max_concurrent, got %d matches", matchedActive)
	}
}

func TestTriggerService_MaxConcurrentZeroMeansUnlimited(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:      "projects/myproj/task/abc.md",
			ID:        "abc12345",
			Status:    "active",
			ProjectID: "myproj",
			Trigger: &types.TriggerConfig{
				Event:         "task.completed",
				MaxConcurrent: 0,
			},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:     "evt_conc2",
		Type:   "task.completed",
		Source: "runner",
	}

	results, err := svc.Evaluate(context.Background(), evt)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result with max_concurrent=0, got %d", len(results))
	}
}

// =============================================================================
// Template Interpolation Tests
// =============================================================================

func TestTriggerService_TemplateInterpolatesEventData(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:         "projects/myproj/task/abc.md",
			ID:           "abc12345",
			Status:       "active",
			ProjectID:    "myproj",
			DirectPrompt: "Review task {{.TaskID}} in project {{.ProjectID}} (was {{.FromStatus}}, now {{.ToStatus}})",
			Trigger:      &types.TriggerConfig{Event: "task.completed"},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:         "evt_tmpl1",
		Type:       "task.completed",
		Source:     "runner",
		ProjectID:  "target-proj",
		TaskID:     "t1234567",
		FromStatus: "in_progress",
		ToStatus:   "completed",
	}

	results, err := svc.Evaluate(context.Background(), evt)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	expected := "Review task t1234567 in project target-proj (was in_progress, now completed)"
	if results[0].InterpolatedPrompt != expected {
		t.Errorf("expected interpolated prompt %q, got %q", expected, results[0].InterpolatedPrompt)
	}
}

func TestTriggerService_TemplateWithMetadataAccess(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:         "projects/myproj/task/abc.md",
			ID:           "abc12345",
			Status:       "active",
			ProjectID:    "myproj",
			DirectPrompt: "Handle event {{.Type}} from {{.Source}}",
			Trigger:      &types.TriggerConfig{Event: "task.*"},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:     "evt_tmpl2",
		Type:   "task.failed",
		Source: "runner",
	}

	results, err := svc.Evaluate(context.Background(), evt)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	expected := "Handle event task.failed from runner"
	if results[0].InterpolatedPrompt != expected {
		t.Errorf("expected %q, got %q", expected, results[0].InterpolatedPrompt)
	}
}

func TestTriggerService_NoTemplateReturnsOriginalPrompt(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:         "projects/myproj/task/abc.md",
			ID:           "abc12345",
			Status:       "active",
			ProjectID:    "myproj",
			DirectPrompt: "Static prompt with no template syntax",
			Trigger:      &types.TriggerConfig{Event: "task.completed"},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:     "evt_tmpl3",
		Type:   "task.completed",
		Source: "runner",
	}

	results, err := svc.Evaluate(context.Background(), evt)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if results[0].InterpolatedPrompt != "Static prompt with no template syntax" {
		t.Errorf("expected original prompt, got %q", results[0].InterpolatedPrompt)
	}
}

func TestTriggerService_InvalidTemplateReturnsOriginal(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:         "projects/myproj/task/abc.md",
			ID:           "abc12345",
			Status:       "active",
			ProjectID:    "myproj",
			DirectPrompt: "Bad template {{.NonExistent}}",
			Trigger:      &types.TriggerConfig{Event: "task.completed"},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:     "evt_tmpl4",
		Type:   "task.completed",
		Source: "runner",
	}

	results, err := svc.Evaluate(context.Background(), evt)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	// Should fall back to original on template error
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

// =============================================================================
// Activate Tests
// =============================================================================

func TestTriggerService_ActivateSetsTaskToPending(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:      "projects/myproj/task/abc.md",
			ID:        "abc12345",
			Status:    "active",
			ProjectID: "myproj",
			Trigger:   &types.TriggerConfig{Event: "task.completed"},
		},
	}
	svc := NewTriggerService(store)

	results := []TriggerResult{
		{
			TaskPath:           "projects/myproj/task/abc.md",
			TaskID:             "abc12345",
			Matched:            true,
			InterpolatedPrompt: "Do the thing",
		},
	}

	activated, err := svc.Activate(context.Background(), results)
	if err != nil {
		t.Fatalf("Activate() error: %v", err)
	}
	if activated != 1 {
		t.Fatalf("expected 1 activated, got %d", activated)
	}

	if len(store.updatedPaths) != 1 {
		t.Fatalf("expected 1 updated path, got %d", len(store.updatedPaths))
	}
	if store.updatedPaths[0] != "projects/myproj/task/abc.md" {
		t.Errorf("expected updated path 'projects/myproj/task/abc.md', got %q", store.updatedPaths[0])
	}

	fields := store.mergedFields["projects/myproj/task/abc.md"]
	if fields == nil {
		t.Fatal("expected merged fields for task path")
	}
	if fields["status"] != "pending" {
		t.Errorf("expected status 'pending', got %v", fields["status"])
	}
}

func TestTriggerService_ActivateSkipsNonMatched(t *testing.T) {
	store := newMockTriggerTaskStore()
	svc := NewTriggerService(store)

	results := []TriggerResult{
		{
			TaskPath: "projects/myproj/task/abc.md",
			TaskID:   "abc12345",
			Matched:  false,
			Reason:   "cooldown",
		},
	}

	activated, err := svc.Activate(context.Background(), results)
	if err != nil {
		t.Fatalf("Activate() error: %v", err)
	}
	if activated != 0 {
		t.Fatalf("expected 0 activated, got %d", activated)
	}
	if len(store.updatedPaths) != 0 {
		t.Fatalf("expected no updates, got %d", len(store.updatedPaths))
	}
}

// =============================================================================
// Skip Non-Active Tasks
// =============================================================================

func TestTriggerService_SkipsNonActiveOrCompletedTasks(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:      "projects/myproj/task/pending.md",
			ID:        "pend1234",
			Status:    "pending",
			ProjectID: "myproj",
			Trigger:   &types.TriggerConfig{Event: "task.completed"},
		},
		{
			Path:      "projects/myproj/task/in_progress.md",
			ID:        "inpr1234",
			Status:    "in_progress",
			ProjectID: "myproj",
			Trigger:   &types.TriggerConfig{Event: "task.completed"},
		},
		{
			Path:      "projects/myproj/task/active.md",
			ID:        "actv1234",
			Status:    "active",
			ProjectID: "myproj",
			Trigger:   &types.TriggerConfig{Event: "task.completed"},
		},
		{
			Path:      "projects/myproj/task/completed.md",
			ID:        "comp1234",
			Status:    "completed",
			ProjectID: "myproj",
			Trigger:   &types.TriggerConfig{Event: "task.completed"},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:     "evt_skip1",
		Type:   "task.completed",
		Source: "runner",
	}

	results, err := svc.Evaluate(context.Background(), evt)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}

	// Only active and completed tasks should be eligible for triggering
	matchedPaths := make(map[string]bool)
	for _, r := range results {
		if r.Matched {
			matchedPaths[r.TaskPath] = true
		}
	}

	if !matchedPaths["projects/myproj/task/active.md"] {
		t.Error("expected active task to match")
	}
	if !matchedPaths["projects/myproj/task/completed.md"] {
		t.Error("expected completed task to match (for recurring triggers)")
	}
	if matchedPaths["projects/myproj/task/pending.md"] {
		t.Error("pending task should not match")
	}
	if matchedPaths["projects/myproj/task/in_progress.md"] {
		t.Error("in_progress task should not match")
	}
}

// =============================================================================
// GlobalWildcard Tests
// =============================================================================

func TestTriggerService_GlobalWildcardMatchesAll(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:      "projects/myproj/task/abc.md",
			ID:        "abc12345",
			Status:    "active",
			ProjectID: "myproj",
			Trigger:   &types.TriggerConfig{Event: "*"},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:     "evt_glob1",
		Type:   "runner.started",
		Source: "runner",
	}

	results, err := svc.Evaluate(context.Background(), evt)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result with global wildcard, got %d", len(results))
	}
}

// =============================================================================
// Self-Trigger Prevention
// =============================================================================

func TestTriggerService_PreventsSelfTrigger(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:      "projects/myproj/task/abc.md",
			ID:        "abc12345",
			Status:    "active",
			ProjectID: "myproj",
			Trigger:   &types.TriggerConfig{Event: "task.completed"},
		},
	}
	svc := NewTriggerService(store)

	// Event about the same task
	evt := types.Event{
		ID:       "evt_self1",
		Type:     "task.completed",
		Source:   "runner",
		TaskID:   "abc12345",
		TaskPath: "projects/myproj/task/abc.md",
	}

	results, err := svc.Evaluate(context.Background(), evt)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	// Should not trigger itself
	if len(results) != 0 {
		t.Fatalf("expected 0 results (self-trigger prevented), got %d", len(results))
	}
}

// =============================================================================
// Error Handling
// =============================================================================

func TestTriggerService_StoreErrorReturnedFromEvaluate(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.listErr = context.DeadlineExceeded
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:     "evt_err1",
		Type:   "task.completed",
		Source: "runner",
	}

	_, err := svc.Evaluate(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error from store, got nil")
	}
}

// =============================================================================
// HandleEvent Tests
// =============================================================================

func TestTriggerService_HandleEventEvaluatesAndActivates(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:         "projects/myproj/task/abc.md",
			ID:           "abc12345",
			Status:       "active",
			ProjectID:    "myproj",
			DirectPrompt: "Run deployment for {{.ProjectID}}",
			Trigger:      &types.TriggerConfig{Event: "task.completed"},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:        "evt_handle1",
		Type:      "task.completed",
		Source:    "runner",
		ProjectID: "target-proj",
		Timestamp: time.Now().UTC(),
	}

	err := svc.HandleEvent(context.Background(), evt)
	if err != nil {
		t.Fatalf("HandleEvent() error: %v", err)
	}

	// Verify the task was activated (set to pending).
	if len(store.updatedPaths) != 1 {
		t.Fatalf("expected 1 activated task, got %d", len(store.updatedPaths))
	}
	if store.updatedPaths[0] != "projects/myproj/task/abc.md" {
		t.Errorf("wrong activated path: %q", store.updatedPaths[0])
	}

	fields := store.mergedFields["projects/myproj/task/abc.md"]
	if fields["status"] != "pending" {
		t.Errorf("expected status 'pending', got %v", fields["status"])
	}
	if fields["direct_prompt"] != "Run deployment for target-proj" {
		t.Errorf("expected interpolated prompt, got %v", fields["direct_prompt"])
	}
}

func TestTriggerService_HandleEventNoMatchDoesNothing(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:      "projects/myproj/task/abc.md",
			ID:        "abc12345",
			Status:    "active",
			ProjectID: "myproj",
			Trigger:   &types.TriggerConfig{Event: "feature.completed"},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:     "evt_handle2",
		Type:   "task.completed", // does not match feature.completed
		Source: "runner",
	}

	err := svc.HandleEvent(context.Background(), evt)
	if err != nil {
		t.Fatalf("HandleEvent() error: %v", err)
	}

	// Nothing should have been activated.
	if len(store.updatedPaths) != 0 {
		t.Fatalf("expected 0 activated tasks, got %d", len(store.updatedPaths))
	}
}

func TestTriggerService_HandleEventReturnsStoreError(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.listErr = context.DeadlineExceeded
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:     "evt_handle3",
		Type:   "task.completed",
		Source: "runner",
	}

	err := svc.HandleEvent(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error from HandleEvent, got nil")
	}
}

// =============================================================================
// Multiple Matching Tasks
// =============================================================================

func TestTriggerService_MultipleTasksCanMatch(t *testing.T) {
	store := newMockTriggerTaskStore()
	store.entries = []types.BrainEntry{
		{
			Path:      "projects/myproj/task/task1.md",
			ID:        "task0001",
			Status:    "active",
			ProjectID: "myproj",
			Trigger:   &types.TriggerConfig{Event: "task.completed"},
		},
		{
			Path:      "projects/myproj/task/task2.md",
			ID:        "task0002",
			Status:    "active",
			ProjectID: "myproj",
			Trigger:   &types.TriggerConfig{Event: "task.*"},
		},
		{
			Path:      "projects/myproj/task/task3.md",
			ID:        "task0003",
			Status:    "active",
			ProjectID: "myproj",
			Trigger:   &types.TriggerConfig{Event: "feature.completed"},
		},
	}
	svc := NewTriggerService(store)

	evt := types.Event{
		ID:     "evt_multi1",
		Type:   "task.completed",
		Source: "runner",
	}

	results, err := svc.Evaluate(context.Background(), evt)
	if err != nil {
		t.Fatalf("Evaluate() error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 matching results, got %d", len(results))
	}
}

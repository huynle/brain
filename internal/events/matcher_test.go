package events

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock AutomationSource
// =============================================================================

type mockAutomationSource struct {
	mu      sync.Mutex
	entries []AutomationEntry
	err     error
}

func (m *mockAutomationSource) ListActiveAutomations(ctx context.Context) ([]AutomationEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	result := make([]AutomationEntry, len(m.entries))
	copy(result, m.entries)
	return result, nil
}

func (m *mockAutomationSource) setEntries(entries []AutomationEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = entries
}

// =============================================================================
// Mock TaskCreator
// =============================================================================

type createdTask struct {
	AutomationID string
	Req          types.CreateEntryRequest
}

type updatedEntry struct {
	PathOrID string
	Req      types.UpdateEntryRequest
}

type mockTaskCreator struct {
	mu       sync.Mutex
	created  []createdTask
	updated  []updatedEntry
	err      error
	existing map[string]bool // generatedKey -> exists (for dedup checks)
}

func newMockTaskCreator() *mockTaskCreator {
	return &mockTaskCreator{
		existing: make(map[string]bool),
	}
}

func (m *mockTaskCreator) CreateTask(ctx context.Context, automationID string, req types.CreateEntryRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.created = append(m.created, createdTask{AutomationID: automationID, Req: req})
	return nil
}

func (m *mockTaskCreator) UpdateEntry(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.updated = append(m.updated, updatedEntry{PathOrID: pathOrID, Req: req})
	return nil
}

func (m *mockTaskCreator) HasGeneratedKey(ctx context.Context, project, generatedKey string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.existing[generatedKey], nil
}

func (m *mockTaskCreator) getCreated() []createdTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]createdTask, len(m.created))
	copy(result, m.created)
	return result
}

func (m *mockTaskCreator) getUpdated() []updatedEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]updatedEntry, len(m.updated))
	copy(result, m.updated)
	return result
}

// =============================================================================
// Tests
// =============================================================================

func TestAutomationMatcher_MatchesEventAndCreatesTask(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "auto1",
				Path:      "projects/test/automation/auto1.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:  "event",
					Event: "task.completed",
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Review the completed task {{.TaskID}}",
					Agent:        "tdd-dev",
				},
				Status: "active",
			},
		},
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)

	// Allow subscription setup
	time.Sleep(50 * time.Millisecond)

	// Publish an event that matches
	bus.Publish(Event{
		Type:      TaskCompleted,
		ProjectID: "test",
		Payload: map[string]any{
			"id":   "task123",
			"path": "projects/test/task/task123.md",
		},
	})

	// Wait for async processing
	require.Eventually(t, func() bool {
		return len(creator.getCreated()) > 0
	}, 3*time.Second, 50*time.Millisecond, "expected task to be created")

	created := creator.getCreated()
	assert.Len(t, created, 1)
	assert.Equal(t, "auto1", created[0].AutomationID)
	assert.Equal(t, "task", created[0].Req.Type)
	assert.Equal(t, "pending", created[0].Req.Status)
	assert.Equal(t, "test", created[0].Req.Project)
	assert.NotEmpty(t, created[0].Req.GeneratedBy)
	assert.Contains(t, created[0].Req.GeneratedBy, "automation:auto1")
}

func TestAutomationMatcher_IgnoresNonMatchingEvent(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "auto1",
				Path:      "projects/test/automation/auto1.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:  "event",
					Event: "task.completed",
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Review task",
				},
				Status: "active",
			},
		},
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Publish a non-matching event
	bus.Publish(Event{
		Type:      EntryCreated,
		ProjectID: "test",
		Payload:   map[string]any{"id": "xyz"},
	})

	time.Sleep(200 * time.Millisecond)

	assert.Empty(t, creator.getCreated(), "should not create task for non-matching event")
}

func TestAutomationMatcher_FilterMatchesPayload(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "auto1",
				Path:      "projects/test/automation/auto1.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:  "event",
					Event: "task.completed",
					Filter: map[string]string{
						"project": "test",
					},
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Do something",
				},
				Status: "active",
			},
		},
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Publish event with non-matching project
	bus.Publish(Event{
		Type:      TaskCompleted,
		ProjectID: "other-project",
		Payload: map[string]any{
			"project": "other-project",
			"id":      "task1",
		},
	})

	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, creator.getCreated(), "filter should reject non-matching project")

	// Publish event with matching project
	bus.Publish(Event{
		Type:      TaskCompleted,
		ProjectID: "test",
		Payload: map[string]any{
			"project": "test",
			"id":      "task2",
		},
	})

	require.Eventually(t, func() bool {
		return len(creator.getCreated()) > 0
	}, 3*time.Second, 50*time.Millisecond, "expected task to be created for matching filter")
}

func TestAutomationMatcher_FilterWildcardMatchesAny(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "auto1",
				Path:      "projects/test/automation/auto1.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:  "event",
					Event: "feature.all_completed",
					Filter: map[string]string{
						"project": "*",
					},
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Merge feature",
				},
				Status: "active",
			},
		},
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	bus.Publish(Event{
		Type:      FeatureAllCompleted,
		ProjectID: "any-project",
		Payload: map[string]any{
			"project":    "any-project",
			"feature_id": "feat1",
		},
	})

	require.Eventually(t, func() bool {
		return len(creator.getCreated()) > 0
	}, 3*time.Second, 50*time.Millisecond, "wildcard filter should match any value")
}

func TestAutomationMatcher_OncePerDedup(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "auto1",
				Path:      "projects/test/automation/auto1.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:    "event",
					Event:   "feature.all_completed",
					OncePer: "feature_id",
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Merge feature",
				},
				Status: "active",
			},
		},
	}

	// Simulate that a task was already created for this automation+feature
	creator.existing["automation:auto1:feat1"] = true

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	bus.Publish(Event{
		Type:      FeatureAllCompleted,
		ProjectID: "test",
		Payload: map[string]any{
			"feature_id": "feat1",
		},
	})

	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, creator.getCreated(), "once_per should prevent duplicate task creation")

	// But a different feature_id should create a task
	bus.Publish(Event{
		Type:      FeatureAllCompleted,
		ProjectID: "test",
		Payload: map[string]any{
			"feature_id": "feat2",
		},
	})

	require.Eventually(t, func() bool {
		return len(creator.getCreated()) > 0
	}, 3*time.Second, 50*time.Millisecond, "different once_per value should allow task creation")
}

func TestAutomationMatcher_LoopPrevention(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "auto1",
				Path:      "projects/test/automation/auto1.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:  "event",
					Event: "entry.created",
					// IgnoreAutomationEvents defaults to nil (= true, ignore automation events)
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Process entry",
				},
				Status: "active",
			},
		},
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Publish an event with Source = "automation_matcher" (loop)
	bus.Publish(Event{
		Type:      EntryCreated,
		Source:    "automation_matcher",
		ProjectID: "test",
		Payload:   map[string]any{"id": "task1"},
	})

	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, creator.getCreated(), "should ignore events from automation_matcher (loop prevention)")

	// Also test Source = "automation" is blocked
	bus.Publish(Event{
		Type:      EntryCreated,
		Source:    "automation",
		ProjectID: "test",
		Payload:   map[string]any{"id": "task2"},
	})

	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, creator.getCreated(), "should ignore events from automation source (loop prevention)")
}

func TestAutomationMatcher_UpdateActionCallsUpdateDirectly(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "auto-update",
				Path:      "projects/test/automation/auto-update.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:  "event",
					Event: "task.completed",
				},
				Action: types.AutomationAction{
					Type: "update",
				},
				Status: "active",
			},
		},
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	bus.Publish(Event{
		Type:      TaskCompleted,
		ProjectID: "test",
		Payload: map[string]any{
			"id":   "task123",
			"path": "projects/test/task/task123.md",
		},
	})

	require.Eventually(t, func() bool {
		return len(creator.getUpdated()) > 0
	}, 3*time.Second, 50*time.Millisecond, "update action should call UpdateEntry")

	// Should NOT have created a task
	assert.Empty(t, creator.getCreated(), "update action should not create a task")
}

func TestAutomationMatcher_CacheRefreshesOnAutomationChange(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	source := &mockAutomationSource{
		entries: []AutomationEntry{}, // Start empty
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Publish task.completed — should be no match (no automations)
	bus.Publish(Event{
		Type:      TaskCompleted,
		ProjectID: "test",
		Payload:   map[string]any{"id": "task1"},
	})

	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, creator.getCreated(), "no automations = no match")

	// Now add an automation to the source
	source.setEntries([]AutomationEntry{
		{
			ID:        "auto1",
			Path:      "projects/test/automation/auto1.md",
			ProjectID: "test",
			Trigger: types.AutomationTrigger{
				Type:  "event",
				Event: "task.completed",
			},
			Action: types.AutomationAction{
				Type:         "prompt",
				DirectPrompt: "Review task",
			},
			Status: "active",
		},
	})

	// Trigger cache invalidation via an automation entry event
	bus.Publish(Event{
		Type:      EntryCreated,
		ProjectID: "test",
		Payload: map[string]any{
			"type": "automation",
			"id":   "auto1",
		},
	})

	// Give cache time to refresh
	time.Sleep(100 * time.Millisecond)

	// Now publish task.completed again — should match
	bus.Publish(Event{
		Type:      TaskCompleted,
		ProjectID: "test",
		Payload:   map[string]any{"id": "task2"},
	})

	require.Eventually(t, func() bool {
		return len(creator.getCreated()) > 0
	}, 3*time.Second, 50*time.Millisecond, "should match after cache refresh")
}

func TestAutomationMatcher_SkipsCronTriggerAutomations(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "cron-auto",
				Path:      "projects/test/automation/cron.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:     "cron",
					Schedule: "* * * * *",
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Run cron job",
				},
				Status: "active",
			},
		},
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Publish any event
	bus.Publish(Event{
		Type:      TaskCompleted,
		ProjectID: "test",
		Payload:   map[string]any{"id": "task1"},
	})

	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, creator.getCreated(), "cron trigger automations should be handled by CronEmitter, not matcher")
}

func TestAutomationMatcher_GracefulShutdown(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	source := &mockAutomationSource{}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		matcher.Start(ctx)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Clean shutdown
	case <-time.After(2 * time.Second):
		t.Fatal("matcher did not shut down within timeout")
	}
}

func TestAutomationMatcher_MultipleAutomationsMatchSameEvent(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "auto1",
				Path:      "projects/test/automation/auto1.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:  "event",
					Event: "task.completed",
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "First automation",
				},
				Status: "active",
			},
			{
				ID:        "auto2",
				Path:      "projects/test/automation/auto2.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:  "event",
					Event: "task.completed",
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Second automation",
				},
				Status: "active",
			},
		},
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	bus.Publish(Event{
		Type:      TaskCompleted,
		ProjectID: "test",
		Payload:   map[string]any{"id": "task1"},
	})

	require.Eventually(t, func() bool {
		return len(creator.getCreated()) >= 2
	}, 3*time.Second, 50*time.Millisecond, "both automations should fire")

	assert.Len(t, creator.getCreated(), 2)
	ids := []string{creator.getCreated()[0].AutomationID, creator.getCreated()[1].AutomationID}
	assert.Contains(t, ids, "auto1")
	assert.Contains(t, ids, "auto2")
}

func TestAutomationMatcher_SourceErrorDoesNotPanic(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	source := &mockAutomationSource{
		err: fmt.Errorf("database connection failed"),
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Should not panic even with source error
	bus.Publish(Event{
		Type:      TaskCompleted,
		ProjectID: "test",
		Payload:   map[string]any{"id": "task1"},
	})

	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, creator.getCreated(), "source error should not cause task creation")
}

func TestAutomationMatcher_ProjectScopeMatching(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "auto1",
				Path:      "projects/proj-a/automation/auto1.md",
				ProjectID: "proj-a",
				Trigger: types.AutomationTrigger{
					Type:  "event",
					Event: "task.completed",
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Project A only",
				},
				Status: "active",
			},
		},
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Event from different project should not match (automation has specific project)
	bus.Publish(Event{
		Type:      TaskCompleted,
		ProjectID: "proj-b",
		Payload:   map[string]any{"id": "task1"},
	})

	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, creator.getCreated(), "project-scoped automation should not match events from other projects")

	// Event from same project should match
	bus.Publish(Event{
		Type:      TaskCompleted,
		ProjectID: "proj-a",
		Payload:   map[string]any{"id": "task2"},
	})

	require.Eventually(t, func() bool {
		return len(creator.getCreated()) > 0
	}, 3*time.Second, 50*time.Millisecond, "should match event from same project")
}

func TestAutomationMatcher_GlobalAutomationMatchesAllProjects(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "global-auto",
				Path:      "global/automation/global-auto.md",
				ProjectID: "", // empty = global
				Trigger: types.AutomationTrigger{
					Type:  "event",
					Event: "task.completed",
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Global automation",
				},
				Status: "active",
			},
		},
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	bus.Publish(Event{
		Type:      TaskCompleted,
		ProjectID: "any-project",
		Payload:   map[string]any{"id": "task1"},
	})

	require.Eventually(t, func() bool {
		return len(creator.getCreated()) > 0
	}, 3*time.Second, 50*time.Millisecond, "global automation should match events from any project")
}

// =============================================================================
// Mock EventMarker
// =============================================================================

type mockEventMarker struct {
	mu        sync.Mutex
	processed []int64
}

func (m *mockEventMarker) MarkProcessed(ctx context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processed = append(m.processed, id)
	return nil
}

func (m *mockEventMarker) getProcessed() []int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]int64, len(m.processed))
	copy(result, m.processed)
	return result
}

// =============================================================================
// Tests: Per-Automation Loop Prevention Override
// =============================================================================

func TestAutomationMatcher_LoopPreventionOverrideAllowsAutomationEvents(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	falseVal := false
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "auto-chain",
				Path:      "projects/test/automation/auto-chain.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:                   "event",
					Event:                  "entry.created",
					IgnoreAutomationEvents: &falseVal, // Explicit opt-in
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Chain automation",
				},
				Status: "active",
			},
		},
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Publish an automation-generated event
	bus.Publish(Event{
		Type:      EntryCreated,
		Source:    "automation_matcher",
		ProjectID: "test",
		Payload:   map[string]any{"id": "task1"},
	})

	require.Eventually(t, func() bool {
		return len(creator.getCreated()) > 0
	}, 3*time.Second, 50*time.Millisecond,
		"automation with ignore_automation_events:false should process automation events")

	assert.Equal(t, "auto-chain", creator.getCreated()[0].AutomationID)
}

func TestAutomationMatcher_LoopPreventionExplicitTrueBlocksAutomationEvents(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	trueVal := true
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "auto1",
				Path:      "projects/test/automation/auto1.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:                   "event",
					Event:                  "entry.created",
					IgnoreAutomationEvents: &trueVal, // Explicit true
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Process entry",
				},
				Status: "active",
			},
		},
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	bus.Publish(Event{
		Type:      EntryCreated,
		Source:    "automation",
		ProjectID: "test",
		Payload:   map[string]any{"id": "task1"},
	})

	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, creator.getCreated(), "explicit ignore_automation_events:true should block automation events")
}

func TestAutomationMatcher_MixedOverrideAutomations(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	falseVal := false
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "auto-block",
				Path:      "projects/test/automation/auto-block.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:  "event",
					Event: "entry.created",
					// nil = default = ignore automation events
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Blocked",
				},
				Status: "active",
			},
			{
				ID:        "auto-allow",
				Path:      "projects/test/automation/auto-allow.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:                   "event",
					Event:                  "entry.created",
					IgnoreAutomationEvents: &falseVal,
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Allowed",
				},
				Status: "active",
			},
		},
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	bus.Publish(Event{
		Type:      EntryCreated,
		Source:    "automation_matcher",
		ProjectID: "test",
		Payload:   map[string]any{"id": "task1"},
	})

	require.Eventually(t, func() bool {
		return len(creator.getCreated()) > 0
	}, 3*time.Second, 50*time.Millisecond)

	// Only the opt-in automation should fire
	created := creator.getCreated()
	assert.Len(t, created, 1)
	assert.Equal(t, "auto-allow", created[0].AutomationID)
}

// =============================================================================
// Tests: Event Processed Marking
// =============================================================================

func TestAutomationMatcher_MarksEventAsProcessed(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	marker := &mockEventMarker{}
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "auto1",
				Path:      "projects/test/automation/auto1.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:  "event",
					Event: "task.completed",
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Review task",
				},
				Status: "active",
			},
		},
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
		Marker:  marker,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	bus.Publish(Event{
		Type:      TaskCompleted,
		ProjectID: "test",
		Payload: map[string]any{
			"id":            "task123",
			"_event_log_id": int64(42),
		},
	})

	require.Eventually(t, func() bool {
		return len(marker.getProcessed()) > 0
	}, 3*time.Second, 50*time.Millisecond, "event should be marked as processed")

	assert.Contains(t, marker.getProcessed(), int64(42))
}

func TestAutomationMatcher_NoMarkerDoesNotPanic(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "auto1",
				Path:      "projects/test/automation/auto1.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:  "event",
					Event: "task.completed",
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Review task",
				},
				Status: "active",
			},
		},
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
		// Marker not set — should not panic
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	bus.Publish(Event{
		Type:      TaskCompleted,
		ProjectID: "test",
		Payload: map[string]any{
			"id":            "task123",
			"_event_log_id": int64(42),
		},
	})

	require.Eventually(t, func() bool {
		return len(creator.getCreated()) > 0
	}, 3*time.Second, 50*time.Millisecond, "should still create tasks without marker")
}

func TestAutomationMatcher_MarksProcessedWithFloat64ID(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	marker := &mockEventMarker{}
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "auto1",
				Path:      "projects/test/automation/auto1.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:  "event",
					Event: "task.completed",
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Review task",
				},
				Status: "active",
			},
		},
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
		Marker:  marker,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// JSON unmarshaling often produces float64 for numbers
	bus.Publish(Event{
		Type:      TaskCompleted,
		ProjectID: "test",
		Payload: map[string]any{
			"id":            "task123",
			"_event_log_id": float64(99),
		},
	})

	require.Eventually(t, func() bool {
		return len(marker.getProcessed()) > 0
	}, 3*time.Second, 50*time.Millisecond)

	assert.Contains(t, marker.getProcessed(), int64(99))
}

// =============================================================================
// Tests: isAutomationSource helper
// =============================================================================

// =============================================================================
// Tests: Webhook Trigger Matching
// =============================================================================

func TestAutomationMatcher_WebhookTrigger(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "wh1",
				Path:      "projects/test/automation/wh1.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:    "webhook",
					Webhook: "/hooks/deploy",
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Deploy triggered by webhook",
				},
				Status: "active",
			},
		},
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Publish a webhook.received event matching the automation
	bus.Publish(Event{
		Type:   WebhookReceived,
		Source: "webhook",
		Payload: map[string]any{
			"webhook_path": "hooks/deploy",
			"repo":         "my-app",
		},
	})

	require.Eventually(t, func() bool {
		return len(creator.getCreated()) > 0
	}, 3*time.Second, 50*time.Millisecond, "expected task to be created for webhook trigger")

	created := creator.getCreated()
	assert.Len(t, created, 1)
	assert.Equal(t, "wh1", created[0].AutomationID)
	assert.Equal(t, "task", created[0].Req.Type)
	assert.Equal(t, "pending", created[0].Req.Status)
}

func TestAutomationMatcher_WebhookTrigger_NoMatch(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "wh1",
				Path:      "projects/test/automation/wh1.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:    "webhook",
					Webhook: "/hooks/deploy",
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Deploy triggered",
				},
				Status: "active",
			},
		},
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Publish a webhook.received event with non-matching path
	bus.Publish(Event{
		Type:   WebhookReceived,
		Source: "webhook",
		Payload: map[string]any{
			"webhook_path": "hooks/build",
		},
	})

	// Give it time to process — should not create a task
	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, creator.getCreated(), "no task should be created for non-matching webhook path")
}

func TestAutomationMatcher_WebhookTrigger_IgnoresNonWebhookEvents(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	creator := newMockTaskCreator()
	source := &mockAutomationSource{
		entries: []AutomationEntry{
			{
				ID:        "wh1",
				Path:      "projects/test/automation/wh1.md",
				ProjectID: "test",
				Trigger: types.AutomationTrigger{
					Type:    "webhook",
					Webhook: "/hooks/deploy",
				},
				Action: types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Deploy triggered",
				},
				Status: "active",
			},
		},
	}

	matcher := NewAutomationMatcher(AutomationMatcherConfig{
		Bus:     bus,
		Source:  source,
		Creator: creator,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go matcher.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Publish a non-webhook event — webhook automations should NOT match
	bus.Publish(Event{
		Type:      TaskCompleted,
		ProjectID: "test",
		Payload: map[string]any{
			"webhook_path": "hooks/deploy",
		},
	})

	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, creator.getCreated(), "webhook automation should not match non-webhook events")
}

func TestNormalizeWebhookPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/hooks/deploy", "hooks/deploy"},
		{"hooks/deploy", "hooks/deploy"},
		{"/hooks/deploy/", "hooks/deploy"},
		{"///hooks/deploy///", "hooks/deploy"},
		{"  /hooks/deploy  ", "hooks/deploy"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.input), func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeWebhookPath(tt.input))
		})
	}
}

func TestIsAutomationSource(t *testing.T) {
	tests := []struct {
		source   string
		expected bool
	}{
		{"automation_matcher", true},
		{"automation", true},
		{"service", false},
		{"external", false},
		{"", false},
		{"auto", false},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			assert.Equal(t, tt.expected, isAutomationSource(tt.source))
		})
	}
}

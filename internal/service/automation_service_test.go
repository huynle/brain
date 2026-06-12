package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

type fakeAutomationPauseChecker struct {
	paused bool
}

func (f fakeAutomationPauseChecker) IsAutomationsPaused() bool {
	return f.paused
}

func TestAutomationService_HandleEventCreatesTaskForMatchingEventAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	completeOnIdle := true

	automationResp, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Follow up when task completes",
		Content: "Creates a follow-up task when a matching task completes.",
		Status:  "active",
		Project: "automation-test",
		Trigger: &types.TriggerConfig{
			Type:    "event",
			Event:   types.EventTaskCompleted,
			Filter:  map[string]string{"project_id": "automation-test"},
			OncePer: "feature_id",
		},
		Action: &types.AutomationAction{
			Type:           "prompt",
			DirectPrompt:   "Create the follow-up summary.",
			Agent:          "general",
			ExecutionMode:  "current_branch",
			CompleteOnIdle: &completeOnIdle,
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	automation := NewAutomationService(brain)
	err = automation.HandleEvent(ctx, types.Event{
		ID:        "evt-test-1",
		Type:      types.EventTaskCompleted,
		Source:    types.EventSourceRunner,
		ProjectID: "automation-test",
		TaskID:    "source-task",
		FeatureID: "feature-a",
	})
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{
		Type:    "task",
		Project: "automation-test",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected one generated task, got %d", len(resp.Entries))
	}

	task := resp.Entries[0]
	if task.Status != "pending" {
		t.Errorf("generated task status = %q, want pending", task.Status)
	}
	if task.DirectPrompt != "Create the follow-up summary." {
		t.Errorf("generated task direct_prompt = %q", task.DirectPrompt)
	}
	if task.GeneratedBy == "" {
		t.Error("expected generated_by to identify the automation")
	}
	if task.GeneratedKey == "" {
		t.Error("expected generated_key for once_per dedup")
	}

	runResp, err := brain.List(ctx, types.ListEntriesRequest{
		Type:    "automation_run",
		Project: "automation-test",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("List automation runs failed: %v", err)
	}
	if len(runResp.Entries) != 1 {
		t.Fatalf("expected one automation run audit entry, got %d", len(runResp.Entries))
	}

	run := runResp.Entries[0]
	if run.Status != "queued" {
		t.Errorf("automation run status = %q, want queued", run.Status)
	}
	if !strings.Contains(run.Content, "automation_id: "+automationResp.ID) {
		t.Errorf("automation run content missing automation_id %q:\n%s", automationResp.ID, run.Content)
	}
	if !strings.Contains(run.Content, "source_event_id: evt-test-1") {
		t.Errorf("automation run content missing source event id:\n%s", run.Content)
	}
	if !strings.Contains(run.Content, "- "+task.ID) {
		t.Errorf("automation run content missing generated task id %q:\n%s", task.ID, run.Content)
	}
	if !strings.Contains(run.Content, "trigger_event: "+types.EventTaskCompleted) {
		t.Errorf("automation run content missing trigger event:\n%s", run.Content)
	}

	rawTask, err := brain.RecallFull(ctx, task.Path)
	if err != nil {
		t.Fatalf("RecallFull generated task failed: %v", err)
	}
	if !strings.Contains(rawTask, "automation_run_id: "+run.ID) {
		t.Errorf("generated task frontmatter missing automation_run_id %q:\n%s", run.ID, rawTask)
	}
}

func TestAutomationService_HandleEventInterpolatesProjectInPrompt(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Project-aware automation",
		Content: "Creates a task using project template variables.",
		Status:  "active",
		Project: "automation-template-test",
		Trigger: &types.TriggerConfig{
			Type:   "event",
			Event:  types.EventTaskCompleted,
			Filter: map[string]string{"project_id": "automation-template-test"},
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Consolidate {{.Project}} / {{.ProjectID}}",
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	automation := NewAutomationService(brain)
	err = automation.HandleEvent(ctx, types.Event{
		ID:        "evt-template-1",
		Type:      types.EventTaskCompleted,
		Source:    types.EventSourceRunner,
		ProjectID: "automation-template-test",
		TaskID:    "source-task",
	})
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{
		Type:    "task",
		Project: "automation-template-test",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected one generated task, got %d", len(resp.Entries))
	}
	if got := resp.Entries[0].DirectPrompt; got != "Consolidate automation-template-test / automation-template-test" {
		t.Fatalf("generated task direct_prompt = %q, want interpolated project", got)
	}
	if got := resp.Entries[0].Content; got != resp.Entries[0].DirectPrompt {
		t.Fatalf("generated task content = %q, direct_prompt = %q, want both interpolated", got, resp.Entries[0].DirectPrompt)
	}
}

func TestAutomationService_HandleEventDefaultsGeneratedTaskCompleteOnIdle(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Automation without explicit idle setting",
		Content: "Creates a task that should auto-complete when idle.",
		Status:  "active",
		Project: "automation-idle-test",
		Trigger: &types.TriggerConfig{
			Type:   "event",
			Event:  types.EventTaskCompleted,
			Filter: map[string]string{"project_id": "automation-idle-test"},
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Run automation work.",
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	automation := NewAutomationService(brain)
	err = automation.HandleEvent(ctx, types.Event{
		ID:        "evt-idle-1",
		Type:      types.EventTaskCompleted,
		Source:    types.EventSourceRunner,
		ProjectID: "automation-idle-test",
		TaskID:    "source-task",
	})
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "automation-idle-test", Limit: 10})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected one generated task, got %d", len(resp.Entries))
	}
	if resp.Entries[0].CompleteOnIdle == nil || !*resp.Entries[0].CompleteOnIdle {
		t.Fatalf("generated task complete_on_idle = %v, want true by default", resp.Entries[0].CompleteOnIdle)
	}
}

func TestAutomationService_HandleEventThreadsSessionModeOntoGeneratedTask(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Session mode automation",
		Content: "Creates a follow-up task with a session mode.",
		Status:  "active",
		Project: "automation-test",
		Trigger: &types.TriggerConfig{
			Type:    "event",
			Event:   types.EventTaskCompleted,
			Filter:  map[string]string{"project_id": "automation-test"},
			OncePer: "feature_id",
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Create the follow-up summary.",
			Agent:        "general",
			SessionMode:  "fresh",
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	automation := NewAutomationService(brain)
	err = automation.HandleEvent(ctx, types.Event{
		ID:        "evt-session-mode-1",
		Type:      types.EventTaskCompleted,
		Source:    types.EventSourceRunner,
		ProjectID: "automation-test",
		TaskID:    "source-task",
		FeatureID: "feature-a",
	})
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{
		Type:    "task",
		Project: "automation-test",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected one generated task, got %d", len(resp.Entries))
	}

	task := resp.Entries[0]
	if task.SessionMode != "fresh" {
		t.Errorf("generated task session_mode = %q, want %q", task.SessionMode, "fresh")
	}
}

func TestAutomationService_HandleEventMatchesAnyOfMultipleEvents(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
	}{
		{name: "first event in set", eventType: types.EventTaskCompleted},
		{name: "second event in set", eventType: types.EventFeatureCompleted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			brain, _, _ := newTestBrainService(t)
			ctx := context.Background()
			project := "automation-multi-event-test-" + tt.eventType

			_, err := brain.Save(ctx, types.CreateEntryRequest{
				Type:    "automation",
				Title:   "Multi-event follow-up",
				Content: "Triggers on task.completed OR feature.completed.",
				Status:  "active",
				Project: project,
				Trigger: &types.TriggerConfig{
					Type:   "event",
					Event:  types.EventTaskCompleted,
					Events: []string{types.EventFeatureCompleted},
				},
				Action: &types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Handle the multi-event trigger.",
				},
			})
			if err != nil {
				t.Fatalf("Save automation failed: %v", err)
			}

			automation := NewAutomationService(brain)
			err = automation.HandleEvent(ctx, types.Event{
				ID:        "evt-multi-" + tt.eventType,
				Type:      tt.eventType,
				Source:    types.EventSourceRunner,
				ProjectID: project,
				TaskID:    "source-task",
			})
			if err != nil {
				t.Fatalf("HandleEvent failed: %v", err)
			}

			resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: project, Limit: 10})
			if err != nil {
				t.Fatalf("List tasks failed: %v", err)
			}
			if len(resp.Entries) != 1 {
				t.Fatalf("expected one generated task for event %q, got %d", tt.eventType, len(resp.Entries))
			}
		})
	}
}

func TestAutomationService_HandleEventSkipsEventNotInMultiEventSet(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	project := "automation-multi-event-skip-test"

	_, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Multi-event follow-up",
		Content: "Triggers on task.completed OR feature.completed only.",
		Status:  "active",
		Project: project,
		Trigger: &types.TriggerConfig{
			Type:   "event",
			Event:  types.EventTaskCompleted,
			Events: []string{types.EventFeatureCompleted},
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Should not run for task.blocked.",
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	automation := NewAutomationService(brain)
	err = automation.HandleEvent(ctx, types.Event{
		ID:        "evt-multi-skip-1",
		Type:      types.EventTaskBlocked,
		Source:    types.EventSourceRunner,
		ProjectID: project,
		TaskID:    "source-task",
	})
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: project, Limit: 10})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 0 {
		t.Fatalf("expected no generated task for event outside multi-event set, got %d", len(resp.Entries))
	}
}

func TestAutomationService_HandleEventMatchesOrableStatusFilter(t *testing.T) {
	tests := []struct {
		name     string
		toStatus string
		want     int
	}{
		{name: "completed in set", toStatus: "completed", want: 1},
		{name: "blocked in set", toStatus: "blocked", want: 1},
		{name: "cancelled not in set", toStatus: "cancelled", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			brain, _, _ := newTestBrainService(t)
			ctx := context.Background()
			project := "automation-or-status-test-" + tt.toStatus

			_, err := brain.Save(ctx, types.CreateEntryRequest{
				Type:    "automation",
				Title:   "OR-able status follow-up",
				Content: "Triggers when to_status is completed OR blocked.",
				Status:  "active",
				Project: project,
				Trigger: &types.TriggerConfig{
					Type:  "event",
					Event: types.EventTaskStatusChanged,
					Filter: map[string]string{
						"to_status": "in:completed,blocked",
					},
				},
				Action: &types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Handle the OR-able status trigger.",
				},
			})
			if err != nil {
				t.Fatalf("Save automation failed: %v", err)
			}

			automation := NewAutomationService(brain)
			err = automation.HandleEvent(ctx, types.Event{
				ID:        "evt-or-status-" + tt.toStatus,
				Type:      types.EventTaskStatusChanged,
				Source:    types.EventSourceRunner,
				ProjectID: project,
				TaskID:    "source-task",
				ToStatus:  tt.toStatus,
			})
			if err != nil {
				t.Fatalf("HandleEvent failed: %v", err)
			}

			resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: project, Limit: 10})
			if err != nil {
				t.Fatalf("List tasks failed: %v", err)
			}
			if len(resp.Entries) != tt.want {
				t.Fatalf("to_status=%q: expected %d generated tasks, got %d", tt.toStatus, tt.want, len(resp.Entries))
			}
		})
	}
}

func TestAutomationService_HandleEventCombinesMultiEventAndOrableStatus(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		toStatus  string
		want      int
	}{
		{name: "task.status_changed + completed", eventType: types.EventTaskStatusChanged, toStatus: "completed", want: 1},
		{name: "feature.status_changed + blocked", eventType: "feature.status_changed", toStatus: "blocked", want: 1},
		{name: "matching event but status not in set", eventType: types.EventTaskStatusChanged, toStatus: "active", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			brain, _, _ := newTestBrainService(t)
			ctx := context.Background()
			project := "automation-combo-test-" + tt.name

			_, err := brain.Save(ctx, types.CreateEntryRequest{
				Type:    "automation",
				Title:   "Combined multi-event + OR-status follow-up",
				Content: "Triggers on multiple events with OR-able to_status filter.",
				Status:  "active",
				Project: project,
				Trigger: &types.TriggerConfig{
					Type:   "event",
					Event:  types.EventTaskStatusChanged,
					Events: []string{"feature.status_changed"},
					Filter: map[string]string{
						"to_status": "in:completed,blocked",
					},
				},
				Action: &types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Handle the combined trigger.",
				},
			})
			if err != nil {
				t.Fatalf("Save automation failed: %v", err)
			}

			automation := NewAutomationService(brain)
			err = automation.HandleEvent(ctx, types.Event{
				ID:        "evt-combo-" + tt.name,
				Type:      tt.eventType,
				Source:    types.EventSourceRunner,
				ProjectID: project,
				TaskID:    "source-task",
				ToStatus:  tt.toStatus,
			})
			if err != nil {
				t.Fatalf("HandleEvent failed: %v", err)
			}

			resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: project, Limit: 10})
			if err != nil {
				t.Fatalf("List tasks failed: %v", err)
			}
			if len(resp.Entries) != tt.want {
				t.Fatalf("%s: expected %d generated tasks, got %d", tt.name, tt.want, len(resp.Entries))
			}
		})
	}
}

func TestAutomationService_StartConsumesEventHubEvents(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	completeOnIdle := true

	_, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Follow up from hub event",
		Content: "Creates a follow-up task from a hub event.",
		Status:  "active",
		Project: "automation-hub-test",
		Trigger: &types.TriggerConfig{
			Type:  "event",
			Event: types.EventTaskCompleted,
		},
		Action: &types.AutomationAction{
			Type:           "prompt",
			DirectPrompt:   "Create the hub follow-up summary.",
			CompleteOnIdle: &completeOnIdle,
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	hub := realtime.NewEventHub()
	automation := NewAutomationService(brain)
	go automation.Start(ctx, hub)

	hub.Publish(types.Event{
		ID:        "evt-hub-1",
		Type:      types.EventTaskCompleted,
		Source:    types.EventSourceRunner,
		ProjectID: "automation-hub-test",
		TaskID:    "source-task",
	})

	task := waitForGeneratedTask(t, brain, "automation-hub-test")
	if task.DirectPrompt != "Create the hub follow-up summary." {
		t.Errorf("generated task direct_prompt = %q", task.DirectPrompt)
	}
}

func TestAutomationService_HandleEventSkipsWhenAutomationsPaused(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	automationResp, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Paused event automation",
		Content: "Should not run while the project is paused.",
		Status:  "active",
		Project: "automation-paused-event-test",
		Trigger: &types.TriggerConfig{
			Type:  "event",
			Event: types.EventTaskCompleted,
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "This should not be generated.",
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	automation := NewAutomationService(brain)
	automation.SetPauseChecker(fakeAutomationPauseChecker{paused: true})
	if err := automation.HandleEvent(ctx, types.Event{
		ID:        "evt-paused-1",
		Type:      types.EventTaskCompleted,
		Source:    types.EventSourceRunner,
		ProjectID: "automation-paused-event-test",
		TaskID:    "source-task",
	}); err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "automation-paused-event-test", Limit: 10})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 0 {
		t.Fatalf("expected no generated tasks while project paused, got %d", len(resp.Entries))
	}

	runs, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation_run", Project: "automation-paused-event-test", Limit: 10})
	if err != nil {
		t.Fatalf("List automation runs failed: %v", err)
	}
	if len(runs.Entries) != 1 {
		t.Fatalf("expected one skipped automation run audit entry, got %d", len(runs.Entries))
	}
	if runs.Entries[0].Status != "skipped" {
		t.Fatalf("run status = %q, want skipped", runs.Entries[0].Status)
	}
	if !strings.Contains(runs.Entries[0].Content, "automation_id: "+automationResp.ID) || !strings.Contains(runs.Entries[0].Content, "skip_reason: paused") {
		t.Fatalf("skipped run content missing automation_id/skip_reason:\n%s", runs.Entries[0].Content)
	}
}

func TestAutomationService_HandleEventDeduplicatesOncePerValue(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Once per feature follow-up",
		Content: "Creates one follow-up task per feature.",
		Status:  "active",
		Project: "automation-dedup-test",
		Trigger: &types.TriggerConfig{
			Type:    "event",
			Event:   types.EventTaskCompleted,
			OncePer: "feature_id",
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Create one feature summary.",
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	automation := NewAutomationService(brain)
	for _, eventID := range []string{"evt-dedup-1", "evt-dedup-2"} {
		err = automation.HandleEvent(ctx, types.Event{
			ID:        eventID,
			Type:      types.EventTaskCompleted,
			Source:    types.EventSourceRunner,
			ProjectID: "automation-dedup-test",
			TaskID:    eventID + "-task",
			FeatureID: "feature-a",
		})
		if err != nil {
			t.Fatalf("HandleEvent failed: %v", err)
		}
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{
		Type:    "task",
		Project: "automation-dedup-test",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected one generated task after duplicate events, got %d", len(resp.Entries))
	}
}

func TestAutomationService_HandleEventSkipsWhenMaxConcurrentGeneratedTasksReached(t *testing.T) {
	tests := []struct {
		name   string
		status string
	}{
		{name: "pending generated task", status: "pending"},
		{name: "active generated task", status: "active"},
		{name: "in_progress generated task", status: "in_progress"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			brain, _, _ := newTestBrainService(t)
			ctx := context.Background()

			automationResp, err := brain.Save(ctx, types.CreateEntryRequest{
				Type:    "automation",
				Title:   "Limited follow-up",
				Content: "Creates at most one runnable generated task.",
				Status:  "active",
				Project: "automation-max-concurrent-test-" + tt.status,
				Trigger: &types.TriggerConfig{
					Type:          "event",
					Event:         types.EventTaskCompleted,
					MaxConcurrent: 1,
				},
				Action: &types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Create max-concurrent summary.",
				},
			})
			if err != nil {
				t.Fatalf("Save automation failed: %v", err)
			}

			generated := true
			_, err = brain.Save(ctx, types.CreateEntryRequest{
				Type:        "task",
				Title:       "Existing generated task",
				Content:     "Already runnable.",
				Status:      tt.status,
				Project:     "automation-max-concurrent-test-" + tt.status,
				Generated:   &generated,
				GeneratedBy: "automation:" + automationResp.ID,
			})
			if err != nil {
				t.Fatalf("Save existing generated task failed: %v", err)
			}

			automation := NewAutomationService(brain)
			err = automation.HandleEvent(ctx, types.Event{
				ID:        "evt-max-concurrent-" + tt.status,
				Type:      types.EventTaskCompleted,
				Source:    types.EventSourceRunner,
				ProjectID: "automation-max-concurrent-test-" + tt.status,
				TaskID:    "source-task",
			})
			if err != nil {
				t.Fatalf("HandleEvent failed: %v", err)
			}

			resp, err := brain.List(ctx, types.ListEntriesRequest{
				Type:    "task",
				Project: "automation-max-concurrent-test-" + tt.status,
				Limit:   10,
			})
			if err != nil {
				t.Fatalf("List tasks failed: %v", err)
			}
			if len(resp.Entries) != 1 {
				t.Fatalf("expected max_concurrent guard to keep one generated task, got %d", len(resp.Entries))
			}
		})
	}
}

func TestAutomationService_HandleEventIgnoresNonRunnableGeneratedTasksForMaxConcurrent(t *testing.T) {
	tests := []struct {
		name   string
		status string
	}{
		{name: "completed generated task", status: "completed"},
		{name: "validated generated task", status: "validated"},
		{name: "blocked generated task", status: "blocked"},
		{name: "cancelled generated task", status: "cancelled"},
		{name: "superseded generated task", status: "superseded"},
		{name: "archived generated task", status: "archived"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			brain, _, _ := newTestBrainService(t)
			ctx := context.Background()
			project := "automation-max-concurrent-non-runnable-test-" + tt.status

			automationResp, err := brain.Save(ctx, types.CreateEntryRequest{
				Type:    "automation",
				Title:   "Limited follow-up after non-runnable task",
				Content: "Non-runnable generated tasks should not count against max_concurrent.",
				Status:  "active",
				Project: project,
				Trigger: &types.TriggerConfig{
					Type:          "event",
					Event:         types.EventTaskCompleted,
					MaxConcurrent: 1,
				},
				Action: &types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Create another summary.",
				},
			})
			if err != nil {
				t.Fatalf("Save automation failed: %v", err)
			}

			generated := true
			_, err = brain.Save(ctx, types.CreateEntryRequest{
				Type:        "task",
				Title:       "Non-runnable generated task",
				Content:     "Already not runnable.",
				Status:      tt.status,
				Project:     project,
				Generated:   &generated,
				GeneratedBy: "automation:" + automationResp.ID,
			})
			if err != nil {
				t.Fatalf("Save non-runnable generated task failed: %v", err)
			}

			automation := NewAutomationService(brain)
			err = automation.HandleEvent(ctx, types.Event{
				ID:        "evt-max-concurrent-non-runnable-" + tt.status,
				Type:      types.EventTaskCompleted,
				Source:    types.EventSourceRunner,
				ProjectID: project,
				TaskID:    "source-task",
			})
			if err != nil {
				t.Fatalf("HandleEvent failed: %v", err)
			}

			resp, err := brain.List(ctx, types.ListEntriesRequest{
				Type:    "task",
				Project: project,
				Limit:   10,
			})
			if err != nil {
				t.Fatalf("List tasks failed: %v", err)
			}
			if len(resp.Entries) != 2 {
				t.Fatalf("expected non-runnable generated task not to block creation, got %d tasks", len(resp.Entries))
			}
		})
	}
}

func TestAutomationService_HandleEventSkipsDuringCooldownUsingExistingGeneratedTasks(t *testing.T) {
	tests := []struct {
		name     string
		cooldown string
		created  time.Time
		now      time.Time
	}{
		{
			name:     "five minute cooldown",
			cooldown: "5m",
			created:  time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
			now:      time.Date(2026, 5, 15, 12, 4, 0, 0, time.UTC),
		},
		{
			name:     "one hour cooldown",
			cooldown: "1h",
			created:  time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
			now:      time.Date(2026, 5, 15, 12, 30, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			brain, _, _ := newTestBrainService(t)
			ctx := context.Background()
			project := "automation-cooldown-test-" + tt.name

			setTestNow(t, tt.created)
			automationResp, err := brain.Save(ctx, types.CreateEntryRequest{
				Type:    "automation",
				Title:   "Cooldown follow-up",
				Content: "Creates only after cooldown expires.",
				Status:  "active",
				Project: project,
				Trigger: &types.TriggerConfig{
					Type:     "event",
					Event:    types.EventTaskCompleted,
					Cooldown: tt.cooldown,
				},
				Action: &types.AutomationAction{
					Type:         "prompt",
					DirectPrompt: "Create cooldown summary.",
				},
			})
			if err != nil {
				t.Fatalf("Save automation failed: %v", err)
			}

			generated := true
			_, err = brain.Save(ctx, types.CreateEntryRequest{
				Type:        "task",
				Title:       "Recent generated task",
				Content:     "Recently created.",
				Status:      "completed",
				Project:     project,
				Generated:   &generated,
				GeneratedBy: "automation:" + automationResp.ID,
			})
			if err != nil {
				t.Fatalf("Save recent generated task failed: %v", err)
			}

			setTestNow(t, tt.now)
			automation := NewAutomationService(brain)
			err = automation.HandleEvent(ctx, types.Event{
				ID:        "evt-cooldown-" + tt.cooldown,
				Type:      types.EventTaskCompleted,
				Source:    types.EventSourceRunner,
				ProjectID: project,
				TaskID:    "source-task",
			})
			if err != nil {
				t.Fatalf("HandleEvent failed: %v", err)
			}

			resp, err := brain.List(ctx, types.ListEntriesRequest{
				Type:    "task",
				Project: project,
				Limit:   10,
			})
			if err != nil {
				t.Fatalf("List tasks failed: %v", err)
			}
			if len(resp.Entries) != 1 {
				t.Fatalf("expected cooldown guard to skip duplicate generation, got %d tasks", len(resp.Entries))
			}
		})
	}
}

func TestAutomationService_HandleEventAllowsGenerationAfterCooldownElapsed(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	project := "automation-cooldown-elapsed-test"
	created := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	setTestNow(t, created)

	automationResp, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Elapsed cooldown follow-up",
		Content: "Creates a task once cooldown has elapsed.",
		Status:  "active",
		Project: project,
		Trigger: &types.TriggerConfig{
			Type:     "event",
			Event:    types.EventTaskCompleted,
			Cooldown: "5m",
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Create elapsed-cooldown summary.",
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	generated := true
	_, err = brain.Save(ctx, types.CreateEntryRequest{
		Type:        "task",
		Title:       "Older generated task",
		Content:     "Created before cooldown window.",
		Status:      "completed",
		Project:     project,
		Generated:   &generated,
		GeneratedBy: "automation:" + automationResp.ID,
	})
	if err != nil {
		t.Fatalf("Save older generated task failed: %v", err)
	}

	setTestNow(t, created.Add(5*time.Minute))
	automation := NewAutomationService(brain)
	err = automation.HandleEvent(ctx, types.Event{
		ID:        "evt-cooldown-elapsed",
		Type:      types.EventTaskCompleted,
		Source:    types.EventSourceRunner,
		ProjectID: project,
		TaskID:    "source-task",
	})
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{
		Type:    "task",
		Project: project,
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("expected cooldown elapsed to allow generation, got %d tasks", len(resp.Entries))
	}
}

func TestAutomationService_HandleEventTreatsInvalidCooldownAsNoCooldown(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	created := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	setTestNow(t, created)

	automationResp, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Invalid cooldown follow-up",
		Content: "Invalid cooldown should not block generation.",
		Status:  "active",
		Project: "automation-invalid-cooldown-test",
		Trigger: &types.TriggerConfig{
			Type:     "event",
			Event:    types.EventTaskCompleted,
			Cooldown: "not-a-duration",
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Create invalid-cooldown summary.",
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	generated := true
	_, err = brain.Save(ctx, types.CreateEntryRequest{
		Type:        "task",
		Title:       "Previous generated task",
		Content:     "Already generated.",
		Status:      "completed",
		Project:     "automation-invalid-cooldown-test",
		Generated:   &generated,
		GeneratedBy: "automation:" + automationResp.ID,
	})
	if err != nil {
		t.Fatalf("Save previous generated task failed: %v", err)
	}

	setTestNow(t, created.Add(time.Minute))
	automation := NewAutomationService(brain)
	err = automation.HandleEvent(ctx, types.Event{
		ID:        "evt-invalid-cooldown",
		Type:      types.EventTaskCompleted,
		Source:    types.EventSourceRunner,
		ProjectID: "automation-invalid-cooldown-test",
		TaskID:    "source-task",
	})
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{
		Type:    "task",
		Project: "automation-invalid-cooldown-test",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("expected invalid cooldown to allow generation, got %d tasks", len(resp.Entries))
	}
}

func TestAutomationService_HandleEventSupportsProjectFilterAlias(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Project alias filter follow-up",
		Content: "Uses trigger.filter.project for compatibility with automation matcher docs.",
		Status:  "active",
		Project: "automation-project-filter-test",
		Trigger: &types.TriggerConfig{
			Type:   "event",
			Event:  types.EventTaskCompleted,
			Filter: map[string]string{"project": "automation-project-filter-test"},
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Create the project-filter summary.",
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	automation := NewAutomationService(brain)
	err = automation.HandleEvent(ctx, types.Event{
		ID:        "evt-project-filter-1",
		Type:      types.EventTaskCompleted,
		Source:    types.EventSourceRunner,
		ProjectID: "automation-project-filter-test",
		TaskID:    "source-task",
	})
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{
		Type:    "task",
		Project: "automation-project-filter-test",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected one generated task, got %d", len(resp.Entries))
	}
}

func TestAutomationService_HandleEventCreatesTaskForSessionAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Summarize discovered session",
		Content: "Creates a task when the runner discovers an agent session.",
		Status:  "active",
		Project: "automation-session-test",
		Trigger: &types.TriggerConfig{
			Type: "session",
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Summarize the discovered session.",
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	automation := NewAutomationService(brain)
	err = automation.HandleEvent(ctx, types.Event{
		ID:        "evt-session-1",
		Type:      types.EventRunnerSessionDiscovered,
		Source:    types.EventSourceRunner,
		ProjectID: "automation-session-test",
		Metadata: map[string]string{
			"session_id": "ses_abc123",
		},
	})
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{
		Type:    "task",
		Project: "automation-session-test",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected one generated session task, got %d", len(resp.Entries))
	}
	if resp.Entries[0].DirectPrompt != "Summarize the discovered session." {
		t.Errorf("generated task direct_prompt = %q", resp.Entries[0].DirectPrompt)
	}
	if resp.Entries[0].GeneratedBy == "" {
		t.Error("expected generated_by to identify the automation")
	}
}

func TestAutomationService_HandleEventIgnoresNonSessionEventForSessionAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Session-only automation",
		Content: "Only runner session discovery should trigger this automation.",
		Status:  "active",
		Project: "automation-session-ignore-test",
		Trigger: &types.TriggerConfig{
			Type: "session",
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "This should only run for sessions.",
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	automation := NewAutomationService(brain)
	err = automation.HandleEvent(ctx, types.Event{
		ID:        "evt-session-ignore-1",
		Type:      types.EventTaskCompleted,
		Source:    types.EventSourceRunner,
		ProjectID: "automation-session-ignore-test",
		TaskID:    "source-task",
	})
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{
		Type:    "task",
		Project: "automation-session-ignore-test",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 0 {
		t.Fatalf("expected no generated task for non-session event, got %d", len(resp.Entries))
	}
}

func TestAutomationService_HandleEventAppliesFiltersForSessionAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Filtered session automation",
		Content: "Only matching session metadata should trigger this automation.",
		Status:  "active",
		Project: "automation-session-filter-test",
		Trigger: &types.TriggerConfig{
			Type: "session",
			Filter: map[string]string{
				"session_id": "ses_expected",
			},
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Handle the filtered session.",
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	automation := NewAutomationService(brain)
	for _, sessionID := range []string{"ses_other", "ses_expected"} {
		err = automation.HandleEvent(ctx, types.Event{
			ID:        "evt-session-filter-" + sessionID,
			Type:      types.EventRunnerSessionDiscovered,
			Source:    types.EventSourceRunner,
			ProjectID: "automation-session-filter-test",
			Metadata: map[string]string{
				"session_id": sessionID,
			},
		})
		if err != nil {
			t.Fatalf("HandleEvent failed: %v", err)
		}
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{
		Type:    "task",
		Project: "automation-session-filter-test",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected one generated task for matching session filter, got %d", len(resp.Entries))
	}
}

func TestAutomationService_HandleEventDeduplicatesSessionAutomationOncePerSession(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Once per session automation",
		Content: "Creates one generated task per discovered session.",
		Status:  "active",
		Project: "automation-session-dedup-test",
		Trigger: &types.TriggerConfig{
			Type:    "session",
			OncePer: "session",
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Handle one session once.",
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	automation := NewAutomationService(brain)
	for _, eventID := range []string{"evt-session-dedup-1", "evt-session-dedup-2"} {
		err = automation.HandleEvent(ctx, types.Event{
			ID:        eventID,
			Type:      types.EventRunnerSessionDiscovered,
			Source:    types.EventSourceRunner,
			ProjectID: "automation-session-dedup-test",
			Metadata: map[string]string{
				"session_id": "ses_dedup",
			},
		})
		if err != nil {
			t.Fatalf("HandleEvent failed: %v", err)
		}
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{
		Type:    "task",
		Project: "automation-session-dedup-test",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected one generated task after duplicate session events, got %d", len(resp.Entries))
	}
	if resp.Entries[0].GeneratedKey == "" {
		t.Fatal("expected generated_key for once_per session dedup")
	}
}

func TestAutomationService_HandleEventCreatesTaskForMatchingWebhookAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Deploy webhook",
		Content: "Creates a task when a matching inbound webhook is received.",
		Status:  "active",
		Project: "automation-webhook-test",
		Trigger: &types.TriggerConfig{
			Type:    "webhook",
			Webhook: "hooks/deploy",
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Deploy from webhook.",
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	automation := NewAutomationService(brain)
	err = automation.HandleEvent(ctx, types.Event{
		ID:        "evt-webhook-1",
		Type:      "webhook.received",
		Source:    "webhook",
		ProjectID: "automation-webhook-test",
		Metadata: map[string]string{
			"webhook_path": "/hooks/deploy/",
		},
	})
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{
		Type:    "task",
		Project: "automation-webhook-test",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected one generated webhook task, got %d", len(resp.Entries))
	}
	if resp.Entries[0].DirectPrompt != "Deploy from webhook." {
		t.Errorf("generated task direct_prompt = %q", resp.Entries[0].DirectPrompt)
	}
}

func TestAutomationService_HandleEventAppliesGuardsForWebhookAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	project := "automation-webhook-guard-test"
	created := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	setTestNow(t, created)

	automationResp, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Guarded webhook",
		Content: "Webhook automation guarded by cooldown and max_concurrent.",
		Status:  "active",
		Project: project,
		Trigger: &types.TriggerConfig{
			Type:          "webhook",
			Webhook:       "hooks/deploy",
			Cooldown:      "10m",
			MaxConcurrent: 1,
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Deploy from guarded webhook.",
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	generated := true
	_, err = brain.Save(ctx, types.CreateEntryRequest{
		Type:        "task",
		Title:       "Existing webhook generated task",
		Content:     "Already runnable.",
		Status:      "pending",
		Project:     project,
		Generated:   &generated,
		GeneratedBy: "automation:" + automationResp.ID,
	})
	if err != nil {
		t.Fatalf("Save existing generated task failed: %v", err)
	}

	setTestNow(t, created.Add(5*time.Minute))
	automation := NewAutomationService(brain)
	err = automation.HandleEvent(ctx, types.Event{
		ID:        "evt-webhook-guard-1",
		Type:      "webhook.received",
		Source:    "webhook",
		ProjectID: project,
		Metadata:  map[string]string{"webhook_path": "hooks/deploy"},
	})
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: project, Limit: 10})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected webhook guards to keep one generated task, got %d", len(resp.Entries))
	}
}

func TestAutomationService_HandleEventAppliesGuardsForSessionAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	project := "automation-session-guard-test"
	created := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	setTestNow(t, created)

	automationResp, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Guarded session automation",
		Content: "Session automation guarded by cooldown and max_concurrent.",
		Status:  "active",
		Project: project,
		Trigger: &types.TriggerConfig{
			Type:          "session",
			Cooldown:      "10m",
			MaxConcurrent: 1,
		},
		Action: &types.AutomationAction{Type: "prompt", DirectPrompt: "Handle guarded session."},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	generated := true
	_, err = brain.Save(ctx, types.CreateEntryRequest{
		Type:        "task",
		Title:       "Existing session generated task",
		Content:     "Already runnable.",
		Status:      "pending",
		Project:     project,
		Generated:   &generated,
		GeneratedBy: "automation:" + automationResp.ID,
	})
	if err != nil {
		t.Fatalf("Save existing generated task failed: %v", err)
	}

	setTestNow(t, created.Add(5*time.Minute))
	automation := NewAutomationService(brain)
	err = automation.HandleEvent(ctx, types.Event{
		ID:        "evt-session-guard-1",
		Type:      types.EventRunnerSessionDiscovered,
		Source:    types.EventSourceRunner,
		ProjectID: project,
		Metadata:  map[string]string{"session_id": "ses_guarded"},
	})
	if err != nil {
		t.Fatalf("HandleEvent failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: project, Limit: 10})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected session guards to keep one generated task, got %d", len(resp.Entries))
	}
}

func TestAutomationService_CheckScheduledAppliesGuardsForCronAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	project := "automation-cron-guard-test"
	created := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	setTestNow(t, created)

	automationResp, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Guarded cron automation",
		Content: "Cron automation guarded by cooldown and max_concurrent.",
		Status:  "active",
		Project: project,
		Trigger: &types.TriggerConfig{
			Type:          "cron",
			Schedule:      "* * * * *",
			Cooldown:      "10m",
			MaxConcurrent: 1,
		},
		Action: &types.AutomationAction{Type: "prompt", DirectPrompt: "Handle guarded cron."},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	generated := true
	_, err = brain.Save(ctx, types.CreateEntryRequest{
		Type:        "task",
		Title:       "Existing cron generated task",
		Content:     "Already runnable.",
		Status:      "pending",
		Project:     project,
		Generated:   &generated,
		GeneratedBy: "automation:" + automationResp.ID,
	})
	if err != nil {
		t.Fatalf("Save existing generated task failed: %v", err)
	}

	now := created.Add(5 * time.Minute)
	setTestNow(t, now)
	automation := NewAutomationService(brain)
	if err := automation.CheckScheduled(ctx, now); err != nil {
		t.Fatalf("CheckScheduled failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: project, Limit: 10})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected cron guards to keep one generated task, got %d", len(resp.Entries))
	}
}

func TestAutomationService_CheckScheduledCreatesTaskForDueCronAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	now := time.Date(2026, 4, 29, 13, 5, 0, 0, time.UTC)

	_, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Cron follow-up",
		Content: "Creates a generated task on a cron schedule.",
		Status:  "active",
		Project: "automation-cron-entry-test",
		Agent:   "build",
		Model:   "test-model",
		Executor: "pi",
		Trigger: &types.TriggerConfig{
			Type:     "cron",
			Schedule: "* * * * *",
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Create the cron automation summary.",
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	automation := NewAutomationService(brain)
	if err := automation.CheckScheduled(ctx, now); err != nil {
		t.Fatalf("CheckScheduled failed: %v", err)
	}
	if err := automation.CheckScheduled(ctx, now); err != nil {
		t.Fatalf("CheckScheduled duplicate failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{
		Type:    "task",
		Project: "automation-cron-entry-test",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected one generated cron task, got %d", len(resp.Entries))
	}
	expectedKey := "automation:cron:" + resp.Entries[0].GeneratedBy[len("automation:"):] + ":202604291305"
	if resp.Entries[0].GeneratedKey != expectedKey {
		t.Fatalf("generated key = %q, want %q", resp.Entries[0].GeneratedKey, expectedKey)
	}
	if resp.Entries[0].CompleteOnIdle == nil || !*resp.Entries[0].CompleteOnIdle {
		t.Fatalf("generated cron task complete_on_idle = %v, want true by default", resp.Entries[0].CompleteOnIdle)
	}
	if resp.Entries[0].Agent != "build" || resp.Entries[0].Model != "test-model" || resp.Entries[0].Executor != "pi" {
		t.Fatalf("generated cron task execution metadata agent/model/executor = %q/%q/%q, want build/test-model/pi", resp.Entries[0].Agent, resp.Entries[0].Model, resp.Entries[0].Executor)
	}
}

func TestAutomationService_CheckScheduledSkipsWhenAutomationsPaused(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	now := time.Date(2026, 4, 29, 13, 5, 0, 0, time.UTC)

	_, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Paused cron automation",
		Content: "Should not create tasks while paused.",
		Status:  "active",
		Project: "automation-paused-cron-test",
		Trigger: &types.TriggerConfig{
			Type:     "cron",
			Schedule: "* * * * *",
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "This should not be generated.",
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	automation := NewAutomationService(brain)
	automation.SetPauseChecker(fakeAutomationPauseChecker{paused: true})
	if err := automation.CheckScheduled(ctx, now); err != nil {
		t.Fatalf("CheckScheduled failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "automation-paused-cron-test", Limit: 10})
	if err != nil {
		t.Fatalf("List tasks failed: %v", err)
	}
	if len(resp.Entries) != 0 {
		t.Fatalf("expected no generated cron tasks while project paused, got %d", len(resp.Entries))
	}
}

func TestAutomationService_StartChecksScheduledAutomationsOnStartup(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	_, err := brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Startup cron follow-up",
		Content: "Creates a generated task when automation service starts.",
		Status:  "active",
		Project: "automation-start-cron-test",
		Trigger: &types.TriggerConfig{
			Type:     "cron",
			Schedule: "* * * * *",
		},
		Action: &types.AutomationAction{
			Type:         "prompt",
			DirectPrompt: "Create the startup cron summary.",
		},
	})
	if err != nil {
		t.Fatalf("Save automation failed: %v", err)
	}

	hub := realtime.NewEventHub()
	automation := NewAutomationService(brain)
	go automation.Start(ctx, hub)

	task := waitForGeneratedTask(t, brain, "automation-start-cron-test")
	if task.DirectPrompt != "Create the startup cron summary." {
		t.Errorf("generated task direct_prompt = %q", task.DirectPrompt)
	}
}

func waitForGeneratedTask(t *testing.T, brain *BrainServiceImpl, project string) types.BrainEntry {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	var lastCount int
	for time.Now().Before(deadline) {
		resp, err := brain.List(context.Background(), types.ListEntriesRequest{
			Type:    "task",
			Project: project,
			Limit:   10,
		})
		if err != nil {
			t.Fatalf("List tasks failed: %v", err)
		}
		lastCount = len(resp.Entries)
		if len(resp.Entries) > 0 {
			return resp.Entries[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for generated task; last count=%d", lastCount)
	return types.BrainEntry{}
}

func setTestNow(t *testing.T, now time.Time) {
	t.Helper()
	original := types.TimeNowUTC
	types.TimeNowUTC = func() time.Time { return now.UTC() }
	t.Cleanup(func() { types.TimeNowUTC = original })
}

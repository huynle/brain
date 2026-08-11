package service

import (
	"context"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// -----------------------------------------------------------------------------
// Phase 4 compositional integration test.
//
// Closes the loop from CheckFeatureCompletion → EventHub → AutomationService.
// The existing dispatch tests in builtin_feature_checkout_simple_test.go
// verify AutomationService.HandleEvent routing given a hand-crafted event.
// This test verifies the *upstream half* — that the event assembled by
// CheckFeatureCompletion (with checkout_mode folded from ResolvedTask.CheckoutMode)
// is exactly the event that reaches HandleEvent, and that routing is correct
// end-to-end.
//
// Design choice: instead of relying on the hub's async subscriber goroutine
// (which would make the test flaky under -race and timing-sensitive), we:
//  1. Wire an EventService with the hub and a mockFeatureTaskLister.
//  2. Subscribe to the hub with a matching EventFilter.
//  3. Call CheckFeatureCompletion, which publishes.
//  4. Receive the event synchronously from the subscription channel.
//  5. Feed that exact event into AutomationService.HandleEvent.
//  6. Assert exactly one task-type entry created with the right shape.
//
// This is deterministic (no timeouts, no sleeps) and still exercises the
// real assembly logic in CheckFeatureCompletion — including the checkout_mode
// fold.
// -----------------------------------------------------------------------------

// registerBothBuiltInCheckoutAutomations installs the AI and simple built-in
// feature-checkout automations. Kept as a helper so both sub-tests share
// exactly the same setup (any drift would be a bug in the test, not the code).
func registerBothBuiltInCheckoutAutomations(t *testing.T, brain *BrainServiceImpl) {
	t.Helper()
	ctx := context.Background()

	if err := EnsureBuiltInFeatureCheckoutAutomation(ctx, brain, BuiltInFeatureCheckoutConfig{
		Enabled:           true,
		MergeTargetBranch: "main",
		TargetWorkdir:     "/repo/brain",
		ExecutionMode:     "worktree",
	}); err != nil {
		t.Fatalf("ensure AI automation: %v", err)
	}
	if err := EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brain, BuiltInFeatureCheckoutSimpleConfig{
		Enabled:           true,
		MergeTargetBranch: "main",
		TargetWorkdir:     "/repo/brain",
	}); err != nil {
		t.Fatalf("ensure simple automation: %v", err)
	}
}

// TestCheckoutMode_EndToEndFromFeatureCompletionToDispatch is the compositional
// integration test required by Phase 4. It exercises the full pipeline:
//
//	CheckFeatureCompletion(tasks with checkout_mode)
//	  → foldCheckoutMode → evt.Metadata["checkout_mode"]
//	  → hub.Publish(EventFeatureCompleted)
//	  → subscriber receives event
//	  → AutomationService.HandleEvent(event)
//	  → matchAutomationFilters routes by checkout_mode
//	  → exactly one task entry created with correct executor/action
//
// Two sub-tests — "simple" and "ai" — cover the two routes. Same setup, same
// mock task list *except* for the CheckoutMode field.
func TestCheckoutMode_EndToEndFromFeatureCompletionToDispatch(t *testing.T) {
	tests := []struct {
		name             string
		taskCheckoutMode string // set on ResolvedTask.CheckoutMode
		wantExecutor     string // "script" for simple, "" (or non-script) for AI
		wantContentPart  string // substring that MUST appear in the generated task content
		wantNotContent   string // substring that MUST NOT appear (route-crossing guard)
	}{
		{
			name:             "simple mode routes to script automation",
			taskCheckoutMode: "simple",
			wantExecutor:     "script",
			wantContentPart:  "git -c merge.ff=true merge --squash",
			wantNotContent:   "feature-checkout skill",
		},
		{
			name:             "ai mode routes to AI (prompt) automation",
			taskCheckoutMode: "ai",
			wantExecutor:     "", // AI template does not set Executor="script"
			wantContentPart:  "feature-checkout skill",
			wantNotContent:   "git -c merge.ff=true merge --squash",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			brain, _, _ := newTestBrainService(t)
			ctx := context.Background()

			// 1. Register both built-in automations — globally, exactly as the
			// server does. No per-project copies exist in production, and
			// making them here would hide a global entry that matches nothing.
			registerBothBuiltInCheckoutAutomations(t, brain)

			// 2. Wire EventService + hub with a FeatureTaskLister returning
			//    two tasks in the target feature, both carrying the checkout_mode
			//    under test. This exercises foldCheckoutMode over multiple tasks.
			eventSvc, hub := newTestEventService()
			lister := &mockFeatureTaskLister{
				tasks: []types.ResolvedTask{
					{ID: "t1", FeatureID: "feat-e2e", Status: "completed", CheckoutMode: tc.taskCheckoutMode},
					{ID: "t2", FeatureID: "feat-e2e", Status: "validated", CheckoutMode: tc.taskCheckoutMode},
				},
			}
			eventSvc.SetFeatureTaskLister(lister)

			// 3. Subscribe to the hub BEFORE publishing so we can receive
			//    the event synchronously. Filter on the exact project/feature
			//    to avoid interference from any incidental publishes.
			sub, unsub := hub.Subscribe(realtime.EventFilter{
				TypePatterns: []string{types.EventFeatureCompleted},
				ProjectID:    "brain",
				FeatureID:    "feat-e2e",
			})
			defer unsub()

			// 4. Trigger the upstream half: CheckFeatureCompletion assembles
			//    an EventFeatureCompleted event with folded checkout_mode
			//    metadata and publishes to the hub.
			eventSvc.CheckFeatureCompletion(ctx, "brain", "feat-e2e", "t2")

			// 5. Receive the event synchronously. If nothing arrives we
			//    have a real bug — no async retry needed, Publish is
			//    synchronous to subscribers per EventHub contract.
			var evt types.Event
			select {
			case evt = <-sub:
			default:
				t.Fatalf("expected an event on subscription channel after CheckFeatureCompletion, got none")
			}

			// Sanity: metadata carries the folded checkout_mode.
			if got := evt.Metadata["checkout_mode"]; got != tc.taskCheckoutMode {
				t.Fatalf("event metadata checkout_mode = %q, want %q", got, tc.taskCheckoutMode)
			}
			if evt.Type != types.EventFeatureCompleted {
				t.Fatalf("event type = %q, want %q", evt.Type, types.EventFeatureCompleted)
			}

			// 6. Feed the exact event into AutomationService.HandleEvent —
			//    this is the downstream half. Deterministic, no timing.
			automationSvc := NewAutomationService(brain)
			if err := automationSvc.HandleEvent(ctx, evt); err != nil {
				t.Fatalf("HandleEvent: %v", err)
			}

			// 7. Exactly one task-type entry should exist in the project,
			//    matching the expected route.
			tasks, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "brain", Limit: 100})
			if err != nil {
				t.Fatalf("list generated tasks: %v", err)
			}
			if len(tasks.Entries) != 1 {
				t.Fatalf("expected exactly 1 generated task, got %d", len(tasks.Entries))
			}
			task := tasks.Entries[0]

			if task.Executor != tc.wantExecutor {
				t.Errorf("generated task executor = %q, want %q", task.Executor, tc.wantExecutor)
			}
			if !strings.Contains(task.Content, tc.wantContentPart) {
				t.Errorf("generated task content missing required substring %q\ncontent:\n%s", tc.wantContentPart, task.Content)
			}
			if tc.wantNotContent != "" && strings.Contains(task.Content, tc.wantNotContent) {
				t.Errorf("generated task content unexpectedly contained %q (route-crossing bug)\ncontent:\n%s", tc.wantNotContent, task.Content)
			}
		})
	}
}

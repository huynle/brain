package service

import (
	"context"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// -----------------------------------------------------------------------------
// EnsureBuiltInFeatureCheckoutSimpleAutomation (Phase 3.3)
// -----------------------------------------------------------------------------

func TestEnsureBuiltInFeatureCheckoutSimpleAutomation_CreatesEntry(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	err := EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brain, BuiltInFeatureCheckoutSimpleConfig{
		Enabled:            true,
		MergeTargetBranch:  "main",
		RemoteBranchPolicy: "delete",
		TargetWorkdir:      "/repo/brain",
	})
	if err != nil {
		t.Fatalf("EnsureBuiltInFeatureCheckoutSimpleAutomation failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Status: "active", Limit: 100})
	if err != nil {
		t.Fatalf("List automations failed: %v", err)
	}

	// Find our entry
	var simple *types.BrainEntry
	for i := range resp.Entries {
		if resp.Entries[i].GeneratedBy == BuiltInFeatureCheckoutSimpleGeneratedBy {
			simple = &resp.Entries[i]
			break
		}
	}
	if simple == nil {
		t.Fatalf("expected simple built-in automation, not found; entries=%d", len(resp.Entries))
	}

	// Trigger shape
	if simple.Trigger == nil {
		t.Fatalf("simple automation missing trigger")
	}
	if simple.Trigger.Type != "event" {
		t.Errorf("trigger type = %q, want %q", simple.Trigger.Type, "event")
	}
	if simple.Trigger.Event != types.EventFeatureCompleted {
		t.Errorf("trigger event = %q, want %q", simple.Trigger.Event, types.EventFeatureCompleted)
	}
	if simple.Trigger.OncePer != "feature_id" {
		t.Errorf("trigger once_per = %q, want %q", simple.Trigger.OncePer, "feature_id")
	}
	if simple.Trigger.Filter["checkout_mode"] != "simple" {
		t.Errorf("trigger filter checkout_mode = %q, want %q", simple.Trigger.Filter["checkout_mode"], "simple")
	}

	// Action shape
	if simple.Action == nil {
		t.Fatalf("simple automation missing action")
	}
	if simple.Action.Type != types.AutomationActionScript {
		t.Errorf("action type = %q, want %q", simple.Action.Type, types.AutomationActionScript)
	}
	if simple.Action.Command == "" {
		t.Fatalf("action command empty")
	}

	// Required script substrings (invariants)
	required := []string{
		"git -c merge.ff=true merge --squash",
		"git worktree remove",
		"git branch -D",
		"{{.FeatureID}}",
	}
	for _, needle := range required {
		if !strings.Contains(simple.Action.Command, needle) {
			t.Errorf("script command missing invariant substring %q", needle)
		}
	}
}

func TestEnsureBuiltInFeatureCheckoutSimpleAutomation_Idempotent(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	cfg := BuiltInFeatureCheckoutSimpleConfig{
		Enabled:           true,
		MergeTargetBranch: "main",
	}
	for i := 0; i < 3; i++ {
		if err := EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brain, cfg); err != nil {
			t.Fatalf("call %d failed: %v", i+1, err)
		}
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 100})
	if err != nil {
		t.Fatalf("List automations failed: %v", err)
	}
	count := 0
	for _, e := range resp.Entries {
		if e.GeneratedBy == BuiltInFeatureCheckoutSimpleGeneratedBy {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 simple built-in automation, got %d", count)
	}
}

func TestEnsureBuiltInFeatureCheckoutSimpleAutomation_DisabledDoesNothing(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if err := EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brain, BuiltInFeatureCheckoutSimpleConfig{Enabled: false}); err != nil {
		t.Fatalf("EnsureBuiltInFeatureCheckoutSimpleAutomation (disabled) failed: %v", err)
	}
	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 10})
	if err != nil {
		t.Fatalf("List automations failed: %v", err)
	}
	if len(resp.Entries) != 0 {
		t.Fatalf("expected no automations when disabled, got %d", len(resp.Entries))
	}
}

func TestEnsureBuiltInFeatureCheckoutSimpleAutomation_UsesConfiguredMergeTarget(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	if err := EnsureBuiltInFeatureCheckoutSimpleAutomation(ctx, brain, BuiltInFeatureCheckoutSimpleConfig{
		Enabled:           true,
		MergeTargetBranch: "develop",
	}); err != nil {
		t.Fatalf("EnsureBuiltInFeatureCheckoutSimpleAutomation failed: %v", err)
	}

	resp, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Limit: 10})
	if err != nil {
		t.Fatalf("List automations failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(resp.Entries))
	}
	entry := resp.Entries[0]
	if !strings.Contains(entry.Action.Command, "develop") {
		t.Errorf("expected script to reference configured merge target 'develop', got: %s", entry.Action.Command)
	}
}

// -----------------------------------------------------------------------------
// End-to-end dispatch routing (Phase 3.5 integration test)
// -----------------------------------------------------------------------------

// TestBuiltinFeatureCheckoutDispatch_SimpleEventOnlyMatchesSimpleAutomation
// verifies that with both automations registered, a feature.completed event
// carrying checkout_mode=simple in metadata generates exactly one script
// task, coming from the simple automation.
func TestBuiltinFeatureCheckoutDispatch_SimpleEventOnlyMatchesSimpleAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Register both built-ins.
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

	// Project-scope both templates so HandleEvent's project-match logic
	// picks them up for the event's project (mirrors how server startup
	// materializes the built-in templates in each user project).
	autos, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Status: "active", Limit: 100})
	if err != nil {
		t.Fatalf("List automations: %v", err)
	}
	for _, tmpl := range autos.Entries {
		generated := true
		_, err := brain.Save(ctx, types.CreateEntryRequest{
			Type:               "automation",
			Title:              tmpl.Title,
			Content:            tmpl.Content,
			Status:             "active",
			Project:            "brain",
			Trigger:            tmpl.Trigger,
			Action:             tmpl.Action,
			ExecutionMode:      tmpl.ExecutionMode,
			TargetWorkdir:      tmpl.TargetWorkdir,
			MergeTargetBranch:  tmpl.MergeTargetBranch,
			MergePolicy:        tmpl.MergePolicy,
			MergeStrategy:      tmpl.MergeStrategy,
			RemoteBranchPolicy: tmpl.RemoteBranchPolicy,
			OpenPRBeforeMerge:  tmpl.OpenPRBeforeMerge,
			Generated:          &generated,
			GeneratedBy:        tmpl.GeneratedBy,
		})
		if err != nil {
			t.Fatalf("materialize automation in project: %v", err)
		}
	}

	automationSvc := NewAutomationService(brain)
	err = automationSvc.HandleEvent(ctx, types.Event{
		ID:        "evt-simple",
		Type:      types.EventFeatureCompleted,
		Source:    types.EventSourceAPI,
		ProjectID: "brain",
		FeatureID: "feat-x",
		Metadata:  map[string]string{"checkout_mode": "simple"},
	})
	if err != nil {
		t.Fatalf("HandleEvent (simple): %v", err)
	}

	tasks, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "brain", Limit: 100})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks.Entries) != 1 {
		t.Fatalf("expected exactly 1 generated task (simple only), got %d", len(tasks.Entries))
	}
	task := tasks.Entries[0]
	if task.Executor != "script" {
		t.Errorf("generated task executor = %q, want %q (must be script)", task.Executor, "script")
	}
	if !strings.Contains(task.Content, "git -c merge.ff=true merge --squash") {
		t.Errorf("generated task content missing invariant substring; got: %s", task.Content)
	}
	if !strings.Contains(task.Content, "feat-x") {
		t.Errorf("generated task content did not template FeatureID; got: %s", task.Content)
	}
}

// TestBuiltinFeatureCheckoutDispatch_AIEventOnlyMatchesAIAutomation
// verifies the AI automation fires (not the simple one) for AI events.
func TestBuiltinFeatureCheckoutDispatch_AIEventOnlyMatchesAIAutomation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
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

	autos, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Status: "active", Limit: 100})
	if err != nil {
		t.Fatalf("List automations: %v", err)
	}
	for _, tmpl := range autos.Entries {
		generated := true
		_, err := brain.Save(ctx, types.CreateEntryRequest{
			Type:               "automation",
			Title:              tmpl.Title,
			Content:            tmpl.Content,
			Status:             "active",
			Project:            "brain",
			Trigger:            tmpl.Trigger,
			Action:             tmpl.Action,
			ExecutionMode:      tmpl.ExecutionMode,
			TargetWorkdir:      tmpl.TargetWorkdir,
			MergeTargetBranch:  tmpl.MergeTargetBranch,
			MergePolicy:        tmpl.MergePolicy,
			MergeStrategy:      tmpl.MergeStrategy,
			RemoteBranchPolicy: tmpl.RemoteBranchPolicy,
			OpenPRBeforeMerge:  tmpl.OpenPRBeforeMerge,
			Generated:          &generated,
			GeneratedBy:        tmpl.GeneratedBy,
		})
		if err != nil {
			t.Fatalf("materialize automation in project: %v", err)
		}
	}

	automationSvc := NewAutomationService(brain)
	err = automationSvc.HandleEvent(ctx, types.Event{
		ID:        "evt-ai",
		Type:      types.EventFeatureCompleted,
		Source:    types.EventSourceAPI,
		ProjectID: "brain",
		FeatureID: "feat-y",
		Metadata:  map[string]string{"checkout_mode": "ai"},
	})
	if err != nil {
		t.Fatalf("HandleEvent (ai): %v", err)
	}

	tasks, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "brain", Limit: 100})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks.Entries) != 1 {
		t.Fatalf("expected exactly 1 generated task (AI only), got %d", len(tasks.Entries))
	}
	task := tasks.Entries[0]
	if task.Executor == "script" {
		t.Errorf("AI event should not create a script task, got executor=%q", task.Executor)
	}
	if !strings.Contains(task.Content, "feature-checkout skill") {
		t.Errorf("AI task content missing AI-skill prompt; got: %s", task.Content)
	}
}

// TestBuiltinFeatureCheckoutDispatch_MissingCheckoutModeMatchesAI verifies
// the belt-and-suspenders normalizer: events without checkout_mode in
// metadata still match the AI automation (backward-compat).
func TestBuiltinFeatureCheckoutDispatch_MissingCheckoutModeMatchesAI(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
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

	autos, err := brain.List(ctx, types.ListEntriesRequest{Type: "automation", Status: "active", Limit: 100})
	if err != nil {
		t.Fatalf("List automations: %v", err)
	}
	for _, tmpl := range autos.Entries {
		generated := true
		_, err := brain.Save(ctx, types.CreateEntryRequest{
			Type:               "automation",
			Title:              tmpl.Title,
			Content:            tmpl.Content,
			Status:             "active",
			Project:            "brain",
			Trigger:            tmpl.Trigger,
			Action:             tmpl.Action,
			ExecutionMode:      tmpl.ExecutionMode,
			TargetWorkdir:      tmpl.TargetWorkdir,
			MergeTargetBranch:  tmpl.MergeTargetBranch,
			MergePolicy:        tmpl.MergePolicy,
			MergeStrategy:      tmpl.MergeStrategy,
			RemoteBranchPolicy: tmpl.RemoteBranchPolicy,
			OpenPRBeforeMerge:  tmpl.OpenPRBeforeMerge,
			Generated:          &generated,
			GeneratedBy:        tmpl.GeneratedBy,
		})
		if err != nil {
			t.Fatalf("materialize automation in project: %v", err)
		}
	}

	automationSvc := NewAutomationService(brain)
	// Event with NO checkout_mode metadata (simulates a raw event from a
	// pre-Phase-3 source or a code path that bypasses CheckFeatureCompletion).
	err = automationSvc.HandleEvent(ctx, types.Event{
		ID:        "evt-nometa",
		Type:      types.EventFeatureCompleted,
		Source:    types.EventSourceAPI,
		ProjectID: "brain",
		FeatureID: "feat-z",
	})
	if err != nil {
		t.Fatalf("HandleEvent (no metadata): %v", err)
	}

	tasks, err := brain.List(ctx, types.ListEntriesRequest{Type: "task", Project: "brain", Limit: 100})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks.Entries) != 1 {
		t.Fatalf("expected exactly 1 generated task (AI only, via defaulting), got %d", len(tasks.Entries))
	}
	if tasks.Entries[0].Executor == "script" {
		t.Errorf("missing checkout_mode should default to AI (not script), got executor=%q", tasks.Entries[0].Executor)
	}
}

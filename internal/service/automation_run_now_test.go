package service

import (
	"context"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// RunAutomationNow must reuse the scheduler's task-generation path so a
// manual run can never diverge from cron/event behavior (the PWA and TUI
// used to rebuild the task client-side, and their stricter validation
// rejected automations the scheduler ran fine).

func saveAutomation(t *testing.T, brain *BrainServiceImpl, action *types.AutomationAction) *types.CreateEntryResponse {
	t.Helper()
	resp, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Manual Run Target",
		Content: "manual-run test automation",
		Status:  "active",
		Project: "manual-run-test",
		Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 5 * * *"},
		Action:  action,
	})
	if err != nil {
		t.Fatalf("Save automation: %v", err)
	}
	return resp
}

// runNowOne drives RunAutomationNow for the single-task cases below: every
// automation here owns a project, so the fan-out is a one-element slice.
func runNowOne(t *testing.T, svc *AutomationService, path string) string {
	t.Helper()
	ids, err := svc.RunAutomationNow(context.Background(), path, "")
	if err != nil {
		t.Fatalf("RunAutomationNow: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("expected exactly one generated task, got %d: %v", len(ids), ids)
	}
	return ids[0]
}

func TestRunAutomationNow_PromptAction(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	auto := saveAutomation(t, brain, &types.AutomationAction{
		Type:         "prompt",
		DirectPrompt: "Do the {{.Project}} thing.",
	})

	svc := NewAutomationService(brain)
	taskID := runNowOne(t, svc, auto.Path)

	task, err := brain.Recall(ctx, taskID)
	if err != nil {
		t.Fatalf("recall created task: %v", err)
	}
	if task.GeneratedBy != "automation:"+auto.ID {
		t.Errorf("generated_by = %q, want automation:%s", task.GeneratedBy, auto.ID)
	}
	if !strings.Contains(task.DirectPrompt, "manual-run-test") {
		t.Errorf("template not rendered: %q", task.DirectPrompt)
	}
}

func TestRunAutomationNow_ScriptAction(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	auto := saveAutomation(t, brain, &types.AutomationAction{
		Type:    "script",
		Command: "echo hello",
	})

	svc := NewAutomationService(brain)
	taskID := runNowOne(t, svc, auto.Path)
	task, err := brain.Recall(ctx, taskID)
	if err != nil {
		t.Fatalf("recall created task: %v", err)
	}
	if task.Executor != "script" {
		t.Errorf("executor = %q, want script", task.Executor)
	}
	if task.DirectPrompt != "echo hello" {
		t.Errorf("direct_prompt = %q, want the script command", task.DirectPrompt)
	}
}

func TestRunAutomationNow_RepeatedRunsAreNotDeduped(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	auto := saveAutomation(t, brain, &types.AutomationAction{Type: "prompt", DirectPrompt: "x"})

	svc := NewAutomationService(brain)
	first := runNowOne(t, svc, auto.Path)
	second := runNowOne(t, svc, auto.Path)
	if first == "" || second == "" || first == second {
		t.Fatalf("manual runs must each create a task: first=%q second=%q", first, second)
	}
}

func TestRunAutomationNow_Validation(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	svc := NewAutomationService(brain)

	if _, err := svc.RunAutomationNow(ctx, "does-not-exist", ""); err == nil {
		t.Error("expected error for missing entry")
	}

	// Non-automation entries are rejected.
	note, err := brain.Save(ctx, types.CreateEntryRequest{
		Type: "task", Title: "not an automation", Content: "x", Project: "manual-run-test",
	})
	if err != nil {
		t.Fatalf("save note: %v", err)
	}
	if _, err := svc.RunAutomationNow(ctx, note.Path, ""); err == nil || !strings.Contains(err.Error(), "not an automation") {
		t.Errorf("expected not-an-automation error, got %v", err)
	}
}

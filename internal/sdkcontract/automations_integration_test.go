package sdkcontract_test

import (
	"context"
	"testing"

	"github.com/huynle/brain-api/sdk/brain"
)

func exerciseAutomationSDK(t *testing.T, c *brain.Client) {
	t.Helper()
	ctx := context.Background()
	project := "sdk-automation"
	kind := "automation_run"
	content := "automation_id: sdk-automation-fixture"
	entry, err := c.Entries().Create(ctx, brain.CreateEntryRequest{Project: &project, Type: kind, Title: "SDK run history", Content: content}, brain.RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// entries.get by legacy path: the SDK sends the path as one escaped
	// segment, which the real handler must resolve like the ID.
	if byPath, err := c.Entries().Get(ctx, entry.Path); err != nil || byPath.Id != entry.Id {
		t.Fatalf("entries.get by legacy path %q: %+v %v", entry.Path, byPath, err)
	}
	run, err := c.Automations().GetRun(ctx, entry.Id)
	if err != nil || run.Content != content {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	id := "sdk-automation-fixture"
	runs, err := c.Automations().Runs(ctx, &brain.AutomationsRunsParams{Project: &project, AutomationId: &id})
	if err != nil || runs.Entries == nil || len(*runs.Entries) != 1 {
		t.Fatalf("runs=%+v err=%v", runs, err)
	}
	if err := c.Entries().Delete(ctx, entry.Id, false); err != nil {
		t.Fatal(err)
	}
	active := "active"
	prompt := "Do the {{.Project}} thing."
	schedule := "0 5 * * *"
	triggerType := "cron"
	automation, err := c.Entries().Create(ctx, brain.CreateEntryRequest{Project: &project, Type: "automation", Title: "SDK manual automation", Content: "manual trigger fixture", Status: &active, Trigger: &brain.TriggerConfig{Type: &triggerType, Schedule: &schedule}, Action: &brain.AutomationAction{Type: "prompt", DirectPrompt: &prompt}}, brain.RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.Automations().Run(ctx, brain.RunAutomationRequest{Path: automation.Id}, brain.RequestOptions{})
	if err != nil || len(result.TaskIds) != 1 {
		t.Fatalf("run=%+v err=%v", result, err)
	}
	generated, err := c.Entries().Get(ctx, result.TaskId)
	if err != nil || generated.Type != "task" || generated.GeneratedBy == nil || *generated.GeneratedBy != "automation:"+automation.Id {
		t.Fatalf("generated=%+v err=%v", generated, err)
	}
	if err := c.Entries().Delete(ctx, result.TaskId, false); err != nil {
		t.Fatal(err)
	}
	if err := c.Entries().Delete(ctx, automation.Id, false); err != nil {
		t.Fatal(err)
	}
	t.Log("real automation SDK parity: filtered run history/get and actual manual task generation; no executor started")
}

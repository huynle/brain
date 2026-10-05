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
	t.Log("real automation run-history SDK parity: filtered list and typed get; no work submitted")
}

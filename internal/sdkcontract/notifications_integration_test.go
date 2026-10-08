package sdkcontract_test

import (
	"context"
	"github.com/huynle/brain-api/sdk/brain"
	"testing"
	"time"
)

func exerciseNotificationSDK(t *testing.T, c *brain.Client) {
	t.Helper()
	ctx := context.Background()
	project := "sdk-notifications"
	reminder, err := c.Reminders().Create(ctx, brain.CreateReminderRequest{Project: &project, Title: "SDK reminder", Config: brain.ReminderConfig{}}, brain.RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	title := "updated reminder"
	if _, err := c.Reminders().Update(ctx, reminder.ReminderId, brain.UpdateReminderRequest{Title: &title}, brain.RequestOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Reminders().List(ctx, &brain.RemindersListParams{Project: &project}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Reminders().Get(ctx, reminder.ReminderId); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Reminders().Snooze(ctx, reminder.ReminderId, brain.SnoozeReminderRequest{RemindAt: time.Now().Add(24 * time.Hour).Format(time.RFC3339)}, brain.RequestOptions{}); err != nil {
		t.Fatal(err)
	}
	if fired, err := c.Reminders().Fire(ctx, reminder.ReminderId, brain.RequestOptions{}); err != nil || fired.FiredAt == nil || fired.FireCount == nil || *fired.FireCount != 1 || fired.GeneratedTaskId != nil {
		t.Fatalf("notify firing=%+v err=%v", fired, err)
	}
	if _, err := c.Reminders().Ack(ctx, reminder.ReminderId, brain.RequestOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Reminders().Delete(ctx, reminder.ReminderId, brain.RequestOptions{}); err != nil {
		t.Fatal(err)
	}
	item, err := c.Attention().Create(ctx, brain.CreateAttentionRequest{Kind: "sdk", Title: "SDK notification"}, brain.RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if item.Recipient != "sdk-example" {
		t.Fatalf("recipient not bound to real token: %q", item.Recipient)
	}
	if _, err := c.Attention().Get(ctx, item.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Attention().List(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if counts, err := c.Attention().Counts(ctx); err != nil || counts.Unread != 1 {
		t.Fatalf("counts=%+v err=%v", counts, err)
	}
	if _, err := c.Attention().Read(ctx, item.Id, brain.RequestOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Attention().Unread(ctx, item.Id, brain.RequestOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Attention().Snooze(ctx, item.Id, brain.SnoozeAttentionRequest{SnoozedUntil: time.Now().Add(time.Hour).Format(time.RFC3339)}, brain.RequestOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Attention().Resolve(ctx, item.Id, brain.RequestOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Attention().Dismiss(ctx, item.Id, brain.RequestOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Log("real SDK notification parity: eight reminder and nine recipient-bound attention operations; notify fire creates no task")
}

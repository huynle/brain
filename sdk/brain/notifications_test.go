package brain_test

import (
	"context"
	"github.com/huynle/brain-api/sdk/brain"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestReminderAndAttentionRoutes(t *testing.T) {
	var got []string
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI())
		_, _ = w.Write([]byte(`{}`))
	}, brain.Config{})
	ctx := context.Background()
	project := "p"
	state := "active"
	yes := true
	_, a := c.Reminders().List(ctx, &brain.RemindersListParams{Project: &project, State: &state})
	_, b := c.Reminders().Get(ctx, "r")
	_, d := c.Reminders().Create(ctx, brain.CreateReminderRequest{}, brain.RequestOptions{})
	_, e := c.Reminders().Update(ctx, "r", brain.UpdateReminderRequest{}, brain.RequestOptions{})
	_, f := c.Reminders().Ack(ctx, "r", brain.RequestOptions{})
	_, g := c.Reminders().Snooze(ctx, "r", brain.SnoozeReminderRequest{RemindAt: time.Now()}, brain.RequestOptions{})
	_, h := c.Reminders().Fire(ctx, "r", brain.RequestOptions{})
	_, i := c.Reminders().Delete(ctx, "r", brain.RequestOptions{})
	_, j := c.Attention().List(ctx, &brain.AttentionListParams{Project: &project, IncludeSnoozed: &yes})
	_, k := c.Attention().Counts(ctx)
	_, l := c.Attention().Get(ctx, "a")
	_, m := c.Attention().Create(ctx, brain.CreateAttentionRequest{}, brain.RequestOptions{})
	_, n := c.Attention().Read(ctx, "a", brain.RequestOptions{})
	_, o := c.Attention().Unread(ctx, "a", brain.RequestOptions{})
	_, p := c.Attention().Snooze(ctx, "a", brain.SnoozeAttentionRequest{SnoozedUntil: time.Now()}, brain.RequestOptions{})
	_, q := c.Attention().Resolve(ctx, "a", brain.RequestOptions{})
	_, r := c.Attention().Dismiss(ctx, "a", brain.RequestOptions{})
	for _, err := range []error{a, b, d, e, f, g, h, i, j, k, l, m, n, o, p, q, r} {
		if err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"GET /api/v1/reminders?project=p&state=active", "GET /api/v1/reminders/r", "POST /api/v1/reminders", "PATCH /api/v1/reminders/r", "POST /api/v1/reminders/r/ack", "POST /api/v1/reminders/r/snooze", "POST /api/v1/reminders/r/fire", "DELETE /api/v1/reminders/r", "GET /api/v1/attention?include_snoozed=true&project=p", "GET /api/v1/attention/counts", "GET /api/v1/attention/a", "POST /api/v1/attention", "POST /api/v1/attention/a/read", "POST /api/v1/attention/a/unread", "POST /api/v1/attention/a/snooze", "POST /api/v1/attention/a/resolve", "POST /api/v1/attention/a/dismiss"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}

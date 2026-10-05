package sdkcontract_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/huynle/brain-api/sdk/brain"
)

func exerciseWebhookSDK(t *testing.T, c *brain.Client) {
	t.Helper()
	ctx := context.Background()
	var deliveries atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event map[string]any
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Error(err)
		}
		if event["type"] != "webhook.test" {
			t.Errorf("unexpected event %+v", event)
		}
		deliveries.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	wh, err := c.Webhooks().Create(ctx, brain.CreateWebhookRequest{Name: "SDK webhook", Url: receiver.URL, Events: &[]string{"webhook.test"}}, brain.RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	name := "SDK webhook updated"
	if changed, err := c.Webhooks().Update(ctx, wh.Id, brain.UpdateWebhookRequest{Name: &name}, brain.RequestOptions{}); err != nil || changed.Name != name {
		t.Fatalf("update=%+v err=%v", changed, err)
	}
	if got, err := c.Webhooks().Get(ctx, wh.Id); err != nil || got.Name != name {
		t.Fatalf("get=%+v err=%v", got, err)
	}
	if list, err := c.Webhooks().List(ctx, true); err != nil || len(list.Webhooks) != 1 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if result, err := c.Webhooks().Test(ctx, wh.Id, brain.RequestOptions{}); err != nil || !result.Success {
		t.Fatalf("delivery=%+v err=%v", result, err)
	}
	if deliveries.Load() != 1 {
		t.Fatalf("receiver calls=%d", deliveries.Load())
	}
	if history, err := c.Webhooks().Deliveries(ctx, wh.Id, 50); err != nil || len(history.Deliveries) != 1 {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	if deleted, err := c.Webhooks().Delete(ctx, wh.Id, brain.RequestOptions{}); err != nil || !deleted.Success {
		t.Fatalf("delete=%+v err=%v", deleted, err)
	}
	t.Log("real webhook SDK parity: seven operations, one actual local HTTP delivery")
}

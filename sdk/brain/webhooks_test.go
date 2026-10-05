package brain_test

import (
	"context"
	"github.com/huynle/brain-api/sdk/brain"
	"net/http"
	"reflect"
	"testing"
)

func TestWebhookOperationRoutes(t *testing.T) {
	var got []string
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI())
		_, _ = w.Write([]byte(`{}`))
	}, brain.Config{})
	ctx := context.Background()
	o := brain.RequestOptions{}
	_, a := c.Webhooks().List(ctx, true)
	_, b := c.Webhooks().Create(ctx, brain.CreateWebhookRequest{}, o)
	_, d := c.Webhooks().Get(ctx, "w")
	_, e := c.Webhooks().Update(ctx, "w", brain.UpdateWebhookRequest{}, o)
	_, f := c.Webhooks().Deliveries(ctx, "w", 50)
	_, g := c.Webhooks().Test(ctx, "w", o)
	_, h := c.Webhooks().Delete(ctx, "w", o)
	for _, err := range []error{a, b, d, e, f, g, h} {
		if err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"GET /api/v1/webhooks?enabled=true", "POST /api/v1/webhooks", "GET /api/v1/webhooks/w", "PATCH /api/v1/webhooks/w", "GET /api/v1/webhooks/w/deliveries?limit=50", "POST /api/v1/webhooks/w/test", "DELETE /api/v1/webhooks/w"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}

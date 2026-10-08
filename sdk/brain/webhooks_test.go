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

// A non-positive limit is invalid on the server (400), so it means "server
// default" (50, the TypeScript default) and is omitted rather than sent.
func TestWebhookDeliveriesOmitsNonPositiveLimit(t *testing.T) {
	var got []string
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.RequestURI())
		_, _ = w.Write([]byte(`{"deliveries":[]}`))
	}, brain.Config{})
	for _, limit := range []int{0, -1, 7} {
		if _, err := c.Webhooks().Deliveries(context.Background(), "w", limit); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"/api/v1/webhooks/w/deliveries", "/api/v1/webhooks/w/deliveries", "/api/v1/webhooks/w/deliveries?limit=7"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}

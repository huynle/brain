package brain_test

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/huynle/brain-api/sdk/brain"
)

func TestObservabilityProjectOrphanRoutes(t *testing.T) {
	var got []string
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.RequestURI())
		_, _ = w.Write([]byte(`null`))
	}, brain.Config{})
	ctx := context.Background()
	_, a := c.Projects().List(ctx)
	_, b := c.Observability().Stats(ctx, "p q", "a,b", true)
	_, d := c.Observability().Stale(ctx, "p q", "note", 7, 8)
	_, e := c.Graph().Orphans(ctx, "p q", "note", 9)
	for _, err := range []error{a, b, d, e} {
		if err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"/api/v1/tasks", "/api/v1/stats?global=true&project=p+q&projects=a%2Cb", "/api/v1/stale?days=7&limit=8&project=p+q&type=note", "/api/v1/orphans?limit=9&project=p+q&type=note"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

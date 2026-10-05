package brain_test

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/huynle/brain-api/sdk/brain"
)

func TestTaskSelectionRoutes(t *testing.T) {
	var got []string
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI())
		_, _ = w.Write([]byte(`{"tasks":[]}`))
	}, brain.Config{})
	for _, call := range []func(context.Context, string) (*brain.TaskSelectionResponse, error){c.Tasks().Waiting, c.Tasks().Blocked} {
		out, err := call(context.Background(), "p q")
		if err != nil || out.Tasks == nil || len(*out.Tasks) != 0 {
			t.Fatalf("response=%+v err=%v", out, err)
		}
	}
	if !reflect.DeepEqual(got, []string{"GET /api/v1/tasks/p%20q/waiting", "GET /api/v1/tasks/p%20q/blocked"}) {
		t.Fatal(got)
	}
}

func TestNextPreservesNullSelection(t *testing.T) {
	c := client(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`null`)) }, brain.Config{})
	got, err := c.Tasks().Next(context.Background(), "empty", nil)
	if err != nil || got != nil {
		t.Fatalf("expected nil selection, got %+v err=%v", got, err)
	}
}

func TestReadyNextFiltersPreserveRepeatedValues(t *testing.T) {
	var got []string
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.RequestURI())
		_, _ = w.Write([]byte(`{"tasks":[],"id":"one"}`))
	}, brain.Config{})
	features := brain.TaskFeatureFilter{"a,b", "c & d"}
	executors := "opencode,pi"
	runner := "r one"
	prefix := "automation:foo"
	ctx := context.Background()
	ready, err := c.Tasks().Ready(ctx, "p q", &brain.TasksReadyParams{FeatureId: &features, Executors: &executors, RunnerId: &runner, GeneratedByPrefix: &prefix})
	if err != nil || ready == nil {
		t.Fatalf("missing ready response: %+v %v", ready, err)
	}
	next, err := c.Tasks().Next(ctx, "p q", &brain.TasksNextParams{FeatureId: &features, Executors: &executors, RunnerId: &runner, GeneratedByPrefix: &prefix})
	if err != nil || next == nil || next.Id != "one" {
		t.Fatalf("missing next response: %+v %v", next, err)
	}
	if _, err := c.Tasks().Ready(ctx, "p", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Tasks().Next(ctx, "p", nil); err != nil {
		t.Fatal(err)
	}
	q := "?executors=opencode%2Cpi&feature_id=a%2Cb&feature_id=c+%26+d&generated_by_prefix=automation%3Afoo&runner_id=r+one"
	want := []string{"/api/v1/tasks/p%20q/ready" + q, "/api/v1/tasks/p%20q/next" + q, "/api/v1/tasks/p/ready", "/api/v1/tasks/p/next"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

package brain_test

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/huynle/brain-api/sdk/brain"
)

func TestTaskAndFeatureReadRoutes(t *testing.T) {
	var seen []string
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		if r.Method == "POST" {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if !reflect.DeepEqual(body, map[string]any{"taskIds": []any{"one", "missing"}}) {
				t.Errorf("body=%v", body)
			}
		}
		_, _ = w.Write([]byte(`{"tasks":[],"allCompleted":false,"notFound":["missing"],"path":"p","complete_on_idle":null,"claimed":false,"isStale":false,"taskId":"one","features":null,"feature":{"featureId":"f & q","tasks":null,"ready":false}}`))
	}, brain.Config{})
	ctx := context.Background()
	ids := []string{"one", "missing"}
	status, err := c.Tasks().Status(ctx, "p q", brain.MultiTaskStatusRequest{TaskIds: &ids})
	if err != nil || status.AllCompleted || status.NotFound == nil || !reflect.DeepEqual(*status.NotFound, []string{"missing"}) {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	metadata, err := c.Tasks().Metadata(ctx, "p q", "one & two")
	if err != nil || metadata.Path != "p" || metadata.CompleteOnIdle != nil {
		t.Fatalf("metadata=%+v err=%v", metadata, err)
	}
	claim, err := c.Tasks().ClaimStatus(ctx, "p q", "one & two")
	if err != nil || claim.Claimed || claim.IsStale || claim.TaskId != "one" {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	for _, call := range []func(context.Context, string) (*brain.FeatureListResponse, error){c.Features().List, c.Features().Ready} {
		out, err := call(ctx, "p q")
		if err != nil || out.Features != nil {
			t.Fatalf("features=%+v err=%v", out, err)
		}
	}
	feature, err := c.Features().Get(ctx, "p q", "f & q")
	if err != nil || feature.Feature.FeatureId != "f & q" {
		t.Fatalf("feature=%+v err=%v", feature, err)
	}
	want := []string{"POST /api/v1/tasks/p%20q/status", "GET /api/v1/tasks/p%20q/one%20&%20two/metadata", "GET /api/v1/tasks/p%20q/one%20&%20two/claim-status", "GET /api/v1/tasks/p%20q/features", "GET /api/v1/tasks/p%20q/features/ready", "GET /api/v1/tasks/p%20q/features/f%20&%20q"}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("routes=%v want=%v", seen, want)
	}
}

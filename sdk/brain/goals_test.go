package brain_test

import (
	"context"
	"github.com/huynle/brain-api/sdk/brain"
	"net/http"
	"reflect"
	"testing"
)

func TestGoalOperationRoutes(t *testing.T) {
	var got []string
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI())
		_, _ = w.Write([]byte(`{}`))
	}, brain.Config{})
	ctx := context.Background()
	project := "p"
	status := "all"
	_, a := c.Goals().List(ctx, &brain.GoalsListParams{Project: &project, Status: &status})
	_, b := c.Goals().Create(ctx, brain.CreateGoalRequest{}, brain.RequestOptions{})
	_, d := c.Goals().Update(ctx, "g", brain.UpdateGoalRequest{}, brain.RequestOptions{})
	_, e := c.Goals().Progress(ctx, "g")
	_, f := c.Goals().Audit(ctx, "g", 50)
	_, g := c.Goals().Run(ctx, "g", brain.RequestOptions{})
	_, h := c.Goals().Delete(ctx, "g", brain.RequestOptions{})
	for _, err := range []error{a, b, d, e, f, g, h} {
		if err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"GET /api/v1/goals?project=p&status=all", "POST /api/v1/goals", "PATCH /api/v1/goals/g", "GET /api/v1/goals/g/progress", "GET /api/v1/goals/g/audit?limit=50", "POST /api/v1/goals/g/run", "DELETE /api/v1/goals/g"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}

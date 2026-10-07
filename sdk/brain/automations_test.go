package brain_test

import (
	"context"
	"github.com/huynle/brain-api/sdk/brain"
	"net/http"
	"reflect"
	"testing"
)

func TestAutomationOperationRoutes(t *testing.T) {
	var got []string
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI())
		_, _ = w.Write([]byte(`{}`))
	}, brain.Config{})
	ctx := context.Background()
	project := "p"
	id := "a"
	limit := 10
	_, a := c.Automations().Run(ctx, brain.RunAutomationRequest{Path: "automation"}, brain.RequestOptions{})
	_, b := c.Automations().Runs(ctx, &brain.AutomationsRunsParams{Project: &project, AutomationId: &id, Limit: &limit})
	_, d := c.Automations().GetRun(ctx, "r")
	for _, err := range []error{a, b, d} {
		if err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"POST /api/v1/automations/run", "GET /api/v1/automation-runs?automation_id=a&limit=10&project=p", "GET /api/v1/automation-runs/r"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}

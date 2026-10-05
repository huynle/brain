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

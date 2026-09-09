package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

func TestResourceHealthScopeAndFreshness(t *testing.T) {
	sample, _ := json.Marshal(types.ResourceSample{SampledAt: time.Now().Add(-time.Minute), UnavailableReason: "sampling_failed"})
	es := &mockEventService{recentFunc: func(_ context.Context, _ int, filters map[string]string) ([]types.Event, error) {
		if filters["project_id"] != "p" || filters["task_id"] != "t" || filters["type"] != types.EventTaskResourceSample {
			t.Fatal(filters)
		}
		return []types.Event{{TaskID: "t", RunnerID: "r", Metadata: map[string]string{"resource": string(sample)}}}, nil
	}}
	h := &Handler{events: es}
	w := httptest.NewRecorder()
	h.HandleResourceHealth(w, httptest.NewRequest("GET", "/?project_id=p&task_id=t", nil))
	var out struct {
		Samples []struct {
			Fresh  bool
			Sample types.ResourceSample
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Samples) != 1 || out.Samples[0].Fresh || out.Samples[0].Sample.RSSBytes != nil {
		t.Fatal(out)
	}
}

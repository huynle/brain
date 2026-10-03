package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/types"
)

type fakeTimelineService struct {
	from, to time.Time
	project  string
}

func (f *fakeTimelineService) Timeline(_ context.Context, from, to time.Time, project string) (*types.TimelineResponse, error) {
	f.from, f.to, f.project = from, to, project
	return &types.TimelineResponse{From: from, To: to, GeneratedAt: from, Items: []types.TimelineItem{}, Warnings: []types.TimelineWarning{}}, nil
}

func TestHandleTimelineParsesRangeAndProject(t *testing.T) {
	service := &fakeTimelineService{}
	handler := NewHandler(&mockBrainService{}, WithTimelineService(service))
	router := NewRouter(config.Config{}, WithHandler(handler))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/timeline?from=2026-10-03T00:00:00Z&to=2026-11-02T00:00:00Z&project=demo", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", recorder.Code, recorder.Body.String())
	}
	if service.project != "demo" || service.from.Format(time.RFC3339) != "2026-10-03T00:00:00Z" || service.to.Format(time.RFC3339) != "2026-11-02T00:00:00Z" {
		t.Fatalf("unexpected query: from=%s to=%s project=%q", service.from, service.to, service.project)
	}
	var response types.TimelineResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
}

func TestHandleTimelineRejectsInvertedOrOversizedRanges(t *testing.T) {
	service := &fakeTimelineService{}
	handler := NewHandler(&mockBrainService{}, WithTimelineService(service))
	router := NewRouter(config.Config{}, WithHandler(handler))

	for _, rawQuery := range []string{
		"from=2026-10-04T00:00:00Z&to=2026-10-03T00:00:00Z",
		"from=2026-01-01T00:00:00Z&to=2027-01-03T00:00:00Z",
		"from=not-a-date&to=2026-10-03T00:00:00Z",
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/timeline?"+rawQuery, nil)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("query %q status = %d, want 400", rawQuery, recorder.Code)
		}
	}
}

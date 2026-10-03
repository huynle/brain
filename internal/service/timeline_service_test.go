package service

import (
	"context"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

type timelineEntryListerFake struct {
	requests []types.ListEntriesRequest
	byType   map[string][]types.BrainEntry
}

func (f *timelineEntryListerFake) List(_ context.Context, request types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
	f.requests = append(f.requests, request)
	entries := f.byType[request.Type]
	if request.Offset >= len(entries) {
		return &types.ListEntriesResponse{Entries: []types.BrainEntry{}}, nil
	}
	end := min(len(entries), request.Offset+request.Limit)
	return &types.ListEntriesResponse{Entries: entries[request.Offset:end]}, nil
}

type timelineEventReaderFake struct {
	filters map[string]string
	events  []types.Event
}

func (f *timelineEventReaderFake) Recent(_ context.Context, _ int, filters map[string]string) ([]types.Event, error) {
	f.filters = filters
	return f.events, nil
}

func TestTimelineServiceLoadsEverySourceTypeWithProjectScope(t *testing.T) {
	from := timelineTime(t, "2026-10-03T00:00:00Z")
	to := timelineTime(t, "2026-11-02T00:00:00Z")
	lister := &timelineEntryListerFake{byType: map[string][]types.BrainEntry{
		"task":       {{ID: "task", Type: "task", Status: "active", Schedule: "0 9 * * *", ProjectID: "demo"}},
		"automation": {}, "reminder": {},
	}}
	events := &timelineEventReaderFake{}
	service := NewTimelineService(lister, events, WithTimelineClock(func() time.Time { return from }))

	result, err := service.Timeline(context.Background(), from, to, "demo")

	if err != nil {
		t.Fatal(err)
	}
	if len(lister.requests) != 3 {
		t.Fatalf("list requests = %d, want task/automation/reminder", len(lister.requests))
	}
	for _, request := range lister.requests {
		if request.Project != "demo" {
			t.Fatalf("request project = %q", request.Project)
		}
	}
	if events.filters["project_id"] != "demo" {
		t.Fatalf("event filters = %#v", events.filters)
	}
	if len(result.Items) == 0 {
		t.Fatal("expected projected task occurrences")
	}
}

package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/huynle/brain-api/internal/calendar"
	"github.com/huynle/brain-api/internal/types"
)

// fakeAutomationLister serves automation entries in pages, the way
// BrainServiceImpl.List does, and records each request.
type fakeAutomationLister struct {
	entries  []types.BrainEntry
	requests []types.ListEntriesRequest
	err      error
}

func (f *fakeAutomationLister) List(_ context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return nil, f.err
	}
	if req.Type != "automation" {
		return &types.ListEntriesResponse{}, nil
	}
	start := req.Offset
	if start > len(f.entries) {
		start = len(f.entries)
	}
	end := start + req.Limit
	if end > len(f.entries) {
		end = len(f.entries)
	}
	return &types.ListEntriesResponse{Entries: f.entries[start:end]}, nil
}

func automationWith(id, status string, tc *types.TriggerConfig) types.BrainEntry {
	return types.BrainEntry{
		ID:      id,
		Path:    "projects/p/automation/" + id + ".md",
		Title:   "Automation " + id,
		Type:    "automation",
		Status:  status,
		Trigger: tc,
	}
}

func TestCalendarReferencesReportsEveryFieldAndStatus(t *testing.T) {
	lister := &fakeAutomationLister{entries: []types.BrainEntry{
		automationWith("a-calendar", "active", &types.TriggerConfig{Type: types.TriggerTypeCalendar, Calendar: "personal"}),
		automationWith("a-skip", "paused", &types.TriggerConfig{Type: types.TriggerTypeCron, SkipIfEvent: &types.CalendarEventFilter{Calendar: "personal"}}),
		automationWith("a-only", "archived", &types.TriggerConfig{Type: types.TriggerTypeCron, OnlyIfEvent: &types.CalendarEventFilter{Calendar: "personal", Title: "re:^OK"}}),
		automationWith("a-other", "active", &types.TriggerConfig{Type: types.TriggerTypeCalendar, Calendar: "work", SkipIfEvent: &types.CalendarEventFilter{Calendar: "work"}}),
		automationWith("a-none", "active", nil),
		automationWith("a-both", "active", &types.TriggerConfig{
			Type:        types.TriggerTypeCron,
			SkipIfEvent: &types.CalendarEventFilter{Calendar: "personal"},
			OnlyIfEvent: &types.CalendarEventFilter{Calendar: "personal"},
		}),
	}}

	refs, err := NewCalendarReferenceFinder(lister)(context.Background(), "personal")
	if err != nil {
		t.Fatalf("finder: %v", err)
	}
	want := []calendar.Reference{
		{ID: "a-calendar", Path: "projects/p/automation/a-calendar.md", Title: "Automation a-calendar", Field: "trigger.calendar"},
		{ID: "a-skip", Path: "projects/p/automation/a-skip.md", Title: "Automation a-skip", Field: "trigger.skip_if_event"},
		{ID: "a-only", Path: "projects/p/automation/a-only.md", Title: "Automation a-only", Field: "trigger.only_if_event"},
		{ID: "a-both", Path: "projects/p/automation/a-both.md", Title: "Automation a-both", Field: "trigger.skip_if_event"},
		{ID: "a-both", Path: "projects/p/automation/a-both.md", Title: "Automation a-both", Field: "trigger.only_if_event"},
	}
	if fmt.Sprint(refs) != fmt.Sprint(want) {
		t.Fatalf("references\n got %+v\nwant %+v", refs, want)
	}
	for _, req := range lister.requests {
		if req.Type != "automation" {
			t.Fatalf("scanned type %q, want automation", req.Type)
		}
		if req.Status != "" {
			t.Fatalf("status filter %q set; every status must be scanned", req.Status)
		}
	}
}

func TestCalendarReferencesPagesThroughEveryAutomation(t *testing.T) {
	const total = 450
	entries := make([]types.BrainEntry, 0, total)
	for i := 0; i < total; i++ {
		entries = append(entries, automationWith(fmt.Sprintf("a-%03d", i), "active", nil))
	}
	// The only match sits on the last page, past two full pages of 200.
	entries[total-1] = automationWith("last", "active", &types.TriggerConfig{Type: types.TriggerTypeCron, SkipIfEvent: &types.CalendarEventFilter{Calendar: "personal"}})
	lister := &fakeAutomationLister{entries: entries}

	refs, err := NewCalendarReferenceFinder(lister)(context.Background(), "personal")
	if err != nil {
		t.Fatalf("finder: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "last" {
		t.Fatalf("references = %+v, want the last automation", refs)
	}
	if len(lister.requests) != 3 {
		t.Fatalf("pages requested = %d, want 3", len(lister.requests))
	}
}

func TestCalendarReferencesWithNoMatchesIsEmpty(t *testing.T) {
	lister := &fakeAutomationLister{entries: []types.BrainEntry{
		automationWith("a", "active", &types.TriggerConfig{Type: types.TriggerTypeCron, Calendar: "xnys"}),
	}}
	refs, err := NewCalendarReferenceFinder(lister)(context.Background(), "personal")
	if err != nil || len(refs) != 0 {
		t.Fatalf("references = %+v, %v; want none", refs, err)
	}
}

func TestCalendarReferencesPropagatesListErrors(t *testing.T) {
	boom := errors.New("storage unavailable")
	lister := &fakeAutomationLister{err: boom}
	if _, err := NewCalendarReferenceFinder(lister)(context.Background(), "personal"); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want %v", err, boom)
	}
}

func TestCalendarReferencesNilListerIsAnError(t *testing.T) {
	if _, err := NewCalendarReferenceFinder(nil)(context.Background(), "personal"); err == nil {
		t.Fatal("nil lister returned no error; removal would silently skip the reference check")
	}
}

// An empty name is never a source; automations with no calendar must not match
// it, or a blank removal would report every trigger.
func TestCalendarReferencesEmptyNameMatchesNothing(t *testing.T) {
	lister := &fakeAutomationLister{entries: []types.BrainEntry{
		automationWith("bare", "active", &types.TriggerConfig{Type: types.TriggerTypeCron}),
		automationWith("empty-filter", "active", &types.TriggerConfig{SkipIfEvent: &types.CalendarEventFilter{Title: "x"}}),
	}}
	refs, err := NewCalendarReferenceFinder(lister)(context.Background(), "")
	if err != nil || len(refs) != 0 {
		t.Fatalf("references for empty name = %+v, %v; want none", refs, err)
	}
}

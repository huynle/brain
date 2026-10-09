package service

import (
	"context"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// The timeline projects each project a parent fires for under that project's
// effective config, so it cannot disagree with the scheduler about a binding.
// It is backed by a real brain, as in production, so bindings are visible.

func timelineBindingService(brain *BrainServiceImpl, from time.Time, projects []string) *TimelineService {
	automation := NewAutomationService(brain)
	automation.SetProjectLister(&stubProjectLister{projects: projects})
	return NewTimelineService(brain, &timelineEventReaderFake{},
		WithTimelineClock(func() time.Time { return from }),
		WithTimelineTargets(automation))
}

// saveTimelineBinding saves a cron binding of parent for project. An inactive
// binding is saved as status when given.
func saveTimelineBinding(t *testing.T, brain *BrainServiceImpl, parent, project, schedule, status string, maxRuns *int) {
	t.Helper()
	if status == "" {
		status = "active"
	}
	req := types.CreateEntryRequest{
		Type:    "automation",
		Title:   "Timeline binding " + project,
		Status:  status,
		Project: project,
		Extends: parent,
		MaxRuns: maxRuns,
	}
	if schedule != "" {
		req.Trigger = &types.TriggerConfig{Type: types.TriggerTypeCron, Schedule: schedule}
	}
	if _, err := brain.Save(context.Background(), req); err != nil {
		t.Fatalf("save timeline binding for %s: %v", project, err)
	}
}

func TestTimelineBindingProjectsItsOwnSchedule(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := saveGlobalDreamParent(t, brain, "Dream")
	saveTimelineBinding(t, brain, parent, "p1", "0 6 * * *", "", nil)

	from := timelineTime(t, "2026-10-08T00:00:00Z")
	to := timelineTime(t, "2026-10-09T00:00:00Z")
	result, err := timelineBindingService(brain, from, []string{"p1", "p2"}).Timeline(context.Background(), from, to, "")
	if err != nil {
		t.Fatal(err)
	}

	p1 := projectionInstants(result.Items, parent, "p1")
	if len(p1) != 1 || !p1[0].Equal(timelineTime(t, "2026-10-08T06:00:00Z")) {
		t.Fatalf("p1 projected %v, want its binding's 06:00 only", p1)
	}
	p2 := projectionInstants(result.Items, parent, "p2")
	if len(p2) != 1 || !p2[0].Equal(timelineTime(t, "2026-10-08T03:00:00Z")) {
		t.Fatalf("p2 projected %v, want the parent's 03:00", p2)
	}
}

func TestTimelineOptedOutProjectIsNotProjected(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := saveGlobalDreamParent(t, brain, "Dream")
	saveTimelineBinding(t, brain, parent, "p1", "", "inactive", nil)

	from := timelineTime(t, "2026-10-08T00:00:00Z")
	to := timelineTime(t, "2026-10-09T00:00:00Z")
	result, err := timelineBindingService(brain, from, []string{"p1", "p2"}).Timeline(context.Background(), from, to, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := projectionInstants(result.Items, parent, "p1"); len(got) != 0 {
		t.Fatalf("opted-out p1 projected %v, want none", got)
	}
	if got := projectionInstants(result.Items, parent, "p2"); len(got) != 1 {
		t.Fatalf("p2 projected %v, want its one 03:00 run", got)
	}
}

func TestTimelineBindingBoundsItsOwnMaxRuns(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := saveGlobalDreamParent(t, brain, "Dream")
	one := 1
	saveTimelineBinding(t, brain, parent, "p1", "0 6 * * *", "", &one)

	from := timelineTime(t, "2026-10-08T00:00:00Z")
	to := timelineTime(t, "2026-10-11T00:00:00Z")
	result, err := timelineBindingService(brain, from, []string{"p1", "p2"}).Timeline(context.Background(), from, to, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := projectionInstants(result.Items, parent, "p1"); len(got) != 1 {
		t.Fatalf("p1 under max_runs 1 projected %d runs, want 1", len(got))
	}
	if got := projectionInstants(result.Items, parent, "p2"); len(got) != 3 {
		t.Fatalf("p2 keeps the parent's uncapped schedule: %d runs, want 3", len(got))
	}
}

func TestTimelineIgnoresBindingsAsSourcesOfTheirOwn(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	parent := saveGlobalDreamParent(t, brain, "Dream")
	saveTimelineBinding(t, brain, parent, "p1", "0 6 * * *", "", nil)

	from := timelineTime(t, "2026-10-08T00:00:00Z")
	to := timelineTime(t, "2026-10-09T00:00:00Z")
	result, err := timelineBindingService(brain, from, []string{"p1"}).Timeline(context.Background(), from, to, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range result.Items {
		if item.SourceKind == "automation" && item.SourceID != parent {
			t.Fatalf("a binding projected under its own id %q: %+v", item.SourceID, item)
		}
	}
}

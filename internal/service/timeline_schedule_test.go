package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/schedule"
)

// projectScopedLister answers List the way the entry store does for the
// timeline: by type, narrowed to one project when asked. Global entries (no
// project) are always returned.
type projectScopedLister struct {
	byType map[string][]types.BrainEntry
}

func (f *projectScopedLister) List(_ context.Context, request types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
	var out []types.BrainEntry
	for _, entry := range f.byType[request.Type] {
		if request.Project != "" && entry.ProjectID != "" && entry.ProjectID != request.Project {
			continue
		}
		if request.Tags != "" && !timelineTestHasTag(entry, request.Tags) {
			continue
		}
		out = append(out, entry)
	}
	return &types.ListEntriesResponse{Entries: out}, nil
}

func timelineTestHasTag(entry types.BrainEntry, tag string) bool {
	for _, have := range entry.Tags {
		if have == tag {
			return true
		}
	}
	return false
}

// timelineServiceWithTargets wires the real AutomationService project
// resolution (the one the scheduler uses) into a timeline over the given
// automations and audit runs.
func timelineServiceWithTargets(from time.Time, projects []string, automations []types.BrainEntry, runs []types.BrainEntry) *TimelineService {
	automation := &AutomationService{}
	automation.SetProjectLister(&stubProjectLister{projects: projects})
	lister := &projectScopedLister{byType: map[string][]types.BrainEntry{
		"automation":     automations,
		"automation_run": runs,
		"task":           {},
		"reminder":       {},
	}}
	return NewTimelineService(lister, &timelineEventReaderFake{},
		WithTimelineClock(func() time.Time { return from }),
		WithTimelineTargets(automation))
}

// nextSlotInstants is the reference answer: the slots pkg/schedule's NextSlot
// yields for one target, from start (inclusive) to end (exclusive).
func nextSlotInstants(t *testing.T, sched *schedule.Schedule, automationID, project string, start, end time.Time) []time.Time {
	t.Helper()
	offset := sched.Offset(automationID, project)
	var out []time.Time
	cursor := start.Add(-time.Nanosecond)
	for {
		slot, ok, err := sched.NextSlot(context.Background(), cursor, offset)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || !slot.At.Before(end) {
			return out
		}
		out = append(out, slot.At)
		cursor = slot.At
	}
}

func projectionInstants(items []types.TimelineItem, automationID, project string) []time.Time {
	var out []time.Time
	for _, item := range items {
		if item.SourceKind == "automation" && item.SourceID == automationID && item.ProjectID == project {
			out = append(out, item.Timestamp)
		}
	}
	return out
}

func sameInstants(a, b []time.Time) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}

func compiledScheduleFor(t *testing.T, entry types.BrainEntry) *schedule.Schedule {
	t.Helper()
	spec, err := automationScheduleSpec(entry)
	if err != nil {
		t.Fatal(err)
	}
	sched, err := schedule.Compile(spec)
	if err != nil {
		t.Fatal(err)
	}
	return sched
}

// A global cron automation with filter.project "*" fires once per project,
// each at its own stagger offset, and each projection is exactly the slot
// NextSlot returns for that project.
func TestTimelineStaggeredGlobalAutomationProjectsEachTargetAtItsOwnOffset(t *testing.T) {
	from := timelineTime(t, "2026-10-03T00:00:00Z")
	to := timelineTime(t, "2026-10-06T00:00:00Z")
	dream := types.BrainEntry{
		ID: "dream", Type: "automation", Status: "active", Title: "dream",
		Trigger: &types.TriggerConfig{
			Type: "cron", Schedule: "0 3 * * *", Timezone: "UTC", Stagger: "2h",
			Filter: map[string]string{"project": "*"},
		},
	}
	projects := []string{"alpha", "beta", "gamma"}
	service := timelineServiceWithTargets(from, projects, []types.BrainEntry{dream}, nil)

	result, err := service.Timeline(context.Background(), from, to, "")
	if err != nil {
		t.Fatal(err)
	}

	sched := compiledScheduleFor(t, dream)
	first := make(map[time.Time]bool)
	for _, project := range projects {
		got := projectionInstants(result.Items, dream.ID, project)
		want := nextSlotInstants(t, sched, dream.ID, project, from, to)
		if len(want) != 3 {
			t.Fatalf("reference for %s has %d slots, want 3", project, len(want))
		}
		if !sameInstants(got, want) {
			t.Fatalf("project %s projected %v, want NextSlot %v", project, got, want)
		}
		first[got[0]] = true
	}
	if len(first) < 2 {
		t.Fatalf("every project shares one first run %v; stagger did not apply", first)
	}
}

// every 4d at 03:00 in America/New_York keeps its local time of day across
// the 1 November change to standard time.
func TestTimelineEveryAtProjectsLocalTimeOfDayInNewYork(t *testing.T) {
	from := timelineTime(t, "2026-10-08T00:00:00Z")
	to := timelineTime(t, "2026-11-03T00:00:00Z")
	entry := types.BrainEntry{
		ID: "every-ny", Type: "automation", Status: "active", ProjectID: "p",
		StartsAt: "2026-10-01T00:00:00-04:00",
		Trigger:  &types.TriggerConfig{Type: "cron", Every: "4d", At: "03:00", Timezone: "America/New_York"},
	}

	result := BuildTimeline([]types.BrainEntry{entry}, nil, TimelineProjectionOptions{From: from, To: to, Now: from})

	got := projectedAutomationTimes(result.Items, "every-ny")
	want := []string{
		"2026-10-09T07:00:00Z", // Oct 9 03:00 EDT
		"2026-10-13T07:00:00Z",
		"2026-10-17T07:00:00Z",
		"2026-10-21T07:00:00Z",
		"2026-10-25T07:00:00Z",
		"2026-10-29T07:00:00Z", // Oct 29 03:00 EDT
		"2026-11-02T08:00:00Z", // Nov 2 03:00 EST
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("projected = %v\nwant      %v", got, want)
	}
}

// The lifecycle window bounds every target: runs only from starts_at up to
// expires_at, and each target's runs are the NextSlot slots inside it.
func TestTimelineStaggeredAutomationRespectsLifecycleWindowPerProject(t *testing.T) {
	from := timelineTime(t, "2026-10-01T00:00:00Z")
	to := timelineTime(t, "2026-10-10T00:00:00Z")
	windowed := types.BrainEntry{
		ID: "window", Type: "automation", Status: "active", Title: "window",
		StartsAt: "2026-10-03T12:00:00Z", ExpiresAt: "2026-10-05T12:00:00Z",
		Trigger: &types.TriggerConfig{
			Type: "cron", Schedule: "0 9 * * *", Timezone: "UTC", Stagger: "1h",
			Filter: map[string]string{"project": "*"},
		},
	}
	projects := []string{"alpha", "beta"}
	service := timelineServiceWithTargets(from, projects, []types.BrainEntry{windowed}, nil)

	result, err := service.Timeline(context.Background(), from, to, "")
	if err != nil {
		t.Fatal(err)
	}

	start := timelineTime(t, windowed.StartsAt)
	end := timelineTime(t, windowed.ExpiresAt)
	sched := compiledScheduleFor(t, windowed)
	for _, project := range projects {
		got := projectionInstants(result.Items, windowed.ID, project)
		want := nextSlotInstants(t, sched, windowed.ID, project, start, end)
		if len(want) != 2 {
			t.Fatalf("reference for %s has %d slots in the window, want 2 (Oct 4, Oct 5)", project, len(want))
		}
		if !sameInstants(got, want) {
			t.Fatalf("project %s projected %v, want %v", project, got, want)
		}
		for _, at := range got {
			if at.Before(start) || !at.Before(end) {
				t.Fatalf("project %s projected %v outside [%v, %v)", project, at, start, end)
			}
		}
	}
}

// max_runs is counted per target project: a project that already used a run
// gets one fewer projected run than a project that has none.
func TestTimelineMaxRunsCapsEachTargetProjectSeparately(t *testing.T) {
	from := timelineTime(t, "2026-10-03T00:00:00Z")
	to := timelineTime(t, "2026-10-06T00:00:00Z")
	maxRuns := 2
	capped := types.BrainEntry{
		ID: "capped", Type: "automation", Status: "active", Title: "capped",
		MaxRuns: &maxRuns,
		Trigger: &types.TriggerConfig{
			Type: "cron", Schedule: "0 9 * * *", Timezone: "UTC", Stagger: "1h",
			Filter: map[string]string{"project": "*"},
		},
	}
	runs := queuedAuditsFor("capped", "alpha", 1)
	service := timelineServiceWithTargets(from, []string{"alpha", "beta"}, []types.BrainEntry{capped}, runs)

	result, err := service.Timeline(context.Background(), from, to, "")
	if err != nil {
		t.Fatal(err)
	}

	if got := len(projectionInstants(result.Items, capped.ID, "alpha")); got != 1 {
		t.Fatalf("alpha projected %d runs, want 1 (max_runs 2, one used)", got)
	}
	if got := len(projectionInstants(result.Items, capped.ID, "beta")); got != 2 {
		t.Fatalf("beta projected %d runs, want 2 (max_runs 2, none used)", got)
	}
}

// A timeline scoped to one project shows only that project's runs of a
// wildcard automation.
func TestTimelineProjectScopedTimelineShowsOnlyThatProjectsRuns(t *testing.T) {
	from := timelineTime(t, "2026-10-03T00:00:00Z")
	to := timelineTime(t, "2026-10-06T00:00:00Z")
	dream := types.BrainEntry{
		ID: "dream", Type: "automation", Status: "active", Title: "dream",
		Trigger: &types.TriggerConfig{
			Type: "cron", Schedule: "0 3 * * *", Timezone: "UTC", Stagger: "2h",
			Filter: map[string]string{"project": "*"},
		},
	}
	service := timelineServiceWithTargets(from, []string{"alpha", "beta", "gamma"}, []types.BrainEntry{dream}, nil)

	result, err := service.Timeline(context.Background(), from, to, "beta")
	if err != nil {
		t.Fatal(err)
	}

	if got := len(projectionInstants(result.Items, dream.ID, "beta")); got != 3 {
		t.Fatalf("beta projected %d runs, want 3", got)
	}
	for _, project := range []string{"alpha", "gamma"} {
		if got := projectionInstants(result.Items, dream.ID, project); len(got) != 0 {
			t.Fatalf("project %s leaked into beta's timeline: %v", project, got)
		}
	}
}

// Without a wired resolver a project-selecting automation is reported, not
// projected onto a guessed project.
func TestTimelineWarnsWhenSelectedProjectsCannotBeResolved(t *testing.T) {
	from := timelineTime(t, "2026-10-03T00:00:00Z")
	to := timelineTime(t, "2026-10-06T00:00:00Z")
	dream := types.BrainEntry{
		ID: "dream", Type: "automation", Status: "active", Title: "dream",
		Trigger: &types.TriggerConfig{
			Type: "cron", Schedule: "0 3 * * *", Timezone: "UTC",
			Filter: map[string]string{"project": "*"},
		},
	}
	lister := &projectScopedLister{byType: map[string][]types.BrainEntry{"automation": {dream}}}
	service := NewTimelineService(lister, &timelineEventReaderFake{}, WithTimelineClock(func() time.Time { return from }))

	result, err := service.Timeline(context.Background(), from, to, "")
	if err != nil {
		t.Fatal(err)
	}

	if got := projectionInstants(result.Items, dream.ID, ""); len(got) != 0 {
		t.Fatalf("unresolved automation projected %v, want none", got)
	}
	for _, item := range result.Items {
		if item.SourceID == dream.ID {
			t.Fatalf("unresolved automation produced item %#v", item)
		}
	}
	found := false
	for _, warning := range result.Warnings {
		if warning.SourceID == dream.ID && strings.Contains(warning.Message, "no project lister is wired") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %#v, want a no-project-lister warning for dream", result.Warnings)
	}
}

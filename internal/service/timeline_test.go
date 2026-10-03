package service

import (
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

func timelineTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestBuildTimelineProjectsTaskCronInItsTimezone(t *testing.T) {
	from := timelineTime(t, "2026-10-03T00:00:00Z")
	to := timelineTime(t, "2026-10-05T00:00:00Z")
	entries := []types.BrainEntry{{
		ID: "task-1", Path: "projects/demo/task/task-1.md", Title: "Morning report",
		Type: "task", Status: "active", ProjectID: "demo",
		Schedule: "0 9 * * *", Timezone: "America/Denver",
	}}

	result := BuildTimeline(entries, nil, TimelineProjectionOptions{From: from, To: to, Now: from})

	if len(result.Items) != 2 {
		t.Fatalf("got %d items, want 2: %#v", len(result.Items), result.Items)
	}
	if got := result.Items[0].Timestamp.Format(time.RFC3339); got != "2026-10-03T15:00:00Z" {
		t.Fatalf("first occurrence = %s, want 2026-10-03T15:00:00Z", got)
	}
	if result.Items[0].TemporalState != types.TimelineStateProjected || result.Items[0].SourceKind != "task" {
		t.Fatalf("unexpected projection metadata: %#v", result.Items[0])
	}
}

func TestBuildTimelineRespectsTaskEligibilityAndRemainingRuns(t *testing.T) {
	from := timelineTime(t, "2026-10-03T00:00:00Z")
	to := timelineTime(t, "2026-10-08T00:00:00Z")
	maxRuns := 3
	disabled := false
	entries := []types.BrainEntry{
		{ID: "bounded", Type: "task", Status: "completed", Schedule: "0 12 * * *", MaxRuns: &maxRuns, Runs: []types.CronRun{{Status: "completed"}, {Status: "failed"}}},
		{ID: "disabled", Type: "task", Status: "active", Schedule: "0 12 * * *", ScheduleEnabled: &disabled},
		{ID: "pending", Type: "task", Status: "pending", Schedule: "0 12 * * *"},
	}

	result := BuildTimeline(entries, nil, TimelineProjectionOptions{From: from, To: to, Now: from})

	if len(result.Items) != 1 || result.Items[0].SourceID != "bounded" {
		t.Fatalf("items = %#v, want one remaining run for bounded task", result.Items)
	}
}

func TestBuildTimelineProjectsAndDeduplicatesFeatureMilestones(t *testing.T) {
	from := timelineTime(t, "2026-10-03T00:00:00Z")
	to := timelineTime(t, "2026-10-06T00:00:00Z")
	entries := []types.BrainEntry{
		{ID: "a", Type: "task", Status: "active", ProjectID: "demo", FeatureID: "launch", FeatureStartsAt: "2026-10-04T08:00:00Z", FeatureExpiresAt: "2026-10-05T18:00:00Z"},
		{ID: "b", Type: "task", Status: "active", ProjectID: "demo", FeatureID: "launch", FeatureStartsAt: "2026-10-04T08:00:00Z", FeatureExpiresAt: "2026-10-05T18:00:00Z"},
	}

	result := BuildTimeline(entries, nil, TimelineProjectionOptions{From: from, To: to, Now: from})

	if len(result.Items) != 2 {
		t.Fatalf("got %d items, want deduplicated start and expiry", len(result.Items))
	}
	if result.Items[0].TemporalKind != types.TimelineKindStart || result.Items[1].TemporalKind != types.TimelineKindExpiry {
		t.Fatalf("unexpected milestone kinds: %#v", result.Items)
	}
}

func TestBuildTimelineExpandsRecurringReminderUntilBoundary(t *testing.T) {
	from := timelineTime(t, "2026-10-03T00:00:00Z")
	to := timelineTime(t, "2026-10-08T00:00:00Z")
	entries := []types.BrainEntry{{
		ID: "rem-entry", Type: "reminder", Status: "active", ProjectID: "demo", Title: "Hydrate",
		Reminder: &types.ReminderConfig{ID: "hydrate", RemindAt: "2026-10-03T09:00:00-06:00", Timezone: "America/Denver", Repeat: types.ReminderRepeatDaily, RepeatUntil: "2026-10-05T15:00:00Z"},
	}}

	result := BuildTimeline(entries, nil, TimelineProjectionOptions{From: from, To: to, Now: from})

	if len(result.Items) != 3 {
		t.Fatalf("got %d reminders, want 3: %#v", len(result.Items), result.Items)
	}
	if result.Items[2].Timestamp.Format(time.RFC3339) != "2026-10-05T15:00:00Z" {
		t.Fatalf("last reminder = %s", result.Items[2].Timestamp.Format(time.RFC3339))
	}
}

func TestBuildTimelineAggregatesDenseCronByLocalDay(t *testing.T) {
	from := timelineTime(t, "2026-10-03T00:00:00Z")
	to := timelineTime(t, "2026-10-04T00:00:00Z")
	entries := []types.BrainEntry{{ID: "frequent", Type: "automation", Status: "active", Trigger: &types.TriggerConfig{Type: "cron", Schedule: "*/15 * * * *", Timezone: "UTC"}}}

	result := BuildTimeline(entries, nil, TimelineProjectionOptions{From: from, To: to, Now: from, DenseDailyThreshold: 24})

	if len(result.Items) != 1 {
		t.Fatalf("got %d items, want one daily aggregate", len(result.Items))
	}
	if result.Items[0].OccurrenceCount != 96 || result.Items[0].WindowStart == nil || result.Items[0].WindowEnd == nil {
		t.Fatalf("unexpected aggregate: %#v", result.Items[0])
	}
}

func TestBuildTimelineAggregatesEveryDayOfThirtyDayMinuteSchedule(t *testing.T) {
	from := timelineTime(t, "2026-10-03T00:00:00Z")
	to := from.Add(30 * 24 * time.Hour)
	entries := []types.BrainEntry{{ID: "frequent", Type: "automation", Status: "active", Trigger: &types.TriggerConfig{Type: "cron", Schedule: "* * * * *", Timezone: "UTC"}}}

	result := BuildTimeline(entries, nil, TimelineProjectionOptions{From: from, To: to, Now: from})

	if result.Truncated {
		t.Fatal("30-day minute schedule should aggregate without truncation")
	}
	if len(result.Items) != 30 {
		t.Fatalf("got %d daily aggregates, want 30", len(result.Items))
	}
	for _, item := range result.Items {
		if item.OccurrenceCount != 1440 {
			t.Fatalf("daily occurrence count = %d, want 1440", item.OccurrenceCount)
		}
	}
}

func TestBuildTimelineKeepsActualEventsAndWarnsOnMalformedSchedules(t *testing.T) {
	from := timelineTime(t, "2026-10-03T00:00:00Z")
	to := timelineTime(t, "2026-10-04T00:00:00Z")
	entries := []types.BrainEntry{{ID: "bad", Type: "task", Status: "active", Schedule: "not cron"}}
	actual := []types.Event{{ID: "evt-1", Type: "task.completed", Source: "runner", Timestamp: from.Add(time.Hour), TaskID: "done"}}

	result := BuildTimeline(entries, actual, TimelineProjectionOptions{From: from, To: to, Now: from})

	if len(result.Items) != 1 || result.Items[0].TemporalState != types.TimelineStateActual {
		t.Fatalf("actual event was not preserved: %#v", result.Items)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].SourceID != "bad" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestBuildTimelineReportsWhenExpansionBudgetTruncatesARecurringSource(t *testing.T) {
	from := timelineTime(t, "2026-10-03T00:00:00Z")
	to := from.Add(24 * time.Hour)
	entries := []types.BrainEntry{{ID: "hourly", Type: "automation", Status: "active", Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 * * * *"}}}

	result := BuildTimeline(entries, nil, TimelineProjectionOptions{From: from, To: to, Now: from, ExpansionBudget: 2})

	if !result.Truncated {
		t.Fatal("expected truncated=true when more occurrences remain")
	}
}

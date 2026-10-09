package service

import (
	"context"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

func projectedAutomationTimes(items []types.TimelineItem, id string) []string {
	var out []string
	for _, item := range items {
		if item.SourceKind == "automation" && item.SourceID == id {
			out = append(out, item.Timestamp.UTC().Format(time.RFC3339))
		}
	}
	return out
}

// A projected automation run only appears inside its starts_at..expires_at
// window. Daily 09:00 with a window of Oct 3 12:00 to Oct 5 12:00 leaves Oct 4
// and Oct 5 only.
func TestTimelineAutomationProjectionRespectsStartsAtAndExpiresAt(t *testing.T) {
	from := timelineTime(t, "2026-10-01T00:00:00Z")
	to := timelineTime(t, "2026-10-10T00:00:00Z")
	entry := types.BrainEntry{
		ID: "auto1", Type: "automation", Status: "active", Title: "windowed",
		Trigger:   &types.TriggerConfig{Type: "cron", Schedule: "0 9 * * *", Timezone: "UTC"},
		Action:    &types.AutomationAction{Type: "prompt", DirectPrompt: "x"},
		StartsAt:  "2026-10-03T12:00:00Z",
		ExpiresAt: "2026-10-05T12:00:00Z",
	}

	result := BuildTimeline([]types.BrainEntry{entry}, nil, TimelineProjectionOptions{From: from, To: to, Now: from})

	got := projectedAutomationTimes(result.Items, "auto1")
	want := []string{"2026-10-04T09:00:00Z", "2026-10-05T09:00:00Z"}
	if len(got) != len(want) {
		t.Fatalf("projected = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("projected = %v, want %v", got, want)
		}
	}
}

// Projected runs are capped by the remaining max_runs. An automation whose
// audits already exhaust max_runs projects nothing; one with runs left
// projects only the remainder.
func TestTimelineOmitsRunsOfExhaustedMaxRuns(t *testing.T) {
	from := timelineTime(t, "2026-10-01T00:00:00Z")
	to := timelineTime(t, "2026-10-10T00:00:00Z")
	build := func(maxRuns int, audits int) []types.TimelineItem {
		max := maxRuns
		lister := &timelineEntryListerFake{byType: map[string][]types.BrainEntry{
			"automation": {{
				ID: "auto2", Type: "automation", Status: "active", Title: "capped", ProjectID: "p",
				Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 9 * * *", Timezone: "UTC"},
				Action:  &types.AutomationAction{Type: "prompt", DirectPrompt: "x"},
				MaxRuns: &max,
			}},
			"task": {}, "reminder": {},
			"automation_run": queuedAuditsFor("auto2", "p", audits),
		}}
		service := NewTimelineService(lister, &timelineEventReaderFake{}, WithTimelineClock(func() time.Time { return from }))
		result, err := service.Timeline(context.Background(), from, to, "p")
		if err != nil {
			t.Fatal(err)
		}
		return result.Items
	}

	if got := projectedAutomationTimes(build(2, 2), "auto2"); len(got) != 0 {
		t.Fatalf("exhausted max_runs=2 projected %v, want none", got)
	}
	if got := projectedAutomationTimes(build(3, 2), "auto2"); len(got) != 1 {
		t.Fatalf("max_runs=3 with 2 used projected %v, want exactly 1", got)
	}
}

func queuedAuditsFor(automationID, project string, n int) []types.BrainEntry {
	out := make([]types.BrainEntry, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, types.BrainEntry{
			ID: "audit" + string(rune('a'+i)), Type: "automation_run", Status: "queued",
			ProjectID: project, Tags: []string{"automation:" + automationID},
		})
	}
	return out
}

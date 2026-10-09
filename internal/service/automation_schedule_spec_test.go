package service

import (
	"context"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/schedule"
)

func scheduleSpecEntry(trigger types.TriggerConfig) types.BrainEntry {
	return types.BrainEntry{
		ID: "auto-spec", Type: "automation", Status: "active",
		Created: "2026-10-01T00:00:00Z", Trigger: &trigger,
	}
}

func mustScheduleSpecTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestScheduleSpecCronCarriesTriggerFieldsWithoutAnchor(t *testing.T) {
	entry := scheduleSpecEntry(types.TriggerConfig{
		Type: "cron", Schedule: "0 3 * * *", Timezone: "America/New_York", Stagger: "2h",
	})

	spec, err := automationScheduleSpec(entry)
	if err != nil {
		t.Fatalf("automationScheduleSpec: %v", err)
	}
	if spec.Cron != "0 3 * * *" || spec.Every != "" || spec.At != "" {
		t.Fatalf("cron fields = {Cron:%q Every:%q At:%q}", spec.Cron, spec.Every, spec.At)
	}
	if spec.Timezone != "America/New_York" {
		t.Fatalf("timezone = %q", spec.Timezone)
	}
	if spec.Stagger != 2*time.Hour {
		t.Fatalf("stagger = %v, want 2h", spec.Stagger)
	}
	if !spec.Anchor.IsZero() {
		t.Fatalf("cron spec carries anchor %v; the anchor is only for every", spec.Anchor)
	}
}

func TestScheduleSpecEveryAnchorsAtStartsAtBeforeCreated(t *testing.T) {
	entry := scheduleSpecEntry(types.TriggerConfig{Type: "cron", Every: "4d", At: "03:00", Timezone: "America/New_York"})
	entry.StartsAt = "2026-10-03T03:00:00-04:00"

	spec, err := automationScheduleSpec(entry)
	if err != nil {
		t.Fatalf("starts_at anchor: %v", err)
	}
	if want := mustScheduleSpecTime(t, "2026-10-03T03:00:00-04:00"); !spec.Anchor.Equal(want) {
		t.Fatalf("anchor = %v, want starts_at %v", spec.Anchor, want)
	}
	if spec.Every != "4d" || spec.At != "03:00" {
		t.Fatalf("every/at = %q/%q", spec.Every, spec.At)
	}

	entry.StartsAt = ""
	spec, err = automationScheduleSpec(entry)
	if err != nil {
		t.Fatalf("created anchor: %v", err)
	}
	if want := mustScheduleSpecTime(t, "2026-10-01T00:00:00Z"); !spec.Anchor.Equal(want) {
		t.Fatalf("anchor = %v, want created %v", spec.Anchor, want)
	}
}

func TestScheduleSpecEveryDSTKeepsLocalTimeOfDayThroughNextSlot(t *testing.T) {
	entry := scheduleSpecEntry(types.TriggerConfig{Type: "cron", Every: "4d", At: "03:00", Timezone: "America/New_York"})
	entry.StartsAt = "2026-10-01T00:00:00-04:00"

	spec, err := automationScheduleSpec(entry)
	if err != nil {
		t.Fatal(err)
	}
	sched, err := schedule.Compile(spec)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	// Oct 1 + 4k days at 03:00 New York. EDT until Nov 1, EST after.
	after := mustScheduleSpecTime(t, "2026-10-08T00:00:00Z")
	want := []string{
		"2026-10-09T07:00:00Z", // Oct 9 03:00 EDT
		"2026-10-13T07:00:00Z", // Oct 13 03:00 EDT
		"2026-10-17T07:00:00Z", // Oct 17 03:00 EDT
		"2026-10-21T07:00:00Z", // Oct 21 03:00 EDT
		"2026-10-25T07:00:00Z", // Oct 25 03:00 EDT
		"2026-10-29T07:00:00Z", // Oct 29 03:00 EDT
		"2026-11-02T08:00:00Z", // Nov 2 03:00 EST (local time kept across DST)
	}
	ctx := context.Background()
	for i, wantAt := range want {
		slot, ok, err := sched.NextSlot(ctx, after, 0)
		if err != nil || !ok {
			t.Fatalf("slot %d: ok=%v err=%v", i, ok, err)
		}
		if got := slot.At.UTC().Format(time.RFC3339); got != wantAt {
			t.Fatalf("slot %d = %s, want %s", i, got, wantAt)
		}
		after = slot.At
	}
}

func TestScheduleSpecStaggerAndTimezoneAreParsed(t *testing.T) {
	entry := scheduleSpecEntry(types.TriggerConfig{Type: "cron", Schedule: "0 3 * * *", Stagger: "90m"})
	spec, err := automationScheduleSpec(entry)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Stagger != 90*time.Minute {
		t.Fatalf("stagger = %v, want 90m", spec.Stagger)
	}
	if spec.Timezone != "" {
		t.Fatalf("empty timezone must stay empty (UTC by Compile), got %q", spec.Timezone)
	}
}

func TestScheduleSpecRejectsMalformedInputs(t *testing.T) {
	cases := map[string]types.BrainEntry{
		"no trigger": {ID: "auto-spec", Type: "automation", Status: "active"},
		"bad stagger": scheduleSpecEntry(types.TriggerConfig{
			Type: "cron", Schedule: "0 3 * * *", Stagger: "soon",
		}),
		"every without any anchor": func() types.BrainEntry {
			entry := scheduleSpecEntry(types.TriggerConfig{Type: "cron", Every: "4d", At: "03:00"})
			entry.Created = ""
			return entry
		}(),
		"every with malformed created": func() types.BrainEntry {
			entry := scheduleSpecEntry(types.TriggerConfig{Type: "cron", Every: "4d", At: "03:00"})
			entry.Created = "yesterday"
			return entry
		}(),
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := automationScheduleSpec(entry); err == nil {
				t.Fatal("automationScheduleSpec accepted malformed input")
			}
		})
	}
}

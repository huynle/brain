package service

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// These pin the translation from an automation's trigger into a pkg/schedule
// Spec: the fields pass through unchanged, the stagger is parsed, and an
// interval anchors at starts_at, falling back to the entry's created instant.

func TestAutomationScheduleSpec_CronPassesTheTriggerThrough(t *testing.T) {
	entry := types.BrainEntry{
		ID:      "auto-spec-cron",
		Created: "2026-10-01T09:00:00Z",
		Trigger: &types.TriggerConfig{
			Type:     "cron",
			Schedule: "0 3 * * *",
			Timezone: "America/New_York",
			Stagger:  "2h",
			CatchUp:  "6h",
		},
	}
	spec, err := automationScheduleSpec(entry)
	if err != nil {
		t.Fatalf("automationScheduleSpec: %v", err)
	}
	if spec.Cron != "0 3 * * *" || spec.Every != "" || spec.At != "" {
		t.Errorf("cron fields = (%q, %q, %q), want the trigger's schedule only", spec.Cron, spec.Every, spec.At)
	}
	if spec.Timezone != "America/New_York" {
		t.Errorf("Timezone = %q, want America/New_York", spec.Timezone)
	}
	if spec.Stagger != 2*time.Hour {
		t.Errorf("Stagger = %v, want 2h", spec.Stagger)
	}
	if spec.DayFilters != nil {
		t.Errorf("DayFilters = %v, want nil until calendar filters exist", spec.DayFilters)
	}
}

func TestAutomationScheduleSpec_EveryAnchorsAtStartsAtThenCreated(t *testing.T) {
	base := types.BrainEntry{
		ID:      "auto-spec-every",
		Created: "2026-09-01T00:00:00Z",
		Trigger: &types.TriggerConfig{Type: "cron", Every: "4d", At: "03:00", Timezone: "America/New_York"},
	}

	withStart := base
	withStart.StartsAt = "2026-10-01T00:00:00-04:00"
	spec, err := automationScheduleSpec(withStart)
	if err != nil {
		t.Fatalf("starts_at case: %v", err)
	}
	if want := mustParseRFC3339(t, "2026-10-01T00:00:00-04:00"); !spec.Anchor.Equal(want) {
		t.Errorf("anchor with starts_at = %v, want %v", spec.Anchor, want)
	}

	spec, err = automationScheduleSpec(base)
	if err != nil {
		t.Fatalf("created case: %v", err)
	}
	if want := mustParseRFC3339(t, "2026-09-01T00:00:00Z"); !spec.Anchor.Equal(want) {
		t.Errorf("anchor without starts_at = %v, want the created instant %v", spec.Anchor, want)
	}
}

func TestAutomationScheduleSpec_RejectsAnUnparseableStagger(t *testing.T) {
	entry := types.BrainEntry{
		ID:      "auto-spec-stagger",
		Created: "2026-10-01T00:00:00Z",
		Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 3 * * *", Stagger: "soon"},
	}
	if _, err := automationScheduleSpec(entry); err == nil {
		t.Fatal("automationScheduleSpec accepted stagger \"soon\", want an error")
	}
}

// An entry that cannot compile is skipped, and the warning is written once
// per modification, not on every tick.
func TestAutomationSchedule_InvalidEntryWarnsOncePerModification(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	svc := NewAutomationService(nil)
	bad := types.BrainEntry{
		ID:       "auto-bad-schedule",
		Modified: "2026-10-01T00:00:00Z",
		Created:  "2026-09-30T00:00:00Z",
		Trigger:  &types.TriggerConfig{Type: "cron", Schedule: "not a cron"},
	}

	for i := 0; i < 3; i++ {
		if _, ok := svc.compiledScheduleFor(bad); ok {
			t.Fatalf("tick %d: compiledScheduleFor accepted an invalid cron", i)
		}
	}
	if n := strings.Count(logs.String(), "auto-bad-schedule"); n != 1 {
		t.Fatalf("warnings for one modification = %d, want 1; log:\n%s", n, logs.String())
	}

	edited := bad
	edited.Modified = "2026-10-02T00:00:00Z"
	if _, ok := svc.compiledScheduleFor(edited); ok {
		t.Fatal("edited entry compiled, want still invalid")
	}
	if n := strings.Count(logs.String(), "auto-bad-schedule"); n != 2 {
		t.Fatalf("warnings after an edit = %d, want 2 (one per modification)", n)
	}
}

func mustParseRFC3339(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed
}

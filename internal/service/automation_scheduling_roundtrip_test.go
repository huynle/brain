package service

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/frontmatter"
)

// Automation scheduling task 1.2 adds durable fields that nothing evaluates
// yet: extends / scheduled_for / binding on the entry, the scheduling keys on
// the trigger and prompt_append on the action. These tests follow
// TestOriginFields_RoundTrip through every service hop — create -> file ->
// reindex -> read -> update -> fallback reconstruction — so a converter that
// forgets a field fails here instead of silently writing it nowhere.

// schedulingValues is one complete set of the new fields plus the lifecycle
// fields an automation already carries.
type schedulingValues struct {
	extends, scheduledFor, binding string
	trigger                        types.TriggerConfig
	promptAppend                   string
	startsAt, expiresAt, timezone  string
	maxRuns                        int
}

func schedulingValuesV1() schedulingValues {
	return schedulingValues{
		// extends is set by the test to a saved cron parent (a binding must
		// name a real parent, validated at save).
		extends:      "",
		scheduledFor: "2026-10-09T03:00:00Z",
		binding:      "bind0001",
		// Clock trigger: every and at are the interval form, which is exclusive
		// with a cron schedule (validated at save).
		trigger: types.TriggerConfig{
			Type:     types.TriggerTypeCron,
			Timezone: "America/New_York",
			Every:    "4d",
			At:       "03:00",
			Stagger:  "2h",
			CatchUp:  "none",
			Calendar: "xnys",
			SkipIfEvent: &types.CalendarEventFilter{
				Calendar: "work", Title: "re:(?i)^OOO", Description: "*",
				Location: "in:home,remote", AllDay: "true",
			},
			OnlyIfEvent: &types.CalendarEventFilter{Calendar: "personal", Title: "has:focus"},
			Match:       map[string]string{"title": "re:(?i)^1:1 (?P<person>.+)$", "all_day": "false"},
			Offset:      "-15m",
			Filter:      map[string]string{"tags": "has:supernote"}, // a binding cannot set filter.project
		},
		promptAppend: "Weight ingestion decisions more heavily.",
		startsAt:     "2026-10-10T00:00:00Z",
		expiresAt:    "2027-06-30T00:00:00-04:00",
		timezone:     "America/New_York",
		maxRuns:      12,
	}
}

func schedulingValuesV2() schedulingValues {
	return schedulingValues{
		// A calendar trigger cannot be a binding (its parent could not be
		// calendar-typed), so v2 is a standalone automation.
		extends:      "",
		scheduledFor: "2026-10-11T07:30:00-04:00",
		binding:      "bind0002",
		trigger: types.TriggerConfig{
			Type:        types.TriggerTypeCalendar,
			Calendar:    "work",
			Match:       map[string]string{"title": "re:standup", "location": "*"},
			At:          "end",
			Offset:      "10m",
			CatchUp:     "1h",
			Every:       "90m",
			Stagger:     "0s",
			SkipIfEvent: &types.CalendarEventFilter{AllDay: "true"},
			OnlyIfEvent: &types.CalendarEventFilter{Location: "re:^HQ"},
		},
		promptAppend: "Summarize the meeting.",
		startsAt:     "2026-11-01T00:00:00Z",
		expiresAt:    "2027-01-01T00:00:00Z",
		timezone:     "Europe/Berlin",
		maxRuns:      3,
	}
}

func (v schedulingValues) createRequest() types.CreateEntryRequest {
	trigger := v.trigger
	maxRuns := v.maxRuns
	return types.CreateEntryRequest{
		Type:         "automation",
		Title:        "Scheduling round trip",
		Content:      "body",
		Project:      "sched",
		Extends:      v.extends,
		ScheduledFor: v.scheduledFor,
		Binding:      v.binding,
		Trigger:      &trigger,
		Action:       &types.AutomationAction{Agent: "explore", PromptAppend: v.promptAppend},
		StartsAt:     v.startsAt,
		ExpiresAt:    v.expiresAt,
		Timezone:     v.timezone,
		MaxRuns:      &maxRuns,
	}
}

func (v schedulingValues) updateRequest() types.UpdateEntryRequest {
	trigger := v.trigger
	extends, scheduledFor, binding := v.extends, v.scheduledFor, v.binding
	startsAt, expiresAt, timezone, maxRuns := v.startsAt, v.expiresAt, v.timezone, v.maxRuns
	return types.UpdateEntryRequest{
		Extends:      &extends,
		ScheduledFor: &scheduledFor,
		Binding:      &binding,
		Trigger:      &trigger,
		Action:       &types.AutomationAction{Agent: "explore", PromptAppend: v.promptAppend},
		StartsAt:     &startsAt,
		ExpiresAt:    &expiresAt,
		Timezone:     &timezone,
		MaxRuns:      &maxRuns,
	}
}

// assertSchedulingEntry checks the read side (notes.metadata ->
// parseMetadataIntoEntry -> BrainEntry).
func assertSchedulingEntry(t *testing.T, stage string, e *types.BrainEntry, want schedulingValues) {
	t.Helper()
	if e.Extends != want.extends || e.ScheduledFor != want.scheduledFor || e.Binding != want.binding {
		t.Errorf("%s: entry refs = (%q, %q, %q), want (%q, %q, %q)", stage,
			e.Extends, e.ScheduledFor, e.Binding, want.extends, want.scheduledFor, want.binding)
	}
	if e.Trigger == nil || !reflect.DeepEqual(*e.Trigger, want.trigger) {
		t.Errorf("%s: entry trigger =\n  %+v\nwant\n  %+v", stage, e.Trigger, want.trigger)
	}
	if e.Action == nil || e.Action.PromptAppend != want.promptAppend {
		t.Errorf("%s: entry action = %+v, want prompt_append %q", stage, e.Action, want.promptAppend)
	}
	if e.StartsAt != want.startsAt || e.ExpiresAt != want.expiresAt || e.Timezone != want.timezone ||
		e.MaxRuns == nil || *e.MaxRuns != want.maxRuns {
		t.Errorf("%s: entry lifecycle = (%q, %q, %q, %v), want (%q, %q, %q, %d)", stage,
			e.StartsAt, e.ExpiresAt, e.Timezone, e.MaxRuns, want.startsAt, want.expiresAt, want.timezone, want.maxRuns)
	}
}

// assertSchedulingFrontmatter checks the on-disk side (Parse of the file).
func assertSchedulingFrontmatter(t *testing.T, stage, content string, want schedulingValues) {
	t.Helper()
	doc, err := frontmatter.Parse(content)
	if err != nil {
		t.Fatalf("%s: parse: %v", stage, err)
	}
	fm := doc.Frontmatter
	if len(fm.Extra) != 0 {
		t.Errorf("%s: unknown keys landed in Extra: %v", stage, fm.Extra)
	}
	if fm.Extends != want.extends || fm.ScheduledFor != want.scheduledFor || fm.Binding != want.binding {
		t.Errorf("%s: file refs = (%q, %q, %q), want (%q, %q, %q)", stage,
			fm.Extends, fm.ScheduledFor, fm.Binding, want.extends, want.scheduledFor, want.binding)
	}
	if got := fmTriggerConfigFromFile(fm.Trigger); !reflect.DeepEqual(got, want.trigger) {
		t.Errorf("%s: file trigger =\n  %+v\nwant\n  %+v", stage, got, want.trigger)
	}
	if fm.Action == nil || fm.Action.PromptAppend != want.promptAppend {
		t.Errorf("%s: file action = %+v, want prompt_append %q", stage, fm.Action, want.promptAppend)
	}
	if fm.StartsAt != want.startsAt || fm.ExpiresAt != want.expiresAt || fm.Timezone != want.timezone ||
		(want.maxRuns != 0 && (fm.MaxRuns == nil || *fm.MaxRuns != want.maxRuns)) {
		t.Errorf("%s: file lifecycle = (%q, %q, %q, %v), want (%q, %q, %q, %d)", stage,
			fm.StartsAt, fm.ExpiresAt, fm.Timezone, fm.MaxRuns, want.startsAt, want.expiresAt, want.timezone, want.maxRuns)
	}
}

// fmTriggerConfigFromFile maps the on-disk trigger to the domain struct field
// by field (independently of the production converters under test).
func fmTriggerConfigFromFile(t *frontmatter.TriggerConfig) types.TriggerConfig {
	if t == nil {
		return types.TriggerConfig{}
	}
	filter := func(f *frontmatter.CalendarEventFilter) *types.CalendarEventFilter {
		if f == nil {
			return nil
		}
		return &types.CalendarEventFilter{Calendar: f.Calendar, Title: f.Title, Description: f.Description, Location: f.Location, AllDay: f.AllDay}
	}
	return types.TriggerConfig{
		Type: t.Type, Event: t.Event, Events: t.Events, Schedule: t.Schedule, Timezone: t.Timezone,
		Every: t.Every, At: t.At, Stagger: t.Stagger, CatchUp: t.CatchUp, Calendar: t.Calendar,
		SkipIfEvent: filter(t.SkipIfEvent), OnlyIfEvent: filter(t.OnlyIfEvent), Match: t.Match, Offset: t.Offset,
		Filter: t.Filter, OncePer: t.OncePer, Webhook: t.Webhook,
		IgnoreAutomationEvents: t.IgnoreAutomationEvents, Cooldown: t.Cooldown, MaxConcurrent: t.MaxConcurrent,
	}
}

func readEntryFile(t *testing.T, brain *BrainServiceImpl, relPath string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(brain.config.BrainDir, relPath))
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	return string(data)
}

func TestAutomationSchedulingFields_ServiceRoundTrip(t *testing.T) {
	brain, _ := newTestBrainAndTaskService(t)
	ctx := context.Background()
	v1, v2 := schedulingValuesV1(), schedulingValuesV2()

	// v1 is a binding, so it needs a real cron-triggered parent.
	parent, err := brain.Save(ctx, types.CreateEntryRequest{
		Type: "automation", Title: "Scheduling parent", Content: "body", Project: "sched",
		Trigger: &types.TriggerConfig{Type: types.TriggerTypeCron, Schedule: "0 9 * * *"},
		Action:  &types.AutomationAction{Type: "prompt", DirectPrompt: "parent prompt"},
	})
	if err != nil {
		t.Fatalf("Save parent: %v", err)
	}
	v1.extends = parent.ID

	// Create -> file.
	saved, err := brain.Save(ctx, v1.createRequest())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	assertSchedulingFrontmatter(t, "create/file", readEntryFile(t, brain, saved.Path), v1)

	// Index -> read.
	entry, err := brain.Recall(ctx, saved.Path)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	assertSchedulingEntry(t, "create/read", entry, v1)

	// Full reindex from disk -> read.
	if _, err := brain.indexer.RebuildAll(); err != nil {
		t.Fatalf("RebuildAll: %v", err)
	}
	entry, err = brain.Recall(ctx, saved.Path)
	if err != nil {
		t.Fatalf("Recall after reindex: %v", err)
	}
	assertSchedulingEntry(t, "reindex/read", entry, v1)

	// Fallback reconstruction (metadata JSON -> Frontmatter -> Serialize).
	// reconstructFrontmatter has never carried max_runs (a pre-existing gap
	// outside this change), so that one lifecycle field is not asserted here.
	row, err := brain.storage.GetNoteByPath(ctx, saved.Path)
	if err != nil || row == nil {
		t.Fatalf("GetNoteByPath: %v (row %v)", err, row)
	}
	reconstructed, err := brain.reconstructFullContent(row)
	if err != nil {
		t.Fatalf("reconstructFullContent: %v", err)
	}
	noMaxRuns := v1
	noMaxRuns.maxRuns = 0
	assertSchedulingFrontmatter(t, "reconstruct", reconstructed, noMaxRuns)

	// Typed update replaces every value.
	updated, err := brain.Update(ctx, saved.Path, v2.updateRequest())
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	assertSchedulingEntry(t, "update/response", updated, v2)
	assertSchedulingFrontmatter(t, "update/file", readEntryFile(t, brain, saved.Path), v2)
	entry, err = brain.Recall(ctx, saved.Path)
	if err != nil {
		t.Fatalf("Recall after update: %v", err)
	}
	assertSchedulingEntry(t, "update/read", entry, v2)

	// An unrelated update keeps every value.
	title := "Renamed"
	if _, err := brain.Update(ctx, saved.Path, types.UpdateEntryRequest{Title: &title}); err != nil {
		t.Fatalf("Update title: %v", err)
	}
	assertSchedulingFrontmatter(t, "unrelated update/file", readEntryFile(t, brain, saved.Path), v2)
	entry, err = brain.Recall(ctx, saved.Path)
	if err != nil {
		t.Fatalf("Recall after title update: %v", err)
	}
	assertSchedulingEntry(t, "unrelated update/read", entry, v2)

	// Clearing a reference with an explicit empty value removes the key.
	empty := ""
	if _, err := brain.Update(ctx, saved.Path, types.UpdateEntryRequest{Binding: &empty}); err != nil {
		t.Fatalf("Update clear binding: %v", err)
	}
	entry, err = brain.Recall(ctx, saved.Path)
	if err != nil {
		t.Fatalf("Recall after clear: %v", err)
	}
	if entry.Binding != "" || entry.Extends != v2.extends {
		t.Errorf("after clearing binding: binding=%q extends=%q, want \"\" and %q", entry.Binding, entry.Extends, v2.extends)
	}
}

// TestAutomationSchedulingFields_AbsentStaysAbsent: an entry without the new
// fields must not grow keys on create or update.
func TestAutomationSchedulingFields_AbsentStaysAbsent(t *testing.T) {
	brain, _ := newTestBrainAndTaskService(t)
	ctx := context.Background()

	saved, err := brain.Save(ctx, types.CreateEntryRequest{
		Type: "automation", Title: "Legacy cron", Content: "body", Project: "sched",
		Trigger: &types.TriggerConfig{Type: types.TriggerTypeCron, Schedule: "0 3 * * *"},
		Action:  &types.AutomationAction{Type: "prompt", DirectPrompt: "dream"},
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	before := readEntryFile(t, brain, saved.Path)
	title := "Legacy cron"
	if _, err := brain.Update(ctx, saved.Path, types.UpdateEntryRequest{Title: &title}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	after := readEntryFile(t, brain, saved.Path)
	if before != after {
		t.Errorf("no-op update changed the file:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	entry, err := brain.Recall(ctx, saved.Path)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	want := types.TriggerConfig{Type: types.TriggerTypeCron, Schedule: "0 3 * * *"}
	if entry.Extends != "" || entry.ScheduledFor != "" || entry.Binding != "" ||
		entry.Trigger == nil || !reflect.DeepEqual(*entry.Trigger, want) || entry.Action.PromptAppend != "" {
		t.Errorf("legacy entry grew scheduling fields: %+v trigger=%+v action=%+v", entry, entry.Trigger, entry.Action)
	}
}

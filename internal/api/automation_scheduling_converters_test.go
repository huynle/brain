package api

import (
	"reflect"
	"testing"

	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/frontmatter"
)

// assertNoZeroFields fails for every exported field of the struct pointed to
// by v that is still its zero value. Converters are tested against fully
// populated inputs, so a zero output field means the converter dropped it.
func assertNoZeroFields(t *testing.T, v any) {
	t.Helper()
	rv := reflect.ValueOf(v).Elem()
	for i := 0; i < rv.NumField(); i++ {
		if rv.Field(i).IsZero() {
			t.Errorf("%s.%s was dropped by the converter (zero value)", rv.Type().Name(), rv.Type().Field(i).Name)
		}
	}
}

func boolPtrForTest(b bool) *bool { return &b }

func fullFMTrigger() *frontmatter.TriggerConfig {
	return &frontmatter.TriggerConfig{
		Type:                   "calendar",
		Event:                  "task.created",
		Events:                 []string{"task.created", "task.updated"},
		Schedule:               "0 9 * * *",
		Timezone:               "America/Denver",
		Every:                  "4d",
		At:                     "09:30",
		Stagger:                "2h",
		CatchUp:                "6h",
		Calendar:               "work",
		SkipIfEvent:            &frontmatter.CalendarEventFilter{Calendar: "holidays", Title: "re:(?i)holiday", Description: "*", Location: "home", AllDay: "true"},
		OnlyIfEvent:            &frontmatter.CalendarEventFilter{Calendar: "oncall", Title: "On call", Description: "has:pager", Location: "office", AllDay: "false"},
		Match:                  map[string]string{"title": "re:^standup", "all_day": "false"},
		Offset:                 "-15m",
		Filter:                 map[string]string{"project": "*"},
		OncePer:                "day",
		Webhook:                "hook-1",
		IgnoreAutomationEvents: boolPtrForTest(true),
		Cooldown:               "5m",
		MaxConcurrent:          2,
	}
}

func TestFmTriggerConfigToType_CarriesEveryField(t *testing.T) {
	src := fullFMTrigger()
	got := fmTriggerConfigToType(src)
	if got == nil {
		t.Fatal("expected non-nil trigger")
	}
	assertNoZeroFields(t, got)

	want := &types.TriggerConfig{
		Type: "calendar", Event: "task.created", Events: []string{"task.created", "task.updated"},
		Schedule: "0 9 * * *", Timezone: "America/Denver",
		Every: "4d", At: "09:30", Stagger: "2h", CatchUp: "6h", Calendar: "work",
		SkipIfEvent: &types.CalendarEventFilter{Calendar: "holidays", Title: "re:(?i)holiday", Description: "*", Location: "home", AllDay: "true"},
		OnlyIfEvent: &types.CalendarEventFilter{Calendar: "oncall", Title: "On call", Description: "has:pager", Location: "office", AllDay: "false"},
		Match:       map[string]string{"title": "re:^standup", "all_day": "false"},
		Offset:      "-15m",
		Filter:      map[string]string{"project": "*"}, OncePer: "day", Webhook: "hook-1",
		IgnoreAutomationEvents: boolPtrForTest(true), Cooldown: "5m", MaxConcurrent: 2,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("trigger mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestFmTriggerConfigToType_DeepCopiesMatchAndEventFilters(t *testing.T) {
	src := fullFMTrigger()
	got := fmTriggerConfigToType(src)
	if got == nil || got.SkipIfEvent == nil || got.OnlyIfEvent == nil {
		t.Fatalf("event filters dropped: %+v", got)
	}

	src.Match["title"] = "mutated"
	src.SkipIfEvent.Title = "mutated"
	src.OnlyIfEvent.Title = "mutated"

	if got.Match["title"] != "re:^standup" {
		t.Errorf("Match aliases the source map: got %q", got.Match["title"])
	}
	if got.SkipIfEvent.Title != "re:(?i)holiday" {
		t.Errorf("SkipIfEvent aliases the source filter: got %q", got.SkipIfEvent.Title)
	}
	if got.OnlyIfEvent.Title != "On call" {
		t.Errorf("OnlyIfEvent aliases the source filter: got %q", got.OnlyIfEvent.Title)
	}
}

func TestFmTriggerConfigToType_NilAndEmptySafe(t *testing.T) {
	if got := fmTriggerConfigToType(nil); got != nil {
		t.Errorf("nil input: got %+v, want nil", got)
	}
	got := fmTriggerConfigToType(&frontmatter.TriggerConfig{Type: "cron", Schedule: "0 9 * * *"})
	if got == nil {
		t.Fatal("expected non-nil trigger")
	}
	if got.SkipIfEvent != nil || got.OnlyIfEvent != nil || got.Match != nil {
		t.Errorf("legacy trigger grew scheduling fields: skip=%v only=%v match=%v", got.SkipIfEvent, got.OnlyIfEvent, got.Match)
	}
	if got.Every != "" || got.At != "" || got.Stagger != "" || got.CatchUp != "" || got.Calendar != "" || got.Offset != "" {
		t.Errorf("legacy trigger grew scheduling strings: %+v", got)
	}
}

func TestFmAutomationActionToType_CarriesEveryField(t *testing.T) {
	src := &frontmatter.AutomationAction{
		Type: "prompt", DirectPrompt: "do it", Command: "echo hi", Agent: "tdd-dev", Model: "m",
		Executor: "pi", TargetWorkdir: "/w", ExecutionMode: "worktree", SessionMode: "fresh",
		CompleteOnIdle: boolPtrForTest(true), Timeout: "5m", RequiresCapability: "gpu",
		SetStatus: "active", PromptAppend: "also check the backlog",
	}
	got := fmAutomationActionToType(src)
	if got == nil {
		t.Fatal("expected non-nil action")
	}
	assertNoZeroFields(t, got)
	if got.PromptAppend != "also check the backlog" {
		t.Errorf("PromptAppend = %q", got.PromptAppend)
	}
	if fmAutomationActionToType(nil) != nil {
		t.Error("nil input should convert to nil")
	}
}

func TestMapFrontmatterToUpdateRequest_LifecycleFieldsSurviveRawEdit(t *testing.T) {
	req := mapFrontmatterToUpdateRequest(frontmatter.Frontmatter{
		Title:        "Weekly review",
		StartsAt:     "2026-10-10T09:00:00Z",
		ExpiresAt:    "2026-12-31T00:00:00Z",
		RunOnceAt:    "2026-10-11T08:00:00Z",
		Timezone:     "America/Denver",
		Extends:      "parent-automation",
		ScheduledFor: "2026-10-12T09:00:00-06:00",
		Binding:      "binding-1",
	}, "")

	checks := []struct {
		name string
		got  *string
		want string
	}{
		{"StartsAt", req.StartsAt, "2026-10-10T09:00:00Z"},
		{"ExpiresAt", req.ExpiresAt, "2026-12-31T00:00:00Z"},
		{"RunOnceAt", req.RunOnceAt, "2026-10-11T08:00:00Z"},
		{"Timezone", req.Timezone, "America/Denver"},
		{"Extends", req.Extends, "parent-automation"},
		{"ScheduledFor", req.ScheduledFor, "2026-10-12T09:00:00-06:00"},
		{"Binding", req.Binding, "binding-1"},
	}
	for _, c := range checks {
		if c.got == nil {
			t.Errorf("%s dropped by raw edit (nil)", c.name)
			continue
		}
		if *c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, *c.got, c.want)
		}
	}
}

func TestMapFrontmatterToUpdateRequest_EmptyLifecycleFieldsStayUnset(t *testing.T) {
	req := mapFrontmatterToUpdateRequest(frontmatter.Frontmatter{Title: "x"}, "")
	for name, p := range map[string]*string{
		"StartsAt": req.StartsAt, "ExpiresAt": req.ExpiresAt, "RunOnceAt": req.RunOnceAt,
		"Timezone": req.Timezone, "Extends": req.Extends, "ScheduledFor": req.ScheduledFor, "Binding": req.Binding,
	} {
		if p != nil {
			t.Errorf("%s should be nil for an absent key (partial-merge semantics), got %q", name, *p)
		}
	}
}

func TestMapFrontmatterToUpdateRequest_CarriesSchedulingTrigger(t *testing.T) {
	req := mapFrontmatterToUpdateRequest(frontmatter.Frontmatter{
		Trigger: fullFMTrigger(),
		Action:  &frontmatter.AutomationAction{Type: "prompt", PromptAppend: "extra"},
	}, "")
	if req.Trigger == nil || req.Trigger.Every != "4d" || req.Trigger.SkipIfEvent == nil || req.Trigger.Match["title"] != "re:^standup" {
		t.Errorf("scheduling trigger fields dropped: %+v", req.Trigger)
	}
	if req.Action == nil || req.Action.PromptAppend != "extra" {
		t.Errorf("action prompt_append dropped: %+v", req.Action)
	}
}

func TestTriggerFromPayload_CarriesSchedulingFields(t *testing.T) {
	got := triggerFromPayload(map[string]any{"trigger": map[string]any{
		"type":     "cron",
		"timezone": "America/Denver",
		"every":    "1w",
		"at":       "08:00",
		"stagger":  "2h",
		"catch_up": "none",
		"calendar": "workdays",
		"offset":   "-15m",
		"match":    map[string]any{"title": "re:^standup", "ignored": 3},
		"skip_if_event": map[string]any{
			"calendar": "holidays", "title": "Holiday", "description": "*", "location": "home", "all_day": true,
		},
		"only_if_event": map[string]any{"calendar": "oncall", "all_day": "false"},
	}})
	if got == nil {
		t.Fatal("expected non-nil trigger")
	}
	want := &types.TriggerConfig{
		Type: "cron", Timezone: "America/Denver", Every: "1w", At: "08:00", Stagger: "2h", CatchUp: "none",
		Calendar: "workdays", Offset: "-15m",
		Match:       map[string]string{"title": "re:^standup"},
		SkipIfEvent: &types.CalendarEventFilter{Calendar: "holidays", Title: "Holiday", Description: "*", Location: "home", AllDay: "true"},
		OnlyIfEvent: &types.CalendarEventFilter{Calendar: "oncall", AllDay: "false"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("trigger mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestTriggerFromPayload_NilAndEmptySafe(t *testing.T) {
	if triggerFromPayload(nil) != nil {
		t.Error("nil payload should yield nil trigger")
	}
	if triggerFromPayload(map[string]any{"trigger": "nope"}) != nil {
		t.Error("non-map trigger should yield nil")
	}
	got := triggerFromPayload(map[string]any{"trigger": map[string]any{
		"type": "event", "skip_if_event": "bad", "only_if_event": map[string]any{}, "match": "bad",
	}})
	if got == nil {
		t.Fatal("expected non-nil trigger")
	}
	if got.SkipIfEvent != nil || got.OnlyIfEvent != nil || got.Match != nil {
		t.Errorf("malformed/empty scheduling values should stay unset: skip=%v only=%v match=%v", got.SkipIfEvent, got.OnlyIfEvent, got.Match)
	}
}

func TestActionFromPayload_CarriesPromptAppend(t *testing.T) {
	got := actionFromPayload(map[string]any{"action": map[string]any{"type": "prompt", "prompt_append": "focus on infra"}})
	if got == nil || got.PromptAppend != "focus on infra" {
		t.Errorf("prompt_append dropped: %+v", got)
	}
	if actionFromPayload(map[string]any{}) != nil {
		t.Error("missing action should yield nil")
	}
}

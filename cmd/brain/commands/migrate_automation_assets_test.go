package commands

import (
	"reflect"
	"testing"

	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/frontmatter"
)

// TestAutomationAssetCreateRequest_CarriesAllFields pins the asset → API
// converter: every frontmatter automation field must survive, including the
// ones it used to drop (trigger timezone/events, action executor,
// target_workdir, session_mode, set_status) and the automation-scheduling
// fields.
func TestAutomationAssetCreateRequest_CarriesAllFields(t *testing.T) {
	asset := `---
title: Weekly review
type: automation
status: active
extends: parent01
starts_at: "2026-10-01T00:00:00Z"
expires_at: "2026-12-31T00:00:00Z"
timezone: America/Denver
max_runs: 5
trigger:
  type: cron
  events: [task.completed, feature.completed]
  timezone: America/New_York
  every: 4d
  at: "09:30"
  stagger: 2h
  catch_up: none
  calendar: workdays
  skip_if_event:
    calendar: work
    title: "re:(?i)^vacation"
  only_if_event:
    calendar: work
    all_day: "true"
  match:
    title: "re:standup"
  offset: -15m
action:
  type: prompt
  direct_prompt: Review the week
  executor: pi
  target_workdir: /srv/projects/demo
  session_mode: fresh
  set_status: completed
  prompt_append: Focus on the API.
---
Body text
`
	req, err := automationAssetCreateRequest([]byte(asset))
	if err != nil {
		t.Fatalf("automationAssetCreateRequest: %v", err)
	}

	if req.Extends != "parent01" {
		t.Errorf("Extends = %q, want parent01", req.Extends)
	}
	if req.StartsAt != "2026-10-01T00:00:00Z" || req.ExpiresAt != "2026-12-31T00:00:00Z" {
		t.Errorf("StartsAt/ExpiresAt = %q/%q", req.StartsAt, req.ExpiresAt)
	}
	if req.Timezone != "America/Denver" {
		t.Errorf("Timezone = %q, want America/Denver", req.Timezone)
	}
	if req.MaxRuns == nil || *req.MaxRuns != 5 {
		t.Errorf("MaxRuns = %v, want 5", req.MaxRuns)
	}

	wantTrigger := &types.TriggerConfig{
		Type:     "cron",
		Events:   []string{"task.completed", "feature.completed"},
		Timezone: "America/New_York",
		Every:    "4d",
		At:       "09:30",
		Stagger:  "2h",
		CatchUp:  "none",
		Calendar: "workdays",
		SkipIfEvent: &types.CalendarEventFilter{
			Calendar: "work", Title: "re:(?i)^vacation",
		},
		OnlyIfEvent: &types.CalendarEventFilter{Calendar: "work", AllDay: "true"},
		Match:       map[string]string{"title": "re:standup"},
		Offset:      "-15m",
	}
	if !reflect.DeepEqual(req.Trigger, wantTrigger) {
		t.Errorf("Trigger =\n%#v\nwant\n%#v", req.Trigger, wantTrigger)
	}

	wantAction := &types.AutomationAction{
		Type:          "prompt",
		DirectPrompt:  "Review the week",
		Executor:      "pi",
		TargetWorkdir: "/srv/projects/demo",
		SessionMode:   "fresh",
		SetStatus:     "completed",
		PromptAppend:  "Focus on the API.",
	}
	if !reflect.DeepEqual(req.Action, wantAction) {
		t.Errorf("Action =\n%#v\nwant\n%#v", req.Action, wantAction)
	}
}

// TestAutomationAssetTrigger_NilCalendarFilters keeps absent day filters nil
// rather than allocating empty ones (which would serialize as {}).
func TestAutomationAssetTrigger_NilCalendarFilters(t *testing.T) {
	got := automationAssetTrigger(&frontmatter.TriggerConfig{Type: "cron", Schedule: "0 9 * * *"})
	if got.SkipIfEvent != nil || got.OnlyIfEvent != nil {
		t.Fatalf("expected nil calendar filters, got %#v / %#v", got.SkipIfEvent, got.OnlyIfEvent)
	}
	if automationAssetTrigger(nil) != nil || automationAssetAction(nil) != nil {
		t.Fatal("nil input must convert to nil")
	}
}

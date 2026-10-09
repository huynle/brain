package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/calendar"
	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/frontmatter"
)

// Save-time calendar validation: a clock trigger's calendar must name a builtin
// day calendar; a calendar trigger's calendar and any skip_if_event or
// only_if_event calendar must name an ics source. Unknown names are rejected
// with the field that holds them. The check runs only when a registry is
// installed.

func saveValidationRegistry(t *testing.T) *calendar.Registry {
	t.Helper()
	return calendarTestRegistry(t) // xnys (builtin), team (ics)
}

func TestValidateAutomationCalendarsKindsAndFields(t *testing.T) {
	reg := saveValidationRegistry(t)
	tests := []struct {
		name      string
		trigger   *frontmatter.TriggerConfig
		registry  *calendar.Registry
		wantField string // "" means valid
	}{
		{name: "no calendars", trigger: &frontmatter.TriggerConfig{Schedule: "0 9 * * *"}, registry: reg},
		{name: "clock trigger names builtin xnys", trigger: &frontmatter.TriggerConfig{Schedule: "0 9 * * *", Calendar: "xnys"}, registry: reg},
		{name: "every trigger names builtin xnys", trigger: &frontmatter.TriggerConfig{Every: "1d", Calendar: "xnys"}, registry: reg},
		{name: "clock trigger names unknown calendar", trigger: &frontmatter.TriggerConfig{Schedule: "0 9 * * *", Calendar: "nope"}, registry: reg, wantField: "trigger.calendar"},
		{name: "clock trigger names an ics source", trigger: &frontmatter.TriggerConfig{Schedule: "0 9 * * *", Calendar: "team"}, registry: reg, wantField: "trigger.calendar"},
		{name: "calendar trigger names an ics source", trigger: &frontmatter.TriggerConfig{Type: "calendar", Calendar: "team", At: "start"}, registry: reg},
		{name: "calendar trigger names builtin xnys", trigger: &frontmatter.TriggerConfig{Type: "calendar", Calendar: "xnys", At: "start"}, registry: reg, wantField: "trigger.calendar"},
		{name: "calendar trigger names unknown calendar", trigger: &frontmatter.TriggerConfig{Type: "calendar", Calendar: "nope", At: "start"}, registry: reg, wantField: "trigger.calendar"},
		{
			name:     "skip_if_event names an ics source",
			trigger:  &frontmatter.TriggerConfig{Schedule: "0 9 * * *", SkipIfEvent: &frontmatter.CalendarEventFilter{Calendar: "team"}},
			registry: reg,
		},
		{
			name:      "skip_if_event names builtin xnys",
			trigger:   &frontmatter.TriggerConfig{Schedule: "0 9 * * *", SkipIfEvent: &frontmatter.CalendarEventFilter{Calendar: "xnys"}},
			registry:  reg,
			wantField: "trigger.skip_if_event.calendar",
		},
		{
			name:      "only_if_event names unknown calendar",
			trigger:   &frontmatter.TriggerConfig{Schedule: "0 9 * * *", OnlyIfEvent: &frontmatter.CalendarEventFilter{Calendar: "nope"}},
			registry:  reg,
			wantField: "trigger.only_if_event.calendar",
		},
		{
			name:      "only_if_event without a calendar",
			trigger:   &frontmatter.TriggerConfig{Schedule: "0 9 * * *", OnlyIfEvent: &frontmatter.CalendarEventFilter{Title: "re:standup"}},
			registry:  reg,
			wantField: "trigger.only_if_event.calendar",
		},
		{name: "no registry installed skips the check", trigger: &frontmatter.TriggerConfig{Schedule: "0 9 * * *", Calendar: "nope"}, registry: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAutomationCalendars(tt.trigger, tt.registry)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("validateAutomationCalendars = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateAutomationCalendars = nil, want an error on %s", tt.wantField)
			}
			if !errors.Is(err, api.ErrInvalidInput) {
				t.Fatalf("error %v does not match api.ErrInvalidInput", err)
			}
			var verr *automationValidationError
			if !errors.As(err, &verr) || verr.Field != tt.wantField {
				t.Fatalf("error field = %+v, want %q", verr, tt.wantField)
			}
		})
	}
}

// Save rejects an automation whose calendar name is unknown, naming the field,
// and accepts one that names a builtin calendar. The create path runs the
// same validation a stored definition does.
func TestSaveRejectsUnknownAndWrongKindCalendars(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	brain.SetCalendars(saveValidationRegistry(t))

	save := func(trigger types.TriggerConfig) error {
		_, err := brain.Save(context.Background(), types.CreateEntryRequest{
			Type: "automation", Title: "calendar gate", Content: "gate", Status: "active",
			Project: "p", Trigger: &trigger,
			Action: &types.AutomationAction{Type: "prompt", DirectPrompt: "run"},
		})
		return err
	}

	if err := save(types.TriggerConfig{Schedule: "0 9 * * *", Calendar: "xnys"}); err != nil {
		t.Fatalf("save with builtin calendar: %v", err)
	}
	for _, tc := range []struct {
		name    string
		trigger types.TriggerConfig
	}{
		{"unknown", types.TriggerConfig{Schedule: "0 9 * * *", Calendar: "nope"}},
		{"ics source as a day calendar", types.TriggerConfig{Schedule: "0 9 * * *", Calendar: "team"}},
	} {
		err := save(tc.trigger)
		if err == nil {
			t.Fatalf("%s: save accepted, want rejection", tc.name)
		}
		if !errors.Is(err, api.ErrInvalidInput) || !strings.Contains(err.Error(), "trigger.calendar") {
			t.Fatalf("%s: error %v, want invalid input naming trigger.calendar", tc.name, err)
		}
	}
}

// An update that changes an automation's trigger runs the same calendar check as
// a create: a builtin name is accepted, while an unknown or ics name is rejected
// on trigger.calendar and the stored definition stays unchanged.
func TestUpdateRejectsUnknownAndWrongKindCalendars(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	brain.SetCalendars(saveValidationRegistry(t))

	created, err := brain.Save(context.Background(), types.CreateEntryRequest{
		Type: "automation", Title: "calendar gate", Content: "gate", Status: "active",
		Project: "p", Trigger: &types.TriggerConfig{Schedule: "0 9 * * *", Calendar: "xnys"},
		Action: &types.AutomationAction{Type: "prompt", DirectPrompt: "run"},
	})
	if err != nil {
		t.Fatalf("save with builtin calendar: %v", err)
	}

	update := func(calendar string) error {
		_, err := brain.Update(context.Background(), created.Path, types.UpdateEntryRequest{
			Trigger: &types.TriggerConfig{Schedule: "0 9 * * *", Calendar: calendar},
		})
		return err
	}
	if err := update("xnys"); err != nil {
		t.Fatalf("update to builtin calendar: %v", err)
	}
	for _, name := range []string{"nope", "team"} {
		err := update(name)
		if err == nil {
			t.Fatalf("update to calendar %q accepted, want rejection", name)
		}
		if !errors.Is(err, api.ErrInvalidInput) || !strings.Contains(err.Error(), "trigger.calendar") {
			t.Fatalf("update to calendar %q: error %v, want invalid input naming trigger.calendar", name, err)
		}
	}
}

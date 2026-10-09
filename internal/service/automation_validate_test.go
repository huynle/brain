package service

import (
	"context"
	"errors"
	"testing"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/frontmatter"
)

// baseAutomation is a valid, unbound cron automation. Each case mutates it.
func baseAutomation(mutate func(fm *frontmatter.Frontmatter)) *frontmatter.Frontmatter {
	fm := &frontmatter.Frontmatter{
		Type:    "automation",
		Title:   "Morning brief",
		Status:  "active",
		Trigger: &frontmatter.TriggerConfig{Type: "cron", Schedule: "0 9 * * 1-5"},
		Action:  &frontmatter.AutomationAction{Type: "prompt", DirectPrompt: "Write the brief."},
	}
	if mutate != nil {
		mutate(fm)
	}
	return fm
}

// withTrigger adapts a trigger mutation to a frontmatter mutation.
func withTrigger(mutate func(tc *frontmatter.TriggerConfig)) func(*frontmatter.Frontmatter) {
	return func(fm *frontmatter.Frontmatter) { mutate(fm.Trigger) }
}

func noParents(context.Context, string) (*types.BrainEntry, error) { return nil, nil }

// requireFieldError asserts err is a field-naming validation error on
// wantField (matching api.ErrInvalidInput), or nil when wantField is "".
func requireFieldError(t *testing.T, err error, wantField string) {
	t.Helper()
	if wantField == "" {
		if err != nil {
			t.Fatalf("expected valid automation, got error: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("expected a validation error on %q, got nil", wantField)
	}
	var fe *automationValidationError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *automationValidationError on %q, got %T: %v", wantField, err, err)
	}
	if fe.Field != wantField {
		t.Fatalf("field = %q, want %q (message: %s)", fe.Field, wantField, fe.Message)
	}
	if !errors.Is(err, api.ErrInvalidInput) {
		t.Fatalf("validation error must match api.ErrInvalidInput, got %v", err)
	}
}

func TestValidateAutomationDefinition_TriggerRules(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(fm *frontmatter.Frontmatter)
		wantField string
	}{
		// Valid shapes.
		{name: "cron schedule accepted", wantField: ""},
		{
			name: "every with clock time on days accepted",
			mutate: withTrigger(func(tc *frontmatter.TriggerConfig) {
				tc.Schedule, tc.Every, tc.At = "", "1d", "09:30"
			}),
		},
		{
			name: "every with clock time on weeks accepted",
			mutate: withTrigger(func(tc *frontmatter.TriggerConfig) {
				tc.Schedule, tc.Every, tc.At = "", "2w", "23:59"
			}),
		},
		{
			name: "every minutes accepted",
			mutate: withTrigger(func(tc *frontmatter.TriggerConfig) {
				tc.Schedule, tc.Every = "", "90m"
			}),
		},
		{
			name: "calendar trigger with start, offset and match accepted",
			mutate: func(fm *frontmatter.Frontmatter) {
				fm.Trigger = &frontmatter.TriggerConfig{
					Type: "calendar", Calendar: "work", At: "start", Offset: "-168h",
					Match: map[string]string{"title": "re:^Standup"},
				}
			},
		},
		{
			name: "calendar trigger at end accepted",
			mutate: func(fm *frontmatter.Frontmatter) {
				fm.Trigger = &frontmatter.TriggerConfig{Type: "calendar", Calendar: "work", At: "end"}
			},
		},
		{
			name: "stagger, catch_up and cooldown accepted",
			mutate: withTrigger(func(tc *frontmatter.TriggerConfig) {
				tc.Stagger, tc.CatchUp, tc.Cooldown = "2h", "10m", "5m"
			}),
		},
		{
			name: "catch_up none accepted",
			mutate: withTrigger(func(tc *frontmatter.TriggerConfig) {
				tc.CatchUp = "none"
			}),
		},
		{
			name: "filter, match and calendar event filters in every value form accepted",
			mutate: func(fm *frontmatter.Frontmatter) {
				fm.Trigger = &frontmatter.TriggerConfig{
					Type:        "event",
					Event:       "task.completed",
					Filter:      map[string]string{"to_status": "in:completed,blocked", "title": "re:(?i)^release", "tags": "has:supernote", "project": "*"},
					SkipIfEvent: &frontmatter.CalendarEventFilter{Calendar: "holidays", Title: "re:Holiday"},
					OnlyIfEvent: &frontmatter.CalendarEventFilter{AllDay: "true"},
				}
			},
		},

		// Schedule and interval.
		{
			name:      "unparseable cron schedule rejected",
			mutate:    withTrigger(func(tc *frontmatter.TriggerConfig) { tc.Schedule = "every tuesday" }),
			wantField: "trigger.schedule",
		},
		{
			name: "unparseable every rejected",
			mutate: withTrigger(func(tc *frontmatter.TriggerConfig) {
				tc.Schedule, tc.Every = "", "5x"
			}),
			wantField: "trigger.every",
		},
		{
			name: "zero every rejected",
			mutate: withTrigger(func(tc *frontmatter.TriggerConfig) {
				tc.Schedule, tc.Every = "", "0d"
			}),
			wantField: "trigger.every",
		},
		{
			name: "schedule and every together rejected",
			mutate: withTrigger(func(tc *frontmatter.TriggerConfig) {
				tc.Every = "1d"
			}),
			wantField: "trigger.every",
		},

		// Clock time: only with every in d or w.
		{
			name: "at without every rejected",
			mutate: withTrigger(func(tc *frontmatter.TriggerConfig) {
				tc.At = "09:30"
			}),
			wantField: "trigger.at",
		},
		{
			name: "at with minute or hour interval rejected",
			mutate: withTrigger(func(tc *frontmatter.TriggerConfig) {
				tc.Schedule, tc.Every, tc.At = "", "4h", "09:30"
			}),
			wantField: "trigger.at",
		},
		{
			name: "at not 24-hour HH:MM rejected",
			mutate: withTrigger(func(tc *frontmatter.TriggerConfig) {
				tc.Schedule, tc.Every, tc.At = "", "1d", "9am"
			}),
			wantField: "trigger.at",
		},

		// Calendar triggers.
		{
			name: "calendar at other than start or end rejected",
			mutate: func(fm *frontmatter.Frontmatter) {
				fm.Trigger = &frontmatter.TriggerConfig{Type: "calendar", Calendar: "work", At: "middle"}
			},
			wantField: "trigger.at",
		},
		{
			name: "calendar at clock time rejected",
			mutate: func(fm *frontmatter.Frontmatter) {
				fm.Trigger = &frontmatter.TriggerConfig{Type: "calendar", Calendar: "work", At: "09:30"}
			},
			wantField: "trigger.at",
		},
		{
			name: "calendar trigger without calendar rejected",
			mutate: func(fm *frontmatter.Frontmatter) {
				fm.Trigger = &frontmatter.TriggerConfig{Type: "calendar", At: "start"}
			},
			wantField: "trigger.calendar",
		},
		{
			name: "offset beyond plus seven days rejected",
			mutate: func(fm *frontmatter.Frontmatter) {
				fm.Trigger = &frontmatter.TriggerConfig{Type: "calendar", Calendar: "work", At: "start", Offset: "169h"}
			},
			wantField: "trigger.offset",
		},
		{
			name: "offset beyond minus seven days rejected",
			mutate: func(fm *frontmatter.Frontmatter) {
				fm.Trigger = &frontmatter.TriggerConfig{Type: "calendar", Calendar: "work", At: "start", Offset: "-169h"}
			},
			wantField: "trigger.offset",
		},
		{
			name: "offset not a duration rejected",
			mutate: func(fm *frontmatter.Frontmatter) {
				fm.Trigger = &frontmatter.TriggerConfig{Type: "calendar", Calendar: "work", At: "start", Offset: "7d"}
			},
			wantField: "trigger.offset",
		},

		// Stagger, catch-up, cooldown.
		{
			name:      "negative stagger rejected",
			mutate:    withTrigger(func(tc *frontmatter.TriggerConfig) { tc.Stagger = "-1h" }),
			wantField: "trigger.stagger",
		},
		{
			name:      "unparseable stagger rejected",
			mutate:    withTrigger(func(tc *frontmatter.TriggerConfig) { tc.Stagger = "soon" }),
			wantField: "trigger.stagger",
		},
		{
			name:      "unparseable catch_up rejected",
			mutate:    withTrigger(func(tc *frontmatter.TriggerConfig) { tc.CatchUp = "later" }),
			wantField: "trigger.catch_up",
		},
		{
			name:      "negative catch_up rejected",
			mutate:    withTrigger(func(tc *frontmatter.TriggerConfig) { tc.CatchUp = "-5m" }),
			wantField: "trigger.catch_up",
		},
		{
			name:      "unparseable cooldown rejected",
			mutate:    withTrigger(func(tc *frontmatter.TriggerConfig) { tc.Cooldown = "x" }),
			wantField: "trigger.cooldown",
		},
		{
			name:      "negative cooldown rejected",
			mutate:    withTrigger(func(tc *frontmatter.TriggerConfig) { tc.Cooldown = "-1m" }),
			wantField: "trigger.cooldown",
		},

		// Filter values use the types.ValidateFilterValue rules.
		{
			name:      "filter with invalid regex rejected",
			mutate:    withTrigger(func(tc *frontmatter.TriggerConfig) { tc.Filter = map[string]string{"title": "re:("} }),
			wantField: "trigger.filter.title",
		},
		{
			name:      "filter with empty regex rejected",
			mutate:    withTrigger(func(tc *frontmatter.TriggerConfig) { tc.Filter = map[string]string{"title": "re:"} }),
			wantField: "trigger.filter.title",
		},
		{
			name: "match with invalid regex rejected",
			mutate: func(fm *frontmatter.Frontmatter) {
				fm.Trigger = &frontmatter.TriggerConfig{Type: "calendar", Calendar: "work", At: "start", Match: map[string]string{"title": "re:["}}
			},
			wantField: "trigger.match.title",
		},
		{
			name: "skip_if_event with invalid regex rejected",
			mutate: withTrigger(func(tc *frontmatter.TriggerConfig) {
				tc.SkipIfEvent = &frontmatter.CalendarEventFilter{Title: "re:("}
			}),
			wantField: "trigger.skip_if_event.title",
		},
		{
			name: "only_if_event with invalid regex rejected",
			mutate: withTrigger(func(tc *frontmatter.TriggerConfig) {
				tc.OnlyIfEvent = &frontmatter.CalendarEventFilter{Description: "re:[a-"}
			}),
			wantField: "trigger.only_if_event.description",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm := baseAutomation(tt.mutate)
			err := validateAutomationDefinition(context.Background(), fm, "", noParents)
			requireFieldError(t, err, tt.wantField)
		})
	}
}

func TestValidateAutomationDefinition_LifecycleRules(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(fm *frontmatter.Frontmatter)
		wantField string
	}{
		{
			name: "lifecycle fields in order accepted",
			mutate: func(fm *frontmatter.Frontmatter) {
				fm.StartsAt = "2026-10-01T00:00:00Z"
				fm.ExpiresAt = "2026-12-31T23:59:59Z"
				fm.MaxRuns = intPtr(-1)
				fm.Timezone = "America/Denver"
			},
		},
		{
			name: "max_runs zero accepted",
			mutate: func(fm *frontmatter.Frontmatter) {
				fm.MaxRuns = intPtr(0)
			},
		},
		{
			name: "starts_at alone accepted",
			mutate: func(fm *frontmatter.Frontmatter) {
				fm.StartsAt = "2026-10-01T00:00:00Z"
			},
		},
		{
			name: "starts_at not RFC3339 rejected",
			mutate: func(fm *frontmatter.Frontmatter) {
				fm.StartsAt = "2026/10/01"
			},
			wantField: "starts_at",
		},
		{
			name: "expires_at not RFC3339 rejected",
			mutate: func(fm *frontmatter.Frontmatter) {
				fm.ExpiresAt = "tomorrow"
			},
			wantField: "expires_at",
		},
		{
			name: "expires_at equal to starts_at rejected",
			mutate: func(fm *frontmatter.Frontmatter) {
				fm.StartsAt = "2026-10-01T00:00:00Z"
				fm.ExpiresAt = "2026-10-01T00:00:00Z"
			},
			wantField: "expires_at",
		},
		{
			name: "expires_at before starts_at rejected",
			mutate: func(fm *frontmatter.Frontmatter) {
				fm.StartsAt = "2026-10-02T00:00:00Z"
				fm.ExpiresAt = "2026-10-01T00:00:00Z"
			},
			wantField: "expires_at",
		},
		{
			name: "max_runs below -1 rejected",
			mutate: func(fm *frontmatter.Frontmatter) {
				fm.MaxRuns = intPtr(-2)
			},
			wantField: "max_runs",
		},
		{
			name: "unknown IANA timezone rejected",
			mutate: func(fm *frontmatter.Frontmatter) {
				fm.Timezone = "Mars/Olympus"
			},
			wantField: "timezone",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm := baseAutomation(tt.mutate)
			err := validateAutomationDefinition(context.Background(), fm, "", noParents)
			requireFieldError(t, err, tt.wantField)
		})
	}
}

// bindingAutomation is a valid binding of parent: no trigger of its own and
// only a prompt suffix. Each case mutates it.
func bindingAutomation(parent string, mutate func(fm *frontmatter.Frontmatter)) *frontmatter.Frontmatter {
	return baseAutomation(func(fm *frontmatter.Frontmatter) {
		fm.Trigger = nil
		fm.Action = &frontmatter.AutomationAction{PromptAppend: "Also check the inbox."}
		fm.Extends = parent
		if mutate != nil {
			mutate(fm)
		}
	})
}

// bindingParents is the set of parent entries the binding cases resolve.
func bindingParents() map[string]*types.BrainEntry {
	return map[string]*types.BrainEntry{
		"cron1": {
			ID: "cron1", Type: "automation",
			Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 9 * * *"},
			Action:  &types.AutomationAction{Type: "prompt", DirectPrompt: "base prompt"},
		},
		"legacy1": {
			ID: "legacy1", Type: "automation",
			Trigger: &types.TriggerConfig{Event: "task.completed"},
		},
		"task1":    {ID: "task1", Type: "task"},
		"binding1": {ID: "binding1", Type: "automation", Extends: "cron1"},
		"goal1":    {ID: "goal1", Type: "automation", Goal: &types.GoalConfig{ID: "g1"}},
		"goal2":    {ID: "goal2", Type: "automation", GeneratedBy: "brain-goal"},
		"cal1": {
			ID: "cal1", Type: "automation",
			Trigger: &types.TriggerConfig{Type: "calendar", Calendar: "work", At: "start"},
		},
	}
}

func bindingLookup(parents map[string]*types.BrainEntry) automationParentLookup {
	return func(_ context.Context, id string) (*types.BrainEntry, error) {
		return parents[id], nil
	}
}

func TestValidateAutomationDefinition_BindingRules(t *testing.T) {
	parents := bindingParents()
	tests := []struct {
		name      string
		fm        *frontmatter.Frontmatter
		selfID    string
		wantField string
	}{
		{name: "binding with prompt suffix accepted", fm: bindingAutomation("cron1", nil)},
		{
			name: "binding that reschedules its parent accepted",
			fm: bindingAutomation("cron1", func(fm *frontmatter.Frontmatter) {
				fm.Trigger = &frontmatter.TriggerConfig{Type: "cron", Schedule: "0 17 * * *"}
			}),
		},
		{
			name: "binding repeating the parent trigger type accepted",
			fm: bindingAutomation("cron1", func(fm *frontmatter.Frontmatter) {
				fm.Trigger = &frontmatter.TriggerConfig{Type: "cron"}
			}),
		},
		{
			name: "binding naming event for a legacy event parent accepted",
			fm: bindingAutomation("legacy1", func(fm *frontmatter.Frontmatter) {
				fm.Trigger = &frontmatter.TriggerConfig{Type: "event"}
			}),
		},
		{
			name:      "unknown parent rejected",
			fm:        bindingAutomation("missing1", nil),
			wantField: "extends",
		},
		{
			name:      "parent that is not an automation rejected",
			fm:        bindingAutomation("task1", nil),
			wantField: "extends",
		},
		{
			name:      "parent that is itself a binding rejected",
			fm:        bindingAutomation("binding1", nil),
			wantField: "extends",
		},
		{
			name:      "goal automation parent rejected",
			fm:        bindingAutomation("goal1", nil),
			wantField: "extends",
		},
		{
			name:      "brain-goal generated parent rejected",
			fm:        bindingAutomation("goal2", nil),
			wantField: "extends",
		},
		{
			name:      "calendar-type parent rejected",
			fm:        bindingAutomation("cal1", nil),
			wantField: "extends",
		},
		{
			name:      "self-extension rejected",
			fm:        bindingAutomation("cron1", nil),
			selfID:    "cron1",
			wantField: "extends",
		},
		{
			name: "trigger type different from parent rejected",
			fm: bindingAutomation("cron1", func(fm *frontmatter.Frontmatter) {
				fm.Trigger = &frontmatter.TriggerConfig{Type: "event", Event: "task.completed"}
			}),
			wantField: "trigger.type",
		},
		{
			name: "calendar trigger type on cron parent rejected",
			fm: bindingAutomation("cron1", func(fm *frontmatter.Frontmatter) {
				fm.Trigger = &frontmatter.TriggerConfig{Type: "calendar", Calendar: "work", At: "start"}
			}),
			wantField: "trigger.type",
		},
		{
			name: "action type rejected",
			fm: bindingAutomation("cron1", func(fm *frontmatter.Frontmatter) {
				fm.Action = &frontmatter.AutomationAction{Type: "script", Command: "echo hi"}
			}),
			wantField: "action.type",
		},
		{
			name: "action direct_prompt rejected",
			fm: bindingAutomation("cron1", func(fm *frontmatter.Frontmatter) {
				fm.Action = &frontmatter.AutomationAction{DirectPrompt: "replace the prompt"}
			}),
			wantField: "action.direct_prompt",
		},
		{
			name: "filter project rejected",
			fm: bindingAutomation("cron1", func(fm *frontmatter.Frontmatter) {
				fm.Trigger = &frontmatter.TriggerConfig{Filter: map[string]string{"project": "brain"}}
			}),
			wantField: "trigger.filter.project",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAutomationDefinition(context.Background(), tt.fm, tt.selfID, bindingLookup(parents))
			requireFieldError(t, err, tt.wantField)
		})
	}
}

func TestValidateAutomationDefinition_BindingLookupFailureIsNotValidation(t *testing.T) {
	boom := errors.New("storage unavailable")
	lookup := func(context.Context, string) (*types.BrainEntry, error) { return nil, boom }
	err := validateAutomationDefinition(context.Background(), bindingAutomation("cron1", nil), "", lookup)
	if !errors.Is(err, boom) {
		t.Fatalf("expected the lookup failure to propagate, got %v", err)
	}
	if errors.Is(err, api.ErrInvalidInput) {
		t.Fatalf("an infrastructure failure must not be reported as invalid input: %v", err)
	}
}

// A calendar trigger renders event text that invite senders control. Only a
// prompt action fences that text as untrusted data; a script, HTTP or update
// action would put it into a shell command or request unfenced, so those are
// rejected on save.
func TestValidateAutomationDefinition_CalendarTriggerRequiresPromptAction(t *testing.T) {
	calendarTrigger := func(fm *frontmatter.Frontmatter) {
		fm.Trigger = &frontmatter.TriggerConfig{Type: "calendar", Calendar: "work"}
	}
	for _, tc := range []struct {
		actionType string
		wantField  string
	}{
		{actionType: "prompt", wantField: ""},
		{actionType: "", wantField: ""},
		{actionType: "script", wantField: "action.type"},
		{actionType: "http", wantField: "action.type"},
		{actionType: "update", wantField: "action.type"},
	} {
		t.Run("action "+tc.actionType, func(t *testing.T) {
			fm := baseAutomation(func(fm *frontmatter.Frontmatter) {
				calendarTrigger(fm)
				fm.Action.Type = tc.actionType
				if tc.actionType == "script" {
					fm.Action.Command = "echo {{.Event.Title}}"
				}
			})
			err := validateAutomationDefinition(context.Background(), fm, "", noParents)
			requireFieldError(t, err, tc.wantField)
		})
	}
}

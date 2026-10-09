package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/cron"
	"github.com/huynle/brain-api/pkg/frontmatter"
	"github.com/huynle/brain-api/pkg/schedule"
)

// automationParentLookup returns the automation entry with the given short
// ID, or (nil, nil) when none exists. Errors are infrastructure failures, not
// validation failures.
type automationParentLookup func(ctx context.Context, id string) (*types.BrainEntry, error)

// maxCalendarOffset bounds a calendar trigger's offset in either direction.
const maxCalendarOffset = 7 * 24 * time.Hour

// automationValidationError rejects one field of an automation definition.
// It matches api.ErrInvalidInput, and the API maps it to a 400 with the
// field name in the validation details.
type automationValidationError struct {
	Field   string
	Message string
}

func (e *automationValidationError) Error() string {
	return api.ErrInvalidInput.Error() + ": " + e.Field + ": " + e.Message
}

func (e *automationValidationError) Is(target error) bool {
	return target == api.ErrInvalidInput
}

// ValidationDetail reports the rejected field in the API's validation format.
func (e *automationValidationError) ValidationDetail() types.ValidationDetail {
	return types.ValidationDetail{Field: e.Field, Message: e.Message}
}

func invalidAutomationField(field, message string) error {
	return &automationValidationError{Field: field, Message: message}
}

// validateAutomationDefinition checks an automation's trigger and lifecycle
// fields against the scheduling rules. selfID is the entry's own short ID
// ("" when it has none yet) and parents resolves a binding's parent.
func validateAutomationDefinition(ctx context.Context, fm *frontmatter.Frontmatter, selfID string, parents automationParentLookup) error {
	if err := validateAutomationTrigger(fm.Trigger); err != nil {
		return err
	}
	if err := validateAutomationLifecycle(fm); err != nil {
		return err
	}
	return validateAutomationBinding(ctx, fm, selfID, parents)
}

// validateAutomationBinding checks an automation that extends a parent. A
// binding overlays its parent: it may reschedule and append to the prompt,
// but it may not change what kind of automation the parent is.
func validateAutomationBinding(ctx context.Context, fm *frontmatter.Frontmatter, selfID string, parents automationParentLookup) error {
	if fm.Extends == "" {
		return nil
	}
	if fm.Extends == selfID {
		return invalidAutomationField("extends", "an automation cannot extend itself")
	}
	parent, err := parents(ctx, fm.Extends)
	if err != nil {
		return fmt.Errorf("look up parent automation %q: %w", fm.Extends, err)
	}
	if parent == nil {
		return invalidAutomationField("extends", fmt.Sprintf("parent automation %q not found", fm.Extends))
	}
	if parent.Type != "automation" {
		return invalidAutomationField("extends", "parent must be an automation")
	}
	if parent.Extends != "" {
		return invalidAutomationField("extends", "parent is itself a binding; bindings extend one level only")
	}
	if parent.Goal != nil || parent.GeneratedBy == "brain-goal" {
		return invalidAutomationField("extends", "goal automations cannot be extended")
	}
	parentType := triggerTypeOrDefault(parent.Trigger)
	if parentType == types.TriggerTypeCalendar {
		return invalidAutomationField("extends", "calendar-type parents cannot have bindings")
	}

	if fm.Trigger != nil {
		if fm.Trigger.Type != "" && fm.Trigger.Type != parentType {
			return invalidAutomationField("trigger.type", fmt.Sprintf("binding cannot change the trigger type from %q", parentType))
		}
		if _, ok := fm.Trigger.Filter["project"]; ok {
			return invalidAutomationField("trigger.filter.project", "a binding cannot change the project filter")
		}
	}
	if fm.Action != nil {
		if fm.Action.Type != "" {
			return invalidAutomationField("action.type", "a binding cannot set the action type")
		}
		if fm.Action.DirectPrompt != "" {
			return invalidAutomationField("action.direct_prompt", "a binding cannot replace the prompt; use prompt_append")
		}
	}
	return nil
}

// triggerTypeOrDefault returns the trigger type, treating an empty type as the
// legacy event trigger.
func triggerTypeOrDefault(tc *types.TriggerConfig) string {
	if tc == nil || tc.Type == "" {
		return types.TriggerTypeEvent
	}
	return tc.Type
}

func validateAutomationTrigger(tc *frontmatter.TriggerConfig) error {
	if tc == nil {
		return nil
	}
	if tc.Schedule != "" && tc.Every != "" {
		return invalidAutomationField("trigger.every", "cannot be combined with trigger.schedule")
	}
	if tc.Schedule != "" {
		if _, err := cron.Parse(tc.Schedule); err != nil {
			return invalidAutomationField("trigger.schedule", err.Error())
		}
	}
	var every schedule.Interval
	if tc.Every != "" {
		iv, err := schedule.ParseEvery(tc.Every)
		if err != nil {
			return invalidAutomationField("trigger.every", err.Error())
		}
		every = iv
	}

	if tc.Type == types.TriggerTypeCalendar {
		if err := validateCalendarTrigger(tc); err != nil {
			return err
		}
	} else if tc.At != "" {
		if tc.Every == "" || !every.Calendar() {
			return invalidAutomationField("trigger.at", "only valid with trigger.every in days (d) or weeks (w)")
		}
		if _, _, err := schedule.ParseAt(tc.At); err != nil {
			return invalidAutomationField("trigger.at", err.Error())
		}
	}

	if tc.Offset != "" {
		d, err := time.ParseDuration(tc.Offset)
		if err != nil {
			return invalidAutomationField("trigger.offset", `want a duration such as "-15m"`)
		}
		if d > maxCalendarOffset || d < -maxCalendarOffset {
			return invalidAutomationField("trigger.offset", "must be within ±7 days")
		}
	}
	if err := validateNonNegativeDuration("trigger.stagger", tc.Stagger); err != nil {
		return err
	}
	if _, err := schedule.ParseCatchUp(tc.CatchUp); err != nil {
		return invalidAutomationField("trigger.catch_up", `want "none" or a duration such as "10m"`)
	}
	if err := validateNonNegativeDuration("trigger.cooldown", tc.Cooldown); err != nil {
		return err
	}

	if err := validateFilterValues("trigger.filter", tc.Filter); err != nil {
		return err
	}
	if err := validateFilterValues("trigger.match", tc.Match); err != nil {
		return err
	}
	if err := validateEventFilterValues("trigger.skip_if_event", tc.SkipIfEvent); err != nil {
		return err
	}
	return validateEventFilterValues("trigger.only_if_event", tc.OnlyIfEvent)
}

// validateNonNegativeDuration accepts an empty value (unset) or a Go duration
// that is not negative.
func validateNonNegativeDuration(field, value string) error {
	if value == "" {
		return nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return invalidAutomationField(field, `want a duration such as "5m"`)
	}
	if d < 0 {
		return invalidAutomationField(field, "must not be negative")
	}
	return nil
}

// validateCalendarTrigger checks the fields only a calendar trigger uses.
func validateCalendarTrigger(tc *frontmatter.TriggerConfig) error {
	switch tc.At {
	case "", "start", "end":
	default:
		return invalidAutomationField("trigger.at", `calendar trigger at must be "start" or "end"`)
	}
	if tc.Calendar == "" {
		return invalidAutomationField("trigger.calendar", "required for a calendar trigger")
	}
	return nil
}

// validateFilterValues checks every value of a filter-style map against the
// shared filter-value rules, naming the offending key in the field.
func validateFilterValues(prefix string, values map[string]string) error {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := types.ValidateFilterValue(values[k]); err != nil {
			return invalidAutomationField(prefix+"."+k, err.Error())
		}
	}
	return nil
}

// validateEventFilterValues checks the populated fields of a calendar event
// filter with the shared filter-value rules.
func validateEventFilterValues(prefix string, f *frontmatter.CalendarEventFilter) error {
	if f == nil {
		return nil
	}
	fields := []struct{ name, value string }{
		{"title", f.Title},
		{"description", f.Description},
		{"location", f.Location},
		{"all_day", f.AllDay},
	}
	for _, field := range fields {
		if err := types.ValidateFilterValue(field.value); err != nil {
			return invalidAutomationField(prefix+"."+field.name, err.Error())
		}
	}
	return nil
}

func validateAutomationLifecycle(fm *frontmatter.Frontmatter) error {
	var starts, expires time.Time
	hasStart, hasExpires := fm.StartsAt != "", fm.ExpiresAt != ""
	if hasStart {
		t, err := time.Parse(time.RFC3339, fm.StartsAt)
		if err != nil {
			return invalidAutomationField("starts_at", "want an RFC3339 timestamp")
		}
		starts = t
	}
	if hasExpires {
		t, err := time.Parse(time.RFC3339, fm.ExpiresAt)
		if err != nil {
			return invalidAutomationField("expires_at", "want an RFC3339 timestamp")
		}
		expires = t
	}
	if hasStart && hasExpires && !starts.Before(expires) {
		return invalidAutomationField("expires_at", "must be after starts_at")
	}
	if fm.MaxRuns != nil && *fm.MaxRuns < -1 {
		return invalidAutomationField("max_runs", "must be -1 (unlimited) or zero or greater")
	}
	if fm.Timezone != "" {
		if _, err := time.LoadLocation(fm.Timezone); err != nil {
			return invalidAutomationField("timezone", "want an IANA timezone name")
		}
	}
	return nil
}

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
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

// The API maps any error with this method to a 400 that names the field
// (internal/api fieldValidationError). This keeps the contract checked.
var _ interface {
	ValidationDetail() types.ValidationDetail
} = (*automationValidationError)(nil)

func invalidAutomationField(field, message string) error {
	return &automationValidationError{Field: field, Message: message}
}

// automationFrontmatterFromRequest returns the on-disk shape of a create
// request, so Save validates exactly what it is about to write.
func automationFrontmatterFromRequest(req types.CreateEntryRequest) *frontmatter.Frontmatter {
	return &frontmatter.Frontmatter{
		Type:      req.Type,
		Trigger:   fmTriggerFromTypes(req.Trigger),
		Action:    automationActionToFM(req.Action),
		Extends:   frontmatter.SanitizeSimpleValue(req.Extends),
		StartsAt:  req.StartsAt,
		ExpiresAt: req.ExpiresAt,
		MaxRuns:   req.MaxRuns,
		Timezone:  req.Timezone,
	}
}

// automationUpdateTouchesDefinition reports whether an update changes a field
// the scheduling rules read. DirectPrompt counts because it writes the
// action's prompt.
func automationUpdateTouchesDefinition(req types.UpdateEntryRequest) bool {
	return req.Trigger != nil || req.Action != nil || req.DirectPrompt != nil ||
		req.Extends != nil || req.StartsAt != nil || req.ExpiresAt != nil ||
		req.MaxRuns != nil || req.Timezone != nil
}

// lookupAutomationParent resolves a binding's parent by short ID. It reads
// the index directly rather than Recall, which records an access.
func (s *BrainServiceImpl) lookupAutomationParent(ctx context.Context, id string) (*types.BrainEntry, error) {
	// resolveEntry is the reviewed, tenant-admitted lookup (no new unscoped
	// storage call site). It also matches paths and titles, but a binding's
	// extends names its parent by ID only: bindings are tagged extends:<id>.
	row, err := s.resolveEntry(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	entry := NoteRowToBrainEntry(row)
	if entry.ID != id {
		return nil, nil
	}
	return &entry, nil
}

// automationMetadataDefinitionKeys are the PATCH /metadata fields that change an
// automation's lifecycle or prompt. A write touching any of them must pass the
// same checks Update applies to a definition change.
var automationMetadataDefinitionKeys = []string{"starts_at", "expires_at", "timezone", "max_runs", "direct_prompt", "extends"}

func automationMetadataTouchesDefinition(fields map[string]interface{}) bool {
	for _, key := range automationMetadataDefinitionKeys {
		if _, ok := fields[key]; ok {
			return true
		}
	}
	return false
}

// validateAutomationMetadata checks an automation's merged definition after a
// PATCH /metadata write. The incoming values are shape-checked first, then
// the stored definition is merged with them and validated as a whole.
func (s *BrainServiceImpl) validateAutomationMetadata(ctx context.Context, row *storage.NoteRow, fields map[string]interface{}) error {
	if err := checkAutomationMetadataShapes(fields); err != nil {
		return err
	}
	merged := make(map[string]interface{})
	if row.Metadata != "" && row.Metadata != "{}" {
		if err := json.Unmarshal([]byte(row.Metadata), &merged); err != nil {
			return fmt.Errorf("parse metadata: %w", err)
		}
	}
	for key, value := range fields {
		merged[key] = value
	}
	if n, ok := wholeMaxRuns(merged["max_runs"]); ok {
		merged["max_runs"] = float64(n)
	}
	fm := reconstructFrontmatter(row, merged)
	if value, ok := fields["direct_prompt"]; ok {
		// Mirror the top-level prompt into the action, as Update does, so the
		// binding rule sees the prompt the caller set.
		if fm.Action == nil {
			fm.Action = &frontmatter.AutomationAction{}
		}
		fm.Action.DirectPrompt, _ = value.(string)
	}
	return s.validateAutomation(ctx, &fm, row.ShortID) // calendar names: automation_validate_calendar.go
}

// checkAutomationMetadataShapes rejects incoming lifecycle values of the wrong
// JSON type, which reconstruction would otherwise drop silently.
func checkAutomationMetadataShapes(fields map[string]interface{}) error {
	for _, key := range []string{"starts_at", "expires_at", "timezone", "direct_prompt", "extends"} {
		value, ok := fields[key]
		if !ok || value == nil {
			continue
		}
		if _, isString := value.(string); !isString {
			return invalidAutomationField(key, "want a string")
		}
	}
	if value, ok := fields["max_runs"]; ok && value != nil {
		if _, isWhole := wholeMaxRuns(value); !isWhole {
			return invalidAutomationField("max_runs", "want a whole number")
		}
	}
	return nil
}

// wholeMaxRuns reads a max_runs value from JSON (float64) or Go (int) form.
func wholeMaxRuns(value interface{}) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		if v != float64(int64(v)) {
			return 0, false
		}
		return int(v), true
	}
	return 0, false
}

// validateAutomationDefinition checks an automation's trigger and lifecycle
// fields against the scheduling rules. selfID is the entry's own short ID
// ("" when it has none yet) and parents resolves a binding's parent.
func validateAutomationDefinition(ctx context.Context, fm *frontmatter.Frontmatter, selfID string, parents automationParentLookup) error {
	if err := validateAutomationTrigger(fm.Trigger); err != nil {
		return err
	}
	if err := validateCalendarTriggerAction(fm); err != nil {
		return err
	}
	if err := validateAutomationLifecycle(fm); err != nil {
		return err
	}
	return validateAutomationBinding(ctx, fm, selfID, parents)
}

// calendarTriggerActionAllowed reports whether a calendar-triggered automation
// may run an action of this type. Event text comes from whoever sends the
// invite; only a prompt action fences it as untrusted data, so a script, HTTP
// or update action would carry it into a shell command or request unfenced.
func calendarTriggerActionAllowed(actionType string) bool {
	switch types.NormalizeAutomationActionType(actionType) {
	case "", types.AutomationActionPrompt:
		return true
	default:
		return false
	}
}

// validateCalendarTriggerAction rejects a calendar trigger paired with any
// action other than a prompt (see calendarTriggerActionAllowed).
func validateCalendarTriggerAction(fm *frontmatter.Frontmatter) error {
	if fm.Trigger == nil || fm.Trigger.Type != types.TriggerTypeCalendar || fm.Action == nil {
		return nil
	}
	if !calendarTriggerActionAllowed(fm.Action.Type) {
		return invalidAutomationField("action.type", "a calendar trigger supports only prompt actions: event text comes from invite senders and is fenced as untrusted data only inside prompts")
	}
	return nil
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
		// Check the prompt first: an update that sets direct_prompt through the
		// top-level mirror also fills in action.type "prompt", and the prompt is
		// the field the caller actually changed.
		if fm.Action.DirectPrompt != "" {
			return invalidAutomationField("action.direct_prompt", "a binding cannot replace the prompt; use prompt_append")
		}
		if fm.Action.Type != "" {
			return invalidAutomationField("action.type", "a binding cannot set the action type")
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
	if tc.Timezone != "" {
		if _, err := time.LoadLocation(tc.Timezone); err != nil {
			return invalidAutomationField("trigger.timezone", "want an IANA timezone name")
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

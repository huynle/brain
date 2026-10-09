package service

import (
	"context"
	"errors"
	"testing"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/frontmatter"
)

// fieldOfValidationError returns the field a validation error names, or "".
func fieldOfValidationError(err error) string {
	var fv interface {
		ValidationDetail() types.ValidationDetail
	}
	if !errors.As(err, &fv) {
		return ""
	}
	return fv.ValidationDetail().Field
}

func triggerTimezoneFrontmatter(tz string) *frontmatter.Frontmatter {
	return &frontmatter.Frontmatter{Type: "automation", Trigger: &frontmatter.TriggerConfig{
		Type: "cron", Schedule: "0 9 * * *", Timezone: tz,
	}}
}

// PATCH /entries/*/metadata writes lifecycle fields straight to the DB, so
// the same rules that Save and Update apply must apply here too.
func TestUpdateMetadata_AutomationLifecycleRejectsInvalidValues(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	expires := "2026-12-31T00:00:00Z"
	auto := saveLifecycleAutomation(t, brain, lifecycleSpec{
		project: "p", schedule: "0 9 * * *", expiresAt: expires,
	})

	tests := []struct {
		name   string
		fields map[string]interface{}
		field  string
	}{
		{"starts_at not RFC3339", map[string]interface{}{"starts_at": "tomorrow"}, "starts_at"},
		{"starts_at not a string", map[string]interface{}{"starts_at": 123}, "starts_at"},
		{"expires_at not RFC3339", map[string]interface{}{"expires_at": "2026-99-01"}, "expires_at"},
		{"starts_at after stored expires_at", map[string]interface{}{"starts_at": "2027-02-01T00:00:00Z"}, "expires_at"},
		{"max_runs below -1", map[string]interface{}{"max_runs": -5}, "max_runs"},
		{"max_runs fractional", map[string]interface{}{"max_runs": 2.5}, "max_runs"},
		{"unknown timezone", map[string]interface{}{"timezone": "Mars/Olympus"}, "timezone"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := brain.UpdateMetadata(ctx, auto.ID, tt.fields)
			if !errors.Is(err, api.ErrInvalidInput) {
				t.Fatalf("err = %v, want api.ErrInvalidInput", err)
			}
			if got := fieldOfValidationError(err); got != tt.field {
				t.Fatalf("validation field = %q, want %q (err: %v)", got, tt.field, err)
			}
		})
	}
}

func TestUpdateMetadata_AutomationValidLifecycleValuesAccepted(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	auto := saveLifecycleAutomation(t, brain, lifecycleSpec{project: "p", schedule: "0 9 * * *"})

	if _, err := brain.UpdateMetadata(ctx, auto.ID, map[string]interface{}{
		"starts_at":  "2026-10-01T00:00:00Z",
		"expires_at": "2027-01-01T00:00:00Z",
		"max_runs":   5,
		"timezone":   "America/New_York",
	}); err != nil {
		t.Fatalf("valid lifecycle patch rejected: %v", err)
	}
}

// Tasks keep their current behaviour: lifecycle validation is for automations.
func TestUpdateMetadata_TaskLifecycleFieldsUnchanged(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	task, err := brain.Save(ctx, types.CreateEntryRequest{
		Type: "task", Title: "Plain task", Content: "body", Project: "p", Status: "pending",
	})
	if err != nil {
		t.Fatalf("Save task: %v", err)
	}
	if _, err := brain.UpdateMetadata(ctx, task.ID, map[string]interface{}{"starts_at": "tomorrow"}); err != nil {
		t.Fatalf("task metadata patch rejected: %v", err)
	}
}

func TestValidateAutomationTrigger_RejectsUnknownTimezone(t *testing.T) {
	bad := triggerTimezoneFrontmatter("Mars/Olympus")
	err := validateAutomationDefinition(context.Background(), bad, "", noParents)
	if got := fieldOfValidationError(err); got != "trigger.timezone" {
		t.Fatalf("validation field = %q, want trigger.timezone (err: %v)", got, err)
	}

	good := triggerTimezoneFrontmatter("America/New_York")
	if err := validateAutomationDefinition(context.Background(), good, "", noParents); err != nil {
		t.Fatalf("valid trigger.timezone rejected: %v", err)
	}
}

// A binding may not replace the prompt, and PATCH /metadata must not let it.
func TestUpdateMetadata_BindingCannotReplacePrompt(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	parent := saveLifecycleAutomation(t, brain, lifecycleSpec{project: "p", schedule: "0 9 * * *"})
	binding, err := brain.Save(ctx, types.CreateEntryRequest{
		Type: "automation", Title: "Binding", Content: "binding", Status: "active",
		Project: "p", Extends: parent.ID,
	})
	if err != nil {
		t.Fatalf("Save binding: %v", err)
	}

	_, err = brain.UpdateMetadata(ctx, binding.ID, map[string]interface{}{"direct_prompt": "replace the prompt"})
	if got := fieldOfValidationError(err); got != "action.direct_prompt" {
		t.Fatalf("validation field = %q, want action.direct_prompt (err: %v)", got, err)
	}
}

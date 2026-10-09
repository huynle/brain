package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// validAutomationRequest is a create request for a valid, unbound automation.
func validAutomationRequest() types.CreateEntryRequest {
	return types.CreateEntryRequest{
		Type:      "automation",
		Title:     "Morning brief",
		Content:   "Summarize overnight work.",
		Status:    "active",
		Trigger:   &types.TriggerConfig{Type: "cron", Schedule: "0 9 * * 1-5"},
		Action:    &types.AutomationAction{Type: "prompt", DirectPrompt: "Write the brief."},
		StartsAt:  "2026-10-01T00:00:00Z",
		ExpiresAt: "2026-12-31T23:59:59Z",
	}
}

func automationCount(t *testing.T, svc *BrainServiceImpl) int {
	t.Helper()
	resp, err := svc.List(context.Background(), types.ListEntriesRequest{Type: "automation", Limit: 1000})
	if err != nil {
		t.Fatalf("List automations: %v", err)
	}
	return len(resp.Entries)
}

func TestSave_AutomationDefinitionValidated(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(req *types.CreateEntryRequest)
		wantField string
	}{
		{name: "valid automation saved", mutate: nil, wantField: ""},
		{
			name: "invalid cron rejected",
			mutate: func(req *types.CreateEntryRequest) {
				req.Trigger = &types.TriggerConfig{Type: "cron", Schedule: "every tuesday"}
			},
			wantField: "trigger.schedule",
		},
		{
			name: "unknown parent rejected",
			mutate: func(req *types.CreateEntryRequest) {
				req.Trigger = nil
				req.Action = &types.AutomationAction{PromptAppend: "Also check the inbox."}
				req.Extends = "nope1"
			},
			wantField: "extends",
		},
		{
			name: "malformed starts_at rejected",
			mutate: func(req *types.CreateEntryRequest) {
				req.StartsAt = "2026/10/01"
			},
			wantField: "starts_at",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := newTestBrainService(t)
			req := validAutomationRequest()
			if tt.mutate != nil {
				tt.mutate(&req)
			}
			before := automationCount(t, svc)
			_, err := svc.Save(context.Background(), req)
			requireFieldError(t, err, tt.wantField)
			if tt.wantField != "" && automationCount(t, svc) != before {
				t.Fatalf("a rejected automation must not be written")
			}
		})
	}
}

func TestSave_AutomationRulesApplyOnlyToAutomations(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	_, err := svc.Save(context.Background(), types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Not an automation",
		Content: "Trigger fields on other types are not validated.",
		Trigger: &types.TriggerConfig{Type: "cron", Schedule: "every tuesday"},
	})
	if err != nil {
		t.Fatalf("non-automation entry rejected by automation rules: %v", err)
	}
}

// saveAutomation saves a valid automation and returns its ID and path.
func saveTestAutomation(t *testing.T, svc *BrainServiceImpl, req types.CreateEntryRequest) (string, string) {
	t.Helper()
	resp, err := svc.Save(context.Background(), req)
	if err != nil {
		t.Fatalf("Save automation: %v", err)
	}
	return resp.ID, resp.Path
}

func TestUpdate_AutomationDefinitionValidatedOnTouch(t *testing.T) {
	tests := []struct {
		name      string
		req       types.UpdateEntryRequest
		wantField string
	}{
		{
			name: "invalid trigger rejected",
			req: types.UpdateEntryRequest{
				Trigger: &types.TriggerConfig{Type: "cron", Schedule: "every tuesday"},
			},
			wantField: "trigger.schedule",
		},
		{
			name:      "expires_at before stored starts_at rejected on the merged result",
			req:       types.UpdateEntryRequest{ExpiresAt: strPtr("2026-09-01T00:00:00Z")},
			wantField: "expires_at",
		},
		{
			name:      "invalid timezone rejected",
			req:       types.UpdateEntryRequest{Timezone: strPtr("Mars/Olympus")},
			wantField: "timezone",
		},
		{
			name:      "max_runs below -1 rejected",
			req:       types.UpdateEntryRequest{MaxRuns: intPtr(-5)},
			wantField: "max_runs",
		},
		{
			name:      "unknown parent rejected",
			req:       types.UpdateEntryRequest{Extends: strPtr("nope1")},
			wantField: "extends",
		},
		{
			name: "valid trigger accepted",
			req: types.UpdateEntryRequest{
				Trigger: &types.TriggerConfig{Type: "cron", Every: "1d", At: "08:00"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := newTestBrainService(t)
			id, _ := saveTestAutomation(t, svc, validAutomationRequest())
			_, err := svc.Update(context.Background(), id, tt.req)
			requireFieldError(t, err, tt.wantField)
		})
	}
}

func TestUpdate_AutomationStatusTagAndContentNeverBlockedByStoredDefinition(t *testing.T) {
	svc, _, dir := newTestBrainService(t)
	id, path := saveTestAutomation(t, svc, validAutomationRequest())

	// Simulate a definition that predates validation or was edited out of band:
	// the stored schedule is no longer a valid cron expression.
	absPath := filepath.Join(dir, path)
	raw, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read stored automation: %v", err)
	}
	corrupted := strings.Replace(string(raw), "0 9 * * 1-5", "not a cron", 1)
	if corrupted == string(raw) {
		t.Fatalf("fixture did not contain the schedule to corrupt:\n%s", raw)
	}
	if err := os.WriteFile(absPath, []byte(corrupted), 0o644); err != nil {
		t.Fatalf("corrupt stored automation: %v", err)
	}

	tests := []struct {
		name string
		req  types.UpdateEntryRequest
	}{
		{name: "status-only update", req: types.UpdateEntryRequest{Status: strPtr("completed")}},
		{name: "tag update", req: types.UpdateEntryRequest{Tags: []string{"review"}}},
		{name: "content update", req: types.UpdateEntryRequest{Content: strPtr("Revised body.")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.Update(context.Background(), id, tt.req); err != nil {
				t.Fatalf("%s must not be blocked by the stored definition: %v", tt.name, err)
			}
		})
	}

	// A trigger update is the fix path, so it is validated and accepted.
	fix := types.UpdateEntryRequest{Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 8 * * 1-5"}}
	if _, err := svc.Update(context.Background(), id, fix); err != nil {
		t.Fatalf("repairing the trigger must be accepted: %v", err)
	}
}

func TestUpdate_BindingDefinitionValidated(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	parentID, _ := saveTestAutomation(t, svc, validAutomationRequest())

	binding := validAutomationRequest()
	binding.Title = "Inbox overlay"
	binding.Trigger = nil
	binding.Action = &types.AutomationAction{PromptAppend: "Also check the inbox."}
	binding.Extends = parentID
	bindingID, _ := saveTestAutomation(t, svc, binding)

	tests := []struct {
		name      string
		req       types.UpdateEntryRequest
		wantField string
	}{
		{
			name:      "action type on a binding rejected",
			req:       types.UpdateEntryRequest{Action: &types.AutomationAction{Type: "script", Command: "echo hi"}},
			wantField: "action.type",
		},
		{
			name:      "direct prompt on a binding rejected through the mirror field",
			req:       types.UpdateEntryRequest{DirectPrompt: strPtr("Replace the prompt.")},
			wantField: "action.direct_prompt",
		},
		{
			name: "rescheduling a binding accepted",
			req:  types.UpdateEntryRequest{Trigger: &types.TriggerConfig{Type: "cron", Schedule: "0 17 * * 1-5"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Update(context.Background(), bindingID, tt.req)
			requireFieldError(t, err, tt.wantField)
		})
	}
}

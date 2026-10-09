package service

import (
	"context"
	"fmt"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// runAuditScheduledFor returns the audit slot as RFC3339 UTC, or "" for the
// zero time, so unscheduled runs write no scheduled_for value.
func runAuditScheduledFor(slot time.Time) string {
	if slot.IsZero() {
		return ""
	}
	return slot.UTC().Format(time.RFC3339)
}

// runAuditBindingTag returns the "binding:<id>" tag, or "" when no binding
// applied.
func runAuditBindingTag(binding string) string {
	if binding == "" {
		return ""
	}
	return "binding:" + binding
}

// listRunAudits returns up to limit automation_run audits tagged with
// automationID in project, newest first. It reads the automation:<id> tag
// index, so it never scans bodies. Legacy audits written before the tag
// existed are not returned here; the API handler falls back to a body scan
// for them.
func (s *AutomationService) listRunAudits(ctx context.Context, project, automationID string, limit int) ([]types.BrainEntry, error) {
	if automationID == "" {
		return nil, fmt.Errorf("list run audits: automation id is required")
	}
	if limit <= 0 || s == nil || s.brain == nil {
		return []types.BrainEntry{}, nil
	}
	resp, err := s.brain.List(ctx, types.ListEntriesRequest{
		Type:      "automation_run",
		Project:   project,
		Tags:      "automation:" + automationID,
		SortBy:    "created",
		SortOrder: "desc",
		Limit:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list run audits: %w", err)
	}
	return resp.Entries, nil
}

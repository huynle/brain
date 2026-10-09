package service

import (
	"context"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// Editing other fields of an automation must keep its lifecycle fields
// (max_runs, starts_at, expires_at) in the markdown file and in the index,
// through a title edit, a durable metadata edit, and a full reindex.
func TestAutomationLifecycleFields_SurviveOtherUpdates(t *testing.T) {
	brain, _, _ := newTestBrainService(t)
	ctx := context.Background()
	limit := 3
	saved, err := brain.Save(ctx, types.CreateEntryRequest{
		Type: "automation", Title: "Persisted lifecycle", Content: "body", Status: "active",
		Project:   "persist",
		Trigger:   &types.TriggerConfig{Type: "cron", Schedule: "0 9 * * *"},
		Action:    &types.AutomationAction{Type: "prompt", DirectPrompt: "work"},
		StartsAt:  "2026-10-01T00:00:00Z",
		ExpiresAt: "2027-01-01T00:00:00Z",
		MaxRuns:   &limit,
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	title := "Renamed lifecycle"
	if _, err := brain.Update(ctx, saved.Path, types.UpdateEntryRequest{Title: &title}); err != nil {
		t.Fatalf("Update title: %v", err)
	}
	if _, err := brain.UpdateMetadata(ctx, saved.ID, map[string]interface{}{"status": "active", "priority": "high"}); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}
	if _, err := brain.indexer.RebuildAll(); err != nil {
		t.Fatalf("RebuildAll: %v", err)
	}

	entry := lifecycleEntry(t, brain, saved.ID)
	if entry.MaxRuns == nil || *entry.MaxRuns != 3 {
		t.Fatalf("index max_runs = %v, want 3", entry.MaxRuns)
	}
	if entry.StartsAt != "2026-10-01T00:00:00Z" || entry.ExpiresAt != "2027-01-01T00:00:00Z" {
		t.Fatalf("index window = %q..%q", entry.StartsAt, entry.ExpiresAt)
	}

	file := readEntryFile(t, brain, saved.Path)
	for _, want := range []string{"max_runs: 3", "starts_at: \"2026-10-01T00:00:00Z\"", "expires_at: \"2027-01-01T00:00:00Z\""} {
		if !strings.Contains(file, want) {
			t.Fatalf("file lost %q after other updates:\n%s", want, file)
		}
	}
}

package events

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/huynle/brain-api/internal/storage"
)

// StorageScheduleSource implements ScheduleSource by querying the storage layer
// for entries with active cron schedules (both regular tasks and automations).
type StorageScheduleSource struct {
	store *storage.StorageLayer
}

// NewStorageScheduleSource creates a ScheduleSource backed by the storage layer.
func NewStorageScheduleSource(store *storage.StorageLayer) *StorageScheduleSource {
	return &StorageScheduleSource{store: store}
}

// ListScheduledEntries returns all brain entries that have active cron schedules.
// It queries both:
//   - Regular tasks/entries with a "schedule" field in metadata
//   - Automation entries with trigger.type="cron"
func (s *StorageScheduleSource) ListScheduledEntries(ctx context.Context) ([]ScheduleEntry, error) {
	// Query all notes — we filter in Go since schedule is in metadata JSON
	// Use a reasonable limit to avoid loading the entire database
	rows, err := s.store.ListNotes(ctx, &storage.ListOptions{
		Limit: 1000,
	})
	if err != nil {
		return nil, err
	}

	var entries []ScheduleEntry

	for _, row := range rows {
		if row.Metadata == "" || row.Metadata == "{}" {
			continue
		}

		var meta map[string]interface{}
		if err := json.Unmarshal([]byte(row.Metadata), &meta); err != nil {
			continue
		}

		entry := s.extractScheduleEntry(row, meta)
		if entry != nil {
			entries = append(entries, *entry)
		}
	}

	slog.Debug("cron_source: listed scheduled entries", "total_rows", len(rows), "scheduled", len(entries))
	return entries, nil
}

// extractScheduleEntry extracts a ScheduleEntry from a NoteRow if it has an active schedule.
// Returns nil if the entry has no active schedule.
func (s *StorageScheduleSource) extractScheduleEntry(row *storage.NoteRow, meta map[string]interface{}) *ScheduleEntry {
	var schedule, timezone string

	// Check for automation trigger with type=cron
	if trigger, ok := meta["trigger"].(map[string]interface{}); ok {
		if trigType, ok := trigger["type"].(string); ok && trigType == "cron" {
			if sched, ok := trigger["schedule"].(string); ok && sched != "" {
				schedule = sched
			}
		}
	}

	// Check for regular task schedule field
	if schedule == "" {
		if sched, ok := meta["schedule"].(string); ok && sched != "" {
			schedule = sched
		}
	}

	if schedule == "" {
		return nil
	}

	// Check schedule_enabled (default true if not set)
	if enabled, ok := meta["schedule_enabled"].(bool); ok && !enabled {
		return nil
	}

	// Extract timezone
	if tz, ok := meta["timezone"].(string); ok {
		timezone = tz
	}

	// Extract project ID
	var projectID string
	if row.ProjectID != nil {
		projectID = *row.ProjectID
	}

	return &ScheduleEntry{
		ID:        row.ShortID,
		Path:      row.Path,
		ProjectID: projectID,
		Schedule:  schedule,
		Timezone:  timezone,
	}
}

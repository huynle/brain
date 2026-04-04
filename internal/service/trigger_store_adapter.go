package service

import (
	"context"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// Compile-time check that TriggerTaskStoreAdapter implements TriggerTaskStore.
var _ TriggerTaskStore = (*TriggerTaskStoreAdapter)(nil)

// TriggerTaskStoreAdapter wraps a StorageLayer to implement TriggerTaskStore.
// It handles the conversion between storage NoteRow and types.BrainEntry.
type TriggerTaskStoreAdapter struct {
	store *storage.StorageLayer
}

// NewTriggerTaskStoreAdapter creates a new adapter wrapping the storage layer.
func NewTriggerTaskStoreAdapter(store *storage.StorageLayer) *TriggerTaskStoreAdapter {
	return &TriggerTaskStoreAdapter{store: store}
}

// ListTriggeredTasks returns all task entries that have a trigger configured.
func (a *TriggerTaskStoreAdapter) ListTriggeredTasks(ctx context.Context) ([]types.BrainEntry, error) {
	rows, err := a.store.ListTriggeredTasks(ctx)
	if err != nil {
		return nil, err
	}

	entries := make([]types.BrainEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, NoteRowToBrainEntry(row))
	}
	return entries, nil
}

// ActivateTask sets a task to pending with the given metadata fields.
func (a *TriggerTaskStoreAdapter) ActivateTask(ctx context.Context, path string, fields map[string]interface{}) error {
	return a.store.ActivateTask(ctx, path, fields)
}

// CountInProgressByTrigger counts tasks that are currently in_progress
// and have a trigger matching the given event pattern within a project.
func (a *TriggerTaskStoreAdapter) CountInProgressByTrigger(ctx context.Context, triggerEvent, projectID string) (int, error) {
	return a.store.CountInProgressByTrigger(ctx, triggerEvent, projectID)
}

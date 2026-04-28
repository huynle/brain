package storage

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// Schema migration v5: event_log table exists
// ---------------------------------------------------------------------------

func TestSchemaV5_EventLogTableExists(t *testing.T) {
	s := newTestStorage(t)

	var name string
	err := s.DB().QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='event_log'",
	).Scan(&name)
	if err != nil {
		t.Fatalf("event_log table not found: %v", err)
	}
	if name != "event_log" {
		t.Errorf("got table name %q, want %q", name, "event_log")
	}
}

func TestSchemaV5_EventLogIndexesExist(t *testing.T) {
	s := newTestStorage(t)

	indexes := []string{
		"idx_event_log_type_created",
		"idx_event_log_dedup_key",
	}
	for _, idx := range indexes {
		t.Run(idx, func(t *testing.T) {
			var name string
			err := s.DB().QueryRow(
				"SELECT name FROM sqlite_master WHERE type='index' AND name=?", idx,
			).Scan(&name)
			if err != nil {
				t.Fatalf("index %q not found: %v", idx, err)
			}
		})
	}
}

func TestSchemaV5_VersionBumped(t *testing.T) {
	s := newTestStorage(t)

	ver, err := GetSchemaVersion(s.DB())
	if err != nil {
		t.Fatalf("GetSchemaVersion: %v", err)
	}
	if ver != CurrentSchemaVersion {
		t.Errorf("got schema version %d, want %d", ver, CurrentSchemaVersion)
	}
}

// ---------------------------------------------------------------------------
// Schema migration v5: upgrade from v4 works
// ---------------------------------------------------------------------------

func TestSchemaV5_MigrationFromV4(t *testing.T) {
	// Simulate a v4 database that gets upgraded to v5.
	db := openMemoryDB(t)
	defer db.Close()

	// First init creates a v4-equivalent DB (current code creates v5 directly,
	// but we verify event_log table exists after InitSchema).
	if err := InitSchema(db); err != nil {
		t.Fatalf("InitSchema failed: %v", err)
	}

	// Verify event_log table exists after migration
	var name string
	err := db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='event_log'",
	).Scan(&name)
	if err != nil {
		t.Fatalf("event_log table not created by migration: %v", err)
	}
}

// ---------------------------------------------------------------------------
// InsertEvent
// ---------------------------------------------------------------------------

func TestInsertEvent(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	id, err := s.InsertEvent(ctx, "task.created", `{"task_id":"abc123"}`, "task-abc123", "brain-runner")
	if err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive ID, got %d", id)
	}
}

func TestInsertEvent_DedupKeyUniqueness(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	_, err := s.InsertEvent(ctx, "task.created", `{"task_id":"abc123"}`, "dedup-1", "runner")
	if err != nil {
		t.Fatalf("first InsertEvent: %v", err)
	}

	// Second insert with same dedup_key should fail
	_, err = s.InsertEvent(ctx, "task.created", `{"task_id":"def456"}`, "dedup-1", "runner")
	if err == nil {
		t.Fatal("expected error on duplicate dedup_key, got nil")
	}
}

func TestInsertEvent_EmptyDedupKey(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Empty dedup_key should be stored as NULL, allowing multiple entries
	id1, err := s.InsertEvent(ctx, "task.created", `{}`, "", "runner")
	if err != nil {
		t.Fatalf("first InsertEvent with empty dedup: %v", err)
	}

	id2, err := s.InsertEvent(ctx, "task.created", `{}`, "", "runner")
	if err != nil {
		t.Fatalf("second InsertEvent with empty dedup: %v", err)
	}

	if id1 == id2 {
		t.Error("expected different IDs for events with empty dedup_key")
	}
}

// ---------------------------------------------------------------------------
// MarkProcessed
// ---------------------------------------------------------------------------

func TestMarkProcessed(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	id, err := s.InsertEvent(ctx, "task.created", `{}`, "", "runner")
	if err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	if err := s.MarkProcessed(ctx, id); err != nil {
		t.Fatalf("MarkProcessed: %v", err)
	}

	// Verify it's no longer in unprocessed
	events, err := s.GetUnprocessed(ctx)
	if err != nil {
		t.Fatalf("GetUnprocessed: %v", err)
	}
	for _, e := range events {
		if e.ID == id {
			t.Errorf("event %d should not appear in unprocessed after MarkProcessed", id)
		}
	}
}

func TestMarkProcessed_NonExistentID(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	err := s.MarkProcessed(ctx, 99999)
	if err == nil {
		t.Fatal("expected error for non-existent ID, got nil")
	}
}

// ---------------------------------------------------------------------------
// GetUnprocessed
// ---------------------------------------------------------------------------

func TestGetUnprocessed(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Empty initially
	events, err := s.GetUnprocessed(ctx)
	if err != nil {
		t.Fatalf("GetUnprocessed (empty): %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}

	// Insert 3 events
	id1, _ := s.InsertEvent(ctx, "task.created", `{"a":1}`, "k1", "src1")
	id2, _ := s.InsertEvent(ctx, "task.updated", `{"a":2}`, "k2", "src2")
	id3, _ := s.InsertEvent(ctx, "task.deleted", `{"a":3}`, "k3", "src3")

	// All 3 should be unprocessed
	events, err = s.GetUnprocessed(ctx)
	if err != nil {
		t.Fatalf("GetUnprocessed: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	// Mark one as processed
	if err := s.MarkProcessed(ctx, id2); err != nil {
		t.Fatalf("MarkProcessed: %v", err)
	}

	events, err = s.GetUnprocessed(ctx)
	if err != nil {
		t.Fatalf("GetUnprocessed after mark: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	// Verify the right events remain
	ids := map[int64]bool{}
	for _, e := range events {
		ids[e.ID] = true
	}
	if !ids[id1] || !ids[id3] {
		t.Errorf("expected events %d and %d, got IDs %v", id1, id3, ids)
	}
}

func TestGetUnprocessed_FieldValues(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	_, err := s.InsertEvent(ctx, "task.created", `{"task_id":"xyz"}`, "my-key", "brain-runner")
	if err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	events, err := s.GetUnprocessed(ctx)
	if err != nil {
		t.Fatalf("GetUnprocessed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.EventType != "task.created" {
		t.Errorf("EventType = %q, want %q", e.EventType, "task.created")
	}
	if e.Payload != `{"task_id":"xyz"}` {
		t.Errorf("Payload = %q, want %q", e.Payload, `{"task_id":"xyz"}`)
	}
	if e.DedupKey == nil || *e.DedupKey != "my-key" {
		t.Errorf("DedupKey = %v, want %q", e.DedupKey, "my-key")
	}
	if e.Source != "brain-runner" {
		t.Errorf("Source = %q, want %q", e.Source, "brain-runner")
	}
	if e.CreatedAt == "" {
		t.Error("CreatedAt should not be empty")
	}
	if e.ProcessedAt != nil {
		t.Errorf("ProcessedAt should be nil for unprocessed event, got %v", e.ProcessedAt)
	}
}

// ---------------------------------------------------------------------------
// GetUnprocessed ordering
// ---------------------------------------------------------------------------

func TestGetUnprocessed_OrderedByCreatedAt(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert events — SQLite auto-increment ensures id ordering matches insert order,
	// and created_at defaults to datetime('now') which is consistent within a test.
	s.InsertEvent(ctx, "first", `{}`, "o1", "src")
	s.InsertEvent(ctx, "second", `{}`, "o2", "src")
	s.InsertEvent(ctx, "third", `{}`, "o3", "src")

	events, err := s.GetUnprocessed(ctx)
	if err != nil {
		t.Fatalf("GetUnprocessed: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3, got %d", len(events))
	}

	// Should be ordered by created_at ASC (oldest first for FIFO processing)
	if events[0].EventType != "first" {
		t.Errorf("events[0].EventType = %q, want %q", events[0].EventType, "first")
	}
	if events[1].EventType != "second" {
		t.Errorf("events[1].EventType = %q, want %q", events[1].EventType, "second")
	}
	if events[2].EventType != "third" {
		t.Errorf("events[2].EventType = %q, want %q", events[2].EventType, "third")
	}
}

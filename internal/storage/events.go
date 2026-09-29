package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// EventRow represents a row in the event_log table.
type EventRow struct {
	ID          int64
	EventType   string
	Payload     string  // JSON
	DedupKey    *string // nullable
	Source      string
	CreatedAt   string
	ProcessedAt *string // nullable
}

// InsertEvent stores a new event in the event_log table and returns its ID.
// If dedupKey is empty, it is stored as NULL (allowing multiple events without dedup).
// A non-empty dedupKey must be unique within the tenant; duplicates return an error.
func (s *TenantStore) InsertEvent(ctx context.Context, eventType, payload, dedupKey, source string) (int64, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return 0, err
	}
	var dk interface{}
	if dedupKey != "" {
		dk = dedupKey
	}

	query := `INSERT INTO event_log (event_type, payload, dedup_key, source) VALUES (?, ?, ?, ?)`
	args := []interface{}{eventType, payload, dk, source}
	if scope.owner != "" {
		query = `INSERT INTO event_log (event_type, payload, dedup_key, source, tenant_id) VALUES (?, ?, ?, ?, ?)`
		args = append(args, scope.owner)
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("insert event: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return id, nil
}

// MarkProcessed sets the processed_at timestamp for an event.
// Returns an error if the event does not exist.
func (s *TenantStore) MarkProcessed(ctx context.Context, id int64) error {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return err
	}
	where, args := scope.where("id = ?", id)
	result, err := s.db.ExecContext(ctx,
		"UPDATE event_log SET processed_at = datetime('now') WHERE "+where,
		args...,
	)
	if err != nil {
		return fmt.Errorf("mark event processed: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("event not found: %d", id)
	}
	return nil
}

// GetEventsByType returns events of the given event_type, ordered newest
// first (created_at DESC, id DESC). A non-positive limit defaults to 100;
// the limit is capped at 1000. Uses the idx_event_log_type_created index.
func (s *TenantStore) GetEventsByType(ctx context.Context, eventType string, limit int) ([]*EventRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	where, args := scope.where("event_type = ?", eventType)
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, event_type, payload, dedup_key, source, created_at, processed_at
		 FROM event_log
		 WHERE `+where+`
		 ORDER BY created_at DESC, id DESC
		 LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query events by type: %w", err)
	}
	defer rows.Close()

	var events []*EventRow
	for rows.Next() {
		e := &EventRow{}
		var dedupKey sql.NullString
		var processedAt sql.NullString
		if err := rows.Scan(&e.ID, &e.EventType, &e.Payload, &dedupKey, &e.Source, &e.CreatedAt, &processedAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		if dedupKey.Valid {
			e.DedupKey = &dedupKey.String
		}
		if processedAt.Valid {
			e.ProcessedAt = &processedAt.String
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	if events == nil {
		return []*EventRow{}, nil
	}
	return events, nil
}

// GetUnprocessed returns all events where processed_at IS NULL,
// ordered by created_at ASC (oldest first, FIFO).
func (s *TenantStore) GetUnprocessed(ctx context.Context) ([]*EventRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	where, args := scope.where("processed_at IS NULL")
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, event_type, payload, dedup_key, source, created_at, processed_at
		 FROM event_log
		 WHERE `+where+`
		 ORDER BY created_at ASC, id ASC`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query unprocessed events: %w", err)
	}
	defer rows.Close()

	var events []*EventRow
	for rows.Next() {
		e := &EventRow{}
		var dedupKey sql.NullString
		var processedAt sql.NullString
		if err := rows.Scan(&e.ID, &e.EventType, &e.Payload, &dedupKey, &e.Source, &e.CreatedAt, &processedAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		if dedupKey.Valid {
			e.DedupKey = &dedupKey.String
		}
		if processedAt.Valid {
			e.ProcessedAt = &processedAt.String
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	if events == nil {
		return []*EventRow{}, nil
	}
	return events, nil
}

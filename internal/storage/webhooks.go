package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Webhook represents a row in the webhooks table.
type Webhook struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	URL       string                 `json:"url"`
	Events    []string               `json:"events"`
	Filter    map[string]interface{} `json:"filter"`
	Secret    string                 `json:"secret,omitempty"`
	Enabled   bool                   `json:"enabled"`
	CreatedAt string                 `json:"created_at"`
	UpdatedAt string                 `json:"updated_at"`
}

// WebhookDelivery represents a row in the webhook_deliveries table.
type WebhookDelivery struct {
	ID         string `json:"id"`
	WebhookID  string `json:"webhook_id"`
	EventType  string `json:"event_type"`
	StatusCode *int   `json:"status_code,omitempty"`
	Success    bool   `json:"success"`
	LatencyMs  *int   `json:"latency_ms,omitempty"`
	Error      string `json:"error,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// generateID generates a random 8-byte hex ID (16 characters).
func generateID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// CreateWebhook inserts a new webhook into the database.
func (s *StorageLayer) CreateWebhook(ctx context.Context, wh *Webhook) error {
	if wh.ID == "" {
		id, err := generateID()
		if err != nil {
			return err
		}
		wh.ID = id
	}

	eventsJSON, err := json.Marshal(wh.Events)
	if err != nil {
		return fmt.Errorf("marshal events: %w", err)
	}

	filterJSON, err := json.Marshal(wh.Filter)
	if err != nil {
		return fmt.Errorf("marshal filter: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if wh.CreatedAt == "" {
		wh.CreatedAt = now
	}
	if wh.UpdatedAt == "" {
		wh.UpdatedAt = now
	}

	enabled := 0
	if wh.Enabled {
		enabled = 1
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO webhooks (id, name, url, events, filter, secret, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		wh.ID, wh.Name, wh.URL, string(eventsJSON), string(filterJSON),
		wh.Secret, enabled, wh.CreatedAt, wh.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert webhook: %w", err)
	}
	return nil
}

// GetWebhook retrieves a webhook by ID.
func (s *StorageLayer) GetWebhook(ctx context.Context, id string) (*Webhook, error) {
	var wh Webhook
	var eventsJSON, filterJSON string
	var enabled int

	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, url, events, filter, secret, enabled, created_at, updated_at
		 FROM webhooks WHERE id = ?`, id,
	).Scan(&wh.ID, &wh.Name, &wh.URL, &eventsJSON, &filterJSON,
		&wh.Secret, &enabled, &wh.CreatedAt, &wh.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("webhook not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("query webhook: %w", err)
	}

	wh.Enabled = enabled == 1

	if err := json.Unmarshal([]byte(eventsJSON), &wh.Events); err != nil {
		return nil, fmt.Errorf("unmarshal events: %w", err)
	}
	if err := json.Unmarshal([]byte(filterJSON), &wh.Filter); err != nil {
		return nil, fmt.Errorf("unmarshal filter: %w", err)
	}

	return &wh, nil
}

// ListWebhooks returns all webhooks, optionally filtered by enabled status.
func (s *StorageLayer) ListWebhooks(ctx context.Context, enabledOnly ...bool) ([]Webhook, error) {
	query := `SELECT id, name, url, events, filter, secret, enabled, created_at, updated_at FROM webhooks`
	if len(enabledOnly) > 0 && enabledOnly[0] {
		query += " WHERE enabled = 1"
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query webhooks: %w", err)
	}
	defer rows.Close()

	var webhooks []Webhook
	for rows.Next() {
		var wh Webhook
		var eventsJSON, filterJSON string
		var enabled int

		if err := rows.Scan(&wh.ID, &wh.Name, &wh.URL, &eventsJSON, &filterJSON,
			&wh.Secret, &enabled, &wh.CreatedAt, &wh.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan webhook: %w", err)
		}

		wh.Enabled = enabled == 1

		if err := json.Unmarshal([]byte(eventsJSON), &wh.Events); err != nil {
			return nil, fmt.Errorf("unmarshal events: %w", err)
		}
		if err := json.Unmarshal([]byte(filterJSON), &wh.Filter); err != nil {
			return nil, fmt.Errorf("unmarshal filter: %w", err)
		}

		webhooks = append(webhooks, wh)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	if webhooks == nil {
		return []Webhook{}, nil
	}
	return webhooks, nil
}

// UpdateWebhook updates an existing webhook. Only non-zero fields are updated.
func (s *StorageLayer) UpdateWebhook(ctx context.Context, wh *Webhook) error {
	eventsJSON, err := json.Marshal(wh.Events)
	if err != nil {
		return fmt.Errorf("marshal events: %w", err)
	}

	filterJSON, err := json.Marshal(wh.Filter)
	if err != nil {
		return fmt.Errorf("marshal filter: %w", err)
	}

	enabled := 0
	if wh.Enabled {
		enabled = 1
	}

	wh.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	result, err := s.db.ExecContext(ctx,
		`UPDATE webhooks SET name = ?, url = ?, events = ?, filter = ?, secret = ?,
		 enabled = ?, updated_at = ? WHERE id = ?`,
		wh.Name, wh.URL, string(eventsJSON), string(filterJSON),
		wh.Secret, enabled, wh.UpdatedAt, wh.ID,
	)
	if err != nil {
		return fmt.Errorf("update webhook: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("webhook not found: %s", wh.ID)
	}
	return nil
}

// DeleteWebhook removes a webhook and its deliveries (via CASCADE) by ID.
func (s *StorageLayer) DeleteWebhook(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM webhooks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("webhook not found: %s", id)
	}
	return nil
}

// CreateDelivery logs a webhook delivery attempt.
func (s *StorageLayer) CreateDelivery(ctx context.Context, d *WebhookDelivery) error {
	if d.ID == "" {
		id, err := generateID()
		if err != nil {
			return err
		}
		d.ID = id
	}

	if d.CreatedAt == "" {
		d.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	success := 0
	if d.Success {
		success = 1
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO webhook_deliveries (id, webhook_id, event_type, status_code, success, latency_ms, error, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.WebhookID, d.EventType, d.StatusCode, success, d.LatencyMs, d.Error, d.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert delivery: %w", err)
	}
	return nil
}

// ListDeliveries returns deliveries for a webhook, ordered by most recent first.
func (s *StorageLayer) ListDeliveries(ctx context.Context, webhookID string, limit int) ([]WebhookDelivery, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, webhook_id, event_type, status_code, success, latency_ms, COALESCE(error, ''), created_at
		 FROM webhook_deliveries WHERE webhook_id = ? ORDER BY created_at DESC LIMIT ?`,
		webhookID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query deliveries: %w", err)
	}
	defer rows.Close()

	var deliveries []WebhookDelivery
	for rows.Next() {
		var d WebhookDelivery
		var statusCode, latencyMs sql.NullInt64
		var success int

		if err := rows.Scan(&d.ID, &d.WebhookID, &d.EventType, &statusCode,
			&success, &latencyMs, &d.Error, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan delivery: %w", err)
		}

		d.Success = success == 1
		if statusCode.Valid {
			sc := int(statusCode.Int64)
			d.StatusCode = &sc
		}
		if latencyMs.Valid {
			lm := int(latencyMs.Int64)
			d.LatencyMs = &lm
		}

		deliveries = append(deliveries, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	if deliveries == nil {
		return []WebhookDelivery{}, nil
	}
	return deliveries, nil
}

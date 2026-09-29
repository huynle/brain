package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
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
func (s *TenantStore) CreateWebhook(ctx context.Context, wh *Webhook) error {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return err
	}
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

	columns, values := "id, name, url, events, filter, secret, enabled, created_at, updated_at", "?, ?, ?, ?, ?, ?, ?, ?, ?"
	args := []interface{}{
		wh.ID, wh.Name, wh.URL, string(eventsJSON), string(filterJSON),
		wh.Secret, enabled, wh.CreatedAt, wh.UpdatedAt,
	}
	if scope.owner != "" {
		columns += ", tenant_id"
		values += ", ?"
		args = append(args, scope.owner)
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO webhooks ("+columns+") VALUES ("+values+")", args...)
	if err != nil {
		return fmt.Errorf("insert webhook: %w", err)
	}
	return nil
}

// GetWebhook retrieves a webhook by ID.
// ErrWebhookNotFound is returned by GetWebhook when no row matches. It exists
// because the previous bare fmt.Errorf("webhook not found: %s") could only be
// recognised by string matching — which exactly one caller did
// (WebhookServiceImpl.TestDeliver), leaving every other caller unable to tell a
// missing webhook from a database failure. The API handler for webhook_get has
// carried an ErrNotFound -> 404 branch the whole time; without a sentinel to
// match, it was unreachable and a missing webhook returned 500.
var ErrWebhookNotFound = errors.New("webhook not found")

func (s *TenantStore) GetWebhook(ctx context.Context, id string) (*Webhook, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	where, args := scope.where("id = ?", id)
	var wh Webhook
	var eventsJSON, filterJSON string
	var enabled int

	err = s.db.QueryRowContext(ctx,
		`SELECT id, name, url, events, filter, secret, enabled, created_at, updated_at
		 FROM webhooks WHERE `+where, args...,
	).Scan(&wh.ID, &wh.Name, &wh.URL, &eventsJSON, &filterJSON,
		&wh.Secret, &enabled, &wh.CreatedAt, &wh.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("webhook %s: %w", id, ErrWebhookNotFound)
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
func (s *TenantStore) ListWebhooks(ctx context.Context, enabledOnly ...bool) ([]Webhook, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	where, args := scope.where("1=1")
	query := `SELECT id, name, url, events, filter, secret, enabled, created_at, updated_at FROM webhooks WHERE ` + where
	if len(enabledOnly) > 0 && enabledOnly[0] {
		query += " AND enabled = 1"
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
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
func (s *TenantStore) UpdateWebhook(ctx context.Context, wh *Webhook) error {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return err
	}
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

	where, keys := scope.where("id = ?", wh.ID)
	args := []interface{}{wh.Name, wh.URL, string(eventsJSON), string(filterJSON), wh.Secret, enabled, wh.UpdatedAt}
	args = append(args, keys...)
	result, err := s.db.ExecContext(ctx,
		`UPDATE webhooks SET name = ?, url = ?, events = ?, filter = ?, secret = ?,
		 enabled = ?, updated_at = ? WHERE `+where, args...,
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
func (s *TenantStore) DeleteWebhook(ctx context.Context, id string) error {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return err
	}
	where, args := scope.where("id = ?", id)
	result, err := s.db.ExecContext(ctx, "DELETE FROM webhooks WHERE "+where, args...)
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
func (s *TenantStore) CreateDelivery(ctx context.Context, d *WebhookDelivery) error {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return err
	}
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

	columns, values := "id, webhook_id, event_type, status_code, success, latency_ms, error, created_at", "?, ?, ?, ?, ?, ?, ?, ?"
	args := []interface{}{
		d.ID, d.WebhookID, d.EventType, d.StatusCode, success, d.LatencyMs, d.Error, d.CreatedAt,
	}
	if scope.owner != "" {
		columns += ", tenant_id"
		values += ", ?"
		args = append(args, scope.owner)
	}
	// The composite foreign key enforces parent ownership atomically, including
	// a concurrent parent deletion. Delivery IDs are unique only within a tenant.
	_, err = s.db.ExecContext(ctx, "INSERT INTO webhook_deliveries ("+columns+") VALUES ("+values+")", args...)
	if err != nil {
		return fmt.Errorf("insert delivery: %w", err)
	}
	return nil
}

// ListDeliveries returns deliveries for a webhook, ordered by most recent first.
func (s *TenantStore) ListDeliveries(ctx context.Context, webhookID string, limit int) ([]WebhookDelivery, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}

	where, args := scope.where("webhook_id = ?", webhookID)
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, webhook_id, event_type, status_code, success, latency_ms, COALESCE(error, ''), created_at
		 FROM webhook_deliveries WHERE `+where+` ORDER BY created_at DESC LIMIT ?`, args...,
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

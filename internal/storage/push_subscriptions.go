package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// push_subscriptions stores browser Web Push endpoints per (tenant, recipient).
// The unique key is (tenant, recipient, endpoint) so a device re-registering
// with rotated keys updates in place rather than accumulating stale rows.
const createPushSubscriptionsTable = `CREATE TABLE IF NOT EXISTS push_subscriptions (
 tenant_id TEXT NOT NULL, id TEXT NOT NULL, recipient TEXT NOT NULL,
 endpoint TEXT NOT NULL, p256dh TEXT NOT NULL, auth TEXT NOT NULL,
 created_at TEXT NOT NULL,
 PRIMARY KEY(tenant_id,recipient,endpoint)
)`

// UpsertPushSubscription inserts or updates a device subscription by endpoint.
func (s *TenantStore) UpsertPushSubscription(ctx context.Context, sub types.PushSubscription) error {
	scope, err := s.attentionScope(ctx)
	if err != nil {
		return err
	}
	if sub.Recipient == "" || sub.Endpoint == "" || sub.P256dh == "" || sub.Auth == "" {
		return fmt.Errorf("push subscription requires recipient, endpoint, p256dh, auth")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx, `INSERT INTO push_subscriptions
 (tenant_id,id,recipient,endpoint,p256dh,auth,created_at) VALUES(?,?,?,?,?,?,?)
 ON CONFLICT(tenant_id,recipient,endpoint) DO UPDATE SET p256dh=excluded.p256dh, auth=excluded.auth`,
		scope, sub.ID, sub.Recipient, sub.Endpoint, sub.P256dh, sub.Auth, now)
	return err
}

// ListPushSubscriptions returns a recipient's registered devices.
func (s *TenantStore) ListPushSubscriptions(ctx context.Context, recipient string) ([]types.PushSubscription, error) {
	scope, err := s.attentionScope(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,recipient,endpoint,p256dh,auth,created_at
 FROM push_subscriptions WHERE tenant_id=? AND recipient=? ORDER BY created_at`, scope, recipient)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.PushSubscription
	for rows.Next() {
		var p types.PushSubscription
		if err := rows.Scan(&p.ID, &p.Recipient, &p.Endpoint, &p.P256dh, &p.Auth, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeletePushSubscription removes a device by endpoint. Used both on explicit
// unsubscribe and when a push endpoint reports 404/410 (gone).
func (s *TenantStore) DeletePushSubscription(ctx context.Context, recipient, endpoint string) error {
	scope, err := s.attentionScope(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE tenant_id=? AND recipient=? AND endpoint=?`,
		scope, recipient, endpoint)
	return err
}

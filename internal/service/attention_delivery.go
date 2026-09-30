package service

import (
	"context"
	"encoding/json"

	"github.com/huynle/brain-api/internal/types"
)

// AttentionDeliveryProvider delivers an attention item to a recipient over some
// out-of-band channel (Web Push, generic webhook, and — later — Telegram or
// Discord). Providers are best-effort: the durable inbox is the source of
// truth, so a delivery failure never blocks or reverses the item's creation.
//
// A provider returns nil when it delivered or had nothing to do for this
// recipient (e.g. no subscriptions). It returns an error only for a genuine
// send failure, which the dispatcher logs.
type AttentionDeliveryProvider interface {
	Name() string
	Deliver(ctx context.Context, item *types.Attention) error
}

// attentionPushPayload is the JSON the browser service worker receives. It is
// intentionally small: enough to render a notification and deep-link back into
// the inbox item, not the full record.
type attentionPushPayload struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	Severity  string `json:"severity"`
	Kind      string `json:"kind"`
	Project   string `json:"project,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	FeatureID string `json:"feature_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

// buildAttentionPushPayload projects an attention item into the compact push
// message. Kept pure and separate so it is unit-testable without a push service.
func buildAttentionPushPayload(item *types.Attention) ([]byte, error) {
	p := attentionPushPayload{
		ID: item.ID, Title: item.Title, Body: item.Body, Severity: item.Severity,
		Kind: item.Kind, Project: item.Project, TaskID: item.TaskID,
		FeatureID: item.FeatureID, SessionID: item.SessionID,
	}
	return json.Marshal(p)
}

// pushEndpointGone reports whether a push service HTTP status means the
// subscription is permanently invalid and should be deleted. 404 Not Found and
// 410 Gone are the RFC 8030 signals for an expired/unsubscribed endpoint.
func pushEndpointGone(status int) bool {
	return status == 404 || status == 410
}

package service

import (
	"context"
	"io"
	"log/slog"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/huynle/brain-api/internal/types"
)

// pushSubscriptionStore is the slice of TenantStore the Web Push provider needs.
type pushSubscriptionStore interface {
	ListPushSubscriptions(ctx context.Context, recipient string) ([]types.PushSubscription, error)
	DeletePushSubscription(ctx context.Context, recipient, endpoint string) error
}

// WebPushProvider delivers attention items to a recipient's registered browser
// endpoints using VAPID-authenticated Web Push, so a notification arrives even
// when every Brain tab is closed. With no VAPID keypair configured it is inert
// (Deliver is a no-op), leaving the in-app inbox and webhooks unaffected.
type WebPushProvider struct {
	store       pushSubscriptionStore
	publicKey   string
	privateKey  string
	subscriber  string
	ttlSeconds  int
	now         func() time.Time
}

// NewWebPushProvider builds the provider. When publicKey/privateKey are empty
// the provider still constructs but Deliver does nothing, so callers need not
// branch on configuration.
func NewWebPushProvider(store pushSubscriptionStore, publicKey, privateKey, subscriber string) *WebPushProvider {
	return &WebPushProvider{
		store: store, publicKey: publicKey, privateKey: privateKey,
		subscriber: subscriber, ttlSeconds: 86400, now: time.Now,
	}
}

// Name identifies the provider in logs.
func (p *WebPushProvider) Name() string { return "webpush" }

// Configured reports whether a VAPID keypair is present.
func (p *WebPushProvider) Configured() bool {
	return p.publicKey != "" && p.privateKey != ""
}

// Deliver pushes the item to each of the recipient's subscriptions. Endpoints
// that report 404/410 (gone) are pruned. Other per-endpoint failures are logged
// and skipped; one bad device never blocks the others.
func (p *WebPushProvider) Deliver(ctx context.Context, item *types.Attention) error {
	if !p.Configured() || item == nil || item.Recipient == "" {
		return nil
	}
	subs, err := p.store.ListPushSubscriptions(ctx, item.Recipient)
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		return nil
	}
	payload, err := buildAttentionPushPayload(item)
	if err != nil {
		return err
	}
	for _, sub := range subs {
		s := &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
		}
		resp, err := webpush.SendNotificationWithContext(ctx, payload, s, &webpush.Options{
			Subscriber:      p.subscriber,
			VAPIDPublicKey:  p.publicKey,
			VAPIDPrivateKey: p.privateKey,
			TTL:             p.ttlSeconds,
		})
		if err != nil {
			slog.Warn("webpush: send failed", "recipient", item.Recipient, "error", err)
			continue
		}
		status := resp.StatusCode
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if pushEndpointGone(status) {
			// Prune a dead device so it stops being retried forever.
			if delErr := p.store.DeletePushSubscription(ctx, item.Recipient, sub.Endpoint); delErr != nil {
				slog.Warn("webpush: prune gone endpoint failed", "error", delErr)
			}
			continue
		}
		if status >= 300 {
			slog.Warn("webpush: non-2xx from push service", "status", status, "recipient", item.Recipient)
		}
	}
	return nil
}

// static assertion.
var _ AttentionDeliveryProvider = (*WebPushProvider)(nil)

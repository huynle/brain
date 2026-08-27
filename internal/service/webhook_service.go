package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	api "github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// retryDelays defines the backoff intervals between delivery attempts.
// 3 attempts: immediate, then 1s, then 5s between retries.
var retryDelays = []time.Duration{1 * time.Second, 5 * time.Second}

// maxRetries is the total number of delivery attempts (1 initial + retries).
const maxRetries = 3

// WebhookServiceImpl implements the api.WebhookService interface.
type WebhookServiceImpl struct {
	store  *storage.StorageLayer
	client *http.Client
}

// NewWebhookService creates a new WebhookServiceImpl.
func NewWebhookService(store *storage.StorageLayer) *WebhookServiceImpl {
	return &WebhookServiceImpl{
		store: store,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// NewWebhookServiceWithClient creates a WebhookServiceImpl with a custom HTTP client.
// Useful for testing.
func NewWebhookServiceWithClient(store *storage.StorageLayer, client *http.Client) *WebhookServiceImpl {
	return &WebhookServiceImpl{
		store:  store,
		client: client,
	}
}

// Create registers a new webhook.
func (s *WebhookServiceImpl) Create(ctx context.Context, req types.CreateWebhookRequest) (*types.WebhookResponse, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.URL == "" {
		return nil, fmt.Errorf("url is required")
	}
	if len(req.Events) == 0 {
		return nil, fmt.Errorf("at least one event pattern is required")
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	// Convert string filter to interface{} filter for storage
	filter := make(map[string]interface{})
	for k, v := range req.Filter {
		filter[k] = v
	}

	wh := &storage.Webhook{
		Name:    req.Name,
		URL:     req.URL,
		Events:  req.Events,
		Filter:  filter,
		Secret:  req.Secret,
		Enabled: enabled,
	}

	if err := s.store.CreateWebhook(ctx, wh); err != nil {
		return nil, fmt.Errorf("create webhook: %w", err)
	}

	return webhookToResponse(wh), nil
}

// Get returns a webhook by ID.
func (s *WebhookServiceImpl) Get(ctx context.Context, id string) (*types.WebhookResponse, error) {
	wh, err := s.store.GetWebhook(ctx, id)
	if err != nil {
		return nil, translateWebhookNotFound(err)
	}
	return webhookToResponse(wh), nil
}

// translateWebhookNotFound maps the storage sentinel onto the API's, so the
// not-found branches the handlers already carry become reachable. Before this,
// a missing webhook surfaced as a 500 — "server fault, try again" — for what is
// an ordinary client mistake.
func translateWebhookNotFound(err error) error {
	if errors.Is(err, storage.ErrWebhookNotFound) {
		return api.ErrNotFound
	}
	return err
}

// List returns all webhooks, optionally filtered by enabled status.
func (s *WebhookServiceImpl) List(ctx context.Context, enabledOnly bool) ([]types.WebhookResponse, error) {
	webhooks, err := s.store.ListWebhooks(ctx, enabledOnly)
	if err != nil {
		return nil, err
	}

	result := make([]types.WebhookResponse, len(webhooks))
	for i, wh := range webhooks {
		result[i] = *webhookToResponse(&wh)
	}
	return result, nil
}

// Update modifies an existing webhook.
func (s *WebhookServiceImpl) Update(ctx context.Context, id string, req types.UpdateWebhookRequest) (*types.WebhookResponse, error) {
	wh, err := s.store.GetWebhook(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		wh.Name = *req.Name
	}
	if req.URL != nil {
		wh.URL = *req.URL
	}
	if req.Events != nil {
		wh.Events = req.Events
	}
	if req.Filter != nil {
		filter := make(map[string]interface{})
		for k, v := range req.Filter {
			filter[k] = v
		}
		wh.Filter = filter
	}
	if req.Secret != nil {
		wh.Secret = *req.Secret
	}
	if req.Enabled != nil {
		wh.Enabled = *req.Enabled
	}

	if err := s.store.UpdateWebhook(ctx, wh); err != nil {
		return nil, err
	}

	return webhookToResponse(wh), nil
}

// Delete removes a webhook by ID.
func (s *WebhookServiceImpl) Delete(ctx context.Context, id string) error {
	return s.store.DeleteWebhook(ctx, id)
}

// TestDeliver sends a test event to a specific webhook synchronously and
// returns the delivery result. Unlike Deliver, this targets a single webhook
// by ID and does not run asynchronously.
func (s *WebhookServiceImpl) TestDeliver(ctx context.Context, webhookID string, event types.Event) (*types.WebhookDeliveryResponse, error) {
	wh, err := s.store.GetWebhook(ctx, webhookID)
	if err != nil {
		// Was strings.Contains(err.Error(), "not found") — a match against
		// prose that any reworded message would have silently broken.
		if errors.Is(err, storage.ErrWebhookNotFound) {
			return nil, api.ErrNotFound
		}
		return nil, fmt.Errorf("get webhook: %w", err)
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}

	start := time.Now()
	statusCode, deliveryErr := s.doHTTPDelivery(ctx, wh, payload)
	latencyMs := int(time.Since(start).Milliseconds())

	success := deliveryErr == nil && statusCode >= 200 && statusCode < 300

	var errMsg string
	if deliveryErr != nil {
		errMsg = deliveryErr.Error()
	} else if !success {
		errMsg = fmt.Sprintf("HTTP %d", statusCode)
	}

	// Log the delivery attempt
	var scPtr *int
	if statusCode > 0 {
		scPtr = &statusCode
	}
	s.logDelivery(ctx, wh.ID, event.Type, scPtr, success, &latencyMs, errMsg)

	return &types.WebhookDeliveryResponse{
		ID:         fmt.Sprintf("test_%s_%d", webhookID, time.Now().UnixMilli()),
		WebhookID:  webhookID,
		EventType:  event.Type,
		StatusCode: scPtr,
		Success:    success,
		LatencyMs:  &latencyMs,
		Error:      errMsg,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// Deliver sends an event to all matching enabled webhooks.
func (s *WebhookServiceImpl) Deliver(ctx context.Context, event types.Event) error {
	webhooks, err := s.store.ListWebhooks(ctx, true) // enabled only
	if err != nil {
		return fmt.Errorf("list webhooks: %w", err)
	}

	for _, wh := range webhooks {
		if !s.matchesWebhook(&wh, event) {
			continue
		}
		// Deliver asynchronously to not block the caller
		go s.deliverToWebhook(context.Background(), &wh, event)
	}

	return nil
}

// ListDeliveries returns recent delivery attempts for a webhook.
func (s *WebhookServiceImpl) ListDeliveries(ctx context.Context, webhookID string, limit int) ([]types.WebhookDeliveryResponse, error) {
	// Confirm the webhook exists before reporting on its deliveries. The
	// deliveries query is a bare `WHERE webhook_id = ?` with no join, so a
	// deleted or mistyped id returned zero rows and rendered "No deliveries
	// recorded for webhook X" — indistinguishable from a live webhook that has
	// never fired, which is exactly the state someone debugging a webhook is
	// trying to tell apart.
	if _, err := s.store.GetWebhook(ctx, webhookID); err != nil {
		return nil, translateWebhookNotFound(err)
	}

	deliveries, err := s.store.ListDeliveries(ctx, webhookID, limit)
	if err != nil {
		return nil, err
	}

	result := make([]types.WebhookDeliveryResponse, len(deliveries))
	for i, d := range deliveries {
		result[i] = types.WebhookDeliveryResponse{
			ID:         d.ID,
			WebhookID:  d.WebhookID,
			EventType:  d.EventType,
			StatusCode: d.StatusCode,
			Success:    d.Success,
			LatencyMs:  d.LatencyMs,
			Error:      d.Error,
			CreatedAt:  d.CreatedAt,
		}
	}
	return result, nil
}

// =============================================================================
// Internal Methods
// =============================================================================

// matchesWebhook checks if an event matches a webhook's event patterns and filters.
func (s *WebhookServiceImpl) matchesWebhook(wh *storage.Webhook, event types.Event) bool {
	// Check event pattern matching
	matched := false
	for _, pattern := range wh.Events {
		if types.MatchEventPattern(pattern, event.Type) {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}

	// Check filter matching
	if len(wh.Filter) > 0 {
		for key, value := range wh.Filter {
			valueStr, ok := value.(string)
			if !ok {
				continue
			}
			if !matchesEventField(event, key, valueStr) {
				return false
			}
		}
	}

	return true
}

// matchesEventField checks if an event field matches the expected value.
func matchesEventField(event types.Event, field, expected string) bool {
	var actual string
	switch field {
	case "project_id":
		actual = event.ProjectID
	case "feature_id":
		actual = event.FeatureID
	case "task_id":
		actual = event.TaskID
	case "source":
		actual = event.Source
	case "runner_id":
		actual = event.RunnerID
	default:
		// Check metadata for custom fields
		if event.Metadata != nil {
			actual = event.Metadata[field]
		}
	}
	return actual == expected
}

// deliverToWebhook sends an event to a single webhook with retry logic.
func (s *WebhookServiceImpl) deliverToWebhook(ctx context.Context, wh *storage.Webhook, event types.Event) {
	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("[webhook] failed to marshal event for webhook %s: %v", wh.ID, err)
		return
	}

	var lastErr error
	attempts := 0
retry:
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := retryDelays[attempt-1]
			select {
			case <-ctx.Done():
				// Labeled: a bare `break` leaves only the select, so a
				// cancelled context fell through into the next delivery
				// attempt instead of ending the loop.
				//
				// No test covers this, deliberately: the only caller of
				// deliverToWebhook detaches into context.Background() on
				// purpose (so an HTTP handler returning cannot kill an
				// in-flight delivery), which leaves this path dormant, and
				// the fall-through attempts were unobservable anyway — they
				// die inside the HTTP client without reaching the endpoint,
				// and logDelivery's write uses the same dead context so
				// nothing reaches the delivery log either.
				lastErr = ctx.Err()
				break retry
			case <-time.After(delay):
			}
		}

		attempts++
		start := time.Now()
		statusCode, deliveryErr := s.doHTTPDelivery(ctx, wh, payload)
		latencyMs := int(time.Since(start).Milliseconds())

		if deliveryErr == nil && statusCode >= 200 && statusCode < 300 {
			// Success - log and return
			s.logDelivery(ctx, wh.ID, event.Type, &statusCode, true, &latencyMs, "")
			return
		}

		errMsg := ""
		if deliveryErr != nil {
			errMsg = deliveryErr.Error()
		} else {
			errMsg = fmt.Sprintf("HTTP %d", statusCode)
		}
		lastErr = fmt.Errorf("attempt %d: %s", attempt+1, errMsg)

		// Log failed attempt (only the final one as a permanent failure)
		if attempt == maxRetries-1 {
			sc := statusCode
			var scPtr *int
			if sc > 0 {
				scPtr = &sc
			}
			s.logDelivery(ctx, wh.ID, event.Type, scPtr, false, &latencyMs, lastErr.Error())
		}
	}

	if lastErr != nil {
		log.Printf("[webhook] delivery to %s failed after %d attempts: %v", wh.URL, attempts, lastErr)
	}
}

// doHTTPDelivery performs a single HTTP POST to the webhook URL.
func (s *WebhookServiceImpl) doHTTPDelivery(ctx context.Context, wh *storage.Webhook, payload []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Brain-Webhook/1.0")

	// Add HMAC-SHA256 signature if secret is configured
	if wh.Secret != "" {
		sig := ComputeHMACSHA256(payload, wh.Secret)
		req.Header.Set("X-Brain-Signature", sig)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()
	// Drain body to allow connection reuse
	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode, nil
}

// ComputeHMACSHA256 computes an HMAC-SHA256 signature for the given payload
// using the provided secret. Returns the hex-encoded signature prefixed with "sha256=".
func ComputeHMACSHA256(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// logDelivery records a delivery attempt in the database.
func (s *WebhookServiceImpl) logDelivery(ctx context.Context, webhookID, eventType string, statusCode *int, success bool, latencyMs *int, errMsg string) {
	d := &storage.WebhookDelivery{
		WebhookID:  webhookID,
		EventType:  eventType,
		StatusCode: statusCode,
		Success:    success,
		LatencyMs:  latencyMs,
		Error:      errMsg,
	}
	if err := s.store.CreateDelivery(ctx, d); err != nil {
		log.Printf("[webhook] failed to log delivery for webhook %s: %v", webhookID, err)
	}
}

// =============================================================================
// Conversion Helpers
// =============================================================================

// webhookToResponse converts a storage.Webhook to a types.WebhookResponse.
func webhookToResponse(wh *storage.Webhook) *types.WebhookResponse {
	filter := make(map[string]string)
	for k, v := range wh.Filter {
		if s, ok := v.(string); ok {
			filter[k] = s
		}
	}

	return &types.WebhookResponse{
		ID:        wh.ID,
		Name:      wh.Name,
		URL:       wh.URL,
		Events:    wh.Events,
		Filter:    filter,
		Enabled:   wh.Enabled,
		CreatedAt: wh.CreatedAt,
		UpdatedAt: wh.UpdatedAt,
	}
}

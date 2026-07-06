package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Mock WebhookService
// =============================================================================

type mockWebhookService struct {
	createFunc         func(ctx context.Context, req types.CreateWebhookRequest) (*types.WebhookResponse, error)
	getFunc            func(ctx context.Context, id string) (*types.WebhookResponse, error)
	listFunc           func(ctx context.Context, enabledOnly bool) ([]types.WebhookResponse, error)
	updateFunc         func(ctx context.Context, id string, req types.UpdateWebhookRequest) (*types.WebhookResponse, error)
	deleteFunc         func(ctx context.Context, id string) error
	deliverFunc        func(ctx context.Context, event types.Event) error
	listDeliveriesFunc func(ctx context.Context, webhookID string, limit int) ([]types.WebhookDeliveryResponse, error)
	testDeliverFunc    func(ctx context.Context, webhookID string, event types.Event) (*types.WebhookDeliveryResponse, error)
}

func (m *mockWebhookService) Create(ctx context.Context, req types.CreateWebhookRequest) (*types.WebhookResponse, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, req)
	}
	return &types.WebhookResponse{ID: "wh_test", Name: req.Name, URL: req.URL, Events: req.Events, Enabled: true}, nil
}

func (m *mockWebhookService) Get(ctx context.Context, id string) (*types.WebhookResponse, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return nil, ErrNotFound
}

func (m *mockWebhookService) List(ctx context.Context, enabledOnly bool) ([]types.WebhookResponse, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, enabledOnly)
	}
	return []types.WebhookResponse{}, nil
}

func (m *mockWebhookService) Update(ctx context.Context, id string, req types.UpdateWebhookRequest) (*types.WebhookResponse, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, id, req)
	}
	return nil, ErrNotFound
}

func (m *mockWebhookService) Delete(ctx context.Context, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

func (m *mockWebhookService) Deliver(ctx context.Context, event types.Event) error {
	if m.deliverFunc != nil {
		return m.deliverFunc(ctx, event)
	}
	return nil
}

func (m *mockWebhookService) ListDeliveries(ctx context.Context, webhookID string, limit int) ([]types.WebhookDeliveryResponse, error) {
	if m.listDeliveriesFunc != nil {
		return m.listDeliveriesFunc(ctx, webhookID, limit)
	}
	return []types.WebhookDeliveryResponse{}, nil
}

func (m *mockWebhookService) TestDeliver(ctx context.Context, webhookID string, event types.Event) (*types.WebhookDeliveryResponse, error) {
	if m.testDeliverFunc != nil {
		return m.testDeliverFunc(ctx, webhookID, event)
	}
	return nil, ErrNotFound
}

// =============================================================================
// Test Helpers
// =============================================================================

func newWebhookTestRouter(mock *mockWebhookService) *chi.Mux {
	h := NewHandler(&mockBrainService{}, WithWebhookService(mock))
	r := chi.NewRouter()
	r.Post("/webhooks", h.HandleCreateWebhook)
	r.Get("/webhooks", h.HandleListWebhooks)
	r.Get("/webhooks/{id}", h.HandleGetWebhook)
	r.Patch("/webhooks/{id}", h.HandleUpdateWebhook)
	r.Delete("/webhooks/{id}", h.HandleDeleteWebhook)
	r.Get("/webhooks/{id}/deliveries", h.HandleListWebhookDeliveries)
	r.Post("/webhooks/{id}/test", h.HandleTestWebhook)
	return r
}

func postWebhook(t *testing.T, router *chi.Mux, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/webhooks", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// =============================================================================
// HandleCreateWebhook Tests
// =============================================================================

func TestHandleCreateWebhook_MissingName_Returns400(t *testing.T) {
	router := newWebhookTestRouter(&mockWebhookService{})
	w := postWebhook(t, router, map[string]any{
		"url":    "https://example.com/hook",
		"events": []string{"task.completed"},
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	// The API returns validation errors under the "details" key.
	details, ok := resp["details"].([]any)
	if !ok || len(details) == 0 {
		t.Fatalf("expected details array, got: %v", resp)
	}

	found := false
	for _, e := range details {
		errMap, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if errMap["field"] == "name" && errMap["message"] == "required" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected detail with field=name and message=required, got: %v", details)
	}
}

func TestHandleCreateWebhook_MissingURL_Returns400(t *testing.T) {
	router := newWebhookTestRouter(&mockWebhookService{})
	w := postWebhook(t, router, map[string]any{
		"name":   "my-hook",
		"events": []string{"task.completed"},
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	details, ok := resp["details"].([]any)
	if !ok || len(details) == 0 {
		t.Fatalf("expected details array, got: %v", resp)
	}

	found := false
	for _, e := range details {
		errMap, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if errMap["field"] == "url" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected detail with field=url, got: %v", details)
	}
}

func TestHandleCreateWebhook_ValidPayload_Returns201(t *testing.T) {
	router := newWebhookTestRouter(&mockWebhookService{})
	w := postWebhook(t, router, map[string]any{
		"name":   "my-hook",
		"url":    "https://example.com/hook",
		"events": []string{"task.completed"},
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d; body: %s", w.Code, w.Body.String())
	}
}

// =============================================================================
// HandleTestWebhook Tests
// =============================================================================

func TestHandleTestWebhook_DeliversTestEvent(t *testing.T) {
	var capturedEvent types.Event
	var capturedWebhookID string

	mock := &mockWebhookService{
		testDeliverFunc: func(ctx context.Context, webhookID string, event types.Event) (*types.WebhookDeliveryResponse, error) {
			capturedWebhookID = webhookID
			capturedEvent = event
			sc := 200
			lat := 42
			return &types.WebhookDeliveryResponse{
				ID:         "del_test123",
				WebhookID:  webhookID,
				EventType:  event.Type,
				StatusCode: &sc,
				Success:    true,
				LatencyMs:  &lat,
				CreatedAt:  "2025-01-01T00:00:00Z",
			}, nil
		},
	}

	router := newWebhookTestRouter(mock)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/wh_abc123/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify the webhook ID was passed correctly
	if capturedWebhookID != "wh_abc123" {
		t.Errorf("expected webhook ID 'wh_abc123', got %q", capturedWebhookID)
	}

	// Verify the event is a test event
	if capturedEvent.Type != "webhook.test" {
		t.Errorf("expected event type 'webhook.test', got %q", capturedEvent.Type)
	}
	if capturedEvent.Source != "api" {
		t.Errorf("expected event source 'api', got %q", capturedEvent.Source)
	}

	// Verify response body contains delivery result
	var resp types.WebhookDeliveryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Success {
		t.Error("expected successful delivery in response")
	}
	if resp.WebhookID != "wh_abc123" {
		t.Errorf("expected webhook_id 'wh_abc123' in response, got %q", resp.WebhookID)
	}
}

func TestHandleTestWebhook_NonexistentWebhook_Returns404(t *testing.T) {
	mock := &mockWebhookService{
		testDeliverFunc: func(ctx context.Context, webhookID string, event types.Event) (*types.WebhookDeliveryResponse, error) {
			return nil, ErrNotFound
		},
	}

	router := newWebhookTestRouter(mock)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/wh_nonexistent/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleTestWebhook_DeliveryFailure_Returns200WithError(t *testing.T) {
	mock := &mockWebhookService{
		testDeliverFunc: func(ctx context.Context, webhookID string, event types.Event) (*types.WebhookDeliveryResponse, error) {
			sc := 502
			lat := 5000
			return &types.WebhookDeliveryResponse{
				ID:         "del_fail456",
				WebhookID:  webhookID,
				EventType:  event.Type,
				StatusCode: &sc,
				Success:    false,
				LatencyMs:  &lat,
				Error:      "HTTP 502",
				CreatedAt:  "2025-01-01T00:00:00Z",
			}, nil
		},
	}

	router := newWebhookTestRouter(mock)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/wh_abc123/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Delivery failure returns 200 with delivery record showing error
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 (delivery result even on webhook failure), got %d; body: %s", w.Code, w.Body.String())
	}

	var resp types.WebhookDeliveryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Success {
		t.Error("expected unsuccessful delivery in response")
	}
	if resp.Error == "" {
		t.Error("expected error message in delivery response")
	}
}

func TestHandleTestWebhook_NilWebhookService_Returns501(t *testing.T) {
	h := NewHandler(&mockBrainService{})
	r := chi.NewRouter()
	r.Post("/webhooks/{id}/test", h.HandleTestWebhook)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/wh_abc123/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected status 501, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateWebhook_MissingAllFields_Returns400WithMultipleErrors(t *testing.T) {
	router := newWebhookTestRouter(&mockWebhookService{})
	w := postWebhook(t, router, map[string]any{})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	details, ok := resp["details"].([]any)
	if !ok || len(details) < 3 {
		t.Fatalf("expected at least 3 details (name, url, events), got: %v", resp)
	}
}

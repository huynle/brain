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

package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"

	_ "github.com/glebarez/go-sqlite"
)

// newTestWebhookService creates a WebhookServiceImpl with an in-memory DB.
func newTestWebhookService(t *testing.T) *WebhookServiceImpl {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	store, err := storage.NewWithDB(db)
	if err != nil {
		t.Fatalf("NewWithDB failed: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return NewWebhookService(store)
}

// newTestWebhookServiceWithServer creates a service with a test HTTP server.
func newTestWebhookServiceWithServer(t *testing.T, handler http.HandlerFunc) (*WebhookServiceImpl, *httptest.Server) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	store, err := storage.NewWithDB(db)
	if err != nil {
		t.Fatalf("NewWithDB failed: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	svc := NewWebhookServiceWithClient(store, server.Client())
	return svc, server
}

// ---------------------------------------------------------------------------
// CRUD Tests
// ---------------------------------------------------------------------------

func TestWebhookService_Create_Success(t *testing.T) {
	svc := newTestWebhookService(t)
	ctx := context.Background()

	resp, err := svc.Create(ctx, types.CreateWebhookRequest{
		Name:   "test-hook",
		URL:    "https://example.com/hook",
		Events: []string{"task.completed", "task.*"},
		Filter: map[string]string{"project_id": "brain-api"},
		Secret: "my-secret",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if resp.ID == "" {
		t.Fatal("expected auto-generated ID")
	}
	if resp.Name != "test-hook" {
		t.Errorf("Name = %q, want %q", resp.Name, "test-hook")
	}
	if resp.URL != "https://example.com/hook" {
		t.Errorf("URL = %q, want %q", resp.URL, "https://example.com/hook")
	}
	if len(resp.Events) != 2 {
		t.Errorf("Events len = %d, want 2", len(resp.Events))
	}
	if resp.Filter["project_id"] != "brain-api" {
		t.Errorf("Filter[project_id] = %q, want %q", resp.Filter["project_id"], "brain-api")
	}
	if !resp.Enabled {
		t.Error("expected Enabled to default to true")
	}
}

func TestWebhookService_Create_Validation(t *testing.T) {
	svc := newTestWebhookService(t)
	ctx := context.Background()

	tests := []struct {
		name string
		req  types.CreateWebhookRequest
	}{
		{"missing name", types.CreateWebhookRequest{URL: "https://example.com", Events: []string{"task.*"}}},
		{"missing url", types.CreateWebhookRequest{Name: "hook", Events: []string{"task.*"}}},
		{"missing events", types.CreateWebhookRequest{Name: "hook", URL: "https://example.com"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Create(ctx, tt.req)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

func TestWebhookService_Create_ExplicitDisabled(t *testing.T) {
	svc := newTestWebhookService(t)
	ctx := context.Background()

	disabled := false
	resp, err := svc.Create(ctx, types.CreateWebhookRequest{
		Name:    "disabled-hook",
		URL:     "https://example.com/hook",
		Events:  []string{"task.*"},
		Enabled: &disabled,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if resp.Enabled {
		t.Error("expected Enabled to be false")
	}
}

func TestWebhookService_Get_Success(t *testing.T) {
	svc := newTestWebhookService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, types.CreateWebhookRequest{
		Name:   "get-hook",
		URL:    "https://example.com/hook",
		Events: []string{"task.completed"},
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
	if got.Name != "get-hook" {
		t.Errorf("Name = %q, want %q", got.Name, "get-hook")
	}
}

func TestWebhookService_Get_NotFound(t *testing.T) {
	svc := newTestWebhookService(t)
	ctx := context.Background()

	_, err := svc.Get(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent webhook")
	}
}

func TestWebhookService_List(t *testing.T) {
	svc := newTestWebhookService(t)
	ctx := context.Background()

	for _, name := range []string{"hook1", "hook2"} {
		_, err := svc.Create(ctx, types.CreateWebhookRequest{
			Name:   name,
			URL:    "https://example.com/" + name,
			Events: []string{"task.*"},
		})
		if err != nil {
			t.Fatalf("Create(%s) failed: %v", name, err)
		}
	}

	// Create a disabled one
	disabled := false
	_, err := svc.Create(ctx, types.CreateWebhookRequest{
		Name:    "disabled",
		URL:     "https://example.com/disabled",
		Events:  []string{"task.*"},
		Enabled: &disabled,
	})
	if err != nil {
		t.Fatalf("Create(disabled) failed: %v", err)
	}

	// List all
	all, err := svc.List(ctx, false)
	if err != nil {
		t.Fatalf("List(all) failed: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("List(all) = %d, want 3", len(all))
	}

	// List enabled only
	enabled, err := svc.List(ctx, true)
	if err != nil {
		t.Fatalf("List(enabledOnly) failed: %v", err)
	}
	if len(enabled) != 2 {
		t.Errorf("List(enabledOnly) = %d, want 2", len(enabled))
	}
}

func TestWebhookService_Update(t *testing.T) {
	svc := newTestWebhookService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, types.CreateWebhookRequest{
		Name:   "update-me",
		URL:    "https://example.com/original",
		Events: []string{"task.completed"},
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	newName := "updated-name"
	newURL := "https://example.com/updated"
	disabled := false
	updated, err := svc.Update(ctx, created.ID, types.UpdateWebhookRequest{
		Name:    &newName,
		URL:     &newURL,
		Events:  []string{"task.*"},
		Enabled: &disabled,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.Name != "updated-name" {
		t.Errorf("Name = %q, want %q", updated.Name, "updated-name")
	}
	if updated.URL != "https://example.com/updated" {
		t.Errorf("URL = %q, want %q", updated.URL, "https://example.com/updated")
	}
	if len(updated.Events) != 1 || updated.Events[0] != "task.*" {
		t.Errorf("Events = %v, want [task.*]", updated.Events)
	}
	if updated.Enabled {
		t.Error("expected Enabled to be false")
	}
}

func TestWebhookService_Delete(t *testing.T) {
	svc := newTestWebhookService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, types.CreateWebhookRequest{
		Name:   "delete-me",
		URL:    "https://example.com/hook",
		Events: []string{"task.*"},
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = svc.Get(ctx, created.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

// ---------------------------------------------------------------------------
// HMAC Signing Tests
// ---------------------------------------------------------------------------

func TestComputeHMACSHA256(t *testing.T) {
	payload := []byte(`{"type":"task.completed","task_id":"abc123"}`)
	secret := "my-secret-key"

	sig := ComputeHMACSHA256(payload, secret)

	// Verify format
	if sig[:7] != "sha256=" {
		t.Errorf("signature should start with sha256=, got %q", sig[:7])
	}

	// Verify the actual HMAC
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if sig != expected {
		t.Errorf("signature = %q, want %q", sig, expected)
	}
}

func TestComputeHMACSHA256_DifferentSecrets(t *testing.T) {
	payload := []byte(`{"test":"data"}`)
	sig1 := ComputeHMACSHA256(payload, "secret1")
	sig2 := ComputeHMACSHA256(payload, "secret2")

	if sig1 == sig2 {
		t.Error("different secrets should produce different signatures")
	}
}

func TestComputeHMACSHA256_DifferentPayloads(t *testing.T) {
	secret := "same-secret"
	sig1 := ComputeHMACSHA256([]byte(`{"a":"1"}`), secret)
	sig2 := ComputeHMACSHA256([]byte(`{"a":"2"}`), secret)

	if sig1 == sig2 {
		t.Error("different payloads should produce different signatures")
	}
}

// ---------------------------------------------------------------------------
// Event Matching Tests
// ---------------------------------------------------------------------------

func TestMatchesWebhook_ExactEvent(t *testing.T) {
	svc := newTestWebhookService(t)

	wh := &storage.Webhook{
		Events:  []string{"task.completed"},
		Filter:  map[string]interface{}{},
		Enabled: true,
	}
	event := types.Event{Type: "task.completed"}

	if !svc.matchesWebhook(wh, event) {
		t.Error("expected exact event match")
	}
}

func TestMatchesWebhook_WildcardEvent(t *testing.T) {
	svc := newTestWebhookService(t)

	wh := &storage.Webhook{
		Events:  []string{"task.*"},
		Filter:  map[string]interface{}{},
		Enabled: true,
	}

	tests := []struct {
		eventType string
		want      bool
	}{
		{"task.completed", true},
		{"task.failed", true},
		{"task.started", true},
		{"feature.completed", false},
		{"runner.started", false},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			event := types.Event{Type: tt.eventType}
			got := svc.matchesWebhook(wh, event)
			if got != tt.want {
				t.Errorf("matchesWebhook(%q) = %v, want %v", tt.eventType, got, tt.want)
			}
		})
	}
}

func TestMatchesWebhook_GlobalWildcard(t *testing.T) {
	svc := newTestWebhookService(t)

	wh := &storage.Webhook{
		Events:  []string{"*"},
		Filter:  map[string]interface{}{},
		Enabled: true,
	}

	for _, eventType := range []string{"task.completed", "runner.started", "feature.progress"} {
		event := types.Event{Type: eventType}
		if !svc.matchesWebhook(wh, event) {
			t.Errorf("global wildcard should match %q", eventType)
		}
	}
}

func TestMatchesWebhook_MultiplePatterns(t *testing.T) {
	svc := newTestWebhookService(t)

	wh := &storage.Webhook{
		Events:  []string{"task.completed", "task.failed"},
		Filter:  map[string]interface{}{},
		Enabled: true,
	}

	if !svc.matchesWebhook(wh, types.Event{Type: "task.completed"}) {
		t.Error("should match first pattern")
	}
	if !svc.matchesWebhook(wh, types.Event{Type: "task.failed"}) {
		t.Error("should match second pattern")
	}
	if svc.matchesWebhook(wh, types.Event{Type: "task.started"}) {
		t.Error("should not match unlisted event")
	}
}

func TestMatchesWebhook_NoEventMatch(t *testing.T) {
	svc := newTestWebhookService(t)

	wh := &storage.Webhook{
		Events:  []string{"task.completed"},
		Filter:  map[string]interface{}{},
		Enabled: true,
	}
	event := types.Event{Type: "runner.started"}

	if svc.matchesWebhook(wh, event) {
		t.Error("should not match unrelated event type")
	}
}

// ---------------------------------------------------------------------------
// Filter Matching Tests
// ---------------------------------------------------------------------------

func TestMatchesWebhook_FilterProjectID(t *testing.T) {
	svc := newTestWebhookService(t)

	wh := &storage.Webhook{
		Events: []string{"task.*"},
		Filter: map[string]interface{}{"project_id": "brain-api"},
	}

	match := types.Event{Type: "task.completed", ProjectID: "brain-api"}
	noMatch := types.Event{Type: "task.completed", ProjectID: "other-project"}

	if !svc.matchesWebhook(wh, match) {
		t.Error("should match project_id filter")
	}
	if svc.matchesWebhook(wh, noMatch) {
		t.Error("should not match different project_id")
	}
}

func TestMatchesWebhook_FilterFeatureID(t *testing.T) {
	svc := newTestWebhookService(t)

	wh := &storage.Webhook{
		Events: []string{"task.*"},
		Filter: map[string]interface{}{"feature_id": "auth-system"},
	}

	match := types.Event{Type: "task.completed", FeatureID: "auth-system"}
	noMatch := types.Event{Type: "task.completed", FeatureID: "other-feature"}

	if !svc.matchesWebhook(wh, match) {
		t.Error("should match feature_id filter")
	}
	if svc.matchesWebhook(wh, noMatch) {
		t.Error("should not match different feature_id")
	}
}

func TestMatchesWebhook_FilterMetadata(t *testing.T) {
	svc := newTestWebhookService(t)

	wh := &storage.Webhook{
		Events: []string{"task.*"},
		Filter: map[string]interface{}{"environment": "production"},
	}

	match := types.Event{
		Type:     "task.completed",
		Metadata: map[string]string{"environment": "production"},
	}
	noMatch := types.Event{
		Type:     "task.completed",
		Metadata: map[string]string{"environment": "staging"},
	}
	noMeta := types.Event{Type: "task.completed"}

	if !svc.matchesWebhook(wh, match) {
		t.Error("should match metadata filter")
	}
	if svc.matchesWebhook(wh, noMatch) {
		t.Error("should not match different metadata value")
	}
	if svc.matchesWebhook(wh, noMeta) {
		t.Error("should not match when metadata is missing")
	}
}

func TestMatchesWebhook_MultipleFilters(t *testing.T) {
	svc := newTestWebhookService(t)

	wh := &storage.Webhook{
		Events: []string{"task.*"},
		Filter: map[string]interface{}{
			"project_id": "brain-api",
			"source":     "runner",
		},
	}

	// Both match
	both := types.Event{Type: "task.completed", ProjectID: "brain-api", Source: "runner"}
	if !svc.matchesWebhook(wh, both) {
		t.Error("should match when all filters match")
	}

	// Only one matches
	partial := types.Event{Type: "task.completed", ProjectID: "brain-api", Source: "api"}
	if svc.matchesWebhook(wh, partial) {
		t.Error("should not match when only some filters match")
	}
}

func TestMatchesWebhook_EmptyFilter(t *testing.T) {
	svc := newTestWebhookService(t)

	wh := &storage.Webhook{
		Events: []string{"task.*"},
		Filter: map[string]interface{}{},
	}

	event := types.Event{Type: "task.completed", ProjectID: "any-project"}
	if !svc.matchesWebhook(wh, event) {
		t.Error("empty filter should match any event with matching type")
	}
}

// ---------------------------------------------------------------------------
// Delivery Tests (with HTTP test server)
// ---------------------------------------------------------------------------

func TestDeliver_SuccessfulDelivery(t *testing.T) {
	var receivedBody []byte
	var receivedHeaders http.Header
	var deliveryCount int32

	svc, server := newTestWebhookServiceWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&deliveryCount, 1)
		receivedHeaders = r.Header
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	})

	ctx := context.Background()
	_, err := svc.Create(ctx, types.CreateWebhookRequest{
		Name:   "delivery-test",
		URL:    server.URL,
		Events: []string{"task.completed"},
		Secret: "test-secret",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	event := types.Event{
		ID:        "evt_test123",
		Type:      "task.completed",
		Source:    "runner",
		ProjectID: "brain-api",
		TaskID:    "abc123",
		Timestamp: time.Now(),
	}

	if err := svc.Deliver(ctx, event); err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	// Wait for async delivery
	time.Sleep(200 * time.Millisecond)

	if atomic.LoadInt32(&deliveryCount) != 1 {
		t.Errorf("delivery count = %d, want 1", deliveryCount)
	}

	// Verify Content-Type
	if receivedHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", receivedHeaders.Get("Content-Type"))
	}

	// Verify User-Agent
	if receivedHeaders.Get("User-Agent") != "Brain-Webhook/1.0" {
		t.Errorf("User-Agent = %q, want Brain-Webhook/1.0", receivedHeaders.Get("User-Agent"))
	}

	// Verify HMAC signature
	sig := receivedHeaders.Get("X-Brain-Signature")
	if sig == "" {
		t.Fatal("expected X-Brain-Signature header")
	}
	expectedSig := ComputeHMACSHA256(receivedBody, "test-secret")
	if sig != expectedSig {
		t.Errorf("X-Brain-Signature = %q, want %q", sig, expectedSig)
	}

	// Verify payload is valid JSON event
	var decoded types.Event
	if err := json.Unmarshal(receivedBody, &decoded); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if decoded.Type != "task.completed" {
		t.Errorf("payload type = %q, want task.completed", decoded.Type)
	}
}

func TestDeliver_NoSignatureWithoutSecret(t *testing.T) {
	var receivedHeaders http.Header

	svc, server := newTestWebhookServiceWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	})

	ctx := context.Background()
	_, err := svc.Create(ctx, types.CreateWebhookRequest{
		Name:   "no-secret-hook",
		URL:    server.URL,
		Events: []string{"task.*"},
		// No secret
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := svc.Deliver(ctx, types.Event{Type: "task.completed"}); err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	if receivedHeaders.Get("X-Brain-Signature") != "" {
		t.Error("should not have X-Brain-Signature without secret")
	}
}

func TestDeliver_SkipsNonMatchingWebhooks(t *testing.T) {
	var deliveryCount int32

	svc, server := newTestWebhookServiceWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&deliveryCount, 1)
		w.WriteHeader(http.StatusOK)
	})

	ctx := context.Background()
	_, err := svc.Create(ctx, types.CreateWebhookRequest{
		Name:   "task-only-hook",
		URL:    server.URL,
		Events: []string{"task.completed"},
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Deliver a non-matching event
	if err := svc.Deliver(ctx, types.Event{Type: "runner.started"}); err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	if atomic.LoadInt32(&deliveryCount) != 0 {
		t.Errorf("delivery count = %d, want 0 (non-matching event)", deliveryCount)
	}
}

func TestDeliver_SkipsDisabledWebhooks(t *testing.T) {
	var deliveryCount int32

	svc, server := newTestWebhookServiceWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&deliveryCount, 1)
		w.WriteHeader(http.StatusOK)
	})

	ctx := context.Background()
	disabled := false
	_, err := svc.Create(ctx, types.CreateWebhookRequest{
		Name:    "disabled-hook",
		URL:     server.URL,
		Events:  []string{"task.*"},
		Enabled: &disabled,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := svc.Deliver(ctx, types.Event{Type: "task.completed"}); err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	if atomic.LoadInt32(&deliveryCount) != 0 {
		t.Errorf("delivery count = %d, want 0 (disabled webhook)", deliveryCount)
	}
}

func TestDeliver_RetriesOnFailure(t *testing.T) {
	var attemptCount int32

	svc, server := newTestWebhookServiceWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		attempt := atomic.AddInt32(&attemptCount, 1)
		if attempt < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	// Use very short retry delays for testing
	origDelays := retryDelays
	retryDelays = []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}
	t.Cleanup(func() { retryDelays = origDelays })

	ctx := context.Background()
	_, err := svc.Create(ctx, types.CreateWebhookRequest{
		Name:   "retry-hook",
		URL:    server.URL,
		Events: []string{"task.*"},
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := svc.Deliver(ctx, types.Event{Type: "task.completed"}); err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	// Wait for retries
	time.Sleep(500 * time.Millisecond)

	count := atomic.LoadInt32(&attemptCount)
	if count != 3 {
		t.Errorf("attempt count = %d, want 3 (2 failures + 1 success)", count)
	}
}

func TestDeliver_LogsDeliveryResult(t *testing.T) {
	svc, server := newTestWebhookServiceWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ctx := context.Background()
	created, err := svc.Create(ctx, types.CreateWebhookRequest{
		Name:   "log-test-hook",
		URL:    server.URL,
		Events: []string{"task.completed"},
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := svc.Deliver(ctx, types.Event{Type: "task.completed"}); err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	// Wait for async delivery
	time.Sleep(200 * time.Millisecond)

	deliveries, err := svc.ListDeliveries(ctx, created.ID, 10)
	if err != nil {
		t.Fatalf("ListDeliveries failed: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("delivery count = %d, want 1", len(deliveries))
	}
	if !deliveries[0].Success {
		t.Error("expected delivery to be successful")
	}
	if deliveries[0].EventType != "task.completed" {
		t.Errorf("EventType = %q, want task.completed", deliveries[0].EventType)
	}
}

// ---------------------------------------------------------------------------
// matchesEventField Tests
// ---------------------------------------------------------------------------

func TestMatchesEventField(t *testing.T) {
	event := types.Event{
		ProjectID: "brain-api",
		FeatureID: "auth",
		TaskID:    "task123",
		Source:    "runner",
		RunnerID:  "runner-1",
		Metadata:  map[string]string{"env": "prod"},
	}

	tests := []struct {
		field    string
		value    string
		expected bool
	}{
		{"project_id", "brain-api", true},
		{"project_id", "other", false},
		{"feature_id", "auth", true},
		{"feature_id", "other", false},
		{"task_id", "task123", true},
		{"source", "runner", true},
		{"source", "api", false},
		{"runner_id", "runner-1", true},
		{"env", "prod", true},
		{"env", "staging", false},
		{"nonexistent", "value", false},
	}

	for _, tt := range tests {
		t.Run(tt.field+"="+tt.value, func(t *testing.T) {
			got := matchesEventField(event, tt.field, tt.value)
			if got != tt.expected {
				t.Errorf("matchesEventField(%q, %q) = %v, want %v", tt.field, tt.value, got, tt.expected)
			}
		})
	}
}

// TestWebhookDeliveries_UnknownWebhookIsNotFound closes a false negative on the
// tool people reach for when a webhook seems not to be firing.
// storage.ListDeliveries is a bare `WHERE webhook_id = ?` with no join, so a
// deleted or mistyped id returned zero rows and rendered "No deliveries recorded
// for webhook X" — the same output a live webhook that has never fired produces,
// which is precisely the distinction being debugged.
func TestWebhookDeliveries_UnknownWebhookIsNotFound(t *testing.T) {
	svc := newTestWebhookService(t)

	_, err := svc.ListDeliveries(context.Background(), "wh-does-not-exist", 50)
	if !errors.Is(err, api.ErrNotFound) {
		t.Errorf("ListDeliveries(unknown) error = %v, want api.ErrNotFound", err)
	}
}

// TestWebhookGet_UnknownWebhookIsNotFound covers the sentinel itself. Storage
// returned bare prose, so the handler's ErrNotFound -> 404 branch was
// unreachable and a missing webhook surfaced as a 500 — "server fault, retry"
// for an ordinary client mistake.
func TestWebhookGet_UnknownWebhookIsNotFound(t *testing.T) {
	svc := newTestWebhookService(t)

	_, err := svc.Get(context.Background(), "wh-does-not-exist")
	if !errors.Is(err, api.ErrNotFound) {
		t.Errorf("Get(unknown) error = %v, want api.ErrNotFound", err)
	}
}

// TestWebhookDeliveries_ExistingWebhookWithNoDeliveriesIsEmpty is the other
// half: a real webhook that has not fired must still return an empty list, not
// an error. Making unknown ids fail must not turn a legitimately empty result
// into a failure — that would move the lie rather than remove it.
func TestWebhookDeliveries_ExistingWebhookWithNoDeliveriesIsEmpty(t *testing.T) {
	svc := newTestWebhookService(t)
	ctx := context.Background()

	wh, err := svc.Create(ctx, types.CreateWebhookRequest{
		Name:   "test hook",
		URL:    "https://example.invalid/hook",
		Events: []string{"task.completed"},
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	deliveries, err := svc.ListDeliveries(ctx, wh.ID, 50)
	if err != nil {
		t.Fatalf("ListDeliveries(existing) unexpected error: %v", err)
	}
	if len(deliveries) != 0 {
		t.Errorf("expected no deliveries for a webhook that never fired, got %d", len(deliveries))
	}
}
